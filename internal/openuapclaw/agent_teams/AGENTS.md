# Agent Teams（域根）

多智能体团队协作子系统。一个 Leader + 若干 Teammate，通过消息总线与任务数据库协同完成复杂任务。

> 本文件改编自上游 `openjiuwen/agent_teams/AGENTS.md`（Go 侧对齐版）。描述的是 Go 现状；
> 上游独有的、Go 尚未实施的部分以「⤴️ 未实施」标注并注明计划章节。各包细节见同目录 `AGENTS.md` 与 `doc.go`。

本文件是域入口索引，深入细节跳转到：
- `tools/AGENTS.md` — 团队工具设计与描述契约
- `prompts/AGENTS.md` — Prompt 模板与语言切换
- `runtime/AGENTS.md` — 对象池 / 派发 / 并发门禁
- `interaction/AGENTS.md` — 三视角交互 inbox + HITT 表面
- `agent/AGENTS.md` — TeamAgent 四象限分解 + 各 manager
- `agent/coordination/AGENTS.md` · `agent/coordination/handlers/AGENTS.md` · `agent/coordination/scheduling/AGENTS.md` — 唤醒循环 / handler 策略 / 调度引擎
- `harness/AGENTS.md` — NativeHarness 运行时外壳与异步工具框架

## 统一术语

```text
Session
└── Turn          一次外部输入 -> 一次稳定外部输出
    └── Iteration 一次 Agent Loop 控制循环
        └── Step  一次可观测原子执行动作
```

`Round` 只用于 multi-agent 协作/协议阶段，一个 Round 可含多个 Agent Turn。历史 NativeHarness/TaskLoop
中的 `round_id`、`on_round` 属 legacy 名称，新增接口不得复制。三方 Harness 协议独立采用
`Session > Turn > Step`（`internal/openuapclaw/harness_protocol/`）。

## 公开入口

Go 侧无 `__init__.py` 导出面；lifecycle 全走 `TeamAgentSpec.build()` 等价物（`schema/deep_agent_spec.go`
+ `agent/agent_configurator.go` 装配链）与 Runner facade（`internal/swarm` 侧 + `runtime/` 包）。上游
`create_agent_team` / factory wrapper 已删，Go 从未实现。

| 入口 | 用途 |
|---|---|
| `TeamAgentSpec` build（`AgentConfigurator`） | 从零组装 TeamAgent 的唯一公共路径。leader 必填，teammate 可选 |
| Runner team 运行入口（`runtime/team_runner.go` + `runtime/manager.go`） | 运行入口；冷启动 / 热恢复 / 切 session 由 dispatch 表自动选 |
| start streaming（无输入 runtime 启动流） | activate + 流式运行（不投首轮输入、不写记忆 query）；流上原样输出 `team.recovered` 标记帧（kernel start 尾部 leader-only 发射，B_07 / S_01 不变量 17） |

**新增配置项一律走 `TeamAgentSpec`**（`schema/blueprint.go`）——扩参数列表永远是 hack，扩 Spec 才是设计。

## 模块地图（Go）

```
internal/openuapclaw/agent_teams/
├── constants.go         # 保留名（user/team_leader/human_agent）
├── context.go           # session_id 跨成员/跨模式共享（Go 用显式传参替代 contextvars，见 doc.go）
├── i18n.go              # 运行时中英文串 + ReplyHintFor(sender)：文案与选择逻辑集中一处（两个消费点：coordination MessageHandler / external）
├── timefmt.go           # 毫秒 epoch → 绝对本地时间 + 相对差（FormatAbsoluteTime / FormatTimeContext，B_04）
├── inboundtags/         # 入站渲染 <team-inbound>/<team-event>/<team-note>/<team-context>/<team-policy>（叶子包，父包 inbound_render.go 纯转发；D-25.0c-D9 先例）
├── taskrender/          # 任务行/看板渲染 RenderTaskLine/RenderTaskBoard（external 与 coordination handlers 共用的单一真源；D-25.4-1，回收条件=external 拆出无 spawn 依赖的 format 子包）
├── external/            # 外部 CLI 成员接入（format.go 渲染 / client.go / runtime.go CliRuntimeBase / spawn.go）——见 15.13/15.16 系列
├── tracker/             # TeamContextTracker + 任务告知三机制（_TaskNoticeTracker/TaskOutcomeTracker/TaskRevisionTracker，F_119/F_129；Python 单文件因包级循环下沉子包，D-25.0c-D9）
├── message_template*    # 框架模板消息两阶段渲染（发送存意图、投递渲染；F_63；prompts/message_template*.go）
├── schema/              # 全部数据模型（blueprint / deep_agent_spec / team / events / status / task）
├── fsm/                 # 成员/任务状态机（七态统一，含 DORMANT；transitions.go）
├── models/              # 多模型部署原语（pool / allocator）
├── agent/               # 核心运行时（TeamAgent + 装配链 + coordination + scheduling）
├── prompts/             # 系统提示词模板、加载器、section 构造
├── rails/               # 团队 Rail（policy / tool / plan_mode / tool_approval / reliability/ / elements）
├── runtime/             # TeamAgent 对象池 + 派发决策 + 并发门禁（gate）
├── experts/             # Team 专家（AgentGroup / 目录 / 模板成员；25.9 部分）
├── interaction/         # 外部交互入口（UserInbox / HumanAgentInbox / @ 路由）
├── tools/               # 团队工具 + TeamBackend + TaskManager + database/
├── messager/            # 消息传输（inprocess / hybrid）
├── spawn/               # 成员启动（inprocess / external_cli_spawn）
├── monitor/             # TeamMonitor 只读视图 + 流式诊断
├── observability/       # 团队 OTel 观测（team 根 span 按 session 注册；demand 子包）
├── harness/             # NativeHarness 运行时外壳（25.1a-1e）
├── team_workspace/      # 团队共享工作空间（25.7）
├── worktree/            # 团队 worktree 隔离（25.7）
├── mcp/                 # 外部 agent 的 stdio MCP server（team-mcp）
├── memory/ metadata/ sessionctx/ registry/ monitor/  # 支撑面
└── workflow/            # ⤴️ 未实施（25.12，40 文件/11,905 行，单独 brainstorming）
```

