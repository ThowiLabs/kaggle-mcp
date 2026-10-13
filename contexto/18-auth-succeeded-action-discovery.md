# Contexto 18 — Reparación de action discovery tras OAuth

Fecha: 2026-10-09
Proyecto exclusivo: `C:\Users\Admin\Documents\GitHub\kagssh-go`.

## Reporte
El usuario volvió a obtener `Authentication succeeded, action discovery failed`. En Kaggle vio:
- `INFO login recibido a través de proxy con Origin diferente origin_present=true` a las 19:16:11
- Igual mensaje a las 19:16:37.

Estos logs del formulario web del panel no son una falla del PIN. OAuth podía tener éxito sin que herramientas MCP fueran descubiertas.

## Diagnóstico (evidencia de código, no captura HTTP del usuario)
`ServeHTTP` en `internal/mcp/server.go` rechazaba POST /mcp HTTP 403 cuando `Origin` estaba presente y era diferente a `s.public`. El cliente de ChatGPT puede tener un origen distinto de la URL pública gradio.live, provocando posible fallo de `server/discover` y `tools/list`, aun después de OAuth correcto. Las pruebas previas no modelaban ese Origin; ahora sí.

## Corrección de seguridad y conformidad MCP
Con base en la especificación oficial MCP 2026-07-28, **se debe validar Origin** para impedir DNS rebinding. No deshabilitar sin restricciones.
- Permitir Origin omitido, la URL pública del servidor y únicamente `https://chatgpt.com`, `https://chat.openai.com`, `https://www.chatgpt.com`.
- Rechazar Origin malformado, null, dominios impostores y arbitrarios con HTTP 403, incluso si tienen Bearer.
- Mantener OAuth Bearer por solicitud, POST application/json, MCP method/headers/meta y PIN web protegido independientemente.
- Añadir logs diferenciados para rechazo por Origin, bearer, content_type, versión, metadata y Mcp-Method. Solo status/reason sin tokens ni raw Origin.
- Respuesta `UnsupportedProtocolVersionError` incluye `data.supported` y `data.requested` para negociación MCP.

## Verificaciones
- Test de Origin permitido/denegado, incluso con Bearer correcto y sin Bearer.
- OAuth PKCE real simulado seguido por /mcp a través de un reverse proxy httptest con Origin chatgpt.com y MCP 2026-07-28 + 2025-11-25.
- `server/discover`, `tools/list`, herramientas y schemas JSON; error moderno de negociación incluye supported/requested.
- Go tests, race, vet, check notebook, binario Linux previstos antes del push.

## Límites y próximos pasos
No se inspeccionó la llamada HTTP real de ChatGPT ni se probó Gradio/Kaggle real; la causa es probable pero no definitiva. Si persiste, pedir únicamente líneas de logs `MCP descubrimiento rechazado reason=...` o `MCP respuesta de descubrimiento`, **nunca tokens ni PIN**. Verificar nueva URL pública actual al reiniciar Kaggle.
