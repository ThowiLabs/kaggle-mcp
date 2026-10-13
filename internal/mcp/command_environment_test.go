package mcp

import (
	"strings"
	"testing"
)

func TestCommandEnvironmentProvidesActiveIntegrations(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "old-test-value")
	t.Setenv("HF_TOKEN", "old-test-value")
	s := newTestServer(t)
	s.githubMu.Lock()
	s.githubSecret = "active-example-git"
	s.githubMu.Unlock()
	s.hfMu.Lock()
	s.hfToken = "active-example-hf"
	s.hfMu.Unlock()
	env, github, hf := s.commandEnvironment()
	combined := strings.Join(env, "\n")
	if github != "active-example-git" || hf != "active-example-hf" {
		t.Fatal("no se retornaron las credenciales activas")
	}
	if !strings.Contains(combined, "GITHUB_TOKEN=active-example-git") || !strings.Contains(combined, "HF_TOKEN=active-example-hf") {
		t.Fatal("los subprocesos no reciben las credenciales configuradas")
	}
	if strings.Contains(combined, "old-test-value") {
		t.Fatal("se heredaron credenciales obsoletas")
	}
	s.githubMu.Lock()
	s.githubSecret = ""
	s.githubMu.Unlock()
	s.hfMu.Lock()
	s.hfToken = ""
	s.hfMu.Unlock()
	env, _, _ = s.commandEnvironment()
	for _, entry := range env {
		if strings.HasPrefix(entry, "GITHUB_TOKEN=") || strings.HasPrefix(entry, "HF_TOKEN=") {
			t.Fatal("se conservó una credencial revocada")
		}
	}
}
