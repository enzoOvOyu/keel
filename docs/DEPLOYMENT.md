# 远端开发与部署

主机 `ubuntu@13.158.38.7`，工作目录 `/opt/keel`。运行版本以 `curl http://127.0.0.1:3000/healthz` 为准，源码版本以 `git log -1` 为准。当前交付是P1本地认证与项目基础能力；完整交付平台仍在开发。

## 工具链

固定版本见 [TOOLCHAIN.json](TOOLCHAIN.json)、`go.mod/go.sum`、`package-lock.json`：Go1.27.2、Node22.23.3、Docker/Compose、tmux、C/C++编译器、protoc和Python3。服务器已准备；新服务器先检查现有端口、进程和代码，再按需运行bootstrap脚本。Go/Node下载验证官方SHA256。

`go generate ./ent` 使用go.mod固定的ent工具重新生成ORM代码。迁移SQL在 `internal/platform/migrations/`，编译时嵌入 `keel-admin` 与API；不用API启动时自动Schema.Create替代版本迁移。

## 数据与配置边界

| 用途 | 位置/端口 | 权限与范围 |
|---|---|---|
| Compose配置 | /opt/keel/.env | 600，仅服务器；包含数据服务和容器API配置 |
| 开发API配置 | /home/ubuntu/.config/keel/development.env | 600，应用角色keel_app |
| 管理CLI配置 | /home/ubuntu/.config/keel/admin.env | 600，数据库owner，仅迁移和服务器管理操作 |
| 首次管理员口令 | /home/ubuntu/.config/keel/bootstrap-admin.password | 600，随机生成，不进Git |
| PostgreSQL | 127.0.0.1:15432 | 主机开发连接；容器内postgres:5432 |
| 部署API/UI | 127.0.0.1:3000 | 已构建预览，通过SSH隧道访问 |
| tmux开发API/Vite | 127.0.0.1:3001 / 5173 | 开发热更新 |
| 开发MinIO | 127.0.0.1:9000 / 9001 | API / Console |
| 隔离测试PG/API | 127.0.0.1:15433 / 3102 | 独立测试数据，不能指向开发或部署服务 |

数据保存在postgres_data、redis_data、minio_data命名卷，禁止用 `down -v` 更新环境。当前认证/项目API接入PostgreSQL；Redis和MinIO已有开发数据服务，尚未接入完整调度、介质等业务。

MinIO社区镜像由固定官方源码构建，仅限开发：RELEASE.2025-10-15T17-29-55Z，revision `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`，源包SHA256在infra/minio/Dockerfile，保留LICENSE/CREDITS。生产对象存储的维护、备份与授权需单独评估；固定版本和开发可用不等于生产选型完成。

## 首次配置与迁移

先确认本机.env含有效随机数据服务凭据且权限600。首次建立数据服务：

```sh
cd /opt/keel
docker compose --profile data up -d --wait postgres redis minio
python3 scripts/configure-development.py
```

配置脚本生成/复用受限的keel_app账号，写入独立development.env/admin.env。已有账号缺失保存凭据时会拒绝覆盖，避免破坏现有连接。脚本不应用迁移、不创建平台管理员。

**迁移前备份数据库。** 备份包含身份和会话等敏感数据，只存权限受限目录；不要提交到Git。示例在服务器执行，实际数据库名/owner若已自定义需相应调整：

```sh
umask 077
mkdir -p /home/ubuntu/.local/state/keel/backups
backup_file="/home/ubuntu/.local/state/keel/backups/pre-migration-$(date -u +%Y%m%dT%H%M%SZ).dump"
(set -C; docker compose exec -T postgres pg_dump -U keel -d keel -Fc > "$backup_file")
test -s "$backup_file"
docker compose exec -T postgres pg_restore -l < "$backup_file"
```

示例使用时间戳和禁止覆盖模式保存备份。核验文件非空且pg_restore可读取目录；不能仅凭命令退出成功声称恢复演练通过。

迁移在独立子shell加载owner配置，避免owner凭据留在普通开发进程环境：

```sh
(
  set -a
  . /home/ubuntu/.config/keel/admin.env
  set +a
  go run ./cmd/keel-admin migrate
)
python3 scripts/configure-development.py --grant
```

迁移使用事务、advisory lock和checksum；已执行版本不能直接改写SQL。API启动只检查迁移，不自动修复或降级。受限应用账号可操作认证/项目表，但不能修改/删除审计或写迁移历史；管理员保留DDL能力，因此审计触发器不能代替生产归档与权限隔离。

## 初始化与服务器账号管理

首次管理员无默认密码。由服务器操作员交互输入，或使用当前用户拥有、权限600且不是符号链接的密码文件：

```sh
(
  set -a
  . /home/ubuntu/.config/keel/admin.env
  set +a
  go run ./cmd/keel-admin init-admin --username enzo --display-name Enzo     --password-file /home/ubuntu/.config/keel/bootstrap-admin.password
)
```

省略password-file时要求真实终端并隐藏输入；不接受密码命令行参数或stdin管道。初始化互斥且一次性，已有管理员时拒绝覆盖。密码至少12字节、最多1024字节；初始口令仅供本人从安全的服务器会话取用，首次取用后可用CLI更换并按凭据保留策略处理初始文件。

