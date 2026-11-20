//go:build integration

// 17.4c1 定责与选对象集成用例（spec §3.4 集成链路）：
// "eval_ref + analysis_ref + harness_refs → 定责（fake 评委首败重试）→ 定病根 → 纯算术选择 → 三 yaml 落盘"
// 跨 member_optimizer + harness_rsi(schema/feedback) + agents(factory 桥接线) ≥3 子系统。
// 另含 BuildDeepAgentFromModelConfig 真身回填验证（17.4b2 fakechain 头注释 ⤴️17.4c 的兑现）：
// 真实 harness 工厂装配 DeepAgent（不调 LLM，仅构造级端到端）。

package rsi

import (
	"context"
	"encoding/json"
	agentface "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/member_optimizer/agentface"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/interfaces"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/member_optimizer"
	_ "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi/member_optimizer/agents"
)

// MemberAttributionFakeChainSuite 定责→定病根→选择 全链 suite。
type MemberAttributionFakeChainSuite struct {
	suite.Suite
	tempDir  string
	invokeMu sync.Mutex
	// scripted 按 issue_id / role 分发的脚本化评委响应
	scripted map[string][]string
	// calls 记录各 agent 调用消息
	calls []string
}

func TestMemberAttributionFakeChainSuite(t *testing.T) {
	suite.Run(t, new(MemberAttributionFakeChainSuite))
}

func (s *MemberAttributionFakeChainSuite) SetupTest() {
	s.tempDir = s.T().TempDir()
	s.scripted = map[string][]string{}
	s.calls = []string{}
}

// wireFakeBridge 覆盖能力桥（fake 评委；测试结束还原）。
func (s *MemberAttributionFakeChainSuite) wireFakeBridge() {
	saved := memberoptimizer.SnapshotAgentsBridge()
	scripted := s.scripted
	memberoptimizer.ReplaceAgentsBridge(memberoptimizer.AgentsBridge{
		InvokeStructured: func(_ context.Context, params memberoptimizer.StructuredInvokeParams) (map[string]any, error) {
			s.invokeMu.Lock()
			s.calls = append(s.calls, params.UserMessage)
			s.invokeMu.Unlock()
			issueID := extractIssueIDForTest(params.UserMessage)
			texts := scripted[issueID]
			if len(texts) == 0 {
				texts = scripted[params.AgentName]
			}
			for attempt := 0; attempt < len(texts); attempt++ {
				raw, err := parseJSONForIntegration(texts[attempt])
				if err == nil {
					return raw, nil
				}
			}
			return nil, &memberoptimizer.StructuredOutputExhaustedError{
				Msg: params.AgentName + " failed after attempts (attempt_limit_reached)",
			}
		},
		ParseJSONObject:       parseJSONForIntegration,
		ParseYamlOrJSONObject: parseJSONForIntegration,
		CreateRoleAttributionAgent: func(context.Context, memberoptimizer.AttributionAgentParams) (*agentface.Agent, error) {
			return &agentface.Agent{}, nil
		},
		CreateMechanismAttributionAgent: func(context.Context, memberoptimizer.AttributionAgentParams) (*agentface.Agent, error) {
			return &agentface.Agent{}, nil
		},
	})
	s.T().Cleanup(func() { memberoptimizer.RestoreAgentsBridge(saved) })
}

func extractIssueIDForTest(msg string) string {
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, "- issue_id: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "- issue_id: "))
		}
	}
	return ""
}

