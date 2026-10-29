# 13.26 KnowledgeBase 抽象 设计文档

> 对应 Python：`openjiuwen/core/retrieval/knowledge_base.py`（211 行）
> Python 测试：`tests/unit_tests/core/retrieval/test_knowledge_base.py`（8 例）+ `test_knowledge_base_validation.py`（15 例）
> 上游依赖：13.1-13.3 Indexer/VectorStore、13.5 Chunker、13.10 Parser、13.17 Extractor、foundation embedding.BaseEmbedding、foundation model_clients.BaseModelClient、common.KnowledgeBaseConfig/RetrievalConfig
> 下游消费方：13.27 SimpleKnowledgeBase / 13.28 GraphKnowledgeBase（同包子类）；8.25 KnowledgeRetrievalComp（⤴️ 依赖 13.26-13.28 完整）

## 0. 在 agent 会话流程中的位置与作用

```
【写入侧：文档 → 索引】（不进 agent 会话，管理面操作）
用户上传文件/URL
  └─ KnowledgeBase.ParseFiles / ParseURLs          ← 13.26 模板方法统一入口
      └─ Parser 层（13.10-13.16，AutoParser 路由 PDF/Word/HTML/微信/...）
  └─ Document 列表
  └─ KnowledgeBase.AddDocuments（抽象，13.27/13.28 实现）
      └─ Chunker 分块（13.5-13.9）→ Indexer 嵌入+入库（13.1-13.3）

【读取侧：查询 → 检索】（进 agent 会话）
AgentServer 会话（E2A 消息）
  └─ DeepAgent / 单 Agent（WorkflowController 意图识别选中工作流）
     └─ Runner.run_workflow_streaming
        └─ 工作流图节点: KnowledgeRetrievalComponent（8.25 ⤴️，未实施）
           ├─ 懒初始化: create_vector_store + OpenAIEmbedding
           │   + 按 use_graph 二分构造 GraphKB / SimpleKB   ← 13.26-13.28 唯一会话内消费方
           ├─ retrieve_multi_kb_with_source（13.27 顶层聚合）
           │   └─ kb.Retrieve（13.26 接口 / 13.27 SimpleKB / 13.28 GraphKB 实现）
           │       └─ Retriever 六兄弟（13.19-13.25）
           └─ 结果聚合 → context 注入 → LLM
```

**作用**（211 行四件事）：
1. **统一出入口**——5 抽象方法（Add/Retrieve/Delete/Update/GetStatistics）+ 4 模板方法（ParseFiles/ParseURLs/DeleteCollection/Close），SimpleKB/GraphKB 可互换，上层多 KB 聚合只依赖抽象
2. **配置一致性防线**——`__setattr__` + validate_index：VectorStore（读侧）与 Indexer（写侧）的 7 项配置逐项比对，不一致抛 155508。防"两份手工维护的配置漂移"（写 db1 读 db2 → 检索静默为空，极难排查）
3. **Chroma 本地限制拦截**——两组件任一类型名 "chroma" 前缀且 index_type != "vector" 且 strict → 155508
4. **资源生命周期**——close() 鸭子探测双组件；_ensure_loaded 钩子（Milvus 构造期 load collection）

**为什么有两个组件可比**：index_manager（写侧：BuildIndex 写数据+建索引）与 vector_store（读侧：Search/SparseSearch/HybridSearch）连接**同一个物理集合**但**分开构造、分开传入**。二者各自持有 7 项 schema 描述（database_name/text_field/vector_field/doc_id_field/distance_metric/sparse_vector_field/metadata_field），任一漂移都会导致写入与读取错位。

## 1. 包结构（拍板：单包收容三兄弟）

```
internal/agentcore/retrieval/knowledgebase/
├── doc.go           # 包文档（本节创建）
├── base.go          # KnowledgeBase 接口 + BaseKnowledgeBase + 构造器/setter/模板方法（13.26）
├── validate.go      # ValidateIndex + attrEqual + isChromaType + storageAttrProvider（13.26）
├── simple.go        # SimpleKnowledgeBase + RetrieveMultiKB×2（13.27 ⤵️ 后续填充）
└── graph.go         # GraphKnowledgeBase（13.28 ⤵️ 后续填充）
```

