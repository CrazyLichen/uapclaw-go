package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DataLoader 把数据集目录读成批次化的用例映射（对齐 DataLoader）。
// 批次产出语义对齐偏差 D-6：Python 一次性迭代器 → Go Load 一次返回全部批次
// （画像与批次计划在返回前落盘，与 Python 产出顺序一致）。
type DataLoader struct {
	// Config 载入配置
	Config config.DataLoaderConfig
	// BatchPlanPath 最近一次批次计划产物路径
	BatchPlanPath string
	// DatasetProfilePath 最近一次数据集画像产物路径
	DatasetProfilePath string

	profiler  DatasetProfiler
	planner   BatchPlanner
	planStore BatchPlanStore
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDataLoader 构造载入器（对齐 DataLoader.__init__）
func NewDataLoader(config config.DataLoaderConfig) (*DataLoader, error) {
	if config.BatchSize < 1 {
		return nil, fmt.Errorf("data_loader.batch_size must be greater than or equal to 1")
	}
	return &DataLoader{Config: config}, nil
}

// Load 载入数据集目录并返回批次（对齐 DataLoader.load；返回前先落盘画像与计划）
func (d *DataLoader) Load(datasetDir string, epoch int) ([][]map[string]any, error) {
	root, err := filepath.Abs(expandTildePath(datasetDir))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("dataset directory not found: %s", root)
	}
	datasetFiles := listDatasetFiles(root, d.Config.FilePattern)
	if len(datasetFiles) == 0 {
		return nil, fmt.Errorf("dataset json files not found: %s", filepath.Join(root, d.Config.FilePattern))
	}
	return d.loadAndPlan(root, datasetFiles, epoch)
}

// LoadFiles 只载入显式冻结的数据集文件（对齐 load_files）
func (d *DataLoader) LoadFiles(datasetFiles []string, epoch int) ([][]map[string]any, error) {
	paths := make([]string, 0, len(datasetFiles))
	for _, value := range datasetFiles {
		path, err := filepath.Abs(expandTildePath(value))
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("dataset_files must not be empty")
	}
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil, fmt.Errorf("dataset json files not found: [%s]", strings.Join(paths, " "))
		}
	}
	roots := map[string]bool{}
	for _, path := range paths {
		roots[filepath.Dir(path)] = true
	}
	if len(roots) != 1 {
		return nil, fmt.Errorf("explicit dataset files must share one directory")
	}
	for root := range roots {
		return d.loadAndPlan(root, paths, epoch)
	}
	return nil, nil
}

// LoadJSONCases 从一个 JSON 文件载入一条或多条用例映射（对齐 load_json_cases）
func LoadJSONCases(path string) ([]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data any
	if jsonErr := json.Unmarshal(raw, &data); jsonErr != nil {
		return nil, fmt.Errorf("dataset json is invalid: %s", path)
	}
	rawCases := []any{}
	suiteShape := false
	if mapping, ok := data.(map[string]any); ok {
		if cases, isList := mapping["cases"].([]any); isList {
			rawCases = cases
		} else if _, exists := mapping["cases"]; exists {
			return nil, fmt.Errorf("dataset cases must be a list: %s", path)
		} else if _, hasValidation := mapping["validation"]; hasValidation {
			// 基准套件：取唯一非空拆分（对齐 validation/evaluation 分支链）
			cases, err := suiteCases(mapping, path)
			if err != nil {
				return nil, err
			}
			rawCases = cases
			suiteShape = true
		} else if _, hasEvaluation := mapping["evaluation"]; hasEvaluation {
			cases, err := suiteCases(mapping, path)
			if err != nil {
				return nil, err
			}
			rawCases = cases
			suiteShape = true
		} else {
			rawCases = []any{mapping}
		}
	} else if list, ok := data.([]any); ok {
		rawCases = list
	} else {
		return nil, fmt.Errorf("dataset json must be a case object, case list, or object with cases: %s", path)
	}
	cases := []map[string]any{}
	for index, item := range rawCases {
		caseMapping, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("dataset case must be a mapping: %s#%d", path, index+1)
		}
		if suiteShape {
			normalized, err := normalizeSuiteCase(caseMapping, path, index+1)
			if err != nil {
				return nil, err
			}
			caseMapping = normalized
		}
		normalizedCase, err := NormalizeGradingCase(caseMapping)
		if err != nil {
			return nil, err
		}
		cases = append(cases, normalizedCase)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("dataset cases must not be empty: %s", path)
	}
	// 仅元数据用例仍可供画像器使用；执行与服务准入要求显式任务输入
	if err := ValidateDatasetFiles(cases, filepath.Dir(absolutePath(path)), false); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(cases))
	for _, caseMapping := range cases {
		adapted, err := AdaptReference(caseMapping, absolutePath(path))
		if err != nil {
			return nil, err
		}
		out = append(out, adapted)
	}
	return out, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadAndPlan 物化用例并落盘一个确定性批次计划（对齐 _load_and_plan）
