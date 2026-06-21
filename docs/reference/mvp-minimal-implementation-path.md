# DeepAgent / AgentTeam / Web 界面 / MCP 最小实现路径分析

> 本文档基于 **Python 源码逐行验证** 分析四个目标功能的真实依赖链，
> 确定最小必需章节集合。很多依赖关系与文档描述不同，以下标注了所有关键差异。
>
> 基于 IMPLEMENTATION_PLAN.md 的当前进度（2025 年初），领域一至四全部完成，
> 领域五部分完成（5.1-5.23 ✅），领域六至十二全部未开始。

---

## 目录

- [1. 关键发现（与文档描述的差异）](#1-关键发现与文档描述的差异)
- [2. DeepAgent 真实依赖树](#2-deepagent-真实依赖树)
- [3. ReActAgent 真实依赖树](#3-reactagent-真实依赖树)
- [4. AgentTeam 真实依赖树](#4-agentteam-真实依赖树)
- [5. Web 界面访问真实依赖树](#5-web-界面访问真实依赖树)
- [6. MCP 配置真实依赖树](#6-mcp-配置真实依赖树)
- [7. 修正后的最小必需章节](#7-修正后的最小必需章节)
- [8. 建议实现顺序（5 个阶段）](#8-建议实现顺序5-个阶段)

---

## 1. 关键发现（与文档描述的差异）

### 1.1 HandoffTeam 完全不需要 Pregel 图引擎

**文档说**：AgentTeam 依赖 8.1-8.9 Pregel 图引擎
**源码真相**：`HandoffTeam` 根本不导入任何 `graph/` 包。它的编排逻辑完全自包含：
- `HandoffOrchestrator` 管理路由图（手动 `Dict[str, Set[str]]`），不用 Pregel
- 消息传递通过 `runtime.publish()`（Pub-Sub），不经过图引擎
- Pregel 图引擎只被 `core/graph/graph.py` 使用，用于 workflow 编排，和 multi_agent 团队系统完全无关

**影响**：8.1-8.9 共 9 个步骤从 AgentTeam MVP 中移除。

### 1.2 TeamAgent 不继承 BaseTeam，是独立体系

**文档说**：9.55 TeamAgent 基于 8.27 BaseTeam
**源码真相**：Python 中存在两套完全独立的"团队"体系：

| 体系 | 位置 | 基类 | 定位 |
|------|------|------|------|
| 核心层 `core/multi_agent/` | `openjiuwen/core/multi_agent/` | BaseTeam (ABC) | 基础设施层 |
| 应用层 `agent_teams/` | `openjiuwen/agent_teams/` | **BaseAgent** | 完整产品级实现 |

`TeamAgent` 继承 `BaseAgent`（不是 BaseTeam），内部通过组合持有 `DeepAgent` 实例。它走的是完全不同的编排路径。

**影响**：MVP 阶段只需实现核心层 `core/multi_agent/`（BaseTeam + HandoffTeam），不需要实现 `agent_teams/TeamAgent` 产品层。

### 1.3 ReActAgent 硬依赖中断系统（6.14-6.16）

**文档说**：6.14-6.16 中断/恢复可以延后
**源码真相**：ReActAgent 顶层 import 了 `ToolInterruptHandler`、`ResumeContext`、`BaseInterruptionState`、`ToolInterruptionState` 等中断模块。中断处理嵌入在 ReAct 循环的核心路径中：
- `_execute_tool_call()` 依赖 `ToolInterruptHandler`
- `_is_interrupted()` 依赖中断状态判断
- `resume()` 方法依赖 `ResumeContext`

**影响**：6.14-6.16 从"可延后"变为"MVP 硬依赖"。

### 1.4 DeepAgent 单轮模式不需要 TaskLoop 全套

**文档说**：9.4-9.7 TaskLoop 全部必需
**源码真相**：DeepAgent 的 `enable_task_loop=False` 时，invoke/stream 直接委托给内部 ReActAgent，完全不需要 TaskLoop 体系。TaskLoop 是 DeepAgent 的高级特性，不是基本运行前提。

**影响**：9.4-9.7 从 P0 降级为 P2（可延后）。9.12 TaskCompletionRail 也随之降级（仅 TaskLoop 启用时自动注入）。

### 1.5 AgentServer 只需 10 个 req_method（非 100+）

**文档说**：10.1.1 ReqMethod 枚举有 ~100 个方法
**源码真相**：Web 核心交互只需 10 个方法：
```
initialize, chat.send, chat.interrupt, chat.resume, chat.user_answer,
history.get, session.list, session.create, session.switch, session.delete
```
其余 90+ 个方法（skills, agents, team, schedule, permissions 等）都是软依赖。

**影响**：Schema 层实现时只需定义核心方法，其余渐进添加。

### 1.6 E2AEnvelope Web 路径只需 10 个字段（非 20+）

**文档说**：E2AEnvelope 需要完整实现
**源码真相**：Web 交互路径只用 10 个核心字段：
```
protocol_version, request_id, channel, session_id, method, params,
is_stream, timestamp, identity_origin, provenance
```
其余字段（auth, a2a_metadata, acp_meta 等）在 Web 路径上均为空/默认值。

**影响**：E2A 协议实现可大幅简化。

### 1.7 MCP 配置的核心阻塞点是 ResourceManager，不是 McpRail

**文档说**：9.16 McpRail 是 MCP 配置的 P0 依赖
**源码真相**：
- McpRail 只提供 MCP **资源浏览**功能（list_resources / read_resource），与 MCP 工具调用无关
- MCP 工具的真正注册路径是 `DeepAgent._register_pending_mcps()` → `Runner.resource_mgr.add_mcp_server()` → `ToolMgr.add_tool_server()`
- 没有 `ToolMgr.add_tool_server()`，MCP 服务器无法连接、发现工具、创建 MCPTool 实例

**影响**：9.16 McpRail 从 P0 降级为软依赖。ResourceManager 的 MCP 相关方法成为新阻塞点。

### 1.8 BaseAgent 元类隐式依赖 Runner

**文档未提及**：`BaseAgent` 的元类 `_AgentMeta.__call__` 在实例化时就会 import Runner 并注册回调。AbilityManager 的工具执行也必须通过 `Runner.resource_mgr` 获取工具实例。

**影响**：Runner（6.25）虽然看似可选，实际是隐式硬依赖。但可以通过接口抽象解耦。

---

## 2. DeepAgent 真实依赖树

### 2.1 DeepAgent 顶层 import 清单

基于 `/home/opensource/agent-core/openjiuwen/harness/deep_agent.py` 源码逐行分析：

#### 核心 agentcore 层 import

| Import | Python 路径 | 领域/步骤 | 硬/软 |
|--------|-----------|-----------|-------|
| `StatusCode, build_error` | `core/common/exception/` | 一 1.3 | **硬** |
| `logger` | `core/common/logging/` | 一 1.5 | **硬** |
| `UserConfig` | `core/common/security/user_config.py` | 一 1.6 | 软（日志脱敏） |
| `Runner` | `core/runner/` | 六 6.25 | **硬**（元类隐式依赖） |
| `Session` | `core/session/agent.py` | 五 5.x | **硬** |
| `InteractiveInput` | `core/session/interaction/interactive_input.py` | 五 5.7 | **硬** |
| `StreamMode` | `core/session/stream/base.py` | 五 5.10 | **硬** |
| `ReActAgent, ReActAgentConfig` | `core/single_agent/agents/react_agent.py` | 六 6.11 | **硬** |
| `BaseAgent` | `core/single_agent/base.py` | 六 6.2 | **硬** |
| `AgentCallbackContext, AgentCallbackEvent, AgentRail, InvokeInputs, RunKind, RunContext` | `core/single_agent/rail/base.py` | 六 6.4-6.7 | **硬** |
| `AgentCard` | `core/single_agent/schema/agent_card.py` | 六 6.1 | **硬** |
| `ToolCard` | `core/foundation/tool/` | 三 3.x ✅ | **硬** |
| `BaseMessage, SystemMessage` | `core/foundation/llm/` | 二 2.x ✅ | **硬** |
| `ControllerConfig` | `core/controller/config.py` | 六 6.19 | 软（仅 TaskLoop） |
| `ContextEngine` | `core/context_engine/` | 五 5.30 | **硬** |
| `ContextUtils` | `core/context_engine/context/context_utils.py` | 五 5.x ✅ | **硬** |
| `TaskInteractionEvent, FollowUpEvent` | `core/controller/schema/event.py` | 六 6.20 | 软（仅 TaskLoop） |

#### Harness 应用层 import

| Import | 步骤 | 硬/软 |
|--------|------|-------|
| `DeepAgentRail` | 9.1 | **硬** |
| `TaskCompletionRail` | 9.12 | 软（仅 TaskLoop 启用时） |
| `DeepAgentConfig` | 9.2 | **硬** |
| `TaskLoopEventHandler` | 9.4 | 软（仅 TaskLoop 启用时） |
| `DEEP_TASK_TYPE, build_deep_executor` | 9.6 | 软（仅 TaskLoop 启用时） |
| `LoopCoordinator` | 9.5 | 软（仅 TaskLoop 启用时） |
| `DeepAgentState` | 9.51 | 软（仅 TaskLoop 启用时） |
| `LoopQueues` | 9.4 | 软（仅 TaskLoop 启用时） |
| `HarnessConfigLoader` | 9.2 | 软（仅 load_harness_config 时） |
| `TaskLoopController` | 9.4 | 软（仅 TaskLoop 启用时） |
| `ProgressiveToolRail` | 9.11 | 软（progressive_tool_enabled 时） |
| `build_permission_interrupt_rail` | 9.9 | 软（permissions.enabled 时） |
| `SessionToolkit` | 9.38-49 | 软（enable_async_subagent 时） |
| `resolve_language, resolve_mode, PromptSection, SystemPromptBuilder` | 9.51 | **硬** |
| `SectionName` | 9.51 | **硬** |
| `build_identity_section` | 9.51 | **硬** |
| `Workspace` | 9.50 | 软（workspace 配置时） |

### 2.2 DeepAgent 单轮模式（enable_task_loop=False）的硬依赖

```
DeepAgent (9.1)
├── [硬] BaseAgent (6.2)
│   ├── [硬] AgentCard (6.1)
│   ├── [硬] AgentCallbackManager (6.6)
│   ├── [硬] AbilityManager (已有部分实现)
│   ├── [硬] Runner (6.25) — 元类隐式依赖
│   ├── [硬] ContextEngine (5.30) — 已有接口定义
│   └── [硬] Session (5.x) ✅
│
├── [硬] ReActAgent (6.11) — 详见第3节
│
├── [硬] AgentRail 体系 (6.4-6.7)
│   ├── [硬] AgentCallbackEvent / AgentCallbackContext
│   ├── [硬] InvokeInputs / RunKind / RunContext
│   └── [硬] ModelCallInputs / ToolCallInputs
│
├── [硬] DeepAgentConfig (9.2)
│   ├── [硬] Model ✅ + AgentCard (6.1) + AgentRail (6.7)
│   ├── [硬] ToolCard ✅ / McpServerConfig ✅
│   ├── [软] SysOperation (9.32) — 仅 sys_operation 字段
│   ├── [软] Workspace (9.50) — 仅 workspace 字段
│   └── [软] AgentMode (9.14) / PermissionsSection (9.9)
│
├── [硬] DeepAgentRail (9.1) — 继承 AgentRail
│
├── [硬] Prompt 体系 (9.51)
│   ├── [硬] SystemPromptBuilder
│   ├── [硬] PromptSection / SectionName
│   ├── [硬] build_identity_section
│   └── [硬] resolve_language / resolve_mode
│
└── [软] ProgressiveToolRail (9.11) — progressive_tool_enabled 时
    └── [软] LoadToolsTool / SearchToolsTool (9.38-49)
```

### 2.3 TaskLoop 模式（enable_task_loop=True）新增硬依赖

```
新增硬依赖：
├── [硬] LoopCoordinator (9.5) — 含 StopConditionEvaluator 体系
├── [硬] TaskLoopController (9.4) — 依赖 Controller (6.19)
│   └── [硬] TaskManager / TaskScheduler / EventQueue (6.20-6.21)
├── [硬] TaskLoopEventHandler (9.4) — 依赖 EventHandler (6.21)
├── [硬] TaskLoopEventExecutor (9.6) — 依赖 TaskExecutor (6.21)
├── [硬] LoopQueues (9.4)
├── [硬] DeepAgentState (9.51) — 含 TaskPlan / PlanModeState
└── [硬] TaskCompletionRail (9.12) — 自动注入
```

### 2.4 全部 Rails 依赖条件

| Rail | 步骤 | 触发条件 | 硬/软 |
|------|------|---------|-------|
| TaskCompletionRail | 9.12 | `enable_task_loop=True` 时自动注入 | 软 |
| ProgressiveToolRail | 9.11 | `progressive_tool_enabled=True` | 软 |
| PermissionInterruptRail | 9.9 | `permissions.enabled=True` | 软 |
| McpRail | 9.16 | config.rails 传入 | **软** |
| SecurityRail | 9.19 | config.rails 传入 | 软 |
| SkillUseRail | 9.19 | harness_config 加载 skills 时 | 软 |
| SubagentRail | 9.19 | config.rails 传入 | 软 |
| TaskPlanningRail | 9.13 | config.rails 传入 | 软 |
| AgentModeRail | 9.14 | config.rails 传入 | 软 |
| HeartbeatRail | 9.15 | config.rails 传入 | 软 |
| LspRail | 9.17 | config.rails 传入 | 软 |
| SysOperationRail | 9.18 | config.rails 传入 | 软 |
| MemoryRail / CodingMemoryRail | 9.19 | config.rails 传入 | 软 |
| 所有其他 Rails | 9.19 | config.rails 传入 | 软 |

**关键**：DeepAgent 的所有 Rails 都是选择性注册的，**唯一自动注入的 Rail 是 TaskCompletionRail**（且仅 TaskLoop 启用时）。MVP 不启用 TaskLoop 则不需要任何 Rail。

---

## 3. ReActAgent 真实依赖树

基于 `/home/opensource/agent-core/openjiuwen/core/single_agent/agents/react_agent.py`（1617 行）逐行分析：

### 3.1 顶层 import 中的硬依赖

| Import | Python 路径 | 功能 | 硬/软 |
|--------|-----------|------|-------|
| `BaseError` | `core/common/exception/errors` | 异常 | **硬** |
| `logger` | `core/common/logging` | 日志 | **硬** |
| `UserConfig` | `core/common/security/user_config` | 日志脱敏 | 软 |
| `PromptTemplate` | `core/foundation/prompt` | 提示词模板渲染 | **硬** |
| `ModelClientConfig, ModelRequestConfig` | `core/foundation/llm/schema/config` | LLM 配置 | **硬** |
| `ContextEngine, ContextEngineConfig, ModelContext` | `core/context_engine` | 上下文管理 | **硬** |
| `AssistantMessage, Model, ToolMessage, UserMessage, SystemMessage` | `core/foundation/llm` | LLM 消息 | **硬** |
| `ToolInfo` | `core/foundation/tool` | 工具描述 | **硬** |
| `with_session` | `core/session` | 会话装饰器 | **硬** |
| `Session, create_agent_session` | `core/session/agent` | 会话管理 | **硬** |
| `OutputSchema` | `core/session/stream` | 流式输出 | **硬** |
| `StreamMode` | `core/session/stream/base` | 流式模式 | **硬** |
| `BaseAgent` | `core/single_agent/base` | Agent 基类 | **硬** |
| `ToolInterruptHandler, ResumeContext` | `core/single_agent/interrupt/handler` | 工具中断处理 | **硬** |
| `BaseInterruptionState, ToolInterruptionState, RESUME_START_ITERATION_KEY, INTERRUPTION_KEY` | `core/single_agent/interrupt/state` | 中断状态 | **硬** |
| `AgentCallbackEvent, AgentCallbackContext, InvokeInputs, ModelCallInputs, rail` | `core/single_agent/rail/base` | Rail 回调 | **硬** |
| `PromptSection, SystemPromptBuilder` | `core/single_agent/prompts/builder` | 提示词构建 | **硬** |
| `AgentCard` | `core/single_agent/schema/agent_card` | Agent 卡片 | **硬** |

### 3.2 延迟 import 分析

| Import | 触发条件 | 硬/软 |
|--------|---------|-------|
| `Runner` | sys_operation_id 配置 / enable_reload 时 / 元类初始化 | **硬**（元类隐式） |
| `InteractiveInput` | ReAct 循环用户输入处理 | **硬** |
| `WorkflowOutput, WorkflowExecutionState` | 工作流中断检测 | 软 |
| `StatusCode, build_error` | 技能警告日志 | 软 |

### 3.3 ReAct 循环核心依赖链

```
ReActAgent.invoke() / _inner_invoke()
├── AgentCallbackContext.lifecycle(BEFORE_INVOKE, AFTER_INVOKE)  — rail.base
├── _init_context(session)
│   ├── ContextEngine.create_context()                           — context_engine ⚠️ 5.30
│   └── ContextEngine.get_context() + reloader_tool()            — context_engine
├── _build_rendered_system_prompt(inputs)
│   ├── SystemMessage                                            — foundation.llm ✅
│   └── PromptTemplate.format()                                  — foundation.prompt ✅
├── SystemPromptBuilder.add_section() / build()                  — prompts.builder 6.29
├── AbilityManager.list_tool_info()                              — ability_manager ✅
│   └── ToolInfo                                                 — foundation.tool ✅
├── for iteration in max_iterations:
│   ├── _call_model(ctx, context, tools)
│   │   ├── _railed_model_call(ctx)  [@rail decorator]           — rail.base 6.7
│   │   │   ├── ModelContext.get_context_window()                 — context_engine
│   │   │   ├── Model.invoke() / Model.stream()                   — foundation.llm ✅
│   │   │   └── Session.write_stream(OutputSchema)                — session ✅
│   │   └── AssistantMessage                                      — foundation.llm ✅
│   ├── context.add_messages(AssistantMessage/ToolMessage/UserMessage) — context_engine
│   ├── _execute_tool_call(ctx, tool_calls, session, context)
│   │   ├── ToolInterruptHandler.handle_interrupt()               — interrupt ⚠️ 6.14
│   │   ├── AbilityManager.execute()                              — ability_manager ✅
│   │   │   └── Runner.resource_mgr.get_tool/get_workflow/get_agent — runner ⚠️ 6.25
│   │   └── context.add_messages(ToolMessage)                    — context_engine
│   └── ContextEngine.save_contexts(session)                     — context_engine
└── with_session() 装饰器                                         — session ✅
```

### 3.4 中断系统硬依赖（MVP 不可省略）

```
ToolInterruptHandler (6.14)
├── ToolInterruptException (interrupt/exception)
│   └── AgentInterrupt (session/interaction/base)
│       └── InterruptRequest (interrupt/response)
├── ToolCallInterruptRequest (interrupt/response)
├── ToolInterruptionState / ToolInterruptEntry (interrupt/state 6.15)
│   └── BaseInterruptionState (interrupt/state)
│       └── AssistantMessage, ToolCall (foundation.llm)
├── ResumeContext (interrupt/handler 6.16)
├── OutputSchema (session/stream/base) ✅
├── InteractionOutput (session/interaction/interaction) ✅
└── InteractiveInput (session/interaction/interactive_input) ✅
```

**关键**：中断处理嵌入在 `_execute_tool_call()` 的核心路径中。即使不使用人机交互（HITL），`_is_interrupted()` 的判断逻辑仍需存在，否则编译不过。

---

## 4. AgentTeam 真实依赖树

### 4.1 两套独立体系

| 体系 | Python 路径 | 基类 | 定位 | MVP 需要？ |
|------|-----------|------|------|-----------|
| 核心层 | `core/multi_agent/` | BaseTeam (ABC) | 基础设施层 | **是** |
| 应用层 | `agent_teams/` | BaseAgent | 完整产品级 | **否**（远期） |

### 4.2 HandoffTeam 真实依赖（无需 Pregel）

```
HandoffTeam (8.34) extends BaseTeam (8.27)
├── BaseTeam (8.27)
│   ├── [硬] TeamCard (8.28) — 继承 BaseCard，含 agent_cards 列表
│   ├── [硬] TeamConfig (8.28) — max_agents/timeout/concurrency
│   ├── [硬] TeamRuntime (8.30) — 运行时引擎
│   │   ├── [硬] MessageBus (8.33) — 消息总线
│   │   │   ├── [硬] MessageQueueInMemory (6.27) — 内存队列 ⚠️ 来自 runner
│   │   │   ├── [硬] SubscriptionManager (8.32) — 订阅管理（自包含）
│   │   │   └── [硬] MessageRouter (8.32) — 消息路由
│   │   │       └── [硬] Runner.run_agent() (6.25) — ⚠️ 关键耦合，需抽象
│   │   └── [硬] CommunicableAgent (8.31) — Agent 通信 Mixin
│   ├── [硬] Session (agent_team) — 团队会话
│   └── [硬] StatusCode + build_error + logger — 基础设施 ✅
│
├── [硬] HandoffTeamConfig (8.28) — extends TeamConfig
├── [硬] ContainerAgent (8.31) — extends CommunicableAgent + BaseAgent
│   ├── [硬] HandoffTool — extends Tool ✅
│   ├── [硬] HandoffSignal / extract_handoff_signal — 纯函数
│   ├── [硬] TeamInterruptSignal / extract_interrupt_signal
│   │   └── AgentInterrupt (session/interaction/base) ✅
│   └── [硬] Runner — 注入 HandoffTool 到 ResourceManager ⚠️ 需抽象
├── [硬] HandoffOrchestrator — 自包含协调器（手动 Dict 路由，不用 Pregel）
├── [硬] HandoffRequest — 纯数据类
└── [硬] standalone_invoke_context / standalone_stream_context
    └── Session / create_agent_team_session
```

### 4.3 核心层不需要的模块

| 模块 | 原因 |
|------|------|
| **8.1-8.9 Pregel 图引擎** | HandoffTeam 不使用图引擎，编排逻辑自包含 |
| **8.10-8.13 图辅助** | 与 Pregel 捆绑 |
| **8.14-8.26 工作流组件** | 与 Pregel 捆绑 |
| **8.29 EventDrivenTeamCard** | 非核心 |
| **8.35-8.36 HierarchicalTeam** | 可后置 |
| **9.55-9.69 TeamAgent 产品层** | 独立体系，基于 BaseAgent 而非 BaseTeam |

### 4.4 需要抽象解耦的 Runner 依赖

TeamRuntime 和 ContainerAgent 都通过延迟 import 耦合 Runner：
1. `MessageRouter` 调用 `Runner.run_agent()` — 需抽象为 `AgentExecutor` 接口
2. `TeamRuntime` 使用 `Runner.resource_mgr` — 需抽象为 `AgentRegistry` 接口
3. `ContainerAgent` 使用 `Runner.resource_mgr.add_tool()` — 同上

Go 实现中建议定义：
```go
// AgentExecutor 替代 Runner.run_agent()
type AgentExecutor interface {
    RunAgent(ctx context.Context, agent Agent, inputs map[string]any, opts ...Option) (any, error)
}

// AgentRegistry 替代 Runner.resource_mgr
type AgentRegistry interface {
    GetTool(toolID string) (Tool, error)
    AddTool(tool Tool) error
    GetAgent(agentID string) (Agent, error)
}
```

---

## 5. Web 界面访问真实依赖树

### 5.1 完整链路

```
浏览器 ←WebSocket→ WebChannel ←→ ChannelManager ←→ MessageHandler ←E2A→ AgentWebSocketServer ←→ AgentManager ←→ Agent
```

### 5.2 最小必需的 req_method（10 个，非 100+）

| req_method | 功能 | 必需性 |
|------------|------|--------|
| `initialize` | 初始化 Agent | **硬** |
| `chat.send` | 发送用户消息（核心交互） | **硬** |
| `chat.interrupt` | 中断当前对话 | **硬** |
| `chat.resume` | 恢复对话 | **硬** |
| `chat.user_answer` | 用户回答 Agent 提问 | **硬** |
| `history.get` | 获取历史记录 | **硬** |
| `session.create` | 创建会话 | **硬** |
| `session.switch` | 切换会话 | **硬** |
| `session.list` | 列出会话 | **硬** |
| `session.delete` | 删除会话 | **硬** |

### 5.3 最小必需的 EventType（11 个）

```
connection.ack, chat.delta, chat.final, chat.error,
chat.processing_status, chat.interrupt_result, chat.tool_call, chat.tool_result,
history.message, chat.usage_summary, chat.file
```

### 5.4 E2A 协议 Web 路径只需 10 个核心字段

E2AEnvelope 必需字段：
```
protocol_version, request_id, channel, session_id, method, params,
is_stream, timestamp, identity_origin, provenance
```

不需要的字段（Web 路径为空/默认值）：
```
auth, bearer_token, a2a_metadata, acp_meta, ext_method,
session_update_kind, expected_output_modes
```

### 5.5 Web 界面本地处理器（Gateway 侧，8 个）

| 方法 | 功能 |
|------|------|
| `config.get` | 读取配置 |
| `config.set` | 修改配置 |
| `config.save_all` | 批量保存配置 |
| `models.list` | 列出可用模型 |
| `session.list` | 列出会话 |
| `session.delete` | 删除会话 |
| `path.get` | 工作空间路径 |
| `path.set` | 设置工作空间路径 |

### 5.6 依赖树

```
Web 界面访问
├── [硬] Schema 层 (10.1)
│   ├── ReqMethod — 只定义 10 个常量
│   ├── EventType — 只定义 11 个常量
│   ├── Mode — 6 个常量
│   ├── Message — 统一消息结构
│   ├── AgentRequest / AgentResponse — 请求响应模型
│   ├── AgentResponseChunk — 流式片段
│   ├── HookEventBase — 钩子事件基类
│   └── PermissionContext — 权限上下文
│
├── [硬] E2A 协议栈 (10.2)
│   ├── E2AEnvelope — 10 个核心字段
│   ├── E2AResponse
│   ├── E2AProvenance
│   ├── gateway_normalize — Message→E2A, AgentResponse→E2A
│   ├── wire_codec — 线编解码
│   ├── agent_compat — E2A→AgentRequest
│   └── constants
│
├── [硬] Gateway 核心 (11.x)
│   ├── BaseChannel (11.1) — 通道抽象
│   ├── ChannelManager (11.2) — 通道管理
│   ├── MessageHandler (11.3) — 消息转发
│   ├── WebSocketAgentServerClient (11.5) — WS 客户端
│   ├── GatewayServer (11.9) — WS 服务器组装
│   └── Web 通道 (11.14) — WebSocket + HTTP RPC
│
├── [硬] AgentServer 核心 (10.3)
│   ├── AgentWebSocketServer (10.3.1) — 10 个方法分发
│   ├── JiuWenClaw 门面 (10.3.2) — SDK 路由
│   ├── AgentAdapter 接口与工厂 (10.3.3)
│   ├── AgentManager (10.3.12) — Agent 管理
│   ├── SessionManager (10.3.15)
│   └── GatewayPush Transport (10.3.21-22)
│
├── [硬] 交互入口 (10.4)
│   ├── CLI 聊天模式 (10.4.1)
│   └── HTTP API (10.4.2)
│
├── [硬] CLI 启动
│   ├── 统一启动器 (12.7)
│   └── Web UI 启动 (12.10)
│
└── [硬] Agent 运行时
    └── ReActAgent (6.11) — 详见第3节
```

### 5.7 软依赖（可延后）

| 模块 | 步骤 | 影响 |
|------|------|------|
| ACP 适配器 | 10.2.8-10 | ACP 协议桥接 |
| A2A 适配器 | 10.2.9-10 | A2A 协议适配 |
| 模式适配器 (Agent/Code/Deep) | 10.3.4-6 | MVP 只需 Agent 模式 |
| 适配器辅助 | 10.3.7-11 | 与模式适配器捆绑 |
| AgentConfigService / TenantAgentPool | 10.3.13-14 | 配置和多租户 |
| 会话历史/元数据/重命名 | 10.3.16-18 | 高级会话管理 |
| 技能管理 (Server) | 10.3.19-20 | 服务端技能 |
| 服务端辅助 | 10.3.23-26 | 可简化 |
| ACP Stdio / Slash 命令 | 10.4.3-5 | 特定交互入口 |
| Slash Command Parser | 11.4 | 命令解析 |
| RouteBinding / SessionMap / InteractionContext | 11.6-8 | 路由细节 |
| Cron / 心跳 / IM Pipeline / Hook | 11.10-13 | 服务端增强 |
| IM 渠道（9 种） | 11.15-11.26 | MVP 只需 Web 通道 |

---

## 6. MCP 配置真实依赖树

### 6.1 运行时调用路径

```
路径 A: DeepAgent 配置式注册（主路径）
1. DeepAgentConfig.mcps = [McpServerConfig(...), ...]
2. DeepAgent._register_pending_mcps()
3.   → Runner.resource_mgr.add_mcp_server(mcp_config)        ← ⚠️ 5.x ResourceManager
4.     → ToolMgr.add_tool_server(config)                      ← ⚠️ 5.x ResourceManager
5.       → McpClient.connect()                                 ← ✅ 3.6
6.       → McpClient.list_tools()                              ← ✅ 3.6
7.       → MCPTool(mcp_client=client, tool_info=card)          ← ✅ 3.5
8.   → ability_manager.add(mcp_config)                         ← ✅ 已实现

路径 D: MCP 工具执行
1. LLM 返回 tool_call(name="mcp_{server}_{tool}", arguments={...})
2. AbilityManager.execute() → railedExecuteSingleToolCall()
3.   → Runner.resource_mgr.get_tool(tool_id, tag, session)     ← ⚠️ 5.x ResourceManager
4.     → MCPTool.invoke()
5.       → mcpClient.call_tool(tool_name, arguments)           ← ✅ 3.6
6.       → extract_mcp_tool_result_content(result)             ← ✅ 3.5
```

### 6.2 依赖树

```
MCP 配置运行时功能
│
├── [硬] 数据模型层 ✅ 已完成
│   ├── McpServerConfig (3.7 ✅)
│   ├── McpToolCard (3.5 ✅)
│   ├── MCPTool (3.5 ✅)
│   ├── McpClient 接口 (3.6 ✅)
│   ├── 5 种客户端实现 (3.6 ✅)
│   ├── ToolCard (3.1 ✅) / ToolInfo (3.2 ✅)
│   └── SchemaUtils (3.9 ✅)
│
├── [硬] 运行时注册层 ⚠️ 未实现
│   ├── ToolMgr.add_tool_server()               ← 5.x ResourceManager / 6.23 ResourceMgr
│   │   ├── McpClient.connect()
│   │   ├── McpClient.list_tools()
│   │   ├── MCPTool 实例创建
│   │   └── McpServerResource 缓存
│   ├── ResourceManager.add_mcp_server()         ← 5.x ResourceManager / 6.23 ResourceMgr
│   ├── ResourceManager.get_mcp_tool()           ← 同上
│   └── ResourceManager.get_mcp_tool_infos()     ← 同上
│
├── [硬] Agent 注册入口 ⚠️ 未实现
│   └── DeepAgent._register_pending_mcps()       ← 9.x DeepAgent
│       ├── Runner.resource_mgr.add_mcp_server()
│       └── ability_manager.add(mcp_config) ✅
│
├── [硬] 工具发现/执行层 ⚠️ 部分实现
│   ├── AbilityManager.add(McpServerConfig) ✅
│   ├── AbilityManager.list_tool_info() MCP 分支 ⤵️ (预留)
│   └── AbilityManager.execute() MCP 路径
│       └── Runner.resource_mgr.get_tool() → MCPTool.invoke()
│
├── [软] MCP 资源浏览
│   ├── McpRail (9.16) — 仅 list_resources / read_resource
│   ├── ListMcpResourcesTool (9.38-49)
│   └── ReadMcpResourceTool (9.38-49)
│
├── [软] 配置加载
│   ├── DeepAgentConfig.mcps 字段 (9.2)
│   ├── HarnessConfig YAML mcps (9.51-53)
│   └── CLI mcp.json 加载 (10.x)
│
└── [软] 高级功能
    ├── Playwright MCP 集成
    ├── ToolMgr.refresh_tool_server (自动刷新)
    └── ToolMgr.remove_tool_server (动态移除)
```

### 6.3 关键结论

1. **McpRail (9.16) 完全是软依赖**。它只提供 MCP 资源浏览（list/read resource），与 MCP 工具调用（call_tool）无关。
2. **核心阻塞点是 ResourceManager/ToolMgr 的 MCP 方法**（6.23 ResourceMgr）。没有它，MCP 服务器无法在运行时连接、发现工具、创建 MCPTool 实例。
3. **3.5-3.7 只是数据模型和客户端**，无法独立运转。真正的运行时注册逻辑在 ResourceManager 中。

---

## 7. 修正后的最小必需章节

### 7.1 与原文档对比的差异表

| 章节 | 原文档 | 修正后 | 原因 |
|------|--------|--------|------|
| 5.30-5.31 ContextEngine 门面 | P0 | **P0** | 不变 |
| 6.1 AgentCard/AgentResult | P0 | **P0** | 不变 |
| 6.2 BaseAgent 接口 | P0 | **P0** | 不变 |
| 6.3 ReActAgentConfig | P0 | **P0** | 不变 |
| 6.4-6.6 回调框架 | P0 | **P0** | 不变 |
| 6.7-6.10 Rail 系统 | P0 | **P0** | 不变 |
| **6.11 ReActAgent** | **P0** | **P0** | 不变（全局最大阻塞点） |
| **6.14-6.16 中断/恢复** | **P1 可延后** | **P0 硬依赖** | 源码验证：嵌入在 ReAct 循环核心路径 |
| 6.12 流式输出 | P0 | **P0** | 不变 |
| 6.17-6.18 Skill 系统 | P0 | **P1** | DeepAgent 单轮模式不强制依赖 |
| 6.25 Runner | 未列出 | **P0** | 元类隐式依赖，需抽象接口解耦 |
| 6.27 MessageQueueInMemory | 未列出 | **P0** | AgentTeam MessageBus 硬依赖 |
| 6.29 Agent Prompts | P1 | **P0** | ReActAgent 硬依赖 SystemPromptBuilder |
| **9.1 DeepAgent** | **P0** | **P0** | 不变 |
| **9.2 DeepAgentConfig** | **P0** | **P0** | 不变 |
| **9.3 DeepAgent Factory** | **P0** | **P0** | 不变 |
| **9.4-9.7 TaskLoop** | **P0** | **P2 可延后** | enable_task_loop=False 时不需要 |
| **9.11 ProgressiveToolRail** | **P0** | **软依赖** | progressive_tool_enabled=False 时不需要 |
| **9.12 TaskCompletionRail** | **P1** | **软依赖** | 仅 TaskLoop 启用时自动注入 |
| **9.16 McpRail** | **P0** | **软依赖** | 仅提供资源浏览，不影响 call_tool |
| **9.51 Prompt/Schema** | 未列出 | **P0** | DeepAgent 硬依赖 SystemPromptBuilder |
| **8.1-8.9 Pregel 图引擎** | **P0** | **不需要** | HandoffTeam 不使用 Pregel |
| 8.27 BaseTeam | P0 | **P0** | 不变 |
| 8.28 TeamCard/TeamConfig | P0 | **P0** | 不变 |
| 8.30-8.33 TeamRuntime | P0 | **P0** | 不变 |
| **8.34 HandoffTeam** | P0 | **P0** | 不变 |
| **9.55-9.60 TeamAgent** | **P0** | **不需要** | TeamAgent 继承 BaseAgent 非 BaseTeam，属应用层 |
| **6.23 ResourceMgr** | 未列出 | **P0** | MCP 注册硬依赖 ToolMgr.add_tool_server() |

### 7.2 修正后的最小必需步骤统计

| 模块 | 原文档步骤数 | 修正后步骤数 | 变化 |
|------|------------|------------|------|
| 5.30-5.31 上下文引擎 | 2 | 2 | 不变 |
| 6.x Agent 核心 | 13 | **17** | +6.14-6.16 中断系统, +6.25 Runner, +6.27 消息队列, +6.29 提示词 |
| 9.x DeepAgent 核心 | 7 | **4** | -9.4-9.7 TaskLoop |
| 9.51 Prompt/Schema | 0 | **1** | 新增 |
| 9.x 内置工具 | 3 | **3** | 不变 |
| 8.x Pregel 图引擎 | **9** | **0** | 移除 |
| 8.x 团队框架 | 8 | 8 | 不变 |
| 9.x TeamAgent 产品层 | **5** | **0** | 移除 |
| 10.x Schema+E2A | 15 | **15** | 不变 |
| 10.x AgentServer | 7 | 7 | 不变 |
| 10.x 交互入口 | 2 | 2 | 不变 |
| 11.x Gateway | 6 | 6 | 不变 |
| 12.x CLI 启动 | 2 | 2 | 不变 |
| **总计** | **~79** | **~67** | -12（移除 Pregel 和 TeamAgent 产品层） |

---

## 8. 建议实现顺序（5 个阶段）

### 阶段一：让 Agent 能跑起来（核心路径）

**目标**：ReActAgent 可以执行 Think→Act→Observe 循环

| 顺序 | 章节 | 内容 | 预估复杂度 | 说明 |
|------|------|------|-----------|------|
| 1 | 5.30 | ContextEngine 门面 | 中 | |
| 2 | 5.31 | Context 实现 | 高 | |
| 3 | 6.1 | AgentCard / AgentResult 模型 | 低 | |
| 4 | 6.2 | BaseAgent 接口（扩展已有最小接口） | 低 | |
| 5 | 6.3 | ReActAgentConfig | 低 | |
| 6 | 6.4 | AgentCallbackEvent 枚举 | 低 | |
| 7 | 6.5 | AgentCallbackContext | 低 | |
| 8 | 6.6 | AgentCallbackManager | 中 | |
| 9 | 6.7 | AgentRail 接口 | 中 | 已有 ToolRail 预留 |
| 10 | 6.10 | ForceFinishRequest / RetryRequest | 低 | |
| 11 | 6.14 | ToolInterruptHandler | 中 | **源码验证：硬依赖** |
| 12 | 6.15 | InterruptionState / ToolInterruptionState | 低 | |
| 13 | 6.16 | ResumeContext | 低 | |
| 14 | **6.11** | **ReActAgent 实现** | **高** | 核心 |
| 15 | 6.12 | 流式输出 | 中 | |
| 16 | 6.25 | Runner（最小实现） | 高 | 元类隐式依赖，需抽象接口 |
| 17 | 6.29 | Agent Prompts | 中 | SystemPromptBuilder |

**验证点**：完整 ReAct 循环可用：用户提问 → Agent 思考 → 调用工具 → 返回结果

### 阶段二：DeepAgent + MCP 基本可用

**目标**：DeepAgent 可执行任务，MCP 工具可配置使用

| 顺序 | 章节 | 内容 | 预估复杂度 |
|------|------|------|-----------|
| 1 | 9.1 | DeepAgent | 高 |
| 2 | 9.2 | DeepAgentConfig | 低 |
| 3 | 9.3 | DeepAgent Factory | 中 |
| 4 | 9.51 | Prompt/Schema（SystemPromptBuilder + SectionName + build_identity_section） | 中 |
| 5 | 6.23 | ResourceMgr（含 ToolMgr.add_tool_server MCP 注册） | 高 |
| 6 | 9.38 | Shell 工具 | 中 |
| 7 | 9.39 | 文件系统工具 | 中 |

**验证点**：DeepAgent 单轮可用，MCP 工具可配置使用

### 阶段三：AgentServer + CLI/HTTP 入口

**目标**：用户可通过 CLI 和 HTTP API 与 Agent 交互

| 顺序 | 章节 | 内容 | 预估复杂度 |
|------|------|------|-----------|
| 1 | 10.1.1-8 | Schema 层（只需 10 个 req_method + 11 个 event） | 中 |
| 2 | 10.2.1-7 | E2A 协议核心（只需 10 个核心字段） | 高 |
| 3 | 10.3.1 | AgentWebSocketServer | 高 |
| 4 | 10.3.2 | JiuWenClaw 门面 | 高 |
| 5 | 10.3.3 | AgentAdapter 接口与工厂 | 中 |
| 6 | 10.3.12 | AgentManager | 中 |
| 7 | 10.3.15 | SessionManager | 中 |
| 8 | 10.3.21-22 | GatewayPush Transport | 中 |
| 9 | 10.4.1 | CLI 聊天模式 | 中 |
| 10 | 10.4.2 | HTTP API | 中 |

**验证点**：用户运行 `uapclaw chat` 即可与 Agent 对话

### 阶段四：Web 界面访问

**目标**：用户可通过浏览器访问 Agent

| 顺序 | 章节 | 内容 | 预估复杂度 |
|------|------|------|-----------|
| 1 | 11.1 | BaseChannel 接口 | 低 |
| 2 | 11.2 | ChannelManager | 中 |
| 3 | 11.3 | MessageHandler | 中 |
| 4 | 11.5 | WebSocketAgentServerClient | 高 |
| 5 | 11.9 | GatewayServer | 高 |
| 6 | 11.14 | Web 通道 | 高 |
| 7 | 12.7 | 统一启动器 | 中 |
| 8 | 12.10 | Web UI 启动 | 低 |

**验证点**：用户打开浏览器即可与 Agent 交互

### 阶段五：AgentTeam

**目标**：多 Agent 可通过 HandoffTeam 协作完成任务（无需 Pregel）

| 顺序 | 章节 | 内容 | 预估复杂度 |
|------|------|------|-----------|
| 1 | 6.27 | LocalMessageQueue | 中 | MessageBus 硬依赖 |
| 2 | 8.32 | SubscriptionManager | 低 | fnmatch 通配符匹配 |
| 3 | 8.33 | MessageBus | 高 | 消息总线核心 |
| 4 | 8.32 | MessageRouter | 中 | 需抽象 Runner 依赖 |
| 5 | 8.31 | CommunicableAgent | 中 | Agent 通信 Mixin |
| 6 | 8.30 | TeamRuntime | 高 | 运行时引擎 |
| 7 | 8.28 | TeamCard / TeamConfig | 低 | |
| 8 | 8.27 | BaseTeam 接口 | 中 | |
| 9 | 8.34 | HandoffTeam | 高 | 含 ContainerAgent + HandoffTool + HandoffOrchestrator |

**验证点**：可定义和执行多 Agent HandoffTeam 协作任务

---

## 附录：Go 实现中的 Runner 解耦建议

Python 源码中 Runner 通过延迟 import 避免循环依赖，但它是实际的硬依赖。Go 实现中建议通过接口解耦：

```go
// 替代 Runner.run_agent() — 供 MessageRouter 和 DeepAgent 使用
type AgentExecutor interface {
    RunAgent(ctx context.Context, agent Agent, inputs map[string]any, opts ...Option) (any, error)
    RunAgentStreaming(ctx context.Context, agent Agent, inputs map[string]any, opts ...Option) (<-chan Schema, error)
}

// 替代 Runner.resource_mgr — 供 AbilityManager 和 MCP 注册使用
type AgentRegistry interface {
    GetTool(toolID string) (Tool, error)
    AddTool(tool Tool) error
    GetAgent(agentID string) (Agent, error)
    AddAgent(agent Agent) error
    AddMCPServer(config McpServerConfig) error
    GetMCPTool(serverID, toolName string) (Tool, error)
    GetMCPToolInfos(serverID string) ([]ToolInfo, error)
}

// 替代 Runner.callback_framework — 供全局回调使用
type CallbackProvider interface {
    GetCallbackFramework() *CallbackFramework
}
```

这样 ReActAgent、DeepAgent、TeamRuntime 等只需依赖接口，不需要直接 import Runner 包，避免循环依赖同时保持可测试性。
