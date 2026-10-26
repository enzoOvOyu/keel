# P1 出站执行通知（源码，待集中验证）

WP-1.6 / PRD FR-10.4 的出站通道复用现有执行 outbox：签名CloudEvents Webhook、Resend邮件和Slack机器人。默认关闭，当前没有向外部接收人发送通知，也没有启用运行 worker。通知配置编辑页面、工单/门禁/AI 告警来源和入站 Flow 触发器尚未完成；不能将此子集计为 F-10.3 或 P1 全部通过。

## 配置与独立凭据

仅 worker 加载 `KEEL_WEBHOOK_SINKS_FILE`。为空时不读取签名密钥、不解析 DNS、不发送通知。文件和密钥必须为当前 worker UID 所有的普通单链接文件，权限 0600，路径绝对且没有符号链接；建议放在 0700 私有目录。容器配置使用独立只读挂载 `/run/keel/webhooks`，宿主来源由 `KEEL_WEBHOOK_DIRECTORY` 指定。不要复用 SSH master、Agent CA 或介质签名私钥。为每个独立保管/项目接收域生成新的 32 字节随机 HMAC 密钥：

```sh
keel-admin webhook-signing-keygen --output /private/webhooks/project-failure.key
```

该命令不连接数据库，不覆盖旧文件；目录须已存在且为当前 UID 私有。只返回 key_id，不输出密钥。后续由已有安全凭据交换流程把 HMAC 密钥交给对应接收方，不通过日志/URL 查询参数/本仓库存储。当前开发未生成任何生产通知凭据。

配置格式示例（项目 ID 和时间必须替换为实际授权值）：

```json
{
  "schema": "keel.webhook-sinks/v1",
  "allowed_origins": ["https://notifications.example.com"],
  "sinks": [{
    "id": "project-failures-v1",
    "project_id": "0123456789abcdef0123456789abcdef",
    "not_before": "2026-10-25T17:00:00Z",
    "event_types": ["execution.state_changed", "execution.circuit_breaker", "execution.authorization_blocked"],
    "url": "https://notifications.example.com/keel/events",
    "signing_key_file": "/run/keel/webhooks/project-failure.key"
  }]
}
```

最多 16 个 sink、32 个精确 origin。HTTPS 是默认要求；直接进程开发测试显式 `KEEL_ENV=development` 时才可用 HTTP。Compose worker 保持安全默认，不把现有开发环境自动视为可用 HTTP 出站。URL 禁账号信息、查询参数和 fragment；不跟随任何重定向，不读取代理环境。复用已有允许源传输的 DNS 地址校验及实际 IP 拨号，拒绝 loopback（显式开发 loopback 例外）、link-local、云 metadata、组播与未指定地址。私网地址仍须由运维通过精确白名单和网络策略授权。

`not_before` 是首次消费下界，必须显式配置，防止首次启动默认重放全部历史。每个 sink 的项目、URL、签名密钥指纹、事件类型及时间下界形成不可变策略摘要；首次绑定与审计原子提交。重启可幂等使用同一策略；同 ID 修改目标/项目/密钥会拒绝启动，不能静默把待发送记录转投其他目标。改目标或轮换 HMAC 必须部署新 ID、新凭据和明确的新时间下界。旧配置移除后不再认领其积压，保留原投递证据；本版本不提供旧队列自动迁移/重放。

## 事件、签名与重放保护

消息为 CloudEvents 1.0 JSON，ID 来自已有 outbox 事件；source/subject/data 固定包含项目 ID、执行 ID、聚合版本及已识别的执行状态。类型只允许上述三种。原始 outbox metadata、命令输出、actor、目标列表和密钥均不出站。消息仅描述已经提交的事实，通知成功不能改变执行结果或代替门禁。

每次 HTTP POST 携带：

- `X-Keel-Event-ID`：稳定事件 ID，重试保持相同。
- `X-Keel-Timestamp`：本次请求 Unix 秒，十进制规范编码。
- `X-Keel-Key-ID`：带独立域的签名密钥摘要，用于接收方选定受信任凭据。
- `X-Keel-Signature`：`sha256=` 加小写十六进制 HMAC-SHA256。

签名输入是原始字节 `timestamp + "\n" + event_id + "\n" + raw_body`，不重新序列化 JSON 后再验。接收方须验证独立配置的 key_id、HMAC、五分钟时间窗、消息 body.id 与头 ID 一致及 body 项目与接收路由一致，并持久化 `(project_id,event_id)` 去重。`internal/webhook.Verify` 实现原始报文和时间窗认证；持久去重与项目路由检查是接收方责任。签名本身不能防止窗口内重放。不要按每次 timestamp 生成不同业务事件。

## 持久投递与可观测性

052 为现有 outbox 添加独立消费者的策略/投递证据表，不改变执行调度器的 published_at/ACK，不另造业务执行框架。扫描按项目、允许类型和时间下界，唯一 `(sink_id,event_id)`；每批最多100条。数据库插入触发器同时复验源事件所属项目、策略类型/时间下界、序号和完整公共投影，拒绝混入额外 metadata 或伪造初始送达状态。每个 sink 独立循环，最多16个并发请求；失效接收方不能阻塞其他 sink、任务调度或心跳。发送前用短事务认领，HTTP期间没有数据库行锁；认领30秒、请求最多8秒。完成按 lease token CAS，重启/认领到期后继续用原事件 ID。

