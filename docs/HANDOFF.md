# 当前开发状态

更新：2026-10-27；当前集中测试结果见 [P1验证记录](P1-VALIDATION-2026-10-27.md) 与§160～162，性能待复验项见 [P1-001](issues/P1-001-test-host-resource-timeouts.md)。用户发布后决定P1带遗留项收口、允许进入P2（§164）；main已合并并推送至`1a96897`，当前P2分支`dev/p2-validation`接续§197；3000现为`7afda32`、数据库64条迁移（最后067），发布证据见§163。后续编号章节保留历史快照；操作方法见 [DEPLOYMENT](DEPLOYMENT.md)，开发契约见 [DEVELOPMENT](DEVELOPMENT.md)。

## 当前恢复入口（2026-10-27，集中测试检查点）

当前P1逻辑设备扇出按200台。本轮执行容量200/200成功（8.235秒，sim），完整12组控制/恢复/权限检查通过；200逻辑ZTP仍因整包超时待复验。原2,000台后续优化，成功率/介质完整性/权限/恢复要求保留。

### 低Token接续入口（2026-10-27，接续§197）

- §197接续4504314：注册命令内部Run/Query消费者、持久主机启动意图/独立停止归档、未知时只查询原采集器并合成历史证据源码；088待应用，源85条/运行64条。尚未接共享Execution创建/worker路由、真实特权helper、已完成runner回执消费或Step2～4发布。新用例NOT_RUN，窗口继续至11:05:21 UTC。

- §186为本轮收尾：预收集范围显式collector_version、NIC驱动版本完整率、工具正文unsupported语义及原停止证据回收质量补强。WP-2.2提交1b30813，WP-2.3提交99ea4bb；两小时窗口于08:35:30 UTC结束，下一步先明确型号级管理口/RDMA/DOM适用性，再按WP-2.12推进拓扑/T7/Step1。源码78条迁移，运行仍64/067、3000未更新；全部新测试NOT_RUN。

- 最新§185在99ea4bb预收集/G1之后补WP-2.2资产自定义视图：个人/团队版本、Redfish+NOS统一投影、目录条件着色、全范围排序分组和CSV/模板导出；新迁移081待应用，源78条（排除原026）。全部新用例NOT_RUN，运行64/最后067、3000仍7afda32；下一开发入口按WP-2.12拓扑与T7/G2 Step1推进，WP-2.8统一豁免仍待接入，P2未闭环。

- 最新源码见§184：WP-2.1整批凭据/库存图接续后，WP-2.3只读预收集、原始分块回收、独立G1与当前有效性、资产页已接线。源77条迁移（排除原026），068～080待应用；新软件/硬件用例全NOT_RUN，运行仍64/最后067、3000为7afda32。下一块WP-2.2资产自定义视图/导出，再按依赖做WP-2.12拓扑；WP-2.8豁免账本待实现。保持单Agent与里程碑集中测试。

- P1已快进合并至本地与GitHub main（`1a968979d3775d754cfd905c21ed22f2f714c85e`），无冲突、无强推；当前唯一工作区/分支为`/opt/keel` / `dev/p2-validation`。P2已开始WP-2.7工单基础源码，见§165；用户再次强调在重大里程碑集中测试，本轮只做格式化、API类型与路由生成。
- 接续`8bcbf61`，WP-2.7基础、网络线缆自动开单/观察历史（§165～166）已有源码，本批加入端口诊断持久证据及T2-rail/T3/T4/T6异常联动（§167）。源新增068～070（67条迁移，排除原026草稿），运行仍64/最后067；P2源码未迁移或发布，3000保持`7afda32`。WP-2.7/2.14保持partial；新增重验范围预览及mTLS定向复采客户端契约（§168）；下一步将其接共享Execution准入/双端资源锁/结果消费，再做有效关闭/失败升级与通知，缓存读取不能冒充设备复采；不把源码或生成成功写成整项验收通过。

- 最新源码见§169～170：已接T1/T2交换机互联定向复采Execution、双端独占锁、不可变证据/判定、有效PASS确认关闭及连续失败升级。真实执行默认关闭；新用例仅编写未运行。源迁移068～072待应用（共69条，排除026）；3000与运行库保持原版本。unknown已补原命令只查询/停止证明/显式对账源码；读取/证据缺口已接续§171；下一步步骤E的模板生命周期与影响分析。

- 最新步骤E源码见§172～173：模板新版本回滚、升级影响与持久待复核/修复队列；源迁移073～074待应用，继续规划/变量Schema缺口。最新工作未测试或发布；变量Schema/目录引用接续§177。

- §180已补WP-2.1 UTC cron/业务事件/有限重试与策略UI，继续复用共享Execution；BOM/凭据/装机事务生产事件，原unknown不自动重试。新迁移078待应用，源共75条（排除原026）；068～078尚未迁移/发布。下一入口为WP-2.1 IPMI回退及凭据批次全量复验，再按WP-2.3接预收集/G1；FR-3/WP-2.1仍partial，新用例NOT_RUN，不把策略/接口存在视为整项完成。

- §181新增受限IPMI兼容回退与fixture内容固定；HTTPS认证/证书验证失败不降级，设备缺失字段仍unknown，BMC通道/停止证明/原始证据沿用共享执行。镜像源码增加固定ipmitool包，未构建或改变运行服务；源迁移仍75条，068～078待应用。下一块凭据批次全量复验，然后WP-2.3预收集/G1。

- 最新阶段决定：用户明确允许P1带遗留项收口并进入P2。200逻辑ZTP整批由用户后续安排；Agent/SSH超时按性能待办P1-001保留，等用户指令再处理，不阻挡P2启动，不把原始失败/未完成改为PASS。以下P1收口主线与“未签署”描述为此前验证状态，以本条和§164为当前排期依据。
- 当前最新实现见§175～176：步骤E补扩展地址池/索引规则、微调独立审核及模板/下发冻结，再接模板DeviceType×Role×配置域及整批适用校验。§177已补网络配置/脚本/事件只读链与最新Golden证据选择；WP-2.1 Redfish基础见§178，共享手动执行/加密凭据/资产页见§179，下一块接cron/事件与回退；步骤D/E新增验证留到重大里程碑集中执行。WP-2.1 Redfish具备独立前置，可按实际任务顺序推进；保持单Agent，不因可独立实施而启动子Agent。

- 工作区`/opt/keel`、分支`dev/p1-network-bom`，单Agent。最新生产代码修复检查点`bf265ec`；后续验收脚本/诊断/文档提交见git log。先读[P1验证记录](P1-VALIDATION-2026-10-27.md)和P1-001恢复本轮结果；已有功能历史见§137～159，不重复实现。
- 用户2026-10-27已授权集中测试，通过后更新；此前等待验证限制已解除。确定性修复及多数软件批次已通过，性能可能影响的失败登记P1-001、不反复追跑，P1尚未签署。先读上方短验证记录，不重做已通过且未受修改影响的检查。无tmux、SSH绕转、子Agent、工作树或推送。
- P1仍partial：源码实现、软件验收和真实PoC分开记录。当前逻辑扇出200；批准脚本/制品可组合为主机DAG，未知副作用不自动重放。重装历史保持不可变，RMA需独立批准并按sim/device隔离退役旧绑定；不迁移凭据或覆盖旧物理身份。
- 下一主线按P1实际WP和F-0.5/F-1退出判据收口软件缺口与集中验收材料；136已接项目模拟，不开放真实136设备会话或监听路由。真实PoC由用户提供，后续G3+、完整IPv6规划和P2～P4能力不强拉进P1。
- 源/运行均64迁移（排除026原草稿），最后067；用户要求先发布到3000后，已部署`7afda320ecebbf9670c61103239e5f9872b2ed49`，容器健康及浏览器登录/项目/型号/退出撤权通过。`.env`更新镜像引用；其他项目服务未动，无GitHub推送。原`internal/platform/fabric_precheck.go`与`migrations/026_fabric_precheck.sql`保留，不提交或执行。备份及回滚边界见§163。


历史安排（2026-10-26）：合并四Agent工作后恢复单Agent；保持本地开发、无tmux/SSH绕转，PoC由用户提供。当时等待集中验证的限制已被2026-10-27授权替代，当前按AGENTS执行。

源码接续39c353f，本轮统一集成见§108：机柜显式批量准备、七阶段事实投影/恢复页面、SSH已查询终态失败后的独立只读probe，以及重启失败不释放真实磁盘claim。前序目标UKI/写盘/首启链见§105～107。WP-1.7/P1仍partial，不把源码合并或编译通过记为装机/SSH/PoC验收。单Agent已补首装SSH allowlist无旧host pin的显式配置源码（§109）；已接`boot.file_served`真实完整HTTP响应事件生产者（§110）；已补注册后崩溃显式对账、无写许可隔离任务收尾与孤立SSH派发修复项（§111～113），并增加装机互信待修复筛选（§114）。TFTP实际发送/ACK门槛、普通SSH配置状态与显式批量只读核验见§115～117；本轮接OS/model金丝雀放量门禁（§118）。首启OS/驱动包历史基线记录见§119；单机受限公钥恢复及独立审批见§120，显式批量恢复/审批/撤销/查询入口见§121；镜像选型联动驱动依赖/矩阵出处见§122，恢复终态失败分类见§123；WP-1.17显式厂商60/43契约及DORA证据见§124；WP-1.5批准小脚本SSH执行源码见§125、Agent复用链见§126、结构化结果见§127，登记入口贯通修正见§128；Agent网口增量观测见§129；固定脚本缓存/分页清单见§130；显式缓存命中免传正文见§131；Agent归属索引心跳清单见§132；过期脚本派发显式未启动证明见§133；同快照执行事实报告导出见§134；脚本回执事实展示/提示修正见§135；集中验收等用户安排。3000最后健康检查仍返回6668a72c，源56迁移（不含026草稿）、运行46；未应用SQL、构建/部署镜像或推送。两份原fabric草稿保留且不纳入本次提交。

- **累计未发布源码（§75～92）**：此前的续签/主密钥重包裹/审计/Webhook等增量继续保留；本次从e0a9467接续，单Agent完成持久装机准备/独立审批/共享独占Execution与页面、隔离ProxyDHCP/HTTP/TFTP传输、Linux只读硬件预检与双架构helper、固定固件/helper启动配方与iPXE/清单生成和消费校验，以及无最终镜像前置循环的离线helper overlay生产/反向校验。修正Linux槽位ABI、NVMe实际health输出、传输中失效和准备完成时的当前准入边界。新增用例与集中/CI入口已写入但未运行。必要Go/TS编译、静态检查和指定双架构构建通过，不等于功能/SQL/镜像/硬件验证。源迁移50、运行46；3000仍§74。原草稿保留。

- **Agent网页只读链路已发布（§74）**：既有mTLS任务通过认证API/资产页启动并进入原执行五Tab，unknown由当前操作员凭worker查询的真实回执显式对账；实际HTTP/Agent/浏览器和完整CI通过。生产HTTP任务准入、listener及worker继续关闭。

- **用户UI要求已落实（2026-10-25，§73）**：既有旧页面原生业务控件全部接入共享shadcn/ui，预算清零；全局采用本地JetBrains Mono v2.304四字重，包含Monaco/DDM及中文回退，正式DS/令牌/授权同步。登录字段保持16px（含移动端），当前3000字体与浏览器已实测；不把控件迁移等同完整UX/无障碍或P1验收。

- 唯一工作区`/opt/keel`，分支`dev/p1-network-bom`。3000运行`6668a72c1adaeb2444b8c28a3f4db4f038db49dd`，镜像`sha256:c6fbfd82912588f0f428a18440761ac6146a9c8b4e7db8de83c48e5adf79bdae`，048/049已应用，运行库46条迁移；候选/运行版健康及真实登录、八页工作台、公钥列表/受限角色API及Agent默认关闭复验见§74，字体摘要/16px登录字段历史实测见§73。旧schema44及更早镜像不可直接回切，备份恢复另走批准流程。
- A3/WP-1.5新增独立批准的受控非root公钥安装，强制helper条目追加保持客户内容，新域钥全新SSH握手查询原回执才写真实互信。unknown保持资源隔离、恢复不重装；安装前或证明后撤销均阻断信任。实际PG/SSH/双人浏览器与全量检查通过，详见§64。
- §71新增Agent一次性注册/mTLS/连接心跳及双架构CLI，实际PG/TLS/gRPC/CLI/受限角色与DB超时恢复通过；身份基础后已接续§72的有界只读任务/统一执行/持久回执及断线query恢复，初始入口是认证管理CLI；遥测/自动安装及完整任务目录仍未完成；续签源码接续见§75，待集中验证。Agent功能提交f02858c已推送，CI/run40188674535成功。
- 生产Agent listener、worker、HTTP任务准入、真实SSH、主密钥提供者、外部介质获取仍关闭；签名只读挂载保持。无生产域钥生成或真机操作，两份原fabric草稿未跟踪且未构建。
- 此前开发批次的功能与修正均已提交推送，最终有效CI通过。WP-1.14 OIDC/LDAP已提交发布（§65/66），生产仍关闭；SSH安装后的普通任务域钥绑定已提交发布（§67），资产页普通只读SSH启动入口已提交发布（§68），后续只读repair域钥绑定安全回归及测试顺序修复已完成（§69）；H-1旧执行包的认证/指纹/缺证据假成功路径已改为离线证据校验（§70）；root首登、PXE、完整repair/旧钥删除轮换/目标撤销、主密钥轮换、Vault销毁、完整Agent任务目录/自动安装/续签与真机PoC仍未闭环。相关WP/P1/FR-0仍partial。

## 本次收工与下次恢复（2026-10-25 14:41 UTC）

用户要求今天停止开发。没有继续运行的开发子代理；3000及数据服务保留运行，不停服务、不清库、不清卷。收工复核3000 healthy，/healthz为6668a72c1adaeb2444b8c28a3f4db4f038db49dd。收工前HEAD为8a930a2（交接文档），功能提交为6668a72，已推送；功能提交的CI与发布证据见§74。除以下两份原草稿外没有未提交业务改动。

下次按以下顺序恢复，不能从历史章节的旧“下一步”重新开始：

1. 工作区固定/opt/keel，分支dev/p1-network-bom。先读AGENTS.md、README.md、本文件当前恢复入口和§75～78、DEPLOYMENT，再定向读PRD/ARCHITECTURE/DESIGN-SYSTEM/IMPLEMENTATION-PLAN对应章节。检查git status、远端差异、实际进程和3000版本，保护已有服务。
2. 当前主线是WP-1.5双通道Runner的剩余能力。Agent的注册/mTLS/心跳、host.identity/host.wait、统一执行/回执/unknown恢复及网页入口已经完成本阶段交付，不要重复实现。UI旧控件迁移和JetBrains Mono已经完成，不再当待办。
3. 同密钥续签与owner状态已经写入源码，新增关闭默认/实际叶证书绑定/本地原子更新，勿重复开发；完整CA轮换仍未完成。下一完整主单元是WP-1.7的共享Execution冻结装机请求、重装准入与独占资源，之后接受控ProxyDHCP/iPXE、同请求回调及真实首连/修复。已有准备接口只预览，不提供启动按钮或安装成功证据。通知邮件/IM/页面等并行依赖按WP-1.6继续，完整Agent任务/分发按WP-1.5推进；不能为关闭FR-0强拉P2/P4。用户要求P1实现后集中联调，不逐增量跑浏览器/全量测试或构建镜像。
4. 宿主所有Go命令仍须GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json、GOMEMLIMIT=768MiB；若overlay不存在，先恢复草稿排除措施。internal/platform/fabric_precheck.go与internal/platform/migrations/026_fabric_precheck.sql是原始未跟踪草稿，不能直接编译、迁移、提交或删除。镜像用精确提交git archive构建。
5. 实际软件验证只使用自有随机keel_test_*隔离库；真实Agent/SSH、主密钥、OIDC/LDAP和外部介质获取默认关闭。不得为展示UI而生成生产CA/token、启用真实设备写入或伪造成功。检查监听归属后再启动开发服务，不能凭历史记录假定5173是Keel。

整体状态：项目/RBAC/审计、BOM/型号/介质/参数、Flow/Execution及共享工作台已有可运行基础；网络域已有BOM、规划/就绪自检、Kea/ZTP与N-S5读取等阶段性增量，完整Golden/健康/Readiness及后续链条未完成。SSH/Agent均已有真实软件协议验证的受限执行子集，完整bootstrap/repair/PXE/凭据生命周期、任务目录、规模与真机PoC仍缺。WP-1.5、P1和跨阶段FR-0保持partial，不按页面数量或旧标签估算完成百分比。

## 1. 工作区与运行版本（早期快照，当前见上方）

| 项目 | 当前记录 |
|---|---|
| 工作区 | `ubuntu@43.207.20.26:/opt/keel`；tmux `codex-keel` |
| 分支/本轮起点 | `dev/p1-network-bom`；整理文档前 HEAD `c8955ca`（docs），最近已部署代码提交 `71049c1`（此前 `df95d0b` 为旧仿真增量） |
| 历史 P1 基点 | `p1-complete` = `e7d5f1a`，为当前开发分支祖先；该标签只代表当时的 P1 范围，不证明 v3.0.3 新增网络任务完成 |
| 3000 预览 | `keel:71049c136628d5583fad3d478865fd7e6b2dabcb`；本轮 `/healthz` 实测版本一致、容器 healthy，规划UI/CSS摘要与已验证构建一致 |
| 运行库 | 当前镜像包含32个已发布迁移：001～025 + 027网络角色 + 028规划/自检 + 029统一目标/Kea执行 + 030 DORA证据 + 031 N-S5采集 + 032模板/意图 + 033 PushJob；未包含026 N-S5草稿。新表已授权受限应用角色，不改变既有成员授权 |
| 开发入口 | API 3001；Vite 5173。与 3000 构建镜像分别检查 |
| 工作状态 | 用户已恢复开发；Kea Runner、DORA与真实239入口已发布3000；N-S5采集/页面增量已验证并发布；真实写入默认关闭 |

版本是本次观察，不是永久常量。恢复时检查 `git status -sb`、`git log -1` 和 3000 `/healthz`；不能凭本文件推断后续运行状态。

## 2. 已有能力与证据边界

| 范围 | 已有开发记录 | 剩余/不能声称 |
|---|---|---|
| 认证/项目 | 本地认证、服务端会话、CSRF、撤销、项目授权；账号/成员/审计管理已有交付记录 | NDE/NH成员角色已加入CLI/API/UI；当前已实现fabric:read/plan，NDE可执行本项目ZTP。LDAP/OIDC、审批签名/生产下发等剩余权限及完整项目生命周期仍未闭环 |
| 平台基础 | P1 执行仿真、介质/参数、型号不可变版本及相关页面已有记录 | 仿真执行不证明真实 Agent/SSH、介质分发和所有平台业务完成 |
| BOM/型号 | 标准包/宽表兼容、原子幂等导入、稳定身份、问题清单、部分型号/工程检查 | 完整商定基线、外部批准证据验真、全规模性能和剩余工程校验未闭环；F-0.1～F-0.4 不记 PASS |
| ZTP | 身份反查、mock Kea staged/active、DORA 契约、239/143 专用 listener、仿真九步时间线、自动重试和凭据模拟分类 | N-S2/N-S3、真实Kea控制作业、DORA及239准入已接线；143/43/IPv6生产适配、个性化最小管理配置、真实凭据取用/可达验证及真机开通未闭环；sim.managed 不投影真实 NP-2 |
| 后续 FR-0 | N-S5采集器读取、不可变证据/预校验、API及Fabric页面/抽屉已验证；026历史草稿未使用 | 完整T类/趋势/保留/工单仍待补齐，不能计WP-2.14完成；Golden、无损/健康、规则治理、Readiness 与全链归档仍未闭环 |
| 前端布局 | 登录页保持 P1 船体龙骨设计；3000 有 BOM/ZTP 页面入口 | 共享 Shell 已实现 56px 顶栏、240→56px 侧栏、全宽内容、双入口 URL 项目切换；隔离真实 API/数据库浏览器检查通过。跨项目运维聚合、完整生命周期和其他 DS 组件仍未完成 |

最近已留存的隔离 PostgreSQL 与 `make check` 结果、BOM/ZTP 增量和边界，详见 [历史记录](history/2026-10-23/HANDOFF.md)。本轮未重跑代码测试；历史通过不能替代后续修改的验证。

## 3. 本轮文档整理

以 PRD/DESIGN-SYSTEM v3.0.3 为基准，整理架构、计划、验收及机器追踪，新增单一开发入口；旧审查与交接流水原样归档。按用户要求，实施方法改为六个完整功能块，集中必要验证、复用已有证据；模块职责与 PoC 修改入口明确，避免扩展手动仿真、重复全量测试。两份基准保留原文；差异及下游采用口径见 DEVELOPMENT §2，其中 dry-run 零副作用与调用审计的持久化边界仍待基准决策。

本轮 `python3 scripts/check-design.py` 通过；64个WP与计划/追踪依赖、验收映射一致且无环，213条原验收ID及章节编号保留；活动入口链接有效，历史快照与整理前原文一致，两份基准SHA256未变。独立复核发现并修正工单恢复互锁与N-S2/N-S3落点缺失。文档检查不证明FR-0已实现；本轮未运行模拟/代码全套、未部署或迁移。

## 4. 保留的修改与恢复入口

- `AGENTS.md` 有早于本轮的未提交修改，保留原样，不纳入本轮文档提交。
- `internal/platform/fabric_precheck.go`、`internal/platform/migrations/026_fabric_precheck.sql` 是暂停时的未跟踪草稿。恢复后先检查其与 PRD N-S5 pending/异常、G2 四值门禁的边界，再补 API/页面与测试；不能直接迁移或部署。
- 恢复开发时先补共享工作台布局与网络角色权限缺口，再按实施计划的依赖补齐 WP-1.15/1.16/1.17 未完成范围，推进 WP-2.14/2.15 及后续 FR-0。不要把“已开始后续 WP”记成前置已验收。

## 5. 部署与回退证据

3000 已经由用户要求同步到当前开发预览，不能再把它称为未更新的纯 P1 环境。旧镜像 `keel:p1-e7d5f1a` 保留；源码从该基点接续且登录源码未改。部署前备份：`/home/ubuntu/.local/state/keel/backups/pre-fr0-preview-20261023T085854Z.dump`，历史记录为 101K、容器 `pg_restore -l` 可读；目录可读不等于恢复演练通过。

专用 239 TLS/143 mTLS listener 在 Compose 未启用；真实交换机 PoC 未执行。此前旧检查缺少 Playwright；本次已使用现有 Playwright/Chromium 对隔离 3102 的新工作台完成真实浏览器验证，不能据此声称完整 3000 业务验收通过。`/api/v1` 的 `phase=p1-foundation` 是旧静态标签，`business_ready=false`；镜像版本以 `/healthz` 为准。

镜像回滚不会回退数据库。当前 25 迁移与旧 P1 镜像不应假定兼容，恢复步骤见 DEPLOYMENT。真实配置/秘密仍仅在服务器受限目录，路径约定见该文件。

## 6. 历史与当前证据分开

完整旧 HANDOFF、早期主机/分支/备份记录及旧“下一步”见 [history](history/2026-10-23/README.md)。源码提交、GitHub 推送、CI、运行镜像与硬件验证分别报告；本轮不凭设计更新提升任何业务验收状态。

## 7. 恢复开发：共享工作台交付

文档基线提交 `a918bf9`。本次仅修改共享 UI、导航、令牌和定向浏览器检查；登录页源码未改，N-S5 未跟踪草稿及 AGENTS 用户修改保留。

实际验证：`web` 下 `tsc --noEmit`、`vite build` 通过；`scripts/check-workspace-browser.mjs` 在独立 PostgreSQL/API3102 上通过顶栏/侧栏尺寸、24px内容边距、双入口切换保留工具、多标签隔离、无项目持久化存储、明暗/中英文、1280/1440及基础页390宽、真实退出撤销与无浏览器错误。证据 `/tmp/keel-shell-dpiey32q/{browser.log,workspace-light.png,workspace-dark.png,workspace-mobile.png}`；隔离数据库、进程和临时凭据已清理。

发布从提交的 `git archive` 构建，排除未提交026迁移和草稿；工作台首次发布保持25迁移，后续网络角色发布增加独立027，当前为26个已发布迁移。后续优先补网络角色权限及 WP-1.17 的真实可替换适配器、N-S2/N-S3，随后 N-S5/Golden；不新增手动推进成功的模拟流程。FR-0仍未闭环。

## 8. 网络角色与字体（当前增量）

- `c39ffc6`：成员目录/CLI/API/Ent/UI增加NDE/NH，既有成员授权不变；当前网络权限目录只返回已实施的 `fabric:read` / `fabric:plan`。NDE可在获授权项目操作ZTP，NH/NE/DE等不能借另一项目的导航能力写入；变更仍复用原事务、审计和会话撤销。审批/生产下发未实现，不返回虚假审批能力。
- `d31bd5f`：按用户2026-10-23追加要求更新DESIGN-SYSTEM字体规范，正文/控件/代码统一Intel One Mono。官方V1.4.0三字重WOFF2、原始OFL及SHA256随应用；无外部运行时字体请求；不覆盖的中文回退系统字体。此前“两份基准保持原文”的记录仅描述文档整理当时，本次字体是用户明确批准的新约束。
- 实测：`TestFabricProjectRoleBoundaries`在独立真实PostgreSQL通过8角色、目标项目写入、跨项目能力不传播、伪造项目角色拒绝、项目管理隔离和改角色撤销会话；`go vet ./internal/platform ./internal/httpapi`通过；前端类型/构建通过。字体及受影响布局浏览器检查证据 `/tmp/keel-shell-d08zdpn1`，包含400/500/700字体实际加载、登录/控件/导航/代码字体、明暗/双语/布局/多标签及退出检查。未重跑全量设备仿真、未做硬件PoC，不记完整FR-0或审批验收PASS。
- 3000发布健康，字体响应二进制SHA256与仓库一致。备份 `/home/ubuntu/.local/state/keel/backups/pre-network-roles-20261023T103534Z.dump` 已用pg_restore读取目录，未做恢复演练。旧镜像仍保留，但25迁移旧镜像不能直接匹配当前26迁移库；如需回退须走兼容构建或审批后的数据库恢复流程。

## 9. N-S2/N-S3 准备模块增量

已实现批准基线下的管理地址/命名/批次计算、稳定分配、零副作用预览、陈旧预览拒绝、幂等原子保存与页面导入/批准引用登记/导出；N-S3四协议探针、批次/通道报告持久化、有效期/配置指纹/当前规划校验与页面已实现。说明和PoC修改入口见 [ZTP-PREPARATION](ZTP-PREPARATION.md)。新迁移028独立于未提交026草稿。

实际检查：定向算法/协议TLS桩测试、独立PostgreSQL事务测试、Go vet、前端类型/构建通过；真实API/数据库上的浏览器链通过导入/登记批准→BOM预览→保存/刷新→四unknown自检；证据 `/tmp/keel-shell-1d9fq34r`。协议桩不是设备PoC。当前未配置真实Kea/采集器/仓库探针，缺失保持unknown；既有mock Kea/仿真会话仍独立，不凭本次准备模块给WP-1.17或FR-0总体PASS。

下一完整功能：将批准规划与新鲜N-S3报告接入真实Kea/Execution准入及239/143设备入口，补配置持久化/读回/DORA、个性化最小管理配置与真实凭据取用；之后推进N-S5/Golden。不扩展手动推进仿真。

发布记录：`6dfd0df` 已推送并部署3000；从干净归档构建，未纳入026草稿或AGENTS用户修改。迁移前备份 `/home/ubuntu/.local/state/keel/backups/pre-ztp-preparation-20261023T110442Z.dump`，pg_restore目录可读，未演练恢复；028迁移及应用角色新表授权成功。进程健康与部署UI/CSS摘要复核通过。GitHub Actions未在本轮查询，不声称CI通过。

## 10. Kea 控制作业与统一Runner增量

新增Kea暂存/执行API及页面，复用N-S2规划、有效N-S3、独立批准脚本与SoT管理MAC；已接入现有Execution/attempt/outbox/fencing/资源租约/回执。统一目标注册新增kea类型，未伪装成sim库存；generic sim入口拒绝系统Kea流程，普通DE/SRE不能操作Kea控制。

真实协议适配支持config-get/test/set/write、活动与完整内容读回；只合并批准子网内目标239预留，完整配置/其中凭据只在内存中。写入不明不伪造失败回执，reaper隔离资源并阻止后续写入。修改入口/协议依据/PoC配置与剩余边界见[KEA-RUNNER](KEA-RUNNER.md)。新增029迁移独立于未提交026草稿。

实际验证：定向Kea协议、冲突/大整数/秘密保留测试；真实API+worker+PostgreSQL+双向TLS协议桩浏览器链及未知写入隔离通过（`/tmp/keel-shell-mmgiri2k`）。原执行器9组边界回归、24逻辑目标100%完成（`/tmp/keel-shell-g87izpv0`），Go vet、前端类型/构建通过。未跑2000规模，未接真实Kea/交换机，未查GitHub Actions。新模块默认不启用写入。

下一步：真实DORA/lease验证与真实活动引导路由、239/143入口准入接线；随后个性化最小管理渲染、凭据、N-S5/Golden。当前config_persisted不等于设备managed或NP PASS，FR-0仍未闭环。

发布：代码`714d3a0`、迁移并发保护`0a5620a`已推送，3000镜像`keel:0a5620ace390a9050135d46808909b6d1cc5490c`健康；Kea/执行页包摘要与浏览器已验证构建一致。干净镜像先在隔离库从旧27迁移升级到28迁移，既有sim目标保留、新目标注册触发器通过；迁移对旧目标表加锁，避免回填/装触发器窗口漏记录。运行库迁移及应用角色授权成功。备份`/home/ubuntu/.local/state/keel/backups/pre-kea-runner-20261023T114346Z.dump`目录已读取，未做恢复演练。旧镜像保留；旧迁移数镜像不能直接匹配新库。AGENTS及026草稿保持未提交。

## 11. DORA与真实239入口增量

新增独立运维canary的UDP DORA验证，随原Kea Execution/Runner运行；030迁移保存不可变证据，`boot_ready`与配置持久化明确分开。真实239入口接线到当前控制摘要、规划、审批、源IP/MAC；最新失败证据和未知写入均阻断，不回退mock。UI展示状态与DORA证据。143/43/IPv6、个性化管理、凭据及后续FR-0功能仍需继续，不能据此宣布WP-1.17完成。

实际验证与配置见KEA-RUNNER新增章节；本轮有效证据 `/tmp/keel-shell-w792odlk`。前序浏览器失败先为文案定位，后为fixture缺少基板MAC导致正确隔离；已补fixture完整身份并保留产品准入，未放宽业务判据。当前继续开发，不等待逐步确认。

## 12. N-S5采集、预校验与Fabric页面增量

当前已实现范围及PoC修改边界见[FABRIC-COLLECTION](FABRIC-COLLECTION.md)。当前BOM设备在无采集数据时也可查看；固定mTLS采集器读取→标准化校验→不可变存储→双向LLDP预校验→设备总表/8Tab抽屉已接通，服务器端口保持pending/NP-3 BLOCKED，缺阈值DDM为unknown。NE复测与Viewer只读按目标项目重新授权。031迁移不依赖026草稿。该增量不代表WP-2.14或FR-0整体完成。

前序DORA发布确认：3000运行16a3371，健康检查版本一致；备份`/home/ubuntu/.local/state/keel/backups/pre-dora-20261023T121330Z.dump`可读取目录，未做恢复演练。新增030后为29个已发布迁移，026仍未部署。当前继续开发，无需逐步请求确认。

N-S5最终权限集成证据：`/tmp/keel-shell-ienrztcf`，覆盖Viewer只读、NE复测和导航权限目录。前序`pxj7dic0`为增加NE前的有效主链证据；补NE发现并修复auth导航能力列表遗漏fabric:collect，未放宽目标项目授权。Go定向检查与前端类型/构建通过。

## 13. 143真实双通道增量

Kea143二进制选项、独立mTLS探针/双通道N-S3引用、DORA同时核对239/143、真实143引导与进度路由已接线。证书序列号必须匹配管理源绑定；239旧证明、最新失败或未知写入不开放143；配置真实端点后禁写不回退mock。落库前重验并锁定账号/角色/执行意图。详见KEA-RUNNER末节。有效集成证据`/tmp/keel-shell-7ly7d7cy`，新143错误URI负例、136编码、原控制协议定向检查及Go vet/前端构建通过；未跑硬件PoC。

N-S5发布确认：3000当前5c256fd，健康版本一致，031迁移成功。备份`/home/ubuntu/.local/state/keel/backups/pre-fabric-collection-20261023T123608Z.dump`目录可读，未做恢复演练；026及AGENTS原修改仍未纳入。后续继续最小管理个性化/凭据/自动真实状态收敛、Golden/PushJob、健康/规则/Readiness与归档，不将本次双通道视为FR-0闭环。

## 14. 2026-10-23 网络模板与 PushJob 增量

此前8b6971c已部署3000，补齐143与239双通道 DORA、批准源地址/身份准入；仍为30个发布迁移，未启用生产网络写入。当前增量待下述提交和部署记录确认：

- 新增 pongo2 v6.1.0 沙箱、批准基线逐SN变量、不可变模板/意图版本、三层检查/预演/golden 读回、独立NH/Admin发布；配置页面 `/p/{projectId}/configuration` 已接入共享导航及中英文/Intel字体。
- 新增 PushJob 冻结快照/结构化差异、普通NE+DE双签/Critical加NH三签、禁止自签和重复计票、当前审批权限复核；准备和下发均复用统一Execution/outbox/Runner/SSH租约/fencing/回执。
- 达门槛批次先一台金丝雀，复采通过再放行余量；失败恢复快照；审批后外部漂移明确拒绝写入且不覆盖外部变更；未知写入不提交虚假失败/停止回执，保留共享资源隔离。终态支持归档，审批通过后仍可由独立审核者撤回为REJECTED。
- mTLS网关为可替换设备边界，不接受浏览器端点；显式生产写入默认关闭，未连接真机。模块与契约见 [NETWORK-CONFIGURATION](NETWORK-CONFIGURATION.md)。新增032/033迁移，部署仍须排除旧026草稿。

实际证据：`go test ./internal/netbaseline ./internal/fabricboot ./internal/fabricmon -count=1`、相关五包`go vet`、前端生成类型/TypeScript/Vite构建通过。最终网络集成目录 `/tmp/keel-shell-82z3_1on`：真实API/PostgreSQL/Worker/浏览器、mTLS设备协议夹具，覆盖完整模板/三签/金丝雀、外部漂移保护、失败阻断/恢复、审批角色撤销/撤回/归档、UNKNOWN隔离、项目授权、UI字体/来源标签。原执行器9组/24目标回归目录 `/tmp/keel-shell-itp33210` 全部通过；该结果不代表2000台规模或真机验收。测试库/进程/临时密钥均已清理。

尚未交付的模板/PushJob细项及PoC接入限制已明确列入模块文档；Golden到NP-2投影、全网健康/规则治理/Readiness、ZTP剩余范围与完整FR-0验收仍未完成，不标FR-0闭环。用户目标仍为持续开发至FR-0整体闭环。

部署确认：代码提交 `71049c136628d5583fad3d478865fd7e6b2dabcb` 已推送GitHub；从该提交的干净archive构建镜像，确认未包含026草稿。部署前运行库备份 `/home/ubuntu/.local/state/keel/backups/pre-network-config-20261023T140043Z.dump`，`pg_restore -l` 可读（未执行恢复演练）。新032/033迁移及受限应用角色授权成功，运行库为32个已发布迁移；3000 `/healthz` 返回上述代码版本、API容器healthy，配置页面bundle请求200。`.env` 镜像引用已更新且保持600；生产网络网关未配置/写入未开启。GitHub CI未查询，不声称通过。


## 15. 2026-10-23 健康、Readiness、网络证据与日志增量

LL-01～LL-07 类型化指标与安全 AST、精确十进制/窗口/来源校验、最低分健康汇总、NP-1～NP-6 只读门禁、LLDP/线缆查询、14 类日志规则及归一化聚合/分页/JSON CSV 导出已接入。健康页 `/p/{projectId}/health` 共用工作台/Intel 字体、URL 筛选；缺证据 unknown/阻断，协议夹具不能放行生产门禁，服务器 NP-3 仍待真实三方证据。模块协议、边界和未完项见 [NETWORK-HEALTH](NETWORK-HEALTH.md)。不新增数据库迁移，仍 32 个已发布迁移。

本轮有效集成 `/tmp/keel-shell-lw2nw8tb`：真实 API/PostgreSQL/Worker/mTLS 采集协议夹具/浏览器完整链路，通过 Readiness 时效/无数据未知、超 2^53 计数器增量、QoS 不一致、LLDP BOM 端口授权、线缆分页、日志聚合/覆盖缺失/过滤/导出、页面与字体检查。相关 Go 定向测试/vet、TypeScript/构建通过；不代表硬件 PoC 或 FR-0 全体验收。此前中断轮次不记通过。

用户已授权并行开发：规则治理、ZTP 剩余流程、FR-1 作业平台分别推进；共享契约/路由由主线合并。034 预留规则治理、035 预留 FR-1、036 预留 ZTP；各批只有实际提交迁移才可发布。当前原有 AGENTS、026 与 fabric_precheck 草稿保留。本节功能待后续部署确认，不因源码已实现而宣称 3000 已更新。

部署确认：`c22ac1ce3874035f2760f67a8f220ff4ee08a889` 已推送；从干净 archive 构建并部署 3000，未包含026草稿，无迁移变更。API容器 healthy、`/healthz` 版本精确匹配、健康页 bundle/SPA 请求200；`.env` 镜像引用与600权限已确认。原镜像保留。当前未查询 GitHub CI，不声称其通过；未接真机。


## 16. 2026-10-24 规则治理、ZTP 最小管理与 FR-1 Flow 工作区合并

- FR-0 lossless 规则版本/独立 NE+NH 审批/发布时角色复核、停用 unknown、生成新版本回滚、typed XOFF/PFC 阈值和有界 48h 逐分钟历史预演已接到健康消费者。034 迁移；详见 NETWORK-RULES。预演遵循 FR-0.7.6 零业务写入，保存候选/发布持久化报告与审计；BASE-08 一般审计文字差异仍保留待统一。其他 rule domain 和大规模异步重放未完成。
- ZTP 个性化最小管理脚本/精确摘要清单审批/239 与143 投递、只读 mTLS 认证可管理性复验、设备级 DORA、凭据过期/有界重试、真实九态会话及共享 SSH 资源互斥已接线；036 迁移。fixture 不能生成 device managed。真实网关/NOS/凭据生命周期/IPv6与厂商适配等限制见 ZTP-COMPLETION，不据此宣称 WP-1.17 或 FR-0 全部验收。
- FR-1 首批 Flow 工作区：Monaco YAML/本地 worker、严格执行解析复用、版本历史/diff、审计/幂等/版本冲突保护的新版本回滚、共享执行器启动。通用发布/回滚不能覆盖业务专属 Kea/fabric Flow。035 未使用，无该批迁移。详见 FR1-IMPLEMENTATION。

实际证据：纯 fabricmon/fabricboot/execution 全包测试和相关五包 vet 通过；合并后契约生成/TypeScript/Vite 构建通过。规则真实 API/PG/mTLS采集夹具/浏览器证据 `/tmp/keel-shell-53_0fu1q`；健康/日志回归 `/tmp/keel-shell-x1h3lzka`；ZTP 新只读接口/空态/权限/页面/Intel 字体 `/tmp/keel-shell-w1ce_2ky`；ZTP 隔离库 TestZTPManagementArtifactsAndDeviceProjection 通过并清理；FR-1 真实 API/PG/Worker/浏览器完整版本/回滚/运行证据 `/tmp/keel-flows-bkvytiei`。ZTP 页面发现并修正嵌套 main 容器，首次定位器失败轮次不计通过。

这些均是软件框架/设备协议夹具证据，未跑真机 PoC、千台性能或 GitHub CI。当前功能待下述发布记录确认，FR-0/FR-1 均不能标全量完成。原 AGENTS/026/fabric_precheck 草稿保持排除。

用户授权继续并行：`codex/fr1-views` 执行详情、`codex/fabric-diagnostics` T4/T6/DDM、`codex/shadcn-ui` 组件库均在 `/home/ubuntu/keel-worktrees/` 独立工作树；后续前端统一采用 shadcn/ui，适配既有设计令牌/工作台/主题/Intel 字体，不替换 Monaco。先完成本批稳定发布，再按该提交同步下一批工作树。

## 17. 2026-10-24 本地进度同步（换设备继续开发入口）

当前远端主工作目录为 `/opt/keel`，分支 `dev/p1-network-bom`，HEAD `6aa8479`。2026-10-24 将本地主工作树的 PFC 风暴聚合草稿同步至此目录：`api/openapi.yaml`、`docs/NETWORK-HEALTH.md`、`internal/platform/network_health.go`、`internal/fabricmon/storm*`、网络健康页面/翻译/CSS及 `FabricStormSummary.tsx`。这批仍未提交、未构建或部署；3000 预览继续运行 `6aa8479` 镜像，不能据此认为页面或 API 已更新。同步前重叠文件备份为 `/home/ubuntu/.local/state/keel/backups/local-sync-20261024/main-overlap-before-sync.tar`。

本地主机另有三个独立开发分支，已原样同步至远端 Git 工作树，尚未合并主工作目录：

| 进度 | 远端工作树 / 分支 | HEAD 与状态 |
|---|---|---|
| 交付证据导出 | `/home/ubuntu/keel-local-sync-20261024/fabric-delivery` / `sync/fabric-delivery-20261024` | `a6b07e8`；WIP，未验证 |
| 物理诊断与 DDM 趋势 | `/home/ubuntu/keel-local-sync-20261024/fabric-diagnostics` / `sync/fabric-diagnostics-20261024` | `e45081c`；尚未合并 |
| FR-1 执行视图与事件流 | `/home/ubuntu/keel-local-sync-20261024/fr1-views` / `sync/fr1-views-20261024` | `4805e7f`；WIP，未验证 |

暂停的 FR-1 重跑草稿保存在 `/home/ubuntu/keel-local-sync-20261024/fr1-rerun-draft/`，见其中 `PAUSED.md`；不要直接复制进编译应用。对应完整传输包与校验清单在 `/home/ubuntu/keel-local-transfer-20261024/`。

原有远端工作树 `/home/ubuntu/keel-worktrees/{fabric-delivery,fabric-diagnostics,fr1-views,shadcn-ui}` 保持原状，其中诊断和执行视图有各自未提交改动；不能将同步分支视为已经合并到这些工作树。`/opt/keel` 原有 `AGENTS.md`、`scripts/check-health-browser.mjs`、`web/src/generated/api.d.ts`、026 迁移与 `fabric_precheck.go` 等改动保留。继续开发前先分别运行 `git status -sb` 并比较重叠文件，按需整合，不覆盖未提交成果。本次仅完成文件同步及逐文件一致性核对，没有运行测试、迁移、GitHub 推送或服务部署。

## 18. 2026-10-24 开发工作流与文档核对

用户指定 `/opt/keel/` Git 仓库为唯一开发工作区域；通过 SSH 执行，复用 `codex-keel` tmux 会话保持进程。历史独立工作树仅作待整合资料，后续修改统一在主目录完成。安全默认配置、合理沙箱和审批机制保留。已比较昨晚本地保存版本：PRD、DESIGN-SYSTEM、ARCHITECTURE、IMPLEMENTATION-PLAN、ACCEPTANCE、README、DEVELOPMENT、DEPLOYMENT、需求追踪文件均逐文件 SHA256 一致；远端 AGENTS 为现行开发规范，HANDOFF 包含新增同步记录。当前继续主目录 PFC 风暴聚合草稿，未因同步提升功能验收状态。

## 19. 2026-10-24 LL-02 Fabric 风暴聚合已验证

开发仅在 `/opt/keel`、tmux `codex-keel` 进行。完成同一判定时点的独立端口去重/三口升级、缺证据 unknown、近似与降级标识、版本化规则引用、健康 API/生成类型及页面受影响设备跳转；不重复扣分，不修改设备或放行生产门禁。规则计算器保留自身判定时间，聚合层不覆写历史时间。

实际通过：fabricmon 全包 race 测试、fabricmon/platform/httpapi 三包 vet、前端完整构建（含类型生成/检查）；真实隔离 API/PG/mTLS 夹具/浏览器 `/tmp/keel-shell-ml8ladbs`，包含两设备三端口风暴与正常/空态/项目隔离/日志回归及设备 URL 跳转。首次近似测试误把 fallback 视为完整事件证据，已依据现有 degraded 语义修正断言后定向通过；未放宽产品判据。026 旧草稿经 build overlay 排除，未改动其源文件。无生产迁移或部署，3000 仍为 `6aa8479`。AGENTS 的现有用户修改及 026/fabric_precheck 草稿保留。下一关联增量为已保存物理诊断/DDM趋势的主目录整合。

## 20. 2026-10-24 物理诊断与 DDM 趋势主目录整合已验证

PFC 聚合提交 `4e89296` 已在远端 Git 保存。在同一 `/opt/keel` 工作目录整合历史 `e45081c` 的 T4/T6、批准 rail 核对和 DDM 趋势，保留现有 PFC 契约及采集夹具。补齐单端口≤2000显示点、各指标极值/首末原始点、完整历史导出摘要、隐藏的缺测/换件/来源变化仍保持断线，以及显示原始点数。旧独立工作树仅留作历史资料。

实际通过：fabricmon 全包 race、platform DDM 边界 race 测试、fabricmon/platform/httpapi vet、前端完整构建（含契约/类型）；真实隔离 API/PG/mTLS协议夹具/浏览器 `/tmp/keel-shell-t_fzzenx`，包括精确CRC增量、T4/T6/rail、窗口/纪元/来源、24/48h趋势/阈值、导出审计与PNG、8Tab/URL和项目权限。隔离进程、测试库与临时凭据已清理。无数据库迁移、部署或生产写入；3000仍为 `6aa8479`。WP-2.14/WP-3.14记 partial，尚缺采样/长期保留、完整规则/工单/服务器域消费及规模/硬件证据。AGENTS用户修改和026/fabric_precheck旧草稿继续保留；后续从主目录Git差异与本记录恢复，不返回其他工作树开发。


## 21. 2026-10-24 网络交付证据导出主目录整合已验证

在 `/opt/keel` 从 `4a1bc9d` 接续已保存 `a6b07e8` 草稿，完成正式API契约/生成类型、路由、项目导航及中英文页面，提供N-S0～8预检、分页引用、draft/delivery分离、不可变JSON归档和历史字节摘要验证。修正规则字段接线、引用去重复杂度、证据到期/夹具来源/缺管理分配阻断、并发幂等与一致性读。独立只读预检复用既有会话验证，修复FOR SHARE与会话刷新的序列化冲突；不放宽权限或交付准入。模块边界见 NETWORK-DELIVERY.md。

实际验证：排除既有026/fabric_precheck草稿的overlay `make check`通过（设计、全仓Go vet/race、前端类型/构建）；引用装配优化后fabricdelivery race与相关包vet通过。最终真实API/隔离PostgreSQL/mTLS协议夹具/浏览器证据 `/tmp/keel-shell-3wts2goj`，覆盖稳定预检摘要、阶段缺失/夹具阻断、分页、过期/错误范围验收拒绝、幂等与并发、变更后旧预检拒绝、历史原字节/摘要、审计、Viewer下载、跨项目/匿名拒绝和确认/历史页面。最终实际JSON按standalone JSON Schema验证通过。此前会话锁冲突轮次不计集成通过；初次测试缺创建项目幂等头已补测试调用，产品要求保持。隔离进程/库/临时凭据已清理。

038迁移只在隔离测试库执行；未迁移运行库、未部署、未进行生产设备写入，3000仍为 `6aa8479`。FR-0/WP-4.10仍partial：N-S0项目模板应用、完整判定索引、服务器G2/PortModel/CMDB消费与归档ACK、CSV/异步流式导出、长期保留和规模/真机证据尚未闭环。100000对象/32MiB为inline边界，不是规模验收。原AGENTS与026/fabric_precheck草稿保留。下一步接续昨天保存的FR-1执行视图/事件流，再核对shadcn组件库成果；历史工作树只读参考，修改统一主目录。


## 22. 2026-10-24 接续昨天FR-1执行详情与事件流已验证

网络交付导出提交 `3b0385c` 后，在唯一主目录 `/opt/keel` 整合昨天保存的 `4805e7f`。五Tab概览/Gantt、虚拟目标矩阵、尝试抽屉、生命周期日志SSE、回执输出/冻结变量和关联视图均复用现有执行内核。SSE按活动Tab订阅、游标续读/重同步、连接轮换与当前授权撤销；不把生命周期事件称为stdout，不把结构化回执称为文件制品。精确锁定的react-virtual依赖已按lockfile安装，npm审计0漏洞。

实际通过：execution/httpapi race、相关Go vet、完整前端契约生成/类型/Vite构建；真实API/隔离PostgreSQL/worker/浏览器 `/tmp/keel-views-n_wiaccz`，覆盖500目标暂停样本的矩阵过滤/分页/虚拟渲染、五Tab懒加载和URL、2目标执行/4条回执、冻结参数与输出导出审计、跨执行/项目拒绝、丢回执unknown不计完成、SSE初开/失效游标重同步及登出撤权关闭。隔离资源已清理。此证据不代表规模性能、真实设备或整个FR-1验收。

无迁移、部署或生产写入；3000仍 `6aa8479`。原AGENTS与026/fabric_precheck草稿保持保留。FR-1重跑独立草稿仍未接入；后续继续核对并整合昨天shadcn组件库保存成果，优先修复已记录的焦点恢复问题，完整前端回归后再计交付。


## 23. 2026-10-24 接续昨天共享 shadcn/ui 基础组件已验证

在主目录 `/opt/keel` 接续历史 `3c489c5`，保留已集成的网络交付与执行视图。接线 Tailwind v4 前缀工具类/既有 Keel 令牌、16个自有基础组件、TooltipProvider、Preferences 和共享 ActionDialog；将新增诊断/PFC/DDM/交付/执行页面迁移到组件边界。旧原生控件预算沿用保存版本，不增加预算绕过 AST 检查。NativeSelect 保留系统选择语义，DDM Checkbox 用 Radix；矩阵尺寸、状态色、执行Tab选中态与右侧尝试抽屉保留。详见 SHADCN-UI.md。

实际通过：完整前端生成/组件边界/TypeScript/Vite构建；独立共享UI浏览器 `/tmp/keel-shadcn-2vx9lbso` 验证明暗令牌、Intel字体、背景、键盘焦点约束/恢复、忙碌Escape保护及Tabs。焦点断言等待Radix关闭生命周期，截图等待入场结束，不以历史未验证记录计通过。真实API/隔离PG/worker/协议夹具/浏览器三路回归通过：网络交付 `/tmp/keel-shell-g7_57whs`、物理诊断及DDM指标Checkbox/PNG `/tmp/keel-shell-1e2552ay`、最终执行视图 `/tmp/keel-views-jrx3w19y`（含8/16/24/32px实际矩阵尺寸、右侧抽屉位置、五Tab/回执/SSE撤权）。测试进程/库/临时凭据及独立5178 Vite已清理，证据截图保留。设计检查器修正数字文件名识别后通过v3.0.3引用检查，未修改正式设计或放宽实现状态。

GitHub SSH仓库读取已验证；origin main/dev均仍为 `6aa8479`，本轮新增提交尚未推送。未迁移运行库、未部署或操作生产设备；3000原服务和依赖仍健康。Go测试继续通过既有overlay排除026/fabric_precheck旧草稿，草稿源文件及AGENTS用户修改未纳入提交。旧页面尚未全量迁移，未完成全组件无障碍/规模/真机验收。下一关联事项为昨天独立保存的FR-1重跑草稿：先按其PAUSED说明和当前执行内核比较，补验证后才接入；不要回其他工作树继续开发。FR-0、FR-1及WP-1.1仍partial。


## 24. 2026-10-24 按新版 A3 接续 FR-1 定向重跑

主目录 `/opt/keel`、分支 `dev/p1-network-bom`，基于 HEAD `1706a17` 接续昨天保存的重跑草稿，未切回历史工作树。任务对应 PRD FR-1.4／实施计划 A3、WP-1.4；未追赶 FR-0 后续阶段依赖。

已接通候选预览、版本/集合摘要确认、单目标及失败目标的新执行、冻结来源快照、幂等/当前权限/参数目录准入、outbox/审计、共享确认弹窗和来源链接；复用原执行器、资源锁及 unknown 核验，不改原执行。Kea/fabric 保留原业务批准入口。模块与边界见 FR1-EXECUTION-VIEWS 最新节。

实际通过：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json go test -race ./internal/execution ./internal/httpapi -count=1`、相关包 vet、`npm run build`（契约生成/组件边界/类型/Vite）、`python3 scripts/check-design.py`；最终真实隔离 API/PG/Worker/浏览器 `/tmp/keel-views-y5o63831`，包含重跑正反路径、并发幂等、来源关联及原五Tab/SSE/矩阵回归。新增脚本首轮误读 overview 字段不计通过；修正为 relations 后通过。隔离进程/测试库/临时凭据已清理。非规模或真机证据。

本批尚未提交/推送；未迁移运行库、未部署3000，原服务保持。用户 AGENTS/实施计划及交换文件、026/fabric_precheck旧草稿和此前模板废弃未提交修改均保留，未纳入本批功能。当前完成的是 A3 内一个功能块，WP-1.4、FR-1、P1仍 partial。后续按 A3/A4 的实际输入继续收口；A4 介质真实字节校验/批准制品读取是可独立推进的缺口，不以完整 Agent 或未来 P3/P4 为该项前提。


## 25. 2026-10-24 A4 介质文件存储、校验和批准读取已验证

在唯一目录 `/opt/keel`、`dev/p1-network-bom`、HEAD基线`1706a17`接续上一节，完成 A4／WP-1.10 的实际文件功能块。新增私有MinIO SDK适配、显式media-init桶初始化、039不可变版本绑定、服务端计算摘要的上传与原登记人附加、读取真实文件的校验/独立Admin或SRE审批、鉴权下载与Range及审计；页面共用现有工作台与确认弹窗。旧manifest字符串不再构成校验通过证据。原BOM/ZTP测试改为真实提供脚本字节；外部对象传输的Go边界mock与真实MinIO集成分别验证。

真实浏览器同时发现共享Radix弹窗滚动锁样式被严格CSP拦截；按请求生成样式nonce，HTML入口和React/get-nonce接线，保持script-src self且不开放unsafe-inline，nonce HTML不缓存；专项单测和介质弹窗回归通过。不是靠忽略控制台错误放行。

实际通过：前端完整生成/组件边界/TypeScript/Vite；overlay全仓Go vet/race、最终httpapi race/nonce单测；Compose配置、设计检查与diff检查。最终真实隔离PG/MinIO/API/Chromium证据 `/tmp/keel-media-koxmml78`，包含版本固定/缺失与停用版本拒绝、字节篡改/绑定不可变、BOM/ZTP相关准入、介质与参数目录回归、真实上传校验审批/下载、Viewer/Range/CSRF/跨项目/匿名/退役等正反路径。首轮测试tmpfs权限、旧语言定位与CSP失败不计通过。隔离库、MinIO、API和临时凭据均清理；未操作运行桶或设备。

039仅用于隔离测试库；尚未提交/推送、未迁移运行库/部署3000或验证新发布镜像，原服务保持。SDK固定在go.mod/go.sum，get-nonce直接依赖固定在web/package-lock。原AGENTS/实施计划/交换文件、026/fabric_precheck草稿、模板废弃草稿及上一节重跑成果全部保留。Go继续使用既有overlay排除旧草稿，未修改其源码。

范围与运维见 MEDIA-ARTIFACTS、DEPLOYMENT §8。此块完成不代表整个WP-1.10/FR-1/P1闭环：签名/离线依赖闭包、目标缓存和Agent/SSH分发、镜像源/外链、隔离治理/保留清理/容量仍未完成；参数目录及业务消费者对真实制品/架构/兼容矩阵的完整引用准入是后续A4／WP-1.11的收口事项，不追赶P3/P4。


## 26. 2026-10-24 A4 参数目录与通用执行的制品引用准入已验证

在唯一主目录 `/opt/keel`、`dev/p1-network-bom`、HEAD基线`1706a17`接续上一节，完成 A4／WP-1.11 的一个完整准入功能块，未开发未来 Agent/P3/P4 前提。目录只列出当前项目/全局已批准、未退役且有真实不可变绑定的制品；共享校验检查 CPU 架构、精确依赖及来源/版本/报告引用、类型、歧义与退役覆盖。修复项目入口能退役全局参数及项目退役后回落全局的问题。

执行列表与 Flow 工作区的运行弹窗共用 ParamSelect/CatalogInputs，版本与受控目录项采用下拉，实时展示缺依赖及矩阵来源；注册链接保留项目。补齐正式参数路由/校验契约与生成类型；Flow输入名采用目录兼容的点分键，任务ID语法不变。执行创建仍在原事务中检查权限/幂等并冻结定义/依赖/介质URI/摘要/固定对象版本；提交实际重读固定对象并校验字节，缺存储/对象或损坏拒绝。重跑复核当前准入，目录定义/制品与源快照变化拒绝，源历史不改写。模块、格式及边界见 CATALOG-ADMISSION.md。

实际通过：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json go test -race ./internal/platform ./internal/execution ./internal/httpapi`、相同相关包 vet；`npm run build`（正式类型生成/共享UI边界/TypeScript/Vite）；`python3 scripts/check-design.py` 与 diff检查。Go无测试库时的普通race不冒充PostgreSQL集成证据，旧026/fabric_precheck草稿仍通过既有overlay排除。

最终真实隔离 PG/MinIO/API/worker/Chromium：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-media-artifacts-integration.py`，证据 `/tmp/keel-media-n5z7aoag`。包括真实版本读取、历史 approved 无字节排除、字节损坏拒绝、全局/项目覆盖与退役、BOM/ZTP相关Go回归、原介质与参数HTTP/浏览器、独立SRE审批/Range、实际目录下拉/实时依赖、执行冻结摘要/对象版本/矩阵、稳定重跑及目录变化拒绝、重名版本/类型/架构/跨项目/匿名/CSRF拒绝。目录截图已视觉复核。新增测试最初的管理员initial标志、旧非结构化依赖夹具、点分输入键及跨项目404预期问题均已定位修正，失败轮次不计最终通过；未放宽权限或业务断言。

另通过原Flow工作区真实API/PG/worker/浏览器回归 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-flows-integration.py`，证据 `/tmp/keel-flows-vmlhtiz3`，包含共享运行入口、Monaco/版本/diff/回滚、幂等/权限/执行冻结。隔离数据库/MinIO/API/worker/临时凭据已清理，运行服务/数据卷保留。

本批未提交/推送、未迁移运行库/部署3000，未验证新发布镜像或GitHub CI。用户AGENTS/实施计划/交换文件、026/fabric_precheck旧草稿、模板废弃草稿及上一轮未提交成果全部保留。WP-1.11/FR-1/P1仍partial：当前架构取已发布Flow的显式参数，真实目标/BOM/型号/OS匹配、关联组合矩阵与报告审批治理、模板默认值同步、目录管理页面、全业务入口/编辑器/lint及真实分发/派发前重验尚未完成。后续按A4现有输入收口，不能把sim成功提升为真实硬件或完整兼容性验收。


## 27. 2026-10-24 A4 型号完整草稿审核与不可变发布已验证

继续唯一主目录 `/opt/keel`、`dev/p1-network-bom`、HEAD基线`1706a17`，接续A4／WP-1.12，修复扫描草稿进入发布页时丢失预期拓扑/面板/BOM期望/角色方案，以及复用草稿返回值缺槽位的问题。导入、复用、审核与发布现在完整承载硬件/拓扑/能力/面板/BOM/角色/槽位；补齐正式草稿API契约/生成类型。发布页逐项核对并保留来源，历史可展开完整规格；扫描不自动发布、不从实测自行生成PASS。

审核页提交所见版本条件及来源草稿ID，服务端在原事务中锁定型号并拒绝陈旧版本、被废弃或错误型号的来源；审计保留基准/来源/最终摘要。既有工具省略条件时保留原追加版本契约，不声称全部旧调用已有审核并发条件或发布幂等重放。新摘要规范化空对象/数组和槽位排序；已有存储摘要不重写，按完整内容识别旧草稿/当前已发布内容，避免伪重复。扫描网络型号保留kind及能力矩阵，同名退役型号不能被复活；项目入口不能改全局型号/废弃其他型号草稿，也不暴露未发布全局草稿。Viewer页面与API均只读。040迁移只在测试库增加草稿能力矩阵及版本/槽位修改删除、发布后追加槽位的数据库拒绝保护。

实际通过：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json go test -race ./internal/platform ./internal/httpapi`、对应包vet、`npm run build`（正式契约生成/组件边界/TypeScript/Vite）、设计引用及diff检查。Go仍通过原overlay排除026/fabric_precheck旧草稿，未修改其源码；没有配置测试库的普通race不作为真实数据库证据。

完整真实隔离PG/API/Chromium：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py`，最终证据 `/tmp/keel-models-nmirxgx7`，包含真实PG并发版本冲突、版本/槽位UPDATE/DELETE及事后追加拒绝、原绑定在新发布/退役后保持、旧摘要语义去重、全局草稿隔离和BOM相关Go回归；原型号HTTP/扫描HTTP/浏览器7组；完整元数据审核、人工修改发布、陈旧页拒绝、错误/已废弃草稿、switch扫描/类型冲突、退役/Viewer/匿名/CSRF正反路径。

截图首轮捕捉到弹窗入场动画中间态；只改测试等待动画完成/滚动位置，未改产品判据，使用 `... python3 scripts/check-model-workspace-integration.py --publication-only` 定向复核，证据 `/tmp/keel-models-uomrow7r`，新浏览器功能检查及稳定截图再次通过并已视觉复核。初轮按钮同名问题已改为“移除此槽位”，旧语言按钮定位沿用当前可访问名称；端口探针处理已退出服务的TIME_WAIT，不绕过正在监听的服务。所有失败轮次不算最终通过。

隔离API、自己的测试库与临时凭据已清理，数据卷/旧服务保留；3000仍`6aa8479`。未提交/推送、未迁移运行库/部署或验证新发布镜像/GitHub CI。用户AGENTS/实施计划和此前未提交成果、026/fabric_precheck草稿、模板废弃修改全部保留。本次只收口完整型号审核发布，不将sim目标当真实BOM设备。WP-1.12/FR-14/P1仍partial，完整BOM适配与G1门禁、面板图形/角色覆盖、显式升级失效证据与真机/规模尚未完成；详细边界见 MODEL-PUBLICATION.md。


## 28. 2026-10-24 A4 型号版本与服务器 BOM 硬件期望校验已验证

在 `/opt/keel`、`dev/p1-network-bom`、HEAD基线`1706a17`接续上一节，推进 WP-1.12／F-14.3 的服务器导入功能块。逐实体 BOM 的预检和提交共用绑定版本校验：物理 NIC 数量、槽位不存在/歧义/重复/缺失，以及 NIC 正式型号名与槽位期望不符均 BLOCK；读取明确 model_ref，不跟随新发布版本。型号发布/扫描禁止非法 nic_count。未声明数量或槽位的旧版本返回对应 unverified WARN，不生成硬件核验通过证据。

兼容宽表每条 MAC 的临时 NIC 无法证明物理 NIC 实况；型号声明硬件期望时明确 BLOCK，要求逐实体 BOM，避免独立兼容路径绕过校验。问题保留原 CSV 行号。API 契约增加可选 model_version_ref/expected/actual，前端使用生成类型，问题表按整行显示明细，CSV 导出保留全部差异；BLOCK 不持久化为已提交包，无本次数据库迁移。字段和边界见 MODEL-PUBLICATION.md。

实际通过：overlay `go test -race ./internal/platform ./internal/httpapi`、对应包 `go vet`；最终 `npm run build`（正式类型生成/组件边界/TypeScript/Vite）；设计检查、Node/Python脚本语法及 diff 检查。普通 race 无测试库的 Skip 不算 PG 验收。Go继续使用 `/tmp/keel-delivery-check/overlay.json` 排除既有026/fabric_precheck草稿，未修改该草稿。

真实隔离 PG/API/Chromium：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py`，证据 `/tmp/keel-models-zqqh7kw9`。相关PG Go 9.827s，覆盖版本固定、槽位/数量拒绝、有合法令牌仍提交复核且无部分写入、宽表证据不足拒绝及已有型号/BOM回归；原型号/扫描HTTP与浏览器回归通过。浏览器实际预检确认版本/期望/实际可见、提交禁用、CSV内容完整。截图发现明细挤在问题码列，改用共享 TableRow/TableCell 整行展示后，只定向执行 `--publication-only`，最终证据 `/tmp/keel-models-yjnthyev`，包含新增宽度断言、完整浏览器检查和已视觉复核的截图；前端最终构建通过。较早 `/tmp/keel-models-bzel6efs` 不包含兼容路径修正，不作为最终证据。

隔离API、测试库及临时凭据均已清理；运行3000仍为旧`6aa8479`，未迁移运行库、未提交/推送、未部署或验证新镜像/GitHub CI。保留用户AGENTS/实施计划、之前全部未提交成果及旧草稿。此块不开发采集器/G1/P2依赖；WP-1.12/FR-14/P1仍partial，GPU/其他部件与MAC前缀规则、面板/角色覆盖、显式升级失效证据和真实硬件/规模验收仍未闭环。后续继续按A4已有输入收口目录治理/消费者与型号编辑能力，不因FR-0跨期而强行实现后置依赖。


## 29. 2026-10-24 A4 参数目录关联兼容组合已验证

在 `/opt/keel`、`dev/p1-network-bom`、HEAD基线`1706a17`继续 WP-1.11／FR-1.9，补齐既有逐键允许值不能表达关联组合的问题。`compatibility.combinations` 支持精确完整行，公共 `requires` 与任一完整行同时满足；不能投影成独立允许值再跨行混搭。参数目录和批准介质共享登记/准入校验，要求来源、版本、报告引用；限定行/列/值/文档大小并拒绝重复行、列不一致、空行及非法类型。不匹配仍按原事务拒绝执行创建；依赖自身的目录/介质规则继续检查，行匹配不豁免退役/缺失/不可选值。

正式OpenAPI、生成类型、CatalogInputs同步；错误显示完整允许组合表和来源/版本/报告，提交禁用。成功快照保留原矩阵行及固定制品；重跑仍复核当前目录，历史不重写。不新增兼容性结论、硬件探测或报告审批工作流。格式和边界见 CATALOG-ADMISSION.md。

实际通过：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json go test -race ./internal/platform ./internal/execution ./internal/httpapi`、相同包vet、最终 `npm run build`（正式契约生成/共享组件边界/TypeScript/Vite）、Node/Python语法、设计引用及diff检查。Go仍沿用既有overlay排除026/fabric_precheck旧草稿，未修改该草稿。

真实PG/MinIO测试和原介质/参数HTTP/介质浏览器回归证据 `/tmp/keel-media-p16qpj09`，其中 go-integration.log 的 platform 21.008s，新增参数级组合和公共依赖AND、跨行拒绝、底层选项不得绕过等用例通过。此轮目录浏览器因前端首次类型构建失败、仍读取旧构建而超时，整轮不记为成功。正式Schema调整后保持至少一类规则的约束并正常生成类型，最终前端完整构建通过；未放宽产品校验或测试断言。

完成构建后只定向运行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-media-artifacts-integration.py --catalog-only`，最终证据 `/tmp/keel-media-t3_34zcv`：真实隔离PG/MinIO/API/worker/Chromium通过。两条完整组合均准入、跨行混搭拒绝、非法创建未新增Execution、批准制品矩阵进入冻结快照、页面整行组合/来源/提交禁用与恢复、重跑及目录变化拒绝、歧义/退役/类型/跨项目/匿名/CSRF和历史快照回归均通过。已视觉复核 catalog-correlated-rejected.png。`--catalog-only`不运行Go/其他介质回归，真实PG Go证据使用上面独立记录，不混同为本轮重跑。

隔离API/worker/MinIO、自己的测试库和临时凭据均清理；未迁移运行库、未提交/推送、未部署3000或验证新镜像/GitHub CI，原3000仍旧`6aa8479`。保留用户AGENTS/实施计划和此前全部未提交成果/旧草稿。本块无新数据库迁移，不需要P2/P3/P4依赖；WP-1.11/FR-1/P1仍partial。目录管理页面、矩阵报告治理、真实目标/BOM/型号/OS交叉准入、模板默认值及全业务消费者/lint仍待按计划推进。


## 30. 2026-10-24 A4 项目参数目录管理已验证

在 `/opt/keel`、`dev/p1-network-bom`、HEAD基线`1706a17`继续 WP-1.11／FR-1.9，完成项目参数目录管理功能块。新增 `/p/{projectId}/params` 及工作台入口，复用共享 Shell/ProjectWorkspace/表单/表格/确认弹窗。页面查看有效/退役定义、作用域、可选值、目录默认值及兼容依赖，支持项目参数新增/编辑/退役；所有默认值下拉，版本选项来自批准且有实际文件的介质，未批准版本不入列，不提供手工版本列表。不可用默认值阻断保存，服务端保存仍重验。

按 DESIGN-SYSTEM §6：项目Admin或平台管理员管理，DL只读；其他角色不能进入管理页或查询include_retired=true。普通执行消费者的有效目录读取保留原项目成员授权。项目页面不允许修改继承全局定义；项目退役墓碑不回落全局。管理列表是当前有效/退役定义，不冒充完整修订历史。目录默认值不冒充已套用项目模板。

正式OpenAPI/生成类型增加 include_retired、create_only、expected_updated_at 及退役条件。新增页面使用create-only或所见时间条件；同一作用域/键写入串行化，现有行加锁。两个编辑者的陈旧写入仅一个成功，陈旧退役确认同样409，表单保留内容并要求取消/刷新核对；省略条件的旧调用仍保留upsert/retire语义，但共用锁。修正更新结果CreatedAt误返回当前时间的问题，更新时间严格递增；失败事务不新增成功审计。退役定义不可恢复，历史执行快照不改写。本块无数据库迁移。

实际通过：overlay相关 platform/execution/httpapi race及vet；权限收紧后 platform/httpapi race和vet再次通过；最终 npm run build（正式类型生成/共享组件边界/TypeScript/Vite）与最终契约类型重新生成；Node/Python语法、设计及diff检查。Go仍使用 `/tmp/keel-delivery-check/overlay.json` 排除既有026/fabric_precheck草稿，没有修改旧草稿或把无测试库的普通race作为PG证据。

真实PG/MinIO/原有API及介质/目录浏览器回归来自 `/tmp/keel-media-hfogrtp_`，其中platform真实PG 25.534s，包含新增并发编辑一成功一409、陈旧退役拒绝、CreatedAt保留、退役后禁恢复、有效列表与退役管理列表/全局墓碑、失败审计不落库等。此轮新管理页测试因参数键与details合并文本导致定位超时，整轮不计成功。参数键独立文本后，后续 `/tmp/keel-media-rkgqazf_` 走通业务但发现HTML pattern在浏览器v语义下报错；修正转义并收紧DS管理权限，失败轮次不算最终通过，未忽略控制台错误。

最终定向运行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-media-artifacts-integration.py --management-only`，证据 `/tmp/keel-media-1mp4g9wv`。真实隔离PG/MinIO/API/Chromium通过：新增/依赖JSON校验与查看、目录默认值、编辑冲突保留表单、批准实际制品版本下拉/排除未批准、陈旧退役拒绝与新确认成功、退役可见但不进入执行选项、create-only拒绝覆盖、DL只读及Viewer管理页/API拒绝且有效消费接口仍可读、跨项目/匿名/CSRF、两个项目标签页及英文界面。catalog-management.png已视觉复核。该定向选项跳过Go和未变介质回归，PG并发Go证据使用上面独立记录。

隔离API/worker/MinIO、自己的测试库和临时凭据均清理；运行3000仍旧`6aa8479`。未提交/推送、未迁移运行库、未部署或验证新镜像/GitHub CI，保留用户AGENTS/实施计划和此前全部未提交成果。WP-1.11/FR-1/P1仍partial：全局目录管理UI、参数修订历史/矩阵报告治理、模板默认值套用同步、真实目标/BOM/型号/OS交叉准入及全业务消费者/lint仍未闭环；后续按A4已有输入继续，不追赶P2/P3/P4依赖。详细契约与边界见 CATALOG-ADMISSION.md。


## 31. 2026-10-24 本批提交与3000发布收口

用户要求明确提交与3000发布时间后，停止扩展A4，收口§24～30成果及既有模板废弃修改。用户AGENTS/实施计划修改已单独保存为a4ed09e；026/fabric_precheck两份未完成草稿继续保留，不纳入提交或发布。

实际通过本批最终overlay `make check`（全仓vet/race、设计、前端检查及完整构建），日志 `/tmp/keel-release-check.log`；真实隔离PG/MinIO/API/worker/协议fixture/浏览器 `python3 scripts/check-kea-integration.py --network`，证据 `/tmp/keel-shell-b6q7wz5a`，含模板废弃权限/CSRF/跨项目/幂等审计/历史保留及已有任务拒绝、网络下发回归。其他功能证据见§24～30。GitHub SSH认证成功，发布前fetch确认远端仍6aa8479，无新增分叉。

本节记录提交前事实：3000仍旧6aa8479；运行库未迁移。将从明确Git提交的archive构建镜像，避免工作区旧草稿进入构建；原deploy.sh要求完全干净工作区，因此本次使用等价的受控手工发布步骤，保留其默认门禁。先备份并在隔离数据库恢复演练，验证新镜像/待执行038、039、040迁移及受限存储账号，再迁移运行库、切换3000；保留旧镜像。严格schema版本校验使单独切旧镜像不足以回滚，必须保留对应数据库备份与恢复步骤。实际发布结果随后追加；本条不代表已部署、推送、CI或P1闭环。

**本次发布实际完成**：代码提交 `c326068aae6786006a760203dd9daf85b0c6a817` 已正常推送至 `origin/dev/p1-network-bom`；计划提交为 `a4ed09e`。从该提交的 `git archive` 经stdin构建镜像，排除工作区草稿与秘密配置，镜像摘要 `sha256:748b194df3644522824014192d4fa624688ca326cd2dee942c4dfd633342356c`。3000在 `2026-10-24T14:54:13.800224+00:00` 已切换该镜像且健康，`/healthz` 返回完整代码SHA；本节后续文档提交无需重建相同代码镜像。

运行库从34条升级为37条迁移，新增038/039/040，在一个事务中执行成功；重新授权keel_app并保持审计表及迁移表写保护。最终停API后备份 `/home/ubuntu/.local/state/keel/releases/20261024T145038Z/cutover-pre-migration.dump` 格式可读，备份、旧.env和私有发布状态保存在 `/home/ubuntu/.local/state/keel/releases/20261024T145038Z`，权限600/目录700，不进Git。先前同批备份已在带disposable标签的测试PostgreSQL恢复，保留旧镜像实际启动并通过health；新镜像在恢复库迁移并完成登录/五页浏览器验证。回滚需匹配旧schema备份，不能只切镜像；尚未在运行库执行恢复或回滚，也未删除任何数据卷。

已显式初始化运行media私有版本化桶，独立service-account只限该桶对象读写/指定版本读取及必要状态读取，不含删除版本、建桶或IAM管理权限；API不使用root账号。实际账号上传/指定版本字节读回成功，删除同一探针版本明确Access Denied且对象仍可读。第一次负例断言仅识别AccessDenied而未识别工具返回的Access Denied，修正测试文本后再次确认拒绝，不放宽策略。测试探针在 `release-probes/c326068aae6786006a760203dd9daf85b0c6a817/`，不登记为产品介质。mc来自官方 `github.com/minio/mc@RELEASE.2025-08-13T08-35-41Z` 的本机构建，仅用于运维，不加入应用依赖或容器。配置只保存在受保护文件，.env仍600。

切换后真实Chromium使用既有管理员登录，参数目录/型号库/介质库/执行/Flow五页均实际打开，无页面alert及非预期控制台错误；匿名目录API为401，认证provider ready。截图在同一私有发布目录 `smoke-3000-*.png`，参数目录已视觉复核。这是运行镜像的只读冒烟，不替代§24～30的创建/批准/冲突等隔离功能验证。保留原服务范围，未额外启用execution profile worker或真实设备适配。GitHub Actions本代码提交的 `Keel platform checks` run 40104306639 已触发，记录时in_progress，尚不能声称CI通过。

隔离发布检查容器/测试数据库随后清理；本批无新增开发任务，026/fabric_precheck草稿继续原样保留。WP-1.10/1.11/1.12及P1仍partial，后续按计划A4既有输入补消费者/治理等缺口，不因本次发布宣称整阶段闭环。后续以可验收功能块及时提交，在可演示检查点发布3000，避免再积压多个功能块。


## 32. 2026-10-24 A4 既有网络消费者遵守参数作用域与退役

从 `/opt/keel`、`dev/p1-network-bom`、HEAD `793f34d` 接续已发布的c326068，按WP-1.11／FR-1.9补齐一个已有消费链路功能块，不扩展P2/P3/P4。新增共享当前目录默认值解析，接入ZTP批次/重试、网络canary/并发、DDM阈值/间隔、预检计数与健康/规则预演权重。先选择完整项目定义再解释值；被覆盖的全局定义不参与解析。退役返回409 PARAM_KEY_RETIRED，不回退全局或内置默认；只有未登记时保留原PRD默认/unknown。项目权重不再逐项混入同名全局对象，实时健康与规则预演共用同一来源。原数值范围、审批、历史计划/任务冻结值及观测不改写；不宣称退役会自动取消所有在途任务。无新迁移或API结构变更，无前端产品代码变更，不新增项目模板等后置依赖。

实际通过：overlay相关platform/httpapi最终race `/tmp/keel-catalog-consumer-final-race.log`（1.989s/1.068s）及vet、Node检查、设计和diff检查。既有026/fabric_precheck两份旧草稿未修改，继续由 `/tmp/keel-delivery-check/overlay.json` 排除。普通race无库的Skip不作为PG证据。

真实隔离PG/MinIO Go证据 `/tmp/keel-catalog-consumers-pfy_eosl/go-integration.log`：新默认值消费者测试、管理并发/退役、239/143、管理制品/设备投影、mock Kea、持久重试分别PASS。该轮管理计划测试因新增夹具把已存在批次上限设为1而按业务规则正确BLOCK，整轮不记成功；改用合法100上限，仅定向复跑失败的 `TestZTPManagementPlanTransactions`，最终 `/tmp/keel-catalog-ztp-04sjaf4p/go-integration.log` PASS（4.034s），验证项目覆盖无效全局、退役后预览/陈旧方案提交均拒绝、旧计划保留。较早FOR SHARE方案与repeatable-read只读预览不兼容，已移除；新查询也在明确ReadOnly的PG事务测试通过，不通过放宽预览事务解决。

最终真实隔离PG/MinIO/API/worker/mTLS设备fixture/Chromium：网络 `/tmp/keel-shell-3d0ir7pr`（`check-kea-integration.py --network`）通过原配置/推送/权限回归、新并发目录默认冻结、退役拒绝创建且无部分任务、真实目录页面退役批次后预览/提交拒绝；诊断 `/tmp/keel-shell-ne60493f`（`--diagnostics`）通过原CRC/DDM/8Tab/导出回归、退役计数/间隔策略拒绝当前消费且其他历史观测不改写。隔离服务、自己的测试库与临时凭据均已清理。失败的只读锁和不合法批次夹具轮次不记最终通过。

此前c326068的GitHub Actions run40104306639现已实际查询为completed/success。本节提交前3000仍c326068；本功能块准备提交后按相同37条schema构建/验证镜像并更新3000，发布实际结果另记。WP-1.11/P1仍partial：全消费者依赖闭包、全局管理、参数历史/报告治理、前端版本字段lint、目标/BOM/型号/OS交叉准入及模板同步/派发重验继续按所属阶段推进。

**§32 发布完成**：代码 `3d387032670b386c5efb838e4b294d6b67e34ddd` 已正常推送GitHub；从该提交archive构建镜像，镜像ID `sha256:8f1710b3021da601dc26eeab94964e4c23436dc1468529aa622157968d24a8cd`。候选镜像先在仅回环映射3108、使用既有受限API连接的容器验证schema/登录/参数目录/型号/介质/执行/Flow页面，随后在 `2026-10-24T15:08:37.287388+00:00` 更新3000并healthy。切换后同一五页、版本SHA、匿名401和控制台检查全部通过，healthz返回完整3d38703。证据与切换前.env受保护备份在 `/home/ubuntu/.local/state/keel/releases/20261024T150724Z`；.env仍600。无数据库迁移，运行schema仍37条，保留兼容旧镜像c326068；未执行数据库恢复或重置、未触碰其他服务/卷。独立候选容器和临时候选配置已清理。

本代码GitHub Actions run40105536743记录时in_progress，尚不能声称通过；上一批c326068为completed/success。本节发布记录提交仅更新文档，3000继续对应上述功能代码SHA。旧草稿原样保留，WP-1.11与P1仍partial。


## 33. 2026-10-24 A4 全局参数目录管理

从 `/opt/keel`、`dev/p1-network-bom`、HEAD `1d993b8` 接续§32，完成 WP-1.11 独立功能块：平台管理员系统入口 `/admin/params`，复用项目目录编辑/默认值/依赖/条件写入/退役确认及中英文界面。正式契约新增全局目录GET/PUT、按键退役POST和全局介质选项GET；全局scope由服务端固定，项目Admin/DL/Viewer均不可管理或读取全局管理接口。服务层nil项目读取也要求平台管理员，项目继承消费保持原授权。全局版本选项只使用全局已批准且有实际文件的制品，不混入项目介质；本次未提供全局介质登记上传页面。

保留项目定义优先、退役墓碑、条件更新冲突及CreatedAt不变、不可恢复退役和历史快照规则；全局修改刷新当前用户目录缓存。项目覆盖不受全局退役影响，未覆盖项目仍拒绝退役参数。无数据库迁移。旧026/fabric_precheck草稿原样保留且不提交；Go仍用 `/tmp/keel-delivery-check/overlay.json` 排除。

实际通过：platform/httpapi race（2.569s/1.117s）及vet，日志 `/tmp/keel-global-catalog-race.log`；正式类型生成/共享组件边界/TypeScript/Vite完整构建 `/tmp/keel-global-catalog-build.log`。真实隔离PG集成 `/tmp/keel-global-catalog-vk9a3nrg/go-integration.log` 通过新增全局权限/作用域/覆盖/冲突/介质来源及既有管理与消费者测试（8.989s）；该Go介质对象传输使用testMediaObjects，不冒充MinIO上传验证。

最终 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-media-artifacts-integration.py --management-only`，证据 `/tmp/keel-media-b3l_84iq`：真实隔离PG/MinIO/API/Chromium通过原项目管理和新增全局页面/API创建编辑退役、陈旧更新/退役409、跨作用域404、项目覆盖隔离、继承只读、排除项目版本、平台权限、匿名401/CSRF403、非法scope与分页过滤输入拒绝及双语导航；隔离服务/测试库/凭据已清理。此前umoux6ns因测试使用错误语言按钮名称而超时，不计最终通过；修正为现有可访问名称后定向复跑，未放宽产品规则。该选项跳过Go及未变介质回归，PG证据单独见上。

上一代码3d38703的GitHub Actions run40105536743已实际查询completed/success。本节提交前3000仍3d38703、运行schema37条；提交后从明确SHA的archive构建，候选验证后更新3000，实际结果随后追加。WP-1.11/FR-1/P1仍partial：参数修订/报告治理、前端版本字段lint、完整消费者/目标BOM型号OS交叉准入、全局介质管理及所属阶段模板同步继续按依赖推进，不因FR-0跨期追赶后置开发。

**§33 发布完成**：代码 `7eb6858ada4926039f24ca6350e9b2f79224b138` 已正常推送至GitHub；从该SHA的git archive构建镜像，ID `sha256:1bd4c64bec0d2ed50dd28f89bcf5463a7d3594c3b4732d1e0b745f7454e45cc5`，未包含既有旧草稿或秘密配置。候选镜像仅映射回环3108，使用现有受限连接，通过登录、原参数/型号/介质/执行/Flow五页以及新增全局目录页面/全局只读API、匿名401、完整SHA与控制台检查。随后于 `2026-10-24T15:24:36.890391+00:00` 切换3000，healthy；切换后同一套实际Chromium检查通过，healthz返回完整代码SHA。全局目录隔离验收截图已视觉复核。

运行schema仍37条，无数据库迁移或重置，保留兼容旧镜像3d38703；既有数据卷、worker/profile范围和其他服务保持原状。.env仍600，切换前.env、受保护发布状态及六页截图在 `/home/ubuntu/.local/state/keel/releases/20261024T152321Z`，不进Git。候选容器及临时候选配置已清理。既有026/fabric_precheck草稿原样保留，未暂存。GitHub Actions run40106739359记录时in_progress，尚不能声称CI通过；本次发布记录单独文档提交，3000继续运行上述代码SHA。下一步继续A4已就绪输入下的WP-1.11剩余治理与版本字段约束，不将全局管理完成等同P1闭环。


## 34. 2026-10-24 A4 版本输入 lint 防回归门禁

在 `/opt/keel`、`dev/p1-network-bom`、HEAD基线4f68b35接续WP-1.11。新增无依赖的TypeScript AST源代码检查及反例测试，接入web typecheck→根check/build→现有CI make check。识别原生/共享/别名/namespace控件的版本语义，拒绝自由输入与手工版本下拉，输出定位且非零退出；Flow动态渲染器单独保护“登记键ParamSelect、未知.version在文本fallback前返回禁用下拉”。允许正式介质登记的精确元数据字段、只读展示与修订引用选择，不豁免整页。

现有mock Kea版本意向并非已批准NOS制品选择；保留原模拟契约，以精确文件/函数/id/控件摘要冻结一个旧例外，变更/复制/过期会失败。没有把模拟意向描述为真实版本准入，也没有扩大例外或为FR-0补后置依赖。完整边界见CATALOG-ADMISSION.md。尚无全语义分析/六界面走查/ReadinessGate lint闭环，E-2/U-4/FR-1/P1仍partial。

实际通过：根 `npm run build`，日志 `/tmp/keel-version-lint-build.log`，含正式类型生成、原UI边界、新lint/六组反例测试、TypeScript及Vite；脚本最终扩充spread/namespace/只读覆盖检查后，定向 `npm run check:versions --workspace=@keel/web` 六组测试和当前树扫描再次通过，日志 `/tmp/keel-version-lint-tests.log`。扩充包装标签检查时曾将Flow修订选项文本误当版本标签；修正为只读取标签自身内容、排除嵌套控件后添加回归，失败轮次不计最终通过。真实CLI临时违例探针返回exit1并含文件/行列，探针随后finally删除，证据 `/tmp/keel-version-lint-cli-probe.log`；未遗留测试源文件。无产品Go/前端运行逻辑/API/数据库更改，因此未重复Go/PG/浏览器或重发相同3000镜像。

设计引用/diff/Node语法检查通过；上一功能代码7eb6858的GitHub Actions run40106739359已实际查询completed/success。当前3000继续运行健康7eb6858，schema仍37；旧026/fabric_precheck草稿原样保留未暂存。此功能块准备单独提交推送，本次CI结果须另行查询，不能把上一提交CI成功冒充本次成功。后续按A4已具备输入推进剩余目录消费者/治理与型号能力，模板同步和后置派发依赖留所属阶段。

**§34 提交完成**：工具代码 `39fd53d` 已正常推送 `origin/dev/p1-network-bom`，GitHub Actions run40107449159实际查询in_progress，尚不宣称通过。3000实查仍健康7eb6858，本次未重新部署；两份旧草稿为仅有未跟踪修改，原样保留。


## 35. 2026-10-24 A4 全局介质管理与目录登记闭环

在 `/opt/keel`、`dev/p1-network-bom`、HEAD基线2be9126接续WP-1.10→1.11。平台管理员新增 `/admin/media` 系统入口，复用原项目介质页面/登记与文件弹窗/服务事务，提供全局登记、实际文件上传/补传、校验、独立审批、完整/Range下载与不可恢复退役；全局目录默认值弹窗链接进入该页。正式契约新增全局同构API六个操作，固定global作用域，不接受body指定scope。上传者不能自行审批，补传只限原登记人且需摘要/大小精确匹配，绑定后不可替换。未新增迁移/存储配置/分发通道。

统一HTTP作用域入口在读取上传正文前鉴权/检查CSRF及管理权限；项目Admin/SRE/Viewer不能进入任何全局管理API或页面。nil项目的直接服务下载也复核平台管理员。既有项目成员继承查看/下载批准全局制品继续有效；项目页面全局行新增只读标识且无管理按钮，跨作用域写仍拒绝，全局API不能读取或修改项目介质。项目变更刷新当前项目介质/目录查询，全局变更刷新当前用户相关缓存，未增加跨会话广播。沿用版本输入lint的原精确登记字段例外，没有扩大排除范围。

实际通过：overlay platform/httpapi race（3.247s/1.118s）及vet，日志 `/tmp/keel-global-media-race.log`；最终根npm run build，日志 `/tmp/keel-global-media-build-final.log`，含正式API类型生成、原UI边界、新版本输入lint/六组反例、TypeScript/Vite。Node/Python语法、设计和diff检查通过。Go继续用 `/tmp/keel-delivery-check/overlay.json` 排除既有026/fabric_precheck草稿，未修改它们。

完整隔离运行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-media-artifacts-integration.py`，证据 `/tmp/keel-media-qx5n14ol`：实际PG Go的mediaartifact 1.055s/platform 28.149s，通过新全局直接下载权限/继承消费及原介质/目录/BOM/ZTP等选定回归；新全局Go用例对象传输仍为testMediaObjects，不能冒充其自身MinIO传输。真实MinIO版本固定检查和全部原介质HTTP/参数HTTP/介质上传/目录执行准入/项目及全局目录管理浏览器检查通过。新增全局浏览器已走到批准、真实下载/继承与目录选项，但因语言按钮定位写成“切换为英文”而超时，整轮不记全部通过。

修正测试为现有可访问名称Switch to English后，仅定向 `... python3 scripts/check-media-artifacts-integration.py --global-media-only`，最终证据 `/tmp/keel-media-ft_r6qf5`：真实隔离PG/MinIO/API/Chromium通过全局UI上传/校验/独立审批/下载/退役、字节摘要/大小/Range、重复/非法scope/无文件/错摘要/补传原登记人/替换拒绝、全局目录默认值与登记链接、项目继承只读/可下载、Admin/SRE/Viewer越权、全局/项目交叉访问、匿名401/CSRF403、双语及退役后目录拒绝/不可恢复。此定向选项跳过Go和未变旧回归，前述证据单列。批准页截图已视觉复核。两轮隔离服务、自有测试库和临时凭据均清理，失败轮次不冒充最终通过。

上一lint代码39fd53d的GitHub Actions run40107449159已实际查询completed/success。本节提交前3000仍健康7eb6858、schema37条；准备单独提交推送后从确切SHA archive构建、候选冒烟再更新3000，实际结果后补。WP-1.10/1.11/FR-1/P1仍partial：厂商签名/离线依赖闭包、外链摄取/目标缓存和真实规模分发、报告/修订治理、全消费者/完整语义lint与后置模板同步等尚未闭环，继续按所属阶段推进。

**§35 发布完成**：代码 `fa32cbd4b6ad40d37f2ea1dc126f0c4d872d7697` 已正常推送至GitHub，从该SHA的git archive构建镜像，镜像ID `sha256:a864227552821a47f53198610b1fdae0a2d51a0e5173655c6e18c161cc4d6dfc`，未包含既有旧草稿和秘密配置。候选镜像先在回环3108使用现有受限连接验证：真实登录、原参数/型号/介质/执行/Flow五页、全局目录和全局介质页及只读API作用域、完整SHA、匿名401和控制台均通过。随后于 `2026-10-24T15:43:40.995576+00:00` 切换3000且healthy，切换后同一七页/全局API/版本/控制台检查再次通过，healthz返回完整功能代码SHA。

本次无数据库迁移，schema仍37条；保留兼容旧镜像7eb6858，未重置/恢复运行库、未变更数据卷/worker启用范围/其他服务。切换前.env、私有发布状态和七页截图在 `/home/ubuntu/.local/state/keel/releases/20261024T154157Z`，.env仍600。独立候选容器和临时配置已清理；旧026/fabric_precheck草稿原样保留，是仅有的未跟踪文件。GitHub Actions run40108146445记录时in_progress，尚不能宣称本次CI通过；上批39fd53d已success。后续文档提交不改变3000对应的上述功能SHA。


## 36. 2026-10-24 A4 介质状态的陈旧确认保护

在 `/opt/keel`、`dev/p1-network-bom`、HEAD基线f70e94d接续WP-1.10和DS §8 MediaLibrary陈旧状态要求。项目/全局状态变更共用MediaTransitionInput，新增可选expected_updated_at；交互弹窗总是提交所见版本。已有行锁内先验证管理权限及作用域，再比较版本；陈旧确认返回409 MEDIA_CONFLICT，不自动用新版本重试，不变更状态/更新时间、不添加成功审计。弹窗保留所见目标并显示明确冲突，取消时刷新当前用户查询，再按当前状态确认。旧调用省略条件仍保持原单向生命周期/独立审批/退役不可恢复。

文件补传和状态推进都严格递增微秒级updated_at，补传即便仍uploaded也使旧确认失效。修正TransitionMedia成功响应未更新UpdatedAt的问题，现在返回与持久化完全一致的时间，可直接作为下一次条件。CreatedAt、不可变摘要/对象版本和历史快照不改写；无数据库迁移。不提供修订浏览器、报告审批治理或后置真实分发等额外功能。

实际通过overlay platform/httpapi race（1.867s/1.065s）和vet，日志 `/tmp/keel-media-conflict-race.log`；根npm run build（正式生成类型/UI边界/版本lint六组反例/TypeScript/Vite），日志 `/tmp/keel-media-conflict-build.log`；Node/Python语法、设计和diff检查。Go继续使用 `/tmp/keel-delivery-check/overlay.json` 排除原026/fabric_precheck草稿，未改草稿，无库Skip不作为PG证据。

实际完整运行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-media-artifacts-integration.py`，证据 `/tmp/keel-media-vcyhfccs`，全部通过。真实隔离PG Go：mediaartifact 1.056s、platform 26.430s，含新增项目/全局双请求一成功一MEDIA_CONFLICT、补传失效、陈旧批准/退役拒绝、独立审批、返回时间与库一致、创建时间/摘要保留和失败无成功审计；该新Go用例对象传输是testMediaObjects，不冒充其自身真实MinIO传输。

同轮真实隔离MinIO/API/worker/Chromium通过原介质HTTP/参数HTTP/介质UI、实际文件/版本绑定与Range、目录依赖/冻结快照/重跑、项目与全局目录管理、全局介质生命周期及权限回归。新增双浏览器项目/全局确认测试通过：上传→已校验和已校验→已审批两种变化，旧退役弹窗均409且保留；取消刷新后新确认200；补传未改状态也拒绝旧确认；非法/零时间400、旧无条件接口兼容、退役不可恢复。media-global-stale.png已视觉复核。隔离服务/自有测试库与临时凭据已清理。新增--media-conflict-only仅用于必要时的定向复核，本轮未使用或重复整轮。

上一功能fa32cbd的GitHub Actions run40108146445已实际查询completed/success。本节提交前3000仍健康fa32cbd、schema37；准备独立提交推送后从确切SHA archive构建并候选验证后更新3000，实际结果后补。WP-1.10/1.11/FR-1/P1仍partial，剩余按依赖推进，不因本轮完善确认流程宣称全阶段闭环。

**§36 发布完成**：代码 `369223df902d46fd872b0fbea5c42c559fe98463` 已正常推送至GitHub，从该SHA的git archive构建镜像，镜像ID `sha256:fa0c7463751d85559ed9b0cf5fd6e48e703b5d0cc99e3efb512489ff544d0399`，未包含旧草稿与秘密配置。候选镜像在回环3108使用既有受限连接通过真实登录、原参数/型号/介质/执行/Flow五页与全局目录/全局介质七页、全局只读API作用域、完整版本SHA、匿名401和控制台检查。随后于 `2026-10-24T15:58:39.149086+00:00` 切换3000且healthy，切换后同套七页/API/版本/控制台检查再次通过，healthz返回完整代码SHA。

无数据库迁移，schema仍37条，保留兼容旧镜像fa32cbd；未重置或恢复运行库，未变更数据卷、worker启用范围或其他服务。.env仍600，切换前.env、私有发布状态与截图在 `/home/ubuntu/.local/state/keel/releases/20261024T155654Z`，不进Git。候选容器与临时候选配置已清理；两份旧026/fabric_precheck草稿原样保留未暂存。GitHub Actions run40109273783记录时in_progress，尚不声称本次CI通过；上一fa32cbd已success。后续文档提交不改变3000对应的上述功能代码SHA。

## 37. A4 / WP-1.12：固定型号版本的 BOM MAC 前缀校验（2026-10-24）

工作目录 `/opt/keel`，分支 `dev/p1-network-bom`；沿用§36已发布版本，不扩展后置Agent/G1/P3/P4依赖。新增可选 `bom_expect.mac_prefixes` 三字节 OUI 白名单，1–256项，大小写兼容、重复项大小写无关拒绝，空/null/通配符/不完整项400。发布与扫描草稿共用验证；OpenAPI及生成类型同步，不从导入观测反推独立规格。

逐实体包和宽表预检/提交复核复用物料写入解析的machineUID，把NIC汇总和服务器端口的永久MAC按固定服务器型号版本校验；交换机不套用服务器规则，原MAC逐口/汇总交叉检查保留。差异BLOCK携带文件/行/mac/版本/期望/实际，拒发提交令牌，提交仍重新校验。宽表保留CSV源行并继续禁止MAC推断物理NIC库存。无规则兼容旧版本；声明规则但无MAC只给未核验WARN，不冒充PASS；没有数据库迁移或历史绑定修改。

实际完成根 `npm run build`（日志 `/tmp/keel-model-mac-build.log`），补齐扫描输入正式契约后执行 `npm run check`；新增前缀校验Go用例通过。完整真实隔离测试命令 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py` 最终证据 `/tmp/keel-models-rteho1xx`：平台PG/race回归9.872s通过，含新版本不改变旧包、不同前缀版本BLOCK、SN/行/已有机器UID归属、同时改NIC汇总与逐口MAC仍BLOCK，以及有效格式令牌不能绕过提交；原型号HTTP/扫描导入HTTP、原扫描浏览器七组、完整发布/并发/权限浏览器均通过。新增真实宽表MAC差异预检、源行2、版本/期望/实际、禁用确认及CSV导出通过，截图 bom-mac-prefix-difference.png 已视觉复核。测试自动清理自有API/两测试库和临时凭据，不触碰运行库。前两轮失败来自新增夹具碰到既有rail与NIC汇总约束，修正夹具后最终整轮通过；未放宽产品门禁。

继续用既有Go overlay排除两份旧fabric_precheck/026草稿，原文件原样未暂存；本轮硬件使用独立fixture，不声明真实设备验收。上一轮369223d的GitHub Actions run40109273783已查询completed/success。本节发布结果、vet及最终检查在完成后补记。FR-14/WP-1.12/P1仍partial；GPU/其他部件、面板/角色、显式升级失效和G1等按依赖后续推进。

§37提交前检查补记：`npm run check` 与 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json go vet ./internal/platform ./internal/httpapi` 均实际退出0，日志分别 `/tmp/keel-model-mac-types.log`、`/tmp/keel-model-mac-vet.log`；设计、Node语法及diff检查通过，GitHub SSH认证成功。

**§37 发布完成**：功能代码 `8dcac8cab6b3a32465805e465fb0e9d37340f08d` 已正常推送GitHub，按确切SHA的git archive构建镜像，镜像ID `sha256:4926a439c515906ddce934ccb6f543207841221ebad268cced19677864b05360`。候选回环3108通过真实登录、原参数/型号/介质/执行/Flow与全局目录/全局介质七页、只读API作用域、匿名401、版本SHA及浏览器控制台检查；随后 `2026-10-24T16:10:54.735340+00:00` 更新3000并healthy，同套检查在3000再次通过，healthz返回上述完整代码SHA。

运行库仍37条迁移记录，无迁移、库重置或卷修改；worker启用范围和其他服务保持原状，保留兼容旧369223d镜像。.env权限600，切换前配置、私有发布状态及截图保留在 `/home/ubuntu/.local/state/keel/releases/20261024T160902Z`；候选容器和临时配置已清理。两份旧草稿仍原样未暂存。GitHub Actions run40110165418当前in_progress，本轮不声明CI通过；前轮369223d已completed/success。后续交接文档提交不改变3000所运行的功能SHA。

## 38. A4 / WP-1.12：GPU 硬件期望与 BOM 校验（2026-10-24）

继续 `/opt/keel`、`dev/p1-network-bom` 的§37，不改旧fabric_precheck/026草稿。依据PRD FR-14.1/14.3、ARCH独立期望与固定型号版本、ACCEPTANCE F-14.1/14.3，沿用已存在的 `hw_spec.gpu_count` / `hw_spec.gpu`，分别表示非负物理GPU数和正式GPU型号名称。发布/扫描草稿共用校验；拒绝null/字符串数值/负数/小数/空白或非法型号类型。OpenAPI和生成类型同步，保留其他规格字段。

服务器BOM使用绑定版本hw_spec，将SN/行号/已有机器UID归属的GPU部件与NIC分开比较。缺少或多出GPU返回model_gpu_count_mismatch，正式GPU型号身份不符返回model_gpu_type_mismatch，不相信sub_type。问题保留源文件/行、版本、期望与实际，预检不给令牌、提交事务重新校验。宽表MAC不能证明GPU库存，即便期望数量0也返回legacy_gpu_inventory_unverifiable要求逐实体清单，原CSV源行不丢失。旧版本省略规格仍兼容；只有型号但无GPU库存返回明确未核验WARN。新型号版本不自动改变旧包或ProjectAsset；不实现真实采集、G1、面板/角色迁移或显式升级，无数据库迁移。

实际验证：新增GPU解析/差异单测通过，根npm run build退出0，日志 `/tmp/keel-gpu-build.log`（含契约生成/UI边界/版本lint/TypeScript/Vite）；`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json go vet ./internal/platform ./internal/httpapi`退出0，日志 `/tmp/keel-gpu-vet.log`。设计/Node语法/diff检查通过。Go继续使用既有overlay排除未纳入交付的两份旧草稿。

真实隔离全套 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py` 最终证据 `/tmp/keel-models-hbollmt1`：PG/race平台11.703s通过，包括新增GPU正向提交及物料/固定型号落库断言、SN/行/已有UID归属、缺少/超额/伪造sub_type/新版本差异、有效格式令牌绕过拒绝、失败无已提交包；原BOM/NIC/MAC/型号不可变性/并发等回归通过。原型号HTTP/扫描导入HTTP、扫描浏览器七组、完整审核/陈旧发布/权限/退役/差异CSV浏览器均通过。GPU新增字段参与草稿去重、审核展示、发布与不可变历史检查；非法GPU规格400，宽表明确拒绝NIC/GPU推断且源行2/版本/期望/实际可见，截图 bom-hardware-difference.png 已视觉复核。第一轮新增夹具保留了optic-v1占位导致model_version_not_found，修正测试映射后最终整轮通过，未改产品门禁。隔离API/自有两库/凭据自动清理，独立fixture不冒充真实GPU硬件验证。

上一代码8dcac8c的GitHub Actions run40110165418已实际查询completed/success。§38推送/候选/3000结果后补；FR-14、WP-1.12和P1仍partial，其他部件规则、面板/角色、显式升级/失效证据和G1按既有依赖继续。

**§38 发布完成**：功能代码 `b138c6370546f29535e8f9823d284f01b0a1dc3f` 已正常推送GitHub，从确切提交的git archive构建镜像，镜像ID `sha256:f5c98ee358cadc9bf6e391930a746e07bfa2ec3dfc3e3d6528ac792cecfb3cb7`。候选3108真实登录、原参数/型号/介质/执行/Flow与全局参数/全局介质七页、只读作用域API、版本SHA、匿名401与控制台检查通过；于 `2026-10-24T23:37:29.280668+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz显示上述完整功能SHA。

未执行迁移，schema仍37条；保留兼容旧8dcac8c镜像，不改数据卷、worker启用范围或其他服务。.env仍600，切换前配置、私有发布状态和截图保留在 `/home/ubuntu/.local/state/keel/releases/20261024T233527Z`。候选容器与临时配置已清理，两份旧草稿仍原样未暂存。GitHub Actions run40143354484当前in_progress，不声明本次CI已通过；上一8dcac8c已success。交接文档后续提交不改变3000所运行的功能SHA；其他部件、面板/角色、显式升级/G1等仍按既有依赖推进，P1不签整体闭环。

## 39. A4 / WP-1.12：全局型号管理与项目只读引用（2026-10-24）

沿用 `/opt/keel`、`dev/p1-network-bom` 的§38，依PRD FR-14.1全局公共/项目私有型号与FR-14.4草稿审核，补齐 `/admin/models` 页面和 `/api/v1/admin/models` 八项管理操作：列表、身份创建、版本列表/发布、退役、草稿导入/列表/废弃。项目与全局共用原模型服务和界面组件、完整规格编辑、发布观察版本条件及事务/审计；不是另建发布流程。平台管理员专属入口及导航，双语，查询/确认按project_id或null全局范围隔离；全局变更失效当前用户私有查询以刷新共享列表。

全局管理作用域固定global。全局入口读写项目型号、项目入口写全局型号，即使平台管理员也拒绝。项目成员仍通过自己的项目读共享已发布版本；项目表内全局行明确只读且隐藏发布/退役/草稿写按钮，不暴露全局草稿。原GET /models保留全局列表，并补显式管理员保护；服务层全局列表、版本、草稿读取亦拒绝非平台管理员。原唯一性、来源草稿保护、独立审核提示、不可变历史、退役禁新引用机制复用。全局新版本/退役不自动迁移设备绑定；无数据库迁移、G1或面板/角色/显式升级实现。

实际通过最终根npm run build（`/tmp/keel-global-model-build.log`，契约/路由生成、UI边界、版本lint、TypeScript、Vite）、`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json go vet ./internal/platform ./internal/httpapi`（`/tmp/keel-global-model-vet.log`）、设计/Node语法/diff检查。Go继续overlay排除两份旧026/fabric_precheck草稿，未改原文件。无库Go测试只作为编译/单测证据，不当PG证据。

完整真实隔离 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py` 最终证据 `/tmp/keel-models-3xk9czox`：平台PG/race14.842s通过，含新增全局草稿复用/审核发布、只读权限/跨scope拒绝、同名独立身份、项目BOM实际采用全局固定版本并落库、新版本不改旧ProjectAsset、退役历史可读；原BOM/NIC/MAC/GPU/型号发布与并发回归通过。原型号HTTP/扫描HTTP/扫描浏览器七组/完整发布浏览器通过。新增全局Chromium通过UI新建/导入/审核发布/历史/陈旧确认409/退役，项目继承只读、全局/项目API隔离、无全局草稿泄露、Admin/SRE/Viewer管理UI/API拒绝、同名与非法scope、匿名401/CSRF403、双语导航。global-models-history.png已视觉复核。首轮新增Go夹具的Admin用户名与引导管理员规范化后重名，修正夹具后整轮通过，未放宽权限。自有隔离API/两库/凭据清理完毕；fixture不是硬件验收。新增--global-models-only供后续定向复核，本轮最终使用完整套件。

上一b138c63的GitHub Actions run40143354484已查询completed/success。§39提交、候选和3000发布结果后补；FR-14/WP-1.12/P1仍partial，G1、布局/角色、显式升级失效、全部硬件/规模等按各自依赖推进。

**§39 发布完成**：功能代码 `4b76052cfe7d4c76b25e4b145dd2bb998ee59b6d` 已正常推送GitHub，按确切SHA的git archive构建镜像，镜像ID `sha256:12815dc4a31dc83b792f04bb34233a2e14ca2eed8da2999aaf02ebcc5d1d7d27`，不含旧草稿/秘密配置。候选3108使用既有受限连接通过真实登录、原参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读全局作用域API、完整版本SHA、匿名401与控制台检查；于 `2026-10-24T23:51:52.242716+00:00` 更新3000并healthy，切换后同套检查再次通过。系统导航“全局型号库”可访问，healthz返回完整功能SHA。

无数据库迁移，schema仍37条，保留兼容旧b138c63镜像；不改数据卷、worker启用范围和其他服务。.env保持600，切换前配置/私有发布状态/截图在 `/home/ubuntu/.local/state/keel/releases/20261024T234951Z`；候选容器与临时候选配置已清理。两份原026/fabric_precheck草稿仍原样未暂存。GitHub Actions run40144306188当前in_progress，不声明本次CI已通过；上一b138c63已success。交接文档后续提交不改变3000运行功能SHA，WP-1.12/P1仍partial。


## 40. A4 / WP-1.12：计算托盘型号与机器 BOM 兼容（2026-10-25）

继续 `/opt/keel`、`dev/p1-network-bom`，依据PRD FR-14与A7的GB300托盘独立型号要求，修复现有compute-tray可发布却不能绑定机器BOM的问题。server/servers.csv允许server或compute-tray型号，机柜/交换机/部件仍拒绝；硬件期望读取与宽表型号解析采用同一范围。复用固定版本的GPU数量/正式型号、NIC数量/槽位/正式型号和MAC前缀校验，不由机柜聚合推断托盘数量、不固定GPU数量。新版本不修改旧绑定，提交重新检查差异。计算托盘类型的创建选择与列表补正确中英文标签。无新API、迁移、机柜层级导入或G1实现。

实际通过根npm run build（`/tmp/keel-tray-build.log`，含契约/边界/版本/TypeScript/Vite）、Go定向单测及vet（`/tmp/keel-tray-vet.log`）、设计/Node语法/diff检查。完整隔离命令 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py` 证据 `/tmp/keel-models-cwetdo5b`，PG/race17.920s通过；server/compute-tray双用例包含正向提交落库、旧版本不变、三种机器归属、错误GPU/槽位/MAC差异BLOCK、机柜/交换机拒绝及伪造令牌提交拒绝。原HTTP、扫描HTTP、扫描浏览器七组、完整发布/权限/全局型号回归全部通过。新增真实API/Chromium完成托盘创建、扫描导入、审核发布、双语类型；宽表正确识别固定托盘版本，不能证明NIC/GPU库存时仍BLOCK，原行2、版本与CSV差异保留，确认禁用。compute-tray-bom-difference.png已视觉复核。自有API/测试库/临时凭据自动清理，独立fixture不代表真实GB300验收。

沿用overlay排除两份旧fabric_precheck/026草稿，原样保留未暂存。上一4b76052的GitHub Actions run40144306188已实际查询completed/success。本轮提交和3000发布结果后补；FR-14/WP-1.12/P1仍partial，后续按依赖推进部件规则、布局/角色、显式升级与失效、G1和真实硬件验收，不向后置阶段强行扩展。

**§40 发布完成**：功能代码 `f9e4f53f181ab949aa8e61b1b43d4998c57d4c44` 已正常推送GitHub，从确切SHA的git archive构建，镜像ID `sha256:54c2394b6b63484c44d2220ee2dd78c4bc1320d95d0bf1727564f51ad1a2646b`。候选3108真实登录、参数/型号/介质/执行/Flow和全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及浏览器控制台检查通过。于 `2026-10-25T00:06:42.861866+00:00` 更新3000且healthy，同套八页/API/版本/控制台检查再次通过，healthz返回上述功能SHA。

无数据库迁移，schema实际仍37条，保留兼容旧4b76052镜像；不改运行库数据卷、worker范围或其他服务。.env仍600，切换前配置、私有发布状态与截图在 `/home/ubuntu/.local/state/keel/releases/20261025T000513Z`；候选容器与临时配置已清理，原两份草稿未暂存。GitHub Actions run40145313377记录时 in_progress/尚无结论，仅据实际状态报告；上一4b76052已completed/success。后续交接文档提交不改变3000所运行的功能SHA。FR-14/WP-1.12/P1仍partial，下一步按现有依赖推进剩余硬件/布局与角色约束。


## 41. A4 / WP-1.12：槽位标识唯一性与编辑冲突拦截（2026-10-25）

继续 `/opt/keel`、`dev/p1-network-bom`，依据PRD FR-14.1/14.3、DS 4.12d冲突拦截与既有BOM槽号/PCIe双标识解析，补齐每个版本内槽位地址唯一性。发布和扫描导入共用校验，拒绝重复PCIe位置、跨槽位的PCIe/十进制槽号别名碰撞和PCIe首尾空白；自身数字别名与01等不透明标签保持兼容，不限制同型号或同角色复用。项目与全局接口均返回400 VALIDATION_ERROR，失败不创建自动型号/草稿/版本、不消费已有草稿；旧版本和设备绑定保持不变。编辑器给双语具体标识提示，在发出请求前拦截，长表单错误获得焦点并滚入视口，保留用户输入。OpenAPI和生成类型同步；无迁移，不扩展G1/项目角色覆盖/图形面板等后置能力。

新增TestModelSlotIdentifiers覆盖正反行序、重复位置、两个方向的数字别名、空白、合法自身别名与原样字符串。实际Go单测和 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json go vet ./internal/platform ./internal/httpapi`退出0（vet日志 `/tmp/keel-slot-vet.log`）。完整隔离命令 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py` 证据 `/tmp/keel-models-lrewldsd`，PG/race18.022s及原HTTP/扫描HTTP/扫描浏览器七组/发布/托盘/全局浏览器回归全部通过。新增数据库断言非法扫描不留自动型号、原草稿与零版本保持；浏览器项目/全局发布与导入API拒绝非法地址，UI零发布请求，修正后原完整发布/陈旧编辑/历史不可变流程仍通过。

截图复核发现原长表单错误提示在视口外，补焦点定位及实际焦点/视口断言后仅重跑受影响构建和publication-only；最终证据后补。保留旧fabric_precheck/026草稿，Go继续沿用隔离overlay；独立fixture不是硬件验收。上一f9e4f53的GitHub Actions run40145313377已查询completed/success。本轮提交和3000发布结果后补；FR-14/WP-1.12/P1仍partial，继续按依赖补齐剩余硬件和布局能力。

§41提交前补记：最终根 `npm run build`退出0（`/tmp/keel-slot-build.log`，含契约生成/TypeScript/UI边界/版本lint/Vite）；仅受影响的 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py --publication-only`退出0，证据 `/tmp/keel-models-hbngcgj1`，实际错误焦点与视口断言、零发布请求、原完整审核/不可变历史/权限/差异导出回归通过。最终model-slot-conflict.png再次视觉复核，错误位于可见区域。设计/Node语法/diff检查通过，GitHub SSH认证成功，远端无待同步提交。

**§41 发布完成**：功能代码 `768965ff3c13af11c9c819c017ea80ec0280e148` 已正常推送GitHub，从确切SHA的git archive构建镜像，镜像ID `sha256:4c445cf516ee10f19463856e0a1d9c784ed7010085e588768a1977251d561618`，不含旧草稿和秘密配置。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查全部通过。于 `2026-10-25T00:22:46.691180+00:00` 更新3000并healthy，切换后同套八页/API/版本/控制台检查再次通过，healthz显示上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧f9e4f53镜像，不改数据卷、worker启用范围或其他服务。.env权限600，切换前配置、私有发布状态及截图在 `/home/ubuntu/.local/state/keel/releases/20261025T002121Z`；候选容器/临时候选配置已清理，两份旧草稿原样未暂存。GitHub Actions run40146358234记录时 in_progress/尚无结论，仅按实际状态报告；上一f9e4f53已completed/success。后续交接文档提交不改变3000功能SHA。WP-1.12/FR-14/P1仍partial，后续布局/角色、显式迁移失效及G1按各自依赖推进。


## 42. A4 / WP-1.12：独立规格面板布局契约与版本预览（2026-10-25）

一小时持续开发从00:24:05 UTC开始，工作区 `/opt/keel`，分支 `dev/p1-network-bom`。本块依据PRD FR-14.1/14.2、DS 4.12d与型号不可变版本要求，增加可选panel_layout.layout_v1：1–16行、1–64列、1–256端口，唯一端口ID/位置、边界和同版本槽位引用校验。省略该字段保留旧元数据且明确不可预览；声明后非法则扫描/发布400，不自动创建型号、草稿或版本。额外元数据保留；无数据库迁移。

共用预览组件在审核表单随输入变化、在版本历史按每版本自身布局和槽位显示登记位置、默认角色及端口详情。键盘可选端口；使用现有Button与设计令牌，无新依赖。明确型号预期而非真实链路或项目RoleBinding，不把缺失槽位自动当板载。不实现P2监控PortPanel、显式迁移失效或G1，WP-1.12/FR-14/P1仍partial。OpenAPI及生成类型同步，MODEL-PUBLICATION记录示例与边界。

实际根npm run build通过（`/tmp/keel-panel-build.log`）；Go面板校验单测和vet通过（`/tmp/keel-panel-unit.log`、`/tmp/keel-panel-vet.log`）。原生控件边界检查曾指出新按钮应复用既有Button，已按约束修正，没有改基线或放宽检查。完整隔离 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py`证据 `/tmp/keel-models-043jl6a1`，PG/race17.732s及原HTTP/扫描/发布/托盘/全局浏览器回归全通过；新增布局审核和历史预览、位置/槽位详情、非法位置/引用/空布局API和UI阻断通过。数据库覆盖非法扫描无自动身份和不可变版本变更；历史布局保留，model-panel-history.png已视觉复核。独立fixture不代表硬件验收。两份旧fabric_precheck/026草稿原样未暂存，Go继续使用既有overlay。发布结果后补。

**§42 发布完成**：功能代码 `ceeaaa8d9f4ef66b4e39d66e71b11e482066790d` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:a528a4e7f107e775e7304a4dd665e93186d3561692f7038dae526769a0c36254`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T00:33:31.076818+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:768965ff3c13af11c9c819c017ea80ec0280e148`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T003019Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40146941962记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。


## 43. B / WP-1.15 × WP-1.12：BOM硬件校验采用写入归属（2026-10-25）

持续开发第二块，沿用 `/opt/keel`、`dev/p1-network-bom`。按PRD附录A的item_uid > 包内bom_line_no > 业务码优先级，修复GPU/NIC校验另行聚合SN/行号/UID而与物料写入归属不同的问题。标准包先完成结构/引用检查和物料解析，再按解析后的machineUID对比固定型号GPU/NIC期望；复用已生成的物料列表，减少重复查机器。原MAC前缀规则已使用同一解析结果。宽表只校验独立声明的库存期望，不能由MAC模拟库存，保留原CSV源行及BLOCK。无新接口/迁移。

真实PG/race回归23.272s通过，证据 `/tmp/keel-models-pv7hp9js`，命令为既有overlay下完整check-model-workspace-integration.py。新增server/compute-tray两种夹具：第一台SN=20/行1，第二台行20且固定GPU数2；NIC与GPU按包行引用各归其主，合法预检及实际提交成功，落库明确两台GPU对应各自版本。第二台GPU型号造假仍在行22、第二台绑定版本下BLOCK，伪造令牌提交拒绝、失败无包落库。原型号/扫描/完整发布/布局/托盘/全局HTTP和浏览器回归全部通过。单测与vet退出0，日志 `/tmp/keel-identity-unit.log`、`/tmp/keel-identity-vet.log`；前端和契约没有改变，复用§42已通过的构建。Go继续overlay排除原fabric_precheck/026草稿，原样未暂存，不冒充真实硬件证据。FR-14/WP-1.12/WP-1.15/P1仍partial，发布结果后补。

**§43 发布完成**：功能代码 `c72ea6efcacf767c7550871aab1de785a6259974` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:45c9c4c20f72cda07fe8dbb4df57c1e1dd45275479d591d0391a52d618f7bc3e`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T00:39:07.984082+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:ceeaaa8d9f4ef66b4e39d66e71b11e482066790d`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T003449Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40147246983记录时 completed/success，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。


## 44. A4/B / WP-1.12：项目资产查看固定型号与历史布局（2026-10-25）

持续开发第三块，工作区与分支沿用前节。新增只读GET /api/v1/projects/{id}/assets/{assetId}/model，在同一查询中绑定项目、资产、型号及确切版本；全局共享或本项目私有scope才可读，退役不挡历史，缺失绑定/异项目/型号版本不匹配均404。不读取Machine当前型号，也不退回latest。资产列表增加model_id/model_version_id/绑定版本号；同样不暴露越scope型号。列表正式OpenAPI与前端生成类型同步，资产version与型号version分开显示。

资产页提供只读双语查看弹窗，固定版本摘要、完整规格、同版本槽位及复用面板预览；库有新版本时明确仍绑定旧版本，未绑定显示明确状态。缓存按用户/项目/资产/绑定版本与记录版本隔离，项目切换销毁弹窗状态。没有新增重绑定、门禁、G1或数据库迁移，不冒充完整P2 PortPanel监控，WP-1.12/FR-14/P1仍partial。

实际Go vet退出0（`/tmp/keel-asset-model-vet.log`）。真实PG/race20.744s通过，证据 `/tmp/keel-models-zzpgbvp5`，包含全局/私有计算托盘、资产记录v9而绑定型号v1、库v2与退役后仍取v1、槽位unused保持、未绑定及跨项目拒绝；原HTTP/扫描/发布/布局/权限回归至发布浏览器通过。首轮新增管理员夹具缺少initial标志被正确拒绝，已修正夹具；浏览器验证旧布局、Viewer/admin、双语、跨scope及匿名时发现默认Close与自有Close重复，统一为自有双语关闭入口并定向复核。最终构建/定向结果后补；没有放宽认证、权限或产品门禁。原两份草稿未暂存，继续overlay排除，fixture不代表硬件PoC。

§44提交前补记：最终根npm run build退出0（`/tmp/keel-asset-model-build.log`，含契约/TypeScript/UI边界/版本lint/Vite）；仅重跑 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py --asset-models-only`退出0，证据 `/tmp/keel-models-mynxcfzr`，Viewer/admin固定旧版本、退役历史布局、双语关闭、跨项目/匿名拒绝全部通过，asset-bound-model.png已视觉复核。没有重复跑未改变的Go回归。

**§44 发布完成**：功能代码 `f1fb61f858bc0c6bbab073ceb6c5f996af93c0ee` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:051ea0c60ed7ca373f3a888e69851472dea333165d93d47e6baf0fb4c534fc52`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T00:47:25.320294+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:c72ea6efcacf767c7550871aab1de785a6259974`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T004501Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40147904900记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。


## 45. B / WP-1.15：NIC汇总MAC按稳定部件身份比对（2026-10-25）

持续开发第四块，按附录A的引用优先级，将NIC汇总MAC与逐口永久MAC的比较移到物料身份解析后，按端口parentUID与NIC UID归属，避免业务码/行号别名误归属，也接受对已有NIC稳定UID的合法引用。格式检查仍在早期，错误或缺少逐口MAC仍BLOCK；差异给出期望/实际、源文件/原始行和型号引用。标准包与兼容宽表使用同一检查，预检及实际提交都重新验证；无迁移、接口或前端变化。

实际定向单测和vet通过（`/tmp/keel-component-mac-unit.log`、`/tmp/keel-component-mac-vet.log`）。完整隔离 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py`退出0，证据 `/tmp/keel-models-oz33rqfu`，PG/race23.489s及全部HTTP/扫描/发布/布局/固定资产版本/托盘/全局浏览器回归通过。新增server与compute-tray稳定NIC UID重导实际提交保留同一部件，汇总MAC伪造在行2显示期望/实际并阻断预检和伪造令牌提交。既有错误UID不得借用同名端口单测通过。前端未改复用§44构建，旧fabric/026草稿未暂存，fixture不作硬件证据。WP/FR-14/P1保持partial，发布后补。

**§45 发布完成**：功能代码 `4d9580d67169556f637448eefe835be407b7f4ad` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:71861d81a69a2a0b3082804b53c82c1605ae3f3bee6d0ebbe6aa2d99ff4a6b73`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T00:51:52.424863+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:f1fb61f858bc0c6bbab073ceb6c5f996af93c0ee`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T005024Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40148254614记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。


## 46. A4 / WP-1.12：基于历史型号版本修订（2026-10-25）

持续开发第五块，复用既有不可变版本发布对话框，新增从任一历史版本载入全部六组JSON及完整槽位的修订入口。显示所选来源版本/标识和观察到的库最新版本，发布才创建新版本；只草稿传source_draft_id。新版本仍受expected_version保护，不更新旧版本或设备绑定。项目私有/全局管理使用同一组件，Viewer、项目内全局共享与退役禁止修订。新增控件使用Button/TableHead/TableCell，不增加原生控件基线；无迁移/新API。

实际根npm run build通过（`/tmp/keel-model-revision-build.log`）；UI边界检查首次指出新增原生表头/单元格，已复用共享组件修正。隔离 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-model-workspace-integration.py --revision-only`退出0，证据 `/tmp/keel-models-dbdem_qa`，项目/全局从v1而非最新v2修订得v3，全部原字段、槽位unused、额外布局元数据保留，v1/v2对象不变、未传草稿ID；并发v4后陈旧编辑409且无v5，Viewer/共享/退役无入口，中英文通过，截图model-revision-project.png视觉复核。共用表单其余浏览器回归结果后补，未改Go复用§45证据。两份原草稿原样未暂存，FR-14/WP-1.12/P1仍partial。

§46提交前补记：`--browser-only`在新隔离库执行全部既有HTTP与浏览器回归退出0（`/tmp/keel-model-revision-regression.log`，Model evidence directory: /tmp/keel-models-sv4cr5p5），包含扫描草稿、完整发布/冲突/布局、资产固定版本、托盘和全局作用域。未重复未变化的Go回归；check-design.py退出0（`/tmp/keel-model-revision-design.log`）。

**§46 发布完成**：功能代码 `4f563fb3a3f20d84f07a9ee362598503fd7edcb6` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:e5c02e9ab0155adf1d96ecd4dbd67f9006a893a736283c2061f0dd61859a3763`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T00:57:11.406838+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:4d9580d67169556f637448eefe835be407b7f4ad`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T005428Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40148518113记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。


## 47. A4 / WP-1.12：型号列表按权限统计绑定资产（2026-10-25）

持续开发第六块，落实DS 4.12d列表引用设备数，明确以ProjectAsset有效绑定为口径：项目列表只计本项目，平台管理员全局列表统计全局型号在所有项目的绑定；跨版本统计，退役/旧版本仍计入。同一物理机器在不同项目各自绑定各计一次，不从Machine投影推断；缺失或型号/版本不匹配不计入。新增可选reference_count仅在列表提供，UI缺失显示未提供。OpenAPI与生成类型同步，并修正scope_key原enum仅global与项目实际响应不符。新表头/单元格使用共享组件，中文/英文提示统计范围。无迁移、新写入口或设备升级，WP/FR-14/P1仍partial。

实际根npm run build通过（`/tmp/keel-model-references-build.log`，Vite2m6s）；隔离完整model-workspace回归中PG/race23.267s已通过（`/tmp/keel-models-yw4_wigm`），新增同机器跨项目历史绑定、v1/v2、私有/共享、无效型号版本对排除、退役保留和Viewer拒绝。新增实际宽BOM提交后的API/浏览器验证通过：项目各自1、全局2且旧v1未自动升级，Viewer只见本项目计数、退役保留和双语正确。原HTTP/草稿/发布/布局回归已通过，其余原浏览器结果提交前补记。旧fabric/026草稿原样未暂存，overlay沿用。

§47提交前补记：完整model-workspace命令退出0（`/tmp/keel-model-references-integration.log`），全部原扫描/发布/布局/修订/资产/托盘/全局HTTP和浏览器回归通过；model-reference-counts.png已视觉复核。check-design.py退出0（`/tmp/keel-model-references-design.log`）。

**§47 发布完成**：功能代码 `0abd781e5d229f9517f70bb38b527e6ef69b17bc` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:6affb72ef2df857e1ed1e518614b69476304559aee538ef5a2845071b5284a46`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T01:11:29.624704+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:4f563fb3a3f20d84f07a9ee362598503fd7edcb6`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T010740Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40149400725记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。


## 48. A3 / WP-1.4、WP-1.2：执行结算读取收敛与独立容量复核（2026-10-25）

持续开发第七块。真实2,000目标检查发现每次回执应用/结算读取完整冻结目标集、所有历史attempt及完整结果；现只读取同一不可变执行快照中的Flow定义、目标数和控制字段，最新attempt按(target_id,task_id,attempt_no)选取并只投影ID/状态。保留执行行锁、租约/fencing、资源隔离、unknown显式对账、熔断与重试规则，不延长5s租约或自动重跑未知结果。无API/前端/迁移。

新增check-execution-workspace-integration.py隔离封装既有HTTP检查，默认2,000（--targets100仅dev档），随机独占测试库、私有API/worker、端口/锁检查、剥离继承KEEL配置和硬件适配器；只清理自身进程/库/临时凭据。报告源码/差异/二进制摘要、API/worker/client CPU/RSS、数据库容器、磁盘与共享网络口径，不将loopback字节当硬件传输。失败保留完整私有快照和unknown回执证据。补冻结DAG发布后仍按旧定义、失败重试不误跳过下游、失败依赖跳过、冻结目标数熔断三组。修正旧介质门禁测试漏cpu.arch导致的PARAM_ARCH_REQUIRED；没有放宽产品门禁。

本轮失败与基准均保留：首次探测TIME_WAIT占用用SO_REUSEADDR修正重启检查，仍拒绝活跃监听；/tmp/keel-execution-capacity-advlt47z在并行构建负载下有回执待对账，未算PASS。停止构建后的旧结算基准/tmp/keel-execution-capacity-h9xau0lk容量子项2,000/2,000、301.286s，但后续过期介质夹具失败，整套未通过，不把子项伪装全PASS。

实际Go execution/httpapi race退出0（/tmp/keel-settlement-unit.log），相关execution/httpapi/platform vet退出0（/tmp/keel-settlement-vet.log）。优化后--targets100隔离真实PG/API/worker共12组退出0，证据/tmp/keel-execution-capacity-wzq70hee，100目标5.62s，仅dev档、不代替2,000档；冻结DAG/重试/依赖/熔断/控制/丢回执对账/权限/参数门禁/事件审计通过。2,000档、其他执行模式与发布结果后补；CPU并行负载异常仍需后续专项故障负载验证，不声称问题全部消失或P1/硬件闭环。两份原fabric/026草稿保留未暂存，overlay沿用。

用户于本轮进行中追加一小时，持续开发窗口由00:24:05–01:24:05延长至2026-10-25 02:24:05 UTC；继续同一工作目录和约束，不把第一次时长到点当任务完成。

§48容量补记：确切功能提交f2f418582947ebcf341c60cec26d0b23893d3ac9、无已跟踪工作区差异下，隔离默认2,000档命令退出0（/tmp/keel-settlement-capacity-integration.log），证据/tmp/keel-execution-capacity-80lt42q7。2,000/2,000成功、130.464s，全部12组145.992s，worker并发8/执行并发64/租约5s/每任务10ms/seed42，与本机旧结算301.286s基准相比用时减少约56.7%，仅本次共享开发主机观测，不保证所有负载SLO或10,000×30s达标。资源报告记录4逻辑CPU/约15.4GiB内存，API峰值176848KiB、worker151452KiB、client31068KiB，worker观测CPU63.52s；完整DB/磁盘/网络/二进制摘要另见私有report。真实设备连接与硬件传输字节0，硬件适配器关闭。未知/迟到回执继续隔离且需证据对账，首轮并行构建下的失败仍保留作后续负载故障验证。

§48其他执行模式补记：真实Kea协议fixture回归退出0（/tmp/keel-settlement-kea-integration.log，/tmp/keel-shell-7qs3bw1j），规划/N-S3/已批准介质、共享worker回执、DORA及配置不确定结果隔离通过。网络配置模式回归退出0（/tmp/keel-settlement-network-integration.log，/tmp/keel-shell-w9u_7gpv），独立NH审批、共享预检、金丝雀/下发/读回、权限撤销、介质/参数退役、歧义写入UNKNOWN、SSH隔离及浏览器检查通过；协议fixture不替代真机PoC。

**§48 发布完成**：功能代码 `f2f418582947ebcf341c60cec26d0b23893d3ac9` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:3b3f3a5acb795e80f2bdcb9d989015360718f84d7732f1cdce537bb636dd664a`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T01:18:11.539385+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:0abd781e5d229f9517f70bb38b527e6ef69b17bc`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T011219Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40149709532记录时 completed/success，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

## 49. A4 / WP-1.10：固定版本制品依赖闭包（2026-10-25）

持续开发第八块。复用已批准的不可变制品与现有deps JSON，新增精确media_id+SHA256的artifacts引用，服务端固化实际名称/类型/版本/架构，不从latest或客户端显示字段推断。登记要求全部依赖已批准且已存文件，同项目或全局可见、架构相同或noarch；全局不能引用项目。最大32直接依赖/128传递依赖/16边深度，检查循环及共享子图最长路径。兼容矩阵原规则保留；参数键不接受制品引用。无迁移。

校验、独立审批、下载与执行准入检查完整闭包当前状态和固定对象实际字节；依赖退役、缺失、身份/架构或摘要不符拒绝，不输出父文件或创建成功执行。目录预检查元数据，提交边界检查字节；传入简化的父项快照也重新装载依赖事实，不漏传递依赖。锁保留至事务结束，新版本不替换旧引用，坏依赖不阻止父介质本身退役。项目/全局共享登记组件提供已批准/已存文件/架构筛选、精确摘要、去重及移除；确认弹窗列固定依赖，列表计数，双语错误保留弹窗。正式platform与execution API及生成类型同步，CatalogMedia.deps使用MediaDependencies，参数自身deps仍为CatalogDependencies。

实际根npm run build通过（/tmp/keel-media-dependency-final-build.log，23m45s发生在内存盘证据积累期）；最后契约修改后生成与类型检查退出0（/tmp/keel-media-dependency-contract-generate.log、-contract-typecheck.log）。platform/execution/httpapi vet、execution/httpapi race及check-design退出0（/tmp/keel-media-dependency-vet.log、-consumer-unit.log、-final-design.log）。隔离真实PG/race通过：/tmp/keel-media-5dcte843，mediaartifact1.055s、platform36.964s，覆盖循环、共享深路径、跨作用域/架构/未批准/不存在/摘要拒绝且不持久化父项、canonical字段、传递全局文件损坏拒绝校验/下载/执行准入、退役阻断且新版本不偷换旧边；对象传输在Go层为mock，后续真实MinIO/API/browser另验证。

最终HTTP/浏览器回归--browser-only退出0（/tmp/keel-media-dependency-browser-regression.log），覆盖新依赖项目/全局双语实际上传、确认、读取、退役阻断；原介质HTTP、参数、上传/Range/独立审批、目录依赖与执行快照、目录管理、全局介质/继承权限和陈旧确认全部通过。首次依赖测试创建全局已批准driver版本干扰旧参数夹具的精确选项断言，已通过真实退役其自建测试制品消除污染，不修改产品规则或放宽断言；初次cycle夹具因noarch->amd64先被架构校验拦截，改为同架构真实循环后通过。失败轮次保留，不能算整体PASS。截图/tmp/keel-media-l_q8td7v/media-dependencies-project.png已视觉检查。

内存问题留证：/tmp为20GiB tmpfs，积累约12GiB旧测试证据，首次全Go进程signal:killed，不算通过。将已结束且无临时凭据/活跃可执行进程的Keel证据目录转存到用户私有磁盘目录并保留原/tmp软链接，无删除证据、无改服务/数据库卷。当前会话目录在/home/ubuntu/.local/state/keel/evidence/20261025-continuous，已完成旧目录在completed-20261025。media测试封装此后将大体积可执行二进制保存在私有磁盘目录media，/tmp内保留真实凭据目录及二进制软链接，不放宽MinIO真实路径安全校验；新增--browser-only允许只重跑受影响HTTP/浏览器，--dependencies-only聚焦新闭包。Go测试以GOMEMLIMIT=768MiB约束重跑，后续结果如上。

不是签名离线仓库manifest、厂商签名、包管理源、目标缓存、重传隔离或规模分发闭环，WP-1.10/FR-1/P1保持partial。原fabric_precheck.go与026迁移草稿保留原样未暂存，所有Go命令沿用/tmp/keel-delivery-check/overlay.json。私有磁盘证据分配的最终隔离验证及3000发布结果后补。

§49收口补记：全部HTTP/浏览器证据在/tmp/keel-media-lrx6wirb。磁盘二进制分配方案的独立复核退出0（/tmp/keel-media-dependency-disk-binaries-check.log，/tmp/keel-media-1o963vof，PG/race7.886s及项目/全局双语浏览器通过）；首次把整个目录软链接到磁盘被MinIO原路径校验拒绝，已改为仅二进制放磁盘，安全路径校验保持，不将该失败算PASS。视觉复核另发现已有真实文件仍显示“校验前需要上传”旧提示，现仅缺文件时显示，并补浏览器断言；完整前端再次构建通过（/tmp/keel-media-dependency-reviewed-build.log，Vite15.70s），内存证据迁出后的资源改善不据此宣称所有负载指标达标。最后界面提示回归随后补记。

§49最后界面与封装复核退出0（/tmp/keel-media-dependency-reviewed-integration.log，/tmp/keel-media-wuvir2ez）：PG/race7.885s及更新后的项目/全局双语依赖浏览器通过，已有真实文件不再误报需上传，损坏/退役失败保留审阅对话框；Go和MinIO原有真实路径安全约束不变。

**§49 发布完成**：功能代码 `293e82cd1d9fde2c973788f7da256efdd041d35f` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:e6df76dd7012d512c9865141556e3640366349a44b2bc142eb3ccee65837fd3b`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T02:07:19.083289+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:f2f418582947ebcf341c60cec26d0b23893d3ac9`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T020510Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40153102448记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

§49并行构建负载补记：确切293e82cd1d9fde2c973788f7da256efdd041d35f、启动时已跟踪工作区干净，以GOMEMLIMIT=768MiB运行默认2,000档，同时从同一提交git archive构建发布镜像、执行候选与3000检查。证据/tmp/keel-execution-capacity-ghquq3rb（/tmp/keel-settlement-build-load-capacity.log），整套12组退出0；2,000/2,000成功164.857s，全套180.671s，开始02:05:22、结束02:08:23 UTC，确有构建/发布检查交叠。API峰172020KiB、worker151008KiB、客户端31144KiB，worker观测CPU69.40s。容量子项没有未决回执，丢确认故障组仍按预期生成unknown并通过证据对账；不是自动重跑unknown。与空载130.464s区分，不把本次未复现前次资源压力失败写成全部负载已解决，更不代替万目标30秒工作量或真机。隔离资源/凭据清理完成。

## 50. A4 收口：兼容矩阵契约一致性与退役并发验证（2026-10-25）

查实execution.openapi.yaml的CompatibilityMatrix副本落后于平台：强制requires且不含combinations/完整边界，导致跨文件生成的冻结快照类型另生schemas-*分支。以平台现有正式契约同步该副本，生成类型消除重复分叉，requires可选且关联行完整保留；不改运行中的矩阵判据。新增httpapi纯契约测试比较两份API的六个共享schema，无新依赖，复用现有yaml.v3；先实际复现不一致FAIL（/tmp/keel-catalog-contract-before.log），修正后race通过1.446s（-after.log），根npm run check生成/控件/版本lint/TS通过（-typecheck.log）。无运行代码、迁移或数据变更，不因文档/测试提交重复发布镜像；3000仍为§49功能SHA。

新增真实PG并发负例：执行准入事务验证并持有所有传递节点，另一会话尝试退役全局传递依赖被锁至请求期限；状态仍approved且无成功退役审计，随后释放准入锁，其他生命周期测试继续通过。隔离--dependencies-only退出0（/tmp/keel-media-dependency-concurrency-check.log，/tmp/keel-media-ke6_l59g），PG/race11.842s及项目/全局双语浏览器通过。仅对象传输Go层mock，PG/事务/会话与实际MinIO浏览器另外留证。

同步FR1-EXECUTION-VIEWS旧暂存说明到§22/§23已验证的真实接续状态，避免AI把初始未返回结果当当前状态；明确2,000软件负载与10,000×30s/硬件判据差别。执行容量封装接续介质封装的磁盘二进制保留，保持/tmp真实凭据路径/安全校验与私有权限，并在资源报告补实际GOMEMLIMIT、GOMAXPROCS及二进制目录；dev100验证结果后补，不替代本轮两个默认2,000档。FR-1/相关WP/P1仍partial，用户原两份草稿未改。

§50验证补记：磁盘二进制版容量封装--targets100退出0（/tmp/keel-capacity-disk-binaries-check.log，/tmp/keel-execution-capacity-yc0kjnng），100/100、4.179s，全套12组18.821s；报告记录GOMEMLIMIT=768MiB、GOMAXPROCS为Go默认和私有磁盘目录，/tmp只保留二进制软链接与小体积证据。此为封装dev档自检，不重复冒充2,000档。

另补新依赖的完整HTTP执行用例，项目和全局各由真实Flow/参数目录创建执行，检查固定依赖快照，worker完成批准模拟；依赖退役后新执行422且总数不增加，历史快照不改写。--dependencies-only退出0（/tmp/keel-media-dependency-execution-check.log，/tmp/keel-media-0_7hd3tb），PG/race12.363s与双语浏览器通过。契约一致性/当前设计引用检查均通过。§49实际功能提交293e82c的GitHub Actions run40153102448已于02:14 UTC查询为completed/success；后续测试/契约/文档提交CI另记，不冒充已通过。

## 51. 当前实施进度追踪收敛（2026-10-25）

对照本轮实际产物修正requirements-traceability的陈旧实施描述：WP-1.2已具有持久化2,000逻辑目标和资源/故障证据，WP-1.9已交付软件扇出部分及既有H-1材料，因此从planned改为partial；SSH/Agent/PXE/互信中断、所需上游联合证据与真机未具备，不改为完成，不提前签署P1。WP-1.4区分空载130.464s/构建负载164.857s与10ms模拟口径；WP-1.10移除“实际文件仍计划中/未部署”的失效描述，保留签名、离线仓库、镜像源、目标缓存与隔离分发缺口；WP-1.11同步已验证关联行、固定制品准入及共享契约检查，完整治理/其他消费者继续partial。日期更新至2026-10-25，checkpoint明确P1/A3+A4，FR-0与阶段状态不因局部产物改变。

证据引用须为文件，首次目录引用被check-design拒绝，已指向实际executions.go；最后设计/依赖/64WP/引用映射检查退出0（/tmp/keel-current-checkpoint-design.log）。§50新增httpapi vet、Python语法与浏览器脚本语法检查退出0；02a98f9已正常推送，3000仍293e82c健康。只文档进度变更，不重复构建/发布相同运行行为。

## 52. A4 平台签名介质清单与独立信任离线验签（2026-10-25）

继续在/opt/keel、dev/p1-network-bom推进WP-1.10，前提复用§49已交付的批准文件及固定依赖闭包，未补造后置Agent/PXE依赖。本步增加Ed25519平台元数据证明：项目成员含Viewer可导出本项目/继承全局，平台全局入口仅管理员；签名前再次校验根与全体传递节点当前批准、作用域/架构/ID/SHA、固定对象版本真实字节，生命周期锁持有至提交，成功才审计media.manifest.export。清单包含批准人/时间、版本/架构/摘要/固定对象版本与依赖/兼容元数据，不包含对象key、凭据或私钥。新版本不升级既有引用，退役阻止新导出但不使历史收据冒充当前准入。

keel-admin新增不依赖DB的media-signing-keygen和media-manifest-verify；私钥PKCS8、公钥PKIX、O_EXCL/600生成，加载要求当前API属主/owner-only/普通文件，拒绝最终符号链接；默认未配置时签名503，错误配置启动失败。独立可信公钥必须另外分发，不能信任清单自带指纹；离线命令分别明确metadata-only与完整<media_id>.bin长度/SHA验证，无网络/状态改写。严格schema、域分隔、重复JSON键/超深嵌套/有界闭包/错误签名与缺失文件失败关闭。Compose只读挂载仓库外私钥目录，默认空目录关闭功能；无新迁移。

共享介质页新增带目标/摘要/依赖/信任边界的审核导出对话框，实际Blob下载在现有CSP下通过，Viewer只读导出及中英文均通过。OpenAPI先行并更新生成类型，未加原生控件例外。详见MEDIA-MANIFESTS/DEPLOYMENT，平台批准证明不等同厂商签名、apt/yum离线镜像源、目标缓存或硬件分发，WP-1.10/FR-1/P1仍partial。

实际验证：/tmp/keel-manifest-integration-final.log、/tmp/keel-media-mps4sxn1，隔离真实PG/race（platform7.345s、mediamanifest1.177s）以及真实MinIO/API/浏览器退出0；测传递闭包、批准来源、固定新旧版本、损坏字节、依赖退役/失败无成功审计、Viewer/匿名/全局管理员/跨项目边界。离线CLI清除数据库环境，独立公钥、完整下载文件、错误公钥/篡改载荷/错字节均验证。临时服务/DB/私钥/凭据已清理，保留公钥和签名证据；对象传输Go层mock与实际MinIO结果分别留证。单元race、前端npm run check/build、Compose配置已通过，静态/设计检查与发布结果随后补记。用户原fabric_precheck.go与026草稿保持原样且不参与构建/提交。

§52静态补记：全仓go test -race ./...退出0（/tmp/keel-manifest-go-all.log，无DB环境的集成项依规范skip，PG证据以前述隔离运行计）；受影响包go vet退出0；设计/64WP/引用检查通过。发布私钥已在仓库外以API UID65532、目录700/文件400安装，公钥独立保存在操作员700目录；当前主机uutils install拒绝不存在账号名的数字UID，因此用root受控Python数值chown完成同一权限设置，未输出密钥。3000切换及实际CI另记。

**§52 发布完成**：功能代码 `b60eb1d82587297d81714f301b1cb08c1676750f` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:b2acda1f1f05eaacb616147210f11a8b17cd80e855a59dfdbe6b8913582b1c3e`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T02:47:33.120160+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库仍37条迁移记录；保留兼容旧镜像 `keel:293e82cd1d9fde2c973788f7da256efdd041d35f`，不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T024533Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40155675776记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

## 53. A4 固定制品有限读取重试、持久隔离与回滚边界（2026-10-25）

继续WP-1.10/FR-1.8：固定对象最多3次完整读取，100/200ms等待、请求期限约束，并关闭MinIO SDK隐含HTTP重试。3次完整读取全部摘要/长度不符才隔离；瞬时两次损坏后恢复可成功，传输故障/不完整读取/取消不当作完整源损坏。记录观测的内部错误从校验/审批/下载/签名导出/执行准入返回后，先回滚业务事务，再以最多5秒独立事务提交media.quarantined，避免回滚吞掉隔离、持有自身锁等待或执行创建留下部分行。首次422 MEDIA_DIGEST_MISMATCH，之后直接MEDIA_ARTIFACT_QUARANTINED/依赖MEDIA_DEPENDENCY_QUARANTINED，持久化失败503且不假报已隔离。

新增041完整性表，为固定绑定同事务建记录并回填现有制品；隔离证据不可改/删、ID不可改，原不可变绑定和四态生命周期保持。批准状态与隔离状态分别显示，后续源读取、签名、目录/新依赖和执行准入阻断；恢复原字节不会解除隔离，只允许退役及独立复核新版本。成功验证对完整性行持共享锁至准入事务结束；稍后的隔离写入等待该次准入，首个隔离才追加审计。ZTP现有生成脚本路径拒绝显式隔离引用，未强行补造其后置依赖。UI中英文隔离提示、禁用校验/审批、隐藏分发/导出、排除依赖选择；继承全局权限保持。

实际PG/race与协议测试：/tmp/keel-media-integrity-protocol-fixed.log、/tmp/keel-media-2gwzp7ab，Go12.789s，真实PG/MinIO/API/浏览器通过项目/全局瞬时恢复、3次GET错误、执行事务无新增行但隔离持久化、随后零GET、传递依赖只隔离源、目录/签名/新引用阻断及退役新版本恢复。协议代理仅存在一次性测试进程，转发真实MinIO并翻转指定响应一字节，不改变存储版本；此为显式协议故障注入，不是存储盘损坏/目标硬件证明。第一轮执行负例缺少cpu.arch被既有门禁拒绝，补齐测试输入后通过；无放宽产品门禁。

新增PG并发/不可变证据检查后，完整Go介质回归44.906s通过（/tmp/keel-media-integrity-full.log，/tmp/keel-media-bhurt5nh），包括实际MinIO版本绑定、生命周期、目录/兼容组合、BOM与ZTP。其HTTP/浏览器多数通过，停在旧“全局继承行不得存在任何按钮”断言：§52新增的是获授权只读清单导出，已将断言精确为允许导出、禁止校验/批准/退役；定向--global-media-only通过（/tmp/keel-media-integrity-global-regression.log，/tmp/keel-media-e0nqhotd）。全仓Go race（无DB集成skip）、受影响包vet、设计/引用检查通过；完整浏览器接续结果与发布随后补记。用户两份原草稿未改变/未构建。

§52实际CI补记：02:57 UTC查询功能b60eb1d run40155675776、交接fbcbdcc run40155853146均completed/success。3000当前仍b60eb1d，schema37，签名私钥已只读配置；本步041发布尚未执行。041之后旧镜像的严格迁移检查不兼容schema38，发布失败必须保留数据并修复前进，不自动回切旧镜像或删除隔离表/迁移记录。先备份验证/隔离恢复演练、迁移与新表授权，再候选和3000验证，实际结果另记。WP-1.10与P1保持partial，目标缓存/镜像源/目标端分发/硬件与规模仍未闭环。

§53最终验证补记：不重复未变化的昂贵Go组合，以--browser-only接续完整11组HTTP/浏览器，现包含签名和隔离新功能；退出0（/tmp/keel-media-integrity-all-browser.log，对应证据路径见日志首行），旧介质/参数、制品、关联矩阵、全局目录/介质、陈旧确认、清单与协议隔离全部通过。最终npm run check/build退出0（-final-typecheck/-final-build.log），设计检查通过。041迁移尚未对运行库应用，后续发布必须先备份/演练与授权。

**§53 发布完成**：功能代码 `974630557b2c746513746e3ce372890851668ca6` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:c8044e853018dd0ad7768cc295b1a522684f56cd35e574a3c574b627afae2410`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T03:05:15.120598+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

041迁移已应用，备份/隔离恢复与新表授权已验证；运行库38条迁移记录；保留旧镜像 `keel:b60eb1d82587297d81714f301b1cb08c1676750f`。旧镜像不兼容新增迁移，不可直接回切；须修复前进，备份恢复另走批准流程。不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T030208Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40156706937记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

## 54. A4 受控HTTP(S)来源获取与不可变存储（2026-10-25）

WP-1.10接续PRD登记外部引用，复用现有manifest_ref及§53固定绑定/完整性状态，不新增迁移。登记URL禁止凭据/查询/片段，要求已声明准确SHA/大小；POST项目/全局media/:id/fetch不接受正文或临时替换URL。复核会话/CSRF/Origin、管理作用域及原登记人，仅uploaded未绑定项能获取；继承全局不能借项目写入。操作员KEEL_MEDIA_FETCH_ORIGINS明确最多32个origin，空值关闭；HTTPS验证证书，HTTP/明确loopback仅development，DNS结果校验后直接拨号固定IP，逐跳重定向重新匹配政策且最多3跳，不转发会话凭据、不继承环境代理。link-local/loopback重绑定/云元数据地址失败关闭；关键地址核验官方资料并在代码/介质文档引用。

最多3次来源获取链、总期限5min、头64KiB、正文上限1GiB；只读到登记大小+1即可判超长，拒绝压缩/206。实际摘要/大小匹配后同事务绑定真实版本桶和完整性记录、media.upload/media.source.fetch审计（只记origin/摘要/大小/次数）。获取成功仍uploaded，继续校验与独立复核；来源文件后来改变不影响固定下载。未绑定外部错误不伪造固定源损坏/隔离。共享UI审核目标、只读来源/摘要/大小及批准边界，中英文错误保持弹窗；非原登记人/只读权限不显示获取按钮。

实际证据：/tmp/keel-media-source-final-integration.log、/tmp/keel-media-8f71ow7o，Go mediasource race1.044s/platform真实PG3.582s，受控真实HTTP→真实MinIO/API/浏览器的项目/全局完整流程退出0；同源重定向成功、源改变后固定原字节、错摘要无绑定/无隔离、禁止重定向离开origin、正文URL替换拒绝、匿名/Viewer/非原登记人/CSRF/跨项目/继承全局边界均通过。上游请求日志断言未收到Cookie/Authorization/CSRF。纯Go另测DNS固定/loopback/link-local/元数据拒绝、环境代理隔离、3跳限制和非完整/压缩响应。类型/构建和设计检查通过，完整浏览器接续及发布另记。

契约中实际scheme名为sessionCookie，补自动检查所有HTTP操作引用已声明的securitySchemes（包含path级parameters的规范解码），不以类型生成成功冒充完整契约校验。来源摄取默认关闭，未获运营来源allowlist不替用户开放外网或造供应商凭据；是受控HTTP文件获取，不是apt/yum同步、厂商签名或目标端分发/缓存，WP-1.10/P1仍partial。§53功能9746305 run40156706937和交接16d0b3f run40156996633已查询completed/success；3000当前仍9746305/schema38。两份旧草稿继续保留原样。

§54最终检查：/tmp/keel-media-source-browser-regression.log完整12组HTTP/浏览器退出0；未重复昂贵PG整组，沿用本步实际PG3.582s和§53完整Go44.906s，新增全仓go test -race ./...退出0（无DB项按规范skip，/tmp/keel-media-source-go-all.log）。受影响包vet、契约security引用、最终npm check/build、Compose/设计检查均通过。新增服务自身5分钟总期限，源正文最多读登记长度+1后拒绝超长，避免已知短文件被恶意大响应消耗。无新增迁移；运行来源origin默认仍未配置。

**§54 发布完成**：功能代码 `cfa89b653a596299444ffd7de56693793a9d76ac` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:d7bd0b9eb2c3e39a43a7b38001efa5ddc7340f1e3c2a605df0aec5dbd55004be`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T03:25:17.032318+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库38条迁移记录；保留旧镜像 `keel:974630557b2c746513746e3ce372890851668ca6`。保留兼容旧镜像。不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T032130Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40157910473记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

## 55. A4 兼容矩阵文件导入与完整审核预览（2026-10-25）

继续/opt/keel、dev/p1-network-bom的WP-1.10/1.11，补齐PRD FR-1.8/1.9已有后端兼容矩阵在介质登记/上传入口的使用能力。新增项目/全局POST media/compatibility/validate，沿用会话、Origin/CSRF和管理作用域，复用登记decodeCompatibility规则，不写介质/批准/成功登记审计。契约先行并生成类型，无迁移。

共享组件从≤64KiB JSON导入来源、版本、报告及requires/完整combinations行；无效/过大/校验中阻止提交，明确移除后才允许不带矩阵登记；AbortController+选择序号阻止迟到响应覆盖新选择。登记合并矩阵与固定制品依赖，实际服务端再次校验。校验/独立审批/签名导出弹窗和Viewer只读查看共用来源与完整关联行展示，报告不自动访问；无原生控件或版本输入例外增加。既有精确整行准入/不可变版本规则不变，示例不当作厂商或硬件验证。

§54补记：功能cfa89b6的GitHub Actions run40157910473已实际查询completed/success；交接deb68b9查询时仍in_progress。3000已为cfa89b6/schema38，来源allowlist保持未配置、摄取默认关闭。保留两份既有fabric草稿未暂存。矩阵本步测试与发布结果后补；WP-1.10/1.11、P1仍partial，目标缓存/通道分发/镜像源及硬件/规模验收未完成。

§55定向验证：/tmp/keel-media-matrix-integration-final.log、/tmp/keel-media-3he70cb3退出0，真实PG/MinIO/API/浏览器覆盖项目和全局文件导入→存储→校验→独立审批→Viewer字节/只读查看→签名清单矩阵保留。匿名、Viewer写、非成员项目、CSRF/Origin、未知字段、缺来源、重复值/行、列错位、129行及>64KiB均拒绝，预览不创建介质；损坏JSON/过大文件阻止登记，明确移除恢复，迟到旧响应不能覆盖最新选择。第一次测试把既有非成员项目的404误写为403，按既有防枚举契约修正测试后重跑通过，未放宽服务端权限。受影响httpapi/platform race和vet通过；最终npm build（含契约生成/类型/控件/版本lint）退出0，现有bundle大小提示保留；设计检查通过。完整共享页面回归与发布另记。

§55完整共享回归：/tmp/keel-media-matrix-regression.log的13组HTTP/浏览器全部退出0，包括原有参数、制品、固定依赖、关联行目录/执行冻结、全局管理、过期确认、签名离线CLI、协议故障隔离、受控来源及新矩阵导入。沿用本步定向真实数据库集成与受影响Go race/vet，不重复未改动的容量/硬件测试；准备确切提交镜像和3000候选发布。

**§55 发布完成**：功能代码 `b20b7c4b92f37ade60aad27c59a063492456908d` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:f4c111f54c3278110b0e5f296e52367a027908efbc31ff0eab9e056845391bc1`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T03:34:54.697384+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库38条迁移记录；保留旧镜像 `keel:cfa89b653a596299444ffd7de56693793a9d76ac`。保留兼容旧镜像。不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T033200Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40158562223记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

## 56. A4 离线制品归档与验签解析一致性（2026-10-25）

接续§52签名清单与已交付闭包，增加无数据库/网络的keel-admin media-bundle-create/media-bundle-verify。先验证独立信任签名，生成时逐文件计算实际写入归档的SHA/大小，600临时文件同步后不覆盖发布；已存在输出或损坏/缺失/链接/特殊文件失败不发布部分结果。输出目录要求当前操作员拥有、无组/其他写权限且最终目录非符号链接，避免不可信目录替换暂存路径。只封装已签名闭包，不引入后置Agent/PXE假设或用户目录中额外文件。

固定USTAR中manifest.json原样在前，按签名排序的ID.bin紧随。校验直接检查每个物理USTAR头，不能让通用tar.Reader自动跳过PAX/GNU扩展；拒绝路径/类型/顺序/大小/字节/额外条目、非零填充、尾块缺失及拼接/附加数据。精确长度从签名集合推导，流式SHA不全量读制品进内存；命令不解包/执行/安装。独立公钥不能从归档自建信任，历史归档不冒充实时批准。单文件/节点数沿用1GiB/129限制，完整包管理仓库/厂商签名/目标缓存与分发验收仍待后续，P1/WP-1.10保持partial。

补验签解析一致性：已存在DisallowUnknownFields会接受大小写别名，现对Envelope/Payload/Artifact/Dependency按契约精确字段名校验，拒绝别名/重复键/无效UTF-8，兼容元数据的任意业务键保持原有大小写含义；签名端复用相同JSON键/深度检查，避免生成验证端拒绝的模糊兼容元数据。不改变签名域、schema或可信密钥。

§55实际CI补记：b20b7c4 run40158562223已查询completed/success，来源交接deb68b9 run40158190235同为success；矩阵发布交接332a6cd查询时仍进行中。3000现运行b20b7c4/schema38，来源摄取仍默认关闭，两份fabric草稿未暂存。

§56实际验证：完整Go race ./...退出0（/tmp/keel-media-bundle-go-all.log，无DB项按规范skip）；定向真实PG/MinIO/API/浏览器/离线CLI退出0（/tmp/keel-media-bundle-final-integration.log、/tmp/keel-media-rjmmufqq，platform13.259s，manifest1.268s），真实下载的3节点闭包能封装/验包，已有输出不覆盖，错公钥、改字节和截断尾块拒绝，退役后仍仅为历史收据。最终目录保护修改另跑race单元/CLI编译并用上述保留真实清单/字节、仅PATH环境再次执行最终二进制封装和验证，final-bundle-cli-proof.json记录通过。当前umask0002下Go测试临时目录可组写，测试明确chmod700后满足保护约束，未放宽产品规则。

验签解析模糊测试 `GOMAXPROCS=2 go test ./internal/mediamanifest -run '^$' -fuzz '^FuzzSignedManifestParser$' -fuzztime=120s -parallel=2` 实际121.038s、1,069,175次输入、通过（/tmp/keel-media-bundle-fuzz.log）；有效输入还须在正式类型编码下验签往返一致。单元涵盖空制品、原样清单、路径/链接/PAX/乱序/大小/内容/填充/尾块、FIFO与不可信输出目录，受影响vet及设计检查通过；本步无前端实现变化，沿用§55最终前端构建和13组共享回归。

§56新增单个大制品实际证明：`--large-artifact-only` /tmp/keel-media-large-artifact.log、/tmp/keel-media-2ie40vim/large-artifact-report.json退出0；500,000,000字节SHA ff5935cae16508fe425be3f5ec80416b0951bfcefbfde1523ab2cbfb20b982b8，归档500,003,328字节。实际上传固定6.945s、校验2.493s、独立审批2.427s、完整下载4.691s、Range2.452s、签名导出2.328s、归档生成5.776s、离线验包1.645s。API进程VmHWM164,852KiB（不含MinIO/内核页缓存），观察值而非全系统上限/性能SLO。本次合成大文件结束删除，仅保留私有报告/清单；仅单制品真实PG/MinIO/HTTP/CLI，不冒充10k目标、厂商、缓存或真机验收。无迁移，发布结果另记。

**§56 发布完成**：功能代码 `3817277f67a6a73c0e11e0404073add4d0b738b4` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:6700087e0b0d252c395a339e0b989650906eb5d53bdb374f5e4e34727329cde4`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T03:49:19.951056+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库38条迁移记录；保留旧镜像 `keel:b20b7c4b92f37ade60aad27c59a063492456908d`。保留兼容旧镜像。不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T034634Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40159454541记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。


## 57. A4 介质筛选与批准来源的分页前准入（2026-10-25）

项目/全局介质GET新增契约化search/type/state/compatible_arch/stored_only，平台服务在授权scope括号内参数化筛选后统一计数/分页，追加ID稳定顺序；%/_/反斜杠是字面字符，拒绝重复参数、无效枚举/布尔/UTF-8/NUL或>120字符。保留无筛选原有全局继承元数据可见性，未把全局草稿元数据可见误当成可下载。更正§55使用文档中不准确的“批准的继承全局项”措辞；批准/字节权限仍由既有路径检查。

共享UI查询/类型/状态/适配架构/清空，改变条件回第一页；架构语义明确包含noarch。依赖选择在后端先筛approved+stored+根源非隔离+架构，再分页，缓存纳入架构、切换回第一页，已选不兼容引用保留错误。目录管理的批准来源也先筛选，修正前端catalogApprovedMedia漏过滤隔离标记的问题，免除草稿占满分页或误列隔离源的混淆。stored_only不宣称完整DAG/目标就绪，实际登记/审批/执行继续原闭包与字节校验。无迁移、无版本自由输入例外。

定向证据：/tmp/keel-media-filter-final-integration.log、/tmp/keel-media-oxjx20r3退出0，真实PG隔离根源筛除race6.219s，实际PG/MinIO/API/浏览器覆盖25草稿之前分页、字面%/_/反斜杠、版本大小写搜索、SQL注入样式零命中、准确计数/第二页、amd64/arm64/noarch、重复/非法查询、项目/全局/匿名/Viewer/跨项目拒绝、中英文及架构切换。第一次仅原生option等待可见性错误，改成等待DOM附着后通过，未改业务规则。受影响host Go race/vet与最终npm build通过；目录来源补充改动后的完整集成/发布后补。两份fabric草稿原样保留，WP/P1保持partial。

§57完整实际集成通过：/tmp/keel-media-filter-full-integration.log、/tmp/keel-media-zp8yh_60，真实PG platform race46.459s及mediaartifact/mediamanifest/mediasource通过，14组HTTP/浏览器退出0，包含全局目录批准来源的更新语义和隔离/关联行/依赖/来源/归档/矩阵全回归。沿用受影响vet、最终前端构建；无变动的容量与硬件不重复。§56精确发布镜像另在network=none、只读根文件系统、匹配操作员UID、无数据库环境下执行两条归档命令通过，/tmp/keel-media-rjmmufqq/image-bundle-proof保留证据；3817277 run40159454541及交接222a3b1 run40159720296已查询completed/success。无迁移，准备确切提交候选/3000发布。

**§57 发布完成**：功能代码 `6a534b019e18f1badc47e4b9eaef2a110308897d` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:75da877baf396ca7f9f9420aa183fb738ed8b51aec2ea11ee8800349ac09b031`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T04:01:37.221550+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库38条迁移记录；保留旧镜像 `keel:3817277f67a6a73c0e11e0404073add4d0b738b4`。保留兼容旧镜像。不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T035901Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40160202413记录时 in_progress/尚无结论，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

## 58. A4 持久隔离证据的作用域只读查看（2026-10-25）

接续§53的不可变完整性表，新增项目/全局GET media/:id/quarantine-evidence及共享只读弹窗。返回期望与末次完整失败观测SHA/长度、固定对象版本、记录时间/原因/3次失败以及当前生命周期；只查既有数据库，不读取存储或修改隔离，无迁移。项目Viewer可读本项目/继承全局，平台全局入口仍要求管理员，跨项目或未隔离404，匿名401。响应private/no-store、nosniff，无对象key/凭据/文件原文。

界面区分“期望文件”和“末次完整读取观测”，展示只读证据/恢复路线，可关闭、错误重试、中英文。不会把一次历史观测当作当前存储复检或硬件诊断，不提供解除隔离/直接重试源按钮。退役仍保留隔离证据且只改变当前生命周期字段，恢复必须新版本独立审批。实际PG/协议故障/浏览器证据和发布结果后补，WP/P1仍partial。

§58实际证据：/tmp/keel-media-quarantine-evidence-integration.log、/tmp/keel-media-udn4yuup及最终/tmp/keel-media-quarantine-evidence-review-integration.log、/tmp/keel-media-2iplabt8均退出0，真实PG platform race14.280s/manifest1.347s。受控HTTP故障注入的观测SHA与首字节翻转后的精确摘要一致，长度/固定版本/3次/时间/原因/状态均核对；读取证据和UI明确重试没有增加源GET。项目/全局、Viewer继承、匿名/非成员项目/平台入口权限、无隔离404、退役后不变记录均通过，Go另把MediaObjects设nil仍能读同一记录。前端对403刷新另用明确负例响应注入，已有缓存观测被隐藏，撤销负例并明确重试后由真实API恢复；该UI故障注入不冒充真实后台撤权，实际作用域API拒绝单独通过。

最终前端构建（契约/类型/控件/版本lint）退出0，受影响host Go race/httpapi2.306s/platform2.387s与vet、设计检查通过。复查§57还发现长度检查原在trim之后，现改为原始搜索先验证，补121空白查询拒绝；/tmp/keel-media-query-boundary-final.log、/tmp/keel-media-4scsif_q实际PG6.248s与筛选完整浏览器退出0。未重复未变容量/硬件或整组昂贵测试。准备无迁移候选和3000验证，原两草稿未暂存。

**§58 发布完成**：功能代码 `76ef23b0955ba6d98a4fbe3c2ccc776ad8248bdc` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:63f571d85fece7561a4e9a239b407cdd4ef35c2746e1f44e8af6589db336cdad`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T04:17:02.255407+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

无迁移，运行库38条迁移记录；保留旧镜像 `keel:6a534b019e18f1badc47e4b9eaef2a110308897d`。保留兼容旧镜像。不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T041048Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40160935174记录时 completed/success，不将进行中视为通过；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

§58运行账号复核：候选3108与已发布3000均使用真实会话及受限应用数据库账号检查；隔离证据路由匿名401、认证后不存在记录404、121空白原始搜索400，批准/已存储/未隔离/架构及全局目录查询200全部通过。证据为本次发布目录下media-read-probe-3108.json和media-read-probe-3000.json，未向运行库制造隔离业务记录；隔离阳性证明仍为前述一次性真实PG/MinIO环境。查询GitHub确认§57功能6a534b0/run40160202413、交接e9b65e9/run40160433457、§58功能76ef23b/run40160935174均completed/success。候选容器及临时配置已清理。

## 59. 架构编译检查与本轮恢复边界（2026-10-25）

按ARCHITECTURE §2的linux/amd64和linux/arm64覆盖要求复查发布链：现有CI仅在宿主amd64上make check/构建，不能推断arm64已构建。新增独立CI步骤，以CGO_ENABLED=0、GOOS=linux、GOARCH=arm64编译api/admin/worker/sim四个控制面程序；临时输出目录由mktemp独立创建并在步骤退出时清理，环境不传播至后续amd64 API/Compose检查。不引入新依赖或改变业务代码/数据库/运行镜像。

本地实际执行同一四程序go build，使用既有overlay排除两份原草稿，退出0；逐个检查ELF64/e_machine=183，全部为AArch64。构建日志/tmp/keel-media-arm64-build.log、私有产物与SHA256证明/home/ubuntu/.local/state/keel/evidence/arm64-20261025/build-proof.json。CI YAML解析、提取步骤bash -n及diff检查通过。这是arm64交叉编译证明，不是arm64运行、Agent双架构、多架构容器发布或真机验收；本次不为CI/文档变更重建3000。CI实际执行结论后补。

发布后私有配置证明在§58发布目录post-release-config-check.json：API healthy/镜像SHA一致、.env600、签名目录只读且私钥配置正确、来源allowlist空、worker未运行、候选已删除均通过。下一次恢复首先读本文件顶部入口，再按A3/WP-1.4→1.5现有内核接通服务器SSH契约，不把当前介质归档当作已完成目标缓存/安装。P1/FR-0维持partial。

§59两小时最终交接（2026-10-25T04:28:27.488095+00:00）：本轮持续窗口始于2026-10-25 02:28:11 UTC，现已超过04:28:11 UTC。工作区仍/opt/keel、dev/p1-network-bom；功能76ef23b的run40160935174、交接da13662的run40161443174及arm64 CI增量936b7a0的run40161575505均实际查询completed/success，后者包含新增arm64步骤及原完整流程。3000保持76ef23b/schema38；最后交接文档提交不会重建服务，其新CI结论须单独查询，不从前述成功外推。私钥权限复核通过（宿主700目录的属主隔离使开发用户不能直接检查内部文件，改以容器API用户仅stat元数据确认400/UID65532，未读取/输出私钥）；证据signer-permissions-check.json。15份非空日志存在性索引two-hour-evidence-index.json仅证明证据保留，实际测试结论仍以各节明确结果为准。原两草稿未暂存；平台阶段与真机/规模剩余验收未升级。

## 60. A3 受限服务器SSH任务、持久回执和对账（2026-10-25）

继续/opt/keel、dev/p1-network-bom，沿现有Execution/outbox/TaskAttempt/资源租约增加SSH mode与稳定machine目标；042只扩展mode/kind/runner约束，无新秘密表/执行队列。正式API支持Admin/DE/DL/SRE创建host.identity或host.wait，只接受已有provisioned项目资产、established互信和操作员精确绑定/主机密钥。快照冻结资产版本与无秘密绑定摘要，claim/打开SSH前复核当前权限/互信；角色或绑定改变暂停待派发。只读任务共用device:machine/ssh:machine共享锁，独占设备锁仍冲突。

TaskCommand版本keel.task/v1；SSH只发送常量helper入口与受限stdin JSON。配置/Ed25519域私钥为当前用户私有普通文件，拒绝root账号/UID0 helper、未知或变更host key、任意shell/来源地址/凭据请求。按目标复用连接，握手上限5秒/请求35秒，慢目标不串行阻塞其他目标，取消可中断握手。目标helper按次运行，在工作前持久化accepted，再原子持久化running/终态；已有相同命令只读原记录，accepted/running也不重放。取消须有终态证明，丢响应只查询，拿不到则保留unknown及待对账租约。授权ssh-receipt API验证真实终态后复用原对账事务，相同幂等key重试不再次连接/执行。

已有执行工作台支持SSH角色控制、只读边界说明与结构化输出；本步启动入口为正式API，未新增目标选择启动向导。Compose有仓库外只读配置挂载和空禁用占位，默认不启用SSH或生产worker；helper随镜像/Makefile/arm64 CI构建，目标端由操作员受控安装，不自动修改客户账号/authorized_keys。详细契约、配置与限制见SSH-RUNNER.md。bootstrap/repair、完整脚本/Agent双通道、缓存/分发/真机及完整P1仍partial。

实际证据：/tmp/keel-ssh-integration-reviewed.log与/tmp/keel-ssh-integration-2l11fil0通过真实PG+loopback SSH/HTTP，覆盖角色、CSRF/匿名/非成员、禁用配置、重复提交、回执恢复幂等、共享/独占锁、撤权/撤互信；/tmp/keel-ssh-integration-browser.log与/tmp/keel-ssh-integration-hgl7lmk0增加真实浏览器登录/SSH详情/输出无错误及独立CLI进程run/query/重复run回执完全一致。测试资产provisioned/互信为显式软件fixture，不冒充PXE、真实首登或硬件验收。首轮仅测试指针/值编译错误，修正测试后通过；未放松业务规则。最后协议版本/连接池取消修正的实际结果随后补记。

全仓Go race（无数据库项按规范skip，/tmp/keel-ssh-all-go.log）、受影响vet、前端正式契约生成/类型/UI版本lint与构建（/tmp/keel-ssh-frontend-build.log，保留现有bundle提示）、Compose配置、设计检查通过。042尚未迁移运行库，3000此时仍76ef23b/schema38；发布需先备份/恢复演练，保留原两草稿未暂存。

§60最终验证补记：/tmp/keel-ssh-admission-final.log、/tmp/keel-ssh-integration-u2qrh1mg退出0，含最终协议版本/连接池并行与握手取消、严格指纹和机器ID验证后的真实PG+SSH+HTTP+浏览器、独立helper进程证明。完整overlay make check退出0（/tmp/keel-ssh-final-check.log），设计首次发现需求追踪中的裸SSH-RUNNER.md路径，改为docs/SSH-RUNNER.md后检查通过；未为此重跑设备/容量测试。四个原控制面程序及新增helper已交叉编译arm64（/tmp/keel-ssh-arm64.log）；仅编译，不宣称arm64运行。042运行库迁移与正式发布结果随后记录。

**§60 发布完成**：功能代码 `b946f3446091123d3edcb0a86b73d588ba9e1d48` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:df9575d5cc8d681ed9785a9f874a796f970c4c6d81c561d0246d9cfcc8c8dfb6`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T05:32:28.714899+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

042约束迁移已应用，schema38→39备份/隔离恢复与既有授权已验证；运行库39条迁移记录；保留旧镜像 `keel:76ef23b0955ba6d98a4fbe3c2ccc776ad8248bdc`。旧镜像不兼容新增迁移，不可直接回切；须修复前进，备份恢复另走批准流程。不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T052751Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40165569843已 completed/success；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

§60发布后补证：现有Kea/网络协议、审批撤销、共享执行器unknown/隔离租约及真实浏览器回归退出0，日志 `/tmp/keel-ssh-existing-network-regression.log`，证据 `/tmp/keel-shell-kky9z_3u`；临时数据库、API及测试凭据已清理。3108及3000应用角色接口检查均通过，包括SSH匿名401/CSRF403/默认禁用503、既有介质查询与隔离证据权限，未创建业务记录，日志 `/tmp/keel-ssh-candidate-app-role.log`、`/tmp/keel-ssh-3000-app-role.log`。042备份格式检查、隔离恢复、迁移及业务行数保持证据在本次私有发布目录 migration-rehearsal.json；生产应用与既有应用角色授权检查见 migration-applied.json。功能CI完整成功，生产worker未启动、SSH配置仍为空。

## 61. 四agent开发后按用户要求累计10分钟暂停（2026-10-25）

开始06:08:35 UTC，用户改为06:18:35 UTC暂停。root负责Execution/集成，另三agent分别负责SSH凭据协议、信任请求领域/API、资产页与执行UI。唯一工作区/分支延续，原fabric Go/026 SQL草稿保持未跟踪。此节记录尚未提交和未发布的源码；运行3000仍b946f34/schema39，生产SSH和worker继续关闭。

已落盘：凭据引用版本与UTC微秒有效期、到期连接隔离、只读trust.verify；043包含不可变单目标限时请求/独立批准/撤销/一次性凭据消费索引及legacy/sim/ssh信任来源；普通SSH拒绝模拟或legacy互信，旧mock不能修改当前/历史真实目标，legacy/sim established可经批准repair补真实验证。execute复用原Execution/outbox/权威资源锁/fencing，真实终态证明与当前权限/资产/绑定/有效期通过才原子投影信任。补稳定目标消费锁、审批主体Share锁与Admin/DL/SRE执行控制；UI可创建/批准/执行/撤销、分页查看与证据链接，中英/来源/错误处理遵循既有组件。

已实际通过：纯传输race（/tmp/keel-ssh-credential-protocol.log，2.301s）、独立真实PG领域race（/tmp/keel-ssh-trust-domain-final-pg.log，4.880s）、前端正式类型tsc（/tmp/keel-ssh-trust-ui-types.log）、设计检查。统一集成首轮因测试试图重复初始化管理员失败，改为普通创建测试管理员；第二轮实际暴露创建后修改不可变execution snapshot的问题，已改为INSERT前冻结完整snapshot，保留不可变保护。第三轮统一PG/SSH/HTTP/浏览器及最终前端构建/受影响vet结果随后补记，未完成的项目不能记PASS。

未做：043运行库迁移、备份恢复演练、完整make check/功能CI、提交推送或3000发布；不得直接部署工作区（含原草稿）。目标公钥/helper安装、真实密码首登、Vault销毁、目标密钥撤销、Agent/真机PoC及完整P1仍未完成。临时凭据到期/绑定轮换后的unknown保持隔离，恢复治理待开发，不能称完整bootstrap/repair或凭据全生命周期闭环。恢复时先检查此轮最后日志和git diff，再补并发消费、撤权/未知恢复等必要集成负例，验证完整后显式纳入本轮文件提交，从确切提交归档构建，做043备份/恢复/授权后才发布。

§61暂停前补证：第三轮 `/tmp/keel-trust-integration-third.log`、`/tmp/keel-ssh-integration-4wquauct` 已退出0，真实PG+SSH+HTTP协议及两账号浏览器通过（新信任批准→Execution→终态→source=ssh，禁止自批/mock旁路，错误host pin真实409、中英UI、实际证据链接），Go race 13.222s，跨进程CLI回执亦通过。临时数据库和凭据已清理。最终前端构建 `/tmp/keel-trust-frontend-final-build.log` 退出0，保留现有bundle体积提示；受影响vet `/tmp/keel-trust-final-vet.log`退出0，设计/diff检查通过。最后一次make check采用65秒上限以遵守用户暂停时间，结果见 `/tmp/keel-trust-pause-check.log`，若被超时终止不得算通过。源码均保持未暂存，043未应用，尚未提交/推送/发布。本轮暂停不等于相关WP完成。

§61最终暂停检查：限时make check在06:18 UTC以124退出，Go全仓vet/race与前端类型/UI/版本检查已通过，第二次Vite构建transforming阶段达到65秒上限而终止，不能记完整make check通过；独立最终前端构建已通过并有单独证据。所有测试脚本已结束，自有临时数据库/凭据已清理，三子agent均已停止。HEAD仍41cc39c，原两草稿和本轮源码均未暂存；3000实测仍b946f34 healthy，schema39及生产配置未修改。恢复优先补信任执行并发消费、第三执行者降权、unknown回执对账等专门集成负例，再完成检查与发布流程。

## 62. 续开发：独立批准的SSH首连验证

延续§61与原四agent职责，未重写既有执行器或越过Agent/PXE依赖。域凭据元数据绑定版本/到期时间，一次性授权冻结单目标/资产版本/可信指纹/凭据引用及有效期，独立Admin/DL批准后由Admin/DL/SRE通过原Execution发起只读trust.verify。真实回执落库时复核当前审批和执行主体权限、项目/资产/绑定/时间窗口，原子更新trust与source=ssh；资产变化即使远端返回成功也拒绝建立信任。普通SSH拒绝legacy/sim来源，旧mock不能改当前或历史真实目标。

补齐同目标消费advisory锁/已消费version唯一索引、审批主体共享锁及第三执行者降权复核，信任执行控制排除DE，未派发取消不悬挂consumed请求。资产页完整创建/独立批准/执行/撤销/分页与证据链接；真实failed/unknown保留原Execution且不自动重跑。列表/写入权限拒绝会清空已打开目标弹窗，执行概览/输出/关联/回执拒绝会隐藏旧缓存，精确到期与确认提交均检查有效期。

实际验证：完整overlay make check `/tmp/keel-trust-resume-check.log`退出0，包含全仓vet/race、正式契约生成/类型/UI/版本检查与前端构建（现有bundle提示保留）。统一真实PG+回环SSH+HTTP+浏览器 `/tmp/keel-trust-resume-integration.log`、`/tmp/keel-ssh-integration-lou_vbtq`退出0，taskrunner race27.205s；独立helper CLI进程仍复用完全相同持久回执。覆盖并发两个批准请求同版本仅一个执行/一次run、第三执行者SRE→DE在claim及transport前拒绝、实际丢响应unknown/隔离锁、无回执不能人工宣称成功、原终态授权对账幂等恢复且不二次run、真实Load凭据轮换保留unknown。自然到期传输由真实SSH短期限测试覆盖；DB授权到期场景为明确expiredRunner故障注入，不伪称等待5分钟自然授权过期。

浏览器两真实账号验证禁止自批/独立批准、错误host pin真实409、中英、实际success/failed/unknown及证据链接；列表403为明确前端故障注入，撤销注入后从真实API刷新恢复，不冒称实际后台撤权。实际后台审批人/第三执行者撤权由独立PG事务测试证明，域并发race4.206s `/tmp/keel-ssh-trust-domain-review-pg.log`，含审批等待行锁时角色撤销与approve/revoke仅一次提交。原Kea/网络共享内核及真实配置页回归 `/tmp/keel-trust-network-regression.log`、`/tmp/keel-shell-_qk31uab`退出0，自有临时数据库/API/MinIO/凭据均已清理。

043尚未应用运行库，发布先做schema39→40备份/隔离恢复及新表应用角色SELECT/INSERT/UPDATE授权，不授予证据DELETE，保留审计/迁移限制。worker和真实SSH仍默认关闭。此交付仅验证已由运维安装的平台公钥/helper；无公钥注入、root首登、目标密钥撤销、Vault销毁或Agent。legacy/sim established可独立批准补真验证，revoked不自动恢复；真实SSH信任变更/完整repair/轮换及到期换凭据查询旧unknown的恢复治理仍待开发。WP-1.4/1.5、FR-1、P1及FR-0均保持partial。功能提交、CI、镜像、迁移与3000结果随后按实际记录。

**§62 发布完成**：功能代码 `efaf0ac43af1d46dab46af3d1f698c15e7069848` 已正常推送GitHub，确切SHA的git archive构建镜像 `sha256:0309f4c12b5e1fd4127387d2b3fc67bd7a6699e31b73e8bc053829ff6dac3299`。候选3108真实登录、参数/型号/介质/执行/Flow与全局型号/目录/介质八页、只读作用域API、匿名401、完整版本及控制台检查通过。于 `2026-10-25T06:31:19.467291+00:00` 更新3000并healthy，切换后同套检查再次通过，healthz返回上述功能SHA。

043信任请求/来源迁移已应用，schema39→40备份/隔离恢复、既有身份保持与新表受限授权已验证；运行库40条迁移记录；保留旧镜像 `keel:b946f3446091123d3edcb0a86b73d588ba9e1d48`。旧镜像不兼容新增迁移，不可直接回切；须修复前进，备份恢复另走批准流程。不改数据卷、worker范围或其他服务。.env仍600，私有发布状态、切换前配置和截图在 `/home/ubuntu/.local/state/keel/releases/20261025T062805Z`，候选容器/临时配置已清理。两份旧草稿保留未暂存。GitHub Actions run40169168724已completed/success；后续交接文档提交不改3000功能SHA。相关WP及P1仍按剩余验收项保持partial。

§62生产复验：`/tmp/keel-trust-candidate-app-role.log`、`/tmp/keel-trust-3000-app-role.log`均退出0；候选/3000使用受限应用角色查询新信任表和资产来源，新资产页显示正确，无浏览器错误；信任创建接口匿名401/CSRF403/配置缺失503，无业务记录创建。迁移演练与应用证据在私有发布目录 migration-rehearsal.json / migration-applied.json，包含新表受限授权与既有审计限制；生产SSH与worker未开启。


## 63. 两小时续开发：加密项目域凭据前置

按A3/WP-1.5及WP-1.6依赖接续§62，先补公钥安装/轮换所需的加密域密钥提供者，不提前假定PXE、Agent或完整repair存在。新增AES-256-GCM密文存储与独立32字节主密钥文件，AAD绑定项目/引用/版本/公钥/到期；私钥只在控制面内存解密，领域快照、API、页面及审计仅保留公开身份。044新表版本不可覆盖/删除，唯一允许首次控制面撤销；创建/撤销与CLI操作员审计原子提交。服务器owner CLI提供master-keygen及ssh-key-create/list/revoke，运行角色只读新表。

SSH目标可显式选择vault提供者，仍保留原文件兼容；精确版本/有效期/公钥不符、数据库不可用、撤销或主密钥错误均拒绝，无文件/sim回退。每次Binding/元数据/操作前复核，拒绝关闭对应目标的池连接；业务事务与凭据读取分离连接池防止自锁。撤销阻止后续取用，不伪造目标机删除公钥证明；已派发结果遵循原回执与unknown隔离语义。公开密钥API/资产列表按当前平台Admin或项目Admin/DL/SRE授权，中英/分页/到期/控制面撤销及403隐藏旧缓存均接入既有组件。

已实际通过加密/文件边界race、真实PG密钥不可变/跨项目/并发幂等撤销/审计/只读运行角色验证（`/tmp/keel-domain-key-check-76tf1c1f`）；后续HTTP验证发现只读事务使用FOR SHARE，已改为只读角色查询。完整真实PG/SSH/HTTP/原双账号审批/新公钥浏览器集成`/tmp/keel-domain-ssh-complete.log`、`/tmp/keel-ssh-integration-icpc5tjd`退出0，覆盖活动池撤销、重启拒绝、unknown保留两把隔离锁、业务池耗尽仍可取钥、五项目角色和实际撤权会话失效；页面中英/390宽/精确公钥及明确注入403后实际刷新恢复通过。完整overlay make check `/tmp/keel-domain-key-full-check.log`退出0，保留现有bundle体积提示。独立CLI进程与最终变更复核、提交/CI/044备份恢复演练及3000发布结果随后按实际补记。

边界：无目标公钥/helper安装、root密码首登、目标双钥轮换/删除、新连接拒绝证明、Vault密文销毁、主密钥轮换或Agent；本步不闭环S-3/F-1.3/WP-1.5/WP-1.6/P1。当前3000仍运行§62功能SHA，生产worker/SSH关闭，044尚未应用；两份原草稿不纳入提交/构建。后续优先补受控公钥安装与真实broken/repair，不另建执行队列。


§63最终增量验证：独立CLI发现首次撤销返回纳秒而DB为微秒，已统一创建/撤销时间精度；定向真实PG/SSH/HTTP/公钥浏览器与独立CLI进程 `/tmp/keel-domain-cli-retry-final.log`、`/tmp/keel-ssh-integration-vvsygfw6`退出0，覆盖master排他创建/原文件保持、公开输出、版本拒绝复用、撤销精确重试响应一致，以及helper跨进程回执保持。最终域PG+vault传输复验 `/tmp/keel-domain-final-pg.log`、`/tmp/keel-domain-key-check-4dijv8ya`退出0。一次并行Vite构建替换dist导致浏览器找不到登录控件，已按构建结束后串行复验，不计该失败为通过；运行库未受影响。配置/设计/diff检查通过。

§63发布：功能提交`336ad9e0fb6fe71defd74bd85116e0b26982275a`已推送，确切提交归档镜像`sha256:644556e4557ef65eca7fba8820fd6520641595cf480b7f65844e506d97c115fb`于`2026-10-25T07:10:04.991318+00:00`发布3000。044已应用，schema40→41备份/隔离恢复演练、新表仅SELECT授权、业务行数/原信任来源保持及审计限制通过。候选和3000八页登录冒烟、新公钥列表/资产页、应用角色API/匿名401/CSRF403/禁用503全部通过，未创建运行环境密钥或业务记录。证据`/home/ubuntu/.local/state/keel/releases/20261025T070606Z`，生产SSH/worker/主密钥提供者均未启用。旧schema40镜像不可直接回切，保留数据修复前进；备份恢复另走批准流程。GitHub功能CI run40171516201已completed/success。两份原草稿仍保留。两小时开发继续下一项受控公钥安装/新钥实际首连验证。


## 64. 两小时续开发：受控公钥安装与新钥首连

接续§63，A3/WP-1.5复用已具备的加密域钥、独立批准请求与统一Execution内核，增加可选install_key_id及不可变公开快照（045）。仅经独立批准后，通过运维临时非root凭据和已安装helper执行trust.install，在当前账号authorized_keys同inode追加强制helper/restrict公钥；持久归属记录、flock、过期/路径/UID/权限/symlink/hardlink/客户同钥边界防止重复认领或覆盖客户内容。并发追加不由旧快照覆盖，未知落盘保持unknown。

建立真实信任必须新域钥全新SSH握手并只查询同一安装回执，再在当前授权/资产/期限与域钥行锁下最终投影。新钥拒绝时unknown/两把租约保留；恢复只查询原命令，不能二次安装；新钥证明后控制面撤销竞争阻止信任投影，不伪造目标回滚。资产页提供公开选钥、明确安装边界与原双人批准/执行流程。普通任务仍不允许任意安装。

实际验证：文件系统及保留预打开客户追加FD测试 `/tmp/keel-key-install-append-files.log`；完整真实PG/SSH/HTTP/原有与新双账号审批浏览器/独立CLI-helper进程 `/tmp/keel-key-install-full-integration.log`、`/tmp/keel-ssh-integration-1zmg2eav`退出0；包含安装前撤销零派发、证明后撤销拒绝建立信任、新钥拒绝后原回执恢复。完整overlay make check `/tmp/keel-key-install-make-check.log`通过（现有bundle提示保留）。无真机操作或生产密钥生成，生产SSH/worker/主密钥提供者仍关闭。提交/CI、045备份恢复演练及3000发布随后按实际补记。

边界：运维仍须准备非root临时登录、host pin和helper；不实现root首登、账号创建、PXE、旧钥删除/完整轮换/目标撤销证明、Vault销毁或Agent。普通任务后续需运维将目标绑定切为新vault版本，不自动覆盖配置。schema41→42后旧镜像不能直接回切；两份原fabric草稿继续保留。WP-1.5/1.7、S-3、P1和FR-0保持partial。


§64发布：功能`b60c0f9413e70111af50ae2e4712c50e4f42b473`已推送GitHub，确切提交归档镜像`sha256:e159f2f4f1a9423683f6e2775fb5ae0f63f062b5787c871588f4907acfb7dc2e`于`2026-10-25T07:26:45.638655+00:00`发布3000。schema41→42备份/隔离恢复、原业务计数和host pin/source保持、安装快照触发器与既有应用/审计授权验证通过。候选及3000八页真实登录、新钥列表/资产页/API、匿名401/CSRF403/禁用503通过，无运行环境业务写入。证据`/home/ubuntu/.local/state/keel/releases/20261025T072335Z`，`/tmp/keel-install-3000-smoke.log`与`/tmp/keel-install-3000-read.log`退出0。演练首次因镜像未就绪中止；第二次发现隔离恢复明确不恢复ACL，已在演练库重新建立既有受限授权后通过。原运行库未受失败演练影响。生产SSH/worker/主密钥提供者仍关闭，旧schema41镜像不可直接回切。功能CI run40172594664已实际核验completed/success。


## 65. P1 WP-1.14：可选 OIDC 组织登录

在本地认证/会话/项目权限前置已具备后独立推进，不把LDAP或PXE当作FR-0前置。新增受保护provider文件与只读Compose挂载、真实发现/授权码/S256 PKCE/state/nonce、签名issuer/audience/expiry/azp与可选at_hash校验；subject显式绑定已存在本地账号，IdP email/角色不建号或授权。成功沿用现有DB会话，锁定预会话与当前启用用户、轮换Cookie、脱敏审计及当前security_version。无密钥/token进入API、审计、浏览器存储或Git。登录页按配置显示provider名称，后端回调只把通用结果交给前端/auth/callback，再按当前权限检查next。

真实PG/回环签名IdP/HTTP与Chromium `/tmp/keel-oidc-first-integration.log`、`/tmp/keel-oidc-integration-69s5jfu2`退出0，包括合法PKCE往返、错误签名/nonce/issuer/audience/azp/到期/未绑定、外浏览器与重放、cross-site、重复state、Cookie轮换、本地非Admin角色、退出与pending期间停用；页面中文/英文390宽/无token持久化及原本地登录均通过。state到期和4096上限/清理亦有race测试。全量检查、最终增强负例和发布结果随后补记。此时3000仍为§64，OIDC生产配置未启用。

边界：待完成授权状态在单进程内存，重启后重新发起；多副本需同一往返固定路由，尚无跨副本状态。配置修改需重启，现有会话紧急撤权仍用本地用户停用/安全版本。LDAP、外部组织IdP实际接入及完整F-10.5/S-6/U-12仍未闭环；WP-1.14/P1保持partial。详见OIDC.md。无新数据库迁移；两份原fabric草稿保持未跟踪。


§65最终验证：`/tmp/keel-oidc-final-integration.log`、`/tmp/keel-oidc-integration-wkypx7tx`退出0，增加PKCE错误挑战拒绝、8并发回调仅一次成功、错误/取消及回调issuer不符立即消耗state、真实来源IP限流和超限不再写审计；原浏览器全链仍通过。完整overlay make check `/tmp/keel-oidc-make-check.log`退出0，最终相关Go vet `/tmp/keel-oidc-final-vet.log`、Compose/设计/diff检查通过。无新数据库迁移；生产OIDC配置仍未启用。


§65发布：功能`43bb7d6f2b8ed9b05caf64ce475de8fd11fc2d5e`已正常推送GitHub，确切归档镜像`sha256:54c87ad380ae0a7b00e41e081d2d83b64bc47221929726aaa4a6b79e342cc597`于`2026-10-25T07:39:36.484137+00:00`发布3000。无新迁移，schema42保持；OIDC生产未配置、SSH/worker/主密钥提供者仍关闭。首次候选八页均可打开但Chromium记录ERR_NETWORK_CHANGED，因此该次检查失败；发布命令缺少遇错停止，仍发生切换，此门禁缺陷已修正为候选证据匹配版本后才切换。网络稳定后候选及3000八页/受限API完整串行重验均退出0：`/tmp/keel-oidc-candidate-recheck.log`、`/tmp/keel-oidc-candidate-read-recheck.log`、`/tmp/keel-oidc-3000-recheck.log`、`/tmp/keel-oidc-3000-read-recheck.log`。原失败不记为通过；私有证据`/home/ubuntu/.local/state/keel/releases/20261025T073728Z`。候选容器/临时配置随后清理，无业务记录创建。CI状态需按功能SHA独立核验。两小时继续WP-1.14的LDAP后端及组织账号入口。


## 66. P1 WP-1.14：LDAP 组织账号

接续§65，在本地账号/会话/权限前置上增加可选受信LDAPS后端。受保护配置显式绑定本地规范用户名与固定DN，拒绝同一DN分配两个本地账号；无目录搜索、自动建号、组/角色导入、服务账号密码或明文/匿名Bind。实际系统/自定义CA与主机名验证、最低TLS1.2及5秒请求/取消边界；目录口令不保存。复用原Login事务/CSRF/Origin/会话轮换/安全版本/审计，Bind后重验预会话期限；未绑定/错误口令统一401，目录故障503不回退或记账号口令锁，账号/IP锁跨本地/LDAP共用，LDAP HTTP入口另有来源请求上限。登录页共享NativeSelect/Label原语提供组织/本地选择，方式变化清空口令。

真实TLS/LDAPv3 Bind/PG/HTTP/浏览器初验 `/tmp/keel-ldap-first-integration.log`、`/tmp/keel-ldap-integration-kmpjo4on`退出0；增加同DN拒绝和本地停用实际LDAP会话后最终 `/tmp/keel-ldap-final-integration.log`、`/tmp/keel-ldap-integration-ztv5qh90`退出0。覆盖不可信CA/主机名不符/明文/空口令/无映射/取消/断连、CSRF零Bind、无本地兜底、503恢复、Bind期间预会话到期、Cookie/当前非Admin角色/退出、共享锁不能换方式绕过和停用拒绝派发。实际Chromium中英390宽、错误和切换清口令、组织与本地成功/退出、无跨源口令或持久化通过。

原本地认证/控制面在新建独立PG库回归 `/tmp/keel-auth-provider-regression-rqanre3a/local-and-control.log`退出0（19.123s）；OIDC完整签名/PKCE/PG/HTTP/浏览器回归 `/tmp/keel-ldap-oidc-regression.log`、`/tmp/keel-oidc-integration-y9kvrrxi`退出0。完整overlay make check `/tmp/keel-ldap-make-check.log`通过，最终Go vet `/tmp/keel-ldap-final-vet.log`、设计/Compose/diff检查通过。共享UI检查曾拒绝新增native input/label，已改用现有NativeSelect/Label，不放宽规则。最后增加provider撤去时清口令，已重新构建，浏览器与提交/CI/3000按后续实测补记。

边界：目录侧停用只拒绝新Bind，不主动撤销既有Keel会话；即时撤权用现有本地停用/安全版本/会话机制。当前只做固定DN LDAPS Bind，未接客户OpenLDAP/AD/外部组织IdP真实环境，不能称完整F-10.5/S-6/U-12或P1完成。生产LDAP/OIDC仍关闭，无迁移、无生产账号/配置/秘密生成；两份旧fabric草稿继续未跟踪。详见LDAP.md。


§66发布：功能`cd5dc87cdbb86d63ccc61a48a98ec9f3ae5bdfd1`已正常推送GitHub，确切归档镜像`sha256:c76393b5a7ee563692a97de702392fb879dbed03b4a047cfbb46cc51d7a3a1e0`于`2026-10-25T07:53:24.722127+00:00`发布3000。候选八页/受限API通过并生成确切版本证据后才切换，修正后的门禁已生效。3000同套复验 `/tmp/keel-ldap-3000-smoke.log`、`/tmp/keel-ldap-3000-read.log`退出0；公开providers仅local，OIDC数组空/LDAP名空，生产未启用组织认证或真实SSH/worker/主密钥提供者。无新迁移，schema42保持；无生产账号或业务记录新增。最后浏览器/PG验证 `/tmp/keel-ldap-last-integration.log`、`/tmp/keel-ldap-integration-aqjdh2zq`退出0。证据`/home/ubuntu/.local/state/keel/releases/20261025T075131Z`。CI状态独立记录，不以本地检查代替。

功能`43bb7d6f2b8ed9b05caf64ce475de8fd11fc2d5e`的GitHub CI run40173471981已核验completed/success。


## 67. A3/WP-1.5：安装后的普通SSH域钥绑定

代码核查发现§64虽然要求运维切到新vault凭据，但普通SSH仅核验已建立信任和host pin，仍可能使用原临时登录配置；已按PRD受控初始化边界修正。046以新钥实际证明事务在Machine持久记录公开域钥身份，历史资产保持null不补造证明；已证明身份不可清空。普通任务创建/派发、已有域钥的只读repair批准/执行/最终投影均检查当前项目、精确密钥版本/公钥/有效期/撤销及运维正常配置真正选择该vault提供者。临时文件凭据或复制引用标签均拒绝，运维显式切换后才可普通执行；不自动改配置/删除目标钥。

定向真实PG/SSH `/tmp/keel-domain-binding-integration.log`、`/tmp/keel-ssh-integration-7l6gidve`退出0，证明安装身份持久化、拒绝清空、旧临时/伪标签文件凭据零派发、切vault后新钥实际握手普通host.identity，以及排队后撤销/后续准入拒绝。完整真实PG/SSH/HTTP/双人安装与原有浏览器/CLI-helper独立进程 `/tmp/keel-domain-binding-full-integration.log`、`/tmp/keel-ssh-integration-xf43ti5y`退出0。最终全量检查、046备份恢复演练、提交/CI和3000结果后续按实测补记。

当前3000仍§66/schema42；046未应用，真实SSH/worker/主密钥提供者和组织认证均关闭。完整root首登/PXE/双钥轮换删除/目标撤销证明/Vault销毁/Agent仍不具备，相关WP/P1/FR-0继续partial。两份原fabric草稿不暂存/构建。


§67最终全量检查 `/tmp/keel-domain-binding-make-check.log`退出0（Go vet/race、正式契约、UI/types/build；保留现有bundle提示），设计/diff检查通过。§65 OIDC功能CI run40173471981及§66 LDAP功能CI run40174367537均已核验completed/success。


§67发布：功能`c346d232bcf5caab504fa00e3d60dc5fe93eaa46`已正常推送，确切归档镜像`sha256:0ed80cec3d4b5c3c90203b9fb39e48247d7c0c678535ab69039e78aafd929af9`于`2026-10-25T08:07:16.616676+00:00`发布3000。046备份/隔离恢复及正式应用通过，schema42→43，旧业务行数/host pin/source保持；历史域钥身份均null，未补造证明，不可清空触发器与受限授权已核验。候选八页/受限API通过并有匹配版本证据后切换；3000同套复验 `/tmp/keel-binding-3000-smoke.log`、`/tmp/keel-binding-3000-read.log`退出0。证据`/home/ubuntu/.local/state/keel/releases/20261025T080253Z`。生产SSH/worker/主密钥提供者及OIDC/LDAP仍关闭；旧schema42镜像不可直接回切。功能GitHub CI run40175093458已核验completed/success；两份原fabric草稿保持。


## 68. A3/WP-1.5：资产页普通只读SSH启动

在§67实际域钥绑定前置上，资产页新增项目Admin/DE/DL/SRE可见的只读执行入口。显式选择当前页已provisioned且真实ssh信任目标，最多32台；仅host.identity/host.wait及100～30000ms超时，不接受shell、目标地址或私钥。复用正式SSHCreateInput契约、CSRF与统一Execution/outbox/租约/回执，不新增执行器。相同参数在当前表单网络重试复用同一幂等键，成功进入既有Execution详情；401/403/404关闭过期表单，未启用或域钥绑定不足有明确提示。

定向真实PG/HTTP/回环SSH/Chromium与独立CLI/helper `/tmp/keel-ssh-launch-dialog-final.log`、`/tmp/keel-ssh-integration-tb3wgo20`退出0：先真实受控安装并新钥证明、运维显式切vault，再以真实项目DE发起identity/wait；第一次POST实际成功后明确注入响应丢失，重试返回原Execution且目标只运行一次。页面403为明确UI故障注入，真实Viewer无入口；中英、390px弹窗内无溢出、长目标标识换行及无JS错误通过。工作区依DS保留1280px最低宽，未将其横向滚动当作移动适配缺陷。首次长按钮导致弹窗溢出已修复；首次全页无横滚断言不符DS，已改核验弹窗视口和内容边界。新增CI使用独立keel_test_ssh_ui库运行同一真实流程。

完整回归/全量检查、提交/CI与3000发布按后续实际补记；生产SSH/worker/组织认证仍关闭，无真机或运行库任务。WP/P1/FR-0仍partial，root首登、PXE、Agent、完整双钥轮换/目标撤销等缺口未改变。两份原fabric草稿保留。


§68完整真实PG/SSH/HTTP、所有既有信任/域钥/双人安装和新增只读启动浏览器、独立CLI/helper进程 `/tmp/keel-ssh-launch-full-integration.log`、`/tmp/keel-ssh-integration-ene4iobu`退出0，Go race 55.724s。全量检查进行中；未将明确注入的响应丢失/页面403记为真实环境故障。


§68最终overlay make check `/tmp/keel-ssh-launch-make-check.log`退出0，包含Go vet/race、正式API类型生成、共享UI边界/版本选择、前端类型/生产构建；现有bundle提示保留。最终异步卸载防护后定向真实PG/SSH/HTTP/浏览器与独立CLI `/tmp/keel-ssh-launch-last-integration.log`、`/tmp/keel-ssh-integration-vlcxprwu`退出0。设计/diff检查通过；SSH文档入口已同步现有受控安装和已安装域钥强制绑定边界。


§68发布：功能`d20899486e1220ec016f1fbfc1e0c058471fd514`已正常推送，确切提交归档镜像`sha256:1b3821ff8745921497bcd2b932ec8f2fab2b63fc75b0b0b82cfca18d559d94a6`于`2026-10-25T08:19:24.042661+00:00`发布3000。无新迁移，schema43保持；候选八页和受限API通过且版本证据匹配后才切换。3000同套复验 `/tmp/keel-launch-3000-smoke.log`、`/tmp/keel-launch-3000-read.log`退出0；私有证据`/home/ubuntu/.local/state/keel/releases/20261025T081802Z`。生产SSH/worker/主密钥提供者/外部介质获取/OIDC/LDAP仍关闭，未创建运行环境任务或密钥。CI状态随后按确切SHA核验。两小时开发继续只读repair域钥绑定的安全回归。


## 69. 已安装域钥的只读repair安全回归

新增真实PG/SSH用例 `TestSSHInstalledDomainReadOnlyRepairBoundaries`，不改变运行协议：实际受控安装和新钥证明后才明确注入broken状态；创建、批准、执行均拒绝复制域标签的临时文件钥，恢复精确vault后原批准流程可继续。正例实际trust.verify成功保留原公开域身份；排队后改回临时凭据零派发，实际远端终态后撤销域钥使修复失败且不投影互信。三组均核验原域身份未替换/清空。`/tmp/keel-domain-repair-first.log`、`/tmp/keel-ssh-integration-c49594yq`退出0（race 6.723s），包含独立CLI/helper进程；相关Go vet及race `/tmp/keel-domain-repair-race.log`退出0。此项为前批安全边界补充证据，不声明完整repair/轮换或H-1。3000仍运行§68功能版本，无新迁移。


§68功能GitHub CI run40176069375已核验completed/success。§69测试提交2a67c0c的CI run40176336228失败：新增文件按注册顺序先建管理员，暴露原TestSSHExecutionPostgresProtocolAndHTTP强制首次初始化的隐含顺序依赖。完整本地复现 `/tmp/keel-repair-all-ci-repro.log`、`/tmp/keel-ssh-integration-yd5231qz`失败同一administrator already initialized，不记作通过；已改为在现有隔离库创建独立Admin，并补拒绝派发后真实lease到期/unknown隔离。后续完整回归与CI结果按实测补记。ab36da7为失败日志注解诊断，未修改运行代码。


§69顺序依赖修复后完整真实PG/SSH/HTTP/全部既有及新增浏览器、独立CLI/helper `/tmp/keel-repair-order-fixed-full.log`、`/tmp/keel-ssh-integration-afmy6umt`退出0（race 57.350s）；相关Go vet通过。派发前改回临时凭据后，实际lease到期进入unknown并保留device/SSH两把隔离租约，未制造stop回执。旧失败保留，不以单独用例通过代替完整顺序回归。


## 70. P1 H-1执行包纠偏：缺证据拒绝，不授予真机PASS

后续验收入口核查发现原H-1脚本仍用不支持的Bearer Token、root任意命令和accept-new指纹，缺平台读回时能退化SSH探测，缺PXE仍可selfcheck成功；与当前Cookie/keel-ctrl/固定pin/统一Execution边界不一致。已移除该自动真机驱动，改为明确的离线证据一致性校验，不连接任何平台或硬件，也不持有认证材料。十台HGX B300的冻结BOM、完整资产/成功bootstrap请求、确切发布health、独立Ed25519主机公钥、实际host.identity快照与终态command/attempt/fence、UTC窗口及每台boot/key-preservation文件均须在位并一致；sim/legacy、repair替代首次引导、截断分页、跨项目、指纹/hostname/回执不符均拒绝。

新poc/h1提供模板和标准库校验器，逐级openat/O_NOFOLLOW拒绝路径越界/目录链接/特殊文件、单件4MiB限制、严格JSON属性和文件SHA256。poc/selfcheck.sh仅跑明确合成fixture，不证明平台/PXE就绪；run-h1.sh缺参数exit2，缺证据exit1，完整一致只输出READY_FOR_REVIEW，hardware_acceptance始终UNVERIFIED。更新环境/采集/人工审查清单及WP-1.9追踪，不自动更新ACCEPTANCE。历史p1-selfcheck.json保留但明确不作当前证据。

`sh poc/selfcheck.sh`八组测试通过，证据 `/tmp/keel-h1-selfcheck-final.log`；覆盖完整合成包只请求审查、缺引导/自动互信/机器/摘要、sim/跨项目/分页/自批、回执/主机名/指纹/fence/时序、遍历/父子symlink/FIFO/超大文件/重复JSON、版本不符、CLI脱敏失败及空模板。空模板实际 `/tmp/keel-h1-unfilled-template.log`BLOCKED/exit1；sh语法、py_compile、设计/diff通过；同一标准库用例纳入make check/CI。无运行代码/迁移改变，3000仍d208994/schema43，无生产任务/硬件操作。完整WP-1.7自动装机注册未具备，H-1/相关WP/P1仍partial。提交/CI随后按确切SHA补记。


§70版本与资产身份增量：992b79e进一步拒绝缺稳定资产ID、缺版本或布尔版本；`/tmp/keel-h1-identities-selfcheck.log`八组自检退出0。c241edb及992b79e已推送，未改3000运行镜像。

CI诊断包装复核：ab36da7加入tee时未显式pipefail，可能掩盖make check退出码，该包装缺陷不能用于宣称检查通过。99f9625已显式set -euo pipefail；受控失败子命令退出17通过tee后仍退出17，证据 `/tmp/keel-ci-pipefail-proof.txt`。修正前CI不作为最终通过门禁；最终以修正后确切SHA结果另记。原2a67c0c失败与本地完整复现保留，相关测试顺序已由9bee8a2修正。


下一实施单元按IMPLEMENTATION-PLAN A3/WP-1.5及ARCHITECTURE §6推进Agent注册基础：短期一次性enrollment token绑定既有项目/机器，先验证重放/错项目拒绝，再签发mTLS、注册和心跳；后续任务仍复用Execution/TaskAttempt/租约/fencing/持久回执。开发时使用隔离软件设备与明确运维预配，不假定尚未交付的PXE/cloud-init或完整旧钥删除已存在；不跳过注册身份边界，也不另建执行队列。SSH root首登/完整轮换与WP-1.7依然列为独立未完成项，不借H-1包修正提前签署。


最终本地全量检查：99f9625当前代码下 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB make check` 已退出0，证据 `/tmp/keel-twohour-final-make-check.log`，含八组H-1校验器测试、Go vet/race、正式契约、共享UI、类型及前端生产构建，现有bundle体积提示保留。此前完整实际PG/SSH/HTTP/所有浏览器回归以§69最终57.350s记录为准；无新运行逻辑变化，不额外重复生产发布。最终GitHub门禁仍待99f9625/run40177456628完成，不以未显式pipefail的中间CI成功代替。


最终GitHub门禁：功能/测试/执行包及显式pipefail均在`99f9625bb2d22eb6e6b99f8e863f4ed3e01f4db5`下，run40177456628已实际核验completed/success，覆盖全量检查、真实认证/浏览器、域钥SSH启动、OIDC/LDAP以及Compose容器构建/启动/健康。确切版本证据 `/tmp/keel-twohour-final-ci-proof.json`。3000继续运行已验证发布功能d208994，后续提交仅测试/执行包/CI/交接，无新运行逻辑或数据库迁移；不为文档重建镜像。原草稿保持未跟踪，真实SSH/worker/组织认证/外部介质获取仍关闭，P1/FR-0及相关缺口保持partial。


## 71. A3/WP-1.5：Agent 一次性注册、mTLS 与连接心跳

接续§70明确的下一实施单元，消费既有项目、Machine 与 provisioned ProjectAsset；运维显式预配隔离软件目标，不假定PXE/cloud-init或G3安装已经可用。新增正式protobuf契约、047及持久公开身份/连接表、受保护本地Ed25519 CA/CSR、owner CLI签发/取消一次性令牌与版本化撤销、可选独立gRPC listener及Linux amd64/arm64 keel-agent。数据库仅保存带域隔离的令牌摘要和公开证书；令牌只落新0600文件，Agent私钥在目标生成并在注册前持久化，不进入控制面、日志或Git。

注册锁定令牌、重查项目/机器/SN/资产版本和CA，原子消费并审计，重放/错绑定/过期/取消均拒绝；证书仅clientAuth、固定URI SAN、一小时有效期。mTLS必须匹配已登记DER摘要；序号/boot ID/流内身份不可变，每次心跳和每2秒复核当前证书/资产。owner撤销中断已有流，不等TLS重连；活动连接保留，过期连接租约后才接新connection ID，旧连接不能续写。客户端采用真实Linux boot ID、2分钟流上限和临时错误退避，无自动注册/任务重放。应用角色可消费令牌、登记证书及连接，但无签发/撤销/删除身份记录权限，列级UPDATE只满足行锁，触发器阻断无撤销信息的证书改写。

实际证据：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB python3 scripts/check-agent-integration.py` 第三轮退出0，目录 `/tmp/keel-agent-integration-mz9h7uk8`，含秘密路径/CA/CSR边界、真实PG/TLS/gRPC八场景、实际keel-admin/keel-agent进程和受限SQL角色。注册并发消费仅一次，匿名/同CA未登记证书拒绝，重复序号/身份变化拒绝，在线撤销与幂等审计，CLI秘密零输出、防覆盖、交接失败取消均通过。前两轮失败定位为测试DSN将options空格编码成“+role”，现以独立role启动参数修正后实测；未放宽业务权限。CA/CSR专包race退出0 `/tmp/keel-agent-pki-tests.log`；双架构静态构建、amd64 version、Compose默认关闭及proto固定工具逐字节重现通过 `/tmp/keel-agent-build-wiring.log`。CI增加独立Agent数据库实际测试，不以缺环境Skip替代验证。

全量make check、提交/CI和3000发布状态在后续补记。在此记录时3000仍§70的d208994/schema43，047尚未应用。生产Agent listener/worker/SSH/主密钥提供者/组织认证保持关闭，不生成生产CA/token，不向物理设备注册。原两份fabric草稿保持未跟踪、不暂存不构建。

边界：仅身份与连接存活，不提供任务下发、遥测/CollectPolicy、文件分发、Agent自动安装/systemd、证书自动续签或CA双根轮换。版本是声明值；心跳不投影SSH/BMC互信、硬件健康或PXE/G3成功。过期或响应丢失按AGENT-IDENTITY的owner复核/撤销/新grant与新state目录恢复，不盲目重试。WP-1.5/F-1.2/S-3/P1与FR-0仍partial。下一步在该身份上接既有Execution/TaskAttempt/outbox/资源租约/fencing/持久回执的有界只读任务及断线对账；不另建执行队列，不顺带补未具备的FR-5.7安装链条。


§71最终本地验证：全量 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB make check` 已退出0（`/tmp/keel-agent-make-check.log`，含Go vet/race、正式契约/UI检查、类型及前端生产构建，已有大bundle提示保留）。独立审查进一步修正临时DB错误被映射成永久身份拒绝的问题：非sql.ErrNoRows的DB错误保留并返回Unavailable，当前流立即终止但可退避恢复；真实撤销/绑定失效仍PermissionDenied，注册仍禁止自动重试。最终专用runner `/tmp/keel-agent-final-integration.log`、`/tmp/keel-agent-integration-1bcmu4xy` 已退出0，新增第九个真实证书行锁超时场景验证流Unavailable且释放锁后原证书仍有效，其余八场景及受限角色全通过。修改后的相关包vet退出0（`/tmp/keel-agent-final-vet.log`）。后续只补发布证据，不重复已通过且不受影响的前端验证。


§71发布补记（2026-10-25 10:58 UTC）：功能提交7670144及可选只读Compose接入8ee27c5已推送；完整发布SHA `8ee27c51bb1e38df2ec183f3a1ab60bd24d41587` 的GitHub Actions [run40186128595](https://github.com/enzoOvOyu/keel/actions/runs/40186128595) completed/success，包含真实Agent PG/TLS/gRPC/CLI/列级权限测试、双架构、既有认证/SSH/OIDC/LDAP浏览器及容器健康检查。精确git archive镜像为 `sha256:3361da00bf74a8d8703935aca79f0d54ad34e4b34b213d41bd22b2d78b50759f`，未纳入两份原fabric草稿；镜像内Agent version实际核验一致。

发布证据目录 `/home/ubuntu/.local/state/keel/releases/20261025T104612Z`：pre047.dump（304242字节）已验证格式并在独立恢复库完整恢复，保留原ACL后执行047、核查业务行数/SSH身份/审计限制和新增最小授权；恢复库仅清理自有副本。运行库迁移43→44，三张Agent表均空、未生成生产CA/token。候选3108八页/公开域钥/真实受限应用角色读探针通过，再切3000；3000相同复验及镜像/health精确SHA通过，Agent/SSH/主密钥/介质获取/OIDC/LDAP保持关闭。候选容器和其临时env已移除，旧镜像、数据库卷、备份及回退资料保留，运行.env保持600。数据库迁移后旧schema43镜像不能直接回切，恢复不自动执行。

用户最新追加的shadcn/ui剩余整合和全局等宽字体调整已排入下一次前端改动，记录于当前恢复入口及DEVELOPMENT §2；本轮没有修改字体或组件。当前代码下一实施单元仍为既有Agent身份上的统一Runner只读任务/持久回执及断线对账，前端一旦涉及则先执行上述用户要求。完整P1/FR-0、Agent安装/续签/遥测和真机验收依然不闭环。

## 72. Agent有界只读任务与断线回执恢复（2026-10-25）

- 接续§71，WP-1.5通过mTLS双向Tasks流接入既有Execution/outbox/TaskAttempt；仅host.identity/host.wait，初始入口为owner侧CLI且须本地平台Admin真实登录，退出撤销临时会话。没有新增执行队列或前端入口。
- 任务池和调度器位于同一worker；无本进程连接的worker不可借数据库在线标记领取。平台/Agent两侧tasks_enabled显式开启；普通API身份listener不派发。Compose可选配置只读挂载，生产默认关闭。
- 本地持久接收/运行/终态、全局fence及单机flock复用既有Wrapper；同command重投只查询，旧fence拒绝，最高fence缺回执保持unknown。Agent和SSH共享稳定机器资源锁，Agent自身另有独占串行租约。
- 断线进入unknown/reconciling并保留资源；重连只查询原回执，worker保存真实终态后由当前有权限操作员显式对账。原提交者被禁用仍允许受信worker查询旧任务；凭据撤销与新回执入库同事务串行，撤销前已存历史证明可幂等消费。
- 实际验证：make check通过；最终go vet ./...与go test -race ./...通过（宿主均带既有overlay/GOMEMLIMIT）。真实PG/TLS/gRPC/三个CLI集成最终证据/tmp/keel-agent-integration-5ygxqroi；受限角色任务流验证/tmp/keel-agent-integration-vewa0d45；SSH实际协议回归/tmp/keel-ssh-integration-ig0ek0h1。双架构构建结果见/tmp/keel-agent-tasks-dualarch.log。上述均为隔离软件验证，无真机/生产CA/令牌。
- 048仅扩展既有执行模式，049仅增加连接任务流元数据；发布前须完成备份恢复演练和CI，未完成前3000仍是§71镜像/schema44。两份原fabric草稿保留、不编译、不提交。
- WP-1.5/FR-1/P1仍partial：完整任务目录、媒体传输、自动安装/systemd、续签/CA轮换、遥测、完整bootstrap/repair与硬件PoC仍待后续。下次前端改动须先完成shadcn/ui剩余整合与全局等宽字体。

§72发布补记：功能提交`f02858cb30f6a70072895d2fdd3453b2eea3e6de`已推送，[GitHub CI/run40188674535](https://github.com/enzoOvOyu/keel/actions/runs/40188674535)全部成功。发布证据目录`/home/ubuntu/.local/state/keel/releases/20261025T112336Z`，pre048049.dump为321895字节；同PG独立克隆恢复并由schema44升级46，业务记录/SSH身份/受限角色/审计限制核验通过，克隆已删除。准确提交归档镜像为`sha256:edfe985b9c1d22f7c02b3ec3a600568e5c4db9b46c29ca8185d4f6e9c1914c51`，未纳入原fabric草稿；运行3000已切换，最终3000八页登录工作台、公共域钥和权限只读探针均通过，runtime-verified.json已记录实际结果；候选容器/临时环境文件已移除。生产worker/Agent listener/SSH/主密钥/OIDC/LDAP/外部介质fetch继续关闭，Agent生产三表仍空。保留旧镜像、原卷与备份，schema44镜像不能直接回切。

## 73. 旧页面共享控件收尾与全局等宽字体（2026-10-25）

所属WP-1.1/1.8，接续§23和用户§72排期。输入为已验证的16个自有shadcn基础组件、既有API/表单、DESIGN-SYSTEM令牌；不依赖未来业务。旧业务button/input/textarea/select/option/label/table族全部迁移，ui-legacy-elements.json实测files为空，预算收紧至零。NativeCheckbox保留原生表单/键盘/onChange；Table新增scrollContainer=false保留已有滚动区与虚拟布局。保持权限、事件、字段和禁用守卫；本轮没有把手写业务抽屉等同完整无障碍验收。

按用户要求统一到本地JetBrains Mono v2.304四字重400/500/600/700，固定官方提交cd5227bd1f61dff3bbd6c814ceaf7ffd95e947d9，WOFF2/OFL/SHA256随应用；全局令牌、Monaco、DDM导出及HTML预加载一致，中文系统回退。未声称是RunPod字体。登录密码留白和项目切换器220/180/110px宽度已复核修正。

实际本地证据：最终前端生成/版本输入守卫/TypeScript/Vite构建退出0，/tmp/keel-ui-preload-final-build.log；设计检查和四个字体SHA256核验通过。共享组件/明暗/忙碌Escape/焦点恢复通过/tmp/keel-shadcn-ba87r_f_；型号实际API/PG/浏览器全组通过/tmp/keel-models-35xsmcih；Flow真实发布/历史/回滚/Monaco和执行入口通过/tmp/keel-flows-985gz_pj；执行视图/矩阵/五Tab/SSE通过/tmp/keel-views-g7_vucf1；网络配置通过/tmp/keel-shell-5qsi93ax；ZTP收尾通过/tmp/keel-shell-s4r1u1c8。介质实际MinIO/API/浏览器各用例均输出PASS，/tmp/keel-media-8_en10_o；总进程退出143，不将该总运行标为退出0，保留各组证据。

新增UI专用runner使用自有随机keel_test_ui库/API3109，验证实际登录/项目表单、14工作台、checkbox键盘与16px尺寸、弹窗焦点、本地字体四字重/等宽、明暗/移动端。开发中修正测试对空项目健康/交付阻断的误判、懒加载等待及控件中文名称；字体资源断言实际发现旧Intel preload并已修正；共享组件首轮受并行Vite重建干扰后在稳定构建通过，不能用失败轮冒充成功。最终总结果及GitHub/3000发布状态随后按实际补记。

本轮无Go业务改动、无迁移/生产写入；schema46保持，两份原fabric草稿继续未跟踪且不构建。WP-1.8全部UX、WP-1.5/P1与FR-0仍partial。Agent完整任务/安装/续签、PXE、完整repair和真机验证不因UI整合而记完成。

§73最终本地补记：新增UI全链最终退出0，/tmp/keel-ui-tj9usmy8，覆盖14页面/登录项目表单/复选框键盘及16px/焦点恢复/本地四字重/等宽/明暗/移动端，确认没有旧Intel字体请求。先前一轮Chromium在并行检查期间内存紧张崩溃，不计通过；仅将本轮已结束的自有测试二进制移至磁盘证据目录，日志和截图保留，UI runner后续直接在该目录构建，串行复验通过。Fabric真实协议/页面及8-tab抽屉/tmp/keel-shell-2feg40hi、规则编辑/版本/发布/tmp/keel-shell-9ys7cnbf均退出0。共享UI及业务页面测试不是完整无障碍或硬件验收。

§73移动登录修正：首功能提交0a87a9e的CI/run40194760988在认证浏览器阶段失败，未切换3000。本地对其候选镜像运行原scripts/check-browser.mjs复现移动输入字号13px低于16px；已恢复用户名/密码16px（含md覆盖）并在新UI回归增加该断言。修正后最终前端构建退出0，实际UI全链加原disabled-provider/双语/明暗/移动登录脚本均退出0，证据/tmp/keel-ui-pzp0lc4d；候选旧容器及临时env已移除，失败CI不计通过。随后以修正提交重新验证CI与发布。

§73发布补记：完整修正提交`e5f540ea82e177d72cf27782d7d082cb5514f5ba`已推送，[CI/run40195483027](https://github.com/enzoOvOyu/keel/actions/runs/40195483027) completed/success（含新增UI/字体回归、原登录/认证/SSH/OIDC/LDAP及容器健康）。准确归档镜像`sha256:076737fa627fa6665168222f9fdc9fb8b22898b6b40030eaec13d81087628a55`已发布3000，发布目录`/home/ubuntu/.local/state/keel/releases/20261025T125818Z`保留source.tar、构建日志、candidate/runtime健康和真实登录八页工作台/权限读探针、四个WOFF2逐字节摘要、移动字段16px/48px高度/密码56px留白/显示隐藏/明暗截图。候选及运行版均退出0；候选容器和临时环境文件已移除。

无新迁移，运行库保持46；生产worker/Agent listener/真实SSH/主密钥/OIDC/LDAP/外部介质fetch继续关闭，Agent生产三表仍空；旧镜像/卷/备份保留。原fabric草稿仍未跟踪、不构建、不提交。仅本轮已结束测试二进制从内存/tmp迁往磁盘证据，日志截图保留；不清理用户其他目录/服务。后续可在已完成共享UI基础上继续既有统一执行/Agent工作，完整业务能力和P1仍按实际证据保持partial。


## 74. Agent 只读任务 API、资产页启动与真实回执对账（2026-10-25）

接续§72/73，所属WP-1.5及WP-1.8。新增默认关闭的KEEL_AGENT_TASKS_ENABLED，认证项目成员可分页读取当前有效Agent目标的公共身份/连接元数据，Admin/DE/DL/SRE可提交host.identity/host.wait。API与owner CLI在同一创建事务复验当前资产/SN/证书及新鲜任务连接，无跨连接嵌套准入；API无任务Broker/CA/listener，worker仍只消费自身真实mTLS流，复用原Execution/outbox/TaskAttempt/资源租约/fencing及审计。无新迁移。

资产页复用shadcn控件/现有字体，显示任务通道连接、最后心跳和证书到期；当前页仅在线目标可选（最多32），30秒连接缓存或证书到期即停止选择，明确连接不等于主机健康。提交保持同body稳定幂等键；失权关闭表单。既有执行五Tab支持agent，尝试详情展示冻结公共绑定及真实proof，unknown只有command/attempt/fence匹配的持久终态才出现Admin/DL/SRE显式对账表单；没有人工指定成功或新远程重放入口。

新增actual PG/HTTP/Agent测试及浏览器fixture。首轮/tmp/keel-agent-integration-l900904t未通过：重复ID验证被首目标离线检查抢先返回503，现已将纯ID校验前移；浏览器终态等待后另读遇到尚在running的记录，改为轮询并断言同一实际HTTP响应的success与真实proof。失败轮不计PASS。修正后HTTP权限/分页/默认关闭/CSRF/八角色/跨项目/幂等/真实任务/断线回执与显式对账全部通过，/tmp/keel-agent-integration-dgzdq9q8的HTTP测试PASS（同轮旧浏览器断言仍失败，不记总通过）；最终独立真实浏览器/tmp/keel-agent-integration-65wl_wc_退出0，含真实失联unknown→重连query→网页对账→资源释放/原command未重放、身份任务响应丢失幂等重试、五Tab、UI403、英语移动端wait与Viewer只读。初轮其余既有Agent PKI/gRPC/CLI/任务/受限角色测试PASS，证据保留。所有为隔离软件身份，无硬件PoC。

宿主Go仍使用既有GOFLAGS overlay与GOMEMLIMIT=768MiB；新runner将大二进制写磁盘私有证据目录，保留/tmp日志截图，支持--browser及--run定向复验。CI新增独立Agent浏览器步；正式OpenAPI和生成类型、Agent运行说明及追踪同步，仍标partial。最终全量检查/提交/CI/3000发布以随后补记为准，此时运行版仍§73。

生产Agent listener/worker/HTTP任务准入、真实SSH/主密钥/外部介质获取/组织认证保持关闭；两份原fabric草稿未跟踪、不编译不提交。完整任务目录、传输/自动安装/systemd、续签/CA轮换、遥测、完整bootstrap/repair/PXE与真机验收仍未完成，WP-1.5/P1/FR-0不能据此闭环。

§74本地最终补记：全量 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB GOMAXPROCS=2 make check` 退出0，/tmp/keel-agent-api-make-check.log，包含设计、H-1离线测试、Go vet/race、正式类型生成、UI零预算/版本输入与生产构建（既有大bundle提示保留）。Compose config --quiet、diff --check通过；GitHub SSH认证成功，fetch后本地与origin无分叉。提交/CI与3000证据随后补记。

§74发布补记（2026-10-25 14:08 UTC）：功能提交`6668a72c1adaeb2444b8c28a3f4db4f038db49dd`已推送，[GitHub CI/run40199976376](https://github.com/enzoOvOyu/keel/actions/runs/40199976376) completed/success，包含完整工程检查、实际PG/HTTP/Agent身份与任务/CLI/受限角色、Agent浏览器真实unknown对账及原有认证/SSH/OIDC/LDAP浏览器与容器健康。精确git archive镜像`sha256:c6fbfd82912588f0f428a18440761ac6146a9c8b4e7db8de83c48e5adf79bdae`候选3108和运行3000均通过实际登录八页工作台/应用角色读探针；新Agent端点匿名401、认证空列表enabled=false、提交AGENT_DISABLED/503实测，未生成生产任务/CA/token。发布证据目录`/home/ubuntu/.local/state/keel/releases/20261025T135722Z`保留source.tar、构建日志、ci-verified、candidate/runtime-verified和浏览器日志截图。

运行库仍46迁移，Agent三表为空，全部真实通道/组织认证维持默认关闭；本轮无迁移或业务写入。候选容器及临时env已移除，.env保持600，旧镜像/数据库卷/备份与两份原fabric草稿保留。当前镜像仅用于已实现范围的开发预览，WP-1.5/P1/FR-0仍partial。

## 75. 接续开发：Agent 同密钥续签与 OS 引导介质组合（2026-10-25，进行中）

用户15:36 UTC重新启动两小时单 Agent 开发，预计17:36停止；后续要求P1实现完成再集中测试。保留该时间窗，完整测试/浏览器/镜像构建/发布均不逐增量执行；当前3000仍为§74功能提交6668a72/schema46。

WP-1.5补齐同CA/同私钥叶证书续签：默认关闭的mTLS Renew RPC、050只追加证书链、前驱幂等/撤销串行、具体叶证书绑定连接；稳定credential及原证书摘要保持任务快照身份。客户端显式renew或opt-in运行窗口间自动续签，受保护本地锁+CAS原子落盘，丢响应/落盘失败重取同一证书，过期不绕过。owner侧agent-credential-status读取绑定、有效期、最近续签及连接证据，不含密钥/PEM/token/session。详见AGENT-IDENTITY。此前一次必要安全验证已通过：带overlay/GOMEMLIMIT运行check-agent-integration.py --run 'TestAgentRenewal|TestAgentIdentityAtomicState'，证据/tmp/keel-agent-integration-d_bdqdt9，退出0。该证据早于后续状态CLI用例，不能计为后续改动全部通过。

WP-1.10→WP-1.7补齐OS引导角色：os rootfs及直接固定的kernel/initrd/config安装模板，绑定已有依赖/摘要/架构/作用域/独立审批；签名清单保留角色，实际读取、审批、下载与准入统一校验。新增只读boot-profile API及介质登记/核对界面，不启动装机。工程契约见MEDIA-ARTIFACTS。源码47条迁移（新050，不含原026草稿），生产仍46；不迁移生产、不生成生产CA/令牌、不启用worker/listener或硬件操作。

本批必要编译：宿主overlay/GOMEMLIMIT/GOMAXPROCS=2的定向go test -run '^$'退出0，日志/tmp/keel-p1-renewal-boot-compile.log（只编译，无测试用例运行）；前端仅api:generate及tsc --noEmit退出0，/tmp/keel-p1-boot-types.log。新增OS角色数据库/签名/拒绝路径用例尚未运行，完整验证按当前用户要求集中进行。两份原fabric草稿保留且不提交。当前仍在继续开发，尚未形成P1闭环或发布声明。

§75装机准备续记：资产页已接BOM驱动配置预览：冻结资产版本、发布型号cpu_arch、显式物理引导口、审批OS组合与本项目至少再有效一小时的公钥。同事务检查当前作用域/角色、资产版本、介质实际字节、架构及密钥撤销；结构化cloud-init模板禁身份覆盖/任意脚本/别名/多文档，生成受限keel-ctrl公钥配置与独立预览摘要。没有创建Execution、回调令牌、DHCP配置或改变互信。新增boot_only筛选避免把OS组件误列为完整镜像。详见PROVISIONING；新用例尚未运行。必要编译退出0：/tmp/keel-p1-provision-compile.log；前端生成+tsc日志/tmp/keel-p1-provision-types.log，最终进程结果另核实后记录。完整测试/浏览器/镜像仍按用户要求延后，3000不变。

§75编译进程最终结果：Go定向编译与前端生成/tsc均退出0。后续继续WP-1.6的owner侧主密钥重包裹维护；只改源码，不对生产库/主密钥执行轮换。该能力不依赖后续P2/P4产物；PXE完整安装仍按PROVISIONING列明的顺序待接。

## 76. owner侧SSH主密钥重包裹（2026-10-25，源码待集中验证）

接续P1/WP-1.6既有独立主密钥与不可变项目域钥。新增owner CLI credential-master-inspect/rewrap，带域指纹、精确数量/来源指纹、稳定操作ID、全记录认证/全事务重加密、逐条摘要证据/单审计、代际建钥约束及历史主密钥禁复用。公钥/任务身份/有效期/撤销保持，历史过期/撤销只认证存储不获得签名能力。051维护表及更新触发器保持应用角色只读；部署仍需明确维护窗口和双文件保留，命令不改.env、不重启/清理服务。工程契约及恢复顺序见CREDENTIAL-MASTER-ROTATION。

本批只做源码开发；没有读取/轮换生产主密钥、迁移运行库或执行维护命令。源迁移现为48条（新增050/051，不含原026草稿），运行3000仍6668a72/schema46。新增crypto/PostgreSQL/回滚/权限用例已写入待集中运行；必要Go编译日志/tmp/keel-p1-master-compile.log退出0。后续源审查收紧cloud-init容器tag及字段类型，并将OS核对根的排他读锁改为共享锁以允许并发预览，生命周期写仍串行；这些后续变更待下一批编译及最终集中验证。P1/相关WP/FR-0仍partial。


## 77. 审计独立留证与集中验证准备（2026-10-25，源码待集中验证）

本窗口已用本地提交 `0daed4b` 保存 Agent 续签/状态、OS 引导介质与 BOM 配置预览、owner 主密钥重包裹。没有 push，避免提前触发全量 CI；没有镜像构建、生产迁移或更新 3000。源码 48 条迁移，运行仍 46；两个原 fabric 草稿继续排除。

随后补齐 owner 审计摘要导出/独立 Ed25519 签名、离线验证和当前库比对 CLI：全部字段的规范化投影、只读一致快照、严格索引及 seal 编码/上限、新增记录单独计数、已归档修改/删除报告差异，导出不含原始审计内容。归档目录不可覆盖，签名私钥和可信公钥由独立保管域管理；没有生成生产密钥或导出生产数据。格式/命令/威胁边界见 AUDIT-EVIDENCE；不能视为完整三年留存、备份恢复或 S-5 验收。

新增单次构建 owner CLI 的 check-p1-lifecycle-integration.py，以及实际 CLI/PG/单元拒绝路径用例，均待按用户要求集中执行。该入口只覆盖新增 P1 生命周期软件子集，对象传输为内存 fixture，没有浏览器/真机/全 P1 验收声明。另修正新增全局 OS boot-profile 的 OpenAPI 路径为实际 /api/v1/admin/media/...，保持正式契约与既有路由一致。后续编译/静态检查结果及本地提交随后补记。

§77必要检查补记：带既有 overlay/GOMEMLIMIT/GOMAXPROCS=2 的 `go test -p=1 ./internal/auditevidence ./internal/platform ./cmd/keel-admin -run '^$'` 退出0，证据 /tmp/keel-p1-audit-final-compile.log，明确只编译而未运行测试。前端正式类型重新生成及 tsc --noEmit 退出0，/tmp/keel-p1-contract-types.log；该结果早于随后两处 UI 缓存失效修正。check-design.py 和 diff --check 退出0，均不证明新增业务用例通过。


## 78. 出站执行 Webhook 与装机准备浏览器入口（2026-10-25，源码待集中验证）

审计独立留证已保存本地提交 `47c3370`，未 push。随后接 WP-1.6 的首个出站通知通道：默认关闭、项目绑定、独立 HMAC/精确源白名单/DNS固定拨号、无重定向、CloudEvents公共投影、现有 outbox 独立消费、不可变策略/原消息/终态、短事务认领+HTTP期间不持锁、30秒认领/8秒请求、最多6次退避和owner分页状态CLI。每个sink有独立循环且最多16个，不占用执行调度/心跳；响应丢失保持同事件ID供接收方幂等。原始metadata/URL/密钥不进入送达状态输出。配置首次绑定原子审计；部署配置只读独立挂载，默认空目录，生产未启用。详见 WEBHOOK-NOTIFICATIONS；邮件/IM/通知管理页面/工单门禁来源等仍待开发。

新增052后源迁移49条（含050/051/052，不含原026草稿），运行仍46。没有执行生产迁移、通知发送、密钥生成、镜像构建、CI或3000发布。新增 Webhook/PG/本地HTTP/实际CLI用例写入生命周期批量入口，未运行；生产缺失迁移时不得直接运行新二进制。

装机准备补 `check-media-artifacts-integration.py --provision-only`：项目/全局 OS 引导角色 UI 与真实 MinIO、BOM/型号/域公钥、owner CLI撤销、无执行/资产改变以及Viewer拒绝；小字节fixture不是真实启动镜像。该脚本和浏览器均未执行。并修正新两个弹窗在查询失败时展示旧缓存的问题：隐藏OS旧核对证据，清除配置预览/选择并禁用提交。OpenAPI全局boot-profile路径与生成类型同步，接口实际前缀为admin/media。必要编译与静态结果及本地提交随后补记。

§78源码收口：装机 UI 失效处理与浏览器入口已保存本地 `f75aaff`，浏览器仍未运行。审查另修 fs_setup 应为 array-of-mapping，并拒绝 fs_setup.cmd/extra_opts、apt.conf、源 filename/路径穿越和 ntp.config 自定义路径；独立批准的软件包仍可执行自身安装逻辑，不能把此模板约束称为完整 OS 沙箱。字段依据沿用 PROVISIONING 中的官方 cloud-init 模块参考。新正负例未运行。

本窗口末批必要编译：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB GOMAXPROCS=2 go test -p=1 ./internal/auditevidence ./internal/webhook ./internal/mediasource ./internal/provisionseed ./internal/platform ./cmd/keel-worker ./cmd/keel-admin -run '^$'` 退出0，/tmp/keel-p1-window-final-compile.log，全部明确 no tests to run。前端最后两处 UI 改动后 tsc --noEmit 退出0，/tmp/keel-p1-preparation-ui-compile.log。JS node --check、三个 Python ast.parse、check-design.py、diff --check、Compose config --quiet 均退出0，只有语法/静态配置证据。新增 SQL 触发器尚未在数据库执行，新增功能/owner CLI/浏览器用例也未执行；原§75早期续签安全验证不覆盖本窗口后续功能。

继续开发仍留在 /opt/keel、dev/p1-network-bom，使用同一宿主 Go overlay/GOMEMLIMIT/GOMAXPROCS。当前 P1 仍 partial：下一项核心链是共享 Execution 下的冻结装机请求与独占资源/重装权限，之后受控 ProxyDHCP/iPXE/可信分发、同请求的一次性回调和真实 SSH 首连/修复；不能把预览或通知送达当安装成功。Agent 全任务目录/介质传输/完整部署轮换、通知邮件/IM与页面、完整留存恢复及规模/真机判据继续按实际依赖推进。不要为 FR-0 强行提前实现 P2/P4。

完整测试/浏览器/镜像按用户要求待 P1 实现里程碑集中进行，当前只准备复用的一次构建生命周期批量入口及现有独立 Agent/SSH/MinIO/浏览器 runners。不要提前 push 触发全量 CI，也不要把任何新用例写作 PASS。运行3000仍为§74的6668a72/schema46；生产worker/Agent/SSH/主密钥/组织认证/外部介质源与新Webhook均未启用。两个原fabric草稿保持未跟踪、排除编译且不提交。后续提交号及时间窗结束另补记。

§78本地提交补记（2026-10-25 17:36:00 UTC）：本窗口5笔源码提交为 `0daed4b`（续签/OS准备/主密钥）、`47c3370`（审计留证）、`f75aaff`（失效预览及浏览器入口）、`3fcdd4b`（cloud-init选项约束）、`b04bab5`（执行Webhook）；`2448b5b`记录下一步事务/共用设备资源落点。全部仅本地，没有 push/CI/发布。最后check-design.py退出0，源迁移为49。除两份原fabric草稿外业务源码已提交；PROVISIONING明确下一单元需在同一创建事务复用准入，实际机器独占device:machine资源，禁止另造队列或落回sim空间。用户要求的集中测试仍待P1实现完成，本轮未开始新集成/浏览器/镜像验收。


## 79. 持久装机准备、独立审批与共享 Execution（2026-10-26，源码待集中验证）

本次在 /opt/keel、dev/p1-network-bom 接续 e0a9467；00:11:10～02:11:10 UTC两小时窗口进行中，单Agent，无新子代理。用户仍要求P1实现后集中测试，未启动完整测试、浏览器、镜像、生产迁移或设备listener。

WP-1.7新增不可变30分钟装机准备申请，冻结BOM/已发布型号/显式物理引导口/批准OS四角色精确对象字节及项目公共域钥，使用新请求ID作为cloud-init instance-id；稳定幂等键、版本冲突、每机器单活动申请、重装意图与完整SN确认。Admin/DE/DL/SRE可申请，独立Admin/DL审批，创建者不能自批；DE仅获装机公共密钥候选，不扩大SSH凭据/互信审批权限。历史在项目关闭后可读。过期仅回收非运行请求，跨项目被动过期审计归原项目，不泄漏旧申请给新项目。

原生provision.prepare通过现有Flow/Execution/outbox/TaskAttempt/心跳/fence/持久回执和unknown对账接入，独占device:machine:<id>，与SSH/Agent同冲突域。派发复验当前操作者/审批人、BOM/介质/公钥，worker实际读取固定对象并重渲染比对摘要。回执绑定request/hash/attempt/fence，拒绝不匹配证明；从未启动的排队任务取消可撤销申请；运行/unknown不能凭超时或客户端成功字符串解锁。成功仅置prepared，固定installation_performed=false，不更新资产安装/SSH信任，也不启动网络引导；实际安装须重新取锁复验。

API集合、创建/详情/审批/取消/准备执行及候选公共密钥契约已写入OpenAPI并生成TS；BOM预览可创建申请，DS路由/p/<projectId>/deploy/provision显示审批/准备/取消和共享执行链接，复用shadcn/ui。API/worker新增KEEL_PROVISION_PREPARE_ENABLED，默认false；worker需配置同一对象存储。053迁移新增请求与provision模式，未执行生产SQL。source50（排除026原草稿），runtime46；3000仍6668a72/§74镜像，未发布或推送触发CI。

实际完成仅编译：带既有overlay及GOMEMLIMIT=768MiB/GOMAXPROCS=2的go test -p=1 [platform/execution/httpapi/keel-api/keel-worker] -run '^$'通过（/tmp/keel-provision-source-compile.log）；execution/httpapi go vet通过（-vet.log）；生成后tsc --noEmit通过（-types/-tsc.log）；UI边界与版本控件静态检查通过（-ui-boundaries/-version-inputs.log），docker compose config --quiet及check-design通过（-design.log），Python AST/JS syntax通过。没有执行新增用例、SQL迁移或浏览器，不视为功能验收。

已写待运行：平台申请/幂等/独立审批/不可变快照/重装确认/DE与Viewer权限、execution真实PG共享Agent设备锁/单次消费/排队取消/错绑回执/unknown占用；既有--provision-only集中入口追加独立随机Agent测试库与实际API/对象存储/浏览器申请→审批→准备链。修正旧预览fixture第二用户错误重复init-admin；仍待集中运行。下一主链是受控ProxyDHCP/iPXE/HTTP、硬件预检、同请求一次性回调，再真实SSH首连/修复与七阶段/规模/PoC；WP-1.7/P1保持partial。


## 80. 受控服务器引导协议适配（2026-10-26，源码待集中验证）

接续本地111364a，新增internal/provisionboot；没有重建镜像/运行网络daemon、启用生产配置、创建授权装机Grant或改变Kea。仍在00:11:10～02:11:10 UTC窗口中，后续继续开发。

传输层含直连Ethernet DHCP/PXE严格解析、scope/架构与MAC边界、UEFI HTTP/iPXE防循环/Legacy文件选择、零地址分配ProxyDHCP响应；Linux通过SO_BINDTODEVICE与接口/目的地址二次核验提供关闭默认的67/4011入口，现有服务端口冲突即失败，不停止他人服务。独立HTTP handler校验直接peer、origin/TLS、固定资源、完整对象SHA/大小后提供Range/HEAD/条件请求；有界并发及到期/撤销检查。Legacy只读TFTP限16MiB启动固件，512字节块、独立TID/套接字、精确ACK、重复ACK忽略（避免Sorcerer放大）、超时自适应重发/4次上限/2分钟总时限、并发上限和重复RRQ去重。不支持任意路径或写入。正式参考链接及全部边界见PROVISIONING。

Authority是明确的待接业务边界：每次必须证明当前审批、scope/请求/机器/MAC/架构、attempt/command/fence、快照及真实独占lease。**尚无生产Authority/装机Execution接线，prepared不足以签发引导许可**；无API/worker自动启动。当前传输源码不是完整可运行装机系统，未证明真实固件/镜像、硬件预检、回调或首连。网络scope当前文件清单不能独立证明现有Kea范围完整，后续须从当前网络计划逐次核验。

仅实际执行Go编译（go test -p=1 ./internal/provisionboot -run '^$'，/tmp/keel-boot-transport-compile.log）和定向go vet（-vet.log），带既有overlay/内存限制；均退出0。新协议/权限/Range/损坏拒绝/TFTP去重与终止/fuzz用例只写入未运行。没有新SQL迁移，source50/runtime46及3000不变。后续先形成批准的启动固件/安装recipe与硬件预检输入，再接真实lease Authority、一次性回调及首连；不能假设现有任意rootfs已具有可启动安装参数或预检helper。


## 81. 安装环境只读硬件预检与双架构 helper（2026-10-26，源码待集中验证）

接续3619992，新增internal/provisionpreflight与cmd/keel-preflight；读取Linux sysfs/DMI/device-tree和固定只读SMART命令，按批准冻结Spec检查SN/架构/唯一物理引导MAC、按physical_slot分组的NIC数、明确PCI身份的GPU在位和当前磁盘健康。缺驱动/槽位/SMART/独立期望/未知设备或陈旧事实保持BLOCKED；支持状态不伪造PASS/WAIVED。原始报告的控制面复算比较完整Spec与逐项结果，仍必须由未来真实装机Authority核验一次性回调与当前安装lease。无硬件执行/磁盘修改、网络回调或生产接线。

型号新增可选gpu_pci_ids/non_gpu_pci_ids，扫描草稿/发布共用互斥非空规范ID校验；OpenAPI与生成TS同步，不改历史型号，也不从本次事实推导独立期望。Makefile新增build-preflight，CI源码追加双架构编译；API镜像不打包/自动启动helper。后续必须把固定二进制和驱动/smartctl打进批准的可启动介质，再形成实际recipe/装机Execution与Authority；prepared不能替代。

本批用例已写但未运行（采集fixture、NIC多端口/VF/驱动、GPU/BMC/零数量、SMART退出位/未知、报告错绑/伪造）。实际仅执行：带overlay/内存限制的go test -p=1 ./internal/provisionpreflight ./cmd/keel-preflight ./internal/platform -run '^$'编译；provisionpreflight/CLI定向go vet；OpenAPI生成与tsc --noEmit；check-design；均退出0。amd64/arm64 CGO_ENABLED=0交叉构建成功，file确认为静态ELF（/tmp/keel-preflight-binaries.log），二进制未执行；日志为/tmp/keel-preflight-{compile,vet,types,tsc,design}.log；无SQL迁移、runtime/source数量和3000不变。保持原overlay/GOMEMLIMIT=768MiB/GOMAXPROCS=2，未启动其他Agent，继续开发窗口至02:11:10 UTC。


## 82. 固定启动配方/固件/helper与iPXE文件生成（2026-10-26，源码待集中验证）

接续ff1791d，在现有os_boot增加可选recipe，明确自定义keel-initrd/v1+squashfs，固定直接批准的同架构UEFI/可选amd64 Legacy、预检和SSH helper ID/摘要。已有四角色仅准备预览兼容，实际Plan.ValidateInstallable拒绝无配方；新增角色不能用kernel/rootfs互充。媒体验证/签名清单保存并复核整个配方，UI下拉固定依赖版本、自动取helper摘要，契约和生成类型同步。公共角色类型移到mediaboot，platform保留类型别名，避免复制或循环依赖。

provisionboot.RenderBootBundle复核完整scope/request/project/machine/attempt/command/fence/hash/期限与NoCloud instance-id，生成绑定原始字节摘要的manifest/preflight与固定iPXE脚本；匹配BOM MAC后仅在对应net0～net255请求DHCP，禁止任意内核参数、下载/引导失败退出。HTTP白名单加入preflight.json及两个固定helper资源。没有签发生产Grant、监听网络或安装写盘；**这是明确的自定义initrd协议，尚无遵守该协议的实际安装环境/一次性回调与当前许可/生产Authority，不能把旧cloud-init或任意rootfs当作已实现装机。** 下一步先构建可审核helper安装包/镜像输入，再接实际安装执行器和授权闭环。

新增角色/ARM与Legacy/摘要/签名篡改/跨请求/fence/seed错绑用例写入未运行，媒体真实PG用例增加缺失固件/helper直接依赖拒绝。实际仅编译mediaboot/mediamanifest/provisionboot/platform/execution/httpapi（go test -p=1 -run '^$'），前四包定向go vet，OpenAPI生成、tsc --noEmit、UI边界/版本输入静态检查及check-design均退出0；日志/tmp/keel-boot-recipe-{compile,vet,types,tsc,ui,version-inputs,design}.log，带原overlay和内存限制。未执行新增用例、SQL、浏览器或镜像构建。source50/runtime46、3000及默认关闭配置不变；用户两小时窗口仍持续至02:11:10 UTC。


## 83. 独立 helper-set 与 installer overlay 生产/反向校验（2026-10-26，源码待集中验证）

接续ff7f336，新增mediaboot.HelperSet、provisionimage gzip/newc writer/reader，以及keel-admin provision-overlay-create/verify。独立同架构config先绑定两份已批准OS型helper及ID/SHA256，生产overlay后才构建/审批最终initrd与OS配方，**不要求先有最终OS/initrd/recipe**。原始config/二进制必须匹配独立信任的签名清单；格式检查静态64位ELF/目标架构/可执行入口，输入复制时复验摘要，归档只含固定只读helper与公开元数据，无/init、账户/私钥/设备节点/任意脚本。受保护目录私有暂存+fsync+不覆盖链接发布；反向读取固定CPIO集合，不落地解包，拒绝改路径/属主/模式/错误签名/字节、尾随/串接/截断。复用既有离线媒体安全发布检查，仅抽取原有输出路径校验，不改变其规则。

Make新增build-installer-helpers跨架构生产两份实际helper；CLI不接DB/网络、不执行二进制/安装。独立config避免生产依赖循环；包格式/签名/ELF通过也不是实际initrd驱动、写盘前检查或磁盘安装验收。实际自定义initrd入口、一次性回调许可、当前网络/设备lease Authority、装机Execution及SSH首连仍待开发。

新增有界归档/确定性/ELF/TOCTOU/错误架构、独立批准config/签名/私有输出/符号链接/损坏/反向验证等用例写入未运行；实际仅执行mediaboot/provisionimage/mediamanifest/keel-admin编译（go test -p=1 -run '^$'）、相同范围定向go vet及check-design，均退出0；新的keel-admin以CGO_ENABLED=0交叉构建amd64/arm64成功（未执行CLI）。日志/tmp/keel-installer-overlay-{compile,vet,design,binaries}.log，带原overlay/内存限制。没有运行任何新用例或生成/验证实际安装包。无SQL迁移、镜像构建/发布或生产操作；source50/runtime46、3000仍§74。单Agent继续开发窗口至02:11:10 UTC。


## 84. Linux 物理槽位 ABI 与 PCI 地址边界修正（2026-10-26，源码待集中验证）

接续3b6118a，对照Linux/pciutils官方源码发现§81初稿对通用devices/<BDF>/physical_slot属性的假设不成立，已改读/sys/bus/pci/slots/<name>/address；明确槽位聚合多功能网卡，PCIe桥下按真实sysfs父链的最近映射归组，不把BDF/端口本身当物理NIC。两段bus占位地址不算卡，同地址多槽位/无来源保持unknown；槽位名0合法。PCI域支持规范4～8位并限有符号domain范围，设备00～1f/function0～7；不可解析条目及未支持非PCI物理网卡记缺失、非法port PCI不形成通过。无内核槽位事实的onboard仍BLOCKED，尚无猜测式fallback。

删除采集fixture虚构的per-device physical_slot属性，增加真实slots ABI、多功能/桥下设备/零槽名/占位/冲突/扩展域/非法路径用例；均只写入未运行。实际仅执行带原overlay/内存限制的go test -p=1 ./internal/provisionpreflight ./cmd/keel-preflight -run '^$'编译、同范围go vet及check-design，均退出0（/tmp/keel-preflight-slots-{compile,vet,design}.log）。没有宿主采集、SMART、网络监听或镜像/SQL变化；P1集中验证策略和原两份草稿保护不变。


## 85. 集中验证入口补齐新模块与真实离线CLI（2026-10-26，仅源码接线）

接续a7a8e81，既有--provision-only集中入口加入完整mediaboot/provisionboot/provisionpreflight/provisionimage软件组，签名配方/overlay/seed及平台模型/申请/共享Execution组；为离线CLI预编译CGO_DISABLED静态native预检/SSH helper，显式路径传给TestInstallerOverlayRealHelperCLI。后者实际运行admin create/verify/禁止覆盖/错误独立公钥，但不执行helper、不接DB/网络或硬件；未显式提供三条二进制路径则skip。普通完整media入口补齐基础/准备域；browser-only不新增Go运行。CI源码增加实际helper/admin离线CLI专步，尚未推送触发。

本批仅实际编译mediamanifest新增用例（go test -p=1 -run '^$'）和定向go vet、Python AST/CI YAML语法检查；编译/vet退出0，日志/tmp/keel-provision-milestone-{compile,vet}.log。没有运行集成脚本、新用例、CLI、浏览器或镜像，遵守用户P1完成再集中测试。仍无生产监听/SQL迁移/发布，P1/WP-1.7保持partial。


## 86. 引导文件消费端完整性与冻结身份校验（2026-10-26，源码待集中验证）

接续6d075f3，新增provisionboot.VerifyBootFiles：独立可信控制脚本/内核参数的SHA256先核原始规范清单，再核固定可安装配方、项目/架构角色、预检原始字节和user/meta-data摘要，关联全套申请/机器/attempt/command/fence/快照/期限/MAC/架构与NoCloud instance-id。拒绝自报信任根、未知/重复/大小写别名JSON及尾随、错绑/过期/字节变化。RenderBootBundle复用原有相同NoCloud metadata规则；HTTP实际固定对象版本上限对齐登记层4096，并拒绝MinIO字面值null版本。

函数只校验数据，不下载/执行/写盘或签发许可；尚未接入实际受控initrd/当前lease Authority/一次性回调。新增往返/内部错绑/篡改/解析歧义及版本边界用例未运行。实际仅执行带原overlay/内存限制的provisionboot编译（go test -p=1 -run '^$'，/tmp/keel-boot-files-compile.log）和定向go vet（/tmp/keel-boot-files-vet.log），退出0；设计静态检查见本次提交。无SQL/镜像/生产或3000变更，P1保持partial。


## 87. 引导对象摘要读取期间的持续授权检查（2026-10-26，源码待集中验证）

接续ce48c9f，HTTP/TFTP在完整摘要验证前创建同一fileGuard，并在每次读取中检查context/期限、每250ms复核当前Authority；摘要后强制一次新检查，避免短对象使用缓存授权。通过后复用同一guard传输，失败保持终止；不再等读完整个大对象才发现撤销。HTTP最终返回状态/头部前仍核验期限，TFTP亦在Open结束前核验真实Authority。

新增无sleep的读取钩子用例，覆盖中途撤销/过期/取消、提前停止读取、失败不可复活及短对象最后一次读取后撤销；仅写入未运行。实际执行带原overlay/内存限制的provisionboot编译与定向go vet，退出0（/tmp/keel-boot-hash-guard-{compile,vet}.log）；设计检查见本次提交。没有监听、生产授权器接线、SQL或3000变动；底层Authority仍须支持context取消和返回不可变精确版本文件。


## 88. 按实际smartctl --health形状核验NVMe告警（2026-10-26，源码待集中验证）

接续1fc17fe，核对smartmontools官方nvmeprint.cpp发现--health输出smart_status.nvme.value，完整nvme_smart_health_information_log只在属性选项打印，原解析只考虑后者不完整。已支持实际health字段及可选完整日志，双来源须一致且8位；原生NVMe路径/显式协议/已出现NVMe块缺告警值保持unknown，非零当前故障不被passed=true覆盖。没有额外smartctl选项、SMART自检或宿主设备访问。新增健康单块/缺值/经桥接设备/完整日志/矛盾/越界用例只写入未运行。

实际仅执行带原overlay和内存限制的provisionpreflight/keel-preflight编译与定向vet，退出0（/tmp/keel-preflight-nvme-{compile,vet}.log）；设计检查见本次提交。关闭默认接线、集中测试策略、数据库和3000均不变。另对最终provisionboot/provisionpreflight/keel-preflight执行linux/arm64、CGO_ENABLED=0交叉编译，不执行二进制，结果见本次窗口收工记录。


## 89. CI覆盖独立准备与共享设备锁集成入口（2026-10-26，仅源码接线）

接续ec562d6，复查发现make check没有提供Agent专用DSN时会跳过复用agentKernel的TestProvisionExecutionKernelPostgres，而后续专用Agent CI步只筛选TestAgent。已把既有该步的筛选扩为Agent及装机准备共享执行器用例，复用同一隔离CI数据库和已编译二进制，避免新增重复构建/环境或遗漏测试；平台准备用例仍由原make check覆盖。当前只校验CI YAML/嵌入shell语法，不推送、不触发或执行CI，测试依然待集中验证。


## 90. 准备任务完整字节校验后的当前准入复核（2026-10-26，源码待集中验证）

接续1277abb，VerifyProvisionRequestTx在完整冻结源读取/重渲染/摘要比较后再次检查context与当前准备期限、审批人权限、BOM/公钥/介质元数据，避免仅在耗时对象读取前校验，而在失效后仍报告prepared。仍只产生准备证据，不能授权实际安装。新增隔离PG用例通过对象读取钩子使请求在字节验证期间失效，避免sleep或修改不可变DB身份；只写入未运行。

实际执行platform/execution编译（go test -p=1 -run '^$'）与定向go vet，结果见/tmp/keel-provision-late-expiry-{compile,vet}.log，带原overlay/内存限制；设计静态检查见本次提交。未执行PG用例、迁移、生产或浏览器操作，集中验证策略不变。


## 91. 准备看板读取失败与切换项目清除旧操作（2026-10-26，源码待集中验证）

接续0945c9e，看板列表任何当前读取失败均隐藏缓存操作行并关闭确认弹窗；项目变化重置分页/选择，弹窗另校验所属项目，防止沿用上一项目选择。仍复用shadcn/ui，无样式/字体替换。既有集中浏览器脚本新增503后旧行消失、恢复读取后重新出现的场景；同时修正旧PASS日志的“no execution”措辞，因为现在实际创建原生准备Execution，只能声明无安装/资产状态变化。场景未运行，仅JS语法、TS类型及UI静态边界检查，结果见窗口收工记录。


## 92. 本轮两小时开发交接（2026-10-26，00:11:10～02:11:10 UTC）

唯一工作区/分支为/opt/keel、dev/p1-network-bom。接续e0a9467单Agent开发；本轮13个实现/验证入口源码提交为111364a、3619992、ff1791d、ff7f336、3b6118a、a7a8e81、6d075f3、ce48c9f、1fc17fe、ec562d6、1277abb、0945c9e、8f40e56，最后另提交本条交接/需求追踪压缩记录；均仅本地，未推送/触发CI/发布。各批具体事实见§79～91。

本轮形成准备请求/独立审批/共享执行器与页面、隔离引导传输、只读硬件预检/双架构helper、固定启动配方/iPXE/清单生成校验、独立批准helper-set的离线overlay生产/反向验证；并修正真实Linux槽位/NVMe输出、传输中失效、准备完成时准入和页面旧操作残留。P1与WP-1.7仍partial：无已验收可启动initrd、实际安装/一次性回调许可/生产Authority/SSH首连闭环，prepared只证明源准备。

实际完成必要Go包/命令编译（go test -run '^$'，无用例运行）、定向go vet、OpenAPI/TS生成与tsc、UI/版本静态边界、设计DAG检查、Python/JS/CI YAML及嵌入shell语法检查，并成功编译指定amd64/arm64静态helper/admin；最终新增boot/preflight/preflight CLI的linux/arm64编译也退出0（/tmp/keel-provision-final-arm64-compile.log）。最终平台/执行器编译与vet为/tmp/keel-provision-late-expiry-{compile,vet}.log；看板tsc/UI为/tmp/keel-provision-board-final-{tsc,ui}.log，均退出0。上述不是功能/SQL/CLI/固件/硬件证据。新用例只写入；遵照用户P1开发完成后集中测试，没有运行集成入口、新离线CLI、浏览器或镜像构建。

02:08 UTC只读复核：3000 /healthz为status=ok、版本6668a72c1adaeb2444b8c28a3f4db4f038db49dd，实际镜像sha256:c6fbfd82912588f0f428a18440761ac6146a9c8b4e7db8de83c48e5adf79bdae、容器healthy；Keel PostgreSQL/Redis/MinIO及15433测试PG亦healthy；runsun/5173未动。.env仍600。没有应用053或任何SQL：源迁移50、运行基线46/至049继续保留，不能把源码当3000已交付。

下一步直接沿PROVISIONING最后一节推进：先明确独立批准目标磁盘/布局的安装契约，完成受控initrd的无写盘准备及默认拒绝许可接口；不把MinimumDisks、现有cloud-init或overlay当可安装证明。随后接真实装机Execution/当前网络与独占lease Authority、一次性认证回调/当前写盘许可、首启host-key和真实SSH首连/修复、七阶段事件投影。不要重做已完成准备/传输，也不借FR-0扩展P2～P4依赖。集中验证入口已接线，待P1实现闭环后使用。

继续保护原未跟踪internal/platform/fabric_precheck.go与internal/platform/migrations/026_fabric_precheck.sql，不编译/提交/迁移/删除；Host Go包括gofmt继续使用GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json、GOMEMLIMIT=768MiB、GOMAXPROCS=2。保留所有数据卷、原服务、回滚镜像和受保护证据。本轮无新增开发子代理或后台装机任务。


## 93. 显式存储布局契约与只读磁盘身份（2026-10-26，三小时窗口源码增量）

接续c0eb0c0，新增internal/provisionstorage：独立磁盘WWID/serial/精确几何/擦除意图，固定UEFI GPT/FAT32+ext4布局与有界规范Plan，完整绑定冻结Spec/原报告摘要/设备号/diskseq；复算预检要求PASS，拒绝歧义、占用/未知、只读/可移除、换盘/重编号/失效及自报摘要掩盖布局篡改。不是实际审批或写盘许可。预检扩展只读存储/UEFI事实、整盘/分区holder与mount/swap占用，采集末尾复核diskseq/几何；SMART新增只读--info。内核来源与具体限制见PROVISIONING末节，未运行宿主采集或SMART。

关闭cloud-init模板的disk_setup/fs_setup/mounts/growpart/resize_rootfs，避免绕过明确选盘/布局，原首启身份/域公钥注入保持。新增512/4Kn布局、唯一选择、未知/忙/换盘/篡改/过期、sysfs/挂载/swap/更换期间采集和模板拒绝用例只写入；集中provision-only基础组加入provisionstorage。当前仅执行定向编译与vet（/tmp/keel-storage-contract-{compile,vet}.log）、seed/platform编译（/tmp/keel-storage-seed-compile.log），结果见本次提交/后续记录；没有执行新用例、PG迁移、浏览器、CLI或镜像/设备写入。P1保持partial，正在继续受控安装环境开发，窗口至05:12:04 UTC。


## 94. initrd公共文件取得与只读helper准备核心（2026-10-26，源码待集中验证）

接续cc2fc70，新增provisioninitrd：严格受信kernel参数、固定接口/地址/CA的IP直连TLS 1.3文件客户端、独立摘要先验与完整BootFiles绑定、固定预检helper及报告复算；成功仅ready_for_storage_review，失败保留FAIL/BLOCKED，无安装/回调/信任投影。Linux helper仅root PID1和ramfs/tmpfs根启用，校验固定root只读二进制后复制到sealed memfd，规格走noexec sealed memfd，固定FD/argv/环境。复用provisionimage静态ELF函数（显式拒绝未知架构），未创建旁路执行器。iPXE增加固定服务器/CIDR并改ip=off，防内核在其他网卡自动DHCP；当前尚无网络配置适配/PID1命令与完整initrd，后续继续实现。

新增解析/TLS/下载和冻结错绑/失败退出/过期/取消等软件用例仅写入。实际仅执行定向Go编译/vet，日志/tmp/keel-initrd-prepare-{compile,vet}.log；后续文件拆分和root环境守卫同批最终结果以提交前检查为准。provision-only基础组补入新包；无宿主采集、memfd/helper运行、网络监听或镜像/SQL变动。05:12:04 UTC窗口继续，P1 partial。

## 95. 指定网卡临时联网与 PID1 只读入口（2026-10-26，源码待集中验证）

接续b78492c，新增独立cmd/keel-initrd与Linux虚拟文件系统/console/public-CA适配，唯一BOM MAC的直接DHCP客户端、严格租约/包绑定和ARP使用前探测/会话冲突监听。仅读会话结束/期限/取消后撤下临时IPv4，不设置默认路由/DNS，不把地址留到后续安装。拒绝relay/已有IPv4/端口占用，不干预其他接口/服务。Prepare保留精确原始报告字节供tmpfs证据和后续磁盘审批，PID1最终保持等待，无回调/写盘/自动重启。完整边界及内核要求见PROVISIONING末节。

Makefile新增双架构build-initrd，CI已有ARM编译列表加入新命令，未执行构建镜像/推送/CI。新增DHCP/ARP/公共CA/普通宿主守卫用例未运行。实际仅定向编译/vet（/tmp/keel-initrd-network-{compile,vet}.log）及linux/arm64静态入口编译（/tmp/keel-initrd-network-arm64），不能作为网络/启动证据。当前仍缺完整批准镜像，接下来实现独立runtime-set生产路径；P1 partial，05:12:04 UTC窗口继续。

## 96. 独立runtime-set到完整只读initrd的无环生产（2026-10-26，源码待集中验证）

接续68ec35e，新增mediaboot.RuntimeSet，先批准init/preflight/ssh_helper/smartctl/public-CA五个无子依赖输入及独立config，再生产initrd，最后另批产物并纳入OS recipe；不依赖未来成品。provisionimage完整固定gzip/newc写入/反向检查复用原CPIO/ELF实现，CA解析移到共享字节层；mediamanifest提供独立验签、全五项hash/arch/size校验、受保护目录0600原子非覆盖发布和校验。keel-admin新增provision-initrd-create/verify早期离线分支，源码不依赖DB/网络、不执行组件。ForPlan入口核对归档实际摘要及内含helper与最终Plan一致，但尚未生产Authority调用。

新runtime JSON/归档/签名/发布/错绑用例仅写入；provision-only和全media入口regex加入TestInstallerInitrd。未执行CLI/功能用例/镜像构建/硬件启动。仅定向编译/vet（/tmp/keel-runtime-image-{compile,vet}.log）及文档/脚本静态检查。完整归档源码不等于已验收可启动镜像，更不等于写盘许可；内建驱动/内核能力与真实smartctl仍需独立输入及PoC。P1 partial，下一步实际安装Execution/lease、一次性凭据/回调与持久存储审批。

## 97. 服务器引导域独立审批与双向网络冲突边界（2026-10-26，源码待集中验证）

接续178119d，新增054_provision_boot_scopes、平台/HTTP/OpenAPI/生成TS和装机页网络域面板，复用既有shadcn/ui和字体。当前Admin/NDE固定接口/HTTPS源/CIDR/外部管理网段显式清单和方案来源，独立另一人持客户/网络批准引用审批；不可变DB证据、版本并发、跨项目隔离、只读成员与闭项仍可撤销。LoadApproved入口校验当前审批角色/项目及平台已批准管理网段；network baseline批准反向拒绝服务器域重叠，两侧使用统一变更锁序，普通live读取不持全局变更锁。尚无生产监听/安装Authority调用。

新增独立审批、声明清单、scope/baseline双向冲突、并发只准一个、撤销、当前NDE失权、DE/Viewer及跨项目用例只写入。provision-only regex补入TestProvisionBootScope。实际定向编译/vet与OpenAPI类型生成、npm run check（含UI边界检查），日志/tmp/keel-provision-scopes-{compile,vet,api,tsc}.log；没有应用054/其他SQL、跑集成或浏览器、构建发布镜像。P1 partial；下一步绑定实际安装Execution/独占lease、一次性启动凭据及存储审批。

§97检查补充：最终直接tsc --noEmit -p web/tsconfig.json及定向编译退出0。npm run check自身含6条版本输入静态检查器自检并已通过（不是新增产品用例/浏览器验收）；额外误调用不存在的scripts/check-ui-boundaries.py退出2，未重跑同类检查，正确web/scripts/check-ui-boundaries.mjs已由npm run check通过。后续仅用直接tsc与必要静态入口，避免重复生成/检查。


## 98. 独立安装会话、启动身份与写盘前审批（2026-10-26，源码待集中验证）

接续e79bfbb，新增055安装会话/单次启动Token/append-only事件源迁移；与prepared证据分离的provision.install原生Execution使用新attempt/command/fence和与SSH/Agent共享的独占machine lease，无自动重试。源申请只能消费一次，活动或unknown安装阻断新准备；平台和execution复用相同机器准入锁编码。实际源字节及签名initrd必须经独立公钥验证，短期不透明验证结果才能用于激活；hash期间不持attempt/resource锁，激活与每次后续变更重查当前权限、批准网络域、冻结BOM/介质/公钥和独占租约。Execution行先锁定，随后attempt/resource和安装行，与完成/控制/回收锁序对齐。

新增Ed25519单次启动身份绑定及完整payload摘要、operation/installation/boot/递增sequence签名，Token只保存hash，数据库owner CLI provision-boot-token-issue将原值写入现有私有目录中新0600文件，不出stdout/公共iPXE/清单/cloud-init；失败尝试撤销，已消费Token不可复用。可信带外交付给目标机器仍须另行实现/明确操作。预检回调存精确原始报告并复算PASS/FAIL/BLOCKED；只有新鲜认证事实才能选定WWID/serial/几何及擦盘意图，另一Admin/DL（不同于安装发起人及选盘人）批准不可变布局。当前审批不是写盘许可。项目安装API及OpenAPI/生成TS已加入；没有新增安装页面操作。

KEEL_PROVISION_INSTALL_ENABLED默认false，与准备开关分离；API/worker开启需独立KEEL_PROVISION_IMAGE_PUBLIC_KEY_FILE。此增量仅写盘前持久状态/准入：没有激活生产引导监听、文件Authority、签名回调HTTP接入、initrd凭据接收/续会话或实际写盘器。正常runner只能持锁等待或证明没有任何permit的写盘前失败/取消；不能生成成功。未来一旦发过permit，缺终态回执必须unknown并保留资源对账，不能借此分支解锁。启动后first-boot host-key绑定、真实SSH首连/修复、七阶段投影与真机PoC仍未完成，WP-1.7/P1保持partial。055未应用，运行库/3000/5173不变。

启动签名错绑/重放/篡改/边界、原生Execution默认关闭/缺配方/幂等/无重试/与Agent互斥、无效initrd启动前失败/伪成功拒绝、未开始取消、撤域、未激活租约回收与资源对账用例只写入未运行；provision-only集中入口已覆盖新组。实际执行定向go test -run '^$'编译（/tmp/keel-install-session-{compile,final-compile}.log）、go vet（/tmp/keel-install-session-vet.log）、OpenAPI生成（/tmp/keel-install-session-api.log）和直接tsc（/tmp/keel-install-session-tsc.log），均退出0；生成首次从web目录查找不存在的本地binary退出127，改用仓库根既有工具成功。SQL/功能/CLI/浏览器/网络/镜像均未执行。保留026与fabric_precheck两份原未跟踪草稿。用户询问硬件资料，已说明优先提供一台代表性机器的启动MAC/网卡PCI身份、DHCP直连或relay拓扑、磁盘WWID/serial/精确几何及擦除授权；资料未到不阻断独立软件开发。


## 99. 当前启动授权、隔离服务与签名预检回调接线（2026-10-26，源码待集中验证）

接续3b84916。在当前本地`/opt/keel`完成，未使用tmux/SSH。新增平台ProvisionBootAuthority将ProxyDHCP、HTTP/TFTP文件和注册/预检回调接到真实DB安装会话；每次检查当前独占attempt/command/fence/lease、冻结BOM/SN/MAC/公钥/介质批准及完整scope摘要/直接peer，新增介质隔离状态门禁。Lookup保存标为auxiliary_only的DHCP指纹事件，不当作身份认证。文件仅批准角色或确定性渲染，不接受路径/URL；大文件复用原hash/重试/隔离核心，IO期间释放SQL连接与执行资源锁，250ms检查+hash后复查，避免挤占heartbeat连接。worker/文件liveness改读小绑定视图，不重复拉取多MiB历史报告。

新增独立机器协议api/provision-boot.openapi.yaml与严格canonical JSON的HTTPS回调handler，固定enroll/preflight、实际TLS/Host/直接peer、无Cookie/Origin/转发信任、有界并发/正文/读超时；注册Token仅Authorization头，后续只用已注册Ed25519逐消息签名/递增sequence。预检报告以base64保留helper stdout精确字节。网络scope和当前权限在消费proof同一事务核对，不因先前HTTP检查放行。新增显式cmd/keel-provision，默认关闭，仅在两个开关、已批准项目scope、独立公钥、介质存储和受保护TLS证书/私钥齐备时源代码允许在固定批准接口/address运行HTTPS+ProxyDHCP+TFTP，scope失效或任何子服务退出取消全部。未调用该命令、未监听或改NIC/现网DHCP，未加入Compose自动服务。Make及既有ARM CI编译列表加入新命令，未实际构建镜像/运行CI。

initrd复用原固定IP/interface/CA/hostname TLS客户端添加两种一次POST，无重定向/自动重发。受保护bootstrap文件保留为带外输入接口`/run/keel-bootstrap.json`：root PID1 ephemeral环境、tmpfs、单链接root 0600规则，必须在LoadBootstrap读取时已到位；实际硬件带外交付机制仍由PoC接入，不是已实现自动凭据分发。完整bootstrap Spec与独立验证manifest/seed/spec一致才注册新内存启动密钥，成功后删Token文件；缺文件仍只读本地证据，坏文件阻断。有凭据则只读Prepare后签精确报告，回包丢失不盲重发或新建身份；已有本地预检事实仍留tmpfs，callback_status=unknown不冒充控制器ACK。正常回执仅到preflight_ready/failed，不授予写盘。更正OpenAPI firmware枚举为实际uefi/bios并重生TS。

新增HTTP/签名/重放/错源/重复字段/容量、客户端完整Spec错绑/回包丢失/POST不重定向、当前scope精确匹配、脱离SQL事务hash仍复查当前权限且撤销不重试、普通宿主不得读bootstrap等软件用例，均只写入未运行。实际定向go test -run '^$'编译（/tmp/keel-boot-{authority,callback,service,service-final}-compile.log）与定向vet（/tmp/keel-boot-service-vet.log）、OpenAPI类型生成（/tmp/keel-boot-service-api.log）、直接tsc（/tmp/keel-boot-service-tsc.log）退出0；纯JSON/Python脚本语法和文档检查另记录提交前结果。不执行新增功能/数据库/CLI/浏览器/网络用例，不应用SQL、不启服务/重建镜像/发布3000/推送。源迁移52条（不含026草稿，新增部分未应用）与运行46条保持差异；两份原未跟踪草稿和全部运行服务保留。

WP-1.7/P1仍partial：下一步是同一启动身份下的互动预检刷新/审批状态衔接，以及受控writer的独立工具/镜像输入、当前写盘许可与真实写盘回执；随后首启host-key绑定、HTTP+DHCP辅助证据核对、SSH首连/修复及七阶段投影。当前客户端只完成一次注册/预检，不把30秒事实窗口自动延长或历史PASS当当前盘身份，也不把上述源码当实际启动或硬件验收。用户提供PoC环节后再接具体硬件/网络机制，软件正确性与硬件证据分别记录。


## 100. 安装会话与显式选盘/独立审批页面（2026-10-26，源码待集中验证）

接续4f176f5，复用shadcn/ui与既有全局JetBrains Mono，装机准备页增加：准备完成后显式选择已批准网络域并发起独占安装会话、当前会话列表及原Execution入口、签名预检事实下的明确磁盘选择和另一Admin/DL布局审批。无默认磁盘，展示WWID/serial/容量/扇区/device号/diskseq，要求序列号确认及整盘擦除意图；缺失/歧义/占用/只读/可移除/非UEFI或事实超过30秒均不可选。布局展示不可变摘要/精确分区，发起人和选盘人不显示自审入口。具体准入仍由当前后端事务核对，UI不提供Token、写盘许可或手动成功按钮。

活跃列表前台每5秒更新，出错/终态/unknown停止轮询；列表SQL省略多MiB原始报告，显式复核窗口才读取完整事实并固定版本，后台刷新不替换正在确认的证据。中英文文案及错误映射加入。页面刷新不冒称硬件刷新，互动预检仍是后续任务；本批未重建/部署3000或浏览器验证。实际直接tsc与UI静态边界退出0（/tmp/keel-install-board-{tsc,ui}.log）；仅平台编译go test -run '^$'结果见同名前缀compile.log，无新增功能用例运行。无SQL/网络/硬件操作，P1保持partial。


## 101. 显式互动预检刷新及稳定审批等待（2026-10-26，源码待集中验证）

接续2276f9b。利用既有append-only安装事件承载一次待处理刷新请求，不新增迁移或修改055。当前用户/项目/独占租约/来源及精确版本下请求刷新，绑定旧报告摘要/序号；待刷新禁止选盘。同一启动身份的机器review只读签名查询回显随机nonce，不消耗回调序号或业务版本；仅显式请求才再跑只读helper，refresh_id与精确报告原字节共同签名，下一序号一次消费，最多32报告，选盘后不准改报告。页面接请求/等待/新报告版本提示，仍复用shadcn与原字体。

initrd等待受原DHCP/ARP/Spec/context限制，不续租、重启或创建新身份；布局批准仅storage_approved_no_write_permit。首份报告在人工等待前保留，刷新报告独立序号目录，错误/丢ACK不重发、不覆盖原证据。完整协议及输入边界见PROVISIONING最后一节。新增签名错绑/nonce重放/只读不消费版本、显式刷新一次采集/留证/审批不写盘及丢ACK用例仅写入未运行。实际定向Go编译、vet、OpenAPI生成、直接tsc、UI静态检查日志/tmp/keel-preflight-refresh-{compile,final-compile,vet,api,tsc,ui}.log退出0；最后纯测试断言增补不改变产品源码，最终结果见本批提交。未运行功能/PG/CLI/浏览器/硬件，没有应用SQL/监听网络/构建发布镜像。P1仍partial，接下来受控writer的独立输入及实际执行/回执，再首启/SSH/阶段投影。


## 102. 正向签名归档到DB启动授权的集中用例（2026-10-26，仅新增用例源码）

接续b25d290。新增execution软件集成fixture：先经真实介质登记/独立审批和平台manifest签名导出5个独立runtime输入，再构建/另批完整initrd、冻结OS recipe、实际验证来源字节与签名，取得opaque验证结果后按真实claim/lease激活。内部ELF和硬件观察明确为不可执行格式/软件事实fixture，不执行组件或证明可启动镜像/硬件。原故意损坏initrd的负例保留，未改为假PASS。

新组TestProvisionInstallationLiveBootPostgres覆盖当前文件取得/hash、Token一次消费、报告重放、status不改版本、刷新请求不改版本且阻断旧选盘、刷新一次消费、摘要列表不拉多MiB报告/详情保留精确字节、独立布局审批不签permit、选盘后报告拒绝、并发刷新只接受一个、撤域拒绝新回调。已接provision-only集中入口，只执行go test -run '^$'编译（/tmp/keel-live-boot-fixture-compile.log，退出0）；没有执行用例/CLI/PG迁移/镜像或硬件。P1仍partial。


## 103. 写盘器前置GPT字节核心（2026-10-26，源码待集中验证）

接续ed1906b。新增provisionstorage的确定性GPT构造/只读回验，绑定完整Plan摘要、3个唯一显式UUIDv4、精确容量和512/4Kn扇区，输出首尾各1MiB及摘要；主备header/array、PMBR/饱和逻辑块数、CRC、GUID端序和零保留区均固定。Segments返回独立副本，调用方不能修改已准备对象。此核心只处理内存字节和调用方提供的ReaderAt，不打开设备/文件、不写盘/格式化/挂载或签发许可，不把GPT正确当实际安装/擦除完成。

新增512/4Kn/大容量和PMBR非饱和计数、主备CRC/数组/稳定ID/边界、篡改/部分读/换GUID/过期/改布局/副本保护用例，仅写入。实际go test -run '^$'编译与vet（/tmp/keel-gpt-layout-{compile,vet}.log）退出0；随后增补同一测试函数的PMBR非饱和断言，不改变产品源码。格式来源见PROVISIONING，不执行任何设备/磁盘工具。P1仍partial，下一块是独立批准的writer工具输入接入完整镜像，之后当前许可及实际写盘回执。


## 104. 无循环的writer工具输入与v2完整归档（2026-10-26，源码待集中验证）

接续e500c2a。mediaboot新增显式v2 runtime-set/归档，在原5输入外独立批准tool类型/同架构静态unsquashfs、mkfs.ext4、mkfs.vfat，八项ID唯一、精确依赖闭包/签名/摘要/路径和反向实际字节校验；保留v1 JSON/五项布局，不通过追加CPIO旁路。复用原离线CLI/发布保护，没有新增并行生产流程或依赖未来成品。RuntimeSet按值比较可选工具，避免解码后误用指针相等。平台真实源验证保留已验runtime schema/config摘要与工具字节能力，boot.activated审计及CLI明确disk_write_authorized=false；没有增加实际工具执行/设备写入或许可。下一步先完成目标OS安装启动内容契约，不能把iPXE EFI当目标系统bootloader，再许可/实际writer/回执、首启/SSH。

新增双架构v2严格归档、确定性、缺项/改字节/错架构/v1降级/追加归档拒绝、独立签名闭包生产/反向绑定与失败不发布用例，只写入未运行。代码复核另修execution共享临时库fixture：在各自cleanup经真实scope API只撤销自己的域，避免下一fixture与遗留批准CIDR冲突；不删表/数据，不绕过网络冲突规则。实际定向Go编译/vet及最终静态结果记录/tmp/keel-writer-runtime-{compile,final-compile,vet,final-vet}.log，CLI/平台审计补充后的终检编译/vet日志为/tmp/keel-writer-runtime-source-final-{compile,vet}.log，无编译/静态错误；设计检查/tmp/keel-writer-runtime-design.log及git diff --check通过。没有应用SQL、运行CLI/新用例/浏览器、实际归档/镜像构建、网络或硬件操作。P1保持partial；3000/5173、运行46迁移和两份原未跟踪草稿继续保留。

本轮最终只读运行复查（2026-10-26 05:08 UTC）：3000 /healthz返回process级ok、版本6668a72c；keel-api/postgres/minio/redis与auth-test-postgres容器healthy，runsun服务保留，.env权限600。此健康证据不覆盖本轮未发布源码或P1端到端验收。


## 105. 新三小时窗口与目标系统UKI契约（2026-10-26，源码待集中验证）

接续e35f1d9，用户于05:40:29 UTC要求继续三小时，至08:40:29 UTC；随后明确启用全部Agent并将可并行后续任务马上推进，覆盖之前的单Agent偏好。本轮root+3 Agent分工为目标内容/客户端集成、当前写盘许可、受控介质校验、writer。继续本地/禁tmux和SSH、P1完成后集中测试；只必要编译/静态检查，不部署3000或应用SQL/真实设备操作。

新增显式v2Recipe目标UKI契约，v1字节兼容且不能通过ValidateWritable；固定rootfs内路径、精确摘要/大小/kernel release和本地GPT根命令行。新增有界全字节PE/同架构/固定单profile校验，不执行镜像。签名manifest严格字段和API/生成TS及共享UI录入/展示同步。新增格式/错架构/重叠/隐藏命令/篡改及签名拒绝用例仅写入。实际Go编译/vet日志/tmp/keel-target-boot-{compile,vet}.log、API生成/直接tsc/UI静态检查/tmp/keel-target-boot-{api,tsc,ui}.log通过；第一次从web查找不存在tsc退出127，已改用根目录既有工具成功。没有运行功能/PG/CLI/浏览器/固件或硬件验收。P1仍partial，本轮后续许可/介质/writer以新章节为准。


## 106. 四Agent当前许可/写盘/首启源码集成检查点（2026-10-26）

接续0a11d74，4Agent按用户最新安排并行且完成后接后续任务。已接当前write-permit/只读nonce check/一次receipt、运行输入的当前独立审批、完整批准Plan及runtime SHA只读交付、asset同事务provisioning版本切换及自有active校验。受控介质全hash+SquashFS/关键内容+sealed工具，Linux writer8阶段/私有工具jail/O_EXCL整盘与精确loop/回读同步/unknown强持引用已落源码。root将审批等待→staging→独立firstboot key→fresh report→permit→writer→receipt ACK串在同一网络会话，main最早dispatch受保护工具子进程并移除“错误必然未安装”的日志断言。真实首启helper/独立公钥绑定及服务端HTTP+DHCP辅助身份登记已落源码，但仍不建立SSH信任。详情与严格证据边界见PROVISIONING末节。

新增pure protocol/PE/SquashFS/HTTP/工作流/宿主拒绝与真实DB composition软件fixture用例，仅写入未执行；firstboot/write-permit DB组已接集中入口regex。实际只Go test -run '^$'编译/vet：/tmp/keel-rootfs-stage-{final-compile,vet}.log、/tmp/keel-firstboot-source-final-{compile,vet}.log、/tmp/keel-write-permit-{final-compile,vet}.log、/tmp/keel-first-boot-server-{final-compile,vet}.log、/tmp/keel-writer-exec-final-{compile,vet}.log及root全链/tmp/keel-install-chain-compile.log均退出0。没有执行上述功能用例/CLI/实际工具/磁盘/网络/首启/SSH、应用SQL或重建发布镜像。3000仍6668a72c；P1仍partial。下一并行单元为受控重启、真实SSH首连/修复队列及七阶段真实事件投影，不能按这批源码或纯格式fixture判闭环。


## 107. 首次真实SSH验证与显式query恢复源码（2026-10-26）

接续66079ac。独立临时SSH endpoint使用已认证首启报告的新host pin及同peer/批准网段，域私钥只经当前受控vault，复用既有host.identity/helper和fresh pinned握手，不使用旧configured pin或跳过host key校验。存储层签发一次started能力并以opaque transport proof核对同native attempt/command/fence/独占lease；成功时asset变provisioned/version+1、machine记录真正hostpin+当前domain binding/trust established、installation registered与native成功receipt在同事务落库，然后Complete。崩溃跨lease过期仍需显式对账，不宣称reaper会自动完成或解锁。

失败进入明确repair事件；Admin/DL/SRE显式请求仅fresh握手query原不可变receipt，不重发run/重装/续租，同repair并发去重。请求人与原审批/域凭据权限在消费及最终注册再次核对。native command/fence保持原值；unknown/missing始终持资源。已证明terminal failed后的新一次显式只读probe是后续单元，不能靠重复query假造旧失败回执成功。静态协议/loopback/DB角色/过期/stale事件用例均只写入未运行。必要定向编译/vet日志/tmp/keel-first-ssh-query-{final-compile,vet}.log退出0，source/no-network。3000/SQL/镜像未改，P1仍partial。

同批writer交界复查修复受控reboot早退未quarantine，Makefile增加独立build-firstboot并接installer-helpers；仅静态编译amd64/arm64的initrd/firstboot四binary，读ELF头确认静态，/tmp/keel-writer-cross-v0pvsppw，无任何CLI/归档/镜像/设备执行。written回执ACK的最长2秒当前lease重启许可已接入root运行链；失ACK、unknown、过期或重启失败不重试。


## 108. 四Agent成果统一集成并恢复单Agent（2026-10-26）

接续39c353f。三个子Agent均完成并停止，无后续派工；用户要求合并后保持单Agent，已同步AGENTS及本文件恢复入口。同一工作区直接统一集成，无需创建分支或重做各模块。

- 机柜批量准备：当前BOM精确rack、1～50个显式资产与逐机完整SN/version/网口/OS/域公钥；后端统一事务、排序machine mutex、完整输入幂等与批次审计，复用原请求创建和独立逐机审批。API/OpenAPI/生成TS/双语shadcn页面同步，支持直达单请求，批量准备不自动审批或启动安装。
- 进度与恢复：七阶段只读投影仅展示持久事实，辅助DHCP、认证环境、写盘自报、首启登记及真实SSH严格分开，缺失证据不补成功；展示实际租约与资源隔离。显式query仅查询当前不可变receipt。只有fresh pinned query证明当前probe终态failed并经人工授权，才产生新只读probe ID；原native command/attempt/fence不变，unknown/missing保持query-only，同一失败和并发恢复互斥。成功注册、资产/trust投影和native receipt仍原子落库。
- 重启边界修复：重启前只清理已卸载的owned loop映射，整盘O_EXCL FD和sealed source保留至真实内核重启。超时、取消或重启syscall失败时Quarantine仍持有真实claim，避免先Close后隔离的资源窗口。

集中入口补入known-failed SSH、机柜与进度用例组，以及writer/firstboot基础包。上述pure/loopback/PG回归用例源码已写入，按用户要求未运行功能测试/真实SSH/磁盘/重启/浏览器。统一编译使用GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json、GOMEMLIMIT=768MiB、GOMAXPROCS=2；统一7个相关包/命令的go test -p=1 -run '^$'编译退出0、6个相关包go vet退出0；TypeScript tsc --noEmit、UI边界、设计检查退出0，git diff --check与集成脚本AST解析通过。日志为/tmp/keel-agent-merge-{compile,vet,tsc,ui,design}.log；编译输出[no tests to run]不记作功能测试通过。没有SQL迁移、镜像构建/发布或push；3000进程健康检查返回6668a72c，数据服务及其他项目保留。P1仍partial。

剩余明确边界：首装临时SSH endpoint虽使用已认证新host pin，operator allowlist加载仍依赖旧pin/旧signer，需独立实现显式provision-only配置，不能要求伪造旧pin。进度投影接受`boot.file_served`白名单结构，但实际完整响应事件生产者尚未落盘，分发阶段缺证据保持pending。受保护bootstrap凭据交付/实际启动环境、完整软件联调及硬件PoC继续按原计划保留，不能因本次合并标闭环。


## 109. 单Agent三小时接续与首装专用SSH allowlist（2026-10-26）

用户于07:13:32 UTC要求继续三小时，当前窗口至10:13:32 UTC。从d118562继续，保持单Agent、直接本地/no tmux/SSH、仅必要编译静态检查及P1后集中验证。生产服务、数据、两份草稿保留，无镜像发布/迁移/push。

首装新增显式provision_only target：只预留当前项目/机器及固定IP端口/非root账号，不要求旧pin/旧signer；拒绝混入旧pin、文件密钥、credential引用/版本/有效期/provider选择，且必须已装vault provider。加载不取钥。普通endpoint路径在任何取钥或连接前直接拒绝，HasTarget保持true防mock回退。仅认证firstboot核验路径在私有copy中绑定新pin+当前批准vault身份，每次fresh握手，不晋升普通连接/配置。旧配置false字段省略，摘要序列化兼容。新增用例源覆盖无旧凭据配置、歧义/重复/无vault拒绝、ordinary能力隔离、真正新pin握手/原回执query/no replay、撤销/错误peer/关闭；尚未执行功能用例。

必要检查：/tmp/keel-provision-only-compile.log（三包仅编译）、/tmp/keel-provision-only-vet.log（taskrunner），均退出0，git diff --check通过；没有执行测试。首装allowlist缺口已接源码；下一步完整文件传输事件生产者，其他联调/PoC边界仍保留，P1 partial。


## 110. 真实完整HTTP分发事件生产者（2026-10-26）

接续5d814ad，单Agent完成隔离Gateway→当前数据库Authority→七阶段投影接线。Gateway对已固定版本且全hash校验的实际响应计数，仅GET/200/完整精确字节/无写错误/当前ctx和最终权限有效时调用server-only记录；HEAD、304、206（即使覆盖全部字节）、错误hash、短写、断流、取消及末次写出后撤销不记完成。ResponseWriter不暴露ReaderFrom绕过计数；Unwrap兼容ResponseController。数据库在短事务重核当前原execution/attempt/fence/独占lease、scope/RBAC/冻结来源、精确角色sha/size/objectVersion；渲染文件共用OpenFile的确定性bytes，不在持SQL时再做对象IO。保持正常锁顺序，并在资源去重锁后重验实际lease截止时间；每installation/resource只有一条full_response/server_write_only事实，不改安装版本、状态或trust。

证据只证明服务器ResponseWriter完整接受了文件体，不证明客户端收齐、校验、执行iPXE或真实启动。回包已提交后若证据落库失败，只留下缺失事实，不修改HTTP响应或伪造成功。进度沿用observed/reported，实际启动与SSH另证；TFTP尚无完整传输记录，本节不替它推断成功。

新增Gateway边界及signed-format/live-DB/真实HTTP软件composition回归用例源码，含并发去重、无状态提升、错误冻结身份/peer/撤销拒绝，已接provision-only集中regex；尚未执行用例。初轮发现新测试误用不存在的fixture.db，已改store.DB；必要复验/tmp/keel-file-delivery-final-{compile,vet}.log三包退出0，git diff --check和脚本AST解析通过。无listener/网络/写盘/SSH/镜像/迁移/push，P1 partial。


## 111. 注册已提交但native完成前崩溃的显式对账（2026-10-26）

从e5aa77e继续。复核发现：真实SSH核验/资产trust/registered/native receipt已同事务提交后，worker可能在Complete前退出；reaper将原attempt置unknown、原独占lease置reconciling，而原注册回执准入仅接受running/live lease，导致既有不可变成功证据也无法走通既有Reconcile。现仅扩展registered分支：在原command/attempt/fence、原机器exclusive reconciling资源、reconciling execution、完整SSH事件转录承诺与实际当前asset版本/hostpin/domain身份全部吻合时，允许原授权Reconcile应用已有native receipt。正常live Complete路径保持原限制；没有续租、重新SSH、写盘、另建attempt或新资源授权。实际资源/资产行在准入事务锁住，错fence/已释放/改变版本/转录不符拒绝。

七阶段资源显示同时修正：registered只说明域投影成功，实际held/reconciling lease仍显示保留；到期或reconciling提示需要执行对账，只有真实released才解除，不覆盖已存SSH成功事实。新增真实DB下游崩溃边界源码fixture明确手工种已提交记录，不冒充实际SSH；覆盖显式对账/幂等/不续租不建attempt及上述拒绝项，已接集中regex。progress pure回归同样只写入。必要两包仅编译与vet退出0，日志/tmp/keel-registered-recovery-{compile,vet}.log；无功能/SQL/浏览器/镜像/硬件测试，P1 partial。


## 112. 从未签发写盘许可的隔离任务人工收尾（2026-10-26）

接续eb3161f，单Agent新增prewrite-reconcile API与双语共享组件入口。仅当前Admin/DL/SRE显式提交准确installation版本、原因和幂等键；执行必须reconciling，原attempt unknown且身份/原机器exclusive reconciling claim一致，安装unknown且permit_issued_at为空，并在正常execution→attempt→installation锁序下再次证实没有write.permit_issued事件。原许可签发无法越过同一锁序并发提交。服务端同事务记录stopped_before_write、原native failed receipt、人工审计、原attempt结束和资源释放；复用原执行settle，不连接目标、不续租、不创建重试/attempt、不改资产/互信。任何发过许可、版本变更、live任务、已失claim或越权都拒绝，unknown写盘结果不借此释放。

进度面板仅在unknown/未签许可/实际reconciling、当前版本和权限吻合时提供显式确认入口，表单网络未知时相同原因保留幂等键。服务端验证始终权威。API/OpenAPI/TS保持一致，安装创建旧描述同步到当前受控writer/首启/SSH源码边界。

新增数据库软件用例源码覆盖并发幂等/一次proof/一次停止、不动资产信任/不重试，以及上述拒绝项；已接集中regex，未运行。必要execution/httpapi仅编译、vet、API生成、TypeScript与UI边界均退出0，日志/tmp/keel-prewrite-recovery-{compile,vet,api,tsc,ui}.log；git diff --check、脚本AST和设计检查通过。3000/SQL/镜像/生产开关未变，P1 partial。


## 113. 孤立首次SSH派发补显式修复项（2026-10-26）

接续2b7ec12。首次SSH派发已持久化，但传输/修复事件写入异常时，后续轮询原来只看见已消费dispatch并返回nil，可能永久不展示query入口。现仅对已认证firstboot、最新dispatch尚无repair且无新授权工作时，补一条verification_unknown修复项；沿原attempt/command/fence，无网络IO、重发run、成功回执或新attempt。事件去重后后续轮询停止重复处理，用户可显式query原receipt。新增DB边界fixture仅模拟已提交dispatch而未做SSH，不冒充真实传输；覆盖两轮只补一次repair、无新receipt或状态成功、无knownfailed新probe授权。源码已接集中regex。必要execution仅编译/vet退出0，日志/tmp/keel-first-ssh-orphan-{compile,vet}.log，未运行用例；P1 partial、3000未更新。


## 114. 装机互信待修复列表（2026-10-26）

接续23cb4a0，单Agent在原装机列表接入严格可选repair_only=true/false，拒绝空值、重复值和其他布尔别名。项目/当前会话权限与分页复用原路径；同一SQL谓词用于总数与页面，仅awaiting_first_boot/unknown且已存在ssh_repair_required持久事件进入队列，不按租约截止时间隐藏未知节点；registered/failed/canceled退出。列表继续省略完整预检报告，不逐行额外查进度或创建队列任务。

双语共享Button筛选、独立查询缓存、切换回第一页、空队列提示与OpenAPI/生成TS同步；逐行展开沿用原进度、显式query/经证明失败后的只读核验授权，不提供批量推送或自动重装。过期任务的现有资源隔离和权限不因可见性而放宽。

新增数据库回归源码覆盖仅首启报告不入队、持久修复入队、精确分页/总数、摘要不加载报告、reaper后unknown保留、跨项目/匿名/坏分页拒绝与终态退出；终态种子仅用于过滤断言，不作为真实硬件结果证明。已接集中入口，按用户安排未运行功能用例。三包仅编译、vet、API生成、TypeScript与UI边界检查退出0，日志/tmp/keel-repair-list-{compile,vet,api,tsc,ui}.log；设计及diff检查另见同前缀design日志。3000、SQL、镜像和生产开关未变，无push；WP-1.7/P1 partial。


## 115. TFTP完整发送与最终ACK后的分发事实（2026-10-26）

接续26062b5。原Linux隔离TFTP socket路径在每次WriteToUDP返回全长且无错误后才确认当前精确DATA字节，顺序累计一次、超时重传不重复计数；空终止块仍须发出并收到最后ACK。最终ACK沿原客户端IP/port TID与interface校验，全部字节/块一致且文件成功关闭后，强制再验当前权限再调用原FileDeliveryAuthority；部分/跳块/改字节/关闭/无最终ACK/取消/撤销不记完成。纯OpenTFTP/Next消费不等于已发送，也不能记录事实。证据存储失败只缺证据，不重发、不假成功。沿用现有按资源去重的boot.file_served及server_write_only语义，不证明客户端执行、iPXE/OS启动或SSH互信。

新增无socket的传输边界用例源码覆盖1/511/512/513/1024字节、整块末尾空包、重传不重计、无发送确认/最终ACK、提前关闭、撤销、取消、落库失败只调用一次及篡改/跳块拒绝；未执行用例，不冒充真实UDP或硬件验证。集中入口既有provisionboot全包已覆盖新测试。三包仅编译与provisionboot/platform vet退出0（/tmp/keel-tftp-delivery-{compile,vet}.log）；设计/diff检查记录同前缀design日志。无镜像/迁移/生产listener/push，3000未更新，WP-1.7/P1 partial。


## 116. 已注册后的普通SSH配置状态（2026-10-26）

接续2b114ca。沿WP-1.7→WP-1.5接续复核确认：真实首次SSH互信与普通operator配置是独立边界，provision-only不能被成功回执隐式晋升。原普通任务服务端已校验实际新host pin、精确已证明vault域身份和当前权限/资源；本次只补原进度API的registered后只读配置状态及双语显示，区分matched/configuration_required/identity_changed/credential_unavailable。读取当前资产provisioned、真实ssh trust/pin及完整公开域凭据，并在同事务核对当前未撤销/未过期的公开域钥版本；普通配置必须当前pin、vault引用/版本/有效期和已加载signer公钥一致且runner未关闭。配置比较不调用Resolve、不连接或修改operator文件，不推断worker/网络可用、无新SSH证明或任务授权，原七阶段持久验证事实保留。

提取原CheckActiveCredential公共配置比较以复用，实际执行仍重新Resolve当前vault权限。新增纯无socket配置边界源码覆盖匹配、wrong pin/target/credential、provider不可用仍只读比较但执行拒绝、关闭/nil/provision-only不晋升；新增合成注册DB投影源码覆盖未配置、当前trust撤销和域钥撤销且不覆盖历史SSH成功，已接集中入口。按用户要求未运行用例。相关三包仅编译/vet、API生成、直接TypeScript/UI边界检查退出0（/tmp/keel-provision-config-status-{compile,vet,api,tsc,ui}.log），design/diff及脚本AST另记同前缀design日志。无SQL/镜像/push/生产连接，3000保持原版，P1 partial。


## 117. 已知失败后的显式批量只读核验（2026-10-26）

接续87b6041。在现有修复队列加入1～32个逐机显式选择的批量核验，复用原单机已查询终态failed的严格准入和worker路径。按installation稳定排序，单事务检查当前Admin/DL/SRE、精确版本/最新repair与failed事件、不可变原命令、批准来源/域钥及原live独占claim；任一成员失败，已写其他成员授权/审计一并回滚。提交前以clock_timestamp再核对所有最早选择节点的真实租约/安装截止时间。未知/缺失/未经query的失败不接受，已消费的失败不能新授权。新probe仅host.identity，原native command/attempt/fence及隔离规则不变，不重装/续租/推送凭据。

同actor/project的Idempotency-Key绑定规范化完整选择和原因，并发去重；相同已接受请求可在worker消费后返回原授权ID，改变输入冲突，不造成run重放。UI仅在逐机已知失败/可用/held未过期时提供加入选择，最多32，跨分页保留明确选择且可清空；确认表单展示SN/版本和精确证据，原因改变换幂等键，网络不明重试保留键。项目/权限丢失清空选择。API/OpenAPI/生成TS/双语共享组件同步；接收只代表授权，仍等待真实逐机结果，不宣称完整批量公钥修复。

新增合成已查询失败DB边界回归源码，明确不冒充SSH测量，覆盖有效首成员后缺失成员的整批回滚、重复节点拒绝、并发同key一次授权、输入变化冲突、原native身份保留、消费后原key返回和新key拒绝旧失败。已接集中入口，按用户要求未运行。三包仅编译/vet、API生成、TypeScript/UI边界退出0（/tmp/keel-ssh-batch-{compile,vet,api,tsc,ui}.log）；设计/diff/脚本AST见同前缀design日志。3000、SQL、镜像/生产开关未改变，无push。WP-1.7/P1仍partial；实际传输联调和PoC独立验收。


## 118. 新三小时窗口与OS/model金丝雀放量门禁（2026-10-26，未验证源码）

用户明确要求持续开发三小时，本窗口08:20:18～11:20:18 UTC，完成单个步骤后继续推进，不提前结束。最新要求以P1实现为主，集中验证由用户在P1闭环时明确安排；本节没有运行Go/TS/静态/功能/PG/浏览器/镜像检查。仍单Agent、/opt/keel直接本地、禁tmux/SSH、不迁移/部署/push，两份原fabric草稿保留。

接续58f40ce，FR-2.5/WP-1.7新增不可变rollout intent表（056，未应用）和明确canary/validated输入。身份绑定同项目、不可变型号版本/内容摘要、架构和完整批准OS角色配方/对象版本/摘要；节点SN/hostname/公钥不同不造成独立OS发布身份。按身份事务锁限定最多两台不同金丝雀机器，同机器只有最新一次canary会话可作证据；失败、unknown、未完成或只registered但原执行未settle都阻断放量。validated必须引用全部1～2个最新金丝雀，不能漏掉失败节点或引用旧成功；首次validated准入后冻结该组金丝雀选择。既有会话没有推断性回填。

金丝雀证据核对持久真实SSH事件转录承诺、精确原command/attempt/fence、实际host.identity终态/hostname/arch/域公钥身份、原native成功receipt及execution/attempt成功、原独占claim已released；replacement probe须有同原attempt/fence的不可变授权事件。持久历史OS成功证明与当前目标/介质/凭据审批分别检查，不从绿色状态或HTTP/TFTP写出推断成功。安装创建、派发和当前write-permit前接入门禁；不替代独立磁盘审批或签发写许可。

原安装表单明确选择金丝雀/已验证放量，展示同组已有金丝雀及阻断状态；原进度API/UI展示冻结策略与证明ID。OpenAPI和生成TS同步；生成命令npm run api:generate成功只记产物生成（/tmp/keel-canary-api-generation.log），没有类型/构建验证。已有安装fixture显式选择canary；新增纯规范化/完整身份绑定与合成注册DB证据源码，覆盖未settle不放量、真实原执行收尾后可读证明、跨项目和不可变保护、坏转录拒绝，均未运行。源码53迁移、运行46；3000仍旧发布版。P1/WP-1.7保持partial，未宣称PXE、软件联调或硬件PoC完成。


## 119. 装机OS与驱动包历史基线（2026-10-26，未验证源码）

接续7a70931，沿FR-2.5/WP-1.7复用既有first_boot.identity_reported签名报告及DB事件，新增可选软件观测：OS ID/VERSION_ID、实际启动kernel release、明确dpkg-status/driver-packages-v1范围内已安装NVIDIA/MLX/OFED/RDMA包名/精确版本/架构。Linux helper只读root拥有的有界文件，不执行shell/dpkg/驱动工具；os-release只接标准vendor软链且仅解析数据。dpkg记录严格有界、去重、排序，只认install ok installed；读取失败、畸形、超限整体unknown，范围内空列表不等于没有驱动，库外/runfile安装不在采集覆盖范围。不由包版本推定兼容性或加载状态。

可选字段纳入原签名和报告SHA承诺；旧v1 helper缺失字段仍保留未知，不推断性回填。进度API/UI关联原冻结snapshot/rootfs/kernel精确介质版本和摘要，以及首启观测事件/采集时间/报告摘要。投影只返回结构化公开软件信息，不暴露整个报告/公钥/凭据，不改SSH信任或native成功条件；当前库存与历史首启记录分开。无需新增SQL，源53/运行46维持。

新增签名防篡改/旧报告兼容、dpkg范围/终态/重复拒绝、os-release数据解析及合成首启DB投影用例源码，并接集中入口；均未运行。按用户要求仅源码格式化和API类型生成，生成首次因schema误放responses而失败，已移回schemas后成功（/tmp/keel-baseline-api-generation.log）；未运行编译、静态、功能/DB/浏览器/镜像验证，不宣称P1或PXE闭环。3000/数据库/镜像不变，无push。


## 120. 首启互信失败的受限域公钥恢复（2026-10-26，未验证源码）

接续9ddfc2f，沿FR-2.3/WP-1.7补原装机任务内的凭据恢复：新057表冻结原安装版本/修复事件、原attempt/native command/fence、已签名首启report/binding、新host pin、原批准域公钥和明确operator临时bootstrap公开身份；申请必须确认SN。原批准域钥不可替换，不凭空补旧host pin/临时凭据，provision-only地址配置不足时拒绝申请。独立Admin/DL审批后由原worker消费，保留原资源独占锁和命令身份；受限trust.install使用新辅助command ID和原attempt/fence，先落不可变派发再联网。源码SQL未应用。

复用既有helper追加/ownership边界，不覆盖/接管客户公钥、不安装任意可执行文件、不改密码。单次run后只查询原receipt，新域钥全新SSH握手和不透明transport proof均需成立，成功再排队独立host.identity并走原SSH注册/TrustLog/native成功事务。原任务状态、当前权限/审批者、介质/域钥/firstboot绑定、租约/有效期在消费和最终落库再次约束。申请/审批/撤销/查询绑定actor/project/输入幂等；只能撤销尚未消费申请。消费后无结果或transport失败保持unknown，崩溃孤立派发也必须显式query，不能撤销后重放或续租；在途操作deadline前拒绝孤立查询。读回失败不伪造成功，不释放原claim。独立审批变化仍阻断后续身份核验。

现有进度API和双语shadcn操作页接申请/独立审批/撤销/原回执查询，展示冻结公钥版本/host pin/申请审批人/到期与状态；active mutation期间阻断只读恢复操作交错。新增合成DB边界及localhost协议用例源码，覆盖无临时凭据、自审批、原fence、幂等、一次派发、零证明拒绝、unknown保留和query原命令、新钥重新握手等，未运行；集中入口已登记。仅格式化/API生成（/tmp/keel-key-repair-api-generation.log），无编译/静态/功能/DB/浏览器/镜像验证。顺带将§119驱动列表上限收紧为32以适配原DB事件16KiB包络，超限unknown不截断冒充完整。源54/运行46；3000不变，无迁移/构建/部署/push。批量凭据恢复入口下一步接续，P1/WP-1.7仍partial。


## 121. 同原任务的显式批量凭据恢复（2026-10-26，未验证源码）

接续034b8a4，将单机申请和审批/撤销/查询抽出同事务内部路径，原单机API保持相同权限/独立审批/冻结身份/未知不重放约束。新增明确action及1～32个唯一installation ID的批量API：create逐项确认SN和修复事件/安装版本；approve/revoke/query绑定既有repair ID和repair版本，拒绝混入创建字段。按installation排序，整批同actor/project/完整规范化输入幂等；任一节点失效整批回滚。事务末clock_timestamp重新核对全部原exclusive held claim、真实attempt/installation期限和适用repair到期；不把批量授权当执行成功，不生成额外execution/attempt/fence或续租。

安装页逐项加入同一种批量操作，可跨页保留明确选择，最多32台。确认弹窗逐节点展示SN/版本及适用冻结host pin/域公钥/申请者，逐项勾选后统一填写原因提交；无默认全选或自动独立审批。actual worker仍消费每台原安装任务。项目/权限/查询错误变化清空选择；服务端仍逐项重核，不信任浏览器缓存。

增加规范化/去重/边界/创建动作字段隔离用例源码，并在既有合成DB用例加入首个有效成员后缺失成员整批回滚、合法独立批量审批和幂等回放源码，集中入口同步；均未运行。仅格式化和API类型生成（/tmp/keel-key-repair-batch-api-generation.log），按用户要求不编译、不跑静态/功能/DB/浏览器/镜像检查。源54/运行46、3000旧发布版均未变，无迁移/部署/push。WP-1.7/P1保持partial；真实PoC和集中验证由用户安排。


## 122. OS选型联动驱动依赖及矩阵出处（2026-10-26，未验证源码）

接续08de4c1，FR-2.5新增项目作用域只读OS依赖API，复用已批准介质DAG/架构/摘要/隔离态约束，返回精确驱动版本及原登记兼容矩阵来源/版本/report_ref。metadata读取不流式校验镜像或驱动对象，不引入大文件重复读取；原准备/执行全量完整性门禁保留。跨项目/非成员、非OS/未批准/隔离、失效依赖均拒绝。空依赖或缺矩阵显示未知，不把依赖关系、CPU架构匹配当兼容结论，也不推定驱动已经预装或现场已安装。

单机装机准备表单与机柜逐机准备复用同一按用户/项目/介质分key的只读展示组件，OS选择后联动驱动精确版本/摘要/架构与矩阵出处；完整组合逐行展示，禁止跨行混搭。实际预装信息仍来自§119首启观测。既有OS composition用例新增metadata无对象提供者可读、精确driver/matrix、空列表、跨项目及retired依赖拒绝的源码，未运行；仅格式化/API生成（/tmp/keel-os-dependency-api-generation.log），没有编译/静态/功能/DB/浏览器/镜像验证。源54/运行46、3000旧版，无迁移/部署/push，P1仍partial。


## 123. 凭据恢复区分已查询终态失败与unknown（2026-10-26，未验证源码）

接续e01bb81，修复只读回原trust.install回执时，以原批准bootstrap binding全新固定pin连接查询确认终态failed，生成独立不透明失败proof；该proof绑定原命令/attempt/fence、首启plan、精确bootstrap公开身份和新鲜查询，不从认证失败、丢包或回执缺失推断终态。真实success仍必须新域钥全新握手查询原回执，不能用bootstrap成功回读替代。

单机/批量/worker共用的恢复结果增加failed终态：只在真实查询失败proof有效时落库，保留原机器资源和unverified，不排队身份成功核验；允许重新申请独立审批来恢复同一冻结域钥，不自动重放且不推断先前零副作用。unknown/孤立派发仍禁止新申请或撤销绕过，只能查询原命令；helper客户条目保留与ownership拒绝边界保持。057尚未应用，直接补该未执行schema的failed状态/合法转换/审批约束。

localhost用例源码增加ownership缺失拒绝后的终态查询、失败proof不可作成功、跨bootstrap拒绝及失败回执query不重放断言，未运行。仅格式化/API生成（/tmp/keel-key-repair-outcomes-api-generation.log），无编译/静态/功能/DB/浏览器/镜像验证；源54/运行46、3000旧版，无迁移/部署/push，P1仍partial。


## 124. 显式DHCP厂商格式接入真实Kea与DORA（2026-10-26，未验证源码）

接续56a7877，继续P1 WP-1.17。先确认批准最小管理渲染/双通道/实际可管理gateway已存在，没有重复实现或引入P2生产网络配置。本次把原模拟60/43字段接真实适配：运维显式profile提供版本/公开出处/VCI/NOS意向/两个厂商子选项编号，严格TLV/文本/255字节边界，无猜测厂商编号或生产默认值。可选配置复制后纳入N-S3指纹和冻结spec；原事务准入/共享Execution/控制端锁/结果未知隔离均复用。

精确SoT/canary预留增加二进制43和平台所有权标记；外部43/自定义43解释/独立元数据拒绝覆盖，旧平台239/143预留可显式升级。option60保留客户端语义，只记录实际探针请求发送值，不伪造服务端返回。启用profile后Offer和Ack均核对精确43字节，证明绑定profile摘要；真实路由还要求该证据匹配原spec。无profile的旧功能继续可用但UI明确不证明43生效。无设备版本兼容/安装成功或managed结论，实际厂商格式和Kea归一化差异留PoC。

OpenAPI/生成TS及双语复核详情同步；新增格式/边界/所有权/指纹绑定与UDP缺失43、Ack变更等用例源码，未执行。仅格式化/API生成（/tmp/keel-dhcp-vendor-api-generation.log），无编译/静态/功能/DB/浏览器/镜像验证。源54/运行46、3000旧版不变，无迁移/部署/push；单Agent继续，WP-1.17/P1保持partial。


## 125. 批准脚本介质接真实SSH执行（2026-10-26，未验证源码）

接续eaf9777，单Agent推进WP-1.5，不拉入P2驱动/网络业务任务。批准Linux noarch工具脚本1～8192字节，登记固定shell/python契约，独立复核批准后从唯一介质源读取精确对象版本；不接受API内联代码/解释器路径/任意参数/环境。冻结版本/SHA/大小/对象版本/批准身份，创建及实际派发重验；当前子集拒绝依赖/兼容矩阵/OS boot，不冒充安装和矩阵完成。新增分页候选API、显式1～32节点脚本API及双语资产页确认；只读入口仍拒绝脚本。全链默认关闭，API/worker/helper分别显式启用，实际配置未改变。

任务复用原Execution/attempt/fence/outbox/未知对账，无自动retry，设备+SSH均exclusive。派发前持久一次消费原命令和实际授权/租约时间，期限不超过原live lease及任务timeout；重启/重复调用不得再run，恢复读取原命令query。心跳检查批准/权限失效请求停止，不从失效推断零副作用。专用非root helper校验正文摘要、固定并钉住root所有解释器fd，私有workspace/固定无秘密环境；每流最多1MiB观测、512字节base64前缀及摘要/完整性。子reaper收割setsid/双fork后代，只有主进程及完整后代ECHILD才写停止终态；未在2秒内证实则unknown保留资源，不把断连当已停止。不回滚外部副作用，外部服务创建的作业不在本前台脚本子集承诺范围内。

新增命令/停止证据、独立helper子进程、实际PG批准介质及既有真实SSH身份后的exclusive/一次派发/unknown隔离用例源码，接既有集中入口；全部未运行。仅gofmt及API类型生成（/tmp/keel-ssh-scripts-api-generation.log），无编译、静态、功能/PG、浏览器或镜像验证。无SQL新增/应用、构建/部署/push，源54/运行46、3000原发布版不变。WP-1.5/P1保持partial；Agent脚本及完整任务目录下一步按依赖接续，不将本子集算整体闭环。


## 126. 批准脚本接Agent通道（2026-10-26，未验证源码）

接续475900e，单Agent推进WP-1.5。复用批准脚本解析/冻结、执行attempt/outbox/授权消费、exclusive设备锁及持久回执，增加独立Agent脚本API与按transport分页目录。只读API仍拒绝脚本及媒体字段；当前mTLS/精确资产身份、当前操作员权限、原脚本批准、两项exclusive claim和原派发事件均参与准入。原授权期限不可重连/续约更新，恢复人工或后台query读取同一原命令；unknown仍隔离，禁用脚本后历史query/cancel不重新取正文或要求仍处批准态。

目标默认scripts_enabled=false，控制面KEEL_AGENT_SCRIPTS_ENABLED=false；实际配置未修改。Agent通过固定root所有helper fd启动独立非root进程，固定无继承环境和私有回执目录，不在常驻Agent设置subreaper。helper自行执行/停止/后代清理；取消先TERM留出清理期，缺停止证明保留unknown。任务帧上限14336，仍在原16KiB gRPC限制内；Request统一校验执行正文/非执行无正文。资产页复用同一批准脚本表单及现有Agent候选列表/本地到期，创建后仍进入原执行中心。

新增PostgreSQL真实批准/默认关闭/不可重放/设备排他/原授权查询与Agent本地opt-out/历史查询、请求正文边界用例源码，均未运行。只做gofmt与API类型生成（/tmp/keel-agent-scripts-api-generation.log），无编译、静态、功能/DB/浏览器/镜像验证；无SQL/迁移、构建/部署/push。源54/运行46，3000原发布版，原fabric草稿保留。WP-1.5/P1仍partial；完整任务目录、G3+安装/systemd、CA轮换和规模/硬件PoC未闭环，集中验证由用户安排。


## 127. 双通道脚本结构化结果（2026-10-26，未验证源码）

接续ab8512f，补FR-1.3首批脚本结构化结果子集：两通道输入显式result_format=json，冻结到Task和派发命令/摘要，原只读端点与sim Flow拒绝真实脚本字段。固定UTF-8单对象，原文/规范化各≤2048字节、深度8/值128/键128字节，拒绝重复键/尾随文档/非有限数值/顶层数组；退出0但JSON无效为明确failed/script_json_invalid，不伪造成功或重跑。原完整stdout base64与大小/SHA/前缀/EOF共同绑定规范化文本，避免大整数经浮点往返丢失；未停止/未完整/非零/超时不生产结构化成功。页面显式选择，原Outputs展示结构化结果并沿用回执导出。同步修正通道目录客户端transport可选参数接线。

新增严格解析边界、完整回执篡改拒绝、专用helper实际执行/查询以及Agent冻结字段用例源码，均未执行。仅gofmt和API生成（/tmp/keel-script-json-api-generation.log）；无编译、静态、功能/DB/浏览器/镜像验证，无SQL/部署/push，源54/运行46及3000旧版本保留。单Agent继续，P1仍partial，统一验证等用户安排。


## 128. 脚本登记/上传/审批入口贯通（2026-10-26，未验证源码）

接续8c9e9de。接入介质登记UI时发现既有上传强制把manifest_ref写为实际artifact摘要，§125～127将用途放入manifest_ref不能经真实上传保留；本节修正当前契约，不声称先前源码已可用。用途改为严格deps.task_script（固定version与shell/python），与实际上传摘要/外链来源独立；补原介质表单选择和审批用途提示。登记即拒绝错误type/arch/OS/硬件范围、空/未知profile、依赖/矩阵/OS boot及0或>8192字节，普通manifest/文件名不赋予执行权。候选SQL、解析/冻结及两通道用例同步，批准/实际对象/权限/独占/一消费约束继续复用。原IngestMedia摘要写入逻辑保留，无历史生产数据迁移。

新增登记边界用例及真实上传后manifest摘要/profile并存断言源码，现有PG/SSH/Agent批准链改用真实新元数据；均未运行。只做gofmt和API生成（/tmp/keel-script-media-profile-api-generation.log），无编译、静态、功能/DB/浏览器/镜像验证；无SQL/迁移、部署/push，源54/运行46和3000旧版保持。单Agent推进，P1仍partial，集中验证由用户安排。


## 129. Agent网口观测/增量与资产页（2026-10-26，未验证源码）

接续bd2ed52推进WP-1.5/F-1.2的网口增量子集。默认关闭，目标非root显式配置读取Linux当前接口/operstate，来源agent，仅索引/名称/MAC/MTU/admin/operstate，不推断健康、LLDP、角色或门禁。最大64接口/12288字节，读取任一接口失败/超限都明确unavailable，不发送部分完整列表。新连接先full，再只发变化和显式删除；客户端仅ACK后接续基线，失败保留基线，重连重建。原16KiB gRPC限制不变。

新增058当前观测表，凭据/实际证书摘要/连接/序号和采集/接收/最后成功时间均留出处；与当前身份、精确资产和原心跳顺序更新同事务，无同连接full基线则拒delta并回滚。旧/禁用心跳不创建观测行，已有列表保留但标disabled；失败保留历史，30秒或断连stale。当前项目成员API及现有Agent列表对话框接入，明确历史/未知/失败，不用心跳替代库存新鲜；本地TTL及凭据到期停止显示近期观测。完整缓存清单/文件分发、遥测和硬件PoC仍缺，P1保持partial。

新增协议/增量/合并/边界、固定路径采集fixture失败不产生部分列表、真实隔离mTLS/PG/full→delta→unavailable→disabled→stale/无基线拒绝及受限runtime角色用例源码，未执行。仅gofmt、proto与API生成（/tmp/keel-agent-network-inventory-api-generation.log）；首次proto因插件未在PATH失败，改用既有/home/ubuntu/go/bin插件后生成成功，非验证。无编译、静态、功能/DB/浏览器/镜像验证，无迁移应用/部署/push；源55/运行46，3000原发布版与原fabric草稿保留，单Agent继续，集中验证等用户安排。


## 130. 固定脚本缓存归属与双通道分页查询（2026-10-26，未验证源码）

接续8df895f，推进WP-1.5/1.10。目标显式缓存开关才把批准脚本写固定/opt/keel-cache/media，要求当前非root平台所有者/0700，无祖先或叶symlink；实际正文hash/大小先校验。0400对象及0600归属索引由共享flock/临时写/fsync/不覆盖rename/原子索引管理；无归属同名文件（含中断孤立对象）不认领，内容/索引不符不覆盖或删客户文件。64个不同摘要/每脚本≤8192字节；命中实际重验后跳过对象重写，仍传输本次批准正文，不声称免传/大文件分发。缓存持久状态不明保留unknown，明确缓存拒绝且未启进程单列failed。

host.cache.inventory接原SSH/Agent只读API/Execution/同设备资源键/回执，默认控制面和目标分别关闭。每页16项实际读回大小/SHA；总登记数不伪装其他页已校验，私有种子媒体/对象引用不出公开清单。续页cursor+manifest摘要冻结，限定单目标，变化显式失败；broker核对原参数，禁用后历史query/cancel仍可恢复。资产页原查询表单和Outputs分页动作接入，下一页是同通道/目标/原清单摘要的新只读Execution，旧回执不变。Agent缓存由独立helper执行，清单查询不建子进程；配置前置/固定所有者均明确，无自动创建客户目录或账号。

新增实际私有文件夹的归属/命中/损坏不覆盖/孤立文件不认领/symlink与分页快照、Agent opt-out历史回执及真实PG参数冻结/当前broker授权用例源码，未执行。仅gofmt/API生成（/tmp/keel-owned-media-cache-api-generation.log），无编译、静态、功能/DB/浏览器/镜像验证；无SQL新增/应用、部署/push，源55/运行46和3000旧版保留。通用大文件/断点续传/缓存免传/缓存心跳清单/完整目录及P1仍partial，单Agent继续，集中验证由用户安排。


## 131. 批准脚本显式缓存优先与命中免传正文（2026-10-26，源码未验证）

- `/opt/keel`，分支`dev/p1-network-bom`，接续`3326455`。SSH/Agent脚本创建增加默认false的`prefer_cache`，冻结到Task/原命令，页面改选后重置确认，普通只读/sim入口拒绝脚本缓存策略。原命令摘要及Agent服务端冻结校验包含该策略。
- 唯一派发授权持久后做最长1秒只读`action:cache`探测，不创建目标command目录/receipt/fence，不证明任务已接收或执行；Agent broker以run同等当前用户/项目/租约/介质审批/独占资源/原授权期限校验。关闭、未命中、忙或未知只在唯一run之前回落到发送批准正文。
- 只有严格绑定原命令、实际归属/大小/摘要命中的响应，唯一run才发`use_cached_script:true`并省略正文。目标持共享锁重读/核对归属文件后执行核对过的内存副本；探测后缓存被改/移除/禁用明确进程未启动失败，绝不补传或第二次run。派发后的崩溃/失联仍unknown隔离，历史query/cancel使用原策略不重授权。
- 补充缓存篡改/未归属/禁用/探测不写fence、请求元数据/正文互斥、非绑定响应拒绝、目标禁用后明确未启动及重复正文run只返回旧失败、PG原授权下探测缺失回落且只派发一次的回归源码，均未执行。
- 实际命令仅源码编辑、受既有overlay的定向gofmt及`npm run api:generate --workspace=@keel/web`（产物生成，日志`/tmp/keel-script-cache-probe-api-generation.log`），未运行测试、编译、静态/数据库/浏览器/镜像验证。3000保持`6668a72c`，源55/运行46迁移不变，未应用SQL/部署/推送；两份fabric草稿保留。WP-1.5/WP-1.10/P1仍partial，完整大文件传输/续传、缓存淘汰/清理/心跳清单及规模/PoC不在本增量内。


## 132. Agent缓存归属索引心跳与资产页（2026-10-26，源码未验证）

- 接续`2384930`，`/opt/keel`/`dev/p1-network-bom`，单Agent继续。原目标`cache_inventory_enabled`与控制面`KEEL_TASK_CACHE_INVENTORY_ENABLED`默认关闭；启用后心跳完整上报固定缓存平台归属索引，最多64条SHA/大小、8192字节、清单摘要/采集时间，无seed项目/媒体/对象引用，不核对全部文件正文。
- proto新增可选缓存字段，兼容旧Agent无字段。客户端发送/服务端接收上限32768容纳网口12288+缓存8192，出站及任务/回执原边界保留。新增059观测表，与实际证书/凭据/连接/心跳序号更新同事务，非法帧整笔拒绝；失败保留明确历史、旧/关闭不创建行、30秒或断连stale。当前身份/资产/项目成员API与原Agent列表缓存弹窗接通，固定`content_verified:false`，仅登记索引不冒充内容核验或批准介质。
- 协议容量/坏摘要/重复顺序/时间/来源泄露拒绝，真实私有缓存损坏后索引仍只为声明而实际任务拒绝，mTLS/PG历史/禁用/TTL/坏帧序号回滚及受限runtime角色回归源码已补，均未执行。真实缓存命中/执行仍复用前两步核对实际字节的链路。
- 仅源码编辑、既有overlay下定向gofmt、既有protoc及API类型生成（`/tmp/keel-agent-cache-heartbeat-api-generation.log`），不记作验证通过。未跑测试/编译/静态/DB/浏览器/镜像，059未应用；源56迁移（排除026草稿）/运行46，3000继续`6668a72c`，未部署/push，两份fabric原草稿保留。完整文件双向传输/断点续传、缓存容量/淘汰/清理、CA轮换、G3+安装/systemd、完整任务目录及规模/硬件PoC仍待，WP-1.5/WP-1.10/P1仍partial。


## 133. 原过期脚本派发显式取得未启动证明（2026-10-26，源码未验证）

- 接续`a00ae37`，`/opt/keel`/`dev/p1-network-bom`，单Agent。补唯一派发授权先消费、但目标接收前崩溃时的受控恢复缺口：当前Admin/DL/SRE以原unknown版本/显式确认/幂等键请求，API只记录原outbox+审计，绑定原command/attempt/version/requester、5分钟有效。无原派发事件、非脚本、非unknown/版本不符或原授权未过期拒绝。
- worker独立有界轮转扫描显式请求，重查目标绑定/请求人当前权限；Agent broker也按原命令/请求/权限验证。目标settle不带正文、不执行、不消费全局fence；只有原授权过期、独占同run.lock且无任何durable accepted才持久明确未启动失败。已有accepted/running、锁忙、坏回执不推断停止；已有真实终态原样返回，晚到旧run只读原回执。脚本开关关闭后仍可处理历史证明，不给予新执行权限。
- worker仅recordHost原证明，不Reconcile/放锁；原尝试抽屉增加显式请求与手动刷新，匹配证明后继续既有说明/版本/权限显式对账。网络错误、请求登记、文件不存在或人工说明均不能单独释放隔离；请求过期/撤权阻断新证明处理。
- 新增目标时间/状态/锁/坏文件/晚到run不执行，以及真实PG未知脚本原派发下显式请求幂等、无证明不放行、请求人禁用阻断等回归源码，未执行。仅overlay定向gofmt与API产物生成（`/tmp/keel-script-nonstart-api-generation.log`）；未跑测试、编译、静态/DB/浏览器/镜像，未新增/应用迁移，源56/运行46、3000旧版`6668a72c`、两份fabric草稿保留；未部署/push。P1仍partial，集中验收由用户统一安排。


## 134. 同快照执行事实摘要报告与导出（2026-10-26，源码未验证）

- 接续`a71788b`，`/opt/keel`/`dev/p1-network-bom`。新增项目成员只读`GET /executions/{id}/report`及原执行中心导出JSON，`execution-evidence-report/v1`；同一repeatable-read事务复用overview计算，Flow版本/hash、目标集合hash、冻结时间、当前状态、计划/未观测任务槽位、历史匹配回执数量及来源、unknown实际持有资源数和原授权分页证据引用共同冻结。
- `complete`只表示全部计划槽位已终态，不等于success；固定hardware_acceptance/gate_evidence=false，显式未知需对账、未完成/无回执等warning。只汇总原有事实，不修改状态/放锁，不携带全量目标回执、正文/凭据/原始日志，避免10k目标摘要导出时放大全部证据。未声称通用report.aggregate Flow任务或签署归档完成。
- 原真实PG脚本用例补未产生尝试的计划槽位、unknown与资源隔离不伪装完成/验收、冻结身份、跨项目及正文不泄露源码断言，未执行。仅定向gofmt及API类型生成（`/tmp/keel-execution-report-api-generation.log`），未运行测试/编译/静态/DB/浏览器/镜像验证；无迁移新增/应用、部署/push。源56/运行46、3000`6668a72c`及两份fabric原草稿保持，单Agent继续；P1仍partial，集中验收由用户安排。


## 135. 脚本工作台事实提示与回执摘要（2026-10-26，源码未验证）

- 接续`c62ddbb`。原工作台按真实冻结task_type区分批准脚本与只读SSH/Agent任务，修正脚本页误沿用「只读」文案；Outputs/尝试详情从已记录host来源回执展示实际启动、停止证明、缓存核对、退出码、失败原因、可用摘要与原过期派发未启动证明，不用前端推断执行或验收成功。未启动明确没有脚本进程待停止；原始回执/结构化结果继续保留。
- 原未启动证明请求幂等作用域补绑定项目/执行/尝试，避免跨项目复用旧key返回其他作用域响应；worker/broker请求授权同时要求原runner与execution mode一致。同步补§134追踪备注（前次追踪编辑脚本缺少可选evidence字段时中止，改为兼容缺省字段后已写入；不涉及任何验证执行）。
- 本步仅源码/文档编辑及两份Go源定向gofmt，无契约变化，不重复API生成。未运行测试/编译/静态/DB/浏览器/镜像，未新增/应用迁移、部署/push；源56/运行46、3000原版`6668a72c`与两份fabric原草稿保持。所有本轮源码等待用户安排的P1集中验收，不宣称闭环。


## 136. 用户调整当前P1扇出为200台（2026-10-26，未执行验收）

用户明确将当前P1逻辑设备扇出由2,000台降为200台，后续再优化。PRD阶段说明、IMPLEMENTATION-PLAN P1出口/WP-1.2/WP-1.9/CI档及ACCEPTANCE规模口径同步；当前P1以200台验收，原2,000不是本轮闭环前置，万台与其他领域规模指标不变，不将小档结果标为大档通过。成功率≥99.5%、介质校验失败0和权限/恢复要求保留。

keel-sim及两份执行模拟/工作台集成脚本默认200，显式2,000档和CLI上界保留，200台使用capacity场景标识并记录实际targets。AGENTS/追踪同步。本步仅编辑及CLI源gofmt，未运行模拟、测试、设计检查、编译或浏览器/镜像，不改变运行服务/数据库。接续任务为批准介质的可恢复文件分发，单Agent继续。


## 137. 批准制品双通道分发与显式检查点续传（2026-10-26，未验证源码）

- 工作区 `/opt/keel`、分支 `dev/p1-network-bom`，接续 `b694265`，单Agent。所属 WP-1.5/1.10、FR-1.2/1.8：输入为现有独立批准固定对象、双通道身份、Execution/TaskAttempt/outbox、设备资源锁及持久回执。本次输出为控制面→目标固定缓存的首批有界分发，不扩展到P2/P3安装依赖。当前P1逻辑设备范围仍200台，未执行规模验证。
- 新增 `POST .../artifact-distributions`，显式1～32目标、介质、SSH/Agent及100～30000ms窗口。冻结批准/更新时间/对象版本/大小/SHA；创建审计 `media.distribute`，派发事件 `artifact.dispatch_committed` 只消费一次；原exclusive设备与通道锁、当前权限/身份、默认关闭、unknown隔离及人工恢复复用。原只读API与sim Flow不接受制品执行字段。源读取在短事务提交后进行，完整校验固定源对象，结尾重核审批；连续源摘要错误走原隔离逻辑。
- 目标固定 `/opt/keel-cache/artifacts`，运维预配非root0700，无symlink。新缓存与小脚本缓存分离，当前脚本清单/心跳不包含它。单对象1GiB、分块8192B、最多64摘要且预留8GiB；受保护owner与实际内容先后fsync，重复已确认分块只核对。完整实际文件大小/SHA匹配后才0400/verified/success；已验证缓存命中重核完整文件并免传源正文。取消/到期query持锁保存paused/停止证明，partial永不标完整；新显式较高fence任务可续传。无归属/落盘与owner不一致保留unknown，不自动覆盖/截断/删除。损坏前缀明确失败，需人工核验。
- Agent append/commit持全局run锁检查当前fence，原连接累计256帧限制改为在途8+最近完成256帧的有界去重窗口，持久命令/摘要/偏移仍防写入重放；单帧/gRPC上限保留。目标 `artifact_transfers_enabled:false` 须配 `tasks_enabled:true`；API/worker与SSH helper `KEEL_TASK_ARTIFACT_TRANSFERS_ENABLED=false`，实际 `.env` 未改。
- 资产页复用当前可信SSH/近期Agent目标和批准已存储介质分页目录，显式确认/稳定幂等键/超时，不接受任意路径或URL。回执页显示checkpoint/完整校验/写入停止，不误标为只读查询、安装或硬件/Gate成功。未知任务先核验，再创建新任务续传，不自动续发旧run。
- 新增协议/帧大小/偏移溢出/停止证明、真实私有文件夹接收→重复分块→暂停→高fence续传→完整校验→缓存命中、损坏前缀/未归属/symlink/过期拒绝、PG独立审批/冻结/独占/原派发消费/unknown及长连接窗口用例源码，全部未执行。只执行源码编辑、定向阅读diff、带既有overlay的gofmt和API类型生成（`/tmp/keel-artifact-distribution-api-generation.log`）；首次编辑脚本因主契约没有预期旧锚点中断，按实际根paths位置补入，无生产操作。生成成功不等于验证通过。
- 无测试/编译/静态/浏览器/镜像/服务探测，无SQL/迁移执行，无部署/push。源56/运行46，059仍未应用，3000最后已知仍 `6668a72c1adaeb2444b8c28a3f4db4f038db49dd`；本轮未重新探测。原fabric两份未跟踪草稿未修改/暂存。
- 待续：WP-1.5完整任务目录/目标→控制面文件通道及WP-1.10缓存生命周期/分发优化仍partial。当前源全文件暂存后发送缺失部分，不宣称免源读或原带宽SLO；SSH逐块调用尚无性能优化。下一块先接制品缓存的有界实际清单/新鲜度观测，复用已完成分发归属事实，不将小脚本清单冒充制品清单；最终统一完成用户安排的P1集中功能/恢复/权限/200台与介质完整性验证，硬件PoC独立记录。


## 138. 双通道制品缓存分页观察（2026-10-26，源码未验证）

接续90320ce，单Agent按WP-1.5/1.10继续。新增只读host.artifact.inventory，沿用原创建API/身份/当前权限/共享设备租约/原回执，不新建执行框架。固定目录持共享锁，检查平台归属、实际文件类型/所有者/大小与检查点；最多64项、8项每页，以公开状态摘要冻结续页，不泄露seed或command引用。明确page_content_verified:false，partial/paused与曾verified分开；历史校验时间不是本次SHA证明。API/两通道任务选择/Outputs分页和30秒历史提示齐全，实际使用仍重验完整字节；不扩展原小脚本心跳语义。

新增私有文件夹分页/冻结变化/改同大小正文仍仅元数据/实际使用拒绝损坏及真实PG默认关/冻结/当前准入源码，均未执行。仅overlay下gofmt/API生成（/tmp/keel-artifact-inventory-api-generation.log），无测试/编译/静态/浏览器/镜像/SQL/迁移/服务探测/部署/push。源56/运行46、3000旧版及原fabric草稿保留。P1范围200台不变；继续实现目标→控制面文件取回，完整缓存淘汰、身份生命周期及P1集中验收仍待。


## 139. 双通道批准文件取回及版本化执行输出（2026-10-26，源码未验证）

接续8f1cae8，/opt/keel、dev/p1-network-bom、单Agent，继续WP-1.5/1.10。新增artifact-retrievals，单目标media.fetch，复用批准身份、SSH/mTLS、一次原授权派发、exclusive设备锁/fencing与原回执恢复；只读通道入口仍不能提交制品操作。目标固定归属缓存先完整重核SHA/大小，固定device/inode/mtime/ctime，分块read核对身份并带分块SHA；历史回执不存正文，read未覆盖完整范围不能commit，源变化明确失败，cancel/到期query提供reader_stopped而非写入停止证明。

控制端私有临时文件逐块核对并累积完整SHA，再保存固定对象版本。新060不可变exec_artifact_outputs绑定execution/project/attempt/command/fence/介质身份及私有对象引用，原成功目标回执与完整存储记录同时具备才开放附件下载；失联不放行、不重派。二进制下载原结果、当前会话/成员权限前后复核、固定版本完整SHA/大小校验、审计及HTTP Range响应；存储损坏独立报错，不隔离媒体来源。资产页增加方向选择/单目标确认，Outputs原回执增加受项目约束的下载链接。没有任意路径/执行/安装或硬件/Gate通过声明。

新增真实私有文件夹完整读取/源变化/方向混用/未读完拒commit/帧大小及软件Runner+真实PG捕获/丢终态拒下载/项目隔离/关闭后历史下载/输出篡改不隔离原源等源码用例，全部未执行。仅定向读取、编辑、overlay gofmt及API产物生成（/tmp/keel-artifact-fetch-api-generation.log）。无测试/编译/静态/浏览器/镜像/SQL/迁移/服务探测/部署/push；源57（排除026草稿）/运行46，060未应用，3000最后已知仍6668a72c，fabric原草稿保留。

当前反向取回失败不保存部分临时文件，新显式任务从头取回；非任意客户路径/任务新产物采集/长期后台传输或跨窗口反向续传，不声称完整文件性能闭环。继续补“原授权已消费但目标尚未接收”的介质任务显式未启动证明，再推进P1剩余身份/缓存与集成缺口；P1尚未验收闭环，200台范围与硬件PoC边界保持。

## 140. 制品显式未启动证明（2026-10-26，源码未验证）

批准制品分发/取回的原派发已消费但目标没有回执时，可由当前Admin/DL/SRE显式请求`POST .../attempts/{attemptId}/artifact-nonstart-proof`。原命令必须已过期、attempt仍unknown且版本匹配；请求仅写五分钟人类授权和审计，不派发run、不释放资源。Worker复用既有恢复扫描，Agent broker重查原冻结命令/请求者当前权限；关闭传输开关后仍允许取得历史恢复证明。

目标settle使用同一命令的独占run.lock，仅在原期限已过且不存在已接受回执时写持久failed、nonstart_proof=true和对应writer_stopped/reader_stopped；不读写制品缓存、不推进fence、不延长授权。已有accepted/running、损坏回执或忙锁不产生未启动结论。取得原命令终态后仍由既有显式对账决定资源释放，不能把人类请求或控制面猜测当作目标停止证明。网页unknown恢复入口与事实回执已接线；API正式契约/类型同步。

新增双方向过期/关闭/持锁/已有回执/损坏状态及PG请求幂等、权限撤销、无目标证明不释放资源等用例源码，未执行。仅gofmt和API类型生成；无SQL、部署、推送或验收通过声明。

## 141. WP-1.17 DHCPv6 136适配与运维入口（2026-10-26，源码未验证）

补独立DUID/subnet6预约候选、dhcp6服务控制与写后unknown、RFC8572 136载荷和专用canary的有界relay四报文收发。CLI候选离线写新0600文件、只打印摘要；probe需私有配置和明确canary确认，不从设备SoT挑身份或自动改设备。细节及输入见ZTP-COMPLETION末节。

新增协议、Kea写后故障、保留客户配置和CLI用例源码未执行；仅gofmt，无构建、发包、数据库或部署。现有IPv4项目执行不自动变为IPv6；后续必须补项目级批准/冻结DUID与IPv6作用域/N-S3/原执行及会话接入，不能把协议库或PoC命令写成P1闭环。不提前实施WP-2.15完整IPv6规划。

## 142. 版本化只读主机Flow贯通双通道（2026-10-26，源码未验证）

通用作业目录新增keel.host/v1，发布host.identity/host.wait的真实多步DAG；单机命令不再是唯一原生入口。复用既有版本/依赖检查、目标冻结、资源/通道锁、统一执行/回执及unknown显式恢复，逐任务wait冻结由Agent broker核对。HTTP/API正式契约/生成类型和作业流模板、选通道/本页目标/显式确认接线。旧sim和业务专有流程边界保留。

新增纯解析边界和PG依赖/锁/版本/暂停/失败/断线不重放、人工恢复用例源码，未执行；仅gofmt/API类型生成。无迁移、发包、部署或推送，3000仍旧记录。当前只读host目录不是完整FR-1.3，脚本/制品仍通过专属批准入口；跨步骤有副作用任务的批准/freeze不允许用此入口绕过。详见SSH-RUNNER末节。

## 143. 主机DAG批准脚本与制品步骤（2026-10-26，源码未验证）

keel.host/v1增加media_id引用的script.run/media.stage/media.fetch；创建执行才复核独立审批和所选通道开关，冻结原版本/大小/SHA。发布source摘要与加入批准事实后的snapshot摘要分别记录，原发布版本不变。脚本/制品沿用原独占资源、一次派发、原窗口、停止证明及输出捕获，不因组合为Flow而自动重试未知副作用；fetch限制单目标。UI列出步骤/介质引用和明确影响确认，API/类型同步。

新增解析边界/PG批准前拒绝、冻结、DAG完整取回/脚本失联依赖阻断用例源码未执行，只gofmt/API生成。本批仍不证明P1通过，不做测试、DB、部署或推送。下一步优先补F-1.1明确要求的YAML错误行号（现有解析返回通用错误、网页丢弃服务端message），再收拢当前P1退出判据，不能用新功能数量报闭环。

## 144. F-1.1 YAML错误行号与编辑器定位（2026-10-26，源码未验证）

修正Flow YAML解析只返回通用错误且客户端丢失位置的问题。非法语法、未知字段/类型、重复键、禁止alias/过深结构和第二文档的可定位错误通过现有Error.details传workflow_yaml_line:<1基行号>；不回显yaml解析器文本、客户键名或值。公共错误序列化透传结构化details，客户端仅保留受限格式/范围的位置，不放开原始message。

共享错误组件显示本地化行号，Monaco标记/滚动到错误行；仅对本次提交的相同草稿启用，修改文本或重新请求即清标记。已有语义错误不伪造YAML坐标。新增源码坐标/敏感内容不回显及HTTP详情用例源码未执行，仅gofmt/API类型生成，无测试、编译、数据库或部署。

## 145. IPv6 136项目模拟协议与九态接入（2026-10-26，源码未验证）

将§141的136适配接入既有mock Kea项目staging/commit/生效查询：可选dhcp6明确网段与DUID/IPv6/hostname/switch_id绑定，逐目标核对本项目BOM；提交重查归属和批准隔离介质、全局互斥拒绝重叠IPv6引导作用域，未commit不产生offer。IPv6响应只含136及明确DUID对应的预约，IPv4选项不混入；原v4配置未添加dhcp6时保持原摘要/响应字段。

模拟会话新增136并强制offer为dhcp6、精确DUID对应同一已解析SwitchID；DHCP后继续原active hash和批准脚本检查，九态fetch证据标记mock-sztp136。原失败、重试/凭据检查及mode=sim不投影设备/NP边界保留。061仅允许136模拟会话，未开放真实136设备会话或监听路由。页面可先创建/选择136模拟目标，再填写实测DUID及明确预约参数；未选目标/批次不符不能stage，运行时使用明确DUID。

新增可变调用者输入不污染已暂存预约、stage/commit与错误DUID/网段/前面板拒绝、PG九态/凭据/作用域/无device evidence用例源码，未运行。仅gofmt和API生成，源58迁移（不含026原草稿），运行仍46；061未应用，无发包/测试/部署/推送。真实136仍需PoC及独立项目级真实批准执行接入，完整IPv6地址规划留WP-2.15。

## 146. 重装/RMA身份历史关联（2026-10-26，源码未验证）

WP-1.17/F-0.5增加项目级GET/POST ztp/identity-history，关联同项目同模式下先后完成、身份matched的两次开通。重装要求同一物理SwitchID；RMA要求不同SwitchID/SN。冻结双方会话版本、SN/UID、批次、通道和完成时间；保留原物理身份和历史，不能混合sim/device，不执行凭据转移或设备配置。

精确版本、显式确认/原因、当前fabric:plan权限和项目锁防止并发覆盖；相同请求/操作者语义重试返回原记录，不重复写事件。目标会话只允许一个不可变前驱，按时间先后拒绝反向/循环关联，目标事件与审计同事务写入。062新增不可变历史表，未应用。页面提供模拟已完成会话选择与历史列表，真实设备关联API沿用已观测会话准入，不伪造设备事实。

新增PG身份保留/重装/RMA/重复请求/不可变/跨项目/跨模式/错误顺序用例源码，未运行。仅gofmt及API生成；源59迁移（不含026草稿），运行46，无测试、构建、数据库、部署或推送。P1仍partial，集中软件验收及硬件PoC没有新通过证据。

## 147. 通用Flow熔断目录默认与冻结（2026-10-26，源码未验证）

修正新增主机DAG原100%默认值与PRD默认5%的偏差。通用sim/host准入读取exec.circuit_breaker_threshold项目覆盖/全局/未配置5%，退休墓碑或非法百分比阻断新准入；明确1～100覆盖可配置默认。快照分别保存目录默认/来源/key ID/更新时间与最终采用百分比。相同幂等请求先返回原执行，后续目录变化不重算已接受作业；主机Flow发布审计正确区分host/sim。

两种Flow启动页留空采用目录默认，正式契约/类型同步。目录作用域/退休/非法值、主机冻结/显式覆盖/目录改变后的幂等用例源码已补。Agent集中脚本/CI纳入HostFlow/HostMedia；Agent脚本纳入制品/YAML协议和HTTP错误用例，lifecycle批次纳入ZTP/136/目录策略。没有执行这些入口，仅gofmt/API生成；无新迁移、部署或验证。继续收口F-1.8的无介质KV路径。

## 148. 无介质项目KV Flow及原子恢复（2026-10-26，源码未验证）

接续F-1.8明确要求的无介质KV路径：keel.control/v1只允许公开项目元数据kv.get/kv.put、原Flow版本/校验/DAG、独立control准入和project类型目标，不需要编造介质或设备连接。使用原Execution/outbox/项目资源/fencing/五Tab；put为显式版本CAS，0只创建，冲突不覆盖，128键/有界文本/固定时限/无重试。模板、确认页面、API及生成类型接通，元数据不是设备配置、秘密或门禁证据。

控制面事务同时写值/版本、不可变原命令回执、执行结果及审计。过期/旧fence不写，当前发起人/项目权限与Kill在事务前重查。崩溃未提交的原事务在执行锁下由回收器生成未提交证明，仍unknown隔离，原Reconcile显式解封；不把DB错误/租约过期当作安全重放授权。新增源码用例覆盖纯KV/错误目录、DAG/重复命令/CAS/跨项目读隔离、Pause/Kill/撤权/过期未提交对账；Agent集中批次/CI已纳入，未运行。

迁移063未应用，源60迁移（排除026原草稿）、运行46；只gofmt/API类型生成，无编译、测试、静态/数据库/浏览器/镜像验证、部署或推送。P1仍partial，不能从源码或执行包编写宣布验收通过。下一步按F-0.5/F-1/F-2与P1实际WP集中收口；真实PoC由用户提供，P2～P4前置不强拉。

## 149. FR-0.3.7批准换件与绑定退役（2026-10-26，源码未验证）

修正§146仅有关联历史、尚不足以覆盖RMA批准流程的缺口。增加项目换件申请/列表/独立Admin审批及拒绝：原managed/matched会话、独立新BOM物理UID、不同SN/管理MAC、明确且相同dc/rack/idx，冻结身份/位置/原会话版本及摘要。申请者当前计划权限、审批者独立账号/当前权限、精确版本及当前BOM事实均复核；待审批新身份阻断引导，BOM变化拒绝旧快照，拒绝申请不变更绑定且可重新申请。

审批同事务将旧项目绑定retired/新UID绑定active，保留旧物理SN、MAC、BOM修订和原会话。mode=sim只写模拟绑定，不修改真实退役、地址分配或设备；device审批退役旧的当前地址投影，原冻结规划仍保留。身份反查拒绝退役或活动审批事实已变化的设备，真实最小管理规划排除已退役目标，Kea新准入拒绝包含旧绑定的规划。旧DHCP配置不被静默重写，新设备仍需正常批准规划/开通；凭据不自动转移。

RMA历史现在要求replacement_id，审批必须先于新会话开始、覆盖精确新旧UID/SN/模式；旧无审批历史保留但不授予换件或退役授权。页面接项目BOM选择、申请/审查/独立确认/拒绝及历史审批选择。064新增审批与模式隔离绑定，未应用。修正历史用例原fixture缺少bom_items父实体，补BOM/MAC/位置关系及申请重试/拒绝重申/自批拒绝/变更快照拒绝/模拟不退役真实绑定/退役不可反向恢复用例源码，未执行。

仅gofmt/API类型生成，源61迁移（排除026原草稿）、运行46；无构建、测试、数据库或浏览器验证、部署、推送。P1签署仍须集中软件验收与对应材料，不把该实现记为真机换件通过。

## 150. ZTP 200逻辑批次与H-15/H-16执行材料（2026-10-26，源码未验证）

WP-1.9/1.15/1.17：增加TestZTPP1LogicalBatch200Postgres源码，同一批准隔离介质/明确DUID预约，200个独立BOM UID和会话逐个走真实持久化九态及反查，首遍managed≥99%、身份100%、协议标签异常0、device observations=0。记录每目标会话/UID/结果、实际分母/分子/耗时和staged hash；parallelism=1，明确是顺序软件开通，不冒充200并发DHCP或硬件传输。原200逻辑执行器扇出是另一验收项，不能由此替代。lifecycle脚本提取批次报告，只有命名用例实际PASS/整批退出0且计数达标才记相应逻辑批次verified；窄跑或skip不记通过。

新增poc/bom-h15环境/人工清单/空证据模板/离线摘要校验器，要求真实来源、别名映射/目录与宽表、三级问题回传、幂等稳定UID、型号与项目拒绝材料。修正H-16材料校验漏掉60/43：五类案例、精确厂商profile/option60/43载荷摘要；≥2实物SN，可同一台覆盖多通道，不错误提高到三台。共同文件SHA校验改流式读取。H-15/H-16新增合成完整性/损坏/缺案例用例源码并接make check，不把离线READY_FOR_REVIEW当真实PoC PASS。

未运行任何新增用例、校验器或执行材料；只有Go格式化，无编译、静态/数据库/浏览器/镜像验证、部署或推送。迁移及运行状态仍§149。P1退出还必须集中软件验证/契约与页面证据；按真实证据签署，当前仍partial。

## 151. 项目通知投递可见性（2026-10-26，源码未验证）

WP-1.6：复用052持久出站投递，新增项目Admin/平台Admin分页只读API及项目审计页投递面板，展示公开事件/原执行、状态、次数、通用错误及时间；接收URL、载荷、签名密钥不出站。每请求同一repeatable-read事务复核会话安全版本和当前项目Admin权限，其他项目404/成员非Admin403/撤销会话401；owner CLI仍独立要求数据库owner，不改为角色绕过。UI缓存按用户/项目隔离、切换项目重置页码，明确空记录不代表配置或健康，不提供对unknown/dead的重放入口。

扩展原Webhook集成用例源码：当前授权/跨项目/权限变更撤销旧会话/重新登录后的非Admin拒绝/分页/敏感元数据不泄露；未执行。仅gofmt和OpenAPI类型生成，无编译、测试、数据库/浏览器/镜像验证或部署推送。源61迁移/运行46不变。邮件/IM专用适配及后续阶段事件仍未交付，不能把现有执行Webhook子集标成FR-10.4全完成。

本轮核对PRD附录A.7：线缆两端必须同包解析，现有引用约束正确；不依据历史“external UID校验待办”扩展到跨包线缆。P1仍partial，继续按实际软件缺口和集中验收材料收口，不强拉P2～P4。

## 152. P1邮件与IM通知适配（2026-10-26，源码未验证）

WP-1.6/FR-10.4：复用原052持久投递、公共CloudEvent、项目/时间下界与不可变sink策略，增加Resend邮件及Slack Incoming Webhook。发送前只接受严格公共事件/当前sink项目及允许类型，固定文本不含命令输出/原始metadata；公开页面只显示通道和投递事实。065新增不可变策略的通道列与event_expired原因，未应用；旧默认CloudEvents摘要保持兼容。

独立受保护邮件token、明确sender/最多16收件人、Slack secret URL均由运维配置，默认关闭；供应商host/path与精确origin/DNS固定/metadata排除/无代理重定向复用。收件人/通道/凭据指纹进入策略摘要，原ID不能静默转投。HTTP2xx还检查Resend UUID或Slack200/ok，实际wire保留HMAC但不声称供应商验证Keel签名。邮件固定幂等键，事件23小时后停止，避免越过供应商24h幂等保留后重复发送；接受不等于邮箱送达/用户已读。Slack未知结果仍至少一次，可能重复。

新增协议/策略/凭据清除及PG outbox-适配器用例源码，并接原TestWebhook集中入口；没有执行。仅gofmt/OpenAPI类型生成，无编译、静态/测试/数据库/浏览器/镜像验证、部署/推送或外部消息。源62迁移（排除026原草稿）/运行46，3000最后已知版本不变。配置编辑UI、更多厂商及后续阶段事件源不冒充交付；P1仍需集中软件验收。

下一实际软件缺口：原生主机DAG已支持SSH/Agent批准脚本和文件步骤，但现有失败/单目标重跑仍只接受mode=sim；需接回同一准入事务并复核当前授权/原始已发布版本和批准制品，保留unknown禁止重跑，不能只放开mode判断。

## 153. 原生主机DAG单目标/失败集重跑（2026-10-26，源码未验证）

WP-1.4/1.5/1.9：原重跑只支持sim，现接发布过的keel.host/v1 SSH/Agent DAG。复用同一授权/幂等事务和原HostFlow准入内核，二次解析独立批准脚本/制品及当前通道/资产绑定，与原frozen flow/目标完整比较，变更拒绝；不是只放开mode判断。新执行保留原版本、来源/选择摘要、确认和独立command，原快照/回执不变；重试先复核当前权限，再返回原child。

预览冻结execution target ID、machine UID和SN、来源版本/摘要与排除项；前端按rerun_supported呈现，原生必须勾选重复整个DAG并可能重复已成功步骤副作用，API强制confirm_full_workflow=true。缺回执或已领取却无停止证明的cancelled/skipped按unknown排除；未结束源仍不能重跑。装机/互信修复/Kea/项目KV保留原业务入口。

新增TestHostFlowRerun*源码（原Agent集中入口已覆盖TestHostFlow）：独立新DAG/新命令/幂等/单目标/未知阻断/退休制品/资产版本变更/撤销会话/源历史不变，源码未执行。必要gofmt及API类型生成完成，无编译、静态/测试/数据库/浏览器/镜像验证、部署或推送。补齐上批生成类型中的event_expired枚举；源62迁移/运行46不变，065未应用。P1仍需集中验收，不能宣称闭环通过。

## 154. 集中验收证据去假通过（2026-10-26，源码未执行）

修正check-agent-integration.py无条件将verified_loopback_tls_grpc写true：现在必须指定身份/任务协议用例实际PASS且整批退出0，skip/窄跑/失败均不记通过。分别记录实际PASS/SKIP/FAIL、Agent/worker/浏览器、原生DAG/重跑和KV组；browser_requested不代替browser_verified。Agent测试服务先复核disposable标签及loopback15433端口。

新增仅元数据的p1_evidence.py与未执行单元用例源码，报告前后Git提交、跟踪修改/额外未跟踪项；只有两份原草稿以精确排除overlay保护时才可标unchanged_clean_snapshot。不读取或执行原草稿。该字段只描述报告脚本看到的源码，不证明运行二进制与提交一致，完整验收仍需父级构建记录。Agent/lifecycle组件结果始终full_p1_acceptance=false。

200逻辑执行容量报告先写suite_completed=false，后续恢复/权限等整批完成才改true，避免容量子项已通过却被当成整批通过。所有脚本/新单元源码均未运行，没有编译、测试、数据库/浏览器/镜像操作、部署或推送；源62迁移/运行46不变。

本轮定向读源码发现两个真实后续项：Control KV已接RunClaim，但公开worker Claim的mode查询遗漏control，需修复并从公开领取入口覆盖；WP-1.3/ADR-01的Redis Streams可重建队列仍未实现，目前PumpOutbox为PostgreSQL消费/去重/调度，Redis仅部署，不可写成Redis事件总线已交付。不要再把已存在的Machine/Component/PhysicalPort/项目资产/稳定BOM/outbox写成planned。

## 155. KV公开worker领取与控制权限修正（2026-10-26，源码未验证）

修复§154发现的control模式断链：公开Service.Claim现在将control与sim作为不依赖设备Runner的候选，原项目资源/租约/fence/当前权限和原子KV回执不变。TestControlKVAtomicReceiptCASAndProjectIsolationPostgres的第一次put改走PumpOutbox+公开Claim，双通道Runner均nil，防止只调用claimExecution私有fixture漏掉正式worker筛选。执行工作台把control归入Admin/DE/DL/SRE控制权限，修正先前错误落入网络NDE分支；后端原来已按该权限校验。

同时补ExecutionSummary列表的rerun_supported只读能力投影，原生DAG与sim列表不再因字段零值被报不支持；实际预览/提交仍执行完整验证。仅gofmt，新增/修改用例未运行，无编译、静态/数据库/浏览器/镜像验证、部署或推送。源62迁移/运行46不变。

下一完整主线是WP-1.3/ADR-01的可重建Redis Streams队列。现有PostgreSQL权威状态、outbox去重、资源锁及unknown恢复必须保留，Redis丢失/重启不得重放副作用或阻断已有DB对账；不是把内存channel或Redis服务存活当成队列交付。

## 156. Redis Streams可重建队列接入（2026-10-26，源码未验证）

WP-1.3/ADR-01新增executionqueue Transport/go-redis v9.23.0及worker的PumpQueue。066保存可重建发布提示与无载荷拒绝记录；源63迁移/运行46，059～066未应用。正式模式默认仍PostgreSQL-only；显式0600队列配置启用Redis Streams，生产TLS+独立口令，Compose提供禁用目录和只读挂载，不修改.env或现有服务。启动日志明确queue_mode，未启用不能算Redis完成。

outbox已提交事件投影为版本化CloudEvent引用，无原payload/脚本/输出/秘密。XADD使用Redis自动ID，避免事务序列分配与提交次序不同导致迟到事件被拒；发布提示落库前崩溃允许重复。消费重新核对DB事件/项目/序列/版本/时间，固定逻辑consumer去重，同事务锁定当前aggregate并CAS created→scheduled，提交后XACK；不创建命令/尝试或触碰unknown任务。非法/未知/投影不符先持久化不带载荷的拒绝原因才ACK。

XAUTOCLAIM恢复30秒PEL，扫描游标持续推进。stream/group重建轮换epoch，发布/ACK带epoch原子检查；未消费引用即使同代次也在2分钟后从outbox重发，覆盖局部Redis丢失/20,000近似裁剪。无数据库锁期间的Redis网络调用。配置队列故障不静默降级冒充成功，created等待恢复，原scheduled领取及DB租约回收继续，失败周期不刷新健康心跳。

新增未执行源码：PG提交后ACK/ACK丢失/重复/忙聚合/非法投影/代次与局部丢失/队列故障隔离，独立真实Redis协议PEL和代次恢复，以及Redis+PG串联调度/缓存丢失不重建TaskAttempt。Agent集中入口增加TestExecutionStream/TestExecutionQueue和独立--redis-address选项，报告三个组分别PASS/SKIP，未提供loopback16379 fixture不能记Redis通过。fixture不启动服务或flush数据库，只清理随机自有命名空间。

仅获取锁定依赖源码/校验和并gofmt；没有编译、静态检查、测试、SQL/迁移/服务探测、浏览器或镜像验证、部署、推送。两份原fabric_precheck/026草稿不变。P1仍partial，实际退出判据及集中验收未闭环；下一步继续按设计检查软件缺口和验收材料，不反复执行验证或强拉后续P2～P4。

## 157. P1集中入口补齐PXE软件链与证据分母（2026-10-26，源码未运行）

修正Agent集中入口默认过滤器漏TestProvision的问题：同一隔离库/既有一次二进制构建批次现在包含完整准备/安装准入、boot/file/write/first-boot、首次SSH/基线/配置、未知写前和注册回执恢复、修复/滚动放行五个独立证据组。这里的对象与目标机证明是fixture，报告明确不是实际PXE或物理装机。没有启动测试、安装服务或更改3000。

修正Go顶层测试PASS但子测试SKIP仍被算作通过的漏洞：保留skipped_subtests，所需根用例只要存在跳过子项，相应组就不记verified。ZTP200批次增加逐设备记录复核，要求200条实际且唯一switch/session/SN、匹配标志/成功数与汇总一致、真实整数计数、至少198首遍成功、身份200/200、零设备观测、sim标签；缺cases、重复设备、布尔冒充计数或错误物理标签均拒绝。新增仅源码的正常/缺失/重复/阈值/skip边界单元用例，未运行。

P1当前按IMPLEMENTATION-PLAN§2退出判据继续收口：认证与项目隔离/执行恢复及锁/共享Shell/200逻辑执行成功率≥99.5%与介质校验0/型号和bootstrap-repair/BOM稳定身份与三级问题/ZTP≥99%和身份100%/P1与H-15/H-16材料分别留证。新增队列、原生DAG、KV、RMA、邮件IM和PXE软件链仍待集中验收，不把一个组件组、旧2000台报告、编译或配置存在当P1签署。F-1.13中G5及工单的实际消费链按后续P2/P3依赖推进，不强写缺少前提的业务服务。源63迁移/运行46，原草稿不变；无编译/静态/测试/数据库/浏览器/镜像验证、部署或推送。

## 158. worker健康绑定队列模式（2026-10-26，源码未验证）

新增067而不改已提交066：exec_worker_heartbeats保存queue_mode，WorkerHealthy同时核对worker身份/执行模式/队列模式/10秒时效，DB-only心跳不能证明Redis模式健康，切换队列配置也不能沿用旧模式健康。实际Redis失败周期仍不更新心跳，原DB对账不中断。新增模式切换/过期心跳PG用例源码并纳入streams_postgresql组，未运行。源64迁移/运行46，059～067未应用；仅gofmt，无测试/编译/静态/SQL/服务/浏览器/镜像验证、部署或推送。

## 159. P1退出判据与集中入口对应（2026-10-26，仅准备，未运行）

集中验证授权仍待用户明确：已通过当前会话的异步问题请求是否开启P1验证，尚无答复时继续遵守不测试/不编译/不部署约束。不能把提问本身或等待时间当授权。新增BOM/型号默认过滤与独立组、首次SSH/密钥修复实际loopback传输组，复用原批次和编译入口，不启动新测试批次。

| IMPLEMENTATION-PLAN§2的P1判据 | 已有集中入口/证据边界 | 本轮状态 |
|---|---|---|
| F-10.5/10.6登录会话 | check-auth-integration.py、check-oidc-integration.py、check-ldap-integration.py，各自隔离fixture | 未运行；本地成功不替代OIDC/LDAP |
| F-17.2/17.5项目边界 | check-auth-integration.py、check-control-integration.py及各领域项目越界用例 | 未运行；需HTTP/UI与缓存/SSE证据 |
| F-1.11/12可靠执行/锁 | check-agent-integration.py的Agent/原生DAG/队列/KV/PXE五组及check-ssh-integration.py | 未运行；Redis组需独立loopback16379，否则SKIP |
| F-1.13受限正向路径 | 同一Agent/SSH/native/KV/PXE准入及unknown用例；后续G5/工单消费者按P2/P3依赖 | 未运行；不伪造后置服务 |
| U-3/12/15共享Shell部分 | check-execution-workspace-integration.py、已有Shell/项目切换/登录浏览器入口 | 未运行；不以组件存在替代交互证据 |
| 200逻辑执行≥99.5% | check-execution-workspace-integration.py默认200及simulation-report/resource-report | 未运行；历史2000结果不代替新源码 |
| 介质校验失败0 | check-media-artifacts-integration.py及Agent/SSH制品/缓存/摘要隔离用例 | 未运行；无介质的sim容量结果不能证明完整性 |
| 型号约束及BOM稳定身份/三级问题 | lifecycle新增bom_identity/model_constraints组、已有model-workspace/BOM浏览器 | 未运行；source组不替代CSV/UI定位 |
| bootstrap/repair | check-ssh-integration.py及Agent入口的provision_loopback_ssh/provision_repair_rollout | 未运行；软件握手与真实PXE分列 |
| ZTP≥99%/身份100% | lifecycle的ztp_200及逐设备分母复核、239/143/136/60-43协议用例 | 未运行；136项目sim非真实设备136准入 |
| #P1、H-15/H-16执行材料 | docs/poc下既有执行包及材料校验入口 | 材料源码已备；真实H项NOT_RUN待用户PoC |

这张表是验收路由，不是PASS报告。各组件报告仍full_p1_acceptance=false，只有按上述退出判据实际集中验证、处理缺口并留证后才能签署软件闭环。源码检查点6028ad2已含队列模式健康绑定，源64迁移/运行46；没有执行测试/构建/静态/SQL/服务探测/浏览器/镜像验证、迁移、部署或推送。

## 160. P1集中验证启动与首轮修复（2026-10-27，进行中）

用户明确授权“启动集中测试，高效完成，测试通过后更新”；继续本地单Agent，不推送，不执行原fabric_precheck/026草稿。私有集中日志目录/home/ubuntu/.local/state/keel/evidence/p1-20261027；独立Redis fixture keel-p1-test-redis-20261027只绑定loopback16379，原业务Redis/数据库/3000未变。

首轮设计与PoC材料单元通过，前端类型/构建通过（契约同步后仍将纳入最终候选构建）。修复证据单元的干净新提交断言；MediaDependencies同步script/os_boot及引用schema，消除平台/执行契约漂移；rerun显式提交boolean确认字段；公钥修复工作数据转换为事件map；缓存测试改用现有外部测试包；TFTP每块/重试重验授权；测试密钥/离线产物目录使用明确私有目录，超长SSH请求断言跟随已支持脚本的16KiB边界。Docker上下文显式排除两份原草稿，以便安全构建同一候选。

尚未宣布全量Go/PG/浏览器/镜像通过；失败先定位后定向复测，集中证据通过后才备份/迁移/部署3000。源64迁移，运行上次记录46，最终部署状态后续回录。本节是启动检查点，后续结果覆盖待验证状态。

## 161. 集中数据库批次定位与性能issue（2026-10-27，继续验证）

用户要求宿主性能相关超时先登记、不反复纠缠。已记录docs/issues/P1-001-test-host-resource-timeouts.md：MemAvailable一度376MiB、无swap、shared约10GiB；worker/CLI期限及未及时取得回执待资源充足时隔离复验，不提高超时或标PASS。gh CLI本机不可用，仅建立仓库内issue记录，未声称已发布GitHub issue。其他项目与业务服务不动，仅清理本轮已废弃的明确自有Go临时构建目录。

基础全仓go test -race通过；前端build/typecheck与设计/PoC材料单元已通过。实际PG首批暴露确定性错误：缓存/网口只读事务错误调用FOR SHARE身份helper，已共享有锁准入/无锁授权快照两条查询；旧JSON BOM提交漏保存model_version_id，现与model_id同事务锁定并冻结最新已发布项目型号，保留原作用域；Webhook投递状态复用参数推断varchar/text冲突，现显式类型。修正测试夹具的多次首管理员初始化、umask下0644负例、审计DBA篡改需显式禁用并恢复触发器、ZTP历史缺映射版本引用。没有放松真实审批、审计追加、身份绑定或unknown约束。

Agent/Redis/PXE初批与lifecycle首批失败不记通过，保留原日志；之后仅对实际修复和资源明确的失败项定向复测，尚未迁移或更新3000。

首轮定向复测（589702b）已通过OS引导组成、PXE预览/请求生命周期/重装序列号/验证期间失效、ZTP身份历史及受保护webhook配置。审计与webhook实际CLI主要断言通过后，受限角色连接夹具仍失败：PG报告`unrecognized configuration parameter "+role"`，将启动参数改为无空格的`-crole=...`，保留受限角色负例。P1-001补录整包预算耗尽及浏览器期限终止，未把局部成功当整项通过。

112361a下owner CLI批次退出0，审计DBA篡改检测/离线CLI、主密钥轮换/恢复CLI、webhook投递与权限/适配器CLI通过。Agent定向批次的缓存/网口读接口、全部6个ExecutionStream用例通过（含真实Redis+PG调度、提交后ACK/重复消费、重建/失败隔离及队列健康模式）；该批整体因PXE后续确定性失败退出1，不能标整批PASS。PXE新增修复：DHCP观测与文件发送INSERT SELECT显式指定varchar参数，避免text/varchar推断冲突；BOM更新后fixture通过真实撤销/重新注册取得当前资产版本身份；回收fixture只断言本次目标attempt进入unknown，不依赖共享隔离DB中全局回收总数恰好1。以上PXE修复待定向复测。

d5f8618定向PXE批次中，首次启动/SSH、孤立派发、密钥修复、注册结果恢复、灰度、SSH批次/已知失败、写许可及准备资源互斥均已PASS；仅写盘前恢复和修复列表失败，未记整批PASS。定位修复：前者错误使用不包含state_version的轻量绑定视图，改从已锁定行读取精确版本；后者平台管理员读取不存在项目缺404，补存在性校验。BOM旧兼容入口维持未发布型号可建planned资产，已发布则冻结最新版本；不把兼容导入当统一网络BOM准备或放宽PXE已发布版本准入。HTTP夹具补未发布→发布后重导冻结的断言；认证测试允许严格12位hex后缀的独占keel_test_http数据库，保留15433回环及隔离API限制，不重置原有测试数据库。

bf265ec的PXE定向收尾3项PASS（baseline、写盘前恢复含并发幂等及全部负例、修复列表），该组退出0。独占keel_test_http随机库HTTP批次的本地登录/项目六角色、账户/成员/审计、型号发布/导入、BOM导入与发布版本冻结均通过；登录/管理/型号导入的Chromium也通过。OIDC与LDAP各自完整真实PG+回环协议+Chromium批次退出0。旧check-bom-browser仍找已移除的JSON文本输入入口，未通过；现按当前标准目录包与CSV文件入口重写，覆盖三级问题定位/导出、统一包提交、重复幂等/异摘要拒绝、型号版本及CSV折叠，未恢复旧指纹直接建立互信断言，真实SSH验收由独立现有批次承担。BOM新脚本待定向复验，SSH批次进行中；3000/业务库尚未更新。

## 162. 集中软件验证收尾与性能项保留（2026-10-27）

完整短报告见[P1验证记录](P1-VALIDATION-2026-10-27.md)。BOM当前目录包/CSV浏览器6组及登录异常/布局检查通过；型号历史/引用/资产绑定/compute-tray/全局页面批次通过；真实MinIO介质/参数/依赖/签名/离线包/损坏隔离/来源/兼容矩阵/筛选的完整HTTP/浏览器批次通过。200逻辑执行容量200/200，8.235秒，12组控制/恢复/权限检查全部通过。最终受影响platform/execution/taskrunner的vet/race与前端类型/构建均返回0；无DB环境的基础race不替代集成结果。

用户再次要求对可能受宿主性能影响的步骤沿用issue，不反复追跑；P1-001保持OPEN。SSH首批公钥安装浏览器等待终态超时，独占库定向8.70秒通过且后续真实SSH/HTTP/CLI通过，但首批仍不改PASS；Agent CLI/脚本/制品/浏览器、200逻辑ZTP整包预算耗尽等保留原始失败及未确认原因。所有确定性修复后的目标用例通过，与尚未完成的时限相关验收分列；P1不签署，不把执行200/200替代ZTP200证据。

原始日志/结果/截图按白名单复制到持久私有evidence目录的runs下（16个隔离环境、128个文件，约8.24MiB），有SHA256索引，排除凭据/私钥文件。测试PG保留；只清理本轮自有数据库/MinIO/Redis fixture，不动其他项目服务、业务数据卷、.env或两份原草稿。3000 /healthz仍6668a72c；只读核实业务库46条、最后049，源码64条，050～067未应用。没有候选部署镜像构建、业务迁移/部署或GitHub推送。后续先按issue补齐签署缺口，再执行已有备份/迁移/授权/发布流程。


## §163 2026-10-27 用户明确要求先发布到3000

- 本节替代前文“3000未更新/运行46迁移”的历史状态。用户在了解剩余验收缺口后要求先发布；不把部署当作P1签署，P1-001保持OPEN，没有补跑集中测试。
- 运行镜像`keel:7afda320ecebbf9670c61103239e5f9872b2ed49`；一次`docker build`完成前端类型/构建及API/admin/worker/sim/SSH runner/Agent二进制。两份原草稿由既有`.dockerignore`排除，未触碰。因原草稿使工作区非净，未使用要求全净的deploy.sh，而在原工作区按同一构建/Compose健康等待/600权限更新镜像引用流程执行，没有stash或新增工作树。
- 首次备份后在自有独立库恢复，再用候选镜像`keel-admin migrate`演练050～067；原有业务表行数保持。切换前停止旧API、再次备份并检查目录，业务迁移在既有事务/checksum机制下成功；`configure-development.py --grant`授权后，核对审计UPDATE/DELETE、迁移历史INSERT及域密钥INSERT对应用角色均为false。运行库64条迁移、最后`migrations/067_worker_queue_health.sql`。
- `docker compose up -d --no-build --wait --wait-timeout 90 api`通过；healthz版本与镜像相符、providers可用、匿名项目请求401。真实浏览器登录，已认证项目/型号API及页面可用，退出后撤权401，无pageerror。没有启动worker、开启真实执行通道或Redis队列。
- 私有证据目录`/home/ubuntu/.local/state/keel/evidence/deploy-20261027/`（700）保留构建/迁移日志、恢复前后行数、两份数据库备份、旧.env副本（600）、浏览器截图和`smoke-result.json`/摘要索引。自有恢复数据库与临时owner凭据文件已清理；旧镜像、运行数据卷和其他服务保留。
- 旧镜像为`6668a72c1adaeb2444b8c28a3f4db4f038db49dd`，不能直接连接当前64迁移schema；如需退回须单独评估停止写入后的备份恢复及后续数据保全，不直接切镜像或删除卷。本次未推送GitHub，P1未签署，真实PoC未执行。


## §164 2026-10-27 P1带遗留项收口，允许进入P2

用户明确将200逻辑ZTP整批验证留待后续继续测；Agent/SSH超时按性能待办记录，待用户再次要求解决。P1因此按带遗留项收口，P2启动不再等待这两组复验。P1-001保持OPEN / DEFERRED，原始结果、关闭判据和真实PoC边界保留；阶段准入不是全部软件验收PASS，也不是已定位所有超时根因。3000保持§163部署版本，无新增测试、实现、迁移或重建。

P2下一入口按IMPLEMENTATION-PLAN步骤D先交付WP-2.7工单基础，再建设WP-2.14采集/预校验；先完成开单、分派、权限、版本及证据引用，待领域采集能力就绪后接定向重验和证据关闭，不倒置依赖G3/portmon。后续WP-2.15模板/Golden，再与服务器采集、G1/拓扑汇合进入G2/G3；不提前拉入P3/P4。原两份fabric草稿保持隔离，不能将其存在视为P2前置已完成。


## §165 2026-10-27 main合并与P2首个实施块

用户要求“合并项目至main，然后开始p2”。先用既有仓库身份ls-remote/fetch确认GitHub可达，origin/main是当前成果祖先；本地main从e7d5f1a快进至1a96897并正常push origin main成功，随后在原工作区创建dev/p2-validation。原两份未跟踪fabric草稿保持原样，不stash、不合入。3000未随分支切换更新。

按实施计划步骤D开始WP-2.7工单基础。新增068迁移：issues、issue_events、身份/首次source/phase/初始证据不可变保护、版本递增及永久时间线。基于现有项目/会话/权限与审计实现跨获授权项目工单池、筛选/分页/详情、项目稳定对象搜索、幂等人工开单、当前项目NE候选、分派、开始处置和现场处置登记。允许机器/部件/未映射物理端口/交换机/线缆关联，至少一个同项目授权对象；责任域默认unknown，不能因定位改变首次source。动作在事务内复核当前权限与版本；换件要求不同的新旧实物SN/标签，BIOS动作限T7；登记不会派发设备命令。

统一`/issues`页面接共享shadcn控件，列表/看板、来源/阶段/类别/责任域/状态/项目筛选、详情时间线与人工开单/分派/处置表单；支持按后端allowed_transitions拖拽打开确认表单和等价键盘按钮，服务端确认后再刷新，409可获取最新记录。全局工单池只聚合授权项目，项目切换保留运维页面并改变筛选，已打开表单冻结原项目与版本。API契约及生成类型、路由同步。

边界：WP-2.7仍partial。自动发现到开单、领域定向重验Execution、有效PASS关闭/重开、失败两次升级与通知须随WP-2.14/服务器领域能力接通；verify/close当前明确拒绝，不接受客户端提供PASS或直接改变状态，不依赖尚未交付的G3/portmon。随后先接既有网络采集证据到工单基础，补WP-2.14真实软件消费者，不重复造队列/工单池或提前开发P3/P4。

用户在本轮再次要求重大里程碑再集中测试。因此仅执行gofmt、`npm run api:generate --workspace web`及`npm run routes --workspace web`生成产物；生成退出0，路由生成有既有工具循环依赖警告。新增两组PostgreSQL集成用例源码，覆盖幂等/跨项目拒绝/未映射端口/版本竞争/换件记录/不可伪造关闭/历史不可变/撤权和网络独立对象聚合，尚未执行。没有Go编译、静态检查、功能/数据库/浏览器/镜像测试，未应用068、未更新3000。受限角色授权脚本新增issue_events只追加及issues不可删除的授权边界，留待后续迁移发布时执行。P1-001继续延期，200规模与单Agent约束保留。


## §166 2026-10-27 WP-2.14网络采集异常接入工单

从11d0f7a继续，单Agent在原工作区实现网络侧明确线缆异常→统一工单→不可变观察历史。复用WP-2.7插入/审计逻辑，但自动发现只由可信采集生产者在现有事务内调用；不开放浏览器写入判定/PASS。采集前后项目/权限/BOM检查沿用原链，报告/样本/工单/观察同事务提交或回滚。

069新增网络发现绑定与观察表，绑定项目、稳定线缆与端口、类别和device/protocol_fixture来源；同类同对象同来源复用工单，原始source/phase/证据不变。每个run保留观察，重复快照不推进工单版本，变化后追加版本与事件；过期或pending/仅网络预检通过不创建新异常或关闭旧工单。复发重开预留在实际closed状态后的新异常，当前仍没有可用的证据关闭路径，不宣称整条生命周期已完成。

网络预校验返回issue_links，现有未关闭工单在pending/预检通过视图中仍保留入口。统一工单详情支持URL直达和分页网络观察历史，展示协议仿真/设备来源、规则、原run/包、时间及stale；读取权限在当前项目内重验。保留原始采集快照，不将历史引用替换成最新样本。受限角色授权脚本新增两表仅追加授权边界，尚未在运行库执行。

边界：现有collector GET读取的是运维端现成快照，不能冒称设备侧定向重新采集。端口DDM/T4/T6等诊断持久化、带范围/版本/资源约束的重验Execution，以及有效关闭/升级/通知继续按P2依赖推进。N-S5仍不确认服务器侧、NP-3固定BLOCKED，真实PoC与协议fixture分开。当前下一完整功能块为端口诊断持久证据接入，再接受限复验，不提前拉P3/P4。

遵循用户在重大里程碑集中测试的要求：本轮只做源码审查、gofmt、API类型及路由生成（退出0；路由工具仍有既有循环依赖警告）。新增PostgreSQL集成用例源码，覆盖自动开单/重复快照/模式隔离/观察不可变/未知不关闭/回滚/网络BOM时效/越权，尚未执行。没有Go编译、静态/功能/数据库/浏览器/镜像测试；未应用068/069、未更新3000、未推送P2分支。两份原草稿保留，P1遗留项不追跑。


## §167 2026-10-27 WP-2.14端口诊断证据与统一工单联动

从8bcbf61在原工作区/分支继续，保持单Agent。复用既有诊断计算与工单基础，新增显式POST记录当前缓存诊断，普通GET不创建证据或工单。记录冻结BOM/稳定设备/可选端口范围、原采集run/样本引用、计数器窗口和阈值、配置意图哈希及接口意图、DDM阈值与模块读值。T2-rail/T4/T6由既有诊断器判定，T3接DDM明确warning/critical；只有在用端口明确异常进入同一工单池，unknown/not_applicable不冒充故障或通过。

070增加三张不可变记录/绑定/观察表和作用域触发器；绑定项目、稳定端口、类别/子类/规则ID和证据来源，device与protocol_fixture隔离。重复相同范围/版本/样本返回原记录，保留原记录时间及过期时间，不靠页面读取或重复run续期。记录/异常工单/观察/审计同事务提交；来源/权限仍按当前项目复核，物理端口解析复用建模端口命名规则，不猜测别名。旧证据不覆盖，也不据正常缓存自动关单。

API及生成类型、共享UI入口与工单内分页诊断历史已接入，页面切换过滤原设备/项目的异步结果。历史stale目前按TTL与当前网络BOM判断，不作为全部配置版本仍有效的准入证明。记录行为不触发设备重新采集或Execution，N-S5/NP-3边界不变。下一完整功能块是限定范围/版本/资源的领域重验Execution，随后接有效PASS确认关闭与连续失败升级；重验若需要尚未具备的连通性或Golden证据继续BLOCKED，不补假PASS。

本批仅源码审查、gofmt及`npm run api:generate --workspace web`（退出0）。新增证据指纹跨项目/设备/BOM/来源/阈值/规则/观察/范围隔离单元用例，扩展PostgreSQL用例覆盖GET不产证据、缓存记录幂等不续期、记录不可变和未知端口拒绝；均未执行。没有Go编译、静态/功能/数据库/浏览器/镜像测试。源67迁移，运行仍64/最后067；068～070待集中迁移验证，无3000部署或P2推送。P1-001与200逻辑ZTP仍按用户延期；两份原草稿未读取/编译/提交/迁移。


## §168 2026-10-27 重验作用域预览与定向复采客户端

接续bcded56，单Agent在/opt/keel、dev/p2-validation开发。先补齐共享执行器所需的领域输入边界：新增verification-scope只读API及工单详情按需预览，返回工单/处置版本、可信发现与规则、当前网络BOM、双端稳定设备/端口、来源、参数/意图摘要、分类检查及明确缺失条件。重新分派或复发后旧处置不能复用，BOM无法解析或多链路范围保持阻断；摘要不因阅读时间改变。此预览未持久冻结请求，未来执行提交必须重算范围并事务鉴权，不能直接相信浏览器摘要。

新增fabric-recollect/v1 mTLS客户端，使用固定origin POST/recollections，绑定命令/执行/围栏/范围/来源，最多双设备双端口、1MiB/30秒、无重定向和自动重试；逐设备/端口核对，拒绝旧于not_before、未来、重复/缺失/额外目标、串模式及范围外配置/遥测。现有GET缓存链保持原义。外部采集器/NOS重采实现、共享Execution提交/双端资源锁/结果消费尚未接通，execution_available明确false；没有派发或假造执行，不宣称T1/T2有效关闭已贯通。下一完整实现块优先接交换机间T1/T2的该执行链，其他类别缺少连通性/主机侧消费者时继续阻断。

只执行源码审查、gofmt、npm run api:generate --workspace web（退出0）。新增复采重放/身份/范围/来源/时间拒绝和HTTP协议用例、预览摘要/分类需求单元用例，扩展隔离PG作用域/缺失处置/重复预览用例源码，均未执行。无编译、静态、数据库、浏览器或镜像验证，无迁移/部署/推送。源67迁移、运行64/最后067，3000仍7afda32；P1延期项与两份原草稿保留。


## 169. P2 工单定向复验执行、关闭与升级源码（2026-10-27）

- 在`/opt/keel`、`dev/p2-validation`单Agent接续`150ab3e`；用户再次要求重大里程碑集中测试。此批未运行测试、编译、类型检查、浏览器、镜像构建或迁移；只做精确文件gofmt及API类型生成。
- WP-2.7/2.14步骤D：当前仅开放有可信发现绑定、当前维修记录和固定规则的交换机互联T1/T2。准入、领取、收集前和关闭时重新核对授权、分派、BOM、身份、规则、阈值/意图与管理规划/凭据摘要；不要求后置Golden、G3或NP-3。设备来源与协议fixture互不替代，真实管理可达证据不可绕过。
- 专用POST创建共享fabric模式Execution、固定`issue.verify`任务与单链路冻结范围；现有幂等/权限/审计/outbox/资源fence复用，两个交换机同时取得`ssh:switch:<id>`独占租约。collector固定mTLS POST仅返回指定端口、不得扩展至全机数据；不自动重试。不可变原始证据与共享receipt同事务保存，领域PASS/FAIL/BLOCKED独立留档。失败或阻断不算成功，PASS只提议关闭，NE本人/DL/Admin确认时再次核对版本、有效期及当前事实。
- 原工单source/原始发现证据不覆盖；FAIL重开，BLOCKED回处置；活动或unknown隔离期间不能登记新维修绕过租约。连续两次实际FAIL生成一次DL升级通知与现有通知outbox，第三次不重复升级；缺证据不冒充失败。通知仅公共引用，无原始维修/凭据数据；现有sink需显式选择`issue.escalated`，本批没有发送外部通知。
- 页面已接范围确认、复验历史、关联Execution、当前可关闭提示、连续失败通知；API正式契约与生成类型同步。新增P2集成用例覆盖PASS关闭、失败升级、unknown不重放/双端隔离，通知投影与管理摘要变化用例已编写但**未执行**。外部collector/NOS适配与真机PoC未提供，不据此声称WP或P2验收通过。
- 新071含请求/证据/结果/升级不可变表及当前fence/双端锁作用域约束；源码68条迁移（不含026草稿），运行仍64/最后067。`KEEL_FABRIC_ISSUE_VERIFY_ENABLED`默认关闭，真实配置未改变；3000保持`7afda320ecebbf9670c61103239e5f9872b2ed49`，无部署/推送。两份原fabric草稿不读取、不编译、不迁移、不提交。
- 下一步：保留冻结发起请求，为unknown补固定collector查询、停止证明、显式对账与历史过期证据处理；不得重POST或凭404释放资源。之后按步骤D补FR-0.8读取/阶段结论与H-17材料，再评估转步骤E。


## 170. P2 原复采查询与 unknown 显式恢复源码（2026-10-27）

- 接续`c0beba6`，新增072：首次POST前提交不可变dispatch（原命令、fence、scope、not_before、目标与摘要）；同一请求不得二次POST。collector仅通过固定mTLS GET原command查询，返回绑定完整原请求摘要的`fabric-recollect-status/v1`。`completed`需原样结果及结束时间；`stopped/rejected`需停止证明，rejected要求持久墓碑拒绝迟到POST；running/unknown/404/重定向/超时均不能解锁。外部适配器仍待PoC提供与集中协议验证。
- 恢复入口仅Admin/DL/SRE显式操作，在网络查询前后重新鉴权；最终锁住执行与unknown attempt，核对原fence、双端reconciling租约，再同事务存不可变查询证明/证据/receipt。调用原有版本化Reconcile消费结果并释放资源，不重试任务、不新增Execution；重放复用原幂等结果。若本地首次dispatch尚未提交，控制锁+attempt锁下证明未发起，可保存`local_not_dispatched`并失败收口；HTTP404绝不能充当该本地证明。
- 历史完成按原finished_at校验，证据有效期仍来自原样本时间；查询时间不续期。过期、权限/版本/管理事实变化可证明任务已停止并释放隔离，但不能关闭工单。补执行抽屉“查询原复采并对账”确认入口与API契约，FlowTask只读枚举明确包含领域专用issue.verify，普通Flow发布仍不能自建该任务。
- 已编写固定GET/身份绑定/历史时间/404隔离、unknown恢复不重POST/幂等、领取后未发起崩溃恢复用例，**均未运行**。只做gofmt与API类型生成，无编译/类型检查/功能测试/浏览器/镜像/迁移/部署/推送。源迁移69条（不含026），运行仍64/067；3000及真实执行默认关闭状态不变。
- 下一步按步骤D核对FR-0.8分阶段读取与Readiness：定向链路证明只覆盖其冻结端口，不能替整网样本续期；NP-1建模与NP-4新鲜/LLDP可读必须分开，不依赖后置Golden来补早期模型事实。之后补H-17可执行材料与阶段缺口，不将源码完成写成验收PASS。


## §171 — N-S5读取边界、就绪分项与H-17材料（2026-10-27）

- 工作目录`/opt/keel`，分支`dev/p2-validation`，接续`7eb07e2`，单Agent。按用户要求持续开发，不运行逐步功能/数据库/浏览器/镜像测试。
- NP-1现只证明当前BOM交换机、稳定端口和线缆已建模；不因ZTP或遥测缺失改变建模事实。NP-4要求同身份、当前device采样的LLDP能力和所有建模连接端口可读；邻居错误/链路down由N-S5判定，不与“可读”混为一谈。NP-2改用当前管理身份/凭据/最新可达核验，仍需Golden；服务器NP-3保持BLOCKED。读取TTL取参数目录`gate.readiness_ttl`（默认30秒，仅允许1～30）。
- 线缆读取可叠加当前有效、同来源且晚于整网发现的定向T1/T2复验证据，返回工单/Execution/原始证明引用；仅更新冻结线缆，不替整网采样或NP-4续期，也不覆盖更新的整网观察。历史发现报告保持不可变。Fabric页面加入分项就绪/有效期、当前线缆和证据跳转，旧发现区明确为发现时快照。
- 新增`poc/fabric-h17/`的N-S5九类样本清单、离线材料检查器及合成测试；复用H-1的受限文件/路径/哈希校验，要求项目、实际设备来源、2交换机/8主机、规则/原始NOS证据及修复关闭的同Execution/fence/回执。输出仅READY_FOR_REVIEW，硬件UNVERIFIED，完整H-17及G2/NP-3仍NOT_RUN；不把N-S5子集视为完整H-17。操作材料见`poc/results/CHECKLIST.md`。
- 实际仅源码审查、Go格式化、OpenAPI/路由类型生成；新增用例、离线检查器、check-design、Go/前端/PG/浏览器/镜像验证均未运行，等待重大里程碑集中执行。源迁移仍69条（排除原026；068～072待应用），3000仍`7afda32`、运行库64/最后067，无部署、真实采集或推送。两份原fabric草稿未读改或提交。
- WP-2.7/2.14保持partial；步骤D本批软件链已有源码，关键失败软件验证及硬件PoC仍欠证据。下一步按步骤E补既有WP-2.15模板新版本回滚/影响分析和规划事实链，不追逐P3监控或后续服务器门禁。


## §172 — WP-2.15模板回滚新版本与升级影响预览（2026-10-27）

- 工作区/分支不变，接续`4580e76`继续步骤E。复用既有pongo2/批准规划/三层检查/仿真golden/独立NH发布/PushJob，未重建已有链路。
- 新迁移073为模板版本补不可变parent/rollback来源和原因；普通导入自动衔接同名版本，可携带head/hash做CAS。回滚只允许同项目同族历史released/deprecated内容，新建单调递增draft，拒绝过时head，不复制历史test_report或发布者；重新验证、独立发布及生产PushJob审批仍必需。服务端与DB同时约束回滚来源、定义及来源引用，审计落同事务；并发恢复只接受一个旧head请求。
- 新只读影响API按旧版本统计引用作业/批次/设备，并分页比较当前生产引用设备。以最新生产Execution为准，unknown/回滚/新执行不由旧成功掩盖；候选按当前批准管理规划和变量重新渲染，身份/基线变化或未确认Golden显示BLOCKED，返回路径/严重度及原Execution。此处仅潜在Golden diff预览，不冒称实时漂移或已创建修复任务，无设备I/O。
- 配置页面接入版本来源、影响分析和带原因/当前head的新草稿恢复确认；编辑NE/NDE与发布NH界面权限保持分离。OpenAPI及生成类型同步。新增PG权限/历史/并发用例及既有网络集中脚本的端到端场景，仅编写未执行。
- 实际只格式化、契约生成与源码审查；无Go/TS编译、功能/数据库/浏览器/镜像测试、迁移、部署或推送。源迁移70条（排除026；068～073待应用），运行库64/最后067、3000仍`7afda32`。两份原草稿保留。WP-2.15保持partial，下一步接模板升级差异的持久待修复队列与后续规划/变量Schema缺口。


## §173 — 模板发布持久队列接普通PushJob（2026-10-27）

- 接续`910a149`，仍单Agent在当前工作区。源迁移074新增不可变模板发布评估条目及作业关联，DB校验同项目/模板族/设备/原Execution。发布在同事务按最新生产引用登记所有旧版本设备，不在发布锁内逐台渲染或调用设备；这只是待复核工作，不能冒称观测到真实T6故障。
- 新分页读取按当前批准规划/完整批次渲染评估DIFF/UNCHANGED/BLOCKED；未知、回滚或更新的生产尝试不能被旧成功覆盖。没有匹配生产结果不记RESOLVED。整批facts_hash、当前生产引用、候选内容与所选条目绑定assessment_hash；从队列创建普通PushJob时在同控制事务复核，过时/已计划/候选停用拒绝，关联实际纳入批次的待办。
- 不另建推送器：普通PushJob保留Doublecheck/备份/预演/NE+DE与Critical NH审批/金丝雀/回读/失败恢复/unknown对账。失败后必须刷新后创建新作业、新审批；队列只有匹配候选版本回读和当前整批事实才显示RESOLVED。页面确认明确展示实际整个批次台数，无直接下发。
- 修正配置页面执行入口权限为Admin/NDE，与服务端fabric:plan一致。OpenAPI、生成类型、授权脚本不可变表权限及集中网络脚本场景同步；脚本新增发布队列→过期hash拒绝→普通审批/金丝雀→回读解决，但未运行。
- 实际只源码审查、格式化、契约生成；无测试/编译/迁移/镜像/浏览器/部署/推送。源71迁移（排除026；068～074待应用），运行64/最后067、3000仍`7afda32`，真实执行开关不变。WP-2.15保持partial，继续FR-0.2批准规划与变量Schema工程化。


## §174 — 变量范围与版本目录引用冻结（2026-10-27）

- 接续`1ed069f`继续WP-2.15。网络模板变量补minimum/maximum（精确有界十进制比较）、Unicode长度和RE2 pattern；拒绝倒置/类型冲突/过大Schema及恶意巨大指数，避免float64舍入使边界外整数被放行。原有普通变量编码/事实摘要保持兼容。
- 新version变量必须声明catalog_key，批准变量只接受严格`{param_key,value}`引用；字符串/version也可绑定目录。实际目录覆盖/停用墓碑、当前选项、型号cpu_arch、唯一批准且已存介质、固定依赖/完整兼容矩阵复用现有目录验证器，不回落手工值或默认架构。变量先解析成批准值再渲染，计划/审批facts保存CatalogSelection及介质ID/digest/object_version；目录/依赖变化重新准入即阻断。
- 只读模板/影响预览使用同一目录/依赖计算器的无行锁快照路径，现有ValidateCatalogTx和介质执行路径仍默认持原FOR SHARE语义；未复制另一套兼容规则。相同批次的目录选择/型号架构本地缓存，避免逐台重复解析相同组合。生产准备/下发的Execution准入另复核引用介质实际固定对象，不把元数据存在当成字节完整。
- OpenAPI和类型、页面Schema帮助同步。新增纯范围/Unicode/目录绕过与PG目录只读/锁定一致、介质固定引用、兼容矩阵及选项变更场景；仅编写未运行。
- 本批只格式化、契约生成和源码审查；未运行测试/编译/SQL/浏览器/镜像或发布推送。源仍71迁移（排除026；068～074待应用），运行64/最后067、3000仍`7afda32`。WP-2.15保持partial；下一步扩展同一批准基线的地址/命名规划和模板适用范围，后续集中验证新链路。


## §175 — 批准地址池派生、微调独立审核与下发快照（2026-10-27）

- 接续`68cbc6b`继续步骤E。批准基线可显式导入address_planning：Mgmt VRF、IPv4/IPv6地址池、用途、分配前缀、保留区、稳定对象与索引/host_offset、对端关系。引擎只执行客户规则，不选择空闲地址或发明设计；新增条目不使既有索引重排。管理地址仍复用C的管理规划；本批扩展loopback/p2p/bgp_peer，server_ip属后续依赖，不逆置最小ZTP前置。
- 新迁移075保存不可变规划及独立Admin/NH审核记录，同项目批准基线/管理规划/已提交网络BOM均绑定。NDE/Admin预览后按hash保存，微调必须有原因，审核人不得等于创建者；服务端与DB检查权限与跨项目关系。预览只读，池容量、保留地址、VRF重叠/管理IP/网关冲突、稳定交换机/物理端口及实际线缆对端必须成立。点对点双方微调需同单元且互为对端。
- 批准规划ID和各设备地址/端口/对端冻结进模板facts、PushJob、Execution适配器输入；旧无扩展基线保持原编码。配置工作台支持调整JSON、预览、冻结、独立确认和最近30项摘要/详情，结果每页50项。批准变化或BOM/端口变化使旧验证/审批facts失效；未批准新草稿不能替代当前批准结果。
- 使用新地址的模板测试和准备必须额外取得address-planning门控。协议桩的归一化JSON配置域为network_addresses，实际比对输入地址与渲染字段，提供不匹配故障；这不是所有NOS的配置格式，更不是设备固定PASS，真实适配器需验证自己的映射并回读。
- 新纯规则IPv4/IPv6/容量/保留/冲突/成对微调用例，PG独立审批/权限/不可变/事实失效用例，以及既有集中浏览器脚本的地址规划链场景，均只编写未运行。实际仅源码审查、gofmt与OpenAPI类型生成；未编译/测试、迁移、构建镜像、浏览器验证、部署或推送。
- 源72条迁移（排除原026；068～075待应用），运行库64/最后067、3000仍`7afda32`。原两份fabric草稿保留。WP-2.15仍partial，地址/命名完整人工调整及模板分类等缺口、软件指标与真机证据不能由本批源码视为通过；继续模板DeviceType×Role×配置域组织与准入。


## §176 — 模板型号×角色×配置域及整批适用门控（2026-10-27）

- 接续`34cf108`继续步骤E；模板definition.scope使用现有DeviceModel稳定ID、leaf/spine/superspine/mgmt角色和base/ports/mlag/bgp_evpn/rocev2/acl/system配置域。三个维度都需显式非空、唯一且有界，规范排序进入不可变definition/content_hash；不新建DeviceType库，不自动合并配置片段。
- 导入、回滚新草稿、样例验证、发布及生产计划/重验复用同一scope检查：型号必须当前项目/global可见、active、switch且存在已发布版本。整批每台目标都必须匹配当前BOM型号与角色；不静默删掉不匹配目标。DeviceModel与Role冻结进facts和渲染上下文，型号/角色变化使旧验证/审批不能复用。只读路径不加行锁，准入路径复核当前型号。
- 旧无scope定义保留原摘要与已留存历史，页面明确标记旧格式未限定，不为兼容自动伪造分类；新导入可以声明scope，界面支持按型号/角色/配置域筛选与完整scope审阅。项目模板覆盖和片段编排按后续WP-4.5实现，不前置到当前ZTP。
- 新纯scope/旧摘要兼容及PG型号未发布/跨作用域/退役/回滚拒绝、实际整批角色/型号不匹配用例仅编写未执行。修复模板队列确认表单提交默认刷新；API及生成类型同步。本批仅源码审查、格式化与生成，无编译/功能/DB/浏览器/镜像测试、迁移、部署或推送。
- 源迁移仍72条（排除026；068～075待应用），运行64/最后067、3000仍`7afda32`，两份原fabric草稿保留。WP-2.15保持partial；完整管理地址/命名人工变更与规定软件/硬件证据仍不能视为闭环。接下来补同阶段可复用的网络配置/事件读取链，避免抽屉占位；后续服务器G1只按独立前置实施。


## §177 — 设备配置/脚本/事件读取及最新Golden证据统一（2026-10-27）

- 接续`092d867`，步骤E新增项目作用域只读设备configuration/configuration-events API及抽屉按需面板。分别读取最近创建计划和按实际Execution创建时间排序的最近生产尝试，未知/回滚/未完成不得回退到旧成功。展示冻结意图、模板源、变量/介质/地址规划引用、变更前备份、准备差异与预检/派发/回读证据；不把备份当当前running，不执行脚本或采集设备。
- 复用实际PushJob事实准入计算器的只读路径；事务内不写审计/业务数据、不锁目录或型号。设备必须有当前项目BOM历史，其他项目设备404。不可变配置事件按实际关联设备分页读取；整批状态明确标batch，不表示该设备成功，也不返回其他设备专属事件。
- 提取统一Golden证据谓词供影响分析/修复队列、抽屉与NP-2复用。核对目标/SN/项目、版本/来源/执行终态、有效fence、所有verify门及批准intended摘要。NP-2修正原按job.created_at选择证据的问题，改为最新生产Execution顺序；只有真实device且5分钟内回读可用，并继续复核当前整批事实/管理可达；模拟不生成设备PASS。就绪有效期取Golden到期与原窗口的较小值。
- 页面只做本地时钟到期降级，无设备/投影轮询；Readiness过期时不继续显示任何PASS。抽屉脚本/事件Tab不额外读取无关采集详情，按需查询；焦点环补summary/textarea。新增用例覆盖整批事件分页、跨项目、只读无写入、先建后执行乱序、最近UNKNOWN及过期/错误来源/身份/摘要/门控，仅编写未运行。
- 本批仅源码审查、格式化、契约生成；未Go/TS编译、测试、SQL迁移、浏览器/镜像验证、部署或推送。源仍72迁移（068～075待应用，排除026）；运行64/最后067、3000仍`7afda32`，原草稿保留。WP-2.15仍partial，配置脚本读取不等于NOS适配器完整性、实时漂移或真机验收；下一步按独立前置推进WP-2.1 Redfish采集，为服务器G1提供数据，网络命名/管理人工变更与集中指标验证保留待办。

## 178. WP-2.1：Redfish 分级采集与原始证据基础（2026-10-27）

接续`d4d6a35`，唯一工作区`/opt/keel`、分支`dev/p2-validation`，单Agent。按WP-2.1独立前置推进，不提前实现G1/G2/G3或P3控制面。

- 新增只读BMC适配契约与L1/L2/L3有界遍历：固定HTTPS来源、可信TLS、禁止重定向/跨源凭据/动作链接，分页去重、资源/深度/字节上限；认证/超时/版本不支持/部分失败分类，凭据固定批次和版本，旧任务不静默切换新版本。协议依据[DMTF DSP0266](https://redfish.dmtf.org/schemas/DSP0266_1.20.1.html)。
- sim身份fixture与HTTPS使用同一采集器、限制和证据模型；sim不伪造健康、传感器、CPU/GPU/NIC事实。每份结构化资源绑定原始MinIO对象的精确版本/字节摘要，以及可经JSONB规范化的结构摘要。缺原始证据、身份不符或能力缺失不能标完整；不产生Gate PASS。
- 076迁移源码加入不可变请求/报告/资源与项目作用域约束；worker证据消费核验冻结请求、当前身份/权限/执行fence及原始字节。只读分页/详情/固定原文下载接口已接正式API与生成类型。静态freshness默认24h/半期aging，独立于网络30s就绪门。
- 已编写协议TLS/分页/去重/版本/凭据/证据篡改、对象固定版本及静态新鲜度用例；**均未运行**。本批只执行gofmt及OpenAPI类型生成，无测试、编译、数据库迁移、浏览器、镜像或部署。
- WP-2.1/FR-3保持partial：手动准入/共享Execution runner、cron/事件调度、实际凭据vault批次管理、IPMI回退及集中验收尚待接入；当前只读接口不提供虚构的采集成功入口。WP-2.2视图/导出尚未实现。
- 源码73条迁移（排除原026草稿），068～076待应用；3000仍`7afda32`、运行库64条/最后067。原`fabric_precheck.go`与026草稿保持隔离，不纳入提交。下一块先接共享Execution手动采集、BMC/设备资源租约与凭据版本准入。

## 179. WP-2.1：手动共享执行、凭据批次与资产页入口（2026-10-27）

接续`98eb650`，单Agent继续源码开发；用户再次要求加速连续两小时，仍在重大里程碑集中测试。本批完成手动采集功能链的源码，操作契约见[BMC说明](BMC-INVENTORY.md)。

- 新增BMC域准入，冻结当前服务器BOM修订、资产版本/序列号、BOM凭据批次/确切版本及受控来源/CA配置指纹；POST不接受端点、口令或任意任务。普通Execution承接outbox、租约、fence、心跳、取消/暂停和结果查询，没有第二个设备队列。只读设备共享锁与BMC通道独占锁兼容既有SSH/Agent/装机冲突语义。
- 077加入BMC模式、单次dispatch及设备密码版本/heads/撤销记录。读设备前必须有当前权限、冻结身份、两个资源租约和不可变原命令dispatch；同命令不重新采集。报告已提交但回执丢失时从原报告恢复；租约过期保留unknown/隔离，由原stopped回执显式对账。没有报告或stopped证明不伪造成功/自动重试。
- 设备密码使用独立的受控主密钥文件、AEAD项目/batch/scope/版本/有效期绑定。服务器所有者CLI只从0600文件读取口令，不进入BOM/argv/返回体/审计。head切换带CAS，新任务用新版本、存量保留旧版本至过期/撤销；轮换批次复验仍pending，不把单台采集当全量复验。共享表可承载switch scope，现有交换机执行器尚未因此接通该提供者。
- 资产页新增懒加载BMC入口、手动L1/L2/L3与原因、普通执行跳转、分页历史、独立质量/静态freshness/来源/身份/能力/失败。manifest与资源元数据分页，单资源正文/原始证据按需读取，不下载整份32MiB报告、不轮询设备。已补JSONB数字规范化，避免指数/小数表示变化破坏原始和结构化关联。
- 新增TLS/加密/固定绑定、PostgreSQL资源冲突/fence/项目隔离/不可变证据/丢失回执恢复/凭据轮换用例，**仅编写、NOT_RUN**。实际只做gofmt和正式OpenAPI类型生成，无编译、静态检查、功能/数据库/浏览器测试、SQL授权/迁移、镜像或部署。
- 源迁移74条（排除026原草稿），068～077待应用；运行保持64条/最后067、3000=`7afda32`。两份原fabric草稿保持不提交。WP-2.1/FR-3仍partial：下一块cron/事件触发与有界重试调度、IPMI兼容回退及凭据批次全量复验，随后WP-2.3 G1；不把手动采集当整个P2/G1闭环。

## 180. WP-2.1 自动采集策略、事务事件与有限重试（2026-10-27，源码）

- 接续`21bc6cb`，唯一工作区`/opt/keel`、分支`dev/p2-validation`、单Agent。用户两小时开发期间询问P2总进度，已说明源码工作量粗估约25%（20%～30%，非验收百分比）；继续当前功能块，未另起并行Agent。
- 新增078：持久策略、项目有序领域事件、不可变触发记录及父子重试链。策略按资产/级别独立，UTC五字段cron支持列表/范围/步长，漏掉多次周期只合并一次；同策略已有未结束任务时记录阻断，不堆积重复任务。不同级别独立准入后由现有BMC独占通道锁串行执行。
- BOM提交、BMC密码批次新版本导入、装机SSH验证后注册，在各自原事务追加领域事实；按项目锁定事件序号直到提交，避免晚提交记录被游标跳过。事件分别限定项目/Machine/当前BOM批次，没有浏览器任意事件写入入口；新策略/编辑从当前游标开始，不回放历史。
- HTTP手动与策略调用同一个事务准入核心，冻结当前资产/BOM/凭据head和策略/触发版本；worker只进行有界准入扫描（5秒/20条/3秒上下文），不在策略事务连接设备或读取密码，实际派发/租约/fence/回执仍属既有Execution。策略是持久授权：执行时重新锁定当前创建者启用状态、成员角色、项目生命周期；撤权留阻断记录并停用，不伪造会话。
- 重试默认0、最多3次、间隔30～3600秒，仅failed终态＋原停止回执＋不可变报告内retryable失败才创建新的普通Execution。父记录唯一子记录、当前策略版本/作用域和重试上限有DB约束；unknown/reconciling、cancelled、缺证据或仅unsupported不会自动重采。轮换全量复验仍pending，不以单台成功结束轮换窗口。
- 新增会话/CSRF/Origin/幂等/CAS策略API、分页历史及资产页按需展开面板；DE只能编辑自己策略，Admin/DL/SRE可管理当前项目策略。停用只阻止后续触发，已准入任务仍由执行页控制。缓存受限错误隐藏数据，查询无自动轮询；所有控件复用共享shadcn/ui与既有字体。
- 新增cron边界、并发准入去重/漏周期合并、不可变历史/CAS、事件去重/作用域/不回放、撤权停用、停止证据重试、unknown不重试、不同级别共享BMC锁等用例源码，全部NOT_RUN。实际只执行带overlay/内存限制的gofmt、`npm run api:generate --prefix web`（生成成功）；未执行编译、测试、类型检查、浏览器、SQL迁移、授权、镜像、部署或推送，不把生成成功视为功能验证。
- 源迁移共75条（排除原026草稿），068～078待应用；3000仍`7afda320ecebbf9670c61103239e5f9872b2ed49`，运行库64条/最后067，功能开关未变。两份原fabric草稿保留，不读取/编辑/提交。下一块IPMI回退、凭据批次全量复验/完整硬件覆盖，再接WP-2.3预收集/G1；WP-2.1/FR-3保持partial。

## 181. WP-2.1 IPMI只读兼容回退与降级证据（2026-10-27，源码）

- 接续`cd2c643`；本轮用户授权继续两小时，开始06:35:30 UTC，工作区/分支`/opt/keel` / `dev/p2-validation`，单Agent，无tmux/SSH绕转/子Agent。
- Redfish根服务超时、连接不可达、5xx或版本/服务不支持时，才允许显式配置的IPMI补充。认证拒绝/凭据过期/TLS证书验证失败保持原失败，不降级绕过。固定lanplus/cipher17/USER/端口623，IP与HTTPS来源独立匹配；只读Device ID/System GUID/Chassis Status/FRU0，不接受任意raw/OEM/配置/电源动作。
- 工具属主/权限/大小/SHA256固定，逐次重新校验且从固定fd执行；密码使用封印匿名memfd传给-f，无argv/环境/落盘口令。有限上下文、输出上限、进程组超时杀停/等待，不另起设备队列或重入原dispatch；继续沿用同一凭据版本及共享Execution双资源租约。
- 回复字节以带版本的hex JSON envelope进入原版本化对象库/JSONB，固定IPMI资源路径只能查证据，HTTPS解析器不能请求。FRU验证公共头、区域边界/校验和、字段长度，只有产品SN确认设备，板卡SN不代用；工具exit0仍必须通过独立二进制校验。缺少的处理器/内存/网卡能力不推定正常，fallback保留原Redfish原因，质量仍partial/failed；manifest/UI显示降级与来源。
- 增加受控Redfish/IPMI sim fixture文件，内容摘要冻结到adapter binding，real拒绝fixture；版本化样本在sim/fixtures/bmc。Dockerfile增加Debian bookworm固定ipmitool=1.8.19-4+deb12u2，TOOLCHAIN注明来源与配置摘要要求；只是源码，未执行安装/镜像构建或启动真实功能。
- 已编写原始证据/降级原因、FRU破损/SN防伪、不绕过认证或TLS、混源/原始层失败、fixture变更失效、匿名凭据fd/工具摘要与共享Execution保存查询等用例，全部NOT_RUN。仅执行带overlay/内存限制的gofmt、正式API类型生成（成功），未运行编译/测试/类型检查/浏览器/迁移/授权/镜像/部署/推送。开始时只读确认原3000/数据容器healthy及端口，未改其他进程。
- 仍75条源迁移（排除原026），068～078待应用，运行库64/最后067；3000仍7afda32。WP-2.1/FR-3保持partial，下一块凭据批次全量复验及型号硬件覆盖，再接WP-2.3预收集/G1；真实H-2和IPMI兼容PoC均NOT_RUN。原fabric两草稿未读取/编辑/提交。

## 182. WP-2.1 凭据批次整批复验与当前证明（2026-10-27，源码）

- 接续92df00a，仍在本轮06:35:30～08:35:30 UTC两小时开发窗口；工作区/分支/单Agent及草稿隔离规则不变。
- 新增079源码：不可变批次请求、完整成员与结果；当前范围由项目资产＋Machine SN＋非retired BOM项＋最新已提交BOM批次确定，不由浏览器传子集。预览冻结完整范围摘要、资产/BOM/适配器及当前密码版本，提交CAS并在同一事务创建1～200个普通L1 BMC Executions；超过200整批拒绝。同范围活跃请求去重，保留HTTP会话/当前角色/CSRF/Origin/幂等审计。
- 复用原调度/双租约/一次dispatch/原报告停止回执，不增设备队列。窗口最多2小时且不晚于密码过期，已结束/过期窗口不再派发；worker沿原5秒有界BMC tick汇总已终止整批或过期批次，无设备IO/自动重放。unknown/reconciling保持pending等待显式恢复或到期。所有成员有效身份＋原始报告＋停止回执/摘要＋精确密码版本才能判定，缺失failed，范围/head/适配器/创建者权限变更stale，到期expired。
- 身份型sim/静态fixture未实际使用密码，不能准入凭据验证；受控HTTPS mock可走密码流程但sim结果仅simulated。全real保留证据才passed并绑定head证明，不把partial采集补成完整，不声称G1。旧密码版本继续自然过期/显式撤销。有效状态重读当前全范围与配置；历史passed不代表当前verified。
- 受限角色只获head verification_receipt_id列更新，触发器校验完整真实证明并派生状态；仍不能改密码/版本/有效期。不可变密码版本行取消无必要且不满足app权限的FOR SHARE锁，只锁head；汇总依靠既有全局准入锁及唯一结果，不请求不可变请求表的UPDATE权限。Owner CLI缺少当前路由上下文时保守pending。
- API/OpenAPI/生成TS与按需展开的批次列表、范围预览、创建、分页历史及有界目标进度面板已接线；无自动轮询，不上传/展示密码。控件沿用shadcn/ui和既有等宽字体。
- 新增隔离PG/HTTPS协议mock用例源码：真实分支/模拟隔离、head轮换存量版本保留、范围新增拒旧预览、单台成功不能替全批、伪造结果/跨项目/不可变、unknown不重放、201台整批拒绝，全部NOT_RUN。real分支协议mock不是硬件PoC。
- 只执行源码审查、受overlay/内存限制的gofmt及npm run api:generate --prefix web（生成成功，/tmp/keel-bmc-credential-api-generation.log）；未运行编译/测试/静态/类型/DB/浏览器/镜像，未迁移/授权/部署/推送。源76条迁移（排除原026），068～079待应用，运行仍64/最后067，3000仍7afda32；所有新开关未启用。WP-2.1/FR-3保持partial，下一块完整型号硬件覆盖，再接WP-2.3预收集/G1；两份原草稿保留。

## 183. WP-2.1 L1～L3库存图覆盖与完整性（2026-10-27，源码）

- 接续4f9beb7，仍在本轮两小时开发中。补充有界库存envelope校验：HTTP 200还必须匹配资源类型、@odata.id、Members/count/nextLink；缺成员/缺页、分页循环、计数不一致和原始成员缺失均partial，保留原始异常文档与分类，不把“首个页面成功”当全量。
- 递归、HTTPS发出密码前、原始路径及fixture均限制Systems/Managers/Chassis/UpdateService库存树，拒绝Account/Session/Actions/OEM/Task/Log/Settings；补上NetworkAdapter/NetworkDeviceFunction等标准单数链接。unsupported能力不被后续成功页抹成collected。完整快照根据保留的类型及集合图重核覆盖，不能只改capabilities字典伪造完整。
- 默认sim仍仅确认身份，修正其DMTF集合类型；另加显式redfish-l3-sim.json合成样本，覆盖PRD所有级别与现代Power/Thermal子系统、网卡端口/设备函数、BIOS/启动项/固件。数量/容量/版本为SIM-only，不用于实际型号规格。库存对话框追加四类失败计数，复用原逐项原因与质量/保鲜状态。
- 已写分级完整图、缺失/类型/身份错误、成员计数、循环/截断分页、不可进入账号/OEM/动作、伪造能力字典的用例，全部NOT_RUN。只做源码审查/gofmt，无测试/编译/类型/DB/浏览器/镜像，无迁移/授权/部署/推送；外部只读核对DMTF公开schema，链接记入BMC-INVENTORY.md。
- 仍76条源迁移（排除原026），运行64/最后067，3000保持7afda32、新开关未启用。WP-2.1/FR-3仍partial，真实型号/兼容性与H-2未验证；下一块按WP-2.3做预收集/G1，WP-2.2视图/导出仍待实现，原fabric两草稿保留。

## 184. WP-2.3 只读预收集、原始证据与独立G1（2026-10-27，源码）

- 接续0866083（此前92df00a、4f9beb7），仍在06:35:30～08:35:30 UTC本轮两小时开发中。复用BOM已登记稳定Machine/Component/PhysicalPort、已发布型号、SSH信任及普通Execution，不以前置存在LLDP/Mapping/后置项目模板为条件。完整边界、接口与PoC限制见[PRECOLLECT-G1](PRECOLLECT-G1.md)。
- 专用任务host.precollect，1～200目标、最多4并发；原设备/SSH只读租约、fence、outbox一次dispatch、幂等审计与停止回执。固定只读sysfs/NVIDIA/ethtool/RDMA/PCI摘要，不运行用户shell、不安装工具、不启动LLDP或下发正式配置；前后仅核对MAC/MTU/管理状态，不冒充完整RoCE配置diff。
- 原命令私有0600 JSON最多2MiB，逐8192字节读取、原回执/command/fence/边界/逐块及全量SHA核验，版本化归档并反读；普通回执仅存小证明。主机25秒/探针2秒/任务30秒，停止证据回收15秒。只读授权可跨心跳续租而仍受原30秒边界，写任务原租约界限不变。unknown仅回读原命令，BOM/BMC过期不阻断回收资源，但要求原连接/信任匹配；关闭新采集不阻止归档已有原停止证据。
- 迁移080仅源码：不可变请求/记录/暂定端口/G1证据和作用域触发器，受限角色无UPDATE/DELETE/TRUNCATE；G1单独在Execution/attempt和原receipt已settled后判定，反核主机及BMC SN/IP的原始版本、当前身份/型号/BOM/端口/参数/适配器与权限。缺GPU仍计完整率分母，sim BMC不能与real主机拼PASS。TTL默认24h可收紧，并不晚于BMC静态保鲜期限。collected状态投影不增加身份version、不回退后续阶段。
- API/OpenAPI/生成TS、按需资产页入口已接线；分页历史/字段缺失与不一致/完整率/原版本下载/Execution链接；历史verdict和当前fresh/stale/invalidated分开，前端到期本地降级，无设备轮询。沿用shadcn/ui和全局字体。通用SSH/Agent/Host Flow/通用重跑不开放该任务。DOM未知保持BLOCKED，DL备注审批与范围豁免待WP-2.8；本步不声称G2/NP-3/Mapping/拓扑四维验收。
- 新增纯解析/门禁/分块协议/准入封闭及隔离PG缺前置与越项目读取用例，全部NOT_RUN。只做源码审查、限定overlay/内存gofmt、API类型生成（/tmp/keel-precollect-api-generation.log），没有编译/测试/静态/类型/DB/浏览器/镜像；未迁移/授权/部署/推送。真实账号权限、工具/模块兼容和200台传输性能留PoC/集中验证。
- 源迁移77条（排除原026），068～080待应用；运行仍64/最后067，3000保持7afda32，默认开关未启用；FR-4/WP-2.3为partial，P2未闭环。下一块WP-2.2视图/导出与WP-2.12拓扑，按实际前置推进；原fabric两草稿保留且未读取/编辑/提交。

## 185. WP-2.2 资产自定义视图、版本与导出（2026-10-27，源码）

- 接续99ea4bb，仍在本轮06:35:30～08:35:30 UTC两小时开发窗口。基于已有BMC报告、FR-0交换机快照与资产登记读取，不增加采集器或任务引擎；详细实现与限制见[ASSET-VIEWS](ASSET-VIEWS.md)。
- 个人/团队模板，稳定ID/expected_version/CAS/精确重复保护与不可变revision。个人仅本人，团队当前成员可读，原作者或Admin/DL可改；团队写还需既有工程角色。范围/作者/项目不可改，分享为新团队副本。会话/成员/项目生命周期、CSRF/Origin和审计沿用平台；迁移081及授权脚本仅源码。
- JSONPath有界属性/引号键/索引/数组通配子集，最多16列/16段/32匹配/4KiB单元；没有脚本/递归/过滤执行。来源超2MiB、缺失/partial/失败/模拟/身份或BOM变化/陈旧均明确标记，不填默认观测。条件着色绑定当前项目覆盖全局目录及原目录版本，退役不回退；不冒充后置正式项目模板或配置合规门禁。
- 统一服务器/交换机范围最多200目标，先全范围投影排序分组再分页，超限整次拒绝；完整CSV保留来源/状态/目录基线和摘要、防公式注入，另可导出模板JSON。资产页按需加载shadcn编辑器，新增/移动列、排序分组、个人/团队复制共享与手动刷新，本地到期取消有效着色，无自动设备轮询；字体沿用现有全局设置。
- 已写路径/边界、目录比较/模拟/陈旧/缺失、数值排序/分组、CSV注入/元数据及隔离PG个人隐私/团队权限/精确重复/CAS/旧revision/跨项目读取用例，全部NOT_RUN。只做源码审查、overlay/内存限定gofmt、API类型生成（/tmp/keel-assetview-api-generation.log），无编译/测试/静态/类型/DB/浏览器/镜像，无迁移/授权/部署/推送。
- 源78条迁移（排除原026），068～081待应用；运行仍64/最后067，3000为7afda32、新开关未启用。WP-2.2/FR-3和P2保持partial，真实字段适配/200台性能/集中验证留后续；下一块WP-2.12拓扑/T7/Step1，尚不提前进入依赖未齐的LLDP/G2。原fabric两草稿未读取/编辑/提交。

## 186. 本轮收尾：采集版本、驱动版本与未知语义（2026-10-27，源码）

- 接续1b30813。冻结范围补显式collector_version，NIC驱动名称/版本分离并计入完整率；N/A/unknown/Not Supported/Unable to等成功退出但未知的正文不再当有效拓扑/驱动事实。原raw仍保留，未读取值不补默认。
- 合法原包中的GPU CSV格式/身份异常改为报告缺失并保留其他事实，接口配置无法读取也保持可归档但不完整，避免已经停止的采集仅因工具格式不兼容而无法记录原证明。超长SN标记未知，不伪造身份。最终化扫描先筛已settled的原成功停止证明，unknown/缺receipt不挤占有限批次。新增语义用例源码NOT_RUN。
- 下一步：先补/明确独立发布型号对管理口、非RDMA口、铜口/特殊DOM的适用性，优先消费PRD附录A已有capability_matrix.ports[port_index].sub_type和光模块media_type/speed及BOM派生来源，不新增拍脑袋豁免字段，不能从本次观测反推无需检查；当前规则遇这些缺失仍BLOCKED。此项与真实工具/权限留PoC对接，尚无硬件PASS。随后按WP-2.12做结构化PCIe/NVLink/LnkCap/LnkSta、预期对比与T7/Step1，G2/NP-3/Mapping前置仍不虚构。
- 本轮06:35:30～08:35:30 UTC持续单Agent开发，源码新增IPMI回退、整批密码复验、库存图完整性、预收集/G1、资产视图和本收尾；全部新软件与硬件验证NOT_RUN。只做定向源码审查、受限gofmt和契约生成，没有测试/编译/静态/类型/DB/浏览器/镜像；无迁移/授权/部署/推送。源78条迁移（排除原026）、068～081待应用，运行64/最后067、3000仍7afda32、新开关仍未启用。P2保持partial，P1带遗留项收口决定不变。原fabric两草稿未读取/编辑/提交。

## 187. WP-2.12拓扑采集与版本化对比（2026-10-27，源码；窗口进行中）

- 本轮从09:05:21 UTC继续单Agent两小时开发，计划截止11:05:21 UTC。接续9b76d0d；采集/四维核心本地提交a3eb2a9。新增sysfs Root/上游BDF链、NUMA/独立CPU socket、PCIe capability端口类型、最大/当前速率与宽度；固定NVIDIA index/UUID/BDF/序列号、topo及NVLink状态查询，保留原raw，不配置硬件。
- 独立型号expected_topology严格schema与来源记录，槽位设计带宽独立于设备最大能力；实际BDF不排序生成预期。GPU用部件SN、NIC用既有永久MAC证据关联稳定Component。缺SN/路径/亲和或不兼容格式为BLOCKED；拓扑格式异常不妨碍原停止证据归档。详见[PCIE-TOPOLOGY](PCIE-TOPOLOGY.md)。
- 源码迁移082：不可变PciTopologySnapshot/TopologyDiff/T7发现绑定，验证消费最新有效G1的同一原始采集，反读raw校验；型号/BOM/身份/G1变化使旧结果失效。显式POST比较、幂等与审计，GET不生成证据；确定差异开/重开T7。资产页按需加载上游树、NVIDIA亲和对照、NVLink状态、链路能力/设计/状态、差异及工单入口；复用shadcn与全局字体。当前树为无新依赖的可访问层级展示，完整React Flow交互未实现，不视为DS全量完成。
- 四类异常fixture、独立预期/打乱BDF/unknown/sim/槽位设计/配置空间环、隔离PG无G1/跨项目/GET无写入用例仅编写，NOT_RUN。实际只做源码审查、受限gofmt和API类型生成（/tmp/keel-topology-api-generation.log）。无编译/测试/类型/静态/DB/浏览器/镜像、迁移/授权/部署/推送，源79条迁移（排除原026），运行64/最后067、3000仍7afda32。
- 接下来继续T7修复后全新预收集→新G1→拓扑重验的有效关闭证据与连续失败升级，再接Step1和H-13。WP-2.12/P2未闭环；管理/非RDMA/特殊DOM适用性仍缺独立明确依据，不猜测豁免。原fabric两个草稿未读取、编辑或提交。

## 188. T7修复后新证据重验、确认关闭与连续失败升级（2026-10-27，源码；窗口进行中）

- 接续1025bb6，继续09:05:21～11:05:21 UTC单Agent窗口。T7只接可信拓扑发现绑定，不接受操作员上传PASS；每个修复版本至多一次重验，必须修复登记后新发起预收集，待新G1及拓扑比较完成再提交，旧快照/不同设备或来源、过期/变更范围拒绝。完整拓扑PASS进verify，仍需已有工单显式确认关闭；关闭重新核对当前G1/型号/BOM/原身份与重验者当前权限，不能用历史PASS代替有效证据。
- 连续两次独立修复的确定FAIL落不可变DL升级、站内收件箱与既有Execution outbox；缺证据/BLOCKED不伪造失败次数，持续失败不反复新增同一升级。没有DL时明确记录无接收者；不自动刷BIOS、重试写操作或断言主板故障。人工处置登记仍不执行设备命令；固件mock执行/批准回执尚未接，F-4.16整体仍partial。
- 新083仅源码，保护重验/升级记录的项目/机器/来源/修复时间/版本关系；授权脚本只编辑未运行。工单页增加T7专用前置预览、资产采集入口、当前快照证据提交、重验历史与升级提示，复用shadcn/字体及标准确认关闭。手工T7缺可信绑定不能借该入口闭环。
- 补操作员伪PASS、跨项目普通成员、缺修复/快照/原因、GET无写入源码用例；修复已有资产视图用例误把管理员作为普通DE的fixture。补不完整拓扑仍能保留原停止证据的源码用例。全部NOT_RUN；只进行源码审查、受限gofmt和API类型生成（/tmp/keel-topology-retest-api-generation.log）。
- 源80条迁移（排除原026），068～083待应用；运行仍64/067、3000仍7afda32；无测试/编译/类型/静态/DB/浏览器/镜像、迁移/授权/部署/推送。下一块拓扑→G2 Step1只读消费、Fleet筛选与H-13执行材料，随后按依赖进入LLDP/四步核查；P2未闭环，原fabric两草稿未读取/编辑/提交。

## 189. GPU-NIC独立关联投影、拓扑总览与H-13材料（2026-10-27，源码；窗口进行中）

- 接续c808940。型号严格schema补gpu_nic_map独立关联与预期affinity，对比引擎消费，不从BDF/NUMA距离生成对应关系。快照版本内的关联投影携带Component/PhysicalPort稳定ID、GPU UUID、永久MAC/接口、路径/NUMA、链路能力与状态；缺预期端口保留BLOCKED行。此投影只是Step1输入，不生成StepReport/G2/NP-3；网络就绪握手和双端LLDP仍是完整G2的前置。
- 增200台上限的项目拓扑总览，按当前有效PASS/FAIL/BLOCKED/未采集先筛选后分页，行可打开拓扑详情；不是WP-2.11的完整全局Fleet或详情抽屉。过期/型号/G1/目录范围变化不沿用历史PASS；失效原因仍允许查历史。复用shadcn、设计令牌和全局字体，无新依赖。
- 新[poc/topology-h13](../poc/topology-h13/ENVIRONMENT.md)：B300 amd64与GB300 arm64分别取独立型号期望；模板NOT_RUN。离线脚本复用既有有界/摘要/路径安全读取器，核对原raw、发布型号和独立规格原件、NVIDIA逐项语义、G1和原fenced停止回执、样本导出有效期。成功仅READY_FOR_REVIEW且hardware_acceptance=UNVERIFIED，不签署真机、P2或性能。未调用脚本或接触硬件。
- 新增独立关联不得由实际自证、缺端口不缩结果、当前阻断不变成有效判定、项目筛选/分页/无写入/普通成员越界及H-13正反向材料用例；全NOT_RUN。实际只做源码审查、受限gofmt与契约生成（/tmp/keel-topology-relations-api-generation.log），无测试/编译/类型/静态/DB/浏览器/镜像、迁移/授权/部署/推送。
- 源迁移仍80条（排除原026），068～083待应用；运行64/067、3000仍7afda32，新开关未启用。WP-2.12保持partial（React Flow完整交互、批准固件mock回执及真实PoC/性能未完成）；T7证据重验源码已接，下一块目标级网络握手→固定LLDP采集/双端回读/后续四步核查，不能要求先有NP-3。原fabric两草稿未读取/编辑/提交。

## 190. 目标级布线就绪握手（2026-10-27，进行中检查点）

接续1553049，单Agent在/opt/keel、dev/p2-validation开发。本窗口09:05:21～11:05:21 UTC继续进行，此检查点不结束两小时任务。WP-2.4新增明确目标/物理端口的只读scope与显式mTLS采集器健康握手，复用现有NP-2认证管理+Golden判据；不探测装机服务，不要求NP-3/5/6。G1/拓扑/BOM/端口原身份、相关交换机NP-1/2/4均须当前有效；探测前释放事务，之后重新鉴权和核对事实摘要。不可变凭证最长30秒、绑定全部事实及源证据期限，GET不探测或写证据，历史不作为准入。

API契约/类型、拓扑关系页端口选择与握手历史、CSRF/工程角色/幂等/审计、迁移084及只读证据授权脚本源码已接；KEEL_WIRING_ENABLED默认false，真实环境未改。编写范围规范化、缺G1/跨项目/拒绝不留证据、采集器检查不依赖引导服务的用例，全部NOT_RUN。只执行gofmt、API类型生成和源码审查；生成日志/tmp/keel-wiring-api-generation.log，不算测试通过。

迁移源81条（排除原026），068～084未应用；运行64条/最后067、3000仍7afda32。未编译、测试、浏览器、镜像构建、迁移、发布或推送，原两份草稿未动。设计细节及未实现执行绑定见WIRING-VALIDATION.md。下一块固定LLDP双端协议/共享Execution与四步StepReport；P2保持partial。

## 191. LLDP双端证据与三方比对核心（2026-10-27，进行中检查点）

接续9b1ed3d，新增internal/wiring纯判据、json0严格解析和固定逐物理端口lldpd计划；稳定交换机MAC绑定加入握手事实摘要，模型/BOM期望不从邻居反推。Step2全选中端口和双端证据完整后才比较Step3，不以缺测/多邻居/旧快照/sim输出通过。ChassisID、物理PortID、hostname、精确SN/rail/port描述逐项核对；独立实例避免多NIC共用全局身份。缺少停止、原接口恢复和配置保留证明也不能PASS。详细约束/官方来源见WIRING-VALIDATION.md。

已编写协议/纯逻辑用例，全部NOT_RUN。仅gofmt与定向源码审查；没有编译、测试、真实LLDP/特权动作或服务变更。现有SSH wrapper必须保持非root，真实lldpd/临时up仍需受控特权适配器及共享Execution接线，不自动sudo/安装/启动。源81条迁移、运行64条/最后067、3000仍7afda32；窗口继续至11:05:21 UTC。下一块Step1与StepReport持久化，不冒称LLDP执行或G2闭环。

## 192. Step1快照与不可变四步记录（2026-10-27，进行中检查点）

接续a48ff7f，本轮继续至11:05:21 UTC。新增唯一消费就绪握手的batch，冻结目标/端口期望及独立GPU→NIC关系，生成每台Step1版本1；前后鉴权/当前范围核验、幂等、审计、项目作用域保留。NE按US-18可发起布线相关握手/Step1，不改变拓扑验证权限。握手消费后返回batch_id并拒绝重用；Step1当前有效性独立跟随拓扑，不因初次30秒网络凭证过期直接抹掉历史。

迁移085增加不可变batch/target/StepReport及同范围前驱PASS/单调版本约束，当前明确拒绝步骤2～4插入，留待原始LLDP执行回执绑定接入。API/类型、握手入口中的四步时间线及到期展示已接；后3步未执行、无G2/NP-3/Mapping发布或资产状态推进。源码82条迁移（排除原026），运行仍64条/最后067，3000仍7afda32。

编写缺少握手不生成batch/跨项目读取与提交拒绝用例，全部NOT_RUN；只gofmt、API类型生成和源码审查，日志/tmp/keel-wiring-step-api-generation.log不代表测试。未编译、测试、迁移、发布、推送、特权接口操作或安装lldpd。下一块真实受控LLDP特权适配器和共享Execution（原wrapper保持非root），再接双端归档、Step2/3和定向重验。

## 193. LLDP会话窗口与停止恢复协调器（2026-10-27，进行中检查点）

接续0715517，internal/wiring增加设备Driver/Session接口与25秒观察+独立3秒cleanup协调器。明确交换机回读必须发生于主机持续广播期间，读取后才停止并恢复；核心对比新增advertising_stopped_at，停止后/广告前快照不能通过。未知启动不重试，缺停止/恢复/原配置一致性证明保持unknown；客户已有LLDP或不支持的接口归属开始前阻断。归档包含原json0、接口前/后状态、计划/命令摘要、停止证明和回读，DecodeArchive反向重建判定投影，不信任可改的邻居字段。

模拟Driver与窗口顺序、观察失败仍cleanup、unknown不重启、客户LLDP冲突、归档篡改/配置变化的源码用例已编写，全NOT_RUN。当前Driver接口/协调器不是已安装特权设备适配器，也不是新任务引擎；尚未连接共享Execution持久会话、派发重验和原回执，所以Step2～4仍被阻断，不声称G2可执行。

仅gofmt及定向源码审查，无编译/测试/浏览器/镜像/迁移/发布/推送/提权操作。源82条/运行64条最后067、3000保持7afda32，原草稿未动。当前两小时窗口继续至11:05:21 UTC，下一步基于该边界接固定回读适配及原执行对账。

## 194. 固定交换机回读与持久派发边界（2026-10-27，进行中检查点）

接续126c34e，internal/wiring将固定mTLS采集器RecollectPorts/QueryRecollection接入广播中的回读，逐交换机按最多2端口分块，不扩大原FR-0.8.2协议。命令ID由原Execution/attempt/command/fence/计划/范围及规范化目标确定；每块先持久派发意图再POST，未知只查询原命令，丢回执或保存失败不能重新POST。保留全部请求/响应，读取归档时逐块校验命令/身份/来源和广告窗口并重建合并投影，不能用合并最早时间掩盖晚采样。交换机状态unknown时，即使主机停止恢复也仍隔离。

迁移086与platform持久账本源码绑定共享attempt/fence及主机device/ssh、相关交换机device/ssh独占锁。开始命令须当前Step1、完全相同物理身份及新的单主机30秒握手；旧Step1握手不作派发证据。握手消费跨Step1/命令统一行锁防重用；每条命令的广告时间不可更换。账本事务在mTLS调用前结束。这里只交付可接入的持久边界，未创建或派发实际wiring Execution，也未安装真实特权helper/修改普通SSH wrapper，Step2～4仍由085拒绝、无G2/NP-3。

新增分块规范化/原命令恢复/持久化失败/不完整状态/原文投影/collector未知后主机cleanup用例，全部NOT_RUN。实际仅gofmt和源码审查，无编译/测试/迁移/授权脚本执行/浏览器/镜像/发布/推送。源83条迁移（排除原026）、运行64条/最后067，3000仍7afda32。原两草稿未读取或修改。09:05:21～11:05:21 UTC窗口继续，下一块停止原文的持久归档与Execution绑定。

## 195. 原命令终态与停止归档持久证据（2026-10-27，进行中检查点）

接续8bc6b14，固定回读查询完成/停止/拒绝须保存原始终态证明，保存失败仍unknown；不能把HTTP缺失或running作为停止。新增087不可变终态查询及capture记录，绑定原注册命令的设备/模拟来源。执行消费者先反向解析原始归档和冻结期望、校验每块回读与持久账本、确认无未决采集器会话，再保存版本对象、摘要/大小并反读校验；事务与对象存储I/O分离。重复同原文读取原证据，冲突不得覆盖。原始采集失败的缺测保留，不能填造时间或PASS。

ReadWiringCapture重新打开固定版本对象、核对SHA256/大小、重建本地/交换机投影并重新计算原停止时刻的历史Step2/3判据，核对保存判定和逐块原始响应，不把存储的派生PASS当真。归档消费目前只是Execution内部持久接口，无HTTP上传/任意对象键入口，不生成StepReport、当前G2、NP-3、Mapping或资产状态；真实/模拟Driver来源由命令注册方固定。真实特权helper和共享Execution派发尚未接通，后续仍须原始已完成runner receipt后才能开放步骤2～4。

新增历史范围/来源替换、派生交换机篡改、collector终态保存失败和缺Step1/握手/命令/锁不得写证据用例，全部NOT_RUN。仅受限gofmt与定向源码审查，未编译/测试/迁移/授权/发布/推送。源84条迁移（排除原026），运行64条/067、3000仍7afda32；原草稿未动。当前两小时窗口继续至11:05:21 UTC。

## 196. 独立双NIC/双端协议模拟适配器（2026-10-27，进行中检查点）

接续1c4cb45，增加固定来源NewFixtureDriver与NewFixtureCollector：前者只模拟接口读取及会话开始/观察/停止，后者只处理当前冻结命令/广播窗口的定向请求和稳定原终态查询；无文件写入、设备/网络访问或子进程。独立json0/接口/交换机观测不由BOM渲染，永久MAC或期望被修改不会自动改变样本；JSON不能覆盖sim/protocol_fixture来源。sim/fixtures/lldp保留双NIC/双交换机正常、T1断链、可读空邻居、不可读、T2错口、停止未知、丢回执及running八组样本和独立期望。

新增纯规则/完整协调器/来源隔离/未知及稳定回执用例，真实门禁仍不允许模拟PASS。读取的物理链路字段保留在原交换机响应，但LLDP派生投影仅保留声明可读的接口/邻居字段，避免窄能力投影带入另一类事实导致矛盾。全部NOT_RUN，未编译或做功能/数据库/浏览器/镜像验证；只gofmt和源码审查。尚未注册实际共享Execution派发/真实特权helper，不开放Step2～4；源84条/运行64条067、3000仍7afda32。当前窗口继续至11:05:21 UTC。

## 197. 注册执行命令的主机会话持久恢复（2026-10-27，进行中检查点）

接续4504314，新增内部RunWiringCommand/QueryWiringCommand消费者，复用已经注册且持有共享Execution命令/fence/全部主机与交换机独占资源的上下文；不是新调度器、任意命令或HTTP上传入口。开始前先提交唯一主机启动意图，重复调用不再Begin；前置检查明确拒绝可保留local_not_started。命令配置、固定Driver来源和采集器由服务器供应，普通非root SSH wrapper不变。

交换机未知而主机已停止时，UnknownCapture携带可独立保留的主机原始停止/恢复归档；088保存不可变版本对象索引和host_evidence_hash，资源继续隔离。Query只GET原交换机命令、保留原终态时间与结果，不重新广播或POST；通过旧停止窗口组合原证据，host hash不允许被晚到回执改写。原查询超过五分钟仍按原FinishedAt验历史，不续当前门禁。采集器未终态仍unknown；双方确实停止但窗口不适用/范围仅部分采集时保存BLOCKED原文，不把数据缺口等同持续运行。记录原文不生成共同runner终态或释放隔离，显式授权的Execution对账仍须后续接入。

目前共享Execution创建/worker路由、真实特权helper、已完成原runner回执后的Step2/3持久化/开单、DL Step4确认尚未接通。085继续拒绝Step2～4，无G2/NP-3/G3发布；不能把内部消费者当作可用执行入口。独立驱动/collector模拟和恢复语义用例已编写，全部NOT_RUN；只受限gofmt、定向源码审查与状态文档更新。追踪表将实际已有源码的WP-2.2/2.3/2.4/2.9(H-13子集)/2.12/2.13从planned改partial，不改验收状态。

源85条迁移（排除原026），068～088均待应用；运行64条/最后067，3000仍7afda32，P2开关未改。无测试/编译/类型/静态/DB/浏览器/镜像/授权/迁移/发布/推送，原两份草稿未动。窗口继续至11:05:21 UTC，收尾时另记录最终提交。

§197补充（源码，NOT_RUN）：完整回读聚合超过2MiB时仍保留独立有界主机停止原文，原交换机分块留在账本；只查询恢复确认所有会话终止后，若完整聚合仍超界则保存BLOCKED主机证据，不重新运行设备。新增超界不丢停止证明、缺模拟源必须返回原拒绝tombstone的用例，未运行。低Token入口仅保留当前窗口最新检查点，前序细节保留在历史章节。
