// 评估指标收集：把用例结果聚合为 summary 指标（metrics_collector.py 一比一对齐）。
package evaluator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/evalerrors"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MetricsCollector 把用例结果聚合为汇总指标（Python MetricsCollector）。
type MetricsCollector struct{}

// NewMetricsCollector 构造指标收集器。
func NewMetricsCollector() *MetricsCollector { return &MetricsCollector{} }

// ──────────────────────────── 导出函数 ────────────────────────────

// Collect 从用例结果产物写 summary.json（Python collect；偏差 D-d3：async → 同步）。
func (c *MetricsCollector) Collect(caseResultsDir, outputPath string) (string, error) {
	root, err := filepath.Abs(caseResultsDir)
	if err != nil {
		return "", err
	}
	// Python glob("*/result.json")：一级子目录的 result.json，排序后逐个加载
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var resultFiles []string
	for _, entry := range dirEntries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(root, entry.Name(), "result.json")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			resultFiles = append(resultFiles, candidate)
		}
	}
	sort.Strings(resultFiles)
	results := make([]map[string]any, 0, len(resultFiles))
	for _, path := range resultFiles {
		result, err := loadCaseResult(path)
		if err != nil {
			return "", err
		}
		results = append(results, result)
	}
	for _, path := range resultFiles {
		result := results[index(resultFiles, path)]
		evaluation, ok := result["evaluation"].(map[string]any)
		if ok && judger.IsExecutionOnlyEvaluation(evaluation) {
			return "", evalerrors.NewEvaluationInfrastructureError(
				fmt.Sprintf("Cannot aggregate a legacy completion-only grade; reevaluate with a real judger: %s", path))
		}
	}
	total := len(results)
	passed := 0
	var scores []float64
	for _, item := range results {
		if casePassed(item) {
			passed++
		}
		if score, ok := item["score"].(float64); ok {
			scores = append(scores, score)
		} else if scoreInt, ok := item["score"].(int); ok {
			scores = append(scores, float64(scoreInt))
		}
	}
	failed := total - passed
	evaluationMethod := aggregateEvaluationMethod(results)
	summary := map[string]any{
		"total_cases":       total,
		"passed_cases":      passed,
		"failed_cases":      failed,
		"evaluation_method": evaluationMethod,
		"case_results":      resultFiles,
	}
	if len(scores) > 0 {
		sum := 0.0
		for _, score := range scores {
			sum += score
		}
		summary["average_score"] = sum / float64(len(scores))
	} else {
		summary["average_score"] = nil
	}
	output, err := filepath.Abs(outputPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(output, encoded, 0o644); err != nil {
		return "", err
	}
	return output, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// index 返回元素下标。
func index(paths []string, target string) int {
	for i, path := range paths {
		if path == target {
			return i
		}
	}
	return -1
}

// loadCaseResult 加载并校验单个用例结果（Python _load_json）。
func loadCaseResult(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("case result must be a mapping: %s", path)
	}
	return result, nil
}

// casePassed 判断用例是否通过（Python _case_passed）。
func casePassed(result map[string]any) bool {
	if evaluation, ok := result["evaluation"].(map[string]any); ok {
		if _, present := evaluation["passed"]; present {
			passed, _ := evaluation["passed"].(bool)
			return passed
		}
	}
	status, _ := result["status"].(string)
	return status == "passed"
}

// aggregateEvaluationMethod 取首个非空 evaluation.method（Python _aggregate_evaluation_method）。
func aggregateEvaluationMethod(results []map[string]any) string {
	for _, result := range results {
		evaluation, ok := result["evaluation"].(map[string]any)
		if !ok {
			continue
		}
		method, _ := evaluation["method"].(string)
		if method != "" {
			return method
		}
	}
	return ""
}
