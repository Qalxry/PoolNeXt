# Phase 0 deterministic fixtures

`generate.py` creates disposable baseline inputs without committing large databases, node lists, or log files. It uses only Python's standard library and a fixed default seed (`20260714`). Identical arguments produce identical text fixtures.

## Legacy database profiles

The database generator keeps schema shape and schema version separate so migration tests can cover the real PoolX history:

| Profile | Schema-version row | Schema family | Purpose |
|---|---:|---|---|
| `unversioned` | table absent | PoolX V1 core tables | Non-empty database created before schema metadata existed |
| `v0` | 0 | PoolX V1 core tables | Initializer/version-traversal input using the oldest evidenced shape |
| `v1` | 1 | PoolX V1 core tables | Historical V1 reconstructed from `cf2bc82`; `app_logs` has no `level`, and proxy-pool tables do not exist |
| `v2` | 2 | PoolNeXt V2 | Fork baseline at `50638db`, including subscription columns and `proxy_settings_json` |
| `legacy-v10` | 10 | PoolX V10 | Pre-reset PoolX schema at `5b85027`, with separate strategy/test/load-balance columns |
| `future-v99` | 99 | PoolNeXt V2 shape | Negative control: the application must reject an unknown future schema |

Each run emits SQLite SQL, a generated SQLite database, PostgreSQL SQL, and a SHA-256 manifest. PostgreSQL SQL must be loaded only into an empty disposable database.

```bash
python3 tests/fixtures/generate.py legacy-db \
  --output-dir /tmp/poolnext-fixtures/legacy \
  --backend all \
  --profile v2 \
  --nodes 10000

psql "$POOLNEXT_TEST_POSTGRES_DSN" -v ON_ERROR_STOP=1 \
  -f /tmp/poolnext-fixtures/legacy/legacy-v2.postgres.sql
```

The schema snapshot is a migration input contract, not a replacement for production migrations. The manifest records the evidence commit and schema family. Tests should assert preservation and target-schema invariants after the application migrates a generated copy. `--nodes` is intentionally ignored for the PoolX V1 family because those tables did not exist yet; the manifest records both requested and generated counts.

## High-cardinality nodes

The default node stream contains 10,000 unique deterministic records across several protocol, region, enabled, and health states. Addresses use the benchmark-only `198.18.0.0/15` range.

```bash
python3 tests/fixtures/generate.py nodes \
  --output /tmp/poolnext-fixtures/nodes-10000.jsonl \
  --count 10000
```

`--format clash-yaml` is available for parser/render smoke inputs. JSONL is the stable source for database, API, and UI performance fixtures.

## Logs and lifecycle

```bash
python3 tests/fixtures/generate.py logs \
  --output-dir /tmp/poolnext-fixtures/logs \
  --lines 10000 \
  --message-bytes 512 \
  --oversized-bytes 131072

python3 tests/fixtures/generate.py lifecycle \
  --output-dir /tmp/poolnext-fixtures/lifecycle \
  --clients 100 \
  --rounds 1000
```

Lifecycle operations are grouped by a shared round barrier. The manifest records required invariants and deterministic fault injections for stale generations, port conflict, controller timeout, a 128 KiB runtime line, SIGTERM, and process crash.

Generate the normal Phase 0 suite with:

```bash
python3 tests/fixtures/generate.py all --output-dir /tmp/poolnext-phase0
```

Generated artifacts should stay under `/tmp`, CI artifact storage, or another disposable test directory. Do not add generated `.db`, 10k-node, flood-log, or lifecycle outputs to Git.
