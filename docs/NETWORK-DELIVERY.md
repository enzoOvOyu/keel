# 网络交付证据归档

对应 PRD FR-0.6.8 / FR-0.8.6、ACCEPTANCE F-0.12 和 WP-4.10 的部分软件交付。当前实现不代表 FR-0、WP-4.10 或真实网络交付验收完成。

## 当前能力

项目页面 `/p/{projectId}/delivery` 提供交付复核、分页引用清单和最近100条归档记录。服务从既有项目、BOM版本、型号、批准管理规划、ZTP、采集、Golden、规则与参数目录装配引用，不建立第二套资产主数据。页面和接口均按当前项目授权，导出创建要求 CSRF 与 Idempotency-Key。

预检使用 repeatable-read 快照，不写业务数据或审批记录。创建时取得既有控制锁，再在独立一致性快照中重读引用图，与用户确认的 preflight_hash 及外部验收范围比较；重复提交返回原归档。写入记录、审计和幂等响应同事务提交。归档由数据库不可变触发器保护，下载逐次核对实际字节 SHA256 并记录审计；历史档案不声称当前仍就绪。

- `draft`：明确标注非交付证据草稿，可保存当前缺失、失败、未知和协议夹具来源。
- `delivery`：N-S0～N-S8 必须满足阶段证据或有效范围绑定豁免；引用完整性、未知写隔离、身份冲突和模拟来源不能通过豁免绕过。缺失或过期证据阻断，豁免保留原结论及原因。
- N-S0 不从“项目存在”推断模板应用；当前缺少模板应用证据，因此正式交付仍受此门禁约束。N-S8 消费既有验收记录，N-S9 导出不要求它自身未来的回执。

## 接口与修改入口

相对 `/api/v1/projects/{id}/fabric/delivery`：

| 接口 | 用途 |
|---|---|
| GET `/preflight` | 当前阶段判定、范围与预检摘要、至多100条引用预览 |
| GET `/references?page=&page_size=` | 引用清单；每页1～1000条 |
| GET `/schema` | 内嵌 standalone JSON Schema，标识 `urn:keel:network-delivery:v1` |
| GET/POST `/exports` | 最近100条元数据 / 幂等创建不可变 JSON 档案 |
| GET `/exports/{exportId}/download` | 原始归档字节与 `X-Keel-Content-SHA256` |

FR-0.8 导出别名 `/fabric/baseline/export?export_id=...` 必须明确选择历史档案，不自动替换草稿或实况。当前仅导出 JSON；未实现 CSV 或大规模异步归档。

业务判定位于 `internal/fabricdelivery`，来源装配及持久化位于 `internal/platform/network_delivery*.go`，HTTP位于 `internal/httpapi/network_delivery.go`；正式契约及生成类型沿用 `api/openapi.yaml`。038迁移只新增归档表，不依赖历史026草稿；本轮仅在隔离测试库应用。

可选验收读取适配器复用运维人员配置的固定 mTLS network 网关，POST `/v1/projects/{id}/network-delivery/review`。请求携带 project_id、scope_hash；响应须匹配版本、项目、范围、请求摘要及30秒新鲜度。验收/豁免只能消费外部既有记录；浏览器不能提交审批结论。未配置或不可用时保持阻断，错误范围或过期响应拒绝。尚无真实 CMDB 对接或归档 ACK 写回证据。

## 验证与剩余范围

执行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json make check`；overlay仅排除既有未跟踪026/fabric_precheck草稿。`scripts/check-kea-integration.py --delivery` 复用 disposable PostgreSQL、独立 `keel_test_kea_3106_*` 数据库/API3106、共享 mTLS设备边界夹具和真实浏览器，覆盖预检、稳定摘要、分页、变更拒绝、重复/并发幂等、档案摘要、历史隔离、审计与确认流程。验收读取的错误范围/过期/夹具来源另行拒绝验证。独立 JSON Schema 可用 Python jsonschema 对实际档案验证。

软件门禁与数据库/浏览器通过不代表真机或规模验收。inline实现限制100000个BOM对象、32MiB归档；尚缺流式/异步大规模导出、完整项目模板应用、全部领域规则/诊断结果索引、G2/PortModel/CMDB归档消费与ACK、长期保留策略及H-15～H-18真实证据。当前通用引用清单保留历史规则版本，`rule_versions`列出已接入的无损/线缆判定版本；不能据此声称所有FR-0判定已完整归档。
