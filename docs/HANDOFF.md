# Keel 开发交接

## 2026-10-23 FR-0.3 mock Kea 制品门禁

- `ztp_mock_kea_plans` 的 commit 现要求 239 URL 指向设备脚本端点，`policy` 引用项目或全局已批准 `ztp_script`，且摘要/大小与生成的固定隔离脚本一致；失败保留 staged 候选、不生成 active offer。active 计划的 DORA 仿真在返回 ACK 前重新读介质状态，已退役或变更的脚本使结果为 `no_offer/bootstrap_artifact_unavailable`。设备 fetch 仍独立复核，避免审批状态在会话中变化后继续发脚本。真实 Kea 没有接入，本门禁只证明模拟链路。
- 隔离 `keel_test_ztp_gate_1023c` PostgreSQL 库执行 239/143、mock Kea 与 BOM 身份集成测试通过；最终调整后在 `keel_test_ztp_gate_1023d` 复测 mock Kea 通过，两库均已删除。`python3 scripts/check-design.py`、相关包 `go vet`、`go test -race`、本轮文件 `git diff --check` 通过。测试容器现有 `/tmp/keel-test-db.env` 中密码与容器实际密码不一致，本轮直接读取该一次性测试容器的环境构建测试连接，未打印或写入凭据；未修改旧测试环境文件。

## 2026-10-23 FR-0.3 引导入口隔离增量

- 工作目录 `ubuntu@43.207.20.26:/opt/keel`；分支 `dev/p1-network-bom`。239 设备脚本 GET 从普通认证 API 移到可选专用 TLS listener（`KEEL_ZTP239_LISTEN_ADDR`、`KEEL_ZTP239_TLS_CERT_FILE`、`KEEL_ZTP239_TLS_KEY_FILE` 必须同时配置），普通 API 仅保留登录后的脚本审阅 manifest。设备 listener 只接受管理网直接 TCP 对端，拒绝转发身份头；业务校验沿用 active mock Kea、精确 URL、六个 CUMULUS 头、SoT 和批准介质摘要，响应含 trace 与脚本摘要。P1 3000 镜像仍是 `keel:p1-e7d5f1a`，未部署本代码。
- 验证：`python3 scripts/check-design.py`、`go test -race ./cmd/keel-api ./internal/httpapi ./internal/platform -count=1`、对应 `go vet` 和本轮文件的 `git diff --check` 通过；未运行真实数据库/设备集成。待办：真实管理网地址部署及设备信任证书验收、DHCP 真实下发与 DORA、设备回报和可管理状态证据；不能把此项记为 FR-0.3 PASS。`AGENTS.md` 有先于本轮存在的未提交修改，保留原样、不包含本轮提交。

## 2026-10-21 暂停点：FR-0.3 Kea 控制适配器

- 工作目录 `ubuntu@43.207.20.26:/opt/keel`，开发分支 `dev/p1-network-bom`。本阶段新增 `internal/fabricboot/kea_control.go`：仅允许显式 CA 与客户端证书连接 HTTPS Kea 控制端点，禁重定向；完整 Dhcp4 配置依次执行当前 hash 核对、`config-test`、漂移复核、`config-set`、生效 hash 回读。拒绝不完整顶层结构或无可核验返回，不记录完整配置及凭据。测试覆盖成功、漂移、测试拒绝、回读不符、重定向及无效配置。
- 该适配器尚未连接已批准作业流、配置持久化或真实 Kea；未测试真实 DORA 或服务重启后的配置保留。现有 `ztp_mock_kea_plans` 仍是模拟选项，不代表生产 DHCP 生效。FR-0.3.1 及 FR-0 全部验收仍未闭环。下一步从批准的完整配置产物与串行作业接入该适配器，并在隔离 Kea 环境验证配置回读、持久化与 DORA。
- 验证：`go test ./internal/fabricboot -run ^TestKea -count=1`、`python3 scripts/check-design.py`、`make check`（含 Go race、vet、前端类型检查与构建）及 `git diff --check` 均通过。用户要求本阶段完成后暂停；暂停期间不部署、不迁移 P1 运行环境。恢复时先核对本节提交、分支与工作树，再继续。

