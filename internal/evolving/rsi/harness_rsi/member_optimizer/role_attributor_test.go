package memberoptimizer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

// fakeRoleAgent 测试用假评委（按 issue_id 分发响应保证并发确定性；记录消息）。
type fakeRoleAgent struct {
	responses map[string][]string // issue_id → 按序响应
	calls     map[string]int
	seenMsg   []string
	mu        sync.Mutex
}

func newFakeRoleAgent(responses map[string][]string) *fakeRoleAgent {
	return &fakeRoleAgent{responses: responses, calls: map[string]int{}}
}

func (f *fakeRoleAgent) nextText(issueID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	texts := f.responses[issueID]
	i := f.calls[issueID]
	f.calls[issueID] = i + 1
	if i < len(texts) {
		return texts[i]
	}
	return texts[len(texts)-1]
}

func (f *fakeRoleAgent) record(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seenMsg = append(f.seenMsg, msg)
}

// withInvokeSeam 覆盖能力桥三项（测试进程不触发 agents 包 init——blank import 会成环）。
// InvokeStructured 模拟真实语义：解析文本、失败进重试、耗尽返回 Exhausted。
func withInvokeSeam(t *testing.T, fake *fakeRoleAgent) {
	t.Helper()
	saved := agentsBridge
	agentsBridge.InvokeStructured = func(_ context.Context, params StructuredInvokeParams) (map[string]any, error) {
		fake.record(params.UserMessage)
		issueID := extractIssueID(params.UserMessage)
		message := params.UserMessage
		for attempt := 0; ; attempt++ {
			raw, err := testParseJSONObject(fake.nextText(issueID))
			if err == nil {
				return raw, nil
			}
			if attempt >= params.RetryLimit {
				return nil, &StructuredOutputExhaustedError{
					Msg: fmt.Sprintf("%s failed after %d attempts (attempt_limit_reached). Last error: parse error: %v",
						params.AgentName, attempt+1, err),
				}
			}
			// 模拟真实重试：UserMessage + 重试反馈消息（output.go 语义）
			if params.BuildRetryMessage != nil {
				message = params.UserMessage + "\n\n" + params.BuildRetryMessage(map[string]any{}, "parse error: "+err.Error())
			}
			fake.record(message)
		}
	}
	agentsBridge.ParseJSONObject = testParseJSONObject
	agentsBridge.CreateRoleAttributionAgent = func(context.Context, AttributionAgentParams) (any, error) {
		return "fake-deep-agent", nil
	}
	agentsBridge.CreateMechanismAttributionAgent = agentsBridge.CreateRoleAttributionAgent
	t.Cleanup(func() { agentsBridge = saved })
}

// extractIssueID 从用户消息提取 issue_id（并发确定性分发用）。
func extractIssueID(msg string) string {
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, "- issue_id: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "- issue_id: "))
		}
	}
	return ""
}

// testParseJSONObject 测试用 JSON 对象解析（等价 agents.ParseJSONObjectResponse）。
func testParseJSONObject(text string) (map[string]any, error) {
	trimmed := strings.TrimSpace(text)
	var m map[string]any
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return nil, errors.New("not a json object")
	}
	return m, nil
}

