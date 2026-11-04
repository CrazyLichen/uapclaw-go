package prompts

import (
	"strings"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UsesSystemAttachmentRole 判断当前路由是否把附件投递为 SystemMessage。
//
// Python: _uses_system_attachment_role。仅百炼/DashScope 需要，
// Anthropic 客户端会把 System 提到顶层 system 字段，故保持 UserMessage。
func UsesSystemAttachmentRole(provider, legacyProvider, endpointProfile, apiMode string) bool {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	provider, legacyProvider = norm(provider), norm(legacyProvider)
	endpointProfile, apiMode = norm(endpointProfile), norm(apiMode)

	if provider == "anthropic" {
		return false
	}
	switch apiMode {
	case "anthropic", "anthropic-messages", "messages":
		return false
	}
	backend := endpointProfile
	switch backend {
	case "", "openai", "openai-compatible":
		if legacyProvider != "" {
			backend = legacyProvider
		} else {
			backend = provider
		}
	}
	return backend == "bailian" || backend == "dashscope"
}

// BuildWindowMutator 构造附件消息的窗口投影（session 隔离 + role 替换）。
//
// Python: build_model_window_mutator。useSystemRole=false 时原样返回，便于调用方统一装配。
func BuildWindowMutator(sessionID string, useSystemRole bool) ceiface.WindowMutator {
	return func(w *ceiface.ContextWindow) *ceiface.ContextWindow {
		if w == nil || !useSystemRole {
			return w
		}
		out := make([]llmschema.BaseMessage, 0, len(w.ContextMessages))
		changed := false
		for _, msg := range w.ContextMessages {
			meta := msg.GetMetadata()
			if meta == nil || meta[MetadataKeyAttachmentHistory] != true {
				out = append(out, msg)
				continue
			}
			if sid, ok := meta[MetadataKeySessionID].(string); ok && sid != "" && sid != sessionID {
				out = append(out, msg)
				continue
			}
			out = append(out, llmschema.NewSystemMessage(msg.GetContent().Text(), llmschema.WithMetadata(meta)))
			changed = true
		}
		if !changed {
			return w
		}
		cp := *w
		cp.ContextMessages = out
		return &cp
	}
}
