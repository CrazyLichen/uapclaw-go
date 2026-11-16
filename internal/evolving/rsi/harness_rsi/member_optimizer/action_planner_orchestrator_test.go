package memberoptimizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)



func TestMemberActionPlanner_Plan_编排全链(t *testing.T) {
	planData := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {})}
		p["action_waves"] = []any{[]any{"a1"}}
	})
	agent := &MemberActionPlannerAgent{prebuiltAgent: nil}
	// 注入 fake：替换 agentsBridge.InvokeStructured（同 c1 惯例）
	saved := agentsBridge
	agentsBridge.InvokeStructured = func(_ context.Context, params StructuredInvokeParams) (map[string]any, error) {
		return planData, nil
	}
	agentsBridge.ParseYamlOrJSONObject = testParseJSONObject
	t.Cleanup(func() { agentsBridge = saved })
	// prebuilt agent 注入点绕过工厂
	agent.prebuiltAgent = "fake"
	// holder 断言跳过（fake 非 holder）
	planner := NewMemberActionPlanner(agent, nil, nil)
	plan, err := planner.Plan(context.Background(), PlanParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{AssignedRoleIssues: []RoleIssueAttribution{{IssueID: "i1", Role: "solver"}}},
		MechanismAttributionReport: &MechanismAttributionReport{},
		ModelConfigRef:             "",
	})
	require.NoError(t, err)
	require.Contains(t, plan.PlanID, "member_plan_")
	require.Len(t, plan.Actions, 1)
	require.Equal(t, [][]string{{"a1"}}, plan.ActionWaves)
	require.Equal(t, true, plan.Metadata["validated"])
	require.Equal(t, "attribution_pipeline", plan.Metadata["semantic_authority"])
	// allowed_tools 直通（bash 会被策略校验拒绝——Python 同语义，sanitize 仅在构造期）
	planData["actions"] = []any{mkAction(func(m map[string]any) {
		m["allowed_tools"] = []any{"read_file", "edit_file"}
	})}
	plan, err = planner.Plan(context.Background(), PlanParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"read_file", "edit_file"}, plan.Actions[0].AllowedTools)
}

func TestMemberActionPlanner_Plan_空目标空方案(t *testing.T) {
	planner := NewMemberActionPlanner(nil, nil, nil)
	// 无限制组直接空 targets
	plan, err := planner.Plan(context.Background(), PlanParams{
		Targets:                    []MemberOptimizationTarget{},
		RoleAttributionReport:      &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
	})
	require.NoError(t, err)
	require.Len(t, plan.Targets, 0)
	require.Equal(t, 0, plan.Metadata["action_count"])
	// 全部问题证据不足 → 过滤后空
	targets := plannerTarget()
	roleReport := &RoleAttributionReport{AssignedRoleIssues: []RoleIssueAttribution{{IssueID: "i1", Role: "solver"}}}
	mechReport := &MechanismAttributionReport{RoleMechanisms: map[string][]RoleMechanismAttribution{
		"solver": {{IssueID: "i1", MechanismType: "insufficient_role_evidence"}},
	}}
	plan, err = planner.Plan(context.Background(), PlanParams{
		Targets: targets, RoleAttributionReport: roleReport, MechanismAttributionReport: mechReport,
	})
	require.NoError(t, err)
	require.Empty(t, plan.Targets)
	require.Equal(t, []any{"i1"}, plan.Metadata["filtered_inactionable_issue_ids"])
}

