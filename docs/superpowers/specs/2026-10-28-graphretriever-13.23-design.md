# 13.23 GraphRetriever 设计（GraphRetriever + TripleBeamSearch）

> 换序结论（2026-10-28 拍板）：**不换序**——前置依赖零缺口（13.19-13.22 全 ✅、RRFFusion 13.2 随出 ✅、错误码 155203-155207 注册零消费 ✅、MilvusClient 接口 13.3 就绪 ✅、chromav2 Get/Query 能力核实 ✅），消费方 13.25 AgenticRetriever/13.28 GraphKB 全在后续。13.24 QueryRewriter 维持编号序（域内零消费）。本节特殊点：需**回填修改已交付的 13.20/13.21 代码**（纯加法，不动已有方法签名与行为）。

## 1. 定位与流程位置

领域13 读取侧第四节。GraphRetriever 是**图增强检索的执行核心**，三元组索引（`kb_{id}_triples`，13.28 GraphKB 调 13.17 TripleExtractor 写入）的读取侧消费方。

```
Agent 会话（workflow 编排）
└─ KnowledgeRetrievalComp（8.25 ☐）
    └─ use_graph=true → GraphKnowledgeBase（13.28 ☐）
        └─ GraphRetriever（13.23 ← 本节）
            ├─ get_retriever_for_mode → Vector/Sparse/Hybrid（13.20/13.21）
            ├─ ① chunk 检索（查询 → top_k chunks）
            ├─ ② _fetch_triples：chunk_id 反查关联三元组
            ├─ ③ TripleBeamSearch：beam search 图扩展（默认 2 跳）
            ├─ ④ _fetch_chunks：扩展三元组 → 回捞原文 chunk
            └─ ⑤ RRFFusion([新, 原], k=60) 融合截断
```

下游：13.25 AgenticRetriever（`link_passages`/`link_triples` 强制要求底层 GraphRetriever；构造时 `getattr(retriever, "index_type")` 推导 default_mode）；13.28 GraphKB（构造注入 + index_type 注入）。

三元组 JSON 字符串流转链（13.22 spec 已钉死）：13.28 写 `metadata["triple"] = json.dumps([s,p,o])` → 本节 `json.loads`（beam 尾防御性解析 L182 / 候选裸解析 L213）→ 13.25 消费。

## 2. Python 源码基线

- 源码：`openjiuwen/core/retrieval/retriever/graph_retriever.py`（813 行）
- 测试：`tests/unit_tests/core/retrieval/retriever/test_graph_retriever.py`（464 行，3 测试类 20 用例）
- 组件：`TripleBeamSearch`（L24-225）+ `GraphRetriever`（L228-814）
- 依赖：`TripleBeam`（13.22 ✅）、`RetrievalResult`（13.22 ✅）、`Retriever`（13.19 ✅）、`rrf_fusion`（13.2 ✅）、numpy、pymilvus MilvusClient（延迟 import）

### 2.1 TripleBeamSearch 行为清单

