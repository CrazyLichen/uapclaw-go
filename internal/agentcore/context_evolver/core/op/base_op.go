package op

import (
	"context"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
)

// ──────────────────────────── 接口 ────────────────────────────

// BaseOp 操作接口。
//
// 操作是可组合的计算原子，通过 Then（顺序）和 With（并行）组合成流水线。
// Python 用 __rshift__ (>>) 和 __or__ (|) 运算符，Go 用方法链。
//
// Python BaseOp.__call__ 返回同一个 context 对象（原地变异），
// Go 改为 Execute 返回 error，通过 RuntimeContext 传递中间结果。
//
// Python: openjiuwen/extensions/context_evolver/core/op/base_op.py
type BaseOp interface {
	// Execute 执行操作，读写 RuntimeContext
	Execute(ctx context.Context, rc *cecontext.RuntimeContext) error
}

// ──────────────────────────── 结构体 ────────────────────────────

// OpBase 操作基类，提供对 ServiceContext 中服务的统一访问。
//
// 对齐 Python BaseOp._service_context 属性及其 llm/embedding_model/vector_store 属性。
// 具体 Op 嵌入此结构体以复用服务访问逻辑，避免每个 Op 重复写字段和方法。
//
// Python: openjiuwen/extensions/context_evolver/core/op/base_op.py
type OpBase struct {
	// sc 服务上下文引用，构造时注入
	sc *cecontext.ServiceContext
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Seq 单操作包装为 SequentialOp（链式起点）。
// 对齐 Python BaseOp.__rshift__(other) → SequentialOp(self, other)。
func Seq(op BaseOp) *SequentialOp {
	return &SequentialOp{ops: []BaseOp{op}}
}

// Par 单操作包装为 ParallelOp（链式起点）。
// 对齐 Python BaseOp.__or__(other) → ParallelOp(self, other)。
func Par(op BaseOp) *ParallelOp {
	return &ParallelOp{ops: []BaseOp{op}}
}

// NewOpBase 创建操作基类。
func NewOpBase(sc *cecontext.ServiceContext) *OpBase {
	return &OpBase{sc: sc}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ServiceContext 返回操作持有的服务上下文。
func (b *OpBase) ServiceContext() *cecontext.ServiceContext {
	return b.sc
}

// LLM 返回 LLM 服务。未注册时返回 nil。
// 对齐 Python BaseOp.llm 属性。
func (b *OpBase) LLM() cecontext.LLMService {
	if b.sc == nil {
		return nil
	}
	return b.sc.LLM()
}

// EmbeddingModel 返回 Embedding 模型服务。未注册时返回 nil。
// 对齐 Python BaseOp.embedding_model 属性。
func (b *OpBase) EmbeddingModel() cecontext.EmbeddingService {
	if b.sc == nil {
		return nil
	}
	return b.sc.EmbeddingModel()
}

// VectorStore 返回向量存储服务。未注册时返回 nil。
// 对齐 Python BaseOp.vector_store 属性。
func (b *OpBase) VectorStore() cecontext.VectorStoreService {
	if b.sc == nil {
		return nil
	}
	return b.sc.VectorStore()
}

// AgentFlow 返回 Agent 执行服务。未注册时返回 nil。
func (b *OpBase) AgentFlow() cecontext.AgentFlowService {
	if b.sc == nil {
		return nil
	}
	return b.sc.AgentFlow()
}
