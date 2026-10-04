//go:build integration

package task_loop

import (
	"testing"

	"github.com/stretchr/testify/suite"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	task_loop "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeEvaluator 用于测试的模拟停止条件评估器
type fakeEvaluator struct {
	// shouldStop 是否应该停止
	shouldStop bool
	// name 评估器名称
	name string
}

// LoopCoordinatorSuite 循环协调器集成测试。
// 对照 Python: tests/unit_tests/harness/test_loop_coordinator.py
type LoopCoordinatorSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewLoopCoordinator_无评估器 无评估器时创建协调器
func (s *LoopCoordinatorSuite) TestNewLoopCoordinator_无评估器() {
	lc := task_loop.NewLoopCoordinator(nil)
	s.NotNil(lc)
	s.Equal(0, lc.Iteration())
	s.Equal(0, lc.TokenUsage())
	s.False(lc.IsAborted())
}

// TestNewLoopCoordinator_有评估器 传入评估器列表创建协调器
func (s *LoopCoordinatorSuite) TestNewLoopCoordinator_有评估器() {
	eval := &fakeEvaluator{shouldStop: false, name: "fake"}
	lc := task_loop.NewLoopCoordinator([]task_loop.StopConditionEvaluator{eval})
	s.NotNil(lc)
	s.Equal(1, len(lc.Evaluators()))
}

// TestLoopCoordinator_迭代计数 验证 IncrementIteration + Iteration
func (s *LoopCoordinatorSuite) TestLoopCoordinator_迭代计数() {
	lc := task_loop.NewLoopCoordinator(nil)
	s.Equal(0, lc.Iteration())

	lc.IncrementIteration()
	s.Equal(1, lc.Iteration())

	lc.IncrementIteration()
	lc.IncrementIteration()
	s.Equal(3, lc.Iteration())
}

// TestLoopCoordinator_TokenUsage 验证 AddTokenUsage + TokenUsage
func (s *LoopCoordinatorSuite) TestLoopCoordinator_TokenUsage() {
	lc := task_loop.NewLoopCoordinator(nil)
	s.Equal(0, lc.TokenUsage())

	lc.AddTokenUsage(100)
	s.Equal(100, lc.TokenUsage())

	lc.AddTokenUsage(50)
	s.Equal(150, lc.TokenUsage())

	// 非正数不累加
	lc.AddTokenUsage(0)
	lc.AddTokenUsage(-10)
	s.Equal(150, lc.TokenUsage())
}

// TestLoopCoordinator_Abort 验证 RequestAbort + IsAborted
func (s *LoopCoordinatorSuite) TestLoopCoordinator_Abort() {
	lc := task_loop.NewLoopCoordinator(nil)
	s.False(lc.IsAborted())

	lc.RequestAbort()
	s.True(lc.IsAborted())
}

// TestLoopCoordinator_GetState 验证 ExportState 返回 LoopCoordinatorState
func (s *LoopCoordinatorSuite) TestLoopCoordinator_GetState() {
	eval := &fakeEvaluator{shouldStop: false, name: "fake"}
	lc := task_loop.NewLoopCoordinator([]task_loop.StopConditionEvaluator{eval})
	lc.IncrementIteration()
	lc.AddTokenUsage(200)

	state := lc.ExportState()
	s.Equal(1, state.Iteration)
	s.Equal(200, state.TokenUsage)
	s.Empty(state.StopReason)
	s.NotNil(state.EvaluatorStates)
}

// TestLoopCoordinator_ShouldContinue_无评估器 无评估器时始终返回 true
func (s *LoopCoordinatorSuite) TestLoopCoordinator_ShouldContinue_无评估器() {
	lc := task_loop.NewLoopCoordinator(nil)
	s.True(lc.ShouldContinue())
}

// TestLoopCoordinator_ShouldContinue_评估器停止 有评估器返回停止时 ShouldContinue 返回 false
func (s *LoopCoordinatorSuite) TestLoopCoordinator_ShouldContinue_评估器停止() {
	eval := &fakeEvaluator{shouldStop: true, name: "FakeStop"}
	lc := task_loop.NewLoopCoordinator([]task_loop.StopConditionEvaluator{eval})
	s.False(lc.ShouldContinue())
	s.Equal("FakeStop", lc.StopReason())
}

// TestLoopCoordinator_Reset 验证 Reset 重置迭代和 token 用量
func (s *LoopCoordinatorSuite) TestLoopCoordinator_Reset() {
	lc := task_loop.NewLoopCoordinator(nil)
	lc.IncrementIteration()
	lc.IncrementIteration()
	lc.AddTokenUsage(300)
	lc.RequestAbort()

	lc.Reset()
	s.Equal(0, lc.Iteration())
	s.Equal(0, lc.TokenUsage())
	s.False(lc.IsAborted())
}

// TestLoopCoordinator_Abort与DeepLoopEventType关联 验证 abort 协调器状态与 schema 层事件类型一致
func (s *LoopCoordinatorSuite) TestLoopCoordinator_Abort与DeepLoopEventType关联() {
	lc := task_loop.NewLoopCoordinator(nil)
	s.False(lc.IsAborted())

	lc.RequestAbort()
	s.True(lc.IsAborted())

	// abort 类事件在 schema 层有对应枚举
	s.Equal("abort", hschema.DeepLoopEventTypeAbort.String())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestLoopCoordinatorSuite 运行 LoopCoordinatorSuite
func TestLoopCoordinatorSuite(t *testing.T) {
	suite.Run(t, new(LoopCoordinatorSuite))
}

// ShouldStop 实现 StopConditionEvaluator 接口
func (e *fakeEvaluator) ShouldStop(_ task_loop.StopEvaluationContext) bool {
	return e.shouldStop
}

// Name 实现 StopConditionEvaluator 接口
func (e *fakeEvaluator) Name() string {
	return e.name
}

// ExportState 实现 StopConditionEvaluator 接口
func (e *fakeEvaluator) ExportState() map[string]any {
	return map[string]any{"should_stop": e.shouldStop}
}

// ImportState 实现 StopConditionEvaluator 接口
func (e *fakeEvaluator) ImportState(_ map[string]any) {}

// Reset 实现 StopConditionEvaluator 接口
func (e *fakeEvaluator) Reset() {}
