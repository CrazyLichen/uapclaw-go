# BaseSession 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 BaseSession 接口和 ProxySession 代理类，为后续 AgentSession/WorkflowSession 等会话类型提供统一抽象。

**Architecture:** 在 `internal/agentcore/session/` 包下新建 `session.go` 定义 BaseSession 接口（8 个方法）和 ProxySession 结构体（全部委托给内部 stub）。依赖的 Config/Tracer/StreamWriterManager/Checkpointer/ActorManager 类型用 `any` 占位，标注 ⤵️ 回填标记。同步新建 `doc.go` 包文档。

**Tech Stack:** Go 1.25，依赖已有 `internal/agentcore/session/state` 包的 `State` 接口。

**设计文档：** `docs/superpowers/specs/2025-07-30-base-session-design.md`

---

### Task 1: 创建 session 包 doc.go

**Files:**
- Create: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 创建 doc.go 文件**

```go
// Package session 提供会话管理的抽象接口和代理实现。
//
// 本包定义 BaseSession 接口，作为所有会话类型（AgentSession、WorkflowSession 等）
// 的统一抽象。BaseSession 提供 Config/State/Tracer/StreamWriterManager/SessionID/
// Checkpointer/ActorManager/Close 八个核心能力。ProxySession 实现代理模式，将调用
// 委托给内部 stub，支持运行时替换底层会话。
//
// 本包依赖 state 子包提供的状态接口（State/CommitState 等），Config/Tracer/
// StreamWriterManager/Checkpointer/ActorManager 等依赖类型暂用 any 占位，
// 待后续步骤（5.8/5.10/5.11/5.12）回填具体类型。
//
// 文件目录：
//
//	session/
//	├── doc.go              # 包文档
//	├── session.go          # BaseSession 接口 + ProxySession 实现
//	└── state/              # 状态接口与内存实现（5.1 已完成）
//
// 对应 Python 代码：openjiuwen/core/session/session.py
//
// 核心类型/接口索引：
//
//	BaseSession    — 会话基类接口，所有会话类型的核心抽象
//	ProxySession   — 代理会话，将调用委托给内部 stub
package session
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/`
Expected: 编译成功（无输出）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/doc.go
git commit -m "feat(session): add session package doc.go"
```

---

### Task 2: 创建 BaseSession 接口定义

**Files:**
- Create: `internal/agentcore/session/session.go`

- [ ] **Step 1: 创建 session.go，定义 BaseSession 接口和 ProxySession**

```go
package session

import "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"

// ──────────────────────────── 接口 ────────────────────────────

// BaseSession 会话基类接口，定义所有会话类型共有的核心能力。
// 对应 Python: openjiuwen/core/session/session.py BaseSession
//
// 8 个方法按职责分三类：
//   - 身份与配置：SessionID、Config
//   - 核心能力：State、Tracer、StreamWriterManager、Checkpointer、ActorManager
//   - 生命周期：Close
type BaseSession interface {
	// Config 获取会话配置
	// ⤵️ 5.12 回填：返回类型从 any 改为 SessionConfig
	Config() any
	// State 获取会话状态
	State() state.State
	// Tracer 获取会话追踪器
	// ⤵️ 5.11 回填：返回类型从 any 改为 Tracer
	Tracer() any
	// StreamWriterManager 获取流写入管理器
	// ⤵️ 5.10 回填：返回类型从 any 改为 StreamWriterManager
	StreamWriterManager() any
	// SessionID 获取会话唯一标识
	SessionID() string
	// Checkpointer 获取检查点管理器
	// ⤵️ 5.8 回填：返回类型从 any 改为 Checkpointer
	Checkpointer() any
	// ActorManager 获取 Actor 管理器（可选，默认返回 nil）
	// ⤵️ 后续回填：返回类型从 any 改为 ActorManager
	ActorManager() any
	// Close 关闭会话，释放资源
	Close() error
}

// ──────────────────────────── 结构体 ────────────────────────────

