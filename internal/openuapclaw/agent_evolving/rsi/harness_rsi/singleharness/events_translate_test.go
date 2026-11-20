package singleharness

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSourceReuseStagePayload 来源复用阶段载荷。
func TestSourceReuseStagePayload(t *testing.T) {
	score := 0.85
	payload := SourceReuseStagePayload(2, 10, &score, "eval.yaml", map[string]any{
		"reused_case_ids":    []any{"a", "b"},
		"evaluated_case_ids": []any{"c"},
	})
	if payload["id"] != "source.reuse" || payload["status"] != "done" {
		t.Fatalf("固定字段错误: %v", payload)
	}
	if payload["name"] != "Batch 2: reused 2/10 case results" {
		t.Fatalf("name 错误: %v", payload["name"])
	}
	if payload["reused_case_count"] != 2 || payload["evaluated_case_count"] != 1 {
		t.Fatalf("计数错误: %v", payload)
	}
	if payload["eval_ref_path"] != "eval.yaml" || payload["score"] != 0.85 {
		t.Fatalf("透传字段错误: %v", payload)
	}
}

// TestCaseStagePayload 逐用例阶段载荷各状态。
func TestCaseStagePayload(t *testing.T) {
	score := 0.9
	payload := CaseStagePayload(1, 4, "passed", "c1", &score, nil)
	if payload["name"] != "Case 1/4 passed · score 0.90" || payload["id"] != "evaluate.case.1" {
		t.Fatalf("passed 载荷错误: %v", payload)
	}
	if payload := CaseStagePayload(1, 4, "failed", "", nil, nil); payload["name"] != "Case 1/4 failed" {
		t.Fatalf("failed 载荷错误: %v", payload)
	}
	if payload := CaseStagePayload(1, 4, "error", "", nil, nil); payload["name"] != "Case 1/4 error" {
		t.Fatalf("error 载荷错误: %v", payload)
	}
	if payload := CaseStagePayload(1, 4, "skipped", "", nil, nil); payload["name"] != "Case 1/4 skipped" {
		t.Fatalf("skipped 载荷错误: %v", payload)
	}
	if payload := CaseStagePayload(1, 4, "", "", nil, nil); payload["status"] != "running" || payload["name"] != "Case 1/4 evaluating" {
		t.Fatalf("空状态应归一为 running: %v", payload)
	}
	completed := 4
	payload = CaseStagePayload(2, 4, "running", "c2", nil, &completed)
	if payload["id"] != "evaluate.parallel" || payload["status"] != "done" || payload["name"] != "Cases 4/4 completed" {
		t.Fatalf("并行完成载荷错误: %v", payload)
	}
	completed = 2
	payload = CaseStagePayload(2, 4, "running", "c2", nil, &completed)
	if payload["status"] != "running" || payload["name"] != "Cases 2/4 completed" {
		t.Fatalf("并行进行载荷错误: %v", payload)
	}
}

// TestGenerateAndAnalysisStagePayloads 候选生成与失败分析阶段载荷。
func TestGenerateAndAnalysisStagePayloads(t *testing.T) {
	if payload := GenerateStagePayload(1, 3, "done", ""); payload["name"] != "Candidate 1/3 generated" {
		t.Fatalf("done 载荷错误: %v", payload)
	}
	if payload := GenerateStagePayload(1, 3, "error", "boom"); payload["name"] != "Candidate 1/3 generation failed" || payload["error"] != "boom" {
		t.Fatalf("error 载荷错误: %v", payload)
	}
	if payload := GenerateStagePayload(1, 3, "", ""); payload["status"] != "running" || payload["name"] != "Generating candidate 1/3" {
		t.Fatalf("running 载荷错误: %v", payload)
	}
	if payload := AnalysisStagePayload("done", nil, ""); payload["name"] != "Failure analysis completed" {
		t.Fatalf("done 载荷错误: %v", payload)
	}
	count := 5
	if payload := AnalysisStagePayload("error", &count, "bad"); payload["name"] != "Failure analysis failed" || payload["failed_case_count"] != 5 || payload["error"] != "bad" {
		t.Fatalf("error 载荷错误: %v", payload)
	}
	if payload := AnalysisStagePayload("", nil, ""); payload["status"] != "running" || payload["name"] != "Analyzing failed cases" {
		t.Fatalf("running 载荷错误: %v", payload)
	}
}

