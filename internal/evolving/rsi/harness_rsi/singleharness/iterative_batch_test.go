package singleharness

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	evaluatorpkg "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/evaluator"
)

// writeEvalRef 写评估引用与用例结果文件。
func writeEvalRef(t *testing.T, dir, name string, casesYAML string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	writeTestYAML(t, path, "cases:\n"+casesYAML)
	return path
}

func TestEvalCaseScores(t *testing.T) {
	dir := t.TempDir()
	ref := writeEvalRef(t, dir, "e.yaml",
		"  - case_id: c1\n    score: 0.5\n  - case_id: c2\n    score: 1.0\n  - case_id: \"\"\n    score: 9\n")
	scores, err := evalCaseScores(ref)
	if err != nil {
		t.Fatalf("evalCaseScores: %v", err)
	}
	if len(scores) != 2 || scores["c1"] != 0.5 || scores["c2"] != 1.0 {
		t.Fatalf("分数映射: %v", scores)
	}
}

func TestEvalCaseNativeSignals_回退与维度(t *testing.T) {
	dir := t.TempDir()
	// 用例 1：无 optimization_signals → 回退 eval_ref 分
	result1 := filepath.Join(dir, "r1.json")
	writeTestYAML(t, result1, `{"evaluation":{"metadata":{}}}`)
	// 用例 2：available 信号 + 维度
	result2 := filepath.Join(dir, "r2.json")
	writeTestYAML(t, result2, `{"evaluation":{"metadata":{"optimization_signals":{
		"continuous_score":{"availability":"available","value":0.8,"source":"rubric"},
		"dimensions":{"d1":{"availability":"available","value":0.6}}}}}}`)
	ref := filepath.Join(dir, "e.yaml")
	writeTestYAML(t, ref, "cases:\n"+
		"  - case_id: c1\n    score: 0.4\n    result_path: "+result1+"\n"+
		"  - case_id: c2\n    score: 0.1\n    result_path: "+result2+"\n")
	signals, err := evalCaseNativeSignals(ref)
	if err != nil {
		t.Fatalf("evalCaseNativeSignals: %v", err)
	}
	if signals["c1"]["source"] != "eval_ref_score_fallback" {
		t.Fatalf("c1 应回退 eval_ref 分: %v", signals["c1"])
	}
	if signals["c1"]["score"].(float64) != 0.4 {
		t.Fatalf("c1 回退分 0.4: %v", signals["c1"])
	}
	if signals["c2"]["score"].(float64) != 0.8 || signals["c2"]["source"] != "rubric" {
		t.Fatalf("c2 应读原生分: %v", signals["c2"])
	}
	dims := signals["c2"]["dimensions"].(map[string]any)
	if dims["d1"].(float64) != 0.6 {
		t.Fatalf("维度值: %v", dims)
	}
}

func TestPairedScoreDelta_缺失返回nil(t *testing.T) {
	if got := pairedScoreDelta(map[string]float64{"c1": 0.5}, map[string]float64{"c1": 0.7}, "c1"); got == nil || math.Abs(*got-0.2) > 1e-9 {
		t.Fatalf("配对差 0.2: %v", got)
	}
	if got := pairedScoreDelta(map[string]float64{}, map[string]float64{"c1": 0.7}, "c1"); got != nil {
		t.Fatalf("源缺失应 nil: %v", got)
	}
}

func TestNativeDimensionDeltas(t *testing.T) {
	source := map[string]map[string]any{
		"c1": {"dimensions": map[string]any{"d1": 0.5, "d2": 1.0}},
	}
	candidate := map[string]map[string]any{
		"c1": {"dimensions": map[string]any{"d1": 0.8, "d2": 0.5}},
	}
	byCase, aggregate := nativeDimensionDeltas(source, candidate, map[string]struct{}{"c1": {}})
	if math.Abs(byCase["c1"]["d1"]-0.3) > 1e-9 || math.Abs(byCase["c1"]["d2"]+0.5) > 1e-9 {
		t.Fatalf("逐用例维度差: %v", byCase["c1"])
	}
	if aggregate == nil || math.Abs(*aggregate+0.1) > 1e-9 {
		t.Fatalf("聚合均值 -0.1: %v", aggregate)
	}
	_, aggregate = nativeDimensionDeltas(map[string]map[string]any{}, map[string]map[string]any{}, map[string]struct{}{"c1": {}})
	if aggregate != nil {
		t.Fatalf("无差值应 nil: %v", aggregate)
	}
}

func TestEpochFullSourceEvalRef_前缀与用例集过滤(t *testing.T) {
	dir := t.TempDir()
	full := writeEvalRef(t, dir, "full.yaml", "  - case_id: c1\n    score: 1.0\n")
	partial := writeEvalRef(t, dir, "partial.yaml", "  - case_id: c2\n    score: 1.0\n")
	state := map[string]any{"completed_batches": map[string]any{
		"epoch_001:b000": map[string]any{"source_eval_ref_path": partial},
		"epoch_001:b001": map[string]any{"source_eval_ref_path": full},
		"epoch_002:b000": map[string]any{"source_eval_ref_path": full},
	}}
	got := epochFullSourceEvalRef(state, 1, map[string]struct{}{"c1": {}})
	if got != full {
		t.Fatalf("应选覆盖全集的 epoch_001 评估: %s", got)
	}
	if got := epochFullSourceEvalRef(state, 3, map[string]struct{}{"c1": {}}); got != "" {
		t.Fatalf("无匹配应空串: %s", got)
	}
}

func TestEvalVerifierStatuses(t *testing.T) {
	dir := t.TempDir()
	result := filepath.Join(dir, "r.json")
	writeTestYAML(t, result, `{"evaluation":{"metadata":{"requirement_results":{"items":[
		{"requirement_id":"t1","group":"fail_to_pass","passed":true,"score":1.0}]}}}}`)
	ref := filepath.Join(dir, "e.yaml")
	writeTestYAML(t, ref, "cases:\n  - case_id: c1\n    result_path: "+result+"\n")
	statuses, err := evalVerifierStatuses(ref)
	if err != nil {
		t.Fatalf("evalVerifierStatuses: %v", err)
	}
	requirements, _ := statuses["c1"]["requirement_results"].([]map[string]any)
	if len(requirements) != 1 || requirements[0]["requirement_id"] != "t1" {
		t.Fatalf("需求结果: %v", statuses)
	}
}

