package oauth

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultRefreshSessionTTLIsOneYear(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: filepath.Join(t.TempDir(), "oauth.json")})
	if err != nil {
		t.Fatal(err)
	}
	if s.refreshTTL != 365*24*time.Hour {
		t.Fatalf("refresh TTL = %s, want 8760h", s.refreshTTL)
	}
}

func TestOAuthAuthorizationCodePKCEAndRefresh(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", AccessTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	regBody := `{"client_name":"test","redirect_uris":["https://client.example/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(regBody)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &reg); err != nil || reg.ClientID == "" {
		t.Fatalf("decode registration: %v, id=%q", err, reg.ClientID)
	}

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{
		"response_type": {"code"}, "client_id": {reg.ClientID}, "redirect_uri": {"https://client.example/callback"},
		"state": {"state-1"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"resource": {"https://example.test/mcp"}, "scope": {"root"},
	}

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
	if get.Code != http.StatusOK {
		t.Fatalf("authorize get status=%d body=%s", get.Code, get.Body.String())
	}
	cookies := get.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("csrf cookie missing")
	}
	csrf := cookies[0].Value
	sessionID := hiddenFormValue(t, get.Body.String(), "authorization_session")
	postForm := url.Values{"authorization_session": {sessionID}, "csrf": {csrf}, "decision": {"approve"}}

	postReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(postForm.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookies[0])
	post := httptest.NewRecorder()
	mux.ServeHTTP(post, postReq)
	if post.Code != http.StatusOK {
		t.Fatalf("authorize post status=%d body=%s", post.Code, post.Body.String())
	}
	loc := handoffURL(t, post.Body.String())
	code := loc.Query().Get("code")
	if code == "" || loc.Query().Get("state") != "state-1" {
		t.Fatalf("invalid redirect: %s", loc.String())
	}

	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {reg.ClientID}, "redirect_uri": {"https://client.example/callback"}, "code_verifier": {verifier}, "resource": {"https://example.test/mcp"}}
	tokRR := httptest.NewRecorder()
	tokReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(tokRR, tokReq)
	if tokRR.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", tokRR.Code, tokRR.Body.String())
	}
	var tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.Unmarshal(tokRR.Body.Bytes(), &tokens); err != nil || tokens.Access == "" || tokens.Refresh == "" {
		t.Fatalf("decode tokens: %v %+v", err, tokens)
	}

	mcpReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	mcpReq.Header.Set("Authorization", "Bearer "+tokens.Access)
	if err := s.ValidateBearer(mcpReq); err != nil {
		t.Fatalf("validate bearer: %v", err)
	}

	refreshForm := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.Refresh}, "client_id": {reg.ClientID}, "resource": {"https://example.test/mcp"}}
	refreshRR := httptest.NewRecorder()
	refreshReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(refreshForm.Encode()))
	refreshReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshRR.Code, refreshRR.Body.String())
	}

	replayRR := httptest.NewRecorder()
	replayReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(refreshForm.Encode()))
	replayReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(replayRR, replayReq)
	if replayRR.Code != http.StatusBadRequest {
		t.Fatalf("refresh replay should fail: status=%d body=%s", replayRR.Code, replayRR.Body.String())
	}
}

func TestProtectedResourceMetadataIncludesLilithIdentity(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("metadata status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["resource_name"] != "KagMCP" || payload["resource_name#en"] != "KagMCP" {
		t.Fatalf("resource identity missing: %#v", payload)
	}
}

func TestOAuthRejectsInvalidPKCE(t *testing.T) {
	if verifyPKCE("verifier", "wrong") {
		t.Fatal("invalid PKCE accepted")
	}
}

func TestRedirectURIValidation(t *testing.T) {
	for _, tc := range []struct {
		uri string
		ok  bool
	}{
		{"https://example.com/callback", true},
		{"http://localhost:8080/callback", true},
		{"http://127.0.0.1:8080/callback", true},
		{"http://example.com/callback", false},
		{"file:///tmp/callback", false},
		{"https://example.com/callback#fragment", false},
	} {
		if got := validRedirectURI(tc.uri); got != tc.ok {
			t.Fatalf("validRedirectURI(%q)=%v want %v", tc.uri, got, tc.ok)
		}
	}
}

