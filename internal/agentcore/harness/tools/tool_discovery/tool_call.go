package tool_discovery

import (
	"context"
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCallInput tool_call 工具的输入参数。
//
// Python: tool_call.py (ToolCallInput)
type ToolCallInput struct {
	// Name tool_search 返回的准确工具名称
	Name string `json:"name"`
	// Args 与 schema 匹配的工具参数
	Args map[string]any `json:"args"`
}

// RelayedToolOutput 携带目标工具模型可见渲染文本的包装结果。
//
// 渲染文本放在私有字段：结构化字段只含 data/error，转发文本单独给模型。
//
// Python: tool_call.py (RelayedToolOutput)
type RelayedToolOutput struct {
	// success 是否成功
	success bool
	// renderedText 目标工具的模型可见渲染文本
	renderedText string
	// data 结构化结果
	data map[string]any
	// err 错误信息
	err error
}

// ToolCallTool 固定的延迟工具执行包装器。
//
// 执行回调由 ProgressiveToolRail 提供；目标工具经 AbilityManager 嵌套执行，
// 从而保留其自身的 rail 生命周期。
//
// Python: harness/tools/tool_discovery/tool_call.py (ToolCallTool)
type ToolCallTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// dispatch 分发回调
	dispatch func(ctx context.Context, name string, args map[string]any, cb tool.ToolCallbackContext) (any, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// nestedToolCallKey 嵌套调用标记（写在回调上下文 Extra 上）
	nestedToolCallKey = "__progressive_nested_tool_call__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRelayedToolOutput 创建包装结果。
func NewRelayedToolOutput(success bool, renderedText string, data map[string]any, err error) *RelayedToolOutput {
	return &RelayedToolOutput{success: success, renderedText: renderedText, data: data, err: err}
}

// RenderedText 返回目标工具的模型可见渲染文本。
func (r *RelayedToolOutput) RenderedText() string { return r.renderedText }

// Success 返回是否成功。
func (r *RelayedToolOutput) Success() bool { return r.success }

// Data 返回结构化结果。
func (r *RelayedToolOutput) Data() map[string]any { return r.data }

// Err 返回错误。
func (r *RelayedToolOutput) Err() error { return r.err }

// ToMap 转换为工具返回 map；渲染文本走 mappedContentKey 交给模型。
//
// Python: ToolCallTool.render_for_llm 转发目标渲染文本
func (r *RelayedToolOutput) ToMap() map[string]any {
	payload := map[string]any{"success": r.success}
	if r.data != nil {
		payload["data"] = r.data
	}
	if r.err != nil {
		payload["error"] = r.err.Error()
	}
	if r.renderedText != "" {
		payload[mappedContentKey] = r.renderedText
	}
	return payload
}

// NewToolCallTool 创建执行包装器。
//
// Python: ToolCallTool.__init__
func NewToolCallTool(language string, agentID string) *ToolCallTool {
	card, err := tools.BuildToolCard("tool_call", "ToolCallTool", language, nil, agentID)
	if err != nil {
		// Python: build_tool_card 失败时退化为同名卡片
		card = tool.NewToolCardWithID("ToolCallTool", "tool_call", "tool_call", nil, nil)
	}
	// 元工具始终直接可调用，防止被渐进策略误藏
	card.Exposure = tool.ToolExposureDirect
	card.SetExposureDeclared(true)
	return &ToolCallTool{card: card}
}

// SetDispatch 注入分发回调。
func (t *ToolCallTool) SetDispatch(dispatch func(ctx context.Context, name string, args map[string]any, cb tool.ToolCallbackContext) (any, error)) {
	t.dispatch = dispatch
}

// AcceptsToolCallbackContext 声明需要执行期回调上下文。
//
// Python: ToolCallTool.accepts_tool_callback_context = True
func (t *ToolCallTool) AcceptsToolCallbackContext() bool { return true }

// Card 返回工具卡片。
func (t *ToolCallTool) Card() *tool.ToolCard { return t.card }

// Invoke 从调用选项取回调上下文后执行。
//
// Python: ToolCallTool.invoke
func (t *ToolCallTool) Invoke(ctx context.Context, inputs map[string]any, options ...tool.ToolOption) (map[string]any, error) {
	opts := &tool.ToolCallOptions{}
	for _, option := range options {
		option(opts)
	}
	out, err := t.InvokeWithCallback(ctx, inputs, opts.CallbackContext)
	if err != nil {
		return nil, err
	}
	return out.ToMap(), nil
}

// InvokeWithCallback 带回调上下文执行。
func (t *ToolCallTool) InvokeWithCallback(
	ctx context.Context,
	inputs map[string]any,
	cb tool.ToolCallbackContext,
) (*RelayedToolOutput, error) {
	name, _ := inputs["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("tool_call requires a non-empty tool name.")
	}
	args, _ := inputs["args"].(map[string]any)
	if cb == nil {
		return nil, errors.New("tool_call requires an active agent callback context")
	}
	result, err := t.dispatch(ctx, name, args, cb)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "tool_call_invoke_failed").
			Str("tool_name", name).
			Err(err).
			Msg("tool_call invoke failed")
		return nil, err
	}
	if relayed, ok := result.(*RelayedToolOutput); ok {
		return relayed, nil
	}
	return NewRelayedToolOutput(true, "", map[string]any{"name": name, "result": result}, nil), nil
}

// Stream 不支持流式。
//
// Python: ToolCallTool.stream（空实现）
func (t *ToolCallTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ──────────────────────────── 非导出函数 ────────────────────────────
