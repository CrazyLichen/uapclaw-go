package openai

import "strings"

// ──────────────────────────── 结构体 ────────────────────────────

// ModelParamRule 按模型名注入 extra_body 字段的规则。
//
// 上游是类属性 _MODEL_PARAM_RULES，支持子类扩展/替换；Go 无继承，改为
// 「规则集作为参数传入」，语义等价（测试用不同规则集表达子类场景）。
// Python: openai_model_client.py:80-85 ModelParamRule
type ModelParamRule struct {
	// Name 规则名（日志与排错用）
	Name string
	// Predicate 命中判定
	Predicate func(model string) bool
	// ExtraBodyFields 命中后合并进 extra_body 的字段
	ExtraBodyFields map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// defaultModelParamRules 默认规则集（上游只有 MiniMax 一条）。
// Python: openai_model_client.py:87-93 _DEFAULT_MODEL_PARAM_RULES
var defaultModelParamRules = []ModelParamRule{
	{
		Name: "minimax_reasoning_split",
		// 大小写敏感：上游是 m.startswith("MiniMax-M")，小写 minimax-m3 不匹配。
		// Python: openai_model_client.py:90 predicate=lambda m: m.startswith("MiniMax-M")
		Predicate:       func(m string) bool { return strings.HasPrefix(m, "MiniMax-M") },
		ExtraBodyFields: map[string]any{"reasoning_split": true},
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyModelSpecificParams 按模型名把规则的 extra_body 字段就地合并进 params。
//
// 已有键保留；多条规则命中时后者覆盖前者同名键；fields 为空的规则跳过。
//
// Python: openai_model_client.py:486-503 _apply_model_specific_params
func ApplyModelSpecificParams(rules []ModelParamRule, model string, params map[string]any) {
	if model == "" {
		return
	}
	for _, rule := range rules {
		if rule.Predicate == nil || !rule.Predicate(model) || len(rule.ExtraBodyFields) == 0 {
			continue
		}
		extraBody, _ := params["extra_body"].(map[string]any)
		if extraBody == nil {
			extraBody = make(map[string]any, len(rule.ExtraBodyFields))
		}
		for k, v := range rule.ExtraBodyFields {
			extraBody[k] = v
		}
		params["extra_body"] = extraBody
	}
}
