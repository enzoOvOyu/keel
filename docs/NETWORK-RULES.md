# 网络规则治理模块

范围：FR-0.7 的 **LL-01～LL-07 无损规则治理增量**，不是全部 FR-0.7 验收完成。真实设备不参与当前框架验证；第三方 NOS 解析仍在采集适配器。

## 模块和接入

- `internal/fabricmon/rule_governance.go`、`governance_parameters.go`：不访问数据库/设备的语义冻结、单位与安全范围检查、候选条件归一化和结果差异。
- `governance-metrics.json` 定义派生输出的单位及默认安全范围；`governance-parameters.json` 定义可调内部条件、单位、安全范围与固定 AST 字面量位置。LL-02 的 `xoff_duration_ms`、`pause_frames_per_minute` 会修改真正的 XOFF/暂停帧判定，不修改布尔结果外层阈值。LL-03 支持队列字节/PFC 拥塞阈值，LL-01 支持升级阈值。布尔外层 comparator/hit 固定，避免把 0/1 包装层误当设备阈值。
- `internal/platform/network_rules.go`：当前项目授权、版本/签名/审计和发布；034 迁移保留不可变定义/预演报告、不可变独立签名、禁止物理删除；版本从内置 1 后单调递增。
- `network_rule_replay.go`：只读历史回放。当前 BOM/来源类别内加载最近48h样本，在每分钟实际观测点及配置快照边界分别运行当前/候选规则，返回带 `evaluated_at` 的新发现/解除/级别变化、受影响单位及最低分。应用配置只用于其采集时点之后且5分钟内，不反推过去配置。计数器空窗、重置、来源变化、历史缺配置均保留 unknown。
- `internal/httpapi/network_rules.go`：`/api/v1/projects/{id}/fabric/rules` 的目录/创建、`/preview`、`/{versionId}/approve`、`/publish`、`/rollback-candidate?target=default|{historicalVersionId}`。
- `web/src/features/network/NetworkRulesPage.tsx`：规则控件、同屏当前/候选对比、历史/签名、恢复候选；路由 `/p/{projectId}/rules`，从健康页进入，字体/布局复用工作区。

生效读取通过 `networkLosslessRulesTx` 接在健康计算的参数权重解析之后；无额外规则缓存，新请求立即读取发布结果，页面30s刷新。作用域仍由既有引擎按 device > port > priority > pod > role > fabric > default 选择，所有同级命中均参与最严判断；disabled 显式 unknown，不回退默认正常。

## 不变量

1. `rule_id`、subject、metric key、AST 形状/指标来源/事实路径、unknown 约束固定；只允许注册条件的数值字面量改变，不能插入代码、取消必需输入或换成常量成功。
2. 新规则范围必须解析到当前 BOM，priority 还要对应批准的无损意图。百分比与 ratio 分开验证；超出默认安全范围必须确认并保留理由。
3. 作者不得签署自己的候选。critical 变更按**当前版本和候选**中较高的级别要求独立 NE+NH；critical 降为 warning 或 disabled 不能绕过双签。发布时重新检查账号启用、security version 与当前项目/管理员权限。恢复/回滚同样走新候选、预演和审批。
4. 预演事务为 `REPEATABLE READ READ ONLY`，不写规则、审计、告警或事件。保存候选时才将预演报告与摘要写入不可变版本；审批绑定该摘要，发布前重核当前 BOM、批准意图、采集批次与生效规则/参数权重。
5. `replay_complete=true` 表示对本次可取得的样本完成计算，**不表示具有完整48h观测覆盖**；缺口在发现与 warnings 中保留。回放查询上限为每设备2881个采集样本/100000指标点、整批10000个端口×优先级检查点；超限明确413，不截断后声称完成。大规模异步回放尚未实现。

## 已知剩余边界

- optics/log/precheck 的覆盖版本还没有接入各消费端，目录只声明 `supported_domains=[lossless]`；保存后未被消费的领域不能标为生效。
- 当前安全范围来自数据注册表；尚未接入参数目录/项目模板对 safe_range 的覆盖。`between` comparator、可调持续窗口数与注册语义定义的独立审批仍待补；当前条件只允许注册位置，不开放任意 AST。
- BASE-08 的“预演调用审计归属”仍待统一。实现遵守 FR-0.7.6 纯计算零写入，未暗设审计写库例外；不据此宣称 FR-10.3 冲突已解决。
- 未执行真实硬件或2000台规模/异步历史回放性能验收；验证结果见本轮交接记录，不把 fixture 判为设备通过。

## 验证入口

```sh
go test ./internal/fabricmon ./internal/platform ./internal/httpapi -run 'Test(Governed|RulePreviewDiff|RuleQuorum)' -count=1
go vet ./internal/fabricmon ./internal/platform ./internal/httpapi
npm run --workspace=@keel/web build
python3 scripts/check-kea-integration.py --rules
```

最后一项复用既有隔离 PostgreSQL/API/协议夹具和浏览器；不会写运行项目、运行库或真实设备。必须在共享路由/健康消费接线以及034迁移进入测试构建后运行。
