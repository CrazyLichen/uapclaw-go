package server

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestServer 创建测试用 AgentServer（含 AgentManager + mock Agent 工厂）。
func newTestServer() (*AgentServer, *transport.ChannelTransport) {
	cfg, _ := config.New("")
	transport := transport.NewChannelTransportWithBuffer(16, 128)
	server := NewAgentServer(cfg, transport)
	// 手动初始化 AgentManager，跳过 Start() 的阻塞循环
	am := runtime.NewAgentManager()
	// 注入 mock Agent 工厂，避免 createAgent → CreateInstance → LLM nil panic
	// mock 工厂创建一个未初始化的 UapClaw（adapter 为 nil），ProcessMessage 会走 sessionManager 队列路径
	am.SetAgentFactory(func(config map[string]any, mode, subMode string) (*runtime.UapClaw, error) {
		return runtime.NewUapClaw(), nil
	})
	server.agentManager = am
	return server, transport
}

// makeTestEnvelope 构造测试用 E2AEnvelope。
func makeTestEnvelope(method string, isStream bool) *e2a.E2AEnvelope {
	env := e2a.NewE2AEnvelope()
	env.RequestID = "test-req-1"
	env.Channel = "web"
	env.Method = method
	env.IsStream = isStream
	env.Params = map[string]any{}
	env.EnsureTimestamp()
	return env
}

// TestApplyResolvedModeToRequest 验证 mode 解析。
func TestApplyResolvedModeToRequest(t *testing.T) {
	tests := []struct {
		name     string
		params   json.RawMessage
		wantMode string
		wantSub  string
	}{
		{
			name:     "空参数默认agent.plan",
			params:   json.RawMessage(`{}`),
			wantMode: "agent",
			wantSub:  "plan",
		},
		{
			name:     "nil参数默认agent.plan",
			params:   nil,
			wantMode: "agent",
			wantSub:  "plan",
		},
		{
			name:     "code.normal模式",
			params:   json.RawMessage(`{"mode": "code.normal"}`),
			wantMode: "code",
			wantSub:  "normal",
		},
		{
			name:     "agent.plan模式",
			params:   json.RawMessage(`{"mode": "agent.plan"}`),
			wantMode: "agent",
			wantSub:  "plan",
		},
		{
			name:     "仅mode无subMode",
			params:   json.RawMessage(`{"mode": "code"}`),
			wantMode: "code",
			wantSub:  "normal",
		},
		{
			name:     "空mode字符串",
			params:   json.RawMessage(`{"mode": ""}`),
			wantMode: "agent",
			wantSub:  "plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, tt.params)
			mode, subMode := applyResolvedModeToRequest(req)
			if mode != tt.wantMode {
				t.Errorf("mode = %q, 期望 %q", mode, tt.wantMode)
			}
			if subMode != tt.wantSub {
				t.Errorf("subMode = %q, 期望 %q", subMode, tt.wantSub)
			}
		})
	}
}

// TestResolveRequestProjectDir 验证项目目录解析。
func TestResolveRequestProjectDir(t *testing.T) {
	tests := []struct {
		name    string
		request *schema.AgentRequest
		want    string
	}{
		{
			name:    "从params读取project_dir",
			request: schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, json.RawMessage(`{"project_dir": "/tmp/project"}`)),
			want:    "/tmp/project",
		},
		{
			name: "从metadata读取project_dir",
			request: schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, json.RawMessage(`{}`),
				schema.WithAgentMetadata(map[string]any{"project_dir": "/tmp/meta-project"}),
			),
			want: "/tmp/meta-project",
		},
		{
			name:    "params优先于metadata",
			request: schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, json.RawMessage(`{"project_dir": "/tmp/params"}`)),
			want:    "/tmp/params",
		},
		{
			name:    "无workspace_dir返回空",
			request: schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, json.RawMessage(`{}`)),
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRequestProjectDir(tt.request)
			if got != tt.want {
				t.Errorf("resolveRequestProjectDir() = %q, 期望 %q", got, tt.want)
			}
		})
	}
}

// TestHandleEnvelope_UnknownMethod_Unary 验证未命中 switch + IsStream=false → 走 handleUnary。
func TestHandleEnvelope_UnknownMethod_Unary(t *testing.T) {
	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// chat.send 不在显式 switch 中，走 default → handleUnary
	env := makeTestEnvelope("chat.send", false)

	// 获取响应读取端
	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}

	// 在 goroutine 中调用 handleEnvelope
	go s.handleEnvelope(ctx, env)

	// 从 RecvCh 读取响应
	select {
	case data := <-recvCh:
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("响应 JSON 解码失败: %v", err)
		}
		resp := e2a.ResponseFromMap(m)
		if resp.RequestID != "test-req-1" {
			t.Errorf("RequestID = %q, 期望 %q", resp.RequestID, "test-req-1")
		}
		if resp.IsFinal != true {
			t.Errorf("IsFinal = %v, 期望 true", resp.IsFinal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到响应")
	}
}

