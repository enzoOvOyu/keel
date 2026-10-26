# Keel

**Knowledge-led Execution, Enforcement & Lifecycle**

面向 B300/GB300 集群的交付部署平台。目标范围为 PRD v3.0.3 的 FR-0 网络前置域与 FR-1～FR-17；具体已实现能力、运行版本和剩余项见 [HANDOFF](docs/HANDOFF.md)。设计范围不等于实现进度。

## 开发入口

主工作区：当前本地环境的 `/opt/keel`，直接执行开发命令，不使用 tmux 或 SSH 绕转。

```sh
cd /opt/keel
git status -sb
```

先读 [AGENTS](AGENTS.md) 与 [Codex 开发入口](docs/DEVELOPMENT.md)。不调用会创建 tmux 会话的 `scripts/dev-session.sh`。

| 文档 | 负责的内容 |
|---|---|
| [PRD v3.0.3](PRD.md) | 产品需求、业务判据、BOM 权威格式和网络前置 |
| [DESIGN-SYSTEM v3.0.3](DESIGN-SYSTEM.md) | 品牌、工作台布局、页面、组件、交互与令牌 |
| [ARCHITECTURE](ARCHITECTURE.md) | 模块、数据、状态、协议和执行不变式 |
| [IMPLEMENTATION-PLAN](IMPLEMENTATION-PLAN.md) | 工作包、依赖、交付物和阶段退出条件 |
| [ACCEPTANCE](ACCEPTANCE.md) | 功能/性能/安全/UX/PoC 判据与证据格式 |
| [需求追踪](docs/requirements-traceability.json) | FR→WP→验收映射与实现状态 |
| [HANDOFF](docs/HANDOFF.md) / [DEPLOYMENT](docs/DEPLOYMENT.md) | 当前事实 / 操作方法 |

[开发入口](docs/DEVELOPMENT.md) 同时登记两份基准的差异和下游采用依据。历史审查与交接原文保留在 [history](docs/history/2026-10-23/README.md)，不作为当前开发指令。

## 访问与检查

当前环境部署预览为 3000；开发 API 为 3001，Vite 为 5173。源码更新不会自动更新已构建的 3000 镜像。

需要从另一台电脑访问历史远端部署时，才按实际主机配置可选隧道；本地开发不执行：

```sh
ssh -i /path/to/enzo-using.pem -N -L 3000:127.0.0.1:3000 -L 5173:127.0.0.1:5173 ubuntu@43.207.20.26
```

通过隧道访问 `http://localhost:3000/login`；需要热更新时使用已启动的 `http://localhost:5173`。私钥和真实配置不进入仓库。

```sh
python3 scripts/check-design.py
```

纯设计修改运行上面的检查；实现修改按影响运行 Go/前端检查，完整工程检查为 `make check`，构建为 `make build`。真实 PostgreSQL 集成测试需独立 `keel_test_` 数据库，缺失配置时的 Skip 不是通过；运行方法见 [DEPLOYMENT](docs/DEPLOYMENT.md)。

固定工具链见 [TOOLCHAIN](docs/TOOLCHAIN.json) 及 lockfile。源码、镜像、数据库迁移、浏览器交互与 GitHub Actions 结果分别核验；`/healthz` 只表示进程存活。

仓库：[enzoOvOyu/keel](https://github.com/enzoOvOyu/keel)。
