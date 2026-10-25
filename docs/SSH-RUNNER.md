# 受限服务器 SSH 执行

实施计划 A3 / WP-1.4→1.5、PRD FR-1.2/1.10/1.11、ARCHITECTURE ADR-01/03/06/13 的当前增量。复用 Execution、outbox、TaskAttempt、PostgreSQL 资源租约、fencing 和回执对账，不另建队列。普通查询支持 `host.identity`（主机名/OS/架构）和 `host.wait`（有界等待）；独立批准的首次连接验证只发送只读 `trust.verify`。不执行用户 shell 或安装软件。

## 操作员配置

默认不配置 `KEEL_SSH_RUNNER_FILE`，SSH 关闭。只读查询无需介质；脚本执行、完整bootstrap/repair、目标公钥注入/轮换/删除、Agent 和目标缓存尚未交付，不借查询例外放行它们。原有 mock 互信动作不证明真实首连。

配置 JSON 的 `targets` 是最多128个显式项目/机器绑定，每项为：

| 字段 | 约束 |
|---|---|
| project_id / machine_id | 实际项目与机器32位小写十六进制ID，必须已存在项目资产关联 |
| address | 明确 IP:port，IPv6 使用方括号；不解析 DNS，不从任务接收地址 |
| user | 本步固定为非特权 `keel-ctrl`；不支持 root 作业 |
| host_key | 经独立可信渠道取得的 OpenSSH SHA256 主机密钥指纹；禁止自动接受未知指纹 |
| credential_ref | 项目/rail 域密钥的操作员引用名，1～64字符 |
| credential_version / credential_expires_at | 普通长期域配置可省略；受限首连验证必填版本与覆盖授权窗口的RFC3339到期时间，消费后不重用该目标的同一引用版本 |
| private_key_file | 文件提供者使用的受控 Ed25519 PKCS8/OpenSSH 私钥文件路径；不含密码，不落数据库；vault 提供者必须为空 |
| credential_provider | 省略沿用文件提供者；`vault` 通过独立主密钥解密数据库中精确项目/引用/版本的域密钥，必须同时配置版本和到期时间 |

配置与私钥必须为当前服务用户拥有、组及其他用户无权限的普通文件（推荐600/400）；拒绝最终符号链接、FIFO、目录和过大文件。配置文件≤128KiB，私钥≤64KiB。配置错误使启动失败，不静默退回 sim。API 与 worker 使用同一配置；API需要凭据查询失联任务的真实回执。

Compose 将仓库外 `KEEL_SSH_RUNNER_DIRECTORY` 只读挂载到 `/run/keel/ssh`，默认只挂载无密钥的禁用目录。启用时使用API/worker实际UID65532拥有的700私有目录，设置容器内 `KEEL_SSH_RUNNER_FILE=/run/keel/ssh/targets.json`，其中私钥路径也指向该只读目录。不要将真实配置、密钥或IP清单放入Git。worker profile仍须操作员显式启用；本次发布不启用生产worker或SSH目标。

## 目标 helper 与回执

首次信任验证须部署支持只读 `trust.verify` 的同版helper；旧helper拒绝未知动作时保留unknown，不退回mock或自动重放。

目标由既有运维部署流程安装对应linux/amd64或linux/arm64的 `keel-ssh-runner` 到 `/usr/local/libexec/keel-ssh-runner`，程序本身应由管理员管理，不可由任务替换。`keel-ctrl` 账号需已有正确公钥、禁止提权；此处不自动创建/修改客户账号或authorized_keys。helper拒绝以UID0运行。准备该用户拥有的700 `/var/lib/keel-ssh/receipts`；运维可用 `KEEL_SSH_RECEIPT_DIRECTORY` 指定其私有目录，不能使用共享/tmp或可写公共目录。

推荐为域公钥使用 forced-command 指向上述 helper，并禁止端口转发、代理转发、X11和PTY；该密钥只能运行这个受限协议。控制面发送的SSH exec字符串是常量，JSON只经stdin传入，没有shell拼接。helper按次运行，无监听、常驻心跳或注册机制，尚不等于keel-agent。

`TaskCommand` 协议版本为 `keel.task/v1`，包含 project_id、machine_id、command_id、attempt_id、fencing_token、operation、timeout_ms、wait_ms。命令/尝试身份和fencing来自现有claim。超时100～30000ms，等待0～30000ms，identity要求wait_ms=0。请求action仅run/query/cancel，完整输入≤4096字节，输出≤16KiB。Agent将来实现同一命令/回执语义；当前不宣称双通道等价已验证。

