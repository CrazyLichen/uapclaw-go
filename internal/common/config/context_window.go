package config

import (
	"regexp"
	"strconv"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxContextWindowTokens 上下文窗口上限（2^53-1），超界视为非法
	maxContextWindowTokens = 1<<53 - 1
	// DefaultContextWindowTokens 默认上下文窗口（256Ki token）。
	// Python: DEFAULT_CONTEXT_WINDOW_TOKENS（context_window.py:12）
	DefaultContextWindowTokens = 256 * 1024
)

// ──────────────────────────── 全局变量 ────────────────────────────

// contextWindowPattern 匹配「数字 + 可选单位」的上下文窗口写法。
// 配置可能来自 YAML、环境变量或设置面板，统一在这里归一：`256k`、`256 K`、`1m`、`262144` 同义。
var contextWindowPattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(tokens?|k|ki?b?|m|mi?b?)?$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParsePositiveTokens 解析带可选 K/M 单位的正 token 数。
// 对齐上游 jiuwenswarm/common/context_window.py::parse_positive_int：
// 接受整数、能整除的浮点、以及 `256k`/`1m`/`262144` 这类字符串；
// 布尔、nil、0、负数、小数、超界值一律返回 ok=false。
func ParsePositiveTokens(value any) (int, bool) {
	if value == nil {
		return 0, false
	}
	switch v := value.(type) {
	case bool:
		return 0, false
	case int:
		return validTokens(v)
	case int64:
		return validTokens(int(v))
	case float64:
		if v != float64(int64(v)) {
			return 0, false
		}
		return validTokens(int(v))
	}

	normalized := strings.TrimSpace(strings.ReplaceAll(toString(value), ",", ""))
	match := contextWindowPattern.FindStringSubmatch(normalized)
	if match == nil {
		return 0, false
	}
	amount, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	multiplier := 1
	switch strings.ToLower(match[2]) {
	case "", "token", "tokens":
		multiplier = 1
	case "k", "kb", "ki", "kib":
		multiplier = 1024
	case "m", "mb", "mi", "mib":
		multiplier = 1024 * 1024
	}
	tokens := amount * float64(multiplier)
	if tokens != float64(int64(tokens)) {
		return 0, false
	}
	return validTokens(int(tokens))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validTokens 校验 token 数在合法区间。
func validTokens(tokens int) (int, bool) {
	if tokens <= 0 || tokens > maxContextWindowTokens {
		return 0, false
	}
	return tokens, true
}

// toString 把任意值转成字符串（仅用于配置值归一）。
func toString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	if b, ok := value.([]byte); ok {
		return string(b)
	}
	return strconv.FormatFloat(toFloatFallback(value), 'f', -1, 64)
}

// toFloatFallback 尽力把非字符串值转成浮点，失败回 0（随后会被正则判非法）。
func toFloatFallback(value any) float64 {
	if f, ok := toFloat64(value); ok {
		return f
	}
	return 0
}

// NormalizeModelWindowMapping 归一「模型名 → 窗口」映射（非正/非法值剔除）。
// 对齐 Python _normalize_model_window_mapping（context_window.py:63-76）：
// 空映射返回 nil；键必须为字符串；值经 ParsePositiveTokens 过滤。
func NormalizeModelWindowMapping(value any) map[string]int {
	mapping, ok := value.(map[string]any)
	if !ok || len(mapping) == 0 {
		return nil
	}
	normalized := map[string]int{}
	for modelName, window := range mapping {
		parsed, ok := ParsePositiveTokens(window)
		if !ok {
			continue
		}
		normalized[modelName] = parsed
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// ResolveContextWindowTokens 无需查询 provider 元数据即可解析模型窗口。
// 对齐 Python resolve_context_window_tokens（context_window.py:79-120）。
// 优先序：全局 context_window_tokens → 选中模型 context_window →
// 显式模型映射（model_context_window_tokens）→ 固定默认值。
// modelContextWindowOverride 由运行时路径在选中模型已转 ModelRequestConfig、
// 内部 _source 标记不可得时传入。
func ResolveContextWindowTokens(modelName string, contextEngineConfig, modelConfigObj, modelContextWindowOverride any) int {
	config, _ := contextEngineConfig.(map[string]any)
	if config == nil {
		config = map[string]any{}
	}
	// Python: "context_engine_config" in config → 下钻一层
	if nested, ok := config["context_engine_config"].(map[string]any); ok {
		config = nested
	}
	modelConfig, _ := modelConfigObj.(map[string]any)
	if modelConfig == nil {
		modelConfig = map[string]any{}
	}

	globalWindow, _ := ParsePositiveTokens(config["context_window_tokens"])

	configuredModelWindow, _ := ParsePositiveTokens(modelConfig["context_window"])
	if configuredModelWindow == 0 {
		configuredModelWindow, _ = ParsePositiveTokens(modelContextWindowOverride)
	}

	if globalWindow != 0 {
		return globalWindow
	}
	if configuredModelWindow != 0 {
		return configuredModelWindow
	}

	if modelName != "" {
		mapped := NormalizeModelWindowMapping(config["model_context_window_tokens"])
		if mapped != nil {
			if window, ok := mapped[modelName]; ok {
				return window
			}
		}
	}
	return DefaultContextWindowTokens
}
