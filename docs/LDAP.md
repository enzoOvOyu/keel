# LDAP 组织账号登录

WP-1.14 / FR-10.1 的可选后端认证，复用本地账号、角色、会话、CSRF和审计。仅做受信TLS上的固定DN Bind；不搜索目录、自动建号、导入组/角色或使用本地密码兜底。与本地登录和可选OIDC并存。

## 配置与运行

默认 `KEEL_LDAP_CONFIG_FILE` 为空，登录页不显示此方式。运维配置独立私有JSON文件，由API实际UID65532所有、0600普通单链接；拒绝symlink/FIFO、非当前UID、其他用户可读、超大文件和未知字段。Compose配置 `KEEL_LDAP_DIRECTORY` 只读挂载到 `/run/keel/ldap`，设置 `KEEL_LDAP_CONFIG_FILE=/run/keel/ldap/directory.json`。文件不得进入Git、镜像、终端日志。

```json
{
  "name": "组织目录",
  "url": "ldaps://directory.example.invalid:636",
  "ca_file": "/run/keel/ldap/organization-ca.crt",
  "bindings": {
    "existing-local-username": "uid=organization-user,ou=people,dc=example,dc=invalid"
  }
}
```

示例不是可用部署。先经现有owner CLI创建本地账号、独立本地口令及项目角色，再将规范本地用户名显式绑定到完整DN。账号停用仍由Keel控制；目录账号不能借Bind创建本地用户。固定DN在配置时解析；同一规范DN不能绑定两个本地账号。提交的用户名不会拼接为DN、过滤器或搜索表达式。无需目录服务账号或把用户目录口令保存到配置。

只接受LDAPS，验证系统/显式CA和URL主机名，最低TLS1.2；不允许明文LDAP、跳过证书校验或空口令/匿名Bind。自定义CA是当前UID所有的普通单链接文件，无组/其他写权限，最多256KiB；保留系统信任根。连接和Bind总窗口最多5秒，服从请求取消；协议/网络/TLS失败不能冒充凭据正确。公开providers只返回目录名称，不返回URL、DN、CA路径、绑定或任何口令。修改配置/CA需重启API。

## 登录和撤权

登录页复用已有表单，配置后出现本地/组织账号选择，切换时清空口令；关闭或撤去方式同样清空口令。口令仅POST至同源后端，不在浏览器持久化。后端复用预会话和CSRF/Origin，LDAP仅在本地账号存在且启用、当前限流允许时Bind。成功后再次检查预会话期限，撤销旧预会话并轮换HttpOnly/Secure/SameSite=Lax Cookie，保留本地用户当前security_version/角色。

口令错误、未绑定或不存在账号均为统一401；空输入400。目录不可用503，绝不改用本地口令，且不把基础设施故障累计成账号口令锁定。目录失败与本地失败共用账号/IP锁定，切换方式不能绕过；LDAP HTTP入口另有每来源IP请求上限，限制故障期间调用量。成功/失败/后端故障均有脱敏审计，不记口令、DN或token。既有本地账号停用、安全版本变更及退出使会话失效。目录侧停用拒绝后续Bind，但不会自动通知Keel撤销已建立会话；即时撤权需现有Keel停用/会话治理，不能冒称支持目录事件同步。

## 验证

构建前端后运行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB python3 scripts/check-ldap-integration.py`，只创建/清理带disposable标记测试容器中的随机 `keel_test_ldap_*` 库。回环TLS fixture实际校验证书、LDAPv3 Bind DN与口令；覆盖错误CA/主机名、明文/空口令/未绑定/协议故障/取消，PG+HTTP验证CSRF、Cookie轮换、无本地兜底、共享锁定、停用及Bind期间预会话到期。Chromium验证中英390宽、方式选择/口令清空、实际组织和本地登录/退出、无跨源口令请求或持久化。测试不等于客户OpenLDAP/AD实际接入验收。

依赖协议接口见 [go-ldap官方文档](https://pkg.go.dev/github.com/go-ldap/ldap/v3)。外部目录/IdP接入、完整S-6和P1出口仍独立验收；具体提交/CI/3000版本以HANDOFF为准。