func TestClientCertificateRequirement(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", RequireClientCertificate: true})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	body := `{"client_name":"test","redirect_uris":["https://client.example/callback"]}`
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body)))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("registration without client certificate status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClientCertificateDNSName(t *testing.T) {
	cert := &x509.Certificate{DNSNames: []string{"client.example"}}
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	if !hasVerifiedClientCertificate(r, "client.example") {
		t.Fatal("verified matching client certificate rejected")
	}
	if hasVerifiedClientCertificate(r, "other.example") {
		t.Fatal("client certificate with wrong DNS SAN accepted")
	}
}

func TestWWWAuthenticateIncludesOAuthMetadata(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json"})
	if err != nil {
		t.Fatal(err)
	}
	value := s.WWWAuthenticate("invalid_token", "Authentication required")
	for _, expected := range []string{"resource_metadata=", "/.well-known/oauth-protected-resource/mcp", `scope="root"`, `error="invalid_token"`, `error_description="Authentication required"`} {
		if !strings.Contains(value, expected) {
			t.Fatalf("challenge %q missing %q", value, expected)
		}
	}
}

func TestAuthorizationSessionSurvivesServerRestart(t *testing.T) {
	stateFile := t.TempDir() + "/state.json"
	first, err := New(Config{PublicURL: "https://example.test", StateFile: stateFile})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	first.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	q := testAuthorizeValues(clientID)

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
	if get.Code != http.StatusOK {
		t.Fatalf("authorize get status=%d body=%s", get.Code, get.Body.String())
	}
	cookies := get.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("csrf cookie missing")
	}

	// Simula un reinicio entre mostrar el consentimiento y enviarlo. La sesión
	// ya no depende de un mapa CSRF en memoria.
	second, err := New(Config{PublicURL: "https://example.test", StateFile: stateFile})
	if err != nil {
		t.Fatal(err)
	}
	mux2 := http.NewServeMux()
	second.RegisterRoutes(mux2)
	sessionID := hiddenFormValue(t, get.Body.String(), "authorization_session")
	postForm := url.Values{"authorization_session": {sessionID}, "csrf": {cookies[0].Value}, "decision": {"approve"}}
	postReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(postForm.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookies[0])
	post := httptest.NewRecorder()
	mux2.ServeHTTP(post, postReq)
	if post.Code != http.StatusOK {
		t.Fatalf("authorization after restart status=%d body=%s", post.Code, post.Body.String())
	}
	_ = handoffURL(t, post.Body.String())
}

func TestAccessPINAndRateLimit(t *testing.T) {
	s, err := New(Config{
		PublicURL:      "https://example.test",
		StateFile:      t.TempDir() + "/state.json",
		AccessPIN:      "654321",
		PINMaxAttempts: 2,
		PINWindow:      time.Minute,
		PINLockout:     time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	q := testAuthorizeValues(clientID)

	get := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	getReq.RemoteAddr = "127.0.0.1:43210"
	getReq.Header.Set("X-Forwarded-For", "203.0.113.10")
	mux.ServeHTTP(get, getReq)
	cookies := get.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("csrf cookie missing")
	}
	sessionID := hiddenFormValue(t, get.Body.String(), "authorization_session")
	postForm := url.Values{"authorization_session": {sessionID}, "csrf": {cookies[0].Value}, "decision": {"approve"}, "pin": {"wrong-pin"}}

	for attempt, wantStatus := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		postReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(postForm.Encode()))
		postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		postReq.Header.Set("X-Forwarded-For", "203.0.113.10")
		postReq.RemoteAddr = "127.0.0.1:43210"
		postReq.AddCookie(cookies[0])
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, postReq)
		if rr.Code != wantStatus {
			t.Fatalf("attempt %d status=%d want=%d body=%s", attempt+1, rr.Code, wantStatus, rr.Body.String())
		}
		if wantStatus == http.StatusTooManyRequests && rr.Header().Get("Retry-After") == "" {
			t.Fatal("rate limited response missing Retry-After")
		}
	}

	postForm.Set("pin", "654321")
	goodReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(postForm.Encode()))
	goodReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	goodReq.Header.Set("X-Forwarded-For", "203.0.113.11")
	goodReq.RemoteAddr = "127.0.0.1:43210"
	goodReq.AddCookie(cookies[0])
	good := httptest.NewRecorder()
	mux.ServeHTTP(good, goodReq)
	if good.Code != http.StatusOK {
		t.Fatalf("correct PIN from unblocked IP status=%d body=%s", good.Code, good.Body.String())
	}
	_ = handoffURL(t, good.Body.String())
}

