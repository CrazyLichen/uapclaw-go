package web_tools

import (
	"testing"
)

// TestDecodeDDGRedirect DDG 重定向解码
func TestDecodeDDGRedirect(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"非DDG重定向", "https://example.com/page", "https://example.com/page"},
		{"DDG重定向", "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage", "https://example.com/page"},
		{"DDG无uddg参数", "https://duckduckgo.com/l/?q=test", "https://duckduckgo.com/l/?q=test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeDDGRedirect(tt.input)
			if got != tt.want {
				t.Errorf("decodeDDGRedirect() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDecodeBingRedirect Bing 重定向解码
func TestDecodeBingRedirect(t *testing.T) {
	// 非Bing URL
	if got := decodeBingRedirect("https://example.com/page"); got != "https://example.com/page" {
		t.Errorf("非Bing URL 应原样返回, got %q", got)
	}

	// 直接http URL
	if got := decodeBingRedirect("https://www.bing.com/ck/a?u=https%3A%2F%2Fexample.com"); got != "https://example.com" {
		t.Errorf("直接 http URL 应解码, got %q", got)
	}
}

// TestContainsCJK CJK 检测
func TestContainsCJK(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"hello", false},
		{"你好", true},
		{"hello世界", true},
		{"", false},
		{"123", false},
	}
	for _, tt := range tests {
		if got := containsCJK(tt.input); got != tt.want {
			t.Errorf("containsCJK(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestQueryTerms 查询词提取
func TestQueryTerms(t *testing.T) {
	// 英文查询 — 注意 "latest", "today", "news" 是停用词
	terms := queryTerms("climate change research")
	if len(terms) == 0 {
		t.Error("英文查询应返回结果")
	}
	found := false
	for _, t := range terms {
		if t == "climate" {
			found = true
		}
	}
	if !found {
		t.Errorf("应包含 'climate', got %v", terms)
	}

	// CJK 查询
	terms = queryTerms("今日新闻")
	if len(terms) == 0 {
		t.Error("CJK 查询应返回结果")
	}
}

// TestScoreRow 搜索结果评分
func TestScoreRow(t *testing.T) {
	row := searchRow{
		Title:   "Example Title",
		URL:     "https://example.com",
		Snippet: "This is a test snippet",
		Source:  "duckduckgo",
	}
	score := scoreRow("test", row)
	if score <= 0 {
		t.Errorf("有标题和摘要的行应得正分, got %f", score)
	}
}

// TestNormalizeURL URL 规范化
func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"已有协议", "https://example.com", "https://example.com"},
		{"无协议", "example.com", "https://example.com"},
		{"空值", "", ""},
		{"DDG重定向", "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com", "https://example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIsTimelyQuery 时效性查询判断
func TestIsTimelyQuery(t *testing.T) {
	if !isTimelyQuery("latest news today") {
		t.Error("包含 'latest' 应为时效性查询")
	}
	if !isTimelyQuery("今日新闻") {
		t.Error("包含 '今日' 应为时效性查询")
	}
	if isTimelyQuery("how to cook pasta") {
		t.Error("不包含时效性词不应为时效性查询")
	}
}

// TestIsLowConfidenceResultDomain 低置信度域名判断
func TestIsLowConfidenceResultDomain(t *testing.T) {
	if !isLowConfidenceResultDomain("https://zhihu.com/question/123") {
		t.Error("zhihu.com 应为低置信度")
	}
	if isLowConfidenceResultDomain("https://example.com/page") {
		t.Error("example.com 不应为低置信度")
	}
}

// TestBuildBingRow 构建Bing结果行
func TestBuildBingRow(t *testing.T) {
	row := buildBingRow("https://example.com", "Title", "Snippet", "Source", "2024-01-01", "")
	if row.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", row.URL, "https://example.com")
	}
	if row.Source != "bing-web" {
		t.Errorf("默认 Source = %q, want %q", row.Source, "bing-web")
	}
}

// TestClipText 文本截断
func TestClipText(t *testing.T) {
	// 短文本不截断
	if got := clipText("hello", 100); got != "hello" {
		t.Errorf("短文本不应截断, got %q", got)
	}
	// 长文本截断
	longText := "abcdefghijklmnopqrstuvwxyz"
	if got := clipText(longText, 10); got != "abcdefghij\n...[truncated]" {
		t.Errorf("长文本应截断, got %q", got)
	}
}

// TestMatchTermCount 查询词匹配计数
func TestMatchTermCount(t *testing.T) {
	row := searchRow{
		Title:   "Golang Programming Language",
		URL:     "https://golang.org",
		Snippet: "Go is a programming language",
		Source:  "test",
	}
	count := matchTermCount("golang programming", row)
	if count < 1 {
		t.Errorf("应至少匹配 1 个词, got %d", count)
	}
}

// TestEngineDisplayName 引擎显示名称
func TestEngineDisplayName(t *testing.T) {
	if got := engineDisplayName("duckduckgo"); got != "DuckDuckGo" {
		t.Errorf("got %q, want %q", got, "DuckDuckGo")
	}
	if got := engineDisplayName("bing"); got != "Bing" {
		t.Errorf("got %q, want %q", got, "Bing")
	}
	if got := engineDisplayName("unknown"); got != "unknown" {
		t.Errorf("未知引擎应原样返回, got %q", got)
	}
}

// TestGetNestedValue 嵌套 map 取值
func TestGetNestedValue(t *testing.T) {
	data := map[string]any{
		"data": map[string]any{
			"webPages": map[string]any{
				"value": []any{"a", "b"},
			},
		},
	}
	if got := getNestedValue(data, "data.webPages.value"); got == nil {
		t.Error("应返回值")
	}
	if got := getNestedValue(data, "data.missing"); got != nil {
		t.Error("不存在的路径应返回 nil")
	}
}
