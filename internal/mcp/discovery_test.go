package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func issueAccessTokenForTest(t *testing.T, srv *Server, pinOverride ...string) string {
	t.Helper()
	handler := srv.Handler()
	regBody := `{"client_name":"test-discovery","redirect_uris":["https://client.example/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	reg := httptest.NewRecorder()
	handler.ServeHTTP(reg, httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(regBody)))
	if reg.Code != http.StatusCreated {
		t.Fatalf("DCR: HTTP %d %s", reg.Code, reg.Body.String())
	}
	var client struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(reg.Body.Bytes(), &client); err != nil || client.ClientID == "" {
		t.Fatalf("DCR inválido: %v", err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "state": {"ok"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"resource": {srv.public + "/mcp"}, "scope": {"root"}}
	begin := httptest.NewRecorder()
	handler.ServeHTTP(begin, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
	if begin.Code != 200 {
		t.Fatalf("authorize GET: HTTP %d %s", begin.Code, begin.Body.String())
	}
	cookies := begin.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("falta cookie CSRF")
	}
	field := `name="authorization_session" value="`
	idx := strings.Index(begin.Body.String(), field)
	if idx < 0 {
		t.Fatal("falta sesion OAuth")
	}
	part := begin.Body.String()[idx+len(field):]
	end := strings.Index(part, `"`)
	if end < 0 {
		t.Fatal("sesion OAuth malformada")
	}
	session := part[:end]
	pin := "LONG-TEST-PIN-ABC"
	if len(pinOverride) > 0 {
		pin = pinOverride[0]
	}
	form := url.Values{"authorization_session": {session}, "csrf": {cookies[0].Value}, "pin": {pin}, "decision": {"approve"}}
	consentReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	consentReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consentReq.AddCookie(cookies[0])
	approved := httptest.NewRecorder()
	handler.ServeHTTP(approved, consentReq)
	if approved.Code != 200 {
		t.Fatalf("authorize POST: HTTP %d %s", approved.Code, approved.Body.String())
	}
	href := `href="`
	i := strings.Index(approved.Body.String(), href)
	if i < 0 {
		t.Fatal("falta redirección OAuth")
	}
	raw := approved.Body.String()[i+len(href):]
	ending := strings.Index(raw, `"`)
	if ending < 0 {
		t.Fatal("URL OAuth malformada")
	}
	callback, err := url.Parse(html.UnescapeString(raw[:ending]))
	if err != nil {
		t.Fatal(err)
	}
	code := callback.Query().Get("code")
	if code == "" {
		t.Fatal("OAuth no emitió code")
	}
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ClientID},
		"redirect_uri": {"https://client.example/callback"}, "code_verifier": {verifier}, "resource": {srv.public + "/mcp"}}
	tokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResp := httptest.NewRecorder()
	handler.ServeHTTP(tokenResp, tokenReq)
	if tokenResp.Code != 200 {
		t.Fatalf("OAuth token: HTTP %d %s", tokenResp.Code, tokenResp.Body.String())
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tokenResp.Body.Bytes(), &token); err != nil || token.AccessToken == "" {
		t.Fatalf("token OAuth incorrecto: %v", err)
	}
	return token.AccessToken
}

