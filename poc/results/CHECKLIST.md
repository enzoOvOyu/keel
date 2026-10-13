# H-1 观察项清单（operator 人工回填）

> 与 poc/results/h1-run-<ts>.json 合并构成 ACCEPTANCE H-1 的 L4 证据。
> 自动断言未覆盖的观察项必须逐条勾选；任何一项 NO 即整包 FAIL。

执行人：____________  执行日期：__________  PoC 环境编号：__________

## 判据 1：HTTP-first 引导成功（脚本不可自动断言）

- [ ] 10 台机器全部经 UEFI HTTP Boot 完成引导（控制台/串口日志无 Legacy PXE 兜底记录）
- [ ] 引导失败的机器数：______（>0 时整包 FAIL，记录失败原因）
- [ ] ProxyDHCP 未扰动现网 DHCP（抽查一台非 PoC 机器 DHCP 正常）

## 判据 2：hostname 正确

- [ ] 脚本 `h1-hostname` 行全部 PASS（自动断言）
- [ ] 抽查 2 台机器：`hostnamectl` 显示与 BOM 一致，cloud-init 渲染日志无警告

## 判据 3：互信自动建立率 100%

- [ ] 平台读回（设置 token 时）：脚本 `h1-trust-rate` 行 PASS
- [ ] 直接 SSH 探测：脚本 `h1-trust-rate-ssh` 行 PASS
- [ ] 平台 trust_logs 页面/接口可见每台机器的 trust.bootstrap 或首次 verify 记录
- [ ] 任意一台机器的 authorized_keys 中**只新增**了平台公钥，未删除客户既有公钥

## 判据 4：host key 指纹记录

- [ ] 脚本 `h1-fingerprint` 行全部输出 SHA256 指纹（自动断言）
- [ ] 指纹已与装机回调记录（或 trust_logs）比对一致
- [ ] 人工抽验 1 台：`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` 与记录一致

## 结论

- [ ] **PASS**：以上全部勾选且脚本 failures=0
- [ ] **FAIL**：原因记录：________________________________________

签字：____________
