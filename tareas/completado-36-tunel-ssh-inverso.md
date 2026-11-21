# Tarea 36: túnel SSH inverso aditivo con contraseña cifrada por PIN

- [x] Guardar la configuración SSH (usuario, host, puerto SSH, puerto remoto) en `ssh-tunnel.json` aparte de `config.json`, con solo la contraseña cifrada.
- [x] Cifrar la contraseña con el PIN: PBKDF2-HMAC-SHA256 (sal aleatoria, 600000 iteraciones) + AES-256-GCM; nunca SHA256(PIN) directo ni contraseña en claro.
- [x] Arrancar el túnel SSH inverso solo si está configurado, en segundo plano y sin tumbar el arranque; la URL temporal (Gradio auto → Cloudflare) se sigue generando como respaldo.
- [x] Verificar la clave del host SSH con huella fijada (TOFU); nunca `InsecureIgnoreHostKey`.
- [x] Health-check de extremo a extremo con el nonce de `/api/health` por el puerto reenviado; keepalive y reconexión con backoff.
- [x] Puerto remoto vacío: el servidor asigna uno y el panel muestra el asignado.
- [x] Panel en Integraciones: guardar (pide el PIN para cifrar), probar conexión, detener con confirmación, volver a iniciar; sin botón borrar.
- [x] Avisar en el panel y en el README: persistencia solo con PIN propio; cambiar el PIN invalida la configuración; el túnel acepta orígenes de cualquier lado (decisión del usuario, MCP temporal).
- [x] Tests unitarios de cifrado, validación, gestor con servidor SSH falso, integración en el servidor y rutas del panel.
- [x] `gofmt`, `go vet ./...`, `go test ./...` sin fallos; `git diff --check` limpio.
- [x] Documentar en `contexto/36-tunel-ssh-inverso.md` y en el README; un solo commit en español en la rama `tarea-36-tunel-ssh-inverso`, sin merge ni push.
