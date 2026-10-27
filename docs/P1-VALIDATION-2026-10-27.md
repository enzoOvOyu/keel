# P1 集中软件验证记录（2026-10-27）

状态：**部分通过，P1尚未签署；3000未更新。** 用户要求将可能受本机性能影响的超时记入issue，保留原始结果，不反复重跑、扩大期限或立即调优。待复验项见 [P1-001](issues/P1-001-test-host-resource-timeouts.md)。

工作区 `/opt/keel`，分支 `dev/p1-network-bom`，单Agent。最新生产代码修复检查点为 `bf265ec`；后续为验收脚本、诊断与文档提交。规模为200个逻辑身份；真实硬件和企业认证端点的PoC单独留证。

| 范围 | 实际结果与边界 |
|---|---|
| 基础检查 | 设计和PoC材料单元通过；首轮全仓Go vet/race返回0。修复后对platform/execution/taskrunner追加vet与race，全部返回0；最终前端类型、UI/版本检查及Vite构建返回0。无数据库环境的基础测试不替代PG集成证据 |
| 认证/项目隔离 | 独占PG的本地登录、会话/CSRF、六角色、撤销、账户/成员/审计HTTP及登录/管理浏览器通过；OIDC与LDAP各自的真实PG、受控回环协议端点及Chromium批次返回0 |
| 执行/200逻辑设备 | 独占API+worker+PG的12组检查通过；200/200成功，8.235秒。含冻结DAG、重试/依赖、熔断、Pause/Resume/Cancel、回执丢失/隔离/不重放、权限和事件游标。该容量数据来源为sim，无硬件或介质分发字节 |
| Redis队列 | 独立16379协议fixture通过；6个ExecutionStream用例通过，覆盖真实Redis+PG调度、业务提交后ACK、去重、故障重建/隔离及队列健康模式。所属Agent整批曾失败，不能用组件通过改写整批结果 |
| PXE软件链 | 本轮定向用例完成：准备互斥、引导/文件发送、配置、写许可、首次启动/SSH、孤立派发、密钥修复、已注册结果恢复、灰度与SSH批次。最后的baseline/写盘前恢复/修复列表定向批次返回0；真实启动、写盘和设备注册由PoC验证 |
| BOM/型号 | 原子提交、稳定身份和型号约束的首轮通过项保留；JSON兼容HTTP验证未发布型号及发布后重导冻结。当前BOM浏览器6组通过：标准目录包、文件/行/字段问题导出、统一包提交、相同包防重复、同批次异摘要拒绝、CSV折叠。型号历史/引用/资产冻结绑定/compute-tray/全局型号浏览器通过 |
| SSH | 首批除域公钥安装浏览器外的协议、权限、撤权、unknown恢复等用例通过；公钥安装在独占数据库中定向通过，随后真实SSH/HTTP和CLI批次返回0。首批等待终态超时仍保留在issue，整批不记PASS |
| Agent | 身份/mTLS/任务等首轮实际通过项保留；缓存/网口只读查询修复后通过。worker CLI、脚本/制品终态及浏览器的时限相关失败仍待隔离判别，见issue |
| 介质/参数 | `check-media-artifacts-integration.py --browser-only`完整批次返回0；真实MinIO上传、摘要/版本、独立审批、Range、依赖/执行冻结、全局/项目权限、签名清单/离线包、字节损坏注入与隔离、来源/兼容矩阵/筛选及浏览器通过。合法对象读取与坏字节按预期隔离分别记录 |
| ZTP | 单目标136九阶段与身份反查、其他已完成协议/管理/身份历史用例通过；200逻辑ZTP批次因整包预算耗尽未完成，**未达到该项签署证据**，不追加追跑 |

本轮修复了API契约漂移、TFTP发送时授权、缓存/网口只读事务误加锁、BOM型号版本漏冻结、webhook和PXE事件SQL参数类型、写盘前恢复版本校验、缺失项目404，以及独立审批/受限角色/共享回收数量等夹具问题。当前BOM浏览器改用已有标准包与CSV文件入口。

证据保存在权限700目录：`/home/ubuntu/.local/state/keel/evidence/p1-20261027/`。`runs/`保留16个本轮隔离环境的日志/报告/截图，`evidence-index.json`记录文件摘要；凭据和私钥未归档到该索引或提交Git。根目录的`go-race.log`为早期混合日志，不作为通过依据；有效首轮结果为`go-race-final.log`，最终增量结果为`go-race-affected-final.log`。

主要集中入口：`check-agent-integration.py`、`check-p1-lifecycle-integration.py`、`check-ssh-integration.py`、`check-oidc-integration.py`、`check-ldap-integration.py`、`check-execution-workspace-integration.py`、`check-media-artifacts-integration.py --browser-only`、`check-model-workspace-integration.py --browser-only`。HTTP/BOM集中调度器保存在同一私有证据目录的`check-platform-workspace.py`，复用仓库现有检查脚本；失败修复仅定向复验。

Go命令使用精确排除两份原草稿的overlay、`-p=1`、`GOMEMLIMIT=768MiB`、`GOMAXPROCS=2`；首轮资源竞争后，后续临时构建产物改放磁盘。两份fabric草稿保持未跟踪且未编译/迁移。隔离测试数据库及MinIO均已按所有权清理；既有测试PG和其他项目服务保留。Issue为仓库内记录，未创建GitHub issue，本轮未推送。

部署门槛尚未满足。本轮没有迁移业务库、构建候选部署镜像、更新`.env`或推送GitHub。3000进程健康，版本仍为`6668a72c1adaeb2444b8c28a3f4db4f038db49dd`；业务库实际46条迁移，最后为`049_agent_task_connections.sql`，源码64条，最后为067，**050～067未应用**。后续更新需先处理/复验issue中的签署缺口，再执行备份/恢复验证、迁移、应用角色授权及一次候选镜像构建/部署。
