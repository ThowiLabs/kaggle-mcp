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

func TestFullAPIWithPATAndNoTokenLeak(t *testing.T) {
	const token = "github_pat_super_private_example"
	requests := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("PAT no está en Authorization")
		}
		if strings.Contains(r.RequestURI, token) {
			t.Error("PAT filtrado a URL")
		}
		if r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Error("API version no fijada")
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var body any
		if r.Method != "GET" {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("JSON body: %v", err)
			}
		}
		_, _ = io.WriteString(w, `{"success":true,"id":1}`)
	}))
	defer srv.Close()
	c := New(token)
	c.base = srv.URL
	c.http = srv.Client()
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/repos/owner/repo/issues?state=open", ""},
		{"POST", "/user/repos", `{"name":"newrepo"}`},
		{"PATCH", "/repos/owner/repo/issues/1", `{"state":"closed"}`},
		{"PUT", "/repos/owner/repo/pulls/1/merge", `{"merge_method":"squash"}`},
		{"DELETE", "/repos/owner/repo", ""},
	} {
		out, err := c.API(context.Background(), tc.method, tc.path, json.RawMessage(tc.body))
		if err != nil || out == nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
	}
	if len(requests) != 5 {
		t.Fatal(requests)
	}
}
func TestGitHubRESTRejectsUnsafeInputs(t *testing.T) {
	c := New("pat")
	for _, tc := range []struct{ method, path, body string }{
		{"TRACE", "/user", ""}, {"GET", "https://attacker.org/steal", ""},
		{"GET", "//evil.org/path", ""}, {"GET", "/repos/../admin", ""},
		{"GET", "/repos/o/r#fragment", ""}, {"GET", "/user?access_token=secret", ""},
		{"GET", "/user", `{"unexpected":true}`},
		{"POST", "/repos/x/y/issues", "not JSON"},
		{"POST", "/repos/x/y/issues", strings.Repeat("x", 1<<20+1)},
	} {
		if _, err := c.API(context.Background(), tc.method, tc.path, json.RawMessage(tc.body)); err == nil {
			t.Errorf("aceptó %s %s body=%d", tc.method, tc.path, len(tc.body))
		}
	}
	if _, err := New("").API(context.Background(), "GET", "/user", nil); err == nil {
		t.Fatal("API sin token permitió peticiones")
	}
}
func TestGitHubCommonOperations(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.Method == "DELETE" && strings.HasSuffix(r.URL.Path, "/contents/file.txt") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("DELETE contents JSON: %v", err)
			}
			if body["sha"] != "abc123" {
				t.Error("sha borrado inválido")
			}
		}
		_, _ = io.WriteString(w, `{"id":1}`)
	}))
	defer srv.Close()
	c := New("pat")
	c.base = srv.URL
	c.http = srv.Client()
	ctx := context.Background()
	if _, err := c.CreateRepository(ctx, "newrepo", "description", true, "user", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRepository(ctx, "newrepo", "", false, "org", "org1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBranch(ctx, "owner/repo", "feature/new", strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeleteFile(ctx, "owner/repo", "file.txt", "main", "Delete file", "abc123"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GraphQL(ctx, "query { viewer { login } }", json.RawMessage(`{"foo":"bar"}`)); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 5 {
		t.Fatalf("peticiones: %v", seen)
	}
	if seen[0] != "POST /user/repos" || seen[1] != "POST /orgs/org1/repos" || seen[2] != "POST /repos/owner/repo/git/refs" || seen[3] != "DELETE /repos/owner/repo/contents/file.txt" || seen[4] != "POST /graphql" {
		t.Fatalf("rutas: %v", seen)
	}
	if _, err := c.CreateBranch(ctx, "owner/repo", "main", "bad"); err == nil {
		t.Fatal("creó rama con SHA inválido")
	}
}
