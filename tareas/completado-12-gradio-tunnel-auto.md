# Tarea 12 — Gradio FRP predeterminado y respaldo automático Cloudflare

Fecha: 2026-10-09
Estado: código y pruebas locales completadas, prueba de Kaggle pendiente.

- [x] Implementar cliente FRP TLS v0.3 sin Python con hashes fijados y caché privada.
- [x] Crear `MCP_TUNNEL=auto` por defecto, probar primero Gradio FRP.
- [x] `MCP_GRADIO_RETRIES` configurable de 1 a 5 (default 3), espera progresiva.
- [x] Mantener `gradio`, `cloudflare`, `none` explícitos sin fallback cruzado.
- [x] Atender health HTTP real antes de comprobar el túnel, desafío aleatorio por proceso.
- [x] Rechazar URLs Gradio inválidas y enlaces que no respondan al proceso correcto.
- [x] Activar OAuth/panel únicamente después de verificar el túnel.
- [x] Cerrar FRP fallido antes de probar el siguiente intento o Cloudflare.
- [x] Actualizar notebook, README y manual.
- [x] Añadir pruebas de URL, checksums, reintentos, fallback, health y validación de configuración.
- [ ] Ejecutar prueba real de Kaggle + FRP + URL .gradio.live + OAuth ChatGPT.

Referencia: `contexto/13-gradio-frp-fallback-cloudflare.md`.
Workspace: `C:\Users\Admin\Documents\GitHub\kagssh-go`.
