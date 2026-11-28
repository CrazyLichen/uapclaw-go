package chunker

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/sourceloc"
)

// ──────────────────────────── source_loc 测试（30.3 Task6） ────────────────────────────

// TestMapChunkLocators_pdf行级合并 chunk 覆盖第一行两词 → 合并为一个行矩形。
func TestMapChunkLocators_pdf行级合并(t *testing.T) {
	pageText := "Alpha Bravo\nCharlie"
	pm := sourceloc.PageMap{PageNo: 1, PageText: pageText,
		Words: []sourceloc.PageWord{
			{Text: "Alpha", BBox: sourceloc.BBox{10, 100, 60, 112}, CharStart: 0, CharEnd: 5},
			{Text: "Bravo", BBox: sourceloc.BBox{70, 100, 120, 112}, CharStart: 6, CharEnd: 11},
			{Text: "Charlie", BBox: sourceloc.BBox{10, 120, 70, 132}, CharStart: 12, CharEnd: 19},
		}}
	docText := pageText
	start := strings.Index(docText, "Alpha")
	end := start + len("Alpha Bravo") // chunk 覆盖第一行两个词

	locs := MapChunkLocators(docText, start, end, &sourceloc.SourceMaps{Pages: []sourceloc.PageMap{pm}})
	if len(locs) != 1 || locs[0].Type != sourceloc.LocatorTypeText || locs[0].PageNo != 1 {
		t.Fatalf("locator 不符: %+v", locs)
	}
	if locs[0].Quote != "Alpha Bravo" {
		t.Fatalf("Quote 不符: %q", locs[0].Quote)
	}
	// 行级合并：同 y 的 Alpha/Bravo 合并为一个行矩形
	if len(locs[0].LineBBoxes) != 1 || locs[0].LineBBoxes[0] != (sourceloc.BBox{10, 100, 120, 112}) {
		t.Fatalf("行级合并不符: %+v", locs[0].LineBBoxes)
	}
}

// TestMapChunkLocators_跨行保留 chunk 覆盖两行 → 两个行矩形按 y 升序。
func TestMapChunkLocators_跨行保留(t *testing.T) {
	pm := sourceloc.PageMap{PageNo: 2, PageText: "Alpha Bravo\nCharlie",
		Words: []sourceloc.PageWord{
			{Text: "Alpha", BBox: sourceloc.BBox{10, 100, 60, 112}, CharStart: 0, CharEnd: 5},
			{Text: "Bravo", BBox: sourceloc.BBox{70, 100, 120, 112}, CharStart: 6, CharEnd: 11},
			{Text: "Charlie", BBox: sourceloc.BBox{10, 120, 70, 132}, CharStart: 12, CharEnd: 19},
		}}
	locs := MapChunkLocators(pm.PageText, 0, len(pm.PageText), &sourceloc.SourceMaps{Pages: []sourceloc.PageMap{pm}})
	if len(locs) != 1 {
		t.Fatalf("应 1 个 text 锚: %+v", locs)
	}
	if len(locs[0].LineBBoxes) != 2 {
		t.Fatalf("跨行应保留 2 个行矩形: %+v", locs[0].LineBBoxes)
	}
	if locs[0].LineBBoxes[0][1] > locs[0].LineBBoxes[1][1] {
		t.Fatalf("行矩形应按 y 升序: %+v", locs[0].LineBBoxes)
	}
}

// TestMapChunkLocators_caption宿主锚 caption 区间 → image 锚（宿主图 bbox）。
func TestMapChunkLocators_caption宿主锚(t *testing.T) {
	// 全文 = "body text\ncaption words"；caption 在第 2 页（区间 10..23）
	docText := "body text\ncaption words"
	pm := sourceloc.PageMap{PageNo: 2,
		Words: []sourceloc.PageWord{
			{Text: "caption", BBox: sourceloc.BBox{10, 100, 60, 112}, CharStart: 10, CharEnd: 17},
			{Text: "words", BBox: sourceloc.BBox{70, 100, 110, 112}, CharStart: 18, CharEnd: 23},
		},
		Images:         []sourceloc.PageImage{{BBox: sourceloc.BBox{100, 100, 200, 200}, Data: []byte{1}, Ext: "png"}},
		CaptionAnchors: []sourceloc.CaptionAnchor{{Start: 10, End: 23, ImageIndex: 0}},
	}
	locs := MapChunkLocators(docText, 10, 23, &sourceloc.SourceMaps{Pages: []sourceloc.PageMap{pm}})
	var hasImage bool
	for _, l := range locs {
		if l.Type == sourceloc.LocatorTypeImage {
			hasImage = true
			if l.PageNo != 2 || len(l.LineBBoxes) != 1 || l.LineBBoxes[0] != (sourceloc.BBox{100, 100, 200, 200}) {
				t.Fatalf("image 锚字段不符: %+v", l)
			}
		}
	}
	if !hasImage {
		t.Fatalf("应含 image 宿主锚: %+v", locs)
	}
}

// TestMapChunkLocators_paragraph docx 段落锚。
func TestMapChunkLocators_paragraph(t *testing.T) {
	// 全文 = "段落一文本\n段落二文本"；chunk 命中段落一
	docText := "段落一文本\n段落二文本"
	paraMaps := []sourceloc.ParagraphMap{
		{Paragraph: 3, Text: "段落一文本"},
		{Paragraph: 7, Text: "段落二文本"},
	}
	locs := MapChunkLocators(docText, 0, len("段落一文本"), &sourceloc.SourceMaps{Paragraphs: paraMaps})
	if len(locs) != 1 || locs[0].Type != sourceloc.LocatorTypeParagraph ||
		locs[0].Paragraph != 3 {
		t.Fatalf("paragraph 锚不符: %+v", locs)
	}
	if locs[0].Quote != "段落一文本" {
		t.Fatalf("Quote 不符: %q", locs[0].Quote)
	}
}

// TestMapChunkLocators_charRange char_range 标记。
func TestMapChunkLocators_charRange(t *testing.T) {
	loc := MapCharRangeLocator(5, 20, "hello world")
	if loc.Type != sourceloc.LocatorTypeCharRange || loc.CharStart != 5 || loc.CharEnd != 20 ||
		loc.Quote != "hello world" {
		t.Fatalf("char_range 锚不符: %+v", loc)
	}
}

// TestMapChunkLocators_空sidecar 无 sidecar → 空切片。
func TestMapChunkLocators_空sidecar(t *testing.T) {
	locs := MapChunkLocators("text", 0, 4, nil)
	if len(locs) != 0 {
		t.Fatalf("无 sidecar 应空: %+v", locs)
	}
}
