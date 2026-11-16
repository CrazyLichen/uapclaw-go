package memberoptimizer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestMechanismType_枚举值完整 逐一对齐 Python schema.py MechanismType 12 成员。
func TestMechanismType_枚举值完整(t *testing.T) {
	want := []string{"prompt", "instruction", "workflow", "skill", "tool", "mcp",
		"memory", "context", "config", "rail", "knowledge", "insufficient_role_evidence"}
	got := make([]string, 0, len(want))
	for _, v := range AllMechanismTypes() {
		got = append(got, string(v))
	}
	require.Equal(t, want, got)
}

// TestOptimizationSurface_枚举值完整 逐一对齐 Python schema.py OptimizationSurface 8 成员。
func TestOptimizationSurface_枚举值完整(t *testing.T) {
	want := []string{"none", "config", "prompt_section", "identity", "soul", "skill", "tool", "rail"}
	got := make([]string, 0, len(want))
	for _, v := range AllOptimizationSurfaces() {
		got = append(got, string(v))
	}
	require.Equal(t, want, got)
}

// TestFailureSignature_枚举值完整 逐一对齐 Python schema.py FailureSignature 10 成员。
func TestFailureSignature_枚举值完整(t *testing.T) {
	want := []string{"repeated_failure_loop", "tool_call_failure", "stalled_objective",
		"missed_exploration_or_capability", "stale_memory_or_context", "skill_code_failure",
		"subagent_or_workflow_handoff_failure", "prompt_instruction_mismatch",
		"config_or_rail_mismatch", "insufficient_role_evidence"}
	got := make([]string, 0, len(want))
	for _, v := range AllFailureSignatures() {
		got = append(got, string(v))
	}
	require.Equal(t, want, got)
}

// TestRoleAttributionReport_YAML往返 snake_case 字段名往返（对齐 Python write_report 字段名）。
func TestRoleAttributionReport_YAML往返(t *testing.T) {
	report := RoleAttributionReport{
		AttributionID:   "attr_1",
		CandidateRoles:  []MemberRoleCandidate{{Role: "solver", HarnessRefPath: "h1"}},
		AssignedRoleIssues: []RoleIssueAttribution{{
			IssueID: "i1", Role: "solver", HarnessRefPath: "h1", Confidence: 0.9,
			Evidence: []map[string]any{{"severity": "high"}},
		}},
		UnassignedIssues: []UnassignedMemberIssue{{IssueID: "i2", Reason: "no role"}},
		Warnings:         []string{"w1"},
	}
	data, err := yaml.Marshal(report)
	require.NoError(t, err)
	require.Contains(t, string(data), "attribution_id")
	require.Contains(t, string(data), "assigned_role_issues")
	require.Contains(t, string(data), "unassigned_issues")
	var round RoleAttributionReport
	require.NoError(t, yaml.Unmarshal(data, &round))
	// nil 切片/map 与空容器 marshal 后等价：以二次 marshal 稳定性代替结构体相等
	data2, err := yaml.Marshal(round)
	require.NoError(t, err)
	require.Equal(t, string(data), string(data2))
}

// TestMechanismAttributionReport_YAML往返 role_mechanisms 按角色分组键往返。
func TestMechanismAttributionReport_YAML往返(t *testing.T) {
	report := MechanismAttributionReport{
		MechanismAttributionID: "mech_1",
		RoleMechanisms: map[string][]RoleMechanismAttribution{
			"solver": {{IssueID: "i1", Role: "solver", MechanismType: "prompt",
				FailureSignature: "prompt_instruction_mismatch", Confidence: 0.8}},
		},
		SkippedRoles: []string{"idle"},
	}
	data, err := yaml.Marshal(report)
	require.NoError(t, err)
	require.Contains(t, string(data), "mechanism_attribution_id")
	require.Contains(t, string(data), "role_mechanisms")
	var round MechanismAttributionReport
	require.NoError(t, yaml.Unmarshal(data, &round))
	// nil 切片/map 与空容器 marshal 后等价：以二次 marshal 稳定性代替结构体相等
	data2, err := yaml.Marshal(round)
	require.NoError(t, err)
	require.Equal(t, string(data), string(data2))
}

// TestMemberSelectionReport_YAML往返 targets/unselected_attributions 往返。
func TestMemberSelectionReport_YAML往返(t *testing.T) {
	report := MemberSelectionReport{
		SelectionID: "member_selection_abcd1234",
		Targets: []MemberOptimizationTarget{{
			Role: "solver", HarnessRefPath: "h1", AttributedIssueIDs: []string{"i1"},
			Confidence: 0.9, MechanismTypes: []string{"prompt"},
		}},
		UnselectedAttributions: []UnselectedAttribution{{IssueID: "i9", Role: "x", Reason: "r", Confidence: 0.4}},
	}
	data, err := yaml.Marshal(report)
	require.NoError(t, err)
	require.Contains(t, string(data), "selection_id")
	require.Contains(t, string(data), "unselected_attributions")
	var round MemberSelectionReport
	require.NoError(t, yaml.Unmarshal(data, &round))
	// nil 切片/map 与空容器 marshal 后等价：以二次 marshal 稳定性代替结构体相等
	data2, err := yaml.Marshal(round)
	require.NoError(t, err)
	require.Equal(t, string(data), string(data2))
}

// TestMemberOptimizationAction_默认值 run_if 默认 dependency_succeeded（Python dataclass 默认）。
func TestMemberOptimizationAction_默认值(t *testing.T) {
	action := NewMemberOptimizationAction("a1", "solver", "prompt", "edit", "instruction", "identity.md")
	require.Equal(t, "dependency_succeeded", action.RunIf)
}

// TestMemberOptimizationArtifact_默认值 promotion_status 默认 not_evaluated；legacy role 字段保留。
func TestMemberOptimizationArtifact_默认值(t *testing.T) {
	artifact := NewMemberOptimizationArtifact()
	require.Equal(t, "not_evaluated", artifact.PromotionStatus)
	require.Equal(t, "", artifact.Role)
}
