# Keel 共享 UI 组件整合记录

2026-10-24 在唯一开发目录 `/opt/keel` 接续历史保存提交 `3c489c5`，保留既有页面与设计令牌。历史工作树仅作只读资料。

## 实现边界

- React/TypeScript 下提供 16 个自有组件文件：Button、Input、Textarea、Label、Select、NativeSelect、Checkbox、Dialog、Tabs、Card、Table、Badge、Alert、Separator、Skeleton、Tooltip。基础样式与交互沿用保存的 shadcn/Radix 成果；NativeSelect 保留系统下拉选择语义，Checkbox 用于 DDM 指标选择。
- Tailwind v4 使用 `ui:` 前缀，不引入全局 Preflight。颜色、字体、圆角和尺寸映射到 Keel 现有设计令牌，保留明暗主题和 Intel One Mono。构建配置、别名和 lockfile 已接线；CLI 仅为开发依赖。
- Preferences 与共享 ActionDialog 已迁移。确认框保留表单提交、忙碌/禁用/危险操作、Escape 忙碌保护、焦点约束与关闭后触发器焦点恢复；点击遮罩不触发提交或关闭。
- 新增执行视图、网络交付、PFC 聚合、物理诊断及 DDM 页面使用共享基础组件。矩阵保留状态颜色与 8/16/24/32px 尺寸；尝试详情保留右侧抽屉、标题及焦点恢复。既有其他页面按冻结清单渐进迁移。
- `npm run check:ui --workspace=@keel/web` 使用 TSX AST 阻止业务代码新增原生控件；旧元素预算来自昨天保存版本，不增加预算绕过检查。共享组件内部允许原生元素，隐藏表单字段除外。

## 验证方法

`npm run build --workspace=@keel/web` 包含类型生成、设计令牌、路由、组件边界、TypeScript 与 Vite 构建。`python3 scripts/check-shadcn-integration.py` 在独立本地 5178 端口启动同源开发夹具，浏览器验证真实 Preferences/ActionDialog 的主题、字体、非透明背景、焦点约束/恢复、忙碌关闭保护和 Tabs，并保留截图；不需要 API 或数据库，结束后清理自身 Vite 进程。Radix 关闭生命周期结束后检查焦点，截图等待入场透明度恢复。

另外复用真实 API/隔离 PostgreSQL/worker/协议夹具的执行视图、网络交付与物理诊断集成脚本，验证替换后的业务交互。执行视图增加四档矩阵真实尺寸及右侧抽屉位置断言；DDM 覆盖真实 Checkbox 指标选择和 PNG 导出。具体证据目录记录在 HANDOFF §23。

本记录只覆盖已验证路径，不宣称全部旧控件迁移、所有基础组件无障碍审计或整个 U 系列/FR-1 验收完成。大型编辑器 bundle 提示仍存在。未部署或变更运行库，既有 026/fabric_precheck 草稿保留并在 Go 集成构建中通过既有 overlay 排除。
