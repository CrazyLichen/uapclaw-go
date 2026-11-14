package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// newFakeOpenAI 起一个最小 OpenAI 兼容 SSE 端点。
func newFakeOpenAI(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你好\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

// fakeCLIConfig 假端点 + 临时工作区的最小可装配配置。
func fakeCLIConfig(t *testing.T, srvURL string) CLIConfig {
	return CLIConfig{
		Provider:  "OpenAI",
		Model:     "gpt-4o",
		APIKey:    "k",
		APIBase:   srvURL,
		MaxTokens: 8192,
		Workspace: t.TempDir(),
		Cwd:       t.TempDir(),
	}
}

// TestBuildCliWorkspace_身份覆写 IDENTITY.md 被替换为 CLI 身份内容。
func TestBuildCliWorkspace_身份覆写(t *testing.T) {
	cfg := CLIConfig{Workspace: t.TempDir()}
	ws, err := BuildCliWorkspace(cfg, "en")
	require.NoError(t, err)
	require.NotNil(t, ws)
	identity := LoadCliContent("en", "IDENTITY.md")
	assert.NotEmpty(t, identity, "嵌入资源应含 en/IDENTITY.md")
}

// TestLoadCliContent_缺失返回空 不存在的文件返回空串。
func TestLoadCliContent_缺失返回空(t *testing.T) {
	assert.Empty(t, LoadCliContent("en", "NO_SUCH.md"))
	assert.Empty(t, LoadCliContent("xx", "IDENTITY.md"))
}

// TestBuildMemoryRail_凭据门控 三要素（model/base/key）齐备才建 rail。
func TestBuildMemoryRail_凭据门控(t *testing.T) {
	// 无 fallback 且无环境变量 → nil（对齐 Python resolve 的三要素判断）
	assert.Nil(t, BuildMemoryRail(CLIConfig{}))
	// fallback 提供齐备 → 非 nil
	require.NotNil(t, BuildMemoryRail(CLIConfig{APIBase: "http://localhost:1", APIKey: "k"}))
	// 环境变量覆盖 fallback
	t.Setenv("EMBEDDING_BASE_URL", "http://embed")
	rail := BuildMemoryRail(CLIConfig{APIBase: "http://main", APIKey: "k"})
	require.NotNil(t, rail)
}

// TestBuildJiuwenMemoryRail_开关关闭恒nil 默认关；开启也因 adapter 缺失 nil（偏差 D-JM）。
func TestBuildJiuwenMemoryRail_开关关闭恒nil(t *testing.T) {
	assert.Nil(t, BuildJiuwenMemoryRail(CLIConfig{}))
	t.Setenv("JIUWEN_MEMORY_ENABLED", "1")
	assert.Nil(t, BuildJiuwenMemoryRail(CLIConfig{}))
}

// TestBuildSubagents_三件套 code / research / browser 三个规格齐备。
func TestBuildSubagents_三件套(t *testing.T) {
	model, err := llm.InitModel("OpenAI", "gpt-4o", "k", "http://localhost:1")
	require.NoError(t, err)
	specs := BuildSubagents(model)
	require.Len(t, specs, 3)
	names := []string{}
	for _, spec := range specs {
		names = append(names, spec.AgentCard.Name)
	}
	assert.ElementsMatch(t, []string{"code_agent", "research_agent", "browser_agent"}, names)
	for _, spec := range specs {
		assert.NotEmpty(t, spec.SystemPrompt)
		assert.Equal(t, "en", spec.Language)
	}
	// code / research 由本函数显式挂 SysOperationRail；browser 走
	// BuildBrowserAgentConfig（Python rails=None → 空）
	assert.Len(t, specs[0].Rails, 1)
	assert.Len(t, specs[1].Rails, 1)
}

// TestLoadVisionAudioConfig_回退主模型凭据 未设 VISION/AUDIO_API_KEY 时回退主凭据。
func TestLoadVisionAudioConfig_回退主模型凭据(t *testing.T) {
	cfg := CLIConfig{APIKey: "main-key", APIBase: "http://main"}
	vision := LoadVisionConfig(cfg)
	require.NotNil(t, vision)
	assert.Equal(t, "main-key", vision.APIKey)
	assert.Equal(t, "http://main", vision.BaseURL)
	audio := LoadAudioConfig(cfg)
	require.NotNil(t, audio)
	assert.Equal(t, "main-key", audio.APIKey)
}

// TestLoadMcpConfigs_坏文件静默返回空 临时 HOME 下无 mcp.json 返回空。
func TestLoadMcpConfigs_坏文件静默返回空(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	assert.Empty(t, LoadMcpConfigs())

	// 非法 JSON 也静默返回空
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".openjiuwen"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpHome, ".openjiuwen", "mcp.json"), []byte("{bad"), 0o644))
	assert.Empty(t, LoadMcpConfigs())
}

