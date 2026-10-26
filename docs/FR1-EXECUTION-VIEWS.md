# FR-1 执行详情与可恢复事件流

基准：PRD FR-1.5/1.6/1.7、DESIGN-SYSTEM §5.12/§4.5、实施计划 WP-1.8。此模块补执行详情的可用链路，复用已发布的 Flow/Execution 内核，不新增任务调度器，也不表示 FR-1 全部完成。

## 用户链路

项目执行列表仍为 `/p/{projectId}/executions`，选择执行后 `?execution={id}&tab=...` 进入五个视图：

- **概览**：一致性首开快照、按目标去重的完成/成功/失败/未知数量、任务级 Gantt。时间条是全部目标和重试的实际区间，不是各机平均耗时；缺完成证据显示未知。
- **目标**：从冻结目标集合读取矩阵。服务端按名称/ID及最新尝试状态过滤、分页，TanStack Virtual 仅渲染可见行；8/16/24/32px 网格缩放。格子打开懒加载的尝试抽屉，再进入该次尝试的日志筛选。
- **日志**：持久化生命周期事件的实时 SSE。明确标注此数据不是设备 stdout/stderr。级别与尝试筛选入 URL；初次取最近200条，界面最多保留500条。
- **输出**：冻结参数、已持久化 Runner 回执与结构化输出；JSON 回执下载单独记审计。目前没有文件制品，界面不会把回执包装成已生成文件。
- **关联**：冻结 FlowRevision/参数/目标摘要，已存在的 Kea Deployment/PushJob 引用；未接入的工单和上游 subflow 关联明确标注。

Overview 首开；Targets/Logs/Outputs/关联按 Tab 挂载查询。仅运行中的活动 Overview/Targets/Logs 订阅事件，切换视图、执行或项目会关闭原订阅。执行详情按 executionId 设置 React key，不能把打开的控制确认或抽屉重定向到另一个执行。状态控制沿用原 Pause/Resume/Cancel API、当前版本比较和权限；冲突要求刷新，不自动重试危险动作。

## 数据与接口

`internal/execution/views.go` 只聚合既有执行、冻结快照、attempt、receipt/outbox 数据，不改执行状态。Overview/Matrix/Journal/Outputs/Relations 使用 PostgreSQL repeatable-read 只读事务；JSON回执导出单独使用当前会话锁定与审计事务。

| `/api/v1/projects/{id}/executions/{executionId}` 下的路径 | 作用 |
|---|---|
| GET `/overview` | 目标 KPI、任务区间和同一快照下的 event_cursor |
| GET `/matrix?page=&page_size=&state=&q=` | 最新尝试矩阵；每页最多500目标，缺尝试为 not_scheduled |
| GET `/attempts/{attemptId}` | 作用域绑定的冻结任务、目标、尝试与可选回执 |
| GET `/journal?after=` | 最近200条或游标后最多200条；过期/外执行/未来游标返回 resync_required |
| GET `/stream?after=` | SSE snapshot/execution/resync/rotate/access_revoked，Last-Event-ID 优先 |
| GET `/outputs?page=&page_size=` | 回执分页、冻结变量，artifacts_supported=false |
| GET `/outputs/{attemptId}/download` | 同执行回执 JSON 下载，审计 action=execution.output.export |
| GET `/relations` | 冻结 Flow、参数、摘要与实际业务引用 |

所有接口重新认证并校验项目成员范围。attempt/receipt 必须属于 URL 指定执行；外项目或串用其他执行的 attempt 返回404。下载审计与当前权限复核同事务，不接受任意文件路径。

SSE 每条连接在25秒内轮换，低于现有 API 的30秒写超时；游标属于当前 execution，不能把另一执行的全局 sequence 当合法续读位置。每批/轮询重新认证并复核项目成员；登出、撤销或改权限后发 access_revoked 并关闭。断线保留已有数据并有限退避重连；游标不可续读时重取完整视图快照，不推测中间事件。生命周期事件不可改写，客户端按事件 ID 去重、sequence 排序。

## 修改入口

