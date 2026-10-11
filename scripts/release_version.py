#!/usr/bin/env python3
"""Determine a GitHub Release tag: explicit Git-compatible name or next X.Y.

Only tags matching X.Y or vX.Y count toward the numeric series; arbitrary
manual names do not perturb the automatic sequence.
"""
from __future__ import annotations

import re
import subprocess
import sys

NUMERIC_VERSION = re.compile(r"v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", re.ASCII)
SAFE_TAG = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,99}", re.ASCII)


def version_from_tags(tags: list[str]) -> str:
    numeric = []
    for tag in tags:
        match = NUMERIC_VERSION.fullmatch(tag)
        if match:
            numeric.append((int(match[1]), int(match[2])))
    if not numeric:
        return "1.0"
    major, minor = max(numeric)
    return f"{major}.{minor + 1}"


def select_version(
    event: str, ref: str, default_branch: str, manual_version: str, tags: list[str]
) -> str:
    if event == "workflow_dispatch":
        if ref != f"refs/heads/{default_branch}":
            raise ValueError(
                f"Las releases manuales deben ejecutarse desde {default_branch}"
            )
        selected = manual_version.strip() or version_from_tags(tags)
    elif event == "push":
        if not ref.startswith("refs/tags/"):
            raise ValueError("El evento push no procede de un tag")
        selected = ref.removeprefix("refs/tags/")
    else:
        raise ValueError("Evento de publicación desconocido")

    if not SAFE_TAG.fullmatch(selected):
        raise ValueError(
            "Versión inválida: utiliza un tag Git sin espacios como "
            "1.0, 1.1, 2.5, v1.0 o beta-1"
        )
    validation = subprocess.run(
        ["git", "check-ref-format", f"refs/tags/{selected}"],
        capture_output=True,
        text=True,
        check=False,
    )
    if validation.returncode != 0:
        raise ValueError("El nombre no cumple las reglas de tags Git")
    return selected


def main() -> int:
    if len(sys.argv) != 5:
        print(
            "Uso: release_version.py EVENT GITHUB_REF DEFAULT_BRANCH MANUAL_VERSION",
            file=sys.stderr,
        )
        return 2
    event, ref, branch, manual = sys.argv[1:]
    result = subprocess.run(
        ["git", "tag", "--list"],
        check=True, capture_output=True, text=True
    )
    tags = result.stdout.splitlines()
    try:
        print(select_version(event, ref, branch, manual, tags))
    except ValueError as exc:
        print(f"::error::{exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
