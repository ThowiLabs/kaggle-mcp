package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ThowiLabs/kaggle-mcp/internal/config"
	"github.com/ThowiLabs/kaggle-mcp/internal/githubtoken"
)

func TestCredentialBridgePrivateSocketAndGitHelper(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "DUMMY_NOT_A_REAL_TOKEN")
	t.Setenv("HF_TOKEN", "")
	t.Setenv("KAGMCP_CREDENTIAL_SOCKET", "")
	t.Setenv("GIT_CONFIG_SYSTEM", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Server{cfg: config.Config{DataDir: t.TempDir()}, github: githubtoken.New(""), githubOn: true}
	if err := s.startCredentialBridge(ctx); err != nil {
		t.Fatal(err)
	}
	socket := os.Getenv("KAGMCP_CREDENTIAL_SOCKET")
	if socket == "" {
		t.Fatal("missing Unix socket")
	}
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe socket permissions: %v %v", info, err)
	}
	configFile := filepath.Join(s.cfg.DataDir, "state", "gitconfig")
	raw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "DUMMY_NOT_A_REAL_TOKEN") {
		t.Fatal("token leaked to Git config")
	}
	if !strings.Contains(string(raw), "credential") {
		t.Fatal("credential helper missing")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	resp, err := client.Get("http://kagmcp.local/git")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v struct {
		Token string `json:"token"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if v.Token != "DUMMY_NOT_A_REAL_TOKEN" {
		t.Fatal("bridge did not return mocked credential")
	}
	get := exec.Command("git", "config", "--get-urlmatch", "credential.helper", "https://github.com")
	output, err := get.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "-git-credential") {
		t.Fatalf("Git config not active: %s %v", output, err)
	}
	cancel()
}
func TestCredentialBridgeRejectsInvalidSync(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("HF_TOKEN", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Server{cfg: config.Config{DataDir: t.TempDir()}, github: githubtoken.New(""), githubOn: false}
	if err := s.startCredentialBridge(ctx); err != nil {
		t.Fatal(err)
	}
	if err := RunCredentialCommand("sync"); err != nil {
		t.Fatal(err)
	}
	// Invalid format cannot enter runtime environment.
	t.Setenv("GITHUB_TOKEN", "fake token with whitespace")
	if err := RunCredentialCommand("sync"); err == nil {
		t.Fatal("unsafe token accepted")
	}
}
