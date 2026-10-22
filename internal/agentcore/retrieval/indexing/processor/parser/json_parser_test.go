package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewJSONParser 构造成功。
func TestNewJSONParser(t *testing.T) {
	p, err := NewJSONParser()
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil parser")
	}
}

// TestJSONParser_成功解析 JSON 对象缩进两空格且保 Unicode（对齐 indent=2 + ensure_ascii=False）。
func TestJSONParser_成功解析(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "t.json")
	if err := os.WriteFile(path, []byte(`{"name":"测试","value":123}`), 0o600); err != nil {
		t.Fatal(err)
	}
	docs, err := p.Parse(context.Background(), path, WithDocID("doc_1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].ID != "doc_1" {
		t.Fatalf("expected ID doc_1, got %q", docs[0].ID)
	}
	want := "{\n  \"name\": \"测试\",\n  \"value\": 123\n}"
	if docs[0].Text != want {
		t.Fatalf("text mismatch:\n got: %q\nwant: %q", docs[0].Text, want)
	}
}

// TestJSONParser_空对象 输出字面 {}。
func TestJSONParser_空对象(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "e.json")
	os.WriteFile(path, []byte(`{}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Text != "{}" {
		t.Fatalf("expected {}, got %v", docs)
	}
}

// TestJSONParser_数组 数组合法 JSON 序列化保留全部元素。
func TestJSONParser_数组(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "a.json")
	os.WriteFile(path, []byte(`[1,2,3,"test"]`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	for _, s := range []string{"1", "2", "test"} {
		if !strings.Contains(docs[0].Text, s) {
			t.Fatalf("expected %q in %q", s, docs[0].Text)
		}
	}
}

// TestJSONParser_非法JSON返回原文 格式错误分支返回原始内容（对齐 JSONDecodeError 分支）。
func TestJSONParser_非法JSON返回原文(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(path, []byte("{ invalid json }"), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Text != "{ invalid json }" {
		t.Fatalf("expected raw content, got %v", docs)
	}
}

// TestJSONParser_HTML字符不转义 SetEscapeHTML(false) 对齐 ensure_ascii=False 的原样输出。
func TestJSONParser_HTML字符不转义(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "h.json")
	os.WriteFile(path, []byte(`{"a":"<b>&"}`), 0o600)
	docs, _ := p.Parse(context.Background(), path)
	if len(docs) != 1 {
		t.Fatal("expected 1 doc")
	}
	if strings.Contains(docs[0].Text, `\u003c`) || strings.Contains(docs[0].Text, `&lt;`) {
		t.Fatalf("HTML chars should stay raw: %q", docs[0].Text)
	}
	if !strings.Contains(docs[0].Text, "<b>&") {
		t.Fatalf("expected raw <b>& in %q", docs[0].Text)
	}
}

// TestJSONParser_文件不存在软失败 空切片 nil err（对齐 Python except → return None）。
func TestJSONParser_文件不存在软失败(t *testing.T) {
	p, _ := NewJSONParser()
	docs, err := p.Parse(context.Background(), "/nonexistent/path.json")
	if err != nil {
		t.Fatalf("expected nil err (soft fail), got %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected empty docs, got %v", docs)
	}
}

// TestJSONParser_registry注册 init 注册后工厂可实例化并完成解析。
func TestJSONParser_registry注册(t *testing.T) {
	f, ok := getParserFactory(".json")
	if !ok {
		t.Fatal("expected .json registered by init()")
	}
	p := f()
	if _, isJSON := p.(*JSONParser); !isJSON {
		t.Fatalf("expected *JSONParser, got %T", p)
	}
	path := filepath.Join(t.TempDir(), "r.json")
	os.WriteFile(path, []byte(`{"k":"v"}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil || len(docs) != 1 {
		t.Fatalf("registry instance parse failed: %v %v", docs, err)
	}
}

// TestJSONParser_Supports默认false 未覆写 Supports（13.11 AutoFileParser 才覆写）。
func TestJSONParser_Supports默认false(t *testing.T) {
	p, _ := NewJSONParser()
	if p.Supports("whatever.json") {
		t.Fatal("expected default false")
	}
}
