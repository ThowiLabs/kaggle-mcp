package dashboard

import (
	"strings"
	"testing"

	"github.com/ThowiLabs/kaggle-mcp/internal/sshtunnel"
)

// TestIntegrationsGridAndModals verifica la reorganización de Integraciones
// (tarea 37): rejilla de tarjetas, una por integración, y cada tarjeta abre
// su propia ventana con el formulario de ESA integración dentro. Los
// endpoints y los campos no cambian.
func TestIntegrationsGridAndModals(t *testing.T) {
	h := testHandler(t)
	h.SSHStatus = func() sshtunnel.Status { return sshtunnel.Status{} }
	session, _ := loginSession(t, h)
	page := call(h, "GET", "/integrations", nil, session)
	if page.Code != 200 {
		t.Fatalf("integrations GET=%d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{
		`class="integration-grid"`,
		`data-open-modal="modal-github"`,
		`data-open-modal="modal-huggingface"`,
		`data-open-modal="modal-ssh"`,
		`id="modal-github"`,
		`id="modal-huggingface"`,
		`id="modal-ssh"`,
		`action="/github"`,
		`action="/huggingface"`,
		`action="/ssh-tunnel"`,
		"Túnel SSH inverso",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("integrations sin %q", want)
		}
	}
	// Cada formulario aparece una sola vez: dentro de su ventana.
	for _, action := range []string{`action="/github"`, `action="/huggingface"`, `action="/ssh-tunnel"`} {
		if strings.Count(body, action) != 1 {
			t.Fatalf("%s aparece %d veces, se esperaba 1", action, strings.Count(body, action))
		}
	}
}

func TestSecurityModal(t *testing.T) {
	h := testHandler(t)
	session, _ := loginSession(t, h)
	page := call(h, "GET", "/security", nil, session)
	if page.Code != 200 {
		t.Fatalf("security GET=%d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{
		`data-open-modal="modal-security"`,
		`id="modal-security"`,
		`action="/change-pin"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("security sin %q", want)
		}
	}
}

func TestProjectsModal(t *testing.T) {
	h := testHandler(t)
	session, _ := loginSession(t, h)
	page := call(h, "GET", "/projects", nil, session)
	if page.Code != 200 {
		t.Fatalf("projects GET=%d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{
		`data-open-modal="modal-project"`,
		`id="modal-project"`,
		`action="/project"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("projects sin %q", want)
		}
	}
}
