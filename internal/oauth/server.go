package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ThowiLabs/kagssh-go/internal/pinauth"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	rootScope          = "root"
	offlineAccessScope = "offline_access"
)

type grantContextKey struct{}

// AccessGrant describes the tenant sandbox bound to an accepted OAuth bearer token.
type AccessGrant struct {
	EnvironmentID string
	OwnerUserID   string
	SandboxID     string
	Kind          string
	Permanent     bool
	ExpiresAt     time.Time
}

// AccessGrantFromRequest returns the sandbox grant attached by ValidateBearer.
func AccessGrantFromRequest(r *http.Request) (AccessGrant, bool) {
	if r == nil {
		return AccessGrant{}, false
	}
	grant, ok := r.Context().Value(grantContextKey{}).(AccessGrant)
	if !ok || grant.OwnerUserID == "" {
		return AccessGrant{}, false
	}
	return grant, grant.Kind == EnvironmentKindMaster || grant.SandboxID != ""
}

type Config struct {
	PublicURL                string
	StateFile                string
	AccessTTL                time.Duration
	RefreshTTL               time.Duration
	CodeTTL                  time.Duration
	SessionTTL               time.Duration
	RequireClientCertificate bool
	ClientCertificateDNSName string
	AccessPIN                string
	PINState                 *pinauth.State
	PINMaxAttempts           int
	PINWindow                time.Duration
	PINLockout               time.Duration
	MultiTenant              bool
	RequireSandbox           bool
	AgentMode                bool
}

type Server struct {
	publicURL                string
	resource                 string
	store                    *Store
	accessTTL                time.Duration
	refreshTTL               time.Duration
	codeTTL                  time.Duration
	sessionTTL               time.Duration
	requireClientCertificate bool
	clientCertificateDNSName string
	pinState                 *pinauth.State
	pinLimiter               *pinLimiter
	clientMetadataHTTPClient *http.Client
	multiTenant              bool
	requireSandbox           bool
	agentMode                bool
}

func New(cfg Config) (*Server, error) {
	base := strings.TrimRight(cfg.PublicURL, "/")
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("public URL must be absolute")
	}
	if u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, errors.New("public URL must use https outside localhost")
	}
	store, err := OpenStore(cfg.StateFile)
	if err != nil {
		return nil, err
	}
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = 365 * 24 * time.Hour
	}
	if cfg.CodeTTL <= 0 {
		cfg.CodeTTL = 5 * time.Minute
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 15 * time.Minute
	}
	now := time.Now().UTC()
	resource := base + "/mcp"
	if !cfg.MultiTenant {
		if released, err := store.ReleaseStaleOwner(now); err != nil {
			return nil, fmt.Errorf("repair legacy oauth owner: %w", err)
		} else if released {
			slog.Warn("released stale OAuth owner left by an incomplete authorization")
		}
		if reset, err := store.ResetOwnerIfResourceChanged(resource, now); err != nil {
			return nil, fmt.Errorf("reset OAuth owner for changed resource: %w", err)
		} else if reset {
			slog.Warn("reset OAuth owner because the public resource changed")
		}
	}
	pinState := cfg.PINState
	if pinState == nil && cfg.AccessPIN != "" {
		pinState, err = pinauth.New(cfg.AccessPIN)
		if err != nil {
			return nil, err
		}
	}
	return &Server{
		publicURL:                base,
		resource:                 resource,
		store:                    store,
		accessTTL:                cfg.AccessTTL,
		refreshTTL:               cfg.RefreshTTL,
		codeTTL:                  cfg.CodeTTL,
		sessionTTL:               cfg.SessionTTL,
		requireClientCertificate: cfg.RequireClientCertificate,
		clientCertificateDNSName: strings.TrimSpace(cfg.ClientCertificateDNSName),
		pinState:                 pinState,
		pinLimiter:               newPINLimiter(cfg.PINMaxAttempts, cfg.PINWindow, cfg.PINLockout),
		clientMetadataHTTPClient: newClientMetadataHTTPClient(),
		multiTenant:              cfg.MultiTenant,
		requireSandbox:           cfg.RequireSandbox,
		agentMode:                cfg.AgentMode,
	}, nil
}

func (s *Server) Resource() string { return s.resource }

func (s *Server) IsLinked() bool {
	if !s.multiTenant {
		return s.store.IsLinked(s.resource, time.Now())
	}
	now := time.Now().UTC()
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	for _, tok := range s.store.data.AccessTokens {
		validEnvironment := s.validateEnvironmentGrantLocked(tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now)
		if tok.Resource == s.resource && now.Before(tok.ExpiresAt) && validEnvironment == nil {
			return true
		}
	}
	for _, tok := range s.store.data.RefreshTokens {
		validEnvironment := s.validateEnvironmentGrantLocked(tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now)
		if tok.Resource == s.resource && now.Before(tok.ExpiresAt) && validEnvironment == nil {
			return true
		}
	}
	return false
}

