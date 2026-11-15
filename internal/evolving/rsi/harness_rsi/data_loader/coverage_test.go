package dataloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/config"
)

// TestCoverage_FileFingerprint 声明文件指纹
func TestCoverage_FileFingerprint(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "asset.json"), `{"x":1}`)
	if fingerprints, err := FileFingerprint(map[string]any{"case_id": "c"}); err != nil || len(fingerprints) != 0 {
		t.Fatalf("no declarations must be empty: %v %v", fingerprints, err)
	}
	caseMapping := map[string]any{
		"case_id":   "c",
		"case_path": filepath.Join(dir, "case.json"),
		"assets":    []any{"asset.json"},
	}
	fingerprints, err := FileFingerprint(caseMapping)
	if err != nil {
		t.Fatal(err)
	}
	if len(fingerprints) != 1 {
		t.Fatalf("expected 1 fingerprint, got %v", fingerprints)
	}
	if _, ok := fingerprints["assets/asset.json"]; !ok {
		t.Fatalf("key format mismatch: %v", fingerprints)
	}
	// 缺 case_path 报错
	if _, err := FileFingerprint(map[string]any{"assets": []any{"a.json"}}); err == nil ||
		err.Error() != "case_path is required to resolve dataset files" {
		t.Fatalf("missing case_path error: %v", err)
	}
}

// TestCoverage_CopyDatasetFiles 快照复制与指纹一致
func TestCoverage_CopyDatasetFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "data.json"), `{"n":1}`)
	caseMapping := map[string]any{
		"case_id":   "c",
		"input":     "do the task",
		"case_path": filepath.Join(dir, "case.json"),
		"reference": map[string]any{"files": []any{"data.json"}},
	}
	target := filepath.Join(dir, "snapshot", "case.json")
	copied, err := CopyDatasetFiles([]map[string]any{caseMapping}, filepath.Join(dir, "case.json"), target)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := copied["data.json"]; !ok {
		t.Fatalf("copied map mismatch: %v", copied)
	}
	if _, err := os.Stat(filepath.Join(dir, "snapshot", "data.json")); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
}

// TestCoverage_CopyPublicAssets 公开资产复制与越界拒绝
func TestCoverage_CopyPublicAssets(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "asset.txt"), "hello")
	caseMapping := map[string]any{
		"case_id":   "c",
		"case_path": filepath.Join(dir, "case.json"),
		"assets":    []any{"asset.txt"},
	}
	workspace := filepath.Join(dir, "workspace")
	copied, err := CopyPublicAssets(caseMapping, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != 1 || copied[0][0] != "asset.txt" {
		t.Fatalf("copied assets mismatch: %v", copied)
	}
	if _, err := os.Stat(filepath.Join(workspace, "asset.txt")); err != nil {
		t.Fatalf("workspace asset missing: %v", err)
	}
	// 无资产用例直接返回空
	empty, err := CopyPublicAssets(map[string]any{"case_id": "c"}, workspace)
	if err != nil || empty != nil {
		t.Fatalf("no assets must return nil: %v %v", empty, err)
	}
}

// TestCoverage_LoadFiles 显式文件列表载入
func TestCoverage_LoadFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "only.json")
	write(t, file, `{"case_id":"c1","input":"x"}`)
	loader, _ := NewDataLoader(makeLoaderConfig())
	batches, err := loader.LoadFiles([]string{file}, 1)
	if err != nil || len(batches) != 1 {
		t.Fatalf("load files failed: %v %v", batches, err)
	}
	// 空列表
	if _, err := loader.LoadFiles(nil, 1); err == nil || err.Error() != "dataset_files must not be empty" {
		t.Fatalf("empty list error: %v", err)
	}
	// 缺失文件
	if _, err := loader.LoadFiles([]string{filepath.Join(dir, "missing.json")}, 1); err == nil {
		t.Fatal("missing file must error")
	}
	// 跨目录
	other := filepath.Join(t.TempDir(), "x.json")
	write(t, other, `{"case_id":"c2","input":"y"}`)
	if _, err := loader.LoadFiles([]string{file, other}, 1); err == nil ||
		err.Error() != "explicit dataset files must share one directory" {
		t.Fatalf("cross-dir error: %v", err)
	}
}

