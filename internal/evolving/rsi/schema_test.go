package rsi

import (
	"encoding/json"
	"testing"
)

// TestArtifactType_常量集合 枚举取值对齐 Python Literal["program","paper"]
func TestArtifactType_常量集合(t *testing.T) {
	if ArtifactTypeProgram != "program" || ArtifactTypePaper != "paper" {
		t.Fatal("枚举取值与 Python 不符")
	}
}

// TestRsiScenario_常量集合 场景枚举对齐 Python Literal["harness","artifact"]
func TestRsiScenario_常量集合(t *testing.T) {
	if RsiScenarioHarness != "harness" || RsiScenarioArtifact != "artifact" {
		t.Fatal("场景枚举与 Python 不符")
	}
}

// TestRsiStatus_七态齐全 对齐 Python RsiStatus Literal 七个成员
func TestRsiStatus_七态齐全(t *testing.T) {
	want := []RsiStatus{"created", "queued", "running", "completed", "failed", "paused", "terminated"}
	if len(validRsiStatuses) != len(want) {
		t.Fatalf("成员数 %d != 7", len(validRsiStatuses))
	}
	for _, s := range want {
		if !validRsiStatuses[s] {
			t.Fatalf("缺少状态 %q", s)
		}
	}
}

// TestModelCallStatus_三态 对齐 Python RsiModelCall.Status Literal
func TestModelCallStatus_三态(t *testing.T) {
	if ModelCallStatusSucceeded != "succeeded" || ModelCallStatusFailed != "failed" || ModelCallStatusIncomplete != "incomplete" {
		t.Fatal("调用状态枚举与 Python 不符")
	}
}

// TestNewRsiUsageTokens_访问器与序列化 私有字段经 JSON 往返后访问器取值一致
func TestNewRsiUsageTokens_访问器与序列化(t *testing.T) {
	tok := NewRsiUsageTokens(intPtr(10), intPtr(20), intPtr(5))
	if tok.Input() == nil || *tok.Input() != 10 || tok.Output() == nil || *tok.Output() != 20 || tok.CacheHit() == nil || *tok.CacheHit() != 5 {
		t.Fatal("访问器取值不符")
	}
	none := NewRsiUsageTokens(nil, nil, nil)
	if none.Input() != nil || none.Output() != nil || none.CacheHit() != nil {
		t.Fatal("未上报应返回 nil")
	}
	// JSON 往返：字段名与 Python asdict 一致（input/output/cache_hit）
	data, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if _, ok := back["input"]; !ok {
		t.Fatalf("序列化键应与 Python 一致: %s", data)
	}
}

// TestNewRsiUsage_访问器 累计用量三字段
func TestNewRsiUsage_访问器(t *testing.T) {
	tok := NewRsiUsageTokens(intPtr(1), intPtr(2), nil)
	u := NewRsiUsage(tok, floatPtr(1.5), 7)
	if u.Tokens() != tok || u.CallCount() != 7 {
		t.Fatal("usage 访问器不符")
	}
	if u.CostEstimate() == nil || *u.CostEstimate() != 1.5 {
		t.Fatal("cost_estimate 不符")
	}
}

// TestNewRsiTaskCreateRequest_全字段 任务创建请求 10 字段
func TestNewRsiTaskCreateRequest_全字段(t *testing.T) {
	req := NewRsiTaskCreateRequest(RsiScenarioArtifact, artifactTypePtr(ArtifactTypePaper), "name",
		strPtr("/p"), strPtr("inst"), strPtr("/d"), intPtr(3),
		map[string]string{"main": "m1"}, 5, strPtr("http://proxy"))
	if req.Scenario() != RsiScenarioArtifact || req.ArtifactType() == nil || *req.ArtifactType() != ArtifactTypePaper {
		t.Fatal("请求字段不符")
	}
	if req.WebProxy() == nil || *req.WebProxy() != "http://proxy" {
		t.Fatal("web_proxy 不符")
	}
}

