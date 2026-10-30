# 13.27 SimpleKnowledgeBase + ConfigManager 设计文档

> 对应实现计划：领域十三 13.27（IMPLEMENTATION_PLAN.md L833）
> Python 源码：`openjiuwen/core/retrieval/simple_knowledge_base.py`（381 行）+ `openjiuwen/core/retrieval/utils/config_manager.py`（112 行）
> 前置依赖：13.26 基座 ✅ / 13.19-13.21 Retriever ✅ / 13.22 结果模型 ✅ / 13.25 AgenticRetriever ✅ / 13.2-13.3 Indexer+VectorStore ✅ / 13.5 Chunker ✅
> 后续消费方：13.28 GraphKnowledgeBase（直接构造 SimpleKB 作 base_kb + 复用两聚合函数）、8.25 KnowledgeRetrievalComp（消费 retrieve_multi_kb_with_source）

## 1. 流程位置与作用

Python 侧完整调用链：

```
用户会话 → Runner 执行工作流 Graph
  └─ KnowledgeRetrievalComponent（8.25，最终消费方）
      ├─ 懒初始化：逐 KB create_vector_store + OpenAIEmbedding
      │    → use_graph ? GraphKB(13.28) : SimpleKB(13.27)
      ├─ retrieve_multi_kb_with_source(kbs, query, config)   ← 本节聚合函数
      │    └─ 并发各 kb.retrieve（单库失败 Warn 吞→[]）
      │         └─ SimpleKB.retrieve                          ← 本节核心方法
      │              ├─ retriever 懒创建（vector→Vector / bm25→Sparse / 其他→Hybrid）
      │              ├─ agentic=True 时外包 AgenticRetriever(13.25)
      │              └─ Retriever(13.19-13.21) → VectorStore(13.2/13.3)
      │    └─ 聚合：text 去重 + score 取 max + kb_ids 并集 + 降序截断
      └─ _format_output：texts + "\n\n".join → context → 工作流下游
```

13.27 是领域13"查"侧管线在 agent 会话运行时的**总入口**：13.26 抽象的完整实现（5 抽象方法）、13.22 `MultiKBRetrievalResult` 的唯一生产场景、13.25 `AgenticRetriever` 的包装消费方。非图路径的检索查询管线在本节完成后闭环。

## 2. 文件结构（拍板：方案 A）

```
internal/agentcore/retrieval/knowledgebase/
├── doc.go            # 13.26（更新文件目录）
├── base.go           # 13.26（不动）
├── validate.go       # 13.26（不动）
├── simple_kb.go      # 新增：SimpleKnowledgeBase（5 抽象方法 + 懒创建 + agentic 包装）
└── multi_kb.go       # 新增：RetrieveMultiKB / RetrieveMultiKBWithSource

internal/agentcore/retrieval/utils/
└── config_manager.go # 新增：ConfigManager
```

- 聚合函数留 knowledgebase 包：参数类型是本包 `KnowledgeBase` 接口，13.28 的 `retrieve_multi_graph_kb` 同包直调，8.25 单包 import 拿全（曾评估 common 包——循环依赖否决）
- ConfigManager 归 utils 包（Python 同构目录），只依赖 exception/common + yaml.v3（go.mod 已有），不 import knowledgebase

## 3. SimpleKnowledgeBase 类型设计（拍板：模式 2）

```go
type SimpleKnowledgeBase struct {
    BaseKnowledgeBase              // 值嵌入（13.8 HybridChunker 嵌 BaseChunker 先例）
    retriever   retriever.Retriever // nil = 懒创建
    retrieverMu sync.Mutex          // 懒创建并发保护（13.24 Mutex 先例）
}

func NewSimpleKnowledgeBase(config common.KnowledgeBaseConfig,
    retriever retriever.Retriever, opts ...KnowledgeBaseOption) (*SimpleKnowledgeBase, error)
```

