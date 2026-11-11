package planapproval

import (
	"strings"
	"testing"
)

// ──────────────────────────── IsPlanApprovalMessage ────────────────────────────

// TestIsPlanApprovalMessage_中文标题 测试中文计划审批标题。
func TestIsPlanApprovalMessage_中文标题(t *testing.T) {
	if !IsPlanApprovalMessage("**计划审批**\n\n计划内容...") {
		t.Error("got false, want true（含中文标题）")
	}
}

// TestIsPlanApprovalMessage_英文标题 测试英文标题。
func TestIsPlanApprovalMessage_英文标题(t *testing.T) {
	if !IsPlanApprovalMessage("**Plan Approval**\n\nplan content...") {
		t.Error("got false, want true（含英文标题）")
	}
}

// TestIsPlanApprovalMessage_普通文案 测试非计划审批。
func TestIsPlanApprovalMessage(t *testing.T) {
	if IsPlanApprovalMessage("工具 `bash` 需要授权才能执行") {
		t.Error("got true, want false（普通确认文案）")
	}
}

// TestIsPlanApprovalMessage_空串 测试空输入。
func TestIsPlanApprovalMessage_空串(t *testing.T) {
	if IsPlanApprovalMessage("") {
		t.Error("空串 got true, want false")
	}
}

// ──────────────────────────── StripInlinePlanApprovalChoices ────────────────────────────

// TestStripInlinePlanApprovalChoices_中文分隔 测试剥离中文选择行。
func TestStripInlinePlanApprovalChoices_中文分隔(t *testing.T) {
	message := "**计划审批**\n\n做点什么\n\n---\n请选择：\n1. 批准\n2. 拒绝"
	got := StripInlinePlanApprovalChoices(message)
	if strings.Contains(got, "请选择") {
		t.Errorf("got %q，选择行应被剥离", got)
	}
	if !strings.Contains(got, "做点什么") {
		t.Errorf("got %q，正文应保留", got)
	}
}

// TestStripInlinePlanApprovalChoices_英文分隔 测试英文分隔。
func TestStripInlinePlanApprovalChoices_英文分隔(t *testing.T) {
	message := "**Plan Approval**\n\ncontent\n\n---\nChoose:\n1. Approve\n2. Reject"
	got := StripInlinePlanApprovalChoices(message)
	if strings.Contains(got, "Choose:") {
		t.Errorf("got %q，选择行应被剥离", got)
	}
}

// TestStripInlinePlanApprovalChoices_无标记不变 测试无选择标记时不变。
func TestStripInlinePlanApprovalChoices_无标记不变(t *testing.T) {
	message := "**计划审批**\n\n正文"
	if got := StripInlinePlanApprovalChoices(message); got != message {
		t.Errorf("got %q, want 原样", got)
	}
}

// TestStripInlinePlanApprovalChoices_尾部rstrip 测试剥离后去尾空白。
//
// Python: message.split(marker, 1)[0].rstrip()
func TestStripInlinePlanApprovalChoices_尾部rstrip(t *testing.T) {
	message := "正文   \n\n---\n请选择：\n1. 批准"
	got := StripInlinePlanApprovalChoices(message)
	if strings.HasSuffix(got, " ") || strings.HasSuffix(got, "\n") {
		t.Errorf("got %q，尾部空白应被去掉", got)
	}
}

// ──────────────────────────── ExtractPlanApprovalContent ────────────────────────────

// TestExtractPlanApprovalContent_中文正文 测试中文正文提取。
func TestExtractPlanApprovalContent_中文正文(t *testing.T) {
	message := "**计划审批**\nAgent 已完成计划制定，等待你审批：\n\n步骤一\n步骤二"
	content, language := ExtractPlanApprovalContent(message)
	if language != "cn" {
		t.Errorf("language = %q, want cn", language)
	}
	if !strings.Contains(content, "步骤一") {
		t.Errorf("content = %q，应含计划正文", content)
	}
	if strings.Contains(content, "已完成计划制定") {
		t.Errorf("content = %q，引导语应被去掉", content)
	}
}

