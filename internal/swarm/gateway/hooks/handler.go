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
	// S-02 治标：defer recover 前移到方法第一行——原位置在 Match/applyGatewayTimeout
	// 之后，前置代码 panic 时（如共享 map 并发写触发）recover 未注册直接炸出。
	defer recoverHookPanic("SessionStart")

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
	hookConfigs = applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

	cwd, _ := os.Getwd() // 失败降级空串（Python Path.cwd() 不会失败，Go 需防御）
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
	// S-02 治标：defer recover 前移到方法第一行
	defer recoverHookPanic("UserPromptSubmit")

	hookConfigs := h.config.Match(hookscfg.HookEventUserPromptSubmit, "")
	if len(hookConfigs) == 0 {
		return
	}
	hookConfigs = applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

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
	// S-02 治标：defer recover 前移到方法第一行
	defer recoverHookPanic("SessionEnd")

	h.mu.Lock()
	delete(h.activeSessions, sessionID)
	h.mu.Unlock()

	hookConfigs := h.config.Match(hookscfg.HookEventSessionEnd, reason)
	if len(hookConfigs) == 0 {
		return
	}
	hookConfigs = applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

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
	// S-02 治标：defer recover 前移到方法第一行
	defer recoverHookPanic("Notification")

	hookConfigs := h.config.Match(hookscfg.HookEventNotification, notificationType)
	if len(hookConfigs) == 0 {
		return
	}
	hookConfigs = applyGatewayTimeout(hookConfigs, h.gatewayTimeout)

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
// S-02 治本：Match 返回的是 config 中共享的 map 引用，Python 单线程就地改
// 无碍，Go 并发调用 On* 时多 goroutine 写同一 map 是数据竞争。此处构造
// 新 slice+浅拷贝 map 后再补 timeout，不污染共享配置（timeout 每次调用
// 注入的值相同，语义不变）。
func applyGatewayTimeout(hookConfigs []map[string]any, timeout int) []map[string]any {
	out := make([]map[string]any, len(hookConfigs))
	for i, cfg := range hookConfigs {
		clone := make(map[string]any, len(cfg)+1)
		for k, v := range cfg {
			clone[k] = v
		}
		if _, ok := clone["timeout"]; !ok {
			clone["timeout"] = timeout
		}
		out[i] = clone
	}
	return out
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
