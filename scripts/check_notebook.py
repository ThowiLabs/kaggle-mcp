"""Validación sin dependencias externas del notebook entregado para Kaggle."""
import ast
import json
import re
from pathlib import Path

notebook = Path(__file__).resolve().parents[1] / "notebooks" / "kaggle_mcp.ipynb"
data = json.loads(notebook.read_text(encoding="utf-8"))
if data.get("nbformat") != 4:
    raise SystemExit("Formato .ipynb incompatible: se esperaba nbformat 4.")
ids = set()
code_count = 0
for index, cell in enumerate(data["cells"], start=1):
    cell_id = cell["id"]
    if cell_id in ids:
        raise SystemExit(f"ID de celda repetido: {cell_id}")
    ids.add(cell_id)
    source = cell["source"]
    content = "".join(source) if isinstance(source, list) else source
    if cell["cell_type"] == "code":
        ast.parse(content, filename=f"{notebook.name}:celda-{index}")
        code_count += 1
required = {"clonar", "instalar-go", "compilar", "configurar", "validacion", "run"}
if missing := required - ids:
    raise SystemExit(f"Celdas faltantes: {sorted(missing)}")

# El notebook debe clonar el repositorio renombrado en un directorio independiente.
clone_source = "".join(next(c for c in data["cells"] if c["id"] == "clonar")["source"])
if 'REPO_URL = "https://github.com/ThowiLabs/kaggle-mcp.git"' not in clone_source:
    raise SystemExit("La celda de clonación no apunta al repositorio Kaggle MCP.")
if 'REPO_DIR = WORKING / "kaggle-mcp"' not in clone_source:
    raise SystemExit("La celda de clonación usa un directorio distinto al esperado.")
if "kagssh-go" in clone_source:
    raise SystemExit("La celda de clonación todavía contiene el nombre anterior.")

build_source = "".join(next(c for c in data["cells"] if c["id"] == "compilar")["source"])
if '"./cmd/kagmcp"' not in build_source or '"./cmd/kagssh"' in build_source:
    raise SystemExit("El notebook debe compilar ./cmd/kagmcp.")

# Los workflows GitHub existentes todavía compilan el alias histórico.
legacy_command = notebook.parents[1] / "cmd" / "kagssh"
if not legacy_command.is_symlink() or legacy_command.readlink() != Path("kagmcp"):
    raise SystemExit("Falta el alias compatible cmd/kagssh -> kagmcp.")

# Prueba de comportamiento: compilar sintácticamente la celda no detecta
# expresiones regulares válidas que nunca coinciden (error anterior \\s / \\d).
install_cell = next(c for c in data["cells"] if c["id"] == "instalar-go")
install_source = "".join(install_cell["source"])
patterns = {}
for node in ast.walk(ast.parse(install_source)):
    if not isinstance(node, ast.Assign) or len(node.targets) != 1:
        continue
    target = node.targets[0]
    call = node.value
    if (
        isinstance(target, ast.Name)
        and isinstance(call, ast.Call)
        and isinstance(call.func, ast.Attribute)
        and isinstance(call.func.value, ast.Name)
        and call.func.value.id == "re"
        and call.func.attr == "search"
        and call.args
        and isinstance(call.args[0], ast.Constant)
        and isinstance(call.args[0].value, str)
    ):
        patterns[target.id] = call.args[0].value

mod_pattern = patterns.get("match")
version_pattern = patterns.get("found")
if not mod_pattern or not version_pattern:
    raise SystemExit("No se encontraron los patrones de go.mod y go version.")

mod_text = (notebook.parents[1] / "go.mod").read_text(encoding="utf-8")
mod_match = re.search(mod_pattern, mod_text)
installed_match = re.search(version_pattern, "go version go1.26.9 linux/amd64")
if mod_match is None or mod_match.groups() != ("1", "26", "9"):
    raise SystemExit("La regex del notebook no reconoce 'go 1.26.9' en go.mod.")
if installed_match is None or installed_match.groups() != ("1", "26", "9"):
    raise SystemExit("La regex del notebook no reconoce 'go version go1.26.9'.")
print(f"Notebook válido: {len(data['cells'])} celdas, {code_count} celdas de Python.")
print("Pruebas de regex go.mod y go version: correctas.")

# La celda debe mantenerse en ejecución con los logs heredados del proceso,
# hasta que el usuario pulse Detener/Interruptar en Kaggle.
run_source = "".join(next(c for c in data["cells"] if c["id"] == "run")["source"])
run_tree = ast.parse(run_source)
calls = [node for node in ast.walk(run_tree) if isinstance(node, ast.Call)]
def has_call(name, attr):
    return any(
        isinstance(node.func, ast.Attribute)
        and isinstance(node.func.value, ast.Name)
        and node.func.value.id == name and node.func.attr == attr
        for node in calls
    )
if not has_call("subprocess", "Popen") or not has_call("kagmcp", "wait"):
    raise SystemExit("La celda debe lanzar Go con Popen y esperar su terminación (espera bloqueante).")
