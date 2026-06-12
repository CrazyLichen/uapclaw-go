# SimpleMemoryIndex 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 SimpleMemoryIndex（步骤 4.18），基于 KV + Vector 双存储的记忆索引，同时前置定义 BaseEmbedding 接口。

**Architecture:** SimpleMemoryIndex 嵌入 `*MemoryIndexBase` 继承 7 个默认方法，组合 `BaseKVStore` + `BaseVectorStore` + `BaseEmbedding` 三个依赖。写入时 KV 存完整文档、Vector 存嵌入向量；搜索时 Vector 命中 → KV 取内容。BaseEmbedding 接口独立为 `store/embedding/` 包，仅定义 3 个方法，具体实现留给 4.19-4.22。

**Tech Stack:** Go 1.22+, 标准库 `encoding/json`/`sync`/`context`/`fmt`/`strings`/`time`, 已有包 `kv`/`vector`/`index`/`logger`/`exception`

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/store/embedding/doc.go` | 包文档 |
| 创建 | `internal/agentcore/store/embedding/base.go` | BaseEmbedding 接口定义 |
| 创建 | `internal/agentcore/store/embedding/base_test.go` | 接口编译约束测试 |
| 创建 | `internal/agentcore/store/index/simple.go` | SimpleMemoryIndex 实现 |
| 创建 | `internal/agentcore/store/index/simple_test.go` | SimpleMemoryIndex 单元测试 |
| 修改 | `internal/agentcore/store/index/doc.go` | 添加 simple.go 到文件目录 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 4.18/4.19 行添加回填标记 |

---

### Task 1: 创建 embedding 包 — doc.go + BaseEmbedding 接口

**Files:**
- Create: `internal/agentcore/store/embedding/doc.go`
- Create: `internal/agentcore/store/embedding/base.go`
- Create: `internal/agentcore/store/embedding/base_test.go`

- [ ] **Step 1: 创建 embedding/doc.go**

```go
// Package embedding 提供向量嵌入模型的抽象接口。
//
// 本包定义了 BaseEmbedding 接口，提供文本到向量的转换能力，
// 供记忆索引等组件进行语义搜索。
//
// 文件目录：
//
//	embedding/
//	├── doc.go           # 包文档
//	├── base.go          # BaseEmbedding 接口定义
//	└── base_test.go     # 单元测试
//	⤵️ 预留：4.19-4.22 实现后回填具体实现文件条目
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_embedding.py
//
// 核心类型/接口索引：
//
//	BaseEmbedding — 向量嵌入模型抽象接口（EmbedQuery/EmbedDocuments/Dimension）
package embedding
```

- [ ] **Step 2: 创建 embedding/base.go**

```go
package embedding

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：BaseEmbedding 是接口，归到结构体区块（接口排在结构体之前）：

```go
package embedding

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// BaseEmbedding 向量嵌入模型的抽象接口。
//
// 提供文本到向量的转换能力，供记忆索引等组件进行语义搜索。
// ⤵️ 预留：4.19-4.22 补充具体实现（OpenAI/DashScope/VLLM/API）
//
// 对应 Python: openjiuwen/core/foundation/store/base_embedding.py (Embedding)
type BaseEmbedding interface {
	// EmbedQuery 将单条查询文本转换为向量。
	EmbedQuery(ctx context.Context, text string) ([]float64, error)

	// EmbedDocuments 将多条文档文本批量转换为向量。
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)

	// Dimension 返回嵌入向量的维度。
	Dimension() int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 创建 embedding/base_test.go**

```go
package embedding

import "testing"

// fakeEmbedding 用于测试的模拟嵌入模型
type fakeEmbedding struct {
	dimension int
}

func newFakeEmbedding(dim int) *fakeEmbedding {
	return &fakeEmbedding{dimension: dim}
}

func (f *fakeEmbedding) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	vec := make([]float64, f.dimension)
	for i := range vec {
		vec[i] = float64(len(text) + i)
	}
	return vec, nil
}

func (f *fakeEmbedding) EmbedDocuments(_ context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		vec := make([]float64, f.dimension)
		for j := range vec {
			vec[j] = float64(len(text) + j + i)
		}
		result[i] = vec
	}
	return result, nil
}

func (f *fakeEmbedding) Dimension() int {
	return f.dimension
}

func TestBaseEmbedding_接口约束(t *testing.T) {
	// 验证 fakeEmbedding 满足 BaseEmbedding 接口
	var _ BaseEmbedding = &fakeEmbedding{}
}
```

注意：需要添加 `"context"` 到 import。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/embedding/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/embedding/
git commit -m "feat(store): 前置定义 BaseEmbedding 接口（4.18 依赖 4.19 接口部分）
```

---

### Task 2: 创建 SimpleMemoryIndex 结构体、常量、构造函数和辅助方法

**Files:**
- Create: `internal/agentcore/store/index/simple.go`

- [ ] **Step 1: 创建 simple.go — 结构体 + 常量 + 构造函数 + SetEmbeddingModel + SetStorageCodec + KV 键构建 + ID 拼接解析 + KV 值读写**

