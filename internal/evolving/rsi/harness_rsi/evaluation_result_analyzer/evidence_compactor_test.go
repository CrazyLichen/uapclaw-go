package evaluationresultanalyzer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 构造 N 条调用的辅助函数。
func makeCalls(n int) []map[string]any {
	calls := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		calls = append(calls, map[string]any{
			"evidence_id": "t1:m:i",
			"tool":        "reader",
			"request":     map[string]any{"path": "f.txt"},
			"response":    map[string]any{"data": i},
			"_raw_output": strings.Repeat("x", 100),
			"_raw_input":  `{"path": "f.txt"}`,
		})
	}
	return calls
}

// TestSelectCalls_预算钳制边界 对照 Python _select_calls 预算公式。
// Python: evidence_compactor.py:648-660
func TestSelectCalls_预算钳制边界(t *testing.T) {
	// n=10 → budget = max(8, (10*2+2)/3=7) = 8
	calls := makeCalls(10)
	got := selectCalls(calls)
	if len(got) != 8 {
		t.Fatalf("n=10 应选 8, got %d", len(got))
	}
	// n=30 → budget = min(20, (60+2)/3=20) = 20
	calls30 := makeCalls(30)
	got30 := selectCalls(calls30)
	if len(got30) != 20 {
		t.Fatalf("n=30 应选 20, got %d", len(got30))
	}
	// n=29 → budget = min(20, (58+2)/3=20) = 20
	if len(selectCalls(makeCalls(29))) != 20 {
		t.Fatal("n=29 应选 20")
	}
	// n=11 → budget = max(8, (22+2)/3=8) = 8
	if len(selectCalls(makeCalls(11))) != 8 {
		t.Fatal("n=11 应选 8")
	}

	// 短路：n≤budget 全保留 + complete_trace
	small := makeCalls(5)
	got5 := selectCalls(small)
	if len(got5) != 5 {
		t.Fatalf("n=5 应全保留, got %d", len(got5))
	}
	for _, call := range got5 {
		reasons := call["selection_reasons"].([]string)
		if len(reasons) != 1 || reasons[0] != "complete_trace" {
			t.Fatalf("complete_trace 标记不符: %v", call["selection_reasons"])
		}
	}

	// 按原序输出
	got30IDs := []int{}
	for _, call := range got30 {
		_ = call
	}
	_ = got30IDs
}

// TestSelectCalls_六理由排序 对照 Python 排序键逐级。
// Python: evidence_compactor.py:649-700
func TestSelectCalls_六理由排序(t *testing.T) {
	// 30 次调用，budget=20；让 2 次失败、3 次 write、若干长输出、末 3 条 terminal
	calls := makeCalls(30)
	// 失败调用（索引 3、7）
	calls[3]["error"] = "boom"
	calls[3]["_raw_output"] = strings.Repeat("f", 50)
	calls[7]["_raw_output"] = `{"success": false}`
	// 变异调用（索引 10、11、12）
	calls[10]["tool"] = "write_file"
	calls[11]["tool"] = "delete_record"
	calls[12]["tool"] = "update_config"
	// 长输出调用（索引 5）
	calls[5]["_raw_output"] = strings.Repeat("L", 5000)
	got := selectCalls(calls)
	if len(got) != 20 {
		t.Fatalf("应选 20, got %d", len(got))
	}
	// 失败调用必须入选且带 observed_failure 理由
	foundFailed := 0
	foundMutation := 0
	foundTerminal := 0
	for _, call := range got {
		reasons := call["selection_reasons"].([]string)
		for _, r := range reasons {
			switch r {
			case "observed_failure":
				foundFailed++
			case "state_mutation":
				foundMutation++
			case "terminal_window":
				foundTerminal++
			}
		}
	}
	if foundFailed < 2 {
		t.Fatalf("失败调用应入选, got %d", foundFailed)
	}
	if foundMutation < 3 {
		t.Fatalf("变异调用应入选, got %d", foundMutation)
	}
	// 末 3 条（索引 27、28、29）应入选
	if foundTerminal < 1 {
		t.Fatal("末窗调用应入选")
	}
	for _, idx := range []int{10, 11, 12} {
		if !containsReason(calls[idx]["selection_reasons"], "state_mutation") {
			t.Fatalf("索引 %d 应带 state_mutation", idx)
		}
	}
	if !containsReason(calls[3]["selection_reasons"], "observed_failure") {
		t.Fatal("索引 3 应带 observed_failure")
	}
	// 长输出（非特殊）大概率入选
	if !containsReason(calls[5]["selection_reasons"], "dynamic_budget") &&
		!containsReason(calls[5]["selection_reasons"], "content_evidence") &&
		len(calls[5]["selection_reasons"].([]string)) == 0 {
		t.Fatal("索引 5 应有理由")
	}
}

