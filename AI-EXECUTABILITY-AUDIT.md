# AI-EXECUTABILITY-AUDIT.md — keel AI 可执行性审计处置索引

| 文档属性 | 值 |
|---|---|
| 当前索引版本 | v2.5 |
| 修订日期 | 2026-10-12 |
| 当前基线 | 五份设计 v2.5 与仓库根 AGENTS.md |
| 性质 | 历史审计归档与处置索引；本轮用户已授权设计修改，旧“只审不改”仅描述历史会话 |

## 当前状态

| 发现组 | v2.5 处置 | 状态与证据 |
|---|---|---|
| B-1 状态枚举 | ARCH 状态机为 SSOT；补 running/暂停/终止/unknown 回执语义 | 设计已解决；F-1.11/1.12 待实施验证 |
| B-2/B-4 页面交互与 API | URL 筛选/项目、页面快照/活动订阅/历史、懒加载与游标恢复契约 | 设计已解决；U-11/U-14/P-6 待验证 |
| B-3/B-6 状态覆盖 | 组件继承完整状态集；unknown 不等于 down，灰度不降低文本对比度 | 设计已解决；U-5/U-8 待验证 |
| B-5 组件与 WP | 34 组件来源、状态、变体按 DS 引用；登录复用，不增加虚构组件数 | 设计已解决；D-6/E-3/U-12 待验证 |
| B-7 新鲜度 | 门禁与监控快照按数据源/档位/有效期判断；陈旧和缺失不通过 | 设计已解决；F-4.19 待验证 |
| C-1 详情/协议 | 单抽屉返回上下文、SSE 断线补齐、时间窗查询与 scheduler 计划 API | 设计已解决；U-8/U-14 待验证 |
| C-2 令牌/表格/表单/Tabs | 单一 token 源生成主题，组件状态和表单/Modal/Tabs 规格、独立虚拟化、触控密度策略 | 设计已解决；U-5/U-9/U-15 待验证 |
| C-3 任务/容量 | 项目和登录 P1、mon ingest 先于 portmon、逻辑 sim 分层、Pause/Kill P1、固定 fixture | 设计已解决；WP-1.13/1.14、D-7、P 项待验证 |
| C-4 验收 | 当前计数和 FR/WP/验收追踪、H-13→P2/H-14→P3、状态矩阵、明确性能计时 | 设计已解决；D-8 与 docs/requirements-traceability.json |
| C-5 历史处置 | REVIEW-FINDINGS 顶部区分已解决/开放/失效，旧结论不作当前规则 | 本文档修订完成；业务实现尚未验收 |
| D/E 旧粘贴块 | **整体停止作为自动套用模板**；含过期 session+store 项目源、整体 opacity、token 双源、AGENTS 假定路径和旧排期 | 旧建议失效，以 v2.5 设计与根 AGENTS.md 为准 |
| F 生成检查 | 路由/API/组件状态/令牌/权限/单位/范围/证据/验收贯穿 DoD；介质登记与下发区分 | 当前执行规则见 AGENTS.md，不能因历史清单自行扩大停工范围 |

## 历史原文阅读规则

下文保留原审计证据，不代表当前缺陷仍存在，也不代表旧建议全部正确。**根目录 [AGENTS.md](AGENTS.md) 是实际行为文件；E-1 仅是旧建议范例，不是另一个生效的代理指令。** 五份当前文档不在 `docs/` 子目录中。

旧建议中“55fps 等于 60fps 的 ±5% 容差”计算和验收逻辑无效；默认不放宽性能阈值。禁止直接粘贴整体 opacity 的可读性冲突、重复 CSS/YAML token、session 全局项目上下文。新组件/代码是否完成，以 HANDOFF、CI 和运行证据为准。

---

## 历史原文：v2.3 文档集 AI 可执行性审计报告

| 文档属性 | 值 |
|---|---|
| 版本 | v1.0 |
| 日期 | 2026-10-11 |
| 审计对象 | v2.3 功能扩展版五份文档（PRD / ARCHITECTURE / DESIGN-SYSTEM / IMPLEMENTATION-PLAN / ACCEPTANCE）+ REVIEW-FINDINGS |
| 审计标尺 | Beszel（列表页 IA/离线态）、Kestra（详情页 Tab/三重编辑视图/key-based 筛选）、Semaphore UI（任务面板）、shadcn dashboard-01 骨架、DESIGN.md 规范（YAML token）、web-interface-guidelines（MUST/NEVER 契约）、TanStack Admin（URL 参数同步） |
| 审计性质 | **只审不改**：本报告为诊断结论与建议；铁律"增量优化不推翻重写"已遵守——所有建议保留既有业务决策/IA/技术栈，仅补 AI 执行缺口 |

