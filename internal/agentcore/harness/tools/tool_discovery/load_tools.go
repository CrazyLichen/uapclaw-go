package tool_discovery

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoadToolsInput load_tools 工具的输入参数。
// 对齐 Python: LoadToolsInput
type LoadToolsInput struct {
	// ToolNames 要加载的工具名称列表
	ToolNames []string `json:"tool_names"`
	// Replace 是否替换当前可见工具集，默认 false（合并）
	Replace bool `json:"replace"`
}

// ──────────────────────────── 全局变量 ────────────────────────────

// loadLogComponent 日志组件标识
var loadLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoadToolsTool 创建加载工具元工具。
// 对齐 Python: LoadToolsTool.__init__
func NewLoadToolsTool(
	loadFn func(ctx context.Context, session interfaces.SessionFacade, toolNames []string, replace bool) (map[string]any, error),
	language string,
	agentID string,
) tool.Tool {
	card, _ := tools.BuildToolCard("load_tools", "LoadToolsTool", language, nil, agentID)

	fn := func(ctx context.Context, input LoadToolsInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 获取会话
		callOpts := tool.NewToolCallOptions(opts...)
		var session interfaces.SessionFacade
		if callOpts.Session != nil {
			if sess, ok := callOpts.Session.(interfaces.SessionFacade); ok {
				session = sess
			}
		}

		// 调用加载回调
		result, err := loadFn(ctx, session, input.ToolNames, input.Replace)
		if err != nil {
			logger.Warn(loadLogComponent).
				Str("tool_name", "load_tools").
				Strs("tool_names", input.ToolNames).
				Bool("replace", input.Replace).
				Err(err).
				Msg("LoadToolsTool 加载失败")
			return map[string]any{"success": false, "error": err.Error()}, nil
		}

		// 日志
		logger.Info(loadLogComponent).
			Str("tool_name", "load_tools").
			Strs("tool_names", input.ToolNames).
			Bool("replace", input.Replace).
			Msg("LoadToolsTool 加载完成")

		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
	return invokeFn
}
