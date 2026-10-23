# ZTP 最小管理与认证复验

范围：WP-1.17 / FR-0.3.2～0.3.6 的增量软件链路。沿用已有管理规划、Kea Execution/Runner、真实 DORA、239/143 身份绑定；不把本增量视为 FR-0 整体验收或真机 PoC。

## 模块与最短操作链

1. 导入网络 BOM，登记 `scope=switch` 的凭据批次引用；批准商定基线并提交管理规划。凭据口令、私钥仍由集中凭据服务托管，平台只传批次引用。
2. ZTP 页选择已规划批次，预览并下载 `ztp-management-manifest/v1`。预览计算不写库。清单绑定项目/批次、规划 ID/摘要、renderer 版本、隔离脚本摘要，以及逐设备脚本的 SHA256/字节数/凭据批次；返回正文供逐项复核，清单中的摘要条目不内嵌脚本文本。
3. 将下载的**精确 JSON 字节**登记为 `ztp_script` 介质，走既有独立复核批准流程。将该介质 ID 交给 Kea 批次；不能把清单 SHA 当作每设备脚本 SHA。
4. 239/143 每次请求重新验证当前规划、身份与源绑定、Kea 实际活动配置和 UNKNOWN 隔离；核对当前清单与批准介质 SHA/size 后实时渲染对应设备脚本。239 返回脚本并记录实际正文摘要；143 包装同一正文为 onboarding CMS，记录实际 CMS 摘要。固定隔离介质仍兼容；个性化清单缺设备时拒绝，不静默换成隔离正文。
5. 自动复验器消费已有不可变 `data-served` 观察，调用固定 mTLS 网关读取设备。只读并发上限 4，使用既有 `ssh:switch:<BOMID>` 资源行共享锁：拒绝任何 held/reconciling lease；持锁至最终复验回执提交。统一 Worker 对同一行的 `FOR UPDATE` 配置写申领因而不能并发，不新增设备写队列。
6. 复验后再次核对当前用户/项目权限、规划、批准介质、凭据批次、实时 Kea 与 UNKNOWN 状态、证据新鲜度。只有 `evidence_source=device` 的完整可信证明可以创建/推进真实设备会话；`protocol_fixture` 只存复验记录，永远不产生真实 managed。NP-2 仍另外需要最近 Golden 结论。

## 设备适配边界

- `internal/fabricboot/management_script.go`：版本化 Cumulus NVUE 最小渲染器。仅关闭前面板口，保留 mgmt VRF，设置批准 hostname、eth0 IPv4/前缀和网关，apply/save；没有 BGP、VLAN、QoS、业务凭据或任意命令输入。先拒绝无关 pending 配置；参数严格校验。凭据批次引用只在清单/请求中，脚本不含口令、私钥或引用令牌。
- `internal/platform/ztp_management_artifacts.go`：当前批准规划与注册凭据引用的清单；既有介质库承担独立复核审批，不另造批准通道。规划/凭据引用/renderer 变化必须重新生成并批准精确清单。
- `internal/fabricboot/manageability.go`：沿用 `KEEL_FABRIC_PROBES_FILE.targets.network` 的固定 URL/CA/客户端证书，调用 `POST /v1/projects/{project_id}/ztp/manageability`。端点不来自浏览器，不跟随重定向；请求 8s、响应最多 64KiB、严格字段解码。
- `internal/platform/ztp_manageability.go` + 036：不可变复验回执、串行副本协调及 4 路只读探测、当前事实复验、真实会话九态、凭据失败分类和指数退避。`fabric.ztp_retry_max` 默认 3，最多 10 次重试；原初尝试另计一次。超限保留 failed/三段式定位。
- `internal/platform/ztp_device_sessions.go` / `internal/httpapi/ztp_completion.go`：独立真实会话只读入口。旧 `/sim-sessions` 保持仅仿真，真实会话无手动“推进成功”入口。
- `web/src/features/ztp/ZTPCompletion.tsx`：复用共享工作台和 Intel 字体，展示制品复核、来源、复验/重试和真实时间线。

