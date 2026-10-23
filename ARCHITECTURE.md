# ARCHITECTURE.md — keel 自动交付部署平台架构说明

| 文档属性 | 值 |
|---|---|
| 版本 | v3.0.3（依据 PRD 与 DESIGN-SYSTEM 重整，保留既有契约锚点） |
| 状态 | 目标架构，不代表已实现；实际源码、运行版本和验收证据只查 docs/HANDOFF.md 与工作树 |
| 上游基准 | PRD.md v3.0.3：业务范围、判据、BOM；DESIGN-SYSTEM.md v3.0.3：工作台、品牌、组件和交互 |
| 本文职责 | 模块边界、数据归属、执行一致性、API/事件、状态和安全；工作顺序查 IMPLEMENTATION-PLAN.md，验证方法查 ACCEPTANCE.md |
| 技术命名 | keel、keel-agent、keel-cache、keel-ctrl-*、KEEL_*；网络前置域为 keel-fabric |

**阅读规则**：先读上游相关 FR 和 UI 章节，再读本文对应模块及不变式；不从旧页面、历史审查记录或架构示意图推导产品要求。阈值与参数默认值只引用 PRD，API 完整 schema 只维护在 `api/`，实现日志只写 HANDOFF。本文保留编号供跨文档引用。

**基准冲突入口**：统一查 [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) §2。显示品牌采用DS、技术标识保留keel；业务平面/门禁按PRD，布局/组件按DS（ADR-17）。未裁决条目保留待决，不能把两份上游写成已经一致。

## 1. 架构总览

### 1.1 系统上下文（C4-L1）

```text
浏览器 ──同源 HTTPS──> keel 控制面（API / UI / Flow / 门禁 / 证据）
                         ├─ keel-fabric ──SSH/NVUE/遥测──> 交换机
                         │   ├─ boot-svc ──控制适配器──> Kea DHCP
                         │   └─ FR-0.8 <──第三方网络平台适配器（可选）
                         ├─ Agent gRPC / SSH Runner ──> 目标服务器
                         ├─ Redfish / IPMI适配器 ──> BMC
                         ├─ HTTP介质 / PXE引导 ──> 固件、NOS、OS
                         └─ OpenAPI / 导出 ──> IPAM（可选）、客户 AIOps
```

FR-0 是整条交付链第 0 步。网络配置内容由客户与网络工程师商定后导入，平台承接版本化、渲染、校验、审批和执行。服务器域只经 FR-0.8 消费网络证据；CMDB 为内置联邦视图，对外供数，不建立第二套资产主数据。

### 1.2 逻辑分层（C4-L2）

| 层 | 职责与唯一数据路径 |
|---|---|
| 前端工作台 | React/TypeScript SPA；必须实现 DS §2.4、§3 的顶栏、可折叠侧栏、双平面及项目上下文；路由与组件不在本文另维护副本 |
| API边界 | 认证 → 项目/资源授权 → CSRF → 领域动作 → 审计；会话偏好不决定请求项目 |
| 领域模块 | Go 模块化单体，模块拥有自己的写入和判定，跨模块通过服务契约消费；完整映射见 §11 |
| 执行层 | Scheduler → Dispatcher → Agent/SSH Runner；BMC、Kea、设备控制经各自适配器进入同一执行/锁/回执机制 |
| 数据层 | PostgreSQL为状态、身份、证据索引、outbox与锁的权威；Redis为可重建队列/缓存；MinIO为介质/原始证据/报告；ClickHouse为可选时序实现 |

`*-svc` 是单体内职责名，不要求一模块一进程或一套数据库。网络域：fabric-svc 编排与判定，fabricmon-svc 采集与规则，netbaseline-svc 基线与网络模板，boot-svc 引导与身份；复用型号、介质、参数、执行、审计和报告模块。

**代码模块与修改入口**：以现有代码增量组织；`internal/platform`的Store/用例随FR-0完整能力逐步拆出，不先全仓重构、不造大量空包。目录名是实现选择，以下依赖边界必须保持：transport/UI调用usecase，usecase依赖domain与能力接口；store/adapters实现这些接口，由启动组合根注入。domain不导入HTTP、数据库或厂商SDK。

| 职责/修改入口 | 允许修改的内容 | 变更后重点验证 |
|---|---|---|
| domain | 身份、状态转移、门禁、证据有效性/范围与业务不变式 | 有变化的决策单测、对应状态/授权负例和功能链 |
| usecase | 批次编排、事务/锁/幂等、审批与执行快照；复用现有Runner | 原子性、恢复/争锁及该完整能力的集成 |
| store | PostgreSQL查询/迁移/实体映射；当前platform逐能力分离 | 迁移兼容、事务与真实持久化；不在SQL散落厂商命令或另作业务判定 |
| transport | 现有HTTP/gRPC/SSE handler、OpenAPI/RBAC适配 | 输入/错误/项目授权/事件契约；只调用用例，不直接执行设备命令 |
| UI | 现有web feature/route与DS共享Shell/组件 | 页面数据/动作、状态与项目隔离的浏览器验证；不接收凭据/厂商命令选择逻辑 |
| adapters | 现有fabricboot等基础上分离Kea、ZTP、NOS采集/解析、BMC/SSH及凭据访问 | 协议连通、能力/超时/幂等、脱敏原始样本→golden契约；更换实现保持用例接口 |
| 版本数据/配置 | 型号能力、NOS命令/字段映射、指标注册、模板/规则与批准基线；endpoint/CA/超时/重试上限/并发 | Schema/单位/版本引用/错误缺口与受影响能力；禁止硬编码到状态机 |

PoC出现差异先定位协议/解析/能力矩阵/数据schema/规则，回录脱敏原始样本并定向验证；仅业务流程或判据确实变化才修改domain。替换NOS/Kea/凭据适配器应不改handler/UI/门禁，若必须改则先修正泄漏的接口边界。接口按“采集LLDP/验证活动配置”等业务能力定义，不能把整段CLI透传当抽象；配置化不得绕过审批/unknown/重试安全约束。

### 1.3 设计原则

1. **事实分离**：BOM 是预期物料，DeviceModelVersion 是独立型号期望，采集是观察，MappingRevision 是确认映射；各自版本化，不能用观察值生成期望后自证。
2. **版本冻结**：Flow、项目模板、型号、介质、参数、规则和证据在执行时绑定不可变版本；回滚创建新版本或关联执行，不改历史。
3. **受控副作用**：Runner只执行控制面已批准命令；所有通道共用资源互斥、稳定command_id和回执；结果未知先对账。
4. **显式缺口**：unknown、stale、unsupported、sim和有效豁免均独立记录，任何一项不自动等于PASS。
5. **双通道等价**：服务器全交付链支持Agent与SSH；Agentless观测能力按PRD显式降级。网络侧不依赖服务器Agent。
6. **可恢复且可退出**：持久化步骤、outbox、回执和所有权台账支撑恢复；固定路径不意味着可清空整个目录。
7. **完整能力增量交付**：复用现有代码、Runner与适配器，一次交付契约+真实持久化/权限/API+DS页面；mock/sim限外部硬件/协议边界，不把手动推进模拟会话扩成主要产品。无硬件时明确unknown/PoC缺口并继续软件链路，不能反复完善模拟世界替代开发。

## 2. 技术选型与理由

### 2.1 后端（Go）

| 领域 | 选型 | 使用边界 |
|---|---|---|
| 控制面 | Go 1.23+、gin v1.10+、ent v0.14+ | 单二进制/模块化单体；复杂事务可用受控原生SQL，不建第二套状态 |
| 执行与事件 | PostgreSQL + Redis zset/Streams、robfig/cron v3 | PostgreSQL持久化；Redis可重建；事件语义见ADR-01 |
| 执行通道 | gRPC双向流、x/crypto/ssh、gofish+自有BMC适配层 | 通道能力、错误/超时/幂等/回执契约一致 |
| 渲染 | 服务器ConfigSet用Go text/template；网络用pongo2 v6 | 两种语法隔离；网络沙箱禁止文件/网络访问 |
| 校验/观测 | validator v10、slog、OpenTelemetry、Prometheus | 统一错误、trace与指标契约 |
| 密钥 | 内置AES-GCM加密存储，可外接Vault | 领域表只存凭据引用，不存明文 |
| 工具适配 | iperf3/perftest/ping/arping、Batfish、可选containerlab | 集成已批准工具；输出通过版本化解析器与golden契约验证 |
| AI | Provider接口+OpenAI-compatible/Anthropic/vLLM | 只输出建议/草稿；不持有设备执行凭据 |

