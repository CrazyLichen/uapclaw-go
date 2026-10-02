package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamTool 团队工具基类。
// Python: TeamTool(Tool, ABC)
// 子类嵌入 TeamTool 获得 Card() 默认实现，只需实现 Invoke()。
type TeamTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// MappedContentKey 工具返回 map 中的映射内容键。
// 对齐 Python: MappedToolOutput._mapped_content。
// BuildToolMessageContent 检测到此键时，优先返回其值作为 LLM 友好文本。
const MappedContentKey = "__mapped_content"

// teamToolLogComponent 日志组件标识
const teamToolLogComponent = logger.ComponentChannel

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamTool 创建 TeamTool 实例。
func NewTeamTool(card *tool.ToolCard) TeamTool {
	return TeamTool{card: card}
}

// Card 返回工具配置卡片。
func (t *TeamTool) Card() *tool.ToolCard {
	return t.card
}

// MapResult 将工具执行结果转换为 LLM 可读文本。
// 对齐 Python: TeamTool.map_result(output) → str
// 默认实现：成功时从 data 提取 key 信息，失败时返回 error。
// 子类应覆盖此方法提供更友好的人类可读输出。
func (t *TeamTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "operation failed"
	}
	data, _ := output["data"].(map[string]any)
	if data == nil {
		return "ok"
	}
	// 默认：简单 JSON 序列化 data
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "ok"
	}
	return string(jsonBytes)
}

// WrapInvokeWithLogging 包装工具的 Invoke 方法，添加日志和 map_result 逻辑。
// 对齐 Python: _wrap_invoke_with_logging(tool)
// 在 invoke 后自动调用 MapResult，将结果包装为 MappedToolOutput 等价格式。
func WrapInvokeWithLogging(t tool.Tool) tool.Tool {
	// 仅对 TeamTool 类型应用 map_result 包装
	teamTool, isTeamTool := t.(interface {
		MapResult(output map[string]any) string
	})
	_ = teamTool
	toolName := ""
	if card := t.Card(); card != nil {
		toolName = card.Name
	}

	// 返回包装后的工具
	return &loggedTeamTool{
		Tool:         t,
		toolName:     toolName,
		isTeamTool:   isTeamTool,
		teamToolImpl: teamTool,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loggedTeamTool 包装工具的 Invoke，添加日志和 map_result 逻辑。
// 对齐 Python: _wrap_invoke_with_logging 装饰器
type loggedTeamTool struct {
	tool.Tool
	// toolName 工具名称
	toolName string
	// isTeamTool 是否为 TeamTool 类型
	isTeamTool bool
	// teamToolImpl TeamTool 的 MapResult 方法（仅 isTeamTool 为 true 时非 nil）
	teamToolImpl interface {
		MapResult(output map[string]any) string
	}
}

// Invoke 包装原始 Invoke，添加日志和 map_result 逻辑。
func (l *loggedTeamTool) Invoke(ctx context.Context, args map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	logger.Debug(teamToolLogComponent).Str("tool_name", l.toolName).Msg("invoke start")

	result, err := l.Tool.Invoke(ctx, args, opts...)
	if err != nil {
		logger.Debug(teamToolLogComponent).Str("tool_name", l.toolName).Err(err).Msg("invoke error")
		return result, err
	}

	logger.Debug(teamToolLogComponent).Str("tool_name", l.toolName).Msg("invoke end")

	// 对齐 Python: if is_team_tool: mapped = tool.map_result(result); return MappedToolOutput.from_output(result, mapped)
	if l.isTeamTool && l.teamToolImpl != nil && result != nil {
		mapped := l.teamToolImpl.MapResult(result)
		if mapped != "" {
			// 在返回 map 中注入 __mapped_content，BuildToolMessageContent 优先使用
			result[MappedContentKey] = mapped
		}
	}

	return result, nil
}
