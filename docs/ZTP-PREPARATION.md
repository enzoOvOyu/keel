# ZTP 准备模块：实现与 PoC 修改入口

范围：WP-1.17 的 N-S2 最小管理规划、N-S3 协议自检。业务依据 PRD FR-0.2.1/0.2.6，页面依据 DESIGN-SYSTEM；本文不新增业务验收或声明 FR-0 完成。

## 模块和接口

| 修改内容 | 入口 |
|---|---|
| 地址/命名/批次纯计算 | `internal/fabricboot/management_plan.go` |
| 批准基线、BOM/参数读取、预览指纹、原子保存 | `internal/platform/ztp_management.go` |
| 环境协议和 TLS 适配 | `internal/fabricboot/environment_probes.go` |
| 自检持久化、授权与报告有效性 | `internal/platform/ztp_environment.go` |
| HTTP/正式类型契约 | `internal/httpapi/ztp_management.go`、`ztp_environment.go`、`api/openapi.yaml` |
| 页面/独立样式/中英文 | `ManagementPlanning.tsx`、`EnvironmentChecks.tsx`、`ztp-planning.css`、`ztp-management-i18n.ts` |

API 均位于 `/api/v1/projects/{id}/ztp/`：`management-plan/preview`、`management-plan/commit`、`management-plan`（最近保存）、`environment-checks`（POST执行/GET读取）。沿用真实会话、CSRF、目标项目授权与审计。只读角色可预览/读报告，NDE/Admin可保存/执行；权限联合列表不能授权另一项目。

## 规划约束

管理规则放在不可变批准基线的 `engineering_limits.management` 中：`cidr`、`first_address`、`gateway`、`batch_prefix`、`batch_by(rack|pod)`；样例见 `internal/fabricboot/testdata/management-baseline-v1.json`，该文件是软件契约样例，**不是客户批准方案**。沿用现有外部批准证据登记，不声称自动验真签署。

当前实现 IPv4 /8～/30 管理网；不分配网络/广播/网关地址。hostname 按 `{dc}-{pod}-{role}-{rack}-{idx}` 转小写，不替换下划线，长度≤63，重复即阻断。只读取当前项目有 BOM 关联的非退役交换机。`fabric.batch_size` 读项目覆盖/全局参数，缺省500，上限500。

既有地址/命名/批次绑定保留，新增 BOM 不使旧设备改号。规则不兼容旧绑定、地址耗尽或冲突时阻断，不静默迁移/回收。预览零写入；提交重读基线/BOM/参数/分配，指纹变化拒绝旧预览；相同结果幂等返回原计划。规划不写设备、不生成 NP-2。人工微调、IPv6、完整 Loopback/VRF/BGP 分配、生产模板生命周期仍归后续 WP-2.15。

## 环境自检与配置

服务器启动时读取 `KEEL_FABRIC_PROBES_FILE` 指向的私有普通 JSON 文件（权限600）；配置来自运维，不接受浏览器传入目标 URL。证书/私钥仅保留受限目录，容器需由运维通过只读绑定挂载，并保证应用 UID 能读取；当前默认 Compose 不挂载、不启用外部探针。配置变更重启服务后生效。

```json
{
  "version": "fabric-probes/v1",
  "targets": {
    "kea": {"url":"https://kea.example.invalid/","ca_file":"/run/keel/fabric/ca.pem","certificate_file":"/run/keel/fabric/client.pem","key_file":"/run/keel/fabric/client.key","through_agent":true},
    "boot": {"url":"https://boot.example.invalid/readyz","ca_file":"/run/keel/fabric/ca.pem"},
    "collector": {"url":"https://collector.example.invalid/readyz","ca_file":"/run/keel/fabric/ca.pem"},
    "repository": {"url":"https://repository.example.invalid/approved-canary-artifact","ca_file":"/run/keel/fabric/ca.pem","sha256":"REPLACE_WITH_APPROVED_ARTIFACT_SHA256"}
  }
}
```

- Kea：只读 `config-hash-get` 协议响应/摘要；要求配置 CA 和客户端证书。并不执行 config-set，不证明 DORA 或配置持久化。
- 引导服务：专用 TLS listener 的 `/readyz` 返回 `service=keel-ztp,status=ready,capabilities=[ztp239]` 或 `[sztp143]`，同时检查数据库迁移。143 listener 仍有 TLS 层客户端证书要求。不能用普通 API `/healthz` 冒充专用引导服务。
- 采集器：接口契约 `service=keel-fabric-collector,status=ready,capabilities=[lldp,interfaces,ddm]`；PoC 采集器适配此契约或修改该协议适配器，不能用200状态代替能力检查。
- 仓库：读取批准的小型 canary 制品（≤1MiB），校验固定 SHA256。不是存储服务端口可达即通过；设备实际介质审批/摘要仍独立核验。

HTTPS、显式CA、不跟随重定向、读响应≤1MiB、4秒请求超时；四探针并发，批次/通道绑定当前保存规划。缺配置/能力为unknown，协议/连接失败为failed；仅四项ready时报告ready。报告有效期5分钟，过期、配置指纹/规划/BOM/参数变化使读取结果失效；外部请求前后均重新授权和校验规划，避免网络等待期间持锁。

**剩余接线**：当前报告提供可校验的 N-S3 结果，尚未替代旧 mock Kea 会话或接入完整真实 Kea/Runner 作业准入。下一步将规划/新鲜报告接入真实 staging→commit→持久化/readback→DORA 及设备引导入口，再接个性化最小管理配置与凭据取用；不能把当前探针通过写成 N-S4/NP PASS。协议桩连通与真机 PoC 分开记录。

## 已执行检查

`TestManagementPlan*` 验证稳定地址/批次、容量和命名拒绝；`TestEnvironmentProtocolProbes` 通过真实TLS协议桩检查只读Kea/能力/制品及重定向拒绝；`TestZTPManagementPlanTransactions` 使用独立真实PostgreSQL检查批准准入、零写预览、陈旧拒绝、幂等/稳定身份、只读角色拒绝、自检unknown持久化及规划变化阻断。

`check-ztp-planning-browser.mjs` 使用独立真实API/数据库，通过UI导入/登记批准、BOM预览、保存/刷新、自检四unknown与1440布局。证据 `/tmp/keel-shell-1d9fq34r`；隔离数据、进程和凭据已清理。未做真机PoC，未声称全量业务验收通过。