// TestRootNodeEvent 根节点事件。
func TestRootNodeEvent(t *testing.T) {
	baseline := 0.42
	state := map[string]any{"baseline_score": baseline, "source_harness_refs_path": "refs.yaml"}
	event := RootNodeEvent(state)
	node := event.Node()
	if node.NodeID() != "h0" || node.Type() != "ROOT" || !node.Adopted() || node.Iteration() != 0 {
		t.Fatalf("根节点字段错误: %v", node)
	}
	if node.Score() == nil || *node.Score() != 0.42 {
		t.Fatalf("分数错误: %v", node.Score())
	}
	if node.Summary() == nil || *node.Summary() != "Initial Harness" {
		t.Fatalf("摘要错误: %v", node.Summary())
	}
	if node.ParentID() != nil {
		t.Fatal("根节点不应有父")
	}
	artifacts := event.Artifacts()
	if len(artifacts) != 1 || artifacts[0]["role"] != "HARNESS_REFS" {
		t.Fatalf("artifacts 错误: %v", artifacts)
	}
}

// TestEpochNodeEvent epoch 节点事件各分支。
func TestEpochNodeEvent(t *testing.T) {
	state := map[string]any{
		"source_harness_refs_path": "refs.yaml",
		"epoch_checkpoints":        []any{},
		"candidate_gates":          []any{},
		"completed_batches":        map[string]any{},
	}
	checkpoint := map[string]any{
		"epoch":                      1,
		"status":                     "accepted",
		"promotion_applied":          true,
		"selected_harness_refs_path": "sel",
		"harness_refs_path":          "sel",
		"score":                      0.7,
		"before_harness_refs_path":   "",
	}
	event := EpochNodeEvent(state, checkpoint)
	node := event.Node()
	if node.NodeID() != "epoch-001" || node.Type() != "ADOPTED" || !node.Adopted() {
		t.Fatalf("采纳节点错误: %v", node)
	}
	if node.Score() == nil || *node.Score() != 0.7 {
		t.Fatal("采纳节点应有分数")
	}
	// 运行中
	checkpointRunning := map[string]any{"epoch": 2, "status": "running"}
	event = EpochNodeEvent(state, checkpointRunning)
	if event.Node().Type() != "RUNNING" || event.Node().Score() != nil || len(event.Artifacts()) != 0 {
		t.Fatalf("运行中节点错误: %v", event.Node())
	}
	// 被拒绝 → 原因链
	checkpointRejected := map[string]any{"epoch": 3, "status": "rejected", "full_evaluation_skipped_reason": "skipped-x"}
	event = EpochNodeEvent(state, checkpointRejected)
	if event.Node().Type() != "REJECTED" {
		t.Fatalf("拒绝节点错误: %v", event.Node())
	}
	if event.Node().Reason() == nil || *event.Node().Reason() != "skipped-x" {
		t.Fatalf("原因应取 skipped reason: %v", event.Node().Reason())
	}
	// UNCHANGED 默认原因
	checkpointNoop := map[string]any{"epoch": 4, "status": "noop"}
	event = EpochNodeEvent(state, checkpointNoop)
	if event.Node().Type() != "UNCHANGED" || event.Node().Reason() == nil || *event.Node().Reason() != "No Harness change passed the acceptance checks" {
		t.Fatalf("默认原因错误: %v", event.Node().Reason())
	}
	// 采纳 + 候选门变更
	state["candidate_gates"] = []any{map[string]any{
		"epoch": 1, "status": "accepted",
		"capabilities": []any{map[string]any{
			"action_group": "prompt", "operation": "modify", "function": "fn",
			"target_path": "p.yaml", "expected_effect": "improve coverage",
		}},
	}}
	event = EpochNodeEvent(state, checkpoint)
	changes := event.Node().Changes()
	if len(changes) != 1 || changes[0].Group() != "PROMPT" || changes[0].Operation() != "MODIFY" {
		t.Fatalf("变更解析错误: %v", changes)
	}
	if event.Node().Summary() == nil || *event.Node().Summary() != "improve coverage" {
		t.Fatalf("摘要应取变更描述: %v", event.Node().Summary())
	}
	// source_evidence 登记与连续模式 extra
	state["completed_batches"] = map[string]any{
		"b1": map[string]any{"epoch": 1, "batch_index": 0, "source_eval_ref_path": "e.yaml", "source_evidence": map[string]any{"reused_case_ids": []any{}}},
		"b2": map[string]any{"epoch": 9, "batch_index": 1, "source_eval_ref_path": "e9.yaml", "source_evidence": map[string]any{}},
	}
	state["promotion_policy"] = map[string]any{"policy_version": "continuous_two_stage_v2"}
	state["best_reference_score"] = 0.8
	event = EpochNodeEvent(state, checkpoint)
	extra := event.Node().Extra()
	if extra["score_mode"] != "continuous" || extra["best_reference_score"] != 0.8 {
		t.Fatalf("连续模式 extra 错误: %v", extra)
	}
	se, _ := extra["source_evidence"].([]any)
	if len(se) != 1 {
		t.Fatalf("source_evidence 应只含本 epoch 批次: %v", se)
	}
}

