package interrupt

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────── 导出函数 ────────────────────

func TestToolInterruptException_Error_有Request(t *testing.T) {
	e := &saschema.ToolInterruptException{
		Request: &saschema.InterruptRequest{Message: "需要确认"},
	}
	assert.Equal(t, "需要确认", e.Error())
}

func TestToolInterruptException_Error_无Request(t *testing.T) {
	e := &saschema.ToolInterruptException{}
	assert.Equal(t, "tool interrupt", e.Error())
}

func TestToolInterruptException_Error_nilRequest(t *testing.T) {
	e := &saschema.ToolInterruptException{Request: nil}
	assert.Equal(t, "tool interrupt", e.Error())
}

func TestToolInterruptException_String_有ToolCall(t *testing.T) {
	e := &saschema.ToolInterruptException{
		Request: &saschema.InterruptRequest{Message: "需要确认"},
		ToolCall: &llmschema.ToolCall{
			ID:   "call_1",
			Name: "search",
		},
	}
	s := e.String()
	assert.Contains(t, s, "需要确认")
	assert.Contains(t, s, "search")
	assert.Contains(t, s, "call_1")
}

func TestToolInterruptException_String_无ToolCall(t *testing.T) {
	e := &saschema.ToolInterruptException{
		Request: &saschema.InterruptRequest{Message: "需要确认"},
	}
	s := e.String()
	assert.Equal(t, "tool interrupt: 需要确认", s)
}

func TestToolInterruptException_String_无Request(t *testing.T) {
	e := &saschema.ToolInterruptException{}
	assert.Equal(t, "tool interrupt", e.String())
}

func TestToolInterruptException_实现error接口(t *testing.T) {
	var err error = &saschema.ToolInterruptException{
		Request: &saschema.InterruptRequest{Message: "中断"},
	}
	assert.NotNil(t, err)
	assert.Equal(t, "中断", err.Error())
}

func TestToolInterruptException_errors_As识别(t *testing.T) {
	inner := &saschema.ToolInterruptException{
		Request: &saschema.InterruptRequest{Message: "中断"},
	}
	// 包装一层
	wrapped := fmt.Errorf("wrapped: %w", inner)

	var tie *saschema.ToolInterruptException
	assert.True(t, errors.As(wrapped, &tie))
	assert.Equal(t, "中断", tie.Request.Message)
}
