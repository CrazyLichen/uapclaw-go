package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"gopkg.in/yaml.v3"
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
	if err != nil {
		t.Errorf("CompressContext 无实例应返回 nil error, got %v", err)
	}
	if result == nil || result["result"] != "noop" {
		t.Errorf("CompressContext 无实例应返回 {result: noop}, got %v", result)
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
	if err != nil {
		t.Errorf("GenerateRecap 无实例应返回 nil error, got %v", err)
	}
	if result == nil || result["status"] != "no_turn" {
		t.Errorf("GenerateRecap 无实例应返回 {status: no_turn}, got %v", result)
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

// TestCodeAdapter_UpdateRuntimeConfig_ForceEnglish 测试 CodeAdapter 覆写的 updateRuntimeConfig 包含 SetForceEnglish。
// Python: JiuwenClawCodeAdapter._update_runtime_config() — 完全覆写，包含 set_force_english
func TestCodeAdapter_UpdateRuntimeConfig_ForceEnglish(t *testing.T) {
	c := NewCodeAdapter()
	c.deep.configCache = map[string]any{"preferred_language": "en"}
	ctx := t.Context()

	// 构造 RuntimePromptRail，挂到 deep 上
	rail := commrails.NewRuntimePromptRail("en", "web")
	c.deep.runtimePromptRail = rail

	config := &runtimeConfig{
		CWD:       "/tmp/cwd",
		Mode:      "code",
		SessionID: "acp_s1",
	}
	c.updateRuntimeConfig(ctx, config)

	// 验证 runtime_state.yaml 被写入
	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("runtime_state.yaml 应被写入: %v", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 runtime_state.yaml 失败: %v", err)
	}

	// Channel 应来自 sessionID 前缀
	if v, _ := raw["channel"].(string); v != "acp" {
		t.Errorf("channel = %q, want %q", v, "acp")
	}

	// Mode 应为 "code"（不在 modeDisplayMap 中，直接显示原值）
	if v, _ := raw["mode"].(string); v != "code" {
		t.Errorf("mode = %q, want %q", v, "code")
	}
}

// TestCodeAdapter_UpdateRuntimeConfig_OutputLanguage 测试 CodeAdapter 用 resolveOutputLanguage 写 YAML。
// Python CodeAdapter 差异: _write_runtime_state(language=self._resolve_output_language(), ...)
func TestCodeAdapter_UpdateRuntimeConfig_OutputLanguage(t *testing.T) {
	c := NewCodeAdapter()
	// CodeAdapter.resolveRuntimeLanguage() 默认返回 "en"
	// CodeAdapter.resolveOutputLanguage() 委托 deep.resolveRuntimeLanguage()
	// 当 preferred_language=zh 时: resolveRuntimeLanguage → "en", resolveOutputLanguage → "cn"
	c.deep.configCache = map[string]any{"preferred_language": "zh"}
	ctx := t.Context()

	c.deep.runtimePromptRail = commrails.NewRuntimePromptRail("en", "web")

	config := &runtimeConfig{
		CWD:  "/tmp/cwd",
		Mode: "code",
	}
	c.updateRuntimeConfig(ctx, config)

	// 验证 YAML 中 language 来自 resolveOutputLanguage（"cn"）而非 resolveRuntimeLanguage（"en"）
	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("runtime_state.yaml 应被写入: %v", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 runtime_state.yaml 失败: %v", err)
	}

	if v, _ := raw["language"].(string); v != "cn" {
		t.Errorf("language = %q, want %q（来自 resolveOutputLanguage）", v, "cn")
	}
}

// TestCodeAdapter_UpdateRuntimeConfig_nil配置 测试 CodeAdapter.updateRuntimeConfig 传入 nil 不 panic。
func TestCodeAdapter_UpdateRuntimeConfig_nil配置(t *testing.T) {
	c := NewCodeAdapter()
	ctx := t.Context()
	// 不应 panic
	c.updateRuntimeConfig(ctx, nil)
}