func (s *Server) requiresPIN() bool {
	return s.requireSandbox || s.pinState != nil || s.HasEnvironments()
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleProtectedResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.handleProtectedResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleAuthorizationServerMetadata)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server/mcp", s.handleAuthorizationServerMetadata)
	mux.HandleFunc("POST /oauth/register", s.handleRegister)
	mux.HandleFunc("GET /oauth/authorize", s.handleAuthorizeGET)
	mux.HandleFunc("POST /oauth/authorize", s.handleAuthorizePOST)
	mux.HandleFunc("POST /oauth/token", s.handleToken)
}

func (s *Server) ResetOwner() error { return s.store.ResetOwner() }

func (s *Server) Challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", s.WWWAuthenticate("", ""))
	w.WriteHeader(http.StatusUnauthorized)
}

func (s *Server) WWWAuthenticate(errorCode, description string) string {
	meta := s.publicURL + "/.well-known/oauth-protected-resource/mcp"
	value := fmt.Sprintf(`Bearer resource_metadata=%q, scope=%q`, meta, rootScope)
	if errorCode != "" {
		value += fmt.Sprintf(`, error=%q`, errorCode)
	}
	if description != "" {
		value += fmt.Sprintf(`, error_description=%q`, description)
	}
	return value
}

func (s *Server) ValidateBearer(r *http.Request) error {
	reject := func(reason string, token string) error {
		slog.Debug("oauth bearer rejected", "request_id", requestID(r), "reason", reason, "token", fingerprint(token))
		return errors.New(reason)
	}
	if s.requireClientCertificate && !hasVerifiedClientCertificate(r, s.clientCertificateDNSName) {
		return reject("verified client certificate is required", "")
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return reject("missing bearer token", "")
	}
	tokenValue := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if tokenValue == "" {
		return reject("empty bearer token", "")
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.cleanupLocked(time.Now())
	tok, ok := s.store.data.AccessTokens[tokenValue]
	if !ok || time.Now().After(tok.ExpiresAt) {
		return reject("invalid or expired bearer token", tokenValue)
	}
	if tok.Resource != s.resource || tok.Scope != rootScope {
		return reject("bearer token audience or scope mismatch", tokenValue)
	}
	if !s.multiTenant && (s.store.data.OwnerClientID == "" || tok.ClientID != s.store.data.OwnerClientID) {
		return reject("bearer token client is not authorized owner", tokenValue)
	}
	now := time.Now().UTC()
	grantErr := s.validateEnvironmentGrantLocked(tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now)
	if grantErr != nil {
		return reject(grantErr.Error(), tokenValue)
	}
	if tok.EnvironmentID != "" {
		env, ok := s.store.data.Environments[tok.EnvironmentID]
		if !ok {
			return reject(ErrEnvironmentRevoked.Error(), tokenValue)
		}
		grant := AccessGrant{EnvironmentID: env.ID, OwnerUserID: env.OwnerUserID, SandboxID: env.SandboxID, Kind: environmentKind(env), Permanent: env.Permanent, ExpiresAt: env.ExpiresAt}
		*r = *r.WithContext(context.WithValue(r.Context(), grantContextKey{}, grant))
	}
	slog.Debug("oauth bearer accepted", "request_id", requestID(r), "token", fingerprint(tokenValue), "client", fingerprint(tok.ClientID), "scope", tok.Scope, "environment", fingerprint(tok.EnvironmentID))
	return nil
}

func (s *Server) handleProtectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.resource,
		"resource_name":            "KagMCP",
		"resource_name#en":         "KagMCP",
		"authorization_servers":    []string{s.publicURL},
		"scopes_supported":         []string{rootScope},
		"bearer_methods_supported": []string{"header"},
	})
}

func (s *Server) handleAuthorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.publicURL,
		"authorization_endpoint":                s.publicURL + "/oauth/authorize",
		"token_endpoint":                        s.publicURL + "/oauth/token",
		"registration_endpoint":                 s.publicURL + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"client_id_metadata_document_supported": true,
		"scopes_supported":                      []string{rootScope, offlineAccessScope},
	})
}

type registrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.requireClientCertificate && !hasVerifiedClientCertificate(r, s.clientCertificateDNSName) {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "verified client certificate is required")
		return
	}
	var req registrationRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	if err := dec.Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid registration payload")
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "at least one redirect URI is required")
		return
	}
	for _, raw := range req.RedirectURIs {
		if !validRedirectURI(raw) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect URI must be https or localhost")
			return
		}
	}
	if req.TokenEndpointAuthMethod != "" && req.TokenEndpointAuthMethod != "none" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "only public clients are supported")
		return
	}
	if len(req.GrantTypes) > 0 && !slices.Contains(req.GrantTypes, "authorization_code") {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "authorization_code grant is required")
		return
	}
	if len(req.ResponseTypes) > 0 && !slices.Contains(req.ResponseTypes, "code") {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "code response type is required")
		return
	}

	clientID, err := randomToken(24)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not create client")
		return
	}
	client := Client{ID: clientID, Name: req.ClientName, RedirectURIs: req.RedirectURIs, CreatedAt: time.Now().UTC()}

	s.store.mu.Lock()
	s.store.data.Clients[clientID] = client
	err = s.store.saveLocked()
	s.store.mu.Unlock()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not persist client")
		return
	}

	redirectHosts := make([]string, 0, len(client.RedirectURIs))
	redirects := make([]string, 0, len(client.RedirectURIs))
	for _, redirectURI := range client.RedirectURIs {
		redirectHosts = append(redirectHosts, redirectHost(redirectURI))
		redirects = append(redirects, fingerprint(redirectURI))
	}
	slog.Debug("oauth client registered", "request_id", requestID(r), "client", fingerprint(clientID), "client_name", client.Name, "redirect_hosts", redirectHosts, "redirects", redirects, "grant_types", req.GrantTypes, "response_types", req.ResponseTypes, "token_auth_method", req.TokenEndpointAuthMethod)

	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

type authorizeRequest struct {
	ClientID      string
	ClientName    string
	RedirectURI   string
	State         string
	CodeChallenge string
	Resource      string
	Scope         string
}

func (s *Server) parseAuthorizeRequest(ctx context.Context, values url.Values) (authorizeRequest, error) {
	req := authorizeRequest{
		ClientID:      values.Get("client_id"),
		RedirectURI:   values.Get("redirect_uri"),
		State:         values.Get("state"),
		CodeChallenge: values.Get("code_challenge"),
		Resource:      values.Get("resource"),
		Scope:         values.Get("scope"),
	}
	if values.Get("response_type") != "code" {
		return req, errors.New("response_type must be code")
	}
	if values.Get("code_challenge_method") != "S256" || req.CodeChallenge == "" {
		return req, errors.New("PKCE S256 is required")
	}
	if req.Resource != s.resource {
		return req, errors.New("resource does not match MCP endpoint")
	}
	normalizedScope, err := normalizeAuthorizationScope(req.Scope)
	if err != nil {
		return req, err
	}
	req.Scope = normalizedScope

	s.store.mu.Lock()
	owner := s.store.data.OwnerClientID
	s.store.mu.Unlock()
	if owner != "" && owner != req.ClientID {
		return req, errors.New("server is already linked to another client")
	}
	client, ok, err := s.resolveOAuthClient(ctx, req.ClientID)
	if err != nil {
		return req, fmt.Errorf("resolve OAuth client: %w", err)
	}
	if !ok {
		return req, errors.New("unknown client")
	}
	if !slices.Contains(client.RedirectURIs, req.RedirectURI) {
		return req, errors.New("redirect URI does not match registration")
	}
	req.ClientName = client.Name
	return req, nil
}

func normalizeAuthorizationScope(raw string) (string, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return rootScope, nil
	}
	hasRoot := false
	for _, scope := range fields {
		switch scope {
		case rootScope:
			hasRoot = true
		case offlineAccessScope:
			// Refresh tokens are always issued after user approval, so
			// offline_access is accepted but does not widen MCP privileges.
		default:
			return "", errors.New("unsupported scope")
		}
	}
	if !hasRoot {
		return "", errors.New("root scope is required")
	}
	return rootScope, nil
}