func TestValidateRoleAttributionOutput_逐字段校验(t *testing.T) {
	// 缺 issue_id
	errs := ValidateRoleAttributionOutput(map[string]any{"decision": "unassigned", "confidence": 0.9}, []string{"solver"})
	require.Contains(t, errs, "missing field: issue_id")
	// 缺 decision
	errs = ValidateRoleAttributionOutput(map[string]any{"issue_id": "i1", "confidence": 0.9}, []string{"solver"})
	require.Contains(t, errs, "missing field: decision")
	// assigned 缺 role
	errs = ValidateRoleAttributionOutput(map[string]any{"issue_id": "i1", "decision": "assigned", "confidence": 0.9}, []string{"solver"})
	require.Contains(t, errs, "missing field: role for assigned decision")
	// role 不在候选
	errs = ValidateRoleAttributionOutput(map[string]any{"issue_id": "i1", "decision": "assigned", "role": "writer", "confidence": 0.9}, []string{"solver"})
	require.Contains(t, errs, "role 'writer' not in candidate_roles")
	// 缺 confidence
	errs = ValidateRoleAttributionOutput(map[string]any{"issue_id": "i1", "decision": "unassigned"}, []string{"solver"})
	require.Contains(t, errs, "missing field: confidence")
	// confidence 越界
	errs = ValidateRoleAttributionOutput(map[string]any{"issue_id": "i1", "decision": "unassigned", "confidence": 1.5}, []string{"solver"})
	require.Contains(t, errs, "confidence 1.5 out of range [0, 1]")
	// confidence 非数字
	errs = ValidateRoleAttributionOutput(map[string]any{"issue_id": "i1", "decision": "unassigned", "confidence": "high"}, []string{"solver"})
	require.Contains(t, errs, "confidence is not a number: high")
	// 合法输出零错误
	errs = ValidateRoleAttributionOutput(map[string]any{"issue_id": "i1", "decision": "assigned", "role": "solver", "confidence": 0.8}, []string{"solver"})
	require.Empty(t, errs)
}

func TestNormalizeMemberKey(t *testing.T) {
	require.Equal(t, "main_solver", NormalizeMemberKey("Main-Solver"))
	require.Equal(t, "", NormalizeMemberKey(nil))
	require.Equal(t, "", NormalizeMemberKey("  "))
}

func TestIsSingleRoleAliasKey(t *testing.T) {
	require.True(t, IsSingleRoleAliasKey("solver"))
	require.True(t, IsSingleRoleAliasKey("single_harness"))
	require.False(t, IsSingleRoleAliasKey("planner"))
}

func TestTargetMembersForIssue(t *testing.T) {
	issue := harnessrsi.TeamIssue{TargetMembers: []string{" Coder ", "", "reviewer"}}
	require.Equal(t, []string{"Coder", "reviewer"}, TargetMembersForIssue(issue))
	// 空 target_members 回落 affected_component
	issue = harnessrsi.TeamIssue{Metadata: map[string]any{"affected_component": " coder "}}
	require.Equal(t, []string{"coder"}, TargetMembersForIssue(issue))
	issue = harnessrsi.TeamIssue{}
	require.Empty(t, TargetMembersForIssue(issue))
}

func TestMatchTargetMember_分支覆盖(t *testing.T) {
	candidates := []MemberRoleCandidate{
		{Role: "solver", MemberName: "Main", HarnessRefPath: "h/solver", Metadata: map[string]any{"aliases": []any{"worker"}}},
		{Role: "reviewer", MemberName: "Check", HarnessRefPath: "h/reviewer"},
	}
	// 非 member_harness
	c, status := MatchTargetMember(harnessrsi.TeamIssue{OptimizationTarget: "team_skill"}, candidates)
	require.Nil(t, c)
	require.Equal(t, "not_member_harness", status)
	// 缺 target_members
	c, status = MatchTargetMember(harnessrsi.TeamIssue{OptimizationTarget: "member_harness"}, candidates)
	require.Nil(t, c)
	require.Equal(t, "missing_target_members", status)
	// 精确匹配（member_name 归一化对上）
	c, status = MatchTargetMember(harnessrsi.TeamIssue{OptimizationTarget: "member_harness", TargetMembers: []string{"main"}}, candidates)
	require.NotNil(t, c)
	require.Equal(t, "solver", c.Role)
	require.Equal(t, "target_members_exact_match", status)
	// 别名匹配
	c, status = MatchTargetMember(harnessrsi.TeamIssue{OptimizationTarget: "member_harness", TargetMembers: []string{"worker"}}, candidates)
	require.NotNil(t, c)
	require.Equal(t, "solver", c.Role)
	require.Equal(t, "target_members_exact_match", status)
	// 歧义
	c, status = MatchTargetMember(harnessrsi.TeamIssue{OptimizationTarget: "member_harness", TargetMembers: []string{"main", "check"}}, candidates)
	require.Nil(t, c)
	require.Equal(t, "target_members_ambiguous", status)
	// 单角色别名捷径
	solo := []MemberRoleCandidate{{Role: "solver", HarnessRefPath: "h/solver"}}
	c, status = MatchTargetMember(harnessrsi.TeamIssue{OptimizationTarget: "member_harness", TargetMembers: []string{"agent"}}, solo)
	require.NotNil(t, c)
	require.Equal(t, "target_members_single_role_alias", status)
	// 不匹配
	c, status = MatchTargetMember(harnessrsi.TeamIssue{OptimizationTarget: "member_harness", TargetMembers: []string{"ghost"}}, candidates)
	require.Nil(t, c)
	require.Equal(t, "target_members_mismatch", status)
}

