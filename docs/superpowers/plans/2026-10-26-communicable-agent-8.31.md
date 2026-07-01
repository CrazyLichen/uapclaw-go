# CommunicableAgent (8.31) 回填实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 回填 CommunicableAgent 与 Python 原版的差异，补充 IsBound() 方法、BindRuntime 幂等性/重绑定检测和 warning 日志。

**Architecture:** 在 CommunicableAgent 结构体上新增 IsBound() 导出方法（通过 runtime != nil && agentID != "" 判断绑定状态），修改 BindRuntime() 增加 is_bound 检测（相同则幂等跳过，不同则 warning 日志），无需新增字段。

**Tech Stack:** Go 1.22+, 项目内 logger 包

---

### Task 1: 新增 IsBound() 导出方法

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/communicable_agent.go:40-54`
- Test: `internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go`

- [ ] **Step 1: 编写 IsBound 测试**

在 `communicable_agent_test.go` 的 `TestCommunicableAgent_BindRuntime` 函数之后新增测试函数：

```go
// TestCommunicableAgent_IsBound 测试绑定状态判断
func TestCommunicableAgent_IsBound(t *testing.T) {
	t.Run("初始未绑定", func(t *testing.T) {
		c := NewCommunicableAgent()
		if c.IsBound() {
			t.Error("初始 IsBound 应为 false")
		}
	})

	t.Run("绑定后为 true", func(t *testing.T) {
		c := NewCommunicableAgent()
		runtime := &TeamRuntime{teamID: "test-team"}
		c.BindRuntime(runtime, "agent-1")
		if !c.IsBound() {
			t.Error("绑定后 IsBound 应为 true")
		}
	})

	t.Run("仅 runtime 非 nil 但 agentID 为空时为 false", func(t *testing.T) {
		c := NewCommunicableAgent()
		c.runtime = &TeamRuntime{teamID: "test-team"}
		if c.IsBound() {
			t.Error("agentID 为空时 IsBound 应为 false")
		}
	})

	t.Run("仅 agentID 非空但 runtime 为 nil 时为 false", func(t *testing.T) {
		c := NewCommunicableAgent()
		c.agentID = "agent-1"
		if c.IsBound() {
			t.Error("runtime 为 nil 时 IsBound 应为 false")
		}
	})
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/ -run TestCommunicableAgent_IsBound -v -count=1`
Expected: FAIL — `c.IsBound undefined`

- [ ] **Step 3: 实现 IsBound() 方法**

在 `communicable_agent.go` 的 `NewCommunicableAgent()` 函数之后、`BindRuntime()` 函数之前，新增：

```go
// IsBound 判断是否已绑定运行时。
//
// 对应 Python: CommunicableAgent.is_bound 属性
func (c *CommunicableAgent) IsBound() bool {
	return c.runtime != nil && c.agentID != ""
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/ -run TestCommunicableAgent_IsBound -v -count=1`
Expected: PASS

- [ ] **Step 5: 更新现有 BindRuntime 测试补充 IsBound 断言**

在 `communicable_agent_test.go` 的 `TestCommunicableAgent_BindRuntime` 函数中，在 `c.BindRuntime(runtime, "agent-1")` 之后补充断言：

```go
		if !c.IsBound() {
			t.Error("绑定后 IsBound 应为 true")
		}
```

完整的 `TestCommunicableAgent_BindRuntime` 改为：

```go
// TestCommunicableAgent_BindRuntime 测试绑定运行时
func TestCommunicableAgent_BindRuntime(t *testing.T) {
	t.Run("绑定后可访问运行时", func(t *testing.T) {
		c := NewCommunicableAgent()
		if c.Runtime() != nil {
			t.Error("初始 Runtime 应为 nil")
		}
		if c.AgentID() != "" {
			t.Error("初始 AgentID 应为空")
		}
		if c.IsBound() {
			t.Error("初始 IsBound 应为 false")
		}

		runtime := &TeamRuntime{teamID: "test-team"}
		c.BindRuntime(runtime, "agent-1")

		if c.Runtime() != runtime {
			t.Error("Runtime 绑定失败")
		}
		if c.AgentID() != "agent-1" {
			t.Errorf("AgentID = %q, want %q", c.AgentID(), "agent-1")
		}
		if !c.IsBound() {
			t.Error("绑定后 IsBound 应为 true")
		}
	})
}
```

- [ ] **Step 6: 运行全部相关测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/ -v -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/communicable_agent.go internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go
git commit -m "feat(multi_agent): 新增 CommunicableAgent.IsBound() 方法，对齐 Python is_bound 属性"
```

---

### Task 2: 修改 BindRuntime — 幂等性 + 重绑定检测 + warning 日志

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/communicable_agent.go:3-8,47-54`
- Test: `internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go`

- [ ] **Step 1: 编写幂等绑定和重绑定测试**

在 `communicable_agent_test.go` 新增两个测试函数：

```go
// TestCommunicableAgent_BindRuntime_幂等绑定 测试相同 runtime 和 agentID 再次绑定
func TestCommunicableAgent_BindRuntime_幂等绑定(t *testing.T) {
	c := NewCommunicableAgent()
	runtime := &TeamRuntime{teamID: "test-team"}
	c.BindRuntime(runtime, "agent-1")

	// 相同 runtime 和 agentID 再次绑定 — 应幂等跳过
	c.BindRuntime(runtime, "agent-1")

	if c.Runtime() != runtime {
		t.Error("幂等绑定后 Runtime 应不变")
	}
	if c.AgentID() != "agent-1" {
		t.Error("幂等绑定后 AgentID 应不变")
	}
	if !c.IsBound() {
		t.Error("幂等绑定后 IsBound 应为 true")
	}
}

// TestCommunicableAgent_BindRuntime_重绑定 测试不同 runtime 再次绑定
func TestCommunicableAgent_BindRuntime_重绑定(t *testing.T) {
	c := NewCommunicableAgent()
	runtime1 := &TeamRuntime{teamID: "team-1"}
	c.BindRuntime(runtime1, "agent-1")

	// 不同 runtime 再次绑定 — 应覆盖并记录 warning
	runtime2 := &TeamRuntime{teamID: "team-2"}
	c.BindRuntime(runtime2, "agent-2")

	if c.Runtime() != runtime2 {
		t.Error("重绑定后 Runtime 应为新值")
	}
	if c.AgentID() != "agent-2" {
		t.Error("重绑定后 AgentID 应为新值")
	}
}

// TestCommunicableAgent_BindRuntime_相同runtime不同agentID 测试相同 runtime 但不同 agentID
func TestCommunicableAgent_BindRuntime_相同runtime不同agentID(t *testing.T) {
	c := NewCommunicableAgent()
	runtime := &TeamRuntime{teamID: "test-team"}
	c.BindRuntime(runtime, "agent-1")

	// 相同 runtime 但不同 agentID — 应覆盖并记录 warning
	c.BindRuntime(runtime, "agent-2")

	if c.AgentID() != "agent-2" {
		t.Error("重绑定后 AgentID 应为新值")
	}
}
```

- [ ] **Step 2: 运行测试确认失败（幂等性未实现）**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/ -run "TestCommunicableAgent_BindRuntime_幂等绑定|TestCommunicableAgent_BindRuntime_重绑定|TestCommunicableAgent_BindRuntime_相同runtime不同agentID" -v -count=1`
Expected: 可能 PASS（因为当前 BindRuntime 直接覆盖，重绑定测试会通过，但幂等测试也可能通过。需要验证行为正确性）

- [ ] **Step 3: 添加 logger 导入并修改 BindRuntime 实现**

修改 `communicable_agent.go`：

1. 在 import 中添加 logger 包：

```go
import (
	"context"
	"fmt"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

2. 将 BindRuntime 替换为：

```go
// BindRuntime 绑定团队运行时，注入运行时引用和 Agent 标识。
// 实现 RuntimeBindable 接口。
//
// 幂等性：相同 runtime 和 agentID 时静默跳过。
// 重绑定：已绑定到不同 runtime 或 agentID 时记录 warning 日志。
//
// 对应 Python: CommunicableAgent.bind_runtime(runtime, agent_id)
func (c *CommunicableAgent) BindRuntime(runtime *TeamRuntime, agentID string) {
	if c.IsBound() {
		if c.runtime == runtime && c.agentID == agentID {
			// 相同 runtime 和 agentID — 幂等，静默跳过
			return
		}
		// 不同 runtime 或 agentID — 重绑定，记录 warning
		logger.Warn(logComponent).
			Str("event_type", "RUNTIME_REBIND").
			Str("class_name", "CommunicableAgent").
			Str("agent_id", c.agentID).
			Msg("Agent 已绑定到运行时，重新绑定可能导致意外行为")
	}
	c.runtime = runtime
	c.agentID = agentID
}
```

- [ ] **Step 4: 运行全部测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/team_runtime/ -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/communicable_agent.go internal/agentcore/multi_agent/team_runtime/communicable_agent_test.go
git commit -m "feat(multi_agent): BindRuntime 增加幂等性和重绑定检测，对齐 Python bind_runtime 逻辑并同步 warning 日志"
```

---

### Task 3: 运行完整包测试 + 编译验证

**Files:** 无新增

- [ ] **Step 1: 运行完整包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -v -count=1`
Expected: PASS

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 无编译错误

- [ ] **Step 3: 提交（如有修复）**

仅在 Step 1 或 Step 2 发现问题时提交修复。
