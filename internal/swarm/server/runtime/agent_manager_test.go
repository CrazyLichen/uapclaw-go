package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// --- NewAgentManager ---

func TestNewAgentManager(t *testing.T) {
	am := NewAgentManager()
	require.NotNil(t, am)
	assert.NotNil(t, am.agents)
	assert.NotNil(t, am.agentCreateParams)
	assert.NotNil(t, am.clientCapabilitiesByChannel)
	assert.NotNil(t, am.latestEnvOverrides)
}

// --- normalize 辅助函数 ---

func TestNormalizeChannelID(t *testing.T) {
	assert.Equal(t, "default", normalizeChannelID(""))
	assert.Equal(t, "default", normalizeChannelID("  "))
	assert.Equal(t, "web", normalizeChannelID("web"))
	assert.Equal(t, "web", normalizeChannelID("  web  "))
}

func TestNormalizeMode(t *testing.T) {
	assert.Equal(t, "agent", normalizeMode(""))
	assert.Equal(t, "agent", normalizeMode("  "))
	assert.Equal(t, "code", normalizeMode("code"))
	assert.Equal(t, "code", normalizeMode("  code  "))
}

func TestNormalizeSubMode(t *testing.T) {
	assert.Equal(t, "", normalizeSubMode(""))
	assert.Equal(t, "", normalizeSubMode("  "))
	assert.Equal(t, "plan", normalizeSubMode("plan"))
	assert.Equal(t, "plan", normalizeSubMode("  plan  "))
}

func TestNormalizeProjectDir(t *testing.T) {
	assert.Equal(t, "", normalizeProjectDir(""))
	assert.Equal(t, "", normalizeProjectDir("  "))

	// 相对路径 → 绝对路径
	abs, err := filepath.Abs("myproject")
	require.NoError(t, err)
	assert.Equal(t, abs, normalizeProjectDir("myproject"))

	// ~ 展开
	home, err := os.UserHomeDir()
	if err == nil {
		assert.Equal(t, home, normalizeProjectDir("~"))
		assert.Equal(t, filepath.Join(home, "project"), normalizeProjectDir("~/project"))
	}
}

func TestMakeAgentCacheKey(t *testing.T) {
	key := makeAgentCacheKey("agent", "plan", "/home/user/proj")
	assert.Equal(t, "agent:plan:/home/user/proj", key)

	key2 := makeAgentCacheKey("code", "", "")
	assert.Contains(t, key2, "code::")

	key3 := makeAgentCacheKey("", "", "")
	assert.Contains(t, key3, "agent::")
}

// --- GetAgent ---

func TestAgentManager_GetAgent(t *testing.T) {
	am := NewAgentManager()
	agent, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)
	assert.NotNil(t, agent)

	// 再次获取应该返回同一个实例
	agent2, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)
	assert.Same(t, agent, agent2)
}

func TestAgentManager_GetAgent_不同mode创建不同实例(t *testing.T) {
	am := NewAgentManager()
	agent1, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)
	assert.NotNil(t, agent1)

	agent2, err := am.GetAgent(context.Background(), "web", "code", "", "")
	require.NoError(t, err)
	assert.NotNil(t, agent2)

	// 不同 mode 应该是不同实例
	assert.NotSame(t, agent1, agent2)
}

// --- GetAgentNoWait ---

func TestAgentManager_GetAgentNoWait(t *testing.T) {
	am := NewAgentManager()

	// 未创建时返回 nil
	agent := am.GetAgentNoWait("web", "agent", "", "")
	assert.Nil(t, agent)

	// 先创建一个
	created, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)

	// 精确查找
	agent = am.GetAgentNoWait("web", "agent", "", "")
	assert.NotNil(t, agent)
	assert.Same(t, created, agent)
}

func TestAgentManager_GetAgentNoWait_字段过滤(t *testing.T) {
	am := NewAgentManager()

	// 创建两个 agent
	agentAgent, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)
	codeAgent, err := am.GetAgent(context.Background(), "web", "code", "", "")
	require.NoError(t, err)

	// 按 mode 过滤
	found := am.GetAgentNoWait("web", "agent", "", "")
	assert.Same(t, agentAgent, found)

	found = am.GetAgentNoWait("web", "code", "", "")
	assert.Same(t, codeAgent, found)
}

