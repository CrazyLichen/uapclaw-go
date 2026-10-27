# 13.21 HybridRetriever 设计文档

- 日期：2026-10-28
- 计划条目：IMPLEMENTATION_PLAN.md 13.21（HybridRetriever——向量+稀疏 RRF 融合检索）
- Python 源：`openjiuwen/core/retrieval/retriever/hybrid_retriever.py`（222 行）
- Python 测试基线：`tests/unit_tests/core/retrieval/retriever/test_hybrid_retriever.py`（132 行，8 用例）
- Go 目标：`internal/agentcore/retrieval/retriever/`（既有包新增 2 文件 + base.go 修订 + 桩改造）
- 前置依赖（全部就绪）：Retriever 四方法接口 + RetrievalMode（13.19 ✅）/ metadataString 归一化辅助 + 错误文案先例（13.20 ✅）/ VectorStore.HybridSearch 含 Chroma/Milvus 双实现，nil queryVector 纯稀疏路径已测（13.2/13.3 ✅）/ 错误码 155200/155201/155202 已注册零消费（155201 本节首次消费）
- 换序结论（2026-10-28 拍板）：**不换序**——13.21 前置依赖零缺口，消费方（13.23 GraphRetriever / 13.25 AgenticRetriever / 13.27 SimpleKB / 13.28 GraphKB）全部在后续章节，按编号顺序天然满足依赖方向；13.22 读取侧数据模型（TripleBeam/TripleMemory/MultiKBRetrievalResult）13.21 零消费，不值得提前

## 1. 背景与定位

领域13 读取侧第三节。HybridRetriever 是 **KB 编排层的兜底检索器**：SimpleKnowledgeBase（13.27）按 `index_type` 三分路由时，非 vector/bm25 的一切值（含 "hybrid"）落到本类；GraphRetriever（13.23）`_get_or_create_retriever` 按 mode 动态创建时 else 分支同样落到本类。

Agent 会话流程位置（不在 DeepAgent ReAct 主循环内，工作流组件链入口）：

```
用户查询 → [工作流图节点] KnowledgeRetrievalComponent (8.25 ⤴️ 依赖 13.26-13.28)
  → retrieve_multi_kb_with_source (13.27)
    → SimpleKnowledgeBase.retrieve (13.27)
        按 index_type 路由：
        ├─ "vector" → VectorRetriever (13.20 ✅)
        ├─ "bm25"   → SparseRetriever (13.20 ✅)
        └─ else（含 "hybrid"）→ ★HybridRetriever 本节★  ← 兜底默认分支
        （retrieval_config.agentic 时外包 AgenticRetriever，13.25）
    → GraphKnowledgeBase.retrieve (13.28)
        └─ GraphRetriever (13.23)
            └─ _get_or_create_retriever 动态创建：vector/sparse/else → ★本节★ else 分支
                （graph_retriever.py L367-373）
```

四个作用：
1. **KB 编排层兜底检索器**——"hybrid" index_type 在读取侧的唯一入口（13.27 L154-160 / 13.23 L367-373 双消费方）
2. **alpha 调权入口**——向量/稀疏权重（0=纯稀疏，1=纯向量，0.5=平衡），构造默认 0.5，调用时覆写；13.19 预留的 alpha 键本节首次真实消费（回填点①）
3. **155201 错误码首次消费**——`score_threshold + mode≠vector` 前置校验（13.20 vector_retriever.go L57-59 注释预留的回填点②）
4. **关键认知：本类不做 RRF 融合**——RRF 在 store 层完成（Chroma 两路搜索+RRFFusion / Milvus 原生 HybridSearch RRF，13.2/13.3 已交付），本类只是 mode 路由 + alpha 透传 + 归一化的薄层

对照 13.19-13.20 spec 的"hybrid 9 例留 13.21"——本节就是那 9 例的兑现（Python 实际 8 例）。

## 2. 已确认的设计决策（讨论结论）

