package single_dim

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	updater "github.com/uapclaw/uapclaw-go/internal/evolving/updater"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockOptimizer 模拟 BaseOptimizer 的关键方法
// 使用 any 占位，通过接口断言调用
type mockOptimizer struct {
	bindCalled         bool
	bindReturn         int
	bindTargets        []string
	addTrajectoryCalls int
	addTrajectoryOrder []any
	backwardCalled     bool
	backwardSignals    []*signal.EvolutionSignal
	stepReturn         map[schema.UpdateKey]any
	stepCalled         bool
	requireForward     bool
}

func (m *mockOptimizer) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	m.bindCalled = true
	m.bindTargets = targets
	return m.bindReturn
}

func (m *mockOptimizer) RequiresForwardData() bool {
	return m.requireForward
}

func (m *mockOptimizer) AddTrajectory(traj any) {
	m.addTrajectoryCalls++
	m.addTrajectoryOrder = append(m.addTrajectoryOrder, traj)
}

func (m *mockOptimizer) Backward(ctx context.Context, signals []*signal.EvolutionSignal) error {
	m.backwardCalled = true
	m.backwardSignals = signals
	return nil
}

func (m *mockOptimizer) Step() map[schema.UpdateKey]any {
	m.stepCalled = true
	return m.stepReturn
}

// ──────────────────────────── 导出函数 ────────────────────────────

// 对齐 Python: test_bind_delegates_to_optimizer
func TestSingleDimUpdater_Bind委托给优化器(t *testing.T) {
	opt := &mockOptimizer{bindReturn: 3}
	u := NewSingleDimUpdater(opt)

	operators := map[string]operator.Operator{}
	n := u.Bind(operators, []string{"target1"}, nil)

	if n != 3 {
		t.Errorf("Bind returned %d, want 3", n)
	}
	if !opt.bindCalled {
		t.Error("optimizer.Bind was not called")
	}
}

// 对齐 Python: test_bind_with_none_targets
func TestSingleDimUpdater_Bind空目标使用Config(t *testing.T) {
	opt := &mockOptimizer{bindReturn: 2}
	u := NewSingleDimUpdater(opt)

	operators := map[string]operator.Operator{}
	config := map[string]any{"targets": []string{"system_prompt"}}
	u.Bind(operators, nil, config)

	if !opt.bindCalled {
		t.Error("optimizer.Bind was not called")
	}
}

// 对齐 Python: test_update_calls_optimizer_chain
func TestSingleDimUpdater_Update调用优化器链路(t *testing.T) {
	expectedUpdates := map[schema.UpdateKey]any{
		schema.UpdateKey{"op1", "target"}: "new_value",
	}
	opt := &mockOptimizer{stepReturn: expectedUpdates}
	u := NewSingleDimUpdater(opt)

	traj1 := "trajectory1"
	traj2 := "trajectory2"

	result, err := u.Update(context.Background(), []any{traj1, traj2}, []*dataset.EvaluatedCase{}, map[string]any{})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if opt.addTrajectoryCalls != 2 {
		t.Errorf("add_trajectory called %d times, want 2", opt.addTrajectoryCalls)
	}
	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if !opt.stepCalled {
		t.Error("step was not called")
	}
	if len(result) != 1 {
		t.Errorf("Update returned %d updates, want 1", len(result))
	}
}

// 对齐 Python: test_update_empty_trajectories
func TestSingleDimUpdater_Update空轨迹(t *testing.T) {
	opt := &mockOptimizer{stepReturn: map[schema.UpdateKey]any{}}
	u := NewSingleDimUpdater(opt)

	_, err := u.Update(context.Background(), nil, []*dataset.EvaluatedCase{}, map[string]any{})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if opt.addTrajectoryCalls != 0 {
		t.Errorf("add_trajectory called %d times, want 0", opt.addTrajectoryCalls)
	}
	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if !opt.stepCalled {
		t.Error("step was not called")
	}
}

// 对齐 Python: test_get_state_returns_empty_dict
func TestSingleDimUpdater_GetState返回空(t *testing.T) {
	opt := &mockOptimizer{}
	u := NewSingleDimUpdater(opt)

	state := u.GetState()
	if len(state) != 0 {
		t.Errorf("GetState returned %v, want empty map", state)
	}
}

// 对齐 Python: test_load_state_is_noop
func TestSingleDimUpdater_LoadState无操作(t *testing.T) {
	opt := &mockOptimizer{}
	u := NewSingleDimUpdater(opt)

	// 不应 panic
	u.LoadState(map[string]any{"key": "value"})
}

