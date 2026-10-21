package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeAdapter AgentAdapter mock，用于 UapClaw 测试。
type fakeAdapter struct {
	mu              sync.Mutex
	createErr       error
	processResp     *schema.AgentResponse
	processErr      error
	streamCh        <-chan *schema.AgentResponseChunk
	streamErr       error
	interruptResp   *schema.AgentResponse
	interruptErr    error
	heartbeatResp   *schema.AgentResponse
	heartbeatErr    error
	userAnswerResp  *schema.AgentResponse
	userAnswerErr   error
	instanceCreated bool
	// ContextCompressor mock 返回值
	compressResult  map[string]any
	compressErr     error
	contextUsage    map[string]any
	contextUsageErr error
	recapResult     map[string]any
	recapErr        error
}

func newFakeAdapter() *fakeAdapter {
	return &fakeAdapter{
		processResp:    schema.NewAgentResponse("fake", "fake", schema.WithResponseOK(true), schema.WithPayload(map[string]any{"content": "mock response"})),
		interruptResp:  schema.NewAgentResponse("fake", "fake", schema.WithResponseOK(true)),
		compressResult: map[string]any{"ok": true, "compressed": false},
		contextUsage:   map[string]any{"usage": 0, "limit": 0},
		recapResult:    map[string]any{"status": "ok", "summary": "mock recap"},
	}
}

func (f *fakeAdapter) CreateInstance(_ context.Context, _ map[string]any, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instanceCreated = true
	return f.createErr
}
func (f *fakeAdapter) ReloadAgentConfig(_ context.Context, _, _ map[string]any) error { return nil }
func (f *fakeAdapter) ProcessMessageImpl(_ context.Context, _ *schema.AgentRequest, _ map[string]any) (*schema.AgentResponse, error) {
	return f.processResp, f.processErr
}
func (f *fakeAdapter) ProcessMessageStreamImpl(_ context.Context, _ *schema.AgentRequest, _ map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	return f.streamCh, f.streamErr
}
func (f *fakeAdapter) ProcessInterrupt(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.interruptResp, f.interruptErr
}
func (f *fakeAdapter) HandleUserAnswer(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.userAnswerResp, f.userAnswerErr
}
func (f *fakeAdapter) HandleHeartbeat(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.heartbeatResp, f.heartbeatErr
}
func (f *fakeAdapter) Cleanup() error                                  { return nil }
func (f *fakeAdapter) SwitchMode(_ context.Context, _, _ string) error { return nil }
func (f *fakeAdapter) Instance() *harness.DeepAgent                    { return nil }

// ContextCompressor 接口实现
func (f *fakeAdapter) CompressContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ bool) (map[string]any, error) {
	return f.compressResult, f.compressErr
}
func (f *fakeAdapter) GetContextUsage(_ context.Context, _ string) (map[string]any, error) {
	return f.contextUsage, f.contextUsageErr
}
func (f *fakeAdapter) GenerateRecap(_ context.Context, _ string) (map[string]any, error) {
	return f.recapResult, f.recapErr
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewUapClaw(t *testing.T) {
	uc := NewUapClaw()
	require.NotNil(t, uc)
	require.NotNil(t, uc.sessionManager)
}

func TestUapClaw_ensureAdapter_幂等(t *testing.T) {
	uc := NewUapClaw()
	a1, err := uc.ensureAdapter("agent")
	require.NoError(t, err)
	require.NotNil(t, a1)
	a2, err := uc.ensureAdapter("agent")
	require.NoError(t, err)
	assert.Equal(t, a1, a2, "ensureAdapter 应幂等返回同一 adapter")
}

func TestUapClaw_ProcessMessage_cancel分支(t *testing.T) {
	uc := NewUapClaw()
	// 注入 fakeAdapter
	fa := newFakeAdapter()
	uc.adapter = fa

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatCancel, nil)
	resp, err := uc.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
}

func TestUapClaw_ProcessMessage_heartbeat分支(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	fa.heartbeatResp = schema.NewAgentResponse("req-1", "web", schema.WithResponseOK(true), schema.WithPayload(map[string]any{"event_type": "heartbeat"}))
	uc.adapter = fa

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, nil)
	resp, err := uc.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
	// 应短路返回 heartbeat 响应
	assert.Equal(t, "heartbeat", resp.Payload["event_type"])
}

func TestUapClaw_ProcessMessage_常规对话(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa

	params := map[string]any{"query": "你好"}
	paramsJSON, _ := json.Marshal(params)
	sessID := "test-sess"
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, paramsJSON,
		schema.WithAgentSessionID(sessID))

	resp, err := uc.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
}

