package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeTunnel struct{ closed int }

func (f *fakeTunnel) Close() error { f.closed++; return nil }
func TestAutomaticFallbackAfterThreeFailedGradioAttempts(t *testing.T) {
	ctx := context.Background()
	calls := 0
	closings := []*fakeTunnel{}
	gradio := func(context.Context) (string, publicTunnel, error) {
		calls++
		f := &fakeTunnel{}
		closings = append(closings, f)
		return "https://bad.gradio.live", f, nil
	}
	fallbackCalls := 0
	cloudflare := func(context.Context) (string, publicTunnel, error) {
		fallbackCalls++
		return "https://good.trycloudflare.com", &fakeTunnel{}, nil
	}
	check := func(_ context.Context, url string) error {
		if strings.Contains(url, "bad.") {
			return errors.New("health 504")
		}
		return nil
	}
	sleeps := 0
	delay := func(_ context.Context, d time.Duration) error { sleeps++; return nil }
	url, tun, provider, err := chooseTunnel(ctx, "auto", 3, gradio, cloudflare, check, delay, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || url != "https://good.trycloudflare.com" || provider != "cloudflare" || fallbackCalls != 1 || calls != 3 || sleeps != 2 {
		t.Fatalf("fallback incorrecto: url=%s provider=%s calls=%d fallback=%d waits=%d err=%v", url, provider, calls, fallbackCalls, sleeps, err)
	}
	for _, f := range closings {
		if f.closed != 1 {
			t.Fatalf("Gradio fallido no cerrado: %+v", f)
		}
	}
	if err = tun.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestExplicitProviderNeverFallsBack(t *testing.T) {
	bad := func(context.Context) (string, publicTunnel, error) { return "", nil, errors.New("not available") }
	cloudCalled := 0
	cf := func(context.Context) (string, publicTunnel, error) {
		cloudCalled++
		return "https://cf.trycloudflare.com", &fakeTunnel{}, nil
	}
	pass := func(context.Context, string) error { return nil }
	delay := func(context.Context, time.Duration) error { return nil }
	if _, _, _, err := chooseTunnel(context.Background(), "gradio", 2, bad, cf, pass, delay, nil); err == nil || cloudCalled != 0 {
		t.Fatal("Gradio explícito activó Cloudflare")
	}
	if _, _, provider, err := chooseTunnel(context.Background(), "cloudflare", 2, bad, cf, pass, delay, nil); err != nil || provider != "cloudflare" || cloudCalled != 1 {
		t.Fatal("Cloudflare explícito no respetado")
	}
}
func TestHealthyGradioNeverStartsCloudflare(t *testing.T) {
	cfCalls := 0
	gradio := func(context.Context) (string, publicTunnel, error) {
		return "https://valid.gradio.live", &fakeTunnel{}, nil
	}
	cf := func(context.Context) (string, publicTunnel, error) { cfCalls++; return "", nil, errors.New("fail") }
	url, _, provider, err := chooseTunnel(context.Background(), "auto", 3, gradio, cf, func(context.Context, string) error { return nil }, func(context.Context, time.Duration) error { return nil }, nil)
	if err != nil || provider != "gradio" || url != "https://valid.gradio.live" || cfCalls != 0 {
		t.Fatalf("Gradio no prioritario: %v", err)
	}
}
func TestHealthProbeReturnsNonceAndBlocksOtherRoutesDuringStartup(t *testing.T) {
	p, err := newProbeHandler()
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(p)
	defer s.Close()
	get, err := http.Get(s.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(get.Body)
	get.Body.Close()
	if err != nil || string(body) != p.nonce {
		t.Fatal("nonce health inválido")
	}
	pre, _ := http.Get(s.URL + "/")
	if pre.StatusCode != 503 {
		t.Fatalf("OAuth expuesto antes de inicializar: %d", pre.StatusCode)
	}
	pre.Body.Close()
	p.Set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	after, _ := http.Get(s.URL + "/")
	if after.StatusCode != 200 {
		t.Fatalf("servidor no activado: %d", after.StatusCode)
	}
	after.Body.Close()
}
func TestPublicHealthRejectsWrongServerAndRedirect(t *testing.T) {
	nonce := "secret-nonce"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("ruta inesperada: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, "wrong server")
	}))
	defer server.Close()
	if err := healthCheckWithClient(context.Background(), server.URL, nonce, server.Client()); err == nil {
		t.Fatal("aceptó nonce de otro proceso")
	}
	valid := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, nonce) }))
	defer valid.Close()
	if err := healthCheckWithClient(context.Background(), valid.URL, nonce, valid.Client()); err != nil {
		t.Fatalf("rechazó health exacto bajo TLS: %v", err)
	}
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/other", 302) }))
	defer redirect.Close()
	c := redirect.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("sin redirecciones") }
	if err := healthCheckWithClient(context.Background(), redirect.URL, nonce, c); err == nil {
		t.Fatal("aceptó redirección")
	}
}
