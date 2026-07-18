package adapter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──── AgentTool 基本测试 ────

// TestAgentTool_Card 测试 Card() 返回正确卡片
func TestAgentTool_Card(t *testing.T) {
	card := tool.NewToolCardWithID("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, nil)
	assert.Equal(t, "Agent", at.Card().Name)
	assert.Equal(t, "agent_tool_test", at.Card().ID)
}

// TestAgentTool_Invoke_缺少subagentType 测试缺少 subagent_type 参数
func TestAgentTool_Invoke_缺少subagentType(t *testing.T) {
	card := tool.NewToolCardWithID("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, nil)
	_, err := at.Invoke(context.Background(), map[string]any{"prompt": "hello"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subagent_type")
	assert.Contains(t, err.Error(), "required")
}

// TestAgentTool_Invoke_缺少prompt 测试缺少 prompt 参数
func TestAgentTool_Invoke_缺少prompt(t *testing.T) {
	card := tool.NewToolCardWithID("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, nil)
	_, err := at.Invoke(context.Background(), map[string]any{"subagent_type": "reviewer"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prompt")
	assert.Contains(t, err.Error(), "required")
}

// TestAgentTool_Invoke_未找到Agent 测试找不到指定的自定义 Agent
func TestAgentTool_Invoke_未找到Agent(t *testing.T) {
	card := tool.NewToolCardWithID("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, []*types.AgentDefinition{})
	_, err := at.Invoke(context.Background(), map[string]any{
		"subagent_type": "nonexistent",
		"prompt":        "hello",
		"description":   "test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestAgentTool_Invoke_自定义Agent列表 测试可用 Agent 列表
func TestAgentTool_Invoke_自定义Agent列表(t *testing.T) {
	card := tool.NewToolCardWithID("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, []*types.AgentDefinition{
		{Name: "reviewer", Description: "代码审查"},
	})
	_, err := at.Invoke(context.Background(), map[string]any{
		"subagent_type": "nonexistent",
		"prompt":        "hello",
		"description":   "test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reviewer")
}

// ──── buildSubSessionID 测试 ────

// TestBuildSubSessionID 测试 sub session ID 格式
func TestBuildSubSessionID(t *testing.T) {
	id := buildSubSessionID("sess123", "reviewer")
	assert.Contains(t, id, "sess123_custom_reviewer_")
	// random hex: 8 字符 (4 bytes)
	prefix := "sess123_custom_reviewer_"
	suffix := id[len(prefix):]
	assert.Len(t, suffix, 8)
}

// TestBuildSubSessionID_唯一性 测试多次调用生成不同 ID
func TestBuildSubSessionID_唯一性(t *testing.T) {
	id1 := buildSubSessionID("sess", "agent")
	id2 := buildSubSessionID("sess", "agent")
	assert.NotEqual(t, id1, id2)
}

// ──── Stream 测试 ────

// TestAgentTool_Stream_不支持 测试 Stream 返回不支持错误
func TestAgentTool_Stream_不支持(t *testing.T) {
	card := tool.NewToolCardWithID("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, nil)
	_, err := at.Stream(context.Background(), map[string]any{})
	assert.Error(t, err)
}

// ──── NewAgentTool 映射测试 ────

// TestNewAgentTool_映射构建 测试 customAgents 映射正确构建
func TestNewAgentTool_映射构建(t *testing.T) {
	card := tool.NewToolCardWithID("agent_tool_test", "Agent", "测试", nil, nil)
	agents := []*types.AgentDefinition{
		{Name: "reviewer", Description: "代码审查"},
		{Name: "tester", Description: "测试"},
	}
	at := NewAgentTool(card, nil, agents)
	assert.Len(t, at.customAgents, 2)
	_, ok1 := at.customAgents["reviewer"]
	_, ok2 := at.customAgents["tester"]
	assert.True(t, ok1)
	assert.True(t, ok2)
}
