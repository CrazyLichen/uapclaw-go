package ltm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewLongTermMemory 测试 NewLongTermMemory 零值初始化。
func TestNewLongTermMemory(t *testing.T) {
	m := NewLongTermMemory()
	assert.NotNil(t, m)
	assert.NotNil(t, m.scopeConfig)
	assert.NotNil(t, m.scopeEmbedding)
	assert.Nil(t, m.kvStore)
	assert.Nil(t, m.writeManager)
	assert.Nil(t, m.searchManager)
}

// TestGetLongTermMemory_单例 测试单例返回同一实例。
func TestGetLongTermMemory_单例(t *testing.T) {
	ResetLongTermMemory()
	m1 := GetLongTermMemory()
	m2 := GetLongTermMemory()
	assert.Same(t, m1, m2, "GetLongTermMemory 应返回同一实例")
}

// TestResetLongTermMemory 测试重置单例。
func TestResetLongTermMemory(t *testing.T) {
	ResetLongTermMemory()
	m1 := GetLongTermMemory()
	ResetLongTermMemory()
	m2 := GetLongTermMemory()
	assert.NotSame(t, m1, m2, "ResetLongTermMemory 后应返回新实例")
}

// TestValidateID 测试 scopeID 校验。
func TestValidateID(t *testing.T) {
	// 空 scopeID
	assert.False(t, validateID("MEMORY_STORE", ""))
	// 包含斜杠
	assert.False(t, validateID("MEMORY_STORE", "scope/id"))
	// 超过 128 字符
	longID := ""
	for i := 0; i < 129; i++ {
		longID += "a"
	}
	assert.False(t, validateID("MEMORY_STORE", longID))
	// 合法 scopeID
	assert.True(t, validateID("MEMORY_STORE", "valid-scope-id"))
}
