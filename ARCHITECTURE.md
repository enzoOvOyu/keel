# ARCHITECTURE.md — keel 自动交付部署平台架构说明

| 文档属性 | 值 |
|---|---|
| 版本 | v3.0.3（与 PRD 网络前置、BOM 导入及审批契约对齐） |
| 状态 | 设计基线；既有 17 个功能域 + 网络底座前置域（FR-0）完整保留，实施状态以 docs/HANDOFF.md 为准 |
| 日期 | 2026-10-20 |
| 上游 | PRD.md v3.0.3（FR-0～FR-17 / NFR-1～NFR-20；编号在本文引用） |
| 下游 | DESIGN-SYSTEM.md / IMPLEMENTATION-PLAN.md / ACCEPTANCE.md |
| 产品命名 | 产品、导航品牌、登录页、命令行与交付包统一为小写 **keel**；目标机程序/路径使用 keel-agent、keel-cache，配置变量使用 KEEL_ 前缀 |
| 网络子系统命名 | 网络底座子系统 **`keel-fabric`**（历史代号 NDP 仅作别名，见 PRD 附录 C-13）；服务器域消费网络能力的唯一入口为 **FR-0.8 证据与排障接口域** |
| 基线内容说明 | Go 模块化单体 + Agent gRPC / Agentless SSH 双执行通道 + BMC 带外能力；部署/运维双平面；G1–G6；型号/物理身份/门禁证据；登录、项目隔离、执行恢复从 P1 建立 |
| v2.5 变更摘要 | 修正准入循环；不可变版本与范围证据；事务 outbox/执行回执/统一资源锁；NCCL 通信组；物理端口前置；维护期白名单与清理闭环；BMC 能力矩阵；会话登录及明确项目域；页面数据/指标单位契约；容量预算与 mock/真机验收边界 |
| v3.0 变更摘要 | ① **网络交付下沉为前置域**：新增 FR-0 承载（服务/数据/接口/状态），服务器域经 FR-0.8 消费，第三方平台走适配器；② **统一 BOM 与导入包**：`bom_item` 单表承载双域物料，新增 `BOMImportPackage/ManifestFile/`逐实体解析（PRD 附录 A.5–A.8），`Machine/Component/PhysicalPort` 身份在导入即建立；③ 前置断言 **NP-1～NP-6** 与阶段标签 **N-S0～N-S9** 接入 GateEvidence（增 `network_readiness`/`network_evidence_refs[]`/`fabric_stage`/`network_degradation[]`）；④ 依赖不变式由 [I-1]～[I-10] 扩为 [I-1]～[I-12]（新增网络证据不冒充 PASS、导入包单批次一致性）；⑤ 网络侧不可采集一律 `unknown` 降级（NFR-19），阈值单一源（C-06） |

---

## 1. 架构总览

### 1.1 系统上下文（C4-L1）

```
                ┌────────────────────────────────────────────────────┐
                │               keel（本系统）                    │
                │  控制面：API / 作业引擎 / 介质库 / 监控 / CMDB / UI  │
                └────────────────────────────────────────────────────┘
                   ▲              ▲                 ▲            ▲
     REST/OpenAPI  │              │ HTTPS/SSH       │ Redfish    │ REST
   ┌───────────────┘              │ (Agentless)     │ (BMC)      │
   │                              │                 │            │
┌──┴─────────────┐   ┌──────────┐  ┌┴──────────────┐ ┌┴──────────┐ ┌┴──────────┐
│ 网络平台        │   │ IPAM     │  │ 目标服务器      │ │ BMC       │ │ AIOps/    │
│ = keel-fabric  │   │ (可选,   │  │ (≥10,000 台)  │ │ (Redfish) │ │ 运维系统   │
│  (内置前置域)   │   │ 地址规划) │  │ keel-agent     │ └──────────┘ │ (CMDB 消费) │
│  第三方=适配器  │   └──────────┘  │ 或无代理 SSH   │              └───────────┘
└───────┬────────┘                 └───────────────┘
        │ 交换机/端口/线缆/光模块（SSH/NVUE/ethtool/syslog）
        ▼ 就绪证明/LLDP 回读/无损计数 → FR-0.8（G2/G5/G6 前置）
（PXE/iPXE 装机通道 + 介质 HTTP 分发通道：平台 ⇄ 目标服务器固件/OS，HTTP 直连）
（CMDB 为平台内置能力（FR-15），对 AIOps 通过 OpenAPI/导出供数——上图消费方为外部 AIOps 系统）
```

### 1.2 逻辑分层（C4-L2）

```
┌─────────────────────────────────────────────────────────────────────────┐
│  前端 SPA（React 19 + TypeScript + Vite）                                │
│  路由（双平面，v2.3，路由/IA 的 SSOT = DESIGN-SYSTEM §3，本图仅索引）:     │
│    部署平面(随 ProjectContext 切换): /projects /deploy/overview          │
│      /deploy/provision{,/bios,/firmware} /deploy/validation{,/topology} │
│      /deploy/nettest /deploy/config /deploy/nccl /deploy/cleanup        │
│    运维平面(全局): /dashboard /fleet /issues /monitor /flows /cmdb /ai  │
│    公共: /login；系统: /admin                                        │
├─────────────────────────────────────────────────────────────────────────┤
│  API 网关层（gin + 中间件链）                                             │
│  authn(session/OIDC/LDAP) → authz(project/RBAC) → CSRF → audit        │
├─────────────────────────────────────────────────────────────────────────┤
│  领域服务层（Go 模块化单体，内部按 bounded-context 分包）                  │
│  flow-svc  execution-svc  provision-svc  redfish-svc                    │
│  validation-svc(G1-G6)  mapping-svc  portmon-svc(网口监控)              │
│  media-svc(介质库)  catalog-svc(参数目录)  config-svc  render-svc       │
│  nettest-svc(RoCE连通)  nccl-svc  issue-svc(工单闭环)                   │
│  mon-svc(监控告警/采集策略)  model-svc(型号库)  cmdb-svc(CMDB联邦视图)  │
│  ai-svc  cleanup-svc  profile-svc(项目模板)  notify-svc  audit-svc     │
│  report-svc(报告聚合，FR-6/FR-11 共用)；项目/身份边界从 P1 建立       │
│  fabric-svc(网络底座:物料/规划/ZTP/意图/Doublecheck/PushJob)          │
│  fabricmon-svc(交换机采集/无损判定/健康分/日志)  netbaseline-svc(网络配置基线+pongo2 沙箱)  boot-svc(Kea/引导/身份反查) │
│  服务名称表示单体内包边界，不等同于独立部署进程                         │
├─────────────────────────────────────────────────────────────────────────┤
│  执行引擎层                                                               │
│  Scheduler(队列) → Dispatcher(扇出) → Runner Pool                        │
│    ├─ Agent Runner（gRPC 长连接通道，目标机 keel-agent）                    │
│    └─ SSH Runner（Agentless：x/crypto/ssh 连接池，互信域密钥 keel-ctrl-*）  │
├─────────────────────────────────────────────────────────────────────────┤
│  数据层                                                                   │
│  PostgreSQL(核心/资源锁) + Redis(队列/缓存) + MinIO(对象存储/日志/报告/介质桶) │
│  + ClickHouse(指标时序，可选) + Vault(密钥，内置或外接)                    │
└─────────────────────────────────────────────────────────────────────────┘
```

### 1.3 设计原则

1. **模块化单体优先（Modular Monolith）**：万卡交付现场多为单机房或弱联网环境，微服务会放大部署与运维成本。Go 单二进制 + 内部 bounded-context 分包，保留未来拆分边界。
2. **一切皆 Flow**：配置同步、驱动升级、采集、验机、连通性测试、NCCL、清理都是 Execution，复用同一套调度/重试/审计/视图能力。
3. **单一事实源**：设备/部件/物理端口使用稳定 ID，SN 与永久 MAC 是有历史的标识；BOM 是预期资产，DeviceModelVersion 是独立期望拓扑，MappingRevision 是确认后的实际映射。三者版本分别冻结并互相比对；**Media 库是所有介质分发的唯一来源**。
4. **控制面与执行面分离**：Runner 只做"受控执行"，不做业务决策；业务决策（门禁判定、熔断、重试策略）全部在控制面，可审计。
5. **Agentless 必须是一等公民**：装机后未装 agent、退场清理 agent 自身，都必须走 SSH 通道；**SSH 互信在装机流程内自动建立（ADR-06），是双通道可用性的前提**。
6. **崩溃可恢复**：长任务、派发事件和执行回执持久化于 PostgreSQL；控制面重启先对账再续跑。对固件、电源等结果未知的副作用任务，不以重投代替恢复。
7. **固定路径与所有权契约**：目标机路径固定（含 `/opt/keel-cache/`），创建时记录所有权台账；固定目录不等于其中任意文件均可删除，清理须再校验对象身份和创建凭据。
8. **参数与介质解耦于 Flow**：Flow 模板引用参数目录与介质 URI，版本由 Execution 快照冻结——同一 Flow 不同时间跑可以对应不同版本组合，全部可追溯。

---

## 2. 技术选型与理由

### 2.1 后端（Go）

| 关注点 | 选型 | 版本基线 | 理由 |
|---|---|---|---|
| 语言 | Go | 1.23+ | 单二进制交付、交叉编译、现场环境友好 |
| HTTP 框架 | gin-gonic/gin | v1.10+ | 中间件生态成熟、性能足够 |
| ORM | entgo.io/ent | v0.14+ | schema 即文档，迁移内建 |
| 任务队列 | 自研轻量队列（Redis zset/stream） | — | 现场单机房规模 Redis 足够；接口抽象化未来可换 Kafka |
| SSH | golang.org/x/crypto/ssh | go.mod/go.sum 锁定 | Agentless 通道核心依赖（含互信管理） |
| gRPC | google.golang.org/grpc | v1.7x | Agent 长连接通道 |
| Redfish | gofish 起步 + 自研适配层 | v0.4+ | BMC 差异用适配器层吸收 |
| 模板渲染 | text/template | stdlib | ConfigSet 渲染 |
| 定时 | robfig/cron | v3 | Flow 触发器 |
| 校验 | go-playground/validator | v10 | API 入参校验 |
| 日志 | slog（stdlib） | 1.22+ | 结构化日志，OTel 对接 |
| 可观测 | OpenTelemetry + Prometheus client | go.mod/go.sum 锁定 | NFR-6 |
| 密钥 | 内置加密存储（AES-GCM）+ 可选外接 Vault | — | 现场无 Vault 时可用 |
| LLM SDK | Provider 接口 + OpenAI-compatible / Anthropic / vLLM 适配器 | 锁定版本 | 统一流式输出、错误及配额 |
| 打流工具链 | 集成而非自研：iperf3、ib_send_bw/ib_write_bw（perftest）、ping/arping | 系统包 | nettest-svc 编排调用，不重复造轮子 |

### 2.2 前端

| 关注点 | 选型 | 理由 |
|---|---|---|
| 框架 | React 19 + TypeScript 5 | 生态最厚；万卡矩阵/DAG/Monaco 组合成熟度最高 |
| 构建 | Vite 6 | 开发热更新快 |
| 路由 | TanStack Router（file-based） | 类型安全路由 + RBAC 守卫 |
| 服务端状态 | TanStack Query v5 | 缓存、轮询、SSE 集成 |
| 表格 | TanStack Table v8 + TanStack Virtual | Table 提供表格模型，Virtual 提供行/列虚拟化；配合服务端筛选、cursor 分页和聚合接口 |
| 图表 | ECharts 5 | 热力矩阵、busbw 分布、Gantt |
| 状态 | Zustand | 轻量 UI 态 |
| UI 基件 | 自研组件层（CSS 变量设计令牌）+ Radix Primitives | keel 品牌、登录页与紫粉色设计令牌统一 |
| YAML 编辑器 | Monaco Editor | Flow 编辑（media/param 函数补全） |
| DAG 拓扑 | @xyflow/react (React Flow) | Flow 拓扑 + mapping 链路 + **网口面板物理图渲染** |
| 表单 | React Hook Form + zod | Flow inputs 动态表单 + **参数目录下拉联动** |
| 拖拽 | @dnd-kit/core | 仅两处使用：工单看板拖拽换泳道（DS §4.14）、表格列拖拽换序（DS §4.2）——两处均为既有能力的交互增强，不引入新组件库 |
| 测试 | Vitest + Testing Library + Playwright | 单测 + E2E |
| i18n | i18next | 中英双语 |

### 2.3 基础设施

| 组件 | 选型 | 说明 |
|---|---|---|
| 数据库 | PostgreSQL 16 | 核心数据；JSONB 存 Redfish 视图 |
| 缓存/队列 | Redis 7.x | 可重建队列/缓存、实时通知；权威业务状态与资源锁在 PostgreSQL |
| 对象存储 | MinIO | 日志、报告、Redfish 原始层、**media 桶（介质库存储 + 内建 apt/yum 镜像源目录）** |
| 时序（可选） | ClickHouse 24.x+ | NCCL/网口状态指标明细 |
| 容器 | docker compose / Helm | 双形态（NFR-7） |

---

**版本与构建约束**：上表表示技术路线及最低兼容族，不是直接安装的浮动版本清单。经过验证的 Go/Node 版本写入工具链文件，Go 依赖写入 go.mod/go.sum，前端依赖写入 package-lock.json，容器使用明确标签并记录 digest；CI 使用同一版本。升级通过独立变更和兼容测试，不使用 latest 或自动跨主版本升级。控制面与 keel-agent 构建 linux/amd64、linux/arm64；HGX B300 与 GB300 NVL72 分别维护 OS、驱动、CUDA、固件和工具包兼容矩阵。

## 3. 核心架构决策（ADR 摘要）

### ADR-01 事件总线：Redis Streams（关键事件持久化）+ Pub/Sub（通知类）（v2.2 修订）

**决策**：事件分两类通道（v2.1 为 Go channel + Redis Pub/Sub，因 Pub/Sub 无持久化、订阅者离线即丢事件，v2.2 修订）：
1. **状态转换类事件（关键）**：`GatePassed(G1..G6)`、`TrustEstablished`、`MediaApproved`、`IssueClosed` —— 走 **Redis Stream + Consumer Group + ACK + PEL 重投**（at-least-once 语义），消费者崩溃恢复后从 Pending Entries List 续投；
2. **通知刷新类事件**：UI 状态刷新、通知中心提醒等非关键通知 —— 可走 Pub/Sub（丢失可容忍，前端有轮询兜底）。

**关键原则**：**状态推进以 PostgreSQL 为准，事件仅作通知**。业务状态变更与 OutboxEvent 同一事务提交，后台发布器重试投递至 Stream；发布确认丢失允许重复发送，不允许提交状态后漏发事件。消费者在业务事务内插入 `(consumer, event_id)` 唯一去重记录，并以状态版本 CAS 执行转移，成功后 ACK。单纯先读 DB 不能保证副作用幂等。

**远端副作用**：TaskAttempt 使用稳定 command_id、attempt_id 与资源 fencing_token，Agent/SSH 包装器持久化接收/启动/完成回执；超时先查询回执及真实设备状态。不可查询或不可幂等的任务标记 `unknown`，保留独占锁进入人工对账，禁止自动重刷固件或重复下电。Redis 丢失可从 DB outbox/待执行状态重建队列；历史结果以 DB 和归档产物为准。

事件 schema 版本化（CloudEvents）保留。跨实例场景 Stream 天然支持多消费者组。

### ADR-02 Agent 协议：gRPC 双向流（保留 v1 + 扩展）

keel-agent 与控制面 gRPC 双向流长连接；注册携带 SN+版本+主机指纹；单连接多任务复用；断线指数退避重连。**心跳周期档位化（v2.2 修订）**：默认 60s，由 CollectPolicy 下发驱动（M1 低频档放宽至 300s，见 FR-13.2）——心跳间隔不是固定值而是采集策略的一部分。心跳消息附带网口状态增量（ethtool/ethtool -m 摘要），供 mon-svc 刷新网口面板（NFR-12 按档位承诺：M1 ≤5min / M2–M4 ≤90s）；心跳附带介质缓存清单（media path → SHA256），供缓存命中检测。**v2.2 数据路径明确**：心跳增量统一由 mon-svc ingest 消费（持久化 MetricSample），portmon-svc / AlertCenter / Dashboard 只读消费 mon-svc 聚合视图，**不直连心跳通道**（单一数据路径，v2.0 中 portmon 直接消费心跳的旧描述废止）。

### ADR-03 Agentless：SSH 连接池 + 幂等脚本（保留 v1 + 扩展）

SSH Runner 连接池（mux 复用）+ 目标机 flock 防重复执行 + 脚本幂等 lint；跨 Agent/SSH/BMC 冲突由控制面统一资源锁解决（§4.3），不能只依赖 flock。连接池绑定平台域密钥（`keel-ctrl-*`，ADR-06）。普通 SSH 任务要求 `Machine.ssh_trust=established`；仅 provision/trust 服务可提交受限的 `trust.bootstrap`/`trust.repair` 系统任务，携带一次性授权凭据、目标身份和审计，不接受任意用户脚本。**v2.2 注**：SSH 通道并发默认 1,000、连接池上限 2,000（参数键 `exec.fanout_ssh_concurrency`），与 PRD FR-1.4 双通道并发口径一致。

