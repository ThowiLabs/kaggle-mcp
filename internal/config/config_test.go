package config

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSecrets struct {
	values map[string]string
	seen   []string
	err    error
}

func (f *fakeSecrets) Get(_ context.Context, label string) (string, bool, error) {
	f.seen = append(f.seen, label)
	if f.err != nil {
		return "", false, f.err
	}
	v, ok := f.values[label]
	return v, ok, nil
}

func TestValidate(t *testing.T) {
	base := Config{
		Listen: "127.0.0.1:2224", LoginUser: "root", LoginPassword: "local", SSHEnabled: true,
		VPSHost: "example.org", VPSUser: "root", VPSPassword: "remote", VPSPort: 22,
		VPSFingerprint: "SHA256:verificada", RemotePort: 2223,
	}
	tests := []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"válida", func(*Config) {}, ""},
		{"sin contraseña local", func(c *Config) { c.LoginPassword = "" }, "SSH_LOGIN_PASSWORD"},
		{"sin credenciales del VPS", func(c *Config) { c.VPSPassword = "" }, "SSH_PASSWORD"},
		{"usuario inválido", func(c *Config) { c.LoginUser = "root\nother" }, "usuario local"},
		{"escucha inválida", func(c *Config) { c.Listen = "2224" }, "SSH_PORT_LOCAL"},
		{"puerto inválido", func(c *Config) { c.RemotePort = 0 }, "puerto"},
		{"known_hosts ausente se inicializará", func(c *Config) {
			c.VPSFingerprint = ""
			c.VPSKnownHosts = filepath.Join(t.TempDir(), "known_hosts")
		}, ""},
		{"ruta de confianza vacía se rechaza", func(c *Config) {
			c.VPSFingerprint = ""
			c.VPSKnownHosts = ""
		}, "SSH_KNOWN_HOSTS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.change(&c)
			err := c.Validate()
			if tt.want == "" && err != nil {
				t.Fatalf("no esperaba error: %v", err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("error=%v; esperaba %q", err, tt.want)
			}
		})
	}
}

func TestLoad_SecretIndependienteYPuertos(t *testing.T) {
	secrets := &fakeSecrets{values: map[string]string{
		"SSH_HOST":           "vps.example",
		"SSH_PASSWORD":       "contraseña-remota",
		"SSH_LOGIN_PASSWORD": "contraseña-local",
		"SSH_PORT_REMOTE":    "2200",
		"SSH_PORT_KAGGLE":    "4321",
		"SSH_PORT_LOCAL":     "2233",
		"SSH_FINGERPRINT":    "SHA256:validada",
	}}
	cfg, err := Load(context.Background(), func(string) (string, bool) { return "", false }, secrets)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VPSHost != "vps.example" || cfg.VPSPort != 2200 || cfg.RemotePort != 4321 || cfg.Listen != "127.0.0.1:2233" {
		t.Fatalf("puertos y host incorrectos: host=%s vps=%d remoto=%d local=%s", cfg.VPSHost, cfg.VPSPort, cfg.RemotePort, cfg.Listen)
	}
	if len(secrets.seen) != len(names) {
		t.Fatalf("consultas: %d, esperaba %d", len(secrets.seen), len(names))
	}
}

