package quicktunnel

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOriginFromAddr(t *testing.T) {
	tests := map[string]string{
		":8080":          "http://127.0.0.1:8080",
		"0.0.0.0:9090":   "http://127.0.0.1:9090",
		"127.0.0.1:8181": "http://127.0.0.1:8181",
		"localhost:7070": "http://localhost:7070",
		"[::]:6060":      "http://127.0.0.1:6060",
		"[::1]:5050":     "http://[::1]:5050",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := OriginFromAddr(input)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("OriginFromAddr(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestOriginFromAddrRejectsDynamicPort(t *testing.T) {
	if _, err := OriginFromAddr("127.0.0.1:0"); err == nil {
		t.Fatal("dynamic port accepted")
	}
}

func TestOutputCollectorFindsQuickTunnelURLAcrossWrites(t *testing.T) {
	collector := newOutputCollector()
	_, _ = collector.Write([]byte("Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):\nhttps://bright-"))
	_, _ = collector.Write([]byte("otter-example.trycloudflare.com\n"))
	select {
	case got := <-collector.urlCh:
		if got != "https://bright-otter-example.trycloudflare.com" {
			t.Fatalf("URL = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("collector did not publish URL")
	}
}

func TestCloudflaredAsset(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "cloudflared-linux-amd64"},
		{"linux", "arm64", "cloudflared-linux-arm64"},
		{"windows", "amd64", "cloudflared-windows-amd64.exe"},
	} {
		got, err := cloudflaredAsset(tc.goos, tc.goarch)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("cloudflaredAsset(%q, %q) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
	if _, err := cloudflaredAsset("plan9", "amd64"); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("unsupported platform error = %v", err)
	}
}

func TestSelectReleaseAssetRequiresOfficialURLAndDigest(t *testing.T) {
	digest := strings.Repeat("ab", sha256.Size)
	metadata := releaseMetadata{Assets: []releaseAsset{{
		Name:               "cloudflared-linux-amd64",
		BrowserDownloadURL: "https://github.com/cloudflare/cloudflared/releases/download/2026.5.2/cloudflared-linux-amd64",
		Digest:             "sha256:" + digest,
		Size:               42 << 20,
	}}}
	asset, gotDigest, err := selectReleaseAsset(metadata, "cloudflared-linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "cloudflared-linux-amd64" || gotDigest != digest {
		t.Fatalf("asset=%+v digest=%q", asset, gotDigest)
	}

	metadata.Assets[0].BrowserDownloadURL = "https://example.com/cloudflared-linux-amd64"
	if _, _, err := selectReleaseAsset(metadata, "cloudflared-linux-amd64"); err == nil {
		t.Fatal("non-official download URL accepted")
	}
	metadata.Assets[0].BrowserDownloadURL = "https://github.com/cloudflare/cloudflared/releases/download/2026.5.2/cloudflared-linux-amd64"
	metadata.Assets[0].Digest = "sha256:not-a-digest"
	if _, _, err := selectReleaseAsset(metadata, "cloudflared-linux-amd64"); err == nil {
		t.Fatal("invalid digest accepted")
	}
}

func TestParseSHA256Digest(t *testing.T) {
	want := strings.Repeat("01", sha256.Size)
	got, err := parseSHA256Digest("sha256:" + strings.ToUpper(want))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("digest = %q, want %q", got, want)
	}
	for _, value := range []string{"", "md5:" + want, "sha256:abcd", "sha256:" + strings.Repeat("zz", sha256.Size)} {
		if _, err := parseSHA256Digest(value); err == nil {
			t.Fatalf("invalid digest %q accepted", value)
		}
	}
}

func TestVerifyCachedCloudflaredDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cloudflared")
	body := []byte("\x7fELFverified-cache")
	if runtime.GOOS == "windows" {
		body = []byte("MZ\x00\x00verified-cache")
	}
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if err := os.WriteFile(path+".sha256", []byte("sha256:"+hex.EncodeToString(sum[:])+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCachedCloudflared(path); err != nil {
		t.Fatalf("verified cache rejected: %v", err)
	}
	body[len(body)-1] ^= 0xff
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyCachedCloudflared(path); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("tampered cache error = %v", err)
	}
}

func TestMigrateVerifiedCloudflaredInstallsLilithCLCache(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "cloudflared")
	destination := filepath.Join(dir, "lilith-cl")
	body := []byte("\x7fELFmanaged-cloudflared")
	if runtime.GOOS == "windows" {
		body = []byte("MZ\x00\x00managed-cloudflared")
	}
	if err := os.WriteFile(legacy, body, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(legacy+".sha256", []byte("sha256:"+digest+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateVerifiedCloudflared(legacy, destination); err != nil {
		t.Fatal(err)
	}
	if err := verifyCachedCloudflared(destination); err != nil {
		t.Fatalf("migrated lilith-cl cache rejected: %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("migrated body = %q, want %q", got, body)
	}
	marker, err := os.ReadFile(destination + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(marker)) != "sha256:"+digest {
		t.Fatalf("migrated marker = %q", marker)
	}
}

func TestAllowedGitHubHost(t *testing.T) {
	for _, host := range []string{"api.github.com", "github.com", "release-assets.githubusercontent.com"} {
		if !allowedGitHubHost(host) {
			t.Fatalf("expected host %q to be allowed", host)
		}
	}
	for _, host := range []string{"example.com", "github.com.evil.example", "githubusercontent.com.evil.example"} {
		if allowedGitHubHost(host) {
			t.Fatalf("unexpected host %q allowed", host)
		}
	}
}
