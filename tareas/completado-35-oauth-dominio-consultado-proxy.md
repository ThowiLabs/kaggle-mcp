# Completo: OAuth basado en el dominio consultado

- [x] Mantener la configuración del notebook original, sin nuevas variables.
- [x] Derivar metadatos OAuth del host público del proxy.
- [x] Vincular tokens y audiencias al recurso del mismo dominio.
- [x] Mantener autorizaciones del dominio fijo tras cambiar el túnel temporal.
- [x] Probar flujo completo OAuth y continuidad durante reconexión.
- [x] Documentar límite del encabezado X-Forwarded-Host y compatibilidad con Caddy.

### Seguimiento: prueba FRP en GitHub Actions
- [x] Reproducir `session shutdown` de manera repetible.
- [x] Sincronizar la finalización de la escritura de respuesta cifrada del simulador FRP.
- [x] Mantener la sesión FRP simulada abierta hasta terminar el cliente.
- [x] Confirmar 500 ejecuciones normales, 60 con `-race`, suite completa y `go vet`.
- [x] Integrar en el mismo commit mediante `--amend`, sin commit adicional.
