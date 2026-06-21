# 5.19 ContextEvent 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现上下文引擎的事件与状态类型，包含处理器结果 ContextEvent、压缩状态 ContextCompressionState 及辅助类型、上下文回调事件 ContextCallEventType。

**Architecture:** 按 Python 分包映射——ContextEvent 放 processor/base.go（与 Processor 基类同包），ContextCompressionState 放 schema/context_state.go（数据模型层），ContextCallEventType 放 callback/events.go（回调基础设施层）。同时在 CallbackFramework 中追加 OnContext/OffContext/TriggerContext 方法。

**Tech Stack:** Go 1.22+, 标准库 encoding/json, testing

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/context_engine/processor/doc.go` | processor 包文档 |
| 创建 | `internal/agentcore/context_engine/processor/base.go` | ContextEvent 处理器结果类型 |
| 创建 | `internal/agentcore/context_engine/processor/base_test.go` | ContextEvent 测试 |
| 创建 | `internal/agentcore/context_engine/schema/context_state.go` | 压缩状态模型（ContextCompressionState + 辅助类型） |
| 创建 | `internal/agentcore/context_engine/schema/context_state_test.go` | 压缩状态模型测试 |
| 修改 | `internal/agentcore/context_engine/schema/doc.go` | 文件目录新增 context_state.go |
| 修改 | `internal/agentcore/context_engine/doc.go` | 文件目录新增 processor/ 子包 |
| 修改 | `internal/agentcore/runner/callback/events.go` | 追加 ContextCallEventType + ContextCallEventData |
| 修改 | `internal/agentcore/runner/callback/framework.go` | 追加 contextCallbacks + OnContext/OffContext/TriggerContext |
| 修改 | `internal/agentcore/runner/callback/doc.go` | 事件体系说明新增 Context 域 |
| 修改 | `internal/agentcore/runner/callback/events_test.go` | 追加 Context 事件测试 |
| 修改 | `internal/agentcore/runner/callback/framework_test.go` | 追加 Context 回调测试 |

---

### Task 1: 创建 processor 包 — doc.go + ContextEvent

**Files:**
- Create: `internal/agentcore/context_engine/processor/doc.go`
- Create: `internal/agentcore/context_engine/processor/base.go`
- Test: `internal/agentcore/context_engine/processor/base_test.go`

- [ ] **Step 1: 创建 processor 目录**

```bash
mkdir -p internal/agentcore/context_engine/processor
```

- [ ] **Step 2: 创建 processor/doc.go**

```go
// Package processor 提供上下文处理器插件体系。
//
// 处理器在两个生命周期点介入上下文管理：
//  1. OnAddMessages     — 消息即将被添加时
//  2. OnGetContextWindow — 上下文窗口即将返回时
//
// 文件目录：
//
//	processor/
//	├── doc.go    # 包文档
//	└── base.go   # ContextEvent 处理器结果类型
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/
package processor
```

- [ ] **Step 3: 创建 processor/base.go**

```go
package processor

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEvent 上下文处理器执行结果，由各 Processor 的 OnAddMessages / OnGetContextWindow 返回。
//
// 当处理器实际执行了操作时返回非 nil 的 ContextEvent，携带修改了哪些消息索引、
// 压缩摘要和压缩用量信息。Context 实例读取这些字段构建 ContextCompressionState。
// 处理器未触发（noop）时返回 nil。
//
// 对应 Python: openjiuwen/core/context_engine/processor/base.py (ContextEvent)
type ContextEvent struct {
	// EventType 处理器类型标识（如 "DialogueCompressor"、"MessageOffloader"）
	EventType string `json:"event_type"`
	// MessagesToModify 被处理器修改的消息索引列表
	MessagesToModify []int `json:"messages_to_modify"`
	// CompactSummary 压缩摘要文本
	CompactSummary string `json:"compact_summary"`
	// CompressionUsage 压缩调用用量（token 数、费用等）
	CompressionUsage map[string]any `json:"compression_usage,omitempty"`
}
```

- [ ] **Step 4: 创建 processor/base_test.go**

```go
package processor

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextEvent_字段默认值 验证 ContextEvent 零值
func TestContextEvent_字段默认值(t *testing.T) {
	var e ContextEvent
	if e.EventType != "" {
		t.Errorf("EventType 零值应为空串，实际 %q", e.EventType)
	}
	if e.MessagesToModify != nil {
		t.Errorf("MessagesToModify 零值应为 nil，实际 %v", e.MessagesToModify)
	}
	if e.CompactSummary != "" {
		t.Errorf("CompactSummary 零值应为空串，实际 %q", e.CompactSummary)
	}
	if e.CompressionUsage != nil {
		t.Errorf("CompressionUsage 零值应为 nil，实际 %v", e.CompressionUsage)
	}
}

