# Tarea 07 — Corregir JSON Schema de Tools MCP

**Estado: completado con pruebas locales**
Fecha: 2026-10-09

## Problema
ChatGPT muestra `Authentication succeeded, action discovery failed` tras `server/discover` y `tools/list` incluso con el protocolo moderno instalado.

## Error demostrado
El esquema generado para herramientas sin campos obligatorios contenía `"required":null`. Es JSON Schema inválido; por ejemplo `environment_info`, `github_status`, `github_repositories` y `list_dir`. Una nueva prueba falló con el código anterior reproduciendo los cuatro errores.

## Corrección
Omitir `required` cuando la lista está vacía. Los esquemas con argumentos obligatorios siguen enviando una lista de strings.

## Regresiones
- `TestAllToolSchemasJSONSchemaRequiredArrays`.
- `TestOAuthThenActionDiscoveryModern` mejorado para comprobar el `required` recibido por HTTP desde `tools/list` tras OAuth real.
- `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, validador notebook, compilación cruzada Linux amd64.

## Instrucciones
Detener celda de ejecución; clonar/actualizar, compilar y arrancar nuevamente. La celda run no actualiza el código por sí misma.