以下命令同样只在加载admin.env的子shell内运行；实际账号、项目ID及权限按需要填写：

```sh
go run ./cmd/keel-admin user-create --username delivery --display-name 交付工程师
go run ./cmd/keel-admin user-password --username enzo
go run ./cmd/keel-admin user-disable --username delivery
go run ./cmd/keel-admin user-unlock --username delivery
go run ./cmd/keel-admin user-unlock --username delivery --ip 127.0.0.1
go run ./cmd/keel-admin member-grant --username delivery --project PROJECT_ID --role DE
go run ./cmd/keel-admin member-revoke --username delivery --project PROJECT_ID
```

user-create默认不是平台管理员；需要时显式加 `--admin`。项目角色可选Admin/DE/DL/NE/SRE/Viewer，项目Admin没有平台创建项目权限。禁用、密码重设和成员变更立即撤销原会话；不能禁用最后有效管理员。user-unlock默认仅解账号锁，IP锁需明确给出IP；所有操作都写审计。

## 开发与检查

```sh
npm ci
make check
make build
sh scripts/dev-session.sh
tmux attach -t keel
```

make build生成bin/keel-api与bin/keel-admin。make dev-api调用scripts/dev-api.sh，自动加载development.env并监听3001；Vite5173代理到3001。可用 `KEEL_CONFIG_FILE` 覆盖开发配置路径。已有tmux窗口不被脚本重启；源码变更后确认自己的API进程已停止再重启该窗口。

make check默认运行设计、Go vet/race、前端类型及构建检查。真实PostgreSQL测试需要 `KEEL_TEST_DATABASE_URL`；缺失时明确Skip。测试会重建目标数据库的public schema，**仅允许独立的keel_test_命名数据库**，不能填写开发数据库连接。

本轮验收临时使用127.0.0.1:15433的独立PG、数据库keel_test_auth，测试配置为/tmp/keel-test-db.env（600）。验收后移除测试容器、auth-test窗口与临时凭据；它们不是长期运行的开发依赖。复检先参考CI定义重建隔离环境，确认临时配置存在后再执行：

```sh
(
  set -a
  . /tmp/keel-test-db.env
  set +a
  make check
)
```

HTTP黑盒使用同一隔离PG里的另一个数据库keel_test_http及独立API3102，防止Go测试重建schema相互干扰。scripts/check-auth-integration.py要求KEEL_TEST_DATABASE_URL、KEEL_ADMIN_BINARY和匹配的API，只接受测试端口和数据库；它创建随机测试账号/项目，不动部署.env。12组检查覆盖认证、CSRF、六角色、跨项目、幂等、限流及撤销。

浏览器检查使用Playwright1.64.0/Chromium。check-auth-integration.py可通过KEEL_AUTH_BROWSER_CREDENTIALS_FILE输出权限600的临时测试凭据，随后运行scripts/check-auth-browser.mjs；验证完成由调用方删除该临时文件。通用视觉/品牌检查为scripts/check-browser.mjs。完整可复用顺序见.github/workflows/ci.yml；本地通过与GitHub Actions实际运行成功分开记录。

## 发布与回滚

数据库迁移、授权与管理员初始化完成后，在干净工作区发布经验证的提交：

```sh
sh scripts/preflight.sh
sh scripts/deploy.sh
curl --fail http://127.0.0.1:3000/healthz
curl --fail http://127.0.0.1:3000/api/v1
curl --fail http://127.0.0.1:3000/api/v1/auth/providers
```

部署脚本构建keel:<完整Git SHA>并等待API健康，成功后更新.env的KEEL_IMAGE，旧镜像保留。单机更新可能短暂中断；失败不自动回滚。/healthz仅证明进程存活，需再查数据库相关认证状态并实际验证登录/授权。

回滚选择已保留镜像，更新.env的KEEL_IMAGE，再执行 `docker compose up -d --no-build --wait --wait-timeout 90 api`。**镜像回滚不会回滚数据库**；schema不兼容时不能强行跳过版本校验，应按迁移恢复方案处理。禁止为解决启动失败删除数据卷。

## 网络、GitHub与换电脑

开发HTTP仅允许显式development模式和精确的loopback Origin。配置脚本允许localhost/127.0.0.1的3000、3001、5173。浏览器Cookie为HttpOnly/SameSite=Lax，开发例外关闭Secure；生产必须配置HTTPS Origin、Secure Cookie和独立环境。后端不信任任意Forwarded/X-Forwarded-For，限流来源为直接TCP连接IP。

自己的电脑通过SSH隧道映射3000或5173访问。换电脑后连接同一服务器，读AGENTS/HANDOFF并检查git status、Compose和tmux。origin为git@github.com:enzoOvOyu/keel.git，使用服务器已有默认SSH身份；私钥、.env和配置文件不得进入Git。

推送先fetch检查分支，再正常push，不强推。Git bundle仅备份已提交历史，不包含数据库、密码文件或密钥；这些需要独立受控备份。
