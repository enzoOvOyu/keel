# Agent 身份、连接与有界任务（WP-1.5 部分交付）

本增量消费已存在的项目、Machine 和 provisioned ProjectAsset，提供一次性注册、短期 mTLS 身份及连接心跳。开发验证使用隔离软件身份，运维显式预配目标；不假定 PXE/cloud-init、G3 或 FR-5.7 安装作业已经完成。WP-1.5、FR-1.2、S-3 和 P1 仍为 partial。

正式契约为 `api/agent/v1/agent.proto`，生成代码在 `internal/agentproto`。身份服务提供 `Enroll` 和双向 `Heartbeats`；可选worker另启 `Tasks` 承载下文规定的有界任务，默认关闭。只读操作与批准小脚本分别准入；不接受内联代码、任意命令路径、文件操作或持续遥测指标；可选网口观测见下节，不替代监控策略或硬件验收。心跳仅证明当前凭据持有者能通信，不证明硬件健康、SSH/BMC 互信、装机成功或版本来源可信；版本为客户端声明值。

## 身份边界

- 管理员在服务器上使用独立 owner 数据库配置执行 `keel-admin agent-enrollment-create`。须指定既有项目、机器、Agent CA 公共证书、原因和新令牌文件；TTL 默认5分钟，允许1～15分钟。项目不可 delivered/archived，资产须 provisioned，当前机器不可已有未撤销证书（包括已过期但未撤销的证书）。
- 令牌为32字节随机值，只写入新建0600文件，不进入命令参数、stdout、数据库或审计。数据库保存域隔离 SHA256 摘要、项目/机器/SN/资产版本、CA 身份和有效期。令牌可以通过现有受控运维渠道交接；本增量没有自动分发。
- Agent 在本机生成 Ed25519 私钥并先持久化0600文件，向已验证服务端 TLS 的 listener 提交签名 CSR 和令牌。控制面忽略 CSR 的 Subject/SAN，只签发绑定项目、机器、credential ID 的 URI SAN，限 clientAuth、有效期1小时。Agent 按运维预装 Agent CA 再验证返回证书，私钥不离开目标。
- 注册锁定同一令牌，原子写公开证书、消费记录与审计；错项目、机器、SN、资产版本、CA、过期、取消或已消费均拒绝。并发消费最多成功一次；同一机器最多一张未撤销证书。
- 心跳必须有受信 mTLS 客户端证书，且精确 DER 摘要在数据库登记并仍有效。每次心跳重新检查当前证书和项目/资产绑定；连接每2秒复核，撤销不会等新 TLS 握手才生效。新序号必须严格递增，流内身份和实际 Linux boot ID 不可变。
- 每张凭据只允许一条活动连接。活跃旧连接保留；断线租约30秒后新连接可取得新 connection ID，旧连接不能继续更新。正常关闭登记 disconnected_at。心跳期望10秒、空闲20秒终止、单流2分钟；客户端只对临时网络/连接容量错误退避，永久身份错误停止。重连从新的连接序号1开始；任务恢复只查询原回执，不自动重放run。
- `agent-enrollment-cancel` 只取消未消费令牌；`agent-revoke` 按 expected-version 撤销证书并关闭其连接，完全相同的重试只产生一条审计。数据库禁止改写/删除注册和证书证据。受限应用角色不能签发令牌、取消令牌或撤销证书；证书 state_version 的列级 UPDATE 仅供 PostgreSQL 行锁使用，实际无撤销字段的修改由触发器拒绝。

## 显式启用与本机命令

默认 `KEEL_AGENT_IDENTITY_FILE` 为空，不创建 listener；现有浏览器 API/3000 不暴露 gRPC。Compose 提供 `/run/keel/agent` 可选只读挂载，不发布 Agent 端口。生产启用需另行准备隔离网络、端口映射和服务端证书，不能仅设置变量后假定目标可达。

先按 DEPLOYMENT 完成备份/迁移047～049及应用角色授权。以下都是运维占位路径，不能直接复用为生产配置；不生成默认 CA 或令牌：

