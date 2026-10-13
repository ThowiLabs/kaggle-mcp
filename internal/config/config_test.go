package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguracionMCP(t *testing.T) {
	cfg, err := Load(context.Background(), func(k string) (string, bool) {
		v, ok := map[string]string{"MCP_ENABLED": "true", "MCP_TUNNEL": "auto"}[k]
		return v, ok
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MCPEnabled || cfg.MCPPort != 8181 || cfg.MCPTunnel != "auto" {
		t.Fatalf("valores predeterminados inválidos: %+v", cfg)
	}
}

func TestValidarMCP(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{"MCP deshabilitado", map[string]string{"MCP_ENABLED": "false"}, "MCP_ENABLED"},
		{"Puerto inválido", map[string]string{"MCP_LISTEN_PORT": "70000"}, "MCP_LISTEN_PORT"},
		{"Túnel inválido", map[string]string{"MCP_TUNNEL": "incorrecto"}, "MCP_TUNNEL"},
		{"PIN inválido", map[string]string{"MCP_ACCESS_PIN": "abc"}, "MCP_ACCESS_PIN"},
		{"Origen obligatorio", map[string]string{"MCP_TUNNEL": "none"}, "MCP_PUBLIC_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(context.Background(), func(k string) (string, bool) { v, ok := tt.env[k]; return v, ok }, nil)
			if err == nil || !strings.Contains(err.Error(), tt.expected) {
				t.Fatalf("esperado %s: %v", tt.expected, err)
			}
		})
	}
}

func TestEnvSinLecturaDeSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KAGMCP_DATA_DIR", filepath.Join(dir, "estado"))
	t.Setenv("MCP_ENABLED", "true")
	t.Setenv("MCP_TUNNEL", "auto")
	t.Setenv("KAGGLE_USER_SECRETS_TOKEN", "secreto-no-consultar")
	cfg, err := FromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != filepath.Join(dir, "estado") {
		t.Fatalf("ruta inesperada: %s", cfg.DataDir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directorio de prueba inválido: %v", err)
	}
}
