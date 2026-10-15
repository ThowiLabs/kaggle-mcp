package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var names = []string{
	"MCP_ENABLED", "MCP_TUNNEL", "MCP_GRADIO_RETRIES", "MCP_PUBLIC_URL",
	"MCP_LISTEN_PORT", "MCP_ACCESS_PIN", "GITHUB_TOKEN", "HF_TOKEN",
}

type Config struct {
	MCPEnabled                                                           bool
	MCPPort, MCPGradioRetries                                            int
	MCPTunnel, MCPPublicURL, MCPAccessPIN, GitHubToken, HFToken, DataDir string
}

type SecretReader interface {
	Get(context.Context, string) (string, bool, error)
}

func FromEnv(ctx context.Context) (Config, error) {
	dataDir := "/kaggle/working/.kagmcp"
	if custom := os.Getenv("KAGMCP_DATA_DIR"); custom != "" {
		if !filepath.IsAbs(custom) {
			return Config{}, errors.New("KAGMCP_DATA_DIR debe ser ruta absoluta")
		}
		dataDir = filepath.Clean(custom)
	}
	settings, err := readSettings(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	cfg, err := load(ctx, os.LookupEnv, nil, settings)
	if err != nil {
		return cfg, err
	}
	cfg.DataDir = dataDir
	return cfg, nil
}

func Load(ctx context.Context, lookup func(string) (string, bool), _ SecretReader) (Config, error) {
	return load(ctx, lookup, nil, nil)
}

func load(_ context.Context, lookup func(string) (string, bool), _ SecretReader, settings map[string]string) (Config, error) {
	values := make(map[string]string, len(names))
	for _, name := range names {
		if value, found := lookup(name); found {
			values[name] = value
			continue
		}
		if value, found := settings[name]; found && publicSettingKeys[name] {
			values[name] = value
		}
	}
	return parse(values)
}

func parse(v map[string]string) (Config, error) {
	value := func(name, fallback string) string {
		if x, ok := v[name]; ok {
			return x
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
	enabled, err := strconv.ParseBool(value("MCP_ENABLED", "true"))
	if err != nil {
		return Config{}, errors.New("MCP_ENABLED debe ser true o false")
	}
	c := Config{
		MCPEnabled: enabled, MCPPort: mcpPort, MCPGradioRetries: retries,
		MCPTunnel: value("MCP_TUNNEL", "auto"), MCPPublicURL: value("MCP_PUBLIC_URL", ""),
		MCPAccessPIN: value("MCP_ACCESS_PIN", ""), GitHubToken: value("GITHUB_TOKEN", ""),
		HFToken: value("HF_TOKEN", ""), DataDir: "/kaggle/working/.kagmcp",
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if !c.MCPEnabled {
		return errors.New("MCP_ENABLED debe ser true para iniciar Kaggle MCP")
	}
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
	return nil
}