func TestAgentManager_GetAgentNoWait_fallback返回第一个(t *testing.T) {
	am := NewAgentManager()

	// 创建一个 agent 模式的 agent
	created, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)

	// 全空参数 → 优先返回 mode="agent"
	found := am.GetAgentNoWait("web", "", "", "")
	assert.Same(t, created, found)
}

func TestAgentManager_GetAgentNoWait_空channel返回nil(t *testing.T) {
	am := NewAgentManager()
	agent := am.GetAgentNoWait("nonexistent", "", "", "")
	assert.Nil(t, agent)
}

// --- Initialize ---

func TestAgentManager_Initialize(t *testing.T) {
	am := NewAgentManager()
	// 非 ACP 通道返回 nil
	caps, err := am.Initialize(context.Background(), "web", nil)
	require.NoError(t, err)
	assert.Nil(t, caps)
}

// --- CreateSession ---

func TestAgentManager_CreateSession(t *testing.T) {
	am := NewAgentManager()
	// 空 sessionID → "default"
	sessionID, err := am.CreateSession("web", "")
	require.NoError(t, err)
	assert.Equal(t, "default", sessionID)
}

func TestAgentManager_CreateSession_指定ID(t *testing.T) {
	am := NewAgentManager()
	sessionID, err := am.CreateSession("web", "my-custom-session")
	require.NoError(t, err)
	assert.Equal(t, "my-custom-session", sessionID)
}

func TestAgentManager_CreateSession_带空格的ID(t *testing.T) {
	am := NewAgentManager()
	sessionID, err := am.CreateSession("web", "  my-session  ")
	require.NoError(t, err)
	assert.Equal(t, "my-session", sessionID)
}

// --- GetClientCapabilities ---

func TestAgentManager_GetClientCapabilities(t *testing.T) {
	am := NewAgentManager()
	// 当前为空
	caps := am.GetClientCapabilities("web")
	assert.NotNil(t, caps)
	assert.Empty(t, caps)
}

func TestAgentManager_GetClientCapabilities_有数据时返回拷贝(t *testing.T) {
	am := NewAgentManager()
	am.mu.Lock()
	am.clientCapabilitiesByChannel["acp"] = map[string]any{"key": "value"}
	am.mu.Unlock()

	caps := am.GetClientCapabilities("acp")
	assert.Equal(t, "value", caps["key"])
}

// --- ProcessMessage ---
// 注意：ProcessMessage/Stream 会调用 UapClaw.ProcessMessage/Stream，
// 而 UapClaw.CreateInstance 会创建真实的 DeepAgent → LLM 调用。
// 因此这里仅验证 GetAgent 委托逻辑（即 GetAgent 能正确解析 mode 并创建 agent），
// 实际 ProcessMessage 的端到端测试由集成测试覆盖。

func TestAgentManager_ProcessMessage_GetAgent委托(t *testing.T) {
	am := NewAgentManager()
	req := &schema.AgentRequest{
		RequestID: "test-req-1",
		ChannelID: "web",
		Params:    []byte(`{"mode": "agent.plan"}`),
	}

	// 验证 GetAgent 能正确解析 mode
	mode, subMode := resolveModeFromRequest(req)
	assert.Equal(t, "agent", mode)
	assert.Equal(t, "plan", subMode)

	agent, err := am.GetAgent(context.Background(), req.ChannelID, mode, "", subMode)
	require.NoError(t, err)
	assert.NotNil(t, agent)
}

// --- ProcessMessageStream ---

func TestAgentManager_ProcessMessageStream_GetAgent委托(t *testing.T) {
	am := NewAgentManager()
	req := &schema.AgentRequest{
		RequestID: "test-req-2",
		ChannelID: "web",
		Params:    []byte(`{"mode": "code.normal"}`),
		IsStream:  true,
	}

	mode, subMode := resolveModeFromRequest(req)
	assert.Equal(t, "code", mode)
	assert.Equal(t, "normal", subMode)

	agent, err := am.GetAgent(context.Background(), req.ChannelID, mode, "", subMode)
	require.NoError(t, err)
	assert.NotNil(t, agent)
}

// --- ReloadAgentsConfig ---

func TestAgentManager_ReloadAgentsConfig(t *testing.T) {
	am := NewAgentManager()
	err := am.ReloadAgentsConfig(context.Background(), nil, nil)
	assert.NoError(t, err)
}

