# Actualización del panel KagMCP · 2026-10-11

## Solicitud
Arreglar porcentajes ficticios de anillos; usar componente monitor original Axolotl; añadir CPU/RAM y almacenamiento en el dashboard; navegación multi-página; sesión web persistente.

## Diagnóstico
axolotl.css conserva .ring con 68% de ejemplo y !important que sobrescribía nuestro gradiente. La plantilla server-metrics.tmpl original tiene datos ficticios (CPU 42%, RAM 68%, discos C:/D:), por lo que solo se conservan estructura/velocímetros/barras, no valores ni simulación. El panel anterior tenía una sola ruta y cookies de 8 h sin sesiones duraderas.

## Implementación
- HTML renderizado desde go:embed con secciones condicionales por .Page y rutas independientes.
- Monitor Axolotl CPU/RAM con SVG semicircular, GPU donut independiente y almacenamiento barra, fuentes reales cgroup-v2/proc + nvidia-smi + statfs.
- Sin lectura disponible: N/D y sin indicador vivo verde.
- Backend runtime CPU muestreado como delta real 180 ms inicial y actualizaciones a 5 s; cgroup como prioridad, host fallback etiquetado. RAM cgroup con fallback host etiquetado. No hay sensores de temperatura/frecuencia CPU si no se obtienen datos: se evita inventarlos.
- Sesiones web 7 días con cookie Secure/HttpOnly/SameSite Strict y hash SHA256 aleatorio almacenado 0600 en /kaggle/working/.kagmcp/state/web-sessions.json; sobreviven reinicio del proceso en mismo dominio, no a dominios cambiados automáticamente.
- Logout y PIN change revocan sesiones persistidas.
- Tests Go de rutas, auth, persistencia, revocación, sintaxis JS, build cruzado Linux.
- Browser headless Chrome local con preview y recursos originales, pero API no accesible fuera de Kaggle (sin telemetría real).

## Pendiente
Comprobar autenticación y métricas en una instancia Kaggle real con GPU, cgroup y volumen; abrir panel con la misma URL antes/después de reinicio del servidor. Si el túnel rota dominio, se requiere login nuevo por cookies segregadas por origen.

## Comandos
`go test ./...`; `go vet ./...`; `node --check internal/dashboard/ui/dashboard.js`; `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/kagssh`.
