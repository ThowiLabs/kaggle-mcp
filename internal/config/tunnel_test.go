package config

import (
	"context"
	"testing"
)

func TestTunnelDefaultGradioWithAutomaticFallback(t *testing.T) {
	cfg, err := Load(context.Background(), func(k string) (string, bool) {
		if k == "MCP_ENABLED" {
			return "true", true
		}
		if k == "MCP_ACCESS_PIN" {
			return "", true
		}
		return "", false
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPTunnel != "auto" || cfg.MCPGradioRetries != 3 {
		t.Fatalf("esperado auto/3: %+v", cfg)
	}
	for _, tc := range []struct {
		mode  string
		valid bool
	}{
		{"auto", true}, {"gradio", true}, {"cloudflare", true}, {"none", false}, {"invalid", false},
	} {
		m := map[string]string{"MCP_ENABLED": "true", "MCP_TUNNEL": tc.mode}
		loaded, err := Load(context.Background(), func(k string) (string, bool) { v, ok := m[k]; return v, ok }, nil)
		if (err == nil) != tc.valid {
			t.Errorf("modo %s: %v", tc.mode, err)
		}
		if err == nil && loaded.MCPTunnel != tc.mode {
			t.Errorf("modo %s no conservado", tc.mode)
		}
	}
	for _, retries := range []struct {
		value string
		valid bool
	}{{"1", true}, {"3", true}, {"5", true}, {"0", false}, {"6", false}, {"abc", false}} {
		m := map[string]string{"MCP_ENABLED": "true", "MCP_GRADIO_RETRIES": retries.value}
		_, err := Load(context.Background(), func(k string) (string, bool) { v, ok := m[k]; return v, ok }, nil)
		if (err == nil) != retries.valid {
			t.Errorf("MCP_GRADIO_RETRIES=%s: %v", retries.value, err)
		}
	}
}
