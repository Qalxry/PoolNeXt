# Source Evidence: traffic resolution and rule routing

> Source file: `tunnel/tunnel.go`
> Reason: focused evidence for metadata preprocessing, mode selection, rule matching, and outbound dialing.
> Commit: `a84724665eb7f989809abe463c05f5723bd24975`

The implementation preprocesses metadata using fake-IP/hosts mappings, then
selects an outbound according to direct, global, or rule mode:

```go
func resolveMetadata(metadata *C.Metadata) (proxy C.Proxy, rule C.Rule, err error) {
	if metadata.SpecialProxy != "" {
		proxy, exist = proxies[metadata.SpecialProxy]
		return
	}
	// RuleMatchHelper can resolve destination IP and discover process identity.
	switch mode {
	case Direct:
		proxy = proxies["DIRECT"]
	case Global:
		proxy = proxies["GLOBAL"]
	default:
		proxy, rule, err = match(metadata, helper)
	}
	return
}
```

For TCP, `handleTCPConn` validates and normalizes metadata, applies DNS/hosts
preprocessing, optionally sniffs a domain, resolves the rule and proxy, and
calls the selected proxy's `DialContext`:

```go
metadata := connCtx.Metadata()
fixMetadata(metadata)
if err := preHandleMetadata(metadata); err != nil { /* ... */ }
if sniffingEnable && snifferDispatcher.Enable() {
	snifferDispatcher.TCPSniff(conn, metadata)
}
proxy, rule, err := resolveMetadata(metadata)
remoteConn, err := proxy.DialContext(ctx, dialMetadata)
```

Rule mode walks rules in order. A matching target must exist, pass-through
adapters are skipped, and UDP-incompatible adapters are skipped. If no usable
rule matches, the implementation returns `DIRECT`:

```go
for _, rule := range getRules(metadata) {
	if matched, ada := rule.Match(metadata, helper); matched {
		adapter, ok := proxies[ada]
		if !ok { continue }
		if metadata.NetWork == C.UDP && !adapter.SupportUDP() { continue }
		return adapter, rule, nil
	}
}
return proxies["DIRECT"], nil, nil
```
