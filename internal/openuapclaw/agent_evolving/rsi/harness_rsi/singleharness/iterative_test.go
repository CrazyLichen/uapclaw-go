package singleharness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/improver_evolution"
)

// harnessDatasetArtifact 数据集工件别名（缩短测试签名）。
type harnessDatasetArtifact = harnessrsi.DatasetArtifact

// 写临时 YAML 文件。
func writeTestYAML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
}

func intPtr(v int) *int { return &v }

func TestNewIterativeSingleHarnessRequest_maxIteration校验(t *testing.T) {
	if _, err := NewIterativeSingleHarnessRequest(nil, "", "", "", false, "", false, "", nil); err != nil {
		t.Fatalf("nil maxIteration 应放行: %v", err)
	}
	if _, err := NewIterativeSingleHarnessRequest(nil, "", "", "", false, "", false, "", intPtr(1)); err != nil {
		t.Fatalf("1 应放行: %v", err)
	}
	_, err := NewIterativeSingleHarnessRequest(nil, "", "", "", false, "", false, "", intPtr(0))
	if err == nil || !strings.Contains(err.Error(), "max_iteration must be an integer") {
		t.Fatalf("0 应拒绝且文案一致, got: %v", err)
	}
}

func TestLoadCases_空ID与重复ID拒绝(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.json")
	writeTestYAML(t, fileA, `[{"case_id":"c1"},{"case_id":""}]`)
	if _, err := LoadCases([]string{fileA}); err == nil ||
		!strings.Contains(err.Error(), "non-empty case_id") {
		t.Fatalf("空 case_id 应拒绝: %v", err)
	}
	fileB := filepath.Join(dir, "b.json")
	writeTestYAML(t, fileB, `[{"case_id":"c1"},{"case_id":"c1"}]`)
	_, err := LoadCases([]string{fileB})
	if err == nil || !strings.Contains(err.Error(), "duplicate case ids") {
		t.Fatalf("重复 case_id 应拒绝: %v", err)
	}
	cases, err := LoadCases([]string{fileA[:len(fileA)-len("a.json")] + "b.json"})
	_ = cases
	_ = err
}

func TestLoadCases_注入casePath与index(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ds.json")
	writeTestYAML(t, file, `[{"case_id":"c1"},{"case_id":"c2"}]`)
	cases, err := LoadCases([]string{file})
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("应载入 2 个用例, got %d", len(cases))
	}
	if cases[0]["case_index"] != 1 || cases[1]["case_index"] != 2 {
		t.Fatalf("case_index 应从 1 递增: %v %v", cases[0]["case_index"], cases[1]["case_index"])
	}
	if cases[0]["case_path"] != file {
		t.Fatalf("case_path 应为绝对路径: %v", cases[0]["case_path"])
	}
}

