//go:build integration

package attachment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// mockSessionFacade 最小 SessionFacade 实现。
//
// 对齐 tests/integration/agentcore/context_engine 的同名 helper。
type mockSessionFacade struct {
	sessionID string
	states    map[string]any
}

// GetSessionID 返回会话 id
func (m *mockSessionFacade) GetSessionID() string { return m.sessionID }

// UpdateState 写入状态
func (m *mockSessionFacade) UpdateState(data map[string]any) {
	for k, v := range data {
		m.states[k] = v
	}
}

// GetState 读取状态
func (m *mockSessionFacade) GetState(key state.StateKey) (any, error) {
	v, ok := m.states[key.String()]
	if !ok {
		return nil, nil
	}
	return v, nil
}

// DumpState 导出状态快照
func (m *mockSessionFacade) DumpState() map[string]any {
	out := make(map[string]any, len(m.states))
	for k, v := range m.states {
		out[k] = v
	}
	return out
}

// WriteStream 流写入（测试空实现）
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 自定义流写入（测试空实现）
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 环境变量（测试固定 nil）
func (m *mockSessionFacade) GetEnv(_ string, _ ...any) any { return nil }

// Interact 交互（测试空实现）
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 清空状态
func (m *mockSessionFacade) ClearSession(_ context.Context) {
	m.states = make(map[string]any)
}

// 编译期校验接口合规
var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// newContext 用真实 ContextEngine 建一个会话上下文。
func newContext(t *testing.T, sessionID string) ceiface.ModelContext {
	t.Helper()
	ce := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())
	mc, err := ce.CreateContext(context.Background(), "ctx-"+sessionID, &mockSessionFacade{
		sessionID: sessionID,
		states:    map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, mc)
	require.Equal(t, sessionID, mc.SessionID())
	return mc
}
