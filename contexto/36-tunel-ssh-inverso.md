# Túnel SSH inverso aditivo con contraseña cifrada por PIN (2026-11-20)

- Fecha: 2026-11-20
- Objetivo: publicar el MCP de Kaggle a través de un servidor SSH propio del usuario, como vía adicional a la URL temporal (Gradio auto → Cloudflare). Aditivo: la URL temporal se sigue generando como respaldo y el túnel SSH solo arranca si está configurado.
- Tarea: tareas/completado-36-tunel-ssh-inverso.md

## Decisiones

- Aditivo, no sustitutivo. Al arrancar se sigue generando la URL temporal. Solo si existe configuración SSH guardada, ese túnel también se inicia solo al arrancar. Sin variables de entorno nuevas ni dominio obligatorio.
- Persistencia solo con PIN propio. `MCP_ACCESS_PIN` vacío hace que Go genere un PIN nuevo en cada arranque, así que una configuración cifrada con un PIN generado sería ilegible al reiniciar. El panel y el README lo advierten. Si el arranque usa PIN generado y existe configuración, el túnel no se inicia y se registra una advertencia.
- Cifrado: PBKDF2-HMAC-SHA256 (stdlib `crypto/pbkdf2`, disponible desde Go 1.24) con sal aleatoria de 16 bytes, 600000 iteraciones, clave de 32 bytes, AES-256-GCM con nonce aleatorio de 12 bytes. Se eligió PBKDF2 sobre Argon2id/scrypt porque está en el stdlib (menos dependencias), sus parámetros son los recomendados por OWASP para PBKDF2-HMAC-SHA256 (600000 iteraciones), y el coste lo paga solo el arranque/guardado, no cada petición. Nunca se usa SHA256(PIN) directo como clave: sin sal, dos instalaciones con el mismo PIN producirían la misma clave y un diccionario serviría para todas.
- Archivo aparte `ssh-tunnel.json` en DataDir (`KAGMCP_DATA_DIR`, por defecto `/kaggle/working/.kagmcp`), directorio 0700, archivo 0600, escritura atómica temp+rename como `SaveSettings`. `config.json` sigue siendo solo opciones no sensibles, nunca credenciales. Metadatos en claro (usuario, host, puertos, huella del host); solo la contraseña va cifrada (sal, nonce, ciphertext).
- Puerto remoto vacío = el servidor SSH asigna uno (`-R 0:127.0.0.1:<MCP_PORT>`); el puerto asignado se muestra en el estado del panel. El usuario puede fijar uno si su servidor lo exige.
- Huella del host (TOFU): en la primera conexión se guarda la huella SHA-256 de la clave del host; después se exige la misma. Nunca `InsecureIgnoreHostKey`. Cambiar de host o puerto SSH reinicia la confianza TOFU. Si la clave cambia, el túnel no conecta y el estado muestra "la clave del host SSH no coincide con la registrada".
- Fallo cerrado y arranque seguro: si el descifrado falla, el túnel no se inicia y el error es genérico ("no se pudo descifrar la configuración del túnel SSH"), sin distinguir PIN incorrecto de archivo manipulado. Si el túnel SSH falla o se cae, el arranque no falla: se registra una advertencia, se reintenta con backoff (1 s, duplicando hasta 16 s) y la URL temporal sigue funcionando.
- Orígenes: decisión explícita del usuario (2026-11-20 19:37). El túnel acepta conexiones de cualquier lado: es un MCP temporal, no un servidor 24/7. No se añade allowlist de origen/dominio para el túnel SSH. Riesgo aceptado y documentado en el README: la URL no es autenticación; el PIN y OAuth siguen siendo obligatorios.
- Sin botón borrar (regla del usuario: no borrar servidores SSH ni secretos). El panel permite reemplazar la configuración o deshabilitarla; detener pide confirmación (patrón `<details>` como cancelar delegación). Si el usuario cambia el PIN, el panel avisa de que la configuración SSH se invalida y habrá que volver a introducir la contraseña; el túnel que ya corre sigue activo hasta reiniciar, porque la contraseña ya está descifrada en memoria.
- Sin herramientas MCP nuevas. La contraseña solo se introduce en el panel (sesión + CSRF), nunca por MCP: una herramienta MCP que aceptara la contraseña la expondría en tráfico y registros de herramientas.
- El PIN para cifrar se pide en el propio formulario SSH ("PIN actual"): el panel no guarda el PIN en claro en ningún momento, solo su hash, así que no puede cifrar sin que el usuario lo vuelva a escribir. La prueba de conexión usa la contraseña del formulario sin guardar nada ni persistir huellas.

## Arquitectura

