package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

func TestDefaultAgentMemoryConfig(t *testing.T) {
	cfg := DefaultAgentMemoryConfig()
	assert.Empty(t, cfg.MemVariables)
	assert.True(t, cfg.EnableLongTermMem)
	assert.True(t, cfg.EnableUserProfile)
	assert.True(t, cfg.EnableSemanticMemory)
	assert.True(t, cfg.EnableEpisodicMemory)
	assert.True(t, cfg.EnableSummaryMemory)
}

func TestAgentMemoryConfig_自定义变量(t *testing.T) {
	cfg := &AgentMemoryConfig{
		MemVariables: []commonschema.Param{
			*commonschema.NewStringParam("name", "用户姓名", true),
		},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}
	assert.Len(t, cfg.MemVariables, 1)
	assert.Equal(t, "name", cfg.MemVariables[0].Name)
}

func TestAgentMemoryConfig_部分关闭(t *testing.T) {
	cfg := &AgentMemoryConfig{
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: false,
		EnableEpisodicMemory: false,
		EnableSummaryMemory:  false,
	}
	assert.True(t, cfg.EnableLongTermMem)
	assert.False(t, cfg.EnableSemanticMemory)
	assert.False(t, cfg.EnableEpisodicMemory)
	assert.False(t, cfg.EnableSummaryMemory)
}
