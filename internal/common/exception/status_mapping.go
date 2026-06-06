package exception

import "strings"

// ──────────────────────────── 结构体 ────────────────────────────

// keywordRule 关键字匹配规则。
type keywordRule struct {
	// keywords 需要匹配的关键字列表（StatusCode.Name 包含任一即命中）
	keywords []string
	// category 命中后对应的 ErrorCategory
	category ErrorCategory
}

// rangeRule 数值区间匹配规则。
type rangeRule struct {
	// start 区间起始（包含）
	start int
	// end 区间结束（包含）
	end int
	// category 命中后对应的 ErrorCategory
	category ErrorCategory
}

// ──────────────────────────── 全局变量 ────────────────────────────

// manualOverrides 手动覆盖表：StatusCode.Name → ErrorCategory。
// 对应 Python: _MANUAL_OVERRIDES_RAW
var manualOverrides = map[string]ErrorCategory{
	"CONTROLLER_INVOKE_LLM_FAILED":         ErrorCategoryFramework,
	"TOOL_EXECUTION_ERROR":                 ErrorCategoryExecution,
	"TOOL_NOT_FOUND_ERROR":                 ErrorCategoryValidation,
	"AGENT_TEAM_EXECUTION_ERROR":           ErrorCategoryExecution,
	"STORE_GRAPH_BACKEND_ALREADY_EXISTS":   ErrorCategoryValidation,
	"STORE_GRAPH_PROTOCOL_NOT_IMPLEMENTED": ErrorCategoryValidation,
	"STORE_GRAPH_BACKEND_NOT_FOUND":        ErrorCategoryValidation,
	"AGENT_RL_PROXY_SERVER_START_FAILED":   ErrorCategoryFramework,
	"AGENT_RL_PROCESSOR_NOT_FOUND":         ErrorCategoryValidation,
	"AGENT_RL_REWARD_NOT_FOUND":            ErrorCategoryValidation,
	"COMMON_ENCRYPTION_ERROR":              ErrorCategoryFramework,
	"COMMON_DECRYPTION_ERROR":              ErrorCategoryFramework,
}

// keywordRules 关键字匹配规则（按优先级排列）。
// 对应 Python: KEYWORD_RULES
var keywordRules = []keywordRule{
	// Validation 语义
	{keywords: []string{"INVALID", "VALIDATE", "NOT_SUPPORTED", "PARAM", "MISSING", "DUPLICATED"}, category: ErrorCategoryValidation},
	{keywords: []string{"CONFIG", "SCHEMA", "FORMAT", "TEMPLATE"}, category: ErrorCategoryValidation},
	// Framework 语义
	{keywords: []string{"INIT", "CONNECT", "SERVICE", "QUEUE", "PROVIDER"}, category: ErrorCategoryFramework},
	{keywords: []string{"CALL", "INVOKE_LLM", "MODEL", "REMOTE"}, category: ErrorCategoryFramework},
	// Execution 语义
	{keywords: []string{"TIMEOUT", "EXECUTE", "EXECUTION", "RUNTIME", "PROCESS", "STREAM", "RESPONSE"}, category: ErrorCategoryExecution},
}

// rangeRules 数值区间匹配规则。
// 对应 Python: RANGE_RULES
var rangeRules = []rangeRule{
	{start: 100000, end: 119999, category: ErrorCategoryExecution}, // Workflow + Component 域
	{start: 120000, end: 139999, category: ErrorCategoryExecution}, // Agent + Runner 域
	{start: 140000, end: 149999, category: ErrorCategoryExecution}, // DevTools + Graph 域
	{start: 150000, end: 159999, category: ErrorCategoryExecution}, // Context + Retrieval + Memory 域
	{start: 160000, end: 179999, category: ErrorCategoryExecution}, // Toolchain 域
	{start: 180000, end: 189999, category: ErrorCategoryFramework}, // Foundation 域
	{start: 190000, end: 198999, category: ErrorCategoryExecution}, // Session 域
	{start: 199000, end: 199999, category: ErrorCategoryExecution}, // SysOperation 域
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveCategory 根据 StatusCode 解析其 ErrorCategory。
//
// 三级优先级：
//  1. MANUAL_OVERRIDES — 手动覆盖表
//  2. KEYWORD_RULES — 按 StatusCode.Name 关键字匹配
//  3. RANGE_RULES — 按 StatusCode.Code 数值区间
//  4. 兜底 ExecutionError
//
// 对应 Python: resolve_exception_class()
func ResolveCategory(status StatusCode) ErrorCategory {
	name := status.Name()
	code := status.Code()

	// 1. 手动覆盖
	if category, ok := manualOverrides[name]; ok {
		return category
	}

	// 2. 关键字匹配
	if category := matchKeyword(name); category >= 0 {
		return category
	}

	// 3. 数值区间匹配
	if category := matchRange(code); category >= 0 {
		return category
	}

	// 4. 兜底 ExecutionError
	return ErrorCategoryExecution
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// matchKeyword 按 StatusCode.Name 关键字匹配 ErrorCategory。
func matchKeyword(name string) ErrorCategory {
	upper := strings.ToUpper(name)
	for _, rule := range keywordRules {
		for _, kw := range rule.keywords {
			if strings.Contains(upper, kw) {
				return rule.category
			}
		}
	}
	return -1
}

// matchRange 按 StatusCode.Code 数值区间匹配 ErrorCategory。
func matchRange(code int) ErrorCategory {
	for _, rule := range rangeRules {
		if code >= rule.start && code <= rule.end {
			return rule.category
		}
	}
	return -1
}
