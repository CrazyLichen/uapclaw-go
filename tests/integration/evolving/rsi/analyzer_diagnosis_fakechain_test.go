//go:build integration

// 17.4b2 诊断运行层集成用例（spec §4.2 方案③ fake 链路）：
// "成绩单→逐案诊断（fake agent JSON 重试链）→聚合→issues.yaml/analysis_ref.yaml"
// 跨 evaluation_result_analyzer + harness_rsi(schema) + member_optimizer ≥3 子系统。
// 真 DeepAgent 端到端用例 ⤴️17.4c（BuildDeepAgentFromModelConfig 真身回填后补）。

package rsi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"strings"

	harness_rsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	rsiconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	evaluation_result_analyzer "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluation_result_analyzer"
	member_optimizer "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/member_optimizer"
)

// ──────────────────────────── 测试替身（对齐 harness/rails/agent_fake_test.go 形制） ────────────────────────────

// diagnosisMockSession SessionFacade mock。
type diagnosisMockSession struct {
	state map[string]any
}

func (m *diagnosisMockSession) GetSessionID() string { return "test-session" }
func (m *diagnosisMockSession) UpdateState(data map[string]any) {
	for key, value := range data {
		m.state[key] = value
	}
}
func (m *diagnosisMockSession) GetState(key state.StateKey) (any, error) {
	if key.Type() != state.StateKeyString {
		return nil, nil
	}
	value, ok := m.state[key.String()]
	if !ok {
		return nil, nil
	}
	return value, nil
}
func (m *diagnosisMockSession) DumpState() map[string]any                        { return m.state }
func (m *diagnosisMockSession) WriteStream(_ context.Context, _ any) error       { return nil }
func (m *diagnosisMockSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (m *diagnosisMockSession) GetEnv(_ string, _ ...any) any                    { return nil }
func (m *diagnosisMockSession) Interact(_ context.Context, _ any) error          { return nil }
func (m *diagnosisMockSession) ClearSession(_ context.Context)                   {}
func (m *diagnosisMockSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}
func (m *diagnosisMockSession) GetParentSessionID() string { return "" }

// diagnosisFakeAgent 满足 BaseAgent 接口的 mock（fake 链路中不参与执行）。
type diagnosisFakeAgent struct {
	cbMgr *agentinterfaces.AgentCallbackManager
}

func (f *diagnosisFakeAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *diagnosisFakeAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *diagnosisFakeAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *diagnosisFakeAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{BaseCard: schema.BaseCard{ID: "diagnosis-fake", Name: "DiagnosisFake"}}
}
func (f *diagnosisFakeAgent) Config() agentinterfaces.AgentConfig { return nil }
func (f *diagnosisFakeAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return nil
}
func (f *diagnosisFakeAgent) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return f.cbMgr
}
func (f *diagnosisFakeAgent) SystemPromptBuilder() prompts.SystemPromptBuilderInterface {
	return nil
}
func (f *diagnosisFakeAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *diagnosisFakeAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *diagnosisFakeAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}
func (f *diagnosisFakeAgent) UapswarmProjectDir() string       { return "" }
func (f *diagnosisFakeAgent) SetUapswarmProjectDir(string)     {}
func (f *diagnosisFakeAgent) UapswarmCodeProjectDir() string   { return "" }
func (f *diagnosisFakeAgent) SetUapswarmCodeProjectDir(string) {}
func (f *diagnosisFakeAgent) UapswarmAdapterMode() string      { return "" }
func (f *diagnosisFakeAgent) SetUapswarmAdapterMode(string)    {}

var (
	_ sessioninterfaces.SessionFacade = (*diagnosisMockSession)(nil)
	_ agentinterfaces.BaseAgent       = (*diagnosisFakeAgent)(nil)
)

// ──────────────────────────── 套件 ────────────────────────────

// AnalyzerDiagnosisFakeChainSuite 诊断运行层 fake 链路套件。
type AnalyzerDiagnosisFakeChainSuite struct {
	suite.Suite
	fixtureDir string
}

// TestAnalyzerDiagnosisFakeChainSuite 入口。
func TestAnalyzerDiagnosisFakeChainSuite(t *testing.T) {
	suite.Run(t, new(AnalyzerDiagnosisFakeChainSuite))
}

