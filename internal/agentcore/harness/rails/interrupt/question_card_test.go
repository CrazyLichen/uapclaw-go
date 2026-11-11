package interrupt

import (
	"strings"
	"testing"
)

// ──────────────────────────── 权限类卡片 ────────────────────────────

// TestFormatQuestion_权限类标题 测试权限审批标题格式。
func TestFormatQuestion_权限类标题(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value:       map[string]any{"message": "需要授权"},
		Source:      permissionInterruptSource,
		ToolName:    "bash",
		Message:     "需要授权",
	})
	if got["header"] != "权限审批: bash" {
		t.Errorf("header = %v, want 权限审批: bash", got["header"])
	}
}

// TestFormatQuestion_权限类无工具名 测试无工具名的默认标题。
func TestFormatQuestion_权限类无工具名(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value:       map[string]any{},
		Source:      permissionInterruptSource,
		Message:     "文案",
	})
	if got["header"] != "权限审批" {
		t.Errorf("header = %v, want 权限审批", got["header"])
	}
}

// TestFormatQuestion_askTitle覆盖标题 测试 ask_title 优先级最高。
func TestFormatQuestion_askTitle覆盖标题(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value: map[string]any{
			"metadata": map[string]any{"ask_title": "自定义标题"},
		},
		Source:   permissionInterruptSource,
		ToolName: "bash",
		Message:  "文案",
	})
	if got["header"] != "自定义标题" {
		t.Errorf("header = %v, want 自定义标题", got["header"])
	}
}

// TestFormatQuestion_权限类空文案补默认 测试兜底文案。
//
// Python: message = f"工具 `{tool_name}` 需要授权才能执行"
func TestFormatQuestion_权限类空文案补默认(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value:       map[string]any{},
		Source:      permissionInterruptSource,
		ToolName:    "bash",
	})
	question, _ := got["question"].(string)
	if question != "工具 `bash` 需要授权才能执行" {
		t.Errorf("question = %q", question)
	}
}

// TestFormatQuestion_multi_select恒false 测试 multi_select 字段。
func TestFormatQuestion_multi_select恒false(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value:       map[string]any{"message": "x"},
		Source:      permissionInterruptSource,
		ToolName:    "bash",
	})
	if got["multi_select"] != false {
		t.Errorf("multi_select = %v, want false", got["multi_select"])
	}
}

// ──────────────────────────── 确认类卡片 ────────────────────────────

// TestFormatQuestion_确认类标题 测试操作确认标题。
func TestFormatQuestion_确认类标题(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value:       map[string]any{"message": "确认吗"},
		Source:      confirmInterruptSource,
		ToolName:    "bash",
		Message:     "确认吗",
	})
	if got["header"] != "操作确认: bash" {
		t.Errorf("header = %v, want 操作确认: bash", got["header"])
	}
}

// TestFormatQuestion_确认类通用问句重写 测试 "Please approve or reject?" 被重写。
//
// Python: needs_message = not message or (source == "confirm_interrupt"
//
//	and generic_confirm_message)
func TestFormatQuestion_确认类通用问句重写(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value:       map[string]any{},
		Source:      confirmInterruptSource,
		ToolName:    "bash",
		Message:     "Please approve or reject?",
	})
	question, _ := got["question"].(string)
	if question == "Please approve or reject?" {
		t.Error("通用问句应被重写为描述性文案")
	}
	if !strings.Contains(question, "bash") {
		t.Errorf("question = %q，应含工具名", question)
	}
}

// ──────────────────────────── 计划审批卡片 ────────────────────────────

// TestFormatQuestion_计划审批标题 测试专用标题。
func TestFormatQuestion_计划审批标题(t *testing.T) {
	message := "**计划审批**\nAgent 已完成计划制定，等待你审批：\n\n步骤一"
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value:       map[string]any{},
		Source:      confirmInterruptSource,
		ToolName:    exitPlanModeToolName,
		Message:     message,
	})
	if got["header"] != "Exit Plan and Execute" {
		t.Errorf("header = %v, want Exit Plan and Execute", got["header"])
	}
	question, _ := got["question"].(string)
	// 注意：strip 只剥离「---\n请选择：」之后的部分，标题本身保留（Python 行为）。
	if !strings.Contains(question, "**计划审批**") {
		t.Errorf("question = %q，标题应保留", question)
	}
	if !strings.Contains(question, "步骤一") {
		t.Errorf("question = %q，正文应保留", question)
	}
}

// ──────────────────────────── 权限类附加字段 ────────────────────────────

