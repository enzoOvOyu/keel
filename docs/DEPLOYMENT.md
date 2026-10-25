# 开发、验证与部署

主工作区 `ubuntu@43.207.20.26:/opt/keel`。本文描述操作方法；当前分支、镜像、迁移与测试结果只维护在 [HANDOFF](HANDOFF.md)。固定工具链见 [TOOLCHAIN.json](TOOLCHAIN.json)、`go.mod`、`.nvmrc` 与 `package-lock.json`。

## 1. 恢复既有开发环境

```sh
cd /opt/keel
git status -sb
git log -1 --oneline
tmux list-sessions
docker compose --profile data ps
curl --fail http://127.0.0.1:3000/healthz
tmux attach -t codex-keel
```

复用 `codex-keel`，不要覆盖已有窗口。当前 `scripts/dev-session.sh` 固定创建/复用的是 `keel`，与用户指定会话不同，不能用它代替恢复 `codex-keel`。需要启动 API/Vite 时先检查 3001/5173 是否已有本项目进程，再在自己的窗口使用 `make dev-api` / `make dev-web`；Go 源码修改不会自动重启 API，3000 镜像也不会随源码更新。

## 2. 配置与端口

| 用途 | 位置/端口 | 边界 |
|---|---|---|
| Compose | `/opt/keel/.env` | 600，仅服务器；数据服务及容器 API 配置 |
| 开发 API/worker | `/home/ubuntu/.config/keel/development.env` | 600；受限应用角色 `keel_app` |
| 管理 CLI | `/home/ubuntu/.config/keel/admin.env` | 600；数据库 owner，只用于迁移/管理 |
| 首次管理员口令 | `/home/ubuntu/.config/keel/bootstrap-admin.password` | 600；随机值，不复制到日志/Git |
| PostgreSQL | `127.0.0.1:15432`；容器内 `postgres:5432` | 不把宿主回环地址用于容器连接 |
| 部署 API/UI | `127.0.0.1:3000` | 已构建镜像预览 |
| 开发 API/Vite | `127.0.0.1:3001` / `5173` | 同源代理、热更新前端 |
| 开发 MinIO | `127.0.0.1:9000` / `9001` | API/Console；来源与锁定摘要见 `infra/minio/Dockerfile` |
| 隔离测试 PG/API | `127.0.0.1:15433` / `3102` | 独立 `keel_test_` 数据库 |

配置目录700、秘密文件600；不打印配置内容。PostgreSQL/Redis/MinIO 命名卷保留；更新不使用 `down -v`。数据服务可用不等于其上业务已实现。开发 MinIO 采用仓库固定的官方源码构建，生产维护/备份/授权按发布要求另行验证。

本机通过 SSH 隧道访问：

```sh
ssh -i /path/to/enzo-using.pem -N -L 3000:127.0.0.1:3000 -L 5173:127.0.0.1:5173 ubuntu@43.207.20.26
```

访问 `http://localhost:3000/login` 或已启动的 Vite 5173。开发 HTTP 只允许显式 development 与配置中的精确 loopback Origin；生产使用 HTTPS、Secure Cookie 和独立凭据。Cookie 为 HttpOnly/SameSite=Lax，后端不信任任意转发头。

## 3. 数据服务与版本迁移

首次配置前确认既有 `.env` 和凭据，缺失时按 `.env.example` 准备受限配置，不覆盖现有连接：

```sh
docker compose --profile data up -d --wait postgres redis minio
python3 scripts/configure-development.py
```

脚本准备/复用 `keel_app` 及分离配置，不自动迁移或初始化平台管理员。迁移 SQL 在 `internal/platform/migrations/`，编译时嵌入程序；已执行 SQL 不得改写。API 只校验版本/checksum，不用自动建表替代迁移。

**迁移前备份，并验证备份可读取。** 示例使用容器现有数据库配置，备份文件不覆盖：

```sh
umask 077
mkdir -p /home/ubuntu/.local/state/keel/backups
backup_file="/home/ubuntu/.local/state/keel/backups/pre-migration-$(date -u +%Y%m%dT%H%M%SZ).dump"
(set -C; docker compose exec -T postgres sh -c 'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$backup_file")
test -s "$backup_file"
docker compose exec -T postgres pg_restore -l < "$backup_file"
```

目录可读只证明备份格式可读；恢复演练须在隔离数据库单独执行。迁移在 owner 子 shell 中运行，之后授权受限应用角色：

