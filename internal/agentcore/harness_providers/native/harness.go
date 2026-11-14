package native

// HarnessProtocol 在 in-process DeepAgent 交互循环之上的实现。
// Python: openjiuwen/harness_providers/native/harness.py（一比一迁移）

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	interruptpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	harness "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	harness_providers "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sessionstream "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AdapterVersion 适配器版本。
	// Python: harness.py:82 ADAPTER_VERSION
	AdapterVersion = "0.1.0"
	// ProviderName provider 名（工厂注册键）。
	// Python: harness.py:83 PROVIDER_NAME
	ProviderName = "deepagent"
	// interactiveInputMetadataKind 标记携带序列化 InteractiveInput 的输入。
	// Python: harness.py:84 INTERACTIVE_INPUT_METADATA_KIND
	interactiveInputMetadataKind = "interactive_input"
	// contextPromptSection 上下文提示词段落名。
	// Python: harness.py:85 _CONTEXT_PROMPT_SECTION
	contextPromptSection = "harness_context"
	// toolCallChunk observationRail 写出的工具调用帧类型。
	// Python: harness.py:86 _TOOL_CALL_CHUNK
	toolCallChunk = "tool_call"
	// toolResultChunk observationRail 写出的工具结果帧类型。
	// Python: harness.py:87 _TOOL_RESULT_CHUNK
	toolResultChunk = "tool_result"
	// observationRailPriority rail 优先级。
	// Python: harness.py:99 _ObservationRail.priority = 5
	observationRailPriority = 5
	// contextPromptPriority 上下文提示词段落优先级。
	// Python: harness.py:680 PromptSection(priority=10)
	contextPromptPriority = 10
)

// ──────────────────────────── 结构体 ────────────────────────────

// observationRail 把工具执行镜像到会话流上（tool_call 块）。
// DeepAgent 流式输出模型文本但不外露工具活动；本 rail 补上工具生命周期，
// 使 harness 能为它发出 ItemLifecycleEvent。
// Python: harness.py:92-139 _ObservationRail
type observationRail struct {
	agentinterfaces.BaseRail
}

// 编译时验证满足 AgentRail 接口。
var _ agentinterfaces.AgentRail = (*observationRail)(nil)

// Priority rail 优先级。
// Python: harness.py:99 _ObservationRail.priority = 5
func (r *observationRail) Priority() int { return observationRailPriority }

