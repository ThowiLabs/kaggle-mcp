package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ThowiLabs/kagssh-go/internal/agenttasks"
)

func agentTool(name string) bool {
	switch name {
	case "agent_task_create", "agent_task_list", "agent_task_status", "agent_task_prompt", "agent_task_open", "agent_task_submit", "agent_task_review":
		return true
	}
	return false
}
func (s *Server) invokeAgent(_ context.Context, name string, raw json.RawMessage) (any, error) {
	if s.agents == nil {
		return nil, errors.New("orquestador no disponible")
	}
	var a struct {
		ID      string `json:"id"`
		Agent   string `json:"agent"`
		Status  string `json:"status"`
		Commit  string `json:"commit"`
		Tests   string `json:"tests"`
		Summary string `json:"summary"`
		Blocker string `json:"blocker"`
		agenttasks.Spec
	}
	if len(raw) > 0 && string(raw) != "null" {
		if len(raw) > 16<<10 {
			return nil, errors.New("argumentos excesivos")
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
	}
	switch name {
	case "agent_task_create":
		task, err := s.agents.Create(a.Spec)
		if err != nil {
			return nil, err
		}
		return map[string]any{"task": task, "prompt": agenttasks.Prompt(task), "notice": "Copia el prompt a otro chat manualmente. No se ha iniciado ningún chat ni otorgado permisos sobre el MCP objetivo."}, nil
	case "agent_task_list":
		return s.agents.List(), nil
	case "agent_task_status":
		return s.agents.Get(a.ID)
	case "agent_task_review":
		t, err := s.agents.Get(a.ID)
		if err != nil {
			return nil, err
		}
		if t.Status != "needs_review" {
			return nil, errors.New("la tarea aún no tiene entrega pendiente de revisión")
		}
		return map[string]any{
			"task": t,
			"review_checklist": []string{
				"Comprueba que estás conectado al MCP destino " + t.MCP + " y ruta " + t.Workspace,
				"Comprueba que el commit " + t.Commit + " es real y pertenece a la rama " + t.Branch,
				"Inspecciona diff, rutas permitidas y efectos de seguridad.",
				"Reproduce pruebas, valida criterios de aceptación y documenta riesgos.",
				"Solicita decisión del usuario; la aprobación/rechazo sólo se registra en /agents con PIN.",
			},
			"owner_approval_required": true,
		}, nil
	case "agent_task_prompt":
		t, err := s.agents.Get(a.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": t.ID, "prompt": agenttasks.Prompt(t)}, nil
	case "agent_task_open":
		t, err := s.agents.Claim(a.ID, a.Agent)
		if err != nil {
			return nil, err
		}
		return map[string]any{"task": t, "prompt": agenttasks.Prompt(t)}, nil
	case "agent_task_submit":
		return s.agents.Submit(a.ID, a.Agent, a.Status, a.Commit, a.Tests, a.Summary, a.Blocker)
	}
	return nil, errors.New("herramienta no reconocida")
}
