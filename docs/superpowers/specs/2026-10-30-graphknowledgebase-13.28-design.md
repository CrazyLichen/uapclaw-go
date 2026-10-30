# 13.28 GraphKnowledgeBase 设计文档

> 对应实现计划：领域十三 13.28（IMPLEMENTATION_PLAN.md L836）
> Python 源码：`openjiuwen/core/retrieval/graph_knowledge_base.py`（371 行）
> Python 单测：`tests/unit_tests/core/retrieval/test_graph_knowledge_base.py`（12 例）
> 前置依赖：13.26 KnowledgeBase 基座 ✅ / 13.27 SimpleKnowledgeBase ✅ / 13.17 TripleExtractor ✅ / 13.23 GraphRetriever ✅ / 13.25 AgenticRetriever ✅ / 13.1-13.4 Indexer ✅ / 13.5-13.9 Chunker ✅
> 后续消费方：8.25 KnowledgeRetrievalComp（`use_graph ? GraphKnowledgeBase : SimpleKnowledgeBase`）

## 1. 流程位置与作用

```
用户会话
  └─ Workflow 组件 8.25 KnowledgeRetrievalComp（领域八，未实现）
       ├─ _create_knowledge_bases(): use_graph ? GraphKB(13.28) : SimpleKB(13.27)
       │     逐 component_kb_config → create_vector_store + OpenAIEmbedding
       └─ invoke → retrieve_multi_kb_with_source(kbs, query, ...)      ← 13.27 multi_kb.go
                     └─ kb.retrieve(query, config)                     多态分派
                          ├─ 图路径（本节）：GraphRetriever(13.23) chunk+triple 双集合
                          │     ├─ GetRetrieverForMode → Vector/Sparse/Hybrid(13.20/13.21)
                          │     ├─ beam search + fetch（13.23）
                          │     └─ agentic 时外包 AgenticRetriever(13.25)
                          └─ fallback：内建 SimpleKB → 向量/稀疏/混合 + agentic
写侧（同一 KB 的入库侧）：
  Parser(13.10-13.16) → Chunker(13.5-13.9) → [Chunk 索引]
                                            → TripleExtractor(13.17) → [Triple 索引]
                                                  ↑ 只有本节会调用
```

本节作用三条：

1. **检索查询管线在会话侧的双子类之一**。对外同一 `KnowledgeBase` 接口，`use_graph` 一个开关把上层组件从"向量检索"透明切到 GraphRAG 图检索；上层无需感知 GraphRetriever 的 chunk/triple 双集合编排。
2. **13.17 TripleExtractor 的唯一生产级消费方**。`add_documents` 在 `use_graph && extractor != nil` 时抽三元组、转 `TextChunk`、写 `kb_{id}_triples` 索引，把抽取器从"零消费"变为闭环的一半。
3. **13.23 GraphRetriever 的唯一生产级构造方**。注入 chunk/triple retriever、双集合名、`index_type`，并把 `RetrievalConfig.graph_expansion` 首次真正传到 retriever 层。

本节完成后领域十三"查"侧管线闭环（验证点 L838 可打勾）。

## 2. 文件结构

```
internal/agentcore/retrieval/knowledgebase/
├── doc.go            # 改：文件目录加 graph_kb.go / kb_helpers.go，删 L21 "13.28 落地时补充"
├── base.go           # 不动（13.26）
├── validate.go       # 不动（13.26）
├── kb_helpers.go     # 新增：包级 wrapAgenticRetriever / vectorStoreDatabaseName /
│                     #       resolveRetrievalMode / defaultRetrievalConfig（13.27 逻辑下沉）
├── simple_kb.go      # 改：两处方法体改为薄委托包级函数（行为零变化）
├── multi_kb.go       # 不动（13.27）
├── graph_kb.go       # 新增：GraphKnowledgeBase 5 抽象方法 + SetGraphRetriever + 两聚合薄委托
└── graph_kb_test.go  # 新增
```

同包新建 `kb_helpers.go` 而非放进 `graph_kb.go`：两函数的消费方是 SimpleKB 与 GraphKB 两个子类，放任一子类文件会造成"GraphKB 依赖 SimpleKB 文件"的错觉（拍板：提包级函数）。

