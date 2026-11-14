package native

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	kvcache "github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sessionstream "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// captureSession 记录 WriteStream 帧的假会话（SessionFacade 全方法实现，
// 模式对齐 cli/rails/tracker_test.go recordingSession）。
type captureSession struct {
	frames []any
}

func (s *captureSession) GetSessionID() string                             { return "test-session" }
func (s *captureSession) UpdateState(_ map[string]any)                     {}
func (s *captureSession) GetState(_ state.StateKey) (any, error)           { return nil, nil }
func (s *captureSession) DumpState() map[string]any                        { return nil }
func (s *captureSession) WriteStream(_ context.Context, data any) error {
	s.frames = append(s.frames, data)
	return nil
}
func (s *captureSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (s *captureSession) GetEnv(_ string, _ ...any) any                    { return nil }
func (s *captureSession) Interact(_ context.Context, _ any) error          { return nil }
func (s *captureSession) ClearSession(_ context.Context)                   {}
func (s *captureSession) GetCacheIdentity() kvcache.KVCacheIdentity {
	return kvcache.KVCacheIdentity{}
}
func (s *captureSession) GetParentSessionID() string { return "" }

// mustBlock 构造内容块的辅助。
func mustBlock(t *testing.T, id, kind string, content any) hp.ContentBlock {
	t.Helper()
	block, err := hp.NewContentBlock(id, kind, content, hp.JsonObject{})
	require.NoError(t, err)
	return block
}

// toolCallContext 构造带工具输入与会话的回调上下文。
func toolCallContext(sess sessioninterfaces.SessionFacade, inputs agentinterfaces.EventInputs) *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
}

// ──────────────────────────── observationRail ────────────────────────────

// TestObservationRail_beforeToolCall 镜像 tool_call 帧
// （Python: harness.py:101-116）。
func TestObservationRail_beforeToolCall(t *testing.T) {
	rail := &observationRail{}
	session := &captureSession{}
	inputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "call-1"},
		ToolName: "read_file",
		ToolArgs: map[string]any{"path": "/tmp/a"},
	}
	require.NoError(t, rail.BeforeToolCall(context.Background(), toolCallContext(session, inputs)))
	require.Len(t, session.frames, 1)
	frame := session.frames[0].(sessionstream.OutputSchema)
	assert.Equal(t, toolCallChunk, frame.Type)
	payload := frame.Payload.(map[string]any)
	assert.Equal(t, "call-1", payload["tool_call_id"])
	assert.Equal(t, "read_file", payload["tool_name"])
	assert.Equal(t, map[string]any{"path": "/tmp/a"}, payload["tool_args"])
}

// TestObservationRail_afterToolCall 镜像 tool_result 帧（含 rendered_result）
// （Python: harness.py:118-139）。
func TestObservationRail_afterToolCall(t *testing.T) {
	rail := &observationRail{}
	session := &captureSession{}
	inputs := &agentinterfaces.ToolCallInputs{
		ToolCall:   &llmschema.ToolCall{ID: "call-1"},
		ToolName:   "read_file",
		ToolResult: map[string]any{"content": "data", "rendered_result": "模型可见"},
	}
	require.NoError(t, rail.AfterToolCall(context.Background(), toolCallContext(session, inputs)))
	require.Len(t, session.frames, 1)
	frame := session.frames[0].(sessionstream.OutputSchema)
	assert.Equal(t, toolResultChunk, frame.Type)
	payload := frame.Payload.(map[string]any)
	assert.Equal(t, "call-1", payload["tool_call_id"])
	assert.Equal(t, map[string]any{"content": "data", "rendered_result": "模型可见"}, payload["tool_result"])
	assert.Equal(t, "模型可见", payload["rendered_result"])
}

// TestObservationRail_非工具输入跳过 session 为 nil 或 inputs 非工具
// 时直接返回（Python: harness.py:104-105）。
func TestObservationRail_非工具输入跳过(t *testing.T) {
	rail := &observationRail{}
	require.NoError(t, rail.BeforeToolCall(context.Background(),
		agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)))
	require.NoError(t, rail.AfterToolCall(context.Background(),
		agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)))
}

