# A4 参数目录与制品引用准入

对应 PRD FR-1.8/1.9、ARCHITECTURE ADR-07、DESIGN-SYSTEM §4.10、实施计划 A4/WP-1.11、ACCEPTANCE F-1.8/F-1.9/U-4 的当前 sim 执行入口。不是整个 WP-1.11、真实设备分发或 P1 验收。

## 单一校验路径

- 目录版本选项只来自当前项目或全局的 approved、未退役介质，且必须存在与登记摘要/大小一致的不可变制品绑定。历史上仅凭 manifest 审批的记录不自动获得文件证据。
- 项目同名参数覆盖全局定义；项目定义退役后继续阻断，不能回退全局绕过。项目 API 不允许退役全局定义。
- 版本选择必须提供 `cpu.arch=amd64|arm64|noarch`；只允许该架构或 noarch 制品。目录支持 `?arch=` 过滤并返回硬件/OS、摘要、URI、对象版本和依赖元数据。同版本标签若匹配多个批准制品，提交拒绝，不能任意取第一条。当前版本参数的字符串语义保持不变；多产品场景须登记可区分的版本标签，后续可扩正式制品身份选择契约。
- 非版本目录键继续按其受控选项验证；注册键不能通过非字符串值绕过。未登记的 `.version` 键拒绝；普通 Flow 自有参数仍由其发布输入定义校验，查询/等待不要求虚构介质。
- 参数选择时与执行创建共用 `ValidateCatalogTx`。前者返回结构化问题及来源；后者在原创建事务中持有定义/介质共享锁，再读取每个选定固定对象的实际字节核对摘要/大小；缺存储、对象丢失或损坏均拒绝，失败不创建执行/outbox/审计成功记录。
- 快照新增可选 `catalog`，保存定义ID/更新时间/依赖与所选介质 URI/摘要/固定对象版本/架构/适用范围/矩阵。既有历史快照和其他业务 Flow 不被补写。新重跑重新校验当前准入，定义或制品与源快照变化时拒绝，要求创建经审阅的新执行；幂等重放仍返回原执行。

## 精确依赖格式

新介质登记、参数目录 upsert 的 `deps` 可以是空对象；非空时使用以下结构，不再接受会被静默忽略的任意 kernel/cuda 字符串。历史不符合新结构的依赖在新选择准入时阻断，历史记录保留，不擅自推导或改写。

```json
{
  "compatibility": {
    "source": "internal-validation-report",
    "version": "reviewed-matrix-1",
    "report_ref": "report:approved-fixture",
    "requires": {
      "cpu.arch": ["amd64"],
      "os.kernel": ["fixture-kernel-exact"],
      "cuda.version": ["fixture-cuda-exact"]
    }
  }
}
```

这只是格式示例，不是厂商兼容结论。`requires` 中每个依赖键按精确字符串校验：键之间 AND，同一键的多个可选值 OR，只适用于明确可独立组合的取值。需要表达关联时使用 `combinations`，禁止把多行投影成独立允许值后重新组合。

```json
{
  "compatibility": {
    "source": "internal-validation-report",
    "version": "reviewed-matrix-2",
    "report_ref": "report:approved-fixture-2",
    "requires": { "cpu.arch": ["amd64"] },
    "combinations": [
      { "os.kernel": "fixture-kernel-a", "runtime.profile": "fixture-tool-a" },
      { "os.kernel": "fixture-kernel-b", "runtime.profile": "fixture-tool-b" }
    ]
  }
}
```

语义为全部公共 `requires` **且**完整匹配 `combinations` 中至少一行。示例中的 kernel-a + tool-b 被拒绝；两条完整行分别允许。两类规则可以单独使用，至少一种非空。组合行限定1～128行，每行1～32个相同列名，值是非空精确字符串（最多120字节）；拒绝重复行、缺列/多列、非法键、非字符串值与未识别字段，整个依赖文档最多64 KiB。没有通配符、范围推断、自动补齐或厂商版本硬编码。

所有非CPU架构依赖都必须是有效目录键，且仍接受自身选项/介质/依赖准入；某行匹配不能绕过退役、不可选值、未批准制品或其他规则。不匹配返回 `PARAM_COMBINATION_REJECTED`，携带完整矩阵及来源/版本/报告；运行表单显示允许的整行组合并禁用提交。执行创建在原事务重新校验，失败不创建 Execution；成功快照保留原矩阵行和固定制品。新执行/重跑重新核验当前准入，历史快照不改写。

介质依赖在不可变登记元数据上随独立介质审批生效；参数级依赖由项目 Admin 配置并冻结时间戳。报告引用现阶段是溯源元数据，不代表自动验证报告内容/签名，也不提供额外的矩阵发布审批工作流。

## API 与页面

