package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ThowiLabs/kagssh-go/internal/agenttasks"
	"github.com/ThowiLabs/kagssh-go/internal/config"
	"github.com/ThowiLabs/kagssh-go/internal/dashboard"
	"github.com/ThowiLabs/kagssh-go/internal/diskguard"
	"github.com/ThowiLabs/kagssh-go/internal/githubtoken"
	"github.com/ThowiLabs/kagssh-go/internal/gpumetrics"
	"github.com/ThowiLabs/kagssh-go/internal/oauth"
	"github.com/ThowiLabs/kagssh-go/internal/pinauth"
	"github.com/ThowiLabs/kagssh-go/internal/projectstate"
	"github.com/ThowiLabs/kagssh-go/internal/securefs"
	"github.com/ThowiLabs/kagssh-go/internal/skills"
	"sync"
)

const protocol = "2025-11-25"       // Transporte legado compatible con initialize.
const modernProtocol = "2026-07-28" // Discovery stateless; la versión no depende de una sesión.
const maxBody = 1 << 20

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type Server struct {
	cfg          config.Config
	public       string
	auth         *oauth.Server
	pinState     *pinauth.State
	generatedPIN string
	githubMu     sync.RWMutex
	github       *githubtoken.Client
	githubOn     bool
	hfMu         sync.RWMutex
	hfToken      string
	projects     *projectstate.Store
	agents       *agenttasks.Store
	skills       skills.Registry
}

func New(cfg config.Config, public string) (*Server, error) {
	pin := cfg.MCPAccessPIN
	generated := ""
	if pin == "" {
		var err error
		pin, err = pinauth.Generate()
		if err != nil {
			return nil, fmt.Errorf("generar PIN criptográfico: %w", err)
		}
		generated = pin
	}
	pinState, err := pinauth.New(pin)
	if err != nil {
		return nil, err
	}
	cfg.MCPAccessPIN = "" // La configuración retenida no necesita el PIN en claro.
	if err := securefs.EnsurePrivateDir(cfg.DataDir); err != nil {
		return nil, fmt.Errorf("proteger directorio KagMCP: %w", err)
	}
	state := filepath.Join(cfg.DataDir, "state", "oauth.json")
	auth, err := oauth.New(oauth.Config{
		PublicURL: public, StateFile: state, PINState: pinState, AgentMode: true,
		AccessTTL: time.Hour, RefreshTTL: 30 * 24 * time.Hour,
		PINMaxAttempts: 5, PINWindow: 5 * time.Minute, PINLockout: 15 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("autorización MCP: %w", err)
	}
	projects, err := projectstate.Open(filepath.Join(cfg.DataDir, "projects.json"))
	if err != nil {
		return nil, fmt.Errorf("cargar proyectos: %w", err)
	}
	agents, err := agenttasks.Open(filepath.Join(cfg.DataDir, "state", "agent-tasks.json"))
	if err != nil {
		return nil, fmt.Errorf("cargar delegaciones: %w", err)
	}

	// Los Kaggle Secrets también estarán disponibles para comandos del runtime.
	if cfg.GitHubToken != "" {
		if err := os.Setenv("GITHUB_TOKEN", cfg.GitHubToken); err != nil {
			return nil, err
		}
	}
	if cfg.HFToken != "" {
		if err := os.Setenv("HF_TOKEN", cfg.HFToken); err != nil {
			return nil, err
		}
	}
	return &Server{cfg: cfg, public: public, auth: auth, pinState: pinState, generatedPIN: generated, github: githubtoken.New(cfg.GitHubToken), githubOn: cfg.GitHubToken != "", hfToken: cfg.HFToken, projects: projects, agents: agents, skills: skills.Registry{Dir: filepath.Join(cfg.DataDir, "skills")}}, nil
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.auth.RegisterRoutes(mux)
	mux.Handle("/mcp", s)

	panel := dashboard.New(s.public, s.pinState, s.auth.ResetOwner, s.projects, s.skills, s.configureGitHub, s.githubConfigured, s.projectExport, s.projectImport)
	panel.HuggingFace = s.configureHF
	panel.HFConfigured = s.hfConfigured
	panel.Agents = s.agents
	if err := panel.EnablePersistence(filepath.Join(s.cfg.DataDir, "state", "web-sessions.json")); err != nil {
		slog.Error("no se pudo cargar las sesiones persistentes", "error", err)
		// Un estado inválido no permite un fallback silencioso a sesiones efímeras.
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "No se pudo iniciar sesión segura. Revisa permisos del directorio state.", 503)
		})
	} else {
		mux.Handle("/", panel)
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "KagMCP OK\n")
	})
	return mux
}
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.MCPPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("escuchar MCP: %w", err)
	}
	defer ln.Close()
	// Se sirve únicamente /api/health hasta validar que el túnel
	// anunciado realmente llega a esta sesión. OAuth aún no se expone.
	bridge, err := newProbeHandler()
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler: bridge, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second,
		WriteTimeout: 65 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- httpServer.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		select {
		case <-serveDone:
		case <-time.After(5 * time.Second):
		}
	}()
	public := cfg.MCPPublicURL
	provider := "externo"
	var tunnel publicTunnel
	if cfg.MCPTunnel != "none" {
		public, tunnel, provider, err = startTunnelWithRetry(ctx, func(c context.Context) (string, publicTunnel, string, error) {
			return startTunnel(c, cfg.MCPTunnel, cfg.MCPGradioRetries, cfg.MCPPort, "http://"+addr, bridge.nonce, log)
		}, sleepContext, log)
		if err != nil {
			return fmt.Errorf("establecer túnel HTTPS: %w", err)
		}
		defer func() {
			if tunnel != nil {
				_ = tunnel.Close()
			}
		}()
	}
	if public == "" {
		return errors.New("falta URL pública del servidor MCP")
	}
	server, err := New(cfg, public)
	if err != nil {
		return err
	}
	bridge.Set(server.Handler())
	if server.generatedPIN != "" {
		log.Warn("PIN temporal generado por Go; úsalo para el panel y OAuth, cambia el PIN desde el panel si deseas", "pin_temporal", server.generatedPIN)
		server.generatedPIN = ""
	}
	log.Info("KagMCP listo para conectar cliente MCP", "url", public+"/mcp", "panel_web", public+"/", "tunnel", provider, "github_token_configurado", cfg.GitHubToken != "")
	writeCurrentTunnelStatus(cfg, public, provider, "online", log)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		warned := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e := diskguard.Check("/kaggle/working")
				if e != nil && !warned {
					log.Error("espacio crítico: detener instalaciones/escrituras", "error", e)
					warned = true
				}
				if e == nil && warned {
					log.Info("espacio de trabajo recuperado")
					warned = false
				}
			}
		}
	}()
	if cfg.MCPTunnel == "none" {
		select {
		case err = <-serveDone:
			if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("servidor HTTP MCP: %w", err)
		case <-ctx.Done():
			return nil
		}
	}
	for {
		watchCtx, stopWatch := context.WithCancel(ctx)
		watchDone := make(chan error, 1)
		go func(current publicTunnel, url string) {
			watchDone <- watchPublicTunnel(watchCtx, current, url, bridge.nonce, tunnelCheckInterval, tunnelFailureThreshold, healthCheck, log)
		}(tunnel, public)
		select {
		case err = <-serveDone:
			stopWatch()
			if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("servidor HTTP MCP: %w", err)
		case <-ctx.Done():
			stopWatch()
			writeCurrentTunnelStatus(cfg, public, provider, "closed", log)
			return nil
		case err = <-watchDone:
			stopWatch()
			if ctx.Err() != nil {
				return nil
			}
			log.Error("túnel público MCP desconectado; recuperando sesión", "provider", provider, "url_anterior", public, "reason", err)
			writeCurrentTunnelStatus(cfg, public, provider, "reconnecting", log)
			bridge.Set(nil)
			_ = tunnel.Close()
			current := server
			launch := func(c context.Context) (string, publicTunnel, string, error) {
				return startTunnel(c, cfg.MCPTunnel, cfg.MCPGradioRetries, cfg.MCPPort, "http://"+addr, bridge.nonce, log)
			}
			server, public, tunnel, provider, err = recoverPublicTunnel(ctx, cfg, current, bridge, launch, sleepContext, log)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("restaurar túnel público: %w", err)
			}
			writeCurrentTunnelStatus(cfg, public, provider, "online", log)
		}
	}
}