| 范围 | 文件 |
|---|---|
| 只读聚合、游标、回执导出 | `internal/execution/views.go` |
| HTTP/SSE、25秒轮换与当前授权 | `internal/httpapi/execution_views.go` |
| 页面容器、控制确认、日志/输出/关联 | `web/src/features/execution/ExecutionWorkspace.tsx` |
| 虚拟矩阵 | `web/src/features/execution/ExecutionTargets.tsx` |
| 类型化请求、事件连接/取消、URL解析 | `web/src/lib/execution-views.ts` |
| 既有执行页/参数接线 | `ExecutionsPage.tsx`、`lib/execution.ts` |
| 正式契约 | `api/execution.openapi.yaml`；生成 `generated/execution.d.ts` |

使用既有稳定工作台和控件边界，不等待或开展组件库迁移。新增依赖仅为官方 npm registry 核对并精确锁定的 `@tanstack/react-virtual 3.14.22`。本增量没有数据库迁移，不执行运行库备份/迁移/重置。

## 验证与边界

验证脚本：`scripts/check-execution-views-integration.py`，只使用 disposable 标签 PostgreSQL、独立 `keel_test_views_*` 数据库、API3104与真实 Worker/浏览器。500目标只创建并暂停，用于快照/服务端过滤/虚拟矩阵；实际运行2目标验证连通与回执，另1目标注入丢回执验证 unknown 不变成完成。此规模不是吞吐 SLO，不能据此声称10,000目标×30秒≤5分钟。

当前执行内核的通用 sim 目标上限不因 UI 虚拟化扩大；真实 Agent/SSH、stdout/stderr、文件制品、subflow/工单关联、90天保留/清理、规模索引与压测仍需后续开发。既有 Kea/fabric Flow 的受控发布/运行入口保持独立，查看详情不赋予生产网络执行权限。软件接口验证不等同真机 PoC。

2026-10-24 的本地暂存阶段已在唯一主目录接续验证：HANDOFF §22/§23记录实际PG/API/worker/浏览器、五Tab/矩阵/回执/SSE/撤权、共享组件及前端完整构建结果，证据分别为/tmp/keel-views-n_wiaccz和/tmp/keel-views-jrx3w19y。最初未返回结果的会话不计通过，后续通过来自这些独立复核；不再把初始待验证状态当当前进度。

2026-10-25执行结算改为只读冻结控制字段、Flow定义和每目标/任务最新attempt，保留租约、fencing与显式unknown对账。默认2,000目标软件复核及并行构建负载复核均通过，资源、用时和12组控制/回执/重试/权限/准入证据见HANDOFF §48/§49；10ms模拟任务不能代替10,000目标×30s的性能档。运行版本、发布与GitHub实际CI以最新HANDOFF为准，真实传输、设备stdout/文件制品和完整FR-1/P1仍待后续验收。

## 暂停交接（2026-10-24）

用户要求逐步暂停。执行视图代码、共享接线、正式契约、生成类型与依赖锁已保存为本地 WIP；未在本地运行构建或集成测试，不能标验收通过。没有继续访问远端或部署。

下一块重跑功能仅有独立草稿，未接入应用：`/private/var/root/keel-local/fr1-rerun-draft`（原 scratch `/private/tmp/keel-fr1-rerun` 同时保留）。其中 rerun.go 还依赖未增加的 Service.create 私有入口；types.go 的来源快照字段、Create 事务接线、HTTP注册、API契约/生成类型与 RerunDialog 的页面接线尚未完成，不能复制进编译目录后直接声称可用。


## 主目录整合与验证（2026-10-24）

从主目录 `3b0385c` 接续已保存 `4805e7f`；比较历史工作树后确认后端仅格式化差异，采用已保存的完整前端成果。所有修改统一在 `/opt/keel`，保留昨天独立工作树和未接入重跑草稿。既有Flow/网络接口与新交付导出导航并存，无数据库迁移。