func TestTraceRefsForIssue(t *testing.T) {
	// raw 带 evidence_refs
	refs := TraceRefsForIssue(harnessrsi.TeamIssue{}, map[string]any{
		"evidence_refs": []any{map[string]any{"case_id": "c1", "trace": "t1"}},
	})
	require.Equal(t, []map[string]string{{"case_id": "c1", "trace": "t1"}}, refs)
	// raw 无 evidence_refs 回落 affected_cases
	refs = TraceRefsForIssue(harnessrsi.TeamIssue{AffectedCases: []string{"c2", "c3"}}, map[string]any{})
	require.Equal(t, []map[string]string{{"case_id": "c2"}, {"case_id": "c3"}}, refs)
}

func TestSummarizeBehaviorTrace_各分支(t *testing.T) {
	// 非字典
	require.Equal(t, "none", SummarizeBehaviorTrace("bad"))
	// 空字典
	require.Equal(t, "none", SummarizeBehaviorTrace(map[string]any{}))
	// workspace_changes
	got := SummarizeBehaviorTrace(map[string]any{
		"workspace_changes": map[string]any{"added": []any{"a.go"}, "modified": []any{"b.go"}, "removed": []any{}},
	})
	require.Contains(t, got, "workspace_changes=added:['a.go'], modified:['b.go'], removed:[]")
	// command_log
	got = SummarizeBehaviorTrace(map[string]any{
		"command_log": []any{map[string]any{"command": "pytest", "exit_code": 1, "stderr_excerpt": "boom"}},
	})
	require.Contains(t, got, "'command': 'pytest'")
	require.Contains(t, got, "'exit_code': 1")
	// trajectory_window_summary
	got = SummarizeBehaviorTrace(map[string]any{
		"trajectory_window_summary": map[string]any{
			"failure_signatures": []any{"repeated_failure_loop"},
			"recent_events":      []any{map[string]any{"event_type": "tool_error", "summary": "s"}},
		},
	})
	require.Contains(t, got, "trajectory_window=failure_signatures:['repeated_failure_loop']")
	// normalized_trace_summary
	got = SummarizeBehaviorTrace(map[string]any{
		"normalized_trace_summary": map[string]any{
			"traces": []any{map[string]any{
				"trace_id": "t1", "member_role": "solver",
				"messages": []any{map[string]any{
					"message_index": 3,
					"tool_calls":    []any{map[string]any{"step_pointer": "sp1", "name": "edit", "error": "boom"}},
				}},
			}},
		},
	})
	require.Contains(t, got, "normalized_trace_failures=[")
	require.Contains(t, got, "'tool': 'edit'")
}

func TestSummarizeIssueAttribution(t *testing.T) {
	require.Equal(t, "none", SummarizeIssueAttribution(map[string]any{}))
	got := SummarizeIssueAttribution(map[string]any{"metadata": map[string]any{
		"attribution": map[string]any{"root_cause": "prompt vague", "target_ref": "identity.md", "evidence_refs": []any{"e1"}},
	}})
	require.Contains(t, got, `"root_cause": "prompt vague"`)
	require.Contains(t, got, `"target_ref": "identity.md"`)
}

func TestSummarizeIssueEvidence(t *testing.T) {
	require.Equal(t, "none", SummarizeIssueEvidence("bad"))
	got := SummarizeIssueEvidence([]any{map[string]any{"case_id": "c1", "signals": []any{"sig"}}})
	require.Contains(t, got, `"case_id": "c1"`)
}