// GetCallbacks 覆盖基类回调映射，增加 BeforeToolCall / AfterToolCall 回调
// （⚠️ 必须覆盖接口方法 GetCallbacks，注册链路只调用它——
// rail-callbacks-vs-getcallbacks-trap）。
func (r *observationRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.BaseRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// BeforeToolCall 工具执行前：写 tool_call 帧。
// Python: harness.py:101-116 _ObservationRail.before_tool_call
func (r *observationRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	session := cbc.Session()
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if session == nil || !ok {
		return nil
	}
	return session.WriteStream(ctx, sessionstream.OutputSchema{
		Type:  toolCallChunk,
		Index: 0,
		Payload: map[string]any{
			"tool_call_id": toolCallID(inputs),
			"tool_name":    inputs.ToolName,
			"tool_args":    parseArgs(inputs.ToolArgs),
		},
	})
}

// AfterToolCall 工具执行后：写 tool_result 帧。
// tool_result 保留结构化值；rendered_result 是单独的模型可见文本，
// 结构解析器不受影响（Python: harness.py:128-131 注释）。
// Python: harness.py:118-139 _ObservationRail.after_tool_call
func (r *observationRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	session := cbc.Session()
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if session == nil || !ok {
		return nil
	}
	payload := map[string]any{
		"tool_call_id": toolCallID(inputs),
		"tool_name":    inputs.ToolName,
		"tool_result":  harness_providers.ToJsonSafe(inputs.ToolResult),
	}
	// rendered_result：模型可见文本（Go 侧从 ToolResult 的 rendered_result
	// 字段或字符串投影取，与 cli/rails/tool_tracker.go 同款投影；
	// Python: ability_manager.py:125-146 resolve_tool_result_text）。
	if rendered, hasRendered := extractRenderedResult(inputs.ToolResult); hasRendered {
		payload["rendered_result"] = rendered
	}
	return session.WriteStream(ctx, sessionstream.OutputSchema{
		Type:    toolResultChunk,
		Index:   0,
		Payload: payload,
	})
}

// turnState 一次外部轮次的 chunk 累积。
// 所有字段只在 ExecuteTurn 的单一 goroutine 内读写（Python 单线程语义），
// 无需加锁。Python: harness.py:142-181 _TurnState
type turnState struct {
	// turnID 轮次标识
	turnID string
	// answerParts 答复文本片段
	answerParts []string
	// reasoningParts 思考文本片段
	reasoningParts []string
	// finalOutput 最终输出（answer 帧给出时；nil=未给出）
	finalOutput *string
	// resultType 结果类型（answer 帧给出时；nil=未给出）
	resultType *string
	// err 轮次错误（execution.error 帧或异常时设置）
	err *hp.TurnError
	// pendingInterrupts 待决中断：interrupt_id（或 call_id）→ 值
	pendingInterrupts map[string]any
	// toolBlocks 工具调用内容块
	toolBlocks []hp.ContentBlock
	// toolMessages 工具结果消息
	toolMessages []hp.TurnMessage
	// emittedOutput 是否已发出过输出（finalOutput 兜底判据）
	emittedOutput bool
}

// newTurnState 构造轮次状态。
// Python: harness.py:145-155 _TurnState.__init__
func newTurnState(turnID string) *turnState {
	return &turnState{
		turnID:            turnID,
		pendingInterrupts: map[string]any{},
	}
}

// pendingKeysSorted 待决中断 key 的升序快照。
func (s *turnState) pendingKeysSorted() []string {
	keys := make([]string, 0, len(s.pendingInterrupts))
	for key := range s.pendingInterrupts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Messages 累积出的 TurnMessage 序列：reasoning 块 + text 块 + tool 块
// 组成 assistant 消息，后接全部工具结果消息。
// Python: harness.py:157-181 _TurnState.messages
func (s *turnState) Messages() ([]hp.TurnMessage, error) {
	blocks := make([]hp.ContentBlock, 0, 2+len(s.toolBlocks))
	if len(s.reasoningParts) > 0 {
		block, err := hp.NewContentBlock(s.turnID+":reasoning", "reasoning",
			strings.Join(s.reasoningParts, ""), hp.JsonObject{})
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	if len(s.answerParts) > 0 {
		block, err := hp.NewContentBlock(s.turnID+":text", "text",
			strings.Join(s.answerParts, ""), hp.JsonObject{})
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	blocks = append(blocks, s.toolBlocks...)
	assistant, err := hp.NewTurnMessage(s.turnID+":assistant", hp.MessageRoleAssistant, blocks, nil)
	if err != nil {
		return nil, err
	}
	messages := make([]hp.TurnMessage, 0, 1+len(s.toolMessages))
	messages = append(messages, assistant)
	messages = append(messages, s.toolMessages...)
	return messages, nil
}

// AgentFactory 按上下文构建未启动 DeepAgent 的工厂。
// Python: harness.py:89 AgentFactory（可返回 Awaitable——Go 仅同步工厂，
// 见 spec 偏差 D1）
type AgentFactory func(ctx context.Context, harnessContext hp.HarnessContext) (*harness.DeepAgent, error)

// DeepAgentHarness 把一个 DeepAgent 的会话内交互循环适配到协议 v1。
// Python: harness.py:184-229 DeepAgentHarness（docstring 语义照搬）
//
// 一次外部 Turn 是一次 attach_output 流：输入经 send_input 派发，DeepAgent
// 结束流时轮次结束（它自己的任务循环延续保持在轮内）。中断保持轮次打开，
// 在下一轮恢复前经宿主交互解决（request_id = 中断 id）：
//
//   - approval 类（permission、confirm、team tool、skill evolution）在宿主
//     声明 TOOL_APPROVAL（且 card 列出）时变 ToolApprovalRequest，否则变
//     UserInputRequest（allow / allow_always / deny 三选）；审批时编辑参数
//     不支持（updated_arguments 拒绝该调用），ABORT 或 interrupt=true 停轮，
//     ALLOW_FOR_SESSION 且 provider_data["persist_allow"] 为 true 永久放行；
//   - client_tool 在宿主声明 DYNAMIC_TOOL_CALL 时变 DynamicToolCallRequest；
//     否则该调用以工具错误结束（宿主不能跑客户端工具）；
//   - 其余一切（ask_user、未声明类型、其他 rail 的类型）变 UserInputRequest
//     并以宿主内容原样回答；
//   - workflow 交互不是中断请求：变 UserInputRequest，request_id 用自身 id、
//     interrupt_id 与 kind 为空，并按该 id 回答。
//
// 请求在 provider_data 携带 kind、interrupt_id、tool_call_id。审批与提问
// 发给每个有交互处理器的宿主，无论其声明了什么能力；无处理器时轮次保持
// 暂停。客户端调用最后问，避免选择停轮的宿主丢掉客户端结果。
type DeepAgentHarness struct {
	// base 基座（构造期接线；Go embedding 无虚派发，基座经 hooks 回调本结构）
	base *harness_providers.SerializedTurnHarness
	// agentFactory 构建未启动 DeepAgent 的配方（agent 在 start 时构建）
	agentFactory AgentFactory
	// agentTemplate 首轮前热加载到 agent 上的 manifest 模板（prompt 段、
	// 工具、rails、技能、子代理）
	agentTemplate *hschema.AgentTemplateSpec
	// sessionIDOverride 显式 DeepAgent 会话 id；缺省为宿主会话 id 与
	// agent id 连接
	sessionIDOverride string
	// language 解析模板用的语言
	language string
	// agent 活跃周期的 DeepAgent（start 前为 nil）
	agent *harness.DeepAgent
	// agentSession 活跃周期的会话
	agentSession *session.Session
}

// AgentFactoryCard provider 卡片：能力面照 Python 类属性。
// Python: harness.py:218-229 DeepAgentHarness.card
func AgentFactoryCard() hp.HarnessCard {
	card, _ := hp.NewHarnessCardWithOptions(ProviderName, AdapterVersion, hp.CardOptions{
		Capabilities: map[hp.HarnessCapability]bool{
			hp.HarnessCapabilitySteer:      true,
			hp.HarnessCapabilityForceAbort: true,
		},
		OptionalHostCapabilities: map[hp.HostCapability]bool{
			hp.HostCapabilityUserInput:        true,
			hp.HostCapabilityToolApproval:     true,
			hp.HostCapabilityDynamicToolCall:  true,
		},
	})
	return card
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Agent 活跃周期的 DeepAgent。
// Python: harness.py:260-263 DeepAgentHarness.agent
func (h *DeepAgentHarness) Agent() *harness.DeepAgent { return h.agent }

// Card 返回 provider 卡片。
func (h *DeepAgentHarness) Card() hp.HarnessCard { return h.base.Card() }

// 协议面（HarnessProtocol）：Python 由 SerializedTurnHarness 继承获得，
// Go 以显式委托等价（base 为组合字段；hooks 已构造期注入，无虚派发问题）。
// Python: base.py SerializedTurnHarness 的对应公开方法。

// State 当前生命周期相位。
// Python: base.py SerializedTurnHarness.state
func (h *DeepAgentHarness) State() hp.HarnessState { return h.base.State() }

// ProviderSessionID provider 原生会话标识。
// Python: base.py provider_session_id
func (h *DeepAgentHarness) ProviderSessionID() *string { return h.base.ProviderSessionID() }

// EventBufferConfig 观测的有界背压策略。
// Python: base.py event_buffer_config
func (h *DeepAgentHarness) EventBufferConfig() hp.EventBufferConfig { return h.base.EventBufferConfig() }

// Start 校验宿主兼容性并开启一次周期。
// Python: base.py SerializedTurnHarness.start
func (h *DeepAgentHarness) Start(ctx context.Context, harnessContext hp.HarnessContext) error {
	return h.base.Start(ctx, harnessContext)
}

// Stop 停止周期并稳定在 terminated。
// Python: base.py SerializedTurnHarness.stop
func (h *DeepAgentHarness) Stop(ctx context.Context) error { return h.base.Stop(ctx) }

// Events 周期级有序观测流。
// Python: base.py SerializedTurnHarness.events
func (h *DeepAgentHarness) Events() hp.HarnessEventCursor { return h.base.Events() }

// TurnEvents 单轮有限观测流。
// Python: base.py SerializedTurnHarness.turn_events
func (h *DeepAgentHarness) TurnEvents(turnID *string) hp.HarnessEventCursor {
	return h.base.TurnEvents(turnID)
}

// Send 接受输入并返回 message→turn 关联。
// Python: base.py SerializedTurnHarness.send
func (h *DeepAgentHarness) Send(ctx context.Context, content hp.HarnessInput,
	mode hp.DeliveryMode) (hp.SendReceipt, error) {
	return h.base.Send(ctx, content, mode)
}

// Abort 中止在飞轮次。
// Python: base.py SerializedTurnHarness.abort
func (h *DeepAgentHarness) Abort(ctx context.Context, mode hp.AbortMode) error {
	return h.base.Abort(ctx, mode)
}

// Pause 暂停在飞轮次（native 未声明 pause_resume，走基座守卫）。
// Python: base.py SerializedTurnHarness.pause
func (h *DeepAgentHarness) Pause(ctx context.Context) error { return h.base.Pause(ctx) }

// Resume 恢复暂停轮次（同上）。
// Python: base.py SerializedTurnHarness.resume
func (h *DeepAgentHarness) Resume(ctx context.Context, query *hp.HarnessInput) error {
	return h.base.Resume(ctx, query)
}

// ExportCheckpoint 最新带版本 provider 快照。
// Python: base.py SerializedTurnHarness.export_checkpoint
func (h *DeepAgentHarness) ExportCheckpoint(ctx context.Context) (*hp.HarnessCheckpoint, error) {
	return h.base.ExportCheckpoint(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// appendContextPrompt 把宿主 system prompt 追加为 harness_context 段落。
// Python: harness.py:672-683 _append_context_prompt
func appendContextPrompt(ctx context.Context, agent *harness.DeepAgent, systemPrompt, language string) error {
	builder := agent.SystemPromptBuilder()
	if builder == nil {
		return nil
	}
	lang := language
	if lang == "" {
		lang = "cn"
	}
	// Python: content={language or "cn": prompt, "en": prompt, "cn": prompt}
	// ——三语言同值（dict 字面量去重后 cn/en 两键）。
	builder.AddSection(saprompts.PromptSection{
		Name:     contextPromptSection,
		Content:  map[string]string{"en": systemPrompt, "cn": systemPrompt},
		Priority: contextPromptPriority,
	})
	return agent.ApplyPromptBuilderToReactAgent(ctx)
}

// toolCallID 输入的调用 id；缺省回退 "{tool_name}:{react_iteration}:{uuid8}"。
// Go 侧 ToolCallInputs 无 react_iteration 字段，用 ToolCall.Index 近似
// （偏差登记：Python harness.py:691 inputs.react_iteration）。
// Python: harness.py:686-691 _tool_call_id
func toolCallID(inputs *agentinterfaces.ToolCallInputs) string {
	if inputs.ToolCall != nil && inputs.ToolCall.ID != "" {
		return inputs.ToolCall.ID
	}
	index := 0
	if inputs.ToolCall != nil {
		index = inputs.ToolCall.Index
	}
	return fmt.Sprintf("%s:%d:%s", inputs.ToolName, index, shortUUID())
}

// parseArgs 参数 JSON 字符串 → 反序列化；非字符串原样（to_json_safe）。
// Python: harness.py:694-700 _parse_args
func parseArgs(value any) any {
	if text, ok := value.(string); ok {
		var parsed any
		if err := json.Unmarshal([]byte(text), &parsed); err == nil {
			return parsed
		}
		return value
	}
	return harness_providers.ToJsonSafe(value)
}

// payloadText 帧载荷的文本内容；非 Mapping 取字符串本身。
// Python: harness.py:703-707 _payload_text
func payloadText(payload any) string {
	if m, ok := payload.(map[string]any); ok {
		if content, ok := m["content"].(string); ok {
			return content
		}
		return ""
	}
	if text, ok := payload.(string); ok {
		return text
	}
	return ""
}

// delta 输出 delta 事件。
// Python: harness.py:710-717 _delta
func delta(turnID string, channel hp.OutputChannel, text string) (hp.OutputEvent, error) {
	return hp.NewOutputEvent(
		fmt.Sprintf("deepagent-output:%s:%s", turnID, string(channel)),
		hp.OutputKindText,
		text,
		hp.OutputOperationDelta,
		channel,
		0, "", nil,
	)
}

// interactionPayload 三形态解析中断载荷：二元组 / 对象属性 / Mapping。
// 返回 (interrupt_id，值)；无 id 时空串。对象属性形态（Python getattr）
// 在 Go 流帧中对应带 "id" 键的 Mapping（帧 payload 为 map/slice/string）。
// Python: harness.py:720-728 _interaction_payload
func interactionPayload(payload any) (string, any) {
	if tuple, ok := payload.([2]any); ok {
		id, _ := tuple[0].(string)
		return id, tuple[1]
	}
	// 对象属性形态：InteractionOutput{ID, Value}（Python: harness.py:723-725
	// getattr(payload, "id") / getattr(payload, "value")——Go 侧统一交互输出结构体）
	if output, ok := payload.(*sessioninteraction.InteractionOutput); ok && output != nil {
		return output.ID, output.Value
	}
	if m, ok := payload.(map[string]any); ok {
		if id, ok := m["id"].(string); ok {
			return id, m["value"]
		}
	}
	return "", payload
}

// interruptIDOf 工具调用中断请求的 interrupt_id（issue 后才有值）。
// Python: harness.py:734 value.interrupt_id
func interruptIDOf(tcReq *saschema.ToolCallInterruptRequest) string {
	switch req := tcReq.Request.(type) {
	case *saschema.InterruptRequest:
		return req.InterruptID
	default:
		return ""
	}
}

// interruptIdentity 待决值的 interrupt_id 与 kind；workflow 交互两者为空串。
// Python: harness.py:731-735 _interrupt_identity
func interruptIdentity(value any) (string, string) {
	if request, ok := value.(*saschema.ToolCallInterruptRequest); ok {
		return interruptIDOf(request), InterruptKindOf(request)
	}
	return "", ""
}

// InterruptKindOf 中断类型，未声明返回空串。
// Python: harness.py:734 interrupt_kind_of(value)
// （Go 侧 response.go InterruptKindOf 接口形态不同——此处按
// InterruptRequest.GetInterruptKind 的运行时类型取值。）
func InterruptKindOf(tcReq *saschema.ToolCallInterruptRequest) string {
	if tcReq == nil || tcReq.Request == nil {
		return ""
	}
	return tcReq.Request.GetInterruptKind()
}

// isClientCall 是否客户端工具中断。
// Python: harness.py:738-739 _is_client_call
func isClientCall(value any) bool {
	request, ok := value.(*saschema.ToolCallInterruptRequest)
	return ok && InterruptKindOf(request) == saschema.InterruptKindClientTool
}

// interruptProviderData 请求 provider_data：身份字段 + tool_call_id + 值快照。
// Python: harness.py:742-743 _interrupt_provider_data
func interruptProviderData(callID string, value any) hp.JsonObject {
	interruptID, kind := interruptIdentity(value)
	return hp.JsonObject{
		"interrupt_id": interruptID,
		"kind":         kind,
		"tool_call_id": callID,
		"value":        harness_providers.ToJsonSafe(value),
	}
}

// resultText 宿主结果的文本投影；nil → 空串。
// Python: harness.py:746-749 _result_text
func resultText(result any) string {
	if text, ok := result.(string); ok {
		return text
	}
	if result != nil {
		return jsonTextNonASCII(result)
	}
	return ""
}

// toolArguments 原始参数 → dict；字符串先反序列化，失败包 {"arguments": raw}。
// Python: harness.py:752-758 _tool_arguments
func toolArguments(raw any) map[string]any {
	if text, ok := raw.(string); ok {
		var parsed any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			return map[string]any{"arguments": text}
		}
		raw = parsed
	}
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// interruptPrompt 把 ask_user 中断值渲染成宿主可见的 prompt：
// questions 列表逐条 question 文本换行拼接；否则 message；否则 JSON 全文；
// 空值兜底等待文案。
// Python: harness.py:761-775 _interrupt_prompt
func interruptPrompt(value any) string {
	data := harness_providers.ToJsonSafe(value)
	if m, ok := data.(map[string]any); ok {
		if questions, ok := m["questions"].([]any); ok {
			lines := make([]string, 0, len(questions))
			for _, item := range questions {
				if q, ok := item.(map[string]any); ok {
					if text, ok := q["question"].(string); ok && text != "" {
						lines = append(lines, text)
					}
				}
			}
			if rendered := strings.Join(lines, "\n"); rendered != "" {
				return rendered
			}
		}
		if message, ok := m["message"].(string); ok && message != "" {
			return message
		}
	}
	if data != nil {
		return jsonTextNonASCII(data)
	}
	return "The agent is waiting for your input."
}

// turnQuery 协议输入对应的 DeepAgent query：带 interactive_input 元数据的
// 输入携带序列化 InteractiveInput（无交互处理器的宿主也能以新输入恢复
// 挂起的中断），否则取文本。
// Python: harness.py:778-788 _turn_query
func turnQuery(content hp.HarnessInput) any {
	if metadata := content.Metadata; metadata != nil {
		if kind, ok := metadata["kind"].(string); ok && kind == interactiveInputMetadataKind {
			builtin := hp.JsonValueToBuiltin(content.Content)
			if input, inputErr := interactiveInputFromBuiltin(builtin); inputErr == nil {
				return input
			}
		}
	}
	return harness_providers.HarnessInputText(content)
}

// interactiveInputFromBuiltin 从反序列化值重建 InteractiveInput
// （Python InteractiveInput.model_validate(dict) 等价：
// 字典键 user_inputs / interrupt_answers / raw_inputs）。
// Python: harness.py:787 InteractiveInput.model_validate(...)
func interactiveInputFromBuiltin(builtin any) (*sessioninteraction.InteractiveInput, error) {
	input, err := sessioninteraction.NewInteractiveInput()
	if err != nil {
		return nil, err
	}
	if m, ok := builtin.(map[string]any); ok {
		if userInputs, ok := m["user_inputs"].(map[string]any); ok {
			input.UserInputs = userInputs
		}
		if answers, ok := m["interrupt_answers"].(map[string]any); ok {
			input.InterruptAnswers = answers
		}
		if raw, present := m["raw_inputs"]; present && raw != nil {
			input.RawInputs = raw
		}
		return input, nil
	}
	// 非 dict 形态按原始输入处理（Python model_validate 对标量的宽容度）。
	input.RawInputs = builtin
	return input, nil
}

// jsonTextNonASCII JSON 文本化（ensure_ascii=False 等价；UTF-8 直出）。
func jsonTextNonASCII(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

// uuidCounter 短 id 计数器（进程内唯一即可；Python 语义为随机 hex，
// 唯一性等价，随机性非行为契约）。
var uuidCounter atomic.Uint64

// extractRenderedResult 从工具结果中取模型可见文本。
// Python: ability_manager.py:125-146 resolve_tool_result_text（简化投影，
// 与 cli/rails/tool_tracker.go extractRendered 同款）。
func extractRenderedResult(toolResult any) (string, bool) {
	if toolResult == nil {
		return "", false
	}
	if data, ok := toolResult.(map[string]any); ok {
		if rendered, exists := data["rendered_result"]; exists {
			if text, isText := rendered.(string); isText && text != "" {
				return text, true
			}
		}
	}
	text := jsonTextNonASCII(toolResult)
	return text, text != ""
}

// shortUUID 8 位十六进制短 id（Python uuid.uuid4().hex[:8]）。
func shortUUID() string {
	return fmt.Sprintf("%08x", uuidCounter.Add(1)&0xffffffff)
}

// ──────────────────────────── Task 4/5：生命周期、主循环与中断解析 ────────────────────────────

// logComponent 本包日志组件。
const logComponent = logger.ComponentAgentCore

// newDeepAgentHarness 构造并接线基座（Go embedding 无虚派发，
// 基座经 hooks 回调本结构——构造期注入）。
// Python: harness.py:231-258 DeepAgentHarness.__init__
// HarnessOption DeepAgentHarness 的函数式选项。
// Python: harness.py:231-252 __init__ 的 keyword-only 参数。
type HarnessOption func(*harnessOptions)

type harnessOptions struct {
	agentTemplate       *hschema.AgentTemplateSpec
	sessionIDOverride   string
	language            string
	eventBufferCapacity int
}

// WithAgentTemplate 首轮前热加载到 agent 上的 manifest 模板。
// Python: harness.py:235 agent_template 形参。
func WithAgentTemplate(spec *hschema.AgentTemplateSpec) HarnessOption {
	return func(o *harnessOptions) { o.agentTemplate = spec }
}

// WithSessionID 显式 DeepAgent 会话 id；缺省为宿主会话 id 与 agent id 连接。
// Python: harness.py:236 session_id 形参。
func WithSessionID(sessionID string) HarnessOption {
	return func(o *harnessOptions) { o.sessionIDOverride = sessionID }
}

// WithLanguage 解析模板用的语言。
// Python: harness.py:237 language 形参。
func WithLanguage(language string) HarnessOption {
	return func(o *harnessOptions) { o.language = language }
}

// WithEventBufferCapacity 有界观测缓冲容量（缺省 1024）。
// Python: harness.py:238 event_buffer_capacity 形参。
func WithEventBufferCapacity(capacity int) HarnessOption {
	return func(o *harnessOptions) { o.eventBufferCapacity = capacity }
}

// NewDeepAgentHarness 构造绑定 agent 构建配方的协议适配器；agent 在 start 时构建。
// Python: harness.py:231-258 DeepAgentHarness.__init__
func NewDeepAgentHarness(agentFactory AgentFactory, opts ...HarnessOption) *DeepAgentHarness {
	o := harnessOptions{eventBufferCapacity: 1024}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	h := &DeepAgentHarness{
		agentFactory:      agentFactory,
		agentTemplate:     o.agentTemplate,
		sessionIDOverride: o.sessionIDOverride,
		language:          o.language,
	}
	h.base = harness_providers.NewSerializedTurnHarness(harness_providers.HarnessOptions{
		Card:                AgentFactoryCard(),
		Hooks:               h,
		EventBufferCapacity: o.eventBufferCapacity,
	})
	return h
}

// ValidateContext 基座校验（card.validate_host）之后追加 provider 限制。
// Python: harness.py:269-276 _validate_context
// （Go 的基座 validateContext 先做 ValidateHost 再调本钩子，等价 super() 链。）
func (h *DeepAgentHarness) ValidateContext(_ context.Context, harnessContext hp.HarnessContext) error {
	if harnessContext.ResumePolicy == hp.ResumePolicyRequireResume || harnessContext.Checkpoint != nil {
		return &hp.UnsupportedHarnessCapabilityError{
			Msg: "the DeepAgent harness does not restore protocol checkpoints",
		}
	}
	if len(harnessContext.McpServers) > 0 {
		return &hp.UnsupportedHarnessCapabilityError{
			Msg: "declare MCP servers in the agent manifest; the DeepAgent harness does not mount context MCP servers",
		}
	}
	return nil
}

// OpenSession 启动 provider 运行时：工厂建 agent → 挂观测 rail → 上下文
// 提示词 → 初始化 → 建会话 → 启动 → 模板热加载。
// Python: harness.py:278-313 _open_session
func (h *DeepAgentHarness) OpenSession(ctx context.Context, harnessContext hp.HarnessContext) (*string, error) {
	agent, session, sessionID, err := h.openSessionInner(ctx, harnessContext)
	if err != nil {
		// Python: except Exception → ProviderStartupError（message 只含异常类型名）。
		typeName := exceptionTypeName(err)
		return nil, &harness_providers.ProviderStartupError{
			Msg: "DeepAgent harness startup failed: " + typeName,
			Err:     hp.TurnError{
				Message:  orDefault(err.Error(), typeName),
				Code:     strPtr(typeName),
				Category: strPtr("sdk_error"),
			},
		}
	}
	h.agent = agent
	h.agentSession = session
	return &sessionID, nil
}

// openSessionInner OpenSession 的成功路径（Python try 块逐条）。
// Python: harness.py:281-305
func (h *DeepAgentHarness) openSessionInner(
	ctx context.Context, harnessContext hp.HarnessContext,
) (*harness.DeepAgent, *session.Session, string, error) {
	// 步骤 1：工厂构建（Python 支持协程工厂；Go 仅同步——spec 偏差 D1）。
	agent, err := h.agentFactory(ctx, harnessContext)
	if err != nil {
		return nil, nil, "", err
	}
	// 步骤 2：挂观测 rail（工具生命周期镜像）。
	agent.AddRail(&observationRail{})
	// 步骤 3：宿主上下文提示词追加。
	if harnessContext.SystemPrompt != "" {
		if err := appendContextPrompt(ctx, agent, harnessContext.SystemPrompt, h.language); err != nil {
			return nil, nil, "", err
		}
	}
	// 步骤 4：初始化（交互循环不会自己初始化 agent：pending rails（含观测
	// rail）注册与 cwd ContextVar 种子必须在此完成、于监督器与任务调度器
	// spawn 之前——Python: harness.py:290-293 注释）。
	if _, err := agent.EnsureInitialized(ctx); err != nil {
		return nil, nil, "", err
	}
	// 步骤 5：会话 id（缺省 host_session_id:agent_id）。
	sessionID := h.sessionIDOverride
	if sessionID == "" {
		sessionID = harnessContext.HostSessionID + ":" + harnessContext.AgentID
	}
	// 步骤 6：建会话并 pre_run。
	sess := session.CreateAgentSession(sessionID, agent.Card(), nil)
	if err := sess.PreRun(ctx); err != nil {
		return nil, nil, "", err
	}
	// 步骤 7：启动。
	if err := agent.Start(ctx, sess); err != nil {
		return nil, nil, "", err
	}
	// 步骤 8：模板热加载（BuildContext：language / workspace / member_card_id）。
	if h.agentTemplate != nil {
		var ws *workspace.Workspace
		if agent.DeepConfig() != nil {
			ws = agent.DeepConfig().Workspace
		}
		buildContext := &hschema.BuildContext{
			Language:     orDefault(h.language, "cn"),
			Workspace:    ws,
			MemberCardID: agent.Card().GetID(),
		}
		if _, err := agent.LoadAgentTemplateSpec(ctx, *h.agentTemplate, buildContext); err != nil {
			return nil, nil, "", err
		}
	}
	return agent, sess, sessionID, nil
}

// CloseSession 释放 provider 运行时；stop 与 post_run 各自吞异常记日志。
// Python: harness.py:315-329 _close_session
func (h *DeepAgentHarness) CloseSession(_ context.Context) error {
	agent := h.agent
	sess := h.agentSession
	h.agent = nil
	h.agentSession = nil
	if agent != nil {
		if err := agent.Stop(context.Background()); err != nil {
			logger.Error(logComponent).Err(err).Msg("[deepagent] stop failed during teardown")
		}
	}
	if sess != nil {
		if err := sess.PostRun(context.Background()); err != nil {
			logger.Error(logComponent).Err(err).Msg("[deepagent] session post_run failed during teardown")
		}
	}
	return nil
}

// ExecuteTurn 把一个已接受的输入跑到底；中断保持轮次打开、经宿主解决后
// 以 InteractiveInput 作为新 query 恢复。
// Python: harness.py:331-356 _execute_turn
func (h *DeepAgentHarness) ExecuteTurn(
	ctx context.Context, turn *harness_providers.PendingTurn,
) (hp.TurnEventKind, hp.TurnResult, error) {
	timing := harness_providers.NewTurnTiming()
	state := newTurnState(turn.TurnID)
	agent := h.agent
	if agent == nil {
		return "", hp.TurnResult{}, &hp.HarnessProtocolError{Msg: "DeepAgent disappeared during an active cycle"}
	}
	query := turnQuery(turn.Content)
	// 主循环：跑一轮 → 待决中断非空且未中止且无错 → 问宿主 → 恢复。
	for {
		if err := h.runRound(ctx, agent, turn, state, query); err != nil {

			if !turn.AbortRequested() {
				logger.Error(logComponent).Err(err).Str("turn_id", turn.TurnID).
					Msg("[deepagent] turn failed")
				state.err = &hp.TurnError{
					Message:  fmt.Sprintf("DeepAgent turn failed: %s", err.Error()),
					Code:     strPtr(exceptionTypeName(err)),
					Category: strPtr("sdk_error"),
				}
			}
			break
		}
		if turn.AbortRequested() || len(state.pendingInterrupts) == 0 || state.err != nil {
			break
		}
		resumeInput := h.resolveInterrupts(ctx, turn, state)
		if resumeInput == nil {
			break
		}
		state.pendingInterrupts = map[string]any{}
		query = resumeInput
	}
	return h.buildResult(turn, state, timing)
}

// runRound 一次 attach_output 流消费：attach → send_input → 逐 chunk 消费
// → finally 关闭（按中止标记带 abort_active_round）。
// Python: harness.py:358-367 _run_round
func (h *DeepAgentHarness) runRound(
	ctx context.Context,
	agent *harness.DeepAgent,
	turn *harness_providers.PendingTurn,
	state *turnState,
	query any,
) error {
	streamHandle, err := agent.AttachOutput(ctx)
	if err != nil {
		return err
	}
	if streamHandle == nil {
		return &hp.HarnessProtocolError{Msg: "DeepAgent output stream already has a consumer"}
	}
	defer streamHandle.Close(ctx, turn.AbortRequested())
	if err := agent.SendInput(ctx, hschema.SendInputRequest{
		RequestID: turn.TurnID,
		Inputs:    map[string]any{"query": query},
	}); err != nil {
		return err
	}
	for {
		chunk, ok := streamHandle.Next(ctx)
		if !ok {
			return nil
		}
		if err := h.consumeChunk(ctx, turn, state, chunk); err != nil {
			return err
		}
	}
}

// consumeChunk 单帧消费与事件翻译（八分支逐条）。
// Python: harness.py:369-469 _consume_chunk
func (h *DeepAgentHarness) consumeChunk(
	ctx context.Context,
	turn *harness_providers.PendingTurn,
	state *turnState,
	chunk stream.Schema,
) error {
	outChunk, ok := chunk.(sessionstream.OutputSchema)
	if !ok {
		return nil
	}
	chunkType := outChunk.Type
	payload := outChunk.Payload

	switch chunkType {
	case "llm_output":
		// Python: harness.py:375-381
		if text := payloadText(payload); text != "" {
			state.answerParts = append(state.answerParts, text)
			state.emittedOutput = true
			event, err := delta(turn.TurnID, hp.OutputChannelAnswer, text)
			if err != nil {
				return err
			}
			return h.emitEvent(ctx, event, turn)
		}
		return nil
	case "llm_reasoning":
		// Python: harness.py:382-387
		if text := payloadText(payload); text != "" {
			state.reasoningParts = append(state.reasoningParts, text)
			event, err := delta(turn.TurnID, hp.OutputChannelReasoning, text)
			if err != nil {
				return err
			}
			return h.emitEvent(ctx, event, turn)
		}
		return nil
	case toolCallChunk:
		// Python: harness.py:388-409
		m, ok := payload.(map[string]any)
		if !ok {
			return nil
		}
		callID := orDefault(strAny(m["tool_call_id"]), shortUUID())
		name := orDefault(strAny(m["tool_name"]), "unknown")
		arguments := harness_providers.ToJsonSafe(m["tool_args"])
		block, err := hp.NewContentBlock(
			fmt.Sprintf("%s:tool:%s", turn.TurnID, callID), "tool_call",
			hp.JsonObject{"name": name, "arguments": arguments},
			hp.JsonObject{"call_id": callID})
		if err != nil {
			return err
		}
		state.toolBlocks = append(state.toolBlocks, block)
		itemData, err := hp.FreezeJsonObject(hp.JsonObject{"name": name, "arguments": arguments})
		if err != nil {
			return err
		}
		event, err := hp.NewItemLifecycleEvent(hp.ItemEventKindStarted, "tool", itemData)
		if err != nil {
			return err
		}
		return h.emitItemEvent(ctx, event, turn, callID)
	case toolResultChunk:
		// Python: harness.py:410-443
		m, ok := payload.(map[string]any)
		if !ok {
			return nil
		}
		callID := orDefault(strAny(m["tool_call_id"]), shortUUID())
		name := orDefault(strAny(m["tool_name"]), "unknown")
		result := harness_providers.ToJsonSafe(m["tool_result"])
		renderedResult, _ := m["rendered_result"].(string)
		blockData := hp.JsonObject{"call_id": callID}
		itemData := hp.JsonObject{"tool_name": name, "result": result}
		if renderedResult != "" {
			blockData["rendered_result"] = renderedResult
			itemData["rendered_result"] = renderedResult
		}
		block, err := hp.NewContentBlock(
			fmt.Sprintf("%s:tool-result:%s", turn.TurnID, callID), "tool_result",
			result, blockData)
		if err != nil {
			return err
		}
		msg, err := hp.NewTurnMessage(
			fmt.Sprintf("%s:tool-result:%s", turn.TurnID, callID), hp.MessageRoleTool,
			[]hp.ContentBlock{block}, nil)
		if err != nil {
			return err
		}
		state.toolMessages = append(state.toolMessages, msg)
		frozen, err := hp.FreezeJsonObject(itemData)
		if err != nil {
			return err
		}
		event, err := hp.NewItemLifecycleEvent(hp.ItemEventKindCompleted, "tool", frozen)
		if err != nil {
			return err
		}
		return h.emitItemEvent(ctx, event, turn, callID)
	case "answer":
		// Python: harness.py:444-448
		m, ok := payload.(map[string]any)
		if !ok {
			return nil
		}
		output := m["output"]
		if text, isText := output.(string); isText {
			state.finalOutput = &text
		} else {
			jsonText := jsonTextNonASCII(harness_providers.ToJsonSafe(output))
			state.finalOutput = &jsonText
		}
		if rt := strAny(m["result_type"]); rt != "" {
			state.resultType = &rt
		} else {
			answer := "answer"
			state.resultType = &answer
		}
		return nil
	case sessioninteraction.InteractionType:
		// Python: harness.py:449-453（INTERACTION chunk）
		interruptID, value := interactionPayload(payload)
		if interruptID != "" {
			state.pendingInterrupts[interruptID] = value
		}
		return nil
	case string(hschema.InteractionEventTypeExecutionError):
		// Python: harness.py:454-459
		m, ok := payload.(map[string]any)
		if !ok {
			return nil
		}
		message := orDefault(strAny(m["message"]), "DeepAgent execution error")
		code := orDefault(strAny(m["code"]), "execution_error")
		state.err = &hp.TurnError{
			Message:  message,
			Code:     strPtr(code),
			Category: strPtr("sdk_error"),
		}
		return nil
	default:
		// Python: harness.py:461-469 其余 → ProviderEvent 透传
		event, err := hp.NewProviderEvent(ProviderName,
			orDefault(chunkType, "chunk"), "1",
			hp.JsonObject(toJsonObject(payload)))
		if err != nil {
			return err
		}
		return h.emitEvent(ctx, event, turn)
	}
}

// resolveInterrupts 逐个向宿主征答待决中断；None 保持轮次打开。
// 客户端调用最后问：宿主答了客户端调用这轮必须带结果继续，所以一切
// 可能停轮的中断先问（Python: harness.py:477-483 docstring）。
// Python: harness.py:471-494 _resolve_interrupts
func (h *DeepAgentHarness) resolveInterrupts(
	ctx context.Context,
	turn *harness_providers.PendingTurn,
	state *turnState,
) *sessioninteraction.InteractiveInput {
	if !h.base.HasHostInteractions() {
		return nil
	}
	interactiveInput, err := sessioninteraction.NewInteractiveInput()
	if err != nil {
		return nil
	}
	keys := state.pendingKeysSorted()
	sort.SliceStable(keys, func(i, j int) bool {
		return !isClientCall(state.pendingInterrupts[keys[i]]) &&
			isClientCall(state.pendingInterrupts[keys[j]])
	})
	for _, callID := range keys {
		value := state.pendingInterrupts[callID]
		if request, ok := value.(*saschema.ToolCallInterruptRequest); ok {
			answer := h.answerInterrupt(ctx, turn, callID, request)
			if answer == nil {
				return nil
			}
			// Python: interactive_input.answer(value.interrupt_id, answer)
			// ——按 interrupt_id 绑定（Go 为直接写 InterruptAnswers 映射，
			// 与 repl.go:534 同款）。
			interactiveInput.InterruptAnswers[interruptIDOf(request)] = answer
		} else {
			answer := h.requestUserInput(ctx, turn, callID, value, nil)
			if answer == nil {
				return nil
			}
			if err := interactiveInput.Update(callID, answer); err != nil {
				return nil
			}
		}
	}
	return interactiveInput
}

// answerInterrupt 按中断类型翻译与征答。
// Python: harness.py:496-509 _answer_interrupt
func (h *DeepAgentHarness) answerInterrupt(
	ctx context.Context,
	turn *harness_providers.PendingTurn,
	callID string,
	request *saschema.ToolCallInterruptRequest,
) any {
	kind := InterruptKindOf(request)
	if kind == saschema.InterruptKindClientTool {
		return h.requestClientTool(ctx, turn, callID, request)
	}
	if kind == saschema.InterruptKindUndeclared || !interruptpkg.IsApprovalKind(kind) {
		// 未声明类型保持本 harness 既有的回答语义：宿主内容原样。
		// Python: harness.py:500-502
		return h.requestUserInput(ctx, turn, callID, request, nil)
	}
	if h.hostDeclares(hp.HostCapabilityToolApproval) {
		return h.requestApproval(ctx, turn, callID, kind, request)
	}
	// 纯文本宿主：给 allow/allow_always/deny 三选，文本经 interrupt_answer
	// 归一；其余内容（含 dict）原样返回、什么都没有按拒绝。
	// Python: harness.py:505-509
	content := h.requestUserInput(ctx, turn, callID, request, interruptpkg.ApprovalChoices)
	if content == nil {
		return nil
	}
	if _, isMap := content.(map[string]any); isMap {
		return content
	}
	if _, isList := content.([]any); isList {
		return content
	}
	answer, err := interruptpkg.InterruptAnswerFor(kind, interruptpkg.InterruptAnswerOptions{
		Approved: boolPtr(true),
		Text:     strPtr(fmt.Sprintf("%v", content)),
	})
	if err != nil {
		return nil
	}
	return answer
}

// hostDeclares 宿主是否声明了某可选能力（card optional ∩ context 声明）。
// Python: harness.py:511-515 _host_declares
func (h *DeepAgentHarness) hostDeclares(capability hp.HostCapability) bool {
	card := h.base.Card()
	harnessContext := h.base.Context()
	return card.OptionalHostCapabilities != nil && card.OptionalHostCapabilities[capability] &&
		harnessContext != nil && harnessContext.HostCapabilities != nil &&
		harnessContext.HostCapabilities[capability]
}

// requestUserInput 请求宿主补充输入；非 COMPLETED 或无处理器 → nil。
// 完成但无内容以空文本回答。
// Python: harness.py:517-534 _request_user_input
func (h *DeepAgentHarness) requestUserInput(
	ctx context.Context,
	turn *harness_providers.PendingTurn,
	callID string,
	value any,
	choices []string,
) any {
	providerData := interruptProviderData(callID, value)
	requestID := providerData["interrupt_id"].(string)
	if requestID == "" {
		requestID = callID
	}
	request, err := hp.NewUserInputRequest(requestID, interruptPrompt(value), hp.UserInputRequestOptions{
		ProviderSessionID: h.base.ProviderSessionID(),
		TurnID:            strPtr(turn.TurnID),
		Choices:           choices,
		ProviderData:      providerData,
	})
	if err != nil {
		return nil
	}
	response, err := h.base.RequestInteraction(ctx, request)
	if err != nil || response == nil {
		return nil
	}
	userResponse, ok := response.(hp.UserInputResponse)
	if !ok || userResponse.Status != hp.InteractionResponseStatusCompleted {
		return nil
	}
	// 完成但无内容 → 空文本（Python: harness.py:532-534）。
	content := hp.JsonValueToBuiltin(userResponse.Content)
	if content == nil {
		return ""
	}
	return content
}

// requestApproval 请求宿主审批工具调用。
// Python: harness.py:536-568 _request_approval
func (h *DeepAgentHarness) requestApproval(
	ctx context.Context,
	turn *harness_providers.PendingTurn,
	callID string,
	kind string,
	request *saschema.ToolCallInterruptRequest,
) any {
	title := metadataString(request, "ask_title")
	description := pointerIfNonEmpty(request.Request.GetMessage())
	reason := metadataString(request, "ask_summary")
	approval, err := hp.NewToolApprovalRequest(
		interruptIDOf(request), callID, request.ToolName, hp.ToolApprovalRequestOptions{
			Arguments:         hp.JsonObject(toolArguments(request.ToolArgs)),
			ProviderSessionID: h.base.ProviderSessionID(),
			TurnID:            strPtr(turn.TurnID),
			Title:             title,
			Description:       description,
			Reason:            reason,
			ProviderData:      interruptProviderData(callID, request),
		})
	if err != nil {
		return nil
	}
	response, err := h.base.RequestInteraction(ctx, approval)
	if err != nil || response == nil {
		return nil
	}
	decision, ok := response.(hp.ToolApprovalResponse)
	if !ok {
		return nil
	}
	// ABORT 或 interrupt=true → nil（停轮）。
	if decision.Decision == hp.ToolApprovalDecisionAbort || decision.Interrupt {
		return nil
	}
	switch decision.Decision {
	case hp.ToolApprovalDecisionAllow, hp.ToolApprovalDecisionAllowForSession:
		// 宿主改写参数：绝不带着原参数执行——回答拒绝该调用。
		// Python: harness.py:555-562
		if decision.UpdatedArguments != nil {
			answer, ansErr := interruptpkg.InterruptAnswerFor(kind, interruptpkg.InterruptAnswerOptions{
				Approved: boolPtr(false),
				Feedback: "The host edited the tool arguments while approving; editing arguments is not " +
				"supported, so the call was not run.",
			})
			if ansErr != nil {
				return nil
			}
			return answer
		}
		remember := decision.Decision == hp.ToolApprovalDecisionAllowForSession
		answerMap, ansErr := interruptpkg.InterruptAnswerFor(kind, interruptpkg.InterruptAnswerOptions{
			Approved: boolPtr(true),
			Remember: remember,
		})
		if ansErr != nil {
			return nil
		}
		// ALLOW_FOR_SESSION 且宿主声明持久化 → persist_allow: true
		// （Python: harness.py:563-566；answer dict 键存在才写）。
		if remember {
			if answerDict, isDict := answerMap.(map[string]any); isDict {
				if _, hasKey := answerDict["persist_allow"]; hasKey {
					if providerData := decision.ProviderData; providerData != nil {
						if providerData["persist_allow"] == true {
							answerDict["persist_allow"] = true
						}
					}
				}
			}
		}
		return answerMap
	default:
		// DENY → feedback（Python: harness.py:568）。
		reasonText := "Denied by the host."
		if decision.Reason != nil && *decision.Reason != "" {
			reasonText = *decision.Reason
		}
		answer, ansErr := interruptpkg.InterruptAnswerFor(kind, interruptpkg.InterruptAnswerOptions{
			Approved: boolPtr(false),
			Feedback: reasonText,
		})
		if ansErr != nil {
			return nil
		}
		return answer
	}
}

// requestClientTool 请求宿主执行客户端工具调用。
// Python: harness.py:570-607 _request_client_tool
func (h *DeepAgentHarness) requestClientTool(
	ctx context.Context,
	turn *harness_providers.PendingTurn,
	callID string,
	request *saschema.ToolCallInterruptRequest,
) *tool.ClientToolResult {
	if !h.hostDeclares(hp.HostCapabilityDynamicToolCall) {
		return tool.NewClientToolResult(tool.ClientToolStatusError,
			"This host cannot run client tools (it did not declare DYNAMIC_TOOL_CALL).", true, nil)
	}
	providerData := interruptProviderData(callID, request)
	// Python: value.client_request —— ClientToolRequest 子类的 client_request 字段；
	// Go 的 ToolCallInterruptRequest 用内嵌 Requester 承载子类，取其 ClientRequest。
	providerData["client_request"] = clientRequestOf(request)
	dynamicCall, err := hp.NewDynamicToolCallRequest(
		interruptIDOf(request), callID, request.ToolName, hp.DynamicToolCallRequestOptions{
			Arguments:         hp.JsonObject(toolArguments(request.ToolArgs)),
			ProviderSessionID: h.base.ProviderSessionID(),
			TurnID:            strPtr(turn.TurnID),
			ProviderData:      providerData,
		})
	if err != nil {
		return tool.NewClientToolResult(tool.ClientToolStatusError, err.Error(), true, nil)
	}
	response, err := h.base.RequestInteraction(ctx, dynamicCall)
	if err != nil || response == nil {
		return nil
	}
	result, ok := response.(hp.DynamicToolCallResponse)
	if !ok {
		return nil
	}
	content := hp.JsonValueToBuiltin(result.Result)
	// 错误：is_error 或 FAILED → ERROR（Python: harness.py:593-598）。
	if result.IsError || result.Status == hp.InteractionResponseStatusFailed {
		message := orDefault(pointerText(result.ErrorMessage), resultText(content))
		return tool.NewClientToolResult(tool.ClientToolStatusError, message, true, nil)
	}
	// 非 COMPLETED → CANCELLED（Python: harness.py:599-603）。
	if result.Status != hp.InteractionResponseStatusCompleted {
		return tool.NewClientToolResult(tool.ClientToolStatusCancelled,
			fmt.Sprintf("The host did not run the call (%s).", string(result.Status)), false, nil)
	}
	// 文本与内容块原样作为结果；其他 JSON 以 JSON 展示并保留为 data
	// （Python: harness.py:604-607）。
	switch value := content.(type) {
	case string:
		return tool.NewClientToolResult(tool.ClientToolStatusCompleted, value, false, nil)
	case []any:
		return tool.NewClientToolResult(tool.ClientToolStatusCompleted, value, false, nil)
	default:
		return tool.NewClientToolResult(tool.ClientToolStatusCompleted,
			resultText(content), false, content)
	}
}

// buildResult 轮次结果：中止 → ABORTED；有错 → FAILED；否则 COMPLETED。
// Python: harness.py:609-649 _build_result
func (h *DeepAgentHarness) buildResult(
	turn *harness_providers.PendingTurn,
	state *turnState,
	timing harness_providers.TurnTiming,
) (hp.TurnEventKind, hp.TurnResult, error) {
	finalOutput := ""
	if state.finalOutput != nil {
		finalOutput = *state.finalOutput
	} else if len(state.answerParts) > 0 {
		finalOutput = strings.Join(state.answerParts, "")
	}
	messages, err := state.Messages()
	if err != nil {
		return "", hp.TurnResult{}, err
	}
	if turn.AbortRequested() {
		return hp.TurnEventKindAborted, harness_providers.InterruptedResult(turn,
			harness_providers.InterruptedResultOptions{
				ProviderName: ProviderName,
				Timing:       timing,
				Messages:     messages,
				FinalOutput:  finalOutput,
			}), nil
	}
	if state.err != nil {
		startedAt := timing.StartedAt
		completedAt := timing.CompletedAt()
		durationMS := timing.DurationMs()
		result, resultErr := hp.NewTurnResult(hp.TurnStatusFailed, hp.ResultOptions{
			Messages:    messages,
			FinalOutput: finalOutput,
			StartedAt:   &startedAt,
			CompletedAt: &completedAt,
			DurationMS:  &durationMS,
			Error:       state.err,
		})
		if resultErr != nil {
			return "", hp.TurnResult{}, resultErr
		}
		return hp.TurnEventKindFailed, result, nil
	}
	stopReason := orDefault(deref(state.resultType), "answer")
	providerData := hp.JsonObject{}
	if len(state.pendingInterrupts) > 0 {
		stopReason = "interrupt"
		keys := state.pendingKeysSorted()
		ids := make([]any, 0, len(keys))
		identity := make([]any, 0, len(keys))
		for _, key := range keys {
			ids = append(ids, key)
			interruptID, kind := interruptIdentity(state.pendingInterrupts[key])
			// tool_call_id 按历史键保留；每项附其中断身份。
			// Python: harness.py:638-643
			identity = append(identity, map[string]any{
				"tool_call_id": key,
				"interrupt_id": interruptID,
				"kind":         kind,
			})
		}
		providerData["pending_interrupt_ids"] = ids
		providerData["pending_interrupts"] = identity
	}
	startedAt := timing.StartedAt
	completedAt := timing.CompletedAt()
	durationMS := timing.DurationMs()
	result, resultErr := hp.NewTurnResult(hp.TurnStatusCompleted, hp.ResultOptions{
		Messages:     messages,
		FinalOutput:  finalOutput,
		StartedAt:    &startedAt,
		CompletedAt:  &completedAt,
		DurationMS:   &durationMS,
		StopReason:   strPtr(stopReason),
		ProviderData: providerData,
	})
	if resultErr != nil {
		return "", hp.TurnResult{}, resultErr
	}
	return hp.TurnEventKindFinished, result, nil
}

// Steer 把内容以 STEER 模式注入活跃轮次。
// Python: harness.py:651-662 _steer
func (h *DeepAgentHarness) Steer(
	ctx context.Context,
	turn *harness_providers.PendingTurn,
	content hp.HarnessInput,
	messageID string,
) error {
	_ = turn
	_ = messageID
	agent := h.agent
	if agent == nil {
		return &hp.HarnessProtocolError{Msg: "DeepAgent disappeared during an active cycle"}
	}
	return agent.SendInput(ctx, hschema.SendInputRequest{
		RequestID: "steer-" + shortUUID(),
		Inputs:    map[string]any{"query": harness_providers.HarnessInputText(content)},
		Mode:      hschema.InputDispatchModeSteer,
	})
}

// InterruptTurn 请求 provider 停止活跃轮次。
// Python: harness.py:664-669 _interrupt_turn
func (h *DeepAgentHarness) InterruptTurn(
	ctx context.Context,
	turn *harness_providers.PendingTurn,
	mode hp.AbortMode,
) error {
	_ = turn
	_ = mode
	agent := h.agent
	if agent == nil {
		return nil
	}
	_, err := agent.CancelRound(ctx, "harness_abort")
	return err
}

// ListModels provider 未声明 MODEL_DISCOVERY。
// Python: base.py:225-227 _list_models 默认体
func (h *DeepAgentHarness) ListModels(_ context.Context) ([]hp.ModelOption, error) {
	return nil, &hp.UnsupportedHarnessCapabilityError{
		Msg: ProviderName + " does not support model discovery",
	}
}

// AuthFallbackPayload 未配置认证回退。
// Python: base.py:229-239 _auth_fallback_payload 默认体
func (h *DeepAgentHarness) AuthFallbackPayload() (hp.JsonObject, bool) { return nil, false }

// ReconnectForAuth 无认证回退。
// Python: base.py:241-252 _reconnect_for_auth 默认体
func (h *DeepAgentHarness) ReconnectForAuth(_ context.Context, _ bool) error {
	return &hp.HarnessProtocolError{Msg: ProviderName + " has no authentication fallback"}
}

// OnAuthEndpointChanged 默认 no-op。
// Python: base.py:254-256 _on_auth_endpoint_changed 默认体
func (h *DeepAgentHarness) OnAuthEndpointChanged(_ context.Context, _ bool) error { return nil }

// ApplyModelSelection 不支持模型选择。
// Python: base.py:258-265 _apply_model_selection 默认体
func (h *DeepAgentHarness) ApplyModelSelection(_ context.Context, _ hp.ModelSelection) error {
	return &hp.UnsupportedHarnessCapabilityError{
		Msg: ProviderName + " does not support model selection",
	}
}

// emitEvent / emitItemEvent 事件发射（经基座 Emit）。
func (h *DeepAgentHarness) emitEvent(ctx context.Context, event hp.HarnessEventPayload, turn *harness_providers.PendingTurn) error {
	return h.base.Emit(ctx, event, harness_providers.EmitOptions{Turn: turn})
}

func (h *DeepAgentHarness) emitItemEvent(ctx context.Context, event hp.ItemLifecycleEvent, turn *harness_providers.PendingTurn, itemID string) error {
	return h.base.Emit(ctx, event, harness_providers.EmitOptions{Turn: turn, ItemID: itemID})
}

// ──────────────────────────── 小助手 ────────────────────────────

// exceptionTypeName 错误的类型名（Python type(exc).__name__ 等价）。
func exceptionTypeName(err error) string {
	if err == nil {
		return ""
	}
	typeName := reflect.TypeOf(err).String()
	if idx := strings.LastIndex(typeName, "."); idx >= 0 {
		typeName = typeName[idx+1:]
	}
	typeName = strings.TrimPrefix(typeName, "*")
	return typeName
}

// metadataString 中断元数据的字符串字段。
// clientRequestOf 提取内嵌 ClientToolRequest 的 client_request 字段。
// Python: harness.py:586-589 value.client_request（merged ToolCallInterruptRequest
// 上来自 ClientToolRequest 子类的 extra 字段）。
func clientRequestOf(request *saschema.ToolCallInterruptRequest) any {
	if inner, ok := request.Request.(*saschema.ClientToolRequest); ok {
		return harness_providers.ToJsonSafe(inner.ClientRequest)
	}
	return harness_providers.ToJsonSafe(request.Request)
}

func metadataString(request *saschema.ToolCallInterruptRequest, key string) *string {
	if value, ok := request.Request.GetMetadata()[key].(string); ok && value != "" {
		return &value
	}
	return nil
}

// pointerIfNonEmpty 非空字符串 → 指针；空 → nil。
func pointerIfNonEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// pointerText 指针文本或空串。
func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// strAny any → 字符串（非字符串返回空串）。
func strAny(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// orDefault 空串兜底。
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// deref 字符串指针取值。
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// strPtr 字符串指针。
func strPtr(value string) *string { return &value }

// boolPtr 布尔指针。
func boolPtr(value bool) *bool { return &value }

// toJsonObject 载荷 → JsonObject（非 map 载荷包 data 键，保持信息不丢）。
// toJsonObject payload 归一为 JSON 对象（先 ToJsonSafe 清洗，非对象包 {"value": ...}）。
// Python: harness_providers/jsonsafe.py:46-52 to_json_object
func toJsonObject(payload any) map[string]any {
	converted := harness_providers.ToJsonSafe(payload)
	if obj, ok := converted.(map[string]any); ok {
		return obj
	}
	return map[string]any{"value": converted}
}
