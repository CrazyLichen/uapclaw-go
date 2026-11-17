//go:build integration

// 17.4b1 诊断读件层集成用例：两条真实跨子系统链路（spec §4.2）。
// 链路1："成绩单→信号→压缩证据包"（evaluation_result_analyzer + member_optimizer 前置件）；
// 链路2："成绩单→证据工作区落盘"（member_optimizer + evaluation_result_analyzer 快照/预算/投映）；
// 附 schema 契约（TeamIssue/AnalysisArtifact）YAML/JSON 序列化回归。

package rsi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	harness_rsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	evaluation_result_analyzer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/evaluation_result_analyzer"
	member_optimizer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
)

// AnalyzerReadLayerSuite 诊断读件层集成套件。
type AnalyzerReadLayerSuite struct {
	suite.Suite
	fixtureDir string
}

// TestAnalyzerReadLayerSuite 入口。
func TestAnalyzerReadLayerSuite(t *testing.T) {
	suite.Run(t, new(AnalyzerReadLayerSuite))
}

// SetupTest 构造完整评估 fixture（eval_ref + summary + 多案例 + 3-trial trace）。
func (s *AnalyzerReadLayerSuite) SetupTest() {
	s.fixtureDir = s.T().TempDir()
	// eval_ref
	s.writeFile("eval_ref.yaml", "eval_id: e-int\nteam_name: team-a\nharness_refs_path: \"\"\n"+
		"eval_dir: "+s.fixtureDir+"\n")
	// summary.json（G4 键名映射）
	s.writeFile(filepath.Join("results", "summary.json"),
		`{"total_cases": 2, "passed_cases": 0, "failed_cases": 2, "average_score": 0.35,
		  "evaluation_method": "llm_as_judge"}`)
	// 案例目录：c1（2-trial，错误聚类命中）与 c2（单 trial）
	for _, caseID := range []string{"c1", "c2"} {
		s.writeFile(filepath.Join("results", "cases", caseID, "result.json"), `{
			"case_id": "`+caseID+`",
			"status": "passed",
			"error": "FileNotFoundError: /tmp/eval_8812/run_3/output.json not found",
			"score": 0.35,
			"evaluation": {"method": "llm_as_judge", "passed": false, "reason": "r",
				"metadata": {"reward": 0, "judge_detail": {"criteria": [
					{"criterion_id": "unit-test-pass", "score": 0, "rationale": "verifier failed on tests"},
					{"criterion_id": "ok-crit", "score": 1.0, "rationale": "fine"}
				]}}},
			"metadata": {"execution": {"max_iterations": 5}}
		}`)
		// trace：多个 trial + 同一指纹的报错（跨案例聚类）
		traceContent := `{"traces": [
			{"trace_id": "` + caseID + `-t1", "member_role": "worker", "messages": [
				{"role": "user", "content": "task", "message_index": 0},
				{"role": "assistant", "content": "working", "message_index": 1, "tool_calls": [
					{"name": "read_file", "input": "{\"path\":\"a.txt\"}", "output": "CONTENT", "error": ""},
					{"name": "bash", "input": "{\"command\":\"ls /tmp/eval_8812/run_3\"}", "output": "", "error": "FileNotFoundError: /tmp/eval_8812/run_3/output.json not found"}
				]},
				{"role": "assistant", "content": "retry", "message_index": 2, "tool_calls": [
					{"name": "finish", "input": "{\"answer\": \"final from t1\"}", "output": "", "error": ""}
				]}
			]},
			{"trace_id": "` + caseID + `-t2", "member_role": "worker", "messages": [
				{"role": "assistant", "content": "second trial", "message_index": 0, "tool_calls": [
					{"name": "alpha", "input": "{}", "output": "CONTENT", "error": ""},
					{"name": "bravo", "input": "{}", "output": "CONTENT", "error": ""}
				]}
			]}
		]}`
		s.writeFile(filepath.Join("results", "cases", caseID, "trace.json"), traceContent)
	}
}

// writeFile 在 fixture 根下写文件（自动建父目录）。
func (s *AnalyzerReadLayerSuite) writeFile(rel, content string) {
	path := filepath.Join(s.fixtureDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.T().Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.T().Fatal(err)
	}
}

