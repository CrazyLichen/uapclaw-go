package dsh

import (
	"fmt"
	"strings"
	"time"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// ──────────────────────────── 常量 ────────────────────────────

// providerName ProviderEvent 的 provider 命名空间。
// Python: mapping.py:37 _PROVIDER
const providerName = "deepseek-harness"

// schemaVersion ProviderEvent 的 schema 版本。
// Python: mapping.py:38 _SCHEMA_VERSION
const schemaVersion = "1"

// ──────────────────────────── 结构体 ────────────────────────────

// MappedDshEvent 一个规约后的 payload 加信封级 provider 关联。
// Python: mapping.py:54-61 MappedDshEvent
type MappedDshEvent struct {
	// Payload 规约后的协议事件 payload
	Payload harnessprotocol.HarnessEventPayload
	// ItemID 事件关于的 provider item（空串表示 nil）
	ItemID string
	// ProviderSessionID 信封级 provider 会话覆盖（空串表示 nil）
	ProviderSessionID string
}

// DshTurnAccumulator 规约一个外部驱动的 DSH 活动区间所需的状态。
// Python: mapping.py:63-76 DshTurnAccumulator
type DshTurnAccumulator struct {
	// TurnID 所属宿主轮次标识
	TurnID string
	// RootSessionID DSH 根会话标识（区分 root/child 事件）
	RootSessionID string
	// messages 归一化消息序列（Python: messages）
	messages []harnessprotocol.TurnMessage
	// messageIds 已入列的 message_id 去重集（Python: _message_ids）
	messageIds map[string]bool
	// finalizedOutputIds 已发过 FINAL 的 output_id 集（Python: _finalized_output_ids）
	finalizedOutputIds map[string]bool
	// usageByStep 按 (native_turn, native_step) 去重的用量（Python: _usage_by_step）
	usageByStep map[[2]int]harnessprotocol.TurnUsage
	// toolNames call_id → 工具名记账（Python: _tool_names）
	toolNames map[string]string
	// lastTurnReason 最近一次 turn/end 的 reason（Python: last_turn_reason）
	lastTurnReason map[string]any
	// nativeTurnEndCount 原生 turn/end 计数（Python: native_turn_end_count）
	nativeTurnEndCount int
	// lastTextOutput 最终答复文本投影（Python: last_text_output）
	lastTextOutput string
}

// NewDshTurnAccumulator 构造 accumulator。
// Python: mapping.py:66-76 __init__
func NewDshTurnAccumulator(turnID, rootSessionID string) *DshTurnAccumulator {
	return &DshTurnAccumulator{
		TurnID:             turnID,
		RootSessionID:      rootSessionID,
		messageIds:         map[string]bool{},
		finalizedOutputIds: map[string]bool{},
		usageByStep:        map[[2]int]harnessprotocol.TurnUsage{},
		toolNames:          map[string]string{},
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Consume 规约一条 SDK 通知，不保留其可变容器。
// Python: mapping.py:78-100 consume
func (a *DshTurnAccumulator) Consume(notification deepseekharness.Notification) ([]*MappedDshEvent, error) {
	method := notification.Method
	payload := asMap(notification.Payload)
	if method == "subagent.started" || method == "subagent.finished" {
		mapped, err := a.mapSubagent(method, payload)
		if err != nil {
			return nil, err
		}
		return []*MappedDshEvent{mapped}, nil
	}
	if method != "session.event" {
		name := method
		if name == "" {
			name = "unknown-notification"
		}
		return a.providerEvents(name, map[string]any{"payload": mapAnyToAny(payload)}, "")
	}

	sessionID := asString(payload["sessionId"])
	if sessionID == "" {
		sessionID = a.RootSessionID
	}
	rawEvent := asMap(payload["event"])
	if sessionID != a.RootSessionID {
		if asString(rawEvent["type"]) == "turn/end" {
			rawEvent = mapAnyToAny(redactTurnEndEvent(rawEvent))
		}
		return a.providerEvents(
			"child.session.event",
			map[string]any{"event": mapAnyToAny(rawEvent)},
			sessionID,
		)
	}
	return a.mapRootSessionEvent(rawEvent)
}

// BuildTerminalResult 在 whole-agent idle 处构建唯一的外部终态结果。
// Python: mapping.py:102-187 build_terminal_result
func (a *DshTurnAccumulator) BuildTerminalResult(
	runResult *deepseekharness.RunResult,
	startedAt float64,
	startedMonotonic time.Time,
) (harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	completedAt := float64(time.Now().Unix()) // Python: time.time()
	durationMS := max(0, int(time.Since(startedMonotonic).Milliseconds()))

	finishReason := runResult.FinishReason
	if finishReason != nil {
		// Python: finish_reason 非串时 str 化（mapping.py:113-115）
		//（Go SDK 已保证 *string，此处仅保留非 nil 判断）
		if *finishReason == "" {
			finishReason = nil
		}
	}
	finalResponse := runResult.FinalResponse
	// Python: final_response 非串回落 last_text_output（mapping.py:116-118）
	providerData := map[string]any{
		"native_turn_end_count": a.nativeTurnEndCount,
	}
	if finishReason != nil {
		providerData["native_finish_reason"] = *finishReason
	}
	frozenProviderData, err := harnessprotocol.FreezeJsonObject(providerData)
	if err != nil {
		return "", harnessprotocol.TurnResult{}, err
	}

	common := harnessprotocol.ResultOptions{
		Messages:     a.messages,
		FinalOutput:  finalResponse,
		StopReason:   finishReason,
		Usage:        a.totalUsage(),
		StartedAt:    &startedAt,
		CompletedAt:  &completedAt,
		DurationMS:   &durationMS,
		ProviderData: frozenProviderData,
	}
	// 分支顺序与条件 = Python mapping.py:135-187 原序
	if finishReason == nil {
		code := "DSH_MISSING_TURN_END"
		turnErr, err := harnessprotocol.NewTurnError(
			"DeepSeek Harness became idle without ending a native turn",
			&code, strPtr("protocol"), nil, harnessprotocol.JsonObject{},
		)
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusFailed, harnessprotocol.ResultOptions{
			Messages: common.Messages, FinalOutput: common.FinalOutput,
			StopReason: common.StopReason, Usage: common.Usage,
			StartedAt: common.StartedAt, CompletedAt: common.CompletedAt,
			DurationMS: common.DurationMS, ProviderData: common.ProviderData,
			Error: &turnErr,
		})
		return harnessprotocol.TurnEventKindFailed, result, err
	}
	if *finishReason == "completed" || *finishReason == "max-tokens" {
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusCompleted, common)
		return harnessprotocol.TurnEventKindFinished, result, err
	}
	if *finishReason == "blocked" {
		message := "DeepSeek Harness blocked the turn"
		termination, err := harnessprotocol.NewTurnTermination(
			harnessprotocol.TurnTerminationKindPolicy, &message, nil, harnessprotocol.JsonObject{})
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, withTermination(common, &termination))
		return harnessprotocol.TurnEventKindAborted, result, err
	}
	if *finishReason == "aborted" || *finishReason == "interrupted" {
		kind := a.terminationKind(*finishReason)
		message := "DeepSeek Harness interrupted the turn"
		providerData, err := harnessprotocol.FreezeJsonObject(safeReasonData(a.lastTurnReason))
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		termination, err := harnessprotocol.NewTurnTermination(kind, &message, nil, providerData)
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, withTermination(common, &termination))
		return harnessprotocol.TurnEventKindAborted, result, err
	}
	if *finishReason == "error" {
		code := reasonErrorCode(a.lastTurnReason)
		providerData, err := harnessprotocol.FreezeJsonObject(safeReasonData(a.lastTurnReason))
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		turnErr, err := harnessprotocol.NewTurnError(
			"DeepSeek Harness reported a failed turn",
			code, strPtr("provider"), nil, providerData,
		)
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusFailed, withError(common, &turnErr))
		return harnessprotocol.TurnEventKindFailed, result, err
	}
	unknownProviderData, err := harnessprotocol.FreezeJsonObject(
		map[string]any{"finish_reason": *finishReason})
	if err != nil {
		return "", harnessprotocol.TurnResult{}, err
	}
	code := "DSH_UNKNOWN_FINISH_REASON"
	turnErr, err := harnessprotocol.NewTurnError(
		"DeepSeek Harness returned an unknown turn ending",
		&code, strPtr("protocol"), nil, unknownProviderData,
	)
	if err != nil {
		return "", harnessprotocol.TurnResult{}, err
	}
	result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusFailed, withError(common, &turnErr))
	return harnessprotocol.TurnEventKindFailed, result, err
}

