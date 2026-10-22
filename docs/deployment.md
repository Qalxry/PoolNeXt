# PoolNeXt 部署说明

本文档描述 `PoolNeXt` 当前阶段的部署基线、启动方式和最小验证步骤。

当前文档只描述 PoolNeXt 服务端与前端的部署，不包含远程 Agent 或多实例集群。

## 1. 前置条件

### 1.1 Server

* Go 1.24.13（发布与可重复构建使用该精确版本）
* Node.js 20.19.5
* 由各前端 `package.json` 固定版本的 pnpm（通过 Corepack 启用）
* 可写 SQLite 文件目录，或可访问的 PostgreSQL 实例

## 2. 启动方式

### 2.0 一键安装脚本（Linux，推荐）

```bash
curl -fsSL -o poolnext-install.sh https://raw.githubusercontent.com/Qalxry/PoolNeXt/main/scripts/poolnext-install.sh
bash poolnext-install.sh install
```

脚本自动识别 `linux/amd64` 与 `linux/arm64` 并从 GitHub Release 下载最新二进制；首次运行自动生成并持久化强 `SESSION_SECRET`（存为 `poolnext-secret`，权限 `0600`，环境变量可覆盖）；检测到 systemd 时注册为 `poolnext` 服务，否则 nohup 后台运行。数据文件（数据库、日志、上传）默认位于 `$HOME/poolnext/`，可通过 `POOLNEXT_HOME`、`POOLNEXT_PORT`、`POOLNEXT_REPO` 调整。

更新到最新版：

```bash
bash poolnext-install.sh update          # 更新到最新版
bash poolnext-install.sh update v1.0.1   # 更新到指定版本
```

首次登录的一次性密码：systemd 模式用 `journalctl -u poolnext -n 60 | grep '一次性密码'`；nohup 模式查看 `$HOME_DIR/poolnext.log`。

### 2.1 构建前端静态产物

```bash
make deps
make build
```

`make build` 会以无远程字体模式构建两个前端，并生成带 `embedassets` 的 `dist/poolnext-server`。

构建完成后，Go Server 会托管：

* 管理端：`/`
* Clash 控制台：`/zashboard/`

### 2.2 源码启动

项目只有一个正式服务入口：`server/main.go`。`make run` 会先重建两个前端，再以 `embedassets` 构建标签启动该入口，开发、发布和容器不会再走不同的静态资源契约。

```bash
export SESSION_SECRET="$(openssl rand -hex 32)"
export SQLITE_PATH="$PWD/server/poolnext.db"
export LOG_LEVEL='info'
# 可选：使用 PostgreSQL
# export SQL_DSN='postgres://template:secret@127.0.0.1:5432/poolx?sslmode=disable'
make run
```

也可以显式执行同一入口：

```bash
make build-assets
cd server
go run -tags embedassets -trimpath -buildvcs=false .
```

默认监听 `17778` 端口。

缺失任一前端产物时，`embedassets` 编译会直接失败。旧的 `server/cmd/server` 重复入口已删除，避免 fresh checkout 在运行期才因找不到磁盘资产而崩溃。

### 2.3 Docker Compose 启动

先准备密钥。Compose 把 PostgreSQL 原始密码和 DSN 中的 URL 编码密码分开，避免 `/`、`?`、`#`、`%` 等字符破坏连接串。十六进制随机密码不需要额外编码：

```bash
export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
export POSTGRES_PASSWORD_URLENCODED="$POSTGRES_PASSWORD"
export SESSION_SECRET="$(openssl rand -hex 32)"
```

若使用任意字符密码，必须生成对应的 URL 编码值：

```bash
export POSTGRES_PASSWORD='your raw password'
export POSTGRES_PASSWORD_URLENCODED="$(python3 -c 'import os, urllib.parse; print(urllib.parse.quote(os.environ["POSTGRES_PASSWORD"], safe=""))')"
```

