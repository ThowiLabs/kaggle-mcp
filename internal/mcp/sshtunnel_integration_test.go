package mcp

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func attachTestSSH(t *testing.T, s *Server) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() {
		if s.ssh != nil {
			s.ssh.Stop()
		}
	})
	s.attachSSHTunnel(ctx, "LONG-TEST-PIN-ABC", "Kaggle MCP-nonce-de-prueba", slog.Default())
	return cancel
}

func TestServerWithoutSSHConfigStartsClean(t *testing.T) {
	s := newTestServer(t)
	attachTestSSH(t, s)
	if st := s.sshStatus(); st.Configured || st.Connected {
		t.Fatalf("sin configuración: %+v", st)
	}
}

func TestServerInvalidSSHConfigDoesNotBreak(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(s.cfg.DataDir, "ssh-tunnel.json")
	if err := os.MkdirAll(s.cfg.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{no-json"), 0600); err != nil {
		t.Fatal(err)
	}
	attachTestSSH(t, s)
	if st := s.sshStatus(); st.Configured || st.Connected {
		t.Fatalf("con configuración inválida: %+v", st)
	}
}

func TestConfigureSSHRequiresPINAndHidesPassword(t *testing.T) {
	s := newTestServer(t)
	attachTestSSH(t, s)
	if _, err := s.configureSSH("PIN-EQUIVOCADO", "usuario", "servidor.example.com", 22, 0, "secreta", false); err == nil {
		t.Fatal("con PIN incorrecto no debe guardar")
	}
	st, err := s.configureSSH("LONG-TEST-PIN-ABC", "usuario", "servidor.example.com", 22, 0, "secreta", false)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Configured || st.Enabled {
		t.Fatalf("estado tras guardar deshabilitado: %+v", st)
	}
	raw, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "ssh-tunnel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secreta") {
		t.Fatal("el archivo guardado contiene la contraseña en claro")
	}
	if strings.Contains(string(raw), "LONG-TEST-PIN-ABC") {
		t.Fatal("el archivo guardado contiene el PIN en claro")
	}
	if _, err := s.configureSSH("LONG-TEST-PIN-ABC", "usuario", "servidor.example.com", 22, 0, "", false); err == nil {
		t.Fatal("con contraseña vacía no debe guardar")
	}
}

func TestAttachWithGeneratedPINDoesNotUnlock(t *testing.T) {
	s := newTestServer(t)
	// Configuración guardada con el PIN real, pero arranque con PIN vacío
	// (generado): no debe descifrar ni arrancar.
	attachTestSSH(t, s)
	if _, err := s.configureSSH("LONG-TEST-PIN-ABC", "usuario", "servidor.example.com", 22, 0, "secreta", true); err != nil {
		t.Fatal(err)
	}
	s.ssh.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer s.ssh.Stop()
	s.attachSSHTunnel(ctx, "", "Kaggle MCP-nonce-de-prueba", slog.Default())
	if st := s.sshStatus(); !st.Configured || st.PINConfigured || st.Connected {
		t.Fatalf("con PIN generado: %+v", st)
	}
	if _, err := s.startSSH(); err == nil {
		t.Fatal("sin contraseña descifrada no debe arrancar")
	}
}

func TestRebuildKeepsSSHTunnel(t *testing.T) {
	s := newTestServer(t)
	attachTestSSH(t, s)
	if _, err := s.configureSSH("LONG-TEST-PIN-ABC", "usuario", "servidor.example.com", 22, 0, "secreta", false); err != nil {
		t.Fatal(err)
	}
	next, err := rebuildMCPForPublic(s.cfg, s, "https://otro.trycloudflare.com")
	if err != nil {
		t.Fatal(err)
	}
	if next.ssh == nil || next.ssh != s.ssh {
		t.Fatal("la reconexión del túnel público debe conservar el gestor SSH")
	}
	if st := next.sshStatus(); !st.Configured {
		t.Fatalf("estado tras reconexión: %+v", st)
	}
}