func TestClientIPTrustsForwardedHeaderOnlyFromLoopback(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/oauth/authorize", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 198.51.100.8")
	if got := clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP behind loopback proxy=%q", got)
	}

	r.RemoteAddr = "198.51.100.20:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.99")
	if got := clientIP(r); got != "198.51.100.20" {
		t.Fatalf("clientIP trusted spoofed forwarded header=%q", got)
	}
}

func registerTestClient(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	body := `{"client_name":"test","redirect_uris":["https://client.example/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &reg); err != nil || reg.ClientID == "" {
		t.Fatalf("decode registration: %v, id=%q", err, reg.ClientID)
	}
	return reg.ClientID
}

func testAuthorizeValues(clientID string) url.Values {
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"https://client.example/callback"},
		"state": {"state-test"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"resource": {"https://example.test/mcp"}, "scope": {"root"},
	}
}

func hiddenFormValue(t *testing.T, body, name string) string {
	t.Helper()
	marker := `name="` + name + `" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("hidden field %q not found in body: %s", name, body)
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatalf("hidden field %q has no closing quote", name)
	}
	return body[start : start+end]
}

func handoffURL(t *testing.T, body string) *url.URL {
	t.Helper()
	if !strings.Contains(body, `<meta http-equiv="refresh"`) {
		t.Fatalf("handoff page missing automatic navigation: %s", body)
	}
	marker := `href="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("handoff link not found in body: %s", body)
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatalf("handoff link has no closing quote: %s", body)
	}
	raw := html.UnescapeString(body[start : start+end])
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse handoff URL: %v", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		t.Fatalf("invalid handoff URL: %s", raw)
	}
	return u
}

func TestAuthorizationApprovalIsIdempotentUntilCodeRedeemed(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", AccessPIN: "654321"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	q := testAuthorizeValues(clientID)

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
	cookies := get.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("csrf cookie missing")
	}
	sessionID := hiddenFormValue(t, get.Body.String(), "authorization_session")
	form := url.Values{"authorization_session": {sessionID}, "csrf": {cookies[0].Value}, "decision": {"approve"}, "pin": {"654321"}}

	postAuthorization := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookies[0])
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	first := postAuthorization()
	if first.Code != http.StatusOK {
		t.Fatalf("first approval status=%d body=%s", first.Code, first.Body.String())
	}
	firstLoc := handoffURL(t, first.Body.String())
	code := firstLoc.Query().Get("code")
	if code == "" {
		t.Fatal("first approval did not return code")
	}

	second := postAuthorization()
	if second.Code != http.StatusOK {
		t.Fatalf("second approval status=%d body=%s", second.Code, second.Body.String())
	}
	secondLoc := handoffURL(t, second.Body.String())
	if got := secondLoc.Query().Get("code"); got != code {
		t.Fatalf("duplicate approval changed authorization code: first=%q second=%q", code, got)
	}

	s.store.mu.Lock()
	ownerBeforeExchange := s.store.data.OwnerClientID
	s.store.mu.Unlock()
	if ownerBeforeExchange != "" {
		t.Fatalf("owner was claimed before token exchange: %q", ownerBeforeExchange)
	}

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	tokenForm := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {"https://client.example/callback"}, "code_verifier": {verifier}, "resource": {"https://example.test/mcp"},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRR := httptest.NewRecorder()
	mux.ServeHTTP(tokenRR, tokenReq)
	if tokenRR.Code != http.StatusOK {
		t.Fatalf("token exchange status=%d body=%s", tokenRR.Code, tokenRR.Body.String())
	}

	s.store.mu.Lock()
	ownerAfterExchange := s.store.data.OwnerClientID
	s.store.mu.Unlock()
	if ownerAfterExchange != clientID {
		t.Fatalf("owner after token exchange=%q want=%q", ownerAfterExchange, clientID)
	}

	third := postAuthorization()
	if third.Code != http.StatusOK || !strings.Contains(third.Body.String(), "Autorización completada") {
		t.Fatalf("approval after completed exchange status=%d body=%s", third.Code, third.Body.String())
	}
}

func TestFailedPKCEDoesNotClaimOwnerAndCanRetryConsent(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json", AccessPIN: "654321"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	clientID := registerTestClient(t, mux)
	q := testAuthorizeValues(clientID)

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
	cookies := get.Result().Cookies()
	sessionID := hiddenFormValue(t, get.Body.String(), "authorization_session")
	form := url.Values{"authorization_session": {sessionID}, "csrf": {cookies[0].Value}, "decision": {"approve"}, "pin": {"654321"}}
	postReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookies[0])
	post := httptest.NewRecorder()
	mux.ServeHTTP(post, postReq)
	loc := handoffURL(t, post.Body.String())
	firstCode := loc.Query().Get("code")
	if firstCode == "" {
		t.Fatalf("first approval status=%d body=%s", post.Code, post.Body.String())
	}

	badTokenForm := url.Values{
		"grant_type": {"authorization_code"}, "code": {firstCode}, "client_id": {clientID},
		"redirect_uri": {"https://client.example/callback"}, "code_verifier": {"wrong-verifier"}, "resource": {"https://example.test/mcp"},
	}
	badReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(badTokenForm.Encode()))
	badReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badRR := httptest.NewRecorder()
	mux.ServeHTTP(badRR, badReq)
	if badRR.Code != http.StatusBadRequest {
		t.Fatalf("bad PKCE status=%d body=%s", badRR.Code, badRR.Body.String())
	}

	s.store.mu.Lock()
	owner := s.store.data.OwnerClientID
	s.store.mu.Unlock()
	if owner != "" {
		t.Fatalf("failed PKCE claimed owner %q", owner)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	retryReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	retryReq.AddCookie(cookies[0])
	retry := httptest.NewRecorder()
	mux.ServeHTTP(retry, retryReq)
	if retry.Code != http.StatusOK {
		t.Fatalf("retry approval status=%d body=%s", retry.Code, retry.Body.String())
	}
	retryLoc := handoffURL(t, retry.Body.String())
	secondCode := retryLoc.Query().Get("code")
	if secondCode == "" || secondCode == firstCode {
		t.Fatalf("retry code=%q first=%q", secondCode, firstCode)
	}
}

func TestNewReleasesLegacyProvisionalOwnerWithoutTokens(t *testing.T) {
	stateFile := t.TempDir() + "/state.json"
	store, err := OpenStore(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.data.Clients["legacy-client"] = Client{ID: "legacy-client", RedirectURIs: []string{"https://client.example/callback"}, CreatedAt: time.Now().UTC()}
	store.data.OwnerClientID = "legacy-client"
	if err := store.saveLocked(); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()

	s, err := New(Config{PublicURL: "https://example.test", StateFile: stateFile})
	if err != nil {
		t.Fatal(err)
	}
	s.store.mu.Lock()
	owner := s.store.data.OwnerClientID
	s.store.mu.Unlock()
	if owner != "" {
		t.Fatalf("legacy provisional owner was not released: %q", owner)
	}
}