```sh
(
  set -a
  . /home/ubuntu/.config/keel/admin.env
  set +a
  go run ./cmd/keel-admin migrate
)
python3 scripts/configure-development.py --grant
```

040 型号发布迁移增加草稿能力矩阵，并禁止修改/删除已发布版本或槽位、向历史版本追加槽位；修正规格须在同一发布事务创建新版本，不能通过直接改历史行升级设备。当前是否已迁移仍以 HANDOFF 为准。

迁移使用事务、advisory lock 与 checksum。应用角色不能改/删审计或写迁移历史；数据库管理员的权限仍需生产归档与恢复制度约束。

## 4. 管理员与项目成员

首位管理员无默认密码；在加载 `admin.env` 的子 shell 中交互初始化，或传入当前用户拥有、权限600且非符号链接的密码文件：

```sh
go run ./cmd/keel-admin init-admin --username enzo --display-name Enzo --password-file /home/ubuntu/.config/keel/bootstrap-admin.password
```

初始化只允许一次。密码至少12字节、最多1024字节，不通过命令参数或 stdin 明文传入；省略密码文件时从真实终端隐藏输入。账号创建、改密、禁用、解锁、成员授权等用 `go run ./cmd/keel-admin --help` 和相应子命令查看当前参数，不在本文复制第二份完整 CLI 目录。

成员目录已支持八角色（含NDE/NH）；当前网络权限实施范围见HANDOFF/DEVELOPMENT。接受角色名称不代表审批签名、生产下发及完整网络权限已交付。平台 Admin 与项目 Admin 分开；禁用/改密/成员变更撤销旧会话，最后有效管理员保护由服务端执行。

## 5. 检查和隔离集成验证

纯设计修改执行 `python3 scripts/check-design.py`；实现按完整功能块集中验证，失败定向重跑，PR/阶段执行完整门禁。同一版本已有有效 CI 结果可复用，不按每个字段重复全量模拟。验证节奏见 DEVELOPMENT §5，完整门禁为：

```sh
npm ci
make check
make build
```

`make check` 包含设计校验、Go vet/race、前端类型和构建。`make build` 生成 API/admin/worker/sim 二进制。`go generate ./ent` 使用 `go.mod` 固定的 Ent 工具；仅在 schema 变化时再生成。

PostgreSQL 测试会重建目标 public schema，只能使用隔离 `keel_test_` 命名库。缺少 `KEEL_TEST_DATABASE_URL` 时的 Skip 不计集成通过。可用仓库 helper 检查/创建带专用标签的临时容器：

```sh
python3 scripts/test-environment.py status
python3 scripts/test-environment.py start
```

确认私有 `/tmp/keel-test-db.env` 与当前容器匹配后，在子 shell 加载并运行目标测试；旧文件存在不证明凭据仍有效。不要为修复配置不一致删除他人正在使用的测试环境。

HTTP/控制/浏览器集成顺序以 `.github/workflows/ci.yml` 和 `scripts/check-integration.sh` 为准。HTTP 使用独立 `keel_test_http` 与 API3102，避免 Go 测试重建 schema 相互干扰。浏览器检查需安装 CI 锁定的 Playwright/Chromium；临时凭据文件600，验证后删除。报告分别记录本机结果、GitHub Actions 实际结果及跳过项，不把依赖缺失记成通过。

### 独立执行容量复核

已有标记为disposable的测试PostgreSQL和固定Go工具链时，可运行：

```sh
python3 scripts/check-execution-workspace-integration.py
```

默认2,000逻辑目标；`--targets 100`仅用于明确的dev档，不能替代2,000档验收。脚本新建随机独占库、回环3102 API和自有worker，剥离继承的KEEL配置，不启用硬件适配器，不重置既有库。端口/锁被占用即退出；使用专用测试容器标签确认，不访问运行库。沿用现有执行HTTP检查，覆盖冻结Flow、回执/租约/待对账、重试/依赖/熔断、控制和权限。

每次保留私有证据目录：simulation-report.json记录容量与模拟口径，resource-report.json记录源码版本/差异摘要、二进制摘要、CPU/内存/磁盘、API/worker/测试进程和数据库容器观测。数据库容器与宿主机网络指标可能包含其他测试负载，明确标为共享；Redis/对象存储不参与此测试，真实设备连接与传输字节为0。失败保留状态和待对账回执证据，不能把容量子步骤或旧结果当整套PASS。结束只清理自有进程/随机库/临时凭据，日志保留600。历史未接入草稿需按当前工作区约定使用Go overlay，不能为了测试删除草稿。

