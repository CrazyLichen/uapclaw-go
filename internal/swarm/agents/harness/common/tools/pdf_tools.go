// read_pdf 工具：页级 PDF 读取。
//
// 对齐 Python jiuwenswarm/agents/harness/common/tools/pdf_tools.py（275 行）。
//
// 用 pdfplumber 等价物逐页提取 PDF 文本层；支持页码范围，便于按 wiki 摄取
// 提示词的要求增量读取大文档。无文本层的页（扫描件）会标记出来，让模型
// 回退到视觉工具。
//
// 语言差异：
//   - Python pdfplumber 的 page.extract_text() → Go ledongthuc/pdf 的
//     Page.GetPlainText(fonts)（13.16 PDFParser 已落地的等价物）
//   - Python asyncio.to_thread → Go 工具闭包已在框架 goroutine 中，直接同步调用
//   - Python get_workspace() 隐式读 ContextVar → Go cwd.GetWorkspace(ctx)
//   - Python read_pdf 返回 str → Go 工具闭包返回 map[string]any{"result": ...}

package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	pdf "github.com/ledongthuc/pdf"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReadPdfRequest 页级 PDF 读取请求。
//
// Python: @dataclass(frozen=True) ReadPdfRequest
type ReadPdfRequest struct {
	// PDFPath PDF 文件路径
	PDFPath string
	// Pages 1-based 页码；nil 表示全部页
	Pages []int
	// MaxChars 输出字符上限
	MaxChars int
}

