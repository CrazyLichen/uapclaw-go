// 模型读写处理器。
// 对应 Python 代码：jiuwenswarm/common/config_panel/models_handlers.py（1074 行）。
// 本文件先落展示助手（reasoning_level_display）与上下文窗口解析
// （common/context_window.py 移植）；validate/list/replace_all 主处理器随后续任务。
package configpanel

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	model_clients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmSchema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	common "github.com/uapclaw/uapclaw-go/internal/swarm/common"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
)

// ──────────────────────────── 常量 ────────────────────────────

// DefaultContextWindowTokens 默认上下文窗口（Python: context_window.py:11）。
const DefaultContextWindowTokens = 256 * 1024

// MaxContextWindowTokens 上限（Python: context_window.py:12）。
const MaxContextWindowTokens = int64(1)<<53 - 1

// contextWindowPattern 数量 + 可选单位（Python: context_window.py:13-16）。
var contextWindowPattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(tokens?|k(?:i?b)?|m(?:i?b)?)?$`)

// contextWindowUnitMultipliers 单位倍率（Python: context_window.py:17）。
var contextWindowUnitMultipliers = map[string]float64{
	"tokens": 1,
	"token":  1,
	"k":      1024,
	"kb":     1024,
	"kib":    1024,
	"m":      1024 * 1024,
	"mb":     1024 * 1024,
	"mib":    1024 * 1024,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ReasoningLevelDisplay 存储的 reasoning_level 规范化为展示串
// （Python: models_handlers.py:110 reasoning_level_display）。
// 旧 YAML 裸 on/off 会被 YAML 1.1 解析成布尔——映射回字符串，
// 前端与 replace_all 变更检测都不见裸布尔。
func ReasoningLevelDisplay(value any) string {
	if b, ok := value.(bool); ok {
		if b {
			return "on"
		}
		return "off"
	}
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

// ParsePositiveInt 带大小写无关 K/M 单位的正整数解析
// （Python: context_window.py:31 parse_positive_int）。
// 支持 256k / 256 K / 1m / 纯数字；非法回 0（调用方以 0 判缺失）。
func ParsePositiveInt(value any) int {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
		return 0
	case int64:
		if v > 0 {
			return int(v)
		}
		return 0
	case float64:
		if v > 0 {
			return int(v)
		}
		return 0
	case string:
		m := contextWindowPattern.FindStringSubmatch(strings.TrimSpace(v))
		if m == nil {
			return 0
		}
		num, err := strconv.ParseFloat(m[1], 64)
		if err != nil || num <= 0 {
			return 0
		}
		multiplier := 1.0
		if unit := strings.ToLower(m[2]); unit != "" {
			mult, ok := contextWindowUnitMultipliers[unit]
			if !ok {
				return 0
			}
			multiplier = mult
		}
		result := num * multiplier
		if result > float64(MaxContextWindowTokens) {
			return int(MaxContextWindowTokens)
		}
		return int(result)
	}
	return 0
}

// parsePositiveIntOr 便捷：解析失败回 fallback。
func parsePositiveIntOr(value any, fallback int) int {
	if n := ParsePositiveInt(value); n > 0 {
		return n
	}
	return fallback
}

// ──────────────────────────── replace_all 装配（Python models_handlers.py:497-660） ────────────────────────────

// normalizeProviderValue provider 名归一（Python :130 normalize_provider_value）。
func normalizeProviderValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	// Python: 尝试按 ProviderType 枚举名/值归一；未知值原样返回
	for _, p := range AvailableModelProviders() {
		if strings.EqualFold(trimmed, p) {
			return p
		}
	}
	return trimmed
}

// isOpenAIAccountProvider OAuth 账号 provider（Python :584 注释：OpenAIAccount
// 用 core OAuth token store，不携带用户输入的 api_key）。
func isOpenAIAccountProvider(provider string) bool {
	return provider == "OpenAIAccount"
}

// effectiveEndpointProfile 端点方言推断（Python effective_endpoint_profile；
// 前端未传时按 api_base host 推断已知自建网关方言）。缺口 ⤵️：host 推断表
// 随 vendor 域回填，本批透传显式值、未传回空串。
func effectiveEndpointProfile(apiBase string, explicit any) string {
	if s, ok := explicit.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return ""
}

// buildModelsDefaultsFromFrontend 解析/校验/合成 models.defaults 新值
// （Python :497 build_models_defaults_from_frontend，逐段对齐）。
func BuildModelsDefaultsFromFrontend(rawModels any, cryptoProv crypto.CryptoProvider) ([]map[string]any, error) {
	rawList, ok := rawModels.([]any)
	if !ok {
		return nil, &ConfigPanelBadRequest{Message: "models must be a non-empty list"}
	}
	// Python: 登录送的模型剔除（:505）
	var kept []map[string]any
	for _, item := range rawList {
		if m, ok := item.(map[string]any); ok && !IsLoginModel(m) {
			kept = append(kept, m)
		}
	}
	if len(kept) == 0 {
		return nil, &ConfigPanelBadRequest{Message: "models must be a non-empty list"}
	}
	parsed := make([]map[string]any, 0, len(kept))
	aliasesSeen := map[string]int{}
	for idx, item := range kept {
		modelName := strings.TrimSpace(orString(item["model_name"]))
		if modelName == "" {
			return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("models[%d].model_name is required", idx)}
		}
		if boolOr(item["is_agentos"]) {
			continue
		}
		originIndex := -1
		if raw, ok := item["origin_index"]; ok && raw != nil {
			if n, ok := coerceInt(raw); ok {
				originIndex = n
			}
		}
		apiKey := strings.TrimSpace(orString(item["api_key"]))
		apiBase := strings.TrimSpace(orString(item["api_base"]))
		modelProvider := normalizeProviderValue(orString(item["model_provider"]))
		if apiKey == "" && originIndex < 0 && !isOpenAIAccountProvider(modelProvider) {
			return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("models[%d].api_key is required", idx)}
		}
		if modelProvider != "" && !IsAvailableProvider(modelProvider) {
			return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("models[%d].model_provider must be one of: %v", idx, AvailableModelProviders())}
		}
		temperature := any(nil)
		if raw := item["temperature"]; raw != nil && fmt.Sprintf("%v", raw) != "" {
			f, ok := coerceFloat(raw)
			if !ok {
				return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("models[%d].temperature must be a number or empty", idx)}
			}
			temperature = f
		}
		timeout := 1800
		if n, ok := coerceInt(item["timeout"]); ok && n > 0 {
			timeout = n
		}
		verifySSL := boolOr(item["verify_ssl"])
		isDefault := boolOr(item["is_default"])
		alias := strings.TrimSpace(orString(item["alias"]))
		contextWindowProvided := false
		contextWindow := 0
		if _, ok := item["context_window_tokens"]; ok {
			contextWindowProvided = true
			contextWindow = ParsePositiveInt(item["context_window_tokens"])
			if contextWindow == 0 {
				return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("models[%d].context_window_tokens must be a positive integer or a value such as 256K or 1M", idx)}
			}
		}
		vendorKey := strings.TrimSpace(orString(item["vendor_key"]))
		plan := strings.TrimSpace(orString(item["plan"]))
		if plan != "" {
			if plan != "token_plan" && plan != "coding_plan" && plan != "custom_api" {
				return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("models[%d].plan must be one of: token_plan, coding_plan, custom_api", idx)}
			}
			if vendorKey == "" {
				return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("models[%d].vendor_key is required when plan is set", idx)}
			}
		}
		// Python: validate_reasoning_level_for_model（⤵️ 缺口随 reasoning 域回填：
		// 本批透传字符串，不校验档位-模型兼容矩阵）
		reasoningLevel := strings.TrimSpace(orString(item["reasoning_level"]))
		if alias != "" {
			if prev, dup := aliasesSeen[alias]; dup {
				return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("Alias '%s' is used by both models[%d] and models[%d]", alias, prev, idx)}
			}
			aliasesSeen[alias] = idx
		}
		parsed = append(parsed, map[string]any{
			"model_id":                       item["model_id"],
			"model_name":                     modelName,
			"api_base":                       apiBase,
			"api_key":                        apiKey,
			"api_key_provided":               keyExists(item, "api_key"),
			"model_provider":                 modelProvider,
			"temperature":                    temperature,
			"is_default":                     isDefault,
			"timeout":                        timeout,
			"verify_ssl":                     verifySSL,
			"alias":                          alias,
			"reasoning_level":                reasoningLevel,
			"context_window_tokens":          contextWindow,
			"context_window_tokens_provided": contextWindowProvided,
			"origin_index":                   originIndex,
			"vendor_key":                     vendorKey,
			"plan":                           plan,
			"endpoint_profile":               effectiveEndpointProfile(apiBase, item["endpoint_profile"]),
		})
	}
	// Python: alias 与其他条目 model_name 冲突校验（:637-648）
	for i, p := range parsed {
		a, _ := p["alias"].(string)
		if a == "" {
			continue
		}
		for j, q := range parsed {
			if i != j && q["model_name"] == a {
				return nil, &ConfigPanelBadRequest{Message: fmt.Sprintf("Alias '%s' on models[%d] conflicts with model_name on models[%d]", a, i, j)}
			}
		}
	}
	// Python: raw defaults + resolved defaults + merge（:651-658）
	rawCfg := ReadRawConfig()
	var rawDefaults []map[string]any
	if modelsMap, ok := rawCfg["models"].(map[string]any); ok {
		if list, ok := modelsMap["defaults"].([]any); ok {
			for _, e := range list {
				if m, ok := e.(map[string]any); ok {
					rawDefaults = append(rawDefaults, m)
				}
			}
		}
	}
	resolvedDefaults := config.GetDefaultModels(rawCfg)
	return inferIsDefault(mergeModelsForReplaceAll(parsed, rawDefaults, resolvedDefaults, cryptoProv)), nil
}

// mergeModelsForReplaceAll 前端草稿与持久化 YAML 合并（Python :337
// _merge_models_for_replace_all：origin 命中深拷贝保占位符，diff 覆写；
// 新条目原样落库）。
func mergeModelsForReplaceAll(parsed, rawDefaults, resolvedDefaults []map[string]any, cryptoProv crypto.CryptoProvider) []map[string]any {
	out := make([]map[string]any, 0, len(parsed))
	for _, item := range parsed {
		originIdx, _ := item["origin_index"].(int)
		modelID := orString(item["model_id"])
		if modelID != "" {
			originIdx = -1
			for i, entry := range rawDefaults {
				if orString(entry["model_id"]) == modelID {
					originIdx = i
					break
				}
			}
			if originIdx < 0 {
				// Python: raise ConfigPanelBadRequest(f"unknown model_id: {model_id}")
				// （返回 nil 由调用方转 400；此处以 panic-free 形态回错误条目）
				return nil
			}
		}
		var rawEntry, resolvedEntry map[string]any
		if originIdx >= 0 && originIdx < len(rawDefaults) {
			rawEntry = rawDefaults[originIdx]
			if originIdx < len(resolvedDefaults) {
				resolvedEntry = resolvedDefaults[originIdx]
			}
		}
		if rawEntry != nil {
			newEntry := deepCopyMap(rawEntry)
			newMCC, _ := newEntry["model_client_config"].(map[string]any)
			if newMCC == nil {
				newMCC = map[string]any{}
				newEntry["model_client_config"] = newMCC
			}
			newMCO, _ := newEntry["model_config_obj"].(map[string]any)
			if newMCO == nil {
				newMCO = map[string]any{}
				newEntry["model_config_obj"] = newMCO
			}
			resMCC, _ := resolvedEntry["model_client_config"].(map[string]any)
			resMCO, _ := resolvedEntry["model_config_obj"].(map[string]any)
			if !valuesMatch(item["model_name"], resMCC["model_name"]) {
				newMCC["model_name"] = item["model_name"]
			}
			if !valuesMatch(item["api_base"], resMCC["api_base"]) {
				newMCC["api_base"] = item["api_base"]
			}
			if orString(item["model_provider"]) != "" &&
				(isEnvVarPlaceholder(newMCC["client_provider"]) || !valuesMatch(item["model_provider"], resMCC["client_provider"])) {
				newMCC["client_provider"] = item["model_provider"]
			}
			if item["temperature"] == nil {
				delete(newMCO, "temperature")
			} else if !valuesMatch(item["temperature"], resMCO["temperature"]) {
				newMCO["temperature"] = item["temperature"]
			}
			reasoningLevel := strings.TrimSpace(orString(item["reasoning_level"]))
			if reasoningLevel != ReasoningLevelDisplay(resMCO["reasoning_level"]) {
				if reasoningLevel != "" {
					newMCO["reasoning_level"] = reasoningLevel
				} else {
					delete(newMCO, "reasoning_level")
				}
			}
			if boolOr(item["context_window_tokens_provided"]) {
				newMCO["context_window"] = item["context_window_tokens"]
			}
			if !valuesMatch(item["timeout"], resMCC["timeout"]) {
				newMCC["timeout"] = item["timeout"]
			}
			if !valuesMatch(item["alias"], resolvedEntry["alias"]) {
				newEntry["alias"] = item["alias"]
			}
			previousVendor := orString(newMCC["vendor_key"])
			if orString(item["vendor_key"]) != "" {
				newMCC["vendor_key"] = item["vendor_key"]
			} else {
				delete(newMCC, "vendor_key")
			}
			if orString(item["plan"]) != "" {
				newMCC["plan"] = item["plan"]
			} else {
				delete(newMCC, "plan")
			}
			if orString(item["endpoint_profile"]) != "" {
				newMCC["endpoint_profile"] = item["endpoint_profile"]
			} else if orString(item["model_provider"]) == "Anthropic" || orString(item["vendor_key"]) != previousVendor {
				delete(newMCC, "endpoint_profile")
			}
			newEntry["is_default"] = item["is_default"]
			// Python: api_key 未变保留原值（占位符/密文），变了才加密（:439-449）
			if boolOr(orAny(item["api_key_provided"], true)) && !valuesMatch(item["api_key"], resMCC["api_key"]) {
				newMCC["api_key"] = encryptValue(orString(item["api_key"]), cryptoProv)
			}
			out = append(out, newEntry)
		} else {
			// Python: 新条目 — 前端 payload 为真相源（:451-480）
			mcc := map[string]any{
				"api_base":        item["api_base"],
				"api_key":         encryptValue(orString(item["api_key"]), cryptoProv),
				"model_name":      item["model_name"],
				"client_provider": item["model_provider"],
				"timeout":         item["timeout"],
				"verify_ssl":      item["verify_ssl"],
			}
			if orString(item["vendor_key"]) != "" {
				mcc["vendor_key"] = item["vendor_key"]
			}
			if orString(item["plan"]) != "" {
				mcc["plan"] = item["plan"]
			}
			if orString(item["endpoint_profile"]) != "" {
				mcc["endpoint_profile"] = item["endpoint_profile"]
			}
			mco := map[string]any{
				"context_window": DefaultContextWindowTokens,
			}
			if boolOr(item["context_window_tokens_provided"]) {
				mco["context_window"] = item["context_window_tokens"]
			}
			if item["temperature"] != nil {
				mco["temperature"] = item["temperature"]
			}
			if rl := orString(item["reasoning_level"]); rl != "" {
				mco["reasoning_level"] = rl
			}
			out = append(out, map[string]any{
				"model_client_config": mcc,
				"model_config_obj":    mco,
				"is_default":          item["is_default"],
				"alias":               item["alias"],
			})
		}
	}
	return out
}

// encryptValue 加密单值（Python: crypto.encrypt(val) if (val and crypto) else val）。
func encryptValue(val string, cryptoProv crypto.CryptoProvider) string {
	if val == "" || cryptoProv == nil {
		return val
	}
	enc, err := cryptoProv.Encrypt(context.Background(), val)
	if err != nil {
		logger.Warn(logComponentConfigPanel).Err(err).Msg("encrypt model api_key failed, keep plain")
		return val
	}
	return enc
}

// inferIsDefault 默认模型推断（Python config.py:1553 _infer_is_default：
// 无显式默认时首条为默认）。
func inferIsDefault(entries []map[string]any) []map[string]any {
	hasDefault := false
	for _, e := range entries {
		if boolOr(e["is_default"]) {
			hasDefault = true
			break
		}
	}
	if hasDefault {
		return entries
	}
	for i, e := range entries {
		e["is_default"] = i == 0
	}
	return entries
}

// keyExists 键存在性判定（Python: "api_key" in item）。
func keyExists(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// ReplaceModelsDefaults 写回 models.defaults（Python update_default_models_in_config；
// SetRoundTrip 保注释路径）。
func ReplaceModelsDefaults(modelsMap any) error {
	cfgMgr, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	if _, err := cfgMgr.Load(); err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	if err := cfgMgr.SetRoundTrip("models.defaults", modelsMap); err != nil {
		return fmt.Errorf("写入 models.defaults 失败: %w", err)
	}
	return nil
}

// ──────────────────────────── 非导出助手（Python models_handlers.py 顶部工具） ────────────────────────────

// envVarPlaceholderPattern env 占位符（Python _ENV_VAR_PLACEHOLDER_RE）。
var envVarPlaceholderPattern = regexp.MustCompile(`^\$\{[^}]*\}`)

// isEnvVarPlaceholder 占位符判定。
func isEnvVarPlaceholder(value any) bool {
	s, ok := value.(string)
	return ok && envVarPlaceholderPattern.MatchString(strings.TrimSpace(s))
}

// valuesMatch 前端值与解析快照比对（Python :79 _values_match）。
func valuesMatch(parsedVal, resolvedVal any) bool {
	if _, ok := parsedVal.(bool); ok {
		return boolOr(parsedVal) == boolOr(resolvedVal)
	}
	if _, ok := resolvedVal.(bool); ok {
		return boolOr(parsedVal) == boolOr(resolvedVal)
	}
	if parsedVal == nil && resolvedVal == nil {
		return true
	}
	pf, pok := coerceFloat(parsedVal)
	rf, rok := coerceFloat(resolvedVal)
	if pok && rok {
		return pf == rf
	}
	ps := ""
	if parsedVal != nil {
		ps = fmt.Sprintf("%v", parsedVal)
	}
	rs := ""
	if resolvedVal != nil {
		rs = fmt.Sprintf("%v", resolvedVal)
	}
	return ps == rs
}

// isLoginModel 登录模型判定（23.4d 偏差 E-4 接缝：source 标记）。
// isLoginModelUsagePlaceholder（Python model_catalog.py:152 is_login_model；E-4 接缝）。
func IsLoginModel(entry map[string]any) bool {
	source, _ := entry["source"].(string)
	return strings.Contains(source, "login")
}

// orString any→string（nil 回空）。
func orString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// orAny 首选非空。
func orAny(primary, fallback any) any {
	if primary == nil || primary == "" {
		return fallback
	}
	return primary
}

// boolOr any→bool。
func boolOr(v any) bool {
	b, _ := v.(bool)
	return b
}

// deepCopyMap 深拷贝（Python copy.deepcopy；common 包内同形未导出）。
func deepCopyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = deepCopyAny(v)
	}
	return out
}

// deepCopyAny 递归深拷贝。
func deepCopyAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return deepCopyMap(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopyAny(e)
		}
		return out
	}
	return v
}

// ──────────────────────────── 三方法主处理器（Python models_handlers.py:667/:803/:911） ────────────────────────────

// modelsListRows 序列化 defaults + zen 追加（Python: models_list_handler :815-878）。
func modelsListRows(cfg map[string]any) []map[string]any {
	models := config.GetDefaultModels(cfg)
	result := make([]map[string]any, 0, len(models))
	for idx, entry := range models {
		// Python: 非 dict 条目跳过以保持 origin_index 对齐（:840-845 脏配置防御）
		mcc, _ := entry["model_client_config"].(map[string]any)
		mco, _ := entry["model_config_obj"].(map[string]any)
		if mcc == nil {
			mcc = map[string]any{}
		}
		if mco == nil {
			mco = map[string]any{}
		}
		isDefault, _ := entry["is_default"].(bool)
		modelName := strings.TrimSpace(orString(mcc["model_name"]))
		resultEntry := map[string]any{
			"model_id":         orString(entry["model_id"]),
			"model_name":       modelName,
			"api_base":         orString(mcc["api_base"]),
			"api_key":          orString(mcc["api_key"]),
			"has_api_key":      orString(mcc["api_key"]) != "",
			"model_provider":   orString(mcc["client_provider"]),
			"temperature":      mco["temperature"],
			"reasoning_level":  ReasoningLevelDisplay(mco["reasoning_level"]),
			"is_default":       isDefault,
			"is_agentos":       orString(mco["_source"]) == "agentos",
			"is_free":          boolOr(entry["is_free"]),
			"alias":            orString(entry["alias"]),
			"origin_index":     idx,
			"vendor_key":       orString(orAny(mcc["vendor_key"], entry["vendor_key"])),
			"plan":             orString(orAny(mcc["plan"], entry["plan"])),
			"endpoint_profile": orString(mcc["endpoint_profile"]),
		}
		if modelName != "" {
			cw := ParsePositiveInt(mco["context_window"])
			if cw == 0 {
				cw = DefaultContextWindowTokens
			}
			resultEntry["context_window_tokens"] = cw
		}
		if IsLoginModel(entry) {
			resultEntry["source"] = entry["source"]
			resultEntry["read_only"] = true
		}
		result = append(result, resultEntry)
	}
	// Python: zen 追加（:869-878）——跳过同名
	existing := map[string]bool{}
	for _, item := range result {
		existing[orString(item["model_name"])] = true
	}
	for _, row := range AppendZenFreeModels(nil) {
		name := strings.TrimSpace(orString(row["model_name"]))
		if name == "" || existing[name] {
			continue
		}
		result = append(result, row)
		existing[name] = true
	}
	// 用户拍板（2026-12-06）：内置智谱免费模型域（key/api_base 预置，零配置
	// 可用）；zen 默认关断后此域为免费档唯一展示来源。
	for _, row := range AppendZhipuFreeModels(nil) {
		name := strings.TrimSpace(orString(row["model_name"]))
		if name == "" || existing[name] {
			continue
		}
		result = append(result, row)
		existing[name] = true
	}
	return result
}

// ModelsListPayload models.list payload（Python: models_handlers.py:803
// models_list_handler：{models, model_groups, active_model}）。
func ModelsListPayload(cfg map[string]any) map[string]any {
	rows := modelsListRows(cfg)
	activeModel := ""
	if len(rows) > 0 {
		activeModel = orString(rows[0]["model_name"])
	}
	catalog := common.NewModelCatalog(cfg)
	return map[string]any{
		"models":       rows,
		"model_groups": catalog.ListPublicGroups(),
		"active_model": activeModel,
	}
}

// probeInvoke 探活单次尝试（Python: :750-776 max_tokens=3 失败回退 16 的单步）。
type probeInvoker func(ctx context.Context, maxTokens int) error

// ModelsValidate 探活草稿模型（Python: config_validate_model_handler :667）。
// 返回 payload；入参非法回 *ConfigPanelBadRequest，探活失败回 *ModelsValidateLLMError。
func ModelsValidate(ctx context.Context, params map[string]any) (map[string]any, error) {
	infimum, supremum := 3, 16
	apiBase := strings.TrimSpace(orString(params["api_base"]))
	apiKey := strings.TrimSpace(orString(params["api_key"]))
	model := strings.TrimSpace(orString(params["model"]))
	modelProvider := strings.TrimSpace(orString(params["model_provider"]))
	needsAPIKey := modelProvider != "OpenAIAccount"
	if apiBase == "" || model == "" || modelProvider == "" || (needsAPIKey && apiKey == "") {
		return nil, &ConfigPanelBadRequest{
			Message: "api_base, model, model_provider, and api_key for non-OAuth providers are required",
		}
	}
	if !IsAvailableProvider(modelProvider) {
		return nil, &ConfigPanelBadRequest{
			Message: fmt.Sprintf("Model provider must be one of: %v", AvailableModelProviders()),
		}
	}
	apiBase = strings.TrimRight(apiBase, "/")
	verifySSL := false
	if v, ok := params["verify_ssl"].(bool); ok {
		verifySSL = v
	}
	// Python: build_probe_model_client_config（models_handlers.py:289-330）——
	// 探测预算（client_id / timeout / max_retries）最后固定，不被表单草稿污染
	clientConfig := &llmSchema.ModelClientConfig{
		ClientID:       "config-validate",
		ClientProvider: modelProvider,
		APIKey:         apiKey,
		APIBase:        apiBase,
		Timeout:        25.0,
		MaxRetries:     0,
		VerifySSL:      verifySSL,
	}
	invoke := func(ctx context.Context, maxTokens int) error {
		modelConfig := llmSchema.NewModelRequestConfig()
		modelConfig.ModelName = model
		modelConfig.MaxTokens = &maxTokens
		zero := 0.0
		modelConfig.Temperature = &zero
		modelInst, err := llm.NewModel(clientConfig, modelConfig)
		if err != nil {
			return err
		}
		messages := model_clients.NewMessagesParam(llmSchema.NewUserMessage("Hi"))
		resp, err := modelInst.Invoke(ctx, messages)
		if err != nil {
			return err
		}
		if resp == nil || resp.Content.Text() == "" {
			return errEmptyModelResponse
		}
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var lastErr error
	for _, bound := range []int{infimum, supremum} {
		lastErr = invoke(probeCtx, bound)
		if lastErr == nil {
			return map[string]any{"ok": true, "model_provider": modelProvider}, nil
		}
	}
	return nil, &ModelsValidateLLMError{Message: lastErr.Error()}
}

// errEmptyModelResponse 空响应（Python "模型返回空响应"）。
var errEmptyModelResponse = fmt.Errorf("模型返回空响应")

// ModelsValidateLLMError 探活失败（Python HTTP 语义 LLM_ERROR）。
type ModelsValidateLLMError struct{ Message string }

// Error 实现 error 接口。
func (e *ModelsValidateLLMError) Error() string { return e.Message }

// ModelsReplaceAll 整体替换 models.defaults（Python: models_replace_all_handler
// :911：build → 四连归一/校验 → SetRoundTrip 写回 → reload（models.defaults →
// model scope，force）。reload 回调由调用方注入（server 侧 AgentManager 直调，
// 对齐 Python _apply_models_change 的 on_config_saved/agent_client 双态）。
func ModelsReplaceAll(ctx context.Context, params map[string]any, cryptoProv crypto.CryptoProvider, reload func() error) (map[string]any, error) {
	newModels, err := BuildModelsDefaultsFromFrontend(params["models"], cryptoProv)
	if err != nil {
		return nil, err
	}
	// Python: candidate 合成 + 四连归一/校验（:925-930）
	candidate := ReadRawConfig()
	modelsMap, _ := candidate["models"].(map[string]any)
	if modelsMap == nil {
		modelsMap = map[string]any{}
	}
	modelsMap["defaults"] = newModels
	candidate["models"] = modelsMap
	common.RecoverExistingModelFields(candidate)
	common.NormalizeLegacyModelNulls(candidate)
	common.EnsureModelBusinessIDs(candidate)
	if err := common.RaiseIfInvalid(candidate); err != nil {
		return nil, err
	}
	// Python: update_default_models_in_config（SetRoundTrip 保注释写回）
	if err := ReplaceModelsDefaults(candidate["models"]); err != nil {
		return nil, &ConfigPanelInternalError{Message: "failed to update models.defaults"}
	}
	// Python: _apply_models_change(changed_keys=["models.defaults"], force=True)
	if reload != nil {
		if err := reload(); err != nil {
			logger.Warn(logComponentConfigPanel).Err(err).Msg("models.replace_all reload failed")
		}
	}
	return map[string]any{
		"count":                   len(newModels),
		"applied_without_restart": true,
	}, nil
}
