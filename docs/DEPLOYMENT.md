# 远端开发与部署

主机 `ubuntu@13.158.38.7`，工作目录 `/opt/keel`。运行版本以 `curl http://127.0.0.1:3000/healthz` 为准，源码版本以 `git log -1` 为准。

## 工具链

Ubuntu 26.04、Go 1.27.2、Node 22.23.3/npm 10.9.9、Docker 29.9.0/Compose 5.6.0；安装了 make、C/C++编译器、protobuf-compiler、ripgrep 和 Python3。Go/Node 的安装脚本验证官方SHA256，版本固定在 `.go-version`、`.nvmrc`。JS依赖使用 package-lock，Go依赖使用go.mod/go.sum。Go主机安装脚本当前支持Linux amd64。

现有服务器已安装。新服务器先检查已有软件和端口；按需 `sudo sh scripts/bootstrap-ubuntu.sh` 安装Docker，再运行 `sh scripts/bootstrap-dev.sh`。脚本不覆盖不同版本的已有Node；首次`.env`需单独初始化。Docker组有主机管理能力，只授予开发管理员。

## 配置与数据

`.env` 只在服务器，权限600；从 `.env.example` 建立时需为 PostgreSQL 与 MinIO 生成随机凭据。禁止提交或输出实际配置。PostgreSQL/Redis没有宿主机端口；MinIO API/Console仅绑定127.0.0.1:9000/9001；前端/API预览仅绑定127.0.0.1:3000。

```sh
docker compose --profile data up -d --build --wait postgres redis minio
docker compose --profile data ps
```

数据保存在 postgres_data、redis_data、minio_data 命名卷。不要用 `down -v` 更新环境。API目前不连接数据服务，健康接口不表示数据库就绪。开发连接数据库可通过 `docker compose exec`，将来主机进程确有需要时另加回环端口覆盖配置。

MinIO社区已转源码分发、官方仓库归档；当前仅开发使用固定官方提交 `9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a`（RELEASE.2025-10-15T17-29-55Z）编译，源包SHA256固定在 infra/minio/Dockerfile，保留LICENSE/CREDITS。构建源码来自[官方发布](https://github.com/minio/minio/releases/tag/RELEASE.2025-10-15T17-29-55Z)，项目状态见[官方仓库](https://github.com/minio/minio)。这不是生产存储选型结论：生产前必须评估持续安全维护、备份恢复和部署授权要求，暂不改已批准的业务S3接口。源码版本通过镜像标签及OCI revision标识，不依赖二进制默认版本字符串。

## 检查与部署

```sh
npm ci
make check
sh scripts/preflight.sh
# 提交经检查的工作后，要求干净工作区：
sh scripts/deploy.sh
curl --fail http://127.0.0.1:3000/healthz
```

部署脚本构建 `keel:<完整Git SHA>`，等待API健康才更新.env的KEEL_IMAGE；旧镜像保留。目录锁防止并发部署，强制终止留下的锁须确认无部署进程后清理。单机部署可能短暂中断，不承诺零停机。失败不自动回滚，依据健康和日志选定保留镜像。

回滚：把 `.env` 的 KEEL_IMAGE 更新为已保留的 `keel:<旧SHA>`，再运行 `docker compose up -d --no-build --wait --wait-timeout 90 api` 并核对 healthz版本。回滚镜像不回滚数据库，业务迁移引入时须另做备份/迁移恢复计划。

## 热更新与预览

生产构建预览3000；本地开发API3001；Vite5173。服务器用 `sh scripts/dev-session.sh` 启动持久tmux会话并 `tmux attach -t keel`；也可用两个终端 `make dev-api` 与 `make dev-web`，浏览器通过SSH隧道访问5173。根目录 `make build` 生成web/dist和bin/keel-api。

## GitHub与换电脑

origin是 `git@github.com:enzoOvOyu/keel.git`，使用服务器已有且认证成功的默认SSH身份。此前 `keel-github` 专用别名使用的另一把key未被仓库接受，不用于当前origin。私钥留服务器，不放进Git。不要因为专用别名失败而误判默认GitHub身份不可用。

推送前先fetch查看远端分支，再正常push，禁止强推未知历史。CI定义已加入仓库，只有GitHub实际执行成功才算CI通过；SSH部署key不提供GitHub Actions API读取能力。当前没有自动连接服务器的发布workflow。

换电脑只需安全配置SSH密钥并连接同一远端目录，先读AGENTS/HANDOFF；GitHub可用后可clone浏览代码。离线bundle可保存完整已提交历史，但不包含.env/数据卷/私钥；这些需独立备份策略。
