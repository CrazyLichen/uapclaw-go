package tool_discovery

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SearchToolsInput search_tools 工具的输入参数。
// 对齐 Python: SearchToolsInput
type SearchToolsInput struct {
	// Query 搜索查询文本
	Query string `json:"query"`
	// Limit 返回候选工具的最大数量，默认 10，范围 [1, 20]
	Limit int `json:"limit"`
	// DetailLevel 详情级别：1=name+描述, 2=+参数摘要, 3=+完整参数
	DetailLevel int `json:"detail_level"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// searchToolsLimitMin limit 最小值
	searchToolsLimitMin = 1
	// searchToolsLimitMax limit 最大值
	searchToolsLimitMax = 20
	// searchToolsLimitDefault limit 默认值
	searchToolsLimitDefault = 10
	// searchToolsDetailLevelDefault detail_level 默认值
	searchToolsDetailLevelDefault = 1

	// callabilityNote 严格对齐 Python SearchToolsTool.invoke 中的 callability_note
	callabilityNote = "Search results are discovery-only. Tools shown here are not callable until load_tools is called."
	// nextStepHint 严格对齐 Python SearchToolsTool.invoke 中的 next_step_hint
	nextStepHint = "If the result is clear enough, call load_tools directly. Increase detail_level to 2 or 3 when you need more parameter detail."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// searchLogComponent 日志组件标识
var searchLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSearchToolsTool 创建搜索候选工具元工具。
// 对齐 Python: SearchToolsTool.__init__
func NewSearchToolsTool(
	searchFn func(ctx context.Context, query string, limit int, detailLevel int) ([]map[string]any, error),
	traceFn func(session interfaces.SessionFacade, event map[string]any),
	language string,
	agentID string,
) tool.Tool {
	card, _ := tools.BuildToolCard("search_tools", "SearchToolsTool", language, nil, agentID)

	fn := func(ctx context.Context, input SearchToolsInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 限幅 limit 到 [1, 20]
		limit := input.Limit
		if limit < searchToolsLimitMin {
			limit = searchToolsLimitMin
		}
		if limit > searchToolsLimitMax {
			limit = searchToolsLimitMax
		}

		// 调用搜索回调
		matches, err := searchFn(ctx, input.Query, limit, input.DetailLevel)
		if err != nil {
			logger.Warn(searchLogComponent).
				Str("tool_name", "search_tools").
				Str("query", input.Query).
				Int("limit", limit).
				Int("detail_level", input.DetailLevel).
				Err(err).
				Msg("SearchToolsTool 搜索失败")
			return map[string]any{"success": false, "error": err.Error()}, nil
		}

		// 调用轨迹回调
		callOpts := tool.NewToolCallOptions(opts...)
		if traceFn != nil && callOpts.Session != nil {
			if sess, ok := callOpts.Session.(interfaces.SessionFacade); ok {
				traceFn(sess, map[string]any{
					"event_type": "tool_search",
					"query":      input.Query,
					"limit":      limit,
					"count":      len(matches),
					"agent_id":   agentID,
				})
			}
		}

		// 日志
		logger.Info(searchLogComponent).
			Str("tool_name", "search_tools").
			Str("query", input.Query).
			Int("limit", limit).
			Int("detail_level", input.DetailLevel).
			Int("match_count", len(matches)).
			Msg("SearchToolsTool 搜索完成")

		// 返回结果
		return map[string]any{
			"query":            input.Query,
			"matches":          matches,
			"count":            len(matches),
			"callability_note": callabilityNote,
			"next_step_hint":   nextStepHint,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatMatches 格式化匹配结果用于日志。
func formatMatches(matches []map[string]any) string {
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		if name, ok := m["name"].(string); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "[]"
	}
	return fmt.Sprintf("%v", names)
}
