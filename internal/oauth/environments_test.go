package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDeriveEnvironmentPINHashMatchesPBKDF2SHA256Vector(t *testing.T) {
	got := deriveEnvironmentPINHash("password", []byte("salt"), 1)
	want := "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"
	if fmt.Sprintf("%x", got) != want {
		t.Fatalf("PBKDF2 hash=%x want=%s", got, want)
	}
}

func TestEnvironmentPINIsPersistedAsSlowHashAndNotListed(t *testing.T) {
	stateFile := t.TempDir() + "/state.json"
	s, err := New(Config{PublicURL: "https://example.test", StateFile: stateFile})
	if err != nil {
		t.Fatal(err)
	}
	env, pin, err := s.CreateEnvironment("Personal", 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(pin) != environmentPINLen || env.Status != "active" {
		t.Fatalf("unexpected environment or PIN: env=%+v pin_len=%d", env, len(pin))
	}

	raw, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), pin) {
		t.Fatal("plaintext environment PIN was persisted")
	}
	if !strings.Contains(string(raw), `"pin_hash": "pbkdf2-sha256$`) {
		t.Fatalf("state does not contain a slow salted PIN hash: %s", raw)
	}

	listed := s.ListEnvironments()
	if len(listed) != 1 || listed[0].ID != env.ID || listed[0].Name != "Personal" {
		t.Fatalf("unexpected environment list: %+v", listed)
	}
	encoded, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "pin") || strings.Contains(string(encoded), "hash") {
		t.Fatalf("environment summary exposed credential material: %s", encoded)
	}
}