- **retriever 显式参数**（Python 签名不对称的直译：基类不收 retriever，子类特有）+ 基座选项直接透传 `NewBaseKnowledgeBase`
- 复用单一构造入口，postAssignValidation（database_name mismatch 构造期报错，Python 测试 1:1 场景）天然继承
- 值嵌入安全：BaseKnowledgeBase 纯数据无锁；同包访问非导出字段（`s.vectorStore` 等）
- 编译期断言 `var _ KnowledgeBase = (*SimpleKnowledgeBase)(nil)`——兑现 13.26 留的断言缺口（伏笔 2）
- 13.28 GraphKB 构造 base_kb 时传 nil retriever 自然

## 4. 五抽象方法对齐要点

| 方法 | 关键行为（Python 字面） |
|---|---|
| `AddDocuments(ctx, documents, opts...)` | chunker 缺→155501（文案 `chunker is required for add_documents`）；index_manager 缺→155502；`strictValidation && vectorStore` 时 `CheckVectorField()`；空白 id 才填 uuid（`strings.TrimSpace(ID)==""`）；`ChunkDocuments`；完成日志（文档数/chunk 数）；IndexConfig **带** `UseCaptionForImages`（`kb_{kb_id}_chunks`）；database_name 防御式取（见 §4.1）；`BuildIndex(WithIndexExtra("database_name", db))`；false→155504 `Failed to build index`；成功 Info 日志 + 返回 doc_ids |
| `Retrieve(ctx, query, config, opts...)` | 见 §5 |
| `DeleteDocuments(ctx, docIDs, opts...)` | index_manager 缺→155502（`for delete_documents`）；strict 校验同 add；逐 doc_id `DeleteIndex`，**循环不中断**，任一 false→总 false |
| `UpdateDocuments(ctx, documents, opts...)` | 前置校验同 add（文案 `for update_documents`）；**无 uuid 填充**；IndexConfig **不带** `UseCaptionForImages`（与 add 不对称，Python 真实行为）；逐 doc 按 `DocID` 过滤 chunks 分组 `UpdateIndex`；成功才收集 doc_id |
| `GetStatistics(ctx)` | index_manager 缺→`{"kb_id", "index_exists": false}` 2 键短路；有→`GetIndexInfo` error **直接冒泡**（Python 无 try）+ 8 键 map（kb_id/index_type/index_info/has_parser/has_chunker/has_extractor/has_embed_model/has_vector_store） |

### 4.1 database_name 防御式取

Python：`getattr(getattr(self.vector_store, "config", None), "database_name", "")` + 非 str 归零。Go 同构：接口断言 `interface{ DatabaseName() string }`（13.26 `storageAttrProvider` 同型，包内私有接口或直接匿名接口），未实现→""。`isinstance` 非 str 归零在 Go 编译期天然满足（归档）。

### 4.2 has_xxx 统计键

6 个 has_xxx 布尔值：parser/chunker/extractor 在基座，embed_model/vector_store 同。Python `self.parser is not None` → Go `s.parser != nil` 直译。

## 5. Retrieve 懒创建与 agentic 包装

```
Retrieve(ctx, query, config, opts...):
  1. s.retriever == nil → retrieverMu 加锁双检：
     a. vectorStore 缺 → 155503（"vector_store or retriever is required for retrieve"）
     b. index_type 三分支：
        "vector" → NewVectorRetriever(vs, embedModel)
        "bm25"   → NewSparseRetriever(vs)
        其他      → NewHybridRetriever(vs, embedModel)
     c. 赋值 s.retriever
     （Python 延迟 import 在 Go 为包级 import，归档）
  2. config == nil → 私有默认（TopK 5；拍板：私有回落，不动 common 包）
  3. mode 推导：index_type "vector"→"vector" / "bm25"→"sparse" / 其他→"hybrid"
  4. config.Agentic → NewAgenticRetriever(s.retriever, s.llmClient, agenticOpts...) 包装
     - KBOptions.Extra["max_iter"] → WithAgenticMaxIter（int 断言）
     - 其余键静默忽略（拍板：处理 2，归档 §8）
  5. retriever.Retrieve(ctx, query, topK, scoreThreshold, mode,
       WithRetrieveFilters(config.Filters))   ← 伏笔 1 回填
```

