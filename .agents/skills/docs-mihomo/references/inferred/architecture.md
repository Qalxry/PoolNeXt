# Runtime Architecture

> This page is AI-authored from repository evidence; it is not upstream documentation.

## Startup and configuration

1. `main.go` resolves flags/environment, chooses file/stdin/base64 input, and
   optionally performs config-only validation.
2. `hub.Parse` asks `hub/executor` to parse bytes or the configured file, then
   applies CLI overrides.
3. `config.ParseRawConfig` constructs general/controller settings, proxies,
   listeners, providers, rules, hosts, DNS, TUN, authentication, tunnels, and
   sniffer configuration, returning errors for invalid references.
4. `hub.ApplyConfig` recreates the external controller and delegates the rest
   to `executor.ApplyConfig`.
5. `executor.ApplyConfig` suspends the tunnel, updates subsystem state under a
   mutex, loads providers, and resumes the tunnel.

## Traffic path

```text
listener / inbound adapter
        -> connection or packet metadata
        -> tunnel pre-processing (hosts, fake-IP, optional sniffing)
        -> direct/global/rule mode selection
        -> ordered rule matching and proxy/group selection
        -> outbound DialContext or packet handling
        -> tracked relay and statistics
```

In rule mode, match helpers resolve an IP or discover a process only when a
rule needs that data. The matcher skips missing targets, pass-through adapters,
and UDP-incompatible adapters. If no usable rule matches, the current
implementation falls back to `DIRECT`.

## Control plane and data plane

- The **control plane** is `config/`, `hub/`, providers, profile storage,
  updaters, and `hub/route/`.
- The **data plane** is `listener/`, `adapter/inbound/`, `tunnel/`,
  `adapter/outbound/`, `transport/`, `dns/`, and the resolver/sniffer
  components.
- Configuration reload is stateful: the executor updates global subsystem
  registries rather than spawning a separate isolated runtime object.

## Extension points visible in this repository

- Add outbound protocols through `adapter.ParseProxy` and an outbound adapter.
- Add rule types through `rules.ParseRule` and a `constant.Rule`
  implementation.
- Add inbound listener types through `listener/parse.go` and `listener/config/`.
- Add controller capabilities through a `hub/route` subrouter.

These are source-level observations, not a compatibility promise for external
plugins or a stable embedding API.

## Evidence

- `main.go`
- `config/config.go`
- `config/utils_test.go`
- `hub/hub.go`
- `hub/executor/executor.go`
- `hub/route/server.go`
- `listener/listener.go`
- `listener/parse.go`
- `tunnel/tunnel.go`
- `adapter/parser.go`
- `rules/parser.go`
