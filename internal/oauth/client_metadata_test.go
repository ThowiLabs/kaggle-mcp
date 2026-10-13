package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func metadataHTTPClient(t *testing.T, clientID, redirectURI string) *http.Client {
	t.Helper()
	body := `{"client_id":"` + clientID + `","client_name":"Modern MCP Client","redirect_uris":["` + redirectURI + `"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != clientID {
			t.Fatalf("unexpected metadata request: %s %s", r.Method, r.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}
}

func TestResolveClientMetadataDocument(t *testing.T) {
	clientID := "https://modern-client.example/oauth/client-metadata.json"
	redirectURI := "https://modern-client.example/connector/oauth/callback"
	client, err := resolveClientMetadata(context.Background(), metadataHTTPClient(t, clientID, redirectURI), clientID)
	if err != nil {
		t.Fatal(err)
	}
	if client.ID != clientID || client.Name != "Modern MCP Client" || len(client.RedirectURIs) != 1 || client.RedirectURIs[0] != redirectURI {
		t.Fatalf("unexpected resolved client: %#v", client)
	}
}

func TestResolveClientMetadataAcceptsSupportedTokenAuthIntersection(t *testing.T) {
	clientID := "https://chatgpt.com/oauth/test/client.json"
	redirectURI := "https://chatgpt.com/connector/oauth/test"
	body := `{"client_id":"` + clientID + `","client_name":"ChatGPT","redirect_uris":["` + redirectURI + `"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_methods_supported":["none","private_key_jwt"],"token_endpoint_auth_method":"private_key_jwt"}`
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	resolved, err := resolveClientMetadata(context.Background(), client, clientID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != clientID || resolved.Name != "ChatGPT" || len(resolved.RedirectURIs) != 1 || resolved.RedirectURIs[0] != redirectURI {
		t.Fatalf("unexpected resolved client: %#v", resolved)
	}
}

func TestResolveClientMetadataRejectsUnsupportedTokenAuthIntersection(t *testing.T) {
	clientID := "https://modern-client.example/oauth/client-metadata.json"
	body := `{"client_id":"` + clientID + `","client_name":"Confidential Client","redirect_uris":["https://modern-client.example/callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_methods_supported":["private_key_jwt"],"token_endpoint_auth_method":"private_key_jwt"}`
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	if _, err := resolveClientMetadata(context.Background(), client, clientID); err == nil || !strings.Contains(err.Error(), "does not support token endpoint authentication method none") {
		t.Fatalf("unsupported token auth methods error=%v", err)
	}
}

func TestResolveClientMetadataRejectsLegacyPrivateKeyJWTOnly(t *testing.T) {
	clientID := "https://modern-client.example/oauth/client-metadata.json"
	body := `{"client_id":"` + clientID + `","client_name":"Legacy Confidential Client","redirect_uris":["https://modern-client.example/callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"private_key_jwt"}`
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	if _, err := resolveClientMetadata(context.Background(), client, clientID); err == nil || !strings.Contains(err.Error(), "must use token_endpoint_auth_method none") {
		t.Fatalf("legacy private_key_jwt error=%v", err)
	}
}

func TestResolveClientMetadataRejectsOversizedDocument(t *testing.T) {
	clientID := "https://modern-client.example/oauth/client-metadata.json"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxClientMetadataBytes+1))),
			Request:    r,
		}, nil
	})}
	if _, err := resolveClientMetadata(context.Background(), client, clientID); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized metadata error=%v", err)
	}
}