## 2026-10-20 P1 回退后的开发接续

- 工作目录 `ubuntu@43.207.20.26:/opt/keel`；GitHub `main` 与本地基点 `p1-complete` = `e7d5f1a`。开发分支 `dev/p1-network-bom` 从该标签分出，已导入 PRD v3.0.3 设计基线；运行中 API 仍为 P1 镜像 `keel:p1-e7d5f1a`，PostgreSQL 已恢复到 10 个迁移，`/healthz` 与 `/api/v1/auth/providers` 可用。运行 Compose 使用 `/home/ubuntu/.config/keel/runtime-p1.env`，不可直接用开发机回环地址启动容器 API。
- 回退前 27 迁移备份：`/home/ubuntu/.local/state/keel/backups/pre-p1-rollback-20261020T083912Z.dump`。设计文档原提交 `fe68c59` 存于 GitHub `design/prd-v3.0.3-20261020`；旧的未完成 BOM 代码仅在本地 `archive/pre-p1-rollback-20261020`，不计入当前实现。
- 新开发按 `IMPLEMENTATION-PLAN.md` v3.0.3 的依赖推进，先闭环 WP-1.15，再做 WP-1.16 网络型号与工程校验、WP-1.17 ZTP。FR-0 是服务器交付第 0 步：G2 启动查 NP-1/NP-2/NP-4，NP-3 只能由 G2 三方确认产出，G5/G6 查 NP-5/NP-6；不可采集一律 unknown/BLOCKED。P2/P3 旧分支历史不得当作当前工作树的已实现功能。真机 PoC 状态独立于 sim 验证。
- WP-1.17 身份底座进行中：标准交换机 BOM 已接收管理口/基板/其他永久 MAC，冲突预检与当前绑定持久化已实现；项目内只读身份反查按 SN→管理口→基板→其他优先级返回 matched/isolated/conflict，已新增项目内仿真会话 API、最近会话列表、ZtpOnboardBoard 操作面板和持久化九态证据时间线、失败三段式记录、版本并发校验及指数退避重试窗口；身份未命中在 lookup 后校验已批准隔离脚本并记录模拟 fetch 后进入 isolated，冲突在 lookup 后拒绝脚本。会话创建时仅显示 SoT 初始快照，lookup 时从当前 SoT 重新反查并保存最终判定；booturl/fetch 重新核对 active mock Kea 与 DHCP 哈希，fetch 重新校验脚本批准状态，人工只驱动步骤，`sim.managed` 不更新交换机状态或 NP-2。`internal/fabricboot` 已新增进程内 mock Kea 契约及 DORA golden 文件，覆盖 239/143/60/43、staging→commit、管理网 eth0 过滤、错误 URL/VCI 负例；项目 API 已将 mock Kea 的 staged/active 选项持久化，项目管理员按哈希与版本提交，DORA 仿真重建 active offer；隔离 PostgreSQL 实测 Store 重开后仍可读取、跨项目不可读取、前面板口无 offer。ZTP 仿真从 powered→dhcp 现要求后端 mock DORA ACK，并把 active 哈希写入事件；无计划或前面板口不可推进。239 设备 GET 端点和固定隔离脚本已在开发分支实现：需管理网直连来源、精确项目/批次/N-S4/已批准介质 URL、六项 CUMULUS-* 身份头；介质必须 `ztp_script` 已批准且摘要/大小与脚本一致，脚本先拒绝已有 NVUE pending 改动、校验本次 pending 只含 swp admin down，再 apply；保留 eth0；未命中返回隔离脚本、冲突拒绝，且写审计。已新增 143 通道的 IDevID 证书验证原语：要求直接完成的 TLS 握手，按显式制造商根证书重新校验链、有效期和客户端认证用途，只从 X.509 subject serialNumber 提取 SN；空根、无证书、无 SN 均拒绝。RFC 8572 JSON conveyed-information 的 CMS DER 编码器仅包含固定隔离 pre-configuration-script，signed-data-preferred 会拒绝未签名数据。143 设备入口现可通过可选独立 TLS listener 提供 RESTCONF get-bootstrapping-data：强制客户端证书，按显式制造商 CA 验证 IDevID，再用实际 TCP 管理网来源与唯一 active mock Kea 的 143 主机 URI 确定项目，经同一 SoT 反查和已批准脚本摘要/大小校验后返回隔离 onboarding；路由重叠和冲突拒绝，读取入审计。option 143 URI 已按 RFC 收紧为 https://host[:port]，不接受路径/查询。隔离 PostgreSQL 测试覆盖未批准、非管理网、错误 Host、签名偏好及重叠路由。仅支持 JSON RPC 且尚未实现 report-progress、真实 Kea/136、独立管理网部署、设备侧可信引导服务器证据或真机 PoC；自动重试、完整最小管理配置和全阶段 golden 契约也未闭环，不记 ZTP PASS。
- 2026-10-21 验证：`go test ./internal/fabricboot -race -count=1`、`python3 scripts/check-design.py` 与 `make check` 通过；另在独立 `keel_test_` PostgreSQL 库运行 `TestZTPMockKeaDurableStageCommit` 通过，测试库已清理；同一隔离库还运行 `TestBOMPackageCommitAtomicIdentityAndIdempotency`，覆盖 DHCP 无请求/无 active plan/前面板口拒绝及 ACK 哈希事件。mock Kea 与这些集成测试仍只证明仿真。源码分支为 `dev/p1-network-bom`，3000 端口仍为独立 P1 镜像 `keel:p1-e7d5f1a`，本增量未迁移或部署到 P1 运行库。下一步将 239 设备脚本请求关联到 ZTP 会话的 booturl/identity/fetch 阶段，补齐凭据可达性与 143 mTLS/IDevID mock，再准备独立管理网 listener 和真机 PoC 执行包。
- 2026-10-21 仿真证据校验增量：`lookup` 阶段重新反查当前 SoT，`booturl`/`fetch` 阶段复核 DHCP 所用 active mock Kea 哈希和已批准脚本；未知身份仅记录已批准隔离制品的模拟 fetch 后进入 isolated，冲突不发脚本。隔离 PostgreSQL 负例覆盖会话期间 SoT MAC 变化、active offer 变化和脚本退役；该证据不代表真实设备完成配置导入、重启或可管理。
- 2026-10-21 143 增量验证：独立 `keel_test_` PostgreSQL 库运行 `TestSZTP143ApprovedIsolationAndAmbiguousRoute` 及受 URI 修正影响的 239、mock Kea、BOM 集成测试均通过，测试库已清理；`TestOptionalSZTPListenerRequiresClientCertificate` 验证无设备证书 TLS 握手失败、有效证书可达专用 listener；HTTP 输入测试验证冒用证书头无效。`make check` 与设计检查通过。此证据仅覆盖开发链路，不覆盖真实 DHCP/SZTP 设备、证书吊销、受信服务器配置或管理网部署。
- 2026-10-21 239 增量验证：独立 `keel_test_` PostgreSQL 库中 `TestZTP239ApprovedIsolationScript`、`TestZTPMockKeaDurableStageCommit` 与含真实 BOM 身份的 `TestBOMPackageCommitAtomicIdentityAndIdempotency` 均通过；覆盖脚本未批准/自批/退役、非管理网来源、错误 Host/HTTP scheme、URL 篡改、已知 SN 与冲突 SN。`bash -n`、`python3 scripts/check-design.py`、`make check` 通过，测试库已清理。仅证明 API/模拟设备契约；NVUE 命令在目标交换机上的兼容性、隔离有效性和 5 分钟指标均未验证。
- WP-1.15 进行中：标准包七 CSV/manifest/hash/安全解包、已批准字段映射、服务器/交换机/部件/光模块型号版本、只读预检令牌、同包原子事务提交、统一 bom_items/revisions 与 Machine/Component/PhysicalPort/Switch 投影、导入 UI、目录/压缩包双入口、问题 CSV 导出与已提交问题项目级查询已在开发分支实现。隔离 PostgreSQL 测试验证同批次幂等、新批次稳定 ID、错误包零写入；P1 运行库未迁移。单文件宽表已通过独立 CSV 兼容入口按 SN 折叠为服务器、以永久 MAC 标记的临时 NIC 和端口，并共享批次摘要、只读预检、原子提交与稳定 ID 版本链；真实 NIC 序列号在标准包中可接管单一临时身份，多临时身份合并会阻断并要求人工核对。标准 BOM 服务器/交换机端口类型已从父设备已发布型号能力矩阵派生，缺声明阻断；宽表临时端口仍保留兼容标记。交换机端口型号能力对照已加入预检，含 breakout 模式/子口范围/速率阻断；光模块速率/介质明示冲突已阻断，缺测留 WARN；服务器计算口 rail 缺失或与型号 rail 数不符已阻断，缺型号期望值留 WARN；网络侧已加入声明上下行带宽比、电源模块/馈线/功率预算与型号下界、Leaf 计算口 rail 数预检；缺测仍为 WARN/unknown，带宽比现可引用项目内批准的工程限值基线版本（含客户/NE 批准证据引用）；未绑定/未覆盖仍为 WARN，外部证据尚未自动验真，不能记 FR-0 PASS；同包线缆的服务器计算口与交换机计算口已做规划 rail 对照，外部 UID 端点尚未覆盖；还缺完整网络配置基线、外部批准证据验真与大规模性能；当前将 manifest 的 project_code 按大小写不敏感规则对齐 P1 Project.slug（P1 无独立项目码字段）；在项目模板物料需求清单落库前，标准包提交至少要求服务器、服务器端口、交换机、交换机端口与线缆各有一行，零行文件仅在格式阶段合法；F-0.1～F-0.4 不记 PASS。用户已同意把最小映射和交换机型号持久化能力前移到 WP-1.15。


