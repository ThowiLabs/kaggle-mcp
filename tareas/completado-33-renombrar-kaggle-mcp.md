# Renombrar el proyecto a Kaggle MCP

Estado: completado. Renombrado remoto confirmado y validaciones locales superadas; la prueba real en Kaggle queda como seguimiento.

- Renombrar el repositorio remoto de `kagssh-go` a `kaggle-mcp`.
- Migrar el módulo Go y sus importaciones internas.
- Renombrar y corregir el notebook Kaggle, incluido URL de clonación y ruta de trabajo.
- Actualizar referencias activas y documentación sin alterar configuración sensible ni historia previa.
- Ejecutar pruebas Go y notebook; crear commit y publicar los cambios.

Compatibilidad: mantener claves `KAGMCP_*`, nombres de ejecutables `kagmcp-*` y ruta `./cmd/kagssh` para evitar cambios disruptivos.
