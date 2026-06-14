# AgentSession 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 5.3 AgentSession，包含公开层 Session + 内部层 AgentSession，回填 AgentStateCollection，扩展 CallbackFramework Session 维度。

**Architecture:** 严格对齐 Python 两层设计——内部层 AgentSession 实现 BaseSession 接口，持有基础设施组件；公开层 Session 组合内部层，实现 PreRun→Invoke/Stream→PostRun 生命周期。未实现依赖用 any + nil 占位。CallbackFramework 加 Session 维度（OnSession/OffSession/TriggerSession）。

**Tech Stack:** Go 1.x，依赖已有 state 包接口、callback 包框架

**Spec:** `docs/superpowers/specs/2025-08-06-agent-session-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新增 | `internal/agentcore/session/state/agent_state_collection.go` | Agent 状态集合（回填 5.1） |
| 新增 | `internal/agentcore/session/state/agent_state_collection_test.go` | AgentStateCollection 测试 |
| 修改 | `internal/agentcore/session/state/doc.go` | 更新文件目录 |
| 新增 | `internal/agentcore/session/internal/doc.go` | internal 子包文档 |
| 新增 | `internal/agentcore/session/internal/agent_session.go` | 内部层 AgentSession |
| 新增 | `internal/agentcore/session/internal/agent_session_test.go` | AgentSession 测试 |
| 修改 | `internal/agentcore/runner/callback/events.go` | 新增 SessionCallEventType + SessionCallEventData |
| 修改 | `internal/agentcore/runner/callback/framework.go` | 新增 sessionCallbacks + OnSession/OffSession/TriggerSession |
| 修改 | `internal/agentcore/runner/callback/events_test.go` | Session 事件测试 |
| 修改 | `internal/agentcore/runner/callback/framework_test.go` | Session 维度测试 |
| 修改 | `internal/agentcore/runner/callback/doc.go` | 更新事件体系说明 |
| 新增 | `internal/agentcore/session/agent.go` | 公开层 Session |
| 新增 | `internal/agentcore/session/agent_test.go` | Session 测试 |
| 修改 | `internal/agentcore/session/doc.go` | 更新文件目录和包文档 |

---

### Task 1: AgentStateCollection 实现

**Files:**
- Create: `internal/agentcore/session/state/agent_state_collection.go`
- Test: `internal/agentcore/session/state/agent_state_collection_test.go`

对应 Python: `openjiuwen/core/session/state/agent_state.py`

- [ ] **Step 1: 编写 AgentStateCollection 测试**

```go
// internal/agentcore/session/state/agent_state_collection_test.go
package state

import (
	"testing"
)

// TestNewAgentStateCollection 测试构造函数
func TestNewAgentStateCollection(t *testing.T) {
	coll := NewAgentStateCollection()
	if coll == nil {
		t.Fatal("NewAgentStateCollection 返回 nil")
	}
}

// TestAgentStateCollection_GetGlobal_空Key返回完整全局状态 测试空 key 返回完整全局状态
func TestAgentStateCollection_GetGlobal_空Key返回完整全局状态(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"foo": "bar", "baz": 123})

	result := coll.GetGlobal("")
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["foo"] != "bar" {
		t.Errorf("期望 foo=bar，实际 %v", m["foo"])
	}
	if m["baz"] != 123 {
		t.Errorf("期望 baz=123，实际 %v", m["baz"])
	}
}

// TestAgentStateCollection_GetGlobal_有Key返回对应值 测试有 key 返回对应值
func TestAgentStateCollection_GetGlobal_有Key返回对应值(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"foo": "bar"})

	result := coll.GetGlobal("foo")
	if result != "bar" {
		t.Errorf("期望 bar，实际 %v", result)
	}
}

// TestAgentStateCollection_GetGlobal_不存在的Key返回Nil 测试不存在的 key 返回 nil
func TestAgentStateCollection_GetGlobal_不存在的Key返回Nil(t *testing.T) {
	coll := NewAgentStateCollection()
	result := coll.GetGlobal("nonexistent")
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestAgentStateCollection_UpdateGlobal 测试更新全局状态
func TestAgentStateCollection_UpdateGlobal(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"a": 1})
	coll.UpdateGlobal(map[string]any{"b": 2})

	// a 仍存在，b 新增
	if coll.GetGlobal("a") != 1 {
		t.Errorf("期望 a=1，实际 %v", coll.GetGlobal("a"))
	}
	if coll.GetGlobal("b") != 2 {
		t.Errorf("期望 b=2，实际 %v", coll.GetGlobal("b"))
	}
}

// TestAgentStateCollection_Get_空Key返回完整Agent状态 测试空 key 返回完整 Agent 状态
func TestAgentStateCollection_Get_空Key返回完整Agent状态(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.Update(map[string]any{"x": "y"})

	result := coll.Get("")
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["x"] != "y" {
		t.Errorf("期望 x=y，实际 %v", m["x"])
	}
}

// TestAgentStateCollection_Get_有Key返回对应值 测试有 key 返回对应值
func TestAgentStateCollection_Get_有Key返回对应值(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.Update(map[string]any{"x": "y"})

	result := coll.Get("x")
	if result != "y" {
		t.Errorf("期望 y，实际 %v", result)
	}
}

// TestAgentStateCollection_Update 测试更新 Agent 状态
func TestAgentStateCollection_Update(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.Update(map[string]any{"a": 1})
	coll.Update(map[string]any{"b": 2})

	if coll.Get("a") != 1 {
		t.Errorf("期望 a=1，实际 %v", coll.Get("a"))
	}
	if coll.Get("b") != 2 {
		t.Errorf("期望 b=2，实际 %v", coll.Get("b"))
	}
}

// TestAgentStateCollection_GetState 测试导出快照
func TestAgentStateCollection_GetState(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"g": 1})
	coll.Update(map[string]any{"a": 2})

	state := coll.GetState()
	gs, ok := state[GlobalStateKey]
	if !ok {
		t.Fatal("快照中缺少 global_state")
	}
	gm, _ := gs.(map[string]any)
	if gm["g"] != 1 {
		t.Errorf("期望 global_state.g=1，实际 %v", gm["g"])
	}

	as, ok := state[AgentStateKey]
	if !ok {
		t.Fatal("快照中缺少 agent_state")
	}
	am, _ := as.(map[string]any)
	if am["a"] != 2 {
		t.Errorf("期望 agent_state.a=2，实际 %v", am["a"])
	}
}

