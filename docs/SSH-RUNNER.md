# 受限服务器 SSH 执行

实施计划 A3 / WP-1.4→1.5、PRD FR-1.2/1.10/1.11、ARCHITECTURE ADR-01/03/06/13 的当前增量。复用 Execution、outbox、TaskAttempt、PostgreSQL 资源租约、fencing 和回执对账，不另建队列。普通查询支持 `host.identity`（主机名/OS/架构）和 `host.wait`（有界等待）；独立批准的连接验证发送只读 `trust.verify`，可选受控公钥安装发送 `trust.install` 并通过新钥全新握手查询原回执证明。helper须预先由运维部署；批准小脚本的默认关闭源码增量见下节末尾，不接受API内联命令或以脚本绕过公钥安装审批。

## 操作员配置

默认不配置 `KEEL_SSH_RUNNER_FILE`，SSH 关闭。只读查询无需介质；目标已有非root临时登录和helper时，可经独立批准追加受限公钥并证明新钥。批准小脚本执行源码已接续；root首登、完整bootstrap/repair、完整双钥轮换/删除、完整Agent任务目录和通用目标缓存尚未交付，不借查询例外放行它们。原有 mock 互信动作不证明真实首连。

配置 JSON 的 `targets` 是最多128个显式项目/机器绑定，普通目标每项为（首装专用目标见下节）：

| 字段 | 约束 |
|---|---|
| project_id / machine_id | 实际项目与机器32位小写十六进制ID，必须已存在项目资产关联 |
| address | 明确 IP:port，IPv6 使用方括号；不解析 DNS，不从任务接收地址 |
| user | 本步固定为非特权 `keel-ctrl`；不支持 root 作业 |
| host_key | 经独立可信渠道取得的 OpenSSH SHA256 主机密钥指纹；禁止自动接受未知指纹 |
| credential_ref | 项目/rail 域密钥的操作员引用名，1～64字符 |
| credential_version / credential_expires_at | 历史未记录安装身份的普通配置可省略；已证明安装身份的普通任务必须使用精确vault版本和到期时间；受限首连验证必填版本与覆盖授权窗口的RFC3339到期时间，消费后不重用该目标的同一引用版本 |
| private_key_file | 文件提供者使用的受控 Ed25519 PKCS8/OpenSSH 私钥文件路径；不含密码，不落数据库；vault 提供者必须为空 |
| credential_provider | 省略沿用文件提供者；`vault` 通过独立主密钥解密数据库中精确项目/引用/版本的域密钥，必须同时配置版本和到期时间 |

配置与私钥必须为当前服务用户拥有、组及其他用户无权限的普通文件（推荐600/400）；拒绝最终符号链接、FIFO、目录和过大文件。配置文件≤128KiB，私钥≤64KiB。配置错误使启动失败，不静默退回 sim。API 与 worker 使用同一配置；API需要凭据查询失联任务的真实回执。

Compose 将仓库外 `KEEL_SSH_RUNNER_DIRECTORY` 只读挂载到 `/run/keel/ssh`，默认只挂载无密钥的禁用目录。启用时使用API/worker实际UID65532拥有的700私有目录，设置容器内 `KEEL_SSH_RUNNER_FILE=/run/keel/ssh/targets.json`，其中私钥路径也指向该只读目录。不要将真实配置、密钥或IP清单放入Git。worker profile仍须操作员显式启用；本次发布不启用生产worker或SSH目标。

## 首装专用allowlist（当前源码）

尚无旧主机指纹或旧登录凭据的目标可显式配置`provision_only: true`，仅允许`project_id`、`machine_id`、明确`address`和固定非root `user: keel-ctrl`。`host_key`、所有credential字段及`private_key_file`必须省略或为空（expiry必须省略/null），且必须已配置vault提供者；加载不解析/解密任何尚未选定的密钥，不需要伪造旧pin或占位私钥。已有普通配置省略此字段，仍沿用原严格pin/credential验证和配置摘要。

此入口只预留网络坐标。装机当前批准请求与认证首启报告绑定的新host pin、同peer/批准网段、精确项目域公钥/版本/到期时间仍必须由控制面核验；每次握手从vault重新取当前凭据，错误pin/peer、过期、撤销和关闭runner均拒绝。普通Binding、CredentialMetadata、run/query/cancel、旧凭据安装/普通域凭据检查一律拒绝此目标；HasTarget仍为true，不能借mock回退。

首次核验和显式原回执query使用私有临时endpoint，不修改operator配置，也不把连接提升为普通任务池。验证成功后若需普通SSH任务，操作员须独立核对并更新为有实际新host pin和已证明vault身份的普通配置。已registered安装的进度面板单独显示当前普通配置状态：当前资产/真实互信及未撤销域钥均有效后，再比对普通配置的实际pin、vault引用/版本/有效期和公钥；provision-only或缺配置显示需操作员更新。只读匹配不Resolve私钥或连接目标，也不证明worker/网络可用，实际任务仍按原准入执行。

