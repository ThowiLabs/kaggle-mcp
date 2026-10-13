package config

import "testing"

func TestPINEnBlancoODesdeSeisCaracteres(t *testing.T) {
	cfg := Config{MCPEnabled: true, MCPPort: 8181, MCPTunnel: "cloudflare"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Go debe poder generar el PIN cuando está vacío: %v", err)
	}
	cfg.MCPAccessPIN = "123456"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("PIN de seis caracteres rechazado: %v", err)
	}
	cfg.MCPAccessPIN = "12345"
	if err := cfg.Validate(); err == nil {
		t.Fatal("PIN de cinco caracteres permitido")
	}
	cfg.MCPAccessPIN = "123\n456"
	if err := cfg.Validate(); err == nil {
		t.Fatal("PIN con salto de línea permitido")
	}
}
