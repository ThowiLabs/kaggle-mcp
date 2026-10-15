# Renombrar comando principal a cmd/kagmcp

Estado: completado. Renombrado y pruebas de compilación AMD64/ARM64, Go y notebook superadas.

- Sustituir ruta principal cmd/kagssh por cmd/kagmcp.
- Adaptar el notebook, guías y scripts que compilan el ejecutable.
- Garantizar que CI y workflow de releases sigan pudiendo compilar.
- Validar go test, go vet, binarios, versión y notebook.
- Crear commit en español y publicar cambios sin reescribir main.

Conservar nombres de variables y del estado persistente usados por instalaciones anteriores.