func TestVerifierDeltasByCase_新增与回归(t *testing.T) {
	dir := t.TempDir()
	srcResult := filepath.Join(dir, "src.json")
	writeTestYAML(t, srcResult, `{"evaluation":{"metadata":{"requirement_results":{"items":[
		{"requirement_id":"t1","group":"fail_to_pass","passed":true,"score":1.0},
		{"requirement_id":"t2","group":"fail_to_pass","passed":false,"score":0.0}]}}}}`)
	candResult := filepath.Join(dir, "cand.json")
	writeTestYAML(t, candResult, `{"evaluation":{"metadata":{"requirement_results":{"items":[
		{"requirement_id":"t1","group":"fail_to_pass","passed":true,"score":1.0},
		{"requirement_id":"t2","group":"fail_to_pass","passed":true,"score":1.0},
		{"requirement_id":"t3","group":"pass_to_pass","passed":false,"score":0.0}]}}}}`)
	source := filepath.Join(dir, "s.yaml")
	writeTestYAML(t, source, "cases:\n  - case_id: c1\n    result_path: "+srcResult+"\n")
	candidate := filepath.Join(dir, "c.yaml")
	writeTestYAML(t, candidate, "cases:\n  - case_id: c1\n    result_path: "+candResult+"\n")
	deltas, err := verifierDeltasByCase(source, candidate, map[string]struct{}{"c1": {}})
	if err != nil {
		t.Fatalf("verifierDeltasByCase: %v", err)
	}
	delta := deltas["c1"]
	if newly, _ := delta["newly_passed_fail_to_pass"].([]string); len(newly) != 1 || newly[0] != "t2" {
		t.Fatalf("t2 应新增通过: %v", delta["newly_passed_fail_to_pass"])
	}
	// Python: regressed = source_passed ∩ candidate_failed——t3 源未通过，不算回归
	if regressed, _ := delta["regressed_pass_to_pass"].([]string); len(regressed) != 0 {
		t.Fatalf("t3 源未通过不应计回归: %v", delta["regressed_pass_to_pass"])
	}
	if delta["partial_progress"] != true {
		t.Fatalf("部分契约进展应为 true: %v", delta["partial_progress"])
	}
}

func TestCandidateInterventionExcerptsByCase_标签与边界(t *testing.T) {
	capabilities := []map[string]any{
		{
			"action_group":    "prompt",
			"target_path":     "/h/prompt.md",
			"intervention":    "改动 A",
			"target_case_ids": []any{"c1"},
		},
		{"action_group": "skill", "intervention": "改动 B"},
	}
	got := candidateInterventionExcerptsByCase(capabilities, map[string]struct{}{"c1": {}})
	if !strings.Contains(got["c1"], "[prompt:/h/prompt.md]\n改动 A") {
		t.Fatalf("带标签块: %q", got["c1"])
	}
	if !strings.Contains(got["c1"], "改动 B") {
		t.Fatalf("无目标能力应落到全部目标: %q", got["c1"])
	}
	// 空干预跳过
	got = candidateInterventionExcerptsByCase(
		[]map[string]any{{"intervention": "  "}}, map[string]struct{}{"c1": {}})
	if len(got) != 0 {
		t.Fatalf("空干预应无输出: %v", got)
	}
}

func TestBoundedCandidateText(t *testing.T) {
	if got := boundedCandidateText("short"); got != "short" {
		t.Fatalf("短文本直通: %q", got)
	}
	long := strings.Repeat("a", 3001) + strings.Repeat("b", 3001)
	got := boundedCandidateText(long)
	if !strings.Contains(got, "... candidate patch omitted ...") {
		t.Fatalf("截断标记: %q", got[:80])
	}
	if len(got) != 6000+len("\n... candidate patch omitted ...\n") {
		t.Fatalf("截断长度: %d", len(got))
	}
}

func TestAverageCaseScores(t *testing.T) {
	scores := map[string]float64{"c1": 1.0, "c2": 0.5, "c3": 0.0}
	if got := averageCaseScores(scores, map[string]struct{}{"c1": {}, "c2": {}}); got != 0.75 {
		t.Fatalf("均值 0.75: %v", got)
	}
	if got := averageCaseScores(scores, map[string]struct{}{"missing": {}}); got != 0.0 {
		t.Fatalf("无值 0.0: %v", got)
	}
}

func TestCapabilityTargetCaseIDs_回退(t *testing.T) {
	capabilities := []map[string]any{
		{"operation": "add", "target_case_ids": []any{"c1"}},
		{"operation": "remove", "target_case_ids": []any{"c2"}},
	}
	got := capabilityTargetCaseIDs(capabilities, map[string]struct{}{"c9": {}})
	if len(got) != 1 {
		t.Fatalf("只取 add/modify: %v", got)
	}
	got = capabilityTargetCaseIDs(nil, map[string]struct{}{"c9": {}})
	if _, ok := got["c9"]; !ok {
		t.Fatalf("空能力回退: %v", got)
	}
}

func TestSkippedCaseIDs(t *testing.T) {
	dir := t.TempDir()
	ref := writeEvalRef(t, dir, "e.yaml",
		"  - case_id: c1\n    status: skipped\n"+
			"  - case_id: c2\n    status: passed\n    metadata:\n      infrastructure_skip: true\n"+
			"  - case_id: c3\n    status: passed\n")
	skipped, err := skippedCaseIDs(ref)
	if err != nil {
		t.Fatalf("skippedCaseIDs: %v", err)
	}
	if len(skipped) != 2 {
		t.Fatalf("应 2 个跳过: %v", skipped)
	}
}

func TestNonpassingCaseIDs(t *testing.T) {
	dir := t.TempDir()
	ref := writeEvalRef(t, dir, "e.yaml",
		"  - case_id: c1\n    status: failed\n    score: 0\n"+
			"  - case_id: c2\n    status: passed\n    score: 0.5\n"+
			"  - case_id: c3\n    status: passed\n    score: 1.0\n"+
			"  - case_id: c4\n    status: skipped\n    score: 0\n")
	nonpassing, err := nonpassingCaseIDs(ref)
	if err != nil {
		t.Fatalf("nonpassingCaseIDs: %v", err)
	}
	if len(nonpassing) != 2 {
		t.Fatalf("c1/c2 未通过: %v", nonpassing)
	}
}

func TestTaskStartTriggeredSkillNames(t *testing.T) {
	dir := t.TempDir()
	result := filepath.Join(dir, "r.json")
	writeTestYAML(t, result, `{"metadata":{"execution":{"skill_triggers":[
		{"delivered":true,"selected_skill_name":"alpha"},
		{"delivered":false,"selected_skill_name":"beta"},
		{"delivered":true,"selected_skill_name":""}]}}}`)
	got := taskStartTriggeredSkillNames(map[string]any{"result_path": result})
	if len(got) != 1 {
		t.Fatalf("只收 delivered=true 且名字非空: %v", got)
	}
}

func TestCaseUsageTraces_含归一化轨迹(t *testing.T) {
	dir := t.TempDir()
	normalized := filepath.Join(dir, "normalized.json")
	writeTestYAML(t, normalized, `{"steps":[]}`)
	trace := filepath.Join(dir, "trace.json")
	writeTestYAML(t, trace, `{"behavior_trace":{"normalized_trace_path":"`+normalized+`"}}`)
	traces := caseUsageTraces(map[string]any{"trace_path": trace})
	if len(traces) != 2 {
		t.Fatalf("应含适配器+归一化两条: %d", len(traces))
	}
}

