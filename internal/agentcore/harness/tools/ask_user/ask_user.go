package ask_user

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserTool 向用户提问的空壳工具。
// invoke/stream 返回空 map{}，真正逻辑在 AskUserRail 中通过中断机制完成。
//
// 对齐 Python: AskUserTool(Tool) — openjiuwen/harness/tools/ask_user.py
// Python 中 AskUserTool.invoke(query, **kwargs) return {} / stream(query, **kwargs) yield {}
type AskUserTool struct{}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAskUserTool 创建 AskUserTool 空壳实例。
// 从 prompts/tools 注册表获取 ToolCard，用 MapFunction 包装空壳 invoke 函数。
//
// 对齐 Python: AskUserTool.__init__(language, agent_id)
//
//	super().__init__(build_tool_card(name="ask_user", tool_id="ask_user", language=language, agent_id=agent_id))
func NewAskUserTool(language, agentID string) (tool.Tool, error) {
	card, err := hprompts.BuildToolCard("ask_user", "ask_user", language, nil, agentID)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "ask_user_tool_create").
			Err(err).
			Msg("构建 ask_user ToolCard 失败")
		return nil, fmt.Errorf("构建 ask_user ToolCard 失败: %w", err)
	}

	// 空壳 invoke：返回空 map（对齐 Python AskUserTool.invoke → return {}）
	askUserTool, err := tool.NewMapFunction(
		card,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{}, nil
		},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("创建 AskUserTool 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("event_type", "ask_user_tool_create").
		Str("tool_id", card.ID).
		Msg("AskUserTool 创建成功")

	return askUserTool, nil
}
