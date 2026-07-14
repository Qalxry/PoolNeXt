#!/usr/bin/env python3
"""Refresh this repository-derived documentation skill."""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

REPO_SOURCE = '/media/takagisan/Storage/Project/OpenSource/PoolNeXt/ref_repos/mihomo'
SKILL_NAME = 'docs-mihomo'


def find_generator(this_skill: Path) -> Path:
    env_path = os.environ.get("SKILLGEN_REPO_SCRIPT")
    candidates = []
    if env_path:
        candidates.append(Path(env_path))
    candidates.append(this_skill.parent / "skillgen-repo" / "scripts" / "create_skill_from_repo.py")
    candidates.append(Path.cwd() / ".agents" / "skills" / "skillgen-repo" / "scripts" / "create_skill_from_repo.py")
    for candidate in candidates:
        if candidate.is_file():
            return candidate
    raise SystemExit("Could not find skillgen-repo generator. Set SKILLGEN_REPO_SCRIPT.")


def main() -> int:
    this_skill = Path(__file__).resolve().parents[1]
    generator = find_generator(this_skill)
    command = [
        sys.executable,
        str(generator),
        REPO_SOURCE,
        "--skill-dir",
        str(this_skill),
        "--name",
        SKILL_NAME,
        "--overwrite",
    ]
    return subprocess.call(command)


if __name__ == "__main__":
    raise SystemExit(main())
