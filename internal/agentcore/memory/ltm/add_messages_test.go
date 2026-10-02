package ltm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// TestAddMessages_ScopeID无效 测试无效 scopeID 返回错误。
func TestAddMessages_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.AddMessages(context.Background(), nil, nil, WithScopeID(""))
	assert.Error(t, err)
}

// TestAddMessages_ScopeID含斜杠 测试含斜杠的 scopeID 返回错误。
func TestAddMessages_ScopeID含斜杠(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.AddMessages(context.Background(), nil, nil, WithScopeID("scope/id"))
	assert.Error(t, err)
}

// TestClassifyWriteResult 测试分类写入结果。
func TestClassifyWriteResult(t *testing.T) {
	writeResult := []mem_model.MemoryUnit{
		&mem_model.VariableUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeVariable, MemID: "v1"},
			VariableName:   "name",
			VariableMem:    "value",
		},
		&mem_model.FragmentMemoryUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeUserProfile, MemID: "p1"},
			Content:        "画像内容",
		},
		&mem_model.SummaryUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeSummary, MemID: "s1"},
			Summary:        "摘要内容",
		},
	}
	result := classifyWriteResult(writeResult)
	assert.Len(t, result.Variables, 1)
	assert.Len(t, result.UserProfile, 1)
	assert.Len(t, result.Summary, 1)
	assert.Len(t, result.SemanticMemory, 0)
	assert.Len(t, result.EpisodicMemory, 0)
}

// TestClassifyWriteResult_空 测试空写入结果。
func TestClassifyWriteResult_空(t *testing.T) {
	result := classifyWriteResult(nil)
	assert.Empty(t, result.Variables)
	assert.Empty(t, result.UserProfile)
}

// TestNewAddMessagesParams_AddMessages 测试 AddMessagesOption 参数传递。
func TestNewAddMessagesParams_AddMessages(t *testing.T) {
	agentCfg := config.DefaultAgentMemoryConfig()
	msgs := []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"),
	}
	p := newAddMessagesParams(msgs, agentCfg,
		WithUserID("u1"),
		WithScopeID("s1"),
		WithGenMem(false),
	)
	assert.Equal(t, "u1", p.UserID)
	assert.Equal(t, "s1", p.ScopeID)
	assert.False(t, p.GenMem)
	assert.Equal(t, agentCfg, p.AgentConfig)
}
