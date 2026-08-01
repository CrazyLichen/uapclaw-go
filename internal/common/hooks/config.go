package hooks

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommandHookConfig command 类型 hook 配置，对齐 Python CommandHookConfig dataclass
type CommandHookConfig struct {
	// Type 类型标识，默认 "command"
	Type string `json:"type"`
	// Command shell 命令
	Command string `json:"command"`
	// Timeout 超时秒数，默认 30
	Timeout int `json:"timeout"`
	// Shell 执行器，默认 "bash"
	Shell string `json:"shell"`
	// StatusMessage 状态消息
	StatusMessage string `json:"status_message"`
}

// PromptHookConfig prompt 类型 hook 配置，对齐 Python PromptHookConfig dataclass
type PromptHookConfig struct {
	// Type 类型标识，默认 "prompt"
	Type string `json:"type"`
	// Prompt 模板字符串
	Prompt string `json:"prompt"`
	// Timeout 超时秒数，默认 15
	Timeout int `json:"timeout"`
	// Model LLM 模型名
	Model string `json:"model"`
	// StatusMessage 状态消息
	StatusMessage string `json:"status_message"`
}

// HookMatcher hook 匹配器，对齐 Python HookMatcher dataclass
type HookMatcher struct {
	// Matcher 匹配表达式，默认 "*"（通配/OR/正则/精确）
	Matcher string `json:"matcher"`
	// Hooks hook 配置列表
	Hooks []map[string]any `json:"hooks"`
}

// HooksConfig hooks 配置聚合，对齐 Python HooksConfig dataclass
type HooksConfig struct {
	// Events 事件 → matcher 列表
	Events map[string][]HookMatcher `json:"events"`
	// DisableAllHooks 禁用所有 hooks
	DisableAllHooks bool `json:"disable_all_hooks"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Matches 检查 query 是否匹配此 matcher，对齐 Python HookMatcher.matches()
// 支持：*（匹配所有）| |分隔的 OR 匹配 | 正则匹配 | 精确匹配
func (m *HookMatcher) Matches(query string) bool {
	pattern := m.Matcher
	if pattern == "" {
		pattern = "*"
	}
	if pattern == "*" {
		return true
	}
	// "|" 分隔的 OR 匹配（不以 ^ 开头时才走 OR 逻辑，对齐 Python）
	if strings.Contains(pattern, "|") && !strings.HasPrefix(pattern, "^") {
		parts := strings.Split(pattern, "|")
		for _, p := range parts {
			if matchSingle(strings.TrimSpace(p), query) {
				return true
			}
		}
		return false
	}
	return matchSingle(pattern, query)
}

// Match 获取匹配该事件 + query 的所有 hook 配置，对齐 Python HooksConfig.match()
func (c *HooksConfig) Match(event, query string) []map[string]any {
	if c.DisableAllHooks {
		return nil
	}
	matchers := c.Events[event]
	var result []map[string]any
	for _, m := range matchers {
		if m.Matches(query) {
			result = append(result, m.Hooks...)
		}
	}
	return result
}

// GetEventSummary 返回各事件的 hook 数量摘要，对齐 Python HooksConfig.get_event_summary()
func (c *HooksConfig) GetEventSummary() []map[string]any {
	allEvents := allHookEventValues()
	summaries := make([]map[string]any, 0, len(allEvents))
	for _, eventName := range allEvents {
		matchers := c.Events[eventName]
		totalHooks := 0
		matcherDetails := make([]map[string]any, 0, len(matchers))
		for _, m := range matchers {
			totalHooks += len(m.Hooks)
			matcherDetails = append(matcherDetails, map[string]any{
				"matcher":    m.Matcher,
				"hook_count": len(m.Hooks),
				"hooks":      m.Hooks,
			})
		}
		summaries = append(summaries, map[string]any{
			"name":        eventName,
			"total_hooks": totalHooks,
			"matchers":    matcherDetails,
		})
	}
	return summaries
}

// LoadHooksConfig 从 config.yaml 的 hooks 段加载配置，对齐 Python load_hooks_config()
func LoadHooksConfig(configBase map[string]any) *HooksConfig {
	if configBase == nil {
		return &HooksConfig{}
	}
	hooksSection, ok := configBase["hooks"]
	if !ok {
		return &HooksConfig{}
	}
	hooksMap, ok := hooksSection.(map[string]any)
	if !ok {
		return &HooksConfig{}
	}

	disableAll := false
	if v, ok := hooksMap["disable_all_hooks"]; ok {
		disableAll = toBool(v)
	}

	events := make(map[string][]HookMatcher)
	for _, eventName := range allHookEventValues() {
		eventConfigs, ok := hooksMap[eventName]
		if !ok {
			continue
		}
		configsList, ok := eventConfigs.([]any)
		if !ok {
			logger.Warn(logger.ComponentCommon).
				Str("event", eventName).
				Msg("hooks 配置：event 段期望 []any 类型")
			continue
		}
		var matchers []HookMatcher
		for _, entry := range configsList {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				logger.Warn(logger.ComponentCommon).
					Str("event", eventName).
					Msg("hooks 配置：entry 期望 map[string]any 类型")
				continue
			}
			matcherStr := "*"
			if v, ok := entryMap["matcher"]; ok {
				if s, ok := v.(string); ok {
					matcherStr = s
				}
			}
			var hooksList []map[string]any
			if v, ok := entryMap["hooks"]; ok {
				if list, ok := v.([]any); ok {
					for _, h := range list {
						if hm, ok := h.(map[string]any); ok {
							hooksList = append(hooksList, hm)
						}
					}
				}
			}
			matchers = append(matchers, HookMatcher{
				Matcher: matcherStr,
				Hooks:   hooksList,
			})
		}
		if len(matchers) > 0 {
			events[eventName] = matchers
		}
	}
	return &HooksConfig{Events: events, DisableAllHooks: disableAll}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// matchSingle 单个模式匹配，对齐 Python HookMatcher._match_single()
func matchSingle(pattern, query string) bool {
	if pattern == query {
		return true
	}
	// 正则匹配：pattern 以 ^ 开头或以 $ 结尾或含 .*，对齐 Python
	if strings.HasPrefix(pattern, "^") || strings.HasSuffix(pattern, "$") || strings.Contains(pattern, ".*") {
		matched, err := regexp.MatchString(pattern, query)
		if err != nil {
			return false
		}
		return matched
	}
	return false
}

// allHookEventValues 返回所有 HookEvent 常量值的有序列表
func allHookEventValues() []string {
	return []string{
		HookEventPreToolUse, HookEventPostToolUse, HookEventPostToolUseFailure,
		HookEventStop, HookEventUserPromptSubmit, HookEventSessionStart,
		HookEventSessionEnd, HookEventNotification, HookEventPermissionRequest,
		HookEventPermissionDenied, HookEventSubagentStart, HookEventSubagentStop,
		HookEventConfigChange, HookEventInstructionsLoaded, HookEventSetup,
		HookEventBeforeModelCall, HookEventAfterModelCall,
	}
}

// toBool 将 any 转为 bool
func toBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// 确保 fmt import 不被移除（调试用）
var _ = fmt.Sprintf
