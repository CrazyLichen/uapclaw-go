package memberoptimizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
)

// buildTestAnalysisRef 构造含单个可执行契约问题的 analysis_ref。
func buildTestAnalysisRef(t *testing.T, dir string, issueMetadata map[string]any) string {
	t.Helper()
	analysis := map[string]any{
		"analysis_id":  "a1",
		"issues":       []any{map[string]any{"issue_id": "i1", "category": "capability missing", "severity": "high", "summary": "s", "affected_cases": []any{"c1"}, "recommendation": "rec", "optimization_target": "member_harness", "target_members": []any{"solver"}, "metadata": issueMetadata}},
		"harness_refs": []any{},
	}
	data, err := yamlMarshalForTest(analysis)
	require.NoError(t, err)
	path := filepath.Join(dir, "analysis_ref.yaml")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func TestCompileOptimizationHypotheses_契约编译(t *testing.T) {
	dir := t.TempDir()
	analysisPath := buildTestAnalysisRef(t, dir, map[string]any{
		"attribution": map[string]any{
			"target_ref": "identity.md", "root_cause": "vague", "critical_mistake": "cm", "general_mechanism": "gm",
			"decision_contract": map[string]any{"acceptance_observable": "file exists", "required_action": "act"},
		},
	})
	// 案例输入
	casesPath := filepath.Join(dir, "cases.yaml")
	require.NoError(t, os.WriteFile(casesPath, []byte("cases:\n  c1:\n    input: do the thing\n"), 0o644))
	out := filepath.Join(dir, "hyp.yaml")
	path, err := CompileOptimizationHypotheses(analysisPath, []map[string]any{{"case_id": "c1", "input": "do the thing"}}, out)
	require.NoError(t, err)
	require.Equal(t, out, path)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "version: 4")
	require.Contains(t, text, "source_analysis_ref_path: "+filepath.Clean(analysisPath))
	require.Contains(t, text, "source_issue_id: i1")
	require.Contains(t, text, "hypothesis_id: hyp_")
	require.Contains(t, text, "content_sha256: ")
	require.Contains(t, text, "deficiency_class: harness_deficiency")
	require.Contains(t, text, "intent: corrective")
	require.Contains(t, text, "diagnostic_lens: capability_gap") // category 含 missing/capability
}

func TestCompileOptimizationHypotheses_缺acceptance拒绝(t *testing.T) {
	dir := t.TempDir()
	analysisPath := buildTestAnalysisRef(t, dir, map[string]any{
		"attribution": map[string]any{"target_ref": "identity.md"},
	})
	out := filepath.Join(dir, "hyp.yaml")
	_, err := CompileOptimizationHypotheses(analysisPath, nil, out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexecutable decision_contract")
	require.Contains(t, err.Error(), "i1")
}

func TestCompileOptimizationHypotheses_证据不足分类(t *testing.T) {
	dir := t.TempDir()
	analysisPath := buildTestAnalysisRef(t, dir, map[string]any{
		"attribution": map[string]any{
			"target_ref":        "unassigned",
			"decision_contract": map[string]any{"acceptance_observable": "observed", "scope_boundary": []any{"b1", "b1", "b2"}},
		},
	})
	out := filepath.Join(dir, "hyp.yaml")
	_, err := CompileOptimizationHypotheses(analysisPath, nil, out)
	require.NoError(t, err)
	data, _ := os.ReadFile(out)
	require.Contains(t, string(data), "deficiency_class: insufficient_evidence")
	// scope_boundary 去重（回读校验 decision_contract.scope_boundary == [b1 b2]）
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(data, &doc))
	hypList := doc["hypotheses"].([]any)
	require.Len(t, hypList, 1)
	hyp := hypList[0].(map[string]any)
	contract := hyp["decision_contract"].(map[string]any)
	require.Equal(t, []any{"b1", "b2"}, contract["scope_boundary"])
}

