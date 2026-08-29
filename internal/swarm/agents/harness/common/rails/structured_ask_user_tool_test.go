package rails

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── NewStructuredAskUserTool ────────────────────────────

// TestNewStructuredAskUserTool_中文 验证中文语言创建
func TestNewStructuredAskUserTool_中文(t *testing.T) {
	askTool, err := NewStructuredAskUserTool("cn", "agent1")
	require.NoError(t, err)
	require.NotNil(t, askTool)

	card := askTool.Card()
	assert.Equal(t, "ask_user_agent1", card.ID)
	assert.Equal(t, "ask_user", card.Name)
	assert.Contains(t, card.Description, "结构化选项")
}

// TestNewStructuredAskUserTool_英文 验证英文语言创建
func TestNewStructuredAskUserTool_英文(t *testing.T) {
	askTool, err := NewStructuredAskUserTool("en", "agent1")
	require.NoError(t, err)
	require.NotNil(t, askTool)

	card := askTool.Card()
	assert.Contains(t, card.Description, "Structured questions")
}

// TestNewStructuredAskUserTool_无AgentID 验证无 agentID 时使用 UUID
func TestNewStructuredAskUserTool_无AgentID(t *testing.T) {
	askTool, err := NewStructuredAskUserTool("cn", "")
	require.NoError(t, err)
	require.NotNil(t, askTool)

	card := askTool.Card()
	// BuildToolCard 生成 ID 格式: "ask_user_<uuid8>"
	assert.Contains(t, card.ID, "ask_user_")
	assert.Greater(t, len(card.ID), len("ask_user_"))
}

// TestNewStructuredAskUserTool_默认语言 验证非 cn/en 语言回退到中文
func TestNewStructuredAskUserTool_默认语言(t *testing.T) {
	askTool, err := NewStructuredAskUserTool("fr", "agent1")
	require.NoError(t, err)
	require.NotNil(t, askTool)

	card := askTool.Card()
	// 非中英文语言，getStructuredDescription 回退到中文
	assert.Contains(t, card.Description, "结构化选项")
}

// TestNewStructuredAskUserTool_InputParams 验证 ToolCard 包含 questions 参数
func TestNewStructuredAskUserTool_InputParams(t *testing.T) {
	askTool, err := NewStructuredAskUserTool("cn", "agent1")
	require.NoError(t, err)
	require.NotNil(t, askTool)

	card := askTool.Card()
	// InputParams 是 []*schema.Param，至少应包含 questions 参数
	assert.NotEmpty(t, card.InputParams)
	// 验证存在名为 questions 的参数
	found := false
	for _, p := range card.InputParams {
		if p.Name == "questions" {
			found = true
			break
		}
	}
	assert.True(t, found, "应包含 questions 参数")
}

// ──────────────────────────── getStructuredDescription ────────────────────────────

// TestGetStructuredDescription_中文 验证中文描述
func TestGetStructuredDescription_中文(t *testing.T) {
	desc := getStructuredDescription("cn")
	assert.Contains(t, desc, "结构化选项")
	assert.Contains(t, desc, "纯文本查询")
}

// TestGetStructuredDescription_英文 验证英文描述
func TestGetStructuredDescription_英文(t *testing.T) {
	desc := getStructuredDescription("en")
	assert.Contains(t, desc, "Structured questions")
	assert.Contains(t, desc, "Plain query")
}