## 3. 类型设计

```go
type GraphKnowledgeBase struct {
    BaseKnowledgeBase
    chunkRetriever  retriever.Retriever
    tripleRetriever retriever.Retriever
    graphRetriever  retriever.Retriever // nil = 待懒创建
    graphMu         sync.Mutex          // 懒创建保护；读路径同样持锁（13.27 §13-1 先例）
}

var _ KnowledgeBase = (*GraphKnowledgeBase)(nil)

func NewGraphKnowledgeBase(config common.KnowledgeBaseConfig,
    chunkRetriever, tripleRetriever retriever.Retriever,
    opts ...KnowledgeBaseOption) (*GraphKnowledgeBase, error)

func (g *GraphKnowledgeBase) SetGraphRetriever(r retriever.Retriever)
```

- 子类特有字段（chunk/triple retriever）走**显式参数**，与 13.27 `NewSimpleKnowledgeBase(config, retriever, opts...)` 同风格（Python 基类不收、子类特有）。
- 基座选项透传 `NewBaseKnowledgeBase`，构造期配置一致性校验（155508）天然继承。
- 值嵌入安全：`BaseKnowledgeBase` 纯数据无锁；同包访问非导出字段。
- `SetGraphRetriever` 对齐 Python `kb.graph_retriever = r` 直写（Python 单测 `test_close` 靠它注入）。

## 4. AddDocuments（写侧双索引）

```
1. chunker 缺      → 155501 "chunker is required for add_documents"
2. indexManager 缺 → 155502 "index_manager is required for add_documents"
3. strictValidation && vectorStore → CheckVectorField()
4. 逐 doc：strings.TrimSpace(ID) == "" → uuid.NewString()
5. ChunkDocuments(ctx, documents) → err 冒泡
   Info: document_count / chunk_count "Chunked documents into chunks"
6. chunkIndexConfig := IndexConfig{
       IndexName:           "kb_" + cfg.KBID + "_chunks",
       IndexType:           cfg.IndexType,
       UseCaptionForImages: cfg.UseCaptionForImages,   // ← 随配置
   }
7. BuildIndex(ctx, chunks, chunkIndexConfig, embedModel,
       indexer.WithIndexExtra("database_name", vectorStoreDatabaseName(vs)))
   非 ok → 155505 "Failed to build chunk index"
8. cfg.UseGraph && extractor != nil:
     Info "Extracting triples for graph index..."
     triples, err := extractor.Extract(ctx, chunks)   // err 冒泡（155507 由 13.17 抛出）
     若 len(triples) > 0:
       Info: triple_count "Extracted triples"
       tripleIndexConfig := IndexConfig{
           IndexName:           "kb_" + cfg.KBID + "_triples",
           IndexType:           cfg.IndexType,
           UseCaptionForImages: true,                 // ← 硬编码，与 chunk 索引不对称
       }
       tripleChunks := ConvertTriples(triples)        // §4.1
       BuildIndex(...) 非 ok → 155506 "Failed to build triple index"
       Info: triple_chunk_count "Built triple index with triples"
9. docIDs 收集 → Info document_count "Successfully added documents to knowledge base"
```

### 4.1 三元组 → TextChunk（读写两侧的元数据契约）

| 字段 | Python 字面 | Go |
|---|---|---|
| `id_` | `str(uuid.uuid4())` | `uuid.NewString()` |
| `text` | `f"{s} {p} {o}"` | `t.Subject + " " + t.Predicate + " " + t.Object` |
| `doc_id` | `triple.metadata.get("doc_id", "")` | 断言 `meta["doc_id"].(string)`，非 string 或缺失 → `""`（偏差 D3） |
| `metadata` | `{**triple.metadata, "triple": json.dumps([s,p,o]), "chunk_index": i, "chunk_id": metadata.get("chunk_id","")}` | 浅拷贝 meta（nil → 新建），再写 3 个键；`triple` = `string(json.Marshal([]string{s,p,o}))`（偏差 D2） |

写入顺序等价于 Python 字典展开后覆盖：先拷贝 `triple.metadata` 全部键，再写 `triple` / `chunk_index` / `chunk_id`（同名键被覆盖）。`chunk_index` 为 `int`（Go 存 `int`，与 Python int 一致）。

