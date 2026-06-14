package controller

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DataContainer 数据容器接口，封装会话核心业务数据，提供统一的访问、更新和序列化接口。
// 对应 Python: openjiuwen/core/session/session_controller/data_container.py (DataContainer)
type DataContainer interface {
	// Get 获取数据，key 可选过滤，nil 返回全部数据
	Get(key any) map[string]any
	// Update 原子更新数据
	Update(data map[string]any) bool
	// Dump 序列化为可持久化格式
	Dump() (any, error)
}

// StateAccessor 会话状态访问最小接口。
// AgentSessionContainer 通过此接口委托给 Session 实例，
// 避免 controller 子包反向导入 session 父包。
// *session.Session 天然实现此接口（Go 隐式接口满足）。
type StateAccessor interface {
	// UpdateState 更新状态数据
	UpdateState(data map[string]any)
	// GetState 根据 key 获取状态值
	GetState(key state.StateKey) (any, error)
	// DumpState 导出完整状态快照
	DumpState() map[string]any
	// PreRun 预运行（load 时调用）
	PreRun(ctx context.Context, inputs ...map[string]any) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// Permission 数据访问权限枚举。
// 对应 Python: openjiuwen/core/session/session_controller/data_container.py (Permission)
type Permission int

const (
	// PermissionRead 只读权限
	PermissionRead Permission = iota + 1
)

// ContainerLoader 从序列化数据重建 DataContainer 的函数类型
type ContainerLoader func(agentID, sessionID string, serialized any) (DataContainer, error)

// ContainerOption DataContainer 创建选项
type ContainerOption func(DataContainer)

// SharingPolicy 下游会话共享策略，定义调用者可以访问被调用者数据的权限级别和字段范围。
// 对应 Python: openjiuwen/core/session/session_controller/data_container.py (SharingPolicy)
type SharingPolicy struct {
	// Permission 授予的权限级别，当前仅支持只读
	Permission Permission
	// FieldScopes 允许访问的字段名集合，nil 表示全部字段可访问
	FieldScopes map[string]struct{}
}

// AgentSessionContainer 默认数据容器实现，委托给 StateAccessor。
// 对应 Python: openjiuwen/core/session/session_controller/data_container.py (AgentSessionContainer)
type AgentSessionContainer struct {
	// session 被委托的会话实例，初始为 nil
	// 通过 SetSession 注入，或通过 Load 创建
	session StateAccessor
}

// factoryEntry 工厂注册条目
type factoryEntry struct {
	// constructor 创建新 DataContainer 的函数
	constructor func(opts ...ContainerOption) DataContainer
	// loader 从序列化数据重建 DataContainer 的函数
	loader ContainerLoader
}

// DataContainerFactory 数据容器工厂，通过类型名注册和创建 DataContainer 实例。
// 对应 Python: openjiuwen/core/session/session_controller/data_container.py (DataContainerFactory)
type DataContainerFactory struct {
	mu      sync.RWMutex
	entries map[string]factoryEntry
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultDataContainerType 默认数据容器类型
	DefaultDataContainerType = "agent"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	factoryOnce    sync.Once
	factoryInstance *DataContainerFactory
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetFactory 获取全局数据容器工厂单例
func GetFactory() *DataContainerFactory {
	factoryOnce.Do(func() {
		factoryInstance = &DataContainerFactory{
			entries: make(map[string]factoryEntry),
		}
	})
	return factoryInstance
}

// NewAgentSessionContainer 创建默认数据容器实例
func NewAgentSessionContainer() *AgentSessionContainer {
	return &AgentSessionContainer{}
}

// LoadAgentSessionContainer 从序列化数据重建 AgentSessionContainer。
// 当前为简化实现，返回空的 AgentSessionContainer。
// ⤵️ 后续回填：需要 create_agent_session 等价函数后完善
func LoadAgentSessionContainer(agentID, sessionID string, serialized any) (DataContainer, error) {
	return NewAgentSessionContainer(), nil
}

// ──────────────────────────── DataContainerFactory 方法 ────────────────────────────

// Register 注册数据容器类型
func (f *DataContainerFactory) Register(containerType string, loader ContainerLoader, constructor func(opts ...ContainerOption) DataContainer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[containerType] = factoryEntry{
		constructor: constructor,
		loader:      loader,
	}
}

// Create 根据类型名创建数据容器实例
func (f *DataContainerFactory) Create(containerType string, opts ...ContainerOption) (DataContainer, error) {
	f.mu.RLock()
	entry, ok := f.entries[containerType]
	f.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("未知 data_container_type: %q，可用类型: %v", containerType, f.listTypesLocked())
	}
	return entry.constructor(opts...), nil
}

// Load 根据类型名从序列化数据重建数据容器实例
func (f *DataContainerFactory) Load(containerType, agentID, sessionID string, serialized any) (DataContainer, error) {
	f.mu.RLock()
	entry, ok := f.entries[containerType]
	f.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("未知 data_container_type: %q，可用类型: %v", containerType, f.listTypesLocked())
	}
	return entry.loader(agentID, sessionID, serialized)
}

// Has 检查数据容器类型是否已注册
func (f *DataContainerFactory) Has(containerType string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.entries[containerType]
	return ok
}

// ListTypes 返回所有已注册的数据容器类型名
func (f *DataContainerFactory) ListTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.listTypesLocked()
}

// ──────────────────────────── AgentSessionContainer 方法 ────────────────────────────

// Get 获取数据，委托给 StateAccessor
func (c *AgentSessionContainer) Get(key any) map[string]any {
	if c.session == nil {
		return nil
	}
	if key == nil {
		return c.session.DumpState()
	}
	if sk, ok := key.(state.StateKey); ok {
		val, _ := c.session.GetState(sk)
		if m, ok := val.(map[string]any); ok {
			return m
		}
	}
	return c.session.DumpState()
}

// Update 更新数据，委托给 StateAccessor
func (c *AgentSessionContainer) Update(data map[string]any) bool {
	if c.session == nil {
		return false
	}
	c.session.UpdateState(data)
	return true
}

// Dump 序列化，返回空 map（对齐 Python return {}）
func (c *AgentSessionContainer) Dump() (any, error) {
	return map[string]any{}, nil
}

// SetSession 注入 StateAccessor 实例
func (c *AgentSessionContainer) SetSession(session StateAccessor) {
	c.session = session
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// listTypesLocked 在持锁状态下返回所有已注册的类型名
func (f *DataContainerFactory) listTypesLocked() []string {
	types := make([]string, 0, len(f.entries))
	for t := range f.entries {
		types = append(types, t)
	}
	return types
}

// init 注册默认 AgentSessionContainer
func init() {
	GetFactory().Register(
		DefaultDataContainerType,
		LoadAgentSessionContainer,
		func(opts ...ContainerOption) DataContainer {
			return NewAgentSessionContainer()
		},
	)
}
