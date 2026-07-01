package resources_manager

import (
	"context"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AbstractManager 泛型抽象管理器，提供 provider 注册/获取/注销三操作。
// 子管理器（AgentMgr/ModelMgr/WorkflowMgr 等）嵌入此结构体复用基础能力。
//
// 对应 Python: AbstractManager(Generic[T]) (openjiuwen/core/runner/resources_manager/abstract_manager.py)
// Python 中 _get_resource 通过 inspect.iscoroutinefunction 区分同步/异步 provider，
// Go 中统一为 func(context.Context) (T, error)，无需区分。
type AbstractManager[T any] struct {
	// providers 资源提供者注册表
	providers *ThreadSafeDict[string, func(context.Context) (T, error)]
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbstractManager 创建泛型抽象管理器。
func NewAbstractManager[T any]() AbstractManager[T] {
	return AbstractManager[T]{
		providers: NewThreadSafeDict[string, func(context.Context) (T, error)](),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerProvider 注册资源提供者，重复注册返回 error。
//
// 对应 Python: AbstractManager._register_resource_provider(resource_id, resource)
func (m *AbstractManager[T]) registerProvider(resourceID string, provider func(context.Context) (T, error)) error {
	if m.providers.Contains(resourceID) {
		return fmt.Errorf("添加资源失败，%s 已存在", resourceID)
	}
	m.providers.Set(resourceID, provider)
	return nil
}

// getResource 调用 provider 获取资源实例。
//
// 对应 Python: AbstractManager._get_resource(resource_id)
// Python 中通过 inspect.iscoroutinefunction 区分同步/异步 provider，
// Go 中统一调用 provider(ctx)。
func (m *AbstractManager[T]) getResource(ctx context.Context, resourceID string) (T, error) {
	var zero T
	provider := m.providers.Get(resourceID)
	if provider == nil {
		return zero, fmt.Errorf("资源 %s 未找到", resourceID)
	}
	return provider(ctx)
}

// unregisterProvider 注销资源提供者，返回被注销的 provider。
//
// 对应 Python: AbstractManager._unregister_resource_provider(resource_id)
func (m *AbstractManager[T]) unregisterProvider(resourceID string) (func(context.Context) (T, error), error) {
	provider := m.providers.Pop(resourceID)
	if provider == nil {
		return nil, fmt.Errorf("资源 %s 未找到", resourceID)
	}
	return provider, nil
}