func TestMemberActionPlanner_Plan_受限组与面(t *testing.T) {
	planner := NewMemberActionPlanner(nil, nil, nil)
	// 非法 prompt 面 → ValueError
	_, err := planner.Plan(context.Background(), PlanParams{
		Targets: plannerTarget(), RoleAttributionReport: &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
		AllowedPromptSurfaces:      []string{"tools"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported allowed_prompt_surfaces")
	// 受限组不含 config → target 面被延后
	plan, err := planner.Plan(context.Background(), PlanParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
		ActionDefinitions:          []harnessrsi.ActionDefinition{{Name: "prompt_modify", Group: "prompt", Operation: "modify"}},
		AllowedActionGroups:        []string{"prompt"},
	})
	require.NoError(t, err)
	require.Empty(t, plan.Targets)
	require.Equal(t, "unsupported_capability_request", plan.Metadata["capability_requests"].([]any)[0].(map[string]any)["status"])
}

func TestMemberActionPlanner_Plan_结构校验失败进重试(t *testing.T) {
	calls := 0
	saved := agentsBridge
	agentsBridge.InvokeStructured = func(_ context.Context, params StructuredInvokeParams) (map[string]any, error) {
		calls++
		if calls == 1 {
			return map[string]any{"actions": "bad"}, nil
		}
		return mkPlan(func(p map[string]any) {
			p["actions"] = []any{mkAction(func(m map[string]any) {})}
			p["action_waves"] = []any{[]any{"a1"}}
		}), nil
	}
	agentsBridge.ParseYamlOrJSONObject = testParseJSONObject
	t.Cleanup(func() { agentsBridge = saved })
	agent := &MemberActionPlannerAgent{prebuiltAgent: "fake"}
	planner := NewMemberActionPlanner(agent, nil, nil)
	plan, err := planner.Plan(context.Background(), PlanParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Len(t, plan.Actions, 1)
}

func TestMemberActionPlanner_Plan_耗尽拒绝(t *testing.T) {
	saved := agentsBridge
	agentsBridge.InvokeStructured = func(_ context.Context, params StructuredInvokeParams) (map[string]any, error) {
		return nil, &StructuredOutputExhaustedError{Msg: "exhausted after attempts"}
	}
	agentsBridge.ParseYamlOrJSONObject = testParseJSONObject
	t.Cleanup(func() { agentsBridge = saved })
	agent := &MemberActionPlannerAgent{prebuiltAgent: "fake"}
	planner := NewMemberActionPlanner(agent, nil, nil)
	_, err := planner.Plan(context.Background(), PlanParams{
		Targets: plannerTarget(), RoleAttributionReport: &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
	})
	require.Error(t, err)
	var rejected *ActionPlanRejectedError
	require.ErrorAs(t, err, &rejected)
}

func TestMemberActionPlanner_Plan_单动作模式禁search(t *testing.T) {
	saved := agentsBridge
	var seen []harnessrsi.ActionDefinition
	agentsBridge.InvokeStructured = func(_ context.Context, params StructuredInvokeParams) (map[string]any, error) {
		return mkPlan(func(p map[string]any) {}), nil
	}
	agentsBridge.ParseYamlOrJSONObject = testParseJSONObject
	t.Cleanup(func() { agentsBridge = saved })
	agent := &MemberActionPlannerAgent{prebuiltAgent: "fake"}
	// 捕获 adapter 收到的定义（fake agent 路径下走不到 adapter；改由 ValidatePlan 断言
	// search_disabled metadata）
	planner := NewMemberActionPlanner(agent, nil, nil)
	plan, err := planner.Plan(context.Background(), PlanParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{},
		MechanismAttributionReport: &MechanismAttributionReport{},
		MaxActionsPerPlan:          1,
	})
	require.NoError(t, err)
	require.Equal(t, true, plan.Metadata["skill_search_disabled_for_single_action"])
	require.Equal(t, 1, plan.Metadata["max_actions_per_plan"])
	_ = seen
}

func TestBindImmutableHypotheses_契约绑定(t *testing.T) {
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {})}
	})
	hypotheses := []map[string]any{{
		"hypothesis_id": "hyp_1", "content_sha256": "sha", "source_issue_id": "i1",
		"required_behavior": "do X", "forbidden_behavior": []any{"f"},
		"public_trigger": []any{}, "decisive_probe": map[string]any{},
		"decision_contract": map[string]any{"acceptance_observable": "obs"},
		"lever_policy":      map[string]any{"recommended_lever": "configuration"},
		"diagnostic_lens":   "failure", "intent": "corrective",
	}}
	bindImmutableHypotheses(plan, hypotheses)
	action := planActionsList(plan)[0]
	constraints := action["constraints"].(map[string]any)
	require.NotEmpty(t, constraints["optimization_contracts"])
	// lever_decision 绑定（policies 非空）
	require.NotNil(t, constraints["lever_decision"])
	require.Equal(t, "do X", action["expected_effect"])
	metadata := plan["metadata"].(map[string]any)
	require.Equal(t, []any{"hyp_1"}, metadata["optimization_hypothesis_ids"])
	require.Equal(t, "immutable_optimization_hypotheses", metadata["semantic_authority"])
}

