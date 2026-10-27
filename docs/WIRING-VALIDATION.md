# 布线验证实施记录（WP-2.4 / WP-2.13）

2026-10-27，当前为源码增量，全部新软件/协议/数据库/浏览器/硬件检查 **NOT_RUN**，未迁移或部署。迁移084增加只读范围及显式在线握手的不可变证据，尚未发布StepReport、G2、NP-3或Mapping。

## 当前握手

`POST /api/v1/projects/{id}/validation/wiring/scope` 读取明确指定的资产与物理端口，不能接收就绪结果、遥测或设备地址。最多200台、每台128端口、整批4096端口；按稳定ID规范化。读取不探测硬件、不插入证据。资产拓扑详情中的端口选择页复用型号显式GPU→NIC关联，缺失关系不猜测。

每个主机必须有当前真实G1和独立型号对比PASS的最新拓扑；每个选中物理端口必须存在于该G1冻结BOM，绑定原始永久MAC、接口和BDF，且有唯一BOM线缆通向已建模交换机端口。对端交换机只检查本次相关范围的NP-1/2/4；NP-2复用认证管理身份、当前Golden执行与五分钟内回读判据；NP-4区分“LLDP可读取”和“邻居正确”，不提前要求链路匹配。NP-3由后续G2产出，NP-5/6属于后续运行态，不作为本握手前提。

`POST .../handshakes` 要求CSRF、幂等键、工程权限和原因。首次读取并检查权限后释放事务，只向固定mTLS采集器健康端点做有界GET；不调用Kea/引导/仓库检查。随后重新鉴权、核对相同事实摘要，再保存最长30秒（目录可缩短）且不超过任何源证据有效期的凭证。就绪时间从探测前开始，等待数据库锁不会延长它。探测失败或范围变化没有成功证据。

`GET .../handshakes` 只返回历史，未过期记录标记unchecked，ready仍false；`GET .../handshakes/{id}` 只重新核对数据库事实，不能主动续期或再次探测。来源、BOM、G1、拓扑、Golden、认证管理绑定、采集配置或功能开关变化后当前有效性不能继续PASS。页面本地计时到期即撤销有效展示；再次检查仍依赖服务端准入。

`KEEL_WIRING_ENABLED=false` 默认关闭，API/worker/compose只增加配置源码，不修改真实环境。开启该标志也不会启动LLDP或发布G2。原始网络草稿保持隔离。

## 后续执行边界

下一步复用共享Execution、稳定物理端口身份、任务回执与unknown恢复，实现固定lldpd配置/双端读取和原始证据归档，再接版本化四步StepReport。整批必须在30秒窗口内接纳；初次凭证未来应绑定唯一Execution，不能复用于另一批。超过30秒的整批排队不应靠延长凭证或要求原凭证在每个后续主机开始时仍有效：派发应重新核对当前范围及在线证明，原凭证只证明最初接纳。该执行绑定与逐主机检查当前**尚未实现**。

硬件工具、型号特性、真实LLDP和PoC由后续受控环境独立取证，不能将fixture、配置存在或源码生成标成硬件通过。统一豁免账本尚未实现，不以unknown或不适用猜测绕过门禁。

## 双端证据与三方判据源码

`internal/wiring`增加有界json0邻居/本地广播身份解析、原始JSON重复键拒绝，以及逐端口的Step2→Step3纯逻辑。Step2要求本地BDF/永久MAC与冻结身份相同、当前MAC与永久MAC一致、链路和观察窗口明确、广告TLV正确，并有广告启动之后的真实交换机端口回读；失败或缺测阻断Step3。Step3通过独立交换机MAC绑定核对服务器看到的ChassisID/端口，并检查交换机读到的服务器永久MAC/物理端口稳定ID/hostname/SN+rail+端口字段。不用sysname或SN前缀代替身份。断链/明确空邻居为T1、确定错线为T2，本地MAC/广告配置冲突为T6；不可读/多邻居/缺可信身份为BLOCKED，不猜测NIC硬件损坏。T3/T4/完整T5/交换机T6继续复用已有fabric诊断，不在LLDP核心重写。

