# Kaggle MCP — Model Context Protocol para Kaggle

Kaggle MCP es un servidor **MCP no oficial** para conectar ChatGPT y otros clientes compatibles con una sesión autorizada de Kaggle. Su núcleo es un binario Go para Linux, con transporte HTTPS, OAuth, PIN de administración y panel web. No requiere GPU.

## Funcionalidades principales

- Herramientas MCP para archivos, operaciones autorizadas del runtime, gestión de proyectos, memoria, tareas e historial de actividad.
- GitHub mediante token personal, integración opcional de Hugging Face y administración de Skills.
- Detección y telemetría de GPU, CPU, memoria y disco; dashboard protegido con PIN.
- Orquestación de tareas entre agentes bajo consentimiento humano y revisión explícita.
- Enlaces temporales de descarga de archivos y publicación de apps HTTP locales mediante Gradio.
- Acceso público al servidor MCP mediante Gradio, con respaldo Cloudflare; también permite proxy HTTPS propio.

## Dominio fijo con Caddy u otro proxy inverso

Kaggle MCP puede usar un **dominio HTTPS propio sin agregar variables al notebook**, aunque el transporte sea `MCP_TUNNEL="auto"` o `"gradio"`. El túnel funciona como conexión de salida; la dirección pública OAuth se obtiene automáticamente del encabezado `X-Forwarded-Host` que envían los proxies inversos habituales (o de `Host` cuando la conexión HTTPS llega directamente al servidor). La URL por defecto del túnel sigue funcionando cuando no existe proxy.

Por ejemplo, si el usuario accede mediante `https://kg.mc365.eu.org/mcp` y Caddy envía `X-Forwarded-Host: kg.mc365.eu.org`, los endpoints `/.well-known/oauth-protected-resource` y `/.well-known/oauth-authorization-server` anunciarán ese mismo dominio para `resource`, `authorization_servers`, `issuer`, `authorization_endpoint`, `token_endpoint` y `registration_endpoint`; el reto `WWW-Authenticate` también lo utiliza. La autorización con PIN, el canje OAuth y la validación del token utilizan el mismo recurso, evitando que las URL de Gradio se filtren en la configuración OAuth del cliente MCP.

Al renovar la URL temporal de Gradio, las autorizaciones emitidas mediante el dominio fijo se conservan y permanecen restringidas a su recurso original. Los clientes conectados directamente a `*.gradio.live` mantienen su comportamiento anterior y necesitarán reconectarse si cambia su enlace. **El proxy debe preservar `X-Forwarded-Host` hasta Kaggle MCP; si un intermediario lo elimina y también reescribe `Host`, el servidor no puede deducir el dominio original.** La rotación del túnel tampoco modifica automáticamente el destino configurado en Caddy.

No requiere `MCP_PUBLIC_URL`, banderas adicionales, middleware externo, cambios en el notebook ni nuevas dependencias. Para probarlo: `go test ./internal/mcp -run TestOAuthAutomaticallyUsesReverseProxyDomain -v`.

La herramienta MCP `exec` puede ejecutar operaciones autorizadas en el runtime. Los clientes MCP autorizados actúan con permisos del proceso de Kaggle: concede acceso solo a clientes de confianza.

## Uso rápido en Kaggle

1. Abre [el notebook del proyecto](notebooks/kaggle_mcp.ipynb) en Kaggle y activa Internet.
2. Ejecuta las celdas para clonar/actualizar, instalar la versión adecuada de Go y compilar.
3. Configura `MCP_ENABLED=True`, `MCP_TUNNEL="auto"` y `pin=""` para que Go genere un PIN temporal.
4. Ejecuta las celdas de validación e inicio. Copia el enlace HTTPS terminado en `/mcp` a tu cliente y sigue el flujo de autorización.

No se necesitan Kaggle Secrets para ejecutar el servidor. Los tokens de GitHub y Hugging Face, si se utilizan, se introducen en memoria con `getpass` o desde el panel protegido, nunca en el repositorio.

## Configuración

| Variable | Valor habitual | Descripción |
|---|---|---|
| `MCP_ENABLED` | `true` | Activa el servidor MCP |
| `MCP_TUNNEL` | `auto` | `auto`, `gradio`, `cloudflare` o `none` |
| `MCP_GRADIO_RETRIES` | `3` | Reintentos de Gradio (1–5) |
| `MCP_LISTEN_PORT` | `8181` | Puerto HTTP local |
| `MCP_PUBLIC_URL` | vacío | Origen HTTPS del proxy propio para `none` |
| `MCP_ACCESS_PIN` | vacío | PIN temporal generado al iniciar o PIN propio |
| `KAGMCP_DATA_DIR` | `/kaggle/working/.kagmcp` | Estado privado |
| `GITHUB_TOKEN`, `HF_TOKEN` | vacío | Integraciones opcionales |

Configura las variables en la celda Python; no publiques tokens ni salidas con PIN. `MCP_TUNNEL=auto` prioriza Gradio y utiliza Cloudflare como respaldo. El servidor HTTP escucha solo en loopback.

## Compilación y pruebas

Desde la raíz del repositorio:

```bash
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -o kagmcp-linux-amd64 ./cmd/kagmcp
```

El comando principal está en `./cmd/kagmcp`. Se mantiene `./cmd/kagssh` únicamente como enlace de compatibilidad para compilaciones antiguas y workflows existentes. Los ejecutables `kagmcp-*` y las variables `KAGMCP_*` no cambian.

Consulta [USO_Y_CONFIGURACION.md](USO_Y_CONFIGURACION.md) para instrucciones detalladas, [GITHUB_CAPACIDADES.md](GITHUB_CAPACIDADES.md) para las herramientas GitHub, [AGENT_ORCHESTRATOR.md](AGENT_ORCHESTRATOR.md) para delegación, y [RELEASES.md](RELEASES.md) para distribución.

## Seguridad

- OAuth, PIN, origen HTTPS y autorización son controles esenciales, no opcionales.
- `/kaggle/input` es solo lectura; `/kaggle/working` contiene archivos modificables.
- El runtime es compartido: las herramientas concedidas tienen los permisos del proceso y deben usarse con cautela.
- Revisa los permisos del PAT y revócalo si se filtra. No compartas el PIN ni las URLs de descarga temporal.
- Kaggle MCP es un proyecto independiente, no oficial de Kaggle, OpenAI, Gradio o GitHub.

Repositorio: [ThowiLabs/kaggle-mcp](https://github.com/ThowiLabs/kaggle-mcp).