```sh
# 现有安全配置目录的一个全新子目录；不可覆盖已有 CA。
keel-admin agent-ca-init --directory /protected/new-agent-ca
# 在 owner 配置已加载的子 shell 中执行；输出只有公开登记信息。
keel-admin agent-enrollment-create --project PROJECT_ID --machine MACHINE_ID \
  --ca-certificate /protected/new-agent-ca/ca.pem --reason 'approved software enrollment' \
  --expires-in 5m --token-output /protected/handoff/new-enrollment.token
keel-admin agent-enrollment-cancel --project PROJECT_ID --id ENROLLMENT_ID --reason 'unused grant retired'
keel-admin agent-revoke --project PROJECT_ID --id CREDENTIAL_ID --expected-version 1 --reason 'credential retired'
```

私有父目录须已存在且0700，文件须为当前进程UID所有、单硬链接的普通文件，秘密文件0600；拒绝任一路径分量的 symlink、特殊文件和覆盖。容器进程UID为65532，挂载配置和密钥必须按该UID准备受控副本，不能把宿主 ubuntu 所有的秘密文件直接当作容器可用配置。CA 私钥留在控制面私有只读目录，客户端仅持公共 CA。

listener 配置（受保护 JSON，所有路径绝对）：

```json
{
  "listen": "127.0.0.1:7443",
  "server_certificate": "/protected/listener/server.pem",
  "server_key": "/protected/listener/server-key.pem",
  "ca_certificate": "/protected/listener/agent-ca.pem",
  "ca_key": "/protected/listener/agent-ca-key.pem"
}
```

服务端证书须具正确主机名/IP SAN，客户端分别预装服务端 CA 与 Agent CA，必须验证 TLS1.3。注册通过服务端认证TLS和一次性秘密授权，心跳另要求客户端证书。服务不信任转发头，注册按直接来源IP限每分钟10次，入站心跳消息32KiB有界（网口12288字节+缓存8192字节各自独立校验），出站/任务消息保持16KiB和原任务14336字节限制；共享出口的批量注册须运维安排批次，尚未做1000/10000目标性能验收。

目标侧 config（0600）示例：

```json
{
  "endpoint": "control.example:7443",
  "server_name": "control.example",
  "server_ca": "/protected/agent/server-ca.pem",
  "agent_ca": "/protected/agent/agent-ca.pem",
  "project_id": "32_HEX_PROJECT_ID",
  "machine_id": "32_HEX_MACHINE_ID",
  "serial_number": "EXISTING_SN",
  "state_directory": "/protected/agent/state",
  "enrollment_token_file": "/protected/agent/enrollment.token"
}
```

state_directory 须为全新、当前用户拥有的0700目录；项目/机器ID须实际32位小写十六进制。

```sh
keel-agent version
keel-agent enroll --config /protected/agent/config.json
keel-agent run --config /protected/agent/config.json
```

`enroll` 只执行一次，公开输出 credential ID。注册后由运维移除自身交接的令牌副本；程序不自动删除外部文件。`run` 不会自行注册、更换私钥或更新软件。默认仅发送心跳；显式启用下文的 `tasks_enabled` 后才接受有界只读任务，收到 SIGTERM/SIGINT 后关闭流。

## 失败恢复与后续实施

响应丢失或本地 identity.json 未持久化时，已写 agent-key.pem 保留，拒绝重新生成私钥或自动重试注册。运维先核对对应 enrollment 是否已消费、其 certificate_id 和公开审计；若已消费，撤销该证书；若未消费，取消该令牌。之后用新令牌及新的私有 state 目录重新注册，旧材料按运维制度保留/销毁。不能通过删除 key 文件盲目重复注册。

