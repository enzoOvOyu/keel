# Keel

自动交付部署平台。目前准备开发与部署基础，等待设计文件后再实现界面和业务。

目标仓库：https://github.com/enzoOvOyu/keel

目标服务器：`ubuntu@13.158.38.7`。实际连接与部署状态见 [开发交接](docs/HANDOFF.md)。

## 本地开发

主开发目录为远端 `/opt/keel`，服务器已安装 Node.js 22、Docker 和 Compose，后续在远端开发。其他电脑仅需配置 SSH 连接即可继续；本地开发也可使用 Node.js 22。Compose 需支持 `up --wait`。

```sh
npm ci
npm run check
npm test
npm run dev
```

`GET /healthz` 返回进程健康和代码版本；`GET /api/v1` 返回基础版本信息。没有用户认证、项目管理、流水线或部署执行接口。健康检查目前不检查数据库或队列，不能代表业务就绪。

## 容器启动

```sh
cp .env.example .env
chmod 600 .env
docker compose up -d --build --wait api
curl --fail http://127.0.0.1:3000/healthz
docker compose logs --tail=100 api
```

默认只监听宿主机回环地址。远端开发使用 SSH 隧道，待域名和认证方案确定后接入 HTTPS 反向代理。

```sh
ssh -i /path/to/enzo-using.pem -L 3000:127.0.0.1:3000 ubuntu@13.158.38.7
```

PostgreSQL 和 Redis 配置放在可选的 `data` profile，尚未接入 API。当前服务器已启动它们作为开发基础资源；新环境默认不启动。启用前设置 `.env` 内的 `POSTGRES_PASSWORD`：

```sh
docker compose --profile data up -d --wait
```

这两个服务没有发布宿主机端口，数据保存在命名卷。首次初始化后更改 `.env` 密码不会改变已有数据库用户密码；不得使用 `down -v` 处理更新，否则会删除数据卷。

## 服务器准备与部署

先确认服务器系统、现有服务和 Docker 状态，再执行 [部署说明](docs/DEPLOYMENT.md)。GitHub Actions 配置会检查代码、构建镜像和验证容器健康；目前没有启用服务器自动发布。CI 在 GitHub 的实际结果尚未验证。

Docker 安装脚本采用[官方 Ubuntu 安装方法](https://docs.docker.com/engine/install/ubuntu/)。