```go
package index

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector"
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
	// embeddingModel 嵌入模型
	// ⤵️ 预留：4.19-4.22 实现后可注入具体实现
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
// 对齐 Python set_storage_codec。
func (s *SimpleMemoryIndex) SetStorageCodec(codec StorageCodec) {
	s.codec = codec
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

// readKVValue 将 KV 存储的 []byte 值解码为字符串，nil 返回空字符串。
func readKVValue(raw []byte) string {
	if raw == nil {
		return ""
	}
	return string(raw)
}

// writeKVValue 将字符串编码为 []byte 写入 KV 存储。
func writeKVValue(text string) []byte {
	return []byte(text)
}

// addIDToTracking 将 ID 添加到全局和类型追踪键中。
// 对齐 Python _add_id_to_tracking。
func (s *SimpleMemoryIndex) addIDToTracking(ctx context.Context, userID, scopeID, memID, memType string) error {
	// 全局 ID 追踪
	key := kvIDsKey(userID, scopeID)
	raw, err := s.kvStore.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("获取全局 ID 追踪键失败: %w", err)
	}
	val := readKVValue(raw)
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
	tval := readKVValue(traw)
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
	key := kvIDsKey(userID, scopeID)
	raw, err := s.kvStore.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("获取全局 ID 追踪键失败: %w", err)
	}
	val := readKVValue(raw)
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
	tval := readKVValue(traw)
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
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/index/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/simple.go
git commit -m "feat(store): 添加 SimpleMemoryIndex 结构体、常量和 KV/ID 辅助方法"
```

---

### Task 3: 添加数据转换和 Vector 辅助方法到 simple.go

**Files:**
- Modify: `internal/agentcore/store/index/simple.go`

- [ ] **Step 1: 在 simple.go 的非导出函数区块末尾添加数据转换方法**

追加到 `removeIDFromTracking` 之后：

```go

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
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/index/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/simple.go
git commit -m "feat(store): 添加 SimpleMemoryIndex 数据转换和 Vector 辅助方法"
```

---

### Task 4: 添加核心方法 — AddMemories, Search, UpdateMemories

**Files:**
- Modify: `internal/agentcore/store/index/simple.go`

- [ ] **Step 1: 在导出函数区块（SetStorageCodec 之后）添加 AddMemories、Search、UpdateMemories**

```go

// AddMemories 添加新的记忆文档。
// 对齐 Python add_memories：按类型分组 → 嵌入 → 写入 Vector → 写入 KV → ID 追踪。
func (s *SimpleMemoryIndex) AddMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error {
	if len(memories) == 0 {
		return nil
	}

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
		embeddings, err := s.embeddingModel.EmbedDocuments(ctx, texts)
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
		if len(embeddings) > 0 {
			if err := s.ensureCollection(ctx, col, len(embeddings[0])); err != nil {
				return err
			}
		}

		// 写入向量存储
		vecDocs := make([]map[string]any, len(docs))
		for i, d := range docs {
			vecDocs[i] = map[string]any{
				"id":        d.ID,
				"embedding": embeddings[i],
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
		for i, doc := range docs {
			kvKey := kvMemKey(userID, scopeID, doc.ID)
			kvData := memoryDocToKVData(doc, userID, scopeID)
			if s.codec != nil {
				if mem, ok := kvData["mem"].(string); ok {
					kvData["mem"] = s.codec.Encode(mem)
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
				// ID 追踪失败不阻断主流程，记录警告
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
		return nil, nil
	}

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

	var results []*MemorySearchResult
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
			decoded := readKVValue(values[i])
			if decoded == "" {
				continue
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(decoded), &data); err != nil {
				continue
			}
			if s.codec != nil {
				if mem, ok := data["mem"].(string); ok {
					data["mem"] = s.codec.Decode(mem)
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

// sortSearchResultsByScore 按 Score 降序排序搜索结果。
func sortSearchResultsByScore(results []*MemorySearchResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
```

注意：需要确认 `sort` 包是否需要导入。这里使用手写插入排序，不需要 `sort` 包。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/index/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/simple.go
git commit -m "feat(store): 添加 SimpleMemoryIndex AddMemories/Search/UpdateMemories"
```

---

### Task 5: 添加核心方法 — DeleteMemories, DeleteByUser, DeleteByScope, DeleteByUserAndScope, GetByID, ListMemories, ListUserScopes

**Files:**
- Modify: `internal/agentcore/store/index/simple.go`

- [ ] **Step 1: 在 UpdateMemories 之后添加 Delete 系列方法和 GetByID**

```go

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
			// 向量删除失败不阻断，记录日志继续
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
				continue
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
				continue
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
			continue
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
	if s.codec != nil {
		if mem, ok := data["mem"].(string); ok {
			data["mem"] = s.codec.Decode(mem)
		}
	}
	return kvDataToMemoryDoc(data, memID), nil
}
```

- [ ] **Step 2: 添加 ListMemories 和 ListUserScopes（覆盖基类空实现）**

```go

