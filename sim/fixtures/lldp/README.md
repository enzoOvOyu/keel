# LLDP 软件模拟样本

所有文件均为合成协议样本，**NOT_RUN**，不是真实硬件或PoC证据。`expectation.json`是独立冻结期望；观测文件显式保存两张NIC各自的接口状态、json0本地/邻居原文和两台交换机的对端邻居，不在运行时从BOM生成观察。

`NewFixtureDriver`只模拟Inspect/Begin/Observe/Stop，不执行命令、不更改网络、不写文件，来源固定sim。`NewFixtureCollector`只处理冻结command/window派生的请求，来源固定protocol_fixture，不访问任何端点。模拟时钟只赋给新模拟会话，查询原命令保持第一次终态时间/结果，不当作真实刷新。两者不能由JSON覆盖为真实来源。

| 文件 | 独立事实 | 纯规则期望（非硬件门禁） |
|---|---|---|
| dual-ended | 两张NIC对应不同永久MAC/物理PortID/rail，双端一致 | Step2 PASS → Step3 PASS |
| link-down | 本地及交换机链路down，无邻居 | Step2 FAIL/T1，阻断Step3 |
| readable-empty | 读取成功、完整窗口内无邻居 | Step2 FAIL/T1，阻断Step3 |
| unreadable | 原文读取失败，对端也不可读 | Step2 BLOCKED，不冒充断链 |
| wrong-peer | 服务器读到swp99，冻结BOM为swp1 | Step2 PASS → Step3 FAIL/T2 |
| stop-unknown | 无原停止/恢复证明 | unknown，资源不释放 |
| lost-reply | 已完成原回读但首次响应丢失 | unknown；只查询原命令，可读到原终态 |
| collector-running | 原采集器会话仍running | unknown，主机停止也不放行 |

无论纯规则期望如何，通过模拟驱动的归档始终保留sim/protocol_fixture来源，真实门禁不得PASS。用例只在下一次P2集中验证运行；样本适配器没有注册到3000、普通SSH或真实特权helper，也没有建立新的任务调度系统。