本节只创建 doc.go / base.go / validate.go + 测试；simple.go / graph.go 留 13.27/13.28（目录注释中预留）。

单包理由：① Python 三文件同目录=同模块命名空间，Go 同包最直译；② 13.28 GraphKB 消费 13.27 的模块级函数 retrieve_multi_kb（Python `from simple_knowledge_base import retrieve_multi_kb`），同包零导出体操；③ 子类访问 BaseKnowledgeBase 字段（Python self.chunker）同包直读，不必开导出访问器；④ 8.25 消费方只经构造器+接口。

依赖方向（无环）：`knowledgebase → {common, vector_store, indexer, parser, chunker, extractor, foundation/embedding, foundation/model_clients, exception, logger}`。全部既有包，无反向依赖（vector_store/indexer 均不 import knowledgebase）。

## 2. 接口与基座结构体

### 2.1 KnowledgeBase 接口

```go
// KnowledgeBase 知识库抽象接口（Python ABC：5 抽象 + 4 模板 + config 访问器）
type KnowledgeBase interface {
    // 5 抽象方法（子类必须实现）
    AddDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error)
    Retrieve(ctx context.Context, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error)
    DeleteDocuments(ctx context.Context, docIDs []string, opts ...KBOption) (bool, error)
    UpdateDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error)
    GetStatistics(ctx context.Context) (map[string]any, error)
    // 4 模板方法（BaseKnowledgeBase 默认实现）
    ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error)
    ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error)
    DeleteCollection(ctx context.Context, collection string) error
    Close()
    // 属性访问器
    Config() common.KnowledgeBaseConfig
}
```

- 文档类型 `reranker.Document`（foundation/store/reranker，项目 Parser/Chunker 层既有契约）
- `config *common.RetrievalConfig`：nil = 用默认值（Python `Optional[RetrievalConfig]`）
- `Config()` 访问器：Python 公有属性 `.config` 的镜像；13.27 聚合函数消费 `kb.config.kb_id`（日志字段）
- 编译期断言 `var _ KnowledgeBase = (*BaseKnowledgeBase)(nil)`

### 2.2 BaseKnowledgeBase 基座

```go
// BaseKnowledgeBase 子类嵌入的共享基座（13.10 BaseParser 先例）
type BaseKnowledgeBase struct {
    strictValidation bool                        // Python strict_validation，默认 true
    config           common.KnowledgeBaseConfig
    vectorStore      vector_store.VectorStore
    embedModel       embedding.BaseEmbedding     // foundation 层接口
    parser           parser.Parser
    chunker          chunker.Chunker
    extractor        extractor.Extractor
    indexManager     indexer.Indexer
    llmClient        model_clients.BaseModelClient
}
```

### 2.3 构造器与选项

```go
func NewBaseKnowledgeBase(config common.KnowledgeBaseConfig, opts ...KnowledgeBaseOption) (*BaseKnowledgeBase, error)
```

选项（8 个）：`WithVectorStore / WithEmbedModel / WithParser / WithChunker / WithExtractor / WithIndexManager / WithLLMClient / WithStrictValidation(false)`。

- config 必填无默认；strictValidation 默认 true（Python 字面）
- **构造尾部**：`if vectorStore != nil && indexManager != nil → postAssignValidation`（对齐 Python __init__ 赋值序：vector_store 先、index_manager 后，后者赋值时刻两者配齐触发唯一构造期校验点）
- 返回 error：Python 构造抛 BaseError → Go 构造器返回 error（build_error → exception.BuildError 既有映射）

### 2.4 二次赋值 setter（方案 B 拍板）

