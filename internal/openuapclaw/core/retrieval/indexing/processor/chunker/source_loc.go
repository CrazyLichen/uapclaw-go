package chunker

import (
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
)

// source_loc 30.3 source_locators 的 chunk 级映射。
// 架构对齐 WeKnora internal/sourceloc（区间索引 → chunk locators）：
// 解析端产 sidecar（pdf PageMap / docx ParagraphMap），本文件按 chunk
// 全文区间 [start,end) 求交产出块级定位；偏差 D-30-3-2/3 已登记 spec。
// sidecar 键（MetaKeySource*）消费后由 ChunkDocuments 从 chunk metadata
// 剔除——坐标数据不进向量库（spec §4 拍板）。

// ──────────────────────────── 常量 ────────────────────────────

// lineMergeTol 行级合并的行判定容差（同页词按 bbox y0 相差 ≤ 2pt 视为同行）
const lineMergeTol = 2.0

// ──────────────────────────── 导出函数 ────────────────────────────

// StripSidecarKeys 从 chunk metadata 剔除内部传递 sidecar 键。
// 坐标/映射数据不进向量库（spec §4）。
func StripSidecarKeys(meta map[string]any) {
	delete(meta, common.MetaKeySourcePageMap)
	delete(meta, common.MetaKeySourceParagraphMap)
	delete(meta, common.MetaKeySourceCharRange)
}

// MapChunkLocators 计算 chunk 全文区间 [start,end) 的定位。
// 优先级：pdf PageMap > docx ParagraphMap > char_range 标记 > 空。
// 返回可含多类锚（pdf text 锚与 image 锚并存）。
func MapChunkLocators(docText string, start, end int,
	pageMaps []*common.PageMap, paraMaps []common.ParagraphMap) []common.SourceLocator {

	var out []common.SourceLocator
	if len(pageMaps) > 0 {
		out = append(out, mapPDFLocators(docText, start, end, pageMaps)...)
	}
	if len(paraMaps) > 0 {
		out = append(out, mapParagraphLocators(docText, start, end, paraMaps)...)
	}
	return out
}