2xx 记 delivered；408/429/5xx 和传输结果未知有界退避重试，最多6次，基础30秒指数增长、上限15分钟；其他状态（含重定向/403）立即 dead。响应正文不保存，状态/错误码不含目标 URL 或接收方敏感数据。至少一次投递意味着接收方可能已收件而平台未获响应，无法宣称恰好一次；接收方必须幂等。终态投递、原事件身份与 payload 不可修改/删除，不提供对 dead 的盲目重试入口。

项目Admin/平台Admin可在项目审计页查看分页投递记录，通过`GET /api/v1/projects/{id}/notifications/webhook-deliveries?page=1&page_size=20`读取。沿用`project:audit:read`边界，每次请求同一只读事务复核当前账号、会话与项目Admin成员身份；其他项目无成员返回404、非Admin成员403，过期/撤销会话401。只返回接收配置ID、事件/执行ID、公开类型、状态、次数、通用错误和时间，不返回目标URL/载荷/密钥。项目切换重置页码，缓存键含用户/项目，失败记录可打开原执行。空列表不表示已启用通知或通道健康；不提供盲目重放按钮。此增量源码未验证。

owner仍可独立只读分页查看项目投递状态，不需要加载 HMAC 密钥：

```sh
keel-admin webhook-delivery-status --project 0123456789abcdef0123456789abcdef --limit 50 --offset 0
```

需要 owner 数据库连接与完整源码迁移；应用角色不能通过该维护命令提权。输出仅为 sink/event ID、状态、次数、HTTP 状态、通用错误码和时间，不含 URL、payload 或签名。worker 健康仍代表执行循环健康，不代表每个通知接收端健康；通知失败必须查看该投递证据，不能把主健康探针当送达证明。

## 待集中验证

新单元/实际 PostgreSQL 与本地 HTTP 接收 fixture 用例覆盖签名、过期/错密钥/报文改写、白名单/metadata/重定向、项目隔离、无原始 metadata 出站、独立调度 ACK、不可变策略、503→204、有界退避、403终止、过期认领恢复、并发不重认领、六次后停止及实际 owner 状态 CLI。已加入 `check-p1-lifecycle-integration.py`，尚未执行；该入口不是完整 P1 或硬件验收。必要编译结果以 HANDOFF 记录为准。

## 邮件与IM适配（源码，未启用）

同一配置的sink可选`format`：省略或`cloudevents`保留原报文/原策略摘要；`resend`和`slack`在发送边界转换公开事实。配置schema仍为v1，新增可选字段；065为不可变策略保存通道类型，不修改原投递载荷。现有CLI/API/审计页显示类型。未新增队列或自动发送入口。

邮件采用[Resend Send Email API](https://resend.com/docs/api-reference/emails/send-email)，不要求SMTP账号或另建邮件服务器。sink额外字段示例（其他id/project/not_before/event_types/signing_key_file沿用前例）：

```json
{
  "format": "resend",
  "url": "https://api.resend.com/emails",
  "api_key_file": "/run/keel/webhooks/project-mail.api-key",
  "email": { "from": "keel@example.com", "to": ["ops@example.com"] }
}
```

`allowed_origins`须明确包含`https://api.resend.com`。独立provider token文件0600/当前UID/普通单链接/无符号链接，内容为token本身，无尾随换行；不得复用HMAC密钥。最多16个明确收件人，重复或地址注入拒绝。sender/recipient、通道、URL、独立provider凭据指纹均进入策略摘要，同ID变更拒绝绑定，收件人和token不写DB/审计/页面。邮件只含固定标题、项目/执行/事件ID、版本、时间及可识别状态，无HTML、附件、日志或执行输出。

`Idempotency-Key=keel/<sink_id>/<event_id>`重启/重试保持一致。[供应商幂等保留24小时](https://resend.com/docs/dashboard/emails/idempotency-keys)，本适配仅发送创建后23小时内的事件，保证全部尝试在该窗口内；积压超时记dead/event_expired且不再发出请求，不能在幂等失效后盲目重发。HTTP 2xx还须包含有效provider message UUID才记接受；缺失/损坏应答按未知结果有限重试，响应不保存。此状态仅证明邮件API接受，不能证明收件箱已投递或用户已读。

IM采用[Slack Incoming Webhooks](https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks/)：`format=slack`，`url=https://hooks.slack.com/services/<team>/<bot>/<secret>`，明确白名单`https://hooks.slack.com`。URL本身是凭据，只保存在受保护配置；不填`email/api_key_file`。使用固定纯文本公开事实，禁链接展开，不允许调用者注入@提醒或任意模板；仅HTTP200及纯文本`ok`应答记接受，2xx错误文本不计成功。该通道至少一次投递，传输未知时可能重复，不声明恰好一次。

两个适配均保留对实际发送JSON的X-Keel HMAC头；供应商身份认证分别依赖独立API token或Slack secret URL，**不声称供应商会验证Keel HMAC**。原CloudEvents自建接收方仍须按前文验证HMAC/时间窗/持久去重。生产只接受上述精确供应商host/path，受同一DNS固定、metadata排除、无代理/重定向及8秒边界限制；显式development仅可替换为loopback协议fixture。未开放任意邮件API/IM供应商或通用SMTP。

新增本地HTTP协议及PG持久outbox用例源码，覆盖实际wire签名、token隔离、稳定邮件幂等键、跨项目/错误200应答拒绝、超时窗口、收件人/密钥变更绑定冲突及关闭清除凭据；未运行，不代表外部服务、真实邮箱或IM验收。后续门禁/工单/AI等事件源按所属阶段接入，不为P1提前实现后续业务。
