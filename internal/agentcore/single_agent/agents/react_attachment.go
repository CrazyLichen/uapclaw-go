package agents

import (
	"context"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// syncPromptAttachments 把动态提示词同步进历史。
//
// Python: ReActAgent._sync_prompt_attachments（react_agent.py:1148-1180）。
// 失败只告警：附件投递不得阻断模型调用。
func syncPromptAttachments(ctx context.Context, m *prompts.Manager, modelCtx ceiface.ModelContext) error {
	if m == nil || modelCtx == nil {
		return nil
	}
	if err := m.Sync(ctx, modelCtx); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "prompt_attachment_sync_failed").
			Err(err).
			Msg("prompt attachment sync failed")
	}
	return nil
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
//
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
