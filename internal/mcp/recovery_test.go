package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ThowiLabs/kagssh-go/internal/githubtoken"
)

type supervisedFakeTunnel struct {
	done   chan struct{}
	closed int
}

func (t *supervisedFakeTunnel) Done() <-chan struct{} { return t.done }
func (t *supervisedFakeTunnel) Close() error {
	t.closed++
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	return nil
}

func TestSupervisorDetectsClosedFRPControlImmediately(t *testing.T) {
	tun := &supervisedFakeTunnel{done: make(chan struct{})}
	checks := 0
	check := func(context.Context, string, string) error { checks++; return nil }
	finished := make(chan error, 1)
	go func() {
		finished <- watchPublicTunnel(context.Background(), tun, "https://one.gradio.live", "nonce", time.Hour, 5, check, nil)
	}()
	close(tun.done)
	select {
	case e := <-finished:
		if e == nil || !strings.Contains(e.Error(), "terminó") {
			t.Fatalf("no detectó conexión FRP cerrada: %v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor no detectó cierre FRP")
	}
	if checks != 0 {
		t.Fatal("consulta health innecesaria tras caída de control")
	}
}
func TestSupervisorRepeatsPublicChecksAndToleratesTransientFailures(t *testing.T) {
	tun := &supervisedFakeTunnel{done: make(chan struct{})}
	checks := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	check := func(context.Context, string, string) error {
		checks++
		switch checks {
		case 1, 2, 4, 5, 6, 7, 8:
			return errors.New("gradio 404 No interface is running")
		}
		return nil
	}
	result := watchPublicTunnel(ctx, tun, "https://one.gradio.live", "nonce", time.Millisecond, 5, check, nil)
	if result == nil || !strings.Contains(result.Error(), "5 pruebas fallidas") {
		t.Fatalf("fallos no acumulados/reiniciados: %v, total lecturas=%d", result, checks)
	}
	if checks != 8 {
		t.Fatalf("debe resetear fallos al recuperar enlace: %d", checks)
	}
}
func TestSupervisorCancelledAndNeverClosesTunnelWithoutController(t *testing.T) {
	tun := &supervisedFakeTunnel{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := watchPublicTunnel(ctx, tun, "https://x.gradio.live", "nonce", time.Millisecond, 5, healthCheck, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("contexto cancelado: %v", err)
	}
	if tun.closed != 0 {
		t.Fatal("watcher cerró túnel dos veces")
	}
}
func TestReconnectedServerPreservesChangedPINAndProjectsButRenewsOAuth(t *testing.T) {
	old := newTestServer(t)
	oldToken := issueAccessTokenForTest(t, old)
	if err := old.pinState.Change("LONG-TEST-PIN-ABC", "NEW-PIN-UNIQUE-123", old.auth.ResetOwner); err != nil {
		t.Fatal(err)
	}
	old.githubMu.Lock()
	old.github = githubtoken.New("fake-github-pat")
	old.githubOn = true
	old.githubMu.Unlock()
	old.hfMu.Lock()
	old.hfToken = "hf_test_reconnect"
	old.hfMu.Unlock()
	renewed, err := rebuildMCPForPublic(old.cfg, old, "https://second.gradio.live")
	if err != nil {
		t.Fatal(err)
	}
	if renewed.pinState != old.pinState || !renewed.pinState.Verify("NEW-PIN-UNIQUE-123") || renewed.pinState.Verify("LONG-TEST-PIN-ABC") {
		t.Fatal("se perdió PIN modificado")
	}
	if old.projects != renewed.projects || old.agents != renewed.agents || old.skills.Dir != renewed.skills.Dir {
		t.Fatal("se perdió memoria/proyectos/skills")
	}
	if renewed.githubClient() != old.githubClient() || !renewed.githubConfigured() {
		t.Fatal("se perdió PAT GitHub de sesión")
	}
	if !renewed.hfConfigured() || renewed.hfToken != "hf_test_reconnect" {
		t.Fatal("se perdió token Hugging Face de sesión")
	}
	if renewed.public != "https://second.gradio.live" {
		t.Fatal("OAuth no tiene URL actual")
	}
	req := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	renewed.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "https://second.gradio.live/oauth/token") {
		t.Fatalf("metadata OAuth antigua: %s", rec.Body.String())
	}
	authReq := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	authReq.Header.Set("Authorization", "Bearer "+oldToken)
	authReq.Header.Set("Content-Type", "application/json")
	rejected := httptest.NewRecorder()
	renewed.Handler().ServeHTTP(rejected, authReq)
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("aceptó token antiguo: %d", rejected.Code)
	}
	freshToken := issueAccessTokenForTest(t, renewed, "NEW-PIN-UNIQUE-123")
	fresh := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	fresh.Header.Set("Authorization", "Bearer "+freshToken)
	fresh.Header.Set("Content-Type", "application/json")
	accepted := httptest.NewRecorder()
	renewed.Handler().ServeHTTP(accepted, fresh)
	if accepted.Code != 200 {
		t.Fatalf("OAuth nuevo no autorizó herramientas: %d %s", accepted.Code, accepted.Body.String())
	}
}
func TestReconnectRetriesAndPublishesOnlyVerifiedNewMCP(t *testing.T) {
	old := newTestServer(t)
	bridge, e := newProbeHandler()
	if e != nil {
		t.Fatal(e)
	}
	bridge.Set(old.Handler())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	newTunnel := &supervisedFakeTunnel{done: make(chan struct{})}
	attempts := 0
	delays := 0
	launch := func(context.Context) (string, publicTunnel, string, error) {
		attempts++
		if attempts < 3 {
			request := httptest.NewRequest("GET", "/", nil)
			reply := httptest.NewRecorder()
			bridge.ServeHTTP(reply, request)
			if reply.Code != 503 {
				t.Errorf("OAuth servido durante reconexión: %d", reply.Code)
			}
			return "", nil, "", errors.New("FRP 503 / flapping")
		}
		return "https://new.gradio.live", newTunnel, "gradio", nil
	}
	wait := func(context.Context, time.Duration) error { delays++; return nil }
	next, public, tun, provider, e := recoverPublicTunnel(ctx, old.cfg, old, bridge, launch, wait, nil)
	if e != nil {
		t.Fatal(e)
	}
	if attempts != 3 || delays != 2 || tun != newTunnel || provider != "gradio" || public != "https://new.gradio.live" {
		t.Fatalf("reconexión: %s %s %d/%d %v", provider, public, attempts, delays, e)
	}
	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/mcp", nil)
	rec := httptest.NewRecorder()
	bridge.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "https://new.gradio.live/mcp") {
		t.Fatalf("metadatos viejos: %d %s", rec.Code, rec.Body.String())
	}
	if next.pinState != old.pinState {
		t.Fatal("PIN no reutilizado")
	}
}
func TestReconnectCancellationStopsAfterBackoff(t *testing.T) {
	old := newTestServer(t)
	bridge, _ := newProbeHandler()
	ctx, cancel := context.WithCancel(context.Background())
	attempt := 0
	launch := func(context.Context) (string, publicTunnel, string, error) {
		attempt++
		return "", nil, "", errors.New("red caída")
	}
	delay := func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }
	_, _, _, _, e := recoverPublicTunnel(ctx, old.cfg, old, bridge, launch, delay, nil)
	if !errors.Is(e, context.Canceled) || attempt != 1 {
		t.Fatalf("no respetó cancelación: %v intentos %d", e, attempt)
	}
}
func TestCurrentTunnelStatusUpdatesPrivately(t *testing.T) {
	old := newTestServer(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	writeCurrentTunnelStatus(old.cfg, "https://old.gradio.live", "gradio", "online", log)
	writeCurrentTunnelStatus(old.cfg, "https://new.gradio.live", "gradio", "reconnecting", log)
	path := filepath.Join(old.cfg.DataDir, "state", "tunnel-status.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Status string `json:"status"`
		MCPURL string `json:"mcp_url"`
	}
	if err = json.Unmarshal(content, &data); err != nil {
		t.Fatal(err)
	}
	if data.Status != "reconnecting" || data.MCPURL != "https://new.gradio.live/mcp" {
		t.Fatalf("estado del túnel obsoleto: %+v", data)
	}
}

func TestStartupRetriesTemporaryTunnelFailures(t *testing.T) {
	attempts := 0
	healthy := &supervisedFakeTunnel{done: make(chan struct{})}
	start := func(context.Context) (string, publicTunnel, string, error) {
		attempts++
		if attempts < 3 {
			return "", nil, "", errors.New("DNS temporal")
		}
		return "https://ready.gradio.live", healthy, "gradio", nil
	}
	waits := []time.Duration{}
	delay := func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	url, tun, provider, err := startTunnelWithRetry(context.Background(), start, delay, nil)
	if err != nil || url != "https://ready.gradio.live" || tun != healthy || provider != "gradio" || attempts != 3 {
		t.Fatalf("arranque con reintentos incorrecto: %v %d", err, attempts)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 2*time.Second {
		t.Fatalf("backoff no creciente: %v", waits)
	}
}
func TestStartupRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	start := func(context.Context) (string, publicTunnel, string, error) {
		return "", nil, "", errors.New("Gradio sin red")
	}
	delay := func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	_, _, _, err := startTunnelWithRetry(ctx, start, delay, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("no respeta cancelación: %v", err)
	}
}

func TestNewPublicURLRevokesPreviouslyValidOAuthToken(t *testing.T) {
	old := newTestServer(t)
	token := issueAccessTokenForTest(t, old)
	next, e := rebuildMCPForPublic(old.cfg, old, "https://rotated.gradio.live")
	if e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"tools/list","params":{}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rejected := httptest.NewRecorder()
	next.Handler().ServeHTTP(rejected, req)
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("token del hostname anterior aún válido: %d", rejected.Code)
	}
	if !next.pinState.Verify("LONG-TEST-PIN-ABC") {
		t.Fatal("PIN existente modificado durante renovación")
	}
	reauthorized := issueAccessTokenForTest(t, next)
	req2 := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"tools/list","params":{}}`))
	req2.Header.Set("Authorization", "Bearer "+reauthorized)
	req2.Header.Set("Content-Type", "application/json")
	accepted := httptest.NewRecorder()
	next.Handler().ServeHTTP(accepted, req2)
	if accepted.Code != 200 {
		t.Fatalf("token nuevo no permite descubrir: %d %s", accepted.Code, accepted.Body.String())
	}
}
