# Tarea 09 — Skills Ponytail v2, memoria, tareas, historial y dashboard web

**Estado: implementación y validaciones locales completadas; integración externa Kaggle pendiente**
Fecha: 2026-10-09
Workspace único: `C:\Users\Admin\Documents\GitHub\kagssh-go`

## Implementación

- [x] Leer `C:\Users\Admin\Documents\codex-ponytail-v2.md` y copiarlo íntegro sin modificar el original.
- [x] Añadir reglas Kaggle: repositorio y cuaderno funcional, pruebas, Gradio final, versiones/hashes y control de disco.
- [x] Exponer Skills MCP con registro, paginación, búsqueda e instalación privada.
- [x] Incorporar proyectos, memoria y tasklist compartida con sincronización de estado por mutex y escritura atómica.
- [x] Registrar historial de comandos con descripción, proyecto, fecha y resultado, sin stdout.
- [x] Exportar a Markdown+JSON versionable; restaurar el estado de un proyecto clonado.
- [x] Dashboard URL raíz en el mismo túnel HTTPS; PIN, sesiones, CSRF, rate limiting, logout.
- [x] Consultar proyectos/tareas/memoria/Skills/historial y configurar GitHub PAT de forma efímera por web.
- [x] Guardarraíl de disco Linux, monitor periódico, rechazo de escrituras/cancelación de comandos.
- [x] Gradio scaffold condicionado a checks, notebook válido, repo Git y versión exacta de Gradio, con lock con hashes documentado.
- [x] Tests unitarios y regresión HTTP de seguridad, documentación y notebook actualizados.

## Validaciones
`go test -count=1 ./...`
`go test -race -timeout 90s ./...`
`go vet ./...`
`python scripts/check_notebook.py`
Build Linux `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`.

## Limitaciones
La interfaz Gradio generada es un **scaffold**, que cada agente debe convertir en aplicación funcional conectada al proyecto. La memoria local no es persistente tras destruir Kaggle; requiere exportación y versionado de JSON/Markdown, y reimportación al nuevo runtime. No se realizó conexión real de ChatGPT a un runtime activo ni validación de producción.

## Próxima prueba manual
Ejecutar el notebook actualizado en Kaggle, comprobar `/mcp` y `/`, iniciar sesión con PIN, crear proyecto, memoria, tareas, probar export/import, autenticar GitHub, y hacer Gradio después de tests reales.
