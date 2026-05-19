#!/usr/bin/env python3

from __future__ import annotations

import shutil
import sys
from pathlib import Path


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: embed_desktop_artifact.py <build_bin_dir> <embed_dir>", file=sys.stderr)
        return 2

    build_bin = Path(sys.argv[1])
    embed_dir = Path(sys.argv[2])
    embed_dir.mkdir(parents=True, exist_ok=True)

    app_target = embed_dir / "Pando.app"
    if app_target.exists():
        shutil.rmtree(app_target)

    candidates = [
        (build_bin / "Pando.app", app_target, True),
        (build_bin / "pando-desktop", embed_dir / "pando-desktop", False),
        (build_bin / "pando-desktop.exe", embed_dir / "pando-desktop", False),
    ]

    for source, target, is_dir in candidates:
        if source.is_dir() if is_dir else source.is_file():
            if is_dir:
                shutil.copytree(source, target)
            else:
                shutil.copy2(source, target)
            return 0

    nested_apps = sorted(build_bin.rglob("*.app"))
    if nested_apps:
        shutil.copytree(nested_apps[0], app_target)
        return 0

    print("ERROR: pando-desktop binary not found in desktop/build/bin/", file=sys.stderr)
    print("Contents of desktop/build/bin/:", file=sys.stderr)
    if build_bin.exists():
        for path in sorted(build_bin.rglob("*")):
            print(path, file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
