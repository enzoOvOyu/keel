# FR-1 Flow 工作区与版本闭环

基准：PRD v3.0.3 FR-1.1/1.5/1.6/1.7，DESIGN-SYSTEM §4.7、实施计划 WP-1.4/1.8。此文记录实现边界，不替代 FR-1 全量验收。

## 已接通的模块

`/p/{projectId}/flows` 复用共享工作台、项目 URL、角色和 Intel One Mono 字体。分页 Flow 列表、概览/源码/版本历史 Tab、版本比较选择和历史页码入 URL。源码与双版本比较使用本地 Monaco 编辑器和 DiffEditor，编辑器及 worker 懒加载，不访问 CDN。修改离开有确认。

- `internal/execution/flow_workbench.go`：纯 YAML 校验与拓扑次序、输入默认值预览、版本详情、结构化 diff、回滚入口。调用现有 `PublishInput.Parse/Definition.Validate/Parameters`，不另建 DSL 执行语义。
- `internal/execution/catalog.go`：版本发布复用原事务/当前权限/幂等/乐观锁；回滚调用同一写路径，复制旧内容创建新版本，并在同事务写 `flow.version.rollback` 审计。旧版本不可经 API 改写。幂等载荷绑定来源版本和原因，重放返回同一新版本，错误复用拒绝。
- `internal/httpapi/flows.go`：增加校验、版本详情、diff、回滚接口，均通过当前项目认证；写操作保留 CSRF/Origin 检查。
- `web/src/features/flows`：源码编辑、版本浏览与 Monaco 差异、二次确认回滚和运行弹窗。运行弹窗按已发布版本的输入定义生成表单，显式勾选目标并展示数量，翻页不扩大选择。
- 实际运行仍调用已有 `/executions`，冻结确切 FlowRevision、输入和目标，复用 worker/outbox/attempt/资源租约/回执。无新队列、无第二套作业状态机。

通用发布/运行当前只支持已经实际实现的 `keel.sim/v1`、`sim.read/sim.write`。Kea/网络业务 Flow 在工作区只读；即使提交有效 sim YAML，也不能覆盖其现有版本，须走对应业务批准入口。普通 Flow 发布/回滚限当前项目 Admin/SRE（平台管理员同规），运行沿用 Admin/SRE/DE/DL。前端隐藏动作不代替服务端当前授权。

## 接口与修改入口

契约在 `api/execution.openapi.yaml`，前端 `generated/execution.d.ts` 由既有命令生成。

| 接口 | 作用 |
|---|---|
| POST `/projects/{id}/flows/validate` | 纯计算：单文档/字段/插件/DAG/参数检查，无版本写入、无派发 |
| GET `/projects/{id}/flows/{flowId}/versions/{versionId}` | 项目/Flow 绑定的不可变版本与操作者引用 |
| GET `/projects/{id}/flows/{flowId}/diff?before=&after=` | 任意两版的原文和结构变化；不能跨 Flow/项目比较 |
| POST `/projects/{id}/flows/{flowId}/rollback` | 必填来源版本、当前 expected_version 和原因；二次确认后生成新版本 |

既有 publish/list/create/control 接口保持兼容。新增 Flow 功能不需要迁移。界面分页边界沿用现有20行；版本比较可指定任意两个已知版本ID。

PoC 改设备传输应修改已有 Runner/适配器接口并补回执与 host-key/凭据契约，不能把 `sim` 标记替换成真实成功。新任务类型应先扩 DSL 的验证/版本契约，再实现对应 Runner；编辑器不得偷偷接受执行器不认识的字段。

## 检查与依赖

Monaco 固定 `0.57.0`；其 DOMPurify 依赖通过根目录精确 override 固定 `3.4.17`。安装时检查了官方 npm registry 当前版本及审计；`npm audit --omit=dev` 返回0漏洞。编辑器与 worker 的浏览器加载仍需用本次构建检查，不以安装成功替代验证。

定向单元测试覆盖真实解析器、DAG次序、别名/多文档/不支持插件/自依赖拒绝、输入枚举和结构/排序 diff。`scripts/check-flows-integration.py` 使用带 disposable 标签的独立 PostgreSQL 库与3104，启动真实 API/worker/浏览器；运行记录追加于下方。不使用运行库，不向真实设备发任务。

## FR-1 尚未闭环的范围

此增量补齐 Flow 工作区、版本/回滚和现有内核运行入口，不声明整个 FR-1 完成。剩余包括条件输入/变量表达式、subflow/failedNext/labels、cron/webhook/事件触发、完整内置任务类型、真实 Agent/gRPC/SSH 执行与凭据/host-key/受控信任初始化、媒体真实分发、参数目录兼容矩阵联动、目标分组/rolling、NCCL整组恢复、执行详情五Tab/Gantt/虚拟矩阵/真实流式日志/输出物、90天保留和定时清理。当前通用执行数量与并发上限沿用内核，不代表10,000×30s≤5min性能达标。

