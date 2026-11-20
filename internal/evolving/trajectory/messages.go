package trajectory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// messages.go 轨迹 span 到聊天消息的规范投影。
// 对齐 Python: agent_evolving/trajectory/messages.py（全文，2026-12-04 快照）。

// ──────────────────────────── 常量 ────────────────────────────

// MessageField 消息投影可选取的字段名。
// Python: messages.py MessageField = Literal["content","reasoning_content","name","tool_calls","tool_call_id"]。
type MessageField = string

// DefaultEvolutionMessageFields 演进投影的默认字段集。
// Python: DEFAULT_EVOLUTION_MESSAGE_FIELDS。
var DefaultEvolutionMessageFields = map[string]struct{}{
	"content": {}, "name": {}, "tool_calls": {}, "tool_call_id": {},
}

// messageFieldsSelectable 默认字段 + reasoning_content（可选取但非默认：
// 演进读的是 agent 说的内容，想读思考的读取方自己要求）。
// Python: _MESSAGE_FIELDS。
var messageFieldsSelectable = func() map[string]struct{} {
	fields := map[string]struct{}{}
	for field := range DefaultEvolutionMessageFields {
		fields[field] = struct{}{}
	}
	fields["reasoning_content"] = struct{}{}
	return fields
}()

// promptAttachmentHistoryMetadataKey PromptAttachment 的历史元数据键。
// Python: messages.py _PROMPT_ATTACHMENT_HISTORY_METADATA_KEY。
const promptAttachmentHistoryMetadataKey = "_openjiuwen_prompt_attachment_history"

// ──────────────────────────── 非导出函数 ────────────────────────────

// messageField 消息字段读取（映射形态）。
// Python: messages.py _field。
func messageField(value map[string]any, name string) any {
	if value == nil {
		return nil
	}
	return value[name]
}

// toolCallField 先读扁平字段，空回落 function 嵌套字段。
// Python: messages.py _tool_call_field。
func toolCallField(toolCall map[string]any, name string) any {
	value := messageField(toolCall, name)
	if value != nil && value != "" {
		return value
	}
	function, _ := toolCall["function"].(map[string]any)
	if function != nil {
		if nested := messageField(function, name); nested != nil {
			return nested
		}
	}
	return value
}

// ToolCallID 从扁平或 OpenAI 嵌套输入读 tool-call ID。
// Python: messages.py tool_call_id。
func ToolCallID(toolCall map[string]any) any { return toolCallField(toolCall, "id") }

// ToolCallName 从扁平或 OpenAI 嵌套输入读 tool-call 名。
// Python: messages.py tool_call_name。
func ToolCallName(toolCall map[string]any) any { return toolCallField(toolCall, "name") }

// ToolCallArguments 从扁平或 OpenAI 嵌套输入读 tool-call 参数。
// Python: messages.py tool_call_arguments。
func ToolCallArguments(toolCall map[string]any) any { return toolCallField(toolCall, "arguments") }

// jsonText JSON 紧凑序列化（非字符串值）；字符串原样。
// Python: messages.py _json_text。
func jsonText(value any) string {
	if text, isString := value.(string); isString {
		return text
	}
	compatible := toJsonCompatible(value)
	// Python: json.dumps(..., sort_keys=True, separators=(",",":"))。
	data, err := json.Marshal(sortJSONKeys(compatible))
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

// sortJSONKeys 递归对 map 键排序（JSON 序列化前的 sort_keys 语义）。
func sortJSONKeys(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := sortedKeys(v)
		ordered := map[string]any{}
		for _, key := range keys {
			ordered[key] = sortJSONKeys(v[key])
		}
		return ordered
	case []any:
		for i, item := range v {
			v[i] = sortJSONKeys(item)
		}
		return v
	default:
		return value
	}
}

// normalizeToolCall 归一化一条 tool call（id/type/function.name/arguments）。
// Python: messages.py _normalize_tool_call。
func normalizeToolCall(toolCall map[string]any) map[string]any {
	result := map[string]any{}
	if callID := ToolCallID(toolCall); callID != nil {
		result["id"] = copyJSON(callID)
	}
	callType := messageField(toolCall, "type")
	if callType != nil {
		result["type"] = copyJSON(callType)
	} else {
		result["type"] = "function"
	}
	function := map[string]any{}
	if name := ToolCallName(toolCall); name != nil {
		function["name"] = copyJSON(name)
	}
	if arguments := ToolCallArguments(toolCall); arguments != nil {
		function["arguments"] = jsonText(arguments)
	}
	result["function"] = function
	return result
}

