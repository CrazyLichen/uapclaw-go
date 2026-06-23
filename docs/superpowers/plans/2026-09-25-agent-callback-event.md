# 6.4 AgentCallbackEvent 枚举实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Agent 生命周期回调事件枚举 `AgentCallbackEvent`（10 种事件），并将框架层 `AgentCallEventType` 重命名为 `AgentCallGlobalEventType` 以区分两层事件体系。

**Architecture:** 新增 `single_agent/rail/` 子包，定义 `AgentCallbackEvent`（type string + 显式字符串值，常量用 `Callback` 前缀）。同时机械重命名 `callback/` 包中的 `AgentCallEventType` → `AgentCallGlobalEventType`（常量名不变）。两层事件不桥接，与 Python 一致。

**Tech Stack:** Go 1.22+，标准库 `fmt`，项目内 `single_agent/rail/` 新包 + `runner/callback/` 已有包修改

**Spec:** `docs/superpowers/specs/2026-09-25-agent-callback-event-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| Create | `internal/agentcore/single_agent/rail/doc.go` | 包文档 |
| Create | `internal/agentcore/single_agent/rail/event.go` | AgentCallbackEvent 枚举定义 + String() + AllCallbackEvents() |
| Create | `internal/agentcore/single_agent/rail/event_test.go` | 枚举测试 |
| Modify | `internal/agentcore/runner/callback/events.go` | AgentCallEventType → AgentCallGlobalEventType |
| Modify | `internal/agentcore/runner/callback/framework.go` | 类型引用重命名 |
| Modify | `internal/agentcore/runner/callback/doc.go` | 文档更新 |
| Modify | `internal/agentcore/single_agent/base.go` | 注释更新（常量引用不变） |
| Modify | `internal/agentcore/single_agent/base_test.go` | 类型引用重命名 |
| Modify | `internal/agentcore/single_agent/doc.go` | 添加 rail/ 子包条目 |

---

### Task 1: 重命名 AgentCallEventType → AgentCallGlobalEventType（callback/events.go）

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go:212-215,219-227,332-334`
- Test: `internal/agentcore/runner/callback/events_test.go`

- [ ] **Step 1: 修改类型定义和常量声明**

在 `events.go` 中，将：
```go
// AgentCallEventType Agent 调用事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (AgentEvents)
type AgentCallEventType string
```
改为：
```go
// AgentCallGlobalEventType Agent 调用全局事件类型。
//
// 与 Rail 层 AgentCallbackEvent（per-Agent 实例级事件）区分：
//   - AgentCallGlobalEventType = 框架级全局观测（日志/监控/transform_io）
//   - AgentCallbackEvent = 实例级 Rail 拦截/控制（重试/提前终止/steering）
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (AgentEvents)
type AgentCallGlobalEventType string
```

将 5 个常量的类型从 `AgentCallEventType` 改为 `AgentCallGlobalEventType`：
```go
const (
	// AgentStarted Agent 执行启动
	AgentStarted AgentCallGlobalEventType = "_framework:agent_started"
	// AgentInvokeInput invoke 调用前触发
	AgentInvokeInput AgentCallGlobalEventType = "_framework:agent_invoke_input"
	// AgentInvokeOutput invoke 调用后触发
	AgentInvokeOutput AgentCallGlobalEventType = "_framework:agent_invoke_output"
	// AgentStreamInput stream 调用前触发
	AgentStreamInput AgentCallGlobalEventType = "_framework:agent_stream_input"
	// AgentStreamOutput stream 每项触发
	AgentStreamOutput AgentCallGlobalEventType = "_framework:agent_stream_output"
)
```

将 `AgentCallEventData` 中 `Event` 字段类型改为 `AgentCallGlobalEventType`：
```go
Event AgentCallGlobalEventType
```

将 `String()` 方法接收者改为 `AgentCallGlobalEventType`：
```go
func (t AgentCallGlobalEventType) String() string {
	return string(t)
}
```

将 `TransformAgentIOInputFunc` / `TransformAgentIOOutputFunc` 中的 `AgentCallEventType` 改为 `AgentCallGlobalEventType`：
```go
type TransformAgentIOInputFunc func(ctx context.Context, event AgentCallGlobalEventType, input any) any
type TransformAgentIOOutputFunc func(ctx context.Context, event AgentCallGlobalEventType, output any) any
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/callback/...`
Expected: 编译失败（framework.go 和其他文件还引用旧类型名），这是预期的，将在 Task 2 修复

