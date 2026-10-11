package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ThowiLabs/kagssh-go/internal/githubtoken"
)

var githubExtendedNames = map[string]bool{
	"github_api": true, "github_graphql": true, "github_repo_create": true,
	"github_repo_delete": true, "github_branch_create": true, "github_branch_delete": true,
	"github_file_delete": true, "github_pr_create": true, "github_pr_merge": true,
	"github_issue_create": true, "github_issue_update": true, "github_release_create": true,
	"github_workflow_dispatch": true, "github_workflows": true,
	"github_tag_create": true, "github_tag_list": true, "github_tag_delete": true,
	"github_file_upload": true, "github_release_asset_upload": true,
	"github_workflow_upsert": true, "github_workflow_set_state": true,
	"github_workflow_runs": true, "github_workflow_run": true,
	"github_workflow_rerun": true, "github_workflow_cancel": true,
}

func githubExtendedTool(name string) bool { return githubExtendedNames[name] }
func (s *Server) invokeGitHubExtended(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var a struct {
		Method       string `json:"method"`
		Endpoint     string `json:"endpoint"`
		JSONBody     string `json:"json_body"`
		Query        string `json:"query"`
		Variables    string `json:"variables"`
		Repository   string `json:"repository"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		Private      *bool  `json:"private"`
		OwnerType    string `json:"owner_type"`
		Organization string `json:"organization"`
		Branch       string `json:"branch"`
		FromSHA      string `json:"from_sha"`
		Path         string `json:"path"`
		SHA          string `json:"sha"`
		Message      string `json:"message"`
		Title        string `json:"title"`
		Head         string `json:"head"`
		Base         string `json:"base"`
		Body         string `json:"body"`
		Number       int    `json:"number"`
		MergeMethod  string `json:"merge_method"`
		TagName      string `json:"tag_name"`
		Workflow     string `json:"workflow"`
		Ref          string `json:"ref"`
		Inputs       string `json:"inputs"`
		RunID        int64  `json:"run_id"`
		ReleaseID    int64  `json:"release_id"`
		LocalPath    string `json:"local_path"`
		FileName     string `json:"file_name"`
		TagMessage   string `json:"tag_message"`
		State        string `json:"state"`
		Confirmed    bool   `json:"confirmed"`
		WorkflowYAML string `json:"workflow_yaml"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	c := s.githubClient()
	if name == "github_api" {
		return c.API(ctx, a.Method, a.Endpoint, json.RawMessage(a.JSONBody))
	}
	if name == "github_graphql" {
		return c.GraphQL(ctx, a.Query, json.RawMessage(a.Variables))
	}
	if name == "github_repo_create" {
		if a.Private == nil {
			return nil, errors.New("private es obligatorio: indica true o false explícitamente")
		}
		return c.CreateRepository(ctx, a.Name, a.Description, *a.Private, a.OwnerType, a.Organization)
	}
	root := ""
	if a.Repository != "" {
		var err error
		root, err = githubtoken.RepoPath(a.Repository)
		if err != nil {
			return nil, err
		}
	}
	if root == "" {
		return nil, errors.New("repository es obligatorio")
	}
	post := func(method, path string, data any) (any, error) {
		body, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		return c.API(ctx, method, path, body)
	}
	if a.Number < 0 {
		return nil, errors.New("number inválido")
	}
	// All uploads come only from the explicitly permitted working volume.
	openUpload := func(maxBytes int64) (*os.File, int64, error) {
		resolved, err := s.path(a.LocalPath, false)
		if err != nil {
			return nil, 0, err
		}
		// A local file upload must not silently publish a mounted dataset.
		if resolved != "/kaggle/working" && !strings.HasPrefix(resolved, "/kaggle/working/") {
			return nil, 0, errors.New("subidas solo desde /kaggle/working")
		}
		stat, err := os.Lstat(resolved)
		if err != nil {
			return nil, 0, err
		}
		if !stat.Mode().IsRegular() || stat.Size() > maxBytes {
			return nil, 0, errors.New("archivo no regular o supera el tamaño permitido")
		}
		file, err := os.Open(resolved)
		if err != nil {
			return nil, 0, err
		}
		return file, stat.Size(), nil
	}
	switch name {
	case "github_tag_create":
		if !a.Confirmed {
			return nil, errors.New("confirma creación del tag: puede disparar GitHub Actions (confirmed=true)")
		}
		return c.CreateTag(ctx, a.Repository, a.TagName, a.SHA, a.TagMessage)
	case "github_tag_list":
		return c.Tags(ctx, a.Repository)
	case "github_tag_delete":
		if !a.Confirmed {
			return nil, errors.New("confirma explícitamente la eliminación del tag (confirmed=true)")
		}
		return c.DeleteTag(ctx, a.Repository, a.TagName)
	case "github_file_upload":
		if !a.Confirmed {
			return nil, errors.New("confirma la publicación del archivo local (confirmed=true)")
		}
		file, size, err := openUpload(1 << 20)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		data := make([]byte, size)
		if _, err := io.ReadFull(file, data); err != nil {
			return nil, err
		}
		return c.WriteBytes(ctx, a.Repository, a.Path, a.Branch, a.Message, a.SHA, data)
	case "github_release_asset_upload":
		if !a.Confirmed {
			return nil, errors.New("confirma la publicación del archivo local en la release (confirmed=true)")
		}
		file, size, err := openUpload(50 << 20)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		name := a.FileName
		if name == "" {
			name = filepath.Base(a.LocalPath)
		}
		return c.UploadReleaseAsset(ctx, a.Repository, a.ReleaseID, name, size, file)
	case "github_workflow_upsert":
		if !a.Confirmed {
			return nil, errors.New("confirma edición de workflows, que pueden ejecutar código (confirmed=true)")
		}
		if strings.ContainsAny(a.Workflow, "/\\") || (!strings.HasSuffix(a.Workflow, ".yml") && !strings.HasSuffix(a.Workflow, ".yaml")) {
			return nil, errors.New("workflow debe ser nombre .yml o .yaml sin directorios")
		}
		if a.WorkflowYAML == "" {
			return nil, errors.New("workflow_yaml obligatorio")
		}
		return c.WriteFile(ctx, a.Repository, ".github/workflows/"+a.Workflow, a.Branch, a.Message, a.WorkflowYAML, a.SHA)
	case "github_workflow_set_state":
		if !a.Confirmed {
			return nil, errors.New("confirma cambio de estado del workflow (confirmed=true)")
		}
		return c.WorkflowSetState(ctx, a.Repository, a.Workflow, a.State)
	case "github_workflow_runs":
		return c.WorkflowRuns(ctx, a.Repository, a.Workflow)
	case "github_workflow_run":
		return c.WorkflowRun(ctx, a.Repository, a.RunID)
	case "github_workflow_rerun":
		if !a.Confirmed {
			return nil, errors.New("confirma rerun, que puede consumir minutos (confirmed=true)")
		}
		return c.WorkflowRunAction(ctx, a.Repository, a.RunID, "rerun")
	case "github_workflow_cancel":
		if !a.Confirmed {
			return nil, errors.New("confirma cancelación de ejecución (confirmed=true)")
		}
		return c.WorkflowRunAction(ctx, a.Repository, a.RunID, "cancel")
	case "github_repo_delete":
		return c.API(ctx, "DELETE", root, nil)
	case "github_branch_create":
		return c.CreateBranch(ctx, a.Repository, a.Branch, a.FromSHA)
	case "github_branch_delete":
		if a.Branch == "" || a.Branch == "main" || a.Branch == "master" {
			return nil, errors.New("rama vacía o por defecto; utiliza github_api conscientemente si debes borrarla")
		}
		return c.API(ctx, "DELETE", root+"/git/refs/heads/"+url.PathEscape(a.Branch), nil)
	case "github_file_delete":
		return c.DeleteFile(ctx, a.Repository, a.Path, a.Branch, a.Message, a.SHA)
	case "github_pr_create":
		if a.Title == "" || a.Head == "" || a.Base == "" {
			return nil, errors.New("faltan title, head o base")
		}
		return post("POST", root+"/pulls", map[string]any{"title": a.Title, "head": a.Head, "base": a.Base, "body": a.Body})
	case "github_pr_merge":
		if a.Number <= 0 {
			return nil, errors.New("número PR inválido")
		}
		method := a.MergeMethod
		if method == "" {
			method = "merge"
		}
		if method != "merge" && method != "squash" && method != "rebase" {
			return nil, errors.New("merge_method inválido")
		}
		return post("PUT", root+"/pulls/"+strconv.Itoa(a.Number)+"/merge", map[string]any{"merge_method": method, "commit_message": a.Message})
	case "github_issue_create":
		if a.Title == "" {
			return nil, errors.New("title obligatorio")
		}
		return post("POST", root+"/issues", map[string]any{"title": a.Title, "body": a.Body})
	case "github_issue_update":
		if a.Number <= 0 || a.JSONBody == "" {
			return nil, errors.New("number y json_body obligatorios")
		}
		return c.API(ctx, "PATCH", root+"/issues/"+strconv.Itoa(a.Number), json.RawMessage(a.JSONBody))
	case "github_release_create":
		if a.TagName == "" {
			return nil, errors.New("tag_name obligatorio")
		}
		return post("POST", root+"/releases", map[string]any{"tag_name": a.TagName, "name": a.Name, "body": a.Body})
	case "github_workflow_dispatch":
		if !a.Confirmed {
			return nil, errors.New("confirma ejecución de Actions (confirmed=true)")
		}
		if a.Workflow == "" || a.Ref == "" {
			return nil, errors.New("workflow y ref obligatorios")
		}
		var inputs any = map[string]any{}
		if a.Inputs != "" {
			if err := json.Unmarshal([]byte(a.Inputs), &inputs); err != nil {
				return nil, err
			}
			if _, ok := inputs.(map[string]any); !ok {
				return nil, errors.New("inputs deben ser objeto JSON")
			}
		}
		return post("POST", root+"/actions/workflows/"+url.PathEscape(a.Workflow)+"/dispatches", map[string]any{"ref": a.Ref, "inputs": inputs})
	case "github_workflows":
		return c.API(ctx, "GET", root+"/actions/workflows?per_page=100", nil)
	}
	return nil, errors.New("herramienta GitHub desconocida")
}
