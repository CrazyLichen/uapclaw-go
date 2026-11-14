package tool

// 客户端执行的工具（client-executed tools）。
//
// 客户端工具对模型而言与普通工具无异，但实际由连接的客户端执行
// （例如文档编辑器前端）。模型调用它且所有 before_tool_call rail 放行后，
// ability manager 以 client_tool 中断挂起本次调用并携带 ClientToolRequest；
// 宿主在客户端执行调用并以结果恢复（InteractiveInput.answer(interrupt_id,
// result)），该结果随后成为本次调用的普通工具输出并走 after_tool_call。
//
// 用 ToolCard(execution="client") 声明客户端工具，或继承 ClientTool
// 定制请求（build_request）与输出（complete）。
//
// Python: openjiuwen/core/foundation/tool/client.py（一比一迁移）
//
// 已登记偏差（2026-11-14，15.6）：Python ToolOutput 为 pydantic 模型，
// Go 侧工具输出统一为鸭子形态 map[string]any{"success","data","error"}
// （与 mcp_result.go 的 McpToolResult 先例一致），DefaultClientOutput /
// AsClientToolOutput / ClientToolUnavailableOutput 输出该形态。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ClientToolResult 宿主对挂起的客户端工具调用的应答。
// Python: client.py:62-127 ClientToolResult
type ClientToolResult struct {
	// Status 调用如何结束；completed 之外均为失败。
	// Python: status（默认 ClientToolStatus.COMPLETED）
	Status ClientToolStatus
	// Content 文本，或内容块：{"type":"text","text":...}，
	// 图片为 {"type":"image","data_url":"data:image/png;base64,..."} 或
	// {"type":"image","source":{"type":"base64","media_type":"image/png","data":...}}。
	// Python: content（str | ContentBlocks，默认 ""）
	Content any
	// IsError 客户端执行了调用但失败。
	// Python: is_error（默认 False）
	IsError bool
	// Data 给 rails 与宿主的结构化结果；不展示给模型。
	// Python: data（默认 None）
	Data any
}

// ClientToolContext 客户端工具调用的现场，传给 build_request 与 complete。
// Python: client.py:134-155 ClientToolContext
type ClientToolContext struct {
	// ToolCall 本次调用（含最终参数）。
	// Python: tool_call（ToolCall）
	ToolCall *llmschema.ToolCall
	// Session agent 会话（get_session_id()/get_state()/update_state()），
	// 例如查找绑定到会话的文档，或在两个钩子之间保存 per-call 状态。
	// Python: session（Optional[Session]）
	Session sessioninterfaces.SessionFacade
	// Agent 执行本次调用的 agent（agentcore.AgentInterface）。
	// Python: agent（BaseAgent，TYPE_CHECKING 前向引用）——Go 侧 single_agent/interfaces
	// 反向依赖本包，用 any 断环；消费方按具体接口断言。
	Agent any
	// CallbackContext 本次调用的 rail 回调上下文。
	// Python: callback_context（AgentCallbackContext）
	CallbackContext ToolCallbackContext
}

// ClientToolUnavailable 由 build_request 抛出以失败本次调用、不接触客户端。
// 例如：没有打开的文档、打开的文档无法执行该工具、输入非法。
// message 成为本次调用的工具错误。
// Python: client.py:158-167 ClientToolUnavailable
type ClientToolUnavailable struct {
	Message   string
	Retryable bool
}

func (e *ClientToolUnavailable) Error() string { return e.Message }

