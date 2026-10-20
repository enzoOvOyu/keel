# Keel

**Knowledge-led Execution, Enforcement & Lifecycle**

自动交付部署平台。品牌字标使用 **Keel**，仓库、包名与服务标识使用 `keel`。设计基线覆盖 17 个功能域 + **网络底座前置域（FR-0，`keel-fabric`）**；**P1 已全部闭环**（项目/登录隔离、执行恢复与锁、2000 目标扇出、介质校验、型号不可变版本、互信全链、PoC 执行包 #P1 产出），**P2 验机链 13 个工作包全部闭环**（后端 + 前端主链路），**P3 配置链已交付 ConfigSet 引擎、四类批量配置渲染器与 RoCE 五层连通测试服务**（WP-3.1/WP-3.2/WP-3.3），整体交付业务持续开发中。

## 当前可用能力

### 平台基础（P1，全部闭环）

- `/login`：船体龙骨与肋骨线条动画、中文/英文、明暗主题、移动布局与减少动画偏好。
- 本地账号登录、预登录 CSRF、HttpOnly 服务端会话、退出撤销、账号/IP 失败限流。没有默认口令或公开注册；首个管理员通过服务器 CLI 初始化。
- `/projects`：真实授权项目列表与空状态；平台管理员可创建项目。六种项目角色与项目内权限隔离。
- **设备型号库（FR-14）**：型号/版本发布不可变（唯一发布路径）、扫描导入只产草稿（FR-14.4，粘贴采集清单 → 草稿审阅 → 预填发布）、幂等三段判定、介质生命周期与自审批拦截。
- **BOM 导入（FR-2.1）**：两段式（只读预检分类冲突 → 单事务提交）；**导入包支持目录/压缩包（`manifest.json` + 逐实体 CSV，一次提交双域物料）与单文件宽表（每行一个 MAC，按 SN 折叠）两种形态**；服务器与网络物料落同一张 `bom_item` 表；导入即分配全局 Machine/Component/PhysicalPort 身份 + 项目内 ProjectAsset 绑定型号；文件版本幂等；问题清单按文件+行号+字段三级定位导出 CSV（PRD 附录 A.5–A.8）。
- **SSH 互信状态机（FR-1.2 mock 通道）**：unverified→established→revoked 单向 + 指纹粘性；bootstrap 受控首登、verify 核对指纹、repair 审计恢复、revoke 退出；资产页互信列与批量动作。
- **PoC 执行包 #P1（H-1）**：`poc/` 自检脚本 + 真机驱动 + 环境清单 + 观察项模板（真机执行由工程所有者回填，GA 门槛）。

### 验机链（P2 全线闭环：13 个工作包，后端 + 前端主链路）