// SetupTest 构造评估 fixture（eval_ref + 失败案例 + 归一化轨迹 + 模型配置引用）。
func (s *AnalyzerDiagnosisFakeChainSuite) SetupTest() {
	s.fixtureDir = s.T().TempDir()
	s.writeFile("eval_ref.yaml", "eval_id: e-b2\nsummary_path: "+s.fixtureDir+"/results/summary.json\n")
	s.writeFile(filepath.Join("results", "summary.json"),
		`{"total_cases": 1, "passed_cases": 0, "failed_cases": 1, "average_score": 0.2,
		  "evaluation_method": "llm_as_judge"}`)
	s.writeFile("model.yaml",
		"model_client_config:\n  client_provider: OpenAI\n  api_key: test-key\n  api_base: https://example.invalid/v1\nmodel_request_config:\n  model: fake-model\n")
	s.writeFile(filepath.Join("results", "cases", "c1", "result.json"), `{
		"case_id": "c1", "status": "failed", "score": 0.2,
		"evaluation": {"method": "llm_as_judge", "passed": false, "reason": "verifier failed",
			"metadata": {"parsed": {"behaviors": [
				{"id": "b1", "score": 0.2, "reason": "did not finish", "failure_reason": "no output"}
			]}}},
		"metadata": {"execution": {"max_iterations": 3}}
	}`)
	s.writeFile(filepath.Join("results", "cases", "c1", "judge", "normalized_trace.json"), `{"traces": [
		{"trace_id": "c1-t1", "member_role": "worker", "messages": [
			{"role": "assistant", "content": "attempting", "message_index": 1, "tool_calls": [
				{"name": "bash", "input": "{\"command\":\"pytest tests/\"}", "output": "2 failed", "error": ""}
			]}
		]}
	]}`)
}

// writeFile 相对 fixtureDir 写文件。
func (s *AnalyzerDiagnosisFakeChainSuite) writeFile(rel, content string) {
	path := filepath.Join(s.fixtureDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.T().Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.T().Fatal(err)
	}
}

// fakeAgentOutput 脚本化输出：先坏 JSON（触发修复链）再好 JSON。
func (s *AnalyzerDiagnosisFakeChainSuite) fakeAgentOutput() []string {
	good := `{"diagnoses": [{
		"summary": "pytest suite failed on two tests",
		"failure_mode": "test-failure",
		"root_cause": "parser selected legacy mode",
		"critical_mistake": "mode flag not read",
		"general_mechanism": "mode selection must be read from recorded progress",
		"target_ref": "member_harness.worker.prompt",
		"evidence_refs": ["normalized_trace.json"],
		"confidence": "medium",
		"severity": "high",
		"failure_cluster": {"failed_checks": ["t1"], "observable_behavior": "2 failed"},
		"affected_components": ["worker"]
	}]}`
	return []string{"sorry I cannot produce JSON today", good}
}

// TestFakeChainFullPipeline 全链路编排断言。
func (s *AnalyzerDiagnosisFakeChainSuite) TestFakeChainFullPipeline() {
	outputDir := filepath.Join(s.fixtureDir, "out")
	facade := evaluation_result_analyzer.NewEvaluationResultAnalyzer(s.newConfig())
	strategy := facade.Strategy()
	calls := 0
	script := s.fakeAgentOutput()
	strategy.SetTestHooks(
		func(ctx context.Context, workspace, systemPrompt, invocationLogDir string) (agentinterfaces.BaseAgent, error) {
			return &diagnosisFakeAgent{cbMgr: agentinterfaces.NewAgentCallbackManager("diagnosis-fake")}, nil
		},
		func(ctx context.Context, agent agentinterfaces.BaseAgent, prompt string, maxRetries int) (string, error) {
			out := script[min(calls, len(script)-1)]
			calls++
			return out, nil
		},
	)

	refPath, err := facade.Analyze(context.Background(), harness_rsi.EvaluationResultAnalysisInvocation{
		EvalRefPath:    filepath.Join(s.fixtureDir, "eval_ref.yaml"),
		CaseResultsDir: filepath.Join(s.fixtureDir, "results", "cases"),
		OutputDir:      outputDir,
		SourceStage:    "member_stage",
	})
	s.Require().NoError(err)
	s.True(strings.HasSuffix(refPath, "analysis_ref.yaml"))
	s.Equal(2, calls, "JSON 修复链应触发两次调用")
	perCaseRaw, err := os.ReadFile(filepath.Join(outputDir, "per_case_diagnoses.json"))
	s.Require().NoError(err)
	s.Contains(string(perCaseRaw), "parser selected legacy mode")
	refRaw, err := os.ReadFile(refPath)
	s.Require().NoError(err)
	s.Contains(string(refRaw), "analysis_id")
	s.Contains(string(refRaw), "completed")
	issuesRaw, err := os.ReadFile(filepath.Join(outputDir, "issues.yaml"))
	s.Require().NoError(err)
	s.Contains(string(issuesRaw), "member_harness.worker.prompt")
	s.Contains(string(issuesRaw), "issue_001")
	entries, err := os.ReadDir(filepath.Join(outputDir, "diagnosis_attempts"))
	s.Require().NoError(err)
	s.Len(entries, 1, "诊断尝试归档目录应存在")
}