开发机的/tmp当前为tmpfs。执行容量与介质集成封装把大体积可执行二进制保留到用户私有磁盘目录`~/.local/state/keel/evidence/{capacity,media}/`，原/tmp证据目录内通过软链接执行和计算摘要；凭据目录仍是真实/tmp路径，MinIO原安全路径校验不变。报告同时记录实际GOMEMLIMIT/GOMAXPROCS设置，不把内存参数不同的结果视为同一性能档。旧证据迁存应保留原路径入口，只处理已结束测试，不删除他人证据或运行数据。

## 6. ZTP 设备入口

普通 3000 UI/API 与设备引导 listener 分开。239/143 只有在专用 TLS 配置齐备且数据库匹配时启用；Compose 预览是否启用见 HANDOFF。

| 通道 | 配置与协议 | 必须保持的校验 |
|---|---|---|
| 239 | `KEEL_ZTP239_LISTEN_ADDR`、`KEEL_ZTP239_TLS_CERT_FILE`、`KEEL_ZTP239_TLS_KEY_FILE`；`GET /api/v1/boot/ztp` | 管理网直接 TCP 来源、拒绝转发身份头、active offer 的精确 URL、六个 CUMULUS 头、SoT 反查、独立批准脚本摘要/大小 |
| 143 | `KEEL_SZTP_LISTEN_ADDR`、`KEEL_SZTP_TLS_CERT_FILE`、`KEEL_SZTP_TLS_KEY_FILE`、`KEEL_SZTP_IDEVID_CA_FILE`；RESTCONF `get-bootstrapping-data` / `report-progress` | 强制 mTLS、制造商 CA 重验 IDevID 与 subject serialNumber、管理网来源/Host/唯一 active URI、同一批准制品 |

143 option URI 为 `https://host[:port]`，不带路径/查询；进度回报使用 RFC8572 JSON，并关联30分钟内同证书 SN、来源和 active offer 的读取。`signed-data-preferred` 不能回退为未签名数据。239 的固定隔离脚本保留 eth0，先检查 NVUE pending，再仅关闭前面板口；不是完整个性化管理配置。

当前 active mock Kea offer 只是开发契约数据，不能证明真实 DHCP 生效。实际 Kea 适配、持久化配置/DORA、设备证书信任、目标 NOS 命令兼容和真机开通均需独立证据。证书私钥仅存受保护位置；设备引导不通过普通 UI 反向代理冒用身份。

## 7. 发布、回滚与 Git

在已验证提交、兼容迁移及受限授权就绪后发布：

```sh
sh scripts/preflight.sh
sh scripts/deploy.sh
curl --fail http://127.0.0.1:3000/healthz
curl --fail http://127.0.0.1:3000/api/v1/auth/providers
```

`deploy.sh` 要求干净工作区，构建 `keel:<完整Git SHA>`，等待容器健康后更新 `.env` 的 `KEEL_IMAGE` 并保留旧镜像；不自动迁移数据库或失败回滚。工作区有用户修改时先确定其归属，不为通过发布门禁擅自 stash/提交/删除。发布证据还需认证、授权及受影响页面实际交互，不能只看 healthz。

回滚选择已保留且与数据库兼容的镜像，再按部署配置切换 API。**镜像回滚不会回退数据库**；版本不兼容时按备份恢复方案处理，不能跳过 schema 校验或删除卷。

origin 使用服务器已配置的仓库身份。GitHub 操作前检查认证，推送前 fetch/比较远端，正常 push，不强推。Git bundle 只包含提交历史，不包含数据库、秘密文件或卷。提交、推送、CI、部署和 PoC 各自记录，不相互代替。

## 8. 私有版本化介质存储

介质文件能力使用 `KEEL_MEDIA_ENDPOINT`、`KEEL_MEDIA_ACCESS_KEY`、`KEEL_MEDIA_SECRET_KEY`、`KEEL_MEDIA_BUCKET`（默认 `media`），配置保存在既有600权限文件，不能输出或提交。Compose API已接入相同变量；容器内地址使用存储服务地址，不能误用宿主回环。仅显式development允许HTTP，staging/production必须HTTPS。

由操作员加载独立的桶管理配置后运行 `keel-admin media-init`，创建私有桶并启用版本化；API运行账号使用限定该桶的对象读写/版本读取与版本状态读取权限，不需要创建桶、删除版本或修改策略。不要直接复用MinIO root凭据到API；运行环境的账号、桶及配置状态以HANDOFF发布记录为准。

