package web_tools

import (
	"os"
	"testing"
)

// TestClipDebugText 调试文本截断
func TestClipDebugText(t *testing.T) {
	// 短文本不截断
	short := "hello"
	if got := clipDebugText(short, 100); got != short {
		t.Errorf("短文本不应截断, got %q", got)
	}

	// 长文本截断保留头尾
	longText := ""
	for i := 0; i < 100; i++ {
		longText += "abcdefghij"
	}
	clipped := clipDebugText(longText, 100)
	if len(clipped) <= 100 {
		t.Error("截断后应包含标记信息，长度可能略超 maxChars")
	}
}

// TestWriteDebugPayload 调试文件写入
func TestWriteDebugPayload(t *testing.T) {
	// 调试模式关闭时不应写入
	os.Unsetenv(freeSearchDebugEnv)
	writeDebugPayload("test", "bing", "raw", map[string]any{"query": "test"})
	// 不应报错
}

// TestSummarizeRows 搜索结果摘要
func TestSummarizeRows(t *testing.T) {
	rows := []searchRow{
		{Title: "Result 1", URL: "https://example.com/1", Source: "duckduckgo"},
		{Title: "Result 2", URL: "https://example.com/2", Source: "bing"},
	}
	summary := summarizeRows(rows)
	if summary["count"] != 2 {
		t.Errorf("count = %v, want 2", summary["count"])
	}
	bySource, ok := summary["by_source"].(map[string]int)
	if !ok {
		t.Fatal("by_source 类型错误")
	}
	if bySource["duckduckgo"] != 1 {
		t.Errorf("duckduckgo count = %d, want 1", bySource["duckduckgo"])
	}
}

// TestNewDebugRunID 生成调试运行 ID
func TestNewDebugRunID(t *testing.T) {
	id := newDebugRunID()
	if len(id) == 0 {
		t.Error("运行 ID 不应为空")
	}
}

// TestExtractDebugHTMLNeighborhood HTML 标记附近内容提取
func TestExtractDebugHTMLNeighborhood(t *testing.T) {
	html := `<html><body><div id="b_results">content here</div></body></html>`
	result := extractDebugHTMLNeighborhood(html, "b_results")
	if result["position"].(int) < 0 {
		t.Error("应找到标记位置")
	}

	// 不存在的标记
	result = extractDebugHTMLNeighborhood(html, "nonexistent")
	if result["position"].(int) >= 0 {
		t.Error("不存在的标记应返回 -1")
	}
}
