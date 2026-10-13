# Contexto 13 — Túnel Gradio FRP prioritario y Cloudflare automático

Fecha: 2026-10-09
Workspace exclusivo: `C:\Users\Admin\Documents\GitHub\kagssh-go`.
No tocar el repositorio Goradio/Lilith ni otros agentes.

## Intención y decisiones

El usuario aportó un ejemplo funcional de Gradio FRP v0.3 en Go sin depender de Python y pidió hacer Gradio el túnel predeterminado con reintentos y respaldo Cloudflare, salvo si se elige expresamente un proveedor.

- Por defecto `MCP_TUNNEL=auto` inicia con Gradio FRP, hasta `MCP_GRADIO_RETRIES=3` (1–5 editable), y luego Cloudflare en automático.
- `gradio`, `cloudflare` y `none` son **modos estrictos**. Si el usuario elige explícitamente uno, no cambiar de proveedor.
- FRP oficial v0.3, SHA-256 publicados en `gradio/gradio/tunneling.py`; incluido `frpc_linux_amd64=c791d1f047b41ff5885772fc4bf20b797c6059bbd82abb9e31de15e55d6a57c4`. Caché verificada en `/kaggle/working/.kagmcp/cache/kagmcp/frp-v0.3`. Librería estándar Go, TLS con CA de la API `https://api.gradio.app/v3/tunnel-request`, sin instalar Gradio en Python.
- Se procesa `start proxy success: https://*.gradio.live` con validación de dominio HTTPS. FRP cierra su proceso y borra el certificado temporal cuando la operación falla o se detiene.
- **Importante:** el servidor HTTP provisional se inicia *antes* de arrancar los túneles, atendiendo solo `GET /api/health` con un desafío aleatorio propio del proceso. La URL pública debe responder el mismo desafío exacto para ser considerada usable. Antes de salud satisfactoria se rechazan otras rutas HTTP con 503; solo entonces se crea el servidor MCP/OAuth y el panel de gestión y se anuncian sus URLs.
- Un enlace emitido por FRP pero que no responde al health no cuenta como éxito. Tras agotar reintentos se cierra FRP y se intenta Cloudflare; este también se somete al health.
- La URL anunciada de Gradio no es una interfaz Gradio Python; es un túnel HTTP que publica al servidor Go. El requerimiento de crear UI Gradio para proyectos verificados sigue siendo independiente.
- Una caída del túnel luego de que comenzó OAuth **no conmuta silenciosamente** a otra URL; por seguridad, el cliente debe reconectar si se reinicia.
- La opción `none` exige `MCP_PUBLIC_URL` HTTPS y una reverse-proxy propia.
- Checksum FRP está fijado y no se autoactualiza. Las fuentes oficiales de Gradio confirman v0.3, endpoint y checksums. La URL de share es temporal y su disponibilidad real depende de Kaggle/red del proveedor.

## Verificaciones

- Tests del parser de URL y de la tabla de SHA, plataforma, validación de host.
- Tests del selector `auto` con tres fallos de salud Gradio, cierre de cada proceso falso, un solo respaldo Cloudflare; proveedor explícito sin respaldo, Gradio saludable sin iniciar Cloudflare.
- Tests del handler health nonce y bloqueo de rutas previo al startup; reject de respuestas falsas.
- Tests de configuración default `auto/3` y máximo cinco reintentos.
- Suite general Go, race, vet, validador notebook, build Linux amd64 requeridos antes de push.

## Límites

El mecanismo se simuló y validó localmente; **no se ha hecho un E2E real de Kaggle/Gradio/ChatGPT**. Si FRP no está disponible o la URL pública no responde, se usa Cloudflare en modo auto. El enlace puede fallar después de arrancar, lo que requiere reconexión del cliente y no fallback automático de OAuth en vivo.
