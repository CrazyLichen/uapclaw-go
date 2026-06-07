package service_api

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"testing"
)

// ──────────────────────────── JsonResponseParser 测试 ────────────────────────────

// TestJsonResponseParser_CanParse 测试 JSON 解析器的 content-type 识别
func TestJsonResponseParser_CanParse(t *testing.T) {
	parser := JsonResponseParser{}

	tests := []struct {
		name        string
		contentType string
		statusCode  int
		headers     map[string]string
		expected    bool
	}{
		{"application/json", "application/json", 200, nil, true},
		{"text/json", "text/json", 200, nil, true},
		{"text/x-json", "text/x-json", 200, nil, true},
		{"application/javascript", "application/javascript", 200, nil, true},
		{"+json后缀", "application/video+json", 200, nil, true},
		{"+hal+json", "application/hal+json", 200, nil, true},
		{"子字符串匹配", "application/json; charset=utf-8", 200, nil, true},
		{"text/plain", "text/plain", 200, nil, false},
		{"无Content-Type但有Accept", "", 200, map[string]string{"Accept": "application/json"}, true},
		{"无Content-Type无Accept", "", 200, nil, false},
		{"空字符串Accept", "", 200, map[string]string{"Accept": ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.CanParse(tt.contentType, tt.statusCode, tt.headers)
			if got != tt.expected {
				t.Errorf("CanParse(%q, %d, %v) = %v, 期望 %v", tt.contentType, tt.statusCode, tt.headers, got, tt.expected)
			}
		})
	}
}

// TestJsonResponseParser_Parse 测试 JSON 解析
func TestJsonResponseParser_Parse(t *testing.T) {
	parser := JsonResponseParser{}

	// 正常 JSON
	data := []byte(`{"key": "value", "num": 42}`)
	result, err := parser.Parse(data, nil)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["key"] != "value" {
		t.Errorf("key: 期望 value，实际 %v", m["key"])
	}

	// 空数据
	result, err = parser.Parse([]byte{}, nil)
	if err != nil {
		t.Fatalf("空数据 Parse 失败: %v", err)
	}
	if m2, ok := result.(map[string]any); !ok || len(m2) != 0 {
		t.Errorf("空数据应返回空 map: %v", result)
	}
}

// TestJsonResponseParser_Parse_无效JSON 测试无效 JSON 解析
func TestJsonResponseParser_Parse_无效JSON(t *testing.T) {
	parser := JsonResponseParser{}
	_, err := parser.Parse([]byte(`{invalid}`), nil)
	if err == nil {
		t.Error("无效 JSON 应返回错误")
	}
}

// ──────────────────────────── TextResponseParser 测试 ────────────────────────────

// TestTextResponseParser_CanParse 测试文本解析器的 content-type 识别
func TestTextResponseParser_CanParse(t *testing.T) {
	parser := TextResponseParser{}

	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{"text/plain", "text/plain", true},
		{"text/html", "text/html", true},
		{"text/xml", "text/xml", true},
		{"application/xml", "application/xml", true},
		{"text/csv", "text/csv", true},
		{"text/javascript", "text/javascript", true},
		{"application/javascript", "application/javascript", true},
		{"application/json", "application/json", false},
		{"空contentType", "", false},
		{"text/自定义", "text/custom-type", true},
		{"含xml不含json", "application/some+xml", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.CanParse(tt.contentType, 200, nil)
			if got != tt.expected {
				t.Errorf("CanParse(%q) = %v, 期望 %v", tt.contentType, got, tt.expected)
			}
		})
	}
}

// TestTextResponseParser_Parse 测试文本解析
func TestTextResponseParser_Parse(t *testing.T) {
	parser := TextResponseParser{}

	data := []byte("hello world")
	result, err := parser.Parse(data, nil)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if result != "hello world" {
		t.Errorf("期望 hello world，实际 %v", result)
	}

	// 空数据
	result, err = parser.Parse([]byte{}, nil)
	if err != nil {
		t.Fatalf("空数据 Parse 失败: %v", err)
	}
	if result != "" {
		t.Errorf("空数据应返回空字符串: %v", result)
	}
}

// ──────────────────────────── Decompressor 测试 ────────────────────────────

// TestGzipDecompressor 测试 GZIP 解压
func TestGzipDecompressor(t *testing.T) {
	d := GzipDecompressor{}

	if !d.CanDecompress("gzip") {
		t.Error("应支持 gzip")
	}
	if !d.CanDecompress("x-gzip") {
		t.Error("应支持 x-gzip")
	}
	if d.CanDecompress("deflate") {
		t.Error("不应支持 deflate")
	}

	// 压缩测试数据
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, _ = writer.Write([]byte("hello gzip"))
	_ = writer.Close()

	decompressed, err := d.Decompress(buf.Bytes())
	if err != nil {
		t.Fatalf("Decompress 失败: %v", err)
	}
	if string(decompressed) != "hello gzip" {
		t.Errorf("期望 hello gzip，实际 %s", decompressed)
	}
}

