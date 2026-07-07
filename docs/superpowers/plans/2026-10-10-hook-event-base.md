# 10.1.7 HookEventBase 钩子事件基类实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 HookEventBase 钩子事件基类，完成 10.1 Schema 层最后一个未实现步骤

**Architecture:** 采用嵌入式组合模式，HookEventBase 为 struct{Scope string}，提供 GetEvent/BuildEventName/ParseEventName 函数，子类在各自消费方包中嵌入使用。最小实现，不预留扩展点。

**Tech Stack:** Go 1.24+，标准库 encoding/json，项目编码规范（中文注释、声明排序、≥85% 覆盖率）

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/swarm/schema/event_base.go` | HookEventBase 结构体 + BuildEventName/ParseEventName/NewHookEventBase + DefaultScope |
| 创建 | `internal/swarm/schema/event_base_test.go` | 12 个测试用例，覆盖全部导出 API |
| 修改 | `internal/swarm/schema/doc.go` | 文件目录新增 event_base.go 条目 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 10.1.7 状态 ☐ → ✅ |

---

### Task 1: 创建 event_base.go — HookEventBase 基类实现

**Files:**
- Create: `internal/swarm/schema/event_base.go`

- [ ] **Step 1: 创建 event_base.go 文件**

```go
package schema

import "encoding/json"

// ──────────────────────────── 结构体 ────────────────────────────

// HookEventBase 带 scope 的钩子事件名基类（与 openjiuwen 0.1.9 EventBase 行为一致）。
//
// 子类通过组合嵌入 HookEventBase 并调用 GetEvent 构建带作用域前缀的事件名。
// Python 中通过 __init_subclass__ 元编程自动替换 scope 前缀，
// Go 中通过构造函数显式初始化实现等价效果。
//
// 对应 Python: jiuwenswarm/common/schema/event_base.py (HookEventBase)
type HookEventBase struct {
	// Scope 事件作用域，默认为 DefaultScope ("_framework")
	Scope string `json:"scope"`
}

