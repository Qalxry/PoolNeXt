#!/usr/bin/env python3
"""Take and compare read-only identity snapshots of an external Mihomo binary."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import stat
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Sequence


SCHEMA_VERSION = "poolnext.mihomo.snapshot/v1"
IDENTITY_FIELDS = ("device", "inode", "mode", "mtime_ns", "size", "sha256")
DEFAULT_MIHOMO = Path("/opt/clash-party/resources/sidecar/mihomo")


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def checked_output_path(output: Path, target: Path) -> Path:
    destination = output.expanduser().resolve(strict=False)
    if destination == target:
        raise ValueError("snapshot output must not be the Mihomo target")
    if destination.exists() and os.path.samefile(destination, target):
        raise ValueError("snapshot output resolves to the Mihomo target inode")
    return destination


def snapshot(path: Path) -> dict[str, Any]:
    target = path.expanduser().resolve(strict=True)
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(target, flags)
    try:
        before = os.fstat(descriptor)
        if not stat.S_ISREG(before.st_mode):
            raise ValueError(f"Mihomo target is not a regular file: {target}")
        digest = hashlib.sha256()
        while True:
            chunk = os.read(descriptor, 1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
        after = os.fstat(descriptor)
    finally:
        os.close(descriptor)
    for field in ("st_dev", "st_ino", "st_mode", "st_mtime_ns", "st_size"):
        if getattr(before, field) != getattr(after, field):
            raise RuntimeError(f"target metadata changed while hashing: {field}")
    return {
        "schema_version": SCHEMA_VERSION,
        "captured_at": utc_now(),
        "path": target.as_posix(),
        "device": before.st_dev,
        "inode": before.st_ino,
        "mode": format(stat.S_IMODE(before.st_mode), "04o"),
        "mtime_ns": before.st_mtime_ns,
        "size": before.st_size,
        "sha256": digest.hexdigest(),
    }


def atomic_write_snapshot(output: Path, target: Path, payload: dict[str, Any], *, force: bool) -> None:
    destination = checked_output_path(output, target)
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists() and not force:
        raise FileExistsError(f"refusing to replace existing snapshot: {destination}")
    descriptor, temporary = tempfile.mkstemp(prefix=f".{destination.name}.", dir=destination.parent)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as handle:
            json.dump(payload, handle, indent=2, sort_keys=True)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, destination)
    except BaseException:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
        raise


def load_snapshot(path: Path) -> dict[str, Any]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    if payload.get("schema_version") != SCHEMA_VERSION:
        raise ValueError(f"unsupported snapshot schema in {path}")
    missing = [field for field in ("path", *IDENTITY_FIELDS) if field not in payload]
    if missing:
        raise ValueError(f"snapshot {path} is missing fields: {', '.join(missing)}")
    return payload


def compare_snapshots(before: dict[str, Any], after: dict[str, Any]) -> dict[str, Any]:
    changes = {
        field: {"before": before[field], "after": after[field]}
        for field in IDENTITY_FIELDS
        if before[field] != after[field]
    }
    if before["path"] != after["path"]:
        changes["path"] = {"before": before["path"], "after": after["path"]}
    return {
        "schema_version": "poolnext.mihomo.comparison/v1",
        "unchanged": not changes,
        "changes": changes,
    }


def command_snapshot(args: argparse.Namespace) -> int:
    target = args.path.expanduser().resolve(strict=True)
    payload = snapshot(target)
    atomic_write_snapshot(args.output, target, payload, force=args.force)
    print(json.dumps(payload, indent=2, sort_keys=True))
    return 0


def command_compare(args: argparse.Namespace) -> int:
    before = load_snapshot(args.before)
    after = load_snapshot(args.after)
    result = compare_snapshots(before, after)
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if result["unchanged"] else 1


def command_verify(args: argparse.Namespace) -> int:
    before = load_snapshot(args.snapshot)
    target = Path(args.path or before["path"])
    after = snapshot(target)
    result = compare_snapshots(before, after)
    result["current"] = after
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if result["unchanged"] else 1


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Read a Mihomo file descriptor, hash its bytes, and compare inode/mode/mtime/size/SHA-256. "
            "This tool never writes, chmods, renames, executes, or removes the target."
        )
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    create = subparsers.add_parser("snapshot", help="write a read-only identity snapshot")
    create.add_argument("--path", type=Path, default=DEFAULT_MIHOMO)
    create.add_argument("--output", type=Path, required=True)
    create.add_argument("--force", action="store_true", help="replace the snapshot file, never the target")
    create.set_defaults(handler=command_snapshot)

    compare = subparsers.add_parser("compare", help="compare two previously captured snapshots")
    compare.add_argument("--before", type=Path, required=True)
    compare.add_argument("--after", type=Path, required=True)
    compare.set_defaults(handler=command_compare)

    verify = subparsers.add_parser("verify", help="compare a saved snapshot with the current target")
    verify.add_argument("--snapshot", type=Path, required=True)
    verify.add_argument("--path", default="", help="optional target override; defaults to snapshot path")
    verify.set_defaults(handler=command_verify)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return args.handler(args)
    except (FileExistsError, FileNotFoundError, json.JSONDecodeError, OSError, RuntimeError, ValueError) as error:
        print(f"mihomo guard failed: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