Kestra 是 Flow/Execution、恢复与交互的参考，当前架构仍为 PRD FR-1 的自研作业平台；Kea 是网络 DHCP 服务。二者职责和接入点不同，不能用mock Kea证明作业引擎或真实DHCP已完成。

### 2.2 前端

| 领域 | 选型 | 使用边界 |
|---|---|---|
| SPA/路由 | React 19、TypeScript 5、Vite 6、TanStack Router | 从URL派生项目域，遵循DS工作台与页面IA |
| 数据/UI状态 | TanStack Query v5、Zustand | 服务端缓存键含项目；草稿不随其他标签页项目切换漂移 |
| 表格/矩阵 | TanStack Table v8 + TanStack Virtual | 双轴虚拟化、服务端筛选/cursor与聚合分离 |
| UI/表单 | CSS令牌组件层+Radix、React Hook Form+zod、i18next | 令牌源、品牌、布局、状态集和权限映射以DS为准 |
| 图表/编辑 | ECharts 5、Monaco、@xyflow/react | 不给每行创建图表；YAML/No-code共用AST |
| 拖拽 | @dnd-kit/core | 工单泳道动作和表格个人列顺序；不绕过业务状态机 |
| 验证 | Vitest、Testing Library、Playwright | 覆盖真实渲染/操作和项目隔离，构建成功不等于UI符合DS |

### 2.3 基础设施

PostgreSQL 16、Redis 7.x、MinIO；ClickHouse 24.x+按容量档可选；Compose/Helm对应single-node/ha-cluster。介质HTTP/镜像源与PXE分发按ADR-07复用存储。服务器PXE的ProxyDHCP与网络ZTP的Kea按管理网规划配置，不能同时无边界争用同一广播域。

**构建锁定**：版本族表示技术路线，不是浮动安装命令。工具链文件、go.mod/go.sum、package-lock.json与镜像digest锁定经验证版本；升级独立验证，不使用latest。控制面/agent覆盖linux/amd64和linux/arm64，HGX B300与GB300的兼容组合分别登记。

## 3. 核心架构决策（ADR 摘要）

### ADR-01 事件总线：Redis Streams（关键事件持久化）+ Pub/Sub（通知类）（v2.2 修订）

业务状态与OutboxEvent在同一PostgreSQL事务提交。发布器重试到Redis Streams；Consumer Group以ACK/PEL保证至少一次投递，消费者在业务事务写唯一`(consumer,event_id)`并CAS聚合版本后ACK。CloudEvents schema版本化。UI刷新通知可用Pub/Sub，轮询兜底；关键状态不能只放Pub/Sub或Go channel。

TaskAttempt使用稳定command_id/attempt_id/fencing_token，Agent/SSH包装器持久化接收、启动、完成回执。超时先查回执与设备；非幂等副作用未知时保留锁进入对账，禁止自动重复刷写/下电。Redis丢失从outbox与待执行状态重建，历史结果以DB和归档为准。

### ADR-02 Agent 协议：gRPC 双向流（保留 v1 + 扩展）

Agent注册携带SN、版本和可信身份，经enrollment签发设备mTLS证书；双向流多任务复用、断线退避。心跳周期受CollectPolicy档位控制，携带端口增量和介质摘要清单；统一`Agent → mon-svc ingest → MetricSample/告警 → portmon/Dashboard`，展示模块不直接订阅原始心跳。周期和可见延迟引用PRD FR-13/NFR-12。

### ADR-03 Agentless：SSH 连接池 + 幂等脚本（保留 v1 + 扩展）

SSH连接池复用项目/rail域密钥；目标flock只防本机重复，跨通道互斥仍由§4.3控制。普通任务须`ssh_trust=established`；仅provision/trust可提交受限trust.bootstrap/repair，绑定一次性凭据、目标与审计。并发/连接池按PRD FR-1.4和参数目录配置，不在此另设默认值。

### ADR-04 PXE：HTTP-first + iPXE 链式引导（保留 v1 + 扩展）

ProxyDHCP+iPXE+HTTP分发；cloud-init在分发前渲染hostname/域公钥。一次性可信回调绑定目标身份后自动首连验证，写ssh_trust并触发预收集；失败进入受限修复队列。OS镜像、脚本与依赖全部来自已批准介质。

### ADR-05 采集分层：原始层 + 结构化层（保留 v1）

Redfish原始JSON存MinIO，结构化记录入JSONB并保留raw_ref/来源/观察与接收时间；解析器版本固定，支持从原始证据重放。网络命令/日志同样保留原文与结构化产物，不能只留UI结论。

### ADR-06 SSH 互信自动建立与生命周期（v2.0 新增）

平台按项目或rail持有ed25519域密钥（keel-ctrl-*），私钥只在控制面。主路径为cloud-init注入公钥；一次性回调或获授权导入的可信host-key指纹用于首连验证，禁止自动接受未知/变更指纹。`uninitialized/established/broken/revoked`为独立准入状态。

密钥轮换先追加新钥、逐机验证再撤旧钥。批准的批次凭据仅用于一次性首登注入，逐目标消费、限定有效期，批次结束撤销平台持有的临时凭据；不得自动修改客户root/既有账户口令。root作业显式声明、审批并将脱敏会话归档到控制面AuditLog引用。退出按§5.6分阶段归档、精确删平台公钥并验证新连接拒绝；maintained不能经普通repair恢复带内访问。当前路线不用SSH CA；未来引入须独立ADR与现场支持证明。

### ADR-07 介质库：对象存储 + 内建镜像源 + 固定缓存契约（v2.0 新增）

MinIO media桶保存不可覆盖版本；任务push、HTTP Range直拉、apt/yum/dnf镜像源均解析同一`media://`对象。目标缓存`/opt/keel-cache/media/<sha256>/`，只对未命中目标传输，命中仍校验摘要。冷缓存带宽按全量计算；包管理器不默认具有事务回滚。

登记校验唯一版本、签名、摘要、OS/arch/型号和依赖闭包，业务执行只选approved版本；离线清单包括仓库元数据、签名和完整工具组合。缺依赖/未批准/摘要错误阻断，不能无限重试或替换摘要放行。

### ADR-08 PortModel：网口可观测与故障定位的统一底座（v2.0 新增）

BOM/预收集先建Machine→Component→PhysicalPort稳定身份，未映射端口也可开单；PortModel是当前观察投影，G3才发布不可变MappingRevision。OS名/BDF不是资产主键。换件新建部件/端口实例并结束旧在位关系，历史工单/结果保留原引用；未知字段显示unknown/unmapped。

### ADR-09 门禁扩展与跨系统就绪握手（v2.0 新增）

G1–G6是唯一门禁链；N-S0～N-S9仅为网络阶段标签。G2启动要求本端G1/拓扑和NP-1/NP-2/NP-4，G2三方确认产生NP-3；G5/G6另消费NP-5/NP-6。readiness只为展示短时缓存；提交及实际派发均按§4.4重验范围、版本、新鲜度并冻结证据，UI禁用不替代后端准入。

NP-2必须同时有ZTP完成与Golden配置同步证据，不由N-S4单独产生；N-S5不产生NP-3。所有结论引用PRD §1.5.3，不能用项目通过率、managed或Machine.state替代。

### ADR-10 监控单一数据源与频次控制（v2.1 新增）

mon-svc为服务器遥测唯一ingest。Agent的D1–D6与Agentless受限D1/D2经同一结构化契约入库，源/时间/质量保留；实施期BMC作为带外旁证，维护期改B7独立告警。CollectPolicy按M1–M4控制本地采样、delta上报、动态测试窗口、随机分片和退避。cleanup先停带内作业/采集并确认，再撤销凭据；不能仅停UI订阅。网络侧独立由fabricmon采集，不依赖服务器心跳。

### ADR-11 DeviceModel 型号库与数据条目冲突防护（v2.1 新增）

DeviceModel是稳定标识，DeviceModelVersion是不可变硬件/槽位/拓扑/能力/面板/角色期望。ProjectAsset固定已发布版本；Machine当前型号是投影。唯一键为`(scope_key,name)`、`(model_id,version)`、`(model_version_id,slot_no)`；scope_key显式global或project:<id>，避免NULL唯一键漏洞。

共享型号修改用新版本或项目派生，不改旧绑定；被引用版本只可退役，禁止硬删除。RoleBinding固定型号版本与项目角色方案，完整性仅检查enabled_required_roles，unused同源驱动BOM/配置/告警。迁移预览差异并使相关证据失效；不从实测BDF排序自动构造独立期望。

