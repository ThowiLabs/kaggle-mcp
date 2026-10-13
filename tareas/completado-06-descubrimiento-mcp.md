# Tarea 06 — Corregir descubrimiento MCP de ChatGPT

**Estado: completado (pruebas locales)**
Fecha: 2026-10-09

## Síntoma
OAuth se conectaba correctamente pero ChatGPT devolvía «Authentication succeeded, action discovery failed».

## Diagnóstico
La versión publicada de KagMCP rechazaba `MCP-Protocol-Version: 2026-07-28` y devolvía método inexistente en `server/discover`. Por tanto había incompatibilidad objetiva con la etapa moderna de descubrimiento MCP, sin que ello pruebe la causa única del error externo de ChatGPT.

## Correcciones
- `server/discover` moderno con versiones, capacidades, metadatos y caché declarados.
- `tools/list` y `tools/call` admiten formato de respuesta `2026-07-28`.
- Validaciones consistentes de `MCP-Protocol-Version`, `Mcp-Method`, `Mcp-Name` y `params._meta` para nuevas solicitudes.
- Compatibilidad original de `initialize` y `tools/list` del protocolo 2025.
- OAuth/PIN siguen siendo obligatorios antes de descubrir herramientas.
- Logs acotados de descubrimiento: nombre del método, versión, sin credenciales.

## Pruebas de regresión
`TestOAuthThenActionDiscoveryModern`: DCR, OAuth con PIN/PKCE, token, server/discover, tools/list y tools/call.
`TestLegacyDiscoveryPreserved`: initialize y tools/list legado.
`TestModernRejectsHeaderMismatchAndNoBearer` y `TestModernRequestHeaderAndMethodValidation`: acceso y metadatos.
Se verifican además tests generales, race, vet, notebook, build Linux y git diff --check.

## Limitaciones
No se ha repetido la conexión contra ChatGPT/Kaggle reales; se necesita recompilar y volver a autorizar desde la URL actual.
Ver `contexto/07-descubrimiento-mcp-2026.md`.
