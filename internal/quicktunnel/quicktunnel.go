package quicktunnel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	startupTimeout        = 30 * time.Second
	managedBinaryMaxAge   = 30 * 24 * time.Hour
	maxBinaryBytes        = 128 << 20
	maxReleaseMetadata    = 4 << 20
	latestReleaseEndpoint = "https://api.github.com/repos/cloudflare/cloudflared/releases/latest"
)

var quickURLPattern = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.(?:com|app)`)

type Tunnel struct {
	URL    string
	cancel context.CancelFunc
	done   <-chan error
}

func Binary(ctx context.Context) (string, error) {
	return cloudflaredBinary(ctx)
}

func ListNamed(ctx context.Context) (string, error) {
	binary, err := Binary(ctx)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, binary, "tunnel", "list")
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return "", fmt.Errorf("cloudflared tunnel list: %w", err)
		}
		return text, fmt.Errorf("cloudflared tunnel list: %w", err)
	}
	return text, nil
}

func Start(ctx context.Context, origin string) (*Tunnel, error) {
	if err := validateOrigin(origin); err != nil {
		return nil, err
	}
	binary, err := cloudflaredBinary(ctx)
	if err != nil {
		return nil, err
	}

	processCtx, cancel := context.WithCancel(ctx)
	collector := newOutputCollector()
	cmd := exec.CommandContext(processCtx, binary, "tunnel", "--no-autoupdate", "--url", origin)
	cmd.Stdout = collector
	cmd.Stderr = collector
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()

	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	select {
	case publicURL := <-collector.urlCh:
		return &Tunnel{URL: publicURL, cancel: cancel, done: done}, nil
	case err := <-done:
		cancel()
		if err == nil {
			err = errors.New("cloudflared exited before publishing a URL")
		}
		return nil, fmt.Errorf("start temporary tunnel: %w: %s", err, collector.diagnostic())
	case <-timer.C:
		cancel()
		return nil, fmt.Errorf("start temporary tunnel: timed out waiting for public URL: %s", collector.diagnostic())
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}

func (t *Tunnel) Done() <-chan error {
	if t == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return t.done
}

func (t *Tunnel) Close() error {
	if t == nil || t.cancel == nil {
		return nil
	}
	t.cancel()
	select {
	case err := <-t.done:
		if err != nil && !isContextExit(err) {
			return err
		}
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("cloudflared did not stop within 5s")
	}
}

func OriginFromAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if port == "" || port == "0" {
		return "", fmt.Errorf("temporary tunnel requires a fixed listen port, got %q", addr)
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func validateOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("temporary tunnel origin must be a local HTTP origin, got %q", origin)
	}
	return nil
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
}

type releaseMetadata struct {
	Assets []releaseAsset `json:"assets"`
}

func cloudflaredBinary(ctx context.Context) (string, error) {
	asset, err := cloudflaredAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache directory for cloudflared: %w", err)
	}
	if info, statErr := os.Stat("/kaggle/working"); statErr == nil && info.IsDir() {
		cacheDir = filepath.Join("/kaggle/working", ".kagmcp", "cache")
	}
	binDir := filepath.Join(cacheDir, "lilith-mcp", "bin")
	name := "lilith-cl"
	legacyName := "cloudflared"
	if runtime.GOOS == "windows" {
		name += ".exe"
		legacyName += ".exe"
	}
	path := filepath.Join(binDir, name)
	cacheVerified := verifyCachedCloudflared(path) == nil
	if !cacheVerified {
		legacyPath := filepath.Join(binDir, legacyName)
		if verifyCachedCloudflared(legacyPath) == nil {
			if err := migrateVerifiedCloudflared(legacyPath, path); err == nil {
				cacheVerified = true
			} else {
				slog.Warn("could not migrate managed cloudflared runtime to lilith-cl", "error", err)
			}
		}
	}
	if cacheVerified {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < managedBinaryMaxAge {
			return path, nil
		}
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create cloudflared cache directory: %w", err)
	}
	if err := downloadCloudflared(ctx, asset, path); err != nil {
		if cacheVerified {
			slog.Warn("cloudflared refresh failed; using verified cached binary", "error", err, "path", path)
			return path, nil
		}
		return "", err
	}
	return path, nil
}

func cloudflaredAsset(goos, goarch string) (string, error) {
	switch goos {
	case "linux":
		switch goarch {
		case "amd64", "386", "arm", "arm64":
			return "cloudflared-linux-" + goarch, nil
		}
	case "windows":
		switch goarch {
		case "amd64", "386":
			return "cloudflared-windows-" + goarch + ".exe", nil
		}
	}
	return "", fmt.Errorf("managed lilith-cl runtime is not available for %s/%s", goos, goarch)
}

func migrateVerifiedCloudflared(source, destination string) error {
	marker, err := os.ReadFile(source + ".sha256")
	if err != nil {
		return err
	}
	digest, err := parseSHA256Digest(strings.TrimSpace(string(marker)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".lilith-cl-migrate-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	written, copyErr := io.Copy(tmp, io.LimitReader(src, maxBinaryBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written <= 0 || written > maxBinaryBytes {
		return fmt.Errorf("migrated cloudflared has invalid size %d", written)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := validateExecutableHeader(tmpPath); err != nil {
		return err
	}
	return publishVerifiedBinary(tmpPath, destination, digest)
}

func downloadCloudflared(ctx context.Context, assetName, destination string) error {
	asset, digest, err := resolveReleaseAsset(ctx, assetName)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return fmt.Errorf("prepare cloudflared download: %w", err)
	}
	req.Header.Set("User-Agent", "lilith-mcp")
	resp, err := secureGitHubClient(90 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("download cloudflared: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download cloudflared: unexpected HTTP status %s", resp.Status)
	}
	if resp.ContentLength > maxBinaryBytes {
		return fmt.Errorf("download cloudflared: binary is unexpectedly large (%d bytes)", resp.ContentLength)
	}

	tmp, err := os.CreateTemp(filepath.Dir(destination), ".cloudflared-*")
	if err != nil {
		return fmt.Errorf("create cloudflared temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(resp.Body, maxBinaryBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("download cloudflared body: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close cloudflared temporary file: %w", closeErr)
	}
	if written == 0 || written > maxBinaryBytes {
		return fmt.Errorf("download cloudflared: invalid binary size %d", written)
	}
	if asset.Size > 0 && written != asset.Size {
		return fmt.Errorf("download cloudflared: size mismatch: got %d bytes, expected %d", written, asset.Size)
	}
	actualDigest := hex.EncodeToString(hasher.Sum(nil))
	if actualDigest != digest {
		return fmt.Errorf("download cloudflared: SHA-256 mismatch: got %s, expected %s", actualDigest, digest)
	}
	if err := validateExecutableHeader(tmpPath); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("make cloudflared executable: %w", err)
	}
	if err := publishVerifiedBinary(tmpPath, destination, digest); err != nil {
		return err
	}
	return nil
}

func resolveReleaseAsset(ctx context.Context, assetName string) (releaseAsset, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseEndpoint, nil)
	if err != nil {
		return releaseAsset{}, "", fmt.Errorf("prepare cloudflared release lookup: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "lilith-mcp")
	resp, err := secureGitHubClient(30 * time.Second).Do(req)
	if err != nil {
		return releaseAsset{}, "", fmt.Errorf("lookup cloudflared release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseAsset{}, "", fmt.Errorf("lookup cloudflared release: unexpected HTTP status %s", resp.Status)
	}
	var metadata releaseMetadata
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseMetadata))
	if err := decoder.Decode(&metadata); err != nil {
		return releaseAsset{}, "", fmt.Errorf("decode cloudflared release metadata: %w", err)
	}
	return selectReleaseAsset(metadata, assetName)
}

func selectReleaseAsset(metadata releaseMetadata, assetName string) (releaseAsset, string, error) {
	for _, asset := range metadata.Assets {
		if asset.Name != assetName {
			continue
		}
		if asset.Size <= 0 || asset.Size > maxBinaryBytes {
			return releaseAsset{}, "", fmt.Errorf("cloudflared release asset %q has invalid size %d", assetName, asset.Size)
		}
		u, err := url.Parse(asset.BrowserDownloadURL)
		if err != nil || u.Scheme != "https" || strings.ToLower(u.Hostname()) != "github.com" || !strings.HasPrefix(u.Path, "/cloudflare/cloudflared/releases/download/") {
			return releaseAsset{}, "", fmt.Errorf("cloudflared release asset %q has an unexpected download URL", assetName)
		}
		digest, err := parseSHA256Digest(asset.Digest)
		if err != nil {
			return releaseAsset{}, "", fmt.Errorf("cloudflared release asset %q: %w", assetName, err)
		}
		return asset, digest, nil
	}
	return releaseAsset{}, "", fmt.Errorf("cloudflared release does not contain asset %q", assetName)
}

func parseSHA256Digest(value string) (string, error) {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return "", errors.New("missing SHA-256 digest")
	}
	digest := strings.ToLower(strings.TrimPrefix(value, prefix))
	raw, err := hex.DecodeString(digest)
	if err != nil || len(raw) != sha256.Size {
		return "", errors.New("invalid SHA-256 digest")
	}
	return digest, nil
}

func secureGitHubClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" || !allowedGitHubHost(req.URL.Hostname()) {
				return fmt.Errorf("refusing cloudflared redirect to %q", req.URL.String())
			}
			return nil
		},
	}
}

func allowedGitHubHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "api.github.com" || host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func verifyCachedCloudflared(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBinaryBytes {
		return errors.New("cached cloudflared is not a valid regular file")
	}
	marker, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return err
	}
	digest, err := parseSHA256Digest(strings.TrimSpace(string(marker)))
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, io.LimitReader(f, maxBinaryBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hasher.Sum(nil)) != digest {
		return errors.New("cached cloudflared SHA-256 mismatch")
	}
	return validateExecutableHeader(path)
}

func publishVerifiedBinary(tmpPath, destination, digest string) error {
	backup := destination + ".previous"
	marker := destination + ".sha256"
	markerBackup := marker + ".previous"
	_ = os.Remove(backup)
	_ = os.Remove(markerBackup)

	hadPrevious := false
	if _, err := os.Stat(destination); err == nil {
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("backup cached cloudflared: %w", err)
		}
		hadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect cached cloudflared: %w", err)
	}
	hadMarker := false
	if _, err := os.Stat(marker); err == nil {
		if err := os.Rename(marker, markerBackup); err != nil {
			if hadPrevious {
				_ = os.Rename(backup, destination)
			}
			return fmt.Errorf("backup cloudflared digest marker: %w", err)
		}
		hadMarker = true
	} else if !errors.Is(err, os.ErrNotExist) {
		if hadPrevious {
			_ = os.Rename(backup, destination)
		}
		return fmt.Errorf("inspect cloudflared digest marker: %w", err)
	}

	restore := func() {
		_ = os.Remove(destination)
		_ = os.Remove(marker)
		if hadPrevious {
			_ = os.Rename(backup, destination)
		}
		if hadMarker {
			_ = os.Rename(markerBackup, marker)
		}
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		restore()
		return fmt.Errorf("install cached cloudflared: %w", err)
	}
	if err := writeDigestMarker(marker, digest); err != nil {
		restore()
		return err
	}
	_ = os.Remove(backup)
	_ = os.Remove(markerBackup)
	return nil
}

func writeDigestMarker(path, digest string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cloudflared-digest-*")
	if err != nil {
		return fmt.Errorf("create cloudflared digest marker: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.WriteString(tmp, "sha256:"+digest+"\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write cloudflared digest marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cloudflared digest marker: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod cloudflared digest marker: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace cloudflared digest marker: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install cloudflared digest marker: %w", err)
	}
	return nil
}

func validateExecutableHeader(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("validate cloudflared download: %w", err)
	}
	defer f.Close()
	header := make([]byte, 4)
	n, err := io.ReadFull(f, header)
	if err != nil || n != len(header) {
		return errors.New("validate cloudflared download: file is too short")
	}
	if runtime.GOOS == "windows" {
		if header[0] != 'M' || header[1] != 'Z' {
			return errors.New("validate cloudflared download: expected a Windows executable")
		}
		return nil
	}
	if string(header) != "\x7fELF" {
		return errors.New("validate cloudflared download: expected an ELF executable")
	}
	return nil
}

func isContextExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

type outputCollector struct {
	mu    sync.Mutex
	buf   strings.Builder
	urlCh chan string
	once  sync.Once
}

func newOutputCollector() *outputCollector {
	return &outputCollector{urlCh: make(chan string, 1)}
}

func (c *outputCollector) Write(p []byte) (int, error) {
	c.mu.Lock()
	if c.buf.Len() < 32<<10 {
		remaining := (32 << 10) - c.buf.Len()
		if len(p) > remaining {
			c.buf.Write(p[:remaining])
		} else {
			c.buf.Write(p)
		}
	}
	text := c.buf.String()
	c.mu.Unlock()

	if publicURL := quickURLPattern.FindString(text); publicURL != "" {
		c.once.Do(func() { c.urlCh <- publicURL })
	}
	return len(p), nil
}

func (c *outputCollector) diagnostic() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	text := strings.TrimSpace(c.buf.String())
	if text == "" {
		return "no cloudflared output"
	}
	if len(text) > 4096 {
		text = text[len(text)-4096:]
	}
	return text
}
