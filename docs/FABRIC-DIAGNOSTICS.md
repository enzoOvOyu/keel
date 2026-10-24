# N-S5 物理排障与 DDM 趋势

本模块补 FR-0.5 的 T4/T6、显式 rail 意图核对及 DDM24/48h趋势；它提供当前网络侧排障判定，不替代 G2 主机三方确认、完整工单或网络交付基线归档。设备协议边界可用可信采集器替换，当前验证使用独立测试库/3105 API 与 mTLS 夹具。

## 代码边界

- `internal/fabricmon/diagnostics.go`：纯判定。物理协商/介质与 CRC/抖动增长映射 T4；应用 NVUE 配置与批准意图不符映射 T6；明确 rail→Leaf 对应不符映射 T2/rail。
- `internal/platform/fabric_diagnostics.go`：当前 BOM、批准逐 SN 配置变量、当前/基准不可变采样、参数目录阈值、当前项目授权和查看审计；返回本次批准意图与阈值策略摘要。
- `fabric_ddm_trends.go`：读取既有 `fabric_collection_runs/fabric_switch_samples` 历史；不新增采样表或阈值表。当前参数阈值用于趋势参考线和本次查询的解释，不重写历史归档判定。
- `internal/httpapi/fabric_diagnostics.go`：设备作用域 `/diagnostics?port=`、`/ddm-trends?port=&hours=24|48`、`POST /ddm-trends/export`。
- 前端 `features/fabric/DDMTrendsPanel.tsx` / `ddm-plot.ts` / `FabricDiagnosticsPanel.tsx`：沿用8Tab抽屉；运行/配置按需读取诊断，光模块按需读取历史。端口、窗口、叠加指标保留在 URL。`FabricControls.tsx` 隔离稳定控件，遵守用户暂停组件库迁移的新要求。

## 适配器新增可选字段

`fabric-snapshot/v1` 保持兼容，增加：

```json
{
  "capabilities": {"interface_counters": true, "physical_link": true, "nvue": true},
  "ports": [{"name":"swp1", "counter_epoch":"boot-id/reset-generation", "crc":9007199254740993, "flaps":0, "duplex":"full", "media_class":"optical"}],
  "applied_interfaces": {"swp1":{"speed_mbps":100000,"mtu":9216,"vlans":[],"pfc_priorities":[6],"autoneg":true}}
}
```

`counter_epoch` 必须来自设备启动/清计数纪元；不知道就留空，不能伪造稳定值。CRC/抖动计算要求明确计数器能力、同 Provider/来源/设备/纪元、未回退的 uint64 计数和完整窗口；reset/来源变化/能力缺失均 unknown。API增长值与上限以十进制字符串展示，保留大于2^53的单次增长精度。

物理速率取双端协商观测，双工/介质需 `physical_link` 能力；只比较可信的实际邻居，不拿错接链路与 BOM 期望对端的物理参数拼接。T6 只使用 `applied_interfaces` 已应用配置，不拿协商速率冒充配置。空 VLAN/PFC 集合与缺字段分别表示“已知为空”和 unknown。

## 批准意图与参数

批准基线的 `engineering_limits.configuration_variables[SN].interfaces[port]`：

```json
{"speed_mbps":100000,"mtu":9216,"vlans":[],"pfc_priorities":[6],"autoneg":true,"rail_id":"customer-blue","leaf_serial":"CUSTOMER-LEAF-SN"}
```

rail_id 和 leaf_serial 必须明确提供，不从 Leaf 名称/编号猜 rail。缺任何一项保持 unknown。服务器端口仍为网络侧 pending，NP-3 固定 BLOCKED，不能把预校验写成主机线序成功。

所有阈值通过已有参数目录读取 global→project 覆盖：

| 参数键 | 含义 |
|---|---|
| `fabric.precheck.counter_window_seconds` | 增长比较窗口，1～3600秒 |
| `fabric.precheck.crc_growth_max` | 窗口内允许 CRC 增长，非负 uint64 |
| `fabric.precheck.flap_growth_max` | 窗口内允许 flap 增长，非负 uint64 |
| `fabric.ddm_interval` | 正数秒或 duration；缺失时只显示离散点，不连线 |
| `fabric.ddm.{metric}.{warn_min|warn_max|critical_min|critical_max}` | 复用既有 DDM 阈值读取和判定 |

没有阈值不私设默认0。项目模板通过既有项目参数策略提供值，不建立第二套阈值存储。未使用 BOM 端口不产生物理/光模块告警，仍可查看其测量历史并显式标为未使用。

## 趋势与 PNG

每条历史点保留时间、模块 SN、Provider、不可变快照摘要和 source=switch；同时间冲突观测保持 unknown。仅在采样间隔已知、同模块/同来源且无空窗时连线，换件/来源变化/缺值不插值或补零。叠加指标使用独立纵轴，图例列出范围与单位，避免把 dBm/°C/V/mA 直接共用数轴。

PNG 由浏览器原生 Canvas 绘制，使用当前主题和 Intel 字体。下载前向服务端提交当前数据摘要；服务端重核数据/阈值/项目权限并写导出审计，数据变化返回409，不能导出旧图后声称新证据。该审计是导出授权/数据引用证明，**未保存 PNG 文件或声称完成 CMDB/证据包归档**。

## 验证与未覆盖范围

```sh
go test ./internal/fabricmon ./internal/platform ./internal/httpapi -run TestPhysical -count=1
go vet ./internal/fabricmon ./internal/platform ./internal/httpapi
npm run --workspace=@keel/web build
python3 scripts/check-kea-integration.py --diagnostics
```

最后一项使用独立3105、随机隔离测试库，不能对运行库执行。原有非 diagnostics 检查仍默认3102。

本包未交付实际采集调度、90天原始/18个月降采样保留任务、全端口规模/P95性能证明、自动工单创建或真实硬件验收。历史查询明确限制17281个设备采集快照，超限413，不静默截断。单端口响应最多2000点：高频历史按桶保留五个指标各自极值和首末原始观测，显示原始点数与采样提示；原始证据不删除。服务端按全部原始观测核对连续性，省略点内出现的换件、来源变化、缺值或空窗仍使曲线断开，不能仅按返回点间距推断连续。导出摘要绑定完整原始历史，未显示点变化也会使旧图导出失效。窗口覆盖不足单独提示，不能把几分钟观测标为完整24/48h。完整预检规则治理/预检版本审批仍需后续领域接线；此处阈值来自现有参数目录，结果携带语义规则 ID 与本次策略摘要。

2026-10-24 已在唯一开发目录 `/opt/keel` 完成整合与验证：fabricmon 全包 race、platform 的 DDM 边界测试、相关三包 vet、契约生成/TypeScript/Vite 构建通过。实际隔离 API/PG/mTLS 夹具/浏览器证据 `/tmp/keel-shell-t_fzzenx`，覆盖大于2^53的 CRC 单次增长、窗口/纪元/来源保护、T4 与 T6 分离、批准 rail 意图、未使用端口豁免、24/48h 趋势/当前阈值、导出摘要拒绝/审计、Viewer 与跨项目限制、8Tab抽屉、多指标 URL 筛选及原生 PNG。5000点单测验证返回≤2000点、极值保留、隐藏断点不连线、原始证据不被修改。026 草稿用 build overlay 排除；未改生产数据、未部署，也未声明完成 P95/容量或硬件验收。