// TestAgentStateCollection_SetState 测试从快照恢复
func TestAgentStateCollection_SetState(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"g": 1})
	coll.Update(map[string]any{"a": 2})

	snapshot := coll.GetState()

	// 新实例从快照恢复
	coll2 := NewAgentStateCollection()
	coll2.SetState(snapshot)

	if coll2.GetGlobal("g") != 1 {
		t.Errorf("恢复后期望 g=1，实际 %v", coll2.GetGlobal("g"))
	}
	if coll2.Get("a") != 2 {
		t.Errorf("恢复后期望 a=2，实际 %v", coll2.Get("a"))
	}
}

// TestAgentStateCollection_Dump 测试完整导出
func TestAgentStateCollection_Dump(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"g": 1})
	coll.Update(map[string]any{"a": 2})

	dump := coll.Dump()
	if _, ok := dump[GlobalStateKey]; !ok {
		t.Fatal("dump 中缺少 global_state")
	}
	if _, ok := dump[AgentStateKey]; !ok {
		t.Fatal("dump 中缺少 agent_state")
	}
}

// TestAgentStateCollection_状态隔离 测试 globalState 和 agentState 互不干扰
func TestAgentStateCollection_状态隔离(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.UpdateGlobal(map[string]any{"key": "global_val"})
	coll.Update(map[string]any{"key": "agent_val"})

	if coll.GetGlobal("key") != "global_val" {
		t.Errorf("全局状态期望 global_val，实际 %v", coll.GetGlobal("key"))
	}
	if coll.Get("key") != "agent_val" {
		t.Errorf("Agent 状态期望 agent_val，实际 %v", coll.Get("key"))
	}
}

// TestAgentStateCollection_GetByPrefix 测试前缀查询
func TestAgentStateCollection_GetByPrefix(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.Update(map[string]any{"nested": map[string]any{"child": "value"}})

	result := coll.GetByPrefix("nested", "child")
	if result != "value" {
		t.Errorf("期望 value，实际 %v", result)
	}
}

// TestAgentStateCollection_GetByTransformer 测试转换函数
func TestAgentStateCollection_GetByTransformer(t *testing.T) {
	coll := NewAgentStateCollection()
	coll.Update(map[string]any{"x": 42})

	result := coll.GetByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("x"))
	})
	if result != 42 {
		t.Errorf("期望 42，实际 %v", result)
	}
}

