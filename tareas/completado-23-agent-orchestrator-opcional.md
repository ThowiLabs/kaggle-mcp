# Agent Orchestrator opcional implementado (2026-10-11)

Objetivo: coordinar dos chats con destino MCP explícito y consentimiento específico del usuario. Implementado almacenamiento privado, prompt reusable y tareas persistentes con estados; herramientas MCP create/list/status/prompt/open/submit/review (review solo lectura), interfaz /agents con pregunta explícita Sí/No, creación, copiado, seguimiento y aprobación/rechazo/cancelación bajo PIN + CSRF. El destino $pl0 fue exclusivamente un ejemplo; ningún otro MCP fue modificado.

Validado: tests de consentimiento, delimitación de rutas, reclamo simultáneo, bloqueos, entrega y revisión, persistencia de archivo y sesión, API MCP y UI autenticada, Go test/vet, JS check, build cruzado Linux y captura de Chrome desktop para estados sin tarea y con entrega. Documentación: AGENT_ORCHESTRATOR.md. Limitaciones comerciales/QA real: tarea pendiente-24 y pendiente-20.
