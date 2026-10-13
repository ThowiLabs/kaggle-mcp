package oauth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type pinAttempt struct {
	windowStart  time.Time
	failures     int
	blockedUntil time.Time
}

type pinLimiter struct {
	mu          sync.Mutex
	attempts    map[string]pinAttempt
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
}

func newPINLimiter(maxAttempts int, window, lockout time.Duration) *pinLimiter {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if window <= 0 {
		window = 5 * time.Minute
	}
	if lockout <= 0 {
		lockout = 15 * time.Minute
	}
	return &pinLimiter{
		attempts:    make(map[string]pinAttempt),
		maxAttempts: maxAttempts,
		window:      window,
		lockout:     lockout,
	}
}

func (l *pinLimiter) blocked(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.attempts[key]
	if !ok {
		return false, 0
	}
	if now.Before(a.blockedUntil) {
		return true, a.blockedUntil.Sub(now)
	}
	if !a.blockedUntil.IsZero() || now.Sub(a.windowStart) >= l.window {
		delete(l.attempts, key)
	}
	return false, 0
}

func (l *pinLimiter) failure(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	a := l.attempts[key]
	if now.Before(a.blockedUntil) {
		return true, a.blockedUntil.Sub(now)
	}
	if a.windowStart.IsZero() || now.Sub(a.windowStart) >= l.window {
		a = pinAttempt{windowStart: now}
	}
	a.failures++
	if a.failures >= l.maxAttempts {
		a.blockedUntil = now.Add(l.lockout)
		l.attempts[key] = a
		return true, l.lockout
	}
	l.attempts[key] = a
	return false, 0
}

func (l *pinLimiter) success(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(strings.TrimSpace(host))
	if peer != nil && peer.IsLoopback() {
		if forwarded := firstForwardedIP(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			return forwarded
		}
		if realIP := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); realIP != nil {
			return realIP.String()
		}
	}
	if peer != nil {
		return peer.String()
	}
	return strings.TrimSpace(host)
}

func firstForwardedIP(value string) string {
	if value == "" {
		return ""
	}
	first, _, _ := strings.Cut(value, ",")
	ip := net.ParseIP(strings.TrimSpace(first))
	if ip == nil {
		return ""
	}
	return ip.String()
}
