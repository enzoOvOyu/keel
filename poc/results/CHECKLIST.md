# H-1 所有者审查清单

本文件是待填写模板，不是既有验收记录。离线校验 READY_FOR_REVIEW 只证明内部一致，不授予H-1 PASS；任意缺证据项必须记录BLOCKED/FAIL，不得用软件自检、手工SSH查询或模拟状态替代。

执行人：____________  审查人：____________  UTC日期：____________
环境编号：____________  项目ID：____________  平台提交：____________
装机发布引用：____________  证据manifest摘要：____________

## 硬件及HTTP-first

- [ ] 10台HGX B300真实SN/MAC与冻结BOM、控制台记录逐一一致，无模拟设备或GB300替代。
- [ ] 每台均经UEFI HTTP Boot完成引导与装机，原始日志在位，无Legacy PXE兜底。
- [ ] 现网非PoC客户端DHCP正常，批准网络窗口和前后观察有记录。
- [ ] cloud-init hostname与BOM及实际host.identity输出逐台一致。

## 自动互信与凭据

- [ ] 公钥注入、回调及信任建立实际由批准的自动流程完成，10/10且无人工补造状态；手动trust.verify/install成功本身不满足此项。
- [ ] 当前平台10台均provisioned、established、来源ssh；完整资产页与BOM一致，未截取部分PASS记录。
- [ ] 每台信任请求、独立批准人与实际终态执行可追踪；repair未替代首次自动引导证明。
- [ ] 每台固定host key与独立可信BMC/控制台公钥一致，未通过自动接受陌生指纹建立信任。
- [ ] authorized_keys公开差异只追加受限平台条目，客户既有内容未删除；私钥只在控制面保管。
- [ ] host.identity的command/attempt/fence/项目/机器/资产版本和观察窗口一致，无unknown或模拟回执。

## 证据真实性与结论

- [ ] manifest校验为READY_FOR_REVIEW，原始文件摘要匹配；所有者另已核验日志来源和真实性。
- [ ] 原始证据不含私钥、口令、Cookie或认证HAR，资料在受控仓库外保存。
- [ ] 发现的sim与真机差异已回录回归用例；未决项列明责任人与处理计划。

所有者结论：待审 / BLOCKED / FAIL / PASS（圈选，不预选）
未决项及证据引用：________________________________________________
执行人签字：____________  审查人签字：____________

只有全部断言有真实证据并完成正式评审后，才可另行更新H-1验收状态；本模板及校验器均不自动完成该操作。


## H-17 网络侧执行材料（步骤 D / N-S5 子集）

模板在`poc/fabric-h17/evidence.template.json`，离线检查入口为`python3 poc/fabric-h17/validate.py /private/poc/h17/evidence.json`。本轮仅编写材料，未执行硬件PoC或检查器；模板默认NOT_RUN。完整H-17要求8台服务器和2台交换机，并需后续G2三方确认/PortModel证据；N-S5子集就绪不能签署完整H-17。

- [ ] 在仓库外受控私有目录保存平台精确提交、项目、批准窗口、设备SN/型号/NOS和UTC起止；不上传凭据、Cookie、私钥或认证HAR。collector按受控mTLS接入，真实配置另按发布流程准备，不能为了填表修改3000默认禁用项。
- [ ] 保存九组对照样本：交换机互联正常、未装机服务器pending、无光T1、错口T2、缺侧pending、DDM越限T3、错轨T2-rail、物理CRC增长T4、批准配置不符T6。每组保留脱敏NOS原始输出和平台判定，不自填PASS冒充设备观察。T4计数需同设备/同源/同epoch且满足参数窗口的前后样本；T6/rail需批准意图。
- [ ] 线缆病例保存`POST /api/v1/projects/{project}/fabric/collections`响应原文；诊断病例保存`POST /api/v1/projects/{project}/fabric/devices/{switch}/diagnostics/record`响应原文（精确路由按OpenAPI），包含固定规则版本、阈值与意图快照。发现工单必须保留source=fabric、稳定物理引用和原始观察。
- [ ] 对一个T1/T2交换机互联案例按分派→处置登记→范围确认→定向复验→NE/DL确认关闭执行。保存`GET /api/v1/issues/{issue}`、完整`GET /api/v1/issues/{issue}/verifications?page=1&page_size=100`和`GET /api/v1/projects/{project}/executions/{execution}`原文；范围摘要、request/evidence/command/fence、设备来源、关闭版本和有效期应一致。完整页不能截取成功记录。
- [ ] 所有文件相对manifest路径，逐文件计算真实SHA256；检查器复用有界/no-follow读取，不连接设备、不执行命令、不自动更新ACCEPTANCE。输出READY_FOR_REVIEW也保留hardware_acceptance=UNVERIFIED、完整H-17/G2=NOT_RUN，需所有者另审日志真实性与故障注入。
- [ ] T3/T4/T6完整关闭仍需对应连通性/配置重验消费者；未交付时保留BLOCKED，不能用T1/T2的LLDP通过代替。服务器G2/NP-3、PortModel关系和完整8+2真机签署按后续实施依赖补齐。


## 步骤 E 扩展地址规划材料（软件与真机分开记录）

- [ ] 保存客户批准地址池/用途/VRF/分配前缀/保留区与显式稳定对象索引；管理地址引用原管理规划，不由平台臆造网络方案。
- [ ] 保存预览hash、微调原因、规划ID与独立审核人/原因；核对当前BOM物理端口、线缆对端、两端子网及无容量/保留/VRF冲突。IPv4与IPv6样本分别记录。
- [ ] 保存模板验证和准备阶段address-planning门控、实际NOS归一化映射/原始输出及回读摘要。协议桩network_addresses字段的PASS只属于模拟证据，不能代替NOS正确性或真机签署。
- [ ] 证明新批准规划使旧测试/PushJob事实失效；未审核草稿不改变当前批准事实。拒绝自批、错误角色、跨项目和过期hash，保留原始响应。

本批用例/材料尚未执行，不据此提升F-0.8/F-0.9或H-17状态。
