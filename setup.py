from __future__ import annotations

import os
import shutil
import stat
import subprocess
import sys
from pathlib import Path

from setuptools import setup
from setuptools.command.build_py import build_py as _build_py

try:
    from setuptools.command.bdist_wheel import bdist_wheel as _bdist_wheel
except (
    ImportError
):  # pragma: no cover - setuptools may delegate to wheel on older versions.
    try:
        from wheel.bdist_wheel import bdist_wheel as _bdist_wheel
    except ImportError:
        _bdist_wheel = None


ROOT = Path(__file__).parent.resolve()


def _go_module() -> str:
    for line in (ROOT / "go.mod").read_text(encoding="utf-8").splitlines():
        if line.startswith("module "):
            return line.removeprefix("module ").strip()
    raise RuntimeError("could not determine Go module path")


def _binary_name() -> str:
    return "goskill.exe" if sys.platform == "win32" else "goskill"


def _update_repo() -> str:
    return os.environ.get("GITHUB_REPOSITORY", "tdeshazo/goskill")


class build_py(_build_py):
    def run(self) -> None:
        super().run()
        self._build_go_binary()

    def _build_go_binary(self) -> None:
        output = Path(self.build_lib) / "skills_cli" / "bin" / _binary_name()
        if output.parent.exists():
            shutil.rmtree(output.parent)
        output.parent.mkdir(parents=True, exist_ok=True)

        version = self.distribution.get_version()
        ldflags = (
            f"-s -w -X main.version={version} "
            f"-X {_go_module()}/internal/commands.defaultUpdateRepo={_update_repo()}"
        )
        subprocess.check_call(
            [
                "go",
                "build",
                "-trimpath",
                "-ldflags",
                ldflags,
                "-o",
                str(output),
                "./cmd/goskill",
            ],
            cwd=ROOT,
        )

        mode = output.stat().st_mode
        output.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


cmdclass = {"build_py": build_py}


if _bdist_wheel is not None:

    class bdist_wheel(_bdist_wheel):
        def finalize_options(self) -> None:
            super().finalize_options()
            self.root_is_pure = False

        def get_tag(self) -> tuple[str, str, str]:
            _python, _abi, platform = super().get_tag()
            return "py3", "none", platform

    cmdclass["bdist_wheel"] = bdist_wheel


setup(cmdclass=cmdclass)
