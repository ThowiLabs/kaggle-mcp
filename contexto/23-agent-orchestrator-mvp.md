# Agent Orchestrator — MVP opcional

Fecha: 2026-10-11. Función: delegación opcional entre chats ChatGPT sin creación automática, con MCP literal destino, ID instancia, ruta absoluta, rama Git, rutas permitidas, criterios de aceptación y contexto reducido; preguntar consentimiento explícito antes de crear.

Arquitectura: internal/agenttasks Store JSON privado + atomic rename, mutex para reclamar, estados pending/in_progress/blocked/needs_review/rejected/approved/cancelled. MCP tools agent_task_create/list/status/prompt/open/submit. Aprobación/rechazo/cancelación sólo mediante /agents tras PIN y CSRF; MCP coordinador distinto del MCP de trabajo externo. El prompt indica distinguir ambos.

Se reutiliza Go estándar y UI Axolotl; sin dependencias nuevas. Tests ciclo de vida, doble reclamo, persistencia, path, MCP discovery, CSRF y revisión. Precisión comercial: las rutas del prompt no son permisos de SO y el nombre del agente no es una identidad fuerte por task. Prioridad futura: aislamiento por worktrees/capability tokens, backends persistentes seguros y verificación automatizada de cambios remotos.
