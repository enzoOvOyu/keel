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
