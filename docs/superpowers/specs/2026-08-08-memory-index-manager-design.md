# 7.4+7.1 MemoryIndexManager 设计方案

## 概述

实现领域 7 的 7.4（MemoryConfig 回填）和 7.1（MemoryIndexManager），为 Agent 提供轻量编程记忆的索引管理与混合搜索能力。

## 已确认决策

| 决策 | 选择 | 理由 |
|------|------|------|
| SQLite 驱动 | `mattn/go-sqlite3`（CGO） | 9197★，Go 生态事实标准，支持 FTS5 + load_extension |
| 向量搜索 | `asg017/sqlite-vec`（vec0 虚拟表） | 7989★，Python 使用的同一库，schema 完全一致 |
| vec0.so 分发 | 预编译二进制放 `vendor/` | Release 提供全平台 .so，50-60KB，简单可靠 |
| FTS5 | `mattn/go-sqlite3` + `sqlite_fts5` build tag | 需要编译时开启 |
| EmbeddingProvider | 复用 `retrieval/embedding`，通过适配器包装 | 避免重复代码，对齐 Python 的 `create_embedding_provider` 工厂 |
| 实现顺序 | 7.4 + 7.1 合并 | 7.4 是 7.1 的前置依赖 |

## 在 Agent 会话中的流程位置

```
Agent 启动
  → 加载配置
  → 初始化 Workspace
  → 初始化 MemoryIndexManager (7.1) ←── 这里
  → 初始化 ToolContext（7.3 引用 manager）
  → 进入 ReAct 任务循环
     → Agent 调用 coding_memory_read/write/edit 工具 (7.2)
     → 工具通过 ToolContext 获取 manager
     → manager 执行 search/read_file 等操作
```

## 第一部分：SQLite 驱动统一迁移

### 当前状态

项目同时引入了两个 SQLite 驱动：
- `glebarez/sqlite`（纯 Go，基于 `modernc.org/sqlite`）— 仅测试代码使用
- `gorm.io/driver/sqlite`（CGO，基于 `mattn/go-sqlite3`）— checkpointer 使用

### 迁移内容

1. **go.mod**：移除 `glebarez/sqlite` + `modernc.org/sqlite`，保留 `mattn/go-sqlite3` + `gorm.io/driver/sqlite`
2. **测试代码**（3 个文件）：
   - `db/default_test.go`：`"github.com/glebarez/sqlite"` → `"gorm.io/driver/sqlite"`，driver 名从 `"sqlite"` → `"sqlite3"`
   - `kv/db_based_test.go`：同上
   - `db/gaussdb/dialector_test.go`：已是 `gorm.io/driver/sqlite`，无需改动
3. **Makefile**：添加 `CGO_ENABLED=1` + `-tags "sqlite_fts5"`
4. **Docker/CI**：确保安装 gcc

## 第二部分：7.4 回填内容

### config.go

| 函数 | 当前 stub | 回填内容 |
|------|----------|---------|
| `IsMemoryEnabled()` | `return false` | 读 `MEMORY_ENABLED` 环境变量，默认 true |
| `CreateMemorySettings()` | `return &MemorySettings{}` | 带默认值创建 + overrides |

### embeddings.go

| 函数 | 当前 stub | 回填内容 |
|------|----------|---------|
| `ResolveEmbeddingConfigFromEnv()` | `return nil` | 读 `EMBEDDING_MODEL_NAME/BASE_URL/API_KEY` |
| `CreateEmbeddingProvider()` | `return MockEmbeddingProvider()` | 创建 `retrieval/embedding` 真实 provider，包装成 `lite.EmbeddingProvider` |

新增 `baseEmbeddingAdapter` 结构体：将 `retrieval/embedding.BaseEmbedding` 适配为 `lite.EmbeddingProvider`。

### internal.go

| 函数 | 当前 stub | 回填内容 |
|------|----------|---------|
| `BuildFTSQuery()` | `return ""` | 正则分词 + OR 连接 |
| `BM25RankToScore()` | `return 0` | `1/(1+rank)` |
| `IsMemoryPath()` | `return false` | 判断 `.md` 后缀 |
| `ListMemoryFiles()` | `return nil` | 遍历 workspace 目录 |
| `NormalizeExtraMemoryPaths()` | `return nil` | 绝对/相对路径归一化 |

