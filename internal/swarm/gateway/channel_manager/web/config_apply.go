package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
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

// CryptoProvider 加密/解密提供者接口。
//
// 对齐 Python ExtensionRegistry.get_crypto_provider()。
type CryptoProvider interface {
	// Encrypt 加密明文
	Encrypt(plaintext string) string
	// Decrypt 解密密文
	Decrypt(ciphertext string) string
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

// ProcessFiles 处理 params 中的 files 字段，下载 URL 文件到本地 workspace。
//
// 对齐 Python _process_files (web_connect.py L189-221)。
func ProcessFiles(params map[string]any) map[string]any {
	files, ok := params["files"]
	if !ok {
		return params
	}
	filesList, ok := files.([]any)
	if !ok || len(filesList) == 0 {
		return params
	}

	workspaceDir := workspace.AgentWorkspaceDir()
	downloadedFiles := make([]any, 0, len(filesList))

	for _, fileItem := range filesList {
		fileInfo, ok := fileItem.(map[string]any)
		if !ok {
			downloadedFiles = append(downloadedFiles, fileItem)
			continue
		}

		fileURL := ""
		if u, ok := fileInfo["url"].(string); ok && u != "" {
			fileURL = u
		} else if u, ok := fileInfo["uri"].(string); ok && u != "" {
			fileURL = u
		}

		fileName := "unknown_file"
		if n, ok := fileInfo["name"].(string); ok && n != "" {
			fileName = n
		} else if n, ok := fileInfo["filename"].(string); ok && n != "" {
			fileName = n
		}

		if fileURL != "" {
			fileContent, err := downloadFile(fileURL)
			if err != nil {
				logger.Warn(logComponentConfigApply).
					Str("url", fileURL).
					Err(err).
					Msg("文件下载失败")
			} else if fileContent != nil {
				if err := os.MkdirAll(workspaceDir, 0o755); err == nil {
					filePath := filepath.Join(workspaceDir, fileName)
					if err := os.WriteFile(filePath, fileContent, 0o644); err != nil {
						logger.Warn(logComponentConfigApply).Err(err).Msg("文件保存失败")
					} else {
						fileInfo["path"] = filePath
					}
				}
			}
		}
		downloadedFiles = append(downloadedFiles, fileInfo)
	}

	params["files"] = downloadedFiles
	return params
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
	cfgData, err := cfg.Load()
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
		// 动态读取 preferred_language（对齐 Python L720-748）
		preferredLang := "zh"
		if lang, ok := cfgData["preferred_language"].(string); ok && lang != "" {
			preferredLang = lang
		}
		return cfg.Set(fmt.Sprintf("memory.forbidden_memory_definition.description.%s", preferredLang), descVal)
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
// crypto 用于加密 api_key；rawModels 中的 origin_index 用于匹配原始 YAML 条目。
func buildModelsDefaultsFromFrontend(rawModels any, crypto CryptoProvider) ([]map[string]any, error) {
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

	// 合并原始 YAML 条目（对齐 Python _merge_models_for_replace_all）
	merged := mergeModelsForReplaceAll(parsed, crypto)

	// 推断 is_default（对齐 Python _infer_is_default）
	merged = inferIsDefault(merged)

	return merged, nil
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

// processFiles 处理 params 中的 files 字段，下载 URL 文件到本地 workspace。
//
// 对齐 Python _process_files (web_connect.py L189-221)。
// 供 HandleWebSocket 内部调用，外部请使用 ProcessFiles。
func processFiles(params map[string]any) map[string]any {
	return ProcessFiles(params)
}

// downloadFile 下载指定 URL 的文件内容。
func downloadFile(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// valuesMatch 比较前端发送值与解析后的值是否相同。
// 对齐 Python _values_match (app_web_handlers.py L63-79)。
func valuesMatch(parsedVal, resolvedVal any) bool {
	// bool 比较
	if _, ok := parsedVal.(bool); ok {
		return fmt.Sprintf("%v", parsedVal) == fmt.Sprintf("%v", resolvedVal)
	}
	if _, ok := resolvedVal.(bool); ok {
		return fmt.Sprintf("%v", parsedVal) == fmt.Sprintf("%v", resolvedVal)
	}
	// nil 比较
	if parsedVal == nil && resolvedVal == nil {
		return true
	}
	// 数值比较
	pF, pErr := parseFloat(parsedVal)
	rF, rErr := parseFloat(resolvedVal)
	if pErr == nil && rErr == nil {
		return pF == rF
	}
	// 字符串比较
	pStr := ""
	if parsedVal != nil {
		pStr = fmt.Sprintf("%v", parsedVal)
	}
	rStr := ""
	if resolvedVal != nil {
		rStr = fmt.Sprintf("%v", resolvedVal)
	}
	return pStr == rStr
}

// mergeModelsForReplaceAll 用 origin_index 匹配原始 YAML 条目，保留占位符和未暴露字段，仅覆写变化字段。
// 对齐 Python _merge_models_for_replace_all (app_web_handlers.py L82-156)。
//
// 对每个前端条目：
//   - 有 origin_index 指向已有原始条目：深拷贝原始条目，仅覆写与解析快照不同的字段
//   - api_key 变更时加密，未变更时保留原始值
//   - 新条目（无 origin_index）：加密 api_key 后原样存入
func mergeModelsForReplaceAll(parsed []map[string]any, crypto CryptoProvider) []map[string]any {
	// 读取原始 YAML 中的 models.defaults
	rawModels := loadRawModelsDefaults()

	result := make([]map[string]any, 0, len(parsed))

	for _, entry := range parsed {
		originIndexVal, hasOrigin := entry["origin_index"]
		if !hasOrigin || originIndexVal == nil {
			// 新条目：加密 api_key 后原样存入
			newEntry := deepCopyMap(entry)
			if crypto != nil {
				if mcc, ok := newEntry["model_client_config"].(map[string]any); ok {
					if apiKey, ok := mcc["api_key"].(string); ok && apiKey != "" {
						mcc["api_key"] = crypto.Encrypt(apiKey)
					}
				}
			}
			result = append(result, newEntry)
			continue
		}

		// 解析 origin_index
		oi := -1
		switch v := originIndexVal.(type) {
		case int:
			oi = v
		case float64:
			oi = int(v)
		default:
			if i, err := parseInt(v); err == nil {
				oi = i
			}
		}

		if oi < 0 || oi >= len(rawModels) {
			// origin_index 越界，当作新条目处理
			newEntry := deepCopyMap(entry)
			if crypto != nil {
				if mcc, ok := newEntry["model_client_config"].(map[string]any); ok {
					if apiKey, ok := mcc["api_key"].(string); ok && apiKey != "" {
						mcc["api_key"] = crypto.Encrypt(apiKey)
					}
				}
			}
			result = append(result, newEntry)
			continue
		}

		// 深拷贝原始条目（保留占位符、custom_headers 等未暴露字段）
		merged := deepCopyMap(rawModels[oi])

		// 前端解析后的快照（即 entry 中的解析值）
		// 仅覆写与原始快照不同的字段
		if frontMCC, ok := entry["model_client_config"].(map[string]any); ok {
			if mergedMCC, ok := merged["model_client_config"].(map[string]any); ok {
				for k, parsedVal := range frontMCC {
					rawVal := mergedMCC[k]
					if !valuesMatch(parsedVal, rawVal) {
						// 字段值不同，覆写
						if k == "api_key" {
							// api_key 特殊处理：加密后写入
							if apiKeyStr, ok := parsedVal.(string); ok && apiKeyStr != "" && crypto != nil {
								mergedMCC[k] = crypto.Encrypt(apiKeyStr)
							} else {
								mergedMCC[k] = parsedVal
							}
						} else {
							mergedMCC[k] = parsedVal
						}
					}
					// 字段值相同，保留原始值（含占位符等）
				}
			}
		}

		if frontMCO, ok := entry["model_config_obj"].(map[string]any); ok {
			if mergedMCO, ok := merged["model_config_obj"].(map[string]any); ok {
				for k, parsedVal := range frontMCO {
					rawVal := mergedMCO[k]
					if !valuesMatch(parsedVal, rawVal) {
						mergedMCO[k] = parsedVal
					}
				}
			}
		}

		// 覆写顶层字段
		for _, k := range []string{"is_default", "alias"} {
			parsedVal, hasParsed := entry[k]
			rawVal, hasRaw := merged[k]
			if hasParsed {
				if !hasRaw || !valuesMatch(parsedVal, rawVal) {
					merged[k] = parsedVal
				}
			}
		}

		// 移除 origin_index（不需要持久化到 YAML）
		delete(merged, "origin_index")

		result = append(result, merged)
	}

	return result
}

// inferIsDefault 确保模型列表中恰好一个 is_default=true。
// 对齐 Python _infer_is_default。
func inferIsDefault(models []map[string]any) []map[string]any {
	if len(models) == 0 {
		return models
	}
	hasDefault := false
	for _, m := range models {
		if isTruthy(m["is_default"]) {
			if hasDefault {
				// 后续 true 设为 false，确保恰好一个
				m["is_default"] = false
			} else {
				hasDefault = true
			}
		}
	}
	if !hasDefault {
		// 无 true 时，设第一个为 true
		models[0]["is_default"] = true
	}
	return models
}

// loadRawModelsDefaults 从 config.yaml 加载原始 models.defaults 列表。
func loadRawModelsDefaults() []map[string]any {
	cfgData := getConfigSnapshot()
	rawVal := getConfigAny(cfgData, "models.defaults", nil)
	if rawVal == nil {
		return nil
	}
	rawList, ok := rawVal.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(rawList))
	for _, item := range rawList {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// deepCopyMap 深拷贝 map[string]any。
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[k] = deepCopyMap(val)
		case []any:
			dst[k] = deepCopySlice(val)
		default:
			dst[k] = v
		}
	}
	return dst
}

// deepCopySlice 深拷贝 []any。
func deepCopySlice(src []any) []any {
	if src == nil {
		return nil
	}
	dst := make([]any, len(src))
	for i, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[i] = deepCopyMap(val)
		case []any:
			dst[i] = deepCopySlice(val)
		default:
			dst[i] = v
		}
	}
	return dst
}
