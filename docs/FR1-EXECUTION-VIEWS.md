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

本轮代码转为本地开发：2026-10-24 用户要求停止远端上传/验证，后续由用户安排 Codex 验证。转本地前，隔离分支的 Journal/Flow 单测、相关 Go vet 和 TypeScript typecheck 有通过记录；最后的 Vite/3104 集成会话未返回结果，不记通过。本地整批代码、依赖锁和生成类型还需后续验证，当前不得标作完整 FR-1/浏览器/硬件验收通过。未在运行库执行任何本增量迁移、备份或重置。

## 暂停交接（2026-10-24）

用户要求逐步暂停。执行视图代码、共享接线、正式契约、生成类型与依赖锁已保存为本地 WIP；未在本地运行构建或集成测试，不能标验收通过。没有继续访问远端或部署。

下一块重跑功能仅有独立草稿，未接入应用：`/private/var/root/keel-local/fr1-rerun-draft`（原 scratch `/private/tmp/keel-fr1-rerun` 同时保留）。其中 rerun.go 还依赖未增加的 Service.create 私有入口；types.go 的来源快照字段、Create 事务接线、HTTP注册、API契约/生成类型与 RerunDialog 的页面接线尚未完成，不能复制进编译目录后直接声称可用。


## 主目录整合与验证（2026-10-24）

从主目录 `3b0385c` 接续已保存 `4805e7f`；比较历史工作树后确认后端仅格式化差异，采用已保存的完整前端成果。所有修改统一在 `/opt/keel`，保留昨天独立工作树和未接入重跑草稿。既有Flow/网络接口与新交付导出导航并存，无数据库迁移。

实际通过：execution/httpapi race、相关Go vet、前端契约生成/TypeScript/Vite构建。最终真实API/PostgreSQL/worker/浏览器证据 `/tmp/keel-views-n_wiaccz`，覆盖500目标暂停样本的概览/服务端过滤/虚拟矩阵、五Tab懒加载/URL/尝试详情、2目标真实worker运行与4条回执、冻结变量/输出审计、跨执行/项目隔离、丢回执unknown保持未完成、SSE初始快照/游标失效重同步/登出撤权关闭。仅连通性样本，非2000/10000吞吐验收；无真实Agent/SSH/stdout或文件制品。隔离进程/库/临时凭据已清理，未部署3000。
