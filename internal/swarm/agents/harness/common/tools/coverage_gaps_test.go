package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── wiki_tools 覆盖补充 ────────────────────────────

// TestWikiResultToText_三分支 验证结果转文案三分支。
// Python: wiki_query/wiki_lint 的 output/error/MarshalIndent 分支
func TestWikiResultToText_三分支(t *testing.T) {
	// output 分支
	if got := wikiResultToText(map[string]any{"output": "答案内容"}, "Error querying wiki"); got != "答案内容" {
		t.Errorf("output 分支异常: %q", got)
	}
	// error 分支
	if got := wikiResultToText(map[string]any{"error": "boom"}, "Error querying wiki"); got != "Error querying wiki: boom" {
		t.Errorf("error 分支异常: %q", got)
	}
	// 其他 → MarshalIndent
	got := wikiResultToText(map[string]any{"other": "x"}, "Error querying wiki")
	if !strings.Contains(got, `"other"`) || !strings.Contains(got, `"x"`) {
		t.Errorf("MarshalIndent 分支异常: %q", got)
	}
}

// TestCopyFilePreserveMeta_复制与mtime保留 验证 copy2 语义。
// Python: shutil.copy2(source_path, destination)
func TestCopyFilePreserveMeta_复制与mtime保留(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello-copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFilePreserveMeta(src, dst); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "hello-copy" {
		t.Errorf("复制内容异常: %q", data)
	}
	srcInfo, _ := os.Stat(src)
	dstInfo, _ := os.Stat(dst)
	if !srcInfo.ModTime().Equal(dstInfo.ModTime()) {
		t.Errorf("mtime 未保留: %v vs %v", srcInfo.ModTime(), dstInfo.ModTime())
	}
	// 源不存在报错
	if err := copyFilePreserveMeta(filepath.Join(dir, "nope"), dst); err == nil {
		t.Error("源不存在应报错")
	}
}

