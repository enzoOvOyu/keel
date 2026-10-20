# Keel 开发交接

## 2026-10-20 P1 回退后的开发接续

- 工作目录 `ubuntu@43.207.20.26:/opt/keel`；GitHub `main` 与本地基点 `p1-complete` = `e7d5f1a`。开发分支 `dev/p1-network-bom` 从该标签分出，已导入 PRD v3.0.3 设计基线；运行中 API 仍为 P1 镜像 `keel:p1-e7d5f1a`，PostgreSQL 已恢复到 10 个迁移，`/healthz` 与 `/api/v1/auth/providers` 可用。运行 Compose 使用 `/home/ubuntu/.config/keel/runtime-p1.env`，不可直接用开发机回环地址启动容器 API。
- 回退前 27 迁移备份：`/home/ubuntu/.local/state/keel/backups/pre-p1-rollback-20261020T083912Z.dump`。设计文档原提交 `fe68c59` 存于 GitHub `design/prd-v3.0.3-20261020`；旧的未完成 BOM 代码仅在本地 `archive/pre-p1-rollback-20261020`，不计入当前实现。
- 新开发按 `IMPLEMENTATION-PLAN.md` v3.0.3 的依赖推进，首先实现 WP-1.15 统一 BOM 导入包；P2/P3 旧分支历史不得当作当前工作树的已实现功能。真机 PoC 状态独立于 sim 验证。
- WP-1.15 已实现标准包解析与 zip/tar.gz 安全解包、七 CSV/manifest/hash/行号/MAC 校验、项目级映射草稿与批准 API，以及交换机型号和不可变版本能力矩阵。映射批准路径在隔离 PostgreSQL 测试库验证。包级数据库预检、事务提交、物料投影和导入 UI 尚未完成，验收 F-0.1～F-0.4 不记 PASS。用户已同意把最小映射和交换机型号持久化能力前移到 WP-1.15。


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