039迁移增加不可变的对象版本绑定；发布前仍按§3备份/迁移/授权，不修改旧迁移或伪造旧介质的文件证据。旧元数据的uploaded状态可以由原登记人附加匹配文件，已verified/approved但缺真实文件的历史记录不得凭manifest继续批准或下载，应登记经过复核的新版本。文件校验/批准/读取全部失败关闭。

API inline上传上限1 GiB，使用600权限临时文件和5分钟请求期限；tmpfs/磁盘容量及代理body/超时需按部署资源设置。下载先校验整个固定版本，再提供Range，不据此承诺分发吞吐。实际能力与未完成项见 [MEDIA-ARTIFACTS](MEDIA-ARTIFACTS.md)。

独立软件集成：`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json python3 scripts/check-media-artifacts-integration.py`；该overlay仅用于当前既有026草稿的隔离，正式干净代码不需要。脚本使用带disposable标签的测试PostgreSQL及新建的临时MinIO，不连接运行库/桶，完成后清理自己的服务、数据库与凭据。未验证发布镜像、规模或真机不能记PASS。

平台签名清单是可选的独立密钥配置：`KEEL_MEDIA_SIGNING_DIRECTORY` 只读挂载宿主私有目录，`KEEL_MEDIA_SIGNING_KEY_FILE` 指向容器内PKCS8 Ed25519私钥。默认关闭，不影响已批准制品现有读取；已配置的非法私钥导致API启动失败。私钥须由API实际用户拥有且仅属主可读，公钥独立可信分发；配置、轮换、离线命令及平台签名边界见 [MEDIA-MANIFESTS](MEDIA-MANIFESTS.md)。本功能无新迁移。

041完整性迁移为固定源新增独立隔离记录并回填既有绑定；已隔离证据不可更新/删除，不改变原审批生命周期或历史绑定。部署新API前按§3备份并验证格式、执行迁移、授予受限应用角色新表权限。回滚旧镜像不会删除隔离记录，但旧镜像不消费隔离状态；如运行库已产生隔离项，不得仅切回旧版恢复分发。回滚后先停止受影响介质/执行入口，修复并重新发布，不能以绕过隔离作为恢复方案。迁移后的实际版本、备份与回滚条件见HANDOFF。

HTTP(S)来源获取使用可选 `KEEL_MEDIA_FETCH_ORIGINS` 准确origin列表，默认空值关闭；合法配置不会自动获取或批准已有登记。每条最多域名/端口，不含路径、凭据、查询或片段；生产仅HTTPS且验证证书，每跳重定向及解析后的连接地址均复核，不继承环境代理。实际来源URL沿用manifest_ref，仅原登记人可通过受控动作固定匹配摘要/大小的文件。运行环境没有已批准的来源配置时保持关闭；不能为演示而允许任意外网。详见MEDIA-ARTIFACTS“受控HTTP(S)来源获取”，无新迁移。

## 受限服务器SSH任务

A3的host.identity/host.wait通过现有执行器、权威租约和实际持久回执运行，API/worker默认不设置KEEL_SSH_RUNNER_FILE，生产worker也不因发布自动开启。配置与目标helper部署、协议边界及独立测试步骤见 [SSH-RUNNER](SSH-RUNNER.md)。Compose只读挂载仓库外私有目录，禁用占位目录不包含密钥。042扩展现有约束后为schema39；新镜像启动要求042，旧schema38镜像不能直接回切，发布前先备份和隔离恢复演练，失败保留数据修复前进。

### 信任验证请求与043升级

受限首连验证复用SSH配置与Execution，仅验证已由运维安装的平台公钥/helper。配置临时凭据须有credential_version及credential_expires_at；请求冻结公开引用、资产版本及人工核验指纹，经另一位Admin/DL批准后由Admin/DL/SRE执行。详见 [SSH-RUNNER](SSH-RUNNER.md)。

043新增不可变ssh_trust_requests与machines.ssh_trust_source，发布镜像匹配schema40。升级前备份并在隔离库恢复/迁移演练，确认原机器状态/指纹及业务行数保持。仅向运行应用角色授予新表SELECT/INSERT/UPDATE，保留审计/迁移表限制；新表证据不应授予DELETE。既有互信来源统一legacy，不自动提升为真实验证；普通SSH拒绝legacy/sim，需人工核验和独立批准的repair补真实回执。

schema39镜像不能直接连接schema40库，保留数据修复前进；备份恢复另按批准流程执行。升级不设置真实目标、安装客户公钥或开启worker；默认SSH禁用。实际运行版本、迁移和发布证据仍以HANDOFF为准。


