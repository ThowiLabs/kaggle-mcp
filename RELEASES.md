# GitHub Actions: compilar, etiquetar y publicar KagMCP

Workflow: `.github/workflows/release.yml` — aparece en **GitHub → Actions → Publicar KagMCP** una vez que este archivo se publique en la rama predeterminada.

## Dos opciones de publicación

**1. Manual (recomendada para versiones comerciales):** En GitHub abre *Actions → Publicar KagMCP → Run workflow*. Selecciona la rama predeterminada (por ejemplo, `main`) e deja **vacío** el campo de versión para generar automáticamente `1.0`, `1.1`, `1.2`… o introduce la versión que quieras, por ejemplo `1.0`, `2.5`, `v1.2.3` o `beta-1` (sin espacios y con nombre válido de tag Git). La ejecución valida pruebas, compila y empaqueta, **crea el tag ligero solamente después de compilar correctamente** y finalmente publica la Release con sus archivos. Si el tag ya existe y apunta al mismo commit, una ejecución fallida antes de publicar puede reanudarse indicando **manualmente esa versión**. Si dejas el campo vacío, calculará el siguiente número. Nunca sobrescribe tags ni releases existentes.

**2. Por tag:** Una vez publicado el workflow, un tag nuevo que empiece por un dígito (`1.0`) o por `v` (`v1.2.3`) y cuyo commit pertenezca al historial de la rama principal activará automáticamente la compilación y la Release.

```bash
git tag 1.0
git push origin 1.0
```

No es necesario un PAT propio para este workflow: utiliza el `GITHUB_TOKEN` temporal que proporciona GitHub Actions. Requiere que el repositorio permita GitHub Actions y que la política organizacional permita `contents: write` para el job de publicación.

## Contenido de cada GitHub Release

Por ejemplo, para `1.0`:

| Archivo | Uso |
| --- | --- |
| `kagmcp-1.0-linux-amd64` | Ejecutable **sin comprimir** Linux x86-64 (incluye Kaggle) |
| `kagmcp-1.0-linux-amd64.zip` | ZIP con ejecutable AMD64 y guías |
| `kagmcp-1.0-linux-arm64` | Ejecutable **sin comprimir** Linux ARM64 |
| `kagmcp-1.0-linux-arm64.zip` | ZIP con ejecutable ARM64 y guías |
| `SHA256SUMS.txt` | Hash SHA-256 de los cuatro archivos anteriores |

Los ZIP incorporan `README.md` y `USO_Y_CONFIGURACION.md`. El binario se compila con `CGO_ENABLED=0`, `-trimpath`, optimizaciones de tamaño y versión embebida: `./kagmcp-1.0-linux-amd64 -version` debería imprimir `kagmcp 1.0`.

Si bajas el ejecutable sin comprimir por HTTP a Linux, hazlo ejecutable antes de usarlo: `chmod +x kagmcp-1.0-linux-amd64`. Verifica integridad mediante `sha256sum -c SHA256SUMS.txt` estando junto a los cuatro archivos.

**No incluye Windows/macOS**: el comando principal Go posee `//go:build linux`. No publicamos binarios de escritorio que no funcionarían.

## Seguridad de la automatización

- Job `build`: permisos **solo lectura**. Valida notebook, `go test`, `go vet`; compila Linux amd64/arm64, comprueba enlazado, comprueba `-version`, crea ZIP y verifica SHA-256. Sube artefactos internos con `actions/upload-artifact`.
- Job `publish`: solo después de `build`, con `contents: write`. Descarga y vuelve a verificar los hashes; crea el tag usando el SHA comprobado. Solo publica nuevas versiones; no sobrescribe una Release ni assets existentes.
- Una versión **manual** puede ser cualquier nombre seguro de tag Git: de 1 a 100 caracteres, sin espacios, empezando por letra/número y usando únicamente letras, números, `.`, `_` o `-` (además debe pasar `git check-ref-format`). La secuencia **automática** considera los tags numéricos `MAJOR.MINOR` o `vMAJOR.MINOR`, elige el mayor por valor numérico y suma 1 a MINOR. Empieza en `1.0` si no hay tags numerados. Las etiquetas como `beta-1` o `v1.2.3` no cambian la secuencia automática; se consideran prerelease cuando contienen `-`.
- Los tags que no pertenecen al historial de la rama predeterminada son rechazados.
- Se serializan los workflows de Release del repositorio para disminuir colisiones entre dos cálculos automáticos; `cancel-in-progress: false`. GitHub puede sustituir una ejecución pendiente por otra más reciente dentro del mismo grupo, por lo que revisa los jobs si lanzas muchas versiones seguidas.
- El `GITHUB_TOKEN` crea el tag sin disparar de nuevo un workflow por push, evitando bucles.
- El job de escritura no hace checkout ni ejecuta código del repo; solo consume los paquetes verificados.
- Si el proceso se interrumpe tras crear el tag y antes de publicar la Release, vuelve a ejecutar manualmente la misma versión desde el mismo commit. Si ya existe una Release, se exige una versión nueva: se prefiere no sobrescribir publicaciones.

## Validaciones realizadas localmente

`actionlint` sobre el workflow, pruebas `scripts/test_release_version.py` (incluye `1.0`, `1.1`, `1.2`, `1.10` y versiones manuales), `go test ./...`, `go vet ./...` y cross-build Linux AMD64/ARM64 con `main.version` inyectada. No se disparó la Release real desde GitHub en esta tarea: hay que subir el commit y ejecutar el workflow una vez con una versión nueva.

Enlaces de referencia:
- https://cli.github.com/manual/gh_release_create
- https://cli.github.com/manual/gh_release_edit
- https://docs.github.com/en/actions/concepts/security/github_token
