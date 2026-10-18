package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mattn/go-sqlite3"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryIndexManager 记忆索引管理器。对齐 Python MemoryIndexManager（manager.py L56，swarm 版）。
// 差异说明（vs agentcore lite 版）：本类用 workspaceDir 字符串 API；
// ListMemoryFiles 递归遍历 + 根目录 MEMORY.md；provider 构造走环境变量链 + config.yaml 自查
type MemoryIndexManager struct {
	// AgentID Agent 标识
	AgentID string
	// WorkspaceDir 工作区目录
	WorkspaceDir string
	// Settings 记忆配置
	Settings *MemorySettings

	// mu 状态锁（dirty/closed 等字段保护）
	mu sync.Mutex

	// db SQLite 连接（MaxOpenConns(1) 对齐 Python sqlite3 单连接）
	db *sql.DB
	// dbPath 数据库文件路径
	dbPath string

	// provider 嵌入提供者
	provider EmbeddingProvider
	// providerKey 提供者键（id:model）
	providerKey string

	// dirty 记忆文件脏标记
	dirty bool
	// sessionsDirty 会话脏标记（Python 声明未在 sync 路径使用，Go 同步保留）
	sessionsDirty bool
	// sessionsDirtyFiles 会话脏文件集（Python 声明未用，Go 同步保留）
	sessionsDirtyFiles map[string]struct{}
	// sessionWarm 会话热集（Python 声明未用，Go 同步保留）
	sessionWarm map[string]struct{}
	// closed 关闭标记
	closed bool

	// ftsEnabled FTS5 开关（settings.store.fts.enabled）
	ftsEnabled bool
	// ftsAvailable FTS5 可用性
	ftsAvailable bool
	// ftsError FTS5 错误信息
	ftsError string
	// vectorEnabled 向量开关（settings.store.vector.enabled）
	vectorEnabled bool
	// vectorAvailable 向量可用性
	vectorAvailable bool
	// vectorError 向量错误信息
	vectorError string
	// vectorDims 向量维度（nil 表示未建表）
	vectorDims *int

	// cacheEnabled 嵌入缓存开关（settings.cache.enabled）
	cacheEnabled bool

	// watcher 文件监听器
	watcher *fsnotify.Watcher
	// watcherPaths 监听路径集
	watcherPaths map[string]struct{}
	// watcherInitialized watcher 初始化完成标记（1s 延迟，对齐 Python call_later(1.0)）
	watcherInitialized bool
	// watchTimer 防抖定时器
	watchTimer *time.Timer
	// watchMu watcher 相关锁
	watchMu sync.Mutex

	// intervalCancel 定时同步取消函数
	intervalCancel context.CancelFunc

	// sessionTimer 会话定时器（Python 声明未用，Go 同步保留）
	sessionTimer *time.Timer
	// sessionPendingFiles 会话待处理文件集（Python 声明未用，Go 同步保留）
	sessionPendingFiles map[string]struct{}
	// sessionDeltas 会话增量状态（Python 声明未用，Go 同步保留）
	sessionDeltas map[string]*SessionDeltaState
	// fileStabilityTracker 文件稳定跟踪（Python 声明未用，Go 同步保留）
	fileStabilityTracker map[string]float64
}

