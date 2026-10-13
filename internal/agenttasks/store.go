package agenttasks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxFile = 2 << 20
const maxTasks = 150

var ident = regexp.MustCompile(`^[a-zA-Z0-9_./:@$-]{1,200}$`)
var branch = regexp.MustCompile(`^[a-zA-Z0-9_./-]{1,160}$`)

type Task struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Objective    string    `json:"objective"`
	MCP          string    `json:"mcp"`
	MCPID        string    `json:"mcp_id,omitempty"`
	Workspace    string    `json:"workspace"`
	Branch       string    `json:"branch"`
	AllowedPaths []string  `json:"allowed_paths"`
	Criteria     string    `json:"criteria"`
	Context      string    `json:"context"`
	Status       string    `json:"status"`
	Agent        string    `json:"agent,omitempty"`
	Commit       string    `json:"commit,omitempty"`
	Tests        string    `json:"tests,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	Blocker      string    `json:"blocker,omitempty"`
	Review       string    `json:"review,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
type Spec struct {
	Title        string   `json:"title"`
	Objective    string   `json:"objective"`
	MCP          string   `json:"mcp"`
	MCPID        string   `json:"mcp_id"`
	Workspace    string   `json:"workspace"`
	Branch       string   `json:"branch"`
	AllowedPaths []string `json:"allowed_paths"`
	Criteria     string   `json:"criteria"`
	Context      string   `json:"context"`
	UserApproved bool     `json:"user_approved"`
}
type Store struct {
	mu    sync.Mutex
	file  string
	tasks []Task
}

func Open(path string) (*Store, error) {
	s := &Store{file: path, tasks: []Task{}}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(content) > maxFile {
		return nil, errors.New("estado de agentes demasiado grande")
	}
	if err := json.Unmarshal(content, &s.tasks); err != nil {
		return nil, fmt.Errorf("estado de agentes inválido: %w", err)
	}
	if len(s.tasks) > maxTasks {
		return nil, errors.New("demasiadas tareas almacenadas")
	}
	return s, nil
}
func cleanText(s string, limit int) bool {
	return len(strings.TrimSpace(s)) > 0 && len(s) <= limit && !strings.ContainsRune(s, 0)
}
func validWorkspace(path string) bool {
	if path == "" || len(path) > 300 || strings.ContainsAny(path, "\r\n\x00") {
		return false
	}
	p := strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(p, "/") && !(len(p) > 2 && p[1] == ':' && p[2] == '/' && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z'))) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." || segment == "." {
			return false
		}
	}
	return true
}
func validRelative(p string) bool {
	if len(p) == 0 || len(p) > 180 || strings.ContainsAny(p, "\\\r\n\x00") || strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func validate(s Spec) error {
	if !s.UserApproved {
		return errors.New("requiere consentimiento explícito del usuario antes de delegar")
	}
	if !cleanText(s.Title, 150) || !cleanText(s.Objective, 3500) || !cleanText(s.Criteria, 2500) || !cleanText(s.Context, 3500) {
		return errors.New("título, objetivo, contexto y criterios obligatorios y acotados")
	}
	if !ident.MatchString(s.MCP) || (s.MCPID != "" && !ident.MatchString(s.MCPID)) || !validWorkspace(s.Workspace) || !branch.MatchString(s.Branch) || strings.Contains(s.Branch, "..") || strings.HasPrefix(s.Branch, "/") || strings.HasSuffix(s.Branch, "/") {
		return errors.New("MCP, instancia, ruta o rama inválida")
	}
	if len(s.AllowedPaths) == 0 || len(s.AllowedPaths) > 20 {
		return errors.New("indica de 1 a 20 rutas relativas permitidas")
	}
	for _, p := range s.AllowedPaths {
		if !validRelative(p) {
			return fmt.Errorf("ruta relativa fuera de alcance: %q", p)
		}
	}
	return nil
}
func (s *Store) saveLocked(next []Task) error {
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > maxFile {
		return errors.New("límite de almacenamiento de tareas alcanzado")
	}
	dir := filepath.Dir(s.file)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".agent-tasks-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), s.file); err != nil {
		return err
	}
	s.tasks = next
	return nil
}
func (s *Store) Create(spec Spec) (Task, error) {
	if err := validate(spec); err != nil {
		return Task{}, err
	}
	var idBytes [8]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return Task{}, err
	}
	now := time.Now().UTC()
	t := Task{
		ID: "TASK-" + strings.ToUpper(hex.EncodeToString(idBytes[:])), Title: strings.TrimSpace(spec.Title),
		Objective: strings.TrimSpace(spec.Objective), MCP: spec.MCP, MCPID: spec.MCPID, Workspace: spec.Workspace,
		Branch: spec.Branch, AllowedPaths: append([]string(nil), spec.AllowedPaths...), Criteria: spec.Criteria,
		Context: spec.Context, Status: "pending", CreatedAt: now, UpdatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tasks) >= maxTasks {
		return Task{}, errors.New("máximo de tareas alcanzado")
	}
	next := append(append([]Task(nil), s.tasks...), t)
	if err := s.saveLocked(next); err != nil {
		return Task{}, err
	}
	return t, nil
}
func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.tasks))
	for i := len(s.tasks) - 1; i >= 0; i-- {
		t := s.tasks[i]
		t.AllowedPaths = append([]string(nil), t.AllowedPaths...)
		out = append(out, t)
	}
	return out
}
func (s *Store) Get(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.ID == id {
			t.AllowedPaths = append([]string(nil), t.AllowedPaths...)
			return t, nil
		}
	}
	return Task{}, errors.New("tarea no encontrada")
}
func (s *Store) change(id string, fn func(*Task) error) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := append([]Task(nil), s.tasks...)
	for i := range next {
		if next[i].ID != id {
			continue
		}
		if err := fn(&next[i]); err != nil {
			return Task{}, err
		}
		next[i].UpdatedAt = time.Now().UTC()
		if err := s.saveLocked(next); err != nil {
			return Task{}, err
		}
		return next[i], nil
	}
	return Task{}, errors.New("tarea no encontrada")
}
func (s *Store) Claim(id, agent string) (Task, error) {
	if !cleanText(agent, 100) {
		return Task{}, errors.New("identificador de agente inválido")
	}
	return s.change(id, func(t *Task) error {
		if t.Status == "rejected" && t.Agent == agent {
			// Sólo el mismo agente puede retomar un trabajo devuelto.
			t.Status = "in_progress"
			t.Commit, t.Tests, t.Summary, t.Blocker = "", "", "", ""
			return nil
		}
		if t.Status != "pending" {
			return errors.New("tarea ya reclamada o cerrada")
		}
		t.Status = "in_progress"
		t.Agent = agent
		return nil
	})
}
func (s *Store) Submit(id, agent, status, commit, tests, summary, blocker string) (Task, error) {
	if !cleanText(agent, 100) || len(commit) > 120 || len(tests) > 2500 || len(summary) > 3000 || len(blocker) > 1500 {
		return Task{}, errors.New("entrega inválida")
	}
	return s.change(id, func(t *Task) error {
		if t.Status != "in_progress" && t.Status != "blocked" {
			return errors.New("tarea no está en ejecución")
		}
		if t.Agent != agent {
			return errors.New("la tarea corresponde a otro agente")
		}
		if status == "needs_review" {
			if !cleanText(commit, 120) || !cleanText(tests, 2500) || !cleanText(summary, 3000) {
				return errors.New("entrega requiere commit, pruebas y resumen")
			}
		} else if status == "blocked" {
			if !cleanText(blocker, 1500) {
				return errors.New("indica el bloqueo")
			}
		} else {
			return errors.New("estado permitido: needs_review o blocked")
		}
		t.Status = status
		t.Commit = commit
		t.Tests = tests
		t.Summary = summary
		t.Blocker = blocker
		return nil
	})
}

