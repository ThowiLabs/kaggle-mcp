# Corrección del falso conflicto de GitHub Release tags · 2026-10-11

Problema reportado: GitHub Actions decía "El tag 1.0 apunta a otro commit" y luego lo mismo con 1.1, pero el usuario no veía tags ni releases.

Diagnóstico remoto verificado, sin escrituras:
- git ls-remote --tags origin (ThowiLabs/kagssh-go) -> vacío
- gh release list -R ThowiLabs/kagssh-go -> vacío
- gh api repos/ThowiLabs/kagssh-go/git/ref/tags/1.0 y 1.1 -> HTTP 404
- gh api repos/ThowiLabs/kagssh-go/git/matching-refs/tags/1.0 y 1.1 -> HTTP 200 y []
- El workflow anterior hacía `existing=$(gh api ... --jq '.object.sha' 2>/dev/null || true)`. Ante 404, gh api devuelve JSON del error en stdout aunque termine con exit code 1, por lo cual existing no quedaba vacío y el script lo comparaba como si fuera SHA.

Solución: usar endpoint matching-refs (array, vacío si no existen refs), sin silenciar errores HTTP; `jq` selecciona `refs/tags/$TAG` exacta (evita confundir 1.1 con 1.10). Maneja tags ligeros (commit) y anotados (dereferencia objeto tag a su commit) y verifica SHA del commit validado antes de reanudar. Si no existe referencia, crear tag tras compilaciones y pruebas; nunca sobrescribir uno de otro commit. Si falta permiso GitHub o hay error de red, paso falla mostrando error real, no inventa conflicto.

Pruebas: actionlint, scripts/test_release_version.py (11 casos, 2 dependientes de jq saltados en Windows y ejecutables en ubuntu-latest) y Go tests/vet. No se creó/borró tag, no se publicó Release ni se hizo push; requiere publicar el commit local para que GitHub Actions utilice nueva lógica.
