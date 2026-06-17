package index

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SimpleMemoryIndex 简单记忆索引，基于 KV + Vector 双存储实现记忆的存储与语义检索。
//
// 写入时将完整文档存入 KV Store，向量嵌入存入 Vector Store；
// 搜索时先通过向量相似度检索命中 ID，再从 KV Store 获取完整内容。
// 支持 StorageCodec 对记忆文本进行加解密。
//
// 对应 Python: openjiuwen/core/foundation/store/index/simple_memory_index.py (SimpleMemoryIndex)
type SimpleMemoryIndex struct {
	// MemoryIndexBase 嵌入基类，提供 7 个默认方法
	*MemoryIndexBase
	// kvStore KV 存储后端
	kvStore kv.BaseKVStore
	// vectorStore 向量存储后端
	vectorStore vector.BaseVectorStore
	// embeddingModel 嵌入模型（具体实现见 retrieval/embedding 包）
	embeddingModel embedding.BaseEmbedding
	// createdCollections 已创建的向量集合缓存
	createdCollections map[string]bool
	// codec 存储编解码器（可选，用于加解密记忆文本）
	codec StorageCodec
	// mu 保护 createdCollections 的并发访问
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// kvPrefix KV 键前缀，对齐 Python _KV_PREFIX = "UMD"
	kvPrefix = "UMD"
	// kvSep KV 键分隔符，对齐 Python _KV_SEP = "/"
	kvSep = "/"
	// idsSuffix ID 追踪键后缀，对齐 Python _IDS_SUFFIX = "ids"
	idsSuffix = "ids"
	// byteNumPerID 每个 ID 的固定字节数，对齐 Python _BYTE_NUM_PER_ID = 24
	byteNumPerID = 24
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSimpleMemoryIndex 创建简单记忆索引实例。
func NewSimpleMemoryIndex(
	kvStore kv.BaseKVStore,
	vectorStore vector.BaseVectorStore,
	embeddingModel embedding.BaseEmbedding,
) *SimpleMemoryIndex {
	return &SimpleMemoryIndex{
		MemoryIndexBase:    NewMemoryIndexBase(),
		kvStore:            kvStore,
		vectorStore:        vectorStore,
		embeddingModel:     embeddingModel,
		createdCollections: make(map[string]bool),
	}
}

// SetEmbeddingModel 设置或替换嵌入模型。
func (s *SimpleMemoryIndex) SetEmbeddingModel(model embedding.BaseEmbedding) {
	s.embeddingModel = model
}

// SetStorageCodec 设置存储编解码器。
// 对齐 Python set_storage_codec。加写锁保护并发安全。
func (s *SimpleMemoryIndex) SetStorageCodec(codec StorageCodec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codec = codec
}

// AddMemories 添加新的记忆文档。
// 对齐 Python add_memories：按类型分组 → 嵌入 → 写入 Vector → 写入 KV → ID 追踪。
func (s *SimpleMemoryIndex) AddMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error {
	if len(memories) == 0 {
		return nil
	}

	// 缓存 codec，避免并发读写竞争
	s.mu.RLock()
	codec := s.codec
	s.mu.RUnlock()

	// 按 memType 分组
	byType := make(map[string][]*MemoryDoc)
	for _, m := range memories {
		byType[m.Type] = append(byType[m.Type], m)
	}

	for memType, docs := range byType {
		col := getCollectionName(userID, scopeID, memType)
		texts := make([]string, len(docs))
		for i, d := range docs {
			texts[i] = d.Text
		}

		// 嵌入模型检查
		if s.embeddingModel == nil {
			logger.Error(logComponent).
				Str("event_type", "memory_store").
				Str("scope_id", scopeID).
				Str("collection", col).
				Msg("嵌入模型未初始化")
			return exception.BuildError(exception.StatusMemoryAddMemoryExecutionError,
				exception.WithParam("memory_type", "vector store"),
				exception.WithParam("error_msg", "vector store failed: embedding model not initialized"),
			)
		}

		// 嵌入文本
		embVecs, err := s.embeddingModel.EmbedDocuments(ctx, texts)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "llm_call_error").
				Str("method", "EmbedDocuments").
				Str("collection", col).
				Err(err).
				Msg("嵌入文档失败")
			return fmt.Errorf("嵌入文档失败: %w", err)
		}

		// 确保集合存在
		if len(embVecs) > 0 {
			if err := s.ensureCollection(ctx, col, len(embVecs[0])); err != nil {
				return err
			}
		}

		// 写入向量存储
		vecDocs := make([]map[string]any, len(docs))
		for i, d := range docs {
			vecDocs[i] = map[string]any{
				"id":        d.ID,
				"embedding": embVecs[i],
			}
		}
		if err := s.vectorStore.AddDocs(ctx, col, vecDocs); err != nil {
			logger.Error(logComponent).
				Str("collection", col).
				Err(err).
				Msg("写入向量存储失败")
			return fmt.Errorf("写入向量存储失败: %w", err)
		}

		// 逐条写 KV + ID 追踪
		for _, doc := range docs {
			kvKey := kvMemKey(userID, scopeID, doc.ID)
			kvData := memoryDocToKVData(doc, userID, scopeID)
			if codec != nil {
				if mem, ok := kvData["mem"].(string); ok {
					kvData["mem"] = codec.Encode(mem)
				}
			}
			data, err := json.Marshal(kvData)
			if err != nil {
				return fmt.Errorf("序列化 KV 数据失败: %w", err)
			}
			if err := s.kvStore.Set(ctx, kvKey, data); err != nil {
				logger.Error(logComponent).
					Str("kv_key", kvKey).
					Err(err).
					Msg("写入 KV 存储失败")
				return fmt.Errorf("写入 KV 存储失败: %w", err)
			}
			if err := s.addIDToTracking(ctx, userID, scopeID, doc.ID, memType); err != nil {
				logger.Error(logComponent).
					Str("mem_id", doc.ID).
					Str("mem_type", memType).
					Err(err).
					Msg("添加 ID 追踪失败")
				return err
			}
		}
	}

	return nil
}

