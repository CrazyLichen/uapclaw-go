package parser

import (
	"strings"
	"testing"
)

// ──────────────────────────── pdfcoord 测试（30.3 Task2） ────────────────────────────

// fixturePDF fixture 路径（与 gen_pdf_fixture_test.go 生成产物一致）
const fixturePDF = "testdata/fixture_pdf_multipage.pdf"

func TestExtractPDFPages_词与图(t *testing.T) {
	pages, err := extractPDFPages(fixturePDF)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("页数 = %d, want 2", len(pages))
	}
	// 第 1 页：词序断言（空白差异放宽为词序子序列）
	texts := make([]string, 0, len(pages[0].Words))
	for _, w := range pages[0].Words {
		texts = append(texts, w.Text)
	}
	if !containsSubsequence(texts, "Alpha", "Bravo", "Charlie") {
		t.Fatalf("第 1 页英文词序不符: %v", texts)
	}
	if !containsSubsequence(texts, "KB", "locator", "fixture", "page", "one") {
		t.Fatalf("第 1 页第二行词缺失: %v", texts)
	}
	for _, w := range pages[0].Words {
		if w.BBox[0] < 0 || w.BBox[2] <= w.BBox[0] || w.BBox[3] <= w.BBox[1] {
			t.Fatalf("词框非法: %+v", w)
		}
	}
	// 第 2 页：一行文本 + 一张嵌入图（placement 100,100→200,200）
	if !containsSubsequence(wordsOf(pages[1]), "Delta", "Echo", "on", "page", "two") {
		t.Fatalf("第 2 页词序不符: %v", wordsOf(pages[1]))
	}
	if len(pages[1].Images) != 1 {
		t.Fatalf("第 2 页图片数 = %d, want 1", len(pages[1].Images))
	}
	img := pages[1].Images[0]
	if len(img.Data) == 0 || img.BBox[2] <= img.BBox[0] || img.BBox[3] <= img.BBox[1] {
		t.Fatalf("图片数据/placement 非法: bbox=%v len=%d", img.BBox, len(img.Data))
	}
	if img.BBox[0] < 95 || img.BBox[0] > 105 || img.BBox[2] < 195 || img.BBox[2] > 205 {
		t.Fatalf("placement 与写入值偏差过大: %v", img.BBox)
	}
}

// wordsOf 提取页词文本列表
func wordsOf(p pdfPageData) []string {
	out := make([]string, 0, len(p.Words))
	for _, w := range p.Words {
		out = append(out, w.Text)
	}
	return out
}

// containsSubsequence 判定 texts 是否按序包含全部子项（相邻等价）
func containsSubsequence(texts []string, subs ...string) bool {
	joined := strings.Join(texts, "\x00")
	pos := 0
	for _, sub := range subs {
		idx := strings.Index(joined[pos:], sub)
		if idx < 0 {
			return false
		}
		pos += idx + len(sub)
	}
	return true
}
