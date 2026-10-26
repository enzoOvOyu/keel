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