func TestUapClaw_ProcessMessageStream_基本流程(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()

	// 构建流式 chunk channel
	chunkCh := make(chan *schema.AgentResponseChunk, 4)
	chunkCh <- schema.NewAgentResponseChunk("req-2", "web", map[string]any{
		"event_type": "chat.delta",
		"content":    "你好",
	})
	chunkCh <- schema.NewAgentResponseChunk("req-2", "web", map[string]any{
		"event_type": "chat.final",
		"content":    "完整回复",
	})
	close(chunkCh)

	fa.streamCh = chunkCh
	uc.adapter = fa

	params := map[string]any{"query": "你好"}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-2", "web", schema.ReqMethodChatSend, paramsJSON,
		schema.WithAgentIsStream(true),
		schema.WithAgentSessionID("stream-sess"))

	resultCh, err := uc.ProcessMessageStream(context.Background(), req)
	require.NoError(t, err)

	// 消费所有 chunk
	chunkCount := 0
	var gotTerminal bool
	for chunk := range resultCh {
		chunkCount++
		if chunk.IsTerminal() {
			gotTerminal = true
		}
	}
	assert.True(t, gotTerminal, "流应以终止哨兵结束")
	assert.GreaterOrEqual(t, chunkCount, 2, "至少应有 chat.delta + terminal")
}

func TestUapClaw_ProcessInterrupt_intent分支(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa

	tests := []struct {
		name   string
		intent string
	}{
		{"pause", "pause"},
		{"resume", "resume"},
		{"supplement", "supplement"},
		{"cancel", "cancel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params, _ := json.Marshal(map[string]any{"intent": tc.intent})
			req := schema.NewAgentRequest("req", "web", schema.ReqMethodChatCancel, params)
			resp, err := uc.ProcessInterrupt(context.Background(), req)
			require.NoError(t, err)
			assert.True(t, resp.OK)
		})
	}
}

func TestUapClaw_Cleanup(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa
	err := uc.Cleanup()
	require.NoError(t, err)
	assert.Nil(t, uc.adapter)
}

func TestUapClaw_CancelInflightWork(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa
	err := uc.CancelInflightWork("[gateway ws disconnect] ")
	require.NoError(t, err)
}

func TestUapClaw_GetContextUsage(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa
	result, err := uc.GetContextUsage(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, 0, result["usage"])
}

func TestUapClaw_CompressContext(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa
	result, err := uc.CompressContext(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
}

func TestUapClaw_GenerateRecap(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa
	result, err := uc.GenerateRecap(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
}

func TestUapClaw_SwitchMode(t *testing.T) {
	uc := NewUapClaw()
	err := uc.SwitchMode(context.Background(), "sess-1", "code.normal")
	assert.NoError(t, err)
}

func TestUapClaw_CreateInstance(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa

	err := uc.CreateInstance(
		WithCreateInstanceConfig(map[string]any{}),
		WithCreateInstanceMode("agent"),
	)
	require.NoError(t, err)

	fa.mu.Lock()
	created := fa.instanceCreated
	fa.mu.Unlock()
	assert.True(t, created, "CreateInstance 应委托给 adapter")
}

func TestUapClaw_CreateInstance_错误(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	fa.createErr = fmt.Errorf("mock error")
	uc.adapter = fa

	err := uc.CreateInstance(
		WithCreateInstanceConfig(map[string]any{}),
		WithCreateInstanceMode("agent"),
	)
	assert.Error(t, err)
}

func TestUapClaw_ReloadAgentConfig(t *testing.T) {
	uc := NewUapClaw()
	fa := newFakeAdapter()
	uc.adapter = fa

	err := uc.ReloadAgentConfig(map[string]any{}, nil)
	require.NoError(t, err)
}

func TestUapClaw_ReloadAgentConfig_无Adapter(t *testing.T) {
	uc := NewUapClaw()
	// adapter 为 nil 时不报错
	err := uc.ReloadAgentConfig(map[string]any{}, nil)
	require.NoError(t, err)
}

func TestUapClaw_GetInstance(t *testing.T) {
	uc := NewUapClaw()
	// 无 adapter 时返回 nil
	assert.Nil(t, uc.GetInstance())

	// 有 fakeAdapter 时返回 nil（fakeAdapter.Instance() 返回 nil）
	fa := newFakeAdapter()
	uc.adapter = fa
	assert.Nil(t, uc.GetInstance())
}

func TestUapClaw_GetInstance_未初始化(t *testing.T) {
	uc := &UapClaw{}
	if inst := uc.GetInstance(); inst != nil {
		t.Error("未初始化 UapClaw GetInstance() 应返回 nil")
	}
}

func TestUapClaw_GetInstance_nil适配器(t *testing.T) {
	uc := &UapClaw{adapter: nil}
	if inst := uc.GetInstance(); inst != nil {
		t.Error("adapter 为 nil 时 GetInstance() 应返回 nil")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestNormalizeSessionID(t *testing.T) {
	assert.Equal(t, "default", session.NormalizeSessionID(""))
	assert.Equal(t, "my-session", session.NormalizeSessionID("my-session"))
}

func TestShouldRecordHistory(t *testing.T) {
	assert.True(t, shouldRecordHistory("chat.delta"))
	assert.True(t, shouldRecordHistory("chat.final"))
	assert.True(t, shouldRecordHistory("chat.tool_result"))
	assert.False(t, shouldRecordHistory("heartbeat"))
	assert.False(t, shouldRecordHistory("system"))
}

func TestExtractChunkContent(t *testing.T) {
	assert.Equal(t, "hello", extractChunkContent(map[string]any{"content": "hello"}))
	assert.Equal(t, "", extractChunkContent(map[string]any{}))
	assert.Equal(t, "", extractChunkContent(nil))
}

func TestParseRequestParams(t *testing.T) {
	// nil 参数
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, nil)
	params := parseRequestParams(req)
	assert.Empty(t, params)

	// 有效 JSON 参数
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, json.RawMessage(`{"query":"hi","mode":"code"}`))
	params = parseRequestParams(req)
	assert.Equal(t, "hi", params["query"])
	assert.Equal(t, "code", params["mode"])
}

func TestExtractChannelFromSessionID(t *testing.T) {
	// 有 sessionID，包含 _
	sessID := "web_my-session"
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, nil, schema.WithAgentSessionID(sessID))
	assert.Equal(t, "web", extractChannelFromSessionID(req))

	// 无 sessionID
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, nil)
	assert.Equal(t, "web", extractChannelFromSessionID(req))
}