| 项 | Python 行为 | Go 归宿 |
|---|---|---|
| 构造 | `num_beams=10 / num_candidates_per_beam=100 / max_length=2 / encoder_batch_size=256 / retrieve_mode=None`；`max_length<1` → 155204 `"expect max_length >= 1; got {max_length=}"` | `NewTripleBeamSearch` + 选项；encoderBatchSize 死参数归档 §8 |
| embed 获取 | `retriever.embed_model if hasattr(...) else None` | EmbedModelProvider 断言（§3.1） |
| beam_search | 空输入 warn 返回 `[]`；无 embed 155202 `"embed_model is required for beam search"`；triples+query 合批 `embed_documents(texts, batch_size=embed_model.max_batch_size)`；query 取末位；余弦 topk 初始 beams；`max_length-1` 轮扩展 | 逐条对齐；批大小消费链 §3.4 |
| _expand_beams | exist_triples（全 beam 文本集合）判重；候选路径 `(beam, triple\|None)`；None=保原 beam 不扩展；路径拼接文本重嵌入重打分；top num_beams；seen_beam_texts 按 `"; ".join` 格式去重；texts 空→原 beams；无 embed→原 beams | 逐条对齐 |
| _search_candidates | beam[-1] `metadata["triple"]` 解析（**有**防御：非 dict/JSON 错 → warn 返回 `[]`）；`len<2` 返回 `[]`；entities={首，尾} join 查询；mode 三级解析 `retriever.index_type > retrieve_mode > "hybrid"`，`bm25→sparse`；`retrieve(top_k=num_candidates_per_beam)`；三重过滤（Contains / 解析失败或 len<2 skip / 首尾都不在 entities skip）；空结果 warn | 逐条对齐；**候选解析 L213 是裸 json.loads 无防御**——TypeError 冒泡由上层 graph_expansion except 捕获降级，字面保留 |
| _cosine_scores | float32 asarray；`norm+1e-12` 哨兵；广播归一化；`q@c.T` | gonum floats + 手写归一化（§3.5） |
| _topk | `k=min(k,N)`；`k<=0→([],[])`；argpartition+argsort 降序 | 手写（§3.5） |
| _format_triples | `"; ".join(x.text for x in triples)` | 同构 |

### 2.2 GraphRetriever 行为清单

| 项 | Python 行为 | Go 归宿 |
|---|---|---|
| 构造 | 六可选参数（chunk_retriever/triple_retriever/vector_store/embed_model/chunk_collection/triple_collection）+ kwargs；`index_type=None`（上层注入） | NewGraphRetriever |
| _allowed_modes | vector→{vector}；bm25→{sparse}；hybrid→{vector,sparse,hybrid} | 同构 |
| _ensure_mode_allowed | index_type None 免检；未知 155203；不兼容 155204（含 allowed 清单文案 `sorted(allowed)`） | 同构 |
| _retriever_supports_mode | isinstance 三兄弟分发；`SUPPORTED_MODES` 类属性探测；兜底 True | Go 类型 switch（§3.3） |
| get_retriever_for_mode | 固定优先（不支持 155205 含 retriever 类名）；动态（无 store 155206）；**先污染 `store.collection_name` 再判空 155207**（顺序字面复刻）；三分支创建；index_type 非空注入动态 retriever | §3.2/§3.3 |
| retrieve | mode 校验 → `graph_hops=kwargs.get(2)` → threshold+mode≠vector 155201 → chunk 检索 → Info 日志 → graph_expansion | 逐条对齐 |
| graph_expansion | 空 chunks：sparse 二次回落检索（topk or 5）/其他 `[]`；triples 未传→_fetch_triples（异常 warn 回落 `[]`）；空 triples→原 chunks[:topk]；beam search（**异常 warn 回落原 chunks**——max_batch_size 炸点的降级终点）；空 beams→原 chunks；列对齐展开（按列优先）；_fetch_chunks；RRF k=60 融合；`fused[:topk]` | 逐条对齐 |
| _fetch_triples | chunk_ids 过滤空；get_retriever_for_mode；hasattr vector_store 探测；client/collection_name 鸭子挖；Milvus `chunk_id IN [...]` limit 100 / Chroma `{"chunk_id": {"$in": ids}}` limit 100；metadata 字符串 JSON 解析；doc_id 双源（顶层 document_id or metadata.doc_id）；chunk_id 取 metadata 回落 `""`；结果→RetrievalResult | §3.6 |
| _fetch_chunks | triples 的 chunk_id 去重集合；逐 chunk_id 并发 fetch（Milvus `==` limit 1 / Chroma eq limit 1）；异常 warn None 跳过；chunk_id 回落查询值 | §3.6 |
| batch_retrieve | 裸 gather 全启动 | 裸 errgroup 先例（13.20） |
| close | inspect.iscoroutinefunction 鸭子探测 chunk/triple 两 retriever | 接口静态 Close() 直调（13.21 §8-7 同款消解） |

