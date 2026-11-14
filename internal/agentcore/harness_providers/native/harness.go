package native

// HarnessProtocol 在 in-process DeepAgent 交互循环之上的实现。
// Python: openjiuwen/harness_providers/native/harness.py（一比一迁移）

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	harness "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	harness_providers "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessionstream "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
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
