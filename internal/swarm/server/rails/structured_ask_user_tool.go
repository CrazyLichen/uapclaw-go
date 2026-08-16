package rails

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStructuredAskUserTool 创建支持结构化选项的 AskUserTool 空壳实例。
// 使用 BuildToolCard 从注册表获取 schema（AskUserMetadataProvider 已有 questions 参数），
// 然后覆盖描述文本为结构化版本（对齐 Python EXTENDED_DESCRIPTION_EN/CN）。
//
// 对齐 Python: StructuredAskUserTool(Tool) — jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
func NewStructuredAskUserTool(language, agentID string) (tool.Tool, error) {
	// 使用 BuildToolCard 获取 schema（AskUserMetadataProvider 已注册 questions 参数）
	card, err := hprompts.BuildToolCard("ask_user", "ask_user", language, nil, agentID)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "structured_ask_user_tool_create").
			Err(err).
			Msg("构建 StructuredAskUserTool ToolCard 失败")
		return nil, fmt.Errorf("构建 StructuredAskUserTool ToolCard 失败: %w", err)
	}

	// 覆盖描述文本为结构化版本（对齐 Python EXTENDED_DESCRIPTION）
	card.Description = getStructuredDescription(language)

	// 空壳 invoke：返回空 map（对齐 Python StructuredAskUserTool.invoke → return {}）
	askUserTool, err := tool.NewMapFunction(
		card,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{}, nil
		},
		nil,
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "structured_ask_user_tool_create").
			Err(err).
			Msg("创建 StructuredAskUserTool 失败")
		return nil, fmt.Errorf("创建 StructuredAskUserTool 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("event_type", "structured_ask_user_tool_create").
		Str("tool_id", card.ID).
		Msg("StructuredAskUserTool 创建成功")

	return askUserTool, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getStructuredDescription 返回结构化 ask_user 工具描述。
// 对齐 Python: EXTENDED_DESCRIPTION_EN / EXTENDED_DESCRIPTION_CN
func getStructuredDescription(language string) string {
	if language == "en" {
		return "Interrupts execution and requests input from the user. " +
			"Supports two modes:\n" +
			"1. Plain query (free-text): pass only `query` — the user types their answer.\n" +
			"2. Structured questions (multi-choice): pass `query` + `questions` — " +
			"the user selects from predefined options. " +
			"Use `questions` when you want the user to choose between specific options " +
			"(e.g., 'Apply update' vs 'Skip'). Each question can have 2-4 options."
	}
	return "中断执行并向用户请求输入。支持两种模式：\n" +
		"1. 纯文本查询：只传 `query` —— 用户自由输入回答。\n" +
		"2. 结构化选项：传 `query` + `questions` —— 用户从预定义选项中选择。" +
		"当你希望用户在特定选项间做选择时（如「应用更新」vs「跳过」）使用 `questions`。" +
		"每个问题可提供 2-4 个选项。"
}
