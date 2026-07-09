package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FsOperation 本地文件系统操作。
// 对齐 Python local/fs_operation.py FsOperation。
type FsOperation struct {
	sysop.BaseFsOperation
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	fsLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var _ sysop.FsOperation = (*FsOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFsOperation 创建本地文件系统操作实例（工厂函数，供 OperationRegistry 调用）。
func NewFsOperation(runConfig any) sysop.SysSubOperation {
	return &FsOperation{}
}

// ReadFile 读取文件。
// 对齐 Python FsOperation.read_file。
func (f *FsOperation) ReadFile(ctx context.Context, path string, opts ...sysop.FsOption) (*result.ReadFileResult, error) {
	o := sysop.NewFsOptions(opts...)
	methodName := "read_file"

	startTime := time.Now()
	logger.Info(fsLogComponent).Str("method_name", methodName).Str("path", path).Msg("开始读取文件")

	resolvedPath, err := f.resolvePath(path, false)
	if err != nil {
		return f.createErrorResult(methodName, err.Error(), startTime), nil
	}

	// 检查文件是否存在
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return f.createErrorResult(methodName, fmt.Sprintf("File not found: %s", resolvedPath), startTime), nil
	}
	if info.IsDir() {
		return f.createErrorResult(methodName, fmt.Sprintf("Path is a directory: %s", resolvedPath), startTime), nil
	}

	// 读取文件内容
	var content []byte
	if o.Mode == "bytes" {
		content, err = os.ReadFile(resolvedPath)
	} else {
		content, err = os.ReadFile(resolvedPath)
	}
	if err != nil {
		return f.createErrorResult(methodName, err.Error(), startTime), nil
	}

	// 行范围处理
	textContent := string(content)
	if o.Mode == "text" {
		lines := strings.Split(textContent, "\n")
		if o.Head > 0 {
			if o.Head < len(lines) {
				lines = lines[:o.Head]
			}
			textContent = strings.Join(lines, "\n")
		} else if o.Tail > 0 {
			if o.Tail < len(lines) {
				lines = lines[len(lines)-o.Tail:]
			}
			textContent = strings.Join(lines, "\n")
		} else if o.LineRange[0] > 0 && o.LineRange[1] > 0 {
			start := o.LineRange[0] - 1 // 1-indexed to 0-indexed
			end := o.LineRange[1]
			if start < 0 {
				start = 0
			}
			if end > len(lines) {
				end = len(lines)
			}
			if start < end {
				lines = lines[start:end]
			} else {
				lines = nil
			}
			textContent = strings.Join(lines, "\n")
		}
	} else {
		textContent = string(content)
	}

	successResult := &result.ReadFileResult{
		BaseResult: result.BaseResult{Code: 0, Message: "success"},
		Data: &result.ReadFileData{
			Path:    resolvedPath,
			Content: textContent,
			Mode:    o.Mode,
		},
	}

	logger.Info(fsLogComponent).Str("method_name", methodName).
		Float64("method_exec_time_ms", float64(time.Since(startTime).Milliseconds())).
		Msg("读取文件完成")

	return successResult, nil
}

// ReadFileStream 流式读取文件。
// 对齐 Python FsOperation.read_file_stream。
func (f *FsOperation) ReadFileStream(ctx context.Context, path string, opts ...sysop.FsOption) (<-chan result.ReadFileStreamResult, error) {
	ch := make(chan result.ReadFileStreamResult, 64)

	o := sysop.NewFsOptions(opts...)
	resolvedPath, err := f.resolvePath(path, false)
	if err != nil {
		close(ch)
		return ch, err
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		close(ch)
		return ch, err
	}

	go func() {
		defer close(ch)
		chunkSize := o.ChunkSize
		if chunkSize <= 0 {
			chunkSize = sysop.DefaultReadStreamChunkSize
		}

		data := string(content)
		index := 0
		for offset := 0; offset < len(data); offset += chunkSize {
			end := offset + chunkSize
			if end > len(data) {
				end = len(data)
			}
			chunk := data[offset:end]
			isLast := end >= len(data)

			ch <- result.ReadFileStreamResult{
				BaseResult: result.BaseResult{Code: 0, Message: "success"},
				Data: &result.ReadFileChunkData{
					Path:         resolvedPath,
					ChunkContent: chunk,
					Mode:         o.Mode,
					ChunkSize:    len(chunk),
					ChunkIndex:   index,
					IsLastChunk:  isLast,
				},
			}
			index++
		}
	}()

	return ch, nil
}

// WriteFile 写入文件。
// 对齐 Python FsOperation.write_file。
func (f *FsOperation) WriteFile(ctx context.Context, path string, content string, opts ...sysop.FsOption) (*result.WriteFileResult, error) {
	o := sysop.NewFsOptions(opts...)
	methodName := "write_file"

	startTime := time.Now()
	logger.Info(fsLogComponent).Str("method_name", methodName).Str("path", path).Msg("开始写入文件")

	resolvedPath, err := f.resolvePath(path, true)
	if err != nil {
		return &result.WriteFileResult{
			BaseResult: result.BuildOperationErrorResult(exception.StatusSysOperationFsExecutionError.Code(), err.Error()),
		}, nil
	}

	// 处理写入模式
	dataBytes := []byte(content)
	if o.Append {
		file, err := os.OpenFile(resolvedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return &result.WriteFileResult{
				BaseResult: result.BuildOperationErrorResult(exception.StatusSysOperationFsExecutionError.Code(), err.Error()),
			}, nil
		}
		defer file.Close()
		n, err := file.Write(dataBytes)
		if err != nil {
			return &result.WriteFileResult{
				BaseResult: result.BuildOperationErrorResult(exception.StatusSysOperationFsExecutionError.Code(), err.Error()),
			}, nil
		}
		successResult := &result.WriteFileResult{
			BaseResult: result.BaseResult{Code: 0, Message: "success"},
			Data: &result.WriteFileData{
				Path: resolvedPath,
				Size: n,
				Mode: o.Mode,
			},
		}
		return successResult, nil
	}

	err = os.WriteFile(resolvedPath, dataBytes, 0644)
	if err != nil {
		return &result.WriteFileResult{
			BaseResult: result.BuildOperationErrorResult(exception.StatusSysOperationFsExecutionError.Code(), err.Error()),
		}, nil
	}

	successResult := &result.WriteFileResult{
		BaseResult: result.BaseResult{Code: 0, Message: "success"},
		Data: &result.WriteFileData{
			Path: resolvedPath,
			Size: len(dataBytes),
			Mode: o.Mode,
		},
	}

	logger.Info(fsLogComponent).Str("method_name", methodName).
		Float64("method_exec_time_ms", float64(time.Since(startTime).Milliseconds())).
		Msg("写入文件完成")

	return successResult, nil
}

// UploadFile 上传文件（本地模式 = 文件拷贝）。
// 对齐 Python FsOperation.upload_file。
func (f *FsOperation) UploadFile(ctx context.Context, localPath string, targetPath string, opts ...sysop.FsOption) (*result.UploadFileResult, error) {
	o := sysop.NewFsOptions(opts...)

	resolvedTarget, err := f.resolvePath(targetPath, o.CreateParentDirs)
	if err != nil {
		return nil, err
	}

	resolvedLocal, err := filepath.Abs(localPath)
	if err != nil {
		return nil, err
	}

	// 简单拷贝
	content, err := os.ReadFile(resolvedLocal)
	if err != nil {
		return nil, fmt.Errorf("source not found: %s", resolvedLocal)
	}

	if err := os.WriteFile(resolvedTarget, content, 0644); err != nil {
		return nil, err
	}

	return &result.UploadFileResult{
		BaseResult: result.BaseResult{Code: 0, Message: "success"},
		Data: &result.UploadFileData{
			LocalPath:  resolvedLocal,
			TargetPath: resolvedTarget,
			Size:       len(content),
		},
	}, nil
}

// UploadFileStream 流式上传文件
func (f *FsOperation) UploadFileStream(ctx context.Context, localPath string, targetPath string, opts ...sysop.FsOption) (<-chan result.UploadFileStreamResult, error) {
	ch := make(chan result.UploadFileStreamResult, 1)
	close(ch)
	return ch, fmt.Errorf("未实现: UploadFileStream")
}

// DownloadFile 下载文件（本地模式 = 文件拷贝）
func (f *FsOperation) DownloadFile(ctx context.Context, sourcePath string, localPath string, opts ...sysop.FsOption) (*result.DownloadFileResult, error) {
	resolvedSource, err := f.resolvePath(sourcePath, false)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(resolvedSource)
	if err != nil {
		return nil, fmt.Errorf("source not found: %s", resolvedSource)
	}

	if err := os.WriteFile(localPath, content, 0644); err != nil {
		return nil, err
	}

	return &result.DownloadFileResult{
		BaseResult: result.BaseResult{Code: 0, Message: "success"},
		Data: &result.DownloadFileData{
			SourcePath: resolvedSource,
			LocalPath:  localPath,
			Size:       len(content),
		},
	}, nil
}

// DownloadFileStream 流式下载文件
func (f *FsOperation) DownloadFileStream(ctx context.Context, sourcePath string, localPath string, opts ...sysop.FsOption) (<-chan result.DownloadFileStreamResult, error) {
	ch := make(chan result.DownloadFileStreamResult, 1)
	close(ch)
	return ch, fmt.Errorf("未实现: DownloadFileStream")
}

// ListFiles 列出目录下文件。
// 对齐 Python FsOperation.list_files。
func (f *FsOperation) ListFiles(ctx context.Context, path string, opts ...sysop.FsOption) (*result.ListFilesResult, error) {
	o := sysop.NewFsOptions(opts...)
	return f.listItems(ctx, path, false, o)
}

// ListDirectories 列出目录下子目录。
// 对齐 Python FsOperation.list_directories。
func (f *FsOperation) ListDirectories(ctx context.Context, path string, opts ...sysop.FsOption) (*result.ListDirsResult, error) {
	o := sysop.NewFsOptions(opts...)

	resolvedPath, err := f.resolvePath(path, false)
	if err != nil {
		return nil, err
	}

	items := f.walkDir(resolvedPath, true, o)

	return &result.ListDirsResult{
		BaseResult: result.BaseResult{Code: 0, Message: "success"},
		Data: &result.FileSystemData{
			TotalCount: len(items),
			ListItems:  items,
			RootPath:   resolvedPath,
			Recursive:  o.Recursive,
		},
	}, nil
}

// SearchFiles 搜索文件。
// 对齐 Python FsOperation.search_files。
func (f *FsOperation) SearchFiles(ctx context.Context, path string, pattern string, opts ...sysop.FsOption) (*result.SearchFilesResult, error) {
	resolvedPath, err := f.resolvePath(path, false)
	if err != nil {
		return nil, err
	}

	var matched []result.FileSystemItem
	err = filepath.Walk(resolvedPath, func(walkPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if !info.IsDir() {
			matchedPattern, _ := filepath.Match(pattern, info.Name())
			if matchedPattern {
				matched = append(matched, f.createFSItem(walkPath, info))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result.SearchFilesResult{
		BaseResult: result.BaseResult{Code: 0, Message: "success"},
		Data: &result.SearchFilesData{
			TotalMatches:  len(matched),
			MatchingFiles: matched,
			SearchPath:    resolvedPath,
			SearchPattern: pattern,
		},
	}, nil
}

// ListTools 返回文件系统操作的工具卡片列表（硬编码）。
// description 严格使用 Python 方法英文 docstring 原文，不翻译。
// 对齐 Python BaseFsOperation.list_tools：read_file, read_file_stream, write_file,
// upload_file, upload_file_stream, download_file, download_file_stream,
// list_files, list_directories, search_files。
func (f *FsOperation) ListTools() []*tool.ToolCard {
	readFileParams := []*schema.Param{
		{Name: "path", Description: "Full or relative path to the file to read (required).", Type: schema.ParamTypeString, Required: true},
		{Name: "mode", Description: `Reading mode - "text" (line-based, default) or "bytes" (raw bytes).`, Type: schema.ParamTypeString, Default: "text",
			Enum: []any{"text", "bytes"}},
		{Name: "head", Description: "Number of lines to read from the start (text mode only).0 is equivalent to None.", Type: schema.ParamTypeInteger, Nullable: true},
		{Name: "tail", Description: "Number of lines to read from the end (text mode only).0 is equivalent to None.", Type: schema.ParamTypeInteger, Nullable: true},
		{Name: "line_range", Description: "Specific line range to read (start, end) - 1-indexed, inclusive (text mode only). If start <= 0 or end <= 0 or start > end, returns empty content.", Type: schema.ParamTypeArray, Nullable: true,
			Items: &schema.Param{Type: schema.ParamTypeInteger}},
		{Name: "encoding", Description: "Character encoding for text mode (default: utf-8).", Type: schema.ParamTypeString, Default: "utf-8"},
		{Name: "chunk_size", Description: "Maximum number of bytes to read at once (default: 0, unlimited)", Type: schema.ParamTypeInteger, Default: 0},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	readFileStreamParams := []*schema.Param{
		{Name: "path", Description: "Full or relative path to the file to read (required).", Type: schema.ParamTypeString, Required: true},
		{Name: "mode", Description: `Reading mode - "text" (line-based, default) or "bytes" (raw bytes).`, Type: schema.ParamTypeString, Default: "text",
			Enum: []any{"text", "bytes"}},
		{Name: "head", Description: "Number of lines to read from the start (text mode only).0 is equivalent to None.", Type: schema.ParamTypeInteger, Nullable: true},
		{Name: "tail", Description: "Number of lines to read from the end (text mode only).0 is equivalent to None.", Type: schema.ParamTypeInteger, Nullable: true},
		{Name: "line_range", Description: "Specific line range to read (start, end) - 1-indexed, inclusive (text mode only). If start <= 0 or end <= 0 or start > end, returns empty content.", Type: schema.ParamTypeArray, Nullable: true,
			Items: &schema.Param{Type: schema.ParamTypeInteger}},
		{Name: "encoding", Description: "Character encoding for text mode (default: utf-8).", Type: schema.ParamTypeString, Default: "utf-8"},
		{Name: "chunk_size", Description: "Buffer size for bytes mode reading (default: 8192 bytes).", Type: schema.ParamTypeInteger, Default: 8192},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	writeFileParams := []*schema.Param{
		{Name: "path", Description: "Full or relative path to the file to write (required).", Type: schema.ParamTypeString, Required: true},
		{Name: "content", Description: "Data to write to the file (string for text mode, bytes for binary mode).", Type: schema.ParamTypeString, Required: true},
		{Name: "mode", Description: `Writing mode: "text" (for string content) or "bytes" (for binary data) (default: "text").`, Type: schema.ParamTypeString, Default: "text",
			Enum: []any{"text", "bytes"}},
		{Name: "prepend_newline", Description: "Add a newline character (`\\n`) before the content (text mode only; default: True).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "append_newline", Description: "Add a newline character (`\\n`) after the content (text mode only; default: False).", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "append", Description: "Append to the file instead of overwriting (default: False).", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "create_if_not_exist", Description: "Auto-create the file if it doesn't exist (default: True).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "permissions", Description: `Octal file permissions (Unix/Linux only; ignored on Windows) (default: "644").`, Type: schema.ParamTypeString, Default: "644"},
		{Name: "encoding", Description: "Character encoding for text mode (default: utf-8).", Type: schema.ParamTypeString, Default: "utf-8"},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	uploadFileParams := []*schema.Param{
		{Name: "local_path", Description: "Local source file path (required, e.g. /tmp/local_file.txt).", Type: schema.ParamTypeString, Required: true},
		{Name: "target_path", Description: "Upload destination path (required, e.g. /mnt/storage/file.txt or sandbox:/opt/bucket/file.txt).", Type: schema.ParamTypeString, Required: true},
		{Name: "overwrite", Description: "Whether to overwrite existing target file (default: False).", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "create_parent_dirs", Description: "Whether to auto-create target parent directories (default: True).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "preserve_permissions", Description: "Whether to preserve file permissions (default: True, Unix/Linux only).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "chunk_size", Description: "Maximum number of bytes to upload at once (default: 0, unlimited)", Type: schema.ParamTypeInteger, Default: 0},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	uploadFileStreamParams := []*schema.Param{
		{Name: "local_path", Description: "Local source file path (required, e.g. /tmp/local_file.txt).", Type: schema.ParamTypeString, Required: true},
		{Name: "target_path", Description: "Upload destination path (required, e.g. /mnt/storage/file.txt or sandbox:/opt/bucket/file.txt).", Type: schema.ParamTypeString, Required: true},
		{Name: "overwrite", Description: "Whether to overwrite existing target file (default: False).", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "create_parent_dirs", Description: "Whether to auto-create target parent directories (default: True).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "preserve_permissions", Description: "Whether to preserve file permissions (default: True, Unix/Linux only).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "chunk_size", Description: "Chunk size for cross-filesystem transfers (default: 1MB, bytes).", Type: schema.ParamTypeInteger, Default: 1048576},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	downloadFileParams := []*schema.Param{
		{Name: "source_path", Description: "Source file path (required, e.g. /mnt/storage/file.txt or sandbox:/opt/bucket/file.txt).", Type: schema.ParamTypeString, Required: true},
		{Name: "local_path", Description: "Local destination file path (required, e.g. /home/user/downloads/file.txt).", Type: schema.ParamTypeString, Required: true},
		{Name: "overwrite", Description: "Whether to overwrite existing target file (default: False).", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "create_parent_dirs", Description: "Whether to auto-create target parent directories (default: True).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "preserve_permissions", Description: "Whether to preserve file permissions (default: True, Unix/Linux only).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "chunk_size", Description: "Maximum number of bytes to download at once (default: 0, unlimited)", Type: schema.ParamTypeInteger, Default: 0},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	downloadFileStreamParams := []*schema.Param{
		{Name: "source_path", Description: "Source file path (required, e.g. /mnt/storage/file.txt or sandbox:/opt/bucket/file.txt).", Type: schema.ParamTypeString, Required: true},
		{Name: "local_path", Description: "Local destination file path (required, e.g. /home/user/downloads/file.txt).", Type: schema.ParamTypeString, Required: true},
		{Name: "overwrite", Description: "Whether to overwrite existing target file (default: False).", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "create_parent_dirs", Description: "Whether to auto-create target parent directories (default: True).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "preserve_permissions", Description: "Whether to preserve file permissions (default: True, Unix/Linux only).", Type: schema.ParamTypeBoolean, Default: true},
		{Name: "chunk_size", Description: "Chunk size for cross-filesystem transfers (default: 1MB, bytes).", Type: schema.ParamTypeInteger, Default: 1048576},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	listFilesParams := []*schema.Param{
		{Name: "path", Description: "Target parent directory path (required).", Type: schema.ParamTypeString, Required: true},
		{Name: "recursive", Description: "Whether to list files in subdirectories recursively. Defaults to False.", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "max_depth", Description: "Maximum recursion depth limit, only effective when recursive=True.", Type: schema.ParamTypeInteger, Nullable: true},
		{Name: "sort_by", Description: `Sorting field, supports three options: 'name' (sort by filename, default), 'modified_time' (sort by last modification time), 'size' (sort by file size in bytes).`, Type: schema.ParamTypeString, Default: "name",
			Enum: []any{"name", "modified_time", "size"}},
		{Name: "sort_descending", Description: "Whether to sort in descending order. Defaults to False (ascending order).", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "file_types", Description: "Filter files by extension (list of extensions), e.g. ['.txt', '.pdf'].", Type: schema.ParamTypeArray, Nullable: true,
			Items: &schema.Param{Type: schema.ParamTypeString}},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	listDirectoriesParams := []*schema.Param{
		{Name: "path", Description: "Target parent directory path (required).", Type: schema.ParamTypeString, Required: true},
		{Name: "recursive", Description: "Whether to list subdirectories recursively. Defaults to False.", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "max_depth", Description: "Maximum recursion depth limit, only effective when recursive=True.", Type: schema.ParamTypeInteger, Nullable: true},
		{Name: "sort_by", Description: `Sorting field, supports three options: 'name' (sort by filename, default), 'modified_time' (sort by last modification time), 'size' (sort by file size in bytes).`, Type: schema.ParamTypeString, Default: "name",
			Enum: []any{"name", "modified_time", "size"}},
		{Name: "sort_descending", Description: "Whether to sort in descending order. Defaults to False (ascending order).", Type: schema.ParamTypeBoolean, Default: false},
		{Name: "options", Description: "Extended configuration options (dict, optional).", Type: schema.ParamTypeObject, Nullable: true},
	}

	searchFilesParams := []*schema.Param{
		{Name: "path", Description: "Base directory path to start the search (required).", Type: schema.ParamTypeString, Required: true},
		{Name: "pattern", Description: "Search pattern to match file names.", Type: schema.ParamTypeString, Required: true},
		{Name: "exclude_patterns", Description: "Optional list of patterns to exclude from results.", Type: schema.ParamTypeArray, Nullable: true,
			Items: &schema.Param{Type: schema.ParamTypeString}},
	}

	return []*tool.ToolCard{
		tool.NewToolCard("read_file",
			"Asynchronously read file with specified mode and parameters.",
			readFileParams, nil),
		tool.NewToolCard("read_file_stream",
			"Asynchronously read file streaming with specified mode and parameters.",
			readFileStreamParams, nil),
		tool.NewToolCard("write_file",
			"Asynchronously writes content to a file with flexible configuration.",
			writeFileParams, nil),
		tool.NewToolCard("upload_file",
			"Asynchronous file upload (semantics: local file \u2192 target path).",
			uploadFileParams, nil),
		tool.NewToolCard("upload_file_stream",
			"Asynchronous file upload streaming(semantics: local file \u2192 target path).",
			uploadFileStreamParams, nil),
		tool.NewToolCard("download_file",
			"Asynchronous file download (semantics: source file \u2192 local destination path).",
			downloadFileParams, nil),
		tool.NewToolCard("download_file_stream",
			"Asynchronous file download streaming(semantics: source file \u2192 local destination path).",
			downloadFileStreamParams, nil),
		tool.NewToolCard("list_files",
			"Asynchronously list files under the specified path.",
			listFilesParams, nil),
		tool.NewToolCard("list_directories",
			"Asynchronously list directories under the specified path.",
			listDirectoriesParams, nil),
		tool.NewToolCard("search_files",
			"Asynchronously search files under the specified path.",
			searchFilesParams, nil),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolvePath 解析路径，基于 CWD 解析相对路径。
// 对齐 Python FsOperation._resolve_path。
func (f *FsOperation) resolvePath(path string, createParent bool) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path 不能为空")
	}

	// 基于 CWD 解析
	base := ResolveCwd("")
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}

	// expanduser
	path = expandUser(path)

	// resolve
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	if createParent {
		dir := filepath.Dir(resolved)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("创建父目录失败: %w", err)
		}
	}

	return resolved, nil
}

// expandUser 展开 ~ 为用户主目录
func expandUser(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// listItems 列出文件/目录项
func (f *FsOperation) listItems(ctx context.Context, path string, dirsOnly bool, o *sysop.FsOptions) (*result.ListFilesResult, error) {
	resolvedPath, err := f.resolvePath(path, false)
	if err != nil {
		return nil, err
	}

	items := f.walkDir(resolvedPath, dirsOnly, o)

	return &result.ListFilesResult{
		BaseResult: result.BaseResult{Code: 0, Message: "success"},
		Data: &result.FileSystemData{
			TotalCount: len(items),
			ListItems:  items,
			RootPath:   resolvedPath,
			Recursive:  o.Recursive,
		},
	}, nil
}

// walkDir 遍历目录
func (f *FsOperation) walkDir(basePath string, dirsOnly bool, o *sysop.FsOptions) []result.FileSystemItem {
	var items []result.FileSystemItem

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return items
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fullPath := filepath.Join(basePath, entry.Name())

		if dirsOnly && !entry.IsDir() {
			continue
		}
		if !dirsOnly && entry.IsDir() {
			continue
		}

		// 文件类型过滤
		if !entry.IsDir() && len(o.FileTypes) > 0 {
			ext := filepath.Ext(entry.Name())
			found := false
			for _, ft := range o.FileTypes {
				if ext == ft {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		items = append(items, f.createFSItem(fullPath, info))

		// 递归
		if o.Recursive && entry.IsDir() {
			subItems := f.walkDir(fullPath, dirsOnly, o)
			items = append(items, subItems...)
		}
	}

	// 排序
	f.sortItems(items, o.SortBy, o.SortDescending)

	return items
}

// createFSItem 创建 FileSystemItem
func (f *FsOperation) createFSItem(path string, info os.FileInfo) result.FileSystemItem {
	var fileType *string
	if !info.IsDir() {
		ext := filepath.Ext(path)
		fileType = &ext
	}
	return result.FileSystemItem{
		Name:         info.Name(),
		Path:         path,
		Size:         info.Size(),
		ModifiedTime: info.ModTime().Format(time.DateTime),
		IsDirectory:  info.IsDir(),
		Type:         fileType,
	}
}

// sortItems 排序
func (f *FsOperation) sortItems(items []result.FileSystemItem, sortBy string, descending bool) {
	sort.Slice(items, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "name":
			less = items[i].Name < items[j].Name
		case "modified_time":
			less = items[i].ModifiedTime < items[j].ModifiedTime
		case "size":
			less = items[i].Size < items[j].Size
		default:
			less = items[i].Name < items[j].Name
		}
		if descending {
			return !less
		}
		return less
	})
}

// createErrorResult 创建错误结果
func (f *FsOperation) createErrorResult(methodName string, errMsg string, startTime time.Time) *result.ReadFileResult {
	logger.Error(fsLogComponent).Str("method_name", methodName).Str("error_msg", errMsg).Msg("文件操作失败")
	return &result.ReadFileResult{
		BaseResult: result.BuildOperationErrorResult(
			exception.StatusSysOperationFsExecutionError.Code(),
			errMsg,
		),
	}
}

// init 注册到 GlobalRegistry
func init() {
	_ = sysop.GlobalRegistry.Register(sysop.OperationDef{
		Name:        "fs",
		Mode:        sysop.OperationModeLocal,
		Description: "local fs operation",
		NewFunc:     NewFsOperation,
	})
}
