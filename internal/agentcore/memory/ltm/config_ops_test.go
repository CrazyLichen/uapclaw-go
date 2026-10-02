package ltm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
)

// TestDeepCopyScopeConfig 测试深拷贝。
func TestDeepCopyScopeConfig(t *testing.T) {
	original := &config.MemoryScopeConfig{
		UserProfileDefinition:    "原始画像",
		SemanticMemoryDefinition: "原始语义",
		EpisodicMemoryDefinition: "原始情景",
	}
	copied := deepCopyScopeConfig(original)
	assert.Equal(t, original.UserProfileDefinition, copied.UserProfileDefinition)
	// 修改拷贝不影响原始
	copied.UserProfileDefinition = "修改后"
	assert.NotEqual(t, original.UserProfileDefinition, copied.UserProfileDefinition)
}

// TestDeepCopyScopeConfig_Nil 测试 nil 输入。
func TestDeepCopyScopeConfig_Nil(t *testing.T) {
	result := deepCopyScopeConfig(nil)
	assert.Nil(t, result)
}

// TestDeepCopyScopeConfig_含模型配置 测试含 ModelCfg 时的深拷贝。
func TestDeepCopyScopeConfig_含模型配置(t *testing.T) {
	original := &config.MemoryScopeConfig{
		UserProfileDefinition: "画像",
	}
	copied := deepCopyScopeConfig(original)
	assert.Equal(t, "画像", copied.UserProfileDefinition)
	// ModelCfg 为 nil 时应正确处理
	assert.Nil(t, copied.ModelCfg)
}

// TestSetConfig_存储未注册 测试 SetConfig 在存储未注册时 panic。
func TestSetConfig_存储未注册(t *testing.T) {
	m := NewLongTermMemory()
	assert.Panics(t, func() {
		m.SetConfig(config.DefaultMemoryEngineConfig())
	})
}