### 5.1 Filters 槽（伏笔 1：13.19 计划行"13.27 SimpleKB 写 filters"）

`retriever.RetrieveOptions` 新增 `Filters map[string]any` + `WithRetrieveFilters` 选项。**死参数字面复刻**：Python 中 filters 进了 retriever kwargs 但 Vector/Sparse/Hybrid/Agentic 四个实现全不读（store 调用硬编码 `filters=None`），Go 同样写而不消费。spec 归档偏差，13.28+ 出现消费方再激活。

### 5.2 config nil 回落（拍板：私有回落）

Python `config or RetrievalConfig()` 默认 `top_k=5`。Go 零值 `RetrievalConfig{}` 的 TopK=0 会传给 retriever 触发 155210。SimpleKB 内私有 `defaultRetrievalConfig()`：nil 时返回 `TopK:5` 的配置。Agentic 检查在回落之后（nil config 的 Agentic 零值 false 与 Python 默认一致）。

## 6. 聚合函数（multi_kb.go）

### 6.1 RetrieveMultiKB

```go
func RetrieveMultiKB(ctx context.Context, kbs []KnowledgeBase,
    query string, config *common.RetrievalConfig, topK *int) []string
```

- 空 kbs → `[]string{}`
- **裸 gather**（拍板，13.17/13.25 方案 A 先例）：goroutine 预分配槽位 `results[i]` 各写各的，无 fail-fast，`wg.Wait` 收齐
- 单库 error → Warn 日志（文案 `retrieve_multi_kb: kb_id=%s failed: %s` 对齐 Python %s 风格）→ 软失败返回空列表
- 聚合：`merged[text] = max(score, existing)`；score nil→0.0
- 排序：`sort.SliceStable` 按 score 降序（Python Timsort 稳定排序等价）
- limit 真值链：`topK or config.TopK or 5`（nil 判断）
- 返回文本列表

### 6.2 RetrieveMultiKBWithSource

```go
func RetrieveMultiKBWithSource(ctx context.Context, kbs []KnowledgeBase,
    query string, config *common.RetrievalConfig, topK *int) []common.MultiKBRetrievalResult
```

- 同上 gather/软失败（Warn 文案 `retrieve_multi_kb_with_source: ...`）
- **配对**：Python `zip(kbs, all_results)` → Go 索引并进（`for i := range kbs`），等价
- kb_id：`kbs[i].Config().KbID`（接口方法，无 getattr 需求，归档）
- 聚合（首见初始化 / 后续更新）：
  - 首见：`score`、`raw_score = meta.get("raw_score", score)`、`raw_score_scaled = meta.get("raw_score_scaled", score)`、`kb_ids` set、`metadata`（首见 meta 固定不更新）
  - 后续：`score = max`；raw_score/raw_score_scaled **非 nil 才 max**（Python `if raw_score is not None`，对应 metadata 里显式给了值才参与；Go 内部聚合结构用 `*float64` 区分，最终装填 13.22 模型 float64）
- `KbIDs`：set 并集 → `sorted` 后装填（`sort.Strings`）
- limit 真值链同 6.1；产出 `[]common.MultiKBRetrievalResult`（13.22 唯一生产场景兑现，伏笔 3）

## 7. ConfigManager（utils/config_manager.go）

```go
type ConfigManager struct {
    configs map[string]any // Python Dict[str, BaseModel] 任意 config 类型
}
func NewConfigManager(configPath string) (*ConfigManager, error) // 空串=不加载
```

