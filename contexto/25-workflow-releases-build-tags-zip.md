# Workflow de compilación y GitHub Releases — 2026-10-11

Implementado workflow .github/workflows/release.yml; eventos workflow_dispatch con versión semver y push de tags v*. Seguridad: build separado (contents: read) y publish (contents: write), concurrency release por tag, validación rama predeterminada, sin sobrescribir tags/releases, reintento seguro cuando mismo tag apunta mismo commit verificado. Build Linux amd64/arm64 (linux-only go build tag), CGO0, main.version inyectado, pruebas notebook/Go/vet, ZIP README/guía, checksum SHA256, GitHub artifact transferencia y subida a Release con gh CLI tras comprobar hashes. GH_TOKEN es nativo, no requiere PAT.

No ejecuciones remotas ni tags creados en este turno. actionlint, go tests/vet, cross build Go Linux comprobados. Guía RELEASES.md. Pendiente E2E de workflow en GitHub tras push autorizado.