func TestInvokedToolNamesByCase_与Skill(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace.json")
	writeTestYAML(t, trace, `{"steps":[
		{"kind":"tool","detail":{"tool_name":"bash","call_result":"ok","call_args":"{\"command\":\"ls\"}"}},
		{"kind":"tool","detail":{"tool_name":"skill","call_result":"ok","call_args":"{\"skill_name\":\"alpha\"}"}}]}`)
	result := filepath.Join(dir, "r.json")
	writeTestYAML(t, result, `{"metadata":{"execution":{"skill_triggers":[
		{"delivered":true,"selected_skill_name":"starter"}]}}}`)
	ref := filepath.Join(dir, "e.yaml")
	writeTestYAML(t, ref, "cases:\n  - case_id: c1\n    result_path: "+result+"\n    trace_path: "+trace+"\n")
	tools, err := invokedToolNamesByCase(ref)
	if err != nil {
		t.Fatalf("invokedToolNamesByCase: %v", err)
	}
	toolSet := tools["c1"]
	if _, ok := toolSet["bash"]; !ok {
		t.Fatalf("bash 应收集: %v", toolSet)
	}
	if _, ok := toolSet["skill_tool"]; !ok {
		t.Fatalf("任务开始触发技能应计 skill_tool: %v", toolSet)
	}
	skills, err := invokedSkillNamesByCase(ref)
	if err != nil {
		t.Fatalf("invokedSkillNamesByCase: %v", err)
	}
	skillSet := skills["c1"]
	if _, ok := skillSet["alpha"]; !ok {
		t.Fatalf("alpha 应收集: %v", skillSet)
	}
	if _, ok := skillSet["starter"]; !ok {
		t.Fatalf("starter 应收集: %v", skillSet)
	}
}

func TestPreEditInvokedNamesByCase_首编辑前(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace.json")
	writeTestYAML(t, trace, `{"steps":[
		{"kind":"tool","detail":{"tool_name":"bash","call_result":"ok","call_args":"{\"command\":\"ls\"}"}},
		{"kind":"tool","detail":{"tool_name":"write_file","call_result":"ok"}}]}`)
	ref := filepath.Join(dir, "e.yaml")
	writeTestYAML(t, ref, "cases:\n  - case_id: c1\n    trace_path: "+trace+"\n")
	names, firstEdits, err := preEditInvokedNamesByCase(ref, "tool", false)
	if err != nil {
		t.Fatalf("preEditInvokedNamesByCase: %v", err)
	}
	if _, ok := names["c1"]["bash"]; !ok {
		t.Fatalf("编辑前 bash 应收集: %v", names["c1"])
	}
	if _, ok := names["c1"]["write_file"]; !ok {
		t.Fatalf("编辑步成功完成同样计入（Python 语义）: %v", names["c1"])
	}
	if firstEdits["c1"] != 1 {
		t.Fatalf("首编辑步号 1: %v", firstEdits)
	}
}

func TestClassifyGateFailure_全分支(t *testing.T) {
	if got := classifyGateFailure(true, "", nil, nil, nil, nil, nil); got != "" {
		t.Fatalf("接受应空: %s", got)
	}
	if got := classifyGateFailure(false, "inconclusive at checkpoint", nil, nil, nil, nil, nil); got != "infrastructure_or_evidence_failure" {
		t.Fatalf("inconclusive: %s", got)
	}
	if got := classifyGateFailure(false, "", nil, nil, nil,
		[]map[string]string{{"runtime_name": "s"}}, nil); got != "natural_skill_activation_failure" {
		t.Fatalf("缺技能调用: %s", got)
	}
	if got := classifyGateFailure(false, "", nil, nil, nil, nil,
		map[string]map[string]any{"c1": {"newly_passed_fail_to_pass": true}}); got != "partial_contract_progress" {
		t.Fatalf("部分契约: %s", got)
	}
	if got := classifyGateFailure(false, "",
		map[string]struct{}{"c1": {}, "c2": {}},
		map[string]float64{"c1": 0.0, "c2": 1.0},
		map[string]int{"c1": 3}, nil, nil); got != "semantic_non_reproduction" {
		t.Fatalf("有首编辑仍失败应语义未复现: %s", got)
	}
	if got := classifyGateFailure(false, "",
		map[string]struct{}{"c1": {}},
		map[string]float64{"c1": 0.0},
		map[string]int{}, nil, nil); got != "execution_convergence_failure" {
		t.Fatalf("无首编辑应收敛失败: %s", got)
	}
	if got := classifyGateFailure(false, "retention check", nil, map[string]float64{}, nil, nil, nil); got != "regression_or_retention_failure" {
		t.Fatalf("保留检查: %s", got)
	}
	if got := classifyGateFailure(false, "expected_tool_not_invoked_on_target_case", nil, map[string]float64{}, nil, nil, nil); got != "capability_activation_failure" {
		t.Fatalf("能力未调用: %s", got)
	}
	if got := classifyGateFailure(false, "", nil, map[string]float64{}, nil, nil, nil); got != "score_non_improvement" {
		t.Fatalf("默认: %s", got)
	}
}

func TestGloballyPromotable_非回归判定(t *testing.T) {
	dir := t.TempDir()
	selected := writeEvalRef(t, dir, "sel.yaml",
		"  - case_id: c1\n    score: 1.0\n    status: passed\n  - case_id: c2\n    score: 0.2\n    status: passed\n")
	prevBest := writeEvalRef(t, dir, "prev.yaml",
		"  - case_id: c1\n    score: 1.0\n    status: passed\n  - case_id: c2\n    score: 0.6\n    status: passed\n")
	score := 0.75
	ok, reason, err := globallyPromotable(selected, prevBest, &score)
	if err != nil {
		t.Fatalf("globallyPromotable: %v", err)
	}
	if ok || reason != "selected_score_regressed" {
		t.Fatalf("均分下降应拒绝: %v %s", ok, reason)
	}
	// 分数持平但受保护用例回归
	higher := writeEvalRef(t, dir, "higher.yaml",
		"  - case_id: c1\n    score: 0.6\n    status: passed\n  - case_id: c2\n    score: 1.0\n    status: passed\n")
	ok, reason, err = globallyPromotable(higher, prevBest, &score)
	if err != nil {
		t.Fatalf("globallyPromotable: %v", err)
	}
	if ok || reason != "protected_case_regressed" {
		t.Fatalf("受保护用例回归应拒绝: %v %s", ok, reason)
	}
	// 通过
	ok, reason, err = globallyPromotable(prevBest, prevBest, &score)
	if err != nil || !ok || reason != "selected_global_non_regression" {
		t.Fatalf("非回归应通过: %v %s %v", ok, reason, err)
	}
}

