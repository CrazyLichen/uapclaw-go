//go:build test

package filesystem

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGetFileReadState_不存在 测试获取不存在的状态
func TestGetFileReadState_不存在(t *testing.T) {
	state, ok := GetFileReadState("/nonexistent/path")
	if ok {
		t.Errorf("不存在的路径应返回 false")
	}
	if state != nil {
		t.Errorf("不存在的路径应返回 nil")
	}
}

// TestSetAndGetFileReadState 测试设置和获取状态
func TestSetAndGetFileReadState(t *testing.T) {
	path := "/test/file.txt"
	expected := &FileReadState{MtimeNS: 123, SizeBytes: 456, IsPartial: true, Content: "hello"}
	SetFileReadState(path, expected)

	state, ok := GetFileReadState(path)
	if !ok {
		t.Errorf("应找到状态")
	}
	if state.MtimeNS != 123 || state.SizeBytes != 456 || state.IsPartial != true || state.Content != "hello" {
		t.Errorf("状态值不匹配，got %+v", state)
	}

	// 清理
	DeleteFileReadState(path)
	_, ok = GetFileReadState(path)
	if ok {
		t.Errorf("删除后不应找到状态")
	}
}

// TestDeleteFileReadState_不存在 测试删除不存在的状态
func TestDeleteFileReadState_不存在(t *testing.T) {
	// 删除不存在的路径不应 panic
	DeleteFileReadState("/nonexistent/path")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestEstimateTokens 测试 token 估算
func TestEstimateTokens(t *testing.T) {
	if estimateTokens("") != 1 {
		t.Errorf("空字符串应至少 1 token")
	}
	if estimateTokens("a") != 1 {
		t.Errorf("1 字符应至少 1 token")
	}
	if estimateTokens("hello world test") != 4 {
		t.Errorf("16 字符应为 4 token，got %d", estimateTokens("hello world test"))
	}
}

// TestIsTextReadForEdit 测试是否为文本可编辑文件
func TestIsTextReadForEdit(t *testing.T) {
	if !isTextReadForEdit("file.go") {
		t.Errorf(".go 应为文本文件")
	}
	if !isTextReadForEdit("file.txt") {
		t.Errorf(".txt 应为文本文件")
	}
	if isTextReadForEdit("file.png") {
		t.Errorf(".png 不应为文本文件")
	}
	if isTextReadForEdit("file.pdf") {
		t.Errorf(".pdf 不应为文本文件")
	}
	if isTextReadForEdit("file.ipynb") {
		t.Errorf(".ipynb 不应为文本文件")
	}
}

// TestExtractNotebookString_字符串数组 测试字符串数组
func TestExtractNotebookString_字符串数组(t *testing.T) {
	cell := map[string]json.RawMessage{
		"source": json.RawMessage(`["line1\n", "line2"]`),
	}
	result := extractNotebookString(cell, "source")
	if result != "line1\nline2" {
		t.Errorf("got %q, want %q", result, "line1\nline2")
	}
}

// TestExtractNotebookString_单个字符串 测试单个字符串
func TestExtractNotebookString_单个字符串(t *testing.T) {
	cell := map[string]json.RawMessage{
		"source": json.RawMessage(`"single line"`),
	}
	result := extractNotebookString(cell, "source")
	if result != "single line" {
		t.Errorf("got %q, want %q", result, "single line")
	}
}

// TestExtractNotebookString_键不存在 测试键不存在
func TestExtractNotebookString_键不存在(t *testing.T) {
	cell := map[string]json.RawMessage{}
	result := extractNotebookString(cell, "source")
	if result != "" {
		t.Errorf("键不存在应返回空字符串，got %q", result)
	}
}

// TestExtractOutputText_text字段 测试 text 字段
func TestExtractOutputText_text字段(t *testing.T) {
	out := map[string]json.RawMessage{
		"text": json.RawMessage(`["output line1\n", "output line2"]`),
	}
	result := extractOutputText(out)
	if result != "output line1\noutput line2" {
		t.Errorf("got %q, want %q", result, "output line1\noutput line2")
	}
}

// TestExtractOutputText_data字段 测试 data 字段中的 text/plain
func TestExtractOutputText_data字段(t *testing.T) {
	out := map[string]json.RawMessage{
		"data": json.RawMessage(`{"text/plain": ["plain text"]}`),
	}
	result := extractOutputText(out)
	if result != "plain text" {
		t.Errorf("got %q, want %q", result, "plain text")
	}
}

// TestExtractOutputText_enameevalue 测试 ename + evalue
func TestExtractOutputText_enameevalue(t *testing.T) {
	out := map[string]json.RawMessage{
		"ename":  json.RawMessage(`"ValueError"`),
		"evalue": json.RawMessage(`"invalid input"`),
	}
	result := extractOutputText(out)
	if result != "ValueError: invalid input" {
		t.Errorf("got %q, want %q", result, "ValueError: invalid input")
	}
}

// TestExtractOutputText_空 测试空输出
func TestExtractOutputText_空(t *testing.T) {
	out := map[string]json.RawMessage{}
	result := extractOutputText(out)
	if result != "" {
		t.Errorf("空输出应返回空字符串，got %q", result)
	}
}

// TestEstimateImageTokens 测试图片 token 估算
func TestEstimateImageTokens(t *testing.T) {
	if estimateImageTokens(nil) != 1 {
		t.Errorf("空数据应至少 1 token")
	}
	if estimateImageTokens([]byte("small")) != 1 {
		t.Errorf("小数据应至少 1 token")
	}
	if estimateImageTokens(make([]byte, 100)) <= 0 {
		t.Errorf("应返回正数 token")
	}
}

// TestDetectImageFormat 测试图片格式检测
func TestDetectImageFormat(t *testing.T) {
	// PNG 魔数
	if detectImageFormat([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}, "") != "png" {
		t.Errorf("PNG 魔数应检测为 png")
	}
	// JPEG 魔数
	if detectImageFormat([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}, "") != "jpeg" {
		t.Errorf("JPEG 魔数应检测为 jpeg")
	}
	// GIF 魔数
	if detectImageFormat([]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00}, "") != "gif" {
		t.Errorf("GIF 魔数应检测为 gif")
	}
	// 回退到扩展名
	if detectImageFormat([]byte{0x00}, ".png") != "png" {
		t.Errorf("回退应使用扩展名")
	}
}