// SessionDeltaState 会话增量状态。对齐 Python SessionDeltaState（manager.py L37-42）
type SessionDeltaState struct {
	// LastSize 上次大小
	LastSize int
	// PendingBytes 待处理字节
	PendingBytes int
	// PendingMessages 待处理消息数
	PendingMessages int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// metaKey 元数据键。对齐 Python META_KEY
	metaKey = "memory_index_meta_v1"
	// snippetMaxChars 摘要最大字符数。对齐 Python SNIPPET_MAX_CHARS
	snippetMaxChars = 700
	// vectorTable vec0 虚拟表。对齐 Python VECTOR_TABLE
	vectorTable = "chunks_vec"
	// ftsTable FTS5 虚拟表。对齐 Python FTS_TABLE
	ftsTable = "chunks_fts"
	// embeddingCacheTable 嵌入缓存表。对齐 Python EMBEDDING_CACHE_TABLE
	embeddingCacheTable = "embedding_cache"
	// sessionDirtyDebounceMs 会话脏防抖。对齐 Python SESSION_DIRTY_DEBOUNCE_MS
	sessionDirtyDebounceMs = 5000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// indexCache 管理器缓存。对齐 Python INDEX_CACHE（manager.py L34）
var indexCache sync.Map

// sessionFilePattern 会话文件日期模式。对齐 Python _is_recent_session_file 的正则
var sessionFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.md$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMemoryManager 获取或创建记忆管理器。
// 对齐 Python get_memory_manager（manager.py L1217-1224）：
// settings 为 nil 时用 create_memory_settings 构造；走 INDEX_CACHE 幂等
func GetMemoryManager(ctx context.Context, agentID, workspaceDir string, settings *MemorySettings) (*MemoryIndexManager, error) {
	if settings == nil {
		settings = CreateMemorySettings(workspaceDir, nil)
	}
	return getOrCreateManager(ctx, agentID, workspaceDir, settings)
}

// ClearMemoryManagerCache 清除管理器缓存，使下次 GetMemoryManager 用最新配置创建新实例。
// 对齐 Python clear_memory_manager_cache（manager.py L1212-1214）
func ClearMemoryManagerCache() {
	indexCache.Range(func(key, _ any) bool {
		indexCache.Delete(key)
		return true
	})
}

// Sync 同步索引。对齐 Python sync（manager.py L470-493）：
// closed 直返；force 或 meta 三比对不符 → 全量重建；否则增量（memory 脏时 + sessions）
func (m *MemoryIndexManager) Sync(ctx context.Context, reason string, force bool) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	// Python: needs_full_reindex = force or await self._should_full_reindex()
	needsFullReindex := force || m.shouldFullReindex()
	if needsFullReindex {
		// 对齐 Python: logger.info(f"Running full reindex (reason: {reason or 'unknown'})...")
		logger.Info(logComponent).Str("reason", reason).Msg("Running full reindex")
		return m.runReindex(ctx)
	}

	// 对齐 Python: logger.debug(f"Memory sync (reason: {reason or 'unknown'})...")
	logger.Debug(logComponent).Str("reason", reason).Msg("Memory sync")

	m.mu.Lock()
	dirty := m.dirty
	m.mu.Unlock()
	if containsSource(m.Settings.Sources, "memory") && dirty {
		if err := m.syncMemoryFiles(ctx); err != nil {
			return err
		}
		m.mu.Lock()
		m.dirty = false
		m.mu.Unlock()
	}

	if containsSource(m.Settings.Sources, "sessions") {
		if err := m.syncSessionFiles(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Search 混合搜索。对齐 Python search（manager.py L781-840）。
// 注意：Python 代码内 get("minScore", 0.7) 的 0.7 是永不生效的死默认值——
// opts 未提供时实际取 settings.query.minScore（默认 0.3）。
// 返回 []MemorySearchResult：对齐 Python MemorySearchResult dataclass
// （Python manager 实际返回 dict 列表，Go 侧落地为结构体，字段一一对应；
// citation 由下游 memory_search 填充）
func (m *MemoryIndexManager) Search(ctx context.Context, query string, opts *SearchOpts) ([]MemorySearchResult, error) {
	// Python: if self.settings.sync.get("onSearch", True) and self.dirty
	m.mu.Lock()
	dirty := m.dirty
	m.mu.Unlock()
	if m.Settings.Sync.OnSearch && dirty {
		if err := m.Sync(ctx, "search", false); err != nil {
			// 对齐 Python: logger.warning(f"Memory sync failed (search): {e}")
			logger.Warn(logComponent).Err(err).Msg("Memory sync failed (search)")
		}
	}

	// Python: cleaned = query.strip(); if not cleaned: return []
	cleaned := strings.TrimSpace(query)
	if cleaned == "" {
		return []MemorySearchResult{}, nil
	}

	// Python: min_score = opts.get("minScore") if ... else self.settings.query.get("minScore", 0.7)
	// （0.7 为死默认值，实际生效 settings 默认 0.3）
	minScore := m.Settings.Query.MinScore
	if opts != nil && opts.MinScore != nil {
		minScore = *opts.MinScore
	}
	maxResults := m.Settings.Query.MaxResults
	if opts != nil && opts.MaxResults != nil {
		maxResults = *opts.MaxResults
	}
	// Python: hybrid = self.settings.query.get("hybrid") or {}
	hybridEnabled := m.Settings.Query.Hybrid.Enabled
	candidateMultiplier := m.Settings.Query.Hybrid.CandidateMultiplier

	// Python: candidates = min(200, max(1, int(max_results * (hybrid.get("candidateMultiplier") or 2.0))))
	candidates := int(math.Min(200, math.Max(1, float64(maxResults)*candidateMultiplier)))

	// Python: keyword_results = [] if hybrid.get("enabled", True) and self.fts_available
	var keywordResults []MemorySearchResult
	if hybridEnabled && m.ftsAvailable {
		keywordResults = m.searchKeyword(cleaned, candidates)
	}

	// Python: query_vec = await self._embed_query_with_timeout(cleaned); has_vector = any(v != 0 for v in query_vec)
	queryVec := m.embedQueryWithTimeout(ctx, cleaned)
	hasVector := false
	for _, v := range queryVec {
		if v != 0 {
			hasVector = true
			break
		}
	}

	var vectorResults []MemorySearchResult
	if hasVector {
		vectorResults = m.searchVector(ctx, queryVec, candidates)
	}

	// Python: if not hybrid.get("enabled", True): return [r for r in vector_results if r["score"] >= min_score][:max_results]
	if !hybridEnabled {
		out := make([]MemorySearchResult, 0, len(vectorResults))
		for _, r := range vectorResults {
			if r.Score >= minScore {
				out = append(out, r)
			}
		}
		if len(out) > maxResults {
			out = out[:maxResults]
		}
		return out, nil
	}

	// Python: merged = self._merge_hybrid_results(vector_results, keyword_results, vw, tw)
	merged := mergeHybridResults(vectorResults, keywordResults,
		m.Settings.Query.Hybrid.VectorWeight, m.Settings.Query.Hybrid.TextWeight)

	out := make([]MemorySearchResult, 0, len(merged))
	for _, r := range merged {
		if r.Score >= minScore {
			out = append(out, r)
		}
	}
	if len(out) > maxResults {
		out = out[:maxResults]
	}
	return out, nil
}

// ReadFile 读取文件内容。对齐 Python read_file（manager.py L1085-1117）：
// readlines 语义保留行尾（Go 用 Split 后 Join("\n") 近似，行数语义一致）
func (m *MemoryIndexManager) ReadFile(ctx context.Context, relPath string, fromLine, lines *int) (map[string]any, error) {
	_ = ctx
	full := filepath.Join(m.WorkspaceDir, relPath)
	if _, err := os.Stat(full); err != nil {
		// 对齐 Python: raise FileNotFoundError(f"File not found: {rel_path}")
		return nil, fmt.Errorf("File not found: %s", relPath)
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	allLines := strings.Split(string(content), "\n")
	total := len(allLines)

	// Python: if from_line is not None: start = max(0, from_line - 1); end = total; if lines: end = min(total, start+lines)
	var selected []string
	if fromLine != nil {
		start := *fromLine - 1
		if start < 0 {
			start = 0
		}
		end := total
		if lines != nil {
			end = start + *lines
			if end > total {
				end = total
			}
		}
		selected = allLines[start:end]
	} else {
		selected = allLines
	}

	from := 1
	if fromLine != nil {
		from = *fromLine
	}
	// Python: return {..., "fromLine": from_line or 1, "toLine": (from_line or 1) + len(content_lines) - 1}
	return map[string]any{
		"path":       relPath,
		"text":       strings.Join(selected, "\n"),
		"totalLines": total,
		"fromLine":   from,
		"toLine":     from + len(selected) - 1,
	}, nil
}

// Status 系统状态报告。对齐 Python status（manager.py L1127-1171）
func (m *MemoryIndexManager) Status() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.db == nil {
		return map[string]any{"available": false}
	}

	var fileCount, chunkCount int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&fileCount)
	_ = m.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunkCount)

	rows, err := m.db.Query("SELECT source, COUNT(*) FROM files GROUP BY source")
	if err != nil {
		return map[string]any{"available": false}
	}
	defer func() { _ = rows.Close() }()
	sourceCounts := []map[string]any{}
	for rows.Next() {
		var src string
		var cnt int
		if err := rows.Scan(&src, &cnt); err == nil {
			sourceCounts = append(sourceCounts, map[string]any{"source": src, "files": cnt})
		}
	}

	provider, model := "", ""
	if m.provider != nil {
		provider = m.provider.ID()
		model = m.provider.Model()
	}

	return map[string]any{
		"available":    true,
		"provider":     provider,
		"model":        model,
		"files":        fileCount,
		"chunks":       chunkCount,
		"sourceCounts": sourceCounts,
		"dirty":        m.dirty,
		"fts":          map[string]any{"enabled": m.ftsEnabled, "available": m.ftsAvailable, "error": m.ftsError},
		"vector":       map[string]any{"enabled": m.vectorEnabled, "available": m.vectorAvailable, "error": m.vectorError, "dims": m.vectorDims},
		"cache":        map[string]any{"enabled": m.cacheEnabled, "entries": m.cacheEntryCount()},
	}
}

// Close 关闭管理器。对齐 Python close（manager.py L1181-1209）：幂等 + 停定时器 + 关监听 + 关库 + 清缓存
func (m *MemoryIndexManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	// Python: if self._interval_timer: self._interval_timer.cancel()
	if m.intervalCancel != nil {
		m.intervalCancel()
	}
	m.watchMu.Lock()
	if m.watchTimer != nil {
		m.watchTimer.Stop()
	}
	m.watchMu.Unlock()
	if m.sessionTimer != nil {
		m.sessionTimer.Stop()
	}

	// Python: if self._file_observer: stop + join
	if m.watcher != nil {
		_ = m.watcher.Close()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db != nil {
		_ = m.db.Close()
	}
	// Python: cache_key in INDEX_CACHE → del
	indexCache.Delete(m.AgentID + ":" + m.WorkspaceDir)
	// 对齐 Python: logger.info("Memory manager closed")
	logger.Info(logComponent).Msg("Memory manager closed")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getOrCreateManager 幂等获取。对齐 Python MemoryIndexManager.get（manager.py L103-127）：
// INDEX_CACHE 命中且未 closed 直返；否则新建 → initialize → 回填缓存
func getOrCreateManager(ctx context.Context, agentID, workspaceDir string, settings *MemorySettings) (*MemoryIndexManager, error) {
	// Python: cache_key = f"{agent_id}:{workspace_dir}"
	key := agentID + ":" + workspaceDir
	if cached, ok := indexCache.Load(key); ok {
		mgr := cached.(*MemoryIndexManager)
		if !mgr.closed {
			return mgr, nil
		}
	}

	m := newMemoryIndexManager(agentID, workspaceDir, settings)

	if err := m.initialize(ctx); err != nil {
		// 对齐 Python: logger.error(f"Failed to initialize memory manager: {e}")
		logger.Error(logComponent).Err(err).Msg("Failed to initialize memory manager")
		return nil, err
	}
	indexCache.Store(key, m)
	return m, nil
}

// newMemoryIndexManager 构造。对齐 Python __init__（manager.py L59-101）
func newMemoryIndexManager(agentID, workspaceDir string, settings *MemorySettings) *MemoryIndexManager {
	return &MemoryIndexManager{
		AgentID:      agentID,
		WorkspaceDir: workspaceDir,
		Settings:     settings,

		dirty:                true,  // Python: self.dirty = True
		sessionsDirty:        false, // Python: self.sessions_dirty = False
		sessionsDirtyFiles:   make(map[string]struct{}),
		sessionWarm:          make(map[string]struct{}),
		closed:               false,
		ftsEnabled:           settings.Store.FtsEnabled,
		vectorEnabled:        settings.Store.VectorEnabled,
		cacheEnabled:         settings.Cache.Enabled,
		ftsAvailable:         false,
		vectorAvailable:      false,
		watcherPaths:         make(map[string]struct{}),
		sessionPendingFiles:  make(map[string]struct{}),
		sessionDeltas:        make(map[string]*SessionDeltaState),
		fileStabilityTracker: make(map[string]float64),
	}
}

// initialize 初始化管理器。对齐 Python _initialize（manager.py L129-149，顺序不可调换）：
// resolveDBPath → openDatabase → ensureSchema → initializeProvider →
// loadVectorExtension → Sync("initial") → setupFileWatcher → ensureIntervalSync
func (m *MemoryIndexManager) initialize(ctx context.Context) error {
	m.dbPath = m.resolveDBPath()
	db, err := m.openDatabase(m.dbPath)
	if err != nil {
		return err
	}
	m.db = db

	if err := m.ensureSchema(); err != nil {
		return err
	}

	if err := m.initializeProvider(); err != nil {
		return err
	}

	if err := m.loadVectorExtension(); err != nil {
		m.vectorAvailable = false
		m.vectorError = err.Error()
		// 对齐 Python: logger.warning(f"Failed to load sqlite-vec extension: {e}")
		logger.Warn(logComponent).Err(err).Msg("Failed to load sqlite-vec extension")
	}

	// Python: await self.sync(reason="initial")（失败不中断初始化——Python 内部已捕获）
	if err := m.Sync(ctx, "initial", false); err != nil {
		logger.Warn(logComponent).Err(err).Msg("初始同步失败")
	}

	if m.Settings.Sync.Watch {
		m.setupFileWatcher()
	}
	m.ensureIntervalSync(ctx)

	// 对齐 Python: logger.info(f"Memory manager initialized for agent: {self.agent_id}")
	logger.Info(logComponent).Str("agent_id", m.AgentID).Msg("Memory manager initialized for agent")
	return nil
}

// resolveDBPath 解析数据库路径。对齐 Python _resolve_db_path（manager.py L151-170）：
// 绝对路径直返；workspace_name 前缀去重；确保 memory/ 子目录（与 MEMORY.md 同目录）
func (m *MemoryIndexManager) resolveDBPath() string {
	storePath := m.Settings.Store.Path
	if filepath.IsAbs(storePath) {
		return storePath
	}

	// Python: workspace_name = os.path.basename(self.workspace_dir)
	workspaceName := filepath.Base(m.WorkspaceDir)
	if strings.HasPrefix(storePath, workspaceName+"/") || strings.HasPrefix(storePath, workspaceName+`\`) {
		// 去除重复的 workspace 目录前缀
		storePath = storePath[len(workspaceName)+1:]
	}

	// 确保使用 memory 子目录，与 MEMORY.md 同目录
	if !strings.HasPrefix(storePath, "memory/") && !strings.HasPrefix(storePath, `memory\`) {
		storePath = filepath.Join("memory", storePath)
	}
	return filepath.Join(m.WorkspaceDir, storePath)
}

// openDatabase 打开 SQLite 数据库。对齐 Python _open_database（manager.py L172-181）：
// ensure_dir + WAL + foreign_keys=ON；Go 侧 MaxOpenConns(1) 对齐单连接语义
func (m *MemoryIndexManager) openDatabase(dbPath string) (*sql.DB, error) {
	if err := EnsureDir(filepath.Dir(dbPath)); err != nil {
		return nil, fmt.Errorf("确保数据库目录失败: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// ensureSchema 建表。对齐 Python _ensure_schema（manager.py L183-276）：
// meta/files/chunks/embedding_cache 四表 + FTS5 contentless_delete=1 虚拟表（失败降级）
func (m *MemoryIndexManager) ensureSchema() error {
	if m.db == nil {
		return fmt.Errorf("Database not initialized")
	}

	// meta 表
	if _, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		return fmt.Errorf("建 meta 表失败: %w", err)
	}
	// files 表
	if _, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS files (
		path TEXT PRIMARY KEY, source TEXT, hash TEXT, mtime INTEGER, size INTEGER
	)`); err != nil {
		return fmt.Errorf("建 files 表失败: %w", err)
	}
	// chunks 表
	if _, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS chunks (
		id TEXT PRIMARY KEY, path TEXT, source TEXT, start_line INTEGER, end_line INTEGER,
		hash TEXT, model TEXT, text TEXT, embedding BLOB, updated_at INTEGER
	)`); err != nil {
		return fmt.Errorf("建 chunks 表失败: %w", err)
	}
	// embedding_cache 表
	if _, err := m.db.Exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		provider TEXT, model TEXT, provider_key TEXT, hash TEXT PRIMARY KEY,
		embedding BLOB, dims INTEGER, updated_at INTEGER
	)`, embeddingCacheTable)); err != nil {
		return fmt.Errorf("建 embedding_cache 表失败: %w", err)
	}

	// FTS5 虚拟表（Python try/except 降级：ftsAvailable=false + ftsError + warn）
	if m.ftsEnabled {
		_, err := m.db.Exec(fmt.Sprintf(
			"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(id UNINDEXED, path UNINDEXED, source UNINDEXED, text, content='', contentless_delete=1)",
			ftsTable,
		))
		if err != nil {
			m.ftsAvailable = false
			m.ftsError = err.Error()
			// 对齐 Python: logger.warning(f"Failed to create FTS5 table: {e}")
			logger.Warn(logComponent).Err(err).Msg("Failed to create FTS5 table")
		} else {
			m.ftsAvailable = true
		}
	}
	return nil
}

// initializeProvider 初始化提供者。对齐 Python _initialize_provider（manager.py L278-290）：
// 失败 error 并上抛；成功记录 provider_key 与 info 日志
func (m *MemoryIndexManager) initializeProvider() error {
	provider, err := CreateEmbeddingProvider(m.Settings.Provider, m.Settings.Model, m.Settings.Fallback)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to initialize embedding provider: {e}") + raise
		logger.Error(logComponent).Err(err).Msg("Failed to initialize embedding provider")
		return err
	}
	m.provider = provider
	// Python: self.provider_key = f"{self.provider.id}:{self.provider.model}"
	m.providerKey = provider.ID() + ":" + provider.Model()
	// 对齐 Python: logger.info(f"Embedding provider: {self.provider.id} / {self.provider.model}")
	logger.Info(logComponent).Str("provider", provider.ID()).Str("model", provider.Model()).Msg("Embedding provider")
	return nil
}