func TestRejectMixedOpaqueSnapshotSelection(t *testing.T) {
	gates := []map[string]any{{"composition_mode": "opaque_snapshot"}}
	selections := []map[string]any{{"retained": true}, {"retained": false}}
	if _, err := rejectMixedOpaqueSnapshotSelection(gates, selections); err == nil {
		t.Fatalf("数量不配对应报错")
	}
	// 非 opaque 不拒绝
	gates = []map[string]any{{"composition_mode": "solo"}, {}}
	selections = []map[string]any{{"retained": true}, {"retained": false}}
	mixed, err := rejectMixedOpaqueSnapshotSelection(gates, selections)
	if err != nil || mixed {
		t.Fatalf("非 opaque 不应拒绝: %v %v", mixed, err)
	}
	// opaque 混合 → 全部拒
	gates = []map[string]any{{"composition_mode": "opaque_snapshot"}, {}}
	selections = []map[string]any{{"retained": true}, {"retained": false}}
	mixed, err = rejectMixedOpaqueSnapshotSelection(gates, selections)
	if err != nil || !mixed {
		t.Fatalf("opaque 混合应拒绝: %v %v", mixed, err)
	}
	for _, selection := range selections {
		if selection["retained"] != false || selection["reason"] != "epoch_opaque_snapshot_partial_retention_unsupported" {
			t.Fatalf("全部改写: %v", selection)
		}
	}
}

func TestCheckpointRelativePath(t *testing.T) {
	if got := checkpointRelativePath("/abs/path"); got != "" {
		t.Fatalf("绝对路径拒绝: %q", got)
	}
	if got := checkpointRelativePath("a/../b"); got != "" {
		t.Fatalf(".. 拒绝: %q", got)
	}
	if got := checkpointRelativePath("skills/foo/skill.md"); got != "skills/foo/skill.md" {
		t.Fatalf("相对路径归一: %q", got)
	}
	if got := checkpointRelativePath("a\\b\\c.md"); got != "a/b/c.md" {
		t.Fatalf("反斜杠转正斜杠: %q", got)
	}
}

func TestCheckpointRoleDirName(t *testing.T) {
	name := checkpointRoleDirName("Coder Role")
	if !strings.HasPrefix(name, "coder_role_") || len(name) != 11+8 {
		t.Fatalf("角色目录名: %q", name)
	}
	if name != checkpointRoleDirName("Coder Role") {
		t.Fatalf("应稳定")
	}
	if got := checkpointRoleDirName("###"); !strings.HasPrefix(got, "role_") {
		t.Fatalf("全符号回退 role: %q", got)
	}
}

func TestCheckpointManifestPathMatches(t *testing.T) {
	if !checkpointManifestPathMatches("skills/a/skill.md", "skills/a/skill.md", true) {
		t.Fatalf("精确匹配")
	}
	if checkpointManifestPathMatches("skills/a/other.md", "skills/a/skill.md", true) {
		t.Fatalf("exact_only 下前缀不算")
	}
	if !checkpointManifestPathMatches("skills/a/skill.md", "skills/a", false) {
		t.Fatalf("非 exact 前缀匹配")
	}
	if checkpointManifestPathMatches("", "skills/a", false) {
		t.Fatalf("空路径不匹配")
	}
}

func TestToolAndSkillNamesMatch(t *testing.T) {
	if !toolNamesMatch("bash_tool", "BASH") {
		t.Fatalf("工具名规范化匹配")
	}
	if toolNamesMatch("", "bash") {
		t.Fatalf("空计划名不匹配")
	}
	if !skillNamesMatch("My-Skill", "my_skill") {
		t.Fatalf("技能名不去 _tool 后缀")
	}
}

func TestMissingCapabilityInvocations(t *testing.T) {
	capabilities := []map[string]any{
		{
			"action_group":     "tool",
			"operation":        "add",
			"runtime_name":     "bash",
			"target_case_ids":  []any{"c1", "c2"},
			"activation_phase": "diagnose",
		},
		{
			"action_group":     "skill",
			"operation":        "modify",
			"runtime_name":     "s1",
			"target_case_ids":  []any{"c1"},
			"activation_phase": "post_diagnosis",
		},
		{"action_group": "tool", "operation": "remove", "runtime_name": "gone"},
	}
	invoked := map[string]map[string]struct{}{
		"c1": {"bash": {}},
	}
	missing := missingCapabilityInvocations(capabilities, "tool", invoked,
		map[string]struct{}{}, toolNamesMatch, nil)
	if len(missing) != 1 || missing[0]["target_case_id"] != "c2" {
		t.Fatalf("c2 未调用应缺失: %v", missing)
	}
	// post_diagnosis 用 later_edit 集
	laterEdit := map[string]map[string]struct{}{"c1": {"s1": {}}}
	missing = missingCapabilityInvocations(capabilities, "skill", invoked,
		map[string]struct{}{}, skillNamesMatch, laterEdit)
	if len(missing) != 0 {
		t.Fatalf("later_edit 命中应不缺失: %v", missing)
	}
}

func TestMissingInvocationsWereLate(t *testing.T) {
	missing := []map[string]string{{"runtime_name": "bash", "target_case_id": "c1"}}
	if !missingInvocationsWereLate(missing, map[string]map[string]struct{}{
		"c1": {"BASH": {}}}, toolNamesMatch) {
		t.Fatalf("规范化命中应判晚")
	}
	if missingInvocationsWereLate(missing, map[string]map[string]struct{}{
		"c1": {"grep": {}}}, toolNamesMatch) {
		t.Fatalf("未命中不判晚")
	}
}

func TestFailedMachineEvidence(t *testing.T) {
	dir := t.TempDir()
	result := filepath.Join(dir, "r.json")
	writeTestYAML(t, result, `{"evaluation":{"metadata":{"artifact_runtime_evidence":{"observations":[
		{"status":"failed","type":"machine_evidence"},
		{"status":"passed","type":"machine_evidence"}]}}}}`)
	ref := filepath.Join(dir, "e.yaml")
	writeTestYAML(t, ref, "cases:\n  - case_id: c1\n    result_path: "+result+"\n")
	failures, err := failedMachineEvidence(ref)
	if err != nil {
		t.Fatalf("failedMachineEvidence: %v", err)
	}
	if len(failures) != 1 || failures[0] != "c1:machine_evidence" {
		t.Fatalf("失败证据: %v", failures)
	}
	ids := machineEvidenceCaseIDs(failures)
	if _, ok := ids["c1"]; !ok {
		t.Fatalf("用例集: %v", ids)
	}
}

