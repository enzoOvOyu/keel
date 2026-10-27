# N-S5 采集与预校验入口

范围：WP-2.14 的交换机侧快照、LLDP预校验、DDM读回和设备状态页增量，不代表该WP或FR-0整体验收完成。

## 模块与接口

- `internal/fabricboot/collector.go`：运维固定的mTLS采集器客户端；复用探针CA/证书，从URL origin读取 `GET /v1/projects/{projectId}/snapshots`。读取采集器现有快照，不建第二套作业队列，不从浏览器接收观测/PASS。响应限8MiB/10s、禁止重定向。
- `internal/fabricmon`：独立的标准化交换机观测契约、时间/能力/身份校验、按端口索引的双向LLDP比对及DDM纯判定。NOS输出/端口别名差异在采集器适配层修改，业务服务和页面不猜测厂商命令。
- `internal/platform/fabric_collection.go`：当前网络BOM与项目准入、采集前后BOM重验、不可变样本与结果、陈旧判定、参数目录阈值、查看审计。031迁移独立于未提交026草稿。
- `/api/v1/projects/{id}/fabric/collections` POST读取/持久化，GET `/latest` 返回最新结果及当前BOM设备，GET `/devices/{deviceId}/evidence` 返回字段带switch来源的样本与DDM判定。
- `FabricPage` 与 `NetworkDeviceDrawer`：URL筛选/排序/选中对象/Tab、列表窗口化、八Tab只渲染当前内容、上一台/下一台、Intel字体与明暗令牌。无生产配置/日志证据的Tab明确显示无证据，不伪造内容。当前实现为项目过滤视图，尚未实现跨项目运维聚合。

采集器出站契约为 `fabric-collection/v1`，含 `project_id`、`evidence_source=device|protocol_fixture`、`devices[]`。设备结构见主OpenAPI的FabricSwitchSnapshot：`version=fabric-snapshot/v1`、`source=switch`、provider、serial、collected_at、capabilities、ports/邻居/DDM/FDB/ARP及可选BGP/PTP读值。证据来源单独保留；协议桩不是硬件验证。返回非本项目BOM设备、重复设备、过期/未来时间及能力矛盾时拒绝整次写入，不覆盖原有效记录。只持久化标准化类型，完整NOS配置和未知原始字段不入库/API。

## 判据与缺失

- 双向交换机LLDP需身份（序列号或SoT机箱MAC）与端口都一致；缺采集能力/接口状态保持pending。明确链路down/LLDP为空为T1，错设备/错口为T2，不支持光模块为T3。
- 未装机服务器接入口始终pending；N-S5不写服务器PortModel，不生成NP-3。NP-3固定BLOCKED，须由后续G2三方比对产生。
- 快照最多5分钟；结果有效期不晚于最早样本+5分钟，BOM变更或过期后旧预校验不再显示通过。该时效不是NP-4的30秒门禁，后者仍需专门实现。
- DDM判据从现有参数目录取 `fabric.ddm.<rx_dbm|tx_dbm|temperature_c|voltage_v|bias_ma>.<warn_min|warn_max|critical_min|critical_max>`，项目覆盖global；不另建阈值表。缺值/阈值为unknown，非法数值/顺序阻断判定。当前阈值命名是适配目录的实现约定；规则治理接入时需要保留版本快照，不能把本次静态OPTICS-DDM@1当完整规则版本链。
- Admin/NDE可读取采集；NE可定向复测，权限点fabric:collect；所有合法项目角色可查看。Viewer不能触发，权限来自当前目标项目成员，不采用跨项目导航权限并集。

## 已验证与剩余

定向身份/陈旧/unknown/LLDP/DDM纯判定、Go vet、前端类型/构建，以及隔离真实PostgreSQL/API/浏览器+mTLS协议桩链已验证；有效证据见HANDOFF。本轮截图已人工查看；无设备数据时展示当前BOM和unknown，不向3000运行库注入fixture。

剩余：采集计划/保留与降采样、趋势/PNG、完整T2-rail/T4/T6及FDB/ARP辅助诊断、工单定向重验与证据关闭、正式规则治理/版本和NP-1～6就绪接口、生产配置/日志Tab、规模与真机PoC。不能因本次有页面和协议连通而勾选整个F-0.6～F-0.8通过。


## P2 接续：网络异常接入统一工单（源码，待里程碑验证）

069迁移增加`fabric_issue_bindings`与只追加的`fabric_issue_observations`。采集持久化事务中，可信判定器的明确线缆异常自动进入WP-2.7同一工单池；当前来源为`fabric`、阶段为`delivery`，至少锚定该项目BOM中的线缆和两端稳定端口，不要求服务器Machine/G3存在。仅现有线缆判定器实际产生的类别可开单，不能据此声称T2-rail/T4/T6全分类或DDM工单已贯通。

同项目/线缆/类别/证据来源复用同一工单；`device`与`protocol_fixture`独立绑定。每个采集run都有不可变观察关联，指纹包含网络BOM、规则/判定和两端实际样本；完全相同的快照不会重复开单或推进工单版本，样本变化则追加事件与版本，原始发现证据不改写。明确单侧故障可保留另一侧缺失，缺失本身不冒充故障。未知/仅网络预校验通过/陈旧报告不产生新异常工单，也不自动关闭原工单。

`FabricCollectionReport.issue_links`是授权范围内的关联投影：同模式、同包线缆已有未关闭工单继续可达，即使最新观察为pending或仅网络预检通过。网络页面可跳转统一`/issues?project_id=...&issue_id=...`详情；`GET /api/v1/issues/{issueId}/fabric-observations`分页返回来源、规则、原采集run/包、观察/有效时间和指纹。过期或当前网络BOM已变化时标记stale，旧证据留档。只读历史与采集缓存读取都不意味着设备侧已重新执行采集。

当前继续使用运维固定的mTLS快照GET契约；受限定向采集/复验Execution、端口诊断持久证据、有效PASS关闭及失败升级仍待后续交付。普通`verify/close`保持阻断，不把N-S5或协议仿真结果当G2/NP-3或真实硬件通过。只完成格式化、API/路由生成；新增集成用例留待P2重大里程碑集中执行。运行库与3000本轮未变。