| # | 决策点 | 结论 | 备选否决理由 |
|---|--------|------|-------------|
| 1 | alpha 调用侧收窄 | **平铺类型化字段 `Alpha *float64` 取代 map**（讨论演进：方案 C 全数值提升+非数值报错 → 中间路径 map+类型化写入口 → 终拍板全平铺）——编译期类型安全，指针区分"未设"（回落构造默认）与"显式 0"（合法纯稀疏） | 否决纯 map（运行期 any 收窄心智负担，写入口无类型保障）；否决 map+写入口并存（双入口防线不拆心智二重） |
| 2 | 构造签名 | **选项模式**：`NewHybridRetriever(store, embed, opts ...HybridRetrieverOption)` + `WithHybridAlpha(0.7)`，缺省回落 0.5 | 否决裸三参数（每个消费方手写 0.5，Python 默认值语义变成调用方责任）；13.17 WithTripleTemperature / 13.19 RetrieveOptions 双先例 |
| 3 | 实现结构 | **全字面复刻**：retrieve/rsr 各自完整实现两份三分支树（Vector 先例：Retrieve/RetrieveSearchResults 就是各自内联） | 否决文件内私有辅助（参数化 mode/threshold 后同包两种风格并存，上游同步两处对照）；否决跨 retriever 共享辅助（要动 13.20 已交付代码，三者细节差异参数化复杂度超过重复本身） |
| 4 | 章节顺序 | **不换序**，13.21 → 13.22 → 13.23 按编号 | 前置依赖零缺口 + 消费方全在后续 + 13.22 零消费不值得提前（见 §1 换序结论） |
| 5 | RetrieveOptions 平铺波及面 | base.go 修订（Extra→Alpha）+ base_test.go 3 例改写 + WithRetrieveAlpha 新增；Vector/Sparse 签名零改动（`_ ...RetrieveOption` 原样）；生产代码 WithRetrieveExtra 零外部调用（grep 全仓核实，重构窗口成本为零） | — |
| 6 | kwargs 构造死参数 | Python `__init__(..., **kwargs)` 的 kwargs 收而不用 → Go 构造不引入对应参数，归档 §8-2（13.19 决策 5"死参数对齐"先例） | — |
| 7 | 日志 | Python hybrid_retriever.py 零 logger → Go 零日志（排除日志英文规则——源头无日志，13.20 同款结论） | — |

## 3. 依赖验证结论（讨论期间核实）

- Python 源码 222 行逐行核对：`retrieve`（L41-130）/ `batch_retrieve`（L132-144）/ `retrieve_search_results`（L146-209）/ `close`（L211-221）
- **155201 前置校验先于 mode 分支**（L64-68 在 L70 if mode == "hybrid" 之前）——`mode="hybrid"+threshold` 报 155201 而非 155200，实施时顺序不能调换（Python 对齐审查要点：执行顺序不可调换）
- retrieve 与 rsr 是两段 ~90 行 90% 相同的 copy-paste（仅差 L64-68 threshold 校验 + L113-128 归一化/过滤段）——字面复刻两份
- 归一化 `chunk_id=result.id`（L126）——主键来源，同 Sparse（L76）、异于 Vector 的 metadata["chunk_id"]（L102）——Python 三作者不一致，13.20 决策 7 延续
- 阈值过滤仅 vector 模式生效且嵌套 `result.score is not None`（L117）——pydantic 必填恒非 None，Go float64 无 nil 对应物（§8-4 先例延续）
- 155200 文案 `Unsupported mode: {mode}`（L111/L208）——**与 Vector/Sparse 的 `XxxRetriever only supports ... mode, got {mode}` 不同**（Python 三作者文案不一致，逐字符字面复刻）
- vector 分支：embed nil → 155202（文案与 13.20 完全一致 `"embed_model is required for vector search"`）→ embed → Search → **空结果 SparseSearch 兜底**（L97-102，同 Vector 语义）——Hybrid 的 vector 分支是 Vector.Retrieve 的完整复刻
- hybrid 分支 embed 可选（L73-74 `if self.embed_model:`）→ queryVector=None 透传 → store 层纯稀疏路径（Chroma/Milvus nil 向量路径已测）
- rsr 无 threshold 校验（L165-209 无 L64-68 对应段）——与 retrieve 不对称，字面复刻（§8-8 先例延续）
- batch_retrieve 裸 gather 全 kwargs 透传（L142 `self.retrieve(query, top_k=top_k, **kwargs)`）→ alpha 经 opts 随 batch 透传
- close 的 getattr+iscoroutinefunction 鸭子探测（L215-221）→ 接口静态 Close() 直调消解（§8-7 先例延续，Go 接口必含 Close）
- Python 零 logger（全文核对）→ Go 零日志
- Go 侧 fakeVectorStore.HybridSearch 现为零实现（`return nil, nil`，base_test.go L91-93）→ 本节可编程化改造（hybridResult/hybridErr/lastAlpha/lastQueryVec/hybridCalls + mu 保护）
- 13.17 选项模式先例核实：`TripleExtractorOption` 类型名 + `WithTripleTemperature(t float64)` 签名 + 构造内默认值 + opts 循环覆写（triple_extractor.go L177-202）——HybridRetrieverOption 命名对齐
- 生产代码 `WithRetrieveExtra` 调用面：仅 base.go 定义 + base_test.go 自测，零外部消费（grep 全仓核实）——平铺重构无外部波及
- 13.19 spec §4.1/决策 5 原文"RetrieveOptions.Extra 收纳 **kwargs ... 13.21 Hybrid 消费 alpha"——本节为该约定的兑现 + 形态修订（Extra→Alpha 平铺），修订记录见 §10 回填清单

