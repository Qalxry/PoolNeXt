# Phase 0 baseline integration tests

Run the fixture, collector, schema, and immutable-kernel guard tests with:

```bash
python3 -m unittest discover -s tests/integration -p 'test_phase0_baseline_tools.py' -v
```

The default suite uses executable local stubs to prove the process runner starts PoolNeXt, executes only a temporary Mihomo copy, emits metrics/manifests, and leaves the source inode/mode/mtime/hash unchanged. It also validates deterministic fixture generation, historical V1/V10 schema fields, SQLite integrity, PostgreSQL seed shape, 10k-node uniqueness, exact oversized-line size, concurrent lifecycle scheduling, HTTP-200 business error accounting, cross-process JSONL writes, DOM final-URL checks, and guard behavior.

Set `POOLNEXT_BINARY` when running `scripts/audit/selftest_phase0_baseline.sh` to exercise a real embedded PoolNeXt binary and the configured read-only external Mihomo source. Optional `POOLNEXT_TEST_POSTGRES_DSN` uses a unique disposable schema. Full product lifecycle, authenticated browser, SQL query-count, and resource-budget assertions still belong to later phase-specific integration tests.