### 2.3 Python 测试基线（20 例 1:1 映射）

- TestGraphRetriever 6 例：init_with_retrievers / init_with_vector_store / retrieve_score_threshold_invalid_mode / graph_expansion_empty_chunks / close（异步 close）/ close_sync_close / close_no_close_method
- TestTripleBeamSearch 10 例：init_invalid_max_length / beam_search_no_embed_model / beam_search_basic / search_candidates 7 例（bm25→sparse 映射 / 非 bm25 透传 / 无 index_type 用 retrieve_mode×2 / 双缺省 hybrid / retriever index_type 优先 / 真实 HybridRetriever 无 index_type 用 retrieve_mode）
- TestGraphRetrieverDynamicRetrieverIndexType 3 例：注入 hybrid 到 triple retriever / 注入 vector / 未设跳过

## 3. Go 设计

新文件 `internal/agentcore/retrieval/retriever/graph_retriever.go`（TripleBeamSearch + GraphRetriever + 能力接口）+ 测试。包内零新文件原则优先，能力接口随本文件走（retriever 包内私有定义+公共导出，见 §3.1 消解理由）。

### 3.1 能力接口（方案 A：hasattr/getattr 的 Go 同构物）

```go
// EmbedModelProvider 嵌入模型提供者（Python hasattr(retriever, "embed_model") 同构）。
type EmbedModelProvider interface {
    EmbedModel() embedding.BaseEmbedding
}

// IndexTypeCarrier index_type 载体（Python getattr(retriever, "index_type", None) + 动态注入同构）。
// 13.25 AgenticRetriever 构造 default_mode 推导复用。
type IndexTypeCarrier interface {
    IndexType() string      // 未注入返回 ""
    SetIndexType(string)
}

// VectorStoreAccessor 向量存储访问器（Python hasattr(retriever, "vector_store") + 属性挖取同构）。
type VectorStoreAccessor interface {
    VectorStore() vector_store.VectorStore
}

// MaxBatchSizeProvider 批大小提供者（Python embed_model.max_batch_size 属性挖取同构）。
type MaxBatchSizeProvider interface {
    MaxBatchSize() int
}
```

类型断言三态语义：未实现接口（Python 无属性）/ 实现但字段 nil（Python 属性 None）/ 实现且非 nil。`if p, ok := r.(EmbedModelProvider); ok && p.EmbedModel() != nil` 与 Python `hasattr + if not self.embed_model` 分支完全同构。

**回填（纯加法，不动已有方法签名）**：
- 13.20/13.21 三实现：私有 `indexType string` 字段 + `IndexType()/SetIndexType()`；`EmbedModel()` 返回 embedModel（nil 语义保真）；`VectorStore()` 返回 vectorStore
- SparseRetriever 补 `VectorStore()`（Python 侧它有 vector_store 属性）；embedModel 无此字段→不实现 EmbedModelProvider（对应 Python SparseRetriever 无 embed_model 属性——hasattr False 同构）
- store 侧：`MilvusVectorStore.CollectionName() string` + `SetCollectionName(name string)`；`ChromaVectorStore` 同款（Python `store.collection_name = x` 字段赋值污染的 setter 对应物；Chroma 的 getCollection 内部缓存语义在 Set 时重置，实施时核实）
- retrieval/embedding 四实现（API/Dashscope/OpenAI/vLLM）：`MaxBatchSize() int` getter

### 3.2 GraphRetriever 结构与构造

```go
type GraphRetriever struct {
    chunkRetriever    Retriever            // nil = Python None（动态创建路径）
    tripleRetriever   Retriever
    vectorStore       vector_store.VectorStore
    embedModel        embedding.BaseEmbedding
    chunkCollection   string
    tripleCollection  string
    indexType         string               // "" = Python None（上层 13.28 注入）
}
```

NewGraphRetriever：六参全收（对齐 Python 构造六可选参数），零校验（index_type 免检语义在 _ensureModeAllowed）。编译期断言 `var _ Retriever = (*GraphRetriever)(nil)`。

