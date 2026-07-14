# Source Evidence: outbound proxy parser cases

> Source file: `adapter/parser.go`
> Reason: public configuration evidence for proxy `type` values accepted by the parser.
> Commit: `a84724665eb7f989809abe463c05f5723bd24975`

`ParseProxy` reads the `type` field and dispatches to these explicit cases:

```text
ss, ssr, socks5, http, vmess, vless, snell, trojan, hysteria,
hysteria2, wireguard, tuic, direct, dns, reject, ssh, mieru, anytls,
sudoku, masque, trusttunnel
```

Each case decodes the mapping into its protocol-specific option structure and
calls the corresponding constructor. Unknown types return
`unsupport proxy type: <type>`. The same function also parses optional `smux`
settings after the base proxy is created.