func TestLoadCasesPrivate_别名等价(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ds.json")
	writeTestYAML(t, file, `[{"case_id":"c1"}]`)
	a, err := LoadCases([]string{file})
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	b, err := loadCasesPrivate([]string{file})
	if err != nil {
		t.Fatalf("loadCasesPrivate: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("别名行为应一致")
	}
}

func TestDatasetArtifact_跨目录拒绝(t *testing.T) {
	dir := t.TempDir()
	request, err := NewIterativeSingleHarnessRequest(
		[]string{filepath.Join(dir, "a.json"), "/tmp/other/b.json"},
		"", "", "ds1", false, "", false, "", nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	if _, err := datasetArtifact(request); err == nil ||
		!strings.Contains(err.Error(), "must share one directory") {
		t.Fatalf("跨目录应拒绝: %v", err)
	}
}

func TestValidateAndFilterPlannedBatches_缺失与重复(t *testing.T) {
	expected := map[string]struct{}{"c1": {}, "c2": {}}
	// 邻近数据集被忽略 + 缺失拒绝
	got, err := validateAndFilterPlannedBatches(
		[][]map[string]any{{{"case_id": "c1"}, {"case_id": "neighbor"}}}, expected)
	if err == nil || !strings.Contains(err.Error(), "omitted requested case ids") {
		t.Fatalf("缺失应拒绝: %v", err)
	}
	_ = got
	// 重复拒绝
	_, err = validateAndFilterPlannedBatches(
		[][]map[string]any{{{"case_id": "c1"}, {"case_id": "c1"}, {"case_id": "c2"}}}, expected)
	if err == nil || !strings.Contains(err.Error(), "duplicate requested case id") {
		t.Fatalf("重复应拒绝: %v", err)
	}
	// 正常路径保留批次结构
	got, err = validateAndFilterPlannedBatches(
		[][]map[string]any{{{"case_id": "c1"}}, {{"case_id": "neighbor"}}, {{"case_id": "c2"}}}, expected)
	if err != nil {
		t.Fatalf("正常路径不应报错: %v", err)
	}
	if len(got) != 2 || len(got[0]) != 1 {
		t.Fatalf("应保留 2 个非空批次, got %d", len(got))
	}
}

func TestValidateSingleHarnessRefs_恰好一个ref(t *testing.T) {
	dir := t.TempDir()
	mapping := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, mapping, "harness_refs:\n  coder: /path/harness\n")
	if err := validateSingleHarnessRefs(mapping); err != nil {
		t.Fatalf("单 ref 映射应通过: %v", err)
	}
	two := filepath.Join(dir, "two.yaml")
	writeTestYAML(t, two, "harness_refs:\n  coder: /a\n  reviewer: /b\n")
	if err := validateSingleHarnessRefs(two); err == nil ||
		!strings.Contains(err.Error(), "exactly one harness ref") {
		t.Fatalf("双 ref 应拒绝: %v", err)
	}
	flat := filepath.Join(dir, "flat.yaml")
	writeTestYAML(t, flat, "version: 1\nsource_harness_refs_path: /x\ncoder: /path/harness\n")
	if err := validateSingleHarnessRefs(flat); err != nil {
		t.Fatalf("扁平形态（排除 version/source 键）应通过: %v", err)
	}
	notMapping := filepath.Join(dir, "list.yaml")
	writeTestYAML(t, notMapping, "- a\n- b\n")
	if err := validateSingleHarnessRefs(notMapping); err == nil ||
		!strings.Contains(err.Error(), "must be a mapping") {
		t.Fatalf("非映射应拒绝: %v", err)
	}
}

func TestRequestFingerprint_字段齐全且稳定(t *testing.T) {
	dir := t.TempDir()
	ds := filepath.Join(dir, "ds.json")
	writeTestYAML(t, ds, `[{"case_id":"c1"}]`)
	request, err := NewIterativeSingleHarnessRequest(
		[]string{ds}, "", "", "ds1", false, "", true, "t1", nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	fp, err := requestFingerprint(request, "/src/refs", 2, "digest-1")
	if err != nil {
		t.Fatalf("requestFingerprint: %v", err)
	}
	if fp["optimization_chain_version"] != 20 {
		t.Fatalf("chain version 应为 20: %v", fp["optimization_chain_version"])
	}
	if fp["sibling_candidate_count"] != 2 || fp["improver_policy_digest"] != "digest-1" ||
		fp["auto_full_baseline"] != true {
		t.Fatalf("指纹字段不齐: %v", fp)
	}
	if fp["baseline_eval_ref_path"] != "" || fp["baseline_eval_ref_sha256"] != "" {
		t.Fatalf("无基线时基线字段应为空串: %v", fp)
	}
}

func TestFileSHA256_稳定性(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	writeTestYAML(t, file, "hello")
	sha1, err := fileSHA256(file)
	if err != nil {
		t.Fatalf("fileSHA256: %v", err)
	}
	sha2, _ := fileSHA256(file)
	if sha1 != sha2 || len(sha1) != 64 {
		t.Fatalf("sha256 应稳定且 64 位: %s vs %s", sha1, sha2)
	}
	if _, err := fileSHA256(filepath.Join(dir, "missing")); err == nil {
		t.Fatalf("缺失文件应报错")
	}
}

func TestSafeCandidateError_脱敏与截断(t *testing.T) {
	err := errors.New("request failed with key sk-abcdef1234567890 and api_key: secret123")
	safe := safeCandidateError(err)
	if strings.Contains(safe["message"], "sk-abcdef") || strings.Contains(safe["message"], "secret123") {
		t.Fatalf("密钥应脱敏: %s", safe["message"])
	}
	if !strings.Contains(safe["message"], "[redacted]") {
		t.Fatalf("应含 [redacted] 占位: %s", safe["message"])
	}
	if safe["error_type"] == "" {
		t.Fatalf("error_type 不应为空: %v", safe)
	}
	long := errors.New(strings.Repeat("x", 3000))
	safe = safeCandidateError(long)
	if len(safe["message"]) != 2000 {
		t.Fatalf("应截断至 2000: %d", len(safe["message"]))
	}
	authErr := errors.New("Authorization: Bearer abcdef123456")
	safe = safeCandidateError(authErr)
	if strings.Contains(safe["message"], "abcdef123456") {
		t.Fatalf("bearer token 应脱敏: %s", safe["message"])
	}
}

func TestResultFromState_字段恢复(t *testing.T) {
	score := 0.85
	state := map[string]any{
		"current_harness_refs_path":   "/cur",
		"best_harness_refs_path":      "/best",
		"published_harness_refs_path": "/pub",
		"best_score":                  score,
	}
	result := resultFromState(state, "/state.yaml", "/report.yaml")
	if result.StatePath != "/state.yaml" || result.CurrentHarnessRefsPath != "/cur" ||
		result.BestHarnessRefsPath != "/best" || result.PublishedHarnessRefsPath != "/pub" {
		t.Fatalf("字段恢复不齐: %+v", result)
	}
	if result.BestScore == nil || *result.BestScore != score {
		t.Fatalf("best_score 应恢复: %v", result.BestScore)
	}
	missing := resultFromState(map[string]any{}, "/s", "/r")
	if missing.BestScore != nil {
		t.Fatalf("缺失 best_score 应为 nil: %v", missing.BestScore)
	}
}

func TestPriorEvalRefsFromState_过滤空引用(t *testing.T) {
	state := map[string]any{
		"baseline_eval_ref_path": "/base.yaml",
		"epoch_checkpoints": []any{
			map[string]any{"eval_ref_path": "/e1.yaml"},
			map[string]any{"eval_ref_path": ""},
			"not-a-dict",
		},
	}
	refs := priorEvalRefsFromState(state)
	if len(refs) != 2 || refs[0] != "/base.yaml" || refs[1] != "/e1.yaml" {
		t.Fatalf("应过滤空引用并跳过非映射: %v", refs)
	}
}

func TestLoadOrCreateState_新建状态字段齐全(t *testing.T) {
	dir := t.TempDir()
	dataset := map[string]any{"dataset_id": "ds1"}
	_ = dataset
	fp := map[string]any{"optimization_chain_version": 20}
	state, err := loadOrCreateState(
		filepath.Join(dir, "state.yaml"), false, fp, "/src/refs",
		makeTestDatasetArtifact(t, dir))
	if err != nil {
		t.Fatalf("loadOrCreateState: %v", err)
	}
	for _, key := range []string{
		"version", "status", "mode", "fingerprint", "dataset",
		"allowed_action_groups", "allowed_prompt_surfaces",
		"source_harness_refs_path", "current_harness_refs_path",
		"working_harness_refs_path", "best_harness_refs_path",
		"published_harness_refs_path", "publication_status",
		"best_score", "best_eval_ref_path", "baseline_eval_ref_path",
		"baseline_score", "retained_case_ids", "batch_plan_paths",
		"completed_batches", "candidate_gates", "improvement_instances",
		"epoch_checkpoints", "optimization_journal", "lever_scoreboard",
		"candidate_feedback_ledger_path",
	} {
		if _, ok := state[key]; !ok {
			t.Fatalf("新建状态缺键: %s", key)
		}
	}
	if state["version"] != 7 || state["publication_status"] != "not_published" {
		t.Fatalf("初始值不符: version=%v status=%v", state["version"], state["publication_status"])
	}
}

func TestLoadOrCreateState_续跑与指纹不匹配拒绝(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.yaml")
	fp := map[string]any{"optimization_chain_version": 20, "k": "v"}
	state, err := loadOrCreateState(statePath, false, fp, "/src/refs",
		makeTestDatasetArtifact(t, dir))
	if err != nil {
		t.Fatalf("新建状态: %v", err)
	}
	if err := writeYAMLAtomic(statePath, state); err != nil {
		t.Fatalf("落盘: %v", err)
	}
	// 指纹一致可续跑
	resumed, err := loadOrCreateState(statePath, true, fp, "/src/refs",
		makeTestDatasetArtifact(t, dir))
	if err != nil {
		t.Fatalf("同指纹续跑应成功: %v", err)
	}
	if _, ok := resumed["working_harness_refs_path"]; !ok {
		t.Fatalf("续跑应补 setdefault 键")
	}
	// 指纹不一致拒绝
	other := map[string]any{"optimization_chain_version": 20, "k": "other"}
	if _, err := loadOrCreateState(statePath, true, other, "/src/refs",
		makeTestDatasetArtifact(t, dir)); err == nil ||
		!strings.Contains(err.Error(), "resume inputs do not match") {
		t.Fatalf("指纹不匹配应拒绝: %v", err)
	}
}

func TestResumeFingerprintMatches_版本迁移分支(t *testing.T) {
	// 完全相等直通
	fp := map[string]any{"optimization_chain_version": 20}
	if !resumeFingerprintMatches(fp, fp) {
		t.Fatalf("相等应直通")
	}
	// 非 dict 拒绝
	if resumeFingerprintMatches(nil, fp) {
		t.Fatalf("nil stored 应拒绝")
	}
	// 12→13 迁移：digest 为当前默认策略时匹配
	currentDigest := improver_evolution.DefaultImproverPolicy().CanonicalDigest()
	stored := map[string]any{"optimization_chain_version": 12}
	requested := map[string]any{
		"optimization_chain_version": 13,
		"improver_policy_digest":     currentDigest,
	}
	if !resumeFingerprintMatches(stored, requested) {
		t.Fatalf("12→13 迁移应匹配")
	}
	// digest 不符拒绝
	badRequested := map[string]any{
		"optimization_chain_version": 13,
		"improver_policy_digest":     "other",
	}
	if resumeFingerprintMatches(stored, badRequested) {
		t.Fatalf("digest 不符应拒绝")
	}
	// 非 12/13 版本拒绝
	if resumeFingerprintMatches(
		map[string]any{"optimization_chain_version": 11}, requested) {
		t.Fatalf("stored 非 12 应拒绝")
	}
}

func TestInitializeFrozenBaseline_基线播种与校验(t *testing.T) {
	dir := t.TempDir()
	// 造 source harness 引用
	sourceRefs := filepath.Join(dir, "refs.yaml")
	writeTestYAML(t, sourceRefs, "harness_refs:\n  coder: /h\n")
	// 造基线 eval ref：cases + result_path 文件
	resultFile := filepath.Join(dir, "case1_result.json")
	writeTestYAML(t, resultFile, "{}")
	baseline := filepath.Join(dir, "baseline.yaml")
	writeTestYAML(t, baseline, "harness_refs_path: "+sourceRefs+"\ncases:\n  - case_id: c1\n    score: 1.0\n    status: passed\n    result_path: "+resultFile+"\n")
	state := map[string]any{}
	expected := map[string]struct{}{"c1": {}}
	if err := initializeFrozenBaseline(state, baseline, sourceRefs, expected); err != nil {
		t.Fatalf("initializeFrozenBaseline: %v", err)
	}
	if state["baseline_eval_ref_path"] != baseline {
		t.Fatalf("基线路径应播种: %v", state["baseline_eval_ref_path"])
	}
	if score, ok := state["baseline_score"].(float64); !ok || score != 1.0 {
		t.Fatalf("基线分应 1.0: %v", state["baseline_score"])
	}
	retained, _ := state["retained_case_ids"].([]any)
	if len(retained) != 1 || retained[0] != "c1" {
		t.Fatalf("通过用例应进保留集: %v", retained)
	}
	// 用例集不匹配拒绝
	badExpected := map[string]struct{}{"c2": {}}
	err := initializeFrozenBaseline(map[string]any{}, baseline, sourceRefs, badExpected)
	if err == nil || !strings.Contains(err.Error(), "does not match the frozen dataset") {
		t.Fatalf("用例集不匹配应拒绝: %v", err)
	}
	// harness_refs_path 不符拒绝
	otherRefs := filepath.Join(dir, "other.yaml")
	writeTestYAML(t, otherRefs, "harness_refs:\n  coder: /h\n")
	err = initializeFrozenBaseline(map[string]any{}, baseline, otherRefs, expected)
	if err == nil || !strings.Contains(err.Error(), "not produced by the source Harness") {
		t.Fatalf("来源不符应拒绝: %v", err)
	}
	// 空基线路径直接返回
	if err := initializeFrozenBaseline(map[string]any{}, "", sourceRefs, expected); err != nil {
		t.Fatalf("空基线应直接返回: %v", err)
	}
}

func TestExperimentOutcome_四分支(t *testing.T) {
	if got := experimentOutcome(map[string]any{
		"regressed_target_case_ids": []any{"c1"},
	}); got != "regressed" {
		t.Fatalf("回归应 regressed: %s", got)
	}
	if got := experimentOutcome(map[string]any{
		"target_score_delta": 0.5, "status": "accepted",
	}); got != "flipped" {
		t.Fatalf("接受且提升应 flipped: %s", got)
	}
	if got := experimentOutcome(map[string]any{
		"target_score_delta": 0.5, "status": "rejected",
	}); got != "improved_but_rejected" {
		t.Fatalf("拒绝但提升应 improved_but_rejected: %s", got)
	}
	if got := experimentOutcome(map[string]any{
		"verifier_deltas_by_case": map[string]any{
			"c1": map[string]any{"newly_passed_fail_to_pass": true},
		},
	}); got != "partial_contract_progress" {
		t.Fatalf("部分契约进展应 partial_contract_progress: %s", got)
	}
	if got := experimentOutcome(map[string]any{}); got != "still_failed" {
		t.Fatalf("默认应 still_failed: %s", got)
	}
}

func TestCapabilityRuntimeName(t *testing.T) {
	if got := capabilityRuntimeName("skill", "/a/b/my_skill/skill.md"); got != "my_skill" {
		t.Fatalf("skill 面应取父目录名: %s", got)
	}
	if got := capabilityRuntimeName("prompt", "/a/b/section.yaml"); got != "section" {
		t.Fatalf("非 skill 面应取文件 stem: %s", got)
	}
	if got := capabilityRuntimeName("prompt", ""); got != "" {
		t.Fatalf("空路径应空: %s", got)
	}
}

func TestMustPreserveBudgetForSiblings(t *testing.T) {
	if !mustPreserveBudgetForSiblings(intPtr(2), 3) {
		t.Fatalf("预算 ≤ 兄弟数应保留")
	}
	if mustPreserveBudgetForSiblings(nil, 3) {
		t.Fatalf("无预算限制不保留")
	}
	if mustPreserveBudgetForSiblings(intPtr(2), 0) {
		t.Fatalf("无兄弟不保留")
	}
	if mustPreserveBudgetForSiblings(intPtr(5), 3) {
		t.Fatalf("预算 > 兄弟数不保留")
	}
}

func TestSyncRetainedCaseIDs_替换语义(t *testing.T) {
	retained := map[string]struct{}{"c0": {}, "c2": {}}
	syncRetainedCaseIDs(retained, map[string]float64{"c1": 1.0, "c2": 0.5}, nil)
	if _, ok := retained["c0"]; !ok {
		t.Fatalf("未评估用例应保留（difference_update 只清已评估键）")
	}
	if _, ok := retained["c1"]; !ok {
		t.Fatalf("满分用例应保留")
	}
	if _, ok := retained["c2"]; ok {
		t.Fatalf("低分用例不应保留")
	}
	// 显式 passing 集
	retained2 := map[string]struct{}{}
	syncRetainedCaseIDs(retained2, map[string]float64{"c1": 0.5},
		map[string]struct{}{"c1": {}})
	if _, ok := retained2["c1"]; !ok {
		t.Fatalf("显式 passing 应保留")
	}
}

func TestCausalCandidateFailureClassification(t *testing.T) {
	if got := causalCandidateFailureClassification(map[string]map[string]any{
		"c1": {"state": "not_triggered"},
	}); got != "intervention_not_activated" {
		t.Fatalf("未触发应 intervention_not_activated: %s", got)
	}
	if got := causalCandidateFailureClassification(map[string]map[string]any{
		"c1": {"state": "triggered", "behavior_activation": map[string]any{
			"predicted_behavior_occurred": "yes", "predicted_outcome_occurred": "no"}},
	}); got != "action_occurred_but_hypothesis_refuted" {
		t.Fatalf("行为发生但假设被驳应 refuted: %s", got)
	}
	if got := causalCandidateFailureClassification(map[string]map[string]any{
		"c1": {"state": "triggered", "behavior_activation": map[string]any{
			"predicted_behavior_occurred": "yes", "predicted_outcome_occurred": "yes"}},
	}); got != "" {
		t.Fatalf("全发生应空串: %s", got)
	}
	if got := causalCandidateFailureClassification(map[string]map[string]any{
		"c1": {"state": "triggered", "behavior_activation": map[string]any{
			"predicted_behavior_occurred": "no"}},
	}); got != "intervention_not_activated" {
		t.Fatalf("行为未发生应 intervention_not_activated: %s", got)
	}
}

func TestUniformAssessmentValue(t *testing.T) {
	if got := uniformAssessmentValue(
		[]map[string]any{{"k": "Yes"}, {"k": "yes"}}, "k"); got != "yes" {
		t.Fatalf("大小写归一后唯一值: %s", got)
	}
	if got := uniformAssessmentValue(
		[]map[string]any{{"k": "a"}, {"k": "b"}}, "k"); got != "mixed" {
		t.Fatalf("多值应 mixed: %s", got)
	}
	if got := uniformAssessmentValue(nil, "k"); got != "unknown" {
		t.Fatalf("无值应 unknown: %s", got)
	}
}

func TestAnalysisIssueSignatures_语义键稳定(t *testing.T) {
	dir := t.TempDir()
	analysis := filepath.Join(dir, "analysis.yaml")
	writeTestYAML(t, analysis, `issues:
  - issue_id: i1
    category: Prompt
    summary: Wrong thing
    recommendation: Fix it
    failure_mode: logic
    affected_cases: [c1, c2]
  - issue_id: i2
`)
	sigs, err := analysisIssueSignatures(analysis)
	if err != nil {
		t.Fatalf("analysisIssueSignatures: %v", err)
	}
	if sigs["i2"] != "i2" {
		t.Fatalf("无语义信息 issue 应原样: %s", sigs["i2"])
	}
	if len(sigs["i1"]) != 64 {
		t.Fatalf("有语义信息 issue 应 sha256: %s", sigs["i1"])
	}
	sigs2, _ := analysisIssueSignatures(analysis)
	if sigs["i1"] != sigs2["i1"] {
		t.Fatalf("语义键应稳定")
	}
}

func TestAnalysisIssueCaseIDs(t *testing.T) {
	dir := t.TempDir()
	analysis := filepath.Join(dir, "analysis.yaml")
	writeTestYAML(t, analysis, `issues:
  - issue_id: i1
    affected_cases: [c1]
    evidence:
      - case_id: c2
  - issue_id: ""
`)
	got, err := analysisIssueCaseIDs(analysis)
	if err != nil {
		t.Fatalf("analysisIssueCaseIDs: %v", err)
	}
	cases := got["i1"]
	if _, ok := cases["c1"]; !ok {
		t.Fatalf("affected_cases 应计入")
	}
	if _, ok := cases["c2"]; !ok {
		t.Fatalf("evidence case 应计入")
	}
	if _, ok := got[""]; ok {
		t.Fatalf("空 issue_id 应跳过")
	}
}

func TestCapabilityActionIDs_与ExpectedRuntimeNames(t *testing.T) {
	gates := []map[string]any{{
		"capabilities": []any{
			map[string]any{"action_id": "a2", "action_group": "prompt",
				"operation": "modify", "runtime_name": "r2"},
			map[string]any{"action_id": "a1"},
			map[string]any{"action_id": "a2", "action_group": "skill",
				"operation": "add", "runtime_name": "r1"},
			map[string]any{"action_id": "", "action_group": "prompt",
				"operation": "add", "runtime_name": "r3"},
			map[string]any{"action_id": "a3", "action_group": "prompt",
				"operation": "remove", "runtime_name": "r4"},
		},
	}}
	if ids := capabilityActionIDs(gates); len(ids) != 3 || ids[0] != "a1" || ids[1] != "a2" || ids[2] != "a3" {
		t.Fatalf("action ids 应排序去重（空 ID 跳过）: %v", ids)
	}
	if names := expectedRuntimeNames(
		[]map[string]any{
			{"action_group": "prompt", "operation": "modify", "runtime_name": "r2"},
			{"action_group": "prompt", "operation": "remove", "runtime_name": "r4"},
			{"action_group": "skill", "operation": "add", "runtime_name": "r1"},
		}, "prompt"); len(names) != 1 || names[0] != "r2" {
		t.Fatalf("只统计目标组 add/modify: %v", names)
	}
}

func TestRejectedCapabilityHistory_复制门字段(t *testing.T) {
	gates := []map[string]any{{
		"status":              "rejected",
		"reason":              "regressed",
		"failure_class":       "score_failure",
		"target_confirmation": map[string]any{"ok": true},
		"capabilities": []any{
			map[string]any{"action_id": "a1", "role": "coder"},
			"not-a-dict",
		},
	}, {
		"status": "accepted",
		"capabilities": []any{
			map[string]any{"action_id": "a2"},
		},
	}}
	history := rejectedCapabilityHistory(gates)
	if len(history) != 1 {
		t.Fatalf("只收被拒门: %d", len(history))
	}
	if history[0]["rejection_reason"] != "regressed" ||
		history[0]["failure_class"] != "score_failure" {
		t.Fatalf("门级字段应复制: %v", history[0])
	}
	if _, ok := history[0]["target_confirmation"].(map[string]any); !ok {
		t.Fatalf("copied_fields 应为映射: %v", history[0]["target_confirmation"])
	}
}

func TestAppendTaskContract_契约注入(t *testing.T) {
	cases := []map[string]any{
		{"case_id": "c1", "input": "原始输入"},
		{"case_id": "c2", "input": map[string]any{"user_message": "原始消息"}},
		{"case_id": "c3", "input": map[string]any{"other": 1, "extra": 2}},
	}
	contract := map[string]any{"case_id": "c1", "semantic_invariant": "x"}
	for _, singleCase := range cases {
		appendTaskContract(singleCase, contract)
	}
	if v, _ := cases[0]["input"].(string); !strings.Contains(v, "<task-acceptance-contract>") ||
		!strings.HasPrefix(v, "原始输入") {
		t.Fatalf("字符串输入应追加契约: %q", v)
	}
	if m, _ := cases[1]["input"].(map[string]any); !strings.Contains(
		m["user_message"].(string), "task-acceptance-contract") {
		t.Fatalf("user_message 形态应改写字段: %v", m)
	}
	if v, _ := cases[2]["input"].(string); !strings.Contains(v, "task-acceptance-contract") {
		t.Fatalf("非字符串非 user_message 形态应 JSON 化后追加: %q", v)
	}
}

func TestBindTaskAcceptanceContracts_绑定与豁免(t *testing.T) {
	dir := t.TempDir()
	// 空 analysis/空目标集 → 原样返回
	cases := []map[string]any{{"case_id": "c1", "input": "x"}}
	bound, contracts, err := bindTaskAcceptanceContracts(cases, "", map[string]struct{}{})
	if err != nil || len(bound) != 1 || len(contracts) != 0 {
		t.Fatalf("空输入应原样返回: %v %v %v", bound, contracts, err)
	}
	// 有诊断件：目标用例绑定契约
	diagnoses := filepath.Join(dir, "diagnoses.json")
	writeTestYAML(t, diagnoses, `{"per_case_diagnoses":[{"case_id":"c1","root_cause":"rc",
		"recommendation":"fix","verifier_failure_output_excerpt":"boom",
		"verifier_observations":{"failed_fail_to_pass_tests":["t1"],
		"failed_pass_to_pass_tests":[]}}]}`)
	analysis := filepath.Join(dir, "analysis.yaml")
	writeTestYAML(t, analysis, "metadata:\n  per_case_diagnoses_path: "+diagnoses+"\n")
	bound, contracts, err = bindTaskAcceptanceContracts(
		cases, analysis, map[string]struct{}{"c1": {}})
	if err != nil {
		t.Fatalf("bindTaskAcceptanceContracts: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("应绑定 1 份契约: %v", contracts)
	}
	if contracts["c1"]["semantic_invariant"] != "rc" {
		t.Fatalf("root_cause 应进 semantic_invariant: %v", contracts["c1"])
	}
	if v, _ := bound[0]["input"].(string); !strings.Contains(v, "task-acceptance-contract") {
		t.Fatalf("绑定用例应注入契约文本: %q", v)
	}
}

func TestUpdateBatchAttemptRecord(t *testing.T) {
	gate := map[string]any{
		"member_optimization_ref_path": "/m1",
		"status":                       "rejected",
		"reason":                       "regressed",
	}
	batch := map[string]any{"candidate_attempts": []any{
		map[string]any{"member_optimization_ref_path": "/m1"},
		map[string]any{"member_optimization_ref_path": "/m2"},
		"not-a-dict",
	}}
	updateBatchAttemptRecord(batch, gate)
	attempts := batch["candidate_attempts"].([]any)
	if attempts[0].(map[string]any)["candidate_gate_status"] != "rejected" {
		t.Fatalf("匹配尝试应更新: %v", attempts[0])
	}
	if _, ok := attempts[1].(map[string]any)["candidate_gate_status"]; ok {
		t.Fatalf("不匹配尝试不应更新: %v", attempts[1])
	}
}

func TestPersistPromotion_接受与拒绝(t *testing.T) {
	dir := t.TempDir()
	memberRef := filepath.Join(dir, "member.yaml")
	manifestPath := filepath.Join(dir, "manifest.yaml")
	writeTestYAML(t, memberRef, "metadata:\n  candidate_manifest_path: "+manifestPath+"\ncandidate_ready_roles: [coder]\n")
	// Python: manifest 不存在则跳过（is_file 检查）——真实流程中 manifest 由优化器预先写好
	writeTestYAML(t, manifestPath, "status: candidate\n")
	gateAccepted := map[string]any{
		"status": "accepted", "accepted": true,
		"reason": "improved", "source_score": 0.5, "candidate_score": 0.9,
		"score_delta": 0.4, "target_case_ids": []any{"c1"},
		"target_confirmation": map[string]any{}, "epoch_checkpoint_outcome": map[string]any{},
	}
	if err := persistPromotion(memberRef, memberRef, gateAccepted); err != nil {
		t.Fatalf("persistPromotion: %v", err)
	}
	payload, err := readYAMLFile(memberRef)
	if err != nil {
		t.Fatalf("回读: %v", err)
	}
	if payload["promotion_status"] != "promoted" {
		t.Fatalf("接受应 promoted: %v", payload["promotion_status"])
	}
	if roles, _ := payload["promoted_roles"].([]any); len(roles) != 1 || roles[0] != "coder" {
		t.Fatalf("promoted_roles 应含 coder: %v", payload["promoted_roles"])
	}
	manifest, _ := readYAMLFile(filepath.Join(dir, "manifest.yaml"))
	if manifest["status"] != "accepted" {
		t.Fatalf("manifest 状态应 accepted: %v", manifest["status"])
	}
	// 拒绝路径
	gateRejected := map[string]any{
		"status": "rejected", "accepted": false,
		"reason": "regressed", "source_score": 0.5, "candidate_score": 0.4,
	}
	if err := persistPromotion(memberRef, memberRef, gateRejected); err != nil {
		t.Fatalf("persistPromotion 拒绝: %v", err)
	}
	payload, _ = readYAMLFile(memberRef)
	if payload["promotion_status"] != "rejected" {
		t.Fatalf("拒绝应保持 rejected: %v", payload["promotion_status"])
	}
	if roles, _ := payload["promoted_roles"].([]any); len(roles) != 0 {
		t.Fatalf("拒绝时 promoted_roles 应空: %v", payload["promoted_roles"])
	}
}

func TestRefreshOptimizationExperience_日志与记分板(t *testing.T) {
	dir := t.TempDir()
	state := map[string]any{
		"candidate_gates": []any{
			map[string]any{
				"status": "accepted", "epoch": 1, "batch_index": 2,
				"capabilities": []any{
					map[string]any{
						"action_id": "a1", "action_group": "prompt",
						"lever_decision": map[string]any{"selected_lever": "clarify"},
					},
				},
				"target_case_ids":    []any{"c1"},
				"target_score_delta": 0.5,
				"reason":             "better",
			},
			map[string]any{"status": "inconclusive"},
			map[string]any{"status": "rejected", "failure_class": "infrastructure_or_evidence_failure"},
		},
	}
	if err := refreshOptimizationExperience(state, dir); err != nil {
		t.Fatalf("refreshOptimizationExperience: %v", err)
	}
	journal, _ := state["optimization_journal"].([]any)
	if len(journal) != 1 {
		t.Fatalf("inconclusive/基础设施失败门应排除: %d", len(journal))
	}
	record := journal[0].(map[string]any)
	if record["experiment_id"] != "e001-b002-a1" {
		t.Fatalf("experiment_id 格式: %v", record["experiment_id"])
	}
	if record["outcome"] != "flipped" {
		t.Fatalf("accepted+正增量应 flipped: %v", record["outcome"])
	}
	scoreboard, _ := state["lever_scoreboard"].(map[string]any)
	entry, _ := scoreboard["clarify"].(map[string]any)
	if entry["attempts"] != 1 || entry["accepted"] != 1 {
		t.Fatalf("记分板统计: %v", entry)
	}
	if entry["average_target_score_delta"].(float64) != 0.5 {
		t.Fatalf("平均增量: %v", entry["average_target_score_delta"])
	}
	if !fileExists(filepath.Join(dir, "optimization_journal.yaml")) {
		t.Fatalf("日志文件应落盘")
	}
	if !fileExists(filepath.Join(dir, "lever_scoreboard.yaml")) {
		t.Fatalf("记分板文件应落盘")
	}
}

func TestCandidateCapabilities_从计划提取(t *testing.T) {
	dir := t.TempDir()
	analysis := filepath.Join(dir, "analysis.yaml")
	writeTestYAML(t, analysis, `issues:
  - issue_id: i1
    affected_cases: [c1]
`)
	plan := filepath.Join(dir, "plan.yaml")
	writeTestYAML(t, plan, `actions:
  - action_id: a1
    role: coder
    action_group: prompt
    operation: modify
    target_path: /h/prompt.md
    description: d
    rationale: r
    intervention: i
    expected_effect: e
    attributed_issue_ids: [i1]
    constraints:
      optimization_contracts:
        - decision_contract:
            activation_phase: diagnose
          hypothesis_id: h1
          content_sha256: abc
      lever_decision:
        selected_lever: clarify
targets:
  - role: coder
    attributed_issue_ids: [i1]
`)
	memberInfo := map[string]any{
		"plan_path": plan,
		"metadata":  map[string]any{"analysis_result_path": analysis},
	}
	caps, err := candidateCapabilities(memberInfo)
	if err != nil {
		t.Fatalf("candidateCapabilities: %v", err)
	}
	if len(caps) != 1 {
		t.Fatalf("应提取 1 个能力: %d", len(caps))
	}
	capability := caps[0]
	if capability["action_id"] != "a1" || capability["runtime_name"] != "prompt" ||
		capability["activation_phase"] != "diagnose" {
		t.Fatalf("能力字段: %v", capability)
	}
	if ids, _ := capability["target_case_ids"].([]string); len(ids) != 1 || ids[0] != "c1" {
		t.Fatalf("目标用例应由 issue 归并: %v", capability["target_case_ids"])
	}
	if ids, _ := capability["optimization_hypothesis_ids"].([]string); len(ids) != 1 || ids[0] != "h1" {
		t.Fatalf("假设 ID 应提取: %v", capability["optimization_hypothesis_ids"])
	}
}

func TestMemberCompositionMode(t *testing.T) {
	if got := memberCompositionMode(map[string]any{
		"composition_mode": "solo"}); got != "solo" {
		t.Fatalf("顶层字段优先: %s", got)
	}
	if got := memberCompositionMode(map[string]any{
		"metadata": map[string]any{"composition_mode": "pair"}}); got != "pair" {
		t.Fatalf("回退 metadata: %s", got)
	}
	if got := memberCompositionMode(map[string]any{}); got != "" {
		t.Fatalf("缺省空串: %s", got)
	}
}

func TestPriorCandidateFeedback_最近三条(t *testing.T) {
	journal := []any{}
	for i := 0; i < 5; i++ {
		journal = append(journal, map[string]any{
			"experiment_id": "e" + string(rune('0'+i)),
			"verifier_deltas_by_case": map[string]any{
				"c1": map[string]any{"delta": i},
			},
			"candidate_failure_diagnoses": map[string]any{
				"c1": map[string]any{"root_cause": "rc"},
			},
			"candidate_patch_excerpts_by_case": map[string]any{"c1": "patch"},
			"candidate_behavior_by_case":       map[string]any{"c1": map[string]any{"ok": true}},
		})
	}
	state := map[string]any{"optimization_journal": journal}
	cases := []map[string]any{{"case_id": "c1"}}
	feedback := priorCandidateFeedback(state, cases)
	byCase, _ := feedback["by_case"].(map[string]any)
	records, _ := byCase["c1"].([]map[string]any)
	if len(records) != 3 {
		t.Fatalf("应只保留最近三条: %d", len(records))
	}
	last := records[2]
	if last["candidate_patch_excerpt"] != "patch" {
		t.Fatalf("补丁摘要应透传: %v", last)
	}
	if _, ok := last["candidate_failure_diagnosis"].(map[string]any); !ok {
		t.Fatalf("单数诊断字段应为首条: %v", last["candidate_failure_diagnosis"])
	}
}

func TestCasesWithIDs_保留源顺序(t *testing.T) {
	cases := []map[string]any{
		{"case_id": "c1"}, {"case_id": "c2"}, {"case_id": "c3"},
	}
	got := casesWithIDs(cases, map[string]struct{}{"c3": {}, "c1": {}})
	if len(got) != 2 || got[0]["case_id"] != "c1" || got[1]["case_id"] != "c3" {
		t.Fatalf("应保留源顺序: %v", got)
	}
}

func TestCompactAnalysisDiagnoses_字段白名单(t *testing.T) {
	dir := t.TempDir()
	diagnoses := filepath.Join(dir, "d.json")
	writeTestYAML(t, diagnoses, `{"per_case_diagnoses":[
		{"case_id":"c1","summary":"s","noise":"drop"},
		{"case_id":"","summary":"skip"},
		"not-a-dict"]}`)
	analysis := filepath.Join(dir, "a.yaml")
	writeTestYAML(t, analysis, "metadata:\n  per_case_diagnoses_path: "+diagnoses+"\n")
	compact, err := compactAnalysisDiagnoses(analysis)
	if err != nil {
		t.Fatalf("compactAnalysisDiagnoses: %v", err)
	}
	entries := compact["c1"]
	if len(entries) != 1 {
		t.Fatalf("c1 应 1 条: %v", compact)
	}
	if _, ok := entries[0]["summary"]; !ok {
		t.Fatalf("白名单字段应保留: %v", entries[0])
	}
	if _, ok := entries[0]["noise"]; ok {
		t.Fatalf("白名单外字段应丢弃: %v", entries[0])
	}
	// 缺失文件 → 空映射
	empty, err := compactAnalysisDiagnoses(filepath.Join(dir, "missing.yaml"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("缺失诊断件应空映射: %v %v", empty, err)
	}
}

func TestRaiseIfCancelled_未取消放行(t *testing.T) {
	if err := raiseIfCancelled(context.Background()); err != nil {
		t.Fatalf("未取消应放行: %v", err)
	}
}

func TestNumberOf_形态矩阵(t *testing.T) {
	if numberOf(true) != nil || numberOf(nil) != nil {
		t.Fatalf("bool/nil 应 nil")
	}
	if v := numberOf(1.5); v == nil || *v != 1.5 {
		t.Fatalf("float 应提取")
	}
	if v := numberOf(2); v == nil || *v != 2.0 {
		t.Fatalf("int 应提取")
	}
	if v := numberOf("3.5"); v == nil || *v != 3.5 {
		t.Fatalf("数字字符串应提取")
	}
	if numberOf("abc") != nil {
		t.Fatalf("非数字字符串应 nil")
	}
}

func TestTargetCaseIDs_evidence归并(t *testing.T) {
	dir := t.TempDir()
	analysis := filepath.Join(dir, "analysis.yaml")
	writeTestYAML(t, analysis, `issues:
  - issue_id: i1
    affected_cases: [c1]
`)
	plan := map[string]any{
		"targets": []any{
			map[string]any{
				"role":                 "coder",
				"attributed_issue_ids": []any{"i1"},
				"evidence_refs": []any{
					map[string]any{"case_id": "c2"},
					map[string]any{"issue_id": "i1"},
				},
			},
			map[string]any{"role": "", "attributed_issue_ids": []any{"i1"}},
		},
	}
	memberInfo := map[string]any{
		"metadata": map[string]any{"analysis_result_path": analysis},
	}
	issueCaseIDs, byRole, err := targetCaseIDs(plan, memberInfo)
	if err != nil {
		t.Fatalf("targetCaseIDs: %v", err)
	}
	roleCases := byRole["coder"]
	if _, ok := roleCases["c1"]; !ok {
		t.Fatalf("issue 归并应计入 c1")
	}
	if _, ok := roleCases["c2"]; !ok {
		t.Fatalf("evidence case 应计入 c2")
	}
	if len(issueCaseIDs) != 1 {
		t.Fatalf("issue 映射: %v", issueCaseIDs)
	}
}

func TestLlmJudgeProtocol_非LLM返回空(t *testing.T) {
	cfg := fakeJudgeConfig{method: "exact_match", scoreMode: "binary"}
	if got := llmJudgeProtocol(cfg); len(got) != 0 {
		t.Fatalf("非 llm_as_judge 应空: %v", got)
	}
	cfg = fakeJudgeConfig{method: "llm-as-judge", scoreMode: "binary"}
	got := llmJudgeProtocol(cfg)
	if got["llm_judge_score_contract"] != "threshold_binary_v1" {
		t.Fatalf("binary 契约: %v", got)
	}
	cfg = fakeJudgeConfig{method: "LLM_AS_JUDGE", scoreMode: "continuous"}
	got = llmJudgeProtocol(cfg)
	if got["llm_judge_score_contract"] != "continuous" {
		t.Fatalf("continuous 契约: %v", got)
	}
}

// fakeJudgeConfig 测试用 evaluator 配置面。
type fakeJudgeConfig struct {
	method    string
	scoreMode string
}

func (f fakeJudgeConfig) GetEvaluationMethod() string     { return f.method }
func (f fakeJudgeConfig) GetScoreMode() string            { return f.scoreMode }
func (f fakeJudgeConfig) GetJudgeBaselineComparison() any { return nil }

// makeTestDatasetArtifact 构造测试用数据集工件。
func makeTestDatasetArtifact(t *testing.T, dir string) harnessDatasetArtifact {
	t.Helper()
	caseCount := 1
	return harnessDatasetArtifact{
		DatasetID:    "ds1",
		DatasetDir:   dir,
		DatasetFiles: []string{filepath.Join(dir, "ds.json")},
		Cases:        &caseCount,
	}
}
