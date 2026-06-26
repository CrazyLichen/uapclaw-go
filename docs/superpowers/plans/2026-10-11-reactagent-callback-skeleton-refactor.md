# ReActAgent 回调骨架重构与输入输出变换修复 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 去掉 WarpBaseAgent/AgentInvoker，将回调骨架直接写入 ReActAgent.Invoke/Stream；修复 InvokeImpl 闭包内未从 cbc.Inputs() 重新取值的问题；合并冗余错误判断。

**Architecture:** WarpBaseAgent 简化为 BaseAgent（纯配置/管理容器），ReActAgent 直接实现 Invoke/Stream 并在方法体内显式写回调骨架（transform_io → emit_before → invokeImpl → transform_io → emit_after），与 FireLifecycle/RailExecutor 的调用风格一致。InvokeImpl/StreamImpl 降级为非导出方法。

**Tech Stack:** Go 1.24+, 现有 callback/rail 包

---

### Task 1: 简化 base.go — WarpBaseAgent → BaseAgent

**Files:**
- Modify: `internal/agentcore/single_agent/base.go`

- [ ] **Step 1: 重写 base.go**

将 WarpBaseAgent 重命名为 BaseAgent，去掉 AgentInvoker 接口、invoker 字段、SetInvoker、Invoke、Stream 方法。保留的导出方法：NewBaseAgent、Configure、Card、Config、AbilityManager、CallbackManager、AgentID、RegisterCallback、RegisterRail、UnregisterRail。

```go
package single_agent

import (
	"context"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/resource"
	agentschema "github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseAgent Agent 基础配置/管理容器。
// 不实现 Invoke/Stream，子类自行实现并在方法体内调用回调骨架。
//
// 对应 Python: BaseAgent（不含 _AgentMeta 装饰逻辑）
type BaseAgent struct {
	// card Agent 身份卡片（必需）
	card *agentschema.AgentCard
	// config Agent 配置（可选，Configure 时设置）
	config interfaces.AgentConfig
	// abilityManager 能力管理器
	abilityManager *ability.AbilityManager
	// callbackManager 回调管理器
	callbackManager *rail.AgentCallbackManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// AgentOption Agent 调用选项函数（re-export from interfaces）。
type AgentOption = interfaces.AgentOption

// 以下类型别名为子包 re-export，保持包内兼容。
type (
	// AbilityManager 能力管理器（re-export from ability 子包）
	AbilityManager = ability.AbilityManager
	// AddAbilityResult 添加能力结果（re-export from ability 子包）
	AddAbilityResult = ability.AddAbilityResult
	// ExecuteResult 工具执行结果（re-export from ability 子包）
	ExecuteResult = ability.ExecuteResult
	// AbilityExecutionError 能力执行错误（re-export from ability 子包）
	AbilityExecutionError = ability.AbilityExecutionError
	// ResourceManager 资源管理器接口（re-export from resource 子包）
	ResourceManager = resource.ResourceManager
	// NoopResourceManager 空资源管理器（re-export from resource 子包）
	NoopResourceManager = resource.NoopResourceManager
	// ResourceOptions 资源选项（re-export from resource 子包）
	ResourceOptions = resource.ResourceOptions
	// ResourceOption 资源选项函数（re-export from resource 子包）
	ResourceOption = resource.ResourceOption
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseAgent 创建 BaseAgent 实例。
func NewBaseAgent(card *agentschema.AgentCard, resourceMgr resource.ResourceManager) *BaseAgent {
	return &BaseAgent{
		card:            card,
		abilityManager:  ability.NewAbilityManager(resourceMgr),
		callbackManager: rail.NewAgentCallbackManager(card.ID),
	}
}

// Configure 配置 Agent。
// 对应 Python: BaseAgent.configure(config)
func (b *BaseAgent) Configure(_ context.Context, config interfaces.AgentConfig) error {
	b.config = config
	return nil
}

// Card 返回 Agent 身份卡片。
func (b *BaseAgent) Card() *agentschema.AgentCard { return b.card }

// Config 返回当前配置。
func (b *BaseAgent) Config() interfaces.AgentConfig { return b.config }

// AbilityManager 返回能力管理器。
// 返回 any，调用方通过类型断言获取 *ability.AbilityManager。
func (b *BaseAgent) AbilityManager() any { return b.abilityManager }

// CallbackManager 返回回调管理器。
func (b *BaseAgent) CallbackManager() *rail.AgentCallbackManager { return b.callbackManager }

// AgentID 返回 Agent 唯一标识。
// 满足 rail.RailAgent 最小接口。
func (b *BaseAgent) AgentID() string {
	if b.card != nil {
		return b.card.ID
	}
	return ""
}

// RegisterCallback 注册回调。
func (b *BaseAgent) RegisterCallback(ctx context.Context, event any, fn any, opts ...callback.CallbackOption) error {
	if b.callbackManager != nil {
		b.callbackManager.RegisterCallback(ctx, event.(rail.AgentCallbackEvent), fn.(callback.PerAgentCallbackFunc), opts...)
	}
	return nil
}

// RegisterRail 注册 Rail。
func (b *BaseAgent) RegisterRail(ctx context.Context, r rail.AgentRail, opts ...callback.CallbackOption) error {
	if b.callbackManager != nil {
		if err := r.Init(b); err != nil {
			return err
		}
		return b.callbackManager.RegisterRail(ctx, r, opts...)
	}
	return nil
}

// UnregisterRail 注销 Rail。
func (b *BaseAgent) UnregisterRail(ctx context.Context, r rail.AgentRail) error {
	if b.callbackManager != nil {
		err := b.callbackManager.UnregisterRail(ctx, r)
		if uninitErr := r.Uninit(b); uninitErr != nil {
			if err == nil {
				return uninitErr
			}
		}
		return err
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：UnregisterRail 中 uninit 错误的日志记录已移除（简化），如需保留可在后续补充。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/... 2>&1 | head -20`