// TestCoverage_AdaptReference_SwebenchFull swebench_official 适配全链与冲突
func TestCoverage_AdaptReference_SwebenchFull(t *testing.T) {
	dir := t.TempDir()
	official := []map[string]any{{
		"instance_id": "c1", "repo": "r", "base_commit": "b", "version": "1", "test_patch": "p",
	}}
	writeJSON(t, filepath.Join(dir, "official.json"), official)
	adapter := map[string]any{
		"adapter": "swebench_official",
		"config": map[string]any{
			"instance_id":           "c1",
			"official_dataset_path": "official.json",
			"repo":                  "r",
		},
	}
	writeJSON(t, filepath.Join(dir, "adapter.json"), adapter)
	caseMapping := map[string]any{
		"case_id":   "c1",
		"input":     "fix the bug",
		"case_path": filepath.Join(dir, "case.json"),
		"reference": map[string]any{"files": []any{"adapter.json", "official.json"}},
	}
	adapted, err := AdaptReference(caseMapping, filepath.Join(dir, "case.json"))
	if err != nil {
		t.Fatal(err)
	}
	config, ok := adapted["swebench"].(map[string]any)
	if !ok || config["instance_id"] != "c1" {
		t.Fatalf("swebench config missing: %v", adapted["swebench"])
	}
	if config["official_dataset_path"] != filepath.Join(dir, "official.json") {
		t.Fatalf("official path must be absolutized: %v", config["official_dataset_path"])
	}
	// instance_id 不匹配
	bad := copyCase(caseMapping)
	bad["case_id"] = "other"
	if _, err := AdaptReference(bad, filepath.Join(dir, "case.json")); err == nil ||
		err.Error() != "reference instance_id must match case_id" {
		t.Fatalf("mismatch error: %v", err)
	}
	// 多个 adapter 声明
	writeJSON(t, filepath.Join(dir, "adapter2.json"), adapter)
	multi := copyCase(caseMapping)
	multi["reference"] = map[string]any{"files": []any{"adapter.json", "adapter2.json"}}
	if _, err := AdaptReference(multi, filepath.Join(dir, "case.json")); err == nil ||
		err.Error() != "reference.files must declare exactly one verifier adapter" {
		t.Fatalf("multi-adapter error: %v", err)
	}
	// 不支持的 adapter
	unsupported := map[string]any{"adapter": "other", "config": map[string]any{}}
	writeJSON(t, filepath.Join(dir, "unsupported.json"), unsupported)
	badAdapter := copyCase(caseMapping)
	badAdapter["reference"] = map[string]any{"files": []any{"unsupported.json"}}
	if _, err := AdaptReference(badAdapter, filepath.Join(dir, "case.json")); err == nil ||
		err.Error() != "unsupported reference adapter: other" {
		t.Fatalf("unsupported adapter error: %v", err)
	}
}

// TestCoverage_CaseID_回退链 CaseID 回退：id 字段与路径 stem
func TestCoverage_CaseID_回退链(t *testing.T) {
	if got := CaseID(map[string]any{"id": "alt"}); got != "alt" {
		t.Fatalf("id fallback failed: %s", got)
	}
	got := CaseID(map[string]any{"case_path": "/tmp/data/case.json", "case_index": 3})
	if !strings.Contains(got, "case#3") {
		t.Fatalf("stem fallback failed: %s", got)
	}
}

// TestCoverage_RoundRobin_多维度 稳定排序与轮转（覆盖 stableCaseSortKey）
func TestCoverage_RoundRobin_多维度(t *testing.T) {
	cases := []map[string]any{
		{"case_id": "a", "dimension": "d2", "source": "s1", "task_type": "t", "case_index": 1},
		{"case_id": "b", "dimension": "d1", "source": "s1", "task_type": "t", "case_index": 2},
		{"case_id": "c", "dimension": "d2", "source": "s1", "task_type": "t", "case_index": 3},
	}
	ordered := roundRobinByDimension(cases)
	if len(ordered) != 3 {
		t.Fatalf("all cases must be ordered: %d", len(ordered))
	}
	// 轮转：第一轮 d1(b)、d2(a)；第二轮 d2(c)
	if CaseID(ordered[0]) != "b" || CaseID(ordered[1]) != "a" || CaseID(ordered[2]) != "c" {
		t.Fatalf("round-robin order mismatch: %s %s %s", CaseID(ordered[0]), CaseID(ordered[1]), CaseID(ordered[2]))
	}
}