// ServeHTTP mantiene el handshake heredado y también admite MCP 2026-07-28
// stateless. OAuth se valida antes de procesar métodos o herramientas.
// acceptedMCPOrigin checks the browser Origin while allowing the known ChatGPT
// clients. A tunnel or ChatGPT client can supply a different Origin than the
// public URL; arbitrary websites must still be rejected (MCP DNS-rebinding rule).
func acceptedMCPOrigin(raw, public string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch raw {
	case "https://chatgpt.com", "https://chat.openai.com", "https://www.chatgpt.com":
		return true
	}
	hostURL, err := url.Parse(public)
	if err != nil {
		return false
	}
	return u.Scheme == hostURL.Scheme && u.Host == hostURL.Host
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !acceptedMCPOrigin(r.Header.Get("Origin"), s.public) {
		slog.Warn("MCP rechazado", "reason", "origin_not_allowed", "http_status", http.StatusForbidden)
		http.Error(w, "Origin no permitido", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "este endpoint requiere POST", http.StatusMethodNotAllowed)
		return
	}
	if err := s.auth.ValidateBearer(r); err != nil {
		slog.Warn("MCP rechazado", "reason", "bearer_invalid", "http_status", http.StatusUnauthorized)
		w.Header().Set("WWW-Authenticate", s.auth.WWWAuthenticate("invalid_token", "autorización requerida"))
		http.Error(w, "autenticación requerida", http.StatusUnauthorized)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.public {
		slog.Info("MCP con Origin de cliente confiable diferente al túnel", "origin_present", true)
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		slog.Warn("MCP rechazado", "reason", "content_type", "http_status", http.StatusUnsupportedMediaType)
		http.Error(w, "Content-Type debe ser application/json", http.StatusUnsupportedMediaType)
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxBody)
	defer body.Close()
	dec := json.NewDecoder(body)
	var req request
	if err := dec.Decode(&req); err != nil {
		slog.Warn("MCP rechazado", "reason", "json_parse", "http_status", http.StatusBadRequest)
		s.rpcStatus(w, http.StatusBadRequest, response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "JSON no válido"}})
		return
	}
	if dec.Decode(new(any)) != io.EOF {
		slog.Warn("MCP rechazado", "reason", "extra_json", "http_status", http.StatusBadRequest)
		s.rpcStatus(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "se admite un mensaje JSON-RPC por solicitud"}})
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		slog.Warn("MCP rechazado", "reason", "invalid_rpc", "http_status", http.StatusBadRequest)
		s.rpcStatus(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "petición inválida"}})
		return
	}
	// La versión se negocia por petición; la URL MCP no mantiene sesiones.
	version := r.Header.Get("MCP-Protocol-Version")
	modern := version == modernProtocol
	if req.Method == "server/discover" || req.Method == "tools/list" || req.Method == "initialize" {
		// Sólo nombres de métodos conocidos, nunca contenido de herramientas o tokens.
		slog.Info("descubrimiento MCP", "method", req.Method, "protocol", version)
	}
	switch version {
	case "", protocol, "2025-03-26", modernProtocol:
	default:
		slog.Warn("MCP descubrimiento rechazado", "method", req.Method, "reason", "protocol_version_unsupported", "http_status", http.StatusBadRequest)
		s.rpcStatus(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32022, Message: "Unsupported protocol version", Data: map[string]any{"supported": []string{modernProtocol, protocol, "2025-03-26"}, "requested": version}}})
		return
	}
	if modern {
		// MCP 2026-07-28 exige coherencia entre las cabeceras espejadas y
		// los datos del mensaje. Nunca confiamos en un header sin compararlo.
		if r.Header.Get("Mcp-Method") != req.Method {
			slog.Warn("MCP descubrimiento rechazado", "method", req.Method, "reason", "mcp_method_header_mismatch", "http_status", http.StatusBadRequest)
			s.rpcStatus(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32020, Message: "Mcp-Method no coincide con el cuerpo"}})
			return
		}
		var params struct {
			Name string `json:"name"`
			Meta struct {
				Protocol     string          `json:"io.modelcontextprotocol/protocolVersion"`
				Capabilities json.RawMessage `json:"io.modelcontextprotocol/clientCapabilities"`
			} `json:"_meta"`
		}
		if len(req.Params) == 0 || json.Unmarshal(req.Params, &params) != nil || params.Meta.Protocol != modernProtocol || len(params.Meta.Capabilities) == 0 {
			slog.Warn("MCP descubrimiento rechazado", "method", req.Method, "reason", "modern_meta_missing_or_mismatch", "http_status", http.StatusBadRequest)
			s.rpcStatus(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32020, Message: "metadata MCP 2026 ausente o no coincide con MCP-Protocol-Version"}})
			return
		}
		if req.Method == "tools/call" && (params.Name == "" || r.Header.Get("Mcp-Name") != params.Name) {
			slog.Warn("MCP llamada rechazada", "reason", "mcp_name_header_mismatch", "http_status", http.StatusBadRequest)
			s.rpcStatus(w, http.StatusBadRequest, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32020, Message: "Mcp-Name no coincide con la herramienta"}})
			return
		}
	} else if req.Method == "server/discover" {
		// La versión 2025-11-25 no define server/discover.
		s.rpc(w, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "Method not found"}})
		return
	}
	// Las notificaciones nunca tienen ID; no se emite un objeto JSON-RPC.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "server/discover":
		// En el protocolo 2026, las capacidades y la identidad deben
		// reflejar las herramientas reales, sin inventar otras capacidades.
		resp.Result = map[string]any{
			"resultType":        "complete",
			"supportedVersions": []string{modernProtocol, protocol},
			"capabilities":      map[string]any{"tools": map[string]any{"listChanged": false}},
			"_meta":             map[string]any{"io.modelcontextprotocol/serverInfo": map[string]string{"name": "kagmcp", "title": "KagMCP", "version": "0.2.0"}},
			"instructions":      mcpInstructions,
			"ttlMs":             30000,
			"cacheScope":        "private",
		}
	case "initialize":
		resp.Result = map[string]any{"protocolVersion": protocol,
			"capabilities": map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":   map[string]string{"name": "kagmcp", "title": "KagMCP", "version": "0.2.0"},
			"instructions": mcpInstructions}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		defs := s.definitions()
		slog.Info("MCP herramientas disponibles", "count", len(defs), "protocol", version)
		out := map[string]any{"tools": defs}
		if modern {
			out["resultType"] = "complete"
			out["ttlMs"] = 30000
			out["cacheScope"] = "private"
		}
		resp.Result = out
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil || call.Name == "" {
			resp.Error = &rpcError{Code: -32602, Message: "tools/call requiere name y arguments"}
		} else {
			out, err := s.invoke(r.Context(), call.Name, call.Arguments)
			resp.Result = toolResult(out, err)
			if modern {
				resp.Result.(map[string]any)["resultType"] = "complete"
			}
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "método desconocido"}
	}
	if req.Method == "server/discover" || req.Method == "initialize" || req.Method == "tools/list" {
		slog.Info("MCP respuesta de descubrimiento", "method", req.Method, "protocol", version, "http_status", 200)
	}
	s.rpc(w, resp)
}