func TestBuildReport_字段(t *testing.T) {
	state := map[string]any{
		"status":                         "running",
		"epoch_checkpoints":              []any{map[string]any{}, map[string]any{}},
		"candidate_gates":                []any{},
		"completed_batches":              map[string]any{"a": 1, "b": 2},
		"allowed_action_groups":          []any{"prompt"},
		"allowed_prompt_surfaces":        []any{"prompt_section"},
		"baseline_score":                 0.5,
		"best_score":                     0.9,
		"publication_status":             "published",
		"candidate_feedback_ledger_path": "",
	}
	dataset := makeTestDatasetArtifact(t, ".")
	report := buildReport(state, dataset)
	if report["status"] != "running" || report["iteration"] != 2 {
		t.Fatalf("基础字段: %v", report)
	}
	if report["completed_batch_count"] != 2 || report["candidate_count"] != 0 {
		t.Fatalf("计数: %v", report)
	}
	if report["publication_status"] != "published" {
		t.Fatalf("发布状态: %v", report)
	}
	if report["dataset_id"] != "ds1" {
		t.Fatalf("数据集 ID: %v", report["dataset_id"])
	}
}

func TestEnsureFinalPublication_无改进不发布(t *testing.T) {
	state := map[string]any{
		"candidate_gates":        []any{},
		"promotion_policy":       map[string]any{},
		"best_harness_refs_path": "/x",
	}
	if err := ensureFinalPublication(state, t.TempDir()); err != nil {
		t.Fatalf("ensureFinalPublication: %v", err)
	}
	if state["publication_status"] != "not_published_no_improvement" {
		t.Fatalf("无受门改进不发布: %v", state["publication_status"])
	}
}

func TestEnsureFinalPublication_发布链路(t *testing.T) {
	dir := t.TempDir()
	// 造最佳 harness 包
	pkgDir := filepath.Join(dir, "pkg", "coder")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTestYAML(t, filepath.Join(pkgDir, "runtime.yaml"), "max_iterations: 10\n")
	bestRefs := filepath.Join(dir, "best.yaml")
	writeTestYAML(t, bestRefs, "harness_refs:\n  coder: "+pkgDir+"\n")
	// 受门改进
	acceptedGate := map[string]any{"accepted": true, "status": "accepted"}
	state := map[string]any{
		"candidate_gates":          []any{acceptedGate},
		"promotion_policy":         map[string]any{},
		"best_harness_refs_path":   bestRefs,
		"best_score":               0.9,
		"source_harness_refs_path": bestRefs,
	}
	if err := ensureFinalPublication(state, dir); err != nil {
		t.Fatalf("ensureFinalPublication: %v", err)
	}
	if state["publication_status"] != "published" {
		t.Fatalf("应发布: %v", state["publication_status"])
	}
	publishedRef := state["published_harness_refs_path"].(string)
	if !fileExists(publishedRef) {
		t.Fatalf("发布引用应落盘: %s", publishedRef)
	}
	payload, err := readYAMLFile(publishedRef)
	if err != nil {
		t.Fatalf("回读: %v", err)
	}
	if payload["promotion_status"] != "published" {
		t.Fatalf("发布载荷状态: %v", payload["promotion_status"])
	}
	if roles, _ := payload["promoted_roles"].([]any); len(roles) != 1 || roles[0] != "coder" {
		t.Fatalf("promoted_roles: %v", payload["promoted_roles"])
	}
	// 已发布且指纹一致 → 幂等返回 published
	state2 := map[string]any{
		"candidate_gates":             []any{map[string]any{"accepted": true, "status": "accepted"}},
		"promotion_policy":            map[string]any{},
		"best_harness_refs_path":      bestRefs,
		"source_harness_refs_path":    bestRefs,
		"published_harness_refs_path": publishedRef,
	}
	if err := ensureFinalPublication(state2, dir); err != nil {
		t.Fatalf("幂等发布: %v", err)
	}
	if state2["publication_status"] != "published" {
		t.Fatalf("幂等应保持 published: %v", state2["publication_status"])
	}
}

func TestMaterializeCheckpointFilteredHarness_组合链路(t *testing.T) {
	dir := t.TempDir()
	// 基线 harness 包
	basePkg := filepath.Join(dir, "base", "coder")
	os.MkdirAll(basePkg, 0o755)
	os.MkdirAll(filepath.Join(basePkg, "skills"), 0o755)
	writeTestYAML(t, filepath.Join(basePkg, "runtime.yaml"), "max_iterations: 5\n")
	skillsYAML := filepath.Join(basePkg, "skills", "skills.yaml")
	writeTestYAML(t, skillsYAML, "skills: []\n")
	baseRefs := filepath.Join(dir, "base.yaml")
	writeTestYAML(t, baseRefs, "harness_refs:\n  coder: "+basePkg+"\n")
	// 候选包含新增技能
	candPkg := filepath.Join(dir, "cand", "coder")
	os.MkdirAll(filepath.Join(candPkg, "skills", "new_skill"), 0o755)
	writeTestYAML(t, filepath.Join(candPkg, "skills", "new_skill", "skill.md"), "# new\n")
	candSkills := filepath.Join(candPkg, "skills", "skills.yaml")
	writeTestYAML(t, candSkills, "skills:\n  - name: new_skill\n    file: skills/new_skill/skill.md\n")
	candRefs := filepath.Join(dir, "cand.yaml")
	writeTestYAML(t, candRefs, "harness_refs:\n  coder: "+candPkg+"\n")
	fullEval := writeEvalRef(t, dir, "full.yaml", "  - case_id: c1\n    score: 1.0\n")

	retainedGates := []map[string]any{{
		"candidate_harness_refs_path": candRefs,
		"capabilities": []any{map[string]any{
			"action_id":    "a1",
			"role":         "coder",
			"action_group": "skill",
			"operation":    "add",
			"target_path":  "skills/new_skill/skill.md",
			"runtime_name": "new_skill",
		}},
	}}
	selected, err := materializeCheckpointFilteredHarness(
		dir, 1, baseRefs, baseRefs, retainedGates, nil, fullEval)
	if err != nil {
		t.Fatalf("materializeCheckpointFilteredHarness: %v", err)
	}
	payload, err := readYAMLFile(selected)
	if err != nil {
		t.Fatalf("回读: %v", err)
	}
	if payload["promotion_status"] != "checkpoint_filtered" {
		t.Fatalf("状态: %v", payload["promotion_status"])
	}
	checkpointFilter := payload["checkpoint_filter"].(map[string]any)
	if checkpointFilter["post_checkpoint_replay_performed"] != false {
		t.Fatalf("replay 标志: %v", checkpointFilter)
	}
	retainedActions, _ := checkpointFilter["retained_actions"].([]any)
	if len(retainedActions) != 1 {
		t.Fatalf("保留动作: %v", checkpointFilter["retained_actions"])
	}
	// 组合后的包应含新技能文件与 manifest 条目
	filter := checkpointFilter
	_ = filter
	selectedPkg := payload["harness_refs"].(map[string]any)["coder"].(string)
	if !fileExists(filepath.Join(selectedPkg, "skills", "new_skill", "skill.md")) {
		t.Fatalf("新技能应复制进组合包")
	}
	mergedSkills, _ := readYAMLFile(filepath.Join(selectedPkg, "skills", "skills.yaml"))
	entries, _ := mergedSkills["skills"].([]any)
	if len(entries) != 1 {
		t.Fatalf("skills.yaml 应含 1 条: %v", mergedSkills)
	}
	// 非法操作拒绝
	badGates := []map[string]any{{
		"candidate_harness_refs_path": candRefs,
		"capabilities": []any{map[string]any{
			"role": "coder", "action_group": "skill", "operation": "replace",
			"target_path": "skills/new_skill/skill.md", "runtime_name": "new_skill"}},
	}}
	if _, err := materializeCheckpointFilteredHarness(
		dir, 2, baseRefs, baseRefs, badGates, nil, fullEval); err == nil ||
		!strings.Contains(err.Error(), "add/modify/remove") {
		t.Fatalf("replace 应拒绝: %v", err)
	}
	// 空 harness_refs 拒绝
	emptyRefs := filepath.Join(dir, "empty.yaml")
	writeTestYAML(t, emptyRefs, "harness_refs: {}\n")
	if _, err := materializeCheckpointFilteredHarness(
		dir, 3, emptyRefs, baseRefs, nil, nil, fullEval); err == nil ||
		!strings.Contains(err.Error(), "no harness_refs") {
		t.Fatalf("空引用应拒绝: %v", err)
	}
}