func TestExtractStringWithFallback(t *testing.T) {
	// params 优先
	params := map[string]any{"project_dir": "/from/params"}
	metadata := map[string]any{"project_dir": "/from/metadata"}
	assert.Equal(t, "/from/params", extractStringWithFallback(params, "project_dir", metadata, "project_dir"))

	// metadata 兜底
	params = map[string]any{}
	assert.Equal(t, "/from/metadata", extractStringWithFallback(params, "project_dir", metadata, "project_dir"))

	// 都没有
	assert.Equal(t, "", extractStringWithFallback(params, "project_dir", nil, "project_dir"))
}

func TestAdapterModeForRequest(t *testing.T) {
	uc := NewUapClaw()
	// 有 mode 参数
	params := map[string]any{"mode": "code.normal"}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, paramsJSON)
	assert.Equal(t, "code", uc.adapterModeForRequest(req))

	// 无 mode 参数，默认 agent
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, nil)
	assert.Equal(t, "agent", uc.adapterModeForRequest(req))

	// mode 为空串
	params = map[string]any{"mode": ""}
	paramsJSON, _ = json.Marshal(params)
	req = schema.NewAgentRequest("r3", "web", schema.ReqMethodChatSend, paramsJSON)
	assert.Equal(t, "agent", uc.adapterModeForRequest(req))
}

func TestExtractResponseContent(t *testing.T) {
	uc := NewUapClaw()
	// 有 content
	resp := schema.NewAgentResponse("r1", "web", schema.WithPayload(map[string]any{"content": "hello"}))
	assert.Equal(t, "hello", uc.extractResponseContent(resp))

	// 无 payload
	resp = schema.NewAgentResponse("r2", "web")
	assert.Equal(t, "", uc.extractResponseContent(resp))

	// payload 无 content 键
	resp = schema.NewAgentResponse("r3", "web", schema.WithPayload(map[string]any{"other": "val"}))
	assert.Equal(t, "", uc.extractResponseContent(resp))
}

func TestExtractIntent(t *testing.T) {
	uc := NewUapClaw()
	// 有 intent
	params := map[string]any{"intent": "pause"}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatCancel, paramsJSON)
	assert.Equal(t, "pause", uc.extractIntent(req))

	// 无 intent，默认 cancel
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatCancel, nil)
	assert.Equal(t, "cancel", uc.extractIntent(req))
}

func TestExtractQuery(t *testing.T) {
	uc := NewUapClaw()
	// 有 query
	params := map[string]any{"query": "你好"}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, paramsJSON)
	assert.Equal(t, "你好", uc.extractQuery(req))

	// 无 query
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, nil)
	assert.Equal(t, "", uc.extractQuery(req))
}