// TestCoverage_HasWindowsDrive 盘符判定（平台无关，对齐 PureWindowsPath）
func TestCoverage_HasWindowsDrive(t *testing.T) {
	if !hasWindowsDrive(`C:\x`) || !hasWindowsDrive(`\\server\share`) {
		t.Fatal("drive letter and UNC must be detected")
	}
	if hasWindowsDrive("relative/path.txt") {
		t.Fatal("plain relative path must not be flagged")
	}
}

// TestCoverage_ValidateDatasetFiles_资产引用冲突 资产与私有引用同文件报错
func TestCoverage_ValidateDatasetFiles_资产引用冲突(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared.json"), "{}")
	caseMapping := map[string]any{
		"case_id":   "c",
		"case_path": filepath.Join(dir, "case.json"),
		"assets":    []any{"shared.json"},
		"reference": map[string]any{"files": []any{"shared.json"}},
	}
	if err := ValidateDatasetFiles([]map[string]any{caseMapping}, dir, false); err == nil ||
		err.Error() != "a private reference file must not also appear in assets" {
		t.Fatalf("conflict error: %v", err)
	}
	// 非 mapping reference
	bad := copyCase(caseMapping)
	bad["reference"] = "not a map"
	if err := ValidateDatasetFiles([]map[string]any{bad}, dir, false); err == nil ||
		err.Error() != "reference must be an object" {
		t.Fatalf("reference type error: %v", err)
	}
}

// TestCoverage_Loader_SuiteSelection 两个非空套件拆分报错
func TestCoverage_Loader_SuiteSelection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "suite.json")
	write(t, path, `{"validation":[{"task_id":"a","prompt":"p"}],"evaluation":[{"task_id":"b","prompt":"q"}]}`)
	if _, err := LoadJSONCases(path); err == nil ||
		!containsStr(err.Error(), "dataset suite must select exactly one populated split") {
		t.Fatalf("dual split error: %v", err)
	}
	// cases 键非列表
	badCases := filepath.Join(dir, "badcases.json")
	write(t, badCases, `{"cases": "not a list"}`)
	if _, err := LoadJSONCases(badCases); err == nil ||
		err.Error() != "dataset cases must be a list: "+badCases {
		t.Fatalf("cases type error: %v", err)
	}
	// case 非 mapping
	badItem := filepath.Join(dir, "baditem.json")
	write(t, badItem, `["not a case"]`)
	if _, err := LoadJSONCases(badItem); err == nil ||
		err.Error() != "dataset case must be a mapping: "+badItem+"#1" {
		t.Fatalf("case type error: %v", err)
	}
}

// makeLoaderConfig 测试助手：默认载入配置
func makeLoaderConfig() config.DataLoaderConfig {
	return config.DataLoaderConfig{FilePattern: "*.json", BatchSize: 2, BatchBalanceKeys: []string{"dimension", "difficulty"}}
}

// copyCase 测试助手：浅拷贝用例映射
func copyCase(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

// writeJSON 测试助手：JSON 序列化写文件（全包只定义一次）
func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCoverage_ProfilerWarningsPath 缺字段告警与低质量分级
func TestCoverage_ProfilerWarningsPath(t *testing.T) {
	cases := []map[string]any{
		{"case_id": "c1"},
		{"case_id": "c2", "dimension": "d"},
	}
	profile := DatasetProfiler{}.Profile(cases, []string{"dimension"})
	warnings := profile["warnings"].([]any)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %v", warnings)
	}
	quality := profile["metadata"].(map[string]any)["quality"]
	if quality != "low_quality_fallback" && quality != "partial_metadata" {
		t.Fatalf("quality mismatch: %v", quality)
	}
}

// TestCoverage_TaskInputDictInput 字典输入直接返回
func TestCoverage_TaskInputDictInput(t *testing.T) {
	input := map[string]any{"other": "shape"}
	value, err := TaskInput(map[string]any{"input": input})
	if err != nil || value == nil {
		t.Fatalf("dict input must pass through: %v %v", value, err)
	}
}

