// Package sourceloc 提供原文定位（source_locators）的共享类型定义。
//
// 30.3 WeKnora 对标新增能力域：parser（坐标生产者）与 chunker（区间映射
// 消费者）跨层传递 sidecar 的强类型载体。放在 internal/common 使
// foundation/store/reranker（Document.SourceMaps 承载方）与
// core/retrieval/common（TextChunk.SourceLocators 承载方）都能零环引用。
// 坐标语义：bbox 为 PDF points（左上原点，y 向下），pdf.js viewport 直接可用。
//
// 传递纪律：Document.SourceMaps 仅 parser→chunker 内部使用；chunker 消费后
// 产出 TextChunk.SourceLocators（分块粒度定位），最终由 kbadmin 序列化落
// kb_chunk_meta.locators_json。坐标不进向量库 metadata。
package sourceloc

// ──────────────────────────── 枚举 ────────────────────────────

// SourceLocator 类型常量（对齐 WeKnora types.SourceLocator 语义子集）
const (
	// LocatorTypeText pdf 文本锚（页码+行级 bbox）
	LocatorTypeText = "text"
	// LocatorTypeImage 图片锚（caption 块宿主：pdf=图片 placement bbox）
	LocatorTypeImage = "image"
	// LocatorTypeParagraph docx 段落号锚
	LocatorTypeParagraph = "paragraph"
	// LocatorTypeCharRange 字符区间锚（txt/md）
	LocatorTypeCharRange = "char_range"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BBox 页面矩形 [x0, y0, x1, y1]，PDF points（左上原点，y 向下）
type BBox [4]float64

// PageWord 页内单词：文本 + 词框 + 全文字符区间（parser 组装期计算）
type PageWord struct {
	// Text 单词文本
	Text string
	// BBox 词框
	BBox BBox
	// Line 词所属行号（gomupdf Word.Line，行级合并分组用）
	Line int
	// CharStart/CharEnd 词在全文中的字符区间（半开）
	CharStart int
	CharEnd   int
}

// PageImage 页内展示图片：placement bbox + 提取字节
type PageImage struct {
	// BBox 页面 placement 矩形
	BBox BBox
	// Data 原始编码字节（PNG/JPEG）
	Data []byte
	// Ext 图片编码扩展名（png/jpeg/…）
	Ext string
}

// CaptionAnchor caption 文本块锚（caption 非原文，锚到宿主图）
type CaptionAnchor struct {
	// Start/End 全文字符区间（半开）
	Start, End int
	// ImageIndex 宿主图在本页 Images 的下标
	ImageIndex int
}

// PageMap 单页映射（pdf sidecar 元素）
type PageMap struct {
	// PageNo 页号（1 起）
	PageNo int
	// PageText 该页拼装文本（与全文组装同语义）
	PageText string
	// Words 页内单词（含 bbox/行号/全文区间）
	Words []PageWord
	// Images 页内展示图片
	Images []PageImage
	// CaptionAnchors caption 锚（全文区间坐标）
	CaptionAnchors []CaptionAnchor
}

// ParagraphMap docx 段落映射 sidecar 元素
type ParagraphMap struct {
	// Paragraph 段落号（word_parser 既有 paragraphNum 语义）
	Paragraph int
	// Text 段落拼装文本（含 caption 追加段）
	Text string
}

// SourceMaps 解析产物 sidecar 载体（Document.SourceMaps 字段类型）。
// 三格式互斥：pdf 填 Pages、docx 填 Paragraphs、txt/md 置 CharRange；
// 其余格式为 nil（无定位）。
type SourceMaps struct {
	// Pages pdf 页-词映射（nil=非 pdf 或无坐标）
	Pages []PageMap
	// Paragraphs docx 段落映射（nil=非 docx）
	Paragraphs []ParagraphMap
	// CharRange txt/md char_range 标记（chunk 全文区间即原文区间）
	CharRange bool
}

// SourceLocator 单条原文定位（chunk 粒度，TextChunk.SourceLocators 元素）
type SourceLocator struct {
	// Type 定位类型（LocatorType* 常量）
	Type string `json:"type"`
	// PageNo pdf 页号（1 起），其余类型 0
	PageNo int `json:"page_no,omitempty"`
	// LineBBoxes pdf 锚：行级合并矩形
	LineBBoxes []BBox `json:"line_bboxes,omitempty"`
	// Paragraph docx 段落号
	Paragraph int `json:"paragraph,omitempty"`
	// CharStart/CharEnd txt/md 全文字符区间（半开）
	CharStart int `json:"char_start,omitempty"`
	CharEnd   int `json:"char_end,omitempty"`
	// Quote 引用原文（WeKnora 同款：前端高亮时页内定位句子）
	Quote string `json:"quote,omitempty"`
}