```go
// SetVectorStore 构造后替换读侧组件（Python kb.vector_store = vs 的显式对应物）
func (b *BaseKnowledgeBase) SetVectorStore(vs vector_store.VectorStore) error
// SetIndexManager 构造后替换写侧组件（Python kb.index_manager = im）
func (b *BaseKnowledgeBase) SetIndexManager(im indexer.Indexer) error
```

赋值后同样走 `vectorStore != nil && indexManager != nil → postAssignValidation`。

**语义等价性论证**：Python `__setattr__` 只对 `{"vector_store","index_manager"}` 集合生效且要求双非空——即"两个组件首次配齐或之后再次变更时校验"。Go 两个入口（构造尾部 + 双 setter）覆盖全部触发点；绕过 setter 的直接字段赋值在 Go 同包外不可达（字段私有），13.27/13.28 同包子类走构造器统一路径。归档 §8-5。

### 2.5 KBOption（kwargs 双语义分流）

```go
// KBOptions 知识库方法选项（Python **kwargs 收纳）
type KBOptions struct {
    // FileName ParseFiles 的 file_name 参数（parse_files kwargs 专用，空串=未设回落路径末段）
    FileName string
    // Extra 其余额外参数：ParseURLs 全量透传 parser；5 抽象方法子类消费
    // （13.27 retrieve → AgenticRetriever max_iter 从此走）
    Extra map[string]any
}
type KBOption func(*KBOptions)
```

### 2.6 访问器

同包子类直读字段，但测试/外域需断言：`VectorStore() / EmbedModel() / Parser() / Chunker() / Extractor() / IndexManager() / LLMClient() / StrictValidation() / Config()`（Python 公有属性测试断言的对应物，13.25 访问器先例）。private 字段同包测试直读为主，访问器主要服务 8.25 外域。

## 3. 校验流水线 postAssignValidation（三段式）

```
触发：构造尾部 / SetVectorStore / SetIndexManager（均要求双组件非 nil）
  └─ postAssignValidation(ctx)
     ├─ ① ValidateIndex()
     │    for attr in [database_name, distance_metric, text_field, vector_field,
     │                 sparse_vector_field, metadata_field, doc_id_field]  ← 固定顺序
     │      vsVal = storageAttr(vectorStore, attr)
     │      imVal = storageAttr(indexManager, attr)
     │      !attrEqual(vsVal, imVal) → 155508
     │    strict && vectorStore != nil → vectorStore.CheckVectorField() 错误传播
     ├─ ② if isChromaType(vectorStore) || isChromaType(indexManager):
     │      strict && config.IndexType != IndexTypeVector → 155508
     │      "Chroma database does not support sparse embedding & hybrid search in local mode yet"
     └─ ③ if el, ok := vectorStore.(ensureLoader); ok:
            el.EnsureLoaded(ctx) 错误传播（异常炸构造对齐 Python）
```

### 3.1 storageAttrProvider 消费接口（B1+V1 拍板）

```go
// storageAttrProvider 校验流水线的属性消费接口（KB 包内非导出，消费侧接口惯例）
type storageAttrProvider interface {
    DatabaseName() string
    DistanceMetric() string
    TextField() string
    VectorField() any        // 装箱联合类型事实：*ChromaVectorField / MilvusVectorFieldConfig
    SparseVectorField() string
    MetadataField() string
    DocIDField() string
}
```

`storageAttr(obj, attr)`：断言 `obj.(storageAttrProvider)`——实现则按 attr 调 getter；未实现 → 该对象此属性按 nil（getattr(obj, attr, None) 同构，Python "缺属性" 的 Go 对应物）。

7 属性固定顺序 = Python `["database_name", "distance_metric"] + data_fields` 字面。

### 3.2 attrEqual 比较器（V1 拍板）

```go
func attrEqual(a, b any) bool {
    if a == nil || b == nil { return a == nil && b == nil }
    return reflect.DeepEqual(a, b)
}
```

