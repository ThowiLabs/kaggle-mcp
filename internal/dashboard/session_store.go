package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const sessionTTL = 7 * 24 * time.Hour
const maxSessionFile = 64 << 10

// EnablePersistence keeps only cookie-token SHA-256 digests and CSRF, never
// the bearer token or the user's PIN. State is private to the Kaggle runtime.
// A rotated Gradio URL has a different cookie domain, so cannot restore it.
func (h *Handler) EnablePersistence(path string) error {
	if path == "" {
		return errors.New("ruta de sesiones vacía")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionFile = path
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("leer sesiones: %w", err)
	}
	if len(content) > maxSessionFile {
		return errors.New("sesiones demasiado grandes")
	}
	var saved map[string]session
	if err := json.Unmarshal(content, &saved); err != nil {
		return fmt.Errorf("sesiones inválidas: %w", err)
	}
	now := time.Now()
	for id, item := range saved {
		if len(id) == 64 && len(item.CSRF) >= 32 && now.Before(item.Expires) && len(h.sessions) < 100 {
			h.sessions[id] = item
		}
	}
	return nil
}
func (h *Handler) saveSessionsLocked() error {
	if h.sessionFile == "" {
		return nil
	}
	dir := filepath.Dir(h.sessionFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	now := time.Now()
	for id, s := range h.sessions {
		if !s.Expires.After(now) {
			delete(h.sessions, id)
		}
	}
	raw, err := json.Marshal(h.sessions)
	if err != nil {
		return err
	}
	if len(raw) > maxSessionFile {
		return errors.New("sesiones demasiado grandes")
	}
	file, err := os.CreateTemp(dir, ".sessions-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), h.sessionFile)
}
