package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	stream "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// fakeSessionAgent 测试用 BaseAgent——记录 Invoke 调用并返回预置结果
type fakeSessionAgent struct {
	invokeErr   error
	invokeDelay time.Duration
	mu          sync.Mutex
	invocations []map[string]any
}

var _ agentinterfaces.BaseAgent = (*fakeSessionAgent)(nil)

func (f *fakeSessionAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error { return nil }
func (f *fakeSessionAgent) Invoke(ctx context.Context, inputs map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	f.mu.Lock()
	f.invocations = append(f.invocations, inputs)
	f.mu.Unlock()
	if f.invokeDelay > 0 {
		select {
		case <-time.After(f.invokeDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	return map[string]any{"output": "done-" + todoStr(inputs, "query")}, nil
}
func (f *fakeSessionAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeSessionAgent) Card() *agentschema.AgentCard { return nil }
func (f *fakeSessionAgent) Config() agentinterfaces.AgentConfig {
	return nil
}
func (f *fakeSessionAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeSessionAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return nil }
func (f *fakeSessionAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}
func (f *fakeSessionAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeSessionAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeSessionAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// newTestToolkit 测试脚手架：注入 fake provider
func newTestToolkit(t *testing.T) (*MultiSessionToolkit, *[]map[string]any) {
	t.Helper()
	var pushed []map[string]any
	var mu sync.Mutex
	SetSessionPushFunc(func(_ context.Context, msg map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		pushed = append(pushed, msg)
		return nil
	})
	t.Cleanup(func() { SetSessionPushFunc(nil) })
	return NewMultiSessionToolkit("parent-sess", "web", "req-1", &saconfig.ReActAgentConfig{}), &pushed
}

// TestNewMultiSessionToolkit_基础 初始化字段
func TestNewMultiSessionToolkit_基础(t *testing.T) {
	tk := NewMultiSessionToolkit("s1", "ch1", "r1", &saconfig.ReActAgentConfig{})
	if tk.sessionID != "s1" || tk.channelID != "ch1" || tk.requestID != "r1" {
		t.Errorf("toolkit fields: %+v", tk)
	}
	if len(tk.GetTools()) != 3 {
		t.Errorf("GetTools len=%d, want 3", len(tk.GetTools()))
	}
	names := []string{tk.GetTools()[0].Card().Name, tk.GetTools()[1].Card().Name, tk.GetTools()[2].Card().Name}
	for i, want := range []string{"session_new", "session_cancel", "session_list"} {
		if names[i] != want {
			t.Errorf("tool[%d] name=%s, want %s", i, names[i], want)
		}
	}
}

// TestSessionTools_文案 session_new/cancel/list 返回文案逐字
func TestSessionTools_文案(t *testing.T) {
	tk, _ := newTestToolkit(t)
	ctx := context.Background()

	// session_list 空态
	res, err := tk.GetTools()[2].Invoke(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !containsDeep(res, "暂无协程") {
		t.Errorf("empty list res=%v", res)
	}

	// session_cancel 未找到
	res2, _ := tk.GetTools()[1].Invoke(ctx, map[string]any{"session_id": "nope"})
	if !containsDeep(res2, "未找到 session_id=nope") {
		t.Errorf("cancel missing res=%v", res2)
	}
}

// containsDeep 深度查找 map 中含子串的字符串值
func containsDeep(m map[string]any, sub string) bool {
	for _, v := range m {
		if s, ok := v.(string); ok && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
