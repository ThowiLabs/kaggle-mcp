package dashboard

import (
	"github.com/ThowiLabs/kagssh-go/internal/pinauth"
	"github.com/ThowiLabs/kagssh-go/internal/projectstate"
	"github.com/ThowiLabs/kagssh-go/internal/skills"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

const baseURL = "https://sample.trycloudflare.com"

func testHandler(t *testing.T) *Handler {
	store, err := projectstate.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := pinauth.New("TEST_LONG_ACCESS_PIN")
	if err != nil {
		t.Fatal(err)
	}
	return New(baseURL, pin, nil, store, skills.Registry{Dir: t.TempDir()}, nil, func() bool { return false }, nil, nil)
}
func call(h http.Handler, method, path string, values url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	body := ""
	if values != nil {
		body = values.Encode()
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == "POST" {
		req.Header.Set("Origin", baseURL)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
func TestLoginCookieCSRFAndProjects(t *testing.T) {
	h := testHandler(t)
	page := call(h, "GET", "/login", nil)
	if page.Code != 200 {
		t.Fatalf("login GET=%d", page.Code)
	}
	first := page.Result().Cookies()[0]
	if !first.Secure || !first.HttpOnly || first.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie insegura")
	}
	ok := call(h, "POST", "/login", url.Values{"pin": {"TEST_LONG_ACCESS_PIN"}, "csrf": {first.Value}}, first)
	if ok.Code != http.StatusSeeOther {
		t.Fatalf("login POST=%d: %s", ok.Code, ok.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range ok.Result().Cookies() {
		if c.Name == "__Host-kagmcp-session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.Secure || !sessionCookie.HttpOnly {
		t.Fatal("sesión insegura")
	}
	csrf := h.sessions[key(sessionCookie.Value)].CSRF
	invalid := call(h, "POST", "/project", url.Values{"csrf": {"falso"}, "id": {"alpha"}, "name": {"nombre"}}, sessionCookie)
	if invalid.Code != 403 {
		t.Fatalf("CSRF no rechazado=%d", invalid.Code)
	}
	valid := call(h, "POST", "/project", url.Values{"csrf": {csrf}, "id": {"alpha"}, "name": {"Proyecto <script>alert(1)</script>"}}, sessionCookie)
	if valid.Code != 303 {
		t.Fatalf("crear proyecto: %d, %s", valid.Code, valid.Body.String())
	}
	home := call(h, "GET", "/projects", nil, sessionCookie)
	if home.Code != 200 || !strings.Contains(home.Body.String(), "Proyecto") || strings.Contains(home.Body.String(), "<script>alert(1)</script>") {
		t.Fatalf("panel inválido: %s", home.Body.String()[:min(home.Body.Len(), 400)])
	}
	logout := call(h, "POST", "/logout", url.Values{"csrf": {csrf}}, sessionCookie)
	if logout.Code != 303 {
		t.Fatalf("logout %d", logout.Code)
	}
	denied := call(h, "GET", "/", nil, sessionCookie)
	if denied.Code != 303 {
		t.Fatal("sesión no revocada")
	}
}
func TestLoginRateLimits(t *testing.T) {
	h := testHandler(t)
	page := call(h, "GET", "/login", nil)
	c := page.Result().Cookies()[0]
	for i := 0; i < 5; i++ {
		wrong := call(h, "POST", "/login", url.Values{"pin": {"INVALID"}, "csrf": {c.Value}}, c)
		if wrong.Code != 401 {
			t.Fatalf("intento %d: %d", i, wrong.Code)
		}
	}
	blocked := call(h, "POST", "/login", url.Values{"pin": {"TEST_LONG_ACCESS_PIN"}, "csrf": {c.Value}}, c)
	if blocked.Code != 429 {
		t.Fatalf("rate limit inefectivo: %d", blocked.Code)
	}
}

func TestImportRequiresSessionAndCSRF(t *testing.T) {
	h := testHandler(t)
	called := false
	h.Import = func(id string) (any, error) { called = id == "alpha"; return map[string]bool{"imported": true}, nil }
	anonymous := call(h, "POST", "/import", url.Values{"project": {"alpha"}, "csrf": {"x"}})
	if anonymous.Code != 401 || called {
		t.Fatalf("acceso no autorizado: %d", anonymous.Code)
	}
	page := call(h, "GET", "/login", nil)
	c := page.Result().Cookies()[0]
	authenticated := call(h, "POST", "/login", url.Values{"csrf": {c.Value}, "pin": {"TEST_LONG_ACCESS_PIN"}}, c)
	var session *http.Cookie
	for _, item := range authenticated.Result().Cookies() {
		if item.Name == "__Host-kagmcp-session" {
			session = item
		}
	}
	if session == nil {
		t.Fatal("faltó sesión")
	}
	invalid := call(h, "POST", "/import", url.Values{"project": {"alpha"}, "csrf": {"incorrecto"}}, session)
	if invalid.Code != 403 || called {
		t.Fatal("importación sin CSRF permitida")
	}
	csrf := h.sessions[key(session.Value)].CSRF
	valid := call(h, "POST", "/import", url.Values{"project": {"alpha"}, "csrf": {csrf}}, session)
	if valid.Code != 303 || !called {
		t.Fatalf("importación autenticada: %d, called=%v", valid.Code, called)
	}
}

func TestChangePINRequiresCurrentAndRevokesAllSessions(t *testing.T) {
	h := testHandler(t)
	revoked := 0
	h.revokePIN = func() error { revoked++; return nil }
	page := call(h, "GET", "/login", nil)
	csrfCookie := page.Result().Cookies()[0]
	signedIn := call(h, "POST", "/login", url.Values{"pin": {"TEST_LONG_ACCESS_PIN"}, "csrf": {csrfCookie.Value}}, csrfCookie)
	if signedIn.Code != 303 {
		t.Fatalf("login HTTP %d", signedIn.Code)
	}
	var sessionCookie *http.Cookie
	for _, c := range signedIn.Result().Cookies() {
		if c.Name == "__Host-kagmcp-session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no hay sesión")
	}
	csrf := h.sessions[key(sessionCookie.Value)].CSRF
	denied := call(h, "POST", "/change-pin", url.Values{"csrf": {csrf}, "current_pin": {"equivocado"}, "new_pin": {"654321"}}, sessionCookie)
	if denied.Code != 400 || revoked != 0 {
		t.Fatalf("PIN actual incorrecto pudo rotar: %d", denied.Code)
	}
	tooShort := call(h, "POST", "/change-pin", url.Values{"csrf": {csrf}, "current_pin": {"TEST_LONG_ACCESS_PIN"}, "new_pin": {"12345"}}, sessionCookie)
	if tooShort.Code != 400 || revoked != 0 {
		t.Fatalf("aceptó PIN menor de seis: %d", tooShort.Code)
	}
	changed := call(h, "POST", "/change-pin", url.Values{"csrf": {csrf}, "current_pin": {"TEST_LONG_ACCESS_PIN"}, "new_pin": {"654321"}}, sessionCookie)
	if changed.Code != 303 || revoked != 1 {
		t.Fatalf("rotación fallida: %d revocations %d", changed.Code, revoked)
	}
	if h.pin.Verify("TEST_LONG_ACCESS_PIN") || !h.pin.Verify("654321") {
		t.Fatal("nuevo PIN no aplicado o anterior aún válido")
	}
	if home := call(h, "GET", "/", nil, sessionCookie); home.Code != 303 {
		t.Fatal("sesión web existente no revocada")
	}
	newPage := call(h, "GET", "/login", nil)
	newCSRF := newPage.Result().Cookies()[0]
	loginNew := call(h, "POST", "/login", url.Values{"pin": {"654321"}, "csrf": {newCSRF.Value}}, newCSRF)
	if loginNew.Code != 303 {
		t.Fatalf("nuevo PIN no permite entrar: %d", loginNew.Code)
	}
}

func TestGradioLoginWithProxyModifiedOrMissingOrigin(t *testing.T) {
	for _, origin := range []string{"", "null", "https://gradio.live", "https://proxy.gradio.live", "https://evil.example"} {
		t.Run("origin_"+origin, func(t *testing.T) {
			h := testHandler(t)
			page := call(h, "GET", "/login", nil)
			if page.Code != 200 {
				t.Fatalf("GET login: %d", page.Code)
			}
			c := page.Result().Cookies()[0]
			values := url.Values{"pin": {"TEST_LONG_ACCESS_PIN"}, "csrf": {c.Value}}
			req := httptest.NewRequest("POST", "/login", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			req.AddCookie(c)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 303 {
				t.Fatalf("login con Origin=%q: %d body=%s", origin, rec.Code, rec.Body.String())
			}
		})
	}
}
func TestProxyCannotBypassCSRF(t *testing.T) {
	for _, tc := range []struct {
		name, origin string
		cookie, csrf bool
	}{
		{"missing_origin_and_missing_cookie", "", false, true},
		{"foreign_origin_and_missing_cookie", "https://evil.example", false, true},
		{"foreign_origin_wrong_csrf", "https://evil.example", true, false},
		{"missing_origin_wrong_csrf", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testHandler(t)
			get := call(h, "GET", "/login", nil)
			c := get.Result().Cookies()[0]
			csrf := "invalid"
			if tc.csrf {
				csrf = c.Value
			}
			payload := url.Values{"pin": {"TEST_LONG_ACCESS_PIN"}, "csrf": {csrf}}
			req := httptest.NewRequest("POST", "/login", strings.NewReader(payload.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.cookie {
				req.AddCookie(c)
			}
			out := httptest.NewRecorder()
			h.ServeHTTP(out, req)
			if out.Code != 403 {
				t.Fatalf("CSRF omitido aceptado, status=%d", out.Code)
			}
			if !strings.Contains(out.Body.String(), "cookie de seguridad") {
				t.Fatal("error no explica la causa")
			}
		})
	}
}
func TestGradioAuthenticatedActionWithMissingOrigin(t *testing.T) {
	h := testHandler(t)
	get := call(h, "GET", "/login", nil)
	cookie := get.Result().Cookies()[0]
	auth := call(h, "POST", "/login", url.Values{"csrf": {cookie.Value}, "pin": {"TEST_LONG_ACCESS_PIN"}}, cookie)
	var sessionCookie *http.Cookie
	for _, c := range auth.Result().Cookies() {
		if c.Name == "__Host-kagmcp-session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no hay sesión")
	}
	csrf := h.sessions[key(sessionCookie.Value)].CSRF
	values := url.Values{"csrf": {csrf}, "id": {"gradio"}, "name": {"Prueba Gradio"}}
	req := httptest.NewRequest("POST", "/project", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 303 {
		t.Fatalf("acción autenticada sin Origin HTTP%d: %s", out.Code, out.Body.String())
	}
	// La misma sesión no puede usar un token CSRF inventado.
	values.Set("csrf", "incorrecto")
	req2 := httptest.NewRequest("POST", "/project", strings.NewReader(values.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(sessionCookie)
	denied := httptest.NewRecorder()
	h.ServeHTTP(denied, req2)
	if denied.Code != 403 {
		t.Fatalf("acción sin CSRF aceptada: %d", denied.Code)
	}
}
func TestLoginInvalidFormGivesHelpfulExplanation(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest("POST", "/login", strings.NewReader(strings.Repeat("x", 5000)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 400 || !strings.Contains(out.Body.String(), "No se pudo leer el formulario") {
		t.Fatalf("no explicó error formulario: %d %s", out.Code, out.Body.String()[:min(out.Body.Len(), 350)])
	}
}
