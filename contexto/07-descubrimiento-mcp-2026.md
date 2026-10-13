# Contexto 07 — Corregir «Authentication succeeded, action discovery failed»

Fecha: 2026-10-09

## Incidente reproducido y causa en el código

El usuario conectó su servidor KagMCP desde ChatGPT y completó OAuth con éxito, pero recibió «Authentication succeeded, action discovery failed». Antes de este cambio, el HTTP MCP solo anunciaba `2025-11-25`; rechazaba `MCP-Protocol-Version: 2026-07-28` con HTTP 400, y `server/discover` siempre respondía `Method not found`. El cliente de ChatGPT puede solicitar descubrimiento moderno `2026-07-28`. Este es un defecto de compatibilidad **confirmado en nuestro código**; el error exacto del lado de ChatGPT todavía no se verificó con captura de red y no se debe afirmar que es la única causa posible.

## Corrección de compatibilidad

- El mismo servidor HTTP y la misma autenticación OAuth/PIN aceptan `2026-07-28` (stateless), `2025-11-25` y `2025-03-26` (legacy).
- `server/discover` moderno responde `resultType: complete`, `supportedVersions`, `capabilities.tools`, `_meta.io.modelcontextprotocol/serverInfo`, `instructions`, `ttlMs` y `cacheScope: private`.
- `tools/list` moderno mantiene esquemas JSON Schema existentes e incluye `resultType`, `ttlMs` y `cacheScope`.
- `tools/call` moderno retorna `resultType: complete`.
- Verifica coincidencia `MCP-Protocol-Version` con `params._meta.io.modelcontextprotocol/protocolVersion`; cabeceras `Mcp-Method` y `Mcp-Name` cuando aplica. Devuelve HTTP 400, `-32020` ante header mismatch o `-32022` ante versión desconocida.
- Conservar `initialize` y solicitudes legadas; la autorización OAuth se comprueba **antes** de cualquier descubrimiento.
- Logs de descubrimiento muestran solo nombres de métodos conocidos y número de versión MCP; jamás tokens, PIN ni argumentos.

## Pruebas

- `TestOAuthThenActionDiscoveryModern`: ejecuta realmente DCR → consentimiento OAuth con PIN → canje de code + PKCE → petición autenticada de `server/discover` → `tools/list` → `tools/call(environment_info)`.
- `TestLegacyDiscoveryPreserved`: flujo de initialize y tools/list antiguos.
- `TestModernRejectsHeaderMismatchAndNoBearer` y `TestModernRequestHeaderAndMethodValidation`: bloqueo de llamadas sin autorización y validación de headers.

## Límites y siguiente paso

La compatibilidad MCP se verificó con `httptest` local y build Linux, **no** contra un runtime real Kaggle ni contra la infraestructura de ChatGPT. Para validar end-to-end, detener el proceso actual, actualizar el clon desde GitHub y recompilar el binario ejecutando las celdas `clonar`, `compilar`, `validacion`, `run` del notebook. Si la URL temporal Cloudflare cambia, actualizar la URL en ChatGPT y volver a autorizar. Revisar logs que muestren métodos de discovery y continuar diagnóstico si persiste el error.

## Referencias

- Especificación Model Context Protocol 2026-07-28: `server/discover`, transportes Streamable HTTP, cabeceras MCP y resultados cacheables.
- Se conserva el nombre remoto `ThowiLabs/kagssh-go` hasta autorización expresa para renombrar GitHub.