- **Redfish 采集（FR-3.2/3.5，WP-2.1/2.2）**：L1/L2/L3 分级独立采集，revision 成功才递增，失败五分类可重试（auth_failed/timeout/version_unsupported/partial/credential_expired），freshness 读时计算（fresh/aging/stale），**never_collected=unknown 永不等于门禁 PASS**；资产页采集矩阵（状态/版本/保鲜度 + 一键采集）。
- **门禁证据（FR-4 契约，WP-2.3）**：证据不可变历史（append-only），G1 判定（新鲜 L1 采集 + MAC 对 BOM），豁免四要素（审批人/理由/范围/到期），过期=blocked 永不 pass，G2-G6 未实现即诚实拒绝。
- **LLDP 线序（FR-4.2，WP-2.4）**：快照版本化，T1-T5 差异分类引擎（T5 整机升级不被逐口噪声淹没；T2 仅当 BOM 携带对端期望时判错插），差异历史永删，定向重验自动关闭未复现差异。
- **工单闭环（FR-4.7，WP-2.7）**：严格配对状态机（open→assigned→in_repair→verify→closed，reopen 仅从 closed），版本乐观锁，九种处置枚举 + 新旧实物标签核对，close 须绑定差异已被复测解决，全事件历史留痕。
- **门禁看板（DS §4.4，WP-2.8）**：G1-G6 六卡横向布局（G1 读最新证据结论，G2-G6 如实标注未实现/无证据，**绝不伪造 PASS**），通过率环 + 豁免弹窗（理由必填 + 范围 + 有效期）；KPI 行（互信三态/开放工单/未解决线序差异）。
- **批量作业平台（FR-1.4 深化）**：失败熔断（阈值默认 5%，分母为冻结目标总数，超阈自动 PAUSED + 事件账目可审计）、单节点/仅失败节点重跑（attempt 链接续、血缘留痕、三类守卫）。
- **映射版本 G3（FR-3.4.4）**：发布即不可变（唯一写路径），守卫三件套（G1 证据覆盖 + 零未解决线序差异 + 每 BOM 端口恰好覆盖一次）；换件追加序号引用前版，永不覆盖；物理端口 G3 前可引用。
- **端口面板（FR-3.4.5）**：围绕稳定 PhysicalPort ID 聚合（规划接口名/链路态带来源时间戳/DOM/LLDP 对端/告警），unknown 保留来源绝不伪造。
- **mon-svc（FR-13）**：阶段档位表逐档精确（M1-M4/maintenance），变化项 ingest（带宽 O(变化数)），flapping 告警（10 分钟窗 3 次且不重复开），maintenance 拒收。
- **PCIe 拓扑（FR-4.8）**：四维判定（槽位路径/NUMA/NVLink 行语义/LnkSta 达标），预期来自型号声明不反推，T7 两次失败只升级绝不自动 RMA。
- **PoC 执行包 #P2**：H-2~H-5/H-13 四件套（自检绿；真机回填是 GA 门槛）。
- **T7 工单闭环（FR-4.8.3→FR-4.7）**：拓扑差异进同一工单环（source=topology），close 门禁按证据类型分流。
- **ServerDetailDrawer + 双状态列（DS §4.12）**：五标签抽屉（台账/映射/网口面板/PCIe 拓扑/监控），映射+网络双徽标三重编码可点直达，网络列独立于映射存在（SSOT），Esc 关闭保列表上下文。

### 配置链（P3 已交付：ConfigSet 引擎 + 四类渲染器 + 角色化渲染）

- **ConfigSet 引擎（FR-5.1，WP-3.1）**：配置即数据（键/schema/作用域/模板）；`{{param.<点号路径>}}` 占位符从冻结快照解析，缺参拒绝渲染绝不半成品；版本化审批 + 自审批拦截（草稿→approved 不可变，唯一发布路径）；按机器渲染预览与最近一次 apply 的行级 diff；G3 映射前置只门禁 apply，预览永远只读；权限先于校验。
- **四类批量配置渲染器（FR-5.2-5.5，WP-3.2）**：GPU 网络配置按映射库与逻辑角色规划每 NIC 的 IP/路由/策略；网卡分割由机型能力矩阵生成（速率/总带宽/PF/VF 分别登记 + 前后 diff，超能力拒绝）；GPU-NIC 绑定渲染 NCCL_NETDEVS_POLICY 与 NUMA pinning（跨 NUMA 预览警告、apply 硬拒）；RoCE 导入把 DCQCN/PFC/ETS/MTU/拥塞作为 ConfigSet 键版本化管理（导入前 diff、导入后读回验证并记录回执）。
- **统一读回契约（FR-5.2）**：四类渲染器共享同一抽检读回校验——读回样本与渲染计划逐键比对，全匹配才 verified；缺样本保持 pending，不一致样本在 apply 行记录 mismatch，绝不把静默当通过。
- **角色化渲染（FR-5.9，WP-3.2）**：模板只引用逻辑角色（management/storage/compute/oob）；机型声明每槽位默认角色，项目可按槽位覆盖（二期 x710→unused 由覆盖吸收，机型与模板零改动）；仅 enabled 且 required 的角色要求零缺失绑定，禁用角色允许空绑，compute 角色不可关闭；角色方案管理页展示覆盖度与缺失影响。

