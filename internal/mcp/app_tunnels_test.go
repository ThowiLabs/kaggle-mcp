package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type fakeAppTunnel struct {
	closed bool
	done   chan struct{}
}

func (f *fakeAppTunnel) Close() error {
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	return nil
}
func (f *fakeAppTunnel) Done() <-chan struct{} { return f.done }
func portFromURL(t *testing.T, address string) int {
	t.Helper()
	host, port, err := net.SplitHostPort(strings.TrimPrefix(address, "http://"))
	if err != nil || host != "127.0.0.1" {
		t.Fatal(address, err)
	}
	n, _ := strconv.Atoi(port)
	return n
}
func TestNamedAppTunnelsLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	port := portFromURL(t, server.URL)
	manager := newAppTunnels(context.Background())
	var handles []*fakeAppTunnel
	fail := false
	manager.start = func(_ context.Context, p int) (string, publicTunnel, error) {
		if p != port {
			t.Fatal("puerto distinto")
		}
		if fail {
			return "", nil, errors.New("Gradio no disponible")
		}
		f := &fakeAppTunnel{done: make(chan struct{})}
		handles = append(handles, f)
		return "https://test.gradio.live", f, nil
	}
	if _, err := manager.create("admin", port, port); err == nil {
		t.Fatal("se expuso el puerto MCP")
	}
	if _, err := manager.create("../bad", port, 9999); err == nil {
		t.Fatal("nombre inseguro")
	}
	item, err := manager.create("mi_app", port, 9999)
	if err != nil || item.URL == "" {
		t.Fatal(item, err)
	}
	if _, err := manager.create("mi_app", port, 9999); err == nil {
		t.Fatal("nombre duplicado")
	}
	if list := manager.list(); len(list) != 1 || list[0].Name != "mi_app" {
		t.Fatalf("lista: %+v", list)
	}
	fail = true
	if _, err := manager.regenerate("mi_app", 9999); err == nil || handles[0].closed {
		t.Fatal("regeneración fallida destruyó túnel anterior")
	}
	fail = false
	if _, err := manager.regenerate("mi_app", 9999); err != nil || !handles[0].closed {
		t.Fatal("no rotó el túnel", err)
	}
	if err := manager.delete("mi_app"); err != nil || !handles[1].closed {
		t.Fatal("no cerró túnel", err)
	}
	if _, err := manager.regenerate("mi_app", 9999); err == nil {
		t.Fatal("regeneración de túnel borrado")
	}
	if len(manager.list()) != 0 {
		t.Fatal("persistió túnel borrado")
	}
}
func TestAppTunnelToolsRegistered(t *testing.T) {
	s := newTestServer(t)
	list, err := s.invokeAppTunnels(context.Background(), "app_tunnel_list", nil)
	if err != nil || len(list.([]appTunnel)) != 0 {
		t.Fatal(err, list)
	}
	raw, _ := json.Marshal(s.definitions())
	for _, name := range []string{"app_tunnel_create", "app_tunnel_list", "app_tunnel_regenerate", "app_tunnel_delete", "file_download_link", "file_download_revoke"} {
		if !strings.Contains(string(raw), `"name":"`+name+`"`) {
			t.Fatalf("falta herramienta %s", name)
		}
	}
}
func TestAppTunnelStopAndLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	port := portFromURL(t, server.URL)
	m := newAppTunnels(context.Background())
	handles := []*fakeAppTunnel{}
	m.start = func(context.Context, int) (string, publicTunnel, error) {
		f := &fakeAppTunnel{done: make(chan struct{})}
		handles = append(handles, f)
		return "https://test.gradio.live", f, nil
	}
	for _, name := range []string{"uno", "dos", "tres", "cuatro", "cinco"} {
		if _, err := m.create(name, port, 9999); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.create("seis", port, 9999); err == nil {
		t.Fatal("límite ignorado")
	}
	m.closeAll()
	for _, f := range handles {
		if !f.closed {
			t.Fatal("túnel sin cerrar")
		}
	}
	if len(m.list()) != 0 {
		t.Fatal("registro no vaciado")
	}
}
