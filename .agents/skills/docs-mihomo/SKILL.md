---
name: docs-mihomo
description: "Offline, repository-derived Mihomo documentation for configuration, CLI and controller APIs, runtime architecture, routing, protocols, and integration evidence. Use when answering Mihomo questions, drafting or reviewing configs, explaining source behavior, or checking an integration against commit a847246; distinguish mirrored upstream text from AI-authored inferred guides and verify important claims against bundled source capsules."
---

# Mihomo Offline Repository Documentation

Use this skill for source-grounded Mihomo questions without relying on the
hosted wiki. The snapshot comes from `MetaCubeX/mihomo` commit
`a84724665eb7f989809abe463c05f5723bd24975`.

## Route by task

- Product scope and repository map: `references/inferred/overview.md`
- Build, config validation, and startup: `references/inferred/quick-start.md`
- YAML areas, defaults, proxies, rules, DNS, TUN: `references/inferred/configuration.md`
- CLI flags and REST controller families: `references/inferred/api.md`
- Startup/reload and traffic-routing flow: `references/inferred/architecture.md`
- Config and integration-test examples: `references/inferred/examples.md`
- Coverage warnings and unresolved details: `references/inferred/limitations.md`
- Complete generated inventory: `references/INDEX.md`

## Evidence order

1. Prefer `references/original/` for mirrored upstream-authored Markdown.
2. Treat `references/inferred/` as AI-authored orientation, never as upstream text.
3. Verify configuration and architecture claims in `references/source/` and map
   each capsule to its repository path through `references/source-map.json`.
4. Check `references/manifest.json` for the source commit, classification,
   generation limits, and warnings.
5. If evidence is absent or only partial, state the uncertainty and identify
   the repository path that would need inspection; do not invent fields,
   defaults, endpoint schemas, compatibility, or platform behavior.

## High-value evidence

- `references/source/main.md`: flags, environment fallbacks, config inputs, reload.
- `references/source/config/config-core.md`: YAML model, defaults, parse order.
- `references/source/docs/config-example.md`: focused upstream config excerpts.
- `references/source/hub/application-flow.md`: hub parsing and controller application.
- `references/source/hub/executor/application-flow.md`: ordered runtime subsystem updates.
- `references/source/tunnel/routing-flow.md`: metadata, matching, and dialing.
- `references/source/hub/route/server.md`: controller router and authentication.
- `references/source/adapter/parser.md`: accepted outbound proxy type names.
- `references/source/rules/parser.md`: accepted rule type names.
- `references/source/test/`: integration and behavior evidence.

## Refresh

Run `python3 scripts/update_docs.py` from this skill directory only when a
mechanical refresh is intended. The wrapper regenerates with `--overwrite`, so
the inferred pages must be reviewed/re-authored and the skill revalidated after
refresh.