// ProxySession 代理会话，将所有 BaseSession 方法委托给内部 stub。
// 对应 Python: openjiuwen/core/session/session.py ProxySession
//
// 修正 Python 遗漏：Python 的 ProxySession 未覆盖 actor_manager 和 close 方法，
// 导致这两个方法不委托给 stub。Go 实现中全部 8 个方法均委托给 stub。
//
// 使用模式：先 NewProxySession() 创建空实例，后续通过 SetSession() 注入真正的会话。
// stub 为 nil 时调用任何方法会 panic。
type ProxySession struct {
	// stub 被代理的底层会话
	stub BaseSession
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProxySession 创建代理会话实例（stub 为 nil）。
// 必须在调用 BaseSession 方法之前通过 SetSession 注入底层会话，否则 panic。
func NewProxySession() *ProxySession {
	return &ProxySession{}
}

// ──────────────────────────── ProxySession 方法 ────────────────────────────

// SetSession 设置被代理的底层会话
func (p *ProxySession) SetSession(stub BaseSession) {
	p.stub = stub
}

// Config 获取底层会话的配置
func (p *ProxySession) Config() any {
	return p.stub.Config()
}

// State 获取底层会话的状态
func (p *ProxySession) State() state.State {
	return p.stub.State()
}

// Tracer 获取底层会话的追踪器
func (p *ProxySession) Tracer() any {
	return p.stub.Tracer()
}

// StreamWriterManager 获取底层会话的流写入管理器
func (p *ProxySession) StreamWriterManager() any {
	return p.stub.StreamWriterManager()
}

// SessionID 获取底层会话的唯一标识
func (p *ProxySession) SessionID() string {
	return p.stub.SessionID()
}

// Checkpointer 获取底层会话的检查点管理器
func (p *ProxySession) Checkpointer() any {
	return p.stub.Checkpointer()
}

// ActorManager 获取底层会话的 Actor 管理器
func (p *ProxySession) ActorManager() any {
	return p.stub.ActorManager()
}

// Close 关闭底层会话
func (p *ProxySession) Close() error {
	return p.stub.Close()
}
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/`
Expected: 编译成功（无输出）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/session.go
git commit -m "feat(session): add BaseSession interface and ProxySession"
```

---

### Task 3: 编写 BaseSession 接口满足性测试

**Files:**
- Create: `internal/agentcore/session/session_test.go`

- [ ] **Step 1: 创建 session_test.go，编写接口满足性测试和 ProxySession 基础测试**

```go
package session

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// Test接口满足_ProxySession 验证 ProxySession 满足 BaseSession 接口。
func Test接口满足_ProxySession(t *testing.T) {
	var _ BaseSession = (*ProxySession)(nil)
}

// TestNewProxySession 验证 NewProxySession 创建的实例 stub 为 nil。
func TestNewProxySession(t *testing.T) {
	p := NewProxySession()
	if p.stub != nil {
		t.Errorf("NewProxySession() stub 应为 nil，实际为 %v", p.stub)
	}
}
```

- [ ] **Step 2: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -run "Test接口满足_ProxySession|TestNewProxySession" -v`
Expected: 两个测试 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/session_test.go
git commit -m "test(session): add BaseSession interface compliance and NewProxySession tests"
```

---

### Task 4: 编写 mock stub 和 ProxySession 委托测试

**Files:**
- Modify: `internal/agentcore/session/session_test.go`

- [ ] **Step 1: 在 session_test.go 中添加 mockStub 和委托测试**

在文件末尾追加以下代码：

```go
// ──────────────────────────── 非导出类型 ────────────────────────────

// mockStub 用于测试的 BaseSession 模拟实现
type mockStub struct {
	configVal              any
	stateVal               state.State
	tracerVal              any
	streamWriterManagerVal any
	sessionIDVal           string
	checkpointerVal        any
	actorManagerVal        any
	closeErr               error
	closeCalled            bool
}

func (m *mockStub) Config() any              { return m.configVal }
func (m *mockStub) State() state.State       { return m.stateVal }
func (m *mockStub) Tracer() any              { return m.tracerVal }
func (m *mockStub) StreamWriterManager() any { return m.streamWriterManagerVal }
func (m *mockStub) SessionID() string        { return m.sessionIDVal }
func (m *mockStub) Checkpointer() any        { return m.checkpointerVal }
func (m *mockStub) ActorManager() any        { return m.actorManagerVal }
func (m *mockStub) Close() error             { m.closeCalled = true; return m.closeErr }
```

然后追加委托测试：

```go
// TestProxySession_SetSession 验证 SetSession 正确注入底层会话。
func TestProxySession_SetSession(t *testing.T) {
	p := NewProxySession()
	stub := &mockStub{sessionIDVal: "test-id"}
	p.SetSession(stub)
	if p.stub != stub {
		t.Error("SetSession 后 stub 应指向注入的实例")
	}
}

// TestProxySession_委托全部方法 验证 ProxySession 的 8 个方法全部委托给 stub。
func TestProxySession_委托全部方法(t *testing.T) {
	// 准备 mock 数据
	expectedState := state.NewInMemoryState()
	stub := &mockStub{
		configVal:              "config-value",
		stateVal:               expectedState,
		tracerVal:              "tracer-value",
		streamWriterManagerVal: "swm-value",
		sessionIDVal:           "session-123",
		checkpointerVal:        "checkpointer-value",
		actorManagerVal:        "actor-value",
		closeErr:               nil,
	}

	p := NewProxySession()
	p.SetSession(stub)

	// 验证 Config 委托
	if got := p.Config(); got != "config-value" {
		t.Errorf("Config() = %v, 期望 %v", got, "config-value")
	}

	// 验证 State 委托
	if got := p.State(); got != expectedState {
		t.Errorf("State() = %v, 期望 %v", got, expectedState)
	}

	// 验证 Tracer 委托
	if got := p.Tracer(); got != "tracer-value" {
		t.Errorf("Tracer() = %v, 期望 %v", got, "tracer-value")
	}

	// 验证 StreamWriterManager 委托
	if got := p.StreamWriterManager(); got != "swm-value" {
		t.Errorf("StreamWriterManager() = %v, 期望 %v", got, "swm-value")
	}

	// 验证 SessionID 委托
	if got := p.SessionID(); got != "session-123" {
		t.Errorf("SessionID() = %v, 期望 %v", got, "session-123")
	}

	// 验证 Checkpointer 委托
	if got := p.Checkpointer(); got != "checkpointer-value" {
		t.Errorf("Checkpointer() = %v, 期望 %v", got, "checkpointer-value")
	}

	// 验证 ActorManager 委托
	if got := p.ActorManager(); got != "actor-value" {
		t.Errorf("ActorManager() = %v, 期望 %v", got, "actor-value")
	}

	// 验证 Close 委托
	err := p.Close()
	if err != nil {
		t.Errorf("Close() 返回意外错误: %v", err)
	}
	if !stub.closeCalled {
		t.Error("Close() 未委托到 stub")
	}
}

// TestProxySession_Close传播错误 验证 ProxySession.Close 传播 stub 的错误。
func TestProxySession_Close传播错误(t *testing.T) {
	expectedErr := errors.New("close failed")
	stub := &mockStub{closeErr: expectedErr}

	p := NewProxySession()
	p.SetSession(stub)

	err := p.Close()
	if err != expectedErr {
		t.Errorf("Close() 错误 = %v, 期望 %v", err, expectedErr)
	}
}

// TestProxySession_NilStub时Panic 验证 stub 为 nil 时调用方法 panic。
func TestProxySession_NilStub时Panic(t *testing.T) {
	p := NewProxySession()

	// 测试每个方法在 nil stub 时 panic
	panicTests := []struct {
		name string
		fn   func()
	}{
		{"Config", func() { p.Config() }},
		{"State", func() { p.State() }},
		{"Tracer", func() { p.Tracer() }},
		{"StreamWriterManager", func() { p.StreamWriterManager() }},
		{"SessionID", func() { p.SessionID() }},
		{"Checkpointer", func() { p.Checkpointer() }},
		{"ActorManager", func() { p.ActorManager() }},
		{"Close", func() { p.Close() }},
	}

	for _, tt := range panicTests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s 在 nil stub 时未 panic", tt.name)
				}
			}()
			tt.fn()
		})
	}
}
```

需要在文件顶部 import 中添加 `"errors"` 和 `state` 包：

```go
import (
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)
```

- [ ] **Step 2: 运行全部 session 包测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v`
Expected: 所有测试 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/session_test.go
git commit -m "test(session): add ProxySession delegation and nil-stub panic tests"
```

---

### Task 5: 更新 IMPLEMENTATION_PLAN.md 5.2 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` 第 341 行附近

- [ ] **Step 1: 将 5.2 状态从 ☐ 改为 ✅**

找到这一行：

```
| 5.2 | ☐ | BaseSession 接口 | `Config/State/SessionID/Close` | `openjiuwen/core/session/session.py` |
```

替换为：

```
| 5.2 | ✅ | BaseSession 接口 | `Config/State/SessionID/Close`；⤵️ Config 返回类型待 5.12 回填；⤵️ Tracer 返回类型待 5.11 回填；⤵️ StreamWriterManager 返回类型待 5.10 回填；⤵️ Checkpointer 返回类型待 5.8 回填；⤵️ ActorManager 返回类型待后续回填 | `openjiuwen/core/session/session.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update 5.2 status to completed in IMPLEMENTATION_PLAN"
```

---

### Task 6: 最终验证

- [ ] **Step 1: 运行全部 session 包测试（含子包）**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -v`
Expected: 所有测试 PASS

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/session/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行全项目编译确认无破坏**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功（无输出）
