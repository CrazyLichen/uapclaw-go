# Swarm Memory 补齐设计（10.6.15 Embeddings + 依赖链）

- 日期：2026-10-18
- 状态：已确认
- 计划条目：IMPLEMENTATION_PLAN.md 10.6.13-18（Swarm Memory）
- Python 基线：`jiuwenswarm/agents/harness/common/memory/`（swarm 层，产品侧 fork）

## 一、背景与流程位置

Python 存在两条并行 embedding/记忆链路：

1. **agentcore 主链路（Go 已完成）**：DeepAdapter → MemoryRail（agentcore）→ lite.MemoryIndexManager（Workspace 对象 API，注入式 EmbeddingConfig）。Agent 工具 memory_search/get/write/edit/read_memory 走此链路。
2. **swarm 产品链路（本设计）**：memory_rpc（10.6.18）→ swarm `get_memory_manager`（workspace_dir 字符串 API，INDEX_CACHE 全局缓存）→ swarm `create_embedding_provider`（环境变量链 + config.yaml embed 段 + Mock 降级）。消费者为 TUI 通道（11.15，未实现）。

Python 侧 swarm memory 包是 lite 的**完整 fork**（manager.py 1224 行，零 openjiuwen import），不是薄封装。Go 镜像此结构：`internal/agentcore/memory/lite/`（已完成，7.1-7.5/7.34）与 `internal/swarm/agents/harness/common/memory/`（本章补齐）各自独立、各自对齐各自 Python 基线。

唯一共享点：vec0 扩展加载器（Python 两版都调第三方库 `sqlite_vec`，Go 复用 `lite.ResolveVec0Path` + `lite.LoadVec0Extension`）。

## 二、目标包结构

```
internal/swarm/agents/harness/common/memory/
├── doc.go              # 更新：补文件目录
├── types.go            # 新增（对齐 types.py 79行）
├── internal.go         # 新增（对齐 internal.py 215行，swarm 特有语义）
├── config.go           # 新增（对齐 config.py 259行）
├── embeddings.go       # 新增（对齐 embeddings.py 165行）★10.6.15
├── manager.go          # 新增（对齐 manager.py 1224行；单文件落地，不拆 impl）
├── forbidden.go        # 已有 ✅
└── dreaming/           # 已有 ✅
```

单文件 manager.go 的理由：Python 是单类单文件，Go 侧同目录同包拆分无收益且增加审查对账成本。

## 三、各文件设计

### 3.1 types.go

对齐 types.py 全部 dataclass：`MemorySearchResult`（含 Citation *string）、`MemoryProviderStatus`、`MemorySyncProgressUpdate`、`FileEntry`（MemoryFileEntry/SessionFileEntry 类型别名）、`MemoryChunk`、`FtsStatus`、`VectorStatus`。`MemorySource` 用常量 `"memory"`/`"sessions"`。

### 3.2 internal.go（swarm 特有语义，独立于 lite）

- `EstimateTokens`：len/4
- `EnsureDir`
- `ListMemoryFiles(workspaceDir, extraPaths)`：**swarm 语义**——根目录 `MEMORY.md`/`memory.md` + **递归遍历** `workspace_dir/memory/` 全部 .md + extraPaths（文件或递归目录），排序去重
- `BuildFileEntry`：sha256 前 16 位 hash + mtimeMs + size
- `ChunkMarkdown`：逐行 token 估算 + 尾部回溯 overlap
- `HashText`：sha256 hexdigest[:16]
- `ParseEmbedding`：list/dict["embedding"]/dict["data"][0]["embedding"] 三态解析（Python 有此函数，Go 补齐）
- `BuildFTSQuery`：\w+ 分词取前 10 个，`"t" OR "t"` 拼接
- `Bm25RankToScore`：rank>=0 ? 1/(1+rank) : 1/(1-rank)
- `IsMemoryPath`：**swarm 语义**——basename ∈ {memory.md, memory} 或路径任一段小写 == "memory"（与 lite 版"以 .md 结尾"语义不同）
- `NormalizeExtraMemoryPaths`、`CosineSimilarity`
- 纯函数与 lite 同名同语义的部分**包内独立实现**（Python 两包也是 fork 重复）

### 3.3 config.go