- Nuevo paquete `internal/sshtunnel`: `config.go` (Stored, validación, PBKDF2+AES-256-GCM, Load/Save atómico) y `manager.go` (Manager: descifrado en memoria, bucle de reconexión, `ssh.Dial` vía `net.Dialer` + `ssh.NewClientConn`, `client.Listen("tcp", "0.0.0.0:<puerto>")`, keepalive `keepalive@openssh.com` cada 15 s, proxy bidireccional a `127.0.0.1:<MCP_PORT>`, health-check de extremo a extremo con el nonce de `/api/health` a través del puerto reenviado).
- `internal/mcp/sshtunnel.go`: `attachSSHTunnel` (llamado en `Run` tras `New`, antes de exponer el panel; nunca tumba el arranque), `configureSSH` (verifica el PIN y delega), `testSSH`, `startSSH`, `stopSSH`. El gestor vive en `Server.ssh` y `rebuildMCPForPublic` lo conserva al rotar el túnel público: el túnel SSH es independiente de Gradio/Cloudflare y sobrevive a la reconexión.
- `internal/dashboard/dashboard.go` + `ui/page.html`: sección en Integraciones (guardar con PIN, probar con `formaction`, detener con confirmación, iniciar), estado en la misma página, aviso de PIN generado y aviso en Seguridad sobre el cambio de PIN. Sin cambios en `dashboard.js`.
- `go.mod`: `golang.org/x/crypto` pasa de indirecta a directa (necesaria para `x/crypto/ssh`; el stdlib de Go no tiene cliente SSH). Sin más dependencias nuevas. `go.sum` no cambia: ya contenía v0.57.0.

## Librerías

- Stdlib: `crypto/pbkdf2` (KDF), `crypto/aes` + `crypto/cipher` (AES-256-GCM), `crypto/sha256`, `net`, `net/http`.
- `golang.org/x/crypto v0.57.0` (directa): `ssh` (cliente, reenvío remoto, TOFU).

## Archivos

- Creados: `internal/sshtunnel/config.go`, `internal/sshtunnel/config_test.go`, `internal/sshtunnel/manager.go`, `internal/sshtunnel/manager_test.go`, `internal/mcp/sshtunnel.go`, `internal/mcp/sshtunnel_integration_test.go`, `internal/dashboard/sshtunnel_test.go`, `contexto/36-tunel-ssh-inverso.md`, `tareas/completado-36-tunel-ssh-inverso.md`.
- Modificados: `internal/mcp/server.go` (campo `ssh`, `attachSSHTunnel` en `Run`, conservar en `rebuildMCPForPublic`, cableado del panel), `internal/dashboard/dashboard.go` (rutas `/ssh-tunnel`, `/ssh-tunnel-test`, `/ssh-tunnel-start`, `/ssh-tunnel-stop`), `internal/dashboard/ui/page.html` (sección SSH en Integraciones, aviso en Seguridad), `go.mod` (x/crypto directa), `README.md` (sección de túnel SSH).

## Problemas

- Red del sandbox caída durante la implementación (DNS sin resolver, MCP con 502 intermitente): el clon se reintentó con un bucle en segundo plano hasta que la red volvió.
- La caché de módulos del sandbox no tenía `x/crypto v0.57.0` (solo v0.48.0/v0.54.0); la toolchain Go 1.26.11 sí estaba en caché y satisface la directiva `go 1.26.9`. Al volver la red, `go vet` y `go test` compilaron con v0.57.0 sin pasos manuales.

## Soluciones

- La red volvió y `go vet`/`go test` corrieron con `x/crypto v0.57.0` sin descarga manual (ya estaba en `go.sum` y disponible en la caché tras el reintento).
- Bloqueo real hallado al verificar (2026-11-20 22:45; el trabajo anterior lo marcaba como verde sin haber corrido la suite hasta el final): `TestManagerEndToEndThroughTunnel` colgaba para siempre. Causa doble en `internal/sshtunnel/manager.go`: (1) `connectOnce` corría `checkThroughTunnel` ANTES del bucle de `Accept`, así que la conexión de salud nunca se proxeaba al MCP local y el chequeo siempre agotaba sus 9 s; (2) `connected=true` se marcaba antes del health-check, así que el test (y el panel) creían que el túnel estaba activo. Al fallar el chequeo, `endSession` cerraba el listener con el canal de reenvío lleno y se bloqueaba contra el mutex de reenvíos de `x/crypto/ssh` (deadlock con `handleChannels`): el gestor quedaba atascado y el bucle de reconexión nunca reintentaba. El mismo patrón existía en `Manager.Test` (sin Accept durante el chequeo). Arreglo: el bucle de `Accept` arranca en goroutine ANTES del health-check en `connectOnce` y en `Test`; `connected` solo se marca si el health-check pasa (y el contexto sigue vivo). En los tests, `http.Get` sin tiempo límite se cambió por un cliente con 10 s, y `TestManagerTestMethod` ya no vuelve a pedir por el puerto tras `Test` (ese método verifica la salud y cierra el reenvío al devolver). Tras el arreglo, `go test ./internal/sshtunnel/` pasa en ~11 s.

