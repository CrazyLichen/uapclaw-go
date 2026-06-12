# SimpleMemoryIndex 设计文档

## 概述

实现步骤 4.18 SimpleMemoryIndex — 基于 KV + Vector 双存储的记忆索引，支持语义搜索和 StorageCodec 加解密。

同时前置定义 BaseEmbedding 接口（步骤 4.19 的接口部分），供 SimpleMemoryIndex 依赖。具体 Embedding 实现（OpenAI/DashScope/VLLM/API）在 4.19-4.22 中回填。

对应 Python 源码：`openjiuwen/core/foundation/store/index/simple_memory_index.py`

## 回填标记

| 位置 | 标记 | 说明 |
|------|------|------|
| `embedding/doc.go` 文件目录 | `⤵️ 预留：4.19-4.22 实现后回填具体实现文件条目` | BaseEmbedding 接口先定义，实现后补 |
| `embedding/base.go` 接口注释 | `⤵️ 预留：4.19-4.22 补充具体实现（OpenAI/DashScope/VLLM/API）` | 接口前置定义，实现延后 |
| `IMPLEMENTATION_PLAN.md` 4.18 行 | `⤵️ 预留：依赖 BaseEmbedding 接口（4.19 定义），4.19-4.22 实现后回填` | 标注依赖关系 |
| `IMPLEMENTATION_PLAN.md` 4.19 行 | `⤴️ 需回填：4.18 已前置定义 BaseEmbedding 接口（EmbedQuery/EmbedDocuments/Dimension），4.19 实现时需在此接口基础上扩展` | 标注回填来源 |

## 文件结构

```
internal/agentcore/store/
├── embedding/
│   ├── doc.go           # 包文档
│   ├── base.go          # BaseEmbedding 接口定义
│   └── base_test.go     # 接口编译约束测试
└── index/
    ├── doc.go           # 包文档（更新文件目录）
    ├── base.go          # 已有：BaseMemoryIndex + MemoryIndexBase
    ├── base_test.go     # 已有：基类测试
    ├── simple.go        # SimpleMemoryIndex 实现
    └── simple_test.go   # SimpleMemoryIndex 单元测试
```

## 第一节：BaseEmbedding 接口

包路径：`internal/agentcore/store/embedding/`

仅定义 SimpleMemoryIndex 实际需要的 3 个方法，与 Python `BaseEmbedding` 接口一一对应。具体实现类在 4.19-4.22 中补充。

```go
// BaseEmbedding 向量嵌入模型的抽象接口。
//
// 提供文本到向量的转换能力，供记忆索引等组件进行语义搜索。
// 具体实现（OpenAI、DashScope、VLLM 等）在后续步骤 4.19-4.22 中补充。
//
// 对应 Python: openjiuwen/core/foundation/store/base_embedding.py (BaseEmbedding)
type BaseEmbedding interface {
    // EmbedQuery 将单条查询文本转换为向量。
    EmbedQuery(ctx context.Context, text string) ([]float64, error)

    // EmbedDocuments 将多条文档文本批量转换为向量。
    EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)

    // Dimension 返回嵌入向量的维度。
    Dimension() int
}
```

测试：仅 `TestBaseEmbedding_接口约束`，验证 fakeEmbedding 满足接口。

## 第二节：SimpleMemoryIndex 结构体与构造

```go
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
    embeddingModel embedding.BaseEmbedding  // ⤵️ 预留：4.19-4.22 实现后可注入具体实现
    // createdCollections 已创建的向量集合缓存
    createdCollections map[string]bool
    // codec 存储编解码器（可选，用于加解密记忆文本）
    codec StorageCodec
    // mu 保护 createdCollections 的并发访问
    mu sync.RWMutex
}
```

构造函数：

```go
// NewSimpleMemoryIndex 创建简单记忆索引实例。
func NewSimpleMemoryIndex(
    kvStore kv.BaseKVStore,
    vectorStore vector.BaseVectorStore,
    embeddingModel embedding.BaseEmbedding,
) *SimpleMemoryIndex
```

与 Python 的差异：
- Python 的 `embedding_model` 可为 `None`（运行时报错），Go 中参数类型为接口，传入 nil 时 AddMemories/Search 会返回明确错误
- 增加 `sync.RWMutex` 保护 `createdCollections`，Python 无此需求（GIL 保证线程安全）
- Python 有 `set_embedding_model` 方法，Go 通过 `SetEmbeddingModel` 方法支持运行时替换