此时会有编译错误（react_agent.go 等引用 WarpBaseAgent），先记录错误，下一步修复。

---

### Task 2: 修改 ReActAgent — 替换 WarpBaseAgent 为 BaseAgent + 新增 Invoke/Stream

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_agent.go`
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go`

- [ ] **Step 1: 修改 react_agent.go**

1. 将 `base *single_agent.WarpBaseAgent` 改为 `base *single_agent.BaseAgent`
2. 将 `single_agent.NewWarpBaseAgent` 改为 `single_agent.NewBaseAgent`
3. 删除 `base.SetInvoker(agent)` 行
4. 更新注释，去掉 AgentInvoker 虚分发描述
5. 将 `card` 字段添加到 ReActAgent（因为 BaseAgent 不再暴露 Invoke/Stream，ReActAgent 需要直接持有 card 来在 Invoke/Stream 中访问 card.ID/Name）

```go
// ReActAgent ReAct 循环 Agent：Think → Act → Observe。
//
// 内嵌 BaseAgent 获取配置/管理能力，
// 自行实现 Invoke/Stream，在方法体内显式调用回调骨架。
//
// 对应 Python: ReActAgent (openjiuwen/core/single_agent/agents/react_agent.py)
type ReActAgent struct {
	// base 基础 Agent（提供 Configure/Card/AbilityManager/CallbackManager 等方法）
	base *single_agent.BaseAgent
	// config Agent 配置
	config *saconfig.ReActAgentConfig
	// contextEngine 上下文引擎
	contextEngine ceinterface.ContextEngine
	// llm LLM 模型实例（延迟初始化）
	llm *llm.Model
	// promptBuilder 系统提示词构建器
	promptBuilder *SystemPromptBuilder
	// llmOnce LLM 初始化同步原语
	llmOnce sync.Once
	// kvReleaseWarningLogged KV cache 释放不支持的一次性警告标记
	kvReleaseWarningLogged bool
	// hitlHandler HITL 中断处理器
	hitlHandler *interrupt.ToolInterruptHandler
}
```

构造函数：
```go
func NewReActAgent(card *agentschema.AgentCard, config *saconfig.ReActAgentConfig) *ReActAgent {
	base := single_agent.NewBaseAgent(card, &resource.NoopResourceManager{})

	agent := &ReActAgent{
		base:          base,
		config:        config,
		promptBuilder: NewSystemPromptBuilder(),
	}

	agent.hitlHandler = interrupt.NewToolInterruptHandler(agent)

	return agent
}
```

- [ ] **Step 2: 修改 react_invoke.go — 新增公开 Invoke/Stream，降级 InvokeImpl/StreamImpl**

1. 将 `InvokeImpl` 重命名为 `invokeImpl`（非导出）
2. 将 `StreamImpl` 重命名为 `streamImpl`（非导出）
3. 在文件开头（导出函数区）新增 `Invoke` 和 `Stream` 方法，内含回调骨架
4. 在 `invokeImpl` 闭包内改用 `cbc.Inputs()` 重新取值
5. 合并冗余错误判断

