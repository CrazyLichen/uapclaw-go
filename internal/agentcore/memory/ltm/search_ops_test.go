package ltm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// TestSearchUserMem_ScopeID无效 测试无效 scopeID 返回错误。
func TestSearchUserMem_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.SearchUserMem(context.Background(), "query", 5, SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestSearchUserHistorySummary_ScopeID无效 测试无效 scopeID 返回错误。
func TestSearchUserHistorySummary_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.SearchUserHistorySummary(context.Background(), "query", 5, SearchWithScopeID("scope/id"))
	assert.Error(t, err)
}

// TestGetVariables_ScopeID无效 测试无效 scopeID 返回错误。
func TestGetVariables_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.GetVariables(context.Background(), nil, SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestGetVariables_NamesType 测试 names 参数类型分发。
func TestGetVariables_NamesType(t *testing.T) {
	m := NewLongTermMemory()
	// 无效类型应返回错误
	_, err := m.GetVariables(context.Background(), 123, SearchWithScopeID("valid"))
	assert.Error(t, err)
}

// TestSearchUserMemImpl_SearchManagerNil 测试 searchManager 未初始化。
func TestSearchUserMemImpl_SearchManagerNil(t *testing.T) {
	m := NewLongTermMemory()
	_, err := m.searchUserMemImpl(context.Background(), &searchParams{
		Query:   "test",
		Num:     5,
		UserID:  "u1",
		ScopeID: "s1",
	}, m.fragmentType)
	assert.Error(t, err)
}

// TestMemResult_构建 测试 MemResult 从搜索结果构建。
func TestMemResult_构建(t *testing.T) {
	mr := &MemResult{
		MemInfo: &MemInfo{
			MemID:   "id1",
			Content: "内容",
			Type:    mem_model.MemoryTypeUserProfile,
		},
		Score: 0.9,
	}
	assert.Equal(t, "id1", mr.MemInfo.MemID)
	assert.Equal(t, mem_model.MemoryTypeUserProfile, mr.MemInfo.Type)
	assert.InDelta(t, 0.9, mr.Score, 0.001)
}