## 4. 类型与接口设计

### 4.1 base.go 修订（RetrieveOptions 平铺）

```go
// RetrieveOptions 检索选项（Python **kwargs 的闭合世界对应物）。
//
// 13.21 修订：原 Extra map[string]any 平铺为类型化字段（编译期类型安全，
// 2026-10-28 拍板）。原"Extra 收纳 **kwargs"设计见 13.19 spec §4.1——
// 修订记录归档两 spec，原约定不删除。
type RetrieveOptions struct {
    // Alpha hybrid 检索权重（0=纯稀疏，1=纯向量，0.5=平衡）。
    // nil = 未设置 → HybridRetriever 回落构造默认；非 nil = 调用时覆写。
    // 指针类型区分"未设"与"显式 0"（alpha=0 是合法纯稀疏语义，不能与
    // 零值混淆）。本节仅 HybridRetriever 消费；Vector/Sparse 不读
    // （对齐 Python 死参数）。
    Alpha *float64
    // 后续按需追加：GraphHops(13.23) / Filters(13.27 写入、retriever 层死参数)
}

// WithRetrieveAlpha 设置 hybrid 检索权重的 RetrieveOption。
func WithRetrieveAlpha(v float64) RetrieveOption {
    return func(o *RetrieveOptions) { o.Alpha = &v }
}
```

- `WithRetrieveExtra` **删除**（生产零调用，测试 3 例改写）
- `NewRetrieveOptions` 保留（聚合 opts 的归一助手，13.2 NewStoreOptions 先例）
- Vector/Sparse 的 `...RetrieveOption` 签名参数原样不动（依旧收而不用）

### 4.2 hybrid_retriever.go

```go
// HybridRetriever 混合检索器实现（向量+稀疏，RRF 融合在 store 层）。
//
// Python: openjiuwen/core/retrieval/retriever/hybrid_retriever.py
type HybridRetriever struct {
    vectorStore vector_store.VectorStore
    embedModel  embedding.BaseEmbedding // nil 合法（hybrid 模式可选；vector 模式缺失报 155202）
    alpha       float64                 // 构造默认 0.5（Python __init__ alpha=0.5）
}

// HybridRetrieverOption HybridRetriever 的构造选项。
type HybridRetrieverOption func(*HybridRetriever)

// WithHybridAlpha 设置 hybrid 权重（0=纯稀疏，1=纯向量）。
func WithHybridAlpha(v float64) HybridRetrieverOption {
    return func(r *HybridRetriever) { r.alpha = v }
}

// NewHybridRetriever 创建混合检索器。默认 alpha=0.5（对齐 Python 默认参数）。
// kwargs 构造死参数不引入（决策 6）。
func NewHybridRetriever(vectorStore vector_store.VectorStore, embedModel embedding.BaseEmbedding, opts ...HybridRetrieverOption) *HybridRetriever {
    r := &HybridRetriever{
        vectorStore: vectorStore,
        embedModel:  embedModel,
        alpha:       0.5,
    }
    for _, opt := range opts {
        opt(r)
    }
    return r
}

// 编译期断言（base.go 全局变量区追加第三条）
var _ Retriever = (*HybridRetriever)(nil)
```