### ADR-12 CMDB 联邦视图（v2.1 新增）

CMDB只聚合源模块，不复制成第二套主数据。CMDBRef存源记录/版本索引；人工字段独立source=manual；cleanup冻结范围/源水位后生成CMDBSnapshot并等待对象/索引ACK。单机360°走索引聚合，项目统计走事件增量物化视图+每分钟全量对账；性能/滞后引用NFR-14。维护期只经BMC白名单刷新，OS数据保留归档时间，禁止偷偷重建SSH。

### ADR-13 Agentless 交付模式为一等能力（v2.1 新增）

TaskCommand定义通道无关语义，Agent/SSH Runner实现相同结果、取消、回执契约；无Agent时交付链仍可完整运行。门禁依赖明确新鲜度/版本的任务采集证据，不强制常驻心跳；监控按FR-1.10/13.4降级并显示快照时间。双通道等价用契约测试证明，agentless→agent切换再恢复动态观测。

### ADR-14 环境抽象与离线优先（v2.1 补充）

所有硬件/外部交互经Environment Adapter（网络证据、Kea、SSH、BMC、PXE、打流、AI），业务只依赖按能力定义的接口。外部设备mock/sim为开发默认档，real按显式配置启用；数据库、授权、业务API和UI使用真实链路，不另造模拟实现绕过它们。复用现有适配器/Runner，mock与real共用接口/错误/超时/幂等契约；故障fixture与golden样本只覆盖必要协议边界，手动模拟控制台仅开发工具，不扩张为主产品。真实地址/凭据不能写死，endpoint/CA/超时/重试上限/并发配置化，NOS命令解析和凭据读取独立可替换。

证据区分`evidence_origin=sim|real`，观测对象另标`source=host|switch`等；不可采集带`degraded/approximation/estimated`标记；sim产物只可通过仿真门禁，不能更新真实managed、IN_SYNC或现场NP断言。未支持能力返回unknown，未执行PoC为NOT_RUN；硬件性能/GA由ACCEPTANCE H组独立留证。授权开发服务器可正常SSH/Git/安装依赖，与交付目标机边界不同。

### ADR-15 PCIe 拓扑模型：sysfs 上游路径 + nvidia-smi topo 语义对齐（v2.3 新增）

实际拓扑从sysfs的physfn/bridge链获得上游BDF与NUMA，LnkCap/LnkSta取PCIe capability；NVLink只取nvidia-smi。独立期望来自已发布型号版本。对照挂载归属、NUMA、NVLink、带宽协商形成TopologyDiff，affinity语义须与nvidia-smi topo -m一致。差异开T7，按证据选配置/冷复位/批准固件/硬件检修，不默认刷BIOS；重验后才进G2。

### ADR-16 BMC 控制平面：双源标注与通道矩阵（v2.3 新增）

redfish-svc复用既有BmcClient适配层。字段标source=agent|bmc，实施期不无标混源，维护期仅BMC；未知传感器/NIC灯不推定正常或断电。电源/UID取ActionInfo/AllowableValues确切能力，Nmi不是普通reboot；强制动作独立审批。

BIOS设置/固件按型号capability_matrix选择Redfish、带内工具或人工路径，不预设CPLD通道；维护期禁带内。每动作声明取消点、任务查询和恢复/回滚能力；超时先查TaskService与实机版本，未知不重刷。影响拓扑/数据面使相关证据失效。可能断管理链路的配置采用备份+target-side watchdog限时应用+独立新连接确认；不支持者须已审批带外恢复方案，BIOS/固件不默认自动回滚。

### ADR-17 项目执行与双平面（v2.3 新增）

Project/成员/资产归属从P1建立。部署域按项目，运维域聚合用户获授权项目，默认项目过滤可移除；Machine为全局实物，ProjectAsset保存使用历史，禁止活跃项目无授权争用。Project生命周期与Machine/Gate独立，版本CAS及证据控制转移。

ProjectContext从当前标签URL派生；所有请求、Query key、SSE、筛选/草稿/确认框显式固定project_id。顶栏切换与侧栏项目入口只导航当前页，不改其他标签的项目/目标。session最近项目仅偏好；运维过滤变化不扩大权限。

**Fabric平面裁决**：PRD FR-17.1的运维平面归属优先于DS §3菜单位置。Fabric总表/规则/无损默认查询获授权项目集合，项目深链或过滤只限制该视图；ZTP/PushJob等写动作始终固定一个项目与批准范围。可从项目时间线深链进入相同工作区，不创建第二套数据/导航事实源；具体规范路由在契约实现前统一登记DS/路由清单。工作台外壳必须遵守DS §2.4（56px顶栏、240→56px侧栏、内容全宽）及§3，不能沿用横向标签外壳后声明设计已交付。

### ADR-18 维护期 BMC-only 监控（Redfish 主通道 / IPMI 兼容回退）

技术清理后，mon-svc调度BMC只读轮询Execution，传感器/SEL/Health/电源状态写B7并继续post-delivery工单。白名单只含自动只读轮询、人工只读刷新、UID与有独立授权/审批的电源动作；其他变更须显式维护流程。平台只保留授权BMC凭据引用，清除临时OS凭据。

容量按完整设备轮询计算：设备数/周期给设备轮询率，再乘每台请求数k与请求耗时求并发；不能把10k设备当10k请求。默认周期/上限与新鲜度引用NFR-16，逾期显示stale并告警，不能静默拉长周期；扩容/分片/批量读取后重新实测。

### ADR-19 网络底座交付域：前置域承载与证据接口（v3.0 新增）

FR-0作为keel-fabric内置第0步，复用软件平台底座，先于依赖它的服务器验证；服务边界按§11，不另建NDP产品或物料库。网络内容来自批准的客户/NE基线，项目差异由ProjectProfile承载；平台不替客户决定地址/VLAN/ASN/QoS/RoCE方案。

主链唯一为`基线导入→版本化/渲染→Doublecheck→备份/结构化diff→修复计划→Batfish/预演→设备预检与审批→PushJob→复采比对`，具体先后和判据以PRD FR-0.4为准。首次ZTP只提供批准最小隔离/管理配置；生产配置和漂移修复共用PushJob。首次生产配置预检不得要求既有IN_SYNC或未装机主机LLDP；批准的预期漂移不是自我阻断条件。普通NE+DE双签、Critical追加NH，执行人不得自审批。

网络pongo2 v6与服务器Go template隔离；沙箱BanTag/BanFilter，禁文件/网络/绝对路径访问，发布模板须渲染→三层Doublecheck→仿真下发→回读golden测试。阈值/安全区间来自catalog+ProjectProfile，规则只改判定，不改采集计划或设备配置；LL-01～LL-07每个产物携带rule_id@version，缺能力/停用为unknown。

FR-0.8是服务器域唯一入口，内置与第三方适配器提供相同就绪、LLDP、线缆、无损、DDM/日志、基线导出契约；不支持返回unknown而非空成功。NP-1/2/4是G2输入，NP-3是G2输出，NP-5/6是G5/G6输入；N-S5只有预校验。NP-2依赖ZTP完成+Golden当前同步证据，N-S4完成不自动满足。

### ZTP 身份底座（WP-1.17 增量）

SN优先于管理口MAC、基板MAC及其他永久MAC；多键不一致判conflict，未命中SoT只得隔离模板。同SN重装沿用身份，RMA新SN新建身份，旧绑定退役留历史。交换机身份三列更新/继承与冲突规则引用PRD附录A.7.4，不能用首次请求覆盖SoT。

boot-svc将239（管理网直接对端+六个CUMULUS-*头）与143（直接mTLS+IDevID证书链/SN）收敛到同一身份反查/渲染。URL绑定项目/批次/阶段/批准介质，不携设备身份；已生效Kea路由、脚本批准、SHA256/大小和管理网边界须逐次复核。冲突不提供脚本，隔离不等于managed；设备声明bootstrap-complete不等于已复验可管理。Cumulus标记、退出码、前面板DHCP风险和143/136协议按FR-0.3；option60/43是厂商/NOS分类选项，不是第三种引导通道。

Kea由boot-svc控制适配器执行staging→验证→commit→活动配置回读，作业引擎负责审批/互斥/重试/审计；真实DORA、持久化配置与设备可管理须独立证据。仿真计划/会话/重试不能代替真实Kea。当前进度与未接通环节只查HANDOFF，本节不保存“已实现”日志。

### 端口类型从型号版本派生（WP-1.15 增量）

