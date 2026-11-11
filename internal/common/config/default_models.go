package config

import (
	"context"
	"log"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
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
			// 先解密 defaults，再追加 agentos（Python 顺序：_decrypt_model_entries
			// 之后 entries.extend(get_agentos_models(config))；agentos 不参与解密
			// 与 is_default 推断）。Python: common/config.py:2104
			return append(decryptModelEntries(entries), GetAgentosModels(configMap)...)
		}
	}

	// 旧格式：单个 default 对象 → 包装为列表
	if defaultEntry, ok := models["default"].(map[string]any); ok {
		// 先解密 default，再追加 agentos（Python: common/config.py:2110）。
		return append(decryptModelEntries([]map[string]any{defaultEntry}), GetAgentosModels(configMap)...)
	}

	// 回退：从环境变量构造（env var 已在 ResolveEnvVars 中解密）
	// Python: common/config.py:2132 entries.extend(get_agentos_models(config))
	return append(buildEnvFallbackModelEntry(), GetAgentosModels(configMap)...)
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
// api_key 解密查全局 crypto provider（10.5.10 ✅ 实装）。
// 依赖方向说明（修正旧错误注释）：config→crypto 合法无环（crypto 仅依赖
// exception 为叶子；真正禁止的是 config→swarm/extensions，那条会环）。
// 解密失败保留原值（对齐 Python try/except pass）。
// custom_headers 经 ParseCustomHeaders 手动处理。
// 2026-10-28 审查已将读链接入 WithNormalize(NormalizeConfig)；此处保留
// 手动解析是因为 GetDefaultModels 亦被无 normalize 的调用链使用（Python
// 侧 get_default_models 同样独立于 get_config），双保险幂等无冲突。
func decryptModelEntries(entries []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(entries))
	provider := crypto.GetCryptoProvider()
	for _, entry := range entries {
		copied := utils.DeepCopyMap(entry)
		if copied == nil {
			copied = map[string]any{}
		}
		if mcc, ok := copied["model_client_config"].(map[string]any); ok {
			// Python: if mcc.get("api_key") and crypto: mcc["api_key"] = crypto.decrypt(...)
			// ctx 说明：GetDefaultModels(cfg) 签名对齐 Python 无 ctx，
			// 此处用 Background；插件 provider 内部有超时兜底
			if key, ok := mcc["api_key"].(string); ok && key != "" && provider != nil {
				if dec, decErr := provider.Decrypt(context.Background(), key); decErr == nil {
					mcc["api_key"] = dec
				}
			}
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

// ──────────────────────────── 导出函数（agentos）────────────────────────────

// RecoverModelFields 修复持久化模型条目的类型错乱，返回是否发生非空修复。
// Python: jiuwenswarm/common/config.py:1643-1669 _recover_model_fields(entry)。
// 三键一体：model_client_config / model_config_obj / model_detail 的非 dict 值
// 重置为 {}；client 四个字符串字段与 detail 两个字符串字段非字符串值重置为 ""。
func RecoverModelFields(entry map[string]any) bool {
	recovered := false
	for _, key := range []string{"model_client_config", "model_config_obj", "model_detail"} {
		value, exists := entry[key]
		if exists {
			if _, ok := value.(map[string]any); !ok {
				entry[key] = map[string]any{}
				recovered = recovered || value != nil
			}
		}
	}
	client, ok := entry["model_client_config"].(map[string]any)
	if ok {
		for _, key := range []string{"model_name", "api_base", "api_key", "client_provider"} {
			value, exists := client[key]
			if exists && value != nil {
				if _, isStr := value.(string); !isStr {
					client[key] = ""
					recovered = true
				}
			}
		}
		if headers, exists := client["custom_headers"]; exists && headers != nil {
			switch headers.(type) {
			case string, map[string]any:
			default:
				client["custom_headers"] = map[string]any{}
				recovered = true
			}
		}
	}
	if detail, ok := entry["model_detail"].(map[string]any); ok {
		for _, key := range []string{"fallback_tag", "model_description"} {
			value, exists := detail[key]
			if exists && value != nil {
				if _, isStr := value.(string); !isStr {
					detail[key] = ""
					recovered = true
				}
			}
		}
	}
	return recovered
}

// GetAgentosModels 读取 models.agentos 备份模型列表，返回带标记的条目。
// Python: jiuwenswarm/common/config.py:1728-1799 get_agentos_models。
// 与 defaults 并列、同等可选可切换，但 is_default=false 绝不抢启动主对话默认
// （_create_model 选默认时只取 is_default=true 条目）；model_config_obj._source=
// "agentos" 仅供前端 is_agentos 置灰只读展示。仅当 models.agentos 是 list 且每条
// model_client_config.model_name 非空（用户已手动填入凭证）才追加；非 list 视为
// 未配置返回空。agentos 条目不做 api_key 解密（Python _decrypt_model_entries 只作用于
// defaults，agentos 走 recover 不解密）。
func GetAgentosModels(cfg map[string]any) []map[string]any {
	configMap := cfg
	if configMap == nil {
		loaded, err := loadDefaultConfigMap()
		if err != nil {
			configMap = map[string]any{}
		} else {
			configMap = loaded
		}
	}
	models, _ := configMap["models"].(map[string]any)
	agentosRaw, _ := models["agentos"].([]any)
	entries := make([]map[string]any, 0, len(agentosRaw))
	for _, block := range agentosRaw {
		blockMap, ok := block.(map[string]any)
		if !ok {
			continue
		}
		mcc, _ := blockMap["model_client_config"].(map[string]any)
		if name, _ := mcc["model_name"].(string); name == "" {
			// model_name 为空 = 该条未配置，跳过不入缓存
			continue
		}
		agentosEntry := utils.DeepCopyMap(blockMap)
		RecoverModelFields(agentosEntry)
		agentosEntry["is_default"] = false
		// _source 注入到 model_config_obj 内部，仅供前端 is_agentos 置灰只读展示。
		mco, _ := agentosEntry["model_config_obj"].(map[string]any)
		if mco == nil {
			mco = map[string]any{}
		}
		mco["_source"] = "agentos"
		agentosEntry["model_config_obj"] = mco
		entries = append(entries, agentosEntry)
	}
	return entries
}
