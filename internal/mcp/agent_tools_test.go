package mcp

import (
	"context"
	"encoding/json"
	"github.com/ThowiLabs/kagssh-go/internal/agenttasks"
	"strings"
	"testing"
)

func TestAgentToolRegisteredAndRequiresConsent(t *testing.T) {
	s := newTestServer(t)
	for _, required := range []string{"agent_task_create", "agent_task_list", "agent_task_status", "agent_task_open", "agent_task_submit", "agent_task_prompt", "agent_task_review"} {
		if !agentTool(required) {
			t.Fatalf("MCP no registró %s", required)
		}
	}
	var found bool
	for _, value := range s.definitions() {
		d := value.(map[string]any)
		if d["name"] == "agent_task_create" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("MCP discovery sin creación de agente")
	}
	payload := map[string]any{
		"title": "API delegada", "objective": "Implementar endpoint", "mcp": "$pl0",
		"mcp_id":    "app://asdk_app_6acbc35eca0c81919f721ddc305e49ad",
		"workspace": "/kaggle/working/app", "branch": "agent/TASK-api",
		"allowed_paths": []string{"internal/api", "internal/test"}, "criteria": "go test ./... pasa",
		"context": "Proyecto Go y mux", "user_approved": false,
	}
	raw, _ := json.Marshal(payload)
	if _, err := s.invokeAgent(context.Background(), "agent_task_create", raw); err == nil {
		t.Fatal("tool creó delegación sin permiso")
	}
	payload["user_approved"] = true
	raw, _ = json.Marshal(payload)
	result, err := s.invokeAgent(context.Background(), "agent_task_create", raw)
	if err != nil {
		t.Fatal(err)
	}
	data := result.(map[string]any)
	if !strings.Contains(data["prompt"].(string), "$pl0") {
		t.Fatal("prompt sin MCP destino")
	}
	task := data["task"].(agenttasks.Task)
	status, err := s.invokeAgent(context.Background(), "agent_task_status", json.RawMessage(`{"id":"`+task.ID+`"}`))
	if err != nil || status.(agenttasks.Task).Status != "pending" {
		t.Fatalf("estado inválido %v", err)
	}
	open, err := s.invokeAgent(context.Background(), "agent_task_open", json.RawMessage(`{"id":"`+task.ID+`","agent":"backend"}`))
	if err != nil || open == nil {
		t.Fatal(err)
	}
	if _, err = s.invokeAgent(context.Background(), "agent_task_open", json.RawMessage(`{"id":"`+task.ID+`","agent":"another"}`)); err == nil {
		t.Fatal("doble reclamo")
	}
	submit, err := s.invokeAgent(context.Background(), "agent_task_submit", json.RawMessage(`{"id":"`+task.ID+`","agent":"backend","status":"needs_review","commit":"abc123","tests":"go test PASS","summary":"API terminada"}`))
	if err != nil || submit.(agenttasks.Task).Status != "needs_review" {
		t.Fatal(err)
	}
	check, err := s.invokeAgent(context.Background(), "agent_task_review", json.RawMessage(`{"id":"`+task.ID+`"}`))
	if err != nil || check.(map[string]any)["owner_approval_required"] != true {
		t.Fatal("revisión solo lectura no disponible")
	}
	if s.agents.List()[0].Status != "needs_review" {
		t.Fatal("revisión modificó estado")
	}
}
