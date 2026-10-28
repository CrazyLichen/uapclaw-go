package query_rewriter

import (
	"encoding/json"
	"strings"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// fillTemplate 只替换模板中显式 {key} 占位符，其他花括号不动
// （JSON 示例不触发 str.format KeyError，Python _fill_template）。
func fillTemplate(template string, kv map[string]string) string {
	out := template
	for key, value := range kv {
		out = strings.ReplaceAll(out, "{"+key+"}", value)
	}
	return out
}

// extractJSON 提取 LLM 输出中首个 { 到最后 } 之间的 JSON 文本
// （Python _extract_json）。
func extractJSON(modelOutput string) string {
	start := strings.IndexByte(modelOutput, '{')
	end := strings.LastIndexByte(modelOutput, '}')
	if start == -1 || end == -1 || end <= start {
		logger.Debug(logComponent).Msg("No JSON object found in model output")
		return ""
	}
	logger.Debug(logComponent).Msg("JSON object from output extracted")
	return modelOutput[start : end+1]
}

// parseLLMJSON 解析 LLM 输出 JSON 字符串：先 json.Unmarshal，
// 失败再经 json-repair 修复重试；返回 dict 与是否成功，不返回 error
// （Python _parse_llm_json 的 None 语义）。
//
// 对齐要点：loads 成功但根非 dict（数组/null 字面量）时 Python 直接
// return None 不再尝试 repair；Go null 进 map Unmarshal 成功但 out 为
// nil，nil 守卫即该语义（"null" 测试用例锁定）。
func parseLLMJSON(jsonStr string) (map[string]any, bool) {
	if strings.TrimSpace(jsonStr) == "" {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &out); err == nil && out != nil {
		return out, true
	}
	// json-repair 修复后重试（Python repair_json 分支）
	result, err := jsonrepair.RepairWithOptions(jsonStr, jsonrepair.RepairOptions{})
	if err != nil {
		return nil, false
	}
	if m, ok := result.Value.(map[string]any); ok {
		logger.Debug(logComponent).Msg("JSON parsed after repair_json")
		return m, true
	}
	return nil, false
}
