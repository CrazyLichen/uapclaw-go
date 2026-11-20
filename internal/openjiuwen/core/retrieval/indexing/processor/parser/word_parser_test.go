package parser

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestWordParserParse_golden对拍 测试 WordParser 全链对拍黄金基准。
func TestWordParserParse_golden对拍(t *testing.T) {
	p, err := NewWordParser()
	if err != nil {
		t.Fatalf("NewWordParser: %v", err)
	}
	docs, err := p.Parse(context.Background(), "testdata/fixture_word.docx")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1（WordParser 单文档产出）", len(docs))
	}
	// golden 对拍（fixture_word.golden.json 的 result 字段）
	want := loadGoldenString(t, "testdata/fixture_word.golden.json")
	if docs[0].Text != want {
		t.Errorf("Text 不匹配 golden:\n got: %q\nwant: %q", docs[0].Text, want)
	}
}

// TestWordParserParse_软失败 测试整体软失败（文件不存在/坏 docx → 空切片 nil error）。
func TestWordParserParse_软失败(t *testing.T) {
	p, _ := NewWordParser()
	docs, err := p.Parse(context.Background(), "testdata/不存在.docx")
	if err != nil {
		t.Fatalf("软失败不返回 error: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("软失败返回空切片, got %d docs", len(docs))
	}
}

// TestWordParserParse_空文档nil 测试空结果判定（"\n".join 空串 → None → 空切片）。
func TestWordParserParse_空文档nil(t *testing.T) {
	// 造一个 body 空的 docx（zip 只有 document.xml 空模板）
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.docx")
	if err := writeMinimalDocx(path, "<w:body/>"); err != nil {
		t.Fatalf("writeMinimalDocx: %v", err)
	}
	p, _ := NewWordParser()
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("空文档不返回 error: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("空文档 → 空切片, got %d docs", len(docs))
	}
}

// TestWordParserSupports_默认false 测试不覆写 Supports（继承 base False）。
func TestWordParserSupports_默认false(t *testing.T) {
	p, _ := NewWordParser()
	if p.Supports("x.docx") {
		t.Error("WordParser 不覆写 Supports（Python 无 supports 覆写，继承默认 False）")
	}
}

// TestWordParserParse_图片落盘 测试全链图片落盘（captioner 禁用态：无 caption 文本产出）。
func TestWordParserParse_图片落盘(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(oldWd)

	p, _ := NewWordParser()
	docs, err := p.Parse(context.Background(), filepath.Join(oldWd, "testdata/fixture_word.docx"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	// 图片落盘 images/{filename}__para_6__img_0.png（相对路径 savedImageDir）
	wantPath := filepath.Join("images", "fixture_word.docx__para_6__img_0.png")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("图片应落盘 %s: %v", wantPath, err)
	}
	// captioner 禁用态（nil LLM）不产出 caption 文本——golden 已锁定无 caption 行
}

// loadGoldenString 加载黄金基准 JSON 的 result 字符串。
func loadGoldenString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var g struct {
		Result *string `json:"result"`
	}
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}
	if g.Result == nil {
		return ""
	}
	return *g.Result
}

// writeMinimalDocx 写最小 docx zip（document.xml 为给定 body XML）。
func writeMinimalDocx(path, bodyXML string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="` + docxMainNS + `">` + bodyXML + `</w:document>`
	w, err := zw.Create("word/document.xml")
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(docXML)); err != nil {
		return err
	}
	return zw.Close()
}
