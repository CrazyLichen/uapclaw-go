package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelPoolEntry 模型池中的单个 LLM 端点条目。
//
// 描述一个可用的模型端点及其凭证和 Provider 信息。
// ModelAllocator 从池中提取条目并转换为 TeamModelConfig，
// 使每个团队成员可以与不同的端点通信，避免单端点速率限制争用。
//
// 两个标识符各有不同角色：
//   - model_id（自动 UUID）：运行时客户端身份，透传到 ModelClientConfig.client_id，
//     供 foundation 层资源管理器去重/缓存底层 HTTP 客户端。不持久化到 DB。
//   - (model_name, group_index)：语义持久身份，存储在 DB 中作为成员的池引用。
//
// 对应 Python: ModelPoolEntry (openjiuwen/agent_teams/models/pool.py)
type ModelPoolEntry struct {
	// ModelName 模型名称
	ModelName string `json:"model_name"`
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// APIBaseURL API 基础 URL
	APIBaseURL string `json:"api_base_url"`
	// APIProvider 服务商标识
	APIProvider string `json:"api_provider"`
	// Description 描述（可选）
	Description string `json:"description,omitempty"`
	// ModelID 进程本地客户端身份，自动生成 UUID，不持久化
	ModelID string `json:"model_id"`
	// Metadata 扩展载荷，合并到物化的 TeamModelConfig
	//
	// 两个保留子键供 ToTeamModelConfig 使用：
	//   - client: 合并到 ModelClientConfig（如 timeout、verify_ssl、ssl_cert、max_retries、custom_headers）
	//   - request: 合并到 ModelRequestConfig（如 temperature、top_p、max_tokens、stop）
	//
	// 池条目的显式字段（api_key、api_base_url、api_provider、model_name）
	// 始终覆盖 metadata 中同名键。
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ModelRouterConfig 单端点路由器配置，多个模型名共享一组凭证。
//
// 当路由式后端（OpenRouter、LiteLLM proxy 等）通过一个 URL 和一个 API Key
// 服务多个模型名时使用。在 TeamAgentSpec.build() 时展平为 ModelPoolEntry 列表。
//
// 对应 Python: ModelRouterConfig (openjiuwen/agent_teams/models/pool.py)
type ModelRouterConfig struct {
	// APIBaseURL API 基础 URL
	APIBaseURL string `json:"api_base_url"`
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// APIProvider 服务商标识
	APIProvider string `json:"api_provider"`
	// ModelNames 路由端点服务的模型名列表，首个为默认，必须非空且唯一
	ModelNames []string `json:"model_names"`
	// Metadata 扩展载荷，深拷贝到每个展开条目
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ModelPoolEntryOption ModelPoolEntry 构造选项函数。
type ModelPoolEntryOption func(*ModelPoolEntry)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelPoolEntry 创建 ModelPoolEntry 实例。
//
// ModelID 自动生成 UUID。可通过选项函数设置 Description、Metadata 等可选字段。
//
// 对应 Python: ModelPoolEntry(model_name=..., api_key=..., api_base_url=..., api_provider=...)
func NewModelPoolEntry(modelName, apiKey, apiBaseURL, apiProvider string, opts ...ModelPoolEntryOption) *ModelPoolEntry {
	entry := &ModelPoolEntry{
		ModelName:   modelName,
		APIKey:      apiKey,
		APIBaseURL:  apiBaseURL,
		APIProvider: apiProvider,
		ModelID:     uuid.New().String(),
	}
	for _, opt := range opts {
		opt(entry)
	}
	return entry
}

// WithDescription 设置描述。
func WithDescription(desc string) ModelPoolEntryOption {
	return func(e *ModelPoolEntry) { e.Description = desc }
}

// WithModelID 设置模型 ID（通常仅用于测试或继承场景）。
func WithModelID(id string) ModelPoolEntryOption {
	return func(e *ModelPoolEntry) { e.ModelID = id }
}

// WithMetadata 设置元数据。
func WithMetadata(metadata map[string]any) ModelPoolEntryOption {
	return func(e *ModelPoolEntry) { e.Metadata = metadata }
}

// ToTeamModelConfig 从池条目物化 TeamModelConfig。
//
// 保留的 metadata.client 和 metadata.request 子字典分别合并到
// ModelClientConfig 和 ModelRequestConfig。池条目的显式字段
// 始终覆盖 metadata 中同名键。
//
// 对应 Python: ModelPoolEntry.to_team_model_config()
func (e *ModelPoolEntry) ToTeamModelConfig() TeamModelConfig {
	// 提取 metadata 子字典
	var clientExtra map[string]any
	var requestExtra map[string]any
	if e.Metadata != nil {
		if v, ok := e.Metadata["client"]; ok {
			if m, ok := v.(map[string]any); ok {
				clientExtra = m
			}
		}
		if v, ok := e.Metadata["request"]; ok {
			if m, ok := v.(map[string]any); ok {
				requestExtra = m
			}
		}
	}
	if clientExtra == nil {
		clientExtra = make(map[string]any)
	}
	if requestExtra == nil {
		requestExtra = make(map[string]any)
	}

	// 构建 ModelClientConfig：metadata.client 作为基础，池条目显式字段覆盖
	clientCfg := buildModelClientConfig(clientExtra, e.ModelID, e.APIProvider, e.APIKey, e.APIBaseURL)

	// 构建 ModelRequestConfig：metadata.request 作为基础，model_name 覆盖
	requestCfg := buildModelRequestConfig(requestExtra, e.ModelName)

	return TeamModelConfig{
		ModelClientConfig:  *clientCfg,
		ModelRequestConfig: requestCfg,
	}
}

// Validate 校验 ModelRouterConfig 的 model_names 约量：
//   - 必须包含非空字符串（拒绝空白或纯空格条目）
//   - 条目必须唯一（拒绝重复）
//
// 对应 Python: ModelRouterConfig._validate_model_names()
func (r *ModelRouterConfig) Validate() error {
	if len(r.ModelNames) == 0 {
		return fmt.Errorf("model_names 不能为空")
	}

	// 检查空白条目
	var blanks []int
	for i, name := range r.ModelNames {
		if name == "" || strings.TrimSpace(name) == "" {
			blanks = append(blanks, i)
		}
	}
	if len(blanks) > 0 {
		return fmt.Errorf("model_names 必须包含非空字符串；空白位于索引: %v", blanks)
	}

	// 检查重复
	seen := make(map[string]int, len(r.ModelNames))
	var duplicates []string
	for _, name := range r.ModelNames {
		if count, exists := seen[name]; exists {
			if count == 1 {
				duplicates = append(duplicates, name)
			}
			seen[name] = count + 1
		} else {
			seen[name] = 1
		}
	}
	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		return fmt.Errorf("model_names 必须唯一；重复项: %v", duplicates)
	}

	return nil
}

// ToPoolEntries 将路由器展开为每个模型名一个 ModelPoolEntry。
//
// 每个展开条目共享 api_key/api_base_url/api_provider，
// metadata 深拷贝以避免调用者意外交叉污染。
//
// 对应 Python: ModelRouterConfig.to_pool_entries()
func (r *ModelRouterConfig) ToPoolEntries() []ModelPoolEntry {
	entries := make([]ModelPoolEntry, 0, len(r.ModelNames))
	for _, name := range r.ModelNames {
		entry := ModelPoolEntry{
			ModelName:   name,
			APIKey:      r.APIKey,
			APIBaseURL:  r.APIBaseURL,
			APIProvider: r.APIProvider,
			ModelID:     uuid.New().String(),
			Metadata:    deepCopyMap(r.Metadata),
		}
		entries = append(entries, entry)
	}
	return entries
}

// InheritPoolIDs 从 current_pool 中继承 model_id 到 new_pool 的 bit-exact 匹配条目。
//
// ModelPoolEntry.model_id 透传为 ModelClientConfig.client_id，
// foundation 客户端缓存可能使用它来去重 HTTP 客户端。仅在新旧条目的
// 完整配置（除 model_id 外的所有字段）完全相同时才安全继承——
// 否则使用旧 api_key 构建的缓存客户端可能静默服务本应使用新凭证的请求。
//
// 按位精确签名匹配：除 model_id 外的每个字段必须相同。
// 多个同签名条目按池顺序一对一配对。无匹配的新条目保留自己的 model_id。
//
// 对应 Python: inherit_pool_ids()
func InheritPoolIDs(currentPool, newPool []ModelPoolEntry) []ModelPoolEntry {
	// 构建旧池签名→条目桶
	oldBySig := make(map[string][]ModelPoolEntry)
	for _, entry := range currentPool {
		sig := entrySignature(entry)
		oldBySig[sig] = append(oldBySig[sig], entry)
	}

	result := make([]ModelPoolEntry, 0, len(newPool))
	for _, newEntry := range newPool {
		sig := entrySignature(newEntry)
		bucket := oldBySig[sig]
		if len(bucket) > 0 {
			// 从桶头部取出匹配条目，继承其 model_id
			inheritedID := bucket[0].ModelID
			oldBySig[sig] = bucket[1:]
			// 复制新条目并覆盖 model_id
			inherited := newEntry
			inherited.ModelID = inheritedID
			result = append(result, inherited)
		} else {
			result = append(result, newEntry)
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// entrySignature 计算条目的规范签名（排除 model_id）。
//
// 两个具有相同签名的条目描述相同的逻辑端点、相同的认证和请求参数。
// 任何差异（包括 api_key 轮换）产生不同签名，强制生成新的 model_id。
//
// 对应 Python: _entry_signature()
func entrySignature(entry ModelPoolEntry) string {
	// 构建排除 model_id 的 map
	sig := map[string]any{
		"model_name":   entry.ModelName,
		"api_key":      entry.APIKey,
		"api_base_url": entry.APIBaseURL,
		"api_provider": entry.APIProvider,
	}
	if entry.Description != "" {
		sig["description"] = entry.Description
	}
	if len(entry.Metadata) > 0 {
		sig["metadata"] = entry.Metadata
	}

	data, err := json.Marshal(sig)
	if err != nil {
		// 降级：用 fmt 格式化
		return fmt.Sprintf("%+v", sig)
	}
	return string(data)
}

// buildModelClientConfig 从 metadata.client 子字典构建 ModelClientConfig，
// 池条目显式字段覆盖同名键。
func buildModelClientConfig(clientExtra map[string]any, modelID, apiProvider, apiKey, apiBaseURL string) *llmschema.ModelClientConfig {
	cfg := llmschema.NewModelClientConfig(apiProvider, apiKey, apiBaseURL)
	cfg.ClientID = modelID

	// 合并 metadata.client 中的字段
	if v, ok := clientExtra["timeout"]; ok {
		if f, ok := toFloat64(v); ok {
			cfg.Timeout = f
		}
	}
	if v, ok := clientExtra["max_retries"]; ok {
		if i, ok := toInt(v); ok {
			cfg.MaxRetries = i
		}
	}
	if v, ok := clientExtra["verify_ssl"]; ok {
		if b, ok := toBool(v); ok {
			cfg.VerifySSL = b
		}
	}
	if v, ok := clientExtra["ssl_cert"]; ok {
		if s, ok := v.(string); ok {
			cfg.SSLCert = s
		}
	}
	if v, ok := clientExtra["custom_headers"]; ok {
		if m, ok := v.(map[string]any); ok {
			headers := make(map[string]string, len(m))
			for k, val := range m {
				if s, ok := val.(string); ok {
					headers[k] = s
				}
			}
			cfg.CustomHeaders = headers
		}
	}
	// 额外字段存入 Extra
	extra := make(map[string]any)
	knownClientKeys := map[string]struct{}{
		"client_id": {}, "client_provider": {}, "api_key": {}, "api_base": {},
		"timeout": {}, "max_retries": {}, "verify_ssl": {}, "ssl_cert": {}, "custom_headers": {},
	}
	for k, v := range clientExtra {
		if _, known := knownClientKeys[k]; !known {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		cfg.Extra = extra
	}

	return cfg
}

// buildModelRequestConfig 从 metadata.request 子字典构建 ModelRequestConfig，
// model_name 覆盖同名键。
func buildModelRequestConfig(requestExtra map[string]any, modelName string) *llmschema.ModelRequestConfig {
	cfg := llmschema.NewModelRequestConfig()
	cfg.ModelName = modelName

	// 合并 metadata.request 中的字段
	if v, ok := requestExtra["temperature"]; ok {
		if f, ok := toFloat64(v); ok {
			cfg.Temperature = f
		}
	}
	if v, ok := requestExtra["top_p"]; ok {
		if f, ok := toFloat64(v); ok {
			cfg.TopP = f
		}
	}
	if v, ok := requestExtra["max_tokens"]; ok {
		if i, ok := toInt(v); ok {
			cfg.MaxTokens = &i
		}
	}
	if v, ok := requestExtra["stop"]; ok {
		if s, ok := v.(string); ok {
			cfg.Stop = &s
		}
	}
	// 额外字段存入 Extra
	extra := make(map[string]any)
	knownRequestKeys := map[string]struct{}{
		"model": {}, "temperature": {}, "top_p": {}, "max_tokens": {}, "stop": {},
	}
	for k, v := range requestExtra {
		if _, known := knownRequestKeys[k]; !known {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		cfg.Extra = extra
	}

	return cfg
}

// deepCopyMap 深拷贝 map[string]any，通过 JSON 序列化/反序列化实现。
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		// 降级：返回浅拷贝
		cp := make(map[string]any, len(m))
		for k, v := range m {
			cp[k] = v
		}
		return cp
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		cp := make(map[string]any, len(m))
		for k, v := range m {
			cp[k] = v
		}
		return cp
	}
	return result
}

// toFloat64 将 any 转换为 float64。
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}

// toInt 将 any 转换为 int。
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

// toBool 将 any 转换为 bool。
func toBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}
