// 思考参数注入与请求参数构建、清理。
// 对应 Python 代码：gateway/common/reasoning_injector.py:1-172（无双副本，gateway 侧为唯一源）。
// 已知偏差 E-2（spec §7）：apply_provider_compat_patches 不移植——Python 运行时给 core
// 打补丁；Go 23.3 profiles 为静态表，已覆盖同等语义。
package common

import "strings"

// ──────────────────────────── 导出函数 ────────────────────────────

// InjectReasoningParams 把 reasoning_level 注入运行时模型配置。
// Python: reasoning_injector.py:71 inject_reasoning_params。
// 步骤顺序照抄：①转 dict ②归一档位 ③拷贝并 pop reasoning_level ④采样覆盖 ⑤无档位短路返回 ⑥注入 reasoning 字段。
func InjectReasoningParams(modelClientConfig map[string]any, modelConfigObj map[string]any) map[string]any {
	// Python: reasoning_injector.py:76 _model_config_to_dict（dict 输入直接浅拷贝）
	modelConfigDict := copyMap(modelConfigObj)
	// Python: reasoning_injector.py:77 归一档位
	level := NormalizeReasoningLevel(asString(modelConfigDict["reasoning_level"]))
	// Python: reasoning_injector.py:78 拷贝运行时配置并 pop 内部提示 reasoning_level
	runtimeModelConfig := copyMap(modelConfigDict)
	delete(runtimeModelConfig, "reasoning_level")
	// Python: reasoning_injector.py:79-84 采样覆盖（api_base 或 base_url，model_name 或 model）
	override := ResolveSamplingOverride(
		firstNonEmpty(asString(modelClientConfig["api_base"]), asString(modelClientConfig["base_url"])),
		firstNonEmpty(asString(modelClientConfig["model_name"]), asString(modelClientConfig["model"])),
	)
	if override != nil {
		for k, v := range override {
			runtimeModelConfig[k] = v
		}
	}
	// Python: reasoning_injector.py:85-86 无档位短路
	if level == "" {
		return runtimeModelConfig
	}
	// Python: reasoning_injector.py:87 注入厂商中立 reasoning 配置
	runtimeModelConfig["reasoning"] = ReasoningConfigForLevel(level)
	return runtimeModelConfig
}

// BuildReasoningModelRequestKwargs 构建 core 模型请求参数。
// Python: reasoning_injector.py:145 build_reasoning_model_request_kwargs。
// 共享模型构建路径（AgentServer、Gateway 校验、TUI 校验、AgentOS、team models 均走此函数）。
func BuildReasoningModelRequestKwargs(modelClientConfig map[string]any, modelConfigObj map[string]any, modelName string) map[string]any {
	// Python: reasoning_injector.py:154 apply_provider_compat_patches()——偏差 E-2 不移植。
	effectiveModelName := ResolveModelName(modelName, modelConfigObj)
	reasoningClientConfig := copyMap(modelClientConfig)
	if effectiveModelName != "" {
		reasoningClientConfig["model_name"] = effectiveModelName
	}
	runtimeModelConfig := InjectReasoningParams(reasoningClientConfig, modelConfigObj)
	return buildModelRequestKwargs(effectiveModelName, runtimeModelConfig)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildModelRequestKwargs 构建最终请求 kwargs 并清理内部字段。
// Python: reasoning_injector.py:91 _build_model_request_kwargs。
func buildModelRequestKwargs(modelName string, modelConfigDict map[string]any) map[string]any {
	requestKwargs := copyMap(modelConfigDict)
	// Python: reasoning_injector.py:97 _source 判 agentos
	isAgentos := asString(requestKwargs["_source"]) == "agentos"
	// Python: reasoning_injector.py:98-105 旧版 AgentOS 用 max_tokens 表示上下文窗口，
	// 迁移到 context_window，避免误限输出；新旧并存以新字段为准；都未配不注入。
	if isAgentos {
		legacyContextWindow, had := requestKwargs["max_tokens"]
		delete(requestKwargs, "max_tokens")
		if _, has := requestKwargs["context_window"]; !has && had && legacyContextWindow != nil {
			requestKwargs["context_window"] = legacyContextWindow
		}
	}
	delete(requestKwargs, "model")
	delete(requestKwargs, "model_name")
	delete(requestKwargs, "reasoning_level")
	// Python: reasoning_injector.py:109-114 _source 是内部标记，不进 core 的
	// ModelRequestConfig——SDK 调用不认该 kwarg 会抛 unexpected keyword argument。
	delete(requestKwargs, "_source")
	// Python: reasoning_injector.py:115-140 context_window 处理。
	// 偏差注记：Python 运行时探测 core 是否已有正式字段（core_has_context_window_field，
	// :15-40）决定出口是否 pop。Go 侧 ModelRequestConfig.ContextWindow 正式字段已存在
	// （internal/agentcore/foundation/llm/schema/config.go:214，测试固化不外发语义），
	// 故取"已加字段"分支：不 pop，仅做归一清理。
	if raw, ok := requestKwargs["context_window"]; ok {
		normalized := normalizeContextWindow(raw)
		if normalized == nil {
			delete(requestKwargs, "context_window")
		} else {
			requestKwargs["context_window"] = normalized
		}
	}
	// Python: reasoning_injector.py:141 model 回填为有效模型名
	requestKwargs["model"] = ResolveModelName(modelName, modelConfigDict)
	return requestKwargs
}

// normalizeContextWindow 归一 context_window：bool/负数/非法 → 删除（nil）。
// Python: reasoning_injector.py:127-140。
func normalizeContextWindow(raw any) any {
	if raw == nil {
		return nil
	}
	// Python: reasoning_injector.py:131 bool 显式排除
	if _, isBool := raw.(bool); isBool {
		return nil
	}
	switch v := raw.(type) {
	case int:
		if v <= 0 {
			return nil
		}
		return v
	case int64:
		if v <= 0 {
			return nil
		}
		return int(v)
	case float64:
		if v <= 0 {
			return nil
		}
		return int(v)
	case jsonNumber:
		// YAML 数字经 map[string]any 解码常见 json.Number
		n, err := v.Int64()
		if err != nil || n <= 0 {
			return nil
		}
		return int(n)
	default:
		return nil
	}
}

// ResolveModelName 解析有效模型名：显式名优先，否则回 model_config_obj 的 model/model_name。
// Python: reasoning_injector.py:55 _resolve_model_name。
func ResolveModelName(modelName string, modelConfigObj map[string]any) string {
	if name := strings.TrimSpace(modelName); name != "" {
		return name
	}
	if modelConfigObj != nil {
		configured := firstNonEmpty(asString(modelConfigObj["model"]), asString(modelConfigObj["model_name"]))
		if name := strings.TrimSpace(configured); name != "" {
			return name
		}
	}
	return ""
}
