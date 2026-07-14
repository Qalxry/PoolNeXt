# Zashboard upstream and local integration

## Provenance

- Upstream: <https://github.com/Zephyruso/zashboard>
- Upstream release: `v2.8.0`
- Upstream commit: `80a2a52d928df9838986bdef317ed0623eb026dc`
- Imported package version: `2.8.0`
- License: MIT, Copyright 2024 Zephyruso

The upstream MIT license is preserved verbatim in [`LICENSE`](./LICENSE), and
the attribution is also included in the repository root [`NOTICE`](../../NOTICE).
Neither file may be removed when updating or redistributing the embedded UI.

## PoolNeXt patch set

The vendored source stays close to the pinned upstream commit. PoolNeXt carries
only the following integration patches:

| File                             | Local purpose                                                                                                                                                                                                                        |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `vite.config.ts`                 | Serve assets from the embedded `/zashboard/` base path.                                                                                                                                                                              |
| `src/api/index.ts`               | Route HTTP and WebSocket Clash API traffic through the authenticated same-origin `/api/zashboard/clash` proxy; never expose the Mihomo secret to the browser.                                                                        |
| `src/router/index.ts`            | Disable the standalone backend setup flow because PoolNeXt owns the backend connection.                                                                                                                                              |
| `src/store/setup.ts`             | Install one fixed PoolNeXt backend and prevent users from replacing or deleting it. The persisted `poolx-clash` UUID is intentionally retained as a compatibility key for existing browsers; it is not an upstream/update reference. |
| `package.json`                   | Add repeatable embedded-build cleanup plus non-mutating lint and format-check commands used by PoolNeXt quality gates.                                                                                                               |
| `scripts/clean-build-output.mjs` | Remove stale `dist/` output before an embedded build so removed upstream assets cannot survive into a release.                                                                                                                       |
| `UPSTREAM.md`                    | Record provenance, the reviewed patch inventory, license obligations, and update policy.                                                                                                                                             |

Keep product-specific behavior out of the vendored UI unless it is required for
the authentication boundary, same-origin routing, embedded base path, or a
small non-business branding label. Product workflows belong in the Next.js UI.

## Updating the vendored source

1. Fetch the desired signed/tagged upstream revision and record its full commit
   hash, package version, and license status in this file.
2. Compare the clean upstream tree against `server/zashboard`; review every
   difference and reapply only the integration patches listed above.
3. Preserve `LICENSE`, update the root `NOTICE`, and add any new third-party
   notices required by the upstream dependency changes.
4. Run `pnpm install --frozen-lockfile`, `pnpm type-check`, and the no-font build
   verification described below.
5. Verify the same-origin HTTP/WebSocket proxy, login redirect, fixed backend,
   and `/zashboard/` asset paths before accepting the update.

## No remote font downloads

The canonical PoolNeXt release and embedded build command is:

```bash
pnpm build:no-fonts
```

This invokes `vite build --mode SystemUI`. In that mode `src/assets/load-fonts.ts`
does not import full text-font packages and does not inject CDN stylesheets.
The resulting UI uses system fonts. Two small, repository-local flag-glyph font
subsets referenced by `src/assets/main.css` remain bundled; they require no
network request at runtime.

The `build:embedded` script and every release/container workflow must delegate
to this SystemUI build. Do not use `build:cdn-fonts` for PoolNeXt artifacts.
After building, verify that the generated `dist/` contains no `unpkg.com`,
`fonts.googleapis.com`, or `fonts.gstatic.com` reference. Build orchestration
should call `pnpm build:no-fonts` explicitly rather than relying on the upstream
default `pnpm build` behavior.
