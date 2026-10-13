# Tarea 18 — Evitar túnel Gradio muerto tras minutos

Fecha: 2026-10-09.
- [x] Revisar Goradio solo lectura; no alterar código trabajado por otro agente.
- [x] Identificar ausencia de supervisor en KagMCP aunque el FRP ya hace heartbeat.
- [x] Detectar cierre de control FRP y fin del proceso Cloudflare.
- [x] Vigilar health pública nonce cada 20s; 5 fallos consecutivos antes de reconectar.
- [x] Soportar errores intermitentes y reiniciar el contador tras recuperación.
- [x] Reconectar túnel con backoff, Gradio primero y fallback solo en auto.
- [x] Reintentar también fallos de red durante arranque.
- [x] Vincular OAuth a URL nueva, sin reutilizar tokens del enlace antiguo.
- [x] Mantener PIN modificado, proyectos, memoria, skills, configuración GitHub.
- [x] Dejar nueva URL en logs y JSON privado de estado.
- [x] Añadir pruebas automatizadas de caídas y regeneración OAuth.
- [ ] Validar disponibilidad real de Gradio + Kaggle durante varias horas.
