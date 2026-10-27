# P2 H-2～H-5 执行包

本包当前为 NOT_RUN，供所有者在批准窗口内执行。脚本只离线检查证据，不连接设备、不修改平台或门禁，也不授予硬件验收 PASS。H-13 双架构拓扑沿用 [独立包](../topology-h13/ENVIRONMENT.md)，H-15/H-16/H-17 分别沿用既有 BOM、ZTP、Fabric 材料，不重复执行已被认可的 P1 环节。

## 环境与冻结信息

准备8台实际服务器、至少2台交换机，逐台登记资产ID、SN、型号、BMC/BIOS/NIC驱动及固件、内核与工具版本。HGX B300使用amd64；GB300使用arm64，并记录compute tray/rack身份和实际BMC/PCIe/NVLink能力。H-13须另有两种架构真实样本；缺型号或设备时登记BLOCKED，不能用其他型号或sim替代。

登记项目ID、运行平台完整40位commit、正式迁移/版本、批准操作窗口、执行人与独立审查人。运行版必须已包含所测功能；当前3000部署版本不能因为源码提交而视为已更新。按正式发布流程准备fixture/adapter/mTLS，不为填表绕过原设备身份、G1、网络就绪或G2/G3准入。

冻结批准型号版本与独立规格、BOM及线缆/rail规划、SSH host key、BMC凭据批次和版本、交换机SN/NOS/接口身份及DOM/计数器阈值。不得保存密码、私钥、Cookie、完整认证HAR；只保存脱敏响应正文及原始公开硬件观测。

## 操作顺序和断言

1. **H-2 Redfish**：8台分别通过平台共享Execution执行L1采集，导出manifest、ComputerSystem原文、Execution及credential批次复验记录。核对物理SN、冻结资产版本、origin=real、身份匹配、成功终态和原始摘要；凭据失败必须显式分类且不能沿用旧成功，轮换后的批次版本实际复验生效。L2/L3按实际型号另记，不以未采字段填默认健康。
2. **H-3 LLDP**：完成原G1、网络NP-1/2/4和独立拓扑前置，冻结四步核查Step1；通过原共享命令采集Step2/3，保存两端原始TLV、交换机侧回读和逐端口证明。核对BDF/永久MAC/接口、交换机SN/端口及BOM/rail三方一致；任何缺侧、陈旧、身份变化均应阻断。选择原始归档/端口证明而非手填PASS。导出当前Step2/3、完整覆盖和G2确认。
3. **H-4 物理映射及维修**：DL按精确当前候选发布G3，8台逐口人工核对BDF、型号面板位、NIC SN、永久MAC和实际线缆/交换机端口，完整启用分母100%一致。记录现场签字/图片、MappingRevision及绑定型号版本/哈希。另在获批端口做错接T2及计数增长T4病例，保留原始故障窗口→开单→分派→处置登记→原范围重验→证据关闭的全部历史。T4必须具备同源、同epoch、完整策略窗口的真实CRC/flap增长；一次拔线或LLDP空值不得冒充T4。T3/T4/T5重验须同类别健康证据，不能仅凭LLDP恢复关闭。
4. **H-5 DOM**：8台对在用模块执行受控只读`ethtool -m <明确接口>`或型号批准等价命令，留原文、工具版本、模块SN/厂商、采样时间和单位；同一时间窗对比面板相同physical port/source=host的温度、Tx/Rx等字段。交换机侧DOM单列，不替代主机读数。不支持模块保留unsupported/unknown与面板降级证据；缺字段/读取失败不标健康。原生厂商输出解析差异回录sim用例。
5. 恢复所有故障注入线缆和配置，保存恢复人/时间/原采集结果，确认无待对账unknown命令。提交sim差异台账，实际超时先留证据/issue，不为了通过放宽窗口或重复操作。

各病例命令/操作均由现场所有者批准执行。本包不会自动拔线、改交换机、刷固件、重启设备或执行远程脚本。真实特权LLDP/DOM适配器未就绪时保留BLOCKED，不以模拟helper作为设备证据。

## 保存及离线检查

将 [evidence.template.json](evidence.template.json) 复制到仓库外私有目录，复制服务器条目至8台、交换机条目至少2台，填真实信息。所有artifact为相对路径和原始文件SHA256，复用H-1有界/no-follow读取规则（单份≤4MiB、拒绝符号链接/重复JSON键/路径越界）。`comparison_record`记录逐物理端口/字段的人工比对表，不能只写一个总PASS。

所需平台响应以当前OpenAPI为准：BMC manifest与原资源、四步报告及覆盖、精确MappingRevision、工单详情与完整`wiring-retests`历史（G2工单使用领域重验接口，不使用fabric的通用verification列表）。不得改写响应JSON满足校验；整批/分页不完整需补齐原响应。校验器只核对选定字段与文件完整性，原始内容真实性、TLV逐字段、DOM单位容差及修复证明仍由所有者审查。

```sh
python3 poc/p2/validate.py /private/poc/p2/evidence.json
```

模板默认NOT_RUN，退出1并报告BLOCKED是正确结果。实际材料一致时返回READY_FOR_REVIEW，hardware_acceptance仍UNVERIFIED；填写 [CHECKLIST](CHECKLIST.md) 的私有副本并完成正式评审后，才可另外更新对应验收项。不能把离线脚本通过当作G1/G2/G3正向链验证或PoC签署。