- [ ] **Step 3: 提交（暂不提交，等 Task 2 一起编译通过后再提交）**

---

### Task 2: 重命名 AgentCallEventType → AgentCallGlobalEventType（callback/framework.go）

**Files:**
- Modify: `internal/agentcore/runner/callback/framework.go:46,50,125,127,430,442,535-536,553,566`

- [ ] **Step 1: 修改 framework.go 中所有 AgentCallEventType 引用**

将 `agentCallbacks` 和 `agentTransformIO` 字段类型改为 `AgentCallGlobalEventType`：
```go
agentCallbacks map[AgentCallGlobalEventType][]AgentCallbackFunc
agentTransformIO map[AgentCallGlobalEventType]*agentTransformIOEntry
```

将 `NewCallbackFramework` 中初始化改为 `AgentCallGlobalEventType`：
```go
agentCallbacks:    make(map[AgentCallGlobalEventType][]AgentCallbackFunc),
agentTransformIO:  make(map[AgentCallGlobalEventType]*agentTransformIOEntry),
```

将 `OnAgent` / `OffAgent` 函数签名改为 `AgentCallGlobalEventType`：
```go
func (fw *CallbackFramework) OnAgent(event AgentCallGlobalEventType, fn AgentCallbackFunc) {
func (fw *CallbackFramework) OffAgent(event AgentCallGlobalEventType, fn AgentCallbackFunc) {
```

将 `RegisterAgentTransformIO` 参数改为 `AgentCallGlobalEventType`：
```go
func (fw *CallbackFramework) RegisterAgentTransformIO(
	inputEvent AgentCallGlobalEventType,
	outputEvent AgentCallGlobalEventType,
	inputFn TransformAgentIOInputFunc,
	outputFn TransformAgentIOOutputFunc,
) {
```

将 `TransformAgentIOInput` / `TransformAgentIOOutput` 参数改为 `AgentCallGlobalEventType`：
```go
func (fw *CallbackFramework) TransformAgentIOInput(ctx context.Context, event AgentCallGlobalEventType, input any) any {
func (fw *CallbackFramework) TransformAgentIOOutput(ctx context.Context, event AgentCallGlobalEventType, output any) any {
```

- [ ] **Step 2: 修改 callback/doc.go**