// TestExtractPlanApprovalContent_英文正文 测试英文正文提取。
func TestExtractPlanApprovalContent_英文正文(t *testing.T) {
	message := "**Plan Approval**\nThe agent has completed a plan and is requesting your approval:\n\nstep one"
	content, language := ExtractPlanApprovalContent(message)
	if language != "en" {
		t.Errorf("language = %q, want en", language)
	}
	if !strings.Contains(content, "step one") {
		t.Errorf("content = %q", content)
	}
}

// TestExtractPlanApprovalContent_空计划中文 测试空计划文案。
//
// Python: body.startswith(_PLAN_EMPTY_INTRO_CN) → return "", "cn"
func TestExtractPlanApprovalContent_空计划中文(t *testing.T) {
	message := "**计划审批**\nAgent 请求退出 plan 模式，但计划文件为空。"
	content, language := ExtractPlanApprovalContent(message)
	if content != "" {
		t.Errorf("content = %q, want 空串（计划为空）", content)
	}
	if language != "cn" {
		t.Errorf("language = %q, want cn", language)
	}
}

// TestExtractPlanApprovalContent_空计划英文 测试英文空计划。
func TestExtractPlanApprovalContent_空计划英文(t *testing.T) {
	message := "**Plan Approval**\nThe agent is requesting to exit plan mode, but the plan file is empty."
	content, language := ExtractPlanApprovalContent(message)
	if content != "" {
		t.Errorf("content = %q, want 空串", content)
	}
	if language != "en" {
		t.Errorf("language = %q, want en", language)
	}
}

// ──────────────────────────── BuildPlanApprovalOptionsFromMessage ────────────────────────────

// TestBuildPlanApprovalOptionsFromMessage_中文 测试中文批准/拒绝。
func TestBuildPlanApprovalOptionsFromMessage_中文(t *testing.T) {
	message := "**计划审批**\nAgent 已完成计划制定，等待你审批：\n\n步骤一"
	options := BuildPlanApprovalOptionsFromMessage(message)
	if len(options) != 2 {
		t.Fatalf("len = %d, want 2", len(options))
	}
	if options[0]["label"] != "批准" {
		t.Errorf("options[0].label = %v, want 批准", options[0]["label"])
	}
	if options[0]["value"] != "approve" {
		t.Errorf("options[0].value = %v, want approve", options[0]["value"])
	}
	if options[1]["label"] != "拒绝" {
		t.Errorf("options[1].label = %v", options[1]["label"])
	}
	if options[1]["value"] != "reject" {
		t.Errorf("options[1].value = %v, want reject", options[1]["value"])
	}
}

// TestBuildPlanApprovalOptionsFromMessage_英文 测试英文 Approve/Reject。
func TestBuildPlanApprovalOptionsFromMessage_英文(t *testing.T) {
	message := "**Plan Approval**\nThe agent has completed a plan and is requesting your approval:\n\nstep"
	options := BuildPlanApprovalOptionsFromMessage(message)
	if len(options) != 2 {
		t.Fatalf("len = %d, want 2", len(options))
	}
	if options[0]["label"] != "Approve" {
		t.Errorf("options[0].label = %v", options[0]["label"])
	}
	if options[1]["label"] != "Reject" {
		t.Errorf("options[1].label = %v", options[1]["label"])
	}
}

// TestBuildPlanApprovalOptionsFromMessage_空计划中文文案 测试空计划时的不同描述。
func TestBuildPlanApprovalOptionsFromMessage_空计划中文文案(t *testing.T) {
	message := "**计划审批**\nAgent 请求退出 plan 模式，但计划文件为空。"
	options := BuildPlanApprovalOptionsFromMessage(message)
	if len(options) != 2 {
		t.Fatalf("len = %d, want 2", len(options))
	}
	if options[0]["description"] != "退出 plan 模式（无计划内容）" {
		t.Errorf("description = %v，空计划的批准描述应不同", options[0]["description"])
	}
}

// TestBuildPlanApprovalOptionsFromMessage_空计划英文文案 测试英文空计划。
func TestBuildPlanApprovalOptionsFromMessage_空计划英文文案(t *testing.T) {
	message := "**Plan Approval**\nThe agent is requesting to exit plan mode, but the plan file is empty."
	options := BuildPlanApprovalOptionsFromMessage(message)
	if options[0]["description"] != "Exit plan mode (no plan content)" {
		t.Errorf("description = %v", options[0]["description"])
	}
}
