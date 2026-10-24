# P1 介质实际文件与批准读取

对应 PRD FR-1.8、ARCHITECTURE ADR-07/[I-2]、DESIGN-SYSTEM 介质库、IMPLEMENTATION-PLAN A4/WP-1.10、ACCEPTANCE F-1.6/F-1.8 的文件存储与读取部分；不是完整介质分发或 P1 验收。

## 已实现的链路

- 保留登记 API；新增 multipart 上传新版本，由服务端从文件实际计算 SHA256/大小。已有登记通过 PUT 附加与登记摘要/大小完全一致的文件；只能由原登记人附加，避免审批人上传后自批。
- 文件保存在配置的私有 MinIO 桶（默认 `media`），强制启用版本化；对象键由平台生成。039迁移的 `media_artifacts` 永久绑定对象版本、摘要、大小和上传人，数据库 trigger 拒绝更新/删除。重传/重复版本不能覆盖绑定；对象库管理员仍可删除版本，此时读取拒绝，不能冒充不可破坏存储。
- 校验、审批、下载均读取固定对象版本并核对实际字节、登记身份和绑定摘要/大小。仅 manifest 非空不再能通过；缺对象、错误摘要不批准、不返回制品字节。校验通过仍需独立 Admin/SRE 审批，摘要完整性不代表厂商来源签名可信。
- 所有 API 按当前会话与项目鉴权；Admin/SRE 管理，成员读取批准文件。全局介质可以按授权读取，不能从项目入口修改全局记录。审批前和退役后禁止通用下载；历史元数据/引用保留，不物理删除文件。
- 下载不暴露 S3 凭据或直连 URL，完整校验到600权限临时文件后，使用同一文件响应 HTTP Range/If-Range；返回摘要与 ETag，保留上传/校验/审批/下载/退役审计。临时文件完成或失败即清理。
- 现有介质页面提供上传新版本、附加登记文件、实际文件状态和批准下载，复用共享组件、确认弹窗、项目 URL、中英文和 Intel 字体。单次 inline 文件上限1 GiB、请求5分钟；不是容量/吞吐承诺。

## 接口

| 项目介质路径 | 行为 |
|---|---|
| POST `/projects/{id}/media/upload` | multipart `metadata` JSON 在前，`file` 在后；自动计算摘要/大小并原子保存登记、版本绑定与审计 |
| PUT `/projects/{id}/media/{mediaId}/content` | 原登记人上传 `application/octet-stream`，必须精确匹配原登记身份，绑定后不能替换 |
| POST `/projects/{id}/media/{mediaId}/transition` | 原状态机，校验与审批新增实际字节核对；不能跳态或自审批 |
| GET `/projects/{id}/media/{mediaId}/content` | 仅批准且未退役文件，项目鉴权、实际校验、审计和 Range；无对象版本时拒绝 |

正式契约见 `api/openapi.yaml`，前端类型由既有生成命令更新。`media:read/manage` 为独立会话能力，具体请求仍在事务里复核当前项目权限。

## 存储与部署

配置见 DEPLOYMENT §8；API 未配置存储时不伪造制品，文件操作返回不可用。操作员通过 `keel-admin media-init` 显式建立/启用版本桶，API不自动创建桶。桶初始化使用独立操作员权限；API服务账号仅需该桶的 GetBucketVersioning、PutObject、GetObjectVersion/相关对象读取权限，不授予删除版本或设置公共策略。生产要求 HTTPS。

上传对象存储成功而数据库提交失败时可能留下未引用对象；数据库记录不会假装成功，也不在异常路径冒险删除历史对象。未引用对象清理/保留制度、目标缓存、Agent/SSH推送、包管理镜像源、外链摄取、厂商签名/离线依赖闭包、持续损坏隔离治理与规模验收仍待后续实现。旧 approved 元数据不自动补造文件证据；直接文件读取仍须真实绑定。参数目录与通用 sim 执行创建的批准制品引用准入已接续，见 [CATALOG-ADMISSION](CATALOG-ADMISSION.md)；其他业务消费者及真实分发仍按各自阶段收口。

## 共享弹窗的 CSP 接线

真实浏览器发现 Radix 滚动锁动态样式被原严格 CSP 拦截。HTML入口（含 `/index.html` 和应用路由）现在获得每次请求独立的样式 nonce，React入口通过锁定的 `get-nonce` 传给共享组件。保持 `script-src 'self'`，不开放 `unsafe-inline`；nonce HTML使用 no-store，避免缓存串用。单测验证 nonce与CSP一致、每次不同且直接入口不绕过。

## 实际验证

2026-10-24 最终集成 `/tmp/keel-media-koxmml78`：真实隔离 PostgreSQL/MinIO/API/浏览器；MinIO版本固定读取、较新版本不覆盖历史、缺失旧版本拒绝回退、停用桶版本拒绝写入；数据库篡改/绑定不可变、存储字节篡改拒绝审批/下载；原BOM/ZTP 239/143/Kea相关准入回归；原介质HTTP/浏览器、参数目录回归；上传自动摘要、独立SRE审批、Viewer完整/Range读取、越权/跨项目/匿名/CSRF/未审批/退役/替换/摘要不符拒绝、两个上传入口和真实确认弹窗。临时数据库/MinIO/进程与凭据均清理。

另通过前端生成/组件边界/TypeScript/Vite、全仓Go vet/race、最终httpapi race与nonce单测、Compose配置和设计引用检查。Go沿用overlay排除原026/fabric_precheck草稿，未修改草稿。真机、千台容量、发布镜像和GitHub CI未验证；039仅执行在隔离库，未迁移运行库或部署3000。首轮测试环境tmpfs权限、旧语言按钮定位及共享弹窗CSP失败轮次不计通过。