// TestAgentStateCollection_实现State接口 测试 AgentStateCollection 满足 State 接口
func TestAgentStateCollection_实现State接口(t *testing.T) {
	var _ State = NewAgentStateCollection()
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/state/ -run "TestNewAgentStateCollection|TestAgentStateCollection" -v -count=1 2>&1 | head -30`

Expected: 编译失败，`NewAgentStateCollection` 未定义

- [ ] **Step 3: 实现 AgentStateCollection**

```go
// internal/agentcore/session/state/agent_state_collection.go
package state

import "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 结构体 ────────────────────────────

// AgentStateCollection Agent 会话状态集合。
//
// 组合 globalState（跨 Agent 共享的全局状态）和 agentState（当前 Agent 专属状态），
// 提供统一的状态读写接口。实现 State 接口。
//
// 对应 Python: openjiuwen/core/session/state/agent_state.py (StateCollection)
type AgentStateCollection struct {
	// globalState 全局状态（跨 Agent 共享）
	globalState *InMemoryState
	// agentState Agent 专属状态
	agentState *InMemoryState
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentStateCollection 创建 Agent 状态集合实例。
func NewAgentStateCollection() *AgentStateCollection {
	logger.Info(logger.ComponentAgentCore).Str("action", "new_agent_state_collection").Msg("创建 Agent 状态集合")
	return &AgentStateCollection{
		globalState: NewInMemoryState(),
		agentState:  NewInMemoryState(),
	}
}

// ──────────────────────────── AgentStateCollection 方法 ────────────────────────────

// GetGlobal 从全局状态获取值。key 为空时返回完整全局状态。
func (s *AgentStateCollection) GetGlobal(key string) any {
	if key == "" {
		return s.globalState.GetState()
	}
	return s.globalState.Get(StringKey(key))
}

// UpdateGlobal 更新全局状态。
func (s *AgentStateCollection) UpdateGlobal(data map[string]any) {
	_ = s.globalState.Update(data)
}

// Get 从 Agent 状态获取值。key 为空时返回完整 Agent 状态。
func (s *AgentStateCollection) Get(key string) any {
	if key == "" {
		return s.agentState.GetState()
	}
	return s.agentState.Get(StringKey(key))
}

// Update 更新 Agent 状态。
func (s *AgentStateCollection) Update(data map[string]any) error {
	return s.agentState.Update(data)
}

// GetState 导出状态快照（用于检查点恢复）。
// 返回 {global_state: {...}, agent_state: {...}}。
func (s *AgentStateCollection) GetState() map[string]any {
	return map[string]any{
		GlobalStateKey: s.globalState.GetState(),
		AgentStateKey:  s.agentState.GetState(),
	}
}

// SetState 从快照恢复状态。
func (s *AgentStateCollection) SetState(state map[string]any) {
	if state == nil {
		return
	}
	if gs, ok := state[GlobalStateKey]; ok {
		if gm, ok := gs.(map[string]any); ok {
			s.globalState.SetState(gm)
		}
	}
	if as, ok := state[AgentStateKey]; ok {
		if am, ok := as.(map[string]any); ok {
			s.agentState.SetState(am)
		}
	}
}

// Dump 导出完整状态（含 trace_state）。
func (s *AgentStateCollection) Dump() map[string]any {
	return map[string]any{
		GlobalStateKey: s.globalState.GetState(),
		AgentStateKey:  s.agentState.GetState(),
	}
}

// GlobalState 返回底层全局状态的 InMemoryState 引用。
// 用于 AgentSession 创建 WorkflowSession 时传递全局状态。
func (s *AgentStateCollection) GlobalState() *InMemoryState {
	return s.globalState
}

// ──────────────────────────── State 接口实现 ────────────────────────────

// Get 根据 StateKey 获取状态值。委托到 agentState。
func (s *AgentStateCollection) Get(key StateKey) any {
	return s.agentState.Get(key)
}

// GetByPrefix 根据 key 和嵌套前缀获取状态值。委托到 agentState。
func (s *AgentStateCollection) GetByPrefix(key StateKey, nestedPrefix string) any {
	return s.agentState.GetByPrefix(key, nestedPrefix)
}

// GetByTransformer 通过转换函数获取状态值。委托到 agentState。
func (s *AgentStateCollection) GetByTransformer(transformer Transformer) any {
	return s.agentState.GetByTransformer(transformer)
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/state/ -run "TestNewAgentStateCollection|TestAgentStateCollection" -v -count=1`

Expected: 所有测试 PASS

- [ ] **Step 5: 更新 state/doc.go 文件目录**

在 `state/doc.go` 的文件目录树中添加 `agent_state_collection.go` 条目：

```
//	state/
//	├── doc.go                        # 包文档
//	├── key.go                        # StateKey 类型 + StateKeyType 枚举 + 构造函数
//	├── state.go                      # 4 层接口 + Transformer 类型 + 常量
//	├── agent_state_collection.go     # Agent 状态集合（组合 global + agent state）
//	├── inmemory_state.go             # InMemoryState 实现 State 接口
//	├── inmemory_commit_state.go      # InMemoryCommitState 实现 CommitState 接口
//	└── utils.go                      # 深拷贝 / 嵌套路径解析 / 状态读写工具函数
```

同时更新核心类型索引，添加 `AgentStateCollection`。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/state/agent_state_collection.go internal/agentcore/session/state/agent_state_collection_test.go internal/agentcore/session/state/doc.go
git commit -m "feat(state): add AgentStateCollection for agent session state management"
```

---

### Task 2: CallbackFramework Session 维度扩展

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go`
- Modify: `internal/agentcore/runner/callback/framework.go`
- Modify: `internal/agentcore/runner/callback/events_test.go`
- Modify: `internal/agentcore/runner/callback/framework_test.go`
- Modify: `internal/agentcore/runner/callback/doc.go`

- [ ] **Step 1: 编写 Session 事件测试**

在 `events_test.go` 末尾添加：

```go
// TestSessionCallEventType_字符串值 测试 Session 事件类型字符串值
func TestSessionCallEventType_字符串值(t *testing.T) {
	if SessionCreated != "_framework:session_created" {
		t.Errorf("SessionCreated 期望 _framework:session_created，实际 %s", SessionCreated)
	}
	if AgentSessionCreated != "_framework:agent_session_created" {
		t.Errorf("AgentSessionCreated 期望 _framework:agent_session_created，实际 %s", AgentSessionCreated)
	}
}

// TestSessionCallEventType_String 测试 String 方法
func TestSessionCallEventType_String(t *testing.T) {
	if SessionCreated.String() != "_framework:session_created" {
		t.Errorf("String() 期望 _framework:session_created，实际 %s", SessionCreated.String())
	}
}

// TestSessionCallEventData_String 测试 String 方法
func TestSessionCallEventData_String(t *testing.T) {
	data := &SessionCallEventData{
		Event:     AgentSessionCreated,
		SessionID: "test-123",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}
```

在 `framework_test.go` 末尾添加：

```go
// TestCallbackFramework_OnSession和TriggerSession 测试注册+触发 Session 回调
func TestCallbackFramework_OnSession和TriggerSession(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	var receivedData *SessionCallEventData

	fn := func(ctx context.Context, data *SessionCallEventData) any {
		called = true
		receivedData = data
		return "result"
	}

	fw.OnSession(AgentSessionCreated, fn)
	results := fw.TriggerSession(context.Background(), &SessionCallEventData{
		Event:     AgentSessionCreated,
		SessionID: "test-session",
	})

	if !called {
		t.Error("回调未被调用")
	}
	if receivedData.SessionID != "test-session" {
		t.Errorf("SessionID 期望 test-session，实际 %s", receivedData.SessionID)
	}
	if len(results) != 1 || results[0] != "result" {
		t.Errorf("结果期望 [result]，实际 %v", results)
	}
}

// TestCallbackFramework_OffSession 测试注销 Session 回调
func TestCallbackFramework_OffSession(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool

	fn := func(ctx context.Context, data *SessionCallEventData) any {
		called = true
		return nil
	}

	fw.OnSession(AgentSessionCreated, fn)
	fw.OffSession(AgentSessionCreated, fn)
	fw.TriggerSession(context.Background(), &SessionCallEventData{
		Event: AgentSessionCreated,
	})

	if called {
		t.Error("注销后回调不应被调用")
	}
}

// TestCallbackFramework_TriggerSession_无回调时返回空 测试无回调时返回空切片
func TestCallbackFramework_TriggerSession_无回调时返回空(t *testing.T) {
	fw := NewCallbackFramework()
	results := fw.TriggerSession(context.Background(), &SessionCallEventData{
		Event: AgentSessionCreated,
	})
	if len(results) != 0 {
		t.Errorf("无回调时期望空切片，实际 %v", results)
	}
}

// TestCallbackFramework_TriggerSession_Nil上下文 测试 nil context 或 nil data
func TestCallbackFramework_TriggerSession_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	var called bool
	fw.OnSession(AgentSessionCreated, func(ctx context.Context, data *SessionCallEventData) any {
		called = true
		return nil
	})

	// nil context
	results := fw.TriggerSession(nil, &SessionCallEventData{Event: AgentSessionCreated})
	if results != nil {
		t.Errorf("nil context 期望 nil，实际 %v", results)
	}

	// nil data
	results = fw.TriggerSession(context.Background(), nil)
	if results != nil {
		t.Errorf("nil data 期望 nil，实际 %v", results)
	}

	if called {
		t.Error("nil 参数时回调不应被调用")
	}
}

// TestCallbackFramework_Session事件与LLMTool隔离 测试 Session 回调不影响 LLM/Tool
func TestCallbackFramework_Session事件与LLMTool隔离(t *testing.T) {
	fw := NewCallbackFramework()
	var llmCalled, toolCalled bool

	fw.OnSession(AgentSessionCreated, func(ctx context.Context, data *SessionCallEventData) any {
		return nil
	})
	fw.OnLLM(LLMCallStarted, func(ctx context.Context, data *LLMCallEventData) any {
		llmCalled = true
		return nil
	})
	fw.OnTool(ToolCallStarted, func(ctx context.Context, data *ToolCallEventData) any {
		toolCalled = true
		return nil
	})

	// 触发 Session 事件，不应触发 LLM/Tool 回调
	fw.TriggerSession(context.Background(), &SessionCallEventData{Event: AgentSessionCreated})
	if llmCalled {
		t.Error("Session 事件不应触发 LLM 回调")
	}
	if toolCalled {
		t.Error("Session 事件不应触发 Tool 回调")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/runner/callback/ -run "TestSessionCallEventType|TestCallbackFramework_OnSession|TestCallbackFramework_OffSession|TestCallbackFramework_TriggerSession|TestCallbackFramework_Session事件" -v -count=1 2>&1 | head -30`

Expected: 编译失败，`SessionCallEventType`、`AgentSessionCreated` 等未定义

- [ ] **Step 3: 在 events.go 中新增 Session 事件类型和数据结构**

在 `events.go` 的 `ToolCallEventType` 枚举之后、导出函数区块之前，添加：

```go
// SessionCallEventType Session 调用事件类型。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (SessionEvents)
type SessionCallEventType string

const (
	// SessionCreated 会话创建事件
	SessionCreated SessionCallEventType = "_framework:session_created"
	// AgentSessionCreated Agent 会话创建事件
	AgentSessionCreated SessionCallEventType = "_framework:agent_session_created"
)

// SessionCallEventData Session 调用事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/session/agent.py 中 trigger(SessionEvents.AGENT_SESSION_CREATED, ...) 的 kwargs
type SessionCallEventData struct {
	// Event 事件类型
	Event SessionCallEventType
	// SessionID 会话标识
	SessionID string
	// Card Agent 身份元数据
	Card any
	// Session 会话实例
	Session any
	// Extra 额外数据
	Extra map[string]any
}
```

在 `events.go` 的导出函数区块末尾添加：

```go
// String 实现 fmt.Stringer 接口。
func (t SessionCallEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *SessionCallEventData) String() string {
	return fmt.Sprintf("SessionCallEventData{Event:%s, SessionID:%s}", d.Event, d.SessionID)
}
```

- [ ] **Step 4: 在 framework.go 中新增 Session 维度**

在 `framework.go` 的结构体区块，`CallbackFramework` 结构体中添加 `sessionCallbacks` 字段：

```go
type CallbackFramework struct {
	mu              sync.RWMutex
	llmCallbacks    map[LLMCallEventType][]LLMCallbackFunc
	toolCallbacks   map[ToolCallEventType][]ToolCallbackFunc
	sessionCallbacks map[SessionCallEventType][]SessionCallbackFunc
}
```

在 `LLMCallbackFunc` 和 `ToolCallbackFunc` 旁边添加：

```go
// SessionCallbackFunc Session 回调函数类型。
type SessionCallbackFunc func(ctx context.Context, data *SessionCallEventData) any
```

在 `NewCallbackFramework` 函数中初始化 `sessionCallbacks`：

```go
func NewCallbackFramework() *CallbackFramework {
	fw := &CallbackFramework{
		llmCallbacks:     make(map[LLMCallEventType][]LLMCallbackFunc),
		toolCallbacks:    make(map[ToolCallEventType][]ToolCallbackFunc),
		sessionCallbacks: make(map[SessionCallEventType][]SessionCallbackFunc),
	}
	// ... 原有 LLM 日志回调注册 ...
	return fw
}
```

在 `TriggerTool` 方法之后、`GetCallbacksForTest` 方法之前，添加三个方法：

```go
// OnSession 注册 Session 事件回调函数。
func (fw *CallbackFramework) OnSession(event SessionCallEventType, fn SessionCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.sessionCallbacks[event] = append(fw.sessionCallbacks[event], fn)
}

// OffSession 注销 Session 事件回调函数。
func (fw *CallbackFramework) OffSession(event SessionCallEventType, fn SessionCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.sessionCallbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.sessionCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerSession 触发 Session 事件，按注册顺序调用所有回调，返回所有回调结果。
func (fw *CallbackFramework) TriggerSession(ctx context.Context, data *SessionCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.sessionCallbacks[data.Event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
}
```

- [ ] **Step 5: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/runner/callback/ -v -count=1`

Expected: 所有测试 PASS（包括原有的 LLM/Tool 测试和新增的 Session 测试）

- [ ] **Step 6: 更新 callback/doc.go**

更新事件体系说明和文件目录：

```
// 事件体系：
//
//	LLMCallEventType    — LLM 调用生命周期事件（9 种）
//	ToolCallEventType   — Tool 调用生命周期事件（11 种）
//	SessionCallEventType — Session 生命周期事件（2 种）
```

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/runner/callback/events.go internal/agentcore/runner/callback/framework.go internal/agentcore/runner/callback/events_test.go internal/agentcore/runner/callback/framework_test.go internal/agentcore/runner/callback/doc.go
git commit -m "feat(callback): add Session event dimension (OnSession/OffSession/TriggerSession)"
```

---

### Task 3: 内部层 AgentSession 实现

**Files:**
- Create: `internal/agentcore/session/internal/doc.go`
- Create: `internal/agentcore/session/internal/agent_session.go`
- Create: `internal/agentcore/session/internal/agent_session_test.go`

- [ ] **Step 1: 创建 internal 子包目录和 doc.go**

```bash
mkdir -p internal/agentcore/session/internal
```

```go
// internal/agentcore/session/internal/doc.go
// Package internal 提供会话的内部实现，不对外暴露。
//
// 本包包含 BaseSession 接口的具体实现（AgentSession 等），
// 由公开层 Session 组合使用，不应被外部包直接引用。
//
// 文件目录：
//
//	internal/
//	├── doc.go              # 包文档
//	└── agent_session.go    # AgentSession — BaseSession 的 Agent 会话实现
//
// 对应 Python 代码：openjiuwen/core/session/internal/agent.py
package internal
```

- [ ] **Step 2: 编写 AgentSession 测试**

```go
// internal/agentcore/session/internal/agent_session_test.go
package internal

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// TestNewAgentSession 测试构造函数
func TestNewAgentSession(t *testing.T) {
	s := NewAgentSession("test-id")
	if s == nil {
		t.Fatal("NewAgentSession 返回 nil")
	}
	if s.SessionID() != "test-id" {
		t.Errorf("SessionID 期望 test-id，实际 %s", s.SessionID())
	}
}

// TestAgentSession_接口实现 测试满足 BaseSession 接口
func TestAgentSession_接口实现(t *testing.T) {
	var _ session.BaseSession = NewAgentSession("test")
}

// TestAgentSession_默认字段为Nil 测试未传选项时字段返回 nil
func TestAgentSession_默认字段为Nil(t *testing.T) {
	s := NewAgentSession("test-id")
	if s.Config() != nil {
		t.Error("默认 Config 应为 nil")
	}
	if s.Tracer() != nil {
		t.Error("默认 Tracer 应为 nil")
	}
	if s.StreamWriterManager() != nil {
		t.Error("默认 StreamWriterManager 应为 nil")
	}
	if s.Checkpointer() != nil {
		t.Error("默认 Checkpointer 应为 nil")
	}
	if s.ActorManager() != nil {
		t.Error("默认 ActorManager 应为 nil")
	}
}

// TestAgentSession_State不为Nil 测试默认创建 AgentStateCollection
func TestAgentSession_State不为Nil(t *testing.T) {
	s := NewAgentSession("test-id")
	if s.State() == nil {
		t.Error("State 不应为 nil")
	}
	// 验证 State 是 AgentStateCollection
	coll, ok := s.State().(*state.AgentStateCollection)
	if !ok {
		t.Errorf("State 期望 *AgentStateCollection，实际 %T", s.State())
	}
	if coll == nil {
		t.Error("AgentStateCollection 不应为 nil")
	}
}

// TestAgentSession_选项注入 测试通过选项注入组件
func TestAgentSession_选项注入(t *testing.T) {
	config := map[string]any{"key": "value"}
	s := NewAgentSession("test-id",
		WithConfig(config),
		WithCard("test-card"),
	)

	if s.Config() == nil {
		t.Error("Config 不应为 nil")
	}
	if s.Card() == nil {
		t.Error("Card 不应为 nil")
	}
}

// TestAgentSession_ActorManager返回Nil 测试 ActorManager 始终返回 nil
func TestAgentSession_ActorManager返回Nil(t *testing.T) {
	s := NewAgentSession("test-id")
	if s.ActorManager() != nil {
		t.Error("ActorManager 应始终返回 nil")
	}
}

// TestAgentSession_Close返回Nil 测试 Close 始终返回 nil
func TestAgentSession_Close返回Nil(t *testing.T) {
	s := NewAgentSession("test-id")
	if err := s.Close(); err != nil {
		t.Errorf("Close 应返回 nil，实际 %v", err)
	}
}

// TestAgentSession_Card 测试 Card 方法
func TestAgentSession_Card(t *testing.T) {
	s := NewAgentSession("test-id")
	if s.Card() != nil {
		t.Error("默认 Card 应为 nil")
	}

	s2 := NewAgentSession("test-id", WithCard("my-card"))
	if s2.Card() != "my-card" {
		t.Errorf("Card 期望 my-card，实际 %v", s2.Card())
	}
}
```

- [ ] **Step 3: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/internal/ -v -count=1 2>&1 | head -20`

Expected: 编译失败，包不存在

- [ ] **Step 4: 实现 AgentSession**

```go
// internal/agentcore/session/internal/agent_session.go
package internal

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentSession Agent 内部会话，实现 BaseSession 接口。
//
// 持有会话运行所需的基础设施组件（配置、状态、追踪器、流写入管理器、检查点器），
// 是纯粹的组件容器，不包含业务逻辑。业务逻辑由公开层 Session 负责。
//
// 对应 Python: openjiuwen/core/session/internal/agent.py (AgentSession)
type AgentSession struct {
	// sessionID 会话唯一标识
	sessionID string
	// config 会话配置
	// ⤵️ 5.12 回填：any → SessionConfig
	config any
	// state 会话状态（AgentStateCollection）
	state state.State
	// tracer 追踪器
	// ⤵️ 5.11 回填：any → Tracer
	tracer any
	// streamWriterManager 流写入管理器
	// ⤵️ 5.10 回填：any → StreamWriterManager
	streamWriterManager any
	// checkpointer 检查点器
	// ⤵️ 5.8 回填：any → Checkpointer
	checkpointer any
	// agentSpan Agent 追踪跨度
	agentSpan any
	// card Agent 身份元数据
	// ⤵️ 后续回填：any → *schema.AgentCard
	card any
}

// ──────────────────────────── 枚举 ────────────────────────────

// AgentSessionOption AgentSession 构造选项函数类型
type AgentSessionOption func(*AgentSession)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentSession 创建内部 AgentSession 实例。
//
// 默认创建 AgentStateCollection 作为状态存储。
// 可通过选项函数注入各基础设施组件。
func NewAgentSession(sessionID string, opts ...AgentSessionOption) *AgentSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_agent_session").
		Str("session_id", sessionID).
		Msg("创建内部 AgentSession")

	s := &AgentSession{
		sessionID: sessionID,
		state:     state.NewAgentStateCollection(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithConfig 设置会话配置的选项
func WithConfig(config any) AgentSessionOption {
	return func(s *AgentSession) {
		s.config = config
	}
}

// WithState 设置会话状态的选项
func WithState(st state.State) AgentSessionOption {
	return func(s *AgentSession) {
		s.state = st
	}
}

// WithTracer 设置追踪器的选项
func WithTracer(tracer any) AgentSessionOption {
	return func(s *AgentSession) {
		s.tracer = tracer
	}
}

// WithStreamWriterManager 设置流写入管理器的选项
func WithStreamWriterManager(mgr any) AgentSessionOption {
	return func(s *AgentSession) {
		s.streamWriterManager = mgr
	}
}

// WithCheckpointer 设置检查点器的选项
func WithCheckpointer(cp any) AgentSessionOption {
	return func(s *AgentSession) {
		s.checkpointer = cp
	}
}

// WithCard 设置 Agent 身份元数据的选项
func WithCard(card any) AgentSessionOption {
	return func(s *AgentSession) {
		s.card = card
	}
}

// WithAgentSpan 设置 Agent 追踪跨度的选项
func WithAgentSpan(span any) AgentSessionOption {
	return func(s *AgentSession) {
		s.agentSpan = span
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 以下方法实现 session.BaseSession 接口，但包内不显式引入接口约束，
// 由测试文件 TestAgentSession_接口实现 验证接口满足。

// Config 获取会话配置
func (s *AgentSession) Config() any {
	return s.config
}

// State 获取会话状态
func (s *AgentSession) State() state.State {
	return s.state
}

// Tracer 获取追踪器
func (s *AgentSession) Tracer() any {
	return s.tracer
}

// StreamWriterManager 获取流写入管理器
func (s *AgentSession) StreamWriterManager() any {
	return s.streamWriterManager
}

// SessionID 获取会话唯一标识
func (s *AgentSession) SessionID() string {
	return s.sessionID
}

// Checkpointer 获取检查点管理器
func (s *AgentSession) Checkpointer() any {
	return s.checkpointer
}

// ActorManager 获取 Actor 管理器（当前始终返回 nil）
func (s *AgentSession) ActorManager() any {
	return nil
}

// Close 关闭会话（当前为空实现，返回 nil）
func (s *AgentSession) Close() error {
	return nil
}

// Card 获取 Agent 身份元数据
func (s *AgentSession) Card() any {
	return s.card
}

// AgentSpan 获取 Agent 追踪跨度
func (s *AgentSession) AgentSpan() any {
	return s.agentSpan
}
```

- [ ] **Step 5: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/internal/ -v -count=1`

Expected: 所有测试 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/internal/
git commit -m "feat(session): add internal AgentSession implementing BaseSession"
```

---

### Task 4: 公开层 Session 实现

**Files:**
- Create: `internal/agentcore/session/agent.go`
- Create: `internal/agentcore/session/agent_test.go`
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 编写 Session 测试**

```go
// internal/agentcore/session/agent_test.go
package session

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// TestNewSession 测试构造函数
func TestNewSession(t *testing.T) {
	s := NewSession()
	if s == nil {
		t.Fatal("NewSession 返回 nil")
	}
	if s.GetSessionID() == "" {
		t.Error("SessionID 不应为空")
	}
}

// TestNewSession_自定义ID 测试自定义 sessionID
func TestNewSession_自定义ID(t *testing.T) {
	s := NewSession(WithSessionID("my-id"))
	if s.GetSessionID() != "my-id" {
		t.Errorf("SessionID 期望 my-id，实际 %s", s.GetSessionID())
	}
}

// TestSession_PreRun 测试 PreRun 触发回调
func TestSession_PreRun(t *testing.T) {
	s := NewSession(WithSessionID("test-pre-run"))

	var triggered bool
	callback.GetCallbackFramework().OnSession(callback.AgentSessionCreated,
		func(ctx context.Context, data *callback.SessionCallEventData) any {
			triggered = true
			if data.SessionID != "test-pre-run" {
				t.Errorf("回调 SessionID 期望 test-pre-run，实际 %s", data.SessionID)
			}
			return nil
		},
	)

	err := s.PreRun(context.Background())
	if err != nil {
		t.Errorf("PreRun 不应返回错误：%v", err)
	}
	if !triggered {
		t.Error("PreRun 应触发 AgentSessionCreated 回调")
	}

	// 清理回调
	callback.GetCallbackFramework().OffSession(callback.AgentSessionCreated, nil)
}

// TestSession_PreRun_幂等 测试重复调用只执行一次
func TestSession_PreRun_幂等(t *testing.T) {
	s := NewSession(WithSessionID("test-idempotent"))

	callCount := 0
	callback.GetCallbackFramework().OnSession(callback.AgentSessionCreated,
		func(ctx context.Context, data *callback.SessionCallEventData) any {
			callCount++
			return nil
		},
	)

	_ = s.PreRun(context.Background())
	_ = s.PreRun(context.Background())

	if callCount != 1 {
		t.Errorf("PreRun 幂等：回调应只触发 1 次，实际 %d 次", callCount)
	}

	// 清理
	callback.GetCallbackFramework().OffSession(callback.AgentSessionCreated, nil)
}

// TestSession_PostRun 测试 PostRun 流程
func TestSession_PostRun(t *testing.T) {
	s := NewSession()
	err := s.PostRun(context.Background())
	if err != nil {
		t.Errorf("PostRun 不应返回错误：%v", err)
	}
}

// TestSession_PostRun_幂等 测试重复调用只执行一次
func TestSession_PostRun_幂等(t *testing.T) {
	s := NewSession()
	_ = s.PostRun(context.Background())
	_ = s.PostRun(context.Background())
	// 不应 panic 或重复关闭
}

// TestSession_Commit 测试提交检查点
func TestSession_Commit(t *testing.T) {
	s := NewSession()
	err := s.Commit(context.Background())
	if err != nil {
		t.Errorf("Commit 不应返回错误：%v", err)
	}
}

// TestSession_GetSessionID 测试获取会话 ID
func TestSession_GetSessionID(t *testing.T) {
	s := NewSession(WithSessionID("abc-123"))
	if s.GetSessionID() != "abc-123" {
		t.Errorf("期望 abc-123，实际 %s", s.GetSessionID())
	}
}

// TestSession_UpdateState 测试更新状态
func TestSession_UpdateState(t *testing.T) {
	s := NewSession()
	s.UpdateState(map[string]any{"key": "value"})
}

// TestSession_GetState 测试获取状态
func TestSession_GetState(t *testing.T) {
	s := NewSession()
	s.UpdateState(map[string]any{"key": "value"})

	result := s.GetState("key")
	if result != "value" {
		t.Errorf("期望 value，实际 %v", result)
	}
}

// TestSession_DumpState 测试导出状态快照
func TestSession_DumpState(t *testing.T) {
	s := NewSession()
	s.UpdateState(map[string]any{"key": "value"})

	dump := s.DumpState()
	if dump == nil {
		t.Fatal("DumpState 不应返回 nil")
	}
}

// TestSession_桩方法返回Nil 测试桩方法不返回错误
func TestSession_桩方法返回Nil(t *testing.T) {
	s := NewSession()

	if err := s.WriteStream(nil); err != nil {
		t.Errorf("WriteStream 桩应返回 nil，实际 %v", err)
	}
	if err := s.WriteCustomStream(nil); err != nil {
		t.Errorf("WriteCustomStream 桩应返回 nil，实际 %v", err)
	}
	if ch := s.StreamIterator(); ch != nil {
		t.Errorf("StreamIterator 桩应返回 nil，实际 %v", ch)
	}
	if err := s.CloseStream(); err != nil {
		t.Errorf("CloseStream 桩应返回 nil，实际 %v", err)
	}
	if err := s.Interact(nil); err != nil {
		t.Errorf("Interact 桩应返回 nil，实际 %v", err)
	}
	if result := s.CreateWorkflowSession(); result != nil {
		t.Errorf("CreateWorkflowSession 桩应返回 nil，实际 %v", result)
	}
}

// TestSession_CloseStreamOnPostRun 测试 closeStreamOnPostRun 选项
func TestSession_CloseStreamOnPostRun(t *testing.T) {
	s1 := NewSession()
	if !s1.closeStreamOnPostRun {
		t.Error("默认 closeStreamOnPostRun 应为 true")
	}

	s2 := NewSession(WithCloseStreamOnPostRun(false))
	if s2.closeStreamOnPostRun {
		t.Error("WithCloseStreamOnPostRun(false) 后应为 false")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/ -run "TestNewSession|TestSession" -v -count=1 2>&1 | head -30`

Expected: 编译失败，`NewSession` 未定义

- [ ] **Step 3: 实现 Session**

```go
// internal/agentcore/session/agent.go
package session

import (
	"context"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Session Agent 公开会话，提供用户面向的 API。
//
// 组合内部层 AgentSession，实现 PreRun→Invoke/Stream→PostRun 完整生命周期。
// 负责：状态读写、流写入、交互、回调触发、检查点持久化。
//
// 对应 Python: openjiuwen/core/session/agent.py (Session)
type Session struct {
	// inner 内部 AgentSession 实例
	inner *internal.AgentSession
	// card Agent 身份元数据
	// ⤵️ 后续回填：any → *schema.AgentCard
	card any
	// preRunDone PreRun 是否已执行
	preRunDone bool
	// postRunDone PostRun 是否已执行
	postRunDone bool
	// closeStreamOnPostRun PostRun 时是否自动关闭流
	closeStreamOnPostRun bool
	// interaction 交互实例（懒初始化）
	// ⤵️ 5.9 回填：any → SimpleAgentInteraction
	interaction any
	// sourceMetadata 流数据来源元数据
	sourceMetadata map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// SessionOption Session 构造选项函数类型
type SessionOption func(*Session)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSession 创建公开层 Session 实例。
//
// 若未指定 sessionID，自动生成 UUID。
// 可通过选项函数注入各组件和配置。
//
// 对应 Python: openjiuwen/core/session/agent.py create_agent_session()
func NewSession(opts ...SessionOption) *Session {
	sessionID := uuid.New().String()

	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_session").
		Str("session_id", sessionID).
		Msg("创建公开层 Session")

	s := &Session{
		inner:                internal.NewAgentSession(sessionID),
		closeStreamOnPostRun: true,
		sourceMetadata:       make(map[string]any),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithSessionID 设置会话 ID 的选项
func WithSessionID(id string) SessionOption {
	return func(s *Session) {
		s.inner = internal.NewAgentSession(id)
	}
}

// WithCard 设置 Agent 身份元数据的选项
func WithCard(card any) SessionOption {
	return func(s *Session) {
		s.card = card
	}
}

// WithCloseStreamOnPostRun 设置 PostRun 时是否自动关闭流的选项
func WithCloseStreamOnPostRun(v bool) SessionOption {
	return func(s *Session) {
		s.closeStreamOnPostRun = v
	}
}

// WithSourceMetadata 设置流数据来源元数据的选项
func WithSourceMetadata(meta map[string]any) SessionOption {
	return func(s *Session) {
		s.sourceMetadata = meta
	}
}

// ──────────────────────────── 身份/配置方法 ────────────────────────────

// GetSessionID 返回会话唯一标识
func (s *Session) GetSessionID() string {
	return s.inner.SessionID()
}

// GetEnv 获取环境变量值
// ⤵️ 5.12 回填：Config() 返回真实类型后实现
func (s *Session) GetEnv(key string, defaultValue ...any) any {
	return nil
}

// GetEnvs 获取所有环境变量
// ⤵️ 5.12 回填：Config() 返回真实类型后实现
func (s *Session) GetEnvs() map[string]any {
	return nil
}

// GetAgentID 返回 Agent ID
// ⤵️ 后续回填：card 类型从 any → *schema.AgentCard 后实现
func (s *Session) GetAgentID() string {
	return ""
}

// GetAgentName 返回 Agent 名称
// ⤵️ 后续回填：card 类型从 any → *schema.AgentCard 后实现
func (s *Session) GetAgentName() string {
	return ""
}

// GetAgentDescription 返回 Agent 描述
// ⤵️ 后续回填：card 类型从 any → *schema.AgentCard 后实现
func (s *Session) GetAgentDescription() string {
	return ""
}

// ──────────────────────────── 状态读写方法 ────────────────────────────

// UpdateState 更新全局状态，委托到 inner.State() 的 AgentStateCollection
func (s *Session) UpdateState(data map[string]any) {
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		coll.UpdateGlobal(data)
	}
}

// GetState 获取全局状态值，委托到 inner.State() 的 AgentStateCollection
func (s *Session) GetState(key string) any {
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		return coll.GetGlobal(key)
	}
	return nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 AgentStateCollection
func (s *Session) DumpState() map[string]any {
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		return coll.Dump()
	}
	return nil
}

// ──────────────────────────── 流操作方法（桩实现） ────────────────────────────

// WriteStream 写入标准输出流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) WriteStream(data any) error {
	return nil
}

// WriteCustomStream 写入自定义流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) WriteCustomStream(data any) error {
	return nil
}

// StreamIterator 返回流迭代 channel。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) StreamIterator() <-chan any {
	return nil
}

// CloseStream 关闭流发射器并注销回调。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) CloseStream() error {
	return nil
}

// ──────────────────────────── 生命周期方法 ────────────────────────────

// PreRun 会话预运行：触发 AGENT_SESSION_CREATED 回调 + 检查点预执行。
//
// 幂等：多次调用只执行一次。
//
// 对应 Python: Session.pre_run()
func (s *Session) PreRun(ctx context.Context, inputs ...map[string]any) error {
	if s.preRunDone {
		return nil
	}

	// 触发 AgentSessionCreated 回调
	callback.GetCallbackFramework().TriggerSession(ctx, &callback.SessionCallEventData{
		Event:     callback.AgentSessionCreated,
		SessionID: s.GetSessionID(),
		Card:      s.card,
		Session:   s,
	})

	// 检查点预执行
	// ⤵️ 5.8 回填：Checkpointer 实现后调用 pre_agent_execute
	// 当前 checkpointer 为 nil，跳过

	s.preRunDone = true
	logger.Info(logger.ComponentAgentCore).
		Str("action", "session_pre_run").
		Str("session_id", s.GetSessionID()).
		Msg("Session PreRun 完成")

	return nil
}

// PostRun 会话后运行：关闭流 + 提交检查点。
//
// 幂等：多次调用只执行一次。
//
// 对应 Python: Session.post_run()
func (s *Session) PostRun(ctx context.Context) error {
	if s.postRunDone {
		return nil
	}

	if s.closeStreamOnPostRun {
		_ = s.CloseStream()
	}

	_ = s.Commit(ctx)

	s.postRunDone = true
	logger.Info(logger.ComponentAgentCore).
		Str("action", "session_post_run").
		Str("session_id", s.GetSessionID()).
		Msg("Session PostRun 完成")

	return nil
}

// Commit 提交当前状态到检查点（不关闭流）。
// ⤵️ 5.8 回填：Checkpointer 实现后调用 post_agent_execute
// 对应 Python: Session.commit()
func (s *Session) Commit(ctx context.Context) error {
	// 当前 checkpointer 为 nil，跳过
	return nil
}

// ──────────────────────────── 交互方法（桩实现） ────────────────────────────

// Interact 请求用户输入。
// ⤵️ 5.9 回填：SimpleAgentInteraction 实现后填充真实逻辑
func (s *Session) Interact(value any) error {
	return nil
}

// ──────────────────────────── 子会话方法（桩实现） ────────────────────────────

// CreateWorkflowSession 创建子 WorkflowSession。
// ⤵️ 5.5 回填：WorkflowSession 实现后填充真实逻辑
func (s *Session) CreateWorkflowSession() any {
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tagStreamPayload 为流数据添加来源元数据。
// 对应 Python: Session._tag_stream_payload()
func (s *Session) tagStreamPayload(data map[string]any) map[string]any {
	if len(s.sourceMetadata) == 0 {
		return data
	}
	result := make(map[string]any, len(data)+len(s.sourceMetadata))
	for k, v := range data {
		result[k] = v
	}
	for k, v := range s.sourceMetadata {
		result[k] = v
	}
	return result
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/ -v -count=1`

Expected: 所有测试 PASS（包括原有的 ProxySession 测试和新增的 Session 测试）

- [ ] **Step 5: 更新 session/doc.go**

更新文件目录和包文档，添加 Session 和 internal 子包：

```go
// Package session 提供会话管理的抽象接口、代理实现和 Agent 公开会话。
//
// 本包定义 BaseSession 接口，作为所有会话类型的统一抽象。ProxySession 实现代理模式。
// Session 是 Agent 场景下的公开会话，组合内部层 AgentSession，提供 PreRun/PostRun
// 生命周期、状态读写、流写入等用户面向 API。
//
// 文件目录：
//
//	session/
//	├── doc.go              # 包文档
//	├── session.go          # BaseSession 接口 + ProxySession 实现
//	├── agent.go            # Session 公开会话（Agent 场景）
//	├── state/              # 状态接口与内存实现（5.1 已完成）
//	└── internal/           # 内部会话实现
//	    └── agent_session.go  # AgentSession（BaseSession 实现）
//
// 对应 Python 代码：openjiuwen/core/session/agent.py + openjiuwen/core/session/session.py
//
// 核心类型/接口索引：
//
//	BaseSession    — 会话基类接口，所有会话类型的核心抽象
//	ProxySession   — 代理会话，将调用委托给内部 stub
//	Session        — Agent 公开会话，用户面向 API
package session
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/agent.go internal/agentcore/session/agent_test.go internal/agentcore/session/doc.go
git commit -m "feat(session): add public Session with PreRun/PostRun lifecycle"
```

---

### Task 5: 全量编译验证 + 更新实现计划

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

Expected: 编译成功，无错误

- [ ] **Step 2: 全量测试验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/... ./internal/agentcore/runner/callback/... -v -count=1`

Expected: 所有测试 PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/session/state/ ./internal/agentcore/session/internal/ ./internal/agentcore/session/ ./internal/agentcore/runner/callback/`

Expected: 各包覆盖率 ≥ 80%

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 5.3 行的状态从 `☐` 改为 `✅`：

```
| 5.3 | ✅ | AgentSession | ...
```

- [ ] **Step 5: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update 5.3 AgentSession status to completed"
```