```go
// SetEmbeddingModel 设置或替换嵌入模型。
func (s *SimpleMemoryIndex) SetEmbeddingModel(model embedding.BaseEmbedding)
```

## 第三节：KV 辅助方法与 ID 追踪

忠实移植 Python 的 KV 键命名和 24 字节固定宽度 ID 追踪机制。

### 常量

```go
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
```

### KV 键构建

```go
// kvMemKey 构建记忆文档的 KV 键：UMD/{userID}/{scopeID}/{memID}
func kvMemKey(userID, scopeID, memID string) string

// kvIDsKey 构建 ID 追踪的 KV 键：
//   - memType 为空：UMD/{userID}/{scopeID}/ids（全局追踪）
//   - memType 非空：UMD/{userID}/{scopeID}/{memType}/ids（按类型追踪）
func kvIDsKey(userID, scopeID, memType string) string
```

### ID 拼接/解析

```go
// parseAllIDs 解析固定宽度拼接的 ID 字符串为 ID 列表。
// 每 byteNumPerID(24) 个字符为一个 ID，对齐 Python _parse_all_ids。
func parseAllIDs(raw string) []string

// appendID 向 ID 字符串追加一个 ID，对齐 Python _append_id。
func appendID(raw string, memID string) string

// removeID 从 ID 字符串中移除指定 ID，对齐 Python _remove_id。
func removeID(raw string, memID string) string
```

### ID 追踪

```go
// addIDToTracking 将 ID 添加到全局和类型追踪键中。
// 对齐 Python _add_id_to_tracking。
func (s *SimpleMemoryIndex) addIDToTracking(ctx context.Context, userID, scopeID, memID, memType string) error

// removeIDFromTracking 从全局和类型追踪键中移除 ID。
// 移除后键为空时删除该键。
// 对齐 Python _remove_id_from_tracking。
func (s *SimpleMemoryIndex) removeIDFromTracking(ctx context.Context, userID, scopeID, memID string, memType string) error
```

### KV 值读写

```go
// readKVValue 将 KV 存储的 []byte 值解码为字符串，nil 返回空字符串。
func readKVValue(raw []byte) string

// writeKVValue 将字符串编码为 []byte 写入 KV 存储。
func writeKVValue(text string) []byte
```

## 第四节：数据转换与 Vector 辅助方法

### 数据转换

```go
// kvDataToMemoryDoc 将 KV 存储的 JSON 数据转换为 MemoryDoc。
// 对齐 Python _kv_data_to_memory_doc，支持多种时间戳格式解析：
//   - time.Time 类型直接使用
//   - 字符串格式："2006-01-02 15-04-05"（旧格式）和 "2006-01-02 15:04:05"（标准格式）
//   - Unix 时间戳（int64/float64）
//   - ISO 8601 格式
//   - 以上均失败时使用当前 UTC 时间
func kvDataToMemoryDoc(data map[string]any, memID string) *MemoryDoc

// memoryDocToKVData 将 MemoryDoc 转换为 KV 存储的 JSON 数据。
// 对齐 Python _memory_doc_to_kv_data：
//   - KV 字段名对齐 Python：id, user_id, scope_id, mem, mem_type, timestamp
//   - timestamp 格式使用旧兼容格式 "2006-01-02 15-04-05"
//   - doc.Fields 合并到输出字典中
func memoryDocToKVData(doc *MemoryDoc, userID, scopeID string) map[string]any
```

### Vector 辅助

```go
// getCollectionName 构建向量集合名称：uid_{userID}_gid_{scopeID}_mtype_{memType}
// 对齐 Python _get_collection_name。
func getCollectionName(userID, scopeID, memType string) string

// parseMemTypeFromCollection 从集合名称中提取 memType。
// 对齐 Python _parse_mem_type_from_collection。
func parseMemTypeFromCollection(name string) string

// ensureCollection 懒创建向量集合，已创建则跳过。
// 对齐 Python _ensure_collection，集合 Schema 包含：
//   - id: VARCHAR(256), 主键
//   - embedding: FLOAT_VECTOR(dim)
func (s *SimpleMemoryIndex) ensureCollection(ctx context.Context, name string, dim int) error

// collectionsFor 列出匹配 userID+scopeID 前缀的所有向量集合。
// 对齐 Python _collections_for。
func (s *SimpleMemoryIndex) collectionsFor(ctx context.Context, userID, scopeID string) ([]string, error)
```

