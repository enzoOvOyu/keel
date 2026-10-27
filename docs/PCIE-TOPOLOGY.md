# PCIe 拓扑实施记录（WP-2.12）

本模块在预收集的原始证据中保存 sysfs PCIe 路径、NUMA、PCIe 端口类型、最大/当前链路速率和宽度；固定 NVIDIA 查询提供 GPU index/UUID/BDF/序列号、topo 矩阵和 NVLink 链路状态。采集只读，不下发驱动、RoCE、LLDP 或固件配置。旧预收集记录没有这些字段时，不伪造拓扑。

型号 `expected_topology.version=keel.pcie-topology/v1` 使用严格 Schema。`provenance` 必须包含独立 `vendor_spec` 或 `approved_golden` 来源、引用及 SHA256；`devices` 逐项定义稳定 key、gpu/nic、pcie_slot、root、upstream、numa、speed_gt、width；`nvlinks` 显式列出 GPU 有向对和连接数，包括零。示例只读行为规格在 `sim/fixtures/nvidia-smi-topo/*.json`，均为人工合成，不代表厂商规格或真实硬件验收。

对比不得按待验 BDF 排序建立型号预期。`Binding` 由稳定 BOM 部件身份与已归档身份观测关联；缺少可靠 GPU 序列号或 NIC 永久 MAC 时不能建立通过关系。实际路径与独立型号路径比较；NUMA、NVLink 矩阵/状态、链路按槽位设计值单独比较。设备最大 x16 与槽位设计 x8 可以同时成立。缺测为 BLOCKED，确切冲突为 FAIL；sim 完整也不能生成真实 PASS。

亲和计算使用 sysfs 路径和 PCIe capability 的端口类型区分 Root Port、Switch Upstream、Switch Downstream。NUMA 不直接等于 socket；CPU package 证据不足时 NODE/SYS 保留 unknown。NVIDIA NIC 编号只通过原始 NIC Legend 的接口名关联，不推断编号顺序。NVLink 仅来自 NVIDIA 的矩阵和链路状态。

2026-10-27：采集与四维核心、严格型号录入和四类异常 fixture/源码用例已编写，**NOT_RUN**。数据库版本化、T7 处置/重验、Step1 消费、前端、H-13 材料在接续实施中，WP-2.12 尚未闭环。真实 B300/GB300 工具输出、槽位属性与亲和语义、性能统一留待集中测试和用户 PoC。
