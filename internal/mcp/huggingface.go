package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Los secretos se retienen solo en el runtime y se inyectan a sus procesos hijo.
func (s *Server) hfConfigured() bool {
	s.hfMu.RLock()
	defer s.hfMu.RUnlock()
	return s.hfToken != ""
}

func validateHF(ctx context.Context, client *http.Client, endpoint, token string) error {
	if len(token) > 4096 || strings.ContainsAny(token, "\x00\r\n\t ") {
		return errors.New("formato de token Hugging Face inválido")
	}
	if token == "" {
		return errors.New("token vacío")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("dirección Hugging Face inválida")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("no se pudo verificar el token con Hugging Face")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Hugging Face rechazó el token (HTTP %d)", resp.StatusCode)
	}
	var result struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result); err != nil || result.Name == "" {
		return errors.New("respuesta de verificación Hugging Face inválida")
	}
	return nil
}

func (s *Server) configureHF(ctx context.Context, token string) (string, error) {
	if token != "" {
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		if err := validateHF(ctx, client, "https://huggingface.co/api/whoami-v2", token); err != nil {
			return "", err
		}
	}
	s.hfMu.Lock()
	defer s.hfMu.Unlock()
	if token == "" {
		if err := os.Unsetenv("HF_TOKEN"); err != nil {
			return "", err
		}
		s.hfToken = ""
		return "Token de Hugging Face desactivado", nil
	}
	if err := os.Setenv("HF_TOKEN", token); err != nil {
		return "", err
	}
	s.hfToken = token
	return "Token de Hugging Face activado para el runtime", nil
}