// TestObservationRail_优先级与回调面（Python: harness.py:99 priority=5）。
func TestObservationRail_优先级与回调面(t *testing.T) {
	rail := &observationRail{}
	assert.Equal(t, observationRailPriority, rail.Priority())
	callbacks := rail.GetCallbacks()
	assert.Contains(t, callbacks, agentinterfaces.CallbackBeforeToolCall)
	assert.Contains(t, callbacks, agentinterfaces.CallbackAfterToolCall)
}

// ──────────────────────────── 助手函数 ────────────────────────────

// TestToolCallID_缺省回退 tool_call_id 缺省回退
// "{tool_name}:{index}:{hex8}"（Python: harness.py:686-691）。
func TestToolCallID_缺省回退(t *testing.T) {
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	got := toolCallID(inputs)
	assert.Contains(t, got, "bash:0:")
	assert.Len(t, got[len(got)-8:], 8)
	inputs.ToolCall = &llmschema.ToolCall{ID: "real-id"}
	assert.Equal(t, "real-id", toolCallID(inputs))
}

// TestParseArgs_字符串反序列化（Python: harness.py:694-700）。
func TestParseArgs_字符串反序列化(t *testing.T) {
	assert.Equal(t, map[string]any{"a": float64(1)}, parseArgs(`{"a": 1}`))
	assert.Equal(t, "not-json", parseArgs("not-json"))
	assert.Equal(t, 42, parseArgs(42))
}

// TestPayloadText_形态（Python: harness.py:703-707）。
func TestPayloadText_形态(t *testing.T) {
	assert.Equal(t, "hi", payloadText(map[string]any{"content": "hi"}))
	assert.Equal(t, "", payloadText(map[string]any{"other": 1}))
	assert.Equal(t, "raw", payloadText("raw"))
	assert.Equal(t, "", payloadText(42))
}

// TestInteractionPayload_三形态（Python: harness.py:720-728）。
func TestInteractionPayload_三形态(t *testing.T) {
	id, value := interactionPayload([2]any{"intr-1", "payload"})
	assert.Equal(t, "intr-1", id)
	assert.Equal(t, "payload", value)
	id, value = interactionPayload(map[string]any{"id": "intr-2", "value": 7})
	assert.Equal(t, "intr-2", id)
	assert.Equal(t, 7, value)
	id, value = interactionPayload("plain")
	assert.Equal(t, "", id)
	assert.Equal(t, "plain", value)
}

// TestInterruptIdentity_工作流交互空串（Python: harness.py:731-739）。
func TestInterruptIdentity_工作流交互空串(t *testing.T) {
	interruptID, kind := interruptIdentity("workflow-value")
	assert.Equal(t, "", interruptID)
	assert.Equal(t, "", kind)
}

// TestInterruptProviderData_字段（Python: harness.py:742-743）。
func TestInterruptProviderData_字段(t *testing.T) {
	data := interruptProviderData("call-9", "value")
	assert.Equal(t, "call-9", data["tool_call_id"])
	assert.Equal(t, "", data["interrupt_id"])
	assert.Equal(t, "", data["kind"])
	assert.NotNil(t, data["value"])
}

// TestResultText_与ToolArguments（Python: harness.py:746-758）。
func TestResultText_与ToolArguments(t *testing.T) {
	assert.Equal(t, "text", resultText("text"))
	assert.Equal(t, `{"a":1}`, resultText(map[string]any{"a": 1}))
	assert.Equal(t, "", resultText(nil))
	assert.Equal(t, map[string]any{"a": float64(1)}, toolArguments(`{"a": 1}`))
	assert.Equal(t, map[string]any{"arguments": "raw{"}, toolArguments("raw{"))
	assert.Equal(t, map[string]any{"x": 1}, toolArguments(map[string]any{"x": 1}))
	assert.Equal(t, map[string]any{}, toolArguments(42))
}

// TestInterruptPrompt_渲染（Python: harness.py:761-775）。
func TestInterruptPrompt_渲染(t *testing.T) {
	questions := interruptPrompt(map[string]any{
		"questions": []any{
			map[string]any{"question": "Q1"},
			map[string]any{"question": "Q2"},
			"junk",
		},
	})
	assert.Equal(t, "Q1\nQ2", questions)
	assert.Equal(t, "plain msg", interruptPrompt(map[string]any{"message": "plain msg"}))
	assert.Contains(t, interruptPrompt(map[string]any{"other": 1}), "{")
	assert.Equal(t, "The agent is waiting for your input.", interruptPrompt(nil))
}