// readPDFInput read_pdf 工具输入参数。
//
// Python: read_pdf(pdf_path, pages=None, max_chars=DEFAULT_MAX_CHARS, **kwargs)
type readPDFInput struct {
	// PDFPath 本地 PDF 文件路径
	PDFPath string `json:"pdf_path" jsonschema:"description=Local PDF file path,required"`
	// Pages 页码（如 3、"1-5"、"1,3,8-10"）
	Pages any `json:"pages"`
	// MaxChars 输出字符上限
	MaxChars any `json:"max_chars"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultMaxChars 默认输出字符上限
	DefaultMaxChars = 50_000
	// maxCharsCeiling max_chars 上限
	maxCharsCeiling = 200_000
	// maxPagesPerCall 单次调用最多读取页数
	maxPagesPerCall = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReadPDFTool 构造 read_pdf 工具。
//
// Python: @tool(name="read_pdf", description=...) 装饰的 read_pdf
// 描述字符串逐字复制 Python 原文（含插值的常量值）。
func NewReadPDFTool() tool.Tool {
	description := "Read the text content of a PDF file, optionally limited to specific pages. " +
		"Use this tool when a PDF file path is available and its content is needed. " +
		fmt.Sprintf("Omitting `pages` reads the whole document, up to max_chars and %d pages, ", maxPagesPerCall) +
		"which is the right default — most documents fit in one call. Only when the return " +
		"reports that pages were left unread, continue with `pages` from the first " +
		"unread page until the document is covered. Any answer based on part of a " +
		"document must state which pages it used. " +
		"Input: pdf_path (local file path), optional pages (e.g. 3, '1-5' or '1,3,8-10'), " +
		fmt.Sprintf("optional max_chars (default %d). Pages without a text layer are reported ", DefaultMaxChars) +
		"as likely scanned images."

	card := tool.NewToolCard("read_pdf", description, nil, nil)
	fn := func(ctx context.Context, input readPDFInput, opts ...tool.ToolOption) (map[string]any, error) {
		result := readPDFCore(ctx, map[string]any{
			"pdf_path":  input.PDFPath,
			"pages":     input.Pages,
			"max_chars": input.MaxChars,
		})
		return map[string]any{"result": result}, nil
	}
	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readPDFCore read_pdf 主体（对齐 Python 的 try/except 包裹）。
//
// Python:
//
//	try:
//	    req = _normalize_request({...})
//	    logger.info("[read_pdf] path=%s pages=%s max_chars=%s", ...)
//	    return await asyncio.to_thread(_read_pdf_sync, req)
//	except Exception as exc:
//	    return f"[ERROR]: read_pdf failed: {exc}"
func readPDFCore(ctx context.Context, inputs map[string]any) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("[ERROR]: read_pdf failed: %v", r)
		}
	}()
	req := normalizeReadPdfRequest(inputs)
	logger.Info(logComponent).
		Str("path", req.PDFPath).
		Str("pages", fmt.Sprint(req.Pages)).
		Int("max_chars", req.MaxChars).
		Msg("read_pdf invoked")
	return readPDFSync(ctx, req)
}

// parsePageRanges 把 pages 输入解析为去重排序的 1-based 页码。
//
// Python: _parse_page_ranges(value)
// 接受 int（单页）、int 列表、或 "1-5" / "1,3,8-10" 形式字符串。
// 空 / None 表示全部页。错误抛 ValueError → Go panic（由 readPDFCore 兜住）。
func parsePageRanges(value any) []int {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case bool:
		panic(fmt.Errorf("pages must be a page number, list, or range string like '1-5,8'"))
	case int:
		if v < 1 {
			panic(fmt.Errorf("page numbers are 1-based, got: %d", v))
		}
		return []int{v}
	case int64:
		if v < 1 {
			panic(fmt.Errorf("page numbers are 1-based, got: %d", v))
		}
		return []int{int(v)}
	case float64:
		n := int(v)
		if n < 1 {
			panic(fmt.Errorf("page numbers are 1-based, got: %v", v))
		}
		return []int{n}
	case []any:
		pages := map[int]bool{}
		for _, entry := range v {
			for _, p := range parsePageRanges(entry) {
				pages[p] = true
			}
		}
		return sortedPageList(pages)
	case []int:
		pages := map[int]bool{}
		for _, entry := range v {
			for _, p := range parsePageRanges(entry) {
				pages[p] = true
			}
		}
		return sortedPageList(pages)
	}

	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return nil
	}
	pages := map[int]bool{}
	for _, rawPart := range strings.Split(text, ",") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			startText, endText := partition(part, "-")
			start, err1 := strconv.Atoi(startText)
			end, err2 := strconv.Atoi(endText)
			if err1 != nil || err2 != nil {
				panic(fmt.Errorf("Invalid page range: %q. Use forms like '1-5' or '3'.", part))
			}
			if start < 1 || end < start {
				panic(fmt.Errorf("Invalid page range: %q (pages are 1-based, start <= end)", part))
			}
			for p := start; p <= end; p++ {
				pages[p] = true
			}
		} else {
			page, err := strconv.Atoi(part)
			if err != nil {
				panic(fmt.Errorf("Invalid page number: %q", part))
			}
			if page < 1 {
				panic(fmt.Errorf("page numbers are 1-based, got: %d", page))
			}
			pages[page] = true
		}
	}
	return sortedPageList(pages)
}

// sortedPageList 把页码集合转为排序切片；空集合返回 nil（Python `or None`）。
func sortedPageList(pages map[int]bool) []int {
	if len(pages) == 0 {
		return nil
	}
	out := make([]int, 0, len(pages))
	for p := range pages {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// partition Python str.partition("-") 等价：返回 sep 前、后两段。
func partition(value, sep string) (string, string) {
	idx := strings.Index(value, sep)
	if idx < 0 {
		return value, ""
	}
	return value[:idx], value[idx+len(sep):]
}

// resolvePDFPath 把 pdf_path 相对 Core 当前主工作区解析。
//
// Python: _resolve_pdf_path(value)
// 校验顺序：空 → 相对且无 workspace → 存在 → is_file → .pdf 后缀。
func resolvePDFPath(ctx context.Context, value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		panic(fmt.Errorf("pdf_path cannot be empty"))
	}
	path := expandHome(text)
	if !filepath.IsAbs(path) {
		workspace := cwd.GetWorkspace(ctx)
		if workspace == "" {
			panic(fmt.Errorf("read_pdf runtime workspace is unavailable"))
		}
		path = filepath.Join(workspace, path)
	}
	path = resolvePath(path)
	info, err := os.Stat(path)
	if err != nil {
		panic(fmt.Errorf("PDF file does not exist: %s", path))
	}
	if info.IsDir() {
		panic(fmt.Errorf("pdf_path is not a file: %s", path))
	}
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
		panic(fmt.Errorf("read_pdf only accepts .pdf files, got: %s", filepath.Base(path)))
	}
	return path
}

// normalizeReadPdfRequest 归一化工具输入。
//
// Python: _normalize_request(inputs)
func normalizeReadPdfRequest(inputs map[string]any) ReadPdfRequest {
	pdfPath := strings.TrimSpace(fmt.Sprint(inputs["pdf_path"]))
	if pdfPath == "" || pdfPath == "<nil>" {
		panic(fmt.Errorf("pdf_path cannot be empty."))
	}
	pages := parsePageRanges(inputs["pages"])
	maxChars := toIntDefault(inputs["max_chars"], DefaultMaxChars)
	if maxChars < 1_000 {
		maxChars = 1_000
	}
	if maxChars > maxCharsCeiling {
		maxChars = maxCharsCeiling
	}
	return ReadPdfRequest{PDFPath: pdfPath, Pages: pages, MaxChars: maxChars}
}

// toIntDefault Python int(value) 等价：失败回退默认值。
func toIntDefault(value any, fallback int) int {
	switch v := value.(type) {
	case nil:
		return fallback
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case bool:
		if v {
			return 1
		}
		return 0
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fallback
		}
		return n
	default:
		return fallback
	}
}

// readPDFSync 同步读取 PDF 并拼装输出文本。
//
// Python: _read_pdf_sync(req)
// 步骤：先校验路径（让路径错误在缺依赖环境里也清晰），再打开 PDF，
// 选页 / 头部 / 截断提示 / 逐页提取 / 无文本层汇总。
func readPDFSync(ctx context.Context, req ReadPdfRequest) string {
	// 先校验路径再读 PDF，让路径错误优先暴露
	path := resolvePDFPath(ctx, req.PDFPath)

	pdfBytes, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	r, err := pdf.NewReader(bytes.NewReader(pdfBytes), int64(len(pdfBytes)))
	if err != nil {
		panic(err)
	}
	totalPages := r.NumPage()

	var selected []int
	var outOfRange []int
	if req.Pages != nil {
		for _, p := range req.Pages {
			if p <= totalPages {
				selected = append(selected, p)
			} else {
				outOfRange = append(outOfRange, p)
			}
		}
	} else {
		for p := 1; p <= totalPages; p++ {
			selected = append(selected, p)
		}
	}
	if len(selected) > maxPagesPerCall {
		selected = selected[:maxPagesPerCall]
	}

	blocks := []string{}
	header := []string{
		fmt.Sprintf("PDF: %s | total pages: %d | reading pages: %s",
			filepath.Base(path), totalPages, formatPageListOrNone(selected)),
	}
	// 头部说明读取子集，但要明说缺了什么、怎么补：无论 pages 指定了子集，
	// 还是单次页数上限截断了，都以「实际缺的页」为准来披露。
	chosen := map[int]bool{}
	for _, p := range selected {
		chosen[p] = true
	}
	skipped := []int{}
	for p := 1; p <= totalPages; p++ {
		if !chosen[p] {
			skipped = append(skipped, p)
		}
	}
	if len(skipped) > 0 {
		remedy := "Omit `pages` to read the whole document in one call."
		if totalPages > maxPagesPerCall {
			remedy = fmt.Sprintf(
				"At most %d pages are read per call, so no single call covers this document — "+
					"call read_pdf again with `pages` starting at %d.",
				maxPagesPerCall, skipped[0])
		}
		header = append(header, fmt.Sprintf(
			"[Partial read: %d of %d pages were NOT read (pages %s). %s Any answer based on "+
				"this call must state which pages it covers.]",
			len(skipped), totalPages, formatPageList(skipped), remedy))
	}
	if len(outOfRange) > 0 {
		header = append(header, fmt.Sprintf(
			"[Note: requested page(s) %s exceed total page count %d and were skipped]",
			formatPageList(outOfRange), totalPages))
	}
	blocks = append(blocks, strings.Join(header, "\n"))

	emptyPages := []int{}
	fonts := map[string]*pdf.Font{}
	charsUsed := 0
	for idx, pageNum := range selected {
		page := r.Page(pageNum)
		pageText := ""
		if !page.V.IsNull() {
			for _, name := range page.Fonts() {
				if _, ok := fonts[name]; !ok {
					f := page.Font(name)
					fonts[name] = &f
				}
			}
			extracted, err := page.GetPlainText(fonts)
			if err != nil {
				panic(err)
			}
			pageText = strings.TrimSpace(extracted)
		}
		if pageText == "" {
			emptyPages = append(emptyPages, pageNum)
			blocks = append(blocks, fmt.Sprintf("--- Page %d ---\n[no text layer on this page]", pageNum))
			continue
		}
		remaining := req.MaxChars - charsUsed
		var unread []int
		if remaining <= 0 {
			unread = selected[idx:]
		} else {
			truncatedHere := len(pageText) > remaining
			if truncatedHere {
				pageText = pageText[:remaining] + "\n[... page truncated at max_chars ...]"
			}
			charsUsed += len(pageText)
			blocks = append(blocks, fmt.Sprintf("--- Page %d ---\n%s", pageNum, pageText))
			if !truncatedHere {
				continue
			}
			unread = selected[idx+1:]
		}
		note := fmt.Sprintf("[Truncated at max_chars=%d: the rest of the document was NOT read", req.MaxChars)
		if len(unread) > 0 {
			note += fmt.Sprintf("; unread pages: %s — call read_pdf again with `pages` starting there", formatPageList(unread))
		}
		note += ". Raise `max_chars` to read more in one call. Any answer based on " +
			"this call must state that the document was read only in part.]"
		blocks = append(blocks, note)
		break
	}

	if len(emptyPages) > 0 {
		blocks = append(blocks, fmt.Sprintf(
			"[Pages without extractable text: %s. These are likely scanned images; "+
				"use image/vision tools on rendered pages if their content is needed.]",
			formatPageList(emptyPages)))
	}
	return strings.Join(blocks, "\n\n")
}

// formatPageListOrNone 空列表返回 "none"。
func formatPageListOrNone(pages []int) string {
	if len(pages) == 0 {
		return "none"
	}
	return formatPageList(pages)
}

// formatPageList 把排序页码压缩为紧凑范围串，如 1-3,7。
//
// Python: _format_page_list(pages)
func formatPageList(pages []int) string {
	if len(pages) == 0 {
		return ""
	}
	set := map[int]bool{}
	for _, p := range pages {
		set[p] = true
	}
	ordered := make([]int, 0, len(set))
	for p := range set {
		ordered = append(ordered, p)
	}
	sort.Ints(ordered)

	parts := []string{}
	start, prev := ordered[0], ordered[0]
	for _, page := range ordered[1:] {
		if page == prev+1 {
			prev = page
			continue
		}
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
		}
		start, prev = page, page
	}
	if start == prev {
		parts = append(parts, strconv.Itoa(start))
	} else {
		parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
	}
	return strings.Join(parts, ",")
}

// expandHome 展开 ~ 前缀（Python Path.expanduser）。
func expandHome(value string) string {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(value, "~"))
		}
	}
	return value
}
