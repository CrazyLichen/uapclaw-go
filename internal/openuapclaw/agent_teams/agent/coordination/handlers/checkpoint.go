package handlers

import (
	"context"

	types "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CheckpointHandler 把成员创建的命名 checkpoint 通告投递进 leader 上下文。
//
// 成员存快照后发布 CHECKPOINT_CREATED 事件（专用 event_key，无 fan-out
// overlap），leader 在此把快照名渲染为框架 <team-event kind="checkpoint">
// 并附 announcement-only 注记。事件——而非 send_message——是刻意选择的投递
// 通道：成员的邮箱消息带 reply hint，可能引诱 leader 回答、污染其上下文；
// announcement-only 框架事件是纯信息性的、明确要求不回复。协调层在此不做
// 任何决策——它只为 fork 协作（list_checkpoints / fork="<name>"）转发名字。
//
// EVENT_METHOD_MAP 事件到方法映射：
//   - checkpoint_created → OnCheckpointCreated
//
// Python: CheckpointHandler (handlers/checkpoint.py:29-66)
type CheckpointHandler struct {
	BaseCoordinationHandler
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCheckpointHandler 创建 CheckpointHandler 实例。
// Python: CheckpointHandler.__init__
func NewCheckpointHandler(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
) *CheckpointHandler {
	return &CheckpointHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表（1 项）。
// Python: CheckpointHandler.EVENT_METHOD_MAP = {CHECKPOINT_CREATED: on_checkpoint_created}
func (h *CheckpointHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		events.TeamEventCheckpointCreated: h.OnCheckpointCreated,
	}
}

// OnCheckpointCreated 渲染快照名并投递为 announcement。
// 仅 leader 处理；payload 解码失败 debug 返回。
// 追加投递（use_steer=False）：名字是信息性的，不打断 leader 当前轮，
// announcement-only 注记明确不要求回复。
// Python: on_checkpoint_created (handlers/checkpoint.py:36-66)
func (h *CheckpointHandler) OnCheckpointCreated(ctx context.Context, event types.CoordinationEvent) {
	// Python: if self._blueprint.role != TeamRole.LEADER: return
	if h.blueprint.Role() != schema.TeamRoleLeader {
		return
	}
	if event.IsInner() || event.Transport == nil {
		return
	}
	// Python: payload = event.get_payload()（pydantic 解码失败 debug 返回）
	em := event.Transport
	name, _ := em.Payload["name"].(string)
	if name == "" {
		logger.Debug(logComponent).Msg("checkpoint_created payload decode failed: missing name")
		return
	}
	memberName, _ := em.Payload["member_name"].(string)
	count := 0
	if v, ok := em.Payload["message_count"].(float64); ok {
		count = int(v)
	} else if v, ok := em.Payload["message_count"].(int); ok {
		count = v
	}
	description, _ := em.Payload["description"].(string)

	// Python: body = t("checkpoint.created_body", name=..., member=..., count=..., description=...)
	descriptionSuffix := ""
	if description != "" {
		descriptionSuffix = " (" + description + ")"
	}
	body, err := agentteams.T("checkpoint.created_body", map[string]any{
		"name":        name,
		"member":      memberName,
		"count":       count,
		"description": descriptionSuffix,
	})
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("checkpoint_created body render failed")
		return
	}
	// Python: note_text = t("checkpoint.created_note")
	noteText, _ := agentteams.T("checkpoint.created_note", nil)
	// Python: render_event(kind="checkpoint", body=..., note_kind="announcement-only", note_text=...)
	content := agentteams.RenderEvent("checkpoint", body, agentteams.EventOptions{
		NoteKind: "announcement-only",
		NoteText: noteText,
	})
	// Python: deliver_input(..., use_steer=False)
	if err := h.round.DeliverInput(ctx, content, false, nil); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("checkpoint", name).
			Msg("checkpoint_created announcement delivery failed")
	}
}