**RetrieveSearchResults**：返回 `nil, nil`（对齐 Python 基类空方法体返回 None——13.19 spec 预告"GraphRetriever 届时自行决定"的兑现）。

### 3.3 get_retriever_for_mode + _retriever_supports_mode

`GetRetrieverForMode(mode RetrievalMode, isChunk bool) (Retriever, error)`（导出：13.25 AgenticRetriever 消费）：

1. `_ensureModeAllowed(mode)`（indexType ""免检 / 未知 155203 `Unsupported index_type=%s` / 不兼容 155204 `mode=%s is incompatible with index_type=%s; allowed modes: [%s]`——allowed 排序逗号空格拼接逐字符对齐）
2. 固定 retriever 优先：`_retrieverSupportsMode` 不支持 → 155205 `Provided %s retriever %s does not support mode=%s`（chunk/triple + 类型名）
3. 动态创建：vectorStore nil → 155206 `vector_store is required for dynamic retriever creation`
4. **先 `SetCollectionName(collectionName)` 再判空 155207**（Python L339-346 污染先于校验的字面顺序复刻；`%s_collection is required for dynamic retriever creation`）
5. 三分支：vector（embedModel nil → 155202 `embed_model is required for vector mode`）/ sparse / hybrid（else 分支）
6. indexType 非空 → `SetIndexType(indexType)` 注入动态创建的 retriever（经 IndexTypeCarrier 断言——三实现全实现，恒成立；自定义 Retriever 实现未实现时跳过注入，对齐 Python `retriever.index_type = X` 对普通对象恒成功的宽松语义，偏差归档 §8）

`_retrieverSupportsMode`：Go 类型 switch（`*VectorRetriever`→mode==vector；`*SparseRetriever`→mode==sparse；`*HybridRetriever`→三 mode）+ `SUPPORTED_MODES` 类属性探测（Go 消解为可选接口 `SupportedModesProvider { SupportedModes() []string }`，13.19 接口无此方法——Python 的类属性探测通道，测试 stub 可选用）+ 兜底 true。

### 3.4 RetrieveOptions 扩容（13.21 Alpha 先例延续）

```go
type RetrieveOptions struct {
    Alpha               *float64  // 13.21 已有
    GraphHops           *int      // nil→2；显式 0→max_length=0→155204（Python 行为保真）
    NumBeams            *int      // nil→10；显式 0→_topk k<=0 返回空 beams→graph_expansion 空 beams 路径
    NumCandidatesPerBeam *int     // nil→100；显式 0→retrieve top_k=0（store 层行为）
}
// WithRetrieveGraphHops / WithRetrieveNumBeams / WithRetrieveNumCandidatesPerBeam
```

潜伏消费字段（有消费逻辑无生产方），GraphRetriever.retrieve 消费 GraphHops，graph_expansion → TripleBeamSearch 构造消费 NumBeams/NumCandidatesPerBeam。死参数归档：`encoder_batch_size`（存而不读）、`graph_expansion`（retrieve 不消费直透 TripleBeamSearch kwargs 吞掉）、`filters`（13.27 才有生产方）、`max_length` 直传（与 graph_hops 双传 Python TypeError 潜在 bug，Go 闭合世界无此入口）。死参数**不加字段**（Python 冗余执行去重先例：13.15 双 cp_image）。

graph_expansion 签名：`GraphExpansion(ctx, query, chunks, triples []RetrievalResult, topk *int, mode, opts...)`——triples 为 nil 切片区分"未传"（触发 _fetch_triples）与"显式空"（Python `if not triples` 两态同均为 falsy 走 _fetch——**注意 Python 传空列表也走 _fetch_triples**，Go nil/空切片同路径，语义同构）；topk *int 对齐 Python Optional[int]（nil→topk or 5 兜底/不截断两分支）。

### 3.5 数值层（gonum floats + 手写 topk）

