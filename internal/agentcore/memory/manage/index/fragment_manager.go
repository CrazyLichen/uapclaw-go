package index

import (
	"context"
	"sort"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/update"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FragmentMemoryManager 碎片记忆管理器，管理三种碎片记忆的全生命周期。
//
// 管理三种碎片记忆类型：user_profile、semantic_memory、episodic_memory。
// 一个实例同时服务三种类型（对齐 Python 中 managers 字典映射到同一实例的设计）。
// 所有实际存储操作委托给 BaseMemoryIndex，FragmentMemoryManager 只负责业务逻辑
// （冲突检查、操作分发、数据转换）。
//
// 对应 Python: openjiuwen/core/memory/manage/index/fragment_memory_manager.py (FragmentMemoryManager)
type FragmentMemoryManager struct {
	// memoryManagerBase 嵌入公共基类
	memoryManagerBase
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// UpdateCheckOldMemoryNum 添加新记忆时检索相关旧记忆的 top_k 数量
	UpdateCheckOldMemoryNum = 5
	// UpdateCheckOldMemoryRelevanceThreshold 旧记忆相关度阈值，高于此值才纳入冲突检查
	UpdateCheckOldMemoryRelevanceThreshold = 0.75
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFragmentMemoryManager 创建碎片记忆管理器。
//
// 对齐 Python: FragmentMemoryManager.__init__(memory_index, crypto_key)
func NewFragmentMemoryManager(memoryIndex index.BaseMemoryIndex, cryptoKey []byte) *FragmentMemoryManager {
	return &FragmentMemoryManager{
		memoryManagerBase: memoryManagerBase{
			memoryIndex: memoryIndex,
			cryptoKey:   cryptoKey,
			memType:     "fragment",
		},
	}
}

// AddMemories 批量添加记忆（含冲突检查和冗余消除）。
//
// 流程（对齐 Python FragmentMemoryManager.add_memories）：
//  1. 分离 ADD/UPDATE/DELETE 操作
//  2. 搜索相关旧记忆（top_k=5, score>0.75）
//  3. 无旧记忆且仅 1 条新记忆 → 直接写入，跳过冲突检查
//  4. MemUpdateChecker 冲突检查（LLM 驱动）
//  5. 执行删除 + 添加
//
// 对齐 Python: FragmentMemoryManager.add_memories
// llm 可选参数用于 LLM 驱动冲突检查（对齐 Python: add_memories(llm=None)）
func (m *FragmentMemoryManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error) {

	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryAddMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	// 类型断言：将基类型转为碎片记忆类型（对齐 Python: isinstance(mem_unit, FragmentMemoryUnit)）
	fragmentMemories := make(map[string][]*mem_model.FragmentMemoryUnit, len(memories))
	for key, units := range memories {
		fragUnits := make([]*mem_model.FragmentMemoryUnit, 0, len(units))
		for _, unit := range units {
			frag, ok := unit.(*mem_model.FragmentMemoryUnit)
			if !ok {
				// 对齐 Python: memory_logger.warning("mem_unit is not a FragmentMemoryUnit", memory_type=..., user_id=..., scope_id=...)
				logger.Warn(logComponent).Str("memory_type", m.memType).
					Str("user_id", userID).Str("scope_id", scopeID).
					Msg("mem_unit is not a FragmentMemoryUnit")
				continue
			}
			fragUnits = append(fragUnits, frag)
		}
		fragmentMemories[key] = fragUnits
	}

	deleteSet := make(map[string]bool)
	processResult := make(map[string]*mem_model.FragmentMemoryUnit)

	// 步骤 1：分离 ADD/UPDATE/DELETE 操作
	// 对齐 Python: _get_new_mem_units_and_update_memories
	newMemUnits, err := m.getNewMemUnitsAndUpdateMemories(ctx, userID, scopeID, fragmentMemories, deleteSet, processResult)
	if err != nil {
		return nil, err
	}
	newMemContent := make(map[string]string)
	for id, unit := range newMemUnits {
		newMemContent[id] = unit.Content
	}

	// 无新记忆且有删除 → 执行删除，返回结果
	if len(newMemUnits) == 0 {
		if len(deleteSet) > 0 {
			ids := mapKeys(deleteSet)
			if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, ids); err != nil {
				return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
			}
			removeUpdateEntriesFromProcessResult(deleteSet, processResult)
		}
		return fragmentUnitsToMemoryUnits(mapValues(processResult)), nil
	}

	// 步骤 2：搜索相关旧记忆
	// 对齐 Python: _get_related_old_memories
	oldMemories, err := m.getRelatedOldMemories(ctx, newMemContent, userID, scopeID)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
	}

	// 无旧记忆且仅 1 条新记忆 → 直接写入，跳过冲突检查
	if len(oldMemories) == 0 && len(newMemContent) == 1 {
		if len(deleteSet) > 0 {
			ids := mapKeys(deleteSet)
			if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, ids); err != nil {
				return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
			}
			removeUpdateEntriesFromProcessResult(deleteSet, processResult)
		}
		addList := mapValues(newMemUnits)
		addDocs := m.convertToMemoryDocs(addList)
		if err := m.memoryIndex.AddMemories(ctx, userID, scopeID, addDocs); err != nil {
			return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
		}
		appendMemUnitListToDict(processResult, addList)
		return fragmentUnitsToMemoryUnits(mapValues(processResult)), nil
	}

	// 步骤 3：MemUpdateChecker 冲突检查
	// 对齐 Python: MemUpdateChecker.check(new_memories, old_memories, base_chat_model, retries=3)
	checker := &update.MemUpdateChecker{}
	// 提取 llmModel 参数（对齐 Python: base_chat_model=llm）
	var model *llm.Model
	if len(llmModel) > 0 {
		model = llmModel[0]
	}
	actionItems, err := checker.Check(ctx, newMemContent, oldMemories, update.WithModel(model))
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
	}
	logger.Info(logComponent).
		Int("action_count", len(actionItems)).
		Str("event_type", "MEMORY_PROCESS").
		Msg("记忆冲突检查完成")

	// 步骤 4：执行添加/删除操作
	var addUnitList []*mem_model.FragmentMemoryUnit
	for _, item := range actionItems {
		switch item.Status {
		case update.MemoryStatusAdd:
			if unit, ok := newMemUnits[item.ID]; ok {
				addUnitList = append(addUnitList, unit)
			}
		case update.MemoryStatusDelete:
			deleteSet[item.ID] = true
		}
	}

	if len(deleteSet) > 0 {
		ids := mapKeys(deleteSet)
		if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, ids); err != nil {
			return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
		}
		removeUpdateEntriesFromProcessResult(deleteSet, processResult)
	}
	if len(addUnitList) > 0 {
		addDocs := m.convertToMemoryDocs(addUnitList)
		if err := m.memoryIndex.AddMemories(ctx, userID, scopeID, addDocs); err != nil {
			return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
		}
		appendMemUnitListToDict(processResult, addUnitList)
	}

	return fragmentUnitsToMemoryUnits(mapValues(processResult)), nil
}