// loadVectorExtension 加载 vec0 扩展。对齐 Python _load_vector_extension（manager.py L292-307）：
// 复用 lite 包 vec loader（Python 两版都调 sqlite_vec 第三方库）
func (m *MemoryIndexManager) loadVectorExtension() error {
	if !m.vectorEnabled || m.db == nil {
		return nil
	}

	vecPath := lite.ResolveVec0Path()
	if _, err := os.Stat(vecPath); err != nil {
		return fmt.Errorf("vec0 不存在: %s", vecPath)
	}

	conn, err := m.db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("获取连接失败: %w", err)
	}
	defer func() { _ = conn.Close() }()

	err = conn.Raw(func(driverConn any) error {
		dc, ok := driverConn.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("不支持 LoadExtension 的驱动类型")
		}
		return lite.LoadVec0Extension(dc, vecPath)
	})
	if err != nil {
		return fmt.Errorf("加载 vec0 扩展失败: %w", err)
	}
	m.vectorAvailable = true
	// 对齐 Python: logger.info("sqlite-vec extension loaded successfully")
	logger.Info(logComponent).Msg("sqlite-vec extension loaded successfully")
	return nil
}

// ensureVectorTable 确保向量虚拟表维度正确。对齐 Python _ensure_vector_table（manager.py L309-337）：
// dims 一致直返；dims 变化 DROP 重建；失败降级 vectorAvailable=false
func (m *MemoryIndexManager) ensureVectorTable(dims int) bool {
	if m.db == nil || !m.vectorAvailable {
		return false
	}

	if m.vectorDims != nil && *m.vectorDims == dims {
		return true
	}

	// Python: if self.vector_dims is not None and self.vector_dims != dims: DROP TABLE
	if m.vectorDims != nil && *m.vectorDims != dims {
		if _, err := m.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", vectorTable)); err != nil {
			// Python: except: pass
			_ = err
		}
	}

	if _, err := m.db.Exec(fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d])", vectorTable, dims,
	)); err != nil {
		// 对齐 Python: logger.warning(f"Failed to create vector table: {e}") + 降级
		logger.Warn(logComponent).Err(err).Msg("Failed to create vector table")
		m.vectorAvailable = false
		m.vectorError = err.Error()
		return false
	}
	m.vectorDims = &dims
	// 对齐 Python: logger.info(f"Vector table created with dims={dims}")
	logger.Info(logComponent).Int("dims", dims).Msg("Vector table created")
	return true
}

