package agents

import (
	"context"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// syncPromptAttachments 把动态提示词同步进历史，并返回同步是否成功。
// 返回值是提交回调的执行前提：附件车道是「当前状态」通道，边界时比对
// 注册区与历史哈希，只有真正写进历史，才能把下游基线推进到"已送达"。
// 失败只告警：附件投递不得阻断模型调用。
// Python: ReActAgent._sync_prompt_attachments（react_agent.py:1148-1182）。
func syncPromptAttachments(ctx context.Context, m *prompts.Manager, hc prompts.HistoryContext) bool {
	if m == nil || hc == nil {
		return true // 无附件车道：不阻断调用方，也没有回调要消费
	}
	if err := m.Sync(ctx, hc); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "prompt_attachment_sync_failed").
			Err(err).
			Msg("prompt attachment sync failed")
		return false
	}
	return true
}

// runCommitCallbacks 执行并清空投递成功回调。
// 只在 Sync 成功之后调用：失败时调用方不会走到这里，回调留在 ctx.extra
// 等下一次边界重试（上游在 pop 之前就 return，回调列表不被动过）。
// Python: ReActAgent._sync_prompt_attachments 尾段（react_agent.py:1169-1182）。
func runCommitCallbacks(cbc *interfaces.AgentCallbackContext) {
	if cbc == nil {
		return
	}
	raw, ok := cbc.DeleteExtra(prompts.CommitCallbacksExtraKey)
	if !ok {
		return
	}
	callbacks, ok := raw.([]func() error)
	if !ok {
		// Python: if not isinstance(callbacks, list): return
		return
	}
	for _, fn := range callbacks {
		runOneCommitCallback(fn)
	}
}

// runOneCommitCallback 执行单条提交回调，失败或 panic 只告警。
// Python 捕 Exception 继续下一条；Go 的 panic 不 recover 会掀掉整个 invoke，
// 故显式兜住（spec D-2）。
func runOneCommitCallback(fn func() error) {
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("event_type", "prompt_attachment_delivery_callback_panicked").
				Any("panic", r).
				Msg("prompt attachment delivery callback panicked")
		}
	}()
	if err := fn(); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "prompt_attachment_delivery_callback_failed").
			Err(err).
			Msg("prompt attachment delivery callback failed")
	}
}

// extraString 从配置 Extra 中取字符串（缺失或类型不符返回空串）。
func extraString(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	if v, ok := extra[key].(string); ok {
		return v
	}
	return ""
}

// attachmentWindowOptions 构建附件窗口投影选项；未注入 manager 或缺少客户端配置时返回空。
// Python: _build_model_context_window 中按 provider 选择 use_system_role 后注册 mutator。
func attachmentWindowOptions(m *prompts.Manager, model *llm.Model, sessionID string) []ceiface.Option {
	if m == nil || model == nil || model.ClientConfig == nil {
		return nil
	}
	cfg := model.ClientConfig
	useSystemRole := prompts.UsesSystemAttachmentRole(
		cfg.ClientProvider,
		extraString(cfg.Extra, "legacy_client_provider"),
		extraString(cfg.Extra, "endpoint_profile"),
		extraString(cfg.Extra, "api_mode"),
	)
	return []ceiface.Option{
		ceiface.WithWindowMutators(prompts.BuildWindowMutator(sessionID, useSystemRole)),
	}
}