// TestNewRsiTreeNode_全字段访问器 树节点 12 字段
func TestNewRsiTreeNode_全字段访问器(t *testing.T) {
	changes := []RsiChange{*NewRsiChange("g", "op", strPtr("fn"), strPtr("t"), "sum")}
	node := NewRsiTreeNode("n1", 2, strPtr("n0"), "root", false, floatPtr(0.9),
		strPtr("sm"), strPtr("a1"), strPtr("r"), strPtr("fc"), changes,
		map[string]any{"k": "v"})
	if node.NodeID() != "n1" || node.Iteration() != 2 || node.ParentID() == nil || *node.ParentID() != "n0" {
		t.Fatal("节点标识字段不符")
	}
	if node.Adopted() || node.Score() == nil || *node.Score() != 0.9 {
		t.Fatal("采纳/分数不符")
	}
	if len(node.Changes()) != 1 || node.Changes()[0].Function() == nil {
		t.Fatal("changes 不符")
	}
	if node.Extra()["k"] != "v" {
		t.Fatal("extra 不符")
	}
}

// TestNewEngineState_全字段访问器 引擎状态 12 字段
func TestNewEngineState_全字段访问器(t *testing.T) {
	usage := NewRsiUsage(NewRsiUsageTokens(nil, nil, nil), nil, 0)
	st := NewEngineState("t1", RsiStatusRunning, 3, 10, strPtr("n5"), floatPtr(0.8), floatPtr(0.5), usage, "ts", strPtr("code"), strPtr("msg"))
	if st.TaskID() != "t1" || st.Status() != RsiStatusRunning || st.Iteration() != 3 || st.TotalIterations() != 10 {
		t.Fatal("状态字段不符")
	}
	if st.BestNodeID() == nil || *st.BestNodeID() != "n5" || st.UpdatedAt() != "ts" {
		t.Fatal("状态可选字段不符")
	}
}

// TestNewEngineReport_兼容字段 paper 兼容字段 best_score/baseline/score_details
func TestNewEngineReport_兼容字段(t *testing.T) {
	r := NewEngineReport("t1", RsiStatusCompleted, strPtr("n1"), nil, nil, strPtr("sum"),
		floatPtr(0.9), floatPtr(0.5), map[string]any{"d": 1})
	if r.BestScore() == nil || *r.BestScore() != 0.9 || r.Baseline() == nil || *r.Baseline() != 0.5 {
		t.Fatal("paper 兼容字段不符")
	}
	if r.Summary() == nil || *r.Summary() != "sum" {
		t.Fatal("summary 不符")
	}
}

// TestNewArtifactRef_全字段下载引用 artifact 引用 7 字段
func TestNewArtifactRef_全字段下载引用(t *testing.T) {
	ref := NewArtifactRef("a1", strPtr("n1"), "name", "kind", "/path", strPtr("sha"), strPtr("http://dl"))
	if ref.ArtifactID() != "a1" || ref.NodeID() == nil || ref.Sha256() == nil || ref.DownloadURL() == nil {
		t.Fatal("引用字段不符")
	}
}

// TestNewTreeResponse_访问器 树响应三字段
func TestNewTreeResponse_访问器(t *testing.T) {
	tr := NewTreeResponse([]RsiTreeNode{}, 3, 5)
	if tr.Depth() != 3 || tr.Iteration() != 5 || len(tr.Nodes()) != 0 {
		t.Fatal("树响应不符")
	}
}

// TestNewEngineResult_访问器 执行控制结果五字段
func TestNewEngineResult_访问器(t *testing.T) {
	r := NewEngineResult("t1", RsiStatusFailed, strPtr("n1"), strPtr("ec"), strPtr("em"))
	if r.ErrorCode() == nil || *r.ErrorCode() != "ec" || r.ErrorMessage() == nil {
		t.Fatal("结果字段不符")
	}
}

// TestNewArtifactValidationResult_访问器 校验结果两字段
func TestNewArtifactValidationResult_访问器(t *testing.T) {
	v := NewArtifactValidationResult(false, []map[string]string{{"f": "e"}})
	if v.Valid() || len(v.Errors()) != 1 {
		t.Fatal("校验结果不符")
	}
}

// TestNewRsiModelCall_默认状态 未显式给状态时默认 succeeded（Python 默认值）
func TestNewRsiModelCall_默认状态(t *testing.T) {
	mc := NewRsiModelCall("m1", 1, NewRsiUsageTokens(nil, nil, nil), "", nil)
	if mc.Status() != ModelCallStatusSucceeded {
		t.Fatalf("默认状态应为 succeeded，得到 %q", mc.Status())
	}
}

