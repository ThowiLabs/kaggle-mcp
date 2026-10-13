# 2026-10-09 — Extensión de verificación obligatoria para Gradio

## Objetivo
Continuar la implementación de Ponytail v2 y mejorar la garantía de la regla del usuario: primero repositorio y notebook funcionales, después UI Gradio. Fortalecer recuperación de memoria web.

## Decisiones tomadas
La comprobación anterior de `gradio_scaffold` solo exigía el booleano `tests_passed` del cliente y un `.ipynb` sintácticamente válido. Era posible indicar `true` sin ejecutar pruebas. Se reemplaza por el método `project_verify` que ejecuta `test_command` y `notebook_command` en `/kaggle/working/<project_id>`, exige repo Git limpio con commit, notebook válido y huellas de commit/notebook iguales antes y después.

El comprobante se persiste en `internal/projectstate.Project.Verification` con UTC, SHA git, SHA256 del notebook y hashes de los comandos. `gradio_scaffold` exige recibo válido y actual; el parámetro legacy `tests_passed` no basta. Un proyecto restaurado desde repo invalida el recibo para forzar tests en el nuevo Kaggle. Todo el contexto importado debe revisarse por un agente autorizado.

El panel `/` dispone de POST /import autenticado con sesión y CSRF, además de /export. Muestra verificaciones y pendientes por proyecto. Corrigió la auditoría de comandos con error de shell cuyo resultado HTTP era éxito pero el payload incluía `error`.

## Arquitectura actual
Servidor Go HTTP/OAuth compartido, Ponytail v2 embed siempre anunciada, herramientas MCP, portal web, memoria JSON privada, exportación Git versionable, vigilante de disco, generación Gradio como plantilla con lock posterior. No se modificó Lilith-MCP ni el directorio fuente Ponytail.

## Librerías usadas
Go 1.26.9 estándar, Gradio 6.30.0 y pip-tools 7.6.2 fijados en el proyecto generado. Ambas versiones verificadas disponibles en PyPI a 2026-10-09, pero todavía no instaladas ni ejecutadas dentro de Kaggle.

## Archivos importantes modificados
`internal/mcp/verification.go`, `internal/mcp/management.go`, `internal/mcp/server.go`, `internal/projectstate/store.go`, `internal/projectstate/verification.go` y test, `internal/mcp/management_test.go`, `internal/dashboard/dashboard.go` y test, README y guía.

## Problemas encontrados
La declaración `tests_passed` no era evidencia; no se podía restaurar memoria desde el panel. Los runtimes Kaggle son efímeros; importar información del repositorio no demuestra el funcionamiento actual. Las pruebas del usuario deben ser reales y adecuadas al stack.

## Soluciones implementadas
Protocolo MCP `project_verify`; comprobante de verificación por proyecto; invalidación cuando el repositorio/notebook cambien o tras importación; UI de restauración; documentación.

## Estado de tests
Go tests generales y de seguridad web, `go test -race`, `go vet`, validador notebook, build Linux amd64 y `git diff --check` pasaron localmente. No se ha probado aún el runtime Kaggle real; publicar commit cuando esté listo.

## Estado de seguridad
Comandos siguen bajo OAuth y privilegios del runtime, con shell permitido a agentes confiables. La verificación puede ejecutar código del proyecto; debe solicitarla solo un cliente autorizado. El Git y notebook deben estar limpios para reducir comprobantes obsoletos. La verificación no puede decidir la calidad semántica de los tests. La web no comparte sesiones OAuth, tiene PIN/CSRF/rate limit. No hay pruebas reales desde ChatGPT ni Kaggle en este entorno.

## Pendientes
Prueba e2e en Kaggle: ejecutar `project_verify` con test y notebook existentes, generar y completar Gradio, acceder al dashboard con HTTPS y PIN. No exponer el PAT ni publicar notas privadas en Git sin revisar.
