package schema

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// 本文件是 gateway/common/reasoning_injector.py 的 Go 对应物。
//
// Python: gateway/common/reasoning_injector.py（172 行）
//
// 职责：模型构造的共享 kwargs 入口。被 BuildModelFromEntry、配置校验、
// 图像模态探测预热等多处共用，保证「从 models.defaults 条目造 Model」
// 只有一份规则。
//
// 偏差：上游 build_reasoning_model_request_kwargs 首行调用
// apply_provider_compat_patches()（monkey-patch 已 patch 的类）。Go 无该机制，
// 不移植（spec 偏差 E-4）；provider 方言补丁在客户端实现内部按 host 分支处理。

// ──────────────────────────── 导出函数 ────────────────────────────

// InjectReasoningParams 把 reasoning_level 翻译成 core 的 reasoning 配置。
// Python: gateway/common/reasoning_injector.py:71-88 inject_reasoning_params
//
// 三步（顺序即语义）：
//  1. level = NormalizeReasoningLevel(model_config_dict["reasoning_level"])
//  2. override = ResolveSamplingOverride(api_base || base_url, model_name || model)
//     override 非空则覆盖 runtime_model_config 的 temperature/top_p
//  3. level 为空 → 原样返回（不注入 reasoning）
//     level 非空 → runtime_model_config["reasoning"] = ReasoningConfigForLevel(level)
//
// reasoning_level 是内部提示，无论是否命中都从结果里 pop 掉。
func InjectReasoningParams(modelClientConfig, modelConfigObj map[string]any) map[string]any {
	modelConfigDict := copyStringAnyMap(modelConfigObj)
	level := NormalizeReasoningLevel(modelConfigDict["reasoning_level"])

	runtimeModelConfig := copyStringAnyMap(modelConfigDict)
	delete(runtimeModelConfig, "reasoning_level")

	override := ResolveSamplingOverride(
		firstNonEmptyString(modelClientConfig["api_base"], modelClientConfig["base_url"]),
		firstNonEmptyString(modelClientConfig["model_name"], modelClientConfig["model"]),
	)
	for key, value := range override {
		runtimeModelConfig[key] = value
	}
	if level == "" {
		return runtimeModelConfig
	}
	runtimeModelConfig["reasoning"] = ReasoningConfigForLevel(level)
	return runtimeModelConfig
}

// BuildReasoningModelRequestKwargs 共享的模型构造 kwargs 计算入口。
// Python: gateway/common/reasoning_injector.py:145-165
//
//	build_reasoning_model_request_kwargs
//
// 被 BuildModelFromEntry / 配置校验 / 图像模态探测预热等多处共用。
// 返回的 map 可直接用于构造 ModelRequestConfig（键集合即正式字段 + extra）。
func BuildReasoningModelRequestKwargs(
	modelClientConfig, modelConfigObj map[string]any, modelName string,
) map[string]any {
	effectiveModelName := resolveModelName(modelName, modelConfigObj)

	reasoningClientConfig := copyStringAnyMap(modelClientConfig)
	if effectiveModelName != "" {
		reasoningClientConfig["model_name"] = effectiveModelName
	}
	runtimeModelConfig := InjectReasoningParams(reasoningClientConfig, modelConfigObj)

	return buildModelRequestKwargs(effectiveModelName, runtimeModelConfig)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildModelRequestKwargs 归一 model_config_obj 为 core 的 kwargs 形状。
// Python: reasoning_injector.py:91-138 _build_model_request_kwargs
//
// 六步：agentos 迁移 → 清理内部键 → context_window 归一 → 回填 model。
func buildModelRequestKwargs(modelName string, modelConfigObj map[string]any) map[string]any {
	requestKwargs := copyStringAnyMap(modelConfigObj)

	// Python: :97-105 agentos 老配置用 max_tokens 表示上下文窗口，
	// core 同名字段表示最大输出，构建前先迁移，避免误限输出。
	if source, _ := requestKwargs["_source"].(string); source == "agentos" {
		legacy, hasLegacy := requestKwargs["max_tokens"]
		if _, hasNew := requestKwargs["context_window"]; !hasNew && hasLegacy {
			requestKwargs["context_window"] = legacy
		}
		delete(requestKwargs, "max_tokens")
	}

	// Python: :106-114 清理内部键，不得进 core 的 ModelRequestConfig
	delete(requestKwargs, "model")
	delete(requestKwargs, "model_name")
	delete(requestKwargs, "reasoning_level")
	delete(requestKwargs, "_source")

	// Python: :123-137 context_window 归一（bool / 非整数 / 非正数丢弃）
	//
	// Go 侧无 core_has_context_window_field() 探测（偏差 E-5）：Go 的
	// ModelRequestConfig.ContextWindow 是正式字段，永远走「保留」分支。
	if raw, ok := requestKwargs["context_window"]; ok {
		if normalized, valid := config.ParsePositiveTokens(raw); valid {
			requestKwargs["context_window"] = normalized
		} else {
			delete(requestKwargs, "context_window")
		}
	}

	// Python: :138 request_kwargs["model"] = _resolve_model_name(...)
	requestKwargs["model"] = modelName
	return requestKwargs
}

// resolveModelName 解析生效的模型名：显式参数优先，其次 mco 里的 model/model_name。
// Python: reasoning_injector.py:55-61 _resolve_model_name
func resolveModelName(modelName string, modelConfigObj map[string]any) string {
	if trimmed := strings.TrimSpace(modelName); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(firstNonEmptyString(
		modelConfigObj["model"], modelConfigObj["model_name"],
	))
}

// copyStringAnyMap 浅拷贝 map（nil 安全）。
func copyStringAnyMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}

// firstNonEmptyString 返回第一个非空字符串（非字符串视为空）。
func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