func (d *DataLoader) loadAndPlan(root string, datasetFiles []string, epoch int) ([][]map[string]any, error) {
	cases := []map[string]any{}
	for _, datasetFile := range datasetFiles {
		loaded, err := LoadJSONCases(datasetFile)
		if err != nil {
			return nil, err
		}
		for index, caseMapping := range loaded {
			if _, exists := caseMapping["case_index"]; !exists {
				caseMapping["case_index"] = index + 1
			}
			caseMapping["case_path"] = datasetFile
			cases = append(cases, caseMapping)
		}
	}
	profile := d.profiler.Profile(cases, d.Config.BatchBalanceKeys)
	batches := d.planner.Plan(cases, d.Config.BatchSize)
	profilePath, err := d.planStore.WriteDatasetProfile(root, profile)
	if err != nil {
		return nil, err
	}
	d.DatasetProfilePath = profilePath
	planPath, err := d.planStore.WriteBatchPlan(root, epoch, d.Config.BatchSize, d.Config.BatchBalanceKeys, profile, batches)
	if err != nil {
		return nil, err
	}
	d.BatchPlanPath = planPath
	return batches, nil
}

// listDatasetFiles 列出匹配模式的文件并排序（对齐 sorted(root.glob(pattern))）
func listDatasetFiles(root, pattern string) []string {
	base := strings.TrimSuffix(pattern, "*")
	_ = base
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if matched, _ := filepath.Match(pattern, entry.Name()); matched {
			out = append(out, filepath.Join(root, entry.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// isSuiteCase 判断顶层是否基准套件形态（validation/evaluation）
func isSuiteCase(data any) bool {
	mapping, ok := data.(map[string]any)
	if !ok {
		return false
	}
	_, hasValidation := mapping["validation"]
	_, hasEvaluation := mapping["evaluation"]
	return hasValidation || hasEvaluation
}

// suiteSplitError 套件拆分校验错误（对齐 validation/evaluation 分支链）
func suiteSplitError(mapping map[string]any, path string) error {
	for _, key := range []string{"validation", "evaluation"} {
		raw, exists := mapping[key]
		if !exists {
			continue
		}
		if _, isList := raw.([]any); !isList {
			return fmt.Errorf("dataset suite splits must be lists: %s", path)
		}
	}
	populated := []string{}
	for _, key := range []string{"validation", "evaluation"} {
		if list, exists := mapping[key].([]any); exists && len(list) > 0 {
			populated = append(populated, key)
		}
	}
	if len(populated) > 1 {
		return fmt.Errorf("dataset suite must select exactly one populated split: %s: %v", path, populated)
	}
	return nil
}

// suiteCases 取套件选中的用例列表
func suiteCases(mapping map[string]any, path string) ([]any, error) {
	if err := suiteSplitError(mapping, path); err != nil {
		return nil, err
	}
	for _, key := range []string{"validation", "evaluation"} {
		if list, exists := mapping[key].([]any); exists && len(list) > 0 {
			return list, nil
		}
	}
	return []any{}, nil
}

// normalizeSuiteCase 补基准任务引擎别名（对齐 _normalize_suite_case）
func normalizeSuiteCase(caseMapping map[string]any, path string, index int) (map[string]any, error) {
	normalized := copyMap(caseMapping)
	caseID := ""
	for _, key := range []string{"case_id", "task_id", "id"} {
		if raw, exists := normalized[key]; exists && raw != nil {
			if str, ok := raw.(string); ok {
				caseID = strings.TrimSpace(str)
				break
			}
		}
	}
	if caseID == "" {
		return nil, fmt.Errorf("dataset suite task must have a non-empty id: %s#%d", path, index)
	}
	if _, exists := normalized["case_id"]; !exists {
		normalized["case_id"] = caseID
	}
	if _, exists := normalized["task_id"]; !exists {
		normalized["task_id"] = caseID
	}
	if _, hasInput := normalized["input"]; !hasInput {
		normalized["input"] = fmt.Sprintf("%v", normalized["prompt"])
	}
	return normalized, nil
}

// expandTildePath 展开 ~ 前缀
func expandTildePath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// absolutePath 绝对化路径
func absolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// osReadFile 读文件（集中 import）
func osReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