// MapCharRangeLocator 产出 char_range 型定位（txt/md 专用）。
func MapCharRangeLocator(start, end int, quote string) common.SourceLocator {
	return common.SourceLocator{
		Type:      common.LocatorTypeCharRange,
		CharStart: start,
		CharEnd:   end,
		Quote:     quote,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sidecarPageMaps 从 Document metadata 提取 pdf 页映射（缺失返回 nil）。
func sidecarPageMaps(meta map[string]any) []*common.PageMap {
	if meta == nil {
		return nil
	}
	if raw, ok := meta[common.MetaKeySourcePageMap]; ok {
		if maps, ok := raw.([]*common.PageMap); ok {
			return maps
		}
	}
	return nil
}

// sidecarParagraphMaps 从 Document metadata 提取段落映射（缺失返回 nil）。
func sidecarParagraphMaps(meta map[string]any) []common.ParagraphMap {
	if meta == nil {
		return nil
	}
	if raw, ok := meta[common.MetaKeySourceParagraphMap]; ok {
		if maps, ok := raw.([]common.ParagraphMap); ok {
			return maps
		}
	}
	return nil
}

// sidecarCharRange 从 Document metadata 读取 char_range 标记。
func sidecarCharRange(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	v, _ := meta[common.MetaKeySourceCharRange].(bool)
	return v
}

// chunkRange 前向游标定位 chunk 文本在全文中的区间（重叠段游标进 1 防
// 同位重复命中）。chunk 非精确子串（token 规整等）返回 ok=false。
func chunkRange(docText, text string, cursor *int) (int, int, bool) {
	if text == "" || *cursor < 0 || *cursor >= len(docText) {
		return 0, 0, false
	}
	rel := strings.Index(docText[*cursor:], text)
	if rel < 0 {
		return 0, 0, false
	}
	start := *cursor + rel
	end := start + len(text)
	if start+1 < len(docText) {
		*cursor = start + 1
	} else {
		*cursor = len(docText)
	}
	return start, end, true
}

// mapPDFLocators pdf 锚：词区间求交 → 行级合并；caption 宿主锚并存。
func mapPDFLocators(docText string, start, end int, pageMaps []*common.PageMap) []common.SourceLocator {
	var out []common.SourceLocator
	for _, pm := range pageMaps {
		// 词求交
		var hit []common.PageWord
		for _, w := range pm.Words {
			if w.CharStart < end && w.CharEnd > start {
				hit = append(hit, w)
			}
		}
		if len(hit) > 0 {
			out = append(out, common.SourceLocator{
				Type:       common.LocatorTypeText,
				PageNo:     pm.PageNo,
				LineBBoxes: mergeLines(hit),
				Quote:      quoteOf(docText, start, end),
			})
		}
		// caption 宿主锚（区间相交 → image 锚，bbox = 宿主图 placement）
		for _, ca := range pm.CaptionAnchors {
			if ca.Start < end && ca.End > start && ca.ImageIndex < len(pm.Images) {
				out = append(out, common.SourceLocator{
					Type:       common.LocatorTypeImage,
					PageNo:     pm.PageNo,
					LineBBoxes: []common.BBox{pm.Images[ca.ImageIndex].BBox},
					Quote:      quoteOf(docText, ca.Start, ca.End),
				})
			}
		}
	}
	return out
}

// mapParagraphLocators docx 锚：段落文本与 chunk 文本相交 → paragraph 锚。
func mapParagraphLocators(docText string, start, end int, paraMaps []common.ParagraphMap) []common.SourceLocator {
	var out []common.SourceLocator
	quote := quoteOf(docText, start, end)
	for _, pm := range paraMaps {
		if pm.Text == "" {
			continue
		}
		// 段落锚判定：chunk 文本与段落文本互含或相交
		// （chunk 是全文子串；段落文本也是全文子串——两者相交即命中）
		if chunksOverlap(docText, start, end, pm.Text) {
			out = append(out, common.SourceLocator{
				Type:      common.LocatorTypeParagraph,
				Paragraph: pm.Paragraph,
				Quote:     quote,
			})
		}
	}
	return out
}

// chunksOverlap 判定全文区间 [start,end) 与段落文本（全文子串）是否相交。
// 前向单遍定位段落文本首个出现位置（全文按段落顺序拼装，游标不回退），
// 找不到视为不命中（preprocessor 改写文本的防御路径）。
func chunksOverlap(docText string, start, end int, paraText string) bool {
	// 区间内直接查找（段落文本必为全文子串；chunk 覆盖段落任一字符即相交）
	lo := start
	if lo < 0 {
		lo = 0
	}
	if lo >= len(docText) || lo >= end {
		return false
	}
	window := docText[lo:end]
	if idx := strings.Index(window, paraText); idx >= 0 {
		return true
	}
	// 段落比 chunk 长时反向：段落起点在 chunk 前但尾部落入 chunk——
	// 用 chunk 文本反向查段落（chunk 为子串场景）
	return strings.Contains(paraText, docText[start:min(end, len(docText))])
}

// mergeLines 行级合并：同页词按（行号优先、bbox y0 相差 ≤ 容差）分组，
// 组内矩形并集，组序按 y 升序（行阅读序）。
func mergeLines(words []common.PageWord) []common.BBox {
	if len(words) == 0 {
		return nil
	}
	type seg struct {
		y0, y1 float64
		box    common.BBox
	}
	segs := make([]seg, 0, len(words))
	for _, w := range words {
		segs = append(segs, seg{y0: w.BBox[1], y1: w.BBox[3], box: w.BBox})
	}
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].y0 < segs[j].y0 })

	var out []common.BBox
	cur := segs[0]
	for _, s := range segs[1:] {
		// 同行判定：y 区间重叠或 y0 相差 ≤ 容差（gomupdf Word.Line 在
		// 提取层已带行号，此处几何容差兜底跨提取器差异）
		if s.y0 <= cur.y1+lineMergeTol || s.y0-cur.y0 <= lineMergeTol {
			cur.box = unionBox(cur.box, s.box)
			if s.y1 > cur.y1 {
				cur.y1 = s.y1
			}
			continue
		}
		out = append(out, cur.box)
		cur = s
	}
	out = append(out, cur.box)
	return out
}

// unionBox 两矩形并集
func unionBox(a, b common.BBox) common.BBox {
	return common.BBox{
		minF(a[0], b[0]), minF(a[1], b[1]),
		maxF(a[2], b[2]), maxF(a[3], b[3]),
	}
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// quoteOf 引用原文截取（WeKnora Quote 同款；防御越界）
func quoteOf(docText string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(docText) {
		end = len(docText)
	}
	if start >= end {
		return ""
	}
	return docText[start:end]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
