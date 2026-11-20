package schema

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestProtocolConstants(t *testing.T) {
	// 一比一验证与 Python protocols.py 常量值一致
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"ApproveAction", ApproveAction, "approve"},
		{"AppendMode", AppendMode, "append"},
		{"ConversationReviewSignal", ConversationReviewSignal, "conversation_review"},
		{"ExecutionFailureSignal", ExecutionFailureSignal, "execution_failure"},
		{"ExperiencesTarget", ExperiencesTarget, "experiences"},
		{"ExperienceEntry", ExperienceEntry, "experience_entry"},
		{"LocalApplyCompleted", LocalApplyCompleted, "local_apply_completed"},
		{"MergeMode", MergeMode, "merge"},
		{"PendingChangeEffect", PendingChangeEffect, "pending_change"},
		{"RejectAction", RejectAction, "reject"},
		{"ReplaceMode", ReplaceMode, "replace"},
		{"RetryAction", RetryAction, "retry"},
		{"SkillExperienceEntry", SkillExperienceEntry, "skill_experience_entry"},
		{"StateEffect", StateEffect, "state"},
		{"ToolFailureSignal", ToolFailureSignal, "tool_failure"},
		{"TrajectoryIssueSignal", TrajectoryIssueSignal, "trajectory_issue"},
		{"UserIntentSignal", UserIntentSignal, "user_intent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}

func TestValidPatchActions(t *testing.T) {
	expected := []string{"append", "merge", "replace", "skip"}
	for _, action := range expected {
		if !ValidPatchActions[action] {
			t.Errorf("ValidPatchActions missing %q", action)
		}
	}
	if ValidPatchActions["invalid"] {
		t.Error("ValidPatchActions should not contain 'invalid'")
	}
}

func TestValidSections(t *testing.T) {
	expected := []string{"Instructions", "Examples", "Troubleshooting", "Scripts",
		"Collaboration", "Roles", "Constraints", "Workflow"}
	for _, section := range expected {
		if !ValidSections[section] {
			t.Errorf("ValidSections missing %q", section)
		}
	}
	if ValidSections["Invalid"] {
		t.Error("ValidSections should not contain 'Invalid'")
	}
}
