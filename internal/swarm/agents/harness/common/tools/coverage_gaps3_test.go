//go:build !windows

package tools

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 平台桩覆盖（非 Windows） ────────────────────────────

// TestWindowsShellStubs_非Windows平台桩 验证平台桩的零值语义。
// 这些函数仅在 resolveExecutionPlan 的 Windows 分支被引用；
// 非 Windows 编译期为桩，本用例固化桩行为防意外改动。
func TestWindowsShellStubs_非Windows平台桩(t *testing.T) {
	if got := availablePowershell(); got != "powershell" {
		t.Errorf("availablePowershell 桩异常: %q", got)
	}
	if isWSLBashPath(`C:\Windows\system32\bash.exe`) {
		t.Error("isWSLBashPath 桩应恒 false")
	}
	if got := gitBashCandidates(); got != nil {
		t.Errorf("gitBashCandidates 桩应恒 nil: %v", got)
	}
	if exe, ok := availableGitBash(); ok || exe != "" {
		t.Errorf("availableGitBash 桩应恒 false: %q %v", exe, ok)
	}
	if exe, ok := availableBash(true); ok || exe != "" {
		t.Errorf("availableBash 桩应恒 false: %q %v", exe, ok)
	}
	if exe, ok := availableSh(); ok || exe != "" {
		t.Errorf("availableSh 桩应恒 false: %q %v", exe, ok)
	}
	if looksLikePosix("ls -la") {
		t.Error("looksLikePosix 桩应恒 false")
	}
}

// ──────────────────────────── 零散分支补充 ────────────────────────────

// TestGetRequestsVerify_取值表 验证 SSL verify 解析。
// Python: ssl_config.py get_requests_verify
func TestGetRequestsVerify_取值表(t *testing.T) {
	falsy := []string{"0", "false", "no", "off"}
	for _, v := range falsy {
		t.Setenv("JIUWENSWARM_SSL_VERIFY", v)
		if getRequestsVerify() {
			t.Errorf("%q 应为 false", v)
		}
	}
	truthy := []string{"1", "true", "yes", "on", "anything-else"}
	for _, v := range truthy {
		t.Setenv("JIUWENSWARM_SSL_VERIFY", v)
		if !getRequestsVerify() {
			t.Errorf("%q 应为 true", v)
		}
	}
	t.Setenv("JIUWENSWARM_SSL_VERIFY", "")
	if !getRequestsVerify() {
		t.Error("空值默认 true")
	}
}

