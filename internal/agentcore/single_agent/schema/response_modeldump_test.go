package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	interrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// plainRequester 未实现 ModelDump 的最小 InterruptRequester 测试替身
// （手动实现接口方法，不嵌入 InterruptRequest——嵌入会继承 ModelDump）。
type plainRequester struct{ message string }

func (r *plainRequester) GetMessage() string              { return r.message }
func (r *plainRequester) GetAutoConfirmKey() string       { return "" }
func (r *plainRequester) SetAutoConfirmKey(string)        {}
func (r *plainRequester) GetMetadata() map[string]any     { return nil }
func (r *plainRequester) SetMetadata(map[string]any)      {}
func (r *plainRequester) GetInterruptOwner() string       { return "" }
func (r *plainRequester) GetInterruptKind() string        { return "" }
func (r *plainRequester) GetInterruptID() string          { return "" }
func (r *plainRequester) SetInterruptID(string)           {}
func (r *plainRequester) SetInterruptKind(string)         {}
func (r *plainRequester) SetInterruptOwner(string)        {}

// TestToolCallInterruptRequest_ModelDump_内嵌缺接口返回错误 验证内嵌请求
// 未实现 ModelDump 时返回错误而非静默降级（拒绝残缺 dict / fmt.Sprint 调试串）。
func TestToolCallInterruptRequest_ModelDump_内嵌缺接口返回错误(t *testing.T) {
	wrapper := &saschema.ToolCallInterruptRequest{Request: &plainRequester{}}
	_, err := wrapper.ModelDump("json", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未实现 ModelDump")
}

// TestToolCallInterruptRequest_ModelDump_正常平铺 验证内嵌请求字段平铺到顶层。
func TestToolCallInterruptRequest_ModelDump_正常平铺(t *testing.T) {
	inner := &saschema.InterruptRequest{Message: "Which city?", InterruptKind: "ask_user"}
	wrapper := &saschema.ToolCallInterruptRequest{
		Request: &interrail.AskUserRequest{
			InterruptRequest: *inner,
			Questions:        []map[string]any{{"question": "Which city?"}},
		},
		ToolName: "ask_user",
	}
	dumped, err := wrapper.ModelDump("json", true)
	require.NoError(t, err)
	assert.Equal(t, "ask_user", dumped["tool_name"])
	assert.Equal(t, "Which city?", dumped["message"])
	assert.Equal(t, "ask_user", dumped["interrupt_kind"])
	assert.Len(t, dumped["questions"], 1)
}
