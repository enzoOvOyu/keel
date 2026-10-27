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

## IPMI 兼容回退（源码，PoC 待执行）

只有Redfish根服务超时、连接不可达、5xx或版本/资源不支持才尝试已显式配置的IPMI；401/403、凭据过期与TLS验证失败不会降级绕过。仍使用当前Execution原命令、同一个BMC独占租约、冻结批次版本和15分钟总期限，不另起设备执行器或重放unknown。

每个目标可追加 `ipmi:{"address":"<same-literal-BMC-IP>","tool_path":"/usr/bin/ipmitool","tool_sha256":"<exact-binary-SHA256>"}`。IPMI地址必须与HTTPS固定来源为同一字面量IP；端口固定623，传输固定lanplus/cipher17/USER，不接受浏览器命令、OEM raw、电源或配置操作。工具必须为root/当前属主、不可组/全局写的常规可执行文件，每次调用重新验证摘要并从固定文件描述符执行。密码经封印匿名memfd传给`-f`，不进入argv/环境/临时磁盘文件；超时杀进程组并等待退出后才有停止回执。支持的IPMI口令格式为1～20字节、用户名1～16字节，无换行/NUL，不支持的格式保留unsupported，不截断。

固定读取清单只有Device ID、System GUID、Chassis Status和FRU0二进制读取。回复字节以JSON envelope保留hex、解析版本和已解析字段，与Redfish同样进入版本化对象库/JSONB；`/ipmi/v2/inventory/...`仅是固定证据标识，HTTPS客户端不能请求这些路径。FRU先验证公共头、区域边界/校验和和字段长度，只有Product Serial Number可确认服务器身份，Board SN不代用。`ipmitool fru read`的exit0不足以证明成功，二进制仍须独立解析校验。IPMI不会补造Processors/Memory/网卡能力，降级报告保持partial/failed，并在manifest/UI保留原Redfish原因和IPMI数量。

镜像源码增加固定Debian bookworm `ipmitool=1.8.19-4+deb12u2`；当前未构建或安装。未来镜像实际工具摘要仍由配置明确固定，无自动探测/启用。开发sim可使用属主0600的`redfish_fixture_file`（schema`keel.redfish-fixture/v1`）与`ipmi.fixture_file`（schema`ipmi-inventory/v1`）；样本在`sim/fixtures/bmc/`，复制为受保护文件后必须匹配其明确的模拟SN。fixture内容摘要进入adapter binding，真实来源拒绝fixture。协议HTTPS mock与结构化sim共用收集/原始证据/失败/执行消费链。

协议依据：[ipmitool手册](https://github.com/ipmitool/ipmitool/blob/master/doc/ipmitool.1.in)、[FRU读取实现及exit状态限制](https://github.com/ipmitool/ipmitool/blob/master/lib/ipmi_fru.c)、[Intel FRU区域格式](https://www.intel.com/content/dam/www/public/us/en/documents/specification-updates/ipmi-platform-mgt-fru-info-storage-def-v1-0-rev-1-3-spec-update.pdf)、[Debian包版本](https://packages.debian.org/bookworm/ipmitool)。本节为实施契约，不是已完成硬件适配/安全PoC证据。

## 凭据批次

复用BOM的 `credential_batches.csv` 引用与scope，实际设备口令由服务器所有者通过CLI导入加密库，禁止明文进入BOM/Git/argv/审计。设备密码使用独立 `KEEL_DEVICE_CREDENTIAL_MASTER_KEY_FILE`，与SSH域钥主密钥维护分离。密钥可由已有 `credential-master-keygen --output <new-protected-path>` 独占生成；没有自动创建或覆盖。

`keel-admin device-credential-import --project ... --batch ... --scope bmc --version ... --expires-at ... --expected-current-version ... --reason ... --credentials-file <absolute-0600-json>`。文件只包含username/password，由CLI有界读取，输出只包含批次、版本、有效期、公开包装指纹及复验状态；本功能没有浏览器密码上传接口。`device-credential-revoke`指定同一作用域/精确版本及原因。表可承载switch批次，但交换机现有执行器不会因此自动改用新密码提供者。

版本通过AEAD绑定项目/批次/scope/版本/有效期，批次head用CAS更新。新任务固定新head，存量任务按旧版本直到过期/撤销，不回退至文件或其他版本。`credential_expired`单独分类并保留可重试标识。轮换后全量复验状态保持pending；当前尚无完整轮换批次复验；策略可对带停止回执的可重试失败有限重试，不将单台采集作为轮换结束。

源迁移076保存不可变报告，077扩展现有执行模式及加密凭据/dispatch，078增加策略/领域事件与不可变触发记录。授权脚本增加只读证据保护和凭据表服务端所有者写限制。**未执行迁移或授权脚本，未改变运行环境。**

## 自动采集策略（源码，尚未启用或集中验证）

资产采集对话框的“自动采集策略”按需加载策略和历史。每台资产最多12条，各级可分别选择 UTC 五字段cron或业务事件；创建/编辑须当前Admin/DE/DL/SRE，DE只能编辑自己创建的策略。所有修改需原因、Idempotency-Key和当前state_version；创建者不可静默替换。修改后从当前事件游标/下一次cron开始，无历史回补。停用阻止后续触发，不自动取消已准入Execution，后者仍用执行页暂停/取消。

`.../assets/{asset}/bmc/policies` GET/POST、`/{policy}` PUT、`/{policy}/runs`分页查询。cron支持数字、通配符、列表、范围和步长，星期0/7为周日；DOM/DOW均限制时取并集，星号开头的日字段保留通配语义。错过多个cron周期合并为一次准入，下一次按当前时刻计算。事件仅由BOM提交、BMC批次新版本导入和装机注册成功事务产生；没有浏览器任意事件注入入口。BOM事件限定项目，装机事件限定Machine，凭据事件限定当前BOM批次。事件序号按项目锁定到提交，避免迟提交事件被游标越过。

worker启用BMC后每5秒有界扫描（每批20条、3秒上下文），创建普通单目标Execution；不建第二套设备执行队列，也不在策略事务中读取密码或连接硬件。策略每次触发重新核验当前创建者启用状态、项目成员角色及生命周期；权限撤销记录阻断并停用。未启用适配器时不扫描。原始准入冻结策略/触发记录、当前资产/BOM/凭据head；后续编辑不改历史。

重试默认0，可显式设置0～3次与30～3600秒间隔。仅终态failed、durable stopped回执、原始不可变报告含retryable失败才允许追加普通Execution；新任务重新准入当前资产/BOM/凭据版本，保留不可变父记录。同一父记录至多一个子记录；unknown/reconciling、cancelled、证据缺失或仅有不支持错误均不自动重试。同策略已有未结束任务/准入失败保留blocked记录；不同级别的独立策略可分别准入，由共享BMC通道锁串行执行；不会把阻断写成采集成功。批次全量重验仍pending，单台成功不结束轮换窗口。

## 后续与验证

手动执行、证据查询、cron/事件/有限重试、受限IPMI回退及UI已有源码；批次轮换全量复验、完整型号硬件覆盖及WP-2.2 JSONPath视图/导出待开发。G1、拓扑、四步G2和G3仍属其各自WP，不以接口存在冒充完成。

新增纯逻辑、HTTPS、加密、PostgreSQL租约/作用域/回执/凭据轮换用例均仅编写、NOT_RUN。按用户要求，重大里程碑集中测试；源码格式化和API生成成功不算测试通过。真实H-2/硬件结果NOT_RUN。
