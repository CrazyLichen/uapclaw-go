package memory

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySearchResult 记忆搜索结果。对齐 Python MemorySearchResult（types.py）；
// Search 返回列表元素（citation 由下游 memory_search 工具填充，manager 层恒为 nil）
type MemorySearchResult struct {
	// ID chunk 标识
	ID string `json:"id"`
	// Path 相对路径
	Path string `json:"path"`
	// Source 来源（memory/sessions）
	Source string `json:"source"`
	// StartLine 起始行
	StartLine int `json:"startLine"`
	// EndLine 结束行
	EndLine int `json:"endLine"`
	// Snippet 文本摘要
	Snippet string `json:"snippet"`
	// Score 相关度分数
	Score float64 `json:"score"`
	// Citation 引用标注（下游 memory_search 填充为 path#L{start}[-L{end}]）
	Citation *string `json:"citation,omitempty"`
}

// SearchOpts 搜索选项。对齐 Python search(query, opts: Dict) 实际读取的键：
// 仅 minScore/maxResults 两键生效（hybrid 取自 settings.query.hybrid；
// Python 调用方传入的 sessionKey 在 manager.py 中从未被读取）。
// 指针字段保真 Python "minScore" in opts 判定——显式 0.0 与未提供可区分
type SearchOpts struct {
	// MinScore 最小相关度分数（nil 时回退 settings.query.minScore）
	MinScore *float64
	// MaxResults 最大结果数（nil 时回退 settings.query.maxResults）
	MaxResults *int
}

// MemoryProviderStatus 嵌入提供者状态。对齐 Python MemoryProviderStatus（types.py）
type MemoryProviderStatus struct {
	// Available 是否可用
	Available bool `json:"available"`
	// Provider 提供者 ID
	Provider *string `json:"provider,omitempty"`
	// Model 模型名
	Model *string `json:"model,omitempty"`
	// Error 错误信息
	Error *string `json:"error,omitempty"`
}

// MemorySyncProgressUpdate 同步进度更新。对齐 Python MemorySyncProgressUpdate（types.py）
type MemorySyncProgressUpdate struct {
	// Phase 阶段
	Phase string `json:"phase"`
	// Current 当前
	Current int `json:"current"`
	// Total 总数
	Total int `json:"total"`
	// Message 消息
	Message *string `json:"message,omitempty"`
}

// FileEntry 文件条目。对齐 Python FileEntry（types.py，MemoryFileEntry/SessionFileEntry 为同结构别名）
type FileEntry struct {
	// Path 相对路径
	Path string `json:"path"`
	// AbsPath 绝对路径
	AbsPath string `json:"absPath"`
	// Hash 内容 hash
	Hash string `json:"hash"`
	// MtimeMs 修改时间毫秒
	MtimeMs int64 `json:"mtimeMs"`
	// Size 文件大小
	Size int64 `json:"size"`
}

// MemoryChunk 记忆分块。对齐 Python MemoryChunk（types.py）
type MemoryChunk struct {
	// Text 文本内容
	Text string `json:"text"`
	// StartLine 起始行
	StartLine int `json:"startLine"`
	// EndLine 结束行
	EndLine int `json:"endLine"`
}

// FtsStatus FTS5 全文搜索状态。对齐 Python FtsStatus（types.py）
type FtsStatus struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// Available 是否可用
	Available bool `json:"available"`
	// Error 错误信息
	Error *string `json:"error,omitempty"`
}

// VectorStatus 向量搜索状态。对齐 Python VectorStatus（types.py）
type VectorStatus struct {
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
	// Available 是否可用
	Available bool `json:"available"`
	// Error 错误信息
	Error *string `json:"error,omitempty"`
	// Dims 维度
	Dims *int `json:"dims,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 记忆来源常量。对齐 Python MemorySource Literal["memory", "sessions"]（types.py）
const (
	// MemorySourceMemory 记忆文件来源
	MemorySourceMemory = "memory"
	// MemorySourceSessions 会话记录来源
	MemorySourceSessions = "sessions"
)
