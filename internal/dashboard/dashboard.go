package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ThowiLabs/kagssh-go/internal/agenttasks"
	"github.com/ThowiLabs/kagssh-go/internal/diskguard"
	"github.com/ThowiLabs/kagssh-go/internal/gpumetrics"
	"github.com/ThowiLabs/kagssh-go/internal/pinauth"
	"github.com/ThowiLabs/kagssh-go/internal/projectstate"
	"github.com/ThowiLabs/kagssh-go/internal/runtimemetrics"
	"github.com/ThowiLabs/kagssh-go/internal/skills"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type session struct {
	CSRF    string
	Expires time.Time
}
type attempts struct {
	Count int
	Until time.Time
}
type Handler struct {
	Public           string
	pin              *pinauth.State
	revokePIN        func() error
	Projects         *projectstate.Store
	Agents           *agenttasks.Store
	Skills           skills.Registry
	GitHub           func(context.Context, string) (string, error)
	GitHubConfigured func() bool
	HuggingFace      func(context.Context, string) (string, error)
	HFConfigured     func() bool
	Export           func(string) (any, error)
	Import           func(string) (any, error)
	mu               sync.Mutex
	sessions         map[string]session
	failures         map[string]attempts
	global           attempts
	sessionFile      string
	monitor          *runtimemetrics.Monitor
}

