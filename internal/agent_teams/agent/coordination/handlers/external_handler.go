// Package handlers 三方外部运行时重试中进度 handler（25.1g）。
//
// 对应 Python：agent_teams/reliability/external_handler.py。
// 消费 EXTERNAL_RUNTIME_RETRYING 跨进程事件（Claude/Codex SDK 运行时仍在自动
// 重试时发布），把非持久化的进度提示上浮给 leader。最终失败不在此处理——它们
// 以 external_runtime_failed JSON 消息持久化到 leader 信箱，由 MessageHandler
// 投递。本 handler 是 ReliabilityHandler 的重试中对应物：同为仅 leader 的协调
// handler，经 deliver_input 把信号路由进 leader 自己的轮次；二者保持解耦——
// 互不调用。
package handlers

import (
	"context"
	"strconv"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalRuntimeHandler 把 Claude/Codex SDK 自动重试进度上浮给 leader（仅 leader）。
//
// Python: external_handler.py ExternalRuntimeHandler。
type ExternalRuntimeHandler struct {
	BaseCoordinationHandler
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExternalRuntimeHandler 构造外部运行时重试 handler（恒注册）。
//
// Python: external_handler.py ExternalRuntimeHandler.__init__。
func NewExternalRuntimeHandler(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
) *ExternalRuntimeHandler {
	return &ExternalRuntimeHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
	}
}

// GetCallbacks 返回 EVENT_METHOD_MAP（1 项）。
//
// Python: external_handler.py EVENT_METHOD_MAP = {EXTERNAL_RUNTIME_RETRYING: on_external_retry}。
func (h *ExternalRuntimeHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		events.TeamEventExternalRuntimeRetrying: func(ctx context.Context, event types.CoordinationEvent) {
			h.onExternalRetry(ctx, event)
		},
	}
}

// onExternalRetry 把重试进度提示投进 leader 的轮次输入。
//
// Python: external_handler.py ExternalRuntimeHandler.on_external_retry。
func (h *ExternalRuntimeHandler) onExternalRetry(ctx context.Context, event types.CoordinationEvent) {
	if h.blueprint.Role() != schema.TeamRoleLeader {
		return
	}
	if event.Transport == nil || event.Transport.EventType != events.TeamEventExternalRuntimeRetrying {
		return
	}
	payload := event.Transport.Payload
	memberName := strAny(payload["member_name"])
	if memberName == "" {
		memberName = "unknown"
	}
	category := strAny(payload["category"])
	summary := strAny(payload["summary"])
	agentKind := strAny(payload["agent_kind"])
	model := strAny(payload["model"])
	if model == "" {
		model = "<unknown>"
	}
	empty := "<unknown>"
	reason := mapOrNone(payload["reason"])
	reasonMessage := strAny(reason["message"])
	if reasonMessage == "" {
		reasonMessage = empty
	}
	httpStatus := strAny(reason["http_status"])
	if httpStatus == "" {
		httpStatus = empty
	}
	sdkErrorCode := strAny(reason["sdk_error_code"])
	if sdkErrorCode == "" {
		sdkErrorCode = empty
	}
	attempt := anyOrUnknown(payload["attempt"])
	maxAttempts := anyOrUnknown(payload["max_attempts"])
	logger.Info(logComponent).
		Str("member", memberName).
		Str("agent_kind", agentKind).
		Str("model", model).
		Str("category", category).
		Str("summary", summary).
		Msg("[external-runtime] member retrying")
	content := i18nT("reliability.external_runtime_retrying", map[string]any{
		"member_name":    memberName,
		"agent_kind":     agentKind,
		"model":          model,
		"category":       category,
		"summary":        summary,
		"reason_message": reasonMessage,
		"http_status":    httpStatus,
		"sdk_error_code": sdkErrorCode,
		"attempt":        attempt,
		"max_attempts":   maxAttempts,
	})
	if err := h.round.DeliverInput(ctx, content, true); err != nil {
		logger.Error(logComponent).Err(err).Msg("[external-runtime] deliver_input failed")
	}
}

// anyOrUnknown nil→"<unknown>"，其余数字→字符串。
func anyOrUnknown(v any) string {
	switch n := v.(type) {
	case nil:
		return "<unknown>"
	case string:
		return n
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case float64:
		return strconv.Itoa(int(n))
	default:
		return "<unknown>"
	}
}
