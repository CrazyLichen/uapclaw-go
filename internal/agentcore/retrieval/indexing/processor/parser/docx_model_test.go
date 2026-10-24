package parser

import (
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
