package filesystem

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"  // PNG 解码器注册
	_ "image/gif"  // GIF 解码器注册
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	pdf "github.com/ledongthuc/pdf"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReadFileInput 读取文件工具的输入参数。
// 对齐 Python: ReadFileTool invoke inputs (filesystem.py L698)
type ReadFileInput struct {
	// FilePath 文件路径（必需）
	FilePath string `json:"file_path"`
	// Offset 跳过行数，默认 0
	Offset int `json:"offset"`
	// Limit 最多读取行数，默认 2000
	Limit int `json:"limit"`
	// Pages PDF 页码范围
	Pages string `json:"pages"`
	// Caption 图片说明
	Caption string `json:"caption"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxLinesToRead 最多读取行数。
	// 对齐 Python: ReadFileTool.MAX_LINES_TO_READ (filesystem.py L274)
	maxLinesToRead = 2000

	// maxSizeBytes 文件内容大小上限 (256KB)。
	// 对齐 Python: ReadFileTool.MAX_SIZE_BYTES (filesystem.py L275)
	maxSizeBytes = 256 * 1024

	// maxTokens token 估算上限。
	// 对齐 Python: ReadFileTool.MAX_TOKENS (filesystem.py L276)
	maxTokens = 25000

	// pdfMaxPagesPerRead PDF 每次最多读取页数。
	// 对齐 Python: ReadFileTool.PDF_MAX_PAGES_PER_READ (filesystem.py L277)
	pdfMaxPagesPerRead = 20

	// pdfAtMentionInlineThreshold PDF 内联页数阈值。
	// 对齐 Python: ReadFileTool.PDF_AT_MENTION_INLINE_THRESHOLD (filesystem.py L278)
	pdfAtMentionInlineThreshold = 100
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReadFileTool 创建 ReadFileTool 实例。
// 对齐 Python: ReadFileTool (filesystem.py L273)
func NewReadFileTool(op sys_operation.SysOperation, language, agentID string, enableImageMultimodal bool) tool.Tool {
	card, _ := tools.BuildToolCard("read_file", "ReadFileTool", language, nil, agentID)

	fn := func(ctx context.Context, input ReadFileInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 参数校验: file_path 必需
		// 对齐 Python L699-701
		if input.FilePath == "" {
			return map[string]any{
				"success": false,
				"error":   "file_path is required",
			}, nil
		}

		// 路径解析
		// 对齐 Python L704-706
		filePath := ResolveToolFilePath(ctx, input.FilePath)

		// 设备路径检查
		// 对齐 Python L709-713
		if IsBlockedDevice(filePath) {
			return map[string]any{
				"success": false,
				"error":   fmt.Sprintf("Reading device file '%s' is not allowed.", filePath),
			}, nil
		}

		// 二进制文件检测（PDF/图片/Notebook 除外）
		// 对齐 Python L716-724
		ext := strings.ToLower(filepath.Ext(filePath))
		isPDF := ext == ".pdf"
		isImage := ImageExtensions[ext]
		isNotebook := ext == ".ipynb"
		if !isPDF && !isImage && !isNotebook && IsBinaryCandidate(filePath) {
			return map[string]any{
				"success": false,
				"error":   fmt.Sprintf("Binary files cannot be read as text: '%s'.", filepath.Base(filePath)),
			}, nil
		}

		// PDF pages 校验
		// 对齐 Python L729-747
		pages := input.Pages
		if pages != "" && isPDF {
			parsed := parsePDFPageRange(pages, math.MaxInt)
			if parsed == nil {
				return map[string]any{
					"success": false,
					"error":   fmt.Sprintf("Invalid PDF page range format: '%s'. Use formats like '3', '1-5', or '10-20'. Pages are 1-indexed.", pages),
				}, nil
			}
			startPg, endPg := parsed[0], parsed[1]
			if endPg >= math.MaxInt || (endPg-startPg+1) > pdfMaxPagesPerRead {
				return map[string]any{
					"success": false,
					"error":   fmt.Sprintf("Page range '%s' exceeds the maximum of %d pages per request. Please use a smaller or fully-bounded range (e.g. '1-20').", pages, pdfMaxPagesPerRead),
				}, nil
			}
		}

		// offset/limit 参数处理
		// 对齐 Python L749-756
		offset := input.Offset
		userSuppliedLimit := input.Limit != 0
		var limit int
		if userSuppliedLimit {
			limit = input.Limit
			if limit > maxLinesToRead {
				limit = maxLinesToRead
			}
		} else {
			limit = maxLinesToRead
		}

		// 获取文件 mtime/size
		// 对齐 Python L759-766
		var mtimeNS int64
		var sizeBytes int64
		if st, err := os.Stat(filePath); err == nil {
			mtimeNS = st.ModTime().UnixNano()
			sizeBytes = st.Size()
		}

		// 文件类型分派
		// 对齐 Python L768-784
		var rendered map[string]any
		var renderErr error

		if isPDF {
			rendered, renderErr = readPDF(ctx, op, filePath, pages)
		} else if isNotebook {
			rendered, renderErr = readNotebook(ctx, op, filePath)
		} else if isImage {
			rendered, renderErr = readImage(ctx, op, filePath, enableImageMultimodal)
		} else {
			rendered, renderErr = readText(ctx, op, filePath, offset, limit, !userSuppliedLimit)
		}

		if renderErr != nil {
			return map[string]any{
				"success": false,
				"error":   renderErr.Error(),
			}, nil
		}

		// 提取 content 字段
		// 对齐 Python L786-793
		content, _ := rendered["content"].(string)
		if content == "" {
			// rendered 可能本身就是 content 字符串
			if rendered["content"] != nil {
				content = fmt.Sprintf("%v", rendered["content"])
			}
		}
		lineCount := 0
		if content != "" {
			lineCount = len(strings.Split(content, "\n"))
		}

		// 更新读取状态注册表
		// 对齐 Python L796-802
		isPartial := userSuppliedLimit || offset > 0
		recordReadState(ctx, op, filePath, mtimeNS, sizeBytes, isPartial, lineCount)

		// 构建返回值
		// 对齐 Python L804-812
		resultData := make(map[string]any)
		for k, v := range rendered {
			resultData[k] = v
		}
		resultData["content"] = content
		resultData["file_path"] = filePath
		resultData["line_count"] = lineCount

		return map[string]any{
			"success": true,
			"data":    resultData,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readText 读取文本文件。
// 对齐 Python: ReadFileTool._read_text (filesystem.py L454-495)
func readText(ctx context.Context, op sys_operation.SysOperation, filePath string, offset, limit int, applySizeCap bool) (map[string]any, error) {
	// 将 offset/limit 转为行号 start/end
	// 对齐 Python L460-461
	start := max(0, offset) + 1 // 0-based skip → 1-indexed start
	end := start + max(0, limit) - 1

	// 调用 Fs().ReadFile 读取文件
	res, err := op.Fs().ReadFile(ctx, filePath, sys_operation.WithFsLineRange(start, end))
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	if !res.IsSuccess() {
		return nil, fmt.Errorf("%s", res.Message)
	}

	content := ""
	if res.Data != nil {
		content = res.Data.Content
	}

	// 大小检查
	// 对齐 Python L469-476
	if applySizeCap {
		byteLen := len([]byte(content))
		if byteLen > maxSizeBytes {
			return nil, fmt.Errorf(
				"File content (%d KB) exceeds maximum allowed size (%d KB). Use offset and limit parameters to read specific portions of the file.",
				byteLen/1024, maxSizeBytes/1024,
			)
		}
	}

	// token 估算检查
	// 对齐 Python L478-480
	tokens := estimateTokens(content)
	if tokens > maxTokens {
		return nil, fmt.Errorf(
			"File content (%d tokens) exceeds maximum allowed tokens (%d). Use offset and limit parameters to read specific portions of the file, or search for specific content instead of reading the whole file.",
			tokens, maxTokens,
		)
	}

	// CatN 添加行号
	// 对齐 Python L482
	rendered := CatN(content)

	// 空文件或 offset 超出文件末尾
	// 对齐 Python L484-495
	if strings.TrimSpace(content) == "" {
		if offset == 0 {
			rendered = "Warning: the file exists but the contents are empty."
		} else {
			lineCount := len(strings.Split(content, "\n"))
			rendered = fmt.Sprintf(
				"Warning: the file exists but is shorter than the provided offset (%d). The file has %d lines.",
				offset, lineCount,
			)
		}
	}

	return map[string]any{"content": rendered}, nil
}

// readNotebook 读取 Jupyter Notebook 文件。
// 对齐 Python: ReadFileTool._read_notebook (filesystem.py L497-545)
func readNotebook(ctx context.Context, op sys_operation.SysOperation, filePath string) (map[string]any, error) {
	res, err := op.Fs().ReadFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("读取 Notebook 失败: %w", err)
	}
	if !res.IsSuccess() {
		return nil, fmt.Errorf("%s", res.Message)
	}

	rawText := ""
	if res.Data != nil {
		rawText = res.Data.Content
	}

	// 大小检查
	// 对齐 Python L505-513
	byteLen := len([]byte(rawText))
	if byteLen > maxSizeBytes {
		return nil, fmt.Errorf(
			"Notebook content (%d KB) exceeds maximum allowed size (%d KB). Use Bash with jq to inspect specific cells:\n  cat \"%s\" | jq '.cells[:20]'        # First 20 cells\n  cat \"%s\" | jq '.cells | length'    # Count total cells",
			byteLen/1024, maxSizeBytes/1024, filePath, filePath,
		)
	}

	// token 估算检查
	// 对齐 Python L515-517
	tokens := estimateTokens(rawText)
	if tokens > maxTokens {
		return nil, fmt.Errorf(
			"Notebook content (%d tokens) exceeds maximum allowed tokens (%d). Use offset and limit parameters to read specific portions of the file, or search for specific content instead of reading the whole file.",
			tokens, maxTokens,
		)
	}

	// 解析 Notebook JSON
	// 对齐 Python L519-545
	var notebook map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawText), &notebook); err != nil {
		return nil, fmt.Errorf("解析 Notebook JSON 失败: %w", err)
	}

	cellsRaw, hasCells := notebook["cells"]
	if !hasCells {
		return map[string]any{"content": ""}, nil
	}

	var cells []map[string]json.RawMessage
	if err := json.Unmarshal(cellsRaw, &cells); err != nil {
		return nil, fmt.Errorf("解析 Notebook cells 失败: %w", err)
	}

	var blocks []string
	for idx, cell := range cells {
		cellType := "unknown"
		if ctRaw, ok := cell["cell_type"]; ok {
			var ct string
			if json.Unmarshal(ctRaw, &ct) == nil {
				cellType = ct
			}
		}

		// 提取 source
		source := extractNotebookString(cell, "source")
		blocks = append(blocks, fmt.Sprintf("## Cell %d [%s]", idx+1, cellType))
		if source != "" {
			blocks = append(blocks, strings.TrimRight(source, "\n"))
		}

		// 提取 outputs
		outputsRaw, hasOutputs := cell["outputs"]
		if hasOutputs {
			var outputs []map[string]json.RawMessage
			if json.Unmarshal(outputsRaw, &outputs) == nil && len(outputs) > 0 {
				blocks = append(blocks, "### Outputs")
				for _, out := range outputs {
					text := extractOutputText(out)
					if text != "" {
						blocks = append(blocks, strings.TrimRight(text, "\n"))
					}
				}
			}
		}
	}

	content := strings.TrimSpace(strings.Join(blocks, "\n"))
	return map[string]any{"content": content}, nil
}

// readPDF 读取 PDF 文件。
// 对齐 Python: ReadFileTool._read_pdf (filesystem.py L547-583)
// 使用 github.com/ledongthuc/pdf 作为 pdfplumber 等价库
func readPDF(ctx context.Context, op sys_operation.SysOperation, filePath string, pages string) (map[string]any, error) {
	// 以 bytes 模式读取
	res, err := op.Fs().ReadFile(ctx, filePath, sys_operation.WithFsMode("bytes"))
	if err != nil {
		return nil, fmt.Errorf("读取 PDF 失败: %w", err)
	}
	if !res.IsSuccess() {
		return nil, fmt.Errorf("%s", res.Message)
	}

	rawContent := ""
	if res.Data != nil {
		rawContent = res.Data.Content
	}

	// base64 解码原始字节（Fs ReadFile bytes 模式返回 base64 编码内容）
	rawBytes, err := base64.StdEncoding.DecodeString(rawContent)
	if err != nil {
		rawBytes = []byte(rawContent)
	}

	// 打开 PDF
	reader, err := pdf.NewReader(bytes.NewReader(rawBytes), int64(len(rawBytes)))
	if err != nil {
		return nil, fmt.Errorf("解析 PDF 失败: %w", err)
	}

	totalPages := reader.NumPage()

	// 当文档过长时要求指定页码范围
	// 对齐 Python L558-564
	if pages == "" && totalPages > pdfAtMentionInlineThreshold {
		return nil, fmt.Errorf(
			"This PDF has %d pages, which is too many to read at once. "+
				"Use the pages parameter to specify a range (e.g., pages='1-10'). "+
				"Maximum %d pages per request.",
			totalPages, pdfMaxPagesPerRead,
		)
	}

	// 解析页码范围
	// 对齐 Python L566-569
	parsed := parsePDFPageRange(pages, totalPages)
	if parsed == nil {
		return nil, fmt.Errorf("Invalid or empty PDF page range: '%s'", pages)
	}
	startPg, endPg := parsed[0], parsed[1]

	// 每次最多读取页数检查
	// 对齐 Python L571-577
	pageCount := endPg - startPg + 1
	if pageCount > pdfMaxPagesPerRead {
		return nil, fmt.Errorf(
			"Requested %d pages exceeds the maximum of %d pages per read. Narrow the pages parameter range.",
			pageCount, pdfMaxPagesPerRead,
		)
	}

	// 预加载字体缓存（跨页共享）
	fonts := make(map[string]*pdf.Font)

	// 按页提取文本
	// 对齐 Python L579-583
	var parts []string
	for pageNo := startPg; pageNo <= endPg; pageNo++ {
		page := reader.Page(pageNo)
		if page.V.IsNull() {
			parts = append(parts, fmt.Sprintf("## Page %d\n", pageNo))
			continue
		}

		// 缓存当前页字体
		for _, name := range page.Fonts() {
			if _, ok := fonts[name]; !ok {
				f := page.Font(name)
				fonts[name] = &f
			}
		}

		pageText, err := page.GetPlainText(fonts)
		if err != nil {
			logger.Debug(logComponent).
				Str("file_path", filePath).
				Int("page_no", pageNo).
				Err(err).
				Msg("ReadFileTool PDF 页面文本提取失败")
			pageText = ""
		}

		// 对齐 Python: parts.append(f"## Page {page_no}\n{page_text}".rstrip())
		block := fmt.Sprintf("## Page %d\n%s", pageNo, pageText)
		parts = append(parts, strings.TrimRight(block, " \t\n\r"))
	}

	content := strings.TrimSpace(strings.Join(parts, "\n\n"))

	logger.Info(logComponent).
		Str("file_path", filePath).
		Int("total_pages", totalPages).
		Int("start_page", startPg).
		Int("end_page", endPg).
		Msg("ReadFileTool PDF 读取完成")

	return map[string]any{"content": content}, nil
}

// readImage 读取图片文件。
// 对齐 Python: ReadFileTool._read_image (filesystem.py L604-692)
// 实现缩略图、token 预算检查、图片压缩三级降级策略
func readImage(ctx context.Context, op sys_operation.SysOperation, filePath string, enableImageMultimodal bool) (map[string]any, error) {
	// 以 bytes 模式读取
	res, err := op.Fs().ReadFile(ctx, filePath, sys_operation.WithFsMode("bytes"))
	if err != nil {
		return nil, fmt.Errorf("读取图片失败: %w", err)
	}
	if !res.IsSuccess() {
		return nil, fmt.Errorf("%s", res.Message)
	}

	rawContent := ""
	if res.Data != nil {
		rawContent = res.Data.Content
	}

	// base64 解码原始字节（Fs ReadFile bytes 模式返回 base64 编码内容）
	raw, err := base64.StdEncoding.DecodeString(rawContent)
	if err != nil {
		raw = []byte(rawContent)
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("Image file is empty: %s", filePath)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	imageType := strings.TrimPrefix(ext, ".")
	if imageType == "" {
		imageType = "png"
	}
	var dimensions string

	// Step 1: 标准缩略图 (thumbnail to 1536×1536)
	// 对齐 Python L619-638
	resized := raw
	img, detectedFmt, decodeErr := image.Decode(bytes.NewReader(raw))
	if decodeErr != nil {
		logger.Debug(logComponent).
			Str("file_path", filePath).
			Err(decodeErr).
			Msg("ReadFileTool 图片解码失败，使用原始字节")
	} else {
		// 检测原始格式并优先使用
		if detectedFmt != "" {
			imageType = detectedFmt
		} else {
			detectedFormat := detectImageFormat(raw, ext)
			if detectedFormat != "" {
				imageType = detectedFormat
			}
		}
		dimensions = fmt.Sprintf("%dx%d", img.Bounds().Dx(), img.Bounds().Dy())

		// 缩略图到 1536×1536
		thumbnailImg := thumbnailImage(img, 1536, 1536)
		candidate, encodeErr := encodeImage(thumbnailImg, imageType)
		if encodeErr == nil && len(candidate) < len(raw) {
			resized = candidate
		}
	}

	// Step 2: token 预算检查 — base64 byte count × 0.125 ≈ tokens
	// 对齐 Python L640-654
	estimatedTokens := estimateImageTokens(resized)
	if estimatedTokens > maxTokens {
		// 激进压缩 (800×800, quality=40)
		compressed := compressImageBytes(raw, 800, 800, 40)
		if compressed != nil && estimateImageTokens(compressed) <= maxTokens {
			resized = compressed
			imageType = "jpeg"
		} else {
			// 最终降级压缩 (400×400, quality=20)
			fallback := compressImageBytes(raw, 400, 400, 20)
			if fallback != nil {
				resized = fallback
				imageType = "jpeg"
			}
		}
	}

	mimeType := "image/" + imageType
	parts := []string{
		fmt.Sprintf("Image file read: %s", filePath),
		fmt.Sprintf("format: %s", imageType),
		fmt.Sprintf("size_bytes: %d", len(raw)),
		fmt.Sprintf("transmitted_size_bytes: %d", len(resized)),
	}
	if dimensions != "" {
		parts = append(parts, fmt.Sprintf("dimensions: %s", dimensions))
	}

	if !enableImageMultimodal {
		parts = append(parts,
			"Image bytes are not attached because read_file native image multimodal input is disabled.",
			"If a vision tool is configured, call image_ocr or visual_question_answering with this file path.",
		)
		return map[string]any{
			"content":   strings.Join(parts, "\n"),
			"multimodal": []map[string]any{},
		}, nil
	}

	encoded := base64.StdEncoding.EncodeToString(resized)
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)
	parts = append(parts, "Image bytes are attached as multimodal input and omitted from this tool result.")

	logger.Info(logComponent).
		Str("file_path", filePath).
		Str("image_type", imageType).
		Int("size_bytes", len(raw)).
		Int("transmitted_size_bytes", len(resized)).
		Str("dimensions", dimensions).
		Msg("ReadFileTool 图片读取完成")

	return map[string]any{
		"content": strings.Join(parts, "\n"),
		"multimodal": []map[string]any{
			{
				"type":        "image",
				"source":      "read_file",
				"source_path": filePath,
				"mime_type":   mimeType,
				"data_url":    dataURL,
			},
		},
	}, nil
}

// recordReadState 记录文件读取状态到注册表。
// 对齐 Python: ReadFileTool._record_read_state (filesystem.py L428-448)
func recordReadState(ctx context.Context, op sys_operation.SysOperation, filePath string, mtimeNS, sizeBytes int64, isPartial bool, renderedLineCount int) {
	if mtimeNS == 0 || !isTextReadForEdit(filePath) {
		return
	}

	var rawContent string
	var rawLineCount int
	effectivePartial := isPartial

	if !isPartial {
		// 读取完整文本内容用于编辑校验
		rawTextState := readRawTextForEditState(ctx, op, filePath)
		if rawTextState != nil {
			rawContent = rawTextState.content
			rawLineCount = rawTextState.lineCount
		} else {
			rawLineCount = renderedLineCount
		}
		if rawLineCount > renderedLineCount {
			effectivePartial = true
		}
	}

	state := &FileReadState{
		MtimeNS:   mtimeNS,
		SizeBytes: sizeBytes,
		IsPartial: effectivePartial,
	}
	if !effectivePartial {
		state.Content = rawContent
	}

	SetFileReadState(filePath, state)
}

// parsePDFPageRange 解析 PDF 页码范围字符串。
// 对齐 Python: ReadFileTool._parse_pdf_page_range (filesystem.py L329-354)
func parsePDFPageRange(pages string, totalPages int) []int {
	if pages == "" {
		if totalPages > 0 {
			return []int{1, totalPages}
		}
		return nil
	}

	raw := strings.TrimSpace(pages)

	// 单页号
	if !strings.Contains(raw, "-") {
		page := 0
		if _, err := fmt.Sscanf(raw, "%d", &page); err != nil || page < 1 {
			return nil
		}
		clamped := page
		if clamped > totalPages {
			clamped = totalPages
		}
		return []int{clamped, clamped}
	}

	// 范围 "start-end"
	parts := strings.SplitN(raw, "-", 2)
	start := 1
	end := totalPages
	if parts[0] != "" {
		if _, err := fmt.Sscanf(parts[0], "%d", &start); err != nil {
			return nil
		}
	}
	if parts[1] != "" {
		if _, err := fmt.Sscanf(parts[1], "%d", &end); err != nil {
			return nil
		}
	}
	if start < 1 {
		start = 1
	}
	if end > totalPages {
		end = totalPages
	}
	if start > end {
		return nil
	}
	return []int{start, end}
}

// estimateTokens 粗估 token 数量（~4 UTF-8 字符/token）。
// 对齐 Python: ReadFileTool._estimate_tokens (filesystem.py L393-395)
func estimateTokens(text string) int {
	charCount := utf8.RuneCountInString(text)
	if charCount/4 < 1 {
		return 1
	}
	return charCount / 4
}

// isTextReadForEdit 判断文件是否为可编辑文本（非图片、非 PDF、非 Notebook）。
// 对齐 Python: ReadFileTool._is_text_read_for_edit (filesystem.py L401-406)
func isTextReadForEdit(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return !ImageExtensions[ext] && ext != ".pdf" && ext != ".ipynb"
}

// rawTextState 读取原始文本状态结果
type rawTextState struct {
	content   string
	lineCount int
}

// readRawTextForEditState 读取原始文本内容，用于 EditFileTool 过时写入检查。
// 对齐 Python: ReadFileTool._read_raw_text_for_edit_state (filesystem.py L414-426)
func readRawTextForEditState(ctx context.Context, op sys_operation.SysOperation, filePath string) *rawTextState {
	res, err := op.Fs().ReadFile(ctx, filePath)
	if err != nil || !res.IsSuccess() {
		return nil
	}

	content := ""
	if res.Data != nil {
		content = res.Data.Content
	}

	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lineCount := 0
	if normalized != "" {
		lineCount = len(strings.Split(normalized, "\n"))
	}

	return &rawTextState{
		content:   normalized,
		lineCount: lineCount,
	}
}

// extractNotebookString 从 Notebook cell 中提取字符串字段（source 等）。
// 对齐 Python: "".join(cell.get("source", []))
func extractNotebookString(cell map[string]json.RawMessage, key string) string {
	raw, ok := cell[key]
	if !ok {
		return ""
	}

	// 尝试解析为字符串数组
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return strings.Join(arr, "")
	}

	// 尝试解析为单个字符串
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}

	return ""
}

// extractOutputText 从 Notebook cell output 中提取文本。
// 对齐 Python L529-542
func extractOutputText(out map[string]json.RawMessage) string {
	// "text" 字段
	if textRaw, ok := out["text"]; ok {
		var arr []string
		if json.Unmarshal(textRaw, &arr) == nil {
			return strings.Join(arr, "")
		}
		var s string
		if json.Unmarshal(textRaw, &s) == nil {
			return s
		}
	}

	// "data" 字段 → text/plain
	if dataRaw, ok := out["data"]; ok {
		var data map[string]json.RawMessage
		if json.Unmarshal(dataRaw, &data) == nil {
			if plainRaw, ok := data["text/plain"]; ok {
				var arr []string
				if json.Unmarshal(plainRaw, &arr) == nil {
					return strings.Join(arr, "")
				}
				var s string
				if json.Unmarshal(plainRaw, &s) == nil {
					return s
				}
			}
		}
	}

	// "ename" + "evalue" 字段
	ename := ""
	evalue := ""
	if enameRaw, ok := out["ename"]; ok {
		json.Unmarshal(enameRaw, &ename)
	}
	if evalueRaw, ok := out["evalue"]; ok {
		json.Unmarshal(evalueRaw, &evalue)
	}
	if ename != "" || evalue != "" {
		return ename + ": " + evalue
	}

	return ""
}

// estimateImageTokens 估算图片 base64 编码后的 token 数量。
// 对齐 Python: max(1, int(len(base64.b64encode(resized)) * 0.125))
// 即 base64 字节数 / 8
func estimateImageTokens(data []byte) int {
	encodedLen := base64.StdEncoding.EncodedLen(len(data))
	tokens := encodedLen / 8
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// thumbnailImage 将图片缩放到指定最大尺寸内，保持宽高比。
// 对齐 Python: img.thumbnail((maxW, maxH))
func thumbnailImage(img image.Image, maxW, maxH int) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// 如果已经在范围内，直接返回
	if w <= maxW && h <= maxH {
		return img
	}

	// 计算缩放比例，保持宽高比
	ratioW := float64(maxW) / float64(w)
	ratioH := float64(maxH) / float64(h)
	ratio := ratioW
	if ratioH < ratioW {
		ratio = ratioH
	}

	newW := int(float64(w) * ratio)
	newH := int(float64(h) * ratio)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	// 使用近似缩放（box filter）
	return imageResize(img, newW, newH)
}

// imageResize 简单的最近邻缩放实现
func imageResize(img image.Image, newW, newH int) image.Image {
	srcBounds := img.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))

	for y := 0; y < newH; y++ {
		srcY := y * srcH / newH
		for x := 0; x < newW; x++ {
			srcX := x * srcW / newW
			dst.Set(x, y, img.At(srcBounds.Min.X+srcX, srcBounds.Min.Y+srcY))
		}
	}
	return dst
}

// compressImageBytes 压缩图片到指定尺寸和质量，输出 JPEG。
// 对齐 Python: _compress_image_bytes(raw, size=(w, h), quality=q)
func compressImageBytes(raw []byte, maxW, maxH, quality int) []byte {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		logger.Debug(logComponent).
			Err(err).
			Msg("compressImageBytes 解码失败")
		return nil
	}

	// 转为 RGBA（确保兼容 JPEG 编码）
	rgba := image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}

	// 缩略图
	thumb := thumbnailImage(rgba, maxW, maxH)

	// 编码为 JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: quality}); err != nil {
		logger.Debug(logComponent).
			Err(err).
			Msg("compressImageBytes JPEG 编码失败")
		return nil
	}

	return buf.Bytes()
}

// encodeImage 将图片编码为指定格式的字节切片。
// 对齐 Python: img.save(out, format=detected_format.upper())
func encodeImage(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
			return nil, err
		}
	default:
		// PNG 作为默认格式
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// detectImageFormat 从文件头字节检测图片格式。
// 对齐 Python: img.format
func detectImageFormat(raw []byte, ext string) string {
	// 从文件头魔数检测
	if len(raw) >= 8 {
		switch {
		case bytes.HasPrefix(raw, []byte{0x89, 0x50, 0x4E, 0x47}):
			return "png"
		case bytes.HasPrefix(raw, []byte{0xFF, 0xD8}):
			return "jpeg"
		case bytes.HasPrefix(raw, []byte{0x47, 0x49, 0x46}):
			return "gif"
		case bytes.HasPrefix(raw, []byte{0x42, 0x4D}):
			return "bmp"
		case len(raw) >= 12 && string(raw[8:12]) == "WEBP":
			return "webp"
		}
	}
	// 回退到扩展名
	return strings.TrimPrefix(ext, ".")
}