**新增 Invoke 方法：**

```go
// Invoke 非流式调用，包含回调包装骨架。
// 执行顺序：① transform_io input → ② emit_before → ③ invokeImpl → ④ transform_io output → ⑤ emit_after
//
// 对应 Python: _AgentMeta 元类装饰后的 invoke
func (a *ReActAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	fw := callback.GetCallbackFramework()
	agentOpts := interfaces.NewAgentOptions(opts...)

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformAgentIOInput(ctx, callback.GlobalAgentInvokeInput, inputs); transformed != nil {
		if v, ok := transformed.(map[string]any); ok {
			inputs = v
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event", "TransformAgentIOInput").
				Str("agent_id", a.base.Card().ID).
				Str("expected", "map[string]any").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO 返回类型不匹配，使用原始输入")
		}
	}

	// ② emit_before: 触发全局 AgentInvokeInput 事件
	fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
		Event:     callback.GlobalAgentInvokeInput,
		AgentID:   a.base.Card().ID,
		AgentName: a.base.Card().Name,
		Inputs:    inputs,
		Session:   agentOpts.Session,
	})

	// ③ 执行真实逻辑
	result, err := a.invokeImpl(ctx, inputs, opts...)
	if err != nil {
		// context.Canceled 时清除上下文消息
		if ctx.Err() == context.Canceled {
			a.ClearContextMessages(agentOpts.Session)
		}
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		logger.Error(logger.ComponentAgentCore).
			Str("agent_id", a.base.Card().ID).
			Err(err).
			Msg("Agent invoke 错误")
		return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
			exception.WithCause(err))
	}

	// ④ transform_io 输出变换（对齐 Python transform_io 的 output_fn）
	result = fw.TransformAgentIOOutput(ctx, callback.GlobalAgentInvokeOutput, result)

	// ⑤ emit_after: 触发全局 AgentInvokeOutput 事件
	fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
		Event:     callback.GlobalAgentInvokeOutput,
		AgentID:   a.base.Card().ID,
		AgentName: a.base.Card().Name,
		Result:    result,
	})

	return result, nil
}
```

**新增 Stream 方法：** 与当前 WarpBaseAgent.Stream 逻辑一致，但改为调用 `a.streamImpl`。

- [ ] **Step 3: 修改 invokeImpl — 改用 cbc.Inputs() + 合并错误判断**

在 FireLifecycle 闭包内，将所有 `invokeInputs.XXX` 改为从 `cbc.Inputs()` 重新取值：

```go
err := cbc.FireLifecycle(rail.CallbackBeforeInvoke, rail.CallbackAfterInvoke, func() error {
    // 从 cbc 重新取 inputs（对齐 Python: user_input = ctx.inputs.query）
    // before_invoke 钩子可能修改 cbc.inputs，必须从 cbc 重新取值
    curInputs := invokeInputs
    if ci, ok := cbc.Inputs().(*rail.InvokeInputs); ok && ci != nil {
        curInputs = ci
    }

    if curInputs.Query.PlainText() == "" && !curInputs.Query.IsInteractiveInput() {
        return fmt.Errorf("input must contain 'query'")
    }

    // ... HITL 恢复中 UserInput: curInputs.Query ...
    // ... 正常路径中 plainText := curInputs.Query.PlainText() ...
    // ... 判断中 if curInputs.Result == nil ...
    return loopErr
})
```

FireLifecycle 之后的返回逻辑：

```go
if err != nil {
    // context.Canceled 时清除上下文消息（合并错误判断）
    if ctx.Err() == context.Canceled {
        a.ClearContextMessages(sess)
    }
    return nil, err
}

// 对齐 Python L1434: return ctx.extra.get("invoke_result", invoke_inputs.result)
if invokeResult, ok := cbc.Extra()["invoke_result"]; ok {
    if r, ok2 := invokeResult.(map[string]any); ok2 {
        return r, nil
    }
}

if curInputs, ok := cbc.Inputs().(*rail.InvokeInputs); ok && curInputs.Result != nil {
    return curInputs.Result, nil
}
return result, nil
```

- [ ] **Step 4: 添加必要 import**