// BuildFailedResult 规约一次 SDK 失败，不复制可能敏感的 stderr。
// Python: mapping.py:189-213 build_failed_result
func (a *DshTurnAccumulator) BuildFailedResult(
	exc error,
	startedAt float64,
	startedMonotonic time.Time,
) (harnessprotocol.TurnResult, error) {
	completedAt := float64(time.Now().Unix())
	durationMS := max(0, int(time.Since(startedMonotonic).Milliseconds()))
	code := fmt.Sprintf("%T", exc) // Python: type(exc).__name__ 等价（Go 用类型字符串）
	turnErr, err := harnessprotocol.NewTurnError(
		"DeepSeek Harness SDK turn failed",
		&code, strPtr("provider_sdk"), nil, harnessprotocol.JsonObject{},
	)
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	frozen, err := harnessprotocol.FreezeJsonObject(
		map[string]any{"exception_type": fmt.Sprintf("%T", exc)})
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusFailed, harnessprotocol.ResultOptions{
		Messages:     a.messages,
		FinalOutput:  a.lastTextOutput,
		Error:        &turnErr,
		Usage:        a.totalUsage(),
		StartedAt:    &startedAt,
		CompletedAt:  &completedAt,
		DurationMS:   &durationMS,
		ProviderData: frozen,
	})
	return result, err
}

