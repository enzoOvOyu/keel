# Agent 身份、连接与有界只读任务（WP-1.5 部分交付）

本增量消费已存在的项目、Machine 和 provisioned ProjectAsset，提供一次性注册、短期 mTLS 身份及连接心跳。开发验证使用隔离软件身份，运维显式预配目标；不假定 PXE/cloud-init、G3 或 FR-5.7 安装作业已经完成。WP-1.5、FR-1.2、S-3 和 P1 仍为 partial。

正式契约为 `api/agent/v1/agent.proto`，生成代码在 `internal/agentproto`。身份服务提供 `Enroll` 和双向 `Heartbeats`；可选worker另启 `Tasks` 承载下文规定的有界只读任务，默认关闭。不接受任意命令、文件或遥测指标。心跳仅证明当前凭据持有者能通信，不证明硬件健康、SSH/BMC 互信、装机成功或版本来源可信；版本为客户端声明值。

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

服务端证书须具正确主机名/IP SAN，客户端分别预装服务端 CA 与 Agent CA，必须验证 TLS1.3。注册通过服务端认证TLS和一次性秘密授权，心跳另要求客户端证书。服务不信任转发头，注册按直接来源IP限每分钟10次，消息16KiB有界；共享出口的批量注册须运维安排批次，尚未做1000/10000目标性能验收。

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

证书过期会停止心跳，目前没有自动续签、CA 双根轮换或CRL/OCSP分发；须 owner 显式撤销旧证书并重新注册。资产版本或SN改变、项目进入 delivered/archived 立即失效，不能靠重连沿用旧身份。此保守生命周期适合当前隔离开发增量，尚不足以声称完整生产 Agent 生命周期闭环。

本阶段在此身份基础上接入下文的既有 Runner 和 Execution/TaskAttempt/outbox/资源锁/fencing/持久回执，提供有界只读任务及断线对账。后续再按前置推进 FR-5.7 的审批介质/G3安装与 systemd 服务、续签/轮换和监控策略/指标；不得另建队列，不得用心跳代替 CollectPolicy/mon-svc 或假定 PXE 已实现。

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

此能力是WP-1.5的下一小段，仍不等于完整Agent任务目录或P1闭环。仅允许 `host.identity` 和 `host.wait`，不允许 trust.install/trust.verify、shell、脚本、文件操作、软件升级或设备写入。复用已有 `keel.task/v1` Request/Observation，不另建持久派发队列。任务由既有Execution/outbox/TaskAttempt/资源租约/fencing调度，目标device资源与SSH使用相同机器稳定ID，不能绕过unknown隔离。

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

本阶段未新增浏览器启动表单和自动安装；前端后续按用户排期先补shadcn/ui整合与全局等宽字体，再接入相应页面。身份凭据仍只有一小时有效期，自动续签/CA轮换及完整任务/进程管理留后续，不能将本阶段运行作为真机安装或硬件健康验收。
