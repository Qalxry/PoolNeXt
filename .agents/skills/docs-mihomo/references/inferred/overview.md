# Mihomo Repository Overview

> This page is AI-authored from repository evidence; it is not upstream documentation.

Mihomo is a Go network proxy kernel. The repository describes local proxy
inbounds, multiple encrypted proxy protocols, DNS handling, ordered rules,
proxy and rule providers, transparent proxying, and an HTTP REST controller.
The executable is configuration-driven: it parses YAML or supplied bytes,
constructs proxies/listeners/providers/rules/DNS state, applies those objects to
runtime subsystems, and then handles TCP and UDP traffic through the tunnel.

## Main concepts

- **Inbound listeners** accept HTTP, SOCKS, mixed, transparent, TUN, and
  protocol-specific traffic. Static top-level ports and the `listeners` array
  are both represented in the configuration model.
- **Outbound proxies** are parsed from `proxies` and can also arrive through
  `proxy-providers`. `proxy-groups` select or combine outbound behavior.
- **Rules** are evaluated in order in rule mode. Rules can match domains, IPs,
  ports, processes, inbound properties, logical expressions, or rule sets.
- **DNS and sniffing** can enrich destination metadata before matching,
  including hosts mappings and fake-IP reverse mappings.
- **The controller** exposes runtime state and mutation endpoints for configs,
  proxies, providers, rules, connections, DNS cache, storage, upgrades, and
  restart.

## Repository shape

`main.go` is the standalone entry point. `config/` parses YAML into runtime
objects; `hub/` applies configuration and runs the controller; `listener/` and
`adapter/inbound/` accept traffic; `tunnel/` performs metadata processing,
matching, and relay; `adapter/outbound/` implements outbound protocols;
`rules/` implements matchers; `dns/` and `component/resolver/` implement name
resolution behavior.

## Evidence

- `README.md`
- `main.go`
- `config/config.go`
- `hub/hub.go`
- `hub/executor/executor.go`
- `tunnel/tunnel.go`
- `adapter/parser.go`
- `rules/parser.go`
