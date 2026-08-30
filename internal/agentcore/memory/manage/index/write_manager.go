package index

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WriteManager 写入操作统一路由器。
//
// 根据记忆类型分发到对应子 Manager；按 ID 操作时先从 memory_index 查类型再路由。
// 去重机制：三种 Fragment 类型共享同一个 FragmentMemoryManager 实例（对齐 Python: set(self.managers.values())）。
//
// 对应 Python: openjiuwen/core/memory/manage/index/write_manager.py (WriteManager)
type WriteManager struct {
	// managers 记忆类型 → Manager 实例映射
	managers map[string]BaseMemoryManager
	// memoryIndex 记忆索引，用于按 ID 查询记忆类型
	memoryIndex index.BaseMemoryIndex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// writeLogComponent 日志组件标识
const writeLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWriteManager 创建写入管理器。
//
// 对齐 Python: WriteManager.__init__(managers, memory_index)
func NewWriteManager(managers map[string]BaseMemoryManager, memoryIndex index.BaseMemoryIndex) *WriteManager {
	return &WriteManager{
		managers:    managers,
		memoryIndex: memoryIndex,
	}
}

// AddMemories 批量添加记忆。
//
// 遍历 managers 去重后调用各 Manager 的 AddMemories。
// 去重是因为三种 Fragment 类型共享同一个 FragmentMemoryManager 实例（对齐 Python: set(self.managers.values())）。
//
// 对齐 Python: WriteManager.add_memories(user_id, scope_id, memories, llm)
func (w *WriteManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error) {

	if len(memories) == 0 {
		logger.Debug(writeLogComponent).
			Str("event_type", "MEMORY_STORE").
			Msg("无记忆单元需要添加")
		return nil, nil
	}

	var result []mem_model.MemoryUnit
	// 去重：同一 Manager 只调用一次（对齐 Python: set(self.managers.values())）
	seen := make(map[BaseMemoryManager]bool)
	for _, manager := range w.managers {
		if seen[manager] {
			continue
		}
		seen[manager] = true

		memUnits, err := manager.AddMemories(ctx, userID, scopeID, memories, llmModel...)
		if err != nil {
			logger.Error(writeLogComponent).
				Str("memory_type", w.getManagerMemType(manager)).
				Err(err).
				Str("event_type", "MEMORY_STORE").
				Msg("添加记忆失败")
			return nil, err
		}
		result = append(result, memUnits...)
	}
	return result, nil
}

// UpdateMemByID 按 ID 更新记忆内容。
//
// 先从 memory_index 查 mem_type，再路由到对应 Manager 的 Update。
//
// 对齐 Python: WriteManager.update_mem_by_id(user_id, scope_id, mem_id, memory)
func (w *WriteManager) UpdateMemByID(ctx context.Context, userID string, scopeID string, memID string, newMemory string) error {
	memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
	if err != nil || memType == "" {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("event_type", "MEMORY_STORE").
			Msg("获取记忆类型失败，跳过本次更新")
		return nil
	}
	manager, ok := w.managers[memType]
	if !ok {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("event_type", "MEMORY_STORE").
			Msg("不支持的记忆类型")
		return nil
	}
	_, err = manager.Update(ctx, userID, scopeID, memID, newMemory)
	return err
}

// DeleteMemByID 按 ID 删除记忆。
//
// 先从 memory_index 查 mem_type，再路由到对应 Manager 的 Delete。
//
// 对齐 Python: WriteManager.delete_mem_by_id(user_id, scope_id, mem_id)
func (w *WriteManager) DeleteMemByID(ctx context.Context, userID string, scopeID string, memID string) error {
	memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
	if err != nil || memType == "" {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("event_type", "MEMORY_STORE").
			Msg("获取记忆类型失败，跳过本次删除")
		return nil
	}
	manager, ok := w.managers[memType]
	if !ok {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("event_type", "MEMORY_STORE").
			Msg("不支持的记忆类型")
		return nil
	}
	_, err = manager.Delete(ctx, userID, scopeID, memID)
	return err
}

// DeleteMemByUserID 删除用户+scope 下所有记忆。
//
// 遍历所有 Manager 调用 DeleteByUserID（对齐 Python: set(self.managers.values()) 去重）。
//
// 对齐 Python: WriteManager.delete_mem_by_user_id(user_id, scope_id)
func (w *WriteManager) DeleteMemByUserID(ctx context.Context, userID string, scopeID string) error {
	seen := make(map[BaseMemoryManager]bool)
	for _, manager := range w.managers {
		if seen[manager] {
			continue
		}
		seen[manager] = true
		_, err := manager.DeleteByUserID(ctx, userID, scopeID)
		if err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getMemTypeFromIndex 从 memory_index 查询记忆类型。
//
// 对齐 Python: WriteManager.__get_mem_type_from_index(user_id, scope_id, mem_id)
func (w *WriteManager) getMemTypeFromIndex(ctx context.Context, userID string, scopeID string, memID string) (string, error) {
	doc, err := w.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return "", err
	}
	if doc != nil && doc.Type != "" {
		memType := doc.Type
		if _, ok := w.managers[memType]; ok {
			return memType, nil
		}
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("event_type", "MEMORY_STORE").
			Msg("不支持的记忆类型")
	}

	logger.Warn(writeLogComponent).
		Str("memory_id", memID).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Str("event_type", "MEMORY_STORE").
		Msg("记忆不存在或记忆类型未知")
	return "", nil
}

// getManagerMemType 获取 Manager 的记忆类型字符串（用于日志）。
func (w *WriteManager) getManagerMemType(manager BaseMemoryManager) string {
	// 尝试通过接口断言获取 memoryManagerBase 的 getMemType
	if base, ok := manager.(interface{ getMemType() string }); ok {
		return base.getMemType()
	}
	return "unknown"
}
