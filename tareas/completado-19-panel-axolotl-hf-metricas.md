# Panel Axolotl, Hugging Face y telemetría

Objetivo: portar diseño Axolotl al panel KagMCP Go con login existente, configurar HF_TOKEN y GITHUB_TOKEN en memoria/entorno de proceso, priorizar herramientas GitHub MCP y métricas en vivo de disco/GPU. Verificar tests Go y build Linux, actualizar documentación.

Completado en código: panel integrado y CSS Axolotl, login existente, métricas privadas, configuración de tokens, prioridad GitHub y recuperación del token tras caída del túnel. Pruebas Go, Go vet, JS, notebook simulado y build Linux aprobadas. La validación de GPU/visual real está registrada por separado en tarea pendiente 20.