---

## A. 诊断摘要

文档集整体成熟度高于典型"人读"文档（FR 编号化、判据 SSOT、不变式可机检、lint 硬约束均已存在），**最大风险不是单文件缺陷而是三处跨文件契约缺口**：①"每页怎么渲染"的组件级契约存在（DS §8 34 项），但"表格/图表/弹窗"等六类高自由度元素的**具体组件变体与状态矩阵**未指名，AI 会在这些位置自由发挥；②数据流协议 SSO​T 在 ARCHITECTURE §9 有（SSE/轮询），但**每页用哪个**未逐页声明，AI 会随机选轮询或 SSE；③状态机双枚举（PRD FR-1.6 大写七态 vs ARCH §4.1 小写九态）未声明映射，AI 两处各取一套必产分歧。历史建议修改顺序：先修**跨文件冲突 B-1/B-2/B-3**（该审计未验证工时，不作工期承诺），再补 DS 状态覆盖矩阵（C 组最大工作量），最后落 AGENTS.md 与生成前置清单。

---

## B. 跨文件一致性冲突清单

| # | 冲突位置（文件A §X ↔ 文件B §Y） | 冲突内容 | 会导致 AI 出什么错 | 建议以哪份为准 | 优先级 |
|---|---|---|---|---|---|
| B-1 | PRD FR-1.6（L126）↔ ARCHITECTURE §4.1（L417） | Execution 状态机双枚举：PRD 大写 `CREATED→RUNNING→SUCCESS/FAILED/PAUSED/KILLED/WARNING`（7 态无 dispatching/scheduled）；ARCH 小写 `created→scheduled→dispatching→running→(paused)→success/failed/killed/warning`（9 态） | 两处各取一套生成枚举与状态迁移表；PAUSED 在 ARCH 写作 paused 且是"旁支"，PRD 是并列终态；前端 StatusBadge 状态色映射会随机对齐其中一套 | **ARCHITECTURE §4.1 为准**（更细、含调度中间态，服务端真实流转）；PRD FR-1.6 改为引用："状态机以 ARCHITECTURE §4.1 为 SSOT，此处仅列用户可感知态" | 【必改】 |
| B-2 | DESIGN-SYSTEM 全文 ↔ ARCHITECTURE §4.2 API 规范 | 列表筛选交互无契约：各页"筛选"仅列筛选字段（级别/状态/机柜/rail），未定义筛选语法（key-based？单选？多选？组合逻辑 AND/OR）、未定义 URL 同步（TanStack Admin 模式：search params ↔ 表格状态双向同步） | AI 每页各写一套筛选器组件；刷新丢筛选条件；"分享链接"无法复现视图；与 TanStack Table 的 columnFilters 生态脱节 | 新增 DS 全局契约一条（Kestra key-based 语法：逗号 OR、空格 AND + URL search params 同步），所有筛选字段引用该契约 | 【必改】 |
| B-3 | DESIGN-SYSTEM §4.2 ↔ ARCHITECTURE §4.1 | Machine 状态→StatusBadge 映射表缺失：状态机 11 态 + blocked(reason) 5 枚举存在，但 DS §4.1 状态系统表只覆盖 RUNNING/PASS/FAIL 等 9 类通用态，未逐态映射（如 `netverified`/`trust_established` 用哪个徽标？blocked 的锁图标已有但色值未定） | AI 在 Fleet 列表/抽屉头部/ProjectTimeline 三处对机器态自行配色配图标，"同状态同色"原则（DS §1 原则 2）被无意识地破坏 | DS §4.1 补"Machine 态 → 徽标"完整映射表（11 态 + blocked 5 原因全部入表） | 【必改】 |
| B-4 | ARCHITECTURE §1.2 路由清单 ↔ DESIGN-SYSTEM §3 导航树 | 路由前缀已统一（本轮 v2.3 已修），但 ARCH L1/L2 图仅列路径不含页面标题 ↔ DS §3 只有标题+路径：**无一处"路由→页面→组件→API"四列对照表** | AI 廚建页面时要在两文件间人工对齐；DS §5 线框无路由键，Storybook 用例命名与路由脱节 | 以 DS §3 为 SSOT，ARCH §1.2 图加一行注："路由/标题/组件的完整对照见 DS §3+§8，本图仅层示意" | 【建议】 |
| B-5 | IMPLEMENTATION-PLAN WP-1.8 ↔ DESIGN-SYSTEM §8 | WP-1.8 交付"核心组件"列出的组件名（DataTable/StatusBadge/…）与 DS §8 清单 34 项的命名一致，但 **WP 未声明"组件实现以 DS §8 哪一行为规格"**（如 DataTable 是否含视图保存列？StatusBadge 是否含 MAINTAINED 态？） | AI 实现 DataTable 时可能漏做"视图保存"（v1 有 v2.3 导航重构后未重申），Storybook 验收 E-3 数组件数时口径不一 | PLAN 每个含组件交付的 WP 行加"规格=DS §8 组件名"引用 | 【建议】 |
| B-6 | ACCEPTANCE U-5（组件三态全过）↔ DESIGN-SYSTEM §9 条 8 | "三态"仅写"加载/空/错误"三项；运维特有态（骨架屏首载/超时/无权限/断连重连中/部分成功/BMC 灰度降级）未入三态定义 → U-5 验收时"三态"口径只查三项，AI 也只实现三项 | 实测场景：BMC 失联页无降级视图（本轮 §9 条 13 已补禁令但 U-5 判据未同步）→ 验收过但实际缺口 | DS §9 条 8 扩为"状态矩阵 N 态"（见 C-2），U-5 引用之 | 【必改】 |
| B-7 | PRD FR-13.4 agentless 快照 ↔ DS §4.12 抽屉监控标签 | PRD 说"面板数据带快照时间戳+降级徽标"；DS 只在 §4.12 画了字样，未定义**快照过期着色**（多旧算 stale？aging 着色与 FR-3.5 freshness 的 fresh/aging/stale 阈值是否同源？） | AI 自定 stale 阈值；或复用 FR-3.5 的 freshness 但两处定义不同步 | FR-3.5 freshness 阈值为 SSOT（参数目录键 `mon.snapshot_stale_after`），DS 引用不另设 | 【建议】 |