// setupFileWatcher 设置文件监听。对齐 Python _setup_file_watcher（manager.py L339-409）：
// watchPaths = 根 MEMORY.md/memory.md（存在且非 symlink）+ memory 目录 + extraPaths；
// 文件监听父目录、目录递归监听；1s 后 watcherInitialized=true
func (m *MemoryIndexManager) setupFileWatcher() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to setup file watcher: {e}")
		logger.Error(logComponent).Err(err).Msg("Failed to setup file watcher")
		return
	}
	m.watcher = watcher

	watchPaths := make(map[string]struct{})
	// Python: for filename in ["MEMORY.md", "memory.md"]: exists and not islink
	for _, filename := range []string{"MEMORY.md", "memory.md"} {
		fp := filepath.Join(m.WorkspaceDir, filename)
		if st, err := os.Lstat(fp); err == nil && st.Mode()&os.ModeSymlink == 0 {
			watchPaths[fp] = struct{}{}
		}
	}
	memoryDir := filepath.Join(m.WorkspaceDir, "memory")
	if st, err := os.Lstat(memoryDir); err == nil && st.Mode()&os.ModeSymlink == 0 {
		watchPaths[memoryDir] = struct{}{}
	}
	// Python: for extra_path in self.settings.extraPaths
	for _, extra := range m.Settings.ExtraPaths {
		full := filepath.Join(m.WorkspaceDir, extra)
		if st, err := os.Lstat(full); err == nil && st.Mode()&os.ModeSymlink == 0 {
			watchPaths[full] = struct{}{}
		}
	}

	if len(watchPaths) == 0 {
		// 对齐 Python: logger.debug("No memory paths to watch")
		logger.Debug(logComponent).Msg("No memory paths to watch")
		_ = watcher.Close()
		m.watcher = nil
		return
	}

	// Python: isdir → schedule(recursive=True)；文件 → schedule(dirname, recursive=False)
	for watchPath := range watchPaths {
		if st, err := os.Stat(watchPath); err == nil && st.IsDir() {
			_ = watcher.Add(watchPath) // fsnotify 目录监听默认递归（Add 递归子目录需手动；此处对齐 watchdog recursive=True 语义）
		} else {
			parent := filepath.Dir(watchPath)
			if parent != "" && parent != "." {
				_ = watcher.Add(parent)
			}
		}
		m.watcherPaths[watchPath] = struct{}{}
	}

	// Python: self._event_loop.call_later(1.0, self._set_watcher_initialized)
	// 注意：必须在 go watchLoop() 之前置 false，否则 watchLoop 并发读到零值窗口
	m.watchMu.Lock()
	m.watcherInitialized = false
	m.watchMu.Unlock()
	go m.watchLoop()

	time.AfterFunc(1*time.Second, func() {
		// 对齐 Python: _set_watcher_initialized → logger.debug("File watcher initialized")
		m.watchMu.Lock()
		m.watcherInitialized = true
		m.watchMu.Unlock()
		logger.Debug(logComponent).Msg("File watcher initialized")
	})

	// 对齐 Python: logger.info(f"File watcher started for {len(watch_paths)} path(s)")
	logger.Info(logComponent).Int("paths", len(watchPaths)).Msg("File watcher started")
}