// normalizeMessage 归一化消息（tool_calls 列表逐条归一化）。
// Python: messages.py _normalize_message。
func normalizeMessage(message map[string]any) map[string]any {
	result := copyJSON(message).(map[string]any)
	toolCalls, isList := result["tool_calls"].([]any)
	if isList {
		normalized := make([]any, 0, len(toolCalls))
		for _, item := range toolCalls {
			call, isMap := item.(map[string]any)
			if isMap {
				normalized = append(normalized, normalizeToolCall(call))
			}
		}
		result["tool_calls"] = normalized
	}
	return result
}

// isPromptAttachmentUser 是否 PromptAttachmentManager 的 UserMessage 脚手架。
// 附件以 role=user + <system-reminder> 正文（可选历史元数据）持久化，
// 不得当作 invoke 本地任务入口。
// Python: messages.py _is_prompt_attachment_user。
func isPromptAttachmentUser(message map[string]any) bool {
	metadata, isMap := message["metadata"].(map[string]any)
	if isMap && metadata[promptAttachmentHistoryMetadataKey] != nil {
		return true
	}
	content, isString := message["content"].(string)
	return isString && strings.Contains(content, "<system-reminder>")
}

// trimPromptToLastUser 保留当前 invoke 入口：最后一条真实 user 消息至末尾。
// LLM span 可能存完整请求 prompt（含先前会话轮）；invoke_local=True 的调用方
// （TTSE detect/induce）让缺窗口回退不重导入那段历史。这是切片，不是文本重叠合并。
// 选择切点时跳过 prompt-attachment user（<system-reminder>），保住真实任务查询；
// 真实 user 之后的附件仍留在返回切片。
// Python: messages.py _trim_prompt_to_last_user。
func trimPromptToLastUser(prompt []map[string]any) []map[string]any {
	lastUser := -1
	for index, message := range prompt {
		if fmt.Sprint(message["role"]) != "user" {
			continue
		}
		if isPromptAttachmentUser(message) {
			continue
		}
		lastUser = index
	}
	if lastUser < 0 {
		return []map[string]any{}
	}
	return prompt[lastUser:]
}

// toolMessage 由 tool call 构造 role=tool 消息。
// Python: messages.py _tool_message。
func toolMessage(toolCall map[string]any, toolCallNames map[string]any) map[string]any {
	result := map[string]any{"role": "tool"}
	callID := toolCall["id"]
	name := toolCall["name"]
	if name == nil && callID != nil {
		name = toolCallNames[fmt.Sprint(callID)]
	}
	if name != nil {
		result["name"] = copyJSON(name)
	}
	if callID != nil {
		result["tool_call_id"] = copyJSON(callID)
	}
	if output := toolCall["output"]; output != nil {
		result["content"] = jsonText(output)
	} else if err, isMap := toolCall["error"].(map[string]any); isMap && err["message"] != nil {
		result["content"] = fmt.Sprint(err["message"])
	}
	return result
}

// selectFields 只保留 role 与选定字段。
// Python: messages.py _select_fields。
func selectFields(message map[string]any, fields map[string]struct{}) map[string]any {
	result := map[string]any{}
	for key, value := range message {
		if key == "role" {
			result[key] = copyJSON(value)
			continue
		}
		if _, selected := fields[key]; selected {
			result[key] = copyJSON(value)
		}
	}
	return result
}

// windowMessage 把一条上下文窗口消息渲染为聊天消息。
// Python: messages.py _window_message。
func windowMessage(message map[string]any) map[string]any {
	result := map[string]any{"role": message["role"]}
	for _, key := range []string{"content", "name", "tool_calls", "tool_call_id"} {
		if value, exists := message[key]; exists && value != nil {
			result[key] = copyJSON(value)
		}
	}
	return normalizeMessage(result)
}

// toolCallIDsOf 消息 tool_calls 的 ID 序列。
// Python: messages.py _tool_call_ids。
func toolCallIDsOf(message map[string]any) []string {
	toolCalls, _ := message["tool_calls"].([]any)
	var ids []string
	for _, item := range toolCalls {
		call, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		if id := ToolCallID(call); id != nil {
			ids = append(ids, fmt.Sprint(id))
		}
	}
	return ids
}

