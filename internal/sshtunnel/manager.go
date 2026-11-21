package sshtunnel

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ErrHostKeyMismatch indica que la clave del host SSH cambió respecto a
// la huella guardada en la primera conexión (TOFU).
var ErrHostKeyMismatch = errors.New("la clave del host SSH no coincide con la registrada")

// Status es lo único que el panel muestra del túnel. Nunca contiene la
// contraseña ni el PIN.
type Status struct {
	Configured    bool      `json:"configured"`
	Enabled       bool      `json:"enabled"`
	Connected     bool      `json:"connected"`
	PINConfigured bool      `json:"pin_configured"`
	User          string    `json:"user"`
	Host          string    `json:"host"`
	SSHPort       int       `json:"ssh_port"`
	RemotePort    int       `json:"remote_port"`
	AssignedPort  int       `json:"assigned_port,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// Manager mantiene el túnel SSH inverso en segundo plano. La contraseña
// descifrada vive solo en memoria mientras el proceso corre; al reiniciar
// hay que descifrarla de nuevo con el PIN.
type Manager struct {
	mu            sync.Mutex
	dataDir       string
	localPort     int
	nonce         string
	log           *slog.Logger
	stored        Stored
	hasStored     bool
	password      string
	hasPassword   bool
	pinConfigured bool

	runCtx   context.Context
	cancel   context.CancelFunc
	running  bool
	gen      int
	client   *ssh.Client
	listener net.Listener

	assignedPort int
	connected    bool
	lastErr      string
}

// New crea el gestor sin arrancar nada. log puede ser nil.
func New(dataDir string, localPort int, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{dataDir: dataDir, localPort: localPort, log: log}
}

// SetNonce fija el nonce de /api/health usado para verificar el túnel.
// Debe llamarse antes de Start.
func (m *Manager) SetNonce(nonce string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nonce = nonce
}

// SetPINConfigured indica si el arranque usó un PIN propio (persistente).
// Con PIN generado el túnel no puede descifrarse tras reiniciar.
func (m *Manager) SetPINConfigured(configured bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pinConfigured = configured
}

// SetContext fija el contexto de larga duración (el del servidor) del
// que cuelga el bucle de reconexión. Debe llamarse antes de Start;
// el contexto de una petición del panel no sirve: muere con la respuesta.
func (m *Manager) SetContext(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runCtx = ctx
}

// Load lee la configuración guardada en disco, si existe.
func (m *Manager) Load() (bool, error) {
	stored, ok, err := Load(m.dataDir)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stored = stored
	m.hasStored = ok
	return ok, nil
}

// Unlock descifra la contraseña guardada con el PIN. No arranca el túnel.
func (m *Manager) Unlock(pin string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasStored {
		return errors.New("túnel SSH no configurado")
	}
	password, err := m.stored.Decrypt(pin)
	if err != nil {
		return err
	}
	m.password = password
	m.hasPassword = true
	return nil
}

// Configure guarda una configuración nueva (o reemplaza la anterior),
// cifra la contraseña con el PIN y arranca o detiene el túnel según
// enabled. Cambiar de host o puerto SSH reinicia la confianza TOFU:
// la huella anterior no vale para otro destino.
func (m *Manager) Configure(pin, user, host string, sshPort, remotePort int, password string, enabled bool) (Status, error) {
	if err := ValidateInput(user, host, sshPort, remotePort, password); err != nil {
		return m.Status(), err
	}
	m.mu.Lock()
	fingerprint := m.stored.HostKeySHA256
	if !m.hasStored || m.stored.Host != host || m.stored.SSHPort != sshPort {
		fingerprint = ""
	}
	m.mu.Unlock()
	stored, err := Encrypt(pin, user, host, sshPort, remotePort, password, enabled, fingerprint)
	if err != nil {
		return m.Status(), err
	}
	if err := stored.Save(m.dataDir); err != nil {
		return m.Status(), err
	}
	m.mu.Lock()
	m.stored = stored
	m.hasStored = true
	m.password = password
	m.hasPassword = true
	m.mu.Unlock()
	if enabled {
		m.Stop()
		if err := m.Start(); err != nil {
			return m.Status(), err
		}
	} else {
		m.Stop()
	}
	return m.Status(), nil
}

// Start arranca el bucle de reconexión en segundo plano si la
// configuración existe, está habilitada y la contraseña está descifrada.
// Usa el contexto fijado con SetContext. No bloquea y nunca tumba el
// arranque: un túnel caído solo registra advertencias y reintenta.
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	if !m.hasStored {
		m.mu.Unlock()
		return errors.New("túnel SSH no configurado")
	}
	if !m.stored.Enabled {
		m.mu.Unlock()
		return errors.New("túnel SSH deshabilitado")
	}
	if !m.hasPassword {
		m.mu.Unlock()
		return errors.New("sin contraseña descifrada: vuelve a guardar la configuración del túnel SSH")
	}
	if m.nonce == "" {
		m.mu.Unlock()
		return errors.New("túnel SSH sin nonce de salud")
	}
	if m.runCtx == nil {
		m.mu.Unlock()
		return errors.New("túnel SSH sin contexto de ejecución")
	}
	runCtx, cancel := context.WithCancel(m.runCtx)
	m.cancel = cancel
	m.running = true
	m.gen++
	gen := m.gen
	m.mu.Unlock()
	go m.run(runCtx, gen)
	return nil
}

// Stop detiene el bucle y cierra la conexión activa, sin tocar disco.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	client := m.client
	listener := m.listener
	m.cancel = nil
	m.client = nil
	m.listener = nil
	m.running = false
	m.connected = false
	m.assignedPort = 0
	m.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	if client != nil {
		_ = client.Close()
	}
	if cancel != nil {
		cancel()
	}
}

// Disable detiene el túnel y persiste el deshabilitado. No borra nada:
// la configuración se puede volver a habilitar o reemplazar.
func (m *Manager) Disable() (Status, error) {
	m.Stop()
	m.mu.Lock()
	if !m.hasStored {
		m.mu.Unlock()
		return m.Status(), errors.New("túnel SSH no configurado")
	}
	m.stored.Enabled = false
	stored := m.stored
	m.mu.Unlock()
	if err := stored.Save(m.dataDir); err != nil {
		return m.Status(), err
	}
	return m.Status(), nil
}

// Enable persiste el habilitado y arranca el túnel con la contraseña
// que ya está en memoria.
func (m *Manager) Enable() (Status, error) {
	m.mu.Lock()
	if !m.hasStored {
		m.mu.Unlock()
		return m.Status(), errors.New("túnel SSH no configurado")
	}
	if !m.hasPassword {
		m.mu.Unlock()
		return m.Status(), errors.New("sin contraseña descifrada: vuelve a guardar la configuración del túnel SSH")
	}
	m.stored.Enabled = true
	stored := m.stored
	m.mu.Unlock()
	if err := stored.Save(m.dataDir); err != nil {
		return m.Status(), err
	}
	if err := m.Start(); err != nil {
		return m.Status(), err
	}
	return m.Status(), nil
}

// Status devuelve el estado actual, sin secretos.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{
		Configured: m.hasStored, Enabled: m.hasStored && m.stored.Enabled,
		Connected: m.connected, PINConfigured: m.pinConfigured,
		LastError: m.lastErr,
	}
	if m.hasStored {
		st.User = m.stored.User
		st.Host = m.stored.Host
		st.SSHPort = m.stored.SSHPort
		st.RemotePort = m.stored.RemotePort
		st.UpdatedAt = m.stored.UpdatedAt
	}
	if m.connected {
		st.AssignedPort = m.assignedPort
	}
	return st
}

// Test comprueba en una sola conexión que las credenciales funcionan,
// que el reenvío remoto se establece y que /api/health responde a
// través del túnel con el nonce esperado. No guarda nada ni persiste
// huellas: solo verifica.
func (m *Manager) Test(ctx context.Context, user, host string, sshPort, remotePort int, password string) (int, error) {
	if err := ValidateInput(user, host, sshPort, remotePort, password); err != nil {
		return 0, err
	}
	m.mu.Lock()
	nonce := m.nonce
	storedFP := ""
	checkFP := false
	if m.hasStored && m.stored.Host == host && m.stored.SSHPort == sshPort && m.stored.HostKeySHA256 != "" {
		storedFP = m.stored.HostKeySHA256
		checkFP = true
	}
	m.mu.Unlock()
	if nonce == "" {
		return 0, errors.New("túnel SSH sin nonce de salud")
	}
	client, err := m.dial(ctx, user, host, sshPort, password, storedFP, checkFP, false)
	if err != nil {
		return 0, err
	}
	defer client.Close()
	listener, err := client.Listen("tcp", "0.0.0.0:"+strconv.Itoa(remotePort))
	if err != nil {
		return 0, fmt.Errorf("no se pudo abrir el reenvío remoto: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port == 0 {
		_ = listener.Close()
		return 0, errors.New("el servidor SSH no asignó puerto remoto")
	}
	// Como en connectOnce: sin Accept, la conexión de salud nunca se
	// proxea y el chequeo siempre agota su tiempo.
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go m.proxyConn(conn)
		}
	}()
	err = checkThroughTunnel(ctx, host, addr.Port, nonce)
	_ = listener.Close()
	<-acceptDone
	if err != nil {
		return 0, err
	}
	return addr.Port, nil
}

func (m *Manager) run(ctx context.Context, gen int) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		err := m.connectOnce(ctx)
		m.mu.Lock()
		// Solo la generación vigente toca el estado: un bucle viejo que
		// termina tras un reinicio no debe apagar el estado del nuevo.
		if m.gen == gen {
			m.connected = false
			m.assignedPort = 0
			if err != nil && ctx.Err() == nil {
				if errors.Is(err, ErrHostKeyMismatch) {
					m.lastErr = "la clave del host SSH no coincide con la registrada"
				} else {
					m.lastErr = "túnel SSH inverso no disponible"
				}
			}
		}
		m.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.log.Warn("túnel SSH inverso caído; reintentando", "error", err)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 16*time.Second {
			backoff *= 2
		}
	}
}

// connectOnce establece una sesión SSH, pide el reenvío remoto y sirve
// conexiones hasta que la sesión se cierre. Devuelve nil solo si el
// contexto ordenó parar mientras todo funcionaba.
func (m *Manager) connectOnce(ctx context.Context) error {
	m.mu.Lock()
	user := m.stored.User
	host := m.stored.Host
	sshPort := m.stored.SSHPort
	remotePort := m.stored.RemotePort
	password := m.password
	nonce := m.nonce
	m.mu.Unlock()
	client, err := m.dial(ctx, user, host, sshPort, password, "", false, true)
	if err != nil {
		return err
	}
	listener, err := client.Listen("tcp", "0.0.0.0:"+strconv.Itoa(remotePort))
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("no se pudo abrir el reenvío remoto: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port == 0 {
		_ = listener.Close()
		_ = client.Close()
		return errors.New("el servidor SSH no asignó puerto remoto")
	}
	m.mu.Lock()
	m.client = client
	m.listener = listener
	m.assignedPort = addr.Port
	m.lastErr = ""
	m.mu.Unlock()
	// La limpieza es por sesión: solo borra el estado si sigue apuntando
	// a esta sesión, para no cerrar el túnel nuevo tras un reinicio.
	defer m.endSession(client, listener)
	// El bucle de Accept debe arrancar ANTES del health-check: sin él,
	// la conexión de salud nunca se proxea al MCP local y el chequeo
	// siempre agota su tiempo. Peor: con el canal de reenvío lleno y
	// nadie aceptando, cerrar el listener se bloquea contra el mutex
	// de reenvíos de x/crypto y el gestor queda atascado para siempre.
	acceptDone := make(chan error, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				acceptDone <- err
				return
			}
			go m.proxyConn(conn)
		}
	}()
	if err := checkThroughTunnel(ctx, host, addr.Port, nonce); err != nil {
		_ = listener.Close()
		_ = client.Close()
		<-acceptDone
		return err
	}
	m.mu.Lock()
	if ctx.Err() == nil {
		m.connected = true
	}
	m.mu.Unlock()
	m.log.Info("túnel SSH inverso activo", "host", host, "puerto_remoto", addr.Port)
	go m.keepalive(ctx, client)
	if err := <-acceptDone; err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}

func (m *Manager) endSession(client *ssh.Client, listener net.Listener) {
	_ = listener.Close()
	_ = client.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client == client {
		m.client = nil
	}
	if m.listener == listener {
		m.listener = nil
	}
	if m.client == nil {
		m.connected = false
		m.assignedPort = 0
	}
}

func (m *Manager) keepalive(ctx context.Context, client *ssh.Client) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				_ = client.Close()
				return
			}
		}
	}
}

func (m *Manager) proxyConn(remote net.Conn) {
	local, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(m.localPort), 5*time.Second)
	if err != nil {
		_ = remote.Close()
		return
	}
	done := make(chan struct{}, 2)
	copy := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go copy(local, remote)
	go copy(remote, local)
	<-done
	_ = remote.Close()
	_ = local.Close()
}

// dial abre la conexión SSH. Si checkFP es true, exige la huella guardada;
// si persist es true, guarda la huella en la primera conexión (TOFU) y
// exige la misma en las siguientes.
func (m *Manager) dial(ctx context.Context, user, host string, sshPort int, password, expectedFP string, checkFP, persist bool) (*ssh.Client, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(sshPort))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("conectar con %s: %w", host, err)
	}
	callback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		fp := Fingerprint(key)
		if checkFP {
			if subtle.ConstantTimeCompare([]byte(fp), []byte(expectedFP)) != 1 {
				return ErrHostKeyMismatch
			}
			return nil
		}
		if !persist {
			return nil
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.stored.HostKeySHA256 == "" {
			m.stored.HostKeySHA256 = fp
			if m.hasStored {
				if err := m.stored.Save(m.dataDir); err != nil {
					return fmt.Errorf("guardar huella de host SSH: %w", err)
				}
			}
			return nil
		}
		if subtle.ConstantTimeCompare([]byte(fp), []byte(m.stored.HostKeySHA256)) != 1 {
			return ErrHostKeyMismatch
		}
		return nil
	}
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: callback,
		Timeout:         10 * time.Second,
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

// checkThroughTunnel verifica de extremo a extremo que el reenvío llega
// a este proceso: pide /api/health al host remoto por el puerto
// reenviado y exige el nonce exacto.
func checkThroughTunnel(ctx context.Context, host string, remotePort int, nonce string) error {
	if nonce == "" {
		return errors.New("túnel SSH sin nonce de salud")
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 9 * time.Second}
			return dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(remotePort)))
		},
	}
	client := &http.Client{Timeout: 9 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirección no permitida en health")
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/api/health", nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("salud por túnel SSH: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("salud por túnel SSH: HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil {
		return err
	}
	if string(body) != nonce {
		return errors.New("el túnel SSH no llega al Kaggle MCP correcto")
	}
	return nil
}