func TestResolveClientMetadataRejectsMismatchedClientID(t *testing.T) {
	clientID := "https://modern-client.example/oauth/client-metadata.json"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"client_id":"https://attacker.example/client.json","client_name":"bad","redirect_uris":["https://attacker.example/callback"]}`)),
			Request:    r,
		}, nil
	})}
	if _, err := resolveClientMetadata(context.Background(), client, clientID); err == nil {
		t.Fatal("mismatched client_id metadata document accepted")
	}
}

func TestClientMetadataURLValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{"https://modern-client.example/oauth/client.json", true},
		{"https://modern-client.example/", true},
		{"https://modern-client.example/client.json?x=1", true},
		{"http://modern-client.example/oauth/client.json", false},
		{"https://modern-client.example", false},
		{" https://modern-client.example/client.json", false},
		{"https://user:pass@modern-client.example/client.json", false},
		{"https://modern-client.example/a/../client.json", false},
		{"https://modern-client.example/client.json#fragment", false},
	} {
		err := validateClientMetadataURL(tc.value)
		if (err == nil) != tc.ok {
			t.Fatalf("validateClientMetadataURL(%q) error=%v want ok=%v", tc.value, err, tc.ok)
		}
	}
}

func TestClientMetadataHTTPClientDoesNotFollowRedirects(t *testing.T) {
	client := newClientMetadataHTTPClient()
	req, err := http.NewRequest(http.MethodGet, "https://modern-client.example/other.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := http.NewRequest(http.MethodGet, "https://modern-client.example/client.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, []*http.Request{previous}); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy error=%v want http.ErrUseLastResponse", err)
	}
}

func TestClientMetadataSSRFAddressFilter(t *testing.T) {
	for _, value := range []string{
		"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.10",
		"192.31.196.1", "192.52.193.1", "192.175.48.1",
		"::1", "fc00::1", "100:0:0:1::1", "2001:db8::1", "2620:4f:8000::1", "3fff::1", "5f00::1",
	} {
		if publicClientMetadataIP(net.ParseIP(value)) {
			t.Fatalf("non-public metadata address %s accepted", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicClientMetadataIP(net.ParseIP(value)) {
			t.Fatalf("public metadata address %s rejected", value)
		}
	}
}

func TestNormalizeAuthorizationScopeAcceptsOfflineAccess(t *testing.T) {
	for _, value := range []string{"", "root", "root offline_access", "offline_access root"} {
		got, err := normalizeAuthorizationScope(value)
		if err != nil {
			t.Fatalf("normalizeAuthorizationScope(%q): %v", value, err)
		}
		if got != rootScope {
			t.Fatalf("normalizeAuthorizationScope(%q)=%q want %q", value, got, rootScope)
		}
	}
	for _, value := range []string{"offline_access", "root profile"} {
		if _, err := normalizeAuthorizationScope(value); err == nil {
			t.Fatalf("unsupported scope %q accepted", value)
		}
	}
}

func TestOAuthMetadataAdvertisesCIMDAndScopedDiscovery(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/mcp"} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("metadata %s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, `"client_id_metadata_document_supported":true`) ||
			!strings.Contains(body, `"registration_endpoint":"https://example.test/oauth/register"`) ||
			!strings.Contains(body, `"offline_access"`) {
			t.Fatalf("metadata %s missing CIMD, DCR or offline access compatibility: %s", path, body)
		}
	}
}

func TestAuthorizeRejectsDifferentOwnerBeforeFetchingCIMD(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json"})
	if err != nil {
		t.Fatal(err)
	}
	s.store.mu.Lock()
	s.store.data.OwnerClientID = "existing-client"
	s.store.mu.Unlock()
	calls := 0
	s.clientMetadataHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("metadata fetch should not occur")
	})}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {"https://modern-client.example/oauth/client-metadata.json"},
		"redirect_uri":          {"https://modern-client.example/connector/oauth/callback"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {"https://example.test/mcp"},
		"scope":                 {"root"},
	}
	if _, err := s.parseAuthorizeRequest(context.Background(), q); err == nil || !strings.Contains(err.Error(), "already linked") {
		t.Fatalf("owner conflict error=%v", err)
	}
	if calls != 0 {
		t.Fatalf("client metadata was fetched despite owner conflict: calls=%d", calls)
	}
}

func TestAuthorizeResolvesCIMDClientWithoutDynamicRegistration(t *testing.T) {
	s, err := New(Config{PublicURL: "https://example.test", StateFile: t.TempDir() + "/state.json"})
	if err != nil {
		t.Fatal(err)
	}
	clientID := "https://modern-client.example/oauth/client-metadata.json"
	redirectURI := "https://modern-client.example/connector/oauth/callback"
	s.clientMetadataHTTPClient = metadataHTTPClient(t, clientID, redirectURI)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {"state-cimd"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {"https://example.test/mcp"},
		"scope":                 {"root offline_access"},
	}
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
	if get.Code != http.StatusOK {
		t.Fatalf("CIMD authorize status=%d body=%s", get.Code, get.Body.String())
	}
	if !strings.Contains(get.Body.String(), "Autorizar acceso root") ||
		!strings.Contains(get.Body.String(), "Modern MCP Client") ||
		!strings.Contains(get.Body.String(), "modern-client.example") {
		t.Fatalf("CIMD authorization identity missing: %s", get.Body.String())
	}
	cookies := get.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("CIMD authorization CSRF cookie missing")
	}
	sessionID := hiddenFormValue(t, get.Body.String(), "authorization_session")
	approve := url.Values{
		"authorization_session": {sessionID},
		"csrf":                  {cookies[0].Value},
		"decision":              {"approve"},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(approve.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookies[0])
	post := httptest.NewRecorder()
	mux.ServeHTTP(post, postReq)
	if post.Code != http.StatusOK {
		t.Fatalf("CIMD approve status=%d body=%s", post.Code, post.Body.String())
	}
	code := handoffURL(t, post.Body.String()).Query().Get("code")
	if code == "" {
		t.Fatal("CIMD authorization code missing")
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"resource":      {"https://example.test/mcp"},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	token := httptest.NewRecorder()
	mux.ServeHTTP(token, tokenReq)
	if token.Code != http.StatusOK {
		t.Fatalf("CIMD token exchange status=%d body=%s", token.Code, token.Body.String())
	}

	s.store.mu.Lock()
	registered := len(s.store.data.Clients)
	owner := s.store.data.OwnerClientID
	s.store.mu.Unlock()
	if registered != 0 {
		t.Fatalf("CIMD client was persisted as DCR client: %d", registered)
	}
	if owner != clientID {
		t.Fatalf("CIMD owner=%q want=%q", owner, clientID)
	}
}