// TestIsTruthyWiki_真值表 验证 is_default 判断。
func TestIsTruthyWiki_真值表(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{nil, false}, {true, true}, {false, false},
		{"1", true}, {"true", true}, {"TRUE", true}, {"yes", true}, {"Yes", true},
		{"0", false}, {"no", false}, {"", false}, {1, false},
	}
	for _, c := range cases {
		if got := isTruthyWiki(c.v); got != c.want {
			t.Errorf("isTruthyWiki(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

// TestSourceManifest_load的sysOp分支 验证 sysOp 双分支 load。
// Python: _SourceManifest._load (wiki_tools.py L133-148)
func TestSourceManifest_load的sysOp分支(t *testing.T) {
	dir := t.TempDir()
	// sysOp 为 nil 但文件不存在 → 空 map（os 分支不存在路径）
	m := newSourceManifest(dir, nil)
	m.ensureInitialized(t.Context())
	if len(m.allEntries()) != 0 {
		t.Error("不存在的 manifest 应为空")
	}
	// 非法 JSON 目录路径（os.ReadFile 目录 → err → 空 map）
	m2 := newSourceManifest(dir, nil)
	m2.path = dir // 目录路径 ReadFile 报错
	m2.ensureInitialized(t.Context())
	if len(m2.allEntries()) != 0 {
		t.Error("目录路径应吞错为空")
	}
}

// TestSourceManifest_save的parent创建 验证 parent 不存在时 MkdirAll。
func TestSourceManifest_save的parent创建(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "sources")
	m := newSourceManifest(deep, nil)
	if err := m.record(t.Context(), "sha-x", "f.txt", "dst"); err != nil {
		t.Fatalf("深路径 record 应成功: %v", err)
	}
	if _, err := os.Stat(filepath.Join(deep, "manifest.json")); err != nil {
		t.Errorf("manifest 应落盘: %v", err)
	}
}

// TestListSources_包装方法 验证 listSources 返回 manifest 全量。
// Python: LLMWiki.list_sources (wiki_tools.py L295-296)
func TestListSources_包装方法(t *testing.T) {
	// llmWiki.listSources 只读 manifest——直接构造最小 wiki 验证
	w := &llmWiki{
		manifest: newSourceManifest(t.TempDir(), nil),
	}
	w.manifest.ensureInitialized(t.Context())
	if len(w.listSources()) != 0 {
		t.Error("空 manifest 应返回空")
	}
}

// TestLLMWiki_ingest_源不存在报错 验证 ingest 的 FileNotFoundError。
// Python: LLMWiki.ingest (wiki_tools.py L298-301)
func TestLLMWiki_ingest_源不存在报错(t *testing.T) {
	w := &llmWiki{
		manifest: newSourceManifest(t.TempDir(), nil),
	}
	_, err := w.ingest(t.Context(), "/no/such/file.txt", false)
	if err == nil || !strings.Contains(err.Error(), "Source file not found") {
		t.Errorf("应报 Source file not found: %v", err)
	}
}

// TestLLMWiki_ensureInitialized_目录与模板创建 验证三目录 + 三模板 + 已存在跳过。
// Python: LLMWiki.ensure_initialized (wiki_tools.py L227-293)
func TestLLMWiki_ensureInitialized_目录与模板创建(t *testing.T) {
	dir := t.TempDir()
	w := &llmWiki{
		workspace:  dir,
		sourcesDir: filepath.Join(dir, "sources"),
		wikiDir:    filepath.Join(dir, "wiki"),
		schemaDir:  filepath.Join(dir, "schema"),
		manifest:   newSourceManifest(filepath.Join(dir, "sources"), nil),
		// agent 为 nil——ensureInitialized 尾部 agent.EnsureInitialized 不可达，
		// 此用例验证目录与模板段（agent 段由 CreateDeepAgent 集成路径覆盖）
	}
	// 先手动建目录（对齐 Python MkdirAll 幂等）+ 只测模板函数
	for _, d := range []string{w.sourcesDir, w.wikiDir, w.schemaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.manifest.ensureInitialized(t.Context())

	if err := w.ensureTemplateFile(t.Context(), filepath.Join(w.schemaDir, "AGENT.md"), wikiSchemaContent); err != nil {
		t.Fatal(err)
	}
	// 已存在跳过（不覆盖）
	if err := w.ensureTemplateFile(t.Context(), filepath.Join(w.schemaDir, "AGENT.md"), "CHANGED"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(w.schemaDir, "AGENT.md"))
	if string(data) != wikiSchemaContent {
		t.Error("已存在模板不应被覆盖")
	}
	// index/log 同样创建
	if err := w.ensureTemplateFile(t.Context(), filepath.Join(w.wikiDir, "index.md"), wikiIndexContent); err != nil {
		t.Fatal(err)
	}
	if err := w.ensureTemplateFile(t.Context(), filepath.Join(w.wikiDir, "log.md"), wikiLogContent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.wikiDir, "log.md")); err != nil {
		t.Error("log.md 应创建")
	}
}

// TestStripAny_类型分支 验证 map 值转字符串。
func TestStripAny_类型分支(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, ""},
		{"  x ", "x"},
		{"plain", "plain"},
		{123, "123"},
		{true, "true"},
	}
	for _, c := range cases {
		if got := stripAny(c.v); got != c.want {
			t.Errorf("stripAny(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

// TestResolvePathStringWiki_回退分支 验证 EvalSymlinks 失败回退 Abs。
func TestResolvePathStringWiki_回退分支(t *testing.T) {
	// 存在路径 → EvalSymlinks 成功
	dir := t.TempDir()
	if got := resolvePathStringWiki(dir); got != dir {
		t.Errorf("存在路径应原样: %s vs %s", got, dir)
	}
	// 不存在路径 → EvalSymlinks 失败 → Abs 回退（相对路径转绝对）
	got := resolvePathStringWiki("relative/does/not/exist")
	if !filepath.IsAbs(got) {
		t.Errorf("应回退 Abs: %s", got)
	}
}

// TestExpandUserHomeWiki_分支 验证 ~/ 展开。
func TestExpandUserHomeWiki_分支(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := expandUserHomeWiki("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("~/x 展开异常: %s", got)
	}
	if got := expandUserHomeWiki("~"); got != home {
		t.Errorf("~ 展开异常: %s", got)
	}
	if got := expandUserHomeWiki("/abs/path"); got != "/abs/path" {
		t.Errorf("非 ~ 前缀应原样: %s", got)
	}
	if got := expandUserHomeWiki("rel/path"); got != "rel/path" {
		t.Errorf("相对路径应原样: %s", got)
	}
}

// ──────────────────────────── mcp_search 覆盖补充 ────────────────────────────

// TestNoProxyEntries_解析 验证 no_proxy 解析。
// Python: _no_proxy_entries（search_tools.py）
func TestNoProxyEntries_解析(t *testing.T) {
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	// 双 env 空 → 默认列表非空
	if got := noProxyEntries(); len(got) == 0 {
		t.Error("双 env 空时应回退默认列表")
	}
	t.Setenv("NO_PROXY", "Example.com, .Foo.org , bar.net")
	got := noProxyEntries()
	// 条目 strip + lower
	want := []string{"example.com", ".foo.org", "bar.net"}
	if len(got) != len(want) {
		t.Fatalf("解析数量异常: %v", got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] 期望 %q 实际 %q", i, w, got[i])
		}
	}
}

// TestShouldBypassFreeSearchProxy_命中 验证代理绕过判断。
// Python: _should_bypass_free_search_proxy（search_tools.py）
func TestShouldBypassFreeSearchProxy_命中(t *testing.T) {
	// 无代理配置 → 恒绕过
	t.Setenv("FREE_SEARCH_PROXY_URL", "")
	if !shouldBypassFreeSearchProxy("https://anything.example.com/x") {
		t.Error("无代理配置应恒绕过")
	}

	// 配置代理 + NO_PROXY 命中
	t.Setenv("FREE_SEARCH_PROXY_URL", "http://proxy:8080")
	t.Setenv("NO_PROXY", "example.com,.foo.org")
	if !shouldBypassFreeSearchProxy("http://example.com/x") {
		t.Error("example.com 应绕过")
	}
	if !shouldBypassFreeSearchProxy("https://sub.foo.org/y") {
		t.Error(".foo.org 后缀应绕过")
	}
	if shouldBypassFreeSearchProxy("https://other.org/y") {
		t.Error("other.org 不应绕过")
	}
	// 无效 URL 不绕过
	if shouldBypassFreeSearchProxy("://bad-url") {
		t.Error("无效 URL 不应绕过")
	}
	// * 通配全绕过
	t.Setenv("NO_PROXY", "*")
	if !shouldBypassFreeSearchProxy("https://anything.net/z") {
		t.Error("* 通配应全绕过")
	}
}

// TestEngineDisplayName_映射 验证引擎显示名。
// Python: _engine_display_name（search_tools.py）
func TestEngineDisplayName_映射(t *testing.T) {
	cases := map[string]string{
		"duckduckgo":       "DuckDuckGo",
		"duckduckgo-jina":  "DuckDuckGo (via jina.ai)",
		"bing":             "Bing",
		"unknown-engine":   "unknown-engine",
	}
	for engine, want := range cases {
		if got := engineDisplayName(engine); got != want {
			t.Errorf("engineDisplayName(%s) = %q, want %q", engine, got, want)
		}
	}
}

// TestPerplexitySearch_真实请求 验证 PPLX provider 完整链路。
// Python: _perplexity_search（search_tools.py）
func TestPerplexitySearch_真实请求(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"citations": ["https://p1.example.com/a", "https://p2.example.com/b"], "choices": [{"message": {"content": "pplx answer"}}]}`))
	}))
	defer srv.Close()
	orig := perplexityAPIURL
	perplexityAPIURL = srv.URL
	t.Cleanup(func() { perplexityAPIURL = orig })

	t.Setenv("PERPLEXITY_API_KEY", "pplx-key")
	result, err := perplexitySearch("test query", 5, 10)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result.Provider != "perplexity" {
		t.Errorf("provider 异常: %q", result.Provider)
	}
	if result.Answer != "pplx answer" {
		t.Errorf("answer 异常: %q", result.Answer)
	}
	if len(result.URLs) != 2 || result.URLs[0] != "https://p1.example.com/a" {
		t.Errorf("citations 抽取异常: %v", result.URLs)
	}
}

// TestPerplexitySearch_无key 验证空 key 报错。
func TestPerplexitySearch_无key(t *testing.T) {
	t.Setenv("PERPLEXITY_API_KEY", "")
	if _, err := perplexitySearch("q", 5, 10); err == nil {
		t.Error("无 key 应报错")
	}
}

// TestJinaSearch_真实请求 验证 Jina deepsearch 完整链路。
// Python: _jina_search（search_tools.py）
func TestJinaSearch_真实请求(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "jina answer with [link](https://j1.example.com/x) and [link2](https://j2.example.com/y)"}}]}`))
	}))
	defer srv.Close()
	orig := jinaDeepSearchAPIURLVar
	jinaDeepSearchAPIURLVar = srv.URL
	t.Cleanup(func() { jinaDeepSearchAPIURLVar = orig })

	t.Setenv("JINA_API_KEY", "jina-key")
	result, err := jinaSearch("test query", 10)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result.Provider != "jina" {
		t.Errorf("provider 异常: %q", result.Provider)
	}
	if !strings.Contains(result.Answer, "jina answer") {
		t.Errorf("answer 异常: %q", result.Answer)
	}
	if len(result.URLs) != 2 || result.URLs[0] != "https://j1.example.com/x" {
		t.Errorf("URL 抽取异常: %v", result.URLs)
	}
	// 无 key 报错
	t.Setenv("JINA_API_KEY", "")
	if _, err := jinaSearch("q", 10); err == nil {
		t.Error("无 key 应报错")
	}
}
