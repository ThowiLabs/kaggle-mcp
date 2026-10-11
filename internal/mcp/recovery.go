package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThowiLabs/kagssh-go/internal/config"
	"github.com/ThowiLabs/kagssh-go/internal/oauth"
)

const (
	tunnelCheckInterval    = 20 * time.Second
	tunnelFailureThreshold = 5
)

type tunnelProbe func(context.Context, string, string) error

// tunnelDone normalizes native Go FRP and cloudflared lifecycles.
// Always pair monitoring with tunnel.Close() to release cloudflared's watcher.
func tunnelDone(t publicTunnel) <-chan struct{} {
	if t == nil {
		return nil
	}
	if x, ok := t.(interface{ Done() <-chan struct{} }); ok {
		return x.Done()
	}
	if x, ok := t.(interface{ Done() <-chan error }); ok {
		out := make(chan struct{})
		go func() { <-x.Done(); close(out) }()
		return out
	}
	return nil
}

// watchPublicTunnel checks *public* routing and the exact per-process nonce,
// not merely that the local FRP TCP connection remains open.
func watchPublicTunnel(ctx context.Context, t publicTunnel, public, nonce string, interval time.Duration, threshold int, check tunnelProbe, log *slog.Logger) error {
	if t == nil {
		return errors.New("túnel no inicializado")
	}
	if interval <= 0 {
		interval = tunnelCheckInterval
	}
	if threshold <= 0 {
		threshold = tunnelFailureThreshold
	}
	done := tunnelDone(t)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if details, ok := t.(interface{ Err() error }); ok && details.Err() != nil {
				return fmt.Errorf("la sesión FRP terminó: %w", details.Err())
			}
			return errors.New("la sesión del túnel terminó mientras el servidor MCP seguía activo")
		case <-tick.C:
			probeCtx, cancel := context.WithTimeout(ctx, 9*time.Second)
			err := check(probeCtx, public, nonce)
			cancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil {
				if failures > 0 && log != nil {
					log.Info("enlace público recuperado tras fallos transitorios", "fallos_previos", failures)
				}
				failures = 0
				continue
			}
			failures++
			if log != nil {
				log.Warn("enlace público no verificable", "fallos_consecutivos", failures, "umbral", threshold, "error", err)
			}
			if failures >= threshold {
				return fmt.Errorf("enlace público caído (%d pruebas fallidas): %w", failures, err)
			}
		}
	}
}
func rebuildMCPForPublic(cfg config.Config, old *Server, public string) (*Server, error) {
	if old == nil || old.pinState == nil {
		return nil, errors.New("estado PIN de MCP ausente durante reconexión")
	}
	if strings.TrimSpace(public) == "" {
		return nil, errors.New("URL pública de reconexión vacía")
	}
	auth, err := oauth.New(oauth.Config{
		PublicURL: public, StateFile: filepath.Join(cfg.DataDir, "state", "oauth.json"),
		PINState: old.pinState, AgentMode: true,
		AccessTTL: time.Hour, RefreshTTL: 30 * 24 * time.Hour,
		PINMaxAttempts: 5, PINWindow: 5 * time.Minute, PINLockout: 15 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("renovar OAuth para enlace nuevo: %w", err)
	}
	old.githubMu.RLock()
	github := old.github
	githubOn := old.githubOn
	old.githubMu.RUnlock()
	old.hfMu.RLock()
	hfToken := old.hfToken
	old.hfMu.RUnlock()
	next := &Server{
		cfg: old.cfg, public: public, auth: auth, pinState: old.pinState,
		github: github, githubOn: githubOn, hfToken: hfToken, projects: old.projects, skills: old.skills,
	}
	return next, nil
}

// recoverPublicTunnel retries with exponential backoff. Each attempted URL
// must pass the nonce challenge before OAuth is attached. The old handler is
// deliberately removed before recovering: stale URLs must never advertise
// obsolete OAuth metadata or grant tokens for a different issuer.
func recoverPublicTunnel(ctx context.Context, cfg config.Config, old *Server, bridge *probeHandler, launch func(context.Context) (string, publicTunnel, string, error), delay func(context.Context, time.Duration) error, log *slog.Logger) (*Server, string, publicTunnel, string, error) {
	if old == nil || bridge == nil {
		return nil, "", nil, "", errors.New("sin servidor o puente MCP")
	}
	bridge.Set(nil)
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, "", nil, "", err
		}
		url, t, provider, err := launch(ctx)
		if err == nil {
			if t == nil {
				err = errors.New("el túnel retornó un manejador vacío")
			} else {
				var next *Server
				next, err = rebuildMCPForPublic(cfg, old, url)
				if err == nil {
					if ctx.Err() != nil {
						_ = t.Close()
						return nil, "", nil, "", ctx.Err()
					}
					bridge.Set(next.Handler())
					if log != nil {
						log.Warn("KagMCP restablecido con URL pública nueva; vuelve a conectar y autorizar ChatGPT", "url", url+"/mcp", "panel_web", url+"/", "provider", provider, "oauth_reauth_required", url != old.public)
					}
					return next, url, t, provider, nil
				}
			}
		}
		if t != nil {
			_ = t.Close()
		}
		if log != nil {
			log.Error("reconexión del túnel fallida; MCP local continúa activo", "intento", attempt, "error", err)
		}
		if err := delay(ctx, backoff); err != nil {
			return nil, "", nil, "", err
		}
		if backoff < 16*time.Second {
			backoff *= 2
		}
	}
}

// Private session status allows local Kaggle users to find the *new* URL
// without relying on an obsolete gradio.live hostname.
func writeCurrentTunnelStatus(cfg config.Config, public, provider, status string, log *slog.Logger) {
	if cfg.DataDir == "" {
		return
	}
	path := filepath.Join(cfg.DataDir, "state", "tunnel-status.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	// Values originate from validated Gradio/Cloudflare urls or the configured
	// public endpoint, not user-provided JSON.
	payload := fmt.Sprintf("{\"status\":%q,\"provider\":%q,\"mcp_url\":%q,\"updated_at\":%q}\n", status, provider, strings.TrimRight(public, "/")+"/mcp", time.Now().UTC().Format(time.RFC3339))
	temp, err := os.CreateTemp(filepath.Dir(path), ".tunnel-status-*")
	if err != nil {
		return
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.WriteString(payload)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil && log != nil {
		log.Warn("no se pudo actualizar estado de túnel", "error", err)
	}
}

type tunnelStarter func(context.Context) (string, publicTunnel, string, error)

// Goradio keeps trying after a temporary network outage. Startup likewise
// retries the configured provider until Kaggle is stopped, with bounded backoff.
func startTunnelWithRetry(ctx context.Context, start tunnelStarter, delay func(context.Context, time.Duration) error, log *slog.Logger) (string, publicTunnel, string, error) {
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", nil, "", err
		}
		url, t, provider, err := start(ctx)
		if err == nil && t != nil {
			return url, t, provider, nil
		}
		if t != nil {
			_ = t.Close()
		}
		if err == nil {
			err = errors.New("túnel vacío durante arranque")
		}
		if log != nil {
			log.Warn("arranque de túnel falló; reintentando sin detener Go", "intento", attempt, "error", err)
		}
		if e := delay(ctx, backoff); e != nil {
			return "", nil, "", e
		}
		if backoff < 16*time.Second {
			backoff *= 2
		}
	}
}