// sameAssistantTurn 窗口的 assistant 消息是否先前记录过的 completion。
// completion 先于任何窗口点名它而记录，没有 message_id 可连接；
// 其 tool_calls 有 id；无 tool call 的轮按文本连接（上下文引擎原样保留）。
// Python: messages.py _same_assistant_turn。
func sameAssistantTurn(completion, message map[string]any) bool {
	completionCalls := toolCallIDsOf(completion)
	messageCalls := toolCallIDsOf(message)
	if len(completionCalls) > 0 || len(messageCalls) > 0 {
		return strings.Join(completionCalls, "\x00") == strings.Join(messageCalls, "\x00")
	}
	return jsonText(completion["content"]) == jsonText(message["content"])
}

// messageProjection 从窗口、completion 与工具结果累积一段对话。
// Python: messages.py _MessageProjection。
type messageProjection struct {
	messages               []map[string]any
	issues                 []map[string]any
	seenMessageIDs         map[string]struct{}
	unconfirmedCompletions []int
	toolResults            map[string]int
	toolCallNames          map[string]any
}

func newMessageProjection() *messageProjection {
	return &messageProjection{
		seenMessageIDs: map[string]struct{}{},
		toolResults:    map[string]int{},
		toolCallNames:  map[string]any{},
	}
}

// addWindow 添加一个窗口的消息（按 message_id 去重）。
// Python: messages.py _MessageProjection.add_window。
func (p *messageProjection) addWindow(window []map[string]any) {
	for _, message := range window {
		messageID := fmt.Sprint(message["message_id"])
		if _, seen := p.seenMessageIDs[messageID]; seen {
			continue
		}
		p.seenMessageIDs[messageID] = struct{}{}
		rendered := windowMessage(message)
		if fmt.Sprint(rendered["role"]) == "assistant" && p.confirmCompletion(rendered) {
			continue
		}
		if fmt.Sprint(rendered["role"]) == "tool" && p.confirmToolResult(rendered) {
			continue
		}
		p.rememberToolCallNames(rendered)
		p.messages = append(p.messages, rendered)
	}
}

// addPrompt 添加 prompt 消息（原样追加）。
// Python: messages.py _MessageProjection.add_prompt。
func (p *messageProjection) addPrompt(prompt []map[string]any) {
	for _, message := range prompt {
		p.rememberToolCallNames(message)
		p.messages = append(p.messages, copyJSON(message).(map[string]any))
	}
}

// addCompletions 添加 completion 消息。
// Python: messages.py _MessageProjection.add_completions。
func (p *messageProjection) addCompletions(completions []map[string]any) {
	for _, message := range completions {
		p.rememberToolCallNames(message)
		if fmt.Sprint(message["role"]) == "assistant" {
			p.unconfirmedCompletions = append(p.unconfirmedCompletions, len(p.messages))
		}
		p.messages = append(p.messages, copyJSON(message).(map[string]any))
	}
}

// addToolResult 添加工具结果消息。
// Python: messages.py _MessageProjection.add_tool_result。
func (p *messageProjection) addToolResult(call map[string]any) {
	message := toolMessage(call, p.toolCallNames)
	if callID := message["tool_call_id"]; callID != nil {
		p.toolResults[fmt.Sprint(callID)] = len(p.messages)
	}
	p.messages = append(p.messages, message)
}

// confirmCompletion 匹配未确认 completion（命中即消费其及更早项）。
// Python: messages.py _MessageProjection._confirm_completion。
func (p *messageProjection) confirmCompletion(message map[string]any) bool {
	for position, index := range p.unconfirmedCompletions {
		if sameAssistantTurn(p.messages[index], message) {
			p.unconfirmedCompletions = p.unconfirmedCompletions[position+1:]
			return true
		}
	}
	return false
}

// confirmToolResult 窗口复述的工具结果：以模型拿到的内容补录。
// Python: messages.py _MessageProjection._confirm_tool_result。
func (p *messageProjection) confirmToolResult(message map[string]any) bool {
	callID := message["tool_call_id"]
	if callID == nil {
		return false
	}
	index, exists := p.toolResults[fmt.Sprint(callID)]
	if !exists {
		return false
	}
	delete(p.toolResults, fmt.Sprint(callID))
	// 模型拿到的是窗口的内容；span 保留工具名，失败的调用无内容时保留错误。
	recorded := p.messages[index]
	if content := message["content"]; content != nil {
		recorded["content"] = copyJSON(content)
	}
	return true
}

