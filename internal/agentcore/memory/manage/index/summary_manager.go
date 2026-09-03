package index

import (
	"context"
	"sort"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SummaryManager 摘要记忆管理器，管理摘要型记忆的全生命周期。
//
// 通过 BaseMemoryIndex 存取摘要记忆，支持语义搜索。
// 摘要记忆用于存储对话的关键信息和主题概括。
//
// 对应 Python: openjiuwen/core/memory/manage/index/summary_manager.py (SummaryManager)
type SummaryManager struct {
	// memoryManagerBase 嵌入基类（依赖 BaseMemoryIndex）
	memoryManagerBase
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSummaryManager 创建摘要记忆管理器。
//
// 对齐 Python: SummaryManager.__init__(memory_index, crypto_key)
func NewSummaryManager(memoryIndex index.BaseMemoryIndex, cryptoKey []byte) *SummaryManager {
	return &SummaryManager{
		memoryManagerBase: memoryManagerBase{
			memoryIndex: memoryIndex,
			cryptoKey:   cryptoKey,
			memType:     mem_model.MemoryTypeSummary.String(),
		},
	}
}

// AddMemories 批量添加摘要记忆。
//
// 从 memories map 中过滤 mem_type=="summary" 的 SummaryUnit，
// 转换为 MemoryDoc 后写入索引。空结果记 Warn 日志并返回空切片。
//
// 对齐 Python: SummaryManager.add_memories
func (m *SummaryManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, _ ...*llm.Model) ([]mem_model.MemoryUnit, error) {

	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryAddMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	// 过滤 summary 类型的 SummaryUnit
	// 对齐 Python: if mem_type != self.mem_type: continue
	// 对齐 Python: if not isinstance(mem_unit, SummaryUnit): continue
	var summaryUnits []*mem_model.SummaryUnit
	for memType, units := range memories {
		if memType != m.memType {
			continue
		}
		for _, unit := range units {
			summary, ok := unit.(*mem_model.SummaryUnit)
			if !ok {
				logger.Warn(logComponent).
					Str("event_type", "MEMORY_STORE").
					Str("memory_type", m.memType).
					Str("user_id", userID).
					Str("scope_id", scopeID).
					Msg("mem_unit 不是 SummaryUnit 类型，跳过")
				continue
			}
			summaryUnits = append(summaryUnits, summary)
		}
	}

	// 对齐 Python: if not memory_docs: memory_logger.warning("No valid summary docs to add"); return []
	if len(summaryUnits) == 0 {
		logger.Warn(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("memory_type", m.memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("无有效摘要文档可添加")
		return nil, nil
	}

	docs := m.convertToMemoryDocs(summaryUnits)
	if err := m.memoryIndex.AddMemories(ctx, userID, scopeID, docs); err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
	}

	// 对齐 Python: return memories[self.mem_type]
	// 将 summaryUnits 转为 []MemoryUnit 返回
	result := make([]mem_model.MemoryUnit, len(summaryUnits))
	for i, u := range summaryUnits {
		result[i] = u
	}
	return result, nil
}

// Update 按 ID 更新摘要记忆内容。
//
// 先获取旧文档，替换 text 后更新索引。
//
// 对齐 Python: SummaryManager.update
func (m *SummaryManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryUpdateMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	memoryDoc, err := m.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return false, m.wrapException(err, exception.StatusMemoryUpdateMemoryExecutionError, m.memType)
	}
	if memoryDoc == nil {
		return false, nil
	}

	// 对齐 Python: updated_doc = MemoryDoc(id=mem_id, text=new_memory, type=self.mem_type, timestamp=..., fields=memory_doc.fields)
	updatedDoc := &index.MemoryDoc{
		ID:        memID,
		Text:      newMemory,
		Type:      m.memType,
		Timestamp: time.Now(),
		Fields:    memoryDoc.Fields,
	}
	if err := m.memoryIndex.UpdateMemories(ctx, userID, scopeID, []*index.MemoryDoc{updatedDoc}); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryUpdateMemoryExecutionError, m.memType)
	}
	return true, nil
}

// Delete 按 ID 删除摘要记忆。
//
// 对齐 Python: SummaryManager.delete
func (m *SummaryManager) Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryDeleteMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, []string{memID}); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryDeleteMemoryExecutionError, m.memType)
	}
	return true, nil
}

// DeleteByUserID 删除用户+scope 下所有摘要记忆。
//
// 对齐 Python: SummaryManager.delete_by_user_id
func (m *SummaryManager) DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryDeleteMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	if err := m.memoryIndex.DeleteByUserAndScope(ctx, userID, scopeID); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryDeleteMemoryExecutionError, m.memType)
	}
	return true, nil
}

// Get 按 ID 获取单条摘要记忆。
//
// 对齐 Python: SummaryManager.get
func (m *SummaryManager) Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryGetMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	doc, err := m.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryGetMemoryExecutionError, m.memType)
	}
	return doc, nil
}

// Search 语义搜索摘要记忆。
//
// memTypes 参数被忽略，硬编码为 [m.memType]。
//
// 对齐 Python: SummaryManager.search
func (m *SummaryManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, _ []string) ([]*index.MemorySearchResult, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryGetMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	// 对齐 Python: search_results = await self.memory_index.search(user_id, scope_id, query, mem_types=[self.mem_type], top_k=top_k)
	results, err := m.memoryIndex.Search(ctx, userID, scopeID, query, []string{m.memType}, topK)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryGetMemoryExecutionError, m.memType)
	}
	return results, nil
}

// ListUserSummary 分页列出用户摘要记忆，按 timestamp 降序排列。
//
// 对齐 Python: SummaryManager.list_user_summary
func (m *SummaryManager) ListUserSummary(ctx context.Context, userID string, scopeID string, offset int, batchSize int) ([]*index.MemoryDoc, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryGetMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	// 对齐 Python: summary_memories = await self.memory_index.list_memories(user_id, scope_id, offset, batch_size, [self.mem_type])
	docs, err := m.memoryIndex.ListMemories(ctx, userID, scopeID, offset, batchSize, []string{m.memType})
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryGetMemoryExecutionError, m.memType)
	}
	if len(docs) == 0 {
		return nil, nil
	}

	// 对齐 Python: result.sort(key=lambda x: x['timestamp'], reverse=True)
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Timestamp.After(docs[j].Timestamp)
	})
	return docs, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// convertToMemoryDocs 将 SummaryUnit 列表转换为 MemoryDoc 列表。
//
// 对齐 Python: SummaryManager._convert_to_memory_docs
// 文本取 mem_unit.summary，字段含 source_id=mem_unit.message_mem_id, metadata={}
func (m *SummaryManager) convertToMemoryDocs(units []*mem_model.SummaryUnit) []*index.MemoryDoc {
	docs := make([]*index.MemoryDoc, 0, len(units))
	for _, unit := range units {
		ts := parseTimestamp(unit.Timestamp)
		docs = append(docs, &index.MemoryDoc{
			ID:        unit.MemID,
			Text:      unit.Summary,
			Type:      m.memType,
			Timestamp: ts,
			Fields: map[string]any{
				"source_id": unit.MessageMemID,
				"metadata":  map[string]any{},
			},
		})
	}
	return docs
}
