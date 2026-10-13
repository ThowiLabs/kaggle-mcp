package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadLinkProtectedAndRevocable(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.zip")
	if err := os.WriteFile(path, []byte("archivo prueba"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("A", 43)
	s.downloads.items[token] = sharedFile{path: path, original: info, expires: time.Now().Add(time.Hour)}
	request := func(method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/download/"+token, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request("GET"); w.Code != 200 || w.Body.String() != "archivo prueba" || !strings.Contains(w.Header().Get("Content-Disposition"), "demo.zip") {
		t.Fatalf("descarga: %d %q", w.Code, w.Body.String())
	}
	if w := request("HEAD"); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("HEAD debe ser vacío", w.Code)
	}
	if w := httptest.NewRecorder(); w.Code != 200 {
		t.Fatal("recorder inválido")
	}
	revoke, _ := json.Marshal(map[string]any{"url": s.public + "/download/" + token})
	if _, err := s.invokeDownload("file_download_revoke", revoke); err != nil {
		t.Fatal(err)
	}
	if w := request("GET"); w.Code != 404 {
		t.Fatalf("enlace revocado sigue sirviendo: %d", w.Code)
	}
	s.downloads.items[token] = sharedFile{path: path, original: info, expires: time.Now().Add(-time.Second)}
	if w := request("GET"); w.Code != 404 {
		t.Fatal("enlace caducado no bloqueado")
	}
}
func TestDownloadRefusesSwappedFileAndPrivatePaths(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	_ = os.WriteFile(path, []byte("original"), 0600)
	info, _ := os.Stat(path)
	token := strings.Repeat("B", 43)
	s.downloads.items[token] = sharedFile{path: path, original: info, expires: time.Now().Add(time.Hour)}
	_ = os.Remove(path)
	_ = os.WriteFile(path, []byte("otro"), 0600)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/download/"+token, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("intercambio de archivo permitido: %d", w.Code)
	}
	for _, path := range []string{"/kaggle/working/.kagmcp/state/secret", "/kaggle/working/my/.git/config", "/kaggle/working/.env", "/kaggle/working/.kagmcp-qa/state/secret"} {
		if safeDownloadPath(path) {
			t.Errorf("ruta privada permitida: %s", path)
		}
	}
	if _, err := s.invokeDownload("file_download_link", json.RawMessage(`{"path":"/etc/passwd"}`)); err == nil {
		t.Fatal("se permitió descarga fuera de Kaggle")
	}
}
