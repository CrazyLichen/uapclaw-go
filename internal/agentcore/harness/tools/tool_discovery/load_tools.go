package tool_discovery

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
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

// LoadToolsTool 加载真实工具到当前会话可见集合的元工具。
// 调用 loadToolsFn 执行工具加载。
// 对齐 Python: LoadToolsTool
type LoadToolsTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// loadToolsFn 加载工具回调
	loadToolsFn func(ctx context.Context, session interfaces.SessionFacade, toolNames []string, replace bool) (map[string]any, error)
	// language 语言
	language string
	// agentID Agent 标识
	agentID string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 LoadToolsTool 满足 Tool 接口
var _ tool.Tool = (*LoadToolsTool)(nil)

// loadLogComponent 日志组件标识
var loadLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoadToolsTool 创建加载工具元工具。
// 对齐 Python: LoadToolsTool.__init__
func NewLoadToolsTool(
	loadFn func(ctx context.Context, session interfaces.SessionFacade, toolNames []string, replace bool) (map[string]any, error),
	language string,
	agentID string,
) *LoadToolsTool {
	provider := &tools.LoadToolsMetadataProvider{}
	desc := provider.GetDescription(language)
	inputParams := buildLoadToolsInputParams()
	card := tool.NewToolCard("load_tools", desc, inputParams, map[string]any{
		"tool_id": "LoadToolsTool",
	})
	return &LoadToolsTool{
		card:        card,
		loadToolsFn: loadFn,
		language:    language,
		agentID:     agentID,
	}
}

// Card 返回工具配置卡片。
func (t *LoadToolsTool) Card() *tool.ToolCard {
	return t.card
}

// Invoke 加载指定工具到当前会话可见集合。
// 对齐 Python: LoadToolsTool.invoke
func (t *LoadToolsTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 步骤 1：解析输入参数
	var toolNames []string
	if v, ok := inputs["tool_names"]; ok {
		switch names := v.(type) {
		case []string:
			toolNames = names
		case []any:
			toolNames = make([]string, 0, len(names))
			for _, item := range names {
				if s, ok := item.(string); ok {
					toolNames = append(toolNames, s)
				}
			}
		}
	}

	replace := false
	if v, ok := inputs["replace"]; ok {
		switch r := v.(type) {
		case bool:
			replace = r
		}
	}

	// 步骤 2：获取会话
	callOpts := tool.NewToolCallOptions(opts...)
	var session interfaces.SessionFacade
	if callOpts.Session != nil {
		if sess, ok := callOpts.Session.(interfaces.SessionFacade); ok {
			session = sess
		}
	}

	// 步骤 3：调用加载回调
	result, err := t.loadToolsFn(ctx, session, toolNames, replace)
	if err != nil {
		logger.Warn(loadLogComponent).
			Str("tool_name", "load_tools").
			Strs("tool_names", toolNames).
			Bool("replace", replace).
			Err(err).
			Msg("LoadToolsTool 加载失败")
		return map[string]any{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	// 步骤 4：日志
	logger.Info(loadLogComponent).
		Str("tool_name", "load_tools").
		Strs("tool_names", toolNames).
		Bool("replace", replace).
		Msg("LoadToolsTool 加载完成")

	// 步骤 5：返回结果
	return result, nil
}

// Stream 不支持流式调用。
func (t *LoadToolsTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported("load_tools")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildLoadToolsInputParams 构建 load_tools 工具的输入参数。
func buildLoadToolsInputParams() []*schema.Param {
	return []*schema.Param{
		schema.NewArrayParam(
			"tool_names",
			"要在当前会话中可见的工具名称列表",
			true,
			schema.NewStringParam("item", "工具名称", false),
		),
		schema.NewBooleanParam("replace", "如果为 true，替换当前可见工具集，否则合并", false),
	}
}