func TestCheckpointCopyManifestEntries_无匹配报错(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "dest", "skills.yaml")
	os.MkdirAll(filepath.Dir(destination), 0o755)
	writeTestYAML(t, destination, "skills: []\n")
	source := filepath.Join(dir, "src", "skills.yaml")
	os.MkdirAll(filepath.Dir(source), 0o755)
	writeTestYAML(t, source, "skills:\n  - name: other\n    file: skills/other/skill.md\n")
	err := checkpointCopyManifestEntries(destination, source, "skills", "skills/new/skill.md", "new")
	if err == nil || !strings.Contains(err.Error(), "no matching entry") {
		t.Fatalf("无匹配条目应报错: %v", err)
	}
}

func TestCheckpointRemoveManifestEntries_名字与路径(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "tools", "tools.yaml")
	os.MkdirAll(filepath.Dir(manifest), 0o755)
	writeTestYAML(t, manifest, "tools:\n  - name: mine\n    file: tools/mine/tool.py\n  - name: keep\n    file: tools/keep/tool.py\n")
	if err := checkpointRemoveManifestEntries(manifest, "tools", "tools/mine/tool.py", "mine", false); err != nil {
		t.Fatalf("checkpointRemoveManifestEntries: %v", err)
	}
	data, _ := readYAMLFile(manifest)
	entries, _ := data["tools"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["name"] != "keep" {
		t.Fatalf("mine 应被移除: %v", entries)
	}
	// 缺失文件吞错
	if err := checkpointRemoveManifestEntries(filepath.Join(dir, "missing.yaml"), "tools", "x", "", false); err != nil {
		t.Fatalf("缺失 manifest 应吞错: %v", err)
	}
}

func TestCheckpointRemovePromptEntry(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "prompt_sections", "sections.yaml")
	os.MkdirAll(filepath.Dir(manifest), 0o755)
	writeTestYAML(t, manifest, "sections:\n  - name: sec1\n    file: prompt_sections/sec1.md\n  - name: sec2\n    file: prompt_sections/sec2.md\n")
	if err := checkpointRemovePromptEntry(manifest, "prompt_sections/sec1.md", ""); err != nil {
		t.Fatalf("checkpointRemovePromptEntry: %v", err)
	}
	data, _ := readYAMLFile(manifest)
	entries, _ := data["sections"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["name"] != "sec2" {
		t.Fatalf("sec1 应被移除: %v", entries)
	}
}

func TestCheckpointRemoveAddedCapability_不支持组(t *testing.T) {
	if err := checkpointRemoveAddedCapability(t.TempDir(), "mystery", "x", ""); err == nil ||
		!strings.Contains(err.Error(), "does not support action_group") {
		t.Fatalf("不支持组应报错: %v", err)
	}
	// tool 组 target 即 manifest 拒绝
	if err := checkpointRemoveAddedCapability(t.TempDir(), "tool", "tools/tools.yaml", ""); err == nil ||
		!strings.Contains(err.Error(), "whose target is only") {
		t.Fatalf("整 manifest 移除应拒绝: %v", err)
	}
}

func TestSelectGateFromEpochCheckpoint_不确定与失败与保留(t *testing.T) {
	dir := t.TempDir()
	gate := map[string]any{
		"target_case_ids": []any{"c1"},
		"capabilities": []any{map[string]any{
			"action_group": "prompt", "runtime_name": "sec1",
			"target_case_ids": []any{"c1"}},
		},
	}
	// 不确定：错误用例
	got, err := selectGateFromEpochCheckpoint(gate, "", map[string]struct{}{"c1": {}}, nil)
	if err != nil {
		t.Fatalf("selectGateFromEpochCheckpoint: %v", err)
	}
	if got["retained"] != false || got["reason"] != "candidate_target_inconclusive_at_epoch_checkpoint" {
		t.Fatalf("错误用例应不确定: %v", got)
	}
	// 失败：目标未过
	failEval := writeEvalRef(t, dir, "fail.yaml", "  - case_id: c1\n    score: 0\n    status: failed\n")
	got, err = selectGateFromEpochCheckpoint(gate, failEval, nil, nil)
	if err != nil {
		t.Fatalf("selectGateFromEpochCheckpoint: %v", err)
	}
	if got["reason"] != "candidate_failed_target_replay_checkpoint" {
		t.Fatalf("目标未过应失败: %v", got)
	}
	// 无技能调用轨迹 → 首编辑步缺失 → execution_convergence_failure（Python 同）
	if got["failure_class"] != "execution_convergence_failure" {
		t.Fatalf("重放分类: %v", got["failure_class"])
	}
	// 保留：目标通过（prompt 能力不查运行时调用）
	passEval := writeEvalRef(t, dir, "pass.yaml", "  - case_id: c1\n    score: 1.0\n    status: passed\n")
	got, err = selectGateFromEpochCheckpoint(gate, passEval, nil, nil)
	if err != nil {
		t.Fatalf("selectGateFromEpochCheckpoint: %v", err)
	}
	if got["retained"] != true || got["reason"] != "candidate_effect_retained_after_epoch_checkpoint" {
		t.Fatalf("应保留: %v", got)
	}
	// 运行时能力未调用：tool 能力 + 无调用记录；
	// 用例结果需带完整 execution_evidence 才越过不确定检查（Python 同）
	toolResult := filepath.Join(dir, "tool_result.json")
	writeTestYAML(t, toolResult, `{"metadata":{"execution":{"execution_evidence":{
		"status":"complete","timing_complete":true}}}}`)
	toolEval := filepath.Join(dir, "tool_eval.yaml")
	writeTestYAML(t, toolEval, "cases:\n  - case_id: c1\n    score: 1.0\n    status: passed\n    result_path: "+toolResult+"\n")
	toolGate := map[string]any{
		"target_case_ids": []any{"c1"},
		"capabilities": []any{map[string]any{
			"action_group": "tool", "runtime_name": "bash",
			"target_case_ids": []any{"c1"}},
		},
	}
	got, err = selectGateFromEpochCheckpoint(toolGate, toolEval, nil, nil)
	if err != nil {
		t.Fatalf("selectGateFromEpochCheckpoint: %v", err)
	}
	if got["reason"] != "candidate_capability_not_used_at_epoch_checkpoint" {
		t.Fatalf("未调用应缺 invocation: %v", got)
	}
}