import 路径沿用 13.19 约定：`vector_store "…/retrieval/vector_store"`、`embedding "…/foundation/store/embedding"`、`common "…/retrieval/common"`。

## 5. 核心流程设计

### 5.1 Retrieve（对齐 hybrid_retriever.py L41-130）

```
 1. alpha 解析：alpha := r.alpha; if opts.Alpha != nil { alpha = *opts.Alpha }
    【对齐 kwargs.get("alpha", self.alpha)：键缺失回落构造默认——Python 测试
      test_retrieve_with_custom_alpha 印证"调用覆写优先于构造默认"】
 2.【155201 前置校验，先于 mode 分支——顺序不可调换】
    scoreThreshold != nil && mode != "vector"
      → 155201 "score_threshold is only supported when mode='vector'"
    【13.20 回填点②兑现：Vector 侧同款校验是死分支已归档 §8-2，本节真实分支】
 3. mode 零值 "" → RetrievalModeHybrid（子类默认，§8-1 先例）
 4. mode == "hybrid":
      queryVector := nil
      if r.embedModel != nil {
          var err error
          queryVector, err = EmbedQuery(ctx, query); err 透传（err 作用域限本分支）
      }
      searchResults, err = HybridSearch(ctx, query, queryVector, topK, alpha, nil)
      【embed 可选：nil 走 store 层纯稀疏路径；filters 字面 None 复刻】
 5. mode == "vector":
      r.embedModel == nil → 155202 "embed_model is required for vector search"
      queryVector, err = EmbedQuery；err 透传
      searchResults, err = Search(ctx, queryVector, topK, nil)
      if len(searchResults) == 0 → SparseSearch(ctx, query, topK, nil) 兜底【同 13.20】
 6. mode == "sparse":
      searchResults, err = SparseSearch(ctx, query, topK, nil)
 7. else → 155200 "Unsupported mode: {mode}"
    【文案与 Vector/Sparse 的 only supports 不同——三作者不一致，逐字符复刻】
 8. 归一化循环：
    for _, r := range searchResults:
        if mode == "vector" && scoreThreshold != nil && r.Score < *scoreThreshold → continue
          【Python result.score is not None 无 Go 对应物，§8-4 延续；
            Python 条件含 mode=="vector" 前置——hybrid/sparse 模式即使传了
            threshold 也不过滤（155201 已拦非法组合，此处模式守卫为双保险结构）】
        append(common.RetrievalResult{
            Text: r.Text, Score: r.Score, Metadata: r.Metadata,
            DocID:   metadataString(r.Metadata, "doc_id"),
            ChunkID: r.ID,                        ← 主键来源（同 Sparse，异于 Vector，决策 7 延续）
        })
 9. 返回 results, nil
```

### 5.2 RetrieveSearchResults（对齐 L146-209，字面复刻第二份）

三分支树与 §5.1 步骤 3-7 **完全相同**（hybrid/vector/sparse/else 四路 + mode 零值回落），差异两处：
- **无 155201 threshold 前置校验**（Python rsr 没有这段——与 retrieve 不对称，字面复刻）
- **无归一化循环/阈值过滤**——直接返回 []SearchResult

### 5.3 BatchRetrieve（对齐 L132-144）

同 13.20 两兄弟结构：裸 errgroup 全启动、首错即返回、`r.Retrieve(ctx, q, topK, nil, "", opts...)` 全 opts 透传（alpha 经 opts 随 batch 透传，对齐 `**kwargs` 展开调用）。

### 5.4 Close（对齐 L211-221）

```go
func (r *HybridRetriever) Close() {
    if r.vectorStore != nil {
        r.vectorStore.Close()
    }
}
```
鸭子探测消解为接口静态直调（§8-7 先例，同 13.20 两兄弟）。

## 6. 错误处理与日志

