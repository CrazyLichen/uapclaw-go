package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 用例 ────────────────────────────

// TestCodingMemoryRail_Init注册工具 Init 把记忆工具注册到 ability_manager 与 resource_mgr。
func TestCodingMemoryRail_Init注册工具(t *testing.T) {
	rail := NewCodingMemoryRail("coding_memory", nil, "cn")
	agent := newFakeBaseAgentForMemoryRail()

	require.NoError(t, rail.Init(context.Background(), agent))

	assert.NotEmpty(t, rail.agentID, "Init 必须记录 agentID")
	assert.NotNil(t, rail.systemPromptBuilder, "Init 必须保存提示词构建器")
	assert.NotEmpty(t, rail.ownedToolNames, "必须登记已注册工具名")
	assert.NotEmpty(t, rail.ownedToolIDs, "必须登记已注册工具 ID")
	assert.NotNil(t, rail.toolCtx, "必须构造工具上下文")
}

// TestCodingMemoryRail_Uninit注销工具 Uninit 移除工具并清空登记集合。
func TestCodingMemoryRail_Uninit注销工具(t *testing.T) {
	rail := NewCodingMemoryRail("coding_memory", nil, "cn")
	agent := newFakeBaseAgentForMemoryRail()
	require.NoError(t, rail.Init(context.Background(), agent))

	require.NoError(t, rail.Uninit(agent))

	assert.Empty(t, rail.ownedToolNames, "Uninit 后工具名集合应清空")
	assert.Empty(t, rail.ownedToolIDs, "Uninit 后工具 ID 集合应清空")
}

// TestCodingMemoryRail_优先级 与 MemoryRail 相同（80）。
func TestCodingMemoryRail_优先级(t *testing.T) {
	rail := NewCodingMemoryRail("", nil, "cn")
	assert.Equal(t, codingMemoryRailPriority, rail.Priority())
}
