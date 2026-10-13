# Contexto 15 — Túnel Gradio con cliente FRP 100 % Go

Fecha: 2026-10-09
Proyecto exclusivo: C:\Users\Admin\Documents\GitHub\kagssh-go

## Referencia y alcance
Se revisaron en modo lectura `C:\Users\Admin\Documents\GitHub\Goradio\gradio_native.go` y `internal/frpmsg/*.go`, sin tocar archivos ni indexar cambios del agente que trabaja allí. El usuario compartió el código nativo a través del chat.

## Implementación
- Se reemplazó `internal/gradiolive/gradiolive.go`: ya no ejecuta ni descarga `frpc`.
- `internal/gradiolive/native_client.go`: conexión FRP 0.44 sobre TLS 1.2+, prefacio 0x17, yamux, login, proxy, cifrado/compresión, conexiones de trabajo, heartbeat, Kill como error (jamás os.Exit).
- `internal/frpmsg`: mensajes FRP de Hugging Face con derechos Apache 2.0 y atribución de procedencia.
- Dependencias Go fijadas en go.mod/go.sum: `github.com/fatedier/golib v0.1.1-0.20220321042308-c306138b83ac` y `github.com/hashicorp/yamux v0.1.1`; compresión snappy transitiva fijada. No requiere paquete Python Gradio.
- Se mantiene `MCP_TUNNEL=auto` priorizando Gradio y usando Cloudflare al agotar reintentos. El challenge HTTP público de KagMCP comprueba la URL antes de habilitar OAuth. Los modos explícitos siguen estrictos.
- Sin descargas de ejecutables FRP, ahorrando cuota de almacenamiento Kaggle.

## Pruebas locales
Se añadió test de integración local contra servidor TLS+yAmux+FRP simulado que verifica prefacio, login 0.44, digest de autenticación, cifrado de control, NewProxy, URL anunciada y cierre de sesión. En ningún caso se llamó a Gradio público o se usó sesión Kaggle real.

## Riesgos pendientes
Realizar E2E en Kaggle/Gradio: verificar URL pública, /api/health, OAuth, login web, reconexión. Si Gradio falla, auto intentará Cloudflare; avisar si un túnel muere luego de publicarse. Protocolo copiado/adaptado de Goradio read-only, no actualiza Goradio.
