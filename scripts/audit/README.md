# Phase 0 audit tools

## Immutable Mihomo guard

`mihomo_guard.py` opens the target read-only, hashes through its file descriptor, and records device, inode, mode, nanosecond mtime, size, and SHA-256. It never executes, writes, chmods, renames, moves, or removes the target.

```bash
python3 scripts/audit/mihomo_guard.py snapshot \
  --path /opt/clash-party/resources/sidecar/mihomo \
  --output /tmp/poolnext-run/mihomo-before.json

# Run tests here.

python3 scripts/audit/mihomo_guard.py snapshot \
  --path /opt/clash-party/resources/sidecar/mihomo \
  --output /tmp/poolnext-run/mihomo-after.json

python3 scripts/audit/mihomo_guard.py compare \
  --before /tmp/poolnext-run/mihomo-before.json \
  --after /tmp/poolnext-run/mihomo-after.json
```

`compare` exits `0` only when path, device, inode, mode, mtime, size, and SHA-256 are unchanged. `verify --snapshot before.json` compares a saved snapshot directly with the current target.

## Baseline collector

`collect_baseline.py` appends records conforming to `tests/baseline/metric-record.schema.json`:

```bash
python3 scripts/audit/collect_baseline.py api \
  --output /tmp/poolnext-run/metrics.jsonl \
  --run-id p0-api-50638db \
  --scenario runtime-status \
  --url http://127.0.0.1:17778/api/runtime \
  --requests 100

python3 scripts/audit/collect_baseline.py disk \
  --output /tmp/poolnext-run/metrics.jsonl \
  --run-id p0-api-50638db \
  --scenario runtime-status \
  --path /tmp/poolnext-data
```

API records retain only the URL path by default, never the query string. Authentication headers can be supplied for a local test, but header values are not written to output. A JSON response with HTTP `200` and `success: false` counts as an API error.

JSONL appenders reject symlinks in the output path, reject non-finite numeric values, and coordinate concurrent Python/Node writers through an exclusive sibling lock file. A stale `.lock` is treated as an error rather than silently removed.

## Frontend DOM collector

After installing the locked `server/web` dependencies and Playwright Chromium:

```bash
node scripts/audit/capture_dom.mjs \
  --url http://127.0.0.1:17778/nodes \
  --run-id p6-nodes-10k \
  --scenario nodes-10k-desktop \
  --viewport 1440x900 \
  --ready-selector 'main' \
  --authenticated-selector 'aside' \
  --row-selector '[data-virtual-row]' \
  --output /tmp/poolnext-run/metrics.jsonl
```

Use `--storage-state` for an authenticated Playwright state file. Protected routes require an authenticated selector (`aside` by default); `--ready-selector` must identify content that proves the target view finished rendering. The collector rejects redirects to another origin/path, labels the route from `page.url()`, and records a query-free final URL. It records total DOM elements, rendered rows, and Chromium JS heap when available.

## Real process run manifest

Build a production/embedded PoolNeXt binary first, generate a fixture suite, then run both PoolNeXt and a temporary Mihomo copy:

```bash
python3 tests/fixtures/generate.py all \
  --output-dir /tmp/poolnext-phase0-fixtures

python3 scripts/audit/run_phase0_baseline.py \
  --output-root tests/baseline/results \
  --run-id phase0-$(git rev-parse --short HEAD) \
  --scenario phase0-real-process-smoke \
  --fixture-manifest /tmp/poolnext-phase0-fixtures/phase0-baseline-suite.manifest.json \
  --poolnext-binary /tmp/poolnext-server \
  --mihomo-binary /opt/clash-party/resources/sidecar/mihomo
```

The runner snapshots the external Mihomo file, copies bytes into a mode `0700` temporary file, validates and runs only that copy, starts PoolNeXt with a random strong session secret and disposable data paths, collects API/process/disk metrics, caps each child log at 4 MiB, stops both processes, verifies the external file again, and writes `run-manifest.json` plus JSONL metrics. Existing run IDs are never overwritten.

For a real disposable PostgreSQL schema, set `--postgres-dsn` and optionally `--postgres-profile v1`. The runner creates a unique schema, loads the generated PostgreSQL fixture when requested, scopes PoolNeXt to that schema, then drops it unless `--keep-postgres-schema` is set. The supplied database role must be allowed to create and drop schemas.

## Self-test

```bash
scripts/audit/selftest_phase0_baseline.sh
```

Set `PHASE0_FULL=1` to generate the 100-client, 1000-round lifecycle stream during the self-test. Set `MIHOMO_BINARY` only when validating another read-only test target. Set `POOLNEXT_BINARY` to an embedded production binary to add the real-process runner; `POOLNEXT_TEST_POSTGRES_DSN` and optional `PHASE0_POSTGRES_PROFILE` add the disposable PostgreSQL path.
