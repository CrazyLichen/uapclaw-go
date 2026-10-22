package hooks

import (
	"context"
	"os"
	"sync"

	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayHookHandler Gateway 层钩子处理器
// Python: GatewayHookHandler (handler.py L16-127)
type GatewayHookHandler struct {
	// config hooks 配置聚合
	config *hookscfg.HooksConfig
	// executor hook 执行器（共享自 common/hooks）
	executor *hookscfg.HookExecutor
	// gatewayTimeout 网关层 hook 超时（秒），短于 AgentServer 30s 避免阻塞转发
	gatewayTimeout int
	// mu 保护 activeSessions
	mu sync.Mutex
	// activeSessions 活跃会话去重集
	activeSessions map[string]bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentGateway

	// defaultGatewayTimeout 默认网关 hook 超时 10 秒（对齐 Python _gateway_timeout）
	defaultGatewayTimeout = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayHookHandler 创建 Gateway 层钩子处理器。
// Python: GatewayHookHandler(hooks_config) (L25-29)
func NewGatewayHookHandler(hooksConfig *hookscfg.HooksConfig) *GatewayHookHandler {
	return &GatewayHookHandler{
		config:         hooksConfig,
		executor:       hookscfg.NewHookExecutor(),
		gatewayTimeout: defaultGatewayTimeout,
		activeSessions: make(map[string]bool),
	}
}

// OnSessionStart 会话开始时触发 SessionStart hooks。
//
// Python: on_session_start (L31-57)：activeSessions 去重；
// match(query=source)；失败仅 Warn 不传播。
func (h *GatewayHookHandler) OnSessionStart(ctx context.Context, sessionID, source string) {
	h.mu.Lock()
	if h.activeSessions[sessionID] {
		h.mu.Unlock()
		return
	}
	h.activeSessions[sessionID] = true
	h.mu.Unlock()

	hookConfigs := h.config.Match(hookscfg.HookEventSessionStart, source)
	if len(hookConfigs) == 0 {
		return
	}
	applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	cwd, _ := os.Getwd() // 失败降级空串（Python Path.cwd() 不会失败，Go 需防御）
	defer recoverHookPanic("SessionStart")
	h.executor.RunAll(ctx, hookConfigs, map[string]any{
		"event":      "SessionStart",
		"source":     source,
		"session_id": sessionID,
		"cwd":        cwd,
	}, sessionID)
}

// OnUserPromptSubmit 用户提交消息时触发 UserPromptSubmit hooks。
//
// Python: on_user_prompt_submit (L59-78)：match 无 query（默认空串）。
func (h *GatewayHookHandler) OnUserPromptSubmit(ctx context.Context, sessionID, prompt string) {
	hookConfigs := h.config.Match(hookscfg.HookEventUserPromptSubmit, "")
	if len(hookConfigs) == 0 {
		return
	}
	applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	defer recoverHookPanic("UserPromptSubmit")
	h.executor.RunAll(ctx, hookConfigs, map[string]any{
		"event":      "UserPromptSubmit",
		"prompt":     prompt,
		"session_id": sessionID,
	}, sessionID)
}

// OnSessionEnd 会话结束时触发 SessionEnd hooks。
//
// Python: on_session_end (L80-103)：先 discard activeSessions，match(query=reason)。
func (h *GatewayHookHandler) OnSessionEnd(ctx context.Context, sessionID, reason string) {
	h.mu.Lock()
	delete(h.activeSessions, sessionID)
	h.mu.Unlock()

	hookConfigs := h.config.Match(hookscfg.HookEventSessionEnd, reason)
	if len(hookConfigs) == 0 {
		return
	}
	applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	defer recoverHookPanic("SessionEnd")
	h.executor.RunAll(ctx, hookConfigs, map[string]any{
		"event":      "SessionEnd",
		"reason":     reason,
		"session_id": sessionID,
	}, sessionID)
}

// OnNotification 通知发送时触发 Notification hooks。
//
// Python: on_notification (L105-127)：match(query=notification_type)。
func (h *GatewayHookHandler) OnNotification(ctx context.Context, notificationType, message, sessionID string) {
	hookConfigs := h.config.Match(hookscfg.HookEventNotification, notificationType)
	if len(hookConfigs) == 0 {
		return
	}
	applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	defer recoverHookPanic("Notification")
	h.executor.RunAll(ctx, hookConfigs, map[string]any{
		"event":             "Notification",
		"notification_type": notificationType,
		"message":           message,
		"session_id":        sessionID,
	}, sessionID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyGatewayTimeout 逐项补默认超时（Python cfg.setdefault("timeout", 10)）。
//
// 就地修改共享 map（Python 同样就地改 Match 返回的 dict 引用，行为一致）。
func applyGatewayTimeout(hookConfigs []map[string]any, timeout int) {
	for i := range hookConfigs {
		if _, ok := hookConfigs[i]["timeout"]; !ok {
			hookConfigs[i]["timeout"] = timeout
		}
	}
}

// recoverHookPanic hook 执行 panic 兜底（对齐 Python try/except + logger.warning）。
func recoverHookPanic(event string) {
	if r := recover(); r != nil {
		logger.Warn(logComponent).
			Str("event", event).
			Any("panic", r).
			Msg("GatewayHookHandler hook failed")
	}
}