const mcpInstructions = skills.Summary + " SUBAGENTES OPCIONALES: antes de usar agent_task_create pregunta SIEMPRE al usuario si desea delegar esta tarea concreta; si responde NO, trabaja aquí sin subagente. Solo si responde SÍ usa user_approved=true, conserva MCP destino explícito, ruta exacta y rama aislada. No creas ni abras chats automáticamente. Diferencia MCP coordinador KagMCP y MCP destino. Un subagente entrega needs_review/blocked; jamás se autoaprueba, hace merge ni push sin autorización. Ejecuta skills_read(ponytail-v2) al iniciar trabajo. PRIORIDAD GITHUB: para consultar o modificar contenido remoto en GitHub utiliza PRIMERO las herramientas github_* de este MCP (incluyendo github_tag_*, github_workflow_*, github_file_upload, github_release_asset_upload y github_api/graphql) cuando estén disponibles y autorizadas; pide consentimiento específico para crear tags, editar/ejecutar workflows, subir archivos, borrar recursos o fusionar PRs. No inventes estado de GitHub ni asumas permisos del PAT; evita exec/git manual para operaciones remotas ya cubiertas. No realices operaciones destructivas sin autorización explícita. Si GITHUB_TOKEN falta, solicita configurarlo en el panel, no inventes acceso. Para proyectos locales Kaggle usa herramientas locales autorizadas. En Kaggle cada prueba termina con repositorio Git y notebook funcional; después de verificarlos, una UI Gradio. Fija versiones, vigila espacio libre para evitar readonly, registra descripción/proyecto de comandos, memoria y tareas. Los datos de /kaggle/input solo lectura. No reveles secretos."

