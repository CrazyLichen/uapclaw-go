package memberoptimizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

func sevAttribution(conf float64, severity string) RoleIssueAttribution {
	return RoleIssueAttribution{
		IssueID:   "i-" + severity,
		Role:      "solver",
		Confidence: conf,
		Evidence:  []map[string]any{{"severity": severity}},
	}
}

func TestMemberSelector_Select_评分与排序(t *testing.T) {
	// A: critical×2 conf 0.9 → score = (4+4)*10 + 0.9*5 + 2/5 = 84.9
	// B: medium×1 conf 0.6 → score = 2*10 + 0.6*5 + 1/5 = 23.2
	roleReport := &RoleAttributionReport{
		AssignedRoleIssues: []RoleIssueAttribution{
			sevAttribution(0.9, "critical"),
			{IssueID: "i-crit2", Role: "solver", Confidence: 0.9, Evidence: []map[string]any{{"severity": "critical"}}},
			{IssueID: "i-med", Role: "reviewer", Confidence: 0.6, Evidence: []map[string]any{{"severity": "medium"}}},
		},
		CandidateRoles: []MemberRoleCandidate{
			{Role: "solver", MemberName: "Main", HarnessRefPath: "h/s"},
			{Role: "reviewer", MemberName: "Check", HarnessRefPath: "h/r"},
		},
	}
	mechReport := &MechanismAttributionReport{
		RoleMechanisms: map[string][]RoleMechanismAttribution{
			"solver":   {{IssueID: "i-critical", MechanismType: "prompt"}, {IssueID: "i-crit2", MechanismType: "skill"}},
			"reviewer": {{IssueID: "i-medium", MechanismType: "config"}},
		},
	}
	report := SelectMemberTargets(roleReport, mechReport, SelectorOptions{})
	require.Len(t, report.Targets, 2)
	a := report.Targets[0]
	require.Equal(t, "solver", a.Role)
	require.Equal(t, "h/s", a.HarnessRefPath)
	require.Equal(t, "Main", a.MemberName)
	require.InDelta(t, 0.9, a.Confidence, 0.01) // confidence 字段承载 avg_confidence（Python MemberOptimizationTarget.confidence）
	require.Contains(t, a.Reason, "Score=84.90")
	require.Contains(t, a.Reason, "2 issue(s) attributed with avg_confidence=0.90")
	require.Contains(t, a.Reason, "Mechanisms: prompt, skill")
	require.Equal(t, []string{"i-critical", "i-crit2"}, a.AttributedIssueIDs)
	require.Len(t, report.UnselectedAttributions, 0)
}

func TestMemberSelector_置信度门槛与max_roles(t *testing.T) {
	roleReport := &RoleAttributionReport{
		AssignedRoleIssues: []RoleIssueAttribution{
			{IssueID: "i-low", Role: "solver", Confidence: 0.3, Evidence: []map[string]any{{"severity": "high"}}},
			{IssueID: "i-ok1", Role: "reviewer", Confidence: 0.8, Evidence: []map[string]any{{"severity": "high"}}},
			{IssueID: "i-ok2", Role: "planner", Confidence: 0.8, Evidence: []map[string]any{{"severity": "high"}}},
			{IssueID: "i-ok3", Role: "executor", Confidence: 0.8, Evidence: []map[string]any{{"severity": "high"}}},
		},
		CandidateRoles: []MemberRoleCandidate{
			{Role: "solver", HarnessRefPath: "h/s"},
			{Role: "reviewer", HarnessRefPath: "h/r"},
			{Role: "planner", HarnessRefPath: "h/p"},
			{Role: "executor", HarnessRefPath: "h/e"},
		},
	}
	report := SelectMemberTargets(roleReport, &MechanismAttributionReport{RoleMechanisms: map[string][]RoleMechanismAttribution{}}, SelectorOptions{})
	// conf 0.3 < 0.5 剔除；max_roles_per_run=2 → 只取前 2
	require.Len(t, report.Targets, 2)
	// 三角色同分 → role↑ 决胜（executor/planner 入选，reviewer 落选）
	require.Len(t, report.UnselectedAttributions, 1)
	require.Equal(t, "i-ok1", report.UnselectedAttributions[0].IssueID)
	require.Equal(t, "reviewer", report.UnselectedAttributions[0].Role)
	require.Equal(t, "Lower score than selected targets for this run", report.UnselectedAttributions[0].Reason)
	require.Equal(t, 0, report.Metadata["filtered_insufficient_role_evidence_count"]) // 0.3 conf 问题仅被门槛剔除，不计入 insufficient 过滤
}

