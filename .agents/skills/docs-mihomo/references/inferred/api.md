# CLI and Controller API Surface

> This page is AI-authored from repository evidence; it is not upstream documentation.

## Standalone CLI

The entry point exposes these flags:

| Flag | Environment fallback | Behavior evidenced in `main.go` |
|---|---|---|
| `-d` | `CLASH_HOME_DIR` | Set the configuration directory. |
| `-f` | `CLASH_CONFIG_FILE` | Select a configuration file; `-` reads stdin. |
| `-config` | `CLASH_CONFIG_STRING` | Supply base64-encoded configuration bytes. |
| `-ext-ui` | `CLASH_OVERRIDE_EXTERNAL_UI_DIR` | Override the external UI directory. |
| `-ext-ctl` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER` | Override controller TCP address. |
| `-ext-ctl-unix` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_UNIX` | Override controller Unix socket. |
| `-ext-ctl-pipe` | `CLASH_OVERRIDE_EXTERNAL_CONTROLLER_PIPE` | Override controller named pipe. |
| `-secret` | `CLASH_OVERRIDE_SECRET` | Override controller bearer secret. |
| `-post-up` / `-post-down` | `CLASH_POST_UP` / `CLASH_POST_DOWN` | Run shell commands around service lifetime. |
| `-m` | — | Enable geodata mode. |
| `-v` | — | Print version/build information and exit. |
| `-t` | — | Parse-test configuration and exit. |

The first positional command can also be `convert-ruleset` or `generate`, each
delegating to its package-specific command parser. Their detailed flags are not
summarized in this skill because the generated evidence set does not include
those parsers.

## REST controller

The top-level router exposes:

```text
GET /
GET /logs
GET /traffic
GET /memory
GET /version
/configs
/proxies
/group
/rules
/connections
/providers/proxies
/providers/rules
/cache
/dns
/storage
/restart
/upgrade
```

Representative subroutes evidenced in `hub/route/*.go` include configuration
GET/PUT/PATCH, proxy and group delay checks, connection listing/deletion,
provider updates and health checks, rule listing, fake-IP/DNS cache flushes,
DNS query, storage CRUD, UI/core/geodata upgrades, and restart. Some mutation
routes are disabled in embedded mode.

When `secret` is non-empty, the TCP/TLS controller group requires
`Authorization: Bearer <secret>`. WebSocket requests may supply `?token=`
because browser WebSocket APIs cannot set the custom header. The repository's
configuration example explicitly warns that Unix-socket, named-pipe, and the
optional controller DoH path do not use the same secret protection; deployments
must secure those transports separately.

## Evidence

- `main.go`
- `hub/route/server.go`
- `hub/route/configs.go`
- `hub/route/proxies.go`
- `hub/route/groups.go`
- `hub/route/connections.go`
- `hub/route/provider.go`
- `hub/route/cache.go`
- `hub/route/dns.go`
- `hub/route/storage.go`
- `hub/route/restart.go`
- `hub/route/upgrade.go`
- `docs/config.yaml`