将文档中 `AgentCallEventType` 改为 `AgentCallGlobalEventType`：
```
AgentCallGlobalEventType — Agent 调用生命周期事件（5 种），预定义枚举事件名
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/runner/callback/...`
Expected: PASS（callback 包内部已全部更新）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/callback/events.go internal/agentcore/runner/callback/framework.go internal/agentcore/runner/callback/doc.go
git commit -m "refactor(callback): 重命名 AgentCallEventType 为 AgentCallGlobalEventType，区分框架层与 Rail 层事件"
```

---

### Task 3: 更新外部引用（single_agent/base.go + base_test.go）

**Files:**
- Modify: `internal/agentcore/single_agent/base.go:110,116,139,143,164,170,196,201`
- Modify: `internal/agentcore/single_agent/base_test.go:422-440,143,151,159-162,286,294-295,429-433`

- [ ] **Step 1: 修改 base_test.go 中的类型引用**

测试中引用 `callback.AgentCallEventType` 的地方改为 `callback.AgentCallGlobalEventType`：

```go
// TestAgentCallGlobalEventType_事件名对齐Python 验证事件名与 Python AgentEvents 对齐
func TestAgentCallGlobalEventType_事件名对齐Python(t *testing.T) {
	// 对应 Python: openjiuwen/core/runner/callback/events.py AgentEvents
	tests := []struct {
		got  callback.AgentCallGlobalEventType
		want string
	}{
		{callback.AgentStarted, "_framework:agent_started"},
		{callback.AgentInvokeInput, "_framework:agent_invoke_input"},
		{callback.AgentInvokeOutput, "_framework:agent_invoke_output"},
		{callback.AgentStreamInput, "_framework:agent_stream_input"},
		{callback.AgentStreamOutput, "_framework:agent_stream_output"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("事件名 = %q, want %q", tt.got, tt.want)
		}
	}
}
```

注意：`base.go` 中的常量引用（`callback.AgentInvokeInput` 等）不需要改，因为常量名没变。只需确认编译通过。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: PASS

- [ ] **Step 3: 运行已有测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/runner/callback/... ./internal/agentcore/single_agent/... -v -count=1`
Expected: PASS（所有已有测试通过）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/base_test.go
git commit -m "refactor(single_agent): 适配 AgentCallGlobalEventType 重命名"
```

---

### Task 4: 创建 single_agent/rail/ 子包 + AgentCallbackEvent 枚举

**Files:**
- Create: `internal/agentcore/single_agent/rail/doc.go`
- Create: `internal/agentcore/single_agent/rail/event.go`

- [ ] **Step 1: 创建 rail/doc.go**

```go
// Package rail 提供 Agent 生命周期 Rail 系统的基础定义。
//
// Rail 是 class-based 的生命周期钩子机制，允许在 Agent 执行流程的
// 特定时机注入拦截逻辑（重试、提前终止、steering 等）。
//
// 本包与框架层 callback/ 包的事件体系是不同层次：
//   - 本包 AgentCallbackEvent = per-Agent 实例级生命周期事件
//   - callback.AgentCallGlobalEventType = 框架级全局观测事件
//
// 两者不桥接，各自独立触发，与 Python 保持一致。
//
// 文件目录：
//
//	rail/
//	├── doc.go       # 包文档
//	└── event.go     # AgentCallbackEvent 枚举定义
//
// 对应 Python 代码：openjiuwen/core/single_agent/rail/base.py
package rail
```

- [ ] **Step 2: 创建 rail/event.go**

```go
package rail

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// AgentCallbackEvent Agent 生命周期回调事件类型。
//
// 定义 per-Agent 实例级的 10 个生命周期事件，
// 供 AgentCallbackManager 注册回调和 AgentRail 钩子使用。
// 与框架层 AgentCallGlobalEventType（全局观测事件）是不同层次：
//   - AgentCallbackEvent = 实例级 Rail 拦截/控制（重试/提前终止/steering）
//   - AgentCallGlobalEventType = 框架级全局观测（日志/监控/transform_io）
//
// 事件值即 Python AgentRail 对应方法名，无需额外 EVENT_METHOD_MAP 映射。
// AgentCallbackManager 注册时通过 agentID 前缀构造唯一事件名
// （如 "{agentID}_before_invoke"），与框架层事件互不冲突。
//
// 对应 Python: openjiuwen/core/single_agent/rail/base.py (AgentCallbackEvent)
type AgentCallbackEvent string

