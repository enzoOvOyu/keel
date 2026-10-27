# BMC 资产采集开发边界

当前为 WP-2.1/FR-3 部分源码，尚未集中验收、迁移或发布。工作目录 `/opt/keel`，单 Agent。真实硬件 PoC 由用户安排。

## 手动采集与读取

资产页按设备打开“BMC 资产采集”，手动选择 L1/L2/L3 并填写原因。POST `/api/v1/projects/{project}/assets/{asset}/bmc/collections` 接受 `level`、`expected_asset_version`、`reason`，需要当前会话/CSRF/Origin、幂等键和 Admin/DE/DL/SRE 项目执行权限。返回普通 Execution，成功受理不表示采集成功。浏览器不能提供地址、口令、凭据版本、来源或任意任务。

请求冻结当前项目 BOM 的凭据批次、资产版本、序列号和服务端适配绑定。共享 Execution 负责 outbox、派发、心跳、fencing 和暂停/取消；`device:machine:<id>` 共享只读租约兼容 SSH/Agent 读取，`bmc:machine:<id>` 独占串行化同一 BMC 通道。原命令先登记不可变 dispatch，再允许设备读取；原 dispatch 不可自动重新进入。采集器返回后才能写 stopped 回执。崩溃后若不可变报告已提交，可只用原报告恢复回执；过期任务仍需显式对账，不自动释放未知资源或重采。

GET 历史按页返回摘要。`/{collection}/manifest` 读取质量、来源、凭据版本、能力覆盖和失败；`/resources` 分页元数据；`/resource?path=...` 单资源正文；`/raw?path=...` 读取已绑定的原始对象。页面不会自动轮询或读取全部正文，下载不连接设备。完整 `/{collection}` 保留全报告调试接口。所有读取核验实际项目/资产归属，Admin 也不能跨项目拼接对象。

原始 JSON 复用既有版本化对象库，按固定 key/version/字节 SHA-256/大小反读校验；结构化 JSONB 使用独立摘要并规范化数字，防止 PostgreSQL 将 `1e3` 转成 `1000` 后丢失语义关联。每份报告绑定解析器版本、目标、时间、源及 sim/real。采集失败、未知能力、身份缺失、对象未固定版本均不能变成完整采集，更不能生成 G1/NP-3 等门禁。

静态新鲜度读取参数目录 `asset.freshness_max_age`，缺省24h，半期aging；资产版本变化stale，失败/身份缺失unknown。前端只用本地时间更新显示，不自动重采；网络30s就绪门不复用该期限。

## 配置契约（仅说明，当前未启用）

API与worker均需显式 `KEEL_BMC_INVENTORY_ENABLED=true` 和 `KEEL_BMC_INVENTORY_FILE`；worker还需现有版本化对象库。配置为属主所有、0600或更严的绝对路径常规文件，禁止符号链接、未知字段与重复目标；示例只含公开引用：

```json
{"schema":"keel.bmc-inventory/v1","timeout_seconds":10,"targets":[{"project_id":"<32hex>","machine_id":"<32hex>","origin":"sim","credential_batch_ref":"<committed-BOM-batch>"}]}
```

`origin=sim` 且无endpoint时使用只有明确序列号的身份fixture，L2/L3缺硬件能力保持partial，不补造健康/CPU/GPU/NIC。协议mock可提供HTTPS `endpoint`和受控`ca_file`，仍必须标sim。real必须固定HTTPS来源，验证证书和主机名；读取不使用环境代理、不跟重定向、不允许跨源/任意查询/动作链接。可用的Machine BMC地址与固定来源独立比对。配置/CA指纹变化会阻断旧任务，不静默切换目标。

## 凭据批次

复用BOM的 `credential_batches.csv` 引用与scope，实际设备口令由服务器所有者通过CLI导入加密库，禁止明文进入BOM/Git/argv/审计。设备密码使用独立 `KEEL_DEVICE_CREDENTIAL_MASTER_KEY_FILE`，与SSH域钥主密钥维护分离。密钥可由已有 `credential-master-keygen --output <new-protected-path>` 独占生成；没有自动创建或覆盖。

`keel-admin device-credential-import --project ... --batch ... --scope bmc --version ... --expires-at ... --expected-current-version ... --reason ... --credentials-file <absolute-0600-json>`。文件只包含username/password，由CLI有界读取，输出只包含批次、版本、有效期、公开包装指纹及复验状态；本功能没有浏览器密码上传接口。`device-credential-revoke`指定同一作用域/精确版本及原因。表可承载switch批次，但交换机现有执行器不会因此自动改用新密码提供者。

版本通过AEAD绑定项目/批次/scope/版本/有效期，批次head用CAS更新。新任务固定新head，存量任务按旧版本直到过期/撤销，不回退至文件或其他版本。`credential_expired`单独分类并保留可重试标识。轮换后全量复验状态保持pending；当前尚无完整轮换批次复验/自动重试调度，不将单台采集作为轮换结束。

源迁移076保存不可变报告，077扩展现有执行模式及加密凭据/dispatch。授权脚本增加只读证据保护和凭据表服务端所有者写限制。**未执行迁移或授权脚本，未改变运行环境。**

## 后续与验证

手动执行、证据查询及UI有源码；cron/事件调度、IPMI回退、批次轮换全量复验、完整型号硬件覆盖及WP-2.2 JSONPath视图/导出待开发。G1、拓扑、四步G2和G3仍属其各自WP，不以接口存在冒充完成。

新增纯逻辑、HTTPS、加密、PostgreSQL租约/作用域/回执/凭据轮换用例均仅编写、NOT_RUN。按用户要求，重大里程碑集中测试；源码格式化和API生成成功不算测试通过。真实H-2/硬件结果NOT_RUN。
