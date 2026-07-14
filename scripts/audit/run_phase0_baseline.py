#!/usr/bin/env python3
"""Run a real Phase 0 smoke baseline with disposable PoolNeXt and Mihomo state."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import secrets
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, BinaryIO, Sequence


REPO_ROOT = Path(__file__).resolve().parents[2]
COLLECTOR = REPO_ROOT / "scripts" / "audit" / "collect_baseline.py"
DOM_COLLECTOR = REPO_ROOT / "scripts" / "audit" / "capture_dom.mjs"
GUARD = REPO_ROOT / "scripts" / "audit" / "mihomo_guard.py"
FIXTURE_GENERATOR = REPO_ROOT / "tests" / "fixtures" / "generate.py"
DEFAULT_MIHOMO = Path("/opt/clash-party/resources/sidecar/mihomo")
DEFAULT_RESULTS_ROOT = REPO_ROOT / "tests" / "baseline" / "results"
RUN_SCHEMA_VERSION = "poolnext.baseline.run/v1"


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def reject_symlink_components(path: Path) -> Path:
    absolute = Path(os.path.abspath(os.fspath(path.expanduser())))
    current = Path(absolute.anchor)
    for component in absolute.parts[1:]:
        current /= component
        try:
            info = current.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(info.st_mode):
            raise ValueError(f"path must not contain symlinks: {current}")
    return absolute


def atomic_write_json(path: Path, payload: dict[str, Any]) -> None:
    destination = reject_symlink_components(path)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination = reject_symlink_components(destination)
    descriptor = os.open(
        destination,
        os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0),
        0o600,
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2, sort_keys=True, allow_nan=False)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
    except BaseException:
        try:
            os.close(descriptor)
        except OSError:
            pass
        raise


def run_checked(
    command: Sequence[str],
    *,
    cwd: Path = REPO_ROOT,
    env: dict[str, str] | None = None,
    timeout: float = 60.0,
) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        list(command),
        cwd=cwd,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=timeout,
        check=False,
    )
    if result.returncode != 0:
        rendered = " ".join(command)
        raise RuntimeError(
            f"command failed with status {result.returncode}: {rendered}\n"
            f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}"
        )
    return result


def git_commit() -> str:
    result = run_checked(["git", "rev-parse", "HEAD"], timeout=10)
    commit = result.stdout.strip()
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError(f"unexpected Git commit: {commit!r}")
    return commit


def reserve_tcp_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def snapshot_mihomo(source: Path, output: Path) -> dict[str, Any]:
    result = run_checked(
        [
            sys.executable,
            str(GUARD),
            "snapshot",
            "--path",
            str(source),
            "--output",
            str(output),
        ],
        timeout=120,
    )
    return json.loads(result.stdout)


def compare_mihomo(before: Path, after: Path) -> dict[str, Any]:
    result = run_checked(
        [
            sys.executable,
            str(GUARD),
            "compare",
            "--before",
            str(before),
            "--after",
            str(after),
        ],
        timeout=30,
    )
    return json.loads(result.stdout)


def copy_mihomo(source: Path, destination: Path, expected_sha256: str) -> str:
    source_flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    source_descriptor = os.open(source, source_flags)
    destination_descriptor: int | None = None
    digest = hashlib.sha256()
    try:
        source_info = os.fstat(source_descriptor)
        if not stat.S_ISREG(source_info.st_mode):
            raise ValueError(f"Mihomo source is not a regular file: {source}")
        destination_descriptor = os.open(
            destination,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0),
            0o700,
        )
        while True:
            chunk = os.read(source_descriptor, 1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
            view = memoryview(chunk)
            while view:
                written = os.write(destination_descriptor, view)
                if written <= 0:
                    raise OSError("short write while copying Mihomo")
                view = view[written:]
        os.fsync(destination_descriptor)
    finally:
        os.close(source_descriptor)
        if destination_descriptor is not None:
            os.close(destination_descriptor)
    copied_sha256 = digest.hexdigest()
    if copied_sha256 != expected_sha256:
        raise RuntimeError(
            f"temporary Mihomo copy hash mismatch: expected {expected_sha256}, got {copied_sha256}"
        )
    return copied_sha256


class ManagedProcess:
    def __init__(
        self,
        command: Sequence[str],
        *,
        cwd: Path,
        env: dict[str, str],
        log_path: Path,
        max_log_bytes: int,
    ) -> None:
        self.command = list(command)
        self.log_path = log_path
        self.max_log_bytes = max_log_bytes
        self.log_bytes = 0
        self.retained_log_bytes = 0
        self.dropped_log_bytes = 0
        self.process = subprocess.Popen(
            self.command,
            cwd=cwd,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        if self.process.stdout is None:
            raise RuntimeError("failed to open child process output pipe")
        self._reader = threading.Thread(
            target=self._drain_output,
            args=(self.process.stdout,),
            name=f"baseline-log-{self.process.pid}",
            daemon=True,
        )
        self._reader.start()

    @property
    def pid(self) -> int:
        return self.process.pid

    def _drain_output(self, stream: BinaryIO) -> None:
        descriptor = os.open(
            self.log_path,
            os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0),
            0o600,
        )
        try:
            while True:
                chunk = stream.read(64 * 1024)
                if not chunk:
                    break
                self.log_bytes += len(chunk)
                remaining = max(0, self.max_log_bytes - self.retained_log_bytes)
                retained = chunk[:remaining]
                if retained:
                    os.write(descriptor, retained)
                    self.retained_log_bytes += len(retained)
                self.dropped_log_bytes += len(chunk) - len(retained)
            if self.dropped_log_bytes:
                marker = (
                    f"\n[PoolNeXt baseline truncated {self.dropped_log_bytes} child log bytes]\n"
                ).encode("utf-8")
                os.write(descriptor, marker)
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
            stream.close()

    def stop(self, timeout: float = 10.0) -> int | None:
        if self.process.poll() is None:
            try:
                os.killpg(self.process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                self.process.wait(timeout=timeout)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(self.process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                self.process.wait(timeout=5)
        self._reader.join(timeout=5)
        return self.process.returncode


def wait_for_json(
    url: str,
    *,
    timeout: float,
    headers: dict[str, str] | None = None,
    require_success: bool = False,
) -> dict[str, Any]:
    deadline = time.monotonic() + timeout
    last_error = "endpoint did not respond"
    while time.monotonic() < deadline:
        request = urllib.request.Request(url, headers=headers or {})
        try:
            with urllib.request.urlopen(request, timeout=min(2.0, timeout)) as response:
                body = response.read(1024 * 1024)
                payload = json.loads(body)
                if require_success and (
                    not isinstance(payload, dict) or payload.get("success") is not True
                ):
                    raise ValueError(f"endpoint returned business failure: {payload!r}")
                return payload
        except (OSError, ValueError, urllib.error.URLError, json.JSONDecodeError) as error:
            last_error = str(error)
            time.sleep(0.1)
    raise TimeoutError(f"timed out waiting for {url}: {last_error}")


def append_search_path(dsn: str, schema: str) -> str:
    if "://" in dsn:
        parsed = urllib.parse.urlsplit(dsn)
        query = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
        query.append(("search_path", schema))
        return urllib.parse.urlunsplit(parsed._replace(query=urllib.parse.urlencode(query)))
    return f"{dsn} search_path={schema}"


def psql_environment(dsn: str, schema: str = "") -> dict[str, str]:
    environment = os.environ.copy()
    environment["PGDATABASE"] = dsn
    environment["PGCONNECT_TIMEOUT"] = "10"
    if schema:
        environment["PGOPTIONS"] = f"-c search_path={schema}"
    else:
        environment.pop("PGOPTIONS", None)
    return environment


def prepare_postgres(
    args: argparse.Namespace,
    run_id: str,
    temporary_root: Path,
) -> tuple[str, str]:
    schema = "poolnext_baseline_" + re.sub(r"[^a-z0-9_]", "_", run_id.lower())[:40]
    schema += "_" + secrets.token_hex(4)
    quoted_schema = '"' + schema.replace('"', '""') + '"'
    run_checked(
        [args.psql, "--no-psqlrc", "--set", "ON_ERROR_STOP=1", "--command", f"CREATE SCHEMA {quoted_schema}"],
        env=psql_environment(args.postgres_dsn),
        timeout=30,
    )
    try:
        if args.postgres_profile:
            fixture_root = temporary_root / "postgres-fixture"
            run_checked(
                [
                    sys.executable,
                    str(FIXTURE_GENERATOR),
                    "legacy-db",
                    "--output-dir",
                    str(fixture_root),
                    "--backend",
                    "postgres",
                    "--profile",
                    args.postgres_profile,
                    "--nodes",
                    str(args.fixture_nodes),
                ],
                timeout=120,
            )
            sql_path = fixture_root / f"legacy-{args.postgres_profile}.postgres.sql"
            run_checked(
                [args.psql, "--no-psqlrc", "--set", "ON_ERROR_STOP=1", "--file", str(sql_path)],
                env=psql_environment(args.postgres_dsn, schema),
                timeout=120,
            )
    except BaseException:
        run_checked(
            [
                args.psql,
                "--no-psqlrc",
                "--set",
                "ON_ERROR_STOP=1",
                "--command",
                f"DROP SCHEMA {quoted_schema} CASCADE",
            ],
            env=psql_environment(args.postgres_dsn),
            timeout=30,
        )
        raise
    return append_search_path(args.postgres_dsn, schema), schema


def drop_postgres_schema(args: argparse.Namespace, schema: str) -> None:
    if not schema or args.keep_postgres_schema:
        return
    quoted_schema = '"' + schema.replace('"', '""') + '"'
    run_checked(
        [args.psql, "--no-psqlrc", "--set", "ON_ERROR_STOP=1", "--command", f"DROP SCHEMA {quoted_schema} CASCADE"],
        env=psql_environment(args.postgres_dsn),
        timeout=30,
    )


def collect_metrics(
    args: argparse.Namespace,
    metrics_path: Path,
    commit: str,
    poolnext: ManagedProcess,
    mihomo: ManagedProcess,
    status_url: str,
    data_path: Path,
) -> None:
    common = [
        "--output", str(metrics_path),
        "--run-id", args.run_id,
        "--scenario", args.scenario,
        "--git-commit", commit,
    ]
    run_checked([
        sys.executable, str(COLLECTOR), "api", *common,
        "--url", status_url, "--route", "/api/status",
        "--requests", str(args.api_requests), "--timeout", str(args.request_timeout),
    ])
    run_checked([
        sys.executable, str(COLLECTOR), "process", *common,
        "--pid", str(poolnext.pid), "--process-name", "poolnext",
    ])
    run_checked([
        sys.executable, str(COLLECTOR), "process", *common,
        "--pid", str(mihomo.pid), "--process-name", "mihomo-temporary-copy",
    ])
    run_checked([
        sys.executable, str(COLLECTOR), "disk", *common, "--path", str(data_path),
    ])
    if args.dom_path:
        command = [
            args.node,
            str(DOM_COLLECTOR),
            "--url", urllib.parse.urljoin(status_url, args.dom_path),
            "--expected-url", urllib.parse.urljoin(status_url, args.dom_path),
            "--run-id", args.run_id,
            "--scenario", args.scenario,
            "--git-commit", commit,
            "--output", str(metrics_path),
            "--ready-selector", args.dom_ready_selector,
        ]
        if args.dom_authenticated_selector:
            command.extend(["--authenticated-selector", args.dom_authenticated_selector])
        if args.dom_storage_state:
            command.extend(["--storage-state", str(args.dom_storage_state)])
        run_checked(command, timeout=args.startup_timeout + 30)


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("value must be greater than zero")
    return parsed


def positive_float(value: str) -> float:
    parsed = float(value)
    if not (parsed > 0 and parsed < float("inf")):
        raise argparse.ArgumentTypeError("value must be finite and greater than zero")
    return parsed


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-root", type=Path, default=DEFAULT_RESULTS_ROOT)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--scenario", default="phase0-real-smoke")
    parser.add_argument("--fixture-manifest", type=Path, required=True)
    parser.add_argument("--poolnext-binary", type=Path, required=True)
    parser.add_argument("--mihomo-binary", type=Path, default=DEFAULT_MIHOMO)
    parser.add_argument("--startup-timeout", type=positive_float, default=30.0)
    parser.add_argument("--request-timeout", type=positive_float, default=5.0)
    parser.add_argument("--api-requests", type=positive_int, default=10)
    parser.add_argument("--max-log-bytes", type=positive_int, default=4 * 1024 * 1024)
    parser.add_argument("--temp-root", type=Path)
    parser.add_argument("--postgres-dsn", default=os.getenv("POOLNEXT_TEST_POSTGRES_DSN", ""))
    parser.add_argument(
        "--postgres-profile",
        choices=("", "v1", "v2", "legacy-v10", "future-v99"),
        default="",
    )
    parser.add_argument("--fixture-nodes", type=positive_int, default=256)
    parser.add_argument("--keep-postgres-schema", action="store_true")
    parser.add_argument("--psql", default="psql")
    parser.add_argument("--node", default="node")
    parser.add_argument("--dom-path", default="")
    parser.add_argument("--dom-storage-state", type=Path)
    parser.add_argument("--dom-ready-selector", default="main")
    parser.add_argument("--dom-authenticated-selector", default="")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", args.run_id):
        parser.error("--run-id must use 1-128 letters, digits, dots, underscores, or hyphens")
    if args.postgres_profile and not args.postgres_dsn:
        parser.error("--postgres-profile requires --postgres-dsn")

    fixture_manifest = args.fixture_manifest.expanduser().resolve(strict=True)
    poolnext_binary = args.poolnext_binary.expanduser().resolve(strict=True)
    mihomo_binary = args.mihomo_binary.expanduser().resolve(strict=True)
    for label, path in (("fixture manifest", fixture_manifest), ("PoolNeXt binary", poolnext_binary)):
        if not path.is_file():
            parser.error(f"{label} is not a regular file: {path}")
    if not os.access(poolnext_binary, os.X_OK):
        parser.error(f"PoolNeXt binary is not executable: {poolnext_binary}")

    output_root = reject_symlink_components(args.output_root)
    output_root.mkdir(parents=True, exist_ok=True)
    output_root = reject_symlink_components(output_root)
    run_dir = output_root / args.run_id
    try:
        run_dir.mkdir(mode=0o700)
    except FileExistsError:
        parser.error(f"run directory already exists; use a new run ID: {run_dir}")

    commit = git_commit()
    started_at = utc_now()
    before_path = run_dir / "mihomo-before.json"
    after_path = run_dir / "mihomo-after.json"
    metrics_path = run_dir / "metrics.jsonl"
    manifest_path = run_dir / "run-manifest.json"
    poolnext_log = run_dir / "poolnext.log"
    mihomo_log = run_dir / "mihomo.log"
    poolnext: ManagedProcess | None = None
    mihomo: ManagedProcess | None = None
    postgres_schema = ""
    result = "incomplete"
    notes: list[str] = []
    before_snapshot: dict[str, Any] = {}
    after_snapshot: dict[str, Any] = {}
    mihomo_version: dict[str, Any] = {}
    poolnext_status: dict[str, Any] = {}
    poolnext_port = reserve_tcp_port()
    controller_port = reserve_tcp_port()
    status_url = f"http://127.0.0.1:{poolnext_port}/api/status"
    controller_url = f"http://127.0.0.1:{controller_port}"

    temporary_parent = None
    if args.temp_root:
        temporary_parent = reject_symlink_components(args.temp_root)
        temporary_parent.mkdir(parents=True, exist_ok=True)
    try:
        before_snapshot = snapshot_mihomo(mihomo_binary, before_path)
        with tempfile.TemporaryDirectory(
            prefix=f"poolnext-{args.run_id}-",
            dir=temporary_parent,
        ) as temporary:
            temporary_root = Path(temporary)
            data_path = temporary_root / "data"
            work_path = temporary_root / "work"
            mihomo_home = temporary_root / "mihomo-home"
            upload_path = data_path / "upload"
            for directory in (data_path, work_path, mihomo_home, upload_path):
                directory.mkdir(parents=True, exist_ok=True)

            copied_mihomo = temporary_root / "mihomo"
            copied_sha256 = copy_mihomo(
                mihomo_binary,
                copied_mihomo,
                before_snapshot["sha256"],
            )
            controller_secret = secrets.token_hex(24)
            mihomo_config = temporary_root / "mihomo.yaml"
            mihomo_config.write_text(
                "\n".join(
                    [
                        f'external-controller: "127.0.0.1:{controller_port}"',
                        f'secret: "{controller_secret}"',
                        "allow-lan: false",
                        "ipv6: false",
                        "mode: rule",
                        "log-level: warning",
                        "proxies: []",
                        "proxy-groups: []",
                        "rules:",
                        "  - MATCH,DIRECT",
                        "",
                    ]
                ),
                encoding="utf-8",
            )
            run_checked(
                [str(copied_mihomo), "-t", "-d", str(mihomo_home), "-f", str(mihomo_config)],
                cwd=work_path,
                timeout=args.startup_timeout,
            )
            child_environment = os.environ.copy()
            child_environment.pop("REDIS_CONN_STRING", None)
            mihomo = ManagedProcess(
                [str(copied_mihomo), "-d", str(mihomo_home), "-f", str(mihomo_config)],
                cwd=work_path,
                env=child_environment,
                log_path=mihomo_log,
                max_log_bytes=args.max_log_bytes,
            )
            mihomo_version = wait_for_json(
                controller_url + "/version",
                timeout=args.startup_timeout,
                headers={"Authorization": f"Bearer {controller_secret}"},
            )

            sql_dsn = ""
            if args.postgres_dsn:
                sql_dsn, postgres_schema = prepare_postgres(args, args.run_id, temporary_root)
            poolnext_environment = child_environment.copy()
            for key in ("SQL_DSN", "DSN", "SQLITE_PATH", "PORT", "POOLNEXT_BOOTSTRAP_FILE"):
                poolnext_environment.pop(key, None)
            poolnext_environment.update(
                {
                    "GIN_MODE": "release",
                    "LOG_LEVEL": "warn",
                    "PORT": str(poolnext_port),
                    "SESSION_SECRET": secrets.token_hex(32),
                    "POOLNEXT_BOOTSTRAP_FILE": str(data_path / "poolnext-bootstrap.txt"),
                    "UPLOAD_PATH": str(upload_path),
                }
            )
            if sql_dsn:
                poolnext_environment["SQL_DSN"] = sql_dsn
            else:
                poolnext_environment["SQLITE_PATH"] = str(data_path / "poolnext.db")
            poolnext = ManagedProcess(
                [str(poolnext_binary)],
                cwd=work_path,
                env=poolnext_environment,
                log_path=poolnext_log,
                max_log_bytes=args.max_log_bytes,
            )
            poolnext_status = wait_for_json(
                status_url,
                timeout=args.startup_timeout,
                require_success=True,
            )
            collect_metrics(
                args,
                metrics_path,
                commit,
                poolnext,
                mihomo,
                status_url,
                data_path,
            )
            result = "passed"
            notes.append(f"temporary Mihomo copy sha256={copied_sha256}")
    except BaseException as error:
        result = "failed"
        notes.append(f"{type(error).__name__}: {error}")
    finally:
        poolnext_exit = poolnext.stop() if poolnext is not None else None
        mihomo_exit = mihomo.stop() if mihomo is not None else None
        try:
            drop_postgres_schema(args, postgres_schema)
        except BaseException as error:
            result = "failed"
            notes.append(f"PostgreSQL cleanup failed: {error}")
        try:
            after_snapshot = snapshot_mihomo(mihomo_binary, after_path)
            comparison = compare_mihomo(before_path, after_path)
            if not comparison.get("unchanged"):
                result = "failed"
                notes.append(f"external Mihomo changed: {comparison.get('changes')}")
        except BaseException as error:
            result = "failed"
            notes.append(f"Mihomo after-snapshot failed: {error}")
        if not metrics_path.exists():
            metrics_path.touch(mode=0o600)

        manifest: dict[str, Any] = {
            "schema_version": RUN_SCHEMA_VERSION,
            "run_id": args.run_id,
            "scenario": args.scenario,
            "started_at": started_at,
            "finished_at": utc_now(),
            "git_commit": commit,
            "environment": {
                "database_backend": "postgres" if args.postgres_dsn else "sqlite",
                "platform": platform.platform(),
                "python": platform.python_version(),
                "poolnext_port": poolnext_port,
                "mihomo_controller_port": controller_port,
                "postgres_schema": postgres_schema,
            },
            "metrics_jsonl": metrics_path.name,
            "fixture_manifest_sha256": sha256_file(fixture_manifest),
            "mihomo_before_snapshot": before_path.name,
            "mihomo_after_snapshot": after_path.name,
            "result": result,
            "notes": "\n".join(notes),
            "poolnext": {
                "binary": str(poolnext_binary),
                "binary_sha256": sha256_file(poolnext_binary),
                "status_url": status_url,
                "status_success": bool(poolnext_status.get("success")),
                "exit_code": poolnext_exit,
                "log_path": poolnext_log.name if poolnext_log.exists() else "",
                "log_bytes": poolnext.log_bytes if poolnext else 0,
                "dropped_log_bytes": poolnext.dropped_log_bytes if poolnext else 0,
            },
            "mihomo": {
                "source": str(mihomo_binary),
                "source_sha256": before_snapshot.get("sha256", ""),
                "after_sha256": after_snapshot.get("sha256", ""),
                "controller_url": controller_url,
                "version": mihomo_version,
                "exit_code": mihomo_exit,
                "log_path": mihomo_log.name if mihomo_log.exists() else "",
                "log_bytes": mihomo.log_bytes if mihomo else 0,
                "dropped_log_bytes": mihomo.dropped_log_bytes if mihomo else 0,
            },
        }
        atomic_write_json(manifest_path, manifest)

    print(json.dumps({"manifest": str(manifest_path), "result": result}, sort_keys=True))
    return 0 if result == "passed" else 1


if __name__ == "__main__":
    sys.exit(main())
