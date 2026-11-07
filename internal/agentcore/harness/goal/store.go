// 本文件对应 Python openjiuwen/harness/goal/store.py，
// 用 Session state 作为 GoalRecord 的唯一持久化真源。
// 禁止 sidecar JSON / SQLite / history 作为替代状态源（上游约束）。
package goal

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalSession Goal 状态持久化所需的会话能力。
// Python: store.py 直接持有 Session（getattr 探测可选 commit）；Go 收窄为 4 方法接口。
type GoalSession interface {
	// GetSessionID 返回会话标识
	GetSessionID() string
	// GetState 读取状态
	GetState(key state.StateKey) (any, error)
	// UpdateState 写入状态
	UpdateState(data map[string]any)
	// Commit 显式提交到检查点
	Commit(ctx context.Context) error
}

// SessionGoalStore 会话级目标持久化。
// Python: SessionGoalStore (store.py L25)
type SessionGoalStore struct {
	// session 绑定的会话
	session GoalSession
}

// ──────────────────────────── 常量 ────────────────────────────

// SessionGoalRecordKey 目标记录在 Session state 中的键。
// Python: SESSION_GOAL_RECORD_KEY (store.py L22)
const SessionGoalRecordKey = "harness.goal.record"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionGoalStore 绑定会话创建存储。
// Python: SessionGoalStore(session) (store.py L28)
func NewSessionGoalStore(session GoalSession) *SessionGoalStore {
	return &SessionGoalStore{session: session}
}

// SessionID 返回绑定会话标识。
// Python: SessionGoalStore.session_id (store.py L31)
func (s *SessionGoalStore) SessionID() string {
	if s.session == nil {
		return ""
	}
	return s.session.GetSessionID()
}

// Load 读取当前目标记录；类型不符或还原失败时清空并返回 nil。
// Python: SessionGoalStore.load (store.py L35)
func (s *SessionGoalStore) Load() *GoalRecord {
	if s.session == nil {
		return nil
	}
	raw, err := s.session.GetState(state.StringKey(SessionGoalRecordKey))
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Err(err).
			Str("event_type", "GOAL_STORE_GET_STATE_ERROR").
			Str("session_id", s.SessionID()).
			Msg("failed to read goal state")
		return nil
	}
	if raw == nil {
		return nil
	}
	data, ok := raw.(map[string]any)
	if !ok {
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "GOAL_STORE_INVALID_TYPE").
			Str("session_id", s.SessionID()).
			Str("state_type", pyTypeName(raw)).
			Msg("invalid goal state type in session, clearing")
		s.Clear()
		return nil
	}
	record, err := FromGoalRecordDict(data)
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Err(err).
			Str("event_type", "GOAL_STORE_DESERIALIZE_ERROR").
			Str("session_id", s.SessionID()).
			Msg("failed to deserialize goal state, clearing")
		s.Clear()
		return nil
	}
	return record
}

// Save 持久化目标记录（dict 形态、零值按删键处理）。
// Python: SessionGoalStore.save (store.py L59)
func (s *SessionGoalStore) Save(record *GoalRecord) {
	if s.session == nil || record == nil {
		return
	}
	s.session.UpdateState(map[string]any{SessionGoalRecordKey: record.ToDict()})
}

// Clear 从会话状态中移除目标记录。
// Python: SessionGoalStore.clear (store.py L63)
func (s *SessionGoalStore) Clear() {
	if s.session == nil {
		return
	}
	s.session.UpdateState(map[string]any{SessionGoalRecordKey: nil})
}

// Commit 显式落盘。
// Python: SessionGoalStore.commit (store.py L67)
func (s *SessionGoalStore) Commit(ctx context.Context) error {
	if s.session == nil {
		return nil
	}
	return s.session.Commit(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pyTypeName 返回 Python 风格的轻量类型名，仅用于日志。
func pyTypeName(value any) string {
	switch value.(type) {
	case nil:
		return "NoneType"
	case string:
		return "str"
	case bool:
		return "bool"
	case int, int64:
		return "int"
	case float64:
		return "float"
	case []any:
		return "list"
	case map[string]any:
		return "dict"
	default:
		return "object"
	}
}
