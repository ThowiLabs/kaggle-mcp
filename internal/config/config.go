package config

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Las mismas claves se usan como variables exportadas o etiquetas de Kaggle Secrets.
var names = []string{
	"SSH_HOST", "SSH_USER", "SSH_PASSWORD", "SSH_KEY", "SSH_FINGERPRINT",
	"SSH_KNOWN_HOSTS", "SSH_PORT_REMOTE", "SSH_PORT_KAGGLE", "SSH_PORT_LOCAL",
	"SSH_LOGIN_USER", "SSH_LOGIN_PASSWORD",
	"SSH_AUTHORIZED_KEYS", "SSH_HOST_KEY",
	"MCP_ENABLED", "MCP_TUNNEL", "MCP_GRADIO_RETRIES", "MCP_PUBLIC_URL", "MCP_LISTEN_PORT", "MCP_ACCESS_PIN",
	"SSH_ENABLED", "GITHUB_TOKEN", "HF_TOKEN",
}

type Config struct {
	Listen, HostKey, LoginUser, LoginPassword, AuthorizedKeys            string
	VPSHost, VPSUser, VPSPassword, VPSKey, VPSKnownHosts, VPSFingerprint string
	VPSPort, RemotePort                                                  int
	Retry                                                                time.Duration
	SSHEnabled, MCPEnabled                                               bool
	MCPPort, MCPGradioRetries                                            int
	MCPTunnel, MCPPublicURL, MCPAccessPIN, GitHubToken, HFToken, DataDir string
}

// SecretReader permite probar Kaggle Secrets sin una conexión real.
type SecretReader interface {
	Get(context.Context, string) (string, bool, error)
}

func FromEnv(ctx context.Context) (Config, error) {
	var reader SecretReader
	if token := os.Getenv("KAGGLE_USER_SECRETS_TOKEN"); token != "" {
		reader = newKaggleSecrets(token, os.Getenv("KAGGLE_IAP_TOKEN"))
	}
	settings, err := readSettings("/kaggle/working/.kagmcp/config.json")
	if err != nil {
		return Config{}, err
	}
	return load(ctx, os.LookupEnv, reader, settings)
}

// Load: export explícito > secret individual de Kaggle > valor predeterminado.
func Load(ctx context.Context, lookup func(string) (string, bool), secrets SecretReader) (Config, error) {
	return load(ctx, lookup, secrets, nil)
}

// load respeta export > Secret > preferencia no sensible de .kagmcp/config.json > default.
func load(ctx context.Context, lookup func(string) (string, bool), secrets SecretReader, settings map[string]string) (Config, error) {
	values := make(map[string]string, len(names))
	for _, name := range names {
		if value, exists := lookup(name); exists {
			values[name] = value
			continue
		}
		if secrets != nil {
			value, found, err := secrets.Get(ctx, name)
			if err != nil {
				return Config{}, fmt.Errorf("leyendo el Secret %s de Kaggle: %w", name, err)
			}
			if found {
				values[name] = value
				continue
			}
		}
		if value, found := settings[name]; found && publicSettingKeys[name] {
			values[name] = value
		}
	}
	return parse(values)
}

