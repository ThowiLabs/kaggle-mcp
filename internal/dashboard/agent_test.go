package dashboard

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThowiLabs/kaggle-mcp/internal/agenttasks"
)

func TestAgentOrchestratorPanelConsentAndReview(t *testing.T) {
	h := testHandler(t)
	agents, err := agenttasks.Open(filepath.Join(t.TempDir(), "agent-tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.Agents = agents
	if r := call(h, "GET", "/agents", nil); r.Code != 303 {
		t.Fatalf("acceso sin sesión HTTP %d", r.Code)
	}
	initial := call(h, "GET", "/login", nil)
	c := initial.Result().Cookies()[0]
	signed := call(h, "POST", "/login", url.Values{"csrf": {c.Value}, "pin": {"TEST_LONG_ACCESS_PIN"}}, c)
	var session *http.Cookie
	for _, v := range signed.Result().Cookies() {
		if v.Name == "__Host-kagmcp-session" {
			session = v
		}
	}
	if session == nil {
		t.Fatal("sesión no emitida")
	}
	csrf := h.sessions[key(session.Value)].CSRF
	page := call(h, "GET", "/agents", nil, session)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "¿Quieres utilizar un subagente") || !strings.Contains(page.Body.String(), "Sí, preparar delegación") {
		t.Fatal("no se solicitó consentimiento")
	}
	form := url.Values{
		"csrf": {csrf}, "title": {"Backend API"}, "mcp": {"$pl0"}, "mcp_id": {"app://asdk_app_6acbc35eca0c81919f721ddc305e49ad"},
		"workspace": {"/kaggle/working/proyecto"}, "branch": {"agent/backend"}, "allowed_paths": {"internal/api internal/db"},
		"objective": {"Implementar API"}, "criteria": {"go test ./..."}, "context": {"Go y net/http"},
	}
	if r := call(h, "POST", "/agent-create", form, session); r.Code != 400 {
		t.Fatalf("delegación sin aprobación %d", r.Code)
	}
	if len(agents.List()) != 0 {
		t.Fatal("creó tarea sin permiso")
	}
	form.Set("user_approved", "yes")
	if r := call(h, "POST", "/agent-create", form, session); r.Code != 303 {
		t.Fatalf("crear tarea: %d, %s", r.Code, r.Body.String())
	}
	records := agents.List()
	if len(records) != 1 {
		t.Fatal("falta tarea")
	}
	task := records[0]
	view := call(h, "GET", "/agents?created="+task.ID, nil, session)
	if view.Code != 200 || !strings.Contains(view.Body.String(), task.ID) || !strings.Contains(view.Body.String(), "agent_task_submit") {
		t.Fatal("no presenta prompt")
	}
	if r := call(h, "POST", "/agent-review", url.Values{"csrf": {csrf}, "id": {task.ID}, "decision": {"approved"}, "note": {"ok"}}, session); r.Code != 400 {
		t.Fatal("aprobó sin entrega")
	}
	if _, err := agents.Claim(task.ID, "backend"); err != nil {
		t.Fatal(err)
	}
	if _, err := agents.Submit(task.ID, "backend", "needs_review", "abc12", "go test:PASS", "done", ""); err != nil {
		t.Fatal(err)
	}
	if r := call(h, "POST", "/agent-review", url.Values{"csrf": {"incorrecto"}, "id": {task.ID}, "decision": {"approved"}, "note": {"ok"}}, session); r.Code != 403 {
		t.Fatal("sin CSRF aceptado")
	}
	if r := call(h, "POST", "/agent-review", url.Values{"csrf": {csrf}, "id": {task.ID}, "decision": {"approved"}, "note": {"Revisado el SHA"}}, session); r.Code != 303 {
		t.Fatalf("rechazó aprobación legítima: %d", r.Code)
	}
	approved, err := agents.Get(task.ID)
	if err != nil || approved.Status != "approved" {
		t.Fatal("estado final no aprobado")
	}
}