// TestDeflateDecompressor 测试 Deflate 解压
func TestDeflateDecompressor(t *testing.T) {
	d := DeflateDecompressor{}

	if !d.CanDecompress("deflate") {
		t.Error("应支持 deflate")
	}
	if d.CanDecompress("gzip") {
		t.Error("不应支持 gzip")
	}

	// 压缩测试数据
	var buf bytes.Buffer
	writer, _ := flate.NewWriter(&buf, flate.DefaultCompression)
	_, _ = writer.Write([]byte("hello deflate"))
	_ = writer.Close()

	decompressed, err := d.Decompress(buf.Bytes())
	if err != nil {
		t.Fatalf("Decompress 失败: %v", err)
	}
	if string(decompressed) != "hello deflate" {
		t.Errorf("期望 hello deflate，实际 %s", decompressed)
	}
}

// TestParserRegistry_Register 测试注册自定义解析器
func TestParserRegistry_Register(t *testing.T) {
	registry := GetParserRegistry()

	// 注册自定义解析器
	customParser := &customTestParser{}
	registry.Register(customParser)

	// 验证可以解析 custom content-type
	result, err := registry.Parse(
		map[string]string{"Content-Type": "application/x-custom"},
		[]byte("custom data"),
		200,
	)
	if err != nil {
		t.Fatalf("自定义解析器应能解析: %v", err)
	}
	if result != "custom:custom data" {
		t.Errorf("自定义解析器结果: 期望 custom:custom data，实际 %v", result)
	}
}

// TestParserRegistry_RegisterDecompressor 测试注册自定义解压器
func TestParserRegistry_RegisterDecompressor(t *testing.T) {
	registry := GetParserRegistry()
	registry.RegisterDecompressor("br", &customTestDecompressor{})
}

// customTestParser 用于测试的自定义解析器
type customTestParser struct{}

func (p *customTestParser) CanParse(contentType string, statusCode int, headers map[string]string) bool {
	return contentType == "application/x-custom"
}

func (p *customTestParser) Parse(data []byte, headers map[string]string) (any, error) {
	return "custom:" + string(data), nil
}

// customTestDecompressor 用于测试的自定义解压器
type customTestDecompressor struct{}

func (d *customTestDecompressor) CanDecompress(encoding string) bool {
	return encoding == "br"
}

func (d *customTestDecompressor) Decompress(data []byte) ([]byte, error) {
	return data, nil
}

// ──────────────────────────── ParserRegistry 测试 ────────────────────────────

// TestParserRegistry_Parse_JSON 测试 ParserRegistry 解析 JSON
func TestParserRegistry_Parse_JSON(t *testing.T) {
	registry := GetParserRegistry()

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	data := []byte(`{"message": "hello"}`)

	result, err := registry.Parse(headers, data, 200)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["message"] != "hello" {
		t.Errorf("message: 期望 hello，实际 %v", m["message"])
	}
}

// TestParserRegistry_Parse_Text 测试 ParserRegistry 解析文本
func TestParserRegistry_Parse_Text(t *testing.T) {
	registry := GetParserRegistry()

	headers := map[string]string{
		"Content-Type": "text/plain",
	}
	data := []byte("hello text")

	result, err := registry.Parse(headers, data, 200)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if result != "hello text" {
		t.Errorf("期望 hello text，实际 %v", result)
	}
}

// TestParserRegistry_Parse_未知ContentType 测试未知 content-type 报错
func TestParserRegistry_Parse_未知ContentType(t *testing.T) {
	registry := GetParserRegistry()

	headers := map[string]string{
		"Content-Type": "application/octet-stream",
	}
	data := []byte("binary data")

	_, err := registry.Parse(headers, data, 200)
	if err == nil {
		t.Error("未知 content-type 应返回错误")
	}
}

// TestParserRegistry_Parse_GzipJSON 测试 Gzip 压缩的 JSON 解析
func TestParserRegistry_Parse_GzipJSON(t *testing.T) {
	registry := GetParserRegistry()

	// 压缩 JSON 数据
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, _ = writer.Write([]byte(`{"compressed": true}`))
	_ = writer.Close()

	headers := map[string]string{
		"Content-Type":     "application/json",
		"Content-Encoding": "gzip",
	}

	result, err := registry.Parse(headers, buf.Bytes(), 200)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际 %T", result)
	}
	if m["compressed"] != true {
		t.Errorf("compressed: 期望 true，实际 %v", m["compressed"])
	}
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestExtractCharset 测试从 Content-Type 提取 charset
func TestExtractCharset(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    string
	}{
		{"含charset", "application/json; charset=utf-8", "utf-8"},
		{"charset带引号", "text/html; charset=\"iso-8859-1\"", "iso-8859-1"},
		{"无charset", "application/json", ""},
		{"空字符串", "", ""},
		{"大写Charset", "text/plain; Charset=UTF-8", "UTF-8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCharset(tt.contentType)
			if got != tt.expected {
				t.Errorf("extractCharset(%q) = %q, 期望 %q", tt.contentType, got, tt.expected)
			}
		})
	}
}