// Search 语义搜索记忆文档，返回最相关的结果及相关度分数。
// 对齐 Python search：嵌入查询 → 逐类型向量搜索 → KV 获取 → 解码 → 排序截取。
func (s *SimpleMemoryIndex) Search(ctx context.Context, userID string, scopeID string, query string, memTypes []string, topK int) ([]*MemorySearchResult, error) {
	if s.embeddingModel == nil {
		logger.Error(logComponent).
			Str("event_type", "memory_retrieve").
			Str("scope_id", scopeID).
			Msg("嵌入模型未初始化")
		return []*MemorySearchResult{}, nil
	}

	// 缓存 codec，避免并发读写竞争
	s.mu.RLock()
	codec := s.codec
	s.mu.RUnlock()

	if topK <= 0 {
		topK = defaultTopK
	}

	// 嵌入查询
	queryVec, err := s.embeddingModel.EmbedQuery(ctx, query)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "llm_call_error").
			Str("method", "EmbedQuery").
			Err(err).
			Msg("嵌入查询失败")
		return nil, fmt.Errorf("嵌入查询失败: %w", err)
	}

	// 确定搜索的 memTypes
	var types []string
	if len(memTypes) > 0 {
		types = memTypes
	} else {
		cols, err := s.collectionsFor(ctx, userID, scopeID)
		if err != nil {
			return nil, err
		}
		for _, c := range cols {
			mt := parseMemTypeFromCollection(c)
			if mt != "" {
				types = append(types, mt)
			}
		}
	}

	// 对齐 T-30 修复：初始化为空切片而非 nil，确保 JSON 序列化为 [] 而非 null，对齐 Python 返回 []
	results := make([]*MemorySearchResult, 0)
	for _, mt := range types {
		col := getCollectionName(userID, scopeID, mt)
		exists, err := s.vectorStore.CollectionExists(ctx, col)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("collection", col).Msg("检查集合是否存在失败")
			continue
		}
		if !exists {
			continue
		}

		hits, err := s.vectorStore.Search(ctx, col, queryVec, "embedding", topK, nil)
		if err != nil {
			logger.Error(logComponent).
				Str("collection", col).
				Err(err).
				Msg("向量搜索失败")
			continue
		}

		var hitIDs []string
		scores := make(map[string]float64)
		for _, h := range hits {
			mid, _ := h.Fields["id"].(string)
			if mid != "" {
				hitIDs = append(hitIDs, mid)
				scores[mid] = h.Score
			}
		}

		if len(hitIDs) == 0 {
			continue
		}

		keys := make([]string, len(hitIDs))
		for i, mid := range hitIDs {
			keys[i] = kvMemKey(userID, scopeID, mid)
		}
		values, err := s.kvStore.MGet(ctx, keys)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("collection", col).Msg("批量获取 KV 数据失败")
			continue
		}

		for i, mid := range hitIDs {
			decoded, ok := readKVValue(values[i])
			if !ok || decoded == "" {
				continue
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(decoded), &data); err != nil {
				// G-10 修复：补充 Warn 日志，记录 decode 失败的 memory_id
				logger.Warn(logComponent).
					Err(err).
					Str("memory_id", mid).
					Msg("JSON 解码记忆数据失败，跳过")
				continue
			}
			if codec != nil {
				if mem, ok := data["mem"].(string); ok {
					data["mem"] = codec.Decode(mem)
				}
			}
			doc := kvDataToMemoryDoc(data, mid)
			results = append(results, &MemorySearchResult{
				Doc:   doc,
				Score: scores[mid],
			})
		}
	}

	// 按 Score 降序排序
	sortSearchResultsByScore(results)

	// 截取 topK
	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// UpdateMemories 更新记忆文档。
