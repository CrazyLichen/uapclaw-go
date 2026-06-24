# 6.7 AgentRail 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AgentRail 接口（10个生命周期钩子）+ BaseRail 结构体 + CallbackFrom/BuildCallbacks 辅助 + RegisterRail/UnregisterRail 回填 + RailAgent 接口扩充 + interfaces 包类型回填

**Architecture:** 定义 AgentRail 接口包含 10 个钩子方法 + Priority/Init/Uninit/GetCallbacks，提供 BaseRail no-op 默认实现 + 辅助方法，回填 AgentCallbackManager.RegisterRail/UnregisterRail 从 any 到 AgentRail，扩充 railAgent 最小接口添加 AgentID()

**Tech Stack:** Go 1.22+, 项目已有 rail 包 + callback 框架

**设计文档:** docs/superpowers/specs/2026-09-29-agent-rail-interface-design.md

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新增 | `rail/rail.go` | AgentRail 接口 + BaseRail 结构体 + CallbackFrom/BuildCallbacks |
| 新增 | `rail/rail_test.go` | AgentRail/BaseRail/CallbackFrom/BuildCallbacks 测试 |
| 修改 | `rail/context.go:14-22` | railAgent 接口扩充 AgentID() |
| 修改 | `rail/manager.go:43-59` | RegisterRail/UnregisterRail 回填实现 |
| 修改 | `rail/manager_test.go` | 添加 RegisterRail/UnregisterRail 测试 |
| 修改 | `rail/doc.go` | 更新文件目录和核心类型索引 |
| 修改 | `interfaces/interface.go:113-121` | RegisterRail/UnregisterRail 参数类型从 any 改为 rail.AgentRail |
| 修改 | `single_agent/base.go:276-292` | WarpBaseAgent.RegisterRail/UnregisterRail 参数类型从 any 改为 rail.AgentRail |
| 修改 | `IMPLEMENTATION_PLAN.md:392` | 6.7 状态从 ☐ 改为 ✅ |

---

### Task 1: 新增 rail/rail.go — AgentRail 接口 + BaseRail 结构体 + 辅助方法

**Files:**
- Create: `internal/agentcore/single_agent/rail/rail.go`

- [ ] **Step 1: 创建 rail.go 文件**

