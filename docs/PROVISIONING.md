# P1 装机准备与待接链路

所属 WP-1.7 / FR-2.2~2.6。当前源码提供已审批 OS 组合核对、BOM 配置预览、独立审批的持久请求和共享 Execution 下的装机准备校验；没有启动 PXE、改变资产状态或建立 SSH 互信。完整装机依次消费这些准备产物，再接 ProxyDHCP/iPXE、可信分发、一次性回调和真实 SSH 首连证明。不能把预览或 DHCP 指纹当成装机成功。

## 输入与配置预览契约

- OS 组合复用 [MEDIA-ARTIFACTS.md](MEDIA-ARTIFACTS.md) 的不可变 `deps.os_boot`，不建第二套镜像登记。`boot_only=true` 筛选仅列有组合元数据的具体架构 OS 根；读取预览时检查当前审批、固定对象字节与全部依赖，筛选结果本身不是就绪证明。
- 目标来自现有统一 BOM 的 `ProjectAsset`、稳定机器/物理口与已发布型号版本。具体架构取绑定版本 `hw_spec.cpu_arch`，禁止默认为 amd64 或跟随 latest。hostname 为 1~63 位小写 DNS label，SN 和选定引导口 MAC 取数据库事实，客户端不能覆盖。
- 操作者为当前项目 Admin/DE/DL/SRE（或平台管理员），项目关闭则拒绝。资产版本不符返回409。引导口必须属于当前机器且为六字节单播 MAC；不根据第一张网卡猜测。公钥必须是本项目未撤销、至少再有效一小时的受控 Ed25519 版本；不读取私钥或解密主密钥。
- GET `/api/v1/projects/{id}/assets/{assetId}/provision/target` 返回绑定型号、版本/摘要、hostname、架构与物理口候选；POST 同前缀 `/preview` 消费精确 `asset_version/boot_port_id/media_id/domain_key_id`，需要同源/CSRF。全部事实在同一业务事务核验，返回 `keel.provision-preview/v1` 的冻结公共元数据、四个介质角色、user-data/meta-data 和配置摘要。
- 摘要为 Go 紧凑 JSON 中 `snapshot_sha256` 置空时的 SHA256，覆盖渲染后的全部公共配置及精确版本。相同事实产生相同预览。实际装机创建必须再次原子核验，并使用新的持久请求 ID 作为 instance-id；禁止把确定性的预览 instance-id 复用为重装授权。

资产页提供“装机配置预览”及持久准备申请，网口、OS 和域公钥均为服务端候选下拉；镜像候选分页和公钥候选分页保持项目隔离，切换选择清除旧结果。准备申请入口不会启动实际安装。

## cloud-init 模板工程决策

v1 的 config 安装模板为最多64 KiB的单个 YAML mapping，可带 `#cloud-config`。无字符串替换、Go 模板或顶层任意 shell 插值；拒绝多文档、重复/非字符串键、YAML aliases/anchors/merge、自定义 scalar tag和过深/过多节点。仅接收 locale/timezone/keyboard/packages/package_update/package_upgrade/apt/yum_repos/ca_certs/ntp 这些已审批的声明式 OS 设置；身份、用户、SSH、网络、回调、脚本及文件写入由平台所有，不从模板导入。disk_setup/fs_setup/mounts/growpart/resize_rootfs均由明确的安装存储契约管理，拒绝通过首启模板绕过目标盘选择；apt 拒绝 conf、源路径穿越和 filename 覆盖，ntp 只接收 enabled/servers/pools/ntp_client。OS 本身及包/仓库设置仍须独立审批；本限制不是完整 cloud-init 语义沙箱或厂商可信来源签名，批准软件包本身也会执行安装逻辑。

渲染固定 BOM hostname、禁密码 SSH/根账号入口、禁数据源公钥导入、重生成 Ed25519 主机密钥。仅创建 keel-ctrl，锁定密码、无 sudo 规则，域公钥附 `restrict,command="/usr/local/libexec/keel-ssh-runner"`；rootfs 必须预装受审批 helper。固定 argv 只检查 helper 和创建 keel-ctrl 拥有的0700回执目录 `/var/lib/keel-ssh/receipts`，没有用户可控命令。配置预览不是已验证的 OS 安装、账号权限或主机互信证明；具体镜像/cloud-init版本的实际启动仍须集成验收。

