package agents

import (
	"context"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
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
