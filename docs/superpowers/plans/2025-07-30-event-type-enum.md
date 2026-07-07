# EventType 枚举实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 EventType 枚举的完整 Go 代码，全量 26 个事件类型，与 Python 一一对应。

**Architecture:** 完全复刻 ReqMethod 模式：`type EventType string` + 26 个 const（按命名空间分组）+ `AllEventTypes()` + `ParseEventType()` + `IsValidEventType()` + `String()`/`GoString()` + `init()` lookup map + 测试覆盖。

**Tech Stack:** Go 1.24+, standard library (fmt, encoding/json, testing)

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/swarm/schema/event_type.go` | EventType 枚举类型定义、常量、查找函数 |
| 创建 | `internal/swarm/schema/event_type_test.go` | EventType 全量测试 |
| 修改 | `internal/swarm/schema/doc.go` | 文件目录增加 event_type.go 条目 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 步骤 10.1.2 状态 ☐→✅ |

---

### Task 1: 创建 event_type.go — EventType 枚举实现

**Files:**
- Create: `internal/swarm/schema/event_type.go`

- [ ] **Step 1: 创建 event_type.go 完整文件**

```go
package schema

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// EventType E2A 协议事件类型枚举。
//
// 定义 AgentServer→Gateway 通信链路中所有合法的事件类型标识，
// 用于 AgentResponse/AgentResponseChunk 的 event_type 字段和 Gateway 消息路由。
// 值为点分字符串格式（如 "chat.delta"），与 Python EventType 枚举值一一对应。
//
// 对应 Python: jiuwenswarm/common/schema/message.py (EventType)
type EventType string

