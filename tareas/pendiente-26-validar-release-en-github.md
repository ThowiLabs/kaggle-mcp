# Verificación remota de release

Publicar primero el commit del workflow en la rama predeterminada (requiere autorización para push); luego Actions > Publicar KagMCP > Run workflow, versión nunca usada vX.Y.Z de pruebas. Confirmar build Go Linux AMD64/ARM64, Release pública, tag apuntando commit probado, archivos sueltos y ZIP descargables y verificables con SHA256SUMS.txt. Revisar políticas de permisos GITHUB_TOKEN contents:write; si el workflow falla no eliminar ni force-pushear tags. La prueba remota no fue realizada al desarrollar la función.
