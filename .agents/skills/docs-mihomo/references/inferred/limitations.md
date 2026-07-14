# Limitations and Open Questions

> This page is AI-authored from repository evidence; it is not upstream documentation.

## Offline skill coverage

- The upstream README points to a hosted wiki, but that site is not bundled in
  this repository-derived skill. This skill therefore cannot replace the full
  published Mihomo documentation.
- The generator classified the repository as `sparse-docs`: only three
  Markdown files were mirrored, and there is no `llms.txt` in the repository.
- Automatic source selection reached its 80-file limit. Seven focused manual capsules
  were added for configuration, application flow, routing, rules, adapters, and
  the repository config example, but the skill is still not full source
  coverage.
- `docs/config.yaml` is a large annotated example, not a formal schema. Fields
  and defaults should be checked against `config/config.go` and the relevant
  parser/implementation at the recorded commit.
- Controller subrouters evolve independently. `api.md` is an endpoint-family
  map, not a complete request/response schema for every route.
- Protocol support listed by `adapter/parser.go` establishes accepted proxy
  type names, not interoperability with every third-party implementation or
  every option combination.
- The Docker-backed integration suite and Go tests were inspected but not run
  while producing this skill. No dependency installation, package-manager
  command, docs build, or project executable was run.

## Refresh caveat

`scripts/update_docs.py` reruns the mechanical generator with `--overwrite`.
After refresh, re-review the manifest, reapply or regenerate the inferred pages,
and rerun validation before treating the skill as complete.

## Open questions for upstream or broader documentation

- Exact compatibility matrices and deprecation timelines for every protocol and
  transport are not established by the copied evidence.
- Detailed controller request/response schemas are only partially inferable
  from route handlers.
- Platform-specific behavior for TUN, transparent proxying, process lookup,
  named pipes, and socket options requires targeted platform source/tests.

## Evidence

- `README.md`
- `docs/config.yaml`
- `config/config.go`
- `adapter/parser.go`
- `hub/route/server.go`
- `test/README.md`
- `test/clash_test.go`
- `test/dns_test.go`
- `test/ss_test.go`
- `test/vmess_test.go`