func TestExtractSessionID(t *testing.T) {
	uc := NewUapClaw()
	// 有 sessionID
	sessID := "my-session"
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, nil, schema.WithAgentSessionID(sessID))
	assert.Equal(t, "my-session", uc.extractSessionID(req))

	// 无 sessionID
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, nil)
	assert.Equal(t, "", uc.extractSessionID(req))
}

// 注意：此测试依赖真实 adapter 创建（DeepAdapter），确保编译通过即可。
// 如需更完整的 ProcessMessage 测试，使用 fakeAdapter 注入。
func TestUapClaw_ensureAdapter_创建真实Adapter(t *testing.T) {
	uc := NewUapClaw()
	a, err := uc.ensureAdapter("agent")
	require.NoError(t, err)
	require.NotNil(t, a)
	// 确保幂等
	a2, _ := uc.ensureAdapter("agent")
	assert.Equal(t, a, a2)
}

// setupCloudMemoryEnv 注入 UAPCLAW_DATA_DIR 指向含 memory_mode: cloud 的临时工作区。
// 返回恢复函数（重置路径缓存 + 还原环境变量）。
func setupCloudMemoryEnv(t *testing.T) func() {
	t.Helper()
	// 先排干包内更早测试遗留的异步写入队列（哨兵确认后 worker 阻塞在接收上
	// 不再读路径缓存），再重置缓存注入环境——避免与遗留 worker 的读竞争
	session.FlushMetadataQueue()
	session.FlushHistoryQueue()

	// 创建临时工作区：<tmp>/.uapclaw 结构（WorkspaceDir 即 UAPCLAW_DATA_DIR 本身）
	// ConfigDir = WorkspaceDir()/config，需存在 config/config.yaml 才走"已初始化"分支
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("创建临时 config 目录失败: %v", err)
	}
	configYAML := "memory_mode: cloud\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatalf("写入临时 config.yaml 失败: %v", err)
	}

	origDataDir := os.Getenv(pathutil.EnvDataDir)
	origHome := os.Getenv(pathutil.EnvHome)
	if err := os.Setenv(pathutil.EnvDataDir, root); err != nil {
		t.Fatalf("设置 UAPCLAW_DATA_DIR 失败: %v", err)
	}
	if err := os.Setenv(pathutil.EnvHome, root); err != nil {
		t.Fatalf("设置 UAPCLAW_HOME 失败: %v", err)
	}
	pathutil.ResetCache()

	return func() {
		// 先等待异步写入队列刷盘（metadata + history），再重置路径缓存。
		// 哨兵确认后 worker 阻塞在 channel 接收上不再读路径缓存，
		// 与 ResetCache 形成 happens-before，避免 race（paths.go ResetCache 注释要求）。
		session.FlushMetadataQueue()
		session.FlushHistoryQueue()
		session.ResetHistoryWorker()
		_ = os.Unsetenv(pathutil.EnvDataDir)
		_ = os.Unsetenv(pathutil.EnvHome)
		if origDataDir != "" {
			_ = os.Setenv(pathutil.EnvDataDir, origDataDir)
		}
		if origHome != "" {
			_ = os.Setenv(pathutil.EnvHome, origHome)
		}
		pathutil.ResetCache()
	}
}