### ADR-04 PXE：HTTP-first + iPXE 链式引导（保留 v1 + 扩展）

ProxyDHCP + iPXE + HTTP 分发 + 回调注册。**v2.0 扩展**：cloud-init 模板由 render-svc 渲染时注入平台域公钥（公钥作为平台配置项分域管理，见 ADR-06；轮换时新装机自动用新钥）；回调注册后 provision-svc 自动触发"SSH 首连验证"子任务，结果写 `Machine.ssh_trust`，失败进互信修复队列。

### ADR-05 采集分层：原始层 + 结构化层（保留 v1）

Redfish 原始 JSON 全量入 MinIO，结构化入 JSONB，JSONPath 视图。

### ADR-06 SSH 互信自动建立与生命周期（v2.0 新增）

**决策**：平台维护专用密钥对体系（ed25519），**密钥分域（v2.2 修订）**：按项目/rail 维度发放域密钥（`keel-ctrl-<project>` 或 `keel-ctrl-<rail>`，vault 管理、项目级独立轮换），单钥泄露影响面从"全网"缩小到"单项目/单 rail"；互信建立唯一主路径 = PXE cloud-init 注入；装机回调后自动首连验证；互信状态 `Machine.ssh_trust ∈ {uninitialized, established, broken, revoked}` 参与调度准入。

**关键设计**：
- 公钥管理：平台配置项 + 版本化（轮换 = 新公钥 + 存量机器批量追加 + 老钥延后撤销），域密钥轮换只影响本域机器；
- 首连验证：通过带一次性 token 和过期时间的装机回调或经授权导入的可信指纹绑定目标身份，再执行 SSH `exec true` 校验。禁止无验证地接受任意 host key；指纹变化进入修复流程，不自动覆盖；
- **降级路径**：经批准的批次凭据仅用于一次性首登注入，逐目标记录消费状态，批次完成后撤销平台持有的临时凭据。只有平台创建且授权轮换的临时账户才修改其口令；客户 root 或已有账户口令不自动改写。失败重试限定目标和有效期；
- 修复队列：互信 broken 的机器聚合成修复 Execution（降级路径即上述一次性密码首登）；
- **root 作业审计**：显式声明 + 审批标记 + 脱敏会话记录归档。AuditLog.log_ref 指向控制面对象存储并按保留策略保存；清理只删除目标机平台临时日志，不删除控制面的交付/审计证据；
- 撤销：清理报告和 CMDB 快照取得控制面持久化 ACK 后，经已有受控会话精确删除平台拥有的 authorized_keys 条目，验证新连接被拒绝并关闭原会话；恢复语义见 §5.6，`ssh_trust=revoked` 是退出完成的必要条件；
- **显式取舍（v2.2）**：不引入 SSH CA 短期证书机制（交付现场无 CA 基础设施，域密钥分域已控制爆炸半径；若未来客户环境支持，列为 FR-16+ 候选——见 REVIEW-FINDINGS Open-3）。

**理由**：Agentless 是装机后到 agent 安装前、以及退出清理期的唯一通道，互信若靠人工建立则是全链路的单点人力瓶颈，必须自动化且状态化；密钥分域避免"单钥失陷 = 万卡失陷"的爆炸半径。

**后果**：目标机 host key 指纹纳入 Machine 实体管理；SSH 任务派发增加准入检查逻辑。

### ADR-07 介质库：对象存储 + 内建镜像源 + 固定缓存契约（v2.0 新增）

**决策**：介质统一存 MinIO `media` 桶（版本化、不可覆盖）；分发三形态：任务推送（agent gRPC 分帧 / SSH scp）、HTTP 直拉（内建 nginx 目录服务，支持 Range 断点续传）、镜像源引用（`apt/yum/dnf` 源指向平台内建源目录结构）。目标机统一缓存目录 `/opt/keel-cache/media/`（SHA256 寻址子目录），agent 上报缓存清单、控制面比对跳过已存在介质。

**理由**：
- 万级目标机重复推送驱动包是带宽灾难，HTTP 直拉避免控制面逐份中转；传输量按未命中目标数×包大小计算，命中目标仅校验，冷缓存仍有全量带宽需求；
- 内建镜像源保留原生包管理器依赖解析和签名校验；不假设所有包管理器支持完整事务回滚，回滚能力必须在操作能力矩阵声明；
- `media://` URI 抽象使 Flow/ConfigSet/测试框架与物理存储解耦，审计可追溯。

**后果**：MinIO 与 nginx 成为 PXE 与介质分发底座；审批由 media-svc 管理。介质入库允许填写新版本标识，校验唯一性、签名、SHA256、OS/arch/型号及依赖；业务任务只能选择已批准版本。离线清单包含完整依赖闭包、apt/yum 元数据及签名、驱动/CUDA/NCCL/固件组合和工具版本；摘要校验失败不能无限重试或改摘要放行。缺少基线或依赖为 `missing/blocked`，不能算 PASS。

### ADR-08 PortModel：网口可观测与故障定位的统一底座（v2.0 新增）

**决策**：BOM 导入/预收集即建立 `Machine → Component → PhysicalPort` 稳定身份（含在位历史），早于 G2；未映射的端口也可开工单。PortModel 是物理端口当前可观测投影，OS 名、永久 MAC、PCI BDF、交换机端口映射可空。G3 发布不可变 MappingRevision，确认身份间的映射，而非到 G3 才创建端口。

**身份与版本**：换卡新建 Component 并结束旧部件在位关系，保留旧工单与结果；物理槽位/位置 ID 稳定，部件端口实例不冒充旧实例。历史结果固定引用当时 component/port/mapping IDs。BDF 和 OS 名是观察值，不能充当跨重启/换件的资产主键。

**后果**：portmon-svc 消费 mon-svc 投影；issue-svc 可按机器/GPU/NIC/物理端口/BMC 关联对象；nettest/nccl 固定引用映射版本。缺失字段显示 `unknown/unmapped`，不伪造健康或已映射状态。

### ADR-09 门禁扩展与跨系统就绪握手（v2.0 新增）

**决策**：门禁从 G1–G5 扩展为 G1–G6（新增 G5 RoCE 连通性，原 NCCL 评审顺延为 G6）；LLDP 启动检查本端 G1、网络 NP-1/NP-2/NP-4；G2 三方确认后才产出 NP-3。连通性与跨机 NCCL 另检查 NP-5/NP-6；对应证明均经 FR-0.8 刷新，满足时才显示可提交；UI 不是授权边界，提交和真正派发时须在后端重查并原子冻结证据与资源锁。

**理由**：跨系统时序不同步是"假失败"的最大来源（广播无人读、打流时交换机未配置），握手把时序问题在触发前拦截。

**后果**：readiness 展示缓存最多 30s，带 observed_at、valid_until、目标范围与版本指纹。准入重查过期/变更项，事务中验证本地状态版本并预留资源；外部凭证失效则停止派发。GateEvidence 绑定目标集合、端口对、档位、映射/配置/BOM/型号/规则版本，不能用项目通过率或仅机器 state 代替（§4.4）。

### ADR-10 监控单一数据源与频次控制（v2.1 新增）

**决策**：Agent 模式交付期 D1–D6 动态数据来自 agent 心跳，Agentless 的受限 D1/D2 来自带 source 标签的 SSH 快照/低频轮询；**所有模式统一经 mon-svc ingest**；交付期 BMC/Redfish 作为带外资产与健康旁证，不与 D1–D6 混源；维护期独立 B7 告警链路见 ADR-18。采集策略由控制面下发（CollectPolicy 实体，按交付阶段 M1–M4 分级），agent 本地采样、**仅上报变化项**（delta + 心跳摘要）；测试 Execution 运行期间目标组自动切"动态窗口"高频档（D1 10s/D5 15s），结束自动回落。

**频次控制三机制**：变化上报（带宽 O(变化数)）、动态窗口提频（绑定 Execution 生命周期）、随机化时间片 + 失败指数退避（防洪峰与风暴）。cleanup 在撤销前停止带内任务和采集并确认；完成后切入 maintained，B7 轮询及告警继续（ADR-18），切换通过可重放的持久化步骤完成。

**理由**：万卡 × 高频采样若全量回传，带宽 = O(设备数×频率×payload)，管理网不可承受；变化上报把它压到 O(变化数)。单一数据源杜绝"agent 与 BMC 两套状态打架"的裁决难题。

**后果**：agentless 模式监控降级为按需拉取 + 低频轮询（FR-13.4，面板带快照时间戳标注）；**mon-svc 是唯一 ingest 路径**（v2.2 明确）：agent 心跳增量 → mon-svc ingest（持久化 MetricSample + 告警规则引擎）→ portmon-svc / AlertCenter / Dashboard 只读消费聚合视图（v2.0 中 portmon 直接消费心跳的描述废止）；ClickHouse 成为监控明细的可选但推荐存储（时序压缩）。

### ADR-11 DeviceModel 型号库与数据条目冲突防护（v2.1 新增）

**决策**：DeviceModel 为稳定型号标识，DeviceModelVersion 保存不可变硬件规格、槽位、独立期望拓扑、面板和默认角色。`ProjectAsset.model_version_id` 绑定已发布版本，Machine 当前型号为投影，模板引用确切版本。共享型号与项目型号区分 scope；修改共享型号先派生项目型号或发布新版本，不影响旧绑定。

**约束**：
1. DeviceModel `(scope_key, name)` 唯一，scope_key 显式为 global 或 project:<id>，避免 NULL 唯一键漏洞；版本 `(model_id, version)`、Slot `(model_version_id, slot_no)` 唯一。已发布版本不可编辑。
2. 已引用型号可退役：禁止新增绑定但保留旧绑定、报告和重放；有历史引用的型号/版本禁止硬删除。版本迁移须预览差异并使受影响证据失效。
3. BOM 与独立型号期望核对；BDF 排序只允许作为已验证型号适配规则，不能用实际观察值生成期望后再自证。
4. RoleBinding 固定引用型号版本与项目角色方案版本；完整性仅检查项目 `enabled_required_roles`，关闭的 storage 等角色不强制在位，`unused` 同源驱动渲染/告警/校验。必需角色无人承担时返回具体槽位差异。

**后果**：型号、部件身份、BOM、映射分别负责唯一职责；b300 二期取消存储网通过版本化项目角色覆盖完成，历史项目/报告可复现。

### ADR-12 CMDB 联邦视图（v2.1 新增）

**决策**：CMDB **不建独立主数据表**，采用联邦视图模式：CMDB 页面聚合各源模块数据（BOM/Redfish/映射/门禁/配置/工单/测试/监控归档快照），`CMDBRef` 表仅存设备→各源记录的索引引用 + cleanup 时生成的全息快照（物化归档）；维护期人工字段独立分列（`source=manual`），与自动字段物理隔离。

**理由**：若 CMDB 复制各源数据成第二主数据，必然出现双源不一致（交付期数据实时变，复制有时差）——联邦视图保证"单一事实源"原则贯穿到维护阶段；快照在 cleanup 冻结任务和源版本后物化，保存每个源版本、水位及完整性清单，等待对象/索引持久化 ACK 后才撤销互信；不假设天然没有一致性窗口。

**后果**：CMDB 查询走聚合 API（性能预算 NFR-14）；AIOps 消费走 OpenAPI + 导出（schema 版本化）；维护期按需刷新仅通过 BMC 只读白名单更新 Redfish 源记录；OS 字段保留交付快照和时间，不能使用已撤销 SSH。**查询性能策略（v2.2 补充）**：单机 360° 走并发索引查询并按 NFR-14 实测；项目级统计走**预聚合物化视图**（事件驱动增量刷新 + 每分钟全量对账），统计口径允许 ≤60s 滞后（NFR-14 统计判据以物化视图时点为准）。

### ADR-13 Agentless 交付模式为一等能力（v2.1 新增）

**决策**：平台全交付链（装机→互信→验机→配置→连通性→NCCL→退出）在无 agent 条件下可完整执行（FR-1.10 能力矩阵）；架构保障：①所有 Flow Task 定义"通道无关"执行语义（Runner 层抽象 TaskCommand，Agent Runner 与 SSH Runner 实现同一接口）；②门禁判定使用任务态采集和明确版本的新鲜度证据，不依赖常驻心跳来证明门禁通过；③监控降级显式建模（按需拉取/低频轮询 + 面板降级徽标），不隐式假装可用。

**理由**：现场 OS 兼容性问题、客户安全策略、装机窗口都可能迫使短期无 agent 交付；交付质量（门禁）不能因此放水，观测能力诚实降级。

**后果**：Runner 抽象层是硬契约（单测覆盖两通道等价性）；设备 `mode` 字段（agent/agentless）贯穿 UI 标注；agentless→agent 切换（FR-5.7）后监控自动恢复。

### ADR-14 环境抽象与离线优先（v2.1 补充）

**决策**：平台所有与外部环境交互的组件（FR-0.8 网络证据接口/第三方网络平台、BMC/Redfish、SSH 目标机、PXE 网络、NCCL/GPU 工具链、AI Provider）一律通过**环境抽象接口（Environment Adapter）**接入：
1. **接口层**：每类外部依赖定义 Go interface（如 `NetPlatformClient`、`RedfishClient`、`SSHTransport`、`TrafficGen`、`LLMClient`），业务服务只依赖接口，不依赖具体实现；
2. **实现双档**：每接口提供 `mock 实现`（内置，默认档）与 `真实实现`（连接真实环境）；`mock 实现` 内置于二进制（生产可裁剪），支持场景脚本化（延迟注入/故障注入/数据模板）；
3. **配置化切换**：环境选择全部走配置文件/环境变量（如 `KEEL_NETPLATFORM_MODE=mock|real`、`KEEL_BMC_ENDPOINTS=...`），**默认值一律为 mock/sim**；**代码中禁止硬编码任何真实环境地址**（CI 静态扫描）；
4. **预期压力驱动的设计**：在无法连接真实环境的前提下，性能相关实现按 PRD NFR 的预期压力设计（连接池尺寸、队列水位、时间片随机化、虚拟滚动分片等均有明确数值依据），并以 sim 同等数据量验证。

**理由**：硬件依赖以 mock/sim 为默认；用户授权的远端开发服务器可正常 SSH、Git、安装工具并运行开发服务，和待交付 GPU/BMC 环境是不同边界。真机 PoC 由工程所有者执行，适配器能力以契约矩阵和 PoC 证据确认；未支持设备不可宣称仅改配置就可用。

**后果**：每个外部依赖接口的 mock 实现是**必交付物**（与真实实现同 PR）；**契约测试矩阵（v2.2 补充）**：每个接口 PR 必含 mock/real 双实现的等价性断言矩阵（接口签名 / 错误码 / 超时语义 / 幂等性四维）+ **mock 数据生成器**（场景脚本化：延迟注入/故障注入/数据模板，随接口交付、不后补）；ADR-13 的 Runner 抽象是本决策在执行通道上的具体实例（Agent Runner / SSH Runner 之外，mock Runner 用于纯离线 CI 快速档）。

### ADR-15 PCIe 拓扑模型：sysfs 上游路径 + nvidia-smi topo 语义对齐（v2.3 新增）

**决策**：GPU-NIC PCIe 拓扑验证（PRD FR-4.8）以独立快照实体建模：
1. **计算口径**：拓扑按 **sysfs 上游路径**计算（`/sys/bus/pci/devices/<BDF>/` 沿 `physfn`/上游 bridge 链遍历至 Root Complex），每 GPU/NIC 产出"上游路径链（BDF 链）+ NUMA 节点"；NVLink 信息**只取自 nvidia-smi**（拓扑/链路数/状态），不做替代推断；LnkCap/LnkSta 从 PCIe capability 配置空间读取（setpci/lspci 口径）；
2. **语义对齐断言**：平台拓扑计算的 affinity 表达（PIX/PXB/PHB/SYS、GPU-NIC 亲和）**必须与 `nvidia-smi topo -m` 输出含义逐项一致**——sim 环境以 mock nvidia-smi 输出为行为规格（ADR-14）；真机一致性由 PoC 执行包 #P2 校验；
3. **对比引擎**：预期拓扑来自独立发布的 DeviceModelVersion.expected_topology（槽位/上游结构/GPU-NIC affinity/NVLink），实际来自采集快照，`TopologyDiff` 按四维比对（挂载归属/NUMA 分布/NVLink 矩阵/LnkSta 带宽协商）；
4. **T7 处置闭环**：拓扑差异 → T7 工单 → 按证据与型号能力选择配置修复、冷复位、经批准固件操作或硬件检修 Runbook → 重跑验证；BIOS 刷写不是所有拓扑故障默认动作。

**理由**：拓扑混乱（BIOS PCIe 枚举错位/训练降带宽）会污染后续所有映射与线序结论——把它前置于线序核查 Step 1，保证线序验证建立在正确的设备内拓扑上。

