package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ThowiLabs/kagssh-go/internal/diskguard"
	"github.com/ThowiLabs/kagssh-go/internal/githubtoken"
	"github.com/ThowiLabs/kagssh-go/internal/projectstate"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) githubConfigured() bool {
	s.githubMu.RLock()
	defer s.githubMu.RUnlock()
	return s.githubOn
}
func (s *Server) githubClient() *githubtoken.Client {
	s.githubMu.RLock()
	defer s.githubMu.RUnlock()
	return s.github
}
func (s *Server) configureGitHub(ctx context.Context, token string) (string, error) {
	if len(token) > 4096 {
		return "", errors.New("token demasiado largo")
	}
	if token == "" {
		s.githubMu.Lock()
		s.github = githubtoken.New("")
		if err := os.Unsetenv("GITHUB_TOKEN"); err != nil {
			s.githubMu.Unlock()
			return "", err
		}
		s.githubOn = false
		s.githubMu.Unlock()
		return "PAT desactivado para herramientas y procesos nuevos", nil
	}
	if strings.ContainsAny(token, "\x00\r\n\t ") {
		return "", errors.New("formato de PAT inválido")
	}
	candidate := githubtoken.New(token)
	info, err := candidate.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("PAT no verificado: %w", err)
	}
	s.githubMu.Lock()
	if err := os.Setenv("GITHUB_TOKEN", token); err != nil {
		s.githubMu.Unlock()
		return "", err
	}
	s.github = candidate
	s.githubOn = true
	s.githubMu.Unlock()
	return fmt.Sprint(info), nil
}
func (s *Server) projectExport(id string) (any, error) {
	p, err := s.projects.Get(id)
	if err != nil {
		return nil, err
	}
	if p.Repository == "" || p.Notebook == "" {
		return nil, errors.New("define repository y notebook del proyecto")
	}
	// El contenido exportado puede incorporarse mediante git sin suponer
	// que el disco temporal de Kaggle será persistente.
	var m strings.Builder
	fmt.Fprintf(&m, "# Proyecto: %s\n\nRepositorio: %s\nNotebook: %s\n\n## Decisiones y memoria\n", p.Name, p.Repository, p.Notebook)
	for _, v := range p.Memory {
		fmt.Fprintf(&m, "\n- %s", v)
	}
	m.WriteString("\n\n## Tasklist\n")
	for _, t := range p.Tasks {
		fmt.Fprintf(&m, "\n- [%s] %s (%s)", map[bool]string{true: "x", false: " "}[t.Status == "done"], t.Title, t.Status)
	}
	content := m.String()
	if len(content) > 1<<20 {
		return nil, errors.New("contexto demasiado grande")
	}
	base, err := s.path(filepath.Join("/kaggle/working", id), true)
	if err != nil {
		return nil, err
	}
	if info, e := os.Stat(filepath.Join(base, ".git")); e != nil || !info.IsDir() {
		return nil, errors.New("repositorio Git aún no creado")
	}
	if err := diskguard.Check(base); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(base, "contexto"), 0700); err != nil {
		return nil, err
	}
	path, err := s.path(filepath.Join(base, "contexto", "kagmcp-proyecto.md"), true)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return nil, err
	}
	statePath, err := s.path(filepath.Join(base, "contexto", "kagmcp-proyecto.json"), true)
	if err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(statePath, encoded, 0600); err != nil {
		return nil, err
	}
	return map[string]any{"markdown": path, "state": statePath, "warning": "Archivos sensibles del proyecto: revisa su contenido antes de hacer commit/push. Para recuperar en otro runtime, usa project_import"}, nil
}
func (s *Server) scaffoldGradio(id string, testsPassed bool) (any, error) {
	_ = testsPassed // Compatibilidad con clientes antiguos: no constituye una prueba.
	// La generación exige comprobante de ejecución real y artefactos sin cambios.
	p, err := s.projects.Get(id)
	if err != nil {
		return nil, err
	}
	if p.Repository == "" || p.Notebook == "" {
		return nil, errors.New("registra repositorio y notebook primero")
	}
	if p.Verification == nil || !p.Verification.Valid() {
		return nil, errors.New("primero ejecuta project_verify con pruebas y notebook reales")
	}
	actualHead, actualNotebook, verifyErr := s.projectArtifacts(context.Background(), id)
	if verifyErr != nil {
		return nil, verifyErr
	}
	if actualHead != p.Verification.GitHead || actualNotebook != p.Verification.NotebookSHA256 {
		return nil, errors.New("repositorio o notebook cambió después de verificar; repite project_verify")
	}
	dir, err := s.path(filepath.Join("/kaggle/working", id), true)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !info.IsDir() {
		return nil, errors.New("primero crea/corrige repositorio Git en /kaggle/working/<project_id>")
	}
	if filepath.IsAbs(p.Notebook) || p.Notebook == ".." || strings.HasPrefix(filepath.Clean(p.Notebook), "../") || strings.Contains(p.Notebook, "\\") {
		return nil, errors.New("ruta de notebook fuera del proyecto")
	}
	nb, err := s.path(filepath.Join(dir, p.Notebook), false)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(nb, ".ipynb") {
		return nil, errors.New("notebook debe ser .ipynb")
	}
	raw, err := os.ReadFile(nb)
	if err != nil {
		return nil, fmt.Errorf("notebook inexistente: %w", err)
	}
	if len(raw) > 8<<20 {
		return nil, errors.New("notebook demasiado grande")
	}
	var n struct {
		Nbformat int   `json:"nbformat"`
		Cells    []any `json:"cells"`
	}
	if err = json.Unmarshal(raw, &n); err != nil || n.Nbformat != 4 || len(n.Cells) == 0 {
		return nil, errors.New("notebook inválido")
	}
	if err := diskguard.Check(dir); err != nil {
		return nil, err
	}
	app := filepath.Join(dir, "app.py")
	f, err := os.OpenFile(app, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("no sobrescribir UI existente: %w", err)
	}
	body := "import gradio as gr\n\n# Integrar la función real después de validar proyecto.\ndef consultar(texto: str) -> str:\n    return f\"Proyecto listo: {texto}\"\n\nui = gr.Interface(fn=consultar, inputs=gr.Textbox(label=\"Entrada\"), outputs=gr.Textbox(label=\"Salida\"), title=" + strconv.Quote(p.Name) + ")\nif __name__ == \"__main__\":\n    ui.launch(server_name=\"127.0.0.1\", server_port=7860, share=False)\n"
	_, err = f.WriteString(body)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(app)
		return nil, errors.New("no se pudo escribir Gradio")
	}
	deps := filepath.Join(dir, "requirements-gradio.in")
	if err = os.WriteFile(deps, []byte("gradio==6.30.0\n"), 0600); err != nil {
		return nil, err
	}
	guide := filepath.Join(dir, "GRADIO_REPRODUCIBLE.md")
	guidance := "# Gradio reproducible (solo después de verificar repo/notebook)\n\n1. Verifica el espacio libre: `df -h /kaggle/working /tmp`.\n2. Pin del compilador: `python -m pip install pip-tools==7.6.2`.\n3. Genera lock con hashes desde un entorno limpio: `pip-compile --generate-hashes -o requirements-gradio.lock requirements-gradio.in`.\n4. Versiona el lock y registra la versión de Python.\n5. Instala solamente desde lock: `python -m pip install --require-hashes --no-cache-dir -r requirements-gradio.lock`.\n6. Integra la función real del notebook en app.py; verifica el funcionamiento completo antes de publicar.\n\nNo ejecutar pip install sin versión ni habilitar share=True por defecto.\n"
	if err = os.WriteFile(guide, []byte(guidance), 0600); err != nil {
		return nil, err
	}
	return map[string]any{"ui": app, "dependencies": deps, "lock_instructions": guide, "warning": "Plantilla: integra la lógica real, genera lock con hashes y prueba Gradio"}, nil
}
func managementTool(name string) bool {
	switch name {
	case "skills_list", "skills_read", "skills_search", "skills_install", "projects_list", "project_create", "project_memory_add", "project_export", "project_import", "tasks_add", "tasks_update", "history_list", "project_verify", "gradio_scaffold":
		return true
	}
	return false
}
func (s *Server) invokeManagement(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var a struct {
		Name            string `json:"name"`
		Query           string `json:"query"`
		Project         string `json:"project"`
		Title           string `json:"title"`
		Repository      string `json:"repository"`
		Notebook        string `json:"notebook"`
		Content         string `json:"content"`
		Task            string `json:"task"`
		Status          string `json:"status"`
		Offset          int    `json:"offset"`
		TestsPassed     bool   `json:"tests_passed"`
		TestCommand     string `json:"test_command"`
		NotebookCommand string `json:"notebook_command"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
	}
	switch name {
	case "skills_list":
		return s.skills.List()
	case "skills_read":
		return s.skills.Read(a.Name, a.Offset, 12000)
	case "skills_search":
		return s.skills.Search(a.Query)
	case "skills_install":
		if err := diskguard.Check(s.cfg.DataDir); err != nil {
			return nil, err
		}
		return map[string]any{"installed": a.Name}, s.skills.Install(a.Name, a.Content)
	case "projects_list":
		return s.projects.Snapshot(), nil
	case "project_create":
		if err := diskguard.Check(s.cfg.DataDir); err != nil {
			return nil, err
		}
		return map[string]any{"project": a.Project}, s.projects.Create(a.Project, a.Title, a.Repository, a.Notebook)
	case "project_memory_add":
		if err := diskguard.Check(s.cfg.DataDir); err != nil {
			return nil, err
		}
		return map[string]any{"recorded": true}, s.projects.Remember(a.Project, a.Content)
	case "project_export":
		return s.projectExport(a.Project)
	case "project_import":
		return s.projectImport(a.Project)
	case "tasks_add":
		if err := diskguard.Check(s.cfg.DataDir); err != nil {
			return nil, err
		}
		return s.projects.TaskAdd(a.Project, a.Title)
	case "tasks_update":
		if err := diskguard.Check(s.cfg.DataDir); err != nil {
			return nil, err
		}
		return map[string]any{"updated": a.Task}, s.projects.TaskSet(a.Project, a.Task, a.Status)
	case "history_list":
		all := s.projects.Snapshot().History
		result := []any{}
		for i := len(all) - 1; i >= 0 && len(result) < 200; i-- {
			if a.Project == "" || all[i].Project == a.Project {
				result = append(result, all[i])
			}
		}
		return result, nil
	case "project_verify":
		return s.verifyProject(ctx, a.Project, a.TestCommand, a.NotebookCommand)
	case "gradio_scaffold":
		return s.scaffoldGradio(a.Project, a.TestsPassed)
	}
	return nil, errors.New("acción desconocida")
}
func (s *Server) execWithAudit(ctx context.Context, command, cwd, project, description string) (any, error) {
	if project != "" {
		if _, err := s.projects.Get(project); err != nil {
			return nil, err
		}
	}
	if err := diskguard.Check("/kaggle/working"); err != nil {
		return nil, err
	}
	out, runErr := s.exec(ctx, command, cwd)
	status := "ok"
	if runErr != nil {
		status = "error"
	}
	if result, ok := out.(map[string]any); ok && result["error"] != nil {
		status = "error"
	}
	if recErr := s.projects.Record(project, safeCommand(description), safeCommand(command), status); recErr != nil {
		return out, fmt.Errorf("comando ejecutado pero auditoría fallida: %w", recErr)
	}
	return out, runErr
}
func safeCommand(command string) string {
	lower := strings.ToLower(command)
	for _, word := range []string{"password", "passwd", "token", "secret", "authorization", "bearer", "cookie", "credential", "api_key", "api-key", "apikey", "access_pin", "ssh_key", "github_pat_", "ghp_", "base64"} {
		if strings.Contains(lower, word) {
			return "[COMANDO OCULTO: posible credencial]"
		}
	}
	return command
}
func (s *Server) projectImport(id string) (any, error) {
	if id == "" || len(id) > 64 {
		return nil, errors.New("ID inválido")
	}
	base, err := s.path(filepath.Join("/kaggle/working", id), false)
	if err != nil {
		return nil, err
	}
	path, err := s.path(filepath.Join(base, "contexto", "kagmcp-proyecto.json"), false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 512<<10 {
		return nil, errors.New("estado JSON no permitido")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p projectstate.Project
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("JSON de proyecto inválido")
	}
	if p.ID != id {
		return nil, errors.New("ID no coincide con repositorio")
	}
	if err = s.projects.Restore(p); err != nil {
		return nil, err
	}
	return map[string]any{"imported": id, "tasks": len(p.Tasks), "memory": len(p.Memory)}, nil
}
