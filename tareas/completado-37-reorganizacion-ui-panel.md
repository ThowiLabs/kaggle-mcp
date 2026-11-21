# Tarea 37: reorganización de la UI del panel (tarjetas + ventanas como thowilabs-mcp)

- [x] Rama tarea-37-reorganizacion-ui DESDE tarea-36-tunel-ssh-inverso (commit e31da9e), sin mergear la 36.
- [x] Integraciones como rejilla de tarjetas (GitHub, Hugging Face, Túnel SSH), cada una con icono, nombre, descripción y estado; cada tarjeta abre su propia ventana con el formulario de ESA integración dentro.
- [x] Mismo patrón tarjeta+ventana en Seguridad (cambiar PIN) y Proyectos (nuevo proyecto), donde tenga sentido.
- [x] Mantener el diseño visual Axolotl: cambia la estructura, no el diseño.
- [x] Sin romper nada: mismos endpoints POST, mismos nombres de campo, misma sesión y CSRF.
- [x] Tests de UI para la rejilla y las ventanas (Integraciones, Seguridad, Proyectos).
- [x] El cuaderno notebooks/kaggle_mcp.ipynb no cambia nada.
- [x] `gofmt`, `go vet ./...`, `go test ./internal/dashboard/` y `./internal/sshtunnel/` en verde, `git diff --check` limpio, `node --check` del JS.
- [x] Documentar en `contexto/37-reorganizacion-ui-panel.md`; un solo commit en español en la rama `tarea-37-reorganizacion-ui`, sin merge ni push.
- [x] Reiniciar el panel de prueba con el mismo entorno y anotar la nueva URL.
