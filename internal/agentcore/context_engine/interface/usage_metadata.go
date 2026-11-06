package iface

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	common_schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ContextUsageStaleKey 消息 metadata 上的"用量记录已过期"标记键
	ContextUsageStaleKey = "context_usage_stale"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// InvalidateUsageMetadata 给保留消息里带用量的 assistant 打"过期"标记。
//
// provider 上报的 usage 是"那条 assistant 之前全部输入"的累计基线；
// 一旦上下文被改写（替换/弹出/压缩/换模型/恢复快照），旧基线不再描述当前前缀，
// 必须作废，否则统计会一直沿用压缩前的数字。
//
// Python: ContextUtils.invalidate_usage_metadata()
func InvalidateUsageMetadata(messages []llm_schema.BaseMessage) {
	for _, message := range messages {
		assistant, ok := message.(*llm_schema.AssistantMessage)
		if !ok || assistant.UsageMetadata == nil {
			continue
		}
		metadata := assistant.GetMetadata()
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata[ContextUsageStaleKey] = true
		assistant.SetMetadata(metadata)
	}
}

// HasValidUsageMetadata 判断某条消息的用量记录是否仍描述其前缀。
//
// 三个条件同时满足才算有效：是 assistant、有用量、总量为正、且未被打过期标记。
//
// Python: ContextUtils.has_valid_usage_metadata()
func HasValidUsageMetadata(message llm_schema.BaseMessage) bool {
	assistant, ok := message.(*llm_schema.AssistantMessage)
	if !ok || assistant.UsageMetadata == nil || assistant.UsageMetadata.TotalTokens <= 0 {
		return false
	}
	metadata := assistant.GetMetadata()
	if metadata == nil {
		return true
	}
	stale, _ := metadata[ContextUsageStaleKey].(bool)
	return !stale
}

// CountCategoryTokens 独立统计三块 token：系统提示词 / 工具定义 / 对话消息。
//
// 与 StatMessages 的区别：**不读 usage 基线**。StatMessages 在有基线时早退，
// 分类字段会保持 0；而用量快照需要四类各自的数量，因此另走这条纯计数路径。
//
// Python: usage/analyzer.py 的分类统计（不依赖 _stat_messages 的早退分支）
func CountCategoryTokens(
	messages []llm_schema.BaseMessage,
	tools []common_schema.ToolInfoInterface,
	counter token.TokenCounter,
) (systemTokens int, toolsTokens int, messageTokens int) {
	for _, message := range messages {
		tokens := countSingleMessageTokens(message, counter)
		if message.GetRole() == llm_schema.RoleTypeSystem {
			systemTokens += tokens
			continue
		}
		messageTokens += tokens
	}
	if len(tools) > 0 {
		if counter != nil {
			if counted, err := counter.CountTools(tools, ""); err == nil {
				toolsTokens = counted
			}
		}
	}
	return systemTokens, toolsTokens, messageTokens
}

// ──────────────────────────── 非导出函数 ────────────────────────────
