export const catalogManagementTranslations = {
 zh: {
  catalogGlobalTitle: '全局参数目录', catalogGlobalIntro: '仅平台管理员可管理全局定义。新消费在项目未定义同名键时继承；已有项目覆盖和历史快照保持不变。', catalogGlobalEditable: '全局', catalogGlobalRetireDetail: '在全局作用域退役参数 {{key}}。', catalogGlobalRetireImpact: '退役后，依赖该全局定义的新选择和当前策略消费被阻断；有效的项目同名覆盖继续生效。历史快照保留，退役不可恢复。',
  catalogTitle: '参数目录', catalogIntro: '管理当前项目的受控参数、默认值和兼容依赖。全局定义仅供查看，项目同名定义优先；目录默认值不代表项目模板已套用。',
  catalogCreate: '新增参数', catalogEdit: '编辑参数', catalogSave: '保存参数', catalogSaved: '参数目录已更新。', catalogRetire: '退役参数', catalogRefresh: '刷新目录',
  catalogKey: '参数键', catalogType: '类型', catalogValues: '可选值', catalogDefault: '目录默认值', catalogScope: '作用域', catalogState: '状态', catalogActions: '操作',
  catalogGlobal: '全局 · 只读', catalogProject: '当前项目', catalogActive: '有效', catalogRetired: '已退役', catalogSource: '介质类型', catalogOptions: '受控可选值（每行一个）', catalogNoDefault: '不设置默认值',
  catalogDependencies: '兼容依赖', catalogEditorDetail: '登记参数定义；版本可选值来自已批准制品。修改不会改写历史执行快照。',
  catalogVersionSource: '版本选项由已批准且已有文件的介质同步，不支持手填版本。',
  catalogDependencyHelp: '无依赖填 {}。兼容依赖使用 compatibility，包含 source、version、report_ref，以及 requires（逐键允许值）或 combinations（完整组合行）。',
  catalogInvalidDependencies: '兼容依赖须为不超过 64 KiB 的 JSON 对象。', catalogConflict: '参数定义已变化或同名参数已存在。请取消并刷新目录后重新核对，当前填写内容不会自动覆盖。',
  catalogRetireDetail: '在项目“{{project}}”退役参数 {{key}}。', catalogRetireImpact: '退役后禁止新增引用，历史执行快照保留；不能恢复有效状态。项目定义退役后不会回退同名全局定义。',
 },
 en: {
  catalogGlobalTitle: 'Global parameter catalog', catalogGlobalIntro: 'Platform administrators manage global definitions. Projects inherit them only without their own definition; project overrides and historical snapshots remain unchanged.', catalogGlobalEditable: 'Global', catalogGlobalRetireDetail: 'Retire parameter {{key}} in the global scope.', catalogGlobalRetireImpact: 'New selections and current policy consumers using this global definition are blocked. Active project overrides remain effective. Historical snapshots are retained; retirement cannot be undone.',
  catalogTitle: 'Parameter catalog', catalogIntro: 'Manage controlled project parameters, defaults and compatibility dependencies. Global definitions are read-only; project definitions take precedence. Catalog defaults do not imply that a project template has been applied.',
  catalogCreate: 'Add parameter', catalogEdit: 'Edit parameter', catalogSave: 'Save parameter', catalogSaved: 'Parameter catalog updated.', catalogRetire: 'Retire parameter', catalogRefresh: 'Refresh catalog',
  catalogKey: 'Parameter key', catalogType: 'Type', catalogValues: 'Options', catalogDefault: 'Catalog default', catalogScope: 'Scope', catalogState: 'State', catalogActions: 'Actions',
  catalogGlobal: 'Global · read-only', catalogProject: 'Current project', catalogActive: 'Active', catalogRetired: 'Retired', catalogSource: 'Media type', catalogOptions: 'Controlled options (one per line)', catalogNoDefault: 'No default',
  catalogDependencies: 'Compatibility dependencies', catalogEditorDetail: 'Register a parameter definition; version options come from approved artifacts. Changes preserve historical execution snapshots.',
  catalogVersionSource: 'Version options sync from approved media with stored files. Manual version entry is unavailable.',
  catalogDependencyHelp: 'Use {} for no dependencies. compatibility contains source, version, report_ref and requires (independent allowed values) or combinations (complete rows).',
  catalogInvalidDependencies: 'Dependencies must be a JSON object within 64 KiB.', catalogConflict: 'The definition changed or the key already exists. Cancel and refresh the catalog before reviewing again; your form will not overwrite it automatically.',
  catalogRetireDetail: 'Retire parameter {{key}} in project “{{project}}”.', catalogRetireImpact: 'Retirement blocks new references and preserves historical execution snapshots. Retired parameters cannot be reactivated. A retired project definition never falls back to a global definition with the same key.',
 }
}
