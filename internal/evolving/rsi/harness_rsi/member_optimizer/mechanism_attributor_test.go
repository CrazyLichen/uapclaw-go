package memberoptimizer

import (
	agentface "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/agentface"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

func TestValidateMechanismAttributionOutput_封闭分类与联锁(t *testing.T) {
	// 缺 role
	errs := ValidateMechanismAttributionOutput(map[string]any{"attributions": []any{}}, map[string]bool{"solver": true})
	require.Contains(t, errs, "missing field: role")
	// role 不在 assigned
	errs = ValidateMechanismAttributionOutput(map[string]any{"role": "writer", "attributions": []any{}}, map[string]bool{"solver": true})
	require.Contains(t, errs, "role 'writer' not in assigned roles")
	// attributions 非列表
	errs = ValidateMechanismAttributionOutput(map[string]any{"role": "solver", "attributions": "bad"}, map[string]bool{"solver": true})
	require.Contains(t, errs, "attributions must be a list")
	// attributions 元素非对象
	errs = ValidateMechanismAttributionOutput(map[string]any{"role": "solver", "attributions": []any{"x"}}, map[string]bool{"solver": true})
	require.Contains(t, errs, "attributions[0] must be an object")
	// 缺 issue_id / 未知 mechanism_type / 未知 failure_signature / 未知 surface
	errs = ValidateMechanismAttributionOutput(map[string]any{"role": "solver", "attributions": []any{
		map[string]any{"mechanism_type": "bogus", "failure_signature": "bogus", "optimization_surface": "bogus", "confidence": 0.9},
	}}, map[string]bool{"solver": true})
	require.Contains(t, errs, "attributions[0]: missing issue_id")
	require.Contains(t, errs, "attributions[0]: unknown mechanism_type 'bogus'")
	require.Contains(t, errs, "attributions[0]: unknown failure_signature 'bogus'")
	require.Contains(t, errs, "attributions[0]: unknown optimization_surface 'bogus'")
	// 联锁：surface=none 需要 insufficient_role_evidence
	errs = ValidateMechanismAttributionOutput(map[string]any{"role": "solver", "attributions": []any{
		map[string]any{"issue_id": "i1", "mechanism_type": "prompt", "failure_signature": "tool_call_failure", "optimization_surface": "none", "confidence": 0.9},
	}}, map[string]bool{"solver": true})
	require.Contains(t, errs, "attributions[0]: optimization_surface 'none' requires insufficient_role_evidence mechanism and signature")
	// 联锁合法路径
	errs = ValidateMechanismAttributionOutput(map[string]any{"role": "solver", "attributions": []any{
		map[string]any{"issue_id": "i1", "mechanism_type": "insufficient_role_evidence", "failure_signature": "insufficient_role_evidence", "optimization_surface": "none", "confidence": 0.9},
	}}, map[string]bool{"solver": true})
	require.Empty(t, errs)
	// confidence 越界/非数字
	errs = ValidateMechanismAttributionOutput(map[string]any{"role": "solver", "attributions": []any{
		map[string]any{"issue_id": "i1", "mechanism_type": "prompt", "failure_signature": "tool_call_failure", "confidence": 2.0},
	}}, map[string]bool{"solver": true})
	require.Contains(t, errs, "attributions[0]: confidence 2 out of range")
	errs = ValidateMechanismAttributionOutput(map[string]any{"role": "solver", "attributions": []any{
		map[string]any{"issue_id": "i1", "mechanism_type": "prompt", "failure_signature": "tool_call_failure", "confidence": "high"},
	}}, map[string]bool{"solver": true})
	require.Contains(t, errs, "attributions[0]: confidence is not a number")
}

func TestSummarizeTrainingSignal(t *testing.T) {
	require.Equal(t, "none", SummarizeTrainingSignal("bad"))
	require.Equal(t, "none", SummarizeTrainingSignal(map[string]any{}))
	got := SummarizeTrainingSignal(map[string]any{"training_signal": map[string]any{
		"expected_failure_modes": []any{"loop"},
		"capability_gap":         "no skill",
		"target_surfaces":        []any{"skill"},
		"difficulty_rationale":   "hard",
	}})
	require.Contains(t, got, "'expected_failure_modes': ['loop']")
	require.Contains(t, got, "'capability_gap': 'no skill'")
}

func TestSummarizeRoleIssueAttribution(t *testing.T) {
	require.Equal(t, "none", SummarizeRoleIssueAttribution([]map[string]any{
		{"attribution": "not-a-dict"},
	}))
	got := SummarizeRoleIssueAttribution([]map[string]any{
		{"attribution": map[string]any{"target_ref": "identity.md", "root_cause": "vague", "evidence_refs": []any{"e1"}}},
	})
	require.Contains(t, got, "'target_ref': 'identity.md'")
	require.Contains(t, got, "'root_cause': 'vague'")
}

func TestBuildMechanismUserMessage(t *testing.T) {
	roleIssues := []RoleIssueAttribution{{
		IssueID: "i1", Confidence: 0.9, Rationale: "clear",
		Evidence: []map[string]any{{"summary": "s1", "attribution": map[string]any{"target_ref": "t"}}},
	}}
	caseEvidence := map[string]map[string]map[string]any{
		"c1": {
			"result": {"status": "failed", "response": strings.Repeat("x", 400), "metadata": map[string]any{}},
			"trace":  {"evaluation": "bad", "behavior_trace": map[string]any{}},
		},
	}
	msg := BuildMechanismAttributionUserMessage("solver", roleIssues, caseEvidence, "Role: solver, Harness: h/s")
	require.Contains(t, msg, "## Role to Diagnose\n\nrole: solver")
	require.Contains(t, msg, "Number of attributed issues: 1")
	require.Contains(t, msg, "  - issue_id: i1")
	require.Contains(t, msg, "evidence_summary: s1")
	require.Contains(t, msg, "## Case Evidence (Bounded Excerpts)")
	require.Contains(t, msg, "  Case c1:")
	require.Contains(t, msg, "response: "+strings.Repeat("x", 300)) // Python [:300] 截断
	require.Contains(t, msg, "training_signal: none")
	require.Contains(t, msg, "## Role Harness Summary")
	require.Contains(t, msg, "## Output")
}

func TestMechanismAttributorAgentBuildRetryMessage(t *testing.T) {
	msg := MechanismAttributorAgentBuildRetryMessage("ORIGINAL", "bad output")
	require.True(t, strings.HasPrefix(msg, "ORIGINAL\n\n"))
	require.Contains(t, msg, "## Validation Error\n\nbad output")
	require.Contains(t, msg, "Please correct the JSON output and return a valid response.")
}

func TestMechanismAttributorAgent_AttributeRoleMechanisms_成功(t *testing.T) {
	fake := newFakeMechAgent(map[string][]string{"solver": {
		`{"role": "solver", "attributions": [{"issue_id": "i1", "mechanism_type": "prompt", "failure_signature": "prompt_instruction_mismatch", "confidence": 0.8, "optimization_surface": "identity", "rationale": "r"}]}`,
	}})
	withMechSeam(t, fake)
	agent := NewMechanismAttributorAgentWithAgent("", 2)
	roleIssues := []RoleIssueAttribution{{IssueID: "i1", Role: "solver", Confidence: 0.9}}
	attributions, err := agent.AttributeRoleMechanisms(context.Background(), "solver", roleIssues, map[string]map[string]map[string]any{}, "summary")
	require.NoError(t, err)
	require.Len(t, attributions, 1)
	a := attributions[0]
	require.Equal(t, "i1", a.IssueID)
	require.Equal(t, "solver", a.Role)
	require.Equal(t, "prompt", a.MechanismType)
	require.Equal(t, "prompt_instruction_mismatch", a.FailureSignature)
	require.Equal(t, 0.8, a.Confidence)
	require.Equal(t, "identity", a.OptimizationSurface)
}

func TestMechanismAttributor_Attribute_编排全链(t *testing.T) {
	fake := newFakeMechAgent(map[string][]string{"solver": {
		`{"role": "solver", "attributions": [{"issue_id": "i1", "mechanism_type": "skill", "failure_signature": "skill_code_failure", "confidence": 0.7}]}`,
	}})
	withMechSeam(t, fake)
	report := &RoleAttributionReport{
		AssignedRoleIssues: []RoleIssueAttribution{{IssueID: "i1", Role: "solver", TraceRefs: []map[string]string{}}},
	}
	candidates := []MemberRoleCandidate{{Role: "solver", HarnessRefPath: "h/s", Description: "d"}}
	attr := NewMechanismAttributorWithAgent(NewMechanismAttributorAgentWithAgent("", 2), 2)
	out, err := attr.Attribute(context.Background(), MechanismAttributeParams{
		RoleAttributionReport: report,
		EvalRef:               &EvalRef{EvalDir: t.TempDir()},
		CandidateRoles:        candidates,
	})
	require.NoError(t, err)
	require.Contains(t, out.MechanismAttributionID, "mechanism_attribution_")
	require.Len(t, out.RoleMechanisms["solver"], 1)
	require.Empty(t, out.SkippedRoles)
	require.Equal(t, []any{"solver"}, out.Metadata["roles_processed"])
}

func TestMechanismAttributor_Attribute_无已定责角色(t *testing.T) {
	attr := NewMechanismAttributorWithAgent(nil, 2)
	out, err := attr.Attribute(context.Background(), MechanismAttributeParams{
		RoleAttributionReport: &RoleAttributionReport{},
		EvalRef:               &EvalRef{EvalDir: t.TempDir()},
	})
	require.NoError(t, err)
	require.Empty(t, out.RoleMechanisms)
	require.Empty(t, out.SkippedRoles)
	require.Equal(t, []string{"No assigned roles to process"}, out.Warnings)
	require.Contains(t, out.MechanismAttributionID, "mechanism_attribution_")
	require.Equal(t, "", out.Metadata["model_config_ref"])
}

func TestMechanismAttributor_Attribute_agent失败包装(t *testing.T) {
	fake := newFakeMechAgent(map[string][]string{"solver": {"bad", "bad", "bad"}})
	withMechSeam(t, fake)
	report := &RoleAttributionReport{
		AssignedRoleIssues: []RoleIssueAttribution{{IssueID: "i1", Role: "solver"}},
	}
	attr := NewMechanismAttributorWithAgent(NewMechanismAttributorAgentWithAgent("", 0), 2)
	_, err := attr.Attribute(context.Background(), MechanismAttributeParams{
		RoleAttributionReport: report,
		EvalRef:               &EvalRef{EvalDir: t.TempDir()},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Mechanism attribution task failed:")
}

func TestBuildHarnessSummary(t *testing.T) {
	refs := map[string]MemberRoleCandidate{"solver": {Role: "solver", HarnessRefPath: "h/s", Description: "d"}}
	require.Equal(t, "Role: solver, Harness: h/s, Description: d", BuildHarnessSummary("solver", nil, refs))
	require.Equal(t, "Role: ghost (no harness ref available)", BuildHarnessSummary("ghost", nil, refs))
}

func TestWriteMechanismAttributionReport_YAML字段(t *testing.T) {
	report := &MechanismAttributionReport{
		MechanismAttributionID:    "mechanism_attribution_abcd1234",
		SourceRoleAttributionPath: "/tmp/role_attribution.yaml",
		RoleMechanisms: map[string][]RoleMechanismAttribution{
			"solver": {{IssueID: "i1", Role: "solver", MechanismType: "prompt", FailureSignature: "prompt_instruction_mismatch", Confidence: 0.8, OptimizationSurface: "identity", Evidence: []map[string]any{{"k": "v"}}, EvidenceRefs: []map[string]string{{"case_id": "c1"}}, Rationale: "r"}},
		},
		SkippedRoles: []string{},
		Warnings:     []string{},
		Metadata:     map[string]any{"roles_processed": []string{"solver"}},
	}
	path, err := WriteMechanismAttributionReport(report, t.TempDir())
	require.NoError(t, err)
	data := readFileForTest(t, path)
	for _, want := range []string{
		"mechanism_attribution_id: mechanism_attribution_abcd1234",
		"source_role_attribution_path: /tmp/role_attribution.yaml",
		"role_mechanisms:",
		"mechanism_type: prompt",
		"failure_signature: prompt_instruction_mismatch",
		"optimization_surface: identity",
		"skipped_roles: []",
		"warnings: []",
	} {
		require.Contains(t, string(data), want, "YAML 缺字段: "+want)
	}
}

// fakeMechAgent 按 role 分发的假评委。
type fakeMechAgent struct {
	responses map[string][]string
}

func newFakeMechAgent(responses map[string][]string) *fakeMechAgent {
	return &fakeMechAgent{responses: responses}
}

// withMechSeam 覆盖能力桥（同 role_attributor 测试惯例）。
func withMechSeam(t *testing.T, fake *fakeMechAgent) {
	t.Helper()
	saved := agentsBridge
	agentsBridge.InvokeStructured = func(_ context.Context, params StructuredInvokeParams) (map[string]any, error) {
		role := extractRoleFromMessage(params.UserMessage)
		texts := fake.responses[role]
		attempt := 0
		for ; attempt < len(texts); attempt++ {
			raw, err := testParseYamlOrJSON(texts[attempt])
			if err == nil {
				return raw, nil
			}
		}
		return nil, &StructuredOutputExhaustedError{
			Msg: fmt.Sprintf("MechanismAttributorAgent[%s] failed after %d attempts (attempt_limit_reached)", role, attempt),
		}
	}
	agentsBridge.ParseYamlOrJSONObject = testParseYamlOrJSON
	agentsBridge.CreateMechanismAttributionAgent = func(context.Context, AttributionAgentParams) (*agentface.Agent, error) {
		return &agentface.Agent{}, nil
	}
	t.Cleanup(func() { agentsBridge = saved })
}

// extractRoleFromMessage 从用户消息提取 role（"role: solver" 行）。
func extractRoleFromMessage(msg string) string {
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, "role: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "role: "))
		}
	}
	return ""
}

// testParseYamlOrJSON 测试用 YAML/JSON 对象解析（等价 agents.ParseYamlOrJsonObjectResponse）。
func testParseYamlOrJSON(text string) (map[string]any, error) {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		return testParseJSONObject(trimmed)
	}
	return yamlUnmarshalMappingForTest(trimmed)
}

var _ = harnessrsi.DefaultOutputRepairConfig
