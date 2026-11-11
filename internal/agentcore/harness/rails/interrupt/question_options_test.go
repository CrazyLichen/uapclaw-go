package interrupt

import (
	"testing"
)

// ──────────────────────────── planApprovalInterruptOptions ────────────────────────────

// TestPlanApprovalInterruptOptions_三条件满足 测试计划审批选项。
func TestPlanApprovalInterruptOptions_三条件满足(t *testing.T) {
	message := "**计划审批**\nAgent 已完成计划制定，等待你审批：\n\n步骤一"
	options := planApprovalInterruptOptions(confirmInterruptSource, exitPlanModeToolName, message)
	if len(options) != 2 {
		t.Fatalf("len = %d, want 2", len(options))
	}
	if options[0]["value"] != "approve" {
		t.Errorf("options[0].value = %v, want approve", options[0]["value"])
	}
}

// TestPlanApprovalInterruptOptions_source不符返回nil 测试条件 1。
func TestPlanApprovalInterruptOptions_source不符返回nil(t *testing.T) {
	message := "**计划审批**\n正文"
	if got := planApprovalInterruptOptions("permission_interrupt", exitPlanModeToolName, message); got != nil {
		t.Errorf("got %v, want nil（source 不是 confirm_interrupt）", got)
	}
}

// TestPlanApprovalInterruptOptions_工具不符返回nil 测试条件 2。
func TestPlanApprovalInterruptOptions_工具不符返回nil(t *testing.T) {
	message := "**计划审批**\n正文"
	if got := planApprovalInterruptOptions(confirmInterruptSource, "bash", message); got != nil {
		t.Errorf("got %v, want nil（工具不是 exit_plan_mode）", got)
	}
}

// TestPlanApprovalInterruptOptions_文案不符返回nil 测试条件 3。
func TestPlanApprovalInterruptOptions_文案不符返回nil(t *testing.T) {
	if got := planApprovalInterruptOptions(confirmInterruptSource, exitPlanModeToolName, "普通文案"); got != nil {
		t.Errorf("got %v, want nil（不是计划审批文案）", got)
	}
}

// ──────────────────────────── fallbackInterruptOptions ────────────────────────────

// TestFallbackInterruptOptions_计划审批优先 测试计划审批时用专用选项。
func TestFallbackInterruptOptions_计划审批优先(t *testing.T) {
	t.Setenv(envEdition, "")
	message := "**计划审批**\nAgent 已完成计划制定，等待你审批：\n\n正文"
	options := fallbackInterruptOptions(confirmInterruptSource, exitPlanModeToolName, message)
	if len(options) != 2 {
		t.Fatalf("len = %d, want 2（计划审批两档）", len(options))
	}
	if options[0]["value"] != "approve" {
		t.Errorf("options[0].value = %v, want approve", options[0]["value"])
	}
}

// TestFallbackInterruptOptions_普通工具四档 测试非计划审批回落四档。
func TestFallbackInterruptOptions_普通工具四档(t *testing.T) {
	t.Setenv(envEdition, "")
	options := fallbackInterruptOptions("permission_interrupt", "bash", "需要授权")
	if len(options) != 4 {
		t.Fatalf("len = %d, want 4（个人版四档）", len(options))
	}
}

// ──────────────────────────── questionOptionsFromUIOptions ────────────────────────────

// TestQuestionOptions_CLI单次允许只留两档 测试第一步短路。
//
// Python: if metadata.get("process_cli_allow_once_only") is True:
//
//	return [o for o in _default_interrupt_options()
//	        if o["value"] in {ALLOW_ONCE, REJECT}]
func TestQuestionOptions_CLI单次允许只留两档(t *testing.T) {
	t.Setenv(envEdition, "")
	value := map[string]any{
		"metadata": map[string]any{processCLIAllowOnceOnlyKey: true},
		"ui_options": []any{
			map[string]any{"label": "自定义", "value": "custom"},
		},
	}
	options := questionOptionsFromUIOptions(value, "permission_interrupt", "bash", "")
	if len(options) != 2 {
		t.Fatalf("len = %d, want 2（CLI 模式只给本次允许 + 拒绝）", len(options))
	}
	if options[0]["value"] != AllowOnce || options[1]["value"] != Reject {
		t.Errorf("got values %v/%v, want allow_once/reject",
			options[0]["value"], options[1]["value"])
	}
}

