package sys_operation

import (
	"context"
	"fmt"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FsOperation 文件系统操作接口，定义读取、写入、列表、搜索等文件系统操作。
// 对齐 Python BaseFsOperation：read_file, read_file_stream, write_file, upload_file,
// 对齐 Python 方法：upload_file_stream, download_file, download_file_stream, list_files, list_directories, search_files。
type FsOperation interface {
	// ReadFile 读取文件内容
	ReadFile(ctx context.Context, path string, opts ...FsOption) (*result.ReadFileResult, error)
	// ReadFileStream 流式读取文件
	ReadFileStream(ctx context.Context, path string, opts ...FsOption) (<-chan result.ReadFileStreamResult, error)
	// WriteFile 写入文件内容
	WriteFile(ctx context.Context, path string, content string, opts ...FsOption) (*result.WriteFileResult, error)
	// UploadFile 上传文件
	UploadFile(ctx context.Context, localPath string, targetPath string, opts ...FsOption) (*result.UploadFileResult, error)
	// UploadFileStream 流式上传文件
	UploadFileStream(ctx context.Context, localPath string, targetPath string, opts ...FsOption) (<-chan result.UploadFileStreamResult, error)
	// DownloadFile 下载文件
	DownloadFile(ctx context.Context, sourcePath string, localPath string, opts ...FsOption) (*result.DownloadFileResult, error)
	// DownloadFileStream 流式下载文件
	DownloadFileStream(ctx context.Context, sourcePath string, localPath string, opts ...FsOption) (<-chan result.DownloadFileStreamResult, error)
	// ListFiles 列出目录下文件
	ListFiles(ctx context.Context, path string, opts ...FsOption) (*result.ListFilesResult, error)
	// ListDirectories 列出目录下子目录
	ListDirectories(ctx context.Context, path string, opts ...FsOption) (*result.ListDirsResult, error)
	// SearchFiles 搜索匹配模式的文件
	SearchFiles(ctx context.Context, path string, pattern string, opts ...FsOption) (*result.SearchFilesResult, error)
	// ListTools 返回文件系统操作的工具卡片列表
	ListTools() []*tool.ToolCard
}

// FsOption 文件系统操作选项函数
type FsOption func(*FsOptions)

// FsOptions 文件系统操作选项。
// 对齐 Python 所有 fs 方法签名的参数并集。
type FsOptions struct {
	// Mode 读取/写入模式：text / bytes
	Mode string
	// Head 头部行数
	Head int
	// Tail 尾部行数
	Tail int
	// LineRange 行范围 [start, end]（1-indexed, inclusive）
	LineRange [2]int
	// Encoding 字符编码
	Encoding string
	// ChunkSize 块大小（字节）
	ChunkSize int
	// Content 写入内容（WriteFile 时使用）
	Content string
	// PrependNewline 写入前添加换行符（text 模式，默认 true）
	PrependNewline *bool
	// AppendNewline 写入后添加换行符（text 模式，默认 false）
	AppendNewline *bool
	// Append 追加写入
	Append bool
	// CreateIfNotExist 不存在时自动创建（默认 true）
	CreateIfNotExist bool
	// Permissions 文件权限（Unix/Linux，默认 "644"）
	Permissions string
	// LocalPath 本地文件路径（Upload/Download 时使用）
	LocalPath string
	// TargetPath 目标文件路径（Upload 时使用）
	TargetPath string
	// SourcePath 源文件路径（Download 时使用）
	SourcePath string
	// Overwrite 覆盖已存在文件
	Overwrite bool
	// CreateParentDirs 自动创建父目录
	CreateParentDirs bool
	// PreservePerms 保留文件权限
	PreservePerms bool
	// Recursive 递归遍历
	Recursive bool
	// MaxDepth 最大递归深度
	MaxDepth int
	// SortBy 排序字段：name / modified_time / size
	SortBy string
	// SortDescending 降序排序
	SortDescending bool
	// FileTypes 文件类型过滤（扩展名列表）
	FileTypes []string
	// ExcludePatterns 排除模式
	ExcludePatterns []string
	// Options 扩展配置选项
	Options map[string]any
}

// BaseFsOperation FsOperation 的空操作桩实现
type BaseFsOperation struct {
	BaseOperation
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultReadChunkSize 默认读取块大小
	DefaultReadChunkSize = 0
	// DefaultReadStreamChunkSize 默认流式读取块大小
	DefaultReadStreamChunkSize = 8192
	// DefaultUploadChunkSize 默认上传块大小
	DefaultUploadChunkSize = 0
	// DefaultUploadStreamChunkSize 默认流式上传块大小
	DefaultUploadStreamChunkSize = 1024 * 1024
	// DefaultDownloadChunkSize 默认下载块大小
	DefaultDownloadChunkSize = 0
	// DefaultDownloadStreamChunkSize 默认流式下载块大小
	DefaultDownloadStreamChunkSize = 1024 * 1024
	// TailChunkSize tail 读取块大小
	TailChunkSize = 1024
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFsOptions 从选项列表构造 FsOptions
func NewFsOptions(opts ...FsOption) *FsOptions {
	o := &FsOptions{
		Mode:             "text",
		Encoding:         "utf-8",
		CreateIfNotExist: true,
		Permissions:      "644",
		CreateParentDirs: true,
		PreservePerms:    true,
		SortBy:           "name",
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithFsMode 设置文件系统操作读取模式
func WithFsMode(mode string) FsOption {
	return func(o *FsOptions) { o.Mode = mode }
}

// WithFsHead 设置文件系统操作头部行数
func WithFsHead(head int) FsOption {
	return func(o *FsOptions) { o.Head = head }
}

// WithFsTail 设置文件系统操作尾部行数
func WithFsTail(tail int) FsOption {
	return func(o *FsOptions) { o.Tail = tail }
}

// WithFsEncoding 设置文件系统操作编码
func WithFsEncoding(encoding string) FsOption {
	return func(o *FsOptions) { o.Encoding = encoding }
}

// WithFsChunkSize 设置块大小
func WithFsChunkSize(chunkSize int) FsOption {
	return func(o *FsOptions) { o.ChunkSize = chunkSize }
}

// WithFsLineRange 设置行范围
func WithFsLineRange(start, end int) FsOption {
	return func(o *FsOptions) { o.LineRange = [2]int{start, end} }
}

// WithFsPrependNewline 设置写入前添加换行符
func WithFsPrependNewline(v bool) FsOption {
	return func(o *FsOptions) { o.PrependNewline = &v }
}

// WithFsAppendNewline 设置写入后添加换行符
func WithFsAppendNewline(v bool) FsOption {
	return func(o *FsOptions) { o.AppendNewline = &v }
}

// WithFsAppend 设置追加写入
func WithFsAppend(append bool) FsOption {
	return func(o *FsOptions) { o.Append = append }
}

// WithFsCreateIfNotExist 设置不存在时自动创建
func WithFsCreateIfNotExist(create bool) FsOption {
	return func(o *FsOptions) { o.CreateIfNotExist = create }
}

// WithFsPermissions 设置文件权限
func WithFsPermissions(perm string) FsOption {
	return func(o *FsOptions) { o.Permissions = perm }
}

// WithFsOverwrite 设置覆盖已存在文件
func WithFsOverwrite(overwrite bool) FsOption {
	return func(o *FsOptions) { o.Overwrite = overwrite }
}

// WithFsCreateParentDirs 设置自动创建父目录
func WithFsCreateParentDirs(create bool) FsOption {
	return func(o *FsOptions) { o.CreateParentDirs = create }
}

// WithFsPreservePerms 设置保留文件权限
func WithFsPreservePerms(preserve bool) FsOption {
	return func(o *FsOptions) { o.PreservePerms = preserve }
}

// WithFsRecursive 设置递归遍历
func WithFsRecursive(recursive bool) FsOption {
	return func(o *FsOptions) { o.Recursive = recursive }
}

// WithFsMaxDepth 设置最大递归深度
func WithFsMaxDepth(maxDepth int) FsOption {
	return func(o *FsOptions) { o.MaxDepth = maxDepth }
}

// WithFsSortBy 设置排序字段
func WithFsSortBy(sortBy string) FsOption {
	return func(o *FsOptions) { o.SortBy = sortBy }
}

// WithFsSortDescending 设置降序排序
func WithFsSortDescending(desc bool) FsOption {
	return func(o *FsOptions) { o.SortDescending = desc }
}

// WithFsFileTypes 设置文件类型过滤
func WithFsFileTypes(fileTypes []string) FsOption {
	return func(o *FsOptions) { o.FileTypes = fileTypes }
}

// WithFsExcludePatterns 设置排除模式
func WithFsExcludePatterns(patterns []string) FsOption {
	return func(o *FsOptions) { o.ExcludePatterns = patterns }
}

// WithFsOptions 设置扩展配置选项
func WithFsOptions(options map[string]any) FsOption {
	return func(o *FsOptions) { o.Options = options }
}

// ReadFile 读取文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) ReadFile(_ context.Context, _ string, _ ...FsOption) (*result.ReadFileResult, error) {
	return nil, fmt.Errorf("未实现: ReadFile")
}

// ReadFileStream 流式读取文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) ReadFileStream(_ context.Context, _ string, _ ...FsOption) (<-chan result.ReadFileStreamResult, error) {
	return nil, fmt.Errorf("未实现: ReadFileStream")
}

// WriteFile 写入文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) WriteFile(_ context.Context, _ string, _ string, _ ...FsOption) (*result.WriteFileResult, error) {
	return nil, fmt.Errorf("未实现: WriteFile")
}

// UploadFile 上传文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) UploadFile(_ context.Context, _ string, _ string, _ ...FsOption) (*result.UploadFileResult, error) {
	return nil, fmt.Errorf("未实现: UploadFile")
}

