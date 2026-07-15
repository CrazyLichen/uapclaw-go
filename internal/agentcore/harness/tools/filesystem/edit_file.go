package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EditFileInput 编辑文件工具的输入参数。
// 对齐 Python: EditFileTool invoke inputs (filesystem.py L1185)
type EditFileInput struct {
	// FilePath 文件路径（必需）
	FilePath string `json:"file_path"`
	// OldString 被替换的字符串（空字符串=创建新文件）
	OldString string `json:"old_string"`
	// NewString 替换后的字符串（必需）
	NewString string `json:"new_string"`
	// ReplaceAll 是否替换所有匹配（默认 false）
	ReplaceAll bool `json:"replace_all"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEditFileTool 创建 EditFileTool 实例。
// 对齐 Python: EditFileTool (filesystem.py L987)
func NewEditFileTool(op sys_operation.SysOperation, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("edit_file", "EditFileTool", language, nil, agentID)

	fn := func(ctx context.Context, input EditFileInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 参数校验
		// 对齐 Python L1186-1194
		if input.FilePath == "" {
			return map[string]any{
				"success": false,
				"error":   "file_path is required",
			}, nil
		}

		newStr := input.NewString
		oldStr := input.OldString
		replaceAll := input.ReplaceAll

		// 路径解析
		// 对齐 Python L1196-1199
		filePath := ResolveToolFilePath(ctx, input.FilePath)

		// 拒绝 Jupyter Notebook
		// 对齐 Python L1202-1207
		if strings.ToLower(filepath.Ext(filePath)) == ".ipynb" {
			return map[string]any{
				"success": false,
				"error":   "Cannot edit .ipynb files with this tool. Use NotebookEdit instead.",
			}, nil
		}

		// 拒绝无操作编辑
		// 对齐 Python L1209-1214
		if oldStr == newStr {
			return map[string]any{
				"success": false,
				"error":   "old_string and new_string are identical; no changes would be made.",
			}, nil
		}

		isUNC := isUNCPath(filePath)
		fileExists := false
		if !isUNC {
			fileExists = fileExistsCheck(filePath)
		} else {
			fileExists = true
		}

		// ---- 新文件创建分支 (old_str == "") ----
		// 对齐 Python L1219-1247
		if oldStr == "" {
			if fileExists && !isUNC {
				existingContent, _ := readExistingText(ctx, op, filePath)
				if strings.TrimSpace(existingContent) != "" {
					return map[string]any{
						"success": false,
						"error":   "Cannot create new file - file already exists.",
					}, nil
				}
			}

			// 写入新文件
			writeRes, writeErr := op.Fs().WriteFile(ctx, filePath, newStr,
				sys_operation.WithFsPrependNewline(false),
				sys_operation.WithFsCreateIfNotExist(true),
			)
			if writeErr != nil {
				return map[string]any{
					"success": false,
					"error":   fmt.Sprintf("Create failed: %s", writeErr.Error()),
				}, nil
			}
			if !writeRes.IsSuccess() {
				return map[string]any{
					"success": false,
					"error":   fmt.Sprintf("Create failed: %s", writeRes.Message),
				}, nil
			}

			// 注册读取状态
			if st, err := os.Stat(filePath); err == nil {
				newStrLF := strings.ReplaceAll(newStr, "\r\n", "\n")
				SetFileReadState(filePath, &FileReadState{
					MtimeNS:   st.ModTime().UnixNano(),
					SizeBytes: st.Size(),
					IsPartial: false,
					Content:   newStrLF,
				})
			}

			return map[string]any{
				"success": true,
				"data": map[string]any{
					"file_path":    filePath,
					"replacements": 0,
					"created":      true,
				},
			}, nil
		}

		// ---- 文件必须存在 ----
		// 对齐 Python L1249-1256
		if !fileExists {
			similar := findSimilarPaths(filePath)
			hint := ""
			if len(similar) > 0 {
				hint = fmt.Sprintf(" Similar paths: %v", similar)
			}
			return map[string]any{
				"success": false,
				"error":   fmt.Sprintf("File not found: '%s'.%s", filePath, hint),
			}, nil
		}

		// ---- 文件大小检查 ----
		// 对齐 Python L1258-1278
		var currentMtimeNS int64
		var currentSize int64
		if !isUNC {
			st, err := os.Stat(filePath)
			if err != nil {
				return map[string]any{
					"success": false,
					"error":   err.Error(),
				}, nil
			}

			if st.Size() > int64(maxFileSizeGiB) {
				return map[string]any{
					"success": false,
					"error": fmt.Sprintf(
						"File is too large (%d GiB). Maximum allowed size is 1 GiB.",
						st.Size()/(1024*1024*1024),
					),
				}, nil
			}

			currentMtimeNS = st.ModTime().UnixNano()
			currentSize = st.Size()
		}

		// ---- Pre-read 校验 ----
		// 对齐 Python L1280-1289
		readState, hasReadState := GetFileReadState(filePath)
		if !hasReadState || readState.IsPartial {
			return map[string]any{
				"success": false,
				"error":   fmt.Sprintf("File must be read before editing. Call read_file on '%s' first.", filePath),
			}, nil
		}

		// ---- 外部修改检测 ----
		// 对齐 Python L1291-1310
		if !isUNC && (readState.MtimeNS != currentMtimeNS || readState.SizeBytes != currentSize) {
			contentUnchanged := false
			if readState.Content != "" {
				compareContent, _ := readExistingText(ctx, op, filePath)
				compareContentLF := strings.ReplaceAll(compareContent, "\r\n", "\n")
				contentUnchanged = compareContentLF == readState.Content
			}

			if !contentUnchanged {
				DeleteFileReadState(filePath)
				return map[string]any{
					"success": false,
					"error":   fmt.Sprintf("'%s' has been modified externally since it was last read. Re-read the file before editing.", filePath),
				}, nil
			}
		}

		// ---- 读取原始文件 ----
		// 对齐 Python L1312-1316
		content, _ := readExistingText(ctx, op, filePath)

		// EOL 检测
		// 对齐 Python L1318
		eol := DetectEOL(content)

		// CRLF → LF 归一化
		// 对齐 Python L1321
		contentLF := strings.ReplaceAll(content, "\r\n", "\n")

		// ---- XML desanitize + 行尾空白处理 ----
		// 对齐 Python L1323-1329
		oldStrClean := Desanitize(oldStr)
		oldStrClean = strings.ReplaceAll(oldStrClean, "\r\n", "\n")

		ext := strings.ToLower(filepath.Ext(filePath))
		preserveMD := mdExtensions[ext]

		newStrClean := Desanitize(newStr)
		newStrClean = strings.ReplaceAll(newStrClean, "\r\n", "\n")
		if !preserveMD {
			newStrClean = StripTrailingWhitespace(newStrClean, false)
		}

		// ---- 匹配 + 引号容错 ----
		// 对齐 Python L1331-1341
		matchStr := oldStrClean
		if !strings.Contains(contentLF, matchStr) {
			variant, found := TryQuoteVariants(oldStrClean, contentLF)
			if !found {
				return map[string]any{
					"success": false,
					"error":   fmt.Sprintf("old_string not found in '%s'.", filePath),
				}, nil
			}
			matchStr = variant
			newStrClean = PreserveQuoteStyle(oldStrClean, matchStr, newStrClean)
		}

		// ---- 唯一性校验 ----
		// 对齐 Python L1343-1353
		count := strings.Count(contentLF, matchStr)
		if !replaceAll && count > 1 {
			return map[string]any{
				"success": false,
				"error": fmt.Sprintf(
					"old_string matches %d times in '%s'. Provide more surrounding context to make it unique, or set replace_all=true to replace every occurrence.",
					count, filePath,
				),
			}, nil
		}

		// ---- 执行替换 ----
		// 对齐 Python L1355-1361
		var newContentLF string
		replaced := count
		if replaceAll {
			newContentLF = strings.ReplaceAll(contentLF, matchStr, newStrClean)
		} else {
			newContentLF = strings.Replace(contentLF, matchStr, newStrClean, 1)
			replaced = 1
		}

		// 还原 EOL 风格
		// 对齐 Python L1363-1364
		var newContent string
		if eol == "\r\n" {
			newContent = strings.ReplaceAll(newContentLF, "\n", "\r\n")
		} else {
			newContent = newContentLF
		}

		// ---- 写回 ----
		// 对齐 Python L1366-1371
		writeRes, writeErr := op.Fs().WriteFile(ctx, filePath, newContent,
			sys_operation.WithFsPrependNewline(false),
		)
		if writeErr != nil {
			return map[string]any{
				"success": false,
				"error":   fmt.Sprintf("Write failed: %s", writeErr.Error()),
			}, nil
		}
		if !writeRes.IsSuccess() {
			return map[string]any{
				"success": false,
				"error":   fmt.Sprintf("Write failed: %s", writeRes.Message),
			}, nil
		}

		// 更新读取状态注册表
		// 对齐 Python L1373-1383
		if st2, err := os.Stat(filePath); err == nil {
			SetFileReadState(filePath, &FileReadState{
				MtimeNS:   st2.ModTime().UnixNano(),
				SizeBytes: st2.Size(),
				IsPartial: false,
				Content:   newContentLF,
			})
		} else {
			DeleteFileReadState(filePath)
		}

		// 日志
		logger.Info(logComponent).
			Str("file_path", filePath).
			Int("replacements", replaced).
			Bool("replace_all", replaceAll).
			Msg("EditFileTool 编辑成功")

		// 构建返回值
		// 对齐 Python L1390-1393
		return map[string]any{
			"success": true,
			"data": map[string]any{
				"file_path":    filePath,
				"replacements": replaced,
			},
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fileExistsCheck 检查文件是否存在。
func fileExistsCheck(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// findSimilarPaths 查找相似路径。
// 对齐 Python: EditFileTool._find_similar_paths (filesystem.py L1077-1092)
func findSimilarPaths(filePath string) []string {
	directory := filepath.Dir(filePath)
	if directory == "" {
		directory = "."
	}

	targetBase := strings.ToLower(strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}

	var similar []string
	for _, entry := range entries {
		candidateBase := strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if candidateBase == targetBase || strings.Contains(candidateBase, targetBase) || strings.Contains(targetBase, candidateBase) {
			similar = append(similar, filepath.Join(directory, entry.Name()))
		}
		if len(similar) >= 5 {
			break
		}
	}
	return similar
}