// 对齐 Python: test_update_preserves_trajectory_order
func TestSingleDimUpdater_Update保持轨迹顺序(t *testing.T) {
	opt := &mockOptimizer{stepReturn: map[schema.UpdateKey]any{}}
	u := NewSingleDimUpdater(opt)

	traj1 := "traj_1"
	traj2 := "traj_2"
	_, err := u.Update(context.Background(), []any{traj1, traj2}, []*dataset.EvaluatedCase{}, map[string]any{})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if len(opt.addTrajectoryOrder) != 2 {
		t.Fatalf("add_trajectory called %d times, want 2", len(opt.addTrajectoryOrder))
	}
	if opt.addTrajectoryOrder[0] != traj1 {
		t.Errorf("first trajectory = %v, want %v", opt.addTrajectoryOrder[0], traj1)
	}
	if opt.addTrajectoryOrder[1] != traj2 {
		t.Errorf("second trajectory = %v, want %v", opt.addTrajectoryOrder[1], traj2)
	}
}

// 对齐 Python: test_update_returns_updates
func TestSingleDimUpdater_Update返回更新(t *testing.T) {
	expectedUpdates := map[schema.UpdateKey]any{
		schema.UpdateKey{"op1", "prompt"}: "new prompt",
	}
	opt := &mockOptimizer{stepReturn: expectedUpdates}
	u := NewSingleDimUpdater(opt)

	result, err := u.Update(context.Background(), nil, []*dataset.EvaluatedCase{}, map[string]any{})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	val, ok := result[schema.UpdateKey{"op1", "prompt"}]
	if !ok {
		t.Error("result missing (op1, prompt) key")
	} else if val != "new prompt" {
		t.Errorf("result[(op1,prompt)] = %v, want %q", val, "new prompt")
	}
}

// 对齐 Python: test_process_uses_signal_first_flow
func TestSingleDimUpdater_Process使用信号优先流程(t *testing.T) {
	expectedUpdates := map[schema.UpdateKey]any{
		schema.UpdateKey{"op1", "prompt"}: "new prompt",
	}
	opt := &mockOptimizer{stepReturn: expectedUpdates}
	u := NewSingleDimUpdater(opt)

	traj1 := "trajectory1"
	traj2 := "trajectory2"
	signals := []*signal.EvolutionSignal{
		{SignalType: "low_score", Section: "Troubleshooting", Excerpt: "score=0.00"},
	}

	result, err := u.Process(context.Background(), []any{traj1, traj2}, signals, map[string]any{})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if opt.addTrajectoryCalls != 2 {
		t.Errorf("add_trajectory called %d times, want 2", opt.addTrajectoryCalls)
	}
	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if len(opt.backwardSignals) != 1 || opt.backwardSignals[0].SignalType != "low_score" {
		t.Errorf("backward signals = %v, want 1 low_score signal", opt.backwardSignals)
	}
	if !opt.stepCalled {
		t.Error("step was not called")
	}
	val, ok := result[schema.UpdateKey{"op1", "prompt"}]
	if !ok {
		t.Error("result missing (op1, prompt) key")
	} else if val != "new prompt" {
		t.Errorf("result[(op1,prompt)] = %v, want %q", val, "new prompt")
	}
}

// 对齐 Python: test_update_adapts_evaluated_cases_to_process
func TestSingleDimUpdater_Update适配EvaluatedCases到Process(t *testing.T) {
	expectedUpdates := map[schema.UpdateKey]any{
		schema.UpdateKey{"op1", "prompt"}: "new prompt",
	}
	opt := &mockOptimizer{stepReturn: expectedUpdates}
	u := NewSingleDimUpdater(opt)

	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "pred"})
	ec.SetScore(0.0)

	result, err := u.Update(context.Background(), nil, []*dataset.EvaluatedCase{ec}, map[string]any{})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if len(opt.backwardSignals) != 1 {
		t.Errorf("backward received %d signals, want 1", len(opt.backwardSignals))
	}
	val, ok := result[schema.UpdateKey{"op1", "prompt"}]
	if !ok {
		t.Error("result missing (op1, prompt) key")
	} else if val != "new prompt" {
		t.Errorf("result[(op1,prompt)] = %v, want %q", val, "new prompt")
	}
}

// 对齐 Python: test_update_respects_score_threshold_from_config
func TestSingleDimUpdater_Update尊重ScoreThreshold(t *testing.T) {
	expectedUpdates := map[schema.UpdateKey]any{
		schema.UpdateKey{"op1", "prompt"}: "new prompt",
	}
	opt := &mockOptimizer{stepReturn: expectedUpdates}
	u := NewSingleDimUpdater(opt)

	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	highScore := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "good"})
	highScore.SetScore(1.0)
	lowScore := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	lowScore.SetScore(0.0)

	threshold := 1.0
	_, err := u.Update(
		context.Background(),
		nil,
		[]*dataset.EvaluatedCase{highScore, lowScore},
		map[string]any{"score_threshold": threshold},
	)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if !opt.backwardCalled {
		t.Error("backward was not called")
	}
	if len(opt.backwardSignals) != 1 {
		t.Errorf("backward received %d signals, want 1 (filtered by threshold)", len(opt.backwardSignals))
	}
	if opt.backwardSignals[0].SignalType != "low_score" {
		t.Errorf("signal type = %q, want %q", opt.backwardSignals[0].SignalType, "low_score")
	}
}

