package openai

import (
	"fmt"
	"reflect"
	"strings"
)

// OpenRouter 显式 prompt 缓存（cache_control 注入）写链。
// Python: openjiuwen/core/foundation/llm/model_clients/openai_model_client.py:95-285
// 读链（cache 用量解析）已由 model_clients/usage_extract.go 承担，本文件只做写链。

// ──────────────────────────── 常量 ────────────────────────────

var (
	// openrouterExplicitPromptCachingProviders 支持显式 prompt 缓存的 OpenRouter 底层 provider。
	// Python: openai_model_client.py:100-102
	openrouterExplicitPromptCachingProviders = map[string]struct{}{"anthropic": {}, "qwen": {}}

	// openrouter1HPromptCacheTTLProviders 支持 1h TTL 的 provider（仅 anthropic）。
	// Python: openai_model_client.py:104-106
	openrouter1HPromptCacheTTLProviders = map[string]struct{}{"anthropic": {}}
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// openrouterModelProvider 从 "provider/model" 形态取 provider 前缀，剥 "~" 并小写；
// 无 "/" 或空串返回空串（对应 Python None）。
// Python: openai_model_client.py:153-157
func openrouterModelProvider(model string) string {
	if model == "" {
		return ""
	}
	idx := strings.IndexByte(model, '/')
	if idx < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimLeft(model[:idx], "~"))
}

// normalizeOpenrouterProviderSet 归一 provider 集合（逗号串/列表，strip+lower）；
// value 为 nil 或解析结果为空时回落默认集合。
// Python: openai_model_client.py:159-167
func normalizeOpenrouterProviderSet(value any, def map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	switch v := value.(type) {
	case string:
		for _, part := range strings.Split(v, ",") {
			if s := strings.ToLower(strings.TrimSpace(part)); s != "" {
				out[s] = struct{}{}
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if t := strings.ToLower(strings.TrimSpace(s)); t != "" {
					out[t] = struct{}{}
				}
			}
		}
	case []string:
		for _, item := range v {
			if t := strings.ToLower(strings.TrimSpace(item)); t != "" {
				out[t] = struct{}{}
			}
		}
	default:
		// nil 与其他不可解析形态均回落默认（Python TypeError → return default）
		return def
	}
	if len(out) == 0 {
		return def
	}
	return out
}

// supportsOpenrouterExplicitPromptCaching 模型 provider 是否支持显式缓存。
// Python: openai_model_client.py:169-174
func supportsOpenrouterExplicitPromptCaching(model string, providers map[string]struct{}) bool {
	_, ok := providers[openrouterModelProvider(model)]
	return ok
}

// supportsOpenrouter1HPromptCacheTTL 模型 provider 是否支持 1h TTL。
// Python: openai_model_client.py:176-181
func supportsOpenrouter1HPromptCacheTTL(model string, providers map[string]struct{}) bool {
	_, ok := providers[openrouterModelProvider(model)]
	return ok
}

// withoutCacheControl 深度剥除 cache_control 键；剥除后若只剩单 text 块则还原为纯字符串，
// content 为单字符串元素的 list 也还原为字符串。
// Python: openai_model_client.py:183-199
func withoutCacheControl(value any) any {
	switch v := value.(type) {
	case map[string]any:
		normalized := map[string]any{}
		for key, item := range v {
			if key == "cache_control" {
				continue
			}
			normalized[key] = withoutCacheControl(item)
		}
		// 剥除后只剩 type=text + text 的单 text 块还原为纯字符串
		if t, _ := normalized["type"].(string); t == "text" && len(normalized) == 2 {
			if text, hasText := normalized["text"].(string); hasText {
				return text
			}
		}
		// content 为单字符串元素的 list 还原为字符串
		if content, ok := normalized["content"].([]any); ok && len(content) == 1 {
			if s, isStr := content[0].(string); isStr {
				normalized["content"] = s
			}
		}
		return normalized
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = withoutCacheControl(item)
		}
		return out
	default:
		return value
	}
}

// containsCacheControl 递归判断 value 中是否含 cache_control 键。
// Python: openai_model_client.py:201-207
func containsCacheControl(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v["cache_control"]; ok {
			return true
		}
		for _, item := range v {
			if containsCacheControl(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsCacheControl(item) {
				return true
			}
		}
	}
	return false
}

