package runtime

import "testing"

// TestHasInterruptResumePayload request_id 与 answers 双非空。
func TestHasInterruptResumePayload(t *testing.T) {
	if HasInterruptResumePayload(nil) {
		t.Error("nil 应为 false")
	}
	if HasInterruptResumePayload(map[string]any{"answers": []any{"a"}}) {
		t.Error("缺 request_id 应为 false")
	}
	if HasInterruptResumePayload(map[string]any{"request_id": "r1", "answers": []any{}}) {
		t.Error("answers 为空应为 false")
	}
	if HasInterruptResumePayload(map[string]any{"request_id": "  ", "answers": []any{"a"}}) {
		t.Error("request_id 全空白应为 false")
	}
	if !HasInterruptResumePayload(map[string]any{"request_id": "r1", "answers": []any{"a"}}) {
		t.Error("双非空应为 true")
	}
}

// TestIsInterruptResumePayload 三条路径：answers_paused_calls / 普通 resume / legacy 审批。
func TestIsInterruptResumePayload(t *testing.T) {
	if !IsInterruptResumePayload(map[string]any{
		"source": "ask_user_interrupt", "answers": []any{"a"}, "request_id": "r1",
	}) {
		t.Error("answers_paused_calls 命中应为 true")
	}

	if IsInterruptResumePayload(map[string]any{
		"source": "other_source", "answers": []any{"a"}, "request_id": "r1",
	}) {
		t.Error("非 legacy source 且非 paused calls 应为 false")
	}

	if !IsInterruptResumePayload(map[string]any{
		"source":         LegacySkillEvolutionApprovalSource,
		"answers":        []any{"a"},
		"request_id":     "r1",
		"evolution_meta": map[string]any{"approval_transport": "interrupt"},
	}) {
		t.Error("legacy 审批 + approval_transport=interrupt 应为 true")
	}

	if IsInterruptResumePayload(map[string]any{
		"source":         LegacySkillEvolutionApprovalSource,
		"answers":        []any{"a"},
		"request_id":     "r1",
		"evolution_meta": map[string]any{"approval_transport": "other"},
	}) {
		t.Error("legacy 审批但 transport 非 interrupt 应为 false")
	}

	if IsInterruptResumePayload(map[string]any{
		"source":     LegacySkillEvolutionApprovalSource,
		"answers":    []any{"a"},
		"request_id": "r1",
	}) {
		t.Error("legacy 审批但缺 evolution_meta 应为 false")
	}
}

// TestIsInterruptResumeRequest 一行转调。
func TestIsInterruptResumeRequest(t *testing.T) {
	if !IsInterruptResumeRequest(map[string]any{
		"source": "permission_interrupt", "answers": []any{"a"}, "request_id": "r1",
	}) {
		t.Error("应转调 IsInterruptResumePayload")
	}
	if IsInterruptResumeRequest(map[string]any{"source": "other"}) {
		t.Error("普通载荷应为 false")
	}
}