func TestReloadAgentsConfig_环境变量注入(t *testing.T) {
	am := NewAgentManager()

	envOverrides := map[string]any{
		"MODEL_PROVIDER": "openai",
		"MODEL_NAME":     "gpt-4",
	}

	err := am.ReloadAgentsConfig(context.Background(), nil, envOverrides)
	require.NoError(t, err)

	assert.Equal(t, "openai", os.Getenv("MODEL_PROVIDER"))
	assert.Equal(t, "gpt-4", os.Getenv("MODEL_NAME"))

	// 清理
	_ = os.Unsetenv("MODEL_PROVIDER")
	_ = os.Unsetenv("MODEL_NAME")
}

func TestReloadAgentsConfig_空字符串Unsetenv(t *testing.T) {
	_ = os.Setenv("TEST_RELOAD_KEY", "old_value")
	defer func() { _ = os.Unsetenv("TEST_RELOAD_KEY") }()

	am := NewAgentManager()
	envOverrides := map[string]any{
		"TEST_RELOAD_KEY": "",
	}

	err := am.ReloadAgentsConfig(context.Background(), nil, envOverrides)
	require.NoError(t, err)

	assert.Equal(t, "", os.Getenv("TEST_RELOAD_KEY"))
}

func TestReloadAgentsConfig_nil值Unsetenv(t *testing.T) {
	_ = os.Setenv("TEST_RELOAD_NIL", "old_value")
	defer func() { _ = os.Unsetenv("TEST_RELOAD_NIL") }()

	am := NewAgentManager()
	envOverrides := map[string]any{
		"TEST_RELOAD_NIL": nil,
	}

	err := am.ReloadAgentsConfig(context.Background(), nil, envOverrides)
	require.NoError(t, err)

	assert.Equal(t, "", os.Getenv("TEST_RELOAD_NIL"))
}

func TestReloadAgentsConfig_保存latestEnvOverrides(t *testing.T) {
	am := NewAgentManager()
	envOverrides := map[string]any{
		"TEST_KEY": "test_value",
	}

	err := am.ReloadAgentsConfig(context.Background(), nil, envOverrides)
	require.NoError(t, err)

	am.mu.RLock()
	val, ok := am.latestEnvOverrides["TEST_KEY"]
	am.mu.RUnlock()
	assert.True(t, ok)
	assert.Equal(t, "test_value", val)

	_ = os.Unsetenv("TEST_KEY")
}

// --- RecreateAgent ---

func TestAgentManager_RecreateAgent(t *testing.T) {
	am := NewAgentManager()

	// 空 channel → 返回 nil
	result, err := am.RecreateAgent(context.Background(), "nonexistent", true)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestAgentManager_RecreateAgent_immediateTrue(t *testing.T) {
	am := NewAgentManager()

	// 先创建（channelID="web" → channel_key="web"）
	_, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)
	require.Equal(t, 1, len(am.agents["web"]))

	// immediate=true 重建
	result, err := am.RecreateAgent(context.Background(), "web", true)
	assert.NoError(t, err)
	assert.NotEmpty(t, result)

	// 应该有新实例
	assert.Equal(t, 1, len(am.agents["web"]))
}

func TestAgentManager_RecreateAgent_immediateFalse(t *testing.T) {
	am := NewAgentManager()

	// 先创建（channelID="web" → channel_key="web"）
	_, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)
	require.Equal(t, 1, len(am.agents["web"]))

	// immediate=false → 不重建
	result, err := am.RecreateAgent(context.Background(), "web", false)
	assert.NoError(t, err)
	assert.NotEmpty(t, result)

	// channel 下应该没有 agent 了
	am.mu.RLock()
	_, ok := am.agents["web"]
	am.mu.RUnlock()
	assert.False(t, ok)
}

// --- CancelAllInflightWork ---

func TestAgentManager_CancelAllInflightWork(t *testing.T) {
	am := NewAgentManager()
	err := am.CancelAllInflightWork(context.Background())
	assert.NoError(t, err)
}

func TestAgentManager_CancelAllInflightWork_有agent时(t *testing.T) {
	am := NewAgentManager()
	_, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)

	err = am.CancelAllInflightWork(context.Background())
	assert.NoError(t, err)
}

// --- Cleanup ---