**为何这三个键重要**：13.23 `graph_fetch` 读侧按 `metadata["doc_id"]` / `metadata["chunk_id"]` 回链原始 chunk（`graph_fetch.go` L180/L182）。写侧布局与读侧读法在此对齐，缺一个键图扩展就断链。

### 4.2 两处 IndexConfig 的不对称（Python 真实行为，照复刻）

| 索引 | `index_type` | `use_caption_for_images` |
|---|---|---|
| `kb_{id}_chunks` | `cfg.IndexType` | `cfg.UseCaptionForImages` |
| `kb_{id}_triples` | `cfg.IndexType` | **`true` 硬编码** |

不在 Go 侧"修正"为随配置——图三元组是纯文本，恒走 text-only 嵌入路径，硬编码是刻意行为。

## 5. Retrieve

```
1. config == nil → defaultRetrievalConfig()（TopK=5，13.27 私有回落）
2. useGraph := (rc.UseGraph != nil && *rc.UseGraph) || (rc.UseGraph == nil && cfg.UseGraph)
   - useGraph == false → §5.3 fallback
3. ensureGraphRetriever()（§5.2）
4. mode := resolveRetrievalMode(cfg.IndexType)   // 13.27 包级函数复用
5. rc.Agentic → wrapAgenticRetriever(g.llmClient, gret, opts...)   // 13.27 规则复用，155510
6. r.Retrieve(ctx, query, rc.TopK, rc.ScoreThreshold, mode,
       retriever.WithRetrieveFilters(rc.Filters),
       retriever.WithRetrieveGraphExpansion(rc.GraphExpansion))
```

- `GraphExpansion` 为 `bool`（Python `RetrievalConfig.graph_expansion: bool = False`），Go 用 `WithRetrieveGraphExpansion(bool)` 恒写显式值 → retriever 侧 `*bool` 拿到非 nil 指针，语义与 Python 一致（默认 False 表示不做图扩展，即使 KB 走图路径）。
- `Filters` 仍是死参数（13.27 §8-14），照写不消费。
- 图路径**不消费** `KBOptions.Extra` 的其他键：非 agentic 时 kwargs 整体丢弃（Python 字面：只有 `AgenticRetriever(... **kwargs)` 收）。

### 5.1 use_graph 三层判定

| `rc.UseGraph` | `cfg.UseGraph` | 结果 |
|---|---|---|
| `true` | 任意 | 图路径 |
| `nil` | `true` | 图路径 |
| `nil` | `false` | fallback |
| `false` | 任意 | fallback（显式关图压过配置） |

`UseGraph` 必须是指针——`false` 与 `nil` 语义不同（`false` 是"本次显式关图"）。

### 5.2 懒创建与 index_type 注入

```
ensureGraphRetriever():
  graphMu.Lock(); defer Unlock()
  g.graphRetriever != nil → 返回（读路径也持锁，偏差 D1）
  vectorStore == nil → 155503 "vector_store is required for graph retrieval"
  chunkCollection  := "kb_" + cfg.KBID + "_chunks"
  tripleCollection := "kb_" + cfg.KBID + "_triples"
  g.graphRetriever = retriever.NewGraphRetriever(
      WithGraphChunkRetriever(g.chunkRetriever),      // 固定检索器优先（nil = 动态创建）
      WithGraphTripleRetriever(g.tripleRetriever),
      WithGraphVectorStore(g.vectorStore),
      WithGraphEmbedModel(g.embedModel),
      WithGraphChunkCollection(chunkCollection),
      WithGraphTripleCollection(tripleCollection))
  IndexTypeCarrier 断言 → SetIndexType(string(cfg.IndexType))
```

- 六个 `WithGraph*` 选项与 `SetIndexType` 此前**零生产调用**，本节首次接线（伏笔回填 3）。
- `SetIndexType` 时机与 Python 一致：构造后立刻注入，早于任何 Retrieve；`AgenticRetriever` 构造时读它推导 `defaultMode`（13.25）。
- 断言失败（自定义 Retriever 未实现 carrier）→ 跳过注入，Python `getattr` 语义等价。

