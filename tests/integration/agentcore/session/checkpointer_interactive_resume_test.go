//go:build integration

package session_test

import (
	"context"
	"testing"

	"errors"
	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CheckpointerInteractiveResumeSuite ValidateInteractiveResume 前置件集成：
// 真实 InMemory/Persistence 检查点后端 + 真实 WorkflowSession + 图侧 pregel.Store 快照。
// 跨子系统：session/checkpointer + session/internal(会话装配) + graph/pregel(图快照) + interaction。
// 对齐 Python: tests/unit_tests/core/session/test_checkpointer*.py 交互恢复场景
// （base.py validate_interactive_resume 修复 6——静默全新起跑 → 显式报错）。
type CheckpointerInteractiveResumeSuite struct {
	isuite.SessionSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCheckpointerInteractiveResumeSuite(t *testing.T) {
	suite.Run(t, new(CheckpointerInteractiveResumeSuite))
}

// ──────────────────────────── 用例 ────────────────────────────

// itWorkflowSession 集成用 InnerSession 实现：真实 config/state + WorkflowIDProvider。
type itWorkflowSession struct {
	sessionID  string
	workflowID string
	cfg        config.SessionConfig
	st         state.SessionState
}

func newWorkflowSession(workflowID string, envs map[string]any) *itWorkflowSession {
	cfg := config.NewSessionConfig(context.Background())
	if envs != nil {
		cfg.SetEnvs(envs)
	}
	return &itWorkflowSession{
		sessionID:  "sess-it-vr",
		workflowID: workflowID,
		cfg:        cfg,
		st: state.NewWorkflowCommitState(
			state.NewInMemoryCommitState(), state.NewInMemoryCommitState(),
			state.NewInMemoryCommitState(), state.NewInMemoryCommitState(),
			nil, "parent1", "node1", true),
	}
}

func (w *itWorkflowSession) Config() config.SessionConfig                     { return w.cfg }
func (w *itWorkflowSession) State() state.SessionState                        { return w.st }
func (w *itWorkflowSession) Tracer() *tracer.Tracer                           { return nil }
func (w *itWorkflowSession) StreamWriterManager() *stream.StreamWriterManager { return nil }
func (w *itWorkflowSession) SessionID() string                                { return w.sessionID }
func (w *itWorkflowSession) Checkpointer() interfaces.Checkpointer            { return nil }
func (w *itWorkflowSession) ActorManager() *stream_actor.ActorManager         { return nil }
func (w *itWorkflowSession) Close() error                                     { return nil }
func (w *itWorkflowSession) WorkflowID() string                               { return w.workflowID }

// resumableGraphState 一份可续跑的图侧快照。
// errInterruptSeed 种子用中断异常（Post 异常分支落盘 io 侧检查点并原样返回）。
var errInterruptSeed = errors.New("interrupt seed")

func resumableGraphState() *pregel.GraphState {
	return &pregel.GraphState{
		PendingNode: map[string]pregel.PendingNode{"n1": {NodeName: "n1"}},
	}
}

// assertPreError 断言 CHECKPOINTER_PRE_WORKFLOW_EXECUTION_ERROR。
func (s *CheckpointerInteractiveResumeSuite) assertPreError(err error) {
	s.Require().Error(err)
	var be *exception.BaseError
	s.Require().True(errors.As(err, &be), "非 BaseError：%v", err)
	s.Equal(exception.StatusCheckpointerPreWorkflowExecutionError, be.Status())
}

// TestInMemory_交互恢复无快照显式报错 全新会话直接交互恢复 → 显式拒绝
// （消除旧静默全新起跑行为）。
func (s *CheckpointerInteractiveResumeSuite) TestInMemory_交互恢复无快照显式报错() {
	cp := checkpointer.NewInMemoryCheckpointer()
	ws := newWorkflowSession("wf-it", nil)
	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.assertPreError(cp.PreWorkflowExecute(context.Background(), ws, ii))
}

// TestInMemory_单组件直调放行 SINGLE_COMPONENT_EXECUTION 标记：io 侧存在即放行，
// 图侧快照缺失不拒绝（execute_single_component 恢复路径依赖）。
func (s *CheckpointerInteractiveResumeSuite) TestInMemory_单组件直调放行() {
	cp := checkpointer.NewInMemoryCheckpointer()
	ctx := context.Background()
	ws := newWorkflowSession("wf-it", map[string]any{constants.SingleComponentExecutionEnvKey: true})
	// 种子：非交互 Pre 建店 + 异常 Post 落 io 侧检查点
	//（Post 异常分支会原样返回该异常，属预期回传，非失败）
	s.Require().NoError(cp.PreWorkflowExecute(ctx, ws, nil))
	if err := cp.PostWorkflowExecute(ctx, ws, nil, errInterruptSeed); err != nil {
		s.Require().ErrorIs(err, errInterruptSeed) // 预期回传
	}
	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(cp.PreWorkflowExecute(ctx, ws, ii))
}

// TestInMemory_图快照可续跑后交互恢复成功 完整链：中断保存 → 图快照落盘 →
// 交互输入恢复成功（恢复路径真实打通）。
func (s *CheckpointerInteractiveResumeSuite) TestInMemory_图快照可续跑后交互恢复成功() {
	cp := checkpointer.NewInMemoryCheckpointer()
	ctx := context.Background()
	ws := newWorkflowSession("wf-it", nil)
	s.Require().NoError(cp.PreWorkflowExecute(ctx, ws, nil))
	// 中断保存（真实链路：引擎把可续跑快照写入图侧 store）
	s.Require().NoError(cp.PostWorkflowExecute(ctx, ws, map[string]any{"__interrupt__": "need_input"}, nil))
	s.Require().NoError(cp.GraphStore().Save(ctx, "sess-it-vr", "wf-it", resumableGraphState()))
	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(cp.PreWorkflowExecute(ctx, ws, ii))
}

// TestInMemory_图快照不可续跑拒绝 图侧快照存在但空（非可续跑）→ 拒绝
// （否则引擎仍会全新起跑）。
func (s *CheckpointerInteractiveResumeSuite) TestInMemory_图快照不可续跑拒绝() {
	cp := checkpointer.NewInMemoryCheckpointer()
	ctx := context.Background()
	ws := newWorkflowSession("wf-it", nil)
	s.Require().NoError(cp.PreWorkflowExecute(ctx, ws, nil))
	s.Require().NoError(cp.PostWorkflowExecute(ctx, ws, map[string]any{"__interrupt__": "need_input"}, nil))
	s.Require().NoError(cp.GraphStore().Save(ctx, "sess-it-vr", "wf-it", &pregel.GraphState{}))
	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.assertPreError(cp.PreWorkflowExecute(ctx, ws, ii))
}

// TestPersistence_交互恢复完整链 Persistence 后端同口径：中断保存 + 图快照 →
// 恢复成功；无图快照 → 拒绝（InMemory/Persistence 行为一致，修复 6 目标）。
func (s *CheckpointerInteractiveResumeSuite) TestPersistence_交互恢复完整链() {
	cp := checkpointer.NewPersistenceCheckpointer(kv.NewInMemoryKVStore())
	ctx := context.Background()
	ws := newWorkflowSession("wf-it", nil)
	s.Require().NoError(cp.PreWorkflowExecute(ctx, ws, nil))
	s.Require().NoError(cp.PostWorkflowExecute(ctx, ws, map[string]any{"__interrupt__": "need_input"}, nil))
	s.Require().NoError(cp.GraphStore().Save(ctx, "sess-it-vr", "wf-it", resumableGraphState()))
	ii, err := interaction.NewInteractiveInput()
	s.Require().NoError(err)
	s.Require().NoError(cp.PreWorkflowExecute(ctx, ws, ii))
}