// TestEpochParentID 父节点解析。
func TestEpochParentID(t *testing.T) {
	state := map[string]any{
		"source_harness_refs_path": "origin",
		"epoch_checkpoints": []any{
			map[string]any{"epoch": 1, "promotion_applied": true, "selected_harness_refs_path": "v1"},
			map[string]any{"epoch": 2, "promotion_applied": true, "selected_harness_refs_path": "v2"},
		},
	}
	// epoch3 的 before=v2 → 父应为 epoch-002
	got := epochParentID(state, map[string]any{"epoch": 3, "before_harness_refs_path": "v2"})
	if got != "epoch-002" {
		t.Fatalf("父应為 epoch-002: %s", got)
	}
	// before 与源头相同 → h0
	if got := epochParentID(state, map[string]any{"epoch": 3, "before_harness_refs_path": "origin"}); got != "h0" {
		t.Fatalf("before=源头应 h0: %s", got)
	}
	// 无 before → h0
	if got := epochParentID(state, map[string]any{"epoch": 3}); got != "h0" {
		t.Fatalf("无 before 应 h0: %s", got)
	}
}

// TestActiveEpochNodeEvent 活跃 epoch 快照。
func TestActiveEpochNodeEvent(t *testing.T) {
	state := map[string]any{
		"source_harness_refs_path":              "origin",
		"epoch_checkpoints":                     []any{},
		"candidate_gates":                       []any{},
		"completed_batches":                     map[string]any{},
		"active_epoch":                          5,
		"active_epoch_before_harness_refs_path": "origin",
	}
	event := ActiveEpochNodeEvent(state)
	if event == nil || event.Node().NodeID() != "epoch-005" || event.Node().Type() != "RUNNING" {
		t.Fatalf("活跃节点错误: %v", event)
	}
	// 已有 checkpoint → nil
	state["epoch_checkpoints"] = []any{map[string]any{"epoch": 5}}
	if event := ActiveEpochNodeEvent(state); event != nil {
		t.Fatal("已有 checkpoint 应返回 nil")
	}
	// 无活跃 epoch → nil
	state["epoch_checkpoints"] = []any{}
	state["active_epoch"] = 0
	if event := ActiveEpochNodeEvent(state); event != nil {
		t.Fatal("无活跃 epoch 应返回 nil")
	}
}

// TestNodeEventAndParentID 通用候选节点与父链。
func TestNodeEventAndParentID(t *testing.T) {
	candidate := map[string]any{
		"status":                      "accepted",
		"accepted":                    true,
		"candidate_id":                "",
		"candidate_score":             0.6,
		"reason":                      "gate_review_passed",
		"candidate_harness_refs_path": "cand1",
		"failure_class":               "  ",
		"causal_failure_class":        "timeout",
		"capabilities":                []any{},
	}
	event := NodeEvent(candidate, 3, nil)
	node := event.Node()
	if node.NodeID() == "" || node.Type() != "ADOPTED" || !node.Adopted() {
		t.Fatalf("采纳节点错误: %v", node)
	}
	if node.Reason() != nil {
		t.Fatal("采纳节点 reason 应为 nil")
	}
	if node.FailureClass() == nil || *node.FailureClass() != "timeout" {
		t.Fatalf("失败类别应回退 causal: %v", node.FailureClass())
	}
	// PROVISIONAL / REJECTED
	prov := map[string]any{"status": "provisional", "candidate_id": "explicit-id"}
	if node := NodeEvent(prov, 1, nil).Node(); node.Type() != "PROVISIONAL" || node.NodeID() != "explicit-id" {
		t.Fatalf("临时节点错误: %v", node)
	}
	rej := map[string]any{"status": "rejected", "reason": "batch_attempt_failed"}
	node = NodeEvent(rej, 2, nil).Node()
	if node.Type() != "REJECTED" {
		t.Fatalf("拒绝节点错误: %v", node)
	}
	if node.Reason() == nil || *node.Reason() != "batch proposals failed" {
		t.Fatalf("原因应脱敏: %v", node.Reason())
	}
	// NodeID 稳定哈希回退
	id1 := NodeID(map[string]any{"member_optimization_ref_path": "m", "candidate_harness_refs_path": "c"}, 7)
	id2 := NodeID(map[string]any{"member_optimization_ref_path": "m", "candidate_harness_refs_path": "c"}, 7)
	if id1 != id2 || len(id1) != len("candidate-")+16 {
		t.Fatalf("稳定哈希错误: %s", id1)
	}
	// ParentNodeID：倒序找最近匹配
	persisted := []map[string]any{
		{"candidate_harness_refs_path": "cand1"},
		{"candidate_harness_refs_path": "cand2"},
	}
	parent := ParentNodeID(map[string]any{"before_harness_refs_path": "cand1"}, persisted)
	if parent == nil || *parent != idOf(persisted[0], 1) {
		t.Fatalf("父解析错误: %v", parent)
	}
	if parent := ParentNodeID(map[string]any{"before_harness_refs_path": ""}, persisted); parent != nil {
		t.Fatal("无 before 应返回 nil")
	}
	if parent := ParentNodeID(map[string]any{"before_harness_refs_path": "nope"}, persisted); parent != nil {
		t.Fatal("无匹配应返回 nil")
	}
}

