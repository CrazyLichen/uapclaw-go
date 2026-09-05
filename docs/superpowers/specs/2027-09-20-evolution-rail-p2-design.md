# 9.24 P2 EvolutionRail 基类 + TrajectoryRail 设计

## 概述

实现 9.24 EvolutionRail 的 P2 阶段：EvolutionRail 基类 + TrajectoryRail 纯收集轨道。

P1 契约层（contracts / approval_events / approval_runtime）已完成，P2 在此基础上构建演化轨道的核心骨架和轨迹自动收集能力，为后续 P3-P6 的具体演化轨道提供基础。

## 已确认决策

| 决策项 | 结论 |
|---|---|
| 继承方式 | 嵌入 DeepAgentRail + 覆写方法（与现有 Rail 一致） |
| 扩展点多态 | EvolutionExtension 接口字段，Go 嵌入无虚方法分派，必须通过接口实现多态 |
| 扩展点接口 | 完整 9 个方法，对齐 Python 全部扩展点 |
| 扩展点放置 | 独立文件 extension.go |
| 异步快照 | 复用 P1 已有的 *EvolutionSnapshot 强类型 |
| 构造方式 | Functional Options 模式 |
| helpers.go | 3 个辅助函数全部放入 |
| builder.go | P2 不动，后续单独做统一框架 |
| factory.go | P2 不动（Python 也不自动添加 TrajectoryRail） |
| extractor.go any 消除 | 纳入 P2 范围，resourceManager 改为 *resources_manager.ResourceMgr |

## 流程位置与作用

### 在 Agent 会话中的流程位置

```
DeepAgent ReAct 循环
│
├── before_invoke ───────→ EvolutionRail.BeforeInvoke()
│   │                         ├── type switch → *InvokeInputs
│   │                         ├── resolveSessionID
│   │                         ├── builder 复用或新建 TrajectoryBuilder
│   │                         └── ext.OnBeforeInvoke()
│   │
│   └── Task Loop（可多轮迭代）
│       │
│       ├── after_model_call ──→ EvolutionRail.AfterModelCall()
│       │                            ├── 构建 LLMCallDetail + TrajectoryStep
│       │                            ├── builder.RecordStep(step)
│       │                            ├── ext.OnAfterModelCall()
│       │                            └── if trigger=AFTER_MODEL_CALL → triggerEvolution
│       │
│       ├── after_tool_call ────→ EvolutionRail.AfterToolCall()
│       │                            ├── 构建 ToolCallDetail + TrajectoryStep
│       │                            ├── builder.RecordStep(step)
│       │                            ├── ext.OnAfterToolCall()
│       │                            └── if trigger=AFTER_TOOL_CALL → triggerEvolution
│       │
│       └── after_task_iteration → EvolutionRail.AfterTaskIteration()
│                                       ├── ext.OnAfterTaskIteration()
│                                       └── if trigger=AFTER_TASK_ITERATION → triggerEvolution
│
└── after_invoke ──────────→ EvolutionRail.AfterInvoke()
                                ├── buildTrajectory()
                                ├── trajectoryStore.Save()
                                ├── publishTrajectorySnapshot() [Sink 接口]
                                ├── ext.OnAfterInvoke()
                                ├── if trigger=AFTER_INVOKE → triggerEvolution
                                └── ext.OnAfterEvolutionTriggered()
```

### 作用

1. **自动轨迹收集**：4 个 final 回调钩子自动将 Agent 执行过程中的 LLM 调用和工具调用记录为 TrajectoryStep，无需子类关心数据采集逻辑
2. **演化触发框架**：通过 EvolutionTriggerPoint 枚举 + triggerEvolution() + 异步后台任务统一管理"何时触发演化"和"如何安全执行演化"
3. **TrajectoryRail**：最简单的子类——只收集轨迹不做演化，用于可观测性调试、离线数据收集、行为分析

## 依赖分析

### 已就绪的依赖（无需跨包改动）

