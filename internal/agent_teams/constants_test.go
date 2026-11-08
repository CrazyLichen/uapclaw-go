package agent_teams

import "testing"

// TestReservedMemberNames_包含 验证三个保留名都在集合中。
func TestReservedMemberNames_包含(t *testing.T) {
	for _, name := range []string{HumanAgentMemberName, UserPseudoMemberName, DefaultLeaderMemberName} {
		if !ReservedMemberNames[name] {
			t.Errorf("ReservedMemberNames[%q] = false, want true", name)
		}
	}
}

// TestReservedMemberNames_不包含 验证普通名不在集合中。
func TestReservedMemberNames_不包含(t *testing.T) {
	if ReservedMemberNames["developer"] {
		t.Error("ReservedMemberNames[\"developer\"] = true, want false")
	}
}

// TestConstantValues 验证常量值与 Python 一致。
func TestConstantValues(t *testing.T) {
	if HumanAgentMemberName != "human_agent" {
		t.Errorf("HumanAgentMemberName = %q, want %q", HumanAgentMemberName, "human_agent")
	}
	if UserPseudoMemberName != "user" {
		t.Errorf("UserPseudoMemberName = %q, want %q", UserPseudoMemberName, "user")
	}
	if DefaultLeaderMemberName != "team_leader" {
		t.Errorf("DefaultLeaderMemberName = %q, want %q", DefaultLeaderMemberName, "team_leader")
	}
	if len(ReservedMemberNames) != 3 {
		t.Errorf("len(ReservedMemberNames) = %d, want 3", len(ReservedMemberNames))
	}
}

// TestInlineTaskMemberEnabled_默认关闭 验证任务级内联成员硬开关默认关闭。
// Python: INLINE_TASK_MEMBER_ENABLED = False (agent_teams/constants.py L36)
func TestInlineTaskMemberEnabled_默认关闭(t *testing.T) {
	if InlineTaskMemberEnabled {
		t.Fatal("InlineTaskMemberEnabled 必须为 false（上游本版本关闭 F_118）")
	}
}
