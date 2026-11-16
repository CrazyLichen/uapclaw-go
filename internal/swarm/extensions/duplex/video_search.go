package duplex

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex/tasks"
)

// ──────────────────────────── 常量 ────────────────────────────

// VideoToolChannelID 双工任务专用渠道标识。
// 对齐 video_search.py:19 VIDEO_TOOL_CHANNEL_ID。
const VideoToolChannelID = "video_tool"

// imageFilenameSuffixes 图片 MIME → 文件后缀。
// 对齐 video_search.py:21-25 _IMAGE_FILENAME_SUFFIXES。
var imageFilenameSuffixes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// 委派上下文与回执限制。
// 对齐 video_search.py:26-30。
const (
	MaxDelegationContextItems    = 6
	MaxDelegationContextChars    = 8_000
	MaxDelegationResultChars     = 2_400
	MaxRealtimeBriefChars        = 180
	fallbackBriefDerivedMaxChars = 100
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentRequest e2a 归一化请求的最小形状。
// 对齐 video_search.py:399-412 e2a_from_agent_fields 入参形态。
type AgentRequest struct {
	// RequestID 请求标识
	RequestID string
	// ChannelID 渠道标识
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// UserID 用户标识（空表匿名）
	UserID string
	// ReqMethod 请求方法（如 chat.send）
	ReqMethod string
	// Params 请求参数
	Params map[string]any
	// IsStream 是否流式
	IsStream bool
	// Timestamp 发起时间
	Timestamp time.Time
}

// AgentResponse e2a 响应的最小形状。
// 对齐 video_search.py:415-422 的 response.ok/payload。
type AgentResponse struct {
	// OK 是否成功
	OK bool
	// Payload 响应载荷
	Payload map[string]any
}

// AgentClient AgentServer 客户端窄接口（依赖倒置允许场景）。
// 偏差 O-3：Python 鸭子类型 client（send_request/send_request_stream），
// Go 定义窄接口，由网关路由面注入。
type AgentClient interface {
	// SendRequest 单次请求
	SendRequest(ctx any, env *AgentRequest) *AgentResponse
	// SendRequestStream 流式请求；返回 nil 通道表示客户端不支持流式
	// （对齐 video_search.py:414 getattr(client,"send_request_stream",None) 的能力探测）
	SendRequestStream(ctx any, env *AgentRequest) <-chan *AgentResponse
}

// CoreAgentResult execute_core_agent 的返回结果。
// 对齐 video_search.py:426-435/:531-539 的返回 dict 合集。
type CoreAgentResult struct {
	// FinalPayload 末帧载荷（流式）或响应载荷（非流式）
	FinalPayload map[string]any
	// DisplayResult 展示用权威内容
	DisplayResult string
	// RealtimeBrief 口播简报结构
	RealtimeBrief map[string]any
	// Answer 等于 DisplayResult
	Answer string
	// RawAnswerChars 原始答案字符数
	RawAnswerChars int
	// ToolsUsed 使用的工具名（保序）
	ToolsUsed []string
	// Files 非流式分支的载荷附加（对齐 **payload 展开）
	Files []map[string]any
}

// CoreAgentOptions execute_core_agent 的关键字参数。
// 对齐 video_search.py:367-380 的参数表。
type CoreAgentOptions struct {
	// Question 用户任务要求
	Question string
	// Query 实时旁证上下文
	Query string
	// VisualContext 实时视觉线索
	VisualContext string
	// SearchSessionID 搜索会话标识
	SearchSessionID string
	// CoreSessionID 核心会话标识
	CoreSessionID string
	// RequestID 请求标识
	RequestID string
	// UserID 用户标识
	UserID string
	// DelegationContext 历史委派上下文
	DelegationContext []map[string]any
	// FrameDataURL 画面帧 dataURL
	FrameDataURL string
	// NormalizeMediaAttachments 附件归一化服务（23.8 链注入）
	NormalizeMediaAttachments func(params map[string]any, coreSessionID string) error
	// OnProgress 进度回调
	OnProgress func(map[string]any) error
	// InteractionAnswer 交互答案回填输入（非 nil 时走 answer 请求方法）
	InteractionAnswer map[string]any
	// BriefNonce 回执协议随机串
	BriefNonce string
	// ManagedTaskBinding 任务绑定
	ManagedTaskBinding map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// FullAnswerLanguageRule 可见完整答案的语言规则。
// 对齐 video_search.py:39-48 full_answer_language_rule。
func FullAnswerLanguageRule() string {
	if ToolLanguage() == "en" {
		return "The complete answer before the markers must be written in English. " +
			"Do not write that answer in Chinese. "
	}
	return "标记之前的完整答案必须使用简体中文。"
}

// CoreAgentBriefProtocol 回执协议段。
// 对齐 video_search.py:50-58 core_agent_brief_protocol。
func CoreAgentBriefProtocol(nonce string) string {
	begin, end := BriefMarkers(nonce)
	languageRule := JoyAIResponseLanguageInstruction(ToolLanguage())
	return "\n\n" + FullAnswerLanguageRule() + languageRule +
		tasks.BuildBriefPrompt(begin, end, languageRule)
}

// PresentCoreAgentResult 把 Core Agent 输出拆分为权威 UI 内容与安全口播简报。
// 对齐 video_search.py:119-156 present_core_agent_result。
func PresentCoreAgentResult(rawAnswer, nonce, question string, toolsUsed []string) map[string]any {
	begin, end := BriefMarkers(nonce)
	beginEscaped := regexp.QuoteMeta(begin)
	endEscaped := regexp.QuoteMeta(end)
	pattern := regexp.MustCompile(`(?s)(?:\r?\n)*` + beginEscaped + `\s*(.*?)\s*` + endEscaped + `(?:\r?\n)*`)
	match := pattern.FindStringSubmatchIndex(rawAnswer)
	displayResult := strings.TrimSpace(pattern.ReplaceAllString(rawAnswer, "\n"))
	if displayResult == "" {
		displayResult = strings.TrimSpace(rawAnswer)
	}
	kind := resultKind(question, displayResult, toolsUsed)
	modelBrief := ""
	if match != nil {
		modelBrief = safeBrief(rawAnswer[match[2]:match[3]])
	}
	summary, source := "", ""
	if modelBrief != "" {
		summary, source = modelBrief, "core_agent"
	} else {
		summary, source = fallbackRealtimeBrief(displayResult, kind)
	}
	return map[string]any{
		"display_result": displayResult,
		"realtime_brief": map[string]any{
			"status": "completed", "result_kind": kind, "summary": summary,
			"displayed_in_ui": true, "response_mode": "brief", "source": source,
		},
	}
}

// CoreAgentText 归一化文本并按上限截断（rune 语义）。
// 对齐 video_search.py:203-217 core_agent_text。
func CoreAgentText(value any, limit int) string {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			text = fmt.Sprintf("%v", v)
		} else {
			text = string(data)
		}
	}
	normalized := strings.TrimSpace(collapseWhitespace(text))
	runes := []rune(normalized)
	if len(runes) <= limit {
		return normalized
	}
	return strings.TrimRight(string(runes[:limit]), "") + "..."
}