// fragmentUnitsToMemoryUnits 将 FragmentMemoryUnit 切片转为 MemoryUnit 切片。
func fragmentUnitsToMemoryUnits(units []*mem_model.FragmentMemoryUnit) []mem_model.MemoryUnit {
	result := make([]mem_model.MemoryUnit, len(units))
	for i, u := range units {
		result[i] = u
	}
	return result
}

// Update 按 ID 更新记忆内容。
//
// 对齐 Python: FragmentMemoryManager.update
func (m *FragmentMemoryManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryUpdateMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	oldDoc, err := m.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return false, m.wrapException(err, exception.StatusMemoryUpdateMemoryExecutionError, m.memType)
	}
	if oldDoc == nil {
		return false, nil
	}

	updatedDoc := &index.MemoryDoc{
		ID:        memID,
		Text:      newMemory,
		Type:      oldDoc.Type,
		Timestamp: time.Now(),
		Fields:    oldDoc.Fields,
	}
	if err := m.memoryIndex.UpdateMemories(ctx, userID, scopeID, []*index.MemoryDoc{updatedDoc}); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryUpdateMemoryExecutionError, m.memType)
	}
	return true, nil
}

// Search 语义搜索记忆。
//
// 对齐 Python: FragmentMemoryManager.search
func (m *FragmentMemoryManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string) ([]*index.MemorySearchResult, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryGetMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	if len(memTypes) == 0 {
		memTypes = FragmentMemoryTypes
	}
	results, err := m.memoryIndex.Search(ctx, userID, scopeID, query, memTypes, topK)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryGetMemoryExecutionError, m.memType)
	}
	// 防御性排序（对齐 Python: result.sort(key=lambda x: x["score"], reverse=True)）
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	// 截断（对齐 Python: return result[:top_k]）
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// Get 按 ID 获取单条记忆。
//
// 对齐 Python: FragmentMemoryManager.get
func (m *FragmentMemoryManager) Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error) {
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

// Delete 按 ID 删除记忆。
//
// 对齐 Python: FragmentMemoryManager.delete
func (m *FragmentMemoryManager) Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryDeleteMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	doc, err := m.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return false, m.wrapException(err, exception.StatusMemoryDeleteMemoryExecutionError, m.memType)
	}
	if doc == nil {
		logger.Error(logComponent).
			Str("memory_id", memID).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("event_type", "MEMORY_STORE").
			Msg("删除记忆失败，记忆不存在")
		return false, nil
	}
	if err := m.memoryIndex.DeleteMemories(ctx, userID, scopeID, []string{memID}); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryDeleteMemoryExecutionError, m.memType)
	}
	return true, nil
}