### 5.3 fallback 保真点

```
NewSimpleKnowledgeBase(cfg, nil,
    WithVectorStore(g.vectorStore), WithEmbedModel(g.embedModel),
    WithParser(g.parser), WithChunker(g.chunker),
    WithIndexManager(g.indexManager), WithLLMClient(g.llmClient))
→ base.Retrieve(ctx, query, config, opts...)
```

两处易漏：

1. **不传 extractor**：Python fallback 构造 `SimpleKnowledgeBase` 时不传 extractor（GraphKB 独有），故 fallback 的 `has_extractor` 为 false。Go 同样不传。
2. **不传 strict_validation**：Python 不传 → SimpleKB 恒为 `true`，即使 GraphKB 自己是 `false`。Go 不传 `WithStrictValidationFalse`。
3. 构造期 `postAssignValidation` 可能返回 155508（两侧配置不匹配），Python 由 `__setattr__` 抛同码错误，对齐。

每次 fallback 都新建一个 SimpleKB（Python 字面，无缓存），其 retriever 每次重新懒创建——性能上接受，行为优先。

## 6. 其余四方法

| 方法 | 行为 |
|---|---|
| `DeleteDocuments` | indexManager 缺 → 155502 `"index_manager is required for delete_documents"`；strict → `CheckVectorField`；先删 chunk 索引（逐 doc_id，`DeleteIndex` 返回 false → `success=false`，error 冒泡）；`cfg.UseGraph` 时再删 triple 索引，**false 只 `Warn("Failed to delete triples for doc_id=...")`、不影响总结果**，error 仍冒泡（Python 无 try/except）；返回 success |
| `UpdateDocuments` | strict → `CheckVectorField`；`DeleteDocuments(docIDs)`；`AddDocuments(documents, opts...)`；返回 add 的 docIDs |
| `GetStatistics` | indexManager nil → `{kb_id, index_exists:false}` 2 键短路；否则 chunk `GetIndexInfo`（error 冒泡）+ `cfg.UseGraph` 时 triple `GetIndexInfo`；返回 **11 键**：`kb_id` / `index_type` / `use_graph` / `chunk_index_info` / `triple_index_info` / `has_parser` / `has_chunker` / `has_extractor` / `has_embed_model` / `has_vector_store` / `has_graph_retriever`。`triple_index_info` 键**恒存在**，非图路径为 nil（Python None） |
| `Close` | `BaseKnowledgeBase.Close()`（vectorStore → indexManager）→ `maybeClose(graphRetriever)` → `maybeClose(chunkRetriever)` → `maybeClose(tripleRetriever)`，顺序字面。**含重复关闭，照复刻**，见 §6.1 |

### 6.1 Close 的重复关闭（照复刻，禁止"顺手优化"）

`GraphRetriever.Close()` 内部已经关闭它持有的固定 `chunk_retriever` / `triple_retriever`（`graph_retriever_impl.go` L413-420 / py L795-814），而 `GraphKnowledgeBase.Close()` 之后又逐字段各关一次 —— 同一对象被关两轮。调用树（显式注入固定检索器时）：

```
GraphKB.Close()
├─ BaseKnowledgeBase.Close()
│   ├─ vector_store.Close()                              [1]
│   └─ index_manager.Close()                             [1]
├─ graph_retriever.Close()
│   ├─ chunk_retriever.Close()  → vector_store.Close()   [2]
│   └─ triple_retriever.Close() → vector_store.Close()   [3]
├─ chunk_retriever.Close()      → vector_store.Close()   [4]
└─ triple_retriever.Close()     → vector_store.Close()   [5]
```

- **主链路（8.25）不受影响**：`GraphKnowledgeBase(config, vector_store, embed_model, llm_client)` 不传固定检索器，`chunkRetriever`/`tripleRetriever` 恒 nil；`GraphRetriever` 的动态检索器每次调用临时创建、不缓存（`GetRetrieverForMode` 直接返回），故 3-5 步皆空操作，`vector_store.Close()` 只调 1 次。
- **只有显式注入固定检索器才触发**（复用同一 store 的高级用法）；Python 单测未暴露，因 `test_close` 用的是独立 `AsyncMock`。
- 决策（2026-10-30 用户拍板）：**字面复刻，不加 `if graphRetriever == nil` 守卫**。理由：13.27 §13.4 取向"对齐优先于防炸"；幂等责任在底层 store。**不加固化测试**（把 Python 缺陷钉进断言会让后续修 bug 变得困难）。

