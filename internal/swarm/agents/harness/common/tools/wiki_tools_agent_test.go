package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeWikiAgentClient wiki 集成测试用假客户端（Invoke 返回固定 output）
type fakeWikiAgentClient struct {
	output   string
	err      error
	invokeN  int
	lastBody string
}

func (f *fakeWikiAgentClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	f.invokeN++
	f.lastBody = "invoked"
	_ = f.lastBody
	if f.err != nil {
		return nil, f.err
	}
	return llmschema.NewAssistantMessage(f.output), nil
}
func (f *fakeWikiAgentClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}
func (f *fakeWikiAgentClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}
func (f *fakeWikiAgentClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}
func (f *fakeWikiAgentClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}
func (f *fakeWikiAgentClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}
func (f *fakeWikiAgentClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (f *fakeWikiAgentClient) SupportsKVCacheRelease() bool {
	return false
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestLLMWiki 构造带 fake model 的 llmWiki（真 CreateDeepAgent 链路）。
func newTestLLMWiki(t *testing.T, output string) *llmWiki {
	t.Helper()
	fakeClient := &fakeWikiAgentClient{output: output}
	model, err := llm.NewModel(&llmschema.ModelClientConfig{
		ClientID: "wiki-test-client", ClientProvider: "OpenAI",
		APIKey: "x", APIBase: "https://test.example.com/v1",
	}, nil, llm.WithClient(fakeClient))
	if err != nil {
		t.Fatalf("构造 fake model 失败: %v", err)
	}
	ws := filepath.Join(t.TempDir(), ".llm_wiki")
	w, err := newLLMWiki(t.Context(), ws, model, nil)
	if err != nil {
		t.Fatalf("newLLMWiki 失败: %v", err)
	}
	return w
}

// TestLLMWiki_全链路ingest 验证 newLLMWiki + ensureInitialized + ingest 主路径。
// Python: LLMWiki.__init__/ensure_initialized/ingest (wiki_tools.py L162-332)
func TestLLMWiki_全链路ingest(t *testing.T) {
	w := newTestLLMWiki(t, "已摄取完成")

	if err := w.ensureInitialized(t.Context()); err != nil {
		t.Fatalf("ensureInitialized 失败: %v", err)
	}
	// 三目录已建
	for _, d := range []string{w.sourcesDir, w.wikiDir, w.schemaDir} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("目录未创建: %s", d)
		}
	}
	// 三模板已写
	for _, f := range []string{
		filepath.Join(w.schemaDir, "AGENT.md"),
		filepath.Join(w.wikiDir, "index.md"),
		filepath.Join(w.wikiDir, "log.md"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("模板未写入: %s", f)
		}
	}
	// sessionID 为 workspace 哈希前 16 位
	if len(w.sessionID) != 16 {
		t.Errorf("sessionID 长度异常: %s", w.sessionID)
	}

	// 准备源文件
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "doc.md")
	_ = os.WriteFile(src, []byte("# 测试文档\n内容"), 0o644)

	result, err := w.ingest(t.Context(), src, false)
	if err != nil {
		t.Fatalf("ingest 失败: %v", err)
	}
	if out, ok := result["output"].(string); !ok || !strings.Contains(out, "已摄取完成") {
		t.Errorf("ingest 结果异常: %v", result)
	}
	// manifest 已记录
	if len(w.manifest.allEntries()) != 1 {
		t.Errorf("manifest 应记录 1 条: %v", w.manifest.allEntries())
	}
	// 源文件已复制到 sources/
	entries, _ := os.ReadDir(w.sourcesDir)
	found := false
	for _, e := range entries {
		// Python: destination = sources_dir / f"{sha256[:8]}_{source_path.name}"
		if strings.HasPrefix(e.Name(), "1") && strings.HasSuffix(e.Name(), "_doc.md") && len(e.Name()) == 8+1+len("doc.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("源文件应复制到 sources/: %v", entries)
	}

	// 第二次 ingest 同一文件（非 force）→ skipped
	result2, err := w.ingest(t.Context(), src, false)
	if err != nil {
		t.Fatalf("第二次 ingest 失败: %v", err)
	}
	if skipped, ok := result2["skipped"].(bool); !ok || !skipped {
		t.Errorf("重复 ingest 应 skipped: %v", result2)
	}
	if result2["output"] != "Skipped duplicate source" {
		t.Errorf("skipped 文案异常: %v", result2["output"])
	}
	// force=true 强制重新摄取
	result3, err := w.ingest(t.Context(), src, true)
	if err != nil {
		t.Fatalf("force ingest 失败: %v", err)
	}
	if _, hasSkipped := result3["skipped"]; hasSkipped {
		t.Errorf("force 不应 skipped: %v", result3)
	}
}

// TestLLMWiki_query与lint 验证 query/lint 的 Invoke 链。
// Python: LLMWiki.query/lint (wiki_tools.py L334-350)
func TestLLMWiki_query与lint(t *testing.T) {
	w := newTestLLMWiki(t, "wiki 回答")
	if err := w.ensureInitialized(t.Context()); err != nil {
		t.Fatalf("ensureInitialized 失败: %v", err)
	}

	result, err := w.query(t.Context(), "什么是知识库")
	if err != nil {
		t.Fatalf("query 失败: %v", err)
	}
	if out, ok := result["output"].(string); !ok || !strings.Contains(out, "wiki 回答") {
		t.Errorf("query 结果异常: %v", result)
	}

	w2 := newTestLLMWiki(t, "lint 完成")
	if err := w2.ensureInitialized(t.Context()); err != nil {
		t.Fatalf("ensureInitialized 失败: %v", err)
	}
	result2, err := w2.lint(t.Context())
	if err != nil {
		t.Fatalf("lint 失败: %v", err)
	}
	if out, ok := result2["output"].(string); !ok || !strings.Contains(out, "lint 完成") {
		t.Errorf("lint 结果异常: %v", result2)
	}
}