// UploadFileStream 流式上传文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) UploadFileStream(_ context.Context, _ string, _ string, _ ...FsOption) (<-chan result.UploadFileStreamResult, error) {
	return nil, fmt.Errorf("未实现: UploadFileStream")
}

// DownloadFile 下载文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) DownloadFile(_ context.Context, _ string, _ string, _ ...FsOption) (*result.DownloadFileResult, error) {
	return nil, fmt.Errorf("未实现: DownloadFile")
}

// DownloadFileStream 流式下载文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) DownloadFileStream(_ context.Context, _ string, _ string, _ ...FsOption) (<-chan result.DownloadFileStreamResult, error) {
	return nil, fmt.Errorf("未实现: DownloadFileStream")
}

// ListFiles 列出文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) ListFiles(_ context.Context, _ string, _ ...FsOption) (*result.ListFilesResult, error) {
	return nil, fmt.Errorf("未实现: ListFiles")
}

// ListDirectories 列出目录（BaseFsOperation 空实现）
func (b *BaseFsOperation) ListDirectories(_ context.Context, _ string, _ ...FsOption) (*result.ListDirsResult, error) {
	return nil, fmt.Errorf("未实现: ListDirectories")
}

// SearchFiles 搜索文件（BaseFsOperation 空实现）
func (b *BaseFsOperation) SearchFiles(_ context.Context, _ string, _ string, _ ...FsOption) (*result.SearchFilesResult, error) {
	return nil, fmt.Errorf("未实现: SearchFiles")
}

// ListTools 返回工具卡片列表（BaseFsOperation 空实现）
func (b *BaseFsOperation) ListTools() []*tool.ToolCard { return nil }
