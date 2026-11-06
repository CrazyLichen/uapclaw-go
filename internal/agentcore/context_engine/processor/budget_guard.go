package processor

import (
	"strings"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
)

// ──────────────────────────── 结构体 ────────────────────────────

// contextWindowProvider 可选能力：上下文自带窗口上限。
//
// SessionModelContext 实现它（内部即 Python 的 context.context_window_tokens()，
// 已完成 resolve_context_max 解析）；测试桩可不实现，此时预算退化为只按调用预算收口。
type contextWindowProvider interface {
	// ContextMaxTokens 返回当前生效的上下文窗口上限
	ContextMaxTokens() int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TruncatedByBudgetMarker 预算截断标记
	TruncatedByBudgetMarker = "... placeholder truncated; original content is preserved in offload storage ..."
	// TruncatedSideMaxChars 截断时首尾各自保留的最大字符数
	TruncatedSideMaxChars = 2000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EffectiveContextBudget 返回最严格的可用上下文预算（窗口与单次调用预算取最小正值）。
//
// 候选与上游 budget_guard.effective_context_budget() 一致：上下文窗口、单次调用预算。
// 上游还有「resolve_context_max(model)」候选，本仓已由 ModelContext.ContextMaxTokens()
// 内部完成解析，故不重复。
// 返回 0 表示两者都未知（测试桩场景），调用方据此跳过预算守卫。
func EffectiveContextBudget(mc iface.ModelContext, callBudget int) int {
	candidates := make([]int, 0, 2)
	if provider, ok := mc.(contextWindowProvider); ok {
		if window := provider.ContextMaxTokens(); window > 0 {
			candidates = append(candidates, window)
		}
	}
	if callBudget > 0 {
		candidates = append(candidates, callBudget)
	}
	if len(candidates) == 0 {
		return 0
	}
	budget := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate < budget {
			budget = candidate
		}
	}
	return budget
}

// TruncateMessageContentToFixedHeadTailIfOverTokenLimit 超限时截成固定首尾预览并保留后缀。
//
// triggerTokenLimit 只决定是否触发；触发后保留固定大小的首尾，不保证回到限内。
// 返回内容不保证小于 triggerTokenLimit（与上游同语义：宁可留摘要也不留空洞）。
//
// Python: budget_guard.truncate_message_content_to_fixed_head_tail_if_over_token_limit()
func TruncateMessageContentToFixedHeadTailIfOverTokenLimit(
	content string,
	triggerTokenLimit int,
	countContentTokens func(string) int,
	preservedSuffix string,
) string {
	if triggerTokenLimit <= 0 {
		if preservedSuffix != "" {
			return preservedSuffix
		}
		return TruncatedByBudgetMarker
	}
	if countContentTokens(content) <= triggerTokenLimit {
		return content
	}

	body := content
	if preservedSuffix != "" && strings.HasSuffix(body, preservedSuffix) {
		body = strings.TrimRight(strings.TrimSuffix(body, preservedSuffix), " \t\r\n")
	}
	return buildHeadTail(body, preservedSuffix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildHeadTail 拼接首尾预览与截断标记。
func buildHeadTail(body, preservedSuffix string) string {
	if body == "" {
		return joinTruncatedParts("", "", preservedSuffix)
	}
	half := len(body) / 2
	headChars := TruncatedSideMaxChars
	if half < headChars {
		headChars = half
	}
	if headChars < 1 {
		headChars = 1
	}
	tailChars := len(body) - headChars
	if tailChars > TruncatedSideMaxChars {
		tailChars = TruncatedSideMaxChars
	}
	head := body[:headChars]
	tail := ""
	if tailChars > 0 {
		tail = body[len(body)-tailChars:]
	}
	return joinTruncatedParts(head, tail, preservedSuffix)
}

// joinTruncatedParts 以截断标记连接首尾片段，并补回后缀。
func joinTruncatedParts(head, tail, preservedSuffix string) string {
	parts := make([]string, 0, 2)
	if head != "" {
		parts = append(parts, head)
	}
	if tail != "" {
		parts = append(parts, tail)
	}
	body := TruncatedByBudgetMarker
	if len(parts) > 0 {
		body = strings.Join(parts, "\n"+TruncatedByBudgetMarker+"\n")
	}
	return body + preservedSuffix
}