ensureCollection 使用 `sync.RWMutex` 保护 `createdCollections` 缓存，避免并发创建同一集合。

## 第五节：核心方法实现

SimpleMemoryIndex 嵌入 `*MemoryIndexBase` 继承 7 个默认方法，需实现 9 个核心抽象方法 + 覆盖 2 个基类空实现方法。

### 9 个核心抽象方法

**1. SetStorageCodec** — 简单赋值 codec 字段。

**2. AddMemories** — 流程：
1. 空 memories 直接返回
2. 按 memType 分组
3. 每组：提取文本 → `embeddingModel.EmbedDocuments` → `ensureCollection` → `vectorStore.AddDocs` → 逐条写 KV（含 StorageCodec 编码） → `addIDToTracking`
4. embeddingModel 为 nil 时返回 `StatusMemoryAddMemoryExecutionError` 错误

**3. Search** — 流程：
1. embeddingModel 为 nil 时记录错误日志并返回空结果
2. `embeddingModel.EmbedQuery` 获取查询向量
3. 确定搜索的 memTypes：传入非空则用传入值，否则从 `collectionsFor` 推断
4. 逐类型搜索：`vectorStore.Search` → 提取 hit IDs + scores → `kvStore.MGet` → 解码（含 StorageCodec 解码）→ 构建 `MemorySearchResult`
5. 按 Score 降序排序，截取 topK

**4. UpdateMemories** — 先 `DeleteMemories` 再 `AddMemories`（delete-then-add 策略）。

**5. DeleteMemories** — 流程：
1. 逐 ID：从 KV 读取获取 memType → 删除 KV 键 → `removeIDFromTracking`
2. 遍历该用户+scope 下所有集合，`vectorStore.DeleteDocsByIDs`

**6. DeleteByUser** — KV：`kvStore.DeleteByPrefix("UMD/{userID}/")`；Vector：遍历所有集合删除 `uid_{userID}_gid_` 前缀的集合，清理 `createdCollections`

**7. DeleteByScope** — KV：`kvStore.GetByPrefix("UMD/")` → 过滤 `parts[2] == scopeID` → `kvStore.BatchDelete`；Vector：删除含 `_gid_{scopeID}_mtype_` 的集合

**8. DeleteByUserAndScope** — KV：`kvStore.DeleteByPrefix("UMD/{userID}/{scopeID}/")`；Vector：`collectionsFor` → 逐个 `deleteCollection`

**9. GetByID** — `kvStore.Get(kvMemKey)` → JSON 解析 → StorageCodec 解码 → `kvDataToMemoryDoc`；不存在返回 nil, nil

### 2 个覆盖基类的方法

**ListMemories** — 读取全局 ID 追踪键 → `parseAllIDs` → `kvStore.MGet` 批量获取 → 解码 → 过滤 memTypes → 按 memTypes 顺序 + 时间戳排序 → 分页返回

**ListUserScopes** — `kvStore.GetByPrefix("UMD/")` → 解析键中 (userID, scopeID) → 去重返回

## 第六节：日志与错误处理

### 日志规则

组件常量使用 `logger.ComponentCommon`（store 属于基础设施层，与 base.go 一致）。

Python 日志同步点：

| Python 调用点 | Go 对应 | 级别 | 事件类型 |
|---|---|---|---|
| `add_memories` 中 embedding_model 为 None | `AddMemories` 中 embeddingModel 为 nil | Error | `MEMORY_STORE` |
| `search` 中 embedding_model 为 None | `Search` 中 embeddingModel 为 nil | Error | `MEMORY_RETRIEVE` |

日志字段对齐 Python：`scope_id`（scopeID）、`collection`（集合名称，add_memories 场景）。

### 异常路径日志（防御性，按规则 3.8）

