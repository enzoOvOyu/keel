# AI-EXECUTABILITY-AUDIT.md — Keel 文档可执行性索引

基准：PRD / DESIGN-SYSTEM v3.0.3。整理日期：2026-10-23。
当前 Codex 协作约定是 [AGENTS.md](AGENTS.md)，任务入口是 [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)。

## 当前执行检查

| 要避免的误读 | 当前唯一入口 |
|---|---|
| 需求/架构/UI/计划互相覆盖，复制出第二套规则 | DEVELOPMENT 文档分工；PRD 附录 C 与 BASE 差异登记 |
| 用项目级全局变量覆盖另一标签页目标 | PRD FR-17.2；架构 ADR-17；DS ProjectSwitcher/ProjectContext |
| 表格、图表、抽屉和 Shell 各自自由设计 | DS §2/§3/§4/§8；WP-1.8 的共享外壳和组件交付 |
| 忽略加载、无权限、陈旧、失联与 unknown | DS §4.0；相应组件继承集；验收 U 项 |
| 把仿真、部署健康或设计检查当作真实验收 | ACCEPTANCE §1/§11；HANDOFF 当前证据范围 |
| 跳过真实前置或用人工成功状态补链 | 实施计划 §3 依赖；架构 §12 不变式；追踪矩阵 |

`python3 scripts/check-design.py` 检查引用和结构一致性，不能判断实现正确或硬件适配通过。

旧审计全文见 [历史快照](docs/history/2026-10-23/AI-EXECUTABILITY-AUDIT.md)。其中 AGENTS 范例、粘贴模板、旧版本/计数与路径不是现行指令，后续开发无需反复读取。
