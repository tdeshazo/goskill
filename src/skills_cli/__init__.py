from __future__ import annotations

import os
import subprocess
import sys
from importlib import resources
from pathlib import Path
from typing import Sequence


def _binary_path() -> Path:
    name = "goskill.exe" if os.name == "nt" else "goskill"
    return Path(str(resources.files(__package__).joinpath("bin", name)))


def main(argv: Sequence[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    binary = _binary_path()
    if not binary.exists():
        raise SystemExit(
            "The bundled goskill binary is missing. Rebuild and reinstall the wheel."
        )

    command = [str(binary), *args]
    if os.name == "nt":
        return subprocess.call(command)

    os.execv(str(binary), command)
    return 127


__all__ = ["main"]
