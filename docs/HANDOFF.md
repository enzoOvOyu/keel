# 当前开发状态

更新：2026-10-23。本文件只记录当前快照，操作方法见 [DEPLOYMENT](DEPLOYMENT.md)，开发契约见 [DEVELOPMENT](DEVELOPMENT.md)。

## 1. 工作区与运行版本

| 项目 | 当前记录 |
|---|---|
| 工作区 | `ubuntu@43.207.20.26:/opt/keel`；tmux `codex-keel` |
| 分支/本轮起点 | `dev/p1-network-bom`；整理文档前 HEAD `c8955ca`（docs），最近代码提交 `0a5620a`（此前 `df95d0b` 为旧仿真增量） |
| 历史 P1 基点 | `p1-complete` = `e7d5f1a`，为当前开发分支祖先；该标签只代表当时的 P1 范围，不证明 v3.0.3 新增网络任务完成 |
| 3000 预览 | `keel:0a5620ace390a9050135d46808909b6d1cc5490c`；本轮 `/healthz` 实测版本一致、容器 healthy，规划UI/CSS摘要与已验证构建一致 |
| 运行库 | 当前镜像包含28个已发布迁移：001～025 + 027网络角色 + 028规划/自检 + 029统一目标/Kea执行；未包含026 N-S5草稿。新表已授权受限应用角色，不改变既有成员授权 |
| 开发入口 | API 3001；Vite 5173。与 3000 构建镜像分别检查 |
| 工作状态 | 用户已恢复开发；Kea统一Runner与前序准备模块已提交、推送并发布3000；真实写入默认关闭 |

版本是本次观察，不是永久常量。恢复时检查 `git status -sb`、`git log -1` 和 3000 `/healthz`；不能凭本文件推断后续运行状态。

## 2. 已有能力与证据边界

| 范围 | 已有开发记录 | 剩余/不能声称 |
|---|---|---|
| 认证/项目 | 本地认证、服务端会话、CSRF、撤销、项目授权；账号/成员/审计管理已有交付记录 | NDE/NH成员角色已加入CLI/API/UI；当前已实现fabric:read/plan，NDE可执行本项目ZTP。LDAP/OIDC、审批签名/生产下发等剩余权限及完整项目生命周期仍未闭环 |
| 平台基础 | P1 执行仿真、介质/参数、型号不可变版本及相关页面已有记录 | 仿真执行不证明真实 Agent/SSH、介质分发和所有平台业务完成 |
| BOM/型号 | 标准包/宽表兼容、原子幂等导入、稳定身份、问题清单、部分型号/工程检查 | 完整商定基线、外部批准证据验真、全规模性能和剩余工程校验未闭环；F-0.1～F-0.4 不记 PASS |
| ZTP | 身份反查、mock Kea staged/active、DORA 契约、239/143 专用 listener、仿真九步时间线、自动重试和凭据模拟分类 | 完整真实 Kea 作业接入、个性化最小管理配置、真实凭据取用/可达验证、网络环境自检及真机开通未闭环；sim.managed 不投影真实 NP-2 |
| 后续 FR-0 | N-S5 数据表与 Store 草稿已写入工作树 | 未接 HTTP/UI、未完成评审/测试，不能计 WP-2.14 完成；Golden、无损/健康、规则治理、Readiness 与全链归档仍未闭环 |
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
