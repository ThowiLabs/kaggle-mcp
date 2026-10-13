package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Solo opciones no sensibles se guardan en config.json. Nunca credenciales.
var publicSettingKeys = map[string]bool{
	"MCP_ENABLED": true, "MCP_TUNNEL": true, "MCP_GRADIO_RETRIES": true, "MCP_PUBLIC_URL": true,
	"MCP_LISTEN_PORT": true,
}

func readSettings(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leer config.json: %w", err)
	}
	if len(raw) > 16<<10 {
		return nil, errors.New("config.json demasiado grande")
	}
	var stored map[string]string
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("config.json inválido: %w", err)
	}
	clean := make(map[string]string)
	for key, value := range stored {
		if publicSettingKeys[key] {
			clean[key] = value
		}
	}
	return clean, nil
}
func SaveSettings(cfg Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(cfg.DataDir, 0700); err != nil {
		return err
	}
	settings := map[string]string{
		"MCP_ENABLED":        strconv.FormatBool(cfg.MCPEnabled),
		"MCP_TUNNEL":         cfg.MCPTunnel,
		"MCP_GRADIO_RETRIES": strconv.Itoa(max(1, cfg.MCPGradioRetries)),
		"MCP_LISTEN_PORT":    strconv.Itoa(cfg.MCPPort),
	}
	if cfg.MCPPublicURL != "" {
		settings["MCP_PUBLIC_URL"] = cfg.MCPPublicURL
	}
	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(cfg.DataDir, "config.json")
	temp, err := os.CreateTemp(cfg.DataDir, ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