这里只是允许无旧pin的首装前置，不能代替首启认证、真正SSH回执或P1验收；新增配置/回环用例仅源码，尚未执行。

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

资产页提供普通只读SSH启动入口：当前项目Admin/DE/DL/SRE显式选择当前页已装机且source=ssh的可信目标（最多32台），仅允许host.identity/host.wait，超时100～30000ms且等待短于超时。提交成功进入既有Execution详情与控制；网络响应不明时相同参数在当前表单重试复用同一幂等键，不扩大目标或自动重派发。401/403/404关闭过期表单，凭据未切为已证明域钥或环境未启用SSH时显示操作员提示。信任安装仍走独立批准入口。只读系统Flow不允许借普通sim编辑/重跑接口执行脚本。WP-1.4/1.5、P1仍partial，服务器脚本/Agent/PXE/维护态完整治理及真机PoC另验。

## 验证

先构建前端，再运行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB python3 scripts/check-ssh-integration.py`。当前overlay仅隔离用户原有026/fabric草稿。脚本确认现有测试容器disposable标签，只新建/清理随机 `keel_test_ssh_*` 数据库；Go测试拥有回环SSH/HTTP listener、一次性密钥和合成资产，浏览器真实登录/查看输出，独立CLI进程证明回执跨进程保留。密钥、会话和临时数据库结束清理，证据保存在私有目录。CI使用独立keel_test_ssh数据库，与其他包重建的测试库分开。

测试覆盖真实SSH握手/固定主机密钥、错误主机密钥/旧fence、重复派发、取消证明、丢响应查询恢复、实际PG事务/锁/权限、HTTP匿名/Viewer/非成员/CSRF拒绝、禁用配置、撤销互信/角色和浏览器视图。资产provisioned/互信是显式测试fixture，不是PXE或真实互信成功证据；协议连接与硬件验收分别计证。042仅扩展既有mode/kind/runner约束，部署前备份并恢复演练；旧schema38镜像不能直接用于schema39，保留数据修复前进。

## 独立批准的首次连接验证

新信任验证请求绑定单台已provisioned资产的版本、经人工核验的host key、操作员配置摘要，以及凭据引用版本/有效期。配置新增可选 `credential_version` 与 RFC3339 `credential_expires_at`；申请一次性验证时两者必填且凭据有效期覆盖批准窗口。有效期统一UTC微秒；到期拒绝调用、关闭该目标连接，在途失联仍为unknown。普通长期域配置兼容省略有效期。

`/api/v1/projects/{id}/ssh-trust-requests` 提供创建/分页列表，`/{requestId}/approve`、`/revoke`、`/execute` 使用资源版本、CSRF和幂等键。Admin/DL/SRE创建和执行，另一位Admin/DL独立批准；请求有效期5～60分钟。执行消费该目标的凭据引用版本一次，复用既有Execution/outbox/租约，仅向已经安装的helper发送只读 `trust.verify`。真实终态回执、当前授权、资产/指纹/绑定与时间窗口均有效后，才在同一事务更新机器信任、日志和请求结果。

043增加 `ssh_trust_source=legacy|sim|ssh`：旧记录默认legacy，mock投影标sim，只有实际验证回执标ssh。普通SSH拒绝legacy/sim，即使历史状态为established；这类状态可经独立批准的repair补真实验证。revoked不自动恢复；已完成真实验证的established不借repair执行其他动作。资产页区分来源，旧mock动作不能修改当前或历史真实目标。

本节原只读增量不安装公钥/helper、不改变客户口令、不实现目标密钥撤销或Vault销毁；一次性指验证授权消费，回执查询不重新执行。授权/凭据到期或绑定轮换后，unknown仍保持隔离，换凭据查询旧命令的恢复治理尚待开发。043要求schema40；备份恢复演练、完整集成/CI、提交和实际运行版本以HANDOFF最新记录为准。

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

本节凭据提供者本身不安装目标公钥、不删除authorized_keys条目、不修改客户密码，也不实现目标双钥轮换或Vault密文销毁。控制面已撤销与“新SSH连接被目标拒绝”的撤销证明是两个不同事实；S-3与完整bootstrap/repair仍需后续完成。新增044迁移，部署必须按DEPLOYMENT备份、隔离恢复演练并授新表只读权限；旧schema40镜像不能直接用于新增迁移后的库。


## 受控公钥安装与新钥首连证明

在上述同一请求创建接口可选传入 `install_key_id`，只能选择本项目未撤销、有效期覆盖审批窗口的域密钥。创建时冻结公开身份，批准、执行前及最终投影重新核验。资产页展示精确公钥与版本；另一位Admin/DL独立批准后，由原Execution/attempt/outbox/资源租约执行内部 `trust.install`。不选择密钥仍为原只读验证。普通SSH创建接口不接受安装操作或任意公钥。

先决条件是人工核验目标host key、运维配置的临时非root登录凭据，以及已安装的受限helper；目标账号和helper不会由本功能创建。helper默认只在当前账号 `.ssh/authorized_keys` 追加 `restrict,command="/usr/local/libexec/keel-ssh-runner"` 受限公钥；运维可显式配置 `KEEL_SSH_AUTHORIZED_KEYS_FILE`，但必须是绝对路径且文件名为authorized_keys。目录须当前UID所有且0700；文件须当前UID所有、普通单链接、无组/其他写权限。拒绝symlink、hardlink、客户已有同公钥及其他目标登记，不认领客户密钥。独立flock与持久归属记录使相同命令幂等；原文件通过同inode追加保留客户内容，不用旧快照覆盖并发追加。部分写入、文件替换、超时或持久化不确定保持unknown。

安装回执本身不建立真实互信。控制面必须以新域密钥另建实际SSH连接，在相同host pin/账号/地址上只查询原命令，核对同一不可变终态回执，且审批仍有效，才能原子投影 `ssh_trust_source=ssh`。不复用旧登录连接来证明新钥。新钥拒绝、断连或查询不一致保留unknown和资源锁；后续回执获取也必须完成新钥查询，绝不重新run安装。最终写信任前再次锁定并检查域密钥；证明后撤销仍阻止建立信任，不声称能回滚已发生的远端追加。

首次安装后，真实证明会把新钥公开身份持久绑定到机器。普通SSH准入/派发及既有只读repair批准/证明投影都强制要求运维将目标配置切换为该vault精确版本；仍用临时文件凭据、复制相同引用标签或使用其他版本均拒绝。运行时重新核验当前绑定及域钥撤销/到期，排队期间撤销不得派发。不自动改写目标配置或移除临时凭据。045冻结安装快照，schema41→42必须先备份及隔离恢复演练，保留既有运行角色授权，无新增运行时写密钥权限。测试使用真实回环SSH/PG与受控文件夹，覆盖新钥首次握手、拒绝后原回执恢复、撤销竞争、客户文件保持及双账号审批浏览器；不属于真机PoC。

本步不涉及root密码首登、客户口令、PXE/cloud-init、账号/helper安装、旧钥删除、完整双钥轮换、目标撤销证明或Vault销毁，WP-1.5/1.7、S-3和P1仍partial。


## 已证明域钥的普通任务准入

046新增 `Machine.ssh_domain_credential`，仅新钥实际首连证明成功时写公开身份；旧资产默认null，不把旧标签或配置补造为安装证明。已记录身份不能清空，后续只读repair保持原绑定；新安装修复经原独立批准/新钥证明后可更新身份，仍不删除目标旧钥。机器被重新分配到不同项目时旧项目域钥不能用于新项目。

普通SSH任务创建和派发均读取当前持久绑定，锁定并检查密钥项目/版本/公钥/期限/撤销，再核对运维正常目标配置确实选择该vault提供者及精确版本/公钥。缺配置返回 `SSH_DOMAIN_BINDING_REQUIRED`，不借临时凭据或sim继续；配置切换由运维显式完成，禁止在证明未完成/unknown期间提前改配置，否则旧命令绑定对账仍会拒绝。legacy纯只读首次证明仍沿用原有受控边界，不能当新域钥安装证据。

046是schema42→43，先备份及隔离恢复演练，保持原业务计数、信任/指纹/来源和受限角色权限；旧schema42镜像不可直接回切。完整S-3/轮换/目标撤销证明与P1仍未闭环。


只读启动浏览器验证 `scripts/check-ssh-launch-browser.mjs` 由真实PG/HTTP/回环SSH fixture驱动，先实际安装/证明新域钥并显式切换vault，再以项目DE登录提交identity和wait。第一次POST实际创建后明确注入丢响应，重试必须返回原执行；页面403是明确故障注入，Viewer隐藏来自真实权限。中英390宽及目标长标识换行纳入检查。CI使用单独keel_test_ssh_ui库运行同一用例；不启用生产SSH。

已安装域钥只读repair的真实回归见 `internal/taskrunner/domain_repair_integration_test.go`：覆盖临时凭据标签伪装在创建/批准/执行阶段的拒绝、派发前配置变化零远端工作、精确vault修复与远端终态后撤销拒绝信任投影，所有路径保留原公开域身份。该证明不覆盖目标旧钥删除或完整轮换。


## 批准的小脚本执行（2026-10-26，源码未验证）

WP-1.5 / FR-1.2、FR-1.3、FR-1.8增加固定脚本子集，复用真实SSH域身份、共享Execution/attempt/fence/outbox和回执。API、worker及目标专用helper必须分别显式设置`KEEL_SSH_SCRIPTS_ENABLED=true`；默认false，未修改本地实际配置或运行服务。Compose仅透传默认关闭开关。目标helper需由运维在受控启动环境设置，不通过SSH请求或用户参数设置。

### 介质和准入

脚本以既有介质库的`tool`登记，`arch=noarch`、`os_scope=linux`、`hw_scope`为空；`deps.task_script`明确声明`version=keel.task-script/v1`及`interpreter=shell|python`，独立于上传摘要或来源URL的`manifest_ref`，实际文件1～8192字节并经独立复核批准。当前子集不接依赖安装、兼容矩阵求值或OS boot角色，具有这些条件的介质拒绝执行。脚本解释器来自批准元数据，不能由HTTP指定路径/参数、提交正文或注入环境变量。版本/名称/精确对象版本/SHA256/大小/批准者/审批时间随任务冻结，正文只在批准存储中取用，不进入execution快照或审计。

新增项目作用域`GET /task-scripts`分页列出符合该子集的项目/全局批准版本及开关状态；`POST /ssh-script-executions`只接`machine_ids`（显式1～32台）、`media_id`及100～30000ms超时。当前Admin/DE/DL/SRE、provisioned资产、真实established SSH与精确已证明域钥/host pin同时成立；普通只读`/ssh-executions`不接受脚本。创建按完整选择幂等，使用非幂等、retry=0任务；普通sim Flow不能嵌入此真实脚本，通用重跑仍仅sim。

### 派发、停止与未知结果

设备及SSH资源使用exclusive；不与Agent/SSH只读或装机的共享机器资源重叠。Claim时核对批准快照；派发前重新完整读取精确对象并核对摘要，再查当前角色、项目、真实租约和两项exclusive claim。原attempt事务内只消费一次`script.dispatch_committed`，持久记录完整公开命令、实际授权/租约时间；目标授权期限不超过该刻原租约（预留100ms）及本次任务超时。心跳续租不改写已发命令；旧命令不能在新的租约窗口偷偷启动，网络/commit响应不明只查询原事件里的命令。

目标专用非root helper先启用Linux child subreaper及no-new-privileges，禁用dumpable。固定`/bin/sh -s`或`/usr/bin/python3 -I -`，解释器解析后打开、核对root所有且无组/其他写权限，并通过固定fd执行；批准正文经stdin，不从命令行拼接。环境只有固定PATH/locale及独立私有workspace的HOME，不继承平台秘密。每流最多观测1MiB，回执只存512字节base64前缀、观测字节数/摘要和完整性；溢出停止并失败，不把截断摘要冒充完整输出。

超时、显式cancel、断连HUP/TERM或正文执行结束后清理剩余进程组/被subreaper接管的后代；`wait4`确认ECHILD且主进程已收割，才可产生终态。只凭主进程退出、SSH断开或kill返回不证明全部停止；2秒内未确认则保留running/unknown，不生成终态成功/停止回执。进程树停止不回滚外部副作用，也不证明经外部服务启动的作业已停止；该子集用于有界前台脚本，不承接服务安装/守护进程治理。目标时钟、/proc、固定解释器/权限及信号行为需软件集中验收/PoC。

传正文路径在任何accepted状态持久化前校验正文摘要；显式缓存路径的瞬时探测不是durable accepted，唯一run按冻结摘要核对实际缓存内容后才启动进程，缓存变化产生明确未启动失败。accepted/running持久后崩溃，即使本地进程看似消失，也不重放；原命令query/cancel无需重新分发正文。媒体撤销或提交人失权在脚本心跳触发停止请求；真实终态仍可作为历史停止事实落库，不凭撤销推断零副作用。资源未知保持隔离，既有`ssh-receipt`只加载原派发命令查询并走显式reconcile。未消费派发而本地失败的安全收尾仍需后续复用/扩展原核实路径，不直接填PASS。

### 页面与源码用例

资产页复用shadcn表单，显式选择当前页受信目标与分页批准版本，展示版本/摘要后勾选确认；输入变化更换幂等键，响应不明的相同输入保留原key。失权/项目变化或介质查询失败清空确认，不自动选脚本。接受后进入原执行中心，不在界面伪造脚本成功。

新增目标专用子进程fixture源码覆盖成功、非零、超时、Python、输出上限、setsid后代收割、重复run只回原回执、query不执行、坏正文不产生accepted；另有真实PG介质/范围/冻结撤销，以及原真实SSH身份后的exclusive Claim/孤立派发一次消费/unknown阻断回归源码。子reaper只在独立fixture进程启用，不能在测试协调器中调用后代清理。按用户安排均未运行；仅格式化和API类型生成，不代表编译、软件协议或硬件验收通过。Agent脚本、参数化/结构化script.json、通用文件分发/缓存、长任务和其他首批任务继续按实施计划推进，WP-1.5/P1仍partial。


### 结构化脚本结果（2026-10-26，源码未验证）

SSH 和 Agent 脚本创建输入可显式选择 `result_format:"json"`，省略保留文本摘要。该选择冻结到 Flow task、原派发命令和回执身份，不可在查询/重连时变更；模拟 Flow 不接受此真实脚本字段。JSON 模式要求 stdout 是单个完整 UTF-8 JSON 对象（原文含空白最多 2048 字节、规范化结果也不超过 2048 字节，最多 8 层、128 个值、键最多128字节），拒绝重复键、多个文档、顶层标量/数组、非有限数值及超限。诊断日志写 stderr。正常退出但输出不符时为 `failed/script_json_invalid`，不是成功或自动重跑。

已停止成功回执保存 `structured_result_json`（规范化 JSON 文本）及 `structured_stdout_base64`（完整有界 stdout），同时核对原大小/SHA/前缀/EOF。用文本承载规范化 JSON 可保留超过 JS 安全整数范围的精确数字，不经浮点往返；资产页选择明确格式，原 Outputs Tab 展示结构化结果并沿用现有回执导出。未停止、输出不完整、非零退出或超时不产生结构化成功。此功能不提供输出物上传或大文件JSON；相关解析/完整回执绑定/专用helper执行与持久查询用例源码已写，未运行。


### 固定平台缓存与分页只读清单（2026-10-26，源码未验证）

批准脚本可由目标运维显式设置 `KEEL_TASK_MEDIA_CACHE_ENABLED=true`，在执行前把已校验正文登记到 `/opt/keel-cache/media/<sha256>`。缓存根目录须由运维预建、归当前非root平台账号且0700，祖先/文件不跟随symlink；程序不创建/改权限客户目录。正文0400、私有归属索引`.keel-owned-v1.json`0600，索引保存首次来源媒体/对象版本与登记时间。共享flock是数据库设备exclusive的本地补充，不能替代控制面租约。无归属文件（即使同摘要）、链接、丢失/损坏登记对象及索引不合法均拒绝，不认领、覆盖、删除或自动修复。写入临时文件/fsync/不覆盖rename、原子索引/fsync后才执行；缓存/索引持久状态不明仍unknown，不把脚本尚未启动当作零缓存副作用。

当前缓存生产者仅覆盖批准1～8192字节脚本，最多64个不同摘要，无自动淘汰。相同SHA/大小必须重新核对实际缓存内容才记cache_hit并跳过对象重写；显式prefer_cache命中可省略脚本正文传输（见下节），仍不冒充大介质分发。记录命中/固定目录的执行回执继续要求实际进程树停止；明确缓存拒绝且进程未启动可failed/script_cache_rejected。

原只读入口增加 `host.cache.inventory`，控制面API/worker须`KEEL_TASK_CACHE_INVENTORY_ENABLED=true`，目标helper同名开关也须true，默认关闭。仅查看平台归属索引，每页16项并逐项核对实际字节大小/SHA，返回索引总登记数不代表其他页已校验。目录/索引不可用、忙、内容不符、取消等有明确失败，不返回空成功替代未知；空归属索引只证明没有登记项，不推断目录无其他文件。私有种子媒体/对象引用不出公开回执，避免设备跨项目后泄露旧项目引用。

续页输入 `cache_cursor` + `cache_manifest_sha256`，限定单目标并冻结到Execution参数及原命令；索引变化明确cache_snapshot_changed，不能混读不同快照。资产页原只读任务可选择缓存清单，Outputs显示当前页及历史查询性质；下一页通过同目标/通道/索引摘要创建新的只读Execution，不覆盖旧回执。历史query/cancel在功能禁用后仍可读取原回执，原权限/身份/资源隔离继续生效。此处不包含缓存清理、远端任意路径访问、通用大文件上传/下载或完整媒体缓存性能验收。


### 命中缓存后省略脚本正文（2026-10-26，源码未验证）

SSH/Agent脚本创建输入新增可选`prefer_cache:true`，默认false；页面为未勾选的显式选项，改变后重新确认。策略冻结为Task的`script_prefer_cache`及原命令的`script_cache_allowed`，查询不能改变。控制面仍读取并核对当前批准版本正文、独立审批、对象版本及摘要，缓存从不替代介质授权。

唯一派发授权提交后、唯一run之前，以原命令发起最长1秒`action:cache`只读探测，只有固定平台缓存实际归属/大小/SHA均成立才返回命中。瞬时`accepted/cache_probe`不是持久任务接收、执行或停止证明，不创建命令目录/receipt/Agent fence。Agent控制面按run同等要求重查当前用户/项目/租约/介质审批/独占资源和原授权期限；query/cancel才保留历史语义。缓存关闭、未命中、忙、探测未知或不合法响应均只允许在首次run之前回落到传输已批准正文。

命中时唯一run仅发送原命令与`use_cached_script:true`，不发送正文；目标端再次以共享锁核对归属索引并读取有界文件副本，实际摘要匹配后将这份内存副本传入固定解释器stdin，不在校验后重新按路径打开。缓存禁用、变化或丢失在进程启动前明确`failed/script_cache_rejected`；此时绝不补传正文或再次run。目标端原fence/accepted/running/终态持久语义、进程树停止证明及unknown隔离不变。大文件断点续传、容量/淘汰/清理和缓存命中率规模验收仍待后续实施。


### 过期脚本派发的显式未启动证明（2026-10-26，源码未验证）

唯一派发在控制面先消费，故控制面/传输在目标持久接收之前崩溃可留下unknown且无目标回执。新增项目作用域`POST /executions/{executionId}/attempts/{attemptId}/script-nonstart-proof`，仅当前Admin/DL/SRE，显式`confirmed:true`、unknown尝试版本及幂等键；仅SSH/Agent批准脚本，必须有唯一原派发事件且原授权已过期。API只向既有outbox和审计登记请求，5分钟有效，冻结原command/attempt/version/requester；不派发脚本、不立即解除隔离。

拥有目标通道的worker轮转处理有界请求，重查当前项目/目标身份和请求人当前启用/角色，Agent broker独立重查原派发和显式请求。目标`action:settle`不带正文/缓存执行标志、不消费Agent fence、不启动子进程，可在脚本功能关闭后处理历史派发。仅原授权已过期且独占原command的run.lock后仍无receipt时，才持久`failed/process_started=false/process_tree_stopped=true/script_authorization_expired_before_start/nonstart_proof=true`。run.lock仍被占用、损坏receipt、已有accepted/running都不推断未启动；已有真实终态原样返回。与run使用同一锁及原命令身份，晚到run只能读取已持久失败，不能执行。

worker仅保存目标终态证明，**不执行Reconcile、不释放unknown资源**。原尝试抽屉可显式请求并手动刷新；取得匹配回执后仍按既有说明/版本/权限显式对账。该证明依赖目标受保护状态目录/文件系统持久化、时钟和已安装helper契约；不将网络错误、文件缺失或人工描述本身当作停止证明。已接收后丢失执行结果仍继续隔离，不能借此自动清理/重跑。请求过期或请求人被禁用/撤权后，不再获取新证明，已记录历史回执仍按原显式对账规则处理。

新增目标未到期拒绝/未接收持久失败/晚到正文不执行/accepted-running-lock忙损坏保持未知及PG请求幂等/无证明不放行/撤权阻断源码用例，均未运行；仅定向格式化/API生成，不代表软件/硬件验收。


### 本轮新增入口的最小请求形状（源码契约，未执行）

批准脚本仍走独立`POST /api/v1/projects/{id}/ssh-script-executions`或`agent-script-executions`，请求示意（ID占位值须替换为当前项目的明确选择）：

```json
{"machine_ids":["<machine-id>"],"media_id":"<approved-media-id>","timeout_ms":5000,"result_format":"json","prefer_cache":true}
```

请求遵循当前会话CSRF、项目角色、显式确认与稳定Idempotency-Key；不能把任意正文/路径/解释器参数塞入此API。`prefer_cache`省略/false保留传正文，`result_format`省略/空字符串保留文本结果，目标开关由操作员现有受保护配置明确管理。

过期unknown脚本在原尝试抽屉显式申请未启动证明，或向`POST /api/v1/projects/{id}/executions/{executionId}/attempts/{attemptId}/script-nonstart-proof`发送：

```json
{"expected_version":7,"confirmed":true}
```

`202 requested`只证明请求已登记，5分钟有效；不能把它当作未启动或释放隔离证明。等待拥有通道的worker取得并保存目标回执后，通过原attempt读取/刷新检查，再按现有Reconcile操作显式对账；此请求不重新执行脚本。演示版本号是占位，必须使用当前尝试版本。当前执行事实摘要可只读`GET /api/v1/projects/{id}/executions/{executionId}/report`，详细内容与边界见FR1-EXECUTION-VIEWS。上述请求未在当前3000旧版执行，本节不是运行或验收记录。

### 批准制品分块分发（2026-10-26，未验证源码）

`POST /api/v1/projects/{id}/artifact-distributions` 显式提交 `transport=ssh|agent`、`media_id`、1～32 个当前可用 `machine_ids` 和 100～30000ms 原授权窗口。复用原 Execution/TaskAttempt/outbox、设备/通道 exclusive 租约、fencing、幂等创建、unknown 隔离与回执核验。原只读端点不接受制品字段。审批、实际对象版本、大小/SHA、当前提交人权限冻结并在派发/心跳重新核对；Agent 分块请求另逐次核对原授权和当前准入。`media.distribute` 审计关联原执行、目标集合摘要及固定对象，不输出对象库凭据。

控制面 API/worker 的 `KEEL_TASK_ARTIFACT_TRANSFERS_ENABLED` 默认 false；SSH 目标 helper 须独立显式开启同名开关。目标由运维预建 `/opt/keel-cache/artifacts`，归运行 helper 的非 root 平台账号所有、0700，祖先/叶均不允许 symlink。不自动创建账号、修复权限或写客户指定路径。制品独立于原 `/opt/keel-cache/media` 小脚本缓存；当前脚本清单、缓存心跳不包含此新目录。

当前工程子集：单制品 1 B～1 GiB，8KiB 分块，最多64个已归属摘要对象，合计预留8GiB。私有 SHA 目录含 `owner.json`（0600）和 `content`（接收0600，完整校验后0400）。run 只消费一次；append 严格绑定原命令、分块 SHA 和精确偏移，内容 fsync 后再原子持久检查点；相同已确认分块只核对并返回原进度。commit 对实际完整文件核对大小/SHA 才记 success。已有 verified 缓存仍完整读回核对，可省略正文传输。所有写入保持原授权截止时间，不因分块/心跳延长。

窗口到期/传输失败后，控制端只作有界 cancel/query；目标持锁保存 paused 检查点和 `writer_stopped=true` 的 failed 回执，不将部分内容标为成功。失联无停止证明则 unknown 隔离，先核验原任务。只有新显式任务、较高 fencing 且旧持有者已 paused 或原授权到期才可接续；同一 run 重发仅回旧回执。落盘/owner 提交不一致、无归属文件、损坏清单等保留 unknown，不自动截断/覆盖/删除，需运维核验。SHA 不符明确 failed，不自动重发损坏前缀。

当前只做控制面→目标固定缓存，不执行/安装内容，不处理依赖闭包；没有目标→控制面通用文件下载、HTTP Range 拉取接线、缓存淘汰或性能通过证据。SSH 每块仍经既有会话调用；没有声称达到原大规模介质 SLO。所有新增测试源码待 P1 集中执行。

### 制品缓存清单（2026-10-26，源码未验证）

双通道原只读创建入口增加 `host.artifact.inventory`，复用当前资产/通道/权限、设备资源键与持久回执；新run须控制面和目标制品开关启用，关闭后历史query/cancel仍可读。固定制品目录受共享缓存锁保护，检查每个摘要目录的归属清单、实际常规文件及已确认大小；8条/页、最多64条，以公开元数据规范化摘要绑定游标，变化拒绝续页。只返回SHA/预期大小/检查点/partial、paused、verified历史状态/文件修改时间与上次完整校验时间，不含seed媒体/项目/对象或command身份。

这是 `page_content_verified:false` 的当前元数据观察，不全量读取1GiB文件，也不拿verified历史状态当作本次内容重验。回执带采集时间和30秒TTL；页面到期显示历史并要求新查询。既有小脚本清单保持真正按页读取字节验证的语义，新缓存不混入其心跳。实际制品命中/commit仍完整核对大小与SHA。协议/真实私有文件夹分页/快照变更/内容被改但元数据不冒充验证/PG准入与冻结用例源码已补，未执行；只gofmt/API类型生成，无迁移或部署。

### 批准文件取回与执行输出（2026-10-26，源码未验证）

`POST /api/v1/projects/{id}/artifact-retrievals` 使用原介质/通道/窗口输入，但只允许一台目标。冻结 `media.fetch` 与独立批准的固定对象身份，沿用一次派发、原授权期限、exclusive设备/通道资源及 `media.retrieve` 审计，不开放任意客户路径。目标仅从平台已归属且曾完整校验的固定制品缓存读取；run重核全文件大小/SHA，固定文件的device/inode/mtime/ctime，每次8KiB read都检查该身份。修改、移除、短读和窗口到期停止取回；query不带正文、不发起新读取，cancel/到期query持同run锁确认reader_stopped。完整读取前不能commit成功。read帧带偏移/分块SHA，正文只存在传输响应，不写历史回执。

控制端逐块核对偏移/大小/SHA，使用私有临时文件累积完整SHA，再写版本化对象库。060新增不可变exec_artifact_outputs，绑定原execution/project/attempt/command/fence及实际大小/SHA与私有存储引用；完整存储记录不是目标成功证明。只有原成功目标回执同时匹配该记录才带下载入口；commit失联保持unknown隔离，不发布半成品。历史回执恢复复用同一存储记录，不重新运行任务。临时文件失败/退出删除，已上传但未获证明的对象不开放下载，不自动删除客户或对象库文件。

`GET .../executions/{executionId}/outputs/{attemptId}/content` 只下载已捕获结果，不连接目标；当前会话/项目成员权限在对象读取前后重查，重新完整核对固定版本大小/SHA，审计后按application/octet-stream附件返回。HTTP Range/If-Range由同一验证后的私有文件响应；原JSON回执下载不变。输出对象损坏不能隔离独立的介质源。关闭分发开关不阻断既有输出下载。

当前双向子集均限1GiB/原有界窗口；向目标发送支持显式检查点续传，反向取回在失败窗口不保留部分临时文件，新任务从头读取。它不是任意路径下载、长期后台传输、跨窗口反向续传或目标程序新产物采集，也无性能通过声明。新增私有文件夹/帧边界/源变化/未读完拒commit与PG完整捕获/丢终态不下载/跨项目/历史关闭/输出篡改源码用例未执行。060未应用，原服务未更新。

### 制品未启动证明（源码未验证）

`media.stage`/`media.fetch`原派发过期且attempt未知时，当前Admin/DL/SRE通过独立artifact-nonstart-proof入口明确授权五分钟恢复查询。Worker和Agent broker绑定原命令、unknown版本及当前请求者权限。目标settle持原run锁，仅对没有任何接受回执的原命令写artifact_authorization_expired_before_start、nonstart_proof和对应停止证明；已有/损坏/忙锁不证明未启动，不访问缓存或推进fence。开关关闭允许历史恢复；请求本身不释放设备，取得终态后仍须显式对账。

### 版本化主机只读Flow（2026-10-26，源码未验证）

通用Flow目录增加keel.host/v1，只发布host.identity/host.wait，零参数输入、零重试、idempotent=true、每步resources=[device]，wait_ms按任务冻结。原YAML依赖校验/不可变版本/回滚保留；拒绝脚本/制品冻结对象、模拟选项、任意命令或业务专有任务。纯只读无需介质。

`POST /api/v1/projects/{id}/host-flow-executions`输入flow_version_id、transport=ssh|agent、明确machine_ids（1..32）、concurrency（默认4，最大32）及circuit_breaker_percent（默认100）。读取原发布版本及摘要，冻结当前项目目标/通道身份、所有原任务/依赖和独立command。SSH沿用provisioned/真实互信/pinned绑定，Agent沿用有效身份/在线连接准入；不自动切换通道。每步共享device、独占所选transport，复用既有调度/outbox/回执/停止/unknown隔离/显式对账，没有新队列。Agent broker按原task_id核对冻结wait_ms，不能借全局参数修改后续步骤。模拟创建入口仍拒绝host版本，域流程仍保留专属批准入口。

作业流页面有主机只读模板，运行时明确选SSH或Agent及本页合格目标，换通道/页重置选择，修改范围重置确认；后台仍重查权限/身份。旧通用仿真和独立脚本/制品入口保留。新增DAG顺序、失败阻断、暂停/恢复、过期租约未知隔离与人工对账、旧版本冻结及篡改wait拒绝的用例源码未执行。只有API生成/gofmt，无编译、测试、DB、部署或硬件验收。

### 主机Flow内的批准介质步骤（2026-10-26，源码未验证）

keel.host/v1进一步允许script.run/media.stage/media.fetch。发布源码只保存media_id引用，仍禁止客户路径、嵌入Script/Artifact审批对象、重试和idempotent=true；脚本可选script_result_format=json及script_prefer_cache。创建时按所选通道检查原开关、当前项目介质授权/独立审批/完整性与固定对象版本，重新冻结每步Script/Artifact。含fetch的DAG仅允许一台目标。原脚本进程隔离/停止证明、制品检查点、原期限/一次派发与捕获下载逻辑原样复用，后续依赖只消费真实成功终态。

发布版本本身保持不变；snapshot.source_flow_content_hash绑定发布定义，snapshot.flow.content_hash绑定加入批准事实后的执行定义。两者不能混用为同一摘要；历史报告/回执仍是执行事实，不冒称硬件Gate。页面列出任务和介质引用，并明确批准脚本可能改设备、未知状态保持隔离；修改范围须重新确认。

新增批准前拒绝/关闭拒绝/独立批准后冻结、原版本不被改写、完整取回捕获后才放行依赖、脚本失联不重放/不放行依赖和资源锁用例源码，未执行。仅gofmt/API类型生成，仍无数据库或部署。

通用主机Flow熔断默认由`exec.circuit_breaker_threshold`项目/全局目录提供，未配置5%；输入省略或0使用默认，明确1～100覆盖。退役或非法目录不能以覆盖输入绕过。目录事实与最终阈值冻结，原幂等请求不受后来目录修改影响；页面留空采用此默认。源码/用例未验证，见HANDOFF§147。
