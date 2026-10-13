# 2026-10-09 — Ponytail v2, proyectos multiagente y panel protegido

## Objetivo
Integrar la Skill `C:\Users\Admin\Documents\codex-ponytail-v2.md` en KagMCP con prioridad permanente, sin leer la versión de Lilith-MCP. Proteger una administración web en `/`, registrar comandos, compartir memoria y tareas por proyecto, respaldar la información en Git y crear Gradio tras pruebas.

## Decisiones y arquitectura
- Fuente de Ponytail v2 copiada íntegra (más de 83 KiB) a `internal/skills/ponytail-v2.md`, se agregó extensión **35** para trabajos Kaggle. `go:embed` la incorpora al binario Go. No se modifica el documento original, ni Lilith.
- `internal/skills`: registro de Skills, búsqueda, lectura paginada (hasta 12 KiB por fragmento), instalación privada y protección de la Skill principal. `skills.Summary` se publica como instrucciones activas en `server/discover` e `initialize`.
- `internal/projectstate`: JSON privado con escritura atómica `/kaggle/working/.kagmcp/projects.json`, mutex compartido, máximo 100 proyectos, 200 notas/tareas por proyecto, 1000 eventos, tamaño <=4MiB. Procesos/agents que usan el mismo servicio Go comparten este store; no es base de datos distribuida.
- MCP incluye `skills_list/read/search/install`, `projects_list`, `project_create`, `project_memory_add`, `project_export/import`, `tasks_add/update`, `history_list`, `gradio_scaffold`. `exec` acepta `project` y `description`; registra éxito/error y comando filtrado, jamás stdout.
- `internal/dashboard` registrado bajo `/` en el mismo HTTPS temporal: login por PIN, sesiones con token aleatorio (8h, máximo 100), cookies Secure/HttpOnly/SameSite Strict, CSRF en POST, Origin matching, tasa por IP/global y logout; páginas de proyectos/tareas/memoria/historial/Skills y PAT personal GitHub. GitHub se verifica por REST y se retiene solo en RAM, protegida por mutex.
- `internal/diskguard`: Linux Statfs, umbral 512MiB o 3% libre, monitor 5s, protección de escritura, monitor 2s durante comando y cancelación.
- `gradio_scaffold` exige declaración de tests ejecutados, repo `.git` y notebook `.ipynb` JSON válido; genera `app.py` como plantilla (requiere conectar lógica real), `requirements-gradio.in` con `gradio==6.30.0` y guía de lock con hashes, compilador `pip-tools==7.6.2`.
- `project_export` crea Markdown + JSON en el repo, sin push automático; `project_import` restaura JSON desde repo después de clone. Revisar contenido sensible antes de sincronizar.

## Librerías usadas
Go 1.26.9 y su stdlib (no se añaden dependencias Go); se mantiene OAuth/Cloudflare/GitHub PAT existente. Gradio es artefacto producido para proyectos, no dependencia del portal.

## Archivos importantes
`internal/skills/`, `internal/projectstate/`, `internal/dashboard/`, `internal/diskguard/`, `internal/mcp/server.go`, `internal/mcp/management.go`, README, manual y notebook.

## Problemas y soluciones
Kaggle puede quedar readonly al colmar su volumen; se emplean umbrales preventivos, no cuotas garantizadas. El disco efímero no persiste al destruir runtime: exportar/importar contexto por Git versionable, con revisión previa del contenido. El PAT web no se escribe al disco. Los agentes MCP pueden ejecutar root, por lo que no existe aislamiento de seguridad contra clientes autorizados maliciosos.

## Estado de tests
Go `go test -count=1 ./...`, `go test -race -timeout 90s ./...`, `go vet ./...`; además validador `scripts/check_notebook.py`, build Linux amd64 y comprobación `git diff --check`.

## Estado de seguridad
Login web seguro básico, rate limits IP/global, tokens y CSRF aleatorios; secreto PIN derivado SHA256 solo en memoria del handler; session tokens almacenados hashed. Registrar comandos exige cautela para no revelar secretos; filtro conservador. Conexión Go HTTP loopback detrás de Cloudflare HTTPS. No verificado end-to-end en Kaggle con un navegador público real.

## Pendientes y próximos pasos
Realizar en un runtime Kaggle la comprobación de `https://...trycloudflare.com/`, login/CSRF, configuración PAT y un flujo de proyecto repo+notebook+Gradio. Comprobar el comportamiento del monitor de disco bajo presión sin agotar una sesión real. Refinar la UI Gradio de cada proyecto concreto después de sus verificaciones.
