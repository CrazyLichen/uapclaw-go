package adapter

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// pendingGoalHistory 挂起的 goal objective 历史记录。
type pendingGoalHistory struct {
	// sessionID 会话标识
	sessionID string
	// requestID 请求标识
	requestID string
	// channelID 渠道标识
	channelID string
	// content 目标正文
	content string
	// channelMetadata 渠道元数据
	channelMetadata map[string]any
	// mode 运行模式
	mode string
	// goalID 目标标识
	goalID string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// pendingGoalObjectives 进程级挂起表：sessionID → 待落盘记录。
// Python: interface_deep.py:_pending_goal_objective_history（模块级 dict）
var (
	pendingGoalObjectivesMu sync.Mutex
	pendingGoalObjectives   = map[string]*pendingGoalHistory{}
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordGoalSetHistoryIfNeeded 成功 set 后写入 objective 用户历史。
// Python: interface_deep.py:17610-17654
//
// 忙碌时（上一轮 user 仍在，或同会话另有并发任务）推迟到轮边界再写，
// 否则重载后的时间线顺序会错。
func (d *DeepAdapter) recordGoalSetHistoryIfNeeded(
	req *schema.AgentRequest, action, resultType string, goalPayload map[string]any,
	deferOverride *bool,
) {
	if req == nil || action != "set" {
		return
	}
	if resultType == goalResultError || resultType == goalResultConfirmRequired || resultType == "" {
		return
	}
	if goalPayload == nil {
		return
	}
	objective, _ := goalPayload["objective"].(string)
	if objective == "" {
		return
	}
	params := parseParams(req.Params)
	goalID, _ := goalPayload["goal_id"].(string)
	sessionID := "default"
	if req.SessionID != nil && *req.SessionID != "" {
		sessionID = *req.SessionID
	}
	mode := paramsString(params, "mode", "")
	if mode == "" {
		mode = "unknown"
	}
	entry := &pendingGoalHistory{
		sessionID:       sessionID,
		requestID:       req.RequestID,
		channelID:       req.ChannelID,
		content:         objective,
		channelMetadata: req.Metadata,
		mode:            mode,
		goalID:          goalID,
	}
	shouldDefer := d.shouldDeferGoalObjectiveHistory(sessionID)
	if deferOverride != nil {
		// 上游在 dispatch 前采样，避免 active_round 切到 goal 后误判
		shouldDefer = *deferOverride
	}
	if shouldDefer {
		pendingGoalObjectivesMu.Lock()
		pendingGoalObjectives[sessionID] = entry
		pendingGoalObjectivesMu.Unlock()
		return
	}
	pendingGoalObjectivesMu.Lock()
	delete(pendingGoalObjectives, sessionID)
	pendingGoalObjectivesMu.Unlock()
	appendGoalObjectiveHistory(entry)
}

// shouldDeferGoalObjectiveHistory 是否需要把 objective 历史推迟到轮边界。
// Python: interface_deep.py:17584-17608
func (d *DeepAdapter) shouldDeferGoalObjectiveHistory(sessionID string) bool {
	if d.currentInteractionRunKind() == "user" {
		return true
	}
	if d.streamContentRunKind == "user" || d.streamRoundKindLatch == "user" {
		return true
	}
	// 本 stream 已 mark 过；count>1 表示同会话还有别的请求在跑
	return d.activeSessionCount(sessionID) > 1
}

// flushPendingGoalObjectiveHistory 把挂起的 goal 用户历史落到磁盘（无挂起则 noop）。
// Python: interface_deep.py:17597-17608
func (d *DeepAdapter) flushPendingGoalObjectiveHistory(sessionID string) {
	pendingGoalObjectivesMu.Lock()
	entry := pendingGoalObjectives[sessionID]
	delete(pendingGoalObjectives, sessionID)
	pendingGoalObjectivesMu.Unlock()
	if entry == nil {
		return
	}
	appendGoalObjectiveHistory(entry)
}

// pendingGoalObjective 返回挂起记录（仅测试与调试用）。
func (d *DeepAdapter) pendingGoalObjective(sessionID string) *pendingGoalHistory {
	pendingGoalObjectivesMu.Lock()
	defer pendingGoalObjectivesMu.Unlock()
	return pendingGoalObjectives[sessionID]
}

// appendGoalObjectiveHistory 真正落盘一条 objective 用户历史。
// Python: append_history_record(role="user", extra={goal_id, is_goal_objective_message})
func appendGoalObjectiveHistory(entry *pendingGoalHistory) {
	sessionpkg.AppendHistoryRecord(
		entry.sessionID, entry.requestID, entry.channelID,
		"user", entry.content, float64(time.Now().UnixMilli())/1000,
		"", map[string]any{
			"goal_id":                   entry.goalID,
			"is_goal_objective_message": true,
		},
		entry.channelMetadata, entry.mode,
	)
}

// recordGoalCompletedHistoryIfNeeded 状态首次变为 completed 时落盘一张完成卡片。
// Python: interface_deep.py:17677-17723
//
// 保持前端 GoalCompletedCard 的 wire 格式（"goal.completed:" + json），
// 让历史恢复与 localStorage 合并无需 content parser 分叉。
func (d *DeepAdapter) recordGoalCompletedHistoryIfNeeded(
	sessionID, channelID string, channelMetadata map[string]any,
	mode string, goalPayload map[string]any,
) {
	if goalPayload == nil {
		return
	}
	status, _ := goalPayload["status"].(string)
	if strings.ToLower(strings.TrimSpace(status)) != "completed" {
		return
	}
	goalID, _ := goalPayload["goal_id"].(string)
	if goalID == "" {
		return
	}
	sid := sessionID
	if sid == "" {
		sid = "default"
	}
	if goalCompletedHistoryExists(sid, goalID) {
		return
	}
	evidence := ""
	if assessment, ok := goalPayload["last_assessment"].(map[string]any); ok {
		evidence, _ = assessment["evidence"].(string)
	}
	raw, err := json.Marshal(map[string]any{"evidence": evidence})
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "GOAL_COMPLETED_HISTORY_MARSHAL_FAILED").
			Str("goal_id", goalID).
			Msg("marshal goal completed evidence failed")
		return
	}
	messageID := "goal-completed-" + goalID
	if mode == "" {
		mode = "unknown"
	}
	sessionpkg.AppendHistoryRecord(
		sid, messageID, channelID,
		"assistant", "goal.completed:"+string(raw),
		float64(time.Now().UnixMilli())/1000, "",
		map[string]any{
			"id":                        messageID,
			"goal_id":                   goalID,
			"is_goal_completed_message": true,
			"evidence":                  evidence,
		},
		channelMetadata, mode,
	)
}

// goalCompletedHistoryExists 该会话是否已为 goal_id 落过完成卡片。
// Python: interface_deep.py:17656-17675
func goalCompletedHistoryExists(sessionID, goalID string) bool {
	messageID := "goal-completed-" + goalID
	// Go 的历史写入是异步队列，Python 是同步 append。读之前必须 flush，
	// 否则同一 goal 的第二次 goal.updated 会看不到第一次的记录而重复落盘
	// （前端会出两张完成卡片）。该路径只在目标完成时走一次，flush 代价可接受。
	sessionpkg.FlushHistoryQueue(5.0)
	records, err := sessionpkg.ReadHistoryRecords(sessionID)
	if err != nil {
		return false
	}
	for _, record := range records {
		if id, _ := record["id"].(string); id == messageID {
			return true
		}
		if done, _ := record["is_goal_completed_message"].(bool); done {
			if recorded, _ := record["goal_id"].(string); recorded == goalID {
				return true
			}
		}
	}
	return false
}
