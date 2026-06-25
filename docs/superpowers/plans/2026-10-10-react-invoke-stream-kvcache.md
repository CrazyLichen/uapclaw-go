# 6.11~6.13 ReActAgent 完整实现（含 Session 接口重构）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整对齐 Python ReActAgent 的 invoke/stream 逻辑（6.11 ReAct 循环、6.12 流式输出、6.13 KV Cache 释放），包含 Session 接口重构（InnerSession + SessionFacade）

**Architecture:** 先做 Session 接口重构（前置依赖），再按 6.11→6.12→6.13 顺序实现。6.11 补全 HITL 中断/恢复路径，6.12 重写 StreamImpl 对齐 Python _inner_stream 模式，6.13 在 railedModelCall 中加入 KV Cache 逻辑。Workflow 中断路径全部延后，用 ⤵️ 标记。不需要预定义 AgentSessionLifecycle/NodeSessionSpecific 接口，需要 Agent 生命周期方法时直接断言 `*session.Session` 具体类型。所有 session 参数统一使用 SessionFacade 接口值类型（Go 接口本身是引用语义，无需传指针）。

**Tech Stack:** Go 1.22+, 接口类型断言（替代 Python 鸭子类型）, goroutine+channel（替代 Python asyncio）

---

## 文件结构

### 新增文件
| 文件 | 职责 |
|------|------|
| `session/interfaces/facade.go` | SessionFacade 接口定义 |
| `single_agent/agents/react_stream.go` | innerStream、writeInvokeResultToStream、streamProcess 等 6.12 流式逻辑 |
| `single_agent/agents/react_hitl.go` | afterExecuteToolCallForHITL、commitInterrupt、clearContextMessages 等 6.11 HITL 集成方法 |

### 修改文件
| 文件 | 变更 |
|------|------|
| `session/interfaces/interfaces.go` | BaseSession → InnerSession（重命名） |
| `session/interfaces/doc.go` | 更新文档 |
| `session/session.go` | type alias 跟随改名 + SessionFacade 相关 |
| `session/agent.go` | Session 补充实现 SessionFacade 接口 |
| `session/node.go` | NodeSessionFacade 补充实现 SessionFacade 接口 |
| `session/checkpointer/inmemory.go` | BaseSession → InnerSession |
| `session/checkpointer/persistence.go` | BaseSession → InnerSession |
| `session/checkpointer/base.go` | BaseSession → InnerSession |
| `session/checkpointer/factory.go` | 如有引用则更新 |
| `session/interaction/base.go` | BaseSession → InnerSession |
| `session/interaction/interaction.go` | BaseSession → InnerSession |
| `session/internal/workflow_session.go` | BaseSession → InnerSession |
| `session/internal/agent_session.go` | 如有引用则更新 |
| `single_agent/interfaces/interface.go` | AgentOptions.Session 改为 SessionFacade |
| `single_agent/rail/context.go` | AgentCallbackContext.session 改为 SessionFacade |
| `single_agent/ability/ability_manager.go` | sess 参数改为 SessionFacade |
| `single_agent/interrupt/handler.go` | ResumeContext.Session 改为 SessionFacade |
| `single_agent/agents/react_agent.go` | 核心改造：InvokeImpl/StreamImpl/reactLoop 完整对齐 |
| `single_agent/resource/resource_manager.go` | ResourceOptions.Session 改为 SessionFacade |
| `context_engine/interface/types.go` | ContextEngine 方法签名 *session.Session → SessionFacade |
| `context_engine/engine.go` | 跟随接口签名变更 |
| `context_engine/context/session_model_context.go` | SetSessionRef/GetSessionRef 改为 SessionFacade |
| `context_engine/context/session_memory_manager.go` | 跟随签名变更 |
| `runner/runner.go` | WithSession 传参跟随变更 |
| 所有相关 _test.go | 跟随接口变更 |

---

## Task 1: Session 接口重构 — InnerSession 改名

**Files:**
- Modify: `session/interfaces/interfaces.go`
- Modify: `session/interfaces/doc.go`
- Modify: `session/session.go`
- Modify: `session/internal/workflow_session.go`
- Modify: `session/internal/agent_session.go`
- Modify: `session/checkpointer/inmemory.go`
- Modify: `session/checkpointer/persistence.go`
- Modify: `session/checkpointer/base.go`
- Modify: `session/checkpointer/factory.go`
- Modify: `session/interaction/base.go`
- Modify: `session/interaction/interaction.go`
- Test: 所有相关 `_test.go`

**说明：** 将 `BaseSession` 重命名为 `InnerSession`，所有引用同步更新。这是纯机械重命名，不改变任何逻辑。

- [ ] **Step 1: 在 interfaces.go 中重命名 BaseSession → InnerSession**

将 `type BaseSession interface` 改为 `type InnerSession interface`，更新所有注释。同时保留 `BaseSession` 作为 `InnerSession` 的类型别名以保持向后兼容过渡期：

```go
// InnerSession 内部会话基础接口。
//
// 对应 Python: BaseSession(ABC) (openjiuwen/core/session/session.py)
// 由 Checkpointer、Storage、Interaction 等内部层使用。
type InnerSession interface {
    Config() config.SessionConfig
    State() state.SessionState
    Tracer() *tracer.Tracer
    StreamWriterManager() *stream.StreamWriterManager
    SessionID() string
    Checkpointer() Checkpointer
    ActorManager() any
    Close() error
}

// BaseSession 向后兼容别名，后续移除。
// Deprecated: 使用 InnerSession
type BaseSession = InnerSession
```

同步更新 `ParentProvider.Parent()` 返回类型：
```go
type ParentProvider interface {
    Parent() InnerSession
}
```

