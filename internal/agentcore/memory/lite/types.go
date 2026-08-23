package lite

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryChunk 记忆分块。对齐 Python MemoryChunk
type MemoryChunk struct {
	// Text 分块文本内容
	Text string
	// StartLine 起始行号
	StartLine int
	// EndLine 结束行号
	EndLine int
}

// SearchResult 搜索结果条目。对齐 Python MemoryIndexManager.search 返回的 dict
type SearchResult struct {
	// ID 分块标识
	ID string
	// Path 文件相对路径
	Path string
	// Source 来源
	Source string
	// StartLine 起始行号
	StartLine int
	// EndLine 结束行号
	EndLine int
	// Snippet 内容片段
	Snippet string
	// Score 相似度分数
	Score float64
	// Citation 引用标记（如 path#L1-L10）
	Citation string
}

// FileEntry 文件索引条目。对齐 Python build_file_entry 返回的 dict
type FileEntry struct {
	// Path 文件相对路径
	Path string
	// AbsPath 文件绝对路径
	AbsPath string
	// Hash 文件内容哈希
	Hash string
	// MtimeMs 最后修改时间（毫秒）
	MtimeMs int64
	// Size 文件大小（字节）
	Size int64
}

// ChunkData 搜索中间态 chunk 数据。对齐 Python searchVector/searchKeyword 内部 chunkMap
type ChunkData struct {
	// ID 分块标识
	ID string
	// Path 文件相对路径
	Path string
	// Source 来源
	Source string
	// StartLine 起始行号
	StartLine int
	// EndLine 结束行号
	EndLine int
	// Snippet 内容片段
	Snippet string
}

// ReadFileResult 读取记忆文件结果。对齐 Python MemoryIndexManager.read_file 返回的 dict
type ReadFileResult struct {
	// Path 文件路径
	Path string
	// Text 文件内容
	Text string
	// TotalLines 总行数
	TotalLines int
	// FromLine 起始行号
	FromLine int
	// ToLine 结束行号
	ToLine int
}

// StatusResult 记忆系统状态。对齐 Python MemoryIndexManager.status 返回的 dict
type StatusResult struct {
	// Available 是否可用
	Available bool
	// Provider 提供者名称
	Provider string
	// Model 模型名称
	Model string
	// Files 文件数量
	Files int
	// Chunks 分块数量
	Chunks int
	// SourceCounts 按来源统计
	SourceCounts []SourceCount
	// Dirty 是否有未同步的变更
	Dirty bool
	// FTS 全文搜索状态
	FTS FTSStatus
	// Vector 向量搜索状态
	Vector VectorStatus
	// Cache 缓存状态
	Cache CacheStatus
}

// SourceCount 按来源统计
type SourceCount struct {
	// Source 来源名称
	Source string
	// Files 文件数量
	Files int
}

// FTSStatus 全文搜索状态
type FTSStatus struct {
	// Enabled 是否启用
	Enabled bool
	// Available 是否可用
	Available bool
	// Error 错误信息
	Error string
}

// VectorStatus 向量搜索状态
type VectorStatus struct {
	// Enabled 是否启用
	Enabled bool
	// Available 是否可用
	Available bool
	// Error 错误信息
	Error string
	// Dims 向量维度
	Dims *int
}

// CacheStatus 缓存状态
type CacheStatus struct {
	// Enabled 是否启用
	Enabled bool
	// Entries 缓存条目数
	Entries int
}

// MemorySearchResult 通用记忆搜索结果。对齐 Python memory_search_with_context 返回的 dict
type MemorySearchResult struct {
	// Results 搜索结果列表
	Results []SearchResult
	// Disabled 是否禁用
	Disabled bool
	// Error 错误信息
	Error string
	// Provider 提供者名称
	Provider string
	// Model 模型名称
	Model string
}

// MemoryGetResult 通用记忆获取结果。对齐 Python memory_get_with_context 返回的 dict
type MemoryGetResult struct {
	// Path 文件路径
	Path string
	// Text 文件内容
	Text string
	// Disabled 是否禁用
	Disabled bool
	// Error 错误信息
	Error string
}

// ReadMemoryResult 通用记忆读取结果。对齐 Python read_memory_with_context 返回的 dict
type ReadMemoryResult struct {
	// Success 是否成功
	Success bool
	// Path 文件路径
	Path string
	// Content 文件内容
	Content string
	// TotalLines 总行数
	TotalLines int
	// StartLine 起始行号
	StartLine int
	// EndLine 结束行号
	EndLine int
	// Truncated 是否截断
	Truncated bool
	// Error 错误信息
	Error string
}

// WriteMemoryResult 通用记忆写入结果。对齐 Python write_memory_with_context 返回的 dict
type WriteMemoryResult struct {
	// Success 是否成功
	Success bool
	// Path 文件路径
	Path string
	// FullPath 完整路径
	FullPath string
	// Appended 是否追加
	Appended bool
	// FileExisted 文件是否已存在
	FileExisted bool
	// Error 错误信息
	Error string
}

// EditMemoryResult 通用记忆编辑结果。对齐 Python edit_memory_with_context 返回的 dict
type EditMemoryResult struct {
	// Success 是否成功
	Success bool
	// Path 文件路径
	Path string
	// Replaced 被替换的文本
	Replaced string
	// NewText 新文本
	NewText string
	// Error 错误信息
	Error string
}

// CodingReadResult 编程记忆读取结果。对齐 Python coding_memory_read_with_context 返回的 dict
type CodingReadResult struct {
	// Success 是否成功
	Success bool
	// Path 文件路径
	Path string
	// Content 文件内容
	Content string
	// TotalLines 总行数
	TotalLines int
	// StartLine 起始行号
	StartLine int
	// EndLine 结束行号
	EndLine int
	// Truncated 是否截断
	Truncated bool
	// Error 错误信息
	Error string
}

// CodingEditResult 编程记忆编辑结果。对齐 Python coding_memory_edit_with_context 返回的 dict
type CodingEditResult struct {
	// Success 是否成功
	Success bool
	// Path 文件路径
	Path string
	// NewContent 新内容
	NewContent string
	// Error 错误信息
	Error string
}

// ConflictResult 冲突检测结果。对齐 Python _prepare_append_mode / _search_similar 返回的 dict
type ConflictResult struct {
	// ConflictDetected 是否检测到冲突
	ConflictDetected bool
	// ConflictingFiles 冲突文件列表
	ConflictingFiles []string
	// Note 备注
	Note string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