| 依赖 | Go 包 | 位置 |
|---|---|---|
| DeepAgentRail 基类 | `agentcore/harness/rails` | `base.go` |
| AgentCallbackContext + EventInputs | `agentcore/single_agent/interfaces` | `callback.go` |
| TrajectoryBuilder + TrajectoryStore | `evolving/trajectory` | `builder.go`, `store.go` |
| TrajectoryStep / LLMCallDetail / ToolCallDetail | `evolving/trajectory` | `types.go` |
| TrajectorySink / TrajectorySource / MemberTrajectorySnapshot | `evolving/trajectory` | `registry.go` |
| BackgroundTask + CreateBackgroundTask | `common/utils` | `background.go` |
| ConversationSignalDetector.ConvertTrajectoryToMessages | `evolving/signal` | `from_conv.go` |
| EvolutionSnapshot + EvolutionHostEventMeta | `agentcore/harness/rails/evolution` | `contracts.go` (P1) |
| EvolutionApprovalRuntime | `agentcore/harness/rails/evolution` | `approval_runtime.go` (P1) |
| ApprovalManager 接口 | `agentcore/harness/rails/evolution` | `contracts.go` (P1) |
| OutputSchema (stream 包) | `agentcore/session/stream` | — |

### 不涉及的范围

- ❌ `factory.go` addDefaultRails — 不修改
- ❌ `builder.go` resolveRails — 不修改
- ❌ `deep_adapter.go` / `deep_adapter_slash.go` — P3/P4 回填

## 文件布局

### evolution 包新增文件

```
evolution/
├── doc.go                    # 更新文件目录
├── contracts.go              # ✅ P1 已有
├── approval_events.go        # ✅ P1 已有
├── approval_runtime.go       # ✅ P1 已有
├── contracts_test.go         # ✅ P1 已有
├── approval_events_test.go   # ✅ P1 已有
├── approval_runtime_test.go  # ✅ P1 已有
├── extension.go              # 🆕 EvolutionExtension 接口 + noOpExtension
├── extension_test.go         # 🆕
├── evolution_rail.go         # 🆕 EvolutionRail 结构体 + EvolutionTriggerPoint + Options
├── evolution_rail_test.go    # 🆕
├── trajectory_rail.go        # 🆕 TrajectoryRail 结构体
├── trajectory_rail_test.go   # 🆕
├── helpers.go                # 🆕 3 个辅助函数 + collectMessagesFromTrajectory
└── helpers_test.go           # 🆕
```

### trajectory 包修改文件

```
trajectory/
├── extractor.go              # ✏️ resourceManager any → *ResourceMgr，实现 GetToolInfos 调用
├── extractor_test.go         # ✏️ 更新 NewTracerTrajectoryExtractor 调用签名
```

## 详细设计

### 1. extension.go — EvolutionExtension 接口

```go
// EvolutionExtension 演化轨道扩展点接口。
//
// EvolutionRail 的 4 个 final 回调内部完成轨迹收集后，
// 通过此接口将控制权交给子类实现的自定义逻辑。
// Go 嵌入无虚方法分派，必须通过接口实现多态。
//
// 对齐 Python: EvolutionRail._on_xxx + run_evolution 系列扩展点。
type EvolutionExtension interface {
    // OnBeforeInvoke 在每次 invoke 开始时调用（轨迹 builder 已初始化或复用）。
    // 对齐 Python: _on_before_invoke(ctx)
    OnBeforeInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error

    // OnAfterModelCall 在每次模型调用后调用（LLM 步骤已记录到 builder）。
    // 对齐 Python: _on_after_model_call(ctx)
    OnAfterModelCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error

    // OnAfterToolCall 在每次工具调用后调用（Tool 步骤已记录到 builder）。
    // 对齐 Python: _on_after_tool_call(ctx)
    OnAfterToolCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error

    // OnAfterInvoke 在每次 invoke 结束时调用（轨迹已保存，builder 仍可用）。
    // 对齐 Python: _on_after_invoke(ctx)
    OnAfterInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error

    // OnAfterTaskIteration 在每次任务循环迭代后调用。
    // 对齐 Python: _on_after_task_iteration(ctx)
    OnAfterTaskIteration(ctx context.Context, cbc *interfaces.AgentCallbackContext) error

    // OnAfterEvolutionTriggered 在 after_invoke 演化触发完成后调用。
    // 子类可覆写此方法消费对 AllowEvolutionTrigger 和快照可见的状态。
    // 对齐 Python: _on_after_evolution_triggered(trajectory, ctx)
    OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *interfaces.AgentCallbackContext) error

    // AllowEvolutionTrigger 返回当前触发点是否允许启动演化。
    // 对齐 Python: _allow_evolution_trigger(trigger_point, ctx) -> bool
    AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *interfaces.AgentCallbackContext) bool

    // SnapshotForEvolution 同步捕获快照（cbc 仍活跃），供后台演化任务使用。
    // 对齐 Python: _snapshot_for_evolution(trajectory, ctx) -> Optional[dict]
    SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *interfaces.AgentCallbackContext) *EvolutionSnapshot

    // RunEvolution 执行演化逻辑。
    // 异步模式下 cbc 不可用，数据来自 snapshot。
    // 对齐 Python: run_evolution(trajectory, ctx=None, *, snapshot=None)
    RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error
}
```