## 第三部分：7.1 MemoryIndexManager 核心实现

### 结构体

```go
type memoryIndexManager struct {
    // 基础信息
    agentID    string
    workspace  *workspace.Workspace
    nodeName   string
    memoryDir  string
    settings   *MemorySettings

    // 数据库
    db     *sql.DB
    dbPath string

    // Embedding
    provider    EmbeddingProvider
    providerKey string

    // 状态
    dirty           bool
    closed          bool
    ftsAvailable    bool
    ftsError        string
    vectorAvailable bool
    vectorError     string
    vectorDims      *int

    // 文件监听
    watcher            *fsnotify.Watcher
    watcherInitialized bool
    watchDebounce      time.Duration
    watchTimer         *time.Timer
    watchMu            sync.Mutex

    // 定时同步
    intervalCancel context.CancelFunc

    // 会话增量
    sessionDeltas map[string]*SessionDeltaState

    // 外部依赖
    embeddingConfig *embedding.EmbeddingConfig
    sysOperation    sysop.SysOperation
    llm             any  // 7.2 冲突检测用，后续 MemUpdateChecker 回填
}
```

### 数据库 Schema（与 Python 完全一致）

```sql
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, source TEXT, hash TEXT, mtime INTEGER, size INTEGER);
CREATE TABLE IF NOT EXISTS chunks (id TEXT PRIMARY KEY, path TEXT, source TEXT, start_line INTEGER, end_line INTEGER, hash TEXT, model TEXT, text TEXT, embedding BLOB, updated_at INTEGER);
CREATE TABLE IF NOT EXISTS embedding_cache (provider TEXT, model TEXT, provider_key TEXT, hash TEXT PRIMARY KEY, embedding BLOB, dims INTEGER, updated_at INTEGER);
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(id UNINDEXED, path UNINDEXED, source UNINDEXED, text, content='', contentless_delete=1);
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_vec USING vec0(embedding float[1024]);
```

### 方法对照（对齐 Python MemoryIndexManager）

| Python 方法 | Go 方法 | 说明 |
|------------|---------|------|
| `MemoryIndexManager.get(params)` | `GetMemoryIndexManager(params)` | 缓存 + 工厂 |
| `initialize()` | `Initialize(ctx)` | 打开 DB → 建 schema → 初始化 provider → 加载 vec0 → sync |
| `_resolve_db_path()` | `resolveDBPath()` | 数据库路径解析 |
| `_ensure_schema()` | `ensureSchema(ctx)` | 建 meta/files/chunks/fts5/embedding_cache 表 |
| `_initialize_provider()` | `initializeProvider(ctx)` | 创建 EmbeddingProvider |
| `_load_vector_extension()` | `loadVectorExtension()` | 加载 `vec0.so` + 创建 vec0 虚拟表 |
| `_ensure_vector_table(dims)` | `ensureVectorTable(dims)` | 创建 vec0 虚拟表（指定维度） |
| `_setup_file_watcher()` | `setupFileWatcher()` | fsnotify 监听 |
| `schedule_watch_sync()` | `scheduleWatchSync()` | debounce 同步 |
| `_ensure_interval_sync()` | `ensureIntervalSync()` | 定时同步 |
| `sync(reason, force)` | `Sync(ctx, reason, force)` | 增量/全量同步 |
| `_should_full_reindex()` | `shouldFullReindex(ctx)` | 检查是否需要全量重建 |
| `_run_reindex()` | `runReindex(ctx)` | 全量重建 |
| `_sync_memory_files()` | `syncMemoryFiles(ctx)` | 同步 .md 文件 |
| `_sync_session_files()` | `syncSessionFiles(ctx)` | 同步 session 文件 |
| `_index_file(entry, source)` | `indexFile(ctx, entry, source)` | 索引单个文件 |
| `_index_chunk(...)` | `indexChunk(ctx, ...)` | 索引单个 chunk |
| `_remove_file_from_index(path)` | `removeFileFromIndex(ctx, path)` | 删除文件索引 |
| `_get_embedding(text)` | `getEmbedding(ctx, text)` | 获取 embedding（带缓存） |
| `search(query, opts)` | `Search(ctx, query, opts)` | 混合搜索 |
| `_search_vector(vec, limit)` | `searchVector(ctx, vec, limit)` | vec0 向量搜索 |
| `_search_vector_fallback(vec, limit)` | `searchVectorFallback(ctx, vec, limit)` | 内存余弦 fallback |
| `_search_keyword(query, limit)` | `searchKeyword(ctx, query, limit)` | FTS5 关键词搜索 |
| `_merge_hybrid_results()` | `mergeHybridResults()` | 混合搜索结果合并 |
| `read_file(...)` | `ReadFile(ctx, ...)` | 读取文件内容 |
| `status()` | `Status()` | 状态报告 |
| `close()` | `Close()` | 关闭 |
| `clear_memory_manager_cache()` | `ClearMemoryManagerCache()` | 清除缓存 |