BOM端口sub_type由父设备已发布型号`capability_matrix.ports[port_index].sub_type`派生；breakout子口可按型号规则继承父口child_sub_type。字段缺失/无效按PRD阻断，禁止以临时端口类型冒充完整型号。宽表临时NIC/端口仅供兼容，真实身份接管须唯一永久MAC且保留版本链。

### 交换机端口型号能力矩阵（WP-1.16 增量）

型号版本拥有端口speed/speeds、breakout_modes、介质能力和独立rail/电源期望；BOM只声明计划值。导入逐口核对子口范围、模式速率、光模块能力、rail、上下行容量和电源预算，工程限值来自批准network_baseline_versions（SN优先/角色回退）；明示不符BLOCK，缺能力/缺限值WARN并保留unknown。breakout父子容量不重复计数。判据和缺测口径只引用PRD FR-0.1.3/附录A.7，不能把工程限值子集批准当完整网络基线或FR-0 PASS；规划rail不能代替G2实测三方确认。

### ADR-20 BOM 导入包：单批次目录形态与幂等（v3.0.2 新增）

统一bom_item是双域物料唯一事实源，标准包格式/列/唯一键/摘要算法只定义在PRD FR-2.1与附录A.5–A.8。一次目录/压缩包导入一个批次，七个实体CSV全声明且存在；零行是否合法由项目所需物料与引用完整性判断，不能硬编码“每类至少一行”。宽表只兼容服务器物料，按SN折叠并保留临时MAC身份接管规则，不能解锁网络前置。

两段式：只读预检→绑定会话/项目/manifest/文件摘要的确认令牌→重传原包→事务内重验并原子提交。预检不写业务库或创建包ID；预检报告随响应返回，提交后报告可持久化。BLOCK未清零不提交；WARN保留质量，不自动变PASS。model-svc提供独立型号期望，profile提供字段映射，catalog只提供参数，导入侧不另建各自副本。

全局身份由业务键决定，bom_line_no仅定位；同项目/批次/摘要幂等，同号异摘要拒绝，新seq按diff保存版本历史。摘要不含manifest自身但覆盖全部声明文件；确认令牌须额外绑定原始manifest，避免仅改manifest沿用令牌。解包拒绝路径穿越/符号链接/未声明文件/明文秘密，目录与压缩包相同内容得到相同摘要。

### 契约冻结约束

API/proto/RBAC/SSE schema在对应WP实现前冻结于api/，变更连同调用方、迁移和契约测试更新。架构示例不是已存在接口；未经实现验证不得作为运行证据。

## 4. 数据模型（核心实体）

以下为逻辑实体和关键字段，非逐表建库脚本；实际迁移/ent schema/API须保持一致。BOM字段/枚举以PRD附录A为准；网络设备投影不能成为第二份物料主数据。


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
FabricReadiness(id, project_id, scope_type[fabric|pod|switch], scope_ref, target_set_hash,
                config_push_state, lldp_readable, readiness_at, ttl_seconds, version_fingerprint,
                evidence_origin[sim|real], np_1, np_2, np_4, evidence_refs[], gaps[])
CableCheck(id, project_id, phase[network_precheck|g2_confirmed], port_pair_ref, bom_version,
           switch_lldp_ref nullable, host_lldp_ref nullable, precheck_status nullable,
           verdict[PASS|FAIL|BLOCKED|WAIVED], evidence_origin[sim|real],
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
ZtpSession(id, switch_id FK, boot_channel[239|143], dhcp_vendor_options[], identity_priority[sn|mgmt_mac|base_mac|other],
           observed_identity, conflict_state[none|sn_dup|mac_mismatch|binding_conflict], started_at, managed_at)
FabricRule(rule_id, domain[lossless|optics|log|precheck], name, scope jsonb, version, subject_unit,
           metric_key, window_seconds, comparator, hit_threshold, observe_threshold, sustained,
           severity, health_deduction_weight, degrade_to_unknown_when, safe_range,
           state[draft|released|disabled], parent_version nullable, change_reason, approved_by[], created_at)
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
      machine_id nullable, component_id nullable, physical_port_id nullable,
      switch_id nullable, switch_port_id nullable, cable_item_uid nullable, phase[delivery|post-delivery],
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
               device_model_version_refs[], blueprint_versions[], gate_policy_version, kb_bundle,
               server_baseline_ref, network_baseline_ref, role_plan_version, collect_policy_ref, status)
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



**数据归属与共用信封**：

| 事实 | 拥有者与不可混用边界 |
|---|---|
| 资产/期望/当前投影 | BOMItem保留导入版本链；型号期望由model-svc发布；Machine/Switch/PortModel为身份或当前投影；MappingRevision为G3确认结果 |
| 证据 | 所有采集/判定保留project、对象、观察/接收时间、来源、原始引用、解析/规则版本、质量与有效期；网络证据另区分evidence_origin=sim\|real和source=host\|switch |
| GateEvidence | 四值结论PASS/FAIL/BLOCKED/WAIVED；validity独立；目标集合、端口对覆盖、档位/版本/来源冻结，不能只挂项目通过率 |
| FabricReadiness | 包含目标范围、配置/LLDP状态、NP-1/2/4、缺口及版本指纹，按PRD gate.readiness_ttl过期；生产准入拒绝sim证据 |
| CableCheck | phase=network_precheck时保存pending/异常与可采证据，服务器端口对的门禁结论保持BLOCKED；phase=g2_confirmed时才按四值返回三方比对结论，PASS可供NP-3消费；不新增“预检通过”门禁值 |
| 网络配置 | NetworkIntent/NetworkSnapshot/NetworkDiff/PushJob绑定同一批准基线/计划/目标快照；变更计划、报告或目标即旧审批失效 |
| 规则/健康 | FabricRule遵循PRD FR-0.7完整结构；rule_id、单位、metric_key、unknown约束不可改；Finding固定rule_id@version；健康rollup取min，unknown限制结论，算法与权重引用PRD |
| 工单 | source保留首次发现模块，responsible_domain为定位归属；允许仅交换机/交换机端口/线缆，不强制存在Machine；必须至少锚定一个受授权稳定对象 |
| 模板 | ProjectProfile承载项目差异及服务器/网络基线；版本、型号、角色、介质/参数引用冻结；不得用条件分支硬编码不同项目 |

逻辑模型未展开的NetworkBaselineVersion、CredentialBatch/Approval和设备能力注册表，须在对应WP前补齐schema/约束/API；不得将“有一个JSON字段”当完整契约。密码/私钥/token只能在密钥系统，领域实体保存引用。

### 4.1 状态机（v2.0 扩展）

| 对象 | 状态与推进条件 |
|---|---|
| Machine | unknown→provisioning→provisioned→trust_established→collected(G1)→lldp(G2)→mapped(G3)→configured(G4)→netverified(G5)→tested(G6)→maintained；表示当前项目里程碑，准入查证据，不要求state恰等某一字符串 |
| Project | planning→deploying→verifying→delivered→archived；客户签署控制delivered，技术清理独立控制Machine.maintained |
| ssh_trust | uninitialized/established/broken/revoked；broken普通SSH拒绝，受限repair按ADR-06；撤销不自动恢复 |
| Issue | open→assigned→in_repair→verify→closed；验证失败/再次发现可reopened；所有流转为有权限的业务action+expected_version；close需要有效复测证据与NE/DL确认，连续两次verify失败升级DL |
| Alert | firing→suppressed→resolved；抑制不停止采样；stale/unknown不自动resolved；维护期带内规则停用、B7继续 |
| MediaItem | uploaded→verified→approved→retired；退役不可逆，对象及历史引用保留 |
| 网络模板 | draft→tested→released→deprecated；发布需仿真验证剧本通过，released内容不可变 |
| Switch.config_state | UNKNOWN、IN_SYNC、DRIFT_CRITICAL/MAJOR/MINOR；结构化diff/复采驱动；ZTP完成不直接写IN_SYNC |
| FabricRule | draft/released/disabled；revise与回滚创建新version，released不可就地改；审批、启停与unknown约束按FR-0.7，不物理删除 |
| LL结论 | normal/warning/critical/unknown；这是观测结论，不替代GateEvidence四值；缺能力/停用保持unknown并带降级标记 |

**Execution（本节为状态SSOT，PRD FR-1.6引用）**：`created→scheduled→dispatching→running→success|failed|killed|warning`，另有`pausing/paused`、`cancelling`、`reconciling`。TaskAttempt可能unknown；控制语义见§4.3，未确认终止不得显示killed。