每命令独立0700目录，以flock保护启动判定。`accepted` 在工作前持久化，随后 `running` 和终态以600临时文件、fsync、原子rename及目录fsync更新。run遇到已有相同身份只返回原记录，身份/参数/fencing变化拒绝；即使只剩accepted/running也不重新启动。query只查记录。cancel写协作取消标记，等待原操作持久化cancelled；传输关闭本身不是取消证明。helper崩溃留下的非终态没有自动重放/清除机制，应保持unknown，后续恢复能力单独实施。

SSH连接按项目/机器及配置固定复用，最多128个绑定；握手最多5秒，单请求最多35秒、无自动run重试。目标配置摘要包含地址、账号、可信主机密钥和公钥身份，快照只保存主机密钥与摘要，不暴露地址/私钥路径。操作员更换绑定、密钥或资产版本后，旧待派发任务阻断。普通SSH没有设备级强fencing，本地flock仅补充防重；跨通道互斥依赖权威数据库租约。

## API 与权限

- `POST /api/v1/projects/{id}/ssh-executions`：Admin/DE/DL/SRE，严格JSON与现有Origin/CSRF/Idempotency-Key；字段machine_ids（1～32个唯一ID）、operation、可选timeout_ms（默认5000）和wait_ms。所有资产必须provisioned、互信established，数据库主机指纹与操作员配置完全相符。先原子冻结Flow/资产/绑定/参数及attempt/outbox/audit，再由worker派发。
- 现有执行列表、五Tab视图、控制及回执下载可读取SSH任务；界面明确显示SSH只读边界，不显示“未连接设备”的sim说明。查询任务使用稳定 `device:machine:<id>` 与 `ssh:machine:<id>` 共享租约，独占设备任务仍冲突。派发及打开SSH前重新核验当前提交者权限、资产、绑定与互信，失败不借sim执行。
- `POST /api/v1/projects/{id}/executions/{executionId}/attempts/{attemptId}/ssh-receipt`：Admin/DL/SRE，正文expected_version。仅查询现有命令，验证终态身份后复用原授权对账事务；相同key重试复用已存证明。无终态仍409，锁不释放；该接口不重新run。可用真实回执恢复时不要求人工编造结果。

Pause停止新派发，当前只读步骤安全完成；Kill需要cancelled/其他实际终态回执，拿不到则保留unknown/reconciling租约。断连先有界查询或取消，不把网络错误伪造成failed/killed。worker租约过期后迟到回执只能走授权对账，不直接释放资源或自动新建尝试。

当前提交入口为正式API；页面复用已有执行详情与控制，尚未新增普通SSH任务启动向导；资产页已有独立批准的信任验证入口。只读系统Flow不允许借普通sim编辑/重跑接口执行脚本。WP-1.4/1.5、P1仍partial，服务器脚本/Agent/PXE/维护态完整治理及真机PoC另验。

## 验证

