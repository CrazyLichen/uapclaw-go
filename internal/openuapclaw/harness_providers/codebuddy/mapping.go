package codebuddy

import (
	"encoding/json"
	"fmt"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// ──────────────────────────── 常量 ────────────────────────────

// providerName ProviderEvent 的 provider 命名空间。
// Python 无对应物（偏差 D-15.22-a）；dsh 同位常量形态参考 mapping.py:37。
const providerName = "codebuddy"

// ──────────────────────────── 结构体 ────────────────────────────

// MappedCodebuddyEvent 一个规约后的 payload 加信封级 provider 关联。
// Python 无对应物；形态对齐 dsh mapping.py:54-61 MappedDshEvent。
type MappedCodebuddyEvent struct {
	// Payload 规约后的协议事件 payload
	Payload harnessprotocol.HarnessEventPayload
	// ItemID 事件关于的 provider item（空串表示 nil）
	ItemID string
}

// contentBlock assistant 帧的 content block（Anthropic 同款形状）。
type contentBlock struct {
	// Type 块类型：text / tool_use
	Type string `json:"type"`
	// Text text 块文本
	Text string `json:"text,omitempty"`
	// ID tool_use 块调用 id
	ID string `json:"id,omitempty"`
	// Name tool_use 块工具名
	Name string `json:"name,omitempty"`
}

// assistantMessage assistant 帧的 message 体。
type assistantMessage struct {
	// ID 消息 id
	ID string `json:"id,omitempty"`
	// Content content blocks
	Content []contentBlock `json:"content"`
}

// CodebuddyTurnAccumulator 规约一个 CLI 活动区间所需的状态。
// Python 无对应物；形态对齐 dsh mapping.py:63-76 DshTurnAccumulator。
type CodebuddyTurnAccumulator struct {
	// TurnID 所属宿主轮次标识
	TurnID string
	// RootSessionID CLI 会话标识
	RootSessionID string
	// lastTextOutput 最终答复文本投影
	lastTextOutput string
	// usage 最近一次 usage_update 快照（CLI 快照语义：整轮累计）
	usage *harnessprotocol.TurnUsage
	// costMicros 累计金额（微单位，USD；CLI credits=0 时恒 0）
	costMicros int64
}

// NewCodebuddyTurnAccumulator 构造 accumulator。
func NewCodebuddyTurnAccumulator(turnID, rootSessionID string) *CodebuddyTurnAccumulator {
	return &CodebuddyTurnAccumulator{
		TurnID:        turnID,
		RootSessionID: rootSessionID,
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Consume 规约一条 CLI 线帧。
// Python 无对应物；四类帧映射按 CLI 2.181.0 实测（偏差 D-15.22-c）：
//   - assistant：text 块 → ANSWER 通道 OutputEvent(delta)；tool_use 块 →
//     tool item STARTED + COMPLETED（CLI 只报 tool_use 起始，完成语义并到
//     result 帧前——注释记录，非上游步骤）
//   - system/usage_update：快照累积 usage
//   - result：不产事件，终态由 BuildTerminalResult 合成
func (a *CodebuddyTurnAccumulator) Consume(frame WireFrame) ([]*MappedCodebuddyEvent, error) {
	switch frame.Type {
	case "assistant":
		return a.mapAssistantFrame(frame)
	case "system":
		if frame.Subtype == "usage_update" {
			if err := a.accumulateUsage(frame); err != nil {
				return nil, err
			}
		}
		// 其余 system 帧（init/status）不产轮内观测事件：
		// STARTED 由基座在轮次启动时发射（base.go emitTurnLifecycle），
		// 会话标识由 harness 层记账
		return nil, nil
	default:
		// result 帧与其余未知帧不产事件
		return nil, nil
	}
}

// BuildTerminalResult 在 result 帧处构建终态结果。
// success → FINISHED；is_error / error_* → FAILED（failed 必须带结构化
// error 的协议不变量由 NewTurnResult 步骤 5 强制）。
func (a *CodebuddyTurnAccumulator) BuildTerminalResult(
	frame WireFrame,
	startedAt float64,
) (harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	duration := int64(0)
	if frame.DurationMs != nil {
		duration = *frame.DurationMs
	}
	durationMS := int(duration)
	completedAt := startedAt + float64(duration)/1000.0
	opts := harnessprotocol.ResultOptions{
		FinalOutput:  a.finalOutput(frame),
		CompletedAt:  &completedAt,
		DurationMS:   &durationMS,
		Usage:        a.usage,
		ProviderData: harnessprotocol.JsonObject{"provider": providerName},
	}
	if !frame.IsError && frame.Subtype == "success" {
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusCompleted, opts)
		return harnessprotocol.TurnEventKindFinished, result, err
	}
	message := frame.Result
	if message == "" {
		message = "codebuddy turn failed (" + frame.Subtype + ")"
	}
	errorTurn, err := harnessprotocol.NewTurnError(message, nil, nil, nil,
		harnessprotocol.JsonObject{"provider": providerName, "subtype": frame.Subtype})
	if err != nil {
		return "", harnessprotocol.TurnResult{}, err
	}
	opts.Error = &errorTurn
	opts.FinalOutput = nil
	result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusFailed, opts)
	return harnessprotocol.TurnEventKindFailed, result, err
}

// LastTextOutput 返回最终答复文本投影。
func (a *CodebuddyTurnAccumulator) LastTextOutput() string { return a.lastTextOutput }

// Usage 返回最近一次 usage 快照（无 usage_update 帧时为 nil）。
func (a *CodebuddyTurnAccumulator) Usage() *harnessprotocol.TurnUsage { return a.usage }

// ──────────────────────────── 非导出函数 ────────────────────────────

// mapAssistantFrame 映射 assistant 帧的 content blocks。
func (a *CodebuddyTurnAccumulator) mapAssistantFrame(frame WireFrame) ([]*MappedCodebuddyEvent, error) {
	if len(frame.Message) == 0 {
		return nil, nil
	}
	var message assistantMessage
	if err := jsonUnmarshalStrictish(frame.Message, &message); err != nil {
		return nil, fmt.Errorf("codebuddy assistant frame decode failed: %w", err)
	}
	var mapped []*MappedCodebuddyEvent
	for _, block := range message.Content {
		switch block.Type {
		case "text":
			if block.Text == "" {
				continue
			}
			a.lastTextOutput += block.Text
			output, err := harnessprotocol.NewOutputEvent(
				"out-"+frame.UUID, harnessprotocol.OutputKindText, block.Text,
				harnessprotocol.OutputOperationDelta, harnessprotocol.OutputChannelAnswer,
				0, "", nil)
			if err != nil {
				return nil, err
			}
			mapped = append(mapped, &MappedCodebuddyEvent{Payload: output})
		case "tool_use":
			// CLI 只报 tool_use 起始：STARTED + COMPLETED 同帧补齐（完成语义
			// 并到 result 帧前——CLI 无 tool_result 线帧，注释记录）
			started, err := harnessprotocol.NewItemLifecycleEvent(
				harnessprotocol.ItemEventKindStarted, "tool",
				harnessprotocol.JsonObject{"name": block.Name, "id": block.ID})
			if err != nil {
				return nil, err
			}
			completed, err := harnessprotocol.NewItemLifecycleEvent(
				harnessprotocol.ItemEventKindCompleted, "tool",
				harnessprotocol.JsonObject{"name": block.Name, "id": block.ID})
			if err != nil {
				return nil, err
			}
			mapped = append(mapped,
				&MappedCodebuddyEvent{Payload: started, ItemID: block.ID},
				&MappedCodebuddyEvent{Payload: completed, ItemID: block.ID})
		}
	}
	return mapped, nil
}

// accumulateUsage 累积 usage_update 帧的整轮快照。
func (a *CodebuddyTurnAccumulator) accumulateUsage(frame WireFrame) error {
	total := metaTurnUsageTotal(frame.Meta)
	if total == nil {
		return nil
	}
	usageMap, _ := total["usage"].(map[string]any)
	if usageMap == nil {
		return nil
	}
	inputTokens := intFromAny(usageMap["input_tokens"])
	outputTokens := intFromAny(usageMap["output_tokens"])
	cachedInputTokens := intFromAny(usageMap["cache_read_input_tokens"])
	totalTokens := intFromAny(usageMap["input_tokens"]) + intFromAny(usageMap["output_tokens"])
	usage, err := harnessprotocol.NewTurnUsage(
		&inputTokens, &outputTokens, &cachedInputTokens, nil, &totalTokens,
		harnessprotocol.JsonObject{"provider": providerName})
	if err != nil {
		return err
	}
	a.usage = &usage
	return nil
}

// finalOutput result 帧的终态输出：result 文本优先，回落累积 answer。
func (a *CodebuddyTurnAccumulator) finalOutput(frame WireFrame) any {
	if frame.Result != "" {
		return frame.Result
	}
	return a.lastTextOutput
}

// metaTurnUsageTotal 从 _meta 提取 codebuddy.ai/turnUsage.total。
func metaTurnUsageTotal(meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}
	turnUsage, _ := meta["codebuddy.ai/turnUsage"].(map[string]any)
	if turnUsage == nil {
		return nil
	}
	total, _ := turnUsage["total"].(map[string]any)
	return total
}

// intFromAny any → int（JSON 数字惯例 float64）。
func intFromAny(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return 0
}

// jsonUnmarshalStrictish json.Unmarshal 别名（与 dsh 的 marshalJSONValue
// 同位助手；保持映射面单点依赖标准库）。
func jsonUnmarshalStrictish(data []byte, target any) error {
	return json.Unmarshal(data, target)
}
