package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// TestSourceManifest_去重与损坏容错 验证 manifest 哈希去重与损坏 JSON 容错。
// Python: _SourceManifest (wiki_tools.py L89-158)
func TestSourceManifest_去重与损坏容错(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(src, []byte("hello"), 0o644)

	m := newSourceManifest(dir, nil)
	m.ensureInitialized(t.Context())
	sha, shaErr := m.sha256Of(src)
	if shaErr != nil {
		t.Fatal(shaErr)
	}

	if err := m.record(t.Context(), sha, "a.txt", filepath.Join(dir, "dst")); err != nil {
		t.Fatal(err)
	}
	// 重复 record 跳过（不报错）
	if err := m.record(t.Context(), sha, "a.txt", "x"); err != nil {
		t.Fatal(err)
	}
	if len(m.allEntries()) != 1 {
		t.Errorf("去重失败: %d", len(m.allEntries()))
	}
	// entry 字段
	entry := m.get(sha)
	if entry == nil || (*entry)["name"] != "a.txt" {
		t.Errorf("entry 异常: %v", entry)
	}
	// isKnown 已知哈希判断
	if !m.isKnown(sha) || m.isKnown("unknown-sha") {
		t.Error("isKnown 异常")
	}

	// 非文件路径 sha256Of 报错
	if _, err := m.sha256Of(filepath.Join(dir, "not-exist.txt")); err == nil || !strings.Contains(err.Error(), "Path is not a valid file") {
		t.Errorf("非文件应报错: %v", err)
	}

	// save 后重 load 持久化
	m2 := newSourceManifest(dir, nil)
	m2.ensureInitialized(t.Context())
	if len(m2.allEntries()) != 1 || !m2.isKnown(sha) {
		t.Error("持久化后重载失败")
	}

	// 损坏 JSON → 空 map
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{broken"), 0o644)
	m3 := newSourceManifest(dir, nil)
	m3.ensureInitialized(t.Context())
	if len(m3.allEntries()) != 0 {
		t.Error("损坏 JSON 应吞错为空")
	}
}

// TestSourceManifest_record落盘格式 验证 save 的 MarshalIndent 格式与 ingested_at 字段。
// Python: record (wiki_tools.py L118-128)
func TestSourceManifest_record落盘格式(t *testing.T) {
	dir := t.TempDir()
	m := newSourceManifest(dir, nil)
	m.ensureInitialized(t.Context())

	if err := m.record(t.Context(), "abc123", "doc.pdf", "/dst/x.pdf"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]map[string]string
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("落盘 JSON 解析失败: %v", err)
	}
	entry, ok := parsed["abc123"]
	if !ok {
		t.Fatal("record 未落盘")
	}
	if entry["name"] != "doc.pdf" || entry["destination"] != "/dst/x.pdf" {
		t.Errorf("entry 字段异常: %v", entry)
	}
	if entry["ingested_at"] == "" {
		t.Error("ingested_at 缺失")
	}
}

// TestResolveWikiWorkspace_四分支 验证 workspace 解析的四分支逻辑。
// Python: _resolve_workspace (wiki_tools.py L376-393)
func TestResolveWikiWorkspace_四分支(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows 兼容

	// 分支1: 空串 → AgentWorkspaceDir/.llm_wiki
	ws := resolveWikiWorkspace("")
	if filepath.Base(ws) != ".llm_wiki" {
		t.Errorf("空串应落到 AgentWorkspaceDir/.llm_wiki: %s", ws)
	}
	// 分支1b: ".llm_wiki" 字面量同分支1
	ws2 := resolveWikiWorkspace(".llm_wiki")
	if filepath.Base(ws2) != ".llm_wiki" || ws2 != ws {
		t.Errorf(".llm_wiki 字面量应与空串同结果: %s vs %s", ws2, ws)
	}
	// 分支2: 相对路径 → AgentWorkspaceDir 基
	ws = resolveWikiWorkspace("mylib")
	if !filepath.IsAbs(ws) || filepath.Base(filepath.Dir(ws)) != "mylib" || filepath.Base(ws) != ".llm_wiki" {
		t.Errorf("相对路径应基于 AgentWorkspaceDir: %s", ws)
	}
	// 分支3: ~/ 展开
	ws = resolveWikiWorkspace("~/wikiX")
	if !filepath.IsAbs(ws) || filepath.Base(ws) != ".llm_wiki" {
		t.Errorf("~/ 展开失败: %s", ws)
	}
	// 分支4: 绝对路径末段非 .llm_wiki 追加
	ws = resolveWikiWorkspace(filepath.Join(home, "plain"))
	if filepath.Base(ws) != ".llm_wiki" || filepath.Base(filepath.Dir(ws)) != "plain" {
		t.Errorf("末段应追加 .llm_wiki: %s", ws)
	}
	// 分支4b: 末段已是 .llm_wiki 不重复追加
	ws = resolveWikiWorkspace(filepath.Join(home, "plain", ".llm_wiki"))
	if strings.Count(filepath.ToSlash(ws), ".llm_wiki") != 1 {
		t.Errorf("不应重复追加 .llm_wiki: %s", ws)
	}
}