**ZTP**：`powered→dhcp→booturl→identity→lookup→fetch→config→restart→managed`；config仅批准最小隔离/管理配置。未知身份进入isolated，冲突进入conflict且不提供脚本；失败按fabric.ztp_retry_max指数退避，超限failed，三段式错误留证。恢复重跑必须重验当前身份、offer和介质，不沿用失效批准；设备回报不替代可管理复验。

**PushJob（PRD FR-0.4.8为状态SSOT，本文仅列执行约束）**：

```text
CREATED → PRECHECKING ─失败→ BLOCKED ─修复→ PRECHECKING
                    └合格→ READY ─审批→ APPROVED → EXECUTING → VERIFYING → COMPLETED/PARTIAL
                                └驳回→ REJECTED
EXECUTING/VERIFYING异常 → ROLLING_BACK → ROLLED_BACK
COMPLETED/PARTIAL/ROLLED_BACK/REJECTED → ARCHIVED
```

共13态；Doublecheck在设备PRECHECKING之前，FAIL清零且WARN逐项确认才READY。审批固定目标/计划/报告摘要和不同签名账号；未批准不得EXECUTING。VERIFYING复采复比，失败按批准策略回滚并验证；PARTIAL重试或REJECTED修改只能创建关联新PushJob和新审批，不能复用原批准执行；单设备暂停/跳过需留事件，不篡改整批结论。sim/real分区，重试只处理允许范围，成功设备不重复执行。回滚效果未知时保留回执/锁进行对账，不能把ROLLING_BACK直接当ROLLED_BACK。

**阻塞与豁免**：blocked不是Machine新生命周期态，原因枚举`gate_pending/gate_failed/waiver_expired/trust_broken/readiness_gap`；按钮“ready”也非持久态。失败停留已完成里程碑，有效豁免只覆盖批准的缺口/对象/版本/时间；WAIVED不得改写PASS。

### 4.2 API 设计规范


规范：REST + OpenAPI 3.1，`/api/v1/*`；cursor 分页（**适用范围 v2.2 注记**：cursor 分页仅用于明细列表接口；聚合视图接口如 `/cmdb/machines/{machine_id}` 360°、`readiness` 为单资源/小结果集接口，不适用 cursor 分页）；SSE 实时流；统一错误体 `{code, message, details[], trace_id}`；变更操作 `Idempotency-Key`；契约文件 `api/openapi.yaml` 唯一事实源。本文表为端点节选，不表示接口已经实现；完整 schema、分页、权限、幂等、错误码及 SSE 事件需在对应 WP 前进入契约。项目请求显式携带 project_id；资源不属项目拒绝访问。状态/集合版本冲突返回 409，已删除或过期证据返回明确原因；无登录返回401；动作无权限403；资源不存在或无项目读取授权统一404（§4.6）。

**领域端点索引（设计节选，不代表已实现）**：

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
| POST | /api/v1/fabric/intents/render | 意图承接与渲染（pongo2 沙箱；禁止绕过基线手写单机配置） |
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


**认证与跨域端点索引**：

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


**动作与事件契约**：所有写动作返回更新后的state_version/资源引用，长任务返回Execution或PushJob ID；不能让前端直接PATCH任意状态。SSE至少包含event_id、type/schema_version、project_id、aggregate_id/version、observed_at及载荷引用，支持Last-Event-ID重放；已过期游标返回resync_required。事件名如PortStatusChanged/IssueStateChanged/GateVerdictChanged需进入版本化schema。网络readiness、预校验、规则dry-run及导出都必须携带来源、范围、版本和质量；空列表不能隐含健康PASS。

### 4.3 执行一致性、资源互斥与控制语义

资源锁权威存PostgreSQL，所有Agent/SSH/BMC/Kea/交换机写动作共用。按稳定资源ID排序申请；组任务原子预留全部成员。租约到期先查回执/进程/TaskService，未知进入reconciling并拒绝冲突任务。Agent验证fencing_token；BMC/普通SSH不天然支持fencing，须单写派发+回执对账，不作设备强保证。

| 类别 | 锁与冲突范围 |
|---|---|
| 只读采集 | machine/switch shared；独占写期间等待，仅允许明确的状态查询 |
| 配置/装机/固件/清理/电源 | machine/switch exclusive；跨通道互斥；可细分的资源需能力契约证明安全 |
| Kea配置 | 实例/生效配置范围exclusive，防止不同批次丢失更新；预期hash校验后staging/commit/回读 |
| RoCE/NCCL | group+GPU/NIC+测试窗口exclusive；整体预留并隔离影响结果的流量/采样 |
| UID | bmc.uid独立受限动作，仍受更新服务能力限制 |

- **Pause/Resume**：停止新派发，运行任务完成安全步骤或支持的检查点后paused；不默认SIGSTOP或暂停半个communicator。Resume重验权限、证据、锁后继续未派发任务。
- **Kill**：停止新派发，按能力取消进程组/远端任务；NCCL整组终止。必要回执全到才killed，不可中断刷写继续cancelling/reconciling。
- **重试**：只读/声明幂等任务可重试；写动作先查稳定command_id和真实状态，unknown不创建新副作用attempt；成功目标不重复执行。
- **恢复**：重建待派发队列、对账未完成回执/租约/动态监控窗口，覆盖DB已提交未发布、重复/丢ACK、远端完成回执丢失等切点。

### 4.4 门禁证据与原子准入

GateEvidence是范围/版本/新鲜度事实，GateRecord是评审记录，Machine.state为投影。证据须覆盖本次目标集合、端口对/路径、档位与通信组；A–B和C–D通过不能证明A–C。抽样必须由发布gate policy明确，G6交付范围须有测试覆盖计划。

提交带目标版本/证据指纹，服务端检查认证、项目/动作权限、生命周期、范围覆盖、来源与新鲜度、阻断工单/有效豁免、参数依赖和能力；事务内CAS相关版本、预留资源、冻结Execution快照并写outbox。实际派发前再验外部readiness有效期和锁，失败返回逐目标缺口。部分执行必须用户显式选择合格子集，不扩大批准范围。

BOM/型号/换件/拓扑/映射/角色/配置/规则变更按依赖图invalidate受影响证据；旧PASS保留历史但不得继续准入。unknown/stale/unsupported/unreachable均不能算通过。TTL引用PRD参数与版本化策略，动态readiness独立于静态资产TTL。豁免有具体缺口、范围、有效期、审批和补偿措施，过期/换版后重新阻断。

**网络依赖防环**：G2输入NP-1/2/4，三方确认后输出NP-3；N-S5 pending不能要求既有Mapping/G2，不能产出NP-3；G3只消费对应有效三方确认，G5/G6追加NP-5/6。仿真准入与生产准入显式隔离，禁止sim把真实断言置PASS。

### 4.5 NCCL GroupRun 与结果归属

NCCLTestRun保存覆盖计划；GroupRun冻结成员、GPU UUID/数量、global/local rank、world_size、接口/rail、算子、消息、迭代和工具参数版本。gang预留整组资源后rendezvous/barrier一次启动；屏障超时或任意rank失败使整次collective无效，取消其余成员并对账，重测创建整组新attempt。

coverage_mode仅`grouped_coverage`（全部目标分组覆盖）或`single_communicator`（全域同组），单机/跨机是test_profile维度。HGX节点与GB300机柜/NVLink domain分别建模，不硬编码8GPU。

algbw/busbw为collective/group测量（GB/s），保留nccl-tests口径与规模；不复制成各台独立busbw。节点掉队用有来源rank timing/DCGM/NIC/组对照分析。阈值来自已批准型号/拓扑/规模/算子/消息基线，无基线为待校准，硬件峰值不能当合格线。

### 4.6 登录、会话与授权

浏览器用同源随机不透明session的HttpOnly/Secure/SameSite=Lax cookie，服务端可即时撤销；不在localStorage保存token/密码。开发HTTP例外必须显式development且回环Origin。公共/login提供已配置本地/OIDC/LDAP，LDAP仅后端可信TLS访问。

本地密码Argon2id等验证哈希；首管理员由服务端交互CLI初始化，无默认密码/公共初始化API。账号+来源限流，统一失败消息，成功轮换session。所有修改含登录均验证会话CSRF与Origin；OIDC授权码+PKCE及state/nonce/issuer/audience/精确redirect校验。next仅同源相对路径并防编码绕过，现有阶段可用路径与尚未开放业务路由按docs/DEVELOPMENT.md记录，不能在设计中凭空宣称已开放。

超时/锁定引用PRD FR-10.1参数；过期401并关闭SSE、清私有缓存、回登录，保留允许的无秘密草稿；退出撤销服务端会话并清Cookie/订阅。SSE建立/每次重放检查项目权限，撤权主动关闭；URL不带token。

