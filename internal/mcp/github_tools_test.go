package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ThowiLabs/kagssh-go/internal/githubtoken"
)

func TestGitHubFullMCPDefinitionsRegistered(t *testing.T) {
	s := newTestServer(t)
	raw, err := json.Marshal(s.definitions())
	if err != nil {
		t.Fatal(err)
	}
	var definitions []struct {
		Name string `json:"name"`
	}
	if err = json.Unmarshal(raw, &definitions); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, d := range definitions {
		found[d.Name] = true
	}
	for name := range githubExtendedNames {
		if !found[name] {
			t.Errorf("herramienta Github no expuesta: %s", name)
		}
	}
	if len(githubExtendedNames) < 12 {
		t.Fatal("faltan funciones GitHub")
	}
}
func TestGitHubMCPValidationWithoutExternalCalls(t *testing.T) {
	s := newTestServer(t)
	s.github = githubtoken.New("test_pat_not_real")
	ctx := context.Background()
	tests := []struct{ name, payload string }{
		{"github_api", `{"method":"TRACE","endpoint":"/user"}`},
		{"github_api", `{"method":"GET","endpoint":"https://malicious.example/steal"}`},
		{"github_graphql", `{"query":""}`},
		{"github_repo_create", `{"name":"","owner_type":"user"}`},
		{"github_branch_create", `{"repository":"owner/repo","branch":"feature","from_sha":"short"}`},
		{"github_pr_merge", `{"repository":"owner/repo","number":0}`},
		{"github_issue_create", `{"repository":"owner/repo","title":""}`},
		{"github_workflow_dispatch", `{"repository":"owner/repo","workflow":"","ref":""}`},
		{"github_tag_create", `{"repository":"owner/repo","tag_name":"v1","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`},
		{"github_tag_delete", `{"repository":"owner/repo","tag_name":"v1"}`},
		{"github_workflow_set_state", `{"repository":"owner/repo","workflow":"ci.yml","state":"disable"}`},
		{"github_workflow_rerun", `{"repository":"owner/repo","run_id":10}`},
		{"github_workflow_cancel", `{"repository":"owner/repo","run_id":10}`},
		{"github_workflow_upsert", `{"repository":"owner/repo","workflow":"ci.yml","workflow_yaml":"name: Test","branch":"main","message":"ci"}`},
		{"github_file_upload", `{"repository":"owner/repo","local_path":"/kaggle/working/binary","path":"binary","branch":"main","message":"add"}`},
		{"github_release_asset_upload", `{"repository":"owner/repo","release_id":1,"local_path":"/kaggle/working/binary"}`},
		{"github_workflow_dispatch", `{"repository":"owner/repo","workflow":"ci.yml","ref":"main"}`},
		{"github_branch_delete", `{"repository":"owner/repo","branch":"main"}`},
		{"github_issue_update", `{"repository":"owner/repo","number":1,"json_body":"not-json"}`},
	}
	for _, tc := range tests {
		out, err := s.invoke(ctx, tc.name, json.RawMessage(tc.payload))
		if err == nil {
			t.Errorf("%s aceptó entrada inválida: %#v", tc.name, out)
		}
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "test_pat_not_real") {
			t.Error("PAT expuesto en error")
		}
	}
}
