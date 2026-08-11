<p align="right">
  <a href="./README_CN.md">中文</a>
</p>

<div align="center">

# PoolNeXt

A proxy pool control panel built with Gin and Next.js. Its core goal is to organize Clash/Mihomo nodes into reusable proxy pools for web scraping, data collection, proxy requests, and automated outbound network scenarios.

</div>

> [!NOTE]
> The node-to-proxy-pool approach based on glider is deprecated. The legacy version is archived in the `glider` branch. This project now provides a full graphical interface system for building proxy pools, currently supporting only the Mihomo core.

> [!WARNING]
> This project is only a proxy pool control panel and does NOT provide nodes or methods to obtain them. It is intended for learning, research, and technical exchange only. Any illegal use is strictly prohibited.

> [!WARNING]
> On first startup, PoolNeXt prints a random one-time root password to the server stdout. Sign in with it and change the password immediately; the password is shown only once and is never stored in a file. If you lose it, run `poolnext-server --reset-password` to reset it interactively.

---

## Introduction

When making multiple requests to target websites using crawlers, anti-scraping mechanisms are often triggered, leading to access bans. These bans are typically based on IP addresses, so switching IPs is an effective way to bypass restrictions and maintain stable scraping.

A common solution is to use a proxy pool. However, existing solutions have drawbacks:

- Free proxy pools often have poor quality, with low stability and availability
- Paid proxy services can be expensive and exceed practical needs

A more cost-effective approach is to utilize nodes provided by proxy service providers (“airports”), combined with open-source proxy cores, to build reusable proxy pools.

This project provides a clean and user-friendly UI for efficiently managing and organizing proxy nodes, eliminating the need to manually write or maintain complex configuration files. It also offers unified core control for centralized management of the proxy engine.

### Core Features

- Import and manage a large number of nodes via configuration files or subscription URLs
- Organize nodes into reusable proxy pools
- Provide stable local proxy endpoints for crawlers, automation tasks, and proxy systems
- Support load balancing, automatic fallback, and latency-based node selection
- Visual dashboard for managing core runtime status

---

## Features

- Web-based management panel with authentication and system settings
- Node pool management with testing, filtering, and cross-workspace reuse
- Workspace-based port configuration for building proxy pool entry points
- Multiple port listeners, each mapped to a different proxy pool configuration
- Built-in zashboard for extended monitoring and management of the core

---

## Typical Use Cases

- Proxy pools for web scraping and crawling services
- Unified outbound proxy gateway for automation platforms
- Rotating or fallback proxy entry points for data collection tasks
- Workspace-based proxy orchestration for different target sites
- Proxy access for AI services to reduce risk and improve stability

---

## Quick Start

Login URL: `http://IP:3000/`  
Username: `root`  
Password: the one-time random value printed to the server stdout on first startup. You will be required to change it after the first sign-in.

### One-command Install (Linux, recommended)

```bash
curl -fsSL -o poolnext-install.sh https://raw.githubusercontent.com/Qalxry/PoolNeXt/main/scripts/poolnext-install.sh
bash poolnext-install.sh install
```

The script automatically:
- Detects `linux/amd64` and `linux/arm64` and downloads the latest release binary
- Generates and persists a strong `SESSION_SECRET` on first run (or reuses an existing one)
- Registers a `systemd` service when available, otherwise runs in the background with `nohup`
- Keeps data files (database, logs, uploads) next to the binary in `$HOME/poolnext/`

After install, sign in at `http://IP:3000/` with `root` and the one-time password from the first-start log:
- systemd: `journalctl -u poolnext -n 60 | grep '一次性密码'`
- nohup mode: `grep '一次性密码' ~/poolnext/poolnext.log`

Update to the latest release at any time:

```bash
bash poolnext-install.sh update
```

Environment variables: `POOLNEXT_HOME` (install dir, default `$HOME/poolnext`), `POOLNEXT_PORT` (default `3000`), `POOLNEXT_REPO` (GitHub repo, default `Qalxry/PoolNeXt`). Downloads honor `https_proxy`/`http_proxy`.

### Docker Deployment

```yaml
services:
  postgres:
    image: postgres:17-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: poolx
      POSTGRES_USER: poolx
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD}
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U poolx -d poolx"]
      interval: 10s
      timeout: 5s
      retries: 5

  poolnext:
    image: ghcr.io/qalxry/poolnext:latest
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "3000:3000"
#     Expose proxy listening ports as needed
    environment:
      SESSION_SECRET: ${SESSION_SECRET:?set SESSION_SECRET to a strong random value}
#     If SQL_DSN is specified, the following will be ignored
      SQLITE_PATH: /data/poolx.db
#     To use SQLite, comment out SQL_DSN and remove postgres service
      SQL_DSN: postgres://poolx:${POSTGRES_PASSWORD}@postgres:5432/poolx?sslmode=disable
      GIN_MODE: release
      LOG_LEVEL: info

    volumes:
      - ./data/poolx:/data
```

### Local Deployment

Download the precompiled binary from the Release page:

```bash
# Start with SQLite
SESSION_SECRET="$(openssl rand -hex 32)" \
SQLITE_PATH=/path/to/poolx.db \
./poolnext-server
```

Access URL: http://localhost:3000

Initial credentials:
* Username: `root`
* Password: the random one-time value printed to the server stdout on first startup

The password is shown only once and is never written to a file; you must change it after the first sign-in. If you lose it, stop the server and run `poolnext-server --reset-password` to reset it interactively. Production startup rejects missing, weak, or placeholder `SESSION_SECRET` values. On Linux, the install script (`scripts/poolnext-install.sh`) handles secret generation for you.


## Configuration

PoolNeXt does not require maintaining a fixed configuration file manually.

Its core workflow is:
* Import nodes
* Organize proxy pools
* Define workspace listening configurations
* Automatically render the final runtime configuration for the core

For runtime parameters, deployment methods, and system configuration, refer to:
* [docs/app-config.md](./docs/app-config.md)
* [docs/deployment.md](./docs/deployment.md)


## License

PoolNeXt is maintained at [Qalxry/PoolNeXt](https://github.com/Qalxry/PoolNeXt). It is a fork of the discontinued [Rain-kl/PoolX](https://github.com/Rain-kl/PoolX) project; that link is retained for historical attribution only. PoolNeXt releases, issues, container images, and automatic updates use the Qalxry repository.

This project is licensed under the [Apache License 2.0](./LICENSE). Third-party notices, including the bundled Zashboard MIT notice, are recorded in [NOTICE](./NOTICE).