`server/docker-compose.yaml` 使用固定镜像 digest、非 root UID `10001`、只读根文件系统、`cap_drop: ALL`、`no-new-privileges`、有界 `/tmp` 和镜像健康检查。等价的核心配置如下：

```yaml
services:
  postgres:
    image: postgres:17.10-alpine3.24@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193
    restart: unless-stopped
    environment:
      POSTGRES_DB: poolx
      POSTGRES_USER: poolx
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?请先设置 POSTGRES_PASSWORD}
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U poolx -d poolx"]
      interval: 10s
      timeout: 5s
      retries: 5

  poolnext:
    image: ghcr.io/qalxry/poolnext:latest
    container_name: poolnext
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "17778:17778"
    environment:
      SESSION_SECRET: ${SESSION_SECRET:?请先设置高强度 SESSION_SECRET}
      SQLITE_PATH: /data/poolnext.db
      SQL_DSN: postgres://poolx:${POSTGRES_PASSWORD_URLENCODED:?请设置 URL 编码后的数据库密码}@postgres:5432/poolx?sslmode=disable
      GIN_MODE: release
      LOG_LEVEL: info
    read_only: true
    tmpfs:
      - /tmp:rw,nosuid,nodev,noexec,size=16m
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    volumes:
      - poolnext-data:/data

volumes:
  postgres-data:
  poolnext-data:
```

启动：

```bash
cd server
docker compose up -d
```

默认使用命名卷，首次挂载时会继承镜像内 `/data` 的 UID/GID。若改成宿主机 bind mount，需先执行 `chown -R 10001:10001 <数据目录>`，否则非 root 进程无法创建数据库和上传目录。

### 2.4 Windows 部署（实验性，NSSM 服务）

Release 页面提供 `poolnext-server-windows-amd64.exe`：单文件、已内嵌管理端与 zashboard，不需要安装 Node.js。Windows 暂无 Linux 那样的一键远程安装脚本与容器镜像；仓库提供便携启动器 `scripts/poolnext-portable.cmd` 与 NSSM 服务封装脚本 `scripts/poolnext-windows-service.ps1`。