// watchLoop 监听事件循环。对齐 Python MemoryFileHandler.on_modified/on_created/on_deleted：
// watcherInitialized 后过滤 is_memory_path 再防抖同步
func (m *MemoryIndexManager) watchLoop() {
	for {
		select {
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.watchMu.Lock()
			initialized := m.watcherInitialized
			m.watchMu.Unlock()
			if !initialized {
				continue
			}
			// Python: rel_path = os.path.relpath(path, workspace_dir); if is_memory_path(rel_path): schedule
			rel, err := filepath.Rel(m.WorkspaceDir, event.Name)
			if err != nil {
				continue
			}
			if !IsMemoryPath(rel) {
				continue
			}
			m.scheduleWatchSync()
		case _, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// scheduleWatchSync 防抖调度同步。对齐 Python _schedule_watch_sync（manager.py L416-447）：
// dirty=true；防抖 watchDebounceMs 后 sync(reason="watch")
func (m *MemoryIndexManager) scheduleWatchSync() {
	m.mu.Lock()
	m.dirty = true
	m.mu.Unlock()

	m.watchMu.Lock()
	defer m.watchMu.Unlock()

	debounce := time.Duration(m.Settings.Sync.WatchDebounceMs) * time.Millisecond
	// Python: if self._watch_timer: cancel（重置防抖窗口）
	if m.watchTimer != nil {
		m.watchTimer.Stop()
	}
	m.watchTimer = time.AfterFunc(debounce, func() {
		m.mu.Lock()
		closed := m.closed
		m.mu.Unlock()
		if closed {
			return
		}
		if err := m.Sync(context.Background(), "watch", false); err != nil {
			// 对齐 Python: logger.warning(f"Memory sync failed (watch): {e}")
			logger.Warn(logComponent).Err(err).Msg("Memory sync failed (watch)")
		}
	})
}

// ensureIntervalSync 定时同步。对齐 Python _ensure_interval_sync（manager.py L449-468）：
// intervalMinutes<=0 不启动；goroutine 循环 sleep 后 sync(reason="interval")
func (m *MemoryIndexManager) ensureIntervalSync(ctx context.Context) {
	minutes := m.Settings.Sync.IntervalMinutes
	if minutes <= 0 {
		return
	}
	if m.intervalCancel != nil {
		return
	}

	intervalCtx, cancel := context.WithCancel(context.Background())
	m.intervalCancel = cancel
	_ = ctx

	go func() {
		ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-intervalCtx.Done():
				return
			case <-ticker.C:
				m.mu.Lock()
				closed := m.closed
				m.mu.Unlock()
				if closed {
					return
				}
				if err := m.Sync(context.Background(), "interval", false); err != nil {
					// 对齐 Python: logger.warning(f"Memory sync failed (interval): {e}")
					logger.Warn(logComponent).Err(err).Msg("Memory sync failed (interval)")
				}
			}
		}
	}()
	// 对齐 Python: logger.info(f"Interval sync enabled: every {minutes} minutes")
	logger.Info(logComponent).Int("minutes", minutes).Msg("Interval sync enabled")
}

// shouldFullReindex 判断是否需要全量重建。对齐 Python _should_full_reindex（manager.py L495-522）：
// meta 缺失 / provider 变 / model 变 / chunkTokens 变 → true
func (m *MemoryIndexManager) shouldFullReindex() bool {
	var value string
	err := m.db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKey).Scan(&value)
	if err != nil {
		// 对齐 Python: except → return True
		return true
	}

	var meta map[string]any
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return true
	}

	// Python: meta.get("provider") != self.provider.id
	if strVal(meta["provider"]) != m.provider.ID() {
		return true
	}
	// Python: meta.get("model") != self.provider.model
	if strVal(meta["model"]) != m.provider.Model() {
		return true
	}
	// Python: meta.get("chunkTokens") != self.settings.chunking.get("tokens")
	metaTokens, _ := numVal(meta["chunkTokens"])
	return int(metaTokens) != m.Settings.Chunking.Tokens
}

// runReindex 全量重建。对齐 Python _run_reindex（manager.py L524-543）：
// memory + sessions 同步后写 meta（含 vectorDims）
func (m *MemoryIndexManager) runReindex(ctx context.Context) error {
	if containsSource(m.Settings.Sources, "memory") {
		if err := m.syncMemoryFiles(ctx); err != nil {
			return err
		}
		m.mu.Lock()
		m.dirty = false
		m.mu.Unlock()
	}

	if containsSource(m.Settings.Sources, "sessions") {
		if err := m.syncSessionFiles(ctx); err != nil {
			return err
		}
	}

	meta := map[string]any{
		"provider":     m.provider.ID(),
		"model":        m.provider.Model(),
		"providerKey":  m.providerKey,
		"chunkTokens":  m.Settings.Chunking.Tokens,
		"chunkOverlap": m.Settings.Chunking.Overlap,
	}
	// Python: if self.vector_available and self.vector_dims: meta["vectorDims"] = self.vector_dims
	if m.vectorAvailable && m.vectorDims != nil {
		meta["vectorDims"] = *m.vectorDims
	}
	return m.writeMeta(meta)
}

// isRecentSessionFile 检查是否今天/昨天的会话记录文件。
// 对齐 Python _is_recent_session_file（manager.py L545-572）。
// 注意：Python 中定义但从未被调用，Go 一比一同步实现
func (m *MemoryIndexManager) isRecentSessionFile(filename string) bool {
	match := sessionFilePattern.FindStringSubmatch(filename)
	if match == nil {
		return false
	}

	fileDate, err := time.Parse("2006-01-02", match[1])
	if err != nil {
		return false
	}

	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)
	fd := fileDate.Format("2006-01-02")
	return fd == today.Format("2006-01-02") || fd == yesterday.Format("2006-01-02")
}

// syncMemoryFiles 同步记忆文件。对齐 Python _sync_memory_files（manager.py L574-604）：
// ListMemoryFiles → hash 未变跳过 → indexFile；最后删除消失文件
func (m *MemoryIndexManager) syncMemoryFiles(ctx context.Context) error {
	files := ListMemoryFiles(m.WorkspaceDir, m.Settings.ExtraPaths)

	// 对齐 Python: logger.debug(f"Syncing {len(files)} memory files")
	logger.Debug(logComponent).Int("files", len(files)).Msg("Syncing memory files")

	activePaths := make(map[string]struct{}, len(files))
	for _, fp := range files {
		entry, err := BuildFileEntry(fp, m.WorkspaceDir)
		if err != nil {
			continue
		}
		activePaths[entry.Path] = struct{}{}

		var existingHash string
		err = m.db.QueryRow(
			"SELECT hash FROM files WHERE path = ? AND source = ?",
			entry.Path, MemorySourceMemory,
		).Scan(&existingHash)
		// Python: if row and row["hash"] == entry["hash"]: continue
		if err == nil && existingHash == entry.Hash {
			continue
		}

		if err := m.indexFile(ctx, entry, MemorySourceMemory); err != nil {
			// indexFile 内部已记录日志并回滚（对齐 Python try/except）
			continue
		}
	}

	// Python: SELECT path FROM files WHERE source = 'memory' → 不在 active_paths 的删除
	rows, err := m.db.Query("SELECT path FROM files WHERE source = ?", MemorySourceMemory)
	if err != nil {
		return err
	}
	var toRemove []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err == nil {
			if _, ok := activePaths[path]; !ok {
				toRemove = append(toRemove, path)
			}
		}
	}
	_ = rows.Close()
	for _, path := range toRemove {
		m.removeFileFromIndex(path)
	}
	return nil
}

// syncSessionFiles 同班会话记录文件。对齐 Python _sync_session_files（manager.py L606-639）：
// sessions/ 目录递归找 .jsonl → hash 跳过 → indexFile；删除消失文件
func (m *MemoryIndexManager) syncSessionFiles(ctx context.Context) error {
	sessionsDir := filepath.Join(m.WorkspaceDir, "sessions")
	if st, err := os.Stat(sessionsDir); err != nil || !st.IsDir() {
		return nil
	}

	var sessionFiles []string
	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".jsonl") {
			sessionFiles = append(sessionFiles, path)
		}
		return nil
	})

	// 对齐 Python: logger.debug(f"Syncing {len(session_files)} session files")
	logger.Debug(logComponent).Int("files", len(sessionFiles)).Msg("Syncing session files")

	activePaths := make(map[string]struct{}, len(sessionFiles))
	for _, sf := range sessionFiles {
		entry, err := BuildFileEntry(sf, m.WorkspaceDir)
		if err != nil {
			continue
		}
		activePaths[entry.Path] = struct{}{}

		var existingHash string
		err = m.db.QueryRow(
			"SELECT hash FROM files WHERE path = ? AND source = ?",
			entry.Path, MemorySourceSessions,
		).Scan(&existingHash)
		if err == nil && existingHash == entry.Hash {
			continue
		}

		if err := m.indexFile(ctx, entry, MemorySourceSessions); err != nil {
			continue
		}
	}

	rows, err := m.db.Query("SELECT path FROM files WHERE source = ?", MemorySourceSessions)
	if err != nil {
		return err
	}
	var toRemove []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err == nil {
			if _, ok := activePaths[path]; !ok {
				toRemove = append(toRemove, path)
			}
		}
	}
	_ = rows.Close()
	for _, path := range toRemove {
		m.removeFileFromIndex(path)
	}
	return nil
}