### 6.2 maybeClose 在 Task 0 之后只剩单变体

Task 0（§13）把 `Retriever` / `VectorStore` / `KnowledgeBase` 三接口统一为 `Close() error`，`maybeClose` 随之收敛为单条断言：

```go
func (b *BaseKnowledgeBase) maybeClose(obj any) {
    if obj == nil { return }
    if c, ok := obj.(interface{ Close() error }); ok {
        if err := c.Close(); err != nil {
            logger.Warn(logComponent).Err(err).Msg("Failed to close object")   // 保留 Python sync 分支的 Warn 吞
        }
    }
}
```

三个 retriever 字段（`retriever.Retriever`，Task 0 后为 `Close() error`）必然命中该断言；`indexManager` 槽依赖动态类型——`indexer.Indexer` 接口无 Close，Chroma/Milvus 具体类型在 Task 0 中已改成 error 变体，故仍能命中（§13.3 拒绝项）。**若 Task 0 被推迟**，本节退回 13.26 双变体语义，行为一致（仅多保留一条不可达分支）。

## 7. 聚合薄委托

```go
func RetrieveMultiGraphKB(ctx context.Context, kbs []KnowledgeBase,
    query string, config *common.RetrievalConfig, topK *int) []string
func RetrieveMultiGraphKBWithSource(ctx context.Context, kbs []KnowledgeBase,
    query string, config *common.RetrievalConfig, topK *int) ([]common.MultiKBRetrievalResult, error)
```

纯委托 13.27 两函数（Python L353-371 同样两行）。放 `graph_kb.go` 末尾，不新建文件——Python 是同一模块内的辅助函数。

## 8. 偏差归档

| # | 项 | Python | Go | 判定依据 |
|---|---|---|---|---|
| 8-1 | 懒创建并发 | 无锁（事件循环单线程） | `graphMu` 读路径也持锁 | 13.27 §13-1：Go 内存模型下锁外读属数据竞争，`-race` 硬要求 |
| 8-2 | `metadata["triple"]` 序列化 | `json.dumps([s,p,o])` → `", "` 分隔 + `ensure_ascii=True`（中文 `\uXXXX`） | `json.Marshal([]string{...})` 紧凑、无空格、中文原样、`<>&` → `\u003c` | **用户 2026-10-30 拍板**：语义等价，下游按 JSON 解析（13.23 graph_fetch 只读 doc_id/chunk_id），不追求逐字节 |
| 8-3 | `doc_id`/`chunk_id` 非 string | pydantic 抛 ValidationError | 断言失败收窄 `""` | 13.25 §8-7 先例（`doc_id`/`chunk_id` 非 string 收窄空串） |
| 8-4 | 延迟 import | 函数内 import GraphRetriever/SimpleKB | 包级 import | 编译期消解，13.27 同款 |
| 8-5 | `graph_retriever` 外部赋值 | 属性直写（`kb.graph_retriever = r`，单测靠它注入） | `SetGraphRetriever(r retriever.Retriever)`：持 `graphMu` 写；传 nil = 复位；类型放宽到接口以支持测试注入 | Go 无动态属性。残余风险：注入非 `*GraphRetriever` 的 fake 时 13.25 `isinstance` 判图路径为假（Python 注入 AsyncMock 同理） |
| 8-6 | `close` async 形态 + 重复关闭 | `async def close`：`await super().close()`（`_maybe_await` 协程/普通双探测）→ graph → chunk → triple；graph 已关固定检索器，KB 再关一遍 | `Close() error` 单一签名（Task 0 统一，§13）；`BaseKnowledgeBase.maybeClose` 单变体 + Warn 吞；重复关闭**字面复刻**（§6.1） | 2026-10-30 用户拍板：签名单一化（方案 B）+ 重复关闭复刻 |
| 8-7 | `graph_hops` 接线 | KB 不传 `graph_hops` | 同样不接 | **注释纠偏**：13.19 `base.go` GraphHops 注释「13.28 接线后激活」不成立，改为「无生产方死参数」 |