## 收口记录（2026-10-12，P1 收口已完成）

- 暂停点已固化并提交：`7ba6b6b`（控制面与模拟执行内核）、`0c572a8`（集成套件接入 worker）、`d339850`（执行契约补全）、`388d10b`（执行 UI）。分支 main，ahead 4，**未推送**。
- 集成套件已跑通：make check + 认证 12 组 + 控制 9 组 + 执行 8 组 + 真实浏览器 26 组全绿。
- **执行引擎已有真实证据**：2,000 个持久化逻辑目标 100% 完成，301.6s，seed 42，worker 并发 8，满足 P1 扇出 ≥99.5% 判据。报告 `sim-capacity-2000-20261012T144047Z.json`。**模拟内核，非硬件验收**（hardware_evidence false、real_device_connections 0）。
- 正式库已迁移至 003（002_control + 003_execution），keel_app 最小权限已核验：审计仅 SELECT/INSERT，迁移历史只读。迁移前备份 `pre-migration-20261012T145432Z.dump`。
- 已部署 `388d10b`，`/healthz` 与 `/api/v1` 版本一致，authentication=ready，business_ready 仍为 false。
- **回滚**：镜像保留 66fecbe 等历史版本；数据库有 pre-p1 / pre-commit / pre-migration 三份 dump。已在隔离库演练恢复：8 张表与用户、项目数据完整还原（9 条 GRANT 报错仅因隔离库无 keel_app 角色）。**回滚代码必须同步回滚数据库**：旧代码遇 003 schema 会拒绝启动。正式库本身尚未做实际恢复演练。
- 追踪矩阵：WP-1.4 由 planned 更新为 partial；P2~P5 仍全部 planned。

