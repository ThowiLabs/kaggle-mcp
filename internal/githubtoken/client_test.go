package githubtoken

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersonalTokenAuthorizationWithoutApp(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer personal-test-token" {
			t.Errorf("Bearer token incorrecto")
		}
		if strings.Contains(r.URL.String(), "token") {
			t.Error("se incluyó token en URL")
		}
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			io.WriteString(w, `{"login":"test","id":4}`)
		case "/user/repos":
			io.WriteString(w, `[{"full_name":"test/repo","private":true,"default_branch":"main"}]`)
		case "/repos/test/repo/branches":
			io.WriteString(w, `[{"name":"main"}]`)
		case "/repos/test/repo/contents/hello.txt":
			if r.Method == "PUT" {
				var payload map[string]string
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if _, exists := payload["sha"]; exists {
					t.Error("para creación no se debe enviar sha vacío")
				}
				if payload["content"] != "aG9sYQ==" {
					t.Error("contenido base64 incorrecto")
				}
				w.WriteHeader(http.StatusCreated)
				io.WriteString(w, `{"content":{"sha":"new"},"commit":{"sha":"commit"}}`)
				return
			}
			io.WriteString(w, `{"type":"file","path":"hello.txt","sha":"old","encoding":"base64","size":4,"content":"aG9sYQ=="}`)
		default:
			t.Errorf("ruta inesperada: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New("personal-test-token")
	c.base = srv.URL
	c.http = srv.Client()
	ctx := context.Background()
	if _, err := c.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Repos(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Branches(ctx, "test/repo"); err != nil {
		t.Fatal(err)
	}
	val, err := c.ReadFile(ctx, "test/repo", "hello.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if val.(map[string]any)["content"] != "hola" {
		t.Fatalf("lectura incorrecta: %v", val)
	}
	if _, err := c.WriteFile(ctx, "test/repo", "hello.txt", "main", "Crear archivo", "hola", ""); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 5 {
		t.Fatalf("se esperaban cinco llamadas, %v", paths)
	}
}
func TestNoTokenFailsClosed(t *testing.T) {
	_, err := New("").Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("debe exigir token personal: %v", err)
	}
}
func TestRejectsUnsafePaths(t *testing.T) {
	for _, value := range []string{"a/../../b", "/a//x", "a\\b", "a/./b"} {
		if _, err := encodePath(value); err == nil {
			t.Errorf("ruta aceptada: %q", value)
		}
	}
}