// BuildStoppedResult 构建周期关闭打断活动时的中断结果。
// Python: mapping.py:215-236 build_stopped_result
func (a *DshTurnAccumulator) BuildStoppedResult(
	startedAt float64,
	startedMonotonic time.Time,
) (harnessprotocol.TurnResult, error) {
	completedAt := float64(time.Now().Unix())
	durationMS := max(0, int(time.Since(startedMonotonic).Milliseconds()))
	message := "DeepSeek Harness stopped before the turn completed"
	termination, err := harnessprotocol.NewTurnTermination(
		harnessprotocol.TurnTerminationKindHarnessStop, &message, nil, harnessprotocol.JsonObject{})
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, harnessprotocol.ResultOptions{
		Messages:    a.messages,
		FinalOutput: a.lastTextOutput,
		Termination: &termination,
		Usage:       a.totalUsage(),
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
		DurationMS:  &durationMS,
	})
	return result, err
}

// BuildQueuedStopResult 已接受但未执行即被停止的轮次的终态结果。
// Python: mapping.py:520-533 build_queued_stop_result
func BuildQueuedStopResult() (harnessprotocol.TurnResult, error) {
	now := float64(time.Now().Unix())
	zero := 0
	message := "DeepSeek Harness stopped before the queued turn started"
	termination, err := harnessprotocol.NewTurnTermination(
		harnessprotocol.TurnTerminationKindHarnessStop, &message, nil, harnessprotocol.JsonObject{})
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	return harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, harnessprotocol.ResultOptions{
		Termination: &termination,
		StartedAt:   &now,
		CompletedAt: &now,
		DurationMS:  &zero,
	})
}

// TotalUsage 跨 DSH 模型调用步的累计用量。
// Python: mapping.py:238-262 total_usage（property）
func (a *DshTurnAccumulator) TotalUsage() *harnessprotocol.TurnUsage { return a.totalUsage() }

// Messages 已归一化的消息序列（BuildTerminalResult 之外的对账入口）。
func (a *DshTurnAccumulator) Messages() []harnessprotocol.TurnMessage { return a.messages }

// LastTextOutput 最终答复文本投影。
func (a *DshTurnAccumulator) LastTextOutput() string { return a.lastTextOutput }

// ──────────────────────────── 非导出函数 ────────────────────────────

// totalUsage 累计用量内部实现（Python: total_usage 属性体）。
func (a *DshTurnAccumulator) totalUsage() *harnessprotocol.TurnUsage {
	if len(a.usageByStep) == 0 {
		return nil
	}
	inputTokens, outputTokens := 0, 0
	cachedInputTokens, reasoningOutputTokens := 0, 0
	cacheWriteTokens := 0
	for _, usage := range a.usageByStep {
		inputTokens += derefInt(usage.InputTokens)
		outputTokens += derefInt(usage.OutputTokens)
		cachedInputTokens += derefInt(usage.CachedInputTokens)
		reasoningOutputTokens += derefInt(usage.ReasoningOutputTokens)
		cacheWriteTokens += asInt(asMap(usage.ProviderData)["cache_write_tokens"])
	}
	usage, err := harnessprotocol.NewTurnUsage(
		&inputTokens, &outputTokens, &cachedInputTokens, &reasoningOutputTokens, nil,
		harnessprotocol.JsonObject{"cache_write_tokens": cacheWriteTokens},
	)
	if err != nil {
		return nil
	}
	total := inputTokens + outputTokens + cachedInputTokens + cacheWriteTokens
	usage.TotalTokens = &total
	return &usage
}