// ListMemories 分页获取记忆文档列表。
// 覆盖 MemoryIndexBase 的空实现。
// 对齐 Python list_memories：ID 追踪 → KV 批量获取 → 过滤 → 排序 → 分页。
func (s *SimpleMemoryIndex) ListMemories(ctx context.Context, userID string, scopeID string, offset int, limit int, memTypes []string) ([]*MemoryDoc, error) {
	idsKey := kvIDsKey(userID, scopeID)
	raw, err := s.kvStore.Get(ctx, idsKey)
	if err != nil {
		return nil, fmt.Errorf("获取 ID 追踪键失败: %w", err)
	}
	val := readKVValue(raw)
	if val == "" {
		return nil, nil
	}

	allIDs := parseAllIDs(val)
	if len(allIDs) == 0 {
		return nil, nil
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
		decoded := readKVValue(values[i])
		if decoded == "" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(decoded), &data); err != nil {
			continue
		}
		if s.codec != nil {
			if mem, ok := data["mem"].(string); ok {
				data["mem"] = s.codec.Decode(mem)
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
		return nil, nil
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
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/index/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/index/simple.go
git commit -m "feat(store): 添加 SimpleMemoryIndex 全部核心方法实现"
```

---

### Task 6: 更新 index/doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/store/index/doc.go`

- [ ] **Step 1: 更新 doc.go 的文件目录和核心类型索引**

将 doc.go 的内容更新为：

```go
// Package index 提供记忆索引的抽象接口和数据模型。
//
// 本包定义了所有记忆索引实现必须满足的 BaseMemoryIndex 接口，
// 以及 MemoryDoc 数据模型、StorageCodec 编解码器接口、
// MemorySearchResult 搜索结果类型和 MemoryIndexBase 默认实现基类。
// 具体实现类（如 SimpleMemoryIndex）嵌入 MemoryIndexBase 后
// 只需实现核心抽象方法即可满足 BaseMemoryIndex 接口。
//
// 文件目录：
//
//	index/
//	├── doc.go           # 包文档
//	├── base.go          # StorageCodec + MemoryDoc + MemorySearchResult + UserScope + BaseMemoryIndex + MemoryIndexBase
//	├── base_test.go     # 基类单元测试
//	├── simple.go        # SimpleMemoryIndex 简单记忆索引实现
//	└── simple_test.go   # SimpleMemoryIndex 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_memory_index.py
//	openjiuwen/core/foundation/store/index/simple_memory_index.py
//
// 核心类型/接口索引：
//
//	StorageCodec        — 存储编解码器接口，用于对记忆文本加解密
//	MemoryDoc           — 记忆文档数据模型（ID/Text/Type/Timestamp/Fields）
//	MemorySearchResult  — 搜索结果，包含 MemoryDoc 和相关度分数
//	UserScope           — 用户-作用域对，ListUserScopes 返回值
//	BaseMemoryIndex     — 记忆索引抽象接口（16 个方法）
//	MemoryIndexBase     — 默认实现基类，提供 7 个非抽象方法的通用行为
//	SimpleMemoryIndex   — 简单记忆索引，KV + Vector 双存储实现
package index
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/store/index/doc.go
git commit -m "docs(store): 更新 index 包 doc.go 文件目录"
```

---

### Task 7: 创建 SimpleMemoryIndex 单元测试 — mock 基础设施 + 辅助方法测试

**Files:**
- Create: `internal/agentcore/store/index/simple_test.go`

- [ ] **Step 1: 创建 simple_test.go — fake 依赖 + 构造函数/辅助方法测试**

```go
package index

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector"
)

// ──────────────────────────── fake 依赖 ────────────────────────────

// fakeKVStore 用于测试的模拟 KV 存储
type fakeKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newFakeKVStore() *fakeKVStore {
	return &fakeKVStore{data: make(map[string][]byte)}
}

func (f *fakeKVStore) Set(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}

func (f *fakeKVStore) ExclusiveSet(_ context.Context, key string, value []byte, _ int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[key]; ok {
		return false, nil
	}
	f.data[key] = value
	return true, nil
}

func (f *fakeKVStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.data[key], nil
}

func (f *fakeKVStore) Exists(_ context.Context, key string) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.data[key]
	return ok, nil
}

func (f *fakeKVStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

func (f *fakeKVStore) GetByPrefix(_ context.Context, prefix string) (map[string][]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make(map[string][]byte)
	for k, v := range f.data {
		if strings.HasPrefix(k, prefix) {
			result[k] = v
		}
	}
	return result, nil
}

func (f *fakeKVStore) DeleteByPrefix(_ context.Context, prefix string, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			delete(f.data, k)
		}
	}
	return nil
}

func (f *fakeKVStore) MGet(_ context.Context, keys []string) ([][]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([][]byte, len(keys))
	for i, k := range keys {
		result[i] = f.data[k]
	}
	return result, nil
}

func (f *fakeKVStore) BatchDelete(_ context.Context, keys []string, _ int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, k := range keys {
		if _, ok := f.data[k]; ok {
			delete(f.data, k)
			count++
		}
	}
	return count, nil
}

func (f *fakeKVStore) Pipeline(_ context.Context) kv.KVPipeline {
	// 测试中不使用 Pipeline
	return nil
}

// fakeVectorStore 用于测试的模拟向量存储
type fakeVectorStore struct {
	mu          sync.RWMutex
	collections map[string]map[string]map[string]any // 集合名 → 文档ID → 字段
	schemas     map[string]*vector.CollectionSchema
}

func newFakeVectorStore() *fakeVectorStore {
	return &fakeVectorStore{
		collections: make(map[string]map[string]map[string]any),
		schemas:     make(map[string]*vector.CollectionSchema),
	}
}

func (f *fakeVectorStore) CreateCollection(_ context.Context, name string, schema *vector.CollectionSchema, _ ...vector.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.collections[name]; ok {
		return fmt.Errorf("collection %s already exists", name)
	}
	f.collections[name] = make(map[string]map[string]any)
	f.schemas[name] = schema
	return nil
}

func (f *fakeVectorStore) DeleteCollection(_ context.Context, name string, _ ...vector.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.collections, name)
	delete(f.schemas, name)
	return nil
}

func (f *fakeVectorStore) CollectionExists(_ context.Context, name string, _ ...vector.Option) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.collections[name]
	return ok, nil
}

func (f *fakeVectorStore) GetSchema(_ context.Context, name string, _ ...vector.Option) (*vector.CollectionSchema, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.schemas[name], nil
}

func (f *fakeVectorStore) AddDocs(_ context.Context, collectionName string, docs []map[string]any, _ ...vector.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	col, ok := f.collections[collectionName]
	if !ok {
		return fmt.Errorf("collection %s not found", collectionName)
	}
	for _, doc := range docs {
		id, _ := doc["id"].(string)
		docCopy := make(map[string]any)
		for k, v := range doc {
			docCopy[k] = v
		}
		col[id] = docCopy
	}
	return nil
}

func (f *fakeVectorStore) Search(_ context.Context, collectionName string, _ []float64, _ string, topK int, _ map[string]any, _ ...vector.Option) ([]vector.VectorSearchResult, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	col, ok := f.collections[collectionName]
	if !ok {
		return nil, nil
	}
	var results []vector.VectorSearchResult
	count := 0
	for id, fields := range col {
		if count >= topK {
			break
		}
		resultFields := make(map[string]any)
		for k, v := range fields {
			resultFields[k] = v
		}
		resultFields["id"] = id
		results = append(results, vector.VectorSearchResult{
			Score:  0.9 - float64(count)*0.1,
			Fields: resultFields,
		})
		count++
	}
	// 按分数排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results, nil
}

func (f *fakeVectorStore) DeleteDocsByIDs(_ context.Context, collectionName string, ids []string, _ ...vector.Option) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	col, ok := f.collections[collectionName]
	if !ok {
		return nil
	}
	for _, id := range ids {
		delete(col, id)
	}
	return nil
}

func (f *fakeVectorStore) DeleteDocsByFilters(_ context.Context, _ string, _ map[string]any, _ ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) ListCollectionNames(_ context.Context) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var names []string
	for name := range f.collections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (f *fakeVectorStore) UpdateSchema(_ context.Context, _ string, _ []any, _ ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) UpdateCollectionMetadata(_ context.Context, _ string, _ map[string]any, _ ...vector.Option) error {
	return nil
}

func (f *fakeVectorStore) GetCollectionMetadata(_ context.Context, _ string, _ ...vector.Option) (map[string]any, error) {
	return nil, nil
}

// fakeEmbedding 用于测试的模拟嵌入模型
type fakeEmbedding struct {
	dimension int
}

func newFakeEmbedding(dim int) *fakeEmbedding {
	return &fakeEmbedding{dimension: dim}
}

func (f *fakeEmbedding) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	vec := make([]float64, f.dimension)
	for i := range vec {
		vec[i] = float64(len(text) + i)
	}
	return vec, nil
}

func (f *fakeEmbedding) EmbedDocuments(_ context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		vec := make([]float64, f.dimension)
		for j := range vec {
			vec[j] = float64(len(text) + j + i)
		}
		result[i] = vec
	}
	return result, nil
}

func (f *fakeEmbedding) Dimension() int {
	return f.dimension
}

// 编译时验证
var _ embedding.BaseEmbedding = &fakeEmbedding{}
var _ kv.BaseKVStore = &fakeKVStore{}
var _ vector.BaseVectorStore = &fakeVectorStore{}

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestIndex 创建测试用 SimpleMemoryIndex
func newTestIndex() *SimpleMemoryIndex {
	return NewSimpleMemoryIndex(newFakeKVStore(), newFakeVectorStore(), newFakeEmbedding(4))
}

// newTestIndexWithNilEmbedding 创建无嵌入模型的测试索引
func newTestIndexWithNilEmbedding() *SimpleMemoryIndex {
	return NewSimpleMemoryIndex(newFakeKVStore(), newFakeVectorStore(), nil)
}

// padID 生成 24 字节固定宽度 ID
func padID(id string) string {
	for len(id) < byteNumPerID {
		id = id + "0"
	}
	if len(id) > byteNumPerID {
		id = id[:byteNumPerID]
	}
	return id
}

// ──────────────────────────── 构造函数测试 ────────────────────────────

func TestNewSimpleMemoryIndex(t *testing.T) {
	idx := newTestIndex()
	if idx == nil {
		t.Fatal("NewSimpleMemoryIndex 返回 nil")
	}
	if idx.kvStore == nil {
		t.Error("kvStore 不应为 nil")
	}
	if idx.vectorStore == nil {
		t.Error("vectorStore 不应为 nil")
	}
	if idx.embeddingModel == nil {
		t.Error("embeddingModel 不应为 nil")
	}
	if idx.createdCollections == nil {
		t.Error("createdCollections 不应为 nil")
	}
}

func TestSetEmbeddingModel(t *testing.T) {
	idx := newTestIndex()
	newModel := newFakeEmbedding(8)
	idx.SetEmbeddingModel(newModel)
	if idx.embeddingModel.Dimension() != 8 {
		t.Errorf("替换后维度应为 8, 实际 %d", idx.embeddingModel.Dimension())
	}
}

func TestSetStorageCodec(t *testing.T) {
	idx := newTestIndex()
	codec := newFakeCodec()
	idx.SetStorageCodec(codec)
	if idx.codec == nil {
		t.Error("codec 不应为 nil")
	}
}

// ──────────────────────────── KV 辅助方法测试 ────────────────────────────

func TestKVHelper_KVMemKey(t *testing.T) {
	key := kvMemKey("user1", "scope1", "mem001")
	expected := "UMD/user1/scope1/mem001"
	if key != expected {
		t.Errorf("kvMemKey: 期望 %q, 实际 %q", expected, key)
	}
}

func TestKVHelper_KVIDsKey(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		scopeID  string
		memType  string
		expected string
	}{
		{"全局追踪", "user1", "scope1", "", "UMD/user1/scope1/ids"},
		{"按类型追踪", "user1", "scope1", "profile", "UMD/user1/scope1/profile/ids"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kvIDsKey(tt.userID, tt.scopeID, tt.memType)
			if result != tt.expected {
				t.Errorf("kvIDsKey(%q, %q, %q): 期望 %q, 实际 %q", tt.userID, tt.scopeID, tt.memType, tt.expected, result)
			}
		})
	}
}

func TestKVHelper_ParseAllIDs(t *testing.T) {
	id1 := padID("id001")
	id2 := padID("id002")
	raw := id1 + id2
	ids := parseAllIDs(raw)
	if len(ids) != 2 {
		t.Fatalf("parseAllIDs: 期望 2 个 ID, 实际 %d", len(ids))
	}
	if ids[0] != id1 {
		t.Errorf("ids[0]: 期望 %q, 实际 %q", id1, ids[0])
	}
	if ids[1] != id2 {
		t.Errorf("ids[1]: 期望 %q, 实际 %q", id2, ids[1])
	}
}

func TestKVHelper_ParseAllIDs_空字符串(t *testing.T) {
	ids := parseAllIDs("")
	if len(ids) != 0 {
		t.Errorf("空字符串应返回空切片, 实际 %d 个", len(ids))
	}
}

func TestKVHelper_AppendID(t *testing.T) {
	id1 := padID("id001")
	id2 := padID("id002")
	result := appendID(id1, id2)
	if len(result) != byteNumPerID*2 {
		t.Errorf("appendID: 期望长度 %d, 实际 %d", byteNumPerID*2, len(result))
	}
	if result != id1+id2 {
		t.Errorf("appendID: 结果不匹配")
	}
}

func TestKVHelper_RemoveID(t *testing.T) {
	id1 := padID("id001")
	id2 := padID("id002")
	id3 := padID("id003")
	raw := id1 + id2 + id3
	result := removeID(raw, id2)
	expected := id1 + id3
	if result != expected {
		t.Errorf("removeID: 期望 %q, 实际 %q", expected, result)
	}
}

func TestKVHelper_RemoveID_不存在(t *testing.T) {
	id1 := padID("id001")
	other := padID("id999")
	raw := id1
	result := removeID(raw, other)
	if result != raw {
		t.Errorf("removeID 不存在的 ID 应返回原字符串")
	}
}

// ──────────────────────────── 数据转换测试 ────────────────────────────

func TestKVHelper_KVDataToMemoryDoc_多种时间戳格式(t *testing.T) {
	tests := []struct {
		name   string
		data   map[string]any
		memID  string
		expect func(*testing.T, *MemoryDoc)
	}{
		{
			name:  "旧格式时间戳",
			data:  map[string]any{"mem": "测试", "mem_type": "type1", "timestamp": "2025-07-25 10-30-00"},
			memID: "mem001",
			expect: func(t *testing.T, doc *MemoryDoc) {
				if doc.ID != "mem001" {
					t.Errorf("ID: 期望 mem001, 实际 %q", doc.ID)
				}
				if doc.Text != "测试" {
					t.Errorf("Text: 期望 测试, 实际 %q", doc.Text)
				}
			},
		},
		{
			name:  "标准格式时间戳",
			data:  map[string]any{"mem": "hello", "mem_type": "type2", "timestamp": "2025-07-25 10:30:00"},
			memID: "mem002",
			expect: func(t *testing.T, doc *MemoryDoc) {
				if doc.Text != "hello" {
					t.Errorf("Text: 期望 hello, 实际 %q", doc.Text)
				}
			},
		},
		{
			name:  "无时间戳",
			data:  map[string]any{"mem": "no_ts", "mem_type": "type3"},
			memID: "mem003",
			expect: func(t *testing.T, doc *MemoryDoc) {
				if doc.Timestamp.IsZero() {
					t.Error("无时间戳时应使用当前时间")
				}
			},
		},
		{
			name:  "扩展字段",
			data:  map[string]any{"mem": "ext", "mem_type": "type4", "priority": "high", "count": float64(42)},
			memID: "mem004",
			expect: func(t *testing.T, doc *MemoryDoc) {
				if doc.Fields["priority"] != "high" {
					t.Errorf("Fields[priority]: 期望 high, 实际 %v", doc.Fields["priority"])
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := kvDataToMemoryDoc(tt.data, tt.memID)
			tt.expect(t, doc)
		})
	}
}

func TestKVHelper_MemoryDocToKVData(t *testing.T) {
	ts := time.Date(2025, 7, 25, 10, 30, 0, 0, time.UTC)
	doc := &MemoryDoc{
		ID:        "mem001",
		Text:      "测试内容",
		Type:      "profile",
		Timestamp: ts,
		Fields:    map[string]any{"priority": "high"},
	}
	data := memoryDocToKVData(doc, "user1", "scope1")

	if data["id"] != "mem001" {
		t.Errorf("id: 期望 mem001, 实际 %v", data["id"])
	}
	if data["user_id"] != "user1" {
		t.Errorf("user_id: 期望 user1, 实际 %v", data["user_id"])
	}
	if data["scope_id"] != "scope1" {
		t.Errorf("scope_id: 期望 scope1, 实际 %v", data["scope_id"])
	}
	if data["mem"] != "测试内容" {
		t.Errorf("mem: 期望 测试内容, 实际 %v", data["mem"])
	}
	if data["mem_type"] != "profile" {
		t.Errorf("mem_type: 期望 profile, 实际 %v", data["mem_type"])
	}
	if data["priority"] != "high" {
		t.Errorf("priority: 期望 high, 实际 %v", data["priority"])
	}
	tsStr, _ := data["timestamp"].(string)
	if tsStr != "2025-07-25 10-30-00" {
		t.Errorf("timestamp: 期望 2025-07-25 10-30-00, 实际 %q", tsStr)
	}
}

// ──────────────────────────── Vector 辅助方法测试 ────────────────────────────

func TestGetCollectionName(t *testing.T) {
	result := getCollectionName("user1", "scope1", "profile")
	expected := "uid_user1_gid_scope1_mtype_profile"
	if result != expected {
		t.Errorf("getCollectionName: 期望 %q, 实际 %q", expected, result)
	}
}

func TestParseMemTypeFromCollection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"正常解析", "uid_user1_gid_scope1_mtype_profile", "profile"},
		{"无 mtype", "uid_user1_gid_scope1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMemTypeFromCollection(tt.input)
			if result != tt.expected {
				t.Errorf("parseMemTypeFromCollection(%q): 期望 %q, 实际 %q", tt.input, tt.expected, result)
			}
		})
	}
}

func TestEnsureCollection_懒创建(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	// 第一次创建
	if err := idx.ensureCollection(ctx, "test_col", 4); err != nil {
		t.Fatalf("第一次 ensureCollection 失败: %v", err)
	}
	if !idx.createdCollections["test_col"] {
		t.Error("createdCollections 应包含 test_col")
	}

	// 第二次应跳过
	if err := idx.ensureCollection(ctx, "test_col", 4); err != nil {
		t.Fatalf("第二次 ensureCollection 失败: %v", err)
	}
}

func TestReadKVValue(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{"nil 返回空", nil, ""},
		{"正常值", []byte("hello"), "hello"},
		{"空字节", []byte{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := readKVValue(tt.input)
			if result != tt.expected {
				t.Errorf("readKVValue: 期望 %q, 实际 %q", tt.expected, result)
			}
		})
	}
}
```

- [ ] **Step 2: 运行辅助方法测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/index/... -run "TestKVHelper|TestGetCollectionName|TestParseMemTypeFromCollection|TestEnsureCollection|TestNewSimpleMemoryIndex|TestSetEmbeddingModel|TestSetStorageCodec|TestReadKVValue" -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/simple_test.go
git commit -m "test(store): 添加 SimpleMemoryIndex 辅助方法和构造函数测试"
```

---

### Task 8: 添加核心方法测试 — AddMemories, Search, UpdateMemories

**Files:**
- Modify: `internal/agentcore/store/index/simple_test.go`

- [ ] **Step 1: 在 simple_test.go 末尾追加 AddMemories/Search/UpdateMemories 测试**

```go

// ──────────────────────────── AddMemories 测试 ────────────────────────────

func TestAddMemories_正常添加(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "用户偏好深色模式", Type: "profile", Timestamp: time.Now().UTC()},
		{ID: padID("mem002"), Text: "用户喜欢 Python", Type: "profile", Timestamp: time.Now().UTC()},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err != nil {
		t.Fatalf("AddMemories 失败: %v", err)
	}

	// 验证 KV 存储
	kvStore := idx.kvStore.(*fakeKVStore)
	kvStore.mu.RLock()
	key1 := kvMemKey("user1", "scope1", padID("mem001"))
	if _, ok := kvStore.data[key1]; !ok {
		t.Error("KV 中应存在 mem001")
	}
	kvStore.mu.RUnlock()

	// 验证 ID 追踪
	idsKey := kvIDsKey("user1", "scope1")
	raw, _ := idx.kvStore.Get(ctx, idsKey)
	val := readKVValue(raw)
	ids := parseAllIDs(val)
	if len(ids) != 2 {
		t.Errorf("全局 ID 追踪应有 2 个 ID, 实际 %d", len(ids))
	}
}

func TestAddMemories_多类型分组(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "偏好深色", Type: "profile", Timestamp: time.Now().UTC()},
		{ID: padID("mem002"), Text: "记住用户名", Type: "fact", Timestamp: time.Now().UTC()},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err != nil {
		t.Fatalf("AddMemories 失败: %v", err)
	}

	// 验证 Vector 存储中有两个集合
	vecStore := idx.vectorStore.(*fakeVectorStore)
	vecStore.mu.RLock()
	col1 := getCollectionName("user1", "scope1", "profile")
	col2 := getCollectionName("user1", "scope1", "fact")
	if _, ok := vecStore.collections[col1]; !ok {
		t.Error("应存在 profile 集合")
	}
	if _, ok := vecStore.collections[col2]; !ok {
		t.Error("应存在 fact 集合")
	}
	vecStore.mu.RUnlock()
}

func TestAddMemories_EmbeddingModel为nil时返回错误(t *testing.T) {
	idx := newTestIndexWithNilEmbedding()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "测试", Type: "profile", Timestamp: time.Now().UTC()},
	}

	err := idx.AddMemories(ctx, "user1", "scope1", docs)
	if err == nil {
		t.Fatal("embeddingModel 为 nil 时应返回错误")
	}
}