**后果**：mapping-svc 增加拓扑对比引擎；validation-svc 流程编排增"拓扑验证"环节（G1 后、G2 前）；采集脚本新增拓扑采集器（mock 优先，ADR-14）。

### ADR-16 BMC 控制平面：双源标注与通道矩阵（v2.3 新增）

**决策**：BMC 域功能（PRD FR-16）由 redfish-svc 扩展承载（不新建服务——BMC 通道已有适配器层 ADR-14），关键设计：
1. **双源标注**：实施期同一字段可能来自 agent 心跳（带内）与 BMC Redfish（带外）——展示层字段级标注 `source=agent|bmc`，**不混源不裁决**（实施期带内为准展示，带外为旁证；维护期带外为唯一源）；
2. **状态灯抽象层**：ServerLampPanel 的灯语义统一映射平台状态色（电源/健康/UID/BMC 心跳/网卡灯），BMC 厂商差异由适配器吸收（FR-16.5）；不支持的 NIC 灯/传感器显示 unsupported，失联显示 unknown，不推断为正常或断电；
3. **控制通道矩阵**：电源/UID 控制 = BMC 带外（Redfish Actions）；BIOS 设置 = 带内 biossetting 工具 或 Redfish BIOS attribute patch（双路径，按设备能力选择，能力矩阵入型号库）；BIOS/BMC/CPLD/HGX SBI/NVSwitch 固件按型号版本 capability matrix 选择 Redfish UpdateService、带内厂商工具或人工路径；CPLD 不预设通道；**维护期仅带外通道可用**（矩阵中带内通道对 maintained 态禁用）；
4. **动作发现**：读取 Redfish ActionInfo / AllowableValues，映射 On、GracefulShutdown、GracefulRestart、ForceRestart 等实际支持值；Nmi 为独立诊断动作，不映射普通 reboot，不使用不存在的 NmiReset。ForceOff 等强制动作须独立能力与审批。
5. **固件升级联动**：影响拓扑或数据面的固件变更使相关 G2–G6 证据失效，按范围重验。每种操作声明可取消点、查询方式和回滚能力；超时先对账任务与设备版本，未知状态禁止自动重刷。
6. **配置恢复**：可能断开管理网的配置使用 target-side watchdog 的 confirmed-apply：先备份、限时应用、独立新连接验证、确认后取消回滚；不支持的设备采用审批过的带外恢复方案。BIOS/固件不默认支持此类回滚。

**理由**：BMC 是唯一贯穿实施与维护期的通道；控制能力差异大（厂商/OEM action 各异），抽象层吸收差异是既有 ADR-14 模式的复用。

**后果**：BmcClient 接口扩展（LED/电源/固件/BIOS patch），mock 实现同 PR 交付（ADR-14 约束）；维护期监控成为 BMC 域消费方（ADR-18）。

### ADR-17 项目执行与双平面（v2.3 新增）

**决策**：平台引入 Project 实体与双平面信息架构（PRD FR-17）：
1. **项目域从 P1 建立**：Project、成员/角色授权及项目归属在业务表前建立。部署页 URL 明确 project_id，请求显式携带，后端按用户权限与资源实际归属交叉校验。session 最近项目仅为入口偏好，不能决定已打开请求的归属。
2. **多标签页隔离**：页面 ProjectContext 从 URL 派生；Query key、SSE、筛选、草稿及确认框含 project_id。切换只导航当前标签页，其他标签页表单保留原项目，提交时展示并校验冻结的项目/目标集合。
3. **平面划分**：部署平面按项目操作；运维平面可跨用户获授权项目聚合。默认项目过滤为可见且可移除的 URL 条件，清除它不授予额外权限。
4. **项目内顺序性**：G1–G6 消费 GateEvidence；项目百分比只用于展示，不作为批次准入。Project 状态 `planning → deploying → verifying → delivered → archived` 独立于设备状态，转移按版本 CAS 与证据检查。

**理由**：多项目并行交付（b300 两期=双项目）需要项目级隔离与切换；横向能力（告警/工单）跨项目本质不应随项目切换而隐藏。

**后果**：api-gateway 增项目上下文中间件（项目级 RBAC + 数据域过滤）；前端路由结构重构（DS §3）；项目字段、索引、后台任务与导出权限在 P1 建立；Machine 为全局物理身份，ProjectAsset 记录项目使用/交付历史，禁止同一设备被多个活跃项目无授权争用。

### ADR-18 维护期 BMC-only 监控（Redfish 主通道 / IPMI 兼容回退）

**决策**：cleanup 后维护期监控（PRD FR-9.7/13.6）：
1. **通道事实**：agent 卸载 + SSH 互信撤销后，带内通道物理消失——维护期监控唯一通道 = BMC（Redfish/IPMI），这是通道能力的客观事实而非设计选择；
2. **采集模型**：mon-svc 调度 BMC 轮询 Execution（复用作业平台 fanout，BmcClient 只读接口），采集传感器/SEL/Health/电源状态入 MetricSample（dtype=B7 带外硬件类）；默认 5min/台（`mon.maintained_bmc_interval`）；
3. **负载量纲**：10k 台每 300s 轮询一次需约 33.3 次设备轮询/s；每台 k 个请求、每请求 2s 时并发约 66.7k，设备数不能当作请求数。默认并发上限 128，记录完整设备轮询耗时/请求数/重试/尾延迟并验证 5min 新鲜度；超预算显示 stale 并告警，不静默拉长至 52min。可按能力批量读取、缓存、分片或经审批扩容。
4. **告警与工单**：带外告警规则进 Alert（source=bmc），触发开 phase=post-delivery 工单；**[I-8] 修订适用**：维护期白名单 = BMC 自动只读轮询、人工 BMC 只读刷新、人工 UID 控制，以及有独立权限和审批的 FR-16.5 电源动作；其余带内 Flow/采集/测试/刷写仍过滤。BMC 配置或固件变更需显式授权的维护变更流程，不能借监控白名单执行；
5. **权限最小化**：维护期仅保留授权范围内 BMC 凭据引用；平台持有的目标机临时 OS 凭据随互信撤销清除（凭据生命周期与通道生命周期绑定）。

**理由**：交付后客户环境的平台残留必须为零（带内通道全部收回），但硬件故障感知能力是交付承诺的一部分——BMC 带外是唯一合规通道。

**后果**：mon-svc 增维护期调度器分支；CleanRun 完成事件触发"监控模式切换"（带内停采 + 带外轮询启动）；NFR-16 定义其预算。

### ADR-19 网络底座交付域：前置域承载与证据接口（v3.0 新增）

**决策**：PRD §3.0 FR-0 网络底座交付域（历史代号 NDP，现 `keel-fabric`）作为**服务器交付的前置条件域**由本平台自建承载，不另立系统、不并入第三方平台：
1. **服务划分（同一模块化单体内，包边界）**：`fabric-svc`（物料/端口建模、网络规划、ZTP 开通编排、意图渲染、Doublecheck、比对、预检、PushJob 下发与复采）、`fabricmon-svc`（交换机侧采集：LLDP/FDB/ARP/DDM/PFC/ECN/CNP/队列/缓冲/WJH + syslog 归一化 + LL-01～LL-07 判定 + 健康分）、`netbaseline-svc`（网络配置基线导入承接 + pongo2 沙箱渲染，可与 render-svc 共用模板引擎适配层）；`boot-svc`（Kea/引导服务/身份反查，option 239/143/60-43）。**复用既有横向能力**：catalog-svc（参数目录，阈值单一源）、model-svc（网络设备型号同库）、audit-svc、notify-svc、report-svc。
2. **证据与排障接口域（FR-0.8）为服务器域唯一消费入口**：就绪状态、LLDP 邻居回读、线缆校验结论、无损与打流计数、光模块与日志证据、交付基线导出；第三方网络平台以**适配器**实现同一契约，不可提供的能力返回 `unknown`，禁止空成功。
3. **只读与审批边界**：网络配置变更走 `意图承接 → Doublecheck → 备份采集 → 比对 → 修复计划 → Batfish/预演 → 审批 → 下发 → 复采验证` 主链路，任何环节不得绕过审批直下发；无损调优只输出建议，禁止自动改写设备配置；对设备视图只读。
4. **模板引擎**：网络配置模板用 **pongo2 v6**（Django/Jinja 语法兼容，与 ConfigSet 的 Go template 语法不兼容，见 PRD 附录 C-10），须在**沙箱模式**（`BanTag`/`BanFilter`）执行，禁止模板内文件/网络访问。
5. **状态机**：ZTP 引导状态机 `上电→DHCP(DORA)→获取引导 URL(239/143)→身份上报/证书校验→身份反查→拉取引导数据→最小隔离/管理配置导入→服务重启→可管理`；引导不下发生产网络配置，生产配置必须经审批 PushJob；PushJob 13 态见 §4.1。

**理由**：网络配置的方案与内容由客户与网络工程师商定（Out of Scope），平台只承接"导入→版本化→渲染→校验→下发→比对闭环"；把网络能力收敛为同一平台的**前置域 + 证据接口**，既满足服务器域对网络就绪的硬前置（NP-1～NP-6），又避免第二套判定枚举与两处各自演化。

**后果**：新增 `bom_item` 单表（双域物料）、`BOMImportPackage`/`BOMImportManifestFile`、网络侧设备/端口/线缆/光模块身份、`FabricRule(LL-01~07)`、`PushJob`、`HealthScore`、`ZtpSession`、`FabricReadiness` 等实体（§4）；FR-0.8 契约为服务器域 G2/G5/G6 的硬依赖；网络设备型号进 model-svc（`kind` 增 `switch`）；交换机凭据走 vault 凭据批次模型（与 BMC 同族）。

### ZTP 身份底座（WP-1.17 增量）

标准交换机 BOM 可声明 `mgmt_mac`、`base_mac`、分号分隔的 `other_macs`。预检规范化永久 MAC，并阻断同包服务器端口、其他交换机和既有 SoT 绑定冲突；当前绑定在 `switch_mac_bindings` 中按角色唯一存放，历史由 BOM 修订快照保留。新 BOM 要更新身份时须同时声明三列；完全不带身份列则继承当前绑定，保持 `bom_items.mac_list` 与绑定表一致。只读身份反查按 SN→管理口 MAC→基板 MAC→其他 MAC 的优先级解析；任意键指向不同交换机、已知 SN 与 MAC 绑定不一致时返回 conflict，未命中或缺少必要 SoT MAC 为 isolated。预检 API 仅供项目内规划/仿真，绝不返回配置或推进 managed。项目内 `ZtpOnboardBoard` 可查看最近仿真会话并人工驱动步骤；ZTP 仿真会话已持久化 `powered → dhcp → booturl → identity → lookup → fetch → config → restart → managed` 的逐步证据时间线，支持状态版本并发校验、失败三段式记录与指数退避窗口（默认最多 3 次重试，可从参数目录 `fabric.ztp_retry_max` 读取 0–10 次）；未命中/冲突身份在 lookup 后进入 isolated/conflict，不能继续导入配置。会话创建时快照身份反查结果，后续步骤不重新校验 SoT；`sim.managed` 只表示仿真路径结束，不写交换机真实可管理状态，也不产出 NP-2/FabricReadiness。239 设备端点、143 mTLS/IDevID、Kea、自动设备重试、隔离模板和最小管理配置尚需独立实现与验证。

### 端口类型从型号版本派生（WP-1.15 增量）

标准 BOM 中的服务器及交换机端口 `sub_type` 均从父设备已发布型号 `capability_matrix.ports[port_index].sub_type` 派生；breakout 子口允许在没有显式子口项时继承父口 `child_sub_type`。未声明或超出物料字段长度为 BLOCK；预检以固定元数据展示派生来源，避免按百万端口生成 INFO 问题行。宽表兼容入口的临时 MAC/NIC 身份保留 `legacy-port:<port_index>` 标记，等待真实型号接管。

### 交换机端口型号能力矩阵（WP-1.16 增量）

已发布 `device_model_versions.capability_matrix.ports` 以端口 `port_index` 为键；单端口可声明 `speed` 或 `speeds[]`，支持 breakout 的父口另列 `breakout_modes[]`（如 `4x200G`）。导入预检逐端口查已绑定型号版本；`swp1s0` 等子口可继承父口 `swp1` 的能力，但子口编号必须落在 breakout 数量内，计划速率必须与模式速率一致。缺端口能力或声明的模式、速率不匹配均为 BLOCK。光模块可由 `fabric/optic_modules.csv.speed/media_type`、已发布光模块型号 `capability_matrix.speed/speeds/media_types`（未填时从 `hw_spec` 取）和交换机端口 `capability_matrix.ports[].optic_media_types` 比对；明确不匹配为 BLOCK，无法确认速率为 WARN `optic_speed_unverified`，不得形成网络就绪 PASS。服务器计算端口 `rail` 必填，按整机汇总去重后与已发布服务器型号 `expected_topology.rail_count` 比较；缺型号 rail 数记 WARN，数量不符或计算端口缺 rail 为 BLOCK。网络侧工程预检增补 `fabric/switches.csv` 的 `planned_psu_count`、`planned_feed_count`、`power_budget_w`、`max_downlink_uplink_ratio`，以及 `fabric/switch_ports.csv` 的 `rail`、`bandwidth_direction`。电源计划值分别对已发布交换机型号 `hw_spec.psu_count/feed_count/max_power_w` 作下界比较；缺任一侧记录 WARN/unknown，明确不足或无效数值 BLOCK。交换机计算口 rail 缺失 BLOCK，Leaf 的已声明 rail 去重数与型号 `expected_topology.rail_count` 核对，缺期望值 WARN。带宽比按交换机汇总有效上下行端口速率（M/G/T 换算为 Gbps），breakout 父子同时存在只计子口。项目内 `network_baseline_versions` 承接外部网络方案的工程限值子集，草稿/批准版本不可变；批准需登记客户和网络工程师批准证据引用并审计。BOM 的 `network_baseline_ref` 指向已批准版本，限值按交换机 SN 优先、角色回退；BOM 自声明限值不得宽于批准限值，超限 BLOCK。未绑定、未覆盖或缺测仍为 WARN/unknown。批准证据引用尚未自动验真，且该子集不包含完整配置模板，因此不能给 FR-0 完整工程 PASS。缺方向/速率/上行或下行容量也为 unknown/WARN。同包线缆两端可唯一解析时，服务器计算口与交换机计算口的规划 rail 不同为 BLOCK；交换机角色缺失为 WARN，角色明确不符为 BLOCK。外部 UID 端点尚不参与此对照。计划 rail 与真实线序的相符性仍须 G2 LLDP 三方确认；本段不构成 FR-0.1.3 完整验收。

### ADR-20 BOM 导入包：单批次目录形态与幂等（v3.0.2 新增）

**决策**：BOM 导入采用**目录/压缩包形态的导入包**（`manifest.json` + 逐实体 CSV，PRD 附录 A.5–A.8）作为标准形态，单文件宽表作为兼容形态：
1. **一次导入 = 一个 `bom_import_batch_id`**：包内服务器物料与网络物料**同批提交**，避免分两次导入后靠人工对齐连接关系；`manifest.json` 声明 schema 版本、批次号、项目、项目模板版本、逐文件列清单与 `content_hash`。
2. **解析管线（两段式，对齐 FR-2.1.6）**：`只读预检（格式/一致性/工程三段 → 问题清单）` → `单事务提交（幂等 upsert）`；预检阶段零写库，问题按**文件 + `bom_line_no` + 字段**三级定位，可导出 CSV 回传。
3. **身份与幂等**：导入即分配稳定 `Machine/Component/PhysicalPort` uid；`(category, serial)`、线缆两端排序 uid、`(parent_uid, port_index)` 为业务唯一键；同批次号且摘要相同的重复导入幂等，同号异摘要拒绝覆盖；新版本用新 seq 按 diff 增量并保留版本链。摘要按 PRD 附录 A.6 排除 manifest 自身并覆盖所有声明文件。
4. **兼容**：宽表按 `sn` 折叠为 server + N 部件/端口（固定折叠规则）；仅有 MAC 而无 NIC 序列号时以 `LEGACY-MAC-<mac>` 标记临时 NIC 身份，后续标准包凭相同永久 MAC 补入真实序列号；多临时身份映射到同一 NIC 时 BLOCK 并人工核对；网络物料（breakout/线缆两端）必须走目录形态；标准包七个逐实体 CSV 均声明且存在，server_ports.csv 逐口记录永久 MAC 并与部件 MAC 汇总交叉核对；服务器单文件宽表只作兼容入口。
5. **校验边界**：model-svc 提供型号期望值（服务器与网络设备同库），导入侧调用；catalog-svc 仅管参数目录，不重叠（§11 注记）。

**理由**：BOM 是全平台物料唯一事实源（§1.3 单一事实源），导入形态必须能一次承载双域物料与连接关系，否则线缆两端 uid 与三方比对无从锚定；目录形态同时给设计院/网络团队提供可回传的定位粒度。

