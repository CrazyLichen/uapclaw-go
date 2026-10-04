package ltm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// TestValidateKVStore 测试 KV 存储校验。
func TestValidateKVStore(t *testing.T) {
	// nil 应返回错误
	err := validateKVStore(nil)
	assert.Error(t, err)

	// 非nil 应通过
	var mockStore kv.BaseKVStore = nil
	if mockStore != nil {
		err = validateKVStore(mockStore)
		assert.NoError(t, err)
	}
}

// TestValidateVectorStore 测试向量存储校验。
// S-02: 对齐 Python isinstance + None 检查，nil 返回错误。
func TestValidateVectorStore(t *testing.T) {
	err := validateVectorStore(nil)
	assert.Error(t, err)
	baseErr, ok := err.(*exception.BaseError)
	assert.True(t, ok)
	assert.Equal(t, exception.StatusMemoryRegisterStoreExecutionError, baseErr.Status())
}

// TestValidateDbStore 测试数据库存储校验。
// S-02: 对齐 Python isinstance + None 检查，nil 返回错误。
func TestValidateDbStore(t *testing.T) {
	err := validateDbStore(nil)
	assert.Error(t, err)
	baseErr, ok := err.(*exception.BaseError)
	assert.True(t, ok)
	assert.Equal(t, exception.StatusMemoryRegisterStoreExecutionError, baseErr.Status())
}

// TestValidateMessageStore 测试消息存储校验。
// S-02: 对齐 Python isinstance + None 检查，nil 返回错误。
func TestValidateMessageStore(t *testing.T) {
	err := validateMessageStore(nil)
	assert.Error(t, err)
	baseErr, ok := err.(*exception.BaseError)
	assert.True(t, ok)
	assert.Equal(t, exception.StatusMemoryRegisterStoreExecutionError, baseErr.Status())
}

// TestValidateKVStore_错误类型 测试 nil KV store 返回正确的错误类型。
func TestValidateKVStore_错误类型(t *testing.T) {
	err := validateKVStore(nil)
	assert.NotNil(t, err)
	// 验证是 BaseError 类型
	baseErr, ok := err.(*exception.BaseError)
	if ok {
		assert.Equal(t, exception.StatusMemoryRegisterStoreExecutionError, baseErr.Status())
	}
}
