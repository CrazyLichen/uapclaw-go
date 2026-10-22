package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gbkArticle GBK 编码的中文文章字节（长样本保证 chardet conf=100）。
// 内容："中国的传统文化悠久以来，在历史长河中形成了独特的美" × 16
// （fixture 经 Python '<内容>'.encode('gbk') 交叉验证逐字节一致）
var gbkArticle = bytesRepeat([]byte{
	0xD6, 0xD0, 0xB9, 0xFA, 0xB5, 0xC4, 0xB4, 0xAB, 0xCD, 0xB3, 0xCE, 0xC4, 0xBB, 0xAF, 0xD3, 0xC6,
	0xBE, 0xC3, 0xD2, 0xD4, 0xC0, 0xB4, 0xA3, 0xAC, 0xD4, 0xDA, 0xC0, 0xFA, 0xCA, 0xB7, 0xB3, 0xA4,
	0xBA, 0xD3, 0xD6, 0xD0, 0xD0, 0xCE, 0xB3, 0xC9, 0xC1, 0xCB, 0xB6, 0xC0, 0xCC, 0xD8, 0xB5, 0xC4,
	0xC3, 0xC0,
}, 16)

// big5Article Big5 编码的"這是一個中文測試" × 20（长样本；fixture 经
// Python '這是一個中文測試'.encode('big5') 交叉验证：b36f ac4f a440 add3 ...）。
var big5Article = bytesRepeat([]byte{
	0xB3, 0x6F, 0xAC, 0x4F, 0xA4, 0x40, 0xAD, 0xD3, 0xA4, 0xA4, 0xA4, 0xE5, 0xB4, 0xFA, 0xB8, 0xD5,
}, 20)

// bytesRepeat 复制字节切片 n 次。
func bytesRepeat(b []byte, n int) []byte {
	out := make([]byte, 0, len(b)*n)
	for i := 0; i < n; i++ {
		out = append(out, b...)
	}
	return out
}

// TestNewTxtMdParser 构造成功。
func TestNewTxtMdParser(t *testing.T) {
	p, err := NewTxtMdParser()
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil parser")
	}
}

// TestTxtMdParser_UTF8中文 UTF-8 文件原样解析（chardet 检出 UTF-8 走零转换路径）。
func TestTxtMdParser_UTF8中文(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "u.txt")
	os.WriteFile(path, []byte("你好世界，这是一个中文测试"), 0o600)
	docs, err := p.Parse(context.Background(), path, WithDocID("d1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != "d1" {
		t.Fatalf("expected 1 doc d1, got %v", docs)
	}
	if docs[0].Text != "你好世界，这是一个中文测试" {
		t.Fatalf("got %q", docs[0].Text)
	}
}

// TestTxtMdParser_GBK解码 chardet 核心价值场景：GBK 文件正确解码为中文。
func TestTxtMdParser_GBK解码(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "g.txt")
	os.WriteFile(path, gbkArticle, 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	// GBK 解码后必须包含中文（不能是乱码问号或 U+FFFD）
	if !strings.Contains(docs[0].Text, "中国的传统文化") {
		n := 40
		if len(docs[0].Text) < n {
			n = len(docs[0].Text)
		}
		t.Fatalf("GBK decode failed, got: %q", docs[0].Text[:n])
	}
}

// TestTxtMdParser_Big5解码 Big5 长样本正确解码。
func TestTxtMdParser_Big5解码(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "b.txt")
	os.WriteFile(path, big5Article, 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if !strings.Contains(docs[0].Text, "這是一個中文測試") {
		n := 40
		if len(docs[0].Text) < n {
			n = len(docs[0].Text)
		}
		t.Fatalf("Big5 decode failed, got: %q", docs[0].Text[:n])
	}
}

// TestTxtMdParser_纯ASCII ASCII 文件原样解析。
func TestTxtMdParser_纯ASCII(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(path, []byte("plain ascii text"), 0o600)
	docs, _ := p.Parse(context.Background(), path)
	if len(docs) != 1 || docs[0].Text != "plain ascii text" {
		t.Fatalf("got %v", docs)
	}
}

// TestTxtMdParser_strip前后空白 对齐 content.strip()。
func TestTxtMdParser_strip前后空白(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "s.txt")
	os.WriteFile(path, []byte("   \n  Content  \n   "), 0o600)
	docs, _ := p.Parse(context.Background(), path)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Text != "Content" {
		t.Fatalf("expected stripped 'Content', got %q", docs[0].Text)
	}
}

// TestTxtMdParser_空文件空切片 对齐 content falsy → None → []。
func TestTxtMdParser_空文件空切片(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "e.txt")
	os.WriteFile(path, []byte(""), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected empty docs, got %v", docs)
	}
}

// TestTxtMdParser_文件不存在软失败 空切片 nil err（对齐 Python except → return None）。
func TestTxtMdParser_文件不存在软失败(t *testing.T) {
	p, _ := NewTxtMdParser()
	docs, err := p.Parse(context.Background(), "/nonexistent/path.txt")
	if err != nil {
		t.Fatalf("expected nil err (soft fail), got %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected empty docs, got %v", docs)
	}
}

// TestTxtMdParser_UTF8BOM剥离 BOM 前缀不残留（spec §4.3 承诺固化）。
func TestTxtMdParser_UTF8BOM剥离(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "bom.txt")
	os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, []byte("BOM content")...), 0o600)
	docs, _ := p.Parse(context.Background(), path)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Text != "BOM content" {
		t.Fatalf("BOM should be stripped, got %q", docs[0].Text)
	}
}

// TestTxtMdParser_registry注册 init 注册全部六扩展名。
func TestTxtMdParser_registry注册(t *testing.T) {
	for _, ext := range []string{".txt", ".md", ".markdown"} {
		f, ok := getParserFactory(ext)
		if !ok {
			t.Fatalf("expected %s registered by init()", ext)
		}
		p := f()
		if _, isTxt := p.(*TxtMdParser); !isTxt {
			t.Fatalf("expected *TxtMdParser for %s, got %T", ext, p)
		}
	}
}

// TestTxtMdParser_Supports默认false 未覆写 Supports（13.11 AutoFileParser 才覆写）。
func TestTxtMdParser_Supports默认false(t *testing.T) {
	p, _ := NewTxtMdParser()
	if p.Supports("whatever.txt") {
		t.Fatal("expected default false")
	}
}
