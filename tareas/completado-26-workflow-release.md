# Workflow release con tag, build, binarios sueltos y ZIP (2026-10-11)

Implementado .github/workflows/release.yml:
- workflow_dispatch con version vMAJOR.MINOR.PATCH y push de tags v*.
- build con contents:read: checkout, notebook, tests Go, vet, Go 1.26.9 desde go.mod, Linux amd64/arm64 CGO=0, versión main.version inyectada, zip con README y USO_Y_CONFIGURACION, sumas SHA256.
- publish con contents:write separado, transferencia de artefactos vía Actions, verificación de hashes, creación segura de tag desde commit verificado cuando es manual y subida de binarios/ZIP/SHA256SUMS.txt a Release mediante gh release create --verify-tag. No sobrescribe publicaciones existentes.
- Ramas: manual solo rama predeterminada, tags deben ser ancestros de su historial; concurrencia por versión.
- Pre-release reconocida con sufijo -rc.

Pruebas: actionlint 1.7.12 validó YAML y Bash, go test ./..., go vet ./..., scripts/check_notebook.py, cross-build Linux amd64 y arm64 en host Windows. Documentado RELEASES.md y README. No se hizo push ni se creó tag remoto; QA real pendiente en tarea pendiente-26-validar-release-en-github.md.
