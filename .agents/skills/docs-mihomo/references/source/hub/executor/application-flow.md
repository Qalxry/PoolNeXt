# Source Evidence: runtime configuration application

> Source file: `hub/executor/executor.go`
> Reason: focused evidence for ordered subsystem updates during startup and reload.
> Commit: `a84724665eb7f989809abe463c05f5723bd24975`

`executor.ApplyConfig` serializes reconfiguration with a mutex, suspends the
tunnel, updates subsystems, loads providers, then resumes traffic handling:

```go
func ApplyConfig(cfg *config.Config, force bool) {
	mux.Lock()
	defer mux.Unlock()
	log.SetLevel(cfg.General.LogLevel)
	tunnel.OnSuspend()
	updateExperimental(cfg.Experimental)
	updateUsers(cfg.Users)
	updateProxies(cfg.Proxies, cfg.Providers)
	updateRules(cfg.Rules, cfg.SubRules, cfg.RuleProviders)
	updateSniffer(cfg.Sniffer)
	updateHosts(cfg.Hosts)
	updateGeneral(cfg.General, true)
	updateNTP(cfg.NTP)
	updateDNS(cfg.DNS, cfg.General.IPv6)
	updateListeners(cfg.General, cfg.Listeners, force)
	updateTun(cfg.General)
	updateIPTables(cfg)
	updateTunnels(cfg.Tunnels)
	tunnel.OnInnerLoading()
	initInnerTcp()
	loadProvider(cfg.Providers)
	updateProfile(cfg)
	loadProvider(cfg.RuleProviders)
	tunnel.OnRunning()
	updateUpdater(cfg)
	resolver.ResetConnection()
}
```