// 对齐 Python update_memories：先删后加策略。
func (s *SimpleMemoryIndex) UpdateMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error {
	if len(memories) == 0 {
		return nil
	}
	ids := make([]string, len(memories))
	for i, m := range memories {
		ids[i] = m.ID
	}
	if err := s.DeleteMemories(ctx, userID, scopeID, ids); err != nil {
		return err
	}
	return s.AddMemories(ctx, userID, scopeID, memories)
}

// DeleteMemories 按 ID 删除记忆文档。
// 对齐 Python delete_memories：KV 删除 + ID 追踪清理 + Vector 删除。
func (s *SimpleMemoryIndex) DeleteMemories(ctx context.Context, userID string, scopeID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	for _, mid := range ids {
		kvKey := kvMemKey(userID, scopeID, mid)
		raw, err := s.kvStore.Get(ctx, kvKey)
		if err != nil {
			logger.Error(logComponent).Str("kv_key", kvKey).Err(err).Msg("获取 KV 数据失败")
			return fmt.Errorf("获取 KV 数据失败: %w", err)
		}

		var memType string
		if raw != nil {
			var data map[string]any
			if err := json.Unmarshal(raw, &data); err == nil {
				memType, _ = data["mem_type"].(string)
			}
		}

		if err := s.kvStore.Delete(ctx, kvKey); err != nil {
			return fmt.Errorf("删除 KV 数据失败: %w", err)
		}
		if err := s.removeIDFromTracking(ctx, userID, scopeID, mid, memType); err != nil {
			logger.Error(logComponent).Str("mem_id", mid).Err(err).Msg("移除 ID 追踪失败")
			return err
		}
	}

	// 从向量存储中删除
	cols, err := s.collectionsFor(ctx, userID, scopeID)
	if err != nil {
		return fmt.Errorf("列出向量集合失败: %w", err)
	}
	for _, col := range cols {
		if err := s.vectorStore.DeleteDocsByIDs(ctx, col, ids); err != nil {
			logger.Error(logComponent).Str("collection", col).Err(err).Msg("从向量存储删除文档失败")
			return fmt.Errorf("从向量存储删除文档失败: %w", err)
		}
	}

	return nil
}

// DeleteByUser 删除指定用户的所有记忆（跨所有 scope）。
// 对齐 Python delete_by_user。
func (s *SimpleMemoryIndex) DeleteByUser(ctx context.Context, userID string) error {
	// KV 前缀删除
	kvKey := kvPrefix + kvSep + userID + kvSep
	if err := s.kvStore.DeleteByPrefix(ctx, kvKey, 0); err != nil {
		return fmt.Errorf("删除用户 KV 数据失败: %w", err)
	}

	// Vector 删除匹配的集合
	allCols, err := s.vectorStore.ListCollectionNames(ctx)
	if err != nil {
		return fmt.Errorf("列出向量集合失败: %w", err)
	}
	marker := fmt.Sprintf("uid_%s_gid_", userID)
	for _, col := range allCols {
		if strings.HasPrefix(col, marker) {
			if err := s.vectorStore.DeleteCollection(ctx, col); err != nil {
				logger.Error(logComponent).Str("collection", col).Err(err).Msg("删除向量集合失败")
				return fmt.Errorf("删除向量集合失败: %w", err)
			}
			s.mu.Lock()
			delete(s.createdCollections, col)
			s.mu.Unlock()
		}
	}

	return nil
}

