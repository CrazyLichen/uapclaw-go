package lite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mattn/go-sqlite3"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	apiEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// metaKey 元数据键名
	metaKey = "memory_index_meta_v1"
	// snippetMaxChars 摘要最大字符数
	snippetMaxChars = 700
	// vectorTable vec0 虚拟表名
	vectorTable = "chunks_vec"
	// ftsTable FTS5 虚拟表名
	ftsTable = "chunks_fts"
	// embeddingCacheTable embedding 缓存表名
	embeddingCacheTable = "embedding_cache"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionDeltaState 会话增量状态。对齐 Python SessionDeltaState
type SessionDeltaState struct {
	// LastSize 上次文件大小
	LastSize int
	// PendingBytes 待处理字节
	PendingBytes int
	// PendingMessages 待处理消息计数。对齐 Python pending_messages: int = 0
	PendingMessages int
}

// memoryIndexManager 记忆索引管理器实现。对齐 Python MemoryIndexManager
type memoryIndexManager struct {
	// 基础信息
	agentID   string
	workspace *workspace.Workspace
	nodeName  string
	memoryDir string
	settings  *MemorySettings

	// 数据库
	db     *sql.DB
	dbPath string

	// Embedding
	provider    EmbeddingProvider
	providerKey string

	// 状态
	mu              sync.Mutex
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
	embeddingConfig *apiEmbedding.EmbeddingConfig
	sysOperation    sysop.SysOperation
	// TODO: 7.2 实现时替换为具体接口（如 MemUpdateChecker），当前用 any 规避循环依赖
	llm any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// indexCache 管理器缓存
var indexCache sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// getMemoryIndexManager 幂等获取管理器实例。对齐 Python MemoryIndexManager.get
func getMemoryIndexManager(params MemoryManagerParams) (MemoryIndexManager, error) {
	if params.Workspace == nil {
		return nil, fmt.Errorf("workspace 为 nil")
	}
	nodePath := params.Workspace.GetNodePath(params.NodeName)
	memoryDir := ""
	if nodePath != nil {
		memoryDir = *nodePath
	}
	cacheKey := fmt.Sprintf("%s:%s:%s", params.AgentID, params.NodeName, memoryDir)

	if cached, ok := indexCache.Load(cacheKey); ok {
		mgr := cached.(*memoryIndexManager)
		if !mgr.closed {
			return mgr, nil
		}
		_ = mgr.Close()
	}

	settings := params.Settings
	if settings == nil {
		settings = CreateMemorySettings(memoryDir, nil)
	}

	mgr := &memoryIndexManager{
		agentID:         params.AgentID,
		workspace:       params.Workspace,
		nodeName:        params.NodeName,
		memoryDir:       memoryDir,
		settings:        settings,
		dirty:           true,
		watchDebounce:   2 * time.Second,
		sessionDeltas:   make(map[string]*SessionDeltaState),
		embeddingConfig: params.EmbeddingConfig,
		sysOperation:    params.SysOperation,
	}

	if err := mgr.Initialize(context.Background()); err != nil {
		logger.Error(logger.ComponentCommon).Err(err).Msg("初始化记忆管理器失败")
		return nil, err
	}

	indexCache.Store(cacheKey, mgr)
	return mgr, nil
}

// clearMemoryManagerCache 清除缓存。对齐 Python clear_memory_manager_cache
func clearMemoryManagerCache() {
	indexCache.Range(func(key, value any) bool {
		if mgr, ok := value.(*memoryIndexManager); ok {
			_ = mgr.Close()
		}
		indexCache.Delete(key)
		return true
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Initialize 初始化管理器。对齐 Python MemoryIndexManager.initialize
func (m *memoryIndexManager) Initialize(ctx context.Context) error {
	m.dbPath = m.resolveDBPath()

	if err := EnsureDir(filepath.Dir(m.dbPath)); err != nil {
		return fmt.Errorf("确保数据库目录失败: %w", err)
	}

	var err error
	m.db, err = sql.Open("sqlite3", m.dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	m.db.SetMaxOpenConns(1)

	if err := m.ensureSchema(); err != nil {
		return fmt.Errorf("建 schema 失败: %w", err)
	}

	if err := m.initializeProvider(ctx); err != nil {
		return fmt.Errorf("初始化 provider 失败: %w", err)
	}

	if err := m.loadVectorExtension(); err != nil {
		m.vectorAvailable = false
		m.vectorError = err.Error()
		logger.Warn(logger.ComponentCommon).Err(err).Msg("加载 vec0 扩展失败，将使用内存余弦 fallback")
	}

	if err := m.Sync(ctx, "initial", false); err != nil {
		logger.Warn(logger.ComponentCommon).Err(err).Msg("初始同步失败")
	}

	// 文件监听
	if syncWatch, _ := m.settings.Sync["watch"].(bool); syncWatch {
		m.setupFileWatcher()
	}

	// 定时同步
	m.ensureIntervalSync()

	logger.Info(logger.ComponentCommon).Str("agent_id", m.agentID).Msg("记忆管理器初始化完成")
	return nil
}

// resolveDBPath 解析数据库路径。对齐 Python _resolve_db_path
func (m *memoryIndexManager) resolveDBPath() string {
	storePath := "memory.db"
	if v, ok := m.settings.Store["path"].(string); ok && v != "" {
		storePath = v
	}
	if filepath.IsAbs(storePath) {
		return storePath
	}
	// 对齐 Python: 处理 workspace_name 前缀
	workspaceName := filepath.Base(m.memoryDir)
	if strings.HasPrefix(storePath, workspaceName+"/") || strings.HasPrefix(storePath, workspaceName+`\`) {
		storePath = storePath[len(workspaceName)+1:]
	}
	return filepath.Join(m.memoryDir, storePath)
}

// ensureSchema 建数据库表。对齐 Python _ensure_schema
func (m *memoryIndexManager) ensureSchema() error {
	// meta 表
	if _, err := m.db.Exec("CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		return fmt.Errorf("建 meta 表失败: %w", err)
	}
	// files 表
	if _, err := m.db.Exec("CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, source TEXT, hash TEXT, mtime INTEGER, size INTEGER)"); err != nil {
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
	// FTS5 虚拟表
	ftsEnabled := true
	if v, ok := m.settings.Store["fts"].(map[string]any); ok {
		if v2, ok := v["enabled"].(bool); ok {
			ftsEnabled = v2
		}
	}
	if ftsEnabled {
		_, err := m.db.Exec(fmt.Sprintf(
			"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(id UNINDEXED, path UNINDEXED, source UNINDEXED, text, content='', contentless_delete=1)",
			ftsTable,
		))
		if err != nil {
			m.ftsAvailable = false
			m.ftsError = err.Error()
			logger.Warn(logger.ComponentCommon).Err(err).Msg("建 FTS5 表失败")
		} else {
			m.ftsAvailable = true
		}
	}
	return nil
}

// initializeProvider 初始化嵌入提供者。对齐 Python _initialize_provider
func (m *memoryIndexManager) initializeProvider(ctx context.Context) error {
	provider, err := CreateEmbeddingProvider(
		m.settings.Provider,
		m.settings.Model,
		m.settings.Fallback,
		m.embeddingConfig,
	)
	if err != nil {
		return err
	}
	m.provider = provider
	// 对齐 Python: self.provider_key = f"{self.provider.id}:{self.provider.model}"
	m.providerKey = fmt.Sprintf("provider:%s", m.settings.Model)
	logger.Info(logger.ComponentCommon).Str("provider", m.settings.Provider).Str("model", m.settings.Model).Msg("嵌入提供者初始化完成")
	return nil
}

// loadVectorExtension 加载 vec0.so 扩展。对齐 Python _load_vector_extension
func (m *memoryIndexManager) loadVectorExtension() error {
	vecEnabled := true
	if v, ok := m.settings.Store["vector"].(map[string]any); ok {
		if v2, ok := v["enabled"].(bool); ok {
			vecEnabled = v2
		}
	}
	if !vecEnabled || m.db == nil {
		return nil
	}

	vecPath := ResolveVec0Path()
	if _, err := os.Stat(vecPath); err != nil {
		return fmt.Errorf("vec0.so 不存在: %s", vecPath)
	}

	conn, err := m.db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("获取连接失败: %w", err)
	}
	defer func() { _ = conn.Close() }()

	err = conn.Raw(func(driverConn interface{}) error {
		dc, ok := driverConn.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("不支持 LoadExtension 的驱动类型")
		}
		return LoadVec0Extension(dc, vecPath)
	})
	if err != nil {
		return fmt.Errorf("加载 vec0 扩展失败: %w", err)
	}

	m.vectorAvailable = true
	logger.Info(logger.ComponentCommon).Str("vec_path", vecPath).Msg("vec0 扩展加载成功")
	return nil
}

// Sync 同步索引。对齐 Python MemoryIndexManager.sync
func (m *memoryIndexManager) Sync(ctx context.Context, reason string, force bool) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	needsFullReindex := force || m.shouldFullReindex()

	if needsFullReindex {
		logger.Info(logger.ComponentCommon).Str("reason", reason).Msg("执行全量重建索引")
		return m.runReindex(ctx)
	}

	logger.Debug(logger.ComponentCommon).Str("reason", reason).Msg("增量同步")

	if containsSource(m.settings.Sources, "memory") && m.dirty {
		if err := m.syncMemoryFiles(ctx); err != nil {
			return err
		}
		m.dirty = false
	}

	if containsSource(m.settings.Sources, "sessions") {
		if err := m.syncSessionFiles(ctx); err != nil {
			return err
		}
	}

	return nil
}

// shouldFullReindex 检查是否需要全量重建。对齐 Python _should_full_reindex
func (m *memoryIndexManager) shouldFullReindex() bool {
	row := m.db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKey)
	var value string
	if err := row.Scan(&value); err != nil {
		return true
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return true
	}
	// 对齐 Python: 检查 provider 和 model 是否变化
	if meta["provider"] != m.settings.Provider {
		return true
	}
	if meta["model"] != m.settings.Model {
		return true
	}
	if meta["chunkTokens"] != m.settings.Chunking["tokens"] {
		return true
	}
	return false
}

// runReindex 全量重建。对齐 Python _run_reindex
func (m *memoryIndexManager) runReindex(ctx context.Context) error {
	if containsSource(m.settings.Sources, "memory") {
		if err := m.syncMemoryFiles(ctx); err != nil {
			return err
		}
		m.dirty = false
	}
	if containsSource(m.settings.Sources, "sessions") {
		if err := m.syncSessionFiles(ctx); err != nil {
			return err
		}
	}
	meta := map[string]any{
		"provider":     m.settings.Provider,
		"model":        m.settings.Model,
		"providerKey":  m.providerKey,
		"chunkTokens":  m.settings.Chunking["tokens"],
		"chunkOverlap": m.settings.Chunking["overlap"],
	}
	if m.vectorAvailable && m.vectorDims != nil {
		meta["vectorDims"] = *m.vectorDims
	}
	return m.writeMeta(meta)
}

// syncMemoryFiles 同步 .md 记忆文件。对齐 Python _sync_memory_files
func (m *memoryIndexManager) syncMemoryFiles(ctx context.Context) error {
	// 对齐 Python: files = list_memory_files(self.workspace, node_name=self.node_name)
	files := ListMemoryFiles(m.workspace, m.settings.ExtraPaths, m.nodeName)

	logger.Debug(logger.ComponentCommon).Int("file_count", len(files)).Msg("同步记忆文件")

	activePaths := make(map[string]bool)
	for _, fp := range files {
		baseDir := m.memoryDir
		entry, err := m.buildFileEntry(fp, baseDir)
		if err != nil {
			continue
		}
		activePaths[entry.Path] = true

		var hash string
		row := m.db.QueryRow("SELECT hash FROM files WHERE path = ? AND source = ?", entry.Path, "memory")
		if err := row.Scan(&hash); err == nil && hash == entry.Hash {
			continue
		}
		if err := m.indexFile(ctx, entry, "memory"); err != nil {
			logger.Error(logger.ComponentCommon).Err(err).Str("path", entry.Path).Msg("索引文件失败")
		}
	}

	// 删除不存在的文件
	rows, err := m.db.Query("SELECT path FROM files WHERE source = ?", "memory")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			continue
		}
		if !activePaths[path] {
			m.removeFileFromIndex(context.Background(), path)
		}
	}
	return nil
}

// syncSessionFiles 同步 session 文件。对齐 Python _sync_session_files
func (m *memoryIndexManager) syncSessionFiles(ctx context.Context) error {
	sessionsDir := filepath.Join(m.memoryDir, "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil
	}

	var sessionFiles []string
	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		sessionFiles = append(sessionFiles, path)
		return nil
	})

	logger.Debug(logger.ComponentCommon).Int("file_count", len(sessionFiles)).Msg("同步 session 文件")

	activePaths := make(map[string]bool)
	for _, sessionFile := range sessionFiles {
		entry, err := m.buildFileEntry(sessionFile, m.memoryDir)
		if err != nil {
			continue
		}
		activePaths[entry.Path] = true

		var hash string
		row := m.db.QueryRow("SELECT hash FROM files WHERE path = ? AND source = ?", entry.Path, "sessions")
		if err := row.Scan(&hash); err == nil && hash == entry.Hash {
			continue
		}
		if err := m.indexFile(ctx, entry, "sessions"); err != nil {
			logger.Error(logger.ComponentCommon).Err(err).Str("path", entry.Path).Msg("索引 session 文件失败")
		}
	}

	// 删除不存在的文件
	rows, err := m.db.Query("SELECT path FROM files WHERE source = ?", "sessions")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			continue
		}
		if !activePaths[path] {
			m.removeFileFromIndex(context.Background(), path)
		}
	}
	return nil
}

// indexFile 索引单个文件。对齐 Python _index_file
func (m *memoryIndexManager) indexFile(ctx context.Context, entry *FileEntry, source string) error {
	absPath := entry.AbsPath
	var content string
	if m.sysOperation != nil {
		// 对齐 Python: 使用 sys_operation 读取文件
		readResult, err := m.sysOperation.Fs().ReadFile(ctx, absPath)
		if err != nil {
			return fmt.Errorf("读取文件失败: %w", err)
		}
		if readResult.Data != nil {
			content = readResult.Data.Content
		}
	} else {
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("读取文件失败: %w", err)
		}
		content = string(data)
	}

	chunkTokens, _ := m.settings.Chunking["tokens"].(int)
	chunkOverlap, _ := m.settings.Chunking["overlap"].(int)
	chunks := ChunkMarkdown(content, chunkTokens, chunkOverlap)

	// 清除旧索引
	_, _ = m.db.Exec("DELETE FROM chunks WHERE path = ?", entry.Path)
	if m.ftsAvailable {
		_, _ = m.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE path = ?", ftsTable), entry.Path)
	}

	for _, chunk := range chunks {
		if err := m.indexChunk(ctx, entry.Path, source, chunk); err != nil {
			logger.Warn(logger.ComponentCommon).Err(err).Str("path", entry.Path).Msg("索引 chunk 失败")
		}
	}

	_, err := m.db.Exec("INSERT OR REPLACE INTO files (path, source, hash, mtime, size) VALUES (?, ?, ?, ?, ?)",
		entry.Path, source, entry.Hash, entry.MtimeMs, entry.Size)
	return err
}

// indexChunk 索引单个 chunk。对齐 Python _index_chunk
func (m *memoryIndexManager) indexChunk(ctx context.Context, filePath, source string, chunk MemoryChunk) error {
	chunkID := fmt.Sprintf("%s:%d:%d", filePath, chunk.StartLine, chunk.EndLine)
	chunkHash := HashText(chunk.Text)

	var embBlob []byte
	emb, err := m.getEmbedding(ctx, chunk.Text)
	if err == nil && emb != nil {
		embBlob = vectorToBlob(emb)
	}

	result, err := m.db.Exec(
		"INSERT OR REPLACE INTO chunks (id, path, source, start_line, end_line, hash, model, text, embedding, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		chunkID, filePath, source, chunk.StartLine, chunk.EndLine, chunkHash, m.settings.Model, chunk.Text, embBlob, time.Now().Unix(),
	)
	if err != nil {
		return err
	}

	rowid, _ := result.LastInsertId()

	// FTS5
	if m.ftsAvailable && rowid > 0 {
		_, err := m.db.Exec(fmt.Sprintf("INSERT OR REPLACE INTO %s (rowid, id, path, source, text) VALUES (?, ?, ?, ?, ?)", ftsTable),
			rowid, chunkID, filePath, source, chunk.Text)
		if err != nil {
			logger.Debug(logger.ComponentCommon).Err(err).Msg("插入 FTS5 失败")
		}
	}

	// vec0
	if m.vectorAvailable && emb != nil && rowid > 0 {
		if m.ensureVectorTable(len(emb)) {
			_, err := m.db.Exec(fmt.Sprintf("INSERT OR REPLACE INTO %s (rowid, embedding) VALUES (?, vec_f32(?))", vectorTable),
				rowid, embBlob)
			if err != nil {
				logger.Debug(logger.ComponentCommon).Err(err).Msg("插入 vec0 失败")
			}
		}
	}

	return nil
}

// removeFileFromIndex 删除文件索引。对齐 Python _remove_file_from_index
func (m *memoryIndexManager) removeFileFromIndex(ctx context.Context, filePath string) {
	if m.vectorAvailable {
		rows, err := m.db.Query("SELECT rowid FROM chunks WHERE path = ?", filePath)
		if err == nil {
			for rows.Next() {
				var rowid int64
				if rows.Scan(&rowid) == nil {
					_, _ = m.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", vectorTable), rowid)
				}
			}
			_ = rows.Close()
		}
	}
	if m.ftsAvailable {
		rows, err := m.db.Query("SELECT rowid FROM chunks WHERE path = ?", filePath)
		if err == nil {
			for rows.Next() {
				var rowid int64
				if rows.Scan(&rowid) == nil {
					_, _ = m.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", ftsTable), rowid)
				}
			}
			_ = rows.Close()
		}
	}
	_, _ = m.db.Exec("DELETE FROM chunks WHERE path = ?", filePath)
	_, _ = m.db.Exec("DELETE FROM files WHERE path = ?", filePath)
}

// buildFileEntry 构建文件索引条目。对齐 Python build_file_entry
func (m *memoryIndexManager) buildFileEntry(absPath, baseDir string) (*FileEntry, error) {
	stat, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	relPath, err := filepath.Rel(baseDir, absPath)
	if err != nil {
		relPath = absPath
	}
	return &FileEntry{
		Path:    relPath,
		AbsPath: absPath,
		Hash:    HashText(string(content)),
		MtimeMs: stat.ModTime().UnixMilli(),
		Size:    stat.Size(),
	}, nil
}

// getEmbedding 获取 embedding（带缓存）。对齐 Python _get_embedding
func (m *memoryIndexManager) getEmbedding(ctx context.Context, text string) ([]float64, error) {
	if m.provider == nil {
		return nil, nil
	}
	textHash := HashText(text)

	// 查缓存
	cacheEnabled := true
	if v, ok := m.settings.Cache["enabled"].(bool); ok {
		cacheEnabled = v
	}
	if cacheEnabled {
		row := m.db.QueryRow(
			fmt.Sprintf("SELECT embedding FROM %s WHERE provider = ? AND model = ? AND provider_key = ? AND hash = ?", embeddingCacheTable),
			"provider", m.settings.Model, m.providerKey, textHash,
		)
		var blob []byte
		if err := row.Scan(&blob); err == nil && blob != nil {
			return blobToVector(blob), nil
		}
	}

	emb, err := m.provider.EmbedQuery(ctx, text)
	if err != nil {
		return nil, err
	}

	// 写缓存
	if cacheEnabled && emb != nil {
		_, _ = m.db.Exec(
			fmt.Sprintf("INSERT OR REPLACE INTO %s (provider, model, provider_key, hash, embedding, dims, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", embeddingCacheTable),
			"provider", m.settings.Model, m.providerKey, textHash, vectorToBlob(emb), len(emb), time.Now().Unix(),
		)
	}

	return emb, nil
}

// ensureVectorTable 确保 vec0 虚拟表存在。对齐 Python _ensure_vector_table
func (m *memoryIndexManager) ensureVectorTable(dims int) bool {
	if !m.vectorAvailable {
		return false
	}
	if m.vectorDims != nil && *m.vectorDims == dims {
		return true
	}
	if m.vectorDims != nil && *m.vectorDims != dims {
		_, _ = m.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", vectorTable))
	}
	_, err := m.db.Exec(fmt.Sprintf("CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d])", vectorTable, dims))
	if err != nil {
		m.vectorAvailable = false
		m.vectorError = err.Error()
		return false
	}
	m.vectorDims = &dims
	logger.Info(logger.ComponentCommon).Int("dims", dims).Msg("vec0 虚拟表创建成功")
	return true
}

// Search 混合搜索。对齐 Python MemoryIndexManager.search
func (m *memoryIndexManager) Search(ctx context.Context, query string, opts map[string]any) ([]SearchResult, error) {
	if opts == nil {
		opts = make(map[string]any)
	}

	// 搜索前同步
	onSearch := true
	if v, ok := m.settings.Sync["onSearch"].(bool); ok {
		onSearch = v
	}
	if onSearch && m.dirty {
		if err := m.Sync(ctx, "search", false); err != nil {
			logger.Warn(logger.ComponentCommon).Err(err).Msg("搜索前同步失败")
		}
	}

	cleaned := strings.TrimSpace(query)
	if len(cleaned) == 0 {
		return nil, nil
	}

	minScore := 0.3
	if v, ok := opts["min_score"].(float64); ok {
		minScore = v
	} else if v, ok := m.settings.Query["min_score"].(float64); ok {
		minScore = v
	}

	maxResults := 10
	if v, ok := opts["max_results"].(float64); ok {
		maxResults = int(v)
	} else if v, ok := m.settings.Query["max_results"].(float64); ok {
		maxResults = int(v)
	}

	hybrid := make(map[string]any)
	if v, ok := m.settings.Query["hybrid"].(map[string]any); ok {
		hybrid = v
	}
	candidateMultiplier := 2.0
	if v, ok := hybrid["candidateMultiplier"].(float64); ok {
		candidateMultiplier = v
	}
	candidates := int(float64(maxResults) * candidateMultiplier)
	if candidates < 1 {
		candidates = 1
	}

	// FTS5 关键词搜索
	var keywordResults []SearchResult
	hybridEnabled := true
	if v, ok := hybrid["enabled"].(bool); ok {
		hybridEnabled = v
	}
	if hybridEnabled && m.ftsAvailable {
		var err error
		keywordResults, err = m.searchKeyword(ctx, cleaned, candidates)
		if err != nil {
			logger.Debug(logger.ComponentCommon).Err(err).Msg("关键词搜索失败")
		}
	}

	// 向量搜索
	queryVec, _ := m.provider.EmbedQuery(ctx, cleaned)
	hasVector := len(queryVec) > 0

	var vectorResults []SearchResult
	if hasVector {
		var err error
		vectorResults, err = m.searchVector(ctx, queryVec, candidates)
		if err != nil {
			logger.Debug(logger.ComponentCommon).Err(err).Msg("向量搜索失败")
		}
	}

	// 非混合模式
	if !hybridEnabled {
		results := vectorResults
		var filtered []SearchResult
		for _, r := range results {
			if r.Score >= minScore {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > maxResults {
			filtered = filtered[:maxResults]
		}
		return filtered, nil
	}

	// 混合合并
	vectorWeight := 0.7
	if v, ok := hybrid["vectorWeight"].(float64); ok {
		vectorWeight = v
	}
	textWeight := 0.3
	if v, ok := hybrid["textWeight"].(float64); ok {
		textWeight = v
	}
	merged := mergeHybridResults(vectorResults, keywordResults, vectorWeight, textWeight)

	var filtered []SearchResult
	for _, r := range merged {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) > maxResults {
		filtered = filtered[:maxResults]
	}
	return filtered, nil
}

// searchVector vec0 向量搜索。对齐 Python _search_vector
func (m *memoryIndexManager) searchVector(ctx context.Context, queryVec []float64, limit int) ([]SearchResult, error) {
	if !m.vectorAvailable {
		return m.searchVectorFallback(ctx, queryVec, limit)
	}
	if m.vectorDims == nil {
		sample, err := m.provider.EmbedQuery(ctx, "sample")
		if err != nil {
			return nil, err
		}
		m.ensureVectorTable(len(sample))
	}
	if !m.vectorAvailable {
		return m.searchVectorFallback(ctx, queryVec, limit)
	}

	queryBlob := vectorToBlob(queryVec)
	sourceFilter, sourceParams := m.buildSourceFilter()

	// 获取 chunk 映射
	chunkMap := make(map[int64]ChunkData)
	rows, err := m.db.Query(fmt.Sprintf("SELECT rowid, id, path, source, start_line, end_line, text FROM chunks WHERE %s", sourceFilter), sourceParams...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rowid int64
		var id, path, source, text string
		var startLine, endLine int
		if err := rows.Scan(&rowid, &id, &path, &source, &startLine, &endLine, &text); err != nil {
			continue
		}
		chunkMap[rowid] = ChunkData{
			ID: id, Path: path, Source: source,
			StartLine: startLine, EndLine: endLine,
			Snippet: truncateString(text, snippetMaxChars),
		}
	}
	_ = rows.Close()

	if len(chunkMap) == 0 {
		return nil, nil
	}

	// vec0 搜索
	placeholders := make([]string, len(chunkMap))
	args := make([]any, 0, len(chunkMap)+2)
	args = append(args, queryBlob)
	i := 0
	for rowid := range chunkMap {
		placeholders[i] = "?"
		args = append(args, rowid)
		i++
	}
	query := fmt.Sprintf("SELECT rowid, vec_distance_cosine(embedding, vec_f32(?)) as distance FROM %s WHERE rowid IN (%s) ORDER BY distance LIMIT ?",
		vectorTable, strings.Join(placeholders, ","))
	args = append(args, limit)

	vecRows, err := m.db.Query(query, args...)
	if err != nil {
		return m.searchVectorFallback(ctx, queryVec, limit)
	}
	defer func() { _ = vecRows.Close() }()

	var results []SearchResult
	for vecRows.Next() {
		var rowid int64
		var distance float64
		if err := vecRows.Scan(&rowid, &distance); err != nil {
			continue
		}
		if chunk, ok := chunkMap[rowid]; ok {
			score := math.Max(0, 1-distance/2)
			results = append(results, SearchResult{
				ID: chunk.ID, Path: chunk.Path, Source: chunk.Source,
				StartLine: chunk.StartLine, EndLine: chunk.EndLine,
				Snippet: chunk.Snippet, Score: score,
			})
		}
	}
	return results, nil
}

// searchVectorFallback 内存余弦相似度 fallback。对齐 Python _search_vector_fallback
func (m *memoryIndexManager) searchVectorFallback(ctx context.Context, queryVec []float64, limit int) ([]SearchResult, error) {
	sourceFilter, sourceParams := m.buildSourceFilter()
	rows, err := m.db.Query(fmt.Sprintf("SELECT id, path, source, start_line, end_line, text, embedding FROM chunks WHERE %s AND embedding IS NOT NULL", sourceFilter), sourceParams...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []SearchResult
	for rows.Next() {
		var id, path, source, text string
		var startLine, endLine int
		var embBlob []byte
		if err := rows.Scan(&id, &path, &source, &startLine, &endLine, &text, &embBlob); err != nil {
			continue
		}
		vec := blobToVector(embBlob)
		if len(vec) != len(queryVec) {
			continue
		}
		similarity := CosineSimilarity(queryVec, vec)
		results = append(results, SearchResult{
			ID: id, Path: path, Source: source,
			StartLine: startLine, EndLine: endLine,
			Snippet: truncateString(text, snippetMaxChars),
			Score:   similarity,
		})
	}
	// 按分数排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// searchKeyword FTS5 关键词搜索。对齐 Python _search_keyword
func (m *memoryIndexManager) searchKeyword(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if !m.ftsAvailable {
		return nil, nil
	}
	ftsQuery := BuildFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	sourceFilter, sourceParams := m.buildSourceFilter()
	// 获取 chunk 映射
	chunkMap := make(map[int64]ChunkData)
	rows, err := m.db.Query(fmt.Sprintf("SELECT rowid, id, path, source, start_line, end_line, text FROM chunks WHERE %s", sourceFilter), sourceParams...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rowid int64
		var id, path, source, text string
		var startLine, endLine int
		if err := rows.Scan(&rowid, &id, &path, &source, &startLine, &endLine, &text); err != nil {
			continue
		}
		chunkMap[rowid] = ChunkData{
			ID: id, Path: path, Source: source,
			StartLine: startLine, EndLine: endLine,
			Snippet: truncateString(text, snippetMaxChars),
		}
	}
	_ = rows.Close()

	if len(chunkMap) == 0 {
		return nil, nil
	}

	ftsRows, err := m.db.Query(fmt.Sprintf("SELECT rowid, rank FROM %s WHERE %s MATCH ? ORDER BY rank LIMIT ?", ftsTable, ftsTable), ftsQuery, limit)
	if err != nil {
		logger.Debug(logger.ComponentCommon).Err(err).Msg("FTS5 查询失败")
		return nil, nil
	}
	defer func() { _ = ftsRows.Close() }()

	var results []SearchResult
	for ftsRows.Next() {
		var rowid int64
		var rank float64
		if err := ftsRows.Scan(&rowid, &rank); err != nil {
			continue
		}
		if chunk, ok := chunkMap[rowid]; ok {
			score := BM25RankToScore(rank)
			results = append(results, SearchResult{
				ID: chunk.ID, Path: chunk.Path, Source: chunk.Source,
				StartLine: chunk.StartLine, EndLine: chunk.EndLine,
				Snippet: chunk.Snippet, Score: score,
			})
		}
	}
	return results, nil
}

// buildSourceFilter 构建 SQL source 过滤条件。对齐 Python _build_source_filter
func (m *memoryIndexManager) buildSourceFilter() (string, []any) {
	sources := m.settings.Sources
	if len(sources) == 0 {
		return "1=0", nil
	}
	if len(sources) == 1 {
		return "source = ?", []any{sources[0]}
	}
	placeholders := make([]string, len(sources))
	args := make([]any, len(sources))
	for i, s := range sources {
		placeholders[i] = "?"
		args[i] = s
	}
	return fmt.Sprintf("source IN (%s)", strings.Join(placeholders, ",")), args
}

// mergeHybridResults 混合搜索结果合并。对齐 Python _merge_hybrid_results
func mergeHybridResults(vectorResults, keywordResults []SearchResult, vectorWeight, textWeight float64) []SearchResult {
	// 用 id 做合并键
	type mergeEntry struct {
		result      SearchResult
		vectorScore float64
		textScore   float64
	}
	byID := make(map[string]*mergeEntry)

	for _, r := range vectorResults {
		e := &mergeEntry{result: r, vectorScore: r.Score, textScore: 0.0}
		byID[r.ID] = e
	}

	for _, r := range keywordResults {
		if existing, exists := byID[r.ID]; exists {
			existing.textScore = r.Score
		} else {
			e := &mergeEntry{result: r, vectorScore: 0.0, textScore: r.Score}
			byID[r.ID] = e
		}
	}

	results := make([]SearchResult, 0, len(byID))
	for _, e := range byID {
		e.result.Score = vectorWeight*e.vectorScore + textWeight*e.textScore
		results = append(results, e.result)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// ReadFile 读取记忆文件内容。对齐 Python MemoryIndexManager.read_file
func (m *memoryIndexManager) ReadFile(ctx context.Context, relPath string, fromLine *int, lines *int) (*ReadFileResult, error) {
	var fullPath string
	if filepath.IsAbs(relPath) {
		fullPath = relPath
	} else if relPath == "USER.md" {
		userPath := m.workspace.GetNodePath("USER.md")
		if userPath != nil {
			fullPath = *userPath
		} else {
			fullPath = filepath.Join(m.memoryDir, relPath)
		}
	} else {
		fullPath = filepath.Join(m.memoryDir, relPath)
	}

	var content string
	if m.sysOperation != nil {
		readResult, err := m.sysOperation.Fs().ReadFile(ctx, fullPath)
		if err != nil {
			return nil, fmt.Errorf("读取文件失败: %w", err)
		}
		if readResult.Data != nil {
			content = readResult.Data.Content
		}
	} else {
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("读取文件失败: %w", err)
		}
		content = string(data)
	}

	allLines := strings.Split(content, "\n")
	totalLines := len(allLines)

	var contentLines []string
	if fromLine != nil {
		start := max(0, *fromLine-1)
		end := totalLines
		if lines != nil {
			end = min(totalLines, start+*lines)
		}
		contentLines = allLines[start:end]
	} else {
		contentLines = allLines
	}

	fromLineVal := 1
	if fromLine != nil {
		fromLineVal = *fromLine
	}

	return &ReadFileResult{
		Path:       relPath,
		Text:       strings.Join(contentLines, "\n"),
		TotalLines: totalLines,
		FromLine:   fromLineVal,
		ToLine:     fromLineVal + len(contentLines) - 1,
	}, nil
}

// Status 返回系统状态报告。对齐 Python MemoryIndexManager.status
func (m *memoryIndexManager) Status() *StatusResult {
	if m.db == nil {
		return &StatusResult{Available: false}
	}

	var fileCount, chunkCount int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&fileCount)
	_ = m.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunkCount)

	// 按来源统计
	sourceCounts := make([]SourceCount, 0)
	rows, err := m.db.Query("SELECT source, COUNT(*) as files FROM files GROUP BY source")
	if err == nil {
		for rows.Next() {
			var source string
			var files int
			if rows.Scan(&source, &files) == nil {
				sourceCounts = append(sourceCounts, SourceCount{Source: source, Files: files})
			}
		}
		_ = rows.Close()
	}

	// 缓存条目数
	var cacheEntries int
	_ = m.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", embeddingCacheTable)).Scan(&cacheEntries)

	ftsEnabled := true
	if v, ok := m.settings.Store["fts"].(map[string]any); ok {
		if v2, ok := v["enabled"].(bool); ok {
			ftsEnabled = v2
		}
	}
	vecEnabled := true
	if v, ok := m.settings.Store["vector"].(map[string]any); ok {
		if v2, ok := v["enabled"].(bool); ok {
			vecEnabled = v2
		}
	}
	cacheEnabled := true
	if v, ok := m.settings.Cache["enabled"].(bool); ok {
		cacheEnabled = v
	}

	return &StatusResult{
		Available:    true,
		Provider:     m.settings.Provider,
		Model:        m.settings.Model,
		Files:        fileCount,
		Chunks:       chunkCount,
		SourceCounts: sourceCounts,
		Dirty:        m.dirty,
		FTS: FTSStatus{
			Enabled:   ftsEnabled,
			Available: m.ftsAvailable,
			Error:     m.ftsError,
		},
		Vector: VectorStatus{
			Enabled:   vecEnabled,
			Available: m.vectorAvailable,
			Error:     m.vectorError,
			Dims:      m.vectorDims,
		},
		Cache: CacheStatus{
			Enabled: cacheEnabled,
			Entries: cacheEntries,
		},
	}
}

// IsClosed 检查管理器是否已关闭
func (m *memoryIndexManager) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// Close 关闭管理器。对齐 Python MemoryIndexManager.close
func (m *memoryIndexManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true

	// 停止定时同步
	if m.intervalCancel != nil {
		m.intervalCancel()
	}

	// 停止文件监听
	if m.watcher != nil {
		_ = m.watcher.Close()
	}

	// 关闭数据库
	if m.db != nil {
		_ = m.db.Close()
	}

	// 从缓存中移除
	cacheKey := fmt.Sprintf("%s:%s:%s", m.agentID, m.nodeName, m.memoryDir)
	indexCache.Delete(cacheKey)

	logger.Info(logger.ComponentCommon).Str("agent_id", m.agentID).Msg("记忆管理器已关闭")
	return nil
}

// setupFileWatcher 设置文件监听。对齐 Python _setup_file_watcher
func (m *memoryIndexManager) setupFileWatcher() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Warn(logger.ComponentCommon).Err(err).Msg("创建文件监听器失败")
		return
	}
	m.watcher = watcher

	// 监听路径
	watchPaths := make(map[string]bool)
	if m.memoryDir != "" {
		if _, err := os.Stat(m.memoryDir); err == nil {
			watchPaths[m.memoryDir] = true
		}
	}

	// 添加额外路径
	for _, ep := range m.settings.ExtraPaths {
		fullPath := filepath.Join(m.memoryDir, ep)
		if _, err := os.Stat(fullPath); err == nil {
			watchPaths[fullPath] = true
		}
	}

	for p := range watchPaths {
		if err := watcher.Add(p); err != nil {
			logger.Warn(logger.ComponentCommon).Err(err).Str("path", p).Msg("添加监听路径失败")
		}
	}

	// 启动监听 goroutine
	go m.watchLoop()

	// 延迟 1 秒后标记为已初始化，避免初始扫描触发
	time.AfterFunc(1*time.Second, func() {
		m.watcherInitialized = true
		logger.Debug(logger.ComponentCommon).Int("watch_paths", len(watchPaths)).Msg("文件监听器已初始化")
	})
}

// watchLoop 监听循环
func (m *memoryIndexManager) watchLoop() {
	for {
		select {
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			if !m.watcherInitialized {
				continue
			}
			if IsMemoryPath(event.Name) {
				m.scheduleWatchSync()
			}
		case _, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// scheduleWatchSync 防抖同步。对齐 Python schedule_watch_sync
func (m *memoryIndexManager) scheduleWatchSync() {
	m.dirty = true
	m.watchMu.Lock()
	defer m.watchMu.Unlock()

	debounceMs := 2000
	if v, ok := m.settings.Sync["watchDebounceMs"].(int); ok {
		debounceMs = v
	}

	if m.watchTimer != nil {
		m.watchTimer.Stop()
	}
	m.watchTimer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, func() {
		if m.closed {
			return
		}
		if err := m.Sync(context.Background(), "watch", false); err != nil {
			logger.Warn(logger.ComponentCommon).Err(err).Msg("文件监听同步失败")
		}
	})
}

// ensureIntervalSync 设置定时同步。对齐 Python _ensure_interval_sync
func (m *memoryIndexManager) ensureIntervalSync() {
	minutes := 0
	if v, ok := m.settings.Sync["intervalMinutes"].(int); ok {
		minutes = v
	}
	if minutes <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.intervalCancel = cancel

	go func() {
		ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if m.closed {
					return
				}
				if err := m.Sync(ctx, "interval", false); err != nil {
					logger.Warn(logger.ComponentCommon).Err(err).Msg("定时同步失败")
				}
			}
		}
	}()

	logger.Info(logger.ComponentCommon).Int("interval_minutes", minutes).Msg("定时同步已启用")
}

// writeMeta 写入元数据。对齐 Python _write_meta
func (m *memoryIndexManager) writeMeta(meta map[string]any) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", metaKey, string(data))
	return err
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// vectorToBlob 向量转二进制。对齐 Python vector_to_blob
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

// blobToVector 二进制转向量。对齐 Python blob_to_vector
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

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// containsSource 检查 sources 列表是否包含指定来源
func containsSource(sources []string, source string) bool {
	for _, s := range sources {
		if s == source {
			return true
		}
	}
	return false
}
