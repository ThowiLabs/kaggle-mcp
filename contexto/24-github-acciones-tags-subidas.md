# GitHub Actions / tags / archivos — 2026-10-11

Ampliado MCP: operaciones GitHub en internal/githubtoken/operations.go y internal/mcp/github_tools.go: tags lightweight/annotated por git/tags+git/refs, tags list/delete; workflows upsert por Contents, enable/disable, runs list/get/rerun/cancel; uploads binarios de ruta /kaggle/working a repo <=1MiB y release assets <=50MiB vía uploads.github.com sin redirigir credenciales. Todas las mutaciones nuevas requieren confirmed=true. github_workflow_dispatch ahora también exige confirmación explícita. Lo que ya cubre github_api y github_graphql no se duplica.

GitHub API permisos recomendados: Contents RW, Workflows RW, Actions RW, extra según funcionalidad. No hay “todos los permisos” garantizados: PAT y políticas GitHub mandan. Limitación de artefactos de Actions: se suben desde workflows, no mediante REST común; GitHub API los lista/descarga/borra. Tests offline httptest, go test/vet, Linux cross-compile, revisión de esquemas MCP.

No se hizo push ni se modificó GitHub remoto. Documentación: GITHUB_CAPACIDADES.md.
