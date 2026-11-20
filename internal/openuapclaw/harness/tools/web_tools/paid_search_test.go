package web_tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestJinaSearch Jina 搜索（mock server）
func TestJinaSearch(t *testing.T) {
	response := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": "The answer is https://example.com/result",
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("应设置 Authorization 头")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// 测试需要覆盖 Jina URL，此处验证 API Key 检测
	os.Unsetenv("JINA_API_KEY")
	_, err := jinaSearch("test", 30)
	if err == nil {
		t.Error("无 API Key 应返回错误")
	}
}

// TestBochaSearch Bocha 搜索（mock server）
func TestBochaSearch(t *testing.T) {
	response := map[string]any{
		"summary": "This is the answer",
		"data": map[string]any{
			"webPages": map[string]any{
				"value": []any{
					map[string]any{"url": "https://example.com/1"},
					map[string]any{"url": "https://example.com/2"},
				},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// 测试 API Key 检测
	os.Unsetenv("BOCHA_API_KEY")
	_, err := bochaSearch("test", 5, 30)
	if err == nil {
		t.Error("无 API Key 应返回错误")
	}
}

// TestSerperSearch Serper 搜索（mock server）
func TestSerperSearch(t *testing.T) {
	response := map[string]any{
		"organic": []any{
			map[string]any{"link": "https://example.com/1"},
			map[string]any{"link": "https://example.com/2"},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	os.Unsetenv("SERPER_API_KEY")
	_, err := serperSearch("test", 5, 30)
	if err == nil {
		t.Error("无 API Key 应返回错误")
	}
}

// TestPerplexitySearch Perplexity 搜索（mock server）
func TestPerplexitySearch(t *testing.T) {
	os.Unsetenv("PERPLEXITY_API_KEY")
	_, err := perplexitySearch("test", 5, 30)
	if err == nil {
		t.Error("无 API Key 应返回错误")
	}
}

// TestExtractBochaURLs Bocha URL 提取
func TestExtractBochaURLs(t *testing.T) {
	data := map[string]any{
		"data": map[string]any{
			"webPages": map[string]any{
				"value": []any{
					map[string]any{"url": "https://example.com/1"},
					map[string]any{"link": "https://example.com/2"},
				},
			},
		},
	}
	urls := extractBochaURLs(data, 5)
	if len(urls) != 2 {
		t.Fatalf("应提取 2 个 URL, got %d", len(urls))
	}
	if urls[0] != "https://example.com/1" {
		t.Errorf("urls[0] = %q, want %q", urls[0], "https://example.com/1")
	}
}

// TestExtractBochaAnswer Bocha 答案提取
func TestExtractBochaAnswer(t *testing.T) {
	// 直接字段
	data := map[string]any{
		"summary": "This is the answer",
	}
	answer := extractBochaAnswer(data)
	if answer != "This is the answer" {
		t.Errorf("answer = %q, want %q", answer, "This is the answer")
	}

	// 嵌套字段
	data = map[string]any{
		"data": map[string]any{
			"answer": "Nested answer",
		},
	}
	answer = extractBochaAnswer(data)
	if answer != "Nested answer" {
		t.Errorf("answer = %q, want %q", answer, "Nested answer")
	}
}

// TestParsePerplexityCitations Perplexity 引用提取
func TestParsePerplexityCitations(t *testing.T) {
	data := map[string]any{
		"citations": []any{
			"https://example.com/1",
			"https://example.com/2",
		},
	}
	urls := parsePerplexityCitations(data)
	if len(urls) != 2 {
		t.Fatalf("应提取 2 个 URL, got %d", len(urls))
	}

	// 对象形式
	data = map[string]any{
		"sources": []any{
			map[string]any{"url": "https://example.com/1"},
			map[string]any{"source_url": "https://example.com/2"},
		},
	}
	urls = parsePerplexityCitations(data)
	if len(urls) != 2 {
		t.Fatalf("应提取 2 个 URL, got %d", len(urls))
	}
}

// TestNewWebPaidSearchTool 工具创建
func TestNewWebPaidSearchTool(t *testing.T) {
	tool := NewWebPaidSearchTool("cn", "test-agent")
	if tool == nil {
		t.Fatal("工具创建失败")
	}
	card := tool.Card()
	if card.Name != "paid_search" {
		t.Errorf("card.Name = %q, want %q", card.Name, "paid_search")
	}
}