遗留：隔离测试库 127.0.0.1:15433 待清理（用 `python3 scripts/test-environment.py stop`）。

记录日期：2026-10-12。主工作区 `ubuntu@13.158.38.7:/opt/keel`，分支main。品牌字标为 **Keel**，slogan 为 **Knowledge-led Execution, Enforcement & Lifecycle**；仓库、命令、包名和服务标识保留 `keel`。

当前源码已进入 **P1 本地认证与项目基础闭环**，不是全部P1或全部交付功能完成。版本不写死在本文件：源码用 `git log -1 --oneline`，运行镜像用 `/healthz` 的version，GitHub用 `git ls-remote origin refs/heads/main` 对照。

## 本轮实现

- 登录页替换为船体龙骨/肋骨线条动画；保留中英、明暗、移动布局与减少动画偏好。
- 本地登录、预会话CSRF、同源Origin校验、随机不透明HttpOnly会话、退出撤销、账号和真实连接IP双维度限流。密码使用Argon2id，验证并发受限；口令、Cookie和token不进入日志或浏览器持久存储。
- 会话空闲30分钟、绝对12小时；预会话15分钟。禁用账号、修改密码、授予或撤回成员权限会使原会话立即失效。
- 项目列表、平台管理员创建项目、URL明确项目域的详情页；无权限项目统一404。项目角色支持Admin/DE/DL/NE/SRE/Viewer；项目Admin不等于平台管理员。
- 项目创建、创建者成员关系、幂等响应与审计在同一事务；重试同一请求不重复创建，不同请求复用key返回409。事务内复核用户和会话状态，退出已提交后旧请求不能继续写入。
- ent七个实体及生成代码、原生PostgreSQL事务、嵌入的版本化SQL迁移。API启动只检查版本与checksum，数据库连接或schema不匹配时拒绝启动。
- `keel-admin` 支持迁移、一次性管理员初始化、创建/禁用账号、密码重设、账号及显式IP解锁、成员授予/撤销；CLI审计记录实际操作系统用户和独立请求ID。
- 应用账号 `keel_app` 与数据库owner配置分离；应用角色不能修改/删除审计或写迁移历史。审计表另有只追加触发器；数据库管理员仍有改变结构的能力，生产归档与恢复方案继续按设计落实。

