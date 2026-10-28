package runtime

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────── ACP 分支测试（10.6.24d 回填点⑤）────────────────────

// TestAgentManager_Initialize_ACP通道 验证 ACP initialize 全链：
// 记录能力 + 创建 code 模式 agent + 返回默认 capabilities。
func TestAgentManager_Initialize_ACP通道(t *testing.T) {
	am := NewAgentManager()
	caps, err := am.Initialize(context.Background(), "acp", map[string]any{
		"client_capabilities": map[string]any{"fs": true},
	})
	require.NoError(t, err)
	require.NotNil(t, caps)

	// capabilities 结构
	assert.Equal(t, 1, caps["protocolVersion"])
	agentInfo, ok := caps["agentInfo"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "jiuwenswarm", agentInfo["name"])
	assert.NotEmpty(t, agentInfo["version"])

	// 能力已记录
	recorded := am.GetClientCapabilities("acp")
	assert.Equal(t, true, recorded["fs"])

	// agent 已以 code 模式创建
	entry := am.GetAgentNoWait("acp", "code", "", "")
	assert.NotNil(t, entry)
}

// TestAgentManager_Initialize_ACP重置 验证二次 initialize 清理旧 agent 重建。
func TestAgentManager_Initialize_ACP重置(t *testing.T) {
	am := NewAgentManager()
	first, err := am.GetAgent(context.Background(), "acp", "code", "", "")
	require.NoError(t, err)

	caps, err := am.Initialize(context.Background(), "acp", nil)
	require.NoError(t, err)
	require.NotNil(t, caps)

	second := am.GetAgentNoWait("acp", "code", "", "")
	require.NotNil(t, second)
	// 重置后为不同实例
	assert.NotSame(t, first, second)
}

// TestAgentManager_Initialize_ACP能力为空 验证无 client_capabilities 不写入。
func TestAgentManager_Initialize_ACP能力为空(t *testing.T) {
	am := NewAgentManager()
	caps, err := am.Initialize(context.Background(), "acp", map[string]any{
		"protocol_version": "1.0",
	})
	require.NoError(t, err)
	assert.NotNil(t, caps)
	// 无 client_capabilities 键 → 不写入
	recorded := am.GetClientCapabilities("acp")
	assert.Empty(t, recorded)
}

// TestAgentManager_Initialize_ACP能力非dict 验证非 dict 能力不写入。
func TestAgentManager_Initialize_ACP能力非dict(t *testing.T) {
	am := NewAgentManager()
	caps, err := am.Initialize(context.Background(), "acp", map[string]any{
		"client_capabilities": "not-a-map",
	})
	require.NoError(t, err)
	assert.NotNil(t, caps)
	recorded := am.GetClientCapabilities("acp")
	assert.Empty(t, recorded)
}

// TestAgentManager_CreateSession_ACP 验证 ACP 通道生成 acp_ 前缀 8 位 hex。
func TestAgentManager_CreateSession_ACP(t *testing.T) {
	am := NewAgentManager()
	sessionID, err := am.CreateSession("acp", "")
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^acp_[0-9a-f]{8}$`), sessionID)
	// 唯一性
	other, _ := am.CreateSession("acp", "")
	assert.NotEqual(t, sessionID, other)
}

// TestAgentManager_GetAgent_ACP配置合并 验证 ACP 通道创建时合并专用配置。
func TestAgentManager_GetAgent_ACP配置合并(t *testing.T) {
	am := NewAgentManager()
	agent, err := am.GetAgent(context.Background(), "acp", "agent", "", "")
	require.NoError(t, err)
	require.NotNil(t, agent)
	// 创建参数应记录 ACP 配置（agentCreateParams["acp"] 存在）
	am.mu.RLock()
	params, ok := am.agentCreateParams["acp"]
	am.mu.RUnlock()
	require.True(t, ok, "应记录 acp channel 创建参数")
	for _, entry := range params {
		cfg := entry.config
		require.NotNil(t, cfg)
		assert.Equal(t, "acp_agent", cfg["agent_name"])
		assert.Equal(t, "acp", cfg["channel_id"])
		assert.Equal(t, "acp", cfg["tool_profile"])
		assert.Equal(t, true, cfg["enable_filesystem_rail"])
	}
}

// TestMergeAcpAgentConfig 合并逻辑单元测试。
func TestMergeAcpAgentConfig(t *testing.T) {
	// base + extra 均覆盖，双键强制
	cfg := mergeAcpAgentConfig(
		map[string]any{"project_dir": "/tmp/p", "channel_id": "web"},
		map[string]any{"agent_name": "custom", "tool_profile": "other"},
	)
	assert.Equal(t, "/tmp/p", cfg["project_dir"])
	assert.Equal(t, "custom", cfg["agent_name"])
	// 强制覆盖
	assert.Equal(t, "acp", cfg["channel_id"])
	assert.Equal(t, "acp", cfg["tool_profile"])
	assert.Equal(t, true, cfg["enable_filesystem_rail"])

	// nil base/extra
	cfg = mergeAcpAgentConfig(nil, nil)
	assert.Equal(t, "acp_agent", cfg["agent_name"])
	assert.Equal(t, "acp", cfg["channel_id"])
}

// TestRandomAcpHex8 格式验证。
func TestRandomAcpHex8(t *testing.T) {
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}$`), randomAcpHex8())
	// 唯一性（极小概率碰撞，仅冒烟）
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		s := randomAcpHex8()
		assert.False(t, seen[s], "碰撞: %s", s)
		seen[s] = true
	}
}
