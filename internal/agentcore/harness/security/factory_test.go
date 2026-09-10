package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeRail 用于测试的假 Rail，实现 agentinterfaces.AgentRail
type fakeRail struct {
	engine *PermissionEngine
	host   *ToolPermissionHost
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeRail) Priority() int                                             { return 0 }
func (f *fakeRail) Init(_ context.Context, _ agentinterfaces.BaseAgent) error { return nil }
func (f *fakeRail) Uninit(_ agentinterfaces.BaseAgent) error                  { return nil }
func (f *fakeRail) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) AfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) AfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) OnModelException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) BeforeToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) AfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) OnToolException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}
func (f *fakeRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return nil
}

// fakeCtor 用于测试的构造器回调
func fakeCtor(
	config map[string]any,
	engine *PermissionEngine,
	_ []string,
	_ *llm.Model,
	_ string,
	host *ToolPermissionHost,
) agentinterfaces.AgentRail {
	return &fakeRail{engine: engine, host: host}
}

// TestBuildPermissionInterruptRail 测试工厂函数
func TestBuildPermissionInterruptRail(t *testing.T) {
	t.Run("nil配置", func(t *testing.T) {
		rail := BuildPermissionInterruptRail(nil, nil, nil, "", nil, "", fakeCtor)
		assert.Nil(t, rail)
	})

	t.Run("enabled=false", func(t *testing.T) {
		config := map[string]any{"enabled": false}
		rail := BuildPermissionInterruptRail(config, nil, nil, "", nil, "", fakeCtor)
		assert.Nil(t, rail)
	})

	t.Run("enabled=true_无host", func(t *testing.T) {
		config := map[string]any{"enabled": true}
		rail := BuildPermissionInterruptRail(config, nil, nil, "", nil, "", fakeCtor)
		assert.NotNil(t, rail)
		fr := rail.(*fakeRail)
		assert.Nil(t, fr.engine) // 传入的 engine 为 nil
		assert.NotNil(t, fr.host)
	})

	t.Run("enabled=true_有host", func(t *testing.T) {
		config := map[string]any{"enabled": true}
		existingEngine := NewPermissionEngine(config, nil, "", "")
		existingHost := &ToolPermissionHost{
			ResolveWorkspaceDir: func() string { return "/existing" },
		}
		rail := BuildPermissionInterruptRail(config, existingEngine, existingHost, "", nil, "", fakeCtor)
		assert.NotNil(t, rail)
		fr := rail.(*fakeRail)
		assert.Equal(t, existingEngine, fr.engine)
		assert.Equal(t, existingHost, fr.host)
	})

	t.Run("enabled=true_补充workspaceRoot", func(t *testing.T) {
		config := map[string]any{"enabled": true}
		host := &ToolPermissionHost{} // 无 ResolveWorkspaceDir
		rail := BuildPermissionInterruptRail(config, nil, host, "/workspace/root", nil, "", fakeCtor)
		assert.NotNil(t, rail)
		fr := rail.(*fakeRail)
		assert.NotNil(t, fr.host.ResolveWorkspaceDir)
		assert.Equal(t, "/workspace/root", fr.host.ResolveWorkspaceDir())
	})

	t.Run("enabled=true_已有ResolveWorkspaceDir_不覆盖", func(t *testing.T) {
		config := map[string]any{"enabled": true}
		host := &ToolPermissionHost{
			ResolveWorkspaceDir: func() string { return "/original" },
		}
		rail := BuildPermissionInterruptRail(config, nil, host, "/workspace/root", nil, "", fakeCtor)
		assert.NotNil(t, rail)
		fr := rail.(*fakeRail)
		assert.Equal(t, "/original", fr.host.ResolveWorkspaceDir())
	})
}
