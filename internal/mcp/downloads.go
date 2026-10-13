package mcp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxDownloadSize int64 = 2 << 30

type sharedFile struct {
	path     string
	original os.FileInfo
	expires  time.Time
}
type fileDownloads struct {
	mu    sync.Mutex
	items map[string]sharedFile
}

func newFileDownloads() *fileDownloads { return &fileDownloads{items: make(map[string]sharedFile)} }
func (d *fileDownloads) purge(now time.Time) {
	for key, entry := range d.items {
		if !now.Before(entry.expires) {
			delete(d.items, key)
		}
	}
}
func safeDownloadPath(path string) bool {
	if path == "" {
		return false
	}
	for _, segment := range strings.Split(filepath.ToSlash(filepath.Clean(path)), "/") {
		// Disallow any instance state directory, git internals and typical private env files.
		if strings.HasPrefix(segment, ".") {
			return false
		}
	}
	return true
}
func (s *Server) invokeDownload(name string, raw json.RawMessage) (any, error) {
	var a struct {
		Path  string `json:"path"`
		Hours int    `json:"hours"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, errors.New("argumentos de descarga inválidos")
	}
	if name == "file_download_revoke" {
		u, err := url.Parse(a.URL)
		if err != nil || u.Scheme != "https" || !strings.HasPrefix(u.Path, "/download/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("URL de descarga inválida")
		}
		token := strings.TrimPrefix(u.Path, "/download/")
		s.downloads.mu.Lock()
		defer s.downloads.mu.Unlock()
		if _, ok := s.downloads.items[token]; !ok {
			return nil, errors.New("enlace inexistente o caducado")
		}
		delete(s.downloads.items, token)
		return map[string]any{"revoked": true}, nil
	}
	if a.Hours == 0 {
		a.Hours = 1
	}
	if a.Hours < 1 || a.Hours > 24 {
		return nil, errors.New("vigencia permitida: 1 a 24 horas")
	}
	path, err := s.path(a.Path, false)
	if err != nil {
		return nil, err
	}
	if !safeDownloadPath(path) {
		return nil, errors.New("ruta privada no publicable")
	}
	// Refuse symlinks, including ancestors, rather than trust a mutable link target.
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if path != real || !safeDownloadPath(real) {
		return nil, errors.New("no se admiten enlaces simbólicos para descarga")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxDownloadSize {
		return nil, errors.New("solo archivos regulares de máximo 2 GiB")
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	now := time.Now()
	expires := now.Add(time.Duration(a.Hours) * time.Hour)
	s.downloads.mu.Lock()
	defer s.downloads.mu.Unlock()
	s.downloads.purge(now)
	if len(s.downloads.items) >= 100 {
		return nil, errors.New("máximo 100 enlaces temporales activos")
	}
	s.downloads.items[token] = sharedFile{path: path, original: info, expires: expires}
	return map[string]any{"url": strings.TrimRight(s.public, "/") + "/download/" + token, "filename": filepath.Base(path), "size": info.Size(), "expires_at": expires.UTC().Format(time.RFC3339), "warning": "Quien tenga esta URL puede descargar el archivo hasta su vencimiento"}, nil
}
func (s *Server) serveDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	token := r.PathValue("token")
	if len(token) != 43 {
		http.NotFound(w, r)
		return
	}
	s.downloads.mu.Lock()
	s.downloads.purge(time.Now())
	item, exists := s.downloads.items[token]
	s.downloads.mu.Unlock()
	if !exists || !safeDownloadPath(item.path) {
		http.NotFound(w, r)
		return
	}
	// Checking the link and file identity again prevents a swapped symlink/file
	// from silently serving a different artifact after the link was issued.
	real, err := filepath.EvalSymlinks(item.path)
	if err != nil || real != item.path {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(item.path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, item.original) || info.Size() != item.original.Size() || !info.ModTime().Equal(item.original.ModTime()) {
		http.NotFound(w, r)
		return
	}
	header := mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(item.path)})
	if header == "" {
		header = "attachment"
	}
	w.Header().Set("Content-Disposition", header)
	w.Header().Set("Content-Type", "application/octet-stream")
	// Solo descargas: ampliar el límite de escritura del servidor (65 s)
	// para archivos grandes, sin permitir conexiones sin límite de tiempo.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
	http.ServeContent(w, r, filepath.Base(item.path), info.ModTime(), f)
}
