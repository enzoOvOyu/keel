# Keel web

React / TypeScript / Vite 前端；代码、安装与构建在远端 `/opt/keel` 完成。

- `/login`：Keel 船体龙骨动画、完整英文 slogan、本地登录、预会话 CSRF、明确的未配置/未初始化/统一错误/限流状态。
- `/projects`：真实授权项目列表、空态、平台管理员创建项目（防重复提交与请求幂等）。
- `/p/<projectId>`：真实项目基础信息、当前角色和规划状态；没有模拟设备指标或交付执行入口。
- HttpOnly 会话由后端维护，CSRF与认证数据仅存内存；浏览器持久化只保存语言和主题。
- `/admin/users`：平台账号管理；`/account/security`：本人修改密码。
- `/p/<projectId>/members`：当前项目成员授权；平台与项目审计分别位于 `/admin/audits` 与 `/p/<projectId>/audits`。
- 项目、账号、成员与审计列表使用真实分页；写入弹窗冻结目标与版本，冲突不自动重放。
- LDAP/OIDC 和交付业务模块按后续工作包实现。

从仓库根目录运行 `npm run check`、`npm run build`。`npm run dev:web` 在127.0.0.1:5173提供热更新，并代理 API 到3001。生产预览由 Go 服务提供构建文件，见根目录部署文档。

OpenAPI 类型由 `api/openapi.yaml` 生成，主题CSS从 `src/styles/tokens.json` 生成；不要手动维护生成文件。路由使用 TanStack 文件路由。