// mapRootSessionEvent 根会话事件分流（Python: mapping.py:264-282）。
func (a *DshTurnAccumulator) mapRootSessionEvent(event map[string]any) ([]*MappedDshEvent, error) {
	eventType := asString(event["type"])
	if eventType == "" {
		eventType = "unknown"
	}
	data := asMap(event["data"])
	switch eventType {
	case "assistant/chunk":
		return a.mapAssistantChunk(data, event)
	case "assistant/message":
		return a.mapAssistantMessage(data, event)
	case "tool/call":
		mapped, err := a.mapToolCall(data)
		if err != nil {
			return nil, err
		}
		return []*MappedDshEvent{mapped}, nil
	case "tool/result":
		mapped, err := a.mapToolResult(data)
		if err != nil {
			return nil, err
		}
		return []*MappedDshEvent{mapped}, nil
	case "step/start", "step/end":
		mapped, err := mapStep(eventType, data)
		if err != nil {
			return nil, err
		}
		return []*MappedDshEvent{mapped}, nil
	case "turn/end":
		a.lastTurnReason = asMap(data["reason"])
		a.nativeTurnEndCount++
		return a.providerEvents(eventType,
			map[string]any{"event": mapAnyToAny(redactTurnEndEvent(event))}, "")
	default:
		return a.providerEvents(eventType, map[string]any{"event": mapAnyToAny(event)}, "")
	}
}

// mapAssistantChunk 助手增量块映射（Python: mapping.py:284-339）。
func (a *DshTurnAccumulator) mapAssistantChunk(
	data map[string]any, rawEvent map[string]any,
) ([]*MappedDshEvent, error) {
	chunk := asMap(data["chunk"])
	chunkType := asString(chunk["type"])
	if chunkType == "" {
		chunkType = "unknown"
	}
	nativeTurn := orZero(asInt(data["turn"]))
	nativeStep := orZero(asInt(data["step"]))
	contentIndex := orZero(asInt(chunk["index"]))

	if chunkType == "text-delta" || chunkType == "reasoning-delta" {
		text := asString(chunk["text"])
		if text == "" {
			// Python: text is None → []（mapping.py:297-299）；空串语义一致
			if _, ok := chunk["text"]; !ok {
				return []*MappedDshEvent{}, nil
			}
		}
		channel := harnessprotocol.OutputChannelAnswer
		if chunkType == "reasoning-delta" {
			channel = harnessprotocol.OutputChannelReasoning
		}
		outputID := outputID(nativeTurn, nativeStep, contentIndex, channel)
		payload, err := harnessprotocol.NewOutputEvent(
			outputID, harnessprotocol.OutputKindText, text,
			harnessprotocol.OutputOperationDelta, channel, contentIndex, "", harnessprotocol.JsonObject{},
		)
		if err != nil {
			return nil, err
		}
		return []*MappedDshEvent{{Payload: payload}}, nil
	}

	if chunkType == "block-end" {
		block := asMap(chunk["block"])
		blockType := asString(block["type"])
		if blockType == "text" || blockType == "reasoning" {
			channel := harnessprotocol.OutputChannelAnswer
			if blockType == "reasoning" {
				channel = harnessprotocol.OutputChannelReasoning
			}
			outputID := outputID(nativeTurn, nativeStep, contentIndex, channel)
			a.finalizedOutputIds[outputID] = true
			text := asString(block["text"])
			if channel == harnessprotocol.OutputChannelAnswer {
				a.lastTextOutput = text
			}
			payload, err := harnessprotocol.NewOutputEvent(
				outputID, harnessprotocol.OutputKindText, text,
				harnessprotocol.OutputOperationFinal, channel, contentIndex, "", harnessprotocol.JsonObject{},
			)
			if err != nil {
				return nil, err
			}
			return []*MappedDshEvent{{Payload: payload}}, nil
		}
	}

	if chunkType == "usage" {
		return a.updateUsage(nativeTurn, nativeStep, asMap(chunk["usage"]))
	}
	return a.providerEvents("assistant/chunk", map[string]any{"event": mapAnyToAny(rawEvent)}, "")
}