func TestRoleAttributorAgent_AttributeIssue_重试与成功(t *testing.T) {
	fake := newFakeRoleAgent(map[string][]string{"i1": {
		"not json",
		`{"issue_id": "i1", "decision": "assigned", "role": "solver", "confidence": 0.9, "rationale": "r"}`,
	}})
	withInvokeSeam(t, fake)
	bundle := &BoundedEvidenceBundle{
		Issue:          map[string]any{"issue_id": "i1"},
		CandidateRoles: []MemberRoleCandidate{{Role: "solver"}},
	}
	agent := NewRoleAttributorAgentWithAgent("", 2, nil)
	raw, err := agent.AttributeIssue(context.Background(), bundle)
	require.NoError(t, err)
	require.Equal(t, "assigned", raw["decision"])
	require.Equal(t, 2, fake.calls["i1"])
	// 第二次调用携带重试消息（含前次输出与校验错误）
	require.Contains(t, fake.seenMsg[len(fake.seenMsg)-1], "## Previous Invalid Output")
}

func TestRoleAttributorAgent_AttributeIssue_重试耗尽(t *testing.T) {
	fake := newFakeRoleAgent(map[string][]string{"i1": {"not json", "still not json", "nope"}})
	withInvokeSeam(t, fake)
	bundle := &BoundedEvidenceBundle{
		Issue:          map[string]any{"issue_id": "i1"},
		CandidateRoles: []MemberRoleCandidate{{Role: "solver"}},
	}
	agent := NewRoleAttributorAgentWithAgent("", 2, nil)
	_, err := agent.AttributeIssue(context.Background(), bundle)
	require.Error(t, err)
	require.Contains(t, err.Error(), "RoleAttributorAgent failed after")
}

func TestRoleAttributorAgentBuildRetryMessage(t *testing.T) {
	msg := RoleAttributorAgentBuildRetryMessage(
		map[string]any{"decision": "assigned"},
		"validation errors: missing field: issue_id",
	)
	require.Contains(t, msg, "## Previous Invalid Output")
	require.Contains(t, msg, `"decision": "assigned"`)
	require.Contains(t, msg, "## Validation Error")
	require.Contains(t, msg, "validation errors: missing field: issue_id")
	require.Contains(t, msg, "Return ONLY a corrected JSON object matching the role attribution schema.")
}

func TestBuildRoleAttributionUserMessage(t *testing.T) {
	bundle := &BoundedEvidenceBundle{
		Issue: map[string]any{
			"issue_id": "i1", "category": "cat", "severity": "high",
			"summary": "sum", "optimization_target": "member_harness",
			"target_members": []any{"solver"}, "affected_component": "comp",
			"suspected_team_scope": "team", "recommendation": "rec",
		},
		CaseResults: []map[string]any{{"case_id": "c1", "status": "failed", "score": 0.1, "response": strings.Repeat("x", 300)}},
		CaseTraces:  []map[string]any{{"case_id": "c1", "evaluation": "bad", "behavior_trace": map[string]any{}}},
		CandidateRoles: []MemberRoleCandidate{
			{Role: "solver", MemberName: "Main", HarnessRefPath: "h/solver", Description: "d"},
		},
	}
	msg := BuildRoleAttributionUserMessage(bundle)
	require.Contains(t, msg, "- issue_id: i1")
	require.Contains(t, msg, "- severity: high")
	require.Contains(t, msg, "- analyzer_attribution: none")
	require.Contains(t, msg, "## Affected Cases")
	require.Contains(t, msg, "Case c1:")
	require.Contains(t, msg, "response_summary: "+strings.Repeat("x", 200)) // Python [:200] 截断
	require.Contains(t, msg, "## Candidate Roles")
	require.Contains(t, msg, "  - role: solver")
	require.Contains(t, msg, "## Output Instructions")
}

