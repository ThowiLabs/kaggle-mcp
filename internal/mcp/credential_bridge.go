package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type credentialRequest struct {
	GitHub *string `json:"github,omitempty"`
	HF     *string `json:"hf,omitempty"`
}

// RunCredentialCommand is invoked from the Bash prompt hook and Git's helper.
// Tokens are transferred exclusively over a private Unix socket.
func RunCredentialCommand(mode string) error {
	socket := os.Getenv("KAGMCP_CREDENTIAL_SOCKET")
	if socket == "" {
		return errors.New("enlace local no disponible")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 17 * time.Second}
	switch mode {
	case "sync":
		gh, hf := os.Getenv("GITHUB_TOKEN"), os.Getenv("HF_TOKEN")
		body, _ := json.Marshal(credentialRequest{GitHub: &gh, HF: &hf})
		resp, err := client.Post("http://kagmcp.local/sync", "application/json", bytes.NewReader(body))
		if err != nil {
			return errors.New("no se pudieron sincronizar credenciales")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("credenciales rechazadas (HTTP %d)", resp.StatusCode)
		}
		return nil
	case "git-credential":
		if len(os.Args) < 3 || os.Args[2] != "get" {
			return nil
		}
		attrs, err := io.ReadAll(io.LimitReader(os.Stdin, 8192))
		if err != nil {
			return err
		}
		host, protocol := "", ""
		for _, line := range strings.Split(string(attrs), "\n") {
			if strings.HasPrefix(line, "host=") {
				host = strings.TrimPrefix(line, "host=")
			}
			if strings.HasPrefix(line, "protocol=") {
				protocol = strings.TrimPrefix(line, "protocol=")
			}
		}
		if host != "github.com" || protocol != "https" {
			return nil
		}
		resp, err := client.Get("http://kagmcp.local/git")
		if err != nil {
			return errors.New("credenciales Git no disponibles")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil
		}
		var result struct {
			Token string `json:"token"`
		}
		if err = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result); err != nil {
			return err
		}
		if result.Token != "" {
			_, err = fmt.Fprintf(os.Stdout, "username=x-access-token\npassword=%s\n\n", result.Token)
		}
		return err
	}
	return errors.New("comando interno no admitido")
}

func (s *Server) startCredentialBridge(ctx context.Context) error {
	dir := filepath.Join(s.cfg.DataDir, "state")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	socket := filepath.Join(dir, "credentials.sock")
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	if err = os.Chmod(socket, 0600); err != nil {
		ln.Close()
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sync", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var v credentialRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&v); err != nil || (v.GitHub == nil && v.HF == nil) {
			http.Error(w, "solicitud inválida", http.StatusBadRequest)
			return
		}
		if v.GitHub != nil {
			if _, err := s.configureGitHub(r.Context(), *v.GitHub); err != nil {
				http.Error(w, "token GitHub no verificado", http.StatusUnprocessableEntity)
				return
			}
		}
		if v.HF != nil {
			if _, err := s.configureHF(r.Context(), *v.HF); err != nil {
				http.Error(w, "token HF no verificado", http.StatusUnprocessableEntity)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /git", func(w http.ResponseWriter, r *http.Request) {
		if !s.githubConfigured() {
			http.NotFound(w, r)
			return
		}
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 4 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second}
	go func() { _ = server.Serve(ln) }()
	cleanup := func() { _ = server.Close(); _ = ln.Close(); _ = os.Remove(socket) }
	executable, err := os.Executable()
	if err != nil {
		cleanup()
		return err
	}
	gitConfig := filepath.Join(dir, "gitconfig")
	helper := "!" + strconv.Quote(executable) + " -git-credential"
	configure := exec.Command("git", "config", "--file", gitConfig, "credential.https://github.com.helper", helper)
	if out, err := configure.CombinedOutput(); err != nil {
		cleanup()
		return fmt.Errorf("configurar Git: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(gitConfig, 0600); err != nil {
		cleanup()
		return err
	}
	if err := os.Setenv("KAGMCP_CREDENTIAL_SOCKET", socket); err != nil {
		cleanup()
		return err
	}
	if err := os.Setenv("GIT_CONFIG_SYSTEM", gitConfig); err != nil {
		cleanup()
		return err
	}
	go func() { <-ctx.Done(); cleanup() }()
	return nil
}
