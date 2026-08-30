package memory_call

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	memoryop "github.com/uapclaw/uapclaw-go/internal/agentcore/operator/memory_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMemoryOptimizerBase_Domain 测试 Domain() 返回 "memory"。
func TestMemoryOptimizerBase_Domain(t *testing.T) {
	base := &MemoryOptimizerBase{}
	assert.Equal(t, "memory", base.Domain())
}

// TestMemoryOptimizerBase_DefaultTargets 测试 DefaultTargets() 返回 enabled 和 max_retries。
func TestMemoryOptimizerBase_DefaultTargets(t *testing.T) {
	base := &MemoryOptimizerBase{}
	targets := base.DefaultTargets()
	assert.Equal(t, []string{"enabled", "max_retries"}, targets)
}

// TestMemoryOptimizerBase_RequiresForwardData 测试 RequiresForwardData() 返回 true。
func TestMemoryOptimizerBase_RequiresForwardData(t *testing.T) {
	base := &MemoryOptimizerBase{}
	assert.True(t, base.RequiresForwardData())
}

// TestMemoryOptimizerBase_Bind_匹配MemoryCallOperator 测试 Bind 正确过滤 MemoryCallOperator。
func TestMemoryOptimizerBase_Bind_匹配MemoryCallOperator(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()

	operators := map[string]operator.Operator{
		"memory_call": memOp,
	}

	count := base.Bind(operators, nil, nil)
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, len(base.Parameters()))
}

// TestMemoryOptimizerBase_Bind_使用默认目标 测试 Bind 使用 DefaultTargets。
func TestMemoryOptimizerBase_Bind_使用默认目标(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()

	operators := map[string]operator.Operator{
		"memory_call": memOp,
	}

	// targets 为 nil 时应使用 DefaultTargets
	count := base.Bind(operators, nil, nil)
	assert.Equal(t, 1, count)
}

// TestMemoryOptimizerBase_Bind_无匹配Operator 测试 Bind 无匹配时返回 0。
func TestMemoryOptimizerBase_Bind_无匹配Operator(t *testing.T) {
	base := &MemoryOptimizerBase{}

	// 空 Operator 映射
	operators := map[string]operator.Operator{}

	count := base.Bind(operators, nil, nil)
	assert.Equal(t, 0, count)
}

// TestMemoryOptimizerBase_Backward 测试 Backward 模板方法流程。
func TestMemoryOptimizerBase_Backward(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()
	operators := map[string]operator.Operator{"memory_call": memOp}
	base.Bind(operators, nil, nil)

	err := base.Backward(context.Background(), nil)
	assert.NoError(t, err)
	// 验证 SelectedSignals 已设置（空信号列表）
	selected := base.SelectedSignals()
	assert.Equal(t, 0, len(selected))
}

// TestMemoryOptimizerBase_Backward_未绑定参数时panic 测试 Backward 未绑定参数时 panic。
func TestMemoryOptimizerBase_Backward_未绑定参数时panic(t *testing.T) {
	defer func() {
		r := recover()
		assert.NotNil(t, r, "期望 Backward 未绑定时 panic")
	}()
	base := &MemoryOptimizerBase{}
	base.Backward(context.Background(), nil)
}

// TestMemoryOptimizerBase_Backward_信号选择 测试 Backward 正确选择信号。
func TestMemoryOptimizerBase_Backward_信号选择(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()
	operators := map[string]operator.Operator{"memory_call": memOp}
	base.Bind(operators, nil, nil)

	sig1 := signal.MakeEvolutionSignal("execution_failure", "Test", "excerpt1")
	sig2 := signal.MakeEvolutionSignal("low_score", "Test", "excerpt2")
	err := base.Backward(context.Background(), []*signal.EvolutionSignal{sig1, sig2})
	assert.NoError(t, err)
	selected := base.SelectedSignals()
	assert.Equal(t, 2, len(selected))
}