// mapAssistantMessage 助手整条消息映射（Python: mapping.py:341-404）。
func (a *DshTurnAccumulator) mapAssistantMessage(
	data map[string]any, rawEvent map[string]any,
) ([]*MappedDshEvent, error) {
	nativeTurn := orZero(asInt(data["turn"]))
	nativeStep := orZero(asInt(data["step"]))
	message := asMap(data["message"])
	if len(message) == 0 {
		message = data
	}
	rawContent, _ := message["content"].([]any)
	messageID := asString(message["id"])
	if messageID == "" {
		messageID = fmt.Sprintf("dsh-message:%d:%d:%v", nativeTurn, nativeStep, rawEvent["seq"])
	}
	var blocks []harnessprotocol.ContentBlock
	var mapped []*MappedDshEvent
	var answerParts []string

	for index, rawBlock := range rawContent {
		block := asMap(rawBlock)
		blockType := asString(block["type"])
		if blockType == "" {
			blockType = "unknown"
		}
		blockID := asString(block["id"])
		if blockID == "" {
			blockID = fmt.Sprintf("%s:block:%d", messageID, index)
		}
		contentBlock, err := contentBlock(blockID, blockType, block)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, contentBlock)
		if blockType != "text" && blockType != "reasoning" {
			continue
		}
		channel := harnessprotocol.OutputChannelAnswer
		if blockType == "reasoning" {
			channel = harnessprotocol.OutputChannelReasoning
		}
		outputID := outputID(nativeTurn, nativeStep, index, channel)
		text := asString(block["text"])
		if channel == harnessprotocol.OutputChannelAnswer {
			answerParts = append(answerParts, text)
		}
		if !a.finalizedOutputIds[outputID] {
			a.finalizedOutputIds[outputID] = true
			payload, err := harnessprotocol.NewOutputEvent(
				outputID, harnessprotocol.OutputKindText, text,
				harnessprotocol.OutputOperationFinal, channel, index, "", harnessprotocol.JsonObject{},
			)
			if err != nil {
				return nil, err
			}
			mapped = append(mapped, &MappedDshEvent{Payload: payload})
		}
	}

	// 与 SDK 的 final_response 投影一致：最新 assistant message 拥有值，
	// 其全部 text 块拼接（mapping.py:383-385）
	a.lastTextOutput = strings.Join(answerParts, "")

	if !a.messageIds[messageID] {
		a.messageIds[messageID] = true
		source := asMap(message["source"])
		messageData := map[string]any{
			"native_turn": nativeTurn,
			"native_step": nativeStep,
		}
		if len(source) > 0 {
			messageData["source"] = freezeObject(source)
		}
		turnMessage, err := harnessprotocol.NewTurnMessage(
			messageID, harnessprotocol.MessageRoleAssistant, blocks, messageData)
		if err != nil {
			return nil, err
		}
		a.messages = append(a.messages, turnMessage)
	}
	if usage := asMap(data["usage"]); len(usage) > 0 {
		extra, err := a.updateUsage(nativeTurn, nativeStep, usage)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, extra...)
	}
	return mapped, nil
}

// mapToolCall 工具调用映射（Python: mapping.py:406-424 _map_tool_call）。
func (a *DshTurnAccumulator) mapToolCall(data map[string]any) (*MappedDshEvent, error) {
	callID := asString(data["callId"])
	if callID == "" {
		callID = "unknown-tool-call"
	}
	toolName := asString(data["name"])
	if toolName == "" {
		toolName = "unknown"
	}
	a.toolNames[callID] = toolName
	frozen, err := harnessprotocol.FreezeJsonObject(map[string]any{
		"name":          toolName,
		"arguments":     freezeValue(data["arguments"]),
		"provider_turn": nullableInt(asInt(data["turn"])),
		"provider_step": nullableInt(asInt(data["step"])),
	})
	if err != nil {
		return nil, err
	}
	payload, err := harnessprotocol.NewItemLifecycleEvent(
		harnessprotocol.ItemEventKindStarted, "tool", frozen)
	if err != nil {
		return nil, err
	}
	return &MappedDshEvent{Payload: payload, ItemID: callID}, nil
}

