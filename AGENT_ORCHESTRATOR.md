# KagMCP Agent Orchestrator — MVP opcional

## Uso (control humano)

La ruta autenticada **/agents** pregunta primero: ¿quieres usar un subagente? No crea nada si respondes No. Si respondes Sí, debes indicar título, objetivo, conector MCP literal (por ejemplo `$pl0`, **solo ejemplo**), identificador de instancia, directorio absoluto, rama Git, rutas relativas permitidas, criterios de aceptación y contexto mínimo. El formulario exige consentimiento explícito para esa tarea. El MCP también exige `user_approved=true` en `agent_task_create`; es una declaración del cliente, **no prueba criptográfica de voluntad del usuario**. El orquestador debe preguntar cada vez.

Una vez creada, copia el prompt resultante a otro chat. Ese chat debe disponer de dos conexiones cuando sean diferentes: (1) **KagMCP coordinador**, donde usar `agent_task_open/status/submit`; (2) el **MCP de trabajo** indicado en el prompt, desde el que manipula el repositorio correspondiente. Debe verificar antes de cualquier cambio que el conector y ruta corresponden. Un chat temporal puede utilizarse solo si dispone realmente de ambos MCP.

El agente secundario no se crea automáticamente. La herramienta NO abre chats ChatGPT ni instala conectores, no inicia procesos externos, no ejecuta cambios Git ni transfiere credenciales. El prompt es autosuficiente si un chat no puede conectarse al coordinador, pero entonces no podrá registrar el estado automáticamente.

## Herramientas MCP

| Herramienta | Propósito |
| --- | --- |
| `agent_task_create` | Crear asignación aprobada e incluir prompt completo para el otro chat |
| `agent_task_prompt` | Recuperar prompt |
| `agent_task_list` | Consultar tablero |
| `agent_task_status` | Ver objetivo, entrega y revisión |
| `agent_task_open` | Reclamar trabajo pendiente (excluye reclamos simultáneos) |
| `agent_task_submit` | Declarar `needs_review` (commit, pruebas, resumen) o `blocked` (motivo) |
| `agent_task_review` | Devuelve checklist, commit y criterios al orquestador; NO modifica ni aprueba la tarea |

La **aprobación/rechazo/cancelación** se gestiona en **/agents** con sesión y CSRF, **no con herramientas MCP del agente**. Una tarea rechazada puede volver a reclamarla el mismo agente. Tras cancelación, el coordinador bloquea reclamos y entregas, pero **no puede detener un chat externo en ejecución**.

## Seguridad y límites comerciales

- Estado persistente privado en `/kaggle/working/.kagmcp/state/agent-tasks.json` con archivo 0600, cambios serializados y escritura atómica; límite 150 tareas / 2 MiB.
- No se guardan tokens ni contraseñas como parte del prompt. Nunca pegues secretos en objetivos/contexto.
- Los permisos de archivo, MCP, Git o nube **no quedan restringidos técnicamente por las rutas del prompt**. Son reglas operativas. La rama o worktree aislado debe ser creada y verificada por el agente o los administradores. No se permite declarar cumplimiento sin pruebas.
- Los clientes MCP autorizados comparten los permisos actuales del servidor: el nombre `agent` es una etiqueta y no una identidad autenticada de forma separada. Para venderlo como producto multiusuario hace falta autorización por tarea, capacidades específicas por agente, aislamiento de runtime y validación real de entregas antes de integrar.
- La revisión del panel registra una decisión, pero no verifica automáticamente que el SHA de otro MCP sea auténtico. Revisar diff y pruebas en el conector real antes de aprobar.
- Si Kaggle destruye el volumen entre sesiones, pueden perderse las tareas. Configurar almacenamiento y backup externos antes de una oferta comercial con retención garantizada.
- Ningún conector ajeno al KagMCP que contiene esta implementación es modificado automáticamente.

## Ejemplo de flujo

1. Orquestador pregunta autorización. Usuario confirma Sí.
2. `agent_task_create` crea una tarea con MCP destino literal, path, rama, objetivo, criterios y contexto.
3. Usuario copia prompt a chat nuevo conectado al MCP coordinador y al MCP de trabajo.
4. Subagente llama `agent_task_open`, comprueba entorno real y trabaja en rama aislada.
5. Subagente llama `agent_task_submit` con commit/pruebas/resumen o reporta bloqueo.
6. Orquestador consulta `agent_task_review`, inspecciona cambios reales con las herramientas Git y expone la revisión. El usuario aprueba/rechaza mediante el panel /agents.

Verificación: `go test ./...`, `go vet ./...`, `node --check internal/dashboard/ui/dashboard.js`, build cruzado Linux.
