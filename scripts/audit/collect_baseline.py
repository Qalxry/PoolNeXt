#!/usr/bin/env python3
"""Collect PoolNeXt Phase 0 baseline metrics as append-only JSONL records."""

from __future__ import annotations

import argparse
import errno
import json
import math
import os
import re
import stat
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable, Sequence


SCHEMA_VERSION = "poolnext.baseline.metric/v1"
METRIC_UNITS = {
    "api.requests": "count",
    "api.errors": "count",
    "api.latency_ms": "milliseconds",
    "api.response_bytes": "bytes",
    "sql.queries": "count",
    "sql.latency_ms": "milliseconds",
    "sql.rows": "count",
    "process.rss_bytes": "bytes",
    "process.fd_count": "count",
    "process.goroutine_count": "count",
    "disk.logical_bytes": "bytes",
    "disk.allocated_bytes": "bytes",
    "disk.file_count": "count",
    "frontend.dom_nodes": "count",
    "frontend.rendered_rows": "count",
    "frontend.heap_bytes": "bytes",
}


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def percentile(samples: Sequence[float], percentile_value: float) -> float:
    if not samples:
        raise ValueError("cannot calculate a percentile without samples")
    ordered = sorted(samples)
    rank = max(1, math.ceil(percentile_value * len(ordered)))
    return ordered[rank - 1]


def base_dimensions(args: argparse.Namespace) -> dict[str, Any]:
    dimensions: dict[str, Any] = {}
    for raw in getattr(args, "dimension", ()):
        key, separator, value = raw.partition("=")
        if not separator or not key:
            raise ValueError(f"invalid dimension {raw!r}; expected key=value")
        dimensions[key] = value
    return dimensions


def record(
    args: argparse.Namespace,
    metric: str,
    value: float | int,
    *,
    sample_count: int = 1,
    dimensions: dict[str, Any] | None = None,
) -> dict[str, Any]:
    if metric not in METRIC_UNITS:
        raise ValueError(f"unsupported metric: {metric}")
    if isinstance(value, float) and not math.isfinite(value):
        raise ValueError(f"metric {metric} must be finite")
    payload: dict[str, Any] = {
        "schema_version": SCHEMA_VERSION,
        "run_id": args.run_id,
        "scenario": args.scenario,
        "captured_at": utc_now(),
        "metric": metric,
        "value": value,
        "unit": METRIC_UNITS[metric],
        "sample_count": sample_count,
        "dimensions": {**base_dimensions(args), **(dimensions or {})},
    }
    if args.git_commit:
        payload["git_commit"] = args.git_commit
    return payload


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
            raise ValueError(f"output path must not contain symlinks: {current}")
    return absolute


@contextmanager
def exclusive_output_lock(path: Path, timeout_seconds: float = 10.0) -> Iterable[None]:
    lock_path = path.with_name(path.name + ".lock")
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0)
    flags |= getattr(os, "O_NOFOLLOW", 0)
    deadline = time.monotonic() + timeout_seconds
    descriptor: int | None = None
    lock_identity: tuple[int, int] | None = None
    while descriptor is None:
        try:
            descriptor = os.open(lock_path, flags, 0o600)
        except OSError as error:
            if error.errno != errno.EEXIST:
                raise
            if time.monotonic() >= deadline:
                raise TimeoutError(f"timed out waiting for JSONL output lock: {lock_path}") from error
            time.sleep(0.01)
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode):
            raise ValueError(f"JSONL output lock is not a regular file: {lock_path}")
        lock_identity = (info.st_dev, info.st_ino)
        os.write(descriptor, f"pid={os.getpid()}\n".encode("ascii"))
        os.fsync(descriptor)
        yield
    finally:
        os.close(descriptor)
        if lock_identity is not None:
            try:
                current = lock_path.lstat()
                if (current.st_dev, current.st_ino) == lock_identity:
                    lock_path.unlink()
            except FileNotFoundError:
                pass


