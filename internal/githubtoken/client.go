package githubtoken

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	token      string
	http       *http.Client
	base       string
	uploadBase string
}

func New(token string) *Client {
	return &Client{token: token, base: "https://api.github.com", http: &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func (c *Client) Request(ctx context.Context, method, path string, body io.Reader, result any) error {
	if c.token == "" {
		return errors.New("GITHUB_TOKEN/PAT no configurado; introdúcelo desde el panel web o la celda configurar del notebook")
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n") {
		return errors.New("ruta GitHub inválida")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "KagMCP")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("petición a GitHub: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errorMessage struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &errorMessage)
		if errorMessage.Message == "" {
			errorMessage.Message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("GitHub HTTP %d: %.300s", resp.StatusCode, errorMessage.Message)
	}
	if result != nil && len(data) > 0 {
		return json.Unmarshal(data, result)
	}
	return nil
}
func RepoPath(repository string) (string, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return "", errors.New("repository debe ser owner/name")
	}
	for _, v := range parts {
		if v == "" || len(v) > 100 || strings.ContainsAny(v, "\\?&#%:\r\n ") || v == "." || v == ".." {
			return "", errors.New("nombre de repositorio inválido")
		}
	}
	return "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]), nil
}
func (c *Client) Status(ctx context.Context) (any, error) {
	var user struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	}
	if err := c.Request(ctx, "GET", "/user", nil, &user); err != nil {
		return nil, err
	}
	return map[string]any{"authenticated": true, "login": user.Login, "user_id": user.ID}, nil
}
func (c *Client) Repos(ctx context.Context) (any, error) {
	var repos []struct {
		FullName      string `json:"full_name"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
		URL           string `json:"html_url"`
	}
	if err := c.Request(ctx, "GET", "/user/repos?per_page=100&sort=updated", nil, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}
func (c *Client) Branches(ctx context.Context, repository string) (any, error) {
	path, err := RepoPath(repository)
	if err != nil {
		return nil, err
	}
	var branches []struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
	}
	if err := c.Request(ctx, "GET", path+"/branches?per_page=100", nil, &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

type File struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	Type     string `json:"type"`
}

func (c *Client) ReadFile(ctx context.Context, repository, path, ref string) (any, error) {
	prefix, err := RepoPath(repository)
	if err != nil {
		return nil, err
	}
	encoded, err := encodePath(path)
	if err != nil {
		return nil, err
	}
	endpoint := prefix + "/contents/" + encoded
	if ref != "" {
		endpoint += "?ref=" + url.QueryEscape(ref)
	}
	var file File
	if err := c.Request(ctx, "GET", endpoint, nil, &file); err != nil {
		return nil, err
	}
	if file.Type != "file" || file.Encoding != "base64" {
		return nil, errors.New("GitHub devolvió un objeto que no es archivo base64")
	}
	if file.Size > 1<<20 {
		return nil, errors.New("archivo GitHub supera 1 MiB")
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("contenido GitHub inválido: %w", err)
	}
	if len(content) > 1<<20 {
		return nil, errors.New("contenido supera 1 MiB")
	}
	return map[string]any{"repository": repository, "path": file.Path, "sha": file.SHA, "content": string(content), "size": len(content)}, nil
}
func (c *Client) WriteFile(ctx context.Context, repository, path, branch, message, content, sha string) (any, error) {
	prefix, err := RepoPath(repository)
	if err != nil {
		return nil, err
	}
	encoded, err := encodePath(path)
	if err != nil {
		return nil, err
	}
	if branch == "" || message == "" {
		return nil, errors.New("branch y message son obligatorios")
	}
	if len(content) > 1<<20 {
		return nil, errors.New("archivo supera 1 MiB")
	}
	params := map[string]string{"message": message, "content": base64.StdEncoding.EncodeToString([]byte(content)), "branch": branch}
	if sha != "" {
		params["sha"] = sha
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	var result struct {
		Content struct {
			SHA string `json:"sha"`
		} `json:"content"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.Request(ctx, "PUT", prefix+"/contents/"+encoded, strings.NewReader(string(body)), &result); err != nil {
		return nil, err
	}
	return map[string]any{"repository": repository, "path": path, "branch": branch, "file_sha": result.Content.SHA, "commit_sha": result.Commit.SHA}, nil
}
func encodePath(path string) (string, error) {
	path = strings.Trim(path, "/")
	if path == "" || strings.Contains(path, "\\") || strings.ContainsAny(path, "\r\n") {
		return "", errors.New("ruta de archivo GitHub inválida")
	}
	parts := strings.Split(path, "/")
	for i, s := range parts {
		if s == "" || s == "." || s == ".." {
			return "", errors.New("segmento de ruta inválido")
		}
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/"), nil
}
