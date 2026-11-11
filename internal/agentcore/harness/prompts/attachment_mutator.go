package prompts

import (
	"strings"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/endpointprofiles"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UsesSystemAttachmentRole 判断当前路由是否把附件投递为 SystemMessage。
// Python: prompt_attachment_manager.py:172-188 _uses_system_attachment_role(model_client_config)。
// 仅百炼/DashScope 需要；Anthropic 客户端会把 System 提到顶层 system 字段，故保持 UserMessage。
// 归一前原名（legacy_client_provider）经 endpointprofiles 取，覆盖别名未归一的调用场景；
// api_mode / endpoint_profile 读结构体字段（S0-3 修复：旧实现从 Extra 读、恒为空串）。
func UsesSystemAttachmentRole(cfg *llmschema.ModelClientConfig) bool {
	if cfg == nil {
		return false
	}
	provider := normConfigValue(cfg.ClientProvider)
	apiMode := normConfigValue(string(cfg.APIMode))
	legacyProvider := normConfigValue(endpointprofiles.LegacyProvider(cfg))
	endpointProfile := normConfigValue(cfg.EndpointProfile)

	if provider == "anthropic" || anthropicAPIModes[apiMode] {
		return false
	}
	backend := endpointProfile
	if genericEndpointProfiles[backend] {
		if legacyProvider != "" {
			backend = legacyProvider
		} else {
			backend = provider
		}
	}
	return systemAttachmentRoleProfiles[backend] || systemAttachmentRoleProviders[backend]
}

// normConfigValue 对齐 Python _normalized_config_value：小写 + trim。
func normConfigValue(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// 三个集合对齐 Python prompt_attachment_manager.py:77-80 的 frozenset。
var (
	anthropicAPIModes             = map[string]bool{"anthropic": true, "anthropic-messages": true, "messages": true}
	genericEndpointProfiles       = map[string]bool{"": true, "openai": true, "openai-compatible": true}
	systemAttachmentRoleProviders = map[string]bool{"bailian": true, "dashscope": true}
	systemAttachmentRoleProfiles  = map[string]bool{"bailian": true, "dashscope": true}
)

// BuildWindowMutator 构造附件消息的窗口投影（session 隔离 + role 替换）。
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