func TestAnnotateActionBundles_束注记(t *testing.T) {
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {})}
	})
	annotateActionBundles(plan, []map[string]any{{"source_issue_id": "i1", "required_behavior": "do X"}})
	bundles := plan["metadata"].(map[string]any)["action_bundles"].([]any)
	require.Len(t, bundles, 1)
	bundle := bundles[0].(map[string]any)
	require.Equal(t, "solver:i1", bundle["bundle_id"])
	require.Equal(t, "do X", bundle["required_behavior"])
	require.Equal(t, true, bundle["atomic"])
}

func TestAttachImprovementBriefs_约束注入(t *testing.T) {
	plan := mkPlan(func(p map[string]any) {
		p["actions"] = []any{mkAction(func(m map[string]any) {
			m["action_group"] = "prompt"
		})}
	})
	targets := plannerTarget()
	roleReport := &RoleAttributionReport{AssignedRoleIssues: []RoleIssueAttribution{{
		IssueID: "i1", Role: "solver",
		Evidence: []map[string]any{{"improvement_brief": map[string]any{"gap": "x"}}},
	}}}
	attachImprovementBriefs(plan, targets, roleReport)
	action := planActionsList(plan)[0]
	require.Equal(t, map[string]any{"gap": "x"}, action["constraints"])
}

func TestAdaptPromptSurfaceWithinInstructionLever(t *testing.T) {
	// 空 allowed → 原样
	targets := plannerTarget()
	mech := &MechanismAttributionReport{}
	gotT, gotM := adaptPromptSurfaceWithinInstructionLever(targets, mech, nil)
	require.Len(t, gotT, 1)
	require.Same(t, mech, gotM)
	// prompt 面目标 → 收窄到 preferred
	targets = []MemberOptimizationTarget{{
		Role: "solver", HarnessRefPath: "h", AttributedIssueIDs: []string{"i1"},
		OptimizationSurfaces: []string{"identity"},
	}}
	mech = &MechanismAttributionReport{RoleMechanisms: map[string][]RoleMechanismAttribution{
		"solver": {{IssueID: "i1", OptimizationSurface: "identity"}},
	}}
	gotT, gotM = adaptPromptSurfaceWithinInstructionLever(targets, mech, map[string]bool{"prompt_section": true})
	require.Equal(t, []string{"prompt_section"}, gotT[0].OptimizationSurfaces)
	require.Equal(t, "prompt_section", gotM.RoleMechanisms["solver"][0].OptimizationSurface)
	metadata := gotT[0].Metadata["within_lever_surface_adaptation"].(map[string]any)
	require.Equal(t, "instruction", metadata["lever"])
}