实际通过：execution/httpapi race、相关Go vet、前端契约生成/TypeScript/Vite构建。最终真实API/PostgreSQL/worker/浏览器证据 `/tmp/keel-views-n_wiaccz`，覆盖500目标暂停样本的概览/服务端过滤/虚拟矩阵、五Tab懒加载/URL/尝试详情、2目标真实worker运行与4条回执、冻结变量/输出审计、跨执行/项目隔离、丢回执unknown保持未完成、SSE初始快照/游标失效重同步/登出撤权关闭。仅连通性样本，非2000/10000吞吐验收；无真实Agent/SSH/stdout或文件制品。隔离进程/库/临时凭据已清理，未部署3000。


## 主目录接续：FR-1.4 定向重跑（2026-10-24）

按最新 IMPLEMENTATION-PLAN 步骤 A3／WP-1.4 接续已保存的独立草稿，在 `/opt/keel` 完成事务、正式契约、生成类型和共享组件页面接线；上文暂停记录描述的是当时事实。

- `POST /projects/{id}/executions/{executionId}/rerun/preview` 返回失败目标或显式所选目标的候选集合、排除原因、来源版本及集合摘要；只读候选计算，不创建执行。
- `POST .../rerun` 必须提交预览摘要、来源状态版本、原因及幂等键。复用 Create 的当前会话/项目/角色、参数目录准入、目标登记与执行创建事务；复核来源目标版本、Flow 摘要和冻结参数。新执行复用原 FlowVersion 与输入，记录 `snapshot.rerun_source`、outbox 与审计；原执行/尝试不改写。同请求并发仅创建一个新执行，错误复用键拒绝。
- 只接受已经终态的通用 `sim` Flow；unknown、活动尝试、缺任务证据或成功/失败缺 Runner 回执的目标不能重跑。显式选择有排除项时整次拒绝，失败模式只取已明确失败且证据完整的目标。Kea/fabric 和未支持的 NCCL 等任务不能通过通用重跑绕过业务准入。
- 页面沿用 shadcn/ui 和 ActionDialog。执行详情提供“仅重跑失败目标”，目标尝试抽屉提供“重跑此目标”；预览确切集合、参数和排除项，原因必填，提交成功跳转新执行；关联 Tab 可回到来源执行。

实际验证：相关 execution/httpapi Go race、vet、前端契约生成/组件边界/TypeScript/Vite、设计引用检查通过。真实 API/隔离 PostgreSQL/Worker/浏览器证据 `/tmp/keel-views-y5o63831`：单目标与失败集合、来源不变、并发幂等/错误复用、陈旧版本/摘要、空集合/活动来源/外目标/跨项目/Viewer/unknown 拒绝、两个 UI 入口及来源链接；既有五 Tab、500目标暂停渲染样本、2目标实际运行、回执导出和 SSE 撤权回归通过。首轮新增脚本误从 overview 摘要读取 snapshot，修正为正式 relations 接口后通过；首轮不计通过。

无迁移、部署或真机操作。通用能力仍限已实现的模拟任务；Agent/SSH、真实文件分发、rolling/分组与 NCCL GroupAttempt、完整 FR-1 和 P1 退出条件仍未完成。Go 检查沿用既有 overlay 排除原026/fabric_precheck草稿，未修改这些文件。


## 执行事实报告导出（2026-10-26，源码未验证）

执行中心增加「导出执行报告」，按当前项目成员/会话权限调用`GET /executions/{id}/report`，返回`execution-evidence-report/v1` JSON。同一repeatable-read事务复用原overview计算，固定Flow版本/hash、目标集合hash、冻结时间和当前执行/尝试状态，另列计划任务槽位、尚无尝试的槽位、匹配command/attempt/fence的历史终态回执数量及声明来源、unknown实际持有的资源数。报告完整仅表示全部计划槽位已终态，不表示成功或验收。

报告固定`hardware_acceptance:false`与`gate_evidence:false`；未开始、运行中、未知和历史来源不被改写为PASS。输出只含汇总和原授权分页matrix/outputs/journal/relations引用，不打包原始日志、脚本正文、凭据或全量目标回执。页面请求时才生成，无后台轮询，无状态变更/自动解锁。此增量提供报告聚合基础与导出，不声称通用`report.aggregate` Flow任务、门禁签署或完整证据归档已完成。相关PG计划槽位/未知隔离/来源与冻结身份/范围/正文不泄露回归源码已补，尚未运行。
