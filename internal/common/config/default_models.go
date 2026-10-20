package config

import (
	"log"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetDefaultModels 获取默认模型列表，兼容新旧格式。
// 对齐 Python: common/config.py get_default_models(config=None) (L697-731)
//
// 优先级：models.defaults（列表） > models.default（单对象） > 环境变量回退。
// 返回条目经过 custom_headers 解析与 is_default 推断（decryptModelEntries）。
// cfg 为 nil 时加载默认 config.yaml（对齐 Python config=None 时 get_config()）。
func GetDefaultModels(cfg map[string]any) []map[string]any {
	configMap := cfg
	if configMap == nil {
		loaded, err := loadDefaultConfigMap()
		if err != nil {
			log.Printf("[config] GetDefaultModels: load config failed: %v", err)
			configMap = map[string]any{}
		} else {
			configMap = loaded
		}
	}
	models, _ := configMap["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}

	// 新格式：已有 defaults 列表
	if defaults, ok := models["defaults"].([]any); ok && len(defaults) > 0 {
		entries := make([]map[string]any, 0, len(defaults))
		skipped := 0
		for _, item := range defaults {
			if m, ok := item.(map[string]any); ok {
				entries = append(entries, m)
			} else {
				// M-06: 跳过的非 dict 条目记日志（Python 对此场景直接 AttributeError，
				// Go 宽容回退但需留排查线索）
				skipped++
			}
		}
		if skipped > 0 {
			log.Printf("[config] GetDefaultModels: skipped %d non-dict entries in models.defaults", skipped)
		}
		if len(entries) > 0 {
			return decryptModelEntries(entries)
		}
	}

	// 旧格式：单个 default 对象 → 包装为列表
	if defaultEntry, ok := models["default"].(map[string]any); ok {
		return decryptModelEntries([]map[string]any{defaultEntry})
	}

	// 回退：从环境变量构造（env var 已在 ResolveEnvVars 中解密）
	return buildEnvFallbackModelEntry()
}

// InferIsDefault 为模型条目列表推断 is_default 字段。
// 对齐 Python: _infer_is_default (L622-665)
//
// 规则（按 model_client_config.model_name 分组，保持出现顺序）：
//   - 同组仅一个条目 → is_default = true
//   - 同组多个条目 → 第一个为 true，其余为 false
//   - 组内已有显式 is_default=true → 首个 true 保留，同组其余置 false
//
// 返回深拷贝结果，不修改入参。
func InferIsDefault(entries []map[string]any) []map[string]any {
	result := make([]map[string]any, len(entries))
	for i, entry := range entries {
		copied := utils.DeepCopyMap(entry)
		if copied == nil {
			copied = map[string]any{}
		}
		result[i] = copied
	}

	// 按 model_name 分组（保持首次出现顺序，对齐 Python OrderedDict）
	groups := make(map[string][]int)
	order := make([]string, 0)
	for i, entry := range result {
		name := ""
		if mcc, ok := entry["model_client_config"].(map[string]any); ok {
			if n, ok := mcc["model_name"].(string); ok {
				name = n
			}
		}
		if _, exists := groups[name]; !exists {
			order = append(order, name)
		}
		groups[name] = append(groups[name], i)
	}

	for _, name := range order {
		indices := groups[name]
		if len(indices) == 1 {
			result[indices[0]]["is_default"] = true
			continue
		}

		hasExplicit := false
		for _, idx := range indices {
			if b, ok := result[idx]["is_default"].(bool); ok && b {
				hasExplicit = true
				break
			}
		}

		if hasExplicit {
			firstTrue := true
			for _, idx := range indices {
				if b, ok := result[idx]["is_default"].(bool); ok && b && firstTrue {
					result[idx]["is_default"] = true
					firstTrue = false
				} else {
					result[idx]["is_default"] = false
				}
			}
		} else {
			for j, idx := range indices {
				result[idx]["is_default"] = j == 0
			}
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// decryptModelEntries 解密模型条目并推断 is_default，返回深拷贝。
// 对齐 Python: _decrypt_model_entries (L668-694)
//
// ⤵️(10.5.10) api_key 解密依赖 ExtensionRegistry CryptoProvider（extensions→common/config
// 依赖方向已存在，crypto 不能进 common/config），当前为空操作——等价 Python crypto=None
// 时跳过解密路径。custom_headers 经 ParseCustomHeaders 手动处理
// （NormalizeConfig 全项目尚未接线，无法依赖 Load 后处理）。
func decryptModelEntries(entries []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		copied := utils.DeepCopyMap(entry)
		if copied == nil {
			copied = map[string]any{}
		}
		if mcc, ok := copied["model_client_config"].(map[string]any); ok {
			// ⤵️(10.5.10) Python: if mcc.get("api_key") and crypto: mcc["api_key"] = crypto.decrypt(...)
			if rawHeaders, exists := mcc["custom_headers"]; exists {
				mcc["custom_headers"] = ParseCustomHeaders(rawHeaders)
			}
		}
		result = append(result, copied)
	}
	return InferIsDefault(result)
}

// buildEnvFallbackModelEntry 从环境变量构造默认模型条目。
// 对齐 Python: get_default_models 环境变量回退段 (L716-731)
//
// 固定值：timeout=1800、verify_ssl=false、temperature=0.95；alias 非空才附加。
func buildEnvFallbackModelEntry() []map[string]any {
	entry := map[string]any{
		"model_client_config": map[string]any{
			"api_base":        os.Getenv("API_BASE"),
			"api_key":         os.Getenv("API_KEY"),
			"model_name":      os.Getenv("MODEL_NAME"),
			"client_provider": os.Getenv("MODEL_PROVIDER"),
			"custom_headers":  ParseCustomHeaders(os.Getenv("CUSTOM_HEADERS")),
			"timeout":         1800,
			"verify_ssl":      false,
		},
		"model_config_obj": map[string]any{"temperature": 0.95},
	}
	if alias := os.Getenv("MODEL_ALIAS"); alias != "" {
		entry["alias"] = alias
	}
	return []map[string]any{entry}
}

// loadDefaultConfigMap 加载默认 config.yaml。
// 对齐 Python: get_config()
func loadDefaultConfigMap() (map[string]any, error) {
	c, err := New("")
	if err != nil {
		return nil, err
	}
	return c.Load()
}
