# Kea 控制作业与 Runner

WP-1.17 当前增量：把 N-S2/N-S3 准备结果接入真实 Kea 控制协议及共享 Execution。依据 PRD FR-0.3、开发入口和实施计划；本文件只记录实现/运行方法，不代表 FR-0 总体完成。

## 已实现路径

`批准的管理规划 + 新鲜N-S3 + 独立批准隔离脚本 + SoT管理MAC → config-get/config-test暂存 → Execution入队 → Worker领取/资源租约 → 重验准入/活动摘要 → config-test/config-set → 活动摘要/完整内容读回 → config-write → 最后摘要读回 → 持久回执/完成`

协议依据：[ISC Kea API](https://kea.readthedocs.io/en/kea-3.2.0/api.html)、[二进制 DHCP 选项](https://kea.readthedocs.io/en/kea-3.2.0/arm/dhcp4-srv.html)。config-write只使用Kea启动配置路径，不接受浏览器文件名；命令成功确认及随后活动读回不等于恢复演练或DORA通过。

| 修改内容 | 入口 |
|---|---|
| mTLS控制协议、持久化/读回、写入不明分类 | `internal/fabricboot/kea_deployment.go`、`kea_control.go` |
| 保留现有配置、只合并目标子网239预留 | `MergeBootstrap239` |
| 批准规划/环境/介质/MAC准入、不可变暂存、配置作业证据 | `internal/platform/ztp_kea_deployment.go` |
| 原子Execution/目标/任务/outbox创建、Runner回执 | `internal/execution/kea.go` |
| 统一Claim/心跳/资源租约/隔离/收敛 | `internal/execution/worker.go`、`sim.go` |
| UI、正式HTTP类型与入口 | `KeaDeployments.tsx`、`ztp_kea_deployment.go`、两份OpenAPI |

## 状态与数据边界

- 任务复用原Execution、attempt、outbox、fencing、receipt和资源租约。`mode=kea`与`sim`明确分开。新增`exec_targets`统一目标注册，不把Kea写入仿真库存；旧sim目标经迁移/触发器关联，原模拟注册与执行入口继续只处理sim。
- `ztp_kea_deployments`：`staged → queued → applying → config_persisted`；准入/预写失败为`blocked`；任何可能已写入而结果不明为`unknown`。该表只存批准事实、MAC/地址/URL、摘要和执行引用；不存完整Kea配置。完整配置可能包含后端凭据，只在内存中读取/合并，不进入API、快照、回执或日志。
- 同一控制端的任务用`kea:<controller_key>`独占锁，跨项目共享；端点配置由服务器运维固定。切换控制端别名/代理拓扑前须排空/核实存量任务，不能用改URL绕过资源隔离。
- NDE/项目Admin/平台Admin可发起；只读角色可查看。提交与Worker调度重验账号、项目和角色；仿真创建接口不能执行系统Kea流程。普通DE/SRE不能因拥有模拟控制权限而取消/恢复Kea任务。
- 写入不明不生成“失败/已停止”的虚假回执。心跳停止后由既有reaper转`reconciling`，资源保持隔离，阻止后续写入；不自动重试config-set。已有真实持久回执的迟到完成继续走原显式核实流程。无回执未知写入的主动读回/修复工作流尚未交付，不能手工填PASS释放锁。

## 配置与PoC边界

复用`KEEL_FABRIC_PROBES_FILE`的Kea目标，运维必须显式设置：

```json
{"version":"fabric-probes/v1","targets":{"kea":{"url":"https://kea.example.invalid/","ca_file":"/run/keel/fabric/ca.pem","certificate_file":"/run/keel/fabric/client.pem","key_file":"/run/keel/fabric/client.key","through_agent":true,"allow_config_write":true,"write_timeout_seconds":15}}}
```

上面仅展示Kea项，完整配置仍需boot/collector/repository才能取得四项全绿报告，见[准备模块](ZTP-PREPARATION.md)。启用Kea时为worker设置至少130秒停止宽限，以容纳120秒任务上限；非正常终止仍按未知写入隔离。API与worker使用相同配置及受限证书挂载；N-S3配置指纹与Worker重验不一致时阻断。配置文件600，证书私钥受限，路径须在各自容器中可读。写请求默认15秒、可配置1～60秒；探针与写请求分开限时，写连接不留空闲连接。默认Compose不挂载这些配置，不启用真实写入；可在独立PoC override中为API/worker同时只读挂载配置并传入该环境变量，秘密不提交Git。

当前适配器只处理已有顶层`Dhcp4.subnet4`管理网中、带唯一SoT管理MAC的Cumulus 239隔离脚本预留。保留其他子网、全局设置和第三方预留；冲突、外部host数据库、独立附加配置均显式拒绝。完整内容读回采用严格规范化JSON比较，实际Kea默认字段规范化差异需在PoC记录原始脱敏样本并改协议适配器，不能放宽准入。媒体必须与服务端生成的已批准隔离脚本SHA256/大小一致，不承接生产网络配置。

**尚未交付**：租约后端独立查询、IPv6生产适配、凭据完整生命周期、无回执未知写入核实恢复（个性化最小管理渲染与可管理确认已接续，见ZTP-COMPLETION）。新增独立DORA探针和239真实路由，`boot_ready`才允许引导；`config_persisted`绝不激活设备路由，也不投影managed/IN_SYNC/NP-2。PoC部署前按剩余功能核对；不能把本次协议桩连通写成设备开通验收。

## 验证与复现

```sh
npm run build --workspace=@keel/web
python3 scripts/check-kea-integration.py
# 执行内核变化时，追加已有模拟边界回归；24目标，不冒充2000规模验收。
python3 scripts/check-kea-integration.py --sim-regression
```

helper只使用带disposable标签的隔离PostgreSQL、`keel_test_`新库、回环API3102和生成的双向TLS协议桩；端口占用即停止，不接触3000运行库。BOM仅作输入fixture，后续基线导入/批准、介质独立批准、规划保存、自检、暂存、Execution/worker/回执和浏览器均真实执行。finally删除隔离库、服务及临时口令/私钥，保留脱敏证据。

已执行：Kea持久化协议/冲突/秘密与大整数保留测试通过；真实API/worker/数据库及TLS桩浏览器链通过，证据`/tmp/keel-shell-mmgiri2k`；结果未知转隔离并阻止第二次写入已验证。原执行器9组边界回归、24逻辑目标100%完成，通过证据`/tmp/keel-shell-g87izpv0`；不是2000规模或真机验收。Go vet/前端类型与构建通过。硬件PoC未执行。

## DORA 与真实239路由（新增）

运维配置可增加 `dhcp`：`server`（IPv4:端口）、`listen`（本地IPv4:端口）、`relay_ip`（与本地监听一致）、`mac`、`address`、`timeout_seconds`（默认8，上限20）。这是明确预留的独立探针，不使用设备MAC/IP；地址必须在批准管理网内，排除网关/网络/广播/规划中设备分配，MAC不得命中SoT。生产relay按RFC2131绑定UDP67，需独立管理网权限/路由；测试桩使用回环高端口，不能把测试端口用于实际Kea。探针预留连同设备预留一起参与原审批事实、暂存摘要、config-test及配置持久化，不能由浏览器临时指定。

同一Runner在持久化后发送真实Discover/Request并验证Offer/Ack：对端、xid、chaddr、giaddr、分配IP、server-id、有效租约时间及239 URL全部核对，最终配置摘要再次读回。证据只存摘要/身份/租期/时间，不存原始配置。报文过载/重复选项目前显式拒绝，PoC差异在此适配层扩展。DORA失败保留`config_persisted`与错误分类，不产生路由或managed；配置作业成功回执不等于引导验收成功。

`boot_ready`记录不可变DORA证据。239专用TLS入口读取当前Kea摘要、当前管理规划与配置指纹，并沿用媒体批准/脚本摘要/身份冲突门禁；已知设备还需匹配本批次管理源IP与MAC。最近配置作业DORA失败会使旧同摘要路由失效；控制端正在写入或结果未知时拒绝引导。启用真实控制后239/143不回退mock路由；缺143专门配置与DORA证据时拒绝，不能以239的证据冒充143。未启用真实控制时原mock契约链仍单独保留。

验证：DHCP UDP报文/取消/反例及Go定向检查通过；API+Worker+数据库+双向TLS/UDP协议桩及浏览器链通过，证据 `/tmp/keel-shell-w792odlk`。同一Worker产生的证据通过真实Store239入口验证：正确身份/源准入、异网段拒绝、最新DORA失败使旧路由失效、未知写入拒绝。前端构建和Go vet通过。未连接真实Kea/交换机，硬件PoC仍NOT_RUN。

## 143双通道接线（新增）

在探针targets中另配`sztp`：HTTPS的专用143 mTLS `/readyz`地址、显式CA及客户端证书/密钥。N-S3对239与143分别自检，双通道均须有效且绑定当前规划/配置；143报告引用与239报告一起冻结到Kea暂存。未配置sztp则保持仅239，不静默安装指向239 listener的143选项。

RFC8572§8.3采用每条URI的2字节网络序长度前缀；Kea预留写入二进制143，与239共用原作业和控制端锁。当前v4适配显式限单选项255字节，超过拒绝；纯编解码亦提供v6选项136封装，但不代表DHCPv6控制/租约适配已完成。DORA同时核对239与143真实返回值；仅239的旧证明不能开放143路由。

143 mTLS/IDevID与进度回执入口现使用相同的真实活动Kea、规划与媒体准入。按已验证URI及直连管理源反查唯一项目/批次，再核对证书序列号对应的本批次设备源地址；冲突/歧义/最新DORA失败/控制端未知均拒绝。bootstrap-complete仍为设备自报事件，不自动等同managed/NP PASS。239和143共用身份与批准隔离数据，个性化管理、凭据与实际可管理确认仍待后续。

补充防护：最终DORA证据落库前重新读取并锁定执行意图、账号是否启用/是否平台管理员及项目成员资格；途中取消/撤权不开放新路由。只要配置了真实Kea端点，关闭写入也不会重新开放mock路由。

已验证：239+143协议桩DORA和真实API/Worker/数据库浏览器链，证据`/tmp/keel-shell-7ly7d7cy`，含Store真实143准入、错误设备源拒绝、陈旧/未知写入阻断。URI二进制编码/136封装/独立选项保护与DORA错误143负例定向检查已执行。协议依据：[RFC8572§8](https://www.rfc-editor.org/rfc/rfc8572.html#section-8)。未做真机PoC。

## 显式厂商60/43契约（2026-10-26，源码未验证）

`fabric-probes/v1`增加可选的顶层`dhcp_vendor`：`version=dhcp-vendor-tlv/v1`、`source_ref`、`vci`、`nos`、`version_intent`、`nos_suboption`、`version_suboption`。后两个编号必须由目标厂商契约/PoC明确提供，范围1～254且不同；不提供Cumulus或其他厂商的猜测编号/默认格式。`source_ref`是公开协议出处，不填秘密。这里仅支持RFC2132的两项code/length/value文本子选项，总包络≤255字节；其他厂商编码需独立适配。

配置在API/worker加载时校验并复制，完整内容进入N-S3指纹和暂存spec摘要；来源或格式改变，旧自检/待执行快照不能继续。真实reservation增加二进制option43及`keel-vendor-profile`所有权摘要，沿原config-test、共享Execution、控制端资源锁、持久化/读回路径执行。既有平台239/143预留可显式升级；第三方43、附加字段、全局/类/其他层自定义43或默认vendor子空间定义均拒绝，不重定义其他客户的协议解释。已有厂商标记不允许通过关闭配置静默删除。

option60按[RFC2132§9.13](https://www.rfc-editor.org/rfc/rfc2132.html#section-9.13)由客户端发送，服务器使用43返回厂商信息；不伪造server echo，不将VCI作认证凭据。独立canary的Discover/Request发送冻结VCI并请求43，Offer与Ack均必须实际返回精确冻结43字节；回执记录发送的VCI、观测43 hex、profile摘要和原始报文转录摘要。只有这些证据与spec一致，才可开放对应真实引导路由。未配置profile保留既有239/143功能，页面明确不证明43已配置/生效。当前适配不对未知VCI客户端宣称分类隔离，也不证明设备理解了NOS意向、安装了该版本或已经可管理。

协议依据：[RFC2132§8.4](https://www.rfc-editor.org/rfc/rfc2132.html#section-8.4)、[ISC二进制选项与厂商格式](https://kea.readthedocs.io/en/kea-2.6.3/arm/dhcp4-srv.html)。新增契约/所有权/配置指纹和真实UDP协议fixture反例源码；按用户安排未执行。仅格式化和API类型生成，不代表Kea版本支持或硬件PoC通过。
