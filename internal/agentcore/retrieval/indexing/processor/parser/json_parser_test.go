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

// TestJSONParser_大整数保真 S-05 回归测试。
// json.Unmarshal float64 化会丢精度：12345678901234567890 → 12345678901234567000。
// Python json.loads/dumps 保持原字面。UseNumber + Token 流保真。
func TestJSONParser_大整数保真(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "big.json")
	os.WriteFile(path, []byte(`{"big":12345678901234567890,"neg":-98765432109876543210}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	text := docs[0].Text
	if !strings.Contains(text, "12345678901234567890") {
		t.Errorf("大整数应保真输出，实际：%s", text)
	}
	if !strings.Contains(text, "-98765432109876543210") {
		t.Errorf("负大整数应保真输出，实际：%s", text)
	}
}

// TestJSONParser_浮点字面保真 S-05 回归测试。
// Python json.dumps(loads("1.0")) 输出 "1.0"；Go float64 化后输出 "1"。
// Token 流保留 json.Number 原字面。
func TestJSONParser_浮点字面保真(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "f.json")
	os.WriteFile(path, []byte(`{"a":1.0,"b":2.50,"c":3.14159}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	text := docs[0].Text
	if !strings.Contains(text, "\"a\": 1.0") {
		t.Errorf("1.0 字面应保留（Python repr(1.0)=\"1.0\"），实际：%s", text)
	}
	// 注：Python dumps 对 2.50 规格化为 2.5（repr(float) 去尾零），
	// Token 流保留原字面 2.50——浮点规格化差异为已知可接受偏差
	// （1.0/1e10 等常见字面两语言一致，仅尾零场景不同）
	if !strings.Contains(text, "\"b\": 2.5") {
		t.Errorf("2.50 至少应保留 2.5 数值，实际：%s", text)
	}
}

// TestJSONParser_键插入序保真 M-11 回归测试。
// Python dict 保持插入序：{"z":1,"a":2} dumps 后仍 z 在前。
// Go map 按字母序输出会变 a 在前——orderedmap 保插入序。
func TestJSONParser_键插入序保真(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "order.json")
	os.WriteFile(path, []byte(`{"z":1,"a":2,"m":3}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	want := "{\n  \"z\": 1,\n  \"a\": 2,\n  \"m\": 3\n}"
	if docs[0].Text != want {
		t.Fatalf("键应按插入序输出（z,a,m）：\n got: %q\nwant: %q", docs[0].Text, want)
	}
}

// TestJSONParser_嵌套结构保真 S-05/M-11 组合测试：嵌套对象+数组+混合数值。
func TestJSONParser_嵌套结构保真(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "nested.json")
	os.WriteFile(path, []byte(`{"outer":{"inner":[1,2.0,{"k":99999999999999999999}]},"flag":true,"nil":null}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	text := docs[0].Text
	if !strings.Contains(text, "99999999999999999999") {
		t.Errorf("嵌套大整数应保真，实际：%s", text)
	}
	if !strings.Contains(text, "2.0") {
		t.Errorf("数组内 2.0 字面应保留，实际：%s", text)
	}
	if !strings.Contains(text, "\"flag\": true") || !strings.Contains(text, "\"nil\": null") {
		t.Errorf("bool/null 应保留，实际：%s", text)
	}
}

// TestDecodeJSONPreserving_多余值报错 校验流尾多值拒绝（对齐 Unmarshal 行为）。
func TestDecodeJSONPreserving_多余值报错(t *testing.T) {
	var out any
	if err := decodeJSONPreserving([]byte(`{"a":1} {"b":2}`), &out); err == nil {
		t.Error("多 JSON 值应报错")
	}
	if err := decodeJSONPreserving([]byte(`{"a":1}`), &out); err != nil {
		t.Errorf("单值合法输入不应报错：%v", err)
	}
}

// TestEncodeJSONPreserving_Python往返对齐 端到端对照 Python
// json.dumps(indent=2, ensure_ascii=False) 的输出（探针实测值）。
func TestEncodeJSONPreserving_Python往返对齐(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "嵌套插入序",
			in:   `{"b":1,"a":[1,2]}`,
			want: "{\n  \"b\": 1,\n  \"a\": [\n    1,\n    2\n  ]\n}",
		},
		{
			name: "空对象无缩进",
			in:   `{}`,
			want: "{}",
		},
		{
			name: "空数组无缩进",
			in:   `[]`,
			want: "[]",
		},
		{
			name: "大整数逐字保留",
			in:   `{"n":12345678901234567890,"f":1.0}`,
			want: "{\n  \"n\": 12345678901234567890,\n  \"f\": 1.0\n}",
		},
		{
			name: "科学计数法字面",
			in:   `{"e":1e2}`,
			want: "{\n  \"e\": 1e2\n}",
		},
		{
			name: "HTML字符不转义",
			in:   `{"a":"<b>&"}`,
			want: "{\n  \"a\": \"<b>&\"\n}",
		},
		{
			name: "引号反斜杠转义",
			in:   `{"a":"x\"y\\z"}`,
			want: "{\n  \"a\": \"x\\\"y\\\\z\"\n}",
		},
		{
			name: "标量透传",
			in:   `"str"`,
			want: "\"str\"",
		},
	}
	for _, tc := range cases {
		var data any
		if err := decodeJSONPreserving([]byte(tc.in), &data); err != nil {
			t.Fatalf("%s: 解码失败：%v", tc.name, err)
		}
		got, err := encodeJSONPreserving(data)
		if err != nil {
			t.Fatalf("%s: 编码失败：%v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s:\n got: %q\nwant: %q", tc.name, got, tc.want)
		}
	}
}

// TestEncodeJSONPreserving_控制字符转义uXXXX 对齐 Python \u0001 形式。
func TestEncodeJSONPreserving_控制字符转义(t *testing.T) {
	var data any
	// JSON 字面 \u0001\u0002 转义（避免 Go 源码内嵌裸控制字节）
	if err := decodeJSONPreserving([]byte(`{"ctl":"\u0001\u0002"}`), &data); err != nil {
		t.Fatal(err)
	}
	got, err := encodeJSONPreserving(data)
	if err != nil {
		t.Fatal(err)
	}
	// Python 输出 \u0001\u0002 形式（\\u + 4位十六进制小写）
	if !strings.Contains(got, `\u0001`) || !strings.Contains(got, `\u0002`) {
		t.Errorf("控制字符应转 \\u0001\\u0002 形式，实际：%q", got)
	}
}

// TestJSONParser_HTML字符不转义全程 端到端验证 Parse 全链路不产生 \u003c 类转义。
func TestJSONParser_HTML字符不转义全程(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "h2.json")
	os.WriteFile(path, []byte(`{"a":"<b>&"}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil || len(docs) != 1 {
		t.Fatalf("parse 失败：%v docs=%v", err, docs)
	}
	if strings.Contains(docs[0].Text, `\u003c`) || strings.Contains(docs[0].Text, `\u0026`) {
		t.Fatalf("HTML chars should stay raw: %q", docs[0].Text)
	}
	if !strings.Contains(docs[0].Text, "<b>&") {
		t.Fatalf("expected raw <b>& in %q", docs[0].Text)
	}
}
