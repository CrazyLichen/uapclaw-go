package tool_discovery

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// mockSessionFacade 模拟会话门面（测试用）。
type mockSessionFacade struct{}

// GetSessionID 返回固定会话 id。
func (m *mockSessionFacade) GetSessionID() string { return "test-session" }

// UpdateState 忽略状态写入。
func (m *mockSessionFacade) UpdateState(_ map[string]any) {}

// GetState 返回空状态。
func (m *mockSessionFacade) GetState(_ state.StateKey) (any, error) { return nil, nil }

// DumpState 返回 nil。
func (m *mockSessionFacade) DumpState() map[string]any { return nil }

// WriteStream 忽略流写入。
func (m *mockSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 忽略自定义流写入。
func (m *mockSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 返回 nil。
func (m *mockSessionFacade) GetEnv(_ string, _ ...any) any { return nil }

// Interact 忽略交互。
func (m *mockSessionFacade) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 忽略清理。
func (m *mockSessionFacade) ClearSession(_ context.Context) {}