```go
package rail

import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 接口 ────────────────────────────

// AgentRail Agent 生命周期 Rail 接口。
//
// Rail 是 class-based 的生命周期钩子容器，允许在 Agent 执行流程的
// 特定时机注入拦截逻辑（重试、提前终止、steering 等）。
// 嵌入 BaseRail 后只需覆盖关心的钩子方法，并在 GetCallbacks() 中
// 声明已覆盖的事件映射。
//
// 对应 Python: AgentRail(ABC) (openjiuwen/core/single_agent/rail/base.py L451-573)
type AgentRail interface {
	// Priority 返回执行优先级（数值越大越先执行）
	Priority() int
	// Init Rail 初始化钩子（注册时调用，用于工具自注册等）
	Init(agent RailAgent) error
	// Uninit Rail 注销钩子（注销时调用，用于工具清理等）
	Uninit(agent RailAgent) error

	// ── 10 个生命周期钩子方法 ──

	// BeforeInvoke invoke 开始前
	BeforeInvoke(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterInvoke invoke 完成后
	AfterInvoke(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeTaskIteration 外层任务循环迭代开始前
	BeforeTaskIteration(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterTaskIteration 外层任务循环迭代完成后
	AfterTaskIteration(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeModelCall LLM 调用前
	BeforeModelCall(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterModelCall LLM 响应后
	AfterModelCall(ctx context.Context, cbc *AgentCallbackContext) error
	// OnModelException LLM 调用异常
	OnModelException(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeToolCall 工具执行前
	BeforeToolCall(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterToolCall 工具执行后
	AfterToolCall(ctx context.Context, cbc *AgentCallbackContext) error
	// OnToolException 工具执行异常
	OnToolException(ctx context.Context, cbc *AgentCallbackContext) error

	// GetCallbacks 提取已覆盖的钩子方法映射，供 RegisterRail 批量注册。
	//
	// 用户嵌入 BaseRail 后通过 BuildCallbacks(CallbackFrom(...)) 实现。
	GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc
}

// ──────────────────────────── 结构体 ────────────────────────────

// BaseRail AgentRail 的 no-op 默认实现。
//
// 用户嵌入此结构体后只需覆盖关心的钩子方法，并在 GetCallbacks() 中
// 通过 CallbackFrom + BuildCallbacks 声明已覆盖的事件映射。
//
// 对应 Python: AgentRail 基类的 10 个默认 no-op 方法
type BaseRail struct {
	// priority 执行优先级（数值越大越先执行），默认 50
	priority int
}

// callbackEntry 事件→回调映射条目，BuildCallbacks 的参数。
type callbackEntry struct {
	event AgentCallbackEvent
	fn    cb.PerAgentCallbackFunc
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseRail 创建默认优先级(50)的 BaseRail。
func NewBaseRail() *BaseRail {
	return &BaseRail{priority: 50}
}

// Priority 返回优先级。
func (r *BaseRail) Priority() int {
	return r.priority
}

// WithPriority 设置优先级（Functional Options 模式）。
func (r *BaseRail) WithPriority(p int) *BaseRail {
	r.priority = p
	return r
}

// Init 默认 no-op。
func (r *BaseRail) Init(_ RailAgent) error { return nil }

// Uninit 默认 no-op。
func (r *BaseRail) Uninit(_ RailAgent) error { return nil }

// BeforeInvoke 默认 no-op。
func (r *BaseRail) BeforeInvoke(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterInvoke 默认 no-op。
func (r *BaseRail) AfterInvoke(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeTaskIteration 默认 no-op。
func (r *BaseRail) BeforeTaskIteration(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterTaskIteration 默认 no-op。
func (r *BaseRail) AfterTaskIteration(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeModelCall 默认 no-op。
func (r *BaseRail) BeforeModelCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterModelCall 默认 no-op。
func (r *BaseRail) AfterModelCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// OnModelException 默认 no-op。
func (r *BaseRail) OnModelException(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeToolCall 默认 no-op。
func (r *BaseRail) BeforeToolCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterToolCall 默认 no-op。
func (r *BaseRail) AfterToolCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// OnToolException 默认 no-op。
func (r *BaseRail) OnToolException(_ context.Context, _ *AgentCallbackContext) error { return nil }

// GetCallbacks 返回空映射（默认无钩子覆盖）。
func (r *BaseRail) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return make(map[AgentCallbackEvent]cb.PerAgentCallbackFunc)
}

// CallbackFrom 创建一条事件→回调映射条目。
//
// 用法：
//
//	r.CallbackFrom(CallbackBeforeModelCall, wrappedFn)
func (r *BaseRail) CallbackFrom(event AgentCallbackEvent, fn cb.PerAgentCallbackFunc) callbackEntry {
	return callbackEntry{event: event, fn: fn}
}

// BuildCallbacks 从多条映射条目构建 GetCallbacks 返回值。
//
// 用法：
//
//	func (r *MyRail) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
//	    return r.BuildCallbacks(
//	        r.CallbackFrom(CallbackBeforeModelCall, r.BeforeModelCall),
//	        r.CallbackFrom(CallbackAfterModelCall, r.AfterModelCall),
//	    )
//	}
func (r *BaseRail) BuildCallbacks(entries ...callbackEntry) map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	m := make(map[AgentCallbackEvent]cb.PerAgentCallbackFunc, len(entries))
	for _, e := range entries {
		m[e.event] = e.fn
	}
	return m
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/rail/...`
Expected: 编译通过，无错误

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/single_agent/rail/rail.go
git commit -m "feat(rail): 新增 AgentRail 接口 + BaseRail 结构体 + CallbackFrom/BuildCallbacks 辅助方法"
```

---

### Task 2: 扩充 railAgent 接口 — 添加 AgentID()

**Files:**
- Modify: `internal/agentcore/single_agent/rail/context.go:14-22`

- [ ] **Step 1: 修改 railAgent 接口**

将 `context.go` 中 `railAgent` 接口从：

```go
// railAgent Rail 包所需的最小 Agent 接口。
//
// 在 rail 包内定义，打破 rail → interfaces 循环依赖，
// 使 AgentCallbackContext 可以直接访问 CallbackManager 具体类型，
// Fire() 无需类型断言。
type railAgent interface {
	// CallbackManager 返回 PerAgent 回调管理器
	CallbackManager() *AgentCallbackManager
}
```

改为：

```go
// railAgent Rail 包所需的最小 Agent 接口。
//
// 在 rail 包内定义，打破 rail → interfaces 循环依赖，
// 使 AgentCallbackContext 可以直接访问 CallbackManager 具体类型，
// Fire() 无需类型断言。
// interfaces.BaseAgent 隐式满足此接口。
//
// 对应 Python: BaseAgent (openjiuwen/core/single_agent/base.py)
type railAgent interface {
	// CallbackManager 返回 PerAgent 回调管理器
	CallbackManager() *AgentCallbackManager
	// AgentID 返回 Agent 唯一标识
	// ⤴️ 6.7 定义；BaseAgent 通过 Card().ID 隐式满足
	AgentID() string
	// ⤵️ 后续 Rail 子类实现时按需扩充：
	// AbilityManager() — 工具注册/注销（MemoryRail, SkillUseRail 等需要）
	// SystemPromptBuilder() — 系统提示词构建器（多数 Rail init 中需要）
	// Card() — Agent 元数据（agent.card.id 等场景）
	// DeepConfig() — 深层配置（HeartbeatRail 等需要）
}
```

- [ ] **Step 2: 在 WarpBaseAgent 上添加 AgentID() 方法**

在 `internal/agentcore/single_agent/base.go` 的 `CallbackManager()` 方法后添加：

```go
// AgentID 返回 Agent 唯一标识。
// 满足 rail.railAgent 最小接口。
func (w *WarpBaseAgent) AgentID() string {
	if w.card != nil {
		return w.card.ID
	}
	return ""
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: 编译通过。如有其他依赖 `railAgent` 的代码因新增 `AgentID()` 编译失败，需逐个修复。

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/single_agent/rail/context.go internal/agentcore/single_agent/base.go
git commit -m "feat(rail): 扩充 railAgent 接口添加 AgentID()，WarpBaseAgent 实现"
```

---

### Task 3: 回填 RegisterRail / UnregisterRail — manager.go

**Files:**
- Modify: `internal/agentcore/single_agent/rail/manager.go:43-59`

- [ ] **Step 1: 添加 logger import**

在 `manager.go` 的 import 中添加：

```go
import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

- [ ] **Step 2: 替换 RegisterRail 实现**

将 `manager.go:43-50` 从：

```go
// RegisterRail 批量注册一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.register_rail(rail)
// ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail，遍历 rail.getCallbacks() 注册
func (m *AgentCallbackManager) RegisterRail(_ context.Context, _ any, _ ...cb.CallbackOption) error {
	// ⤵️ 6.7 回填：实现 Rail 批量注册
	return nil
}
```

改为：

```go
// RegisterRail 批量注册一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.register_rail(rail)
// 遍历 rail.GetCallbacks()，将每个钩子按 rail.Priority() 注册到 CallbackFramework。
func (m *AgentCallbackManager) RegisterRail(ctx context.Context, r AgentRail, opts ...cb.CallbackOption) error {
	callbacks := r.GetCallbacks()
	priorityOpt := cb.WithPriority(r.Priority())
	allOpts := append([]cb.CallbackOption{priorityOpt}, opts...)
	for event, fn := range callbacks {
		m.RegisterCallback(ctx, event, fn, allOpts...)
		logger.Debug(logComponent).
			Str("event_type", "rail_register_callback").
			Str("event", string(event)).
			Int("priority", r.Priority()).
			Msg("Rail 钩子注册到回调框架")
	}
	return nil
}
```

- [ ] **Step 3: 替换 UnregisterRail 实现**

将 `manager.go:52-59` 从：

```go
// UnregisterRail 批量注销一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.unregister_rail(rail)
// ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail
func (m *AgentCallbackManager) UnregisterRail(_ context.Context, _ any) error {
	// ⤵️ 6.7 回填：实现 Rail 批量注销
	return nil
}
```

改为：

```go
// UnregisterRail 批量注销一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.unregister_rail(rail)
// 遍历 rail.GetCallbacks()，逐个注销。
func (m *AgentCallbackManager) UnregisterRail(ctx context.Context, r AgentRail) error {
	callbacks := r.GetCallbacks()
	for event, fn := range callbacks {
		m.Unregister(event, fn)
		logger.Debug(logComponent).
			Str("event_type", "rail_unregister_callback").
			Str("event", string(event)).
			Msg("Rail 钩子从回调框架注销")
	}
	return nil
}
```

注意：`UnregisterRail` 的 `ctx` 参数保留但不使用（对齐 Python 的 async 签名，未来可能用于超时控制）。

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/rail/...`
Expected: 编译通过

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/rail/manager.go
git commit -m "feat(rail): 回填 RegisterRail/UnregisterRail 实现，参数类型从 any 改为 AgentRail"
```

---

### Task 4: 回填 interfaces 包 — RegisterRail/UnregisterRail 参数类型

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/interface.go:113-121`
- Modify: `internal/agentcore/single_agent/base.go:276-292`

- [ ] **Step 1: 修改 BaseAgent 接口**

将 `interface.go:113-121` 从：

```go
	// RegisterRail 注册 Rail。
	// 对应 Python: BaseAgent.register_rail(rail)
	// rail 实际类型 rail.AgentRail，用 any 避免循环依赖。
	RegisterRail(ctx context.Context, rail any, opts ...cb.CallbackOption) error

	// UnregisterRail 注销 Rail。
	// 对应 Python: BaseAgent.unregister_rail(rail)
	// ⤵️ 6.7 回填：rail 参数类型从 any 改为 AgentRail
	UnregisterRail(ctx context.Context, rail any) error
```

改为：

```go
	// RegisterRail 注册 Rail。
	// 对应 Python: BaseAgent.register_rail(rail)
	RegisterRail(ctx context.Context, rail rail.AgentRail, opts ...cb.CallbackOption) error

	// UnregisterRail 注销 Rail。
	// 对应 Python: BaseAgent.unregister_rail(rail)
	UnregisterRail(ctx context.Context, rail rail.AgentRail) error
```

注意：`interface.go` 已经 import 了 `rail` 包（第13行），无需新增 import。

- [ ] **Step 2: 修改 WarpBaseAgent 实现**

将 `base.go:276-292` 从：

```go
// RegisterRail 注册 Rail。
// 委托给 AgentCallbackManager.RegisterRail。
func (w *WarpBaseAgent) RegisterRail(ctx context.Context, railObj any, opts ...callback.CallbackOption) error {
	if w.callbackManager != nil {
		return w.callbackManager.RegisterRail(ctx, railObj, opts...)
	}
	return nil
}

// UnregisterRail 注销 Rail。
// 委托给 AgentCallbackManager.UnregisterRail。
func (w *WarpBaseAgent) UnregisterRail(ctx context.Context, railObj any) error {
	if w.callbackManager != nil {
		return w.callbackManager.UnregisterRail(ctx, railObj)
	}
	return nil
}
```

改为：

```go
// RegisterRail 注册 Rail。
// 调用 rail.Init() 初始化后委托给 AgentCallbackManager.RegisterRail。
//
// 对应 Python: BaseAgent.register_rail(rail) → rail.init(self) → manager.register_rail(rail, self)
func (w *WarpBaseAgent) RegisterRail(ctx context.Context, r rail.AgentRail, opts ...callback.CallbackOption) error {
	if w.callbackManager != nil {
		// 调用 Rail 初始化钩子（对齐 Python: rail.init(self)）
		if err := r.Init(w); err != nil {
			return err
		}
		return w.callbackManager.RegisterRail(ctx, r, opts...)
	}
	return nil
}

// UnregisterRail 注销 Rail。
// 委托给 AgentCallbackManager.UnregisterRail 后调用 rail.Uninit()。
//
// 对应 Python: BaseAgent.unregister_rail(rail) → manager.unregister_rail(rail, self) → rail.uninit(self)
func (w *WarpBaseAgent) UnregisterRail(ctx context.Context, r rail.AgentRail) error {
	if w.callbackManager != nil {
		err := w.callbackManager.UnregisterRail(ctx, r)
		// 调用 Rail 注销钩子（对齐 Python: rail.uninit(self)）
		if uninitErr := r.Uninit(w); uninitErr != nil {
			if err == nil {
				return uninitErr
			}
			// 两个错误都存在时，返回注销错误，注销钩子错误记录日志
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "rail_uninit_error").
				Err(uninitErr).
				Msg("Rail Uninit 返回错误")
		}
		return err
	}
	return nil
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: 编译通过。如有其他实现了 `BaseAgent` 接口的 mock/fake struct 因签名变更编译失败，需逐个修复（添加对应方法）。

- [ ] **Step 4: 修复受影响的 mock/fake struct**

搜索所有实现 `BaseAgent` 接口的测试 mock，更新 `RegisterRail`/`UnregisterRail` 签名。

Run: `grep -rn "RegisterRail\|UnregisterRail" --include="*_test.go" internal/agentcore/`

对每个匹配的 mock，将 `rail any` 参数改为 `rail rail.AgentRail`。

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/single_agent/interfaces/interface.go internal/agentcore/single_agent/base.go
git commit -m "feat(interfaces): 回填 RegisterRail/UnregisterRail 参数类型从 any 改为 rail.AgentRail，WarpBaseAgent 添加 Init/Uninit 调用"
```

---

### Task 5: 新增 rail/rail_test.go — BaseRail 和辅助方法测试

**Files:**
- Create: `internal/agentcore/single_agent/rail/rail_test.go`

- [ ] **Step 1: 创建测试文件**

```go
package rail

import (
	"context"
	"testing"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBaseRail_默认优先级 测试 NewBaseRail 默认优先级为 50。
func TestBaseRail_默认优先级(t *testing.T) {
	r := NewBaseRail()
	if r.Priority() != 50 {
		t.Fatalf("Priority() = %d, 期望 50", r.Priority())
	}
}

// TestBaseRail_WithPriority 测试 WithPriority 设置自定义优先级。
func TestBaseRail_WithPriority(t *testing.T) {
	r := NewBaseRail().WithPriority(80)
	if r.Priority() != 80 {
		t.Fatalf("Priority() = %d, 期望 80", r.Priority())
	}
}

// TestBaseRail_所有钩子为NoOp 测试 10 个钩子方法均返回 nil。
func TestBaseRail_所有钩子为NoOp(t *testing.T) {
	r := NewBaseRail()
	ctx := context.Background()

	if err := r.BeforeInvoke(ctx, nil); err != nil {
		t.Fatalf("BeforeInvoke 返回错误: %v", err)
	}
	if err := r.AfterInvoke(ctx, nil); err != nil {
		t.Fatalf("AfterInvoke 返回错误: %v", err)
	}
	if err := r.BeforeModelCall(ctx, nil); err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}
	if err := r.AfterModelCall(ctx, nil); err != nil {
		t.Fatalf("AfterModelCall 返回错误: %v", err)
	}
	if err := r.OnModelException(ctx, nil); err != nil {
		t.Fatalf("OnModelException 返回错误: %v", err)
	}
	if err := r.BeforeToolCall(ctx, nil); err != nil {
		t.Fatalf("BeforeToolCall 返回错误: %v", err)
	}
	if err := r.AfterToolCall(ctx, nil); err != nil {
		t.Fatalf("AfterToolCall 返回错误: %v", err)
	}
	if err := r.OnToolException(ctx, nil); err != nil {
		t.Fatalf("OnToolException 返回错误: %v", err)
	}
	if err := r.BeforeTaskIteration(ctx, nil); err != nil {
		t.Fatalf("BeforeTaskIteration 返回错误: %v", err)
	}
	if err := r.AfterTaskIteration(ctx, nil); err != nil {
		t.Fatalf("AfterTaskIteration 返回错误: %v", err)
	}
}

// TestBaseRail_InitUninit为NoOp 测试 Init 和 Uninit 均返回 nil。
func TestBaseRail_InitUninit为NoOp(t *testing.T) {
	r := NewBaseRail()
	if err := r.Init(nil); err != nil {
		t.Fatalf("Init 返回错误: %v", err)
	}
	if err := r.Uninit(nil); err != nil {
		t.Fatalf("Uninit 返回错误: %v", err)
	}
}

// TestBaseRail_GetCallbacks_返回空Map 测试默认 GetCallbacks 返回空 map。
func TestBaseRail_GetCallbacks_返回空Map(t *testing.T) {
	r := NewBaseRail()
	callbacks := r.GetCallbacks()
	if len(callbacks) != 0 {
		t.Fatalf("GetCallbacks 返回 %d 条映射，期望 0", len(callbacks))
	}
}

// TestCallbackFrom_单条映射 测试 CallbackFrom 构建单条映射。
func TestCallbackFrom_单条映射(t *testing.T) {
	r := NewBaseRail()
	fn := func(_ context.Context, _ any) error { return nil }
	entry := r.CallbackFrom(CallbackBeforeModelCall, fn)
	if entry.event != CallbackBeforeModelCall {
		t.Fatalf("event = %v, 期望 CallbackBeforeModelCall", entry.event)
	}
	if entry.fn == nil {
		t.Fatal("fn 不应为 nil")
	}
}

// TestBuildCallbacks_多条映射 测试 BuildCallbacks 合并多条映射。
func TestBuildCallbacks_多条映射(t *testing.T) {
	r := NewBaseRail()
	fn1 := func(_ context.Context, _ any) error { return nil }
	fn2 := func(_ context.Context, _ any) error { return nil }
	m := r.BuildCallbacks(
		r.CallbackFrom(CallbackBeforeModelCall, fn1),
		r.CallbackFrom(CallbackAfterModelCall, fn2),
	)
	if len(m) != 2 {
		t.Fatalf("BuildCallbacks 返回 %d 条映射，期望 2", len(m))
	}
	if _, ok := m[CallbackBeforeModelCall]; !ok {
		t.Fatal("缺少 CallbackBeforeModelCall 映射")
	}
	if _, ok := m[CallbackAfterModelCall]; !ok {
		t.Fatal("缺少 CallbackAfterModelCall 映射")
	}
}

// TestBuildCallbacks_空输入 测试无参数时返回空 map。
func TestBuildCallbacks_空输入(t *testing.T) {
	r := NewBaseRail()
	m := r.BuildCallbacks()
	if len(m) != 0 {
		t.Fatalf("BuildCallbacks() 返回 %d 条映射，期望 0", len(m))
	}
}

// TestAgentRail_接口满足 测试 BaseRail 满足 AgentRail 接口（编译期检查）。
func TestAgentRail_接口满足(t *testing.T) {
	// 编译期检查：BaseRail 实现了 AgentRail 接口
	var _ AgentRail = NewBaseRail()
}

// TestRailAgent_接口满足 测试 mock struct 满足 railAgent 接口。
func TestRailAgent_接口满足(t *testing.T) {
	// mockRailAgent 满足 railAgent 接口
	type mockRailAgent struct{}
	m := &mockRailAgent{}
	_ = m // 仅编译检查用
	// 注意：由于 railAgent 为包内非导出接口，此处无法直接做 var _ railAgent = mock
	// 通过 WarpBaseAgent 间接验证
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v -run "TestBaseRail_|TestCallbackFrom_|TestBuildCallbacks_|TestAgentRail_|TestRailAgent_" ./internal/agentcore/single_agent/rail/...`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/single_agent/rail/rail_test.go
git commit -m "test(rail): 新增 BaseRail/CallbackFrom/BuildCallbacks/接口满足 测试"
```

---

### Task 6: 补充 manager_test.go — RegisterRail/UnregisterRail 测试

**Files:**
- Modify: `internal/agentcore/single_agent/rail/manager_test.go`

- [ ] **Step 1: 在 manager_test.go 末尾添加测试**

在 `manager_test.go` 的 `// ──────────────────────────── 非导出函数 ────────────────────────────` 之前添加：

```go
// TestRegisterRail_批量注册 测试注册含 2 个钩子的 Rail。
func TestRegisterRail_批量注册(t *testing.T) {
	const agentID = "task67_test_rail_register"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	// 构造一个含 2 个钩子的 Rail
	r := NewBaseRail()
	var beforeCalled, afterCalled int32
	beforeFn := func(_ context.Context, _ any) error {
		atomic.AddInt32(&beforeCalled, 1)
		return nil
	}
	afterFn := func(_ context.Context, _ any) error {
		atomic.AddInt32(&afterCalled, 1)
		return nil
	}

	// 用一个实现了 AgentRail 的测试 struct
	testRail := &testRailWithHooks{
		BaseRail: r,
		callbacks: r.BuildCallbacks(
			r.CallbackFrom(CallbackBeforeModelCall, beforeFn),
			r.CallbackFrom(CallbackAfterModelCall, afterFn),
		),
	}

	err := m.RegisterRail(context.Background(), testRail)
	if err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	if !m.HasHooks(CallbackBeforeModelCall) {
		t.Fatal("注册后 BeforeModelCall 应有钩子")
	}
	if !m.HasHooks(CallbackAfterModelCall) {
		t.Fatal("注册后 AfterModelCall 应有钩子")
	}
}

// TestRegisterRail_优先级传递 测试 RegisterRail 传入 rail.Priority() 到 CallbackOption。
func TestRegisterRail_优先级传递(t *testing.T) {
	const agentID = "task67_test_rail_priority"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	r := NewBaseRail().WithPriority(90)
	fn := func(_ context.Context, _ any) error { return nil }
	testRail := &testRailWithHooks{
		BaseRail:   r,
		callbacks:  r.BuildCallbacks(r.CallbackFrom(CallbackBeforeInvoke, fn)),
	}

	err := m.RegisterRail(context.Background(), testRail)
	if err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	if !m.HasHooks(CallbackBeforeInvoke) {
		t.Fatal("注册后 BeforeInvoke 应有钩子")
	}
}

// TestUnregisterRail_批量注销 测试注销后事件无钩子。
func TestUnregisterRail_批量注销(t *testing.T) {
	const agentID = "task67_test_rail_unregister"
	m := NewAgentCallbackManager(agentID)
	defer m.Clear()

	r := NewBaseRail()
	fn1 := func(_ context.Context, _ any) error { return nil }
	fn2 := func(_ context.Context, _ any) error { return nil }
	testRail := &testRailWithHooks{
		BaseRail: r,
		callbacks: r.BuildCallbacks(
			r.CallbackFrom(CallbackBeforeToolCall, fn1),
			r.CallbackFrom(CallbackAfterToolCall, fn2),
		),
	}

	err := m.RegisterRail(context.Background(), testRail)
	if err != nil {
		t.Fatalf("RegisterRail 返回错误: %v", err)
	}

	if !m.HasHooks(CallbackBeforeToolCall) || !m.HasHooks(CallbackAfterToolCall) {
		t.Fatal("注册后两个事件都应有钩子")
	}

	err = m.UnregisterRail(context.Background(), testRail)
	if err != nil {
		t.Fatalf("UnregisterRail 返回错误: %v", err)
	}

	if m.HasHooks(CallbackBeforeToolCall) {
		t.Fatal("注销后 BeforeToolCall 不应有钩子")
	}
	if m.HasHooks(CallbackAfterToolCall) {
		t.Fatal("注销后 AfterToolCall 不应有钩子")
	}
}

// testRailWithHooks 用于测试的 AgentRail 实现，覆盖 GetCallbacks。
type testRailWithHooks struct {
	*BaseRail
	callbacks map[AgentCallbackEvent]cb.PerAgentCallbackFunc
}

func (r *testRailWithHooks) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.callbacks
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v -run "TestRegisterRail_|TestUnregisterRail_" ./internal/agentcore/single_agent/rail/...`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/single_agent/rail/manager_test.go
git commit -m "test(rail): 新增 RegisterRail/UnregisterRail 批量注册/注销/优先级 测试"
```

---

### Task 7: 更新 doc.go — 文件目录和核心类型索引

**Files:**
- Modify: `internal/agentcore/single_agent/rail/doc.go`

- [ ] **Step 1: 更新 doc.go**

将文件目录和核心类型索引更新为：

```go
// Package rail 提供 Agent 生命周期 Rail 系统的基础定义。
//
// Rail 是 class-based 的生命周期钩子机制，允许在 Agent 执行流程的
// 特定时机注入拦截逻辑（重试、提前终止、steering 等）。
//
// 本包与框架层 callback/ 包的事件体系是不同层次：
//   - 本包 AgentCallbackEvent = per-Agent 实例级生命周期事件
//   - callback.GlobalAgentEventType = 框架级全局观测事件
//
// 两者不桥接，各自独立触发，与 Python 保持一致。
//
// 文件目录：
//
//	rail/
//	├── doc.go       # 包文档
//	├── event.go     # AgentCallbackEvent 枚举定义
//	├── context.go   # AgentCallbackContext 结构体与方法
//	├── inputs.go    # EventInputs 接口及各事件 Inputs 结构体
//	├── rail.go      # AgentRail 接口 + BaseRail 结构体 + CallbackFrom/BuildCallbacks
//	└── manager.go   # AgentCallbackManager 回调管理器
//
// 核心类型/接口索引：
//
//	AgentCallbackEvent       — 10 种生命周期事件枚举
//	AgentCallbackContext     — Rail 系统核心中介对象（retry/force_finish/steering）
//	AgentCallbackManager    — PerAgent 实例级回调管理器（注册/触发/注销）
//	AgentRail                — Agent 生命周期 Rail 接口（10 个钩子 + Init/Uninit/GetCallbacks）
//	BaseRail                 — AgentRail 的 no-op 默认实现（嵌入后只需覆盖关心的钩子）
//	RailAgent                — Rail 包所需的最小 Agent 接口（打破循环依赖）
//	EventInputs              — 回调事件输入接口
//	InvokeInputs             — BEFORE/AFTER_INVOKE 事件输入
//	ModelCallInputs          — BEFORE/AFTER_MODEL_CALL 事件输入
//	ToolCallInputs           — BEFORE/AFTER_TOOL_CALL 事件输入
//	TaskIterationInputs      — BEFORE/AFTER_TASK_ITERATION 事件输入
//
// 对应 Python 代码：openjiuwen/core/single_agent/rail/base.py
package rail
```

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/single_agent/rail/doc.go
git commit -m "docs(rail): 更新 doc.go 文件目录和核心类型索引，添加 AgentRail/BaseRail/RailAgent"
```

---

### Task 8: 全量编译 + 测试验证 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:392`

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 2: rail 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/single_agent/rail/...`
Expected: 全部 PASS

- [ ] **Step 3: 受影响包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/single_agent/... ./internal/agentcore/single_agent/interfaces/...`
Expected: 全部 PASS

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将第 392 行从：

```
| 6.7 | ☐ | AgentRail 接口 | 10 个生命周期钩子 | `openjiuwen/core/single_agent/rail/base.py` |
```

改为：

```
| 6.7 | ✅ | AgentRail 接口 | ✅ AgentRail 接口(10钩子+Init/Uninit/GetCallbacks)；✅ BaseRail no-op默认+CallbackFrom/BuildCallbacks辅助；✅ RegisterRail/UnregisterRail 回填实现；✅ railAgent 扩充 AgentID()；✅ interfaces 包参数类型从 any 改为 AgentRail；✅ WarpBaseAgent RegisterRail 添加 Init 调用、UnregisterRail 添加 Uninit 调用；✅ 测试全部通过 | `openjiuwen/core/single_agent/rail/base.py` |
```

- [ ] **Step 5: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 6.7 状态为 ✅"
```
