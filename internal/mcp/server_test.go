package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThowiLabs/kagssh-go/internal/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Config{MCPEnabled: true, MCPPort: 8181, MCPTunnel: "none",
		MCPPublicURL: "https://sample.trycloudflare.com", MCPAccessPIN: "LONG-TEST-PIN-ABC",
		DataDir: filepath.Join(t.TempDir(), ".kagmcp")}
	s, err := New(cfg, cfg.MCPPublicURL)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestOAuthRequiredForToolAccess(t *testing.T) {
	s := newTestServer(t)
	for _, tc := range []struct {
		method, path, origin string
		status               int
	}{
		{"POST", "/mcp", "", http.StatusUnauthorized},
		{"POST", "/mcp", "https://attacker.example", http.StatusForbidden},
		{"GET", "/mcp", "", http.StatusMethodNotAllowed},
		{"GET", "/.well-known/oauth-authorization-server", "", http.StatusOK},
		{"GET", "/.well-known/oauth-protected-resource/mcp", "", http.StatusOK},
		{"GET", "/health", "", http.StatusOK},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if tc.method == "POST" {
			req.Header.Set("Content-Type", "application/json")
		}
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%s %s status=%d want=%d response=%s", tc.method, tc.path, rec.Code, tc.status, rec.Body.String())
		}
		if tc.path == "/mcp" && tc.status == http.StatusUnauthorized &&
			!strings.Contains(rec.Header().Get("WWW-Authenticate"), "resource_metadata") {
			t.Fatal("OAuth discovery missing in challenge")
		}
	}
}
func TestDefinitionsAndNoSecrets(t *testing.T) {
	s := newTestServer(t)
	s.cfg.GitHubToken = "TEST_SECRET_TOKEN"
	raw, err := json.Marshal(s.definitions())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), s.cfg.GitHubToken) {
		t.Fatal("GitHub token leaked into MCP tool schemas")
	}
	var defs []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &defs); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, d := range defs {
		found[d.Name] = true
	}
	for _, name := range []string{"exec", "read_file", "write_file", "list_dir", "github_status", "github_repositories", "github_branches", "github_file_read", "github_file_write"} {
		if !found[name] {
			t.Errorf("missing tool %s", name)
		}
	}
}
func TestStateInPrivateDir(t *testing.T) {
	s := newTestServer(t)
	if s.auth == nil {
		t.Fatal("OAuth no inicializado")
	}
	path := filepath.Join(s.cfg.DataDir, "state", "oauth.json")
	req := httptest.NewRequest("GET", "/health", nil)
	if _, err := s.readFile(path); err == nil {
		t.Fatal("el servidor debe impedir leer sus propias credenciales")
	}
	_ = req
}

func TestAllToolSchemasJSONSchemaRequiredArrays(t *testing.T) {
	s := newTestServer(t)
	raw, err := json.Marshal(s.definitions())
	if err != nil {
		t.Fatal(err)
	}
	var defs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &defs); err != nil {
		t.Fatal(err)
	}
	optionalCount := 0
	for _, item := range defs {
		var name string
		if err := json.Unmarshal(item["name"], &name); err != nil {
			t.Fatal(err)
		}
		var schema map[string]json.RawMessage
		if err := json.Unmarshal(item["inputSchema"], &schema); err != nil {
			t.Fatalf("%s schema: %v", name, err)
		}
		if string(schema["type"]) != `"object"` {
			t.Errorf("%s: inputSchema.type debe ser object", name)
		}
		var properties map[string]any
		if err := json.Unmarshal(schema["properties"], &properties); err != nil {
			t.Errorf("%s: properties: %v", name, err)
		}
		// JSON Schema 2020-12: required es opcional; si existe debe ser
		// array de cadenas únicas, nunca null (antes había required:null).
		required, ok := schema["required"]
		if !ok {
			optionalCount++
			continue
		}
		var fields []string
		if err := json.Unmarshal(required, &fields); err != nil || fields == nil {
			t.Errorf("%s: required debe ser una lista de cadenas, no %s: %v", name, required, err)
			continue
		}
		seen := make(map[string]bool)
		for _, field := range fields {
			if seen[field] {
				t.Errorf("%s: required duplicado %s", name, field)
			}
			seen[field] = true
			if _, present := properties[field]; !present {
				t.Errorf("%s: required refiere a propiedad ausente %s", name, field)
			}
		}
	}
	if optionalCount == 0 {
		t.Fatal("se esperaba herramientas sin argumentos obligatorios")
	}
}