- 双 nil → true（test_validation_passes_when_attributes_are_none 对应）
- 单 nil → false → 155508
- DeepEqual 首步即类型比较：Chroma 配置 vs Milvus 配置 → false → 155508（**混配拦截正是我们要的行为**）；同源同参数 → true；字符串属性同串 → true
- 测试 fake 的 `VectorField() any` 返回 `any("test_vector_field")` 同串 → 通过（Python MagicMock setattr 字符串的测试形态同构）

### 3.3 isChromaType（R1 拍板）

```go
func isChromaType(obj any) bool {
    t := reflect.TypeOf(obj)
    if t == nil { return false }
    if t.Kind() == reflect.Ptr { t = t.Elem() }
    return strings.HasPrefix(strings.ToLower(t.Name()), "chroma")
}
```

忠实复刻 Python `type(...).__name__.casefold().startswith("chroma")` 命名约定探测：不是行为契约（无接口同构物），第三方 `ChromaFooStore` 命名类型同样被拦——与 Python 行为一致。反射仅读类型名字符串，不做动态派发控制流。

### 3.4 ensureLoader 钩子

```go
// ensureLoader 可选加载钩子（Python getattr(vs, "_ensure_loaded", None) 鸭子探测同构）
type ensureLoader interface {
    EnsureLoaded(ctx context.Context) error
}
```

MilvusVectorStore 回填导出 `EnsureLoaded(ctx) error`（一行转发既有私有 ensureLoaded）；ChromaVectorStore 无此方法 → 断言失败跳过（Python 侧 Chroma 无 _ensure_loaded 同构）。

### 3.5 错误消息逐字复刻

```
Vector store and index manager have incompatible {attr} configs:
- Vector Store ({vsTypeName}) is using "{vsVal}"
- Index manager ({imTypeName}) is using "{imVal}"
```

- attr 用 Python 蛇形名（database_name/distance_metric/text_field/vector_field/sparse_vector_field/metadata_field/doc_id_field）——Python 消息字面
- 类型名 reflect 取（fake 类型名照样进消息，test_validation_error_message_includes_type_names 对应）
- 值渲染：nil → `None`；string 原样带引号；其他对象 `%v`（pydantic str() 渲染差异归档 §8-2）

## 4. 模板方法（4 个）

### 4.1 ParseFiles

```
parser nil → 155500 "parser is required for parse_files"
for file_path in filePaths:
    file_name = opts.FileName 空串 ? file_path 按 "/" split 取末段 : opts.FileName   ← 字面 "/" 不用 filepath.Base
    file_id   = uuid.NewString()
    docs, err = parser.Parse(ctx, file_path, parser.WithDocID(fileID), parser.WithExtra("file_name", fileName))
    err → logger.Error("Failed to parse file %s: %s") + continue
return all
```

**只透传 file_name**（Python 字面：parse 只收 file_name/file_id 三个参数，其余 kwargs 不透传——与 ParseURLs 不对称，各自字面复刻）。选项用既有 `parser.WithDocID(fileID)` + `parser.WithExtra("file_name", fileName)`（已核实存在，base.go L107/L117）。

### 4.2 ParseURLs

```
parser nil → 155500 "parser is required for parse_urls"
for url in urls:
    !parser.Supports(url) → Warn "Parser does not support URL %s, skipping" + continue
    doc_id = uuid.NewString()
    docs, err = parser.Parse(ctx, url, parser.WithDocID(docID), /* Extra 逐键 WithExtra 展开 */ ...)
    err → Error "Failed to parse URL %s: %s" + continue
return all
```

**kwargs 全量透传**（Python `**kwargs` 直通 parse——13.11 AutoParser Extra["file_name"] 链路）。Go 落地：KBOptions.Extra 逐键 `parser.WithExtra(k, v)` 展开为 ParseOption。

### 4.3 DeleteCollection

```
vectorStore nil → 155503 "vector_store is required for delete_collection"
return vectorStore.DeleteTable(ctx, collection)
```

### 4.4 Close