// DeleteByUserID 删除用户+scope 下所有记忆。
//
// 对齐 Python: FragmentMemoryManager.delete_by_user_id
func (m *FragmentMemoryManager) DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryDeleteMemoryExecutionError, m.memType); err != nil {
		return false, err
	}

	if err := m.memoryIndex.DeleteByUserAndScope(ctx, userID, scopeID); err != nil {
		return false, m.wrapException(err, exception.StatusMemoryDeleteMemoryExecutionError, m.memType)
	}
	return true, nil
}

// ListFragmentMemories 分页列出碎片记忆。
//
// 对齐 Python: FragmentMemoryManager.list_fragment_memories
func (m *FragmentMemoryManager) ListFragmentMemories(ctx context.Context, userID string, scopeID string, offset int, batchSize int, memType string) ([]*index.MemoryDoc, error) {
	if err := m.validateParams(userID, scopeID,
		exception.StatusMemoryGetMemoryExecutionError, m.memType); err != nil {
		return nil, err
	}

	var memTypes []string
	if memType != "" {
		// 非 FragmentMemoryType 校验（对齐 Python: mem_type.value not in FRAGMENT_MEMORY_TYPE）
		if !isFragmentMemoryType(memType) {
			logger.Error(logComponent).
				Str("mem_type", memType).
				Str("memory_type", m.memType).
				Msg("非法碎片记忆类型")
			return nil, nil
		}
		memTypes = []string{memType}
	} else {
		memTypes = FragmentMemoryTypes
	}

	docs, err := m.memoryIndex.ListMemories(ctx, userID, scopeID, offset, batchSize, memTypes)
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryGetMemoryExecutionError, m.memType)
	}
	// 对齐 Python: result.sort(key=lambda x: (x['mem'], str(x.get('timestamp') or '')), reverse=True)
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].Text != docs[j].Text {
			return docs[i].Text > docs[j].Text
		}
		ti := docs[i].Timestamp.Format(time.RFC3339Nano)
		tj := docs[j].Timestamp.Format(time.RFC3339Nano)
		return ti > tj
	})
	return docs, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getNewMemUnitsAndUpdateMemories 分离 ADD/UPDATE/DELETE 操作，执行 UPDATE 和 DELETE。
//
// 对齐 Python: FragmentMemoryManager._get_new_mem_units_and_update_memories
func (m *FragmentMemoryManager) getNewMemUnitsAndUpdateMemories(
	ctx context.Context,
	userID string, scopeID string,
	memories map[string][]*mem_model.FragmentMemoryUnit,
	deleteSet map[string]bool,
	processResult map[string]*mem_model.FragmentMemoryUnit,
) (map[string]*mem_model.FragmentMemoryUnit, error) {
	newMemUnits := make(map[string]*mem_model.FragmentMemoryUnit)
	updateMemUnits := make(map[string]*mem_model.FragmentMemoryUnit)

	for memType, memoryList := range memories {
		if !isFragmentMemoryType(memType) {
			continue
		}
		for _, unit := range memoryList {
			switch unit.OperationType {
			case mem_model.OperationTypeUpdate:
				if unit.Content != "" {
					if _, exists := updateMemUnits[unit.MemID]; exists {
						logger.Warn(logComponent).
							Str("memory_id", unit.MemID).
							Str("event_type", "MEMORY_STORE").
							Msg("更新记忆重复，旧值将被覆盖")
					}
					updateMemUnits[unit.MemID] = unit
				}
			case mem_model.OperationTypeDelete:
				deleteSet[unit.MemID] = true
				processResult[unit.MemID] = unit
			default: // 新增操作
				if unit.Content != "" {
					newMemUnits[unit.MemID] = unit
				}
			}
		}
	}

	// 执行 UPDATE 操作
	if len(updateMemUnits) > 0 {
		updateDocs := m.convertToMemoryDocs(mapValues(updateMemUnits))
		if err := m.memoryIndex.UpdateMemories(ctx, userID, scopeID, updateDocs); err != nil {
			return nil, m.wrapException(err, exception.StatusMemoryUpdateMemoryExecutionError, m.memType)
		}
		for id, unit := range updateMemUnits {
			processResult[id] = unit
		}
	}

	return newMemUnits, nil
}