func TestAddMemories_空列表(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	err := idx.AddMemories(ctx, "user1", "scope1", nil)
	if err != nil {
		t.Errorf("空列表应返回 nil, 实际 %v", err)
	}
}

// ──────────────────────────── Search 测试 ────────────────────────────

func TestSearch_正常搜索(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "用户偏好深色模式", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	results, err := idx.Search(ctx, "user1", "scope1", "深色模式", nil, 10)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) == 0 {
		t.Error("搜索应返回结果")
	}
}

func TestSearch_指定memTypes(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "偏好深色", Type: "profile", Timestamp: time.Now().UTC()},
		{ID: padID("mem002"), Text: "记住事实", Type: "fact", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	results, err := idx.Search(ctx, "user1", "scope1", "深色", []string{"profile"}, 10)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	// 应只搜索 profile 类型的集合
	_ = results
}

func TestSearch_EmbeddingModel为nil时返回空(t *testing.T) {
	idx := newTestIndexWithNilEmbedding()
	ctx := context.Background()

	results, err := idx.Search(ctx, "user1", "scope1", "查询", nil, 10)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("embeddingModel 为 nil 时应返回空结果, 实际 %d 条", len(results))
	}
}

func TestSearch_StorageCodec解码(t *testing.T) {
	idx := newTestIndex()
	idx.SetStorageCodec(newFakeCodec())
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "敏感数据", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	results, err := idx.Search(ctx, "user1", "scope1", "敏感", nil, 10)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	// 搜索结果应经过 codec 解码
	for _, r := range results {
		if r.Doc.Text != "敏感数据" {
			t.Errorf("解码后文本应为 '敏感数据', 实际 %q", r.Doc.Text)
		}
	}
}

