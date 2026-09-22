package context

import (
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ServiceContext 管理共享服务（LLM/Embedding/VectorStore）的上下文。
//
// Python 用 __new__ 单例模式，Go 改为依赖注入——由调用方构造并传入。
// 不使用单例的原因：Go 惯用法倾向显式依赖注入，
// 且单例模式在测试中难以隔离。
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
func (sc *ServiceContext) RegisterService(name string, svc any) {
	sc.services[name] = svc
}

// GetService 获取已注册的服务，不存在时返回 nil。
// 对齐 Python ServiceContext.get_service(name)。
func (sc *ServiceContext) GetService(name string) any {
	return sc.services[name]
}

// LLM 获取 LLM 服务。
// 对齐 Python ServiceContext.llm 属性。
func (sc *ServiceContext) LLM() any {
	return sc.GetService("llm")
}

// EmbeddingModel 获取 Embedding 模型服务。
// 对齐 Python ServiceContext.embedding_model 属性。
func (sc *ServiceContext) EmbeddingModel() any {
	return sc.GetService("embedding_model")
}

// VectorStore 获取 VectorStore 服务。
// 对齐 Python ServiceContext.vector_store 属性。
// 返回 any 类型而非 *vector_store.MemoryVectorStore，避免 context 包循环依赖 vector_store 包。
func (sc *ServiceContext) VectorStore() any {
	return sc.GetService("vector_store")
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
