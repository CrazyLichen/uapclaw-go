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

type BaseFsOperation struct {
	BaseOperation
}

// ──────────────────────────── 枚举 ────────────────────────────

type FsOption func(*FsOptions)

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

func WithFsMode(mode string) FsOption {
	return func(o *FsOptions) { o.Mode = mode }
}

func WithFsHead(head int) FsOption {
	return func(o *FsOptions) { o.Head = head }
}

func WithFsTail(tail int) FsOption {
	return func(o *FsOptions) { o.Tail = tail }
}

func WithFsEncoding(encoding string) FsOption {
	return func(o *FsOptions) { o.Encoding = encoding }
}

func WithFsChunkSize(chunkSize int) FsOption {
	return func(o *FsOptions) { o.ChunkSize = chunkSize }
}

func WithFsLineRange(start, end int) FsOption {
	return func(o *FsOptions) { o.LineRange = [2]int{start, end} }
}

func WithFsPrependNewline(v bool) FsOption {
	return func(o *FsOptions) { o.PrependNewline = &v }
}

func WithFsAppendNewline(v bool) FsOption {
	return func(o *FsOptions) { o.AppendNewline = &v }
}

func WithFsAppend(append bool) FsOption {
	return func(o *FsOptions) { o.Append = append }
}

func WithFsCreateIfNotExist(create bool) FsOption {
	return func(o *FsOptions) { o.CreateIfNotExist = create }
}

func WithFsPermissions(perm string) FsOption {
	return func(o *FsOptions) { o.Permissions = perm }
}

func WithFsOverwrite(overwrite bool) FsOption {
	return func(o *FsOptions) { o.Overwrite = overwrite }
}

func WithFsCreateParentDirs(create bool) FsOption {
	return func(o *FsOptions) { o.CreateParentDirs = create }
}

func WithFsPreservePerms(preserve bool) FsOption {
	return func(o *FsOptions) { o.PreservePerms = preserve }
}

func WithFsRecursive(recursive bool) FsOption {
	return func(o *FsOptions) { o.Recursive = recursive }
}

func WithFsMaxDepth(maxDepth int) FsOption {
	return func(o *FsOptions) { o.MaxDepth = maxDepth }
}

func WithFsSortBy(sortBy string) FsOption {
	return func(o *FsOptions) { o.SortBy = sortBy }
}

func WithFsSortDescending(desc bool) FsOption {
	return func(o *FsOptions) { o.SortDescending = desc }
}

func WithFsFileTypes(fileTypes []string) FsOption {
	return func(o *FsOptions) { o.FileTypes = fileTypes }
}

func WithFsExcludePatterns(patterns []string) FsOption {
	return func(o *FsOptions) { o.ExcludePatterns = patterns }
}

func WithFsOptions(options map[string]any) FsOption {
	return func(o *FsOptions) { o.Options = options }
}

func (b *BaseFsOperation) ReadFile(_ context.Context, _ string, _ ...FsOption) (*result.ReadFileResult, error) {
	return nil, fmt.Errorf("未实现: ReadFile")
}

func (b *BaseFsOperation) ReadFileStream(_ context.Context, _ string, _ ...FsOption) (<-chan result.ReadFileStreamResult, error) {
	return nil, fmt.Errorf("未实现: ReadFileStream")
}

func (b *BaseFsOperation) WriteFile(_ context.Context, _ string, _ string, _ ...FsOption) (*result.WriteFileResult, error) {
	return nil, fmt.Errorf("未实现: WriteFile")
}

func (b *BaseFsOperation) UploadFile(_ context.Context, _ string, _ string, _ ...FsOption) (*result.UploadFileResult, error) {
	return nil, fmt.Errorf("未实现: UploadFile")
}

func (b *BaseFsOperation) UploadFileStream(_ context.Context, _ string, _ string, _ ...FsOption) (<-chan result.UploadFileStreamResult, error) {
	return nil, fmt.Errorf("未实现: UploadFileStream")
}

func (b *BaseFsOperation) DownloadFile(_ context.Context, _ string, _ string, _ ...FsOption) (*result.DownloadFileResult, error) {
	return nil, fmt.Errorf("未实现: DownloadFile")
}

func (b *BaseFsOperation) DownloadFileStream(_ context.Context, _ string, _ string, _ ...FsOption) (<-chan result.DownloadFileStreamResult, error) {
	return nil, fmt.Errorf("未实现: DownloadFileStream")
}

func (b *BaseFsOperation) ListFiles(_ context.Context, _ string, _ ...FsOption) (*result.ListFilesResult, error) {
	return nil, fmt.Errorf("未实现: ListFiles")
}

func (b *BaseFsOperation) ListDirectories(_ context.Context, _ string, _ ...FsOption) (*result.ListDirsResult, error) {
	return nil, fmt.Errorf("未实现: ListDirectories")
}

func (b *BaseFsOperation) SearchFiles(_ context.Context, _ string, _ string, _ ...FsOption) (*result.SearchFilesResult, error) {
	return nil, fmt.Errorf("未实现: SearchFiles")
}

func (b *BaseFsOperation) ListTools() []*tool.ToolCard { return nil }
