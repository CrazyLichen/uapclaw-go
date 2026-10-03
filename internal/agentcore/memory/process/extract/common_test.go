package extract

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExtractMemoryParams_字段赋值 测试 ExtractMemoryParams 字段赋值
func TestExtractMemoryParams_字段赋值(t *testing.T) {
	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        nil,
		HistoryMessages: nil,
		BaseChatModel:   nil,
	}
	if params.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", params.UserID, "user1")
	}
	if params.ScopeID != "scope1" {
		t.Errorf("ScopeID = %q, want %q", params.ScopeID, "scope1")
	}
}

// TestMemoryOperationParams_字段赋值 测试 MemoryOperationParams 字段赋值
func TestMemoryOperationParams_字段赋值(t *testing.T) {
	params := &MemoryOperationParams{
		UserID:        "user1",
		ScopeID:       "scope1",
		MessageMemID:  "msg1",
		Timestamp:     "2026-01-05 10:00:00",
		BaseModel:     nil,
		SemanticStore: nil,
	}
	if params.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", params.UserID, "user1")
	}
	if params.MessageMemID != "msg1" {
		t.Errorf("MessageMemID = %q, want %q", params.MessageMemID, "msg1")
	}
	if params.Timestamp != "2026-01-05 10:00:00" {
		t.Errorf("Timestamp = %q, want %q", params.Timestamp, "2026-01-05 10:00:00")
	}
}

// TestExtractMemoryParams_类型检查 测试消息字段和模型字段的类型
func TestExtractMemoryParams_类型检查(t *testing.T) {
	// 验证 Messages 字段可赋值 []schema.BaseMessage
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ []schema.BaseMessage = ExtractMemoryParams{}.Messages
	// 验证 HistoryMessages 字段可赋值 []schema.BaseMessage
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ []schema.BaseMessage = ExtractMemoryParams{}.HistoryMessages
	// 验证 BaseChatModel 字段可赋值 *llm.Model
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ *llm.Model = ExtractMemoryParams{}.BaseChatModel
}

// TestMemoryOperationParams_类型检查 测试模型字段和语义存储字段的类型
func TestMemoryOperationParams_类型检查(t *testing.T) {
	// 验证 BaseModel 字段可赋值 *llm.Model
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ *llm.Model = MemoryOperationParams{}.BaseModel
	// 验证 SemanticStore 字段可赋值 any
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ any = MemoryOperationParams{}.SemanticStore
}