// CoreAgentProgress 把 AgentServer 事件映射为任务进度条目；无法映射返回 nil。
// 对齐 video_search.py:219-364 core_agent_progress。
func CoreAgentProgress(payload map[string]any) map[string]any {
	eventType := strings.TrimSpace(toString(payload["event_type"]))
	if eventType == "chat.file" || eventType == "chat.final" {
		files := NormalizeFileItems(payload["files"])
		if len(files) > 0 {
			return map[string]any{"stage": "file", "title": "文件已返回", "status": "completed", "files": files}
		}
	}
	if eventType == "chat.reasoning" {
		content := toString(payload["content"])
		if content == "" {
			return nil
		}
		return map[string]any{"stage": "reasoning", "title": "正在分析问题", "status": "running", "content": content}
	}
	if eventType == "chat.tool_call" {
		tool, ok := payload["tool_call"].(map[string]any)
		if !ok {
			tool = payload
		}
		name := strings.TrimSpace(toString(orChain(tool["display_name"], tool["name"], payload["tool_name"], "工具")))
		detail := CoreAgentText(orNil(tool["formatted_args"], tool["arguments"]), 280)
		return map[string]any{
			"stage": "tool_call", "title": "调用工具：" + name, "detail": detail, "status": "running",
			"tool_call_id":      toString(orChain(tool["id"], tool["tool_call_id"], payload["tool_call_id"], "")),
			"tool_name":         strings.TrimSpace(toString(orChain(tool["name"], payload["tool_name"], "unknown"))),
			"tool_arguments":    orFallbackMap(tool["arguments"]),
			"tool_description":  strings.TrimSpace(toString(tool["description"])),
			"tool_formatted_args": strings.TrimSpace(toString(tool["formatted_args"])),
			"tool_display_name":   strings.TrimSpace(toString(tool["display_name"])),
		}
	}
	if eventType == "chat.tool_update" {
		update, ok := payload["tool_update"].(map[string]any)
		if !ok {
			update = payload
		}
		name := strings.TrimSpace(toString(orChain(update["tool_name"], update["name"], "工具")))
		detail := CoreAgentText(orNil(update["beam_search"], update["progress"]), 280)
		return map[string]any{
			"stage": "tool_update", "title": name + " 正在执行", "detail": detail, "status": "running",
			"tool_call_id": toString(update["tool_call_id"]), "tool_name": name,
		}
	}
	if eventType == "chat.tool_result" {
		result, ok := payload["tool_result"].(map[string]any)
		if !ok {
			result = payload
		}
		name := strings.TrimSpace(toString(orChain(result["tool_name"], result["name"], "工具")))
		rawStatus := strings.ToLower(strings.TrimSpace(toString(result["status"])))
		failed := result["success"] == false || failedStatuses[rawStatus]
		detail := CoreAgentText(orNil(result["summary"], result["error"], result["result"]), 280)
		rawResult := firstNotNil(result["result"], result["raw_output"], result["data"], result["error"], result["summary"])
		title := name + "执行完成"
		status := "completed"
		if failed {
			title = name + "执行失败"
			status = "failed"
		}
		return map[string]any{
			"stage": "tool_result", "title": title, "detail": detail, "status": status,
			"tool_call_id": toString(orChain(result["tool_call_id"], payload["tool_call_id"], "")),
			"tool_name":    name,
			"tool_result":  rawResult,
			"tool_summary": strings.TrimSpace(toString(result["summary"])),
			"tool_success": !failed,
		}
	}
	if eventType == "todo.updated" {
		todos, ok := payload["todos"].([]any)
		if !ok || len(todos) == 0 {
			return nil
		}
		completed := 0
		todoItems := []map[string]any{}
		for index, item := range todos {
			todo, isMap := item.(map[string]any)
			if !isMap {
				continue
			}
			status := strings.ToLower(toString(todo["status"]))
			if status == "completed" {
				completed++
			}
			todoItems = append(todoItems, map[string]any{
				"id":      toString(orChain(todo["id"], fmt.Sprintf("%d", index), "")),
				"content": toString(orChain(todo["content"], todo["activeForm"], "")),
				"status":  toString(orChain(todo["status"], "pending", "")),
			})
		}
		return map[string]any{
			"stage": "plan", "title": "执行计划已更新",
			"detail": fmt.Sprintf("%d/%d 项已完成", completed, len(todos)),
			"status": "running", "todos": todoItems,
		}
	}
	if eventType == "chat.delta" && strings.TrimSpace(toString(payload["content"])) != "" {
		return map[string]any{"stage": "answer", "title": "正在整理执行结果", "status": "running"}
	}
	if eventType == "chat.error" {
		return map[string]any{
			"stage": "error", "title": "Core Agent 执行失败", "status": "failed",
			"detail": CoreAgentText(orNil(payload["error"], payload["content"]), 280),
		}
	}
	return nil
}