// TestParsePDFPageRange_空 测试空页码范围
func TestParsePDFPageRange_空(t *testing.T) {
	result := parsePDFPageRange("", 10)
	if result == nil || result[0] != 1 || result[1] != 10 {
		t.Errorf("空页码应返回全部页，got %v", result)
	}
}

// TestParsePDFPageRange_单页 测试单页
func TestParsePDFPageRange_单页(t *testing.T) {
	result := parsePDFPageRange("5", 10)
	if result == nil || result[0] != 5 || result[1] != 5 {
		t.Errorf("单页应返回相同起止，got %v", result)
	}
}

// TestParsePDFPageRange_范围 测试范围
func TestParsePDFPageRange_范围(t *testing.T) {
	result := parsePDFPageRange("2-7", 10)
	if result == nil || result[0] != 2 || result[1] != 7 {
		t.Errorf("范围应解析正确，got %v", result)
	}
}

// TestParsePDFPageRange_超出上限 测试超出上限
func TestParsePDFPageRange_超出上限(t *testing.T) {
	result := parsePDFPageRange("15", 10)
	if result == nil || result[0] != 10 || result[1] != 10 {
		t.Errorf("超出上限应截断，got %v", result)
	}
}

// TestParsePDFPageRange_无效页码 测试无效页码
func TestParsePDFPageRange_无效页码(t *testing.T) {
	result := parsePDFPageRange("0", 10)
	if result != nil {
		t.Errorf("页码 0 应返回 nil，got %v", result)
	}
	result = parsePDFPageRange("abc", 10)
	if result != nil {
		t.Errorf("非数字应返回 nil，got %v", result)
	}
}

// TestParsePDFPageRange_起始大于结束 测试起始大于结束
func TestParsePDFPageRange_起始大于结束(t *testing.T) {
	result := parsePDFPageRange("8-3", 10)
	if result != nil {
		t.Errorf("起始大于结束应返回 nil，got %v", result)
	}
}

// TestParsePDFPageRange_无总页数 测试无总页数
func TestParsePDFPageRange_无总页数(t *testing.T) {
	result := parsePDFPageRange("", 0)
	if result != nil {
		t.Errorf("空页码无总页数应返回 nil，got %v", result)
	}
}
