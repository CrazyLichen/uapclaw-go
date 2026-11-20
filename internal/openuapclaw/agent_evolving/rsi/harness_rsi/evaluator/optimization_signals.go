package evaluator

import (
	"math"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator/judger"
)

// init 向 judger 反转缝注册优化信号契约（evaluator/optimization_signals 能力）。
func init() {
	judger.RegisterOptimizationSignalsContract(OptimizationSignalsContract)
}

// OptimizationSignalsSchemaVersion 优化信号契约版本。
const OptimizationSignalsSchemaVersion = 1

// ──────────────────────────── 导出函数 ────────────────────────────

// OptimizationSignalsContract 构建带版本的、数据集中立的搜索信号契约。
// Python: optimization_signals.py optimization_signals_contract——
// 这些信号可用于选择修复父本；晋级始终使用 case.score；
// continuous 协议直接把可信 rubric 分写入该字段。
func OptimizationSignalsContract(
	continuousScore any,
	dimensions map[string]any,
	source string,
) map[string]any {
	score := finiteFloat(continuousScore)
	normalizedDimensions := map[string]any{}
	for rawName, rawValue := range dimensions {
		name := trimSpaces(toSignalString(rawName))
		value := dimensionValue(rawValue)
		if name == "" || value == nil {
			continue
		}
		dimensionSource := source
		if rawMap, ok := rawValue.(map[string]any); ok {
			if s := toSignalString(rawMap["source"]); s != "" {
				dimensionSource = s
			}
		}
		normalizedDimensions[name] = map[string]any{
			"availability": "available",
			"value":        *value,
			"source":       dimensionSource,
		}
	}
	availability := "not_available"
	if score != nil {
		availability = "available"
	}
	// Python: dict(sorted(normalized_dimensions.items()))——键排序输出
	names := make([]string, 0, len(normalizedDimensions))
	for name := range normalizedDimensions {
		names = append(names, name)
	}
	sort.Strings(names)
	sortedDimensions := map[string]any{}
	for _, name := range names {
		sortedDimensions[name] = normalizedDimensions[name]
	}
	scoreValue := any(nil)
	if score != nil {
		scoreValue = *score
	}
	return map[string]any{
		"schema_version": OptimizationSignalsSchemaVersion,
		"continuous_score": map[string]any{
			"availability": availability,
			"value":        scoreValue,
			"source":       source,
		},
		"dimensions":          sortedDimensions,
		"promotion_authority": "eval_ref_case_score",
	}
}

// EvaluationOptimizationSignals 从评估元数据归一化显式优化契约。
// Python: optimization_signals.py evaluation_optimization_signals。
func EvaluationOptimizationSignals(metadata map[string]any) map[string]any {
	raw, ok := metadata["optimization_signals"].(map[string]any)
	if !ok {
		return OptimizationSignalsContract(nil, nil, "")
	}
	continuous, _ := raw["continuous_score"].(map[string]any)
	if continuous == nil {
		continuous = map[string]any{}
	}
	dimensions, _ := raw["dimensions"].(map[string]any)
	if dimensions == nil {
		dimensions = map[string]any{}
	}
	var continuousScore any
	if toSignalString(continuous["availability"]) == "available" {
		continuousScore = continuous["value"]
	}
	return OptimizationSignalsContract(
		continuousScore,
		dimensions,
		toSignalString(continuous["source"]),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// dimensionValue 维度值提取（Python: _dimension_value）。
func dimensionValue(value any) *float64 {
	if m, ok := value.(map[string]any); ok {
		availability := toSignalString(m["availability"])
		if availability == "" {
			availability = "available"
		}
		if availability != "available" {
			return nil
		}
		value = m["value"]
	}
	return finiteFloat(value)
}

// finiteFloat 有限浮点提取（Python: _finite_float——bool 拒绝，NaN/Inf 拒绝）。
func finiteFloat(value any) *float64 {
	if _, isBool := value.(bool); isBool {
		return nil
	}
	var number float64
	switch v := value.(type) {
	case float64:
		number = v
	case float32:
		number = float64(v)
	case int:
		number = float64(v)
	case int64:
		number = float64(v)
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		number = parsed
	case nil:
		return nil
	default:
		return nil
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return nil
	}
	return &number
}

// toSignalString 任意值转字符串（Python: str() 等价——nil 映射 "<nil>" 场景本包不触发）。
func toSignalString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		// Python str() 对非字符串的转换；YAML 数值等场景走格式化
		data, err := yaml.Marshal(v)
		if err != nil {
			return ""
		}
		// 去掉 Marshal 尾部换行
		s := string(data)
		if len(s) > 0 && s[len(s)-1] == '\n' {
			s = s[:len(s)-1]
		}
		return s
	}
}

// trimSpaces 去首尾空白。
func trimSpaces(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\n' || value[start] == '\r') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\n' || value[end-1] == '\r') {
		end--
	}
	return value[start:end]
}
