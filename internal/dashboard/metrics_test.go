package dashboard

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPanelAxolotlYMetricasAutorizadas(t *testing.T) {
	h := testHandler(t)
	for _, asset := range []string{"/assets/axolotl.css", "/assets/dashboard.css", "/assets/dashboard.js", "/assets/axolotl.svg"} {
		got := call(h, "GET", asset, nil)
		if got.Code != http.StatusOK || got.Body.Len() < 50 {
			t.Fatalf("%s: código %d, tamaño %d", asset, got.Code, got.Body.Len())
		}
	}
	if got := call(h, "GET", "/assets/../state.json", nil); got.Code == http.StatusOK {
		t.Fatal("recurso privado accesible")
	}
	if got := call(h, "GET", "/api/metrics", nil); got.Code != http.StatusSeeOther {
		t.Fatalf("métricas sin sesión: %d", got.Code)
	}
	login := call(h, "GET", "/login", nil)
	if !strings.Contains(login.Body.String(), "Bienvenido a KagMCP") {
		t.Fatal("no se presentó login")
	}
	cookie := login.Result().Cookies()[0]
	signed := call(h, "POST", "/login", url.Values{"pin": {"TEST_LONG_ACCESS_PIN"}, "csrf": {cookie.Value}}, cookie)
	var session *http.Cookie
	for _, c := range signed.Result().Cookies() {
		if c.Name == "__Host-kagmcp-session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("sesión no establecida")
	}
	home := call(h, "GET", "/", nil, session)
	if home.Code != 200 || !strings.Contains(home.Body.String(), "HF_TOKEN") || !strings.Contains(home.Body.String(), "gpuRing") {
		t.Fatalf("panel Axolotl incompleto: HTTP %d", home.Code)
	}
	response := call(h, "GET", "/api/metrics", nil, session)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API métricas: %d", response.Code)
	}
	var payload struct {
		Storage struct {
			Available bool `json:"available"`
		} `json:"storage"`
		GPU struct {
			Available bool `json:"available"`
		} `json:"gpu"`
		Time string `json:"time"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Time == "" {
		t.Fatalf("payload métricas inválido: %v", err)
	}
	denied := call(h, "POST", "/huggingface", url.Values{"csrf": {"incorrecto"}, "token": {"hf_bad"}}, session)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("CSRF Hugging Face: %d", denied.Code)
	}
}