func TestLoadOptimizationHypotheses_往返校验(t *testing.T) {
	dir := t.TempDir()
	analysisPath := buildTestAnalysisRef(t, dir, map[string]any{
		"attribution": map[string]any{
			"target_ref":        "identity.md",
			"decision_contract": map[string]any{"acceptance_observable": "file exists"},
		},
	})
	out := filepath.Join(dir, "hyp.yaml")
	_, err := CompileOptimizationHypotheses(analysisPath, nil, out)
	require.NoError(t, err)
	hypotheses, err := LoadOptimizationHypotheses(out)
	require.NoError(t, err)
	require.Len(t, hypotheses, 1)
	require.NotEmpty(t, hypotheses[0]["content_sha256"])
	// 不存在文件返回空
	empty, err := LoadOptimizationHypotheses(filepath.Join(dir, "missing.yaml"))
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestLoadOptimizationHypotheses_内容漂移拒绝(t *testing.T) {
	dir := t.TempDir()
	analysisPath := buildTestAnalysisRef(t, dir, map[string]any{
		"attribution": map[string]any{
			"target_ref":        "identity.md",
			"decision_contract": map[string]any{"acceptance_observable": "file exists"},
		},
	})
	out := filepath.Join(dir, "hyp.yaml")
	_, err := CompileOptimizationHypotheses(analysisPath, nil, out)
	require.NoError(t, err)
	// 篡改 required_behavior → 摘要失配
	raw, err := os.ReadFile(out)
	require.NoError(t, err)
	tampered := replaceAllForTest(string(raw), "source_issue_id: i1", "source_issue_id: i1-tampered")
	require.NoError(t, os.WriteFile(out, []byte(tampered), 0o644))
	_, err = LoadOptimizationHypotheses(out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "content digest mismatch")
}

func TestPublicCaseInput_键序与transport剥离(t *testing.T) {
	// dict user_message
	got := PublicCaseInput(map[string]any{"input": map[string]any{"user_message": "execution environment: x\nreal task"}})
	require.Equal(t, "real task", got)
	// 字符串
	got = PublicCaseInput(map[string]any{"query": "hello"})
	require.Equal(t, "hello", got)
	// 其他类型 JSON 化
	got = PublicCaseInput(map[string]any{"task_input": map[string]any{"b": 1, "a": 2}})
	require.Equal(t, `{"a": 2, "b": 1}`, got)
	// 无已知键 → 空
	require.Equal(t, "", PublicCaseInput(map[string]any{"other": 1}))
}

func TestDiagnosticLens(t *testing.T) {
	require.Equal(t, "capability_gap", DiagnosticLens(harnessrsi.TeamIssue{Category: "capability"}))
	require.Equal(t, "capability_gap", DiagnosticLens(harnessrsi.TeamIssue{Summary: "missing skill"}))
	require.Equal(t, "failure", DiagnosticLens(harnessrsi.TeamIssue{Category: "x", Summary: "y"}))
}

func TestDecisionContract_激活相位推断(t *testing.T) {
	// 供应 supplied
	contract := DecisionContract(
		harnessrsi.TeamIssue{Summary: "s", Recommendation: "r", Metadata: map[string]any{}},
		map[string]any{"decision_contract": map[string]any{
			"acceptance_observable": "obs", "activation_phase": "PRE_SUBMISSION",
			"wrong_decision": "w", "causal_distinction": "c", "required_action": "a",
		}},
	)
	require.Equal(t, "pre_submission", contract["activation_phase"])
	// 未供应 → required_action/recommendation 含 finalize → pre_submission
	contract = DecisionContract(
		harnessrsi.TeamIssue{Summary: "s", Recommendation: "finalize the report", Metadata: map[string]any{}},
		map[string]any{"decision_contract": map[string]any{"acceptance_observable": "obs"}},
	)
	require.Equal(t, "pre_submission", contract["activation_phase"])
	// after diagnosis 标记 → post_diagnosis
	contract = DecisionContract(
		harnessrsi.TeamIssue{Summary: "s", Recommendation: "after diagnosis act", Metadata: map[string]any{}},
		map[string]any{"decision_contract": map[string]any{"acceptance_observable": "obs"}},
	)
	require.Equal(t, "post_diagnosis", contract["activation_phase"])
	// 默认 task_start
	contract = DecisionContract(
		harnessrsi.TeamIssue{Summary: "s", Recommendation: "r", Metadata: map[string]any{}},
		map[string]any{"decision_contract": map[string]any{"acceptance_observable": "obs"}},
	)
	require.Equal(t, "task_start", contract["activation_phase"])
	// wrong_decision 回落链：supplied → critical_mistake → summary
	contract = DecisionContract(
		harnessrsi.TeamIssue{Summary: "s", Metadata: map[string]any{}},
		map[string]any{"critical_mistake": "cm"},
	)
	require.Equal(t, "cm", contract["wrong_decision"])
}

func TestEvidenceRefs_过滤空值(t *testing.T) {
	refs := EvidenceRefs([]map[string]any{
		{"case_id": "c1", "trace_id": "t1", "role": "", "step_pointer": nil},
		{"role": ""},
	})
	require.Len(t, refs, 1)
	require.Equal(t, map[string]string{"case_id": "c1", "trace_id": "t1"}, refs[0])
}

func TestWriteCandidateManifest(t *testing.T) {
	dir := t.TempDir()
	analysisPath := buildTestAnalysisRef(t, dir, map[string]any{
		"attribution": map[string]any{
			"target_ref":        "identity.md",
			"decision_contract": map[string]any{"acceptance_observable": "file exists"},
		},
	})
	hypPath := filepath.Join(dir, "hyp.yaml")
	_, err := CompileOptimizationHypotheses(analysisPath, nil, hypPath)
	require.NoError(t, err)
	plan := map[string]any{
		"actions": []any{map[string]any{
			"action_id": "act1", "action_group": "prompt", "operation": "edit_section",
			"target_path": "identity.md", "attributed_issue_ids": []any{"i1"},
			"constraints": map[string]any{"lever_decision": map[string]any{"recommended_lever": "instruction"}},
		}},
	}
	manifestPath := filepath.Join(dir, "manifest.yaml")
	path, err := WriteCandidateManifest(manifestPath, "refs.yaml", hypPath, plan)
	require.NoError(t, err)
	require.Equal(t, manifestPath, path)
	data, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "version: 2")
	require.Contains(t, text, "status: planned")
	require.Contains(t, text, "hypothesis_ids:")
	require.Contains(t, text, "action_id: act1")
	require.Contains(t, text, "lever_decision:")
}