## 9. 测试设计

### 9.1 Python 12 例 1:1 映射

| # | Python 用例 | Go 用例 | 断言要点 |
|---|---|---|---|
| 1 | `test_parse_files_success` | `TestGraphKB_ParseFiles` | 基座模板方法继承可用，1 文档 |
| 2 | `test_add_documents_with_graph` | `TestGraphKB_AddDocuments_图索引` | `build_index` 2 次 + `extract` 1 次 |
| 3 | `test_add_documents_without_graph` | `TestGraphKB_AddDocuments_非图` | `build_index` 1 次 + extract 0 次 |
| 4 | `test_retrieve_with_graph` | `TestGraphKB_Retrieve_图路径` | 图路径分派到注入的 fake 检索器，1 结果（见 §9.4） |
| 5 | `test_retrieve_with_agentic` | `TestGraphKB_Retrieve_Agentic` | agentic 包装；底层 retriever=注入的图检索器；llm 传入 |
| 6 | `test_retrieve_without_graph_falls_back_to_simple` | `TestGraphKB_Retrieve_回落SimpleKB` | 回落路径 llmClient 传入 |
| 7 | `test_delete_documents_with_graph` | `TestGraphKB_DeleteDocuments_图` | `delete_index` 2 次，返回 true |
| 8 | `test_delete_documents_without_graph` | `TestGraphKB_DeleteDocuments_非图` | 1 次 |
| 9 | `test_update_documents` | `TestGraphKB_UpdateDocuments` | delete ≥1 且 build ≥1 |
| 10 | `test_get_statistics_with_graph` | `TestGraphKB_GetStatistics_图` | 4 键存在 |
| 11 | `test_get_statistics_without_index_manager` | `TestGraphKB_GetStatistics_无IndexManager` | 2 键短路 |
| 12 | `test_close` | `TestGraphKB_Close` | graph/chunk/triple 三 retriever 均关闭 |

### 9.2 Go 增强项

1. **三元组 chunk 字段逐项**：text 三空格拼接、doc_id 取 meta、metadata 含 `triple`/`chunk_index`/`chunk_id` 三键且 `chunk_index` 为 0/1 序号、`triple` 为可解析的 JSON 数组。
2. **triple IndexConfig 硬编码**：捕获 `BuildIndex` 的 `IndexConfig` → `UseCaptionForImages == true`，而 chunk 索引取其配置值（不对称验证）。
3. **五条错误路径**：155501 / 155502 / 155503（图路径无 vectorStore）/ 155505（chunk build false）/ 155506（triple build false）。
4. **triple 删除失败**：`DeleteIndex` 返回 false → 总结果仍 true + Warn 日志。
5. **index_type 注入生效**：`bm25` 配置 → 图检索器构造后 `IndexType()=="bm25"`；agentic 包装后 `defaultMode==sparse`（13.25 联动）。
6. **并发 Retrieve 只建一次**：N goroutine 并发调用，`graphRetriever` 构造计数 == 1（`-race` 下跑）。
7. **fallback 保真**：构造出的 SimpleKB `has_extractor==false`；GraphKB `strictValidation=false` 时 fallback 仍严格（用属性不匹配桩验证 155508 仍触发）。
8. **graph_expansion 透传**：fake retriever 捕获 `RetrieveOptions.GraphExpansion`，显式为 `false` 指针（非 nil）。
9. **空 triples 不建三索引**：extractor 返回空 → `build_index` 仅 1 次。
10. **两聚合函数委托**：`RetrieveMultiGraphKB(WithSource)` 与 13.27 同名函数同结果。
11. **`SetGraphRetriever` 短路**：注入后 Retrieve 不再构造新检索器。
12. **Task 0 回归（本节的先决条件）**：统一 Close 签名后，13.19-13.27 既有测试**断言一字不改**全绿；额外补一条 `TestBaseKB_Close_组件错误被Warn吞`（Task 0 前由 `Close() error` 变体承载，统一后由单变体承载，行为不变）。

### 9.3 覆盖率