func New(public string, pin *pinauth.State, revokePIN func() error, store *projectstate.Store, reg skills.Registry, github func(context.Context, string) (string, error), configured func() bool, export func(string) (any, error), importProject func(string) (any, error)) *Handler {
	return &Handler{Public: public, pin: pin, revokePIN: revokePIN, Projects: store, Skills: reg, GitHub: github, GitHubConfigured: configured, Export: export, Import: importProject,
		sessions: map[string]session{}, failures: map[string]attempts{}, monitor: runtimemetrics.New()}
}
func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func key(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: age, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
func (h *Handler) security(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; script-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	w.Header().Set("Cache-Control", "no-store")
}

// La cabecera Origin puede ser modificada u omitida por los túneles
// Gradio FRP/Cloudflare. La autenticación del navegador descansa en una
// cookie __Host privada SameSite=Strict y un token CSRF aleatorio que debe
// coincidir con el formulario; nunca en Origin/Host reescritos por el proxy.
func ip(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
func (h *Handler) allowed(r *http.Request) bool {
	now := time.Now()
	k := ip(r)
	h.mu.Lock()
	defer h.mu.Unlock()
	for addr, v := range h.failures {
		if now.After(v.Until) {
			delete(h.failures, addr)
		}
	}
	if len(h.failures) > 1000 {
		return false
	}
	if v, ok := h.failures[k]; ok && v.Count >= 5 && now.Before(v.Until) {
		return false
	}
	if h.global.Count >= 50 && now.Before(h.global.Until) {
		return false
	}
	if now.After(h.global.Until) {
		h.global = attempts{Until: now.Add(15 * time.Minute)}
	}
	return true
}
func (h *Handler) fail(r *http.Request) {
	now := time.Now()
	k := ip(r)
	h.mu.Lock()
	defer h.mu.Unlock()
	v := h.failures[k]
	if now.After(v.Until) {
		v = attempts{Until: now.Add(15 * time.Minute)}
	}
	v.Count++
	h.failures[k] = v
	h.global.Count++
}
func (h *Handler) loginPage(w http.ResponseWriter, status int, message string) {
	csrf := random()
	cookie(w, "__Host-kagmcp-login", csrf, 600)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = pageTemplate.Execute(w, view{Login: true, CSRF: csrf, Message: message})
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if _, ok := h.authorize(r); ok {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		h.loginPage(w, http.StatusOK, "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "método no permitido; abre /login", http.StatusMethodNotAllowed)
		return
	}
	if !h.allowed(r) {
		w.Header().Set("Retry-After", "900")
		h.loginPage(w, http.StatusTooManyRequests, "Demasiados intentos. Vuelve a probar después de 15 minutos.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		slog.Warn("formulario de login inválido", "reason", "parse_or_size", "origin_present", r.Header.Get("Origin") != "")
		h.loginPage(w, http.StatusBadRequest, "No se pudo leer el formulario. Abre de nuevo la URL pública de KagMCP y prueba otra vez.")
		return
	}
	c, err := r.Cookie("__Host-kagmcp-login")
	if err != nil || c.Value == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.FormValue("csrf"))) != 1 {
		slog.Warn("formulario de login rechazado", "reason", "csrf_cookie_or_token", "cookie_present", err == nil, "origin_present", r.Header.Get("Origin") != "")
		h.loginPage(w, http.StatusForbidden, "No se pudo comprobar la cookie de seguridad. Abre la URL pública directamente en una pestaña nueva y permite sus cookies; vuelve a introducir el PIN.")
		return
	}
	// Un proxy HTTP legítimo puede cambiar Origin o suprimirlo; el CSRF
	// secreto verificado arriba impide que otra web envíe el formulario.
	if r.Header.Get("Origin") != h.Public {
		slog.Info("login recibido a través de proxy con Origin diferente", "origin_present", r.Header.Get("Origin") != "")
	}
	if !h.pin.Verify(r.FormValue("pin")) {
		h.fail(r)
		h.loginPage(w, http.StatusUnauthorized, "PIN incorrecto. Usa el PIN temporal de los logs de Kaggle o el que configuraste desde el panel.")
		return
	}
	token := random()
	csrf := random()
	h.mu.Lock()
	for id, current := range h.sessions {
		if time.Now().After(current.Expires) {
			delete(h.sessions, id)
		}
	}
	if len(h.sessions) >= 100 {
		h.mu.Unlock()
		http.Error(w, "demasiadas sesiones", 429)
		return
	}
	h.sessions[key(token)] = session{CSRF: csrf, Expires: time.Now().Add(sessionTTL)}
	if err := h.saveSessionsLocked(); err != nil {
		delete(h.sessions, key(token))
		h.mu.Unlock()
		http.Error(w, "No se pudo guardar la sesión segura", http.StatusInternalServerError)
		return
	}
	delete(h.failures, ip(r))
	h.mu.Unlock()
	cookie(w, "__Host-kagmcp-session", token, int(sessionTTL.Seconds()))
	cookie(w, "__Host-kagmcp-login", "", -1)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (h *Handler) authorize(r *http.Request) (session, bool) {
	c, err := r.Cookie("__Host-kagmcp-session")
	if err != nil {
		return session{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[key(c.Value)]
	if !ok || time.Now().After(s.Expires) {
		delete(h.sessions, key(c.Value))
		return session{}, false
	}
	return s, true
}
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) (session, bool) {
	s, ok := h.authorize(r)
	if !ok {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		} else {
			http.Error(w, "sesión requerida", 401)
		}
		return session{}, false
	}
	if r.Method == http.MethodPost {
		// Validación mediante cookie de sesión privada + CSRF ligado a ella;
		// no asumir Origin intacto a través de Gradio/Cloudflare.
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil || subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(s.CSRF)) != 1 {
			http.Error(w, "Formulario rechazado: token de seguridad inválido o expirado. Actualiza el panel y vuelve a intentarlo.", 403)
			return session{}, false
		}
	}
	return s, true
}

var pageDescriptions = map[string][2]string{
	"/dashboard":    {"Dashboard", "Estado de CPU, memoria, GPU y disco de Kaggle"},
	"/resources":    {"Recursos Kaggle", "Telemetría detallada del runtime, sin valores de demostración"},
	"/projects":     {"Proyectos", "Administra proyectos, tareas y memoria compartida"},
	"/agents":       {"Agentes", "Delegación opcional entre chats y revisión de entregas"},
	"/activity":     {"Actividad", "Registro de comandos ejecutados por los agentes"},
	"/integrations": {"Integraciones", "Tokens de GitHub y Hugging Face"},
	"/skills":       {"Skills", "Habilidades y documentación de los agentes"},
	"/security":     {"Seguridad", "Gestiona tu PIN y la sesión de administración"},
}

func isPage(path string) bool {
	if path == "/" {
		return true
	}
	_, ok := pageDescriptions[path]
	return ok
}

type view struct {
	Login        bool
	CSRF         string
	Message      string
	Projects     []projectstate.Project
	Activity     projectstate.HistoryPage
	AgentList    []agenttasks.Task
	AgentCurrent *agenttasks.Task
	AgentPrompt  string
	SkillList    []skills.Info
	SkillContent string
	SkillName    string
	NextOffset   int
	GitHubOn     bool
	Page         string
	PageTitle    string
	PageSubtitle string
	HFOn         bool
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.security(w)
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		h.serveAsset(w, r)
		return
	}
	if r.URL.Path == "/login" {
		h.login(w, r)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/dashboard" && r.URL.Path != "/resources" && r.URL.Path != "/projects" && r.URL.Path != "/agents" && r.URL.Path != "/agent-create" && r.URL.Path != "/agent-review" && r.URL.Path != "/agent-cancel" && r.URL.Path != "/activity" && r.URL.Path != "/integrations" && r.URL.Path != "/security" && r.URL.Path != "/skills" && r.URL.Path != "/project" && r.URL.Path != "/memory" && r.URL.Path != "/task" && r.URL.Path != "/task-status" && r.URL.Path != "/github" && r.URL.Path != "/huggingface" && r.URL.Path != "/api/metrics" && r.URL.Path != "/api/activity" && r.URL.Path != "/export" && r.URL.Path != "/import" && r.URL.Path != "/change-pin" && r.URL.Path != "/logout" {
		http.NotFound(w, r)
		return
	}
	sess, ok := h.Verify(w, r)
	if !ok {
		return
	}
	if r.URL.Path == "/api/metrics" {
		if r.Method != http.MethodGet {
			http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
			return
		}
		h.metrics(w, r)
		return
	}
	if r.URL.Path == "/api/activity" {
		if r.Method != http.MethodGet {
			http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
			return
		}
		page, err := activityPageNumber(r)
		if err != nil {
			http.Error(w, "página inválida", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(h.Projects.HistoryPage(page))
		return
	}
	if r.Method == http.MethodGet {
		if !isPage(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		page, err := activityPageNumber(r)
		if r.URL.Path == "/activity" && err != nil {
			http.Error(w, "página inválida", http.StatusBadRequest)
			return
		}
		pageName := r.URL.Path
		if pageName == "/" {
			pageName = "/dashboard"
		}
		meta := pageDescriptions[pageName]
		v := view{CSRF: sess.CSRF, Page: strings.TrimPrefix(pageName, "/"), PageTitle: meta[0], PageSubtitle: meta[1], GitHubOn: h.GitHubConfigured(), HFOn: h.HFConfigured != nil && h.HFConfigured()}
		if pageName == "/dashboard" || pageName == "/projects" {
			v.Projects = h.Projects.Snapshot().Projects
		}
		if pageName == "/activity" {
			v.Activity = h.Projects.HistoryPage(page)
		}
		if pageName == "/agents" && h.Agents != nil {
			v.AgentList = h.Agents.List()
			if id := r.URL.Query().Get("created"); id != "" {
				if task, e := h.Agents.Get(id); e == nil {
					v.AgentCurrent = &task
					v.AgentPrompt = agenttasks.Prompt(task)
				}
			}
		}
		if pageName == "/skills" {
			v.SkillList, _ = h.Skills.List()
			name := r.URL.Query().Get("name")
			if name == "" {
				name = skills.Active
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			body, err := h.Skills.Read(name, offset, 7000)
			if err != nil {
				http.Error(w, "skill no encontrada", 404)
				return
			}
			v.SkillName = name
			v.SkillContent = body["content"].(string)
			v.NextOffset = body["next_offset"].(int)
		}
		h.page(w, v)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "método inválido", 405)
		return
	}
	if r.URL.Path == "/change-pin" {
		if !h.allowed(r) {
			w.Header().Set("Retry-After", "900")
			http.Error(w, "intentos limitados", 429)
			return
		}
		err := h.pin.Change(r.FormValue("current_pin"), r.FormValue("new_pin"), h.revokePIN)
		if err != nil {
			h.fail(r)
			http.Error(w, err.Error(), 400)
			return
		}
		h.mu.Lock()
		h.sessions = make(map[string]session)
		if err := h.saveSessionsLocked(); err != nil {
			h.mu.Unlock()
			http.Error(w, "No se pudo revocar las sesiones", 500)
			return
		}
		h.failures = make(map[string]attempts)
		h.global = attempts{}
		h.mu.Unlock()
		cookie(w, "__Host-kagmcp-session", "", -1)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.URL.Path == "/logout" {
		c, _ := r.Cookie("__Host-kagmcp-session")
		if c != nil {
			h.mu.Lock()
			delete(h.sessions, key(c.Value))
			if err := h.saveSessionsLocked(); err != nil {
				h.mu.Unlock()
				http.Error(w, "No se pudo cerrar la sesión", 500)
				return
			}
			h.mu.Unlock()
		}
		cookie(w, "__Host-kagmcp-session", "", -1)
		http.Redirect(w, r, "/login", 303)
		return
	}
	var err error
	switch r.URL.Path {
	case "/agent-create":
		if h.Agents == nil {
			err = errors.New("delegación no disponible")
			break
		}
		spec := agenttasks.Spec{
			Title: r.FormValue("title"), Objective: r.FormValue("objective"), MCP: r.FormValue("mcp"),
			MCPID: r.FormValue("mcp_id"), Workspace: r.FormValue("workspace"), Branch: r.FormValue("branch"),
			AllowedPaths: strings.Fields(r.FormValue("allowed_paths")), Criteria: r.FormValue("criteria"), Context: r.FormValue("context"),
			UserApproved: r.FormValue("user_approved") == "yes",
		}
		var task agenttasks.Task
		task, err = h.Agents.Create(spec)
		if err == nil {
			http.Redirect(w, r, "/agents?created="+task.ID, http.StatusSeeOther)
			return
		}
	case "/agent-review":
		if h.Agents == nil {
			err = errors.New("revisión no disponible")
			break
		}
		_, err = h.Agents.Review(r.FormValue("id"), r.FormValue("decision"), r.FormValue("note"))
	case "/agent-cancel":
		if h.Agents == nil {
			err = errors.New("cancelación no disponible")
			break
		}
		_, err = h.Agents.Cancel(r.FormValue("id"), r.FormValue("note"))
	case "/project":
		err = h.Projects.Create(r.FormValue("id"), r.FormValue("name"), r.FormValue("repository"), r.FormValue("notebook"))
	case "/memory":
		err = h.Projects.Remember(r.FormValue("project"), r.FormValue("text"))
	case "/task":
		_, err = h.Projects.TaskAdd(r.FormValue("project"), r.FormValue("title"))
	case "/task-status":
		err = h.Projects.TaskSet(r.FormValue("project"), r.FormValue("task"), r.FormValue("status"))
	case "/export":
		if h.Export == nil {
			err = errors.New("exportación no disponible")
		} else {
			_, err = h.Export(r.FormValue("project"))
		}
	case "/import":
		if h.Import == nil {
			err = errors.New("restauración no disponible")
		} else {
			_, err = h.Import(r.FormValue("project"))
		}
	case "/huggingface":
		if h.HuggingFace == nil {
			err = errors.New("Hugging Face no disponible")
		} else {
			_, err = h.HuggingFace(r.Context(), r.FormValue("token"))
		}
	case "/github":
		if h.GitHub == nil {
			err = errors.New("GitHub no disponible")
		} else {
			_, err = h.GitHub(r.Context(), r.FormValue("token"))
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	redirect := map[string]string{
		"/github": "/integrations", "/huggingface": "/integrations",
		"/project": "/projects", "/memory": "/projects", "/task": "/projects", "/task-status": "/projects",
		"/export": "/projects", "/import": "/projects",
		"/agent-create": "/agents", "/agent-review": "/agents", "/agent-cancel": "/agents",
	}
	dest := redirect[r.URL.Path]
	if dest == "" {
		dest = "/dashboard"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// Shared validation for SSR links and the live JSON API.
// A page number of zero, negative or too large is not silently accepted.
func activityPageNumber(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("page")
	if raw == "" {
		return 1, nil
	}
	if len(raw) > 9 {
		return 0, errors.New("página inválida")
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 0, errors.New("página inválida")
	}
	return page, nil
}
func (h *Handler) page(w http.ResponseWriter, v view) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pageTemplate.Execute(w, v)
}

// Recursos autocontenidos: sin CDN ni dependencias externas durante Kaggle.
//
//go:embed ui/*
var uiFiles embed.FS

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	kind := ""
	switch name {
	case "axolotl.css", "dashboard.css":
		kind = "text/css; charset=utf-8"
	case "dashboard.js", "activity.js":
		kind = "application/javascript; charset=utf-8"
	case "axolotl.svg":
		kind = "image/svg+xml"
	default:
		http.NotFound(w, r)
		return
	}
	b, err := uiFiles.ReadFile("ui/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	report, err := gpumetrics.New().Metrics(ctx)
	if err != nil {
		report = gpumetrics.Report{Available: false, Status: "error", Reason: "No se pudieron leer las métricas GPU"}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"time":    time.Now().UTC(),
		"storage": diskguard.Usage("/kaggle/working"),
		"runtime": h.monitor.Read(),
		"gpu":     report,
	})
}

var pageData, _ = uiFiles.ReadFile("ui/page.html")
var pageTemplate = template.Must(template.New("dashboard").Parse(string(pageData)))