**后果**：`BOMFile/BOMRecord` 被 `BOMImportPackage` + `BOMItem`（单表）取代/扩展（保留历史表兼容）；解析器、问题清单导出、幂等键入 P1；模型层与 API 见 §4/§4.2。

### 契约冻结约束

ADR 一旦实施，接口契约（proto / OpenAPI / SSE 事件 schema）冻结于 `api/` 目录，前后端共享契约源（契约先行）。

---

## 4. 数据模型（核心实体）

```text
User(id, identity_provider, subject, display_name, local_password_hash nullable, enabled)
Session(id_hash, user_id, csrf_secret_hash, issued_at, last_seen_at, expires_at, revoked_at)
Project(id, name, profile_version_id, state[planning|deploying|verifying|delivered|archived],
        enabled_required_roles[], state_version, created_by, delivered_at, archived_at)
ProjectMember(project_id, user_id, role, UNIQUE(project_id, user_id, role))
ProjectAsset(id, project_id, machine_id, model_version_id FK, valid_from, valid_to, assignment_state)
BOMImportPackage(id, project_id, bom_import_batch_id, schema_version, profile_version_ref,
                 content_hash, UNIQUE(project_id, bom_import_batch_id),
                 declared_by, network_baseline_ref nullable, credential_batch_ref nullable,
                 row_count, uploaded_by, precheck_state[parsing|precheck|ready|blocked|committed],
                 precheck_report_ref, committed_at)
BOMImportManifestFile(package_id, domain[server|fabric], file_path, entity, columns[], row_count, file_hash)
BOMImportIssue(package_id, file_path, bom_line_no nullable, field nullable, severity[BLOCK|WARN|INFO],
               code, message, resolved_at)
BOMItem(id PK, item_uid UNIQUE, project_id, bom_import_batch_id FK, bom_line_no,
        category[server|switch|nic|gpu|psu|bmc|port|optic_module|cable|other], sub_type,
        model_version_ref FK, vendor, serial, position jsonb, parent_uid FK nullable,
        endpoint_a_uid FK nullable, endpoint_b_uid FK nullable, port_index nullable, port_name nullable,
        port_role nullable, rail, pod, role, mac_list jsonb, credential_batch_ref nullable,
        expect_version jsonb, source_of_record[design_bom|discovered|manual],
        status[planned|imported|verified|installed|retired], valid_from, valid_to)
-- 兼容保留（v3.0.2 起为历史表，宽表导入写入 BOMItem）
BOMFile(id, project_id, version, uploaded_by, validated_at, status)
BOMRecord(id, bom_file_id, rack, unit, sn, mac[], hostname, rail, ip_hint, bmc_cred_batch, status)
Machine(id, sn, hostname, rack, unit, state, state_version, mode[agent|agentless],
        active_project_asset_id nullable, model_version_id [当前绑定投影], ssh_trust[uninitialized|established|broken|revoked],
        host_key_fp, bmc_ip, os_info, driver_ver, collected_at, freshness)
Component(id, machine_id, type[gpu|nic|bmc|board|other], serial, physical_slot,
          installed_at, removed_at, replaces_component_id nullable)
PhysicalPort(id, machine_id, component_id, physical_slot, port_no, installed_at, removed_at)
-- 网络底座域实体（v3.0，ADR-19）
Switch(id, item_uid FK→BOMItem, sn, hostname, dc, pod, role[leaf|spine|superspine|mgmt], rack, idx,
       model_version_id FK, nos_version, ztp_state[powered|dhcp|booturl|identity|lookup|fetch|config|restart|managed|failed],
       config_state[IN_SYNC|DRIFT_CRITICAL|DRIFT_MAJOR|DRIFT_MINOR|UNKNOWN], last_snapshot_at)
SwitchPort(id, switch_id FK, item_uid FK→BOMItem, port_index, port_name, breakout_parent nullable,
           speed, breakout_mode, port_role[management|storage|compute|oob|unused], operational_state)
FabricReadiness(id, project_id, scope_type[fabric|pod|switch], scope_ref, config_push_state, lldp_readable,
                readiness_at, ttl_seconds, version_fingerprint, np_1, np_2, np_4)
CableCheck(id, project_id, phase[network_precheck|g2_confirmed], port_pair_ref, bom_version,
           switch_lldp_ref nullable, host_lldp_ref nullable, verdict[PASS|FAIL|BLOCKED|WAIVED],
           rule_version_ref nullable, collected_at, valid_until, evidence_refs[])
-- N-S5 仅记录交换机互联/服务器接入口预校验；G2 Step 2/3 三方确认后才写 PortModel 并产出 NP-3。
NetworkIntent(device_id FK→Switch, revision, rendered_config_ref, config_section_set[], source_baseline_ref,
              rendered_at, created_by)
NetworkSnapshot(id, device_id FK→Switch, revision, running_config_ref, taken_at, trigger[scheduled|ondemand|pre_push])
NetworkDiff(id, device_id FK→Switch, intent_revision, snapshot_revision, severity[Critical|Major|Minor],
            section, diff_json, excluded_noise[], created_at)
PushJob(id, project_id, scope_type[batch|role|device], target_set_hash, intent_revision_set[],
        precheck_report_ref, approval_ref nullable, canary_device_id nullable, concurrency,
        state[CREATED|PRECHECKING|BLOCKED|READY|APPROVED|REJECTED|EXECUTING|VERIFYING|COMPLETED|PARTIAL|ROLLING_BACK|ROLLED_BACK|ARCHIVED],
        state_version, rollback_snapshot_ref, started_at, finished_at)
ZtpSession(id, switch_id FK, boot_channel[239|143|60-43], identity_priority[sn|mgmt_mac|base_mac|other],
           observed_identity, conflict_state[none|sn_dup|mac_mismatch|binding_conflict], started_at, managed_at)
FabricRule(rule_id, domain[lossless|ddm|log|precheck], name, scope jsonb, version, window_seconds,
           comparator, hit_threshold, observe_threshold, severity, safe_range, state[draft|released|disabled],
           parent_version nullable, change_reason, approved_by[], created_at)
FabricRuleFinding(rule_id, rule_version, device_id, port_id nullable, priority nullable, metric_key,
                  window_start, window_end, value, verdict, approximation_flags[])
HealthScore(id, scope_type[device|pod|fabric], scope_ref, score, device_min, weights_ref, computed_at,
            input_rule_versions[], degradation_flags[])
OpticModule(id, item_uid FK→BOMItem, port_id FK→SwitchPort, model_version_id FK, serial, vendor, media_type,
            speed, ddm_rx_dbm, ddm_tx_dbm, ddm_voltage_v, ddm_temp_c, ddm_bias_ma, ddm_status[normal|warning|critical|unknown],
            ddm_source[switch|host], observed_at)
FabricBaselineExport(id, project_id, revision, scope_ref, json_schema_version, consistency_check_rate,
                     generated_at, evidence_refs[])
TrustLog(id, project_id, machine_id, action[bootstrap|verify|repair|revoke], credential_ref, detail, at)
CollectRecord(id, project_id, machine_id, phase, payload_raw_ref, payload_struct,
              observed_at, received_at, source, version, freshness_status)
GateEvidence(id, project_id, gate[G1..G6], target_set_hash, targets[], port_pair_coverage[],
             test_profile_version, bom_version, model_versions[], mapping_revision_ids[],
             config_versions[], role_plan_version, rule_version, topology_versions[],
             source_evidence_refs[], observed_at, valid_until, revision_fingerprint,
             verdict[PASS|FAIL|BLOCKED|WAIVED], validity[fresh|stale|invalidated], quality, invalidation_reason,
             -- 网络前置证据扩展（v3.0，PRD §1.5.3/§1.5.4）
             network_readiness{fabric,pod,switch_ids[],readiness_at,ttl_seconds,source} nullable,
             network_readiness_ref nullable, network_evidence_refs[] nullable,
             fabric_stage[N-S0..N-S9] nullable, network_degradation[] nullable)
GateRecord(id, project_id, machine_id nullable, gate, evidence_id, judged_at, waiver_id nullable)
GateWaiver(id, project_id, evidence_id, exact_scope, missing_conditions[], approved_by,
           reason, valid_until, compensating_controls, revoked_at)
MappingRevision(id, project_id, machine_id, version, model_version_id, bom_version,
                topology_snapshot_id, role_plan_version, port_bindings jsonb, confirmed_by, created_at)
PortModel(physical_port_id PK, project_id, mapping_revision_id nullable, mapping_state,
          os_ifname nullable, perm_mac nullable, pci_bdf nullable, sw_port_id nullable,
          rail, role, dom_capable, status[up|down|degraded|oos|unknown], speed, last_observed_at)
PortEvent(id, physical_port_id, mapping_revision_id nullable, event_type, payload, at)
Issue(id, project_id, source[topology|g2|fabric|g5|g6|portmon|bmc],
      responsible_domain[host|fabric|unknown], category[T1..T7],
      machine_id, component_id nullable, physical_port_id nullable, phase[delivery|post-delivery],
      state[open|assigned|in_repair|verify|closed|reopened], state_version,
      evidence_refs[], action_taken, replaced_component_ids[], verify_exec_id,
      opened_at, closed_at, assignee)
ConfigSet(id, project_id, name, version, schema, template, created_by, status)
ConfigApply(id, project_id, configset_version, target_set_hash, param_snapshot_id,
            previous_state_ref, confirmation_deadline, rollback_capability, result, applied_at)
MediaItem(id, scope_key, type, name, version, sha256, signature_ref, uri_ref, hw_scope,
          os_scope, arch, deps jsonb, manifest_ref,
          state[uploaded|verified|approved|retired], size, uploaded_by, approved_by)
MediaDistLog(media_id, exec_id, machine_id, method[push|http|repo], result, at)
ParamKey(key, scope_key, value_type, options[], default_value, source_media_type, profile_id)
ParamSnapshot(id, exec_id, params jsonb, dependency_manifest_hash, frozen_at)
Flow(id, project_id nullable, namespace, name, enabled)
FlowVersion(id, flow_id, version, yaml_ast, content_hash, published_at, created_by)
Execution(id, project_id, flow_version_id, target_set_hash, param_snapshot_id,
          admission_evidence_ids[], state, state_version, paused_at, started_at, finished_at)
ExecutionTarget(exec_id, machine_id, state, output_ref, log_ref)
TaskAttempt(id, exec_id, target_id, command_id, attempt_no, runner, state,
            lease_owner, lease_until, fencing_token, receipt_ref, observed_result, state_version)
TaskReceipt(command_id, attempt_id, target_id, received_at, started_at, finished_at,
            exit_code, process_identity, result_digest, capability, last_observed_at)
ResourceLease(resource_type, resource_id, mode, owner_attempt_id, fencing_token,
              expires_at, state[held|reconciling|released])
OutboxEvent(id, aggregate_id, aggregate_version, type, payload, created_at, published_at)
ConsumedEvent(consumer, event_id, handled_at, UNIQUE(consumer, event_id))
NetTestRun(id, project_id, scope, rail_filter, tier_plan, param_snapshot_id, evidence_id, report_ref)
NetTestPairResult(run_id, src_physical_port_id, dst_physical_port_id,
                  mapping_revision_ids[], tier[1..5], verdict, metric_refs[], path_ref)
NCCLTestRun(id, project_id, scope, coverage_mode, group_plan, matrix jsonb,
            evidence_id, report_ref, verdict, tuning_list jsonb)
GroupRun(id, run_id, group_id, members_and_ranks[], world_size, resource_reservation_id,
         rendezvous_ref, barrier_deadline, attempt_no, communicator_state, collective_results_ref)
NCCLIssueLink(run_id, group_run_id nullable, issue_id, evidence_ref)
DeviceModel(id, scope_key, name, state[active|retired], UNIQUE(scope_key, name))
DeviceModelVersion(id, model_id, version, hw_spec jsonb, expected_topology jsonb,
                   capability_matrix jsonb, panel_layout jsonb, bom_expect jsonb,
                   default_role_plan jsonb, content_hash, published_at, UNIQUE(model_id, version))
DeviceModelSlot(model_version_id, slot_no, pcie_slot, expect_nic_model, panel_port,
                default_role, UNIQUE(model_version_id, slot_no))
RoleBinding(project_id, role_plan_version, model_version_id, slot_no,
            role[management|storage|compute|oob|unused], phase_note, updated_by)
CollectPolicy(id, project_id, scope, stage[M1..M4|maintained], intervals jsonb,
              freshness_limits jsonb, bandwidth_budget, dynamic_window, backoff, version, active)
MetricSample(id, project_id, machine_id, dtype[D1..D6|B7], metric_key, value, unit,
             source, observed_at, received_at, freshness_status, method, scope, quality)
AlertRule(id, version, expression, threshold, dtype, severity, suppression_policy, published_at)
Alert(id, project_id, machine_id, physical_port_id nullable, rule_version_id, source,
      level[info|warning|critical], state[firing|suppressed|resolved], first_at, last_at, count, issue_id)
CMDBRef(project_asset_id, source, source_ref, source_version, snapshot_ref nullable, synced_at)
CMDBSnapshot(id, project_asset_id, archive_ref, manifest_hash, source_watermarks, archived_at, clean_run_id)
CMDBManualField(project_asset_id, field_key, value, source[manual], updated_by, updated_at)
OwnershipRecord(id, project_id, machine_id, exec_id, object_type, exact_locator,
                owner_marker, original_state_ref, created_at, removed_at)
CleanRun(id, project_id, scope, checklist_version, step_state, source_watermarks,
         archive_ack[pre_cleanup|residue|final_technical], residue_report_ref,
         trust_revocation_receipt, technical_state, signoff_ref, state_version)
AIProvider(id, scope_key, type[openai_compatible|anthropic|vllm], endpoint, model, key_ref, limits)
KnowledgeDocument(id, scope_key, source_ref, version, classification, content_hash, approved_at)
KnowledgeChunk(id, document_id, ordinal, text_ref, embedding_model, embedding_version, vector)
AIInvoc(id, project_id, actor, action, context_refs[], citation_refs[], provider_id, result_ref, tokens)
ProjectProfile(id, scope_key, name, version, bom_mapping, param_defaults,
               device_model_version_refs[], blueprint_versions[], gate_policy_version, kb_bundle, status)
AuditLog(id, project_id nullable, actor, action, target, detail_ref, trace_id, at)
PciTopologySnapshot(id, project_id, machine_id, version, model_version_id, gpu_nic_map,
                    upstream_paths, numa_map, nvlink_matrix, lnk_sta, raw_output_ref, verdict, created_at)
TopologyDiff(id, snapshot_id, expect, actual, category[pcie_misplaced|numa_anomaly|nvlink_missing|lnk_degraded],
             detail, issue_id nullable)
StepReport(id, project_id, machine_id, batch_id, step[1|2|3|4], verdict, diff_ref,
           issue_ids[], snapshot_ref, operator, at)
BiosConfigSet(id, project_id, name, version, model_version_id, attrs, state, created_by)
BiosApply(id, project_id, biosconfigset_version, scope, mode[agent_ssh|redfish_patch], result, diff, applied_at)
FirmwareRun(id, project_id, component[bios|bmc|cpld|hgx_sbi|hgx_nvswitch], media_ref, scope,
            capability_version, from_version, to_version, remote_task_ref, state,
            rollback_supported, canary_result, applied_at)
```

关键字段说明：
- `Machine.ssh_trust`：SSH 任务派发准入字段（ADR-06）；普通带内采集要求 established；初始化/修复例外不能代替门禁通过；
- `PhysicalPort` 是早于 G2 的身份；`PortModel` 是当前投影；`MappingRevision` 是 G3 确认后的不可变映射，三者不能混用（ADR-08）；
- `MediaItem.deps`：介质依赖图（驱动↔内核↔CUDA 版本），参数目录依赖校验的数据源；
- `ParamSnapshot`：Execution 冻结的参数快照，"当时用什么版本"的永久证据；
- `Issue`：统一故障档案，`source` 标记来源、`phase` 区分实施期/交付后（v2.3）、`category` T1–T7 分类、`physical_port_id` 锚定 PhysicalPort，也可仅关联 Component 或 Machine；
- `PciTopologySnapshot/TopologyDiff`（v2.3）：设备内拓扑的版本化证据链，T7 工单与 FR-16.6 固件升级重验的数据源（ADR-15）；
- `StepReport`（v2.3）：四步线序核查的逐步结果记录（FR-4.2），G2 门禁详情页时间线数据源；
- `Project`（v2.3）：项目执行主体（FR-17.3 生命周期状态机），ProjectContext 的持久层（ADR-17）。
- `BOMImportPackage`/`BOMItem`（v3.0.2）：导入包为两段式提交（预检只读 → 单事务写库），`BOMItem` 是全平台物料唯一事实源，双域（服务器/网络）同表同编号规则；稳定 `Machine/Component/PhysicalPort` uid 在提交时分配（ADR-20、PRD 附录 A）；
- `Switch/SwitchPort/OpticModule`（v3.0）：网络物料稳定身份，`SwitchPort.breakout_parent` 表达子口归属，端口唯一键 `(switch_id, port_index)`；`OpticModule.ddm_source` 字段级标 `switch|host`，不混源（PRD 附录 C-11）；
- `FabricReadiness`（v3.0）：就绪证明带 `readiness_at`/`ttl_seconds`/`version_fingerprint`，`gate.readiness_ttl=30s`，执行前必须刷新（NP-4）；
- `NetworkIntent/NetworkSnapshot/NetworkDiff/PushJob`（v3.0）：网络配置闭环四件套；`PushJob` 13 态状态机为唯一定义（PRD FR-0.4.8）；每次下发前自动快照，失败回滚到快照；
- `FabricRule/FabricRuleFinding`（v3.0）：告警规则版本化（`rule_id@version`），禁止物理删除（仅 `disabled`），判定产物携带 `rule_id@version`；规则调整不改变采集计划（NFR-20、PRD 附录 C-06）；
- `HealthScore`（v3.0）：设备/端口/优先级加权健康分，`device_min` 为 fabric 健康门禁的 min 口径（PRD FR-0.6）；
- `GateEvidence.network_readiness`（PRD FR-4.3 对象字段）及内部 `network_readiness_ref`、`network_evidence_refs[]`、`fabric_stage`、`network_degradation[]`：服务器域消费网络前置证据；`fabric_stage` 取值 `N-S0..N-S9` 仅作证据标签。NP-3 由 G2 的 CableCheck `g2_confirmed` 产出，不随 N-S5 预校验伪造。

