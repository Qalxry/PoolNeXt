# PoolNeXt 配置项说明

本文档描述 `PoolNeXt` 当前支持的服务端与前端配置项。

当前配置说明面向 PoolNeXt 当前保留能力，不再把 Agent、OpenResty 等历史链路配置视为正式配置基线。

## 1. Server 配置

Server 支持三类配置来源：

1. 命令行参数
2. 环境变量
3. 数据库 `options` 表中的运行时配置

### 1.1 命令行参数

服务端只有 `server/main.go` 一个正式入口。推荐在仓库根目录通过 `make run` 构建嵌入资产并启动：

```bash
SESSION_SECRET="$(openssl rand -hex 32)" make run
```

需要传递命令行参数时，可显式运行同一入口：

```bash
make build-assets
cd server
SESSION_SECRET="$(openssl rand -hex 32)" go run -tags embedassets -trimpath -buildvcs=false . --port 17778 --log-dir ./logs
```

| 参数 | 作用 | 默认值 |
| --- | --- | --- |
| `--port` | 指定 Server 监听端口 | `17778` |
| `--data-dir` | 数据根目录；派生 `<dir>/poolx.db`、`<dir>/upload`、`<dir>/data/runtime`，显式 `SQLITE_PATH`/`UPLOAD_PATH` 仍优先 | 空（相对当前工作目录） |
| `--session-secret-file` | 从文件读取 `SESSION_SECRET`；环境变量 `SESSION_SECRET` 优先级更高 | 空 |
| `--log-dir` | 指定日志目录 | 空 |
| `--version` | 输出当前版本后退出 | `false` |
| `--reset-password` | 交互式重置管理员密码后退出（不启动服务） | `false` |
| `--help` | 输出帮助信息后退出 | `false` |

### 1.2 环境变量

| 环境变量 | 作用 | 默认值 |
| --- | --- | --- |
| `PORT` | Server 监听端口 | `17778` |
| `GIN_MODE` | Gin 运行模式 | 非 `debug` 时按 release |
| `LOG_LEVEL` | 日志等级 | `info` |
| `SESSION_SECRET` | Session 签名密钥；生产模式至少 32 字节且不能是占位值 | 无，生产必填 |
| `SESSION_SECRET_FILE` | 从文件读取 `SESSION_SECRET`（内容去首尾空白）；`SESSION_SECRET` 优先 | 空 |
| `SESSION_COOKIE_SECURE` | 强制 Session Cookie 使用 Secure | TLS 请求自动启用，否则 `false` |
| `TRUSTED_PROXIES` | 可信反向代理 IP/CIDR，逗号分隔 | 空，不信任转发头 |
| `MAX_REQUEST_BODY_BYTES` | 普通请求体上限 | `8388608` |
| `MAX_UPLOAD_BODY_BYTES` | 已知上传路由请求体上限 | `134217728` |
| `MAX_EXPENSIVE_REQUESTS` | 全局昂贵操作并发上限 | `4` |
| `SQLITE_PATH` | SQLite 数据库文件路径 | `poolx.db` |
| `SQL_DSN` | 兼容旧命名的 PostgreSQL DSN，优先级低于 `DSN` | 空 |
| `REDIS_CONN_STRING` | Redis 连接串 | 空 |
| `UPLOAD_PATH` | 上传目录 | `upload` |

说明：

* `SESSION_SECRET` 在生产环境必须显式配置；仅测试二进制，或同时设置 `GIN_MODE=debug` 与 `POOLNEXT_ALLOW_EPHEMERAL_SESSION_SECRET=true` 时，才允许临时 secret
* `--session-secret-file` 与 `SESSION_SECRET_FILE` 二选一即可；文件内容会去掉首尾空白，空文件或不可读文件会直接拒绝启动；`SESSION_SECRET` 环境变量优先级最高
* `--data-dir` 只填充默认路径：`SQLITE_PATH`、`UPLOAD_PATH` 环境变量仍然优先；运行配置目录固定为 `<data-dir>/data/runtime/<kernel>`。便携目录可直接使用 `scripts/poolnext-portable.cmd`，详见 [deployment.md](./deployment.md) 2.4
* 首次启动会在 stdout 打印一次随机生成的 root 一次性密码，不会写入任何文件；首次登录后强制要求修改密码
* 忘记密码时，运行 `poolnext-server --reset-password` 交互式重置管理员密码（不启动服务，不暴露网络）
* `REDIS_CONN_STRING` 未配置时，相关能力退化为进程内实现
* 服务端升级默认从 `Qalxry/PoolNeXt` 查询发布版本，可通过运行时配置 `ServerUpdateRepo` 覆盖

### 1.3 运行时配置（Option）

以下配置由管理端维护，可热更新：

