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

**尚未交付**：真实DORA/lease验证、143/43/IPv6生产适配、真实活动引导路由与239/143入口准入接线、个性化最小管理渲染、凭据取用/轮换、无回执未知写入核实恢复。当前`config_persisted`绝不激活设备路由，也不投影managed/IN_SYNC/NP-2。PoC部署前按剩余功能核对；不能把本次协议桩连通写成设备开通验收。

## 验证与复现

```sh
npm run build --workspace=@keel/web
python3 scripts/check-kea-integration.py
# 执行内核变化时，追加已有模拟边界回归；24目标，不冒充2000规模验收。
python3 scripts/check-kea-integration.py --sim-regression
```

helper只使用带disposable标签的隔离PostgreSQL、`keel_test_`新库、回环API3102和生成的双向TLS协议桩；端口占用即停止，不接触3000运行库。BOM仅作输入fixture，后续基线导入/批准、介质独立批准、规划保存、自检、暂存、Execution/worker/回执和浏览器均真实执行。finally删除隔离库、服务及临时口令/私钥，保留脱敏证据。

已执行：Kea持久化协议/冲突/秘密与大整数保留测试通过；真实API/worker/数据库及TLS桩浏览器链通过，证据`/tmp/keel-shell-mmgiri2k`；结果未知转隔离并阻止第二次写入已验证。原执行器9组边界回归、24逻辑目标100%完成，通过证据`/tmp/keel-shell-g87izpv0`；不是2000规模或真机验收。Go vet/前端类型与构建通过。硬件PoC未执行。