```
maybeClose(vectorStore); maybeClose(indexManager)   ← 顺序字面
maybeClose(obj):
    obj nil → return
    if c, ok := obj.(interface{ Close() }); ok { c.Close(); return }        // 真实类型 void 签名
    if c, ok := obj.(interface{ Close() error }); ok {                       // 错误变体：Warn 吞
        if err := c.Close(); err != nil { Warn "Failed to close object" }
    }
```

双变体可选接口承接 Python `iscoroutinefunction(close)` / `hasattr close` 双探测（async 形态编译期消解归档 §8-3）。test_close_with_exception（close 抛异常被吞）由 `Close() error` 变体承载。

**注**：VectorStore 接口已有 `Close()`（void），Indexer 无 Close 接口方法但 ChromaIndexer/MilvusIndexer 有 Close() void 具体方法——maybeClose 的断言分支各自可达。

## 5. 回填清单（计划表 13.26 产出列注明，不动历史行）

| 目标 | 回填内容 | 先例 |
|---|---|---|
| ChromaVectorStore / MilvusVectorStore（vector_store 包 chroma.go/milvus.go） | +6 getter：`DatabaseName() string / TextField() string / VectorField() any / SparseVectorField() string / MetadataField() string / DocIDField() string`（DistanceMetric 已有） | 13.23 三检索器访问器回填 |
| ChromaIndexer / MilvusIndexer（indexer 包） | 同款 +6 getter | 同上 |
| MilvusVectorStore | +`EnsureLoaded(ctx context.Context) error`（一行转发私有 ensureLoaded） | 同上 |
| vector_store/doc.go、indexer/doc.go | 核心类型索引补 getter 说明一行（文件条目不变，getter 进既有文件） | doc.go 规范 |

**DatabaseName 复用证据**：Python SimpleKB.add_documents 读 `vector_store.config.database_name`（13.27 L107）；Go store 未存 config 对象，getter 即对应物——本节回填非只服务 validate 的死代码。

**回填不丢内容保证**：13.2/13.3 已完成行历史描述零改动；13.26 行产出列完整记录"回填 4 类型×6 getter + EnsureLoaded"；领域13 顶部备注链追加一行；13.23 调用方（GraphRetriever capability 断言）不受影响（只增方法不改签名）。

## 6. 错误码（本节从零消费转已消费）

| 码 | 常量 | 触发点 | error_msg |
|---|---|---|---|
| 155500 | StatusRetrievalKbParserNotFound | ParseFiles/ParseURLs parser nil | `parser is required for parse_files` / `parser is required for parse_urls` |
| 155503 | StatusRetrievalKbVectorStoreNotFound | DeleteCollection vs nil | `vector_store is required for delete_collection` |
| 155508 | StatusRetrievalKbDatabaseConfigInvalid | ValidateIndex 7 属性任一不等；Chroma 拦截 | 三行模板 / `Chroma database does not support sparse embedding & hybrid search in local mode yet` |

155501（chunker）/155502（index_manager）/155504（build_index）留 13.27 消费。

## 7. 日志映射（ComponentAgentCore）

| Python | Go（结构化字段） |
|---|---|
| `Failed to parse file %s: %s`（Error） | `.Str("file_path", p).Err(err).Msg("Failed to parse file")` |
| `Failed to parse URL %s: %s`（Error） | `.Str("url", u).Err(err).Msg("Failed to parse URL")` |
| `Parser does not support URL %s, skipping`（Warn） | `.Str("url", u).Msg("Parser does not support URL, skipping")` |
| `Failed to close object`（Warn，exc_info） | `.Err(err).Msg("Failed to close object")` |

文案英文（既定规则）。Python 文件共 4 条 logger 调用，全对齐。

## 8. 偏差归档

