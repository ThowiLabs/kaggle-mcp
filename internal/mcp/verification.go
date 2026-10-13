package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ThowiLabs/kagssh-go/internal/projectstate"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// projectArtifacts exige un repositorio limpio y un cuaderno válido.
// Esto verifica el estado, no sustituye la ejecución del notebook.
func (s *Server) projectArtifacts(ctx context.Context, id string) (string, string, error) {
	p, err := s.projects.Get(id)
	if err != nil {
		return "", "", err
	}
	if p.Repository == "" || p.Notebook == "" {
		return "", "", errors.New("primero define repositorio y cuaderno del proyecto")
	}
	dir, err := s.path(filepath.Join("/kaggle/working", id), false)
	if err != nil {
		return "", "", err
	}
	if filepath.IsAbs(p.Notebook) || strings.Contains(p.Notebook, "\\") || p.Notebook == ".." || strings.HasPrefix(filepath.Clean(p.Notebook), "../") {
		return "", "", errors.New("notebook fuera del proyecto")
	}
	path, err := s.path(filepath.Join(dir, p.Notebook), false)
	if err != nil {
		return "", "", err
	}
	if !strings.HasSuffix(strings.ToLower(path), ".ipynb") {
		return "", "", errors.New("notebook debe terminar en .ipynb")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return "", "", errors.New("notebook inválido o demasiado grande")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var n struct {
		Nbformat int `json:"nbformat"`
		Cells    []struct {
			CellType string `json:"cell_type"`
		} `json:"cells"`
	}
	if err = json.Unmarshal(raw, &n); err != nil || n.Nbformat != 4 || len(n.Cells) == 0 {
		return "", "", errors.New("formato de notebook inválido")
	}
	hasCode := false
	for _, cell := range n.Cells {
		if cell.CellType == "code" {
			hasCode = true
			break
		}
	}
	if !hasCode {
		return "", "", errors.New("notebook no contiene celdas de código")
	}
	// CWD acotado al repo real: rechazar gitdir enlazado a otro proyecto.
	gitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(gitCtx, "git", args...)
		cmd.Dir = dir
		cmd.Env = os.Environ()
		b, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("comando Git fallido: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	root, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	if filepath.Clean(root) != filepath.Clean(dir) {
		return "", "", errors.New("la carpeta del proyecto no es la raíz Git")
	}
	status, err := run("status", "--porcelain")
	if err != nil {
		return "", "", err
	}
	if status != "" {
		return "", "", errors.New("haz commit/ignora archivos pendientes antes de verificar el proyecto")
	}
	head, err := run("rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	return head, shaHex(raw), nil
}
func shaHex(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (s *Server) verifyProject(ctx context.Context, id, testCommand, notebookCommand string) (any, error) {
	if strings.TrimSpace(testCommand) == "" || strings.TrimSpace(notebookCommand) == "" {
		return nil, errors.New("debes especificar comando de pruebas y de ejecución del notebook")
	}
	if len(testCommand) > 4000 || len(notebookCommand) > 4000 {
		return nil, errors.New("comando demasiado largo")
	}
	head, notebookSHA, err := s.projectArtifacts(ctx, id)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join("/kaggle/working", id)
	for _, entry := range []struct{ name, command string }{{"pruebas", testCommand}, {"ejecución del notebook", notebookCommand}} {
		result, runErr := s.execWithAudit(ctx, entry.command, dir, id, "Verificación: "+entry.name)
		if runErr != nil {
			return nil, fmt.Errorf("%s: %w", entry.name, runErr)
		}
		if m, ok := result.(map[string]any); ok {
			if m["error"] != nil || m["timeout"] == true {
				return nil, fmt.Errorf("falló %s: %v", entry.name, m["error"])
			}
		} else {
			return nil, errors.New("resultado de pruebas no reconocido")
		}
	}
	currentHead, currentNotebook, err := s.projectArtifacts(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("artefactos modificados por las pruebas: %w", err)
	}
	if currentHead != head || currentNotebook != notebookSHA {
		return nil, errors.New("Git o notebook cambió durante pruebas")
	}
	receipt := projectstate.Verification{TestedAt: time.Now().UTC(), GitHead: head, NotebookSHA256: notebookSHA, TestCommandSHA256: shaHex([]byte(testCommand)), NotebookCommandSHA256: shaHex([]byte(notebookCommand))}
	if err := s.projects.SetVerification(id, receipt); err != nil {
		return nil, err
	}
	return map[string]any{"verified": true, "project": id, "git_head": head, "notebook_sha256": notebookSHA, "tested_at": receipt.TestedAt}, nil
}