| 触发点 | 错误码 | error_msg（逐字符对齐 Python） |
|--------|--------|------------------------------|
| Retrieve：threshold + mode≠vector（前置） | 155201 | `score_threshold is only supported when mode='vector'` |
| Retrieve/rsr：mode 不在三元集 | 155200 | `Unsupported mode: {mode}` |
| Retrieve/rsr vector 分支：embedModel nil | 155202 | `embed_model is required for vector search` |
| EmbedQuery / Search / SparseSearch / HybridSearch 错误 | 透传 | 不包装（Python await 直接冒泡） |

- 155201 由"注册零消费"转"已消费（13.21）"——错误码台账同步
- Python 零 logger → Go 零日志（归档事实，规范 3 无同步点）

## 7. 测试设计

### 7.1 桩改造（base_test.go fakeVectorStore）

```go
// 增量字段（mu 保护，BatchRetrieve 并发打桩先例）：
hybridResult   []common.SearchResult
hybridErr      error
hybridCalls    int
lastAlpha      float64    // alpha 覆写断言用（Python call_args 对应物）
lastQueryVec   []float64  // embed nil 纯稀疏路径断言用
lastQueryText  string
// HybridSearch 实现：计数 + 捕获 + 返回 hybridResult/hybridErr
```

### 7.2 Python 基线 1:1（8 例）

| # | Python 用例 | Go 用例 |
|---|------------|---------|
| H1 | test_retrieve_hybrid_mode | TestHybridRetriever_Retrieve_hybrid模式（2 结果、hybrid_search 调 1 次、embed_query 调 1 次） |
| H2 | test_retrieve_vector_mode | TestHybridRetriever_Retrieve_vector模式（search 调 1 次） |
| H3 | test_retrieve_sparse_mode | TestHybridRetriever_Retrieve_sparse模式（sparse_search 调 1 次，无 embed 构造） |
| H4 | test_retrieve_with_custom_alpha | TestHybridRetriever_Retrieve_alpha覆写（构造 0.7 + 调用 0.8 → lastAlpha 断言 0.8） |
| H5 | test_retrieve_vector_mode_without_embed_model | TestHybridRetriever_Retrieve_vector模式无embed（155202） |
| H6 | test_retrieve_with_score_threshold | TestHybridRetriever_Retrieve_阈值过滤（vector 模式 0.95/0.85 阈值 0.85 → 全保留） |
| H7 | test_retrieve_score_threshold_invalid_mode | TestHybridRetriever_Retrieve_阈值非vector模式（hybrid+threshold → 155201） |
| H8 | test_batch_retrieve | TestHybridRetriever_BatchRetrieve（2 query → 2×2） |

### 7.3 Go 增强（9 项，钉 Python 盲区）

1. **mode 零值回落**：`""` → hybrid（§8-1 钉死）
2. **embed nil 纯稀疏路径**：hybrid 模式无 embed → HybridSearch 收到 nil queryVector（lastQueryVec 断言）+ embed_query 零调用
3. **alpha 未设回落构造值**：构造 0.7 不传 opts → lastAlpha==0.7
4. **155200 文案**：mode="foo" → 逐字符断言 `Unsupported mode: foo`（与 Vector/Sparse 文案差异钉死）
5. **H6 边界**：阈值 0.9 → 0.85 被过滤（Python 用例只测了全保留边界）
6. **rsr 三分支**：hybrid/vector/sparse 各 1 例 + 直接返回 SearchResult 不归一化断言
7. **rsr 无 threshold 校验差异**：hybrid+threshold 调 rsr 不报 155201（与 retrieve 不对称钉死）
8. **HybridSearch 错误冒泡**：不包装原样返回
9. **Close**：closeCalls 断言 + nil store 不 panic

### 7.4 覆盖率目标

- retriever 包维持 ≥90% 水位（13.20 交付时 94.7%，本节纯增量无死分支）
- 编译期断言：`var _ Retriever = (*HybridRetriever)(nil)`
- 全量回归：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（CI 同款标签，memory 包 FTS 假失败教训）

## 8. 已知偏差归档（Go vs Python）