### vec0.so 加载方式

```go
func (m *memoryIndexManager) loadVectorExtension() error {
    // vendor/sqlite-vec/linux-amd64/vec0.so
    vecPath := filepath.Join("vendor", "sqlite-vec", runtime.GOOS+"-"+runtime.GOARCH, "vec0.so")

    conn, err := m.db.Conn(context.Background())
    if err != nil {
        return err
    }
    defer conn.Close()

    // 通过 mattn/go-sqlite3 的 LoadExtension 加载
    return conn.Raw(func(driverConn interface{}) error {
        dc, ok := driverConn.(*sqlite3.SQLiteConn)
        if !ok {
            return fmt.Errorf("不支持 LoadExtension 的驱动类型")
        }
        return dc.LoadExtension(vecPath, "sqlite3_vec_init")
    })
}
```

### 混合搜索流程

```
Search(ctx, query, opts)
  ├─ 检查是否需要同步 (onSearch=true + dirty)
  ├─ FTS5 关键词搜索
  │   └─ searchKeyword(ctx, query, limit)
  │       ├─ build_fts_query(query) → "token1" OR "token2"
  │       └─ SELECT rowid, rank FROM chunks_fts WHERE chunks_fts MATCH ? ORDER BY rank
  ├─ vec0 向量搜索
  │   └─ searchVector(ctx, vec, limit)
  │       ├─ embed_query(query) → query_vec
  │       ├─ SELECT rowid, vec_distance_cosine(embedding, vec_f32(?)) FROM chunks_vec
  │       └─ 失败时降级 → searchVectorFallback(ctx, vec, limit)
  │           └─ 内存余弦相似度遍历 chunks 表
  └─ mergeHybridResults(vectorResults, keywordResults, vectorWeight=0.7, textWeight=0.3)
```

### 文件监听

- 使用 `fsnotify.Watcher`（项目已有 `fsnotify` 依赖）
- debounce 2 秒（对齐 Python `watchDebounceMs=2000`）
- 监听路径：memory_dir、daily_memory、workspace root、extra_paths

### 缓存机制

- `INDEX_CACHE`：`sync.Map`，key = `agentID:nodeName:memoryDir`
- `GetMemoryIndexManager()`：幂等获取，缓存命中时检查 `closed` 状态
- `ClearMemoryManagerCache()`：清空缓存

## 第四部分：回填标记清理

| 文件 | 当前标记 | 回填后 |
|------|---------|--------|
| `manager.go` | `⤵️ 回填: 7.1` | 真实实现 |
| `config.go` | `⤵️ 回填: 7.4` | 真实实现 |
| `embeddings.go` | `⤵️ 回填: 7.4` | 真实实现 |
| `internal.go` | `⤵️ 回填: 7.4` | 真实实现 |

## 第五部分：IMPLEMENTATION_PLAN.md 更新

- 7.4：`☐` → `✅`
- 7.1：`☐` → `✅`

## 不在本次范围

- 7.2（CodingMemoryTools）— 依赖 7.1+7.3+7.5
- 7.3（ToolContext）— 依赖 7.1
- 7.5（Frontmatter）— 独立回填
- 7.8（MemUpdateChecker）— 7.2 的冲突检测依赖，`llm` 为 nil 时自动降级