const (
	// CallbackBeforeInvoke invoke 开始前
	CallbackBeforeInvoke AgentCallbackEvent = "before_invoke"
	// CallbackAfterInvoke invoke 完成后
	CallbackAfterInvoke AgentCallbackEvent = "after_invoke"
	// CallbackBeforeTaskIteration 外层任务循环迭代开始前
	CallbackBeforeTaskIteration AgentCallbackEvent = "before_task_iteration"
	// CallbackAfterTaskIteration 外层任务循环迭代完成后
	CallbackAfterTaskIteration AgentCallbackEvent = "after_task_iteration"
	// CallbackBeforeModelCall LLM 调用前
	CallbackBeforeModelCall AgentCallbackEvent = "before_model_call"
	// CallbackAfterModelCall LLM 响应后
	CallbackAfterModelCall AgentCallbackEvent = "after_model_call"
	// CallbackOnModelException LLM 调用异常
	CallbackOnModelException AgentCallbackEvent = "on_model_exception"
	// CallbackBeforeToolCall 工具执行前
	CallbackBeforeToolCall AgentCallbackEvent = "before_tool_call"
	// CallbackAfterToolCall 工具执行后
	CallbackAfterToolCall AgentCallbackEvent = "after_tool_call"
	// CallbackOnToolException 工具执行异常
	CallbackOnToolException AgentCallbackEvent = "on_tool_exception"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AllCallbackEvents 返回所有 AgentCallbackEvent 枚举值。
// 用于遍历清理等场景。
func AllCallbackEvents() []AgentCallbackEvent {
	return []AgentCallbackEvent{
		CallbackBeforeInvoke,
		CallbackAfterInvoke,
		CallbackBeforeTaskIteration,
		CallbackAfterTaskIteration,
		CallbackBeforeModelCall,
		CallbackAfterModelCall,
		CallbackOnModelException,
		CallbackBeforeToolCall,
		CallbackAfterToolCall,
		CallbackOnToolException,
	}
}

// String 实现 fmt.Stringer 接口。
func (e AgentCallbackEvent) String() string {
	return string(e)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (e AgentCallbackEvent) GoString() string {
	return fmt.Sprintf("rail.AgentCallbackEvent(%q)", string(e))
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/rail/...`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/rail/doc.go internal/agentcore/single_agent/rail/event.go
git commit -m "feat(single_agent/rail): 新增 AgentCallbackEvent 枚举，10 种 Agent 生命周期回调事件"
```

---

### Task 5: 编写 AgentCallbackEvent 枚举测试

**Files:**
- Create: `internal/agentcore/single_agent/rail/event_test.go`

- [ ] **Step 1: 创建 event_test.go**

```go
package rail

import (
	"testing"
)

// TestAgentCallbackEvent_事件值对齐Python 验证事件值与 Python AgentCallbackEvent 完全对齐
func TestAgentCallbackEvent_事件值对齐Python(t *testing.T) {
	// 对应 Python: openjiuwen/core/single_agent/rail/base.py AgentCallbackEvent
	tests := []struct {
		got  AgentCallbackEvent
		want string
	}{
		{CallbackBeforeInvoke, "before_invoke"},
		{CallbackAfterInvoke, "after_invoke"},
		{CallbackBeforeTaskIteration, "before_task_iteration"},
		{CallbackAfterTaskIteration, "after_task_iteration"},
		{CallbackBeforeModelCall, "before_model_call"},
		{CallbackAfterModelCall, "after_model_call"},
		{CallbackOnModelException, "on_model_exception"},
		{CallbackBeforeToolCall, "before_tool_call"},
		{CallbackAfterToolCall, "after_tool_call"},
		{CallbackOnToolException, "on_tool_exception"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("事件值 = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestAgentCallbackEvent_String 验证 String() 方法返回事件值
func TestAgentCallbackEvent_String(t *testing.T) {
	if got := CallbackBeforeModelCall.String(); got != "before_model_call" {
		t.Errorf("CallbackBeforeModelCall.String() = %q, want %q", got, "before_model_call")
	}
}

// TestAgentCallbackEvent_GoString 验证 GoString() 方法返回带类型名前缀
func TestAgentCallbackEvent_GoString(t *testing.T) {
	if got := CallbackBeforeModelCall.GoString(); got != `rail.AgentCallbackEvent("before_model_call")` {
		t.Errorf("CallbackBeforeModelCall.GoString() = %q, want %q", got, `rail.AgentCallbackEvent("before_model_call")`)
	}
}

// TestAllCallbackEvents 验证 AllCallbackEvents 返回全部 10 个事件
func TestAllCallbackEvents(t *testing.T) {
	events := AllCallbackEvents()
	if len(events) != 10 {
		t.Fatalf("AllCallbackEvents() 返回 %d 个事件，want 10", len(events))
	}

	// 验证无重复
	seen := make(map[AgentCallbackEvent]bool)
	for _, e := range events {
		if seen[e] {
			t.Errorf("重复事件: %q", e)
		}
		seen[e] = true
	}

	// 验证包含关键事件
	if !seen[CallbackBeforeInvoke] {
		t.Error("缺少 CallbackBeforeInvoke")
	}
	if !seen[CallbackOnToolException] {
		t.Error("缺少 CallbackOnToolException")
	}
}

// TestAgentCallbackEvent_事件值即方法名 验证事件值就是 Python AgentRail 对应方法名
func TestAgentCallbackEvent_事件值即方法名(t *testing.T) {
	// 事件值直接对应 Python AgentRail 的方法名，无需 EVENT_METHOD_MAP
	methodNames := map[AgentCallbackEvent]string{
		CallbackBeforeInvoke:       "before_invoke",
		CallbackAfterInvoke:        "after_invoke",
		CallbackBeforeTaskIteration: "before_task_iteration",
		CallbackAfterTaskIteration:  "after_task_iteration",
		CallbackBeforeModelCall:    "before_model_call",
		CallbackAfterModelCall:     "after_model_call",
		CallbackOnModelException:   "on_model_exception",
		CallbackBeforeToolCall:     "before_tool_call",
		CallbackAfterToolCall:      "after_tool_call",
		CallbackOnToolException:    "on_tool_exception",
	}
	for event, methodName := range methodNames {
		if string(event) != methodName {
			t.Errorf("事件 %q 的值不等于方法名 %q", event, methodName)
		}
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/rail/... -v -count=1`
Expected: PASS（5 个测试全部通过）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/rail/event_test.go
git commit -m "test(single_agent/rail): AgentCallbackEvent 枚举测试，验证事件值对齐 Python"
```

---

### Task 6: 更新 single_agent/doc.go 添加 rail/ 子包条目

**Files:**
- Modify: `internal/agentcore/single_agent/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录树**

在文件目录的 `resource/` 条目后添加 `rail/` 条目：

```go
// Package single_agent 提供 Agent 核心能力管理，包括 AbilityManager 注册与调度。
//
// AbilityManager 是 Agent 的能力注册与调度中心，管理四类 Ability
// （Tool / Workflow / Agent / McpServer）的完整生命周期：
// 注册管理、LLM 工具描述生成、并行执行、JSON 参数修复、路由分发。
//
// Workflow/Agent 接口定义从本包抽出至 interfaces 子包，
// 供 tracer 等外部包引用，避免 tracer → single_agent → context_engine 循环依赖。
//
// 文件目录：
//
//	single_agent/
//	├── doc.go                 # 包文档
//	├── base.go                # WarpBaseAgent — BaseAgent 默认实现，Invoke/Stream 回调包装骨架
//	├── ability/
//	│   ├── doc.go             # 子包文档
//	│   ├── ability_manager.go # AbilityManager 核心结构 + 注册/查询/执行
//	│   ├── ability_types.go   # Ability 联合类型 + AddAbilityResult + AbilityExecutionError + ToolRail 预留
//	│   └── json_repair.go     # RepairToolArgumentsJSON + ParseToolArguments
//	├── config/
//	│   ├── doc.go             # 子包文档
//	│   └── agent_config.go    # ReActAgentConfig 结构体 + Option + AgentConfig 接口实现 + Validate
//	├── interfaces/
//	│   ├── doc.go             # 子包文档
//	│   └── interface.go       # Workflow/Agent 接口 + WorkflowOption/AgentOption 类型
//	├── rail/
//	│   ├── doc.go             # 子包文档
//	│   └── event.go           # AgentCallbackEvent 枚举 — Agent 生命周期回调事件类型（10 种）
//	├── resource/
//	│   ├── doc.go             # 子包文档
//	│   └── resource_manager.go # ResourceManager 接口 + NoopResourceManager + ResourceOptions
//	└── schema/
//	    ├── doc.go             # 子包文档
//	    ├── agent_card.go      # AgentCard 结构体 + 构造函数 + Ability 接口实现
//	    └── agent_result.go    # Part/Artifact/AgentResult 结果模型
//
// 对应 Python 代码：openjiuwen/core/single_agent/ability_manager.py
package single_agent
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/doc.go
git commit -m "docs(single_agent): doc.go 添加 rail/ 子包条目"
```

---

### Task 7: 全量编译 + 测试验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: PASS

- [ ] **Step 2: 运行受影响包的测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/runner/callback/... ./internal/agentcore/single_agent/... -v -count=1`
Expected: PASS（所有测试通过）

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/single_agent/rail/...`
Expected: 覆盖率 ≥ 85%

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md 状态

- [ ] **Step 1: 将 6.4 状态从 ☐ 改为 ✅**

在 `IMPLEMENTATION_PLAN.md` 第 388 行，将：
```
| 6.4 | ☐ | AgentCallbackEvent 枚举 | 10 种事件类型 |
```
改为：
```
| 6.4 | ✅ | AgentCallbackEvent 枚举 | ✅ AgentCallbackEvent 枚举（type string，10 种事件，常量 Callback 前缀）；✅ AllCallbackEvents/String/GoString；✅ AgentCallEventType 重命名为 AgentCallGlobalEventType（常量名不变）；✅ 测试全部通过 |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 6.4 状态为 ✅"
```
