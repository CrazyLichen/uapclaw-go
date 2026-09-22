package context

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// VectorStoreService 向量存储服务本地接口。
// 对齐 Python ServiceContext.vector_store 属性返回 Optional[Any]，
// Go 改为返回本地接口以提供编译期类型安全。
// 此接口在 context 包定义，避免 context → vector_store 单向依赖，
// MemoryVectorStore 隐式实现此接口（duck typing）。
//
// Python: openjiuwen/extensions/context_evolver/core/context/service_context.py
type VectorStoreService interface {
	// Upsert 插入或更新向量节点
	Upsert(ctx context.Context, node *schema.VectorNode) error
	// Search 向量相似度搜索
	Search(ctx context.Context, embedding []float64, topK int, metadataFilter map[string]any) ([]*schema.VectorNode, error)
	// Delete 删除向量节点
	Delete(ctx context.Context, nodeID string) (bool, error)
	// Clear 清除所有向量
	Clear()
	// Count 获取向量数量
	Count() int
}

// ──────────────────────────── 结构体 ────────────────────────────

// ServiceContext 管理共享服务（LLM/Embedding/VectorStore）的上下文。
//
// 非并发安全：需在初始化阶段完成所有 RegisterService 调用后，才能并发读取。
// Python 用 __new__ 单例模式（初始化后只读），Go 改为依赖注入——
// 由调用方构造并传入。如果 P6 的 reconfigure 需要运行时修改，届时需加锁。
//
// Python: openjiuwen/extensions/context_evolver/core/context/service_context.py
type ServiceContext struct {
	services map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewServiceContext 创建空的服务上下文。
func NewServiceContext() *ServiceContext {
	return &ServiceContext{services: make(map[string]any)}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// RegisterService 注册服务。
// 对齐 Python ServiceContext.register_service(name, service)。
// 非并发安全，需在初始化阶段调用。
func (sc *ServiceContext) RegisterService(name string, svc any) {
	sc.services[name] = svc
}

// GetService 获取已注册的服务，不存在时返回 nil。
// 对齐 Python ServiceContext.get_service(name)。
func (sc *ServiceContext) GetService(name string) any {
	return sc.services[name]
}

// LLM 获取 LLM 服务。
// 对齐 Python ServiceContext.llm 属性（返回 Optional[Any]）。
// TODO(P6): 定义 LLMService 本地接口替换 any，待 OpenAILLMWrapper 接口确定后实施。
func (sc *ServiceContext) LLM() any {
	return sc.GetService("llm")
}

// EmbeddingModel 获取 Embedding 模型服务。
// 对齐 Python ServiceContext.embedding_model 属性（返回 Optional[Any]）。
// TODO(P6): 定义 EmbeddingService 本地接口替换 any，待 OpenAIEmbeddingWrapper 接口确定后实施。
func (sc *ServiceContext) EmbeddingModel() any {
	return sc.GetService("embedding_model")
}

// VectorStore 获取 VectorStore 服务。
// 对齐 Python ServiceContext.vector_store 属性（返回 Optional[Any]）。
// Go 改为返回 VectorStoreService 本地接口，提供编译期类型安全。
// 注册时需传入实现 VectorStoreService 接口的类型（如 *MemoryVectorStore）。
func (sc *ServiceContext) VectorStore() VectorStoreService {
	svc := sc.GetService("vector_store")
	if svc == nil {
		return nil
	}
	vs, ok := svc.(VectorStoreService)
	if !ok {
		return nil
	}
	return vs
}

// Clear 清除所有已注册的服务。
// 对齐 Python ServiceContext.clear()。
func (sc *ServiceContext) Clear() {
	sc.services = make(map[string]any)
}

// String 实现 Stringer 接口。
// 对齐 Python ServiceContext.__repr__()。
func (sc *ServiceContext) String() string {
	names := make([]string, 0, len(sc.services))
	for k := range sc.services {
		names = append(names, k)
	}
	return fmt.Sprintf("ServiceContext(services=%v)", names)
}
