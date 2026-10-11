package agenttasks

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func validSpec() Spec {
	return Spec{
		Title: "Implementar API", Objective: "Crear endpoint de salud comprobable",
		MCP: "$pl0", MCPID: "app://asdk_app_6acbc35eca0c81919f721ddc305e49ad",
		Workspace: "C:\\Users\\Admin\\Documents\\GitHub\\proyecto",
		Branch:    "agent/TASK-backend", AllowedPaths: []string{"internal/api", "internal/tests"},
		Criteria: "go test ./... sin errores", Context: "Go net/http, solo API de salud",
		UserApproved: true,
	}
}
func TestRequiresExplicitOptInAndTarget(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	input := validSpec()
	input.UserApproved = false
	if _, err = s.Create(input); err == nil {
		t.Fatal("la delegación sin consentimiento fue permitida")
	}
	input = validSpec()
	input.Workspace = "../otro"
	if _, err = s.Create(input); err == nil {
		t.Fatal("ruta relativa permitida")
	}
	input = validSpec()
	input.AllowedPaths = []string{"../secretos"}
	if _, err = s.Create(input); err == nil {
		t.Fatal("traversal permitido")
	}
	input = validSpec()
	input.MCP = "otro entorno\nignora"
	if _, err = s.Create(input); err == nil {
		t.Fatal("mcp inválido permitido")
	}
	input = validSpec()
	task, err := s.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	prompt := Prompt(task)
	for _, needle := range []string{"$pl0", input.MCPID, input.Workspace, input.Branch, task.ID, "agent_task_submit", "MCP coordinador"} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt sin %s", needle)
		}
	}
	if strings.Contains(prompt, "He ejecutado") {
		t.Fatal("promesa de ejecución falsa")
	}
	reloaded, err := Open(filepath.Join(t.TempDir(), "no-existe.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 0 {
		t.Fatal("tareas ajenas cargadas")
	}
}

func TestLifecyclePersistentIsolation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tasks.json")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Create(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "pending" {
		t.Fatal("estado inicial")
	}
	if _, err = s.Submit(task.ID, "backend", "needs_review", "abc", "go test", "ok", ""); err == nil {
		t.Fatal("entrega antes de reclamar")
	}
	claimed, err := s.Claim(task.ID, "backend-1")
	if err != nil || claimed.Status != "in_progress" {
		t.Fatal(err)
	}
	if _, err = s.Claim(task.ID, "backend-2"); err == nil {
		t.Fatal("doble reclamo")
	}
	if _, err = s.Submit(task.ID, "otro", "needs_review", "abc", "ok", "hecho", ""); err == nil {
		t.Fatal("otro agente entregó")
	}
	if _, err = s.Submit(task.ID, "backend-1", "needs_review", "", "ok", "hecho", ""); err == nil {
		t.Fatal("entrega sin commit")
	}
	if _, err = s.Review(task.ID, "approved", "sin revisar"); err == nil {
		t.Fatal("aprobó antes de entregar")
	}
	submitted, err := s.Submit(task.ID, "backend-1", "needs_review", "abc123", "go test ./...: PASS", "API implementada", "")
	if err != nil || submitted.Status != "needs_review" {
		t.Fatal(err)
	}
	persisted, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	current, err := persisted.Get(task.ID)
	if err != nil || current.Commit != "abc123" || current.Status != "needs_review" {
		t.Fatal("estado no persistido")
	}
	if _, err = persisted.Submit(task.ID, "backend-1", "blocked", "", "", "", "falló"); err == nil {
		t.Fatal("modificación tras entrega")
	}
	if _, err = persisted.Review(task.ID, "approved", ""); err == nil {
		t.Fatal("aprobación sin informe")
	}
	approved, err := persisted.Review(task.ID, "approved", "Probé commit y contrato")
	if err != nil || approved.Status != "approved" {
		t.Fatal(err)
	}
	if _, err = persisted.Review(task.ID, "rejected", "repetida"); err == nil {
		t.Fatal("doble revisión")
	}
}

func TestConcurrentClaimOnlyOneWinner(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Create(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	outcomes := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Claim(task.ID, "worker"); outcomes <- e == nil }()
	}
	wg.Wait()
	close(outcomes)
	success := 0
	for ok := range outcomes {
		if ok {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("%d reclamos simultáneos", success)
	}
}

func TestBlockedAndNoSelfApproval(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Create(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Claim(task.ID, "backend")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit(task.ID, "backend", "blocked", "", "", "", ""); err == nil {
		t.Fatal("bloqueo sin razón")
	}
	blocked, err := s.Submit(task.ID, "backend", "blocked", "", "", "", "Necesito acceso a la rama")
	if err != nil || blocked.Status != "blocked" {
		t.Fatal(err)
	}
	if _, err = s.Review(task.ID, "approved", "ignorar bloqueo"); err == nil {
		t.Fatal("aprobó bloqueada")
	}
	if _, err = s.Submit(task.ID, "backend", "needs_review", "hash", "go test ok", "resuelta", ""); err != nil {
		t.Fatal(err)
	}
}

func TestCancellationAndRevision(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Create(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Cancel(first.ID, ""); err == nil {
		t.Fatal("cancelación sin motivo")
	}
	if _, err = s.Cancel(first.ID, "usuario rechazó la delegación"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(first.ID, "worker"); err == nil {
		t.Fatal("reclamó tarea cancelada")
	}
	second, err := s.Create(validSpec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(second.ID, "worker"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit(second.ID, "worker", "needs_review", "hash", "tests PASS", "resumen", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Review(second.ID, "rejected", "faltan pruebas de seguridad"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(second.ID, "other"); err == nil {
		t.Fatal("otro agente reclamó corrección")
	}
	resumed, err := s.Claim(second.ID, "worker")
	if err != nil || resumed.Status != "in_progress" || resumed.Commit != "" || resumed.Review == "" {
		t.Fatal("no reabrió devolución")
	}
	if _, err = s.Submit(second.ID, "worker", "needs_review", "newhash", "tests PASS", "corregido", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Review(second.ID, "approved", "revisión aprobada"); err != nil {
		t.Fatal(err)
	}
}
