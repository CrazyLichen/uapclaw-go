package ace

import (
	"context"
	"fmt"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ACERecallMemoryOp ACE 记忆检索操作。
// 从 vector store 全量加载 ACE 记忆（playbook bullets），不做语义搜索/排序。
// 对齐 Python RecallMemoryOp。
//
// Python: openjiuwen/extensions/context_evolver/retrieve/task/ace/run.py
type ACERecallMemoryOp struct {
	op.OpBase
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewACERecallMemoryOp 创建 ACE 记忆检索操作。
// 对齐 Python RecallMemoryOp(service_context)。
func NewACERecallMemoryOp(sc *cecontext.ServiceContext) *ACERecallMemoryOp {
	return &ACERecallMemoryOp{
		OpBase: *op.NewOpBase(sc),
	}
}

// Execute 执行 ACE 记忆检索。
// 对齐 Python RecallMemoryOp.async_execute：
// 从 RuntimeContext 获取 user_id → dummy embedding + metadata filter → Search → 转为 []ACEMemory。
func (o *ACERecallMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	vectorStore := o.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("vector store not configured in ServiceContext")
	}

	// 从 RuntimeContext 获取 user_id（默认 "default"）
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	// 对齐 Python: 用 dummy embedding [0.0]*2560 + top_k=50 搜索
	// metadata_filter = {"workspace_id": user_id, "type": "ace_memory"}
	logger.Debug(logComponent).
		Str("user_id", userID).
		Msg("Loading all ACE memories from vector store...")

	// 构造 dummy embedding（对齐 Python: [0.0] * 2560）
	dummyEmbedding := make([]float64, 2560)
	metadataFilter := map[string]any{
		"workspace_id": userID,
		"type":         "ace_memory",
	}

	nodes, err := vectorStore.Search(ctx, dummyEmbedding, 50, metadataFilter)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("user_id", userID).
			Msg("ACE 记忆检索失败")
		return fmt.Errorf("ACE memory search failed: %w", err)
	}

	// 对齐 Python: 遍历 VectorNode → NewACEMemoryFromVectorNode → 转为 MemoryItem
	// Go 中 []ConcreteType 不能断言为 []Interface，因此存入 []MemoryItem 统一类型
	items := make([]ceschema.MemoryItem, 0, len(nodes))
	for _, node := range nodes {
		aceMemory := ceschema.NewACEMemoryFromVectorNode(node)
		if aceMemory == nil {
			logger.Warn(logComponent).
				Str("node_id", node.ID).
				Msg("Failed to convert ACE memory from node")
			continue
		}
		items = append(items, *aceMemory)
	}

	// 写入 RuntimeContext
	rc.Set("retrieved_memories", items)

	logger.Info(logComponent).
		Str("user_id", userID).
		Int("retrieved_count", len(items)).
		Msg("Retrieved ACE memories (playbook bullets)")

	return nil
}