| Go 方法 | 异常分支 | 日志内容 |
|---|---|---|
| `AddMemories` | `EmbedDocuments` 返回 error | Error 日志，含 event_type=LLM_CALL_ERROR, method, 错误详情 |
| `AddMemories` | `vectorStore.AddDocs` 返回 error | Error 日志 |
| `AddMemories` | `kvStore.Set` 返回 error | Error 日志 |
| `Search` | `EmbedQuery` 返回 error | Error 日志，含 event_type=LLM_CALL_ERROR |
| `Search` | `vectorStore.Search` 返回 error | Error 日志 |
| `DeleteMemories` | `kvStore.Get` 返回 error | Error 日志 |
| `ensureCollection` | `CreateCollection` 返回 error | Error 日志 |

### 错误码映射

| Python StatusCode | Go StatusCode |
|---|---|
| `MEMORY_ADD_MEMORY_EXECUTION_ERROR` | `StatusMemoryAddMemoryExecutionError` |

其他异常路径直接返回底层 error（KV/Vector store 的错误透传），不额外包装。

## 第七节：测试策略

### Mock 方式

| 依赖 | Mock 方式 |
|---|---|
| `BaseKVStore` | `fakeKVStore`，内部用 `map[string][]byte` + `sync.RWMutex` |
| `BaseVectorStore` | `fakeVectorStore`，内部用集合名→文档ID→字段映射 |
| `BaseEmbedding` | `fakeEmbedding`，返回固定维度向量 |
| `StorageCodec` | 复用 `base_test.go` 中已有的 `fakeCodec` |

### 测试用例清单

**BaseEmbedding**（`embedding/base_test.go`）：
- `TestBaseEmbedding_接口约束` — fakeEmbedding 满足接口

**SimpleMemoryIndex**（`index/simple_test.go`）：

| 测试函数 | 覆盖场景 |
|---|---|
| `TestNewSimpleMemoryIndex` | 构造函数 |
| `TestSetEmbeddingModel` | 运行时替换嵌入模型 |
| `TestSetStorageCodec` | 设置编解码器 |
| `TestAddMemories_正常添加` | 单类型多文档写入 KV+Vector+ID 追踪 |
| `TestAddMemories_多类型分组` | 不同 memType 分组写入不同集合 |
| `TestAddMemories_EmbeddingModel为nil时返回错误` | 无嵌入模型时报错 |
| `TestSearch_正常搜索` | 向量搜索 → KV 获取 → 解码 → 排序截取 |
| `TestSearch_指定memTypes` | 只搜索指定类型 |
| `TestSearch_EmbeddingModel为nil时返回空` | 无嵌入模型返回空结果 |
| `TestSearch_StorageCodec解码` | 搜索结果经编解码器解密 |
| `TestUpdateMemories` | 先删后加策略 |
| `TestDeleteMemories_正常删除` | KV+Vector+ID 追踪均清理 |
| `TestDeleteMemories_ID追踪清空时删除键` | 追踪键为空时删除 KV 键 |
| `TestDeleteByUser` | 按用户删除 KV 前缀 + Vector 集合 |
| `TestDeleteByScope` | 按 scope 扫描删除 |
| `TestDeleteByUserAndScope` | 按 用户+scope 组合删除 |
| `TestGetByID_存在` | 正常获取 |
| `TestGetByID_不存在` | 返回 nil, nil |
| `TestGetByID_StorageCodec解码` | 编解码器解密 |
| `TestListMemories_正常列表` | 分页 + 类型过滤 + 排序 |
| `TestListMemories_无数据` | 返回空切片 |
| `TestListUserScopes` | 扫描 KV 键提取 用户/范围 对 |
| `TestKVHelper_KVMemKey` | 键格式验证 |
| `TestKVHelper_KVIDsKey` | 全局/类型追踪键格式验证 |
| `TestKVHelper_ParseAllIDs` | 固定宽度 ID 解析 |
| `TestKVHelper_AppendID` | ID 追加 |
| `TestKVHelper_RemoveID` | ID 移除 |
| `TestKVHelper_KVDataToMemoryDoc_多种时间戳格式` | 时间戳解析兼容 |
| `TestKVHelper_MemoryDocToKVData` | 文档转 KV 数据 |
| `TestGetCollectionName` | 集合命名格式 |
| `TestParseMemTypeFromCollection` | 从集合名提取 memType |
| `TestEnsureCollection_懒创建` | 首次创建 + 重复跳过 |

覆盖率目标：≥ 85%，通过 mock 覆盖所有正常和异常路径，无需 `//go:build` 标签。
