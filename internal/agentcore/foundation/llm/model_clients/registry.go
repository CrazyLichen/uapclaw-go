package model_clients

import (
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/endpointprofiles"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ClientRegistry 模型客户端注册表，管理客户端类型的注册和创建。
// Python: openjiuwen/core/common/clients/client_registry.py (ClientRegistry)
// 线程安全，使用 RWMutex 保护内部 map。
type ClientRegistry struct {
	mu        sync.RWMutex
	factories map[string]ClientFactory // 键: "{客户端类型}_{客户端名称}"
}

// registryProviderValidator 实现 llmschema.ProviderValidator 接口。
// 将 ClientRegistry 中已注册的 LLM 客户端作为 Provider 验证来源。
type registryProviderValidator struct{}

// ClientFactory 客户端工厂函数类型。
// 工厂函数接收 ModelRequestConfig 和 ModelClientConfig，返回 BaseModelClient 实例。
// Python: factory 可以抛异常，Go 通过 error 返回值表达创建失败。
type ClientFactory func(modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) (BaseModelClient, error)

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
// name 为客户端名称（如 "OpenAI"），clientType 为类型标识（如 "llm"）。
// 重复注册时打印警告日志但不报错（与 Python 行为一致）。
func (r *ClientRegistry) Register(name, clientType string, factory ClientFactory) {
	fullName := buildRegistryKey(name, clientType)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[fullName]; exists {
		logger.Warn(logComponent).
			Str("full_name", fullName).
			Msg("client type already registered, skipping")
		return
	}

	r.factories[fullName] = factory
}

// GetClient 根据名称和类型获取客户端实例。
// 先尝试 "{clientType}_{name}" 格式精确查找，再尝试直接用 name 查找。
// 未找到时返回 MODEL_PROVIDER_INVALID 错误。
func (r *ClientRegistry) GetClient(name, clientType string, modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) (BaseModelClient, error) {
	if name == "" {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("客户端名称不能为空"),
		)
	}

	if clientConfig == nil {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("客户端配置不能为空"),
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
				exception.WithMsg(fmt.Sprintf("不支持的 client_provider: '%s'，支持的类型: %v", name, available)),
			)
		}
	}

	return factory(modelConfig, clientConfig)
}

// Unregister 注销客户端。
func (r *ClientRegistry) Unregister(name, clientType string) error {
	fullName := buildRegistryKey(name, clientType)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[fullName]; !exists {
		return exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg(fmt.Sprintf("客户端类型 '%s' 未注册", fullName)),
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

// ImplementationProvider 返回实际承接请求的客户端实现名。
//
// 规则（顺序敏感）：
//  1. anthropic_messages 协议 → 一律 Anthropic（Anthropic 兼容网关入口，无视原 provider）
//  2. 非 OpenAI → 原样返回（如 intelli_router 有自己的实现）
//  3. OpenAI + openai_account_oauth → OpenAIAccount
//  4. 其余 → OpenAI
//
// Python: model_clients/__init__.py:24-38 _implementation_provider
func ImplementationProvider(cfg *llmschema.ModelClientConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.APIMode == llmschema.APIModeAnthropicMessages {
		return "Anthropic"
	}
	if cfg.ClientProvider != "OpenAI" {
		return cfg.ClientProvider
	}
	if cfg.AuthMode == llmschema.AuthModeOpenAIAccountOAuth {
		return "OpenAIAccount"
	}
	return "OpenAI"
}

// CreateModelClient 根据配置创建对应的模型客户端实例。
//
// 与上游一致的三步：
//  1. 归一：provider 别名 → client_provider=OpenAI + endpoint_profile（保留原名到 Extra）
//  2. 分派：按 ImplementationProvider 得实现名
//  3. 创建：从注册表取工厂
//
// 归一返回副本，不修改入参；归一失败（如 kv_cache.mode 非法）原样上抛。
//
// Python: openjiuwen/core/foundation/llm/model_clients/__init__.py create_model_client
func CreateModelClient(clientConfig *llmschema.ModelClientConfig, modelConfig *llmschema.ModelRequestConfig) (BaseModelClient, error) {
	if clientConfig.ClientProvider == "" {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("模型客户端配置 client_provider 为空"),
		)
	}
	if clientConfig.ClientID == "" {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("模型客户端配置 client_id 为空"),
		)
	}

	// 1. 别名归一（返回副本，不改入参）
	normalized, err := endpointprofiles.NormalizeModelClientConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	// 2. 实现名分派
	dispatchProvider := ImplementationProvider(normalized)

	// 3. 创建
	return GetClientRegistry().GetClient(dispatchProvider, clientTypeLLM, modelConfig, normalized)
}

// ValidateProvider 验证 provider 是否已在注册表中注册。
// Python: ModelClientConfig.validate_client_provider() 通过 ClientRegistry 查询。
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
