package dashboard

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ThowiLabs/kaggle-mcp/internal/sshtunnel"
)

func loginSession(t *testing.T, h *Handler) (*http.Cookie, string) {
	t.Helper()
	page := call(h, "GET", "/login", nil)
	if page.Code != 200 {
		t.Fatalf("login GET=%d", page.Code)
	}
	cookie := page.Result().Cookies()[0]
	signedIn := call(h, "POST", "/login", url.Values{"pin": {"TEST_LONG_ACCESS_PIN"}, "csrf": {cookie.Value}}, cookie)
	if signedIn.Code != http.StatusSeeOther {
		t.Fatalf("login POST=%d", signedIn.Code)
	}
	var session *http.Cookie
	for _, c := range signedIn.Result().Cookies() {
		if c.Name == "__Host-kagmcp-session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no hay sesión")
	}
	return session, h.sessions[key(session.Value)].CSRF
}

func TestSSHTunnelRoutesRequireSession(t *testing.T) {
	h := testHandler(t)
	rec := call(h, "POST", "/ssh-tunnel", url.Values{"csrf": {"x"}})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("sin sesión: %d", rec.Code)
	}
}

func TestSSHTunnelSaveTestStop(t *testing.T) {
	h := testHandler(t)
	var saved sshtunnel.Status
	h.SSHStatus = func() sshtunnel.Status { return saved }
	h.SSHConfigure = func(pin, user, host string, sshPort, remotePort int, password string, enabled bool) (sshtunnel.Status, error) {
		if pin != "TEST_LONG_ACCESS_PIN" {
			return sshtunnel.Status{}, errors.New("PIN incorrecto")
		}
		saved = sshtunnel.Status{Configured: true, Enabled: enabled, User: user, Host: host, SSHPort: sshPort, RemotePort: remotePort}
		return saved, nil
	}
	h.SSHTest = func(_ context.Context, _, _ string, _, _ int, _ string) (int, error) { return 22222, nil }
	stopped := false
	h.SSHStop = func() (sshtunnel.Status, error) {
		stopped = true
		saved.Enabled = false
		return saved, nil
	}
	session, csrf := loginSession(t, h)

	page := call(h, "GET", "/integrations", nil, session)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Túnel SSH inverso") {
		t.Fatalf("integrations sin sección SSH: %d", page.Code)
	}
	values := url.Values{
		"csrf": {csrf}, "pin": {"TEST_LONG_ACCESS_PIN"}, "user": {"usuario"},
		"host": {"servidor.example.com"}, "ssh_port": {"2222"}, "remote_port": {""},
		"password": {"secreta"}, "enabled": {"yes"},
	}
	rec := call(h, "POST", "/ssh-tunnel", values, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("guardar: %d %s", rec.Code, rec.Body.String())
	}
	if !saved.Configured || saved.SSHPort != 2222 || saved.RemotePort != 0 {
		t.Fatalf("guardado incorrecto: %+v", saved)
	}
	denied := call(h, "POST", "/ssh-tunnel-stop", url.Values{"csrf": {csrf}}, session)
	if denied.Code != http.StatusBadRequest || stopped {
		t.Fatalf("detener sin confirmar: %d stopped=%v", denied.Code, stopped)
	}
	ok := call(h, "POST", "/ssh-tunnel-stop", url.Values{"csrf": {csrf}, "confirm": {"yes"}}, session)
	if ok.Code != http.StatusSeeOther || !stopped {
		t.Fatalf("detener: %d stopped=%v", ok.Code, stopped)
	}
	tested := call(h, "POST", "/ssh-tunnel-test", values, session)
	if tested.Code != http.StatusSeeOther || !strings.Contains(tested.Header().Get("Location"), "ssh_test=ok&port=22222") {
		t.Fatalf("probar: %d Location=%q", tested.Code, tested.Header().Get("Location"))
	}
	after := call(h, "GET", "/integrations?ssh_test=ok&port=22222", nil, session)
	if after.Code != 200 || !strings.Contains(after.Body.String(), "22222") {
		t.Fatalf("resultado de prueba no visible: %d", after.Code)
	}
}

func TestSSHTunnelSaveRejectsWrongPIN(t *testing.T) {
	h := testHandler(t)
	h.SSHStatus = func() sshtunnel.Status { return sshtunnel.Status{} }
	h.SSHConfigure = func(pin, user, host string, sshPort, remotePort int, password string, enabled bool) (sshtunnel.Status, error) {
		return sshtunnel.Status{}, errors.New("PIN incorrecto")
	}
	session, csrf := loginSession(t, h)
	rec := call(h, "POST", "/ssh-tunnel", url.Values{
		"csrf": {csrf}, "pin": {"equivocado"}, "user": {"usuario"},
		"host": {"servidor.example.com"}, "ssh_port": {"22"},
		"password": {"secreta"},
	}, session)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "PIN incorrecto") {
		t.Fatalf("PIN incorrecto aceptado: %d %s", rec.Code, rec.Body.String())
	}
}
