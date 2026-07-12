package adapter

import (
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewCodeAdapter 测试 CodeAdapter 构造函数。
func TestNewCodeAdapter(t *testing.T) {
	c := NewCodeAdapter()

	// deep 非空
	if c.deep == nil {
		t.Error("deep 不应为 nil")
	}

	// isCodeAgent = true
	if !c.deep.isCodeAgent {
		t.Error("isCodeAgent 应为 true")
	}

	// forceEnglishRuntimePrompt = true
	if !c.forceEnglishRuntimePrompt {
		t.Error("forceEnglishRuntimePrompt 应为 true")
	}

	// agentName 继承 DeepAdapter 默认值
	if c.deep.agentName != "main_agent" {
		t.Errorf("agentName = %q, want %q", c.deep.agentName, "main_agent")
	}
}

// TestCodeAdapter_接口满足性 编译期检查 CodeAdapter 实现 AgentAdapter 接口。
func TestCodeAdapter_接口满足性(t *testing.T) {
	var _ AgentAdapter = (*CodeAdapter)(nil)
}

// TestCodeAdapter_CreateInstance_dreamingMode 测试 CodeAdapter 固定 dreaming_mode="code"。
func TestCodeAdapter_CreateInstance_dreamingMode(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()

	err := c.CreateInstance(ctx, nil, "code", "")
	if err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}
	if c.deep.dreamingMode != "code" {
		t.Errorf("dreamingMode = %q, want %q", c.deep.dreamingMode, "code")
	}
	if c.deep.isCodeAgent != true {
		t.Errorf("isCodeAgent = %v, want true", c.deep.isCodeAgent)
	}
}

// TestCodeAdapter_CreateInstance_mode存储 测试 mode/subMode 存储。
func TestCodeAdapter_CreateInstance_mode存储(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()

	err := c.CreateInstance(ctx, nil, "code.plan", "plan")
	if err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}
	if c.deep.mode != "code.plan" {
		t.Errorf("mode = %q, want %q", c.deep.mode, "code.plan")
	}
	if c.deep.subMode != "plan" {
		t.Errorf("subMode = %q, want %q", c.deep.subMode, "plan")
	}
}

// TestCodeAdapter_Cleanup 测试 Cleanup 委托。
func TestCodeAdapter_Cleanup(t *testing.T) {
	c := NewCodeAdapter()
	if err := c.Cleanup(); err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}
}

// TestCodeAdapter_ProcessInterrupt_委托 测试 ProcessInterrupt 委托 DeepAdapter。
func TestCodeAdapter_ProcessInterrupt_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	params, _ := json.Marshal(map[string]any{"intent": "cancel"})
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), params)
	resp, err := c.ProcessInterrupt(ctx, req)
	if err != nil {
		t.Errorf("ProcessInterrupt error: %v", err)
	}
	if resp == nil {
		t.Error("ProcessInterrupt 返回 nil 响应")
	}
}

// TestCodeAdapter_HandleUserAnswer_委托 测试 HandleUserAnswer 委托。
func TestCodeAdapter_HandleUserAnswer_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	params, _ := json.Marshal(map[string]any{"request_id": "test_123", "answers": []any{}})
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.user_answer"), params)
	resp, err := c.HandleUserAnswer(ctx, req)
	if err != nil {
		t.Errorf("HandleUserAnswer error: %v", err)
	}
	if resp == nil {
		t.Error("HandleUserAnswer 返回 nil 响应")
	}
}

// TestCodeAdapter_HandleHeartbeat_委托 测试 HandleHeartbeat 委托。
func TestCodeAdapter_HandleHeartbeat_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	sid := "normal_session"
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil,
		schema.WithAgentSessionID(sid),
	)
	resp, err := c.HandleHeartbeat(ctx, req)
	if err != nil {
		t.Errorf("HandleHeartbeat error: %v", err)
	}
	if resp != nil {
		t.Errorf("非 heartbeat 应返回 nil，got %v", resp)
	}
}

// TestCodeAdapter_ProcessMessageImpl_未初始化 测试未初始化时返回错误。
func TestCodeAdapter_ProcessMessageImpl_未初始化(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil)
	_, err := c.ProcessMessageImpl(ctx, req, nil)
	if err == nil {
		t.Error("instance=nil 时应返回错误")
	}
}

// TestCodeAdapter_ProcessMessageStreamImpl_未初始化 测试未初始化时返回错误。
func TestCodeAdapter_ProcessMessageStreamImpl_未初始化(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil)
	_, err := c.ProcessMessageStreamImpl(ctx, req, nil)
	if err == nil {
		t.Error("instance=nil 时应返回错误")
	}
}

// TestCodeAdapter_ReloadAgentConfig_未初始化 测试未初始化时返回错误。
func TestCodeAdapter_ReloadAgentConfig_未初始化(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	err := c.ReloadAgentConfig(ctx, nil, nil)
	if err == nil {
		t.Error("instance=nil 时应返回错误")
	}
}

// TestCodeAdapter_CompressContext_委托 测试 CompressContext 委托。
func TestCodeAdapter_CompressContext_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	result, err := c.CompressContext(ctx, "s1", nil, false)
	if result != nil || err != nil {
		t.Errorf("CompressContext 占位实现应返回 nil, nil")
	}
}

// TestCodeAdapter_GetContextUsage_委托 测试 GetContextUsage 委托。
func TestCodeAdapter_GetContextUsage_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	result, err := c.GetContextUsage(ctx, "s1")
	if result != nil || err != nil {
		t.Errorf("GetContextUsage 无实例应返回 nil, nil")
	}
}

// TestCodeAdapter_GenerateRecap_委托 测试 GenerateRecap 委托。
func TestCodeAdapter_GenerateRecap_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	result, err := c.GenerateRecap(ctx, "s1")
	if result != nil || err != nil {
		t.Errorf("GenerateRecap 占位实现应返回 nil, nil")
	}
}

// TestCodeAdapter_TryStartDreaming_委托 测试 TryStartDreaming 委托。
func TestCodeAdapter_TryStartDreaming_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	// dreamingMode 未设置（空），应跳过
	if err := c.TryStartDreaming(ctx, nil); err != nil {
		t.Errorf("TryStartDreaming error = %v", err)
	}
}

// TestCodeAdapter_TryStopDreaming_委托 测试 TryStopDreaming 委托。
func TestCodeAdapter_TryStopDreaming_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	if err := c.TryStopDreaming(ctx); err != nil {
		t.Errorf("TryStopDreaming error = %v", err)
	}
}

// TestCodeAdapter_AbortOnGatewayDisconnect_委托 测试 AbortOnGatewayDisconnect 委托。
func TestCodeAdapter_AbortOnGatewayDisconnect_委托(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	// 不应 panic
	c.AbortOnGatewayDisconnect(ctx)
}
