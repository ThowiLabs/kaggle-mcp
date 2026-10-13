# Error de Actividad sin descripción · 2026-10-11

Captura usuario: 2 registros sin descripción, sin estado, fecha no disponible y Proyecto general aunque aparece el contador y «En vivo». Causa reproducida: el JSON API /api/activity usa etiquetas minúsculas definidas en internal/projectstate/store.go: description, status, when, project, command. El frontend activity.js intentaba leer event.Description / Status / When / Project / Command (con mayúsculas), devolviendo los textos fallback.

Al corregir las claves se detectó mediante prueba JavaScript otro error: Intl.DateTimeFormat("es-MX",{dateStyle:"short",timeStyle:"medium",timeZoneName:"short"}) es inválido en V8 porque dateStyle/timeStyle no pueden combinarse con timeZoneName. Impedía renderizar toda la lista. Solución: año/mes/día/hora/minuto/segundo/timeZoneName explícitos.

Ahora cabecera lastUpdated en la página Actividad refleja actualización real o «Sin conexión», no «Sin datos» permanentemente. El renderizado mediante DOM sigue usando textContent, sin inyección HTML. script scripts/test_activity_render.cjs simula API real con claves minúsculas y exige descripción, estado, fecha, proyecto, comando y contador. Workflow release.yml ejecuta esa prueba con Node antes de compilar/publicar, para que no se repita.

Pruebas locales node --check, node scripts/test_activity_render.cjs, go test ./..., go vet ./...; no se borra ni migra historial existente: los datos estaban bien guardados. Para observar el cambio en Kaggle hay que publicar el commit, crear/usar nueva Release y reiniciar KagMCP con binario actualizado (go:embed assets).