knowledgebase 包基线 96.2%（13.27 收尾值），本节新增代码预期 90%+；整包目标不低于基线。全量回归必须 `CGO_ENABLED=1` + `-tags "sqlite_fts5 test"`。

### 9.4 可测性拍板：不加 mock 接缝

Python 用例 4/5 用 `patch("...graph_knowledge_base.GraphRetriever")` 替换类。Go 无等价机制，且不为测试往生产代码塞函数变量接缝（13.25 拍板：不为测试引入间接层）。替代方案：

- **分派验证**（用例 4/5/9.2-11）：`SetGraphRetriever(fake)` 注入后调用 `Retrieve`，断言 fake 收到 `query/topK/scoreThreshold/mode/GraphExpansion/Filters`，且返回其自身结果。这覆盖 Python 用例真正想验证的"图路径走图检索器、agentic 在其上再包一层"。
- **懒创建验证**（9.2-5/9.2-6）：不注入，直接用 fake vector_store + fake embed_model 让 `NewGraphRetriever` 真实构造（纯结构体组装、无副作用、无网络），断言 `IndexType()` 与并发只建一次。
- `test_close` 同理由 `SetGraphRetriever` 注入可关闭的 fake。

## 10. 伏笔回填对账

| # | 伏笔出处 | 本节动作 |
|---|---|---|
| 1 | 错误码 155505 / 155506 当前零消费（grep 确认，仅 155507 被 13.17 用） | 零消费 → 已消费 |
| 2 | 13.17 计划行「★TripleExtractor 的真实消费方」+ `extractor/doc.go` L8 注释 | `AddDocuments` 接线兑现，计划行标注落地 |
| 3 | 13.23 `capability.go` L44「SetIndexType 注入（上层 13.28 GraphKB 调用）」+ 六个 `WithGraph*` 无生产方 | 首次生产调用 |
| 4 | 13.23 `retriever/base.go` L74 `GraphExpansion`「13.28 GraphKB 传 config.graph_expansion」 | 首次真实生产方 |
| 5 | 13.26 `base.go` L25/L66 注释「5 抽象方法由 13.27/13.28 实现」、`doc.go` L21「13.28 graph.go 落地时补充目录条目」 | 补 `var _ KnowledgeBase` 第二实现 + doc.go 目录 |
| 6 | 13.27 `simple_kb.go` 注释「13.28 GraphKB 构造 base_kb 时传 nil retriever 自然」 | fallback 复用兑现 |
| 7 | 13.19 `retriever/base.go` L66 `GraphHops`「13.28 接线后激活」 | **纠偏**：Python GraphKB 不传 `graph_hops`，改为「无生产方死参数」（不删字段） |
| 8 | 13.23 `graph_fetch.go` 读 `metadata["doc_id"]/["chunk_id"]` | 写侧本节补齐这两键，读写对齐 |

## 11. doc.go 与计划表更新

- `knowledgebase/doc.go`：目录加 `kb_helpers.go` / `graph_kb.go`；包概述补"GraphKB 图增强实现 + 双索引写入"；删 L21 待办句。
- `retriever/capability.go` L44、`retriever/base.go` L66/L74 注释：把"13.28 将…"改为"13.28 已接线"。
- `IMPLEMENTATION_PLAN.md`：13.28 ☐→✅（含实现要点摘要）；领域十三验证点 L838「检索查询管线闭环」☐→✅；L840 跨域联动标注 13.26-13.28 已完成；7.36 行 ⏵ 缺口说明补"领域十三已覆盖完整写入+读取管线"。

## 12. 实施顺序建议（供 writing-plans 展开）

0. **Task 0（前置，独立 commit）**：Close 签名统一为 `Close() error`，见 §13
1. Task 1：`kb_helpers.go` 抽取四函数（`wrapAgenticRetriever` / `vectorStoreDatabaseName` / `resolveRetrievalMode` / `defaultRetrievalConfig`）+ `simple_kb.go` 改薄委托（行为零变化，先跑 13.27 既有测试作回归网）
2. Task 2：`graph_kb.go` 类型 + `AddDocuments`（含三元组转换）+ 测试（用例 2/3/9.2-1/9.2-2/9.2-3 前半/9.2-9）
3. Task 3：`Retrieve` 三层判定 + 懒创建 + index_type 注入 + agentic + fallback + 测试（用例 4/5/6 + 9.2-5/6/7/8）
4. Task 4：`DeleteDocuments` / `UpdateDocuments` / `GetStatistics` / `Close` + 测试（用例 7/8/9/10/11/12 + 9.2-4/11）
5. Task 5：两聚合薄委托 + 测试（9.2-10）
6. Task 6：doc.go ×1 + 注释纠偏 + 计划表状态同步
7. 全量回归：`go test -race` 单包 + `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`

