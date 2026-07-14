# Repository Examples and Test Evidence

> This page is AI-authored from repository evidence; it is not upstream documentation.

## Configuration example

The README points to `docs/config.yaml`. It is a broad annotated configuration
catalog rather than a minimal tutorial. It demonstrates top-level inbounds,
controller and TLS settings, TUN, sniffing, DNS, outbound proxies, proxy groups,
proxy/rule providers, ordered rules, sub-rules, and protocol-specific listeners.
Use it to discover field shapes, then verify behavior and defaults in
`config/config.go` or the protocol implementation.

## Integration test suite

`test/README.md` describes Docker-backed TCP/UDP ping-pong and large-data
tests. At this commit it marks Shadowsocks variants, VMess transports, Trojan,
Snell, DNS server, fake-IP, and hosts behavior as covered. The Go tests provide
concrete configuration construction for those cases:

- `test/ss_test.go`: Shadowsocks and plugin combinations.
- `test/vmess_test.go`: VMess, TLS, WebSocket, HTTP/2, HTTP, and gRPC variants.
- `test/vless_test.go`: VLESS variants.
- `test/trojan_test.go`: Trojan variants.
- `test/snell_test.go`: Snell and obfuscation variants.
- `test/hysteria_test.go`: Hysteria integration.
- `test/dns_test.go`: DNS integration behavior.
- `test/clash_test.go`: shared proxy process and traffic test scaffolding.

The test suite requires Docker according to its README. It was not run during
skill generation.

## Small operational examples

Validate a file:

```bash
./mihomo -t -f ./config.yaml
```

Run with controller authentication:

```yaml
mixed-port: 7890
external-controller: 127.0.0.1:9090
secret: replace-me
```

```bash
curl -H 'Authorization: Bearer replace-me' http://127.0.0.1:9090/version
```

The YAML keys and bearer behavior above are directly evidenced; the values are
illustrative local choices.

## Evidence

- `README.md`
- `docs/config.yaml`
- `test/README.md`
- `test/clash_test.go`
- `test/dns_test.go`
- `test/hysteria_test.go`
- `test/snell_test.go`
- `test/ss_test.go`
- `test/trojan_test.go`
- `test/vless_test.go`
- `test/vmess_test.go`
- `hub/route/server.go`
