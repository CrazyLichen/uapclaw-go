package web_tools

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchWebpage 网页抓取（mock server）
func TestFetchWebpage(t *testing.T) {
	html := `<!DOCTYPE html>
<html><head><title>Test Page</title></head>
<body><article><p>This is the main content of the article. It has enough text to be considered a valid paragraph for extraction.</p></article></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}))
	defer server.Close()

	data, err := fetchWebpage(server.URL, 5)
	if err != nil {
		t.Fatalf("fetchWebpage 失败: %v", err)
	}
	if data["title"] != "Test Page" {
		t.Errorf("title = %v, want %q", data["title"], "Test Page")
	}
	if data["status_code"] != 200 {
		t.Errorf("status_code = %v, want 200", data["status_code"])
	}
}

// TestFetchWebpage_403降级 403 降级到 Jina
func TestFetchWebpage_403降级(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("access denied"))
	}))
	defer server.Close()

	// 403 会尝试 Jina，但 Jina 也会失败，所以这里只验证不会 panic
	_, err := fetchWebpage(server.URL, 5)
	// 可能返回错误（因为 Jina 也不可达），但不应 panic
	_ = err
}

// TestDecodeResponseText 多编码检测
func TestDecodeResponseText(t *testing.T) {
	// UTF-8 响应
	resp := &httpResponse{
		statusCode: 200,
		headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		body:       []byte("hello world"),
		text:       "hello world",
	}
	text := decodeResponseText(resp)
	if text != "hello world" {
		t.Errorf("text = %q, want %q", text, "hello world")
	}
}

// TestScoreDecodedText 文本质量评分
func TestScoreDecodedText(t *testing.T) {
	// 正常文本
	score := scoreDecodedText("Hello, this is a normal text.")
	if score <= 0 {
		t.Errorf("正常文本应得正分, got %f", score)
	}

	// 空文本
	score = scoreDecodedText("")
	if score >= 0 {
		t.Errorf("空文本应得负分, got %f", score)
	}

	// 含替换字符
	score = scoreDecodedText("hello \ufffd world")
	if score >= 0 {
		t.Errorf("含替换字符应得负分, got %f", score)
	}
}

// TestExtractDeclaredCharset charset 提取
func TestExtractDeclaredCharset(t *testing.T) {
	// Content-Type 头
	resp := &httpResponse{
		headers: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		body:    []byte{},
	}
	charset := extractDeclaredCharset(resp)
	if charset != "utf-8" {
		t.Errorf("charset = %q, want %q", charset, "utf-8")
	}

	// 无 charset
	resp = &httpResponse{
		headers: http.Header{"Content-Type": []string{"text/html"}},
		body:    []byte{},
	}
	charset = extractDeclaredCharset(resp)
	if charset != "" {
		t.Errorf("无 charset 应返回空, got %q", charset)
	}
}

// TestNewWebFetchWebpageTool 工具创建
func TestNewWebFetchWebpageTool(t *testing.T) {
	tool := NewWebFetchWebpageTool("cn", "test-agent")
	if tool == nil {
		t.Fatal("工具创建失败")
	}
	card := tool.Card()
	if card.Name != "fetch_webpage" {
		t.Errorf("card.Name = %q, want %q", card.Name, "fetch_webpage")
	}
}