原始身份阶段证书过期会停止心跳；050新增下文默认关闭的同密钥续签。没有有效后继时仍须owner复核、撤销旧身份并重新注册；CA双根轮换和CRL/OCSP分发尚未实现。资产版本或SN改变、项目进入 delivered/archived 立即失效，不能靠重连沿用旧身份。此保守生命周期适合当前隔离开发增量，尚不足以声称完整生产 Agent 生命周期闭环。

本阶段在此身份基础上接入下文的既有 Runner 和 Execution/TaskAttempt/outbox/资源锁/fencing/持久回执，提供有界只读任务及断线对账。后续再按前置推进 FR-5.7 的审批介质/G3安装与 systemd 服务、CA轮换和监控策略/指标；不得另建队列，不得用心跳代替 CollectPolicy/mon-svc 或假定 PXE 已实现。

## 可复现验证

```sh
# 自建仅用于本增量的随机数据库，结束只删除该数据库。
python3 scripts/check-agent-integration.py
# 通用门禁；集成环境缺失的Skip不能当作真实协议验证。
make check
make build-agent APP_VERSION=SOURCE_COMMIT
```

本地存在两份原 fabric 草稿时，沿用 HANDOFF 中受审查 overlay 执行宿主 Go；生产构建只用已提交 git archive。测试覆盖秘密路径、CSR/CA、真实PG事务与受限角色、TLS/gRPC注册和匿名拒绝、重放/并发、身份/序号变化、撤销及实际客户端。具体结果及运行版本只在 HANDOFF 记录，不将软件fixture标为硬件PASS。生成命令/固定版本见 TOOLCHAIN.json 和 Makefile。


## 可选有界只读任务与统一执行内核

此能力是WP-1.5的下一小段，仍不等于完整Agent任务目录或P1闭环。只读入口仅允许 `host.identity` 和 `host.wait`，不允许 trust.install/trust.verify、shell、脚本、文件操作、软件升级或设备写入。独立批准脚本入口见下节，不改变只读入口边界。复用已有 `keel.task/v1` Request/Observation，不另建持久派发队列。任务由既有Execution/outbox/TaskAttempt/资源租约/fencing调度，目标device资源与SSH使用相同机器稳定ID，不能绕过unknown隔离。

任务流必须由拥有当前mTLS身份的Agent主动连接**执行worker**，并绑定同一Agent当前心跳的connection_id。任务池与调度器在同一worker进程；普通API身份listener保持不派发任务。worker只能使用归属于自身活动连接的Agent，不能因为另一worker写有在线标记就向错误进程派发。心跳或证书失效会结束相应任务流；断线后先查询原command_id持久回执，不能把重新连接当成重新执行。

平台和目标都须显式开启，缺一侧则不接收任务：

- worker设置 `KEEL_AGENT_WORKER_FILE` 指向受保护JSON，字段沿用listener配置，并增加 `"tasks_enabled": true`。配置中使用独立监听地址，Agent endpoint指向此worker；CA身份必须与注册凭据一致。
- Compose通过 `KEEL_AGENT_WORKER_DIRECTORY` 将私有配置只读挂载至 `/run/keel/agent-worker`，默认引用无秘密的禁用目录。默认不启用worker profile，不发布任何Agent端口。远端目标访问须运维另行配置隔离网络与映射。
- Agent配置同样增加 `"tasks_enabled": true`；默认false只上报心跳。本地私有state下保存任务接收/运行/终态和fence记录，不能共享、清空或用客户目录替换。

本地先写持久接收记录再执行，只允许目标自身project/machine绑定；同command请求查询已有回执，不再次运行。持久fence防止旧新命令越权覆盖，query/cancel只能针对原命令身份。进程中断或丢失终态时保持未知，控制面租约转reconciling；证据不足不释放资源、不把传输错误写成成功或已停止。

首批操作入口为服务器owner侧CLI，必须再使用**启用的本地平台Admin真实登录**；不通过数据库owner权限伪造用户/会话，也不持久化输出Cookie。口令仅来自受保护文件或终端，CLI结束撤销临时会话。所有修改使用显式幂等键；同一次操作重试必须复用相同键和参数，其他操作使用新键。

