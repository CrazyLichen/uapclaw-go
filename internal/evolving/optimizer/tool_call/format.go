package tool_call

import (
	"encoding/json"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseJSON 从 LLM 输出中提取 JSON。
// 支持带 header 查找和兜底 eval 解析。
//
// 对齐 Python: parse_json(output, header=None)
//
//  1. 如果指定 header，先尝试查找 '{"<header>":' 或 '{\n"<header>":'
//  2. 否则查找 '{\n' 或 '{'
//  3. 提取第一个 '{' 到最后一个 '}' 之间的内容
//  4. 尝试 json.Unmarshal，失败时尝试单引号→双引号修复
func ParseJSON(output string, header ...string) map[string]any {
	// 对齐 Python: json_idx = -1
	jsonIdx := -1

	if len(header) > 0 && header[0] != "" {
		// 对齐 Python: json_idx = output.find(f'{{"{header}":')
		jsonIdx = strings.Index(output, `{"`+header[0]+`":`)
		if jsonIdx == -1 {
			// 对齐 Python: json_idx = output.find(f'{{\n"{header}":')
			jsonIdx = strings.Index(output, "{\n\""+header[0]+"\":")
		}
	}

	if jsonIdx == -1 {
		// 对齐 Python: json_idx = output.find('{\n')
		jsonIdx = strings.Index(output, "{\n")
	}
	if jsonIdx == -1 {
		// 对齐 Python: json_idx = output.find('{')
		jsonIdx = strings.Index(output, "{")
	}

	// 对齐 Python: json_end_idx = output.rfind('}')
	jsonEndIdx := strings.LastIndex(output, "}")
	if jsonEndIdx != -1 {
		// 对齐 Python: json_end_idx = json_end_idx + 1
		jsonEndIdx++
	}

	if jsonIdx == -1 || jsonEndIdx == -1 || jsonIdx >= jsonEndIdx {
		return map[string]any{}
	}

	// 对齐 Python: output = output[json_idx:json_end_idx].strip()
	extracted := strings.TrimSpace(output[jsonIdx:jsonEndIdx])

	var result map[string]any
	// 对齐 Python: output_json = json.loads(output)
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		// 对齐 Python: output_json = ast.literal_eval(output)
		// 常见问题：单引号 → 双引号
		fixed := strings.ReplaceAll(extracted, "'", `"`)
		if jsonErr := json.Unmarshal([]byte(fixed), &result); jsonErr != nil {
			return map[string]any{}
		}
	}
	return result
}

// FormatPromptLlama 格式化 Llama 风格提示词。
// 当前实现为直接拼接 system + user prompt（对齐 Python）。
//
// 对齐 Python: format_prompt_llama(system_prompt, user_prompt)
//
//	return system_prompt + user_prompt
func FormatPromptLlama(systemPrompt, userPrompt string) string {
	return systemPrompt + userPrompt
}

// ──────────────────────────── 非导出函数 ────────────────────────────