先构建前端，再运行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB python3 scripts/check-ssh-integration.py`。当前overlay仅隔离用户原有026/fabric草稿。脚本确认现有测试容器disposable标签，只新建/清理随机 `keel_test_ssh_*` 数据库；Go测试拥有回环SSH/HTTP listener、一次性密钥和合成资产，浏览器真实登录/查看输出，独立CLI进程证明回执跨进程保留。密钥、会话和临时数据库结束清理，证据保存在私有目录。CI使用独立keel_test_ssh数据库，与其他包重建的测试库分开。

测试覆盖真实SSH握手/固定主机密钥、错误主机密钥/旧fence、重复派发、取消证明、丢响应查询恢复、实际PG事务/锁/权限、HTTP匿名/Viewer/非成员/CSRF拒绝、禁用配置、撤销互信/角色和浏览器视图。资产provisioned/互信是显式测试fixture，不是PXE或真实互信成功证据；协议连接与硬件验收分别计证。042仅扩展既有mode/kind/runner约束，部署前备份并恢复演练；旧schema38镜像不能直接用于schema39，保留数据修复前进。

## 独立批准的首次连接验证

新信任验证请求绑定单台已provisioned资产的版本、经人工核验的host key、操作员配置摘要，以及凭据引用版本/有效期。配置新增可选 `credential_version` 与 RFC3339 `credential_expires_at`；申请一次性验证时两者必填且凭据有效期覆盖批准窗口。有效期统一UTC微秒；到期拒绝调用、关闭该目标连接，在途失联仍为unknown。普通长期域配置兼容省略有效期。

`/api/v1/projects/{id}/ssh-trust-requests` 提供创建/分页列表，`/{requestId}/approve`、`/revoke`、`/execute` 使用资源版本、CSRF和幂等键。Admin/DL/SRE创建和执行，另一位Admin/DL独立批准；请求有效期5～60分钟。执行消费该目标的凭据引用版本一次，复用既有Execution/outbox/租约，仅向已经安装的helper发送只读 `trust.verify`。真实终态回执、当前授权、资产/指纹/绑定与时间窗口均有效后，才在同一事务更新机器信任、日志和请求结果。

043增加 `ssh_trust_source=legacy|sim|ssh`：旧记录默认legacy，mock投影标sim，只有实际验证回执标ssh。普通SSH拒绝legacy/sim，即使历史状态为established；这类状态可经独立批准的repair补真实验证。revoked不自动恢复；已完成真实验证的established不借repair执行其他动作。资产页区分来源，旧mock动作不能修改当前或历史真实目标。

本增量不安装公钥/helper、不改变客户口令、不实现目标密钥撤销或Vault销毁；一次性指验证授权消费，回执查询不重新执行。授权/凭据到期或绑定轮换后，unknown仍保持隔离，换凭据查询旧命令的恢复治理尚待开发。043要求schema40；备份恢复演练、完整集成/CI、提交和实际运行版本以HANDOFF最新记录为准。

## 加密项目域密钥提供者

WP-1.6为WP-1.5提供的运行时凭据前置：平台生成项目域Ed25519密钥，AES-256-GCM加密后存入`ssh_domain_keys`；主密钥是独立于数据库的32随机字节文件。项目、引用、版本、公钥和UTC微秒到期时间共同作为AEAD附加认证数据，移动或篡改记录不能解密。每版新生成独立密钥，引用/版本不可覆盖或复用，创建和首次撤销与脱敏CLI操作员审计同事务提交。数据库备份必须与主密钥分开保管；遗失主密钥不能从数据库恢复私钥，替换主密钥也不会自动重加密旧记录。

以下命令由授权运维在服务器执行。加载既有owner配置仅用于管理操作，不能把owner DSN提供给API/worker。示例中的项目ID、路径和到期时间需替换为实际值；命令输出只有公开元数据：

```sh
keel-admin credential-master-keygen --output /private/keel/ssh/master.key
export KEEL_CREDENTIAL_MASTER_KEY_FILE=/private/keel/ssh/master.key
keel-admin ssh-key-create --project PROJECT_ID --ref keel-ctrl-project --version v1 --expires-at RFC3339_TIME
keel-admin ssh-key-list --project PROJECT_ID --limit 50 --offset 0
keel-admin ssh-key-revoke --project PROJECT_ID --id KEY_ID --expected-version 1 --reason '撤销原因'
```

主密钥创建使用0600与排他创建，不覆盖已有文件；提供者拒绝相对路径、最终符号链接、FIFO、非当前UID拥有、其他用户可读或长度错误的文件。创建需有效期大于五分钟，按现有项目状态拒绝已交付/归档项目新增密钥；撤销仍可进行。撤销使用状态版本；同一旧版本与相同原因的精确重试返回原证据，冲突请求拒绝。公开公钥带`keel-ctrl-项目-引用-版本`注释，便于后续受控安装登记；注释本身不证明目标归属或安装成功。

目标配置使用`credential_provider: "vault"`，`private_key_file: ""`，并准确填写公开元数据中的`credential_ref`、`credential_version`与`credential_expires_at`。API/worker设置`KEEL_CREDENTIAL_MASTER_KEY_FILE`，其路径可位于原有只读`/run/keel/ssh`挂载中，文件和私有目录由容器实际UID65532拥有。运维CLI须使用有权读取该文件的同UID上下文，不能为方便而放宽文件权限或把密钥输出到终端。文件提供者保留兼容；vault配置错误或不可用绝不回退文件/sim。

提供者每次Binding、元数据读取以及run/query/cancel前重验数据库版本、撤销状态、有效期和公钥身份，包括已有连接池。凭据读取使用独立有界连接池，避免持有业务事务时自锁；数据库不可用时拒绝取钥。拒绝时关闭对应目标的池连接，新操作不会继续派发；已派发动作仍按真实回执与授权投影处理，不宣称撤销能回滚远端已执行动作。失联结果保持unknown及资源隔离，不借新凭据重放旧命令。

`GET /api/v1/projects/{id}/ssh-domain-keys`与资产页公开密钥列表仅允许当前平台Admin或项目Admin/DL/SRE，分页返回公钥、引用/版本、到期及控制面撤销证据。无私钥/密文/文件路径字段。浏览器无密钥生成、导入或撤销入口；管理动作仅服务器owner CLI。运行角色对新表只授SELECT，禁止INSERT/UPDATE/DELETE/TRUNCATE；`configure-development.py --grant`保留这一限制及原信任请求、审计和迁移证据限制。

本功能不安装目标公钥、不删除authorized_keys条目、不修改客户密码，也不实现目标双钥轮换或Vault密文销毁。控制面已撤销与“新SSH连接被目标拒绝”的撤销证明是两个不同事实；S-3与完整bootstrap/repair仍需后续完成。新增044迁移，部署必须按DEPLOYMENT备份、隔离恢复演练并授新表只读权限；旧schema40镜像不能直接用于新增迁移后的库。