```sh
keel-admin agent-task-create --username LOCAL_ADMIN --password-file /protected/operator.password \
  --project PROJECT_ID --machine MACHINE_ID --operation host.identity --timeout-ms 5000 \
  --idempotency-key SAME_CREATE_KEY_ON_RETRY
keel-admin agent-task-status --username LOCAL_ADMIN --password-file /protected/operator.password \
  --project PROJECT_ID --execution EXECUTION_ID
keel-admin agent-task-action --username LOCAL_ADMIN --password-file /protected/operator.password \
  --project PROJECT_ID --execution EXECUTION_ID --action cancel --expected-version CURRENT_EXECUTION_VERSION \
  --idempotency-key SAME_CANCEL_KEY_ON_RETRY
keel-admin agent-task-reconcile --username LOCAL_ADMIN --password-file /protected/operator.password \
  --project PROJECT_ID --execution EXECUTION_ID --attempt UNKNOWN_ATTEMPT_ID \
  --expected-version CURRENT_ATTEMPT_VERSION --idempotency-key SAME_RECONCILE_KEY_ON_RETRY
```

create成功后由既有outbox调度，无单独start命令；action仅支持pause/resume/cancel。status展示原有执行和回执状态，不将正在排队解释为完成。worker后台对unknown只query并持久化真实终态，CLI reconcile只能消费已经匹配command/attempt/fence的真实回执；没有回执则拒绝并保留隔离。CLI不从独立进程伪造跨worker恢复或另发run。

单张叶证书仍只有一小时有效期；同密钥续签见下文050，自动安装、CA轮换及完整任务/进程管理留后续，不能将本阶段运行作为真机安装或硬件健康验收。

### 认证 API 与网页只读入口

`KEEL_AGENT_TASKS_ENABLED=true` 显式开启 HTTP 准入，默认 false。它不创建 listener、不加载 CA 或私钥，也不启用 worker；运维须另行准备上文的 worker 和目标配置。owner CLI 的显式操作不依赖此网页开关。API 与 CLI 在创建执行的同一事务内冻结当前身份、资产版本及新鲜任务连接，worker 仍只能派发至自身拥有的真实流；数据库在线标记不是跨进程传输池。

- `GET /api/v1/projects/{id}/agent-targets`：认证项目成员读取分页的当前有效注册目标；关闭时返回 `enabled:false` 和空列表。只含资产、证书 ID/到期时间、最后心跳及任务通道状态，不返回证书 PEM、注册令牌或配置。离线目标仍可见，已撤销/过期或资产版本/SN 失配的身份不进入候选。
- `POST /api/v1/projects/{id}/agent-executions`：当前 Admin/DE/DL/SRE 可提交 1～32 个目标，仅 `host.identity` / `host.wait`，超时 100～30000ms；要求原有 CSRF 和 Idempotency-Key。关闭时拒绝 `AGENT_DISABLED`，无新鲜任务流拒绝 `AGENT_OFFLINE`。复用原执行、outbox、资源锁、审计和不可变回执。
- 资产页展示通道连接、到期时间及只读任务确认表单；列表分页，当前页仅在线目标可选。连接缓存超过 30 秒或证书过期即停止选择，操作者可刷新。通道在线不等于主机健康或互信验收。
- 创建后进入原执行列表及五 Tab。断线后的 unknown 不自动解锁；worker 重连只查询原 command 的本地回执并保存真实证明。当前 Admin/DL/SRE 在尝试详情核对 command/attempt/fence 与证明、填写依据，调用既有 `/reconcile` 消费已保存的终态。没有证明不可手工指定成功，也没有新增 API 远程执行/重放入口。

实际隔离数据库/HTTP/mTLS/浏览器验证入口：先构建 web，再执行 `python3 scripts/check-agent-integration.py --browser`；普通模式不运行浏览器。证据由 runner 输出到私有目录；生产能力与真实硬件验收另行记录。


### 同密钥证书续签与身份连续性（WP-1.5，050）

