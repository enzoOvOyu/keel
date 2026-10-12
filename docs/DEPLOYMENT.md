# 远端环境与部署

目标：ubuntu@13.158.38.7。约定项目路径 `/opt/keel`，已在服务器创建，归 ubuntu 所有。

## 首次连接

私钥只存在各电脑本地，新电脑应通过安全方式配置自己的密钥。当前电脑提供的路径为 `/Users/yubing/Downloads/enzo-using.pem`；另有 `/private/var/root/enzo-using.pem` 副本。私钥不在仓库中。

```sh
chmod 600 /path/to/enzo-using.pem
ssh -i /path/to/enzo-using.pem -o IdentitiesOnly=yes ubuntu@13.158.38.7
```

首次在新电脑连接时核验服务器主机指纹。查看 `/etc/os-release`、`df -h`、`ss -lntp`、`docker ps` 和现有项目目录，确认没有需要保留的冲突服务。

如果服务器是受支持的 Ubuntu、尚未安装 Docker 且没有冲突软件，运行仓库中的 `sudo sh scripts/bootstrap-ubuntu.sh`。脚本会安装 Git、Docker 和 Compose，并启用 Docker 开机启动；发现已有 Docker 或冲突包会停止自动安装。其他系统需另行适配。

## 获取代码

以下仅在仓库已经推送且路径不存在时执行。私有仓库用服务器专用的只读 GitHub deploy key；避免使用含令牌的 URL。

```sh
sudo install -d -o ubuntu -g ubuntu /opt/keel
git clone git@keel-github:enzoOvOyu/keel.git /opt/keel
cd /opt/keel
cp .env.example .env
chmod 600 .env
```

部署账户需要 Docker 权限。将用户加入 docker 组等同于授予主机管理权限，应在确认账户用途后配置。没有权限时由管理员运行 Docker 命令。

服务器的 `keel-github` SSH 别名使用专用密钥 `~/.ssh/keel_github_ed25519`，GitHub 主机公钥从官方 HTTPS metadata 接口取得。新服务器需要重新配置自己的别名和 deploy key。

主机开发运行时由 `sudo sh scripts/bootstrap-node.sh` 安装 Node.js 22；脚本从 Node 官方站点下载并校验 SHA256。ubuntu 已加入 docker 组，新登录会话生效。

## 部署已提交版本

```sh
cd /opt/keel
sh scripts/preflight.sh
sh scripts/deploy.sh
curl --fail http://127.0.0.1:3000/healthz
```

脚本要求干净的 Git 工作区，镜像使用完整 commit SHA 标记，等待健康检查通过后才记录新版本到 `.env`。并发部署用目录锁阻止。进程被强制终止留下锁时，确认没有部署仍在运行，再手动删除 `.deploy-lock`。构建过程需要访问容器镜像源。

这是单服务器重建部署，可能短暂中断服务。失败不会自动回滚；旧镜像保留，可按以下步骤回滚。

## 更新与回滚

更新前检查工作区与分支，采用 `git pull --ff-only`，随后运行部署脚本。查看 `.env` 的 `KEEL_IMAGE` 并记录旧镜像标签。

回滚时，将 `.env` 的 `KEEL_IMAGE` 改为已保留的 `keel:<旧完整SHA>`，然后执行：

```sh
docker compose up -d --no-build --wait --wait-timeout 90 api
curl --fail http://127.0.0.1:3000/healthz
```

镜像回滚不处理数据库迁移。将来引入迁移时要补充独立的迁移和备份恢复流程。暂不执行镜像清理，以保留回滚版本。

## 后续自动发布

先完成远端基础验证，再配置 GitHub 环境和部署专用 SSH 凭据、可信主机指纹及目标分支。当前 workflow 只做 CI，没有自动连接服务器。域名、HTTPS、平台登录、审计、项目接入、任务队列和独立部署执行器均是后续工作。