正式路径均以前缀 `/api/v1` 开始，沿用现有项目作用域 API；没有虚构独立 catalog 服务端点。

| 接口 | 行为 |
|---|---|
| GET `/projects/{id}/params?arch=amd64` | 当前有效定义、批准制品版本及 `media_options`；分页；只读；管理查询可加 `include_retired=true`，不改变执行选项接口的默认行为 |
| PUT `/projects/{id}/params` | Admin 注册/更新受控参数与精确依赖；原默认值准入保留 |
| POST `/projects/{id}/params/validate` | `{values:{...}}`；项目成员、CSRF/Origin；只做元数据校验，不创建执行；200 中 `valid/issues/selections` 表达合法/不合法组合 |
| GET `/projects/{id}/params/{keyName}/check?value=...&arch=...` | 单值检查；有组合依赖时须使用 validate 提交整组参数，不能凭单值忽略依赖 |
| POST `/projects/{id}/params/{keyId}/retire` | 原退役接口，新增全局作用域保护 |

`api/openapi.yaml` 补齐这些既有参数路由及新校验/元数据类型，执行快照扩展在 `api/execution.openapi.yaml`，前端类型由仓库命令生成。`ParamSelect` 共用 NativeSelect/Label，版本下拉显示适用架构/硬件与 approved，缺失版本禁用保留提示；登记链接进入当前项目介质页。执行列表与 Flow 工作区运行弹窗共用动态输入和实时依赖提示，校验加载或失败时不能提交；提交仍由后端重验，客户端结果不构成授权。

## 验证与边界

验证记录见 HANDOFF 最新节。复用隔离介质测试脚本，加入真实 PostgreSQL/MinIO/API/worker/Chromium 的参数组合、冻结快照和重跑测试；Go 边界测试还覆盖历史 approved 无制品、实际字节损坏、全局/项目覆盖和退役。数据库/对象传输 mock 的边界与真实 MinIO 证据分别记录，不混同。

当前 CPU 架构是已发布 Flow 中的显式参数；不能当作真实目标机架构探测或型号适配证据。硬件/OS适用范围目前是元数据；完整目标/BOM/型号版本交叉校验、兼容矩阵的审批报告治理、项目模板默认值同步、全业务消费者和编辑器/lint全量覆盖仍未完成。Agent/SSH分发与派发前门禁也须按其阶段接入，不能把 sim.read 的成功当作真实驱动安装或介质分发。当前运行库/3000发布版本与验证结果以HANDOFF为准。


## 项目参数目录管理

工作台“参数目录”入口为 `/p/{projectId}/params`，复用 ProjectWorkspace、URL 项目上下文和共享组件。按 DESIGN-SYSTEM §6，项目 DL、Admin 或平台管理员可查看有效及退役的当前定义、作用域、受控选项、目录默认值和完整兼容依赖；仅项目 Admin 或平台管理员可新增/修改/退役项目自身定义。继承的全局定义只读。列表仍采用“项目同名定义覆盖全局”语义，包括退役墓碑；不是所有历史修订的版本浏览器。

非版本参数在登记表单维护受控可选值；所有默认值采用下拉。版本参数不提供手工版本列表，默认值选项只显示批准且存在实际文件的介质；服务端保存时再次用目录规则核验。现阶段按介质类型同步，不代表已验证实际目标架构/型号。兼容依赖可导入已有 JSON 对象，仍执行公共精确矩阵校验；编辑不创建新的兼容性结论。

新增时页面提交 `create_only=true`；编辑时提交所见 `expected_updated_at`，两者互斥。后端按作用域/参数键串行化写入并锁定当前行；同一旧定义上的并发编辑只允许一个成功。冲突返回409 `PARAM_KEY_CONFLICT`，保留当前表单，需取消/刷新后重新核对。退役确认同样提交 `expected_updated_at`，陈旧确认不能退役已变化的定义。省略条件的旧API调用保留原upsert/retire语义，但仍与条件写入共用锁，不声称旧客户端全部已有冲突保护。更新保留真实created_at，updated_at严格递增；失败事务不追加成功审计。

退役显示明确影响确认，之后禁止编辑/恢复，当前退役定义仍可查看；新选择不能回退同名全局定义或继续使用退役值，已冻结执行快照不改写。查询缓存、表单、确认框绑定当前URL项目，切换项目或其他标签页不会转移未提交操作。中英文、只读角色、冲突与失败状态由现有工作台承载。

本块无数据库迁移，不实现全局目录管理页面、完整参数修订历史、项目模板默认值套用/同步或审批报告治理；目录默认值和模板默认值不能混称。完整 WP-1.11/FR-1/P1 仍按各自剩余验收推进。


## 既有网络消费者的默认值解析

