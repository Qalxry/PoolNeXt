#!/usr/bin/env python3
"""Generate deterministic, disposable PoolNeXt baseline fixtures.

Only Python's standard library is used. Generated databases and large fixture
streams belong in a temporary/output directory and are intentionally not kept
in the repository.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import sqlite3
import sys
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Iterable, Iterator, Sequence


GENERATOR_VERSION = 2
DEFAULT_SEED = 20260714
BASE_TIME = datetime(2026, 1, 1, tzinfo=timezone.utc)
LEGACY_PROFILES = {
    # V1 is reconstructed from the initial PoolX models plus the explicit V2
    # migration that added app_logs.level. It intentionally has no proxy-pool
    # tables: those did not exist in V1.
    "unversioned": {
        "version": None,
        "schema_family": "poolx-v1",
        "schema_commit": "cf2bc82",
    },
    "v0": {
        "version": 0,
        "schema_family": "poolx-v1",
        "schema_commit": "cf2bc82",
    },
    "v1": {
        "version": 1,
        "schema_family": "poolx-v1",
        "schema_commit": "cf2bc82",
    },
    # PoolNeXt V2 at 50638db introduced subscription source fields and the
    # consolidated proxy_settings_json representation.
    "v2": {
        "version": 2,
        "schema_family": "poolnext-v2",
        "schema_commit": "50638db",
    },
    # PoolX V10 immediately before c98b60f reset the version baseline. The
    # strategy/test/load-balance fields are separate historical columns.
    "legacy-v10": {
        "version": 10,
        "schema_family": "poolx-v10",
        "schema_commit": "5b85027",
    },
    # Negative control uses a known current-family shape with an unknown future
    # version. The application must reject the version rather than rewrite it.
    "future-v99": {
        "version": 99,
        "schema_family": "poolnext-v2",
        "schema_commit": "50638db",
    },
}
NODE_TYPES = ("ss", "vmess", "trojan", "vless", "hysteria2")
REGIONS = ("hk", "sg", "jp", "us", "de", "gb", "au", "ca")
LOG_LEVELS = ("debug", "info", "warn", "error")
LOG_CATEGORIES = ("runtime", "health", "business", "security")
LIFECYCLE_ACTIONS = ("start", "reload", "stop")
LIFECYCLE_FAULTS = (
    "stale_generation_exit",
    "port_conflict",
    "controller_timeout",
    "runtime_log_128k_line",
    "sigterm_during_transition",
    "managed_process_crash",
)


def utc_text(offset_seconds: int = 0, *, milliseconds: int = 0) -> str:
    value = BASE_TIME + timedelta(seconds=offset_seconds, milliseconds=milliseconds)
    return value.isoformat(timespec="milliseconds").replace("+00:00", "Z")


def compact_json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def stable_token(seed: int, namespace: str, index: int, length: int = 32) -> str:
    payload = f"poolnext-fixture:{seed}:{namespace}:{index}".encode()
    return hashlib.sha256(payload).hexdigest()[:length]


def deterministic_uuid(seed: int, index: int) -> str:
    raw = stable_token(seed, "uuid", index, 32)
    return f"{raw[:8]}-{raw[8:12]}-4{raw[13:16]}-a{raw[17:20]}-{raw[20:32]}"


def node_record(index: int, seed: int) -> dict[str, Any]:
    node_id = index + 1
    node_type = NODE_TYPES[index % len(NODE_TYPES)]
    source_id = index // 1000 + 1
    second_octet = 18 + ((index // (254 * 256)) % 2)
    third_octet = (index // 254) % 256
    fourth_octet = index % 254 + 1
    server = f"198.{second_octet}.{third_octet}.{fourth_octet}"
    port = 10000 + ((index * 37) % 50000)
    region = REGIONS[index % len(REGIONS)]
    identity = {
        "name": f"fixture-{region}-{node_id:05d}",
        "type": node_type,
        "server": server,
        "port": port,
        "credential": stable_token(seed, "credential", index, 24),
        "uuid": deterministic_uuid(seed, index),
    }
    fingerprint = sha256_bytes(compact_json(identity).encode())
    enabled = index % 17 != 0
    status_selector = index % 11
    if status_selector in (0, 1):
        test_status = "failed"
        latency: int | None = None
        test_error = "fixture dial timeout"
    elif status_selector == 2:
        test_status = "unknown"
        latency = None
        test_error = ""
    else:
        test_status = "success"
        latency = 18 + ((index * 13) % 480)
        test_error = ""
    metadata = {
        "cipher": "aes-128-gcm" if node_type == "ss" else "",
        "network": "tcp" if index % 3 else "ws",
        "password": identity["credential"],
        "tls": node_type in ("trojan", "vless", "hysteria2"),
        "uuid": identity["uuid"],
    }
    tested_at = None if test_status == "unknown" else utc_text(index % 86400)
    return {
        "id": node_id,
        "source_config_id": source_id,
        "source_config_name": f"fixture-source-{source_id:03d}.yaml",
        "name": identity["name"],
        "type": node_type,
        "server": server,
        "port": port,
        "tags": f"region:{region},tier:{1 + index % 3}",
        "fingerprint": fingerprint,
        "metadata_json": compact_json(metadata),
        "enabled": enabled,
        "last_test_status": test_status,
        "last_latency_ms": latency,
        "last_test_error": test_error,
        "last_tested_at": tested_at,
        "created_at": utc_text(index % 86400),
        "updated_at": utc_text(index % 86400),
    }


def iter_nodes(count: int, seed: int) -> Iterator[dict[str, Any]]:
    for index in range(count):
        yield node_record(index, seed)


def atomic_write_text(path: Path, content: str, *, force: bool) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists() and not force:
        raise FileExistsError(f"refusing to replace existing fixture: {path}")
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    except BaseException:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
        raise


def atomic_write_lines(path: Path, lines: Iterable[str], *, force: bool) -> int:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists() and not force:
        raise FileExistsError(f"refusing to replace existing fixture: {path}")
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    count = 0
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as handle:
            for line in lines:
                handle.write(line)
                if not line.endswith("\n"):
                    handle.write("\n")
                count += 1
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    except BaseException:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
        raise
    return count


def sqlite_type(kind: str) -> str:
    return {
        "id": "INTEGER PRIMARY KEY AUTOINCREMENT",
        "pk": "INTEGER PRIMARY KEY",
        "int": "INTEGER",
        "bool": "INTEGER",
        "text": "TEXT",
        "time": "TEXT",
    }[kind]


def postgres_type(kind: str) -> str:
    return {
        "id": "BIGSERIAL PRIMARY KEY",
        "pk": "BIGINT PRIMARY KEY",
        "int": "BIGINT",
        "bool": "BOOLEAN",
        "text": "TEXT",
        "time": "TIMESTAMPTZ",
    }[kind]


def column_sql(backend: str, name: str, kind: str, constraints: str = "") -> str:
    sql_type = sqlite_type(kind) if backend == "sqlite" else postgres_type(kind)
    suffix = f" {constraints.strip()}" if constraints.strip() else ""
    return f'  "{name}" {sql_type}{suffix}'


def create_table(backend: str, name: str, columns: Sequence[tuple[str, str, str]]) -> str:
    rendered = ",\n".join(column_sql(backend, *column) for column in columns)
    return f'CREATE TABLE "{name}" (\n{rendered}\n);'


def bool_default(backend: str, value: bool) -> str:
    if backend == "postgres":
        return "TRUE" if value else "FALSE"
    return "1" if value else "0"


def core_tables(*, include_log_level: bool) -> list[tuple[str, list[tuple[str, str, str]]]]:
    app_log_columns = [
        ("id", "id", ""),
        ("classification", "text", "NOT NULL"),
    ]
    if include_log_level:
        app_log_columns.append(("level", "text", "NOT NULL DEFAULT 'info'"))
    app_log_columns.extend(
        [
            ("message", "text", "NOT NULL"),
            ("created_at", "time", ""),
        ]
    )
    return [
        ("app_logs", app_log_columns),
        ("files", [
            ("id", "id", ""), ("filename", "text", ""),
            ("description", "text", ""), ("uploader", "text", ""),
            ("uploader_id", "int", ""), ("link", "text", ""),
            ("upload_time", "text", ""), ("download_counter", "int", ""),
        ]),
        ("users", [
            ("id", "id", ""), ("username", "text", ""),
            ("password", "text", "NOT NULL"), ("display_name", "text", ""),
            ("role", "int", "DEFAULT 1"), ("status", "int", "DEFAULT 1"),
            ("token", "text", ""), ("email", "text", ""),
            ("github_id", "text", ""), ("wechat_id", "text", ""),
        ]),
        ("options", [
            ("key", "text", "PRIMARY KEY"), ("value", "text", ""),
        ]),
    ]


def proxy_pool_tables(
    backend: str,
    *,
    subscription_columns: bool,
    historical_strategy_columns: bool,
) -> list[tuple[str, list[tuple[str, str, str]]]]:
    source_columns: list[tuple[str, str, str]] = [("id", "id", "")]
    if subscription_columns:
        source_columns.extend(
            [
                ("source_type", "text", "NOT NULL DEFAULT 'upload'"),
                ("source_url", "text", "NOT NULL DEFAULT ''"),
                ("content_type", "text", "NOT NULL DEFAULT ''"),
                ("fetched_at", "time", ""),
            ]
        )
    source_columns.extend(
        [
            ("filename", "text", "NOT NULL"),
            ("content_hash", "text", "NOT NULL"),
            ("raw_content", "text", "NOT NULL"),
            ("status", "text", "NOT NULL DEFAULT 'parsed'"),
            ("total_nodes", "int", "NOT NULL DEFAULT 0"),
            ("valid_nodes", "int", "NOT NULL DEFAULT 0"),
            ("invalid_nodes", "int", "NOT NULL DEFAULT 0"),
            ("duplicate_nodes", "int", "NOT NULL DEFAULT 0"),
            ("imported_nodes", "int", "NOT NULL DEFAULT 0"),
            ("uploaded_by", "text", ""),
            ("uploaded_by_id", "int", ""),
            ("created_at", "time", ""),
            ("updated_at", "time", ""),
        ]
    )
    profile_common = [
        ("id", "id", ""), ("name", "text", "NOT NULL"),
        ("listen_host", "text", "NOT NULL DEFAULT '127.0.0.1'"),
        ("mixed_port", "int", "NOT NULL"),
        ("socks_port", "int", "NOT NULL DEFAULT 0"),
        ("http_port", "int", "NOT NULL DEFAULT 0"),
    ]
    if historical_strategy_columns:
        profile_settings = [
            ("strategy_type", "text", "NOT NULL DEFAULT 'select'"),
            ("strategy_group_name", "text", "NOT NULL DEFAULT 'POOLX'"),
            ("test_url", "text", "NOT NULL"),
            ("test_interval_seconds", "int", "NOT NULL DEFAULT 300"),
            ("load_balance_strategy", "text", "NOT NULL DEFAULT 'consistent-hashing'"),
            ("load_balance_lazy", "bool", f"NOT NULL DEFAULT {bool_default(backend, False)}"),
            ("load_balance_disable_udp", "bool", f"NOT NULL DEFAULT {bool_default(backend, False)}"),
        ]
    else:
        profile_settings = [("proxy_settings_json", "text", "NOT NULL")]
    profile_tail = [
        ("include_in_runtime", "bool", f"NOT NULL DEFAULT {bool_default(backend, True)}"),
        ("kernel_type", "text", "NOT NULL DEFAULT 'mihomo'"),
        ("created_at", "time", ""), ("updated_at", "time", ""),
    ]
    template_tail = [
        ("include_in_runtime", "bool", f"NOT NULL DEFAULT {bool_default(backend, True)}"),
        ("node_ids_json", "text", "NOT NULL"),
        ("created_at", "time", ""), ("updated_at", "time", ""),
    ]
    return [
        ("source_configs", source_columns),
        ("proxy_nodes", [
            ("id", "id", ""), ("source_config_id", "int", "NOT NULL"),
            ("source_config_name", "text", "NOT NULL"), ("name", "text", "NOT NULL"),
            ("type", "text", "NOT NULL"), ("server", "text", "NOT NULL"),
            ("port", "int", "NOT NULL"), ("tags", "text", "NOT NULL DEFAULT ''"),
            ("fingerprint", "text", "NOT NULL"), ("metadata_json", "text", "NOT NULL"),
            ("enabled", "bool", f"NOT NULL DEFAULT {bool_default(backend, True)}"),
            ("last_test_status", "text", "NOT NULL DEFAULT 'unknown'"),
            ("last_latency_ms", "int", ""), ("last_test_error", "text", ""),
            ("last_tested_at", "time", ""), ("created_at", "time", ""),
            ("updated_at", "time", ""),
        ]),
        ("port_profiles", profile_common + profile_settings + profile_tail),
        ("port_profile_templates", profile_common + profile_settings + template_tail),
        ("port_profile_nodes", [
            ("id", "id", ""), ("port_profile_id", "int", "NOT NULL"),
            ("proxy_node_id", "int", "NOT NULL"),
            ("sort_order", "int", "NOT NULL DEFAULT 0"), ("created_at", "time", ""),
        ]),
        ("runtime_configs", [
            ("id", "id", ""), ("port_profile_id", "int", "NOT NULL"),
            ("kernel_type", "text", "NOT NULL"), ("checksum", "text", "NOT NULL"),
            ("rendered_config", "text", "NOT NULL"),
            ("created_at", "time", ""), ("updated_at", "time", ""),
        ]),
        ("kernel_instances", [
            ("id", "id", ""), ("kernel_type", "text", "NOT NULL"),
            ("status", "text", "NOT NULL DEFAULT 'stopped'"), ("pid", "int", ""),
            ("work_dir", "text", "NOT NULL"), ("config_path", "text", "NOT NULL"),
            ("controller_address", "text", "NOT NULL"),
            ("controller_secret", "text", "NOT NULL"),
            ("active_config_checksum", "text", "NOT NULL DEFAULT ''"),
            ("active_profile_count", "int", "NOT NULL DEFAULT 0"),
            ("active_listener_count", "int", "NOT NULL DEFAULT 0"),
            ("last_action", "text", "NOT NULL DEFAULT ''"),
            ("last_error", "text", ""), ("last_started_at", "time", ""),
            ("last_stopped_at", "time", ""), ("last_reloaded_at", "time", ""),
            ("created_at", "time", ""), ("updated_at", "time", ""),
        ]),
    ]


def schema_statements(backend: str, profile: str) -> list[str]:
    if backend not in ("sqlite", "postgres"):
        raise ValueError(f"unsupported backend: {backend}")
    profile_config = LEGACY_PROFILES[profile]
    schema_family = profile_config["schema_family"]
    tables = core_tables(include_log_level=schema_family != "poolx-v1")
    if schema_family == "poolx-v10":
        tables.extend(proxy_pool_tables(
            backend,
            subscription_columns=False,
            historical_strategy_columns=True,
        ))
    elif schema_family == "poolnext-v2":
        tables.extend(proxy_pool_tables(
            backend,
            subscription_columns=True,
            historical_strategy_columns=False,
        ))
    if profile_config["version"] is not None:
        tables.append(("database_schema_versions", [
            ("id", "pk", ""), ("version", "int", "NOT NULL"),
            ("updated_at", "time", ""),
        ]))
    statements = [create_table(backend, name, columns) for name, columns in tables]
    indexes: dict[str, tuple[str, ...]] = {
        "app_logs": ("classification", "created_at"),
        "files": ("filename", "uploader", "uploader_id"),
        "users": ("display_name", "token", "email", "github_id", "wechat_id"),
    }
    if schema_family != "poolx-v1":
        indexes["app_logs"] += ("level",)
        indexes.update({
            "source_configs": (
                "filename", "content_hash", "status", "uploaded_by",
                "uploaded_by_id", "created_at", "updated_at",
            ),
            "proxy_nodes": (
                "source_config_id", "source_config_name", "name", "type", "server",
                "port", "tags", "enabled", "last_test_status", "last_tested_at",
                "created_at", "updated_at",
            ),
            "port_profiles": ("name", "include_in_runtime", "created_at", "updated_at"),
            "port_profile_templates": ("name", "include_in_runtime", "created_at", "updated_at"),
            "port_profile_nodes": ("port_profile_id", "proxy_node_id", "created_at"),
            "runtime_configs": ("created_at", "updated_at"),
            "kernel_instances": ("status", "created_at", "updated_at"),
        })
        if schema_family == "poolnext-v2":
            indexes["source_configs"] += ("source_type", "fetched_at")
    for table, columns in indexes.items():
        for column in columns:
            statements.append(f'CREATE INDEX "idx_{table}_{column}" ON "{table}" ("{column}");')
    statements.extend(
        [
            'CREATE UNIQUE INDEX "uidx_files_link" ON "files" ("link");',
            'CREATE UNIQUE INDEX "uidx_users_username" ON "users" ("username");',
        ]
    )
    if schema_family != "poolx-v1":
        statements.extend(
            [
                'CREATE UNIQUE INDEX "uidx_proxy_nodes_fingerprint" ON "proxy_nodes" ("fingerprint");',
                'CREATE UNIQUE INDEX "uidx_runtime_configs_port_profile_id" ON "runtime_configs" ("port_profile_id");',
                'CREATE UNIQUE INDEX "uidx_kernel_instances_kernel_type" ON "kernel_instances" ("kernel_type");',
            ]
        )
    return statements


def sql_literal(value: Any, backend: str) -> str:
    if value is None:
        return "NULL"
    if isinstance(value, bool):
        if backend == "postgres":
            return "TRUE" if value else "FALSE"
        return "1" if value else "0"
    if isinstance(value, (int, float)):
        return str(value)
    escaped = str(value).replace("'", "''")
    return f"'{escaped}'"


def insert_statements(
    backend: str,
    table: str,
    columns: Sequence[str],
    rows: Iterable[Sequence[Any]],
    *,
    batch_size: int = 200,
) -> Iterator[str]:
    batch: list[Sequence[Any]] = []
    rendered_columns = ", ".join(f'"{column}"' for column in columns)
    for row in rows:
        batch.append(row)
        if len(batch) >= batch_size:
            yield render_insert_batch(backend, table, rendered_columns, batch)
            batch = []
    if batch:
        yield render_insert_batch(backend, table, rendered_columns, batch)


def render_insert_batch(backend: str, table: str, columns: str, rows: Sequence[Sequence[Any]]) -> str:
    values = ",\n  ".join(
        "(" + ", ".join(sql_literal(value, backend) for value in row) + ")" for row in rows
    )
    return f'INSERT INTO "{table}" ({columns}) VALUES\n  {values};'


def legacy_seed_rows(profile: str, node_count: int, seed: int) -> dict[str, tuple[list[str], Iterable[Sequence[Any]]]]:
    profile_config = LEGACY_PROFILES[profile]
    schema_family = profile_config["schema_family"]
    include_log_level = schema_family != "poolx-v1"
    app_log_columns = ["id", "classification"]
    app_log_rows: list[Sequence[Any]]
    if include_log_level:
        app_log_columns.extend(["level", "message", "created_at"])
        app_log_rows = [
            (1, "system", "info", "fixture database initialized", utc_text()),
            (2, "business", "warn", "fixture degraded endpoint", utc_text(1)),
            (3, "security", "error", "fixture rejected operation", utc_text(2)),
        ]
    else:
        app_log_columns.extend(["message", "created_at"])
        app_log_rows = [
            (1, "system", "fixture V1 database initialized", utc_text()),
            (2, "business", "fixture V1 migration preservation marker", utc_text(1)),
        ]
    rows: dict[str, tuple[list[str], Iterable[Sequence[Any]]]] = {
        "app_logs": (app_log_columns, app_log_rows),
        "files": ([
            "id", "filename", "description", "uploader", "uploader_id", "link",
            "upload_time", "download_counter",
        ], [
            (1, "fixture.txt", "migration preservation marker", "fixture", 1,
             "fixture/marker.txt", utc_text(), 7),
        ]),
        "users": ([
            "id", "username", "password", "display_name", "role", "status", "token",
            "email", "github_id", "wechat_id",
        ], [
            # Intentionally unusable test-only hash; fixtures must never create a known login.
            (1, "fixture_admin", "$2a$10$invalidFixtureHashNotForAuthentication000000000000000",
             "Fixture Admin", 100, 1, "", "fixture@example.invalid", "", ""),
        ]),
        "options": (["key", "value"], [
            ("RegisterEnabled", "false"),
            ("FixtureSchemaFamily", schema_family),
        ]),
    }
    if profile_config["version"] is not None:
        rows["database_schema_versions"] = (
            ["id", "version", "updated_at"],
            [(1, profile_config["version"], utc_text())],
        )
    if schema_family == "poolx-v1":
        return rows

    source_count = max(1, math.ceil(node_count / 1000))
    profile_count = max(1, min(20, math.ceil(max(node_count, 1) / 500)))
    sources: list[Sequence[Any]] = []
    source_columns = ["id"]
    if schema_family == "poolnext-v2":
        source_columns.extend(["source_type", "source_url", "content_type", "fetched_at"])
    source_columns.extend([
        "filename", "content_hash", "raw_content", "status", "total_nodes", "valid_nodes",
        "invalid_nodes", "duplicate_nodes", "imported_nodes", "uploaded_by", "uploaded_by_id",
        "created_at", "updated_at",
    ])
    for index in range(source_count):
        source_id = index + 1
        count_for_source = min(1000, max(0, node_count - index * 1000))
        common = [source_id]
        if schema_family == "poolnext-v2":
            common.extend([
                "upload" if index % 2 == 0 else "subscription_url",
                "" if index % 2 == 0 else f"https://fixtures.invalid/subscription/{source_id}",
                "text/yaml",
                utc_text(index) if index % 2 else None,
            ])
        common.extend([
            f"fixture-source-{source_id:03d}.yaml",
            stable_token(seed, "source", index, 64),
            f"# deterministic fixture source {source_id}\n",
            "imported",
            count_for_source,
            count_for_source,
            0,
            0,
            count_for_source,
            "fixture",
            1,
            utc_text(index),
            utc_text(index),
        ])
        sources.append(tuple(common))

    nodes = list(iter_nodes(node_count, seed))
    node_columns = [
        "id", "source_config_id", "source_config_name", "name", "type", "server", "port", "tags",
        "fingerprint", "metadata_json", "enabled", "last_test_status", "last_latency_ms",
        "last_test_error", "last_tested_at", "created_at", "updated_at",
    ]
    node_rows = [tuple(node[column] for column in node_columns) for node in nodes]

    settings = compact_json({
        "auth_enabled": False,
        "load_balance_disable_udp": False,
        "load_balance_lazy": True,
        "load_balance_strategy": "consistent-hashing",
        "strategy_type": "fallback",
        "test_interval_seconds": 300,
        "test_url": "https://cp.cloudflare.com/generate_204",
        "udp_enabled": True,
    })
    profiles: list[Sequence[Any]] = []
    bindings: list[Sequence[Any]] = []
    binding_id = 1
    for index in range(profile_count):
        profile_id = index + 1
        if schema_family == "poolx-v10":
            profiles.append((
                profile_id, f"fixture-pool-{profile_id:02d}", "127.0.0.1", 20000 + index,
                0, 0, "fallback", "POOLX", "https://cp.cloudflare.com/generate_204",
                300, "consistent-hashing", True, False, index % 7 != 0, "mihomo",
                utc_text(index), utc_text(index),
            ))
        else:
            profiles.append((
                profile_id, f"fixture-pool-{profile_id:02d}", "127.0.0.1", 20000 + index,
                0, 0, settings, index % 7 != 0, "mihomo", utc_text(index), utc_text(index),
            ))
        start = index * 500
        stop = min(start + 500, node_count)
        for sort_order, node_index in enumerate(range(start, stop)):
            bindings.append((binding_id, profile_id, node_index + 1, sort_order, utc_text(index)))
            binding_id += 1

    if schema_family == "poolx-v10":
        templates = [
            (1, "fixture-fallback", "127.0.0.1", 21000, 0, 0, "fallback", "POOLX",
             "https://cp.cloudflare.com/generate_204", 300, "consistent-hashing", True,
             False, True, "[1,2,3]", utc_text(), utc_text()),
            (2, "fixture-disabled", "127.0.0.1", 21001, 0, 0, "select", "POOLX",
             "https://cp.cloudflare.com/generate_204", 300, "consistent-hashing", False,
             False, False, "[]", utc_text(1), utc_text(1)),
        ]
        profile_columns = [
            "id", "name", "listen_host", "mixed_port", "socks_port", "http_port",
            "strategy_type", "strategy_group_name", "test_url", "test_interval_seconds",
            "load_balance_strategy", "load_balance_lazy", "load_balance_disable_udp",
            "include_in_runtime", "kernel_type", "created_at", "updated_at",
        ]
        template_columns = [
            "id", "name", "listen_host", "mixed_port", "socks_port", "http_port",
            "strategy_type", "strategy_group_name", "test_url", "test_interval_seconds",
            "load_balance_strategy", "load_balance_lazy", "load_balance_disable_udp",
            "include_in_runtime", "node_ids_json", "created_at", "updated_at",
        ]
    else:
        templates = [
            (1, "fixture-fallback", "127.0.0.1", 21000, 0, 0, settings, True,
             "[1,2,3]", utc_text(), utc_text()),
            (2, "fixture-disabled", "127.0.0.1", 21001, 0, 0, settings, False,
             "[]", utc_text(1), utc_text(1)),
        ]
        profile_columns = [
            "id", "name", "listen_host", "mixed_port", "socks_port", "http_port",
            "proxy_settings_json", "include_in_runtime", "kernel_type", "created_at", "updated_at",
        ]
        template_columns = [
            "id", "name", "listen_host", "mixed_port", "socks_port", "http_port",
            "proxy_settings_json", "include_in_runtime", "node_ids_json", "created_at", "updated_at",
        ]
    runtime_configs = [
        (index + 1, index + 1, "mihomo", stable_token(seed, "runtime", index, 64),
         f"# fixture runtime config {index + 1}\n", utc_text(index), utc_text(index))
        for index in range(min(profile_count, 3))
    ]
    rows["options"] = (["key", "value"], [
        ("KernelType", "mihomo"),
        ("MihomoBinaryPath", "/fixture/immutable/mihomo"),
        ("ClashExternalController", "127.0.0.1:29090"),
        ("RegisterEnabled", "false"),
        ("FixtureSchemaFamily", schema_family),
    ])
    rows.update({
        "source_configs": (source_columns, sources),
        "proxy_nodes": (node_columns, node_rows),
        "port_profiles": (profile_columns, profiles),
        "port_profile_templates": (template_columns, templates),
        "port_profile_nodes": ([
            "id", "port_profile_id", "proxy_node_id", "sort_order", "created_at",
        ], bindings),
        "runtime_configs": ([
            "id", "port_profile_id", "kernel_type", "checksum", "rendered_config",
            "created_at", "updated_at",
        ], runtime_configs),
        "kernel_instances": ([
            "id", "kernel_type", "status", "pid", "work_dir", "config_path",
            "controller_address", "controller_secret", "active_config_checksum",
            "active_profile_count", "active_listener_count", "last_action", "last_error",
            "last_started_at", "last_stopped_at", "last_reloaded_at", "created_at", "updated_at",
        ], [
            (1, "mihomo", "stopped", None, "/fixture/runtime", "/fixture/runtime/config.yaml",
             "127.0.0.1:29090", "fixture-controller-secret", "", 0, 0, "stop", "",
             None, utc_text(), None, utc_text(), utc_text()),
        ]),
    })
    return rows


def render_legacy_sql(backend: str, profile: str, node_count: int, seed: int) -> str:
    statements = [
        "-- Generated by tests/fixtures/generate.py; load only into a disposable empty database.",
        f"-- generator_version={GENERATOR_VERSION} profile={profile} seed={seed} nodes={node_count}",
        "BEGIN;",
    ]
    statements.extend(schema_statements(backend, profile))
    seed_rows = legacy_seed_rows(profile, node_count, seed)
    for table, (columns, rows) in seed_rows.items():
        statements.extend(insert_statements(backend, table, columns, rows))
    if backend == "postgres":
        serial_tables = (
            "app_logs", "files", "users", "source_configs", "proxy_nodes", "port_profiles",
            "port_profile_templates", "port_profile_nodes", "runtime_configs", "kernel_instances",
        )
        for table in (name for name in serial_tables if name in seed_rows):
            statements.append(
                f"SELECT setval(pg_get_serial_sequence('{table}', 'id'), COALESCE(MAX(id), 1), MAX(id) IS NOT NULL) FROM \"{table}\";"
            )
    statements.append("COMMIT;")
    return "\n\n".join(statements) + "\n"


def write_sqlite_database(path: Path, sql: str, *, force: bool) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists() and not force:
        raise FileExistsError(f"refusing to replace existing fixture: {path}")
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    os.close(fd)
    try:
        connection = sqlite3.connect(temporary)
        try:
            connection.executescript(sql)
            result = connection.execute("PRAGMA integrity_check").fetchone()
            if result != ("ok",):
                raise RuntimeError(f"generated SQLite database failed integrity_check: {result}")
        finally:
            connection.close()
        os.replace(temporary, path)
    except BaseException:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
        raise


def artifact(path: Path, root: Path, **extra: Any) -> dict[str, Any]:
    result = {
        "path": path.relative_to(root).as_posix(),
        "bytes": path.stat().st_size,
        "sha256": sha256_file(path),
    }
    result.update(extra)
    return result


def write_manifest(path: Path, payload: dict[str, Any], *, force: bool) -> None:
    atomic_write_text(path, json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n", force=force)


def generate_legacy(args: argparse.Namespace) -> dict[str, Any]:
    root = args.output_dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    backends = ("sqlite", "postgres") if args.backend == "all" else (args.backend,)
    artifacts: list[dict[str, Any]] = []
    for backend in backends:
        sql = render_legacy_sql(backend, args.profile, args.nodes, args.seed)
        sql_path = root / f"legacy-{args.profile}.{backend}.sql"
        atomic_write_text(sql_path, sql, force=args.force)
        artifacts.append(artifact(sql_path, root, kind=f"{backend}-sql"))
        if backend == "sqlite":
            db_path = root / f"legacy-{args.profile}.sqlite.db"
            write_sqlite_database(db_path, sql, force=args.force)
            artifacts.append(artifact(db_path, root, kind="sqlite-database"))
    manifest_path = root / f"legacy-{args.profile}.manifest.json"
    payload = {
        "fixture": "legacy-database",
        "generator_version": GENERATOR_VERSION,
        "profile": args.profile,
        "schema_family": LEGACY_PROFILES[args.profile]["schema_family"],
        "schema_commit": LEGACY_PROFILES[args.profile]["schema_commit"],
        "schema_version": LEGACY_PROFILES[args.profile]["version"],
        "seed": args.seed,
        "requested_node_count": args.nodes,
        "node_count": 0 if LEGACY_PROFILES[args.profile]["schema_family"] == "poolx-v1" else args.nodes,
        "artifacts": artifacts,
    }
    write_manifest(manifest_path, payload, force=args.force)
    payload["manifest"] = manifest_path.name
    return payload


def clash_proxy(node: dict[str, Any], seed: int) -> list[str]:
    password = stable_token(seed, "credential", node["id"] - 1, 24)
    prefix = [
        f"  - name: {node['name']}",
        f"    type: {node['type']}",
        f"    server: {node['server']}",
        f"    port: {node['port']}",
    ]
    if node["type"] == "ss":
        return prefix + ["    cipher: aes-128-gcm", f"    password: {password}"]
    if node["type"] in ("vmess", "vless"):
        return prefix + [f"    uuid: {deterministic_uuid(seed, node['id'] - 1)}", "    network: tcp"]
    return prefix + [f"    password: {password}"]


def generate_nodes(args: argparse.Namespace) -> dict[str, Any]:
    output = args.output.resolve()
    if args.format == "jsonl":
        count = atomic_write_lines(
            output,
            (compact_json(node) for node in iter_nodes(args.count, args.seed)),
            force=args.force,
        )
    else:
        def yaml_lines() -> Iterator[str]:
            yield "proxies:"
            for node in iter_nodes(args.count, args.seed):
                yield from clash_proxy(node, args.seed)

        count = atomic_write_lines(output, yaml_lines(), force=args.force)
    return {
        "fixture": "nodes",
        "format": args.format,
        "seed": args.seed,
        "records": args.count,
        "physical_lines": count,
        "artifact": artifact(output, output.parent),
    }


def fixed_ascii_payload(prefix: str, byte_length: int, fill: str = "x") -> str:
    encoded = prefix.encode("ascii")
    if len(encoded) > byte_length:
        raise ValueError(f"prefix is longer than requested byte length: {len(encoded)} > {byte_length}")
    return prefix + fill * (byte_length - len(encoded))


def generate_logs(args: argparse.Namespace) -> dict[str, Any]:
    output_dir = args.output_dir.resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    flood_path = output_dir / "observability-flood.jsonl"
    oversized_path = output_dir / "runtime-oversized-line.log"

    def events() -> Iterator[str]:
        for index in range(args.lines):
            prefix = f"event={index:08d} "
            message = fixed_ascii_payload(prefix, args.message_bytes, chr(97 + index % 26))
            event = {
                "classification": LOG_CATEGORIES[index % len(LOG_CATEGORIES)],
                "event_type": "fixture.log.flood",
                "level": LOG_LEVELS[index % len(LOG_LEVELS)],
                "message": message,
                "operation_id": f"fixture-op-{index // 10:08d}",
                "seq": index + 1,
                "timestamp": utc_text(milliseconds=index),
            }
            yield compact_json(event)

    atomic_write_lines(flood_path, events(), force=args.force)
    oversized = fixed_ascii_payload("mihomo fixture oversized line ", args.oversized_bytes, "Z")
    atomic_write_text(oversized_path, oversized + "\n", force=args.force)
    payload = {
        "fixture": "logs",
        "generator_version": GENERATOR_VERSION,
        "line_count": args.lines,
        "message_bytes": args.message_bytes,
        "oversized_line_bytes_excluding_newline": args.oversized_bytes,
        "artifacts": [
            artifact(flood_path, output_dir, records=args.lines),
            artifact(oversized_path, output_dir, records=1),
        ],
    }
    manifest_path = output_dir / "logs.manifest.json"
    write_manifest(manifest_path, payload, force=args.force)
    payload["manifest"] = manifest_path.name
    return payload


def lifecycle_events(clients: int, rounds: int, seed: int) -> Iterator[str]:
    for round_index in range(rounds):
        for client_index in range(clients):
            operation_index = round_index * clients + client_index
            action = LIFECYCLE_ACTIONS[(round_index + client_index + seed) % len(LIFECYCLE_ACTIONS)]
            event: dict[str, Any] = {
                "action": action,
                "barrier": f"round-{round_index:06d}",
                "client": client_index,
                "deadline_ms": 5000,
                "logical_tick": round_index,
                "operation_id": f"lifecycle-{operation_index:09d}",
                "requested_generation": round_index + 1,
                "round": round_index,
            }
            if operation_index < len(LIFECYCLE_FAULTS):
                event["inject_fault"] = LIFECYCLE_FAULTS[operation_index]
            yield compact_json(event)


def generate_lifecycle(args: argparse.Namespace) -> dict[str, Any]:
    output_dir = args.output_dir.resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    events_path = output_dir / "concurrent-lifecycle.jsonl"
    count = atomic_write_lines(
        events_path,
        lifecycle_events(args.clients, args.rounds, args.seed),
        force=args.force,
    )
    payload = {
        "fixture": "concurrent-lifecycle",
        "generator_version": GENERATOR_VERSION,
        "seed": args.seed,
        "clients": args.clients,
        "rounds": args.rounds,
        "operations": count,
        "faults": list(LIFECYCLE_FAULTS),
        "invariants": [
            "at_most_one_managed_mihomo_process",
            "one_terminal_result_per_operation",
            "stale_generation_exit_cannot_clear_active_generation",
            "stop_cannot_signal_unowned_or_pid_reused_process",
            "external_mihomo_metadata_and_sha256_unchanged",
            "fd_and_goroutine_counts_return_to_budget_after_quiescence",
        ],
        "artifacts": [artifact(events_path, output_dir, records=count)],
    }
    manifest_path = output_dir / "concurrent-lifecycle.manifest.json"
    write_manifest(manifest_path, payload, force=args.force)
    payload["manifest"] = manifest_path.name
    return payload


def generate_all(args: argparse.Namespace) -> dict[str, Any]:
    root = args.output_dir.resolve()
    root.mkdir(parents=True, exist_ok=True)
    results: list[dict[str, Any]] = []
    for profile in ("v0", "v1", "v2", "legacy-v10", "future-v99"):
        results.append(generate_legacy(argparse.Namespace(
            output_dir=root / "legacy",
            backend="all",
            profile=profile,
            nodes=args.legacy_nodes,
            seed=args.seed,
            force=args.force,
        )))
    results.append(generate_nodes(argparse.Namespace(
        output=root / "nodes" / f"nodes-{args.nodes}.jsonl",
        format="jsonl",
        count=args.nodes,
        seed=args.seed,
        force=args.force,
    )))
    results.append(generate_logs(argparse.Namespace(
        output_dir=root / "logs",
        lines=args.log_lines,
        message_bytes=args.log_message_bytes,
        oversized_bytes=args.oversized_bytes,
        force=args.force,
    )))
    results.append(generate_lifecycle(argparse.Namespace(
        output_dir=root / "lifecycle",
        clients=args.lifecycle_clients,
        rounds=args.lifecycle_rounds,
        seed=args.seed,
        force=args.force,
    )))
    payload = {
        "fixture": "phase0-baseline-suite",
        "generator_version": GENERATOR_VERSION,
        "seed": args.seed,
        "parameters": {
            "nodes": args.nodes,
            "legacy_nodes": args.legacy_nodes,
            "log_lines": args.log_lines,
            "log_message_bytes": args.log_message_bytes,
            "oversized_bytes": args.oversized_bytes,
            "lifecycle_clients": args.lifecycle_clients,
            "lifecycle_rounds": args.lifecycle_rounds,
        },
        "results": results,
    }
    manifest_path = root / "phase0-baseline-suite.manifest.json"
    write_manifest(manifest_path, payload, force=args.force)
    payload["manifest"] = manifest_path.name
    return payload


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("value must be greater than zero")
    return parsed


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    legacy = subparsers.add_parser("legacy-db", help="generate legacy SQLite/PostgreSQL schema and seed fixtures")
    legacy.add_argument("--output-dir", type=Path, required=True)
    legacy.add_argument("--backend", choices=("sqlite", "postgres", "all"), default="all")
    legacy.add_argument("--profile", choices=tuple(LEGACY_PROFILES), default="v2")
    legacy.add_argument("--nodes", type=positive_int, default=256)
    legacy.add_argument("--seed", type=int, default=DEFAULT_SEED)
    legacy.add_argument("--force", action="store_true")
    legacy.set_defaults(handler=generate_legacy)

    nodes = subparsers.add_parser("nodes", help="generate a deterministic high-cardinality node stream")
    nodes.add_argument("--output", type=Path, required=True)
    nodes.add_argument("--format", choices=("jsonl", "clash-yaml"), default="jsonl")
    nodes.add_argument("--count", type=positive_int, default=10000)
    nodes.add_argument("--seed", type=int, default=DEFAULT_SEED)
    nodes.add_argument("--force", action="store_true")
    nodes.set_defaults(handler=generate_nodes)

    logs = subparsers.add_parser("logs", help="generate bounded flood and oversized-line log fixtures")
    logs.add_argument("--output-dir", type=Path, required=True)
    logs.add_argument("--lines", type=positive_int, default=10000)
    logs.add_argument("--message-bytes", type=positive_int, default=512)
    logs.add_argument("--oversized-bytes", type=positive_int, default=128 * 1024)
    logs.add_argument("--force", action="store_true")
    logs.set_defaults(handler=generate_logs)

    lifecycle = subparsers.add_parser("lifecycle", help="generate barrier-grouped concurrent lifecycle operations")
    lifecycle.add_argument("--output-dir", type=Path, required=True)
    lifecycle.add_argument("--clients", type=positive_int, default=100)
    lifecycle.add_argument("--rounds", type=positive_int, default=1000)
    lifecycle.add_argument("--seed", type=int, default=DEFAULT_SEED)
    lifecycle.add_argument("--force", action="store_true")
    lifecycle.set_defaults(handler=generate_lifecycle)

    all_fixtures = subparsers.add_parser("all", help="generate the Phase 0 baseline fixture suite")
    all_fixtures.add_argument("--output-dir", type=Path, required=True)
    all_fixtures.add_argument("--nodes", type=positive_int, default=10000)
    all_fixtures.add_argument("--legacy-nodes", type=positive_int, default=256)
    all_fixtures.add_argument("--log-lines", type=positive_int, default=10000)
    all_fixtures.add_argument("--log-message-bytes", type=positive_int, default=512)
    all_fixtures.add_argument("--oversized-bytes", type=positive_int, default=128 * 1024)
    all_fixtures.add_argument("--lifecycle-clients", type=positive_int, default=100)
    all_fixtures.add_argument("--lifecycle-rounds", type=positive_int, default=10)
    all_fixtures.add_argument("--seed", type=int, default=DEFAULT_SEED)
    all_fixtures.add_argument("--force", action="store_true")
    all_fixtures.set_defaults(handler=generate_all)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        result = args.handler(args)
    except (FileExistsError, OSError, sqlite3.Error, ValueError) as error:
        parser.exit(2, f"fixture generation failed: {error}\n")
    print(json.dumps(result, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