- `LoadFromFile(path)`：不存在→155300（`Configuration file does not exist: %s`）；后缀 .json→`encoding/json`；.yaml/.yml→`yaml.v3`（**155301 pyyaml 缺失为编译期死路径**，归档 §8）；其他后缀→155302（`Unsupported configuration file format: %s`）；解析 `map[string]any` → `KnowledgeBaseConfig` 构造（**忽略未知键**对齐 pydantic 默认，归档）→ 存 `"knowledge_base"` 键
- `SaveToFile(path)`：无 `"knowledge_base"` 键→155303 `No configuration to save`；`model_dump` → JSON（indent 2 + 非 ASCII 不转义，`json.Encoder` + `SetEscapeHTML(false)` + `SetIndent`，13.16 JSONParser 同款）/ YAML（`yaml.Marshal` allow_unicode 默认）；后缀分支同上
- `GetConfig(configType reflect.Type) (any, bool)`：按类型断言遍历（Python `get_config(Type)` isinstance 同构；Go 泛型替代方案见偏差）；未找到→nil,false
- `GetKnowledgeBaseConfig()`：`"knowledge_base"` 键不存在→155304 `Knowledge base configuration not loaded`；存在且是 `KnowledgeBaseConfig`→返回
- `UpdateConfig(config any)`：**按类型名存键**（`reflect.TypeOf(config).String()`）——与 load 的 `"knowledge_base"` 键**双轨不一致**，`GetKnowledgeBaseConfig` 在 update 路径必失败，Python 真实行为字面复刻（拍板，归档 §8）

### 7.1 GetConfig 的 Go 形态

Python `get_config(Type[T]) -> Optional[T]` 是泛型签名。Go 两选一：
- `GetConfig[T any](m *ConfigManager) (T, bool)`——调用侧 `GetConfig[common.KnowledgeBaseConfig](m)`，类型断言 `cfg, ok := v.(T)`
- 采用泛型版（与 Python TypeVar 语义最贴近，不引 reflect 参数版本）

## 8. 偏差归档

| # | 偏差 | Python 行为 | Go 决策 | 理由 |
|---|---|---|---|---|
| 1 | ConfigManager 键双轨 | load 存 "knowledge_base"，update 按类名存键，get_knowledge_base_config 在 update 路径必失败 | 字面复刻（拍板） | bug 也是行为，对齐原则 |
| 2 | 155301 pyyaml 死路径 | `try: import yaml except ImportError` 运行时分支 | yaml.v3 编译期依赖，155301 保留定义不可达 | Go 依赖编译期保证 |
| 3 | 延迟 import | retrieve 内 `from ... import VectorRetriever` | 包级 import（Go 惯例，13.25 同） | 编译期解析 |
| 4 | asyncio.gather 并发 | 事件循环单线程并发 | goroutine 槽位 + WaitGroup（13.17/13.25 方案 A 先例） | 已验证模式 |
| 5 | getattr 双层防御取 database_name | `getattr(getattr(vs, "config", None), "database_name", "")` + 非 str 归零 | 接口断言 `DatabaseName() string`，未实现→""；非 str 归零编译期消解 | 13.26 storageAttrProvider 同型 |
| 6 | agentic kwargs 直通 | `AgenticRetriever(retriever, llm_client, **kwargs)` 只认 max_iter，其他键 TypeError 崩 | `Extra["max_iter"]` 映射 WithAgenticMaxIter，其余键**静默忽略**（拍板：处理 2） | Go 无 kwargs；生产链路与测试均不传，实际不可达；静默 vs 崩是行为分叉已认可 |
| 7 | zip 配对 | `zip(kbs, all_results)` | 索引并进 `for i := range kbs` | 等价直译 |
| 8 | pydantic 忽略未知键 | `KnowledgeBaseConfig(**data)` 默认忽略 | JSON/YAML 解析为 map 后显式取已知字段构造 | pydantic 默认行为 |
| 9 | has_xxx 统计 | `is not None` | `!= nil` 直译 | 平凡 |
| 10 | update_config 键 | `type(config).__name__`（Python 类名 `KnowledgeBaseConfig`） | `reflect.TypeOf(config).String()`（Go 类型名 `common.KnowledgeBaseConfig` 含包前缀） | 各自语言的类型名字面 |
| 11 | ConfigManager 死代码 | 零生产消费方 | 照复刻（10.6.24 audio_tools 先例） | 对齐原则 |
| 12 | score nil→0.0 | `r.score if r.score is not None else 0.0` | Go RetrievalResult.Score 类型视 common 包定义（float64 直装或指针判 nil） | 实施时按现状直译 |
| 13 | RawScore None 边界 | 聚合内部 None 区分"未设置"，装填 `v.get("raw_score")` 可为 None | 内部聚合结构 `*float64`，13.22 模型 RawScore 是 float64 直装（13.22 已归档） | 已有先例 |
| 14 | filters 死参数 | 写入 kwargs 四实现不读 | `RetrieveOptions.Filters` 写而不消费（伏笔 1 字面复刻） | 13.19 计划行承诺 |
| 15 | AsyncMock 并发语义 | Python 测试 mock 的"并发"是协程假象 | Go goroutine 真并发，软失败语义靠槽位写保证 | 语言差异 |