func postMCPAuthenticated(t *testing.T, server *Server, token, method, version string, params map[string]any, name string) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 17, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
		if version == modernProtocol {
			req.Header.Set("Mcp-Method", method)
			if method == "tools/call" {
				req.Header.Set("Mcp-Name", name)
			}
		}
	}
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code == http.StatusUnauthorized {
		return resp.Code, nil // OAuth challenge no contiene JSON-RPC.
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON-RPC response: %v %s", err, resp.Body.String())
	}
	return resp.Code, result
}
func modernParams(additions map[string]any) map[string]any {
	params := map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": modernProtocol,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{}, "io.modelcontextprotocol/clientInfo": map[string]string{"name": "chatgpt-test", "version": "1"}}}
	for k, v := range additions {
		params[k] = v
	}
	return params
}
func TestOAuthThenActionDiscoveryModern(t *testing.T) {
	server := newTestServer(t)
	token := issueAccessTokenForTest(t, server)
	code, reply := postMCPAuthenticated(t, server, token, "server/discover", modernProtocol, modernParams(nil), "")
	if code != 200 {
		t.Fatalf("discovery HTTP %d: %+v", code, reply)
	}
	out, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("discovery no devolvió resultado: %+v", reply)
	}
	if out["resultType"] != "complete" || out["cacheScope"] != "private" || out["ttlMs"] == nil {
		t.Fatalf("metadata de discover inválida: %+v", out)
	}
	versions, ok := out["supportedVersions"].([]any)
	if !ok || len(versions) == 0 || versions[0] != modernProtocol {
		t.Fatalf("versiones soportadas inconsistentes: %+v", out["supportedVersions"])
	}
	meta, ok := out["_meta"].(map[string]any)
	if !ok || meta["io.modelcontextprotocol/serverInfo"] == nil {
		t.Fatalf("faltan metadatos del servidor: %+v", out)
	}
	caps, ok := out["capabilities"].(map[string]any)
	if !ok || caps["tools"] == nil {
		t.Fatalf("faltan capacidades de herramientas: %+v", out)
	}
	code, list := postMCPAuthenticated(t, server, token, "tools/list", modernProtocol, modernParams(nil), "")
	if code != 200 {
		t.Fatalf("tools/list HTTP %d: %+v", code, list)
	}
	toolsResult, ok := list["result"].(map[string]any)
	if !ok || toolsResult["resultType"] != "complete" || toolsResult["cacheScope"] != "private" || toolsResult["ttlMs"] == nil {
		t.Fatalf("tools/list no conforme: %+v", list)
	}
	tools, ok := toolsResult["tools"].([]any)
	if !ok || len(tools) < 5 {
		t.Fatalf("no se descubrieron herramientas: %+v", list)
	}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok || tool["name"] == "" || tool["inputSchema"] == nil {
			t.Fatalf("esquema herramienta incorrecto: %+v", item)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Fatalf("schema inválido en %s: %+v", tool["name"], tool["inputSchema"])
		}
		// Validación sobre el JSON de la respuesta HTTP real; JSON null
		// no es una lista válida en la palabra clave JSON Schema required.
		if rawRequired, exists := schema["required"]; exists {
			required, ok := rawRequired.([]any)
			if !ok {
				t.Fatalf("tool %s: required no es array: %#v", tool["name"], rawRequired)
			}
			for _, value := range required {
				if _, ok := value.(string); !ok {
					t.Fatalf("tool %s: required contiene no-string", tool["name"])
				}
			}
		}
	}
	code, called := postMCPAuthenticated(t, server, token, "tools/call", modernProtocol, modernParams(map[string]any{"name": "environment_info", "arguments": map[string]any{}}), "environment_info")
	if code != 200 {
		t.Fatalf("tools/call HTTP %d: %+v", code, called)
	}
	value, ok := called["result"].(map[string]any)
	if !ok || value["resultType"] != "complete" || value["isError"] != false || value["content"] == nil {
		t.Fatalf("tools/call no conforme: %+v", called)
	}
}
func TestLegacyDiscoveryPreserved(t *testing.T) {
	server := newTestServer(t)
	token := issueAccessTokenForTest(t, server)
	code, init := postMCPAuthenticated(t, server, token, "initialize", protocol, map[string]any{"protocolVersion": protocol}, "")
	if code != 200 {
		t.Fatalf("initialize legacy HTTP %d: %+v", code, init)
	}
	v := init["result"].(map[string]any)
	if v["protocolVersion"] != protocol {
		t.Fatalf("legacy negotiate: %+v", v)
	}
	code, list := postMCPAuthenticated(t, server, token, "tools/list", protocol, map[string]any{}, "")
	if code != 200 || list["result"] == nil {
		t.Fatalf("tools/list legacy: %d %+v", code, list)
	}
}
func TestModernRejectsHeaderMismatchAndNoBearer(t *testing.T) {
	s := newTestServer(t)
	// El discovery jamás se debe poder consultar sin autenticación.
	code, _ := postMCPAuthenticated(t, s, "bad-token", "server/discover", modernProtocol, modernParams(nil), "")
	if code != http.StatusUnauthorized {
		t.Fatalf("descubrimiento sin autorización permitido: %d", code)
	}
	token := issueAccessTokenForTest(t, s)
	// Se envía un header 2026 y un body que afirma 2025.
	code, got := postMCPAuthenticated(t, s, token, "tools/list", modernProtocol, map[string]any{
		"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": protocol,
			"io.modelcontextprotocol/clientCapabilities": map[string]any{}}}, "")
	if code != 400 {
		t.Fatalf("no rechazó discrepancia: %d %+v", code, got)
	}
	errInfo, ok := got["error"].(map[string]any)
	if !ok || errInfo["code"] != float64(-32020) {
		t.Fatalf("no devolvió HeaderMismatch: %+v", got)
	}
}

