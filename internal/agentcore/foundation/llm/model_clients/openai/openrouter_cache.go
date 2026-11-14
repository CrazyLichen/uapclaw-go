package openai

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// OpenRouter 显式 prompt 缓存（cache_control 注入）写链。
// Python: openjiuwen/core/foundation/llm/model_clients/openai_model_client.py:95-285
// 读链（cache 用量解析）已由 model_clients/usage_extract.go 承担，本文件只做写链。

// ──────────────────────────── 结构体 ────────────────────────────

// openrouterCacheConfig OpenRouter 显式缓存的 5 个配置键解析结果。
// Python: openai_model_client.py:436-453 __init__ 的 extra.get(...)
type openrouterCacheConfig struct {
	// enableExplicitCaching 显式缓存总开关（openrouter_enable_explicit_prompt_caching，默认 true）
	enableExplicitCaching bool
	// enablePrefixMatching 跨轮最长前缀匹配开关（openrouter_enable_prompt_cache_prefix_matching，默认 true）
	enablePrefixMatching bool
	// enable1HTTL 1h TTL 开关（openrouter_enable_1h_prompt_cache_ttl，默认 false）
	enable1HTTL bool
	// explicitProviders 显式缓存 provider 集合
	explicitProviders map[string]struct{}
	// ttl1hProviders 1h TTL provider 集合
	ttl1hProviders map[string]struct{}
}

// ──────────────────────────── 常量 ────────────────────────────

var (
	// openrouterExplicitPromptCachingProviders 支持显式 prompt 缓存的 OpenRouter 底层 provider。
	// Python: openai_model_client.py:100-102
	openrouterExplicitPromptCachingProviders = map[string]struct{}{"anthropic": {}, "qwen": {}}

	// openrouter1HPromptCacheTTLProviders 支持 1h TTL 的 provider（仅 anthropic）。
	// Python: openai_model_client.py:104-106
	openrouter1HPromptCacheTTLProviders = map[string]struct{}{"anthropic": {}}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// applyOpenrouterProfile OpenRouter 显式缓存注入主入口。仅在 endpoint profile 为
// openrouter 时生效；步骤与 Python :1088-1161 逐条对应（方法体内步骤注释带行号）。
func (c *OpenAIModelClient) applyOpenrouterProfile(params map[string]any, modelName string) {
	// Python :1088-1090 非 openrouter profile 直接 return（不重置 previous）
	profile := strings.ToLower(strings.TrimSpace(c.ClientConfig.EndpointProfile))
	if profile != "openrouter" {
		return
	}
	// Python :1091-1095 总开关关：重置 previous 并 return
	if !c.enableOpenrouterExplicitCaching {
		c.previousOpenrouterPromptCacheMessages = nil
		return
	}
	// Python :1096-1105 模型不支持显式缓存：warning +（1h 开启时补 warning）+ 重置 previous + return
	if !supportsOpenrouterExplicitPromptCaching(modelName, c.openrouterCacheProviders) {
		logger.Warn(logComponent).
			Str("model_name", modelName).
			Msg("OpenRouter explicit prompt caching is enabled but unsupported for model; skipping cache_control markers.")
		if c.enableOpenrouter1HPromptCacheTTL {
			logger.Warn(logComponent).
				Str("model_name", modelName).
				Msg("OpenRouter 1h prompt-cache TTL is enabled but unsupported for model; the ttl flag will not be added.")
		}
		c.previousOpenrouterPromptCacheMessages = nil
		return
	}
	// Python :1106-1122 prefix matching：取上一轮做基线并保存本轮深拷贝；
	// messages / tools 深拷贝后改写（避免污染调用方结构）
	var previous []any
	currentMessages, _ := params["messages"].([]any)
	if c.enableOpenrouterPromptCachePrefixMatching {
		previous = c.previousOpenrouterPromptCacheMessages
		if currentMessages != nil {
			c.previousOpenrouterPromptCacheMessages = deepCopyMessages(currentMessages)
		} else {
			c.previousOpenrouterPromptCacheMessages = nil
		}
	} else {
		previous = nil
		c.previousOpenrouterPromptCacheMessages = nil
	}
	if currentMessages != nil {
		params["messages"] = deepCopyMessages(currentMessages)
	}
	if tools, ok := params["tools"].([]any); ok && len(tools) > 0 {
		params["tools"] = deepCopyMessages(tools)
	}
	// Python :1123-1136 enable1HTTL 计算 + 开启但不支持时 warning
	enable1HTTL := c.enableOpenrouter1HPromptCacheTTL &&
		supportsOpenrouter1HPromptCacheTTL(modelName, c.openrouter1HTTLProviders)
	if c.enableOpenrouter1HPromptCacheTTL && !enable1HTTL {
		logger.Warn(logComponent).
			Str("model_name", modelName).
			Msg("OpenRouter 1h prompt-cache TTL is enabled but unsupported for model; using default ephemeral cache_control markers.")
	}
	// Python :1137-1141 打标记
	applyOpenrouterPromptCacheControl(params, previous, enable1HTTL)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseOpenrouterCacheConfigExtra 解析 Extra 中的 5 个配置键。
// 布尔值对齐 Python truthiness：bool 原样；字符串 "true"/"false" 等兼容；未知形态回落默认。
// Python: openai_model_client.py:436-453
func parseOpenrouterCacheConfigExtra(extra map[string]any) openrouterCacheConfig {
	boolKey := func(key string, def bool) bool {
		v, ok := extra[key]
		if !ok || v == nil {
			return def
		}
		switch b := v.(type) {
		case bool:
			return b
		case string:
			s := strings.TrimSpace(strings.ToLower(b))
			switch s {
			case "true", "1", "yes", "on":
				return true
			case "false", "0", "no", "off", "":
				return false
			}
			return def
		default:
			return def
		}
	}
	cfg := openrouterCacheConfig{
		enableExplicitCaching: boolKey("openrouter_enable_explicit_prompt_caching", true),
		enablePrefixMatching:  boolKey("openrouter_enable_prompt_cache_prefix_matching", true),
		enable1HTTL:           boolKey("openrouter_enable_1h_prompt_cache_ttl", false),
	}
	// Python: :447-450 显式缓存 provider 集合
	cfg.explicitProviders = normalizeOpenrouterProviderSet(
		extra["openrouter_explicit_prompt_cache_providers"], openrouterExplicitPromptCachingProviders)
	// Python: :451-454 1h TTL provider 集合
	cfg.ttl1hProviders = normalizeOpenrouterProviderSet(
		extra["openrouter_prompt_cache_1h_ttl_providers"], openrouter1HPromptCacheTTLProviders)
	return cfg
}

// deepCopyMessages 任意 JSON 树的 []any 深拷贝（对齐 Python deepcopy，走 JSON 往返）。
// 仅在 profile 方法内对 messages/tools 使用，二者恒为 JSON 可序列化树。
func deepCopyMessages(value []any) []any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out []any
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}

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
