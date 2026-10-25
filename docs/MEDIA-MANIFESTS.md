# 平台签名介质清单

WP-1.10 / FR-1.8 的当前软件交付：导出已批准且真实文件校验通过的根介质和完整固定依赖闭包，使用平台 Ed25519 密钥签名，并通过独立可信公钥离线验签、验文件。它证明导出时的平台登记与审批事实；不是厂商来源签名，也不授权绕过平台当前权限、退役或执行准入。完整离线 apt/yum 仓库元数据、镜像源同步、目标缓存与硬件安装不在本次交付内，WP-1.10/P1 仍为 partial。

## 密钥与部署

使用已构建的 `keel-admin`，以下两个命令不需要数据库或对象存储连接。先在仓库外创建操作员私有目录（700），运行：

```sh
keel-admin media-signing-keygen \
  --private-key-file /private/operator/manifest-private.pem \
  --public-key-file /private/operator/manifest-public.pem
```

命令通过系统随机源生成 Ed25519 密钥，私钥采用 PKCS8 PEM，公钥采用 PKIX PEM；文件以600创建，已有路径不覆盖。输出仅包含成功状态和公钥指纹，不输出私钥。公钥指纹是原始32字节公钥的 SHA256 小写十六进制值，不能以清单自带指纹替代可信公钥分发。

API 可选环境变量 `KEEL_MEDIA_SIGNING_KEY_FILE` 指向容器内私钥。未配置时平台正常启动，签名导出返回503 `MEDIA_SIGNING_UNAVAILABLE`；已配置但文件不可读、不符合权限或格式要求时启动失败。私钥必须是 API 当前用户拥有、组和其他用户均不可读写的普通文件；拒绝最终路径符号链接和非普通文件。不要将私钥放进环境变量、Git、镜像或日志。

Compose 通过 `KEEL_MEDIA_SIGNING_DIRECTORY` 将仓库外目录只读挂载到 `/run/keel/media-signing`。默认挂载 `infra/media-signing-disabled` 且不指定私钥，签名保持关闭。启用时将私钥安全安装到宿主私有目录，以容器实际 API 用户（当前镜像 UID 65532）为属主，目录700、文件400；设置：

```dotenv
KEEL_MEDIA_SIGNING_DIRECTORY=/private/api-owned/media-signing
KEEL_MEDIA_SIGNING_KEY_FILE=/run/keel/media-signing/manifest-private.pem
```

宿主目录必须事先存在，Compose 禁止自动创建；公钥另存操作员受控目录并通过独立可信渠道分发。改动配置后验证 Compose 并按发布流程重建 API；不要将公钥仅与待校验清单一起接收后直接信任。轮换时安装新的私钥并重启 API，保留旧可信公钥校验历史收据。清单只包含指纹，不提供自行建立信任的内嵌公钥；无在线撤销查询，离线验签不能判断当前批准或退役状态。

## 导出与权限

项目 `GET /api/v1/projects/{projectId}/media/{mediaId}/manifest` 对项目成员（含 Viewer）开放，可读取本项目及继承的全局介质；平台 `GET /api/v1/admin/media/{mediaId}/manifest` 仅平台管理员可用且仅允许全局介质。跨项目访问拒绝。共享介质页的“导出签名清单”先展示目标、固定依赖和信任边界，再校验并下载 JSON。无可用签名密钥时显示错误并保留对话框。

导出要求根介质 approved 且有固定对象版本，所有传递依赖的批准、作用域、架构、ID/SHA256 和真实固定版本字节均通过校验；任何一个失败均拒绝整个导出。不自动升级到同名新版本。读事务持有根和依赖的生命周期锁直至提交，成功写 `media.manifest.export` 审计（指纹、根摘要、节点数）；失败不写成功审计。响应 `private, no-store` / `nosniff`，不包含桶凭据、内部对象 key 或私钥路径。

## 离线验签与验文件

```sh
keel-admin media-manifest-verify \
  --manifest-file ./keel-media-ROOT_ID.manifest.json \
  --public-key-file /trusted/manifest-public.pem

keel-admin media-manifest-verify \
  --manifest-file ./keel-media-ROOT_ID.manifest.json \
  --public-key-file /trusted/manifest-public.pem \
  --artifact-directory ./artifacts
```

第一条仅校验可信公钥、签名和闭包元数据，输出 `artifact_bytes_verified: false`，不能据此声称文件验证通过。第二条要求目录中完整提供每个节点的 `<media_id>.bin`，逐文件验证长度和 SHA256，成功才输出 `artifact_bytes_verified: true`。文件名称由受验证的32位小写十六进制 ID 构成，不按 URI 执行命令或打开路径；拒绝最终符号链接、特殊文件、缺失/错字节，命令出错返回非零。此命令不下载文件、不连接数据库、不改变平台状态。先通过受控平台下载所有固定版本并按 ID 命名；文件分发和清单信任分别处理。

历史签名在依赖退役后仍可离线验证，含义是导出时事实。平台新的导出、下载和执行继续检查当前批准状态，不能持旧清单重启被退役的任务。厂商签名与来源可信度仍须单独验证。

## 格式与限制

Schema 为 `keel.media-manifest/v1`，外层 `algorithm=ed25519`、`payload`、`signature`。签名原文为 UTF-8 `Keel platform media manifest v1` 加一个零字节，再拼接 Go `encoding/json.Marshal(Payload)` 的紧凑编码；字段顺序由 `internal/mediamanifest` 的类型定义确定，字符串使用 Go 默认 HTML 转义。兼容元数据作为签名载荷的一部分保留，不使用通用 JSON canonicalization 标准；第三方实现须遵循此准确编码，推荐使用配套 CLI。JSON 缩进可变化，但不得重排兼容元数据对象键或改写其数值。外层签名为标准 Base64，指纹位于载荷中并与独立可信公钥比较。

节点按 media_id 排序；每节点依赖按 media_id 排序；记录 URI、类型/名称/版本、作用域、架构、SHA256、长度、固定对象版本、审批人/时间、直接依赖与兼容元数据。根加最多128个传递节点、每节点最多32直接依赖、最长16条边；要求完整可达无环闭包，拒绝重复/未知 JSON 字段及超64层 JSON 嵌套；JSON 总长上限16MiB，单制品长度上限1GiB。验签不执行兼容矩阵政策或声明目标硬件满足要求。

## 验证

`GOFLAGS=-overlay=/tmp/keel-delivery-check/overlay.json GOMEMLIMIT=768MiB python3 scripts/check-media-artifacts-integration.py --manifest-only` 使用明确标记 disposable 的 PostgreSQL、独立 MinIO、真实会话/API和浏览器；overlay 仅为当前用户原有026草稿隔离。完整集成模式也包含签名检查。测试生成的私钥在 finally 清理，凭据不进入证据或 Git；保留公钥、签名清单及测试结果以便核验。Go 对象传输 mock、真实 MinIO 与浏览器结果分别留证，均不等同硬件或规模性能验收。