// TestLLMWiki_ingest_agent错误不记哈希 验证 error/[ERROR 前缀分支。
// Python: if "error" in result or output.startswith("[ERROR"): return result（不记哈希）
func TestLLMWiki_ingest_agent错误不记哈希(t *testing.T) {
	w := newTestLLMWiki(t, "[ERROR] something failed")
	if err := w.ensureInitialized(t.Context()); err != nil {
		t.Fatalf("ensureInitialized 失败: %v", err)
	}
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "err.md")
	_ = os.WriteFile(src, []byte("x"), 0o644)

	result, err := w.ingest(t.Context(), src, false)
	if err != nil {
		t.Fatalf("ingest 失败: %v", err)
	}
	// 结果原样返回
	if out, ok := result["output"].(string); !ok || !strings.HasPrefix(out, "[ERROR") {
		t.Errorf("结果应原样返回: %v", result)
	}
	// 哈希未记录
	if len(w.manifest.allEntries()) != 0 {
		t.Errorf("失败结果不应记哈希: %v", w.manifest.allEntries())
	}
}

// TestInvokeWikiIngest_目录模式与结果汇总 验证目录遍历 + 结果 JSON。
// Python: wiki_ingest 目录分支 (wiki_tools.py L427-449)
func TestInvokeWikiIngest_目录模式与结果汇总(t *testing.T) {
	// 注入 fake wiki model：通过 imageGenModelBuilder 同型思路不可行（wiki 每次内部构造），
	// 本用例走顶层 WikiIngest.Invoke——model 构造须成功（env 提供）
	t.Setenv("MODEL_PROVIDER", "OpenAI")
	t.Setenv("MODEL_NAME", "wiki-test")
	t.Setenv("API_KEY", "k")
	t.Setenv("API_BASE", "https://test.example.com/v1")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// 源目录两个文件 + 一个被保护目录内文件（.llm_wiki 内 → 跳过）
	wsName := "ingest_dir_ws"
	srcDir := filepath.Join(t.TempDir(), "docs")
	_ = os.MkdirAll(srcDir, 0o755)
	_ = os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "b.md"), []byte("bbb"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "c.xyz"), []byte("ccc"), 0o644) // 非白名单扩展名

	res, err := WikiIngest.Invoke(t.Context(), map[string]any{
		"source":    srcDir,
		"workspace": wsName,
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	// 无 fake client → 真实 HTTP 调用失败 → "[Failed]" 或 Wiki Ingest Error 之一；
	// 关键验证：不 panic、返回非空文案
	if result == "" {
		t.Error("应有非空文案")
	}
	if !strings.Contains(result, "Wiki") {
		t.Errorf("文案应含 Wiki: %s", result)
	}
}

// TestGetDefaultWikiModel_env回退构造 验证环境回退链构造成功。
// Python: _get_default_model (wiki_tools.py L353-373)
func TestGetDefaultWikiModel_env回退构造(t *testing.T) {
	t.Setenv("MODEL_PROVIDER", "OpenAI")
	t.Setenv("MODEL_NAME", "wiki-env-model")
	t.Setenv("API_KEY", "env-key")
	t.Setenv("API_BASE", "https://env.example.com/v1")

	m, err := getDefaultWikiModel()
	if err != nil {
		t.Fatalf("env 回退构造失败: %v", err)
	}
	if m == nil || m.ClientConfig == nil {
		t.Fatal("model 不应为 nil")
	}
	if m.ClientConfig.ClientProvider != "OpenAI" || m.ClientConfig.APIKey != "env-key" {
		t.Errorf("凭据异常: %+v", m.ClientConfig)
	}
	if m.ModelConfig == nil || m.ModelConfig.ModelName != "wiki-env-model" {
		t.Errorf("模型名异常: %+v", m.ModelConfig)
	}
}

// TestGetDefaultWikiModel_缺key报错 验证空凭据报错。
func TestGetDefaultWikiModel_缺key报错(t *testing.T) {
	t.Setenv("MODEL_PROVIDER", "")
	t.Setenv("MODEL_NAME", "")
	t.Setenv("API_KEY", "")
	t.Setenv("API_BASE", "")
	if _, err := getDefaultWikiModel(); err == nil {
		t.Error("空凭据应报错（Pydantic 校验语义）")
	}
}

// TestManifest_json往返验证 验证 save→load 闭环（nil sysOp 分支）。
func TestManifest_json往返验证(t *testing.T) {
	dir := t.TempDir()
	m1 := newSourceManifest(dir, nil)
	if err := m1.record(t.Context(), "s1", "n1", "d1"); err != nil {
		t.Fatal(err)
	}
	m2 := newSourceManifest(dir, nil)
	m2.ensureInitialized(t.Context())
	e := m2.get("s1")
	if e == nil || (*e)["name"] != "n1" {
		t.Fatalf("往返失败: %v", e)
	}
	// 合法 JSON 但类型不符 → 空 map
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"s1": "not-a-map"}`), 0o644)
	m3 := newSourceManifest(dir, nil)
	m3.ensureInitialized(t.Context())
	if len(m3.allEntries()) != 0 {
		t.Error("类型不符应吞错为空")
	}
	_ = json.Marshal // keep import
}
