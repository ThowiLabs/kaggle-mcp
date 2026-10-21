# OAuth dinámico por dominio público del proxy (2026-10-21)

- Solicitud del usuario: mantener un dominio fijo como kg.mc365.eu.org cuando el enlace Gradio del cuaderno cambia, sin variables adicionales ni cambios en Caddy Proxy Manager. Trabajar solo en ThowiLabs/kaggle-mcp.
- Problema: internal/oauth/server.go generaba recursos y JSON `issuer`, `authorization_servers`, `authorization_endpoint`, `registration_endpoint`, `token_endpoint` con `s.publicURL` temporal. Cambiar solo cabeceras Location en Caddy no afectaba metadatos JSON.
- Solución: `oauth.Server.ForRequest(r)` clona los datos inmutables del servidor por petición usando `X-Forwarded-Host` o `Host` con TLS, compartiendo el mismo Store, sesiones, limitador y tokens. Cada ruta OAuth usa el clon. El endpoint `/mcp` emplea el mismo origen para desafío y validación de bearer; no se agregan variables, ajustes en notebook ni dependencias.
- La persistencia de OAuth distingue tokens emitidos desde el host proxied (`proxy_origin` opcional), preservándolos cuando el túnel temporal rota, sin dar acceso a otros recursos. Los tokens de conexión directa mantienen su comportamiento de antes.
- Cobertura: `internal/mcp/proxy_domain_test.go` comprueba metadatos y desafío, registro dinámico, autorización con PIN y PKCE, canje, acceso MCP, aislamiento por dominio y rotación Gradio manteniendo el dominio fijo. Las suites Go preexistentes se mantienen.
- Límite de infraestructura: Caddy u otro proxy debe transmitir `X-Forwarded-Host`, o la petición HTTPS debe conservar `Host`. Si Gradio elimina ambos antes de entregar a Go, no se puede recuperar el origen sin configurar el túnel/proxy. El usuario gestionará el upstream de Caddy por separado.

## Corrección de prueba FRP inestable en CI (2026-10-21)

- GitHub Actions fallaba ocasionalmente en `internal/gradiolive/native_protocol_test.go:TestNativeFRPControlTLSAndEncryptedMessages` con `session shutdown`.
- Se reprodujo con `go test ./internal/gradiolive -run '^TestNativeFRPControlTLSAndEncryptedMessages$' -count=80`.
- Causa: el cliente se cancelaba al recibir la URL antes de que el servidor FRP de pruebas acabara de escribir la respuesta cifrada (`iv` y mensaje requieren escrituras independientes de yamux). El cierre prematuro interrumpía el `WriteMsg` del simulador.
- Solución: sincronizar con canales `responseWritten` y `clientFinished`. El servidor de pruebas espera a que la sesión del cliente termine y el test espera que la escritura cifrada concluya antes de cancelar.
- Verificado con 500 ejecuciones, otras 60 con detector `-race`, `go test -timeout 120s ./...` y `go vet ./...`; todas correctas. Sin cambio al cliente FRP ni a las funciones OAuth. Integrar mediante amend del commit de dominio fijo (a solicitud del usuario).