---

## C. 逐文件问题清单

### C-1 ARCHITECTURE.md

| 位置 | 问题描述 | 会导致 AI 出什么错 | 修改建议 | 优先级 |
|---|---|---|---|---|
| §1.2 L1/L2 图 | 路由清单更新了但**每页数据流协议未标注**（该页走 SSE 还是 5s 轮询？聚合粒度？） | DS §9 条 1 写"默认 5s 用户可调"是全局值，但万卡矩阵页 NFR-3 首屏 ≤2s 明显不能 5s 全量轮询——AI 面对矩阵页会困惑选哪个 | DS §3 导航树每页面行尾标注协议标记：`[SSE]`/`[poll 5s]`/`[poll 60s]`/`[on-demand]`；矩阵/网口面板/执行详情=SSE，台账类=按需，列表=5s | 【必改】 |
| §4.2 端点表 | 端点表 60+ 行无**页面消费映射**（哪个页面调哪些端点） | 前端页面数据源自由拼装；或重复请求已聚合过的数据（违背 §10 IA 自检 12"同 API 聚合源"） | 增"页面→端点"消费列或独立对照表（可放 DS §3 各页面行内） | 【建议】 |
| 详情页结构 | Kestra 模式（详情页多维 Tab）已有抽屉五标签，但**Tab 间跳转与返回路径未定义**：从告警中心点工单→从工单点端口→从端口点拓扑，链路深了 3 层后"返回"行为未定义（逐层 Esc？面包屑？） | AI 每处跳转各写返回逻辑；用户丢失上下文 | 全局契约：抽屉内跳转=替换当前抽屉内容（栈深 1，Esc 统一回列表）；页面级跳转=面包屑（DS §7 补一条） | 【必改】 |
| 数据流断连降级 | ADR-02 说断线指数退避，但**前端呈现**的断连态未定义（SSE 断了 UI 显示什么？部分数据保留还是清空？） | SSE 断线时 AI 可能整页 loading 卡死（DS §9 条 3 只写"重连+补齐"后端行为） | DS 补"SSE 断连前端契约"：顶部全局黄条"实时连接中断，显示最后数据 HH:MM"+数据灰阶 50%+自动重连，**不清空已有数据**（Beszel 模式） | 【必改】 |
| §5 流程时序 | 五张时序图无**用户操作入口标注**（DE 从哪个页面触发哪一步） | 编码时流程与页面脱节：S1–S9 自动执行但"档位选择"入口在哪页未写 | 每流程图首行加"入口：/deploy/nccl [发起测试]"样式标注（部分已有，补齐 5.3–5.6） | 【可选】 |

### C-2 DESIGN-SYSTEM.md

