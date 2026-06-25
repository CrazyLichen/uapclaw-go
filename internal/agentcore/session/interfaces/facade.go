package interfaces

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionFacade 门面会话共有接口。
//
// 对应 Python agent.Session 和 node.Session 的共有方法集。
// ReActAgent 的 invoke/stream 签名使用此接口，而非具体类型。
// 需要特定门面独有方法时，直接断言具体类型：
//   - Agent 生命周期：sess.(*session.Session) 获取 PreRun/CloseStream/Commit/StreamIterator
//   - Node 独有方法：sess.(*NodeSessionFacade) 获取 GetWorkflowID/Trace 等
type SessionFacade interface {
	// GetSessionID 获取会话唯一标识
	GetSessionID() string
	// UpdateState 更新会话状态
	UpdateState(data map[string]any)
	// GetState 获取会话状态值
	GetState(key state.StateKey) (any, error)
	// DumpState 导出完整会话状态
	DumpState() map[string]any
	// WriteStream 写入流数据
	WriteStream(ctx context.Context, data any) error
	// WriteCustomStream 写入自定义流数据
	WriteCustomStream(ctx context.Context, data any) error
	// GetEnv 获取环境变量
	GetEnv(key string, defaultValue ...any) any
	// Interact 交互（等待用户输入）
	Interact(ctx context.Context, value any) error
}
