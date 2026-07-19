package optimizer

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockOperator 用于测试的模拟 Operator
type mockOperator struct {
	operatorID string
	tunables   map[string]operator.TunableSpec
	state      map[string]any
}

func (m *mockOperator) OperatorID() string                           { return m.operatorID }
func (m *mockOperator) GetTunables() map[string]operator.TunableSpec { return m.tunables }
func (m *mockOperator) GetState() map[string]any                     { return m.state }
func (m *mockOperator) SetParameter(target string, value any)        {}
func (m *mockOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return schema.ApplyResult{}
}
func (m *mockOperator) LoadState(state map[string]any) {}

// 确保 mockOperator 满足 Operator 接口（编译期校验）
var _ operator.Operator = (*mockOperator)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// TextualParameter 测试
func TestTextualParameter_梯度操作(t *testing.T) {
	p := NewTextualParameter("op1")
	p.SetGradient("system_prompt", "improved prompt")
	if p.GetGradient("system_prompt") != "improved prompt" {
		t.Error("gradient mismatch")
	}
	if p.GetGradient("nonexistent") != nil {
		t.Error("nonexistent gradient should be nil")
	}
}

func TestTextualParameter_描述操作(t *testing.T) {
	p := NewTextualParameter("op1")
	p.SetDescription("test description")
	if p.GetDescription() != "test description" {
		t.Error("description mismatch")
	}
}

func TestTextualParameter_OperatorID(t *testing.T) {
	p := NewTextualParameter("agent1/llm_main")
	if p.OperatorID != "agent1/llm_main" {
		t.Errorf("OperatorID = %q, want %q", p.OperatorID, "agent1/llm_main")
	}
}

func TestTextualParameter_默认空Gradients(t *testing.T) {
	p := NewTextualParameter("op1")
	if p.Gradients == nil {
		t.Error("Gradients should be initialized as empty map, not nil")
	}
	if len(p.Gradients) != 0 {
		t.Errorf("Gradients len = %d, want 0", len(p.Gradients))
	}
}

// BaseOptimizerMixin.Bind 测试
func TestBaseOptimizerMixin_Bind匹配(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	n := m.Bind(ops, []string{"system_prompt"}, nil)
	if n != 1 {
		t.Errorf("Bind returned %d, want 1", n)
	}
	if len(m.parameters) != 1 {
		t.Errorf("parameters count = %d, want 1", len(m.parameters))
	}
	if len(m.trajectories) != 0 {
		t.Errorf("trajectories should be empty after bind")
	}
}

func TestBaseOptimizerMixin_Bind无匹配(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"tool_description": {Name: "tool_description"}},
		},
	}
	n := m.Bind(ops, []string{"system_prompt"}, nil)
	if n != 0 {
		t.Errorf("Bind returned %d, want 0", n)
	}
}

func TestBaseOptimizerMixin_Bind创建TextualParameter(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
		"op2": &mockOperator{
			operatorID: "op2",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}, "user_prompt": {Name: "user_prompt"}},
		},
	}
	n := m.Bind(ops, []string{"system_prompt"}, nil)
	if n != 2 {
		t.Errorf("Bind returned %d, want 2", n)
	}
	params := m.Parameters()
	if params["op1"].OperatorID != "op1" {
		t.Errorf("op1 parameter OperatorID = %q, want op1", params["op1"].OperatorID)
	}
	if params["op2"].OperatorID != "op2" {
		t.Errorf("op2 parameter OperatorID = %q, want op2", params["op2"].OperatorID)
	}
}

// BaseOptimizerMixin.AddTrajectory/GetTrajectories/ClearTrajectories 测试
func TestBaseOptimizerMixin_轨迹缓存(t *testing.T) {
	m := &BaseOptimizerMixin{}
	traj1 := &trajectory.Trajectory{ExecutionID: "exec1", Source: "offline"}
	traj2 := &trajectory.Trajectory{ExecutionID: "exec2", Source: "offline"}

	m.AddTrajectory(traj1)
	m.AddTrajectory(traj2)

	trajs := m.GetTrajectories()
	if len(trajs) != 2 {
		t.Fatalf("GetTrajectories returned %d, want 2", len(trajs))
	}
	if trajs[0].ExecutionID != "exec1" {
		t.Errorf("trajs[0].ExecutionID = %q, want exec1", trajs[0].ExecutionID)
	}

	m.ClearTrajectories()
	if len(m.GetTrajectories()) != 0 {
		t.Error("ClearTrajectories should empty the list")
	}
}

// GetTrajectories 返回副本
func TestBaseOptimizerMixin_GetTrajectories副本(t *testing.T) {
	m := &BaseOptimizerMixin{}
	traj1 := &trajectory.Trajectory{ExecutionID: "exec1"}
	m.AddTrajectory(traj1)

	trajs := m.GetTrajectories()
	// 修改返回的切片不应影响内部状态
	trajs[0] = &trajectory.Trajectory{ExecutionID: "modified"}
	inner := m.GetTrajectories()
	if inner[0].ExecutionID != "exec1" {
		t.Error("modifying GetTrajectories() result should not affect internal state")
	}
}