// ExecuteCoreAgent 经标准 Core Agent API 执行一次委派视频任务。
// 对齐 video_search.py:366-540 execute_core_agent。
// 流式分支：chat.ask_user_question → 交互挂起；chat.error → 失败；
// chat.delta 聚合、chat.final 收末帧；无末帧 → ExecutionUncertain。
func ExecuteCoreAgent(ctx any, client AgentClient, opts CoreAgentOptions) *CoreAgentResult {
	requestID := opts.RequestID
	if requestID == "" {
		requestID = "video-core-" + newRequestID()
	}
	briefNonce := opts.BriefNonce
	if briefNonce == "" {
		briefNonce = newRequestID()
	}
	coreSessionID := opts.CoreSessionID
	if coreSessionID == "" {
		coreSessionID = "video-tool-" + newRequestID()
	}
	contextText := delegationContextText(opts.DelegationContext)
	prompt := tasks.BuildExecutionPrompt(opts.Question, opts.Query, opts.VisualContext, contextText, CoreAgentBriefProtocol(briefNonce))
	params := map[string]any{
		"query":                     prompt,
		"content":                   prompt,
		"mode":                      "agent",
		"work_mode":                 "work",
		"source":                    "video_tool",
		"log_as_user":               false,
		"supports_user_interaction": true,
		"video_question":            opts.Question,
		"video_query":               opts.Query,
		"video_visual_context":      opts.VisualContext,
		"search_session_id":         opts.SearchSessionID,
		"video_core_session_id":     coreSessionID,
		"video_delegation_context":  orEmptyList(opts.DelegationContext),
	}
	reqMethod := "chat.send"
	if mediaItem := frameMediaItem(opts.FrameDataURL); mediaItem != nil {
		params["media_items"] = []map[string]any{mediaItem}
		if opts.NormalizeMediaAttachments == nil {
			panicOrError(errorsNew("Core media attachment service is unavailable"))
		}
		if err := opts.NormalizeMediaAttachments(params, coreSessionID); err != nil {
			panicOrError(err)
		}
	}
	if opts.InteractionAnswer != nil {
		answerParams, _ := opts.InteractionAnswer["params"].(map[string]any)
		merged := map[string]any{}
		for k, v := range answerParams {
			merged[k] = v
		}
		merged["log_as_user"] = false
		params = merged
		if m, ok := opts.InteractionAnswer["req_method"].(string); ok && m != "" {
			reqMethod = m
		}
	}
	if opts.ManagedTaskBinding != nil {
		params["managed_task_binding"] = opts.ManagedTaskBinding
	}
	env := &AgentRequest{
		RequestID: requestID, ChannelID: VideoToolChannelID, UserID: opts.UserID,
		SessionID: coreSessionID, ReqMethod: reqMethod, Params: params,
		IsStream: false, Timestamp: time.Now(),
	}

	// 非流式分支（send_request_stream 不可用，对齐 :414-432）
	stream := client.SendRequestStream(ctx, env)
	if stream == nil {
		response := client.SendRequest(ctx, env)
		payload := map[string]any{}
		if response != nil && response.Payload != nil {
			payload = response.Payload
		}
		if response == nil || !response.OK {
			errText := toString(orChain(payload["error"], "Jiuwen Core Agent failed"))
			panicOrError(errorsNew(toString(errText)))
		}
		finalPayload := copyMap(payload)
		finalPayload["event_type"] = "chat.final"
		fileProgress := CoreAgentProgress(finalPayload)
		if fileProgress != nil && opts.OnProgress != nil {
			_ = opts.OnProgress(fileProgress)
		}
		rawAnswer := strings.TrimSpace(toString(orChain(payload["content"], payload["answer"], "")))
		if rawAnswer == "" && fileProgress != nil {
			rawAnswer = "文件已返回，请查看产物。"
		}
		if rawAnswer == "" {
			panicOrError(errorsNew("Jiuwen Core Agent returned empty output"))
		}
		presented := PresentCoreAgentResult(rawAnswer, briefNonce, orOther(opts.Question, opts.Query), []string{})
		return &CoreAgentResult{
			FinalPayload: payload,
			DisplayResult: toString(presented["display_result"]),
			RealtimeBrief: presented["realtime_brief"].(map[string]any),
			Answer:        toString(presented["display_result"]),
			RawAnswerChars: len([]rune(rawAnswer)),
			ToolsUsed:     []string{},
		}
	}

	// 流式分支（对齐 :434-539）
	finalPayload := map[string]any{}
	deltaParts := []string{}
	toolsUsed := []string{}
	emittedOnce := map[string]bool{}
	receivedFiles := false
	var pendingInteraction map[string]any
	for chunk := range stream {
		payload := map[string]any{}
		if chunk != nil && chunk.Payload != nil {
			payload = chunk.Payload
		}
		eventType := strings.TrimSpace(toString(payload["event_type"]))
		if eventType == "chat.ask_user_question" {
			source, _ := payload["source"].(string)
			var interaction map[string]any
			var err error
			if tasks.NATIVEApprovalSources[source] {
				interaction, err = tasks.NativeApprovalQuestion(payload)
			} else {
				interaction, err = tasks.InformationQuestion(payload)
			}
			if err != nil {
				panicOrError(&tasks.ExecutionUncertain{Msg: err.Error()})
			}
			pendingInteraction = interaction
			if opts.OnProgress != nil {
				questions, _ := interaction["questions"].([]any)
				questionLines := []string{}
				for _, q := range questions {
					qm, _ := q.(map[string]any)
					questionLines = append(questionLines, toString(qm["question"]))
				}
				_ = opts.OnProgress(map[string]any{
					"stage": "interaction", "title": "等待用户回答", "status": "running",
					"interaction": interaction, "detail": strings.Join(questionLines, "\n"),
				})
			}
			continue
		}
		if eventType == "chat.error" {
			panicOrError(errorsNew(toString(orChain(payload["error"], payload["content"], "Jiuwen Core Agent failed"))))
		}
		content := toString(payload["content"])
		if eventType == "chat.delta" && content != "" {
			deltaParts = append(deltaParts, content)
		} else if eventType == "chat.final" {
			finalPayload = payload
		}
		progress := CoreAgentProgress(payload)
		if progress == nil {
			continue
		}
		stage := toString(progress["stage"])
		receivedFiles = receivedFiles || stage == "file"
		toolKey := toString(progress["tool_call_id"])
		dedupeKey := stage
		if toolKey != "" {
			dedupeKey = stage + ":" + toolKey
		}
		// reasoning 以增量流式下发；去重历史缺陷见 :510 注释
		if (stage == "answer" || stage == "plan") && emittedOnce[dedupeKey] {
			continue
		}
		emittedOnce[dedupeKey] = true
		toolName := strings.TrimSpace(toString(progress["tool_name"]))
		if toolName != "" && !containsString(toolsUsed, toolName) {
			toolsUsed = append(toolsUsed, toolName)
		}
		if opts.OnProgress != nil {
			_ = opts.OnProgress(progress)
		}
	}
	if pendingInteraction != nil {
		source, _ := pendingInteraction["source"].(string)
		if map[string]bool{"ask_user_interrupt": true, "permission_interrupt": true, "confirm_interrupt": true}[source] {
			panicOrError(&tasks.InteractionPending{})
		}
	}
	if len(finalPayload) == 0 {
		panicOrError(&tasks.ExecutionUncertain{Msg: "Agent stream ended without a final result; execution is unknown"})
	}
	rawAnswer := strings.TrimSpace(toString(finalPayload["content"]))
	if rawAnswer == "" {
		rawAnswer = strings.TrimSpace(strings.Join(deltaParts, ""))
	}
	if rawAnswer == "" && receivedFiles {
		rawAnswer = "文件已返回，请查看产物。"
	}
	if rawAnswer == "" {
		panicOrError(errorsNew("Jiuwen Core Agent returned empty output"))
	}
	presented := PresentCoreAgentResult(rawAnswer, briefNonce, orOther(opts.Question, opts.Query), toolsUsed)
	return &CoreAgentResult{
		FinalPayload:   finalPayload,
		DisplayResult:  toString(presented["display_result"]),
		RealtimeBrief:  presented["realtime_brief"].(map[string]any),
		Answer:         toString(presented["display_result"]),
		RawAnswerChars: len([]rune(rawAnswer)),
		ToolsUsed:      toolsUsed,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// BriefMarkers 生成回执协议标记对。
// 对齐 video_search.py:32-37 _brief_markers。
func BriefMarkers(nonce string) (string, string) {
	return fmt.Sprintf("[[JIUWEN_BRIEF_BEGIN:%s]]", nonce), fmt.Sprintf("[[JIUWEN_BRIEF_END:%s]]", nonce)
}

// resultKind 按工具与文本特征分型结果。
// 对齐 video_search.py:60-77 _result_kind。
func resultKind(question, answer string, toolsUsed []string) string {
	combinedTools := strings.ToLower(strings.Join(toolsUsed, " "))
	combinedText := strings.ToLower(question + "\n" + answer)
	if regexp.MustCompile(`search|fetch|browser|网页|搜索`).MatchString(combinedTools) {
		return "research"
	}
	if strings.Contains(answer, "```") || regexp.MustCompile(`\b(?:python|typescript|javascript|java|c\+\+|sql)\b|代码|程序|函数`).MatchString(combinedText) {
		return "code"
	}
	if regexp.MustCompile(`计算|求解|等于|方程|积分|概率|\d+\s*[-+*/^]\s*\d+`).MatchString(combinedText) {
		return "calculation"
	}
	return "action"
}

// safeBrief 校验模型生成的口播简报是否安全可用。
// 对齐 video_search.py:79-92 _safe_brief。
var unsafeBriefPattern = regexp.MustCompile("(?i)https?://|www\\.|```|`[^`]+`|\\[\\[JIUWEN_")
func safeBrief(value string) string {
	brief := strings.TrimSpace(value)
	if brief == "" || len([]rune(brief)) > MaxRealtimeBriefChars {
		return ""
	}
	if strings.ContainsAny(brief, "\n\r") {
		return ""
	}
	if unsafeBriefPattern.MatchString(brief) {
		return ""
	}
	if strings.HasPrefix(brief, "{") || strings.HasPrefix(brief, "[") ||
		regexp.MustCompile(`"(?:status|result|answer|code)"\s*:`).MatchString(brief) {
		return ""
	}
	return brief
}

// fallbackRealtimeBrief 生成降级口播简报。
// 对齐 video_search.py:94-117 _fallback_realtime_brief。
func fallbackRealtimeBrief(displayResult, resultKind string) (string, string) {
	if ToolLanguage() == "en" {
		return "The task has finished. The full result is available in the interface.", "fallback"
	}
	derived := safeBrief(displayResult)
	if derived != "" && len([]rune(derived)) <= fallbackBriefDerivedMaxChars {
		return derived, "derived"
	}
	messages := map[string]string{
		"research":    "资料已核实，完整结论和必要来源已经显示在界面中。",
		"code":        "代码已经生成，完整内容已经显示在界面中。",
		"calculation": "计算已经完成，完整结果和过程已经显示在界面中。",
		"file":        "文件任务已经完成，完整结果已经显示在界面中。",
		"action":      "任务已经执行完成，详细结果已经显示在界面中。",
		"generic":     "任务已经完成，完整结果已经显示在界面中。",
	}
	message, ok := messages[resultKind]
	if !ok {
		message = messages["generic"]
	}
	return message, "fallback"
}

// delegationContextText 渲染历史委派上下文文本。
// 对齐 video_search.py:162-184 _delegation_context_text。
func delegationContextText(items []map[string]any) string {
	if len(items) == 0 {
		return "None"
	}
	blocks := []string{}
	remaining := MaxDelegationContextChars
	tail := items
	if len(tail) > MaxDelegationContextItems {
		tail = tail[len(tail)-MaxDelegationContextItems:]
	}
	for index, item := range tail {
		question := toString(orChain(item["question"], item["query"], "None"))
		query := toString(orChain(item["query"], item["question"], "None"))
		files := []any{}
		if f, ok := item["files"].([]any); ok {
			if len(f) > 5 {
				f = f[:5]
			}
			files = f
		}
		filesJSON, _ := json.Marshal(files)
		block := fmt.Sprintf("[%d] User instruction: %s\nExecution goal: %s\nReported files (reading contents still requires file tools): %s\nCore Agent result: %s",
			index+1, question, query, string(filesJSON), toString(orChain(item["result"], "None")))
		if len(block) > remaining {
			block = block[:remaining]
			block = strings.TrimRight(block, "")
		}
		if block == "" {
			break
		}
		blocks = append(blocks, block)
		remaining -= len(block) + 2 // 为下一块的分隔符预留
		if remaining <= 0 {
			break
		}
	}
	joined := strings.Join(blocks, "\n\n")
	if joined == "" {
		return "None"
	}
	return joined
}

// frameMediaItem 把画面帧 dataURL 转为附件条目；非法返回 nil。
// 对齐 video_search.py:186-201 _frame_media_item。
func frameMediaItem(frameDataURL string) map[string]any {
	header, rest, found := strings.Cut(frameDataURL, ",")
	encoded := rest
	separatorEmpty := !found
	if separatorEmpty || !strings.HasPrefix(strings.ToLower(header), "data:") {
		return nil
	}
	parts := strings.Split(strings.ToLower(header[5:]), ";")
	mimeType := parts[0]
	suffix, ok := imageFilenameSuffixes[mimeType]
	if !ok {
		return nil
	}
	hasBase64 := false
	for _, param := range parts[1:] {
		if param == "base64" {
			hasBase64 = true
		}
	}
	if !hasBase64 || encoded == "" {
		return nil
	}
	return map[string]any{
		"type":       "image",
		"filename":   "video-search-frame" + suffix,
		"mimeType":   mimeType,
		"base64Data": encoded,
	}
}

// failedStatuses 工具失败状态集合。
// 对齐 video_search.py:297-305 raw_status in 集合。
var failedStatuses = map[string]bool{
	"error": true, "failed": true, "failure": true, "timeout": true, "timed_out": true,
}

// collapseWhitespace 压缩连续空白为单空格。
// 对齐 re.sub(r"\s+"," ",text)。
var collapseWhitespace = func() func(string) string {
	pattern := regexp.MustCompile(`\s+`)
	return func(s string) string { return pattern.ReplaceAllString(s, " ") }
}()

// normalizedTask 归一化任务文本。
// 对齐 video_search.py:158-160 _normalized_task。
func normalizedTask(value string) string {
	return strings.ToLower(strings.TrimSpace(collapseWhitespace(value)))
}

// errorsNew errors.New 的本地包装（便于 panicOrError 语法统一）。
// Python 侧无对应实现。
func errorsNew(msg string) error { return fmt.Errorf("%s", msg) }

// orChain 依次取第一个非空值。
// Python 侧无对应实现：Python `a or b or c` 链。
func orChain(values ...any) any {
	for _, v := range values {
		if s, ok := v.(string); ok {
			if strings.TrimSpace(s) != "" {
				return s
			}
			continue
		}
		if v != nil {
			return v
		}
	}
	return ""
}

// orNil 依次取第一个非 nil 值。
// Python 侧无对应实现：Python `a or b` 对 None 链。
func orNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// firstNotNil 同 orNil（语义别名，对齐 :305-311 raw_result 回退链）。
func firstNotNil(values ...any) any { return orNil(values...) }

// orFallbackMap 非nil map 直传，否则空 map。
// 对齐 video_search.py:282-283 tool.get("arguments") if ... else {}。
func orFallbackMap(value any) any {
	if value != nil {
		return value
	}
	return map[string]any{}
}

// orEmptyList nil 切片回空切片。
func orEmptyList(items []map[string]any) []map[string]any {
	if items == nil {
		return []map[string]any{}
	}
	return items
}

// containsString 判定包含。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// copyMap 浅拷贝 map。
func copyMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}

// orOther 二选一回退（Python `question or query`）。
func orOther(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}
