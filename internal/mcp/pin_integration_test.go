package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThowiLabs/kagssh-go/internal/config"
)

func TestBlankPINGeneratedByGo(t *testing.T) {
	cfg := config.Config{MCPEnabled: true, MCPPort: 8181, MCPTunnel: "none", MCPPublicURL: "https://sample.trycloudflare.com", MCPAccessPIN: "", DataDir: filepath.Join(t.TempDir(), "state")}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, cfg.MCPPublicURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.generatedPIN) != 20 || !s.pinState.Verify(s.generatedPIN) || s.cfg.MCPAccessPIN != "" {
		t.Fatal("Go no generó PIN único de 20 caracteres en memoria")
	}
	token := issueAccessTokenForTest(t, s, s.generatedPIN)
	code, _ := postMCPAuthenticated(t, s, token, "tools/list", protocol, map[string]any{}, "")
	if code != 200 {
		t.Fatalf("el PIN temporal no autorizó MCP: %d", code)
	}
}

func TestPINRotatedInWebRevokesOAuthAndAllowsNewConnection(t *testing.T) {
	s := newTestServer(t)
	token := issueAccessTokenForTest(t, s)
	status, _ := postMCPAuthenticated(t, s, token, "tools/list", protocol, map[string]any{}, "")
	if status != 200 {
		t.Fatalf("OAuth anterior inactivo antes de rotar: %d", status)
	}
	handler := s.Handler()
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest("GET", "/login", nil))
	cookie := get.Result().Cookies()[0]
	login := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"pin": {"LONG-TEST-PIN-ABC"}, "csrf": {cookie.Value}}.Encode()))
	login.Header.Set("Origin", s.public)
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.AddCookie(cookie)
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, login)
	if authorized.Code != 303 {
		t.Fatalf("login PIN anterior: %d", authorized.Code)
	}
	var session *http.Cookie
	for _, c := range authorized.Result().Cookies() {
		if c.Name == "__Host-kagmcp-session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no cookie sesión")
	}
	home := httptest.NewRecorder()
	homeReq := httptest.NewRequest("GET", "/", nil)
	homeReq.AddCookie(session)
	handler.ServeHTTP(home, homeReq)
	if home.Code != 200 {
		t.Fatalf("home HTTP %d", home.Code)
	}
	key := `name="csrf" value="`
	i := strings.Index(home.Body.String(), key)
	if i < 0 {
		t.Fatal("no CSRF")
	}
	csrf := strings.SplitN(home.Body.String()[i+len(key):], `"`, 2)[0]
	post := httptest.NewRequest("POST", "/change-pin", strings.NewReader(url.Values{"current_pin": {"LONG-TEST-PIN-ABC"}, "new_pin": {"654321"}, "csrf": {csrf}}.Encode()))
	post.AddCookie(session)
	post.Header.Set("Origin", s.public)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, post)
	if response.Code != 303 {
		t.Fatalf("cambio PIN HTTP %d: %s", response.Code, response.Body.String())
	}
	if s.pinState.Verify("LONG-TEST-PIN-ABC") || !s.pinState.Verify("654321") {
		t.Fatal("PIN no actualizado en OAuth")
	}
	expired, _ := postMCPAuthenticated(t, s, token, "tools/list", protocol, map[string]any{}, "")
	if expired != 401 {
		t.Fatalf("OAuth anterior sobrevivió a cambio de PIN: %d", expired)
	}
	newToken := issueAccessTokenForTest(t, s, "654321")
	status, _ = postMCPAuthenticated(t, s, newToken, "tools/list", protocol, map[string]any{}, "")
	if status != 200 {
		t.Fatalf("OAuth con nuevo PIN: %d", status)
	}
}
