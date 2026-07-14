#!/usr/bin/env python3
"""Integration tests for the Phase 0 fixture and audit tooling."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import textwrap
import threading
import unittest
from concurrent.futures import ThreadPoolExecutor
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Sequence


REPO_ROOT = Path(__file__).resolve().parents[2]
GENERATOR = REPO_ROOT / "tests" / "fixtures" / "generate.py"
COLLECTOR = REPO_ROOT / "scripts" / "audit" / "collect_baseline.py"
DOM_COLLECTOR = REPO_ROOT / "scripts" / "audit" / "capture_dom.mjs"
GUARD = REPO_ROOT / "scripts" / "audit" / "mihomo_guard.py"
RUNNER = REPO_ROOT / "scripts" / "audit" / "run_phase0_baseline.py"
METRIC_SCHEMA = REPO_ROOT / "tests" / "baseline" / "metric-record.schema.json"
RUN_SCHEMA = REPO_ROOT / "tests" / "baseline" / "run-manifest.schema.json"
NODE = shutil.which("node")
REAL_MIHOMO = os.getenv("POOLNEXT_REAL_MIHOMO", "")


def run(command: Sequence[str], *, expected: int = 0) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        list(command),
        cwd=REPO_ROOT,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if result.returncode != expected:
        raise AssertionError(
            f"command returned {result.returncode}, expected {expected}: {' '.join(command)}\n"
            f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}"
        )
    return result


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_executable(path: Path, content: str) -> None:
    path.write_text(textwrap.dedent(content).lstrip(), encoding="utf-8")
    path.chmod(0o700)


class FixtureGeneratorTests(unittest.TestCase):
    def test_ten_thousand_nodes_are_deterministic(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            first = root / "first.jsonl"
            second = root / "second.jsonl"
            base = [sys.executable, str(GENERATOR), "nodes", "--count", "10000"]
            run([*base, "--output", str(first)])
            run([*base, "--output", str(second)])
            self.assertEqual(file_sha256(first), file_sha256(second))
            with first.open("r", encoding="utf-8") as handle:
                rows = [json.loads(line) for line in handle]
            self.assertEqual(len(rows), 10000)
            self.assertEqual(rows[0]["id"], 1)
            self.assertEqual(rows[-1]["id"], 10000)
            self.assertEqual(len({row["fingerprint"] for row in rows}), 10000)

    def test_legacy_sqlite_and_postgres_profiles(self) -> None:
        expectations = {
            "unversioned": (None, "poolx-v1", False),
            "v0": (0, "poolx-v1", True),
            "v1": (1, "poolx-v1", True),
            "v2": (2, "poolnext-v2", True),
            "legacy-v10": (10, "poolx-v10", True),
            "future-v99": (99, "poolnext-v2", True),
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for profile, (version, schema_family, has_version_table) in expectations.items():
                output = root / profile
                run([
                    sys.executable,
                    str(GENERATOR),
                    "legacy-db",
                    "--output-dir",
                    str(output),
                    "--backend",
                    "all",
                    "--profile",
                    profile,
                    "--nodes",
                    "120",
                ])
                database = output / f"legacy-{profile}.sqlite.db"
                connection = sqlite3.connect(database)
                try:
                    self.assertEqual(connection.execute("PRAGMA integrity_check").fetchone(), ("ok",))
                    tables = {
                        row[0]
                        for row in connection.execute(
                            "SELECT name FROM sqlite_master WHERE type = 'table'"
                        ).fetchall()
                    }
                    app_log_columns = {
                        row[1] for row in connection.execute("PRAGMA table_info(app_logs)").fetchall()
                    }
                    if schema_family == "poolx-v1":
                        self.assertNotIn("level", app_log_columns)
                        self.assertNotIn("source_configs", tables)
                        self.assertNotIn("proxy_nodes", tables)
                    else:
                        self.assertIn("level", app_log_columns)
                        self.assertEqual(
                            connection.execute("SELECT COUNT(*) FROM proxy_nodes").fetchone(),
                            (120,),
                        )
                        source_columns = {
                            row[1]
                            for row in connection.execute(
                                "PRAGMA table_info(source_configs)"
                            ).fetchall()
                        }
                        self.assertEqual(
                            "source_type" in source_columns,
                            schema_family == "poolnext-v2",
                        )
                        profile_columns = {
                            row[1]
                            for row in connection.execute(
                                "PRAGMA table_info(port_profiles)"
                            ).fetchall()
                        }
                        if schema_family == "poolx-v10":
                            self.assertNotIn("proxy_settings_json", profile_columns)
                            self.assertTrue({
                                "strategy_type",
                                "strategy_group_name",
                                "test_url",
                                "test_interval_seconds",
                                "load_balance_strategy",
                                "load_balance_lazy",
                                "load_balance_disable_udp",
                            } <= profile_columns)
                        else:
                            self.assertIn("proxy_settings_json", profile_columns)
                            self.assertNotIn("strategy_type", profile_columns)
                    version_table = connection.execute(
                        "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'database_schema_versions'"
                    ).fetchone()
                    self.assertEqual(version_table, (1 if has_version_table else 0,))
                    if has_version_table:
                        version_rows = connection.execute(
                            "SELECT version FROM database_schema_versions WHERE id = 1"
                        ).fetchall()
                        self.assertEqual(version_rows, [(version,)])
                    self.assertEqual(connection.execute(
                        "SELECT value FROM options WHERE key = 'FixtureSchemaFamily'"
                    ).fetchone(), (schema_family,))
                finally:
                    connection.close()
                postgres_sql = (output / f"legacy-{profile}.postgres.sql").read_text(encoding="utf-8")
                if schema_family == "poolx-v1":
                    self.assertNotIn('CREATE TABLE "proxy_nodes"', postgres_sql)
                    self.assertNotIn('"level"', postgres_sql)
                else:
                    self.assertIn('CREATE TABLE "proxy_nodes"', postgres_sql)
                    self.assertIn("pg_get_serial_sequence('proxy_nodes', 'id')", postgres_sql)
                if schema_family == "poolx-v10":
                    self.assertIn('"load_balance_strategy"', postgres_sql)
                    self.assertNotIn('"proxy_settings_json"', postgres_sql)
                manifest = json.loads((output / f"legacy-{profile}.manifest.json").read_text(encoding="utf-8"))
                self.assertEqual(manifest["profile"], profile)
                self.assertEqual(manifest["schema_family"], schema_family)
                self.assertEqual(manifest["node_count"], 0 if schema_family == "poolx-v1" else 120)

    def test_long_log_and_oversized_line_contract(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            run([
                sys.executable,
                str(GENERATOR),
                "logs",
                "--output-dir",
                str(output),
                "--lines",
                "37",
                "--message-bytes",
                "256",
                "--oversized-bytes",
                str(128 * 1024),
            ])
            flood = (output / "observability-flood.jsonl").read_text(encoding="utf-8").splitlines()
            self.assertEqual(len(flood), 37)
            self.assertTrue(all(len(json.loads(line)["message"].encode("ascii")) == 256 for line in flood))
            oversized = (output / "runtime-oversized-line.log").read_bytes()
            self.assertTrue(oversized.endswith(b"\n"))
            self.assertEqual(len(oversized) - 1, 128 * 1024)

    def test_concurrent_lifecycle_schedule_is_barrier_grouped(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            run([
                sys.executable,
                str(GENERATOR),
                "lifecycle",
                "--output-dir",
                str(output),
                "--clients",
                "100",
                "--rounds",
                "7",
            ])
            events = [
                json.loads(line)
                for line in (output / "concurrent-lifecycle.jsonl").read_text(encoding="utf-8").splitlines()
            ]
            self.assertEqual(len(events), 700)
            self.assertEqual({event["action"] for event in events}, {"start", "stop", "reload"})
            for round_index in range(7):
                round_events = [event for event in events if event["round"] == round_index]
                self.assertEqual(len(round_events), 100)
                self.assertEqual({event["barrier"] for event in round_events}, {f"round-{round_index:06d}"})
            manifest = json.loads(
                (output / "concurrent-lifecycle.manifest.json").read_text(encoding="utf-8")
            )
            self.assertIn("stale_generation_exit_cannot_clear_active_generation", manifest["invariants"])

    def test_suite_manifest_is_independent_of_output_location(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            arguments = [
                "all", "--nodes", "25", "--legacy-nodes", "10", "--log-lines", "5",
                "--lifecycle-clients", "3", "--lifecycle-rounds", "2",
            ]
            for name in ("first", "second"):
                run([
                    sys.executable, str(GENERATOR), *arguments, "--output-dir", str(root / name),
                ])
            first = root / "first" / "phase0-baseline-suite.manifest.json"
            second = root / "second" / "phase0-baseline-suite.manifest.json"
            self.assertEqual(first.read_bytes(), second.read_bytes())
            self.assertNotIn(temporary, first.read_text(encoding="utf-8"))


class BaselineCollectorTests(unittest.TestCase):
    def test_metric_schema_names_cover_required_baselines(self) -> None:
        schema = json.loads(METRIC_SCHEMA.read_text(encoding="utf-8"))
        run_schema = json.loads(RUN_SCHEMA.read_text(encoding="utf-8"))
        metrics = set(schema["properties"]["metric"]["enum"])
        required = {
            "api.requests",
            "api.latency_ms",
            "sql.queries",
            "process.rss_bytes",
            "process.fd_count",
            "process.goroutine_count",
            "disk.logical_bytes",
            "frontend.dom_nodes",
        }
        self.assertTrue(required <= metrics)
        self.assertTrue({
            "fixture_manifest_sha256", "mihomo_before_snapshot", "mihomo_after_snapshot", "result",
        } <= set(run_schema["required"]))
        self.assertTrue({"poolnext", "mihomo"} <= set(run_schema["properties"]))

    def test_process_disk_sql_and_dom_records_share_one_contract(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            metrics = root / "metrics.jsonl"
            (root / "data").mkdir()
            (root / "data" / "one.bin").write_bytes(b"x" * 128)
            common = [
                "--output", str(metrics), "--run-id", "fixture-run", "--scenario", "collector-selftest",
            ]
            run([
                sys.executable, str(COLLECTOR), "process", *common,
                "--pid", str(os.getpid()), "--process-name", "unittest", "--goroutines", "7",
            ])
            run([
                sys.executable, str(COLLECTOR), "disk", *common, "--path", str(root / "data"),
            ])
            run([
                sys.executable, str(COLLECTOR), "sql", *common,
                "--operation", "GET /api/runtime", "--query-count", "62", "--duration-ms", "12.5", "--rows", "20",
            ])
            run([
                sys.executable, str(COLLECTOR), "dom", *common,
                "--route", "/nodes", "--viewport", "1440x900", "--dom-nodes", "780",
                "--rendered-rows", "82", "--heap-bytes", "1048576",
            ])
            records = [json.loads(line) for line in metrics.read_text(encoding="utf-8").splitlines()]
            names = {record["metric"] for record in records}
            self.assertTrue({
                "process.rss_bytes", "process.fd_count", "process.goroutine_count",
                "disk.logical_bytes", "disk.allocated_bytes", "disk.file_count",
                "sql.queries", "sql.latency_ms", "sql.rows",
                "frontend.dom_nodes", "frontend.rendered_rows", "frontend.heap_bytes",
            } <= names)
            for item in records:
                self.assertEqual(item["schema_version"], "poolnext.baseline.metric/v1")
                self.assertEqual(item["run_id"], "fixture-run")
                self.assertGreaterEqual(item["sample_count"], 1)

    def test_api_collector_records_status_latency_and_bytes(self) -> None:
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:  # noqa: N802 - stdlib callback name
                payload = b'{"status":"ok"}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, _format: str, *_args: object) -> None:
                return

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as temporary:
                output = Path(temporary) / "api.jsonl"
                run([
                    sys.executable,
                    str(COLLECTOR),
                    "api",
                    "--output",
                    str(output),
                    "--run-id",
                    "api-run",
                    "--scenario",
                    "api-selftest",
                    "--url",
                    f"http://127.0.0.1:{server.server_port}/api/status?secret=not-recorded",
                    "--requests",
                    "5",
                ])
                records = [json.loads(line) for line in output.read_text(encoding="utf-8").splitlines()]
                self.assertIn("api.latency_ms", {record["metric"] for record in records})
                self.assertTrue(all(record["dimensions"]["route"] == "/api/status" for record in records))
                self.assertNotIn("secret", output.read_text(encoding="utf-8"))
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)

    def test_api_collector_counts_http_200_business_failures(self) -> None:
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:  # noqa: N802 - stdlib callback name
                payload = b'{"success":false,"message":"fixture failure"}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, _format: str, *_args: object) -> None:
                return

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as temporary:
                output = Path(temporary) / "business-errors.jsonl"
                run([
                    sys.executable,
                    str(COLLECTOR),
                    "api",
                    "--output",
                    str(output),
                    "--run-id",
                    "business-error-run",
                    "--scenario",
                    "business-error-selftest",
                    "--url",
                    f"http://127.0.0.1:{server.server_port}/api/failure",
                    "--requests",
                    "4",
                ])
                records = [json.loads(line) for line in output.read_text(encoding="utf-8").splitlines()]
                errors = [record for record in records if record["metric"] == "api.errors"]
                self.assertEqual(len(errors), 1)
                self.assertEqual(errors[0]["value"], 4)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)

    def test_collector_rejects_non_finite_values_and_symlink_outputs(self) -> None:
        common = ["--run-id", "invalid-run", "--scenario", "invalid-values"]
        run([
            sys.executable, str(COLLECTOR), "record", *common,
            "--metric", "api.requests", "--value", "NaN",
        ], expected=2)
        run([
            sys.executable, str(COLLECTOR), "sql", *common,
            "--operation", "fixture", "--query-count", "1", "--duration-ms", "Infinity",
        ], expected=2)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            target = root / "target.jsonl"
            target.write_text("sentinel\n", encoding="utf-8")
            output = root / "metrics.jsonl"
            output.symlink_to(target)
            run([
                sys.executable, str(COLLECTOR), "record", *common,
                "--output", str(output), "--metric", "api.requests", "--value", "1",
            ], expected=2)
            self.assertEqual(target.read_text(encoding="utf-8"), "sentinel\n")

    def test_collector_concurrent_jsonl_appends_do_not_interleave(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "metrics.jsonl"

            def append(index: int) -> None:
                run([
                    sys.executable,
                    str(COLLECTOR),
                    "record",
                    "--output",
                    str(output),
                    "--run-id",
                    f"worker-{index}",
                    "--scenario",
                    "concurrent-jsonl",
                    "--metric",
                    "api.requests",
                    "--value",
                    str(index),
                ])

            with ThreadPoolExecutor(max_workers=12) as executor:
                list(executor.map(append, range(1, 33)))
            records = [json.loads(line) for line in output.read_text(encoding="utf-8").splitlines()]
            self.assertEqual(len(records), 32)
            self.assertEqual({record["run_id"] for record in records}, {
                f"worker-{index}" for index in range(1, 33)
            })
            self.assertFalse((output.parent / f"{output.name}.lock").exists())


@unittest.skipUnless(NODE, "Node.js is required for DOM collector helper tests")
class DOMCollectorTests(unittest.TestCase):
    def test_final_url_helpers_use_page_url_without_query_secrets(self) -> None:
        module_url = json.dumps(DOM_COLLECTOR.as_uri())
        script = f"""
            import {{ assertExpectedFinalUrl, sanitizedPageUrl }} from {module_url};
            assertExpectedFinalUrl(
              'http://127.0.0.1:3000/nodes?session=secret',
              'http://127.0.0.1:3000/nodes',
            );
            if (sanitizedPageUrl('http://127.0.0.1:3000/nodes?session=secret') !==
                'http://127.0.0.1:3000/nodes') process.exit(3);
            let rejected = false;
            try {{
              assertExpectedFinalUrl(
                'http://127.0.0.1:3000/login?redirect=%2Fnodes',
                'http://127.0.0.1:3000/nodes',
              );
            }} catch {{ rejected = true; }}
            if (!rejected) process.exit(4);
        """
        run([NODE, "--input-type=module", "--eval", textwrap.dedent(script)])

    def test_node_jsonl_writer_rejects_symlinks_and_serializes_concurrent_writers(self) -> None:
        module_url = json.dumps(DOM_COLLECTOR.as_uri())
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = root / "dom.jsonl"

            def append(index: int) -> None:
                script = (
                    f"import {{ writeJsonl }} from {module_url};"
                    f"writeJsonl({json.dumps(str(output))}, [{{worker:{index}}}]);"
                )
                run([NODE, "--input-type=module", "--eval", script])

            with ThreadPoolExecutor(max_workers=8) as executor:
                list(executor.map(append, range(20)))
            records = [json.loads(line) for line in output.read_text(encoding="utf-8").splitlines()]
            self.assertEqual({record["worker"] for record in records}, set(range(20)))
            self.assertFalse((root / "dom.jsonl.lock").exists())

            target = root / "target.jsonl"
            target.write_text("sentinel\n", encoding="utf-8")
            symlink = root / "linked.jsonl"
            symlink.symlink_to(target)
            script = f"""
                import {{ writeJsonl }} from {module_url};
                let rejected = false;
                try {{ writeJsonl({json.dumps(str(symlink))}, [{{worker:99}}]); }}
                catch {{ rejected = true; }}
                if (!rejected) process.exit(5);
            """
            run([NODE, "--input-type=module", "--eval", textwrap.dedent(script)])
            self.assertEqual(target.read_text(encoding="utf-8"), "sentinel\n")

            non_finite = f"""
                import {{ writeJsonl }} from {module_url};
                let rejected = false;
                try {{ writeJsonl({json.dumps(str(root / 'nan.jsonl'))}, [{{value:NaN}}]); }}
                catch {{ rejected = true; }}
                if (!rejected) process.exit(6);
            """
            run([NODE, "--input-type=module", "--eval", textwrap.dedent(non_finite)])


class BaselineRunnerTests(unittest.TestCase):
    def test_runner_starts_poolnext_and_only_executes_a_temporary_mihomo_copy(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fake_mihomo = root / "external-mihomo"
            write_executable(fake_mihomo, r'''
                #!/usr/bin/env python3
                import json
                import re
                import sys
                from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
                from pathlib import Path

                config_path = Path(sys.argv[sys.argv.index("-f") + 1])
                config = config_path.read_text(encoding="utf-8")
                if "-t" in sys.argv:
                    raise SystemExit(0)
                address = re.search(r'external-controller:\s*"([^"]+)"', config).group(1)
                secret = re.search(r'secret:\s*"([^"]+)"', config).group(1)
                host, port = address.rsplit(":", 1)

                class Handler(BaseHTTPRequestHandler):
                    def do_GET(self):
                        if self.headers.get("Authorization") != f"Bearer {secret}":
                            self.send_response(401)
                            self.end_headers()
                            return
                        payload = json.dumps({"meta": True, "version": "fixture-mihomo"}).encode()
                        self.send_response(200)
                        self.send_header("Content-Type", "application/json")
                        self.send_header("Content-Length", str(len(payload)))
                        self.end_headers()
                        self.wfile.write(payload)

                    def log_message(self, _format, *_args):
                        return

                ThreadingHTTPServer((host, int(port)), Handler).serve_forever()
            ''')
            fake_poolnext = root / "poolnext-server"
            write_executable(fake_poolnext, r'''
                #!/usr/bin/env python3
                import json
                import os
                from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
                from pathlib import Path

                database = Path(os.environ["SQLITE_PATH"])
                database.parent.mkdir(parents=True, exist_ok=True)
                database.write_bytes(b"fixture sqlite data")

                class Handler(BaseHTTPRequestHandler):
                    def do_GET(self):
                        payload = json.dumps({
                            "success": True,
                            "message": "",
                            "data": {"version": "fixture-poolnext"},
                        }).encode()
                        self.send_response(200)
                        self.send_header("Content-Type", "application/json")
                        self.send_header("Content-Length", str(len(payload)))
                        self.end_headers()
                        self.wfile.write(payload)

                    def log_message(self, _format, *_args):
                        return

                ThreadingHTTPServer(("127.0.0.1", int(os.environ["PORT"])), Handler).serve_forever()
            ''')
            fixture_manifest = root / "fixture-manifest.json"
            fixture_manifest.write_text('{"fixture":"runner-selftest"}\n', encoding="utf-8")
            results = root / "results"
            original_identity = (
                fake_mihomo.stat().st_ino,
                fake_mihomo.stat().st_mode,
                fake_mihomo.stat().st_mtime_ns,
                file_sha256(fake_mihomo),
            )
            completed = run([
                sys.executable,
                str(RUNNER),
                "--output-root",
                str(results),
                "--run-id",
                "runner-selftest",
                "--scenario",
                "runner-process-smoke",
                "--fixture-manifest",
                str(fixture_manifest),
                "--poolnext-binary",
                str(fake_poolnext),
                "--mihomo-binary",
                str(fake_mihomo),
                "--api-requests",
                "3",
                "--startup-timeout",
                "10",
            ])
            self.assertIn('"result": "passed"', completed.stdout)
            run_dir = results / "runner-selftest"
            manifest = json.loads((run_dir / "run-manifest.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["result"], "passed")
            self.assertTrue(manifest["poolnext"]["status_success"])
            self.assertEqual(manifest["mihomo"]["version"]["version"], "fixture-mihomo")
            self.assertEqual(manifest["mihomo"]["source_sha256"], original_identity[3])
            before = json.loads((run_dir / "mihomo-before.json").read_text(encoding="utf-8"))
            after = json.loads((run_dir / "mihomo-after.json").read_text(encoding="utf-8"))
            self.assertEqual(before["sha256"], after["sha256"])
            final_identity = (
                fake_mihomo.stat().st_ino,
                fake_mihomo.stat().st_mode,
                fake_mihomo.stat().st_mtime_ns,
                file_sha256(fake_mihomo),
            )
            self.assertEqual(final_identity, original_identity)
            records = [
                json.loads(line)
                for line in (run_dir / "metrics.jsonl").read_text(encoding="utf-8").splitlines()
            ]
            self.assertTrue({
                "api.requests",
                "api.errors",
                "process.rss_bytes",
                "disk.logical_bytes",
            } <= {record["metric"] for record in records})

    @unittest.skipUnless(
        REAL_MIHOMO and Path(REAL_MIHOMO).is_file(),
        "set POOLNEXT_REAL_MIHOMO to run the real temporary-copy smoke",
    )
    def test_real_mihomo_is_started_only_from_a_temporary_copy(self) -> None:
        source = Path(REAL_MIHOMO).resolve(strict=True)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fake_poolnext = root / "poolnext-server"
            write_executable(fake_poolnext, r'''
                #!/usr/bin/env python3
                import json
                import os
                from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
                from pathlib import Path

                database = Path(os.environ["SQLITE_PATH"])
                database.parent.mkdir(parents=True, exist_ok=True)
                database.write_bytes(b"real-mihomo runner fixture")

                class Handler(BaseHTTPRequestHandler):
                    def do_GET(self):
                        payload = json.dumps({"success": True, "message": "", "data": {}}).encode()
                        self.send_response(200)
                        self.send_header("Content-Type", "application/json")
                        self.send_header("Content-Length", str(len(payload)))
                        self.end_headers()
                        self.wfile.write(payload)

                    def log_message(self, _format, *_args):
                        return

                ThreadingHTTPServer(("127.0.0.1", int(os.environ["PORT"])), Handler).serve_forever()
            ''')
            fixture_manifest = root / "fixture-manifest.json"
            fixture_manifest.write_text('{"fixture":"real-mihomo-copy"}\n', encoding="utf-8")
            source_identity = (
                source.stat().st_ino,
                source.stat().st_mode,
                source.stat().st_mtime_ns,
                file_sha256(source),
            )
            run([
                sys.executable,
                str(RUNNER),
                "--output-root",
                str(root / "results"),
                "--run-id",
                "real-mihomo-copy",
                "--scenario",
                "real-mihomo-copy-smoke",
                "--fixture-manifest",
                str(fixture_manifest),
                "--poolnext-binary",
                str(fake_poolnext),
                "--mihomo-binary",
                str(source),
                "--api-requests",
                "1",
                "--startup-timeout",
                "20",
            ])
            manifest = json.loads(
                (root / "results" / "real-mihomo-copy" / "run-manifest.json").read_text(
                    encoding="utf-8"
                )
            )
            self.assertEqual(manifest["result"], "passed")
            self.assertEqual(manifest["mihomo"]["source_sha256"], source_identity[3])
            final_identity = (
                source.stat().st_ino,
                source.stat().st_mode,
                source.stat().st_mtime_ns,
                file_sha256(source),
            )
            self.assertEqual(final_identity, source_identity)


class MihomoGuardTests(unittest.TestCase):
    def test_snapshot_compare_and_verify_are_read_only(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            target = root / "mihomo-copy"
            target.write_bytes(b"fixture mihomo bytes\n" * 100)
            target.chmod(0o755)
            original_stat = target.stat()
            before = root / "before.json"
            after = root / "after.json"
            base = [sys.executable, str(GUARD), "snapshot", "--path", str(target)]
            run([*base, "--output", str(before)])
            run([*base, "--output", str(after)])
            comparison = run([
                sys.executable, str(GUARD), "compare", "--before", str(before), "--after", str(after),
            ])
            self.assertTrue(json.loads(comparison.stdout)["unchanged"])
            verification = run([sys.executable, str(GUARD), "verify", "--snapshot", str(before)])
            self.assertTrue(json.loads(verification.stdout)["unchanged"])
            final_stat = target.stat()
            self.assertEqual(
                (original_stat.st_ino, original_stat.st_mode, original_stat.st_mtime_ns, original_stat.st_size),
                (final_stat.st_ino, final_stat.st_mode, final_stat.st_mtime_ns, final_stat.st_size),
            )

            unchanged_bytes = target.read_bytes()
            run([*base, "--output", str(target), "--force"], expected=2)
            self.assertEqual(target.read_bytes(), unchanged_bytes)

            target.write_bytes(unchanged_bytes + b"changed")
            changed = root / "changed.json"
            run([*base, "--output", str(changed)])
            mismatch = run([
                sys.executable, str(GUARD), "compare", "--before", str(before), "--after", str(changed),
            ], expected=1)
            report = json.loads(mismatch.stdout)
            self.assertFalse(report["unchanged"])
            self.assertIn("sha256", report["changes"])
            self.assertIn("size", report["changes"])


if __name__ == "__main__":
    unittest.main()