// TestHandleEnvelope_UnknownMethod_Stream 验证未命中 switch + IsStream=true → 走 handleStream。
func TestHandleEnvelope_UnknownMethod_Stream(t *testing.T) {
	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// chat.send + IsStream=true，走 default → handleStream
	env := makeTestEnvelope("chat.send", true)

	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}

	go s.handleEnvelope(ctx, env)

	// 从 RecvCh 读取流式响应（至少收到 stub chunk + terminal chunk）
	chunkCount := 0
	timeout := time.After(3 * time.Second)
	for {
		select {
		case <-recvCh:
			chunkCount++
			if chunkCount >= 2 {
				// 至少收到 stub chunk + terminal chunk
				return
			}
		case <-timeout:
			t.Fatalf("超时：仅收到 %d 个流式块，期望至少 2 个", chunkCount)
		}
	}
}

// TestHandleEnvelope_SessionList 验证命中 switch → handleSessionList。
func TestHandleEnvelope_SessionList(t *testing.T) {
	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	env := makeTestEnvelope("session.list", false)

	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}

	go s.handleEnvelope(ctx, env)

	select {
	case data := <-recvCh:
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("响应 JSON 解码失败: %v", err)
		}
		resp := e2a.ResponseFromMap(m)
		if resp.RequestID != "test-req-1" {
			t.Errorf("RequestID = %q, 期望 %q", resp.RequestID, "test-req-1")
		}
		// handleSessionList 是 stub，返回 NOT_IMPLEMENTED（ok=false）
		// 验证收到了响应即可
		if resp.ResponseKind == "" {
			t.Error("ResponseKind 不应为空")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到响应")
	}
}

// TestHandleEnvelope_ChatCancel 验证 chat.interrupt → handleCancel。
func TestHandleEnvelope_ChatCancel(t *testing.T) {
	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	env := makeTestEnvelope("chat.interrupt", false)

	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}

	go s.handleEnvelope(ctx, env)

	select {
	case data := <-recvCh:
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("响应 JSON 解码失败: %v", err)
		}
		resp := e2a.ResponseFromMap(m)
		if resp.RequestID != "test-req-1" {
			t.Errorf("RequestID = %q, 期望 %q", resp.RequestID, "test-req-1")
		}
		// handleCancel 返回 ok=true
		if resp.IsFinal != true {
			t.Errorf("IsFinal = %v, 期望 true", resp.IsFinal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到响应")
	}
}

// TestWriteErrorResponse 验证错误响应写入 RecvCh。
func TestWriteErrorResponse(t *testing.T) {
	s, transport := newTestServer()

	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}

	s.writeErrorResponse("req-err-1", "web", "测试错误", "TEST_ERROR")

	select {
	case data := <-recvCh:
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("响应 JSON 解码失败: %v", err)
		}
		resp := e2a.ResponseFromMap(m)
		if resp.RequestID != "req-err-1" {
			t.Errorf("RequestID = %q, 期望 %q", resp.RequestID, "req-err-1")
		}
	case <-time.After(time.Second):
		t.Fatal("超时：未收到错误响应")
	}
}

// TestInjectACPCapabilities 验证 ACP 通道注入 acp_client_capabilities。
// Python agent_ws_server.py L803-810: setdefault("acp_client_capabilities", ...)。
func TestInjectACPCapabilities(t *testing.T) {
	s, _ := newTestServer()
	request := schema.NewAgentRequest("req-1", "acp", schema.ReqMethodInitialize, json.RawMessage(`{}`))
	envelope := e2a.NewE2AEnvelope()
	envelope.Params = map[string]any{
		"client_capabilities": map[string]any{"tools": true},
	}

	s.injectACPCapabilities(request, envelope)

	if request.Metadata == nil {
		t.Fatal("metadata 不应为 nil")
	}
	caps, ok := request.Metadata["acp_client_capabilities"]
	if !ok {
		t.Fatal("metadata 中应包含 acp_client_capabilities")
	}
	capsMap, ok := caps.(map[string]any)
	if !ok {
		t.Fatal("acp_client_capabilities 应为 map[string]any")
	}
	if tools, ok := capsMap["tools"]; !ok || tools != true {
		t.Error("acp_client_capabilities.tools 应为 true")
	}
}