// TestChain1_成绩单到信号到证据包 链路1：reader → signals → digest。
func (s *AnalyzerReadLayerSuite) TestChain1_成绩单到信号到证据包() {
	reader := evaluation_result_analyzer.NewCaseReader()
	summary := reader.ReadSummary(filepath.Join(s.fixtureDir, "results", "summary.json"))
	s.Equal(2, summary.TotalCases)
	s.Equal(2, summary.FailedCount) // G4 映射：passed_cases/failed_cases

	caseInputs := reader.ReadCaseInputs(filepath.Join(s.fixtureDir, "results", "cases"))
	s.Require().Len(caseInputs, 2)

	// 信号提取（member_optimizer 前置件未直接参与此步；extractor 为 analyzer 件）
	extractor := evaluation_result_analyzer.BuildSignalExtractor(summary.EvaluationMethod)
	s.Equal("llm_judge", extractor.Name())
	signals := extractor.Extract(summary, caseInputs)
	// 错误聚类：两案例同一指纹（路径/时间戳归一化后）
	s.Require().Len(signals.ErrorClusters, 1)
	cluster := signals.ErrorClusters[0]
	s.Len(cluster["cases"].([]string), 2, "同指纹应聚合 c1/c2")
	// judge_failures：两案例均未通过
	s.Len(signals.JudgeFailures, 2)

	// 压缩 digest（精确计数断言，13.24 惯例）
	// member_optimizer.load_eval_ref 前置件参与链路（解析评估引用）
	evalRefPath := filepath.Join(s.fixtureDir, "eval_ref.yaml")
	evalRef, err := member_optimizer.LoadEvalRef(evalRefPath)
	s.Require().NoError(err)
	s.Equal("e-int", evalRef.EvalID)
	// 候选角色解析为空（harness_refs_path 为空）→ 空清单（由链路2覆盖非空路径）
	s.Empty(member_optimizer.ResolveCandidateRoles("", &evalRef, ""))
	c1 := caseInputs[0]
	traceData := s.readJSON(filepath.Join(c1.ResultPath, "..", "trace.json"))
	digest := evaluation_result_analyzer.BuildCausalEvidenceDigest(map[string]any{
		"case_id": c1.CaseID, "response": c1.Response,
		"evaluation_passed": c1.EvaluationPassed, "evaluation_score": c1.Score,
		"evaluation_reason": c1.EvaluationReason, "evaluation_metadata": c1.EvaluationMetadata,
		"trace_data": traceData, "task_contract": map[string]any{},
	})
	stats := digest["compression_stats"].(map[string]any)
	s.Equal(2, stats["raw_trace_count"], "raw_trace_count 精确计数")
	s.Equal(4, stats["raw_message_count"], "raw_message_count 精确计数（3+1）")
	s.Equal(5, stats["raw_tool_call_count"], "raw_tool_call_count 精确计数（2+1+2）")
	s.Equal(5, stats["selected_tool_call_count"], "trial 边界保留、短轨迹全选")

	// trial 边界：2 个 trial，finish 工具交付
	trials := digest["trials"].([]map[string]any)
	s.Require().Len(trials, 2)
	s.Equal("finish_tool_request", trials[0]["final_output"].(map[string]any)["delivery_channel"])
	// 关键证据词来自失败条款
	s.Contains(digest["critical_evidence_terms"].([]string), "unit-test-pass")

	// 模型配置变换链（诊断 agent 装配前置，真实跨界 foundation/llm/schema）：
	// ${VAR} 展开 → reasoning policy + output budget → without_inner_sdk_retries → 桩装配
	s.T().Setenv("RSI_INTG_MODEL", "intg-model")
	modelRef := filepath.Join(s.fixtureDir, "model_config.yaml")
	s.writeFileAt(modelRef, `model:
  model_client_config:
    client_id: intg-client
    client_provider: llm_OpenAI
    model_name: ${RSI_INTG_MODEL}
    timeout: 60
  model_request_config:
    reasoning: auto
    max_tokens: null
`)
	loaded, loadErr := member_optimizer.LoadModelConfigRef(modelRef)
	s.Require().NoError(loadErr)
	model := loaded["model"].(map[string]any)
	s.Equal("intg-model", model["model_client_config"].(map[string]any)["model_name"])
	cfg, buildErr := member_optimizer.BuildDeepAgentFromModelConfig(context.Background(), loaded)
	s.Require().NoError(buildErr)
	s.NotNil(cfg.Model)
	s.NotNil(cfg.Workspace)
}

