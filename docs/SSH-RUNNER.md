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

正文摘要在任何accepted状态持久化前校验。accepted/running持久后崩溃，即使本地进程看似消失，也不重放；原命令query/cancel无需重新分发正文。媒体撤销或提交人失权在脚本心跳触发停止请求；真实终态仍可作为历史停止事实落库，不凭撤销推断零副作用。资源未知保持隔离，既有`ssh-receipt`只加载原派发命令查询并走显式reconcile。未消费派发而本地失败的安全收尾仍需后续复用/扩展原核实路径，不直接填PASS。

### 页面与源码用例

资产页复用shadcn表单，显式选择当前页受信目标与分页批准版本，展示版本/摘要后勾选确认；输入变化更换幂等键，响应不明的相同输入保留原key。失权/项目变化或介质查询失败清空确认，不自动选脚本。接受后进入原执行中心，不在界面伪造脚本成功。

新增目标专用子进程fixture源码覆盖成功、非零、超时、Python、输出上限、setsid后代收割、重复run只回原回执、query不执行、坏正文不产生accepted；另有真实PG介质/范围/冻结撤销，以及原真实SSH身份后的exclusive Claim/孤立派发一次消费/unknown阻断回归源码。子reaper只在独立fixture进程启用，不能在测试协调器中调用后代清理。按用户安排均未运行；仅格式化和API类型生成，不代表编译、软件协议或硬件验收通过。Agent脚本、参数化/结构化script.json、通用文件分发/缓存、长任务和其他首批任务继续按实施计划推进，WP-1.5/P1仍partial。


### 结构化脚本结果（2026-10-26，源码未验证）

SSH 和 Agent 脚本创建输入可显式选择 `result_format:"json"`，省略保留文本摘要。该选择冻结到 Flow task、原派发命令和回执身份，不可在查询/重连时变更；模拟 Flow 不接受此真实脚本字段。JSON 模式要求 stdout 是单个完整 UTF-8 JSON 对象（原文含空白最多 2048 字节、规范化结果也不超过 2048 字节，最多 8 层、128 个值、键最多128字节），拒绝重复键、多个文档、顶层标量/数组、非有限数值及超限。诊断日志写 stderr。正常退出但输出不符时为 `failed/script_json_invalid`，不是成功或自动重跑。

已停止成功回执保存 `structured_result_json`（规范化 JSON 文本）及 `structured_stdout_base64`（完整有界 stdout），同时核对原大小/SHA/前缀/EOF。用文本承载规范化 JSON 可保留超过 JS 安全整数范围的精确数字，不经浮点往返；资产页选择明确格式，原 Outputs Tab 展示结构化结果并沿用现有回执导出。未停止、输出不完整、非零退出或超时不产生结构化成功。此功能不提供输出物上传或大文件JSON；相关解析/完整回执绑定/专用helper执行与持久查询用例源码已写，未运行。
