package sshtunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fakeSSHServer es un servidor SSH mínimo solo para tests: acepta una
// contraseña, atiende reenvíos remotos (tcpip-forward) y reenvía cada
// conexión entrante al cliente por un canal forwarded-tcpip.
type fakeSSHServer struct {
	t        *testing.T
	listener net.Listener
	conn     ssh.Conn
	password string
	signer   ssh.Signer
	mu       sync.Mutex
	forwards []net.Listener
	closed   bool
}

type tcpipForwardRequest struct {
	BindAddr string
	BindPort uint32
}

type forwardedTCPIPEndpoint struct {
	Host       string
	Port       uint32
	OriginAddr string
	OriginPort uint32
}

func newFakeSSHServer(t *testing.T, addr, password string) *fakeSSHServer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSSHServer{t: t, listener: listener, password: password, signer: signer}
	go s.serve()
	return s
}

func (s *fakeSSHServer) Addr() string { return s.listener.Addr().String() }

func (s *fakeSSHServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeSSHServer) handle(netConn net.Conn) {
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "usuario" && string(pass) == s.password {
				return nil, nil
			}
			return nil, fmt.Errorf("contraseña incorrecta")
		},
	}
	config.AddHostKey(s.signer)
	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, config)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.conn = sshConn
	s.mu.Unlock()
	go s.handleRequests(sshConn, reqs)
	for ch := range chans {
		// El cliente de este paquete no abre canales directos; se rechazan.
		_ = ch.Reject(ssh.UnknownChannelType, "no se aceptan canales directos")
	}
}

func (s *fakeSSHServer) handleRequests(conn *ssh.ServerConn, reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type != "tcpip-forward" {
			_ = req.Reply(false, nil)
			continue
		}
		var payload tcpipForwardRequest
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
			_ = req.Reply(false, nil)
			continue
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			_ = req.Reply(false, nil)
			continue
		}
		s.mu.Lock()
		s.forwards = append(s.forwards, ln)
		s.mu.Unlock()
		port := uint32(ln.Addr().(*net.TCPAddr).Port)
		if err := req.Reply(true, ssh.Marshal(struct{ Port uint32 }{Port: port})); err != nil {
			_ = ln.Close()
			continue
		}
		// El canal reenviado debe llevar la dirección y el puerto asignado
		// reales: el cliente los compara con el listener registrado y
		// rechaza el canal si no coinciden (el puerto pedido era 0).
		go s.serveForward(conn, ln, payload.BindAddr, port)
	}
}

func (s *fakeSSHServer) serveForward(conn *ssh.ServerConn, ln net.Listener, bindAddr string, bindPort uint32) {
	for {
		remote, err := ln.Accept()
		if err != nil {
			return
		}
		go func(remote net.Conn) {
			defer remote.Close()
			originHost, originPortStr, _ := net.SplitHostPort(remote.RemoteAddr().String())
			originPort, _ := strconv.Atoi(originPortStr)
			endpoint := forwardedTCPIPEndpoint{
				Host: bindAddr, Port: bindPort,
				OriginAddr: originHost, OriginPort: uint32(originPort),
			}
			ch, reqs, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(endpoint))
			if err != nil {
				return
			}
			go ssh.DiscardRequests(reqs)
			proxyPair(remote, ch)
		}(remote)
	}
}

