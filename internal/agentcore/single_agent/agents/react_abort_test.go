package agents

import (
	"context"
	"testing"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// abortTestSession 满足 SessionFacade 的最小会话桩（记录 UpdateState 结果）。
type abortTestSession struct {
	// sessionID 会话标识
	sessionID string
	// stateData 最近一次 UpdateState 写入的数据
	stateData map[string]any
}

// cancelAwareContextEngine 记录 SaveContexts 收到 ctx 的取消状态。
//
// 复用 react_agent_test.go 的 fakeContextEngine 提供其余接口方法，
// 只覆写 SaveContexts：真实 PersistenceCheckpointer 在 ctx 已取消时写库必失败，
// 这里用 sawCanceledCtx 复现同一行为。
type cancelAwareContextEngine struct {
	fakeContextEngine
	// saveCalls SaveContexts 调用次数
	saveCalls int
	// modelUpdates 记录 UpdateModelContext 调用参数
	modelUpdates [][2]any
	// recoveryCalls RecoverFromModelContextOverflow 调用次数
	recoveryCalls int
	// recoveryResult RecoverFromModelContextOverflow 返回值
	recoveryResult bool
	// sawCanceledCtx SaveContexts 是否收到了已取消的 ctx
	sawCanceledCtx bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSaveContextsDetached_取消态仍落盘 验证取消后持久化不再被取消信号打断。
func TestSaveContextsDetached_取消态仍落盘(t *testing.T) {
	engine := &cancelAwareContextEngine{}
	agent := &ReActAgent{contextEngine: engine}
	sess := &abortTestSession{sessionID: "sess-detached"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	agent.saveContextsDetached(ctx, sess)

	if engine.saveCalls != 1 {
		t.Fatalf("SaveContexts 调用次数 = %d, want 1", engine.saveCalls)
	}
	if engine.sawCanceledCtx {
		t.Fatalf("取消态路径仍把已取消的 ctx 传给 SaveContexts，写库会失败")
	}
}

// TestSaveContexts_保留原语义 验证普通路径仍透传调用方 ctx（不误用 detached）。
func TestSaveContexts_保留原语义(t *testing.T) {
	engine := &cancelAwareContextEngine{}
	agent := &ReActAgent{contextEngine: engine}
	sess := &abortTestSession{sessionID: "sess-normal"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	agent.saveContexts(ctx, sess)

	if !engine.sawCanceledCtx {
		t.Fatalf("普通 saveContexts 应原样透传调用方 ctx")
	}
}

// TestHandleContextAbort_取消态仍落盘 验证中断路径的清洗结果能写回。
func TestHandleContextAbort_取消态仍落盘(t *testing.T) {
	engine := &cancelAwareContextEngine{}
	agent := &ReActAgent{contextEngine: engine}
	sess := &abortTestSession{sessionID: "sess-abort"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	agent.handleContextAbort(ctx, sess, "[Request cancelled by user]")

	if engine.saveCalls != 1 {
		t.Fatalf("SaveContexts 调用次数 = %d, want 1", engine.saveCalls)
	}
	if engine.sawCanceledCtx {
		t.Fatalf("中断路径必须用脱离取消的 ctx 落盘（等价 asyncio.shield）")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SaveContexts 覆写：记录 ctx 取消状态，取消时按真实 checkpointer 语义返回错误。
func (e *cancelAwareContextEngine) SaveContexts(
	ctx context.Context, sess sessioninterfaces.SessionFacade, _ []string,
) (map[string]any, error) {
	e.saveCalls++
	if err := ctx.Err(); err != nil {
		e.sawCanceledCtx = true
		return nil, err
	}
	return map[string]any{"saved": true}, nil
}

// RecoverFromModelContextOverflow 记录溢出自愈调用（B-07 用例断言用）。
func (e *cancelAwareContextEngine) RecoverFromModelContextOverflow(
	_ context.Context, _ sessioninterfaces.SessionFacade, _ string, _ bool, _ int,
) bool {
	e.recoveryCalls++
	return e.recoveryResult
}

// UpdateModelContext 记录热切换调用（可选能力，对齐引擎同名方法）。
func (e *cancelAwareContextEngine) UpdateModelContext(modelName string, tokens int) {
	e.modelUpdates = append(e.modelUpdates, [2]any{modelName, tokens})
}

// GetSessionID 获取会话标识。
func (s *abortTestSession) GetSessionID() string { return s.sessionID }

// UpdateState 记录写入的状态数据。
func (s *abortTestSession) UpdateState(data map[string]any) { s.stateData = data }

// GetState 读取状态（本用例不使用）。
func (s *abortTestSession) GetState(_ state.StateKey) (any, error) { return nil, nil }

// DumpState 导出完整状态。
func (s *abortTestSession) DumpState() map[string]any { return s.stateData }

// WriteStream 写入流数据（本用例不产生流）。
func (s *abortTestSession) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 写入自定义流数据（本用例不产生流）。
func (s *abortTestSession) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 读取环境变量（无配置时返回默认值）。
func (s *abortTestSession) GetEnv(_ string, defaultValue ...any) any {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// Interact 等待用户输入（本用例不交互）。
func (s *abortTestSession) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 清空会话状态。
func (s *abortTestSession) ClearSession(_ context.Context) {}

// 确保结构体满足接口（防止接口演进后静默退化）。
var (
	_ sessioninterfaces.SessionFacade = (*abortTestSession)(nil)
	_ ceinterface.ContextEngine       = (*cancelAwareContextEngine)(nil)
)
