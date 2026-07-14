# Configuration Guide

> This page is AI-authored from repository evidence; it is not upstream documentation.

## Input and precedence

The executable accepts configuration from base64 bytes (`-config`), standard
input (`-f -`), an explicit file (`-f`), or the default home-directory
`config.yaml`. CLI controller/UI/secret overrides are applied after parsing, so
those options replace corresponding YAML values for that process.

## Major YAML sections

| Area | Evidenced keys or sections |
|---|---|
| Inbounds | `port`, `socks-port`, `mixed-port`, `redir-port`, `tproxy-port`, `tun`, `listeners`, `ss-config`, `vmess-config` |
| Access | `allow-lan`, `bind-address`, `authentication`, `skip-auth-prefixes`, LAN allow/deny CIDRs |
| Routing | `mode`, `proxies`, `proxy-groups`, `proxy-providers`, `rules`, `sub-rules`, `rule-providers` |
| Resolution | `hosts`, `dns`, `sniffer`, global `ipv6` |
| Controller | `external-controller`, TLS/Unix/pipe variants, `secret`, CORS, UI and controller DoH settings |
| System | interface/routing mark, keepalive, process lookup, geodata/update URLs, NTP, iptables, profile, TLS, experimental settings |

## Defaults that matter

At the recorded commit, `DefaultRawConfig` sets rule mode, global IPv6 enabled,
LAN access disabled, info logging, DNS disabled, TUN disabled, geodata automatic
updates disabled, and selected-proxy persistence enabled. Do not infer every
default from `docs/config.yaml`; the code is authoritative for parser defaults.

## Proxies and rules

`adapter.ParseProxy` accepts the proxy type names listed in
`source/adapter/parser.md`. `rules.ParseRule` accepts the rule names listed in
`source/rules/parser.md`. Configuration parsing validates several cross
references: proxy/group names, dialer-proxy chains, provider references,
sub-rules, and tunnel proxy targets can produce parse errors when absent or
circular.

Rules are ordered. In rule mode, the first usable matching rule selects its
target; if no usable rule matches, `tunnel.match` returns `DIRECT` at this
commit. Treat that fallback as implementation evidence rather than a timeless
configuration contract.

## DNS, fake-IP, sniffing, and TUN

DNS configuration is parsed after hosts and rules/providers. Fake-IP mappings
can restore a domain from a destination IP before matching. Sniffing may supply
a domain when metadata preprocessing is insufficient. TUN parsing receives the
parsed DNS and general settings, so these sections are coupled in the startup
pipeline.

## Validation advice

Use `mihomo -t -f <file>` to parse-test a configuration before starting. For
protocol-specific fields, consult `docs/config.yaml`, the relevant outbound or
listener option struct, and tests; this offline guide intentionally does not
invent a universal schema.

## Evidence

- `main.go`
- `config/config.go`
- `config/initial.go`
- `config/utils.go`
- `config/utils_test.go`
- `constant/path.go`
- `docs/config.yaml`
- `adapter/parser.go`
- `rules/parser.go`
- `tunnel/tunnel.go`