固定计划为每个物理端口生成独立lldpd实例、独立socket/PID/配置，避免全局ChassisID/sysdesc错误复用于多个NIC；只给明确选中接口，保留全部邻居而非随机选一个，不读写客户全局配置。实现依据[lldpd官方手册](https://lldpd.github.io/usage.html)与[上游json0结构测试](https://github.com/lldpd/lldpd/blob/master/tests/integration/test_lldpcli.py)；启动配置先于自动resume的语义依据[官方daemon源码](https://github.com/lldpd/lldpd/blob/master/src/daemon/lldpd.c)。命令计划只是可审查数据，未执行或注册到普通任意命令入口。

现有SSH wrapper为非root；真实临时interface-up、隔离lldpd及停止/恢复证明还需受控特权适配器，不能直接在该wrapper中偷偷加sudo或启用系统lldpd。共享Execution派发/停止回收接线尚未完成，页面仍不提供LLDP执行。新增单元场景覆盖完整双端、缺测、不可读、空邻居、多邻居、旧/fixture交换机、错线、序列号前缀和sim隔离；均NOT_RUN。

## Step 1 与四步记录

迁移085新增不可变batch/targets/StepReport。显式`POST .../batches`以本次握手ID、事实摘要、原因和幂等键消费唯一未使用握手，再次核对当前范围，归档每台的独立拓扑GPU→NIC关系为Step1版本1；最多200台，不更改资产里程碑。NE可发起布线握手/Step1，与DE/DL/SRE/Admin共同工程权限，拓扑校验权限本身未扩展。握手一旦被消费，GET明确返回batch_id和不可再次准入状态。重试同幂等键只返回原batch，不能再消耗或生成版本。

StepReport具备step/version/parent/source_hash/verdict/detail及历史、当前有效判定。Step1有效性只依赖其当前拓扑，初次网络握手过期不会回溯抹掉它；拓扑/G1/型号变化或过期则当前Step1为BLOCKED，历史内容保留。前端显示四步时间线，后3步仍未执行，不显示假的PASS或G2。当前数据库明确拒绝Step2～4插入，待真实受控执行回执绑定迁移后才开放；不能上传判定绕过前驱PASS和范围检查。下一工作包仍是LLDP特权适配器/共享Execution停止证据、Step2/3持久化及定向重验，之后才是DL确认和NP-3。全部新增验证NOT_RUN，当前P2保持partial。

## LLDP会话与双端采集窗口

设备适配器保留`Inspect→Begin→Observe→Stop`边界；已有客户LLDP、VF/bond/bridge归属或冻结BDF/MAC不符时不能开始。固定计划的每端口实例不等于已有真实特权实现：当前仅实现驱动接口与协调器、源码中的模拟驱动用例，尚未安装/启用Linux特权helper，也未注册普通SSH任意动作。

`CaptureWithReadback`在同一次有界流程中先取得主机观察，再**保持广播期间**调用固定FR-0.8.2回读，之后停止自有进程并恢复原接口。交换机快照必须落在advertising_at～advertising_stopped_at，不能用停止后读取的空邻居造成假T1。原网络握手不代替本次新回读。观察或回读失败也必须尝试停止；独立3秒cleanup不受调用取消影响。缺少确切停止/恢复证明、原地址/MTU/MAC等配置变化或启动结果不明返回unknown，不自动再Begin。缺少协调回读时局部抓取仍可归档，但不可供G2通过。

归档保留原始有界lldpcli json0正文、观察元数据、原/后接口状态、计划/命令摘要、停止证明和交换机投影。反向读取按同一纯投影逻辑重建，拒绝篡改派生邻居或“已恢复”字段；解析失败保留缺口，不从空成功构造数据。模拟来源保持sim/protocol_fixture，不能变成真实门禁。协调器、unknown与原配置保留、双端窗口先后及原文/投影一致性的新增用例全部NOT_RUN。接共享Execution的持久会话/原回执/派发重验和真实受控特权适配器仍是下一工作。
