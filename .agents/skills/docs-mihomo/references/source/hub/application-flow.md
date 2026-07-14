# Source Evidence: hub parse and controller application

> Source file: `hub/hub.go`
> Reason: focused evidence for how parsed configuration and CLI overrides reach the controller and executor.
> Commit: `a84724665eb7f989809abe463c05f5723bd24975`

`hub.Parse` parses configuration bytes or the configured file, applies CLI
override options, then applies both the controller and runtime configuration:

```go
func Parse(configBytes []byte, options ...Option) error {
	var cfg *config.Config
	var err error
	if len(configBytes) != 0 {
		cfg, err = executor.ParseWithBytes(configBytes)
	} else {
		cfg, err = executor.Parse()
	}
	if err != nil {
		return err
	}
	for _, option := range options {
		option(cfg)
	}
	ApplyConfig(cfg)
	return nil
}
```

`ApplyConfig` first recreates the external controller from `cfg.Controller`
and `cfg.TLS`, then calls `executor.ApplyConfig(cfg, true)` for the runtime
subsystems.