// TestContextEvent_构造 验证结构体字面量构造
func TestContextEvent_构造(t *testing.T) {
	e := &ContextEvent{
		EventType:        "DialogueCompressor",
		MessagesToModify: []int{0, 1, 2},
		CompactSummary:   "压缩了3条消息",
		CompressionUsage: map[string]any{
			"calls":        1,
			"total_tokens": 500,
		},
	}
	if e.EventType != "DialogueCompressor" {
		t.Errorf("EventType = %q, want DialogueCompressor", e.EventType)
	}
	if len(e.MessagesToModify) != 3 {
		t.Errorf("MessagesToModify 长度 = %d, want 3", len(e.MessagesToModify))
	}
	if e.CompactSummary != "压缩了3条消息" {
		t.Errorf("CompactSummary = %q, want 压缩了3条消息", e.CompactSummary)
	}
	if e.CompressionUsage["calls"] != 1 {
		t.Errorf("CompressionUsage[calls] = %v, want 1", e.CompressionUsage["calls"])
	}
}

// TestContextEvent_JSON序列化 验证 JSON 序列化/反序列化
func TestContextEvent_JSON序列化(t *testing.T) {
	original := &ContextEvent{
		EventType:        "MessageOffloader",
		MessagesToModify: []int{5},
		CompactSummary:   "卸载了1条消息",
		CompressionUsage: map[string]any{"input_tokens": float64(100)},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored ContextEvent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.EventType != original.EventType {
		t.Errorf("EventType = %q, want %q", restored.EventType, original.EventType)
	}
	if len(restored.MessagesToModify) != 1 || restored.MessagesToModify[0] != 5 {
		t.Errorf("MessagesToModify = %v, want [5]", restored.MessagesToModify)
	}
	if restored.CompactSummary != original.CompactSummary {
		t.Errorf("CompactSummary = %q, want %q", restored.CompactSummary, original.CompactSummary)
	}
}

// TestContextEvent_JSON省略空字段 验证 omitempty 行为
func TestContextEvent_JSON省略空字段(t *testing.T) {
	e := ContextEvent{EventType: "test"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)
	// CompressionUsage 为 nil 时应省略
	if _, ok := m["compression_usage"]; ok {
		t.Error("compression_usage 应被 omitempty 省略")
	}
	// EventType 应保留
	if _, ok := m["event_type"]; !ok {
		t.Error("event_type 不应被省略")
	}
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/... -v
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_engine/processor/
git commit -m "feat(context_engine): 创建 processor 包，实现 ContextEvent 处理器结果类型"
```

---

### Task 2: 创建 ContextCompressionState 及辅助类型

**Files:**
- Create: `internal/agentcore/context_engine/schema/context_state.go`
- Test: `internal/agentcore/context_engine/schema/context_state_test.go`

- [ ] **Step 1: 创建 schema/context_state.go**

```go
package schema

import (
	contextengine "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextCompressionMetric 上下文压缩前后指标快照。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionMetric)
type ContextCompressionMetric struct {
	// Time 观测时间（ISO 8601 毫秒精度），空串表示未记录
	Time string `json:"time,omitempty"`
	// Messages 消息数量
	Messages int `json:"messages"`
	// Tokens Token 数量
	Tokens int `json:"tokens"`
	// ContextPercent 上下文使用百分比（0-100），0 且 omitempty 省略表示无上限
	ContextPercent int `json:"context_percent,omitempty"`
}

// ContextCompressionSaved 上下文压缩节省量。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionSaved)
type ContextCompressionSaved struct {
	// Messages 节省的消息数
	Messages int `json:"messages"`
	// Tokens 节省的 Token 数
	Tokens int `json:"tokens"`
	// Percent 节省百分比
	Percent float64 `json:"percent"`
}

// ContextCompressionUsage 上下文压缩 LLM 调用用量。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionUsage)
type ContextCompressionUsage struct {
	// Calls LLM 调用次数
	Calls int `json:"calls"`
	// InputTokens 输入 Token 数
	InputTokens int `json:"input_tokens"`
	// OutputTokens 输出 Token 数
	OutputTokens int `json:"output_tokens"`
	// TotalTokens 总 Token 数
	TotalTokens int `json:"total_tokens"`
	// CacheTokens 缓存 Token 数
	CacheTokens int `json:"cache_tokens"`
	// InputCost 输入费用
	InputCost float64 `json:"input_cost"`
	// OutputCost 输出费用
	OutputCost float64 `json:"output_cost"`
	// TotalCost 总费用
	TotalCost float64 `json:"total_cost"`
	// ModelName 模型名称
	ModelName string `json:"model_name"`
	// Details 每次 LLM 调用的原始用量详情
	Details []map[string]any `json:"details,omitempty"`
}

// ContextCompressionState 上下文压缩状态完整快照。
//
// 由 ProcessorStateRecorder.BuildState() 构建，记录一次压缩操作的完整生命周期。
// 通过回调框架和 session stream 发射，供外部系统观测上下文引擎行为。
//
// 对应 Python: openjiuwen/core/context_engine/schema/context_state.py (ContextCompressionState)
type ContextCompressionState struct {
	// Type 事件类型标识，固定为 ContextCompressionStateType
	Type string `json:"type"`
	// OperationID 操作唯一标识
	OperationID string `json:"operation_id"`
	// Status 操作状态
	Status CompressionStatus `json:"status"`
	// Phase 操作阶段
	Phase CompressionPhase `json:"phase"`
	// Processor 处理器类型名称
	Processor string `json:"processor"`
	// Model 使用的 LLM 模型名称
	Model string `json:"model"`
	// Before 压缩前指标
	Before ContextCompressionMetric `json:"before"`
	// After 压缩后指标，nil 表示操作未完成或被跳过
	After *ContextCompressionMetric `json:"after,omitempty"`
	// Statistic 上下文统计快照
	Statistic contextengine.ContextStats `json:"statistic"`
	// Saved 压缩节省量，nil 表示无节省（操作未完成）
	Saved *ContextCompressionSaved `json:"saved,omitempty"`
	// CompressionUsage LLM 调用用量，nil 表示未调用 LLM
	CompressionUsage *ContextCompressionUsage `json:"compression_usage,omitempty"`
	// DurationMs 操作耗时（毫秒），0 且 omitempty 省略表示未完成
	DurationMs int `json:"duration_ms,omitempty"`
	// ContextMax 上下文窗口 Token 上限，0 且 omitempty 省略表示无上限
	ContextMax int `json:"context_max,omitempty"`
	// Summary 人类可读的操作摘要
	Summary string `json:"summary"`
	// CompactSummary 紧凑摘要（供流式输出）
	CompactSummary string `json:"compact_summary"`
	// Error 错误信息，空串表示无错误
	Error string `json:"error,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// CompressionStatus 压缩操作状态字面量类型。
//
// 对应 Python: Literal["started", "completed", "noop", "skipped", "failed"]
type CompressionStatus string

const (
	// CompressionStarted 压缩操作已启动
	CompressionStarted CompressionStatus = "started"
	// CompressionCompleted 压缩操作已完成
	CompressionCompleted CompressionStatus = "completed"
	// CompressionNoop 压缩操作无变更
	CompressionNoop CompressionStatus = "noop"
	// CompressionSkipped 压缩操作已跳过
	CompressionSkipped CompressionStatus = "skipped"
	// CompressionFailed 压缩操作已失败
	CompressionFailed CompressionStatus = "failed"
)

// CompressionPhase 压缩操作阶段字面量类型。
//
// 对应 Python: Literal["add_messages", "get_context_window", "active_compress"]
type CompressionPhase string

const (
	// PhaseAddMessages 添加消息阶段
	PhaseAddMessages CompressionPhase = "add_messages"
	// PhaseGetContextWindow 获取上下文窗口阶段
	PhaseGetContextWindow CompressionPhase = "get_context_window"
	// PhaseActiveCompress 主动压缩阶段
	PhaseActiveCompress CompressionPhase = "active_compress"
)

// ──────────────────────────── 常量 ────────────────────────────

// ContextCompressionStateType 压缩状态事件类型标识。
// 用于回调事件名和 session stream 的 OutputSchema.Type 字段。
//
// 对应 Python: CONTEXT_COMPRESSION_STATE_TYPE = "context.compression_state"
const ContextCompressionStateType = "context.compression_state"
```

- [ ] **Step 2: 创建 schema/context_state_test.go**

```go
package schema

import (
	"encoding/json"
	"testing"

	contextengine "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextCompressionMetric_字段默认值 验证零值
func TestContextCompressionMetric_字段默认值(t *testing.T) {
	var m ContextCompressionMetric
	if m.Time != "" {
		t.Errorf("Time 零值应为空串，实际 %q", m.Time)
	}
	if m.Messages != 0 {
		t.Errorf("Messages 零值应为 0，实际 %d", m.Messages)
	}
	if m.Tokens != 0 {
		t.Errorf("Tokens 零值应为 0，实际 %d", m.Tokens)
	}
	if m.ContextPercent != 0 {
		t.Errorf("ContextPercent 零值应为 0，实际 %d", m.ContextPercent)
	}
}

// TestContextCompressionMetric_JSON省略空字段 验证 omitempty
func TestContextCompressionMetric_JSON省略空字段(t *testing.T) {
	m := ContextCompressionMetric{Messages: 10, Tokens: 500}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	if _, ok := raw["time"]; ok {
		t.Error("空 Time 应被 omitempty 省略")
	}
	if _, ok := raw["context_percent"]; ok {
		t.Error("零值 ContextPercent 应被 omitempty 省略")
	}
	if _, ok := raw["messages"]; !ok {
		t.Error("非零 Messages 不应被省略")
	}
}

// TestContextCompressionSaved_构造 验证结构体构造
func TestContextCompressionSaved_构造(t *testing.T) {
	s := ContextCompressionSaved{
		Messages: 5,
		Tokens:   200,
		Percent:  33.3,
	}
	if s.Messages != 5 {
		t.Errorf("Messages = %d, want 5", s.Messages)
	}
	if s.Tokens != 200 {
		t.Errorf("Tokens = %d, want 200", s.Tokens)
	}
	if s.Percent != 33.3 {
		t.Errorf("Percent = %f, want 33.3", s.Percent)
	}
}

// TestContextCompressionUsage_构造 验证完整字段构造
func TestContextCompressionUsage_构造(t *testing.T) {
	u := ContextCompressionUsage{
		Calls:        2,
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		CacheTokens:  30,
		InputCost:    0.01,
		OutputCost:   0.02,
		TotalCost:    0.03,
		ModelName:    "qwen-max",
		Details:      []map[string]any{{"input_tokens": float64(100)}},
	}
	if u.Calls != 2 {
		t.Errorf("Calls = %d, want 2", u.Calls)
	}
	if u.ModelName != "qwen-max" {
		t.Errorf("ModelName = %q, want qwen-max", u.ModelName)
	}
	if len(u.Details) != 1 {
		t.Errorf("Details 长度 = %d, want 1", len(u.Details))
	}
}

// TestContextCompressionUsage_Details省略 验证 omitempty
func TestContextCompressionUsage_Details省略(t *testing.T) {
	u := ContextCompressionUsage{Calls: 1}
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	if _, ok := raw["details"]; ok {
		t.Error("空 Details 应被 omitempty 省略")
	}
}

// TestCompressionStatus_字符串值 验证各常量值
func TestCompressionStatus_字符串值(t *testing.T) {
	tests := []struct {
		status CompressionStatus
		want   string
	}{
		{CompressionStarted, "started"},
		{CompressionCompleted, "completed"},
		{CompressionNoop, "noop"},
		{CompressionSkipped, "skipped"},
		{CompressionFailed, "failed"},
	}
	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("CompressionStatus = %q, want %q", tt.status, tt.want)
		}
	}
}

// TestCompressionPhase_字符串值 验证各常量值
func TestCompressionPhase_字符串值(t *testing.T) {
	tests := []struct {
		phase CompressionPhase
		want  string
	}{
		{PhaseAddMessages, "add_messages"},
		{PhaseGetContextWindow, "get_context_window"},
		{PhaseActiveCompress, "active_compress"},
	}
	for _, tt := range tests {
		if string(tt.phase) != tt.want {
			t.Errorf("CompressionPhase = %q, want %q", tt.phase, tt.want)
		}
	}
}

// TestContextCompressionState_完整构造 验证完整状态构造
func TestContextCompressionState_完整构造(t *testing.T) {
	state := ContextCompressionState{
		Type:        ContextCompressionStateType,
		OperationID: "op-001",
		Status:      CompressionCompleted,
		Phase:       PhaseGetContextWindow,
		Processor:   "DialogueCompressor",
		Model:       "qwen-max",
		Before:      ContextCompressionMetric{Messages: 20, Tokens: 5000},
		After:       &ContextCompressionMetric{Messages: 10, Tokens: 2000},
		Statistic:   contextengine.ContextStats{TotalMessages: 10},
		Saved:       &ContextCompressionSaved{Messages: 10, Tokens: 3000, Percent: 60.0},
		CompressionUsage: &ContextCompressionUsage{
			Calls:       1,
			TotalTokens: 500,
			ModelName:   "qwen-max",
		},
		DurationMs:     150,
		ContextMax:     8192,
		Summary:        "Compressed 20 -> 10 messages, ~5k -> ~2k tokens",
		CompactSummary: "压缩了10条消息",
	}
	if state.Type != "context.compression_state" {
		t.Errorf("Type = %q, want context.compression_state", state.Type)
	}
	if state.Status != CompressionCompleted {
		t.Errorf("Status = %q, want completed", state.Status)
	}
	if state.After == nil {
		t.Error("After 不应为 nil")
	}
	if state.Saved == nil {
		t.Error("Saved 不应为 nil")
	}
	if state.Statistic.TotalMessages != 10 {
		t.Errorf("Statistic.TotalMessages = %d, want 10", state.Statistic.TotalMessages)
	}
}

// TestContextCompressionState_最小构造 验证仅必填字段的构造
func TestContextCompressionState_最小构造(t *testing.T) {
	state := ContextCompressionState{
		Type:        ContextCompressionStateType,
		OperationID: "op-002",
		Status:      CompressionStarted,
		Phase:       PhaseAddMessages,
		Before:      ContextCompressionMetric{Messages: 5, Tokens: 1000},
	}
	if state.After != nil {
		t.Error("After 应为 nil")
	}
	if state.Saved != nil {
		t.Error("Saved 应为 nil")
	}
	if state.CompressionUsage != nil {
		t.Error("CompressionUsage 应为 nil")
	}
	if state.DurationMs != 0 {
		t.Errorf("DurationMs 应为 0，实际 %d", state.DurationMs)
	}
	if state.Error != "" {
		t.Errorf("Error 应为空串，实际 %q", state.Error)
	}
}

// TestContextCompressionState_JSON序列化 验证完整序列化/反序列化
func TestContextCompressionState_JSON序列化(t *testing.T) {
	original := ContextCompressionState{
		Type:        ContextCompressionStateType,
		OperationID: "op-003",
		Status:      CompressionCompleted,
		Phase:       PhaseActiveCompress,
		Processor:   "MicroCompactProcessor",
		Before:      ContextCompressionMetric{Messages: 15, Tokens: 3000},
		After:       &ContextCompressionMetric{Messages: 8, Tokens: 1500, ContextPercent: 18},
		Statistic:   contextengine.ContextStats{TotalMessages: 8},
		Saved:       &ContextCompressionSaved{Messages: 7, Tokens: 1500, Percent: 50.0},
		DurationMs:  200,
		ContextMax:  8192,
		Summary:     "Compressed 15 -> 8 messages",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored ContextCompressionState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.OperationID != original.OperationID {
		t.Errorf("OperationID = %q, want %q", restored.OperationID, original.OperationID)
	}
	if restored.Status != original.Status {
		t.Errorf("Status = %q, want %q", restored.Status, original.Status)
	}
	if restored.After == nil {
		t.Error("反序列化后 After 不应为 nil")
	}
	if restored.After.ContextPercent != 18 {
		t.Errorf("After.ContextPercent = %d, want 18", restored.After.ContextPercent)
	}
}

// TestContextCompressionState_JSON省略可选字段 验证 nil/零值字段被省略
func TestContextCompressionState_JSON省略可选字段(t *testing.T) {
	state := ContextCompressionState{
		Type:        ContextCompressionStateType,
		OperationID: "op-004",
		Status:      CompressionStarted,
		Phase:       PhaseAddMessages,
		Before:      ContextCompressionMetric{Messages: 5},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	// 这些字段应为 nil/零值，被 omitempty 省略
	if _, ok := raw["after"]; ok {
		t.Error("nil After 应被 omitempty 省略")
	}
	if _, ok := raw["saved"]; ok {
		t.Error("nil Saved 应被 omitempty 省略")
	}
	if _, ok := raw["compression_usage"]; ok {
		t.Error("nil CompressionUsage 应被 omitempty 省略")
	}
	if _, ok := raw["duration_ms"]; ok {
		t.Error("零值 DurationMs 应被 omitempty 省略")
	}
	if _, ok := raw["context_max"]; ok {
		t.Error("零值 ContextMax 应被 omitempty 省略")
	}
	if _, ok := raw["error"]; ok {
		t.Error("空串 Error 应被 omitempty 省略")
	}
	// 这些字段应保留
	for _, key := range []string{"type", "operation_id", "status", "phase", "before", "summary"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("%q 不应被省略", key)
		}
	}
}

// TestContextCompressionState_错误状态 验证失败场景
func TestContextCompressionState_错误状态(t *testing.T) {
	errMsg := "token counter failed"
	state := ContextCompressionState{
		Type:        ContextCompressionStateType,
		OperationID: "op-005",
		Status:      CompressionFailed,
		Phase:       PhaseGetContextWindow,
		Processor:   "DialogueCompressor",
		Before:      ContextCompressionMetric{Messages: 10, Tokens: 2000},
		Error:       errMsg,
	}
	if state.Error != errMsg {
		t.Errorf("Error = %q, want %q", state.Error, errMsg)
	}
	if state.Status != CompressionFailed {
		t.Errorf("Status = %q, want failed", state.Status)
	}
}

// TestContextCompressionStateType_常量值 验证常量值与 Python 对齐
func TestContextCompressionStateType_常量值(t *testing.T) {
	if ContextCompressionStateType != "context.compression_state" {
		t.Errorf("ContextCompressionStateType = %q, want context.compression_state", ContextCompressionStateType)
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/schema/... -v
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/schema/context_state.go internal/agentcore/context_engine/schema/context_state_test.go
git commit -m "feat(context_engine): 实现 ContextCompressionState 及辅助类型（压缩状态模型）"
```

---

### Task 3: 回填 doc.go — schema/doc.go + context_engine/doc.go

**Files:**
- Modify: `internal/agentcore/context_engine/schema/doc.go`
- Modify: `internal/agentcore/context_engine/doc.go`

- [ ] **Step 1: 更新 schema/doc.go 文件目录**

在文件目录树中新增 `context_state.go` 条目：

旧内容（文件目录部分）：
```
//	schema/
//	├── doc.go       # 包文档
//	├── config.go    # ContextEngineConfig 上下文引擎配置
//	└── offload.go   # Offload 消息模型（OffloadInfo + Offload 子类型 + Offloadable 接口 + 反序列化工厂）
```

新内容：
```
//	schema/
//	├── doc.go              # 包文档
//	├── config.go           # ContextEngineConfig 上下文引擎配置
//	├── context_state.go    # 压缩状态模型（ContextCompressionState + 辅助类型 + CompressionStatus/Phase）
//	└── offload.go          # Offload 消息模型（OffloadInfo + Offload 子类型 + Offloadable 接口 + 反序列化工厂）
```

- [ ] **Step 2: 更新 context_engine/doc.go 文件目录**

在文件目录树中新增 `processor/` 子包条目。先读取当前文件内容确认结构，然后添加：

新增内容（追加到文件目录末尾）：
```
//	processor/             # 上下文处理器插件（5.19 创建，5.21 回填 Processor 基类）
```

- [ ] **Step 3: 运行编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/context_engine/...
```

Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/schema/doc.go internal/agentcore/context_engine/doc.go
git commit -m "docs(context_engine): 更新 doc.go 文件目录，新增 processor/ 和 context_state.go"
```

---

### Task 4: 追加 ContextCallEventType + EventData 到 callback 包

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go`
- Modify: `internal/agentcore/runner/callback/events_test.go`

- [ ] **Step 1: 在 events.go 结构体区块追加 ContextCallEventData**

在 `SessionCallEventData` 结构体之后追加：

```go
// ContextCallEventData 上下文调用事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ContextEvents) +
//              openjiuwen/core/runner/callback/framework.py (trigger kwargs)
type ContextCallEventData struct {
	// Event 事件类型
	Event ContextCallEventType
	// SessionID 会话标识
	SessionID string
	// ContextID 上下文标识
	ContextID string
	// State 压缩状态（仅 ContextCompressionStateEvent 事件有值，实际类型 *schema.ContextCompressionState）
	State any
	// Context 上下文实例引用（实际类型 context_engine.ModelContext）
	Context any
	// Extra 额外数据
	Extra map[string]any
}
```

- [ ] **Step 2: 在 events.go 枚举区块追加 ContextCallEventType**

在 `SessionCallEventType` 枚举和常量之后追加：

```go
// ContextCallEventType 上下文调用事件类型。
//
// 事件名格式 "_framework:{event_name}"，与 Python EventBase.get_event() 构建规则一致。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ContextEvents)
type ContextCallEventType string

const (
	// ContextUpdated 上下文更新事件（add_messages 后触发）
	ContextUpdated ContextCallEventType = "_framework:context_updated"
	// ContextOffloaded 上下文卸载事件（offload 后触发）
	ContextOffloaded ContextCallEventType = "_framework:context_offloaded"
	// ContextRetrieved 上下文检索事件（get_context_window 后触发）
	ContextRetrieved ContextCallEventType = "_framework:context_retrieved"
	// ContextCleared 上下文清空事件（clear 后触发）
	ContextCleared ContextCallEventType = "_framework:context_cleared"
	// ContextCompressionStateEvent 压缩状态事件（处理器执行后触发）
	ContextCompressionStateEvent ContextCallEventType = "_framework:context.compression_state"
)
```

- [ ] **Step 3: 在 events.go 导出函数区块追加 String 方法**

```go
// String 实现 fmt.Stringer 接口。
func (t ContextCallEventType) String() string {
	return string(t)
}

// String 实现 fmt.Stringer 接口，返回事件数据的简洁描述。
func (d *ContextCallEventData) String() string {
	if d == nil {
		return "nil"
	}
	return fmt.Sprintf("ContextCallEventData{事件:%s, 会话ID:%s, 上下文ID:%s}", d.Event, d.SessionID, d.ContextID)
}
```

- [ ] **Step 4: 在 events_test.go 追加 Context 事件测试**

```go
// TestContextCallEventType_字符串值 测试 Context 事件类型字符串值
func TestContextCallEventType_字符串值(t *testing.T) {
	tests := []struct {
		event ContextCallEventType
		want  string
	}{
		{ContextUpdated, "_framework:context_updated"},
		{ContextOffloaded, "_framework:context_offloaded"},
		{ContextRetrieved, "_framework:context_retrieved"},
		{ContextCleared, "_framework:context_cleared"},
		{ContextCompressionStateEvent, "_framework:context.compression_state"},
	}
	for _, tt := range tests {
		if string(tt.event) != tt.want {
			t.Errorf("ContextCallEventType = %q, want %q", tt.event, tt.want)
		}
	}
}

// TestContextCallEventType_String 测试 String 方法
func TestContextCallEventType_String(t *testing.T) {
	if ContextUpdated.String() != "_framework:context_updated" {
		t.Errorf("String() = %q, want _framework:context_updated", ContextUpdated.String())
	}
}

// TestContextCallEventData_String 测试 String 方法
func TestContextCallEventData_String(t *testing.T) {
	data := &ContextCallEventData{
		Event:     ContextCleared,
		SessionID: "sess-001",
		ContextID: "ctx-001",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestContextCallEventData_NilString 测试 nil String
func TestContextCallEventData_NilString(t *testing.T) {
	var d *ContextCallEventData
	if d.String() != "nil" {
		t.Errorf("nil String() = %q, want nil", d.String())
	}
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/... -v
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/runner/callback/events.go internal/agentcore/runner/callback/events_test.go
git commit -m "feat(callback): 追加 ContextCallEventType + ContextCallEventData 上下文回调事件类型"
```

---

### Task 5: 扩展 CallbackFramework — OnContext/OffContext/TriggerContext

**Files:**
- Modify: `internal/agentcore/runner/callback/framework.go`
- Modify: `internal/agentcore/runner/callback/framework_test.go`

- [ ] **Step 1: 在 framework.go 追加 ContextCallbackFunc 类型**

在 `CustomCallbackFunc` 类型定义之后追加：

```go
// ContextCallbackFunc 上下文事件回调函数类型。
type ContextCallbackFunc func(ctx context.Context, data *ContextCallEventData) any
```

- [ ] **Step 2: 在 CallbackFramework 结构体追加 contextCallbacks 字段**

在 `customCallbacks` 字段之后追加：

```go
	// contextCallbacks 上下文事件回调函数注册表
	contextCallbacks map[ContextCallEventType][]ContextCallbackFunc
```

- [ ] **Step 3: 在 NewCallbackFramework 初始化 contextCallbacks**

在 `customCallbacks` 初始化之后追加：

```go
		contextCallbacks:   make(map[ContextCallEventType][]ContextCallbackFunc),
```

- [ ] **Step 4: 在 framework.go 导出函数区块追加三个方法**

在 `TriggerCustom` 方法之后追加：

```go
// OnContext 注册上下文事件回调函数。
//
// 同一事件可注册多个回调，按注册顺序执行。
//
// 对应 Python: AsyncCallbackFramework.on(event, callback)
func (fw *CallbackFramework) OnContext(event ContextCallEventType, fn ContextCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.contextCallbacks[event] = append(fw.contextCallbacks[event], fn)
}

// OffContext 注销上下文事件回调函数。
//
// 移除指定事件中与 fn 匹配的回调（按指针匹配）。
// 若事件下无匹配回调，不做任何操作。
//
// 对应 Python: AsyncCallbackFramework.unregister(event, callback)
func (fw *CallbackFramework) OffContext(event ContextCallEventType, fn ContextCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.contextCallbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.contextCallbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// TriggerContext 触发上下文事件，按注册顺序调用所有回调，返回所有回调结果。
//
// 若 ctx 为 nil 或 data 为 nil，直接返回 nil。
//
// 对应 Python: AsyncCallbackFramework.trigger(event, **kwargs) → List[Any]
func (fw *CallbackFramework) TriggerContext(ctx context.Context, data *ContextCallEventData) []any {
	if ctx == nil || data == nil {
		return nil
	}

	fw.mu.RLock()
	callbacks := fw.contextCallbacks[data.Event]
	fw.mu.RUnlock()

	results := make([]any, 0, len(callbacks))
	for _, fn := range callbacks {
		result := fn(ctx, data)
		results = append(results, result)
	}
	return results
}
```

- [ ] **Step 5: 在 framework_test.go 追加 Context 回调测试**

```go
// TestCallbackFramework_OnContext和TriggerContext 测试 Context 回调注册与触发
func TestCallbackFramework_OnContext和TriggerContext(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnContext(ContextUpdated, func(_ context.Context, data *ContextCallEventData) any {
		if data.SessionID != "sess-001" {
			t.Errorf("SessionID = %q, want sess-001", data.SessionID)
		}
		atomic.AddInt32(&called, 1)
		return nil
	})

	fw.TriggerContext(context.Background(), &ContextCallEventData{
		Event:     ContextUpdated,
		SessionID: "sess-001",
		ContextID: "ctx-001",
	})

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("called = %d, want 1", called)
	}
}

// TestCallbackFramework_OffContext 测试注销 Context 回调
func TestCallbackFramework_OffContext(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *ContextCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	}

	fw.OnContext(ContextCleared, fn)
	fw.OffContext(ContextCleared, fn)

	fw.TriggerContext(context.Background(), &ContextCallEventData{
		Event: ContextCleared,
	})

	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("OffContext 后不应触发，called = %d", called)
	}
}

// TestCallbackFramework_TriggerContext_Nil上下文 测试 nil context 防御
func TestCallbackFramework_TriggerContext_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32
	fw.OnContext(ContextRetrieved, func(_ context.Context, _ *ContextCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	})
	fw.TriggerContext(nil, &ContextCallEventData{Event: ContextRetrieved}) //nolint:staticcheck // SA1012: 专门测试 nil context 的防御逻辑
	if atomic.LoadInt32(&called) != 0 {
		t.Error("nil context 不应触发回调")
	}
}

// TestCallbackFramework_TriggerContext_NilData 测试 nil data 防御
func TestCallbackFramework_TriggerContext_NilData(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32
	fw.OnContext(ContextRetrieved, func(_ context.Context, _ *ContextCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	})
	fw.TriggerContext(context.Background(), nil)
	if atomic.LoadInt32(&called) != 0 {
		t.Error("nil data 不应触发回调")
	}
}
```

- [ ] **Step 6: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/callback/... -v
```

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/runner/callback/framework.go internal/agentcore/runner/callback/framework_test.go
git commit -m "feat(callback): 扩展 CallbackFramework，追加 OnContext/OffContext/TriggerContext 方法"
```

---

### Task 6: 更新 callback/doc.go

**Files:**
- Modify: `internal/agentcore/runner/callback/doc.go`

- [ ] **Step 1: 更新事件体系说明**

在 `SessionCallEventType` 行之后追加：

```
//	ContextCallEventType  — Context 生命周期事件（5 种），预定义枚举事件名
```

- [ ] **Step 2: 运行编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/callback/...
```

Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/runner/callback/doc.go
git commit -m "docs(callback): 更新 doc.go 事件体系说明，新增 Context 域"
```

---

### Task 7: 全量编译和覆盖率验证

**Files:**
- 无新文件

- [ ] **Step 1: 检查残留编译进程**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true
```

- [ ] **Step 2: 全量编译**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

Expected: 编译成功

- [ ] **Step 3: 运行全部相关测试并检查覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/context_engine/... ./internal/agentcore/runner/callback/...
```

Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md 5.19 状态**

将 `| 5.19 | ☐ | ContextEvent | 上下文事件 |` 改为 `| 5.19 | ✅ | ContextEvent | ✅ ContextEvent 处理器结果类型 + ContextCompressionState 压缩状态模型（辅助类型 4 个 + 枚举 2 个）+ ContextCallEventType 回调事件（5 种）+ CallbackFramework OnContext/OffContext/TriggerContext |`

- [ ] **Step 5: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 5.19 状态为已完成"
```