if not all(has_call("kagmcp", name) for name in ("poll", "terminate", "kill")):
    raise SystemExit("Falta limpieza del proceso al interrumpir la celda.")
if not any(isinstance(node, ast.ExceptHandler) and isinstance(node.type, ast.Name)
           and node.type.id == "KeyboardInterrupt" for node in ast.walk(run_tree)):
    raise SystemExit("La celda debe atender KeyboardInterrupt de Kaggle.")
if any(value in run_source for value in (
    "start_new_session=True", "stdout=logfile",
    "threading.Thread", "display_id=True",
)):
    raise SystemExit("La celda no puede desacoplar el proceso ni capturar/ocultar logs.")
if ("stdout=subprocess.PIPE" not in run_source
        or "stderr=subprocess.STDOUT" not in run_source
        or 'for line in kagmcp.stdout:' not in run_source
        or 'print(line, end="", flush=True)' not in run_source):
    raise SystemExit("Los logs deben imprimirse línea por línea en la celda, sin hilos.")
if any(c["id"] in {"estado", "cierre", "stop"} for c in data["cells"]):
    raise SystemExit("El notebook no debe depender de celdas STOP/estado adicionales.")
print("Inicio interactivo, logs directos y parada por Interruptar: estructura correcta.")

# Validación de la configuración sin iniciar servicios reales.
import contextlib
import io
import subprocess
from unittest.mock import patch
import getpass
import os

config_source = "".join(next(c for c in data["cells"] if c["id"] == "configurar")["source"])
test_pat = "TEST_ONLY_GITHUB_PAT"
local = {"WORKING": Path("/kaggle/working"), "REPO_DIR": Path("/kaggle/working/kaggle-mcp")}
output = io.StringIO()
with patch.dict(os.environ, {
    "KAGGLE_USER_SECRETS_TOKEN": "TEST_ONLY_INTERNAL",
    "KAGGLE_IAP_TOKEN": "TEST_ONLY_IAP",
    "MCP_ENABLED": "false",
    "GITHUB_TOKEN": "stale-token",
}), contextlib.redirect_stdout(output):
    exec(compile(config_source, "celda-configurar", "exec"), local)
env = local["RUNTIME_ENV"]
if any(k in env for k in ("KAGGLE_USER_SECRETS_TOKEN", "KAGGLE_IAP_TOKEN", "GITHUB_TOKEN")):
    raise SystemExit("El entorno contiene datos que debieron filtrarse.")
if env.get("MCP_ENABLED") != "true" or env.get("MCP_TUNNEL") != "auto":
    raise SystemExit("El servidor no usa la configuración MCP esperada.")
if env.get("MCP_ACCESS_PIN") != "":
    raise SystemExit("El PIN temporal debe ser generado por Go, no Python.")

custom_source = config_source.replace('pin = ""', 'pin = "ABCDEF"')
with patch.dict(os.environ, {}, clear=True), contextlib.redirect_stdout(io.StringIO()):
    custom = {"WORKING": Path("/kaggle/working"), "REPO_DIR": Path("/kaggle/working/kaggle-mcp")}
    exec(compile(custom_source, "celda-configurar", "exec"), custom)
if custom["RUNTIME_ENV"]["MCP_ACCESS_PIN"] != "ABCDEF":
    raise SystemExit("El PIN explícito no se transmite al binario.")

github_source = config_source.replace("USE_GITHUB = False", "USE_GITHUB = True")
with patch.dict(os.environ, {}, clear=True), patch.object(getpass, "getpass", return_value=test_pat), contextlib.redirect_stdout(io.StringIO()):
    github = {"WORKING": Path("/kaggle/working"), "REPO_DIR": Path("/kaggle/working/kaggle-mcp")}
    exec(compile(github_source, "celda-configurar", "exec"), github)
if github["RUNTIME_ENV"].get("GITHUB_TOKEN") != test_pat:
    raise SystemExit("La integración GitHub no obtiene el PAT mediante getpass.")
print("Configuración MCP y credenciales opcionales: correctas.")

validation_source = "".join(next(c for c in data["cells"] if c["id"] == "validacion")["source"])
observed = {}
class FakeCheck:
    returncode = 0
    stdout = "configuración válida"
    stderr = ""
def fake_check(*args, **kwargs):
    observed.update(kwargs)
    if not args or args[0][-1] != "-check":
        raise AssertionError("Se esperaba -check")
    return FakeCheck()
with patch.object(subprocess, "run", fake_check), contextlib.redirect_stdout(io.StringIO()):
    exec(compile(validation_source, "celda-validacion", "exec"), {
        "BINARY": "/kaggle/working/kagmcp-linux-amd64",
        "WORKING": Path("/kaggle/working"), "RUNTIME_ENV": env, "subprocess": subprocess,
    })
if observed.get("env") is not env:
    raise SystemExit("La validación no recibió el entorno preparado.")
print("Validación del binario: flujo correcto.")
