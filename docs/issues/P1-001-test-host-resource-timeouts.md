# P1-001：集中测试宿主资源竞争下的超时（待隔离复验）

- 状态：OPEN；2026-10-27 用户要求先登记，不以反复重跑或扩大超时参数处理。
- 范围：本地 `/opt/keel` 的软件集成测试；不能据此判定真实设备性能。
- 首次测试候选：`869ea1f`。仅记录相关性，尚未确认所有超时均由资源导致。

## 现场与影响

全量 Go race、Agent/Redis/PXE 集成批次与 lifecycle 批次执行期间，宿主 15,782 MiB 内存中可用量一度只有 376 MiB；无 swap，shared 为 10,375 MiB。本轮 execution.test 约 620 MiB，platform.test 约 261 MiB；另有其他项目与既存 tmpfs 数据，保持不动。编译/链接出现明显等待，私有测试日志保留在 `/home/ubuntu/.local/state/keel/evidence/p1-20261027/`。

待隔离复验：`TestAgentWorkerAndCLIExecutionLifecycle` 的 `agent-task-status` 子进程在 12 秒期限内未完成，输出为空；该根用例约45秒后失败。`TestAgentApprovedScriptPostgresAdmissionAndUnknown` 还出现未及时取得终态回执，需要在同一问题中保留原始结果并判别原因，不能直接归因为性能或标成PASS。

日志：`agent-batch.log` 对应 `/tmp/keel-agent-integration-dxd9l_fe/go-integration.log`；需保留其中实际用例输出、失败边界、耗时及候选版本。测试日志可能包含一次性测试数据，不提交完整私有日志或凭据。

## 处置边界

首批还有整包10分钟预算耗尽：execution包结束时`TestProvisionInstallationFirstSSHPostgres`仅运行11秒；platform包结束时`TestZTPP1LogicalBatch200Postgres`运行3分44秒，堆栈位于200条DUID预留的`MergeBootstrap136`计算，未发现锁等待证据。两者均不能据此判定单用例死锁或通过，保留待隔离复验，不直接提高整包预算。Agent浏览器子进程也在期限内被终止，虽然打印了部分成功断言，整项仍为未通过。

1. 本轮资源相关超时保留为未通过/待复验；不提高全局超时，不因宿主压力改变 unknown、回执或权限判据。
2. 后续将重型批次串行安排，Go 临时构建产物放磁盘目录；仅清理本轮明确拥有且已停止使用的临时构建目录，不停止其他项目或清理其缓存/数据库。
3. 在空闲或更宽裕的宿主上，仅复验上述失败用例，记录 CPU、MemAvailable、tmpfs、磁盘等待和PG阻塞；仍失败再按实际代码问题处理。
4. 关闭条件：原期限下目标用例通过且留有资源与来源记录，或明确定位独立逻辑错误并修复后定向通过。未达到时保持OPEN。

## 与逻辑错误分开

Agent缓存/网口读取的 `SELECT FOR SHARE in a read-only transaction` 是已定位的逻辑错误，正在修复并定向验证，不列作宿主性能问题。API契约、权限、摘要、恢复、资源锁或其他确定性失败同样不得以此issue豁免。更新部署需报告该issue和未完成的验证边界。