// 对齐 Python: test_process_is_accepted_by_protocol_mock
// 验证 SingleDimUpdater 可被用作 Updater 接口的实现
func TestSingleDimUpdater_实现Updater接口(t *testing.T) {
	opt := &mockOptimizer{stepReturn: map[schema.UpdateKey]any{}}
	u := NewSingleDimUpdater(opt)

	// 编译期验证
	var _ updater.Updater = u
	_ = u.Bind
	_ = u.RequiresForwardData
	_ = u.Update
	_ = u.Process
	_ = u.GetState
	_ = u.LoadState
}

// 补充测试: optimizer 为 nil 时的默认行为
func TestSingleDimUpdater_NilOptimizer(t *testing.T) {
	u := NewSingleDimUpdater(nil)

	// Bind 应返回 0（nil 无法断言为 binder）
	n := u.Bind(nil, nil, nil)
	if n != 0 {
		t.Errorf("Bind with nil optimizer returned %d, want 0", n)
	}

	// RequiresForwardData 应返回 true（默认值）
	if !u.RequiresForwardData() {
		t.Error("RequiresForwardData with nil optimizer should return true")
	}

	// Process 应返回空 map
	result, err := u.Process(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Process returned %d updates, want 0", len(result))
	}
}

// 补充测试: config["targets"] 类型不匹配时使用 nil
func TestSingleDimUpdater_BindConfigTargets类型不匹配(t *testing.T) {
	opt := &mockOptimizer{bindReturn: 1}
	u := NewSingleDimUpdater(opt)

	// config["targets"] 为非 []string 类型
	config := map[string]any{"targets": 123}
	n := u.Bind(nil, nil, config)

	if n != 1 {
		t.Errorf("Bind returned %d, want 1", n)
	}
	// targets 应为 nil（因类型断言失败）
	if opt.bindTargets != nil {
		t.Errorf("bindTargets = %v, want nil (type assertion failed)", opt.bindTargets)
	}
}

// 补充测试: config 中无 targets 键
func TestSingleDimUpdater_BindConfig无Targets键(t *testing.T) {
	opt := &mockOptimizer{bindReturn: 1}
	u := NewSingleDimUpdater(opt)

	config := map[string]any{"other_key": "value"}
	n := u.Bind(nil, nil, config)

	if n != 1 {
		t.Errorf("Bind returned %d, want 1", n)
	}
}

// 补充测试: config 为 nil
func TestSingleDimUpdater_BindConfigNil(t *testing.T) {
	opt := &mockOptimizer{bindReturn: 1}
	u := NewSingleDimUpdater(opt)

	n := u.Bind(nil, []string{"system_prompt"}, nil)

	if n != 1 {
		t.Errorf("Bind returned %d, want 1", n)
	}
	if opt.bindTargets == nil || opt.bindTargets[0] != "system_prompt" {
		t.Errorf("bindTargets = %v, want [system_prompt]", opt.bindTargets)
	}
}

// 补充测试: config 中 score_threshold 为非 float64 类型
func TestSingleDimUpdater_UpdateScoreThreshold类型不匹配(t *testing.T) {
	opt := &mockOptimizer{stepReturn: map[schema.UpdateKey]any{}}
	u := NewSingleDimUpdater(opt)

	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	ec.SetScore(0.0)

	// score_threshold 为字符串类型，应不生效
	_, err := u.Update(
		context.Background(),
		nil,
		[]*dataset.EvaluatedCase{ec},
		map[string]any{"score_threshold": "1.0"},
	)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	// score_threshold 为 nil → 不过滤，1 个信号
	if len(opt.backwardSignals) != 1 {
		t.Errorf("backward received %d signals, want 1", len(opt.backwardSignals))
	}
}

// 补充测试: config 为 nil 时 Update 不 panic
func TestSingleDimUpdater_UpdateConfigNil(t *testing.T) {
	opt := &mockOptimizer{stepReturn: map[schema.UpdateKey]any{}}
	u := NewSingleDimUpdater(opt)

	case_ := dataset.NewCase(
		map[string]any{"query": "q"},
		map[string]any{"answer": "a"},
	)
	ec := dataset.NewEvaluatedCase(*case_, map[string]any{"output": "bad"})
	ec.SetScore(0.0)

	_, err := u.Update(context.Background(), nil, []*dataset.EvaluatedCase{ec}, nil)
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
}
