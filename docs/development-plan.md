# PoolNeXt Goal 000 实施计划

本文档定义 Goal 000 的执行方法、阶段准入和关键 spike。完整任务、依赖、进度与验收门以 [`goals/goal-000-poolnext-refactor.md`](../goals/goal-000-poolnext-refactor.md) 为唯一权威来源；本文件不得维护一套与 Goal 文件平行的任务状态。

## 1. 总体策略

实施采用“分阶段对齐”，不做无保护的全量重写，也不以孤立补丁维持旧产品形态。

1. 先完成安全止血、外部内核边界和可重复质量门。
2. 再稳定 V3 数据模型、API v2、任务与查询契约。
3. RuntimeManager、健康系统和可观测性在契约稳定后并行推进。
4. 正式 WebUI 先建立高密度设计系统与兼容 adapter，再逐页替换旧业务页面。
5. 每阶段必须可构建、可测试、可回滚；任务代码完成但验收门未通过时，阶段仍未完成。
6. `/opt/clash-party/resources/sidecar/mihomo` 始终是只读外部依赖；破坏性测试只针对临时副本。

## 2. 阶段与交付顺序

| 阶段 | 主要交付 | 后续准入 |
|---|---|---|
| Phase 0 | 安全止血、外部内核保护、统一质量入口、CI 与基线夹具 | P1、P5 |
| Phase 1 | V3 schema、API v2、cursor/operation/SSE 契约、数据库约束 | P2、P3、P4、P5 |
| Phase 2 | RuntimeManager、自动启动、进程所有权、原子快照与受控重载 | P6 |
| Phase 3 | 有界探测、健康状态机、隔离恢复、故障转移与端口就绪 | P6、P7 |
| Phase 4 | 结构化事件、有界总线、SSE、保留与存储健康 | P6、P7 |
| Phase 5 | 高密度设计系统、Shell、数据组件与运维总览 | P6 |
| Phase 6 | Nodes、Pools、Runtime、Logs、Settings 正式页面迁移 | P7 |
| Phase 7 | 可信代理端口、接入片段、订阅元数据、告警与历史 | P8 |
| Phase 8 | 资源、安全、性能、发布、RC、回滚与最终审计 | Goal 完成 |

Phase 2、3、4 可以在 Phase 1 契约稳定后使用互斥文件域并行实施。Phase 6 不得在其消费的 API 与状态模型稳定前宣告完成。

## 3. 单任务执行循环

每个 Goal 任务使用同一闭环：

1. 从 Goal 文件选择第一个满足依赖的待办，将其标为 `[~]`。
2. 记录外部 Mihomo 前置指纹；不涉及内核的纯前端任务也要引用最近一次有效指纹。
3. 先建立可失败的测试、基线或重现命令，再修改实现。
4. 只改该任务声明的文件域；新增交叉依赖时先更新 Goal 文件变更清单。
5. 运行任务级验证、阶段回归和外部 Mihomo 后置指纹。
6. 仅在证据充分时标为 `[x]`，并在 Goal 执行日志记录文件、命令、结果与已知限制。

不得以“未发现错误”代替验证，也不得用窄范围单测证明阶段级资源、安全或构建要求。

## 4. Phase 0 的 8 个关键 Spike

Spike 用于在大规模实现前消除高代价不确定性。它可以产生测试、脚本或小型原型，但结果必须收敛为明确决策，并链接到后续 Goal 任务。所有 spike 都使用第 5 节模板记录结果。

### S0-01 外部 Mihomo 不可变边界

**要回答的问题**：PoolNeXt 如何验证、执行并监督外部 Mihomo，同时从代码和测试上排除覆盖、升级、删除、移动与改权？

执行步骤：

1. 使用 `stat` 与 `sha256sum` 记录 inode、mode、uid/gid、size、mtime、SHA-256。
2. 枚举 kernel service、升级 handler、zashboard proxy 和文件工具中所有能触达二进制路径的写操作。
3. 建立 `external_immutable` 与 `managed` 模式的权限矩阵；未知模式和未知操作默认拒绝。
4. 对原文件只允许元数据读取、`-v` 等只读版本检查和作为受监督子进程执行。
5. 所有升级、故障注入、替换与权限测试使用临时副本。
6. 测试结束再次比较完整指纹；任一差异都使 spike 与阶段验收失败。

通过条件：服务层和 HTTP 层均有拒绝测试；原文件前后完整指纹一致。

### S0-02 Zashboard Controller 允许列表

