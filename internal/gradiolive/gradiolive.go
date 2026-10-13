package gradiolive

// Gradio FRP nativo: TLS + yamux + mensajes FRP 0.44 compatibles.
// Sin ejecutar frpc, sin descargas binarias y sin instalar Gradio Python.
import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const apiURL = "https://api.gradio.app/v3/tunnel-request"
const startupTimeout = 45 * time.Second

type serverInfo struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	RootCA string `json:"root_ca"`
}
type Tunnel struct {
	URL      string
	cancel   context.CancelFunc
	finished chan struct{}
	once     sync.Once
	errMu    sync.RWMutex
	lastErr  error
}

func (t *Tunnel) Close() error {
	if t == nil {
		return nil
	}
	t.once.Do(func() { t.cancel() })
	select {
	case <-t.finished:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("túnel nativo no terminó en 5 segundos")
	}
}
func (t *Tunnel) Done() <-chan struct{} {
	if t == nil {
		return nil
	}
	return t.finished
}

// Err explains an unexpected native FRP session termination after Done closes.
// This never exposes credentials or the random proxy name.
func (t *Tunnel) Err() error {
	if t == nil {
		return nil
	}
	t.errMu.RLock()
	defer t.errMu.RUnlock()
	return t.lastErr
}

var gradioURL = regexp.MustCompile(`^https://[a-zA-Z0-9-]+\.gradio\.live/?$`)
var domain = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,251}[a-zA-Z0-9]$`)

func validHost(host string) bool {
	if len(host) > 253 || !domain.MatchString(host) || strings.Contains(host, "..") {
		return false
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified()
	}
	return !strings.EqualFold(host, "localhost") && strings.Contains(host, ".")
}
func client() *http.Client {
	return &http.Client{
		Timeout:       35 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("redirección Gradio inesperada") },
	}
}
func getServer(ctx context.Context) (serverInfo, error) {
	var result serverInfo
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return result, err
	}
	res, err := client().Do(req)
	if err != nil {
		return result, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return result, fmt.Errorf("Gradio API HTTP %d", res.StatusCode)
	}
	var servers []serverInfo
	if err = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&servers); err != nil || len(servers) == 0 {
		return result, errors.New("respuesta inválida de Gradio")
	}
	result = servers[0]
	if result.Port < 1 || result.Port > 65535 || !validHost(result.Host) {
		return serverInfo{}, errors.New("host/puerto FRP inválido")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(result.RootCA)) {
		return serverInfo{}, errors.New("CA TLS de Gradio inválida")
	}
	return result, nil
}
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !gradioURL.MatchString(raw) {
		return errors.New("URL gradio.live inesperada")
	}
	return nil
}

// Start publica un puerto local con el protocolo Gradio FRP nativo.
// El orquestador MCP comprueba posteriormente el endpoint HTTPS público.
func Start(ctx context.Context, localPort int) (*Tunnel, error) {
	if localPort < 1 || localPort > 65535 {
		return nil, errors.New("puerto local inválido")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := getServer(ctx)
	if err != nil {
		return nil, err
	}
	proxy, err := randomProxyName()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	ready := make(chan string, 1)
	result := make(chan error, 1)
	tunnel := &Tunnel{cancel: cancel, finished: make(chan struct{})}
	go func() {
		defer close(tunnel.finished)
		err := runNativeFRPSession(runCtx, info, localPort, proxy, func(link string) {
			select {
			case ready <- link:
			default:
			}
		})
		tunnel.errMu.Lock()
		tunnel.lastErr = err
		tunnel.errMu.Unlock()
		result <- err
	}()
	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	select {
	case public := <-ready:
		if err = validateURL(public); err != nil {
			_ = tunnel.Close()
			return nil, err
		}
		select {
		case failure := <-result:
			_ = tunnel.Close()
			return nil, fmt.Errorf("FRP se desconectó al publicar URL: %w", failure)
		default:
		}
		tunnel.URL = strings.TrimSuffix(public, "/")
		return tunnel, nil
	case failure := <-result:
		_ = tunnel.Close()
		if failure == nil {
			failure = errors.New("FRP terminó sin enlace")
		}
		return nil, failure
	case <-timer.C:
		_ = tunnel.Close()
		return nil, errors.New("timeout de Gradio FRP nativo (45 segundos)")
	case <-ctx.Done():
		_ = tunnel.Close()
		return nil, ctx.Err()
	}
}
