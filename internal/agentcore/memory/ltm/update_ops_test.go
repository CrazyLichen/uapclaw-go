package ltm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUpdateMemByID_ScopeID无效 测试无效 scopeID 返回错误。
func TestUpdateMemByID_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	err := m.UpdateMemByID(context.Background(), "mem1", "new content", SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestUpdateVariables_ScopeID无效 测试无效 scopeID 返回错误。
func TestUpdateVariables_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	err := m.UpdateVariables(context.Background(), map[string]string{"k": "v"}, SearchWithScopeID(""))
	assert.Error(t, err)
}

// TestUpdateMemByIDImpl_ScopeID无效 测试 impl 层 scopeID 无效。
func TestUpdateMemByIDImpl_ScopeID无效(t *testing.T) {
	m := NewLongTermMemory()
	err := m.updateMemByIDImpl(context.Background(), "mem1", "content", &searchParams{
		UserID:  "u1",
		ScopeID: "",
	})
	assert.Error(t, err)
}

// TestUpdateMemByIDImpl_ScopeID含斜杠 测试含斜杠的 scopeID。
func TestUpdateMemByIDImpl_ScopeID含斜杠(t *testing.T) {
	m := NewLongTermMemory()
	err := m.updateMemByIDImpl(context.Background(), "mem1", "content", &searchParams{
		UserID:  "u1",
		ScopeID: "scope/id",
	})
	assert.Error(t, err)
}