- `cosineScores(queryVec []float64, candVecs [][]float64) []float64`：gonum `floats.Norm` 逐向量归一化（+1e-12 哨兵）+ `floats.Dot` 点积。**float32 截断不复刻**（bit-exact 因 numpy BLAS 求和序本就不可达，归档 §8）
- `topkIndices(scores []float64, k int) ([]int, []float64)`：`k=min(k,N)`；`k<=0→nil,nil`；索引数组 `sort.Slice` 降序取前 k（argpartition 的选择集+降序语义等价；等值非稳定性与 Python 同构归档 §8）

### 3.6 _fetch 层（方案 1：store 具体类型断言）

```go
// 包内私有，对齐 Python _query_by_filter_milvus/_query_by_filter_chroma
type fetchItem struct {  // Python dict {"id","content","metadata","score"} 的结构化对应物
    id       string
    content  string
    metadata map[string]any
    score    float64
}
func queryByFilterMilvus(ctx, client vector_store.MilvusClient, collection, filterExpr string, limit int) ([]fetchItem, error)
func queryByFilterChroma(ctx, client chromav2.Client, collection string, where map[string]any, limit int) ([]fetchItem, error)
```

- 探测点：`VectorStoreAccessor → vs.(*vector_store.MilvusVectorStore) / vs.(*vector_store.ChromaVectorStore)`（Python isinstance(client, MilvusClient)/_is_chroma_client 的 store 层等价物；两分支外 → warn `[graph] Unsupported client type` 返回空，对齐 Python）
- Milvus 分支：`chunk_id IN ["a", "b"]` 字符串拼 filter（双引号转义字面复刻）→ `client.Query(QueryOption)` → ResultSet→fetchItem（output_fields=["*"] 语义：全字段，实施时按 milvus SDK v2.6.5 ResultSet 结构映射，13.3 先例）
- Chroma 分支：`GetCollection → collection.Get(WithWhere(EqString/In), WithLimit)` → GetResult→fetchItem（score 0.0 字面复刻——Chroma get 不返分数）
- fetchItem→RetrievalResult 归一化：metadata 字符串 JSON 解析防御；doc_id 双源 `item document_id or metadata.doc_id`；chunk_id `metadata.get("chunk_id", "")`
- _fetch_chunks 并发：裸 errgroup（对齐 asyncio.gather 全启动；逐 chunk_id fetchChunk 返回 nil 跳过——errgroup 不能返回 nil 元素，改为结果切片+nil 过滤，**注意 errgroup 首错即返回与 Python gather 首错传播同构，但 Python fetchChunk 内部 try/except 吞错返回 None，Go 对应 fetchChunk 内部吞错返回 nil**——errgroup 实际永不收到 error，语义等价）

### 3.7 TripleBeamSearch 结构

```go
type TripleBeamSearch struct {
    retriever            Retriever     // 候选检索器
    numBeams             int
    numCandidatesPerBeam int
    maxLength            int
    encoderBatchSize     int           // 死参数：存而不读（Python 字面复刻，归档 §8）
    retrieveMode         string        // "" = Python None
    embedModel           embedding.BaseEmbedding  // 构造时经 EmbedModelProvider 断言提取
}
```

NewTripleBeamSearch：选项式（WithTripleNumBeams 等）+ `maxLength < 1` → 155204。embedModel 提取：`if p, ok := retriever.(EmbedModelProvider); ok { embedModel = p.EmbedModel() }`（nil 断言不到→embedModel nil，beam_search 155202）。

**max_batch_size 消解（拍板 c）**：批量嵌入前 `if mp, ok := embedModel.(MaxBatchSizeProvider); ok { opts = append(opts, embedding.WithBatchSize(mp.MaxBatchSize())) } else { return nil, 155202 "embed_model does not support max_batch_size" }`——保留 Python 炸点→graph_expansion except 降级返回原 chunks 的完整路径，错误形态 AttributeError→155202 归档 §8。