```go
// noOpExtension EvolutionExtension 的默认空实现。
// TrajectoryRail 直接使用此实例；子类可嵌入后只覆写需要的方法。
type noOpExtension struct{}

func (noOpExtension) OnBeforeInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { return nil }
func (noOpExtension) OnAfterModelCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { return nil }
func (noOpExtension) OnAfterToolCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { return nil }
func (noOpExtension) OnAfterInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { return nil }
func (noOpExtension) OnAfterTaskIteration(ctx context.Context, cbc *interfaces.AgentCallbackContext) error { return nil }
func (noOpExtension) OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *interfaces.AgentCallbackContext) error { return nil }
func (noOpExtension) AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *interfaces.AgentCallbackContext) bool { return true }
func (noOpExtension) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *interfaces.AgentCallbackContext) *EvolutionSnapshot {
    messages := collectMessagesFromTrajectory(traj)
    return &EvolutionSnapshot{Trajectory: traj, Messages: messages}
}
func (noOpExtension) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error { return nil }
```

### 2. evolution_rail.go — EvolutionRail 结构体

#### 枚举

```go
// EvolutionTriggerPoint 演化触发时机枚举。
// 对齐 Python: EvolutionTriggerPoint
type EvolutionTriggerPoint string

const (
    TriggerAfterInvoke         EvolutionTriggerPoint = "after_invoke"
    TriggerAfterModelCall      EvolutionTriggerPoint = "after_model_call"
    TriggerAfterToolCall       EvolutionTriggerPoint = "after_tool_call"
    TriggerAfterTaskIteration  EvolutionTriggerPoint = "after_task_iteration"
    TriggerNone                EvolutionTriggerPoint = "none"
)
```

#### 结构体

```go
// EvolutionRail 所有演化轨道的基类。
//
// 嵌入 DeepAgentRail，4 个 final 回调自动完成轨迹收集，
// 通过 ext EvolutionExtension 接口实现子类多态分派。
//
// 对齐 Python: EvolutionRail(DeepAgentRail)
type EvolutionRail struct {
    rails.DeepAgentRail
    // ext 扩展点接口，实现多态分派
    ext EvolutionExtension
    // trajectoryStore 轨迹存储
    trajectoryStore trajectory.TrajectoryStore
    // builder 轨迹构造器（跨 invoke 复用）
    builder *trajectory.TrajectoryBuilder
    // maxTrajectorySteps 最大轨迹步骤数
    maxTrajectorySteps int
    // evolutionTrigger 演化触发时机
    evolutionTrigger EvolutionTriggerPoint
    // asyncEvolution 是否异步执行演化
    asyncEvolution bool
    // evolutionSem 演化并发控制信号量（chan struct{} 容量为 maxConcurrentEvolution）
    evolutionSem chan struct{}
    // disabledSkills 禁用的技能名称集合
    disabledSkills map[string]bool
    // trajectorySink 轨迹快照写入端点
    trajectorySink trajectory.TrajectorySink
    // teamID 团队标识
    teamID string
    // memberRole 成员角色
    memberRole string
    // bgTasks 后台任务集合
    bgTasks map[*utils.BackgroundTask]bool
    // pendingHostEvents 待排空的主机事件缓冲
    pendingHostEvents []*stream.OutputSchema
}
```

#### 优先级

```go
// Priority 返回优先级 60。
// 对齐 Python: EvolutionRail.priority = 60
func (r *EvolutionRail) Priority() int { return 60 }
```

#### 构造函数 + Functional Options

```go
type EvolutionRailOption func(*EvolutionRail)

func WithTrajectoryStore(store trajectory.TrajectoryStore) EvolutionRailOption { ... }
func WithMaxTrajectorySteps(n int) EvolutionRailOption { ... }
func WithEvolutionTrigger(trigger EvolutionTriggerPoint) EvolutionRailOption { ... }
func WithAsyncEvolution(async bool) EvolutionRailOption { ... }
func WithMaxConcurrentEvolution(n int) EvolutionRailOption { ... }
func WithDisabledSkills(names []string) EvolutionRailOption { ... }

func NewEvolutionRail(ext EvolutionExtension, opts ...EvolutionRailOption) *EvolutionRail {
    r := &EvolutionRail{
        ext:                ext,
        trajectoryStore:    trajectory.NewInMemoryTrajectoryStore(),
        maxTrajectorySteps: 200,
        evolutionTrigger:   TriggerAfterInvoke,
        asyncEvolution:     true,
        evolutionSem:       make(chan struct{}, 1),
        disabledSkills:     make(map[string]bool),
        bgTasks:            make(map[*utils.BackgroundTask]bool),
        pendingHostEvents:  make([]*stream.OutputSchema, 0),
    }
    for _, opt := range opts {
        opt(r)
    }
    return r
}
```