| 配置项 | 作用 | 默认值 |
| --- | --- | --- |
| `PasswordLoginEnabled` | 是否启用密码登录 | `true` |
| `PasswordRegisterEnabled` | 是否启用密码注册 | `true` |
| `EmailVerificationEnabled` | 是否启用邮箱验证码流程 | `false` |
| `RegisterEnabled` | 是否允许用户注册 | `false` |
| `ServerUpdateRepo` | 服务端版本检查与升级使用的 GitHub 仓库，格式为 `owner/repo` | `Qalxry/PoolNeXt` |
| `KernelType` | 当前启用的代理内核类型 | `mihomo` |
| `MihomoBinaryPath` | Mihomo 二进制安装路径 | 空 |
| `MihomoBinaryVersion` | 最近一次校验通过的 Mihomo 版本输出 | 空 |
| `MihomoBinarySource` | Mihomo 二进制来源：`existing` / `upload` / `download` | 空 |
| `ClashAllowLAN` | 最终 Mihomo 配置中的 `allow-lan` | `false` |
| `ClashExternalController` | 最终 Mihomo 配置中的 `external-controller` | `127.0.0.1:19090` |
| `ClashMode` | 最终 Mihomo 配置中的 `mode`，支持 `rule` / `global` / `direct` | `rule` |
| `ClashSecret` | 最终 Mihomo 配置中的控制密钥 | `3ebc195c9fbe81c01eb9299e3c6bf644` |
| `NodeTestDefaultURL` | 配置导入与节点池共享的默认测速 URL | `https://cp.cloudflare.com/generate_204` |
| `NodeTestDefaultTimeoutMS` | 配置导入与节点池共享的默认测速超时（毫秒） | `8000` |
| `GeoIPProvider` | IP 归属解析方式 | `disabled` |
| `GitHubOAuthEnabled` | 是否启用 GitHub OAuth 登录 | `false` |
| `WeChatAuthEnabled` | 是否启用微信登录 | `false` |
| `TurnstileCheckEnabled` | 是否启用 Turnstile 校验 | `false` |
| `SMTPServer` | SMTP 服务地址 | 空 |
| `SMTPPort` | SMTP 端口 | `587` |
| `SMTPAccount` | SMTP 账号 | 空 |
| `SMTPToken` | SMTP 密钥或授权码 | 空 |
| `SystemName` | 系统名称 | `PoolNeXt` |
| `Notice` | 系统公告 | 空 |
| `About` | 关于页内容 | 空 |
| `Footer` | 页脚文案 | 默认值 |
| `GlobalApiRateLimitNum` | API 限流次数 | `300` |
| `GlobalApiRateLimitDuration` | API 限流时间窗口 | `180` |
| `GlobalWebRateLimitNum` | Web 限流次数 | `300` |
| `GlobalWebRateLimitDuration` | Web 限流时间窗口 | `180` |
| `UploadRateLimitNum` | 上传接口限流次数 | `50` |
| `UploadRateLimitDuration` | 上传接口限流时间窗口 | `60` |
| `DownloadRateLimitNum` | 下载接口限流次数 | `50` |
| `DownloadRateLimitDuration` | 下载接口限流时间窗口 | `60` |
| `CriticalRateLimitNum` | 敏感接口限流次数 | `100` |
| `CriticalRateLimitDuration` | 敏感接口限流时间窗口 | `1200` |

说明：

* Token、Secret 一类敏感配置默认不会通过选项列表直接回显；`ClashSecret` 作为运行控制必需配置，允许在管理员设置页中直接维护
* `ServerUpdateRepo` 默认值为 `Qalxry/PoolNeXt`，用于版本检查与自动升级；如使用自建发布仓库，可改为自己的 `owner/repo`
* `KernelType` 当前仅允许设置为 `mihomo`，`xray` 与 `singbox` 仅保留前端预留入口
* `MihomoBinaryPath`、`MihomoBinaryVersion` 与 `MihomoBinarySource` 只允许由专用内核接口维护，通用 Option 更新接口会拒绝直接改写，避免绕过 `external_immutable` 保护
* `ClashAllowLAN`、`ClashExternalController`、`ClashMode` 与 `ClashSecret` 由设置页“Clash 设置”统一维护，并会在运行阶段参与最终 Mihomo 配置渲染
* `NodeTestDefaultURL` 与 `NodeTestDefaultTimeoutMS` 由设置页统一维护，配置导入和节点池页不再单独提供测速参数表单
* 已移除业务的运行时配置项只允许在兼容清理时兜底处理，不应重新作为正式选项暴露
* 配置项如有增删，必须同步更新本文档

## 2. Frontend 构建环境变量

| 环境变量 | 作用 | 默认值 |
| --- | --- | --- |
| `NEXT_PUBLIC_API_BASE_URL` | 前端请求 API 的基础路径 | `/api` |
| `NEXT_PUBLIC_APP_VERSION` | 前端展示版本号 | `dev` |
| `NEXT_DEV_BACKEND_URL` | 本地开发代理到后端的地址 | `http://127.0.0.1:17778` |

生产构建还显式传递 `NEXT_BUILD_ID` 和 `ZASHBOARD_COMMIT_ID`。Make、Release 与 Docker 使用同一 `VERSION`/`COMMIT` 输入，并统一启用 Go `-buildvcs=false`，避免构建环境是否包含 `.git` 改变制品内容。

## 3. Docker 数据与 PostgreSQL 密码

官方镜像以 UID/GID `10001:10001` 运行，仅 `/data` 和有界 `/tmp` 可写。默认 Compose 使用命名卷；改用 bind mount 时，宿主目录必须预先授权给 `10001:10001`。

Compose 中：

* `POSTGRES_PASSWORD` 是传给 PostgreSQL 的原始密码
* `POSTGRES_PASSWORD_URLENCODED` 是写入 `SQL_DSN` 的 RFC 3986 URL 编码值
* 使用 `openssl rand -hex 24` 生成密码时，两者可以使用相同值
* 任意字符密码必须对 DSN 用户信息部分进行 URL 编码，不能直接插值

## 4. 文档维护要求

以下内容变化时，必须同步更新本文档：

* 服务端命令行参数
* 服务端环境变量
* 运行时配置项
* 前端构建环境变量
* 任一配置项的默认值、用途或示例