func TestEnvironmentRotationRevocationAndExpiryInvalidateOAuth(t *testing.T) {
	s, err := New(Config{
		PublicURL:  "https://example.test",
		StateFile:  t.TempDir() + "/state.json",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	env, firstPIN, err := s.CreateEnvironment("Personal", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	first := authorizeEnvironmentToken(t, mux, clientID, firstPIN)
	assertBearerAccepted(t, s, first.Access)

	rotated, secondPIN, err := s.RotateEnvironment(env.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if secondPIN == firstPIN || rotated.Revision != env.Revision+1 {
		t.Fatalf("rotation did not change credential: env=%+v", rotated)
	}
	assertBearerError(t, s, first.Access, ErrEnvironmentRotated)
	assertRefreshErrorContains(t, mux, clientID, first.Refresh, ErrEnvironmentRotated.Error())

	second := authorizeEnvironmentToken(t, mux, clientID, secondPIN)
	assertBearerAccepted(t, s, second.Access)
	if _, err := s.RevokeEnvironment(env.ID); err != nil {
		t.Fatal(err)
	}
	assertBearerError(t, s, second.Access, ErrEnvironmentRevoked)

	_, thirdPIN, err := s.RotateEnvironment(env.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	third := authorizeEnvironmentToken(t, mux, clientID, thirdPIN)
	assertBearerAccepted(t, s, third.Access)

	s.store.mu.Lock()
	current := s.store.data.Environments[env.ID]
	current.ExpiresAt = time.Now().UTC().Add(-time.Second)
	s.store.data.Environments[env.ID] = current
	if err := s.store.saveLocked(); err != nil {
		s.store.mu.Unlock()
		t.Fatal(err)
	}
	s.store.mu.Unlock()
	assertBearerError(t, s, third.Access, ErrEnvironmentExpired)
	assertRefreshErrorContains(t, mux, clientID, third.Refresh, ErrEnvironmentExpired.Error())
	if s.IsLinked() {
		t.Fatal("expired environment still reported as linked")
	}
}

func TestFirstEnvironmentDisablesLegacyBearer(t *testing.T) {
	s, err := New(Config{
		PublicURL: "https://example.test",
		StateFile: t.TempDir() + "/state.json",
		AccessPIN: "654321",
		AccessTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	legacy := authorizeEnvironmentToken(t, mux, clientID, "654321")
	assertBearerAccepted(t, s, legacy.Access)

	if _, _, err := s.CreateEnvironment("Personal", 30*time.Minute); err != nil {
		t.Fatal(err)
	}
	assertBearerError(t, s, legacy.Access, ErrEnvironmentRequired)
	assertRefreshErrorContains(t, mux, clientID, legacy.Refresh, ErrEnvironmentRequired.Error())
}

type testOAuthTokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
}

func authorizeEnvironmentToken(t *testing.T, mux *http.ServeMux, clientID, pin string) testOAuthTokens {
	t.Helper()
	q := testAuthorizeValues(clientID)
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
	if get.Code != http.StatusOK {
		t.Fatalf("authorize GET status=%d body=%s", get.Code, get.Body.String())
	}
	cookies := get.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("csrf cookie missing")
	}
	sessionID := hiddenFormValue(t, get.Body.String(), "authorization_session")
	postForm := url.Values{
		"authorization_session": {sessionID},
		"csrf":                  {cookies[0].Value},
		"decision":              {"approve"},
	}
	if pin != "" {
		postForm.Set("pin", pin)
	}
	postReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(postForm.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookies[0])
	post := httptest.NewRecorder()
	mux.ServeHTTP(post, postReq)
	if post.Code != http.StatusOK {
		t.Fatalf("authorize POST status=%d body=%s", post.Code, post.Body.String())
	}
	code := handoffURL(t, post.Body.String()).Query().Get("code")
	if code == "" {
		t.Fatal("authorization code missing")
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {"https://client.example/callback"},
		"code_verifier": {"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"},
		"resource":      {"https://example.test/mcp"},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRR := httptest.NewRecorder()
	mux.ServeHTTP(tokenRR, tokenReq)
	if tokenRR.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", tokenRR.Code, tokenRR.Body.String())
	}
	var tokens testOAuthTokens
	if err := json.Unmarshal(tokenRR.Body.Bytes(), &tokens); err != nil || tokens.Access == "" || tokens.Refresh == "" {
		t.Fatalf("decode tokens: %v %+v", err, tokens)
	}
	return tokens
}

func assertBearerAccepted(t *testing.T, s *Server, token string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if err := s.ValidateBearer(req); err != nil {
		t.Fatalf("bearer rejected: %v", err)
	}
}

func assertBearerError(t *testing.T, s *Server, token string, want error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	err := s.ValidateBearer(req)
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("bearer error=%v want=%v", err, want)
	}
}

func assertRefreshErrorContains(t *testing.T, mux *http.ServeMux, clientID, refresh, want string) {
	t.Helper()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {clientID},
		"resource":      {"https://example.test/mcp"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), want) {
		t.Fatalf("refresh status=%d body=%s want=%q", rr.Code, rr.Body.String(), want)
	}
}

func refreshEnvironmentToken(t *testing.T, mux *http.ServeMux, clientID, refresh string) testOAuthTokens {
	t.Helper()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {clientID},
		"resource":      {"https://example.test/mcp"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", rr.Code, rr.Body.String())
	}
	var tokens testOAuthTokens
	if err := json.Unmarshal(rr.Body.Bytes(), &tokens); err != nil || tokens.Access == "" || tokens.Refresh == "" {
		t.Fatalf("decode refreshed tokens: %v %+v", err, tokens)
	}
	return tokens
}

func sandboxBearerRequest(t *testing.T, s *Server, token string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "192.0.2.10:43120"
	req.Header.Set("Authorization", "Bearer "+token)
	if err := s.ValidateBearer(req); err != nil {
		t.Fatalf("sandbox bearer rejected: %v", err)
	}
	return req
}

func TestSandboxOAuthGrantStaysOperationalUntilRotationOrExpiry(t *testing.T) {
	s, err := New(Config{
		PublicURL:      "https://example.test",
		StateFile:      t.TempDir() + "/state.json",
		AccessTTL:      time.Hour,
		RefreshTTL:     24 * time.Hour,
		MultiTenant:    true,
		RequireSandbox: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	env, firstPIN, err := s.CreateSandboxEnvironment("Temporal", "user-1", "sandbox-1", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	tokens := authorizeEnvironmentToken(t, mux, clientID, firstPIN)

	req := sandboxBearerRequest(t, s, tokens.Access)
	grant, ok := AccessGrantFromRequest(req)
	if !ok || grant.EnvironmentID != env.ID || grant.OwnerUserID != "user-1" || grant.SandboxID != "sandbox-1" {
		t.Fatalf("unexpected sandbox grant: ok=%v grant=%+v", ok, grant)
	}
	// A bearer issued after OAuth remains usable without any per-tool PIN gate.
	sandboxBearerRequest(t, s, tokens.Access)

	_, secondPIN, err := s.RotateEnvironment(env.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	assertBearerError(t, s, tokens.Access, ErrEnvironmentRotated)
	assertRefreshErrorContains(t, mux, clientID, tokens.Refresh, ErrEnvironmentRotated.Error())

	rotated := authorizeEnvironmentToken(t, mux, clientID, secondPIN)
	sandboxBearerRequest(t, s, rotated.Access)

	s.store.mu.Lock()
	current := s.store.data.Environments[env.ID]
	current.ExpiresAt = time.Now().UTC().Add(-time.Second)
	s.store.data.Environments[env.ID] = current
	if err := s.store.saveLocked(); err != nil {
		s.store.mu.Unlock()
		t.Fatal(err)
	}
	s.store.mu.Unlock()

	assertBearerError(t, s, rotated.Access, ErrEnvironmentExpired)
	assertRefreshErrorContains(t, mux, clientID, rotated.Refresh, ErrEnvironmentExpired.Error())
	if s.IsLinked() {
		t.Fatal("expired sandbox grant still reported as linked")
	}
}

func TestPermanentSandboxPINAndGrantContext(t *testing.T) {
	s, err := New(Config{
		PublicURL:      "https://example.test",
		StateFile:      t.TempDir() + "/state.json",
		AccessTTL:      time.Hour,
		RefreshTTL:     24 * time.Hour,
		MultiTenant:    true,
		RequireSandbox: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	env, pin, err := s.CreateSandboxEnvironment("Personal", "user-1", "sandbox-1", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if !env.Permanent || !env.ExpiresAt.IsZero() {
		t.Fatalf("unexpected permanent environment: %+v", env)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	tokens := authorizeEnvironmentToken(t, mux, clientID, pin)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.Access)
	if err := s.ValidateBearer(req); err != nil {
		t.Fatal(err)
	}
	grant, ok := AccessGrantFromRequest(req)
	if !ok || grant.OwnerUserID != "user-1" || grant.SandboxID != "sandbox-1" || !grant.Permanent {
		t.Fatalf("unexpected sandbox grant: ok=%v grant=%+v", ok, grant)
	}

	s.store.mu.Lock()
	current := s.store.data.Environments[env.ID]
	current.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	s.store.data.Environments[env.ID] = current
	s.store.mu.Unlock()
	assertBearerAccepted(t, s, tokens.Access)
}

func TestMultiTenantAllowsDifferentOAuthClientsForDifferentSandboxes(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", MultiTenant: true, RequireSandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	_, pin1, err := s.CreateSandboxEnvironment("Uno", "user-1", "sandbox-1", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	_, pin2, err := s.CreateSandboxEnvironment("Dos", "user-2", "sandbox-2", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	client1 := registerTestClient(t, mux)
	client2 := registerTestClient(t, mux)
	one := authorizeEnvironmentToken(t, mux, client1, pin1)
	two := authorizeEnvironmentToken(t, mux, client2, pin2)
	assertBearerAccepted(t, s, one.Access)
	assertBearerAccepted(t, s, two.Access)
}

func TestRequireSandboxRejectsLegacyUnboundEnvironment(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", MultiTenant: true, RequireSandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	_, pin, err := s.CreateEnvironment("Legacy", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	q := testAuthorizeValues(clientID)
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
	cookie := get.Result().Cookies()[0]
	sessionID := hiddenFormValue(t, get.Body.String(), "authorization_session")
	form := url.Values{"authorization_session": {sessionID}, "csrf": {cookie.Value}, "decision": {"approve"}, "pin": {pin}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), "incorrect") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMasterEnvironmentIsAcceptedInSandboxModeAndCarriesMasterGrant(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", MultiTenant: true, RequireSandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	env, pin, err := s.CreateMasterEnvironment("Control maestro MCP", "admin-1", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if env.Kind != EnvironmentKindMaster || env.SandboxID != "" || !env.Permanent {
		t.Fatalf("unexpected master environment: %+v", env)
	}
	if _, _, err := s.CreateMasterEnvironment("Otro", "admin-1", 0, true); err == nil {
		t.Fatal("a second master environment should be rejected")
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	tokens := authorizeEnvironmentToken(t, mux, clientID, pin)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.Access)
	if err := s.ValidateBearer(req); err != nil {
		t.Fatal(err)
	}
	grant, ok := AccessGrantFromRequest(req)
	if !ok || grant.Kind != EnvironmentKindMaster || grant.OwnerUserID != "admin-1" || grant.SandboxID != "" || !grant.Permanent {
		t.Fatalf("unexpected master grant: ok=%v grant=%+v", ok, grant)
	}
}

func TestValidateMasterPINRejectsSandboxPIN(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", MultiTenant: true, RequireSandbox: true})
	if err != nil {
		t.Fatal(err)
	}
	_, masterPIN, err := s.CreateMasterEnvironment("Windows", "windows-host", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateMasterPIN(masterPIN, time.Now().UTC()); err != nil {
		t.Fatalf("master PIN should validate: %v", err)
	}
	_, sandboxPIN, err := s.CreateSandboxEnvironment("sb", "owner", "sandbox-1", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateMasterPIN(sandboxPIN, time.Now().UTC()); err == nil {
		t.Fatal("sandbox PIN must not validate as master")
	}
}

func TestAgentModeAuthorizationPageUsesMasterPINLanguage(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", MultiTenant: true, RequireSandbox: true, AgentMode: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateMasterEnvironment("Agente directo", "direct-host", 0, true); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+testAuthorizeValues(clientID).Encode(), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", rr.Code, rr.Body.String())
	}
	for _, want := range []string{"Autorizar agente directo", "PIN maestro", "directamente sobre este equipo"} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Fatalf("agent authorization page missing %q: %s", want, rr.Body.String())
		}
	}
}
