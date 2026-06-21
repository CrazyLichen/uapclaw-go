package schema

import (
	"encoding/json"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
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
		Statistic:   iface.ContextStats{TotalMessages: 10},
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
		Statistic:   iface.ContextStats{TotalMessages: 8},
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