// TestWikiTemplates_内容逐字 验证三个模板常量内容逐字对齐 Python。
// Python: ensure_initialized 内的 schema/index/log 模板 (wiki_tools.py L233-291)
func TestWikiTemplates_内容逐字(t *testing.T) {
	// AGENT.md 7 条规则骨架
	if !strings.Contains(wikiSchemaContent, "Never modify files inside `sources/`") ||
		!strings.Contains(wikiSchemaContent, "DO NOT create subdirectories") {
		t.Error("schema 模板内容异常")
	}
	if !strings.Contains(wikiSchemaContent, "# Wiki Maintainer Rules") ||
		!strings.Contains(wikiSchemaContent, "7. DO NOT create subdirectories (like `wiki/entity/`). Save all files in the `wiki/` root.") {
		t.Error("schema 模板 7 条规则异常")
	}
	if !strings.Contains(wikiIndexContent, "## Entities") || !strings.Contains(wikiLogContent, "Append-only timeline") {
		t.Error("index/log 模板内容异常")
	}
	if !strings.Contains(wikiIndexContent, "<!-- entity pages go here, one bullet per page -->") {
		t.Error("index 模板 Entities 注释异常")
	}
	if !strings.Contains(wikiLogContent, "## [YYYY-MM-DD] <operation> | <title>") {
		t.Error("log 模板 grep 头异常")
	}
}

// TestWikiPrompts_双语常量 验证系统提示词/描述双语常量存在。
// Python: DEFAULT_WIKI_AGENT_SYSTEM_PROMPT_EN/CN + DESCRIPTION_EN/CN (wiki_tools.py L34-86)
func TestWikiPrompts_双语常量(t *testing.T) {
	if !strings.Contains(wikiAgentSystemPromptEN, "You are an LLM Wiki Maintainer") ||
		!strings.Contains(wikiAgentSystemPromptEN, "read `schema/AGENT.md` first") {
		t.Error("EN 系统提示词异常")
	}
	if !strings.Contains(wikiAgentSystemPromptCN, "你是 LLM Wiki 维护者") {
		t.Error("CN 系统提示词异常")
	}
	if !strings.Contains(wikiAgentDescriptionEN, "Wiki Maintainer agent") {
		t.Error("EN 描述异常")
	}
	if !strings.Contains(wikiAgentDescriptionCN, "Wiki 维护代理") {
		t.Error("CN 描述异常")
	}
}

// TestGetDefaultWikiModel_空配置容错 验证无配置时不 panic。
// Python: _get_default_model (wiki_tools.py L353-373)
func TestGetDefaultWikiModel_空配置容错(t *testing.T) {
	// 无配置时不 panic（GetDefaultModels 环境回退），返回 Model 或错误皆可——验证不 panic
	_, _ = getDefaultWikiModel()
}