var authorizePage = template.Must(template.New("authorize").Parse(`<!doctype html>
<html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Autorizar acceso root</title><style>
body{font-family:system-ui,sans-serif;background:#111;color:#eee;display:grid;place-items:center;min-height:100vh;margin:0;padding:20px;box-sizing:border-box}.card{width:min(100%,620px);background:#1b1b1b;border:1px solid #333;border-radius:16px;padding:28px;box-shadow:0 20px 60px #0008;box-sizing:border-box}h1{margin-top:0}.warn{padding:14px;border-radius:10px;background:#3a2600;color:#ffd98a}.meta{font-size:.92rem;color:#aaa;overflow-wrap:anywhere}.error{padding:12px;border-radius:10px;background:#4a1616;color:#ffd0d0;margin:16px 0}.field{display:grid;gap:8px;margin-top:18px}.field input{width:100%;box-sizing:border-box;border:1px solid #444;background:#101010;color:#fff;border-radius:10px;padding:12px 14px;font:inherit}.actions{display:flex;gap:12px;margin-top:22px;flex-wrap:wrap}button{border:0;border-radius:10px;padding:12px 18px;font-weight:700;cursor:pointer}.approve{background:#eee;color:#111}.deny{background:#333;color:#eee}
</style></head><body><main class="card"><h1>{{if .AgentMode}}Autorizar agente directo{{else}}Autorizar acceso root{{end}}</h1><p>{{if .AgentMode}}El cliente solicita trabajar directamente sobre este equipo mediante el alcance <strong>root</strong>.{{else}}El cliente solicita control completo del host mediante el alcance <strong>root</strong>.{{end}}</p><p class="warn">Podrá ejecutar comandos y modificar archivos con los permisos del proceso Lilith. Autoriza únicamente si confías plenamente en el cliente.</p>{{if .ClientName}}<p class="meta">Cliente: {{.ClientName}}</p>{{end}}{{if .ClientIDHost}}<p class="meta">Origen del cliente: {{.ClientIDHost}}</p>{{end}}<p class="meta">Destino OAuth: {{.RedirectHost}}</p>{{if .Error}}<div class="error" role="alert">{{.Error}}</div>{{end}}<form method="post" action="/oauth/authorize">
<input type="hidden" name="authorization_session" value="{{.SessionID}}"><input type="hidden" name="csrf" value="{{.CSRF}}">{{if .PINEnabled}}<label class="field"><span>{{if .AgentMode}}PIN maestro{{else if .EnvironmentMode}}PIN temporal del entorno{{else}}PIN de acceso{{end}}</span><input type="password" name="pin" autocomplete="one-time-code" required autofocus maxlength="128"></label>{{if .AgentMode}}<p class="meta">Este PIN autoriza al agente directo para trabajar directamente sobre el host. Se administra desde Conexión MCP en la TUI.</p>{{else if .EnvironmentMode}}<p class="meta">El PIN selecciona el entorno y su ventana de acceso. Si caducó o fue revocado, genera uno nuevo en el dashboard local.</p>{{end}}{{end}}<div class="actions"><button class="approve" name="decision" value="approve">{{if .AgentMode}}Autorizar agente{{else}}Autorizar root{{end}}</button><button class="deny" name="decision" value="deny">Cancelar</button></div></form></main></body></html>`))

var authorizeCompletePage = template.Must(template.New("authorize-complete").Parse(`<!doctype html>
<html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Autorización completada</title><style>body{font-family:system-ui,sans-serif;background:#111;color:#eee;display:grid;place-items:center;min-height:100vh;margin:0;padding:20px;box-sizing:border-box}.card{width:min(100%,620px);background:#1b1b1b;border:1px solid #333;border-radius:16px;padding:28px;box-sizing:border-box}.ok{padding:14px;border-radius:10px;background:#12351f;color:#bdf5ca}</style></head><body><main class="card"><h1>Autorización completada</h1><p class="ok">El cliente MCP ya canjeó esta autorización. Puedes cerrar esta pestaña y volver al cliente.</p></main></body></html>`))

var oauthHandoffPage = template.Must(template.New("oauth-handoff").Parse(`<!doctype html>
<html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="0; url={{.Target}}"><title>Continuando autorización</title><style>
body{font-family:system-ui,sans-serif;background:#111;color:#eee;display:grid;place-items:center;min-height:100vh;margin:0;padding:20px;box-sizing:border-box}.card{width:min(100%,620px);background:#1b1b1b;border:1px solid #333;border-radius:16px;padding:28px;box-sizing:border-box}.ok{padding:14px;border-radius:10px;background:#12351f;color:#bdf5ca}.meta{color:#aaa;overflow-wrap:anywhere}a{color:#9ecbff}
</style></head><body><main class="card"><h1>Autorización aceptada</h1><p class="ok">Continuando con el cliente MCP…</p><p class="meta">Si no continúa automáticamente, usa el enlace siguiente.</p><p><a href="{{.Target}}" rel="noreferrer">Continuar autorización</a></p></main></body></html>`))