// DeleteByScope 删除指定 scope 的所有记忆（跨所有 user）。
// 对齐 Python delete_by_scope。
func (s *SimpleMemoryIndex) DeleteByScope(ctx context.Context, scopeID string) error {
	// KV 扫描删除
	kvKey := kvPrefix + kvSep
	allKV, err := s.kvStore.GetByPrefix(ctx, kvKey)
	if err != nil {
		return fmt.Errorf("扫描 KV 数据失败: %w", err)
	}
	var toDelete []string
	for key := range allKV {
		parts := strings.Split(key, kvSep)
		if len(parts) >= 3 && parts[2] == scopeID {
			toDelete = append(toDelete, key)
		}
	}
	if len(toDelete) > 0 {
		if _, err := s.kvStore.BatchDelete(ctx, toDelete, 0); err != nil {
			return fmt.Errorf("批量删除 KV 数据失败: %w", err)
		}
	}

	// Vector 删除匹配的集合
	scopeMarker := fmt.Sprintf("_gid_%s_mtype_", scopeID)
	allCols, err := s.vectorStore.ListCollectionNames(ctx)
	if err != nil {
		return fmt.Errorf("列出向量集合失败: %w", err)
	}
	for _, col := range allCols {
		if strings.HasPrefix(col, "uid_") && strings.Contains(col, scopeMarker) {
			if err := s.vectorStore.DeleteCollection(ctx, col); err != nil {
				logger.Error(logComponent).Str("collection", col).Err(err).Msg("删除向量集合失败")
				return fmt.Errorf("删除向量集合失败: %w", err)
			}
			s.mu.Lock()
			delete(s.createdCollections, col)
			s.mu.Unlock()
		}
	}

	return nil
}

// DeleteByUserAndScope 删除指定用户和 scope 组合的所有记忆。
// 对齐 Python delete_by_user_and_scope。
func (s *SimpleMemoryIndex) DeleteByUserAndScope(ctx context.Context, userID string, scopeID string) error {
	// KV 前缀删除
	kvKey := kvPrefix + kvSep + userID + kvSep + scopeID + kvSep
	if err := s.kvStore.DeleteByPrefix(ctx, kvKey, 0); err != nil {
		return fmt.Errorf("删除用户+scope KV 数据失败: %w", err)
	}

	// Vector 删除匹配的集合
	cols, err := s.collectionsFor(ctx, userID, scopeID)
	if err != nil {
		return fmt.Errorf("列出向量集合失败: %w", err)
	}
	for _, col := range cols {
		if err := s.vectorStore.DeleteCollection(ctx, col); err != nil {
			logger.Error(logComponent).Str("collection", col).Err(err).Msg("删除向量集合失败")
			return fmt.Errorf("删除向量集合失败: %w", err)
		}
		s.mu.Lock()
		delete(s.createdCollections, col)
		s.mu.Unlock()
	}

	return nil
}

// GetByID 按 ID 获取单条记忆文档，不存在时返回 nil, nil。
// 对齐 Python get_by_id。
func (s *SimpleMemoryIndex) GetByID(ctx context.Context, userID string, scopeID string, memID string) (*MemoryDoc, error) {
	raw, err := s.kvStore.Get(ctx, kvMemKey(userID, scopeID, memID))
	if err != nil {
		return nil, fmt.Errorf("获取 KV 数据失败: %w", err)
	}
	if raw == nil {
		return nil, nil
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("解析 KV 数据失败: %w", err)
	}

	// 缓存 codec
	s.mu.RLock()
	codec := s.codec
	s.mu.RUnlock()

	if codec != nil {
		if mem, ok := data["mem"].(string); ok {
			data["mem"] = codec.Decode(mem)
		}
	}
	return kvDataToMemoryDoc(data, memID), nil
}