// Cancel blocks future claims or submissions in KagMCP. It cannot remotely
// terminate an already-running ChatGPT chat or an external MCP command.
func (s *Store) Cancel(id, note string) (Task, error) {
	if !cleanText(note, 1000) {
		return Task{}, errors.New("indica motivo de cancelación")
	}
	return s.change(id, func(t *Task) error {
		if t.Status == "approved" || t.Status == "cancelled" {
			return errors.New("la tarea ya está cerrada")
		}
		t.Status = "cancelled"
		t.Blocker = note
		return nil
	})
}
func (s *Store) Review(id, decision, note string) (Task, error) {
	if !cleanText(note, 2500) {
		return Task{}, errors.New("explica la decisión")
	}
	if decision != "approved" && decision != "rejected" {
		return Task{}, errors.New("decisión inválida")
	}
	return s.change(id, func(t *Task) error {
		if t.Status != "needs_review" {
			return errors.New("solo se revisan entregas pendientes")
		}
		t.Status = decision
		t.Review = note
		return nil
	})
}
func Prompt(t Task) string {
	lines := []string{
		"Eres un subagente especializado. Antes de trabajar identifica el conector MCP EXACTO asignado:",
		"Conector: " + t.MCP,
	}
	if t.MCPID != "" {
		lines = append(lines, "Identificador de instancia MCP: "+t.MCPID)
	}
	lines = append(lines,
		"Directorio de trabajo: "+t.Workspace,
		"Rama exclusiva de trabajo: "+t.Branch,
		"ID de delegación: "+t.ID,
		"",
		"Comprueba que tienes acceso REAL al MCP, al directorio y a la rama indicada. Si no coinciden, DETENTE y reporta bloqueo.",
		"Usa únicamente ese conector para esta tarea. No cambies de entorno sin nueva autorización.",
		"El MCP coordinador es KagMCP (la instancia en la que se creó "+t.ID+"). Conecta también ese MCP para registrar tu progreso. No presupongas que el MCP de trabajo tiene herramientas agent_task_*.",
		"En el MCP coordinador llama agent_task_open con id="+t.ID+" y un identificador estable de agente. Si no está conectado, continúa solamente con estas instrucciones y pide al usuario conectar el coordinador para reportar.",
		"En el MCP de trabajo usa exclusivamente el conector "+t.MCP+".",
		"Objetivo: "+t.Objective,
		"Contexto mínimo: "+t.Context,
		"Rutas permitidas (relativas al directorio): "+strings.Join(t.AllowedPaths, ", "),
		"Criterios de aceptación: "+t.Criteria,
		"No edites fuera del alcance, no ejecutes merge/push, no expongas secretos y no declares aprobado tu propio trabajo.",
		"Usa una rama o worktree aislado. Verifica pruebas y registra el SHA del commit real (no inventes evidencias).",
		"Al concluir llama agent_task_submit (needs_review) con commit, pruebas y resumen. Si te falta una decisión o acceso, usa status=blocked y justifica.",
		"El chat orquestador decidirá la integración. No necesitas el contexto completo de su conversación.",
	)
	return strings.Join(lines, "\n")
}