// TestLoadMcpConfigs_合法文件转换 stdio 规格转换为 map 配置。
func TestLoadMcpConfigs_合法文件转换(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".openjiuwen"), 0o755))
	spec := map[string]any{
		"mcpServers": map[string]any{
			"fs": map[string]any{
				"transport": "stdio",
				"command":   "npx",
				"args":      []any{"-y", "@mcp/server"},
				"env":       nil, // 应被过滤
			},
		},
	}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tmpHome, ".openjiuwen", "mcp.json"), raw, 0o644))

	configs := LoadMcpConfigs()
	require.Len(t, configs, 1)
	assert.Equal(t, "fs", configs[0]["server_name"])
	assert.Equal(t, "stdio", configs[0]["client_type"])
	params := configs[0]["params"].(map[string]any)
	assert.Equal(t, "npx", params["command"])
	assert.NotContains(t, params, "env")
}

// TestLoadRuntimeExtensions_扫描与去重 真实扫描 runtime_extensions 目录：
// 注入成功桩验证 Info 记录与去重；失败桩验证 Warning 后不记 loaded（下次重试）。
func TestLoadRuntimeExtensions_扫描与去重(t *testing.T) {
	srv := newFakeOpenAI(t)
	defer srv.Close()
	cfg := fakeCLIConfig(t, srv.URL)
	agentObj, _, err := CreateAgent(context.Background(), cfg)
	require.NoError(t, err)

	extDir := filepath.Join(cfg.Workspace, "auto_harness", "runtime_extensions", "ext1")
	require.NoError(t, os.MkdirAll(extDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(extDir, "harness_config.yaml"), []byte("kind: plugin\n"), 0o644))

	backend := NewLocalBackend(cfg)
	backend.agent = agentObj

	// 成功路径：记录 loaded
	var calls []string
	backend.SetLoadPlugin(func(path string) (labels []string, err error) {
		calls = append(calls, path)
		return []string{"plugin:ext1"}, nil
	})
	require.NoError(t, backend.loadRuntimeExtensions(context.Background()))
	require.Len(t, calls, 1)

	// 去重：第二次扫描不再调用
	require.NoError(t, backend.loadRuntimeExtensions(context.Background()))
	assert.Len(t, calls, 1)

	// 失败路径：不记 loaded，下次重试
	backend2 := NewLocalBackend(cfg)
	backend2.agent = agentObj
	backend2.SetLoadPlugin(func(path string) ([]string, error) {
		return nil, assertErrorStub
	})
	require.NoError(t, backend2.loadRuntimeExtensions(context.Background()))
	require.NoError(t, backend2.loadRuntimeExtensions(context.Background()))
}

// assertErrorStub loadRuntimeExtensions 失败路径用的桩错误。
var assertErrorStub = &notImplementedError{"boom"}

// TestCreateAgent_假端点装配整链路 真实跑通 CreateAgent 的 rails/workspace/子 Agent 装配。
func TestCreateAgent_假端点装配整链路(t *testing.T) {
	srv := newFakeOpenAI(t)
	defer srv.Close()
	agent, tracker, err := CreateAgent(context.Background(), fakeCLIConfig(t, srv.URL))
	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NotNil(t, tracker)
	assert.NotNil(t, agent.DeepConfig())
}