| 位置 | 问题描述 | 会导致 AI 出什么错 | 修改建议 | 优先级 |
|---|---|---|---|---|
| §2 设计令牌 | **YAML 结构化缺失**（DESIGN.md 规范双层结构）：CSS 变量是代码块但**间距/圆角/阴影/字阶未给 YAML/JSON 机器块**，且无"组件级 token 消费表"（哪个组件用哪个 token） | AI 写组件时 token 名靠猜（`--space-4` 还是 `spacing-md`？）；跨会话不一致 | §2 末追加一个 YAML 块（tokens 全集）+ 各组件规范行内引用 token 名（现有组件描述多为散文） | 【必改】 |
| §4.2 表格 | ~~v2.3 导航重构时丢失行高~~ **复核修正：行高 36px/28px 规格仍在（L235），审计初判有误**——但变体与视觉规则确缺失：无变体清单（dense/matrix/inline-edit）、无 hover/斑马纹/边框规则、无筛选语法（key-based OR/AND） | AI 自由决定表格视觉：hover 色随机、斑马纹随机开、筛选器各页异构 | §4.2 已按 D 组段落补全（四变体+视觉规则+URL 同步筛选）——本条已闭环 | 【必改→已修】 |
| §4.2 表格 | 六类高自由度元素之**表格**未指名组件来源与变体（TanStack Table v8 + shadcn 风格？无边框？斑马纹？hover 行高亮？） | 表格是全平台出现最多的元素，每页自由发挥 = 视觉不一致 | §4.2 补：DataTable 变体清单（list 默认/dense 紧凑/inline 编辑/矩阵 StatusMatrix 独立组件），行 hover=--color-bg-muted，斑马纹=off，边框=行分隔 1px --color-border | 【必改】 |
| §4 状态徽章 | **状态矩阵缺口**：default/hover/active/disabled/focus 五交互态全文档未系统定义（仅色值）；空态/骨架屏/错误态/超时态/无权限态五全局态分散（§9 条 8 三态、§4.9 toast） | 每组件交互态随机实现（focus 环有的有有的无）；U-5 验收口径含糊 | 新增 §4.0 全局状态矩阵表：行=状态（default/hover/active/focus/disabled + 首载骨架/空/错/超时/无权限/断连/部分成功/stale），列=视觉规则（token+组件），**每组件必须声明继承哪些** | 【必改】 |
| §4 图表 | **图表契约缺失**（六类高自由度之二）：NFR/报告页用 ECharts 还是 Recharts？DS §2.2 只写 ECharts 5 进 ARCH 选型表——但 ARCH §2.2 表里也有 ECharts 5，未冲突；缺的是**图表类型→页面映射**（busbw 分布=bar？趋势=line？热力=heatmap？）+ 主题变量绑定 | AI 可能引 Recharts（shadcn 生态惯性）与既定 ECharts 冲突；图表配色脱离 token | §2 末补"图表契约"小节：统一 ECharts 5；图表色板=rail 色板+状态色；图表类型映射表（Gantt/KPI spark/热力矩阵/分布直方各归页面）；dark 主题经 CSS 变量注入 | 【必改】 |
| §4 表单/弹窗/Tabs | **表单/弹窗/Tab 三类无组件变体规格**：表单=React Hook Form+zod（ARCH 选型有）但布局规则（label 位置/错误展示/必填标记）未定；弹窗=Modal 与 Drawer 边界已有（§11 U-7）但 Modal 内部结构未定；Tabs=抽屉五标签有了但页面级 Tabs 样式未定 | 表单错误提示位置随机（下/右/tooltip）；Modal 结构各页不同 | 各补 3–5 行规格：label 上置 12px、错误红字 helper 紧贴字段下、必填 *；Modal=标题+内容+右下动作区（危险左置）；Tabs=下划线式 active 用 --color-primary-600 | 【必改】 |
| 全文 | 模糊词扫描结果：**"现代化/简洁/美观/高级感/合理间距" 0 命中**（v2.1 已清理过）——此项达标，无需修改 | — | 保持 | ✅ |
| §7 快捷键 | 无障碍有对比度/键盘，但**44px 触控目标**（taste-as-code）与现有 28px 紧凑行高冲突未声明优先级 | AI 在 1280 断点下无法两全 | §7 补：触控目标 ≥44px 仅约束**操作类控件**（按钮/开关/复选框），数据密度类元素（行高/格宽）豁免 | 【建议】 |
| §9 条 13 | 反模式禁令已补（v2.3）但**无 lint 落地声明**（禁 transition:all 等如何机检？） | 禁令停留在文字，CI 无对应规则 → AI 违禁不报错 | 条 13 末注明：全部条目配 eslint/stylelint 规则（`declaration-property-value-disallowed-list` for transition:all 等），WP-1.1 lint 管线落位 | 【建议】 |

### C-3 IMPLEMENTATION-PLAN.md

