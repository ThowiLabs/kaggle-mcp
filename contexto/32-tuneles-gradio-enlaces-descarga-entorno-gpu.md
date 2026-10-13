# Fecha
2026-10-13

# Objetivo
Agregar herramientas de gestión de túneles Gradio de aplicaciones locales, enlaces de descarga de archivos, exposición de credenciales configuradas en subprocesos autorizados y ocultación de métricas GPU cuando no hay acelerador.

# Decisiones tomadas
- Reutilizar `internal/gradiolive.Start`: protocolo FRP nativo sin Python Gradio, frpc ni dependencias nuevas.
- Herramientas MCP: `app_tunnel_create`, `app_tunnel_list`, `app_tunnel_regenerate`, `app_tunnel_delete`, `file_download_link`, `file_download_revoke`.
- Los nombres de túneles son aliases locales (1-40 caracteres), no subdominios; URLs HTTPS aleatorias del proveedor y efímeras.
- Comprobar que el puerto es HTTP accesible en 127.0.0.1; no permitir puertos privilegiados ni el puerto del MCP. Máximo cinco túneles simultáneos. Regeneración primero crea la nueva URL y solo entonces cierra la anterior.
- Los gestores residen en memoria compartida entre reconstrucciones OAuth del MCP y se cierran con el proceso principal. No se publican automáticamente al reiniciar; evita que un servicio privado se exponga sin una nueva acción.
- Las descargas son URLs públicas con token aleatorio de 256 bits, acceso solo a archivo regular dentro de las rutas permitidas de Kaggle, sin paths ocultos ni symlinks. Duración 1-24 h, máximo 100 enlaces y archivos de 2 GiB. Revocación inmediata, comprobación de identidad/tamaño/modificación de archivo; GET/HEAD y Range mediante ServeContent; deadline de 30 min para descarga.
- `exec` hereda las credenciales actualmente configuradas de GitHub y Hugging Face al lanzar cada proceso. No hereda valores obsoletos; sustituye los tokens literales en la salida capturada. Esta máscara no convierte código arbitrario en seguro: ejecuciones autorizadas pueden exportar o guardar sus tokens.
- GPU: tarjeta oculta inicialmente; mostrarla solo si API reporta GPU disponible con al menos un dispositivo real; al perder conexión vuelve a ocultarse. Backend mantiene su API de métricas para compatibilidad.

# Arquitectura actual
`internal/mcp/app_tunnels.go`: registro con mutex, límites, ciclo de vida y Gradio. `internal/mcp/downloads.go`: emisión/revocación en memoria y endpoint GET/HEAD `/download/{token}` en el túnel principal. `Server.New`, `Server.Handler`, `Server.invoke`, `Run` y `rebuildMCPForPublic` integran los gestores. `internal/dashboard/ui/dashboard.js` y CSS/HTML controlan tarjeta GPU. GitHub y Hugging Face se sincronizan con el proceso MCP y los comandos hijos.

# Librerías usadas
Sin dependencias adicionales: Go estándar y los componentes gradiolive existentes.

# Archivos importantes modificados
`internal/mcp/server.go`, `management.go`, `recovery.go`, `app_tunnels.go`, `app_tunnels_test.go`, `downloads.go`, `downloads_test.go`, `command_environment_test.go`; `internal/dashboard/ui/{dashboard.js,dashboard.css,page.html}`; `scripts/test_gpu_visibility.cjs`; `README.md`; `USO_Y_CONFIGURACION.md`; `.github/workflows/{ci.yml,release.yml}` y este archivo.

# Problemas encontrados
- `exec` filtraba GITHUB_TOKEN y HF_TOKEN y no los volvía a añadir, aunque el panel los configuraba en memoria.
- El panel mostraba tarjeta GPU con N/D cuando no existía GPU.
- El servidor anterior solo podía establecer túnel para su propia API y no tenía registro de apps.
- No existía un endpoint acotado para descargar archivos concretos con permisos de enlace.

# Soluciones implementadas
Seis herramientas MCP y sus rutas/validaciones; acceso de credenciales desde comandos nuevos; ocultación dinámica de GPU sin pérdida de API; documentación de seguridad y ejemplos de uso.

# Estado de tests
Go unitarios MCP y dashboard, tests de fake Gradio para CRUD y fallos, enlace de descarga/ruta privada/revocación/reemplazo, propagación de tokens con valores ficticios, y test Node VM de GPU disponible/ausente. CI y release ejecutan nuevo test Node; suite Go existente sigue cubriendo OAuth, HTTPS y protocolos MCP. Integración real con Gradio remoto aún depende de red/proveedor/instancia activa.

# Estado de seguridad
Las URLs Gradio de apps no están protegidas automáticamente por PIN del MCP: debe protegerlas su propia app antes de exponerse. URL de archivo funciona como bearer token hasta que expire/revoque; nunca compartirla si el archivo es privado. Los procesos de exec son potentes y pueden leer sus tokens. No se copian tokens a Git ni a los JSON privados. No se modifica la instancia original de puerto 8181.

# Pendientes
Prueba manual con app real en puerto distinto al MCP y usuario autorizado; verificación de descarga grande HTTPS a través del proveedor (cuotas ajenas a Go). Confirmar acceso del cliente MCP actualizado tras reinicio controlado. No prometer conservar URLs temporales entre cambios de dominio/reinicios.

# Próximos pasos
Actualizar binario y reiniciar de forma planificada la instancia elegida (sin matar la que provee el conector actual); desde MCP, crear app_tunnel_create y file_download_link, confirmar acceso real y revocar.
