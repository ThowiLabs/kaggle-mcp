package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ThowiLabs/kagssh-go/internal/gradiolive"
	"github.com/ThowiLabs/kagssh-go/internal/quicktunnel"
)

type publicTunnel interface {
	Close() error
}
type tunnelLaunch func(context.Context) (string, publicTunnel, error)

type probeHandler struct {
	mu     sync.RWMutex
	actual http.Handler
	nonce  string
}

func newProbeHandler() (*probeHandler, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	return &probeHandler{nonce: "KagMCP-" + base64.RawURLEncoding.EncodeToString(b[:])}, nil
}
func (p *probeHandler) Set(handler http.Handler) { p.mu.Lock(); p.actual = handler; p.mu.Unlock() }
func (p *probeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/health" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.WriteString(w, p.nonce)
		return
	}
	p.mu.RLock()
	h := p.actual
	p.mu.RUnlock()
	if h == nil {
		http.Error(w, "KagMCP inicializando", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}
func healthCheck(ctx context.Context, public, nonce string) error {
	c := &http.Client{Timeout: 9 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return errors.New("redirección no permitida en health")
	}}
	return healthCheckWithClient(ctx, public, nonce, c)
}

// Permite probar la respuesta real de un servidor TLS local sin desactivar
// la verificación de certificados en el cliente de producción.
func healthCheckWithClient(ctx context.Context, public, nonce string, c *http.Client) error {
	if !strings.HasPrefix(public, "https://") {
		return errors.New("túnel sin HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(public, "/")+"/api/health", nil)
	if err != nil {
		return err
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("health público: HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil {
		return err
	}
	if string(body) != nonce {
		return errors.New("el enlace público no llega al KagMCP correcto")
	}
	return nil
}
func checkRepeated(ctx context.Context, url, nonce string) error {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		child, cancel := context.WithTimeout(ctx, 9*time.Second)
		last = healthCheck(child, url, nonce)
		cancel()
		if last == nil {
			return nil
		}
		if attempt < 4 {
			timer := time.NewTimer(1500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return fmt.Errorf("túnel sin respuesta confirmada: %w", last)
}
func chooseTunnel(ctx context.Context, mode string, retries int, gradio, cloudflare tunnelLaunch, check func(context.Context, string) error, delay func(context.Context, time.Duration) error, log *slog.Logger) (string, publicTunnel, string, error) {
	if retries <= 0 {
		retries = 3
	}
	if retries > 5 {
		return "", nil, "", errors.New("máximo 5 intentos Gradio")
	}
	try := func(label string, start tunnelLaunch) (string, publicTunnel, error) {
		public, t, err := start(ctx)
		if err != nil {
			return "", nil, err
		}
		if t == nil {
			return "", nil, errors.New("túnel vacío")
		}
		if err = check(ctx, public); err != nil {
			_ = t.Close()
			return "", nil, err
		}
		return public, t, nil
	}
	if mode == "auto" || mode == "gradio" {
		for attempt := 1; attempt <= retries; attempt++ {
			if err := ctx.Err(); err != nil {
				return "", nil, "", err
			}
			public, t, err := try("gradio", gradio)
			if err == nil {
				return public, t, "gradio", nil
			}
			if log != nil {
				log.Warn("Gradio no disponible", "intento", attempt, "max_intentos", retries, "error", err)
			}
			if attempt < retries {
				if err := delay(ctx, time.Duration(attempt)*2*time.Second); err != nil {
					return "", nil, "", err
				}
			}
		}
		if mode == "gradio" {
			return "", nil, "", fmt.Errorf("Gradio falló tras %d intentos y modo explícito no permite fallback", retries)
		}
		if log != nil {
			log.Warn("iniciando automáticamente Cloudflare tras fallos de Gradio")
		}
	}
	if mode == "auto" || mode == "cloudflare" {
		public, t, err := try("cloudflare", cloudflare)
		if err != nil {
			return "", nil, "", fmt.Errorf("Cloudflare no pudo establecer túnel: %w", err)
		}
		return public, t, "cloudflare", nil
	}
	return "", nil, "", fmt.Errorf("túnel desconocido %q", mode)
}
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func startTunnel(ctx context.Context, mode string, retries, port int, origin, nonce string, log *slog.Logger) (string, publicTunnel, string, error) {
	gradio := func(ctx context.Context) (string, publicTunnel, error) {
		t, err := gradiolive.Start(ctx, port)
		if err != nil {
			return "", nil, err
		}
		return t.URL, t, nil
	}
	cloudflare := func(ctx context.Context) (string, publicTunnel, error) {
		t, err := quicktunnel.Start(ctx, origin)
		if err != nil {
			return "", nil, err
		}
		return t.URL, t, nil
	}
	probe := func(ctx context.Context, public string) error { return checkRepeated(ctx, public, nonce) }
	return chooseTunnel(ctx, mode, retries, gradio, cloudflare, probe, sleepContext, log)
}
