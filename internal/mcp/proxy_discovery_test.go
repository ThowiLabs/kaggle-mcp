package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

func TestOriginAllowlistRejectsForgedOriginsEvenWithOAuthBearer(t *testing.T) {
	srv := newTestServer(t)
	token := issueAccessTokenForTest(t, srv)
	for _, origin := range []string{
		"null", "https://proxy.gradio.live", "https://unrelated.example",
		"https://chatgpt.com.attacker.test", "http://chatgpt.com",
		"https://chatgpt.com/path", "https://chatgpt.com?x=1",
		"https://chatgpt.com@evil.example", "https://chatgpt.com:8443",
	} {
		t.Run(origin, func(t *testing.T) {
			payload := `{"jsonrpc":"2.0","id":17,"method":"tools/list","params":{}}`
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(payload))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("Origin %q no rechazado: %d %s", origin, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRealReverseProxyActionDiscoveryWithChatGPTOrigin(t *testing.T) {
	srv := newTestServer(t)
	token := issueAccessTokenForTest(t, srv)
	origin := httptest.NewServer(srv.Handler())
	defer origin.Close()
	target, e := url.Parse(origin.URL)
	if e != nil {
		t.Fatal(e)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	gateway := httptest.NewServer(proxy)
	defer gateway.Close()
	for _, tc := range []struct{ method, version string }{
		{"server/discover", modernProtocol}, {"tools/list", modernProtocol},
		{"tools/list", protocol},
	} {
		t.Run(tc.method+"_"+tc.version, func(t *testing.T) {
			params := map[string]any{}
			if tc.version == modernProtocol {
				params = modernParams(nil)
			}
			body, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "req-123", "method": tc.method, "params": params})
			if e != nil {
				t.Fatal(e)
			}
			req, e := http.NewRequest("POST", gateway.URL+"/mcp", strings.NewReader(string(body)))
			if e != nil {
				t.Fatal(e)
			}
			req.Header.Set("Origin", "https://chatgpt.com")
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("MCP-Protocol-Version", tc.version)
			if tc.version == modernProtocol {
				req.Header.Set("Mcp-Method", tc.method)
			}
			resp, e := gateway.Client().Do(req)
			if e != nil {
				t.Fatal(e)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("HTTP %d detrás de proxy", resp.StatusCode)
			}
			var result struct {
				Result struct {
					Tools             []json.RawMessage `json:"tools"`
					SupportedVersions []string          `json:"supportedVersions"`
					ResultType        string            `json:"resultType"`
				} `json:"result"`
			}
			if e = json.NewDecoder(resp.Body).Decode(&result); e != nil {
				t.Fatal(e)
			}
			if tc.method == "tools/list" && len(result.Result.Tools) < 35 {
				t.Fatalf("descubrimiento parcial: %d herramientas", len(result.Result.Tools))
			}
			if tc.method == "server/discover" && len(result.Result.SupportedVersions) == 0 {
				t.Fatal("sin versiones")
			}
			if tc.version == modernProtocol && result.Result.ResultType != "complete" {
				t.Fatal("resultType incorrecto")
			}
		})
	}
}

func TestUnsupportedProtocolReturnsNegotiableVersions(t *testing.T) {
	srv := newTestServer(t)
	token := issueAccessTokenForTest(t, srv)
	body := `{"jsonrpc":"2.0","id":17,"method":"server/discover","params":{}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2026-10-09")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("esperado 400 got %d", rec.Code)
	}
	var resp struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Supported []string `json:"supported"`
				Requested string   `json:"requested"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error.Code != -32022 || resp.Error.Data.Requested != "2026-10-09" || len(resp.Error.Data.Supported) < 2 {
		t.Fatalf("faltan datos de negociación: %s", rec.Body.String())
	}
}

func TestAllMCPToolSchemasWellFormedAndNotDuplicated(t *testing.T) {
	srv := newTestServer(t)
	names := make(map[string]bool)
	raw, e := json.Marshal(srv.definitions())
	if e != nil {
		t.Fatal(e)
	}
	var tools []map[string]any
	if e := json.Unmarshal(raw, &tools); e != nil {
		t.Fatal(e)
	}
	for _, tool := range tools {
		name, ok := tool["name"].(string)
		if !ok || name == "" || len(name) > 128 {
			t.Fatalf("invalid tool name: %#v", tool["name"])
		}
		if names[name] {
			t.Fatalf("duplicate tool %s", name)
		}
		names[name] = true
		desc, ok := tool["description"].(string)
		if !ok || desc == "" {
			t.Fatalf("missing tool description for %s", name)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Fatalf("%s schema must be object", name)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing properties object", name)
		}
		if schema["additionalProperties"] != false {
			t.Fatalf("%s unexpected additionalProperties", name)
		}
		for prop, definition := range props {
			shape, ok := definition.(map[string]any)
			if !ok {
				t.Fatalf("%s.%s has invalid schema", name, prop)
			}
			kind, ok := shape["type"].(string)
			if !ok || (kind != "string" && kind != "integer" && kind != "boolean" && kind != "array") {
				t.Fatalf("%s.%s invalid type %v", name, prop, shape["type"])
			}
			if kind == "array" {
				items, valid := shape["items"].(map[string]any)
				if !valid || items["type"] != "string" {
					t.Fatalf("%s.%s array requires string items", name, prop)
				}
			}
		}
		if required, exists := schema["required"]; exists {
			fields, ok := required.([]any)
			if !ok {
				t.Fatalf("%s required not list", name)
			}
			for _, v := range fields {
				field, ok := v.(string)
				if !ok {
					t.Fatalf("%s requires non-string field", name)
				}
				if _, found := props[field]; !found {
					t.Fatalf("%s requires missing field %s", name, field)
				}
			}
		}
	}
	if len(names) < 40 {
		t.Fatalf("missing tools: got %d", len(names))
	}
	if len(raw) > 1<<20 {
		t.Fatalf("tools/list too large: %d bytes", len(raw))
	}
}