func TestMemberSelector_证据不足剔除改道记账(t *testing.T) {
	roleReport := &RoleAttributionReport{
		AssignedRoleIssues: []RoleIssueAttribution{
			{IssueID: "i-thin", Role: "solver", Confidence: 0.9, Evidence: []map[string]any{{"severity": "high"}}},
			{IssueID: "i-good", Role: "solver", Confidence: 0.8, Evidence: []map[string]any{{"severity": "high"}}},
		},
		CandidateRoles: []MemberRoleCandidate{{Role: "solver", HarnessRefPath: "h/s"}},
	}
	mechReport := &MechanismAttributionReport{
		RoleMechanisms: map[string][]RoleMechanismAttribution{
			"solver": {
				{IssueID: "i-thin", MechanismType: "insufficient_role_evidence", FailureSignature: "insufficient_role_evidence"},
				{IssueID: "i-good", MechanismType: "prompt"},
			},
		},
	}
	report := SelectMemberTargets(roleReport, mechReport, SelectorOptions{})
	require.Len(t, report.Targets, 1)
	// i-thin 被剔除，只保留 i-good
	require.Equal(t, []string{"i-good"}, report.Targets[0].AttributedIssueIDs)
	require.Equal(t, 1, report.Metadata["filtered_insufficient_role_evidence_count"])
	require.Equal(t, []any{"i-thin"}, report.Metadata["deferred_contract_issue_ids"])
	require.Equal(t, "team_skill", report.Metadata["deferred_contract_route"])
}

func TestMemberSelector_证据不足为唯一问题时整角色不入选(t *testing.T) {
	roleReport := &RoleAttributionReport{
		AssignedRoleIssues: []RoleIssueAttribution{
			{IssueID: "i-thin", Role: "solver", Confidence: 0.9, Evidence: []map[string]any{{"severity": "high"}}},
		},
		CandidateRoles: []MemberRoleCandidate{{Role: "solver", HarnessRefPath: "h/s"}},
	}
	mechReport := &MechanismAttributionReport{
		RoleMechanisms: map[string][]RoleMechanismAttribution{
			"solver": {{IssueID: "i-thin", MechanismType: "insufficient_role_evidence"}},
		},
	}
	report := SelectMemberTargets(roleReport, mechReport, SelectorOptions{})
	require.Empty(t, report.Targets)
	require.Len(t, report.UnselectedAttributions, 1)
	require.Contains(t, report.UnselectedAttributions[0].Reason, "insufficient_role_evidence")
	require.Equal(t, "deterministic_priority_selector", report.Metadata["selector"])
}

func TestMemberSelector_严重度未知默认1(t *testing.T) {
	roleReport := &RoleAttributionReport{
		AssignedRoleIssues: []RoleIssueAttribution{
			{IssueID: "i1", Role: "solver", Confidence: 0.8, Evidence: []map[string]any{{"severity": "bogus"}}},
		},
		CandidateRoles: []MemberRoleCandidate{{Role: "solver", HarnessRefPath: "h/s"}},
	}
	report := SelectMemberTargets(roleReport, &MechanismAttributionReport{RoleMechanisms: map[string][]RoleMechanismAttribution{}}, SelectorOptions{})
	require.Len(t, report.Targets, 1)
	// score = 1*10 + 0.8*5 + 1/5 = 14.2
	require.Contains(t, report.Targets[0].Reason, "Score=14.20")
}

func TestMemberSelector_WriteReport_YAML字段(t *testing.T) {
	report := &MemberSelectionReport{
		SelectionID:                   "member_selection_abcd1234",
		SourceRoleAttributionPath:     "/tmp/role_attribution.yaml",
		SourceMechanismAttributionPath: "/tmp/mechanism_attribution.yaml",
		Targets: []MemberOptimizationTarget{{
			Role: "solver", MemberName: "Main", HarnessRefPath: "h/s",
			AttributedIssueIDs: []string{"i1"}, Confidence: 0.8,
			Reason:             "Score=84.90. 2 issue(s)...",
			EvidenceRefs:       []map[string]string{{"issue_id": "i1"}},
			MechanismTypes:     []string{"prompt"},
			OptimizationSurfaces: []string{"identity"},
		}},
		UnselectedAttributions: []UnselectedAttribution{{IssueID: "i2", Role: "solver", Reason: "r", Confidence: 0.6}},
		Metadata:               map[string]any{"selector": "deterministic_priority_selector"},
	}
	path, err := WriteMemberSelectionReport(report, t.TempDir())
	require.NoError(t, err)
	require.Equal(t, filepath.Join("member_selection.yaml"), filepath.Base(path))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, want := range []string{
		"selection_id: member_selection_abcd1234",
		"source_role_attribution_path: /tmp/role_attribution.yaml",
		"source_mechanism_attribution_path: /tmp/mechanism_attribution.yaml",
		"targets:",
		"member_name: Main",
		"attributed_issue_ids:",
		"- i1",
		"mechanism_types:",
		"- prompt",
		"unselected_attributions:",
		"metadata:",
		"selector: deterministic_priority_selector",
	} {
		require.Contains(t, string(data), want, "YAML 缺字段: "+want)
	}
}

var _ = harnessrsi.DefaultOutputRepairConfig