**要回答的问题**：zashboard 为诊断所需的最小 Mihomo controller 表面是什么，如何保证新增上游路由不会自动暴露？

执行步骤：

1. 基于 Mihomo v1.19.24 路由证据建立“方法 + 规范化路径 + 是否 WebSocket”矩阵。
2. 允许只读版本、日志、流量、内存、规则、代理、连接和必要 provider 状态。
3. 显式拒绝 `/upgrade`、`/restart`、`/configs` 的 PUT/PATCH、GEO/UI/core/provider 更新、storage mutation 与 debug mutation。
4. 路径解码、重复斜杠、尾斜杠、大小写和转义不能绕过匹配。
5. 未列出的新路由默认 403/404；controller secret 只保留在服务端。
6. 使用 HTTP 与 WebSocket 表驱动测试验证允许/拒绝矩阵。

通过条件：危险 mutation 全部拒绝，允许的只读 HTTP/WebSocket 仍能代理，未知路由 fail closed。

### S0-03 安全初始化与会话撤销

**要回答的问题**：新部署如何在没有固定密码和弱 Session Secret 的情况下可用，既有会话又如何在账号状态变化后失效？

执行步骤：

1. 搜索源码、compose、文档和测试中的固定管理员凭据与示例 secret。
2. 比较一次性初始化令牌与随机一次性密码的部署体验，选择一个正式流程并明确强制改密状态。
3. 定义 production 启动时弱 secret 拒绝规则和显式 test/dev 注入方式。
4. 为用户引入凭据/会话版本；改密、禁用、删除和安全重置必须推进版本。
5. 校验 Cookie 的 HttpOnly、Secure、SameSite、Path、Max-Age/Expires。
6. 建立旧 Session/Token 失效、OAuth state 和站内 redirect 测试。

通过条件：新部署无固定凭据；弱 secret 不能生产启动；账号安全变化后旧凭据不可继续使用。

### S0-04 Runtime 与升级并发生命周期

**要回答的问题**：现有 Runtime/Update 的竞态、旧 generation 回写、广播 panic、pipe 泄漏和超长行阻塞能否被确定性重现并修复？

执行步骤：

1. 先保留 `go test -race` 的重现命令与报告，定位 `exec.Cmd.ProcessState` 的非同步访问。
2. 使用短命子进程、并发 Start/Stop、旧进程延迟退出和重复订阅/取消构造确定性测试。
3. 明确单次运行 generation；旧 generation 退出不得覆盖新运行状态。
4. 广播器关闭和发送必须由单一所有者协调，慢订阅者不能阻塞生产者。
5. stdout/stderr reader 必须可取消、可回收，并处理超过 64 KiB 的单行。
6. 重复运行 race、goroutine/FD 计数和超时测试。

通过条件：原竞态与 panic 不再复现，测试无超时、无稳定增长的 goroutine/FD。

### S0-05 Mihomo 重载与重启边界

**要回答的问题**：哪些 desired state 变化可以安全热重载，哪些变化必须受控重启，失败时 active state 如何保持不变？

执行步骤：

1. 使用目标版本源码证据和临时配置验证 controller reload 的 path 语义；路径必须为绝对路径或目标 API 明确接受的空值。
2. 将 listener/node/group/strategy 变化与 controller/secret/binary/home-dir 变化分类。
3. 用临时配置和临时二进制副本验证“校验 → 写 pending → reload/restart → 健康确认 → active”顺序。
4. 注入解析失败、controller 超时、端口冲突和进程退出。
5. 失败时不得推进 active checksum，必须保留 last-known-good 与可操作错误。

通过条件：形成热重载/重启决策表和自动化契约测试；失败路径可回滚且不会伪报成功。

### S0-06 节点探测与故障转移语义

**要回答的问题**：运行中 controller delay、单临时 Mihomo 批量探测和每节点进程三种方案，哪一种在语义、峰值 RSS、吞吐和取消方面最适合？

执行步骤：

1. 使用可控健康/失联/慢响应代理和本地探测目标，避免依赖不稳定公网。
2. 对三种方案记录启动次数、峰值 RSS、FD、goroutine、总耗时、单节点延迟和取消时间。
3. 验证 fallback/url-test/load-balance 在首节点失联、全节点失联和恢复时对新连接的行为。
4. 区分连接失败、TLS、HTTP 状态、目标站故障与节点故障；模拟探测目标整体异常。
5. 明确 PoolNeXt 不透明重放已经发送应用数据的请求。

通过条件：选择一个有界探测架构，记录淘汰方案原因，并形成全失联保护与恢复再加入测试需求。