上游 `organization/`（跨团队组织、任务池 2798 行）⤴️ 归 25.9；`cli/`（TUI）Go 由 swarm channels 承担，不移植；
`skill/`、`tiny_agent.py` ⤴️ 未排期（见 IMPLEMENTATION_PLAN 领域二十五）。

### agent/ — 运行时主骨架

`TeamAgent`（`agent/team_agent.go`）单一实现承担 Leader/Teammate，按 `TeamRole` 切，内部组合 DeepAgent。
四象限拆分与 manager 清单见 [`agent/AGENTS.md`](agent/AGENTS.md)。

### prompts/

| Go 文件 | 职责 |
|---|---|
| `prompts/loader.go` | `LoadTemplate`，按语言加载 `.md` |
| `prompts/sections.go` · `sections_leader.go` | `BuildTeam*Section` 构造 PromptSection（唯一装配路径）+ leader 侧 `BuildLeaderBootstrapSection`/`BuildLeaderPrebuiltSection`/`BuildLeaderBoundGroupSection`/`BuildLeaderPolicyDisclosure` |
| `prompts/messages.go` | 团队状态消息正文 + `DiffRoster`。纯渲染 |
| `prompts/{cn,en}/` | 语言模板（25.0g 后 34 个/语言） |

模板里的工具一律写**裸名**；外部 CLI 成员经 `<team-policy tools="...">` + `tool_namespace.md` 一次声明归属。
详见 [`prompts/AGENTS.md`](prompts/AGENTS.md)。

### rails/ — 团队 Rail 装配

team rail 经 manifest 声明式装配（F_32）：`AgentConfigurator` 把 rail 作为 RailSpec 注入 `build_spec.rails`，
live handle 经 BuildContext extras 传递，rail 随 `spec.build + ensure_initialized` 自动挂载。Go 关键文件：
`rails/team_policy_rail.go`（F_76 两条装配道 + F_46/F_50/F_52/F_68/F_70/F_71/F_78）、
`rails/team_tool_rail.go`、`rails/team_plan_mode_rail.go`、`rails/tool_approval_rail.go`、
`rails/reliability/`（25.1g）、`rails/elements.go`。`TeamPermissionRail` ⤴️ 平台侧职责未实施。

### schema/ — 数据模型分层

```
blueprint.go        # TeamAgentSpec / LeaderSpec / TransportSpec / StorageSpec
deep_agent_spec.go  # DeepAgentSpec / SubAgentSpec / RailSpec（ResolveParts 已落，25.1b）
team.go             # TeamSpec / TeamRole / TeamLifecycle / TeamMemberSpec
events/             # EventMessage / TeamEvent（task_events.go 含全部 TASK_* 事件）
status.go           # MemberStatus / ExecutionStatus / TaskStatus + 三组成员子集 + TaskReassignableStatuses（F_82）
stream.go、task.go   # 输出归属 / 任务返回模型
```

- `Spec` = 可 JSON 序列化的装配蓝图；不放运行时资源引用。
- **Session checkpoint 状态按 team 分桶**：`state["teams"][team_name] = {spec, context, model_allocator_state, lifecycle, db_state, pending_resume}`；读写走 `runtime/manager.go` 的 namespace 助手（25.2b R-7 交付），不要直接 `update_state`。
- `MemberStatus` 七态流转与三组子集（departed/unreachable、settled、quiescent）**任何两组不合并**（F_74/F_118）。
- leader 流上的四种框架标记 chunk：`team.completed`（关流）/ `team.idle`（全员静止持续 2s 且任务板无非终态任务，不关流，F_77）/ `team.interact.failed` / `team.recovered`（B_07）。`IsTeamEventMarker` 统一判定。