// rememberToolCallNames 记录 tool_call_id → name（供 tool 消息补名）。
// Python: messages.py _MessageProjection._remember_tool_call_names。
func (p *messageProjection) rememberToolCallNames(message map[string]any) {
	toolCalls, _ := message["tool_calls"].([]any)
	for _, item := range toolCalls {
		call, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		callID := ToolCallID(call)
		name := ToolCallName(call)
		if callID != nil && name != nil {
			p.toolCallNames[fmt.Sprint(callID)] = copyJSON(name)
		}
	}
}

// ProjectTrajectoryMessages 把轨迹投影为脱离的聊天消息及其缺陷。
// 每次模型请求贡献其发送的上下文窗口（由 context.window.commit 链重建）；
// 早前窗口已贡献的消息按 message_id 识别，不重复。请求随后贡献其
// completions，工具 span 贡献其结果。后窗口复述的 completion/工具结果只保留
// 一次：工具结果取模型拿到的内容，completion 保留模型产出的内容。
// 无提交窗口的请求按现状贡献其记录的 prompt 并报告
// missing_context_window；不猜测其消息中哪些早前请求已有。
// 压缩请求不贡献任何消息：其 prompt 关于对话而非属于对话。
// invoke_local=True（TTSE detect/induce）把缺窗口 prompt 或已提交窗口切片到
// 最后一条真实 user 消息（选切点时跳过 <system-reminder> 附件 user）。
// 同一投影中后续 span 不重追加该 prompt，只加 completions/tools。
// Python: messages.py project_trajectory_messages。
func ProjectTrajectoryMessages(trajectory *Trajectory, fields map[string]struct{}, invokeLocal bool) ([]map[string]any, []map[string]any, error) {
	selectedFields := fields
	if selectedFields == nil {
		selectedFields = DefaultEvolutionMessageFields
	}
	for field := range selectedFields {
		if _, selectable := messageFieldsSelectable[field]; !selectable {
			return nil, nil, fmt.Errorf("unknown trajectory message fields: %s", field)
		}
	}
	replay := ReplayWindows(trajectory)
	projection := newMessageProjection()
	// 间隙只说明主体的某事件未被捕获（如窗口外的 ask_user 记录在 agent span
	// 上）；后续提交仍可回放，不会使消息变错。
	for _, issue := range replay.Issues {
		if code, _ := issue["code"].(string); code != "v2.sequence_gap" {
			projection.issues = append(projection.issues, issue)
		}
	}
	spans := IterSpans(trajectory)
	sort.SliceStable(spans, func(i, j int) bool {
		return compareSpanSortKey(SpanSortKeyOf(spans[i]), SpanSortKeyOf(spans[j])) < 0
	})
	for _, span := range spans {
		category := SpanCategory(span)
		if category == "llm" {
			if IsCompactionSpan(span) {
				continue
			}
			rawPrompt, rawCompletions := ReadLLMExchange(span)
			window := WindowForInference(replay, span)
			if window == nil {
				projection.issues = append(projection.issues, map[string]any{
					"code":    "missing_context_window",
					"message": "model request has no committed context window",
					"span_id": fmt.Sprint(span["spanId"]),
				})
				prompt := make([]map[string]any, 0, len(rawPrompt))
				for _, message := range rawPrompt {
					prompt = append(prompt, normalizeMessage(message))
				}
				if invokeLocal {
					if len(projection.messages) == 0 {
						projection.addPrompt(trimPromptToLastUser(prompt))
					}
				} else {
					projection.addPrompt(prompt)
				}
			} else {
				if invokeLocal {
					window = trimPromptToLastUser(window)
				}
				projection.addWindow(window)
			}
			completions := make([]map[string]any, 0, len(rawCompletions))
			for _, message := range rawCompletions {
				completions = append(completions, normalizeMessage(message))
			}
			projection.addCompletions(completions)
			continue
		}
		if category == "tool" {
			projection.addToolResult(ReadToolCall(span))
		}
	}
	messages := make([]map[string]any, 0, len(projection.messages))
	for _, message := range projection.messages {
		messages = append(messages, selectFields(message, selectedFields))
	}
	return messages, projection.issues, nil
}

// TrajectoryToMessages 把规范 span 投影为脱离的 OpenAI 兼容消息。
// 见 ProjectTrajectoryMessages（它同时返回 issues）。
// Python: messages.py trajectory_to_messages。
func TrajectoryToMessages(trajectory *Trajectory, fields map[string]struct{}, invokeLocal bool) ([]map[string]any, error) {
	messages, _, err := ProjectTrajectoryMessages(trajectory, fields, invokeLocal)
	return messages, err
}
