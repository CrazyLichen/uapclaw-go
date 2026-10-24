package parser

import (
	"bytes"
	"testing"
)

// TestDocxLoadStyles 测试 docx zip 解包与 styles.xml 解析。
//
// 用真实 fixture（python-docx 产出）验证 styleId → w:name 映射表。
func TestDocxLoadStyles(t *testing.T) {
	dm, err := loadDocxModel("testdata/fixture_word.docx")
	if err != nil {
		t.Fatalf("loadDocxModel: %v", err)
	}
	if got := dm.styleName("Title"); got != "Title" {
		t.Errorf("styleName(Title) = %q, want %q", got, "Title")
	}
	if got := dm.styleName("Heading1"); got != "heading 1" {
		t.Errorf("styleName(Heading1) = %q, want %q（原始 w:name，BabelFish 在外层）", got, "heading 1")
	}
	if got := dm.styleName("NonExistent"); got != "" {
		t.Errorf("styleName(NonExistent) = %q, want empty", got)
	}
}

// TestDocxLoadStylesRel 测试 rels 解析（rId → media target 映射）。
func TestDocxLoadStylesRel(t *testing.T) {
	dm, err := loadDocxModel("testdata/fixture_word.docx")
	if err != nil {
		t.Fatalf("loadDocxModel: %v", err)
	}
	// fixture 含图片（rId10 → media/image1.png，hyperlink rId 在同表）
	if got := dm.mediaTarget("rId10"); got != "media/image1.png" {
		t.Errorf("mediaTarget(rId10) = %q, want media/image1.png", got)
	}
	if got := dm.mediaTarget("rId999"); got != "" {
		t.Errorf("mediaTarget(rId999) = %q, want empty", got)
	}
	// media 字节可按 rels target 查到（word/ 前缀拼接）
	if data, ok := dm.mediaBytes("rId10"); !ok || len(data) == 0 {
		t.Errorf("mediaBytes(rId10) 应返回非空字节（图片存在）")
	}
}

// TestDocxLoadModel_坏文件 测试 zip 打开失败路径。
func TestDocxLoadModel_坏文件(t *testing.T) {
	if _, err := loadDocxModel("testdata/不存在.docx"); err == nil {
		t.Error("文件不存在应返回 error")
	}
	dir := t.TempDir()
	if _, err := loadDocxModel(dir); err == nil {
		t.Error("非 zip 文件应返回 error")
	}
}

// TestDocxParseBlocks 测试保序块遍历 + paragraph.text 语义 + 样式名查询。
//
// 用 fixture_word.docx（含 Title/Heading/tab/br/hyperlink/图片/表格）对拍 python-docx 输出。
func TestDocxParseBlocks(t *testing.T) {
	dm, err := loadDocxModel("testdata/fixture_word.docx")
	if err != nil {
		t.Fatalf("loadDocxModel: %v", err)
	}
	blocks := dm.parseBlocks()

	// 块序（python-docx iter_inner_content 保序）：
	// Title 段 / H1 段 / H2 段 / tab+br 段 / hyperlink 段 / plain 段 /
	// 图片段 / vMerge 表 / gridSpan 表
	if len(blocks) != 9 {
		t.Fatalf("blocks = %d, want 9（实际: %v）", len(blocks), summarizeBlocks(blocks))
	}

	// ① 样式名（pStyle → w:name → BabelFish UI 名）
	if blocks[0].styleUI != "Title" {
		t.Errorf("blocks[0].styleUI = %q, want Title", blocks[0].styleUI)
	}
	if blocks[1].styleUI != "Heading 1" {
		t.Errorf("blocks[1].styleUI = %q, want 'Heading 1'", blocks[1].styleUI)
	}
	if blocks[2].styleUI != "Heading 2" {
		t.Errorf("blocks[2].styleUI = %q, want 'Heading 2'", blocks[2].styleUI)
	}
	// 普通段落无样式（空串）
	if blocks[5].styleUI != "" {
		t.Errorf("blocks[5].styleUI = %q, want empty（普通段落无 pStyle）", blocks[5].styleUI)
	}

	// ② paragraph.text（tab/br/hyperlink 语义）
	if blocks[3].text != "before\tafter_tab\nafter_br" {
		t.Errorf("blocks[3].text = %q, want %q", blocks[3].text, "before\tafter_tab\nafter_br")
	}
	if blocks[4].text != "link text" {
		t.Errorf("blocks[4].text = %q, want %q", blocks[4].text, "link text")
	}
	if blocks[5].text != "plain paragraph" {
		t.Errorf("blocks[5].text = %q, want %q", blocks[5].text, "plain paragraph")
	}

	// ③ 图片段（无文本但 paraXML 含 blip）
	if blocks[6].text != "" {
		t.Errorf("blocks[6].text = %q, want empty（带图段落无文字）", blocks[6].text)
	}
	if !bytes.Contains(blocks[6].paraXML, []byte("blip")) {
		t.Errorf("blocks[6].paraXML 应含 blip（图片段），实际: %s", blocks[6].paraXML)
	}

	// ④ 表格块 kind 与段落 kind
	if blocks[7].kind != "table" || blocks[8].kind != "table" {
		t.Errorf("blocks[7/8].kind = %q/%q, want table/table", blocks[7].kind, blocks[8].kind)
	}
}

// summarizeBlocks 测试辅助：块摘要。
func summarizeBlocks(blocks []docxBlock) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.kind+":"+b.text)
	}
	return out
}
