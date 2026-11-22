package rsi

// services/context/materializer/artifact/activation 单测（Task 10+12 核心链）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateScenario_组合 全分支。
func TestValidateScenario_组合(t *testing.T) {
	sc, at, err := ValidateScenario("harness", nil, false)
	if err != nil || *sc != ScenarioHarness || *at != "" {
		t.Fatalf("harness 组合漂移: %v %v %v", sc, at, err)
	}
	if _, _, err := ValidateScenario("harness", strPtr("PAPER"), false); err == nil {
		t.Fatalf("harness+artifact_type 应拒")
	}
	if _, _, err := ValidateScenario("ARTIFACT", nil, true); err == nil {
		t.Fatalf("ARTIFACT 缺 type 应拒")
	}
	if _, _, err := ValidateScenario("bogus", nil, false); err == nil {
		t.Fatalf("非法 scenario 应拒")
	}
	if _, _, err := ValidateScenario("ARTIFACT", strPtr("x"), true); err == nil {
		t.Fatalf("非法 artifact_type 应拒")
	}
}

// TestTaskServiceCreate_HARNESS全链 create → materialize → task.json 落盘。
func TestTaskServiceCreate_HARNESS全链(t *testing.T) {
	root := t.TempDir()
	// 数据集 + harness 配置。
	ds := filepath.Join(root, "ds.json")
	_ = os.WriteFile(ds, []byte(`{"cases":[{"case_id":"c1","input":"问题"}]}`), 0o644)
	harnessRoot := filepath.Join(root, "harness-src")
	_ = os.MkdirAll(harnessRoot, 0o755)
	_ = os.WriteFile(filepath.Join(harnessRoot, "harness_config.yaml"),
		[]byte("schema_version: 1\nid: demo-harness\nname: demo\ntools: []\n"), 0o644)
	store := NewTaskStore(filepath.Join(root, "tasks"), nil)
	resolver := NewModelConfigResolver()
	resolver.defaultsLoader = func(map[string]any) []map[string]any {
		return []map[string]any{{
			"model_client_config": map[string]any{
				"model_name": "m1", "client_provider": "OpenAI", "api_base": "http://x"},
			"model_config_obj": map[string]any{"model": "m1"},
			"is_default":       true,
		}}
	}
	resolver.modelBuilder = func(mcc, mco map[string]any) (map[string]any, map[string]any, error) {
		return cloneMap(mcc), cloneMap(mco), nil
	}
	materializer := NewTaskMaterializer(store.TasksRoot(), "", harnessRoot, false)
	// adapterResolver：HARNESS 场景返回 nil（ARTIFACT-only 校验分支不触发）。
	svc := NewTaskService(store, func(string, string) ScenarioAdapter { return nil }, materializer, resolver, nil)
	refsProvider := func(map[string]any) (string, error) { return harnessRoot, nil }
	svc.BindTaskService(refsProvider)
	out, err := svc.Create(map[string]any{
		"scenario": "HARNESS", "name": "实验一",
		"input_file": ds,
		"model_refs": map[string]any{"optimizer": "m1", "tester": "m1"},
	})
	if err != nil {
		t.Fatalf("create 漂移: %v", err)
	}
	taskID := valueToString(out["task_id"])
	if valueToString(out["status"]) != "CREATED" {
		t.Fatalf("status 漂移")
	}
	got, err := store.Get(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config["active_ref_released"] != true {
		t.Fatalf("物化任务应放行删除")
	}
	if valueToString(got.Config["orchestrator_config_path"]) == "" {
		t.Fatalf("profile 未物化")
	}
	// 物化副本可校验（run_dir 存在）。
	if !dirExists(got.RunDir) {
		t.Fatalf("run_dir 未建")
	}
}

func cloneMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// TestTaskServiceCreate_校验错误码全集 名称/model_refs/harness tester。
func TestTaskServiceCreate_校验错误码全集(t *testing.T) {
	root := t.TempDir()
	store := NewTaskStore(filepath.Join(root, "tasks"), nil)
	svc := NewTaskService(store, nil, nil, nil, nil)
	mk := func(name string, refs map[string]any) map[string]any {
		return map[string]any{"scenario": "HARNESS", "name": name, "input_file": "/x.json", "model_refs": refs}
	}
	if _, err := svc.Create(mk("", map[string]any{"optimizer": "m", "tester": "t"})); err == nil ||
		err.Error() != "实验名称必填且 1–100 字符" {
		t.Fatalf("名称校验: %v", err)
	}
	if _, err := svc.Create(mk("n", nil)); err == nil || err.Error() != "model_refs 必填" {
		t.Fatalf("model_refs 校验: %v", err)
	}
	if _, err := svc.Create(mk("n", map[string]any{"optimizer": "m"})); err == nil ||
		err.Error() != "harness 优化必填 model_refs.tester" {
		t.Fatalf("tester 校验: %v", err)
	}
	if _, err := svc.Create(map[string]any{"scenario": "HARNESS", "name": "n",
		"model_refs": map[string]any{"optimizer": "m", "tester": "t"}}); err == nil ||
		err.Error() != "harness 优化必填 input_file（数据集）" {
		t.Fatalf("input_file 校验: %v", err)
	}
}

// TestTaskServiceDelete_守卫 QUEUED 产物在用拒删；released 后可删。
func TestTaskServiceDelete_守卫(t *testing.T) {
	root := t.TempDir()
	store := NewTaskStore(filepath.Join(root, "tasks"), nil)
	activation := NewHarnessActivationStore(store.TasksRoot())
	svc := NewTaskService(store, nil, nil, nil, activation)
	task := RsiTask{TaskID: "rsi-d", Name: "n", Scenario: "HARNESS", Status: "CREATED",
		CreatedAt: UtcnowISO(), ModelRefs: map[string]any{},
		Config: map[string]any{"harness_refs_path": "/refs", "active_ref_released": false}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateStatus("rsi-d", []string{"CREATED"}, "QUEUED", "enqueue"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(map[string]any{"task_id": "rsi-d"}, nil); err == nil ||
		!strings.Contains(err.Error(), "产物仍在生效") {
		t.Fatalf("在用产物应拒删: %v", err)
	}
	if err := store.MarkActiveRefReleased("rsi-d"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(map[string]any{"task_id": "rsi-d"}, nil); err != nil {
		t.Fatalf("released 后应可删: %v", err)
	}
}

// TestArtifactService_快照与定位 打包/幂等/best。
func TestArtifactService_快照与定位(t *testing.T) {
	root := t.TempDir()
	store := NewTaskStore(root, nil)
	task := RsiTask{TaskID: "rsi-s", Name: "n", Scenario: "HARNESS", Status: "CREATED",
		CreatedAt: UtcnowISO(), ModelRefs: map[string]any{}, Config: map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "rsi-s", "harness_refs.yaml")
	_ = os.WriteFile(artifact, []byte("harness_refs:\n  role: /x\n"), 0o644)
	svc := NewArtifactService(root)
	id := svc.MakeSnapshot("rsi-s", "c1", "N1", []RsiArtifactPath{{Role: "role", Path: artifact, Format: ""}})
	if id != "AN1" {
		t.Fatalf("artifact_id 漂移: %s", id)
	}
	// 无效路径跳过 → 空快照。
	if svc.MakeSnapshot("rsi-s", "c2", "N2", []RsiArtifactPath{{Role: "r", Path: "/nonexistent", Format: ""}}) != "" {
		t.Fatalf("无效路径应跳过")
	}
	// 按 ID 定位。
	file, err := svc.Locate("rsi-s", strPtr("AN1"))
	if err != nil || !strings.HasSuffix(file.Path, "AN1.zip") {
		t.Fatalf("定位漂移: %v", file)
	}
	// 缺省 = best（最新 adopted）。
	best, err := svc.Locate("rsi-s", nil)
	if err != nil || !best.IsBest {
		t.Fatalf("best 定位漂移: %v", best)
	}
	// best_artifact 对象。
	if ba := svc.BestArtifact("rsi-s"); ba == nil || ba["artifact_id"] != "AN1" {
		t.Fatalf("best_artifact 漂移: %v", ba)
	}
}

// TestActivationStore_轮换与序号 commit 轮换历史 + 序号递增。
func TestActivationStore_轮换与序号(t *testing.T) {
	root := t.TempDir()
	s := NewHarnessActivationStore(root)
	v1Dir := filepath.Join(root, "v1")
	_ = os.MkdirAll(v1Dir, 0o755)
	rec1 := map[string]any{"installation_id": "inst-1", "runtime_path": v1Dir,
		"sha256": strings.Repeat("a", 64)}
	if err := s.Commit(rec1); err != nil {
		t.Fatal(err)
	}
	if s.ResolveActiveRuntimePath() != v1Dir {
		t.Fatalf("激活路径漂移")
	}
	v2Dir := filepath.Join(root, "v2")
	_ = os.MkdirAll(v2Dir, 0o755)
	rec2 := map[string]any{"installation_id": "inst-2", "runtime_path": v2Dir,
		"sha256": strings.Repeat("b", 64)}
	if err := s.Commit(rec2); err != nil {
		t.Fatal(err)
	}
	// v1 进历史，v2 激活。
	if len(s.ListHistory()) != 1 {
		t.Fatalf("历史轮换漂移: %v", s.ListHistory())
	}
	versions := s.ListVersions()
	if len(versions) != 2 || safeInt(versions[0]["version_sequence"]) != 1 {
		t.Fatalf("序号排序漂移: %v", versions)
	}
	// sha 非法拒。
	if err := s.Commit(map[string]any{"installation_id": "x", "runtime_path": v1Dir, "sha256": "zz"}); err == nil {
		t.Fatalf("sha 校验缺失")
	}
	// runtime 越界拒。
	if err := s.Commit(map[string]any{"installation_id": "y", "runtime_path": "/etc", "sha256": strings.Repeat("a", 64)}); err == nil {
		t.Fatalf("越界 runtime 应拒")
	}
}

// TestInstaller_Rollback门禁 运行中任务存在即拒。
func TestInstaller_Rollback门禁(t *testing.T) {
	root := t.TempDir()
	store := NewTaskStore(filepath.Join(root, "tasks"), nil)
	task := RsiTask{TaskID: "rsi-r", Name: "n", Scenario: "HARNESS", Status: "RUNNING",
		CreatedAt: UtcnowISO(), ModelRefs: map[string]any{}, Config: map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	inst := NewHarnessInstaller(store, nil, nil, NewHarnessActivationStore(store.TasksRoot()))
	if _, err := inst.Rollback("whatever"); err == nil ||
		!strings.Contains(err.Error(), "不能回退") {
		t.Fatalf("门禁漂移: %v", err)
	}
}

// TestInstaller_Install前置校验 非完成态/缺发布物。
func TestInstaller_Install前置校验(t *testing.T) {
	root := t.TempDir()
	store := NewTaskStore(filepath.Join(root, "tasks"), nil)
	task := RsiTask{TaskID: "rsi-i", Name: "n", Scenario: "HARNESS", Status: "RUNNING",
		CreatedAt: UtcnowISO(), ModelRefs: map[string]any{}, Config: map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	inst := NewHarnessInstaller(store, nil, nil, NewHarnessActivationStore(store.TasksRoot()))
	if _, err := inst.Install("rsi-i"); err == nil ||
		!strings.Contains(err.Error(), "尚未完成") {
		t.Fatalf("完成态校验: %v", err)
	}
	if _, err := store.UpdateStatus("rsi-i", []string{"RUNNING"}, "COMPLETED", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.Install("rsi-i"); err == nil ||
		!strings.Contains(err.Error(), "缺少 published") {
		t.Fatalf("发布物校验: %v", err)
	}
}

// TestMaterializeDataset_副本与摘要 Evo-Bench 归一化与标准路径双分支。
func TestMaterializeDataset_副本与摘要(t *testing.T) {
	root := t.TempDir()
	m := NewTaskMaterializer(filepath.Join(root, "tasks"), "", "", false)
	ds := filepath.Join(root, "ds.json")
	_ = os.WriteFile(ds, []byte(`{"cases":[{"case_id":"c1","input":"问题"}]}`), 0o644)
	out, err := m.MaterializeDataset("rsi-m", ds, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if valueToString(out["dataset_id"]) != "" {
		t.Fatalf("标准数据集 dataset_id 应 nil: %v", out["dataset_id"])
	}
	if valueToString(out["sha256"]) == "" {
		t.Fatalf("摘要缺失")
	}
	if !fileExists(valueToString(out["path"])) {
		t.Fatalf("副本未落盘")
	}
	// Evo-Bench domain 过滤。
	ds2 := filepath.Join(root, "evobench.json")
	_ = os.WriteFile(ds2, []byte(`{"validation":[{"id":"gdpval-1","domain":"general","prompt":"p"}]}`), 0o644)
	out2, err := m.MaterializeDataset("rsi-m", ds2, "general", nil)
	if err != nil {
		t.Fatal(err)
	}
	if valueToString(out2["dataset_id"]) != "evobench_validation_1" {
		t.Fatalf("evobench 归一漂移: %v", out2["dataset_id"])
	}
	// 数据集越根拒绝。
	if _, err := m.MaterializeDataset("rsi-m", "/etc/hostname", "", nil); err == nil {
		t.Fatalf("信任根校验缺失")
	}
}

// TestMaterializeHarnessRefs_版本目录 包复制 + refs 生成 + wrapper 溯源。
func TestMaterializeHarnessRefs_版本目录(t *testing.T) {
	root := t.TempDir()
	harnessRoot := filepath.Join(root, "harness-src")
	_ = os.MkdirAll(harnessRoot, 0o755)
	_ = os.WriteFile(filepath.Join(harnessRoot, "harness_config.yaml"),
		[]byte("schema_version: 1\nid: demo\n"), 0o644)
	m := NewTaskMaterializer(filepath.Join(root, "tasks"), "", harnessRoot, false)
	out, err := m.MaterializeHarnessRefs("rsi-h", harnessRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(valueToString(out["version_id"]), "baseline-") {
		t.Fatalf("version_id 漂移: %v", out["version_id"])
	}
	if !fileExists(valueToString(out["path"])) {
		t.Fatalf("refs 未落盘")
	}
	// refs 内容指向包路径。
	data, _ := os.ReadFile(valueToString(out["path"]))
	if !strings.Contains(string(data), "harness_refs:") {
		t.Fatalf("refs 形状漂移: %s", data)
	}
}

// TestProfileOptions_归一 evaluation_method 校验与 single-harness 约束。
func TestProfileOptions_归一(t *testing.T) {
	if _, err := normalizeProfileOptions(map[string]any{"evaluation_method": "bogus"}); err == nil {
		t.Fatalf("非法 method 应拒")
	}
	if _, err := normalizeProfileOptions(map[string]any{"sibling_candidate_count": 2}); err == nil {
		t.Fatalf("多候选应拒")
	}
	out, err := normalizeProfileOptions(map[string]any{})
	if err != nil || valueToString(out["evaluation_method"]) != "script-based" {
		t.Fatalf("缺省漂移: %v", out)
	}
}

// TestResolveHarnessSource_包装与直包 refs 包装解析 + 单 role。
func TestResolveHarnessSource_包装与直包(t *testing.T) {
	root := t.TempDir()
	// refs 包装。
	wrapper := filepath.Join(root, "refs.yaml")
	pkg := filepath.Join(root, "pkg")
	_ = os.MkdirAll(pkg, 0o755)
	_ = os.WriteFile(wrapper, []byte("harness_refs:\n  validation_harness: "+pkg+"\n"), 0o644)
	resolved, err := resolveHarnessSource(wrapper, "", "validation_harness")
	if err != nil || resolved != pkg {
		t.Fatalf("包装解析漂移: %s %v", resolved, err)
	}
	// 直包（harness_config.yaml 含 id）。
	direct := filepath.Join(root, "direct")
	_ = os.MkdirAll(direct, 0o755)
	_ = os.WriteFile(filepath.Join(direct, "harness_config.yaml"), []byte("id: x\n"), 0o644)
	resolved, err = resolveHarnessSource(direct, "", "role")
	if err != nil || resolved != direct {
		t.Fatalf("直包解析漂移: %s %v", resolved, err)
	}
}

// TestProviderReportToWeb_HARNESS形状 最小等价投影（D-9-1 连带）。
func TestProviderReportToWeb_HARNESS形状(t *testing.T) {
	out := providerReportToWeb(map[string]any{
		"status": "completed", "score": 0.9, "baseline": 0.5, "iteration": 3,
		"usage":        map[string]any{"call_count": 5},
		"best_node_id": "G001",
	}, map[string]any{})
	if out["status"] != "completed" || out["best_score"] != 0.9 {
		t.Fatalf("投影漂移: %v", out)
	}
	if out["markdown"] != nil {
		t.Fatalf("markdown 应 nil")
	}
}

// TestUsageService_优先消费视角 adapter 在场时优先 recorder。
func TestUsageService_优先消费视角(t *testing.T) {
	store := NewTaskStore(t.TempDir(), nil)
	task := RsiTask{TaskID: "rsi-u", Name: "n", Scenario: "HARNESS", Status: "CREATED",
		CreatedAt: UtcnowISO(), ModelRefs: map[string]any{}, Config: map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	recorder := NewUsageRecorder()
	recorder.Record("rsi-u", nil, RsiModelCall{Model: "m", CallCount: 1, Tokens: Tokens{Input: 7}}, nil)
	svc := NewUsageService(recorder, store, nil)
	out, err := svc.Get(map[string]any{"task_id": "rsi-u"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	usage := out["usage"].(map[string]any)
	if safeInt(usage["call_count"]) != 1 {
		t.Fatalf("usage 聚合漂移: %v", usage)
	}
	// 未记录且无 adapter → NOT_FOUND。
	if _, err := svc.Get(map[string]any{"task_id": "rsi-ghost"}, nil); err == nil {
		t.Fatalf("未记录应 NOT_FOUND")
	}
}

// TestServiceContext_组合根 build + 单次恢复。
func TestServiceContext_组合根(t *testing.T) {
	root := t.TempDir()
	tasksRoot := filepath.Join(root, "tasks")
	store := NewTaskStore(tasksRoot, nil)
	task := RsiTask{TaskID: "rsi-c", Name: "n", Scenario: "HARNESS", Status: "RUNNING",
		CreatedAt: UtcnowISO(), ModelRefs: map[string]any{}, Config: map[string]any{}}
	if _, err := store.Create(task); err != nil {
		t.Fatal(err)
	}
	ctx := BuildRsiServiceContext(tasksRoot, false, false)
	// HARNESS provider 注册。
	provider := NewHarnessProvider(tasksRoot, nil)
	ctx.RegisterHarnessProvider(provider)
	// recover 一次性。
	sum1 := ctx.RecoverWorkspace()
	if sum1["recovered"] != 1 {
		t.Fatalf("恢复漂移: %v", sum1)
	}
	// Python 语义：二次调用返回首次摘要原样（一次性对账标记）。
	sum2 := ctx.RecoverWorkspace()
	if sum2["recovered"] != 1 {
		t.Fatalf("恢复摘要应原样返回: %v", sum2)
	}
	got, _ := ctx.Store.Get("rsi-c")
	if got.Status != string(TaskStatusPaused) {
		t.Fatalf("对账状态漂移: %s", got.Status)
	}
}
