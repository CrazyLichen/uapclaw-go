package tool

import (
	"testing"

	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// fakeCallbackContext 测试用回调上下文。
type fakeCallbackContext struct {
	// extra 调用级扩展数据
	extra map[string]any
	// session 会话门面
	session sessioninterfaces.SessionFacade
}

// Extra 返回调用级扩展数据。
func (f *fakeCallbackContext) Extra() map[string]any { return f.extra }

// Session 返回会话门面。
func (f *fakeCallbackContext) Session() sessioninterfaces.SessionFacade { return f.session }

// TestWithToolCallbackContext 验证选项能写入回调上下文。
func TestWithToolCallbackContext(t *testing.T) {
	fake := &fakeCallbackContext{extra: map[string]any{"k": "v"}}
	opts := &ToolCallOptions{}
	WithToolCallbackContext(fake)(opts)
	if opts.CallbackContext == nil {
		t.Fatal("选项应写入 CallbackContext")
	}
	if got := opts.CallbackContext.Extra()["k"]; got != "v" {
		t.Fatalf("Extra 应透传，got %v", got)
	}
}

// TestWithToolCallbackContext_nil不覆盖 验证 nil 不覆盖已有值。
func TestWithToolCallbackContext_nil不覆盖(t *testing.T) {
	fake := &fakeCallbackContext{extra: map[string]any{}}
	opts := &ToolCallOptions{}
	WithToolCallbackContext(fake)(opts)
	WithToolCallbackContext(nil)(opts)
	if opts.CallbackContext == nil {
		t.Fatal("nil 不应覆盖已有值")
	}
}