// ListMemories 分页获取记忆文档列表。
// 覆盖 MemoryIndexBase 的空实现。
// 对齐 Python list_memories：ID 追踪 → KV 批量获取 → 过滤 → 排序 → 分页。
func (s *SimpleMemoryIndex) ListMemories(ctx context.Context, userID string, scopeID string, offset int, limit int, memTypes []string) ([]*MemoryDoc, error) {
	// 缓存 codec，避免并发读写竞争
	s.mu.RLock()
	codec := s.codec
	s.mu.RUnlock()

	idsKey := kvIDsKey(userID, scopeID, "")
	raw, err := s.kvStore.Get(ctx, idsKey)
	if err != nil {
		return nil, fmt.Errorf("获取 ID 追踪键失败: %w", err)
	}
	val, _ := readKVValue(raw)
	if val == "" {
		return []*MemoryDoc{}, nil
	}

	allIDs := parseAllIDs(val)
	if len(allIDs) == 0 {
		return []*MemoryDoc{}, nil
	}

	keys := make([]string, len(allIDs))
	for i, mid := range allIDs {
		keys[i] = kvMemKey(userID, scopeID, mid)
	}
	values, err := s.kvStore.MGet(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("批量获取 KV 数据失败: %w", err)
	}

	var docs []*MemoryDoc
	for i, mid := range allIDs {
		decoded, ok := readKVValue(values[i])
		if !ok || decoded == "" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(decoded), &data); err != nil {
			// G-10 修复：补充 Warn 日志，记录 decode 失败的 memory_id
			logger.Warn(logComponent).
				Err(err).
				Str("memory_id", mid).
				Msg("JSON 解码记忆数据失败，跳过")
			continue
		}
		if codec != nil {
			if mem, ok := data["mem"].(string); ok {
				data["mem"] = codec.Decode(mem)
			}
		}
		doc := kvDataToMemoryDoc(data, mid)
		if len(memTypes) == 0 {
			docs = append(docs, doc)
		} else {
			for _, mt := range memTypes {
				if doc.Type == mt {
					docs = append(docs, doc)
					break
				}
			}
		}
	}

	// 按 memTypes 顺序 + 时间戳排序
	if len(memTypes) > 0 {
		typeOrder := make(map[string]int)
		for i, mt := range memTypes {
			typeOrder[mt] = i
		}
		sortDocsByTypeAndTime(docs, typeOrder)
	}

	// 分页
	if offset >= len(docs) {
		return []*MemoryDoc{}, nil
	}
	end := offset + limit
	if end > len(docs) {
		end = len(docs)
	}
	return docs[offset:end], nil
}

// ListUserScopes 列出索引中所有 (userID, scopeID) 对。
// 覆盖 MemoryIndexBase 的空实现。
// 对齐 Python list_user_scopes。
func (s *SimpleMemoryIndex) ListUserScopes(ctx context.Context) ([]UserScope, error) {
	kvKey := kvPrefix + kvSep
	allKV, err := s.kvStore.GetByPrefix(ctx, kvKey)
	if err != nil {
		return nil, fmt.Errorf("扫描 KV 数据失败: %w", err)
	}
	seen := make(map[string]bool)
	var scopes []UserScope
	for key := range allKV {
		parts := strings.Split(key, kvSep)
		if len(parts) >= 3 {
			pair := parts[1] + ":" + parts[2]
			if !seen[pair] {
				seen[pair] = true
				scopes = append(scopes, UserScope{UserID: parts[1], ScopeID: parts[2]})
			}
		}
	}
	return scopes, nil
}

