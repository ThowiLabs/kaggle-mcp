# Fecha
2026-10-11

# Objetivo
Actualizar el dashboard web de KagMCP con el diseño Axolotl, credenciales Hugging Face/GitHub y métricas reales de GPU/almacenamiento.

# Decisiones tomadas
- Reusar el login existente: PIN + cookies __Host seguras, CSRF, rate limit y expiración de ocho horas; diseño de acceso nuevo.
- Adaptar estilos de la plantilla local Axolotl (licencia MIT conservada) mediante recursos go:embed; no CDN ni runtime frontend adicional.
- GET /api/metrics exige sesión y devuelve GPU de gpumetrics.New().Metrics/nvidia-smi y capacidad del volumen /kaggle/working por syscall.Statfs Linux.
- Poll cada 5 segundos, donuts animados con CSS conic-gradient, estado no disponible cuando no hay GPU, opción de varias GPUs.
- HF_TOKEN puede venir de export/Secrets individuales/entrada oculta en notebook o panel. Verificación de Hugging Face por GET /api/whoami-v2; token retenido solo en memoria del proceso, no persistido.
- El panel configura GITHUB_TOKEN también en entorno del proceso; la API MCP GitHub continúa usando el cliente token validado.
- Herramienta genérica MCP exec oculta HF_TOKEN, GITHUB_ y otras variables sensibles del shell lanzado; evita exposición accidental. El token se hereda en otros procesos autorizados según su mecanismo original.
- Instrucciones del protocolo MCP priorizan github_* al trabajar con GitHub, sin cambiar el prompt de sistema global del cliente.

# Arquitectura actual
Go net/http + html/template y embed, sin frameworks nuevos; handler dashboard sirve recursos públicos no sensibles y endpoints privados, servidor MCP pasa callbacks de integraciones. CSS Axolotl copiado y CSS/JS adaptados separados.

# Librerías usadas
Solo librería estándar Go y gpumetrics existente. Documentación oficial Hugging Face consultada para HF_TOKEN y whoami-v2.

# Archivos importantes modificados

# Problemas encontrados
Sin instancia Linux Kaggle/GPU NVIDIA conectada en el host de Windows, las lecturas en vivo no se pueden certificar hasta la primera ejecución real del notebook. El diseño no se ha comprobado visualmente en navegador automatizado.

# Soluciones implementadas
Fallback explícito cuando GPU o Statfs no están disponibles; pruebas HTTP de autenticación y metrics, test de validación de Hugging Face, tests Go y validación de sintaxis JS.

# Pendientes
Prueba manual en Kaggle Linux con GPU activada y desactivada; verificación responsive en Chrome y túnel Gradio real; confirmar almacenamiento real según configuración de instancia.

# Próximos pasos
Ejecutar notebook clonado en Kaggle (configurar → validar → iniciar), visitar la URL raíz con PIN y comprobar donuts, login e integraciones. Evitar mostrar o registrar los tokens.