| # | 偏差 | 理由 |
|---|---|---|
| 1 | llmClient 类型收紧：Python 基类 `Any` → Go `model_clients.BaseModelClient` | SimpleKB 已收窄 BaseModelClient，基类提前对齐（下游签名唯一合法形态） |
| 2 | vector_field 错误消息渲染 `%v` vs pydantic `str()` | 保证 attr 名/None/字符串值逐字对齐；对象内部渲染形态差异不可避免 |
| 3 | close 的 async 形态编译期消解；双变体可选接口（Close()/Close() error）承接鸭子探测 | Go 无协程函数；错误变体承载 test_close_with_exception 映射 |
| 4 | 全方法 +ctx 首参；构造期 ctx 用 context.Background() | Python async 对应物 |
| 5 | `__setattr__` 触发点收窄为"构造尾部 + 双 setter" | Go 无法拦截字段赋值；两入口覆盖全部合法触达路径，字段私有同包外不可绕过（拍板：方案 B） |
| 6 | ParseURLs 透传落地：KBOptions.Extra 逐键展开为 parser.WithExtra(k,v)；ParseFiles 只透 file_name（Python 两方法 kwargs 透传范围本就不对称） | 既有选项 WithDocID/WithExtra 逐键式（base.go L107/L117），已核实 |
| 7 | Python `file_path.split("/")[-1]` 字面实现，非 filepath.Base | 跨平台路径分隔符行为差异：Windows 反斜杠路径 Python 也只按 "/" 切——字面复刻 |
| 8 | `str(uuid.uuid4())` → uuid.NewString() | 等价直译 |
| 9 | storageAttrProvider 未实现接口 → 7 属性全 nil（Python 逐属性 getattr） | Python 真实类型 7 属性要么全有（Chroma/Milvus 两对真实类型）要么 mock 全设——"部分属性缺失"仅在 MagicMock 局部 setattr 病态场景，Go fake 实现接口即全有。粒度差异归档 |

## 9. 测试设计

### 9.1 桩基建

- **fakeAttrStore / fakeAttrIndexer**：实现 storageAttrProvider 7 getter + 字段可配置（含各属性设 nil 复刻 test_validation_passes_when_attributes_are_none）；同时实现 VectorStore/Indexer 接口（不触达的方法空方法体返回零值——校验/模板方法路径不触达真实存储语义）
- **fakeCloserStore**：`Close() error` 变体，可注入错误（test_close_with_exception）
- **fakeEnsureLoader**：实现 ensureLoader + 调用计数（增强测试）
- **ChromaFakeStore**（类型名 chroma 前缀）：触发 R1 拦截路径（增强测试）

### 9.2 Python 23 例 1:1 映射

test_knowledge_base.py 8 例（base_test.go）：

| Python | Go |
|---|---|
| test_init | TestNewBaseKnowledgeBase_默认构造（字段断言） |
| test_init_with_components | TestNewBaseKnowledgeBase_全组件构造 |
| test_init_with_mismatch_metrics | TestNewBaseKnowledgeBase_距离度量不匹配 |
| test_init_with_mismatch_names | TestNewBaseKnowledgeBase_库名不匹配 |
| test_close_with_async_close | TestClose_错误变体关闭（async 形态映射到 Close() error 桩） |
| test_close_with_sync_close | TestClose_void变体关闭 |
| test_close_with_none_components | TestClose_双nil组件 |
| test_close_with_exception | TestClose_关闭异常吞并Warn |

test_knowledge_base_validation.py 15 例（validate_test.go）：

| Python | Go |
|---|---|
| test_validation_passes_when_all_attributes_match | TestValidateIndex_全属性匹配 |
| test_validation_skipped_when_vector_store_is_none | TestValidateIndex_vs缺失跳过 |
| test_validation_skipped_when_index_manager_is_none | TestValidateIndex_im缺失跳过 |
| test_validation_runs_when_setting_vector_store_after_index_manager | TestSetVectorStore_后置触发校验 |
| test_validation_runs_when_setting_index_manager_after_vector_store | TestSetIndexManager_后置触发校验 |
| test_validation_fails_on_mismatch_{database_name,distance_metric,text_field,vector_field,sparse_vector_field,metadata_field,doc_id_field} | TestValidateIndex_七属性逐项不匹配（7 例） |
| test_validation_error_message_includes_type_names | TestValidateIndex_错误消息含类型名与值 |
| test_validation_passes_when_attributes_are_none | TestValidateIndex_双nil属性通过 |
| test_validation_fails_on_first_mismatch | TestValidateIndex_首个不匹配即停（顺序敏感） |

