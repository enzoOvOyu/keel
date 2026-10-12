# 开发交接

记录日期：2026-10-12。

## 用户目标

在 ubuntu@13.158.38.7 的远端服务器开发自动交付部署平台，代码保存到 https://github.com/enzoOvOyu/keel，方便换电脑继续。当前准备基础环境，等待后续设计文件。

## 当前工作位置

- 服务器：Ubuntu 26.04 LTS，约 16 GiB 内存、193 GiB 根磁盘。
- 主开发目录：`/opt/keel`，归 ubuntu 所有。后续代码修改与检查均在此目录执行。
- `/home/ubuntu` 的原始 Git 仓库保留，未把其 .ssh、历史记录等文件移入新仓库。
- Git main 基于原始提交 abb6822；沿用该提交的 Git 作者身份。
- Node.js 22.23.3、npm 10.9.9、Docker Engine 29.9.0、Compose 5.6.0 已安装。
- Docker 已启用开机启动，ubuntu 已加入 docker 组。
- Git origin 使用服务器专用 SSH 别名：`git@keel-github:enzoOvOyu/keel.git`。
- GitHub deploy key 已生成，等待用户添加仓库写权限；尚未确认推送成功。

## 当前实现

- Node 基础 API：GET /healthz 和 GET /api/v1，尚无业务接口或认证。
- Docker 容器以非 root 用户运行，文件系统只读，API 端口仅绑定宿主机 127.0.0.1:3000。
- API、PostgreSQL 16、Redis 7 容器已启动并通过健康检查；数据服务尚未接入 API。
- .env 中数据库随机密码只保存在服务器，文件权限 600，不提交到 Git。
- CI 检查、环境安装、预检、基于 commit SHA 的部署脚本和回滚操作说明已准备。
- 业务认证、项目管理、流水线、GitHub webhook、部署执行器、HTTPS 和前端等待后续实现。

## 继续开发

在新电脑配置自己的服务器登录密钥：

```sh
ssh -i /path/to/enzo-using.pem ubuntu@13.158.38.7
cd /opt/keel
cat AGENTS.md docs/HANDOFF.md
```

告诉后续开发助手：在远端 `/opt/keel` 工作，先读 AGENTS.md、README.md、docs/HANDOFF.md 和 docs/DEPLOYMENT.md，再检查 git status 与当前运行状态。GitHub 推送完成后也可在新电脑 clone 仓库查看文档，但远端是主开发环境。

不得复制 .env、私钥或个人令牌进 Git。当前本地的绝对私钥路径不会自动适用于新电脑。

## 验证与待办

- Node 官方 SHA256 校验和 Docker hello-world 通过。
- 远端 npm run check、npm test 的 4 项测试通过。
- Docker 镜像构建和 compose config 检查通过。
- 三个服务健康启动；API /healthz 返回实际 commit SHA。
- PostgreSQL 使用密码通过 TCP 执行 SELECT 1，Redis PING 返回 PONG。
- 三个容器重启恢复验收进行中；Docker 开机启动已启用，尚未实际重启服务器。
- GitHub deploy key 尚未被仓库接受，推送和 GitHub CI 尚未验证。

GitHub 权限配置完成后需要读取远端分支，确认与原始提交一致，再推送并验证 CI。
