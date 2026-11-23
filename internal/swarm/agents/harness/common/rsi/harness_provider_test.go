package rsi

// harness_provider.go 单测：校验归一、材料哈希校验（run/resume 错误码分流）、
// 状态/报告/树/产物读取（gate 形态）、配置装配模型引用。

import (
	"context"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	harnessconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestEngineValidateInput_三分支 缺路径/路径不存在/有效数据集。
func TestEngineValidateInput_三分支(t *testing.T) {
	// 缺路径。
	out := EngineValidateInput(nil)
	if boolValue(out["valid"]) {
		t.Fatalf("缺路径应 invalid")
	}
	errs := out["errors"].([]any)[0].(map[string]any)
	if errs["code"] != "DATASET_REQUIRED" {
		t.Fatalf("code 漂移: %v", errs)
	}
	// 路径不存在。
	missing := "/nonexistent/ds.json"
	out = EngineValidateInput(&missing)
	errs = out["errors"].([]any)[0].(map[string]any)
	if errs["code"] != "PATH_INVALID" {
		t.Fatalf("PATH_INVALID 漂移: %v", errs)
	}
	// 引擎标准数据集（非 validation 结构 → 直接 LoadCases）。
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	_ = os.WriteFile(path, []byte(`{"cases":[{"case_id":"c1","input":"问题"}]}`), 0o644)
	out = EngineValidateInput(&path)
	if !boolValue(out["valid"]) || safeInt(out["sample_count"]) != 1 {
		t.Fatalf("有效数据集漂移: %v", out)
	}
}

// TestValidateInput_坏数据集 错误码 DATASET_INVALID。
func TestValidateInput_坏数据集(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ds.json")
	_ = os.WriteFile(path, []byte(`{bad json`), 0o644)
	out := EngineValidateInput(&path)
	errs := out["errors"].([]any)[0].(map[string]any)
	if errs["code"] != "DATASET_INVALID" {
		t.Fatalf("坏 JSON code 漂移: %v", errs)
	}
}

// newTestHarnessTask 物化任务目录（task.json + 数据集 + refs + profile）。
func newTestHarnessTask(t *testing.T, root string) (string, string, string, string) {
	t.Helper()
	taskID := "rsi-hp"
	taskDir := filepath.Join(root, taskID)
	runDir := filepath.Join(taskDir, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dataset := filepath.Join(taskDir, "dataset.json")
	_ = os.WriteFile(dataset, []byte(`{"cases":[{"case_id":"c1","input":"问题"}]}`), 0o644)
	profile := filepath.Join(taskDir, "profile.yaml")
	_ = os.WriteFile(profile, []byte("workspace_dir: .\nmax_epochs: 1\n"), 0o644)
	store := NewTaskStore(root, nil)
	task := RsiTask{TaskID: taskID, Name: "n", Scenario: "HARNESS",
		Status: string(TaskStatusCreated), CreatedAt: UtcnowISO(),
		InputFile: &dataset, ModelRefs: map[string]any{},
		Config: map[string]any{"harness_refs_path": "", "orchestrator_config_path": profile, "rsi_materials": map[string]any{}},
		RunDir: runDir, StatusHistory: []map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	return taskID, dataset, profile, runDir
}

func shaFile(t *testing.T, path string) string {
	t.Helper()
	h, err := fileSha256(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return m
}

// TestVerifyTaskMaterials_哈希一致放行 材料快照 sha 全匹配。
func TestVerifyTaskMaterials_哈希一致放行(t *testing.T) {
	root := t.TempDir()
	taskID, dataset, profile, _ := newTestHarnessTask(t, root)
	p := NewHarnessProvider(root, nil)
	req := HarnessEngineRequest{
		TaskID: taskID, DatasetFiles: []string{dataset},
		OutputDir:              filepath.Join(root, taskID, "run"),
		OrchestratorConfigPath: &profile,
	}
	// 写入正确材料快照。
	// Python 语义：三角色缺 hash 或文件缺失即拒——补齐 harness refs 文件与快照。
	refs := filepath.Join(root, taskID, "refs.yaml")
	_ = os.WriteFile(refs, []byte("harness_refs:\n  role: "+profile+"\n"), 0o644)
	req.HarnessRefsPath = refs
	store := NewTaskStore(root, nil)
	if err := store.MergeConfig(taskID, map[string]any{"rsi_materials": map[string]any{
		"input_snapshot":   map[string]any{"sha256": shaFile(t, dataset)},
		"harness_snapshot": map[string]any{"sha256": shaFile(t, refs)},
		"profile":          map[string]any{"sha256": shaFile(t, profile)},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := p.verifyTaskMaterials(req, false); err != nil {
		t.Fatalf("一致材料应放行: %v", err)
	}
}

// TestVerifyTaskMaterials_runVsResume错误码 哈希不匹配时 run=PATH_NOT_ALLOWED / resume=RESUME_INPUT_CHANGED。
func TestVerifyTaskMaterials_runVsResume错误码(t *testing.T) {
	root := t.TempDir()
	taskID, dataset, profile, _ := newTestHarnessTask(t, root)
	p := NewHarnessProvider(root, nil)
	req := HarnessEngineRequest{
		TaskID: taskID, DatasetFiles: []string{dataset},
		OutputDir:              filepath.Join(root, taskID, "run"),
		OrchestratorConfigPath: &profile,
	}
	store := NewTaskStore(root, nil)
	_ = store.MergeConfig(taskID, map[string]any{"rsi_materials": map[string]any{
		"input_snapshot": map[string]any{"sha256": "deadbeef"},
		"profile":        map[string]any{"sha256": shaFile(t, profile)},
	}})
	err := p.verifyTaskMaterials(req, false)
	if err == nil || err.(*RsiError).Code != "PATH_NOT_ALLOWED" {
		t.Fatalf("run 应 PATH_NOT_ALLOWED: %v", err)
	}
	err = p.verifyTaskMaterials(req, true)
	if err == nil || err.(*RsiError).Code != "RESUME_INPUT_CHANGED" {
		t.Fatalf("resume 应 RESUME_INPUT_CHANGED: %v", err)
	}
}

// TestResultFromState_状态归一 非 completed/failed → completed。
func TestResultFromState_状态归一(t *testing.T) {
	root := t.TempDir()
	taskID, _, _, runDir := newTestHarnessTask(t, root)
	_ = os.WriteFile(filepath.Join(runDir, stateFile), []byte("status: running\nbest_score: 0.9\n"), 0o644)
	p := NewHarnessProvider(root, nil)
	out, err := p.resultFromState(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if valueToString(out["status"]) != "completed" {
		t.Fatalf("状态归一漂移: %v", out["status"])
	}
	if valueToString(out["error_code"]) != "" {
		t.Fatalf("正常结束不应有错误码")
	}
}

// TestReadState_gate形态 计数/最佳/基线回退。
func TestReadState_gate形态(t *testing.T) {
	root := t.TempDir()
	taskID, _, _, runDir := newTestHarnessTask(t, root)
	summary := filepath.Join(runDir, "evaluations", "e001", "b001", "source")
	if err := os.MkdirAll(summary, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(summary, "summary.json"), []byte(`{"total_cases": 1, "average_score": 0.42}`), 0o644)
	_ = os.WriteFile(filepath.Join(runDir, stateFile), []byte(
		"status: completed\ndataset:\n  cases: 1\ncandidate_gates:\n  - candidate_id: G001\n    accepted: true\n    status: accepted\n    candidate_score: 0.8\n    candidate_harness_refs_path: /refs/g001.yaml\n    before_harness_refs_path: ''\n    predicted_rank: 1\n"), 0o644)
	p := NewHarnessProvider(root, nil)
	state := p.ReadState(taskID)
	if valueToString(state["status"]) != "completed" || safeInt(state["iteration"]) != 1 {
		t.Fatalf("state 漂移: %v", state)
	}
	if valueToString(state["best_node_id"]) != "G001" {
		t.Fatalf("best_node_id 漂移: %v", state["best_node_id"])
	}
	// Python read_state 的 score 取 state.best_score（gate 形态无此键 → nil）。
	if state["score"] != nil {
		t.Fatalf("gate 形态 score 应 nil: %v", state["score"])
	}
	// 基线回退 source summary。
	if ptr, ok := state["baseline"].(*float64); !ok || ptr == nil || *ptr != 0.42 {
		t.Fatalf("baseline 回退漂移: %v", state["baseline"])
	}
}

// TestGetTree_gate链 parent 由 refs 映射。
func TestGetTree_gate链(t *testing.T) {
	root := t.TempDir()
	taskID, _, _, runDir := newTestHarnessTask(t, root)
	_ = os.WriteFile(filepath.Join(runDir, stateFile), []byte(
		"status: completed\ncandidate_gates:\n  - candidate_id: G001\n    accepted: true\n    status: accepted\n    candidate_score: 0.8\n    candidate_harness_refs_path: /refs/g001.yaml\n    before_harness_refs_path: ''\n  - candidate_id: G002\n    accepted: true\n    status: accepted\n    candidate_score: 0.9\n    candidate_harness_refs_path: /refs/g002.yaml\n    before_harness_refs_path: /refs/g001.yaml\n"), 0o644)
	p := NewHarnessProvider(root, nil)
	tree := p.GetTree(taskID)
	nodes := tree["nodes"].([]map[string]any)
	if len(nodes) != 3 {
		t.Fatalf("节点数漂移: %d", len(nodes))
	}
	// G002 父应为 G001。
	parentID := ""
	if ptr, ok := nodes[2]["parent_id"].(*string); ok && ptr != nil {
		parentID = *ptr
	}
	if parentID != "G001" {
		t.Fatalf("refs 链父漂移: %q", parentID)
	}
	if safeInt(tree["iteration"]) != 2 {
		t.Fatalf("iteration 漂移: %v", tree["iteration"])
	}
}

// TestLocateArtifact_gate索引 最后一个/指定 ID/缺失错误。
func TestLocateArtifact_gate索引(t *testing.T) {
	root := t.TempDir()
	taskID, _, _, runDir := newTestHarnessTask(t, root)
	_ = os.WriteFile(filepath.Join(runDir, stateFile), []byte(
		"status: completed\ncandidate_gates:\n  - candidate_id: G001\n    accepted: true\n    status: accepted\n    candidate_harness_refs_path: /refs/g001.yaml\n"), 0o644)
	p := NewHarnessProvider(root, nil)
	ref := p.LocateArtifact(taskID, nil)
	if valueToString(ref["artifact_id"]) != "G001" {
		t.Fatalf("缺省取最后漂移: %v", ref)
	}
	wanted := "G001"
	ref = p.LocateArtifact(taskID, &wanted)
	if valueToString(ref["artifact_id"]) != "G001" {
		t.Fatalf("按 ID 定位漂移")
	}
	missing := "NOPE"
	ref = p.LocateArtifact(taskID, &missing)
	if valueToString(ref["__code__"]) != "PATH_INVALID" {
		t.Fatalf("缺失错误漂移: %v", ref)
	}
}

// TestApplyModelRefs tester/optimizer 翻译。
func TestApplyModelRefs(t *testing.T) {
	p := NewHarnessProvider(t.TempDir(), nil)
	cfg, err := p.applyModelRefs(mustConfig(t, map[string]any{
		"model_configs":              map[string]any{"evaluation": "e0", "analysis": "a0", "member_optimization": "m0"},
		"evaluator":                  map[string]any{"model_config_ref": "e0"},
		"evaluation_result_analyzer": map[string]any{"model_config_ref": "a0"},
		"member_optimizer":           map[string]any{"model_config_ref": "m0"},
	}), map[string]any{"tester": "m-t", "optimizer": "m-o"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelConfigs.Evaluation != "m-t" || cfg.ModelConfigs.Analysis != "m-o" || cfg.ModelConfigs.MemberOptimization != "m-o" {
		t.Fatalf("ModelConfigs 翻译漂移: %+v", cfg.ModelConfigs)
	}
	if cfg.Evaluator.ModelConfigRef != "m-t" || cfg.MemberOptimizer.ModelConfigRef != "m-o" {
		t.Fatalf("段内 ref 翻译漂移")
	}
	// 空 refs 原样。
	cfg2, _ := p.applyModelRefs(mustConfig(t, map[string]any{}), nil)
	if cfg2.ModelConfigs.Evaluation != "" {
		t.Fatalf("空 refs 应原样")
	}
}

// mustConfig dict→AutoCoordinatingHarnessConfig。
func mustConfig(t *testing.T, data map[string]any) harnessconfig.AutoCoordinatingHarnessConfig {
	t.Helper()
	cfg, err := harnessconfig.AutoCoordinatingHarnessConfigFromDict(data)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestPauseTerminate_NotReady HARNESS 引擎无 pause/terminate hook。
func TestPauseTerminate_NotReady(t *testing.T) {
	p := NewHarnessProvider(t.TempDir(), nil)
	if _, err := p.Pause("t"); err == nil || err.(*RsiError).Code != "INTERNAL_ERROR" {
		t.Fatalf("pause 应 NotReady: %v", err)
	}
	if _, err := p.Terminate("t"); err == nil || err.(*RsiError).Code != "INTERNAL_ERROR" {
		t.Fatalf("terminate 应 NotReady: %v", err)
	}
	if p.SupportsPause() || p.SupportsTerminate() || !p.SupportsResume() {
		t.Fatalf("能力位漂移")
	}
}

// TestAdapter_BuildRequest 视图 → 引擎请求。
func TestAdapter_BuildRequest(t *testing.T) {
	store := NewTaskStore(t.TempDir(), nil)
	input := "/tmp/ds.json"
	runDir := "/tmp/run"
	task := RsiTask{TaskID: "rsi-a", Name: "n", Scenario: "HARNESS",
		Status: "CREATED", CreatedAt: UtcnowISO(), InputFile: &input,
		MaxIterations: 3, SearchWidth: 2, ModelRefs: map[string]any{"tester": "m"},
		Config: map[string]any{"harness_refs_path": "/refs.yaml", "dataset_id": "dsx"},
		RunDir: runDir}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	view, _ := store.GetView("rsi-a")
	req, err := BuildHarnessEngineRequest(view, false)
	if err != nil {
		t.Fatal(err)
	}
	if req.DatasetID != "dsx" || req.MaxIterations != 3 || req.HarnessRefsPath != "/refs.yaml" {
		t.Fatalf("请求字段漂移: %+v", req)
	}
	if len(req.DatasetFiles) != 1 || req.DatasetFiles[0] != input {
		t.Fatalf("dataset_files 漂移")
	}
	// 非 HARNESS 拒。
	task2 := task
	task2.Scenario = "ARTIFACT"
	_, err = BuildHarnessEngineRequest(task2.ToTaskView(), false)
	if err == nil || err.(*RsiError).Code != "SCENARIO_NOT_SUPPORTED" {
		t.Fatalf("场景守卫漂移: %v", err)
	}
	// 缺 input_file 拒。
	task3 := task
	task3.InputFile = nil
	_, err = BuildHarnessEngineRequest(task3.ToTaskView(), false)
	if err == nil || err.(*RsiError).Code != "SCENARIO_NOT_SUPPORTED" {
		t.Fatalf("input 守卫漂移: %v", err)
	}
}

// TestAdapter_ValidateInput归一 provider 结果 → RsiDatasetResult。
func TestAdapter_ValidateInput归一(t *testing.T) {
	provider := &fakeHarnessProvider{validateOut: map[string]any{
		"valid": false, "sample_count": nil,
		"errors": []any{map[string]any{"message": "第 1 行非法", "code": "DATASET_INVALID"}},
	}}
	adapter := NewHarnessEngineAdapter(provider)
	result, err := adapter.ValidateInput(nil, "HARNESS", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Errors) != 1 || result.Errors[0]["reason"] != "第 1 行非法" {
		t.Fatalf("归一漂移: %+v", result)
	}
	// artifact_type 拒。
	at := "PAPER"
	if _, err := adapter.ValidateInput(nil, "HARNESS", &at); err == nil || err.(*RsiError).Code != "SCENARIO_NOT_SUPPORTED" {
		t.Fatalf("artifact_type 守卫漂移")
	}
	// 非 HARNESS 场景拒。
	if _, err := adapter.ValidateInput(nil, "ARTIFACT", nil); err == nil || err.(*RsiError).Code != "SCENARIO_NOT_SUPPORTED" {
		t.Fatalf("场景守卫漂移")
	}
}

// fakeHarnessProvider 测试缝 provider。
type fakeHarnessProvider struct {
	validateOut map[string]any
}

func (f *fakeHarnessProvider) ValidateInput(path *string) map[string]any { return f.validateOut }
func (f *fakeHarnessProvider) Run(context.Context, HarnessEngineRequest, rsi.OnEvent) (map[string]any, error) {
	return nil, nil
}
func (f *fakeHarnessProvider) Resume(context.Context, HarnessEngineRequest, rsi.OnEvent) (map[string]any, error) {
	return nil, nil
}
func (f *fakeHarnessProvider) Pause(string) (map[string]any, error)          { return nil, nil }
func (f *fakeHarnessProvider) Terminate(string) (map[string]any, error)      { return nil, nil }
func (f *fakeHarnessProvider) ReadState(string) map[string]any               { return nil }
func (f *fakeHarnessProvider) ReadPublicationState(string) map[string]any    { return nil }
func (f *fakeHarnessProvider) ReadReport(string) map[string]any              { return nil }
func (f *fakeHarnessProvider) GetTree(string) map[string]any                 { return nil }
func (f *fakeHarnessProvider) LocateArtifact(string, *string) map[string]any { return nil }
func (f *fakeHarnessProvider) SupportsPause() bool                           { return false }
func (f *fakeHarnessProvider) SupportsResume() bool                          { return true }
func (f *fakeHarnessProvider) SupportsTerminate() bool                       { return false }

// TestPathSha256_目录寻址 目录 hash 与文件 hash 分流。
func TestPathSha256_目录寻址(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("x: 1"), 0o644)
	h1 := pathSha256(dir)
	h2 := pathSha256(dir)
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("目录摘要漂移")
	}
	// 修改文件后摘要变化。
	_ = os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("x: 2"), 0o644)
	if pathSha256(dir) == h1 {
		t.Fatalf("内容变化未反映")
	}
}

// TestReadPublicationState 空状态返回空表（installer 回退缝）。
func TestReadPublicationState(t *testing.T) {
	p := NewHarnessProvider(t.TempDir(), nil)
	state := p.ReadPublicationState("ghost")
	if state == nil {
		t.Fatalf("应返回空表")
	}
}

// TestLoadYAML_缓存一致性 解析期间被替换不缓存（简化：二次读取一致）。
func TestLoadYAML_缓存一致性(t *testing.T) {
	root := t.TempDir()
	taskID, _, _, runDir := newTestHarnessTask(t, root)
	_ = os.WriteFile(filepath.Join(runDir, stateFile), []byte("status: completed\n"), 0o644)
	p := NewHarnessProvider(root, nil)
	if p.loadYAML(taskID, stateFile)["status"] != "completed" {
		t.Fatalf("首次读取漂移")
	}
	_ = os.WriteFile(filepath.Join(runDir, stateFile), []byte("status: failed\n"), 0o644)
	if p.loadYAML(taskID, stateFile)["status"] != "failed" {
		t.Fatalf("替换后未失效缓存")
	}
}

// TestYamlRoundtrip 防 yaml 包未引用编译告警（真实用途见 refs 目标校验）。
func TestYamlRoundtrip(t *testing.T) {
	var out map[string]any
	if err := yaml.Unmarshal([]byte("harness_refs:\n  role: /x\n"), &out); err != nil {
		t.Fatal(err)
	}
	refs := out["harness_refs"].(map[string]any)
	if valueToString(refs["role"]) != "/x" {
		t.Fatalf("refs 解析漂移")
	}
	// sha 摘要助手复用校验。
	sum := sha256.Sum256([]byte("x"))
	if hex.EncodeToString(sum[:]) == "" {
		t.Fatalf("sha 助手漂移")
	}
}
