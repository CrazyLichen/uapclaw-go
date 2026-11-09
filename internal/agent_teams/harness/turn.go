package harness

import (
	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberTurn 团队成员一次工作的轨迹身份。
// Python: MemberTurn (agent_teams/harness/turn.py L52-94)
type MemberTurn struct {
	// TurnID 该 turn 内所有 span 共享的稳定标识
	TurnID string
	// TurnNumber 成员会话内的 1-based 轮次号
	TurnNumber int
}

// TurnStateStore turn 身份所需的最小会话状态面。
// Python: TurnStateStore Protocol (turn.py L40-49)
type TurnStateStore interface {
	// GetState 读取状态
	GetState(key state.StateKey) (any, error)
	// UpdateState 浅合并状态
	UpdateState(data map[string]any)
}

// ──────────────────────────── 常量 ────────────────────────────

// memberTurnStateKey 成员 turn 在 session state 中的键。
// 与 deepagent blob 分开命名：pause/abort 回滚 DeepAgentState 时不能把计数器一起回退。
// Python: MEMBER_TURN_STATE_KEY = "trajectory_member_turn"（turn.py L34）
const memberTurnStateKey = "trajectory_member_turn"

const (
	// turnIDField 状态字典中的 turn 标识键
	turnIDField = "turn_id"
	// turnNumberField 状态字典中的 turn 序号键
	turnNumberField = "turn_number"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadMemberTurn 读取成员最近一次 turn；任何异常/畸形数据视为不存在。
// Python: load_member_turn (turn.py L97-112)
func LoadMemberTurn(store TurnStateStore) (MemberTurn, bool) {
	if store == nil {
		return MemberTurn{}, false
	}
	raw, err := store.GetState(state.StringKey(memberTurnStateKey))
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("member turn state read failed")
		return MemberTurn{}, false
	}
	return memberTurnFromDict(raw)
}

// ResolveMemberTurn 解析本轮所属 turn。
// continuesTurn=true 且已有记录时续用；否则新开一个并推进计数器。
// 第二个返回值表示本轮是否新开（只有新开需要持久化 commit）。
// Python: resolve_member_turn (turn.py L115-142)
func ResolveMemberTurn(store TurnStateStore, continuesTurn bool) (MemberTurn, bool) {
	known, hasKnown := LoadMemberTurn(store)
	if continuesTurn && hasKnown {
		return known, false
	}
	previous := 0
	if hasKnown {
		previous = known.TurnNumber
	}
	opened := MemberTurn{TurnID: uuid.NewString(), TurnNumber: previous + 1}
	if store != nil {
		// 失败只影响观测计数，不能因此丢掉成员本轮工作；身份仍返回。
		// Python: except Exception → logger.debug（turn.py L137-141）
		store.UpdateState(map[string]any{memberTurnStateKey: opened.toDict()})
	}
	return opened, true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toDict 序列化为会话状态字典。
// Python: MemberTurn.to_dict (turn.py L65-74)
func (t MemberTurn) toDict() map[string]any {
	return map[string]any{
		turnIDField:     t.TurnID,
		turnNumberField: t.TurnNumber,
	}
}

// memberTurnFromDict 从会话状态恢复；畸形数据返回不存在。
// Python: MemberTurn.from_dict (turn.py L76-94)
func memberTurnFromDict(data any) (MemberTurn, bool) {
	m, ok := data.(map[string]any)
	if !ok {
		return MemberTurn{}, false
	}
	id, _ := m[turnIDField].(string)
	if id == "" {
		return MemberTurn{}, false
	}
	var number int
	switch v := m[turnNumberField].(type) {
	case int:
		number = v
	case int64:
		number = int(v)
	default:
		return MemberTurn{}, false
	}
	if number < 1 {
		return MemberTurn{}, false
	}
	return MemberTurn{TurnID: id, TurnNumber: number}, true
}