func proxyPair(a, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	copy := func(dst io.WriteCloser, src io.Reader) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go copy(a, b)
	go copy(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
}

func (s *fakeSSHServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	_ = s.listener.Close()
	for _, ln := range s.forwards {
		_ = ln.Close()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
}

// localHTTP simula el servidor MCP local: /api/health responde el nonce.
func localHTTP(t *testing.T, nonce string) (*httptest.Server, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			_, _ = io.WriteString(w, nonce)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return srv, port
}

// testHTTPClient pone tiempo límite a las peticiones de los tests: un
// túnel atascado debe fallar la prueba, no colgar la suite.
var testHTTPClient = &http.Client{Timeout: 10 * time.Second}

func waitConnected(t *testing.T, m *Manager, timeout time.Duration) Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st := m.Status()
		if st.Connected && st.AssignedPort > 0 {
			return st
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("el túnel no conectó en %v: %+v", timeout, m.Status())
	return Status{}
}

func testManager(t *testing.T, localPort int, nonce string) *Manager {
	t.Helper()
	m := New(t.TempDir(), localPort, nil)
	m.SetNonce(nonce)
	m.SetContext(context.Background())
	return m
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

func TestManagerEndToEndThroughTunnel(t *testing.T) {
	const nonce = "Kaggle MCP-nonce-de-prueba"
	_, localPort := localHTTP(t, nonce)
	server := newFakeSSHServer(t, "127.0.0.1:0", "contraseña")
	defer server.Close()
	host, sshPort := splitHostPort(t, server.Addr())

	m := testManager(t, localPort, nonce)
	if _, err := m.Configure(testPIN, "usuario", host, sshPort, 0, "contraseña", true); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	st := waitConnected(t, m, 5*time.Second)
	if st.RemotePort != 0 {
		t.Fatalf("puerto remoto solicitado=%d", st.RemotePort)
	}
	// La salud llega de extremo a extremo por el túnel reenviado.
	res, err := testHTTPClient.Get("http://" + host + ":" + strconv.Itoa(st.AssignedPort) + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != nonce {
		t.Fatalf("salud por túnel=%q", body)
	}
	// La huella quedó guardada (TOFU) y la contraseña no está en claro.
	raw, err := os.ReadFile(Path(m.dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "contraseña") {
		t.Fatal("el archivo guardado contiene la contraseña en claro")
	}
	if !strings.Contains(string(raw), "host_key_sha256") {
		t.Fatal("la huella TOFU no quedó guardada")
	}
}

func TestManagerWrongPasswordDoesNotConnect(t *testing.T) {
	const nonce = "Kaggle MCP-nonce-de-prueba"
	_, localPort := localHTTP(t, nonce)
	server := newFakeSSHServer(t, "127.0.0.1:0", "contraseña")
	defer server.Close()
	host, sshPort := splitHostPort(t, server.Addr())

	m := testManager(t, localPort, nonce)
	if _, err := m.Configure(testPIN, "usuario", host, sshPort, 0, "equivocada", true); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	time.Sleep(1500 * time.Millisecond)
	if st := m.Status(); st.Connected || st.LastError == "" {
		t.Fatalf("con contraseña incorrecta no debe conectar: %+v", st)
	}
}

func TestManagerHostKeyMismatch(t *testing.T) {
	const nonce = "Kaggle MCP-nonce-de-prueba"
	_, localPort := localHTTP(t, nonce)
	first := newFakeSSHServer(t, "127.0.0.1:0", "contraseña")
	addr := first.Addr()
	host, sshPort := splitHostPort(t, addr)

	dir := t.TempDir()
	m1 := New(dir, localPort, nil)
	m1.SetNonce(nonce)
	m1.SetContext(context.Background())
	if _, err := m1.Configure(testPIN, "usuario", host, sshPort, 0, "contraseña", true); err != nil {
		t.Fatal(err)
	}
	waitConnected(t, m1, 5*time.Second)
	m1.Stop()
	first.Close()

	// Mismo host y puerto, otra clave de host: debe rechazar.
	second := newFakeSSHServer(t, addr, "contraseña")
	defer second.Close()
	m2 := New(dir, localPort, nil)
	m2.SetNonce(nonce)
	m2.SetContext(context.Background())
	if _, err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m2.Unlock(testPIN); err != nil {
		t.Fatal(err)
	}
	if err := m2.Start(); err != nil {
		t.Fatal(err)
	}
	defer m2.Stop()
	time.Sleep(1500 * time.Millisecond)
	st := m2.Status()
	if st.Connected {
		t.Fatal("con otra clave de host no debe conectar")
	}
	if !strings.Contains(st.LastError, "clave del host") {
		t.Fatalf("LastError=%q, debe mencionar la clave del host", st.LastError)
	}
}

func TestManagerTestMethod(t *testing.T) {
	const nonce = "Kaggle MCP-nonce-de-prueba"
	_, localPort := localHTTP(t, nonce)
	server := newFakeSSHServer(t, "127.0.0.1:0", "contraseña")
	defer server.Close()
	host, sshPort := splitHostPort(t, server.Addr())

	m := testManager(t, localPort, nonce)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	port, err := m.Test(ctx, "usuario", host, sshPort, 0, "contraseña")
	if err != nil {
		t.Fatal(err)
	}
	if port == 0 {
		t.Fatal("la prueba no devolvió puerto asignado")
	}
	// Test ya verifica la salud de extremo a extremo por el túnel y
	// cierra el reenvío al devolver; no se puede volver a pedir por
	// ese puerto.
	if _, err := m.Test(ctx, "usuario", host, sshPort, 0, "equivocada"); err == nil {
		t.Fatal("la prueba con contraseña incorrecta debe fallar")
	}
}

func TestManagerStartWithoutConfig(t *testing.T) {
	m := testManager(t, 8181, "nonce")
	if err := m.Start(); err == nil {
		t.Fatal("sin configuración no debe arrancar")
	}
	if st := m.Status(); st.Configured || st.Connected {
		t.Fatalf("estado sin configurar: %+v", st)
	}
}

func TestManagerDisableEnable(t *testing.T) {
	const nonce = "Kaggle MCP-nonce-de-prueba"
	_, localPort := localHTTP(t, nonce)
	server := newFakeSSHServer(t, "127.0.0.1:0", "contraseña")
	defer server.Close()
	host, sshPort := splitHostPort(t, server.Addr())

	m := testManager(t, localPort, nonce)
	if _, err := m.Configure(testPIN, "usuario", host, sshPort, 0, "contraseña", true); err != nil {
		t.Fatal(err)
	}
	waitConnected(t, m, 5*time.Second)
	st, err := m.Disable()
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || st.Connected {
		t.Fatalf("tras deshabilitar: %+v", st)
	}
	st, err = m.Enable()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled {
		t.Fatalf("tras habilitar: %+v", st)
	}
	waitConnected(t, m, 5*time.Second)
	m.Stop()
}