// buildCacheControlMarker 构建 ephemeral 标记，enable1HTTL 时附 ttl:"1h"。
// Python: openai_model_client.py:211-216
func buildCacheControlMarker(enable1HTTL bool) map[string]any {
	marker := map[string]any{"type": "ephemeral"}
	if enable1HTTL {
		marker["ttl"] = "1h"
	}
	return marker
}

// addCacheControlMarker setdefault 语义：已有 cache_control 则不动。
// Python: openai_model_client.py:218-220
func addCacheControlMarker(block map[string]any, enable1HTTL bool) map[string]any {
	if _, ok := block["cache_control"]; !ok {
		block["cache_control"] = buildCacheControlMarker(enable1HTTL)
	}
	return block
}

// markMessageWithCacheControl 给消息打标记：已含标记跳过；list content 给尾块（dict 直改，
// 非 dict 包成 text 块）；其余（字符串/None）整体包成单 text 块。
// Python: openai_model_client.py:223-246
func markMessageWithCacheControl(message map[string]any, enable1HTTL bool) {
	if containsCacheControl(message) {
		return
	}
	content, present := message["content"]
	list, isList := content.([]any)
	if present && isList {
		if len(list) == 0 {
			return
		}
		last := len(list) - 1
		if blk, ok := list[last].(map[string]any); ok {
			addCacheControlMarker(blk, enable1HTTL)
		} else {
			// 非 dict 尾块包成 text 块（Python str(last_block) 对齐）
			text := ""
			switch v := list[last].(type) {
			case string:
				text = v
			default:
				text = fmt.Sprintf("%v", v)
			}
			list[last] = addCacheControlMarker(map[string]any{"type": "text", "text": text}, enable1HTTL)
		}
		return
	}
	// 非 list content：字符串原样，None→空串，其余 str() 化
	text := ""
	if present && content != nil {
		if s, ok := content.(string); ok {
			text = s
		} else {
			text = fmt.Sprintf("%v", content)
		}
	}
	message["content"] = []any{addCacheControlMarker(map[string]any{"type": "text", "text": text}, enable1HTTL)}
}

// longestPrefixOverlapIndex 返回与上一轮 messages 最长公共前缀的最后一条下标；
// 无 previous 或零重叠返回 -1（对应 Python None）。比较前先剥 cache_control。
// Python: openai_model_client.py:248-261
func longestPrefixOverlapIndex(previous []any, current []any) int {
	if len(previous) == 0 {
		return -1
	}
	overlap := 0
	for i := 0; i < len(previous) && i < len(current); i++ {
		if !reflect.DeepEqual(withoutCacheControl(previous[i]), withoutCacheControl(current[i])) {
			break
		}
		overlap++
	}
	if overlap == 0 {
		return -1
	}
	return overlap - 1
}

// applyOpenrouterPromptCacheControl 打标记主函数：tools 尾块 + messages[0] +
// 最长前缀尾消息（若命中）+ messages[-1]。
// Python: openai_model_client.py:263-285
func applyOpenrouterPromptCacheControl(params map[string]any, previous []any, enable1HTTL bool) {
	// Python :265-268 tools 尾块
	tools, _ := params["tools"].([]any)
	if len(tools) > 0 {
		if blk, ok := tools[len(tools)-1].(map[string]any); ok {
			addCacheControlMarker(blk, enable1HTTL)
		}
	}
	// Python :270-272 messages 缺失或为空直接返回
	messages, _ := params["messages"].([]any)
	if len(messages) == 0 {
		return
	}
	// Python :274 最长公共前缀下标
	prefixIndex := longestPrefixOverlapIndex(previous, messages)
	// Python :276-277 messages[0]
	if blk, ok := messages[0].(map[string]any); ok {
		markMessageWithCacheControl(blk, enable1HTTL)
	}
	// Python :278-279 前缀尾消息
	if prefixIndex >= 0 {
		if blk, ok := messages[prefixIndex].(map[string]any); ok {
			markMessageWithCacheControl(blk, enable1HTTL)
		}
	}
	// Python :280-281 messages[-1]
	if blk, ok := messages[len(messages)-1].(map[string]any); ok {
		markMessageWithCacheControl(blk, enable1HTTL)
	}
}
