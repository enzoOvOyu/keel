# PCIe 拓扑实施记录（WP-2.12）

本模块在预收集的原始证据中保存 sysfs PCIe 路径、NUMA、PCIe 端口类型、最大/当前链路速率和宽度；固定 NVIDIA 查询提供 GPU index/UUID/BDF/序列号、topo 矩阵和 NVLink 链路状态。采集只读，不下发驱动、RoCE、LLDP 或固件配置。旧预收集记录没有这些字段时，不伪造拓扑。

型号 `expected_topology.version=keel.pcie-topology/v1` 使用严格 Schema。`provenance` 必须包含独立 `vendor_spec` 或 `approved_golden` 来源、引用及 SHA256；`devices` 逐项定义稳定 key、gpu/nic、pcie_slot、root、upstream、numa、speed_gt、width；`nvlinks` 显式列出 GPU 有向对和连接数，包括零。示例只读行为规格在 `sim/fixtures/nvidia-smi-topo/*.json`，均为人工合成，不代表厂商规格或真实硬件验收。

对比不得按待验 BDF 排序建立型号预期。`Binding` 由稳定 BOM 部件身份与已归档身份观测关联；缺少可靠 GPU 序列号或 NIC 永久 MAC 时不能建立通过关系。实际路径与独立型号路径比较；NUMA、NVLink 矩阵/状态、链路按槽位设计值单独比较。设备最大 x16 与槽位设计 x8 可以同时成立。缺测为 BLOCKED，确切冲突为 FAIL；sim 完整也不能生成真实 PASS。

亲和计算使用 sysfs 路径和 PCIe capability 的端口类型区分 Root Port、Switch Upstream、Switch Downstream。NUMA 不直接等于 socket；CPU package 证据不足时 NODE/SYS 保留 unknown。NVIDIA NIC 编号只通过原始 NIC Legend 的接口名关联，不推断编号顺序。NVLink 仅来自 NVIDIA 的矩阵和链路状态。

2026-10-27：采集与四维核心、严格型号录入和四类异常 fixture/源码用例已编写，**NOT_RUN**。数据库版本化、当前G1范围核验、T7差异开单与资产页视图已接源码；T7处置重验、Step1消费及H-13材料继续实施中，WP-2.12 尚未闭环。真实 B300/GB300 工具输出、槽位属性与亲和语义、性能统一留待集中测试和用户 PoC。

视图当前使用已有React能力提供可访问的上游层级树；设计要求的完整React Flow交互仍未完成。新迁移082仅源码，未应用。当前窗口开发检查点不代表验证、发布或P2完成。

T7源码重验链（083待应用）：先登记处置，随后新发起预收集并获得新G1，验证其拓扑后从工单提交当前快照。每个处置版本一次复测；全拓扑PASS进入待确认，标准关闭动作再次核对当前范围和权限。两次独立处置仍FAIL生成DL升级与站内/Execution outbox记录。缺证据不算确定失败，未配DL明确显示。固件审批/mock执行回执仍未接入，不能将处置登记当设备执行。所有新增用例NOT_RUN。

型号追加显式 `gpu_nic_map`（gpu/nic稳定key及affinity），由独立规格声明；关系API只投影模型列出的对应项，并保留缺测行和物理端口身份，不发布Step1/G2。项目拓扑总览最多200台，按当前有效判定筛选后分页。全局Fleet、完整React Flow、固件批准/mock回执和真实PoC仍待后续。H-13执行入口见[poc/topology-h13/ENVIRONMENT.md](../poc/topology-h13/ENVIRONMENT.md)，全部NOT_RUN。
