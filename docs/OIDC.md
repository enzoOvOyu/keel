# OIDC 组织登录

WP-1.14 / FR-10.1 的可选组织登录；本地账号、初始管理员CLI和现有会话仍是前置。没有公开注册、默认口令、按邮箱合并或来自IdP的管理员/项目角色授权。LDAP由独立可选适配器提供，见 [LDAP](LDAP.md)，须分别配置和验收。

## 配置

默认 `KEEL_OIDC_PROVIDERS_FILE` 为空。启用时运维在独立私有目录准备JSON数组，文件由实际API运行UID65532拥有，权限0600、普通单链接；拒绝符号链接/FIFO/过大文件及未知字段。Compose配置 `KEEL_OIDC_DIRECTORY` 挂载到 `/run/keel/oidc`，只读；`KEEL_OIDC_PROVIDERS_FILE=/run/keel/oidc/providers.json`。不要把此文件加入Git、镜像或粘贴到终端日志。

```json
[
  {
    "id": "organization",
    "name": "组织统一身份",
    "issuer": "https://identity.example.invalid/realm/organization",
    "client_id": "keel",
    "client_secret": "REPLACE_IN_PRIVATE_FILE",
    "redirect_uri": "https://keel.example.invalid/api/v1/auth/oidc/organization/callback",
    "bindings": { "EXACT_STABLE_SUBJECT": "existing-local-username" }
  }
]
```

示例值不可作为真实配置。先通过既有owner CLI建立本地账号/项目成员，再显式绑定IdP的稳定subject与本地账号名。无绑定、账号停用、签名/issuer/audience/nonce/azp/到期不符均不能登录；IdP的email/email_verified或角色声明不会创建账号或提权。修改配置需重启API；本地停用、密码/权限安全版本变更与退出按现有会话机制立即生效。

回调必须是已配置PublicOrigin加固定 `/api/v1/auth/oidc/{id}/callback`，并在IdP精确注册相同值。issuer、发现的授权/token/JWKS地址均要求HTTPS；只有显式development且回环HTTP Cookie配置允许回环HTTP测试。TLS使用系统信任根，不提供跳过证书校验开关。发现失败、非法配置或读取失败阻止API启动，不回退为成功或伪造provider就绪。网络请求有超时、响应大小上限并拒绝跟随重定向。

## 协议与会话边界

`GET /api/v1/auth/providers` 只返回配置的provider公开id/name，不返回内部issuer、客户端秘密、绑定和目录。登录页本地入口之外按配置显示组织名称，关闭的方式没有可用按钮。

同源GET start使用5分钟随机state/nonce及S256 PKCE，绑定未认证浏览器的服务端预会话；拒绝跨站start和已登录账号切换。回调先校验host、单值state/code、同浏览器与一次性state，再后端交换授权码和验签。SDK校验issuer/audience/到期/签名，应用校验nonce和azp；access-token hash存在时也校验。成功后在同一DB事务锁定预会话和当前启用用户、撤销旧会话、按当前security_version建立新不透明Cookie和脱敏审计。前端不处理code/token，后端仅重定向到 `/auth/callback?status=...`；回调页再读取当前会话并进入允许且有权限的站内页面。

Cookie复用HttpOnly、Secure、SameSite=Lax与已有idle/absolute期限、CSRF、即时撤销规则。next经过后端已实现路由白名单及前端当前权限检查，编码、协议相对URL、反斜线或外部地址均落到projects。失败统一结果、不暴露具体账号/IdP错误；前置请求受真实来源IP限流，state最多4096个且过期清理。授权码、PKCE verifier、nonce及token仅短时驻留服务进程内存，不写数据库/日志/审计/浏览器存储。

**当前部署边界**：待完成授权状态保存在单API进程内；重启后必须重新发起登录，不恢复旧回调。多副本需把同一授权往返路由到原进程；未实现跨副本共享状态，不能假称可任意负载均衡。现有已登录会话仍在PostgreSQL，可按原会话规则使用。改绑定/关闭provider不主动撤销既有会话；停用本地用户或现有会话撤销流程用于即时撤权。

## 实际验证

构建当前前端后运行 `GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB python3 scripts/check-oidc-integration.py`。只在标记disposable的测试PG容器创建并清理随机 `keel_test_oidc_*` 库。真实回环HTTP身份fixture签发RSA JWT并实际验证PKCE，覆盖合法往返、错误签名/issuer/audience/nonce/azp/过期/未绑定subject、外浏览器/state重放、跨站start、重复参数、会话轮换、本地角色与退出、账号停用。Chromium验证真实provider按钮/跳转/回调、中文与英文390宽、Cookie、无token持久化及原本地登录。fixture不是外部组织IdP验收。

协议依据与依赖文档：[coreos go-oidc](https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc)、[Go oauth2 PKCE](https://pkg.go.dev/golang.org/x/oauth2)。具体运行、提交/CI与3000状态以HANDOFF为准，F-10.5/S-6/U-12及WP-1.14仍保留外部IdP和其他完整安全验收，LDAP另按其文档验证。
