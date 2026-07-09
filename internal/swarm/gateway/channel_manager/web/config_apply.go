package web

import (
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConfigBadRequest 配置请求参数错误。
// 对齐 Python: _ConfigBadRequest(ValueError)。
type ConfigBadRequest struct {
	// Message 错误信息
	Message string
}

// ConfigInternalError 配置内部错误。
// 对齐 Python: _ConfigInternalError(RuntimeError)。
type ConfigInternalError struct {
	// Message 错误信息
	Message string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponentConfigApply 本文件日志组件
const logComponentConfigApply = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// configYAMLKeys 需要写入 config.yaml 的配置键集合。
// 对齐 Python: _CONFIG_YAML_KEYS (app_web_handlers.py L371-377)。
var configYAMLKeys = map[string]bool{
	"context_engine_enabled":       true,
	"kv_cache_affinity_enabled":    true,
	"permissions_enabled":          true,
	"memory_forbidden_enabled":     true,
	"memory_forbidden_description": true,
}

// availableModelProviders 可用的模型服务商列表。
// 对齐 Python: [provider.value for provider in ProviderType]。
var availableModelProviders = []string{
	"OpenAI",
	"OpenRouter",
	"SiliconFlow",
	"DashScope",
	"DeepSeek",
	"InferenceAffinity",
	"intelli_router",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *ConfigBadRequest) Error() string { return e.Message }

// Error 实现 error 接口。
func (e *ConfigInternalError) Error() string { return e.Message }

// ApplyConfigPayload 将 config.set 风格的参数应用到 .env 和 config.yaml，不触发重载。
// 对齐 Python: _apply_config_payload (app_web_handlers.py L701-761)。
//
// 返回值：
//   - envUpdates: 本次变更的环境变量增量
//   - yamlUpdated: 本次变更的 YAML 键列表
func ApplyConfigPayload(params map[string]any) (envUpdates map[string]string, yamlUpdated []string, err error) {
	// TODO(⤵️ 加密): 对齐 Python _encrypt_config_params，加密 api_key/token 字段
	envUpdates = make(map[string]string)
	yamlUpdated = make([]string, 0)

	// 环境变量映射处理
	for paramKey, envKey := range configEnvMap {
		val, ok := params[paramKey]
		if !ok {
			continue
		}
		// provider 校验（对齐 Python available_model_providers 校验，仅非空值校验）
		if strings.HasSuffix(paramKey, "_provider") {
			strVal := strings.TrimSpace(fmt.Sprintf("%v", val))
			if strVal != "" && !isAvailableProvider(strVal) {
				return nil, nil, &ConfigBadRequest{
					Message: fmt.Sprintf("Model provider must in: %v", availableModelProviders),
				}
			}
		}
		if val == nil {
			envUpdates[envKey] = ""
		} else {
			envUpdates[envKey] = strings.TrimSpace(fmt.Sprintf("%v", val))
		}
	}

	// agents/team 写入 config.yaml
	if _, hasAgents := params["agents"]; hasAgents {
		if err := replaceTeamsInConfig(params); err != nil {
			logger.Warn(logComponentConfigApply).
				Err(err).
				Msg("写回 modes.team 失败")
			return nil, nil, &ConfigInternalError{Message: "failed to update modes.team"}
		}
		yamlUpdated = append(yamlUpdated, "modes.team")
	} else if _, hasTeam := params["team"]; hasTeam {
		if err := replaceTeamsInConfig(params); err != nil {
			logger.Warn(logComponentConfigApply).
				Err(err).
				Msg("写回 modes.team 失败")
			return nil, nil, &ConfigInternalError{Message: "failed to update modes.team"}
		}
		yamlUpdated = append(yamlUpdated, "modes.team")
	}

	// YAML 键写入 config.yaml
	for paramKey := range configYAMLKeys {
		val, ok := params[paramKey]
		if !ok {
			continue
		}
		if err := updateYAMLKeyInConfig(paramKey, val); err != nil {
			logger.Warn(logComponentConfigApply).
				Str("key", paramKey).
				Err(err).
				Msg("写回 config.yaml 失败")
		} else {
			yamlUpdated = append(yamlUpdated, paramKey)
		}
	}

	// 设置环境变量 + 持久化 .env
	for envKey, value := range envUpdates {
		_ = os.Setenv(envKey, value)
	}
	if len(envUpdates) > 0 {
		if err := persistEnvUpdates(envUpdates); err != nil {
			logger.Warn(logComponentConfigApply).
				Err(err).
				Msg("持久化 .env 文件失败")
		} else {
			logger.Info(logComponentConfigApply).
				Strs("keys", sortedKeys(envUpdates)).
				Msg("已更新 .env")
		}
	}
	if len(yamlUpdated) > 0 {
		logger.Info(logComponentConfigApply).
			Strs("keys", yamlUpdated).
			Msg("已更新 config.yaml")
	}

	return envUpdates, yamlUpdated, nil
}

// NotifyConfigSavedOnce 在所有文件写入完成后触发一次热重载。
// 对齐 Python: _notify_config_saved_once (app_web_handlers.py L763-782)。
func NotifyConfigSavedOnce(
	onConfigSaved OnConfigSavedFunc,
	envUpdates map[string]string,
	yamlUpdated []string,
	force bool,
) {
	if !force && len(envUpdates) == 0 && len(yamlUpdated) == 0 {
		return
	}
	if onConfigSaved == nil {
		return
	}

	// 获取最新配置快照
	configPayload := getConfigSnapshot()

	// 合并变更键（对齐 Python: set(env_updates.keys()) | set(yaml_updated)）
	updatedKeys := make([]string, 0, len(envUpdates)+len(yamlUpdated))
	for k := range envUpdates {
		updatedKeys = append(updatedKeys, k)
	}
	updatedKeys = append(updatedKeys, yamlUpdated...)

	// envUpdates 转 map[string]any
	envUpdatesAny := make(map[string]any, len(envUpdates))
	for k, v := range envUpdates {
		envUpdatesAny[k] = v
	}

	// 调用回调
	if err := onConfigSaved(updatedKeys, envUpdatesAny, configPayload); err != nil {
		logger.Warn(logComponentConfigApply).
			Err(err).
			Msg("onConfigSaved 回调失败")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isAvailableProvider 检查 provider 是否在可用列表中。
func isAvailableProvider(provider string) bool {
	for _, p := range availableModelProviders {
		if p == provider {
			return true
		}
	}
	return false
}

// updateYAMLKeyInConfig 将单个 YAML 键写入 config.yaml。
// 对齐 Python: update_context_engine_enabled_in_config 等系列函数。
func updateYAMLKeyInConfig(key string, value any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}

	// 加载当前配置
	_, err = cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	parsed := isTruthy(value)

	switch key {
	case "context_engine_enabled":
		return cfg.Set("react.context_engine_config.enabled", parsed)
	case "kv_cache_affinity_enabled":
		return cfg.Set("react.context_engine_config.enable_kv_cache_release", parsed)
	case "permissions_enabled":
		return cfg.Set("permissions.enabled", parsed)
	case "memory_forbidden_enabled":
		return cfg.Set("memory.forbidden_memory_definition.enabled", parsed)
	case "memory_forbidden_description":
		descVal := strings.TrimSpace(fmt.Sprintf("%v", value))
		// 对齐 Python: update_memory_forbidden_description_in_config
		// description 是多语言字典，此处用默认语言 zh 覆盖
		return cfg.Set("memory.forbidden_memory_definition.description.zh", descVal)
	default:
		return fmt.Errorf("未知的 YAML 配置键: %s", key)
	}
}

// replaceTeamsInConfig 替换 config.yaml 中的 modes.team 配置。
// 对齐 Python: replace_teams_in_config (common/config.py L989-1038)。
func replaceTeamsInConfig(params map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}

	_, err = cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// team 数据
	teamData, hasTeam := params["team"]
	if hasTeam {
		// 空数组：删除 modes.team
		if teamList, ok := teamData.([]any); ok && len(teamList) == 0 {
			// TODO(⤵️ 配置): 实现 cfg.Delete("modes.team") 或等价逻辑
			logger.Info(logComponentConfigApply).Msg("modes.team 空数组，待实现删除逻辑")
			return nil
		}
		// 非空数组：写入 modes.team
		if err := cfg.Set("modes.team", teamData); err != nil {
			return fmt.Errorf("写入 modes.team 失败: %w", err)
		}
	}

	// agents 数据写入 web_config_panel.agent_team_agents
	agentsData, hasAgents := params["agents"]
	if hasAgents {
		if err := cfg.Set("web_config_panel.agent_team_agents", agentsData); err != nil {
			return fmt.Errorf("写入 web_config_panel.agent_team_agents 失败: %w", err)
		}
	}

	return nil
}

// updateDefaultModelsInConfig 将默认模型列表写入 config.yaml 的 models.defaults 段。
// 对齐 Python: update_default_models_in_config (common/config.py L734-742)。
func updateDefaultModelsInConfig(models []map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}

	_, err = cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if err := cfg.Set("models.defaults", models); err != nil {
		return fmt.Errorf("写入 models.defaults 失败: %w", err)
	}

	return nil
}

// buildModelsDefaultsFromFrontend 从前端 models 参数构建 models.defaults 列表。
// 对齐 Python: _build_models_defaults_from_frontend (app_web_handlers.py L784-868)。
func buildModelsDefaultsFromFrontend(rawModels any) ([]map[string]any, error) {
	modelsList, ok := rawModels.([]any)
	if !ok || len(modelsList) == 0 {
		return nil, &ConfigBadRequest{Message: "models must be a non-empty list"}
	}

	parsed := make([]map[string]any, 0, len(modelsList))
	aliasesSeen := make(map[string]int)

	for idx, item := range modelsList {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return nil, &ConfigBadRequest{Message: fmt.Sprintf("models[%d] must be object", idx)}
		}

		modelName := strings.TrimSpace(fmt.Sprintf("%v", itemMap["model_name"]))
		if modelName == "" || modelName == "<nil>" {
			return nil, &ConfigBadRequest{Message: fmt.Sprintf("models[%d].model_name is required", idx)}
		}

		apiKey := strings.TrimSpace(fmt.Sprintf("%v", itemMap["api_key"]))
		originIndex := itemMap["origin_index"] // 可为 nil
		if (apiKey == "" || apiKey == "<nil>") && originIndex == nil {
			return nil, &ConfigBadRequest{Message: fmt.Sprintf("models[%d].api_key is required", idx)}
		}

		apiBase := strings.TrimSpace(fmt.Sprintf("%v", itemMap["api_base"]))
		modelProvider := ""
		if mp, ok := itemMap["model_provider"]; ok && mp != nil {
			modelProvider = strings.TrimSpace(fmt.Sprintf("%v", mp))
		}
		// provider 校验：仅当值非空时才检查（对齐 Python 逻辑）
		if modelProvider != "" && !isAvailableProvider(modelProvider) {
			return nil, &ConfigBadRequest{
				Message: fmt.Sprintf("models[%d].model_provider must be one of: %v", idx, availableModelProviders),
			}
		}

		temperature := 0.95
		if t, ok := itemMap["temperature"]; ok {
			if f, err := parseFloat(t); err == nil {
				temperature = f
			}
		}

		timeout := 1800
		if t, ok := itemMap["timeout"]; ok {
			if i, err := parseInt(t); err == nil {
				timeout = i
			}
		}

		alias := ""
		if a, ok := itemMap["alias"]; ok && a != nil {
			alias = strings.TrimSpace(fmt.Sprintf("%v", a))
		}
		if alias != "" {
			if prevIdx, exists := aliasesSeen[alias]; exists {
				return nil, &ConfigBadRequest{
					Message: fmt.Sprintf("Alias '%s' is used by both models[%d] and models[%d]", alias, prevIdx, idx),
				}
			}
			aliasesSeen[alias] = idx
		}

		isDefault := false
		if d, ok := itemMap["is_default"]; ok {
			isDefault = isTruthy(d)
		}

		entry := map[string]any{
			"model_client_config": map[string]any{
				"model_name":      modelName,
				"api_base":        apiBase,
				"api_key":         apiKey,
				"client_provider": modelProvider,
				"timeout":         timeout,
			},
			"model_config_obj": map[string]any{
				"temperature": temperature,
			},
			"is_default": isDefault,
			"alias":      alias,
		}
		if originIndex != nil {
			entry["origin_index"] = originIndex
		}

		parsed = append(parsed, entry)
	}

	// alias 与其他条目的 model_name 冲突校验
	for i, p := range parsed {
		a, _ := p["alias"].(string)
		if a == "" {
			continue
		}
		for j, q := range parsed {
			if i == j {
				continue
			}
			if q["model_client_config"] != nil {
				if mcc, ok := q["model_client_config"].(map[string]any); ok {
					if mn, _ := mcc["model_name"].(string); mn == a {
						return nil, &ConfigBadRequest{
							Message: fmt.Sprintf("Alias '%s' on models[%d] conflicts with model_name on models[%d]", a, i, j),
						}
					}
				}
			}
		}
	}

	return parsed, nil
}

// getConfigSnapshot 获取当前配置快照。
// 对齐 Python: get_config() / get_config_raw()。
func getConfigSnapshot() map[string]any {
	cfg, err := config.New("")
	if err != nil {
		return make(map[string]any)
	}
	data, err := cfg.Load()
	if err != nil {
		return make(map[string]any)
	}
	return data
}

// isTruthy 判断值是否为真（对齐 Python bool 语义）。
func isTruthy(val any) bool {
	switch v := val.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		return lower == "true" || lower == "1" || lower == "yes"
	case int:
		return v != 0
	case float64:
		return v != 0
	default:
		return fmt.Sprintf("%v", val) != ""
	}
}

// parseFloat 安全解析浮点数。
func parseFloat(val any) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case string:
		var f float64
		_, err := fmt.Sscanf(v, "%f", &f)
		return f, err
	default:
		return 0, fmt.Errorf("无法解析为浮点数: %v", val)
	}
}

// parseInt 安全解析整数。
func parseInt(val any) (int, error) {
	switch v := val.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case string:
		var i int
		_, err := fmt.Sscanf(v, "%d", &i)
		return i, err
	default:
		return 0, fmt.Errorf("无法解析为整数: %v", val)
	}
}