// ──────────────────────────── UpdateMemories 测试 ────────────────────────────

func TestUpdateMemories(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "旧内容", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	updated := []*MemoryDoc{
		{ID: padID("mem001"), Text: "新内容", Type: "profile", Timestamp: time.Now().UTC()},
	}
	err := idx.UpdateMemories(ctx, "user1", "scope1", updated)
	if err != nil {
		t.Fatalf("UpdateMemories 失败: %v", err)
	}

	// 验证内容已更新
	doc, _ := idx.GetByID(ctx, "user1", "scope1", padID("mem001"))
	if doc == nil {
		t.Fatal("更新后应能获取到文档")
	}
	if doc.Text != "新内容" {
		t.Errorf("更新后文本应为 '新内容', 实际 %q", doc.Text)
	}
}

func TestUpdateMemories_空列表(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	err := idx.UpdateMemories(ctx, "user1", "scope1", nil)
	if err != nil {
		t.Errorf("空列表应返回 nil, 实际 %v", err)
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/index/... -run "TestAddMemories|TestSearch|TestUpdateMemories" -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/simple_test.go
git commit -m "test(store): 添加 AddMemories/Search/UpdateMemories 测试"
```

---

### Task 9: 添加核心方法测试 — Delete 系列, GetByID, ListMemories, ListUserScopes

**Files:**
- Modify: `internal/agentcore/store/index/simple_test.go`

- [ ] **Step 1: 在 simple_test.go 末尾追加 Delete/GetByID/List 测试**

```go

// ──────────────────────────── DeleteMemories 测试 ────────────────────────────

func TestDeleteMemories_正常删除(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "测试1", Type: "profile", Timestamp: time.Now().UTC()},
		{ID: padID("mem002"), Text: "测试2", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	err := idx.DeleteMemories(ctx, "user1", "scope1", []string{padID("mem001")})
	if err != nil {
		t.Fatalf("DeleteMemories 失败: %v", err)
	}

	// 验证 KV 已删除
	doc, _ := idx.GetByID(ctx, "user1", "scope1", padID("mem001"))
	if doc != nil {
		t.Error("删除后 GetByID 应返回 nil")
	}

	// 验证 ID 追踪已更新
	idsKey := kvIDsKey("user1", "scope1")
	raw, _ := idx.kvStore.Get(ctx, idsKey)
	val := readKVValue(raw)
	ids := parseAllIDs(val)
	if len(ids) != 1 {
		t.Errorf("ID 追踪应剩 1 个, 实际 %d", len(ids))
	}
}

func TestDeleteMemories_ID追踪清空时删除键(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "唯一文档", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	err := idx.DeleteMemories(ctx, "user1", "scope1", []string{padID("mem001")})
	if err != nil {
		t.Fatalf("DeleteMemories 失败: %v", err)
	}

	// 全局 ID 追踪键应被删除
	idsKey := kvIDsKey("user1", "scope1")
	raw, _ := idx.kvStore.Get(ctx, idsKey)
	if raw != nil {
		t.Error("ID 追踪清空后键应被删除")
	}
}

func TestDeleteMemories_空列表(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	err := idx.DeleteMemories(ctx, "user1", "scope1", nil)
	if err != nil {
		t.Errorf("空列表应返回 nil, 实际 %v", err)
	}
}

// ──────────────────────────── DeleteByUser 测试 ────────────────────────────

func TestDeleteByUser(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "用户1数据", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	err := idx.DeleteByUser(ctx, "user1")
	if err != nil {
		t.Fatalf("DeleteByUser 失败: %v", err)
	}

	// 验证 KV 已清空
	prefix := kvPrefix + kvSep + "user1" + kvSep
	kvStore := idx.kvStore.(*fakeKVStore)
	kvStore.mu.RLock()
	for k := range kvStore.data {
		if strings.HasPrefix(k, prefix) {
			t.Errorf("KV 中不应存在 user1 前缀的键: %q", k)
		}
	}
	kvStore.mu.RUnlock()
}

// ──────────────────────────── DeleteByScope 测试 ────────────────────────────

func TestDeleteByScope(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "scope1数据", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	err := idx.DeleteByScope(ctx, "scope1")
	if err != nil {
		t.Fatalf("DeleteByScope 失败: %v", err)
	}

	// 验证 scope1 的数据已删除
	doc, _ := idx.GetByID(ctx, "user1", "scope1", padID("mem001"))
	if doc != nil {
		t.Error("DeleteByScope 后应获取不到文档")
	}
}

// ──────────────────────────── DeleteByUserAndScope 测试 ────────────────────────────

func TestDeleteByUserAndScope(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "user1+scope1", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	err := idx.DeleteByUserAndScope(ctx, "user1", "scope1")
	if err != nil {
		t.Fatalf("DeleteByUserAndScope 失败: %v", err)
	}

	doc, _ := idx.GetByID(ctx, "user1", "scope1", padID("mem001"))
	if doc != nil {
		t.Error("DeleteByUserAndScope 后应获取不到文档")
	}
}

// ──────────────────────────── GetByID 测试 ────────────────────────────

func TestGetByID_存在(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "获取测试", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	doc, err := idx.GetByID(ctx, "user1", "scope1", padID("mem001"))
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if doc == nil {
		t.Fatal("文档应存在")
	}
	if doc.Text != "获取测试" {
		t.Errorf("Text: 期望 '获取测试', 实际 %q", doc.Text)
	}
}

func TestGetByID_不存在(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	doc, err := idx.GetByID(ctx, "user1", "scope1", padID("nonexistent"))
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if doc != nil {
		t.Error("不存在的文档应返回 nil")
	}
}

func TestGetByID_StorageCodec解码(t *testing.T) {
	idx := newTestIndex()
	idx.SetStorageCodec(newFakeCodec())
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "加密数据", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	doc, _ := idx.GetByID(ctx, "user1", "scope1", padID("mem001"))
	if doc == nil {
		t.Fatal("文档应存在")
	}
	if doc.Text != "加密数据" {
		t.Errorf("解码后文本应为 '加密数据', 实际 %q", doc.Text)
	}
}

// ──────────────────────────── ListMemories 测试 ────────────────────────────

func TestListMemories_正常列表(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "偏好深色", Type: "profile", Timestamp: time.Now().UTC()},
		{ID: padID("mem002"), Text: "记住事实", Type: "fact", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	result, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, nil)
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("应返回 2 条记忆, 实际 %d", len(result))
	}
}

