# PoC 执行包 #P2 — H-2~H-5、H-13（真机）

> 执行机制（§5.5 / ADR-14）：开发期不连接真实硬件；本包由工程所有者在受控 PoC 环境（8 台 BMC 真机 + 2 台交换机 + B300/GB300 对应架构样本机）择机执行回填。回填记录即 ACCEPTANCE L4 证据；**产出是 P2 判据，执行回填不是**。

## 验收判据（与 ACCEPTANCE 一字不差）

| # | 项 | PoC 环境 | PASS 判据 |
|---|----|---------|----------|
| H-2 | Redfish 真机 | 8 台 BMC | L1 采集成功；凭据批次管理生效 |
| H-3 | LLDP 真机 | 8 台 + 2 交换机 | TLV 交换机可读；网络平台回读比对通过 |
| H-4 | 映射物理比对 | 8 台 | 映射与人工核对（BDF/面板位/线序）100% 一致；人工拔线触发 T2/T4 分类与工单闭环演练成功 |
| H-5 | DOM 真机 | 8 台 | 光模块 DOM 读出；面板显示与 ethtool -m 实测一致；不支持 DOM 的模块正确降级标注 |
| H-13 | PCIe 拓扑真机 | B300 与 GB300 对应样本 | 型号独立预期与 sysfs/nvidia-smi 输出逐项比对；NVLink/LnkSta 读出；Arm 工具/Agent/介质适配留证；拓扑页范围与实机一致 |

## 环境要求清单

**硬件**：8-10 台带 BMC 的样机（H-2/H-3/H-5 共用）；2 台交换机（H-3）；B300 样机与 GB300 Arm 样机各一（H-13 需两套独立预期）。

**网络/服务**：BMC 网可达（Redfish HTTPS）；交换机侧 LLDP 开启且网络平台可回读；目标机已装 lldpd 且 keel 平台互信已建立（复用 #P1 的互信成果）。

**凭据**：`KEEL_BMC_BATCH` 批次引用 + 每台 BMC 的凭据环境变量（`KEEL_BMC_<user_ref>`，形如 `user:password`）——H-2 的凭据批次语义：批次更换后全量重验，不可达分类为 credential 类不误报 BMC 故障。

**工具**：curl（H-2）、ssh/lldpcli（H-3）、ethtool（H-5）、lspci + nvidia-smi（H-13）、jq。

**清单文件**：
- `poc/hosts-p2.csv`：`hostname,bmc_ip,bmc_user_ref,switch,switch_port`（8-10 行）
- `poc/topology-b300.expected.json` / `poc/topology-gb300.expected.json`：**按架构独立**的拓扑预期（GPU/NIC BDF、NVLink 拓扑、LnkSta 期望——来源：型号库规格，不是从实机抄回来的值）

## 执行步骤

```sh
# 0) 自检（缺什么报什么）
KEEL_BASE_URL=http://<platform> sh poc/selfcheck-p2.sh

# 1) 真机驱动（H-2/H-3/H-5/H-13 的可自动断言部分）
KEEL_BASE_URL=... KEEL_BMC_BATCH=batch-04 \
KEEL_BMC_admin='user:pass' DOM_HOST=poc-b300-n01 TOPO_HOST=poc-b300-n01 POC_ARCH=b300 \
sh poc/run-p2.sh
# GB300 侧重复：POC_ARCH=gb300 TOPO_HOST=<gb300-sample>

# 2) H-4 人工比对与拔线演练：按 CHECKLIST-P2.md 逐项核对并记录；
#    拔线演练须走完 T2/T4 分类 → 工单创建 → 处置 → 定向复测 → 关闭全链

# 3) 回填：p2-run-<ts>.json + CHECKLIST-P2.md 署名即 L4 证据
```

## 已知边界（诚实声明）

- H-3 的"网络平台回读比对"与 H-4 的"人工核对 100% 一致"**无法脚本自动断言**——脚本只采集本端 LLDP 邻居原始记录，比对结论由 operator 在清单中签署
- H-13 的逐项比对基于**预期文件 vs 实机输出**，预期文件由工程所有者按型号库规格预先编写（两套架构分别独立），不能以实机输出反填预期
- 凭据以环境变量注入仅为 PoC 便利；正式凭据走 vault 批次管理（FR-3.6 真实通道）