beam_search 主流程：空 triples → Warn `beam search got empty input triples` 返回 nil；embedModel nil → 155202 `"embed_model is required for beam search"`；`append(texts, query)` 合批嵌入 → queryVec=末位 → cosineScores → topkIndices(numBeams) → 初始 beams（逐个 `NewTripleBeam([]common.RetrievalResult{triples[idx]}, score)`）→ `maxLength-1` 轮：`errgroup` 并发 `_searchCandidates`（**裸 errgroup 首错即返回对齐 asyncio.gather 无 return_exceptions——Python 此处 gather 无异常吞噬，_searchCandidates 内部吞错返回 []，errgroup 实际不收 error；_search_candidates 的 RuntimeError("unexpected empty beam") 路径 Go 对应 panic？否——len<1 分支不可达（beam 构造至少 1 元素），Go 直接删去归档 §8**）→ `_expandBeams`。

_expandBeams：existTriples 全 beam Contains 集合；候选路径 `[]struct{beam *TripleBeam; triple *common.RetrievalResult}`（nil=保原 beam）；texts 空或 embedModel nil → 原 beams；重嵌入打分 topk；seenBeamTexts 去重；返回新 beams。

_searchCandidates：`beam.Get(-1)` 尾元素 metadata["triple"] 防御解析（非字符串/json 错 → Warn 返回 nil）；`len<2` 返回 nil；entities 集合+join 查询串；mode 三级（IndexTypeCarrier 断言 > retrieveMode > "hybrid"；bm25→sparse）；`retriever.Retrieve(ctx, queryStr, numCandidatesPerBeam, nil, mode)`（scoreThreshold nil、无 opts——Python kwargs 空）；三重过滤（`beam.Contains(x)` / 裸 json.loads 候选**无防御**：Go 侧 `json.Unmarshal` 错误冒泡返回（对齐 TypeError 冒泡由上层降级）；解析成功 `len<2` 或首尾都不在 entities → skip）；空结果 Warn。

### 3.8 日志（逐条对齐，全英文 Msg）

11 条：beam_search 空输入 Warn / embed 缺失路径无日志（错误码自带）/ retrieve 主流程 Info（`graph_expansion=True chunk_hits=%d topk=%d mode=%s`）/ graph_expansion 空 chunks Warn / fetch triples Info（chunks=%d triples=%d）/ fetch 失败 Warn / no triples Info / beam failed Warn / empty beams Info / expansion results Info（triples=%d additional_chunks=%d）/ beam 空候选 Warn / beam 无 triple metadata Warn / beam 候选解析失败 Warn。组件 ComponentAgentCore（retrieval 归属先例）。

## 4. 回填清单（兑现历史预留，不丢失）

| # | 预留出处 | 本节兑现 |
|---|---------|---------|
| 1 | 13.21 spec：RetrieveOptions "后续按需追加：GraphHops(13.23)" | §3.4 GraphHops+两兄弟 |
| 2 | 13.19 spec：三实现字段私有 "13.23 GraphRetriever 到时按需补" getter | §3.1 EmbedModel()/VectorStore() |
| 3 | 13.22 spec：TripleBeam C2 浅拷贝密封"为 append(beam.Triples(), t) 准备" | _expandBeams 落地 `append(beam.Triples(), t)` |
| 4 | 错误码台账：155203/155204/155205/155206/155207 注册零消费 | 本节全部转已消费 |
| 5 | 13.20 ⤵️ PGVectorStore 拆后交付 | **确认无关不随出**（Python 13.23 _fetch 仅 Milvus/Chroma 两分支） |
| 6 | Python `RetrievalResult` chunk_id 无 `omitempty` 差异 | 实施时核实 13.22 result.go 已带 omitempty——fetchItem 归一化时零值行为与 Python `metadata.get("chunk_id", "")` 空串语义一致，无需动 |

## 5. 测试设计

Python 20 例 1:1 映射 + Go 增强：

