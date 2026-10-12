# keel — 开发助手协作约定

## 开始与工作位置

- 先读本文件、README.md、docs/HANDOFF.md、docs/DEPLOYMENT.md，再看本次相关设计章节和 git status。
- 用户已批准 v2.5 复审修订、keel 品牌和登录设计；当前工作为设计落地与 Go/React 开发环境准备。功能是否实现以 HANDOFF 的验证证据为准。
- 主开发环境是 `ubuntu@13.158.38.7:/opt/keel`，代码归档目标是 `https://github.com/enzoOvOyu/keel`。本地设计目录可编辑后同步；业务代码、构建与环境验证在远端完成。
- 持久开发用 `sh scripts/dev-session.sh` / `tmux attach -t keel`，沿用已有会话，不重启或覆盖用户正在使用的窗口。api3001/web5173用于开发，部署预览3000独立。SSH断开不终止tmux；主机重启后需重建开发会话。
- 首次接通先检查代码、进程、容器、磁盘、监听端口，不覆盖用户改动、不自动卸载软件。保留数据库卷和可回滚版本。

## 文档职责与优先级

- 用户当前指令及已经批准的决定优先。根目录 PRD.md 管功能和业务判据，ARCHITECTURE.md 管模型/协议/状态机，DESIGN-SYSTEM.md 管视觉/交互，IMPLEMENTATION-PLAN.md 管工作依赖，ACCEPTANCE.md 管验证证据。
- `docs/requirements-traceability.json` 追踪 FR/WP/验收及复审修改；历史 REVIEW-FINDINGS.md、AI-EXECUTABILITY-AUDIT.md 只提供来源，旧建议与示例不是第二套生效规范。
- 本文件管开发行为，HANDOFF 管已验证工程状态；不要复制整份业务规范到这里。设计修订同步相关文档及追踪记录。
- 遇到影响业务判据、权限、危险操作或接口兼容的冲突，记录具体条款与建议并就受影响部分澄清，继续独立工作。普通实现细节按既定约束自主完成并记录，不对每步重复申请许可。

## 实现边界

- 品牌字标 `keel`；后端 Go/gin/ent、模块化单体，前端 React/TypeScript/Vite、TanStack Router/Query/Table/Virtual、Radix + 自有 CSS 令牌。版本以 go.mod、package-lock.json、工具链清单为准。
- 按需加载领域依赖；不因为环境准备就实现全部业务或把占位 API 标成可用。认证就绪前只允许公开只读健康/版本/公开认证配置，所有业务资源先认证授权。
- 外部交付设备、BMC、网络平台、GPU 工具与 AI 适配器开发默认 mock/sim；远端开发服务器属于获授权的工程环境，可以 SSH 开发。真机 PoC 必须独立记录，不用模拟结果冒充硬件达标。
- 接口契约先行，前端类型由正式契约生成。页面复用 DESIGN-SYSTEM 组件和令牌；版本选择、就绪门、危险确认、项目作用域遵循规范。登录不使用默认口令或浏览器持久化 bearer token。
- 机器状态不替代门禁证据；执行前冻结目标与版本并核验有效范围。Agent/SSH/BMC 共享资源冲突规则；不盲目重试结果未知的固件、装机或清理动作。
- 不把 Docker socket 挂入公共 API；执行器和 PXE 网络操作需要独立权限边界。

## 凭据与发布

- 私钥、token、密码、真实 .env 不进 Git、不打印到日志。SSH 私钥位置是每台电脑的本地配置，不写死在脚本。服务器 .env 留服务器，权限 600。
- GitHub 认证成功后先 fetch/检查分支再正常 push，不强推、不覆盖未知远端历史；没有推送成功时明确记录并保存离线 Git bundle。
- 配置存在、代码构建通过、模拟测试通过、远端健康、GitHub 已推送、CI 已通过是不同证据，分别报告。

## 验证与交接

- 设计修改运行 `python3 scripts/check-design.py`。实现修改按实际范围运行 Go 测试/静态检查、前端类型检查/构建；仓库提供 `make check` 时优先使用。
- 涉及容器，验证 `docker compose config --quiet`、镜像构建、健康检查及依赖连接；不用 `down -v` 处理升级。认证和执行边界变更必须包含有意义的负例验证。
- 不声称单机开发服务器能证明万台真实硬件吞吐。标注功能模拟、传输集成、容量测试、硬件 PoC 的不同范围。
- 结束更新 HANDOFF：工作目录、分支/提交、运行服务、完成内容、实际命令结果、未完成项与下一步，保证换电脑能继续。
