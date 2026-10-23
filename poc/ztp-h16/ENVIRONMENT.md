# H-16 ZTP 真机 PoC 执行包

本包由工程所有者在受控环境执行；当前开发/CI 不连接真实交换机。执行包就绪不等于 H-16 PASS。验收要求至少两台 Cumulus 交换机，覆盖 239/143 引导、option 60/43、SN/管理口 MAC 身份反查、未知身份隔离、冲突拒绝，以及首次引导只有最小管理配置。生产网络配置只有经 PushJob 审批及复采后才能标记 IN_SYNC（ACCEPTANCE H-16）。

## 环境

- 两台以上目标 NOS 版本的 Cumulus 交换机及制造商 IDevID 链；记录型号、NOS 版本、SN、管理口 MAC，不上传私钥。
- 独立管理网、可控制的 Kea DHCPv4 实例和抓包端口；239 TLS 脚本入口与 143 mTLS 引导入口的服务证书受设备信任。禁止连接客户生产 DHCP。
- 项目 BOM 含交换机 SN/MAC 与介质库已独立批准的隔离脚本；另准备一台未命中 SoT 的身份和一组冲突身份进行负例。
- `tcpdump`/`tshark`、`openssl`、设备 `journalctl`/`nv show`、平台只读导出工具。输出按下面的目录放置，不包含 Cookie、密钥、明文凭据或设备全量配置。

## 执行与证据

1. 记录 Kea 完整配置的审批引用、活动配置哈希、重启后哈希；在独立管理网抓包并核对 eth0 的 DORA 与 option 239/143/60/43。前面板端口不得得到引导 offer。
2. 对 239 通道验证六个 `CUMULUS-*` 头、精确 URL、脚本 SHA256、设备脚本成功日志；对 143 通道验证服务端 mTLS 证书链、IDevID SN、`get-bootstrapping-data`/`report-progress`、CMS 摘要。两通道分别检查 SN/MAC 与 BOM 一致。
3. 验证未知身份只得到最小隔离/管理配置，冲突身份不收到业务配置；检查业务端口、管理口和设备运行配置。首次引导不得记为生产配置 IN_SYNC。
4. 若 PushJob/Golden 比对尚未实现，H-16 标记 `BLOCKED`，不得用 ZTP 完成声明代替审批、下发和复采证据。
5. 将每个案例的证据文件置于 `poc/ztp-h16/evidence/`，按 `evidence.template.json` 填写路径与 SHA256；运行 `python3 poc/ztp-h16/validate.py <manifest>`。脚本只验证证据齐备与摘要，输出 `READY_FOR_REVIEW`，人工复核及签署另行回填 ACCEPTANCE H-16。

验证器不会启动 DHCP、重置交换机、修改配置或把自填 JSON 解释为设备行为证明。