// TestFormatQuestion_card_id 测试有队列卡片时附带 card_id。
func TestFormatQuestion_card_id(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction:  map[string]any{},
		Value:        map[string]any{"message": "x"},
		Source:       permissionInterruptSource,
		ToolName:     "bash",
		InvocationID: "inv-1",
	})
	if got["card_id"] != "inv-1" {
		t.Errorf("card_id = %v, want inv-1", got["card_id"])
	}
}

// TestFormatQuestion_无card_id不输出 测试空 InvocationID 不出键。
func TestFormatQuestion_无card_id不输出(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{},
		Value:       map[string]any{"message": "x"},
		Source:      permissionInterruptSource,
		ToolName:    "bash",
	})
	if _, exists := got["card_id"]; exists {
		t.Error("无队列卡片时不应输出 card_id")
	}
}

// TestFormatQuestion_reviewer_metadata 测试审查器元数据附带。
func TestFormatQuestion_reviewer_metadata(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{
			"metadata": map[string]any{"risk_level": "HIGH"},
		},
		Value:    map[string]any{"message": "x"},
		Source:   permissionInterruptSource,
		ToolName: "bash",
	})
	metadata, ok := got["reviewer_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("reviewer_metadata 类型 = %T", got["reviewer_metadata"])
	}
	if metadata["risk_level"] != "HIGH" {
		t.Errorf("risk_level = %v", metadata["risk_level"])
	}
}

// TestFormatQuestion_确认类不带审查器字段 测试只有权限类附带。
func TestFormatQuestion_确认类不带审查器字段(t *testing.T) {
	got := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction: map[string]any{
			"metadata": map[string]any{"risk_level": "HIGH"},
		},
		Value:    map[string]any{"message": "x"},
		Source:   confirmInterruptSource,
		ToolName: "bash",
	})
	if _, exists := got["reviewer_metadata"]; exists {
		t.Error("确认类中断不应附带 reviewer_metadata")
	}
}

// ──────────────────────────── BuildVerified ────────────────────────────

// TestBuildVerified_request_id匹配 测试正常渲染。
func TestBuildVerified_request_id匹配(t *testing.T) {
	interaction := map[string]any{
		"id": "tc-1",
		"value": map[string]any{
			"message":      "需要授权",
			"interrupt_id": "intr-1",
		},
	}
	payload := BuildVerifiedPermissionAskUserQuestion(interaction, "tc-1", "inv-1")
	if payload == nil {
		t.Fatal("payload = nil, want 渲染结果")
	}
	if payload["event_type"] != "chat.ask_user_question" {
		t.Errorf("event_type = %v", payload["event_type"])
	}
	if payload["request_id"] != "tc-1" {
		t.Errorf("request_id = %v", payload["request_id"])
	}
	if payload["source"] != permissionInterruptSource {
		t.Errorf("source = %v", payload["source"])
	}
	if payload["interrupt_id"] != "intr-1" {
		t.Errorf("interrupt_id = %v", payload["interrupt_id"])
	}
}

// TestBuildVerified_request_id不匹配返回nil 测试身份校验。
//
// 这是权限路径的核心保护：中断快照与队列对不上时不能渲染。
func TestBuildVerified_request_id不匹配返回nil(t *testing.T) {
	interaction := map[string]any{
		"id":    "tc-1",
		"value": map[string]any{"message": "x"},
	}
	if got := BuildVerifiedPermissionAskUserQuestion(interaction, "tc-OTHER", "inv-1"); got != nil {
		t.Errorf("got %v, want nil（request_id 与卡片不符）", got)
	}
}

// TestBuildVerified_questions单条 测试 questions 只含一条。
func TestBuildVerified_questions单条(t *testing.T) {
	interaction := map[string]any{
		"id":    "tc-1",
		"value": map[string]any{"message": "需要授权", "tool_name": "bash"},
	}
	payload := BuildVerifiedPermissionAskUserQuestion(interaction, "tc-1", "inv-1")
	questions, ok := payload["questions"].([]any)
	if !ok {
		t.Fatalf("questions 类型 = %T", payload["questions"])
	}
	if len(questions) != 1 {
		t.Fatalf("len = %d, want 1", len(questions))
	}
	question, _ := questions[0].(map[string]any)
	if question["header"] != "权限审批: bash" {
		t.Errorf("header = %v", question["header"])
	}
	if question["card_id"] != "inv-1" {
		t.Errorf("card_id = %v", question["card_id"])
	}
}

// TestBuildVerified_带kind 测试 kind 字段附带。
func TestBuildVerified_带kind(t *testing.T) {
	interaction := map[string]any{
		"id": "tc-1",
		"value": map[string]any{
			"interrupt_kind": "permission",
		},
	}
	payload := BuildVerifiedPermissionAskUserQuestion(interaction, "tc-1", "inv-1")
	if payload["kind"] != "permission" {
		t.Errorf("kind = %v, want permission", payload["kind"])
	}
}
