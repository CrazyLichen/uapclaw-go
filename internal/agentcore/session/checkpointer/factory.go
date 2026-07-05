package checkpointer

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CheckpointerProvider 检查点器提供者接口。
// 对应 Python: openjiuwen/core/session/checkpointer/checkpointer.py (CheckpointerProvider)
type CheckpointerProvider interface {
	// Create 创建检查点器实例
	Create(ctx context.Context, conf map[string]any) (interfaces.Checkpointer, error)
}

// CheckpointerFactoryConfig 检查点器工厂配置结构体，用于工厂创建检查点器实例。
// 对应 Python: openjiuwen/core/session/checkpointer/checkpointer.py (CheckpointerConfig)
// 注意：与 CheckpointerConfig 接口（GetEnv）不同，此结构体仅用于工厂创建参数。
type CheckpointerFactoryConfig struct {
	// Type 检查点器类型（如 "in_memory"、"redis"）
	Type string
	// Conf 检查点器额外配置
	Conf map[string]any
}

// CheckpointerFactory 检查点器工厂，管理 Provider 注册和实例创建。
// 对应 Python: openjiuwen/core/session/checkpointer/checkpointer.py (CheckpointerFactory)
type CheckpointerFactory struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// registry 已注册的 Provider，key=类型名
	registry map[string]CheckpointerProvider
	// defaultCheckpointer 默认检查点器实例
	defaultCheckpointer interfaces.Checkpointer
	// typeCheckpointers 按类型缓存的检查点器实例
	typeCheckpointers map[string]interfaces.Checkpointer
}

// inMemoryProvider InMemory 检查点器提供者。
// 对应 Python: InMemoryCheckpointerProvider
type inMemoryProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// defaultFactory 全局默认工厂单例
var defaultFactory *CheckpointerFactory

// defaultInMemoryCheckpointer 全局默认 InMemory 检查点器实例
// 对应 Python: default_inmemory_checkpointer
var defaultInMemoryCheckpointer interfaces.Checkpointer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCheckpointerFactory 创建检查点器工厂，自动注册 in_memory 和 persistence Provider。
func NewCheckpointerFactory() *CheckpointerFactory {
	f := &CheckpointerFactory{
		registry:          make(map[string]CheckpointerProvider),
		typeCheckpointers: make(map[string]interfaces.Checkpointer),
	}
	f.Register("in_memory", &inMemoryProvider{})
	f.Register("persistence", &persistenceProvider{})
	return f
}

// String 返回脱敏后的配置字符串表示，实现 fmt.Stringer 接口。
// 对应 Python: CheckpointerConfig.__repr__()
// 递归脱敏 Conf 中的 URL 密码，防止日志泄露数据库连接字符串。
func (c CheckpointerFactoryConfig) String() string {
	redactedConf := utils.RedactURLInValue(c.Conf)
	return fmt.Sprintf("CheckpointerConfig(type=%q, conf=%v)", c.Type, redactedConf)
}

// Register 注册检查点器 Provider。
// 对应 Python: CheckpointerFactory.register(name)
func (f *CheckpointerFactory) Register(name string, provider CheckpointerProvider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registry[name] = provider
}

// Create 根据配置创建检查点器实例。
// 对应 Python: CheckpointerFactory.create(checkpointer_conf)
// Python CheckpointerConfig.type 默认值为 "in_memory"，此处对齐：
// 若 conf.Type 为空则回退到 "in_memory"。
func (f *CheckpointerFactory) Create(ctx context.Context, conf CheckpointerFactoryConfig) (interfaces.Checkpointer, error) {
	// 对齐 Python CheckpointerConfig 的默认值
	if conf.Type == "" {
		conf.Type = "in_memory"
	}
	if conf.Conf == nil {
		conf.Conf = make(map[string]any)
	}

	f.mu.RLock()
	provider, exists := f.registry[conf.Type]
	f.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("未知的检查点器类型: %s", conf.Type)
	}
	return provider.Create(ctx, conf.Conf)
}

// SetDefaultCheckpointer 设置默认检查点器实例。
// 对应 Python: CheckpointerFactory.set_default_checkpointer(checkpointer)
func (f *CheckpointerFactory) SetDefaultCheckpointer(cp interfaces.Checkpointer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultCheckpointer = cp
}

// SetCheckpointer 设置指定类型的检查点器实例。
// 对应 Python: CheckpointerFactory.set_checkpointer(store_type, checkpointer)
func (f *CheckpointerFactory) SetCheckpointer(storeType string, cp interfaces.Checkpointer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.typeCheckpointers[storeType] = cp
}

// GetCheckpointer 获取检查点器实例。
// 对应 Python: CheckpointerFactory.get_checkpointer(store_type)
//
// 优先级：
// 1. 指定 storeType 时（非空字符串），先查 typeCheckpointers 缓存
// 2. storeType 为 "in_memory" 时，返回 defaultInMemoryCheckpointer
// 3. 返回 defaultCheckpointer
// 4. defaultCheckpointer 未设置时，返回 defaultInMemoryCheckpointer
//
// 空字符串与不传参数等价（对齐 Python 行为），不会触发类型缓存查找。
func (f *CheckpointerFactory) GetCheckpointer(storeType ...string) interfaces.Checkpointer {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if len(storeType) > 0 && storeType[0] != "" {
		st := storeType[0]
		// 1. 检查类型缓存
		if cp, ok := f.typeCheckpointers[st]; ok {
			return cp
		}
		// 2. in_memory 类型返回默认实例
		if st == "in_memory" {
			return defaultInMemoryCheckpointer
		}
	}

	// 3. 返回默认实例
	if f.defaultCheckpointer != nil {
		return f.defaultCheckpointer
	}
	return defaultInMemoryCheckpointer
}

// GetCheckpointer 从全局工厂获取检查点器实例。
func GetCheckpointer(storeType ...string) interfaces.Checkpointer {
	return defaultFactory.GetCheckpointer(storeType...)
}

// SetDefaultCheckpointer 设置全局默认检查点器。
func SetDefaultCheckpointer(cp interfaces.Checkpointer) {
	defaultFactory.SetDefaultCheckpointer(cp)
}

// SetCheckpointer 设置全局指定类型的检查点器。
func SetCheckpointer(storeType string, cp interfaces.Checkpointer) {
	defaultFactory.SetCheckpointer(storeType, cp)
}

// CreateCheckpointer 从全局工厂创建检查点器。
func CreateCheckpointer(ctx context.Context, conf CheckpointerFactoryConfig) (interfaces.Checkpointer, error) {
	return defaultFactory.Create(ctx, conf)
}

// RegisterCheckpointer 向全局工厂注册 Provider。
func RegisterCheckpointer(name string, provider CheckpointerProvider) {
	defaultFactory.Register(name, provider)
}

// Create 创建 InMemory 检查点器。
func (p *inMemoryProvider) Create(ctx context.Context, conf map[string]any) (interfaces.Checkpointer, error) {
	return defaultInMemoryCheckpointer, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
func init() {
	defaultInMemoryCheckpointer = NewInMemoryCheckpointer()
	defaultFactory = NewCheckpointerFactory()
}
