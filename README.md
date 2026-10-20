# Keel

**Knowledge-led Execution, Enforcement & Lifecycle**

自动交付部署平台。品牌字标使用 **Keel**，仓库、包名与服务标识使用 `keel`。设计基线为 PRD v3.0.3，覆盖 FR-0 网络前置域与 FR-1～FR-17。当前代码回到 GitHub 的 `p1-complete`（`e7d5f1a`）：P1 阶段门槛已闭环，P2～P5 和 FR-0 网络底座仍待开发。

## 当前可用能力

- `/login`：船体龙骨与肋骨线条动画、中文/英文、明暗主题、移动布局与减少动画偏好。
- 本地账号登录、预登录 CSRF、HttpOnly 服务端会话、退出撤销、账号/IP 失败限流。没有默认口令或公开注册；首个管理员通过服务器 CLI 初始化。
- `/projects`：真实授权项目列表与空状态；平台管理员可创建项目。`/p/<projectId>` 显示该项目的真实基础信息和当前角色。
- PostgreSQL 持久化用户、会话、项目、成员、审计与幂等回执；项目创建、创建者成员关系和审计同事务。六种项目角色已建立，当前只开放已实现的项目权限。
- Go/gin/ent 后端、React/TypeScript/Vite 前端、OpenAPI 类型生成、受校验的版本迁移、Compose 与 tmux 开发环境。

`/healthz` 只表示进程存活。`/api/v1` 返回 `phase: p1-foundation`；`business_ready: false` 表示整体交付业务尚未就绪。认证状态为 `not_configured`、`setup_required` 或 `ready`，登录页据此展示真实入口。

LDAP/OIDC、成员管理网页、项目生命周期变更、ProjectAsset、执行调度、Agent 与硬件联调属于后续工作；页面没有虚构设备指标或执行能力。

## 在远端继续开发

主工作区：`ubuntu@43.207.20.26:/opt/keel`。先读 [协作约定](AGENTS.md)、[交接记录](docs/HANDOFF.md) 与 [部署说明](docs/DEPLOYMENT.md)。

```sh
cd /opt/keel
git status -sb
sh scripts/dev-session.sh
tmux attach -t codex-keel
```

已有 `codex-keel` 会话包含 work/api/web/logs；脚本不重启已有窗口。按 **Ctrl-b，再按 d** 分离，SSH 断开后进程继续。开发 API 使用3001，Vite使用5173，部署预览使用3000。

从自己的电脑建立隧道：

```sh
ssh -i /path/to/enzo-using.pem -N   -L 3000:127.0.0.1:3000   -L 5173:127.0.0.1:5173   ubuntu@43.207.20.26
```

访问 `http://localhost:3000/login`，或打开 `http://localhost:5173` 进行热更新开发。每台电脑自行配置私钥路径，不把密钥放进仓库。

## 检查与持久化

```sh
npm ci
make check
make build
```

`make check` 包含设计检查、Go vet/race、前端类型检查和构建。真实 PostgreSQL 单测需另设 `KEEL_TEST_DATABASE_URL`；未配置时会明确跳过这部分，不能据此声称集成验证通过。隔离数据库及 HTTP/浏览器验证方式见 [部署说明](docs/DEPLOYMENT.md)。

`make dev-api` 自动读取服务器 `/home/ubuntu/.config/keel/development.env`，使用受限数据库账号 `keel_app`。数据库迁移和账号管理使用独立 `admin.env`，通过 `keel-admin` CLI 执行；API 启动不会自动修改数据库结构。两份配置及初始密码文件均仅保留服务器，权限600。

## 设计入口

| 文档 | 职责 |
|---|---|
| [PRD](PRD.md) | 功能与业务判据 |
| [架构](ARCHITECTURE.md) | 模型、协议、状态与边界 |
| [前端设计](DESIGN-SYSTEM.md) | Keel品牌、登录、页面与交互 |
| [实施计划](IMPLEMENTATION-PLAN.md) | 工作包与依赖 |
| [验收](ACCEPTANCE.md) | 验收ID与证据要求 |
| [需求追踪](docs/requirements-traceability.json) | 需求、设计和实现状态 |
| [工具链](docs/TOOLCHAIN.json) | 固定版本与来源 |

GitHub：`https://github.com/enzoOvOyu/keel`。服务器 origin 使用 `git@github.com:enzoOvOyu/keel.git`，通信已恢复；源码提交、运行镜像和 GitHub Actions 的结果分别核验。