Monaco 当前提供 YAML 语法高亮、真实服务端严格校验与双版本比较；尚未提供任务类型/`media()`/`param()` 补全、行级服务端诊断映射、No-Code/拓扑编辑器和 Playground。概览是任务依赖列表，不宣称 React Flow DAG 画布已完成。

## 本轮验证记录

2026-10-23：定向 Go 测试 `go test ./internal/execution -run TestFlowWorkbench -count=1` 通过；前端生成契约、TypeScript 与 Vite 构建通过。最终真实 API/Worker/PostgreSQL/浏览器证据目录 `/tmp/keel-flows-bkvytiei`：严格校验、v1/v2/diff、回滚生成 v3、同请求幂等、陈旧版本与错误复用拒绝、原版本保留、Kea owned 流禁止通用修改/回滚、Viewer 只读、跨项目404、统一 worker 冻结版本/目标/参数并完成；浏览器真实 Monaco 编辑/发布 v4、DiffEditor、URL Tab、Intel 字体、显式目标运行均通过，无页面错误。隔离库/3104进程/临时凭据已清理。

前两次中断仅为 Playwright 对 Monaco 原生 EditContext 隐藏辅助 textbox 和本地化按钮标签的错误选择器；已改为渲染视图与作用域 submit 按钮，未放宽产品权限或业务断言。视觉复核后另补 Tab 样式与强制双栏 diff；最终组合前端构建由主代理再执行。原图本机副本 `/private/tmp/keel-flow-revisions.png`，最终通过图位于上述证据目录。代码尚未由本子任务提交/推送/部署；由主代理合并发布，不能将此记录当作3000运行版本证据。

## A4 参数目录接线

2026-10-24：Flow 工作区运行弹窗与执行列表共用 CatalogInputs/ParamSelect，版本类参数不再提供自由文本入口。实时检查和创建事务均消费同一目录/批准制品元数据；提交时实际核对固定对象字节并冻结定义/依赖/摘要/对象版本。详见 [CATALOG-ADMISSION](CATALOG-ADMISSION.md)。完整兼容矩阵治理、模板和真实目标准入仍未闭环。实际验证以 HANDOFF 最新记录为准。

## P1 无介质项目 KV（源码未验证）

WP-1.4/F-1.8、PRD FR-1.3/I-2：输入为已有项目授权、版本化Flow及原执行器/锁/回执；产物为`keel.control/v1`的`kv.get`/`kv.put`，使用平台项目元数据目标，不伪造设备或sim运行结果。没有介质、SSH/Agent连接或后续门禁前提。验收源码见`internal/execution/control_kv_test.go`，尚未执行。后续子流/gate.wait、安装/驱动/NCCL按各自WP及门禁前置接入，不以本次KV替代。

```yaml
api_version: keel.control/v1
inputs: {}
tasks:
  - id: remember
    type: kv.put
    retry: 0
    idempotent: false
    timeout_ms: 2000
    resources: [project]
    kv:
      key: delivery.note
      value: Ready for review
      expected_version: 0
  - id: inspect
    type: kv.get
    depends_on: [remember]
    retry: 0
    idempotent: true
    timeout_ms: 2000
    resources: [project]
    kv:
      key: delivery.note
```

通过原Flow发布API保存版本，再`POST /api/v1/projects/{id}/control-flow-executions`，body为`{"flow_version_id":"已发布版本ID"}`，提供当前会话/CSRF/Idempotency-Key；页面有“新建KV Flow”模板和显式范围确认。项目公开元数据跨Flow可读，不能存秘密、凭据、配置或硬件判据。每项目最多128键，单值≤4096字符且JSON编码≤8192字节，key为受限小写标识；不存在get明确失败。

put必须给expected_version，0只创建，更新提供原get回执的精确版本。冲突保留原值并失败，编辑后发布新版本才能发起新的意图；原Flow版本和回执不变。每任务100～5000ms、无自动重试，kv.get为幂等读、kv.put显式非幂等。没有任意SQL/文件路径/模板求值或设备写操作。

同项目KV写独占原项目资源、读共享；值/版本、原命令不可变回执、执行结果与审计同一数据库事务提交。重复原命令只观察原结果，不再次递增版本；Pause停派发，已领取有限事务可达安全终点，Kill/当前权限撤销发生在事务前则不写入。租约/fence过期拒绝写；回收器持有原执行锁后确认无提交回执，才写`kv_transaction_not_committed`失败证明，保留unknown及资源隔离，管理员显式Reconcile才能释放，不自动重放。迁移063及相关用例未执行。
