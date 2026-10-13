# Tarea 16 — Exponer GPU NVIDIA mediante MCP

Fecha: 2026-10-09.
Estado: implementado y probado localmente. Kaggle real pendiente.

- [x] Definir tools MCP gpu_detect, gpu_metrics, gpu_processes y gpu_monitor.
- [x] Usar nvidia-smi solo-lectura sin shell ni Python, con consultas fijas.
- [x] Distinguir GPU sin detectar, sin driver y error de consulta de GPU activa.
- [x] Mostrar GPU %, VRAM MiB/%, temperatura, potencia, potencia límite, ventilador y pstate si disponibles.
- [x] Procesos NVIDIA compute con PID, UUID y VRAM, sin matar ni alterar nada.
- [x] Monitor cancelable, máximo 10 lecturas, límite 20 segundos nominal y 30 segundos absoluto.
- [x] Manejar N/A y CSV robusto, múltiples GPUs y tamaños de salida acotados.
- [x] Pruebas unitarias y MCP con lecturas simuladas, sin requerir GPU física.
- [x] Documentación de activación en Kaggle y límites de nvidia-smi.
- [ ] Probar métricas contra GPU NVIDIA real con Kaggle Accelerator GPU y revisar disponibilidad MIG.