func (s *Server) rpc(w http.ResponseWriter, v response) {
	s.rpcStatus(w, http.StatusOK, v)
}

func (s *Server) rpcStatus(w http.ResponseWriter, status int, v response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func toolResult(data any, err error) any {
	if err != nil {
		return map[string]any{"content": []any{map[string]string{"type": "text", "text": err.Error()}}, "isError": true}
	}
	b, marshalErr := json.Marshal(data)
	if marshalErr != nil {
		b = []byte("{}")
	}
	return map[string]any{"content": []any{map[string]string{"type": "text", "text": string(b)}}, "isError": false}
}
func def(name, desc string, properties map[string]any, required ...string) any {
	schema := map[string]any{
		"type": "object", "properties": properties, "additionalProperties": false,
	}
	// JSON Schema requiere un array en "required"; nil se serializa a
	// JSON null, lo cual provoca fallo al descubrir las herramientas.
	if len(required) > 0 {
		schema["required"] = required
	}
	return map[string]any{"name": name, "description": desc, "inputSchema": schema}
}
func stringProp(desc string) any { return map[string]string{"type": "string", "description": desc} }
func (s *Server) definitions() []any {
	return []any{
		def("skills_list", "Lista Skills disponibles; Ponytail v2 siempre activa", map[string]any{}),
		def("skills_read", "Lee Ponytail v2 u otra Skill en fragmentos", map[string]any{"name": stringProp("ID de Skill"), "offset": map[string]any{"type": "integer"}}, "name"),
		def("skills_search", "Busca texto en Skills", map[string]any{"query": stringProp("Texto a buscar")}, "query"),
		def("skills_install", "Instala Skill personalizada privada", map[string]any{"name": stringProp("ID de Skill"), "content": stringProp("Markdown de Skill")}, "name", "content"),

		def("gpu_detect", "Detectar GPUs NVIDIA disponibles en Kaggle: modelo, UUID, índice, versión del driver y VRAM total. Informa si nvidia-smi no está disponible.", map[string]any{}),
		def("gpu_metrics", "Métricas actuales de TODAS las GPUs NVIDIA: porcentaje de uso GPU y memoria, VRAM total/usada/libre, temperatura, energía, ventilador y estado de rendimiento.", map[string]any{}),
		def("gpu_processes", "Procesos de cómputo que usan GPU, con PID, nombre, UUID y VRAM MiB. Visibilidad compartida en Kaggle; no modifica procesos.", map[string]any{}),
		def("gpu_monitor", "Muestrear utilización, VRAM, temperatura y potencia NVIDIA a intervalos, hasta 10 muestras y 20 segundos. Sin tareas persistentes.", map[string]any{"samples": map[string]any{"type": "integer", "description": "Número de muestras, 1 a 10"}, "interval_seconds": map[string]any{"type": "integer", "description": "Separación entre muestras, 1 a 5 segundos"}}, "samples", "interval_seconds"),
		def("environment_info", "Información del runtime Kaggle sin credenciales", map[string]any{}),
		def("list_dir", "Lista directorios en /kaggle/working o /kaggle/input", map[string]any{"path": stringProp("Ruta de Kaggle")}),
		def("read_file", "Lee un archivo de texto Kaggle hasta 1 MiB", map[string]any{"path": stringProp("Ruta a leer")}, "path"),
		def("write_file", "Escribe un archivo en /kaggle/working hasta 1 MiB", map[string]any{"path": stringProp("Ruta a escribir"), "content": stringProp("Contenido completo")}, "path", "content"),
		def("exec", "Ejecuta un comando en el runtime Kaggle con timeout y salida limitada", map[string]any{"command": stringProp("Comando shell"), "cwd": stringProp("Directorio de trabajo en /kaggle/working"), "project": stringProp("ID de proyecto, para historial"), "description": stringProp("Descripción del propósito del comando")}, "command", "description"),
		def("agent_task_create", "Crea asignación de subagente SOLO después de preguntar y obtener consentimiento explícito del usuario. Nunca delegar automáticamente. No inicia chats, no otorga permisos al MCP destino.", map[string]any{
			"title": stringProp("Título"), "objective": stringProp("Trabajo específico"), "mcp": stringProp("Nombre literal del conector destino, ejemplo $pl0"), "mcp_id": stringProp("ID opcional de instancia"), "workspace": stringProp("Ruta absoluta de trabajo en el MCP destino"), "branch": stringProp("Rama/worktree aislado"), "allowed_paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "criteria": stringProp("Criterios de aceptación"), "context": stringProp("Contexto necesario"), "user_approved": map[string]any{"type": "boolean", "description": "Verdadero SOLO si el usuario confirmó que desea delegar esta tarea en concreto"},
		}, "title", "objective", "mcp", "workspace", "branch", "allowed_paths", "criteria", "context", "user_approved"),
		def("agent_task_list", "Lista tareas de subagentes del coordinador KagMCP", map[string]any{}),
		def("agent_task_status", "Consulta estado y entrega de una tarea", map[string]any{"id": stringProp("ID exacto")}, "id"),
		def("agent_task_prompt", "Recupera prompt generado para copiarlo al chat secundario", map[string]any{"id": stringProp("ID exacto")}, "id"),
		def("agent_task_review", "Orquestador: obtiene entrega y checklist para revisar commit/diff/pruebas. SOLO LECTURA; la aprobación requiere usuario en /agents.", map[string]any{"id": stringProp("ID exacto")}, "id"),
		def("agent_task_open", "Subagente reclama tarea pendiente con su nombre de agente; debe verificar MCP destino antes de editar", map[string]any{"id": stringProp("ID exacto"), "agent": stringProp("Nombre único del subagente")}, "id", "agent"),
		def("agent_task_submit", "Entrega commit y tests para revisión humana; o declara bloqueo. No aprueba, fusiona ni publica cambios.", map[string]any{"id": stringProp("ID exacto"), "agent": stringProp("Nombre reclamante"), "status": stringProp("needs_review o blocked"), "commit": stringProp("SHA commit producido"), "tests": stringProp("Pruebas con resultados"), "summary": stringProp("Resumen de cambios"), "blocker": stringProp("Motivo de bloqueo")}, "id", "agent", "status"),
		def("projects_list", "Lista los proyectos y su memoria/tareas compartidas", map[string]any{}),
		def("project_create", "Crea un proyecto multiagente", map[string]any{"project": stringProp("ID único"), "title": stringProp("Nombre"), "repository": stringProp("owner/repo"), "notebook": stringProp("ruta al .ipynb")}, "project", "title"),
		def("project_memory_add", "Guarda una decisión técnica persistente", map[string]any{"project": stringProp("ID del proyecto"), "content": stringProp("Nota de contexto")}, "project", "content"),
		def("project_export", "Exporta memoria y tareas al repo para versionarlas", map[string]any{"project": stringProp("ID del proyecto")}, "project"),
		def("project_import", "Restaura memoria y tareas desde contexto JSON versionado en Git", map[string]any{"project": stringProp("ID de proyecto")}, "project"),
		def("tasks_add", "Añade una tarea del proyecto", map[string]any{"project": stringProp("ID"), "title": stringProp("Tarea")}, "project", "title"),
		def("tasks_update", "Actualiza tarea pending/in_progress/done", map[string]any{"project": stringProp("ID"), "task": stringProp("ID tarea"), "status": stringProp("Estado")}, "project", "task", "status"),
		def("history_list", "Consulta historial de comandos con descripción", map[string]any{"project": stringProp("ID, opcional")}),
		def("project_verify", "Ejecuta las pruebas y el notebook, exige Git limpio y registra el commit/digest antes de Gradio", map[string]any{"project": stringProp("ID de proyecto"), "test_command": stringProp("Comando reproducible de tests"), "notebook_command": stringProp("Comando que ejecuta el notebook y comprueba su funcionamiento")}, "project", "test_command", "notebook_command"),
		def("gradio_scaffold", "Genera plantilla Gradio solo con comprobante vigente de project_verify", map[string]any{"project": stringProp("ID de proyecto")}, "project"),
		def("github_status", "Valida GITHUB_TOKEN y muestra cuenta autenticada", map[string]any{}),
		def("github_repositories", "Lista repositorios accesibles con GITHUB_TOKEN (100 recientes)", map[string]any{}),
		def("github_branches", "Lista ramas de un repositorio", map[string]any{"repository": stringProp("owner/repo")}, "repository"),
		def("github_file_read", "Lee un archivo mediante API GitHub", map[string]any{"repository": stringProp("owner/repo"), "path": stringProp("ruta relativa"), "ref": stringProp("rama o SHA opcional")}, "repository", "path"),
		def("github_file_write", "Crea o actualiza un archivo mediante API GitHub y genera un commit", map[string]any{"repository": stringProp("owner/repo"), "path": stringProp("ruta relativa"), "branch": stringProp("rama destino"), "message": stringProp("mensaje de commit"), "content": stringProp("contenido completo"), "sha": stringProp("SHA anterior obligatorio al actualizar")}, "repository", "path", "branch", "message", "content"),
		def("github_api", "Acceso REST GitHub completo (GET/POST/PATCH/PUT/DELETE) con PAT y ruta API. Autorizar acciones destructivas.", map[string]any{"method": stringProp("method"), "endpoint": stringProp("endpoint"), "json_body": stringProp("json_body")}, "method", "endpoint"),
		def("github_graphql", "Consultas y mutaciones GraphQL autenticadas con PAT; todas las capacidades permitidas por scopes.", map[string]any{"query": stringProp("query"), "variables": stringProp("variables")}, "query"),
		def("github_repo_create", "Crear repositorio personal o de organización.", map[string]any{"name": stringProp("name"), "description": stringProp("description"), "private": map[string]any{"type": "boolean"}, "owner_type": stringProp("owner_type"), "organization": stringProp("organization")}, "name", "private"),
		def("github_repo_delete", "Eliminar repositorio en GitHub; peligro, requiere autorización explícita.", map[string]any{"repository": stringProp("repository")}, "repository"),
		def("github_branch_create", "Crear rama desde un SHA Git.", map[string]any{"repository": stringProp("repository"), "branch": stringProp("branch"), "from_sha": stringProp("from_sha")}, "repository", "branch", "from_sha"),
		def("github_branch_delete", "Eliminar rama remota; requiere autorización explícita.", map[string]any{"repository": stringProp("repository"), "branch": stringProp("branch")}, "repository", "branch"),
		def("github_file_delete", "Eliminar archivo mediante GitHub Contents API y registrar commit.", map[string]any{"repository": stringProp("repository"), "path": stringProp("path"), "branch": stringProp("branch"), "sha": stringProp("sha"), "message": stringProp("message")}, "repository", "path", "branch", "sha", "message"),
		def("github_pr_create", "Crear Pull Request.", map[string]any{"repository": stringProp("repository"), "title": stringProp("title"), "head": stringProp("head"), "base": stringProp("base"), "body": stringProp("body")}, "repository", "title", "head", "base"),
		def("github_pr_merge", "Fusionar Pull Request; puede activar Actions.", map[string]any{"repository": stringProp("repository"), "number": map[string]any{"type": "integer"}, "merge_method": stringProp("merge_method"), "message": stringProp("message")}, "repository", "number"),
		def("github_issue_create", "Crear Issue.", map[string]any{"repository": stringProp("repository"), "title": stringProp("title"), "body": stringProp("body")}, "repository", "title"),
		def("github_issue_update", "Actualizar Issue con JSON de campos.", map[string]any{"repository": stringProp("repository"), "number": map[string]any{"type": "integer"}, "json_body": stringProp("json_body")}, "repository", "number", "json_body"),
		def("github_release_create", "Crear Release de GitHub.", map[string]any{"repository": stringProp("repository"), "tag_name": stringProp("tag_name"), "name": stringProp("name"), "body": stringProp("body")}, "repository", "tag_name"),
		def("github_workflow_dispatch", "Ejecuta workflow de GitHub Actions si dispone de workflow_dispatch; puede consumir minutos y exige confirmación explícita.", map[string]any{"repository": stringProp("repository"), "workflow": stringProp("workflow"), "ref": stringProp("ref"), "inputs": stringProp("inputs"), "confirmed": map[string]any{"type": "boolean"}}, "repository", "workflow", "ref", "confirmed"),
		def("github_workflows", "Listar workflows de GitHub Actions.", map[string]any{"repository": stringProp("repository")}, "repository"),
		def("github_tag_create", "Crea tag ligero/anotado sin sobrescribir refs; puede disparar Actions, requiere confirmación explícita.", map[string]any{
			"repository": stringProp("owner/repo"), "tag_name": stringProp("Nombre de tag v1.2.3"), "sha": stringProp("SHA Git completo del commit"), "tag_message": stringProp("Mensaje opcional para tag anotado"), "confirmed": map[string]any{"type": "boolean"},
		}, "repository", "tag_name", "sha", "confirmed"),
		def("github_tag_list", "Lista hasta 100 tags del repositorio", map[string]any{"repository": stringProp("owner/repo")}, "repository"),
		def("github_tag_delete", "BORRA un tag; requiere autorización explícita confirmed=true.", map[string]any{"repository": stringProp("owner/repo"), "tag_name": stringProp("Nombre tag"), "confirmed": map[string]any{"type": "boolean"}}, "repository", "tag_name", "confirmed"),
		def("github_file_upload", "Sube archivo binario local de /kaggle/working al repo (máx 1 MiB). Si se actualiza requiere SHA anterior. Confirmar publicación.", map[string]any{
			"repository": stringProp("owner/repo"), "local_path": stringProp("Ruta absoluta /kaggle/working/..."), "path": stringProp("Ruta destino dentro del repo"), "branch": stringProp("Rama de destino"), "message": stringProp("Mensaje commit"), "sha": stringProp("SHA de contenido anterior si se actualiza"), "confirmed": map[string]any{"type": "boolean"},
		}, "repository", "local_path", "path", "branch", "message", "confirmed"),
		def("github_release_asset_upload", "Publica archivo ZIP/binario local /kaggle/working como adjunto de Release existente (máx 50 MiB). Confirmación explícita.", map[string]any{
			"repository": stringProp("owner/repo"), "release_id": map[string]any{"type": "integer"}, "local_path": stringProp("Archivo absoluto /kaggle/working"), "file_name": stringProp("Nombre remoto opcional"), "confirmed": map[string]any{"type": "boolean"},
		}, "repository", "release_id", "local_path", "confirmed"),
		def("github_workflow_upsert", "Crea o actualiza .github/workflows/archivo.yml por API. Requiere permiso Workflows write y confirmación explícita. Nunca ejecuta workflow automáticamente.", map[string]any{
			"repository": stringProp("owner/repo"), "workflow": stringProp("archivo.yml o .yaml"), "workflow_yaml": stringProp("Contenido YAML"), "branch": stringProp("Rama destino"), "message": stringProp("Mensaje de commit"), "sha": stringProp("SHA archivo anterior requerido al actualizar"), "confirmed": map[string]any{"type": "boolean"},
		}, "repository", "workflow", "workflow_yaml", "branch", "message", "confirmed"),
		def("github_workflow_set_state", "Habilita/deshabilita un workflow; confirmación explícita obligatoria.", map[string]any{
			"repository": stringProp("owner/repo"), "workflow": stringProp("Archivo o ID workflow"), "state": stringProp("enable o disable"), "confirmed": map[string]any{"type": "boolean"},
		}, "repository", "workflow", "state", "confirmed"),
		def("github_workflow_runs", "Lista hasta 30 ejecuciones de Actions en un repo o workflow concreto.", map[string]any{
			"repository": stringProp("owner/repo"), "workflow": stringProp("Archivo/ID workflow opcional"),
		}, "repository"),
		def("github_workflow_run", "Consulta estado y conclusión de una ejecución de Actions.", map[string]any{
			"repository": stringProp("owner/repo"), "run_id": map[string]any{"type": "integer"},
		}, "repository", "run_id"),
		def("github_workflow_rerun", "Vuelve a ejecutar un workflow; consume minutos. Requiere confirmación explícita.", map[string]any{
			"repository": stringProp("owner/repo"), "run_id": map[string]any{"type": "integer"}, "confirmed": map[string]any{"type": "boolean"},
		}, "repository", "run_id", "confirmed"),
		def("github_workflow_cancel", "Cancela workflow run. Requiere confirmación explícita.", map[string]any{
			"repository": stringProp("owner/repo"), "run_id": map[string]any{"type": "integer"}, "confirmed": map[string]any{"type": "boolean"},
		}, "repository", "run_id", "confirmed"),
	}
}
func (s *Server) invoke(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if agentTool(name) {
		return s.invokeAgent(ctx, name, raw)
	}
	if gpuTool(name) {
		return invokeGPU(ctx, name, raw, gpumetrics.New())
	}
	if githubExtendedTool(name) {
		return s.invokeGitHubExtended(ctx, name, raw)
	}
	if managementTool(name) {
		return s.invokeManagement(ctx, name, raw)
	}
	var a struct {
		Path        string `json:"path"`
		Content     string `json:"content"`
		Command     string `json:"command"`
		Project     string `json:"project"`
		Description string `json:"description"`
		Cwd         string `json:"cwd"`
		Repository  string `json:"repository"`
		Ref         string `json:"ref"`
		Branch      string `json:"branch"`
		Message     string `json:"message"`
		SHA         string `json:"sha"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("argumentos JSON inválidos: %w", err)
		}
	}
	switch name {
	case "environment_info":
		hostname, _ := os.Hostname()
		return map[string]any{"platform": "kaggle", "hostname": hostname, "working": "/kaggle/working", "datasets": "/kaggle/input", "mcp_url": s.public + "/mcp", "github_configured": s.githubConfigured(), "huggingface_configured": s.hfConfigured(), "ssh_enabled": s.cfg.SSHEnabled, "always_active_skill": skills.Active, "project_count": len(s.projects.Snapshot().Projects), "web_ui": s.public + "/"}, nil
	case "list_dir":
		return s.listDir(a.Path)
	case "read_file":
		return s.readFile(a.Path)
	case "write_file":
		return s.writeFile(a.Path, a.Content)
	case "exec":
		return s.execWithAudit(ctx, a.Command, a.Cwd, a.Project, a.Description)
	case "github_status":
		return s.githubClient().Status(ctx)
	case "github_repositories":
		return s.githubClient().Repos(ctx)
	case "github_branches":
		return s.githubClient().Branches(ctx, a.Repository)
	case "github_file_read":
		return s.githubClient().ReadFile(ctx, a.Repository, a.Path, a.Ref)
	case "github_file_write":
		return s.githubClient().WriteFile(ctx, a.Repository, a.Path, a.Branch, a.Message, a.Content, a.SHA)
	}
	return nil, errors.New("herramienta desconocida")
}
func (s *Server) path(raw string, write bool) (string, error) {
	if raw == "" {
		raw = "/kaggle/working"
	}
	if !filepath.IsAbs(raw) {
		raw = filepath.Join("/kaggle/working", raw)
	}
	cleaned := filepath.Clean(raw)
	private := filepath.Clean(s.cfg.DataDir)
	if cleaned == private || strings.HasPrefix(cleaned, private+string(os.PathSeparator)) {
		return "", errors.New("el directorio privado .kagmcp no es accesible mediante MCP")
	}
	inWorking := cleaned == "/kaggle/working" || strings.HasPrefix(cleaned, "/kaggle/working/")
	inInput := cleaned == "/kaggle/input" || strings.HasPrefix(cleaned, "/kaggle/input/")
	if !inWorking && (!inInput || write) {
		return "", errors.New("ruta fuera del ámbito autorizado de Kaggle")
	}
	// No permitir desbordamiento mediante symlinks dentro del árbol autorizado.
	inspect := cleaned
	for {
		target, err := filepath.EvalSymlinks(inspect)
		if err == nil {
			suffix, relErr := filepath.Rel(inspect, cleaned)
			if relErr == nil {
				actual := filepath.Clean(filepath.Join(target, suffix))
				withinWorking := actual == "/kaggle/working" || strings.HasPrefix(actual, "/kaggle/working/")
				withinInput := actual == "/kaggle/input" || strings.HasPrefix(actual, "/kaggle/input/")
				if !(withinWorking || (!write && withinInput)) || actual == private || strings.HasPrefix(actual, private+"/") {
					return "", errors.New("enlace simbólico fuera del ámbito autorizado")
				}
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(inspect)
		if parent == inspect {
			return "", errors.New("no se pudo validar la ruta")
		}
		inspect = parent
	}
	return cleaned, nil
}
func (s *Server) listDir(raw string) (any, error) {
	path, err := s.path(raw, false)
	if err != nil {
		return nil, err
	}
	list, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	files := make([]any, 0, min(100, len(list)))
	for _, f := range list {
		if f.Name() == ".kagmcp" || len(files) >= 100 {
			continue
		}
		info, e := f.Info()
		if e != nil {
			continue
		}
		files = append(files, map[string]any{"name": f.Name(), "is_dir": f.IsDir(), "size": info.Size()})
	}
	return map[string]any{"path": path, "entries": files, "truncated": len(list) > 100}, nil
}
func (s *Server) readFile(raw string) (any, error) {
	path, err := s.path(raw, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("solo archivos regulares de hasta 1 MiB")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "size": len(content), "content": string(content)}, nil
}
func (s *Server) writeFile(raw, content string) (any, error) {
	path, err := s.path(raw, true)
	if err != nil {
		return nil, err
	}
	if len(content) > 1<<20 {
		return nil, errors.New("escritura limitada a 1 MiB")
	}
	if err := diskguard.Check(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "bytes_written": len(content)}, nil
}

type limitedWriter struct {
	b   bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.b.Len() < w.max {
		_, _ = w.b.Write(p[:min(len(p), w.max-w.b.Len())])
	}
	return n, nil
}
func (s *Server) exec(ctx context.Context, command, cwd string) (any, error) {
	if command == "" || len(command) > 16<<10 {
		return nil, errors.New("comando vacío o demasiado grande")
	}
	if cwd == "" {
		cwd = "/kaggle/working"
	}
	dir, err := s.path(cwd, true)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() {
		return nil, errors.New("cwd no es un directorio")
	}
	if err := diskguard.Check(dir); err != nil {
		return nil, err
	}
	taskCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(taskCtx, shell, "-lc", command)
	cmd.Dir = dir
	for _, line := range os.Environ() {
		if strings.HasPrefix(line, "GITHUB_") || strings.HasPrefix(line, "MCP_") || strings.HasPrefix(line, "KAGGLE_") || strings.HasPrefix(line, "SSH_") || strings.HasPrefix(line, "HF_TOKEN=") {
			continue
		}
		cmd.Env = append(cmd.Env, line)
	}
	output := &limitedWriter{max: 65536}
	cmd.Stdout = output
	cmd.Stderr = output
	// Control continuo de almacenamiento durante el comando; si se agota,
	// cancela el shell antes de que Kaggle pueda quedar readonly.
	watchStop := make(chan struct{})
	watchErr := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchStop:
				return
			case <-taskCtx.Done():
				return
			case <-ticker.C:
				if e := diskguard.Check(dir); e != nil {
					watchErr <- e
					cancel()
					return
				}
			}
		}
	}()
	err = cmd.Run()
	close(watchStop)
	result := map[string]any{"output": output.b.String(), "truncated": output.b.Len() == 65536}
	select {
	case e := <-watchErr:
		return result, e
	default:
	}
	if taskCtx.Err() != nil {
		result["timeout"] = true
		return result, errors.New("comando cancelado o excedió 45s")
	}
	if err != nil {
		result["error"] = err.Error()
	}
	return result, nil
}
