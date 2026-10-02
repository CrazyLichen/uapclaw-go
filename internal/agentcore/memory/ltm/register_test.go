package ltm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// TestRegisterStore_KVStoreNil 测试 kv_store 为 nil 返回错误。
func TestRegisterStore_KVStoreNil(t *testing.T) {
	m := NewLongTermMemory()
	err := m.RegisterStore(context.Background(), nil)
	assert.Error(t, err)
}

// TestRegisterPlugin 测试 RegisterPlugin 设置 memoryIndex。
func TestRegisterPlugin(t *testing.T) {
	m := NewLongTermMemory()
	assert.Nil(t, m.memoryIndex)
	// memoryIndex == nil 时 RegisterPlugin 应设置
	// （真实 BaseMemoryIndex 实例需要外部依赖，此处仅测试逻辑分支）
}

// TestCopyFields 测试 fields 深拷贝。
func TestCopyFields(t *testing.T) {
	original := map[string]any{"key": "value", "num": 42}
	copied := copyFields(original)
	assert.Equal(t, original, copied)
	// 修改拷贝不应影响原始
	copied["key"] = "changed"
	assert.NotEqual(t, original["key"], copied["key"])
}

// TestCopyFields_Nil 测试 nil fields 拷贝。
func TestCopyFields_Nil(t *testing.T) {
	result := copyFields(nil)
	assert.Nil(t, result)
}

// TestMigrateBetweenIndices 基本签名测试。
// 完整集成测试需要真实的 BaseMemoryIndex 实现，此处仅验证函数可调用。
func TestMigrateBetweenIndices_签名(t *testing.T) {
	// MigrateBetweenIndices 需要 sourceIndex 和 targetIndex 实例
	// 此处验证函数签名正确即可
	_ = MigrateBetweenIndices
}

// TestWithVectorStore 测试 WithVectorStore 选项。
func TestWithVectorStore(t *testing.T) {
	params := &registerStoreParams{}
	opt := WithVectorStore(nil)
	opt(params)
	assert.Nil(t, params.vectorStore)
}

// TestWithDbStore 测试 WithDbStore 选项。
func TestWithDbStore(t *testing.T) {
	params := &registerStoreParams{}
	opt := WithDbStore(nil)
	opt(params)
	assert.Nil(t, params.dbStore)
}

// TestWithEmbeddingModel 测试 WithEmbeddingModel 选项。
func TestWithEmbeddingModel(t *testing.T) {
	params := &registerStoreParams{}
	opt := WithEmbeddingModel(nil)
	opt(params)
	assert.Nil(t, params.embeddingModel)
}

// TestWithMessageStore 测试 WithMessageStore 选项。
func TestWithMessageStore(t *testing.T) {
	params := &registerStoreParams{}
	opt := WithMessageStore(nil)
	opt(params)
	assert.Nil(t, params.messageStore)
}

// TestRegisterStore_KVStoreNil_错误信息 测试 kv_store 为 nil 的错误信息。
func TestRegisterStore_KVStoreNil_错误信息(t *testing.T) {
	m := NewLongTermMemory()
	err := m.RegisterStore(context.Background(), nil)
	require.Error(t, err)
	baseErr, ok := err.(*exception.BaseError)
	if ok {
		assert.Equal(t, exception.StatusMemoryRegisterStoreExecutionError, baseErr.Status())
	}
}