### 4.1 状态机（v2.0 扩展）

**Machine（当前项目生命周期投影）**：`unknown → provisioning → provisioned → trust_established → collected(G1) → lldp(G2) → mapped(G3) → configured(G4) → netverified(G5) → tested(G6) → maintained`
- **`netverified` 态**：G5 通过后的展示里程碑；NCCL 准入实际检查目标组当前有效的范围证据，`tested` 后可调优重测，不能只做字符串相等判断；
- **`maintained` 态**：技术清理、分阶段归档 ACK 和凭据撤销证据齐全后进入（FR-9.6），不等待客户签署；带内任务/采集停止、相应规则停用，B7 监控/告警继续。仅 ADR-18 白名单可执行，CMDB 可查询/维护人工字段/BMC 只读刷新；客户签署另行控制 Project.delivered；
- 门禁失败停留当前态并标记 `blocked(reason)`；豁免可跳过单门禁（审计）；
- **blocked 原因枚举（v2.2 补充）**：`gate_pending`（门禁待评估）/ `gate_failed`（门禁失败）/ `waiver_expired`（豁免过期，见不变式 [I-9]）/ `trust_broken`（互信中断）/ `readiness_gap`（就绪握手缺口）；
- **态转换语义（v2.2 说明）**：门禁通过解锁下一阶段动作，机器态在对应 Execution 成功后转换；流程图中的 `lldp-ready`/`configured-ready` 等字样仅为 UI 按钮可用性语义，**不是持久化状态**；
- `ssh_trust` 独立跟踪：broken 时普通 SSH 任务拒绝并进入受限的 trust.repair 队列，修复成功回到 established。

**Issue**：`open → assigned → in_repair → verify → closed`；`closed` 后重验证失败可 `reopened`；连续两次 verify 失败升级通知 DL。每次转移使用业务 action 与状态版本 CAS；assign、repair、verify、close 权限分别检查，verify 关联限定范围的复测，close 必须有有效 PASS 证据和 NE/DL 确认。看板拖拽仅调用这些动作，不能直接 PATCH 任意状态。

**Alert**：`firing → suppressed（明确的处置/时间窗抑制） → resolved`；issue in_repair 可抑制重复通知但继续采样计数。维护期停用带内规则，B7 规则继续；数据 stale/unknown 不自动 resolved。

**Execution**：`created → scheduled → dispatching → running → success | failed | killed | warning`，另有 `pausing/paused`、`cancelling`、`reconciling` 控制状态。TaskAttempt 可为 unknown；未知副作用未对账前不能报告已 killed/success，具体语义见 §4.3。

**MediaItem**：`uploaded → verified → approved → retired`（单向，retired 不可逆但对象不删除）。

**网络底座域状态机（v3.0）**：
- **ZTP 引导（Switch）**：`powered → dhcp → booturl → identity → lookup → fetch → config → restart → managed`；任一步失败自动重试（指数退避，≤`fabric.ztp_retry_max`）超限置 `failed`，给出三段式定位（现象/原因/下一步动作）；身份未命中 SoT 进入隔离模板（仅 mgmt 可达）；
- **配置状态（Switch.config_state）**：`UNKNOWN → IN_SYNC ⇄ DRIFT_CRITICAL / DRIFT_MAJOR / DRIFT_MINOR`（结构化 diff 分级），漂移经修复计划闭环回到 `IN_SYNC`；
- **PushJob（13 态，唯一定义）**：主干 `CREATED → PRECHECKING →（FAIL 则 BLOCKED）→ READY →（驳回则 REJECTED）→ APPROVED → EXECUTING → VERIFYING → COMPLETED / PARTIAL`；异常 `EXECUTING/VERIFYING 失败 → ROLLING_BACK → ROLLED_BACK`；终态 `COMPLETED / PARTIAL / ROLLED_BACK / REJECTED → ARCHIVED`；`BLOCKED → PRECHECKING` 仅在缺口修复后。约束：预检未全过不进 READY；未审批不进 EXECUTING；VERIFYING 做复采复比，未达标设备进 PARTIAL；重试失败设备需创建关联的新 PushJob 并重新审批，或按原批准范围回滚；
- **FabricRule**：`draft → released ⇄ disabled`（`publish` 需预演已执行且审批通过；`revise` 复制为新 version，released 不可就地修改；critical 级 disable 需双签 + 必填 `change_reason`；**禁止物理删除**，仅 `disabled`）；
- **LL-01～LL-07 结论**：`normal / warning / critical / unknown`（不可采集一律 `unknown` + `degraded`/`approximation`/`estimated`，禁止静默为 `normal`，NFR-19）。

---

### 4.2 API 设计规范

规范：REST + OpenAPI 3.1，`/api/v1/*`；cursor 分页（**适用范围 v2.2 注记**：cursor 分页仅用于明细列表接口；聚合视图接口如 `/cmdb/machines/{machine_id}` 360°、`readiness` 为单资源/小结果集接口，不适用 cursor 分页）；SSE 实时流；统一错误体 `{code, message, details[], trace_id}`；变更操作 `Idempotency-Key`；契约文件 `api/openapi.yaml` 唯一事实源。本文表为端点节选，不表示接口已经实现；完整 schema、分页、权限、幂等、错误码及 SSE 事件需在对应 WP 前进入契约。项目请求显式携带 project_id；资源不属项目拒绝访问。状态/集合版本冲突返回 409，已删除或过期证据返回明确原因；无登录返回 401，无权限返回 403。

**v2.0 核心端点（节选）**：

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | /api/v1/media/items | 登记介质（FR-1.8） |
| POST | /api/v1/media/items/{id}/approve | 介质审批 |
| GET | /api/v1/media/items?type=driver&state=approved | 介质列表（参数下拉数据源） |
| POST | /api/v1/media/items/{id}/retire | 介质退役 |
| GET | /api/v1/catalog/params | 参数目录（下拉可选项 + 依赖元数据） |
| PUT | /api/v1/catalog/params/{key}/default | 参数默认值（模板联动） |
| POST | /api/v1/trust/repair | 互信修复（批量推送） |
| GET | /api/v1/machines/{id}/trust | 互信状态查询 |
| POST | /api/v1/provision/pxe | 发起 PXE 批次（镜像从介质下拉选择） |
| POST | /api/v1/validation/precollect | 触发预收集（G1） |
| POST | /api/v1/validation/lldp/start | LLDP 手动触发（就绪握手检查内置） |
| GET | /api/v1/validation/readiness?scope=... | 就绪握手三连状态查询（按钮启用依据） |
| POST | /api/v1/gates/{gate}/waive | 门禁豁免（DL 审批） |
| GET | /api/v1/mapping?view=all\|rail\|machine | 映射三视角 |
| GET | /api/v1/machines/{id}/ports | 网口面板数据（PortModel 列表） |
| GET | /api/v1/machines/{id}/ports/{pid}/events | 端口事件时间线 |
| GET | /api/v1/portmon/summary?rack=... | 柜级/全局端口健康汇总 |
| POST | /api/v1/nettest/runs | 发起连通性测试（tier 计划 + 参数快照） |
| GET | /api/v1/nettest/runs/{id} | 测试进度（分层结果） |
| GET | /api/v1/nettest/runs/{id}/matrix?rail=... | rail 热力矩阵数据 |
| POST | /api/v1/issues | 手动开单（portmon 发现类） |
| POST | /api/v1/issues/{id}/actions | 处置登记（NE） |
| POST | /api/v1/issues/{id}/verify | 触发定向重验证 |
| GET | /api/v1/issues?state=open&category=T3 | 工单池查询 |
| POST | /api/v1/config/apply | 应用 ConfigSet（参数快照冻结） |
| POST | /api/v1/nccl/runs | 发起 NCCL 测试（档位选择） |
| GET | /api/v1/nccl/runs/{id}/report | 报告（含调优清单/溯源章节） |
| POST | /api/v1/profiles | 创建项目模板 |
| POST | /api/v1/projects | 创建项目（套用模板初始化参数目录） |
| POST | /api/v1/profiles/{id}/sync | 存量项目同步模板（diff 预览模式） |
| POST | /api/v1/cleanup/runs | 优雅退出清理（含互信撤销验证） |
| GET | /api/v1/mon/policies | 采集策略查询（含当前阶段档位） |
| PUT | /api/v1/mon/policies/{id} | 采集策略调整（admin，含带宽预算校验） |
| GET | /api/v1/mon/alerts?level=&state= | 告警列表（网口/主机/GPU/RDMA 维度过滤） |
| POST | /api/v1/mon/alerts/{id}/suppress | 告警抑制（处置联动） |
| GET | /api/v1/mon/ingest/stats | 上报带宽/丢失率观测（运维用） |
| POST | /api/v1/models | 创建型号（唯一性预检） |
| POST | /api/v1/models/{id}/versions | 型号新版本（硬件规格变更） |
| POST | /api/v1/models/{id}/retire | 型号退役；保留旧引用，禁止新增绑定；有引用时禁止硬删除 |
| POST | /api/v1/models/{id}/role-bindings | 项目级角色绑定（FR-5.9，b300 两期覆盖） |
| POST | /api/v1/models/{id}/role-bindings:validate | 项目级批量校验：全型号槽位 × 角色方案 × 在位网卡，输出孤儿配置/空绑定明细（v2.2 新增，FR-5.9.3 校验入口） |
| GET | /api/v1/models?project= | 型号库列表（含槽位/默认角色方案） |
| GET | /api/v1/cmdb/machines/{machine_id} | 设备 360° 联邦视图（聚合 API） |
| GET | /api/v1/cmdb/machines/{machine_id}/snapshot | cleanup 归档快照 |
| PUT | /api/v1/cmdb/machines/{machine_id}/manual-fields | 维护期人工字段（source=manual 分列） |
| GET | /api/v1/cmdb/projects/{id}/stats | 项目级资料统计（AIOps 消费） |
| GET | /api/v1/cmdb/export?format=json\|csv | 标签化导出（schema 版本化） |
| GET | /api/v1/machines/{id}/pcie-topology | PCIe 拓扑快照（最新/指定版本，含 NVLink 矩阵/NUMA/LnkSta） |
| POST | /api/v1/validation/pcie-topology/run | 触发拓扑采集+对比（FR-4.8） |
| GET | /api/v1/machines/{id}/pcie-topology/diffs | 拓扑差异明细（预期 vs 实际） |
| GET | /api/v1/validation/lldp/steps?batch= | 四步线序 StepReport 时间线（FR-4.2） |
| POST | /api/v1/validation/lldp/steps/{id}/rerun | 单步重跑（Step 2/3 增量） |
| GET | /api/v1/bmc/machines/{id}/lamp | 服务器状态灯面板数据（FR-16.5） |
| POST | /api/v1/bmc/machines/{id}/power | 按 AllowableValues 选择确切 ResetType，能力校验/资源锁/审批 |
| POST | /api/v1/bmc/machines/{id}/uid | UID 点亮/熄灭 |
| GET | /api/v1/bios/configsets | BIOS 配置模板列表（版本化） |
| POST | /api/v1/bios/configsets/{ver}/apply | BIOS 批量置位（金丝雀→全量） |
| GET | /api/v1/firmware/runs | 固件升级记录（含 HGX/主板） |
| POST | /api/v1/firmware/runs | 发起固件升级（media://firmware 引用） |
| GET | /api/v1/projects | 项目列表（状态/进度摘要） |
| POST | /api/v1/projects/{id}/context | 仅保存最近项目偏好；请求归属始终来自页面 URL 的显式 project_id |
| GET | /api/v1/projects/{id}/timeline | 项目部署时间线（gate 进度/装机 flow 环节状态） |
| POST | /api/v1/projects/{id}/bom/legacy/preview | 上传 UTF-8 宽表 CSV 与批次号，只读折叠服务器、临时 NIC 和端口；仅服务器物料，不产生网络就绪结论 |
| POST | /api/v1/projects/{id}/bom/legacy/commit | 重传同一 CSV、批次号和令牌，写入统一 BOM 版本链；同批次号与标准包共享摘要冲突域 |
| POST | /api/v1/projects/{id}/bom/packages/preview | 上传目录或压缩包只读预检，返回问题清单与绑定会话/项目/原始 manifest/文件摘要的确认令牌；不写库、不生成包 ID |
| POST | /api/v1/projects/{id}/bom/packages/commit | 重传同一目录或压缩包及确认令牌；事务内重跑全套校验后原子提交；同批次号同摘要返回已提交结果，同号异摘要 409 |
| GET | /api/v1/projects/{id}/bom/packages/{package_id}/issues?severity=BLOCK\|WARN\|INFO | 已提交包的问题清单（文件+行号+字段三级定位，可导出 CSV）；预检问题直接随预检响应返回 |
| GET | /api/v1/projects/{id}/bom/items?category=&serial=&parent_uid= | 物料查询（双域同表） |
| GET | /api/v1/fabric/switches?pod=&role= | 网络设备状态总表（PRD FR-0.4.12） |
| GET | /api/v1/fabric/switches/{id} | 网络设备抽屉 8 Tab 聚合 |
| GET | /api/v1/fabric/readiness?scope=fabric\|pod\|switch | 就绪状态（`readiness_at`/`ttl_seconds`，执行前刷新） |
| POST | /api/v1/fabric/ztp/onboard | ZTP 开通编排（批次/角色/单设备） |
| POST | /api/v1/fabric/intents/render | 意图承接与渲染（pongo2 沙箱；禁止手写单机配置） |
| POST | /api/v1/fabric/doublecheck | Doublecheck 三层复核（语法/语义/一致性） |
| POST | /api/v1/fabric/pushjobs | 创建 PushJob（预检→审批→灰度→下发→复采） |
| POST | /api/v1/fabric/pushjobs/{id}/actions | PushJob 状态转移（审批/驳回/执行/回滚/归档，带 expected_version） |
| GET | /api/v1/fabric/diffs?severity=Critical | 漂移明细（intended vs running 结构化 diff） |
| GET | /api/v1/fabric/rules | 告警规则列表（`rule_id@version`，含 scope/安全区间） |
| POST | /api/v1/fabric/rules/{id}/dry-run | 规则预演（近 48h 历史样本，零副作用 diff） |
| POST | /api/v1/fabric/rules/{id}/publish | 规则发布/回滚（生成新 version；critical 双签） |
| GET | /api/v1/fabric/health | 全网健康（device_min/pod/fabric + 降级标记） |
| GET | /api/v1/fabric/lossless/findings?rule=LL-03 | 无损异常结论（LL-01～LL-07，携带 rule_id@version） |
| GET | /api/v1/fabric/optics?status=warning\|critical | 光模块与 DDM（字段级 source=switch） |
| GET | /api/v1/fabric/baseline/export?format=json | 网络交付基线导出（一致率复核 100%，schema 版本化） |
| GET | /api/v1/fabric/logs/analysis?device=&window=48h | 交换机日志归一化与规则命中 |
| GET | /api/v1/fabric/evidence | FR-0.8 证据接口聚合索引；每项含 object_ref、来源、采集时间、有效期与质量；无数据返回 unknown |
| GET | /api/v1/fabric/ports/{id}/lldp | FR-0.8.2 交换机侧 LLDP 邻居回读；缺采集返回 unknown |
| GET | /api/v1/fabric/cable-checks?port_pair= | FR-0.8.3 返回 N-S5 预校验或 G2 确认阶段、结论与证据；仅 g2_confirmed PASS 可满足 NP-3 |
| GET | /api/v1/fabric/lossless/counters?port= | FR-0.8.4 按端口与优先级返回 PFC/ECN/CNP/队列等计数、来源及降级 |
| GET | /api/v1/fabric/optics/evidence?port= | FR-0.8.5 返回交换机侧 DDM 与日志证据引用 |