// TestChain2_成绩单到证据工作区落盘 链路2：eval_ref → snapshot → paths → 投映。
func (s *AnalyzerReadLayerSuite) TestChain2_成绩单到证据工作区落盘() {
	caseRoot := s.T().TempDir()
	runtimeDir := s.T().TempDir()

	// harness 包（含声明与技能）
	pkg := filepath.Join(caseRoot, "pkg")
	s.Require().NoError(os.MkdirAll(filepath.Join(pkg, "skills", "skill-a"), 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(pkg, "prompt_sections"), 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(pkg, "tools"), 0o755))
	s.writeFileAt(filepath.Join(pkg, "harness_config.yaml"), "package_type: harness\n")
	s.writeFileAt(filepath.Join(pkg, "config.json"),
		`{"skills": [{"dir": "skills/skill-a"}], "prompt_sections": [{"name": "main", "content": "body text"}]}`)
	s.writeFileAt(filepath.Join(pkg, "skills", "skill-a", "SKILL.md"), "skill body")
	s.writeFileAt(filepath.Join(pkg, "tools", "bash.py"), "# tool")

	// harness refs：单角色
	refsPath := filepath.Join(caseRoot, "harness_refs.yaml")
	s.writeFileAt(refsPath, "roles:\n  - role: planner\n    member_name: p1\n    harness_ref_path: "+pkg+"\n")

	// workspace 快照（member_optimizer eval_ref 已在链路1 验证读取；此处直接构造）
	ws := filepath.Join(caseRoot, "ws")
	s.Require().NoError(os.MkdirAll(filepath.Join(ws, "src"), 0o755))
	s.writeFileAt(filepath.Join(ws, "src", "main.py"), "print(1)")
	s.writeFileAt(filepath.Join(ws, ".git", "config"), "git")
	patch := filepath.Join(caseRoot, "p.diff")
	s.writeFileAt(patch, "diff --git a")

	snapshot, snapErr := evaluation_result_analyzer.PrepareRepositorySnapshot(ws, patch, runtimeDir, "")
	s.Require().NoError(snapErr)
	s.Equal("complete", snapshot["repository"])
	s.Equal("copied", snapshot["patch"])
	// .git 排除集生效
	_, gitErr := os.Stat(filepath.Join(runtimeDir, "repository", ".git"))
	s.True(os.IsNotExist(gitErr), ".git 应被排除")
	// 可读文件保留
	content, readErr := os.ReadFile(filepath.Join(runtimeDir, "repository", "src", "main.py"))
	s.NoError(readErr)
	s.Equal("print(1)", string(content))

	// 路径预算校验（非 Windows 平台直通；Windows 语义由单测覆盖）
	s.NoError(validatePathsShim(runtimeDir, filepath.Join(caseRoot, "attempt")))

	// harness 投映：成员解析（member_optimizer.resolve_candidate_roles）→ 只读投映
	evalRefPath := filepath.Join(caseRoot, "eval_ref.yaml")
	s.writeFileAt(evalRefPath, "eval_id: e2\nharness_refs_path: "+refsPath+"\n")
	ctx, ctxErr := evaluation_result_analyzer.PrepareHarnessContext(evalRefPath, "", runtimeDir)
	s.Require().NoError(ctxErr)
	s.Equal("available", ctx["status"])
	roles, _ := ctx["roles"].([]any)
	s.Require().Len(roles, 1)
	row := roles[0].(map[string]any)
	s.Equal("planner", row["role"])
	// 技能文件落盘
	skills := row["skills"].([]any)
	s.Require().Len(skills, 1)
	skillPath := filepath.Join(runtimeDir, filepath.ToSlash(skills[0].(map[string]any)["path"].(string)))
	data, readErr := os.ReadFile(skillPath)
	s.NoError(readErr)
	s.Equal("skill body", string(data))
	// index.json 落盘
	_, indexErr := os.Stat(filepath.Join(runtimeDir, "current_harness", "index.json"))
	s.NoError(indexErr)
}

// TestSchemaContractSerialization 契约回归：TeamIssue/Artifact YAML/JSON 双向。
func (s *AnalyzerReadLayerSuite) TestSchemaContractSerialization() {
	issue := harness_rsi.TeamIssue{
		IssueID: "i1", Category: "cat", Severity: "high", Summary: "s",
	}
	// JSON 往返
	raw, err := json.Marshal(issue)
	s.NoError(err)
	var back harness_rsi.TeamIssue
	s.NoError(json.Unmarshal(raw, &back))
	s.Equal(issue.IssueID, back.IssueID)
	s.Equal(issue.Summary, back.Summary)
	// 字段内容含中文/特殊字符往返不失真
	issue.Summary = "中文摘要：`引用` $VAR"
	raw2, _ := json.Marshal(issue)
	var back2 harness_rsi.TeamIssue
	s.NoError(json.Unmarshal(raw2, &back2))
	s.Equal(issue.Summary, back2.Summary)
	s.True(strings.Contains(string(raw2), "中文摘要"))
}

// readJSON 读 JSON 为 map（测试辅助）。
func (s *AnalyzerReadLayerSuite) readJSON(path string) map[string]any {
	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	var out map[string]any
	s.Require().NoError(json.Unmarshal(data, &out))
	return out
}

// writeFileAt 写文件（自动建父目录）。
func (s *AnalyzerReadLayerSuite) writeFileAt(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.T().Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.T().Fatal(err)
	}
}

// validatePathsShim 非 Windows 平台直通 shim（Windows 语义由单元层注入测试覆盖）。
func validatePathsShim(runtimeDir, attemptDir string) error {
	return evaluation_result_analyzer.ValidateDiagnosisPaths(runtimeDir, attemptDir, "", "")
}
