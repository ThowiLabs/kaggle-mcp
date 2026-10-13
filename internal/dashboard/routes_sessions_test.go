package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPagesHaveOwnRoutesAndAreProtected(t *testing.T) {
	h := testHandler(t)
	routes := map[string]string{"/dashboard": "cpuGaugeArc", "/resources": "cpuGaugeArc", "/projects": "Nuevo proyecto", "/activity": "Historial de comandos", "/integrations": "HF_TOKEN", "/skills": "Skills disponibles", "/security": "Seguridad y sesión"}
	for path := range routes {
		resp := call(h, "GET", path, nil)
		if resp.Code != 303 || resp.Result().Header.Get("Location") != "/login" {
			t.Fatalf("%s expuesta sin login (%d)", path, resp.Code)
		}
	}
	login := call(h, "GET", "/login", nil)
	c := login.Result().Cookies()[0]
	signed := call(h, "POST", "/login", url.Values{"csrf": {c.Value}, "pin": {"TEST_LONG_ACCESS_PIN"}}, c)
	if signed.Code != 303 {
		t.Fatalf("login: %d", signed.Code)
	}
	var token *http.Cookie
	for _, cookie := range signed.Result().Cookies() {
		if cookie.Name == "__Host-kagmcp-session" {
			token = cookie
		}
	}
	if token == nil || token.MaxAge != int(sessionTTL.Seconds()) {
		t.Fatal("cookie no persiste 7 días")
	}
	for path, needle := range routes {
		resp := call(h, "GET", path, nil, token)
		if resp.Code != 200 || !strings.Contains(resp.Body.String(), needle) {
			t.Fatalf("%s: %d; falta %q", path, resp.Code, needle)
		}
		if strings.Contains(resp.Body.String(), "href=\"#resources\"") {
			t.Fatalf("%s sigue apuntando a hash", path)
		}
	}
}

func TestSessionSurvivesNewHandlerAndIsRevocable(t *testing.T) {
	h := testHandler(t)
	path := filepath.Join(t.TempDir(), "state", "web-sessions.json")
	if err := h.EnablePersistence(path); err != nil {
		t.Fatal(err)
	}
	start := call(h, "GET", "/login", nil)
	cookie := start.Result().Cookies()[0]
	done := call(h, "POST", "/login", url.Values{"csrf": {cookie.Value}, "pin": {"TEST_LONG_ACCESS_PIN"}}, cookie)
	var sessionCookie *http.Cookie
	for _, c := range done.Result().Cookies() {
		if c.Name == "__Host-kagmcp-session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("cookie ausente")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), sessionCookie.Value) {
		t.Fatal("token de sesión en claro en el disco")
	}
	if strings.Contains(string(raw), "TEST_LONG_ACCESS_PIN") {
		t.Fatal("PIN en archivo")
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("permisos inseguros: %o", info.Mode().Perm())
	}
	// Simulate a process restart while retaining the same browser cookie/domain.
	newer := testHandler(t)
	if err := newer.EnablePersistence(path); err != nil {
		t.Fatal(err)
	}
	if page := call(newer, "GET", "/resources", nil, sessionCookie); page.Code != 200 {
		t.Fatalf("sesión perdida tras reinicio: %d", page.Code)
	}
	csrf := newer.sessions[key(sessionCookie.Value)].CSRF
	logout := call(newer, "POST", "/logout", url.Values{"csrf": {csrf}}, sessionCookie)
	if logout.Code != 303 {
		t.Fatalf("logout: %d", logout.Code)
	}
	renewed := testHandler(t)
	if err := renewed.EnablePersistence(path); err != nil {
		t.Fatal(err)
	}
	if denied := call(renewed, "GET", "/resources", nil, sessionCookie); denied.Code != 303 {
		t.Fatal("logout no se persistió")
	}
}

func TestSessionExpiryRemovesStaleEntries(t *testing.T) {
	h := testHandler(t)
	file := filepath.Join(t.TempDir(), "sessions.json")
	h.sessions["abc"] = session{CSRF: "z", Expires: time.Now().Add(-time.Hour)}
	if err := h.EnablePersistence(file); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	err := h.saveSessionsLocked()
	h.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	var entries map[string]session
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("sesiones expiradas persisten: %d", len(entries))
	}
}

// A real TLS cookie jar must keep a signed-in session across page refreshes
// and recreation of the dashboard handler on the same public domain.
func TestBrowserSessionReloadAndServerRestart(t *testing.T) {
	h := testHandler(t)
	state := filepath.Join(t.TempDir(), "state", "sessions.json")
	if err := h.EnablePersistence(state); err != nil {
		t.Fatal(err)
	}
	current := h
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.ServeHTTP(w, r) }))
	defer server.Close()
	h.Public = server.URL
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	response, err := client.Get(server.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	var loginCSRF string
	endpoint, _ := url.Parse(server.URL)
	for _, c := range jar.Cookies(endpoint) {
		if c.Name == "__Host-kagmcp-login" {
			loginCSRF = c.Value
		}
	}
	if loginCSRF == "" {
		t.Fatal("missing csrf cookie")
	}
	response, err = client.PostForm(server.URL+"/login", url.Values{"csrf": {loginCSRF}, "pin": {"TEST_LONG_ACCESS_PIN"}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("login failed: %d", response.StatusCode)
	}
	for i := 0; i < 4; i++ {
		response, err = client.Get(server.URL + "/dashboard")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("refresh #%d redirected to login: %d", i, response.StatusCode)
		}
	}
	newer := testHandler(t)
	if err := newer.EnablePersistence(state); err != nil {
		t.Fatal(err)
	}
	newer.Public = server.URL
	current = newer
	response, err = client.Get(server.URL + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("session not restored after restart: %d", response.StatusCode)
	}
}