服务端受保护配置增加 `renewal_enabled:true` 才开放mTLS `Renew`，默认false；目标配置的同名开关只控制run在两分钟传输窗口之间调用续签，不开启listener或任务。原证书剩余有效期≤20分钟时服务端允许追加一小时同密钥后继；客户端在≤15分钟时请求。不能用过期/撤销/未登记证书、其他私钥、失配资产/SN或不同CA续签。CA剩余有效期不足一小时则拒绝，双根CA轮换仍待单独实现。

050新增只追加的agent_certificate_renewals，按credential_id+前驱DER摘要唯一；同一前驱重试返回同一证书和单条审计。数据库约束核查前驱/有效窗口及到期延长，更新/删除被拒绝；撤销仍锁定原credential，覆盖所有后继。原credential、注册公钥与原DER摘要是稳定身份锚，不是当前传输叶证书；Execution冻结绑定/command/fence及已有回执保持不变。连接另绑定实际叶证书摘要，后继不能借用前驱的心跳connection_id。历史原证书到期不撤销仍有效的后继，也不让旧叶继续认证。

客户端复用本地私钥生成CSR，验证返回身份/CA/密钥/有效期后，以私有目录锁、内容/文件身份校验、临时文件fsync、原子rename及目录fsync更新identity.json。失败保留原记录和私钥；响应丢失或本地提交失败时，持原前驱重试可取同一后继，不重新注册、不自动换钥。前驱已过期且后继未持久化时停止并交owner复核恢复，不突破过期身份授权。run只在连接之间续签，不中断活跃任务或改变unknown隔离；显式 `keel-agent renew --config /protected/agent.json` 可执行同样的到期检查，未到窗口返回renewed=false。

050需在P1集中发布里程碑前进行隔离迁移验证及运行库备份/恢复演练；本段实现不表示已经迁移或发布3000。真实软件测试使用受控短期初始证书避免等待45分钟，不是生产注册或真机验收。续签、CA轮换、私钥轮换、软件升级和自动安装是不同能力，不能互相代替。

owner复核可用 `keel-admin agent-credential-status --project PROJECT_ID --id CREDENTIAL_ID`：返回原始身份锚、初始/有效到期时间、撤销版本、当前资产绑定、最近10条续签与5条连接（含各自实际叶摘要）；不输出PEM、注册令牌、私钥或任务session handle。active指当前登记身份可用，heartbeat_connected/tasks_connected单列，不等于设备健康。已过期或撤销的历史身份仍可查询，读请求严格限定项目。该命令使用已有受控数据库配置，不是公共HTTP查询接口。


### 可选批准小脚本（2026-10-26，源码未验证）

接续 SSH 批准脚本契约，Agent 增加独立的 `POST /api/v1/projects/:id/agent-script-executions`，输入与 SSH 脚本相同，仅提交 `machine_ids/media_id/timeout_ms`；不提交正文或命令行。`GET /task-scripts?transport=agent` 返回本通道当前开关和分页批准介质，省略 transport 仍按 SSH。当前 Admin/DE/DL/SRE 才可创建；目标必须是本项目 provisioned、当前身份/资产版本匹配且具有本 worker 认证任务流的 Agent。资产页复用当前 Agent 列表和原执行中心，在线状态超时即取消候选，不另建任务队列。

批准版本、精确对象版本、SHA/大小、独立批准身份及固定 shell/python 解释器均与 SSH 共用；1～8192 字节，不安装依赖，不开放自定义环境或参数。两侧开关缺一不可：API 与 worker 设置 `KEEL_AGENT_SCRIPTS_ENABLED=true`，API 还须原 `KEEL_AGENT_TASKS_ENABLED=true`；目标受保护 Agent JSON 设置 `tasks_enabled:true` 和 `scripts_enabled:true`。默认均关闭，实际运行配置未修改。

