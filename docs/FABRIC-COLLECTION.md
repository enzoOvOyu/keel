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


## P2 接续：端口诊断持久证据（源码，待里程碑验证）

070迁移增加不可变`fabric_diagnostic_records`、端口工单绑定和异常观察。`POST /api/v1/projects/{id}/fabric/devices/{deviceId}/diagnostics/record`接受可选的建模端口，仅计算当前项目最新有效采集缓存；权限为当前项目`fabric:collect`，需要Origin/CSRF。普通GET诊断不创建记录或工单。该操作不调用设备重新采集，不等同定向重验Execution。

记录冻结网络BOM、设备/端口范围、device/protocol_fixture来源、原采集run和样本引用、计数窗口/阈值、配置意图与DDM阈值/光模块读值。已有诊断器产生的T2-rail/T4/T6明确异常，以及DDM明确warning/critical的T3，按稳定端口/类别/子类/规则ID/来源复用同一统一工单，版本化规则留在每次观察；未知和不适用不自动开单，也不关闭既有工单。缺失后续配置意图仍为unknown，不把尚未开发的Golden作为本步开工前置。

同一范围和完全相同的证据/版本复用原记录及原过期时间，读取时间或重复run不能延长其有效期。记录及关联观察只追加，规则和参数变化生成独立证据。`GET /api/v1/issues/{issueId}/port-diagnostics`按当前项目权限分页查询，保留原记录；stale当前反映TTL和网络BOM变化，不能代替后续重验准入对全部配置/规则版本与范围的复核。前端有显式记录入口、工单跳转与历史原值查看；NP-3始终BLOCKED。

本批仅格式化和生成OpenAPI类型，新增指纹隔离单元用例及缓存记录/幂等/不可变/未知端口的PostgreSQL用例源码，均未执行；070尚未应用，3000不变。下一步接领域限定的重验Execution与完整证据关闭/失败升级，不把本次缓存记录冒充硬件复采或整项WP闭环。


## P2 接续：重验作用域预览与定向复采契约（源码，待里程碑验证）

`GET /api/v1/issues/{issueId}/verification-scope`按当前项目成员权限返回只读预览：工单/最近处置版本、可信发现绑定与规则版本、当前网络BOM、最多两台交换机及具体端口、原device/protocol_fixture来源、参数/意图摘要、分类所需检查和缺失条件。重新分派/异常重开后要求新的处置登记；原链路无法对应当前BOM、跨主机侧、重复链路、未知身份不能直接套用旧范围。读取时间不参与scope_hash；这只是当前事实摘要，并非持久冻结请求或已接受的Execution，后续提交必须在事务内重算并复核当前权限和版本。界面按需查看，不因打开工单触发复采。

T1/T2需单链路LLDP+交换机回读，端口T2还需rail意图核验；T3/T4需LLDP+DDM+单路径连通性；T5需预收集增量+LLDP+连通性；T6需配置回读+连通性；T7需拓扑+预收集增量。当前连通性、服务器侧等缺失条件显式返回。共享Execution提交/资源租约/结果消费与证据关闭尚未接通，`execution_available=false`，verify/close仍阻断。本批没有新增迁移或伪造执行记录。

`ProbeSuite.RecollectPorts`新增固定mTLS origin上的`POST /v1/projects/{projectId}/recollections`客户端契约；现有GET snapshots仍仅缓存读取。请求/响应均为`fabric-recollect/v1`，绑定project_id、execution_id、command_id、fencing_token、scope_hash与evidence_source；请求另含not_before及targets[{switch_id,serial,ports}]。仅允许1～2台、每台1～2端口；设备/协议fixture不可切换。POST使用command_id作Idempotency-Key，禁止重定向、30秒边界、响应上限1MiB，无自动重试。响应必须精确匹配目标serial/端口且collected_at位于not_before与本次接收时刻之间，拒绝缓存、范围外读回及未请求的全局遥测。

该客户端尚未由运行中的worker调用，也没有实现外部采集器服务或真实NOS重采适配；不能因契约存在宣称设备复采通过。后续先接交换机间T1/T2的共享Execution提交与双端资源锁，再将该响应作为围栏保护下的持久证据消费；T3/T4/T6完整关闭须等待其连通性消费者。新增拒绝重放/串范围/来源变化、HTTP路由/重定向、摘要隔离及PG预览用例均未执行，留待重大里程碑。