func TestClassifierHelpers_字符串集合差(t *testing.T) {
	got := stringSetKeyDiff(map[string]struct{}{"a": {}, "b": {}}, map[string]struct{}{"b": {}})
	if len(got) != 1 {
		t.Fatalf("差集: %v", got)
	}
}

func TestInconclusiveCapabilityCasesExported(t *testing.T) {
	dir := t.TempDir()
	// 无 result → 不确定
	ref := writeEvalRef(t, dir, "e.yaml", "  - case_id: c1\n    result_path: /nonexistent.json\n")
	got := InconclusiveCapabilityCases(ref, []map[string]string{{"target_case_id": "c1"}})
	if !got["c1"] {
		t.Fatalf("结果读取失败应不确定: %v", got)
	}
	// 空 required → 空
	if got := InconclusiveCapabilityCases(ref, nil); len(got) != 0 {
		t.Fatalf("空 required 应空: %v", got)
	}
}

func TestEvaluatorRequirementGroups_常量对齐(t *testing.T) {
	if evaluatorpkg.FailToPassGroup != "fail_to_pass" ||
		evaluatorpkg.PassToPassGroup != "pass_to_pass" ||
		evaluatorpkg.AtomicCheckGroup != "atomic_check" {
		t.Fatalf("组常量应对齐 Python")
	}
}

func TestEvalOfficialMetrics与ErrorCaseIDs(t *testing.T) {
	dir := t.TempDir()
	ref := writeEvalRef(t, dir, "e.yaml",
		"  - case_id: c1\n    status: error\n"+
			"  - case_id: c2\n    status: passed\n"+
			"official_metrics:\n  accuracy: 0.5\n")
	metrics, err := evalOfficialMetrics(ref)
	if err != nil || metrics["accuracy"] != 0.5 {
		t.Fatalf("official_metrics: %v %v", metrics, err)
	}
	errorSet, err := errorCaseIDs(ref)
	if err != nil || len(errorSet) != 1 {
		t.Fatalf("errorCaseIDs: %v %v", errorSet, err)
	}
	if !evalHasErrors(ref) {
		t.Fatalf("evalHasErrors 应真")
	}
	clean := writeEvalRef(t, dir, "clean.yaml", "  - case_id: c1\n    status: passed\n")
	if evalHasErrors(clean) {
		t.Fatalf("无错误应假")
	}
}

func TestInvokedNames聚合(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace.json")
	writeTestYAML(t, trace, `{"steps":[{"kind":"tool","detail":{"tool_name":"bash","call_result":"ok"}}]}`)
	ref := filepath.Join(dir, "e.yaml")
	writeTestYAML(t, ref, "cases:\n  - case_id: c1\n    trace_path: "+trace+"\n")
	tools, err := invokedToolNames(ref)
	if err != nil || len(tools) != 1 {
		t.Fatalf("invokedToolNames: %v %v", tools, err)
	}
	skills, err := invokedSkillNames(ref)
	if err != nil || len(skills) != 0 {
		t.Fatalf("invokedSkillNames 应空: %v %v", skills, err)
	}
}

func TestNativeCaseScores(t *testing.T) {
	scores := nativeCaseScores(map[string]map[string]any{
		"c1": {"score": 0.7}, "c2": {"score": nil}, "c3": {},
	})
	if len(scores) != 1 || scores["c1"] != 0.7 {
		t.Fatalf("只收数值: %v", scores)
	}
}

func TestCandidatePatchExcerptsByCase(t *testing.T) {
	dir := t.TempDir()
	patch := filepath.Join(dir, "patch.diff")
	writeTestYAML(t, patch, "+added line\n")
	result := filepath.Join(dir, "r.json")
	writeTestYAML(t, result, `{"evaluation":{"metadata":{"model_patch_path":"`+patch+`"}}}`)
	missing := filepath.Join(dir, "missing.json")
	writeTestYAML(t, missing, `{"evaluation":{"metadata":{"model_patch_path":"/nonexistent.diff"}}}`)
	ref := filepath.Join(dir, "e.yaml")
	writeTestYAML(t, ref, "cases:\n"+
		"  - case_id: c1\n    result_path: "+result+"\n"+
		"  - case_id: c2\n    result_path: "+missing+"\n")
	excerpts, err := candidatePatchExcerptsByCase(ref, map[string]struct{}{"c1": {}, "c2": {}})
	if err != nil {
		t.Fatalf("candidatePatchExcerptsByCase: %v", err)
	}
	if excerpts["c1"] != "+added line\n" {
		t.Fatalf("补丁摘要: %q", excerpts["c1"])
	}
	if excerpts["c2"] != "" {
		t.Fatalf("缺失补丁应空串: %q", excerpts["c2"])
	}
}