react_invoke.go 需要新增 import:
- `"github.com/uapclaw/uap-claw-go/internal/agentcore/runner/callback"`
- `"github.com/uapclaw/uap-claw-go/internal/common/exception"`

react_agent.go 需要新增 import（如果 Invoke/Stream 中用到 fmt）:
- `"fmt"` (如 Invoke 中 TransformIO 类型不匹配日志)

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/... 2>&1 | head -20`

---

### Task 3: 修改 innerStream — 去掉 a.base.Invoke 调用

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go`

- [ ] **Step 1: 修改 innerStream 中的 a.base.Invoke 调用**

当前 `innerStream` 的 `streamProcess` 中调用 `a.base.Invoke(ctx, inputs, opts...)` 走虚分发。
重构后应改为调用 `a.Invoke(ctx, inputs, opts...)`（ReActAgent 自身的公开方法）。

```go
// 在 streamProcess 闭包中：
result, err := a.Invoke(ctx, inputs, opts...)
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/... 2>&1 | head -20`

---

### Task 4: 修改 react_helpers.go / react_prompt.go — 适配 BaseAgent

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_helpers.go`
- Modify: `internal/agentcore/single_agent/agents/react_prompt.go`

- [ ] **Step 1: 检查 a.base.XXX 调用**

当前已知引用：
- `a.base.AbilityManager()` (react_helpers.go L100) — 不变，BaseAgent 保留此方法
- `a.base.CallbackManager()` (react_prompt.go L44) — 不变
- `a.base.AgentID()` (react_prompt.go L49) — 不变

这些方法在 BaseAgent 中都有，无需修改。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/... 2>&1 | head -20`

---

### Task 5: 更新 base_test.go — 适配 BaseAgent 重命名

**Files:**
- Modify: `internal/agentcore/single_agent/base_test.go`

- [ ] **Step 1: 重写 base_test.go**

1. 全局替换 `WarpBaseAgent` → `BaseAgent`
2. 全局替换 `NewWarpBaseAgent` → `NewBaseAgent`
3. 删除 `AgentInvoker`/`stubInvoker`/`testSubAgent`/`SetInvoker` 相关测试
4. 删除 `TestWarpBaseAgent_Invoke_*` 和 `TestWarpBaseAgent_Stream_*` 系列测试（骨架逻辑已移入 ReActAgent，不再由 BaseAgent 测试）
5. 保留 `TestNewBaseAgent`（重命名）、`TestBaseAgent_Configure`、`TestBaseAgent_访问器`、`TestBaseAgent_AgentID*`、`TestBaseAgent_RegisterCallback*`、`TestBaseAgent_RegisterRail*`、`TestBaseAgent_UnregisterRail*` 等纯 BaseAgent 方法测试

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/... -count=1 -v 2>&1 | tail -30`

---

### Task 6: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/single_agent/doc.go`
- Modify: `internal/agentcore/single_agent/agents/doc.go`

- [ ] **Step 1: 更新 single_agent/doc.go**

将 `WarpBaseAgent — BaseAgent 默认实现，Invoke/Stream 回调包装骨架` 改为 `BaseAgent — Agent 基础配置/管理容器`

- [ ] **Step 2: 更新 agents/doc.go**

1. 将 `由 base.go 中的 WarpBaseAgent 提供公共委托实现` 改为 `由 BaseAgent 提供配置/管理能力，子类自行实现 Invoke/Stream`
2. 更新 `react_invoke.go` 描述：`Invoke/Stream 入口 + invokeImpl/streamImpl + ReAct 循环`

---

### Task 7: 更新 rail_test.go 注释

**Files:**
- Modify: `internal/agentcore/single_agent/rail/rail_test.go`

- [ ] **Step 1: 修改注释**

将 `TestRailAgent_接口满足 测试 WarpBaseAgent 隐式满足 RailAgent 接口` 改为 `TestRailAgent_接口满足 测试 BaseAgent 隐式满足 RailAgent 接口`

---

### Task 8: 全量编译 + 测试验证

**Files:**
- 无新增修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./... 2>&1 | head -30`

- [ ] **Step 2: 运行相关包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/... -count=1 -v 2>&1 | tail -50`

- [ ] **Step 3: 提交变更**

```
git add -A
git commit -m "refactor: 去掉 WarpBaseAgent/AgentInvoker，回调骨架直接写入 ReActAgent.Invoke/Stream；修复 InvokeImpl 闭包内未从 cbc.Inputs() 重新取值；合并冗余错误判断"
```
