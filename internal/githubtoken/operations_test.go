package githubtoken

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTagsLightweightAnnotatedAndNoOverwrite(t *testing.T) {
	var requests []string
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const tagObject = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer pat-test" {
			t.Error("falta token")
		}
		if strings.HasSuffix(r.URL.Path, "/git/tags") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["object"] != sha || body["type"] != "commit" || body["message"] != "Publicación estable" {
				t.Error("tag anotado inválido")
			}
			fmt.Fprintf(w, `{"sha":"%s"}`, tagObject)
			return
		}
		if r.Method == "POST" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["ref"] == "refs/tags/v1.2.3" && body["sha"] != sha {
				t.Error("SHA ligero inválido")
			}
			if body["ref"] == "refs/tags/v1.3.0" && body["sha"] != tagObject {
				t.Error("SHA anotado inválido")
			}
		}
		_, _ = w.Write([]byte(`{"ref":"refs/tags/v1.2.3"}`))
	}))
	defer srv.Close()
	c := New("pat-test")
	c.base = srv.URL
	c.http = srv.Client()
	ctx := context.Background()
	if _, err := c.CreateTag(ctx, "o/r", "v1.2.3", sha, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateTag(ctx, "o/r", "v1.3.0", sha, "Publicación estable"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tags(ctx, "o/r"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeleteTag(ctx, "o/r", "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 5 {
		t.Fatalf("solicitudes tag %v", requests)
	}
	if requests[0] != "POST /repos/o/r/git/refs" || requests[1] != "POST /repos/o/r/git/tags" || requests[2] != "POST /repos/o/r/git/refs" || requests[3] != "GET /repos/o/r/tags" || requests[4] != "DELETE /repos/o/r/git/refs/tags/v1.2.3" {
		t.Fatalf("endpoints tags %v", requests)
	}
	for _, bad := range []string{"../evil", "v..1", "a.lock", "x/../y", "x?foo", "evil\nHeader", "bad//tag", "a~b", "x@y"} {
		if _, err := c.CreateTag(ctx, "o/r", bad, sha, ""); err == nil {
			t.Errorf("tag inválido permitido %q", bad)
		}
	}
	if _, err := c.CreateTag(ctx, "o/r", "v4", "xyz", ""); err == nil {
		t.Fatal("SHA inválido")
	}
}

func TestWorkflowRoutesAndMutations(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == "POST" || r.Method == "PUT" {
			w.WriteHeader(204)
			return
		}
		_, _ = w.Write([]byte(`{"total_count":0}`))
	}))
	defer srv.Close()
	c := New("pat-test")
	c.base = srv.URL
	c.http = srv.Client()
	ctx := context.Background()
	if _, err := c.WorkflowRuns(ctx, "o/r", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WorkflowRuns(ctx, "o/r", "build.yml"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WorkflowRun(ctx, "o/r", 42); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WorkflowSetState(ctx, "o/r", "build.yml", "enable"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WorkflowSetState(ctx, "o/r", "build.yml", "disable"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WorkflowRunAction(ctx, "o/r", 42, "rerun"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WorkflowRunAction(ctx, "o/r", 42, "cancel"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /repos/o/r/actions/runs", "GET /repos/o/r/actions/workflows/build.yml/runs",
		"GET /repos/o/r/actions/runs/42", "PUT /repos/o/r/actions/workflows/build.yml/enable",
		"PUT /repos/o/r/actions/workflows/build.yml/disable", "POST /repos/o/r/actions/runs/42/rerun",
		"POST /repos/o/r/actions/runs/42/cancel",
	}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("rutas workflow %v", calls)
	}
	if _, err := c.WorkflowSetState(ctx, "o/r", "../evil", "enable"); err == nil {
		t.Fatal("path traversal workflow")
	}
	if _, err := c.WorkflowRunAction(ctx, "o/r", -1, "cancel"); err == nil {
		t.Fatal("run_id negativo")
	}
	if _, err := c.WorkflowSetState(ctx, "o/r", "build.yml", "delete"); err == nil {
		t.Fatal("state inválido")
	}
}
func TestRepositoryBinaryFileUpload(t *testing.T) {
	binary := []byte{0, 1, 2, 255, 10}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/repos/o/r/contents/dist/test.bin" {
			t.Errorf("ruta no esperada %s", r.URL.Path)
		}
		var body struct{ Content, Branch, Message, SHA string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		decoded, err := base64.StdEncoding.DecodeString(body.Content)
		if err != nil || !bytes.Equal(decoded, binary) {
			t.Error("bytes alterados")
		}
		if body.Branch != "feature" || body.SHA != "old-sha" {
			t.Error("metadata incorrecta")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"content":{"sha":"new-sha"},"commit":{"sha":"commit"}}`))
	}))
	defer srv.Close()
	c := New("pat-test")
	c.base = srv.URL
	c.http = srv.Client()
	if _, err := c.WriteBytes(context.Background(), "o/r", "dist/test.bin", "feature", "Subir", "old-sha", binary); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteBytes(context.Background(), "o/r", "../token.env", "main", "Subir", "", binary); err == nil {
		t.Fatal("ruta inválida")
	}
	if _, err := c.WriteBytes(context.Background(), "o/r", "file", "main", "Subir", "", make([]byte, 1<<20+1)); err == nil {
		t.Fatal("más de 1 MiB permitido")
	}
}
func TestReleaseAssetBinaryUploadAndHostRestriction(t *testing.T) {
	payload := []byte{80, 75, 3, 4, 0, 255}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases/17/assets" || r.URL.Query().Get("name") != "release.zip" {
			t.Errorf("ruta no válida: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer pat-test" {
			t.Error("sin PAT")
		}
		if r.Header.Get("Content-Type") == "" || r.ContentLength != int64(len(payload)) {
			t.Error("media type o tamaño incorrectos")
		}
		content, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(content, payload) {
			t.Error("contenido modificado")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"id":99,"name":"release.zip","size":6,"state":"uploaded","browser_download_url":"https://github.com/o/r/releases/download/v1/release.zip"}`))
	}))
	defer server.Close()
	c := New("pat-test")
	c.uploadBase = server.URL
	result, err := c.UploadReleaseAsset(context.Background(), "o/r", 17, "release.zip", int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("no devolvió asset")
	}
	for _, name := range []string{"../x", "/secret.txt", "x\\evil", ""} {
		if _, err := c.UploadReleaseAsset(context.Background(), "o/r", 17, name, 1, bytes.NewReader(payload)); err == nil {
			t.Errorf("nombre peligroso aceptado %q", name)
		}
	}
	if _, err := c.UploadReleaseAsset(context.Background(), "o/r", 17, "z.zip", 50<<20+1, bytes.NewReader(payload)); err == nil {
		t.Fatal("50 MiB superados")
	}
	if _, err := New("").UploadReleaseAsset(context.Background(), "o/r", 17, "z.zip", 1, bytes.NewReader(payload)); err == nil {
		t.Fatal("sin token se intentó subir")
	}
}