func TestRoleAttributor_Attribute_编排全链(t *testing.T) {
	fake := newFakeRoleAgent(map[string][]string{
		"i1": {`{"issue_id": "i1", "decision": "unassigned", "confidence": 0.6, "reason": "not a member issue"}`},
		"i2": {`{"issue_id": "i2", "decision": "assigned", "role": "solver", "confidence": 0.9, "rationale": "clear"}`},
	})
	withInvokeSeam(t, fake)
	evalRef := &EvalRef{EvalID: "e1", EvalDir: t.TempDir(), Cases: []map[string]any{}}
	issues := []harnessrsi.TeamIssue{
		{IssueID: "i1", Category: "cat", Severity: "high", Summary: "s1",
			OptimizationTarget: "team_skill", Recommendation: "r1", Metadata: map[string]any{}},
		{IssueID: "i2", Category: "cat", Severity: "low", Summary: "s2",
			OptimizationTarget: "member_harness", TargetMembers: []string{"solver"},
			Recommendation: "r2", Metadata: map[string]any{}},
	}
	candidates := []MemberRoleCandidate{{Role: "solver", MemberName: "Main", HarnessRefPath: "h/solver"}}
	attr := NewRoleAttributorWithAgent(NewRoleAttributorAgentWithAgent("", 2, nil), 4)
	report, err := attr.Attribute(context.Background(), AttributeParams{
		EvalRef:        evalRef,
		TeamIssues:     issues,
		CandidateRoles: candidates,
	})
	require.NoError(t, err)
	require.Len(t, report.AssignedRoleIssues, 1)
	require.Len(t, report.UnassignedIssues, 1)
	// i2 命中 target_members 精确匹配
	ri := report.AssignedRoleIssues[0]
	require.Equal(t, "i2", ri.IssueID)
	require.Equal(t, "solver", ri.Role)
	require.Equal(t, "h/solver", ri.HarnessRefPath)
	require.Equal(t, 0.9, ri.Confidence)
	require.Equal(t, "target_members_exact_match", ri.Evidence[0]["assignment_source"])
	require.Equal(t, "case_evidence", ri.Evidence[0]["evidence_quality"])
	// 排序与元数据
	require.Equal(t, "i1", report.UnassignedIssues[0].IssueID)
	require.Equal(t, 2, report.Metadata["issue_count"])
	require.Equal(t, "exact_match_or_single_role_alias", report.Metadata["target_member_routing"])
	require.Contains(t, report.AttributionID, "role_attribution_")
	require.NotEmpty(t, report.SourceEvalRefPath)
}

func TestRoleAttributor_Attribute_目标成员覆盖LLM(t *testing.T) {
	fake := newFakeRoleAgent(map[string][]string{"i1": {
		`{"issue_id": "i1", "decision": "assigned", "role": "reviewer", "confidence": 0.8}`,
	}})
	withInvokeSeam(t, fake)
	evalRef := &EvalRef{EvalDir: t.TempDir()}
	issues := []harnessrsi.TeamIssue{
		{IssueID: "i1", Summary: "s", OptimizationTarget: "member_harness",
			TargetMembers: []string{"main"}, Metadata: map[string]any{}},
	}
	candidates := []MemberRoleCandidate{{Role: "solver", MemberName: "Main", HarnessRefPath: "h/solver"}}
	// LLM 说 assigned 给别的角色 → 被 target_members 覆盖
	attr := NewRoleAttributorWithAgent(NewRoleAttributorAgentWithAgent("", 2, nil), 4)
	report, err := attr.Attribute(context.Background(), AttributeParams{
		EvalRef: evalRef, TeamIssues: issues, CandidateRoles: candidates,
	})
	require.NoError(t, err)
	require.Len(t, report.AssignedRoleIssues, 1)
	ri := report.AssignedRoleIssues[0]
	require.Equal(t, "solver", ri.Role) // 覆盖为 target_members 角色
	require.Equal(t, "target_members_overrode_unassigned_or_mismatch", ri.Evidence[0]["assignment_source"])
	require.Equal(t, "case_evidence", ri.Evidence[0]["evidence_quality"])
}