| 位置 | 问题描述 | 会导致 AI 出什么错 | 修改建议 | 优先级 |
|---|---|---|---|---|
| WP 粒度总体 | **粒度总体合格**（每 WP 有交付物/前置/依赖），但个别 WP 是"巨型步骤"：WP-4.7"项目执行与双平面"含 7 项子交付（Project 实体/中间件/2 组件/导航重构/E2E/数据迁移）——单 WP 一次交付无法机检中间态 | AI 一次 PR 塞 7 件事，失败难定位；阶段判据只验终态 | WP-4.7 拆为 4.7a（Project 实体+中间件）/4.7b（ProjectSwitcher+Timeline 组件）/4.7c（导航重构+路由迁移）/4.7d（E2E 断言+数据集）——各自 DoD 可独立判定 | 【必改】 |
| WP-2.12 | 拓扑 WP 合格但**mock nvidia-smi 规格文件未指名产出物路径**（"mock nvidia-smi topo 输出为 sim 行为规格"——规格存哪？yaml？json fixture？） | AI 自选格式；后续 WP-3.13 场景矩阵消费该规格时对不上 | WP-2.12 交付物列加：`sim/fixtures/nvidia-smi-topo/*.json`（结构化 fixture，含四类异常注入模板） | 【建议】 |
| WP-5.1 | 性能达标 WP 只列 P-1~P10 引用，**无执行方式**（压测脚本怎么跑？sim 起多少容器？断言输出到哪？） | AI 到 P5 才现写压测工具，前面 WP 的"性能哨兵"（§4 条 4）无落点 | WP-5.1 补：压测 runner=k6 或自研 Go harness【需确认】；每 P-x 项一命令行示例（如 `bdp-perf run --scenario fanout-10k --assert p95<300s`） | 【建议】 |
| §5.3 性能表 | 执行档已注明 staging，但**sim 10k 的资源声明缺失**（10k 容器需多少核/内存？降密度模式怎么开？） | staging 环境准备失败才发现跑不动 | §5.2 补资源预算：10k 容器模式 ≈【需确认】核/【需确认】GB；或声明降密度模式（1 容器 10 机）为 staging 默认 | 【建议】 |
| DoD §9 | DoD 六条合格（契约/测试/走查/标注/文档），但**缺"lint 硬约束通过"独立条**（藏在条 4 里与 Storybook 混列） | CI 阈值不明：lint 失败算 DoD 失败吗 | DoD 拆条 4 为：4a Storybook 三态+4b Playwright+4c lint 硬约束（ParamSelect/ReadinessGate/反模式 13 条）各自独立判定 | 【建议】 |

### C-4 ACCEPTANCE.md

| 位置 | 问题描述 | 会导致 AI 出什么错 | 修改建议 | 优先级 |
|---|---|---|---|---|
| U-5 | "组件清单全量 Storybook 三态全过"——**"三态"定义过窄**（见 B-6）：骨架屏/断连/BMC 灰度等 v2.3 新态不在"加载/空/错误"内 | 验收通过但状态覆盖不全；AI 实现也只做三态 | U-5 判据改为"组件按 DS §4.0 状态矩阵中声明的状态全过（矩阵化走查，非固定三项）" | 【必改】 |
| U-1 | "10 页抽样走查"——抽样页未指名，**随机抽样可能漏掉最难页**（万卡矩阵/BMC 失联/双平面切换） | 走查过但高风险页未覆盖 | U-1 附固定抽样页清单（含 /fleet 矩阵、/deploy/overview、/monitor/maintained、BIOS 批量页、NCCL 报告 5 必抽 + 5 随机） | 【建议】 |
| F 项可判定性抽检 | 抽检 20 条 F 项：**18 条可机检**（注入+断言写明），2 条弱判定：F-3.3"自定义视图可用"、F-12.4"diff 预览确认"——"可用/确认"无断言细节 | AI 编码可过但验收时扯皮 | F-3.3 补：断言=新建视图保存后另一账号可见且着色生效；F-12.4 补：断言=同步 diff 含 N 变更项且确认后才执行 | 【建议】 |
| 性能 NFR 验收 | P-3"60fps"**机检方式未定义**（Playwright trace？Chrome tracing？） | "60fps"无法自动判 → 人工裁量违背 D-7 | P-3 判据补：用 Playwright + CDP tracing 采样长任务帧率 p95 ≥55fps 视为达标（55 容差带对应 §10 ±5%） | 【建议】 |
| 无 H 项反向映射 | H-13/H-14 新增了但**对应 WP 未在 PLAN 标注**（WP-2.12/3.12 交付物应含"执行包脚本 H-13/H-14"） | 执行包脚本没人写 | PLAN WP-2.12/3.12 交付物列各补 "+ 执行包 #P2(H-13)/#P3(H-14) 脚本" | 【必改】 |

