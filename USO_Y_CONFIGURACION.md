# Guía de uso de Kaggle MCP

## 1. Propósito

Kaggle MCP conecta una sesión de Kaggle con clientes Model Context Protocol. Ofrece herramientas y un panel web para administración, proyectos, métricas, archivos e integraciones, con autenticación OAuth y PIN. Se ejecuta en Linux como binario Go único.

## 2. Notebook de arranque

Abre `notebooks/kaggle_mcp.ipynb` en Kaggle. Ejecuta en este orden:

1. **Clonar:** obtiene la rama `main` y verifica el remoto.
2. **Instalar Go:** utiliza el runtime adecuado y valida la descarga cuando se necesita.
3. **Compilar:** construye el binario Linux.
4. **Configurar:** elige transporte HTTPS, puerto, PIN y credenciales opcionales.
5. **Validar:** ejecuta `-check` sin abrir el servicio.
6. **Iniciar:** ejecuta Kaggle MCP y expone la URL de `/mcp`.
7. **Conectar:** añade el enlace a ChatGPT u otro cliente MCP y autoriza el acceso.

El PIN temporal lo genera Go si `pin=""`; consulta su salida inicial de forma privada. Si especificas tu propio PIN, debe tener entre 6 y 128 caracteres. Las credenciales opcionales se piden mediante entrada oculta.

## 3. Configuración de la conexión

Valores recomendados en la celda de configuración:

```python
MCP_ENABLED = True
MCP_TUNNEL = "auto"
GRADIO_RETRIES = 3
MCP_PUBLIC_URL = ""
MCP_LISTEN_PORT = 8181
pin = ""
USE_GITHUB = False
USE_HUGGINGFACE = False
```

**Conexión mediante dominio fijo:** no hace falta definir `MCP_PUBLIC_URL` para usar `kg.mc365.eu.org` con el túnel Gradio o Cloudflare. Configura tu proxy inverso hacia la URL temporal del túnel y conecta el cliente MCP a `https://kg.mc365.eu.org/mcp`. El proxy debe conservar `X-Forwarded-Host` con el dominio original hasta Kaggle MCP; de esa forma los metadatos JSON y todo el flujo OAuth usan automáticamente el dominio consultado y no el subdominio temporal. Los tokens del dominio fijo sobreviven a la renovación del túnel, pero el upstream de Caddy debe actualizarse si el túnel cambia de dirección.

El túnel `auto` prueba primero Gradio y, cuando no está disponible, usa Cloudflare. `none` requiere configurar un origen `MCP_PUBLIC_URL` con HTTPS válido y proxy administrado por ti. Ninguna modalidad exige GPU.

## 4. Herramientas MCP

El conjunto incluye administración de archivos en las carpetas autorizadas de Kaggle, información del entorno, ejecución acotada de operaciones del runtime mediante la herramienta MCP `exec`, memoria y tareas por proyecto, Skills, telemetría GPU y GitHub con token personal. La interfaz administrativa web sirve para iniciar sesión, ver el dashboard, gestionar proyectos, actividad, integraciones y preferencias de seguridad.

La herramienta MCP `exec` requiere un cliente autorizado y está limitada en tiempo y volumen de salida; los procesos conservan los permisos del runtime.

## 5. GitHub y Hugging Face

Activa `USE_GITHUB` o `USE_HUGGINGFACE` para introducir los tokens mediante `getpass`. Utiliza los permisos mínimos necesarios. También puedes configurar las integraciones desde el panel con PIN; las credenciales no deben registrarse en `config.json` ni en el notebook publicado.

## 6. Seguridad y datos

- OAuth y PIN protegen los accesos; conserva privados los tokens y la salida que incluye el PIN temporal.
- El endpoint MCP opera en loopback detrás del transporte HTTPS seleccionado.
- `/kaggle/working/.kagmcp` es el directorio privado para configuración no sensible, autorización y caché.
- `/kaggle/input` es solo lectura, mientras `/kaggle/working` admite cambios.
- Los túneles de apps y URLs de descarga pueden dar acceso público a recursos puntuales: revisa cuidadosamente sus condiciones.
- Kaggle puede finalizar la sesión sin previo aviso; exporta el trabajo importante.

## 7. Desarrollo y verificación

```bash
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -o kagmcp-linux-amd64 ./cmd/kagmcp
```

El módulo Go y la ruta actual de GitHub conservan su identificador histórico para no romper importaciones y flujos de compilación.

## 8. Documentación adicional

Consulta `README.md`, `GITHUB_CAPACIDADES.md`, `AGENT_ORCHESTRATOR.md` y `RELEASES.md`. El historial operativo vigente del servidor se concentra en `contexto/`.
