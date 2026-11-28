package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/sourceloc"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/reranker"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PDFParser PDF 文档解析器。
// 嵌入 BaseParser，覆写 ParseContent 与 Parse（Parse 覆写为 30.3 sidecar
// 注入点——ImageParser 覆写先例）。
//
// 30.3 重写（偏差 D-30-3-1，用户拍板「直接替换」）：统一 gomupdf 单库链——
//   - 文本：逐页词框（含坐标与行号），行分组拼页文本
//   - 图片：placement bbox + 原生字节（归档命名语义不变），ledongthuc/pdfcpu 退役
//   - sidecar：SourceMaps（词全文区间 + 图片 placement + caption 宿主锚）
//     经 Document.SourceMaps 类型化字段传递（方案A2 独立包），全程零 any
//
// 正文组装语义保持：非空页文本 + 该页图片 caption 紧随其后；
// 过滤空行后 "\n".join；空串返回（"result if result else None"）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/pdf_parser.py (PDFParser)
type PDFParser struct {
	// BaseParser 默认适配器（LazyParse/Process/Supports 默认实现）
	*BaseParser
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 PDFParser 实现 Parser
var _ Parser = (*PDFParser)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPDFParser 构造 PDFParser。
// NewBaseParser(self) 注入使模板方法动态派发到本类覆写。
// Python: pdf_parser.py PDFParser.__init__
func NewPDFParser() (*PDFParser, error) {
	p := &PDFParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// ParseContent 解析 PDF 文本通道（模板派发点，签名对齐 Python _parse）。
// 委托 parseWithLocators，仅返回文本（sidecar 由 Parse 覆写注入 Document）。
// 整体软失败：任一通道 err → Error 日志 + 返回 ("", nil)
// （对齐 Python except → logger.error → return None）。
// Python: pdf_parser.py PDFParser._parse
func (p *PDFParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	content, _, err := p.parseWithLocators(ctx, doc, llmClient)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse PDF {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse pdf")
		return "", nil
	}
	return content, nil
}

// Parse 解析文档并注入 source_locators sidecar（30.3 覆写点）。
// 复刻 BaseParser.Parse 模板（非空内容包装为单文档），SourceMaps 类型化
// 字段携带 pdf 页映射（仅 parser→chunker 内部传递）。
// 软失败语义与 ParseContent 一致：err → Error 日志 + 空切片。
func (p *PDFParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	o := NewParseOptions(opts...)
	content, pageMaps, err := p.parseWithLocators(ctx, doc, o.LLMClient)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse PDF {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse pdf")
		return []reranker.Document{}, nil
	}
	if content == "" {
		return []reranker.Document{}, nil
	}
	return []reranker.Document{{
		ID:         o.DocID,
		Text:       content,
		Metadata:   map[string]any{},
		SourceMaps: &sourceloc.SourceMaps{Pages: pageMaps},
	}}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册到 registry（对齐 Python @register_parser([".pdf", ".PDF"])
// import 时执行；".PDF" 被 first-wins 小写归一跳过——13.16 既定语义）。
func init() {
	RegisterParser([]string{".pdf", ".PDF"}, func() Parser {
		p, _ := NewPDFParser()
		return p
	})
}

// pageAssemble 单页文本拼装产物
type pageAssemble struct {
	// text 页文本（同词空格 join、跨行 "\n" 分隔）
	text string
	// wordRel 每词在页文本中的起始 offset（与输入词序下标对齐）
	wordRel []int
}

// parseWithLocators 完整解析：词框文本通道 + 图片归档/caption + sidecar。
// 步骤（组装语义对齐旧链与 Python _async_parse_pdf 主循环）：
//  1. extractPDFPages 提取词框与图片（placement + 字节）
//  2. 逐页拼页文本（行分组）；非空页文本入全文（页间/段间 "\n"）
//  3. 每页图片归档落盘（命名 {filename}__page_{页码}__img_{页内序号}.{ext}，
//     Python 命名复刻）；caption 经 CaptionImages 生成，非空 caption
//     紧随该页文本（软失败：归档/调用失败跳过该 caption，不阻塞）
//  4. 词区间 = 页起始 offset + 页内 offset（全文为顺序追加，折算精确）；
//     caption 记 CaptionAnchor（全文区间 + 宿主图下标）
//  5. 产出 PageMap sidecar
func (p *PDFParser) parseWithLocators(ctx context.Context, doc string, llmClient *llm.Model) (string, []sourceloc.PageMap, error) {
	pages, err := extractPDFPages(doc)
	if err != nil {
		return "", nil, err
	}

	captioner := NewImageCaptioner(llmClient)
	filename := filepath.Base(doc)

	var b strings.Builder
	pageMaps := make([]sourceloc.PageMap, 0, len(pages))
	for _, pd := range pages {
		pa := assemblePageText(pd.Words)
		pm := sourceloc.PageMap{PageNo: pd.PageNo, PageText: pa.text, Images: pd.Images}

		// 非空页文本入全文；词区间折算（pageStart 在分隔符写入后取值）
		pageStart := -1
		if pa.text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			pageStart = b.Len()
			b.WriteString(pa.text)
		}
		for i := range pd.Words {
			pd.Words[i].CharStart = pageStart + pa.wordRel[i]
			pd.Words[i].CharEnd = pd.Words[i].CharStart + len(pd.Words[i].Text)
		}
		pm.Words = pd.Words

		// 该页图片归档 → caption（整页一批，caption 下标 = 图片下标）
		imgPaths := make([]string, 0, len(pd.Images))
		for i, img := range pd.Images {
			imagePath, aerr := archivePageImage(filename, pd.PageNo, i, img)
			if aerr != nil {
				logger.Error(logComponent).Err(aerr).
					Str("file_path", doc).
					Msg("failed to archive pdf image")
				continue // 图片通道独立软失败，文本通道继续
			}
			imgPaths = append(imgPaths, imagePath)
			if len(imgPaths)-1 != i {
				// 归档失败导致的下标错位：后续锚点以 imgPaths 实际下标为准
				continue
			}
		}
		if len(imgPaths) > 0 {
			captions := captioner.CaptionImages(ctx, imgPaths)
			for i, c := range captions {
				if c == "" || i >= len(pd.Images) {
					continue
				}
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				capStart := b.Len()
				b.WriteString(c)
				pm.CaptionAnchors = append(pm.CaptionAnchors, sourceloc.CaptionAnchor{
					Start: capStart, End: b.Len(), ImageIndex: i,
				})
			}
		}

		pageMaps = append(pageMaps, pm)
	}
	return b.String(), pageMaps, nil
}

// assemblePageText 单页词→文本：同 Line 空格 join、跨 Line "\n" 分隔，
// 同步记录每词页内 offset（词序 = 提取序，语义对齐旧 GetPlainText 读取序）。
func assemblePageText(words []sourceloc.PageWord) pageAssemble {
	var b strings.Builder
	rel := make([]int, len(words))
	prevLine := -1
	for i, w := range words {
		switch {
		case b.Len() == 0:
			// 行首
		case w.Line != prevLine:
			b.WriteString("\n")
		default:
			b.WriteString(" ")
		}
		rel[i] = b.Len()
		b.WriteString(w.Text)
		prevLine = w.Line
	}
	return pageAssemble{text: b.String(), wordRel: rel}
}

// archivePageImage 归档单张页图片（旧 extractImagesByPage 命名语义）。
// Python: pdf_parser.py _extract_images_from_pdf_page 命名复刻
func archivePageImage(filename string, pageNo, imgIndex int, img sourceloc.PageImage) (string, error) {
	if err := os.MkdirAll(savedImageDir, 0o755); err != nil {
		return "", fmt.Errorf("创建图片目录失败: %w", err)
	}
	ext := img.Ext
	if ext == "" {
		ext = "png" // 防御：无扩展名回落 png
	}
	imagePath := filepath.Join(savedImageDir, fmt.Sprintf("%s__page_%d__img_%d.%s", filename, pageNo, imgIndex, ext))
	if err := os.WriteFile(imagePath, img.Data, 0o644); err != nil {
		return "", fmt.Errorf("落盘图片失败: %w", err)
	}
	return imagePath, nil
}