def write_all(descriptor: int, payload: bytes) -> None:
    view = memoryview(payload)
    while view:
        written = os.write(descriptor, view)
        if written <= 0:
            raise OSError("short write while appending baseline JSONL")
        view = view[written:]


def emit(output: str, records: Iterable[dict[str, Any]]) -> None:
    lines = [
        json.dumps(
            item,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        )
        for item in records
    ]
    if output == "-":
        for line in lines:
            print(line)
        return
    path = reject_symlink_components(Path(output))
    path.parent.mkdir(parents=True, exist_ok=True)
    path = reject_symlink_components(path)
    payload = ("\n".join(lines) + "\n").encode("utf-8")
    with exclusive_output_lock(path):
        flags = os.O_WRONLY | os.O_CREAT | os.O_APPEND | getattr(os, "O_CLOEXEC", 0)
        flags |= getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
        descriptor = os.open(path, flags, 0o600)
        try:
            info = os.fstat(descriptor)
            if not stat.S_ISREG(info.st_mode):
                raise ValueError(f"JSONL output is not a regular file: {path}")
            write_all(descriptor, payload)
            os.fsync(descriptor)
        finally:
            os.close(descriptor)


def is_business_failure(body: bytes) -> bool:
    if not body:
        return False
    try:
        payload = json.loads(body)
    except (UnicodeDecodeError, json.JSONDecodeError):
        return False
    return isinstance(payload, dict) and payload.get("success") is False


def collect_api(args: argparse.Namespace) -> list[dict[str, Any]]:
    parsed = urllib.parse.urlsplit(args.url)
    route = args.route or parsed.path or "/"
    headers: dict[str, str] = {}
    for item in args.header:
        key, separator, value = item.partition(":")
        if not separator or not key.strip():
            raise ValueError(f"invalid header {item!r}; expected Name: value")
        headers[key.strip()] = value.lstrip()
    latencies: list[float] = []
    response_sizes: list[int] = []
    statuses: list[int] = []
    errors = 0
    for _ in range(args.requests):
        request = urllib.request.Request(args.url, method=args.method, headers=headers)
        started = time.perf_counter_ns()
        try:
            with urllib.request.urlopen(request, timeout=args.timeout) as response:
                body = response.read(args.max_response_bytes + 1)
                if len(body) > args.max_response_bytes:
                    raise ValueError(f"response exceeded {args.max_response_bytes} bytes")
                statuses.append(response.status)
                response_sizes.append(len(body))
                if is_business_failure(body):
                    errors += 1
        except urllib.error.HTTPError as error:
            body = error.read(args.max_response_bytes + 1)
            statuses.append(error.code)
            response_sizes.append(min(len(body), args.max_response_bytes))
            errors += 1
        except (OSError, ValueError):
            statuses.append(0)
            response_sizes.append(0)
            errors += 1
        finally:
            latencies.append((time.perf_counter_ns() - started) / 1_000_000)

    common = {"method": args.method, "route": route}
    result = [
        record(args, "api.requests", args.requests, sample_count=args.requests, dimensions=common),
        record(args, "api.errors", errors, sample_count=args.requests, dimensions=common),
    ]
    for label, value in (
        ("p50", percentile(latencies, 0.50)),
        ("p95", percentile(latencies, 0.95)),
        ("p99", percentile(latencies, 0.99)),
        ("max", max(latencies)),
    ):
        result.append(record(
            args,
            "api.latency_ms",
            round(value, 3),
            sample_count=len(latencies),
            dimensions={**common, "stat": label},
        ))
    result.append(record(
        args,
        "api.response_bytes",
        round(sum(response_sizes) / len(response_sizes), 3),
        sample_count=len(response_sizes),
        dimensions={**common, "stat": "mean"},
    ))
    for status_code in sorted(set(statuses)):
        result.append(record(
            args,
            "api.requests",
            statuses.count(status_code),
            sample_count=args.requests,
            dimensions={**common, "status": status_code},
        ))
    return result