- [ ] **Step 2: 更新 Checkpointer/Storage 接口中的 BaseSession → InnerSession**

在 `interfaces.go` 中，所有 Checkpointer 和 Storage 方法的 `BaseSession` 参数改为 `InnerSession`：

```go
type Checkpointer interface {
    PreWorkflowExecute(ctx context.Context, sess InnerSession, ...) error
    PostWorkflowExecute(ctx context.Context, sess InnerSession, ...) error
    PreAgentExecute(ctx context.Context, sess InnerSession, ...) error
    // ... 所有方法
}
```

- [ ] **Step 3: 更新 session.go 中的 type alias**

```go
// InnerSession 内部会话接口（re-export from interfaces）
type InnerSession = interfaces.InnerSession

// BaseSession 向后兼容别名
// Deprecated: 使用 InnerSession
type BaseSession = interfaces.InnerSession
```

ProxySession 中 `stub BaseSession` 改为 `stub InnerSession`，`SetSession(stub BaseSession)` 改为 `SetSession(stub InnerSession)`。

- [ ] **Step 4: 批量更新 checkpointer/interaction/workflow_session 中的 BaseSession → InnerSession**

所有 `interfaces.BaseSession` 引用改为 `interfaces.InnerSession`，所有 `session.BaseSession` 改为 `session.InnerSession`。具体文件：
- `checkpointer/inmemory.go`
- `checkpointer/persistence.go`
- `checkpointer/base.go`
- `checkpointer/factory.go`
- `interaction/base.go`
- `interaction/interaction.go`
- `internal/workflow_session.go`

- [ ] **Step 5: 更新所有测试文件中的 BaseSession → InnerSession**

更新所有 `_test.go` 中 `fakeBaseSession`、`testSession`、`testWorkflowSession` 等测试 mock 中的 `interfaces.BaseSession` → `interfaces.InnerSession`。

- [ ] **Step 6: 编译验证**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...
```

Expected: 编译通过，无错误。

- [ ] **Step 7: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/agentcore/session/... -count=1
```

Expected: 所有测试通过。

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "refactor(session): rename BaseSession → InnerSession, keep BaseSession as deprecated alias"
```

---

## Task 2: Session 接口重构 — 新增 SessionFacade

**Files:**
- Create: `session/interfaces/facade.go`
- Modify: `session/agent.go`
- Modify: `session/node.go`
- Test: `session/agent_test.go`, `session/node_test.go`（新增接口满足性测试）

**说明：** 定义门面层共有接口 `SessionFacade`，让 `*session.Session` 和 `*NodeSessionFacade` 满足。不需要预定义 Agent/Node 独有接口 — 需要 Agent 生命周期方法时直接断言 `*session.Session`，需要 Node 独有方法时直接断言 `*NodeSessionFacade`。

- [ ] **Step 1: 创建 facade.go 定义 SessionFacade 接口**

```go
package interfaces

