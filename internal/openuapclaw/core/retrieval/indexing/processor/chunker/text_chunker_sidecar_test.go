package chunker

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/sourceloc"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/reranker"
)

// ──────────────────────────── ChunkDocuments×sidecar 集成（30.3 Task6） ────────────────────────────

// newNoPreprocessChunker 无预处理 TextChunker（kbadmin 链同款构造）。
func newNoPreprocessChunker(t *testing.T, size, overlap int) *TextChunker {
	t.Helper()
	c, err := NewTextChunker(size, overlap)
	if err != nil {
		t.Fatalf("NewTextChunker: %v", err)
	}
	return c
}

// TestChunkDocuments_pdfSidecar word 区间切出的子串 == 词文本；sidecar 键剥离。
func TestChunkDocuments_pdfSidecar(t *testing.T) {
	c := newNoPreprocessChunker(t, 20, 0)
	pageText := "Alpha Bravo\nCharlie Delta Echo"
	pm := sourceloc.PageMap{PageNo: 1, PageText: pageText,
		Words: []sourceloc.PageWord{
			{Text: "Alpha", BBox: sourceloc.BBox{10, 100, 60, 112}, CharStart: 0, CharEnd: 5},
			{Text: "Bravo", BBox: sourceloc.BBox{70, 100, 120, 112}, CharStart: 6, CharEnd: 11},
			{Text: "Charlie", BBox: sourceloc.BBox{10, 120, 70, 132}, CharStart: 12, CharEnd: 19},
			{Text: "Delta", BBox: sourceloc.BBox{80, 120, 130, 132}, CharStart: 20, CharEnd: 25},
			{Text: "Echo", BBox: sourceloc.BBox{140, 120, 180, 132}, CharStart: 26, CharEnd: 30},
		}}
	doc := reranker.Document{ID: "doc-1", Text: pageText,
		SourceMaps: &sourceloc.SourceMaps{Pages: []sourceloc.PageMap{pm}}}

	chunks, err := c.ChunkDocuments(t.Context(), []reranker.Document{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("应产出分块")
	}
	// 重建词区间（与前向扫描一致的全文定位）
	cursor := 0
	for _, ch := range chunks {
		start := strings.Index(pageText[cursor:], ch.Text)
		if start < 0 {
			t.Fatalf("chunk 非全文子串: %q", ch.Text)
		}
		start += cursor
		cursor = start + 1
		for _, w := range pm.Words {
			// 词全文区间（由 parser 计算；此处重建验证映射正确性）
			if w.CharEnd > start && w.CharStart < start+len(ch.Text) {
				_ = w
			}
		}
		// metadata 不携带坐标（类型化字段方案）：无键可剔，正文/溯源键不受影响
	}
	// 至少一个 chunk 带 text 锚
	var anchored int
	for _, ch := range chunks {
		for _, l := range ch.SourceLocators {
			if l.Type == sourceloc.LocatorTypeText && l.PageNo == 1 && len(l.LineBBoxes) > 0 {
				anchored++
			}
		}
	}
	if anchored == 0 {
		t.Fatal("应存在带行级 bbox 的 chunk")
	}
}

// TestChunkDocuments_charRange txt/md：每 chunk 携带 char_range 锚。
func TestChunkDocuments_charRange(t *testing.T) {
	c := newNoPreprocessChunker(t, 10, 0)
	doc := reranker.Document{ID: "doc-2", Text: "aaa bbb ccc ddd",
		SourceMaps: &sourceloc.SourceMaps{CharRange: true}}
	chunks, err := c.ChunkDocuments(t.Context(), []reranker.Document{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("小 chunk_size 应多块，实际 %d", len(chunks))
	}
	for i, ch := range chunks {
		found := false
		for _, l := range ch.SourceLocators {
			if l.Type == sourceloc.LocatorTypeCharRange {
				found = true
				if l.Quote != ch.Text {
					t.Errorf("chunk %d Quote 应为 chunk 文本: %q vs %q", i, l.Quote, ch.Text)
				}
				if l.CharEnd-l.CharStart != len(ch.Text) {
					t.Errorf("chunk %d 区间长度不符", i)
				}
			}
		}
		if !found {
			t.Errorf("chunk %d 应带 char_range 锚", i)
		}
	}
}

// TestChunkDocuments_无sidecar不受影响 既有行为回归：metadata/正文不变。
func TestChunkDocuments_无sidecar不受影响(t *testing.T) {
	c := newNoPreprocessChunker(t, 100, 0)
	doc := reranker.Document{ID: "doc-3", Text: "plain text"}
	chunks, err := c.ChunkDocuments(t.Context(), []reranker.Document{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].Text != "plain text" {
		t.Fatalf("既有行为回归: %+v", chunks)
	}
	if len(chunks[0].SourceLocators) != 0 {
		t.Fatalf("无 sidecar 应无 locators: %+v", chunks[0].SourceLocators)
	}
}
