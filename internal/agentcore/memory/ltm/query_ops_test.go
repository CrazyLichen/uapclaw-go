package ltm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// TestGetRecentMessages_ScopeID无效 测试无效 scopeID 返回错误。
func TestGetRecentMessages_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.GetRecentMessages(context.Background(), SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestGetMessageByID_MessageManagerNil 测试 messageManager 未初始化。
func TestGetMessageByID_MessageManagerNil(t *testing.T) {
	m := NewLongTermMemory()
	_, _, err := m.GetMessageByID(context.Background(), "msg1")
	assert.Error(t, err)
}

// TestUserMemTotalNum_ScopeID无效 测试无效 scopeID 返回错误。
func TestUserMemTotalNum_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.UserMemTotalNum(context.Background(), SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestUserMemTotalNum_SearchManagerNil 测试 searchManager 未初始化。
func TestUserMemTotalNum_SearchManagerNil(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.UserMemTotalNum(context.Background(), SearchWithScopeID("valid"))
	assert.Error(t, err)
}

// TestGetUserMemByPage_ScopeID无效 测试无效 scopeID 返回错误。
func TestGetUserMemByPage_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.GetUserMemByPage(context.Background(), 10, 1, mem_model.MemoryTypeUnknown, SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestGetUserMemByPage_SearchManagerNil 测试 searchManager 未初始化。
func TestGetUserMemByPage_SearchManagerNil(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.GetUserMemByPage(context.Background(), 10, 1, mem_model.MemoryTypeUnknown, SearchWithScopeID("valid"))
	assert.Error(t, err)
}
