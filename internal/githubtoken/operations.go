package githubtoken

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GitHub refs reject punctuation that would make a branch/tag ambiguous.
var tagPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,199}$`)

func validTag(tag string) bool {
	if !tagPattern.MatchString(tag) || strings.Contains(tag, "..") || strings.Contains(tag, "@{") || strings.HasSuffix(tag, ".") || strings.HasSuffix(tag, "/") || strings.Contains(tag, "//") {
		return false
	}
	for _, v := range strings.Split(tag, "/") {
		if v == "" || v == "." || v == ".." || strings.HasSuffix(v, ".lock") {
			return false
		}
	}
	return true
}

// CreateTag creates an immutable reference. Annotated tags require a tag object
// followed by a refs/tags creation. A conflict never force-updates a prior tag.
func (c *Client) CreateTag(ctx context.Context, repo, tag, sha, message string) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	if !validTag(tag) || !gitSHA.MatchString(sha) {
		return nil, errors.New("tag inválido o sha no es SHA Git completo")
	}
	refSHA := sha
	if message != "" {
		value, err := c.API(ctx, "POST", root+"/git/tags", mustJSON(map[string]any{
			"tag": tag, "message": message, "object": sha, "type": "commit",
		}))
		if err != nil {
			return nil, err
		}
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("tag GitHub devolvió objeto inválido")
		}
		created, _ := obj["sha"].(string)
		if !gitSHA.MatchString(created) {
			return nil, errors.New("tag GitHub no devolvió SHA válida")
		}
		refSHA = created
	}
	result, err := c.API(ctx, "POST", root+"/git/refs", mustJSON(map[string]any{"ref": "refs/tags/" + tag, "sha": refSHA}))
	if err != nil {
		return nil, err
	}
	return map[string]any{"tag": tag, "repository": repo, "annotated": message != "", "object_sha": refSHA, "reference": result}, nil
}
func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func (c *Client) Tags(ctx context.Context, repo string) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	return c.API(ctx, "GET", root+"/tags?per_page=100", nil)
}
func (c *Client) DeleteTag(ctx context.Context, repo, tag string) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	if !validTag(tag) {
		return nil, errors.New("tag inválido")
	}
	// Encode each segment; never allow escaping out of refs/tags.
	pieces := strings.Split(tag, "/")
	for i, v := range pieces {
		pieces[i] = url.PathEscape(v)
	}
	return c.API(ctx, "DELETE", root+"/git/refs/tags/"+strings.Join(pieces, "/"), nil)
}
func workflowSegment(name string) (string, error) {
	if len(name) == 0 || len(name) > 120 || strings.ContainsAny(name, "/\\?#\r\n\x00") || name == "." || name == ".." {
		return "", errors.New("workflow debe ser nombre de archivo o ID")
	}
	return url.PathEscape(name), nil
}
func (c *Client) WorkflowSetState(ctx context.Context, repo, workflow, state string) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	name, err := workflowSegment(workflow)
	if err != nil {
		return nil, err
	}
	if state != "enable" && state != "disable" {
		return nil, errors.New("state debe ser enable o disable")
	}
	return c.API(ctx, "PUT", root+"/actions/workflows/"+name+"/"+state, nil)
}
func (c *Client) WorkflowRuns(ctx context.Context, repo, workflow string) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	if workflow == "" {
		return c.API(ctx, "GET", root+"/actions/runs?per_page=30", nil)
	}
	name, err := workflowSegment(workflow)
	if err != nil {
		return nil, err
	}
	return c.API(ctx, "GET", root+"/actions/workflows/"+name+"/runs?per_page=30", nil)
}
func (c *Client) WorkflowRun(ctx context.Context, repo string, id int64) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, errors.New("run_id inválido")
	}
	return c.API(ctx, "GET", root+"/actions/runs/"+strconv.FormatInt(id, 10), nil)
}
func (c *Client) WorkflowRunAction(ctx context.Context, repo string, id int64, action string) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, errors.New("run_id inválido")
	}
	if action != "rerun" && action != "cancel" {
		return nil, errors.New("acción workflow inválida")
	}
	return c.API(ctx, "POST", root+"/actions/runs/"+strconv.FormatInt(id, 10)+"/"+action, nil)
}

// WriteBytes supports small binary repository files, without converting them to
// text. Files that exceed the safe MCP upload bound should go through git push.
func (c *Client) WriteBytes(ctx context.Context, repo, path, branch, message, sha string, data []byte) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	encoded, err := encodePath(path)
	if err != nil {
		return nil, err
	}
	if branch == "" || message == "" {
		return nil, errors.New("branch y message obligatorios")
	}
	if len(data) > 1<<20 {
		return nil, errors.New("archivo excede 1 MiB; utiliza git push o Git LFS")
	}
	payload := map[string]string{"branch": branch, "message": message, "content": base64.StdEncoding.EncodeToString(data)}
	if sha != "" {
		payload["sha"] = sha
	}
	encodedBody := mustJSON(payload)
	var obj json.RawMessage
	if err := c.Request(ctx, "PUT", root+"/contents/"+encoded, bytes.NewReader(encodedBody), &obj); err != nil {
		return nil, err
	}
	return map[string]any{"repository": repo, "path": path, "branch": branch, "bytes": len(data), "result": obj}, nil
}

const maxReleaseAsset = 50 << 20

// UploadReleaseAsset streams raw bytes to GitHub's fixed uploads.github.com
// endpoint. No URL is accepted from the caller, preventing bearer exfiltration.
func (c *Client) UploadReleaseAsset(ctx context.Context, repo string, releaseID int64, name string, size int64, reader io.Reader) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	if c.token == "" {
		return nil, errors.New("GITHUB_TOKEN no configurado")
	}
	if releaseID <= 0 {
		return nil, errors.New("release_id inválido")
	}
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) || len(name) > 180 || strings.ContainsAny(name, "/\\\r\n\x00") {
		return nil, errors.New("nombre de archivo inválido")
	}
	if size < 0 || size > maxReleaseAsset {
		return nil, fmt.Errorf("archivo supera máximo seguro de %d MiB", maxReleaseAsset>>20)
	}
	if reader == nil {
		return nil, errors.New("archivo sin contenido")
	}
	base := c.uploadBase
	if base == "" {
		base = "https://uploads.github.com"
	}
	endpoint := base + root + "/releases/" + strconv.FormatInt(releaseID, 10) + "/assets?name=" + url.QueryEscape(name)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "KagMCP")
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	// Use the configured transport but a longer timeout for bounded uploads.
	client := &http.Client{Timeout: 120 * time.Second, Transport: c.http.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub upload: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		var payload struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &payload)
		if payload.Message == "" {
			payload.Message = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("GitHub upload HTTP %d: %.200s", resp.StatusCode, payload.Message)
	}
	var data struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Size   int64  `json:"size"`
		State  string `json:"state"`
		Digest string `json:"digest"`
		URL    string `json:"browser_download_url"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}
