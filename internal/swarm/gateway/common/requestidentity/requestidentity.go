package requestidentity

import "strings"

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeRoutingIdentity 归一化路由身份：去空白、丢弃空值。
// 输入键为 user_id/group_id/bot_id/gateway_id（wire 层头或 query 平铺）。
// Python: openjiuwen_protocol.identity normalize_routing_identity（语义对齐，
// wheel 源不可读，见包文档偏差说明）。
func NormalizeRoutingIdentity(fields map[string]string) map[string]string {
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out[key] = trimmed
	}
	return out
}

// ApplyRoutingMetadata 把归一化身份写入消息 metadata：
// user_id 摊到顶层，group/bot/gateway 收进 metadata["routing"]。
// Python: openjiuwen_protocol.identity apply_routing_metadata（语义对齐）。
func ApplyRoutingMetadata(metadata map[string]any, identity map[string]string) map[string]any {
	out := make(map[string]any, len(metadata)+2)
	for key, value := range metadata {
		out[key] = value
	}
	if uid := identity["user_id"]; uid != "" {
		out[UserIDField] = uid
	}
	routing := map[string]any{}
	for _, field := range []string{"group_id", "bot_id", "gateway_id"} {
		if value := identity[field]; value != "" {
			routing[field] = value
		}
	}
	if len(routing) > 0 {
		out[RoutingMetadataKey] = routing
	}
	return out
}

// WebRoutingIdentity 从 metadata 读全路由身份（含顶层 user_id）。
// Python: openjiuwen_protocol.identity web_routing_identity（语义对齐）。
func WebRoutingIdentity(metadata map[string]any) map[string]string {
	out := map[string]string{}
	if metadata == nil {
		return out
	}
	if uid, ok := metadata[UserIDField].(string); ok {
		out[UserIDField] = uid
	}
	if routing, ok := metadata[RoutingMetadataKey].(map[string]any); ok {
		for _, field := range []string{"group_id", "bot_id", "gateway_id"} {
			if value, ok := routing[field].(string); ok {
				out[field] = value
			}
		}
	}
	return out
}

// MergeRoutingIntoParams 仅用于 Gateway 本地 handler：handler 签名只有 params。
// Python: gateway/common/request_identity.py:49-56 merge_routing_into_params。
func MergeRoutingIntoParams(params map[string]any, metadata map[string]any, override bool) map[string]any {
	result := make(map[string]any, len(params)+4)
	for key, value := range params {
		result[key] = value
	}
	for field, value := range WebRoutingIdentity(metadata) {
		if override {
			result[field] = value
			continue
		}
		if _, exists := result[field]; !exists {
			result[field] = value
		}
	}
	return result
}