目标运维须预先安装同版本、root 所有且不可组/其他写的普通可执行文件 `/usr/local/libexec/keel-ssh-runner`，不可用请求字段指定其他路径。Agent 钉住 helper fd 后启动独立进程，固定传入私有任务回执目录及脚本开关，不继承 Agent 环境；helper 和脚本仍以 Agent 的非 root 账号执行，不需要 SSH listener。子reaper、解释器和后代回收由该独立进程承担，常驻 Agent 不设 subreaper，不跨任务收割进程。退出发送 TERM 并保留 helper 清理时间；没有完整停止回执仍为 unknown。

脚本同时持有设备及 Agent exclusive 租约，与 SSH/PXE 使用相同设备 ID。原 attempt 派发前持久记录唯一命令/授权期限，实际传输再次核对原事件、当前批准及两项 exclusive claim。帧不超过 14336 字节，沿用 16KiB gRPC 上限。Agent 在落 fence 前校验原正文摘要，重复 run 只查原回执。续约/重连不更新脚本授权期限；缺失原派发事件不可补发。查询/取消不含正文，关闭脚本或介质退役后仍允许读取原命令回执；当前 mTLS 身份/项目/尝试约束继续生效。未知结果保持隔离，操作者凭真实终态回执显式对账。

这仍是有界前台脚本子集，不等于 FR-5.7 G3+ 批量 Agent 安装/systemd、软件升级、通用文件分发或完整任务目录。相关 PostgreSQL 授权/排他/派发丢失与本地 opt-out/历史查询用例已写入既有 Agent 集中入口，尚未执行；目标 helper 运行语义由共享脚本用例覆盖，尚无 Agent 脚本端到端验收结论。

批准脚本也可显式选择 `result_format:"json"`，严格有界 stdout 对象、原文摘要绑定及失败语义与 SSH 共用，见 [SSH-RUNNER](SSH-RUNNER.md) 的结构化脚本结果；不新增 Agent 操作或绕过独立批准。


### 可选网口全量/增量观测（058，2026-10-26，源码未验证）

Agent 配置显式设置 `network_inventory_enabled:true`（默认false，非root）才读取当前 Linux `net.Interfaces` 及固定 `/sys/class/net/<校验后的接口名>/operstate`。没有命令、外部URL、可写路径或硬件型号默认值。只上报索引、名称、MAC（可为空）、MTU、admin-up 和内核 operstate；不推断线缆/邻居、NIC角色、固件/链路健康或G1/NP PASS。最大64接口，任何接口读取失败或完整包超出12288字节均上报明确unavailable，不能发送部分全量冒充完整列表。

Heartbeat proto 增加可选 `network_inventory_json`，严格版本 `agent-network-inventory/v1`，保留原16KiB消息上限。每次新连接第一次成功采集发full，此后以接口索引只发变化及显式removed；失败保留已确认基线，断线重连清空客户端基线。设备采集时间限定过去30秒至未来5秒。listener/worker受保护配置也须 `network_inventory_enabled:true`，服务端环境 `KEEL_AGENT_NETWORK_INVENTORY_ENABLED=true`；缺任何服务端开关则不接纳观测，旧/禁用客户端无字段继续普通心跳。

身份/当前资产绑定和原heartbeat顺序号先校验，观测合并与heartbeat更新时间共事务；无本connection完整基线的delta拒绝并回滚，不能拼接另一连接/设备的旧列表。058表保存当前来源凭据/实际证书摘要/连接/序号、最后完整合并列表及采集/接收/最后成功时间。读取失败或关闭功能仅改变可用性，历史接口保留，不将心跳在线当库存新鲜。证书撤销、资产版本变化或项目越界继续阻断身份与读取。

当前项目成员可读 `GET /api/v1/projects/:id/agent-targets/:machineId/network-inventory`；只读返回来源agent、enabled、ready/unavailable/disabled/stale和30秒TTL，ready仅指观测可用。采集/成功时间过期或连接关闭即stale；API环境开关关闭则disabled。资产页从现有Agent列表打开网口对话框，显式刷新，本地到期或凭据过期也会停止显示近期观测；历史数据、失败原因与未知区分显示。