**v2.5 补充契约端点（P1 先定义，随功能阶段实现）**：

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | /api/v1/auth/providers | 可用登录方式和部署标识，不返回凭据或内部目录地址 |
| GET | /api/v1/auth/csrf | 建立预登录会话并返回 CSRF token |
| POST | /api/v1/auth/login | 本地或 LDAP 登录，限流、统一失败响应 |
| GET | /api/v1/auth/oidc/{provider}/start | 校验相对 next 后生成 state/nonce/PKCE |
| GET | /api/v1/auth/oidc/{provider}/callback | 后端校验响应并建立 session |
| GET | /api/v1/auth/session | 当前用户、权限、可见项目与会话到期时间 |
| POST | /api/v1/auth/logout | 服务端撤销 session、清 cookie 与 SSE |
| POST | /api/v1/issues/{id}/transition | 带 action 和 expected_version 的状态转移；关闭验证证据 |
| GET | /api/v1/metrics/history | 指标范围、单位、来源、时间桶、采样精度及覆盖率 |
| GET | /api/v1/machines/{id}/availability | 来源区分的 online/offline/unknown 时间段及计算口径 |
| GET | /api/v1/dashboard/kpis | 按显式项目过滤的当前值、前期对照、趋势桶和水位 |
| GET | /api/v1/engine/schedules/preview | 未来 24h 定时触发计划（时区/暂停/错过触发策略），不是 Prometheus 预测 |
| GET | /api/v1/events | 会话及项目授权的 SSE，支持 cursor/Last-Event-ID 重放 |
| GET | /api/v1/nccl/runs/{id}/groups | 通信组成员/rank/启动屏障/整体结果和覆盖口径 |

### 4.3 执行一致性、资源互斥与控制语义

资源锁由 PostgreSQL 保存权威状态，Redis 只加速排队；所有通道共同申请。锁按稳定资源 ID 排序申请，组任务原子预留全部成员，避免占住一半资源后死锁。租约到期不等于目标机操作已经停止：先核对回执/进程/Redfish TaskService，未确认则进入 reconciling，禁止冲突任务进入。Agent 验证 fencing_token；无法由设备验证 token 的 BMC/SSH 动作由控制面单写派发与回执对账共同约束，不宣称设备具备天然 fencing。

| 操作类别 | 资源模式 | 冲突范围 |
|---|---|---|
| 普通只读资产/健康采集 | machine shared | 可共享；遇固件/重装等独占则等待或使用明确允许的 BMC 状态查询 |
| 配置/装机/固件/清理/电源 | machine exclusive（配置可进一步细分） | 与同机 Agent/SSH/BMC 写动作、测试互斥 |
| RoCE/NCCL 性能测试 | group + GPU/NIC + 测试窗口 exclusive | 组成员资源整体预留；隔离可影响结果的并行流量/采样 |
| UID 灯 | bmc.uid 独立受限动作 | 不与性能数据面互斥，但受固件更新能力限制 |

- **Pause**：停止新派发，当前已启动任务继续并回收结果；只有已运行任务结束/到安全点后进入 paused。不会默认 SIGSTOP 远端进程，也不会暂停半个 NCCL communicator。Resume 重验权限、证据和资源锁后继续未派发任务。
- **Kill**：先停止派发，再按能力取消目标进程组/远端作业；NCCL 以 GroupRun 整组终止。收到所有必要终止回执后才 killed。不可取消的刷写进入 cancelling/reconciling，显示继续等待或需人工处置，不能以 UI 成功掩盖后台仍运行。
- **重试**：只读/声明幂等任务可按策略重试；非幂等写入先查询 command_id 回执和真实状态，结果 unknown 时不创建新的有副作用 attempt。数据库状态更新与 outbox 同事务，消费者去重后再决定派发。
- **重启恢复**：重建待派发队列、查询未完成回执、恢复租约/监控动态窗口；覆盖 DB 成功而 Stream 未发布、发布后未 ACK、远端完成但回执丢失等故障切点。

### 4.4 门禁证据与原子准入

GateEvidence 保存范围、版本和数据有效期；GateRecord 是评审记录，Machine.state 只是里程碑投影。A–B 与 C–D 通过不能证明 A–C 路径通过；G5 证据的 port_pair_coverage 与测试档位必须覆盖本次 GroupRun 需要的路径，允许的抽样必须由已发布 gate policy 声明。G6 交付范围必须由明确测试覆盖计划支持。

提交请求包含目标集合版本和证据指纹，服务端依次校验认证/项目/动作权限、生命周期、范围覆盖、各来源新鲜度、工单/豁免、参数依赖与资源能力；事务中 CAS 相关版本、预留资源、冻结 Execution 快照并写 outbox。派发前重新检查外部就绪有效期与锁，失败返回缺口，不自动扩大目标范围。未来新增设备不自动加入已批准批次。

BOM、型号版本、部件、拓扑、MappingRevision、RoleBinding、ConfigSet 或规则变更后按依赖图标记受影响证据 invalidated。历史报告仍可读且明确当时版本；不得篡改旧 PASS。数据 `unknown/stale/unsupported/unreachable` 与 fail/pass 分开，缺失数据不得算通过。各数据类型 TTL 在版本化 CollectPolicy/gate policy 中明确，默认引用 PRD 的 asset.freshness_max_age、gate.evidence_ttl、gate.readiness_ttl、mon.snapshot_stale_after 参数；readiness 缓存上限 30s。必填配置缺失时阻断并提示，不能假定无限有效。豁免绑定具体缺口/范围/有效期/审批人和补偿措施；过期或版本变化后重验。

### 4.5 NCCL GroupRun 与结果归属

NCCLTestRun 保存测试计划，每个 GroupRun 冻结成员、GPU UUID/数量、global/local rank、world_size、网络接口/rail、算子、消息大小、迭代次数、工具与参数版本。gang 调度预留整组资源后建立 rendezvous，所有成员 ready 后一次启动，屏障超时整体退出并释放已确认结束的成员资源。组内任何 rank 失败使该次 collective 无效，取消剩余成员并对账；重新测试创建整组新 attempt，不只重试单台。

报告的 `coverage_mode` 统一为 `grouped_coverage`（全部目标分组覆盖）或 `single_communicator`（全域 GPU 同一通信组），与 PRD FR-6.2 一致；单机/跨机测试档位由 test_profile 表达，不混入覆盖模式枚举。不得将分批滚动覆盖标成全局通信测试。HGX 节点与 GB300 NVL72 的机架/NVLink domain 分别建模，不把每个节点都硬编码为 8 GPU。

algbw/busbw 为 collective 或 group 测量，单位 GB/s，并保存 nccl-tests 的计算口径与参与规模。节点掉队依据有来源的 rank timing、DCGM、NIC 吞吐/错误与组对照，不能把同一组 busbw 复制为每台独立测量。阈值按型号/拓扑/通信规模/算子/消息大小的基线版本判断；未建立基线时显示待校准，不用硬件峰值自动宣布合格。

### 4.6 登录、会话与授权

`/login` 为 keel 公共入口，提供本地账户和已配置的 OIDC/LDAP；LDAP 密码仅传给后端并通过可信 TLS 访问目录。浏览器统一使用随机不透明 session ID 的同源 `HttpOnly; Secure; SameSite=Lax` cookie；只在显式开发配置且绑定回环 HTTP 时允许关闭 Secure。session 记录服务端存储并支持即时撤销；不将 bearer token、密码或 refresh token 放入 localStorage。

本地密码存储使用 Argon2id 等经过验证的密码哈希；首次管理员通过服务端交互 CLI 创建，无默认账号口令/公共初始化 API；凭据不写日志。登录/LDAP/OIDC 回调统一失败文案，按账号与来源限流，认证成功轮换 session ID。所有修改请求校验绑定会话的 CSRF token 与 Origin；登录也使用预登录 CSRF。OIDC 使用授权码 + PKCE 并验证 state、nonce、issuer、audience 和精确 redirect URI；`next` 仅允许同源相对路径，拒绝协议相对 URL、外域和编码绕过。

会话空闲/最长有效期和登录失败限制使用 PRD FR-10.1/参数目录的 auth.idle_timeout、auth.absolute_timeout、auth.login_max_failures、auth.lockout_window，session API 返回 expires_at；过期统一 401、停止订阅并返回登录页，保留允许保存且不含秘密的草稿。退出撤销服务端 session 并清理前端用户/项目缓存和 SSE。SSE 使用同源 cookie 鉴权，连接建立与每次重放检查项目权限，权限撤销/会话过期主动关闭；URL 不带访问令牌。

RBAC 的六类角色（DE/DL/NE/SRE/Viewer/Admin）和权限代码维护在 `api/rbac.yaml`，OpenAPI 每个操作引用权限及 scope，前端消费 session.permissions；后端始终再校验。issue assign/verify/close、trust bootstrap/repair、固件、电源、清理、门禁豁免、介质批准分别为独立动作；Viewer 仅在获授权范围导出，Admin 不隐式绕过交付审批。

P1资源错误约定：未认证返回401，无动作权限返回403；项目详情等资源读取在不存在或无项目授权时均返回404，避免泄露资源存在性。此行为保持拒绝跨项目访问的判据。

### 4.7 指标、历史与页面刷新

所有指标使用 `{value, unit, source, observed_at, received_at, freshness, method, scope, quality}`，区分网络 Gb/s 与 NCCL GB/s；空值/未知/不支持显式编码。在线率分别表示 Agent 通道、SSH 可达或 BMC 电源状态，Agent 失联不能推断设备断电；unknown 区间单列并显示覆盖率，不悄悄计入可用或不可用。

页面首次打开获取带 snapshot_version/cursor 的快照；活动页/Tab 按 DESIGN-SYSTEM 的 poll 或 SSE 更新，隐藏或卸载时取消不需要的订阅。SSE 事件含 event_id、aggregate_id/version 与 project_id，支持 Last-Event-ID 重放和去重；服务端游标过期返回 resync_required，客户端重取快照再接增量。采集 delta 也包含 base_version，缺失增量后要求完整快照。断线保留最后数据与时间，显示 stale 状态，不以降低文字透明度掩盖故障或破坏可读性。

历史服务显式支持 1h/24h/7d，默认展示分辨率为 1h 窗口用 1min 桶、24h 用 5min 桶、7d 用 1h 桶；原始样本保留至少 7d，小时桶至少 90d；交付/审计证据按独立长期策略保存。桶包含 min/max/avg/count/coverage、来源和质量，禁止跨单位或不同含义来源混聚合。PG 可支撑开发/小规模，容量档可选 ClickHouse；清单声明保留周期、基数、存储量和降采样配置，变更需留痕。未来 24h 计划来自调度器持久化 cron/时区/暂停/错过触发策略，webhook/事件触发只显示等待条件，不伪造未来时间。

### 4.8 AI 检索与建议边界

三类知识（Runbook、NVIDIA 文档、案例）以 KnowledgeDocument/Chunk 建模并保存来源、审批、版本及访问范围。默认实现采用 PostgreSQL + pgvector 检索适配器，扩展版本固定在镜像清单；缺少扩展的开发档允许有标识的词法/mock 检索，不冒充语义检索已验收。embedding 模型与维数版本化，更新模型重建索引并保留旧引用。

检索过滤先应用用户与项目授权，再做向量/关键词召回和重排；模型输出包含引用、适用型号和版本，未知时说明缺口。外部资料和工具输出始终按数据处理，fenced block 不能替代权限隔离。AI 仅生成可审查建议、Flow/ConfigSet 草稿；发布/执行仍走正常权限、审批、介质校验和门禁，不给模型直接执行凭据。AI 不因资料中的指令扩大取数范围或调用工具。

验证覆盖召回命中、引用正确性、错误建议与应拒答场景、跨项目泄漏、恶意内容及未经批准执行；采纳率来自真实用户反馈。OpenAI-compatible、Anthropic、vLLM 三适配器均交付流式/取消/错误/用量契约测试，mock 成功不等于真实 Provider 或现场 PoC 已通过。

---

## 5. 关键流程时序（v2.0）

### 5.1 装机与互信链（扩展）

```
BOM 导入 → OS 镜像选择（介质库下拉） → cloud-init 渲染（hostname 规则 + 平台域公钥注入）
→ PXE 装机（HTTP-first） → 一次性回调身份核验 → 受限 trust.bootstrap（exec true + 已验证 host key）
→ ssh_trust=established → 事件 TrustEstablished → 解锁预收集 Flow
互信失败 → 修复队列（降级：vault 批次密码一次性首登注入，见 ADR-06） → 修复验证 → established
```

（v2.2 注：渲染发生在 PXE 装机之前——cloud-init 模板渲染完成才进入分发链，与 ADR-04 描述顺序对齐。）

### 5.2 验机全链（G1→G3，含拓扑验证与四步线序，v2.3 重排）

```
装机+互信完成 → 事件触发 precollect Flow（DOM 一并采集）
G1 评估（字段完整+MAC↔BOM+DOM 能力标注） → pass → 机器态→collected
PCIe 拓扑验证（FR-4.8）：sysfs 采集 + nvidia-smi（NVLink）+ LnkCap/LnkSta
  → 预期(型号库) vs 实际对比 → pass → 产出 GPU-NIC 关系表（Step 1 输入）
  → 拓扑差异 → T7 工单 → 经批准 issue.repair（按证据选择 Runbook）→ 定向重验
DE 点击 [启动线序核查] → 前置：GET /validation/readiness（本端 G1+拓扑 pass + NP-1/NP-2/NP-4；不要求 NP-3）
Step 1 GPU-NIC 关系表输出（含 PCIe BDF/上游路径/NUMA）→ StepReport(1)
Step 2 lldpd 批量下发 → 实际邻居关系 + 本地 MAC 清单 → StepReport(2)
Step 3 经 FR-0.8.2 回读交换机 LLDP → 三方比对（服务器 LLDP↔交换机 LLDP↔BOM）→ 确认端口对写 PortModel、产出 NP-3 或差异 T1–T6 分类开单 → StepReport(3)
Step 4 全部通过/有效豁免 → DL 线序确认 → G2 pass → mapping-svc 生成待确认映射
G3 评估（覆盖+冲突） → pass → 发布 MappingRevision → 机器态→mapped（基线配置解锁）
```
（Component/PhysicalPort 身份在 BOM/预收集已建立；每步 StepReport 版本化，FAIL 阻断正常后续步骤并建单。获批准的 issue.repair 只豁免其修复对象涉及的上游门禁，不豁免身份/凭据、介质兼容、资源锁及维护期白名单，完成后 issue.verify 重验，防止 G3 前故障无法修复。）

### 5.3 连通性测试链（G5）

```
DE 发起（冻结目标/档位/时长） → 原子准入（有效 G4 + 范围/版本 + 未处置故障检查 + NP-4/NP-5/NP-6）
  （关联工单的 issue.verify 允许限定故障范围定向复测；其他目标仍正常检查）
→ 参数快照冻结（工具版本来自介质库） → 分层执行：
  L1 链路层（读 PortModel，不发流量） → L2 网状 ping（分批） → L3 RDMA 定向打流（per NIC pair，分批滚动）
  → L4 全带宽打流矩阵（rail 间路径热点覆盖） → L5 PFC/拥塞验证（FR-0.8.4 交换机侧计数与降级结论交叉）
→ per-pair 结果入 NetTestPairResult → 失败分类（T1–T6）→ 开单
→ 报告生成（分层结果+端口对覆盖+版本+失败统计+工单清单）
→ 全 PASS 或有效 DL 豁免 → 发布 G5 GateEvidence → 机器态→netverified
```

### 5.4 NCCL 测试链（G6，v2.0 细化）

```
跨机/网络档目标组准入（当前有效 G5 范围证据与 NP-5/NP-6，允许 tested 重测）；纯机内 NVLink 诊断可在 G4 后单独运行但不产出 G6 PASS → S1 预检（版本/实机/RAS/P2P）
→ S2 环境准备（NCCL 包介质库分发→缓存命中检测、环境一致性校验）
→ S3 单机 GroupRun → S4 跨机 GroupRun（rack 内→rack 组，整组屏障/熔断）
→ S5 按计划选择分组覆盖或全量单 communicator（报告明确 coverage_mode，不混同）
→ S6 结果回收聚合（按同规格 GroupRun 统计 P10/P50/P90；有来源的 rank/节点掉队诊断）→ S7 报告生成
→ S8 失败溯源（PortModel 端口定位 + G5 基线比对 + DCGM 事件 + 工单关联）
→ S9 调优清单（2.31 系环境变量差异汇总，AI 辅助解读）
→ G6 评审（DL）→ pass → 解锁优雅退出
调优落地：S9 清单 → ConfigSet 版本化修改 → 应用 → 定向重测 → 案例库自动沉淀（FR-6.7）
```

### 5.5 故障工单闭环链（v2.0 新增）

