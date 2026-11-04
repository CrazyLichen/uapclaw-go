//go:build integration

package testhelpers

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

// ──────────────────────────── 结构体 ────────────────────────────

// MockSessionFacade 最小 SessionFacade 实现，供上下文相关集成测试复用。
type MockSessionFacade struct {
	// SessionID 会话 id
	SessionID string
	// States 状态表
	States map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockSessionFacade 创建最小 SessionFacade。
func NewMockSessionFacade(sessionID string) *MockSessionFacade {
	return &MockSessionFacade{SessionID: sessionID, States: map[string]any{}}
}

// GetSessionID 返回会话 id
func (m *MockSessionFacade) GetSessionID() string { return m.SessionID }

// UpdateState 写入状态
func (m *MockSessionFacade) UpdateState(data map[string]any) {
	for k, v := range data {
		m.States[k] = v
	}
}

// GetState 读取状态
func (m *MockSessionFacade) GetState(key state.StateKey) (any, error) {
	v, ok := m.States[key.String()]
	if !ok {
		return nil, nil
	}
	return v, nil
}

// DumpState 导出状态快照
func (m *MockSessionFacade) DumpState() map[string]any {
	out := make(map[string]any, len(m.States))
	for k, v := range m.States {
		out[k] = v
	}
	return out
}

// WriteStream 流写入（测试空实现）
func (m *MockSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 自定义流写入（测试空实现）
func (m *MockSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 环境变量（测试固定 nil）
func (m *MockSessionFacade) GetEnv(_ string, _ ...any) any { return nil }

// Interact 交互（测试空实现）
func (m *MockSessionFacade) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 清空状态
func (m *MockSessionFacade) ClearSession(_ context.Context) {
	m.States = map[string]any{}
}

// 编译期校验接口合规
var _ sessioninterfaces.SessionFacade = (*MockSessionFacade)(nil)

// NewTestContext 用真实 ContextEngine 建一个会话上下文。
//
// 覆盖链路：context_engine（CreateContext）→ session（SessionFacade）→ 上下文实例。
func NewTestContext(t *testing.T, sessionID string) ceiface.ModelContext {
	t.Helper()
	ce := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())
	mc, err := ce.CreateContext(context.Background(), "ctx-"+sessionID, NewMockSessionFacade(sessionID))
	require.NoError(t, err)
	require.NotNil(t, mc)
	require.Equal(t, sessionID, mc.SessionID())
	return mc
}