- `_load_config` 等价物：**包内私有缓存**（sync.RWMutex + map），config.New("").Load() 结果缓存；`ClearConfigCache()` 清缓存（对齐 Python `_config_cache` + `clear_config_cache`）
- `GetEmbedConfig() map[string]string`：读 embed 段（embed_api_key/embed_base_url/embed_model，None 值保留为空串语义）
- `MemorySettings` 结构体：对齐 dataclass 默认值（provider=openai_compatible, model=text-embedding-v3, fallback=mock, sources=[memory,sessions], chunking={256,32}, query={maxResults:10, minScore:0.3, hybrid:{enabled,0.7,0.3,2.0}}, store={memory.db, vector.on, fts.on}, sync={watch.on,2000,onSearch.on,onSessionStart.on,0}, cache={on,10000}）
- `CreateMemorySettings(workspaceDir, overrides)`：读 config.yaml memory 段 + embed 段合并（settings.model 先取 embed_config.model）；store 段特殊处理（只透传 path/vector/fts 三键，对齐 Python overrides["store"] 构造）
- `ResolveModeMemory(mode, config)`：token 解析（`agent.plan`/`plan`/`code` 三形态 → modes.agent.<sub> 或 modes.code）
- `IsMemoryEnabled` / `IsProactiveMemory` / `GetMemoryMode`：导出版本（供 adapter 迁移使用）

### 3.4 embeddings.go（10.6.15 核心）

- `EmbeddingProvider` 接口：`EmbedQuery(ctx, text) ([]float64, error)` / `EmbedDocuments(ctx, texts) ([][]float64, error)` / `ID() string` / `Model() string` / `Dims() int`
- `OpenAICompatibleEmbeddingProvider`：net/http 直调 `{base_url}/embeddings`，60s 超时，**无重试**（对齐 httpx 单次 POST）；Bearer 认证；`encoding_format=float`；按 index 升序解析；dims 首次成功后更新；构造时环境变量链：参数 > EMBED_KEY > EMBED_API_KEY > config embed 段；base_url 以 /embeddings 结尾裁剪；http.Client 惰性创建复用
- `MockEmbeddingProvider`：md5(text) 作种子 → 128 维 uniform(-1,1)。Go 无法复刻 Python Mersenne Twister 序列，采用与 lite Mock 相同的近似策略（哈希展开），注释说明与 Python 值不等价但确定性、维度一致
- `CreateEmbeddingProvider(provider, model, fallback)`：provider=="mock" 直接 Mock；否则解析 embed_key（env 链 + config），有 → OpenAICompatible，无且 fallback=="mock" → warn + Mock，否则 error
- 日志：包级 `const logComponent = logger.ComponentAgentServer`，对齐 Python `logger.warning("Embedding API key not found, using mock provider")` 等调用点

### 3.5 manager.go

对齐 manager.py 全部 1224 行语义：

- 常量：META_KEY/SNIPPET_MAX_CHARS/VECTOR_TABLE/FTS_TABLE/EMBEDDING_CACHE_TABLE/SESSION_DIRTY_DEBOUNCE_MS
- `IndexCache`（sync.Map）+ `GetMemoryManager(agentID, workspaceDir, settings)` 幂等获取 + `ClearMemoryManagerCache()`（导出，对齐 Python 函数）
- `MemoryIndexManager` struct：agentID/workspaceDir/settings/db/provider/providerKey/dirty/fts&vector 状态/watcher（fsnotify）/interval timer/session 增量脚手架字段（Python 声明未用，Go 同步声明注释）
- 初始化序列（**顺序不可调换**）：event_loop 等价物（Go 无）→ resolveDBPath → openDatabase（WAL + foreign_keys + MaxOpenConns(1)）→ ensureSchema（meta/files/chunks/embedding_cache/FTS5 contentless_delete=1）→ initializeProvider（swarm CreateEmbeddingProvider + providerKey 组装）→ loadVectorExtension（复用 lite vec loader，失败降级 vectorAvailable=false）→ Sync("initial") → setupFileWatcher → ensureIntervalSync
- `resolveDBPath`：abs 直接返回；workspace_name 前缀去重；非 memory/ 前缀补 memory/；join(workspaceDir, path)
- `Sync(reason, force)`：shouldFullReindex（meta 的 provider/model/chunkTokens 三比对）→ runReindex（sync memory+sessions + writeMeta 含 vectorDims）或增量（dirty → syncMemoryFiles；sources 含 sessions → syncSessionFiles）
- `syncMemoryFiles`：swarm ListMemoryFiles → 逐文件 BuildFileEntry → hash 不变跳过 → indexFile；删除消失文件（含 FTS/vector/chunks/files 四表清理）
- `syncSessionFiles`：workspace_dir/sessions 递归 .jsonl
- `indexFile/indexChunk`：chunks INSERT OR REPLACE RETURNING rowid + FTS + vector(vec_f32) 三写；chunk_id = `{path}:{start}:{end}`
- `_get_embedding` 嵌入缓存：四键 (provider, model, providerKey, hash) 查 embedding_cache，miss 时 embed_query + 写回
- `Search(query, opts)`：opts nil 兜底 defaults；onSearch+dirty 先 Sync；**minScore 陷阱**——Python 代码 `get("minScore", 0.7)` 但 dataclass 默认 0.3，实际生效 0.3，Go 按 settings 值实现并注释；hybrid 关闭时仅向量结果过滤；候选数 candidates=min(200, max(1, maxResults*candidateMultiplier))；`_search_vector`（sqlite-vec vec_distance_cosine，score=max(0,1-d/2)）失败降级 `_search_vector_fallback`（内存余弦）；`_search_keyword`（FTS5 MATCH + bm25_rank_to_score）；`_merge_hybrid_results`（加权 0.7/0.3 by id 合并）
- `_embed_query_with_timeout`：60s context timeout，失败返回空向量（has_vector=false 跳过向量搜索）
- `ReadFile`：行区间读取
- `Status`：available/provider/model/files/chunks/sourceCounts/dirty/fts/vector/cache 完整报告
- `Close`：停 timer/observer、关 db、清 INDEX_CACHE
- Search 返回 `[]map[string]any`（Python dict，下游 memory_search 追加 citation 字段需要可变性）
- Watcher：fsnotify，watchPaths = 根 MEMORY.md/memory.md + memory 目录 + extraPaths；IsMemoryPath（swarm 语义）过滤；watchDebounceMs 防抖；1s 后 watcherInitialized
- 日志对齐：Python 每个分支的 logger 调用（info/warning/error/debug）在 Go 等价位置以结构化字段补齐

