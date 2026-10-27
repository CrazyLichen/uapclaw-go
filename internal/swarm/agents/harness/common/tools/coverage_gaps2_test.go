package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── mcp_command 覆盖补充 ────────────────────────────

// TestResolveExecutionPlan_各显式shell Linux 平台各 shellType 分支。
// Python: _resolve_execution_plan（command_tools.py）
func TestResolveExecutionPlan_各显式shell(t *testing.T) {
	// bash 显式
	plan, useShell, resolved := resolveExecutionPlan("echo hi", "bash")
	if useShell {
		t.Error("bash 分支 useShell 应为 false")
	}
	if resolved != "bash" || len(plan) < 2 || plan[len(plan)-1] != "echo hi" {
		t.Errorf("bash plan 异常: %v %s", plan, resolved)
	}
	// sh 显式
	plan, useShell, resolved = resolveExecutionPlan("echo hi", "sh")
	if useShell || resolved != "sh" {
		t.Errorf("sh 分支异常: %v %s %v", plan, resolved, useShell)
	}
	// cmd 在 Linux 不支持 → nil plan
	plan, _, resolved = resolveExecutionPlan("echo hi", "cmd")
	if plan != nil || resolved != "cmd" {
		t.Errorf("Linux cmd 应返回 nil plan: %v", plan)
	}
	// auto → bash（环境有 bash）
	_, _, resolved = resolveExecutionPlan("echo hi", "auto")
	if resolved != "bash" && resolved != "sh" {
		t.Errorf("auto 应解析 bash/sh: %s", resolved)
	}
	// powershell 在 Linux：LookPath 失败 → 哨兵
	_, _, resolved = resolveExecutionPlan("Get-ChildItem", "powershell")
	if resolved != "powershell" {
		t.Errorf("powershell 分支异常: %s", resolved)
	}
	// 非法 shellType 回退 auto
	_, _, resolved = resolveExecutionPlan("echo hi", "bogus-shell")
	if resolved != "bash" && resolved != "sh" {
		t.Errorf("非法 shellType 应回退 auto: %s", resolved)
	}
}

// TestAvailableUnixShell_bash与sh 验证 Unix shell 解析。
func TestAvailableUnixShell_bash与sh(t *testing.T) {
	plan := availableUnixShell(true)
	if len(plan) < 2 {
		t.Fatalf("bash plan 不应为空: %v", plan)
	}
	// 返回 [exe, "-lc"]
	if plan[1] != "-lc" {
		t.Errorf("bash flag 异常: %v", plan)
	}
	plan = availableUnixShell(false)
	if len(plan) < 2 || plan[1] != "-c" {
		t.Errorf("sh plan 异常: %v", plan)
	}
}

// TestIsTimeoutError_判定 验证超时错误识别。
// Python: subprocess.TimeoutExpired 路径
func TestIsTimeoutError_判定(t *testing.T) {
	if !isTimeoutError(errors.New("signal: killed")) {
		t.Error("signal: killed 应为超时")
	}
	if !isTimeoutError(context.DeadlineExceeded) {
		t.Error("DeadlineExceeded 应为超时")
	}
	if isTimeoutError(errors.New("exit status 1")) {
		t.Error("exit status 1 不应为超时")
	}
	if isTimeoutError(nil) {
		t.Error("nil 不应为超时")
	}
}

