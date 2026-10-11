# GitHub Actions: compilar, etiquetar y publicar KagMCP

Workflow: `.github/workflows/release.yml` — aparece en **GitHub → Actions → Publicar KagMCP** una vez que este archivo se publique en la rama predeterminada.

## Dos opciones de publicación

**1. Manual (recomendada para versiones comerciales):** En GitHub abre *Actions → Publicar KagMCP → Run workflow*. Selecciona la rama predeterminada (por ejemplo, `main`) e introduce una versión nueva como `v1.2.3` o `v1.2.3-rc.1`. La ejecución valida pruebas, compila y empaqueta, **crea el tag ligero solamente después de compilar correctamente** y finalmente publica la Release con sus archivos. Si el tag ya existe y apunta al mismo commit, una ejecución fallida antes de publicar puede reanudarse con la misma versión. Nunca sobrescribe tags ni releases existentes.

**2. Por tag:** Una vez publicado el workflow, un tag nuevo con formato semántico `v1.2.3` y cuyo commit pertenezca al historial de la rama principal activará automáticamente la compilación y la Release.

```bash
git tag v1.2.3
git push origin v1.2.3
```

No es necesario un PAT propio para este workflow: utiliza el `GITHUB_TOKEN` temporal que proporciona GitHub Actions. Requiere que el repositorio permita GitHub Actions y que la política organizacional permita `contents: write` para el job de publicación.

## Contenido de cada GitHub Release

Por ejemplo, para `v1.2.3`:

| Archivo | Uso |
| --- | --- |
| `kagmcp-v1.2.3-linux-amd64` | Ejecutable **sin comprimir** Linux x86-64 (incluye Kaggle) |
| `kagmcp-v1.2.3-linux-amd64.zip` | ZIP con ejecutable AMD64 y guías |
| `kagmcp-v1.2.3-linux-arm64` | Ejecutable **sin comprimir** Linux ARM64 |
| `kagmcp-v1.2.3-linux-arm64.zip` | ZIP con ejecutable ARM64 y guías |
| `SHA256SUMS.txt` | Hash SHA-256 de los cuatro archivos anteriores |

Los ZIP incorporan `README.md` y `USO_Y_CONFIGURACION.md`. El binario se compila con `CGO_ENABLED=0`, `-trimpath`, optimizaciones de tamaño y versión embebida: `./kagmcp-v1.2.3-linux-amd64 -version` debería imprimir `kagmcp v1.2.3`.

Si bajas el ejecutable sin comprimir por HTTP a Linux, hazlo ejecutable antes de usarlo: `chmod +x kagmcp-v1.2.3-linux-amd64`. Verifica integridad mediante `sha256sum -c SHA256SUMS.txt` estando junto a los cuatro archivos.

**No incluye Windows/macOS**: el comando principal Go posee `//go:build linux`. No publicamos binarios de escritorio que no funcionarían.

## Seguridad de la automatización

- Job `build`: permisos **solo lectura**. Valida notebook, `go test`, `go vet`; compila Linux amd64/arm64, comprueba enlazado, comprueba `-version`, crea ZIP y verifica SHA-256. Sube artefactos internos con `actions/upload-artifact`.
- Job `publish`: solo después de `build`, con `contents: write`. Descarga y vuelve a verificar los hashes; crea el tag usando el SHA comprobado. Solo publica nuevas versiones; no sobrescribe una Release ni assets existentes.
- Los tags deben seguir `vMAJOR.MINOR.PATCH` con sufijo de pre-release opcional; las releases `-rc.N` se marcan como *prerelease*.
- Los tags que no pertenecen al historial de la rama predeterminada son rechazados.
- El grupo de concurrencia evita dos publicaciones de una misma versión al mismo tiempo; `cancel-in-progress: false`.
- El `GITHUB_TOKEN` crea el tag sin disparar de nuevo un workflow por push, evitando bucles.
- El job de escritura no hace checkout ni ejecuta código del repo; solo consume los paquetes verificados.
- Si el proceso se interrumpe tras crear el tag y antes de publicar la Release, vuelve a ejecutar manualmente la misma versión desde el mismo commit. Si ya existe una Release, se exige una versión nueva: se prefiere no sobrescribir publicaciones.

## Validaciones realizadas localmente

`actionlint` sobre el workflow, `go test ./...`, `go vet ./...` y cross-build Linux AMD64/ARM64 con `main.version` inyectada. No se disparó la Release real desde GitHub en esta tarea: hay que subir el commit y ejecutar el workflow una vez con una versión nueva.

Enlaces de referencia:
- https://cli.github.com/manual/gh_release_create
- https://cli.github.com/manual/gh_release_edit
- https://docs.github.com/en/actions/concepts/security/github_token