#### GetCallbacks — 注册 5 个事件

```go
func (r *EvolutionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
    callbacks := r.DeepAgentRail.GetCallbacks()
    callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
        return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
        return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
        return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackAfterInvoke] = func(ctx context.Context, railCtx any) error {
        return r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackAfterTaskIteration] = func(ctx context.Context, railCtx any) error {
        return r.AfterTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    return callbacks
}
```

#### 4 个 Final 回调 + AfterTaskIteration

**BeforeInvoke**：
1. type switch `cbc.Inputs()` → `*InvokeInputs`，非 InvokeInputs 则 return
2. 调用 `resolveSessionID(ctx, inputs)` 解析 session ID
3. builder 复用判断：`r.builder != nil && r.builder.SessionID() == sessionID` → 复用
4. 否则新建 `TrajectoryBuilder`（含 memberID、memberRole meta）
5. 调用 `r.ext.OnBeforeInvoke(ctx, cbc)`

**AfterModelCall**：
1. guard: `r.builder == nil` → return
2. type switch → `*ModelCallInputs`，非 ModelCallInputs 则 return
3. 构建 `LLMCallDetail`：调用 `splitResponseTokenFields` 分离 token 字段
4. 构建 `TrajectoryStep{Kind: StepKindLLM, Detail: llmDetail, PromptTokenIDs, CompletionTokenIDs, Logprobs, Meta}`
5. `r.builder.RecordStep(step)`
6. `r.ext.OnAfterModelCall(ctx, cbc)`
7. if `r.evolutionTrigger == TriggerAfterModelCall && r.ext.AllowEvolutionTrigger(...)` → `r.triggerEvolution(traj, cbc)`

**AfterToolCall**：
1. guard: `r.builder == nil` → return
2. type switch → `*ToolCallInputs`，非 ToolCallInputs 则 return
3. 构建 `ToolCallDetail{ToolName, CallArgs, CallResult, ToolCallID}`
4. 构建 `TrajectoryStep{Kind: StepKindTool, Detail: toolDetail, Meta}`
5. `r.builder.RecordStep(step)`
6. `r.ext.OnAfterToolCall(ctx, cbc)`
7. if `r.evolutionTrigger == TriggerAfterToolCall && r.ext.AllowEvolutionTrigger(...)` → `r.triggerEvolution(traj, cbc)`

**AfterTaskIteration**：
1. `r.ext.OnAfterTaskIteration(ctx, cbc)`
2. if `r.evolutionTrigger == TriggerAfterTaskIteration && r.ext.AllowEvolutionTrigger(...)` → `r.triggerEvolution(traj, cbc)`

**AfterInvoke**：
1. guard: `r.builder == nil` → return
2. `traj := r.buildTrajectory()` → snapshot steps
3. `r.trajectoryStore.Save(traj, "")`
4. `r.publishTrajectorySnapshot(traj)` — 如果 trajectorySink != nil 且 teamID 非空
5. `r.ext.OnAfterInvoke(ctx, cbc)`
6. if `r.evolutionTrigger == TriggerAfterInvoke && r.ext.AllowEvolutionTrigger(...)`:
   - `r.triggerEvolution(traj, cbc)`
   - `r.ext.OnAfterEvolutionTriggered(ctx, traj, cbc)`

#### triggerEvolution — 异步/同步分派

