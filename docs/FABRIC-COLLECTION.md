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

剩余：采集计划/保留与降采样、趋势/PNG、完整T2-rail/T4/T6及FDB/ARP辅助诊断、工单联动、正式规则治理/版本和NP-1～6就绪接口、生产配置/日志Tab、规模与真机PoC。不能因本次有页面和协议连通而勾选整个F-0.6～F-0.8通过。