RBAC维护于api/rbac.yaml：服务器基础角色DE/DL/NE/SRE/Viewer/Admin，网络增加NDE/NH；角色为默认权限包，最终授权以权限点+项目资源归属为准。项目Admin不等平台Admin，Admin不自动绕过独立审批；assign/verify/close、trust、固件/电源、cleanup、waive、media批准、fabric执行/批准独立。账号/成员界面角色下拉存在基准冲突，按docs/DEVELOPMENT.md BASE-09补齐，不能删除NDE/NH需求。

未认证401；已知动作越权403；项目/资源读取不存在或无项目授权统一404，避免泄露；状态/目标版本冲突409，前端重新预览确认，不静默重放危险提交。

### 4.7 指标、历史与页面刷新

指标信封`{value,unit,source,observed_at,received_at,freshness,method,scope,quality}`，网络Gb/s与NCCL GB/s不可混用；缺测不补零。Agent在线、SSH可达、BMC电源状态分别计算，未知区间单列覆盖率，不能推断彼此。

快照含snapshot_version/cursor，活动页面/Tab按DS登记poll/SSE，隐藏时停无用订阅；事件去重/版本校验，过期游标resync后重取快照。采集delta有base_version，缺增量要求全量。断线保留最后数据/时点和stale提示，文字保持可读；筛选/排序/活动Tab入URL。

历史查询1h/24h/7d按DS约定时间桶，返回min/max/avg/count/coverage与源/质量，禁止跨单位/不同事实混合。通用服务器指标按DS至少7d原始/90d小时桶；**网络DDM/接口/无损时序必须按PRD NFR-10至少90d原始/18个月降采样**，不能被通用默认缩短。配置快照/网络基线归报告证据长期保存，日志/审计保留分别引用PRD。PG开发/小规模与ClickHouse容量档使用同一查询/质量契约。

未来24h计划来自持久化cron/时区/暂停/错过触发策略；事件/webhook只显示等待条件，不伪造未来时间。

### 4.8 AI 检索与建议边界

Runbook/NVIDIA文档/案例以Document/Chunk保存来源、批准版本与访问范围；默认PG+pgvector，扩展版本固定。无扩展开发档可标识词法/mock检索，不冒充语义验收；embedding模型/维数换版重建索引并保留旧引用。

先授权过滤再召回/重排，答案带来源、适用型号/版本；外部资料始终是数据，不能扩大取数或执行权限。AI只产出建议、Flow/ConfigSet草稿，发布/执行仍按权限/审批/介质/门禁。三Provider适配器覆盖流式/取消/错误/用量契约，测试召回、引用、错误建议/拒答、泄漏与恶意资料；mock不替代Provider/PoC实测，采纳率只来自真实反馈。

## 5. 关键流程时序（v2.0）

### 5.1 装机与互信链（扩展）

```text
BOM/型号/介质前置 → 渲染cloud-init（含域公钥）→ PXE/HTTP装机
→ 一次性回调核验身份/指纹 → 受限trust.bootstrap首连验证
→ established/TrustEstablished → 解锁预收集
失败 → 批准批次凭据修复 → 验证后established
```

### 5.2 验机全链（G1→G3，含拓扑验证与四步线序，v2.3 重排）

```text
预收集/G1 → PCIe实际与独立型号期望比对（差异T7处置重验）
→ G2 readiness：G1+拓扑+NP-1/NP-2/NP-4，不要求NP-3
→ Step1 GPU-NIC关系 → Step2 主机LLDP/本地MAC
→ Step3 FR-0.8交换机LLDP+BOM三方比对 → 逐端口对确认/NP-3或T1–T6工单
→ Step4 DL确认有效结果/豁免 → G2 → G3覆盖/冲突检查 → MappingRevision
```

Component/PhysicalPort身份先于G2，StepReport逐步版本化；正常流程FAIL阻断。issue.repair/verify按批准对象和范围可修复上游故障，仍检查身份/凭据/介质兼容/锁/维护白名单，不允许形成“须先通过G3才能修复G2”的循环。

### 5.3 连通性测试链（G5）

```text
冻结目标/档位 → 有效G4/范围/版本/工单/NP-4/NP-5/NP-6原子准入
→ 参数介质快照 → L1链路 → L2 ping → L3 RDMA定向 → L4全带宽路径 → L5无损验证
→ per-pair/路径证据+失败开单 → 报告 → G5 GateEvidence → netverified
```

正常测试与限定issue.verify分开授权；G5结果保留真实PASS/FAIL/BLOCKED/WAIVED，不把豁免记PASS。

### 5.4 NCCL 测试链（G6，v2.0 细化）

```text
当前G5覆盖/档位与NP-5/6 → S1预检 → S2介质/环境
→ S3单机组 → S4跨机组 → S5计划覆盖 → S6组结果 → S7报告
→ S8端口/G5/DCGM/工单溯源 → S9调优清单 → DL G6评审
→ ConfigSet修改/应用 → 定向重测 → 案例沉淀
```

GroupRun规则见§4.5；tested可重测，maintained拒绝。纯机内NVLink诊断可按PRD在G4后单独跑，不产出G6 PASS。

### 5.5 故障工单闭环链（v2.0 新增）

工单基础依稳定物理身份和权限即可使用，必须先于N-S5/G2/T7首次失败交付；不依赖G3 Mapping或网口监控，后续portmon只挂接既有服务，避免故障修复依赖尚未通过的门禁。

fabric/G2/G5/G6/portmon/topology/BMC发现→issue-svc保留首次source、稳定对象与原始/版本证据→分派/处置→类别Runbook定向重验→有效PASS与NE/DL确认关闭；失败两次升级DL。类别/重验组合引用PRD FR-4.7，fabric侧映射引用FR-0.5.3，不另创T8。

### 5.6 优雅退出链（扩展）

```text
有效G6 → 冻结范围/源水位，拒绝新带内作业并drain
→ 所有权清单 → 原始日志/证据/最后快照上传 → pre_cleanup ACK
→ Agentless精确清理平台对象（保留撤销通道）
→ 残留扫描+报告+CMDB清单 → residue ACK → DL确认范围/报告
→ 最后带内动作：撤平台SSH公钥/临时凭据 → 撤销回执/新连接拒绝/关闭原会话
→ 最终技术报告 → final_technical ACK → maintained/B7白名单
另行：DL最终报告确认 → 客户PDF签署 → 签署件归档 → Project.delivered评审
```

CleanRun持久化每步、幂等键、对象摘要/回执；三个ACK不能互代。OwnershipRecord逐项核对canonical path、符号链接、对象身份和创建凭据，不清空共享目录、客户账户或整份authorized_keys；目标已被替换/不属平台则停止该项报告。

pre_cleanup ACK缺失不删除，residue ACK缺失不撤信任。撤销前从已确认步骤恢复；撤销后未知进reconciling，核对控制面记录、新连接拒绝/BMC，不自动重建SSH。必要的一次性修复须批准，完成后重撤并补证。技术证据齐全经outbox推进maintained，客户未签不恢复带内；签署独立阻断项目delivered，自动化不代签。

## 6. Agent 设计（keel-agent）

```text
/opt/keel-agent/keel-agent          # Go二进制
/opt/keel-agent/keel-agent.conf     # 控制面/TLS/注册凭据
/etc/systemd/system/keel-agent.service
/var/lib/keel-agent/               # 任务/回执/幂等记录
/var/log/keel/                     # 目标临时日志
/opt/keel-cache/media/             # SHA256缓存
/opt/keel-cache/nccl/              # 测试目录
```

启动使用短期一次性enrollment token核验设备/项目再签mTLS，重放/错项目拒绝；控制面管理CA轮换/吊销。执行先验证command_id/token、持久化接收回执，再fork受限进程组，流式输出并持久化终态；重连不是重执行。升级通过已批准media://agent版本并验证回滚能力。卸载由Agentless停止服务/删平台对象/吊销Agent证书，再按§5.6撤SSH；两个通道分别验证。

### 6.1 部署拓扑

### 现场单机形态（默认）

```text
管理网 → 反代/静态前端 + keel-api + PostgreSQL + Redis + MinIO
          ├─ 介质/PXE HTTP + 服务器ProxyDHCP
          ├─ Kea DHCP + boot-svc 239/143受控设备入口
          └─ 采集/Runner → 服务器、交换机、BMC
```

同一进程可承载API与独立设备listener，但认证边界/证书/管理网入口独立；设备无浏览器session不能因此开放普通管理API。Kea、引导服务、采集器、仓库环境自检全绿后才N-S4。实际开发端口/镜像/迁移查HANDOFF/DEPLOYMENT，不在目标拓扑写死开发服务器地址。