```
来源触发（fabric 网络侧发现 / G2 差异 / G5 失败 / G6 网络问题 / portmon 告警 / T7 拓扑 / BMC）
→ issue-svc 开单（保留首次发现 source；经定位后另记 responsible_domain；机器/部件/PhysicalPort 身份 + 类别 + 期望/实际 + 版本证据 + 日志引用）
→ 分派 NE → 处置登记（动作枚举+实物 SN 核对）
→ 定向重验证（按类别编排：T2→LLDP 单口；T3/T4→LLDP+DOM+单路径连通；T5→预收集增量+LLDP+连通；T6→配置读回+连通；T7→拓扑与受影响门禁）
→ PASS → 提议关闭 → 确认 → closed（档案永久留档）
→ FAIL ×2 → 升级 DL（建议换件）
```

### 5.6 优雅退出链（扩展）

```text
有效 G6 → 冻结交付范围/源版本，拒绝新带内作业并 drain 已运行任务
→ 生成所有权清单（固定路径 + OwnershipRecord；检查 canonical path、符号链接、对象身份）
→ 上传原始执行日志/交付证据/最后采集快照及清单 → 控制面持久化 pre_cleanup ACK
→ 受控 Agentless 会话逐项清理平台创建的服务/文件/账号/规则/缓存；保留撤销所需会话及凭据
→ 残留扫描 + 清理报告 + CMDB 归档清单（引用原始证据和源水位）→ 控制面持久化 residue ACK
→ DL 确认待退出范围/报告 → 撤销平台 SSH 公钥与临时凭据（最后一个带内动作）
→ 经已有会话采集撤销回执，新连接拒绝验证；关闭原会话 → 汇总最终报告
→ 控制面持久化 final_technical ACK → 技术清理完成 → maintained + B7 监控白名单
另行签署链：DL 确认最终报告 → 客户 PDF 签署 → 签署件归档 → 解锁 Project.delivered 评审
```

CleanRun 为持久化步骤编排，每步带幂等键和证据，不以一条丢失事件决定完成。archive_ack 按 pre_cleanup、residue、final_technical 三个阶段记录对象摘要和持久化回执，后续步骤不能把前一阶段 ACK 当作自己的完成证明；清理前必须保全仍在目标机上的原始日志与证据。平台只移除自身所有权对象，不能清空共享目录、客户账户或整份 authorized_keys；文件被替换/目录为符号链接/所有权不符时停止该项并报告。私钥撤销不等于既有会话或 Agent 自动消失，须单独确认卸载、停采、关闭会话。

撤销前中断可用尚有效受控会话按最后一个已确认步骤恢复；pre_cleanup ACK 缺失不能开始删除，residue ACK 缺失不能撤销互信。撤销后回执不确定进入 `reconciling`，核验控制面记录、目标新连接拒绝和 BMC 状态，不能自动重建 SSH 信任。必要时由有权限人员批准一次性修复会话，完成后重新撤销并补证；无法证明清理/撤销则不显示技术清理成功。技术证据及 final_technical ACK 齐全后通过 outbox 对账推进 maintained，客户尚未签署不恢复带内调度，也不阻止 B7 监控。签署状态与 technical_state 分开保存，签署未完成只阻断项目 delivered；客户签署为外部确认，自动化不能代签。

---

## 6. Agent 设计（keel-agent）

```
/opt/keel-agent/keel-agent          # 单二进制（Go, static）
/opt/keel-agent/keel-agent.conf     # 控制面地址、TLS、注册凭据
/etc/systemd/system/keel-agent.service
/var/lib/keel-agent/               # 任务状态、幂等记录
/var/log/keel/                     # 执行日志
/opt/keel-cache/media/             # 介质缓存（SHA256 寻址）
/opt/keel-cache/nccl/              # NCCL 测试工作目录
```

- **启动**：systemd → 注册（SN/版本/指纹 + 一次性短期 enrollment token）→ 校验设备归属并签发设备 mTLS 证书 → gRPC 任务等待；控制面 CA/轮换/吊销由服务端管理，目标机只持有本机凭据；重放 token 或错项目绑定拒绝；
- **任务执行**：核对 command_id/fencing_token → 持久化接收回执 → fork 进程组（资源限额/超时）→ 流式回传 + 结构化 output → 持久化终态回执；控制面断线后仍可查询，不把重连当重新执行；
- **v2.0 心跳扩展**：心跳消息附网口状态增量（ethtool + ethtool -m 摘要）+ 介质缓存清单（ADR-02）；
- **自我升级**：新版本二进制作为介质分发（media://agent/<ver>）→ systemd restart，失败回滚；
- **卸载**：Agentless 显式停止并卸载 agent、吊销设备 mTLS 证书；完成清理后最后撤销 SSH 互信，两个通道分别验证。

---

### 6.1 部署拓扑

### 现场单机形态（默认）

```
┌──────────── 交付机房管理网 ────────────────┐
│  ┌────────────────────────────────────┐  │
│  │ 平台节点（1 台物理机/VM）             │  │
│  │ docker compose:                    │  │
│  │  api(单二进制) postgres redis minio │  │
│  │  dnsmasq(ProxyDHCP)                 │  │
│  │  nginx(静态前端+反代+PXE HTTP        │  │
│  │       +介质镜像源目录)               │  │
│  └────────────────────────────────────┘  │
│      │ 管理网: SSH(互信)/Redfish/gRPC     │
│      ▼                                    │
│  10,000+ 目标机 (agent / SSH / HTTP 拉流) │
└───────────────────────────────────────────┘
        ▲ 对外: 网络平台 REST（握手/回读/计数）
```

**交付形态（v2.2 新增，对齐 NFR-4）**：

| 形态 | 架构 | HA 承诺 | 适用场景 |
|---|---|---|---|
| **single-node（默认）** | 上图单节点 compose 全栈 | 无 HA；进程级自恢复（systemd/compose restart），故障后人工介入恢复 | 交付现场快速起平台，交付期短、可容忍分钟级停机 |
| **ha-cluster（可选）** | 2 节点 api（负载分担）+ PostgreSQL 主备流复制 + Redis 哨兵 + MinIO 纠删码 | RTO<5min（**NFR-4 仅在此形态承诺**） | 长驻交付/7×24 交付期 |

形态选择为部署配置项（`KEEL_DEPLOY_TOPO=single|ha`），应用代码无差异；ha-cluster 的 PG 切换、哨兵仲裁和 MinIO 存储拓扑在交付形态验证时落实并进行故障演练；未验证不能承诺 HA RTO。mock 证明恢复逻辑，不能代替真实基础设施故障恢复证据。

### 性能预算（v2.2 重校准）

| 环节 | 预算 | 依据 |
|---|---|---|
| Dispatcher 取目标（10k） | <2s | 索引查询 |
| ExecutionTarget 生成（10k） | <5s | 批量 insert |
| 扇出执行（10k × 30s，Agent） | 端到端 ≤5min 目标；记录提交至最后有效终态 | 默认并发 1,000 的纯运行下界已为 300s，不能保证含调度开销的 SLO；容量验收档需 >1,000 并实测资源余量，固定环境/档位写报告 |
| 扇出执行（10k × 30s，SSH） | 同一端到端 ≤5min 目标，池上限须容纳验收并发 | 默认 1,000/池 2,000 仅默认配置；容量档需预热/认证/回执开销预算和 >1,000 实测并发。资源受限可降档并标记“该档未达到标准 SLO”，不可算标准验收通过 |
| 介质分发（500MB 驱动包 ×10k 机，缓存命中 95%） | 首次 ≤15min（要求介质出口 ≥50Gbps，见 A-6）/ 重发 ≤2min | HTTP 并发拉取 + 缓存命中跳过；**出口带宽不足时按规模分档断言**（2,000 台 ≤15min @ ≥10Gbps；万卡分层分发为 Open-2 候选） |
| Redfish 采集（10k BMC，16 并发） | 全量 <2h | L1 ~8s/BMC |
| 网口状态呈现延迟 | **按档位承诺（NFR-12 v2.2）**：M1 ≤5min / M2–M4 ≤90s / 维护态转 B7 | 心跳周期档位化（ADR-02）+ mon-svc ingest 开销 |
| **监控 ingest 吞吐** | 聚合预算 ≤1Gbps；单机 50KB/s 稳态与 200KB/s 动态为上限而非保证同时可用；可丢遥测样本 <0.1% | 10k×50KB/s≈4Gbps、全动态≈16Gbps，控制面按项目/优先级/活跃窗口分配全局令牌桶，必要时合并降采样；状态、告警、任务最终回执不可按遥测丢弃策略处理 |
| **CMDB 聚合查询（v2.1）** | 单机 360° ≤2s；项目级统计 ≤5s（万卡，物化视图口径 ≤60s 滞后，ADR-12） | 联邦视图聚合 API + 索引（NFR-14） |
| 连通性测试（1 rail × 128 机，快速档） | ≤25min | L2 网状 ping 抽样档 + L3/L4 打流分批滚动（配对策略参数化，PRD FR-11.1） |
| **PCIe 拓扑采集（v2.3）** | 单机 ≤30s（sysfs 遍历+nvidia-smi）；万卡快照（并发 1,000）≤15min；对比判定单机 ≤5s | NFR-15（量纲：10,000×30s÷1,000=300s≤15min ✓） |
| **BMC 维护期轮询** | 10k 台默认每 5min 更新，默认并发上限 128，失败率 <1%；逾期显示 stale | 预算按每台完整轮询请求数 k 及实测耗时核算（ADR-18），不能沿用单轮 52min 口径；不达标必须调整容量/采集方案并重新验证 |
| **BMC 实时读取（v2.3）** | 抽屉状态灯/资产刷新单机 ≤3s | NFR-16 |
| **项目切换** | 当前标签页 URL 项目切换后视图刷新 ≤2s；列表 ≤1s | NFR-17；其他标签页冻结的项目/表单不得漂移 |
| 状态矩阵页刷新 | <1s 聚合 | Redis 缓存 + 增量 SSE |
| **Fleet / 矩阵渲染** | 万级逻辑行、双轴虚拟化；微指标单行增量 <1ms，滚动 60fps 目标，固定浏览器/数据集/硬件 | TanStack Virtual + 可见单元格 CSS/SVG；服务端筛选/聚合/cursor 分页，成本按可见单元格计；不为每行创建 ECharts 实例 |

---

**网络身份容量**：PRD NFR-1 要求统一 BOM 支持至少 80,000 个服务器物理口及最多 5,000×256 个交换机 breakout 子口，合计至少 1,360,000 个端口实体；按 `(parent_uid, port_index)`、稳定 `item_uid` 与端点外键建立索引，容量测试应核对插入、引用解析与分页查询。明细时序原始样本至少保留 90 天，过期后保留聚合、LL 事件与越限快照，不能以“长期不保存明细”缩短原始保留期。

**容量验证边界**：开发服务器资源只代表开发档，不能推导可同时运行 10k 容器或现场全量介质。CI 使用少量真实 SSH/Agent/BMC 传输端点验证协议，负载发生器模拟 10k 逻辑身份与故障种子；报告记录逻辑身份数、真实连接数、目标硬件、服务资源、数据体积、冷/热缓存和计时边界。模拟器证明行为与控制面容量；RDMA/NVLink/CUDA/Arm64 真机能力由 H-1～H-18 对应 PoC 独立出具，未测/跳过不能记 PASS。介质、日志、指标保留、备份与磁盘水位分别估算，恢复演练覆盖数据库/对象存储/密钥备份的一致性。

## 7. 安全设计（v2.5）

| 层面 | 措施 |
|---|---|
| 传输 | API 同源 HTTPS、agent mTLS、可信 Redfish TLS；SSH host key 按可信回调/授权导入验证，禁止未知指纹自动接受（ADR-06） |
| 凭据 | BMC/SSH 密码池/AI key 全入 vault；平台域私钥（keel-ctrl-*）仅存控制面（内存/加密盘），永不落目标机 |
| 互信 | 公钥注入唯一主路径 cloud-init；降级密码首登走 vault 批次凭据且**一次性消费**（ADR-06 v2.2）；互信撤销是退出必要条件 |
| 权限 | RBAC 6 角色（DE/DL/NE/SRE/Viewer/Admin）× 模块动作矩阵；门禁豁免、介质审批、清理执行、模板同步等高危动作二次确认+审计 |
| 目标机 | agent 注册凭据一次性分发；root 作业显式声明 + 审批标记 + **会话录像归档**（v2.2，AuditLog 关联 log_ref） |
| 审计 | 应用角色对 AuditLog 仅 INSERT/SELECT；管理员仍可能改数据库，不能宣称绝对不可篡改。证据摘要、对象版本/保留锁及异地备份共同提供校验；介质、工单、互信、权限、登录全留痕，控制面交付/审计保留至少 3 年，受配置与客户约定约束 |
| AI | 出域开关、脱敏、授权检索与来源版本；资料按不可信数据处理；AI 生成草稿不持有执行凭据，执行仍走正常审批与门禁（§4.8） |
| 前端 | 同源 HttpOnly session cookie + CSRF，登录过期/退出撤销；敏感字段按权限返回，不在 localStorage 存访问令牌（§4.6） |
| Webhook（v2.2） | **入站**（FR-7.2）：校验已配置来源/绑定项目、HMAC 原始报文签名、时间窗和事件去重，拒绝重放/错项目；**出站**（FR-10 通知）：目标白名单 + HMAC 签名（可配，默认开启签名） |

---

## 8. 可观测性（v2.0 扩充）

- **Metrics**：execution 分布、扇出水位、SSH/gRPC 错误率、Redfish 成功率、gate 通过率、**介质分发吞吐与缓存命中率、网口状态翻转率、工单开单/关闭速率、连通性分层通过率、NCCL busbw 基线**；
- **Tracing**：OTel 全链路，trace_id 透传前端；
- **平台自诊断**：system flows 定时（组件健康、队列积压、磁盘水位、**介质桶存储水位、互信异常率**）。

---

## 9. 前后端协作约束（vibecoding 契约，v2.0 扩充）

1. **契约先行**：`api/openapi.yaml` + `api/proto/` + `api/rbac.yaml` 同源描述接口/事件/动作；前端类型自动生成，契约存在/设计冻结不等于接口已实现；
2. **错误规范**：统一错误体 + trace_id；前端全局错误处理分支；
3. **实时刷新**：列表轮询 + 详情 SSE；SSE 事件 schema 契约化（新增 `PortStatusChanged`、`IssueStateChanged`、`GateVerdictChanged` 事件）；
4. **虚拟滚动契约**：矩阵行/列及列表使用 TanStack Virtual，服务端筛选/排序/cursor 与聚合分离；跨页全选冻结查询和实际目标 ID/摘要并显示排除数量，不只保存当前页选中态；
5. **权限映射**：api/rbac.yaml 与 OpenAPI operation 同源，后端返回 `permissions[]`，前端按钮/拖拽按动作渲染；行内动作固定本行，批量动作只能从批量条发起。后端重验冻结的项目、目标、权限和 expected_version，关闭工单需验证证据；
6. **i18n 键规范**：层级命名，展示文案全在前端；
7. **参数下拉契约（v2.0）**：业务任务版本选择统一 ParamSelect（`GET /catalog/params`）并校验依赖；介质/型号/配置的“登记新版本”表单允许输入新版本标识，经服务端校验后发布。lint 检查业务执行表单不绕过已批准目录，不禁止版本源头创建；
8. **就绪握手契约（v2.0）**：所有跨系统触发按钮（LLDP/连通性）必须绑定 readiness API 状态，禁用态必须展示缺口明细（组件 ReadinessGate 封装该逻辑）；
9. **环境抽象契约（v2.1 补充，ADR-14）**：面向交付硬件的前后端开发 mock-first——①前端开发基于契约生成的 mock server（不依赖后端真实实现）；②后端外部依赖（网络平台/BMC/SSH/打流/AI）一律先写 mock 实现再写真实实现，同 PR 交付；③环境地址/凭据/开关全部配置化且默认 mock 档，**禁止硬编码真实环境地址**（CI 静态扫描项）；④性能设计以 PRD NFR 压力为依据，mock 报告注明边界，承诺硬件性能或 GA 时仍需指定 PoC 实测；授权远端开发环境可正常联网开发；
10. **页面数据流契约（v2.3 新增，AI 审计 B-2/C-1）**：每个页面的实时数据协议在 DESIGN-SYSTEM §3 导航树逐页标注——`[SSE]`（状态矩阵/网口面板/执行详情/告警中心）、`[poll 5s]`（设备列表/工单池/测试批次，用户可调 5–60s）、`[poll 60s]`（CMDB/项目时间线聚合）、`[on-demand]`（首次打开才拉快照；活动标签可另标 poll/SSE，协议按 §4.7 配合使用）。**SSE 断连前端契约**：顶部全局黄条“实时连接中断，显示最后数据 HH:MM”+ stale 徽标 + 自动重连；保持正文对比度，**不清空已有数据**（Beszel 模式）。**列表筛选一律 URL search params 同步**（刷新/分享/回退可复现视图，TanStack Admin 模式）。