func TestListMemories_无数据(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	result, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, nil)
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if result != nil {
		t.Errorf("无数据时应返回 nil, 实际 %v", result)
	}
}

func TestListMemories_类型过滤(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "偏好深色", Type: "profile", Timestamp: time.Now().UTC()},
		{ID: padID("mem002"), Text: "记住事实", Type: "fact", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	result, err := idx.ListMemories(ctx, "user1", "scope1", 0, 100, []string{"profile"})
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("过滤后应返回 1 条, 实际 %d", len(result))
	}
	if result[0].Type != "profile" {
		t.Errorf("类型应为 profile, 实际 %q", result[0].Type)
	}
}

// ──────────────────────────── ListUserScopes 测试 ────────────────────────────

func TestListUserScopes(t *testing.T) {
	idx := newTestIndex()
	ctx := context.Background()

	docs := []*MemoryDoc{
		{ID: padID("mem001"), Text: "用户1范围1", Type: "profile", Timestamp: time.Now().UTC()},
	}
	_ = idx.AddMemories(ctx, "user1", "scope1", docs)

	scopes, err := idx.ListUserScopes(ctx)
	if err != nil {
		t.Fatalf("ListUserScopes 失败: %v", err)
	}
	if len(scopes) == 0 {
		t.Error("应返回至少一个 scope")
	}
	found := false
	for _, sc := range scopes {
		if sc.UserID == "user1" && sc.ScopeID == "scope1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("应包含 (user1, scope1) 对")
	}
}
```

- [ ] **Step 2: 运行全部测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/index/... -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/simple_test.go
git commit -m "test(store): 添加 SimpleMemoryIndex 全部核心方法测试"
```