| 形态 | 承诺与验证 |
|---|---|
| single-node（默认） | Compose单节点，进程级自恢复，无HA；磁盘/节点故障需恢复流程 |
| ha-cluster（可选） | 多API+PG主备+Redis哨兵+MinIO纠删码；仅此形态适用NFR-4 RTO；仲裁/存储/切换拓扑实现前明确并实测，不能用“两台API”证明全栈HA |

部署配置选择KEEL_DEPLOY_TOPO，不改变领域契约。引导服务目标多副本无单点，single-node不能据此承诺基础设施HA。

### 性能预算（v2.2 重校准）

本文保留工程量纲，不复制PRD阈值表；验收目标以PRD NFR与ACCEPTANCE性能项为准，报告必须记录档位、环境和计时边界。

| 环节 | 容量/实现约束 |
|---|---|
| 控制面调度 | 原有工程预算：10k目标索引查询<2s、ExecutionTarget批量生成<5s、状态矩阵聚合刷新<1s；仍需在标准容量档实测 |
| 扇出10k×30s | 默认并发1000纯运行下界已300s；满足含调度端到端目标须更高验收并发、认证/回执/队列预算与实测；SSH池须容纳档位，不能算术宣布SLO通过 |
| 介质冷/热缓存 | 500MB×10k=5TB，15min在90%效率下约49.4Gbps，现场低带宽须分档或分层缓存；缓存95%命中不能宣称冷启动成本消失 |
| 监控ingest | 10k×50KB/s≈4Gbps、全动态200KB/s≈16Gbps；总预算1Gbps须全局令牌桶、优先级/活跃窗口、delta/合并降采样；状态/告警/最终回执不按遥测丢弃策略处理 |
| 资产采集 | 原有预算：10k BMC全量Redfish采集在16并发/约8s每台假设下<2h；按实际设备请求数和尾延迟验证，不能替代维护期轮询新鲜度 |
| BMC轮询 | 设备数×每台请求数×耗时/周期决定并发，见ADR-18；逾期stale，保留失败/重试/尾延迟 |
| 身份/端口 | 80k服务器口+5000×256交换机子口至少1.36M端口；索引item_uid、(parent_uid,port_index)、端点引用，测导入/解析/分页而非只测对象计数 |
| 列表/矩阵 | 万级逻辑行双轴虚拟化、服务端聚合/cursor、只渲染可见单元格；不为每行创建ECharts；固定浏览器/数据集/机器后测帧率 |
| 查询/配置/线缆/规则 | 按NFR-14～NFR-20及FR-0批次规模测360°/统计、模板吞吐、10万端口对比、DDM P95、规则热加载/dry-run；不能以小样本功能测试替代容量验收 |

开发负载发生器可模拟10k身份，但报告须分别记录逻辑身份、真实连接、硬件、资源、体积、故障seed与冷/热缓存。控制面规模不等于RDMA/NVLink/CUDA/Arm64真机能力，硬件由H-1～H-18 PoC留证。介质/日志/时序保留、备份和磁盘水位分别估算；恢复演练验证DB/对象/密钥备份一致性。

## 7. 安全设计（v2.5）

| 边界 | 约束 |
|---|---|
| 传输/身份 | 同源HTTPS、Agent mTLS、可信Redfish TLS、可信SSH host-key；设备239/143边界按ADR-19与FR-0.3 |
| 凭据 | Vault/加密控制面存储、按用途最小权、领域表只存引用；控制面域私钥不落目标；轮换/撤销留回执 |
| 权限/审批 | 六服务器基础角色+NDE/NH，按动作和项目授权；高危确认不替代后端；执行与审批分离、Admin不绕过批准 |
| 原始输入 | 包/模板/脚本/设备输出/日志受界限校验；路径/符号链接/命令参数安全，网络模板沙箱；AI资料不赋权 |
| 审计 | 应用角色AuditLog只INSERT/SELECT，脱敏日志/介质/工单/互信/权限/认证全留痕；DB管理员仍可能改结构，摘要/对象版本与保留锁/异地备份提供校验，不宣称绝对防篡改 |
| Webhook | 入站校验绑定项目/来源、原始报文HMAC、时间窗与事件去重；出站目标白名单+签名（默认开），拒绝重放/错项目 |
| 浏览器 | HttpOnly session/CSRF、退出清私有缓存与SSE、敏感字段按权限；不在URL/浏览器存储留秘密 |

规则dry-run计算必须零持久化、零告警/事件/健康副作用；与PRD通用“所有API审计”有上游张力，调用审计边界在docs/DEVELOPMENT.md BASE-08保留待决，不能自行设例外或据此给验收PASS。

## 8. 可观测性（v2.0 扩充）

平台Metrics覆盖execution/调度/队列/通道错误与回执对账、Gate缺口、介质吞吐/缓存、采集新鲜度/未知覆盖率、工单闭环、测试分层、ZTP/PushJob/规则/健康。指标带单位/来源/版本/质量，不将unknown纳入正常比例。OTel trace_id贯穿API/执行/产物。system flows自诊断组件、积压、磁盘/介质桶、凭据/互信异常；业务证据与平台遥测保留策略分开。

## 9. 前后端协作约束（vibecoding 契约，v2.0 扩充）

1. **契约**：OpenAPI/proto/RBAC/SSE schema同源，类型生成；每页面先有真实查询/操作/错误/权限/事件契约，设计端点存在不等于已实现。
2. **工作台先行**：业务页面复用DS顶栏/侧栏/双平面、令牌/组件/数据状态，不以临时横向导航替代；完整布局符合性需浏览器证据。
3. **集合与动作**：服务端筛选/排序/cursor/聚合；跨页全选冻结selection_token/目标ID摘要及排除数，行内只作用本行，批量条发起批量；后端重验范围/权限/版本。
4. **状态可恢复**：错误体含trace_id，401/403/409/429等按DS处理；活动Tab按DS协议拉快照+增量；SSE断线保留旧值/时间与stale，不清空/假成功；筛选/Tab入URL。
5. **参数与就绪**：业务版本用ParamSelect选批准目录并验依赖；登记页可输入新版本元数据。ReadinessGate展示本次动作逐目标适用断言/缺口，提交后端重验；不能用一个全局ready替代G2/G5/PushJob不同前置。
6. **环境**：UI接真实业务API/授权/存储；契约mock仅用于隔离联调，硬件适配器默认mock且与real共契约，endpoint/CA/凭据配置化；外部输入仿真须标识，不呈现虚构生产健康。
7. **编辑**：YAML/拓扑/No-code共享版本化AST；无法表达节点保留原文并只读/回YAML，不静默丢字段；保存/发布校验schema、引用/依赖/危险动作，回滚建新FlowVersion。
8. **权限/i18n**：按钮/拖拽消费后端permissions/allowed_transitions，后端为准；所有展示文案用层级i18n键，来源原始日志可保留原文；确认框冻结项目/对象/版本。

## 10. 假设与依赖登记（v2.0 更新）

| ID | 实现前需确认的输入/决策 | 处理与验证落点 |
|---|---|---|
| A-1 | 第三方网络平台能力 | 内置FR-0.8不依赖外部控制器；第三方需同契约适配，缺能力unknown，P2契约/PoC |
| A-2 | BOM/项目模板/商定基线 | 按PRD附录A交付，字段映射/必需物料清单/凭据引用有发布版本；P1导入验证 |
| A-3 | BMC实现差异 | DMTF版本不等于每项可用；逐型号适配/能力/超时与恢复，P2/维护期PoC |
| A-4 | amd64/arm64与硬件组合 | HGX B300/GB300分别锁OS/内核/cloud-init/Agent/工具，未实测架构不宣称支持 |
| A-5 | CUDA/驱动/NCCL兼容 | 准确补丁组合及工具能力由批准兼容矩阵+P3 PoC确认，不把候选版本写死业务逻辑 |
| A-6 | 管理网与分发出口 | SSH/引导/证书/网段/出口带宽按现场清单，按§6.1容量档实测；分层分发方案未落地前不承诺低带宽万卡SLO |
| A-7 | IPAM/地址规划 | 输入BOM/已批准地址规则或可选IPAM适配；平台按规则分配，不设计网络方案 |
| A-8 | DOM/设备计数能力 | 缺测显式unknown/降级，host与switch保留来源；P2/P3三种降级PoC |
| A-9 | PFC/ECN/WJH/队列口径 | NOS适配器+指标注册表+golden样本；单位/重置/时间窗/无数据语义先冻结 |
| A-10 | NOS/网络型号范围 | Cumulus基线与其他NOS分别适配，真实命令/计数/恢复先PoC；不能只改配置就宣称支持 |
| A-11 | 客户/NE批准输入 | 外部方案source_ref/签署证据的验真范围明确；工程限值子集不代表完整模板或生产变更批准 |

