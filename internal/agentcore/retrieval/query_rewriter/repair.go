package query_rewriter

import (
	"encoding/json"
	"fmt"
	"strings"

	jsonrepair "github.com/RealAlexandreAI/json-repair"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 枚举 ────────────────────────────

// schemaFieldType schema 字段期望类型（Python str/list/dict 类型对象）。
type schemaFieldType int

const (
	// schemaFieldString 期望字符串
	schemaFieldString schemaFieldType = iota
	// schemaFieldList 期望列表
	schemaFieldList
	// schemaFieldDict 期望字典
	schemaFieldDict
)

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
//
// T-03：kv 为有序键值对切片（对齐 Python **kwargs 按声明序迭代——
// history 先替换、query 后替换，后值可包含已被前值引入的 {key} 占位符；
// map 迭代序随机会 50% 概率破坏该替换顺序语义）。
type templateKV struct {
	key   string
	value string
}

func fillTemplate(template string, kv ...templateKV) string {
	out := template
	for _, pair := range kv {
		out = strings.ReplaceAll(out, "{"+pair.key+"}", pair.value)
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

// forceString 值转字符串：dict 走 JSON 序列化（非 ASCII 不转义、
// HTML 字符不转义——T-04 utils.MarshalNoEscape 对齐 ensure_ascii=False），
// 失败 Debug 日志后回落 fmt.Sprint；其他类型 fmt.Sprint
// （Python _force_string）。
func forceString(raw any) string {
	if m, ok := raw.(map[string]any); ok {
		data, err := utils.MarshalNoEscape(m)
		if err == nil {
			return string(data)
		}
		logger.Debug(logComponent).Msg("json.dumps failed when forcing value to string, using str(raw)")
	}
	return fmt.Sprint(raw)
}

// forceList 值转列表：list 直返，其他包单元素（Python _force_list）。
func forceList(raw any) []any {
	if l, ok := raw.([]any); ok {
		return l
	}
	return []any{raw}
}

// forceJSON 值转 dict：dict 直返；str 尝试 JSON 解析（成 dict 返回 /
// 成功非 dict 包 {key: parsed} / 失败包 {key: raw}）；list/其他包 {key: raw}
// （Python _force_json）。
func forceJSON(key string, raw any) map[string]any {
	switch v := raw.(type) {
	case map[string]any:
		return v
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			if m, ok := parsed.(map[string]any); ok {
				return m
			}
			return map[string]any{key: parsed}
		}
		return map[string]any{key: raw}
	default:
		return map[string]any{key: raw}
	}
}

// schemaRepair 按 schema 校验修复 LLM 输出：类型不匹配时强制转换。
// 返回只含 schema 字段且类型匹配的 dict（Python _schema_repair）。
//
// Python 中非 dict 入参与未知类型两分支抛 155604；Go 入参已是
// map[string]any（Python 非 dict 分支编译期消解，spec §8-3），
// schemaFieldType 仅三态（"其他类型"分支同样编译期消解），error
// 无发生路径，按 Go 惯例去掉 error 返回值（spec §8-9）。
func schemaRepair(output map[string]any, outputSchema map[string]schemaFieldType) map[string]any {
	repaired := map[string]any{}

	for field, expectedType := range outputSchema {
		value, exists := output[field]
		// Python output.get(field)：缺失与 None 同走默认填充路径
		if !exists || value == nil {
			switch expectedType {
			case schemaFieldString:
				value = ""
			case schemaFieldList:
				value = []any{}
			case schemaFieldDict:
				value = map[string]any{}
			}
		}

		// typo 特判：不看 expected_type，先 list 化再逐元素三键收敛
		// （Python L155-173）
		if field == "typo" {
			if _, isList := value.([]any); !isList {
				value = forceList(value)
			}
			repairedTypo := []any{}
			for _, item := range value.([]any) {
				m, isMap := item.(map[string]any)
				if !isMap {
					m = forceJSON(field, item)
				}
				repairedItem := map[string]any{}
				for _, key := range []string{"original", "corrected", "reason"} {
					v, ok := m[key]
					if !ok || v == nil {
						v = ""
					} else if _, isStr := v.(string); !isStr {
						v = forceString(v)
					}
					repairedItem[key] = v
				}
				repairedTypo = append(repairedTypo, repairedItem)
			}
			value = repairedTypo
		}

		// 最终类型校验：不匹配走 force 三兄弟（Python L175-186）
		switch expectedType {
		case schemaFieldString:
			if _, isStr := value.(string); !isStr {
				value = forceString(value)
			}
		case schemaFieldList:
			if _, isList := value.([]any); !isList {
				value = forceList(value)
			}
		case schemaFieldDict:
			if _, isMap := value.(map[string]any); !isMap {
				value = forceJSON(field, value)
			}
		}

		repaired[field] = value
	}
	logger.Debug(logComponent).Msg("Schema check and repair done")
	return repaired
}
