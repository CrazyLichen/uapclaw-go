package interrupt

import (
	"testing"

	"github.com/stretchr/testify/assert"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestInterruptRequest_字段默认值(t *testing.T) {
	req := &InterruptRequest{}
	assert.Equal(t, "", req.Message)
	assert.Nil(t, req.PayloadSchema)
	assert.Equal(t, "", req.AutoConfirmKey)
	assert.Nil(t, req.UIOptions)
}

func TestInterruptRequest_完整字段(t *testing.T) {
	req := &InterruptRequest{
		Message:        "请确认是否继续",
		PayloadSchema:  map[string]any{"type": "object"},
		AutoConfirmKey: "auto_yes",
		UIOptions:      []map[string]any{{"label": "确认"}, {"label": "取消"}},
	}
	assert.Equal(t, "请确认是否继续", req.Message)
	assert.NotNil(t, req.PayloadSchema)
	assert.Equal(t, "auto_yes", req.AutoConfirmKey)
	assert.Len(t, req.UIOptions, 2)
}

func TestToolCallInterruptRequest_从ToolCall创建(t *testing.T) {
	req := &InterruptRequest{
		Message:        "需要用户输入",
		PayloadSchema:  nil,
		AutoConfirmKey: "",
		UIOptions:      nil,
	}
	tc := &llmschema.ToolCall{
		ID:        "call_123",
		Name:      "search",
		Arguments: `{"query": "test"}`,
		Index:     1,
	}

	tcReq := NewToolCallInterruptRequest(req, tc)
	assert.Equal(t, "需要用户输入", tcReq.Message)
	assert.Equal(t, "search", tcReq.ToolName)
	assert.Equal(t, "call_123", tcReq.ToolCallID)
	assert.Equal(t, `{"query": "test"}`, tcReq.ToolArgs)
	assert.Equal(t, 1, tcReq.Index)
}

func TestToolCallInterruptRequest_Index默认值(t *testing.T) {
	req := &InterruptRequest{Message: "test"}
	tc := &llmschema.ToolCall{ID: "c1", Name: "tool1", Arguments: "{}", Index: 0}
	tcReq := NewToolCallInterruptRequest(req, tc)
	assert.Equal(t, 0, tcReq.Index) // 0 表示未设置
}