func TestRoleAttributor_Attribute_置信度钳制(t *testing.T) {
	evalRef := &EvalRef{EvalDir: t.TempDir()}
	issues := []harnessrsi.TeamIssue{
		{IssueID: "i1", Summary: "s", OptimizationTarget: "member_harness",
			TargetMembers: []string{"main", "check"}, Metadata: map[string]any{}},
	}
	candidates := []MemberRoleCandidate{
		{Role: "solver", MemberName: "Main", HarnessRefPath: "h/s"},
		{Role: "reviewer", MemberName: "Check", HarnessRefPath: "h/r"},
	}
	// target_members 歧义（target_candidate 为 nil），LLM assigned 生效走 llm_assigned；
	// decision=assigned 时 confidence 直接取 raw 值 0.2（Python role_attributor.py:505）
	fake := newFakeRoleAgent(map[string][]string{"i1": {
		`{"issue_id": "i1", "decision": "assigned", "role": "solver", "confidence": 0.2}`,
	}})
	withInvokeSeam(t, fake)
	attr := NewRoleAttributorWithAgent(NewRoleAttributorAgentWithAgent("", 2, nil), 4)
	report, err := attr.Attribute(context.Background(), AttributeParams{
		EvalRef: evalRef, TeamIssues: issues, CandidateRoles: candidates,
	})
	require.NoError(t, err)
	require.Len(t, report.AssignedRoleIssues, 1)
	require.Equal(t, "solver", report.AssignedRoleIssues[0].Role)
	require.Equal(t, "llm_assigned", report.AssignedRoleIssues[0].Evidence[0]["assignment_source"])
	require.Equal(t, 0.2, report.AssignedRoleIssues[0].Confidence)
}

func TestRoleAttributor_Attribute_agent失败包装RuntimeError(t *testing.T) {
	fake := newFakeRoleAgent(map[string][]string{"i1": {"bad", "bad", "bad"}})
	withInvokeSeam(t, fake)
	evalRef := &EvalRef{EvalDir: t.TempDir()}
	issues := []harnessrsi.TeamIssue{{IssueID: "i1", Metadata: map[string]any{}}}
	attr := NewRoleAttributorWithAgent(NewRoleAttributorAgentWithAgent("", 0, nil), 4)
	_, err := attr.Attribute(context.Background(), AttributeParams{
		EvalRef: evalRef, TeamIssues: issues,
		CandidateRoles: []MemberRoleCandidate{{Role: "solver"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Role attribution task failed:")
	require.Contains(t, err.Error(), "RoleAttributorAgent failed after")
}

func TestWriteRoleAttributionReport_YAML字段(t *testing.T) {
	report := &RoleAttributionReport{
		AttributionID:            "role_attribution_abcd1234",
		SourceEvalRefPath:        "/tmp/eval_ref.yaml",
		SourceAnalysisResultPath: "",
		HarnessRefsPath:          "",
		CandidateRoles:           []MemberRoleCandidate{{Role: "solver", MemberName: "Main", HarnessRefPath: "h/s", Description: "d", Metadata: map[string]any{"k": "v"}}},
		AssignedRoleIssues: []RoleIssueAttribution{{
			IssueID: "i1", Role: "solver", MemberName: "Main", HarnessRefPath: "h/s",
			Confidence: 0.9, Rationale: "ok",
			Evidence:  []map[string]any{{"assignment_source": "llm_assigned", "evidence_quality": "case_evidence"}},
			TraceRefs: []map[string]string{{"case_id": "c1"}},
		}},
		UnassignedIssues: []UnassignedMemberIssue{{IssueID: "i2", Reason: "x", Rationale: "y"}},
		Warnings:         []string{"i2: ambiguous"},
		Metadata:         map[string]any{"issue_count": 2},
	}
	dir := t.TempDir()
	path, err := WriteRoleAttributionReport(report, dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "role_attribution.yaml"), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	for _, want := range []string{
		"attribution_id: role_attribution_abcd1234",
		"source_eval_ref_path: /tmp/eval_ref.yaml",
		"assigned_role_issues:",
		"assignment_source: llm_assigned",
		"evidence_quality: case_evidence",
		"unassigned_issues:",
		"warnings:",
		"issue_count: 2",
	} {
		require.Contains(t, text, want, "YAML 缺字段: "+want)
	}
}

// readFileForTest 读文件（测试辅助）。
func readFileForTest(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

// yamlUnmarshalMappingForTest 测试用 YAML 映射解析。
func yamlUnmarshalMappingForTest(text string) (map[string]any, error) {
	var m map[string]any
	if err := yaml.Unmarshal([]byte(text), &m); err != nil {
		return nil, err
	}
	return m, nil
}
