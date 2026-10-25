# Keel 共享 UI 组件整合记录

2026-10-24 在唯一开发目录 `/opt/keel` 接续历史保存提交 `3c489c5`，保留既有页面与设计令牌。历史工作树仅作只读资料。

## 实现边界

- React/TypeScript 下提供 16 个自有组件文件：Button、Input、Textarea、Label、Select、NativeSelect、Checkbox、Dialog、Tabs、Card、Table、Badge、Alert、Separator、Skeleton、Tooltip。基础样式与交互沿用保存的 shadcn/Radix 成果；NativeSelect 保留系统下拉选择语义，Checkbox 用于 DDM 指标选择。
- Tailwind v4 使用 `ui:` 前缀，不引入全局 Preflight。颜色、字体、圆角和尺寸映射到 Keel 现有设计令牌，保留明暗主题，当前字体为JetBrains Mono（下方2026-10-25增量）。构建配置、别名和 lockfile 已接线；CLI 仅为开发依赖。
- Preferences 与共享 ActionDialog 已迁移。确认框保留表单提交、忙碌/禁用/危险操作、Escape 忙碌保护、焦点约束与关闭后触发器焦点恢复；点击遮罩不触发提交或关闭。
- 新增执行视图、网络交付、PFC 聚合、物理诊断及 DDM 页面使用共享基础组件。矩阵保留状态颜色与 8/16/24/32px 尺寸；尝试详情保留右侧抽屉、标题及焦点恢复。本次基础之后的旧页面迁移见下方增量。
- `npm run check:ui --workspace=@keel/web` 使用 TSX AST 阻止业务代码新增原生控件；旧元素预算来自昨天保存版本，不增加预算绕过检查。共享组件内部允许原生元素，隐藏表单字段除外。

## 验证方法

`npm run build --workspace=@keel/web` 包含类型生成、设计令牌、路由、组件边界、TypeScript 与 Vite 构建。`python3 scripts/check-shadcn-integration.py` 在独立本地 5178 端口启动同源开发夹具，浏览器验证真实 Preferences/ActionDialog 的主题、字体、非透明背景、焦点约束/恢复、忙碌关闭保护和 Tabs，并保留截图；不需要 API 或数据库，结束后清理自身 Vite 进程。Radix 关闭生命周期结束后检查焦点，截图等待入场透明度恢复。

另外复用真实 API/隔离 PostgreSQL/worker/协议夹具的执行视图、网络交付与物理诊断集成脚本，验证替换后的业务交互。执行视图增加四档矩阵真实尺寸及右侧抽屉位置断言；DDM 覆盖真实 Checkbox 指标选择和 PNG 导出。具体证据目录记录在 HANDOFF §23。

本记录只覆盖已验证路径，不宣称全部旧控件迁移、所有基础组件无障碍审计或整个 U 系列/FR-1 验收完成。大型编辑器 bundle 提示仍存在。未部署或变更运行库，既有 026/fabric_precheck 草稿保留并在 Go 集成构建中通过既有 overlay 排除。

## 2026-10-25 旧页面接入与统一字体

原冻结清单的业务原生控件已经全部迁移到components/ui；登录、Shell、项目/成员/用户、BOM/资产、型号、介质、执行/Flow、Fabric/配置/规则/日志和ZTP均使用共享控件。ui-legacy-elements.json的预算现为零；隐藏表单字段仍允许。迁移保持原事件/权限守卫/验证字段，Table的scrollContainer=false用于已有滚动区域和虚拟表，避免双层滚动。NativeCheckbox保留原生onChange/表单及键盘语义，尺寸16px；Radix Checkbox继续用于既有交互。手写业务抽屉未借此次控件迁移改写，完整无障碍和规模验收仍需各自证据。

字体使用官方JetBrains Mono v2.304（400/500/600/700），来源、固定提交、SHA256与SIL OFL授权随web/public/fonts/jetbrains-mono保存；全局令牌、Monaco和DDM导出一致，禁用连字，中文系统回退。不依赖CDN，也不声称该字体是RunPod用字。旧Intel资源作为历史文件保留但不再由CSS请求。

新增scripts/check-ui-migration-integration.py在自有随机keel_test_ui数据库/API3109验证实际登录/项目表单、14个项目页面、checkbox键盘和尺寸、弹窗焦点、明暗/移动端及本地四字重/等宽度量；CI运行相同脚本。健康/交付的新项目证据缺口应展示明确阻断，不伪造健康或交付成功。具体实际结果见HANDOFF最新章节。