---

**Flow 编辑一致性**：YAML、拓扑和 No-code 视图共享版本化 AST；高级表达式不能被无代码编辑器表达时保留原节点并显示只读/返回 YAML，不静默丢字段。保存前运行 schema、引用、依赖与危险动作检查；回滚通过新建引用旧内容的 FlowVersion 实现，执行固定版本不改历史。

## 10. 假设与依赖登记（v2.0 更新）

| # | 假设/依赖 | 影响范围 | 验证时点（P=构建阶段，对应 PoC 执行包） |
|---|---|---|---|
| A-1 | ~~网络平台提供 LLDP/就绪/打流计数 API~~ → **v3.0 起域内自供（FR-0.8）**；仅当客户自有网管时经**适配器**实现同一契约（不可提供能力返回 `unknown`） | G2/G5/G6 | P2 前联调（域内无外部依赖；适配器形态执行包 #P2） |
| A-2 | BOM 由网络团队/服务器团队产出（**v3.0.2 导入包目录格式，PRD 附录 A.5**），含 SN/MAC/hostname 规则/rail/breakout/线缆两端/**凭据批次**；字段映射由项目模板声明 | FR-2.1 | P1 导入测试（执行包 #P1） |
| A-3 | BMC Redfish 满足 DMTF 1.9+ | FR-3 | P2 PoC（执行包 #P2） |
| A-4 | 按 HGX B300（amd64）与 GB300 Grace（arm64）分别验证 OS/内核、cloud-init、公钥/指纹回调、agent 和工具包；支持矩阵引用批准版本 | FR-2.3/FR-6/FR-16 | P1/P3 各架构 PoC，未测架构不标支持 |
| A-5 | NCCL/CUDA/驱动/内核/固件以批准兼容矩阵组合，CUDA 13 常规路径驱动至少 R580 并校验具体补丁要求；工具包按 arch 签名/摘要锁定 | FR-6/FR-11 | P3 PoC + 基线数据版本 |
| A-6 | 现场管理网 SSH 22 端口可达；**平台介质分发出口 ≥50Gbps**（万卡同发 500MB×10k=5TB（十进制） 首次 ≤15min 口径，按 90% 传输效率折算 约49.4Gbps 需求；现场不足时按规模分档断言：2,000 台 ≤15min @ ≥10Gbps，万卡启用分层分发/机柜级缓存——REVIEW-FINDINGS Open-2） | Agentless/介质分发 | P1 模拟器 + 执行包 #P1 回填实测 |
| A-7 | IP 由 BOM 携带（IPAM 对接 P1 增强） | FR-5.2 | 首现场调研 |
| A-8 | 光模块 DOM 读出在主流 CX8 平台可用（不可读走降级标注） | FR-4.5 | P2 真机 PoC（执行包 #P2） |
| A-9 | ~~网络平台提供 PFC/ECN 计数~~ → **v3.0 起 fabricmon-svc 经 `ethtool -S`/NVUE/WJH 采集（FR-0.6.2）**，不可采集置 `unknown`（NFR-19） | FR-0.6/FR-11 层 5 | P3 联调（执行包 #P3） |
| A-10 | 交换机 NOS 为 Cumulus Linux 5.x，可用 SSH/NVUE/ethtool/syslog/DDM；其他 NOS 经适配层接入 | FR-0.3/FR-0.5/FR-0.6 | P2/P3（NFR-8） |
| A-11 | 客户可提供商定后的网络配置基线（地址/命名/VLAN/ASN/VNI/QoS/breakout）用于导入 | FR-0.2/FR-0.4.1 | P2 导入承接（PRD §1.4 Out of Scope 的商定部分） |

## 11. 模块-服务映射（架构 ⇄ PRD 对照，按 FR 序排列）

| PRD 模块 | 架构承载 | 核心表 |
|---|---|---|
| **FR-0 网络底座交付域（前置域）** | **fabric-svc（物料/规划/ZTP/意图/Doublecheck/比对/预检/PushJob）+ fabricmon-svc（交换机采集/无损判定/健康分/日志）+ netbaseline-svc（网络配置基线导入+pongo2 沙箱渲染）+ boot-svc（Kea/引导/身份反查）** | **BOMImportPackage/BOMItem/Switch/SwitchPort/OpticModule/FabricReadiness/NetworkIntent/NetworkSnapshot/NetworkDiff/PushJob/ZtpSession/FabricRule/HealthScore** |
| **FR-0.8 网络证据与排障接口域** | fabric-svc 对外契约 + 第三方适配器（ADR-14/ADR-19） | FabricReadiness/FabricBaselineExport/证据引用 |
| FR-2.1 BOM 导入包 | fabric-svc + model-svc（期望值）+ profile-svc（字段映射）；provision-svc 消费 | BOMImportPackage/BOMImportManifestFile/BOMImportIssue/BOMItem |
| FR-1 作业平台 | flow-svc + execution-svc + 双通道 Runner + outbox/Stream | Flow/FlowVersion/Execution/TaskAttempt/ResourceLease/OutboxEvent |
| FR-1.8 介质库 | media-svc + MinIO media 桶 + nginx 镜像源 + agent 缓存协议 | MediaItem/MediaDistLog |
| FR-1.9 参数目录 | catalog-svc + 介质元数据联动 + Execution 快照冻结 | ParamKey/ParamSnapshot |
| FR-2 PXE（含互信） | provision-svc + render-svc（cloud-init 公钥注入）+ 首连验证 | BOMImportPackage/BOMItem/Machine/TrustLog |
| FR-3 采集 | redfish-svc + 原始层 + JSONB 视图 | CollectRecord |
| FR-4 验机 | validation-svc(G1/G2) + mapping-svc(G3) + issue-svc | Component/PhysicalPort/GateEvidence/GateRecord/MappingRevision/PortModel/Issue |
| FR-4.5 网口监控视角 | portmon-svc（消费 mon-svc 数据）+ ServerDetailDrawer 聚合 API | PortModel/PortEvent |
| FR-5 配置（含 FR-5.9 角色） | config-svc + render-svc（参数占位符 + 角色渲染） | ConfigSet/ConfigApply/RoleBinding |
| FR-6 NCCL | nccl-svc + report-svc（S1–S9 + 溯源 + 调优清单） | NCCLTestRun/GroupRun/NCCLIssueLink |
| FR-7 自定义作业 | flow-svc 设计器 + 蓝图库 + media/param 函数解析 | Flow |
| FR-8 AI | ai-svc + RAG（FR-6.7 三库种子） | AIProvider/AIInvoc/KnowledgeDocument/KnowledgeChunk |
| FR-9 退出（含 FR-9.6 维护态） | cleanup-svc（清理 + 互信撤销 + maintained 转换事件） | CleanRun |
| FR-10 平台级 | api-gateway(session/RBAC/CSRF/project) + notify-svc + audit-svc | User/Session/ProjectMember/AuditLog |
| FR-11 连通性测试 | nettest-svc（五层编排 + **FR-0.8 网络计数消费**，域内 fabricmon-svc / 第三方适配器） | NetTestRun/NetTestPairResult |
| FR-12 项目模板 | profile-svc（模板版本化 + 型号引用 + 项目初始化） | ProjectProfile |
| FR-13 监控告警 | mon-svc（策略下发/ingest/告警引擎/停采转换） | CollectPolicy/MetricSample/Alert |
| FR-14 型号库 | model-svc（型号版本化/槽位/角色默认/唯一性约束；BOM 导入校验的期望值定义方，导入侧调用其接口） | DeviceModel/DeviceModelVersion/DeviceModelSlot/RoleBinding |
| FR-15 CMDB | cmdb-svc（联邦视图聚合/快照归档/人工字段/AIOps 导出） | CMDBRef/CMDBSnapshot/CMDBManualField |
| FR-16 BMC 带外管理 | redfish-svc 扩展（BMC 控制/BIOS/BMC 设置/固件升级，ADR-16）+ mon-svc（维护期轮询，ADR-18） | BiosConfigSet/BiosApply/FirmwareRun |
| FR-17 项目执行/双平面 | api-gateway（显式 project_id 授权）+ profile-svc（生命周期），P1 建立 | Project/ProjectAsset/ProjectMember |

> **支撑服务注记（v2.2）**：notify-svc / audit-svc / profile-svc / report-svc 为横向支撑服务，分别挂接 FR-10（通知/审计）、FR-12（项目模板）、FR-6 与 FR-11（报告聚合共用）；完整服务清单见 §1.2 领域服务层。BOM 导入校验职责边界：model-svc 拥有型号与 BOM 期望值定义，catalog-svc 仅负责参数目录，二者不重叠。
> **网络域服务注记（v3.0）**：fabric-svc / fabricmon-svc / netbaseline-svc / boot-svc 为网络底座前置域承载，命名前缀 `keel-fabric`（ADR-19）；复用 model-svc（网络设备型号 `kind=switch`）、catalog-svc（阈值单一源）、audit-svc/notify-svc/report-svc。**网络配置变更唯一走 PushJob 主链路，无旁路直下发**。

## 12. 端到端依赖总图

````
                    ┌──────────┐         ┌──────────┐
                    │ 项目模板  │ FR-12   │ 型号库    │ FR-14
                    └────┬─────┘         └────┬─────┘
              套用初始化 │（参数默认值/蓝图/      │ 槽位/角色方案/BOM 期望
              门禁策略/型号引用）                │
                        ▼                     ▼
  ┌──────────────────────────────────────────────────────────────┐
  │ 网络底座前置域 FR-0（keel-fabric）:  BOM导入包→网络建模→ZTP→     │
  │ 网络侧 N-S5 预校验→Golden闭环→全网健康 → NP-1/2/4/5/6       │
  └───────────────────────────┬──────────────────────────────────┘
                              ▼ NP-1/2/4 启动 G2；NP-5/6 准入 G5/G6
 ┌──────┐  OS镜像(media://)  ┌─────────┐  公钥注入   ┌──────────┐
 │介质库 │──────────────────▶│ PXE装机  │───────────▶│ SSH互信   │
 │FR-1.8│                    └────┬────┘            └────┬─────┘
 └──┬───┘                          ▼ 回调               established
    │ 参数目录FR-1.9          ┌─────────┐                   ▼
    │ (下拉传参)              │ 预收集G1 │◀── 事件触发 ───────┘
    │                        └────┬────┘
    │                             ▼
    │        ┌────────────────────────────────┐
    │        │ LLDP线序G2（NP-1/2/4 后启动） │
    │        └────┬───────────────────────────┘
    │             ▼ NP-3 端口对三方确认；差异 T1–T7
    │        ┌─────────┐    开单     ┌──────────┐
    │        │ 工单闭环 │◀───────────│ issue-svc│
    │        └────┬────┘   处置+重验证 └──────────┘
    │             ▼ 关闭
    │        ┌─────────────┐ MappingRevision ┌───────────┐
    │        │ Mapping G3  │──────────────▶│ 网口监控    │
    │        └────┬────────┘               │ portmon   │
    │             ▼                        └─────┬─────┘
    │        ┌─────────────┐   参数快照+        │ 告警
    │        │ 基线配置 G4  │◀─ 角色方案(FR-5.9)  │ 开单
    │        └────┬────────┘   ← model-svc      │
    │             ▼                             ▼
    │        ┌──────────────────────┐    ┌──────────┐
    │        │ RoCE连通性测试 G5     │───▶│ 工单闭环  │
    │        └────┬─────────────────┘    └──────────┘
    │             ▼ netverified
    │        ┌─────────────┐  失败溯源/调优
    │        │ NCCL测试 G6  │────────────▶ S8/S9 → 工单/ConfigSet
    │        └────┬────────┘
    │             ▼
    │        ┌─────────────┐ 先归档ACK/再撤销 ┌─────────────┐
    │        │ 优雅退出     │◀─────────────▶│ CMDB 归档    │ FR-15
    │        └────┬────────┘  ssh_trust=    │ (全息快照/    │
    │             │            revoked      │  maintained) │
    │             │                          └──────┬──────┘
    │             ▼                                 ▼
    │        ┌──────────────────────────────────────────┐
    └────────┴─▶ 全程：介质分发（media://）+ 参数传参（param()）+ 审计   │
             └──────────────────────────────────────────┘
                                                    │
                                     AIOps/运维系统 ◀┘（OpenAPI/导出消费）
````

**依赖不变式（架构级断言，CI 校验，[I-1]–[I-12]，v3.0）**：

1. **[I-1] SSH 准入**：普通任务必须 established；仅受限 `trust.bootstrap`/`trust.repair` 可使用批准的一次性凭据建立/修复互信。不可将该例外扩为任意脚本；maintained 不允许通过该入口自动恢复带内访问。
2. **[I-2] 介质引用**：涉及介质分发/安装/刷写的任务必须引用已批准 `media://` 对象与摘要；纯查询、等待、KV 等任务无需虚构介质引用。Flow 保存可接受合法草稿，发布/执行时完成引用和依赖校验。
3. **[I-3] NCCL 准入**：本次目标集合/通信组所需 G5 范围和路径必须有当前有效 GateEvidence，版本/档位匹配；tested 等后续状态可重测，maintained 仍拒绝。前后端均检查，API 返回实际缺口，不能以 Machine.state 恰等 netverified 代替。
4. **[I-4] 工单修复与复测**：正常测试遇关联的未处置阻断告警拒绝；`issue.verify` 可在绑定工单/精确范围/有效权限下复测以形成关闭证据。G3 前的 T7 等允许获批 `issue.repair` 修复受影响上游门禁，不豁免身份/凭据/介质兼容/资源锁或维护白名单，修复后必须重验。
5. **[I-5] 快照完整**：Execution 必须冻结项目、目标、FlowVersion、ParamSnapshot、介质摘要与 GateEvidence 指纹；参数可为空对象但快照不可缺失，不执行可变草稿。
6. **[I-6] 独立期望与身份**：G1 前必须绑定已发布 model_version_id；Component/PhysicalPort 身份先于 G2；G3 确认 MappingRevision 并进入 mapped。BDF 仅为观察值，实际数据不能自行构造独立预期。
7. **[I-7] 角色方案同源**：unused 槽位的配置跳过、监控不作在用端口告警、BOM 期望一致；仅检查项目 enabled_required_roles，角色/型号版本变更使受影响证据失效。
8. **[I-8] 维护期白名单**：带内任务/动态采集停止；允许 BMC 自动只读轮询、人工只读刷新、人工 UID 和经独立审批授权的电源动作，B7 告警继续。其他动作须显式维护变更流程，不能借通用 Flow 或监控白名单执行。
9. **[I-9] 豁免有限范围**：有效豁免只覆盖批准缺口、目标、版本和时间；进入 Waiver Register。过期/范围变化/上游版本改变重新阻断，GA 前清零或在签署交付报告明确接受，不可让 unknown/skipped 默认为 PASS。
10. **[I-10] 项目隔离**：请求、Query key、SSE、草稿、确认框均明确 project_id；授权以请求与资源实际归属为准，不受其他标签页修改 session 偏好影响。两种切换入口只导航当前页；跨项目运维仅聚合用户获授权的范围。
11. **[I-11] 网络证据不冒充 PASS**：网络前置（NP-1～NP-6）与无损判定（LL-01～LL-07）在数据不可采集/规则停用/设备能力不支持时，结论必须为 `unknown`/`BLOCKED` 并标注 `degraded`/`approximation`/`estimated`，**不得静默为 `normal`/PASS**；G5/G6 消费网络证据时按 GateEvidence 四值枚举（`PASS/FAIL/BLOCKED/WAIVED`），缺数据不得推定通过（NFR-19、PRD §1.5.3）。
12. **[I-12] 导入包单批次一致性**：标准 BOM 导入包（PRD 附录 A.5）须包含七个逐实体 CSV，在一次提交内保证服务器物料与网络物料的引用完整性——线缆两端、`parent_uid`、端口 `(parent_uid, port_index)` 全部可解析，同一 `bom_import_batch_id` 同摘要幂等、异摘要拒绝覆盖；`BLOCK` 问题未清零不得进入 N-S4/ZTP 与 PXE 装机；字段映射由项目模板声明，不得代码硬编码。

**验证落点**：契约/RBAC schema 检查 + gate/执行状态机集成测试 + 双标签页 E2E + 故障注入。覆盖事件重复/丢 ACK、已执行未回执、过期证据/换件失效、异通道资源争用、整组 NCCL 失败、T7 修复与工单复测、撤销后断线恢复、跨项目缓存/SSE 和无权限拖拽。mock/模拟规模、真实传输和真机 PoC 分开报告，不将设计契约存在当作实现通过；**网络侧增补：ZTP 引导失败注入、PushJob 13 态与灰度/回滚、规则 dry-run 零副作用、无损降级三路径（无 WJH/per-XOFF/队列深度）全部 `unknown`、导入包缺列/坏引用/路径穿越注入**。mock/模拟规模、真实传输和真机 PoC 分开报告，不将设计契约存在当作实现通过。