func (s *Server) handleAuthorizeGET(w http.ResponseWriter, r *http.Request) {
	req, err := s.parseAuthorizeRequest(r.Context(), r.URL.Query())
	if err != nil {
		slog.Debug("oauth authorize request rejected", "request_id", requestID(r), "reason", err.Error(), "client", fingerprint(r.URL.Query().Get("client_id")))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	csrf, err := randomToken(24)
	if err != nil {
		http.Error(w, "could not create authorization request", http.StatusInternalServerError)
		return
	}
	sessionID, err := randomToken(24)
	if err != nil {
		http.Error(w, "could not create authorization request", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	session := AuthorizationSession{
		ID: sessionID, CSRFHash: hashValue(csrf), ClientID: req.ClientID, ClientName: req.ClientName, RedirectURI: req.RedirectURI,
		State: req.State, CodeChallenge: req.CodeChallenge, Resource: req.Resource, Scope: req.Scope,
		ExpiresAt: now.Add(s.sessionTTL),
	}

	s.store.mu.Lock()
	s.cleanupLocked(now)
	s.store.data.AuthorizationSessions[sessionID] = session
	err = s.store.saveLocked()
	s.store.mu.Unlock()
	if err != nil {
		slog.Debug("oauth authorization session persistence failed", "request_id", requestID(r), "error", err)
		http.Error(w, "could not persist authorization request", http.StatusInternalServerError)
		return
	}

	secure := strings.HasPrefix(s.publicURL, "https://")
	http.SetCookie(w, &http.Cookie{Name: "kagmcp_csrf", Value: csrf, Path: "/oauth/authorize", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(s.sessionTTL.Seconds())})
	slog.Debug("oauth authorization session created", "request_id", requestID(r), "session", fingerprint(sessionID), "client", fingerprint(req.ClientID), "redirect_host", redirectHost(req.RedirectURI), "redirect", fingerprint(req.RedirectURI), "client_ip", clientIP(r), "expires_in", s.sessionTTL.String())
	s.renderAuthorizePage(w, session, csrf, "", http.StatusOK)
}

func (s *Server) handleAuthorizePOST(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		slog.Debug("oauth authorization form rejected", "request_id", requestID(r), "reason", "parse_form", "error", err)
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	sessionID := r.PostForm.Get("authorization_session")
	csrf := r.PostForm.Get("csrf")
	cookie, cookieErr := r.Cookie("kagmcp_csrf")
	if sessionID == "" || csrf == "" || cookieErr != nil || cookie.Value == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(csrf)) != 1 {
		slog.Debug("oauth authorization session rejected", "request_id", requestID(r), "session", fingerprint(sessionID), "reason", "csrf_cookie_mismatch", "has_cookie", cookieErr == nil, "client_ip", clientIP(r))
		http.Error(w, "invalid authorization session; restart the connection flow", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	s.store.mu.Lock()
	s.cleanupLocked(now)
	session, ok := s.store.data.AuthorizationSessions[sessionID]
	s.store.mu.Unlock()
	if !ok || now.After(session.ExpiresAt) {
		slog.Debug("oauth authorization session rejected", "request_id", requestID(r), "session", fingerprint(sessionID), "reason", "session_missing_or_expired", "client_ip", clientIP(r))
		http.Error(w, "authorization session expired; restart the connection flow", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(session.CSRFHash), []byte(hashValue(csrf))) != 1 {
		slog.Debug("oauth authorization session rejected", "request_id", requestID(r), "session", fingerprint(sessionID), "reason", "csrf_state_mismatch", "client_ip", clientIP(r))
		http.Error(w, "invalid authorization session; restart the connection flow", http.StatusBadRequest)
		return
	}

	if r.PostForm.Get("decision") != "approve" {
		s.clearCSRFCookie(w)
		slog.Debug("oauth authorization denied", "request_id", requestID(r), "session", fingerprint(sessionID), "client", fingerprint(session.ClientID), "client_ip", clientIP(r))
		redirectOAuth(w, r, session.RedirectURI, session.State, "", "access_denied")
		return
	}

	if s.requiresPIN() {
		ip := clientIP(r)
		if blocked, retry := s.pinLimiter.blocked(ip, time.Now()); blocked {
			slog.Debug("oauth PIN blocked", "request_id", requestID(r), "session", fingerprint(sessionID), "client_ip", ip, "retry_after", retry.Round(time.Second).String())
			s.writeRateLimited(w, retry)
			return
		}
		providedPIN := r.PostForm.Get("pin")
		if s.requireSandbox || s.HasEnvironments() {
			env, authErr := s.authenticateEnvironmentPIN(providedPIN, now)
			if authErr != nil {
				if blocked, retry := s.pinLimiter.failure(ip, time.Now()); blocked {
					slog.Debug("oauth environment PIN lockout started", "request_id", requestID(r), "session", fingerprint(sessionID), "client_ip", ip, "retry_after", retry.Round(time.Second).String())
					s.writeRateLimited(w, retry)
					return
				}
				slog.Debug("oauth environment PIN rejected", "request_id", requestID(r), "session", fingerprint(sessionID), "client_ip", ip, "reason", authErr.Error())
				s.renderAuthorizePage(w, session, csrf, authErr.Error(), http.StatusUnauthorized)
				return
			}
			session.EnvironmentID = env.ID
			session.EnvironmentRev = env.Revision
			session.EnvironmentUntil = env.ExpiresAt
		} else {
			if s.pinState == nil || !s.pinState.Verify(providedPIN) {
				if blocked, retry := s.pinLimiter.failure(ip, time.Now()); blocked {
					slog.Debug("oauth PIN lockout started", "request_id", requestID(r), "session", fingerprint(sessionID), "client_ip", ip, "retry_after", retry.Round(time.Second).String())
					s.writeRateLimited(w, retry)
					return
				}
				slog.Debug("oauth PIN rejected", "request_id", requestID(r), "session", fingerprint(sessionID), "client_ip", ip)
				s.renderAuthorizePage(w, session, csrf, "PIN incorrecto.", http.StatusUnauthorized)
				return
			}
		}
		s.pinLimiter.success(ip)
		slog.Debug("oauth PIN accepted", "request_id", requestID(r), "session", fingerprint(sessionID), "client_ip", ip, "environment", fingerprint(session.EnvironmentID))
	}

	candidateCode, err := randomToken(32)
	if err != nil {
		http.Error(w, "could not create authorization code", http.StatusInternalServerError)
		return
	}

	s.store.mu.Lock()
	s.cleanupLocked(now)
	current, ok := s.store.data.AuthorizationSessions[sessionID]
	if !ok || now.After(current.ExpiresAt) {
		s.store.mu.Unlock()
		http.Error(w, "authorization session expired; restart the connection flow", http.StatusBadRequest)
		return
	}
	if session.EnvironmentID != "" {
		current.EnvironmentID = session.EnvironmentID
		current.EnvironmentRev = session.EnvironmentRev
		current.EnvironmentUntil = session.EnvironmentUntil
	}
	if err := s.validateEnvironmentGrantLocked(current.EnvironmentID, current.EnvironmentRev, current.EnvironmentUntil, now); err != nil {
		s.store.mu.Unlock()
		s.renderAuthorizePage(w, current, csrf, err.Error(), http.StatusUnauthorized)
		return
	}
	owner := s.store.data.OwnerClientID
	if !s.multiTenant && owner != "" && owner != current.ClientID {
		s.store.mu.Unlock()
		slog.Debug("oauth authorization rejected", "request_id", requestID(r), "session", fingerprint(sessionID), "reason", "different_owner", "owner", fingerprint(owner), "client", fingerprint(current.ClientID))
		http.Error(w, "server is already linked to another client", http.StatusForbidden)
		return
	}

	if current.ApprovedCode != "" {
		if _, exists := s.store.data.Codes[current.ApprovedCode]; exists {
			code := current.ApprovedCode
			s.store.mu.Unlock()
			slog.Debug("oauth callback handoff retried", "request_id", requestID(r), "session", fingerprint(sessionID), "client", fingerprint(current.ClientID), "redirect_host", redirectHost(current.RedirectURI), "redirect", fingerprint(current.RedirectURI))
			s.renderOAuthHandoff(w, r, current.RedirectURI, current.State, code, "")
			return
		}
		if !s.multiTenant && owner == current.ClientID {
			s.store.mu.Unlock()
			slog.Debug("oauth authorization already completed", "request_id", requestID(r), "session", fingerprint(sessionID), "client", fingerprint(current.ClientID))
			s.renderAuthorizeComplete(w)
			return
		}
		// El código anterior se perdió o fue rechazado antes de emitir tokens.
		// Se puede emitir uno nuevo dentro de la misma sesión y consentimiento.
		current.ApprovedCode = ""
	}

	code := candidateCode
	s.store.data.Codes[code] = AuthorizationCode{
		CodeChallenge: current.CodeChallenge, ClientID: current.ClientID, RedirectURI: current.RedirectURI,
		Resource: current.Resource, Scope: current.Scope, EnvironmentID: current.EnvironmentID, EnvironmentRev: current.EnvironmentRev,
		EnvironmentUntil: current.EnvironmentUntil, ExpiresAt: now.Add(s.codeTTL),
	}
	current.ApprovedCode = code
	s.store.data.AuthorizationSessions[sessionID] = current
	err = s.store.saveLocked()
	s.store.mu.Unlock()
	if err != nil {
		slog.Debug("oauth authorization persistence failed", "request_id", requestID(r), "session", fingerprint(sessionID), "error", err)
		http.Error(w, "could not persist authorization", http.StatusInternalServerError)
		return
	}

	slog.Debug("oauth authorization approved; handing off to client", "request_id", requestID(r), "session", fingerprint(sessionID), "client", fingerprint(current.ClientID), "code", fingerprint(code), "redirect_host", redirectHost(current.RedirectURI), "redirect", fingerprint(current.RedirectURI), "code_ttl", s.codeTTL.String())
	s.renderOAuthHandoff(w, r, current.RedirectURI, current.State, code, "")
}

func (s *Server) renderAuthorizePage(w http.ResponseWriter, session AuthorizationSession, csrf, errorMessage string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	_ = authorizePage.Execute(w, map[string]any{
		"SessionID": session.ID, "CSRF": csrf, "PINEnabled": s.requiresPIN(), "EnvironmentMode": s.requireSandbox || s.HasEnvironments(), "AgentMode": s.agentMode, "Error": errorMessage,
		"ClientName": session.ClientName, "ClientIDHost": clientIDHost(session.ClientID), "RedirectHost": redirectHost(session.RedirectURI),
	})
}

func (s *Server) renderAuthorizeComplete(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(http.StatusOK)
	_ = authorizeCompletePage.Execute(w, nil)
}

func (s *Server) clearCSRFCookie(w http.ResponseWriter) {
	secure := strings.HasPrefix(s.publicURL, "https://")
	http.SetCookie(w, &http.Cookie{Name: "kagmcp_csrf", Path: "/oauth/authorize", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (s *Server) writeRateLimited(w http.ResponseWriter, retry time.Duration) {
	seconds := int64(retry.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	http.Error(w, "demasiados intentos de PIN; intenta nuevamente más tarde", http.StatusTooManyRequests)
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if s.requireClientCertificate && !hasVerifiedClientCertificate(r, s.clientCertificateDNSName) {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "verified client certificate is required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		slog.Debug("oauth token request rejected", "request_id", requestID(r), "reason", "parse_form", "error", err)
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form")
		return
	}
	slog.Debug("oauth token request received", "request_id", requestID(r), "grant_type", r.Form.Get("grant_type"), "client", fingerprint(r.Form.Get("client_id")), "resource", r.Form.Get("resource"))
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.exchangeAuthorizationCode(w, r)
	case "refresh_token":
		s.exchangeRefreshToken(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant type is not supported")
	}
}

func (s *Server) exchangeAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	codeValue := r.Form.Get("code")
	clientID := r.Form.Get("client_id")
	redirectURI := r.Form.Get("redirect_uri")
	resource := r.Form.Get("resource")
	verifier := r.Form.Get("code_verifier")
	slog.Debug("oauth token exchange started", "request_id", requestID(r), "grant_type", "authorization_code", "client", fingerprint(clientID), "code", fingerprint(codeValue), "resource", resource, "redirect_host", redirectHost(redirectURI), "redirect", fingerprint(redirectURI))
	if codeValue == "" || clientID == "" || verifier == "" || resource == "" {
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", "missing_required_field", "has_code", codeValue != "", "has_client_id", clientID != "", "has_verifier", verifier != "", "has_resource", resource != "")
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, client_id, code_verifier and resource are required")
		return
	}

	now := time.Now().UTC()
	s.store.mu.Lock()
	s.cleanupLocked(now)
	code, ok := s.store.data.Codes[codeValue]
	if ok {
		delete(s.store.data.Codes, codeValue)
	}
	owner := s.store.data.OwnerClientID
	if !ok {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", "code_not_found", "client", fingerprint(clientID), "code", fingerprint(codeValue))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
		return
	}
	if now.After(code.ExpiresAt) {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", "code_expired", "client", fingerprint(clientID), "code", fingerprint(codeValue))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
		return
	}
	if code.ClientID != clientID {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", "client_mismatch", "expected_client", fingerprint(code.ClientID), "actual_client", fingerprint(clientID))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
		return
	}
	if code.RedirectURI != redirectURI {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", "redirect_uri_mismatch", "expected_host", redirectHost(code.RedirectURI), "actual_host", redirectHost(redirectURI), "expected_redirect", fingerprint(code.RedirectURI), "actual_redirect", fingerprint(redirectURI))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
		return
	}
	if code.Resource != resource {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", "resource_mismatch", "expected_resource", code.Resource, "actual_resource", resource)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
		return
	}
	if !s.multiTenant && owner != "" && owner != clientID {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", "different_owner", "owner", fingerprint(owner), "client", fingerprint(clientID))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
		return
	}
	if err := s.validateEnvironmentGrantLocked(code.EnvironmentID, code.EnvironmentRev, code.EnvironmentUntil, now); err != nil {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", err.Error(), "environment", fingerprint(code.EnvironmentID))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}
	if !verifyPKCE(verifier, code.CodeChallenge) {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth token exchange rejected", "request_id", requestID(r), "reason", "pkce_verification_failed", "client", fingerprint(clientID), "code", fingerprint(codeValue))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}

	access, refresh, err := s.issueTokensLocked(clientID, code.Resource, code.Scope, code.EnvironmentID, code.EnvironmentRev, code.EnvironmentUntil)
	if err == nil {
		if !s.multiTenant {
			s.store.data.OwnerClientID = clientID
		}
		err = s.store.saveLocked()
	}
	s.store.mu.Unlock()
	if err != nil {
		slog.Debug("oauth token exchange failed", "request_id", requestID(r), "reason", "issue_or_persist_tokens", "error", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not issue tokens")
		return
	}
	slog.Debug("oauth token exchange completed", "request_id", requestID(r), "client", fingerprint(clientID), "access_token", fingerprint(access), "refresh_token", fingerprint(refresh), "scope", code.Scope)
	s.writeTokenResponse(w, access, refresh, code.Scope)
}

func (s *Server) exchangeRefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshValue := r.Form.Get("refresh_token")
	clientID := r.Form.Get("client_id")
	resource := r.Form.Get("resource")
	slog.Debug("oauth refresh started", "request_id", requestID(r), "client", fingerprint(clientID), "refresh_token", fingerprint(refreshValue), "resource", resource)
	if refreshValue == "" || clientID == "" || resource == "" {
		slog.Debug("oauth refresh rejected", "request_id", requestID(r), "reason", "missing_required_field", "has_refresh_token", refreshValue != "", "has_client_id", clientID != "", "has_resource", resource != "")
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token, client_id and resource are required")
		return
	}

	now := time.Now().UTC()
	s.store.mu.Lock()
	s.cleanupLocked(now)
	tok, ok := s.store.data.RefreshTokens[refreshValue]
	if ok {
		delete(s.store.data.RefreshTokens, refreshValue)
	}
	if !ok || now.After(tok.ExpiresAt) || tok.ClientID != clientID || tok.Resource != resource || (!s.multiTenant && s.store.data.OwnerClientID != clientID) {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth refresh rejected", "request_id", requestID(r), "reason", "invalid_refresh_grant", "client", fingerprint(clientID), "refresh_token", fingerprint(refreshValue))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	grantErr := s.validateEnvironmentGrantLocked(tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil, now)
	if grantErr != nil {
		_ = s.store.saveLocked()
		s.store.mu.Unlock()
		slog.Debug("oauth refresh rejected", "request_id", requestID(r), "reason", grantErr.Error(), "environment", fingerprint(tok.EnvironmentID))
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", grantErr.Error())
		return
	}
	access, refresh, err := s.issueTokensLocked(clientID, tok.Resource, tok.Scope, tok.EnvironmentID, tok.EnvironmentRev, tok.EnvironmentUntil)
	if err == nil {
		err = s.store.saveLocked()
	}
	s.store.mu.Unlock()
	if err != nil {
		slog.Debug("oauth refresh failed", "request_id", requestID(r), "reason", "issue_or_persist_tokens", "error", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not rotate tokens")
		return
	}
	slog.Debug("oauth refresh completed", "request_id", requestID(r), "client", fingerprint(clientID), "access_token", fingerprint(access), "refresh_token", fingerprint(refresh), "scope", tok.Scope)
	s.writeTokenResponse(w, access, refresh, tok.Scope)
}

func (s *Server) issueTokensLocked(clientID, resource, scope, environmentID string, environmentRev uint64, environmentUntil time.Time) (string, string, error) {
	access, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	refresh, err := randomToken(48)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	grant := Token{ClientID: clientID, Resource: resource, Scope: scope, EnvironmentID: environmentID, EnvironmentRev: environmentRev, EnvironmentUntil: environmentUntil}
	accessToken := grant
	accessToken.ExpiresAt = now.Add(s.accessTTL)
	refreshToken := grant
	refreshToken.ExpiresAt = now.Add(s.refreshTTL)
	s.store.data.AccessTokens[access] = accessToken
	s.store.data.RefreshTokens[refresh] = refreshToken
	return access, refresh, nil
}

func (s *Server) writeTokenResponse(w http.ResponseWriter, access, refresh, scope string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int64(s.accessTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

func (s *Server) cleanupLocked(now time.Time) {
	for key, session := range s.store.data.AuthorizationSessions {
		if now.After(session.ExpiresAt) {
			delete(s.store.data.AuthorizationSessions, key)
		}
	}
	for key, code := range s.store.data.Codes {
		if now.After(code.ExpiresAt) {
			delete(s.store.data.Codes, key)
		}
	}
	for key, tok := range s.store.data.AccessTokens {
		if now.After(tok.ExpiresAt) {
			delete(s.store.data.AccessTokens, key)
		}
	}
	for key, tok := range s.store.data.RefreshTokens {
		if now.After(tok.ExpiresAt) {
			delete(s.store.data.RefreshTokens, key)
		}
	}
}

func hasVerifiedClientCertificate(r *http.Request, dnsName string) bool {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return false
	}
	if dnsName == "" {
		return true
	}
	return r.TLS.PeerCertificates[0].VerifyHostname(dnsName) == nil
}

func verifyPKCE(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	actual := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(actual), []byte(challenge)) == 1
}

func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func randomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func oauthRedirectURL(redirectURI, state, code, oauthErr string) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if state != "" {
		q.Set("state", state)
	}
	if oauthErr != "" {
		q.Set("error", oauthErr)
	} else {
		q.Set("code", code)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *Server) renderOAuthHandoff(w http.ResponseWriter, r *http.Request, redirectURI, state, code, oauthErr string) {
	target, err := oauthRedirectURL(redirectURI, state, code, oauthErr)
	if err != nil {
		http.Error(w, "invalid redirect URI", http.StatusBadRequest)
		return
	}
	u, _ := url.Parse(target)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(http.StatusOK)
	_ = oauthHandoffPage.Execute(w, map[string]any{"Target": target})
	slog.Debug("oauth callback handoff rendered", "request_id", requestID(r), "redirect_host", u.Host, "redirect_path", u.Path, "redirect", fingerprint(redirectURI), "has_state", state != "", "has_code", code != "", "oauth_error", oauthErr)
}

func redirectOAuth(w http.ResponseWriter, r *http.Request, redirectURI, state, code, oauthErr string) {
	target, err := oauthRedirectURL(redirectURI, state, code, oauthErr)
	if err != nil {
		http.Error(w, "invalid redirect URI", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func fingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}

func clientIDHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return u.Host
}

func redirectHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid"
	}
	return u.Host
}

func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Lilith-MCP-Request-ID"); id != "" {
		return id
	}
	return r.Header.Get("X-RootMCP-Request-ID")
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
