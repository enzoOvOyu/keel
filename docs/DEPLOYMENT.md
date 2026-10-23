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

迁移使用事务、advisory lock 与 checksum。应用角色不能改/删审计或写迁移历史；数据库管理员的权限仍需生产归档与恢复制度约束。

## 4. 管理员与项目成员

首位管理员无默认密码；在加载 `admin.env` 的子 shell 中交互初始化，或传入当前用户拥有、权限600且非符号链接的密码文件：

```sh
go run ./cmd/keel-admin init-admin --username enzo --display-name Enzo --password-file /home/ubuntu/.config/keel/bootstrap-admin.password
```

初始化只允许一次。密码至少12字节、最多1024字节，不通过命令参数或 stdin 明文传入；省略密码文件时从真实终端隐藏输入。账号创建、改密、禁用、解锁、成员授权等用 `go run ./cmd/keel-admin --help` 和相应子命令查看当前参数，不在本文复制第二份完整 CLI 目录。

当前六角色实现与目标 NDE/NH 扩展的差异见 HANDOFF/DEVELOPMENT；不能因 CLI 接受某个角色名称就声称完整网络权限已交付。平台 Admin 与项目 Admin 分开；禁用/改密/成员变更撤销旧会话，最后有效管理员保护由服务端执行。

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
