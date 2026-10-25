# PoC 执行包 #P1 — H-1：十台 B300 的 HTTP-first 装机与自动互信

本包对应 IMPLEMENTATION-PLAN WP-1.9、ACCEPTANCE H-1/E-6。开发阶段只自检执行包；真机由工程所有者在独立批准环境执行。当前 SSH 安装/只读执行已具备部分能力，完整 WP-1.7 PXE/cloud-init/自动注册尚未交付，不能以本包或回环 SSH 测试宣称 H-1、WP-1.9 或 P1 完成。

`run-h1.sh` 现为**离线证据一致性校验**，不连接硬件、不调用平台 API、不装机、不创建执行，也不持有登录凭据。原有 Bearer Token、root 任意命令和自动接受指纹路径已移除。平台使用既有同源 Cookie 会话；不得使用旧 KEEL_TOKEN 方式。校验结果只有 BLOCKED 或 READY_FOR_REVIEW，`hardware_acceptance` 始终 UNVERIFIED。结构、摘要和交叉引用一致不能验证硬件身份、日志真实性或自动化率，签署仍由所有者完成。

## 环境与前置

- ACCEPTANCE H-1 要求 **10 台 HGX B300**；不沿用历史文档的8–12台窗口，也不以 GB300 Arm 替代。
- 独立 PoC 项目及批准网络窗口，BMC/控制台可达；经验证的 ProxyDHCP/UEFI HTTP Boot、介质分发和 cloud-init 版本必须已具备，引用其实际发布记录。当前开发环境没有这些能力时停止，不用模拟 provisioned 标记补齐。
- 不扰动现网 DHCP；收集非 PoC 客户端租约/网络观察。每台 hostname 与冻结 BOM 一致。
- 操作员按 SSH-RUNNER.md 准备非 root `keel-ctrl`、固定主机指纹、helper 和受限公钥；不修改3000开发环境默认禁用项。自动公钥注入/回调/信任建立必须另有真实流程及日志；手工批准一次 SSH 查询不证明自动装机互信。
- 用既有账号登录独立 PoC 平台，拥有项目 Admin/DL/SRE 所需读权限。通过 UI 导入 BOM：预检、确认再提交。样例 `poc/bom-h1.csv` 的 SN/MAC 必须替换为真实清单，不能把示例当真机证据。
- 本地校验只依赖 Python3 标准库和 POSIX sh，不需要 SSH 私钥、Token、jq、curl 或 Docker。

## 收集证据

在仓库外创建私有目录，将 `poc/h1/evidence.template.json` 复制为 `evidence.json`。填入环境、操作员、实际平台40位提交、项目32位ID、装机发布引用及UTC观察起止时间；预留的false项必须由所有者按实际观察逐项填写，不能为通过自检而直接改true。

所有文件路径相对 manifest 目录；只接受普通文件、每份最多4MiB，拒绝路径越界、任一级符号链接、FIFO、重复JSON属性及摘要不符。文件逐份计算SHA256填入manifest，缺件不会降级为直接SSH探测。

| 证据 | 实际来源与断言 |
|---|---|
| platform_health | `GET /healthz` 原始JSON，版本必须与manifest的40位提交一致；此记录仅证明进程版本，不证明业务就绪 |
| bom | 两段式导入后冻结的完整CSV，至少含SN/hostname，恰好10个唯一SN和hostname |
| assets | 当前登录项目 `GET /api/v1/projects/{project}/assets?page=1&page_size=50` 原始JSON正文；专用项目必须恰好10台，完整单页，SN/hostname与BOM一致、provisioned且ssh_trust_source=ssh |
| trust_requests | 同一项目 `GET /api/v1/projects/{project}/ssh-trust-requests?page=1&page_size=50` 完整单页JSON正文；每台选择实际succeeded的trust.bootstrap请求，项目/机器/独立批准/指纹/执行ID/观察窗口一致；repair不证明首次自动引导 |
| host_public_key | 每台从独立可信BMC/控制台取得 `/etc/ssh/ssh_host_ed25519_key.pub`；只留公钥，校验器计算SHA256指纹并与平台证明及执行快照对比。不能用未核验ssh-keyscan建立信任 |
| identity_execution | 现有执行中心成功host.identity后 `GET /api/v1/projects/{project}/executions/{executionId}` 原始JSON正文；同项目、精确机器/资产版本/主机指纹、终态回执command/attempt/fence及hostname一致。一次10目标执行可复用同一文件，不能使用分页缺失的attempt列表 |
| boot_log | 每台完整HTTP-first引导/装机日志，由所有者核验没有Legacy PXE兜底、时间和SN正确；校验器只检查文件在位/摘要 |
| key_preservation | 每台authorized_keys变更前后公开差异，所有者核验只追加受限平台公钥、保留客户内容；不复制私钥、口令或Cookie |

登录后可通过浏览器 Network 保存指定请求的响应JSON正文；不要导出含Cookie/口令的HAR。平台分页不完整时校验拒绝；使用专用项目并选取能覆盖全部记录的受支持page_size，不能截取十个PASS记录掩盖失败资产。当前回执中的 `hardware_evidence=false` 表示传输证明不直接签署硬件验收，不得手改为true。

## 自检、校验和签署

```sh
# 仅自检校验器，使用明确的合成数据，无网络或硬件操作。
sh poc/selfcheck.sh

# 对实际采集包做离线校验；缺件/不一致exit 1，参数缺失exit 2。
sh poc/run-h1.sh /private/poc/h1/evidence.json > /private/poc/h1/validation.json
```

READY_FOR_REVIEW仅表示10台记录内部一致、文件摘要匹配，**不是H-1 PASS**。所有者还须审阅原始日志、证明HTTP-first及互信100%确由自动流程完成、核验硬件身份/客户公钥保留/现网DHCP不受扰动，并填写 `poc/results/CHECKLIST.md` 的副本。该人工记录与原始证据、发布来源和校验结果一并评审后，才能另行签署H-1；软件不会自动更新ACCEPTANCE或trace状态。

历史 `poc/results/p1-selfcheck.json` 保留为2026-10-13旧脚本快照；当时仅检测工具/公钥文件，缺PXE仍记NOTE，**不能作为当前平台就绪或H-1证据**。新自检不覆盖该历史文件，也不往Git写真机记录。
