# Contexto 12 — PIN opcional generado por Go y cambio desde panel web

Fecha: 2026-10-09
Repositorio exclusivo: `C:\Users\Admin\Documents\GitHub\kagssh-go`.
Runtime: `/kaggle/working`.

## Petición
El usuario quiere `pin = ""` editable en el notebook; si está vacío, que lo genere el **servidor Go**, nunca Python, y que admita PIN configurado de **6 a 128 caracteres** (no mínimo 12). Después de entrar a la web `/`, debe poder cambiar el PIN.

## Implementación
- `internal/config`: un PIN vacío permite `-check`; si se configura, longitud 6–128 sin saltos de línea/nulos.
- `internal/pinauth`: utiliza crypto/rand para 15 bytes (20 caracteres URL-safe, 120 bits de entropía), SHA256 + comparación constante, lectura protegida por mutex y rotación con validación y callback de revocación.
- `internal/mcp/server.go`: New() genera PIN Go si no hay PIN definido; `Run()` lo informa **una sola vez en los logs privados de Kaggle** con `pin_temporal`, antes de iniciar solicitudes externas; no persiste la clave en la configuración ni disco, ni requiere Kaggle Secrets.
- `internal/oauth/server.go`: autentica PIN contra el estado compartido y no conserva otra copia/hash independiente desincronizable. `ResetOwner` revoca owner, sesiones de autorización, access tokens, refresh tokens y codes.
- `internal/dashboard/dashboard.go`: formulario `/change-pin` tras login protegido por sesión, origin y CSRF. Solicita PIN actual y nuevo; aplica rate limiting, mínimo seis, no permite repetir el anterior, cierra todas las sesiones web y revoca OAuth anterior mediante el callback. Requiere que el agente reconecte con nuevo PIN.

## Seguridad y límites
El PIN de seis caracteres se acepta por compatibilidad con la preferencia explícita del usuario; se recomienda un PIN más largo. El PIN generado automáticamente tiene 20 caracteres aleatorios (120 bits). Solo se muestra en la salida de Kaggle `run` y esa salida debe mantenerse privada. Si `pin = ""`, **cada nuevo proceso Go** genera un nuevo PIN; un cambio web es temporal durante ese proceso, no se escribe en Secrets ni notebook. Si se desea un PIN estable tras reiniciar, definir uno en el cuaderno privado, con el riesgo documentado.

## Verificaciones
Se añaden tests de `internal/pinauth`, `internal/config`, `internal/dashboard` e integración `internal/mcp` con OAuth real. El test de integración obtiene token con PIN antiguo, rota mediante login web, comprueba que el token anterior falla con 401 y que la nueva autorización con PIN de seis caracteres funciona. Notebook simulado valida PIN vacío, PIN explícito y ausencia de Secrets extra. Go vet, race y compilación Linux amd64 pendientes de cierre final.

## Limitaciones
No se ha usado una sesión real de Kaggle/Cloudflare en estos tests locales, por lo que la reconexión real ChatGPT debe comprobarse con el usuario.