### C-5 REVIEW-FINDINGS.md

| 位置 | 问题描述 | 会导致 AI 出什么错 | 修改建议 | 优先级 |
|---|---|---|---|---|
| §5 修订清单 | **闭环状态缺列**：清单有修改点无"状态"列（v2.2 已修——但读者需自行对照四文件确认） | 后续迭代者不确定哪些已生效，可能重复修或漏修 | 清单表加"状态"列（30 项全部"已修 v2.2"）；Open 项加"回流位置"列（如 Open-7→PRD FR-6.8 已标注） | 【建议】 |
| 分类 | H/M/L 按影响分级但**无类别维度**（IA/视觉/性能/可访问性/状态覆盖） | AI 规避时无法按类检索 | 每条目已有 [类别] 标签（合格），仅需目录加类别索引 | 【可选】 |
| §7 v2.3 附录 | Open-7~9 已记录但**未排入 PLAN 步骤**（Open-7 说"H-8 回填时校准"算排期；Open-8 说"P3 WP-3.12 落地"算排期；Open-9 无归属） | Open-9 悬空 | Open-9 加归属："文档变更评审（交付谈判时点），非工程任务" | 【可选】 |

---

## D. 历史增补建议（已失效，禁止直接粘贴）

```
【文件】PRD.md
【位置】FR-1.6 生命周期管理
【原文】状态机（CREATED→RUNNING→SUCCESS/FAILED/PAUSED/KILLED/WARNING）、Kill/Pause/Resume。
【替换为】状态机以 ARCHITECTURE §4.1 为 SSOT（created→scheduled→dispatching→running→(paused)→success|failed|killed|warning）；本条仅列用户可感知终态（成功/失败/暂停/终止）。Kill/Pause/Resume。
【理由】消除 B-1 双枚举冲突；AI 以前者为唯一实现源，本处仅保留产品语义。
```

```
【文件】ARCHITECTURE.md
【位置】§9 前后端协作约束（新增第 10 条）
【原文】（无）
【替换为】10. **页面数据流契约**：每个页面的实时数据协议在 DESIGN-SYSTEM §3 导航树逐页标注——[SSE]（状态矩阵/网口面板/执行详情/告警中心）、[poll 5s]（设备列表/工单池/测试批次，用户可调 5–60s）、[poll 60s]（CMDB/项目时间线聚合）、[on-demand]（抽屉标签/报告页/拓扑页，打开才拉）。SSE 断连前端契约：顶部全局黄条"实时连接中断，显示最后数据 HH:MM"+数据灰阶 50%+自动重连，不清空已有数据。列表筛选一律 URL search params 同步（刷新/分享可复现）。
【理由】B-2/C-1 数据流缺口：防 AI 每页随机选协议与筛选交互。
```

```
【文件】DESIGN-SYSTEM.md
【位置】§4.2 表格（ DataTable 规格恢复与补全）
【原文】万级行虚拟滚动；行高 36px（紧凑 28px 可切）；首列复选框；列固定/显隐/视图保存；行点击 → 右侧抽屉详情；空态插画 + 主 CTA。
（注：v2.3 导航重构后此规格已从 §4.2 丢失，仅剩一句"万级行虚拟滚动"在 §4.2 行内——先恢复再增强）
【替换为】基于 TanStack Table v8 自研 DataTable，四变体：list（默认）/dense（紧凑）/matrix（StatusMatrix 独立组件，8px/格）/inline-edit。行高 36px 默认、28px 紧凑模式（用户偏好持久化 localStorage）。行 hover=--color-bg-muted；无斑马纹；行分隔 1px --color-border。首列复选框 28×28px；列固定左（主机名）/右（操作）；列显隐+视图保存（个人/团队两级）。行点击开 ServerDetailDrawer；空态=插画+主 CTA+三步引导（§11 U-1）。筛选=URL search params 同步（ARCH §9 条 10 契约）。
【理由】C-2 表格规格丢失与高自由度缺口——表格是全平台密度最高元素，规格缺失必然视觉分裂。
```