## 13. Task 0（前置）：Close 签名统一为 `Close() error`

**动机**（2026-10-30 用户拍板方案 B）：13.26 为承接 Python `_maybe_await` 的"协程/普通"双探测，把 close 做成了 `Close()` + `Close() error` 双变体可选接口。这在 Go 里等于同一职责两种签名，读代码的人必须先理解鸭子探测才知道该实现哪个。统一为单一 `Close() error` 后 `maybeClose` 收敛为单条断言，且全部组件满足 `io.Closer` 惯例。

### 13.1 改动面（已 grep 确认：`internal/` 下域外零实现者）

| 类别 | 文件 | 改动 |
|---|---|---|
| 接口 | `retriever/base.go` L33 | `Close()` → `Close() error` |
| 接口 | `vector_store/base.go` L55 | 同上 |
| 接口 | `knowledgebase/base.go` L45 + 编译期断言 L52-58 | 同上 |
| 实现（叶子） | `retriever/vector_retriever.go` / `sparse_retriever.go` / `hybrid_retriever.go` | 关 store 后 `return nil` |
| 实现（委托） | `retriever/graph_retriever_impl.go` / `agentic_retriever.go` | 逐个关下层，**返回首个非 nil 错误** |
| 实现（叶子） | `vector_store/chroma.go` / `milvus.go`、`indexing/indexer/chroma.go` / `milvus.go` | `return nil` |
| 实现（模板） | `knowledgebase/base.go` `Close` / `maybeClose` | `Close()` 恒返回 nil；`maybeClose` 单变体断言，**保留 Warn 吞** |
| 测试桩 | `retriever` 三个测试文件、`knowledgebase/simple_kb_test.go`、`knowledgebase/base_test.go` | 桩签名同步改 `Close() error { return nil }` |

### 13.2 语义边界（"行为零变化"的确切含义）

| 位置 | 现状 | 统一后 |
|---|---|---|
| 叶子实现 | void，本就没有可失败动作 | `return nil` |
| 委托实现（Graph/AgenticRetriever） | void，失败无通道 | 返回首个非 nil 错误（生产路径全 nil；仅错误桩可观测） |
| `BaseKnowledgeBase.Close` / `maybeClose` | error 变体 Warn 吞、void 变体直调 | 单变体 + Warn 吞，`Close()` 恒返回 nil |
| `KnowledgeBase.Close()` 调用方 | `kb.Close()` 语句 | Go 允许有返回值的调用作语句，**调用点零改动** |
| Python 对齐 | `_maybe_await` 两条探测分支 | 探测消失（单一签名），Warn 吞语义保留（对应 Python sync 分支） |

### 13.3 拒绝项

- **不做 `errors.Join` 聚合、不让 `KnowledgeBase.Close()` 上抛**——那是方案 C，与本任务"零行为变化"冲突，若需要另立一节。
- **不新增 `Indexer.Close() error` 接口方法**：Python `Indexer` base 也没有 close（仅 `ChromaIndexer` 有 sync close），故 `maybeClose(indexManager)` 的动态类型探测保留。**因此两个 Indexer 具体实现的 `Close()` 必须同步改成 error 变体**，否则统一后 `maybeClose` 断言不到它们 → 退化成"索引管理器不再被关闭"的真实回归。

### 13.4 验收

1. 三个包 `go build` + 单测全绿，且**既有断言一字不改**（这是"行为零变化"的证据）。
2. `go vet ./...` 跨包检查（接口变更会暴露遗漏的桩）。
3. `-race` 单包 + 全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`。
4. 独立 commit，message 前缀 `refactor(retrieval):`，不与 GraphKB 功能混提。