func TestBuildPlannerHarnessSummaries_目录探测(t *testing.T) {
	// 不存在路径 → no structure summary available
	targets := plannerTarget()
	summaries := BuildPlannerHarnessSummaries(targets)
	require.Equal(t, "no structure summary available", summaries["solver"])
	// 真实结构目录
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "identity.md"), []byte("i"), 0o644)
	os.WriteFile(filepath.Join(dir, "harness.yaml"), []byte("tools:\n  - a\nskills: []\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "skills"), 0o755)
	targets = []MemberOptimizationTarget{{Role: "solver", HarnessRefPath: dir}}
	summaries = BuildPlannerHarnessSummaries(targets)
	require.Contains(t, summaries["solver"], "files: identity.md")
	require.Contains(t, summaries["solver"], "files: harness.yaml")
	require.Contains(t, summaries["solver"], "surface: skills/")
	require.Contains(t, summaries["solver"], "config: tools=1, skills=0, prompt_sections=0, rails=0")
}

func TestBuildPlannerUserMessage_模板段(t *testing.T) {
	agent := &MemberActionPlannerAgent{}
	params := PlannerCreateParams{
		Targets:                    plannerTarget(),
		RoleAttributionReport:      &RoleAttributionReport{AssignedRoleIssues: []RoleIssueAttribution{{IssueID: "i1", Role: "solver", Evidence: []map[string]any{{"summary": "s"}}}}},
		MechanismAttributionReport: &MechanismAttributionReport{RoleMechanisms: map[string][]RoleMechanismAttribution{"solver": {{IssueID: "i1", MechanismType: "config"}}}},
		ActionDefinitions:          []harnessrsi.ActionDefinition{{Name: "config_modify", Group: "config", Operation: "modify", Purpose: "p", Function: "f"}},
	}
	msg := agent.BuildUserMessage(params)
	for _, want := range []string{
		"## Current Action Contract",
		"## Run-Specific Strict Allowlist",
		"## Action Definitions",
		"- config/modify: p (function=f)",
		"## Selected Optimization Targets",
		"  - role: solver, member: ",
		"## Attributed Role Issues",
		"## Mechanism Attribution Summary",
		"## Output",
		"Return ONLY a JSON plan object as specified in the system prompt.",
	} {
		require.Contains(t, msg, want)
	}
	// validation feedback 段
	params.ValidationErrors = []string{"err1"}
	msg = agent.BuildUserMessage(params)
	require.Contains(t, msg, "## Previous Plan Validation Errors")
	// hypothesis contract 段
	params.OptimizationHypotheses = []map[string]any{{"hypothesis_id": "h1", "source_issue_id": "i1"}}
	msg = agent.BuildUserMessage(params)
	require.Contains(t, msg, "## Immutable Optimization Hypotheses")
	// experience context 段
	params.OptimizationExperience = map[string]any{"journal": []any{}}
	msg = agent.BuildUserMessage(params)
	require.Contains(t, msg, "## Optimization Experience")
	// policy + sibling 段
	params.OptimizationExperience["improver_policy"] = map[string]any{"version_id": "v1"}
	params.OptimizationExperience["sibling_generation"] = map[string]any{"candidate_id": "c1"}
	msg = agent.BuildUserMessage(params)
	require.Contains(t, msg, "## Frozen Improver Policy")
	require.Contains(t, msg, "## Sibling Candidate Generation")
}

func TestMemberActionPlannerAgentBuildRetryMessage(t *testing.T) {
	msg := MemberActionPlannerAgentBuildRetryMessage("TASK", "ERR")
	require.Contains(t, msg, "Return ONLY one valid JSON or YAML mapping.")
	require.Contains(t, msg, "Original task:\nTASK")
	require.Contains(t, msg, "Last error:\nERR")
	require.Contains(t, msg, "return an empty")
}

func TestWritePlan_YAML落盘(t *testing.T) {
	plan := &MemberOptimizationPlan{
		PlanID: "member_plan_x", Targets: plannerTarget(),
		Actions:     []MemberOptimizationAction{*mkActionStruct()},
		ActionWaves: [][]string{{"a1"}},
		Metadata:    map[string]any{"validated": true},
	}
	path, err := WritePlan(plan, t.TempDir())
	require.NoError(t, err)
	require.FileExists(t, path)
}

func mkActionStruct() *MemberOptimizationAction {
	return &MemberOptimizationAction{
		ActionID: "a1", Role: "solver", ActionGroup: "config", Operation: "modify",
		TargetPath: "runtime.yaml", DeclaredWritePaths: []string{"runtime.yaml"},
		AttributedIssueIDs: []string{"i1"}, RunIf: "dependency_succeeded",
		Constraints: map[string]any{},
	}
}

func TestCompactSiblingProposal_Action压缩(t *testing.T) {
	proposal := map[string]any{
		"candidate_id": "c1", "summary": "s",
		"actions": []any{map[string]any{
			"role": "solver", "constraints": map[string]any{"lever_decision": map[string]any{"selected_lever": "instruction"}},
		}},
		"capabilities": []any{"not-a-dict"},
	}
	compact := compactSiblingProposal(proposal)
	require.Equal(t, "c1", compact["candidate_id"])
	actions := compact["actions"].([]any)
	require.Len(t, actions, 1)
	require.Equal(t, "instruction", actions[0].(map[string]any)["lever_decision"].(map[string]any)["selected_lever"])
	// capabilities 非 dict 全滤 → 不写字段
	require.NotContains(t, compact, "capabilities")
}

func TestCompactExperimentForPlanner(t *testing.T) {
	out := compactExperimentForPlanner(map[string]any{
		"experiment_id": "e1", "bulky": "drop-me", "status": "ok",
	})
	require.Equal(t, "e1", out["experiment_id"])
	require.NotContains(t, out, "bulky")
}

var _ = harnessrsi.DefaultOutputRepairConfig