ZTP批次与重试上限、网络推送canary/并发、DDM阈值/采样间隔、预检计数阈值及实时健康/规则预演权重统一解析当前项目定义，项目未登记该键时才继承全局定义。先选整个定义再解释值：被项目覆盖的全局值即使无效或退役也不参与解析；项目权重对象不会再和同名全局对象逐项混合。

有效定义已退役时返回409 `PARAM_KEY_RETIRED`，包含参数键，不回落全局或内置默认。只有项目与全局均未登记该参数时，消费者沿用原PRD默认值或缺阈值的unknown行为。新ZTP预览/提交、新推送创建和依赖该策略的当前查询执行此规则；已有执行/推送冻结的数值、ZTP计划和历史观测不改写，不宣称追溯取消所有进行中的任务。

解析在原事务中用同一个查询选择作用域，兼容repeatable-read只读预览，不加写锁或审计副作用。原数值范围、阈值顺序及独立规则审批约束继续生效。该功能块没有迁移，也不实现项目模板套用/同步、所有消费入口的依赖闭包、正式交付引用重整或派发前统一重验；这些缺口仍按所属WP推进。


## 全局参数目录管理

平台管理员从系统导航进入 `/admin/params`，复用项目目录的表格、编辑、依赖查看、默认值下拉及条件退役确认。项目Admin、DL和Viewer无全局入口，直接访问页面也不显示目录，所有全局接口独立拒绝非平台管理员。后端scope-less读取同样要求平台管理员；项目成员仍通过项目接口读取继承定义。

正式接口为 GET/PUT `/api/v1/admin/params`、POST `/api/v1/admin/params/{keyId}/retire` 和 GET `/api/v1/admin/params/media-options`。前三者复用原目录事务、create_only、expected_updated_at、审计及不可恢复退役，目标作用域固定global，不接受body指定scope_key。参数列表支持原分页/arch/include_retired；介质接口分页返回仅全局元数据（含状态），页面仅将approved且有真实不可变绑定的版本列为可选默认值，保存仍按全局介质重验。

全局目录不能借用某项目的批准介质，不能从全局路由修改项目参数或反向从项目路由退役全局参数。全局编辑/退役保留有效项目同名覆盖；没有项目覆盖的消费者按当前全局有效性准入，已冻结快照不回写。全局保存后使当前用户相关查询失效；跨标签页陈旧编辑仍由服务端条件写入拒绝。

全局版本默认值弹窗通过“登记新介质”链接进入 `/admin/media`，完成全局登记、实际文件上传、校验与独立审批后再选；只有平台管理员可管理全局介质。项目介质不能混入全局默认值。这里仍不实现目录修订历史、审批报告治理、全消费入口lint或项目模板默认值同步。无数据库迁移；WP-1.11/FR-1/P1仍partial。目录管理验证见HANDOFF §33，接续全局介质验证与发布见§35。


## 前端版本输入源代码门禁

`npm run check:versions --workspace=@keel/web` 使用仓库已有TypeScript AST，扫描src TSX中的原生/共享Input、Textarea及NativeSelect，识别id/name/value/变更回调/相邻标签中的version或“版本”；共享控件的命名别名、namespace导入和带版本引用的spread也检查。可识别的版本输入应使用ParamSelect，普通Flow修订下拉不属于介质版本输入。明确只读展示/隐藏字段允许保留；条件readOnly、条件disabled和可被后续spread覆盖的只读属性不能作为豁免。动态Flow渲染器另行检查：已登记键由ParamSelect承接，未知.version键在文本fallback之前直接返回禁用下拉；删除保护分支或改成可编辑选择会失败。

检查接入web typecheck，因此根npm run check、npm run build及CI现有make check均执行，并首先运行反例测试。违规返回非零和文件/行列；源码语法错误也失败。没有自动登记例外命令。例外清单位于web/scripts/version-input-exceptions.json，过期、重复或变更的例外失败：

- 正式介质登记仅允许MediaPage的RegisterMediaDialog中media-version元数据字段录入新版本，不豁免整个页面或执行表单。
- 既有mock Kea的kea-version是仿真DHCP option43意向字段，尚不是目录批准NOS制品选择；以精确文件/函数/id/控件摘要冻结，禁止改写或复制例外。本次不改该旧模拟链路的契约，后续接入目录/NOS准入后应移除此例外。

这是可识别控件与现有动态渲染器的防回归门禁，不能证明任意自定义包装器、无语义变量名、任意JSON/编辑器中的值或未来六类下发界面均满足准入。新增执行界面须继续人工走查和服务端准入；所有业务入口的语义覆盖、mock意向整改及ReadinessGate lint仍未闭环，E-2/U-4和P1不得据此标为完成。未改产品运行代码/API/schema，3000无需为本工具变更重发镜像。验证记录见HANDOFF §34。
