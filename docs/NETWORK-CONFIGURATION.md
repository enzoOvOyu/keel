# 网络配置闭环：当前框架与 PoC 接入

依据 PRD v3.0.3 FR-0.2/FR-0.4，复用共享 Execution；本文件记录已实现契约，不提升 FR-0 总体验收状态。

## 模块与修改入口

| 职责 | 文件 | PoC 修改范围 |
|---|---|---|
| pongo2 v6 沙箱、变量类型、JSON 输出与结构化 diff | `internal/netbaseline/render.go` | 扩充规范化配置节与噪声排除策略，不在页面拼接生产命令 |
| 固定 mTLS 设备网关协议 | `internal/netbaseline/client.go`、`internal/fabricboot/environment_probes.go` | 对接 SSH/凭据服务、Batfish、断言集、可选 containerlab；RESTCONF 复用同一协议 |
| 模板/意图版本、批准基线变量 | `internal/platform/network_templates.go`、`network_baselines.go`、迁移032 | 客户配置内容仍通过商定基线导入；旧版本不覆盖 |
| PushJob、冻结差异、审批、设备级证据 | `internal/platform/network_push*.go`、迁移033 | 补齐 WARN 确认、细粒度单台控制与变更计划展示 |
| 调度、SSH 锁、fencing、回执与未知结果隔离 | `internal/execution/network.go`、既有 `worker.go` | 不另建队列、租约或 Runner |
| 配置页面 | `web/src/features/network/`、生成 API 类型 | 在既有 Shell、Intel One Mono、主题令牌上扩展 |

## 接入与权限

操作者在权限600的 `KEEL_FABRIC_PROBES_FILE` 中添加可选 `targets.network`：`url`、`ca_file`、`certificate_file`、`key_file`；全部证书路径由服务器维护。`allow_config_write` 默认 false。API 与 Worker 加载相同文件；专用网关地址不接受浏览器参数。

网关固定接收 `POST /v1/projects/{project_id}/network-operations`，客户端不跟随重定向、不重试生产写入。`template-test / prepare / verify` 只读生产设备；`apply / rollback` 要求显式写入许可与有效 fencing token。模板试运行只能写隔离的验证设备。

- NE/NDE/Admin 可导入、验证模板；NH/Admin 独立发布，创建人不能自发布。
- NDE/Admin 创建并执行自己的 PushJob。所有目标从当前批准规划和已发布模板解析。
- 普通差异由独立 NE + DE 签署；Critical 追加 NH。同一账号不能重复计票，执行人不能自审批；派发前重新校验签署人的当前项目角色及账号状态。
- Viewer 只读；每次调用验证目标项目授权。归档保留审批、事件和证据。

`engineering_limits.configuration_variables` 按交换机 SN 导入变量；变量为客户商定内容，秘密只允许 `credential_ref`。渲染绑定批准的 hostname、管理 IP、gateway 和型号引用，不由平台生成生产设计。

## 网关输入和证据

输入含 operation/project/device/serial、phase、fencing token、凭据批次引用、baseline ID、template hash、approved_variables 与 intended。apply 含审批时 running hash 与备份引用；网关必须在写入的同一设备临界区检查 hash，发现外部漂移返回明确 rejected，不能覆盖新配置。operation ID 同时为 Idempotency-Key。

响应版本 `network-operation/v1`，绑定输入原始请求字节 SHA256、同一身份/项目/fence、provider、最近一分钟的 observed_at、`device` 或 `protocol_fixture` 来源。normalized running 为 JSON 对象，hash 为按键排序的紧凑 JSON SHA256；不得包含真实凭据。每项 gate 含 PASS/WARN/FAIL/UNKNOWN、位置、代码、证据摘要。缺失/UNKNOWN/FAIL 不能当 PASS。

| 阶段 | 必须具有的 PASS gate |
|---|---|
| 模板验证 | syntax、semantic、consistency、rehearsal、golden；回读与 intended 一致 |
| 准备 | syntax、semantic、consistency、reachability、credentials、system、ports、neighbors、configuration、services、batfish、assertions、rehearsal；另有 running 和持久备份引用 |
| 下发 | applied；网关按已批准计划执行，不自行选择配置或设备 |
| 复采 | connectivity、bgp、nvue、hardware、lossless；复采规范化配置 hash 与 intended 相等 |
| 回滚 | restored；回读 hash 与下发前快照一致 |

当前框架保守阻断 WARN，尚无逐项 DE 豁免界面。rehearsal 当前要求显式 PASS；按批次配置可选 containerlab 与免跑判据仍需扩展。网关实现必须保存工具输出/命令回执并使证据摘要可追溯；仅返回固定 PASS 不构成真机验收。

## 主链与故障边界

模板：不可变 draft → tested → 独立 NH/Admin released。按批准批次最多500台渲染，外部模板验证最大16并发；正式吞吐指标尚未测量。deprecated 与影响分析尚未提供接口，不以数据库枚举冒充已交付。

PushJob 使用 PRD 13态枚举。创建冻结目标、意图、配置指纹；准备通过后冻结结构化 diff、快照、报告、金丝雀门槛及并发参数并生成 review hash。签署不修改快照。`fabric.canary_min_size` / `fabric.push_concurrency` 从活动参数目录按项目覆盖全局解析，未登记时用 PRD 默认5/50。

准备和生产下发各自创建互链的域专属 Execution，复用 outbox、attempt、资源锁、fencing 与 Runner。普通 sim 创建入口不能运行这些 Flow。金丝雀复采通过后才允许其余设备取得 SSH 租约；失败取消余量并恢复金丝雀快照。

- 预检失败：BLOCKED；修复后可重做 PRECHECKING，审批证据重新生成。
- 明确拒绝写入（如审批后发生外部漂移）：不回滚、不覆盖外部修改，转部分完成并保留原因。
- 已确认的下发或复采失败：自动回滚并要求恢复读回证据。
- 写入/回滚响应丢失或证据不可信：设备 UNKNOWN，不伪造失败/停止回执；共享租约到期后进入 reconciling，资源隔离，禁止再次下发。
- 终态可归档；PARTIAL/REJECTED 不原地再次执行，需新 PushJob 与新审批。

## 已验证与尚未交付

隔离真实 API/PostgreSQL/Worker/浏览器 + mTLS 设备协议夹具覆盖：模板导入/验证/独立NH发布、三层结果、结构化 Critical diff、普通双签/Critical三签、错误摘要/重复签署拒绝、金丝雀、复采、恢复快照、外部漂移保护、未知写隔离、Viewer/跨项目拒绝、页面字体和来源标签。源码脚本 `scripts/check-kea-integration.py --network`；它只创建和删除带标签测试容器中的独立测试数据库，不写运行库或真机。

未完成：设备端生产 SSH/Batfish/containerlab 实装和真机 PoC；单台暂停/跳过、WARN 豁免、周期快照/漂移任务、模板废弃/影响分析、噪声排除、完整命令序列视图、未知写主动对账恢复、Golden 向 NP-2/Readiness 的全链投影与三年证据归档策略验证。FR-0 全局健康、规则治理、Readiness/G2 仍不能计为完成。协议夹具只替代设备边界，不能证明生产工具和硬件通过。
