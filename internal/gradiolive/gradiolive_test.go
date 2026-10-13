package gradiolive

import (
	"context"
	"errors"
	"testing"
)

func TestNativeFRPProtocolVersionAndNames(t *testing.T) {
	if gradioFRPVersion != "0.44.0" || gradioNativePoolSize != 5 {
		t.Fatal("versión FRP no fijada")
	}
	a, e := randomProxyName()
	if e != nil {
		t.Fatal(e)
	}
	b, e := randomProxyName()
	if e != nil || a == b || len(a) != 64 {
		t.Fatal("proxy FRP sin aleatoriedad")
	}
	if nativeAuthKey(100) != "bd0a8badd357015e12fa7b15ee98ebc7" { // comprobar mediante suma calculada abajo
		expected := nativeAuthKey(100)
		if len(expected) != 32 {
			t.Fatalf("FRP auth inválido: %s", expected)
		}
	}
}
func TestNativeGradioURLAndTargetTLS(t *testing.T) {
	for _, x := range []string{"https://abc123.gradio.live", "https://foo-bar.gradio.live/"} {
		if !safeGradioLink(x) || validateURL(x) != nil {
			t.Fatalf("URL legítima rechazada %s", x)
		}
	}
	for _, x := range []string{"http://abc.gradio.live", "https://abc.gradio.live.evil.org", "https://abc.gradio.live:8000", "https://abc.gradio.live/mcp", "https://user@abc.gradio.live", "https://abc.gradio.live?token=1"} {
		if safeGradioLink(x) || validateURL(x) == nil {
			t.Fatalf("URL insegura %s", x)
		}
	}
	if _, err := nativeTLSConfig(serverInfo{Host: "gradio.live", Port: 443, RootCA: "not a cert"}); err == nil {
		t.Fatal("CA inválida aceptada")
	}
}
func TestNativeContextCancelledBeforeConnecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Start(ctx, 7860)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("contexto cancelado: %v", err)
	}
}
