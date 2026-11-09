package auto_permission

import (
	"strings"
	"testing"

	hsecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── resultFromBaseSideEffect ────────────────────────────

// TestResultFromBaseSideEffect_非nil直接返回 验证优先返回入参。
func TestResultFromBaseSideEffect_非nil直接返回(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	got := resultFromBaseSideEffect(cbc, "explicit")
	if got != "explicit" {
		t.Errorf("got %v", got)
	}
}

// TestResultFromBaseSideEffect_nilCtx 验证 nil 上下文安全。
func TestResultFromBaseSideEffect_nilCtx(t *testing.T) {
	if got := resultFromBaseSideEffect(nil, nil); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestResultFromBaseSideEffect_无skipTool标记 验证不还原。
func TestResultFromBaseSideEffect_无skipTool标记(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{ToolResult: "result"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	if got := resultFromBaseSideEffect(cbc, nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestResultFromBaseSideEffect_从ToolResult还原 验证副作用还原。
func TestResultFromBaseSideEffect_从ToolResult还原(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{ToolResult: map[string]any{"success": false}}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	cbc.SetExtra(skipToolExtraKey, true)

	got := resultFromBaseSideEffect(cbc, nil)
	if got == nil {
		t.Fatal("should reconstruct from inputs.ToolResult")
	}
	if mapping, ok := got.(map[string]any); !ok || mapping["success"] != false {
		t.Errorf("got %v", got)
	}
}

// TestResultFromBaseSideEffect_skipTool非true 验证严格判等。
func TestResultFromBaseSideEffect_skipTool非true(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{ToolResult: "result"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	cbc.SetExtra(skipToolExtraKey, "true") // 字符串不算
	if got := resultFromBaseSideEffect(cbc, nil); got != nil {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── permissionToolResultText ────────────────────────────

// TestPermissionToolResultText_各形态 验证三级回落。
func TestPermissionToolResultText_各形态(t *testing.T) {
	// feedback 优先
	if got := permissionToolResultText(map[string]any{"feedback": "custom"}); got != "custom" {
		t.Errorf("feedback -> %q", got)
	}
	// 非空字符串
	if got := permissionToolResultText("plain text"); got != "plain text" {
		t.Errorf("string -> %q", got)
	}
	// 空字符串回落默认
	if got := permissionToolResultText(""); got != defaultPermissionDeniedText {
		t.Errorf("empty -> %q", got)
	}
	// nil 回落默认
	if got := permissionToolResultText(nil); got != defaultPermissionDeniedText {
		t.Errorf("nil -> %q", got)
	}
}

// ──────────────────────────── permissionToolResultPayload ────────────────────────────

// TestPermissionToolResultPayload_拒绝类 验证结构化 payload。
func TestPermissionToolResultPayload_拒绝类(t *testing.T) {
	got := permissionToolResultPayload(map[string]any{
		"permission_status": "denied",
	}, "denied text")
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want map", got)
	}
	if payload["status"] != "denied" || payload["permission_decision"] != "deny" {
		t.Errorf("got %v", payload)
	}
}

// TestPermissionToolResultPayload_非拒绝类 验证返回纯文本。
func TestPermissionToolResultPayload_非拒绝类(t *testing.T) {
	got := permissionToolResultPayload("allow result", "text")
	if text, ok := got.(string); !ok || text != "text" {
		t.Errorf("got %v, want text", got)
	}
}

// ──────────────────────────── isPermissionDeniedMetadataPayload ────────────────────────────

// TestIsPermissionDeniedMetadataPayload_三种判定 验证三个条件。
func TestIsPermissionDeniedMetadataPayload_三种判定(t *testing.T) {
	cases := []struct {
		name   string
		result any
		want   bool
	}{
		{"permission_status", map[string]any{"permission_status": "DENIED"}, true},
		{"status", map[string]any{"status": " denied "}, true},
		{"feedback含标记", map[string]any{"feedback": "x [permission_denied] y"}, true},
		{"无标记", map[string]any{"status": "ok"}, false},
		{"非map", "string", false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPermissionDeniedMetadataPayload(c.result); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// ──────────────────────────── permissionDeniedToolResultPayload ────────────────────────────

// TestPermissionDeniedToolResultPayload_固定字段 验证 7 个固定字段。
func TestPermissionDeniedToolResultPayload_固定字段(t *testing.T) {
	got := permissionDeniedToolResultPayload(nil, "denied")
	expected := map[string]any{
		"success":             false,
		"status":              "denied",
		"is_error":            true,
		"result":              "denied",
		"error":               "denied",
		"permission_decision": "deny",
		"permission_status":   "denied",
	}
	for key, want := range expected {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
}

// TestPermissionDeniedToolResultPayload_可选字段 验证 reviewer_metadata 与 reason。
func TestPermissionDeniedToolResultPayload_可选字段(t *testing.T) {
	got := permissionDeniedToolResultPayload(map[string]any{
		"reviewer_metadata": map[string]any{"route": "auto"},
		"reason":            "policy",
	}, "denied")

	metadata, ok := got["reviewer_metadata"].(map[string]any)
	if !ok || metadata["route"] != "auto" {
		t.Errorf("reviewer_metadata = %v", got["reviewer_metadata"])
	}
	if got["reason"] != "policy" {
		t.Errorf("reason = %v", got["reason"])
	}
}

// TestPermissionDeniedToolResultPayload_深拷贝metadata 验证不共享引用。
func TestPermissionDeniedToolResultPayload_深拷贝metadata(t *testing.T) {
	source := map[string]any{"reviewer_metadata": map[string]any{"k": "v"}}
	got := permissionDeniedToolResultPayload(source, "denied")
	metadata := got["reviewer_metadata"].(map[string]any)
	metadata["k"] = "changed"
	if source["reviewer_metadata"].(map[string]any)["k"] != "v" {
		t.Error("metadata should be copied")
	}
}

// TestPermissionDeniedToolResultPayload_空reason忽略 验证空值不写入。
func TestPermissionDeniedToolResultPayload_空reason忽略(t *testing.T) {
	got := permissionDeniedToolResultPayload(map[string]any{"reason": ""}, "denied")
	if _, exists := got["reason"]; exists {
		t.Errorf("empty reason should be skipped, got %v", got["reason"])
	}
}

// ──────────────────────────── permissionInterruptMessage ────────────────────────────

// TestPermissionInterruptMessage_四级回落 验证优先级。
func TestPermissionInterruptMessage_四级回落(t *testing.T) {
	// value.message 优先
	got := permissionInterruptMessage(map[string]any{
		"value":   map[string]any{"message": "nested"},
		"message": "outer",
	})
	if got != "nested" {
		t.Errorf("value.message -> %q", got)
	}
	// message
	got = permissionInterruptMessage(map[string]any{"message": "outer"})
	if got != "outer" {
		t.Errorf("message -> %q", got)
	}
	// feedback
	got = permissionInterruptMessage(map[string]any{"feedback": "fb"})
	if got != "fb" {
		t.Errorf("feedback -> %q", got)
	}
	// 默认
	if got := permissionInterruptMessage(nil); got != defaultManualApprovalMessage {
		t.Errorf("nil -> %q", got)
	}
}

// ──────────────────────────── permissionInterruptExtraFields ────────────────────────────

// TestPermissionInterruptExtraFields_合并与排除 验证 metadata 全量 + value 排除两项。
func TestPermissionInterruptExtraFields_合并与排除(t *testing.T) {
	got := permissionInterruptExtraFields(map[string]any{
		"metadata": map[string]any{"from_metadata": 1},
		"value": map[string]any{
			"message":     "excluded",
			"question":    "excluded",
			"kept":        "yes",
			"another_key": 2,
		},
	})
	if got["from_metadata"] != 1 {
		t.Errorf("metadata not merged: %v", got)
	}
	if got["kept"] != "yes" || got["another_key"] != 2 {
		t.Errorf("value fields not merged: %v", got)
	}
	if _, exists := got["message"]; exists {
		t.Error("message should be excluded")
	}
	if _, exists := got["question"]; exists {
		t.Error("question should be excluded")
	}
}

// TestPermissionInterruptExtraFields_非map 验证返回空 map。
func TestPermissionInterruptExtraFields_非map(t *testing.T) {
	if got := permissionInterruptExtraFields("string"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
	if got := permissionInterruptExtraFields(nil); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── permissionTerminalOutcome ────────────────────────────

// TestPermissionTerminalOutcome_四类别 验证映射。
func TestPermissionTerminalOutcome_四类别(t *testing.T) {
	// ClassifyPermissionResult 的判定依据：
	//   allow          → security.PermissionConfirmResponse{Approved: true}
	//   denied         → feedback 含 "[permission_denied]"
	//   user_rejection → feedback 含 "[permission_rejected]"
	cases := []struct {
		name   string
		result any
		want   string
	}{
		{"allow", hsecurity.PermissionConfirmResponse{Approved: true}, "allow"},
		{"denied", map[string]any{"feedback": "[permission_denied] denied"}, "deny"},
		{"user_rejection", map[string]any{"feedback": "[permission_rejected] rejected"}, "cancel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := permissionTerminalOutcome(c.result); got != c.want {
				t.Errorf("got %q, want %q（class=%q）", got, c.want,
					permissions.ClassifyPermissionResult(c.result))
			}
		})
	}
	// interrupt 无终态
	if got := permissionTerminalOutcome(map[string]any{"interrupt": true}); got != "" {
		t.Errorf("interrupt -> %q, want empty", got)
	}
}

// ──────────────────────────── permissionTerminalDecisionSource ────────────────────────────

// TestPermissionTerminalDecisionSource 验证正则校验。
func TestPermissionTerminalDecisionSource(t *testing.T) {
	cases := []struct{ in, want string }{
		{"auto_reviewer", "auto_reviewer"},
		{"  AUTO  ", "auto"},
		{"a", "a"},
		{"", unknownDecisionSource},
		{"  ", unknownDecisionSource},
		{"1invalid", unknownDecisionSource},
		{"has-dash", unknownDecisionSource},
		{"has space", unknownDecisionSource},
		{"UPPER", "upper"},
	}
	for _, c := range cases {
		if got := permissionTerminalDecisionSource(c.in); got != c.want {
			t.Errorf("permissionTerminalDecisionSource(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 超长（>64）应回落
	long := "a" + strings.Repeat("b", 64)
	if got := permissionTerminalDecisionSource(long); got != unknownDecisionSource {
		t.Errorf("overlong -> %q", got)
	}
}

// ──────────────────────────── permissionFeedbackText ────────────────────────────

// TestPermissionFeedbackText_三形态 验证字符串/map/对象。
func TestPermissionFeedbackText_三形态(t *testing.T) {
	// 字符串
	if got := permissionFeedbackText("direct"); got != "direct" {
		t.Errorf("string -> %q", got)
	}
	// map 的 feedback
	if got := permissionFeedbackText(map[string]any{"feedback": "fb"}); got != "fb" {
		t.Errorf("map -> %q", got)
	}
	// 嵌套 value.feedback
	if got := permissionFeedbackText(map[string]any{
		"value": map[string]any{"feedback": "nested"},
	}); got != "nested" {
		t.Errorf("nested -> %q", got)
	}
	// 无 feedback
	if got := permissionFeedbackText(map[string]any{}); got != "" {
		t.Errorf("empty map -> %q", got)
	}
	// 实现了 Feedback 的对象
	if got := permissionFeedbackText(feedbackProvider{"object-fb"}); got != "object-fb" {
		t.Errorf("provider -> %q", got)
	}
	// 其他类型
	if got := permissionFeedbackText(42); got != "" {
		t.Errorf("int -> %q", got)
	}
}

// feedbackProvider 测试用 Feedback 提供者。
type feedbackProvider struct{ text string }

// Feedback 实现 feedback 接口。
func (f feedbackProvider) Feedback() string { return f.text }