此功能不等于缓存清单、双向文件传输、CollectPolicy/mon-svc、LLDP三方比对或硬件验收。新增协议/完整与增量合并/失败不交部分列表、真实隔离mTLS/PostgreSQL与受限runtime角色用例源码，统一Agent入口包含该包，均未运行。proto/TS仅生成；058迁移未应用，生产开关仍关闭。


### 批准脚本缓存及只读清单（2026-10-26，源码未验证）

目标受保护JSON可显式`media_cache_enabled:true`，必须同时scripts_enabled/tasks_enabled；Agent只把固定缓存开关传给独立脚本helper，不继承其他环境。缓存与SSH共用上述固定目录/归属/摘要契约，要求同一明确的平台非root所有者，不自动创建账号、改权限或切换身份。默认不开启。

`cache_inventory_enabled:true`（依赖tasks_enabled）才接受`host.cache.inventory`新run；API/worker另需`KEEL_TASK_CACHE_INVENTORY_ENABLED=true`。缓存查询在常驻Agent做有界只读校验，无子reaper/子进程，不需要开启脚本执行。每页16项、续页固定manifest摘要，当前身份/资产/原参数由broker再次核对；禁用后旧query/cancel仍可读原回执。缓存清单由显式查询任务提供，本次未塞进身份心跳，也未将其等同FR-1.2完整缓存心跳/双向文件传输。具体边界见 [SSH-RUNNER](SSH-RUNNER.md)。


批准脚本可显式选择`prefer_cache`，经原派发授权做只读缓存探测，命中后唯一run省略正文、目标端重新核对实际缓存字节；未命中只在首次run前传正文，派发后不得补传重跑。探测不创建Agent fence/任务回执，Agent broker按run同等当前权限/租约/独占资源/介质审批/原授权期限校验；禁用脚本后不可新探测，历史query/cancel不受此影响。固定目录、容量、归属边界和具体字段见SSH-RUNNER「命中缓存后省略脚本正文」。本段为源码实现，未运行集中验证。


### 缓存归属心跳清单（2026-10-26，源码未验证）

目标原`cache_inventory_enabled:true`同时启用固定平台缓存归属索引心跳，控制面沿用`KEEL_TASK_CACHE_INVENTORY_ENABLED=true`，默认关闭。每次心跳只读取受保护归属索引，最多64条`sha256/size`，完整`agent-cache-inventory/v1`帧最多8192字节，携带索引摘要与采集时间；不读取/哈希所有对象、不上传私有seed media/object引用，不自动认领/修复文件。目录、索引、锁忙或中断明确unavailable，无部分列表成功。旧Agent不带字段时仍正常心跳；既有清单保留并标disabled。

新增059缓存观测表，与当前凭据/实际叶证书摘要/连接/心跳序号更新处于同一事务；非法帧不推进心跳。默认关闭时忽略有界可选帧，不创建观测行。失败保存上次清单为历史，断连/采集或接收成功超过30秒则stale；资产/凭据变化后不沿用旧身份清单。项目成员`GET /agent-targets/:machineId/cache-inventory`及原Agent列表「查看缓存」只展示登记观测，`content_verified:false`为固定输出，明确不能证明对象实际存在、内容正确或媒体当前获批。

为同时容纳既有网口帧和缓存帧，gRPC客户端发送/服务端接收上限增至32768字节；服务端发送/客户端接收仍16384，任务JSON14336及回执16384原边界不变，每个库存字段独立严格限制。真正执行/缓存任务仍在目标核对实际字节；心跳清单不替代任务终态、缓存命中、健康/LLDP/门禁证据。全量大介质缓存、续传/淘汰/清理及规模验收仍缺。新增协议/坏索引不交部分成功/索引与实际内容区分、隔离mTLS/PG历史与TTL/非法帧原子回滚、受限runtime角色回归源码，均未运行；proto与TS仅生成，059未应用。