## 四、回填清单（不丢失已有内容）

1. **deep_adapter.go ReloadAgentConfig**：`lite.ClearMemoryManagerCache()` → `commmem.ClearMemoryManagerCache()`（对齐 Python import 源 `jiuwenswarm...memory`），并在其前补 `commmem.ClearConfigCache()`（对齐 Python `clear_config_cache()`）
2. **deep_adapter_rails.go**：删除私有 `getMemoryMode/isMemoryEnabled/isProactiveMemory/resolveModeMemory`（1419-1540 行区域），调用点改 `commmem.GetMemoryMode/IsMemoryEnabled/IsProactiveMemory`（包内保留私有 wrapper 或直接调用）；`isBuiltinMemoryAllowed/getMemoryEngine/isExternalMemoryEnabled` 属 external_memory_config.py（10.6.16），**保留原地不动**
3. **IMPLEMENTATION_PLAN.md**：10.6.13-18 行更新状态（Embeddings✅ + manager/types/internal 补齐说明）；10.3.2 行 ClearMemoryManagerCache 注记更新指向 swarm 包

## 五、边界（本章不做）

- external_memory_config/external_memory_builder（10.6.16）
- memory_rpc（10.6.18）
- swarm tools/memory_tools（10.6.24 工具集）
- multimodal_config.go 的 getEmbedConfig 整合（避免无谓 churn）
- TUI 通道消费者（11.15）

## 六、测试策略（≥85%）

- `embeddings_test.go`：httptest 模拟 /embeddings（200 正常/index 乱序/非 200 错误/空 data/dims 更新）；环境变量链优先级（参数>EMBED_KEY>EMBED_API_KEY>config）；base_url 裁剪；Mock 确定性 + 128 维；工厂 mock 直返/降级/抛错三路
- `config_test.go`：tempdir 写 config.yaml 验证 embed/memory 段读取 + 默认值；mode token 三形态解析；缓存 + ClearConfigCache 生效性
- `internal_test.go`：ListMemoryFiles 递归 + MEMORY.md；ChunkMarkdown 边界（超长行/overlap 回溯）；FTS 查询构造；BM25 转换；IsMemoryPath swarm 语义
- `manager_test.go`：t.TempDir 真实 SQLite——schema 创建、sync 增量（hash 未变跳过）、全量 reindex 触发（换 provider/model）、Search 混合流程（mock provider）、embedding 缓存命中、文件 watcher 防抖（写文件后等 sync）、Close 清理、GetMemoryManager 缓存幂等、ClearMemoryManagerCache
- vec0 缺失环境自动走内存余弦 fallback（CI 无 .so 也能测）

## 七、实现顺序（writing-plans 阶段细化）

1. types.go → internal.go（纯函数，先可测）
2. config.go + 测试
3. embeddings.go + 测试（10.6.15 核心）
4. manager.go + 测试
5. 回填三处（adapter/计划文档）+ doc.go 更新
6. 全量 `go test -cover ./internal/swarm/agents/harness/common/memory/...` 验证 ≥85%
