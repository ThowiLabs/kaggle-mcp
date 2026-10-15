# 14 de octubre de 2026 — Renombrado a Kaggle MCP

## Objetivo
Renombrar `ThowiLabs/kagssh-go` a `ThowiLabs/kaggle-mcp` y actualizar el notebook ejecutable para Kaggle, manteniendo los mecanismos de autenticación y la compatibilidad existente.

## Decisiones tomadas
- Nombre público: **Kaggle MCP**.
- Dirección GitHub renombrada y confirmada: `https://github.com/ThowiLabs/kaggle-mcp`.
- Módulo Go e importaciones internas: `github.com/ThowiLabs/kaggle-mcp`.
- Notebook nuevo: `notebooks/kaggle_mcp.ipynb`; clona `/kaggle/working/kaggle-mcp` desde el nuevo repositorio.
- El cambio posterior a `./cmd/kagmcp` dejó `./cmd/kagssh` como enlace simbólico para workflows heredados; el prefijo de ejecutables `kagmcp-*`, las variables `KAGMCP_*` y el estado `.kagmcp` se conservan por compatibilidad.
- Los registros históricos de `contexto/` y `tareas/` se mantienen intactos salvo actualización del estado inicial vigente.

## Arquitectura actual
Servidor MCP de Go para Kaggle con endpoint HTTP local, OAuth/PIN, túnel Gradio/Cloudflare, panel protegido y herramientas de archivos, proyecto, GPU y GitHub. El notebook Python clona el código, obtiene Go si se necesita, compila y ejecuta el binario en Kaggle.

## Librerías usadas
Go (según `go.mod`), biblioteca estándar de Python para notebook y scripts. No se introdujeron dependencias.

## Archivos importantes modificados
`go.mod`, importaciones `cmd/` e `internal/`, interfaz del panel, README y guías de uso, `.github/workflows/release.yml`, `scripts/check_notebook.py`, renombre del notebook.

## Problemas encontrados
El repositorio histórico y el notebook seguían usando `kagssh-go`; renombrar solamente el título habría dejado referencias incompatibles. El historial anterior explica decisiones antiguas y no debe reescribirse retrospectivamente.

## Soluciones implementadas
Rutas de GitHub, módulo Go, importaciones y branding activo actualizados. El validador del notebook comprueba ahora el URL y el directorio de clonación para prevenir regresiones.

## Pruebas y límites
`python3 scripts/check_notebook.py` y 11 pruebas de versiones superadas. `go test -timeout 120s ./...`, `go vet ./...`, compilación local y pruebas JS del panel superadas. Pendiente ejecución real dentro de Kaggle con red y autenticación externa: la validación local no reproduce todas las políticas del runtime de Kaggle.

## Pendientes
Comprobar la ejecución real del notebook en Kaggle y revisar cualquier enlace externo antiguo que pudiera depender del nombre anterior.

## Próximos pasos
Usar el notebook nuevo en Kaggle; mantener Internet habilitado, comprobar el entorno y autorizar exclusivamente clientes MCP de confianza. El notebook anterior puede conservarse como copia personal, pero no debe distribuirse como la versión actual.