// mapToolResult 工具结果映射（Python: mapping.py:426-448 _map_tool_result）。
func (a *DshTurnAccumulator) mapToolResult(data map[string]any) (*MappedDshEvent, error) {
	message := asMap(data["message"])
	source := asMap(message["source"])
	callID := asString(source["callId"])
	if callID == "" {
		callID = asString(data["callId"])
	}
	if callID == "" {
		callID = "unknown-tool-call"
	}
	var content any
	if len(message) > 0 {
		content = message["content"]
	} else {
		content = data["content"]
	}
	itemData := map[string]any{
		"tool_name":     a.toolNames[callID],
		"result":        freezeValue(content),
		"provider_turn": nullableInt(asInt(data["turn"])),
		"provider_step": nullableInt(asInt(data["step"])),
	}
	if data["error"] != nil {
		itemData["error"] = freezeValue(data["error"])
	}
	if data["meta"] != nil {
		itemData["meta"] = freezeValue(data["meta"])
	}
	frozen, err := harnessprotocol.FreezeJsonObject(itemData)
	if err != nil {
		return nil, err
	}
	payload, err := harnessprotocol.NewItemLifecycleEvent(
		harnessprotocol.ItemEventKindCompleted, "tool", frozen)
	if err != nil {
		return nil, err
	}
	return &MappedDshEvent{Payload: payload, ItemID: callID}, nil
}

// mapStep 步骤映射（Python: mapping.py:450-462 _map_step）。
func mapStep(eventType string, data map[string]any) (*MappedDshEvent, error) {
	nativeTurn := orZero(asInt(data["turn"]))
	nativeStep := orZero(asInt(data["step"]))
	kind := harnessprotocol.ItemEventKindCompleted
	if eventType == "step/start" {
		kind = harnessprotocol.ItemEventKindStarted
	}
	payload, err := harnessprotocol.NewItemLifecycleEvent(kind, "step",
		harnessprotocol.JsonObject{
			"provider_turn": nativeTurn,
			"provider_step": nativeStep,
		})
	if err != nil {
		return nil, err
	}
	return &MappedDshEvent{
		Payload: payload,
		ItemID:  fmt.Sprintf("dsh-step:%d:%d", nativeTurn, nativeStep),
	}, nil
}

// mapSubagent 子代理映射（Python: mapping.py:464-472 _map_subagent）。
func (a *DshTurnAccumulator) mapSubagent(method string, payload map[string]any) (*MappedDshEvent, error) {
	childID := asString(payload["childSessionId"])
	if childID == "" {
		childID = asString(payload["sessionId"])
	}
	if childID == "" {
		childID = "unknown-subagent"
	}
	kind := harnessprotocol.ItemEventKindCompleted
	if method == "subagent.started" {
		kind = harnessprotocol.ItemEventKindStarted
	}
	frozen, err := harnessprotocol.FreezeJsonObject(mapAnyToAny(payload))
	if err != nil {
		return nil, err
	}
	payloadEvent, err := harnessprotocol.NewItemLifecycleEvent(kind, "subagent", frozen)
	if err != nil {
		return nil, err
	}
	return &MappedDshEvent{
		Payload:           payloadEvent,
		ItemID:            childID,
		ProviderSessionID: childID,
	}, nil
}

// updateUsage 用量更新（Python: mapping.py:474-486 _update_usage）。
func (a *DshTurnAccumulator) updateUsage(
	nativeTurn, nativeStep int, rawUsage map[string]any,
) ([]*MappedDshEvent, error) {
	usage, err := turnUsage(rawUsage)
	if err != nil {
		return nil, err
	}
	key := [2]int{nativeTurn, nativeStep}
	if existing, ok := a.usageByStep[key]; ok && sameUsage(&existing, usage) {
		return []*MappedDshEvent{}, nil
	}
	a.usageByStep[key] = *usage
	total := a.totalUsage()
	if total == nil {
		return []*MappedDshEvent{}, nil
	}
	payload, err := harnessprotocol.NewUsageUpdatedEvent(
		*total, harnessprotocol.UsageUpdateModeCumulative)
	if err != nil {
		return nil, err
	}
	return []*MappedDshEvent{{Payload: payload}}, nil
}

// providerEvents ProviderEvent 组装（Python: mapping.py:488-503 _provider_event）。
func (a *DshTurnAccumulator) providerEvents(
	eventType string, payload map[string]any, providerSessionID string,
) ([]*MappedDshEvent, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	frozen, err := harnessprotocol.FreezeJsonObject(payload)
	if err != nil {
		return nil, err
	}
	payloadEvent, err := harnessprotocol.NewProviderEvent(
		providerName, eventType, schemaVersion, frozen)
	if err != nil {
		return nil, err
	}
	return []*MappedDshEvent{{
		Payload:           payloadEvent,
		ProviderSessionID: providerSessionID,
	}}, nil
}

