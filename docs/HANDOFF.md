# keel 开发交接

记录日期：2026-10-12。主工作区 `ubuntu@13.158.38.7:/opt/keel`，分支main。当前提交用 `git log -1 --oneline` 查看；运行镜像提交用 `/healthz` 的version查看。上轮Node基础版本37e72b5保留在Git历史与旧Docker镜像中。

## 本轮完成

- 用户批准的设计修订全部纳入v2.5：17个功能域、34个前端领域组件、56个WP与189个验收ID，追踪表区分设计完成与业务待实现。
- 统一品牌keel；新增登录设计、项目/角色/危险操作边界，补门禁证据版本、分布式执行、资源锁与未知结果恢复、时序/来源/单位、固件恢复与归档清理顺序。
- AGENTS.md已整理为助手协作约定，业务判据仍由PRD等专门文档维护。
- Go/gin替代Node临时API，React19/TypeScript5/Vite6前端可构建；TanStack Router/Query、中英与明暗主题、令牌生成、OpenAPI类型生成已接通。ent领域模型/数据库迁移尚待P1。
- `/login` 已实做桌面和移动预览，身份未启用时明确提示并禁提交。没有默认账户、假登录、业务KPI或真实会话。
- Go/Node固定工具链、编译器、protoc、Docker/Compose、tmux3.6持久会话、Makefile、CI、部署脚本与数据服务已准备。版本详见TOOLCHAIN.json和锁文件。
- PostgreSQL16、Redis7、开发MinIO采用命名卷，原有数据库/Redis卷保留。MinIO官方社区镜像不可获取，已改从固定官方源码校验构建，具体约束见DEPLOYMENT。

## 实际验证证据

- `python3 scripts/check-design.py`：17FR、34组件、引用、权限表列数、56工作包依赖无环通过；不表示业务验收通过。
- Go静态检查、`go test -race ./...`通过；测试含业务API未认证拒绝、未启用认证不发session，以及关闭服务等待在途请求排空。
- 前端类型检查、OpenAPI类型生成、生产构建通过；npm生产依赖审计0条报告漏洞。TanStack生成器有上游循环依赖提示，构建及浏览器运行均通过。
- Docker多阶段镜像构建、Compose配置、四服务健康检查通过。
- Playwright1.64.0/Chromium实际浏览器8组场景通过：中英亮暗、390px布局、禁提交且无凭据网络/存储、密码显示、偏好持久化、根路由、健康入口、故障注入与重试；正常页面无控制台错误和外部请求。
- 浏览器截图见screenshots/login-light.png、login-dark.png、login-mobile.png；检查脚本scripts/check-browser.mjs。
- 数据检查脚本scripts/check-data.py实际验证MinIO创建桶/PUT/GET内容一致/DELETE，PostgreSQL密码认证TCP SELECT1，Redis PING。keel-dev-smoke桶可保留，探测对象已删除。
- Docker开机启动已启用，服务unless-stopped。本轮四个容器实际重启后恢复healthy，并通过数据读写复检；未执行真实服务器重启。

## GitHub与备份

GitHub main已成功推送并核对服务器/远端同一提交；后续状态仍按下方命令核验。目标 `https://github.com/enzoOvOyu/keel`；origin使用服务器默认SSH身份 `git@github.com:enzoOvOyu/keel.git`，认证与仓库读取已成功。专用keel-github别名的另一把key失败，不影响默认身份。推送状态以 `git status -sb` 与 `git ls-remote origin refs/heads/main` 对比HEAD核验。

CI配置存在、远端本机检查通过与GitHub Actions通过是不同证据；本轮未通过Actions API确认CI结果。无需为了代码推送把个人token写入服务器或仓库。

离线Git bundle放 `/tmp/keel-v25.bundle`（交付时创建）并带回当前电脑；包含已提交历史，不含.env、密钥或数据卷。旧环境备份 `/tmp/keel-before-v25.bundle` 保留。bundle不是数据库备份。

## 换电脑接续

```sh
ssh -i /path/to/enzo-using.pem ubuntu@13.158.38.7
cd /opt/keel
cat AGENTS.md docs/HANDOFF.md
git status -sb
docker compose --profile data ps
```

预览：电脑执行 `ssh -i /path/to/enzo-using.pem -N -L 3000:127.0.0.1:3000 ubuntu@13.158.38.7`，浏览器打开 `http://localhost:3000/login`。服务器上的开发进程 `make dev-api` 用3001、`make dev-web`用5173，与部署预览分开；需要热更新时另映射5173。

浏览器复检：当前服务器已在 `/tmp/keel-browser-check` 安装Playwright和Chromium，可运行 `KEEL_PLAYWRIGHT_MODULE=/tmp/keel-browser-check/node_modules/playwright/index.mjs node scripts/check-browser.mjs`。新服务器先独立安装同版Playwright及浏览器。截图/测试探测数据不是硬件业务证据。

## tmux持久开发

服务器已有tmux3.6，已创建`keel`会话：work是交互工作台，api运行3001开发后端，web运行5173热更新，logs跟随Compose日志。会话设置限定在keel，没有覆盖个人.tmux.conf。

```sh
sh scripts/dev-session.sh  # 幂等；已有会话不会重启任何窗口
tmux attach -t keel
```

Ctrl-b再按d仅分离，SSH断开后进程继续。Ctrl-b再按w切窗口。进程退出时保留窗口错误输出，排除问题后可 `tmux respawn-window -t keel:api`（或web）。脚本遇到已有非本会话占用3001/5173会停止，不杀未知进程。开发会话不跨主机重启自动恢复；重启后重跑脚本。Compose服务独立由Docker自动启动。

## 下一步

按P1先建立ent模型/迁移、项目隔离、真实本地/LDAP/OIDC认证与六角色权限，再推进执行器等业务。API当前没有连接持久化服务，`healthz`只证明进程存活。暂未接入真实BMC/GPU设备，也未做万台容量PoC；RAG/pgvector及生产对象存储维护方案按计划评估。设计中的接口与状态不要标成现有功能。