### 加密SSH域密钥

可选`KEEL_CREDENTIAL_MASTER_KEY_FILE`沿用私有SSH目录只读挂载，默认空值关闭。044迁移新增加密域密钥表；迁移与备份恢复演练后只给`keel_app`新表SELECT，不授INSERT/UPDATE/DELETE/TRUNCATE。主密钥独立保管，数据库备份不包含它；不得因部署自动生成或替换真实主密钥。API/worker继续使用受限角色，创建/撤销只由授权服务器owner CLI执行。命令、权限和控制面撤销边界见[SSH-RUNNER](SSH-RUNNER.md#加密项目域密钥提供者)。本功能发布不自动开启真实SSH、生产worker或目标公钥安装。


## 可选 OIDC 登录

默认关闭。私有provider文件、只读挂载、精确回调/subject绑定、TLS、重启及单进程授权状态的部署边界见 [OIDC](OIDC.md)。不得把provider文件/client secret加入镜像或日志；关闭或改绑定不等于撤销既有会话，紧急撤权使用现有本地停用/会话撤销流程。


## 可选 LDAP 组织账号

默认关闭。固定DN Bind、受保护配置及只读目录挂载、CA/主机名验证、无本地兜底和目录撤权边界见 [LDAP](LDAP.md)。目录口令不进入配置/数据库；配置变化需重启。

## Agent 身份基础（可选，默认关闭）

WP-1.5 的一次性注册、短期mTLS与连接心跳见 [AGENT-IDENTITY](AGENT-IDENTITY.md)。迁移047后须重新授予受限角色的列级消费/行锁权限；未设置 `KEEL_AGENT_IDENTITY_FILE` 时不创建listener。Compose只提供可选只读配置挂载，不发布新端口。后续048/049增加下文有界只读Runner；自动安装、遥测和证书续签仍未交付，不能据此签署完整P1验收。真实PG/TLS/gRPC/CLI验证使用 `python3 scripts/check-agent-integration.py` 的独立随机测试数据库，不操作运行库或物理设备。

## Agent 有界只读任务增量（048/049）

启用前备份并在独立恢复库演练048/049；048扩展既有执行模式，049只增加活动连接的任务流元数据，不另建队列。已有047的受限应用角色对agent_connections整表UPDATE覆盖新增列；保留注册/证书撤销的owner权限和审计禁止更新/删除约束。运行库迁移后不能直接回切schema44旧镜像。

任务worker通过受保护的KEEL_AGENT_WORKER_FILE显式启用；配置增加tasks_enabled=true，目标Agent也须显式开启。Compose只读挂载/run/keel/agent-worker且默认不开worker profile或端口。普通API身份listener不提供任务派发。配置、真实平台Admin登录、CLI幂等键和未知回执处置见[Agent运行说明](AGENT-IDENTITY.md)。默认发布保持所有真实执行通道关闭。

网页/API入口另由 `KEEL_AGENT_TASKS_ENABLED=true` 开启，默认false；开关只允许提交既有只读任务，不启动worker或签发身份。资产页的在线/离线来自当前证书绑定和30秒心跳/任务连接，不能替代主机健康；执行页沿用原五Tab，unknown在worker查询到真实终态后由当前有权限操作员显式对账。此入口无新增迁移；`npm run build` 后运行 `python3 scripts/check-agent-integration.py --browser` 可验证隔离PG/HTTP/真实Agent/浏览器链路。


## 本轮 P1 源码与可选通知配置（尚未发布）

050/051/052 新增 Agent 续签、owner 主密钥维护和独立 Webhook 消费证据；源迁移49条，运行仍46，当前未执行生产迁移。新二进制不能直接连接旧运行 schema。集中验收/备份恢复与权限演练后再按既有发布流程部署，不能自动启用真实通道。可选 worker `KEEL_WEBHOOK_SINKS_FILE` 默认空，独立只读 `/run/keel/webhooks` 挂载默认无密钥；详见 [WEBHOOK-NOTIFICATIONS](WEBHOOK-NOTIFICATIONS.md)。主密钥维护与审计归档分别见 [CREDENTIAL-MASTER-ROTATION](CREDENTIAL-MASTER-ROTATION.md)、[AUDIT-EVIDENCE](AUDIT-EVIDENCE.md)，本批没有执行这些生产维护操作。运行版本与验证事实以 HANDOFF 最后记录为准。