---

### Task 10: 运行覆盖率检查

**Files:** 无新增

- [ ] **Step 1: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/index/... ./internal/agentcore/store/embedding/...`
Expected: 两个包覆盖率均 ≥ 85%

- [ ] **Step 2: 如果覆盖率不足，查看详情并补充测试**

Run: `cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/store/index/... && go tool cover -func=coverage.out`

根据覆盖率报告补充缺失的测试用例。

- [ ] **Step 3: 确认覆盖率达标后提交**

```bash
git add -A
git commit -m "test(store): 补充 SimpleMemoryIndex 测试至覆盖率 ≥ 85%"
```

---

### Task 11: 更新 IMPLEMENTATION_PLAN.md 回填标记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 4.18 行**

将：
```
| 4.18 | ☐ | SimpleMemoryIndex | 简单记忆索引实现 | `openjiuwen/core/foundation/store/index/simple_memory_index.py` |
```

改为：
```
| 4.18 | ✅ | SimpleMemoryIndex | 简单记忆索引实现 ⤵️ 预留：依赖 BaseEmbedding 接口（4.19 定义），4.19-4.22 实现后回填 | `openjiuwen/core/foundation/store/index/simple_memory_index.py` |
```

- [ ] **Step 2: 更新 4.19 行**

将：
```
| 4.19 | ☐ | Embedding 接口 | `EmbedQuery/EmbedDocuments/Dimension` | `openjiuwen/core/foundation/store/base_embedding.py` |
```

改为：
```
| 4.19 | ☐ | Embedding 接口 | `EmbedQuery/EmbedDocuments/Dimension` ⤴️ 需回填：4.18 已前置定义 BaseEmbedding 接口，4.19 实现时需在此接口基础上扩展 | `openjiuwen/core/foundation/store/base_embedding.py` |
```

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 4.18/4.19 回填标记"
```

---

### Task 12: 最终验证 — 全量编译和测试

**Files:** 无新增

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 确认覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/index/... ./internal/agentcore/store/embedding/...`
Expected: 两个包覆盖率均 ≥ 85%