func parse(v map[string]string) (Config, error) {
	login := "root"
	if u, err := user.Current(); err == nil && u.Username != "" {
		login = u.Username
	}
	value := func(name, fallback string) string {
		if v, ok := v[name]; ok {
			return v
		}
		return fallback
	}
	port := func(name string, fallback int) (int, error) {
		raw, ok := v[name]
		if !ok {
			return fallback, nil
		}
		p, err := strconv.Atoi(raw)
		if err != nil || p < 1 || p > 65535 {
			return 0, fmt.Errorf("%s debe estar entre 1 y 65535", name)
		}
		return p, nil
	}
	vpsPort, err := port("SSH_PORT_REMOTE", 22)
	if err != nil {
		return Config{}, err
	}
	remotePort, err := port("SSH_PORT_KAGGLE", 2223)
	if err != nil {
		return Config{}, err
	}
	localPort, err := port("SSH_PORT_LOCAL", 2224)
	if err != nil {
		return Config{}, err
	}
	mcpPort, err := port("MCP_LISTEN_PORT", 8181)
	if err != nil {
		return Config{}, err
	}
	retries := 3
	if raw, ok := v["MCP_GRADIO_RETRIES"]; ok {
		retries, err = strconv.Atoi(raw)
		if err != nil || retries < 1 || retries > 5 {
			return Config{}, errors.New("MCP_GRADIO_RETRIES debe estar entre 1 y 5")
		}
	}
	mcpEnabled, err := strconv.ParseBool(value("MCP_ENABLED", "false"))
	if err != nil {
		return Config{}, errors.New("MCP_ENABLED debe ser true o false")
	}
	defaultSSH := "true"
	if mcpEnabled && value("SSH_HOST", "") == "" {
		defaultSSH = "false"
	}
	sshEnabled, err := strconv.ParseBool(value("SSH_ENABLED", defaultSSH))
	if err != nil {
		return Config{}, errors.New("SSH_ENABLED debe ser true o false")
	}
	c := Config{
		Listen:           net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)),
		SSHEnabled:       sshEnabled,
		MCPEnabled:       mcpEnabled,
		MCPPort:          mcpPort,
		MCPGradioRetries: retries,
		MCPTunnel:        value("MCP_TUNNEL", "auto"),
		MCPPublicURL:     value("MCP_PUBLIC_URL", ""),
		MCPAccessPIN:     value("MCP_ACCESS_PIN", ""),
		GitHubToken:      value("GITHUB_TOKEN", ""),
		HFToken:          value("HF_TOKEN", ""),
		DataDir:          "/kaggle/working/.kagmcp",
		HostKey:          value("SSH_HOST_KEY", filepath.Join("/kaggle/working", ".kagmcp", "ssh", "host_ed25519")),
		LoginUser:        value("SSH_LOGIN_USER", login),
		LoginPassword:    value("SSH_LOGIN_PASSWORD", ""),
		AuthorizedKeys:   value("SSH_AUTHORIZED_KEYS", ""),
		VPSHost:          value("SSH_HOST", ""),
		VPSUser:          value("SSH_USER", "root"),
		VPSPassword:      value("SSH_PASSWORD", ""),
		VPSKey:           value("SSH_KEY", ""),
		VPSKnownHosts:    value("SSH_KNOWN_HOSTS", filepath.Join("/kaggle/working", ".kagmcp", "ssh", "known_hosts")),
		VPSFingerprint:   value("SSH_FINGERPRINT", ""),
		VPSPort:          vpsPort,
		RemotePort:       remotePort,
		Retry:            5 * time.Second,
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if !c.SSHEnabled && !c.MCPEnabled {
		return errors.New("habilita MCP_ENABLED o SSH_ENABLED")
	}
	if c.MCPEnabled {
		if c.MCPAccessPIN != "" && (len(c.MCPAccessPIN) < 6 || len(c.MCPAccessPIN) > 128 || strings.ContainsAny(c.MCPAccessPIN, "\r\n\x00")) {
			return errors.New("MCP_ACCESS_PIN debe estar vacío (Go genera PIN) o tener entre 6 y 128 caracteres")
		}
		switch c.MCPTunnel {
		case "auto", "gradio", "cloudflare", "none":
		default:
			return errors.New("MCP_TUNNEL admite auto (Gradio→Cloudflare), gradio, cloudflare o none")
		}
		if c.MCPGradioRetries < 0 || c.MCPGradioRetries > 5 {
			return errors.New("MCP_GRADIO_RETRIES debe estar entre 1 y 5")
		}
		if c.MCPPort < 1 || c.MCPPort > 65535 {
			return errors.New("MCP_LISTEN_PORT fuera de rango")
		}
		if c.MCPPublicURL != "" {
			u, err := url.Parse(c.MCPPublicURL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(c.MCPPublicURL, "\r\n\x00") {
				return errors.New("MCP_PUBLIC_URL debe ser un origen https sin path, credenciales, query ni fragment")
			}
			if c.MCPTunnel == "cloudflare" {
				return errors.New("MCP_PUBLIC_URL sólo se usa con MCP_TUNNEL=none; Cloudflare crea URL temporal")
			}
		}
		if c.MCPTunnel == "none" && c.MCPPublicURL == "" {
			return errors.New("MCP_PUBLIC_URL es obligatorio con MCP_TUNNEL=none para OAuth remoto")
		}
	}
	if !c.SSHEnabled {
		return nil
	}
	if c.LoginUser == "" || strings.ContainsAny(c.LoginUser, "\r\n\x00") {
		return errors.New("usuario local inválido")
	}
	if c.LoginPassword == "" && c.AuthorizedKeys == "" {
		return errors.New("configura SSH_LOGIN_PASSWORD o SSH_AUTHORIZED_KEYS en Secrets o export")
	}
	if c.VPSPassword == "" && c.VPSKey == "" {
		return errors.New("configura SSH_PASSWORD o SSH_KEY en Secrets o export")
	}
	if c.VPSHost == "" || strings.ContainsAny(c.VPSHost, "\r\n\x00") {
		return errors.New("SSH_HOST: configura la IP pública o dominio de tu VPS en Kaggle Secrets o en el entorno")
	}
	if c.VPSUser == "" || strings.ContainsAny(c.VPSUser, "\r\n\x00") {
		return errors.New("SSH_USER inválido")
	}
	if c.VPSFingerprint == "" && c.VPSKnownHosts == "" {
		return errors.New("SSH_KNOWN_HOSTS o SSH_FINGERPRINT debe tener una ruta/huella; se configura ruta predeterminada automáticamente")
	}
	if c.VPSFingerprint == "" {
		if info, err := os.Stat(c.VPSKnownHosts); err == nil && info.IsDir() {
			return errors.New("SSH_KNOWN_HOSTS debe ser un archivo, no un directorio")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("comprobar known_hosts del VPS: %w", err)
		}
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("SSH_PORT_LOCAL inválido: %w", err)
	}
	if c.VPSPort < 1 || c.VPSPort > 65535 || c.RemotePort < 1 || c.RemotePort > 65535 {
		return errors.New("puerto fuera de rango")
	}
	return nil
}
