package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// TestDecodeResponseText_编码候选链 验证 declared→utf-8→apparent→gb18030 候选链。
func TestDecodeResponseText_编码候选链(t *testing.T) {
	// GBK 编码字节数组（"中文测试" GBK: D6 D0 CE C4 B2 E2 CA D4）
	gbkBytes := []byte{0xD6, 0xD0, 0xCE, 0xC4, 0xB2, 0xE2, 0xCA, 0xD4}
	// declared=gbk → 候选链首个即命中
	if got := decodeResponseText(gbkBytes, "gbk", "", ""); got != "中文测试" {
		t.Errorf("gbk 解码失败: %q", got)
	}
	// 无 declared → utf-8 失败 → gb18030 兜底
	if got := decodeResponseText(gbkBytes, "", "", ""); got != "中文测试" {
		t.Errorf("gb18030 兜底失败: %q", got)
	}
	// 拉丁 declared 被跳过（iso-8859-1 不进首候选）→ utf-8 优先
	utf8Bytes := []byte("hello")
	if got := decodeResponseText(utf8Bytes, "iso-8859-1", "", ""); got != "hello" {
		t.Errorf("拉丁 declared 跳过失败: %q", got)
	}
	// 空输入
	if got := decodeResponseText(nil, "", "", ""); got != "" {
		t.Errorf("空输入异常: %q", got)
	}
}

// TestExtractDeclaredCharset_三级检测 验证 header 正则 + meta 正则。
// Python 语义：原样返回（strip 引号），小写化在 decodeResponseText 候选链做。
func TestExtractDeclaredCharset_三级检测(t *testing.T) {
	if got := extractDeclaredCharset("text/html; charset=GBK", nil); got != "GBK" {
		t.Errorf("header 检测: %s", got)
	}
	if got := extractDeclaredCharset("text/html; charset=\"utf-8\"", nil); got != "utf-8" {
		t.Errorf("header 引号剥离: %s", got)
	}
	meta := []byte(`<html><head><meta http-equiv="Content-Type" content="text/html; charset=gb2312">`)
	if got := extractDeclaredCharset("", meta); got != "gb2312" {
		t.Errorf("meta 检测: %s", got)
	}
	metaLower := []byte(`<META CHARSET="Big5">`)
	if got := extractDeclaredCharset("", metaLower); got != "Big5" {
		t.Errorf("meta 简写检测: %s", got)
	}
	if got := extractDeclaredCharset("", nil); got != "" {
		t.Errorf("无 declared: %s", got)
	}
}

// TestNormalizeURL_前缀补齐与DDG解码 验证 URL 归一化。
func TestNormalizeURL_前缀补齐与DDG解码(t *testing.T) {
	if got := normalizeURL("example.com/x"); got != "https://example.com/x" {
		t.Errorf("无 scheme 补齐失败: %s", got)
	}
	if got := normalizeURL("https://duckduckgo.com/l/?uddg=https%3A%2F%2Fa.com"); got != "https://a.com" {
		t.Errorf("DDG 解码失败: %s", got)
	}
	if got := normalizeURL("http://b.com"); got != "http://b.com" {
		t.Errorf("http 保留失败: %s", got)
	}
	if got := normalizeURL("   "); got != "" {
		t.Errorf("空串: %q", got)
	}
}

// TestMcpFetchWebpage_空URL 验证空 URL 前置检查。
func TestMcpFetchWebpage_空URL(t *testing.T) {
	result, err := McpFetchWebpage.Invoke(t.Context(), map[string]any{
		"url": "  ",
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	if result["result"] != "[ERROR]: url cannot be empty." {
		t.Errorf("空 URL 文案异常: %s", result["result"])
	}
}

// TestMcpFetchWebpage_403触发Jina兜底 验证 401/403/429 → Jina Reader。
func TestMcpFetchWebpage_403触发Jina兜底(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer target.Close()
	jinaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Title: Jina\nContent Body"))
	}))
	defer jinaSrv.Close()
	// 注入 jina reader URL 前缀
	origPrefix := jinaReaderURLPrefix
	jinaReaderURLPrefix = jinaSrv.URL + "/"
	t.Cleanup(func() { jinaReaderURLPrefix = origPrefix })
	result, err := McpFetchWebpage.Invoke(t.Context(), map[string]any{
		"url": target.URL, "max_chars": 0, "timeout_seconds": 5,
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	s := result["result"].(string)
	if !strings.Contains(s, "Status: 200") || !strings.Contains(s, "Jina") {
		t.Errorf("Jina 兜底未触发: %s", s)
	}
}

// TestMcpFetchWebpage_HTML抽取 验证 title 提取 + script/style 剥离。
func TestMcpFetchWebpage_HTML抽取(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>页面标题</title><script>evil()</script><style>.x{}</style></head><body>正文内容</body></html>`))
	}))
	defer srv.Close()
	result, err := McpFetchWebpage.Invoke(t.Context(), map[string]any{
		"url": srv.URL, "max_chars": 0, "timeout_seconds": 5,
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	s := result["result"].(string)
	if !strings.Contains(s, "Title: 页面标题") {
		t.Errorf("title 提取失败: %s", s)
	}
	if !strings.Contains(s, "正文内容") {
		t.Errorf("正文缺失: %s", s)
	}
	if strings.Contains(s, "evil()") || strings.Contains(s, ".x{}") {
		t.Errorf("script/style 未剥离: %s", s)
	}
}

// TestMcpFetchWebpage_非HTML空白折叠 验证非 HTML 内容走空白折叠分支。
func TestMcpFetchWebpage_非HTML空白折叠(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("line1\n  line2\n\nline3"))
	}))
	defer srv.Close()
	result, err := McpFetchWebpage.Invoke(t.Context(), map[string]any{
		"url": srv.URL, "max_chars": 0, "timeout_seconds": 5,
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	s := result["result"].(string)
	if !strings.Contains(s, "line1 line2 line3") {
		t.Errorf("空白折叠异常: %s", s)
	}
}

// TestMcpFetchWebpage_空内容占位 验证空内容输出 [empty]。
func TestMcpFetchWebpage_空内容占位(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("   "))
	}))
	defer srv.Close()
	result, err := McpFetchWebpage.Invoke(t.Context(), map[string]any{
		"url": srv.URL, "max_chars": 0, "timeout_seconds": 5,
	}, tool.WithSkipInputsValidate(true))
	if err != nil {
		t.Fatal(err)
	}
	s := result["result"].(string)
	if !strings.Contains(s, "[empty]") {
		t.Errorf("空内容占位异常: %s", s)
	}
}