## 网关契约

请求 `ztp-manageability-request/v1` 包含请求 ID、项目/批次/设备/SN、规划/活动 offer/实际投递证据/脚本 SHA、凭据批次、管理 MAC、批准 hostname/IP/前缀/网关。网关按用途 `ztp-management-read` 从中央凭据存储解析批次，不能接收明文密码或任意 SSH 命令。

响应 `ztp-manageability/v1` 绑定**请求精确 JSON 的 SHA256**、相同项目/设备/SN/批次凭据，时间不早于 60s、不晚于未来 5s；来源只能 device/protocol_fixture。结果：ready / credential_expired / unreachable / not_ready。ready 还必须提供：

- SSH 已认证、可信主机密钥 SHA256/信任记录、用途匹配；不以端口可达替代认证。
- 管理 hostname/IP/前缀/网关与批准规划一致，VRF=mgmt，配置导入和服务重启证明，前面板隔离证明。
- **该设备**管理 MAC/IP 的 DORA/租约证据摘要；不能用独立 canary 的 DORA 代替设备 DORA。
- 非空、最多 32 个 SHA256 证据引用。完整字段见 Go DTO；未知字段（包括额外秘密字段）拒绝。

可选 `stage_evidence[]` 保存九阶段 `state/at/sha256`，若提供必须完整、顺序一致、时间单调且在 24h 内；原样留在不可变证明中。会话 `device.verified_*` 时间为服务端**验证时间**，不是伪造设备发生时间。没有详细阶段证据时不发明那些原始时间。

## HTTP 入口

均要求当前登录与目标项目 `fabric:read`：

- `GET /api/v1/projects/{id}/ztp/management-artifacts/preview?batch_ref=...`
- `GET /api/v1/projects/{id}/ztp/manageability-checks`（最近 100 条）
- `GET /api/v1/projects/{id}/ztp/device-sessions`（最近 100 条）
- `GET /api/v1/projects/{id}/ztp/device-sessions/{sessionId}`

自动 reconciliation 从 API 后台循环启动，缺少 network/Kea 配置时空跑，绝不回退模拟成功。失败的传输记 unknown/unreachable 回执并有界重试；credential_expired 进入凭据问题及重试分类。

## 验证与 PoC 边界

已执行：`go test ./internal/fabricboot ./internal/platform ./internal/httpapi -run 'Test(Manageability|ManagementScript|ZTPPersonalized)' -count=1`；相关三包 `go vet`。协议测试覆盖 TLS 客户端证书、固定路径、请求绑定、过期/错身份/不可信主机密钥/隔离缺失/错误设备 DORA、expired 和重定向。独立 PostgreSQL 用例 `TestZTPManagementArtifactsAndDeviceProjection` 覆盖清单零写预览、精确批准摘要、凭据引用变更阻断、fixture 禁止真实会话、expired 重试与真实证明九态、资源共享锁。

尚需真实 PoC：目标 NVUE 版本的命令/默认 pending/持久化和服务行为、工厂 credential 引导/集中凭据网关实现及可信 SSH host-key 来源、设备级 DHCP 日志解析、真实 IPv6 136/厂商 60-43 控制适配、轮换期存量旧凭据自然过期全链、RMA 换件完整流程、千台规模与 HA。当前实现不修改设备用户口令/SSH 授权密钥；网关必须通过已经批准的集中凭据验证可管理性。缺上述能力时保持阻断/unknown，不冒称已完成硬件交付。

NVUE 命令依据：[NVIDIA NVUE Cheat Sheet](https://docs.nvidia.com/networking-ethernet-software/knowledge-base/Setup-and-Getting-Started/NVUE-Cheat-Sheet/) 与 [Cumulus 5.14 Quick Start](https://docs.nvidia.com/networking-ethernet-software/cumulus-linux-514/Quick-Start-Guide/)。不同 NOS 版本的 PoC 差异在独立 renderer/网关适配器内修改，不改业务准入。
