# H-13：B300 / GB300 PCIe 拓扑 PoC

状态：**NOT_RUN**。本目录仅提供执行材料；不主动连接硬件、配置设备或触发任务。P2 新源码尚未发布，须先在获授权的受控环境部署对应提交并迁移；3000 当前版本不具备这些新接口。

准备一台 HGX B300（amd64）和一台 GB300 对应托盘/机柜通信域样本（arm64）。记录实际序列号、内核、NVIDIA/lspci 版本与介质/Agent架构。不能将 B300 的 8 GPU 假设直接用于 GB300，数量、槽位与拓扑取各自已发布型号。提供独立厂商规格或已审批金样来源原件及 SHA256，逐项录入 `expected_topology`，其中 `gpu_nic_map` 明确关联，不能由待验 BDF 排序或实测路径反写预期。

在用户批准的 PoC 窗口内：

1. 完成 BOM、型号、BMC 库存、SSH 固定身份与预收集准入。采集使用固定只读 sysfs/NVIDIA/lspci 查询，不安装工具、不配置网卡、不启动 LLDP。不支持的字段保留未知；GPU SN 无法可靠与 BOM 关联、多 PF 无独立型号表达或板载槽位无法读取时记录 BLOCKED，不手工填“已读取”。
2. 从资产页发起预收集，等待共享 Execution 原命令停止回执和单独 G1 判定；下载原始采集 `GET /api/v1/projects/{id}/assets/{assetId}/precollect/{precollectId}/raw`。保留原始字节和摘要。字段不完整或 G1 不通过时，记录实际缺口，不能用 PoC 清单代替门禁。
3. 在资产的 PCIe 拓扑入口刷新范围，验证独立预期。导出 `GET .../topology/{snapshotId}` 正文、`GET .../gates/G1` 正文、型号版本列表中对应完整对象/页面、原预收集 Execution 完整 detail（全部 attempts）及拓扑页面截图。healthz 的 version 必须等于本次平台 40 位提交。
4. 逐项核对 sysfs 上游树/NUMA/socket，平台亲和计算和原始 `nvidia-smi topo -m`，NVLink 链路和矩阵，LnkCap/LnkSta 与槽位设计。不同 NUMA 不直接等于不同 socket；板载 GPU/交换芯片、SNC、根桥与特殊 NVLink/NIC 组合遇未适配格式时先保留证据，不构造 PASS。记录差异供回录。
5. 若出现 T7，先诊断与登记适配处置，再重新发起预收集、新 G1、拓扑验证及工单复测。确认关闭使用当前有效证据；连续两个独立处置复测失败交 DL/厂商诊断。处置登记不执行设备命令；固件审批/mock回执尚未实现，不能拿登记记录声称已刷写。

复制 `evidence.template.json`，填写相对路径、每份原件 SHA256、实际 UTC 开始/结束及每样本导出时间。原件单个上限沿用既有证据读取器 4 MiB；凭据、私钥和会话 Cookie 不入包。完成真实采集后才将状态改为 `COLLECTED`。

后续集中验证或 PoC 时运行（本轮未运行）：

```sh
python3 poc/topology-h13/validate.py /path/to/evidence/manifest.json
```

离线脚本检查摘要、作用域、实际架构、型号来源、原始节点/NVIDIA身份与亲和逐项一致、G1和原停止回执。成功仅输出 `READY_FOR_REVIEW`，**hardware_acceptance 仍 UNVERIFIED**；负责人审核硬件身份、独立规格、UI一致性后另行签署。它不签署完整 P2/G2/NP-3，不证明并发/性能或 React Flow 交互完成。

软件行为规格在 `sim/fixtures/nvidia-smi-topo/*.json`；四类异常及打乱 BDF 用例均人工合成，不能填入真机证据。新增 Python/Go 用例仍 **NOT_RUN**。