// TestTurnQuery_元数据分派（Python: harness.py:778-788）。
func TestTurnQuery_元数据分派(t *testing.T) {
	input := hp.HarnessInput{
		Content:  map[string]any{"user_inputs": map[string]any{"node-1": "answer"}},
		Metadata: map[string]any{"kind": interactiveInputMetadataKind},
	}
	query, ok := turnQuery(input).(*sessioninteraction.InteractiveInput)
	require.True(t, ok)
	assert.Equal(t, "answer", query.UserInputs["node-1"])
	assert.Equal(t, "plain query", turnQuery(hp.HarnessInput{Content: "plain query"}))
}

// TestInteractiveInputFromBuiltin_键映射 序列化字典重建。
func TestInteractiveInputFromBuiltin_键映射(t *testing.T) {
	input, err := interactiveInputFromBuiltin(map[string]any{
		"user_inputs":       map[string]any{"n": 1},
		"interrupt_answers": map[string]any{"i": "a"},
		"raw_inputs":        "raw",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, input.UserInputs["n"])
	assert.Equal(t, "a", input.InterruptAnswers["i"])
	assert.Equal(t, "raw", input.RawInputs)
	scalar, err := interactiveInputFromBuiltin("scalar")
	require.NoError(t, err)
	assert.Equal(t, "scalar", scalar.RawInputs)
}

// ──────────────────────────── turnState ────────────────────────────

// TestTurnState_Messages 累积投影：reasoning/text/tool 块 + 工具消息
// （Python: harness.py:157-181）。
func TestTurnState_Messages(t *testing.T) {
	state := newTurnState("turn-1")
	state.reasoningParts = []string{"think"}
	state.answerParts = []string{"hello", " world"}
	toolBlock, err := hp.NewContentBlock("turn-1:tool:c1", "tool_call",
		map[string]any{"name": "bash", "arguments": map[string]any{}},
		hp.JsonObject{"call_id": "c1"})
	require.NoError(t, err)
	state.toolBlocks = append(state.toolBlocks, toolBlock)
	msg, err := hp.NewTurnMessage("turn-1:tool-result:c1", hp.MessageRoleTool,
		[]hp.ContentBlock{mustBlock(t, "turn-1:tool-result:c1", "tool_result", "ok")}, nil)
	require.NoError(t, err)
	state.toolMessages = append(state.toolMessages, msg)

	messages, err := state.Messages()
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assistant := messages[0]
	require.Len(t, assistant.Content, 3)
	assert.Equal(t, "reasoning", assistant.Content[0].Kind)
	assert.Equal(t, "text", assistant.Content[1].Kind)
	assert.Equal(t, "tool_call", assistant.Content[2].Kind)
	assert.Equal(t, hp.MessageRoleAssistant, assistant.Role)
	assert.Equal(t, hp.MessageRoleTool, messages[1].Role)
}

// TestTurnState_Messages_空累积 无内容时仅 assistant 空块消息。
func TestTurnState_Messages_空累积(t *testing.T) {
	messages, err := newTurnState("turn-2").Messages()
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Empty(t, messages[0].Content)
}

// ──────────────────────────── card ────────────────────────────

// TestAgentFactoryCard_能力面 card 能力照 Python 类属性
// （Python: harness.py:218-229）。
func TestAgentFactoryCard_能力面(t *testing.T) {
	card := AgentFactoryCard()
	assert.Equal(t, ProviderName, card.Name)
	assert.Equal(t, AdapterVersion, card.ImplementationVersion)
	assert.True(t, card.Capabilities[hp.HarnessCapabilitySteer])
	assert.True(t, card.Capabilities[hp.HarnessCapabilityForceAbort])
	assert.True(t, card.OptionalHostCapabilities[hp.HostCapabilityUserInput])
	assert.True(t, card.OptionalHostCapabilities[hp.HostCapabilityToolApproval])
	assert.True(t, card.OptionalHostCapabilities[hp.HostCapabilityDynamicToolCall])
}

// TestAppendContextPrompt_常量锚定 段落名与优先级。
func TestAppendContextPrompt_常量锚定(t *testing.T) {
	assert.Equal(t, "harness_context", contextPromptSection)
	assert.Equal(t, 10, contextPromptPriority)
	// agentTemplate 形状依赖（15.15 交付的 AgentTemplateSpec）。
	_ = hschema.AgentTemplateSpec{}
	_ = session.Session{}
}