## 验证证据与范围

本轮后端已实际完成：

- Go vet、完整Go测试、ent再生成；真实PostgreSQL集成测试通过 `-race`。
- 隔离 `keel_test_auth` 数据库验证迁移重复执行/checksum篡改拒绝、实体字段、首次管理员并发唯一、项目四路并发幂等及事务原子性。
- 验证预会话/空闲/绝对到期，CSRF跨会话拒绝，退出/成员变更/密码重设/禁用撤销，最后有效管理员保护，审计不可改删。
- 独立 `keel_test_http` + 3102 API 的12组HTTP黑盒通过：真实本地登录与Cookie、非法输入、Origin/CSRF、六角色授权、跨项目404、项目创建幂等、撤销、统一失败响应、账号/IP限流，以及伪造X-Forwarded-For不能绕过限流。
- 最后复核补了 `/api/v1` 信息契约断言：`phase=p1-foundation`，认证状态使用ready/setup_required/not_configured；对应后端单测及最终版本12组HTTP复检通过。

前端类型检查/构建、含隔离PG的完整make check与preflight通过。`scripts/check-auth-browser.mjs` 的8组真实浏览器检查全部通过，覆盖登录、实际项目创建/刷新/退出、外域next拒绝、私有页面拒绝、英文移动布局、秘密不落浏览器存储，以及会话后台验证不重置编辑表单。`scripts/check-browser.mjs` 的身份源未启用fixture 8组检查也通过；docs/screenshots已更新为本轮真实ready登录明暗和移动截图。

设计检查仍覆盖17FR、34领域组件及工作包依赖，不代表所有业务验收完成。测试数据库结果不替代正式数据库迁移、正式账号初始化、已部署镜像健康或GitHub Actions执行证据。

## 服务器配置与账号

