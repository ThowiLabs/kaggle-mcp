# Tarea 05 — KagMCP: OAuth, GitHub PAT y túneles HTTPS

**Estado: completado (implementación y pruebas locales)**
Fecha: 2026-10-09

## Entregado
- Servidor HTTP MCP, discovery OAuth, DCR/CIMD y consentimiento protegido con `MCP_ACCESS_PIN`.
- Cloudflare Quick Tunnel automático; opción de proxy HTTPS externo (`MCP_TUNNEL=none`).
- **GitHub con Personal Access Token directamente en `GITHUB_TOKEN`**, sin GitHub Apps, callback ni registro OAuth GitHub.
- Herramientas MCP para archivos y shell de Kaggle; GitHub para estado, repositorios, ramas, lectura y escritura de archivos.
- Kaggle Secrets individuales con prioridad por export y preferencias no sensibles en `/kaggle/working/.kagmcp/config.json`.
- Notebook interactivo, sin procesos Go desacoplados, logs en la misma celda y Stop nativo.
- README, guía, ejemplo .env, CI y `contexto/06-kagmcp-oauth-github-token.md` actualizados.

## Evidencia local
- `python scripts/check_notebook.py`: correcto (estructura, logs e interrupción simulados).
- `go test -count=1 ./...`: correcto.
- `go vet ./...`: correcto.
- `go test -race -timeout 90s ./...`: correcto (todas las suites; el comando combinado posterior que intentó compilar sin GOOS falló por los build tags Linux, no por los tests).
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ... ./cmd/kagssh`: correcto.
- `git diff --check`: correcto.

## Limitaciones honestas
- Falta comprobar **en Kaggle real** el túnel Quick Tunnel, autorización completa desde ChatGPT y desconexión real por botón Stop.
- GitHub PAT se usa para las herramientas iniciales de archivos/repos/ramas; clone/commit/push multiarchivo aún no se implementa.
- No se renombró GitHub ni el path del módulo Go.

## Archivo de contexto
`contexto/06-kagmcp-oauth-github-token.md`.
