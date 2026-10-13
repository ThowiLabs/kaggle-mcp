# Actividad paginada y sidebar estable · 2026-10-11

## Diagnóstico
El historial se rendereaba con todos los eventos del snapshot solo al cargar la ruta /activity, sin actualizaciones, navegación ni orden garantizado. Axolotl original contrae iconos HTML i, pero en KagMCP eran span.nav-icon y el CSS ocultaba TODOS los span: menú colapsado sin iconos, con etiquetas del brand, secciones y pie desbordadas.

## Implementación
- projectstate.HistoryPage(page) expone solo 20 eventos y metadatos, ordena por timestamp descendente (empates por orden de inserción inverso); máximo 1000 en Store.Record, ya existente.
- GET /api/activity?page=N autenticado, sin cache, solicitud validada y página acotada, método solo GET.
- SSR de /activity?page=N con 20 registros y navegación usable sin JS.
- Script activity.js separado, polling cada 5 s visible, refresh al recuperar foco, navegación pushState/popstate y enlaces accesibles. Actualiza nodos con textContent para evitar XSS.
- Sidebar conserva nav-icon, oculta solo texto, cabecera y leyendas al plegar; flecha accesible; preferencia localStorage tolerante a bloqueo. Drawer móvil aislado de modo plegado, con cerrar por botón visible, fondo y Escape. aria-expanded y control de foco.
- Las otras páginas no afirman tener telemetría en vivo cuando no monitorizan.

## Evidencia y verificaciones
- Tests projectstate/page y dashboard API/SSR/CSRF/acceso/XSS, go test ./..., go vet, JS node check, build GOOS=linux amd64.
- Chrome headless capturas desktop expandido, colapsado, historial y móvil. Vista de histórico sin datos remotos marca correctamente desconectado.
- Real-time aquí = polling hasta 5 segundos, NO push de eventos instantáneo.
- Riesgos comerciales pendientes: no se ha validado Kaggle de extremo a extremo; máximo 1000 registros en archivo JSON, no almacenamiento de auditoría con política contractual, sin multi-tenant ni SLA definidos.