// ClientTool 客户端执行的工具基类。
// invoke 永远不会被 agent 调用。覆盖 buildRequestFn 告诉客户端做什么
// （作为 client_request 发送），覆盖 completeFn 校验或重塑客户端结果。
// Python: client.py:228-263 ClientTool
type ClientTool struct {
	card *ToolCard
	// buildRequestFn 返回客户端执行本次调用所需的内容（作为 client_request 发送）。
	// 返回 error 为 *ClientToolUnavailable 时以工具错误失败本次调用、不挂起等客户端。
	// Python: build_request（可覆盖，默认返回 {}）
	buildRequestFn func(arguments map[string]any, ctx *ClientToolContext) (map[string]any, error)
	// completeFn 把客户端结果变成本次调用的工具输出。
	// 返回 map[string]any（ToolOutput 鸭子形态），或 string 替换默认输出的文本。
	// Python: complete（可覆盖，默认 default_client_output）
	completeFn func(arguments map[string]any, result *ClientToolResult, ctx *ClientToolContext) (any, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ClientToolStatus 宿主报告的客户端工具调用结局。
// Python: client.py:39-49 ClientToolStatus(str, Enum)
type ClientToolStatus string

const (
	// ClientToolStatusCompleted 完成。
	// Python: ClientToolStatus.COMPLETED = "completed"
	ClientToolStatusCompleted ClientToolStatus = "completed"
	// ClientToolStatusError 错误。
	// Python: ClientToolStatus.ERROR = "error"
	ClientToolStatusError ClientToolStatus = "error"
	// ClientToolStatusTimeout 宿主放弃等待客户端。
	// Python: ClientToolStatus.TIMEOUT = "timeout"
	ClientToolStatusTimeout ClientToolStatus = "timeout"
	// ClientToolStatusClientGone 执行工具的客户端已断连。
	// Python: ClientToolStatus.CLIENT_GONE = "client_gone"
	ClientToolStatusClientGone ClientToolStatus = "client_gone"
	// ClientToolStatusCancelled 调用在客户端返回结果之前被取消。
	// Python: ClientToolStatus.CANCELLED = "cancelled"
	ClientToolStatusCancelled ClientToolStatus = "cancelled"
)

// ──────────────────────────── 常量 ────────────────────────────

// blockTypes 识别为单个内容块的字段类型集合。
// Python: client.py:130 _BLOCK_TYPES
const blockTypes = "text,image,image_url"

// envelopeKeys ClientToolResult 信封字段集合。
// Python: client.py:131 _ENVELOPE_KEYS
const envelopeKeys = "status,content,is_error,data"

// ──────────────────────────── 全局变量 ────────────────────────────

// RetryableClientToolStatuses 模型可以直接重试调用的结局集合。
// Python: client.py:53-57 RETRYABLE_CLIENT_TOOL_STATUSES
var RetryableClientToolStatuses = map[ClientToolStatus]bool{
	ClientToolStatusTimeout:    true,
	ClientToolStatusClientGone: true,
	ClientToolStatusCancelled:  true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewClientToolResult 构造 ClientToolResult，按 Python field_validator
// 归一 content（client.py:80-90 _normalize_content）：
// None→""；dict 为单个内容块（type 命中）否则 JSON 文本；
// list 非全 dict → JSON 文本；其余原样。
func NewClientToolResult(status ClientToolStatus, content any, isError bool, data any) *ClientToolResult {
	return &ClientToolResult{
		Status:  status,
		Content: normalizeClientContent(content),
		IsError: isError,
		Data:    data,
	}
}

// Failed 是否未成功：错误结果或 completed 以外的任何状态。
// Python: client.py:92-95 ClientToolResult.failed
func (r *ClientToolResult) Failed() bool {
	return r.IsError || r.Status != ClientToolStatusCompleted
}

// CoerceClientToolResult 从宿主给出的任何应答构造结果（client.py:98-127）：
//   - *ClientToolResult 原样使用；
//   - nil / string / list → 仅 content；
//   - 非 dict → JSON 文本为 content 且保留 data；
//   - dict 键全部落在信封集合内 → 校验为信封（校验失败落到下一分支）；
//   - 其他 dict → 整体即结果：模型看到 JSON，data 保留原值；
//     仅当 is_error 为真或 status 是 completed 以外的状态才算失败。
func CoerceClientToolResult(value any) *ClientToolResult {
	if result, ok := value.(*ClientToolResult); ok {
		return result
	}
	if value == nil {
		return &ClientToolResult{Status: ClientToolStatusCompleted, Content: ""}
	}
	switch v := value.(type) {
	case string, []map[string]any:
		return &ClientToolResult{Status: ClientToolStatusCompleted, Content: normalizeClientContent(v)}
	case map[string]any:
		if isClientEnvelope(v) {
			if result, ok := clientResultFromEnvelope(v); ok {
				return result
			}
		}
		status := clientStatusOf(v["status"])
		return &ClientToolResult{
			Status:  status,
			IsError: v["is_error"] == true,
			Content: jsonText(v),
			Data:    v,
		}
	default:
		return &ClientToolResult{Status: ClientToolStatusCompleted, Content: jsonText(value), Data: value}
	}
}

// ClientToolUnavailableOutput build_request 拒绝的调用的工具输出。
// Python: client.py:171-177 client_tool_unavailable_output
func ClientToolUnavailableOutput(err *ClientToolUnavailable) map[string]any {
	return map[string]any{
		"success": false,
		"error":   err.Message,
		"data":    map[string]any{"status": "unavailable", "retryable": err.Retryable},
	}
}

// AsClientToolOutput 保证客户端调用以工具输出收尾。
// completeFn 可能返回纯文本或其他值；此时替换默认输出的文本，
// 保留 result 的图片、status 与重试提示。
// Python: client.py:180-195 as_client_tool_output
func AsClientToolOutput(output any, result *ClientToolResult, toolName string) map[string]any {
	if m, ok := output.(map[string]any); ok {
		return m
	}
	base := DefaultClientOutput(result, toolName)
	text := ""
	if s, ok := output.(string); ok {
		text = s
	} else {
		text = jsonText(output)
	}
	data, _ := base["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	data["content"] = text
	if success, _ := base["success"].(bool); success {
		base["data"] = data
		return base
	}
	if text != "" {
		base["error"] = text
	}
	base["data"] = data
	return base
}

// DefaultClientOutput 把客户端结果变成工具输出（鸭子形态 map）。
// 文本进 data["content"]（模型读取的内容）；图片进 data["multimodal"]，
// agent 在工具消息之后转发给模型。失败的调用是普通的失败结果
// （success=false），data["status"] 与 data["retryable"] 供 rails 与宿主读取。
// Python: client.py:203-225 default_client_output
func DefaultClientOutput(result *ClientToolResult, toolName string) map[string]any {
	text, images := splitClientContent(result.Content)
	data := map[string]any{"content": text}
	if len(images) > 0 {
		label := "client tool result"
		if toolName != "" {
			label = fmt.Sprintf("%s result", toolName)
		}
		multimodal := make([]map[string]any, 0, len(images))
		for _, url := range images {
			multimodal = append(multimodal, map[string]any{
				"type": "image", "data_url": url, "source": "client", "source_path": label,
			})
		}
		data["multimodal"] = multimodal
	}
	if result.Data != nil {
		data["result"] = result.Data
	}
	if !result.Failed() {
		return map[string]any{"success": true, "data": data}
	}
	status := result.Status
	if status == ClientToolStatusCompleted {
		status = ClientToolStatusError
	}
	data["status"] = string(status)
	data["retryable"] = RetryableClientToolStatuses[status]
	return map[string]any{
		"success": false,
		"error":   failureText(status, toolNameOr(toolName, "client tool"), text),
		"data":    data,
	}
}

// NewClientTool 构造客户端工具基类；卡片缺省改写为 execution="client"
// （标记副本，不动调用方的卡片）。buildRequestFn/completeFn 为 nil 时
// 使用 Python 默认实现（build_request 返回 {}，complete 走默认输出）。
// Python: client.py:237-241 ClientTool.__init__
func NewClientTool(card *ToolCard,
	buildRequestFn func(arguments map[string]any, ctx *ClientToolContext) (map[string]any, error),
	completeFn func(arguments map[string]any, result *ClientToolResult, ctx *ClientToolContext) (any, error),
) *ClientTool {
	if card.Execution != ToolExecutionClient {
		copied := *card
		copied.Execution = ToolExecutionClient
		card = &copied
	}
	return &ClientTool{card: card, buildRequestFn: buildRequestFn, completeFn: completeFn}
}

// BuildRequest 返回客户端执行本次调用所需的内容（作为 client_request 发送）。
// Python: client.py:243-249 ClientTool.build_request
func (t *ClientTool) BuildRequest(arguments map[string]any, ctx *ClientToolContext) (map[string]any, error) {
	if t.buildRequestFn == nil {
		return map[string]any{}, nil
	}
	return t.buildRequestFn(arguments, ctx)
}

// Complete 把客户端的结果变成本次调用的工具输出。
// 返回工具输出（鸭子形态 map），或替换默认输出文本的字符串。
// Python: client.py:251-257 ClientTool.complete
func (t *ClientTool) Complete(arguments map[string]any, result *ClientToolResult, ctx *ClientToolContext) (any, error) {
	if t.completeFn == nil {
		return DefaultClientOutput(result, t.card.Name), nil
	}
	return t.completeFn(arguments, result, ctx)
}

// Card 返回工具配置卡片。
func (t *ClientTool) Card() *ToolCard { return t.card }

// Invoke 客户端工具不可由 agent 调用。
// Python: client.py:259-260 ClientTool.invoke
func (t *ClientTool) Invoke(_ context.Context, _ map[string]any, _ ...ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("'%s' is a client tool; the connected client executes it", t.card.Name)
}

// Stream 流式执行的返回块。
// Python: client.py:262-263 ClientTool.stream
func (t *ClientTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	result, err := t.Invoke(ctx, inputs, opts...)
	if err != nil {
		ch <- StreamChunk{Error: err}
	} else {
		ch <- StreamChunk{Data: result}
	}
	close(ch)
	return ch, nil
}

// GetCallTimeout 沿用卡片级超时策略（Python 基类未覆盖）。
func (t *ClientTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 { return defaultTimeout }

// SessionID 会话 id，无会话时为空串。
// Python: client.py:152-155 ClientToolContext.session_id
func (c *ClientToolContext) SessionID() string {
	if c.Session == nil {
		return ""
	}
	return c.Session.GetSessionID()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeClientContent 归一 content 字段。
// Python: client.py:80-90 ClientToolResult._normalize_content
func normalizeClientContent(value any) any {
	if value == nil {
		return ""
	}
	if m, ok := value.(map[string]any); ok {
		if t, ok := m["type"].(string); ok && blockTypesContains(t) {
			return []map[string]any{m}
		}
		return jsonText(m)
	}
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if _, ok := item.(map[string]any); !ok {
				return jsonText(value)
			}
		}
		return value
	}
	if _, ok := value.([]map[string]any); ok {
		return value
	}
	return value
}

// isClientEnvelope dict 的键是否全部落在信封集合内。
// Python: client.py:116 set(value) <= _ENVELOPE_KEYS
func isClientEnvelope(m map[string]any) bool {
	for key := range m {
		switch key {
		case "status", "content", "is_error", "data":
		default:
			return false
		}
	}
	return true
}

// clientResultFromEnvelope 校验并构造信封形态的结果；
// 非法信封返回 false（调用方落到"其他 dict"分支）。
// Python: client.py:118-120 cls.model_validate(value) / except ValidationError
func clientResultFromEnvelope(m map[string]any) (*ClientToolResult, bool) {
	statusStr, _ := m["status"].(string)
	status := ClientToolStatus(statusStr)
	switch status {
	case ClientToolStatusCompleted, ClientToolStatusError, ClientToolStatusTimeout,
		ClientToolStatusClientGone, ClientToolStatusCancelled:
	default:
		if statusStr != "" {
			return nil, false
		}
		status = ClientToolStatusCompleted
	}
	isError, ok := m["is_error"].(bool)
	if m["is_error"] != nil && !ok {
		return nil, false
	}
	content := normalizeClientContent(m["content"])
	if _, isStr := content.(string); !isStr {
		if _, isBlocks := content.([]map[string]any); !isBlocks {
			if _, isAnyList := content.([]any); !isAnyList {
				return nil, false
			}
		}
	}
	return &ClientToolResult{
		Status:  status,
		Content: content,
		IsError: isError,
		Data:    m["data"],
	}, true
}

// clientStatusOf 探测 dict["status"] 对应的枚举；无匹配返回 completed。
// Python: client.py:121 next((s for s in ClientToolStatus if s == value.get("status")), None)
// 的"None → COMPLETED"兜底。
func clientStatusOf(value any) ClientToolStatus {
	if s, ok := value.(string); ok {
		if _, valid := map[ClientToolStatus]bool{
			ClientToolStatusCompleted: true, ClientToolStatusError: true,
			ClientToolStatusTimeout: true, ClientToolStatusClientGone: true,
			ClientToolStatusCancelled: true,
		}[ClientToolStatus(s)]; valid {
			return ClientToolStatus(s)
		}
	}
	return ClientToolStatusCompleted
}

// blockTypesContains type 是否命中内容块类型集合。
// Python: client.py:87 value.get("type") in _BLOCK_TYPES
func blockTypesContains(t string) bool {
	for _, name := range strings.Split(blockTypes, ",") {
		if name == t {
			return true
		}
	}
	return false
}

// jsonText JSON 文本化（禁转义 HTML，保留非 ASCII；default=str 等价 %v 兜底）。
// Python: client.py:266-267 _json_text
func jsonText(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

// splitClientContent 拆出文本与图片 data_url。
// Python: client.py:270-287 _split_content
func splitClientContent(content any) (string, []string) {
	if text, ok := content.(string); ok {
		return text, nil
	}
	var texts, images []string
	appendBlocks := func(blocks []map[string]any) {
		for _, block := range blocks {
			appendBlock(block, &texts, &images)
		}
	}
	switch blocks := content.(type) {
	case []map[string]any:
		appendBlocks(blocks)
	case []any:
		for _, item := range blocks {
			if block, ok := item.(map[string]any); ok {
				appendBlock(block, &texts, &images)
			}
		}
	}
	var kept []string
	for _, text := range texts {
		if text != "" {
			kept = append(kept, text)
		}
	}
	return strings.Join(kept, "\n"), images
}

// appendBlock 处理单个内容块。
// Python: client.py:275-286 _split_content 循环体
func appendBlock(block map[string]any, texts *[]string, images *[]string) {
	blockType, _ := block["type"].(string)
	switch blockType {
	case "text":
		text, _ := block["text"].(string)
		*texts = append(*texts, text)
	case "image", "image_url":
		if url := imageDataURL(block); url != "" {
			*images = append(*images, url)
		} else {
			*texts = append(*texts, "[image omitted: only base64 data images are supported]")
		}
	default:
		*texts = append(*texts, jsonText(block))
	}
}

// imageDataURL 提取块的 base64 图片 data URL；无候选返回空串。
// Python: client.py:290-302 _image_data_url
func imageDataURL(block map[string]any) string {
	imageURL, _ := block["image_url"].(map[string]any)
	var urlValue string
	if imageURL != nil {
		urlValue, _ = imageURL["url"].(string)
	}
	for _, candidate := range []any{block["data_url"], urlValue} {
		if s, ok := candidate.(string); ok && strings.HasPrefix(s, "data:image/") {
			return s
		}
	}
	source, _ := block["source"].(map[string]any)
	if source != nil {
		sourceType, _ := source["type"].(string)
		data, _ := source["data"].(string)
		mediaType, _ := source["media_type"].(string)
		if sourceType == "base64" && data != "" && strings.HasPrefix(mediaType, "image/") {
			return fmt.Sprintf("data:%s;base64,%s", mediaType, data)
		}
	}
	return ""
}

// failureText 失败结果的错误文案；TIMEOUT/CLIENT_GONE/CANCELLED 各有
// "结果可能已生效"的提示，其余回落文本或无错误信息兜底。
// Python: client.py:305-323 _failure_text
func failureText(status ClientToolStatus, toolName string, text string) string {
	var message string
	switch status {
	case ClientToolStatusTimeout:
		message = fmt.Sprintf(
			"Client tool '%s' did not return a result in time. It may or may not have taken "+
				"effect; check the current state before calling it again.", toolName)
	case ClientToolStatusClientGone:
		message = fmt.Sprintf(
			"Client tool '%s' could not finish: the client that runs it disconnected. It may or "+
				"may not have taken effect; check the current state, then call it again once the client is back.", toolName)
	case ClientToolStatusCancelled:
		message = fmt.Sprintf(
			"Client tool '%s' was cancelled before the client returned a result. It may or may not "+
				"have taken effect; check the current state before calling it again.", toolName)
	default:
		if text != "" {
			return text
		}
		return fmt.Sprintf("Client tool '%s' failed without an error message.", toolName)
	}
	if text != "" {
		return fmt.Sprintf("%s\n%s", message, text)
	}
	return message
}

// toolNameOr 空名兜底（Python default_client_output 的 tool_name or "client tool"）。
func toolNameOr(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}
