# Phase 0 baseline results

`scripts/audit/run_phase0_baseline.py` writes one immutable subdirectory per run ID here. Each run contains `run-manifest.json`, `metrics.jsonl`, bounded PoolNeXt/Mihomo logs, and before/after snapshots of the external Mihomo source.

Do not hand-edit generated evidence or reuse a run ID. Generate final repository evidence only after the integrated Phase 0 build is fixed and reproducible; exploratory runs should use a temporary `--output-root`.
