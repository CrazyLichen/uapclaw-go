package controller

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

type DataContainer interface {
	// Get 获取数据，key 可选过滤，nil 返回全部数据
	Get(key any) map[string]any
	// Update 原子更新数据
	Update(data map[string]any) bool
	// Dump 序列化为可持久化格式
	Dump() (any, error)
}

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

type SharingPolicy struct {
	// Permission 授予的权限级别，当前仅支持只读
	Permission Permission
	// FieldScopes 允许访问的字段名集合，nil 表示全部字段可访问
	FieldScopes map[string]struct{}
}

type AgentSessionContainer struct {
	// session 被委托的会话实例，初始为 nil
	// 通过 SetSession 注入，或通过 Load 创建
	session StateAccessor
}

type factoryEntry struct {
	// constructor 创建新 DataContainer 的函数
	constructor func(opts ...ContainerOption) DataContainer
	// loader 从序列化数据重建 DataContainer 的函数
	loader ContainerLoader
}

type DataContainerFactory struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// entries 已注册的工厂条目
	entries map[string]factoryEntry
}

// ContainerLoader 容器加载函数类型。
type ContainerLoader func(agentID, sessionID string, serialized any) (DataContainer, error)

// ContainerOption 容器选项函数类型。
type ContainerOption func(DataContainer)

// ──────────────────────────── 枚举 ────────────────────────────

type Permission int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PermissionRead 只读权限
	PermissionRead Permission = iota + 1
)

const (
	// DefaultDataContainerType 默认数据容器类型
	DefaultDataContainerType = "agent"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	factoryOnce     sync.Once
	factoryInstance *DataContainerFactory

	// sessionCreator 从序列化数据重建 Session 的函数。
	// 由 session 包在 init 时通过 RegisterSessionCreator 注册，
	// 解决 controller → session 的循环依赖问题。
	// 对齐 Python: AgentSessionContainer.load → create_agent_session(session_id, card=AgentCard(id=agent_id))
	sessionCreator func(sessionID string, card *agentschema.AgentCard, envs map[string]any) StateAccessor
)

// ──────────────────────────── 导出函数 ────────────────────────────

func GetFactory() *DataContainerFactory {
	factoryOnce.Do(func() {
		factoryInstance = &DataContainerFactory{
			entries: make(map[string]factoryEntry),
		}
	})
	return factoryInstance
}

func RegisterSessionCreator(creator func(sessionID string, card *agentschema.AgentCard, envs map[string]any) StateAccessor) {
	sessionCreator = creator
}

func NewAgentSessionContainer() *AgentSessionContainer {
	return &AgentSessionContainer{}
}

func LoadAgentSessionContainer(agentID, sessionID string, serialized any) (DataContainer, error) {
	container := NewAgentSessionContainer()
	if sessionCreator != nil {
		// 对齐 Python: create_agent_session(session_id=session_id, card=AgentCard(id=agent_id))
		// controller 只有 agentID，构建最小 card 传给 sessionCreator
		card := &agentschema.AgentCard{
			BaseCard: schema.BaseCard{ID: agentID},
		}
		sa := sessionCreator(sessionID, card, nil)
		container.SetSession(sa)
		// PreRun 触发 AGENT_SESSION_CREATED 回调，回调中会注入 Session
		// G-08 修复：PreRun 失败时返回 error，阻止部分初始化的容器被使用
		if err := sa.PreRun(context.Background()); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("action", "load_agent_session_container").
				Str("session_id", sessionID).
				Err(err).
				Msg("PreRun 失败")
			return nil, fmt.Errorf("PreRun 失败: %w", err)
		}
	}
	return container, nil
}

func (f *DataContainerFactory) Register(containerType string, loader ContainerLoader, constructor func(opts ...ContainerOption) DataContainer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[containerType] = factoryEntry{
		constructor: constructor,
		loader:      loader,
	}
}

func (f *DataContainerFactory) Create(containerType string, opts ...ContainerOption) (DataContainer, error) {
	f.mu.RLock()
	entry, ok := f.entries[containerType]
	f.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("未知 data_container_type: %q，可用类型: %v", containerType, f.listTypesLocked())
	}
	return entry.constructor(opts...), nil
}

func (f *DataContainerFactory) Load(containerType, agentID, sessionID string, serialized any) (DataContainer, error) {
	f.mu.RLock()
	entry, ok := f.entries[containerType]
	f.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("未知 data_container_type: %q，可用类型: %v", containerType, f.listTypesLocked())
	}
	return entry.loader(agentID, sessionID, serialized)
}

func (f *DataContainerFactory) Has(containerType string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.entries[containerType]
	return ok
}

func (f *DataContainerFactory) ListTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.listTypesLocked()
}

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

func (c *AgentSessionContainer) Update(data map[string]any) bool {
	if c.session == nil {
		return false
	}
	c.session.UpdateState(data)
	return true
}

func (c *AgentSessionContainer) Dump() (any, error) {
	return map[string]any{}, nil
}

func (c *AgentSessionContainer) SetSession(session StateAccessor) {
	c.session = session
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func (f *DataContainerFactory) listTypesLocked() []string {
	types := make([]string, 0, len(f.entries))
	for t := range f.entries {
		types = append(types, t)
	}
	return types
}

func init() {
	GetFactory().Register(
		DefaultDataContainerType,
		LoadAgentSessionContainer,
		func(opts ...ContainerOption) DataContainer {
			return NewAgentSessionContainer()
		},
	)
}