import (
    "context"

    "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionFacade 门面会话共有接口。
//
// 对应 Python agent.Session 和 node.Session 的共有方法集。
// ReActAgent 的 invoke/stream 签名使用此接口，而非具体类型。
// 需要特定门面独有方法时，直接断言具体类型：
//   - Agent 生命周期：sess.(*session.Session) 获取 PreRun/CloseStream/Commit/StreamIterator
//   - Node 独有方法：sess.(*NodeSessionFacade) 获取 GetWorkflowID/Trace 等
type SessionFacade interface {
    // GetSessionID 获取会话唯一标识
    GetSessionID() string
    // UpdateState 更新会话状态
    UpdateState(data map[string]any)
    // GetState 获取会话状态值
    GetState(key state.StateKey) (any, error)
    // DumpState 导出完整会话状态
    DumpState() map[string]any
    // WriteStream 写入流数据
    WriteStream(ctx context.Context, data any) error
    // WriteCustomStream 写入自定义流数据
    WriteCustomStream(ctx context.Context, data any) error
    // GetEnv 获取环境变量
    GetEnv(key string, defaultValue ...any) any
    // Interact 交互（等待用户输入）
    Interact(ctx context.Context, value any) error
}
```

- [ ] **Step 2: 让 *session.Session 满足 SessionFacade 接口**

当前 `*session.Session` 已有 `GetSessionID`、`GetState`、`UpdateState`、`DumpState`、`GetEnv`、`Interact` 方法。需要补充 `WriteStream` 和 `WriteCustomStream` 的 context 参数版本。

在 `agent.go` 中添加适配方法（原有无 ctx 版本保留，新增带 ctx 版本满足接口）：

```go
// WriteStream 写入流数据（SessionFacade 接口实现）
func (s *Session) WriteStream(ctx context.Context, data any) error {
    return s.writeStream(data)
}

// WriteCustomStream 写入自定义流数据（SessionFacade 接口实现）
func (s *Session) WriteCustomStream(ctx context.Context, data any) error {
    return s.writeCustomStream(data)
}
```

注意：原有 `WriteStream(data any) error` 和 `WriteCustomStream(data any) error` 无 ctx 参数。为了满足 `SessionFacade` 接口需要带 ctx 版本。将原有方法改为非导出 `writeStream`/`writeCustomStream`，导出方法加 ctx 参数。

同时添加编译时接口检查：
```go
var _ interfaces.SessionFacade = (*Session)(nil)
```

- [ ] **Step 3: 让 *NodeSessionFacade 满足 SessionFacade 接口**

`*NodeSessionFacade` 已有全部 SessionFacade 方法（`GetSessionID`、`UpdateState`、`GetState`、`DumpState`、`WriteStream(ctx,...)`、`WriteCustomStream(ctx,...)`、`GetEnv`、`Interact`）。

添加编译时接口检查：
```go
var _ interfaces.SessionFacade = (*NodeSessionFacade)(nil)
```

- [ ] **Step 4: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...
```

- [ ] **Step 5: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/agentcore/session/... -count=1
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(session): add SessionFacade interface"
```

---

## Task 3: 下游参数类型统一 — AgentOption/AgentCallbackContext/AbilityManager/ContextEngine 改为 SessionFacade

**Files:**
- Modify: `single_agent/interfaces/interface.go`
- Modify: `single_agent/rail/context.go`
- Modify: `single_agent/ability/ability_manager.go`
- Modify: `single_agent/interrupt/handler.go`
- Modify: `single_agent/resource/resource_manager.go`
- Modify: `context_engine/interface/types.go`
- Modify: `context_engine/engine.go`
- Modify: `context_engine/context/session_model_context.go`
- Modify: `context_engine/context/session_memory_manager.go`
- Modify: `runner/runner.go`
- Test: 所有相关 `_test.go`

**说明：** 将所有 `*session.Session` 指针参数改为 `interfaces.SessionFacade` 接口值类型。Go 接口本身就是引用语义（内含指针），无需传 `*session.Session`。这是对齐 Python 鸭子类型签名的关键步骤。

- [ ] **Step 1: AgentOptions.Session 改为 SessionFacade**

在 `single_agent/interfaces/interface.go` 中：

```go
type AgentOptions struct {
    // Session 门面会话实例（可选）
    Session interfaces.SessionFacade
    // StreamModes 流式输出模式（可选）
    StreamModes []stream.StreamMode
}

func WithSession(sess interfaces.SessionFacade) AgentOption {
    return func(o *AgentOptions) { o.Session = sess }
}
```

需要新增 import: `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"`

- [ ] **Step 2: AgentCallbackContext.session 改为 SessionFacade**

在 `single_agent/rail/context.go` 中：

```go
type AgentCallbackContext struct {
    // ... 其他字段不变
    // session 当前门面会话
    session interfaces.SessionFacade
    // ...
}

func NewAgentCallbackContext(
    agent RailAgent,
    inputs EventInputs,
    sess interfaces.SessionFacade,
) *AgentCallbackContext {
    // ...
}

func (c *AgentCallbackContext) Session() interfaces.SessionFacade { return c.session }
```

需要新增 import: `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"`，移除 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session"`（除非其他地方还在用）。

- [ ] **Step 3: AbilityManager 方法参数改 SessionFacade**

在 `ability/ability_manager.go` 中，所有 `sess *session.Session` 参数改为 `sess interfaces.SessionFacade`。

但 `CreateWorkflowSession()` 是 `*session.Session` 独有方法（在 `SessionFacade` 中不存在），需要类型断言：

```go
// 创建 workflow session 时需要 AgentSession 独有方法
if agentSess, ok := sess.(*session.Session); ok {
    workflowSess = agentSess.CreateWorkflowSession()
}
```

- [ ] **Step 4: ResumeContext.Session 改为 SessionFacade**

在 `interrupt/handler.go` 中：

```go
type ResumeContext struct {
    State          *ToolInterruptionState
    UserInput      any
    Ctx            *rail.AgentCallbackContext
    ModelContext   ceinterface.ModelContext
    Session        interfaces.SessionFacade
    InvokeInputs   *rail.InvokeInputs
    ExecuteToolCall ExecuteToolCallFunc
}
```

`ExecuteToolCallFunc` 签名也需跟随变更：
```go
type ExecuteToolCallFunc func(ctx context.Context, cbc *rail.AgentCallbackContext, toolCalls []*llmschema.ToolCall, sess interfaces.SessionFacade, modelCtx ceinterface.ModelContext) ([]ability.ExecuteResult, error)
```

- [ ] **Step 5: ContextEngine 接口签名改 SessionFacade**

在 `context_engine/interface/types.go` 中：

```go
type ContextEngine interface {
    CreateContext(ctx context.Context, contextID string, sess interfaces.SessionFacade, opts ...CreateContextOption) (ModelContext, error)
    SaveContexts(ctx context.Context, sess interfaces.SessionFacade, contextIDs []string) (map[string]any, error)
    CompressContext(ctx context.Context, contextID string, sess interfaces.SessionFacade, opts ...CompressContextOption) (string, error)
    // ...
}
```

`ModelContext` 中 `SetSessionRef`/`GetSessionRef` 也改为 `SessionFacade`：
```go
SetSessionRef(sess interfaces.SessionFacade)
GetSessionRef() interfaces.SessionFacade
```

- [ ] **Step 6: ContextEngine 实现跟随签名变更**

`context_engine/engine.go`、`session_model_context.go`、`session_memory_manager.go` 等实现文件的参数类型跟随改为 `SessionFacade`。由于 ContextEngine 内部只调用 `GetSessionID`/`GetState`/`UpdateState`（均在 SessionFacade 中），无需类型断言。

- [ ] **Step 7: ResourceManager 跟随变更**

`resource/resource_manager.go` 中 `ResourceOptions.Session` 改为 `SessionFacade`。

- [ ] **Step 8: Runner 跟随变更**

`runner/runner.go` 中 `interfaces.WithSession(sess)` — `sess` 类型从 `*session.Session` 变为需传 `SessionFacade`。由于 `*session.Session` 已实现 `SessionFacade`，此处自然满足。

- [ ] **Step 9: 更新所有测试文件**

所有 mock/stub 中的 `*session.Session` 参数跟随改为 `SessionFacade`。测试中构造 `session.NewSession()` 后传参自然满足（`*Session` 实现了 `SessionFacade`）。

- [ ] **Step 10: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...
```

- [ ] **Step 11: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/agentcore/... -count=1
```

- [ ] **Step 12: Commit**

```bash
git add -A && git commit -m "refactor: unify session parameter types to SessionFacade across agent/rail/ability/context_engine"
```

---

## Task 4: 6.11 ReAct 循环 — HITL 中断/恢复集成

**Files:**
- Create: `single_agent/agents/react_hitl.go`
- Modify: `single_agent/agents/react_agent.go`
- Test: `single_agent/agents/react_agent_test.go`

**说明：** 在 ReActAgent 中添加 HITL handler 字段，在 InvokeImpl 中加载中断状态并走恢复路径，在 reactLoop 中加入 HITL 中断检测，在 finally 中加入清理逻辑。

- [ ] **Step 1: 创建 react_hitl.go，包含 HITL 集成方法**

```go
package agents

import (
    "context"

    ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
    agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
    llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AfterExecuteToolCallForHITL 执行工具后检测 HITL 中断。
//
// 对应 Python: ReActAgent._after_execute_tool_call_for_hitl()
func (a *ReActAgent) AfterExecuteToolCallForHITL(
    results []ability.ExecuteResult,
    toolCalls []*llmschema.ToolCall,
    aiMessage *llmschema.AssistantMessage,
    iteration int,
    originalQuery string,
) (*interrupt.ToolInterruptionState, []any) {
    if a.hitlHandler == nil {
        return nil, nil
    }
    intState, payloads := a.hitlHandler.BuildInterruptState(
        results, toolCalls, aiMessage, iteration, originalQuery,
    )
    if intState == nil {
        return nil, nil
    }
    return intState, payloads
}

// CommitInterrupt 提交中断状态。
//
// 对应 Python: ReActAgent._commit_interrupt() 的 HITL 分支
func (a *ReActAgent) CommitInterrupt(
    ctx context.Context,
    intState *interrupt.ToolInterruptionState,
    modelCtx ceinterface.ModelContext,
    sess interfaces.SessionFacade,
    invokeInputs *rail.InvokeInputs,
    subAgentOutputs []any,
) map[string]any {
    if a.hitlHandler == nil {
        return nil
    }
    return a.hitlHandler.CommitInterrupt(ctx, intState, modelCtx, sess, invokeInputs, subAgentOutputs)
}

// ClearContextMessages 清除当前上下文消息（保留历史）。
//
// 对应 Python: ReActAgent.clear_context_messages(with_history=False)
func (a *ReActAgent) ClearContextMessages(sess interfaces.SessionFacade) {
    if a.contextEngine == nil {
        return
    }
    ctx := context.Background()
    sessionID := sess.GetSessionID()
    mc := a.contextEngine.GetContext(
        ceinterface.WithSessionID(sessionID),
    )
    if mc != nil {
        mc.ClearMessages(ctx, false)
    }
}
```

- [ ] **Step 3: 在 ReActAgent 结构体中添加 hitlHandler 字段**

在 `react_agent.go` 中：

```go
type ReActAgent struct {
    base          *single_agent.WarpBaseAgent
    config        *saconfig.ReActAgentConfig
    contextEngine ceinterface.ContextEngine
    llm           *llm.Model
    promptBuilder *SystemPromptBuilder
    hitlHandler   *interrupt.ToolInterruptHandler  // 新增：HITL 中断处理器
    llmOnce       sync.Once
    kvReleaseWarningLogged bool
}
```

在 `NewReActAgent` 中初始化：

```go
func NewReActAgent(card *agentschema.AgentCard, config *saconfig.ReActAgentConfig) *ReActAgent {
    base := single_agent.NewWarpBaseAgent(card, &resource.NoopResourceManager{})
    agent := &ReActAgent{
        base:          base,
        config:        config,
        promptBuilder: NewSystemPromptBuilder(),
    }
    // HITL 处理器需要 InterruptAgent 接口（ReActAgent 隐式满足）
    agent.hitlHandler = interrupt.NewToolInterruptHandler(agent)
    base.SetInvoker(agent)
    return agent
}
```

- [ ] **Step 4: 重写 InvokeImpl — 入口对齐 Python _inner_invoke**

```go
func (a *ReActAgent) InvokeImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
    agentOpts := interfaces.NewAgentOptions(opts...)
    sess := agentOpts.Session

    if sess == nil {
        sess = session.NewSession(session.WithSessionID("default_session"))
    }

    // AgentSession 生命周期断言（直接断言 *session.Session 具体类型）
    // 对应 Python: hasattr(session, "pre_run") and hasattr(session, "close_stream") and hasattr(session, "commit")
    var agentSess *session.Session
    if as, ok := sess.(*session.Session); ok {
        agentSess = as
        if err := as.PreRun(ctx, inputs); err != nil {
            logger.Warn(logComponent).Err(err).Msg("PreRun 失败")
        }
    }

    query, _ := inputs["query"].(string)
    conversationID, _ := inputs["conversation_id"].(string)

    invokeInputs := &rail.InvokeInputs{
        Query:          rail.NewInvokeQueryString(query),
        ConversationID: conversationID,
    }
    cbc := rail.NewAgentCallbackContext(a, invokeInputs, sess)

    // 设置 extra
    if userID, ok := inputs["user_id"].(string); ok {
        cbc.Extra()["user_id"] = userID
    }
    if streaming, ok := inputs["_streaming"].(bool); ok {
        cbc.Extra()["_streaming"] = streaming
    } else {
        cbc.Extra()["_streaming"] = false
    }
    if sq, ok := inputs["_steering_queue"]; ok {
        if ch, ok2 := sq.(chan string); ok2 {
            cbc.BindSteeringQueue(ch)
        }
    }

    var result any
    var err error

    // 核心逻辑（含 try/finally 等价）
    func() {
        var innerResult map[string]any
        innerErr := cbc.FireLifecycle(rail.CallbackBeforeInvoke, rail.CallbackAfterInvoke, func() error {
            // 加载中断状态
            hitlState := a.hitlHandler.Load(sess)
            var interruptionState any
            if hitlState != nil {
                interruptionState = hitlState
                a.hitlHandler.Clear(sess)
            }
            // ⤵️ 6.11: Workflow interruption load
            // else interruptionState = a.loadInterruptionState(sess)

            // 恢复原始 query
            if interruptionState != nil {
                if hitls, ok := interruptionState.(*interrupt.ToolInterruptionState); ok {
                    cbc.Extra()["_original_query"] = hitls.OriginalQuery
                }
            }

            // 初始化上下文
            modelCtx, initErr := a.initContext(ctx, sess)
            if initErr != nil {
                return fmt.Errorf("初始化上下文失败: %w", initErr)
            }
            cbc.SetModelContext(modelCtx)

            // Resume 分支
            startIteration := 0
            if interruptionState != nil {
                if _, isHITL := interruptionState.(*interrupt.ToolInterruptionState); isHITL {
                    // HITL resume
                    a.hitlHandler.HandleResume(ctx, &interrupt.ResumeContext{
                        State:           interruptionState.(*interrupt.ToolInterruptionState),
                        UserInput:       query,
                        Ctx:             cbc,
                        ModelContext:    modelCtx,
                        Session:         sess,
                        InvokeInputs:    invokeInputs,
                        ExecuteToolCall: a.executeToolCalls,
                    })
                    if si, ok := cbc.Extra()[agentschema.ResumeStartIterationKey].(int); ok {
                        startIteration = si
                        delete(cbc.Extra(), agentschema.ResumeStartIterationKey)
                    }
                }
                // ⤵️ 6.11: Workflow handleResume
            } else {
                // 正常路径：添加 UserMessage
                if modelCtx != nil && query != "" {
                    _, _ = modelCtx.AddMessages(ctx, llmschema.NewUserMessage(query))
                }
            }

            // ReAct 循环
            if invokeInputs.Result == nil {
                innerResult, initErr = a.reactLoop(ctx, cbc, sess, modelCtx, startIteration)
                if initErr != nil {
                    return initErr
                }
                invokeInputs.Result = innerResult
            }
            return nil
        })

        if innerErr != nil {
            err = innerErr
            return
        }

        if invokeResult, ok := cbc.Extra()["invoke_result"]; ok {
            if r, ok2 := invokeResult.(map[string]any); ok2 {
                result = r
                return
            }
        }
        result = innerResult
    }()

    // CancelledError 处理
    if errors.Is(err, context.Canceled) {
        a.ClearContextMessages(sess)
        return nil, err
    }
    if err != nil {
        return nil, err
    }

    // finally 清理（agentSess 非 nil 说明是 Agent Session，需要生命周期清理）
    if agentSess != nil {
        a.saveContexts(sess)
        agentSess.CloseStream()
        agentSess.Commit(ctx)
    }

    return result, nil
}
```

- [ ] **Step 5: 改造 reactLoop — 加入 HITL 中断检测 + startIteration**

```go
func (a *ReActAgent) reactLoop(
    ctx context.Context,
    cbc *rail.AgentCallbackContext,
    sess interfaces.SessionFacade,
    modelCtx ceinterface.ModelContext,
    startIteration int,
) (map[string]any, error) {
    maxIter := defaultMaxIterations
    if a.config != nil && a.config.MaxIterations > 0 {
        maxIter = a.config.MaxIterations
    }

    tools, _ := a.getTools()
    var iterResult map[string]any
    var invokeInputs *rail.InvokeInputs
    if ii, ok := cbc.Inputs().(*rail.InvokeInputs); ok {
        invokeInputs = ii
    }

    for iteration := startIteration; iteration < maxIter; iteration++ {
        logger.Info(logComponent).Int("iteration", iteration+1).Int("max_iterations", maxIter).Msg("ReAct 迭代")

        // steering 注入
        if steeringMsgs := cbc.DrainSteering(); len(steeringMsgs) > 0 && modelCtx != nil {
            for _, msg := range steeringMsgs {
                _, _ = modelCtx.AddMessages(ctx, llmschema.NewUserMessage("[STEERING] "+msg))
            }
        }

        // 调用 LLM
        aiMsg, err := a.callModel(ctx, cbc, modelCtx, tools)
        if err != nil {
            return nil, fmt.Errorf("迭代 %d 模型调用失败: %w", iteration, err)
        }

        // force-finish #1
        if finish := cbc.ConsumeForceFinish(); finish != nil {
            a.saveContexts(sess)
            iterResult = finish.Result
            break
        }

        if aiMsg != nil && modelCtx != nil {
            _, _ = modelCtx.AddMessages(ctx, aiMsg)
        }

        // 无工具调用
        if aiMsg == nil || len(aiMsg.ToolCalls) == 0 {
            if cbc.HasPendingSteering() {
                continue
            }
            content := ""
            if aiMsg != nil {
                content = aiMsg.Content.Text()
            }
            a.saveContexts(sess)
            iterResult = map[string]any{"output": content, "result_type": "answer"}
            break
        }

        // 执行工具
        results, err := a.executeToolCalls(ctx, cbc, aiMsg.ToolCalls, sess, modelCtx)
        if err != nil {
            logger.Error(logComponent).Str("event_type", "tool_execution_error").Int("iteration", iteration).Err(err).Msg("工具执行失败")
        }

        // force-finish #2
        if finish := cbc.ConsumeForceFinish(); finish != nil {
            a.saveContexts(sess)
            iterResult = finish.Result
            break
        }

        // HITL 中断检测
        originalQuery := ""
        if oq, ok := cbc.Extra()["_original_query"].(string); ok {
            originalQuery = oq
        }
        hitlInterrupt, subAgentOutputs := a.AfterExecuteToolCallForHITL(
            results, aiMsg.ToolCalls, aiMsg, iteration, originalQuery,
        )
        if hitlInterrupt != nil {
            a.CommitInterrupt(ctx, hitlInterrupt, modelCtx, sess, invokeInputs, subAgentOutputs)
            break
        }

        // ⤵️ 6.11: Workflow 中断检测
        // workflowInterrupt := a.AfterExecuteToolCall(results, aiMsg.ToolCalls, aiMsg, iteration, originalQuery)
        // if workflowInterrupt != nil {
        //     a.CommitWorkflowInterrupt(ctx, workflowInterrupt, modelCtx, sess, invokeInputs)
        //     break
        // }
    }

    if iterResult == nil {
        a.saveContexts(sess)
        iterResult = map[string]any{"output": "Max iterations reached without completion", "result_type": "error"}
    }

    if invokeInputs != nil {
        invokeInputs.Result = iterResult
    }

    return iterResult, nil
}
```

- [ ] **Step 6: 改造 initContext/saveContexts 参数类型**

```go
func (a *ReActAgent) initContext(ctx context.Context, sess interfaces.SessionFacade) (ceinterface.ModelContext, error) {
    if a.contextEngine == nil {
        return nil, nil
    }
    return a.contextEngine.CreateContext(ctx, "default_context", sess)
}

func (a *ReActAgent) saveContexts(sess interfaces.SessionFacade) {
    if a.contextEngine == nil || sess == nil {
        return
    }
    if _, err := a.contextEngine.SaveContexts(context.Background(), sess, nil); err != nil {
        logger.Warn(logComponent).Str("event_type", "save_contexts_error").Err(err).Msg("保存上下文失败")
    }
}
```

- [ ] **Step 7: 改造 executeToolCalls 参数类型**

```go
func (a *ReActAgent) executeToolCalls(
    ctx context.Context,
    cbc *rail.AgentCallbackContext,
    toolCalls []*llmschema.ToolCall,
    sess interfaces.SessionFacade,
    modelCtx ceinterface.ModelContext,
) ([]ability.ExecuteResult, error) {
    // ... 逻辑不变，am.Execute 调用跟随 ability_manager 签名变更
}
```

- [ ] **Step 8: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/...
```

- [ ] **Step 9: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/agentcore/single_agent/... -count=1
```

- [ ] **Step 10: Commit**

```bash
git add -A && git commit -m "feat(react): integrate HITL interrupt/resume in reactLoop and InvokeImpl (6.11)"
```

---

## Task 5: 6.12 流式输出 — StreamImpl 完整重写

**Files:**
- Create: `single_agent/agents/react_stream.go`
- Modify: `single_agent/agents/react_agent.go`（移除旧 StreamImpl）
- Test: `single_agent/agents/react_agent_test.go`

**说明：** 重写 StreamImpl，对齐 Python `_inner_stream` 模式。包含 innerStream、writeInvokeResultToStream、streamProcess。railedModelCall 加入流式 chunk 实时推送。

- [ ] **Step 1: 创建 react_stream.go，包含流式逻辑**

```go
package agents

import (
    "context"
    "errors"

    "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WriteInvokeResultToStream 将 invoke 结果写入会话流。
//
// 对应 Python: ReActAgent._write_invoke_result_to_stream()
func (a *ReActAgent) WriteInvokeResultToStream(
    ctx context.Context,
    result map[string]any,
    sess interfaces.SessionFacade,
) {
    resultType, _ := result["result_type"].(string)
    if resultType == "interrupt" {
        if _, hasInterruptIDs := result["interrupt_ids"]; hasInterruptIDs {
            // HITL 中断写入流
            if a.hitlHandler != nil {
                a.hitlHandler.WriteInterruptToStream(ctx, result, sess)
            }
        } else {
            // ⤵️ 6.12: Workflow 中断写入流
            // workflowState := result["workflow_execution_state"]
            // componentIDs := result["component_ids"]
            // pendingID := componentIDs[0]
            // 遍历 workflowState.result，写入 payload.id == pendingID 的 schema
        }
    } else {
        // 正常 answer 结果
        output, _ := result["output"].(string)
        _ = sess.WriteStream(ctx, &stream.OutputSchema{
            Type:  "answer",
            Index: 0,
            Payload: map[string]any{
                "output":      output,
                "result_type": resultType,
            },
        })
    }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// innerStream 内部流式执行。
//
// 对应 Python: ReActAgent._inner_stream()
func (a *ReActAgent) innerStream(
    ctx context.Context,
    sess interfaces.SessionFacade,
    agentSess *session.Session, // 非 nil 时表示是 Agent Session，需要生命周期管理
    isAgentSess bool,
    inputs map[string]any,
    opts []interfaces.AgentOption,
    outCh chan<- stream.Schema,
) {
    // streamProcess: 在后台执行 invoke，结果写入 session stream
    streamProcess := func() {
        defer func() {
            // finally: 清理
            if isAgentSess && agentSess != nil {
                a.saveContexts(sess)
                agentSess.CloseStream()
                agentSess.Commit(ctx)
            }
        }()

        // 捕获 panic 防止 goroutine 崩溃
        defer func() {
            if r := recover(); r != nil {
                logger.Error(logComponent).Any("panic", r).Msg("streamProcess panic")
                a.WriteInvokeResultToStream(ctx, map[string]any{
                    "output":      fmt.Sprintf("panic: %v", r),
                    "result_type": "error",
                }, sess)
            }
        }()

        // 走完整虚分发（对齐 Python: self.invoke(inputs, session, _streaming=True)）
        result, err := a.base.Invoke(ctx, inputs, opts...)
        if err != nil {
            // 错误结果写入流
            logger.Error(logComponent).Err(err).Str("event_type", "LLM_CALL_ERROR").Msg("streamProcess invoke 错误")
            a.WriteInvokeResultToStream(ctx, map[string]any{
                "output":      err.Error(),
                "result_type": "error",
            }, sess)
            return
        }

        // 正常结果写入流
        if resultMap, ok := result.(map[string]any); ok {
            a.WriteInvokeResultToStream(ctx, resultMap, sess)
        } else if resultList, ok := result.([]stream.Schema); ok {
            // invoke 返回 schema 列表（中断路径）
            for _, schema := range resultList {
                _ = sess.WriteStream(ctx, schema)
            }
        }
    }

    if isAgentSess && agentSess != nil {
        // Agent session: 启动 streamProcess goroutine，从 StreamIterator 消费
        go streamProcess()

        for chunk := range agentSess.StreamIterator() {
            outCh <- chunk
        }
    } else {
        // Workflow session: 直接执行 streamProcess
        // 输出通过 session.WriteStream → StreamWriterManager 传递给 Workflow
        streamProcess()
    }
}
```

- [ ] **Step 2: 重写 StreamImpl — 对齐 Python stream()**

在 `react_agent.go` 中替换旧 StreamImpl：

```go
// StreamImpl 实现 AgentInvoker 接口 —— 流式调用。
//
// 对应 Python: ReActAgent.stream()
func (a *ReActAgent) StreamImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
    agentOpts := interfaces.NewAgentOptions(opts...)
    sess := agentOpts.Session

    if sess == nil {
        sess = session.NewSession(session.WithSessionID("default_session"))
        // 补充到 opts 中
        opts = append(opts, interfaces.WithSession(sess))
    }

    // AgentSession 生命周期断言（直接断言 *session.Session）
    var agentSess *session.Session
    isAgentSess := false
    if as, ok := sess.(*session.Session); ok {
        agentSess = as
        isAgentSess = true
        if err := as.PreRun(ctx, inputs); err != nil {
            logger.Warn(logComponent).Err(err).Msg("PreRun 失败")
        }
    }

    inputs["_streaming"] = true
    outCh := make(chan stream.Schema, 64)

    go func() {
        defer close(outCh)
        a.innerStream(ctx, sess, agentSess, isAgentSess, inputs, opts, outCh)
    }()

    return outCh, nil
}
```

- [ ] **Step 3: 改造 railedModelCall — 流式 chunk 实时推送**

在 `react_agent.go` 的 `callLLMStream` 中，增加 chunk 实时写入 session stream 的逻辑：

```go
func (a *ReActAgent) callLLMStream(
    ctx context.Context,
    llmModel *llm.Model,
    modelName string,
    messages []llmschema.BaseMessage,
    tools []*cschema.ToolInfo,
    sess interfaces.SessionFacade,
) (*llmschema.AssistantMessage, error) {
    toolProviders := make([]cschema.ToolInfoProvider, len(tools))
    for i, t := range tools {
        toolProviders[i] = t
    }
    msgsParam := model_clients.NewMessagesParam(messages...)
    chunkCh, err := (*llmModel).Stream(ctx, msgsParam,
        model_clients.WithStreamModel(modelName),
        model_clients.WithStreamTools(toolProviders...),
    )
    if err != nil {
        return nil, fmt.Errorf("LLM stream 失败: %w", err)
    }

    var finalMsg *llmschema.AssistantMessage
    chunkIndex := 0
    for chunk := range chunkCh {
        if finalMsg == nil {
            finalMsg = llmschema.NewAssistantMessage("")
        }
        // 累积内容
        finalMsg.Content = llmschema.NewTextContent(finalMsg.Content.Text() + chunk.Content.Text())
        if len(chunk.ToolCalls) > 0 {
            finalMsg.ToolCalls = append(finalMsg.ToolCalls, chunk.ToolCalls...)
        }

        // 实时写入 session stream（对齐 Python railed_model_call L776-809）
        if chunk.Content.Text() != "" {
            _ = sess.WriteStream(ctx, &stream.OutputSchema{
                Type:  "llm_output",
                Index: chunkIndex,
                Payload: map[string]any{
                    "content":     chunk.Content.Text(),
                    "result_type": "answer",
                },
            })
            chunkIndex++
        }
        if chunk.ReasoningContent != "" {
            _ = sess.WriteStream(ctx, &stream.OutputSchema{
                Type:  "llm_reasoning",
                Index: chunkIndex,
                Payload: map[string]any{
                    "content":     chunk.ReasoningContent,
                    "result_type": "answer",
                },
            })
            chunkIndex++
        }
    }

    if finalMsg == nil {
        finalMsg = llmschema.NewAssistantMessage("")
    }

    // usage_metadata 写入流
    if finalMsg.UsageMetadata != nil {
        _ = sess.WriteStream(ctx, &stream.OutputSchema{
            Type:  "llm_usage",
            Index: 0,
            Payload: map[string]any{
                "usage_metadata": finalMsg.UsageMetadata,
                "result_type":    "answer",
            },
        })
    }

    return finalMsg, nil
}
```

需要在 `railedModelCall` 中传入 `sess`，从 `cbc.Session()` 获取。

- [ ] **Step 4: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/...
```

- [ ] **Step 5: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/agentcore/single_agent/... -count=1
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(react): rewrite StreamImpl with innerStream/writeInvokeResultToStream/realtime chunk push (6.12)"
```

---

## Task 6: 6.13 KV Cache 释放

**Files:**
- Modify: `single_agent/agents/react_agent.go`（railedModelCall 中加入 KV Cache 逻辑）
- Test: `single_agent/agents/react_agent_test.go`

**说明：** 在 `railedModelCall` 中检查 `enable_kv_cache_release` + `supports_kv_cache_release`，条件满足时传 model 给 `GetContextWindow`，构建 extra invoke kwargs。

- [ ] **Step 1: 在 railedModelCall 中加入 KV Cache 检查和传参逻辑**

在 `railedModelCall` 方法中，`GetContextWindow` 调用前，加入：

```go
// KV Cache 释放逻辑（对应 Python _railed_model_call L686-720）
var enableKVRelease bool
if a.config != nil && a.config.ContextEngineConfig != nil {
    enableKVRelease = a.config.ContextEngineConfig.EnableKVCacheRelease
}

var supportsKVRelease bool
llmModel, _ := a.getLLM()
if llmModel != nil {
    if releaser, ok := (*llmModel).(KVCacheReleaser); ok {
        supportsKVRelease = releaser.SupportsKVCacheRelease()
    }
}

// 不支持时一次性警告
if enableKVRelease && !supportsKVRelease && !a.kvReleaseWarningLogged {
    logger.Warn(logComponent).
        Str("event_type", "kv_cache_release_not_supported").
        Msg("enable_kv_cache_release is True but LLM does not support KV cache release")
    a.kvReleaseWarningLogged = true
}

// 构建 GetContextWindow 选项
var contextWindowOpts []ceinterface.GetContextWindowOption
if enableKVRelease && supportsKVRelease {
    contextWindowOpts = append(contextWindowOpts, ceinterface.WithKVCacheModel(*llmModel))
}

contextWindow, err := modelCtx.GetContextWindow(ctx, systemMsgs, nil, 0, 0, contextWindowOpts...)
```

- [ ] **Step 2: 构建 extra invoke kwargs**

在 LLM invoke/stream 调用前，加入：

```go
// 构建 KV Cache extra kwargs（对应 Python L736-742）
extraKVPairs := make(map[string]any)
if llmModel != nil {
    if builder, ok := (*llmModel).(KVCacheKwargsBuilder); ok {
        extraKVPairs = builder.BuildKVCacheInvokeKwargs(sess, enableKVRelease)
    }
}
```

- [ ] **Step 3: 在 ReActAgent 同文件定义 KV Cache 接口**

在 `react_agent.go` 顶部添加接口定义（或在 `llm` 包中定义）：

```go
// KVCacheReleaser KV Cache 释放能力接口。
//
// 对应 Python: llm.supports_kv_cache_release() 方法
type KVCacheReleaser interface {
    SupportsKVCacheRelease() bool
}

// KVCacheKwargsBuilder KV Cache 调用参数构建接口。
//
// 对应 Python: llm.build_kv_cache_invoke_kwargs() 方法
type KVCacheKwargsBuilder interface {
    BuildKVCacheInvokeKwargs(sess interfaces.SessionFacade, enableKVCacheRelease bool) map[string]any
}
```

- [ ] **Step 4: 在 ContextEngineConfig 中添加 EnableKVCacheRelease 字段**

检查 `saconfig.ReActAgentConfig` 或 `ceconfig.ContextEngineConfig` 是否已有该字段，若无则添加：

```go
// EnableKVCacheRelease 是否启用 KV Cache 释放
EnableKVCacheRelease bool
```

- [ ] **Step 5: 添加 GetContextWindow 选项支持**

在 `context_engine/interface/types.go` 的 `GetContextWindowOption` 中添加：

```go
// WithKVCacheModel 传入 LLM model 用于 KV Cache 释放
func WithKVCacheModel(model any) GetContextWindowOption {
    return func(o *getContextWindowOptions) {
        o.model = model
    }
}
```

- [ ] **Step 6: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/...
```

- [ ] **Step 7: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./internal/agentcore/single_agent/... -count=1
```

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat(react): add KV Cache release support in railedModelCall (6.13)"
```

---

## Task 7: 全量编译 + 测试 + IMPLEMENTATION_PLAN 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

**说明：** 全量编译测试，更新实现计划状态标记。

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uap-claw-go && go build ./...
```

- [ ] **Step 2: 全量测试**

```bash
cd /home/opensource/uap-claw-go && go test -tags=test ./... -count=1
```

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 状态**

将 6.11、6.12、6.13 对应行从 `☐` 改为 `✅`。

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "chore: mark 6.11-6.13 as completed in IMPLEMENTATION_PLAN.md"
```

---

## ⤵️ 延后标记清单

| 位置 | 标记 | 对应 Python | 回填时机 |
|------|------|------------|---------|
| InvokeImpl | `// ⤵️ 6.11: Workflow interruption load` | `_load_interruption_state(session)` | Workflow 中断实现时 |
| InvokeImpl | `// ⤵️ 6.11: Workflow handleResume` | `_handle_resume(InterruptionState 分支)` | Workflow 中断实现时 |
| reactLoop | `// ⤵️ 6.11: Workflow 中断检测` | `_after_execute_tool_call()` | Workflow 中断实现时 |
| reactLoop | `// ⤵️ 6.11: Workflow commitInterrupt` | `_commit_interrupt(InterruptionState 分支)` | Workflow 中断实现时 |
| WriteInvokeResultToStream | `// ⤵️ 6.12: Workflow 中断写入流` | `_write_invoke_result_to_stream(workflow 分支)` | Workflow 中断实现时 |
