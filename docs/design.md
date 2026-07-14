# PoolNeXt Proxy Pool Control Plane 设计基线

本文档定义 PoolNeXt 在“从已停止维护的 `Rain-kl/PoolX` fork 演进为独立产品”阶段的有效产品定位、系统边界、核心能力与架构约束。实施顺序与进度以 [`goals/goal-000-poolnext-refactor.md`](../goals/goal-000-poolnext-refactor.md) 为唯一权威入口。

当前前提是：项目不再以“纯模板工程维护”为目标，而是以既有 Go/Next.js 基础设施为资产，持续交付高密度、健康感知、可验证的单机 IP 代理池控制端。

如果新增需求超出本文档范围，必须先更新本文档，再进入实现。

## 1. 产品定位

PoolNeXt 定位为面向代理池场景的单主机 Web 控制面：一个 PoolNeXt 服务管理一个 Mihomo 进程，由该进程承载多个 HTTP/SOCKS/Mixed 监听入口，为用户提供节点导入、健康探测、代理池管理、运行控制、结构化可观测性和高密度运维界面。

当前阶段的定位原则：

* 复用 fork 中可验证的用户、鉴权、上传、设置、导入解析和 Go 分层能力
* 以“统一控制平面”而非“前端直连内核”作为核心交互模式
* 以 Mihomo v1.19.24 闭环为当前唯一数据面目标，暂停 sing-box / Xray 实现
* 以高信息密度、低资源占用、明确故障状态和可恢复运维为默认工程目标

## 2. 范围边界

当前明确纳入主线：

* 用户注册、登录、登出、会话、Token、自助资料维护
* 用户管理、角色与状态控制
* 邮箱验证码、密码重置、邮箱绑定
* 文件上传、删除、检索、下载与元数据管理
* 系统设置、系统公告、关于页与公共状态信息
* 应用日志记录与管理端查看
* 服务端版本检查、手动上传升级包和服务端升级流程
* 配置源上传、解析、节点标准化、双层去重与导入确认
* 订阅地址手动拉取（导入流程中填写订阅 URL 即时抓取内容，仅支持手动触发，导入后不保留定时同步）
* 节点池管理、搜索筛选、节点标签、批量测试、测试结果展示
* 工作台配置，包括监听入口、节点集合、策略、测试 URL、间隔、模板保存与片段预览
* 运行配置渲染、片段快照、片段合并、校验和比较、启动、停止、热重载、运行状态、自动刷新与日志
* 内核能力发现、能力协商与按能力降级的前端体验
* 自动启动、崩溃恢复、节点健康隔离、连接建立阶段故障转移与端口就绪验证
* 有界结构化事件、日志保留预算、错误聚合和按 operation/runtime session 追踪

当前明确不纳入 MVP：

* 多租户平台化
* 多用户隔离工作区
* 多实例并行运行
* 复杂规则编辑器
* 订阅源定时同步（周期性自动刷新仍不支持）
* 对外暴露内核私有控制 API
* 云端分布式节点编排、Agent 集群或边缘分发体系
* sing-box / Xray 运行实现；只保留不妨碍 Mihomo 主线的抽象边界

当前明确不做：

* 把不同内核的私有配置格式直接暴露为前端主模型
* 用户直接编辑唯一运行中配置文件并绕过数据库
* 通过拼接 shell 的方式执行内核控制命令
* 修改、覆盖、升级、删除、移动或改变外部 Mihomo 二进制的权限
* 把 zashboard Vue 组件并入 Next.js，或向浏览器开放完整 Mihomo 写接口
* 对已经发送应用数据的任意 HTTP/TCP 请求进行透明重放

## 3. 技术基线

### 3.1 Server

服务端当前采用：

* Go 1.24+
* Gin
* GORM
* SQLite / PostgreSQL
* Session 登录体系
* 基于 `os/exec` 的本地进程控制

### 3.2 Frontend

前端当前采用：

* Next.js App Router
* React 19
* TypeScript
* Tailwind CSS
* TanStack Query
* React Hook Form + Zod

## 4. 整体架构

```text
PoolNeXt Proxy Pool Control Plane
├── Server
│   ├── Admin API / Auth / Import / Settings
│   ├── RuntimeManager / Health Scheduler / Observability
│   ├── Database desired state / DATA_DIR snapshots
│   ├── Restricted Mihomo controller proxy
│   └── Next.js + isolated zashboard static host
├── Mihomo (one process, multiple listeners)
└── Web
    ├── Next.js formal operations UI
    └── Restricted zashboard advanced diagnostics
```