// TestQuestionOptions_CLI标志非true不短路 测试 is True 语义。
func TestQuestionOptions_CLI标志非true不短路(t *testing.T) {
	t.Setenv(envEdition, "")
	value := map[string]any{
		"metadata": map[string]any{processCLIAllowOnceOnlyKey: 1},
	}
	options := questionOptionsFromUIOptions(value, "permission_interrupt", "bash", "")
	if len(options) != 4 {
		t.Errorf("len = %d, want 4（数字 1 不等于 True，不短路）", len(options))
	}
}

// TestQuestionOptions_用ui_options 测试第二步。
func TestQuestionOptions_用ui_options(t *testing.T) {
	t.Setenv(envEdition, "")
	value := map[string]any{
		"ui_options": []any{
			map[string]any{"label": "允许", "value": "allow_once", "description": "d1"},
			map[string]any{"label": "拒绝", "value": "reject", "description": "d2"},
		},
	}
	options := questionOptionsFromUIOptions(value, "permission_interrupt", "bash", "")
	if len(options) != 2 {
		t.Fatalf("len = %d, want 2", len(options))
	}
	if options[0]["label"] != "允许" {
		t.Errorf("options[0].label = %v", options[0]["label"])
	}
}

// TestQuestionOptions_label为空被丢弃 测试归一化后过滤。
func TestQuestionOptions_label为空被丢弃(t *testing.T) {
	t.Setenv(envEdition, "")
	value := map[string]any{
		"ui_options": []any{
			map[string]any{"label": "", "value": ""},
			map[string]any{"label": "有效", "value": "allow_once"},
		},
	}
	options := questionOptionsFromUIOptions(value, "permission_interrupt", "bash", "")
	if len(options) != 1 {
		t.Fatalf("len = %d, want 1（label 为空的项应丢弃）", len(options))
	}
}

// TestQuestionOptions_无ui_options兜底 测试第三步。
func TestQuestionOptions_无ui_options兜底(t *testing.T) {
	t.Setenv(envEdition, "")
	options := questionOptionsFromUIOptions(map[string]any{}, "permission_interrupt", "bash", "")
	if len(options) != 4 {
		t.Errorf("len = %d, want 4（兜底四档）", len(options))
	}
}

// TestQuestionOptions_企业版剔除always 测试企业版过滤。
func TestQuestionOptions_企业版剔除always(t *testing.T) {
	t.Setenv(envEdition, "enterprise")
	value := map[string]any{
		"ui_options": []any{
			map[string]any{"label": "本次允许", "value": AllowOnce},
			map[string]any{"label": "永久记住", "value": AlwaysAllow},
			map[string]any{"label": "拒绝", "value": Reject},
		},
	}
	options := questionOptionsFromUIOptions(value, "permission_interrupt", "bash", "")
	for _, option := range options {
		if option["value"] == AlwaysAllow {
			t.Error("企业版不应含 always 档")
		}
	}
	if len(options) != 2 {
		t.Errorf("len = %d, want 2", len(options))
	}
}

// TestQuestionOptions_企业版剔除后为空走兜底 测试企业版兜底。
func TestQuestionOptions_企业版剔除后为空走兜底(t *testing.T) {
	t.Setenv(envEdition, "enterprise")
	value := map[string]any{
		"ui_options": []any{
			map[string]any{"label": "永久记住", "value": AlwaysAllow},
		},
	}
	options := questionOptionsFromUIOptions(value, "permission_interrupt", "bash", "")
	if len(options) != 3 {
		t.Errorf("len = %d, want 3（企业版兜底三档）", len(options))
	}
}

// TestQuestionOptions_计划审批时企业版兜底仍是计划选项 测试兜底优先级。
func TestQuestionOptions_计划审批时企业版兜底仍是计划选项(t *testing.T) {
	t.Setenv(envEdition, "enterprise")
	message := "**计划审批**\nAgent 已完成计划制定，等待你审批：\n\n正文"
	options := questionOptionsFromUIOptions(map[string]any{}, confirmInterruptSource, exitPlanModeToolName, message)
	if len(options) != 2 {
		t.Fatalf("len = %d, want 2（计划审批两档，不受企业版影响）", len(options))
	}
	if options[0]["value"] != "approve" {
		t.Errorf("options[0].value = %v", options[0]["value"])
	}
}