func TestCopyFilePreserve(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	writeTestYAML(t, src, "data")
	dst := filepath.Join(dir, "b.txt")
	if err := copyFilePreserve(src, dst); err != nil {
		t.Fatalf("copyFilePreserve: %v", err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "data" {
		t.Fatalf("内容: %q", data)
	}
	srcInfo, _ := os.Stat(src)
	dstInfo, _ := os.Stat(dst)
	if !srcInfo.ModTime().Equal(dstInfo.ModTime()) {
		t.Fatalf("应保留 mtime（copy2 语义）")
	}
}

func TestCheckpointCopyManifestEntries_替换同名条目(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "dest", "skills.yaml")
	os.MkdirAll(filepath.Dir(destination), 0o755)
	writeTestYAML(t, destination, "skills:\n  - name: new_skill\n    file: skills/new_skill/old.md\n")
	source := filepath.Join(dir, "src", "skills.yaml")
	os.MkdirAll(filepath.Dir(source), 0o755)
	writeTestYAML(t, source, "skills:\n  - name: new_skill\n    file: skills/new_skill/skill.md\n")
	if err := checkpointCopyManifestEntries(destination, source, "skills", "skills/new_skill/skill.md", "new_skill"); err != nil {
		t.Fatalf("checkpointCopyManifestEntries: %v", err)
	}
	data, _ := readYAMLFile(destination)
	entries, _ := data["skills"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["file"] != "skills/new_skill/skill.md" {
		t.Fatalf("同名条目应替换: %v", entries)
	}
}

func TestCheckpointCopyManifestEntries_原生manifest(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "dest", "prompt_sections", "sections.yaml")
	os.MkdirAll(filepath.Dir(destination), 0o755)
	// 原生 manifest.json 位于 source 的上上级
	source := filepath.Join(dir, "src", "prompt_sections", "sections.yaml")
	os.MkdirAll(filepath.Dir(source), 0o755)
	nativeManifest := filepath.Join(dir, "src", "manifest.json")
	writeTestYAML(t, nativeManifest, `{"prompt_sections":[{"name":"sec1","file":"prompt_sections/sec1.md"}]}`)
	if err := checkpointCopyManifestEntries(destination, source, "sections", "prompt_sections/sec1.md", ""); err != nil {
		t.Fatalf("checkpointCopyManifestEntries: %v", err)
	}
	data, _ := readYAMLFile(destination)
	entries, _ := data["sections"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["name"] != "sec1" {
		t.Fatalf("原生 manifest 条目应复制: %v", entries)
	}
}

func TestCheckpointRemoveAddedCapability_prompt与skill与config(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "harness")
	os.MkdirAll(filepath.Join(root, "prompt_sections"), 0o755)
	os.MkdirAll(filepath.Join(root, "skills", "new_skill"), 0o755)
	writeTestYAML(t, filepath.Join(root, "prompt_sections", "sections.yaml"),
		"sections:\n  - name: sec1\n    file: prompt_sections/sec1.md\n")
	writeTestYAML(t, filepath.Join(root, "skills", "skills.yaml"), "skills: []\n")
	// prompt 移除：路径 + manifest 条目
	if err := checkpointRemoveAddedCapability(root, "prompt", "prompt_sections/sec1.md", "sec1"); err != nil {
		t.Fatalf("prompt 移除: %v", err)
	}
	if fileExists(filepath.Join(root, "prompt_sections", "sec1.md")) {
		t.Fatalf("提示词文件应删除")
	}
	// skill 移除：技能根目录 + manifest
	os.MkdirAll(filepath.Join(root, "skills", "new_skill"), 0o755)
	if err := checkpointRemoveAddedCapability(root, "skill", "skills/new_skill/skill.md", "new_skill"); err != nil {
		t.Fatalf("skill 移除: %v", err)
	}
	// config 移除：runtime.yaml
	writeTestYAML(t, filepath.Join(root, "runtime.yaml"), "max_iterations: 5\n")
	if err := checkpointRemoveAddedCapability(root, "config", "runtime.yaml", ""); err != nil {
		t.Fatalf("config 移除: %v", err)
	}
	if fileExists(filepath.Join(root, "runtime.yaml")) {
		t.Fatalf("runtime.yaml 应删除")
	}
}

func TestDatasetArtifact单目录(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ds.json")
	writeTestYAML(t, file, `[{"case_id":"c1"}]`)
	request, err := NewIterativeSingleHarnessRequest(
		[]string{file}, "", "", "ds1", false, "", false, "", nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	artifact, err := datasetArtifact(request)
	if err != nil {
		t.Fatalf("datasetArtifact: %v", err)
	}
	if artifact.DatasetID != "ds1" || artifact.DatasetDir != dir || artifact.Cases == nil || *artifact.Cases != 1 {
		t.Fatalf("工件: %+v", artifact)
	}
}

func TestRequestFingerprint带基线(t *testing.T) {
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.yaml")
	writeTestYAML(t, baseline, "cases: []\n")
	ds := filepath.Join(dir, "ds.json")
	writeTestYAML(t, ds, `[{"case_id":"c1"}]`)
	request, err := NewIterativeSingleHarnessRequest(
		[]string{ds}, "", "", "", false, baseline, false, "", nil)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	fp, err := requestFingerprint(request, "/src", 1, "d")
	if err != nil {
		t.Fatalf("requestFingerprint: %v", err)
	}
	if fp["baseline_eval_ref_path"] == "" || fp["baseline_eval_ref_sha256"] == "" {
		t.Fatalf("基线字段应填充: %v", fp)
	}
}

func TestEvalCaseExplicitPassed(t *testing.T) {
	if got := evalCaseExplicitPassed(map[string]any{
		"metadata": map[string]any{"evaluation_passed": true}}); got == nil || !*got {
		t.Fatalf("true 提取: %v", got)
	}
	if got := evalCaseExplicitPassed(map[string]any{
		"metadata": map[string]any{"evaluation_passed": "yes"}}); got != nil {
		t.Fatalf("非 bool 应 nil: %v", got)
	}
	if got := evalCaseExplicitPassed(map[string]any{}); got != nil {
		t.Fatalf("缺 metadata 应 nil: %v", got)
	}
}

func TestTruthy与ToInt与SetDefault矩阵(t *testing.T) {
	if !truthy(true) || truthy(false) || !truthy(1) || truthy(0) || truthy(nil) || truthy("x") {
		t.Fatalf("truthy 矩阵")
	}
	if toInt(2) != 2 || toInt(int64(3)) != 3 || toInt(4.9) != 4 || toInt("x") != 0 {
		t.Fatalf("toInt 矩阵")
	}
	state := map[string]any{"k1": "v"}
	setDefault(state, "k1", "other")
	setDefault(state, "k2", "new")
	if state["k1"] != "v" || state["k2"] != "new" {
		t.Fatalf("setDefault: %v", state)
	}
}

func TestBuildReport完整分支(t *testing.T) {
	state := map[string]any{
		"status":            "done",
		"epoch_checkpoints": []any{},
		"candidate_gates": []any{
			map[string]any{"accepted": true, "status": "accepted"},
			map[string]any{
				"accepted": false, "status": "rejected",
				"candidate_failure_analysis_status": "unavailable",
				"candidate_failure_analysis_error":  map[string]any{"error_type": "E", "message": "m"},
				"epoch":                             1, "batch_index": 0, "batch_attempt_index": 0,
				"candidate_eval_ref_path": "/e",
				"target_case_ids":         []any{"c1"},
			},
		},
		"completed_batches":       map[string]any{},
		"allowed_action_groups":   []any{},
		"allowed_prompt_surfaces": []any{},
		"promotion_policy":        map[string]any{"policy_version": "v1"},
	}
	dataset := makeTestDatasetArtifact(t, ".")
	report := buildReport(state, dataset)
	if report["accepted_candidate_count"] != 1 || report["analysis_unavailable_count"] != 1 {
		t.Fatalf("计数: accepted=%v unavailable=%v",
			report["accepted_candidate_count"], report["analysis_unavailable_count"])
	}
	failures, _ := report["analysis_failures"].([]any)
	failure := failures[0].(map[string]any)
	if failure["error_type"] != "E" || failure["epoch"] != 1 {
		t.Fatalf("分析失败聚合: %v", failure)
	}
}