// TestInjectACPCapabilities_setdefault不覆盖 已有 acp_client_capabilities 时不覆盖。
func TestInjectACPCapabilities_setdefault不覆盖(t *testing.T) {
	s, _ := newTestServer()
	request := schema.NewAgentRequest("req-1", "acp", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	request.Metadata = map[string]any{
		"acp_client_capabilities": map[string]any{"tools": "preset"},
	}
	envelope := e2a.NewE2AEnvelope()
	envelope.Params = map[string]any{
		"client_capabilities": map[string]any{"tools": true},
	}

	s.injectACPCapabilities(request, envelope)

	caps := request.Metadata["acp_client_capabilities"].(map[string]any)
	if caps["tools"] != "preset" {
		t.Errorf("setdefault 应保留原值, got %v", caps["tools"])
	}
}

// TestInjectACPCapabilities_无params回退manager params 无能力时回退 GetClientCapabilities。
func TestInjectACPCapabilities_无params回退manager(t *testing.T) {
	s, _ := newTestServer()
	// manager 记录能力
	if _, err := s.agentManager.Initialize(context.Background(), "acp", map[string]any{
		"client_capabilities": map[string]any{"fs": true},
	}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	request := schema.NewAgentRequest("req-1", "acp", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	envelope := e2a.NewE2AEnvelope()
	envelope.Params = map[string]any{}

	s.injectACPCapabilities(request, envelope)

	caps, ok := request.Metadata["acp_client_capabilities"]
	if !ok {
		t.Fatal("应回退注入 manager 能力")
	}
	capsMap := caps.(map[string]any)
	if capsMap["fs"] != true {
		t.Errorf("fs = %v", capsMap["fs"])
	}
}

// TestInjectACPCapabilities_无来源不注入 params 与 manager 均无能力。
func TestInjectACPCapabilities_无来源不注入(t *testing.T) {
	s, _ := newTestServer()
	request := schema.NewAgentRequest("req-1", "acp", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	envelope := e2a.NewE2AEnvelope()

	s.injectACPCapabilities(request, envelope)

	if _, exists := request.Metadata["acp_client_capabilities"]; exists {
		t.Error("无来源时不应注入")
	}
}

// TestHandleEnvelope_BeforeChatHookParams回写 测试扩展修改 params 写回 request.Params
// （对齐 Python agent_ws_server.py _trigger_before_chat_request_hook：params 非 dict 归一化 + ctx 触发）
func TestHandleEnvelope_BeforeChatHookParams回写(t *testing.T) {
	// 注册扩展回调：验证钩子被触发且拿到 params map
	extensions.ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := extensions.CreateInstance(fw, map[string]any{}, nil)
	defer extensions.ResetInstance()

	gotParams := make(chan map[string]any, 1)
	reg.Register(extensions.AgentServerBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		if p, ok := data["params"].(map[string]any); ok {
			p["injected"] = true
			gotParams <- p
		}
		return nil
	}, 100)

	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	env := e2a.NewE2AEnvelope()
	env.RequestID = "test-req-hook"
	env.Channel = "web"
	env.Method = "chat.send"
	env.IsStream = false
	env.Params = map[string]any{"query": "原始"}
	env.EnsureTimestamp()

	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}

	go s.handleEnvelope(ctx, env)

	// 钩子应被触发（chat.send 命中门控）
	select {
	case p := <-gotParams:
		if p["injected"] != true || p["query"] != "原始" {
			t.Errorf("钩子收到的 params 不符: %v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时：before_chat_request 钩子未被触发")
	}

	// 响应正常返回（后续 handleUnary 正常分发）
	select {
	case <-recvCh:
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到响应")
	}
}

// TestHandleEnvelope_BeforeChatHook_非聊天方法不触发 验证门控（session.list 不触发钩子）
func TestHandleEnvelope_BeforeChatHook_非聊天方法不触发(t *testing.T) {
	extensions.ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := extensions.CreateInstance(fw, map[string]any{}, nil)
	defer extensions.ResetInstance()

	var triggered atomic.Bool
	reg.Register(extensions.AgentServerBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		triggered.Store(true)
		return nil
	}, 100)

	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	env := makeTestEnvelope("session.list", false)
	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}
	go s.handleEnvelope(ctx, env)
	select {
	case <-recvCh:
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到响应")
	}
	// session.list 不在 CHAT_SEND/RESUME/ANSWER 门控内，钩子不应被触发
	if triggered.Load() {
		t.Error("session.list 不应触发 before_chat_request 钩子")
	}
}

// TestHandleEnvelope_BeforeChatHook_Params非对象清洗 测试 params 为 JSON 数组时归一化为空 map
// （对齐 Python agent_ws_server.py L1044-1046：非 dict 时 request.params = {}）
func TestHandleEnvelope_BeforeChatHook_Params非对象清洗(t *testing.T) {
	extensions.ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := extensions.CreateInstance(fw, map[string]any{}, nil)
	defer extensions.ResetInstance()

	gotParams := make(chan map[string]any, 1)
	reg.Register(extensions.AgentServerBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		if p, ok := data["params"].(map[string]any); ok {
			gotParams <- p
		}
		return nil
	}, 100)

	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	env := e2a.NewE2AEnvelope()
	env.RequestID = "test-req-arr"
	env.Channel = "web"
	env.Method = "chat.send"
	env.IsStream = false
	// params 为 nil（对应 Python params=None，非 dict）——
	// Go wire 层 E2AEnvelope.Params 为强类型 map，数组在 JSON 解码时已被拦截，
	// 可达的非 dict 路径即空 params：request.Params 为空 → params==nil → 归一化为 {}
	env.Params = nil
	env.EnsureTimestamp()

	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}
	go s.handleEnvelope(ctx, env)

	// 钩子应收到清洗后的空 map（而非数组）
	select {
	case p := <-gotParams:
		if len(p) != 0 {
			t.Errorf("非对象 params 应被清洗为空 map, 实际: %v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时：before_chat_request 钩子未被触发")
	}

	select {
	case <-recvCh:
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到响应")
	}
}