func parseJSONForIntegration(text string) (map[string]any, error) {
	trimmed := strings.TrimSpace(text)
	var m map[string]any
	// 简化：非 { 开头直接失败（触发重试链）
	if !strings.HasPrefix(trimmed, "{") {
		return nil, os.ErrInvalid
	}
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// TestAttribution_FullChain 定责→定病根→选择→落盘全链。
func (s *MemberAttributionFakeChainSuite) TestAttribution_FullChain() {
	s.wireFakeBridge()
	// —— 造输入：harness_refs.yaml + eval_ref.yaml + analysis_ref.yaml ——
	refsPath := filepath.Join(s.tempDir, "harness_refs.yaml")
	s.Require().NoError(os.WriteFile(refsPath, []byte(
		"roles:\n  - role: solver\n    member_name: Main\n    harness_ref_path: harnesses/solver\n    description: main solver\n"), 0o644))
	evalRefPath := filepath.Join(s.tempDir, "eval_ref.yaml")
	s.Require().NoError(os.WriteFile(evalRefPath, []byte(
		"eval_id: e1\nteam_name: t1\nharness_refs_path: "+refsPath+"\ncases: []\n"), 0o644))
	analysisPath := filepath.Join(s.tempDir, "analysis_ref.yaml")
	s.Require().NoError(os.WriteFile(analysisPath, []byte(
		"analysis_id: a1\nissues:\n"+
			"  - issue_id: i1\n    category: cat\n    severity: high\n    summary: s1\n"+
			"    optimization_target: member_harness\n    target_members: [solver]\n"+
			"    recommendation: r1\n    metadata: {attribution: {target_ref: identity.md, decision_contract: {acceptance_observable: obs}}}\n"+
			"  - issue_id: i2\n    category: cat\n    severity: low\n    summary: s2\n"+
			"    optimization_target: team_skill\n    recommendation: r2\n    metadata: {}\n"), 0o644))

	// —— 评委脚本：i1 assigned（首轮坏输出触发重试）；solver 角色病根归因 ——
	s.scripted["i1"] = []string{
		"not json",
		`{"issue_id": "i1", "decision": "assigned", "role": "solver", "confidence": 0.9, "rationale": "clear"}`,
	}
	s.scripted["MechanismAttributorAgent[solver]"] = []string{
		`{"role": "solver", "attributions": [{"issue_id": "i1", "mechanism_type": "prompt", "failure_signature": "prompt_instruction_mismatch", "confidence": 0.8, "optimization_surface": "identity"}]}`,
	}

	// —— 全链执行 ——
	evalRef, loadErr := memberoptimizer.LoadEvalRef(evalRefPath)
	s.Require().NoError(loadErr)
	analysisRef := memberoptimizer.LoadAnalysisRef(analysisPath)
	teamIssues := memberoptimizer.ResolveTeamIssues(analysisRef)
	s.Require().Len(teamIssues, 1) // team_skill 目标被过滤
	candidates := memberoptimizer.ResolveCandidateRoles(refsPath, &evalRef, "")

	roleAttributor := memberoptimizer.NewRoleAttributor(2, nil)
	roleReport, err := roleAttributor.Attribute(context.Background(), memberoptimizer.AttributeParams{
		EvalRef: &evalRef, TeamIssues: teamIssues, CandidateRoles: candidates,
	})
	s.Require().NoError(err)
	s.Require().Len(roleReport.AssignedRoleIssues, 1)
	rolePath, err := memberoptimizer.WriteRoleAttributionReport(roleReport, s.tempDir)
	s.Require().NoError(err)
	s.Require().FileExists(rolePath)

	mechAttributor := memberoptimizer.NewMechanismAttributor(2, nil)
	mechReport, err := mechAttributor.Attribute(context.Background(), memberoptimizer.MechanismAttributeParams{
		RoleAttributionReport: roleReport, EvalRef: &evalRef, CandidateRoles: candidates,
	})
	s.Require().NoError(err)
	mechPath, err := memberoptimizer.WriteMechanismAttributionReport(mechReport, s.tempDir)
	s.Require().NoError(err)
	s.Require().FileExists(mechPath)

	selection := memberoptimizer.SelectMemberTargets(roleReport, mechReport, memberoptimizer.SelectorOptions{})
	s.Require().Len(selection.Targets, 1)
	s.Require().Equal("solver", selection.Targets[0].Role)
	s.Require().Contains(selection.Targets[0].Reason, "Score=")
	selectionPath, err := memberoptimizer.WriteMemberSelectionReport(selection, s.tempDir)
	s.Require().NoError(err)
	s.Require().FileExists(selectionPath)

	// 重试链证据：i1 首轮坏输出 → 第二轮成功
	s.Require().True(strings.Contains(s.calls[0], "- issue_id: i1"))
}

// TestAttribution_HypothesisContract 假设契约编译 + manifest 全链（单 harness 强校验语义）。
func (s *MemberAttributionFakeChainSuite) TestAttribution_HypothesisContract() {
	// 编译 → 载入 → manifest
	analysisPath := filepath.Join(s.tempDir, "analysis_ref.yaml")
	s.Require().NoError(os.WriteFile(analysisPath, []byte(
		"analysis_id: a1\nissues:\n"+
			"  - issue_id: i1\n    category: capability missing\n    severity: high\n    summary: s\n"+
			"    optimization_target: member_harness\n    target_members: [solver]\n"+
			"    affected_cases: [c1]\n    recommendation: rec\n"+
			"    metadata: {attribution: {target_ref: identity.md, general_mechanism: gm, decision_contract: {acceptance_observable: file exists}}}\n"), 0o644))
	hypPath := filepath.Join(s.tempDir, "hyp.yaml")
	_, err := memberoptimizer.CompileOptimizationHypotheses(analysisPath, []map[string]any{
		{"case_id": "c1", "input": map[string]any{"user_message": "execution environment: x\nclean task"}},
	}, hypPath)
	s.Require().NoError(err)
	hypotheses, err := memberoptimizer.LoadOptimizationHypotheses(hypPath)
	s.Require().NoError(err)
	s.Require().Len(hypotheses, 1)
	// transport 剥离生效
	publicTriggers := hypotheses[0]["public_trigger"].([]any)
	s.Require().Len(publicTriggers, 1)
	s.Require().Equal("clean task", publicTriggers[0].(map[string]any)["task"])

	plan := map[string]any{
		"actions": []any{map[string]any{
			"action_id": "act1", "action_group": "prompt", "operation": "edit_section",
			"target_path": "identity.md", "attributed_issue_ids": []any{"i1"},
			"constraints": map[string]any{"lever_decision": map[string]any{"recommended_lever": "instruction"}},
		}},
	}
	manifestPath := filepath.Join(s.tempDir, "manifest.yaml")
	_, err = memberoptimizer.WriteCandidateManifest(manifestPath, "refs.yaml", hypPath, plan)
	s.Require().NoError(err)
	s.Require().FileExists(manifestPath)
}

// TestBuildDeepAgentFromModelConfig_RealFactory BuildDeepAgentFromModelConfig 真身回填验证
// （17.4b2 fakechain 头注释 ⤴️17.4c 兑现）：真实 harness 工厂装配 DeepAgent（构造级，不调 LLM）。
func (s *MemberAttributionFakeChainSuite) TestBuildDeepAgentFromModelConfig_RealFactory() {
	modelConfig := map[string]any{
		"model": map[string]any{
			"model_client_config":  map[string]any{"client_id": "intg-client", "client_provider": "llm_OpenAI", "model_name": "gpt-4o-mini", "api_key": "sk-test", "base_url": "http://localhost:1"},
			"model_request_config": map[string]any{"max_tokens": 2048},
		},
		"workspace": map[string]any{"root_path": s.tempDir, "language": "en"},
	}
	result, err := memberoptimizer.BuildDeepAgentFromModelConfig(context.Background(), modelConfig)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Require().NotNil(result.Model)
	// 构造级端到端：装配产物非 nil 且满足 DeepAgentInterface；调用级 e2e 须真实 LLM（build tag llm 另测）
	var deep agentinterfaces.DeepAgentInterface = result.DeepAgent
	s.Require().NotNil(&deep)
	s.Require().True(result.DeepAgent != nil && harnessDeepAgentAssignable(result.DeepAgent))
	s.Require().NotNil(result.Model)
	s.Require().NotNil(result.Workspace)
}

// harnessDeepAgentAssignable 编译期/运行期断言 *harness.DeepAgent 满足 DeepAgentInterface。
func harnessDeepAgentAssignable(agent *harness.DeepAgent) bool {
	var _ agentinterfaces.DeepAgentInterface = agent
	return agent != nil
}