// BaseOptimizerMixin.Parameters 测试
func TestBaseOptimizerMixin_Parameters副本(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	m.Bind(ops, []string{"system_prompt"}, nil)

	params := m.Parameters()
	// 修改副本不应影响原始
	delete(params, "op1")
	if _, ok := m.parameters["op1"]; !ok {
		t.Error("modifying Parameters() copy should not affect original")
	}
}

// BaseOptimizerMixin.SelectSignals 测试
func TestBaseOptimizerMixin_SelectSignals默认全选(t *testing.T) {
	m := &BaseOptimizerMixin{}
	signals := []*signal.EvolutionSignal{
		{SignalType: "low_score"},
		{SignalType: "error"},
	}
	selected := m.SelectSignals(signals)
	if len(selected) != 2 {
		t.Errorf("SelectSignals returned %d, want 2", len(selected))
	}
}

// BaseOptimizerMixin.ValidateParameters 测试
func TestBaseOptimizerMixin_ValidateParameters空时panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("ValidateParameters should panic on empty parameters")
		}
	}()
	m := &BaseOptimizerMixin{}
	m.ValidateParameters()
}

func TestBaseOptimizerMixin_ValidateParameters有参数时不panic(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	m.Bind(ops, []string{"system_prompt"}, nil)
	// 不应 panic
	m.ValidateParameters()
}

// FilterOperators 测试
func TestFilterOperators_匹配(t *testing.T) {
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	filtered := FilterOperators(ops, []string{"system_prompt"})
	if len(filtered) != 1 {
		t.Errorf("FilterOperators returned %d, want 1", len(filtered))
	}
}

func TestFilterOperators_不匹配(t *testing.T) {
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"tool_description": {Name: "tool_description"}},
		},
	}
	filtered := FilterOperators(ops, []string{"system_prompt"})
	if len(filtered) != 0 {
		t.Errorf("FilterOperators returned %d, want 0", len(filtered))
	}
}

func TestFilterOperators_空操作符(t *testing.T) {
	filtered := FilterOperators(nil, []string{"system_prompt"})
	if len(filtered) != 0 {
		t.Errorf("FilterOperators returned %d, want 0", len(filtered))
	}
}

func TestFilterOperators_部分匹配(t *testing.T) {
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
		"op2": &mockOperator{
			operatorID: "op2",
			tunables:   map[string]operator.TunableSpec{"tool_description": {Name: "tool_description"}},
		},
	}
	filtered := FilterOperators(ops, []string{"system_prompt"})
	if len(filtered) != 1 {
		t.Errorf("FilterOperators returned %d, want 1", len(filtered))
	}
	if _, ok := filtered["op1"]; !ok {
		t.Error("op1 should be in filtered result")
	}
}

// BaseOptimizer 接口编译期校验 — 使用一个最小实现验证接口完整
type stubOptimizer struct {
	BaseOptimizerMixin
}

func (s *stubOptimizer) Domain() string                                                { return "stub" }
func (s *stubOptimizer) RequiresForwardData() bool                                     { return true }
func (s *stubOptimizer) DefaultTargets() []string                                      { return nil }
func (s *stubOptimizer) Backward(_ context.Context, _ []*signal.EvolutionSignal) error { return nil }
func (s *stubOptimizer) Step() map[schema.UpdateKey]any                                { return nil }

// TestBaseOptimizer_接口完整性 编译期验证 stubOptimizer 实现 BaseOptimizer
func TestBaseOptimizer_接口完整性(t *testing.T) {
	var _ BaseOptimizer = &stubOptimizer{}
}

// BaseOptimizerMixin 新增访问方法测试
func TestBaseOptimizerMixin_Operators(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	m.Bind(ops, []string{"system_prompt"}, nil)

	result := m.Operators()
	if len(result) != 1 {
		t.Errorf("Operators() returned %d, want 1", len(result))
	}
	if _, ok := result["op1"]; !ok {
		t.Error("Operators() should contain op1")
	}
	// 修改副本不应影响原始
	delete(result, "op1")
	if _, ok := m.Operators()["op1"]; !ok {
		t.Error("modifying Operators() copy should not affect original")
	}
}

func TestBaseOptimizerMixin_Targets(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	m.Bind(ops, []string{"system_prompt", "user_prompt"}, nil)

	result := m.Targets()
	if len(result) != 2 || result[0] != "system_prompt" || result[1] != "user_prompt" {
		t.Errorf("Targets() = %v, want [system_prompt, user_prompt]", result)
	}
}

func TestBaseOptimizerMixin_SelectedSignals(t *testing.T) {
	m := &BaseOptimizerMixin{}
	sig1 := &signal.EvolutionSignal{SignalType: "low_score"}
	sig2 := &signal.EvolutionSignal{SignalType: "error"}

	m.SetSelectedSignals([]*signal.EvolutionSignal{sig1, sig2})
	result := m.SelectedSignals()
	if len(result) != 2 {
		t.Errorf("SelectedSignals() returned %d, want 2", len(result))
	}

	// nil 情况
	m.SetSelectedSignals(nil)
	result = m.SelectedSignals()
	if len(result) != 0 {
		t.Errorf("SelectedSignals() after nil should be empty, got %d", len(result))
	}
}
