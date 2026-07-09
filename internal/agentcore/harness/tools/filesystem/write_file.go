package filesystem

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WriteFileInput 写入文件工具的输入参数。
// 对齐 Python: WriteFileTool invoke inputs (filesystem.py L864)
type WriteFileInput struct {
	// FilePath 文件路径（必需）
	FilePath string `json:"file_path"`
	// Content 写入内容（必需）
	Content string `json:"content"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxFileSizeGiB 文件大小上限 (1 GiB)。
	// 对齐 Python: WriteFileTool.MAX_FILE_SIZE (filesystem.py L828)
	maxFileSizeGiB = 1 * 1024 * 1024 * 1024

	// maxContentBytes 写入内容大小上限 (5 MiB)。
	// 对齐 Python: WriteFileTool.MAX_CONTENT_SIZE (filesystem.py L829)
	maxContentBytes = 5 * 1024 * 1024
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWriteFileTool 创建 WriteFileTool 实例。
// 对齐 Python: WriteFileTool (filesystem.py L825)
func NewWriteFileTool(op sys_operation.SysOperation, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("write_file", "WriteFileTool", language, nil, agentID)

	fn := func(ctx context.Context, input WriteFileInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 参数校验
		// 对齐 Python L868-873
		if input.FilePath == "" {
			return map[string]any{
				"success": false,
				"error":   "file_path is required",
			}, nil
		}

		content := input.Content

		// 内容大小检查
		// 对齐 Python L875-886
		contentBytes := len([]byte(content))
		if contentBytes > maxContentBytes {
			return map[string]any{
				"success": false,
				"error": fmt.Sprintf(
					"content is too large (%d MiB) for write_file. Maximum allowed is %d MiB. Use the bash or powershell tool to write large files instead, e.g.:\n  python -c \"open('path', 'w').write('...' * N)\"\n  or use shell redirection / a script that writes incrementally.",
					contentBytes/(1024*1024), maxContentBytes/(1024*1024),
				),
			}, nil
		}

		// 路径解析
		// 对齐 Python L888-891
		path := ResolveToolFilePath(ctx, input.FilePath)
		isUNC := isUNCPath(path)

		operationType := "create"
		var oldContent *string

		if !isUNC {
			// 对齐 Python L899-943
			if st, err := os.Stat(path); err == nil {
				// 目录检测
				// 对齐 Python L903-904
				if st.IsDir() {
					return map[string]any{
						"success": false,
						"error":   fmt.Sprintf("Target path is a directory: %s", path),
					}, nil
				}

				// 文件大小检测
				// 对齐 Python L906-914
				if st.Size() > int64(maxFileSizeGiB) {
					return map[string]any{
						"success": false,
						"error": fmt.Sprintf(
							"File is too large (%d GiB). Maximum allowed size is 1 GiB.",
							st.Size()/(1024*1024*1024),
						),
					}, nil
				}

				// Pre-read 校验
				// 对齐 Python L916-924
				readState, hasReadState := GetFileReadState(path)
				if !hasReadState || readState.IsPartial {
					return map[string]any{
						"success": false,
						"error":   fmt.Sprintf("File has not been read yet. Call read_file on '%s' first before writing to it.", path),
					}, nil
				}

				// 外部修改检测
				// 对齐 Python L926-939
				existingText, _ := readExistingText(ctx, op, path)
				existingTextLF := strings.ReplaceAll(existingText, "\r\n", "\n")

				currentMtimeNS := st.ModTime().UnixNano()
				currentSize := st.Size()

				if readState.MtimeNS != currentMtimeNS || readState.SizeBytes != currentSize {
					contentUnchanged := readState.Content != "" && existingTextLF == readState.Content
					if !contentUnchanged {
						DeleteFileReadState(path)
						return map[string]any{
							"success": false,
							"error":   "File has been modified since read, either by the user or by a linter. Read it again before attempting to write it.",
						}, nil
					}
				}

				operationType = "update"
				oldContent = &existingText
			} else if !os.IsNotExist(err) {
				// stat 出错（非文件不存在）
				return map[string]any{
					"success": false,
					"error":   err.Error(),
				}, nil
			}
		}

		// 写入文件
		// 对齐 Python L945-953
		writeRes, writeErr := op.Fs().WriteFile(ctx, path, content,
			sys_operation.WithFsPrependNewline(false),
			sys_operation.WithFsCreateIfNotExist(true),
		)
		if writeErr != nil {
			return map[string]any{
				"success": false,
				"error":   writeErr.Error(),
			}, nil
		}
		if !writeRes.IsSuccess() {
			return map[string]any{
				"success": false,
				"error":   writeRes.Message,
			}, nil
		}

		// 更新读取状态注册表
		// 对齐 Python L955-965
		if !isUNC {
			if stAfter, err := os.Stat(path); err == nil {
				contentLF := strings.ReplaceAll(content, "\r\n", "\n")
				SetFileReadState(path, &FileReadState{
					MtimeNS:   stAfter.ModTime().UnixNano(),
					SizeBytes: stAfter.Size(),
					IsPartial: false,
					Content:   contentLF,
				})
			} else {
				DeleteFileReadState(path)
			}
		}

		// 日志
		logger.Info(logComponent).
			Str("file_path", path).
			Str("operation_type", operationType).
			Int("content_bytes", contentBytes).
			Msg("WriteFileTool 写入成功")

		// 构建返回值
		// 对齐 Python L972-981
		data := map[string]any{
			"file_path":     path,
			"bytes_written": contentBytes,
			"type":          operationType,
			"created":       operationType == "create",
		}
		if oldContent != nil {
			data["original_file"] = *oldContent
		}

		return map[string]any{
			"success": true,
			"data":    data,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readExistingText 读取已有文件的文本内容。
// 对齐 Python: WriteFileTool._read_existing_text (filesystem.py L852-862)
func readExistingText(ctx context.Context, op sys_operation.SysOperation, filePath string) (string, string) {
	res, err := op.Fs().ReadFile(ctx, filePath)
	if err != nil || !res.IsSuccess() {
		return "", "utf-8"
	}

	content := ""
	if res.Data != nil {
		content = res.Data.Content
	}

	// 检测编码
	encoding := "utf-8"
	rawBytes := []byte(content)
	if len(rawBytes) >= 2 && rawBytes[0] == 0xFF && rawBytes[1] == 0xFE {
		encoding = "utf-16-le"
	}

	return content, encoding
}
