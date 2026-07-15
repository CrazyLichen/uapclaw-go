package result

// ──────────────────────────── 结构体 ────────────────────────────

// ReadFileData 读取文件结果数据。
// 对齐 Python ReadFileData：path, content, mode。
type ReadFileData struct {
	// Path 文件路径
	Path string `json:"path"`
	// Content 文件内容（文本或二进制 base64）
	Content string `json:"content"`
	// Mode 读取模式：text / bytes
	Mode string `json:"mode"`
}

// ReadFileResult 读取文件结果
type ReadFileResult struct {
	BaseResult
	// Data 结果数据
	Data *ReadFileData `json:"data"`
}

// ReadFileChunkData 读取文件流式块数据。
// 对齐 Python ReadFileChunkData：path, chunk_content, mode, chunk_size, chunk_index, is_last_chunk。
type ReadFileChunkData struct {
	// Path 文件路径
	Path string `json:"path"`
	// ChunkContent 当前块内容
	ChunkContent string `json:"chunk_content"`
	// Mode 读取模式：text / bytes
	Mode string `json:"mode"`
	// ChunkSize 块大小（字节）
	ChunkSize int `json:"chunk_size"`
	// ChunkIndex 块索引
	ChunkIndex int `json:"chunk_index"`
	// IsLastChunk 是否为最后一块
	IsLastChunk bool `json:"is_last_chunk"`
}

// ReadFileStreamResult 读取文件流式结果
type ReadFileStreamResult struct {
	BaseResult
	// Data 流式块数据
	Data *ReadFileChunkData `json:"data"`
}

// WriteFileData 写入文件结果数据。
// 对齐 Python WriteFileData：path, size, mode。
type WriteFileData struct {
	// Path 文件路径
	Path string `json:"path"`
	// Size 写入大小（字节）
	Size int `json:"size"`
	// Mode 写入模式：text / bytes
	Mode string `json:"mode"`
}

// WriteFileResult 写入文件结果
type WriteFileResult struct {
	BaseResult
	// Data 结果数据
	Data *WriteFileData `json:"data"`
}

// UploadFileData 上传文件结果数据。
// 对齐 Python UploadFileData：local_path, target_path, size。
type UploadFileData struct {
	// LocalPath 本地源文件路径
	LocalPath string `json:"local_path"`
	// TargetPath 目标文件路径
	TargetPath string `json:"target_path"`
	// Size 文件大小（字节）
	Size int `json:"size"`
}

// UploadFileResult 上传文件结果
type UploadFileResult struct {
	BaseResult
	// Data 结果数据
	Data *UploadFileData `json:"data"`
}

// UploadFileChunkData 上传文件流式块数据。
// 对齐 Python UploadFileChunkData：local_path, target_path, chunk_size, chunk_index, is_last_chunk。
type UploadFileChunkData struct {
	// LocalPath 本地源文件路径
	LocalPath string `json:"local_path"`
	// TargetPath 目标文件路径
	TargetPath string `json:"target_path"`
	// ChunkSize 块大小（字节）
	ChunkSize int `json:"chunk_size"`
	// ChunkIndex 块索引
	ChunkIndex int `json:"chunk_index"`
	// IsLastChunk 是否为最后一块
	IsLastChunk bool `json:"is_last_chunk"`
}

// UploadFileStreamResult 上传文件流式结果
type UploadFileStreamResult struct {
	BaseResult
	// Data 流式块数据
	Data *UploadFileChunkData `json:"data"`
}

// DownloadFileData 下载文件结果数据。
// 对齐 Python DownloadFileData：source_path, local_path, size。
type DownloadFileData struct {
	// SourcePath 源文件路径
	SourcePath string `json:"source_path"`
	// LocalPath 本地目标文件路径
	LocalPath string `json:"local_path"`
	// Size 文件大小（字节）
	Size int `json:"size"`
}

// DownloadFileResult 下载文件结果
type DownloadFileResult struct {
	BaseResult
	// Data 结果数据
	Data *DownloadFileData `json:"data"`
}

// DownloadFileChunkData 下载文件流式块数据。
// 对齐 Python DownloadFileChunkData：source_path, local_path, chunk_size, chunk_index, is_last_chunk。
type DownloadFileChunkData struct {
	// SourcePath 源文件路径
	SourcePath string `json:"source_path"`
	// LocalPath 本地目标文件路径
	LocalPath string `json:"local_path"`
	// ChunkSize 块大小（字节）
	ChunkSize int `json:"chunk_size"`
	// ChunkIndex 块索引
	ChunkIndex int `json:"chunk_index"`
	// IsLastChunk 是否为最后一块
	IsLastChunk bool `json:"is_last_chunk"`
}

// DownloadFileStreamResult 下载文件流式结果
type DownloadFileStreamResult struct {
	BaseResult
	// Data 流式块数据
	Data *DownloadFileChunkData `json:"data"`
}

// FileSystemItem 文件/目录公共属性。
// 对齐 Python FileSystemItem：name, path, size, modified_time, is_directory, type。
type FileSystemItem struct {
	// Name 文件/目录名称
	Name string `json:"name"`
	// Path 完整绝对路径
	Path string `json:"path"`
	// Size 大小（字节）
	Size int64 `json:"size"`
	// ModifiedTime 最后修改时间
	ModifiedTime string `json:"modified_time"`
	// IsDirectory 是否为目录
	IsDirectory bool `json:"is_directory"`
	// Type 文件扩展名（仅文件有效）
	Type *string `json:"type,omitempty"`
}

// FileSystemData 文件列表/目录列表数据。
// 对齐 Python FileSystemData：total_count, list_items, root_path, recursive, max_depth。
type FileSystemData struct {
	// TotalCount 总数量
	TotalCount int `json:"total_count"`
	// ListItems 文件/目录详情列表
	ListItems []FileSystemItem `json:"list_items"`
	// RootPath 根路径
	RootPath string `json:"root_path"`
	// Recursive 是否递归
	Recursive bool `json:"recursive"`
	// MaxDepth 最大递归深度
	MaxDepth *int `json:"max_depth,omitempty"`
}

// ListFilesResult 列出文件结果
type ListFilesResult struct {
	BaseResult
	// Data 结果数据
	Data *FileSystemData `json:"data"`
}

// ListDirsResult 列出目录结果
type ListDirsResult struct {
	BaseResult
	// Data 结果数据
	Data *FileSystemData `json:"data"`
}

// SearchFilesData 搜索文件结果数据。
// 对齐 Python SearchFilesData：total_matches, matching_files, search_path, search_pattern, exclude_patterns。
type SearchFilesData struct {
	// TotalMatches 匹配文件总数
	TotalMatches int `json:"total_matches"`
	// MatchingFiles 匹配文件列表
	MatchingFiles []FileSystemItem `json:"matching_files"`
	// SearchPath 搜索路径
	SearchPath string `json:"search_path"`
	// SearchPattern 搜索模式
	SearchPattern string `json:"search_pattern"`
	// ExcludePatterns 排除模式
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
}

// SearchFilesResult 搜索文件结果
type SearchFilesResult struct {
	BaseResult
	// Data 结果数据
	Data *SearchFilesData `json:"data"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