def read_rss_bytes(pid: int) -> int:
    status_path = Path(f"/proc/{pid}/status")
    for line in status_path.read_text(encoding="utf-8").splitlines():
        if line.startswith("VmRSS:"):
            match = re.fullmatch(r"VmRSS:\s+(\d+)\s+kB", line)
            if not match:
                break
            return int(match.group(1)) * 1024
    raise ValueError(f"VmRSS was not available for pid {pid}")


def read_goroutine_count(url: str, timeout: float) -> int:
    with urllib.request.urlopen(url, timeout=timeout) as response:
        first_line = response.readline(4096).decode("utf-8", errors="replace")
    match = re.search(r"goroutine profile:\s*total\s+(\d+)", first_line)
    if not match:
        raise ValueError(f"could not parse goroutine count from {url}")
    return int(match.group(1))


def collect_process(args: argparse.Namespace) -> list[dict[str, Any]]:
    dimensions = {"pid": args.pid, "process": args.process_name}
    records = [
        record(args, "process.rss_bytes", read_rss_bytes(args.pid), dimensions=dimensions),
        record(args, "process.fd_count", len(os.listdir(f"/proc/{args.pid}/fd")), dimensions=dimensions),
    ]
    goroutines: int | None = args.goroutines
    if args.goroutine_url:
        goroutines = read_goroutine_count(args.goroutine_url, args.timeout)
    if goroutines is not None:
        records.append(record(args, "process.goroutine_count", goroutines, dimensions=dimensions))
    return records


def disk_usage(path: Path) -> tuple[int, int, int]:
    logical_bytes = 0
    allocated_bytes = 0
    file_count = 0
    stack = [path]
    while stack:
        current = stack.pop()
        info = current.lstat()
        if stat.S_ISLNK(info.st_mode):
            logical_bytes += info.st_size
            allocated_bytes += info.st_blocks * 512
            file_count += 1
        elif stat.S_ISDIR(info.st_mode):
            with os.scandir(current) as iterator:
                stack.extend(Path(entry.path) for entry in iterator)
        else:
            logical_bytes += info.st_size
            allocated_bytes += info.st_blocks * 512
            file_count += 1
    return logical_bytes, allocated_bytes, file_count


def collect_disk(args: argparse.Namespace) -> list[dict[str, Any]]:
    path = args.path.resolve()
    logical, allocated, files = disk_usage(path)
    dimensions = {"path": path.as_posix()}
    return [
        record(args, "disk.logical_bytes", logical, dimensions=dimensions),
        record(args, "disk.allocated_bytes", allocated, dimensions=dimensions),
        record(args, "disk.file_count", files, dimensions=dimensions),
    ]


def collect_sql(args: argparse.Namespace) -> list[dict[str, Any]]:
    dimensions = {"operation": args.operation}
    result = [record(args, "sql.queries", args.query_count, dimensions=dimensions)]
    if args.duration_ms is not None:
        result.append(record(args, "sql.latency_ms", args.duration_ms, dimensions=dimensions))
    if args.rows is not None:
        result.append(record(args, "sql.rows", args.rows, dimensions=dimensions))
    return result


def collect_dom(args: argparse.Namespace) -> list[dict[str, Any]]:
    dimensions = {"route": args.route, "viewport": args.viewport}
    result = [record(args, "frontend.dom_nodes", args.dom_nodes, dimensions=dimensions)]
    if args.rendered_rows is not None:
        result.append(record(args, "frontend.rendered_rows", args.rendered_rows, dimensions=dimensions))
    if args.heap_bytes is not None:
        result.append(record(args, "frontend.heap_bytes", args.heap_bytes, dimensions=dimensions))
    return result


def collect_manual(args: argparse.Namespace) -> list[dict[str, Any]]:
    return [record(args, args.metric, args.value, sample_count=args.sample_count)]


