# Contexto 16 — Integración GitHub API total con PAT desde MCP

Fecha: 2026-10-09
Proyecto exclusivo: C:\Users\Admin\Documents\GitHub\kagssh-go

## Objetivo
Ampliar las cinco herramientas GitHub previas para dar a los agentes autenticados acceso a todas las operaciones que la REST/GraphQL API de GitHub permita dentro de los permisos del PAT, sin Git instalado cuando se opera por API.

## Implementación
- `internal/githubtoken/extended.go` con `API(method,endpoint,json)` sobre `https://api.github.com`: GET/POST/PUT/PATCH/DELETE, validación de URL/rutas, JSON de entrada ≤1MiB y respuesta JSON ≤2MiB, Bearer token y versión fija API.
- `GraphQL(query,variables)` para queries y mutaciones. No acepta URLs absolutas/hosts externos ni token en query.
- Atajos MCP: `github_repo_create`, `github_repo_delete`, `github_branch_create`, `github_branch_delete`, `github_file_delete`, `github_pr_create`, `github_pr_merge`, `github_issue_create`, `github_issue_update`, `github_release_create`, `github_workflow_dispatch`, `github_workflows`, `github_api`, `github_graphql`.
- `github_repo_create` exige booleano `private` explícito (para impedir creación pública accidental).
- `github_api` cubre además organización, workflows, permisos, colaboradores, reviews, comentarios, Git Trees/Commits y otros endpoints REST no empaquetados como herramienta dedicada.
- El PAT permanece en memoria y se configura desde el panel web/notebook; no se envía a respuestas de herramientas. Operaciones destructivas necesitan autorización expresa; scopes y restricciones GitHub siguen aplicando.

## Pruebas
HTTP servidor GitHub simulado sin credenciales reales: token solo en Authorization, API Version fijada, los cinco métodos REST, CreateRepository user/org, CreateBranch con SHA, DeleteFile con SHA/JSON, GraphQL y entradas malformadas. Esquemas MCP y validación de argumentos probados. No se hicieron cambios en la cuenta GitHub del usuario.

## Límites importantes
La API JSON tiene límites tamaño; subida de release assets binarios, creación de secretos cifrados, Git LFS y casos especiales requieren payload específico o implementación especializada. No se puede ampliar scopes automáticamente. Git local sigue requerido para clonar/código local/project_verify. Queda pendiente E2E real usando PAT autorizado y destino seguro.