**尚须落地的工程决策**：FR-0.8的NP逐项响应和证据来源schema、完整NetworkBaseline/Approval/CredentialBatch模型、Kea与ProxyDHCP广播域/持久化配置所有权、多副本引导路由与证书吊销、HA仲裁/存储拓扑、全量规模容量档、规则dry-run审计边界。相关WP开始实现前提交具体契约/迁移/测试决策，不以空字段或“后续支持”宣称完成；上游冲突统一查docs/DEVELOPMENT.md §2。

## 11. 模块-服务映射（架构 ⇄ PRD 对照，按 FR 序排列）

| PRD | 唯一写入/判定职责 | 主要实体与消费边界 |
|---|---|---|
| FR-0 | fabric-svc：规划/开通/预校验/Doublecheck/diff/PushJob编排；fabricmon-svc：交换机采集/无损/健康/日志；netbaseline-svc：基线/模板；boot-svc：Kea/设备引导/身份 | Switch/Port、NetworkIntent/Snapshot/Diff、PushJob、ZtpSession、FabricRule/Finding/HealthScore；复用执行、型号、介质、参数 |
| FR-0.8 | fabric-svc证据门面+第三方适配器 | FabricReadiness/CableCheck/计数/原始引用/导出；服务器模块不得绕过门面直读设备或另作网络健康判定 |
| FR-2.1 | BOM导入模块（fabric-svc承载） | BOMImportPackage/ManifestFile/Issue/Item；model-svc给期望，profile-svc给映射，双域原子写入 |
| FR-1 | flow-svc/execution-svc+Runner | FlowVersion/Execution/TaskAttempt/Receipt/Lease/outbox；Kestra仅参考 |
| FR-1.8/1.9 | media-svc/catalog-svc | MediaItem/DistLog、ParamKey/Snapshot；版本/依赖/阈值单一源 |
| FR-2 | provision-svc/render-svc/trust | 装机、cloud-init、Machine/TrustLog；网络ZTP不在服务器PXE服务内混写 |
| FR-3 | redfish-svc | CollectRecord原始/结构化；独立能力适配 |
| FR-4 | validation-svc/mapping-svc/issue-svc；portmon-svc只读投影 | GateEvidence/Record、MappingRevision、PhysicalPort/PortModel/PortEvent、StepReport/TopologyDiff |
| FR-5 | config-svc/render-svc | ConfigSet/Apply/RoleBinding；服务器配置与网络pongo2分开 |
| FR-6 | nccl-svc/report-svc | NCCLTestRun/GroupRun/NCCLIssueLink；组级结果不冒充单机测量 |
| FR-7 | flow-svc | 编辑器、蓝图、版本AST/触发器；执行复用FR-1 |
| FR-8 | ai-svc | AIProvider/Invoc、KnowledgeDocument/Chunk；只建议不执行 |
| FR-9 | cleanup-svc | OwnershipRecord/CleanRun；分阶段ACK/撤销/技术完成 |
| FR-10 | API身份/项目授权、notify-svc/audit-svc | User/Session/ProjectMember/AuditLog；高危动作独立授权 |
| FR-11 | nettest-svc/report-svc | NetTestRun/PairResult；网络计数经FR-0.8 |
| FR-12 | profile-svc | ProjectProfile不可变版本，项目差异/基线/角色/参数引用唯一承载 |
| FR-13 | mon-svc | CollectPolicy/MetricSample/Alert；portmon/Dashboard只读聚合，维护B7 |
| FR-14 | model-svc | DeviceModel/Version/Slot；服务器/网络型号同库，拥有独立BOM期望 |
| FR-15 | cmdb-svc | Ref/Snapshot/ManualField；联邦聚合、归档、AIOps导出 |
| FR-16 | redfish-svc + mon-svc维护轮询 | BiosConfigSet/Apply、FirmwareRun；能力/任务回执/维护白名单 |
| FR-17 | 项目授权边界+profile-svc | Project/ProjectAsset/Member、双平面上下文、生命周期 |

横向notify/audit/report不拥有业务判据。服务名是包边界；catalog不负责型号期望，CMDB不负责改源资产，前端不负责DDM/无损判定。

## 12. 端到端依赖总图

```text
ProjectProfile + DeviceModelVersion + Media/Param批准版本
  → 统一BOM导入（服务器+网络）
  → FR-0第0步：N-S2规划 → N-S3就绪 → N-S4最小ZTP
     → N-S5网络预校验（主机口pending）→ N-S6 Golden/PushJob → N-S7健康/无损
服务器可并行装机/G1/拓扑，但：
  G2启动 ← NP-1 + NP-2（ZTP+Golden）+ NP-4新鲜readiness
  G2三方确认 → NP-3 → G3映射 → G4基线配置
  G5连通 / G6 NCCL ← 对应范围前序GateEvidence + NP-5/NP-6
  → 清理/分阶段归档ACK/撤销 → maintained；客户签署另控Project.delivered
全程：执行/资源锁/回执 + 源版本/证据 + 工单修复重验 + 审计
```

**依赖不变式（架构级断言，CI校验，[I-1]–[I-12]）**：

1. **[I-1] SSH准入**：普通任务established；仅受限trust.bootstrap/repair可用批准一次性凭据，例外不能扩为任意脚本或maintained自动恢复。
2. **[I-2] 介质引用**：分发/安装/刷写引用approved media://与摘要；查询/等待/KV不虚构介质；发布/执行验引用，草稿可未就绪。
3. **[I-3] NCCL准入**：目标/通信组所需当前有效G5范围/路径/档位匹配，tested可重测、maintained拒绝，不能只比Machine.state。
4. **[I-4] 工单修复与复测**：正常测试拒绝关联未处置阻断故障；issue.repair/verify限定对象/权限/范围，不豁免身份/凭据/介质/锁/维护白名单，完成须重验。
5. **[I-5] 快照完整**：Execution冻结project、目标、FlowVersion、ParamSnapshot、介质摘要和GateEvidence指纹；空参数仍保存快照，不执行可变草稿。
6. **[I-6] 独立期望与身份**：G1前发布型号绑定；Component/PhysicalPort先于G2；G3发布MappingRevision；BDF/实测不能自造独立期望。
7. **[I-7] 角色同源**：unused/required角色在BOM、渲染、采集/告警同源；角色/型号换版invalidate相关证据。
8. **[I-8] 维护白名单**：停带内；仅BMC只读/UID与独立审批电源等明确动作，其他变更走维护流程，不借Flow/监控绕过。
9. **[I-9] 有限豁免**：只覆盖批准缺口/对象/版本/时间，Waiver Register留存；过期/换版阻断，GA前清零或签署明确接受，unknown/skipped不变PASS。
10. **[I-10] 项目隔离**：请求/缓存/SSE/草稿/确认框显式项目与实际资源归属一致；双入口只导航当前标签，运维仅聚合获授权项目。
11. **[I-11] 网络证据不冒充PASS**：NP与LL缺数据/停规则/能力不支持均unknown并按门禁BLOCKED+降级；sim不冒充real；N-S5不产NP-3，N-S4不单独产NP-2。
12. **[I-12] 导入包单批一致**：标准包七实体CSV存在，单事务双域引用完整；身份按业务键，同批同摘要幂等/异摘要拒绝；BLOCK不提交/不启动N-S4或PXE，映射来自项目模板。

**验证节奏**：同一完整能力先运行关键决策unit/协议连通，再做一次真实持久化→API→UI功能集成；仅在新增改动、失败或明确未解决风险时定向重跑。PR/阶段执行规定全套门禁，性能和硬件指标按既定验收时点留证。无硬件不反复重跑模拟全套，也不删掉业务逻辑、安全拒绝和恢复覆盖；详见实施计划的验证方法表。

验证映射到ACCEPTANCE的契约、状态机/故障注入、浏览器和H组PoC：重复/丢ACK、未知副作用、过期/换件失效、跨通道争锁、组任务失败、上游修复、撤销恢复、跨标签/项目、ZTP隔离/冲突、PushJob13态/灰度/回滚、规则零副作用/三种unknown降级、BOM坏引用/路径穿越。报告分别注明设计检查、sim、真实传输和真机范围；本文检查通过仅证明文档契约完整。
