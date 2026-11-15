package dataloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/config"
)

// TestDataLoader_LoadJSONCases_对象形态 单对象 JSON 载入
func TestDataLoader_LoadJSONCases_对象形态(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.json")
	write(t, path, `{"case_id":"c1","input":"hello","reference":{}}`)
	cases, err := LoadJSONCases(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0]["case_id"] != "c1" {
		t.Fatalf("unexpected cases: %v", cases)
	}
}

// TestDataLoader_LoadJSONCases_列表与套件形态 列表形态与 validation 套件形态
func TestDataLoader_LoadJSONCases_列表与套件形态(t *testing.T) {
	dir := t.TempDir()
	listPath := filepath.Join(dir, "list.json")
	write(t, listPath, `[{"case_id":"a","input":"x"},{"case_id":"b","input":"y"}]`)
	cases, err := LoadJSONCases(listPath)
	if err != nil || len(cases) != 2 {
		t.Fatalf("list shape failed: %v %v", cases, err)
	}
	suitePath := filepath.Join(dir, "suite.json")
	write(t, suitePath, `{"validation":[{"task_id":"t1","prompt":"p"}],"evaluation":[]}`)
	suiteCases, err := LoadJSONCases(suitePath)
	if err != nil || len(suiteCases) != 1 {
		t.Fatalf("suite shape failed: %v %v", suiteCases, err)
	}
	if suiteCases[0]["case_id"] != "t1" || suiteCases[0]["task_id"] != "t1" {
		t.Fatalf("suite aliases missing: %v", suiteCases[0])
	}
	if suiteCases[0]["input"] != "p" {
		t.Fatalf("suite input alias missing: %v", suiteCases[0]["input"])
	}
}

