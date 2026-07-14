# Baseline record contract

Performance and resource evidence is append-only JSONL. Every line follows `metric-record.schema.json` and stores one scalar, which makes runs diffable and allows later phases to add collectors without changing existing records.

Required identity fields are `run_id`, `scenario`, `captured_at`, and optional `git_commit`. Dimensions carry stable labels such as API route, HTTP method, SQL operation, process name, disk path, viewport, or statistic (`p50`, `p95`, `p99`, `max`). Never put authorization values, cookies, URL queries, node credentials, or subscription tokens in dimensions.

Covered metric families:

| Family | Metrics |
|---|---|
| API | request/error counts, latency, response bytes |
| SQL | query count, latency, affected/returned rows |
| Process | RSS, FD count, Go goroutine count |
| Disk | logical bytes, allocated bytes, file count |
| Frontend | total DOM nodes, rendered rows, JS heap bytes |

Example:

```json
{"schema_version":"poolnext.baseline.metric/v1","run_id":"p0-runtime-50638db","scenario":"runtime-status-20-profiles","captured_at":"2026-07-14T08:00:00.000Z","git_commit":"50638db","metric":"sql.queries","value":62,"unit":"count","sample_count":1,"dimensions":{"operation":"GET /api/runtime"}}
```

`run-manifest.schema.json` links a completed run to its fixture manifest, metrics JSONL, Git commit, bounded child logs, PoolNeXt status, temporary-copy Mihomo controller/version evidence, result, and the required external Mihomo before/after snapshots. Use a new run ID when the commit, environment, fixture parameters, or scenario changes; never rewrite old measurements in place.

The Phase 0 collector can measure API/process/disk directly and record SQL/DOM values supplied by their harnesses:

```bash
python3 scripts/audit/collect_baseline.py process \
  --output /tmp/run/metrics.jsonl \
  --run-id p0-runtime-50638db \
  --scenario runtime-idle \
  --git-commit 50638db \
  --pid "$POOLNEXT_PID" \
  --goroutine-url http://127.0.0.1:6060/debug/pprof/goroutine?debug=1
```

Product-code cooperation still required in later tasks:

- SQL query counts need a test-only GORM logger or equivalent query instrumentation.
- Go goroutine counts need an explicitly test-scoped pprof endpoint, or a count supplied by the test harness.
- Frontend heap measurements require Chromium; `performance.memory` may be unavailable in non-Chromium engines.
- Budget pass/fail thresholds belong to the relevant phase acceptance test. This format preserves raw evidence and does not silently redefine budgets.
