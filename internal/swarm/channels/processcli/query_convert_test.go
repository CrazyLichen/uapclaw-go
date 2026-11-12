package processcli

import (
	"errors"
	"reflect"
	"testing"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── 目录序列化 ────────────────────────────

// TestModelDescriptorToDict_八字段 逐字段对齐 model_catalog.py:43-54。
func TestModelDescriptorToDict_八字段(t *testing.T) {
	got := modelDescriptorToDict(swruntime.RuntimeModelDescriptor{
		SelectionKey:   "gpt-4#0",
		DisplayName:    "gpt-4",
		ModelName:      "gpt-4",
		Provider:       "OpenAI",
		ReasoningLevel: "high",
		IsDefault:      true,
		IsAgentOS:      false,
		IsCurrent:      true,
	})
	want := map[string]any{
		"selection_key":   "gpt-4#0",
		"display_name":    "gpt-4",
		"model_name":      "gpt-4",
		"provider":        "OpenAI",
		"reasoning_level": "high",
		"is_default":      true,
		"is_agentos":      false,
		"is_current":      true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

// TestModelCatalogToDict_三项 模型列表 + 当前选择 + 当前显示名。
func TestModelCatalogToDict_三项(t *testing.T) {
	got := modelCatalogToDict(swruntime.ModelCatalogResult{
		Models: []swruntime.RuntimeModelDescriptor{
			{SelectionKey: "a#0", ModelName: "a", IsCurrent: true},
			{SelectionKey: "b#1", ModelName: "b"},
		},
		CurrentSelection:   "a#0",
		CurrentDisplayName: "a",
	})
	if got["current_selection"] != "a#0" {
		t.Errorf("current_selection = %v", got["current_selection"])
	}
	if got["current_display_name"] != "a" {
		t.Errorf("current_display_name = %v", got["current_display_name"])
	}
	models, ok := got["models"].([]any)
	if !ok || len(models) != 2 {
		t.Fatalf("models = %v", got["models"])
	}
}

// TestModelCatalogToDict_空目录 空切片而非 nil。
func TestModelCatalogToDict_空目录(t *testing.T) {
	got := modelCatalogToDict(swruntime.ModelCatalogResult{})
	models, ok := got["models"].([]any)
	if !ok {
		t.Fatalf("models 类型 = %T, 期望 []any", got["models"])
	}
	if len(models) != 0 {
		t.Errorf("空目录应为空切片，得 %v", models)
	}
}

// TestModeCatalogToDict_一项 模式列表序列化。
func TestModeCatalogToDict_一项(t *testing.T) {
	got := modeCatalogToDict(swruntime.ModeCatalogResult{
		Modes: []swruntime.RuntimeModeDescriptor{
			{Mode: "agent.code.normal", WorkMode: "code"},
		},
	})
	modes, ok := got["modes"].([]any)
	if !ok || len(modes) != 1 {
		t.Fatalf("modes = %v", got["modes"])
	}
}

// TestModeDescriptorToDict_含IsPlan 关键字段存在。
func TestModeDescriptorToDict_含IsPlan(t *testing.T) {
	got := modeDescriptorToDict(swruntime.RuntimeModeDescriptor{
		Mode: "agent.code.plan", WorkMode: "code", IsPlan: true,
	})
	if got["mode"] != "agent.code.plan" {
		t.Errorf("mode = %v", got["mode"])
	}
	if got["is_plan"] != true {
		t.Errorf("is_plan = %v", got["is_plan"])
	}
}

// ──────────────────────────── 会话序列化 ────────────────────────────

// TestSessionSummaryToDict_十一字段 逐字段对齐 session_catalog.py:64-76。
func TestSessionSummaryToDict_十一字段(t *testing.T) {
	got := sessionSummaryToDict(&swruntime.SessionSummary{
		SessionID: "s-1", ChannelID: "process_cli", Title: "t",
		Mode: "agent.code.normal", WorkMode: "code",
		ProjectID: "p", ProjectDir: "/p", Model: "gpt-4",
		CreatedAt: 1.5, LastMessageAt: 2.5, MessageCount: 3,
	})
	want := map[string]any{
		"session_id":      "s-1",
		"channel_id":      "process_cli",
		"title":           "t",
		"mode":            "agent.code.normal",
		"work_mode":       "code",
		"project_id":      "p",
		"project_dir":     "/p",
		"model":           "gpt-4",
		"created_at":      1.5,
		"last_message_at": 2.5,
		"message_count":   3,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

// TestSessionSummaryToDict_nil返回nil nil 摘要返回 nil（对齐 Python None）。
func TestSessionSummaryToDict_nil返回nil(t *testing.T) {
	if got := sessionSummaryToDict(nil); got != nil {
		t.Errorf("nil 摘要应返回 nil，得 %v", got)
	}
}

// TestSessionListToDict_分页四字段 会话数组 + total/limit/offset。
func TestSessionListToDict_分页四字段(t *testing.T) {
	got := sessionListToDict(swruntime.SessionListResult{
		Sessions: []swruntime.SessionSummary{{SessionID: "s-1"}, {SessionID: "s-2"}},
		Total:    10,
		Limit:    2,
		Offset:   4,
	})
	sessions, ok := got["sessions"].([]any)
	if !ok || len(sessions) != 2 {
		t.Fatalf("sessions = %v", got["sessions"])
	}
	if got["total"] != 10 || got["limit"] != 2 || got["offset"] != 4 {
		t.Errorf("分页字段 = %v/%v/%v", got["total"], got["limit"], got["offset"])
	}
}

// TestSessionListToDict_空列表 空切片而非 nil。
func TestSessionListToDict_空列表(t *testing.T) {
	got := sessionListToDict(swruntime.SessionListResult{})
	sessions, ok := got["sessions"].([]any)
	if !ok {
		t.Fatalf("sessions 类型 = %T", got["sessions"])
	}
	if len(sessions) != 0 {
		t.Errorf("空列表应为空切片，得 %v", sessions)
	}
}

// ──────────────────────────── 参数读取助手 ────────────────────────────

// TestStringParam 缺失/非字符串返回空串。
func TestStringParam(t *testing.T) {
	params := map[string]any{"a": "x", "b": 1, "c": ""}
	if got := stringParam(params, "a"); got != "x" {
		t.Errorf("a = %q", got)
	}
	if got := stringParam(params, "b"); got != "" {
		t.Errorf("非字符串应返回空串，得 %q", got)
	}
	if got := stringParam(params, "missing"); got != "" {
		t.Errorf("缺失应返回空串，得 %q", got)
	}
	if got := stringParam(nil, "a"); got != "" {
		t.Errorf("nil params 应返回空串，得 %q", got)
	}
}

// TestIntParam 缺失返回 0，浮点截断。
func TestIntParam(t *testing.T) {
	params := map[string]any{"i": 3, "f": 4.7, "s": "5"}
	if got := intParam(params, "i"); got != 3 {
		t.Errorf("i = %d", got)
	}
	if got := intParam(params, "f"); got != 4 {
		t.Errorf("f = %d, 期望截断为 4", got)
	}
	if got := intParam(params, "missing"); got != 0 {
		t.Errorf("缺失应返回 0，得 %d", got)
	}
	if got := intParam(nil, "i"); got != 0 {
		t.Errorf("nil params 应返回 0，得 %d", got)
	}
}

// TestStringSliceParam 切片与单值两种形态。
func TestStringSliceParam(t *testing.T) {
	params := map[string]any{
		"list": []string{"a", "b"},
		"any":  []any{"c", 1},
		"one":  "d",
	}
	if got := stringSliceParam(params, "list"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("list = %v", got)
	}
	if got := stringSliceParam(params, "any"); !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("any = %v（非字符串元素应跳过）", got)
	}
	if got := stringSliceParam(params, "missing"); len(got) != 0 {
		t.Errorf("缺失应为空，得 %v", got)
	}
}

// TestStringSliceToAny 转 []any。
func TestStringSliceToAny(t *testing.T) {
	got := stringSliceToAny([]string{"a", "b"})
	if !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("got %v", got)
	}
	if got := stringSliceToAny(nil); len(got) != 0 {
		t.Errorf("nil 应为空切片，得 %v", got)
	}
}

// ──────────────────────────── errorCodeOf ────────────────────────────

// TestErrorCodeOf_优先Code方法 实现了 Code() 的错误取其码。
func TestErrorCodeOf_优先Code方法(t *testing.T) {
	err := &ProcessCliBoundaryError{Message: "boundary"}
	if got := errorCodeOf(err); got != codeProcessCliChannelInvalid {
		t.Errorf("got %q, 期望 %q", got, codeProcessCliChannelInvalid)
	}
}

// TestErrorCodeOf_普通错误回落 无 Code 方法时回落 RUNTIME_FAILED。
// Python: query.py:117-121
func TestErrorCodeOf_普通错误回落(t *testing.T) {
	if got := errorCodeOf(errors.New("plain")); got != "RUNTIME_FAILED" {
		t.Errorf("got %q, 期望 RUNTIME_FAILED", got)
	}
}

// TestErrorCodeOf_空码回落 Code() 返回空串时回落。
func TestErrorCodeOf_空码回落(t *testing.T) {
	if got := errorCodeOf(&emptyCodeError{}); got != "RUNTIME_FAILED" {
		t.Errorf("got %q, 期望 RUNTIME_FAILED", got)
	}
}

// emptyCodeError Code 返回空串的错误替身。
type emptyCodeError struct{}

// Error 实现 error 接口。
func (e *emptyCodeError) Error() string { return "empty code" }

// Code 返回空串（触发回落）。
func (e *emptyCodeError) Code() string { return "   " }

// ──────────────────────────── clientFactoryOrDefault ────────────────────────────

// TestClientFactoryOrDefault_注入优先 传入工厂时原样返回。
func TestClientFactoryOrDefault_注入优先(t *testing.T) {
	sentinel := &InProcessRuntimeClient{}
	factory := clientFactoryOrDefault(func() *InProcessRuntimeClient { return sentinel })
	if got := factory(); got != sentinel {
		t.Errorf("注入的工厂未被使用")
	}
}

// TestClientFactoryOrDefault_nil用默认 nil 时返回默认构造。
func TestClientFactoryOrDefault_nil用默认(t *testing.T) {
	factory := clientFactoryOrDefault(nil)
	if factory == nil {
		t.Fatal("nil 时应返回默认工厂")
	}
	if client := factory(); client == nil {
		t.Error("默认工厂产出 nil")
	}
}