### S0-07 结构化日志、背压与磁盘预算

**要回答的问题**：正常与突发事件量下，怎样避免慢客户端、日志洪水和数据库写放大拖垮控制面？

执行步骤：

1. 定义稳定事件 envelope、错误指纹、epoch/seq 和白名单 fields。
2. 模拟 50 event/s 常态、1000 event/s 突发、100k 总事件、慢 SSE 和 128 KiB 单消息。
3. 比较阻塞、丢最旧、按级别丢弃和聚合策略；所有缺口必须由 counter/gap 事件显式表示。
4. 测量数据库、JSONL、压缩文件、CPU、RSS 与清理耗时。
5. 验证敏感字段脱敏和服务端固定 audit actor/source。

通过条件：给出总字节、文件数、行数、保留时间和队列容量预算；生产者不被慢消费者无限阻塞。

### S0-08 高密度 WebUI 与大数据基线

**要回答的问题**：10k/100k 节点下，怎样获得类似 zashboard/clash party 的紧凑体验，同时控制 DOM、内存、请求数和可访问性？

执行步骤：

1. 固定 desktop/mobile 视口、主题和 compact/comfortable 密度基线。
2. 使用 10k 确定性节点数据比较大卡片分页、服务端 cursor + 虚拟表格两种路径。
3. 记录首屏时间、交互时间、DOM 节点数、JS heap、网络请求数和滚动稳定性。
4. 验证 sticky header、键盘导航、焦点恢复、44px 触控目标、axe 和 reduced motion。
5. 验证筛选/排序 URL 恢复、选中项补取、增量加载和日志 feed 上限。
6. 生产构建必须不依赖联网字体下载，并能被 Go 静态宿主正确回退/404。

通过条件：采用服务端 cursor + 前端 virtualization；形成 desktop/mobile 性能与可访问性预算。

## 5. Spike 结果模板

每个 spike 在 `tests/baseline/results/` 或后续 Goal 指定位置保存一份 Markdown/JSON 结果。不得只把结论写在聊天或提交信息中。

```markdown
# S0-XX <标题>

- 日期：YYYY-MM-DD
- 基线提交：<git sha>
- 执行环境：<OS/arch/Go/Node/DB>
- 对应 Goal：G000-Px-yy
- 状态：passed | failed | inconclusive

## 假设与决策问题

<要证明或证伪的具体命题>

## 范围与非目标

<包含什么；明确不证明什么>

## 外部 Mihomo 指纹

| 时点 | inode | mode | uid/gid | size | mtime | SHA-256 |
|---|---:|---|---|---:|---:|---|
| before | ... | ... | ... | ... | ... | ... |
| after | ... | ... | ... | ... | ... | ... |

## 命令与夹具

<可复制命令、随机种子、数据规模、超时和临时目录>

## 观察结果

| 场景 | 预期 | 实际 | 证据 | 结论 |
|---|---|---|---|---|
| ... | ... | ... | ... | pass/fail |

## 资源结果

<p50/p95/p99、RSS、FD、goroutine、磁盘、SQL、DOM 等适用指标>

## 决策

<选择；被淘汰方案及原因；适用边界>

## 后续任务

- G000-Px-yy：<需要实施或修复的内容>

## 已知限制

<未覆盖、环境差异和不能外推的结论>
```

若结果为 `inconclusive`，必须写明缺失证据和下一次实验的最小恢复条件，不能据此推进不可逆设计。

## 6. 阶段准入与回归

每阶段至少满足：

* 对应 Goal 任务全部有任务级验证证据。
* 阶段验收门全部通过，不以“后续再补”跳过安全、迁移、资源或文档。
* Go、Next.js、zashboard、Swagger、生产嵌入构建和容器 smoke 使用统一入口执行。
* SQLite/PostgreSQL、desktop/mobile 和外部 Mihomo 不可变指纹按阶段要求覆盖。
* 新增 API、配置项、错误码和用户可见语义同步到 Swagger、README 或专门文档。
* 任何失败都能回退到阶段开始时的可运行状态，不留下半迁移 schema、孤儿进程或 pending 配置冒充 active。

## 7. 当前实施入口

当前从 Goal 文件的 Phase 0 开始。第一任务是 `G000-P0-01`，完成本设计基线、8 个 spike 执行说明和结果模板；随后按依赖完成外部内核/zashboard、安全认证、Runtime 竞态、HTTP 生命周期、统一构建、CI、品牌许可和基线夹具。
