# P1 装机准备与待接链路

所属 WP-1.7 / FR-2.2~2.6。当前实现只提供已审批 OS 组合核对和 BOM 驱动的装机配置预览；没有启动 PXE、改变资产状态或建立 SSH 互信。完整装机依次消费这些准备产物，再接共享 Execution/资源锁、ProxyDHCP/iPXE、可信分发、一次性回调和真实 SSH 首连证明。不能把预览或 DHCP 指纹当成装机成功。

## 输入与配置预览契约

- OS 组合复用 [MEDIA-ARTIFACTS.md](MEDIA-ARTIFACTS.md) 的不可变 `deps.os_boot`，不建第二套镜像登记。`boot_only=true` 筛选仅列有组合元数据的具体架构 OS 根；读取预览时检查当前审批、固定对象字节与全部依赖，筛选结果本身不是就绪证明。
- 目标来自现有统一 BOM 的 `ProjectAsset`、稳定机器/物理口与已发布型号版本。具体架构取绑定版本 `hw_spec.cpu_arch`，禁止默认为 amd64 或跟随 latest。hostname 为 1~63 位小写 DNS label，SN 和选定引导口 MAC 取数据库事实，客户端不能覆盖。
- 操作者为当前项目 Admin/DL/SRE（或平台管理员），项目关闭则拒绝。资产版本不符返回409。引导口必须属于当前机器且为六字节单播 MAC；不根据第一张网卡猜测。公钥必须是本项目未撤销、至少再有效一小时的受控 Ed25519 版本；不读取私钥或解密主密钥。
- GET `/api/v1/projects/{id}/assets/{assetId}/provision/target` 返回绑定型号、版本/摘要、hostname、架构与物理口候选；POST 同前缀 `/preview` 消费精确 `asset_version/boot_port_id/media_id/domain_key_id`，需要同源/CSRF。全部事实在同一业务事务核验，返回 `keel.provision-preview/v1` 的冻结公共元数据、四个介质角色、user-data/meta-data 和配置摘要。
- 摘要为 Go 紧凑 JSON 中 `snapshot_sha256` 置空时的 SHA256，覆盖渲染后的全部公共配置及精确版本。相同事实产生相同预览。实际装机创建必须再次原子核验，并使用新的持久请求 ID 作为 instance-id；禁止把确定性的预览 instance-id 复用为重装授权。

资产页提供只读“装机配置预览”，网口、OS 和域公钥均为服务端候选下拉；镜像候选分页和公钥候选分页保持项目隔离，切换选择清除旧结果。当前页面没有装机启动动作。

## cloud-init 模板工程决策

v1 的 config 安装模板为最多64 KiB的单个 YAML mapping，可带 `#cloud-config`。无字符串替换、Go 模板或顶层任意 shell 插值；拒绝多文档、重复/非字符串键、YAML aliases/anchors/merge、自定义 scalar tag和过深/过多节点。仅接收 locale/timezone/keyboard/packages/package_update/package_upgrade/apt/yum_repos/ca_certs/ntp/growpart/resize_rootfs/disk_setup/fs_setup/mounts 这些已审批的声明式 OS 设置；身份、用户、SSH、网络、回调、脚本及文件写入由平台所有，不从模板导入。fs_setup 按实际 array-of-mapping 接收并排除 cmd/extra_opts；apt 拒绝 conf、源路径穿越和 filename 覆盖，ntp 只接收 enabled/servers/pools/ntp_client。OS 本身及包/仓库设置仍须独立审批；本限制不是完整 cloud-init 语义沙箱或厂商可信来源签名，批准软件包本身也会执行安装逻辑。

渲染固定 BOM hostname、禁密码 SSH/根账号入口、禁数据源公钥导入、重生成 Ed25519 主机密钥。仅创建 keel-ctrl，锁定密码、无 sudo 规则，域公钥附 `restrict,command="/usr/local/libexec/keel-ssh-runner"`；rootfs 必须预装受审批 helper。固定 argv 只检查 helper 和创建 keel-ctrl 拥有的0700回执目录 `/var/lib/keel-ssh/receipts`，没有用户可控命令。配置预览不是已验证的 OS 安装、账号权限或主机互信证明；具体镜像/cloud-init版本的实际启动仍须集成验收。

字段依据：[cloud-init users/groups](https://docs.cloud-init.io/en/latest/reference/yaml_examples/user_groups.html)、[模块参考](https://docs.cloud-init.io/en/latest/reference/modules.html)。输出使用结构化 YAML，平台身份字段以专用结构生成。

## 当前状态与后续顺序

代码包含模板安全、真实数据库/BOM/介质审批/公钥生命周期及无执行副作用用例；按用户要求，留待 P1 集中运行。当前仅有 Go 编译和生成后 TypeScript 静态检查，不声称新增场景通过。

下一步按可用依赖补齐：共享 Execution 下的冻结装机请求/重装权限与资源占用 → 显式受控网络范围中的 ProxyDHCP/iPXE/HTTP 引导和实际硬件预检 → 同请求/SN/MAC/项目的一次性回调（DHCP 指纹仅辅助）→ 新主机密钥绑定及 SSH 首连/修复队列 → 七阶段页面与200台并行装机验收。网络域的Kea配置不得被服务器ProxyDHCP覆盖或无边界争用。安装脚本必须来自批准介质，不以预览代替下一环。

## 集中浏览器验证准备

`python3 scripts/check-media-artifacts-integration.py --provision-only` 已接入实际隔离 PostgreSQL/MinIO/API 和真实 owner CLI 的装机准备场景：项目/全局上传固定引导角色、实际存储核对、BOM/型号/公钥预览、撤销拒绝和无装机副作用；仅使用测试生成的小字节 fixture，不是真实可启动镜像。新增浏览器场景尚未运行。配置查询失权/失败时页面清除旧预览并禁用提交，OS 核对弹窗不显示已失败请求的缓存字节证据。

## 下一个实施单元的代码落点

沿用 internal/execution/ssh.go 的授权、稳定幂等键、冻结公共身份、exec_targets 和创建 outbox 的同事务模式，不另开装机队列。平台预览目前在自己的只读事务取数，实际创建需抽出可在调用事务内复用的准入/渲染路径，不能先读取预览后在第二个事务直接信任结果。每次实际重装使用新的持久请求 ID；冻结资产/型号/物理引导口、四角色对象版本/摘要及项目域公钥，派发前复验当前授权、资产与撤销事实。

资源接入需同时修改 worker 的真实机器命名和模式映射：与 SSH/Agent 共用现有 device:machine:<machine_id>，装机取独占，不能退回 sim:<target_id> 或只锁新建 provision:<id>。unknown 保留原设备占用，回调必须匹配请求/项目/SN/MAC及当前 attempt/fence，不能凭 DHCP 指纹或客户端成功字符串释放。网络侧现有 Kea/ZTP 配置不等于服务器 ProxyDHCP 已实现，二者测试网络范围与控制权必须明确隔离。只有实际 installer/首连回执成立后才投影安装/互信状态；准备接口和软件fixture不提前推进设备状态。
