package agent_mode

import (
	"context"
	"reflect"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeDeepAgentForConstructor 测试用 DeepAgentInterface 空实现，仅用于构造函数测试
type fakeDeepAgentForConstructor struct{}

func (f *fakeDeepAgentForConstructor) ReactAgent() *agents.ReActAgent {
	return nil
}
func (f *fakeDeepAgentForConstructor) Card() *agentschema.AgentCard {
	return nil
}
func (f *fakeDeepAgentForConstructor) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	return nil
}
func (f *fakeDeepAgentForConstructor) LoopController() controller.ControllerInterface {
	return nil
}
func (f *fakeDeepAgentForConstructor) EventHandler() modules.EventHandler { return nil }
func (f *fakeDeepAgentForConstructor) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return &hschema.DeepAgentState{}
}
func (f *fakeDeepAgentForConstructor) DeepConfig() *hschema.DeepAgentConfig { return nil }
func (f *fakeDeepAgentForConstructor) IsInvokeActive() bool                 { return false }
func (f *fakeDeepAgentForConstructor) IsAutoInvokeScheduled() bool          { return false }
func (f *fakeDeepAgentForConstructor) SetAutoInvokeScheduled(_ bool)        {}
func (f *fakeDeepAgentForConstructor) ScheduleAutoInvokeOnSpawnDone(_ context.Context, _ string, _ float64) error {
	return nil
}
func (f *fakeDeepAgentForConstructor) CreateSubagent(_ context.Context, _ string, _ string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}
func (f *fakeDeepAgentForConstructor) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeDeepAgentForConstructor) SwitchMode(_ sessioninterfaces.SessionFacade, _ string) {}
func (f *fakeDeepAgentForConstructor) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade) {
}
func (f *fakeDeepAgentForConstructor) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string {
	return ""
}
func (f *fakeDeepAgentForConstructor) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {
}
func (f *fakeDeepAgentForConstructor) FindRailsByType(_ ...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}
func (f *fakeDeepAgentForConstructor) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}
func (f *fakeDeepAgentForConstructor) InnerInvokeOverride() func(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}

// ──────────────────────────── 构造函数测试 ────────────────────────────

// TestNewEnterPlanModeTool 测试创建 EnterPlanModeTool 实例
func TestNewEnterPlanModeTool(t *testing.T) {
	agent := &fakeDeepAgentForConstructor{}
	tool := NewEnterPlanModeTool(agent, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewEnterPlanModeTool 不应返回 nil")
	}
}

// TestNewExitPlanModeTool 测试创建 ExitPlanModeTool 实例
func TestNewExitPlanModeTool(t *testing.T) {
	agent := &fakeDeepAgentForConstructor{}
	tool := NewExitPlanModeTool(agent, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewExitPlanModeTool 不应返回 nil")
	}
}

// TestNewSwitchModeTool 测试创建 SwitchModeTool 实例
func TestNewSwitchModeTool(t *testing.T) {
	agent := &fakeDeepAgentForConstructor{}
	tool := NewSwitchModeTool(agent, "cn", "test-agent-id")
	if tool == nil {
		t.Error("NewSwitchModeTool 不应返回 nil")
	}
}
