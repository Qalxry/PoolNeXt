# Source Evidence: repository configuration example

> Source file: `docs/config.yaml`
> Reason: focused excerpts from the configuration example linked by the README.
> Commit: `a84724665eb7f989809abe463c05f5723bd24975`

The upstream example is large (about two thousand lines). These focused
excerpts preserve its keys and values while omitting some inline commentary;
they are not a full schema.

```yaml
mixed-port: 10801
allow-lan: true
bind-address: "*"
authentication:
  - "username:password"
skip-auth-prefixes:
  - 127.0.0.1/8
  - ::1/128
find-process-mode: strict
mode: rule
log-level: debug
ipv6: true
```

```yaml
external-controller: 0.0.0.0:9093
external-controller-tls: 0.0.0.0:9443
# secret: "123456" # `Authorization:Bearer ${secret}`
external-controller-cors:
  allow-origins:
    - "*"
  allow-private-network: true
```

```yaml
tun:
  enable: false
  stack: system # gvisor/mixed
  dns-hijack:
    - 0.0.0.0:53 # 需要劫持的 DNS
  # auto-detect-interface: true # 自动识别出口网卡
  # auto-route: true # 配置路由表
```

```yaml
dns:
  cache-algorithm: arc
  enable: false
  prefer-h3: false
  listen: 0.0.0.0:53
  enhanced-mode: fake-ip # or redir-host
  fake-ip-range: 198.18.0.1/16
```

The same file contains extensive `proxies`, `proxy-groups`,
`proxy-providers`, `rule-providers`, `rules`, `sub-rules`, and `listeners`
sections. Consult the upstream file when an exact protocol field is required.