// getRelatedOldMemories 搜索相关旧记忆用于冲突检查。
//
// 对齐 Python: FragmentMemoryManager._get_related_old_memories
func (m *FragmentMemoryManager) getRelatedOldMemories(
	ctx context.Context,
	newMemContent map[string]string,
	userID string, scopeID string,
) (map[string]string, error) {
	oldMemories := make(map[string]string)
	oldMemIDs := make(map[string]bool)

	for _, newMem := range newMemContent {
		searchResults, err := m.Search(ctx, userID, scopeID, newMem, UpdateCheckOldMemoryNum, nil)
		if err != nil {
			return nil, err
		}
		for _, result := range searchResults {
			if result.Doc != nil && result.Score > UpdateCheckOldMemoryRelevanceThreshold {
				if !oldMemIDs[result.Doc.ID] {
					oldMemories[result.Doc.ID] = result.Doc.Text
					oldMemIDs[result.Doc.ID] = true
				}
			}
		}
	}
	return oldMemories, nil
}

// convertToMemoryDoc 将 FragmentMemoryUnit 转换为 MemoryDoc。
//
// 对齐 Python: FragmentMemoryManager._convert_to_memory_doc
//
// 设计决策：Fields 保持 map[string]any，对齐 Python fields=dict 设计。
// 写入时硬编码 key 字符串（如 "source_id"），读取时需类型断言。
func (m *FragmentMemoryManager) convertToMemoryDoc(unit *mem_model.FragmentMemoryUnit) *index.MemoryDoc {
	ts := parseTimestamp(unit.Timestamp)
	return &index.MemoryDoc{
		ID:        unit.MemID,
		Text:      unit.Content,
		Type:      unit.MemType.String(),
		Timestamp: ts,
		Fields:    map[string]any{"source_id": unit.MessageMemID},
	}
}

// convertToMemoryDocs 批量转换 FragmentMemoryUnit 为 MemoryDoc。
func (m *FragmentMemoryManager) convertToMemoryDocs(units []*mem_model.FragmentMemoryUnit) []*index.MemoryDoc {
	docs := make([]*index.MemoryDoc, 0, len(units))
	for _, unit := range units {
		docs = append(docs, m.convertToMemoryDoc(unit))
	}
	return docs
}

// parseTimestamp 解析多种时间格式为 time.Time。
//
// 对齐 Python: FragmentMemoryManager._parse_timestamp
func parseTimestamp(ts string) time.Time {
	if ts == "" {
		return time.Now()
	}
	layouts := []string{
		"2006-01-02 15-04-05",
		"2006-01-02 15:04:05",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, ts); err == nil {
			return t
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t
	}
	return time.Now()
}

// isFragmentMemoryType 判断是否为碎片记忆类型。
func isFragmentMemoryType(memType string) bool {
	for _, t := range FragmentMemoryTypes {
		if t == memType {
			return true
		}
	}
	return false
}

// removeUpdateEntriesFromProcessResult 从结果中移除被删除的 UPDATE 条目。
//
// 对齐 Python: _remove_update_entries_from_process_result
func removeUpdateEntriesFromProcessResult(deleteSet map[string]bool, processResult map[string]*mem_model.FragmentMemoryUnit) {
	for memID := range deleteSet {
		if unit, ok := processResult[memID]; ok && unit.OperationType == mem_model.OperationTypeUpdate {
			delete(processResult, memID)
		}
	}
}

// appendMemUnitListToDict 将列表追加到字典（去重 + 覆盖）。
//
// 对齐 Python: _append_mem_unit_list_to_dict
func appendMemUnitListToDict(dict map[string]*mem_model.FragmentMemoryUnit, list []*mem_model.FragmentMemoryUnit) {
	for _, unit := range list {
		if _, exists := dict[unit.MemID]; exists {
			logger.Warn(logComponent).
				Str("memory_id", unit.MemID).
				Str("event_type", "MEMORY_STORE").
				Msg("记忆重复，旧值将被覆盖")
		}
		dict[unit.MemID] = unit
	}
}

// mapKeys 返回 map 的所有 key 为切片
func mapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// mapValues 返回 map 的所有 value 为切片
func mapValues[K comparable, V any](m map[K]V) []V {
	values := make([]V, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return values
}
