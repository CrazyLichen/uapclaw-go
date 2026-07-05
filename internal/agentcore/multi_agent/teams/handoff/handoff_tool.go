package handoff

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// HandoffTool 交接工具，用于将当前任务交接给目标 Agent 处理。
//
// 工具名称格式为 transfer_to_{targetID}，
// 调用后返回包含 HandoffTargetKey/HandoffMessageKey/HandoffReasonKey 的 map，
// 由 ExtractHandoffSignal 解析为 HandoffSignal。
//
// 对应 Python: HandoffTool(Tool)
type HandoffTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// targetID 目标 Agent ID
	targetID string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 HandoffTool 满足 Tool 接口
var _ tool.Tool = (*HandoffTool)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHandoffTool 创建交接工具实例。
//
// 参数：
//   - targetID：目标 Agent ID，工具名称为 transfer_to_{targetID}
//   - targetDescription：目标描述，追加到工具描述末尾（可选）
//
// 对应 Python: HandoffTool(target_id, target_description="")
func NewHandoffTool(targetID, targetDescription string) *HandoffTool {
	toolName := fmt.Sprintf("transfer_to_%s", targetID)
	description := fmt.Sprintf("Transfer the current task to %s for processing.", targetID)
	if targetDescription != "" {
		description += " " + targetDescription
	}

	inputParams := []*schema.Param{
		schema.NewStringParam(
			"reason",
			"Reason for handoff to the target agent.",
			true,
		),
		schema.NewStringParam(
			"message",
			"Context information to pass to the target agent.",
			false,
		),
	}

	card := tool.NewToolCard(toolName, description, inputParams, nil)

	return &HandoffTool{
		card:     card,
		targetID: targetID,
	}
}

// Card 返回工具配置卡片。
func (h *HandoffTool) Card() *tool.ToolCard {
	return h.card
}

// Invoke 执行交接工具，返回包含交接信号的 map。
//
// inputs 中可包含 reason 和 message 字段。
// 若 inputs 缺少 reason 或 message，对应字段返回空字符串。
//
// 对应 Python: HandoffTool.invoke(inputs, **kwargs)
func (h *HandoffTool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	reason := ""
	if r, ok := inputs["reason"]; ok {
		if rStr, ok := r.(string); ok {
			reason = rStr
		}
	}

	message := ""
	if m, ok := inputs["message"]; ok {
		if mStr, ok := m.(string); ok {
			message = mStr
		}
	}

	return map[string]any{
		HandoffTargetKey:  h.targetID,
		HandoffMessageKey: message,
		HandoffReasonKey:  reason,
	}, nil
}

// Stream 流式执行交接工具，一次性 yield 完整结果后关闭。
//
// 对应 Python: HandoffTool.stream(inputs, **kwargs) → yield await self.invoke(inputs, **kwargs)
func (h *HandoffTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	result, err := h.Invoke(ctx, inputs, opts...)
	if err != nil {
		return nil, err
	}

	ch := make(chan tool.StreamChunk, 2)
	ch <- tool.StreamChunk{Data: result, Done: false}
	ch <- tool.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}