职责划分：

* Server：提供管理端 API、数据库访问、认证、导入、配置渲染、生命周期、健康与可观测性能力
* Next.js：正式业务与运维 UI，不直接读取或写入 Mihomo controller
* RuntimeManager：串行拥有 Mihomo 进程及 desired/observed state 对账
* Mihomo：负责代理协议、监听、组策略、数据面主动健康检查和连接建立阶段的重选
* zashboard：独立、受限的高级诊断 UI，仅通过服务端方法+路径允许列表访问 controller

## 5. 当前工程组织

当前项目仍以 `server/` 为服务端工作目录，并保持既有工程基线：

```text
server/
├── main.go
├── frontend_assets*.go
├── internal/app/
├── internal/handler/
├── internal/service/
├── internal/model/
├── internal/middleware/
├── internal/router/
├── internal/pkg/
└── web/
```

当前约束：

* `main.go` 是唯一服务启动入口；开发、发布和容器统一遵守 `embedassets` 静态资源契约
* `internal/handler/` 负责接口层
* `internal/service/` 负责业务逻辑编排
* `internal/model/` 负责数据模型与迁移
* `internal/middleware/` 负责通用中间件
* `internal/router/` 负责路由注册
* `internal/pkg/` 负责项目内公共能力与可复用基础设施
* `web/` 作为管理端前端工程与构建产物目录

`docs/dev/Tech.md` 中提出的 Domain / Application / Adapter 思路应被吸收为当前工程的设计原则，而不是直接替换现有目录基线。除非出现明确收益，不引入整套平行目录体系。

## 6. 架构原则

### 6.1 服务端分层

服务端继续遵循 MVC 扩展分层：

* Handler：请求解析、权限入口、响应封装
* Service：业务规则、流程编排、事务边界
* Model：数据模型、迁移与持久化表达
* Middleware：横切关注点
* Router：路由组织与挂载
* Pkg：解析器、渲染器、进程控制、日志流等跨模块可复用基础能力

### 6.2 统一产品模型

业务层统一使用以下产品概念：

* 配置源 `source_configs`
* 代理节点 `proxy_nodes`
* 监听入口 / 端口配置 `port_profiles`
* 工作台模板 `port_profile_templates`
* 端口与节点绑定 `port_profile_nodes`
* 工作台配置片段快照 `runtime_configs`
* 内核实例 `kernel_instances`

前后端、接口层和持久化层都应优先围绕统一概念展开，避免直接使用 Mihomo 私有字段作为主模型。

### 6.3 Runtime IR 与 Adapter

渲染运行配置时，必须先从数据库实体构建统一 Runtime IR，再由具体内核适配器完成目标配置格式输出。

当前主链路约束：

* 单个工作台端口配置生成的是“可合并的配置片段”，不是最终启动文件
* `runtime_configs` 当前保存的是按端口配置生成的片段快照
* Phase 3 启动 Mihomo 时，必须先聚合多个工作台片段，再生成最终运行配置文件

要求：

* 不直接从前端 DTO 或数据库实体拼接 Mihomo YAML
* 渲染、启动、重载、状态读取通过内核适配器统一抽象
* 能力协商结果必须能驱动前端按钮、策略选项和降级提示

### 6.4 前后端同步

能力变更时，必须同步处理：

* 后端接口
* 前端页面
* 导航入口
* API client
* 类型定义
* Swagger
* 部署文档
* 配置项说明

### 6.5 数据库迁移显式化

数据库 schema 变更必须显式维护版本与迁移逻辑，迁移代码统一收敛在 `server/internal/model/migrate/`。

`AutoMigrate` 可以作为建表与补齐的底层手段，但不能替代版本化迁移方案本身。

迁移实现约束：

* `server/internal/model/migrate/` 下的公共迁移基础设施应集中维护，不按单个小方法零散拆文件
* 当前正式版 schema 基线重置为 `v1`，不再继续维护 pre-release 历史版本的逐级升级链路
* 每个版本文件负责当前 release 仍然有效的初始化、校验或版本归一逻辑
* `server/internal/model/main.go` 只负责迁移接入，不承载具体版本升级实现
* `server/internal/model/migrate/scheduler.go` 只负责通用调度，不承载业务化迁移步骤
* 已存在且 schema 合法的数据库可以在启动时把历史版本号归一到当前正式版基线

### 6.6 安全默认

运行控制相关能力必须满足：

