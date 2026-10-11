package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateHF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hf_test_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"tester"}`))
	}))
	defer server.Close()
	client := server.Client()
	if err := validateHF(context.Background(), client, server.URL, "hf_test_token"); err != nil {
		t.Fatal(err)
	}
	if err := validateHF(context.Background(), client, server.URL, "bad"); err == nil {
		t.Fatal("token erróneo aceptado")
	}
	for _, token := range []string{"hf_token\nmal", "", strings.Repeat("x", 4097)} {
		if err := validateHF(context.Background(), client, server.URL, token); err == nil {
			t.Fatalf("aceptó valor inválido")
		}
	}
}