- 部署预览：`127.0.0.1:3000`；tmux开发API：3001；Vite：5173。
- 开发PostgreSQL宿主机连接：`127.0.0.1:15432`，仅回环开放；容器内使用postgres:5432。Redis与开发MinIO保留原命名卷。
- 应用配置：`/home/ubuntu/.config/keel/development.env`；迁移/账号管理owner配置：同目录 `admin.env`。目录700、文件600，均不进Git。
- 一次性初始化目标账号为 `enzo`；初始随机口令文件为 `/home/ubuntu/.config/keel/bootstrap-admin.password`，权限600。没有代码内默认密码；不要把文件内容复制到日志、文档或Git。
- 初始化是否完成以 `GET /api/v1/auth/providers` 为准：setup_required表示仍需服务器CLI初始化，ready表示已存在有效管理员。初始化命令再次运行会拒绝覆盖已有管理员。
- `python3 scripts/configure-development.py` 准备受限应用账号及配置；迁移成功后使用 `--grant` 授权。详细顺序和备份见 [DEPLOYMENT](DEPLOYMENT.md)。

正式数据库的本轮实际记录：

- 迁移前public业务表数量为0；备份为 `/home/ubuntu/.local/state/keel/backups/pre-p1-20261012T122307Z.dump`，权限600，pg_restore -l已确认可读取备份目录；尚未进行实际恢复演练。
- 嵌入001迁移、keel_app授权、enzo首管理员初始化均成功；初始随机口令文件已按上述位置和权限保存。
- 应用角色对审计仅可SELECT/INSERT，UPDATE/DELETE/TRUNCATE拒绝；迁移历史不可INSERT，权限检查通过。
- 使用正式development配置的受限应用连接、enzo登录、空项目列表与退出已验证。正式库没有写入测试项目或模拟业务数据。
- 最终源码提交与运行镜像按发布步骤对照healthz及GitHub refs核验；本文件不提前声称GitHub Actions结果。

隔离测试使用临时的 `keel-auth-test-postgres` 容器及auth-test窗口；验收后移除测试容器、窗口、临时数据库配置和浏览器凭据。长期保留正式四个Compose服务及work/api/web/logs，不依赖临时测试数据。需要复检时按CI定义重建独立测试环境。

## 换电脑接续

```sh
ssh -i /path/to/enzo-using.pem ubuntu@13.158.38.7
cd /opt/keel
cat AGENTS.md docs/HANDOFF.md
git status -sb
docker compose --profile data ps
sh scripts/dev-session.sh
tmux attach -t keel
```

已有会话不重启；Ctrl-b再按d仅分离。API窗口使用 `make dev-api`，自动加载受限应用配置。修改Go后需在确认窗口属于本项目且进程已退出后重启该窗口；不重启或覆盖其他人的会话。部署3000与开发3001独立。

本地访问：SSH映射3000→3000，开发热更新另映射5173→5173。API与前端通过同源Cookie工作；开发HTTP例外仅用于显式development与localhost/127.0.0.1 Origin。生产需HTTPS、Secure Cookie和独立配置。

## GitHub、备份与发布

origin为 `git@github.com:enzoOvOyu/keel.git`，使用服务器现有默认SSH身份，GitHub通信已正常。旧 `keel-github` 别名的专用key曾失败，不能据此误判当前origin。

提交前先fetch/检查分支，再正常push，禁止强推。CI已接入隔离PostgreSQL与HTTP/浏览器检查，但当前没有通过Actions API确认本轮结果；本机检查成功和GitHub Actions成功分别记录。

历史Git bundle与旧镜像保留；bundle仅包含提交历史，不含.env、SSH密钥和数据库卷。迁移前必须另做数据库备份；回滚镜像不会回滚数据库，当前API对schema版本或checksum不匹配会拒绝启动。

## 下一步

- 优先继续WP-1.13：项目成员管理UI、列表分页与身份管理，把现有CLI权限能力接入可审计的管理界面；随后补齐LDAP/OIDC联合登录。
- 再按P1依赖推进ProjectAsset、领域实体与项目隔离、执行回执/outbox/资源锁/Agent基础；真实硬件默认仍使用mock/sim适配器。
- 项目生命周期、资产/BOM、型号/介质、调度与交付流程按原工作包逐步落实。当前项目状态仅planning，不展示模拟设备KPI或可执行交付入口。
- 生产TLS/身份源、备份恢复、会话与限流记录维护、审计归档、万台容量和硬件PoC仍需独立验证。
