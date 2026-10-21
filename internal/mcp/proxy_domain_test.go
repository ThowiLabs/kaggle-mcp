package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Real OAuth flow via a stable reverse-proxy domain. The upstream may still
// be reached through an ephemeral gradio.live URL, without configuration.
func TestOAuthAutomaticallyUsesReverseProxyDomain(t *testing.T) {
	srv := newTestServer(t)
	const domain = "kg.mc365.eu.org"
	const public = "https://" + domain
	handler := srv.Handler()
	call := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "random.gradio.live"
		req.Header.Set("X-Forwarded-Host", domain)
		req.Header.Set("X-Forwarded-Proto", "https")
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	discovery := call("GET", "/.well-known/oauth-protected-resource/mcp", "")
	if discovery.Code != http.StatusOK {
		t.Fatalf("discovery %d: %s", discovery.Code, discovery.Body.String())
	}
	var protected struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.Unmarshal(discovery.Body.Bytes(), &protected); err != nil {
		t.Fatal(err)
	}
	if protected.Resource != public+"/mcp" || len(protected.AuthorizationServers) != 1 || protected.AuthorizationServers[0] != public {
		t.Fatalf("wrong public resource: %#v", protected)
	}
	metadata := call("GET", "/.well-known/oauth-authorization-server", "")
	var oauthMetadata struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		RegistrationEndpoint  string `json:"registration_endpoint"`
	}
	if err := json.Unmarshal(metadata.Body.Bytes(), &oauthMetadata); err != nil {
		t.Fatal(err)
	}
	if oauthMetadata.Issuer != public || oauthMetadata.AuthorizationEndpoint != public+"/oauth/authorize" ||
		oauthMetadata.TokenEndpoint != public+"/oauth/token" || oauthMetadata.RegistrationEndpoint != public+"/oauth/register" {
		t.Fatalf("old temporary URL leaked in metadata: %+v", oauthMetadata)
	}
	challenged := call("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if challenged.Code != http.StatusUnauthorized || !strings.Contains(challenged.Header().Get("WWW-Authenticate"), public+"/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("401 challenge: %d %s", challenged.Code, challenged.Header().Get("WWW-Authenticate"))
	}
	register := call("POST", "/oauth/register", `{"client_name":"kaggle-mcp-test","redirect_uris":["https://client.example/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`)
	if register.Code != http.StatusCreated {
		t.Fatalf("register %d %s", register.Code, register.Body.String())
	}
	var client struct {
		ID string `json:"client_id"`
	}
	if err := json.Unmarshal(register.Body.Bytes(), &client); err != nil || client.ID == "" {
		t.Fatal(err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	digest := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"https://client.example/callback"},
		"state": {"state-kg"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])},
		"code_challenge_method": {"S256"}, "resource": {public + "/mcp"}, "scope": {"root"},
	}
	auth := call("GET", "/oauth/authorize?"+query.Encode(), "")
	if auth.Code != http.StatusOK {
		t.Fatalf("authorize GET: %d %s", auth.Code, auth.Body.String())
	}
	cookie := auth.Result().Cookies()[0]
	marker := `name="authorization_session" value="`
	idx := strings.Index(auth.Body.String(), marker)
	if idx < 0 {
		t.Fatal("missing session")
	}
	rest := auth.Body.String()[idx+len(marker):]
	session := rest[:strings.Index(rest, `"`)]
	submit := url.Values{"authorization_session": {session}, "csrf": {cookie.Value}, "pin": {"LONG-TEST-PIN-ABC"}, "decision": {"approve"}}
	approved := call("POST", "/oauth/authorize", submit.Encode(), cookie)
	if approved.Code != http.StatusOK {
		t.Fatalf("authorize POST %d %s", approved.Code, approved.Body.String())
	}
	marker = `href="`
	idx = strings.Index(approved.Body.String(), marker)
	if idx < 0 {
		t.Fatalf("missing OAuth callback: %s", approved.Body.String())
	}
	rest = approved.Body.String()[idx+len(marker):]
	callback, err := url.Parse(html.UnescapeString(rest[:strings.Index(rest, `"`)]))
	if err != nil {
		t.Fatal(err)
	}
	code := callback.Query().Get("code")
	if code == "" {
		t.Fatal("missing auth code")
	}
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ID},
		"redirect_uri": {"https://client.example/callback"}, "code_verifier": {verifier}, "resource": {public + "/mcp"},
	}
	token := call("POST", "/oauth/token", form.Encode())
	if token.Code != http.StatusOK {
		t.Fatalf("token %d: %s", token.Code, token.Body.String())
	}
	var bearer struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(token.Body.Bytes(), &bearer); err != nil || bearer.AccessToken == "" {
		t.Fatal("no token")
	}
	request := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Host = "random.gradio.live"
	request.Header.Set("X-Forwarded-Host", domain)
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+bearer.AccessToken)
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusOK {
		t.Fatalf("bearer for %s rejected %d %s", domain, accepted.Code, accepted.Body.String())
	}
	// The same OAuth session remains bound to this fixed resource even after
	// the underlying ephemeral tunnel URL is replaced.
	rotated, err := rebuildMCPForPublic(srv.cfg, srv, "https://new-gradio.gradio.live")
	if err != nil {
		t.Fatal(err)
	}
	rotatedReq := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	rotatedReq.Header.Set("Content-Type", "application/json")
	rotatedReq.Header.Set("Authorization", "Bearer "+bearer.AccessToken)
	rotatedReq.Header.Set("X-Forwarded-Host", domain)
	rotatedResponse := httptest.NewRecorder()
	rotated.Handler().ServeHTTP(rotatedResponse, rotatedReq)
	if rotatedResponse.Code != http.StatusOK {
		t.Fatalf("fixed-domain token lost after tunnel rotation: %d %s", rotatedResponse.Code, rotatedResponse.Body.String())
	}
	rotatedMetadata := httptest.NewRecorder()
	rotated.Handler().ServeHTTP(rotatedMetadata, httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	if !strings.Contains(rotatedMetadata.Body.String(), "https://new-gradio.gradio.live/oauth/token") {
		t.Fatalf("direct tunnel not updated: %s", rotatedMetadata.Body.String())
	}
	fixedMetadataReq := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	fixedMetadataReq.Header.Set("X-Forwarded-Host", domain)
	fixedMetadata := httptest.NewRecorder()
	rotated.Handler().ServeHTTP(fixedMetadata, fixedMetadataReq)
	if !strings.Contains(fixedMetadata.Body.String(), public+"/oauth/token") {
		t.Fatalf("fixed proxy domain changed: %s", fixedMetadata.Body.String())
	}
	// The exact same token cannot be used for a different public audience.
	request.Host = "another.gradio.live"
	request.Header.Set("X-Forwarded-Host", "other.example.org")
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, request)
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("token replay at different domain: %d", rejected.Code)
	}
	// Legacy direct tunnel without proxy headers is unchanged.
	direct := httptest.NewRecorder()
	handler.ServeHTTP(direct, httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	if !strings.Contains(direct.Body.String(), srv.public) {
		t.Fatal("direct tunnel metadata changed unexpectedly")
	}
}

func TestOAuthProxyHostOnlyAcceptsAuthority(t *testing.T) {
	srv := newTestServer(t)
	request := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	request.Header.Set("X-Forwarded-Host", "attacker.example/redirect")
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), srv.public) {
		t.Fatal("malformed forwarded host not rejected")
	}
}

func TestOAuthUsesDirectHTTPSHostWithoutProxyHeaders(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "https://fixed.example.net/.well-known/oauth-authorization-server", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"issuer":"https://fixed.example.net"`) {
		t.Fatalf("expected direct HTTPS authority in metadata, got %d: %s", response.Code, response.Body.String())
	}
}
