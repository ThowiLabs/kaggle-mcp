# Contexto 17 — GPU NVIDIA disponible para agentes MCP

Fecha: 2026-10-09.
Repositorio y workspace exclusivo: `C:\Users\Admin\Documents\GitHub\kagssh-go`.
Kaggle runtime: `/kaggle/working`.
No se modificaron Goradio ni Lilith-MCP.

## Solicitud
El usuario preguntó si MCP puede ver la GPU de Kaggle y pidió herramientas dedicadas para detectarla y consultar consumo/métricas.

## Implementación
Nuevo paquete `internal/gpumetrics` en Go puro (biblioteca estándar), con `ExecRunner` que resuelve `nvidia-smi` y ejecuta solo comandos de consulta fijos sin invocar shell:
- `gpu_detect`: `--query-gpu=index,uuid,name,driver_version,memory.total,pci.bus_id`
- `gpu_metrics`: índices, UUID, nombre, GPU/mem util %, VRAM total/usada/libre MiB, temperatura, potencia, potencia límite, ventilador, pstate y porcentaje de VRAM ocupada
- `gpu_processes`: `--query-compute-apps=gpu_uuid,pid,process_name,used_gpu_memory`
- `gpu_monitor`: muestras de GPU de 1 a 10 intervalos 1–5 segundos, duración nominal <=20 s y máximo absoluto 30 s (incluidas consultas)

Todos los comandos tienen timeout de 6s y salida total limitada a 128KiB; no crean procesos persistentes ni escriben archivos. No guardan métricas en memoria persistente ni atribuyen ownership a agentes. El parser CSV utiliza encoding/csv, admite nombres de proceso entrecomillados y campos no soportados `N/A` devueltos como nil/JSON omitido en vez de falsos ceros.

`internal/mcp/server.go` registra estas 4 herramientas MCP, `internal/mcp/gpu_tools.go` las invoca. OAuth/PIN existentes protegen los endpoints.

## Estados
Con GPU ausente, binario no instalado o driver inaccesible, informe `available=false` con status `not_installed`, `driver_or_query_error`, `no_devices` y guía. La lógica no afirma que Kaggle tenga GPU desactivada si el fallo no es inequívoco.

## Pruebas
`internal/gpumetrics/gpu_test.go`: detección multitarjeta, porcentaje VRAM, energía, lectura N/A, procesos GPU con comas, errores de driver/binario, CSV incorrecto, límites de memoria/salida, monitor y cancelación.
`internal/mcp/gpu_tools_test.go`: listado tools MCP, invocación de las cuatro y rechazo de monitor sin límites válidos.
Pruebas generales, race, vet, notebook y build Linux antes del push.

## Restricciones y pendientes
No se ha probado una GPU física Kaggle; ejecutar E2E al activar Accelerator GPU en un notebook. `nvidia-smi` no es un medidor per-proceso perfecto, y algunas GPU/MIG/controladores no reportan todos los campos. No usa NVML ni GPU AMD/Intel. No instala CUDA/Python.
Referencia: https://docs.nvidia.com/deploy/nvidia-smi/index.html