// TestNewRsiTaskEnvelope_访问器 信封四字段
func TestNewRsiTaskEnvelope_访问器(t *testing.T) {
	req := NewRsiTaskCreateRequest(RsiScenarioHarness, nil, "n", nil, nil, nil, nil, nil, 3, nil)
	env := NewRsiTaskEnvelope("t1", "/run/dir", nil, req)
	if env.TaskID() != "t1" || env.RunDir() != "/run/dir" || env.ArtifactType() != nil || env.Config() != req {
		t.Fatal("信封字段不符")
	}
}

func floatPtr(v float64) *float64                  { return &v }
func strPtr(v string) *string                      { return &v }
func artifactTypePtr(v ArtifactType) *ArtifactType { return &v }

// TestSchemaAccessors_全覆盖 补齐各契约剩余访问器分支（覆盖率门禁）
func TestSchemaAccessors_全覆盖(t *testing.T) {
	// RsiTaskCreateRequest 剩余字段
	req := NewRsiTaskCreateRequest(RsiScenarioHarness, nil, "n", strPtr("a"), strPtr("o"), strPtr("d"), intPtr(2), map[string]string{"k": "v"}, 9, nil)
	if req.Name() != "n" || req.ArtifactPath() == nil || req.OptimizationInstruction() == nil ||
		req.DatasetFile() == nil || req.SearchWidth() == nil || *req.SearchWidth() != 2 ||
		req.ModelRefs()["k"] != "v" || req.MaxIterations() != 9 {
		t.Fatal("请求剩余访问器不符")
	}
	// RsiModelCall 剩余访问器 + MarshalJSON
	mc := NewRsiModelCall("m", 1, NewRsiUsageTokens(intPtr(1), intPtr(2), intPtr(3)), ModelCallStatusFailed, intPtr(50))
	if mc.Model() != "m" || mc.CallCount() != 1 || mc.DurationMs() == nil || *mc.DurationMs() != 50 {
		t.Fatal("model call 剩余访问器不符")
	}
	if _, err := mc.MarshalJSON(); err != nil {
		t.Fatal(err)
	}
	// RsiChange 剩余访问器
	ch := NewRsiChange("g", "op", nil, strPtr("t"), "s")
	if ch.Group() != "g" || ch.Operation() != "op" || ch.Function() != nil || ch.Target() == nil || ch.Summary() != "s" {
		t.Fatal("change 剩余访问器不符")
	}
	// RsiTreeNode 剩余访问器
	node := NewRsiTreeNode("n", 1, nil, "leaf", true, nil, strPtr("s"), strPtr("a"), strPtr("r"), strPtr("f"), nil, nil)
	if node.Type() != "leaf" || !node.Adopted() || node.Summary() == nil || node.SnapshotArtifactID() == nil ||
		node.Reason() == nil || node.FailureClass() == nil {
		t.Fatal("节点剩余访问器不符")
	}
	// EngineResult 剩余访问器
	r := NewEngineResult("t", RsiStatusCompleted, nil, nil, nil)
	if r.TaskID() != "t" || r.Status() != RsiStatusCompleted || r.FinalNodeID() != nil || r.ErrorCode() != nil || r.ErrorMessage() != nil {
		t.Fatal("result 剩余访问器不符")
	}
	// EngineState 剩余访问器
	st := NewEngineState("t", RsiStatusPaused, 0, 1, nil, floatPtr(0.1), floatPtr(0.2), nil, "u", nil, nil)
	if st.Score() == nil || *st.Score() != 0.1 || st.Baseline() == nil || *st.Baseline() != 0.2 || st.Usage() != nil {
		t.Fatal("state 剩余访问器不符")
	}
	// ArtifactRef 剩余访问器
	ref := NewArtifactRef("a", nil, "nm", "kd", "p", nil, nil)
	if ref.Name() != "nm" || ref.Kind() != "kd" || ref.Path() != "p" || ref.Sha256() != nil || ref.DownloadURL() != nil {
		t.Fatal("ref 剩余访问器不符")
	}
	// TreeResponse/ArtifactValidation/Usage/EventUsage.Ts 已测部分之外的分支
	tr := NewTreeResponse(nil, 0, 0)
	if tr.Nodes() != nil && len(tr.Nodes()) != 0 {
		t.Fatal("树节点空列表")
	}
}