const (
	// ─── 连接 ───

	// EventTypeConnectionAck 连接确认
	EventTypeConnectionAck EventType = "connection.ack"
	// EventTypeHello 握手
	EventTypeHello EventType = "hello"

	// ─── chat 流式 ───

	// EventTypeChatDelta 流式文本增量
	EventTypeChatDelta EventType = "chat.delta"
	// EventTypeChatReasoning 推理过程
	EventTypeChatReasoning EventType = "chat.reasoning"
	// EventTypeChatUsageMetadata 用量元数据
	EventTypeChatUsageMetadata EventType = "chat.usage_metadata"
	// EventTypeChatUsageSummary 用量汇总
	EventTypeChatUsageSummary EventType = "chat.usage_summary"
	// EventTypeChatFinal 最终完整响应
	EventTypeChatFinal EventType = "chat.final"
	// EventTypeChatMedia 媒体内容
	EventTypeChatMedia EventType = "chat.media"
	// EventTypeChatFile 文件内容
	EventTypeChatFile EventType = "chat.file"

	// ─── chat 工具 ───

	// EventTypeChatToolCall 工具调用
	EventTypeChatToolCall EventType = "chat.tool_call"
	// EventTypeChatToolUpdate 工具更新
	EventTypeChatToolUpdate EventType = "chat.tool_update"
	// EventTypeChatToolResult 工具结果
	EventTypeChatToolResult EventType = "chat.tool_result"

	// ─── chat 状态 ───

	// EventTypeChatProcessingStatus 处理状态
	EventTypeChatProcessingStatus EventType = "chat.processing_status"
	// EventTypeChatError 对话错误
	EventTypeChatError EventType = "chat.error"
	// EventTypeChatInterruptResult 中断结果
	EventTypeChatInterruptResult EventType = "chat.interrupt_result"
	// EventTypeChatEvolutionStatus 进化状态
	EventTypeChatEvolutionStatus EventType = "chat.evolution_status"
	// EventTypeChatSubtaskUpdate 子任务更新
	EventTypeChatSubtaskUpdate EventType = "chat.subtask_update"
	// EventTypeChatAskUserQuestion Agent 提问
	EventTypeChatAskUserQuestion EventType = "chat.ask_user_question"
	// EventTypeChatSessionResult 会话结果
	EventTypeChatSessionResult EventType = "chat.session_result"

	// ─── context ───

	// EventTypeContextUsage 上下文用量
	EventTypeContextUsage EventType = "context.usage"

	// ─── todo ───

	// EventTypeTodoUpdated 待办更新
	EventTypeTodoUpdated EventType = "todo.updated"

	// ─── team ───

	// EventTypeTeamMember 团队成员
	EventTypeTeamMember EventType = "team.member"
	// EventTypeTeamTask 团队任务
	EventTypeTeamTask EventType = "team.task"
	// EventTypeTeamMessage 团队消息
	EventTypeTeamMessage EventType = "team.message"

	// ─── heartbeat ───

	// EventTypeHeartbeatRelay 心跳中继
	EventTypeHeartbeatRelay EventType = "heartbeat.relay"

	// ─── history ───

	// EventTypeHistoryGet 历史消息
	EventTypeHistoryGet EventType = "history.message"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// eventTypeLookup 字符串值到 EventType 枚举的查找表，用于 ParseEventType/IsValidEventType 的 O(1) 查找。
var eventTypeLookup map[string]EventType

// ──────────────────────────── 导出函数 ────────────────────────────

// AllEventTypes 返回所有 EventType 枚举值。
// 用于遍历清理等场景。
func AllEventTypes() []EventType {
	return []EventType{
		// 连接
		EventTypeConnectionAck,
		EventTypeHello,
		// chat 流式
		EventTypeChatDelta,
		EventTypeChatReasoning,
		EventTypeChatUsageMetadata,
		EventTypeChatUsageSummary,
		EventTypeChatFinal,
		EventTypeChatMedia,
		EventTypeChatFile,
		// chat 工具
		EventTypeChatToolCall,
		EventTypeChatToolUpdate,
		EventTypeChatToolResult,
		// chat 状态
		EventTypeChatProcessingStatus,
		EventTypeChatError,
		EventTypeChatInterruptResult,
		EventTypeChatEvolutionStatus,
		EventTypeChatSubtaskUpdate,
		EventTypeChatAskUserQuestion,
		EventTypeChatSessionResult,
		// context
		EventTypeContextUsage,
		// todo
		EventTypeTodoUpdated,
		// team
		EventTypeTeamMember,
		EventTypeTeamTask,
		EventTypeTeamMessage,
		// heartbeat
		EventTypeHeartbeatRelay,
		// history
		EventTypeHistoryGet,
	}
}

// ParseEventType 从字符串解析 EventType，不合法返回错误。
// 使用包级查找表实现 O(1) 查找，与 Python EventType 枚举严格对齐。
func ParseEventType(s string) (EventType, error) {
	if et, ok := eventTypeLookup[s]; ok {
		return et, nil
	}
	return EventType(""), fmt.Errorf("不合法的 EventType 值: %q", s)
}

// IsValidEventType 判断字符串是否为合法的 EventType 值。
func IsValidEventType(s string) bool {
	_, ok := eventTypeLookup[s]
	return ok
}

// String 实现 fmt.Stringer 接口。
func (et EventType) String() string {
	return string(et)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (et EventType) GoString() string {
	return fmt.Sprintf("schema.EventType(%q)", string(et))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 构建查找表
	events := AllEventTypes()
	eventTypeLookup = make(map[string]EventType, len(events))
	for _, et := range events {
		eventTypeLookup[string(et)] = et
	}
}
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 无错误

---

### Task 2: 创建 event_type_test.go — 全量测试

**Files:**
- Create: `internal/swarm/schema/event_type_test.go`

- [ ] **Step 1: 创建 event_type_test.go 完整文件**

```go
package schema

import (
	"encoding/json"
	"testing"
)

// TestAllEventTypes 验证 AllEventTypes 返回全部 26 个枚举值
func TestAllEventTypes(t *testing.T) {
	events := AllEventTypes()
	if len(events) != 26 {
		t.Fatalf("AllEventTypes() 返回 %d 个事件，want 26", len(events))
	}

	// 验证无重复
	seen := make(map[EventType]bool)
	for _, et := range events {
		if seen[et] {
			t.Errorf("重复事件: %q", et)
		}
		seen[et] = true
	}

	// 验证包含关键事件
	keyEvents := []EventType{
		EventTypeConnectionAck,
		EventTypeHello,
		EventTypeChatDelta,
		EventTypeChatFinal,
		EventTypeChatError,
		EventTypeChatToolCall,
	}
	for _, ke := range keyEvents {
		if !seen[ke] {
			t.Errorf("缺少关键事件: %q", ke)
		}
	}
}

// TestParseEventType_合法值 验证解析合法值成功
func TestParseEventType_合法值(t *testing.T) {
	tests := []struct {
		input string
		want  EventType
	}{
		{"connection.ack", EventTypeConnectionAck},
		{"hello", EventTypeHello},
		{"chat.delta", EventTypeChatDelta},
		{"chat.final", EventTypeChatFinal},
		{"chat.tool_call", EventTypeChatToolCall},
		{"chat.error", EventTypeChatError},
		{"context.usage", EventTypeContextUsage},
		{"todo.updated", EventTypeTodoUpdated},
		{"team.member", EventTypeTeamMember},
		{"heartbeat.relay", EventTypeHeartbeatRelay},
		{"history.message", EventTypeHistoryGet},
	}
	for _, tt := range tests {
		got, err := ParseEventType(tt.input)
		if err != nil {
			t.Errorf("ParseEventType(%q) 返回错误: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseEventType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestParseEventType_非法值 验证解析非法值返回错误
func TestParseEventType_非法值(t *testing.T) {
	invalidInputs := []string{
		"",
		"unknown.event",
		"chat.",
		".delta",
		"CHAT_DELTA",
		"chat/delta",
		"foo.bar.baz",
	}
	for _, input := range invalidInputs {
		_, err := ParseEventType(input)
		if err == nil {
			t.Errorf("ParseEventType(%q) 应返回错误，但返回 nil", input)
		}
	}
}

// TestIsValidEventType 验证 IsValidEventType 对合法/非法值的判断
func TestIsValidEventType(t *testing.T) {
	// 合法值
	if !IsValidEventType("chat.delta") {
		t.Error(`IsValidEventType("chat.delta") = false, want true`)
	}
	if !IsValidEventType("connection.ack") {
		t.Error(`IsValidEventType("connection.ack") = false, want true`)
	}
	if !IsValidEventType("chat.ask_user_question") {
		t.Error(`IsValidEventType("chat.ask_user_question") = false, want true`)
	}

	// 非法值
	if IsValidEventType("") {
		t.Error(`IsValidEventType("") = true, want false`)
	}
	if IsValidEventType("nonexistent.event") {
		t.Error(`IsValidEventType("nonexistent.event") = true, want false`)
	}
}

// TestEventTypeString 验证 String() 返回原始字符串值
func TestEventTypeString(t *testing.T) {
	if got := EventTypeChatDelta.String(); got != "chat.delta" {
		t.Errorf("EventTypeChatDelta.String() = %q, want %q", got, "chat.delta")
	}
	if got := EventTypeConnectionAck.String(); got != "connection.ack" {
		t.Errorf("EventTypeConnectionAck.String() = %q, want %q", got, "connection.ack")
	}
}

// TestEventTypeGoString 验证 GoString() 格式
func TestEventTypeGoString(t *testing.T) {
	if got := EventTypeChatDelta.GoString(); got != `schema.EventType("chat.delta")` {
		t.Errorf("EventTypeChatDelta.GoString() = %q, want %q", got, `schema.EventType("chat.delta")`)
	}
}

// TestEventTypeJSON序列化往返 验证 JSON marshal/unmarshal 往返一致
func TestEventTypeJSON序列化往返(t *testing.T) {
	events := []EventType{
		EventTypeConnectionAck,
		EventTypeChatDelta,
		EventTypeChatFinal,
		EventTypeChatToolCall,
		EventTypeTeamMember,
	}
	for _, et := range events {
		data, err := json.Marshal(et)
		if err != nil {
			t.Errorf("json.Marshal(%q) 错误: %v", et, err)
			continue
		}
		var got EventType
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("json.Unmarshal(%s) 错误: %v", data, err)
			continue
		}
		if got != et {
			t.Errorf("JSON 往返: got %q, want %q", got, et)
		}
	}
}

// TestEventType常量值与Python对齐 验证全部 26 个常量字符串值与 Python EventType 完全对齐
func TestEventType常量值与Python对齐(t *testing.T) {
	// 对应 Python: jiuwenswarm/common/schema/message.py (EventType)
	tests := []struct {
		got  EventType
		want string
	}{
		{EventTypeConnectionAck, "connection.ack"},
		{EventTypeHello, "hello"},
		{EventTypeChatDelta, "chat.delta"},
		{EventTypeChatReasoning, "chat.reasoning"},
		{EventTypeChatUsageMetadata, "chat.usage_metadata"},
		{EventTypeChatUsageSummary, "chat.usage_summary"},
		{EventTypeChatFinal, "chat.final"},
		{EventTypeChatMedia, "chat.media"},
		{EventTypeChatFile, "chat.file"},
		{EventTypeChatToolCall, "chat.tool_call"},
		{EventTypeChatToolUpdate, "chat.tool_update"},
		{EventTypeChatToolResult, "chat.tool_result"},
		{EventTypeContextUsage, "context.usage"},
		{EventTypeTodoUpdated, "todo.updated"},
		{EventTypeChatProcessingStatus, "chat.processing_status"},
		{EventTypeChatError, "chat.error"},
		{EventTypeChatInterruptResult, "chat.interrupt_result"},
		{EventTypeChatEvolutionStatus, "chat.evolution_status"},
		{EventTypeChatSubtaskUpdate, "chat.subtask_update"},
		{EventTypeChatAskUserQuestion, "chat.ask_user_question"},
		{EventTypeChatSessionResult, "chat.session_result"},
		{EventTypeTeamMember, "team.member"},
		{EventTypeTeamTask, "team.task"},
		{EventTypeTeamMessage, "team.message"},
		{EventTypeHeartbeatRelay, "heartbeat.relay"},
		{EventTypeHistoryGet, "history.message"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("常量值 = %q, want %q", tt.got, tt.want)
		}
	}
	// 验证测试覆盖全部 26 个常量
	if len(tests) != 26 {
		t.Errorf("Python 对齐测试覆盖 %d 个常量，want 26", len(tests))
	}
}
```

- [ ] **Step 2: 运行测试验证全部通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/schema/ -v -run TestEventType -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 运行全包测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/schema/ -count=1`
Expected: 全部 PASS（包括原有 ReqMethod 测试）

---

### Task 3: 更新 doc.go — 文件目录回填

**Files:**
- Modify: `internal/swarm/schema/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录**

将 doc.go 内容修改为：

```go
// Package schema 提供 E2A 协议和 Gateway/AgentServer 通信所需的全部类型定义。
//
// 本包定义了 E2A 协议的核心数据模型，包括 RPC 方法名枚举（ReqMethod）、
// 事件类型枚举（EventType）、运行模式枚举（Mode）、消息模型（Message）、
// Agent 请求/响应模型等，作为 swarm 层的类型基础。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── req_method.go    # ReqMethod 枚举（142 个 RPC 方法名）
//	└── event_type.go    # EventType 枚举（26 个事件类型）
//
// 对应 Python 代码：jiuwenswarm/common/schema/
package schema
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/schema/`
Expected: 无错误

---

### Task 4: 更新 IMPLEMENTATION_PLAN.md — 状态回填

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 找到并更新步骤 10.1.2 状态**

在 IMPLEMENTATION_PLAN.md 中找到步骤 `10.1.2` 对应的行，将状态标记从 `☐` 改为 `✅`。

Run: `cd /home/opensource/uapclaw-gateway && grep -n "10.1.2" IMPLEMENTATION_PLAN.md`
Expected: 找到包含 `☐` 的行

将 `☐` 替换为 `✅`。

- [ ] **Step 2: 验证修改正确**

Run: `cd /home/opensource/uapclaw-gateway && grep "10.1.2" IMPLEMENTATION_PLAN.md`
Expected: 行中包含 `✅`

---

### Task 5: 最终验证 — 全量编译和测试

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 无错误

- [ ] **Step 2: schema 包测试覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/schema/`
Expected: 覆盖率 ≥ 90%

- [ ] **Step 3: 提交代码**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/schema/event_type.go internal/swarm/schema/event_type_test.go internal/swarm/schema/doc.go IMPLEMENTATION_PLAN.md
git commit -m "feat(schema): 实现 EventType 枚举（10.1.2）— 全量 26 个事件类型与 Python 对齐"
```
