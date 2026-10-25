# 当前开发状态

更新：2026-10-24；最新开发事实见末尾增量记录，早期表格为对应批次快照。本文件只记录当前快照，操作方法见 [DEPLOYMENT](DEPLOYMENT.md)，开发契约见 [DEVELOPMENT](DEVELOPMENT.md)。

## 1. 工作区与运行版本

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
