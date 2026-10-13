
Fecha: 2026-10-09
Repositorio y **único workspace modificado**: `C:\Users\Admin\Documents\GitHub\kagssh-go`.
Runtime Kaggle: `/kaggle/working`; clone `/kaggle/working/kagssh-go`.
No se altera la carpeta `C:\Users\Admin\Documents\GitHub\Lilith-MCP` ni proyectos de otros agentes.

## Motivación


## Diseño implementado

El notebook tiene la celda nueva **configurar**, anterior a validación, con:

- Solicitud privada `getpass.getpass` del `MCP_ACCESS_PIN` (12–128 caracteres), en memoria. `USE_GITHUB=True` pide token personal PAT por getpass, no Secret.
- Las celdas `validacion` y `run` pasan **la misma copia** mediante `env=RUNTIME_ENV`, evitando que Go descubra el token de Secrets y vuelva a consultar etiquetas.
- El PIN y el PAT no se persisten en Git/notebook ni config.json. El entorno en memoria y procesos del mismo runtime no constituyen aislamiento frente a un adversario con acceso root.
- Reutiliza exactamente el protocolo/OAuth Go y código de carga legacy; ningún cambio del módulo Lilith-MCP.

## Verificaciones


## Actualización en Kaggle

**Importante:** ejecutar celda `clonar` descarga cambios Go, pero NO edita el notebook ya abierto. Para tener la celda `configurar`, volver a importar en Kaggle el notebook actualizado o copiar manualmente la celda. Orden: clonar → instalar-go (si hace falta) → compilar → configurar → validación → run. El PIN se introducirá en cada kernel nuevo, no en cada llamada a la API.

## Limitaciones
