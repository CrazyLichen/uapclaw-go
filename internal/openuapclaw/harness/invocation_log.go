package harness

// 调用日志作用域接线（17.4b2 增强件）。
// Python: deep_agent.py 的 inherit_invocation_log_dir / create_subagent 传参 /
// _invocation_log_context 包裹 invoke+stream 三处。

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/logging/invocation"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InheritInvocationLogDir 父调用无配置时继承父的调用日志目录（Python inherit_invocation_log_dir）。
// parentDir 为空、或本 agent 已有配置时不覆盖。
func (d *DeepAgent) InheritInvocationLogDir(parentDir string) {
	// Python: if parent_dir is None or self._deep_config is None or self._deep_config.invocation_log_dir is not None: return
	if parentDir == "" {
		return
	}
	d.configMu.Lock()
	defer d.configMu.Unlock()
	if d.deepConfig == nil || d.deepConfig.InvocationLogDir != "" {
		return
	}
	// Python: self._deep_config = replace(self._deep_config, invocation_log_dir=parent_dir)
	d.deepConfig.InvocationLogDir = parentDir
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// startInvocationLogContext 调用日志作用域（Python _invocation_log_context, deep_agent.py:3325）。
// 返回注入后的 ctx 与收尾函数（写 invocation_end 并关闭文件）；目录未配置返回原 ctx 与 nil 收尾。
func (d *DeepAgent) startInvocationLogContext(ctx context.Context, sess sessioninterfaces.SessionFacade) (context.Context, func(error), error) {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()
	// Python: directory = self._deep_config.invocation_log_dir if ... else None
	if cfg == nil || cfg.InvocationLogDir == "" {
		return ctx, nil, nil
	}
	// Python: session_id = session.get_session_id() if session is not None else ""
	sessionID := ""
	if sess != nil {
		sessionID = sess.GetSessionID()
	}
	// Python: invocation_log_scope(directory, agent_id=self.card.id, session_id=..., role=self.card.name)
	// 嵌套 scope 的 parent 经 ctx 取活动 scope（Python _ACTIVE_INVOCATION.get()）
	span, err := invocation.StartInvocationLog(ctx, cfg.InvocationLogDir, d.card.ID, sessionID, d.card.Name)
	if err != nil {
		// Python: except Exception: logger.warning("Invocation log capture unavailable: %s", type(exc).__name__)
		return ctx, nil, err
	}
	return span.Ctx, span.Finish, nil
}

// warnInvocationLogUnavailable 构造失败告警（Python warning 文案）
func warnInvocationLogUnavailable(err error) {
	logger.Warn(logger.ComponentCommon).Str("error_type", err.Error()).Msg("Invocation log capture unavailable")
}
