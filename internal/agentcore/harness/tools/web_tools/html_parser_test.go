package web_tools

import (
	"strings"
	"testing"
)

// TestStripTags HTML 标签清理
func TestStripTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"简单标签", "<b>hello</b>", "hello"},
		{"嵌套标签", "<div><p>hello world</p></div>", "hello world"},
		{"带属性", `<a href="http://example.com">link</a>`, "link"},
		{"HTML实体", "a &amp; b", "a & b"},
		{"空白压缩", "  hello   world  ", "hello world"},
		{"无标签", "plain text", "plain text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripTags(tt.input)
			if got != tt.want {
				t.Errorf("stripTags() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestParseHTML goquery 解析
func TestParseHTML(t *testing.T) {
	html := `<html><body><h1>Title</h1><p>Content</p></body></html>`
	doc := parseHTML(html)
	if doc.Length() == 0 {
		t.Error("解析结果不应为空")
	}
	title := doc.Find("h1").Text()
	if title != "Title" {
		t.Errorf("h1 text = %q, want %q", title, "Title")
	}
}

// TestExtractMainTextFromHTML 正文提取
func TestExtractMainTextFromHTML(t *testing.T) {
	html := `<html><head><title>Test Page</title></head>
<body><article><p>This is the main content of the article.</p></article></body></html>`
	title, content := extractMainTextFromHTML(html)
	if title != "Test Page" {
		t.Errorf("title = %q, want %q", title, "Test Page")
	}
	if !strings.Contains(content, "main content") {
		t.Errorf("content 应包含 'main content', got %q", content)
	}
}

// TestExtractMainTextFromHTML_脚本样式移除
func TestExtractMainTextFromHTML_脚本样式移除(t *testing.T) {
	html := `<html><head><title>Test</title></head>
<body><article>
<script>var x = 1;</script>
<style>.cls { color: red; }</style>
<p>Visible content</p>
</article></body></html>`
	_, content := extractMainTextFromHTML(html)
	if strings.Contains(content, "var x") {
		t.Error("content 不应包含 script 内容")
	}
	if strings.Contains(content, "color: red") {
		t.Error("content 不应包含 style 内容")
	}
}