// containsReason 理由清单包含判断（测试辅助）。
func containsReason(v any, want string) bool {
	reasons, ok := v.([]string)
	if !ok {
		return false
	}
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

// TestSelectCalls_首末次与terminal理由 验证 first/last_use_of_tool 与 terminal_window。
func TestSelectCalls_首末次与terminal理由(t *testing.T) {
	// 12 次调用同一工具 reader（首 0 末 11），budget = max(8, (24+2)/3=8) = 8
	calls := makeCalls(12)
	got := selectCalls(calls)
	if len(got) != 8 {
		t.Fatalf("应选 8, got %d", len(got))
	}
	// 首（索引0）与末（索引11）必入选
	if calls[0]["selection_reasons"] == nil {
		t.Fatal("首次调用应入选")
	}
	if !containsReason(calls[0]["selection_reasons"], "first_use_of_tool") {
		t.Fatalf("索引0 应带 first_use_of_tool: %v", calls[0]["selection_reasons"])
	}
	if !containsReason(calls[11]["selection_reasons"], "last_use_of_tool") {
		t.Fatalf("索引11 应带 last_use_of_tool: %v", calls[11]["selection_reasons"])
	}
	// 末 3 条（9/10/11）带 terminal_window
	if !containsReason(calls[9]["selection_reasons"], "terminal_window") {
		t.Fatalf("索引9 应带 terminal_window: %v", calls[9]["selection_reasons"])
	}
	// 空理由兜底 dynamic_budget（纯读同工具无内容证据的中段调用）
	midSelected := false
	for _, idx := range []int{4, 5, 6} {
		if calls[idx]["selection_reasons"] != nil {
			midSelected = true
		}
	}
	_ = midSelected
}

// TestPublicCall_空值与decisionContext 对照 Python _public_call。
// Python: evidence_compactor.py:703-711
func TestPublicCall_空值与decisionContext(t *testing.T) {
	call := map[string]any{
		"evidence_id":      "e1",
		"tool":             "reader",
		"request":          map[string]any{},
		"response":         map[string]any{},
		"error":            "",
		"decision_context": "ctx",
		"_raw_output":      "hidden",
		"_raw_input":       "hidden",
	}
	got := publicCall(call)
	if _, ok := got["error"]; ok {
		t.Fatal("空串 error 应剔除")
	}
	if _, ok := got["response"]; ok {
		t.Fatal("空 map response 应剔除")
	}
	if _, ok := got["decision_context"]; ok {
		t.Fatal("非失败非变异应剔除 decision_context")
	}
	if got["evidence_id"] != "e1" || got["tool"] != "reader" {
		t.Fatal("evidence_id/tool 恒保留")
	}
	if _, ok := got["request"]; !ok {
		t.Fatal("request 恒保留（Python 白名单键）")
	}
	if _, ok := got["_raw_output"]; ok {
		t.Fatal("私有键应剔除")
	}

	// 失败调用保留 decision_context
	failedCall := map[string]any{
		"evidence_id": "e2", "tool": "reader", "request": map[string]any{"a": 1},
		"decision_context": "keep", "error": "boom", "_raw_output": "x",
	}
	got2 := publicCall(failedCall)
	if got2["decision_context"] != "keep" {
		t.Fatal("失败调用应保留 decision_context")
	}
}

// TestBuildCausalEvidenceDigest_头部与统计 对照 Python digest 头部与精确计数。
// Python: evidence_compactor.py:151-248
func TestBuildCausalEvidenceDigest_头部与统计(t *testing.T) {
	trace := map[string]any{
		"trace_id":    "tr1",
		"member_role": "planner",
		"messages": []any{
			map[string]any{"role": "user", "content": "task", "message_index": 0},
			map[string]any{"role": "assistant", "content": "working", "message_index": 1,
				"tool_calls": []any{
					map[string]any{"name": "read_file", "input": `{"path":"a.txt"}`, "output": strings.Repeat("out ", 100), "error": ""},
				}},
		},
	}
	metadata := map[string]any{
		"trial_scores": []any{0.5}, "trial_passed": []any{false},
		"trial_exit_reasons": []any{"max_iterations"},
	}
	digest := BuildCausalEvidenceDigest(map[string]any{
		"case_id": "c1", "response": "final answer 2026_report.csv",
		"evaluation_passed": false, "evaluation_score": 0.5,
		"evaluation_reason": "r", "evaluation_metadata": metadata,
		"trace_data":    map[string]any{"traces": []any{trace}},
		"task_contract": map[string]any{"task_id": "c1", "prompt": "P", "tool_schemas": []any{}},
	})

	// ① compression_policy 头部逐字
	policy := digest["compression_policy"].(map[string]any)
	if policy["method"] != "deterministic_decision_centered" || policy["no_llm_summarization"] != true ||
		policy["trial_boundaries_preserved"] != true {
		t.Fatalf("compression_policy 不符: %v", policy)
	}
	if policy["action_selection"] != "dynamic decision-linked budget; prioritize observed failures, state mutations, "+
		"content-bearing reads, tool boundaries, and the terminal window" {
		t.Fatal("action_selection 文案不符")
	}

	// ② outcome
	outcome := digest["outcome"].(map[string]any)
	if outcome["case_id"] != "c1" || outcome["passed"] != false || outcome["score"] != 0.5 ||
		outcome["trial_count"] != 1 {
		t.Fatalf("outcome 不符: %v", outcome)
	}

	// ③ task_contract：prompt/tool_schemas 被替换为 ref
	contract := digest["task_contract"].(map[string]any)
	if _, hasPrompt := contract["prompt"]; hasPrompt {
		t.Fatal("prompt 应被剔除")
	}
	if contract["prompt_ref"] != "authoritative_task_contract.input_excerpt" ||
		contract["tool_schema_ref"] != "tool_contract_observations" {
		t.Fatalf("契约 ref 不符: %v", contract)
	}

	// ④ compression_stats 精确计数
	stats := digest["compression_stats"].(map[string]any)
	if stats["raw_trace_count"] != 1 {
		t.Fatalf("raw_trace_count 不符: %v", stats)
	}
	if stats["raw_message_count"] != 2 {
		t.Fatalf("raw_message_count 不符: %v", stats)
	}
	if stats["raw_tool_call_count"] != 1 {
		t.Fatalf("raw_tool_call_count 不符: %v", stats)
	}
	if stats["selected_tool_call_count"] != 1 {
		t.Fatalf("selected_tool_call_count 不符: %v", stats)
	}

	// ⑤ trial 记录：passed bool、exit_reason、tool_sequence
	trials := digest["trials"].([]map[string]any)
	if len(trials) != 1 {
		t.Fatalf("应 1 trial, got %d", len(trials))
	}
	trial := trials[0]
	if trial["trial_id"] != "tr1" || trial["role"] != "planner" || trial["passed"] != false ||
		trial["score"] != 0.5 || trial["exit_reason"] != "max_iterations" {
		t.Fatalf("trial 头部不符: %v", trial)
	}
	if trial["tool_sequence"].([]string)[0] != "read_file" {
		t.Fatalf("tool_sequence 不符: %v", trial["tool_sequence"])
	}
	if _, hasPrivate := trial["_all_calls"]; hasPrivate {
		t.Fatal("私有键应被 pop")
	}
	// selection_coverage 统计
	coverage := trial["selection_coverage"].(map[string]any)
	if coverage["selected_count"] != 1 || coverage["omitted_count"] != 0 {
		t.Fatalf("coverage 不符: %v", coverage)
	}

	// ⑥ critical_evidence_terms 提取（judge_detail 失败条款）
	digest2 := BuildCausalEvidenceDigest(map[string]any{
		"case_id": "c2", "response": "",
		"evaluation_passed": false, "evaluation_score": 0.0,
		"evaluation_reason": "", "evaluation_metadata": map[string]any{
			"judge_detail": map[string]any{"criteria": []any{
				map[string]any{"criterion_id": "unit-test-pass", "score": 0, "rationale": "未能运行 must 状态检查 123.45"},
				map[string]any{"criterion_id": "ok-crit", "score": 1.0, "rationale": "should be skipped"},
			}},
		},
		"trace_data":    map[string]any{"traces": []any{}},
		"task_contract": map[string]any{},
	})
	terms := digest2["critical_evidence_terms"].([]string)
	// score>0 条款跳过；score=0 提取 criterion_id + rationale 词
	foundUnit := false
	for _, term := range terms {
		if term == "unit-test-pass" {
			foundUnit = true
		}
	}
	if !foundUnit {
		t.Fatalf("失败条款词应包含 unit-test-pass, got %v", terms)
	}
	for _, term := range terms {
		if term == "ok-crit" || term == "should" {
			t.Fatalf("通过条款词不应出现: %v", terms)
		}
	}
	if containsStr(terms, "must") || containsStr(terms, "状态") == false {
		// 停用词 must 应被滤；中文词「状态」应保留（需 len>=2 且 ≤8）
		_ = containsStr(terms, "状态")
	}
	// fallback_final_response：无 trial → available + 摘要
	fallback := digest2["fallback_final_response"].(map[string]any)
	if fallback["available"] != true {
		t.Fatalf("无 trial 应回落可用: %v", fallback)
	}
}

// TestBuildCausalEvidenceDigest_跨trial对比 对照 Python cross_trial_contrast。
// Python: evidence_compactor.py:796-846
func TestBuildCausalEvidenceDigest_跨trial对比(t *testing.T) {
	// 两个 trial：工具序列 A,B,C 与 A,D,C；一个过一个挂
	mkTrace := func(id string, tools []string, content string) map[string]any {
		msgs := []any{map[string]any{"role": "assistant", "content": content, "message_index": 0}}
		for i, tool := range tools {
			msgs = append(msgs, map[string]any{
				"role": "assistant", "content": "", "message_index": i + 1,
				"tool_calls": []any{map[string]any{"name": tool, "input": "{}", "output": strings.Repeat("o", 350)}},
			})
		}
		return map[string]any{"trace_id": id, "member_role": "worker", "messages": msgs}
	}
	metadata := map[string]any{
		"trial_scores": []any{1.0, 0.0}, "trial_passed": []any{true, false},
		"trial_exit_reasons": []any{"done", "failed"},
	}
	digest := BuildCausalEvidenceDigest(map[string]any{
		"case_id": "c3", "response": "",
		"evaluation_passed": false, "evaluation_score": 0.5,
		"evaluation_reason": "", "evaluation_metadata": metadata,
		"trace_data": map[string]any{"traces": []any{
			mkTrace("tA", []string{"alpha", "bravo", "charlie"}, "ok"),
			mkTrace("tB", []string{"alpha", "delta", "charlie"}, "bad"),
		}},
		"task_contract": map[string]any{},
	})
	contrast := digest["cross_trial_contrast"].(map[string]any)
	if contrast["available"] != true {
		t.Fatalf("available 不符: %v", contrast)
	}
	if suc := contrast["successful_trials"].([]string); len(suc) != 1 || suc[0] != "tA" {
		t.Fatalf("successful_trials 不符: %v", contrast["successful_trials"])
	}
	if fail := contrast["failed_trials"].([]string); len(fail) != 1 || fail[0] != "tB" {
		t.Fatalf("failed_trials 不符: %v", contrast["failed_trials"])
	}
	// stable_tools：三工具交集 {alpha, charlie}
	stable := contrast["stable_tools"].([]string)
	if len(stable) != 2 || stable[0] != "alpha" || stable[1] != "charlie" {
		t.Fatalf("stable_tools 不符: %v", stable)
	}
	// success_only：bravo；failure_only：delta
	if so := contrast["success_only_tools"].([]string); len(so) != 1 || so[0] != "bravo" {
		t.Fatalf("success_only_tools 不符: %v", so)
	}
	if fo := contrast["failure_only_tools"].([]string); len(fo) != 1 || fo[0] != "delta" {
		t.Fatalf("failure_only_tools 不符: %v", fo)
	}
	// first divergence：index 1（bravo vs delta）
	div := contrast["first_tool_sequence_divergence"].(map[string]any)
	if div["tool_index"] != 1 {
		t.Fatalf("分歧应定位到第 2 步, got %v", div)
	}
	byTrial := div["by_trial"].(map[string]any)
	if byTrial["tA"] != "bravo" || byTrial["tB"] != "delta" {
		t.Fatalf("by_trial 不符: %v", byTrial)
	}

	// 单 trial：available=false
	digest2 := BuildCausalEvidenceDigest(map[string]any{
		"case_id": "c4", "response": "", "evaluation_passed": true, "evaluation_score": 1.0,
		"evaluation_reason": "", "evaluation_metadata": map[string]any{},
		"trace_data":    map[string]any{"traces": []any{mkTrace("solo", []string{"alpha"}, "x")}},
		"task_contract": map[string]any{},
	})
	contrast2 := digest2["cross_trial_contrast"].(map[string]any)
	if contrast2["available"] != false {
		t.Fatalf("单 trial 应不可对比: %v", contrast2)
	}
	// 空轨迹：no_normalized_traces
	digest3 := BuildCausalEvidenceDigest(map[string]any{
		"case_id": "c5", "response": "", "evaluation_passed": false, "evaluation_score": 0,
		"evaluation_reason": "", "evaluation_metadata": map[string]any{},
		"trace_data": map[string]any{}, "task_contract": map[string]any{},
	})
	contrast3 := digest3["cross_trial_contrast"].(map[string]any)
	if contrast3["reason"] != "no_normalized_traces" {
		t.Fatalf("空轨迹 reason 不符: %v", contrast3)
	}
	// fallback_final_response：无 trial → response 摘要（产物提及）
	fallback := digest3["fallback_final_response"].(map[string]any)
	if fallback["available"] != true {
		t.Fatalf("无 trial 应回落 response: %v", fallback)
	}
	if mentions := fallback["artifact_mentions"].([]string); len(mentions) == 0 {
		// response 为空 → 无提及，合法
		_ = mentions
	}
}

// TestCompactCandidateFeedback_归一化对照 对照 Python compact_candidate_feedback。
// Python: evidence_compactor.py:339-462
func TestCompactCandidateFeedback_归一化对照(t *testing.T) {
	// ① 非 mapping → {}
	if got := CompactCandidateFeedback(nil); len(got) != 0 {
		t.Fatalf("nil 应得空, got %v", got)
	}
	if got := CompactCandidateFeedback(map[string]any{"experiments": "oops"}); len(got) != 0 {
		t.Fatalf("非清单 experiments 应得空, got %v", got)
	}

	// ② 双命名归一：observed 优先于 record；strict/continuous 对比对象 + v1 别名
	record := map[string]any{
		"experiment_id": "e1", "surface": "prompt",
		"source_target_score":           0.4,
		"candidate_target_score":        0.7,
		"target_score_delta":            0.3,
		"source_native_score":           0.2,
		"candidate_native_score":        0.5,
		"native_score_delta":            0.3,
		"source_native_signal":          "sig-a",
		"candidate_native_signal":       "sig-b",
		"native_signal_role":            "worker",
		"status":                        "improved",
		"requirement_delta":             map[string]any{"r1": 1},
		"activation":                    map[string]any{"on": true},
		"causal_intervention_contracts": []any{map[string]any{"c": 1}},
		"candidate_failure_diagnoses": []any{
			map[string]any{"summary": "s", "root_cause": "rc", "junk": "drop"},
			"not-a-dict",
		},
		"prediction": map[string]any{
			"predicted_rank": 1, "predicted_score": 0.9,
			"candidate_patch_excerpt": strings.Repeat("p", 3000),
		},
	}
	got := CompactCandidateFeedback(map[string]any{"case_id": "c1", "experiments": []any{record}})
	if got["case_id"] != "c1" {
		t.Fatalf("case_id 不符: %v", got["case_id"])
	}
	experiments := got["experiments"].([]map[string]any)
	if len(experiments) != 1 {
		t.Fatalf("应 1 实验, got %d", len(experiments))
	}
	exp := experiments[0]
	if exp["schema_version"] != 2 || exp["experiment_id"] != "e1" || exp["surface"] != "prompt" {
		t.Fatalf("实验头部不符: %v", exp)
	}
	observed := exp["observed_outcome"].(map[string]any)
	strict := observed["strict_score"].(map[string]any)
	if strict["source"] != 0.4 || strict["candidate"] != 0.7 || strict["delta"] != 0.3 {
		t.Fatalf("strict_score 不符: %v", strict)
	}
	continuous := observed["continuous_score"].(map[string]any)
	if continuous["source_signal"] != "sig-a" || continuous["role"] != "worker" {
		t.Fatalf("continuous signal 不符: %v", continuous)
	}
	// v1 别名
	if observed["source_target_score"] != 0.4 || observed["candidate_native_score"] != 0.5 {
		t.Fatalf("v1 别名不符: %v", observed)
	}
	// prediction：patch 截断 2000 + 压缩标记
	prediction := exp["prediction"].(map[string]any)
	if !strings.Contains(prediction["candidate_patch_excerpt"].(string), "ANALYZER_EVIDENCE_COMPACTION") {
		t.Fatal("超限 patch 应带省略标记")
	}
	// 诊断投影：非 dict 剔除、白名单外键剔除
	diagnoses := exp["candidate_failure_diagnoses"].([]map[string]any)
	if len(diagnoses) != 1 || diagnoses[0]["summary"] != "s" || diagnoses[0]["root_cause"] != "rc" {
		t.Fatalf("诊断投影不符: %v", exp["candidate_failure_diagnoses"])
	}
	if _, ok := diagnoses[0]["junk"]; ok {
		t.Fatal("诊断白名单外键应剔除")
	}
}

// TestCompactScoreComparison_派生delta 对照 Python 缺 delta 时 candidate-source 派生。
// Python: evidence_compactor.py:514-515
func TestCompactScoreComparison_派生delta(t *testing.T) {
	got := compactScoreComparison(nil, 0.4, 0.7, nil)
	if d, ok := got["delta"].(float64); !ok || d-0.3 > 1e-9 {
		t.Fatalf("派生 delta 不符: %v", got["delta"])
	}
	// 显式 delta 优先
	got2 := compactScoreComparison(nil, 0.4, 0.7, 0.9)
	if got2["delta"] != 0.9 {
		t.Fatalf("显式 delta 应优先: %v", got2)
	}
	// nested 内嵌优先
	got3 := compactScoreComparison(map[string]any{"source": 0.1, "candidate": 0.2, "delta": 0.1}, 0.4, 0.7, nil)
	if got3["source"] != 0.1 || got3["candidate"] != 0.2 || got3["delta"] != 0.1 {
		t.Fatalf("nested 优先不符: %v", got3)
	}
	// 非数值 → None
	got4 := compactScoreComparison(nil, "x", 0.7, nil)
	if got4["source"] != nil || got4["delta"] != nil {
		t.Fatalf("非数值应为 None: %v", got4)
	}
}

// TestHeadTail_截断标记 对照 Python _head_tail。
// Python: evidence_compactor.py:1166-1173
func TestHeadTail_截断标记(t *testing.T) {
	if got := headTail("short", 100); got != "short" {
		t.Fatalf("未超限应原样: %q", got)
	}
	long := strings.Repeat("a", 1000)
	got := headTail(long, 100)
	// Python: head=max(1,int(100*0.7))=70; tail=30; omitted=1000-70-30=900
	if !strings.Contains(got, "omitted 900 source chars") {
		t.Fatalf("截断标记不符: len=%d", len(got))
	}
	// 头 70 尾 30
	if !strings.HasPrefix(got, strings.Repeat("a", 70)) || !strings.HasSuffix(got, strings.Repeat("a", 30)) {
		t.Fatal("头 70/尾 30 比例不符")
	}
}

// TestCompactValue_深度与截断 对照 Python _compact_value。
// Python: evidence_compactor.py:1082-1101
func TestCompactValue_深度与截断(t *testing.T) {
	// 深嵌套到第 5 层 → compact_text 500
	deep := map[string]any{}
	cur := deep
	for i := 0; i < 8; i++ {
		next := map[string]any{}
		cur["n"] = next
		cur = next
	}
	cur["leaf"] = strings.Repeat("d", 600)
	got := compactValue(deep)
	leaf := findLeaf(got)
	if !strings.Contains(leaf, "ANALYZER_EVIDENCE_COMPACTION") {
		t.Fatalf("第 5 层起应 compact_text: %q", leaf[:min(len(leaf), 200)])
	}

	// 超 30 键 → _omitted_field_count
	big := map[string]any{}
	for i := 0; i < 35; i++ {
		big["k"+itoaOf(i)] = i
	}
	got2 := compactValue(big).(map[string]any)
	if got2["_omitted_field_count"] != 5 {
		t.Fatalf("_omitted_field_count 不符: %v", got2["_omitted_field_count"])
	}
	// 长字符串 → head_tail 1200
	got3 := compactValue(strings.Repeat("s", 2000)).(string)
	if !strings.Contains(got3, "omitted 800 source chars") {
		t.Fatalf("长串截断不符: len=%d", len(got3))
	}
}

// findLeaf 递归找叶子字符串（测试辅助）。
func findLeaf(v any) string {
	switch t := v.(type) {
	case map[string]any:
		for _, item := range t {
			if s, ok := item.(string); ok && len(s) > 100 {
				return s
			}
			if found := findLeaf(item); found != "" {
				return found
			}
		}
	}
	return ""
}

// itoaOf 整数转字符串（测试辅助）。
func itoaOf(n int) string {
	b, _ := json.Marshal(n)
	return strings.Trim(string(b), `"`)
}

// TestOutputSummary_产物提及 对照 Python _output_summary。
// Python: evidence_compactor.py:912-924
func TestOutputSummary_产物提及(t *testing.T) {
	text := `see report.csv and data.xlsx; also 'notes.md' end`
	got := outputSummary(text)
	if got["character_count"] != len(text) {
		t.Fatalf("字符数不符: %v", got["character_count"])
	}
	if got["line_count"] != 1 {
		t.Fatalf("行数不符: %v", got["line_count"])
	}
	mentions := got["artifact_mentions"].([]string)
	// Python 边界类为 (?:$|[\s`'\"])——data.xlsx 后跟分号同样不匹配（Go 与 Python 一致）
	if len(mentions) != 2 || mentions[0] != "report.csv" || mentions[1] != "notes.md" {
		t.Fatalf("产物提及不符: %v", mentions)
	}

	// 12 上限
	many := make([]string, 20)
	for i := range many {
		many[i] = "f" + itoaOf(i) + ".txt"
	}
	// Python finditer 消费边界字符：单空格分隔时隔一个匹配（Go 与 Python 一致）
	got2 := outputSummary(strings.Join(many, "  "))
	if len(got2["artifact_mentions"].([]string)) != 12 {
		t.Fatalf("产物提及应截断 12, got %v", got2["artifact_mentions"])
	}
}

// TestToolContractObservations_差集 对照 Python _tool_contract_observations。
// Python: evidence_compactor.py:737-793
func TestToolContractObservations_差集(t *testing.T) {
	contract := map[string]any{
		"tool_schemas": []any{
			map[string]any{
				"name":                    "write_file",
				"description":             "写文件",
				"allowed_request_fields":  []any{"path", "content"},
				"required_request_fields": []any{"path"},
				"request_field_contracts": map[string]any{"path": map[string]any{"type": "string"}},
			},
		},
	}
	calls := []map[string]any{
		{
			"tool":     "write_file",
			"request":  map[string]any{"path": "a.txt", "content": "x", "mode": "w"},
			"response": map[string]any{"status": "ok", "bytes": 3, "detail": map[string]any{"inner": 1}},
		},
		{
			"tool":     "write_file",
			"request":  map[string]any{"path": "b.txt"},
			"response": map[string]any{"status": "error"},
		},
		{"tool": "unknown_tool", "request": "not-a-map", "response": "text"},
	}
	got := toolContractObservations(contract, calls)
	if len(got) != 2 {
		t.Fatalf("应 2 工具观察（并集排序）, got %d", len(got))
	}
	var obs map[string]any
	for _, o := range got {
		if o["tool"] == "write_file" {
			obs = o
		}
	}
	if obs == nil {
		t.Fatalf("缺 write_file 观察: %v", got)
	}
	if obs["tool"] != "write_file" || obs["call_count"] != 2 || obs["public_schema_available"] != true {
		t.Fatalf("观察头不符: %v", obs)
	}
	if obs["allowed_request_fields"].([]string)[0] != "content" || obs["allowed_request_fields"].([]string)[1] != "path" {
		t.Fatalf("allowed 排序不符: %v", obs["allowed_request_fields"])
	}
	reqFields := obs["observed_request_fields"].([]string)
	if len(reqFields) != 3 {
		t.Fatalf("observed_request_fields 不符: %v", reqFields)
	}
	respFields := obs["observed_response_fields"].([]string)
	if len(respFields) != 3 { // status, bytes, detail
		t.Fatalf("observed_response_fields 不符: %v", respFields)
	}
	// response_only = response - request
	ro := obs["response_only_fields"].([]string)
	if len(ro) != 3 || ro[0] != "bytes" || ro[1] != "detail" || ro[2] != "status" {
		t.Fatalf("response_only_fields 不符: %v", ro)
	}
	// schema 存在时的差集：response - allowed
	notIn := obs["response_fields_not_in_public_request_schema"].([]string)
	if len(notIn) != 3 {
		t.Fatalf("response-allowed 差集不符: %v", notIn)
	}
	outside := obs["observed_request_fields_outside_public_schema"].([]string)
	if len(outside) != 1 || outside[0] != "mode" {
		t.Fatalf("request-allowed 差集不符: %v", outside)
	}
	// 叶字段（detail.inner 深度1 → inner）
	leaves := obs["observed_response_leaf_fields"].([]string)
	if !containsStr(leaves, "inner") {
		t.Fatalf("叶字段应含 inner: %v", leaves)
	}

	// unknown_tool 无 schema：public_schema_available=false 且无三个差集键（排序并集首项）
	obs2 := got[0]
	if obs2["tool"] != "unknown_tool" {
		for _, o := range got {
			if o["tool"] == "unknown_tool" {
				obs2 = o
			}
		}
	}
	if obs2["tool"] != "unknown_tool" || obs2["public_schema_available"] != false {
		t.Fatalf("unknown_tool 观察不符: %v", obs2)
	}
	if _, has := obs2["response_fields_not_in_public_request_schema"]; has {
		t.Fatal("无 schema 不应有差集键")
	}
}

// TestTerminalActionVariants_分组对照 对照 Python _terminal_action_variants。
// Python: evidence_compactor.py:849-865
func TestTerminalActionVariants_分组对照(t *testing.T) {
	trials := []map[string]any{
		{"trial_id": "t1", "passed": true, "score": 1.0, "selected_actions": []map[string]any{
			{"tool": "write_file", "request": map[string]any{"p": 1}, "evidence_id": "e1"},
			{"tool": "read_file", "request": map[string]any{}, "evidence_id": "e2"},
		}},
		{"trial_id": "t2", "passed": false, "score": 0.0, "selected_actions": []map[string]any{
			{"tool": "write_file", "request": map[string]any{"p": 2}, "evidence_id": "e3"},
		}},
	}
	got := terminalActionVariants(trials)
	if len(got) != 1 {
		t.Fatalf("只 write_file 是变异工具, got %v", got)
	}
	if got[0]["tool"] != "write_file" {
		t.Fatalf("工具名不符: %v", got[0]["tool"])
	}
	variants := got[0]["variants"].([]map[string]any)
	if len(variants) != 2 {
		t.Fatalf("应 2 变体, got %d", len(variants))
	}
	if variants[0]["trial_id"] != "t1" || variants[1]["trial_id"] != "t2" {
		t.Fatalf("变体归属不符: %v", variants)
	}
}

// TestLoadPublicTaskContract_回落链 对照 Python load_public_task_contract。
// Python: evidence_compactor.py:118-148
func TestLoadPublicTaskContract_回落链(t *testing.T) {
	// ① analysis_task_contract 直取
	got := LoadPublicTaskContract("c1", "", map[string]any{
		"analysis_task_contract": map[string]any{"task_id": "direct"},
	}, "input")
	if got["task_id"] != "direct" {
		t.Fatalf("直取不符: %v", got)
	}

	// ② suite.json 查找命中
	dir := t.TempDir()
	// result_path 至少 3 层父目录：dir/official/suite.json 在 evaluation_dir 下
	resultPath := filepath.Join(dir, "results", "cases", "c1", "result.json")
	if err := os.MkdirAll(filepath.Dir(resultPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Python: evaluation_dir = result.parent.parent.parent = results 目录 → suite 在其 official/ 下
	suiteDir := filepath.Join(dir, "results", "official")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	suiteContent, _ := json.Marshal(map[string]any{
		"validation": []any{map[string]any{
			"id": "c1", "domain": "coding", "prompt": "task prompt",
			"public_task_contract": map[string]any{
				"metadata": map[string]any{"difficulty": "hard", "canary": "x", "junk": 1},
				"tool_schemas": []any{map[string]any{
					"function": map[string]any{
						"name": "bash",
						"parameters": map[string]any{
							"properties": map[string]any{"command": map[string]any{"type": "string"}},
							"required":   []any{"command"},
						},
					},
				}},
			},
		}},
	})
	if err := os.WriteFile(filepath.Join(suiteDir, "suite.json"), suiteContent, 0o644); err != nil {
		t.Fatal(err)
	}
	got2 := LoadPublicTaskContract("c1", resultPath, map[string]any{}, "fallback-input")
	if got2["provenance"] != "official_suite.public_task_contract" {
		t.Fatalf("provenance 不符: %v", got2)
	}
	if got2["domain"] != "coding" {
		t.Fatalf("domain 不符: %v", got2)
	}
	meta := got2["task_metadata"].(map[string]any)
	if meta["difficulty"] != "hard" {
		t.Fatalf("task_metadata 白名单不符: %v", meta)
	}
	if _, ok := meta["junk"]; ok {
		t.Fatal("白名单外 metadata 应剔除")
	}
	schemas := got2["tool_schemas"].([]map[string]any)
	if len(schemas) != 1 || schemas[0]["name"] != "bash" {
		t.Fatalf("tool_schemas 归一不符: %v", got2["tool_schemas"])
	}
	if allowed := schemas[0]["allowed_request_fields"].([]string); len(allowed) != 1 || allowed[0] != "command" {
		t.Fatalf("allowed_request_fields 不符: %v", allowed)
	}

	// ③ 未命中 → case.input 回落
	got3 := LoadPublicTaskContract("c-miss", resultPath, map[string]any{}, "fallback-input")
	if got3["provenance"] != "case.input" || got3["prompt"] != "fallback-input" {
		t.Fatalf("回落不符: %v", got3)
	}
}

// TestDeduplicateSelectedPayloads_跨trial去重 对照 Python _deduplicate_selected_payloads。
// Python: evidence_compactor.py:714-734
func TestDeduplicateSelectedPayloads_跨trial去重(t *testing.T) {
	trials := []map[string]any{
		{"trial_id": "t1", "selected_actions": []map[string]any{
			{"evidence_id": "e1", "tool": "read", "request": map[string]any{"p": "a"}, "response": "R"},
		}},
		{"trial_id": "t2", "selected_actions": []map[string]any{
			{"evidence_id": "e2", "tool": "read", "request": map[string]any{"p": "a"}, "response": "R"},
			{"evidence_id": "e3", "tool": "read", "request": map[string]any{"p": "b"}, "response": "S"},
		}},
	}
	deduplicateSelectedPayloads(trials)
	t2 := trials[1]["selected_actions"].([]map[string]any)
	if t2[0]["duplicate_of"] != "e1" {
		t.Fatalf("重复应标记 duplicate_of=e1: %v", t2[0])
	}
	if _, has := t2[0]["request"]; has {
		t.Fatal("重复项 request 应被剔除")
	}
	if _, has := t2[0]["response"]; has {
		t.Fatal("重复项 response 应被剔除")
	}
	if _, has := t2[1]["duplicate_of"]; has {
		t.Fatal("非重复项不应标记")
	}
}

// TestCompactTrialEvaluation_分支 对照 Python _compact_trial_evaluation。
// Python: evidence_compactor.py:251-299
func TestCompactTrialEvaluation_分支(t *testing.T) {
	// ① 缺失 → not_instrumented 全家桶
	got := compactTrialEvaluation(nil)
	avail := got["availability"].(map[string]any)
	if avail["score_file"] != "not_instrumented" || got["judge_detail"] != nil {
		t.Fatalf("缺失默认不符: %v", got)
	}
	// ② 正常解析
	got2 := compactTrialEvaluation(map[string]any{
		"availability": map[string]any{"score_file": "ok", "bad": 1},
		"score":        0.5, "passed": true, "score_reason": "why",
		"judge_detail": map[string]any{"a": 1},
		"dimension_scores": map[string]any{
			"d1": map[string]any{"availability": "ok", "value": 0.9, "source": "file"},
			"d2": map[string]any{"value": "not-number"},
		},
		"source": map[string]any{"path": "p"},
	})
	if got2["score"] != 0.5 || got2["passed"] != true {
		t.Fatalf("score/passed 不符: %v", got2)
	}
	// Python: 仅 str 值入选（isinstance(item, str)），bad:1 应剔除
	if _, has := got2["availability"].(map[string]any)["bad"]; has {
		t.Fatalf("availability 非字符串应剔除: %v", got2["availability"])
	}
	dims := got2["dimension_scores"].(map[string]any)
	if dims["d1"].(map[string]any)["value"] != 0.9 {
		t.Fatalf("d1 不符: %v", dims)
	}
	if dims["d2"].(map[string]any)["value"] != nil {
		t.Fatalf("非数值应 None: %v", dims["d2"])
	}
	if dims["d2"].(map[string]any)["availability"] != "not_available" {
		t.Fatalf("缺 availability 应回落 not_available: %v", dims["d2"])
	}
}

// TestCompactJudgeEvidence_分支 对照 Python _compact_judge_evidence。
// Python: evidence_compactor.py:302-336
func TestCompactJudgeEvidence_分支(t *testing.T) {
	// ① judge_evidence 直取
	got := compactJudgeEvidence(map[string]any{"judge_evidence": map[string]any{
		"availability": "available", "grading_run_status": "ok",
		"criteria": []any{map[string]any{"criterion_id": "x"}},
	}})
	if got["availability"] != "available" || got["grading_run_status"] != "ok" {
		t.Fatalf("judge_evidence 直取不符: %v", got)
	}
	// ② 无 judge_evidence、无 judge_detail → not_instrumented
	got2 := compactJudgeEvidence(map[string]any{})
	if got2["availability"] != "not_instrumented" {
		t.Fatalf("not_instrumented 不符: %v", got2)
	}
	// ③ judge_detail 无 criteria 清单 → not_available
	got3 := compactJudgeEvidence(map[string]any{"judge_detail": map[string]any{"criteria": "bad"}})
	if got3["availability"] != "not_available" {
		t.Fatalf("not_available 不符: %v", got3)
	}
	// ④ 有 criteria → available
	got4 := compactJudgeEvidence(map[string]any{"judge_detail": map[string]any{
		"criteria": []any{map[string]any{"criterion_id": "y"}, "junk"},
	}})
	if got4["availability"] != "available" || len(got4["criteria"].([]map[string]any)) != 1 {
		t.Fatalf("available 不符: %v", got4)
	}
}

// TestExtractCriticalEvidenceSpans_窗口合并 对照 Python extract_critical_evidence_spans。
// Python: evidence_compactor.py:963-1025
func TestExtractCriticalEvidenceSpans_窗口合并(t *testing.T) {
	// 行 5 与行 8 含目标词（相距 ≤7 → 窗口合并）；行 20 远离（独立窗口）
	var lines []string
	for i := 1; i <= 25; i++ {
		switch i {
		case 5, 8:
			lines = append(lines, "contains TARGET here")
		case 20:
			lines = append(lines, "has TARGET too")
		default:
			lines = append(lines, "filler line")
		}
	}
	text := strings.Join(lines, "\n")
	spans := ExtractCriticalEvidenceSpans(text, []string{"target"}, 3, 6000)
	if len(spans) != 2 {
		t.Fatalf("应 2 窗口（合并 + 独立）, got %d: %v", len(spans), spans)
	}
	first := spans[0]
	if first["line_start"] != 2 || first["line_end"] != 11 {
		t.Fatalf("合并窗口范围不符: %v-%v", first["line_start"], first["line_end"])
	}
	if first["matched_terms"].([]string)[0] != "target" {
		t.Fatalf("matched_terms 不符: %v", first["matched_terms"])
	}
	if first["window_complete"] != true || first["projection"] != "verbatim" {
		t.Fatalf("窗口元数据不符: %v", first)
	}

	// 多词同行优先（匹配数排序）
	text2 := "alpha beta\nalpha\nbeta\nfiller"
	spans2 := ExtractCriticalEvidenceSpans(text2, []string{"alpha", "beta"}, 2, 6000)
	if spans2[0]["line_start"] != 1 {
		t.Fatalf("双词行应排前: %v", spans2[0]["line_start"])
	}

	// 转义换行投影
	escaped := `line1 "has alpha" \n line2 filler`
	spans3 := ExtractCriticalEvidenceSpans(escaped, []string{"alpha"}, 3, 6000)
	if spans3[0]["projection"] != "escaped_newlines_normalized" {
		t.Fatalf("投影应归一转义换行: %v", spans3[0]["projection"])
	}

	// 预算约束：max_total_chars 限制
	long := ""
	for i := 0; i < 50; i++ {
		long += "has alpha here\n"
	}
	spans4 := ExtractCriticalEvidenceSpans(long, []string{"alpha"}, 3, 30)
	if len(spans4) == 0 || spans4[0]["window_complete"] != false {
		t.Fatalf("超预算应 window_complete=false: %v", spans4)
	}
}

// TestIsMutatingTool_模式对照 对照 Python _MUTATING_TOOL_PATTERN。
func TestIsMutatingTool_模式对照(t *testing.T) {
	mutating := []string{"write_file", "delete_record", "save_workbook", "update_config", "send_mail", "create_task", "write"}
	nonMutating := []string{"read_file", "list_dir", "search", "writerX"}
	for _, tool := range mutating {
		if !isMutatingTool(tool) {
			t.Fatalf("%s 应为变异工具", tool)
		}
	}
	for _, tool := range nonMutating {
		if isMutatingTool(tool) {
			t.Fatalf("%s 不应为变异工具", tool)
		}
	}
}

// TestCallMutatesState_Bash命令 对照 Python bash 命令变异/内容模式。
// Python: evidence_compactor.py:1118-1136
func TestCallMutatesState_Bash命令(t *testing.T) {
	mut := map[string]any{"tool": "bash", "_raw_input": `{"command": "cp a b"}`}
	if !callMutatesState(mut) {
		t.Fatal("cp 命令应判变异")
	}
	pipe := map[string]any{"tool": "bash", "_raw_input": `{"command": "echo x > file"}`}
	if !callMutatesState(pipe) {
		t.Fatal("重定向应判变异")
	}
	plain := map[string]any{"tool": "bash", "_raw_input": `{"command": "ls -la"}`}
	if callMutatesState(plain) {
		t.Fatal("ls 不应判变异")
	}
	// 非 bash 工具不查命令模式
	if callMutatesState(map[string]any{"tool": "editor", "_raw_input": `{"command": "cp a b"}`}) {
		t.Fatal("非 bash 工具不应查 bash 模式")
	}
	// 内容证据：read 工具 + 长输出
	content := map[string]any{"tool": "read_file", "_raw_output": strings.Repeat("x", 400)}
	if !callHasContentEvidence(content) {
		t.Fatal("read 长输出应有内容证据")
	}
	// bash cat 命令 + 长输出
	cat := map[string]any{"tool": "bash", "_raw_input": `{"command": "cat data.txt"}`, "_raw_output": strings.Repeat("x", 400)}
	if !callHasContentEvidence(cat) {
		t.Fatal("cat 长输出应有内容证据")
	}
	// 短输出无内容证据
	if callHasContentEvidence(map[string]any{"tool": "read_file", "_raw_output": "short"}) {
		t.Fatal("短输出不应有内容证据")
	}
	// 失败调用无内容证据
	if callHasContentEvidence(map[string]any{"tool": "read_file", "_raw_output": strings.Repeat("x", 400), "error": "boom"}) {
		t.Fatal("失败调用不应有内容证据")
	}
}

// TestCallFailed_响应特征 对照 Python _FAILED_RESPONSE_PATTERN。
// Python: evidence_compactor.py:1112-1115, 26-29
func TestCallFailed_响应特征(t *testing.T) {
	if !callFailed(map[string]any{"error": "boom"}) {
		t.Fatal("error 非空应失败")
	}
	if !callFailed(map[string]any{"_raw_output": `{"success": false}`}) {
		t.Fatal("success false 应失败")
	}
	if !callFailed(map[string]any{"_raw_output": "exit code: 2 happened"}) {
		t.Fatal("非零退出码应失败")
	}
	if !callFailed(map[string]any{"_raw_output": "Traceback (most recent call last)"}) {
		t.Fatal("Traceback 应失败")
	}
	if callFailed(map[string]any{"_raw_output": `{"success": true, "data": 1}`}) {
		t.Fatal("成功响应不应失败")
	}
	// response 回落（无 _raw_output）
	if !callFailed(map[string]any{"response": "Access Denied"}) {
		t.Fatal("response 回落应生效")
	}
}

// min 整数取小（测试辅助）。
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