```go
func (r *EvolutionRail) triggerEvolution(traj *trajectory.Trajectory, cbc *interfaces.AgentCallbackContext) error {
    if r.asyncEvolution {
        // Phase 1: 同步捕获快照（cbc 仍活跃）
        snapshot := r.ext.SnapshotForEvolution(context.Background(), traj, cbc)
        if snapshot == nil {
            return nil
        }
        // Phase 2: 启动后台任务
        bgTask, err := utils.CreateBackgroundTask(context.Background(),
            func(ctx context.Context) error {
                return r.safeRunEvolution(ctx, snapshot)
            },
            "evolution-"+snapshotKey(snapshot), "evolution",
        )
        if err != nil {
            logger.Warn(logger.ComponentAgentCore).Err(err).Msg("创建演化后台任务失败")
            return err
        }
        r.bgTasks[bgTask] = true
        // 清理已完成任务
        for t := range r.bgTasks {
            if t.Done() {
                delete(r.bgTasks, t)
            }
        }
    } else {
        // 同步模式
        return r.ext.RunEvolution(context.Background(), traj, nil)
    }
    return nil
}
```

#### safeRunEvolution — 后台安全执行

```go
func (r *EvolutionRail) safeRunEvolution(ctx context.Context, snapshot *EvolutionSnapshot) error {
    // 获取信号量
    r.evolutionSem <- struct{}{}
    defer func() { <-r.evolutionSem }()

    var outcome map[string]string
    traj := snapshot.Trajectory
    err := r.ext.RunEvolution(ctx, traj, snapshot)
    if err != nil {
        outcome = map[string]string{"status": "failed", "message": err.Error()}
        logger.Warn(logger.ComponentAgentCore).Err(err).Msg("后台演化执行失败")
    }
    if outcome != nil {
        r.emitBackgroundOutcomeEvent(outcome)
    }
    return err
}
```

#### 公开访问器方法

| 方法 | 签名 | 说明 |
|---|---|---|
| `TrajectoryStore()` | `trajectory.TrajectoryStore` | 获取轨迹存储 |
| `DisabledSkills()` | `map[string]bool` | 获取禁用技能集合 |
| `Builder()` | `*trajectory.TrajectoryBuilder` | 获取当前 builder |
| `SetTrajectorySink()` | `func(sink trajectory.TrajectorySink, teamID string, memberRole ...string)` | 绑定轨迹写入端点 |
| `EmitHostEvent()` | `func(event *stream.OutputSchema)` | 缓存一个主机事件 |
| `DrainPendingHostEvents()` | `func(wait bool, timeout *time.Duration) []*stream.OutputSchema` | 排空主机事件 |
| `DrainPendingApprovalEvents()` | `func(wait bool, timeout *time.Duration) []*stream.OutputSchema` | 兼容别名 |
| `CleanupBackgroundTasks()` | `func(ctx context.Context) error` | 取消所有后台任务 |

#### 非导出辅助方法

| 方法 | 说明 |
|---|---|
| `resolveSessionID(cbc, inputs) string` | 解析运行时 session ID |
| `buildTrajectory() *Trajectory` | 从 builder 构建 trajectory 并 snapshot steps |
| `saveTrajectory(traj)` | 保存到 trajectoryStore |
| `publishTrajectorySnapshot(traj)` | 通过 Sink 发布成员轨迹快照 |
| `triggerEvolution(traj, cbc) error` | 异步/同步演化分派 |
| `safeRunEvolution(ctx, snapshot) error` | 后台安全执行（信号量+异常捕获） |
| `emitBackgroundOutcomeEvent(outcome)` | 将后台执行结果写入 host event 缓冲 |
| `collectPendingHostEvents() []*OutputSchema` | 返回并清空主机事件缓冲 |
| `resetTrajectoryBuilder()` | 重置 builder（子类生命周期边界用） |

### 3. trajectory_rail.go — TrajectoryRail

```go
// TrajectoryRail 纯轨迹收集轨道，不触发任何演化逻辑。
//
// 嵌入 EvolutionRail，使用 noOpExtension 作为扩展点实现。
// 对齐 Python: TrajectoryRail(priority=10)
type TrajectoryRail struct {
    *EvolutionRail
}

// NewTrajectoryRail 创建纯轨迹收集轨道。
func NewTrajectoryRail(opts ...EvolutionRailOption) *TrajectoryRail {
    rail := NewEvolutionRail(noOpExtension{}, opts...)
    return &TrajectoryRail{EvolutionRail: rail}
}

// Priority 返回优先级 10。
// 对齐 Python: TrajectoryRail.priority = 10
func (r *TrajectoryRail) Priority() int { return 10 }
```

### 4. helpers.go — 辅助函数