### 连通测试（P3：RoCE 五层连通测试服务，WP-3.3）

- **五层连通测试（FR-11，WP-3.3）**：L1–L5 五层共享同一 per-NIC-pair 结果模型（ping/RDMA/集合通信/NCCL 分级）；FR-11.2 预检四查（G4 门禁、端口映射覆盖、活跃告警、平台握手 freshness>1h 拒收）；计划分 quick（`nettest.required_path_coverage=0.25`，matrix-sample）与 full（1.0）两档，端口集合来自最新发布映射版本。
- **失败分类与工单闭环（FR-11.4/11.5→FR-4.7）**：T1–T6 分类（T6 配置类：ip_conflict/vlan_unreachable/pfc_misconfig/mtu_mismatch），失败对自动开 T 工单进同一工单环（source=nettest，repair 注册后方可请求验证），无法分类的失败如实保留 unclassified 绝不伪造归因。
- **覆盖率核算与 G5 证据**：逐层按 `ceil(计划×required)` 判定 complete，任一层实测<计划即 sampled；G5 证据仅在运行结论（finish）时落库，未完成运行不出证据；flapping 告警阻止预检、issue-bound waiver 可放行。
- **NCCL 准入（FR-11.6）**：net_verified = 最新 G5 证据 PASS/WAIVED 且未过期；准入面板按机器逐台展示，失败层点亮对应端口。
- **看板与端口面板联动（DS §4.4/§4.12）**：G5 门禁卡展示 L1–L5 分层通过率 mini bar（读真实证据，无证据=no_evidence 绝不伪造 PASS）；ServerDetailDrawer 网口面板新增连通性列，失败端口高亮并带失败层提示。

### 验证规模

隔离套件 **239 组检查全绿**（真实 PostgreSQL HTTP 检查 + 真实 Chromium 浏览器验收），覆盖每个语义约束：不可变发布、幂等导入、状态机边界、权限隔离、项目隔离、失败分类、渲染读回一致性、证据诚实性。

`/healthz` 只表示进程存活。`/api/v1` 返回 `phase: p1-foundation`；`business_ready: false` 表示整体交付业务尚未就绪。

### 尚未实现（诚实声明）

ProxyDHCP/iPXE/cloud-init/首启回调（真实通道）、keel-ctrl 密钥托管与 authorized_keys 注入、gRPC Agent/mTLS、G2-G6 判定引擎（G5 证据判定已随 WP-3.3 落地，G2/G3/G4/G6 仍未实现）、SSE 实时推送、ServerLampPanel 状态灯（需 FR-16 BMC 带外）、抽屉拉宽/翻页导航、趋势图、CMDB 联邦视图、NCCL 测试链（S1–S9，消费 FR-11.6 准入）、配置 apply 的看门狗/回滚、FR-5.6 驱动升级链（介质库分发）——按 ADR-14 mock-first 推进，**页面与 API 对未实现能力如实标注，不虚构通过**。

## 在远端继续开发

主工作区：`ubuntu@13.158.38.7:/opt/keel`。先读 [协作约定](AGENTS.md)、[交接记录](docs/HANDOFF.md) 与 [部署说明](docs/DEPLOYMENT.md)。

```sh
cd /opt/keel
git status -sb
sh scripts/dev-session.sh
tmux attach -t keel
```

已有 `keel` 会话包含 work/api/web/logs；脚本不重启已有窗口。按 **Ctrl-b，再按 d** 分离，SSH 断开后进程继续。开发 API 使用3001，Vite使用5173，部署预览使用3000。

`make check` 包含设计检查、Go vet/race、前端类型检查和构建。真实 PostgreSQL 单测需另设 `KEEL_TEST_DATABASE_URL`；未配置时会明确跳过这部分，不能据此声称集成验证通过。隔离数据库及 HTTP/浏览器验证方式见 [部署说明](docs/DEPLOYMENT.md)。