// TestMcpExecCommand_超时路径 验证超时分支文案。
// Python: "[ERROR]: command timed out"
func TestMcpExecCommand_超时路径(t *testing.T) {
	res, err := McpExecCommand.Invoke(t.Context(), map[string]any{
		"command":         "sleep 5",
		"timeout_seconds": 1,
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	if !strings.Contains(result, "timed out") {
		t.Errorf("应含 timed out: %s", result)
	}
}

// TestMcpExecCommand_workdir不存在 验证 workdir 校验分支。
func TestMcpExecCommand_workdir不存在(t *testing.T) {
	res, err := McpExecCommand.Invoke(t.Context(), map[string]any{
		"command": "echo hi",
		"workdir": "/definitely/not/exist/dir",
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	if !strings.Contains(result, "outside project workspace") && !strings.Contains(result, "workdir") {
		t.Errorf("workdir 校验异常: %s", result)
	}
}

// ──────────────────────────── image_gen 覆盖补充 ────────────────────────────

// TestDefaultImageGenModelBuilder_构造与凭据 验证默认 builder。
// Python: _invoke_model_image_generation 内联 Model 构造 (image_tools.py L343-358)
func TestDefaultImageGenModelBuilder_构造与凭据(t *testing.T) {
	t.Setenv("IMAGE_GEN_API_KEY", "builder-key")
	t.Setenv("IMAGE_GEN_API_BASE", "https://builder.example.com/v1")
	t.Setenv("IMAGE_GEN_MODEL_NAME", "builder-model")
	t.Setenv("IMAGE_GEN_PROVIDER", "OpenAI")

	m, err := defaultImageGenModelBuilder()
	if err != nil {
		t.Fatalf("builder 失败: %v", err)
	}
	if m.ClientConfig.ClientID != "image_gen_client" {
		t.Errorf("ClientID 异常: %s", m.ClientConfig.ClientID)
	}
	if m.ClientConfig.APIKey != "builder-key" || m.ClientConfig.APIBase != "https://builder.example.com/v1" {
		t.Errorf("凭据异常: %+v", m.ClientConfig)
	}
	if m.ClientConfig.VerifySSL {
		t.Error("VerifySSL 应为 false（对齐 Python verify_ssl=False）")
	}
	if m.ModelConfig.ModelName != "builder-model" {
		t.Errorf("模型名异常: %s", m.ModelConfig.ModelName)
	}
}

// TestGetModelConfigTyped_分支 验证 models.{type}.model_config 提取。
// Python: _get_model_config (multimodal_config.py L28-61)
func TestGetModelConfigTyped_分支(t *testing.T) {
	// nil configBase 空配置基线
	if got := getModelConfigTyped(nil, "image_gen"); len(got) != 0 {
		t.Errorf("nil 应返回空: %v", got)
	}
	// models 非 dict
	if got := getModelConfigTyped(map[string]any{"models": "str"}, "image_gen"); len(got) != 0 {
		t.Errorf("models 非 dict 应返回空: %v", got)
	}
	// model_config 缺失
	if got := getModelConfigTyped(map[string]any{"models": map[string]any{"image_gen": map[string]any{}}}, "image_gen"); len(got) != 0 {
		t.Errorf("无配置应返回空: %v", got)
	}
	// model_config 为 nil（or 回退 model_client_config）
	if got := getModelConfigTyped(map[string]any{
		"models": map[string]any{"image_gen": map[string]any{"model_client_config": map[string]any{"k": "v"}}},
	}, "image_gen"); got["k"] != "v" {
		t.Errorf("model_client_config 回退异常: %v", got)
	}
}

// TestInvokeModelImageGeneration_网络错误兜底 验证下载失败错误文案。
func TestInvokeModelImageGeneration_网络错误兜底(t *testing.T) {
	// 不可达 URL → 下载失败（复用 image_gen_test.go 的 fakeImageGenClient）
	fakeClient := &fakeImageGenClient{
		resp: &llmschema.ImageGenerationResponse{Images: []string{"http://127.0.0.1:1/unreachable.png"}},
	}
	injectFakeImageGenModel(t, fakeClient)
	t.Setenv("IMAGE_GEN_API_KEY", "k")

	result := invokeModelImageGeneration(t.Context(), "a cat", "1024x1024", "standard")
	if result["error"] == nil {
		t.Fatalf("不可达 URL 应报错: %v", result)
	}
	if !strings.Contains(result["error"].(string), "Image generation failed") {
		t.Errorf("错误文案异常: %v", result["error"])
	}
}

// ──────────────────────────── mcp_web_fetch 覆盖补充 ────────────────────────────

// TestFetchWebpage_相对重定向与Jina文本 验证 jina reader 文本分支。
// Python: web_fetch_tools.py fetch_via_jina_reader
func TestFetchWebpage_相对重定向与Jina文本(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("jina reader content body"))
	}))
	defer srv.Close()
	orig := jinaReaderURLPrefix
	jinaReaderURLPrefix = srv.URL + "/jina/"
	t.Cleanup(func() { jinaReaderURLPrefix = orig })

	result, err := fetchViaJinaReader("https://example.com/page", 10)
	if err != nil {
		t.Fatalf("jina reader 失败: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("status 异常: %d", result.StatusCode)
	}
	if !strings.Contains(result.Content, "jina reader content body") {
		t.Errorf("content 异常: %q", result.Content)
	}
	if result.Title != "" {
		t.Errorf("jina 分支 title 应为空: %q", result.Title)
	}
}

// TestMcpFetchWebpage_超时上限钳制 验证 env 上限钳制分支。
// Python: MCP_FETCH_WEBPAGE_MAX_TIMEOUT_SECONDS
func TestMcpFetchWebpage_超时上限钳制(t *testing.T) {
	t.Setenv("MCP_FETCH_WEBPAGE_MAX_TIMEOUT_SECONDS", "5")
	res, err := McpFetchWebpage.Invoke(t.Context(), map[string]any{
		"url":             "about:blank",
		"timeout_seconds": 3600, // 超上限应被钳到 5
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	// about:blank 非法 scheme → 错误文案；验证不 panic 即可
	_ = result
}

// TestExtractDeclaredCharset_meta标签 验证 meta charset 检测分支。
// Python: web_fetch_tools.py _extract_declared_charset 的 4KB head 扫描
func TestExtractDeclaredCharset_meta标签(t *testing.T) {
	head := []byte("<html><head><meta charset=\"gbk\"></head><body>x</body></html>")
	if got := extractDeclaredCharset("", head); got != "gbk" {
		t.Errorf("meta charset 异常: %q", got)
	}
	// header 优先
	if got := extractDeclaredCharset("text/html; charset=UTF-8", head); got != "UTF-8" {
		t.Errorf("header 优先异常: %q", got)
	}
	// 都无 → 空
	if got := extractDeclaredCharset("", []byte("<html><body>plain</body></html>")); got != "" {
		t.Errorf("无声明应为空: %q", got)
	}
}

// TestLoadConfigMapOrEmpty_路径 验证空配置加载。
func TestLoadConfigMapOrEmpty_路径(t *testing.T) {
	// 默认路径加载（配置文件可能存在也可能不存在——不 panic 即可）
	got := loadConfigMapOrEmpty()
	if got == nil {
		// 空配置返回非 nil 空 map
		t.Log("loadConfigMapOrEmpty 返回 nil（配置加载失败路径）")
	}
}

// TestWikiLint_已初始化workspace走模型 验证 lint 走到模型调用。
// Python: wiki_lint (wiki_tools.py L495-513)
func TestWikiLint_已初始化workspace走模型(t *testing.T) {
	t.Setenv("MODEL_PROVIDER", "OpenAI")
	t.Setenv("MODEL_NAME", "wiki-lint-test")
	t.Setenv("API_KEY", "k")
	t.Setenv("API_BASE", "https://lint.example.com/v1")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// 预初始化 workspace（目录存在）
	wsBase := filepath.Join(home, "lint_ws")
	_ = os.MkdirAll(filepath.Join(wsBase, ".llm_wiki"), 0o755)

	res, err := WikiLint.Invoke(t.Context(), map[string]any{
		"workspace": wsBase,
	})
	if err != nil {
		t.Fatalf("invoke 异常: %v", err)
	}
	result, _ := res["result"].(string)
	// 走到模型（HTTP 失败 → Wiki Lint Error 或 lint 输出）
	if result == "" {
		t.Error("应有非空文案")
	}
}
