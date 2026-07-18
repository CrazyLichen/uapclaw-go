package updater

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// mockUpdater 用于验证 Updater 接口兼容性
type mockUpdater struct {
	bindCalled      bool
	bindReturn      int
	requireForward  bool
	updateCalled    bool
	processCalled   bool
	getStateCalled  bool
	loadStateCalled bool
	lastSignals     []*signal.EvolutionSignal
	lastConfig      map[string]any
}

func (m *mockUpdater) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	m.bindCalled = true
	return m.bindReturn
}

func (m *mockUpdater) RequiresForwardData() bool {
	return m.requireForward
}

func (m *mockUpdater) Update(ctx context.Context, trajectories []*trajectory.Trajectory, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) (map[schema.UpdateKey]any, error) {
	m.updateCalled = true
	return map[schema.UpdateKey]any{}, nil
}

func (m *mockUpdater) Process(ctx context.Context, trajectories []*trajectory.Trajectory, signals []*signal.EvolutionSignal, config map[string]any) (map[schema.UpdateKey]any, error) {
	m.processCalled = true
	m.lastSignals = signals
	m.lastConfig = config
	return map[schema.UpdateKey]any{}, nil
}

func (m *mockUpdater) GetState() map[string]any {
	m.getStateCalled = true
	return map[string]any{}
}

func (m *mockUpdater) LoadState(state map[string]any) {
	m.loadStateCalled = true
}

// TestUpdater_接口兼容性 验证 Updater 接口可被实现并调用
func TestUpdater_接口兼容性(t *testing.T) {
	var _ Updater = &mockUpdater{}

	u := &mockUpdater{bindReturn: 3, requireForward: true}

	n := u.Bind(nil, nil, nil)
	if n != 3 {
		t.Errorf("Bind returned %d, want 3", n)
	}
	if !u.bindCalled {
		t.Error("Bind was not called")
	}

	if !u.RequiresForwardData() {
		t.Error("RequiresForwardData returned false, want true")
	}

	_, _ = u.Update(context.Background(), nil, nil, nil)
	if !u.updateCalled {
		t.Error("Update was not called")
	}

	sig := &signal.EvolutionSignal{SignalType: "low_score"}
	_, _ = u.Process(context.Background(), nil, []*signal.EvolutionSignal{sig}, map[string]any{"key": "val"})
	if !u.processCalled {
		t.Error("Process was not called")
	}
	if len(u.lastSignals) != 1 || u.lastSignals[0].SignalType != "low_score" {
		t.Errorf("Process signals = %v, want 1 signal with type low_score", u.lastSignals)
	}
	if u.lastConfig["key"] != "val" {
		t.Errorf("Process config = %v, want key=val", u.lastConfig)
	}

	state := u.GetState()
	if !u.getStateCalled {
		t.Error("GetState was not called")
	}
	if len(state) != 0 {
		t.Errorf("GetState returned %v, want empty map", state)
	}

	u.LoadState(map[string]any{"key": "val"})
	if !u.loadStateCalled {
		t.Error("LoadState was not called")
	}
}

// TestUpdater_接口方法完整性 验证接口定义了所有必需方法
func TestUpdater_接口方法完整性(t *testing.T) {
	// 编译期检查：mockUpdater 必须实现 Updater 接口的所有方法
	var u Updater = &mockUpdater{}

	// 逐方法验证存在性
	_ = u.Bind
	_ = u.RequiresForwardData
	_ = u.Update
	_ = u.Process
	_ = u.GetState
	_ = u.LoadState
}