func TestModernRequestHeaderAndMethodValidation(t *testing.T) {
	s := newTestServer(t)
	token := issueAccessTokenForTest(t, s)
	for _, tc := range []struct {
		name, version, method, mirroredMethod, mirroredName string
		params                                              map[string]any
		wantCode                                            int
	}{
		{"Mcp-Method vacío", modernProtocol, "server/discover", "", "", modernParams(nil), 400},
		{"Mcp-Method distinto", modernProtocol, "tools/list", "tools/call", "", modernParams(nil), 400},
		{"Mcp-Name ausente", modernProtocol, "tools/call", "tools/call", "", modernParams(map[string]any{"name": "environment_info", "arguments": map[string]any{}}), 400},
		{"versión no soportada", "2026-10-09", "tools/list", "tools/list", "", modernParams(nil), 400},
		{"legacy 2025-03-26", "2025-03-26", "tools/list", "", "", map[string]any{}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "req-1", "method": tc.method, "params": tc.params})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(payload)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("MCP-Protocol-Version", tc.version)
			if tc.mirroredMethod != "" {
				req.Header.Set("Mcp-Method", tc.mirroredMethod)
			}
			if tc.mirroredName != "" {
				req.Header.Set("Mcp-Name", tc.mirroredName)
			}
			out := httptest.NewRecorder()
			s.Handler().ServeHTTP(out, req)
			if out.Code != tc.wantCode {
				t.Fatalf("HTTP %d en vez de %d, body=%s", out.Code, tc.wantCode, out.Body.String())
			}
		})
	}
}

func TestActionDiscoveryWithBearerAndRewrittenOrigin(t *testing.T) {
	srv := newTestServer(t)
	token := issueAccessTokenForTest(t, srv)
	for _, origin := range []string{"", "https://chatgpt.com", "https://chat.openai.com", "https://www.chatgpt.com", "https://sample.trycloudflare.com"} {
		for _, protocolVersion := range []string{protocol, modernProtocol} {
			t.Run(origin+"_"+protocolVersion, func(t *testing.T) {
				for _, method := range []string{"server/discover", "tools/list"} {
					if method == "server/discover" && protocolVersion != modernProtocol {
						continue
					}
					params := map[string]any{}
					if protocolVersion == modernProtocol {
						params = modernParams(nil)
					}
					body, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 17, "method": method, "params": params})
					if e != nil {
						t.Fatal(e)
					}
					req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Accept", "application/json, text/event-stream")
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("Origin", origin)
					req.Header.Set("MCP-Protocol-Version", protocolVersion)
					if protocolVersion == modernProtocol {
						req.Header.Set("Mcp-Method", method)
					}
					response := httptest.NewRecorder()
					srv.Handler().ServeHTTP(response, req)
					if response.Code != 200 {
						t.Fatalf("origin=%q method=%s HTTP%d: %s", origin, method, response.Code, response.Body.String())
					}
					var decoded struct {
						Result map[string]json.RawMessage `json:"result"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
						t.Fatal(err)
					}
					key := "tools"
					if method == "server/discover" {
						key = "supportedVersions"
					}
					if len(decoded.Result[key]) == 0 {
						t.Fatalf("falta %s en %s", key, response.Body.String())
					}
				}
			})
		}
	}
}
func TestActionDiscoveryRejectsMissingOrBadBearerRegardlessOfOrigin(t *testing.T) {
	srv := newTestServer(t)
	for _, origin := range []string{"", "https://chatgpt.com", "https://sample.trycloudflare.com"} {
		for _, authorization := range []string{"", "Bearer wrong-token"} {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", origin)
			if authorization != "" {
				req.Header.Set("Authorization", authorization)
			}
			resp := httptest.NewRecorder()
			srv.Handler().ServeHTTP(resp, req)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("origin=%q token=%q status=%d", origin, authorization, resp.Code)
			}
			if strings.Contains(resp.Body.String(), "tools") {
				t.Fatal("herramientas reveladas sin Bearer")
			}
		}
	}
}