// CleanupBackup 清理备份。
// 覆盖 MemoryIndexBase 的默认实现，对齐 Python cleanup_backup。
// Python 中此方法为 @abstractmethod，子类必须实现；
// Go 的基类提供了默认实现（只删内存 map），此覆盖确保语义显式化。
func (s *SimpleMemoryIndex) CleanupBackup(ctx context.Context, backupID string) error {
	return s.MemoryIndexBase.CleanupBackup(ctx, backupID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// kvMemKey 构建记忆文档的 KV 键：UMD/{userID}/{scopeID}/{memID}
func kvMemKey(userID, scopeID, memID string) string {
	return kvPrefix + kvSep + userID + kvSep + scopeID + kvSep + memID
}

// kvIDsKey 构建 ID 追踪的 KV 键。
// memType 为空时：UMD/{userID}/{scopeID}/ids（全局追踪）
// memType 非空时：UMD/{userID}/{scopeID}/{memType}/ids（按类型追踪）
func kvIDsKey(userID, scopeID, memType string) string {
	if memType == "" {
		return kvPrefix + kvSep + userID + kvSep + scopeID + kvSep + idsSuffix
	}
	return kvPrefix + kvSep + userID + kvSep + scopeID + kvSep + memType + kvSep + idsSuffix
}

// parseAllIDs 解析固定宽度拼接的 ID 字符串为 ID 列表。
// 每 byteNumPerID(24) 个字符为一个 ID，对齐 Python _parse_all_ids。
func parseAllIDs(raw string) []string {
	n := len(raw) / byteNumPerID
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		start := i * byteNumPerID
		end := start + byteNumPerID
		if end <= len(raw) {
			ids = append(ids, raw[start:end])
		}
	}
	return ids
}

// appendID 向 ID 字符串追加一个 ID，对齐 Python _append_id。
func appendID(raw string, memID string) string {
	return raw + memID
}

// removeID 从 ID 字符串中移除指定 ID，对齐 Python _remove_id。
func removeID(raw string, memID string) string {
	total := len(raw) / byteNumPerID
	for i := 0; i < total; i++ {
		start := i * byteNumPerID
		end := start + byteNumPerID
		if end <= len(raw) && raw[start:end] == memID {
			return raw[:start] + raw[end:]
		}
	}
	return raw
}

// readKVValue 将 KV 存储的 []byte 值解码为字符串。
// 返回 (值, 是否存在)：raw 为 nil 时返回 ("", false)，否则返回 (string(raw), true)。
// 对齐 Python _read_kv_value 中 raw is None → None 的语义区分。
func readKVValue(raw []byte) (string, bool) {
	if raw == nil {
		return "", false
	}
	return string(raw), true
}

// writeKVValue 将字符串编码为 []byte 写入 KV 存储。
func writeKVValue(text string) []byte {
	return []byte(text)
}

// addIDToTracking 将 ID 添加到全局和类型追踪键中。
// 对齐 Python _add_id_to_tracking。
func (s *SimpleMemoryIndex) addIDToTracking(ctx context.Context, userID, scopeID, memID, memType string) error {
	// 全局 ID 追踪
	key := kvIDsKey(userID, scopeID, "")
	raw, err := s.kvStore.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("获取全局 ID 追踪键失败: %w", err)
	}
	val, _ := readKVValue(raw)
	ids := parseAllIDs(val)
	found := false
	for _, id := range ids {
		if id == memID {
			found = true
			break
		}
	}
	if !found {
		if err := s.kvStore.Set(ctx, key, writeKVValue(appendID(val, memID))); err != nil {
			return fmt.Errorf("更新全局 ID 追踪键失败: %w", err)
		}
	}

	// 按类型 ID 追踪
	tkey := kvIDsKey(userID, scopeID, memType)
	traw, err := s.kvStore.Get(ctx, tkey)
	if err != nil {
		return fmt.Errorf("获取类型 ID 追踪键失败: %w", err)
	}
	tval, _ := readKVValue(traw)
	tids := parseAllIDs(tval)
	tfound := false
	for _, id := range tids {
		if id == memID {
			tfound = true
			break
		}
	}
	if !tfound {
		if err := s.kvStore.Set(ctx, tkey, writeKVValue(appendID(tval, memID))); err != nil {
			return fmt.Errorf("更新类型 ID 追踪键失败: %w", err)
		}
	}

	return nil
}

