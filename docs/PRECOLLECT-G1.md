# 只读预收集与 G1（WP-2.3）

2026-10-27：本增量为源码实现，迁移080、相关软件用例及硬件 PoC 均 **NOT_RUN**，未部署到3000。需求为 PRD FR-4.1/FR-4.3、实施计划 WP-2.3；不能据此签署 P2。

## 实施边界与依赖

复用已存在的 BOM Machine/Component/PhysicalPort 稳定身份、已发布型号、SSH 信任与共享 Execution；不等待后置 Mapping、LLDP 或完整项目模板。G1 范围绑定当前目标、稳定物理端口、独立型号数量/架构、BOM 批次、BMC 原记录、SSH 连接、参数目录版本、采集协议及判定规则；G1 尚不适用的配置/Mapping/端口对确认关系不虚构版本。端口观测仅为暂定身份事实，不发布 PortModel 确认、NP-3 或 MappingRevision。

`host.precollect` 是专用域任务，只允许从预收集 API 创建。通用 SSH/Agent/Host Flow、通用重跑入口不开放它；重新采集必须重新预览并冻结范围。批量1～200台，共享调度并发最多4，设备/SSH只读租约、fencing、原始停止回执、幂等及审计沿用原执行器。

采集成功表示证据回收成功。只有原 Execution/attempt 已终止且原停止回执、原始版本、摘要、权限和当前范围全部匹配，worker 才单独生成不可变 G1 判定。当前通过与历史 PASS 分开读取；过期、型号/BOM/端口/信任/参数/适配器变化均失效。G1 PASS 的 `collected` 是资产状态投影，不增加身份 version，也不回退已进入后续阶段的资产。

## 固定只读采集

Linux helper 使用固定参数向量，无 shell、用户命令或任意路径输入：

- DMI/设备树 SN、OS release、运行架构；`nvidia-smi` GPU UUID、PCI BDF、显存容量、驱动与未纠正 ECC 观测、NVLink 状态、拓扑摘要。
- sysfs 接口与 PCI PF/VF 关系，`ethtool -P`/链路/`-i`/`-m`，RDMA sysfs、`ibv_devices`、`rdma -j link show` 交叉关联。
- `lspci -D -tv` 摘要；采集前后比较接口 MAC、MTU、管理 up 状态。该比较没有声称覆盖完整 PFC/ECN/RoCE 配置；这些配置及完整无变更 PoC 留后续环节。
- 固定工具候选必须由 root 拥有且不可被组/其他人写入；无自动安装、sudo、网卡 up、lldpd 启动或正式 GPUrail/RoCE 修改。当前非 root 账号对 DMI/模块读取的权限需 PoC 提供。

单次采集25秒、单探针2秒、任务30秒、原始 JSON 最多2MiB；接口128、RDMA128、GPU256，所有截断/超时/不可读保持显式缺失。只读任务的授权界限为原任务30秒，允许跨心跳续租；写任务仍限制在原租约内。它没有修改全局 worker 租期。

原始包放在原命令的私有目录0600文件，fsync后返回摘要和大小；SSH `read` 只能按8192字节对齐读取这个原命令的文件，校验原回执/fence/大小/逐块和全量SHA，禁止任意路径、软链接和内容上传。控制端写版本化对象并反向核验，Execution回执只保存记录ID/摘要。停止证明回收单独有界15秒，心跳保持至完成。unknown只显式查询/读取原命令，不能再次run；BOM/BMC过期不阻断原回执回收，但原SSH连接/信任仍必须匹配。关闭采集开关不阻断已有原始证据归档；关闭后不再派发/判定新工作。

## 判定与保鲜

架构与 GPU/NIC 数量来自不可变已发布型号，永久 MAC/组件/端口来自 BOM，不从本次结果反推预期，不硬编码8卡或某个型号。缺预期 GPU 的字段计入完整率分母。永久 MAC 不匹配/重复、SN或架构不符、GPU数量不符为 FAIL；缺字段、模拟来源、陈旧、DOM未知、配置比较不一致为 BLOCKED。缺失证据不能用零或默认版本填补。

BMC IP 必须在原 Managers/EthernetInterface 的保留观测中得到确认；BMC 与主机来源均需真实，不能将真实主机和 sim BMC 拼成 PASS。最终判定反读主机原始版本和 BMC SN/IP资源的原始版本，防止只有结构化记录而丢失源文件。BMC其他L2/L3缺失仍保留原采集质量，不因G1生成而被改成完整。

G1 TTL 默认24h、可收紧；BMC静态事实按 `asset.freshness_max_age`（默认24h、最多365天）。PASS 最迟有效期取两者较早值。历史判定不覆盖；当前G1返回历史证据、fresh/stale/invalidated及effective_verdict。前端在证据到期时本地降为阻断，不自动轮询设备。原始版本下载会再次校验完整摘要。

DOM不可读目前保留降级/阻断原因，不提供伪造豁免；DL备注审批与范围豁免账本随WP-2.8实现。NVLink/PCIe文本保留仅作预收集事实；预期拓扑四维比对、LnkCap/LnkSta、T7工单与G2 Step1属于WP-2.12，不能把本步当拓扑验收。

## API 与页面

- `GET /api/v1/projects/:id/assets/:assetId/precollect/scope`：冻结准入预览；GET不采集。
- `POST /api/v1/projects/:id/validation/precollect`：当前Admin/DE/DL/SRE、CSRF/Origin、幂等键；每目标提交精确scope hash与原因。
- `GET .../precollect`：分页请求/执行状态；详情 `.../precollect/:precollectId` 按需返回报告和门禁；`.../:precollectId/raw` 下载原版本。
- `GET .../assets/:assetId/gates/G1`：当前有效判定，与历史结果分离。

资产页按需加载“预收集 / G1”对话框，使用既有shadcn/ui和全局字体；展示范围阻断、完整率、字段原因、历史有效性、Execution链接与原始下载，无自动轮询。失败且未产出原始包的任务以原Execution状态追踪，不生成虚假的完整报告。

## 启用与后续集中验证

源码迁移080待隔离迁移演练和权限应用；API/worker `KEEL_PRECOLLECT_ENABLED=false`，目标helper另有 `KEEL_SSH_PRECOLLECT_ENABLED=false`。需要已有SSH配置/信任、完整已提交BOM与发布型号、BMC观测和版本化对象存储。compose/env示例仅增加默认关闭字段，未修改运行环境。

已写但未运行：amd64/arm64及不同GPU数量、MAC/DOM/ECC/陈旧/模拟/配置变化、缺GPU分母、固定原始解析、额外/重复输入、只读块边界、原fence/摘要绑定、软链接拒绝、已有原回执不重采、通用准入/重跑封闭及隔离PG缺前置/越项目读取用例。真实DMI权限、NVIDIA/ethtool/RDMA输出兼容、模块和硬件状态、200台规模与传输性能均待用户安排PoC/重大里程碑集中验证；没有测试PASS结论。

工具协议参考：[NVIDIA nvidia-smi](https://docs.nvidia.com/deploy/nvidia-smi/)；实际工具/驱动版本须随PoC记录，不能用本文代替兼容性验证。