// TestPublicText 控制面词汇脱敏。
func TestPublicText(t *testing.T) {
	cases := map[string]string{
		"gate_review_passed":     "reviews review passed",
		"epoch_checkpoint_done":  "cycles reviews done",
		"batch_attempt_failed":   "batch proposals failed",
		"all_attempts_exhausted": "all proposals exhausted",
		"plain_text":             "plain text",
		"The GATES were checked": "The reviews were checked",
	}
	for input, want := range cases {
		if got := publicText(input); got != want {
			t.Fatalf("publicText(%q)=%q, want %q", input, got, want)
		}
	}
}

// TestProgressEvent 进度事件。
func TestProgressEvent(t *testing.T) {
	state := map[string]any{
		"epoch_checkpoints": []any{map[string]any{"epoch": 1}, map[string]any{"epoch": 2}},
		"best_score":        0.8,
		"baseline_score":    0.5,
		"usage":             map[string]any{"tokens": map[string]any{}, "call_count": 3},
	}
	progress := ProgressEvent(state, 10)
	if progress.Iteration() != 2 || progress.TotalIterations() != 10 {
		t.Fatalf("进度计数错误: %d/%d", progress.Iteration(), progress.TotalIterations())
	}
	if progress.Score() == nil || *progress.Score() != 0.8 {
		t.Fatal("分数错误")
	}
	if progress.Baseline() == nil || *progress.Baseline() != 0.5 {
		t.Fatal("基线错误")
	}
	if progress.Usage() == nil {
		t.Fatal("usage 快照缺失")
	}
	// 负迭代钳制
	if progress := ProgressEvent(map[string]any{}, -3); progress.TotalIterations() != 0 {
		t.Fatal("负值应钳制为 0")
	}
}

// TestHarnessArtifacts refs 与插件目录描述。
func TestHarnessArtifacts(t *testing.T) {
	if got := HarnessArtifacts(""); got != nil {
		t.Fatal("空 refs 应返回 nil")
	}
	dir := t.TempDir()
	pkg := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "harness_config.yaml"), []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(dir, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	refs := filepath.Join(dir, "refs.yaml")
	if err := os.WriteFile(refs, []byte("harness_refs:\n  solver: "+pkg+"\n  other: "+bare+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifacts := HarnessArtifacts(refs)
	if len(artifacts) != 2 {
		t.Fatalf("应含 refs+一个 PRIMARY: %v", artifacts)
	}
	if artifacts[0]["role"] != "HARNESS_REFS" || artifacts[1]["role"] != "PRIMARY" || artifacts[1]["format"] != "dir" {
		t.Fatalf("artifacts 结构错误: %v", artifacts)
	}
	// 读取失败 → 仅 refs
	if artifacts := HarnessArtifacts(filepath.Join(dir, "missing.yaml")); len(artifacts) != 1 {
		t.Fatalf("读取失败应仅 refs: %v", artifacts)
	}
	// 非 mapping YAML
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("- 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if artifacts := HarnessArtifacts(bad); len(artifacts) != 1 {
		t.Fatalf("非 mapping 应仅 refs: %v", artifacts)
	}
	// 顶层即 refs（无 harness_refs 键）
	direct := filepath.Join(dir, "direct.yaml")
	if err := os.WriteFile(direct, []byte("solver: "+pkg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if artifacts := HarnessArtifacts(direct); len(artifacts) != 2 {
		t.Fatalf("顶层 refs 应生效: %v", artifacts)
	}
}

// idOf 取持久化候选的 NodeID。
func idOf(candidate map[string]any, iteration int) string {
	return NodeID(candidate, iteration)
}