// TestNewFreeSearchHTTPClient_组合 验证 verify/proxy 组合构造。
func TestNewFreeSearchHTTPClient_组合(t *testing.T) {
	t.Setenv("FREE_SEARCH_PROXY_URL", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	// 无代理：Transport 非 nil（ProxyFromEnvironment 默认路径）
	c := newFreeSearchHTTPClient(true, "")
	if c == nil || c.Transport == nil {
		t.Fatal("client/Transport 不应为 nil")
	}
	// 显式代理：Transport 非 nil
	c2 := newFreeSearchHTTPClient(false, "http://proxy:8080")
	if c2 == nil || c2.Transport == nil {
		t.Error("带代理时 Transport 不应为 nil")
	}
}

// TestDecodeBingRedirect_多分支 验证 Bing 重定向解码分支。
// Python: _decode_bing_redirect
func TestDecodeBingRedirect_多分支(t *testing.T) {
	// a1 前缀 base64
	enc := base64URLEncode("https://target.example.com/x")
	if got := decodeBingRedirect("https://www.bing.com/ck/a?u=a1" + enc); got != "https://target.example.com/x" {
		t.Errorf("a1 解码异常: %q", got)
	}
	// u 为明文 http URL → 直传
	if got := decodeBingRedirect("https://www.bing.com/ck/a?u=https://plain.example.com/y"); got != "https://plain.example.com/y" {
		t.Errorf("明文 u 直传异常: %q", got)
	}
	// 非 bing 域直传
	if got := decodeBingRedirect("https://other.example.com/page"); got != "https://other.example.com/page" {
		t.Errorf("直传异常: %q", got)
	}
	// bing 域但无 u 参数 → 直传
	if got := decodeBingRedirect("https://www.bing.com/ck/a?other=1"); got != "https://www.bing.com/ck/a?other=1" {
		t.Errorf("无 u 直传异常: %q", got)
	}
	// 非法 URL → 直传
	if got := decodeBingRedirect("://bad"); got != "://bad" {
		t.Errorf("非法 URL 直传异常: %q", got)
	}
	// a1 前缀但解码后非 http → 直传
	encNonHTTP := base64URLEncode("not-a-url")
	if got := decodeBingRedirect("https://www.bing.com/ck/a?u=a1" + encNonHTTP); !strings.Contains(got, "bing.com") {
		t.Errorf("非 http 解码应直传: %q", got)
	}
}

// TestNewMcpFreeSearchTool_引擎禁用与钳制 验证 free search 工具分支。
// Python: mcp_free_search (search_tools.py)
func TestNewMcpFreeSearchTool_引擎禁用与钳制(t *testing.T) {
	// 全引擎禁用 → 每个引擎都报错（可用空 query 触发参数分支）
	t.Setenv("FREE_SEARCH_DDG_ENABLED", "0")
	t.Setenv("FREE_SEARCH_BING_ENABLED", "0")
	res, err := McpFreeSearch.Invoke(t.Context(), map[string]any{"query": "anything"})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	if !strings.Contains(result, "all free search engines disabled") && !strings.Contains(result, "disabled") {
		t.Errorf("禁用文案异常: %s", result)
	}
}

// TestDecodeWithEncoding_多编码 验证编码解码分支。
// Python: _decode_with_encoding
func TestDecodeWithEncoding_多编码(t *testing.T) {
	// UTF-8 直通
	if got, err := decodeWithEncoding([]byte("plain utf8"), "utf-8"); err != nil || got != "plain utf8" {
		t.Errorf("utf-8 异常: %q %v", got, err)
	}
	// GBK 中文
	gbkBytes := []byte{0xD6, 0xD0, 0xCE, 0xC4} // "中文"
	if got, err := decodeWithEncoding(gbkBytes, "gbk"); err != nil || got != "中文" {
		t.Errorf("gbk 异常: %q %v", got, err)
	}
	// Big5 中文
	big5Bytes := []byte{0xA4, 0xA4, 0xA4, 0xE5} // "中文" Big5
	if got, err := decodeWithEncoding(big5Bytes, "big5"); err != nil || got != "中文" {
		t.Errorf("big5 异常: %q %v", got, err)
	}
	// 未知编码名 → 错误
	if _, err := decodeWithEncoding([]byte("x"), "bogus-encoding"); err == nil {
		t.Error("未知编码应报错")
	}
	// 严格模式：UTF-8 声明但内容非法 → 错误
	if _, err := decodeWithEncoding([]byte{0xFF, 0xFE, 0xFD}, "utf-8"); err == nil {
		t.Error("非法 UTF-8 严格模式应报错")
	}
}

// TestUtf8SequenceLen_分支 验证 UTF-8 序列长度。
// 注意：实现仅判多字节前缀（2/3/4 字节），ASCII 单字节返回 0
// （调用方 utf8Valid 对 0 走默认单字节路径）。
func TestUtf8SequenceLen(t *testing.T) {
	cases := []struct {
		b    byte
		want int
	}{
		{0x00, 0}, {0x7F, 0}, // ASCII 单字节 → 0（非多字节前缀）
		{0xC0, 2}, {0xDF, 2},
		{0xE0, 3}, {0xEF, 3},
		{0xF0, 4}, {0xF7, 4},
		{0x80, 0}, {0xFF, 0}, // 续字节/非法 → 0
	}
	for _, c := range cases {
		if got := utf8SequenceLen(c.b); got != c.want {
			t.Errorf("utf8SequenceLen(0x%02X) = %d, want %d", c.b, got, c.want)
		}
	}
}

// TestClipTextFetch_边界 验证 fetch 版裁剪。
func TestClipTextFetch_边界(t *testing.T) {
	if got := clipTextFetch("hello", 10); got != "hello" {
		t.Errorf("未超限异常: %q", got)
	}
	if got := clipTextFetch("hello", 3); !strings.HasPrefix(got, "hel") || !strings.Contains(got, "truncated") {
		t.Errorf("超限裁剪异常: %q", got)
	}
	if got := clipTextFetch("hello", 0); got != "hello" {
		t.Errorf("0 不裁剪异常: %q", got)
	}
}

// TestAbsPathOrRaw_分支 验证 abs 转换。
func TestAbsPathOrRaw_分支(t *testing.T) {
	// 相对路径 → abs
	if got := absPathOrRaw("rel/x.txt"); !filepath.IsAbs(got) {
		t.Errorf("相对路径应转 abs: %s", got)
	}
	// 空串 → abs(cwd)
	if got := absPathOrRaw(""); !filepath.IsAbs(got) {
		t.Errorf("空串应转 abs: %s", got)
	}
}

// TestSourceManifest_get未知sha 验证 get 的 nil 分支。
func TestSourceManifest_get未知sha(t *testing.T) {
	m := newSourceManifest(t.TempDir(), nil)
	m.ensureInitialized(t.Context())
	if got := m.get("nope"); got != nil {
		t.Errorf("未知 sha 应返回 nil: %v", got)
	}
}

// TestSourceManifest_load合法JSON类型不符 验证类型吞错分支。
func TestSourceManifest_load合法JSON类型不符(t *testing.T) {
	dir := t.TempDir()
	// entry 非 map 类型 → Unmarshal 失败 → 空
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"sha": "string-not-map"}`), 0o644)
	m := newSourceManifest(dir, nil)
	m.ensureInitialized(t.Context())
	if len(m.allEntries()) != 0 {
		t.Error("类型不符应吞错为空")
	}
}

// TestSourceManifest_save的非法路径失败 验证 save 错误传播。
// root 用户绕过目录权限，故用非法路径字符触发写失败。
func TestSourceManifest_save的非法路径失败(t *testing.T) {
	// manifest 路径含 NUL 字节 → os.WriteFile 报错
	m := newSourceManifest("invalid\x00path", nil)
	m.ensureInitialized(t.Context())
	if err := m.record(t.Context(), "s", "n", "d"); err == nil {
		t.Error("非法路径应报错")
	}
}

// TestBochaSearch_错误分支 验证 bocha 非 2xx 报错。
// Python: _bocha_search
func TestBochaSearch_错误分支(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	orig := bochaAPIURL
	bochaAPIURL = srv.URL
	t.Cleanup(func() { bochaAPIURL = orig })

	t.Setenv("BOCHA_API_KEY", "bad-key")
	if _, err := bochaSearch("q", 5, 10); err == nil {
		t.Error("401 应报错")
	}
}

// TestSerperSearch_错误分支 验证 serper 非 2xx 报错。
func TestSerperSearch_错误分支(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	orig := serperAPIURL
	serperAPIURL = srv.URL
	t.Cleanup(func() { serperAPIURL = orig })

	t.Setenv("SERPER_API_KEY", "bad-key")
	if _, err := serperSearch("q", 5, 10); err == nil {
		t.Error("500 应报错")
	}
}

// TestInvokeWikiQuery_成功路径走模型 验证 query 走到模型调用。
// Python: wiki_query (wiki_tools.py L466-484)
func TestInvokeWikiQuery_成功路径走模型(t *testing.T) {
	t.Setenv("MODEL_PROVIDER", "OpenAI")
	t.Setenv("MODEL_NAME", "wiki-q-test")
	t.Setenv("API_KEY", "k")
	t.Setenv("API_BASE", "https://q.example.com/v1")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// 预初始化 workspace
	wsBase := filepath.Join(home, "query_ws")
	_ = os.MkdirAll(filepath.Join(wsBase, ".llm_wiki"), 0o755)

	res, err := WikiQuery.Invoke(t.Context(), map[string]any{
		"query":     "什么是知识库",
		"workspace": wsBase,
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	// 走到模型（HTTP 失败 → "Wiki Query Error" 文案）
	if result == "" {
		t.Error("应有非空文案")
	}
}

// TestDefaultImageGenModelBuilder_DashScope默认 验证 DashScope 默认凭据。
func TestDefaultImageGenModelBuilder_DashScope默认(t *testing.T) {
	for _, k := range []string{"IMAGE_GEN_API_KEY", "IMAGE_GEN_API_BASE", "IMAGE_GEN_MODEL_NAME", "IMAGE_GEN_PROVIDER", "API_KEY", "API_BASE"} {
		t.Setenv(k, "")
	}
	t.Setenv("IMAGE_GEN_API_KEY", "ds-key")

	m, err := defaultImageGenModelBuilder()
	if err != nil {
		t.Fatalf("builder 失败: %v", err)
	}
	if m.ClientConfig.APIBase != "https://dashscope.aliyuncs.com/api/v1" {
		t.Errorf("默认 base 异常: %s", m.ClientConfig.APIBase)
	}
	if m.ModelConfig.ModelName != "wanx-v1" {
		t.Errorf("默认 model 异常: %s", m.ModelConfig.ModelName)
	}
	if m.ClientConfig.ClientProvider != "DashScope" {
		t.Errorf("默认 provider 异常: %s", m.ClientConfig.ClientProvider)
	}
}

// base64URLEncode 辅助：URL-safe base64（对齐实现的 URLEncoding）。
func base64URLEncode(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

// TestMcpPaidSearch_jinaProvider 验证 jina provider 路由。
// Python: mcp_paid_search provider 分支
func TestMcpPaidSearch_jinaProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "jina answer"}}]}`))
	}))
	defer srv.Close()
	orig := jinaDeepSearchAPIURLVar
	jinaDeepSearchAPIURLVar = srv.URL
	t.Cleanup(func() { jinaDeepSearchAPIURLVar = orig })

	t.Setenv("JINA_API_KEY", "jk")
	t.Setenv("BOCHA_API_KEY", "")
	t.Setenv("PERPLEXITY_API_KEY", "")
	t.Setenv("SERPER_API_KEY", "")

	res, err := McpPaidSearch.Invoke(t.Context(), map[string]any{
		"query":    "test",
		"provider": "jina",
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	if !strings.Contains(result, "jina answer") {
		t.Errorf("jina 路由异常: %s", result)
	}
}
