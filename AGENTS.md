# 项目协作约定

- 开始工作先读 README.md、docs/HANDOFF.md 和 docs/DEPLOYMENT.md。
- 当前任务是基础准备；收到设计文件后再实现界面和业务，不把占位接口写成已完成功能。
- 远端目标 ubuntu@13.158.38.7；代码目标 https://github.com/enzoOvOyu/keel。
- 私钥、访问令牌、密码和 .env 不能提交，也不要在日志中显示。SSH 私钥路径属于每台电脑的本地配置。
- 首次接通远端先检查已有代码、进程、容器、磁盘和监听端口，不覆盖现有项目或自动卸载软件。
- API 当前仅有公开只读基础信息。增加任何项目、流水线或部署接口前实现认证授权。
- 不把 Docker socket 挂载进公共 API 容器；后续执行器需要独立权限边界。
- 变更后运行 npm run check 和 npm test；涉及容器时验证 compose config 与健康启动。
- 完成开发后更新 docs/HANDOFF.md，记录已验证结果、阻塞和下一步，便于换电脑继续。
- 不把本地完成、远端完成、GitHub 推送成功混为一谈。