```
【文件】DESIGN-SYSTEM.md
【位置】§4.0（新增小节，置于 §4.1 前）
【原文】（无）
【替换为】### 4.0 全局状态矩阵（每组件必须声明继承）

交互五态（全组件强制）：default / hover（--color-bg-muted 或 --color-primary-600 按组件类型）/ active（--color-primary-700）/ focus（outline 2px --color-focus，offset 2px）/ disabled（opacity .45 + cursor not-allowed + 不降对比度）。

全局九态（按组件声明）：首载骨架（Skeleton，--color-bg-muted 块，禁 spinner 全屏）/ 空 / 错误（重试按钮+trace_id）/ 超时（>30s 转后台任务中心）/ 无权限（置灰 tooltip，不隐藏）/ SSE 断连（黄条+灰阶 50%+时间戳，不清数据）/ 部分成功（WARNING 琥珀+成功失败分列）/ 数据陈旧 stale（快照时间戳+aging 琥珀角标，阈值=FR-3.5 freshness SSOT）/ BMC 失联（整块灰度降级+最后数据时间戳，§9 条 13 禁令）。

各组件在 §8 清单行内标注继承集（如 DataTable：骨架/空/错/断连/stale；ServerLampPanel：断连=灰度降级专项）。
【理由】B-6/C-2 状态矩阵缺口：三态定义过窄是 U-5 验收与实现的双向漏洞。
```