// terminationKind 中断原因映射（Python: mapping.py:505-517 _termination_kind）。
func (a *DshTurnAccumulator) terminationKind(finishReason string) harnessprotocol.TurnTerminationKind {
	if finishReason == "interrupted" {
		return harnessprotocol.TurnTerminationKindProvider
	}
	reason := asMap(a.lastTurnReason)
	cause := asMap(reason["reason"])
	causeKind := asString(cause["kind"])
	switch causeKind {
	case "user":
		return harnessprotocol.TurnTerminationKindUserAbort
	case "disposed":
		return harnessprotocol.TurnTerminationKindHarnessStop
	case "hook":
		return harnessprotocol.TurnTerminationKindPolicy
	default:
		return harnessprotocol.TurnTerminationKindProvider
	}
}

// ──────────────────────────── 包级助手 ────────────────────────────

// safeReasonData 保留有用的非消息失败字段，不复制 provider 诊断文本。
// Python: mapping.py:552-569 _safe_reason_data
func safeReasonData(reason map[string]any) harnessprotocol.JsonObject {
	if len(reason) == 0 {
		return harnessprotocol.JsonObject{}
	}
	result := harnessprotocol.JsonObject{}
	if kind := asString(reason["kind"]); kind != "" {
		result["kind"] = kind
	}
	if cause := asMap(reason["reason"]); len(cause) > 0 {
		filtered := map[string]any{}
		for key, value := range cause {
			if key != "message" {
				filtered[key] = value
			}
		}
		result["cause"] = freezeObject(filtered)
	}
	if errorMap := asMap(reason["error"]); len(errorMap) > 0 {
		filtered := map[string]any{}
		for _, key := range []string{"code", "status", "requestId", "retryable"} {
			if value, ok := errorMap[key]; ok {
				filtered[key] = value
			}
		}
		result["error"] = freezeObject(filtered)
	}
	return result
}

// redactTurnEndEvent 保留 DSH 事件形状而不暴露 provider 错误文本。
// Python: mapping.py:572-592 _redact_turn_end_event
func redactTurnEndEvent(event map[string]any) map[string]any {
	safeEvent := map[string]any{}
	for key, value := range event {
		safeEvent[key] = value
	}
	data := map[string]any{}
	for key, value := range asMap(event["data"]) {
		data[key] = value
	}
	reason := asMap(data["reason"])
	safeReason := map[string]any{}
	if kind := asString(reason["kind"]); kind != "" {
		safeReason["kind"] = kind
	}
	if cause := asMap(reason["reason"]); len(cause) > 0 {
		filtered := map[string]any{}
		for key, value := range cause {
			if key != "message" {
				filtered[key] = value
			}
		}
		safeReason["reason"] = filtered
	}
	if errorMap := asMap(reason["error"]); len(errorMap) > 0 {
		filtered := map[string]any{}
		for _, key := range []string{"code", "status", "requestId", "retryable"} {
			if value, ok := errorMap[key]; ok {
				filtered[key] = value
			}
		}
		safeReason["error"] = filtered
	}
	data["reason"] = safeReason
	safeEvent["data"] = data
	return safeEvent
}

// reasonErrorCode 从 turn/end reason 提取 provider 错误码。
// Python: mapping.py:595-598 _reason_error_code
func reasonErrorCode(reason map[string]any) *string {
	errorMap := asMap(reason["error"])
	code := errorMap["code"]
	switch typed := code.(type) {
	case string:
		return &typed
	case float64:
		text := fmt.Sprintf("%v", int64(typed))
		return &text
	default:
		return nil
	}
}

// turnUsage 用量解析（Python: mapping.py:601-614 _turn_usage）。
func turnUsage(raw map[string]any) (*harnessprotocol.TurnUsage, error) {
	inputTokens := nonNegativeInt(raw["inputTokens"])
	outputTokens := nonNegativeInt(raw["outputTokens"])
	cacheReadTokens := nonNegativeInt(raw["cacheReadTokens"])
	cacheWriteTokens := nonNegativeInt(raw["cacheWriteTokens"])
	reasoningTokens := nonNegativeInt(raw["reasoningTokens"])
	usage, err := harnessprotocol.NewTurnUsage(
		inputTokens, outputTokens, cacheReadTokens, reasoningTokens, nil,
		harnessprotocol.JsonObject{"cache_write_tokens": derefInt(cacheWriteTokens)},
	)
	if err != nil {
		return nil, err
	}
	total := derefInt(inputTokens) + derefInt(outputTokens) +
		derefInt(cacheReadTokens) + derefInt(cacheWriteTokens)
	usage.TotalTokens = &total
	return &usage, nil
}

