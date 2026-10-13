# Tarea 08 — Migrar configuración del notebook a Python

**Completado en código y simulación local (2026-10-09).**

- [x] PIN y token personal por `getpass` de forma interactiva, sin guardarse en celdas.
- [x] Go `-check` y `run` heredan exactamente `RUNTIME_ENV` y no reciben `KAGGLE_USER_SECRETS_TOKEN`.
- [x] No modificar Lilith ni otros repositorios; rutas de workspace explícitas en todas las operaciones.
- [x] Ajustar documentación y controles automatizados del notebook.
- [ ] Verificar con un Kaggle notebook real el acceso ChatGPT externo (requiere la sesión del usuario).

**Pruebas locales:** `C:/ProgramData/miniconda3/python.exe scripts/check_notebook.py`, `go test -count=1 ./...`, `go test -race ./...`, `go vet ./...`, build `GOOS=linux GOARCH=amd64 CGO_ENABLED=0`, `git diff --check`.

Ver `contexto/09-configuracion-python-notebook.md`.
