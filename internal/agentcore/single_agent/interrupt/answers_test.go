package interrupt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// 本文件对应 Python tests/unit_tests/core/single_agent/test_interrupt_answers.py
// 的 4 个参数化用例（展开后 28 条）。

// boolPtr 取 bool 指针。
func boolPtr(value bool) *bool { return &value }

// strPtr 取 string 指针。
func strPtr(value string) *string { return &value }

// TestInterruptAnswerFor_答案形状 对齐 test_interrupt_answers.py:23-49。
//
// 13 条参数化用例，覆盖两种答案形状（审批类 / skill_evolution）与
// approved 与 text 两条输入路径。
func TestInterruptAnswerFor_答案形状(t *testing.T) {
	cases := []struct {
		name string
		kind string
		opts InterruptAnswerOptions
		want any
	}{
		{
			name: "permission 批准",
			kind: saschema.InterruptKindPermission,
			opts: InterruptAnswerOptions{Approved: boolPtr(true)},
			want: map[string]any{"approved": true, "feedback": "", "auto_confirm": false},
		},
		{
			name: "confirm 批准并记住",
			kind: saschema.InterruptKindConfirm,
			opts: InterruptAnswerOptions{Approved: boolPtr(true), Remember: true},
			want: map[string]any{
				"approved": true, "feedback": "", "auto_confirm": true, "persist_allow": false,
			},
		},
		{
			name: "team_tool_approval 拒绝带反馈",
			kind: saschema.InterruptKindTeamToolApproval,
			opts: InterruptAnswerOptions{Approved: boolPtr(false), Feedback: "no"},
			want: map[string]any{"approved": false, "feedback": "no", "auto_confirm": false},
		},
		{
			name: "skill_evolution 批准",
			kind: saschema.InterruptKindSkillEvolutionApproval,
			opts: InterruptAnswerOptions{Approved: boolPtr(true)},
			want: map[string]any{"action": "allow_once"},
		},
		{
			name: "skill_evolution 文本 always",
			kind: saschema.InterruptKindSkillEvolutionApproval,
			opts: InterruptAnswerOptions{Text: strPtr("always")},
			want: map[string]any{"action": "allow_always"},
		},
		{
			name: "skill_evolution 文本拒绝反馈原文",
			kind: saschema.InterruptKindSkillEvolutionApproval,
			opts: InterruptAnswerOptions{Text: strPtr("not this one")},
			want: map[string]any{"action": "reject", "feedback": "not this one"},
		},
		{
			name: "permission 文本 yes",
			kind: saschema.InterruptKindPermission,
			opts: InterruptAnswerOptions{Text: strPtr("yes")},
			want: map[string]any{"approved": true, "feedback": "", "auto_confirm": false},
		},
		{
			name: "permission 空文本拒绝且反馈为空",
			kind: saschema.InterruptKindPermission,
			opts: InterruptAnswerOptions{Text: strPtr("")},
			want: map[string]any{"approved": false, "feedback": "", "auto_confirm": false},
		},
		{
			name: "ask_user 文本直通",
			kind: saschema.InterruptKindAskUser,
			opts: InterruptAnswerOptions{Text: strPtr("blue")},
			want: "blue",
		},
		{
			name: "undeclared 批准",
			kind: saschema.InterruptKindUndeclared,
			opts: InterruptAnswerOptions{Approved: boolPtr(true)},
			want: map[string]any{"approved": true, "feedback": "", "auto_confirm": false},
		},
		{
			name: "undeclared 文本 a 记住",
			kind: saschema.InterruptKindUndeclared,
			opts: InterruptAnswerOptions{Text: strPtr("a")},
			want: map[string]any{
				"approved": true, "feedback": "", "auto_confirm": true, "persist_allow": false,
			},
		},
		{
			name: "skill_evolution 文本 allow_once",
			kind: saschema.InterruptKindSkillEvolutionApproval,
			opts: InterruptAnswerOptions{Text: strPtr("allow_once")},
			want: map[string]any{"action": "allow_once"},
		},
		{
			name: "skill_evolution 文本 reject 反馈为空",
			kind: saschema.InterruptKindSkillEvolutionApproval,
			opts: InterruptAnswerOptions{Text: strPtr("reject")},
			want: map[string]any{"action": "reject", "feedback": ""},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := InterruptAnswerFor(testCase.kind, testCase.opts)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestInterruptAnswerFor_形状不符被拒 对齐 test_interrupt_answers.py:52-58。
func TestInterruptAnswerFor_形状不符被拒(t *testing.T) {
	cases := []struct {
		name string
		kind string
		opts InterruptAnswerOptions
	}{
		{"ask_user 传 approved", saschema.InterruptKindAskUser, InterruptAnswerOptions{Approved: boolPtr(true)}},
		{"client_tool 传 approved", saschema.InterruptKindClientTool, InterruptAnswerOptions{Approved: boolPtr(false)}},
		{"permission 两个都不传", saschema.InterruptKindPermission, InterruptAnswerOptions{}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := InterruptAnswerFor(testCase.kind, testCase.opts)
			require.Error(t, err)
		})
	}
}

// TestParseApprovalText_批准词 对齐 test_interrupt_answers.py:61-67。
func TestParseApprovalText_批准词(t *testing.T) {
	for _, word := range []string{"y", "Yes", "ok", "approve", "true", "1", "allow", "allow_once"} {
		t.Run(word, func(t *testing.T) {
			approved, remember, feedback := ParseApprovalText(word)
			assert.True(t, approved)
			assert.False(t, remember)
			assert.Equal(t, "", feedback)
		})
	}
}

// TestParseApprovalText_拒绝词 对齐 test_interrupt_answers.py:69-76。
func TestParseApprovalText_拒绝词(t *testing.T) {
	for _, word := range []string{"n", "no", "deny", "reject"} {
		t.Run(word, func(t *testing.T) {
			approved, remember, feedback := ParseApprovalText(word)
			assert.False(t, approved)
			assert.False(t, remember)
			assert.Equal(t, "", feedback)
		})
	}
}

// TestParseApprovalText_总是允许词 覆盖 _ALWAYS_WORDS 三个词。
func TestParseApprovalText_总是允许词(t *testing.T) {
	for _, word := range []string{"a", "always", "allow_always", "Always"} {
		t.Run(word, func(t *testing.T) {
			approved, remember, feedback := ParseApprovalText(word)
			assert.True(t, approved)
			assert.True(t, remember)
			assert.Equal(t, "", feedback)
		})
	}
}

// TestParseApprovalText_未知词当反馈 未知词按去空白后的原文作反馈。
func TestParseApprovalText_未知词当反馈(t *testing.T) {
	approved, remember, feedback := ParseApprovalText("  换个方式  ")
	assert.False(t, approved)
	assert.False(t, remember)
	// Python: word = text.strip()，反馈是去空白后的原文（不是小写形式）
	assert.Equal(t, "换个方式", feedback)
}

// TestIsApprovalKind_五真两假 覆盖 APPROVAL_INTERRUPT_KINDS 的 5 项成员。
func TestIsApprovalKind_五真两假(t *testing.T) {
	for _, kind := range []string{
		saschema.InterruptKindPermission,
		saschema.InterruptKindConfirm,
		saschema.InterruptKindTeamToolApproval,
		saschema.InterruptKindUndeclared,
		saschema.InterruptKindSkillEvolutionApproval,
	} {
		assert.True(t, IsApprovalKind(kind), kind)
	}
	assert.False(t, IsApprovalKind(saschema.InterruptKindAskUser))
	assert.False(t, IsApprovalKind(saschema.InterruptKindClientTool))
	assert.False(t, IsApprovalKind("不存在的类型"))
}

// TestApprovalInterruptKinds_五项 集合大小固定为 5。
func TestApprovalInterruptKinds_五项(t *testing.T) {
	require.Len(t, ApprovalInterruptKinds, 5)
}

// TestApprovalChoices_三项 APPROVAL_CHOICES 的字面量守卫。
func TestApprovalChoices_三项(t *testing.T) {
	assert.Equal(t, []string{"allow", "allow_always", "deny"}, ApprovalChoices)
}

// TestInterruptAnswerFor_反馈优先级 显式 feedback 覆盖文本解析出的反馈。
// Python: answers.py:116 `feedback = feedback or parsed_feedback`
func TestInterruptAnswerFor_反馈优先级(t *testing.T) {
	// 文本是未知词（解析出反馈"乱写"），但显式 feedback 非空 → 用显式值
	got, err := InterruptAnswerFor(saschema.InterruptKindPermission, InterruptAnswerOptions{
		Text:     strPtr("乱写"),
		Feedback: "显式理由",
	})
	require.NoError(t, err)
	value, ok := got.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "显式理由", value["feedback"])
}

// TestInterruptAnswerFor_批准时反馈恒空 批准形状的 feedback 恒为空串。
// Python: answers.py:123 `"feedback": "" if approved else feedback`
func TestInterruptAnswerFor_批准时反馈恒空(t *testing.T) {
	got, err := InterruptAnswerFor(saschema.InterruptKindPermission, InterruptAnswerOptions{
		Approved: boolPtr(true),
		Feedback: "不该出现",
	})
	require.NoError(t, err)
	value, ok := got.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", value["feedback"])
}