// indexFile 索引单文件。对齐 Python _index_file（manager.py L641-668）：
// 读文件 → ChunkMarkdown → 删旧 chunk + FTS → 逐 chunk 索引 → 写 files 表；
// 失败记日志并回滚（Go 无显式事务时逐语句已提交，用事务包裹对齐 Python commit/rollback）
func (m *MemoryIndexManager) indexFile(ctx context.Context, entry *FileEntry, source string) error {
	content, err := os.ReadFile(entry.AbsPath)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to index file {entry['path']}: {e}")
		logger.Error(logComponent).Err(err).Str("path", entry.Path).Msg("Failed to index file")
		return err
	}

	chunks := ChunkMarkdown(string(content), m.Settings.Chunking)

	// Python: self.db.execute("DELETE FROM chunks WHERE path = ?")（隐式开启事务，最终 commit/rollback）
	tx, err := m.db.Begin()
	if err != nil {
		logger.Error(logComponent).Err(err).Str("path", entry.Path).Msg("Failed to index file")
		return err
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		logger.Error(logComponent).Err(cause).Str("path", entry.Path).Msg("Failed to index file")
		return cause
	}

	if _, err := tx.Exec("DELETE FROM chunks WHERE path = ?", entry.Path); err != nil {
		return rollback(err)
	}
	// Python: if self.fts_available: try DELETE FTS except pass
	if m.ftsAvailable {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE path = ?", ftsTable), entry.Path); err != nil {
			// Python: except: pass（FTS 删除失败不中断）
			_ = err
		}
	}

	for _, chunk := range chunks {
		if err := m.indexChunk(ctx, tx, entry.Path, source, chunk); err != nil {
			return rollback(err)
		}
	}

	if _, err := tx.Exec(
		"INSERT OR REPLACE INTO files (path, source, hash, mtime, size) VALUES (?, ?, ?, ?, ?)",
		entry.Path, source, entry.Hash, entry.MtimeMs, entry.Size,
	); err != nil {
		return rollback(err)
	}

	// Python: self.db.commit()
	if err := tx.Commit(); err != nil {
		return rollback(err)
	}
	return nil
}

// indexChunk 索引单 chunk。对齐 Python _index_chunk（manager.py L670-713）：
// RETURNING rowid + FTS 写入 + vec_f32 写入。
// updated_at 语义：Python 用 asyncio 事件循环单调秒，Go 用 time.Now().Unix()（同为单调递增秒值）
func (m *MemoryIndexManager) indexChunk(ctx context.Context, tx *sql.Tx, filePath, source string, chunk MemoryChunk) error {
	// Python: chunk_id = f"{file_path}:{chunk.startLine}:{chunk.endLine}"
	chunkID := fmt.Sprintf("%s:%d:%d", filePath, chunk.StartLine, chunk.EndLine)
	chunkHash := HashText(chunk.Text)

	embedding := m.getEmbedding(ctx, tx, chunk.Text)

	var embBlob []byte
	if embedding != nil {
		embBlob = vectorToBlob(embedding)
	}

	// Python: INSERT OR REPLACE INTO chunks ... RETURNING rowid
	var rowid int64
	err := tx.QueryRow(
		"INSERT OR REPLACE INTO chunks (id, path, source, start_line, end_line, hash, model, text, embedding, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING rowid",
		chunkID, filePath, source, chunk.StartLine, chunk.EndLine, chunkHash, m.provider.Model(), chunk.Text, embBlob, time.Now().Unix(),
	).Scan(&rowid)
	if err != nil {
		return err
	}

	// FTS5 写入（Python: try/except → logger.debug）
	if m.ftsAvailable && rowid > 0 {
		if _, err := tx.Exec(fmt.Sprintf(
			"INSERT OR REPLACE INTO %s (rowid, id, path, source, text) VALUES (?, ?, ?, ?, ?)", ftsTable,
		), rowid, chunkID, filePath, source, chunk.Text); err != nil {
			// 对齐 Python: logger.debug(f"Failed to insert into FTS: {e}")
			logger.Debug(logComponent).Err(err).Msg("Failed to insert into FTS")
		}
	}

	// vec0 写入（Python: try/except → logger.debug）
	if m.vectorAvailable && embedding != nil && rowid > 0 {
		if m.ensureVectorTable(len(embedding)) {
			if _, err := tx.Exec(fmt.Sprintf(
				"INSERT OR REPLACE INTO %s (rowid, embedding) VALUES (?, vec_f32(?))", vectorTable,
			), rowid, embBlob); err != nil {
				// 对齐 Python: logger.debug(f"Failed to insert into vector table: {e}")
				logger.Debug(logComponent).Err(err).Msg("Failed to insert into vector table")
			}
		}
	}
	return nil
}

// removeFileFromIndex 从索引移除文件。对齐 Python _remove_file_from_index（manager.py L715-742）：
// 向量/FTS/chunks/files 四表清理；失败记日志并回滚
func (m *MemoryIndexManager) removeFileFromIndex(filePath string) {
	rows, err := m.db.Query("SELECT rowid FROM chunks WHERE path = ?", filePath)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to remove file from index: {e}")
		logger.Error(logComponent).Err(err).Str("path", filePath).Msg("Failed to remove file from index")
		return
	}
	var rowids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			rowids = append(rowids, id)
		}
	}
	_ = rows.Close()

	// Python: if self.vector_available: DELETE FROM VECTOR_TABLE（try/except pass）
	if m.vectorAvailable {
		for _, id := range rowids {
			if _, err := m.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", vectorTable), id); err != nil {
				_ = err
			}
		}
	}
	// Python: if self.fts_available: DELETE FROM FTS_TABLE（try/except pass）
	if m.ftsAvailable {
		for _, id := range rowids {
			if _, err := m.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", ftsTable), id); err != nil {
				_ = err
			}
		}
	}

	if _, err := m.db.Exec("DELETE FROM chunks WHERE path = ?", filePath); err != nil {
		logger.Error(logComponent).Err(err).Str("path", filePath).Msg("Failed to remove file from index")
		return
	}
	if _, err := m.db.Exec("DELETE FROM files WHERE path = ?", filePath); err != nil {
		logger.Error(logComponent).Err(err).Str("path", filePath).Msg("Failed to remove file from index")
		return
	}
}