// GetEvent 用当前 scope 构建完整事件名。
//
// 返回格式为 "scope:eventName"，对齐 Python HookEventBase.get_event。
func (h *HookEventBase) GetEvent(eventName string) string {
	return BuildEventName(h.Scope, eventName)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultScope 默认事件作用域，对齐 Python DEFAULT_SCOPE
	DefaultScope = "_framework"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildEventName 拼接作用域事件名。
//
// 返回格式为 "scope:eventName"，对齐 Python build_event_name。
func BuildEventName(scope, eventName string) string {
	return scope + ":" + eventName
}

// ParseEventName 解析作用域事件名。
//
// 按第一个冒号拆分为 (scope, eventName)；无冒号时 scope 回退为 DefaultScope。
// 对齐 Python parse_event_name。
func ParseEventName(scopedEvent string) (scope, eventName string) {
	for i := 0; i < len(scopedEvent); i++ {
		if scopedEvent[i] == ':' {
			return scopedEvent[:i], scopedEvent[i+1:]
		}
	}
	return DefaultScope, scopedEvent
}

// NewHookEventBase 创建 HookEventBase 实例，Scope 默认为 DefaultScope。
func NewHookEventBase() *HookEventBase {
	return &HookEventBase{Scope: DefaultScope}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalHookEventBase 序列化 HookEventBase 为 JSON 字节。
// 用于测试中验证 JSON 往返一致性。
func marshalHookEventBase(h *HookEventBase) ([]byte, error) {
	return json.Marshal(h)
}

// unmarshalHookEventBase 从 JSON 字节反序列化 HookEventBase。
// 用于测试中验证 JSON 往返一致性。
func unmarshalHookEventBase(data []byte) (*HookEventBase, error) {
	var h HookEventBase
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 无错误输出

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/schema/event_base.go
git commit -m "feat(schema): 实现 HookEventBase 钩子事件基类 (10.1.7)"
```

---

### Task 2: 创建 event_base_test.go — 全量单元测试

**Files:**
- Create: `internal/swarm/schema/event_base_test.go`

- [ ] **Step 1: 创建测试文件**

```go
package schema

import (
	"testing"
)

// ──────────────────────────── BuildEventName 测试 ────────────────────────────

// TestBuildEventName 拼接 scope:eventName
func TestBuildEventName(t *testing.T) {
	got := BuildEventName("gateway", "before_chat_request")
	want := "gateway:before_chat_request"
	if got != want {
		t.Errorf("BuildEventName(\"gateway\", \"before_chat_request\") = %q, want %q", got, want)
	}
}

// TestBuildEventName_空scope scope 为空字符串
func TestBuildEventName_空scope(t *testing.T) {
	got := BuildEventName("", "event")
	want := ":event"
	if got != want {
		t.Errorf("BuildEventName(\"\", \"event\") = %q, want %q", got, want)
	}
}

// TestBuildEventName_空eventName eventName 为空字符串
func TestBuildEventName_空eventName(t *testing.T) {
	got := BuildEventName("scope", "")
	want := "scope:"
	if got != want {
		t.Errorf("BuildEventName(\"scope\", \"\") = %q, want %q", got, want)
	}
}

// TestBuildEventName_默认Scope 使用 DefaultScope 拼接
func TestBuildEventName_默认Scope(t *testing.T) {
	got := BuildEventName(DefaultScope, "started")
	want := "_framework:started"
	if got != want {
		t.Errorf("BuildEventName(DefaultScope, \"started\") = %q, want %q", got, want)
	}
}

// ──────────────────────────── ParseEventName 测试 ────────────────────────────

// TestParseEventName 正常解析
func TestParseEventName(t *testing.T) {
	scope, eventName := ParseEventName("gateway:before_chat_request")
	if scope != "gateway" {
		t.Errorf("scope = %q, want %q", scope, "gateway")
	}
	if eventName != "before_chat_request" {
		t.Errorf("eventName = %q, want %q", eventName, "before_chat_request")
	}
}

// TestParseEventName_无冒号 无冒号回退默认 scope
func TestParseEventName_无冒号(t *testing.T) {
	scope, eventName := ParseEventName("before_chat_request")
	if scope != DefaultScope {
		t.Errorf("scope = %q, want %q", scope, DefaultScope)
	}
	if eventName != "before_chat_request" {
		t.Errorf("eventName = %q, want %q", eventName, "before_chat_request")
	}
}

// TestParseEventName_多冒号 多冒号只拆第一个
func TestParseEventName_多冒号(t *testing.T) {
	scope, eventName := ParseEventName("a:b:c")
	if scope != "a" {
		t.Errorf("scope = %q, want %q", scope, "a")
	}
	if eventName != "b:c" {
		t.Errorf("eventName = %q, want %q", eventName, "b:c")
	}
}

// TestParseEventName_与Build往返 往返一致性
func TestParseEventName_与Build往返(t *testing.T) {
	tests := []struct {
		scope     string
		eventName string
	}{
		{"gateway", "before_chat_request"},
		{"agent_server", "memory_before_chat"},
		{DefaultScope, "started"},
		{"a", "b"},
	}
	for _, tt := range tests {
		built := BuildEventName(tt.scope, tt.eventName)
		scope, eventName := ParseEventName(built)
		if scope != tt.scope {
			t.Errorf("ParseEventName(BuildEventName(%q, %q)): scope = %q, want %q", tt.scope, tt.eventName, scope, tt.scope)
		}
		if eventName != tt.eventName {
			t.Errorf("ParseEventName(BuildEventName(%q, %q)): eventName = %q, want %q", tt.scope, tt.eventName, eventName, tt.eventName)
		}
	}
}

// TestParseEventName_空字符串 空字符串输入
func TestParseEventName_空字符串(t *testing.T) {
	scope, eventName := ParseEventName("")
	if scope != DefaultScope {
		t.Errorf("scope = %q, want %q", scope, DefaultScope)
	}
	if eventName != "" {
		t.Errorf("eventName = %q, want %q", eventName, "")
	}
}

// ──────────────────────────── HookEventBase 测试 ────────────────────────────

// TestNewHookEventBase 工厂函数默认 Scope
func TestNewHookEventBase(t *testing.T) {
	h := NewHookEventBase()
	if h.Scope != DefaultScope {
		t.Errorf("NewHookEventBase().Scope = %q, want %q", h.Scope, DefaultScope)
	}
}

// TestHookEventBase_GetEvent GetEvent 方法
func TestHookEventBase_GetEvent(t *testing.T) {
	h := &HookEventBase{Scope: "gateway"}
	got := h.GetEvent("started")
	want := "gateway:started"
	if got != want {
		t.Errorf("GetEvent(\"started\") = %q, want %q", got, want)
	}
}

// TestHookEventBase_GetEvent_默认Scope 默认 Scope 的 GetEvent
func TestHookEventBase_GetEvent_默认Scope(t *testing.T) {
	h := NewHookEventBase()
	got := h.GetEvent("started")
	want := "_framework:started"
	if got != want {
		t.Errorf("GetEvent(\"started\") = %q, want %q", got, want)
	}
}

// TestDefaultScope 常量值
func TestDefaultScope(t *testing.T) {
	if DefaultScope != "_framework" {
		t.Errorf("DefaultScope = %q, want %q", DefaultScope, "_framework")
	}
}

// TestHookEventBase_JSON往返 JSON 序列化往返
func TestHookEventBase_JSON往返(t *testing.T) {
	original := &HookEventBase{Scope: "gateway"}

	data, err := marshalHookEventBase(original)
	if err != nil {
		t.Fatalf("marshalHookEventBase 失败: %v", err)
	}

	decoded, err := unmarshalHookEventBase(data)
	if err != nil {
		t.Fatalf("unmarshalHookEventBase 失败: %v", err)
	}

	if decoded.Scope != original.Scope {
		t.Errorf("decoded.Scope = %q, want %q", decoded.Scope, original.Scope)
	}
}

// TestHookEventBase_JSON往返_默认Scope 默认 Scope 的 JSON 往返
func TestHookEventBase_JSON往返_默认Scope(t *testing.T) {
	original := NewHookEventBase()

	data, err := marshalHookEventBase(original)
	if err != nil {
		t.Fatalf("marshalHookEventBase 失败: %v", err)
	}

	decoded, err := unmarshalHookEventBase(data)
	if err != nil {
		t.Fatalf("unmarshalHookEventBase 失败: %v", err)
	}

	if decoded.Scope != DefaultScope {
		t.Errorf("decoded.Scope = %q, want %q", decoded.Scope, DefaultScope)
	}
}
```

- [ ] **Step 2: 运行测试验证全部通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/swarm/schema/ -run "TestBuildEventName|TestParseEventName|TestNewHookEventBase|TestHookEventBase_GetEvent|TestDefaultScope|TestHookEventBase_JSON"`
Expected: 全部 PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/schema/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/schema/event_base_test.go
git commit -m "test(schema): HookEventBase 全量单元测试 (10.1.7)"
```

---

### Task 3: 更新 doc.go — 文件目录新增条目

**Files:**
- Modify: `internal/swarm/schema/doc.go:10-17`

- [ ] **Step 1: 更新 doc.go 文件目录**

在 `schema/` 树形结构中 `agent.go` 和 `permission.go` 之间新增 `event_base.go` 条目，并更新包功能概述：

```go
// Package schema 提供 E2A 协议和 Gateway/AgentServer 通信所需的全部类型定义。
//
// 本包定义了 E2A 协议的核心数据模型，包括 RPC 方法名枚举（ReqMethod）、
// 事件类型枚举（EventType）、运行模式枚举（Mode）、消息方向类型枚举（MessageType）、
// 消息模型（Message）、Agent 请求/响应模型（AgentRequest/AgentResponse/AgentResponseChunk）、
// 钩子事件基类（HookEventBase）、权限上下文（PermissionContext）等，作为 swarm 层的类型基础。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── req_method.go    # ReqMethod 枚举（142 个 RPC 方法名）
//	├── event_type.go    # EventType 枚举（26 个事件类型）
//	├── mode.go          # Mode 枚举（6 个运行模式）
//	├── message.go       # MessageType 枚举 + Message 模型 + 工厂函数 + Validate
//	├── agent.go         # AgentRequest/AgentResponse/AgentResponseChunk 模型 + 工厂函数 + Validate + IsTerminal/NewTerminalChunk
//	├── event_base.go    # HookEventBase 钩子事件基类 + BuildEventName/ParseEventName
//	└── permission.go    # PermissionContext 权限上下文 + 派生方法 + 序列化 + 工厂函数 + Validate
//
// 对应 Python 代码：jiuwenswarm/common/schema/
package schema
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 无错误输出

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/schema/doc.go
git commit -m "docs(schema): doc.go 新增 event_base.go 条目 (10.1.7)"
```

---

### Task 4: 更新 IMPLEMENTATION_PLAN.md — 状态标记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:622`

- [ ] **Step 1: 更新 10.1.7 状态**

将第 622 行：
```
| 10.1.7 | ☐ | HookEventBase | 钩子事件基类 | `jiuwenswarm/common/schema/event_base.py` |
```
改为：
```
| 10.1.7 | ✅ | HookEventBase | 钩子事件基类 | `jiuwenswarm/common/schema/event_base.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新 IMPLEMENTATION_PLAN.md 10.1.7 状态为 ✅"
```

---

### Task 5: 最终验证 — 全量编译与测试

**Files:** 无新增

- [ ] **Step 1: 全量编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 无错误输出

- [ ] **Step 2: schema 包全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/swarm/schema/`
Expected: 全部 PASS

- [ ] **Step 3: schema 包覆盖率检查**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/schema/`
Expected: 覆盖率 ≥ 85%