def add_common_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--output", default="-", help="JSONL output path or - for stdout")
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--scenario", required=True)
    parser.add_argument("--git-commit", default="")
    parser.add_argument("--dimension", action="append", default=[], metavar="KEY=VALUE")


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("value must be greater than zero")
    return parsed


def nonnegative_int(value: str) -> int:
    parsed = int(value)
    if parsed < 0:
        raise argparse.ArgumentTypeError("value must not be negative")
    return parsed


def finite_float(value: str) -> float:
    parsed = float(value)
    if not math.isfinite(parsed):
        raise argparse.ArgumentTypeError("value must be finite")
    return parsed


def positive_float(value: str) -> float:
    parsed = finite_float(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("value must be greater than zero")
    return parsed


def nonnegative_float(value: str) -> float:
    parsed = finite_float(value)
    if parsed < 0:
        raise argparse.ArgumentTypeError("value must not be negative")
    return parsed


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    api = subparsers.add_parser("api", help="sample an HTTP API endpoint")
    add_common_arguments(api)
    api.add_argument("--url", required=True)
    api.add_argument("--route", default="", help="stable route label; defaults to URL path")
    api.add_argument("--method", choices=("GET", "HEAD"), default="GET")
    api.add_argument("--requests", type=positive_int, default=10)
    api.add_argument("--timeout", type=positive_float, default=5.0)
    api.add_argument("--max-response-bytes", type=positive_int, default=4 * 1024 * 1024)
    api.add_argument("--header", action="append", default=[], metavar="NAME:VALUE")
    api.set_defaults(handler=collect_api)

    process = subparsers.add_parser("process", help="sample Linux RSS, FD and optional Go goroutine count")
    add_common_arguments(process)
    process.add_argument("--pid", type=positive_int, required=True)
    process.add_argument("--process-name", default="poolnext")
    process.add_argument("--goroutines", type=nonnegative_int)
    process.add_argument("--goroutine-url", default="")
    process.add_argument("--timeout", type=positive_float, default=5.0)
    process.set_defaults(handler=collect_process)

    disk = subparsers.add_parser("disk", help="sample logical/allocated bytes and file count without following symlinks")
    add_common_arguments(disk)
    disk.add_argument("--path", type=Path, required=True)
    disk.set_defaults(handler=collect_disk)

    sql = subparsers.add_parser("sql", help="record query count/latency emitted by a test query logger")
    add_common_arguments(sql)
    sql.add_argument("--operation", required=True)
    sql.add_argument("--query-count", type=nonnegative_int, required=True)
    sql.add_argument("--duration-ms", type=nonnegative_float)
    sql.add_argument("--rows", type=nonnegative_int)
    sql.set_defaults(handler=collect_sql)

    dom = subparsers.add_parser("dom", help="record browser DOM/row/heap measurements")
    add_common_arguments(dom)
    dom.add_argument("--route", required=True)
    dom.add_argument("--viewport", default="1440x900")
    dom.add_argument("--dom-nodes", type=nonnegative_int, required=True)
    dom.add_argument("--rendered-rows", type=nonnegative_int)
    dom.add_argument("--heap-bytes", type=nonnegative_int)
    dom.set_defaults(handler=collect_dom)

    manual = subparsers.add_parser("record", help="append one scalar metric")
    add_common_arguments(manual)
    manual.add_argument("--metric", choices=tuple(METRIC_UNITS), required=True)
    manual.add_argument("--value", type=finite_float, required=True)
    manual.add_argument("--sample-count", type=positive_int, default=1)
    manual.set_defaults(handler=collect_manual)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if args.git_commit and not re.fullmatch(r"[0-9a-f]{7,64}", args.git_commit):
        parser.error("--git-commit must contain 7-64 lowercase hexadecimal characters")
    try:
        records = args.handler(args)
        emit(args.output, records)
    except (OSError, ValueError, urllib.error.URLError) as error:
        parser.exit(2, f"baseline collection failed: {error}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