func TestAgentManager_Cleanup(t *testing.T) {
	am := NewAgentManager()

	// 创建几个 agent
	_, err := am.GetAgent(context.Background(), "web", "agent", "", "")
	require.NoError(t, err)
	_, err = am.GetAgent(context.Background(), "web", "code", "", "")
	require.NoError(t, err)

	err = am.Cleanup()
	assert.NoError(t, err)

	// 清理后 maps 应该为空
	assert.Empty(t, am.agents)
	assert.Empty(t, am.agentCreateParams)
	assert.Empty(t, am.clientCapabilitiesByChannel)
}

// --- resolveModeFromRequest ---

func TestResolveModeFromRequest(t *testing.T) {
	tests := []struct {
		name      string
		params    string
		wantMode  string
		wantSub   string
	}{
		{"nil params", "", "agent", ""},
		{"empty json", `{}`, "agent", "plan"},  // mode 为空时默认 "agent.plan"
		{"agent.plan", `{"mode": "agent.plan"}`, "agent", "plan"},
		{"code.normal", `{"mode": "code.normal"}`, "code", "normal"},
		{"code only", `{"mode": "code"}`, "code", ""},
		{"empty mode defaults to agent.plan", `{"mode": ""}`, "agent", "plan"},  // 空 mode 默认 "agent.plan"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &schema.AgentRequest{}
			if tt.params != "" {
				req.Params = []byte(tt.params)
			} else {
				req.Params = nil
			}
			mode, subMode := resolveModeFromRequest(req)
			assert.Equal(t, tt.wantMode, mode)
			assert.Equal(t, tt.wantSub, subMode)
		})
	}
}

// --- resolveWorkspaceDirFromRequest ---

func TestResolveWorkspaceDirFromRequest(t *testing.T) {
	req := &schema.AgentRequest{Params: []byte(`{"workspace_dir": "/home/user/proj"}`)}
	assert.Equal(t, "/home/user/proj", resolveWorkspaceDirFromRequest(req))

	req2 := &schema.AgentRequest{Params: nil}
	assert.Equal(t, "", resolveWorkspaceDirFromRequest(req2))
}

// --- copyMap ---

func TestCopyMap(t *testing.T) {
	original := map[string]any{"key": "value", "num": 42}
	copied := copyMap(original)

	assert.Equal(t, original, copied)
	// 修改拷贝不影响原 map
	copied["key"] = "changed"
	assert.Equal(t, "value", original["key"])

	// nil map
	assert.Nil(t, copyMap(nil))
}

// --- makeAgentCacheKey 与 GetAgent 的集成测试 ---

func TestAgentManager_GetAgent_cacheKey隔离(t *testing.T) {
	am := NewAgentManager()

	// 不同 projectDir 应创建不同实例
	agent1, err := am.GetAgent(context.Background(), "web", "agent", "/home/user/proj1", "")
	require.NoError(t, err)
	agent2, err := am.GetAgent(context.Background(), "web", "agent", "/home/user/proj2", "")
	require.NoError(t, err)
	assert.NotSame(t, agent1, agent2)

	// 同一个 projectDir 返回同一个实例
	agent3, err := am.GetAgent(context.Background(), "web", "agent", "/home/user/proj1", "")
	require.NoError(t, err)
	assert.Same(t, agent1, agent3)
}

// --- normalizeProjectDir 额外边界测试 ---

func TestNormalizeProjectDir_仅空格(t *testing.T) {
	assert.Equal(t, "", normalizeProjectDir("   "))
}

func TestNormalizeChannelID_default值(t *testing.T) {
	// 对齐 Python: None 或空 → "default"
	assert.Equal(t, "default", normalizeChannelID(""))
	assert.Equal(t, "default", normalizeChannelID("   "))
	assert.Equal(t, "acp", normalizeChannelID("acp"))
}

func TestMakeAgentCacheKey_各组合(t *testing.T) {
	tests := []struct {
		mode       string
		subMode    string
		projectDir string
		wantPrefix string
	}{
		{"agent", "plan", "", "agent:plan:"},
		{"code", "normal", "", "code:normal:"},
		{"code", "team", "", "code:team:"},
		{"", "", "", "agent::"},
	}

	for _, tt := range tests {
		key := makeAgentCacheKey(tt.mode, tt.subMode, tt.projectDir)
		assert.True(t, strings.HasPrefix(key, tt.wantPrefix), "key=%s, wantPrefix=%s", key, tt.wantPrefix)
	}
}
