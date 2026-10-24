# 网络健康、只读证据与日志框架

基于 PRD v3.0.3 FR-0.6 / FR-0.8，实现模块供后续规则治理与服务器域消费；本记录不代表 FR-0 全体验收。

## 实现边界

- `internal/fabricmon/metrics.go`：类型化指标注册、十进制精确计数器、窗口覆盖/重置/来源切换检查、受限表达式 AST。禁止执行任意代码。缺失指标、单位错误、重置或窗口断裂不能作为零值和正常。
- `lossless-defaults.json` / `lossless.go`：LL-01～LL-07 数据规则、同层取严和 device > port > priority > pod > role > fabric > 默认优先级；停用覆盖为 unknown。配置态与批准逐 SN 意图分别输入；不猜 DSCP 26/优先级 3，不以共享池深度冒充单端口深度。LL-03 弱缓冲证据单列降级；LL-07 估算标明 estimated。
- 健康扣分按端口×优先级组合去重后用权重×sqrt(n)；端口→设备→Pod→Fabric 取最低分，unknown 不显示健康通过。全局/项目 `fabric.health_score.weights` 覆盖内置权重。
- 光模块仅评估 BOM 线缆连接的交换机端口；未使用端口不产生告警。明确非光口可豁免 DDM，已规划端口缺快照/缺阈值/适用性不明则 BLOCKED。当前 BGP/PTP 为归一化协议状态，尚无逐邻居/逐时钟域诊断。
- Readiness 使用当前 BOM、批准规划、Kea N-S3、当前采样、真实 ZTP managed 和有效 Golden 读回。TTL 30 秒；证据过期或事实变化不能沿用旧 PASS。NP-3 只由后续服务器 G2 三方证据产生，G2 前置使用 NP-1/2/4，不循环依赖自身产物。协议夹具不能产出生产 NP PASS。
- `logs.go` / `log-defaults.json`：14 类内置正则规则、同规则聚合、未识别错误兜底、首末时间/对象/3 条样例；来源 syslog / snmp_trap、归一化级别和时间窗验证。采集器须在上传前脱敏；检测到显式凭据则拒绝入库。日志窗口由采集器声明，缺完整覆盖保持未知，不能把空结果当采集成功。

## API 与 UI

所有接口先验证当前项目权限并记查看/导出审计；统一字段 `source=switch` 与独立 `evidence_source=device|protocol_fixture`，不混淆证据归属和可信级别。

| GET 相对 `/api/v1/projects/{id}/fabric` | 返回 |
|---|---|
| `/health` | 有界总览、设备计数与最低分；不重复完整 finding 明细 |
| `/readiness` | NP-1～NP-6、N-S7、G2 准入与 TTL |
| `/devices/{deviceId}/health` | 当前 BOM 中指定设备的 LL-01～LL-07 明细 |
| `/lldp?device_id=&port=` | 规划端口的邻居读回、引用与时效；无数据 BLOCKED，不返回空成功 |
| `/cables?page=&page_size=` | N-S5 预检四值门禁、规则版本、来源与有效期；服务器 NP-3 保持 BLOCKED |
| `/devices/{deviceId}/logs?hours=&level=&keyword=&page=&page_size=` | 6/24/48/72h、默认 48h，问题聚合与分页原始行 |
| 上述日志接口加 `format=json|csv` | 导出筛选后的完整采集窗口（上限 20000 行），独立导出审计，CSV 防公式执行 |

`/p/{projectId}/health` 共用工作台/主题/Intel One Mono；设备、规则、日志筛选及页码入 URL。日志原始行和样例按需展开，不在页面判断计数器或生成设备状态。PoC 只替换采集器边界，保留归一化契约和平台判据。

## 采集协议增量

`fabric-snapshot/v1` 可选 `metric_points`、`applied_lossless`、`logs`、`log_window_start`；端口可选 `optics_applicable`。提供数据须对应显式 capability。每设备最多 100000 指标点、20000 条日志、8192 端口；历史指标取同 BOM、同来源的近 48h，不混用矛盾样本。指标值由 Go json.Number/big.Rat 保留精度，浏览器只展示服务端结论。

## 本批验证与后续范围

有效集成目录 `/tmp/keel-shell-lw2nw8tb`：真实 API/PostgreSQL/Worker、mTLS 采集器协议夹具与浏览器。覆盖无数据 unknown、30s Readiness、服务器门禁不循环、夹具不能满足生产门禁、完整窗口正常、2^53 以上计数器 +1、QoS 不一致、LLDP 项目端口校验、线缆分页、日志归一化/聚合/覆盖缺失/过滤/JSON CSV 导出、页面及 Intel 字体。定向 Go 测试与 vet、契约生成/TypeScript/Vite 构建通过。

本批不包含真实 NOS 采集器、设备接线 PoC、后台采样调度与完整保留策略、规则治理审批持久化、LL-02 Fabric ≥3 端口聚合升级、逐 BGP 邻居/PTP 细粒度检查、完整 T2/T4/T6 诊断、日志工单/证据包关联、网络交付基线导出或服务器 G2 执行消费。独立日志 API 不等于整个 FR-0.6.7 闭环，Readiness 输出不等于服务器域已消费，仍须完成这些后续范围。

## LL-02 跨端口升级（2026-10-24）

`storm-policy.json` 登记默认三个端口门槛与版本，`storm.go` 只合并同一判定时点的 LL-02 结果。同一设备端口的多个优先级计一次；混入历史判定、缺少窗口或设备端口身份时保持 unknown。已确认至少三口命中时保留升级结论，并单列其他端口缺失证据与暂停帧近似标志。健康 API 与页面显示命中数、降级状态及可跳转的受影响设备；不另扣健康分或自动修改设备。

判定时间由规则计算器原生写入，聚合层不重写时间；暂停帧替代 XOFF 事件时同时保留 approximation 与 degraded，不把近似观测当完整事件证据。默认跨端口门槛尚未接入规则管理编辑器。

实际验证：`go test -race ./internal/fabricmon`、相关三包 `go vet`、`npm run build`（含契约生成与 TypeScript）通过。隔离真实 API/PostgreSQL/mTLS 协议夹具/浏览器证据 `/tmp/keel-shell-ml8ladbs`，覆盖空态 unknown、正常窗口、同一窗口两设备三独立 BOM 端口升级、规则版本引用、受影响设备跳转与 URL 筛选、项目隔离，以及现有日志/健康回归。单测覆盖优先级去重、混入历史时点、缺失身份/窗口、近似降级与已确认命中不被其他未知数据抹除。原 026 草稿通过 Go build overlay 排除；隔离进程、测试库及临时凭据已清理。未部署、未跑硬件 PoC；历史批次“本批不包含”不再适用于本节已验证的跨端口升级，但其他边界继续有效。
