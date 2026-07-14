# Evidence-Grounded Quick Start

> This page is AI-authored from repository evidence; it is not upstream documentation.

## Build from source

The repository declares Go 1.20 in `go.mod`; its README says Go 1.20 or newer
and provides this build path:

```bash
git clone https://github.com/MetaCubeX/mihomo.git
cd mihomo
go mod download
go build
```

The README also documents `go build -tags with_gvisor` for a build with the
gVisor TUN stack. These commands are recorded here from upstream evidence; they
were not executed while generating this offline skill.

## Start with a configuration file

The standalone executable accepts `-f <path>` for a YAML file and `-d <dir>`
for the configuration directory. If neither `-f` nor inline configuration is
provided, the default path is `config.yaml` under the Mihomo home directory.
On first use, `config.Init` creates a minimal file containing
`mixed-port: 7890` if the default file is absent.

A minimal explicit file can therefore start with:

```yaml
mixed-port: 7890
mode: rule
```

Validate it without starting the long-running service:

```bash
./mihomo -t -f /path/to/config.yaml
```

Then start the process with:

```bash
./mihomo -f /path/to/config.yaml
```

## Other evidenced configuration inputs

- `-config <base64>` / `CLASH_CONFIG_STRING`: base64-encoded YAML bytes.
- `-f -`: read configuration bytes from standard input.
- `CLASH_HOME_DIR` and `CLASH_CONFIG_FILE`: environment equivalents for the
  home and file flags.
- `SIGHUP`: reparse and reapply the same configuration source while running.

See `configuration.md` before adding DNS, TUN, controller, providers, or
protocol-specific proxies.

## Evidence

- `README.md`
- `go.mod`
- `main.go`
- `config/initial.go`
- `constant/path.go`
- `docs/config.yaml`
