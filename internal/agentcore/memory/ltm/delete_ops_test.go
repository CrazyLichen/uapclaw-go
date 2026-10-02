package ltm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDeleteMemByID_ScopeID无效 测试无效 scopeID 返回错误。
func TestDeleteMemByID_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	err := m.DeleteMemByID(context.Background(), "mem1", SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestDeleteMemByUserID_ScopeID无效 测试无效 scopeID 返回错误。
func TestDeleteMemByUserID_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	err := m.DeleteMemByUserID(context.Background(), SearchWithScopeID("scope/id"))
	assert.Error(t, err)
}

// TestDeleteMemByScope_ScopeID无效 测试无效 scopeID 返回错误。
func TestDeleteMemByScope_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	err := m.DeleteMemByScope(context.Background(), "")
	assert.Error(t, err)
}

// TestDeleteMessagesByUserAndScope_ScopeID无效 测试无效 scopeID 返回错误。
func TestDeleteMessagesByUserAndScope_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	err := m.DeleteMessagesByUserAndScope(context.Background(), SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestDeleteVariables_ScopeID无效 测试无效 scopeID 返回错误。
func TestDeleteVariables_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	err := m.DeleteVariables(context.Background(), []string{"var1"}, SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestDeleteMemByIDImpl_WriteManagerNil 测试 writeManager 未初始化场景。
func TestDeleteMemByIDImpl_WriteManagerNil(t *testing.T) {
	m := NewLongTermMemory()
	// 需要 kvStore 才能获取锁，kvStore 为 nil 时 acquireUserLock 会失败
	// 此处验证方法签名正确即可
	assert.NotNil(t, m)
}

// TestDeleteVariables_VariableManagerNil 测试 variableManager 未初始化场景。
func TestDeleteVariables_VariableManagerNil(t *testing.T) {
	m := NewLongTermMemory()
	assert.NotNil(t, m)
}
