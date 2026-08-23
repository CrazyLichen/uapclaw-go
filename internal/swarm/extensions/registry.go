package extensions

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentServerClientExtension AgentServerClient 扩展本地接口
// 对齐 Python AgentServerClientExtension（在 sdk 包定义），此处为打破循环依赖的等价接口
// 与 sdk.AgentServerClientExtension 方法签名完全一致，任何实现 sdk 接口的类型也满足此接口
type AgentServerClientExtension interface {
	// Initialize 扩展初始化，对齐 Python initialize(config: ExtensionConfig)
	Initialize(ctx context.Context, config *ExtensionConfig) error
	// Shutdown 扩展关闭，对齐 Python shutdown()
	Shutdown(ctx context.Context) error
	// Metadata 返回扩展元数据，对齐 Python metadata @property
	Metadata() *ExtensionMetadata
	// SetExtensionDir 设置扩展目录，对齐 Python set_extension_dir(path)
	SetExtensionDir(path string)
	// GetClient 返回与 AgentServer 通信的客户端，对齐 Python get_client()
	GetClient() transport.AgentTransport
}

// CryptoUtilityExtension CryptoUtility 扩展本地接口
// 对齐 Python CryptoUtility（在 sdk 包定义），此处为打破循环依赖的等价接口
// ⤵️ 10.5.10 延后：GetCrypto 返回类型待定为 CryptoProvider 接口
type CryptoUtilityExtension interface {
	Initialize(ctx context.Context, config *ExtensionConfig) error
	Shutdown(ctx context.Context) error
	Metadata() *ExtensionMetadata
	SetExtensionDir(path string)
	GetCrypto() any
}

// ExtensionRegistry 扩展注册中心单例，对齐 Python ExtensionRegistry
// 回调机制复用 callback.CallbackFramework.OnCustom/TriggerCustom/OffCustom
type ExtensionRegistry struct {
	mu                   sync.RWMutex
	callbackFramework    *callback.CallbackFramework
	config               *ExtensionConfig
	agentServerClientExt AgentServerClientExtension
	// cryptoUtil 加解密扩展，⤵️ 10.5.10 延后实现
	cryptoUtil CryptoUtilityExtension
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

var (
	registryInstance *ExtensionRegistry
	registryMu       sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetInstance 获取 ExtensionRegistry 单例，对齐 Python ExtensionRegistry.get_instance()
// 未初始化时返回 nil，应使用 GetInstanceErr() 获取错误信息
func GetInstance() *ExtensionRegistry {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registryInstance
}

// GetInstanceErr 获取 ExtensionRegistry 单例，带错误返回
// 对齐 Python ExtensionRegistry.get_instance() 的 RuntimeError 行为
func GetInstanceErr() (*ExtensionRegistry, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if registryInstance == nil {
		return nil, fmt.Errorf("ExtensionRegistry 尚未初始化，请先调用 CreateInstance()")
	}
	return registryInstance, nil
}

// CreateInstance 创建 ExtensionRegistry 单例，对齐 Python ExtensionRegistry.create_instance()
// 已存在时返回 error（对齐 Python: 已存在时 raise RuntimeError）
func CreateInstance(framework *callback.CallbackFramework, config map[string]any, logger any) (*ExtensionRegistry, error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if registryInstance != nil {
		return nil, fmt.Errorf("ExtensionRegistry 已初始化，请勿重复调用 create_instance()")
	}
	registryInstance = &ExtensionRegistry{
		callbackFramework: framework,
		config:            &ExtensionConfig{Config: config, Logger: logger},
	}
	return registryInstance, nil
}

// ResetInstance 重置 ExtensionRegistry 单例为 nil，对齐 Python ExtensionRegistry.reset_instance()
func ResetInstance() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registryInstance = nil
}

// RegisterAgentServerClient 注册 AgentServerClient 扩展，对齐 Python register_agent_server_client(ext)
func (r *ExtensionRegistry) RegisterAgentServerClient(ext AgentServerClientExtension) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentServerClientExt = ext
}

// RegisterCryptoUtility 注册 CryptoUtility 扩展，对齐 Python register_crypto_utility(ext)
// ⤵️ 10.5.10 延后：当前 cryptoUtil 字段为 nil
func (r *ExtensionRegistry) RegisterCryptoUtility(ext CryptoUtilityExtension) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cryptoUtil = ext
}

// GetAgentServerClientExtension 获取 AgentServerClient 扩展实例，
// 对齐 Python get_agent_server_client_extension()
func (r *ExtensionRegistry) GetAgentServerClientExtension() AgentServerClientExtension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agentServerClientExt
}

// GetAgentServerClient 获取底层 AgentTransport 客户端，
// 对齐 Python get_agent_server_client()
// Python 返回 AgentServerClient，Go 返回 transport.AgentTransport（对齐规则 6）
func (r *ExtensionRegistry) GetAgentServerClient() transport.AgentTransport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.agentServerClientExt == nil {
		return nil
	}
	return r.agentServerClientExt.GetClient()
}

// GetCryptoUtilityExtension 获取 CryptoUtility 扩展实例，
// 对齐 Python get_crypto_utility_extension()
// ⤵️ 10.5.10 延后：当前始终返回 nil
func (r *ExtensionRegistry) GetCryptoUtilityExtension() CryptoUtilityExtension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cryptoUtil
}

// GetCryptoProvider 获取 CryptoProvider，对齐 Python get_crypto_provider()
// ⤵️ 10.5.10 延后：当前始终返回 nil
func (r *ExtensionRegistry) GetCryptoProvider() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cryptoUtil == nil {
		return nil
	}
	// ⤵️ 10.5.10: 调 cryptoUtil.GetCrypto() 返回 CryptoProvider
	return r.cryptoUtil.GetCrypto()
}

// Register 注册回调到 CallbackFramework，对齐 Python register(event, handler, priority=100)
// 内部调 callbackFramework.OnCustom(event, fn, WithPriority(priority))
func (r *ExtensionRegistry) Register(event string, handler callback.CustomCallbackFunc, priority int) {
	r.callbackFramework.OnCustom(event, handler, callback.WithPriority(priority))
}

// Unregister 注销回调，对齐 Python unregister(event, handler)
// 内部调 callbackFramework.OffCustom(event, fn)
func (r *ExtensionRegistry) Unregister(event string, handler callback.CustomCallbackFunc) {
	r.callbackFramework.OffCustom(event, handler)
}

// Trigger 触发事件，对齐 Python trigger(event, context)
// 内部调 callbackFramework.TriggerCustom(ctx, event, data)
// nil context 不触发（对齐 CallbackFramework 行为）
func (r *ExtensionRegistry) Trigger(ctx context.Context, event string, data map[string]any) []any {
	return r.callbackFramework.TriggerCustom(ctx, event, data)
}

// Config 获取 ExtensionConfig，对齐 Python ExtensionRegistry.config @property
func (r *ExtensionRegistry) Config() *ExtensionConfig {
	return r.config
}

// ──────────────────────────── 非导出函数 ────────────────────────────