* 内核控制接口默认仅监听 `127.0.0.1`
* Secret 等敏感配置不得在日志或接口响应中明文回显
* 执行进程时必须使用参数数组，禁止拼接 shell
* 上传内容、渲染内容和运行目录必须受白名单与权限控制
* 外部内核模式下只允许读取元数据、执行只读版本检查和作为子进程运行；维护/升级 API 必须拒绝该路径
* zashboard 只允许经审计的只读 controller 路由及必要的诊断 WebSocket，所有写路由默认拒绝
* Session Secret 为空、示例值或弱值时拒绝生产启动；首次管理员凭据不得固定写入源码或镜像

## 7. 架构决策记录（ADR）

以下决策自 2026-07-14 起为 Goal 000 的强制约束。变更必须先修改本节、记录替代决策，再进入代码实现。

| ADR | 状态 | 决策 | 主要理由与后果 |
|---|---|---|---|
| ADR-001 | Accepted | 单主机、单 PoolNeXt、单 Mihomo 进程、多监听入口 | 降低进程、端口、配置和恢复复杂度；多实例与远程 Agent 不属于 Goal 000。 |
| ADR-002 | Accepted | 数据库 desired state 是配置真相源 | UI 与 Mihomo 均不能绕过服务层直接改最终运行配置；运行文件是可重建快照。 |
| ADR-003 | Accepted | `/opt/clash-party/resources/sidecar/mihomo` 是 external immutable dependency | 只允许 `stat`、哈希、只读版本检查和执行；禁止覆盖、升级、删除、移动、改权。破坏性测试只使用临时副本。 |
| ADR-004 | Accepted | Mihomo-first，暂停其他内核运行实现 | 先完成可靠闭环；抽象不得牺牲 Mihomo 的配置、健康、日志与 controller 语义。 |
| ADR-005 | Accepted | Next.js 是正式 UI，zashboard 是隔离且受限的高级诊断 UI | 不混合 React/Vue；zashboard 经服务端允许列表代理，升级、重启、配置替换、GEO/UI 更新等接口不可用。 |
| ADR-006 | Accepted | 故障转移承诺限定在新连接和连接建立阶段 | 已发送应用数据的请求可能产生副作用，控制面不得承诺通用透明重放；应用层重试必须由调用方按幂等语义决定。 |

### 7.1 外部内核不可变契约

实施和验证前后必须记录目标文件的 inode、mode、uid/gid、size、mtime 与 SHA-256。任何一项变化均视为验收失败，并立即停止所有涉及该路径的测试。配置验证、生命周期、升级和故障注入必须使用 PoolNeXt `DATA_DIR` 或临时目录中的配置；需要破坏性二进制场景时，只能使用临时副本。

### 7.2 Mihomo 控制边界

PoolNeXt 可以使用 Mihomo 的本地 controller 完成状态、日志、流量、连接、代理组与延迟等诊断，也可以由 RuntimeManager 执行受控配置重载。浏览器不得直接持有 controller secret。服务端必须按调用者和用途拆分接口：正式 UI 使用 PoolNeXt DTO；zashboard 只获得显式允许的方法与路径，未知路径默认拒绝。

### 7.3 故障转移语义

Mihomo 的 fallback、url-test、load-balance 和连接建立逻辑用于新连接选择与建连失败重选。PoolNeXt 补充健康同步、隔离、最低健康节点门槛、全失联保护与端口就绪，但不截获并重放任意应用 payload。产品文案、API 与测试必须使用同一承诺边界。

## 8. 核心对象

当前长期保留的核心对象包括：

* `users`
* `files`
* `options`
* `app_logs`
* `source_configs`
* `proxy_nodes`
* `port_profiles`
* `port_profile_templates`
* `port_profile_nodes`
* `runtime_configs`
* `kernel_instances`

如后续引入能力缓存、标签、健康记录等对象，必须先确认其是否服务于当前主线。

Goal 000 允许新增并长期保留：节点探测任务、节点健康快照/事件、运行节点绑定、结构化可观测性事件和配置历史。它们必须采用有界保留策略，不能无限保存原始样本。

## 9. 文档维护原则

以下内容变化时，应同步更新本文档：

* 产品范围或系统边界变化
* MVP / 非 MVP 边界变化
* 核心对象变化
* 核心架构与职责划分变化
* Runtime IR / Adapter 策略变化
* 服务端工程组织基线变化
* 数据库迁移策略变化
* 外部内核模式、zashboard 允许列表或故障转移承诺变化