// newConfig 构造分析器配置（模型引用文件由 fixture 提供）。
func (s *AnalyzerDiagnosisFakeChainSuite) newConfig() *rsiconfig.EvaluationResultAnalyzerConfig {
	return &rsiconfig.EvaluationResultAnalyzerConfig{
		ModelConfigRef:              filepath.Join(s.fixtureDir, "model.yaml"),
		OutputFilename:              "issues.yaml",
		DiagnosisAgentMaxRetries:    0,
		MaxIssues:                   5,
		EvidenceLimitPerIssue:       3,
		DiagnosisAgentMaxIterations: 3,
	}
}

// TestModelConfigRefLoadCrossPackage 跨子系统核验：member_optimizer 配置载入真实可读。
func (s *AnalyzerDiagnosisFakeChainSuite) TestModelConfigRefLoadCrossPackage() {
	// fixture 顶层即模型配置（Python ref_data.get("model", ref_data) 回退分支）
	refData, err := member_optimizer.LoadModelConfigRef(filepath.Join(s.fixtureDir, "model.yaml"))
	s.Require().NoError(err)
	model := refData
	if m, ok := refData["model"].(map[string]any); ok {
		model = m
	}
	cfg, err := member_optimizer.ValidateTeamModelConfig(member_optimizer.WithoutInnerSDKRetries(model))
	s.Require().NoError(err)
	s.Equal("fake-model", cfg.ModelRequestConfig.ModelName)
}

// TestPerCaseFilteredByPassAndCompleteness 通过案例不进诊断。
func (s *AnalyzerDiagnosisFakeChainSuite) TestPerCaseFilteredByPassAndCompleteness() {
	s.writeFile(filepath.Join("results", "cases", "c2", "result.json"), `{
		"case_id": "c2", "status": "passed", "score": 1.0,
		"evaluation": {"method": "llm_as_judge", "passed": true, "reason": "ok"}
	}`)
	s.writeFile(filepath.Join("results", "summary.json"),
		`{"total_cases": 2, "passed_cases": 1, "failed_cases": 1, "average_score": 0.6,
		  "evaluation_method": "llm_as_judge"}`)
	facade := evaluation_result_analyzer.NewEvaluationResultAnalyzer(s.newConfig())
	calls := 0
	facade.Strategy().SetTestHooks(
		func(ctx context.Context, workspace, systemPrompt, invocationLogDir string) (agentinterfaces.BaseAgent, error) {
			return &diagnosisFakeAgent{cbMgr: agentinterfaces.NewAgentCallbackManager("diagnosis-fake")}, nil
		},
		func(ctx context.Context, agent agentinterfaces.BaseAgent, prompt string, maxRetries int) (string, error) {
			calls++
			return s.fakeAgentOutput()[1], nil
		},
	)
	_, err := facade.Analyze(context.Background(), harness_rsi.EvaluationResultAnalysisInvocation{
		EvalRefPath:    filepath.Join(s.fixtureDir, "eval_ref.yaml"),
		CaseResultsDir: filepath.Join(s.fixtureDir, "results", "cases"),
		OutputDir:      filepath.Join(s.fixtureDir, "out2"),
	})
	s.Require().NoError(err)
	s.Equal(1, calls, "只有失败案例 c1 进诊断")
	perCaseRaw, err := os.ReadFile(filepath.Join(s.fixtureDir, "out2", "per_case_diagnoses.json"))
	s.Require().NoError(err)
	s.Contains(string(perCaseRaw), `"case_id": "c1"`)
	s.NotContains(string(perCaseRaw), `"case_id": "c2"`, "通过案例不应进诊断")
}
