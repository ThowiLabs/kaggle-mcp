# Contexto 14 — Login del panel corregido tras cambio a Gradio

Fecha: 2026-10-09
Workspace modificado exclusivamente: `C:\Users\Admin\Documents\GitHub\kagssh-go`.
No alterar `Goradio` o `Lilith-MCP`.

## Incidencia
El usuario indicó que al introducir el PIN en `https://...gradio.live/login` recibía el texto **«solicitud inválida»** sin detalles.

## Causa más probable, identificada en código
`internal/dashboard/dashboard.go` devolvía 403 antes de comparar el PIN cuando `r.Header.Get("Origin")!=h.Public`. Un proxy Gradio FRP/Cloudflare puede modificar/omitir Origin, por lo que el control estricto de ese encabezado no puede considerarse prueba de autenticación de la sesión. Otra fuente del texto era error al parsear formulario.

## Corrección
Se eliminó la dependencia de `Origin` para login y acciones administrativas. **Se mantienen CSRF y sesiones autenticadas**: el login verifica cookie aleatoria privada `__Host-kagmcp-login` y token CSRF del formulario, con cookie `Secure`, `HttpOnly`, `SameSite=Strict`. Las acciones autenticadas siguen verificando cookie de sesión y CSRF aleatorio específico de esa sesión.

La interfaz HTML ahora presenta explicaciones explícitas para falta de cookie/CSRF, formato de formulario erróneo y PIN incorrecto, y deja volver a intentar con CSRF nuevo. Los logs registran solamente indicadores de error y presencia de Origin/cookie, sin PIN ni CSRF ni URL sensible.

## Verificaciones
Pruebas `internal/dashboard/security_test.go` cubren login con Origin ausente, `null`, `gradio.live`, Origin arbitrario pero CSRF válido; niegan las mismas peticiones con cookie ausente o token falso; comprueban que formularios autenticados siguen requiriendo CSRF; prueban error de formulario demasiado grande. Suite general Go, race, vet, notebook y Linux build pendientes de cierre final.

## Riesgos y límites
CSRF de doble presentación cookie/token evita solicitudes forjadas porque una web externa no puede conocer el token aleatorio emitido desde KagMCP y las cookies SameSite=Strict no viajan en solicitudes cross-site. No confundir una cabecera Origin con autorización. Si Gradio bloquea también cookies, el nuevo mensaje lo advertirá y habrá que abrir la URL directamente. No se ha observado la solicitud HTTP real del usuario, por lo que la causa específica del proxy es una hipótesis apoyada por la ruta de código. La validación final en Kaggle sigue pendiente.
