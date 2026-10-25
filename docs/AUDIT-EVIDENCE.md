# 审计独立留证（P1 源码，待集中验证）

本能力实现 PRD FR-10.3、WP-1.6 / S-5 中的独立摘要留证和数据库篡改比对。它不替代完整数据库备份、异地留存策略或恢复演练，也不证明 S-5/P1 已验收。应用角色的审计只追加权限保持；数据库 owner 仍有能力修改数据库，必须把签名凭据、可信公钥和归档副本放在数据库管理员无法一起改写的独立保管域。

## 使用顺序

使用当前源码构建的 `keel-admin`。数据库命令要求当前数据库 owner 身份和完整迁移；没有 HTTP 入口，不授予应用角色维护权。签名私钥是独立的 Ed25519 PKCS#8 PEM，权限须符合受保护文件检查；不复用生产 Agent CA、SSH 主密钥或介质签名私钥。以下命令中的绝对路径必须由操作人员配置，不能把示例当作已经存在的文件：

```sh
keel-admin audit-signing-keygen --private-key-file /protected/audit-private.pem --public-key-file /independent-trust/audit-public.pem
keel-admin audit-evidence-export --private-key-file /protected/audit-private.pem --output-directory /archive/new-snapshot
keel-admin audit-evidence-verify --public-key-file /independent-trust/audit-public.pem --index-file /archive/new-snapshot/audit-index.jsonl --seal-file /archive/new-snapshot/seal.json
keel-admin audit-evidence-compare --public-key-file /independent-trust/audit-public.pem --index-file /archive/new-snapshot/audit-index.jsonl --seal-file /archive/new-snapshot/seal.json
```

只有 export/compare 需要 `KEEL_DATABASE_URL`。verify 纯离线，不读取数据库、环境主密钥或归档自带公钥。导出目录必须全新：0700，两个文件 0600，写入并 fsync 后返回成功；不覆盖已有归档。失败不会报告完成。文件对外转移由已授权的保管流程执行，本命令不发送消息或上传归档。导出和比对只读数据库，不为了导出再追加一条改变自身快照的审计。

## 格式与比对语义

`keel.audit-evidence/v1` 的 `audit-index.jsonl` 按 32 位小写十六进制记录 ID 逐字节升序，逐行只含 ID 和 SHA-256；不含原始 metadata、账号字段或签名私钥。每条摘要覆盖 audits 的全部现有列：id、actor_id、project_id、action、target_id、trace_id、metadata、created_at。投影区分 SQL NULL/空字符串/JSON null，metadata 对象键稳定排序，数字保持 JSON 数字表示，时间统一 UTC RFC3339Nano。摘要域是 `Keel audit record v1` 后接 NUL。后续新增列或修改投影必须升级格式，不能悄悄沿用 v1。

`seal.json` 固定记录 schema、快照 ID、签名公钥指纹、UTC 导出时间、行数、索引字节数和完整索引摘要；Ed25519 签名使用 `Keel audit evidence v1` 后接 NUL 的独立域。验证拒绝未知字段、重复字段、非规范编码、条目乱序/重复/截断、摘要或签名不符。上限为 100 万条、128 MiB 索引、16 KiB seal、每条 metadata 1 MiB；超限失败，不悄悄截取。大规模归档分片及跨片清单尚待后续设计。

导出使用只读 REPEATABLE READ 一致快照。compare 先完整校验独立证据，再与当前一致快照流式对照：matched 为相同记录，changed 为内容改变，missing 为归档 ID 已删除，additional 为当前新增且未归档记录。新增记录单独计数，不算篡改；changed/missing 任一非零则 archived_rows_unchanged=false，CLI 输出差异计数及最多 20 个 ID，退出非零。空快照只能证明该次归档为空。公钥指纹和签名都不能自行证明导出时间可信或快照覆盖了以后新增的数据。

独立副本用于发现归档后数据库记录被修改或删除，不保证首次导出前的数据真实性。只有数据库之外受信任的时间记录和留存策略才能约束回滚到旧归档、替换整个归档集合及到期销毁。默认审计至少三年的产品要求仍需部署方实现并演练；本开发过程没有生成生产签名密钥、读取生产审计、导出生产数据或修改运行库。

## 集中验证入口

`python3 scripts/check-p1-lifecycle-integration.py` 为新 OS 配置预览、主密钥重包裹和审计留证的批量软件验证入口，使用带 disposable 标签的隔离 PostgreSQL、独占随机数据库及一次构建的实际 owner CLI；对象存储在该入口中为内存 fixture，不代替真实 MinIO/浏览器或真机验收。它会记录实际 PASS 用例和两项 CLI 完成标记，不把构建成功当作用例通过。当前新增测试已写入，尚未运行；按用户要求待 P1 实现里程碑集中验证。