// removeIDFromTracking 从全局和类型追踪键中移除 ID。
// 移除后键为空时删除该键。
// 对齐 Python _remove_id_from_tracking。
func (s *SimpleMemoryIndex) removeIDFromTracking(ctx context.Context, userID, scopeID, memID string, memType string) error {
	// 全局 ID 追踪
	key := kvIDsKey(userID, scopeID, "")
	raw, err := s.kvStore.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("获取全局 ID 追踪键失败: %w", err)
	}
	val, _ := readKVValue(raw)
	newVal := removeID(val, memID)
	if newVal != "" {
		if err := s.kvStore.Set(ctx, key, writeKVValue(newVal)); err != nil {
			return fmt.Errorf("更新全局 ID 追踪键失败: %w", err)
		}
	} else {
		if err := s.kvStore.Delete(ctx, key); err != nil {
			return fmt.Errorf("删除全局 ID 追踪键失败: %w", err)
		}
	}

	if memType == "" {
		return nil
	}

	// 按类型 ID 追踪
	tkey := kvIDsKey(userID, scopeID, memType)
	traw, err := s.kvStore.Get(ctx, tkey)
	if err != nil {
		return fmt.Errorf("获取类型 ID 追踪键失败: %w", err)
	}
	tval, _ := readKVValue(traw)
	newTVal := removeID(tval, memID)
	if newTVal != "" {
		if err := s.kvStore.Set(ctx, tkey, writeKVValue(newTVal)); err != nil {
			return fmt.Errorf("更新类型 ID 追踪键失败: %w", err)
		}
	} else {
		if err := s.kvStore.Delete(ctx, tkey); err != nil {
			return fmt.Errorf("删除类型 ID 追踪键失败: %w", err)
		}
	}

	return nil
}

// kvDataToMemoryDoc 将 KV 存储的 JSON 数据转换为 MemoryDoc。
// 对齐 Python _kv_data_to_memory_doc，支持多种时间戳格式解析：
//   - 字符串格式："2006-01-02 15-04-05"（旧格式）和 "2006-01-02 15:04:05"（标准格式）
//   - Unix 时间戳（int64/float64）
//   - ISO 8601 格式
//   - 以上均失败时使用当前 UTC 时间
func kvDataToMemoryDoc(data map[string]any, memID string) *MemoryDoc {
	skip := map[string]bool{"id": true, "mem": true, "mem_type": true, "timestamp": true, "user_id": true, "scope_id": true}
	extra := make(map[string]any)
	for k, v := range data {
		if !skip[k] {
			extra[k] = v
		}
	}

	var timestamp time.Time
	tsRaw, ok := data["timestamp"]
	if !ok || tsRaw == nil {
		timestamp = time.Now().UTC()
	} else {
		switch ts := tsRaw.(type) {
		case time.Time:
			timestamp = ts
		case string:
			if ts == "" {
				timestamp = time.Now().UTC()
			} else {
				// 尝试旧格式 "2006-01-02 15-04-05"
				parsed, err := time.Parse("2006-01-02 15-04-05", ts)
				if err == nil {
					timestamp = parsed.UTC()
				} else {
					// 尝试标准格式 "2006-01-02 15:04:05"
					parsed, err = time.Parse("2006-01-02 15:04:05", ts)
					if err == nil {
						timestamp = parsed.UTC()
					} else {
						// 尝试 ISO 8601
						parsed, err = time.Parse(time.RFC3339, ts)
						if err == nil {
							timestamp = parsed.UTC()
						} else {
							// 尝试 time.Parse 兼容的 ISO 格式
							parsed, err = time.Parse("2006-01-02T15:04:05", ts)
							if err == nil {
								timestamp = parsed.UTC()
							} else {
								timestamp = time.Now().UTC()
							}
						}
					}
				}
			}
		case float64:
			// JSON 数字反序列化为 float64
			timestamp = time.Unix(int64(ts), 0).UTC()
		case int64:
			timestamp = time.Unix(ts, 0).UTC()
		default:
			timestamp = time.Now().UTC()
		}
	}

	mem, _ := data["mem"].(string)
	memType, _ := data["mem_type"].(string)

	return &MemoryDoc{
		ID:        memID,
		Text:      mem,
		Type:      memType,
		Timestamp: timestamp,
		Fields:    extra,
	}
}

// memoryDocToKVData 将 MemoryDoc 转换为 KV 存储的 JSON 数据。
// 对齐 Python _memory_doc_to_kv_data：
//   - KV 字段名对齐 Python：id, user_id, scope_id, mem, mem_type, timestamp
//   - timestamp 格式使用旧兼容格式 "2006-01-02 15-04-05"
//   - doc.Fields 合并到输出字典中
func memoryDocToKVData(doc *MemoryDoc, userID, scopeID string) map[string]any {
	ts := doc.Timestamp.Format("2006-01-02 15-04-05")
	if doc.Timestamp.IsZero() {
		ts = time.Now().UTC().Format("2006-01-02 15-04-05")
	}

	result := map[string]any{
		"id":        doc.ID,
		"user_id":   userID,
		"scope_id":  scopeID,
		"mem":       doc.Text,
		"mem_type":  doc.Type,
		"timestamp": ts,
	}
	for k, v := range doc.Fields {
		result[k] = v
	}
	return result
}

