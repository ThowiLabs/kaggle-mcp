package githubtoken

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

var allowedMethod = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
var denyHeaderChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)

// API permite toda la API REST oficial de GitHub dentro de los permisos
// del PAT: sin ejecutar git, sin exponer token ni redireccionar a otros hosts.
func (c *Client) API(ctx context.Context, method, path string, body json.RawMessage) (any, error) {
	method = strings.ToUpper(method)
	if !allowedMethod[method] {
		return nil, errors.New("método GitHub debe ser GET, POST, PUT, PATCH o DELETE")
	}
	if len(path) > 3000 || len(path) < 2 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") ||
		strings.Contains(path, "\\") || denyHeaderChars.MatchString(path) || strings.Contains(path, "#") {
		return nil, errors.New("ruta GitHub inválida")
	}
	u, err := url.ParseRequestURI(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/") ||
		strings.HasPrefix(u.Path, "//") {
		return nil, errors.New("ruta API GitHub inválida")
	}
	for _, p := range strings.Split(u.Path, "/")[1:] {
		if p == "." || p == ".." {
			return nil, errors.New("ruta con segmentos prohibidos")
		}
	}
	if strings.Contains(strings.ToLower(u.RawQuery), "token=") || strings.Contains(strings.ToLower(u.RawQuery), "access_token=") {
		return nil, errors.New("nunca pongas credenciales en la URL")
	}
	if len(body) > 1<<20 {
		return nil, errors.New("body GitHub supera 1 MiB")
	}
	if len(body) > 0 && !json.Valid(body) {
		return nil, errors.New("body debe ser JSON válido")
	}
	if (method == "GET" || method == "DELETE") && len(body) > 0 {
		return nil, errors.New("usa GET/DELETE sin body")
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = strings.NewReader(string(body))
	}
	var response json.RawMessage
	if err := c.Request(ctx, method, path, reader, &response); err != nil {
		return nil, err
	}
	if len(response) == 0 {
		return map[string]any{"ok": true, "method": method, "path": u.Path}, nil
	}
	var result any
	if err := json.Unmarshal(response, &result); err != nil {
		return nil, fmt.Errorf("respuesta JSON de GitHub inválida: %w", err)
	}
	return result, nil
}
func (c *Client) GraphQL(ctx context.Context, query string, variables json.RawMessage) (any, error) {
	if strings.TrimSpace(query) == "" || len(query) > 100000 {
		return nil, errors.New("query GraphQL vacía o demasiado larga")
	}
	if len(variables) == 0 {
		variables = []byte("{}")
	}
	if !json.Valid(variables) || variables[0] != '{' {
		return nil, errors.New("variables deben ser objeto JSON")
	}
	payload, err := json.Marshal(map[string]any{"query": query, "variables": json.RawMessage(variables)})
	if err != nil {
		return nil, err
	}
	return c.API(ctx, "POST", "/graphql", payload)
}
func (c *Client) CreateRepository(ctx context.Context, name, description string, private bool, ownerType, organization string) (any, error) {
	if name == "" || len(name) > 100 {
		return nil, errors.New("nombre de repo inválido")
	}
	endpoint := "/user/repos"
	if ownerType == "org" {
		if organization == "" || strings.ContainsAny(organization, "/\\ \r\n?#") {
			return nil, errors.New("organización inválida")
		}
		endpoint = "/orgs/" + url.PathEscape(organization) + "/repos"
	} else if ownerType != "" && ownerType != "user" {
		return nil, errors.New("owner_type debe ser user u org")
	}
	body, _ := json.Marshal(map[string]any{"name": name, "description": description, "private": private, "auto_init": false})
	return c.API(ctx, "POST", endpoint, body)
}
func (c *Client) CreateBranch(ctx context.Context, repo, branch, fromSHA string) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	if branch == "" || strings.HasPrefix(branch, "/") || strings.ContainsAny(branch, "\r\n\x00") || strings.Contains(branch, "..") || len(branch) > 200 {
		return nil, errors.New("rama inválida")
	}
	if !gitSHA.MatchString(fromSHA) {
		return nil, errors.New("from_sha debe ser SHA Git completo")
	}
	body, _ := json.Marshal(map[string]any{"ref": "refs/heads/" + branch, "sha": fromSHA})
	return c.API(ctx, "POST", root+"/git/refs", body)
}

var gitSHA = regexp.MustCompile(`^[a-fA-F0-9]{40}([a-fA-F0-9]{24})?$`)

func (c *Client) DeleteFile(ctx context.Context, repo, path, branch, message, sha string) (any, error) {
	root, err := RepoPath(repo)
	if err != nil {
		return nil, err
	}
	file, err := encodePath(path)
	if err != nil {
		return nil, err
	}
	if branch == "" || message == "" || sha == "" {
		return nil, errors.New("branch, message y sha son obligatorios")
	}
	// DELETE /contents requiere JSON body: GitHub lo documenta explícitamente.
	body, _ := json.Marshal(map[string]any{"message": message, "sha": sha, "branch": branch})
	var response json.RawMessage
	if err := c.Request(ctx, "DELETE", root+"/contents/"+file, strings.NewReader(string(body)), &response); err != nil {
		return nil, err
	}
	var output any
	if err := json.Unmarshal(response, &output); err != nil {
		return nil, err
	}
	return output, nil
}
