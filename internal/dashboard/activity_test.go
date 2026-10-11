package dashboard

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/ThowiLabs/kagssh-go/internal/projectstate"
)

func TestActivityLiveAndPrivatePagination(t *testing.T) {
	h := testHandler(t)
	for _, path := range []string{"/api/activity", "/api/activity?page=2", "/activity?page=1"} {
		if r := call(h, "GET", path, nil); r.Code != 303 {
			t.Fatalf("ruta %s sin login: %d", path, r.Code)
		}
	}
	signin := call(h, "GET", "/login", nil)
	loginCookie := signin.Result().Cookies()[0]
	login := call(h, "POST", "/login", url.Values{"pin": {"TEST_LONG_ACCESS_PIN"}, "csrf": {loginCookie.Value}}, loginCookie)
	var session *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == "__Host-kagmcp-session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("sesión no creada")
	}
	for _, path := range []string{"/api/activity?page=-1", "/api/activity?page=0", "/api/activity?page=foobar", "/api/activity?page=123456789012345"} {
		if r := call(h, "GET", path, nil, session); r.Code != 400 {
			t.Fatalf("página malformada %s: %d", path, r.Code)
		}
	}
	for i := 0; i < 43; i++ {
		if err := h.Projects.Record("p", "Evento "+strconv.Itoa(i), "echo "+strconv.Itoa(i), "ok"); err != nil {
			t.Fatal(err)
		}
	}
	get := func(path string) projectstate.HistoryPage {
		t.Helper()
		r := call(h, "GET", path, nil, session)
		if r.Code != 200 || !strings.Contains(r.Header().Get("Content-Type"), "application/json") || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("api %s: HTTP %d", path, r.Code)
		}
		var page projectstate.HistoryPage
		if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	first := get("/api/activity")
	second := get("/api/activity?page=2")
	third := get("/api/activity?page=3")
	if first.Total != 43 || first.TotalPages != 3 || len(first.Items) != 20 || first.Items[0].Description != "Evento 42" {
		t.Fatal("página 1 no empieza por el más reciente")
	}
	if len(second.Items) != 20 || second.Items[0].Description != "Evento 22" {
		t.Fatal("página 2 inválida")
	}
	if len(third.Items) != 3 || third.Items[0].Description != "Evento 2" {
		t.Fatal("página 3 inválida")
	}
	ssr := call(h, "GET", "/activity?page=2", nil, session)
	if ssr.Code != 200 || !strings.Contains(ssr.Body.String(), "Evento 22") || strings.Contains(ssr.Body.String(), "Evento 42") || !strings.Contains(ssr.Body.String(), "activityPagination") {
		t.Fatal("SSR no pagina correctamente")
	}
	if err := h.Projects.Record("p", "Evento 43", "echo 43", "ok"); err != nil {
		t.Fatal(err)
	}
	newer := get("/api/activity?page=1")
	if newer.Total != 44 || newer.Items[0].Description != "Evento 43" {
		t.Fatal("API no refleja el nuevo evento")
	}
	if err := h.Projects.Record("p", "<img src=x onerror=alert(1)>", "echo TEST", "ok"); err != nil {
		t.Fatal(err)
	}
	safePage := call(h, "GET", "/activity", nil, session)
	if safePage.Code != 200 || strings.Contains(safePage.Body.String(), "<img src=x") || !strings.Contains(safePage.Body.String(), "&lt;img") {
		t.Fatal("XSS en el historial renderizado")
	}
	if call(h, "GET", "/activity?page=bad", nil, session).Code != 400 {
		t.Fatal("página HTML malformada aceptada")
	}
	csrf := h.sessions[key(session.Value)].CSRF
	if call(h, "POST", "/api/activity", url.Values{"csrf": {csrf}}, session).Code != 405 {
		t.Fatal("endpoint de sólo lectura acepta POST")
	}
	asset := call(h, "GET", "/assets/activity.js", nil)
	if asset.Code != 200 || !strings.Contains(asset.Body.String(), "setInterval(refresh, 5000)") {
		t.Fatal("cliente live no disponible")
	}
}