前置条件：Windows 10/11 或 Windows Server 2016+；管理员 PowerShell 5.1+；[NSSM](https://nssm.cc)（把 `nssm.exe` 放进 `PATH`、脚本同目录或 `C:\nssm\nssm.exe`）。

1. 下载并放置二进制：

```powershell
mkdir C:\PoolNeXt -Force
curl.exe -L -o C:\PoolNeXt\poolnext-server-windows-amd64.exe `
  https://github.com/Qalxry/PoolNeXt/releases/latest/download/poolnext-server-windows-amd64.exe
```

2. 在管理员 PowerShell 中注册服务（推荐）：

```powershell
.\scripts\poolnext-windows-service.ps1 install `
  -BinaryPath C:\PoolNeXt\poolnext-server-windows-amd64.exe `
  -InstallDir C:\PoolNeXt `
  -NssmPath C:\nssm\nssm.exe
```

没有克隆仓库时，可单独下载脚本：

```powershell
curl.exe -L -o poolnext-windows-service.ps1 `
  https://raw.githubusercontent.com/Qalxry/PoolNeXt/main/scripts/poolnext-windows-service.ps1
```

常用参数：`-InstallDir`（默认 `C:\PoolNeXt`）、`-ServiceName`（默认 `PoolNeXt`）、`-Port`（默认 `17778`）、`-NssmPath`、`-SessionSecret`（不传则生成并复用 `poolnext-secret`）、`-BinaryPath`（`install`/`update` 必填）、`-RemoveData`（卸载时连带删除安装目录）。

脚本会生成长期 `SESSION_SECRET` 并保存为 `C:\PoolNeXt\poolnext-secret`（ACL 收紧到 Administrators/SYSTEM）、固定工作目录、把 stdout/stderr 重定向并轮转到 `C:\PoolNeXt\logs`、配置异常退出自动重启与优雅停止，最后启动服务并探测 `/api/status`。**首次启动的一次性 root 密码只打印在 `C:\PoolNeXt\logs\poolnext.stdout.log`**，脚本结束时会显示日志尾部。

日常操作：

```powershell
.\scripts\poolnext-windows-service.ps1 status
.\scripts\poolnext-windows-service.ps1 restart
.\scripts\poolnext-windows-service.ps1 update -BinaryPath C:\Downloads\poolnext-server-windows-amd64.exe
.\scripts\poolnext-windows-service.ps1 uninstall            # 停服务并删除服务，保留数据目录
.\scripts\poolnext-windows-service.ps1 uninstall -RemoveData
```

3. 手工前台启动（不使用服务时）：

```powershell
$bytes = New-Object byte[] 32
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
$rng.Dispose()
$env:SESSION_SECRET = -join ($bytes | ForEach-Object { $_.ToString('x2') })
$env:SQLITE_PATH = 'C:\PoolNeXt\poolx.db'
cd C:\PoolNeXt
.\poolnext-server-windows-amd64.exe --port 17778
```

4. 便携目录（绿色版，可整体搬走）：

```
C:\PoolNeXt-Portable\
  poolnext-server-windows-amd64.exe
  poolnext-portable.cmd
  mihomo.exe            # 首次配置内核时手动上传或官方下载
```

```powershell
.\poolnext-portable.cmd
```

启动器会自动：生成 `poolnext-secret.txt`（64 位十六进制）、`cd` 到脚本目录、用 `--data-dir` 把数据库/上传/运行配置固定在当前目录、日志写入 `logs\`；支持用环境变量 `POOLNEXT_PORT`、`POOLNEXT_DATA_DIR` 覆盖端口与数据位置。等价的命令行：

```powershell
.\poolnext-server-windows-amd64.exe `
  --data-dir . `
  --session-secret-file .\poolnext-secret.txt `
  --port 17778 --log-dir .\logs
```

注意：若内核以 managed 方式安装，数据库记录的是**绝对路径**；整个目录搬走后需在设置页重新指定/安装 `mihomo.exe`。其余节点、工作台与端口配置随数据库一起搬动。

5. 手工 NSSM 配置（已有自己的服务封装时可参考）：

```cmd
nssm install PoolNeXt "C:\PoolNeXt\poolnext-server-windows-amd64.exe"
nssm set PoolNeXt AppDirectory "C:\PoolNeXt"
nssm set PoolNeXt AppParameters "--port 17778"
nssm set PoolNeXt AppEnvironmentExtra "SESSION_SECRET=<至少 32 字节强随机串>" "SQLITE_PATH=C:\PoolNeXt\poolx.db" "LOG_LEVEL=info"
nssm set PoolNeXt AppStdout "C:\PoolNeXt\logs\poolnext.stdout.log"
nssm set PoolNeXt AppStderr "C:\PoolNeXt\logs\poolnext.stderr.log"
nssm set PoolNeXt AppRotateFiles 1
nssm set PoolNeXt AppRotateOnline 1
nssm set PoolNeXt AppRotateBytes 10485760
nssm set PoolNeXt AppExit Default Restart
nssm set PoolNeXt AppRestartDelay 5000
nssm set PoolNeXt AppStopMethodConsole 15000
nssm set PoolNeXt Start SERVICE_AUTO_START
nssm start PoolNeXt
```

Windows 注意事项：

* 服务端自动升级在 Windows 上被禁用；升级用脚本 `update`（或停服务替换 exe 后启动）。页面里的“手动升级”会 `start` 一个脱离服务管理的新进程，服务模式下不要使用。
* Mihomo 内核的“官方下载”在 Windows 上会选择官方 `.zip` 资产并自动解出 `mihomo-*.exe`；下载失败时仍可“手动上传”。
* 默认监听 `0.0.0.0:17778`，首次运行需放行 Windows 防火墙；exe 未签名，SmartScreen 可能提示。
* 自定义数据目录可给 exe 传 `--data-dir`（NSSM 场景放进 `AppParameters`），显式 `SQLITE_PATH`/`UPLOAD_PATH` 仍优先；便携目录见第 4 步。
* 数据默认位于安装目录（`poolx.db`、`upload/`、`data/runtime/mihomo/`、`logs/`），备份或迁移时整目录处理。
* 忘记 root 密码：停止服务后，在安装目录运行 `.\poolnext-server-windows-amd64.exe --reset-password`（读取当前目录的 `poolx.db`，无需 `SESSION_SECRET`）。

## 3. 首次登录

访问 `http://localhost:17778`

初始账号：

* 用户名：`root`
* 密码：首次启动时在服务 stdout（Docker 用 `docker logs poolnext` 查看）打印的一次性随机密码

该密码只显示一次，不会写入任何文件；首次登录后系统强制要求修改密码。若忘记密码，可停止服务后运行 `poolnext-server --reset-password` 交互式重置管理员密码（Docker 部署：`docker compose exec poolnext poolnext-server --reset-password`）。生产模式在数据库初始化前拒绝缺失、弱值或占位值 `SESSION_SECRET`。

如需打开内置 Clash 控制台，可在运行状态页点击“打开 Clash 控制台”，或直接访问：

`http://localhost:17778/zashboard/`

该界面会通过 PoolNeXt 服务端的受限同源代理连接 Mihomo controller，继续复用管理端登录态，不需要在浏览器中填写 `secret`。升级、重启、配置替换和 GEO/UI 更新等危险 controller 路由不会开放。

## 4. Swagger

登录管理端后访问：

`http://localhost:17778/swagger/index.html`

如需重新生成文档：

```bash
go install github.com/swaggo/swag/cmd/swag@v1.16.4
cd server
swag init -g main.go -o docs
```

## 5. 本地开发配合方式

前端本地开发：

```bash
cd server/web
pnpm dev
```

`pnpm dev` 默认启动 `http://127.0.0.1:3001`，并通过 `NEXT_DEV_BACKEND_URL` 同源代理到服务端。

## 6. 最小验证步骤

1. 构建前端静态产物
2. 启动服务端
3. 登录管理端
4. 验证用户登录、文件上传、日志、系统设置页面可访问
5. 如启用邮件能力，验证邮箱验证码或密码重置流程
6. 如启用升级能力，验证升级相关页面与接口可访问

## 7. 升级说明

当前 PoolNeXt 保留服务端升级能力：

* 检查最新版本
* 上传服务端二进制进行手动升级
* 执行服务端升级流程

默认升级仓库为 `Qalxry/PoolNeXt`，可在系统设置中通过 `ServerUpdateRepo` 改为自己的 GitHub 发布仓库，格式为 `owner/repo`。

当前系统设置还支持配置代理内核：

* 当前仅开放 `mihomo`
* “现有路径检查”会把二进制登记为 `external_immutable`，禁止覆盖、升级、删除、移动或改权
* 手动上传和官方下载只允许写入 managed 路径，不能覆盖已登记的外部不可变文件
* 官方下载会按当前平台挑选 Mihomo Release 资产：Linux/macOS 使用 `.gz`（直接解压出内核），Windows 使用 `.zip`（自动解出包内的 `mihomo-*.exe`）；解压后的内核同样会执行版本校验，校验失败不会落盘
* 安装完成后会立即执行版本校验，并写回运行时配置

当前文档不描述 Agent 升级、节点升级或 OpenResty 重载。

## 8. 常用验证命令

```bash
# 快速只读质量门
make quality

# 完整本地 CI：shuffle/race/coverage、Swagger、可重复构建、
# desktop/mobile Chromium、容器权限与跨通道一致性
make ci
```

### 8.1 Server

```bash
cd server
GOCACHE=/tmp/poolx-go-cache go test ./...
```

### 8.2 Frontend

```bash
cd server/web
pnpm build
```

## 9. 文档维护要求

启动方式、部署方式、升级流程或联调步骤变化时，必须同步更新本文档和 `README.md`。