func TestLoad_ExportSobrescribeSecret(t *testing.T) {
	f := &fakeSecrets{values: map[string]string{
		"SSH_PORT_KAGGLE": "3333", "SSH_PASSWORD": "desde-secrets",
		"SSH_LOGIN_PASSWORD": "local", "SSH_HOST": "vps.example",
	}}
	explicit := map[string]string{"SSH_PORT_KAGGLE": "4444", "SSH_PASSWORD": "desde-export"}
	cfg, err := Load(context.Background(), func(k string) (string, bool) { v, ok := explicit[k]; return v, ok }, f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RemotePort != 4444 || cfg.VPSPassword != "desde-export" {
		t.Fatal("la variable exportada no tiene prioridad")
	}
	for _, k := range f.seen {
		if k == "SSH_PORT_KAGGLE" || k == "SSH_PASSWORD" {
			t.Fatalf("consultó Secret cuando había export: %s", k)
		}
	}
}

func TestLoad_DefaultsSinKaggle(t *testing.T) {
	values := map[string]string{"SSH_HOST": "203.0.113.10", "SSH_PASSWORD": "remota", "SSH_LOGIN_PASSWORD": "local", "SSH_FINGERPRINT": "SHA256:verificada"}
	cfg, err := Load(context.Background(), func(k string) (string, bool) { v, ok := values[k]; return v, ok }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VPSPort != 22 || cfg.RemotePort != 2223 || cfg.Listen != "127.0.0.1:2224" {
		t.Fatal("defaults incorrectos")
	}
}

func TestLoad_ErroresEnSecretYValidacion(t *testing.T) {
	f := &fakeSecrets{err: fmt.Errorf("simulación")}
	if _, err := Load(context.Background(), func(string) (string, bool) { return "", false }, f); err == nil || !strings.Contains(err.Error(), "SSH_HOST") {
		t.Fatalf("error sin etiqueta: %v", err)
	}
	for _, port := range []string{"0", "65536", "abc", "22x", ""} {
		t.Run("port_"+port, func(t *testing.T) {
			_, err := parse(map[string]string{"SSH_PASSWORD": "v", "SSH_LOGIN_PASSWORD": "l", "SSH_PORT_REMOTE": port})
			if err == nil || !strings.Contains(err.Error(), "SSH_PORT_REMOTE") {
				t.Fatalf("puerto inválido %q: %v", port, err)
			}
		})
	}
}

func TestLoad_AutoKnownHostsSinNuevaVariable(t *testing.T) {
	values := map[string]string{
		"SSH_HOST": "203.0.113.10", "SSH_PASSWORD": "remota", "SSH_LOGIN_PASSWORD": "local",
		"SSH_KNOWN_HOSTS": filepath.Join(t.TempDir(), ".ssh", "known_hosts"),
	}
	cfg, err := Load(context.Background(), func(k string) (string, bool) { v, ok := values[k]; return v, ok }, nil)
	if err != nil {
		t.Fatalf("sin fingerprint y sin archivo debe iniciar para registrar la primera clave: %v", err)
	}
	if cfg.VPSFingerprint != "" || cfg.VPSKnownHosts != values["SSH_KNOWN_HOSTS"] {
		t.Fatalf("configuración de confianza inesperada: %#v", cfg.VPSKnownHosts)
	}
}

func TestLoad_RutaKnownHostsPredeterminada(t *testing.T) {
	values := map[string]string{
		"SSH_HOST": "203.0.113.10", "SSH_PASSWORD": "remota", "SSH_LOGIN_PASSWORD": "local",
	}
	cfg, err := Load(context.Background(), func(k string) (string, bool) { v, ok := values[k]; return v, ok }, nil)
	if err != nil {
		t.Fatalf("debe funcionar sin crear manualmente known_hosts: %v", err)
	}
	if cfg.VPSKnownHosts == "" {
		t.Fatal("no definió la ubicación predeterminada de known_hosts")
	}
}

func TestLoad_RequiereHostDelVPS(t *testing.T) {
	values := map[string]string{"SSH_PASSWORD": "remota", "SSH_LOGIN_PASSWORD": "local"}
	_, err := Load(context.Background(), func(k string) (string, bool) { v, ok := values[k]; return v, ok }, nil)
	if err == nil || !strings.Contains(err.Error(), "SSH_HOST") {
		t.Fatalf("el binario no debe emplear dominios corporativos predeterminados: %v", err)
	}
}

func TestTokenDeHuggingFaceDesdeEntorno(t *testing.T) {
	values := map[string]string{
		"MCP_ENABLED":  "true",
		"SSH_ENABLED":  "false",
		"HF_TOKEN":     "hf_prueba_unicamente",
		"GITHUB_TOKEN": "github_pat_test",
	}
	cfg, err := Load(context.Background(), func(k string) (string, bool) {
		v, ok := values[k]
		return v, ok
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HFToken != values["HF_TOKEN"] || cfg.GitHubToken != values["GITHUB_TOKEN"] {
		t.Fatal("no se cargaron las variables de entorno de los proveedores")
	}
}
