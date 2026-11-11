package interrupt

import (
	"testing"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── InterruptKind / Source ────────────────────────────

// TestInterruptKindOfValue_permission 测试 permission 类型直接读出。
func TestInterruptKindOfValue_permission(t *testing.T) {
	value := map[string]any{"interrupt_kind": saschema.InterruptKindPermission}
	if got := interruptKindOfValue(value); got != saschema.InterruptKindPermission {
		t.Errorf("got %q, want %q", got, saschema.InterruptKindPermission)
	}
}

// TestInterruptKindOfValue_metadata回落 测试从 metadata 读 kind（历史形态）。
//
// Python: _interrupt_kind 先读字段，再回落 metadata["interrupt_kind"]
func TestInterruptKindOfValue_metadata回落(t *testing.T) {
	value := map[string]any{
		"metadata": map[string]any{"interrupt_kind": saschema.InterruptKindSkillEvolutionApproval},
	}
	if got := interruptKindOfValue(value); got != saschema.InterruptKindSkillEvolutionApproval {
		t.Errorf("got %q, want 从 metadata 回落", got)
	}
}

// TestInterruptKindOfValue_未声明返回空串 测试空值。
func TestInterruptKindOfValue_未声明返回空串(t *testing.T) {
	if got := interruptKindOfValue(map[string]any{}); got != "" {
		t.Errorf("got %q, want 空串", got)
	}
	if got := interruptKindOfValue(nil); got != "" {
		t.Errorf("nil got %q, want 空串", got)
	}
}

// TestInterruptKindOfValue_非字符串返回空串 测试类型兜底。
func TestInterruptKindOfValue_非字符串返回空串(t *testing.T) {
	if got := interruptKindOfValue(map[string]any{"interrupt_kind": 123}); got != "" {
		t.Errorf("got %q, want 空串（非字符串）", got)
	}
}

// TestResolveInterruptSource_permission 测试 permission kind 直接映射。
func TestResolveInterruptSource_permission(t *testing.T) {
	value := map[string]any{"interrupt_kind": saschema.InterruptKindPermission}
	if got := resolveInterruptSource(value); got != "permission_interrupt" {
		t.Errorf("got %q, want permission_interrupt", got)
	}
}

// TestResolveInterruptSource_confirm 测试 confirm kind 直接映射。
func TestResolveInterruptSource_confirm(t *testing.T) {
	value := map[string]any{"interrupt_kind": saschema.InterruptKindConfirm}
	if got := resolveInterruptSource(value); got != "confirm_interrupt" {
		t.Errorf("got %q, want confirm_interrupt", got)
	}
}

// TestResolveInterruptSource_无kind按措辞判定 测试回落分支。
//
// Python: kind 读不到时，按 message/tool_name 措辞判断是否权限中断。
// 命中 _PERMISSION_INTERRUPT_MARKERS 的特征词才算权限中断。
func TestResolveInterruptSource_无kind按措辞判定(t *testing.T) {
	cases := []string{
		"操作需要授权",
		"检测到受保护的文件路径访问",
		"检测到风险命令结构",
		"安全风险评估",
	}
	for _, message := range cases {
		value := map[string]any{"message": message}
		if got := resolveInterruptSource(value); got != "permission_interrupt" {
			t.Errorf("%q got %q, want permission_interrupt", message, got)
		}
	}
}

// TestResolveInterruptSource_无kind含工具名前缀 测试 "**工具 `" 开头判为权限。
func TestResolveInterruptSource_无kind含工具名前缀(t *testing.T) {
	value := map[string]any{"message": "**工具 `bash` 需要审批"}
	if got := resolveInterruptSource(value); got != "permission_interrupt" {
		t.Errorf("got %q, want permission_interrupt（**工具 ` 开头）", got)
	}
}

// TestResolveInterruptSource_无kind非白名单工具 测试 tool_name 非确认白名单。
func TestResolveInterruptSource_无kind非白名单工具(t *testing.T) {
	value := map[string]any{"tool_name": "bash", "message": "随便什么文案"}
	if got := resolveInterruptSource(value); got != "permission_interrupt" {
		t.Errorf("got %q, want permission_interrupt（tool_name 非白名单）", got)
	}
}

// TestResolveInterruptSource_无kind空文案白名单工具 测试确认类工具空文案。
func TestResolveInterruptSource_无kind空文案白名单工具(t *testing.T) {
	value := map[string]any{"tool_name": "switch_mode"}
	if got := resolveInterruptSource(value); got != "confirm_interrupt" {
		t.Errorf("got %q, want confirm_interrupt（switch_mode 在确认白名单）", got)
	}
}

// TestResolveInterruptSource_无kind空文案非白名单 测试空文案 + 非白名单。
func TestResolveInterruptSource_无kind空文案非白名单(t *testing.T) {
	value := map[string]any{"tool_name": "bash"}
	if got := resolveInterruptSource(value); got != "permission_interrupt" {
		t.Errorf("got %q, want permission_interrupt（空文案 + 非白名单工具）", got)
	}
}

// TestResolveInterruptSource_无kind普通措辞 测试回落确认类。
func TestResolveInterruptSource_无kind普通措辞(t *testing.T) {
	value := map[string]any{"message": "确认继续吗"}
	if got := resolveInterruptSource(value); got != "confirm_interrupt" {
		t.Errorf("got %q, want confirm_interrupt", got)
	}
}

// TestResolveInterruptSource_askUser不映射表 测试 ask_user 不在 _SOURCE_BY_KIND。
//
// Python: _SOURCE_BY_KIND 只有 PERMISSION 与 CONFIRM 两项。ask_user 查表失败
// 后走措辞判断：空文案 + 空 tool_name 时，第四条 `return tool_name not in
// _CONFIRM_INTERRUPT_TOOLS` → "" not in {"switch_mode","exit_plan_mode"} → True
// → permission_interrupt。这是 Python 的真实行为（safe-by-default 方向：
// 无法判定的中断按权限中断处理）。
func TestResolveInterruptSource_askUser不映射表(t *testing.T) {
	value := map[string]any{"interrupt_kind": saschema.InterruptKindAskUser}
	got := resolveInterruptSource(value)
	if got != "permission_interrupt" {
		t.Errorf("got %q, want permission_interrupt（空文案+空工具名回落为权限中断）", got)
	}
}

// ──────────────────────────── InterruptIdentity ────────────────────────────

// TestInterruptIdentity_两字段 测试返回 interrupt_id + kind。
func TestInterruptIdentity_两字段(t *testing.T) {
	value := map[string]any{
		"interrupt_id":   "intr_abc",
		"interrupt_kind": saschema.InterruptKindPermission,
	}
	got := interruptIdentity(value)
	if got["interrupt_id"] != "intr_abc" {
		t.Errorf("interrupt_id = %v", got["interrupt_id"])
	}
	if got["kind"] != saschema.InterruptKindPermission {
		t.Errorf("kind = %v", got["kind"])
	}
}

// TestInterruptIdentity_空值 测试缺字段时返回空串。
func TestInterruptIdentity_空值(t *testing.T) {
	got := interruptIdentity(map[string]any{})
	if got["interrupt_id"] != "" {
		t.Errorf("interrupt_id = %v, want 空串", got["interrupt_id"])
	}
	if got["kind"] != "" {
		t.Errorf("kind = %v, want 空串", got["kind"])
	}
}

// ──────────────────────────── QuestionInteractions ────────────────────────────

// TestQuestionInteractions_过滤clientTool 测试 client_tool 被排除。
//
// Python: A client tool call is not a question: it has its own
// chat.client_tool_request event.
func TestQuestionInteractions_过滤clientTool(t *testing.T) {
	inputs := []map[string]any{
		{"id": "r1", "value": map[string]any{"interrupt_kind": saschema.InterruptKindPermission}},
		{"id": "r2", "value": map[string]any{"interrupt_kind": saschema.InterruptKindClientTool}},
		{"id": "r3", "value": map[string]any{"interrupt_kind": saschema.InterruptKindAskUser}},
	}
	got := QuestionInteractions(inputs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2（client_tool 应被过滤）", len(got))
	}
	if got[0]["id"] != "r1" || got[1]["id"] != "r3" {
		t.Errorf("顺序或内容错误: %v", got)
	}
}

// TestQuestionInteractions_空输入 测试空列表。
func TestQuestionInteractions_空输入(t *testing.T) {
	if got := QuestionInteractions(nil); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestQuestionInteractions_全部clientTool 测试全过滤后为空。
func TestQuestionInteractions_全部clientTool(t *testing.T) {
	inputs := []map[string]any{
		{"id": "r1", "value": map[string]any{"interrupt_kind": saschema.InterruptKindClientTool}},
	}
	if got := QuestionInteractions(inputs); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