// TestDataLoader_LoadJSONCases_错误形态 无效 JSON、空列表、缺 case_id
func TestDataLoader_LoadJSONCases_错误形态(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	write(t, bad, "{not json")
	if _, err := LoadJSONCases(bad); err == nil || err.Error() != "dataset json is invalid: "+bad {
		t.Fatalf("unexpected error: %v", err)
	}
	empty := filepath.Join(dir, "empty.json")
	write(t, empty, `[]`)
	if _, err := LoadJSONCases(empty); err == nil || err.Error() != "dataset cases must not be empty: "+empty {
		t.Fatalf("unexpected error: %v", err)
	}
	noID := filepath.Join(dir, "suite_bad.json")
	write(t, noID, `{"validation":[{"prompt":"p"}],"evaluation":[]}`)
	if _, err := LoadJSONCases(noID); err == nil || err.Error() != "dataset suite task must have a non-empty id: "+noID+"#1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDataLoader_Load_目录与批次 目录载入、批次规划与产物落盘
func TestDataLoader_Load_目录与批次(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.json"), `[{"case_id":"c1","input":"x","difficulty":"easy","dimension":"d1"},
		{"case_id":"c2","input":"y","difficulty":"hard","dimension":"d2"},
		{"case_id":"c3","input":"z","difficulty":"medium","dimension":"d1"}]`)
	loader, err := NewDataLoader(config.DataLoaderConfig{FilePattern: "*.json", BatchSize: 2, BatchBalanceKeys: []string{"dimension", "difficulty"}})
	if err != nil {
		t.Fatal(err)
	}
	batches, err := loader.Load(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(batches))
	}
	// 课程均衡：easy 最先
	if CaseID(batches[0][0]) != "c1" {
		t.Fatalf("first batch must start with easy case, got %s", CaseID(batches[0][0]))
	}
	if loader.BatchPlanPath == "" || loader.DatasetProfilePath == "" {
		t.Fatal("plan artifacts must be persisted")
	}
	if _, err := os.Stat(loader.BatchPlanPath); err != nil {
		t.Fatalf("batch plan missing: %v", err)
	}
}

// TestDataLoader_Load_目录缺失与无文件 目录缺失与无匹配文件报错文案
func TestDataLoader_Load_目录缺失与无文件(t *testing.T) {
	loader, _ := NewDataLoader(config.DataLoaderConfig{FilePattern: "*.json", BatchSize: 1, BatchBalanceKeys: []string{"dimension"}})
	if _, err := loader.Load(t.TempDir()+"/missing", 1); err == nil || !containsStr(err.Error(), "dataset directory not found: ") {
		t.Fatalf("missing dir error: %v", err)
	}
	empty := t.TempDir()
	if _, err := loader.Load(empty, 1); err == nil || err.Error() != "dataset json files not found: "+filepath.Join(empty, "*.json") {
		t.Fatalf("no files error: %v", err)
	}
}

// TestBatchPlanner_BatchPlanItem 计划条目结构与主导难度
func TestBatchPlanner_BatchPlanItem(t *testing.T) {
	batch := []map[string]any{
		{"case_id": "c1", "difficulty": "easy", "dimension": "d1", "source": "s", "task_type": "t"},
		{"case_id": "c2", "difficulty": "hard", "dimension": "d2", "source": "s", "task_type": "t"},
	}
	item := BatchPlanItem(batch, 3)
	if item["batch_id"] != "batch_003" {
		t.Fatalf("batch id mismatch: %v", item["batch_id"])
	}
	metadata := item["metadata"].(map[string]any)
	if metadata["difficulty_stage"] != "easy" {
		t.Fatalf("dominant difficulty mismatch: %v", metadata["difficulty_stage"])
	}
	dimensions := metadata["dimensions"].([]string)
	if len(dimensions) != 2 || dimensions[0] != "d1" {
		t.Fatalf("dimensions mismatch: %v", dimensions)
	}
}

// TestProfiler_CaseValue_元数据回退 均衡值顶层缺失时回退 metadata
func TestProfiler_CaseValue_元数据回退(t *testing.T) {
	caseMapping := map[string]any{"metadata": map[string]any{"dimension": "ops"}}
	if CaseValue(caseMapping, "dimension") != "ops" {
		t.Fatalf("metadata fallback failed: %s", CaseValue(caseMapping, "dimension"))
	}
	if CaseValue(map[string]any{}, "dimension") != UnknownValue {
		t.Fatal("missing value must be unknown")
	}
}

// TestProfiler_Profile_质量分级 警告比例驱动质量分级
func TestProfiler_Profile_质量分级(t *testing.T) {
	profile := DatasetProfiler{}.Profile([]map[string]any{{"case_id": "c", "dimension": "d"}}, []string{"dimension"})
	if profile["metadata"].(map[string]any)["quality"] != "normal" {
		t.Fatalf("quality mismatch: %v", profile["metadata"])
	}
	empty := DatasetProfiler{}.Profile(nil, []string{"dimension"})
	if empty["metadata"].(map[string]any)["quality"] != "empty" {
		t.Fatal("empty quality mismatch")
	}
}

// TestCaseFiles_TaskInput 别名与校验
func TestCaseFiles_TaskInput(t *testing.T) {
	value, err := TaskInput(map[string]any{"query": "  q  "})
	if err != nil || value != "  q  " {
		t.Fatalf("alias read failed: %v %v", value, err)
	}
	// legacy input 对象展开
	value, err = TaskInput(map[string]any{"input": map[string]any{"user_message": "hi"}})
	if err != nil || value != "hi" {
		t.Fatalf("legacy unwrap failed: %v %v", value, err)
	}
	if _, err := TaskInput(map[string]any{"input": " "}); err == nil || err.Error() != "case : input must not be empty" {
		t.Fatalf("empty input error: %v", err)
	}
	if _, err := TaskInput(map[string]any{"foo": 1}); err == nil || err.Error() != "case : input is required" {
		t.Fatalf("missing input error: %v", err)
	}
}

// TestCaseFiles_ResolveDatasetFile 穿越与绝对路径拒绝、越界拒绝
func TestCaseFiles_ResolveDatasetFile(t *testing.T) {
	base := t.TempDir()
	if _, err := ResolveDatasetFile(base, "../escape.json"); err == nil || err.Error() != "dataset file must be a relative path within the dataset: ../escape.json" {
		t.Fatalf("traversal error: %v", err)
	}
	if _, err := ResolveDatasetFile(base, "/abs.json"); err == nil {
		t.Fatal("absolute path must be rejected")
	}
	if _, err := ResolveDatasetFile(base, "missing.json"); err == nil || err.Error() != "dataset file not found: missing.json" {
		t.Fatalf("missing file error: %v", err)
	}
	write(t, filepath.Join(base, "ok.json"), "{}")
	resolved, err := ResolveDatasetFile(base, "ok.json")
	if err != nil || resolved != filepath.Join(base, "ok.json") {
		t.Fatalf("valid file resolution failed: %v %v", resolved, err)
	}
}

// TestGradingContract_ParseWeightedRubric 权重解析、中文标签与求和校验
func TestGradingContract_ParseWeightedRubric(t *testing.T) {
	positive, negative, err := ParseWeightedRubric("[weight 60%] 正确实现\n[权重 40%] 文档齐全\n[deduct 10%] 编译失败")
	if err != nil {
		t.Fatal(err)
	}
	if len(positive) != 2 || len(negative) != 1 {
		t.Fatalf("unexpected split: %d %d", len(positive), len(negative))
	}
	if positive[0]["weight"] != 0.6 || positive[0]["id"] != "rubric_001" {
		t.Fatalf("positive item mismatch: %v", positive[0])
	}
	if negative[0]["penalty"] != 0.1 || negative[0]["id"] != "deduction_001" {
		t.Fatalf("negative item mismatch: %v", negative[0])
	}
	// 权重不求和 100 报错
	if _, _, err := ParseWeightedRubric("[weight 50%] 只有一半"); err == nil ||
		err.Error() != "positive rubric percentages must sum to 100; weights will not be silently rescaled" {
		t.Fatalf("sum error: %v", err)
	}
	// 分组条目后的普通续行并入描述（Python group 分支）
	positive2, _, err2 := ParseWeightedRubric("[weight 100%] 正确实现\n  未标注续行并入描述")
	if err2 != nil || !containsStr(positive2[0]["description"].(string), "未标注续行并入描述") {
		t.Fatalf("continuation line must be appended: %v %v", positive2, err2)
	}
	// 编号分组外的编号行无法识别
	if _, _, err := ParseWeightedRubric("1. [weight 100%] ok\n2. 未标注行"); err == nil {
		t.Fatal("unrecognized item must error")
	}
}

// TestGradingContract_NormalizeGradingCase 别名映射与冲突
func TestGradingContract_NormalizeGradingCase(t *testing.T) {
	normalized, err := NormalizeGradingCase(map[string]any{
		"case_id":            "c1",
		"reference_solution": "answer text",
		"judge_rubrics":      "整体满足需求",
	})
	if err != nil {
		t.Fatal(err)
	}
	reference := normalized["reference"].(map[string]any)
	if reference["answer"] != "answer text" {
		t.Fatalf("answer alias missing: %v", reference)
	}
	if reference["answer_role"] != "reference" || reference["penalty_mode"] != "subtract" {
		t.Fatalf("reference contract mismatch: %v", reference)
	}
	// 与 reference.rubric 组合报错
	if _, err := NormalizeGradingCase(map[string]any{
		"case_id":       "c1",
		"judge_rubrics": "prose",
		"reference":     map[string]any{"rubric": []any{"manual"}},
	}); err == nil || err.Error() != "judge_rubrics cannot be combined with reference.rubric" {
		t.Fatalf("conflict error: %v", err)
	}
}

// TestPlanStore_往返 画像与批次计划往返
func TestPlanStore_往返(t *testing.T) {
	store := BatchPlanStore{}
	dir := t.TempDir()
	profile := map[string]any{
		"summary":  map[string]any{"total_cases": 1},
		"warnings": []any{},
		"metadata": map[string]any{"quality": "normal"},
	}
	profilePath, err := store.WriteDatasetProfile(dir, profile)
	if err != nil {
		t.Fatal(err)
	}
	if profilePath != filepath.Join(dir, "dataset_profile.yaml") {
		t.Fatalf("profile path mismatch: %s", profilePath)
	}
	batches := [][]map[string]any{{{"case_id": "c1", "difficulty": "easy", "dimension": "d"}}}
	planPath, err := store.WriteBatchPlan(dir, 2, 1, []string{"dimension"}, profile, batches)
	if err != nil {
		t.Fatal(err)
	}
	if planPath != filepath.Join(dir, "batch_plan.yaml") {
		t.Fatalf("plan path mismatch: %s", planPath)
	}
	raw, _ := os.ReadFile(planPath)
	if !containsStr(string(raw), "batch_plan_epoch_002") || !containsStr(string(raw), "curriculum_balanced") {
		t.Fatalf("plan content mismatch:\n%s", raw)
	}
}

// TestReferenceAdapter_非JSON引用跳过 非 JSON 引用不解析直接返回原用例
func TestReferenceAdapter_非JSON引用跳过(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "ref.txt"), "plain")
	caseMapping := map[string]any{
		"case_id":   "c1",
		"input":     "x",
		"case_path": filepath.Join(dir, "case.json"),
		"reference": map[string]any{"files": []any{"ref.txt"}},
	}
	adapted, err := AdaptReference(caseMapping, filepath.Join(dir, "case.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, hasSwebench := adapted["swebench"]; hasSwebench {
		t.Fatal("non-JSON reference must not produce adapter config")
	}
}

// write 测试助手：写文件（全包只定义一次）
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// containsStr 测试助手：子串判断（全包只定义一次）
func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOfStr(haystack, needle) >= 0)
}

// indexOfStr 子串查找
func indexOfStr(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