| # | 偏差 | 说明 |
|---|------|------|
| 1 | **kwargs 闭合世界**（本节新增） | Python **kwargs 开放世界（任意键可传、未识别键收下即丢）→ Go RetrieveOptions 平铺闭合结构体。上游新增 kwargs 键需 Go 加字段（同步纪律注记：后续每节 Python 对齐审查含 kwargs 清单检查）。13.23 graph_hops / 13.27 filters 届时按需追加 |
| 2 | 构造 kwargs 死参数 | Python `__init__(..., **kwargs)` 收而不用 → Go 构造不引入（13.19 决策 5 先例） |
| 3 | alpha=None 不可表达 | Python 显式 `alpha=None` 透传 store 层 TypeError 炸（bug 路径非特性）→ Go `Alpha *float64` nil=未设回落默认，编译期不可达该路径 |
| 4 | Score nil 检查消解 | Python `result.score is not None`（pydantic 必填恒非 None，防御性死代码）→ Go float64 无 nil 对应物（13.20 §8-4 先例延续） |
| 5 | rsr 无 threshold 校验 | Python retrieve 有 155201 前置校验、rsr 没有（同文件内不对称）→ 字面复刻（13.20 §8-8 先例延续） |
| 6 | 155200 文案三作者不一致 | Hybrid `Unsupported mode: {mode}` vs Vector/Sparse `XxxRetriever only supports ... mode, got {mode}` → 逐字符字面复刻 |
| 7 | 鸭子 close 探测消解 | 同 13.20 §8-7（getattr+iscoroutinefunction → 接口静态 Close() 直调） |
| 8 | mode 零值回落 | 同 13.19 §8-1（Python 显式传 `""` 走 155200；Go `""` 等同未设回落 "hybrid"） |

## 9. 实施完成回填清单（防丢失，逐项核对）

1. IMPLEMENTATION_PLAN.md 13.21 行 `☐`→`✅` + 产出描述（不触碰 13.22-13.28 已登记行）
2. **13.19 行追加修订注记**："RetrieveOptions.Extra 收纳 **kwargs" 描述后追加"（13.21 修订：平铺为 Alpha *float64 类型化字段）"——**原描述保留不删**（防丢失要求）
3. **13.20 vector_retriever.go L57-59 注释更新**："155201 留 13.21 HybridRetriever 真实消费" → "已由 13.21 HybridRetriever 消费"（回填点②兑现）
4. 错误码台账：155201 由"注册零消费"转"已消费（13.21）"
5. retriever 包 doc.go：文件目录加 hybrid_retriever.go 条目 + 核心类型索引加 HybridRetriever
6. 13.19 spec（2026-10-27-retriever-13.19-13.20-design.md）追加修订记录段：Extra→Alpha 平铺（2026-10-28 拍板），原 §4.1 内容保留
7. 验证点行保持 ☐ 不动（13.19-13.28 全链路未完）；跨域联动行（8.25 ⤴️ / 7.36 ⤵️）原样保留
8. git 提交参照 13.19-13.20 先例 `feat(retriever): 13.21 HybridRetriever ...`（显式 add 具体文件，禁 -am——git 搭便车陷阱记忆）

## 10. 文件清单

```
internal/agentcore/retrieval/retriever/           ← 既有包
├── doc.go                       # 修改：文件目录 + 索引增补
├── base.go                      # 修改：RetrieveOptions 平铺（Extra→Alpha）+ WithRetrieveAlpha + 断言第三条
├── hybrid_retriever.go          # ★ 新增（~200 行：结构体+构造+四方法）
├── base_test.go                 # 修改：3 个 Extra 测试改写平铺断言 + 桩 HybridSearch 可编程化
└── hybrid_retriever_test.go     # ★ 新增（H1-H8 + G1-G9）

IMPLEMENTATION_PLAN.md                            # §9 回填（13.21 行 + 13.19 修订注记）
docs/superpowers/specs/2026-10-27-retriever-13.19-13.20-design.md  # §9-6 修订记录追加
```

- vector_retriever.go / sparse_retriever.go：**零改动**（L57-59 注释更新属回填项 3，语义零变化）
- 桩改造放 base_test.go（fakeVectorStore 单一定义点，13.20 布局先例）