// getEmbedding 获取嵌入（带缓存）。对齐 Python _get_embedding（manager.py L744-779）：
// 四键（provider/model/provider_key/hash）查缓存 → miss 时 EmbedQuery + 写回
func (m *MemoryIndexManager) getEmbedding(ctx context.Context, tx *sql.Tx, text string) []float64 {
	if m.provider == nil {
		return nil
	}

	textHash := HashText(text)

	if m.cacheEnabled {
		var cachedBlob []byte
		err := tx.QueryRow(fmt.Sprintf(
			"SELECT embedding FROM %s WHERE provider = ? AND model = ? AND provider_key = ? AND hash = ?",
			embeddingCacheTable,
		), m.provider.ID(), m.provider.Model(), m.providerKey, textHash).Scan(&cachedBlob)
		if err == nil {
			return blobToVector(cachedBlob)
		}
	}

	embedding, err := m.provider.EmbedQuery(ctx, text)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to get embedding: {e}")
		logger.Error(logComponent).Err(err).Msg("Failed to get embedding")
		return nil
	}

	if m.cacheEnabled {
		if _, err := tx.Exec(fmt.Sprintf(
			"INSERT OR REPLACE INTO %s (provider, model, provider_key, hash, embedding, dims, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			embeddingCacheTable,
		), m.provider.ID(), m.provider.Model(), m.providerKey, textHash, vectorToBlob(embedding), len(embedding), time.Now().Unix()); err != nil {
			// Python: 无对应日志（commit 静默失败不影响主流程）
			_ = err
		}
	}
	return embedding
}

// searchVector 向量检索。对齐 Python _search_vector（manager.py L842-912）：
// vec_distance_cosine + score=max(0,1-d/2)；不可用或失败时降级内存余弦
func (m *MemoryIndexManager) searchVector(ctx context.Context, queryVec []float64, limit int) []MemorySearchResult {
	// Python: if not self.vector_enabled: return await self._search_vector_fallback(...)
	if !m.vectorEnabled {
		return m.searchVectorFallback(queryVec, limit)
	}

	// Python: if not self.vector_dims: sample = await provider.embed_query("sample"); ensure_vector_table(len(sample))
	if m.vectorDims == nil {
		sample, err := m.provider.EmbedQuery(ctx, "sample")
		if err == nil {
			m.ensureVectorTable(len(sample))
		}
	} else {
		m.ensureVectorTable(*m.vectorDims)
	}

	// Python: if not self.vector_available: return fallback
	if !m.vectorAvailable {
		return m.searchVectorFallback(queryVec, limit)
	}

	results, err := m.searchVectorWithVec0(queryVec, limit)
	if err != nil {
		// 对齐 Python: logger.debug(f"Vector search with sqlite-vec failed: {e}") → fallback
		logger.Debug(logComponent).Err(err).Msg("Vector search with sqlite-vec failed")
		return m.searchVectorFallback(queryVec, limit)
	}
	return results
}

// searchVectorWithVec0 sqlite-vec 路径检索。对齐 Python _search_vector 主体（manager.py L860-908）：
// chunk_map + vec_distance_cosine 排序 + score=max(0,1-d/2)
func (m *MemoryIndexManager) searchVectorWithVec0(queryVec []float64, limit int) ([]MemorySearchResult, error) {
	queryBlob := vectorToBlob(queryVec)
	sourceFilter := m.buildSourceFilter()

	// Python: SELECT rowid, id, path, source, start_line, end_line, text FROM chunks WHERE {source_filter}
	chunkRows, err := m.db.Query(fmt.Sprintf(
		"SELECT rowid, id, path, source, start_line, end_line, text FROM chunks WHERE %s", sourceFilter,
	))
	if err != nil {
		return nil, err
	}
	type chunkBase struct {
		id        string
		path      string
		source    string
		startLine int
		endLine   int
		snippet   string
	}
	chunkMap := make(map[int64]chunkBase)
	for chunkRows.Next() {
		var rowid int64
		var b chunkBase
		var text string
		if err := chunkRows.Scan(&rowid, &b.id, &b.path, &b.source, &b.startLine, &b.endLine, &text); err == nil {
			b.snippet = snippetOf(text)
			chunkMap[rowid] = b
		}
	}
	_ = chunkRows.Close()

	if len(chunkMap) == 0 {
		return []MemorySearchResult{}, nil
	}

	// Python: SELECT rowid, vec_distance_cosine(embedding, vec_f32(?)) as distance FROM VECTOR_TABLE
	//         WHERE rowid IN (...) ORDER BY distance LIMIT ?
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunkMap)), ",")
	args := make([]any, 0, len(chunkMap)+2)
	args = append(args, queryBlob)
	for id := range chunkMap {
		args = append(args, id)
	}
	args = append(args, limit)

	vecRows, err := m.db.Query(fmt.Sprintf(
		"SELECT rowid, vec_distance_cosine(embedding, vec_f32(?)) as distance FROM %s WHERE rowid IN (%s) ORDER BY distance LIMIT ?",
		vectorTable, placeholders,
	), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = vecRows.Close() }()

	results := make([]MemorySearchResult, 0)
	for vecRows.Next() {
		var rowid int64
		var distance float64
		if err := vecRows.Scan(&rowid, &distance); err == nil {
			if base, ok := chunkMap[rowid]; ok {
				// Python: score = max(0, 1 - distance / 2)
				score := math.Max(0, 1-distance/2)
				results = append(results, MemorySearchResult{
					ID: base.id, Path: base.path, Source: base.source,
					StartLine: base.startLine, EndLine: base.endLine,
					Snippet: base.snippet, Score: score,
				})
			}
		}
	}
	return results, nil
}