// TestCoverage_ValidateCaseFieldsBadAssets 非法资产列表报错
func TestCoverage_ValidateCaseFieldsBadAssets(t *testing.T) {
	if err := ValidateCaseFields(map[string]any{
		"case_id": "c", "input": "x", "assets": "not a list",
	}, true); err == nil || err.Error() != "assets must be a list of non-empty strings" {
		t.Fatalf("bad assets error: %v", err)
	}
	if err := ValidateCaseFields(map[string]any{
		"case_id": "c", "input": "x", "assets": []any{""},
	}, true); err == nil {
		t.Fatal("empty asset string must error")
	}
}

// TestCoverage_StringValueCoercionToAction 字符串路径覆盖 LoadFiles 单文件成功路径
func TestCoverage_LoadFiles_产物落盘(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "one.json")
	write(t, file, `{"case_id":"c1","input":"x","dimension":"d"}`)
	loader, _ := NewDataLoader(makeLoaderConfig())
	batches, err := loader.LoadFiles([]string{file}, 7)
	if err != nil || len(batches) != 1 {
		t.Fatalf("load failed: %v %v", batches, err)
	}
	if loader.DatasetProfilePath == "" || loader.BatchPlanPath == "" {
		t.Fatal("artifacts must persist")
	}
}

// TestCoverage_CoverageClosing 剩余小分支收口
func TestCoverage_CoverageClosing(t *testing.T) {
	// NewDataLoader batch_size 非法
	if _, err := NewDataLoader(config.DataLoaderConfig{BatchSize: 0}); err == nil ||
		err.Error() != "data_loader.batch_size must be greater than or equal to 1" {
		t.Fatalf("batch_size validation: %v", err)
	}
	// expandTildePath tilde 展开
	if expandTildePath("~/data") == "~/data" {
		t.Fatal("tilde must expand")
	}
	// CopyDatasetFiles 冲突路径：目标等于 target 本身
	dir := t.TempDir()
	write(t, filepath.Join(dir, "data.json"), "{}")
	badTarget := map[string]any{
		"case_id": "c", "input": "x",
		"case_path": filepath.Join(dir, "case.json"),
		"reference": map[string]any{"files": []any{"../case.json"}},
	}
	if _, err := CopyDatasetFiles([]map[string]any{badTarget}, filepath.Join(dir, "case.json"), filepath.Join(dir, "snap", "case.json")); err == nil {
		t.Fatal("escaping path must be rejected")
	}
	// NormalizeGradingCase：reference.solution 与顶层 reference_solution 冲突
	if _, err := NormalizeGradingCase(map[string]any{
		"case_id":            "c",
		"reference_solution": "a",
		"reference":          map[string]any{"solution": "b"},
	}); err == nil || err.Error() != "conflicting grading fields: reference_solution and reference.solution" {
		t.Fatalf("solution conflict: %v", err)
	}
	// reference 非映射报错（NormalizeGradingCase 直接入口）
	if _, err := NormalizeGradingCase(map[string]any{"case_id": "c", "reference": "bad"}); err == nil ||
		err.Error() != "reference must be an object" {
		t.Fatalf("reference type error: %v", err)
	}
	// judge_rubrics 顶层与 reference.judge_rubrics 冲突
	if _, err := NormalizeGradingCase(map[string]any{
		"case_id":       "c",
		"judge_rubrics": "text1",
		"reference":     map[string]any{"judge_rubrics": "text2"},
	}); err == nil || err.Error() != "conflicting grading fields: judge_rubrics and reference.judge_rubrics" {
		t.Fatalf("rubrics conflict: %v", err)
	}
	// difficultySortKey 未知难度（通过 dominant/plan 已覆盖；这里直接构造 Plan 验证排序稳定）
	planner := BatchPlanner{}
	ordered := planner.Plan([]map[string]any{
		{"case_id": "u", "difficulty": "unknown-ish", "dimension": "d"},
		{"case_id": "h", "difficulty": "hard", "dimension": "d"},
	}, 2)
	if CaseID(ordered[0][0]) != "h" {
		t.Fatalf("hard must precede unknown difficulty: %s", CaseID(ordered[0][0]))
	}
}