// TestUapClaw_ProcessMessage_AfterChat钩子双条件 表驱动验证非流式云端记忆对话后钩子门控。
// 对齐 Python interface.py:852-878：result.ok and payload.get("content") 双条件——
// 响应无内容（payload 无 content 键或空串）不触发 MEMORY_AFTER_CHAT。
func TestUapClaw_ProcessMessage_AfterChat钩子双条件(t *testing.T) {
	// 先验证环境注入生效：BuildInputs 应解析出 memory_mode=cloud
	t.Run("环境注入验证", func(t *testing.T) {
		cleanup := setupCloudMemoryEnv(t)
		defer cleanup()
		uc := NewUapClaw()
		params, _ := json.Marshal(map[string]any{"query": "你好"})
		req := schema.NewAgentRequest("req-env", "web", schema.ReqMethodChatSend, params)
		_, memoryMode, _ := uc.BuildInputs(req)
		require.Equal(t, "cloud", memoryMode, "临时 config 应注入 memory_mode=cloud")
	})

	tests := []struct {
		name       string
		ok         bool
		payload    map[string]any
		wantAfter  bool
		wantDescr  string
	}{
		{
			name:      "OK且有内容_触发",
			ok:        true,
			payload:   map[string]any{"content": "助手回复"},
			wantAfter: true,
			wantDescr: "result.ok and payload.get('content') 均满足",
		},
		{
			name:      "OK无content键_不触发",
			ok:        true,
			payload:   map[string]any{"other": "val"},
			wantAfter: false,
			wantDescr: "payload.get('content') 为 None",
		},
		{
			name:      "OK空串content_不触发",
			ok:        true,
			payload:   map[string]any{"content": ""},
			wantAfter: false,
			wantDescr: "空串 content 为 Python 假值",
		},
		{
			name:      "失败响应_不触发",
			ok:        false,
			payload:   map[string]any{"content": "错误文本"},
			wantAfter: false,
			wantDescr: "result.ok 为 False",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupCloudMemoryEnv(t)
			defer cleanup()

			extensions.ResetInstance()
			fw := callback.NewCallbackFramework()
			reg, err := extensions.CreateInstance(fw, map[string]any{}, nil)
			require.NoError(t, err)
			defer extensions.ResetInstance()

			var afterTriggered atomic.Bool
			var beforeTriggered atomic.Bool
			reg.Register(extensions.AgentServerMemoryAfterChat, func(ctx context.Context, data map[string]any) any {
				afterTriggered.Store(true)
				return nil
			}, 100)
			reg.Register(extensions.AgentServerMemoryBeforeChat, func(ctx context.Context, data map[string]any) any {
				beforeTriggered.Store(true)
				return nil
			}, 100)

			uc := NewUapClaw()
			fa := newFakeAdapter()
			fa.processResp = schema.NewAgentResponse("req-1", "web",
				schema.WithResponseOK(tc.ok),
				schema.WithPayload(tc.payload))
			uc.adapter = fa

			params, _ := json.Marshal(map[string]any{"query": "你好"})
			req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, params,
				schema.WithAgentSessionID("cloud-sess"))

			resp, err := uc.ProcessMessage(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			// before 钩子无条件触发（memory_mode=cloud 即触发，对齐 Python L834-845）
			assert.True(t, beforeTriggered.Load(), "before 钩子应触发")
			// after 钩子受双条件门控
			assert.Equal(t, tc.wantAfter, afterTriggered.Load(),
				"after 钩子触发=%v，期望=%v（%s）", afterTriggered.Load(), tc.wantAfter, tc.wantDescr)
		})
	}
}

// TestUapClaw_ProcessMessageStream_AfterChat钩子无内容条件 流式云端记忆对话后钩子无双条件。
// 对齐 Python interface.py:1120-1150：memory_mode=cloud 即触发（final 为空时也触发，
// 此时 assistant_message 为空串）——与非流式 L852 双条件不同，勿"顺手修复"。
func TestUapClaw_ProcessMessageStream_AfterChat钩子无内容条件(t *testing.T) {
	cleanup := setupCloudMemoryEnv(t)
	defer cleanup()

	extensions.ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, err := extensions.CreateInstance(fw, map[string]any{}, nil)
	require.NoError(t, err)
	defer extensions.ResetInstance()

	var afterContent atomic.Value // string
	reg.Register(extensions.AgentServerMemoryAfterChat, func(ctx context.Context, data map[string]any) any {
		if s, ok := data["assistant_message"].(string); ok {
			afterContent.Store(s)
		} else if p, ok := data["assistant_message"].(*string); ok && p != nil {
			afterContent.Store(*p)
		}
		return nil
	}, 100)

	uc := NewUapClaw()
	fa := newFakeAdapter()

	// 流只发 delta 无 final：assistantMessage 回退拼接 delta
	chunkCh := make(chan *schema.AgentResponseChunk, 2)
	chunkCh <- schema.NewAgentResponseChunk("req-s", "web", map[string]any{
		"event_type": "chat.delta",
		"content":    "流式回复",
	})
	close(chunkCh)
	fa.streamCh = chunkCh
	uc.adapter = fa

	params, _ := json.Marshal(map[string]any{"query": "你好"})
	req := schema.NewAgentRequest("req-s", "web", schema.ReqMethodChatSend, params,
		schema.WithAgentIsStream(true),
		schema.WithAgentSessionID("cloud-stream"))

	resultCh, err := uc.ProcessMessageStream(context.Background(), req)
	require.NoError(t, err)
	// 消费完流（含终止哨兵）确保钩子已执行
	for range resultCh {
	}

	// 流式 after 钩子即便无 chat.final 也触发（无内容条件），内容回退 delta 拼接
	require.NotNil(t, afterContent.Load(), "流式 after 钩子应触发（memory_mode=cloud 即触发，无内容条件）")
	assert.Equal(t, "流式回复", afterContent.Load().(string), "assistant_message 应回退拼接 delta")
}
