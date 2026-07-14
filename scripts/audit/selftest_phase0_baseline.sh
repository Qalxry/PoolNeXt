#!/usr/bin/env bash

set -u -o pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd)
PYTHON=${PYTHON:-python3}
MIHOMO_BINARY=${MIHOMO_BINARY:-/opt/clash-party/resources/sidecar/mihomo}
POOLNEXT_BINARY=${POOLNEXT_BINARY:-}
TEMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/poolnext-phase0-selftest.XXXXXX")
BEFORE="$TEMP_ROOT/mihomo-before.json"
AFTER="$TEMP_ROOT/mihomo-after.json"
STATUS=0
SNAPSHOT_TAKEN=0

cleanup() {
  rm -rf -- "$TEMP_ROOT"
}
trap cleanup EXIT

cd -- "$REPO_ROOT" || exit 2

if [[ -f "$MIHOMO_BINARY" ]]; then
  "$PYTHON" scripts/audit/mihomo_guard.py snapshot \
    --path "$MIHOMO_BINARY" \
    --output "$BEFORE" >"$TEMP_ROOT/mihomo-before.stdout" || STATUS=$?
  if [[ $STATUS -eq 0 ]]; then
    SNAPSHOT_TAKEN=1
  fi
else
  printf 'selftest: external Mihomo target not present; immutable-target check skipped: %s\n' "$MIHOMO_BINARY" >&2
fi

if [[ $STATUS -eq 0 ]]; then
  "$PYTHON" -m unittest discover \
    -s tests/integration \
    -p 'test_phase0_baseline_tools.py' \
    -v || STATUS=$?
fi

if [[ $STATUS -eq 0 ]]; then
  LIFECYCLE_ROUNDS=10
  if [[ ${PHASE0_FULL:-0} == 1 ]]; then
    LIFECYCLE_ROUNDS=1000
  fi
  "$PYTHON" tests/fixtures/generate.py all \
    --output-dir "$TEMP_ROOT/generated" \
    --nodes 10000 \
    --legacy-nodes 256 \
    --log-lines 10000 \
    --lifecycle-clients 100 \
    --lifecycle-rounds "$LIFECYCLE_ROUNDS" \
    >"$TEMP_ROOT/generator.stdout" || STATUS=$?
fi

if [[ $STATUS -eq 0 ]]; then
  "$PYTHON" scripts/audit/collect_baseline.py process \
    --output "$TEMP_ROOT/metrics.jsonl" \
    --run-id phase0-selftest \
    --scenario audit-tools \
    --pid "$$" \
    --process-name selftest-shell \
    --goroutines 0 || STATUS=$?
fi

if [[ $STATUS -eq 0 ]]; then
  "$PYTHON" scripts/audit/collect_baseline.py disk \
    --output "$TEMP_ROOT/metrics.jsonl" \
    --run-id phase0-selftest \
    --scenario audit-tools \
    --path "$TEMP_ROOT/generated" || STATUS=$?
fi

if [[ $STATUS -eq 0 ]]; then
  "$PYTHON" scripts/audit/collect_baseline.py sql \
    --output "$TEMP_ROOT/metrics.jsonl" \
    --run-id phase0-selftest \
    --scenario audit-tools \
    --operation 'fixture query logger' \
    --query-count 1 \
    --duration-ms 0.1 \
    --rows 1 || STATUS=$?
fi

if [[ $STATUS -eq 0 ]] && command -v node >/dev/null 2>&1; then
  node --check scripts/audit/capture_dom.mjs || STATUS=$?
fi

if [[ $STATUS -eq 0 && -n $POOLNEXT_BINARY ]]; then
  RUNNER_ARGS=(
    --output-root "$TEMP_ROOT/results"
    --run-id phase0-real-selftest
    --scenario phase0-real-process-smoke
    --fixture-manifest "$TEMP_ROOT/generated/phase0-baseline-suite.manifest.json"
    --poolnext-binary "$POOLNEXT_BINARY"
    --mihomo-binary "$MIHOMO_BINARY"
    --api-requests 3
  )
  if [[ -n ${POOLNEXT_TEST_POSTGRES_DSN:-} ]]; then
    RUNNER_ARGS+=(
      --postgres-dsn "$POOLNEXT_TEST_POSTGRES_DSN"
      --postgres-profile "${PHASE0_POSTGRES_PROFILE:-v1}"
    )
  fi
  "$PYTHON" scripts/audit/run_phase0_baseline.py "${RUNNER_ARGS[@]}" || STATUS=$?
fi

if [[ $SNAPSHOT_TAKEN -eq 1 ]]; then
  "$PYTHON" scripts/audit/mihomo_guard.py snapshot \
    --path "$MIHOMO_BINARY" \
    --output "$AFTER" >"$TEMP_ROOT/mihomo-after.stdout" || STATUS=$?
  if [[ -f "$AFTER" ]]; then
    "$PYTHON" scripts/audit/mihomo_guard.py compare \
      --before "$BEFORE" \
      --after "$AFTER" >"$TEMP_ROOT/mihomo-compare.stdout" || STATUS=$?
  fi
fi

if [[ $STATUS -eq 0 ]]; then
  printf 'Phase 0 baseline self-test passed. Generated artifacts were disposable.\n'
else
  printf 'Phase 0 baseline self-test failed with status %d.\n' "$STATUS" >&2
fi

exit "$STATUS"
