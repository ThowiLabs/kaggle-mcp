package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/ThowiLabs/kaggle-mcp/internal/gradiolive"
)

// Each named tunnel publishes an HTTP app already listening on 127.0.0.1.
// Gradio assigns temporary hostnames; names are local aliases, not DNS names.
type appTunnel struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
	Status  string `json:"status"`
	Warning string `json:"warning,omitempty"`
	handle  publicTunnel
}
type appTunnels struct {
	mu     sync.Mutex
	byName map[string]*appTunnel
	start  func(context.Context, int) (string, publicTunnel, error)
	ctx    context.Context
}

var appName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,39}$`)

func newAppTunnels(ctx context.Context) *appTunnels {
	return &appTunnels{ctx: ctx, byName: make(map[string]*appTunnel), start: func(ctx context.Context, port int) (string, publicTunnel, error) {
		t, err := gradiolive.Start(ctx, port)
		if err != nil {
			return "", nil, err
		}
		return t.URL, t, nil
	}}
}
func localHTTP(port, excluded int) error {
	if port < 1024 || port > 65535 || port == excluded {
		return errors.New("puerto no permitido: usa el puerto HTTP local de una aplicación (1024-65535), distinto del MCP")
	}
	// No follow redirects or use HTTP proxy: only the explicit loopback port.
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest(http.MethodHead, fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("no se detectó una app HTTP en 127.0.0.1:%d: %w", port, err)
	}
	_ = res.Body.Close()
	if res.StatusCode < 100 || res.StatusCode >= 600 {
		return errors.New("respuesta HTTP de aplicación inválida")
	}
	return nil
}

func (m *appTunnels) list() []appTunnel {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]appTunnel, 0, len(m.byName))
	for _, t := range m.byName {
		entry := *t
		entry.handle = nil
		if done := tunnelDone(t.handle); done != nil {
			select {
			case <-done:
				entry.Status = "desconectado"
			default:
			}
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
func (m *appTunnels) create(name string, port, excluded int) (appTunnel, error) {
	if !appName.MatchString(name) {
		return appTunnel{}, errors.New("nombre inválido: 1-40 letras, números, guiones o guiones bajos; empieza con letra/número")
	}
	if err := localHTTP(port, excluded); err != nil {
		return appTunnel{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byName[name]; exists {
		return appTunnel{}, errors.New("ya existe un túnel con ese nombre; usa regenerar")
	}
	if len(m.byName) >= 5 {
		return appTunnel{}, errors.New("máximo cinco túneles activos por instancia")
	}
	if m.ctx.Err() != nil {
		return appTunnel{}, errors.New("instancia MCP detenida")
	}
	url, handle, err := m.start(m.ctx, port)
	if err != nil {
		return appTunnel{}, err
	}
	if handle == nil || url == "" {
		if handle != nil {
			_ = handle.Close()
		}
		return appTunnel{}, errors.New("Gradio no devolvió un túnel válido")
	}
	t := &appTunnel{Name: name, Port: port, URL: url, Status: "activo", handle: handle}
	m.byName[name] = t
	return appTunnel{Name: t.Name, Port: t.Port, URL: t.URL, Status: t.Status}, nil
}
func (m *appTunnels) regenerate(name string, excluded int) (appTunnel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.byName[name]
	if !ok {
		return appTunnel{}, errors.New("túnel no encontrado")
	}
	if err := localHTTP(old.Port, excluded); err != nil {
		return appTunnel{}, err
	}
	if m.ctx.Err() != nil {
		return appTunnel{}, errors.New("instancia MCP detenida")
	}
	url, handle, err := m.start(m.ctx, old.Port)
	if err != nil {
		return appTunnel{}, err
	}
	if handle == nil || url == "" {
		if handle != nil {
			_ = handle.Close()
		}
		return appTunnel{}, errors.New("Gradio no devolvió URL válida")
	}
	m.byName[name] = &appTunnel{Name: name, Port: old.Port, URL: url, Status: "activo", handle: handle}
	result := appTunnel{Name: name, Port: old.Port, URL: url, Status: "activo"}
	if err := old.handle.Close(); err != nil {
		result.Warning = "nuevo túnel activo, pero no se pudo confirmar el cierre del anterior"
	}
	return result, nil
}
func (m *appTunnels) delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byName[name]
	if !ok {
		return errors.New("túnel no encontrado")
	}
	if err := t.handle.Close(); err != nil {
		return err
	}
	delete(m.byName, name)
	return nil
}
func (m *appTunnels) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, t := range m.byName {
		_ = t.handle.Close()
		delete(m.byName, name)
	}
}
func (s *Server) invokeAppTunnels(_ context.Context, name string, raw json.RawMessage) (any, error) {
	var a struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, errors.New("argumentos de túnel inválidos")
		}
	}
	switch name {
	case "app_tunnel_list":
		return s.appTunnels.list(), nil
	case "app_tunnel_create":
		return s.appTunnels.create(a.Name, a.Port, s.cfg.MCPPort)
	case "app_tunnel_regenerate":
		return s.appTunnels.regenerate(a.Name, s.cfg.MCPPort)
	case "app_tunnel_delete":
		return map[string]any{"deleted": a.Name}, s.appTunnels.delete(a.Name)
	}
	return nil, errors.New("herramienta de túneles desconocida")
}
