# PoC 执行包 #P1 — H-1：PXE 装机 + 自动互信（真机）

> 执行机制（IMPLEMENTATION-PLAN §5.5 / ADR-14）：开发期不连接真实硬件；本包由工程所有者在受控 PoC 环境（8–10 台真机）择机执行并回填结果。回填记录即 ACCEPTANCE L4 证据。**PoC 未执行不阻塞下一阶段**；H-1 回填 PASS 是 GA 前置条件。

## 验收判据（与 ACCEPTANCE H-1 一字不差）

| # | 断言 | PASS 判据 |
|---|------|----------|
| 1 | HTTP-first 引导成功 | BOM 中全部机器经 UEFI HTTP Boot 完成引导与装机（operator 观察项，见 CHECKLIST） |
| 2 | hostname 正确 | 每台机器 `hostname` 输出与 BOM 的 hostname 列一致 |
| 3 | 互信自动建立率 100% | 平台读回 `ssh_trust=established` 的机器数 = BOM 机器数；且 keel-ctrl 密钥可 BatchMode 登录全部机器 |
| 4 | host key 指纹记录 | 每台机器的 ED25519 指纹被记录进运行记录（`h1-fingerprint` 行）并可导入平台 `trust_logs` |

## 环境要求清单

**硬件**
- 10 台 B300 样机（H-1 判据窗口 8–12 台，目标 10），UEFI 启动模式
- 每台 BMC 可达（ProxyDHCP 与 HTTP 分发不依赖现网 DHCP）
- 2 台交换机（本包不验证交换机行为，仅为拓扑完整）

**网络/服务**
- ProxyDHCP 服务可答 UEFI HTTPBoot 请求（不扰动现网 DHCP——FR-2.2）
- HTTP 分发端可服务 kernel/initrd/ks 模板（`PXE_HTTP_BASE` 指向它，selfcheck 会探测）
- cloud-init 模板含平台公钥注入（FR-2.3：**同一模板**渲染 hostname 并写 root authorized_keys，只登记本平台公钥，不删客户既有公钥）

**凭据/平台**
- 平台服务可达（`KEEL_BASE_URL`，默认 http://127.0.0.1:3000）
- `keys/keel-ctrl` + `keys/keel-ctrl.pub`：平台控制面 SSH 密钥对（selfcheck 校验在位；**私钥只存在于控制面，不出现在任何目标机**）
- `KEEL_TOKEN` + `PROJECT_ID`（可选）：用于脚本读回资产/互信状态；未设置时脚本退化为直接 SSH 探测
- BOM 样例 `poc/bom-h1.csv`（列：SN,hostname,model_name,mac_address,port_name,rail；≤10 台）

**工具**（selfcheck 逐一探测）
- `ssh` / `ssh-keygen` / `ssh-keyscan` / `jq` / `curl`

## 执行步骤

```sh
# 0) 前置自检（不满足会明确报缺什么，exit 1）
KEEL_BASE_URL=http://<platform> sh poc/selfcheck.sh

# 1) 平台侧：UI 导入 BOM（两段式：预览确认冲突 → 提交，FR-2.1 要求人工确认，脚本不代替）
#    Assets → 导入 BOM → 粘贴 poc/bom-h1.csv 内容（CSV 模式，文件版本 h1-<ts>）→ 预检 → 确认导入

# 2) 启动装机并等待回调（operator 在装机服务侧执行 PXE 计划；观察项记入 CHECKLIST）

# 3) 互信自动建立后，运行真机断言
KEEL_BASE_URL=http://<platform> PROJECT_ID=<id> KEEL_TOKEN=<token> sh poc/run-h1.sh
#    未设置 token 时仍可跑（直接 SSH 探测 hostname/登录/指纹），但平台读回列会标注 NOTE

# 4) 回填
#    - 运行记录：poc/results/h1-run-<ts>.json（自动生成）
#    - 观察清单：复制 poc/results/CHECKLIST.md → 勾选并署名
#    - 两份文件合并即 ACCEPTANCE H-1 的 L4 证据
```

## BOM 样例格式

```csv
SN,hostname,model_name,mac_address,port_name,rail
B300-POC-0001,poc-b300-n01,B300 HGX 风冷 8U,aa:bb:cc:00:10:01,port1,0
B300-POC-0002,poc-b300-n02,B300 HGX 风冷 8U,aa:bb:cc:00:10:02,port1,0
```
（完整 10 行由工程所有者按真实 SN/MAC 回填；此格式与平台 CSV 导入完全一致）

## 已知边界（诚实声明）

- **脚本不代替两段式导入的确认动作**：FR-2.1 要求冲突预览→人工确认，包里只留 UI 路径
- **平台读回是可选路径**：无 token 时退化为直接 SSH 探测，判据 3 的"平台侧 100%"以 API 读回为准、SSH 探测为独立佐证
- **装机过程本身（ProxyDHCP/iPXE/cloud-init 渲染）属 WP-1.7 真实通道段**，本包验证其结果而非实现过程；装机服务由工程所有者按 FR-2.2/2.3 部署
