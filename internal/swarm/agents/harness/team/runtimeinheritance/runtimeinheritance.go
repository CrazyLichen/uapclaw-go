// Package runtimeinheritance 提供 team_runtime_inheritance.py 中被 swarm
// config_specs 消费的两个纯配置函数。层级说明（偏差 D-15.16-e10 ④）：
// Python 的 team_runtime_inheritance.py 是 harness/team 层独立文件；Go 既有
// team 包因 team_manager_spec 反向依赖 swarm 包形成环，故将本批消费的两个
// 自包含符号抽到本包消环；该文件的其余符号仍留在 Go harness/team 包
// （既有漂移，随此偏差登记）。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/team/team_runtime_inheritance.py
// （get_context_engine_enabled :525-537 / resolve_model_config :395-448）
package runtimeinheritance

import "strings"

// GetContextEngineEnabled 检查上下文压缩是否启用。
// Python: get_context_engine_enabled (team_runtime_inheritance.py:525-537)
//
// 读取 react.context_engine_config.enabled，默认 true。
func GetContextEngineEnabled(config map[string]any) bool {
	if config == nil {
		return true
	}
	react, _ := config["react"].(map[string]any)
	if react != nil {
		ctxCfg, _ := react["context_engine_config"].(map[string]any)
		if ctxCfg != nil {
			if b, ok := ctxCfg["enabled"].(bool); ok {
				return b
			}
		}
	}
	return true
}

// ResolveModelConfig 从配置字典解析 model 相关参数。
// Python: resolve_model_config (team_runtime_inheritance.py:395-448)
//
// 优先 models.defaults 列表 is_default=true 条目 → 第一个条目 →
// models.default 单对象 → react 段回退。返回浅拷贝（对齐 Python .copy()）。
func ResolveModelConfig(config map[string]any) (mcc map[string]any, mco map[string]any, modelName string) {
	if config == nil {
		return map[string]any{}, map[string]any{}, "gpt-4"
	}
	models, _ := config["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}

	// 优先从 models.defaults 列表取 is_default=true 的条目
	if defaults, ok := models["defaults"].([]any); ok && len(defaults) > 0 {
		for _, item := range defaults {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if b, ok := entry["is_default"].(bool); ok && b {
				mcc = copyMapShallow(entry["model_client_config"])
				mco = copyMapShallow(entry["model_config_obj"])
				if name := mapStrField(mcc, "model_name"); name != "" {
					return mcc, mco, name
				}
			}
		}
		// 无 is_default=true 时取第一个
		if first, ok := defaults[0].(map[string]any); ok {
			mcc = copyMapShallow(first["model_client_config"])
			mco = copyMapShallow(first["model_config_obj"])
			if name := mapStrField(mcc, "model_name"); name != "" {
				return mcc, mco, name
			}
		}
	}

	// 回退到旧格式：models.default + react 段
	defaultModelConfig, _ := models["default"].(map[string]any)
	if defaultModelConfig == nil {
		defaultModelConfig = map[string]any{}
	}
	reactConfig, _ := config["react"].(map[string]any)
	if reactConfig == nil {
		reactConfig = map[string]any{}
	}

	modelClientConfig, _ := defaultModelConfig["model_client_config"].(map[string]any)
	if modelClientConfig == nil {
		modelClientConfig, _ = reactConfig["model_client_config"].(map[string]any)
	}
	if modelClientConfig == nil {
		modelClientConfig = map[string]any{}
	}

	modelName = mapStrField(modelClientConfig, "model_name")
	if modelName == "" {
		modelName = mapStrField(reactConfig, "model_name")
	}
	if modelName == "" {
		modelName = "gpt-4"
	}

	modelConfigObj, _ := defaultModelConfig["model_config_obj"].(map[string]any)
	if modelConfigObj == nil {
		modelConfigObj, _ = reactConfig["model_config_obj"].(map[string]any)
	}
	if modelConfigObj == nil {
		modelConfigObj = map[string]any{}
	}

	return modelClientConfig, modelConfigObj, modelName
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// copyMapShallow 浅拷贝 map（对齐 Python .copy()）。
func copyMapShallow(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		out[k] = val
	}
	return out
}

// mapStrField 读 map 的字符串字段。
func mapStrField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