// contentBlock 内容块组装（Python: mapping.py:617-630 _content_block）。
func contentBlock(blockID, blockType string, block map[string]any) (harnessprotocol.ContentBlock, error) {
	if blockType == "text" || blockType == "reasoning" {
		return harnessprotocol.NewContentBlock(
			blockID, blockType, asString(block["text"]), harnessprotocol.JsonObject{})
	}
	if blockType == "tool-call" {
		name := asString(block["name"])
		if name == "" {
			name = "unknown"
		}
		data := harnessprotocol.JsonObject{
			"call_id": asString(block["id"]),
		}
		if data["call_id"] == "" {
			data["call_id"] = blockID
		}
		return harnessprotocol.NewContentBlock(blockID, "tool_call",
			harnessprotocol.JsonObject{
				"name":      name,
				"arguments": freezeValue(block["arguments"]),
			}, data)
	}
	return harnessprotocol.NewContentBlock(blockID, blockType,
		freezeObject(block), harnessprotocol.JsonObject{})
}

// outputID 输出块标识（Python: mapping.py:633-634 _output_id）。
func outputID(nativeTurn, nativeStep, contentIndex int, channel harnessprotocol.OutputChannel) string {
	return fmt.Sprintf("dsh-output:%d:%d:%d:%s", nativeTurn, nativeStep, contentIndex, string(channel))
}

// asMap 宽松对象转换（Python: mapping.py:637-638 _mapping）。
func asMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	if typed, ok := value.(harnessprotocol.JsonObject); ok {
		return typed
	}
	if typed, ok := value.(deepseekharness.JSONObject); ok {
		return typed
	}
	return nil
}

// mapAnyToAny 通用 map 归一（供 freeze 前的类型统一）。
func mapAnyToAny(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

// asString 宽松字符串转换（Python: mapping.py:641-642 _string）。
func asString(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

// asInt 宽松整数转换（Python: mapping.py:645-646 _int；JSON 数字统一
// float64，排除 bool）。
func asInt(value any) int {
	switch typed := value.(type) {
	case float64:
		if typed == float64(int64(typed)) {
			return int(typed)
		}
		return 0
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

// nonNegativeInt 非负整数转换（Python: mapping.py:649-651 _non_negative_int）。
func nonNegativeInt(value any) *int {
	if typed, ok := value.(float64); ok && typed >= 0 && typed == float64(int64(typed)) {
		number := int(typed)
		return &number
	}
	if typed, ok := value.(int); ok && typed >= 0 {
		return &typed
	}
	return nil
}

// nullableInt 0 → nil（Python: _int(value) 返回 None 的 JSON 等价）。
func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

// orZero nil 兜底为 0（Python: _int(...) or 0）。
func orZero(value int) int { return value }

// derefInt 指针整数解引用。
func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// strPtr 字符串指针。
func strPtr(value string) *string { return &value }

// freezeValue 冻结 JSON 值（Python freeze_json_value 对 JSON 解码产物不抛错；
// 本包输入全部来自 SDK 通知的 JSON 容器，冻结失败不可能发生，失败时置 nil）。
func freezeValue(value any) harnessprotocol.JsonValue {
	frozen, err := harnessprotocol.FreezeJsonValue(value)
	if err != nil {
		return nil
	}
	return frozen
}

// freezeObject 冻结 JSON 对象（语义同 freezeValue）。
func freezeObject(value map[string]any) harnessprotocol.JsonObject {
	frozen, err := harnessprotocol.FreezeJsonObject(value)
	if err != nil {
		return harnessprotocol.JsonObject{}
	}
	return frozen
}

// sameUsage 用量相等比较（Python: == 值比较）。
func sameUsage(left, right *harnessprotocol.TurnUsage) bool {
	if left == nil || right == nil {
		return left == right
	}
	return derefInt(left.InputTokens) == derefInt(right.InputTokens) &&
		derefInt(left.OutputTokens) == derefInt(right.OutputTokens) &&
		derefInt(left.CachedInputTokens) == derefInt(right.CachedInputTokens) &&
		derefInt(left.ReasoningOutputTokens) == derefInt(right.ReasoningOutputTokens) &&
		derefInt(left.TotalTokens) == derefInt(right.TotalTokens)
}

// withTermination 给公共字段追加 termination。
func withTermination(common harnessprotocol.ResultOptions, termination *harnessprotocol.TurnTermination) harnessprotocol.ResultOptions {
	common.Termination = termination
	return common
}

// withError 给公共字段追加 error。
func withError(common harnessprotocol.ResultOptions, turnErr *harnessprotocol.TurnError) harnessprotocol.ResultOptions {
	common.Error = turnErr
	return common
}
