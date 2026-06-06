package model_clients

import (
	"fmt"
	"strings"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ClientRegistry 模型客户端注册表，管理客户端类型的注册和创建。
//
// 对应 Python: openjiuwen/core/common/clients/client_registry.py (ClientRegistry)
// 线程安全，使用 RWMutex 保护内部 map。
type ClientRegistry struct {
	mu        sync.RWMutex
	factories map[string]ClientFactory // key: "{clientType}_{clientName}"
}

// ClientFactory 客户端工厂函数类型。
//
// 工厂函数接收 ModelRequestConfig 和 ModelClientConfig，返回 BaseModelClient 实例。
type ClientFactory func(modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) BaseModelClient

// ──────────────────────────── 常量 ────────────────────────────

// clientTypeLLM LLM 客户端类型标识。
const clientTypeLLM = "llm"

// ──────────────────────────── 全局变量 ────────────────────────────

// globalRegistry 全局客户端注册表单例。
var globalRegistry = NewClientRegistry()

// ──────────────────────────── 导出函数 ────────────────────────────

// NewClientRegistry 创建新的客户端注册表。
func NewClientRegistry() *ClientRegistry {
	return &ClientRegistry{
		factories: make(map[string]ClientFactory),
	}
}

// GetClientRegistry 返回全局客户端注册表单例。
func GetClientRegistry() *ClientRegistry {
	return globalRegistry
}

// Register 注册客户端工厂。
//
// name 为客户端名称（如 "OpenAI"），clientType 为类型标识（如 "llm"）。
// 重复注册时打印警告日志但不报错（与 Python 行为一致）。
func (r *ClientRegistry) Register(name, clientType string, factory ClientFactory) {
	fullName := buildRegistryKey(name, clientType)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[fullName]; exists {
		logger.Warn(logComponent).
			Str("full_name", fullName).
			Msg("Client type already registered, skipping")
		return
	}

	r.factories[fullName] = factory
}

// GetClient 根据名称和类型获取客户端实例。
//
// 先尝试 "{clientType}_{name}" 格式精确查找，再尝试直接用 name 查找。
// 未找到时返回 MODEL_PROVIDER_INVALID 错误。
func (r *ClientRegistry) GetClient(name, clientType string, modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) (BaseModelClient, error) {
	if name == "" {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("Client name cannot be empty"),
		)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// 尝试精确查找
	lookupName := buildRegistryKey(name, clientType)
	factory, ok := r.factories[lookupName]
	if !ok {
		// 尝试直接用 name 查找
		factory, ok = r.factories[name]
		if !ok {
			available := r.listClientsLocked()
			return nil, exception.NewBaseError(
				exception.NewStatusCode("MODEL_PROVIDER_INVALID", 181000, ""),
				exception.WithMsg(fmt.Sprintf("Unsupported client_provider: '%s', Supported types: %v", name, available)),
			)
		}
	}

	return factory(modelConfig, clientConfig), nil
}

// Unregister 注销客户端。
func (r *ClientRegistry) Unregister(name, clientType string) error {
	fullName := buildRegistryKey(name, clientType)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[fullName]; !exists {
		return exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg(fmt.Sprintf("Client type '%s' not registered", fullName)),
		)
	}

	delete(r.factories, fullName)
	return nil
}

// ListClients 列出所有已注册客户端的全名。
func (r *ClientRegistry) ListClients() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.listClientsLocked()
}

// CreateModelClient 根据配置创建对应的模型客户端实例。
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/__init__.py (create_model_client)
//
// ⚠️ 后续注册点（2.7-2.12 节实现时修改）：
//   - 2.7 OpenAI 客户端：注册 "llm_OpenAI" + "llm_OpenRouter"
//   - 2.8 DashScope 客户端：注册 "llm_DashScope"
//   - 2.9 DeepSeek 客户端：注册 "llm_DeepSeek"
//   - 2.10 SiliconFlow 客户端：注册 "llm_SiliconFlow"
//   - 2.11 InferenceAffinity 客户端：注册 "llm_InferenceAffinity"
//   - 2.12 IntelliRouter 客户端：注册 "llm_intelli_router"
func CreateModelClient(clientConfig *llmschema.ModelClientConfig, modelConfig *llmschema.ModelRequestConfig) (BaseModelClient, error) {
	if clientConfig.ClientProvider == "" {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("model client config client_provider is none"),
		)
	}
	if clientConfig.ClientID == "" {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("model client config client_id is none"),
		)
	}

	provider := clientConfig.ClientProvider
	return GetClientRegistry().GetClient(provider, clientTypeLLM, modelConfig, clientConfig)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 桥接 ClientRegistry → ProviderValidator，实现 2.5 节预埋的 SetProviderValidator
	llmschema.SetProviderValidator(&registryProviderValidator{})
}

// buildRegistryKey 构建注册表键名。
func buildRegistryKey(name, clientType string) string {
	if clientType != "" {
		return clientType + "_" + name
	}
	return name
}

// listClientsLocked 列出所有已注册客户端（调用方已持有锁）。
func (r *ClientRegistry) listClientsLocked() []string {
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// registryProviderValidator 实现 llmschema.ProviderValidator 接口。
//
// 将 ClientRegistry 中已注册的 LLM 客户端作为 Provider 验证来源。
type registryProviderValidator struct{}

// ValidateProvider 验证 provider 是否已在注册表中注册。
//
// 对应 Python: ModelClientConfig.validate_client_provider() 通过 ClientRegistry 查询。
func (v *registryProviderValidator) ValidateProvider(provider string) string {
	names := GetClientRegistry().ListClients()
	prefix := clientTypeLLM + "_"
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			registeredProvider := name[len(prefix):]
			if strings.EqualFold(registeredProvider, provider) {
				return registeredProvider
			}
		}
	}
	return ""
}
