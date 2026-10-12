# keel

自动交付部署平台。项目需求与技术设计基线为 v2.5，覆盖17个功能域；当前工程处于基础环境和登录页预览阶段。

## 已可运行

- Go/gin API、React/TypeScript/Vite 前端，`/login` 支持中英、明暗主题与移动布局。
- `/healthz` 仅表示进程存活；`/api/v1` 明确返回 `business_ready: false`。
- 登录页读取公开身份配置；认证尚未实现，禁止提交凭据，不产生会话。业务接口拒绝未认证访问。
- PostgreSQL、Redis、开发用 MinIO 数据服务由 Compose 管理；API 尚未接入业务存储。
- OpenAPI → TypeScript 类型、CSS tokens、文件路由生成，以及构建、检查和部署脚本。

## 在服务器继续开发

主工作区：`ubuntu@13.158.38.7:/opt/keel`。先读 [AGENTS.md](AGENTS.md) 和 [交接记录](docs/HANDOFF.md)，再检查工作区。

```sh
cd /opt/keel
npm ci
make check
```

已部署预览只监听服务器 `127.0.0.1:3000`，在自己的电脑建立隧道：

```sh
ssh -i /path/to/enzo-using.pem -N -L 3000:127.0.0.1:3000 ubuntu@13.158.38.7
```

打开 `http://localhost:3000/login`。密钥路径由各电脑自己配置。

开发热更新：服务器分别运行 `make dev-api`（3001）和 `make dev-web`（5173，代理到3001），电脑额外映射5173端口。已部署3000不受开发进程影响。

## 设计入口

| 文档 | 职责 |
|---|---|
| [PRD](PRD.md) | 功能与业务判据 |
| [架构](ARCHITECTURE.md) | 数据模型、协议、状态与边界 |
| [前端设计](DESIGN-SYSTEM.md) | keel品牌、登录、页面与交互 |
| [实施计划](IMPLEMENTATION-PLAN.md) | 56个工作包与依赖 |
| [验收](ACCEPTANCE.md) | 189项验收ID与证据要求 |
| [需求追踪](docs/requirements-traceability.json) | FR、复审、工作包及验收映射 |
| [部署](docs/DEPLOYMENT.md) | 工具链、部署、回滚与服务连接 |

设计通过检查不等于业务已实现。真实认证、项目隔离、ent迁移、执行调度、Agent、硬件联调和万台容量验证按实施计划推进。
