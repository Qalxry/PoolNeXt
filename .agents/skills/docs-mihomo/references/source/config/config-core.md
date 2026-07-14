# Source Evidence: configuration model and parse pipeline

> Source file: `config/config.go`
> Reason: focused evidence for top-level YAML keys, defaults, and parse order.
> Commit: `a84724665eb7f989809abe463c05f5723bd24975`

The repository's `RawConfig` maps these major YAML sections into the runtime
configuration. The extracts below omit unrelated fields and condense formatting
while preserving the cited identifiers and values:

```go
type RawConfig struct {
	Port               int  `yaml:"port" json:"port"`
	SocksPort          int  `yaml:"socks-port" json:"socks-port"`
	RedirPort          int  `yaml:"redir-port" json:"redir-port"`
	TProxyPort         int  `yaml:"tproxy-port" json:"tproxy-port"`
	MixedPort          int  `yaml:"mixed-port" json:"mixed-port"`
	AllowLan           bool `yaml:"allow-lan" json:"allow-lan"`
	BindAddress        string `yaml:"bind-address" json:"bind-address"`
	Mode               T.TunnelMode `yaml:"mode" json:"mode"`
	LogLevel           log.LogLevel `yaml:"log-level" json:"log-level"`
	IPv6               bool `yaml:"ipv6" json:"ipv6"`
	ExternalController string `yaml:"external-controller" json:"external-controller"`
	ExternalControllerTLS string `yaml:"external-controller-tls" json:"external-controller-tls"`
	ExternalControllerUnix string `yaml:"external-controller-unix" json:"external-controller-unix"`
	ExternalControllerPipe string `yaml:"external-controller-pipe" json:"external-controller-pipe"`
	Secret             string `yaml:"secret" json:"secret"`

	ProxyProvider map[string]map[string]any `yaml:"proxy-providers" json:"proxy-providers"`
	RuleProvider  map[string]map[string]any `yaml:"rule-providers" json:"rule-providers"`
	Proxy         []map[string]any          `yaml:"proxies" json:"proxies"`
	ProxyGroup    []map[string]any          `yaml:"proxy-groups" json:"proxy-groups"`
	Rule          []string                  `yaml:"rules" json:"rule"`
	SubRules      map[string][]string       `yaml:"sub-rules" json:"sub-rules"`
	Listeners     []map[string]any          `yaml:"listeners" json:"listeners"`
	Hosts         map[string]any            `yaml:"hosts" json:"hosts"`
	DNS           RawDNS                    `yaml:"dns" json:"dns"`
	NTP           RawNTP                    `yaml:"ntp" json:"ntp"`
	Tun           RawTun                    `yaml:"tun" json:"tun"`
	IPTables      RawIPTables               `yaml:"iptables" json:"iptables"`
	Experimental  RawExperimental           `yaml:"experimental" json:"experimental"`
	Profile       RawProfile                `yaml:"profile" json:"profile"`
	GeoXUrl       RawGeoXUrl                `yaml:"geox-url" json:"geox-url"`
	Sniffer       RawSniffer                `yaml:"sniffer" json:"sniffer"`
	TLS           RawTLS                    `yaml:"tls" json:"tls"`
}
```

`DefaultRawConfig` establishes defaults before YAML is unmarshaled. Selected
defaults include rule mode, IPv6 enabled globally, DNS disabled, TUN disabled,
and profile selection persistence enabled:

```go
return &RawConfig{
	AllowLan: false,
	BindAddress: "*",
	IPv6: true,
	Mode: T.Rule,
	GeoAutoUpdate: false,
	GeoUpdateInterval: 24,
	UnifiedDelay: false,
	LogLevel: log.INFO,
	TCPConcurrent: false,
	FindProcessMode: process.FindProcessStrict,
	DNS: RawDNS{Enable: false, EnhancedMode: C.DNSMapping, FakeIPRange: "198.18.0.1/16"},
	Tun: RawTun{Enable: false, Stack: C.TunGvisor, AutoRoute: true, AutoDetectInterface: true},
	Profile: RawProfile{StoreSelected: true},
}
```

`ParseRawConfig` builds the runtime `Config` in stages. The implementation
parses general/controller settings, proxies and listeners, providers and rules,
hosts, DNS and TUN, users/tunnels, and sniffing before returning the result.

```go
general, err := parseGeneral(rawCfg)
controller, err := parseController(rawCfg)
proxies, providers, err := parseProxies(rawCfg)
listeners, err := parseListeners(rawCfg)
ruleProviders, err := parseRuleProviders(rawCfg)
subRules, err := parseSubRules(rawCfg, proxies, ruleProviders)
rules, err := parseRules(rawCfg.Rule, proxies, ruleProviders, subRules, "rules")
hosts, err := parseHosts(rawCfg)
dnsCfg, err := parseDNS(rawCfg, ruleProviders)
err = parseTun(rawCfg.Tun, dnsCfg, config.General)
config.Users = parseAuthentication(rawCfg.Authentication)
config.Sniffer, err = parseSniffer(rawCfg.Sniffer, ruleProviders)
```