// getCollectionName 构建向量集合名称：uid_{userID}_gid_{scopeID}_mtype_{memType}
// 对齐 Python _get_collection_name。
func getCollectionName(userID, scopeID, memType string) string {
	return fmt.Sprintf("uid_%s_gid_%s_mtype_%s", userID, scopeID, memType)
}

// parseMemTypeFromCollection 从集合名称中提取 memType。
// 对齐 Python _parse_mem_type_from_collection。
func parseMemTypeFromCollection(name string) string {
	if !strings.Contains(name, "_mtype_") {
		return ""
	}
	parts := strings.SplitN(name, "_mtype_", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// ensureCollection 懒创建向量集合，已创建则跳过。
// 对齐 Python _ensure_collection，集合 Schema 包含：
//   - id: VARCHAR(256), 主键
//   - embedding: FLOAT_VECTOR(dim)
func (s *SimpleMemoryIndex) ensureCollection(ctx context.Context, name string, dim int) error {
	s.mu.RLock()
	if s.createdCollections[name] {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	exists, err := s.vectorStore.CollectionExists(ctx, name)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection", name).Msg("检查集合是否存在失败")
		return fmt.Errorf("检查集合是否存在失败: %w", err)
	}
	if exists {
		s.mu.Lock()
		s.createdCollections[name] = true
		s.mu.Unlock()
		return nil
	}

	idField, err := vector.NewFieldSchema("id", vector.VectorDataTypeVarchar,
		vector.WithPrimary(),
		vector.WithMaxLength(256),
	)
	if err != nil {
		return fmt.Errorf("创建 id 字段 Schema 失败: %w", err)
	}
	embField, err := vector.NewFieldSchema("embedding", vector.VectorDataTypeFloatVector,
		vector.WithDim(dim),
	)
	if err != nil {
		return fmt.Errorf("创建 embedding 字段 Schema 失败: %w", err)
	}
	schema, err := vector.NewCollectionSchemaFromFields([]*vector.FieldSchema{idField, embField},
		vector.WithCollectionDescription("Semantic memory collection"),
	)
	if err != nil {
		return fmt.Errorf("创建集合 Schema 失败: %w", err)
	}

	if err := s.vectorStore.CreateCollection(ctx, name, schema); err != nil {
		logger.Error(logComponent).Err(err).Str("collection", name).Msg("创建向量集合失败")
		return fmt.Errorf("创建向量集合失败: %w", err)
	}

	s.mu.Lock()
	s.createdCollections[name] = true
	s.mu.Unlock()
	return nil
}

// collectionsFor 列出匹配 userID+scopeID 前缀的所有向量集合。
// 对齐 Python _collections_for。
func (s *SimpleMemoryIndex) collectionsFor(ctx context.Context, userID, scopeID string) ([]string, error) {
	prefix := fmt.Sprintf("uid_%s_gid_%s_mtype_", userID, scopeID)
	names, err := s.vectorStore.ListCollectionNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("列出向量集合失败: %w", err)
	}
	var result []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			result = append(result, n)
		}
	}
	return result, nil
}

// sortSearchResultsByScore 按 Score 降序排序搜索结果。
func sortSearchResultsByScore(results []*MemorySearchResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}

// sortDocsByTypeAndTime 按 memType 顺序排列，同类型按时间戳降序。
func sortDocsByTypeAndTime(docs []*MemoryDoc, typeOrder map[string]int) {
	for i := 1; i < len(docs); i++ {
		for j := i; j > 0; j-- {
			orderJ := typeOrder[docs[j].Type]
			orderJ1 := typeOrder[docs[j-1].Type]
			ordLen := len(typeOrder)
			if orderJ >= ordLen {
				orderJ = ordLen
			}
			if orderJ1 >= ordLen {
				orderJ1 = ordLen
			}
			if orderJ < orderJ1 || (orderJ == orderJ1 && docs[j].Timestamp.After(docs[j-1].Timestamp)) {
				docs[j], docs[j-1] = docs[j-1], docs[j]
			} else {
				break
			}
		}
	}
}