| 函数 | Go 签名 | 对齐 Python |
|---|---|---|
| `splitResponseTokenFields` | `func splitResponseTokenFields(response *llmschema.AssistantMessage) (map[string]any, []int, []int, any)` | `_split_response_token_fields` |
| `normalizeSkillNames` | `func normalizeSkillNames(raw any) map[string]bool` | `_normalize_skill_names` |
| `normalizeMemberRole` | `func normalizeMemberRole(role any) *string` | `_normalize_member_role` |
| `collectMessagesFromTrajectory` | `func collectMessagesFromTrajectory(traj *trajectory.Trajectory) []map[string]any` | `_collect_messages_from_trajectory` |

`collectMessagesFromTrajectory` 内部流程：
1. 调用 `signal.ConversationSignalDetector{}.ConvertTrajectoryToMessages(traj)` 获取消息列表
2. 调用 `normalizeCallbackMessages` 将 BaseMessage 规范化为 `map[string]any`
3. 去重（对齐 Python deduped 逻辑）

### 5. extractor.go — resourceManager any 消除

#### 修改内容

**字段类型**：
```go
// 之前
resourceManager any

// 之后
resourceManager *resources_manager.ResourceMgr
```

**构造函数**：
```go
// 之前
func NewTracerTrajectoryExtractor(resourceManager ...any) *TracerTrajectoryExtractor

// 之后
func NewTracerTrajectoryExtractor(resourceManager ...*resources_manager.ResourceMgr) *TracerTrajectoryExtractor
```

**buildToolDetail 中实现 GetToolInfos 调用**：
```go
if e.resourceManager != nil && toolName != "" {
    toolInfos, err := e.resourceManager.GetToolInfos([]string{toolName}, nil)
    if err == nil && len(toolInfos) > 0 {
        info := toolInfos[0]
        toolDescription = info.GetDescription()
        // toolSchema 从 info.Parameters() 提取
        if params := info.Parameters(); params != nil {
            toolSchema = params
        }
    }
}
```

**新增 import**：
```go
import (
    "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
)
```

**依赖方向验证**：
```
trajectory (evolving) → resources_manager (agentcore/runner)  ✅ 单向无循环
resources_manager 不导入 trajectory                          ✅
```

## 测试策略

### extension_test.go

- `noOpExtension` 所有方法返回零值/true
- `noOpExtension.SnapshotForEvolution` 正确构建 EvolutionSnapshot（trajectory + messages）

### evolution_rail_test.go

- `NewEvolutionRail` 构造函数默认值校验
- `NewEvolutionRail` 所有 Functional Options 生效
- `EvolutionRail.GetCallbacks()` 注册 5 个事件
- `EvolutionRail.Priority()` 返回 60
- `BeforeInvoke`：新建 builder / 复用已有 builder
- `AfterModelCall`：正确构建 LLMCallDetail + TrajectoryStep + RecordStep
- `AfterToolCall`：正确构建 ToolCallDetail + TrajectoryStep + RecordStep
- `AfterInvoke`：保存轨迹 + publish snapshot + 调用扩展点
- `AfterTaskIteration`：调用扩展点
- `triggerEvolution` 异步模式：创建 BackgroundTask + 信号量控制
- `triggerEvolution` 同步模式：直接调用 RunEvolution
- `safeRunEvolution`：异常捕获 + emitBackgroundOutcomeEvent
- `DrainPendingHostEvents` / `EmitHostEvent`
- `SetTrajectorySink` + `publishTrajectorySnapshot`
- `CleanupBackgroundTasks`
- 使用 mockEvolutionExtension 验证扩展点调用顺序和参数

### trajectory_rail_test.go

- `NewTrajectoryRail` 构造函数
- `TrajectoryRail.Priority()` 返回 10
- 集成验证：BeforeInvoke → AfterModelCall → AfterToolCall → AfterInvoke 完整流程

### helpers_test.go

- `splitResponseTokenFields`：正常分离 / nil response / 无 token 字段
- `normalizeSkillNames`：nil / string / []string / 空 / 混合
- `normalizeMemberRole`：nil / string / 枚举 value / 空
- `collectMessagesFromTrajectory`：正常轨迹 / nil / 去重验证

### extractor_test.go 更新

- `NewTracerTrajectoryExtractor` 接受 `*resources_manager.ResourceMgr` 参数
- `buildToolDetail` 中 GetToolInfos 调用路径验证

## 实现顺序

1. helpers.go + helpers_test.go（无外部依赖，最先实现）
2. extension.go + extension_test.go（定义接口，EvolutionRail 依赖）
3. evolution_rail.go + evolution_rail_test.go（核心结构体）
4. trajectory_rail.go + trajectory_rail_test.go（最简子类）
5. extractor.go + extractor_test.go 更新（any 消除，独立任务）
6. doc.go 更新