```
【文件】DESIGN-SYSTEM.md
【位置】§2 末（新增 2.5 结构化 token 块）
【原文】（无）
【替换为】### 2.5 设计令牌机器块（DESIGN.md 规范，YAML 层）

```yaml
tokens:
  color:
    primary: {50:#faf5ff,500:#a855f7,600:#9333ea,700:#7e22ce}
    accent_pink: {base:#ec4899}
    status: {success:#16a34a,warning:#f59e0b,danger:#dc2626,info:#0284c7,neutral:#94a3b8,waived:#7c3aed}
    bg: {base:#ffffff,subtle:#f8fafc,muted:#f1f5f9}
    text: {primary:#0f172a,secondary:#475569,muted:#94a3b8}
    border: {default:#e2e8f0,strong:#cbd5e1}
  font:
    family: {sans:[Inter,"PingFang SC",system-ui],mono:[JetBrains Mono,Consolas]}
    size: {xs:11,sm:12,base:13,lg:15,xl:18,2xl:24}
  space: {1:4,2:8,3:12,4:16,5:20,6:24,8:32}
  radius: {sm:6,md:10,lg:14}
  shadow: {1:"0 1px 2px rgba(15,23,42,.06)",2:"0 4px 12px rgba(15,23,42,.08)",3:"0 8px 24px rgba(15,23,42,.12)"}
  layout: {topbar:56,sidebar:240,sidebar_collapsed:56,drawer:720,drawer_max:960,row_height:36,row_height_dense:28}
  touch_target: 44  # 仅操作类控件；数据密度元素豁免
```
（dark 主题同名键覆盖值见 §2.1 dark 块，一一对应不增删键；组件规范中色值一律引用本块键名，禁字面量。）
【理由】C-2 token 机器可读性：散文→YAML，跨会话一致性锚点。
```

```
【文件】IMPLEMENTATION-PLAN.md
【位置】WP-4.7（拆分）
【原文】| WP-4.7 | **项目执行与双平面（v2.3，FR-17，ADR-17）**：Project 实体/状态机 + ProjectContext SSOT（session+store 同源）+ api-gateway 项目中间件（数据域过滤 + 项目级 RBAC）+ ProjectSwitcher/ProjectTimeline 组件 + 导航双平面重构（路由 /p/<projectId>/ 前缀段）+ 双入口等价 E2E 断言（不变式 [I-10]）+ 多项目并行数据集（b300 两期）+ Execution/Issue/Alert/PortModel project_id 迁移 | project 域 + 前端导航重构 | WP-1.6, WP-4.5 |
【替换为】
| WP-4.7a | Project 实体/状态机 + ProjectContext SSOT（session+store 同源）+ api-gateway 项目中间件（数据域过滤+项目级 RBAC） | project 域后端 | WP-1.6 |
| WP-4.7b | ProjectSwitcher + ProjectTimeline 组件（DS §4.18/4.19 规格） | 2 组件 | WP-4.7a, WP-1.8 |
| WP-4.7c | 导航双平面重构（路由 /p/<projectId>/ 前缀段迁移+DS §3 全页面协议标注落地） | 前端路由重构 | WP-4.7b |
| WP-4.7d | 双入口等价 E2E 断言（不变式 [I-10]）+ 数据域越界注入测试 + b300 两期多项目数据集 + Execution/Issue/Alert/PortModel project_id 迁移 | 验证套件+迁移 | WP-4.7c |
【理由】C-3 巨型步骤拆分：四段各有独立 DoD，失败可定位；4.7d 的迁移可回滚验证。
```

```
【文件】ACCEPTANCE.md
【位置】U-5 判据
【原文】| U-5 | 三态实现 | 组件清单全量 Storybook 三态全过 | P0 |
【替换为】| U-5 | 状态矩阵实现 | 组件按 DS §4.0 状态矩阵声明的继承集全过（Storybook 每 states 一 story；断言含断连灰度降级/stale 时间戳/BMC 失联非红点三项专项） | P0 |
【理由】B-6：三态过窄→矩阵化，与 DS §4.0 闭环。
```

---

## E. 配套文件

### E-1 历史 AGENTS.md 范例（非当前行为指令）

```markdown
# AGENTS.md — B300/GB300 交付平台 AI 编码代理守则

> 五份设计文档（docs/）是唯一需求依据；本文件是编码行为契约。冲突时以设计文档为准并停下上报（PRD §1.4 规范 4）。

## 技术栈（不得更换）
- 后端：Go 1.23+ / gin / ent / Redis Streams；模块化单体 19 服务
- 前端：React 19 + TypeScript 5 + Vite 6 + TanStack Router/Query/Table v8 + Zustand + ECharts 5 + React Flow + Monaco
- UI 基建：自研组件层（CSS 变量令牌）+ Radix Primitives；**不引入新组件库**

## 组件来源约定
- 表格 → 自研 DataTable（TanStack Table 内核，DS §4.2 四变体）
- 徽章/灯 → StatusBadge / ServerLampPanel（DS §4.1/4.17）
- 下拉选版本 → ParamSelect（禁自由文本，lint 强制）
- 跨系统按钮 → ReadinessGate 包裹（lint 强制）
- 图表 → ECharts 5（禁 Recharts/Chart.js）；色板引用 DS §2.5 token 键
- 弹窗 → 危险确认 ConfirmDanger；信息展示一律 Drawer；禁止套娃弹层

## 命名规范
- 路由 `/p/<projectId>/...`（部署平面）与全局路由并存（DS §3）
- 权限点 `<模块>:<动作>`（bmc:control / mon:suppress ...）
- 状态枚举以 ARCHITECTURE §4.1 为 SSOT（小写）；错误体 {code,message,details[],trace_id}
- i18n 键层级 `<page>.<element>.<state>`；文案禁硬编码

## 禁止清单（违反即 CI 失败）
- 禁 transition: all；禁动画 top/left/width/height（用 transform/opacity）
- 禁离线/断连/降级只用红点（必须灰度降级+最后数据时间戳）
- 禁占位式界面（三 KPI 卡+空图表骨架）
- 禁版本号自由文本输入；禁未包 ReadinessGate 的跨系统触发按钮
- 禁批量操作混入 ServerDetailDrawer 单机流
- 禁绕过 ProjectContext 直取项目数据；禁硬编码真实环境地址（ADR-14）
- 禁新造状态色（只用 DS §2.5 token 键）；禁在组件内写死色值

## 开发方式
- mock-first：契约生成 mock server；外部依赖 mock/real 同 PR（ADR-14）
- 每 PR 标注 FR/WP 编号；歧义=停下上报（IMPLEMENTATION-PLAN §11）
- 离线红线：开发/CI 不连真实环境；真机走执行包回填制
```

### E-2 token 合并块

见上文 D 组 DESIGN-SYSTEM §2.5 增补段（YAML 已可直接粘贴合并；dark 键一一对应）。

---

## F. 生成前置清单（每轮让 AI 写页面前核对）

1. 该页在 DS §3 的路由与协议标注（[SSE]/[poll]/[on-demand]）确认了吗？
2. 该页消费的 API 端点在 ARCH §4.2 表中存在吗？聚合源唯一吗？
3. 页面内每个 UI 元素映射到 DS §8 哪个组件？高自由度六类（表格/图表/表单/弹窗/徽章/Tabs）是否都有规格可引？
4. 该页所有状态在 DS §4.0 矩阵的继承集里声明了吗（含断连/stale/无权限）？
5. 用到的色值/间距是 DS §2.5 token 键引用（非字面量）吗？
6. 版本类字段是 ParamSelect？跨系统按钮包 ReadinessGate 了吗？
7. 该页属部署平面还是运维平面？ProjectContext 过滤行为对吗（双入口等价）？
8. 列表筛选是 URL search params 同步的吗？抽屉跳转保持栈深 1 吗？
9. 纯图标按钮有 aria-label？触控目标 ≥44px（操作类）？
10. 本页对应验收项（ACC F/U 编号）能在 DoD 里写出断言吗——写不出=规格缺口，停下上报。

---

**审计结论**：文档集基础远超一般项目（编号化/SSOT/不变式/机检判据四支柱已立），本轮 25 项发现中【必改】10 项集中三类——状态枚举统一（B-1）、状态矩阵补全（B-3/B-6/C-2）、交互契约显式化（B-2/C-1）。落地顺序建议：D 组段落粘贴（约 6 处）→ E-1 AGENTS.md 建档 → F 清单入 CI 模板。全部为增量补强，零推翻重写。