## 9. 测试设计

### 9.1 Python 27 例 1:1 映射

**TestSimpleKnowledgeBase（17）**：parse_files 成功/无 parser/异常吞（方法实现在 13.26 基座，但**本节保留 1:1 映射**——测 SimpleKB 组合入口行为，与 Python 测试文件归属一致）；database_name mismatch 构造期报错；add 成功（doc_ids 断言 + chunk_documents/build_index 调用断言）/无 chunker/无 index_manager/build 失败；retrieve 带 retriever/agentic（patch 类 → Go 直接构造真 AgenticRetriever 包 mock retriever 或注入工厂，断言构造参数 retriever/llm_client 透传）/非 agentic/无 retriever 且无 vector_store；delete 成功（调用次数）/无 index_manager；update 成功；statistics 成功/无 index_manager。

**TestRetrieveMultiKb（4）**：空列表/成功去重排序/部分失败/with_source（MultiKBRetrievalResult 字段断言：text/score=0.95/kb_ids=[kb1,kb2]）。

**TestConfigManager（6）**：路径初始化/JSON 加载/YAML 加载/文件不存在/GetConfig/GetConfig 未找到。

### 9.2 Go 增强项

1. 懒创建并发 -race：同一 KB 并发 Retrieve 只建一次 retriever（计数断言）
2. update 路径 `UseCaptionForImages=false` vs add 路径 true 不对称断言（fake Indexer 捕获 IndexConfig）
3. ConfigManager 键双轨：UpdateConfig 后 GetKnowledgeBaseConfig 报 155304（固化 Python 真实行为）
4. with_source 聚合：首见 meta 固定不更新 / 后续 raw_score 非 nil 才 max / KbIDs sorted 去重
5. limit 真值链三档：topK 显式/config.TopK/默认 5
6. YAML 往返：SaveToFile → LoadFromFile 字段一致
7. filters 伏笔：Retrieve 传 Filters 后 retriever 收到（fake Retriever 捕获 RetrieveOptions）
8. agentic max_iter 映射 + 其他键静默忽略
9. 稳定排序：同 score 不同 text 的顺序稳定性
10. 155301 死路径不可达（编译期，仅归档说明）
11. agentic 分支 llmClient 为 nil 时 NewAgenticRetriever 返回 155209 error 冒泡（Python AsyncMock llm 不会 nil，Go 需显式补）

### 9.3 覆盖率

- knowledgebase 包基线 94.9%，utils 包基线（fusion/common 已有）
- 目标 ≥85%（规范下限），预期 90%+

## 10. 伏笔回填对账