### models/ — 多模型部署原语

`pool.go`（ModelPoolEntry / inherit_pool_ids）+ `allocator.go`（四条分配策略 RoundRobin/ByModelName/Router/IntelliRouter + `BuildModelAllocator`）。
**可靠性只归一层**（S_11 不变量 14）：allocator 或 client router 二选一。`ModelRouterConfig`/`IntelliRouterConfig`
在 build 时展开成 pool；`model_pool`/`model_router`/`model_intelli_router` 三者互斥；空池回退 per-agent 配置。

### messager/ — 消息传输

`Messager` 抽象 + `NewInProcessMessager` / hybrid。**任何直接建 socket / 操作裸 channel 的代码都是错的**，统一经工厂。

### spawn/ — 成员启动

**注册与拉起是两件事**（S_05 不变量 1）：
- 注册：spawn 工具与 predefined 成员只落 `TeamBackend.SpawnMember`——只写 DB 行（UNSTARTED），不启动。
- 拉起：唯一链 `TeamAgent.auto_start → TeamBackend.StartupMember → UNSTARTED→STARTING CAS → spawn`。

六个触发点共用 `TeamBackend.AutostartUnstarted()`（自带 leader 门与回调门；跳过 lifetime 非空的未结束任务成员，B_10）。
CAS 是链上唯一并发闸。模式由 `TeamAgentSpec.spawn_mode` 决定（Go 默认 inprocess；external CLI 走 15.16b spawn）。

### runtime/ — 对象池 + 派发 + 并发门禁

`TeamRuntimePool` + `TeamRuntimeManager` + 7 路 dispatch truth table（`runtime/dispatch.go`）+ `InteractGate`
（`runtime/gate.go`）。详见 [`runtime/AGENTS.md`](runtime/AGENTS.md)。

### interaction/ — 外部交互 + HITT + Bridge

三视角交互 + inbox 实现 + HITT runtime 表面。详见 [`interaction/AGENTS.md`](interaction/AGENTS.md)。

### external/ · mcp/ — 外部 agent 直连

外部 CLI（claude/codex 等）以一等成员直连共享 DB + messager；与 bridge（本地 avatar + relay）正交。
Go 侧见 15.13/15.16 系列 spec。`ExternalTeamClient` 的协同工具协议与 `HarnessProtocol` 的 provider
session/Turn 协议**不要合并**。

### worktree / team_workspace

通用 worktree 在 `internal/openuapclaw/harness/tools/worktree`；team 侧三件事见 `worktree/AGENTS.md`（25.7 交付）。
**worktree 管代码隔离，workspace 管产物协同**，两者独立。

## 架构铁律

1. **Card / Config 分层不可破坏**：Card 是可序列化标识；Config/Manager/Runtime 持运行时状态。
2. **TeamAgent 是单一实现**：Leader/Teammate 靠 `TeamRole` 切 policy，不是两个类。
3. **coordinator 不做决策**：coordination 只管 wake-up，业务行为由 DeepAgent + team tools 驱动。
4. **i18n 两条路径**：运行时硬编码串走 `i18n.go`；prompt/工具描述长文本走 `prompts/` 与 `tools/locales/`。
   调度器交接消息只有模板一份（F_63）——同一消息两处文案必然漂移。
5. **paths.go 是文件系统布局单一真相源**：创建和清理都走这里。
6. **Spec → build → Runtime 单向流**：热更新走 session + resume，不反向污染 Spec。

## 测试

- 单测与包同目录（`*_test.go`）；集成测试在 `tests/integration/agentteams/`（规则 7 两层门禁）。
- 内存后端：`database.InMemoryTeamDatabase` + `messager.NewInProcessMessager` 适合单测。
- 多成员场景优先 inprocess spawn，避免子进程开销。
- 集成测试必须自带 `suite.Run`；审计 `go run ./cmd/integration-audit`。

## 对齐与文档同步（本域强制）

本域是 Python 对齐体量最大的域（583 文件/10.5 万行）。规则 9/12（结构忠实）在此域执行最严。
- 每个包的 `doc.go` 是 Go 侧结构真相源；本目录各 `AGENTS.md` 是**决策与不变量**真相源。
- 读到与代码不一致的描述必须当场修文档（对齐上游 AGENTS.md 双向同步铁律）。
- 设计/计划归 `docs/superpowers/{specs,plans}/`；进度归 `IMPLEMENTATION_PLAN.md`。