（15 = 表中单例 7 + 拆分 7 + 双 nil + 首错序，即 7 单例行中 mismatch_{} 为 7 例合计 15）

### 9.3 Go 增强测试（Python 未覆盖）

1. isChromaType 命名约定路径：ChromaFakeStore + index_type=hybrid + strict → 155508 消息逐字；index_type=vector → 放行
2. strict_validation=false：属性不匹配仍校验（Python strict 只影响 check_vector_field 与 chroma 拦截，不影响 7 属性比对——**重要语义**，增强测试固化）
3. ensureLoader 调用计数：Milvus 型桩 EnsureLoaded 被调；Chroma 型桩不被调
4. 三段式顺序：属性比对失败时 EnsureLoaded 未被调（①先于③）
5. SetVectorStore(nil)/SetIndexManager(nil)：无校验无 panic（Python setattr None 不触发）
6. 构造只传单组件（vs 有 im 无 / im 有 vs 无）：不触发校验（对齐 Python 单组件赋值时刻另一侧 None）
7. attrEqual 直测：跨类型 DeepEqual false；同源同参 true；双 nil true；单 nil false
8. ParseFiles/ParseURLs/DeleteCollection 模板方法（Python 基类测试未覆盖——abstract 测试范式不测模板方法，Go 补全）：parser nil 155500；file_name 回落 "/" split；supports 不支持 Warn 跳过；解析异常 Error 继续；DeleteCollection 155503 + 转发
9. KBOptions 双语义：FileName 只进 ParseFiles；Extra 只进 ParseURLs 透传（桩断言收到的 opts）
10. -race：并发 Close/ValidateIndex 只读场景

### 9.4 覆盖率目标

≥ 85% 硬线，预期 93%+（包新建，回填 getter 进既有包各自覆盖率微升）。全量回归带 CI 同款构建标签（`CGO_ENABLED=1 -tags "sqlite_fts5 test"`）。

## 10. 实施步骤（writing-plans 细化前的骨架）

1. 回填：4 类型 × 6 getter + MilvusVectorStore.EnsureLoaded + 两 doc.go 索引行（先回填后建包，依赖就绪）
2. knowledgebase 包：doc.go → base.go（接口/基座/构造/setter/选项/访问器/模板方法）→ validate.go（storageAttrProvider/attrEqual/isChromaType/ensureLoader/ValidateIndex/postAssignValidation）
3. base_test.go + validate_test.go：桩基建 → 23 例映射 → 增强测试
4. IMPLEMENTATION_PLAN.md：13.26 行 ✅ + 产出列完整描述 + 领域13 顶部备注链追加
5. 全量回归 + -race + 覆盖率

## 11. 拍板决策记录

| # | 决策 | 拍板 |
|---|---|---|
| 1 | 章节组织：按编号单独实施 13.26，不合并不换序 | 用户拍板（依赖全就绪、211 行有分量、13.27 依赖 13.26 反向无依赖） |
| 2 | `__setattr__` 复刻形态：构造尾部校验 + 显式 setter（方案 B） | 用户拍板 |
| 3 | 属性访问器：回填 6 getter 到 4 真实类型（B1） | 用户拍板 |
| 4 | vector_field 槽位与比较：any + reflect.DeepEqual（V1） | 用户拍板（Chroma 单类/Milvus 5 类接口是 Python 源码真实形态，强行统一不值） |
| 5 | Chroma 拦截：reflect 读类型名前缀（R1） | 用户拍板 |
| 6 | ensureLoaded 钩子：回填导出方法 + 非导出可选接口断言 | 设计内定（无分叉） |
| 7 | 包组织：knowledgebase 单包收容三兄弟 | 设计确认（用户整体确认） |
