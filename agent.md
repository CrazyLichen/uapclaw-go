# agent.md 索引（包级架构文档）

> 本仓各核心包维护一份 `agent.md`：改编自上游 `openjiuwen/**/AGENTS.md`（Go 侧对齐版）。
> `doc.go` 描述"是什么"（包职责 + 文件树）；`agent.md` 描述"为什么/决策与不变量"。
> 两处与代码不一致时，**以代码为准当场修文档**。

## Agent Teams（领域二十五）

- [internal/openuapclaw/agent_teams/agent.md](internal/openuapclaw/agent_teams/agent.md) — 域根：术语/入口/模块地图/铁律
- [agent_teams/agent/agent.md](internal/openuapclaw/agent_teams/agent/agent.md) — TeamAgent 四象限 + manager 清单
- [agent_teams/agent/coordination/agent.md](internal/openuapclaw/agent_teams/agent/coordination/agent.md) — 唤醒循环与三条铁律
- [agent_teams/agent/coordination/handlers/agent.md](internal/openuapclaw/agent_teams/agent/coordination/handlers/agent.md) — handler 五决策维度与策略
- [agent_teams/agent/coordination/scheduling/agent.md](internal/openuapclaw/agent_teams/agent/coordination/scheduling/agent.md) — 调度引擎十三条不变量
- [agent_teams/harness/agent.md](internal/openuapclaw/agent_teams/harness/agent.md) — NativeHarness 外壳 / pause-abort / 异步工具框架
- [agent_teams/prompts/agent.md](internal/openuapclaw/agent_teams/prompts/agent.md) — 模板目录与编辑硬约束
- [agent_teams/runtime/agent.md](internal/openuapclaw/agent_teams/runtime/agent.md) — 对象池 / 7 路派发 / 并发门禁
- [agent_teams/interaction/agent.md](internal/openuapclaw/agent_teams/interaction/agent.md) — 三视角交互 + HITT
- [agent_teams/tools/agent.md](internal/openuapclaw/agent_teams/tools/agent.md) — 工具契约 / 形态 / i18n / SQLite 并发

## 未覆盖的包

上游 `cli/`（TUI，Go 由 swarm channels 承担）与 `docs/`（文档迁移说明）无对应物、不移植；
`workflow/`（swarmflow，40 文件/11,905 行）⤴️ 25.12 实施时补写 `workflow/agent.md`。
上游原文（Python 真相源）：`~/opensource/agent-core/openjiuwen/agent_teams/**/AGENTS.md`。

## 维护约定

- 改动触及某包契约/不变量时，同改动更新该包 `agent.md`（双向同步铁律，对齐上游）。
- F_NN / B_NN / S_NN 编号沿用上游 `.agents/agent-docs/` 体系，便于与原文互查；
  本仓对应 spec/plan 在 `docs/superpowers/`，进度在 `IMPLEMENTATION_PLAN.md`。