// searchVectorFallback 内存余弦降级检索。对齐 Python _search_vector_fallback（manager.py L914-962）：
// 查询向量归一化 → 逐行 blob 解包余弦 → 排序截断
func (m *MemoryIndexManager) searchVectorFallback(queryVec []float64, limit int) []MemorySearchResult {
	// Python: query_norm = math.sqrt(sum(x*x)); if query_norm < 1e-10: return []
	var qn float64
	for _, x := range queryVec {
		qn += x * x
	}
	queryNorm := math.Sqrt(qn)
	if queryNorm < 1e-10 {
		return []MemorySearchResult{}
	}
	normalized := make([]float64, len(queryVec))
	for i, x := range queryVec {
		normalized[i] = x / queryNorm
	}

	sourceFilter := m.buildSourceFilter()
	rows, err := m.db.Query(fmt.Sprintf(
		"SELECT id, path, source, start_line, end_line, text, embedding FROM chunks WHERE %s AND embedding IS NOT NULL",
		sourceFilter,
	))
	if err != nil {
		return []MemorySearchResult{}
	}
	defer func() { _ = rows.Close() }()

	results := make([]MemorySearchResult, 0)
	for rows.Next() {
		var id, path, source, text string
		var startLine, endLine int
		var blob []byte
		if err := rows.Scan(&id, &path, &source, &startLine, &endLine, &text, &blob); err != nil || blob == nil {
			continue
		}

		vec := blobToVector(blob)
		if len(vec) != len(normalized) {
			continue
		}

		// Python: dot = sum(a*b); vec_norm = sqrt(sum(x*x)); similarity = dot / vec_norm
		var dot, vn float64
		for i := range vec {
			dot += vec[i] * normalized[i]
			vn += vec[i] * vec[i]
		}
		vecNorm := math.Sqrt(vn)
		if vecNorm < 1e-10 {
			continue
		}

		results = append(results, MemorySearchResult{
			ID: id, Path: path, Source: source,
			StartLine: startLine, EndLine: endLine,
			Snippet: snippetOf(text),
			Score:   math.Max(0, dot/vecNorm),
		})
	}

	// Python: results.sort(key=lambda x: x["score"], reverse=True); return results[:limit]
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// searchKeyword FTS5 关键词检索。对齐 Python _search_keyword（manager.py L964-1024）：
// build_fts_query → MATCH → bm25_rank_to_score；异常返回空
func (m *MemoryIndexManager) searchKeyword(query string, limit int) []MemorySearchResult {
	if !m.ftsAvailable {
		return []MemorySearchResult{}
	}

	ftsQuery := BuildFTSQuery(query)
	if ftsQuery == "" {
		return []MemorySearchResult{}
	}

	sourceFilter := m.buildSourceFilter()

	chunkRows, err := m.db.Query(fmt.Sprintf(
		"SELECT rowid, id, path, source, start_line, end_line, text FROM chunks WHERE %s", sourceFilter,
	))
	if err != nil {
		// 对齐 Python: except → logger.debug + return []
		logger.Debug(logComponent).Err(err).Msg("Keyword search failed")
		return []MemorySearchResult{}
	}
	type chunkBase struct {
		id        string
		path      string
		source    string
		startLine int
		endLine   int
		snippet   string
	}
	chunkMap := make(map[int64]chunkBase)
	for chunkRows.Next() {
		var rowid int64
		var b chunkBase
		var text string
		if err := chunkRows.Scan(&rowid, &b.id, &b.path, &b.source, &b.startLine, &b.endLine, &text); err == nil {
			b.snippet = snippetOf(text)
			chunkMap[rowid] = b
		}
	}
	_ = chunkRows.Close()

	if len(chunkMap) == 0 {
		return []MemorySearchResult{}
	}

	// Python: SELECT rowid, rank FROM FTS_TABLE WHERE FTS_TABLE MATCH ? ORDER BY rank LIMIT ?
	ftsRows, err := m.db.Query(fmt.Sprintf(
		"SELECT rowid, rank FROM %s WHERE %s MATCH ? ORDER BY rank LIMIT ?", ftsTable, ftsTable,
	), ftsQuery, limit)
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("Keyword search failed")
		return []MemorySearchResult{}
	}
	defer func() { _ = ftsRows.Close() }()

	results := make([]MemorySearchResult, 0)
	for ftsRows.Next() {
		var rowid int64
		var rank float64
		if err := ftsRows.Scan(&rowid, &rank); err == nil {
			if base, ok := chunkMap[rowid]; ok {
				// Python: score = bm25_rank_to_score(float(row["rank"]))
				results = append(results, MemorySearchResult{
					ID: base.id, Path: base.path, Source: base.source,
					StartLine: base.startLine, EndLine: base.endLine,
					Snippet: base.snippet,
					Score:   Bm25RankToScore(rank),
				})
			}
		}
	}
	return results
}

// buildSourceFilter 构造 source 过滤 SQL。对齐 Python _build_source_filter（manager.py L1026-1035）：
// 空 → 1=0；单个 → source = 'x'；多个 → IN + repr 单引号
func (m *MemoryIndexManager) buildSourceFilter() string {
	sources := m.Settings.Sources
	if len(sources) == 0 {
		return "1=0"
	}
	if len(sources) == 1 {
		return fmt.Sprintf("source = '%s'", sources[0])
	}
	quoted := make([]string, 0, len(sources))
	for _, s := range sources {
		quoted = append(quoted, "'"+s+"'")
	}
	return fmt.Sprintf("source IN (%s)", strings.Join(quoted, ", "))
}

// mergeHybridResults 合并混合检索结果。对齐 Python _merge_hybrid_results（manager.py L1037-1068）：
// by_id 合并，score = vw*vector + tw*text，按 score 降序
func mergeHybridResults(vectorResults, keywordResults []MemorySearchResult, vectorWeight, textWeight float64) []MemorySearchResult {
	byID := make(map[string]*hybridEntry)

	for _, r := range vectorResults {
		e := r
		byID[r.ID] = &hybridEntry{result: &e, vectorScore: r.Score, textScore: 0}
	}
	for _, r := range keywordResults {
		if existing, ok := byID[r.ID]; ok {
			existing.textScore = r.Score
		} else {
			e := r
			byID[r.ID] = &hybridEntry{result: &e, vectorScore: 0, textScore: r.Score}
		}
	}

	results := make([]MemorySearchResult, 0, len(byID))
	for _, e := range byID {
		// Python: r["score"] = vector_weight * r["_vector_score"] + text_weight * r["_text_score"]
		e.result.Score = vectorWeight*e.vectorScore + textWeight*e.textScore
		results = append(results, *e.result)
	}

	// Python: results.sort(key=lambda x: x["score"], reverse=True)
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// hybridEntry 混合合并中间结构（对齐 Python _vector_score/_text_score 临时字段）
type hybridEntry struct {
	result      *MemorySearchResult
	vectorScore float64
	textScore   float64
}

// embedQueryWithTimeout 带超时的查询嵌入。对齐 Python _embed_query_with_timeout（manager.py L1070-1083）：
// 60s 超时；超时 warn 返回 nil；错误 error 返回 nil
func (m *MemoryIndexManager) embedQueryWithTimeout(ctx context.Context, query string) []float64 {
	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	vec, err := m.provider.EmbedQuery(timeoutCtx, query)
	if err != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			// 对齐 Python: logger.warning("Embedding query timed out")
			logger.Warn(logComponent).Msg("Embedding query timed out")
			return nil
		}
		// 对齐 Python: logger.error(f"Embedding query failed: {e}")
		logger.Error(logComponent).Err(err).Msg("Embedding query failed")
		return nil
	}
	return vec
}

// writeMeta 写元数据。对齐 Python _write_meta（manager.py L1119-1125）
func (m *MemoryIndexManager) writeMeta(meta map[string]any) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", metaKey, string(data))
	return err
}

// cacheEntryCount 缓存条目数。对齐 Python _get_cache_entry_count（manager.py L1173-1179）：异常返回 0
func (m *MemoryIndexManager) cacheEntryCount() int {
	var count int
	if err := m.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", embeddingCacheTable)).Scan(&count); err != nil {
		return 0
	}
	return count
}

// containsSource 检查 sources 是否包含指定来源
func containsSource(sources []string, target string) bool {
	for _, s := range sources {
		if s == target {
			return true
		}
	}
	return false
}

// snippetOf 截取摘要（对齐 Python str(row["text"][:SNIPPET_MAX_CHARS])）
func snippetOf(text string) string {
	runes := []rune(text)
	if len(runes) > snippetMaxChars {
		return string(runes[:snippetMaxChars])
	}
	return text
}

// vectorToBlob 向量转二进制。对齐 Python vector_to_blob（manager.py L45-47）：struct.pack float32 小端
func vectorToBlob(vec []float64) []byte {
	blob := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(float32(v))
		blob[i*4] = byte(bits)
		blob[i*4+1] = byte(bits >> 8)
		blob[i*4+2] = byte(bits >> 16)
		blob[i*4+3] = byte(bits >> 24)
	}
	return blob
}

// blobToVector 二进制转向量。对齐 Python blob_to_vector（manager.py L50-53）：struct.unpack float32
func blobToVector(blob []byte) []float64 {
	if len(blob)%4 != 0 {
		return nil
	}
	vec := make([]float64, len(blob)/4)
	for i := range vec {
		bits := uint32(blob[i*4]) | uint32(blob[i*4+1])<<8 | uint32(blob[i*4+2])<<16 | uint32(blob[i*4+3])<<24
		vec[i] = float64(math.Float32frombits(bits))
	}
	return vec
}
