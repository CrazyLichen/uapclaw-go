// Package server —— session messaging 服务面 port 适配器。
// 把 *SessionMessageService 适配为 tools.SessionMessagingServicePort
// （tools 叶子包窄接口，防 tools→server 循环依赖；源类型逐字段转换，
// 两类型字段一一对应：session_messaging_toolkit.py:64-74 / :28-36）。
package server

import (
	"context"

	sessionmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// sessionMessagingServiceAdapter tools.SessionMessagingServicePort 的服务面实现。
type sessionMessagingServiceAdapter struct {
	svc *SessionMessageService
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionMessagingServicePort 把 mailbox 服务包装为 toolkit 窄接口。
func NewSessionMessagingServicePort(svc *SessionMessageService) sessionmtools.SessionMessagingServicePort {
	if svc == nil {
		return nil
	}
	return &sessionMessagingServiceAdapter{svc: svc}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toToolsSource server 源 → tools 源（字段一一对应）。
func toToolsSource(s *SessionMessageSource) *sessionmtools.SessionMessageSource {
	if s == nil {
		return nil
	}
	return &sessionmtools.SessionMessageSource{
		SessionID:       s.SessionID,
		RequestID:       s.RequestID,
		ToolCallID:      s.ToolCallID,
		IdempotencyKey:  s.IdempotencyKey,
		UserID:          s.UserID,
		ChainID:         s.ChainID,
		ParentMessageID: s.ParentMessageID,
		HopCount:        s.HopCount,
	}
}

// toServerSource tools 源 → server 源。
func toServerSource(s *sessionmtools.SessionMessageSource) *SessionMessageSource {
	if s == nil {
		return nil
	}
	return &SessionMessageSource{
		SessionID:       s.SessionID,
		RequestID:       s.RequestID,
		ToolCallID:      s.ToolCallID,
		IdempotencyKey:  s.IdempotencyKey,
		UserID:          s.UserID,
		ChainID:         s.ChainID,
		ParentMessageID: s.ParentMessageID,
		HopCount:        s.HopCount,
	}
}

// ListTargets session_list 服务面。
func (a *sessionMessagingServiceAdapter) ListTargets(ctx context.Context, source *sessionmtools.SessionMessageSource, query string, limit, offset int) (map[string]any, error) {
	return a.svc.ListTargets(ctx, toServerSource(source), query, limit, offset)
}

// SendMessage session_send_message 服务面。
func (a *sessionMessagingServiceAdapter) SendMessage(ctx context.Context, source *sessionmtools.SessionMessageSource, targetSessionID, message, inputMode string) (map[string]any, error) {
	return a.svc.SendMessage(ctx, toServerSource(source), targetSessionID, message, inputMode)
}

// ListMessages session_message_list 服务面。
func (a *sessionMessagingServiceAdapter) ListMessages(ctx context.Context, source *sessionmtools.SessionMessageSource, targetSessionID string, limit, offset int) (map[string]any, error) {
	return a.svc.ListMessages(ctx, toServerSource(source), targetSessionID, limit, offset)
}

// ContinueQueued session_continue_queued 服务面。
func (a *sessionMessagingServiceAdapter) ContinueQueued(ctx context.Context, source *sessionmtools.SessionMessageSource, targetSessionID string) (map[string]any, error) {
	return a.svc.ContinueQueued(ctx, toServerSource(source), targetSessionID)
}

// ReadSession session_read 服务面。
func (a *sessionMessagingServiceAdapter) ReadSession(ctx context.Context, source *sessionmtools.SessionMessageSource, targetSessionID, cursor string, limit, maxOutputChars int) (map[string]any, error) {
	return a.svc.ReadSession(ctx, toServerSource(source), targetSessionID, cursor, limit, maxOutputChars)
}

// ResolveUnknown session_message_resolve 服务面。
func (a *sessionMessagingServiceAdapter) ResolveUnknown(ctx context.Context, source *sessionmtools.SessionMessageSource, messageID, resolution string) (map[string]any, error) {
	return a.svc.ResolveUnknown(ctx, toServerSource(source), messageID, resolution)
}

// PendingContext 每轮模型调用前的队列快照。
func (a *sessionMessagingServiceAdapter) PendingContext(ctx context.Context, source *sessionmtools.SessionMessageSource) (map[string]any, error) {
	return a.svc.PendingContext(ctx, toServerSource(source))
}