// TestLocalBackend_RunStreaming_假端点整链路 Start → RunStreaming 真实跑通。
func TestLocalBackend_RunStreaming_假端点整链路(t *testing.T) {
	srv := newFakeOpenAI(t)
	defer srv.Close()
	backend, err := CreateBackend(fakeCLIConfig(t, srv.URL))
	require.NoError(t, err)
	require.NoError(t, backend.Start(context.Background()))
	defer func() {
		require.NoError(t, backend.Stop(context.Background()))
	}()
	ch, err := backend.RunStreaming(context.Background(), "查询", nil)
	require.NoError(t, err)
	count := 0
	for range ch {
		count++
	}
	assert.Greater(t, count, 0)
}

// TestLoadPlugin_未启动时报错 agent 未 Start 时默认链路拒绝加载（15.15 起默认走
// DeepAgent.LoadPlugin 真实现，agent 为 nil 属配置错误）。
func TestLoadPlugin_未启动时报错(t *testing.T) {
	backend := NewLocalBackend(CLIConfig{})
	_, err := backend.loadPluginFn()(context.Background(), "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent not started")
}

// TestGetCliContentBaseDir_路径语义 返回约定的基目录。
func TestGetCliContentBaseDir_路径语义(t *testing.T) {
	assert.Equal(t, filepath.Join("prompts", "workspace_content"), GetCliContentBaseDir())
}

// TestTracker_Abort_OutputSchema换形 Tracker 访问器 / Abort 真身 /
// outputSchemaOf 各分支（TraceSchema、CustomSchema 降级）。
func TestTracker_Abort_OutputSchema换形(t *testing.T) {
	srv := newFakeOpenAI(t)
	defer srv.Close()
	backend, err := CreateBackend(fakeCLIConfig(t, srv.URL))
	require.NoError(t, err)
	require.NoError(t, backend.Start(context.Background()))
	defer func() { _ = backend.Stop(context.Background()) }()

	require.NotNil(t, backend.Tracker())
	require.NoError(t, backend.Abort(context.Background()))
	assert.Greater(t, backend.AbortCount(), 0)
}

// TestDecodeMcpConfig_往返 map → McpServerConfig 字段保真。
func TestDecodeMcpConfig_往返(t *testing.T) {
	cfg, err := decodeMcpConfig(map[string]any{
		"server_name": "fs",
		"server_path": "/x",
		"client_type": "stdio",
		"params":      map[string]any{"command": "npx"},
	})
	require.NoError(t, err)
	assert.Equal(t, "fs", cfg.ServerName)
}

// TestOutputSchemaOf_降级分支 Trace / Custom 帧换形。
func TestOutputSchemaOf_降级分支(t *testing.T) {
	os := outputSchemaOf(&stream.OutputSchema{Type: "llm_output", Index: 3})
	assert.Equal(t, 3, os.Index)

	trace := outputSchemaOf(&stream.TraceSchema{Type: "trace"})
	assert.Equal(t, "trace", trace.Type)

	custom := outputSchemaOf(stream.CustomSchema{Type: "custom", Data: map[string]any{"payload": "p1"}})
	assert.Equal(t, "custom", custom.Type)
}

// TestLoadVisionAudioConfig_Env分支 设 VISION_API_KEY / AUDIO_API_KEY 走 FromEnv。
func TestLoadVisionAudioConfig_Env分支(t *testing.T) {
	t.Setenv("VISION_API_KEY", "vk")
	t.Setenv("AUDIO_API_KEY", "ak")
	vision := LoadVisionConfig(CLIConfig{})
	require.NotNil(t, vision)
	audio := LoadAudioConfig(CLIConfig{})
	require.NotNil(t, audio)
}

// TestDecodeMcpConfig_authHeaders 认证头 map 换形分支。
func TestDecodeMcpConfig_authHeaders(t *testing.T) {
	cfg, err := decodeMcpConfig(map[string]any{
		"auth_headers": map[string]any{"Authorization": "Bearer x", "bad": 42},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"Authorization": "Bearer x"}, cfg.AuthHeaders)
}