// TestGetDefaultWikiModel_全字段透传 验证 M-01 修复：client 侧 max_retries/ssl_cert/custom_headers
// 与 req 侧 temperature/top_p/max_tokens/stop 全部透传进 Model 配置。
// 对齐 Python ModelClientConfig(**client_config) / ModelRequestConfig(**req_config) 全字段透传。
func TestGetDefaultWikiModel_全字段透传(t *testing.T) {
	// 隔离 workspace：UAPCLAW_DATA_DIR + config/ 目录（模拟已初始化防 resources 回退）+ ResetCache
	wsRoot := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", wsRoot)
	upath.ResetCache()
	t.Cleanup(func() {
		os.Unsetenv("UAPCLAW_DATA_DIR")
		upath.ResetCache()
	})
	// 屏蔽环境回退（models.defaults 命中后 env 不参与）
	t.Setenv("MODEL_PROVIDER", "")
	t.Setenv("MODEL_NAME", "")
	t.Setenv("API_KEY", "")
	t.Setenv("API_BASE", "")

	configDir := filepath.Join(wsRoot, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// YAML 数值在解析后为 float64；这里直接写 YAML 走真实加载链
	yamlContent := `models:
  defaults:
    - is_default: true
      model_client_config:
        client_provider: OpenAI
        api_key: wiki-key
        api_base: https://wiki.example.com/v1
        model_name: wiki-full-model
        timeout: 120
        max_retries: 7
        verify_ssl: true
        ssl_cert: /path/to/ca.pem
        custom_headers:
          X-Gateway-Auth: internal-token
          X-Org: org-42
      model_config_obj:
        model: wiki-full-model
        temperature: 0.33
        top_p: 0.77
        max_tokens: 4096
        stop: "END"
`
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := getDefaultWikiModel()
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if m == nil || m.ClientConfig == nil || m.ModelConfig == nil {
		t.Fatal("model/clientConfig/modelConfig 不应为 nil")
	}

	// client 侧断言
	cc := m.ClientConfig
	if cc.Timeout != 120 {
		t.Errorf("timeout 异常: %v", cc.Timeout)
	}
	if cc.MaxRetries != 7 {
		t.Errorf("max_retries 应透传 7: %d", cc.MaxRetries)
	}
	if !cc.VerifySSL {
		t.Error("verify_ssl 应透传 true")
	}
	if cc.SSLCert != "/path/to/ca.pem" {
		t.Errorf("ssl_cert 应透传: %q", cc.SSLCert)
	}
	if cc.CustomHeaders["X-Gateway-Auth"] != "internal-token" || cc.CustomHeaders["X-Org"] != "org-42" {
		t.Errorf("custom_headers 应透传: %v", cc.CustomHeaders)
	}

	// req 侧断言
	mc := m.ModelConfig
	if mc.Temperature != 0.33 {
		t.Errorf("temperature 应透传 0.33: %v", mc.Temperature)
	}
	if mc.TopP == nil || *mc.TopP != 0.77 {
		t.Errorf("top_p 应透传 0.77: %v", mc.TopP)
	}
	if mc.MaxTokens == nil || *mc.MaxTokens != 4096 {
		t.Errorf("max_tokens 应透传 4096: %v", mc.MaxTokens)
	}
	if mc.Stop == nil || *mc.Stop != "END" {
		t.Errorf("stop 应透传 END: %v", mc.Stop)
	}
}

// TestWikiIngest_工具卡schema 验证三个工具的卡片名称。
// Python: wiki_ingest/wiki_query/wiki_lint @tool (wiki_tools.py L396-513)
func TestWikiIngest_工具卡schema(t *testing.T) {
	if WikiIngest.Card().Name != "wiki_ingest" {
		t.Errorf("卡名异常: %s", WikiIngest.Card().Name)
	}
	if WikiQuery.Card().Name != "wiki_query" || WikiLint.Card().Name != "wiki_lint" {
		t.Error("wiki_query/wiki_lint 卡名异常")
	}
	// 描述逐字检查
	if !strings.Contains(WikiIngest.Card().Description, "identical files (by SHA-256) are skipped perfectly") {
		t.Error("wiki_ingest 描述异常")
	}
	if !strings.Contains(WikiQuery.Card().Description, "compiled knowledge base directly via Natural Language") {
		t.Error("wiki_query 描述异常")
	}
	if !strings.Contains(WikiLint.Card().Description, "Health-check and trigger automatic repairs") {
		t.Error("wiki_lint 描述异常")
	}
}

// TestWikiQuery_空query与未初始化workspace 验证防御分支文案。
// Python: wiki_query (wiki_tools.py L460-484)
func TestWikiQuery_空query与未初始化workspace(t *testing.T) {
	// 空 query（try 之外的前置检查）
	res, err := WikiQuery.Invoke(t.Context(), map[string]any{"query": "  "})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	if res["result"] != "Error: Query cannot be empty." {
		t.Errorf("空 query 文案异常: %v", res["result"])
	}

	// 未初始化 workspace → 引导文案（不触发模型调用前返回）
	// 注：需先设置有效模型 env（Python _get_default_model 环境回退后 Model 构造须成功，
	// 否则同样走 except "Wiki Query Error"——对齐 Python 真实路径）
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("MODEL_PROVIDER", "OpenAI")
	t.Setenv("MODEL_NAME", "test-wiki-model")
	t.Setenv("API_KEY", "test-key")
	t.Setenv("API_BASE", "https://test.example.com/v1")
	res, err = WikiQuery.Invoke(t.Context(), map[string]any{
		"query":     "什么是知识库",
		"workspace": "never_inited_ws",
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	if !strings.Contains(result, "does not have an initialized LLM Wiki") ||
		!strings.Contains(result, "You must use 'wiki_ingest' first") {
		t.Errorf("引导文案异常: %v", result)
	}
}

// TestWikiLint_未初始化workspace 验证 wiki_lint 防御分支。
// Python: wiki_lint (wiki_tools.py L493-513)
func TestWikiLint_未初始化workspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("MODEL_PROVIDER", "OpenAI")
	t.Setenv("MODEL_NAME", "test-wiki-model")
	t.Setenv("API_KEY", "test-key")
	t.Setenv("API_BASE", "https://test.example.com/v1")
	res, err := WikiLint.Invoke(t.Context(), map[string]any{
		"workspace": "never_inited_ws_lint",
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	if !strings.Contains(result, "does not have an initialized LLM Wiki") {
		t.Errorf("引导文案异常: %v", result)
	}
}

// TestWikiIngest_源不存在 验证 wiki_ingest 的源不存在分支。
// Python: wiki_ingest (wiki_tools.py L421-422)
func TestWikiIngest_源不存在(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	res, err := WikiIngest.Invoke(t.Context(), map[string]any{
		"source":    "no_such_source_file.txt",
		"workspace": "never_inited_ws_ingest",
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	// 模型构造失败或源不存在——两者皆吞错返回文案；仅验证不 panic 且有文案
	if result == "" {
		t.Error("应有非空文案返回")
	}
}