// TestMemoryOptimizerBase_Step 测试 Step 模板方法流程。
func TestMemoryOptimizerBase_Step(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()
	operators := map[string]operator.Operator{"memory_call": memOp}
	base.Bind(operators, nil, nil)

	updates := base.Step()
	assert.NotNil(t, updates)
	assert.Equal(t, 0, len(updates))
}

// TestMemoryOptimizerBase_Step_未绑定参数时panic 测试 Step 未绑定参数时 panic。
func TestMemoryOptimizerBase_Step_未绑定参数时panic(t *testing.T) {
	defer func() {
		r := recover()
		assert.NotNil(t, r, "期望 Step 未绑定时 panic")
	}()
	base := &MemoryOptimizerBase{}
	base.Step()
}

// TestMemoryOptimizerBase_Step_清空轨迹 测试 Step 后轨迹被清空。
func TestMemoryOptimizerBase_Step_清空轨迹(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()
	operators := map[string]operator.Operator{"memory_call": memOp}
	base.Bind(operators, nil, nil)

	base.AddTrajectory(&trajectory.Trajectory{ExecutionID: "test"})
	assert.Equal(t, 1, len(base.GetTrajectories()))

	base.Step()
	assert.Equal(t, 0, len(base.GetTrajectories()))
}

// TestMemoryOptimizerBase_AddTrajectoryAndGet 测试 AddTrajectory/GetTrajectories/ClearTrajectories。
func TestMemoryOptimizerBase_AddTrajectoryAndGet(t *testing.T) {
	base := &MemoryOptimizerBase{}

	// 初始为空
	trajs := base.GetTrajectories()
	assert.Equal(t, 0, len(trajs))

	// 添加一条轨迹
	traj := &trajectory.Trajectory{
		ExecutionID: "test-exec",
		SessionID:   "test-session",
		Source:      "test",
		Steps:       nil,
	}
	base.AddTrajectory(traj)
	trajs = base.GetTrajectories()
	assert.Equal(t, 1, len(trajs))
	assert.Equal(t, "test-exec", trajs[0].ExecutionID)

	// 清空
	base.ClearTrajectories()
	trajs = base.GetTrajectories()
	assert.Equal(t, 0, len(trajs))
}

// TestMemoryOptimizerBase_Parameters 测试 Parameters 返回副本。
func TestMemoryOptimizerBase_Parameters(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()

	operators := map[string]operator.Operator{
		"memory_call": memOp,
	}
	base.Bind(operators, nil, nil)

	params := base.Parameters()
	assert.Equal(t, 1, len(params))
	assert.Contains(t, params, "memory_call")
	assert.Equal(t, "memory_call", params["memory_call"].OperatorID)
}

// TestMemoryOptimizerBase_SelectSignals 测试 SelectSignals 默认保留全部信号。
func TestMemoryOptimizerBase_SelectSignals(t *testing.T) {
	base := &MemoryOptimizerBase{}

	signals := []*signal.EvolutionSignal{
		signal.MakeEvolutionSignal("execution_failure", "Troubleshooting", "test excerpt"),
		signal.MakeEvolutionSignal("low_score", "Examples", "another excerpt"),
	}

	selected := base.SelectSignals(signals)
	assert.Equal(t, 2, len(selected))
	assert.Equal(t, "execution_failure", selected[0].SignalType)
	assert.Equal(t, "low_score", selected[1].SignalType)
}

// TestMemoryOptimizerBase_Step_绑定后无梯度 测试绑定后 Step 返回空 map（_backward 是 pass）。
func TestMemoryOptimizerBase_Step_绑定后无梯度(t *testing.T) {
	base := &MemoryOptimizerBase{}
	memOp := memoryop.NewMemoryCallOperator()

	operators := map[string]operator.Operator{
		"memory_call": memOp,
	}
	base.Bind(operators, nil, nil)

	// Backward 是 pass，不写梯度
	err := base.Backward(context.Background(), nil)
	assert.NoError(t, err)

	// Step 应返回空 map
	updates := base.Step()
	assert.Equal(t, 0, len(updates))
}

// ──────────────────────────── 非导出函数 ────────────────────────────
