# 14 de octubre de 2026 — Comando principal cmd/kagmcp

## Fecha
2026-10-14.

## Objetivo
Renombrar el ejecutable Go en `cmd/kagssh` a `cmd/kagmcp`, manteniendo la compilación del notebook y los workflows de GitHub Actions.

## Decisiones tomadas
- Código del comando principal en `cmd/kagmcp/main.go`.
- `cmd/kagssh` es un enlace simbólico relativo a `kagmcp`, sin duplicación de código.
- Los workflows `.github/workflows/ci.yml` y `release.yml` siguen apuntando al alias temporal porque la integración GitHub no dispone de permiso para modificarlos.
- El notebook `notebooks/kaggle_mcp.ipynb`, el README y la guía compilan directamente `./cmd/kagmcp`.
- Se conservan `KAGMCP_*`, `MCP_*`, estado `.kagmcp` y nombres de los binarios para compatibilidad.

## Arquitectura actual
`cmd/kagmcp/main.go` arranca el servidor; el alias `cmd/kagssh` se resuelve al mismo paquete. Los paquetes de aplicación permanecen en `internal/`.

## Librerías usadas
Go y estándar Python; sin dependencias nuevas.

## Archivos importantes modificados
`cmd/kagmcp/main.go` (renombrado), `cmd/kagssh` (symlink), `notebooks/kaggle_mcp.ipynb`, `scripts/check_notebook.py`, `README.md`, `USO_Y_CONFIGURACION.md`, `contexto/00-estado-actual-mcp.md`, `contexto/33-renombrado-kaggle-mcp.md`, `tareas/completado-34-renombrar-comando-kagmcp.md`.

## Problemas encontrados
GitHub rechaza cambios en workflows a través de la GitHub App por falta de permiso `workflows`. No se omite ni rompe la compilación: se mantiene un alias Go probado para rutas heredadas.

## Soluciones implementadas
Nuevo comando real, enlace simbólico relativo que compila en Linux, variables locales del notebook renombradas a `kagmcp` y verificación de ruta y alias en `scripts/check_notebook.py`.

## Pruebas
- `go test -timeout 120s ./...`: OK.
- `go vet ./...`: OK.
- `python3 scripts/check_notebook.py`: OK, sintaxis y estructura del notebook.
- `python3 -m unittest discover -s scripts -p 'test_*.py'`: 11 pruebas OK.
- Compilación estática con `CGO_ENABLED=0` desde `./cmd/kagmcp` y desde el alias `./cmd/kagssh`: OK; `readelf` sin PT_INTERP, `-version` OK.

## Pendientes
Prueba de integración real en Kaggle y, al habilitar permisos de workflows, migrar CI/release a `./cmd/kagmcp` y retirar el alias cuando no haya consumidores legacy.

## Próximos pasos
Descargar/abrir el notebook actualizado desde GitHub y probar el arranque completo de Kaggle MCP en Kaggle con Internet y OAuth/PIN.