- **能力接口三态**：stub 未实现接口（对应 _RetrieverStubNoIndexType）/ 实现但返回 nil / 实现且非 nil——三态分支全覆盖（Python 仅隐式覆盖两态）
- **GraphRetriever 主链**：mock chunk/triple retriever（Retriever 接口桩）走 init/score_threshold 155201/empty chunks/Close 三形态（Go Close 静态——sync/async/无方法三例合并为接口桩 Close 断言调用）
- **beam_search 全链**：embed 桩两段式 side_effect（初始批+扩展批）；mode 三级解析 7 例逐一映射（IndexTypeCarrier 断言优先级/双缺省 hybrid/真实 HybridRetriever）
- **动态创建**：注入 index_type 到动态 Hybrid/Vector retriever（经 SetIndexType 断言回读）；未设跳过
- **Go 增强**：fetch 层 Milvus/Chroma 双分支 mock（milvusClient 桩 Query 断言 filter 表达式字面/chroma 桩 Get 断言 where dict）；候选裸解析 TypeError 冒泡→graph_expansion 降级返回原 chunks 的端到端用例；GraphHops 显式 0 → 155204；RetrieveSearchResults 返回 nil,nil
- 覆盖率目标 ≥85%（retriever 包基线 94.7% 不回落）

## 6. 章节顺序

不换序（§1 结论）。13.24 维持编号序。领域13 剩余：13.24 QueryRewriter → 13.25 AgenticRetriever → 13.26 KB 抽象 → 13.27 SimpleKB → 13.28 GraphKB。

## 7. 风险与开放点（实施时核实）

1. chromav2 `GetResult` 字段映射（ids/documents/metadata 数组对齐 Python zip 形态）——实施时读 SDK 源码钉死
2. Milvus SDK v2.6.5 `QueryOption` 构造（FilterExpr 字符串/OutputFields/Limit）——13.3 milvusClientAdapter.Query 已有通路
3. ChromaVectorStore.SetCollectionName 与内部 getCollection 缓存的交互——构造时 `s.collection` 已绑原集合名，Set 后需失效缓存（Python 侧 collection_name 赋值同样不刷新已持有的 collection 引用，**行为偏差点：Python get 是 `client.get_collection(name)` 每次新建，Go 侧需对齐"每次按名取"而非用缓存引用**——实施时核实 Go getCollection 实现取用哪条路径，选与 Python 每次新建语义一致者）
4. Milvus ResultSet 的 document_id 顶层字段（Python 注释"may be at top level"）——按 Go SDK 实际返回结构映射，双源逻辑保序

## 8. 偏差归档（实施后按实际编号回填细化）

| # | 偏差 | 理由 |
|---|------|------|
| 1 | 探测对象 client→store 具体类型断言 | Go 接口无 runtime isinstance；client 由 store 挖出等价 |
| 2 | fetchItem 结构体替代 Python dict | 类型安全；四字段闭合对齐 dict 键集 |
| 3 | float32 截断不复刻 | numpy BLAS 求和序 bit-exact 不可达；排名稳健 |
| 4 | argpartition→sort.Slice 全排序 | 选择集+降序语义等价；等值非稳定两侧同构 |
| 5 | encoder_batch_size 死参数存而不读 | Python 字面复刻 |
| 6 | 候选解析 TypeError→Go Unmarshal error 冒泡 | 错误形态变异，降级终点（graph_expansion except）保真 |
| 7 | max_batch_size 断言失败 AttributeError→155202 | 拍板 c：保留炸点→降级路径，错误形态业务化 |
| 8 | _search_candidates len(beam)<1 RuntimeError 分支删去 | beam 构造恒 ≥1 元素，不可达 |
| 9 | SetIndexType 对未实现 IndexTypeCarrier 的动态 retriever 跳过注入 | Python 动态属性恒成功的宽松语义，Go 类型化收窄 |
| 10 | 死参数 kwargs（encoder_batch_size/graph_expansion/filters/max_length 直传）不加 RetrieveOptions 字段 | 冗余执行去重先例 |
