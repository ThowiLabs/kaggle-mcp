# GitHub avanzado en KagMCP — repositorios, tags, Actions y archivos

KagMCP dispone de `github_api` (REST genérica) y `github_graphql` (GraphQL), además de herramientas de alto nivel. **No es una cuenta GitHub ilimitada:** el servidor usa el PAT configurado, las políticas de ramas, el plan de GitHub y sus permisos. Todas las operaciones remotas se ejecutan desde el MCP conectado (no desde el chat sin conector).

## Operaciones implementadas

| Caso | Herramientas |
| --- | --- |
| Repositorios y ramas | github_repositories, github_repo_create/delete, github_branches, github_branch_create/delete |
| Archivos de texto | github_file_read/write/delete |
| Subir archivo local binario a repo | `github_file_upload` — solo archivo regular en /kaggle/working, máximo **1 MiB**, destino + rama + mensaje + SHA anterior para actualizar |
| Tags ligeros y anotados | `github_tag_create` (SHA completo; mensaje opcional para anotado), `github_tag_list`, `github_tag_delete` |
| Crear/editar workflow YAML | `github_workflow_upsert` sube `.github/workflows/archivo.yml`, rama y SHA existente. También `github_file_write` |
| Acciones manuales | `github_workflow_dispatch` (workflow con `workflow_dispatch`), `github_workflow_set_state` (enable/disable) |
| Inspeccionar Actions | `github_workflows`, `github_workflow_runs`, `github_workflow_run` |
| Volver a lanzar o cancelar run | `github_workflow_rerun`, `github_workflow_cancel` |
| Releases y binarios | `github_release_create`, `github_release_asset_upload` (archivo local a release existente, máximo **50 MiB**) |
| PRs, issues, publicaciones | github_pr_create/merge, github_issue_create/update, release_create y github_api |
| Otras operaciones documentadas por GitHub | `github_api` o `github_graphql` con los permisos del token (jobs, logs, artifacts, reglas repo, etc.) |

Las herramientas de operaciones con riesgo exigen `confirmed=true`, además de la autorización del cliente MCP y la del usuario para esa acción. Esto incluye crear/borrar tags, subir binarios, modificar un workflow, ejecutar `workflow_dispatch`, cambiar su estado, rerun y cancelación. Otras herramientas legadas como github_api pueden efectuar acciones destructivas con permisos amplios y deben emplearse **solo tras autorización explícita**, tal como indican sus descripciones.

## Ejemplo: publicar nueva versión

1. Consultar `github_status` y `github_branches`, obtener el SHA del commit objetivo mediante `github_api GET /repos/OWNER/REPO/git/ref/heads/main`.
2. Solicitar aprobación explícita para crear el tag. Llamar `github_tag_create` con `repository`, `tag_name=v1.2.0`, `sha`, `tag_message` opcional y `confirmed=true`.
3. Crear release con `github_release_create`, o consultar una existente mediante `github_api`.
4. Verificar nombre y tamaño del binario local en `/kaggle/working`, y tras confirmar subir con `github_release_asset_upload` (`release_id`, `local_path`, `confirmed=true`).
5. Comprobar en GitHub el asset/commit/tag resultante. No inventar resultados.

## Ejemplo: Actions

1. `github_workflows` descubre los workflows. Para añadir uno: `github_workflow_upsert` requiere YAML válido, rama de destino, commit message y `confirmed=true`. Para modificar un workflow existente, obtener primero el SHA de su archivo.
2. El workflow debe contener un evento `workflow_dispatch` para dispararlo manualmente con `github_workflow_dispatch` (`ref`, `inputs`, `confirmed=true`).
3. `github_workflow_runs` muestra la ejecución. Consultar estado con `github_workflow_run`.
4. Con autorización, `github_workflow_rerun` o `github_workflow_cancel`. Otros endpoints de jobs, logs o artifacts son accesibles por `github_api`.

## Permisos del PAT

Para Fine-grained PAT, dar acceso solo a repositorios necesarios, con permisos según el caso:
- **Contents: Read/Write** para archivos, tags y releases.
- **Workflows: Read/Write** (además de Contents) para escribir en `.github/workflows`.
- **Actions: Read/Write** para ejecutar, deshabilitar, cancelar y repetir workflows; Read basta para consultar ejecuciones cuando GitHub lo permite.
- **Issues/Pull requests/Administration** solo si quieres utilizar esas funciones.
- Con tokens clásicos, repos privados suelen necesitar `repo`; para modificar YAML de workflows se requiere también `workflow`.

Si no tienes permiso, GitHub devolverá 403 o 404; KagMCP no eleva privilegios. Algunas reglas de protección y políticas de organizaciones pueden impedir ciertas operaciones aunque el PAT tenga permiso.

## Límites conscientes

- Repositorio: la herramienta de **archivo individual desde Kaggle** admite 1 MiB. Para archivos mayores o repositorios completos utiliza Git push/PR del flujo de trabajo autorizado, y Git LFS si aplica.
- Releases: assets binarios directos de hasta 50 MiB en esta implementación; se usa exclusivamente `uploads.github.com` con token en Authorization, redirecciones deshabilitadas, lectura por streaming.
- GitHub Actions **artifacts** creados durante una ejecución se suelen publicar mediante acciones dentro de un workflow como `actions/upload-artifact`. La API REST permite consultarlos, descargarlos y borrarlos, pero no proporciona el mismo endpoint genérico de subida que las releases.
- La instalación en Kaggle y los permisos reales del usuario necesitan validación de extremo a extremo. **Las pruebas locales usan servidores HTTP simulados, sin cambios remotos**.
- No configura automáticamente secretos de Actions ni publica a producción sin consentimiento específico. Toda creación de workflow puede activar jobs según sus triggers, y cada ejecución puede generar costos.

Documentación GitHub: https://docs.github.com/en/rest/git/tags , https://docs.github.com/en/rest/git/refs , https://docs.github.com/en/rest/actions/workflows , https://docs.github.com/en/rest/actions/workflow-runs , https://docs.github.com/en/rest/repos/contents , https://docs.github.com/en/rest/releases/assets
