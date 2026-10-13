# Contexto 19 — Supervisión continua de Gradio Live y reconexión

Fecha: 2026-10-09.
KagMCP workspace: `C:\Users\Admin\Documents\GitHub\kagssh-go`.
Goradio: `C:\Users\Admin\Documents\GitHub\Goradio`, **exclusivamente lectura**, ningún archivo escrito ni commit hecho allí.

## Problema confirmado por inspección de código
Usuario: la URL `gradio.live` que publica KagMCP se queda muerta tras unos minutos. El módulo FRP nativo de KagMCP ya disponía de heartbeat y evento `Done`, pero `Run` comprobaba `/api/health` **solo al arrancar** y luego esperaba ctx o cierre de HTTP; no supervisaba ni recuperaba el túnel. Goradio tiene un monitor público cada 20 segundos con 5 fallos antes de renovar URL, más detección de canal FRP y reintentos con backoff.

## Cambios
- `internal/mcp/recovery.go`: watcher `watchPublicTunnel` sobre canal `Done` nativo y `/api/health` HTTPS con nonce exacto. Cada 20 s, 5 fallos; fallo transitorio se reinicia al recuperar.
- `Run` inicia un loop que observa cierre FRP y salud pública. Ante caída retira rutas OAuth transitoriamente (`bridge.Set(nil)`), cierra túnel viejo, reintenta `startTunnel` que valida nonce y devuelve uno nuevo; conserva modo `auto` Gradio→Cloudflare. Nuevos arranques también reintentan fallos externos con backoff 1-2-4-8-16s.
- `rebuildMCPForPublic` conserva `old.pinState` (incluido PIN cambiado) y `projects`, `skills`, GitHub client en memoria. Reconstruye OAuth ligado al issuer/recurso actual; un nuevo hostname invalida tokens del anterior (necesita reconectar ChatGPT). No imprime secretos.
- `gradiolive.Tunnel.Err()` devuelve de forma segura el error de cierre del FRP nativo después de `Done`.
- `/kaggle/working/.kagmcp/state/tunnel-status.json` (0600) contiene status/provider/mcp_url/updated_at, sin PIN/PAT.
- Logs identifican evento y nueva URL. Si hostname cambia, no hay modo de mantener la conexión ChatGPT antigua; requiere actualización manual o dominio propio estable.

## Pruebas
`internal/mcp/recovery_test.go`: canal de control caído, 5 fallos consecutivos y fallos intermitentes, cancelación, backoff, nueva URL OAuth, PIN cambiado, tokens anteriores inválidos, nuevo token OAuth aceptado y `tools/list` válido, Projects/Skills/PAT preservados y status JSON actualizado.
Suite Go completa, race, go vet, notebook, build Linux y git diff --check al cerrar.

## Limitaciones
No se ha probado el servicio externo Gradio en Kaggle durante horas en esta ejecución. El tiempo de vida del entorno Kaggle es ajeno a este componente. Gradio Live / Cloudflare quick tunnel no dan dominio fijo; al renovar URL hay que actualizar conexión ChatGPT. Para URL permanente usa `MCP_TUNNEL=none` con proxy/dominio HTTPS propio.