字段依据：[cloud-init users/groups](https://docs.cloud-init.io/en/latest/reference/yaml_examples/user_groups.html)、[模块参考](https://docs.cloud-init.io/en/latest/reference/modules.html)。输出使用结构化 YAML，平台身份字段以专用结构生成。

## 当前状态与后续顺序

代码包含模板安全、真实数据库/BOM/介质审批/公钥生命周期及无执行副作用用例；按用户要求，留待 P1 集中运行。当前仅有 Go 编译和生成后 TypeScript 静态检查，不声称新增场景通过。

下一步按可用依赖补齐： 显式受控网络范围中的 ProxyDHCP/iPXE/HTTP 引导和实际硬件预检 → 同请求/SN/MAC/项目的一次性回调（DHCP 指纹仅辅助）→ 新主机密钥绑定及 SSH 首连/修复队列 → 七阶段页面与200台并行装机验收。网络域的Kea配置不得被服务器ProxyDHCP覆盖或无边界争用。安装脚本必须来自批准介质，不以预览代替下一环。

## 集中浏览器验证准备

`python3 scripts/check-media-artifacts-integration.py --provision-only` 已接入实际隔离 PostgreSQL/MinIO/API 和真实 owner CLI 的装机准备场景：项目/全局上传固定引导角色、实际存储核对、BOM/型号/公钥预览、撤销拒绝和无装机副作用；仅使用测试生成的小字节 fixture，不是真实可启动镜像。新增浏览器场景尚未运行。配置查询失权/失败时页面清除旧预览并禁用提交，OS 核对弹窗不显示已失败请求的缓存字节证据。

## 持久装机准备请求与共享执行（新增源码，待集中验证）

- 资产预览可提交 `/projects/{id}/assets/{assetId}/provision/requests`，必须提供理由和稳定 Idempotency-Key；创建使用新的持久请求 ID 渲染 instance-id，冻结精确资产/型号/网口、四角色对象版本/摘要、公钥及配置。已有已装机/已互信资产须显式 `reinstall=true` 并确认完整 BOM SN。每台物理机器只能有一条活动请求，防止跨项目重复占用；非运行请求30分钟过期，运行/unknown 请求不能自动过期或解锁。
- 项目 Admin/DE/DL/SRE（含平台管理员）可读/申请/取消，另一位当前 Admin/DL 审批，创建者不能自批；公钥候选 `/projects/{id}/provision/public-keys` 仅公开有效公共身份，不扩大 DE 的 SSH 凭据/互信审批权限。项目关闭仍可读取历史。
- `/projects/{id}/provision/requests/{requestId}` 提供详情，集合 GET 支持分页；POST `/approve`、`/cancel` 消费精确 `expected_version` 与理由；POST `/prepare` 一次消费当前审批版本，返回原生 `mode=provision` Execution。全部写请求使用同源、CSRF、稳定幂等键。页面 `/p/<projectId>/deploy/provision` 复用共享外壳和 shadcn/ui，提供审批、准备/取消及执行记录链接。
- API 与 worker 必须显式 `KEEL_PROVISION_PREPARE_ENABLED=true`，默认关闭。worker 需要与 API 相同的受控对象存储配置，读取已固定对象；不需要私钥、主密钥、DHCP listener 或设备连接。Compose 仅提供关闭默认的配置接线，本次不启用生产实例。
- `provision.prepare` 复用已有 outbox、TaskAttempt、心跳、fence、回执与对账。真实机器资源必须是 `device:machine:<machine_id>`，准备取独占，与 SSH/Agent 共用冲突域。派发重查当前操作者与审批人、资产/物理口、介质审批和公钥撤销；执行重新校验完整对象字节并重渲染比对摘要。损坏介质沿用隔离审计，不把旧预览当当前证明。
- 请求状态 `requested → approved → preparing → prepared/failed/canceled`；非运行请求可取消/过期。准备结果投影与完成回执在同一事务。未知保留机器资源，须有匹配 request/hash/attempt/fence 的持久回执才能显式对账；从未开始的排队任务取消可直接撤销准备请求，不伪造停止回执。
- `prepared` 仅证明控制面输入与字节校验完成，**不证明硬件预检、PXE、安装或 SSH 信任**。回执固定 `installation_performed=false`，不会更新资产状态或机器互信。准备结束会释放设备锁，后续实际安装必须重新取得独占锁、复验授权与快照，不能把已释放的准备锁当作安装占用。

053 迁移新增不可变准备请求、受控状态迁移及 provision 执行模式；未对运行库应用。新增平台/执行器真实 PostgreSQL 用例和既有 `--provision-only` 浏览器链路留到 P1 集中执行；Go 仅编译、TS 和语法检查不是数据库/网络或硬件验收。

## 下一个实施单元

在隔离的受控网段提供 ProxyDHCP/iPXE/HTTP 引导与实际硬件预检；接同请求/项目/SN/MAC及当前 attempt/fence 的一次性回调，DHCP 指纹仅辅助；然后绑定新主机密钥并通过真实 SSH 首连/修复队列形成互信证明。网络侧既有 Kea/ZTP 不等于服务器 ProxyDHCP 已实现，两者网段和控制权必须明确隔离。安装脚本来自批准介质，任何模拟/准备证据均不得直接投影安装成功。

## 引导传输适配层（新增源码，尚未接装机）

`internal/provisionboot` 是受控网络边界，**没有接入 API/worker 启动路径，也没有可授权真实装机的生产 Authority**。其默认配置关闭；仅写出协议适配与待运行的软件用例，不表示 PXE、可启动镜像或安装成功。

- Linux ProxyDHCP 使用内核 SO_BINDTODEVICE 绑定明确的已启用接口及本机地址/准确子网；接收再核验接口索引与目的地址。仅处理直连 Ethernet Discover 与发给本代理4011的PXE请求；拒绝relay、重载、重复/截断选项、普通DHCP Request、跨网段及未授权机器。yiaddr不分配地址，也不提供租期、路由、DNS或掩码。禁止与配置中Kea范围重叠；后续生产Authority仍须从当前管理网计划核验清单完整性，不能只信任文件参数。
- 区分HTTPClient(arch16/19)、已有iPXE(option77)及Legacy/UEFI PXE。已加载iPXE返回脚本URL，避免循环发回固件；UEFI HTTP返回固件HTTP URL。EBC架构9不冒充x64；按当前IANA登记使用7=x64 UEFI、11=ARM64 UEFI。boot filename过长拒绝，不截断为另一资源。每秒/每MAC限制授权查询，状态表有界。
- Grant必须绑定项目/网络scope、请求、机器、MAC、架构、attempt/command/fence、摘要与到期时间，并由调用方实时核验独占机器租约和审批。**当前prepared请求不足以签发引导Grant**，不能把MAC或DHCP指纹作为回调/注册/互信授权。
- HTTP只接受精确origin、直接管理网peer与固定资源名，生产需TLS，不信任转发头、不跟随任意URI。完整验证固定对象版本的SHA256/大小后才处理Range/HEAD/条件请求；容量有界，流式传输检查到期与持续授权，避免撤销后继续完整分发。实际审批/完整闭包/租约的Authority接线尚未完成。
- Legacy TFTP只传至多16MiB的已批准iPXE启动固件，不提供OS、cloud-init或任意文件路径。采用RFC1350 512字节octet块、精确ACK/超时重发与终止空块，重复ACK不触发DATA重发以避免重传放大；RFC2347可选参数不协商时按标准退回DATA/512。Linux daemon 已提供独立transfer socket、4次超时上限/自适应退避/2分钟总时限、同RRQ去重及最多64路并发；默认关闭且尚未接装机Authority或启动路径，没有运行中的生产TFTP服务。

后续还须完成：真实装机Execution/持久网络Grant、当前网络计划与批准启动固件/安装recipe、受批准initrd中的硬件预检、可信文件Authority和一次性回调、启动路径接线、软件协议/实际可启动镜像验收；再按独立PoC窗口验证真实固件。此次没有新生产网络监听、Kea变更或数据库迁移。

协议依据：[RFC1350](https://www.rfc-editor.org/rfc/rfc1350.html)、[RFC2347](https://www.rfc-editor.org/rfc/rfc2347.html)、[RFC1123](https://www.rfc-editor.org/rfc/rfc1123.html)、[RFC2131](https://www.rfc-editor.org/rfc/rfc2131.html)、[RFC4578](https://www.rfc-editor.org/rfc/rfc4578.html)、[IANA处理器架构登记](https://www.iana.org/assignments/dhcpv6-parameters/dhcpv6-parameters.xhtml#processor-architecture)、[iPXE UEFI HTTP](https://ipxe.org/appnote/uefihttp)、[iPXE user-class](https://ipxe.org/appnote/userclass)、[iPXE PXE discovery定义](https://dox.ipxe.org/group__dhcpopts.html)。仅据官方协议实现传输，没有把这些资料视作Keel已验证证据。


## 安装环境只读硬件预检（源码增量，未接真实引导）

`internal/provisionpreflight` 与 `cmd/keel-preflight` 实现 FR-2.2 的安装前判定。iPXE 负责进入批准的、含预检 helper/驱动/smartctl 的 Linux 安装环境，不能把 iPXE 本身当作可执行 SMART 的 Linux。`make build-preflight` 产出 linux/amd64 与 linux/arm64 静态二进制；须通过受审核介质打包进 initrd/安装环境并固定摘要。此目标不修改现有 API 镜像，也不自动运行 helper；现有任意 rootfs 不能据此声称已具备可启动安装环境。

控制面将项目/申请/机器、实际安装 attempt/command/fence、快照摘要、期限及固定型号期望形成 `keel.provision-preflight/v1` Spec；helper 仅从本地 `--spec-file` 读取有界、严格无重复字段的 JSON，不接受自定义 sysfs 根目录或用户命令。该 Spec 尚待真实装机执行器和受限文件授权链签发；准备阶段不是签发权限。helper 输出原始事实与 PASS/FAIL/BLOCKED 报告，退出0仅代表本地预检 PASS，退出1为 FAIL/BLOCKED、退出2为非法/过期规格或输出失败。没有 HTTP 回调、凭据、安装命令、挂载、擦盘或资产状态更新。

- 硬件事实读取 DMI/ARM device-tree SN、运行架构、PCI 类别/精确 vendor:device、物理槽位、VF 标记和物理 netdev/MAC。按内核 `/sys/bus/pci/slots/*/address` 的明确槽位映射聚合多端口/多功能 NIC，不把端口或 VF 算成物理卡；未知槽位/驱动缺失阻断。BOM 引导 MAC 必须在真实非 VF 的 network-class PCI 端口上唯一出现；不能用 GPU 的 BDF、bond/VLAN/loopback 证明网卡。
- `ExpectationFromModel` 只消费申请固定的已发布型号，调用方仍须复核版本与内容摘要。`bom_expect.nic_count`、`hw_spec.gpu_count` 缺失保持 unknown，不从本次观测反推期望。新增可选 `hw_spec.gpu_pci_ids` / `non_gpu_pci_ids` 为非空、互斥、无重复的小写 `vendor:device` 列表；前者识别 GPU，后者由型号审核显式排除 BMC 显示等非 GPU 设备。未知显示/加速设备阻断，即使期望 GPU=0；不按厂商名字静默猜测，VF 不算物理 GPU。PCI 类别/ID 只证明在位，不证明 GPU 驱动、HBM 或 G2/G3 健康。
- 磁盘只运行固定绝对路径 smartctl 的 `--json=c --health /dev/<受限设备名>`，不启用 SMART、不启动自测。进程/JSON exit_status 必须一致；读取/解析/不支持（位0～2）为 unknown，当前失败（位3～4）或 NVMe critical warning 非零为 FAIL。仅历史位不是当前故障结论；原 exit status 留在事实中，此项不替代完整磁盘验收。语义参照 [smartmontools 官方状态位定义](https://github.com/smartmontools/smartmontools/blob/main/src/smartctl.h)。至少1块磁盘是预检工程底线，不是目标盘选择授权；未支持的非虚拟磁盘类型或 RAID 后端须补可信适配，否则 BLOCKED。
- 文件/目录/JSON有界，采集预算90秒、每盘SMART子进程8秒、报告输入2MiB上限；陈旧/不完整来源不得算零硬件。控制面 `EvaluateReport` 比对整个冻结 Spec，并根据事实重算结论/逐项检查，拒绝改期望、改 fence、伪造 PASS/WAIVED。这个函数不提供认证：真正消费前必须核验一次性回调凭据、当前审批/独占 lease、同一安装 attempt/command/fence/申请/项目；报告不能独立授权擦盘或安装。

本批仅编译/静态检查，新增采集/判定/SMART/规格错绑用例待 P1 集中执行；没有在开发宿主机执行任何采集/SMART/装机操作。后续仍缺批准的引导固件与安装 recipe、把 helper 打入实际可启动介质、装机执行器及网络 Authority、一次性回调、SSH 首连和真实/规模验收。WP-1.7/P1 保持 partial。


## 批准启动配方与确定性引导文件（源码增量）

`os_boot.recipe` 为可选 `keel.boot-recipe/v1`；已有四角色介质仍可用于准备预览，但 `mediaboot.Plan.ValidateInstallable` / `RenderBootBundle` 会拒绝没有配方的实际引导输入。当前唯一协议是显式的 **自定义 `keel-initrd/v1` + squashfs 安装环境**，不是 Ubuntu ISO/Subiquity 等通用镜像兼容声明。后续需按此协议构建并验证安装 initrd，不能因为有 cloud-init 配置就假定会安装 OS。

配方固定同架构 UEFI iPXE 固件、预检 helper、SSH helper（amd64可另加Legacy固件）的直接介质依赖与helper SHA256；与kernel/initrd/template/rootfs互不混用，不允许传递依赖或latest替代。登记、生命周期、下载及签名清单共用角色/架构/摘要约束，旧清单省略recipe的序列化保持兼容。解析结果无配方4角色，有配方7～8角色；UI用固定依赖版本下拉，并从已选helper版本自动保存摘要。元数据声明不证明initrd已包含驱动/smartctl或遵守写盘顺序，必须另有实际镜像验收。

`RenderBootBundle` 绑定同scope/申请/项目/机器/attempt/command/fence/期限、冻结快照、唯一BOM引导MAC、完整批准介质与NoCloud instance-id；生成公开的 `boot.ipxe`、`manifest.json`、`preflight.json`，清单固定所有原始字节摘要及helper版本。iPXE脚本只在匹配BOM MAC的net0～net255上DHCP，固定kernel/initrd参数，失败退出，无任意脚本/内核参数、安装口令、回调凭据或成功投影。HTTPS origin须适合脚本字面值，不把元数据插入脚本表达式；内核的 `keel.manifest_sha256` 固定控制面清单。启动下载依据 [iPXE kernel](https://ipxe.org/cmd/kernel)、[initrd/imgfetch](https://ipxe.org/cmd/imgfetch) 与 [ifconf](https://ipxe.org/cmd/ifconf) 官方协议，使用 `initrd=initrd.magic`。

批准initrd未来应实现：验证清单及固定对象字节/架构→下载或验证固定helper→运行预检→提交同attempt/fence的一次性可信回调→仅当前安装许可允许写盘→安装cloud-init/受限SSH helper→回传新主机密钥→由控制面实际SSH首连确认。**当前尚未实现这条initrd执行协议、一次性回调许可或生产Authority；脚本只描述受控入口，不能启动任何任意rootfs后称为装机完成。** 受控传输与实际装机Execution接线须等这些先决条件完成，无真实监听/安装已启用。


## helper 安装包的无循环生产路径（源码增量）

`keel-admin provision-overlay-create` / `provision-overlay-verify` 复用独立签名清单和离线介质工作流，生产/核验只读 gzip/newc helper overlay。**生产入口消费独立 helper-set config，不消费最终 OS recipe 或 initrd**，避免“先有最终镜像才能生产镜像输入”的循环：

1. `make build-installer-helpers` 编译 linux/amd64 与 linux/arm64 的预检和受限SSH helper；不运行二进制。
2. 将所需架构两个二进制登记为OS型介质并独立审批，记录精确ID/SHA256；架构不得用noarch替代。
3. 创建同架构config型独立helper-set JSON，其字段为 `schema=keel.installer-helper-set/v1`、`arch`、`preflight_media_id`、`ssh_helper_media_id`、`preflight_sha256`、`ssh_helper_sha256`；直接依赖恰为上述两份批准二进制。保存、独立审批并导出签名清单，不引用任何尚不存在的initrd/OS配方。
4. 下载原始config与两个helper字节到受控目录，各命名`<media_id>.bin`；使用独立预置的签名公钥，而不是从包中自行信任一个新公钥。
5. `keel-admin provision-overlay-create --manifest-file <helper-set-manifest.json> --public-key-file <trusted-public.pem> --artifact-directory <inputs> --output-file <new-protected-output/helpers.cpio.gz>`；输出目录须属于当前操作员且禁止组/他人写，已存在路径和符号链接均不会覆盖。
6. `keel-admin provision-overlay-verify --overlay-file <helpers.cpio.gz> --public-key-file <trusted-public.pem>`；核对报告的完整归档SHA256/大小和原批准helper身份。
7. **之后**才将overlay作为实际initrd构建输入，接入受控启动入口、驱动/smartctl和写盘前预检/一次性许可流程；验证镜像后登记新的initrd，最后创建绑定该initrd与同一helper版本的OS recipe。这些实际镜像构建/执行协议/许可接线仍未完成，不能反向拿最终配方当第3步的前置条件。

工具严格核验原始config/两份二进制的签名链与实际摘要、尺寸、64位小端目标架构、静态ELF（无解释器/动态库，入口在可执行LOAD段）；静态格式通过不证明二进制功能或真实驱动兼容。归档使用固定所有者/权限/时间、固定文件名与newc trailer，包含两个只读可执行helper、组件元数据、原签名清单与原helper-set JSON；不包含`/init`、用户账户、私钥、设备节点、链接或安装脚本。参考 [Linux initramfs buffer format](https://www.kernel.org/doc/html/latest/driver-api/early-userspace/buffer-format.html)。不把它冒充可启动镜像。

输出只在全部流式校验、gzip关闭和fsync后以不覆盖的方式发布；复制过程中再次核验输入，防止先验后改。反向校验有界读取固定文件集合，不解包到任何宿主路径，拒绝符号链接、任意路径、改属主/权限、未知条目、损坏/尾随/串接归档和自带信任根；完整元数据必须与独立信任的原签名及config匹配。历史导出审批不替代当前线上准入；工具不访问数据库/网络、不执行helper、不操作设备或改变安装状态。本轮仅编译/静态检查，新增用例待P1集中执行。


### Linux 物理槽位事实来源修正

预检使用 `/sys/bus/pci/slots/<kernel-slot-name>/address` 解析精确device地址；不读取不存在的通用`devices/<BDF>/physical_slot`属性。`domain:bus`占位地址不能证明一张物理卡；同一device映射多个槽位时保持unknown。多端口/多功能共用槽位，卡内PCIe桥的子设备沿实际sysfs父链匹配最近的显式槽位；没有内核槽位事实的onboard设备继续BLOCKED，不把BDF或一条MAC直接算一张卡。名为`0`的真实内核槽位本身合法。支持规范4～8位PCI域，设备号限00～1f、function限0～7，无法解析的实际PCI条目和未支持的非PCI物理网卡记缺失而非静默忽略。参考 [Linux PCI slot ABI实现](https://github.com/torvalds/linux/blob/master/drivers/pci/slot.c) 与 [pciutils sysfs槽位读取](https://github.com/pciutils/pciutils/blob/master/lib/sysfs.c)。仍仅源码与编译检查，未采集宿主或真机。


## P1 集中验证入口（已接线，尚未执行）

`scripts/check-media-artifacts-integration.py --provision-only`在既有disposable PostgreSQL/MinIO保护下使用随机测试库，先运行mediaboot/provisionboot/provisionpreflight/provisionimage的完整软件用例，再验证签名配方/overlay/seed与平台独立申请审批、共享Execution/unknown/设备锁及实际浏览器路径。入口为归档测试编译静态预检/SSH helper；`TestInstallerOverlayRealHelperCLI`仅在三个显式测试二进制路径齐备时运行真实admin创建→反向核验→拒绝覆盖→错误独立公钥的离线链。它不会运行硬件helper，也不会引导或安装OS；未传路径时会明确skip，不冒充CLI证明。

普通完整media入口也补齐新的基础和准备域用例，`--browser-only`保持跳过Go检查。CI源码增加实际静态helper/真实admin离线CLI步骤，原make check仍覆盖软件用例；当前没有推送/触发CI或运行这些入口。P1实现闭环后的集中测试仍须单独记录真实PG/HTTP/浏览器、归档CLI、可启动镜像/协议与真机/规模的各自证据，不能混写。


## 引导文件消费端校验（源码增量）

`provisionboot.VerifyBootFiles`供未来受控安装环境复用：调用方从独立可信启动链取得清单摘要（受控脚本/内核参数），校验原始清单、规范JSON、完整可安装介质配方、同项目角色与架构、预检规格和cloud-init原始字节摘要，并逐一比较申请/机器/attempt/command/fence/快照/MAC/架构/有效期和NoCloud instance-id。未知、重复或大小写别名字段及尾随文档拒绝。对象版本长度与登记层保持4096字节上限，空版本及字面值`null`拒绝。

该函数仅处理有界数据，不下载、不执行helper、不授权写盘；清单中的自报摘要不能充当独立信任根，受控启动链本身和实际镜像仍须验收。当前审批/网络归属/独占lease与一次性许可仍须由后续服务器Authority核验。新用例只写入，待P1集中验证。


引导HTTP/TFTP的完整摘要预读和实际发送共用持续授权guard：逐次检查context/期限，实际Authority最多每250ms复核一次；摘要后无条件再核当前授权，短对象也不能仅依赖摘要前的缓存。中途失效停止继续读取/发送，已失败传输不能恢复；文件源须遵守context并返回不可变精确版本。这是传输资源/授权边界补强，尚无生产Authority接线或功能运行证据。


### NVMe 只读健康输出对应关系

采集命令保持`smartctl --json=c --health`。NVMe的该选项输出`smart_status.nvme.value`；`nvme_smart_health_information_log.critical_warning`属于完整属性日志，不能假定每次`--health`都有。解析器以实际健康字段为主，并接受完整日志作为补充；两处同时出现必须数值一致，均限8位。原生NVMe命名、显式NVMe协议或任一NVMe块表明设备类型时，缺少告警值保持unknown；告警非零判当前故障，不被`passed=true`覆盖。ATA/SCSI既有健康判据保留。依据 [smartmontools nvmeprint.cpp](https://github.com/smartmontools/smartmontools/blob/master/smartmontools/nvmeprint.cpp) 中`print_critical_warning`及`smart_check_status`/`smart_vendor_attrib`分支；只核对官方源码，未运行smartctl或访问宿主磁盘。


CI中的既有Agent专用PostgreSQL集成步亦显式包含`TestProvisionExecutionKernelPostgres`，因为该用例复用Agent隔离fixture、普通`make check`没有该DSN时会skip；只扩展已有筛选，不额外重复构建/启动测试环境。当前仅更新CI源码，尚未运行。


准备任务的完整对象读取与配置摘要比对完成后，再检查context及当前请求期限/审批人/BOM/公钥/介质准入；读取前的验证不作为耗时读取完成时的有效性证明。已过期或失权的源不记录为prepared。该复核不赋予实际安装许可，真实安装仍须取得新的共享独占lease并重验当前事实。


## 下一开发单元与输入边界

WP-1.7仍为partial；从已完成的helper-set/overlay和文件校验继续，不重做准备申请或传输模块。下一完整单元是受控安装环境：先补明确的存储安装契约（独立批准的目标磁盘选择/布局和身份，缺失或歧义阻断；MinimumDisks仅健康库存下限，不能选盘授权），再实现实际initrd入口、驱动/smartctl输入、固定对象字节校验和写盘前预检。现有受限cloud-init模板用于首启账户/域公钥配置，不承担磁盘安装/任意脚本授权；不得假定普通rootfs或helper overlay可自行安装。此处是下一单元的输入要求，当前没有新增选盘字段、许可或写盘实现。

initrd入口先交付无写盘准备与默认拒绝的许可接口，不依赖尚不存在的在线服务才能构建，也不能在许可缺失时开放安装。此后接真实安装Execution/独占lease与当前网络所有权Authority、只经可信启动环境交付的一次性回调凭据、预检结果/当前许可、首启新host-key绑定、真实SSH首连及修复队列，最后将已有真实事件投影为七阶段页面。回调项目/申请/SN/MAC/attempt/command/fence必须一致；Grant公共ID、DHCP fingerprint、prepared和自报PASS均不能替代一次性认证及当前许可。各单元按设计可独立开发，P1实现闭环后集中执行已接线的软件/PG/HTTP/CLI/浏览器检查，真实可启动镜像与硬件验收另列证据。


## 显式单盘布局与只读存储事实（源码，尚无安装许可）

WP-1.7的首次实现支持固定`uefi-gpt-ext4/v1`布局：512字节或4Kn扇区、1MiB边界、512MiB EFI分区、余量ext4根分区（至少16GiB），末尾保留1MiB GPT空间，容量上限1PiB。BIOS安装/RAID/LVM/加密/任意脚本/自动选择第一块盘不在此实现范围；旧Legacy引导可用于独立只读准备，不把它当该布局可安装。

`provisionstorage.Intent`明确WWID、磁盘序列号、精确容量/逻辑扇区及擦除所选盘意图。`Build`消费独立冻结Spec及由`EvaluateReport`复算的当前PASS报告，必须观察UEFI、唯一所选WWID、健康且非只读/非可移除/未占用设备，输出带完整Spec、原报告摘要、设备号/当前diskseq、分区布局与全计划SHA256的规范Plan。`VerifyCurrent`要求新的可信报告保持相同身份、节点、设备号、diskseq、容量、扇区、健康和占用状态；重挂接/重编号需要新计划，不自动重定向。Plan/自报PASS/摘要都不是审批、一次性认证或当前写盘许可。

采集增加sysfs WWID/serial/size/logical_block_size/dev/diskseq/ro/removable和实际UEFI事实；`size`按512字节单位换算，与4Kn逻辑扇区分开。SMART命令增加只读`--info`取得序列号，现为`smartctl --json=c --info --health`，不启动自检/启用SMART。SMART与sysfs序列矛盾保持unknown；采集结束再读diskseq/设备号/几何，跨读取替换或变更使存储事实不可用。身份缺失不改写原健康结果，但不能生成存储计划。

整盘及分区holders、当前mountinfo和swaps表必须齐备才能声明未占用；存在任何活动swap时保守阻断全部候选。实际initrd须使用宿主挂载命名空间，容器隐藏挂载不算可信事实。缺失/畸形字段保持unknown；不打开或写入块设备。未来真正写盘前仍须持有独占块设备fd并立即核验fstat/磁盘ioctl、当前lease和一次性许可，本单元未实现该动作。

实施顺序为只读引导→可信预检/磁盘事实→明确选择/独立布局审批→同attempt/fence当前许可→受控安装；不会以“已完成安装”或“已审批最终镜像”作为生产helper/只读预检的前提。当前纯数据规划和只读采集源码完成，持久存储审批/API/页面与真正initrd/许可仍需后续接入；新增用例仅写入。事实来源参考 [Linux NVMe sysfs ABI](https://github.com/torvalds/linux/blob/master/Documentation/ABI/stable/sysfs-nvme)、[block queue ABI](https://github.com/torvalds/linux/blob/master/Documentation/ABI/stable/sysfs-block) 与 [diskseq实现](https://github.com/torvalds/linux/blob/master/block/genhd.c)。


## 受控initrd只读准备协议（源码，尚未接启动入口）

`internal/provisioninitrd`接收受信内核参数中的固定协议、manifest HTTPS地址/独立SHA256、BOOTIF、引导服务器IPv4及管理CIDR；拒绝未知/重复/大小写别名参数，要求`ip=off`。iPXE生成器同步提供这些参数，禁止内核在未经选择的其他网卡自行DHCP；后续initrd网络适配只配置唯一BOM MAC。当前网络适配与PID1入口仍在后续单元开发，没有启动任何监听/设备联网。

HTTPS文件客户端必须显式CA信任池、已分配本地地址及对应接口；TCP固定引导服务器IP并绑定接口，TLS 1.3校验原始主机名，不查询DNS、不用环境代理/重定向/cookie，不获取任意资源或rootfs。仅有界GET manifest/preflight/user-data/meta-data，先核独立manifest摘要再拉剩余文件，最后复用VerifyBootFiles比较全冻结身份和原始配置字节。开发例外仅loopback TLS用于未来软件测试，生产内核参数不能打开该例外。

准备函数先核两份固定helper，再运行受限预检并复算完整报告，验证退出码与PASS/FAIL/BLOCKED一致及报告/期限新鲜度。PASS只输出ready_for_storage_review，失败保留原因状态；installation_performed始终false。Linux进程适配仅root PID1且根文件系统为ramfs/tmpfs时可用，拒绝普通宿主或容器overlay入口；读取root所有且不可写的固定helper，复用实际SHA/静态ELF校验，复制到sealed executable memfd，规格通过独立不可执行sealed memfd交付；固定fd/argv和最小环境，无路径替换、shell或继承控制面配置。只支持具备显式memfd exec/noexec及sealing能力的批准内核，不降级为不受保护的路径执行，参考 [Linux memfd exec/noexec契约](https://www.kernel.org/doc/html/latest/userspace-api/mfd_noexec.html)。

该源码没有PID1命令、完整可启动镜像、当前网络Authority或回调交付；不宣称图片/协议或真实helper运行已验收。新增固定输入、TLS文件边界、helper错绑/退出码/失效和失败报告用例仅写入，待P1集中执行。

### 受控只读启动入口与临时 IPv4（源码，尚未启动验收）

`cmd/keel-initrd` 是独立镜像的 `/init` 入口，不放入现有 API/worker 镜像。启动必须为 root PID1、ramfs/tmpfs 根；仅挂 proc/sysfs/devtmpfs/tmpfs，devtmpfs 后连接 console。严格解析受信启动参数并从镜像内固定 `/etc/keel/boot-ca.pem` 读取有界、有效的公共 CA；不接受 DHCP/下载内容提供初始信任根。证书不得混入私钥、重复块或任意前后文本。

新 DHCP 客户端只在唯一 BOM MAC 对应且无既存 IPv4 的 Ethernet 接口启用；端口68冲突失败，不复用/停止其他进程。仅支持批准 CIDR 内直接 DHCP，拒绝 relay、重复/overload 选项、错 XID/MAC、错 server-ID/来源、零地址 Proxy offer、掩码不符及不足300秒/超过7天租约。DISCOVER/REQUEST 有界指数退避；只接广播回复，服务器必须能直接回包。租约从首个 REQUEST 起保守计时，不从 ACK 收到时延长。不会采用 DHCP 的默认路由、DNS 或启动路径；HTTPS 仍固定服务器 IPv4 和域名证书。

使用前对选定接口执行三次 ARP probe，冲突发 DECLINE 后停止；会话期间继续监听冲突，期限/取消/冲突均中止读取。固定接口设置地址与精确掩码，工作结束撤下本次地址；没有静默续租或把本次租约用于后续写盘。未来独立审批/安装必须重新取得当前网络会话和许可。当前会话只取四份固定公开文件、校验两个 helper、运行只读 preflight，再将原始报告与摘要/复算结果写入 `/run/keel` 的独占0600文件；无回调、磁盘/客户文件系统挂载或资产状态变化。PID1随后保持等待并回收孤儿，不通过信号自动重试/重启/执行 shell。

此入口依赖批准镜像实际包含静态 `/init`、预检/SSH helper、静态 smartctl 和公共 CA，及与机器兼容的内建 NIC/存储驱动、proc/sysfs/devtmpfs、ARP/IPv4、sealed executable memfd 内核能力。`make build-initrd` 仅编译双架构入口和 helper，既有 helper overlay 仍不能冒充完整镜像。下一步独立 runtime-set 输入与完整 initrd 生产，不以成品 initrd/OS recipe 为输入前提。新增 DHCP/ARP/CA/宿主拒绝用例只写入；未配置宿主网络、运行 helper/PID1 或开展硬件启动。

协议依据：[RFC 2131](https://www.rfc-editor.org/rfc/rfc2131) 的 DHCP 客户端选择/请求/拒绝及重传流程、[RFC 5227](https://www.rfc-editor.org/rfc/rfc5227) 的使用前冲突探测。当前只实现同网段、短期只读会话子集，不宣称支持 relay、持久租约续租或完整操作系统网络管理。

### 完整只读 runtime-set 与 initrd 离线生产（源码，非启动验收）

`mediaboot.RuntimeSet` / `keel.installer-runtime-set/v1` 为独立config：固定schema、arch及init/preflight/ssh_helper/smartctl/boot_ca五个`{media_id,sha256}`，使用Go紧凑规范JSON。前四项是同架构、无动态依赖的静态ELF os介质，CA是noarch config；五个ID互异。配置的签名manifest须恰含配置+五个无子依赖的直接输入，无kernel/成品initrd/OS反向依赖。先通过既有介质审批/manifest导出得到独立输入，再在受保护目录中生产新归档：

```sh
keel-admin provision-initrd-create --public-key-file /protected/media-public.pem --manifest-file /protected/runtime-manifest.json --artifact-directory /protected/runtime-inputs --output-file /protected/output/keel-initrd.cpio.gz
keel-admin provision-initrd-verify --public-key-file /protected/media-public.pem --initrd-file /protected/output/keel-initrd.cpio.gz
```

这两个子命令在DB/网络初始化前分支，仅离线读取/产出文件，不执行输入。源文件名为`<media_id>.bin`；公共签名根必须独立提供。生产前验签、核对每个输入实际摘要/大小/静态ELF/CA；复制时再次摘要校验，deterministic gzip/newc，精确目录和文件白名单，root所有、固定mode/时间，无symlink/设备节点/未知脚本。归档含`/init`、两helper、`/usr/sbin/smartctl`、`/etc/keel/boot-ca.pem`、三份公开输入/元数据。输出独占0600、fsync及不覆盖发布，失败不发表部分归档。反向校验有界展开≤固定五个输入各上限，拒绝额外路径、错误mode/owner、拼接gzip、尾随/截断/篡改；重新独立验签并比较完整五项记录。原helper-set/overlay保留其两项输入与非完整镜像边界。

产物实际摘要/大小须导入介质库并**另行审批**，然后才能成为最终OS recipe的initrd角色。`VerifyInstallerInitrdForPlan`提供实际归档+独立签名根与冻结Plan的校验，核对外层initrd摘要/大小/架构及内含preflight/SSH helper的ID/摘要/大小，防仅凭recipe字段声称兼容。它不替代当前介质撤销检查、内核/驱动启动验收或live安装锁/许可；尚未接生产装机Authority。静态ELF格式也不能证明某二进制确实是期望工具，必须独立审批真实构建来源/版本，不能拿软件格式fixture充当smartctl或已验收入口。

当前没有运行上述CLI/生成部署镜像/启动硬件。新增纯格式、实际签名/私有发布/拒绝覆盖、错误密钥/输入变化/取消及最终Plan错绑用例留待P1集中测试。实际只编译相关包/命令并做vet与静态文档/脚本检查；窗口继续接持久安装授权和存储审批，不把本阶段“完整只读归档源码”报告为可写盘安装器。

### 持久引导网络域与当前网络所有权（源码，054待集中验证）

新增`provision_boot_scopes`及项目API `/provision/boot-scopes`（列表/读取/创建、`/{scopeId}/approve|revoke`），装机页复用shadcn/ui提供操作。Admin/NDE登记固定生产接口、服务器IPv4、规范/8～/30客户端CIDR、HTTPS源、外部Kea/管理网段**显式清单**（可声明空数组）和方案来源；另一个当前Admin/NDE凭客户及网络团队引用批准。DB触发器固定身份/输入/审批记录和状态版本，不能修改已批准范围或删除证据。DE/Viewer等项目成员只读；不得跨项目读取。创建/批准要求开放项目，撤销也允许在已关闭项目进行，防无法释放已停用范围。

登记、批准和每次`LoadApprovedProvisionScopeTx`均复核平台内所有已批准network baseline的management CIDR与已批准服务器域，不把操作者文件当平台当前网络事实。外部清单完整性仍依赖网络团队独立审核，平台不能自动证明未知现网设备配置。反向在网络baseline批准时拒绝与已批准服务器域重叠；两个批准路径使用同一事务所有权锁，先锁所有权再锁baseline，避免并发都通过或锁序倒置。正常授权读取不持该全局变更锁，防串行化每台文件传输；批准scope行/审批人当前权限、项目状态仍须校验。保守保留历史已批准管理池，不能因项目结束自动认定现场DHCP已撤下。

本阶段不监听端口、不配置NIC/DHCP/Kea、不授予安装。撤销/审批人失权/项目关闭会在后续live Authority检查拒绝新授权；已经写入且结果未知的任务仍需真实回执核实，不因撤域自动解锁。新源迁移054尚未应用；部署前须集中运行审批/冲突/并发/RBAC/撤销集成用例，并另行验证当前网络拓扑。页面和API契约已生成类型，未发布3000或运行浏览器。


### 安装写盘前持久状态与启动身份（源码，055未应用）

prepared只证明源准备完成，不能复用已经释放的准备锁开始装机。`POST /api/v1/projects/{id}/provision/requests/{requestId}/install`消费当前prepared版本、已批准安装配方和scope，创建独立无重试provision.install Execution；安装API默认由KEEL_PROVISION_INSTALL_ENABLED=false关闭，API/worker开启时必须提供独立Ed25519公钥文件KEEL_PROVISION_IMAGE_PUBLIC_KEY_FILE及介质存储。该开关不启动DHCP/PXE/HTTP监听或写盘。实际源与签名initrd校验后才允许绑定新的attempt/command/fence和预检Spec，公开Grant本身没有认证能力。

数据库owner可用`keel-admin provision-boot-token-issue --project <id> --installation <id> --reason <reason> --output-file <absolute-private-path> --expires-in 5m`发行单次启动注册凭据；使用owner数据库连接，禁止通过公共API发行。目录须已存在、归当前操作者且0700，无符号链接；只创建新0600文件，stdout仅非秘密绑定信息。有效期1～10分钟且不超过安装会话；文件含schema/token/binding，必须通过可信带外通道送到指定机器，当前没有自动交付实现。未消费凭据用`provision-boot-token-revoke --project <id> --installation <id> --token-id <id> --reason <reason>`显式撤销后才能另发；已注册启动不能换绑公钥/BootID。Token原值不入DB/日志/iPXE/公共清单/cloud-init。目标生成每次启动Ed25519身份，注册后每条消息绑定完整payload SHA256、installation/boot/operation和严格递增sequence；这依赖带外凭据正确交付，不是TPM硬件证明。

平台接口复核live独占租约及完整冻结来源，认证预检复算后存精确原始报告。`/provision/installations`列表/读取供Admin/DE/DL/SRE查看；`/{id}/storage`在30秒新鲜预检窗口显式选择WWID/serial/容量/扇区与擦盘意图，生成完整绑定Spec/原报告/设备号/diskseq的固定UEFI GPT/ext4布局；过期必须先在启动环境刷新预检，不能复用历史PASS。`/{id}/storage/approve`由另一当前Admin/DL批准确切版本，且不同于安装发起人与选盘人。人工批准不要求历史报告永远保持新鲜，但将来实际写入前必须重新采集验证并取得当前许可。每次变更重查scope审批、BOM、介质和公钥；失败或撤域不能制造安装成功/主机信任。

此批次当时尚无生产文件Authority与回调接线；后续小节已补源码接线、initrd带外文件输入及显式互动预检刷新。仍没有write-permit端点、真实写盘、首启回调/host-key或SSH首连/修复、完整阶段投影。worker仅能证明从未发出写盘许可的失败/取消；将来发过permit而无终态证据须unknown，保留资源对账，不盲重试。055及新用例尚未运行，不能开启此源码增量当作已完成安装服务。


### 当前授权与隔离引导服务（源码，尚未启用）

`ProvisionBootAuthority`将已有隔离ProxyDHCP/HTTP/TFTP和签名注册/预检接到当前DB安装会话。公开MAC/Grant不提供回调权限；每次文件访问检查scope完整摘要、直接peer、当前执行租约/冻结来源及介质隔离状态。固定批准角色经真实字节校验后分发，iPXE/manifest/spec/seed确定性渲染；不接受客户端文件路径。对象IO期间不占SQL连接或attempt/resource锁，传输每250ms及hash后复查当前权限。DHCP指纹只记录辅助证据事件，不能代替一次凭据或启动私钥。后续真正完成注册仍须结合HTTP完成证据、指纹及首启/SSH验证，不在本阶段自动投影。

独立协议见[provision-boot.openapi.yaml](../api/provision-boot.openapi.yaml)。POST `/boot/{grant}/enroll`使用单次Bearer Token，正文canonical `{enrollment,proof}`；POST `/boot/{grant}/preflight`不带Token，正文canonical `{report,proof}`，report为精确helper stdout的base64。原字节签名/重算，拒绝别名/重复字段、跨项目/启动/operation/sequence及过期/撤销。回调独立HTTPS、固定Host/直接peer、无Cookie/Origin/代理身份，scope当前状态在消费proof的同一事务重查。ACK只表示注册或预检入库，不是写盘许可。

新增可执行源码`cmd/keel-provision`，仅显式启动，API/worker和Compose不自动拉起。须同时设置KEEL_PROVISION_SERVICE_ENABLED=true与KEEL_PROVISION_INSTALL_ENABLED=true、KEEL_PROVISION_PROJECT_ID/KEEL_PROVISION_SCOPE_ID、KEEL_PROVISION_IMAGE_PUBLIC_KEY_FILE、介质存储配置及KEEL_PROVISION_TLS_CERT_FILE/KEEL_PROVISION_TLS_KEY_FILE。scope须已独立批准并与实际服务器地址/接口/子网一致。证书须匹配批准origin且当前有效，私钥文件归当前操作者且0600；HTTPS仅TLS1.3，bind固定server IPv4及SO_BINDTODEVICE，UDP复用原ProxyDHCP与TFTP约束，不分配地址。scope失效/DB无法确认或子服务退出会取消其他listener；运行需显式网络端口/设备绑定权限，不自动提权/修改NIC。当前未执行该命令，未部署或验证监听。

`/run/keel-bootstrap.json`是保留的可信带外输入，不在公共initrd/iPXE/seed中；必须由PoC确定的可信机制在runtime LoadBootstrap时交付，当前不提供自动交付。只在root PID1 ephemeral环境从tmpfs、root单链接私有文件读取；不存在则保持未注册只读准备，存在却无效则阻断。bootstrap整个Spec与独立验证boot files完全匹配才生成内存Ed25519密钥并注册。成功后删除Token文件，后续预检不再带Token。报告原字节签名；回包未知不重发、不重新注册、不声称成功，已采集事实仍保留本地tmpfs。初次预检之后可按下节显式请求刷新并读取审批状态；新网络会话与writer仍待接，不将历史事实当实时写盘依据。

装机准备页面现提供独立安装会话发起、会话列表和存储选盘/独立审批。列表仅摘要，复核窗口读取完整认证事实并固定版本；前台轮询不刷新硬件报告。磁盘必须明确选择，稳定身份/几何/占用完整、新鲜PASS和UEFI才显示可选，并确认磁盘序列号及擦除意图。审批不同于写盘许可；没有Token浏览器交付、手动成功或未知结果自动解锁。该页面源码尚未发布/浏览器验收。


## 同一启动会话的显式预检刷新与审批等待（源码，待集中验证）

POST项目安装会话`/preflight/refresh`要求当前Admin/DE/DL/SRE、未选盘的preflight_ready、精确版本和理由，将请求ID/当前报告摘要/回调序号记入既有append-only事件及审计。相同报告序号只准一个待处理请求；请求登记不变更业务版本，收到新签名报告才递增版本。等待中的请求阻断旧事实选盘。初次报告序号2，最多32份报告，不无限续签事实。

机器POST `/boot/{grant}/review`使用已注册启动密钥签署canonical `{nonce}`及最后确认的报告序号；完整当前执行/来源/scope/peer仍在同一短事务重查。只读查询不消费序号，不修改审批状态/版本，不授权写盘；固定TLS连接返回匹配nonce/boot/installation/sequence/expiry和当前报告摘要。客户端每次生成新的128-bit随机nonce，核对本地最后ACK的报告摘要，拒绝旧回包/换报告。查询只允许preflight_ready/awaiting_storage_review/storage_approved，未知、撤销、租约失效或网络错误停止会话。

只有待处理请求触发同一内存启动密钥下再次调用原只读helper。刷新POST仍走preflight路径，附refresh_id，proof operation为preflight-refresh，签名输入为`Keel preflight refresh v1`、NUL、请求ID、NUL、精确helper原字节。控制器同时核对当前待处理请求、下一序号和新鲜事实；重复请求、错绑及选盘后报告均拒绝。回包丢失不重发，已有报告留本地。首份证据在等待人工前即落tmpfs；后续各份保留独立`/run/keel/reports/<sequence>`目录，不覆盖历史证据。

initrd审批等待受原DHCP租约、ARP冲突监测、Spec到期和上下文共同约束，不自动续租/重新注册/换接口。布局批准后仅返回storage_approved_no_write_permit并撤下临时网络，仍没有写盘器、写盘许可或自动重启；未来writer必须在独立工具/镜像输入及当前许可完成后接在同一受控运行阶段。页面增加请求刷新按钮与等待提示；列表刷新只是读取状态，不能刷新硬件时间戳。


## 已批准布局到确定的GPT字节（源码，不执行写盘）

provisionstorage.PrepareGPT消费完整有效Plan和独立的3个UUIDv4（磁盘/EFI分区/根分区），确定性生成目标盘首尾各1MiB的PMBR、primary/backup GPT、128×128字节分区数组、CRC和零保留区。保持明确的UEFI FAT32+ext4两分区布局与512/4Kn几何，不变动批准的分区边界；两端字节与完整Plan摘要/UUID/容量/扇区共同摘要，供后续当前许可绑定。InspectGPTEdges只读核对上述精确区域，返回同一摘要；不证明真实盘身份/几何、文件系统、擦除完成或可启动。无打开设备、写入/格式化/挂载调用，没有许可接口调用或安装成功投影。

格式字段依据[Linux GPT结构定义](https://github.com/torvalds/linux/blob/master/block/partitions/efi.h)与[GPT解析校验](https://github.com/torvalds/linux/blob/master/block/partitions/efi.c)，类型GUID参照[gptfdisk类型登记](https://android.googlesource.com/platform/external/gptfdisk/+/c59dc86/parttypes.cc)。实现为独立Go字节构造，不复制其源码；generic Linux filesystem类型不冒称DPS根分区自动发现，GPT不等同安装bootloader。真实writer仍须独立批准工具/镜像、当前VerifyCurrent及一次性许可、受控设备FD和每个破坏操作前的权限检查，再回读/回执。


## 独立writer工具输入进入完整镜像（v2源码，尚无工具执行）

v1 runtime-set及initrd归档保持原五输入和精确JSON/路径；v2明确使用`keel.installer-runtime-set/v2`与`keel.installer-initrd/v2`，增加writer_tools.unsquashfs、writer_tools.mkfs_ext4、writer_tools.mkfs_vfat三项media_id/sha256。对应制品类型tool、相同amd64/arm64架构、静态ELF且无子依赖；固定归档路径usr/sbin/unsquashfs、usr/sbin/mkfs.ext4、usr/sbin/mkfs.vfat。八个输入ID必须互异，CA仍config/noarch。v1不能悄悄携带工具，v2不能省略任何一项；拒绝大小写别名/重复/未知字段、架构错绑、动态ELF、缺项和改字节。原大小/展开边界继续生效。

开发/生产顺序：先取得并独立批准实际三个工具字节（工具构建与行为验收由镜像输入/PoC提供）；与已有五项共同登记v2 config的八条直接依赖，独立批准并导出签名manifest；复用现有provision-initrd-create/verify离线链生成/反向验证单一严格归档，不能追加另一未批准CPIO；最后将生成的initrd作为新制品另行批准并纳入OS recipe。不要求未来OS成品先存在，不启动工具或设备操作。独立签名与所有八项字节核对后才记writer_tool_bytes_verified，CLI与boot.activated事件同时明确disk_write_authorized=false；运行入口目前仍只有只读预检/审批等待。

该输入链不等于写盘器或可启动系统。后续先落实目标OS的安装启动内容契约（网络引导iPXE EFI不是目标盘bootloader），再将当前硬件复核、GPT摘要、可信工具/系统内容与独占lease绑定到一次性许可，执行受控操作/回读/失败unknown回执，之后接首启/SSH。未执行本批CLI、工具、归档生产、网络监听或镜像/硬件启动。


## 目标系统启动内容契约（源码，非启动验收）

`keel.boot-recipe/v1`继续支持原启动/预检输入；实际写盘必须通过`Plan.ValidateWritable`，要求显式`keel.boot-recipe/v2`及`target_boot`。该声明绑定已批准squashfs中的固定文件`usr/lib/keel/target.efi`、精确SHA256/字节数/内核release与固定命令行`root=PARTLABEL=keel-root rootfstype=ext4 rw rootwait`。先独立制作包含目标系统UKI/模块/首启组件的rootfs，再登记配方和独立审批，没有未来最终安装结果的反向依赖。网络引导iPXE EFI仍只负责进入安装环境。v1不自动补默认目标启动内容。

目标UKI使用单一PE32+ UEFI application、相同CPU架构，要求实际`.linux`、`.initrd`、`.cmdline`、`.uname`和`.osrel`；核对完整字节摘要、段边界/重叠、固定命令行、release及零填充，拒绝multi-profile和隐藏附加命令。格式依据[UKI规范](https://uapi-group.org/specifications/specs/unified_kernel_image/)；这是Keel首次实现的受限输入格式，不代表所有UKI均须具备这些可选段。目标内容安装到新EFI分区的架构固定fallback路径，amd64为`EFI/BOOT/BOOTX64.EFI`，arm64为`EFI/BOOT/BOOTAA64.EFI`，不会修改固件NVRAM或复用iPXE。

rootfs还须包含`usr/lib/os-release`、对应`usr/lib/modules/<release>/modules.dep`、同冻结版本`usr/local/libexec/keel-ssh-runner`、`usr/bin/cloud-init`及精确NoCloud配置`etc/cloud/cloud.cfg.d/99-keel-datasource.cfg`（内容`datasource_list: [ NoCloud ]`并换行）。安装器只读取/复制内容，不执行rootfs内程序或维护任意模板脚本。静态PE/文件存在与摘要一致均不是启动成功、驱动可用、SecureBoot信任、cloud-init已启用或SSH互信证据；这些须实际镜像和首启验证。页面使用共享组件录入/展示显式目标UKI文件信息，无自动构造或猜测摘要。


## 当前许可、受控writer与独立首启身份（源码，未执行）

写盘入口在同一受控boot和有界网络会话内，顺序为：认证批准布局→从当前server review取得真实激活时的runtime config摘要→固定rootfs分发和全字节校验→封存工具/系统内容→独占打开明确盘→再次只读采集VerifyCurrent→一次write-permit→逐步骤当前权限检查→实际回读及同步卸载→一次write-receipt。此前30秒报告不能授权后来写入。运行环境v1或缺v2目标内容不进入writer，缺任何实际配置/能力均拒绝，不自动猜盘或换设备重试。

许可通过当前签名boot key绑定storage plan、精确GPT identity/字节摘要、rootfs、目标UKI、runtime config及可选独立firstboot public binding。服务端重新验证当前执行/独占lease、scope、请求/布局审批人权限和所有独立runtime输入的审批/隔离状态。issue只消费一次序号，同事务asset变为provisioning/version+1并将SSH trust重置unverified；旧pin和已证明域身份作为历史保留。唯有该installation事件中的精确active version能继续核验，普通准备入口仍拒绝provisioning，不放松全局guard。每个破坏步骤和长操作期间250ms复查，authorization最长2秒，独立watchdog及时取消慢查询中的操作。回包丢失不重发mutation；发过许可后的无终态证据保持unknown与资源隔离。

安装环境只接受8GiB以内压缩rootfs，并要求实际可用内存覆盖归档/UKI/工具缓冲和保留空间；这不是要求用户提供固定硬件型号。squashfs v4输入全摘要/尺寸/表边界及有界固定文件只读解码后封存memfd；所有工具来自server绑定的独立批准runtime输入。根内容另须包含同架构静态`usr/local/libexec/keel-firstboot`，信任来自整个已批准rootfs，不拿自算文件摘要当独立批准。SquashFS格式参考[原作者结构定义](https://github.com/plougher/squashfs-tools/blob/master/squashfs-tools/squashfs_fs.h)，命令参数参考同项目unsquashfs帮助源码；本实现独立处理有界字节。

Linux writer仅root PID1 initramfs可启用，持整盘O_EXCL FD并核对设备号/diskseq/几何，使用同一FD精确offset/sizelimit的专属loop映射两分区，不释放整盘后重开动态分区名。固定8阶段为GPT、EFI格式化、根格式化、rootfs提取、UKI安装、seed/首启配置、同步、回读。静态sealed工具在私有PID/mount/net/IPC/UTS namespace的只读jail内运行，不执行rootfs脚本；目标文件写入有路径边界。回读覆盖主备GPT、FS/UUID、UKI/SSH/首启/NoCloud/模块关键文件、seed/fstab及同步卸载；rootfs摘要表示来源绑定，不表示全文件系统逐字节或数据安全擦除证明。任何异常quarantine强持本地设备/挂载引用及控制面资源；即使本地written而ACK未知也不释放或重写。

首启使用安装时新生成的独立Ed25519 key，公开binding由原boot key签入permit，私钥仅通过受控writer写入root-owned 0600文件，不进入公共seed、下载或日志。固定systemd oneshot在cloud-final完成后执行同批准rootfs中的firstboot helper，不自动重试；要求实际kernel boot ID不同于安装环境，核对主机名/SN/MAC/CPU、根盘身份/几何/GPT分区及真正新SSH host key。固定TLS CA/主机名/IP/interface发送一次签名报告，本地durable intent先于IO，丢ACK保留unknown；ACK落盘才删除单次私钥binding。控制面同时核对当前同installation的实际DHCP辅助事件，但不把DHCP当认证。身份登记后仍awaiting_first_boot、SSH trust未建立；仅后续真实SSH首连证明才能registered。以上源协议与格式/DB/HTTP/工作流用例均尚未运行，不等同镜像启动、设备写入或P1验收。


## 当前源码集成：机柜准备、事实进度与SSH受控恢复（2026-10-26）

机柜入口仅在当前项目/BOM精确rack内显式选1～50台，逐机确认完整SN、版本、网口、批准OS和域公钥；单事务与幂等缓存返回一批原requested请求。每台仍独立审批、准备、安装会话和显式选盘；不提供自动整柜执行或跨机授权。

安装进度API/双语页面按七阶段投影持久事实，显示证据ID与当前实际租约；不从后阶段反推早阶段，也不把DHCP、环境注册、自报written变成实际启动/安装/SSH成功。`boot.file_served`投影为预留输入，实际HTTP完整响应记录生产者仍待实现。

SSH未知结果仅显式query原helper回执；只有当前fresh pinned query证明终态failed、repair快照仍可用、权限及原lease有效，人工授权才允许独立新只读probe。新verification command只作为append-only绑定，原native command/attempt/fence不变。未知或missing禁止借新ID重发；成功native receipt与注册/trust仍同事务。重启失败的隔离保留真实整盘独占FD。

当前仅源码/编译/静态证据，功能测试及硬件PoC未执行。首次装机无旧host pin的operator allowlist配置边界仍待补齐；不应以伪造旧pin解决前提。用户最新要求统一合并后单Agent开发，后续以HANDOFF当前恢复入口为准。


### 完整文件分发事实生产者（当前源码）

前节预留的`boot.file_served`现由隔离HTTP Gateway生产：固定版本全hash后，仅GET/200完整服务器写出且最终当前权限仍有效，数据库再核scope/lease/冻结sha、size和objectVersion才去重追加server_write_only事实。不记录HEAD、缓存命中、Range、短写或取消，证据持久化失败则保持阶段缺证据。普通文件分发不改变安装许可/版本/trust。它只证明服务器完整写入ResponseWriter，不能证明客户端接收、执行或boot；TFTP完成仍另计。配置与传输用例均未运行，编译/vet证据见HANDOFF 109～110。


### 已注册成功后的worker崩溃对账（当前源码）

若注册/trust和native receipt已原子提交而Complete前worker退出，原attempt/lease会由reaper隔离。授权用户可通过既有执行Reconcile消费该份已存证明：必须原native身份、exclusive reconciling资源、reconciling执行、准确SSH事件承诺和当前投影身份/资产版本一致；错误、失去claim或没有证明均拒绝。该路径不续租、不调用SSH、不重装，也不适用于尚无成功证明的unknown安装。页面区分registered投影与实际资源释放。仅编译/vet，崩溃边界fixture未运行，见HANDOFF 111。


### 写盘前隔离任务的显式收尾（当前源码）

对于reaper隔离但从未签发写盘许可的安装，当前Admin/DL/SRE可提交准确版本/原因/幂等键至`prewrite-reconcile`。服务器锁住原执行、attempt和installation，再证明permit字段和事件均不存在、原机器独占claim仍reconciling，才同事务记失败/回执/审计并释放原资源。原资产与互信不变；不启动设备、不续租或重试。任何发过许可的unknown不允许此操作，必须继续隔离核实真实结果。页面按钮依赖当前事实，服务端始终复核；功能/数据库/浏览器用例未运行，见HANDOFF 112。


### 装机互信待修复列表

原安装列表GET支持可选`repair_only=true`（仅接受单个`true`或`false`）。该视图从同项目未完成安装的持久`first_boot.ssh_repair_required`事件派生，保留租约已过期的`unknown`，与分页总数共用过滤条件。它是当前修复证据的查看入口，不授予新attempt、重装、续租、解除隔离或批量推送权限；逐机进度及显式查询/只读核验仍由原服务端准入决定。`registered/failed/canceled`不列入，摘要仍不返回完整预检报告。源码回归与集中验证状态见HANDOFF §114。


### TFTP分发事实

小型iPXE固件仍是TFTP唯一允许资源。隔离socket逐块确认完整UDP写出、严格核对同一客户端TID/interface上的最终ACK后，复用HTTP的`boot.file_served`存储；此前各块必须实际确认发送，重传不加字节，512整数倍文件必须完成空终止块握手。记录前重新检查当前授权，存储层仍核对冻结对象版本/sha/大小和原独占租约。该事件只作服务器传输辅助事实，`server_write_only`不因UDP ACK提升成客户端执行或启动证据。纯协议驱动、部分/取消/撤销和缺ACK不产出事件；写证据失败不触发发送重试。源码边界用例尚未运行，见HANDOFF §115；§110的TFTP缺口历史记录由本节源码补齐，真实UDP/固件PoC仍待验证。