| 伏笔 | 出处 | 本节动作 |
|---|---|---|
| 1 | 13.19 计划行"13.27 SimpleKB 写 filters" | `RetrieveOptions.Filters` + `WithRetrieveFilters`（写而不消费） |
| 2 | 13.26 base.go 断言注释"完整断言由 13.27 承接" | `var _ KnowledgeBase = (*SimpleKnowledgeBase)(nil)` |
| 3 | 13.22 MultiKBRetrievalResult | RetrieveMultiKBWithSource 生产装填 |
| 4 | 13.25 AgenticRetriever | agentic 分支包装消费 |
| 5 | 13.2/13.3 DatabaseName() getter | AddDocuments 防御式取值消费 |
| 6 | 错误码 155501/155502/155504 + 155300-155304 | 零消费→已消费（共 8 个；155503 已由 13.26 消费，本节复用不计入新增） |

## 11. doc.go 与计划表更新

- `knowledgebase/doc.go`：文件目录加 simple_kb.go / multi_kb.go 条目，包概述补"SimpleKB 完整实现 + 多 KB 聚合"
- `utils/doc.go`：文件目录加 config_manager.go 条目
- `IMPLEMENTATION_PLAN.md`：13.27 ☐→✅；13.19 计划行的 filters 伏笔标注更新为已兑现

## 12. 实施顺序建议（供 writing-plans 展开）

1. Task 1：utils/config_manager.go + 测试（独立无依赖，先收错误码 155300-155304）
2. Task 2：RetrieveOptions.Filters 槽位 + WithRetrieveFilters + retriever 测试补
3. Task 3：simple_kb.go 五方法 + 懒创建 + agentic 包装 + 测试
4. Task 4：multi_kb.go 两聚合函数 + 测试
5. Task 5：doc.go ×2 + IMPLEMENTATION_PLAN.md 状态同步
6. 全量回归：CGO_ENABLED=1 + `-tags "sqlite_fts5 test"`（记忆 testing-build-tags）

## 13. 实施期修订（2026-10-30）

实施与审查过程中发现 4 处计划缺陷 + 3 处测试期望值错误，均已修正并固化为测试。

### 13.1 计划缺陷

| # | 计划原方案 | 问题 | 实际落地 |
|---|---|---|---|
| 1 | `ensureRetriever` 用"锁外快路径 + 锁内双检" | Go 内存模型下锁外读 `retriever` 与锁内写并发构成数据竞争（`-race` 实测触发） | 读路径同样持锁（锁仅覆盖字段读取与一次性构造，检索在锁外执行） |
| 2 | `knowledgeBaseConfigFromMap` 从零值构造 | 丢失 pydantic 字段默认值（index_type=hybrid / chunk_size=512 / chunk_overlap=50） | 以默认值作基线再覆盖 |
| 3 | 只按 `float64` 解析 `chunk_size` / `chunk_overlap` | JSON 数字解码为 float64，YAML 数字解码为 int，YAML 路径会静默丢值 | 新增 `asInt` 统一吃 int/int64/float64 |
| 4 | 多 KB 聚合依赖 map 迭代 + 稳定排序 | Go map 迭代无序，同分时无法复现 Python dict 插入序 | 显式记录 `order` 切片承载首见序 |

### 13.2 计划测试期望值修正

| # | 用例 | 计划期望 | 实际（按 Python 逐条推演） |
|---|---|---|---|
| 1 | `AddDocuments传数据库名` | vector_store 与 index_manager 属性不配仍构造成功 | 需构造属性匹配的桩对，否则构造期 155508 |
| 2 | `RetrieveMultiKBWithSource_RawScore语义` | `RawScore == 0.2` | `max(0.1, 0.95, 0.2) = 0.95`（kb2 无 meta → 回落 score 0.95 参与取最大） |
| 3 | `ConfigManager_YAML往返` | `UpdateConfig` 后直接 `SaveToFile` | 键双轨下 `SaveToFile` 只读 `"knowledge_base"` 键，必报 155303；改为先 `LoadFromFile` 再保存 |

### 13.3 覆盖率与回归

- knowledgebase 95.6% / utils 89.1%（均高于 85% 规范下限）
- 全量回归 307 包零 FAIL（CGO_ENABLED=1 + `-tags "sqlite_fts5 test"`），`-race` 干净