## Tests

- `internal/sshtunnel/config_test.go`: round-trip cifrado/descifrado, PIN incorrecto, ciphertext manipulado, mismo error genérico en ambos casos, archivo guardado sin contraseña ni PIN en claro, permisos 0600, validación de host/puertos, sal/nonce distintos por guardado.
- `internal/sshtunnel/manager_test.go`: servidor SSH falso en el propio test (contraseña + `tcpip-forward` + `forwarded-tcpip`): extremo a extremo por el túnel con el nonce de `/api/health`, contraseña incorrecta sin conexión, cambio de clave de host rechazado (TOFU), método `Test` con puerto asignado, arranque sin configuración, deshabilitar/habilitar.
- `internal/mcp/sshtunnel_integration_test.go`: arranque sin configuración no rompe el servidor, configuración inválida no lo tumba, configurar exige PIN y no deja la contraseña en claro, PIN generado no descifra, la reconexión del túnel público conserva el gestor SSH.
- `internal/dashboard/sshtunnel_test.go`: rutas exigen sesión, guardar/probar/detener desde el panel, detener sin confirmar se rechaza, PIN incorrecto se rechaza.
- Verificación 2026-11-20 22:45-23:00 (sandbox android-build): `gofmt` limpio en los archivos de la tarea (`internal/config/settings_test.go` sale en `gofmt -l` pero es preexistente y no lo toca esta tarea); `go vet ./...` sin errores; `git diff --check` limpio; `go test ./internal/sshtunnel/` PASS (~11 s), `./internal/dashboard/` PASS; resto de paquetes PASS. `go test ./internal/mcp/` falla solo en 2 tests preexistentes sin relación con SSH: `TestCredentialBridgePrivateSocketAndGitHelper` (falla también en `main` limpio, sin los cambios) y `TestRealReverseProxyActionDiscoveryWithChatGPTOrigin` (flaky: pasa 3/3 aislado en la rama, falla una vez en la corrida completa del paquete).

## Seguridad

- Qué se cifra: solo la contraseña SSH, con PBKDF2-HMAC-SHA256 (sal 16 B, 600000 iteraciones) + AES-256-GCM (nonce 12 B). La clave derivada solo existe en memoria durante cifrado/descifrado.
- Dónde vive la clave: en ningún disco. El PIN no se guarda en disco (solo su hash SHA-256 en `pinauth.State`); la contraseña descifrada solo vive en memoria del proceso mientras corre.
- Qué nunca se registra ni se devuelve: la contraseña y el PIN. Los logs del túnel registran host y puerto, nunca credenciales. El estado del panel y las respuestas no incluyen la contraseña. El error de descifrado es genérico, sin oráculo. `definitions()` no expone herramientas nuevas ni secretos.
- Host-key: TOFU con huella SHA-256 guardada; nunca `InsecureIgnoreHostKey`; cambio de clave = sin conexión.
- Fallback: si el túnel SSH falla, el servidor sigue con la URL temporal; si el descifrado falla, no se inicia el túnel.
- Orígenes: sin allowlist por decisión explícita del usuario; la URL no es autenticación, PIN + OAuth siguen siendo obligatorios.
- El archivo `ssh-tunnel.json` vive en DataDir (0700/0600) y nunca se expone por las herramientas MCP de archivos (el directorio privado `.kagmcp` está excluido en `Server.path`).

## Pendientes

- Validación real en Kaggle (requiere al usuario): arrancar el cuaderno con PIN propio, configurar el túnel desde el panel, comprobar que la URL por SSH responde `/mcp` y que tras detener/iniciar el cuaderno la configuración persiste. Anotado también en pendiente-20: Kaggle debe conservar `/kaggle/working` al detener/iniciar el cuaderno.
- Probar contra un servidor SSH real con puerto remoto fijo y con puerto 0 (asignado por el servidor), y con un servidor cuya clave cambie, para confirmar el rechazo TOFU fuera de los tests.
- Si el usuario cambia el PIN desde el panel, el túnel en curso sigue activo hasta reiniciar; valorar en una tarea futura detenerlo al cambiar el PIN para que el aviso sea inmediato en vez de solo al reiniciar.
