package interrupt

import (
	"context"
	"encoding/json"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	askuser "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/ask_user"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserPayload 用户回答载荷。
// 问题文本到回答的映射。
//
// 对齐 Python: AskUserPayload
type AskUserPayload struct {
	// Answers 问题文本到回答的映射
	Answers map[string]string `json:"answers"`
}

// AskUserRequest 扩展 InterruptRequest，携带问题列表。
// 满足 InterruptRequester 接口（通过嵌入 InterruptRequest 继承 GetMessage/GetAutoConfirmKey）。
//
// 对齐 Python: AskUserRequest(InterruptRequest)
type AskUserRequest struct {
	// InterruptRequest 嵌入基础中断请求
	saschema.InterruptRequest
	// Questions 要向用户展示的问题列表
	Questions []map[string]any `json:"questions"`
}

// AskUserRail 向用户提问的 Rail。
// 拦截 ask_user 工具调用，首次触发中断等待用户输入，
// 用户输入后解析为 AskUserPayload 并通过 Reject 返回格式化结果。
//
// 对齐 Python: AskUserRail(BaseInterruptRail) — openjiuwen/harness/rails/interrupt/ask_user_rail.py
type AskUserRail struct {
	BaseInterruptRail
	// tools 已注册的 AskUserTool 引用，供 Uninit 注销
	tools []tool.Tool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 AskUserRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*AskUserRail)(nil)

// ──────────────────────────── 全局变量 ────────────────────────────

var askUserRailLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAskUserRail 创建 AskUserRail 实例。
// 默认拦截 "ask_user" 工具；可传入自定义工具名覆盖。
//
// 对齐 Python: AskUserRail.__init__(tool_names=["ask_user"])
func NewAskUserRail(toolNames ...string) *AskUserRail {
	// 默认拦截 "ask_user" 工具
	if len(toolNames) == 0 {
		toolNames = []string{"ask_user"}
	}
	r := &AskUserRail{
		BaseInterruptRail: *NewBaseInterruptRail(toolNames...),
	}
	// 覆盖 ResolveInterruptFn
	r.ResolveInterruptFn = r.resolveAskUserInterrupt
	return r
}

// Init 注册 AskUserTool 到 ResourceMgr + AbilityManager。
//
// 对齐 Python: AskUserRail.init(agent)
func (r *AskUserRail) Init(agent agentinterfaces.BaseAgent) error {
	var language string
	var agentID string

	sb := agent.SystemPromptBuilder()
	if sb != nil {
		language = sb.Language()
	} else {
		language = "cn"
	}
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 从 ask_user 子包创建空壳工具（对齐 Python AskUserTool.__init__）
	askUserTool, err := askuser.NewAskUserTool(language, agentID)
	if err != nil {
		logger.Warn(askUserRailLogComponent).
			Str("event_type", "ask_user_rail_init").
			Err(err).
			Msg("创建 AskUserTool 失败")
		return fmt.Errorf("创建 AskUserTool 失败: %w", err)
	}
	r.tools = []tool.Tool{askUserTool}

	// 注册到 AbilityManager + ResourceMgr
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		if am != nil {
			am.Add(t.Card())
		}
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
	}

	logger.Info(askUserRailLogComponent).
		Str("event_type", "ask_user_rail_init").
		Msg("AskUserRail 已注册 ask_user 工具")

	return nil
}

// Uninit 从 AbilityManager + ResourceMgr 注销 AskUserTool。
//
// 对齐 Python: AskUserRail.uninit(agent)
func (r *AskUserRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if len(r.tools) == 0 {
		return nil
	}

	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(askUserRailLogComponent).
						Str("event_type", "ask_user_rail_uninit").
						Str("tool_name", t.Card().Name).
						Msgf("注销工具失败: %v", rec)
				}
			}()
			if am != nil {
				am.Remove(t.Card().Name)
			}
			if resourceMgr != nil {
				_, _ = resourceMgr.RemoveTool([]string{t.Card().ID})
			}
		}(t)
	}
	r.tools = nil

	logger.Info(askUserRailLogComponent).
		Str("event_type", "ask_user_rail_uninit").
		Msg("AskUserRail 注销完成")

	return nil
}

// ParseToolArgs 解析 ToolCall.Arguments JSON 为 map。
//
// 对齐 Python: AskUserRail._parse_tool_args(tool_call)
func ParseToolArgs(toolCall *llmschema.ToolCall) map[string]any {
	if toolCall == nil {
		return map[string]any{}
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
		return map[string]any{}
	}
	return args
}

// BuildAskRequest 构建 AskUserRequest。
// 返回 *AskUserRequest（InterruptRequester 接口实现），携带 questions 字段。
// JSON 序列化时，questions 自然出现在输出中（对齐 Python model_dump + extra="allow"）。
//
// 对齐 Python: AskUserRail._build_ask_request(tool_call)
func (r *AskUserRail) BuildAskRequest(toolCall *llmschema.ToolCall) *AskUserRequest {
	args := ParseToolArgs(toolCall)
	questions, _ := args["questions"].([]any)
	// 转换 []any → []map[string]any
	questionsList := make([]map[string]any, 0, len(questions))
	for _, q := range questions {
		if qMap, ok := q.(map[string]any); ok {
			questionsList = append(questionsList, qMap)
		}
	}

	return &AskUserRequest{
		InterruptRequest: saschema.InterruptRequest{
			Message:        "",
			PayloadSchema:  askUserPayloadSchema(),
			AutoConfirmKey: "",
			UIOptions:      nil,
		},
		Questions: questionsList,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveAskUserInterrupt AskUserRail 的中断解析逻辑。
//
// 无用户输入→中断；有有效输入→Reject（返回格式化结果）。
//
// 对齐 Python: AskUserRail.resolve_interrupt(ctx, tool_call, user_input, auto_confirm_config)
func (r *AskUserRail) resolveAskUserInterrupt(
	_ context.Context,
	_ *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	_ map[string]any,
) (decision InterruptDecision) {
	// 对齐 Python try/except Exception：异常时回退到 interrupt
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(askUserRailLogComponent).
				Str("event_type", "ask_user_rail_resolve_interrupt").
				Msgf("解析用户输入异常，回退到 interrupt: %v", rec)
			decision = r.Interrupt(r.BuildAskRequest(toolCall))
		}
	}()

	// 无用户输入 → 中断
	if userInput == nil {
		return r.Interrupt(r.BuildAskRequest(toolCall))
	}

	// 解析用户输入为 AskUserPayload
	payload, ok := r.parseUserInput(userInput, toolCall)
	if !ok || len(payload.Answers) == 0 {
		return r.Interrupt(r.BuildAskRequest(toolCall))
	}

	// 有有效输入 → Reject（跳过工具执行，返回格式化结果）
	toolResult := r.formatToolResult(toolCall, payload)
	return r.Reject(toolResult)
}

// parseUserInput 解析用户输入为 AskUserPayload。
// 支持 AskUserPayload / map[string]any / string 三种格式。
//
// 对齐 Python: AskUserRail.resolve_interrupt 中的解析逻辑
func (r *AskUserRail) parseUserInput(userInput any, toolCall *llmschema.ToolCall) (*AskUserPayload, bool) {
	switch input := userInput.(type) {
	case *AskUserPayload:
		return input, true
	case map[string]any:
		return r.parseUserInputDict(input, toolCall)
	case string:
		if input == "" {
			return &AskUserPayload{}, true
		}
		// 字符串输入：尝试匹配第一个问题
		args := ParseToolArgs(toolCall)
		questions, _ := args["questions"].([]any)
		if len(questions) > 0 {
			if q, ok := questions[0].(map[string]any); ok {
				if question, ok := q["question"].(string); ok {
					return &AskUserPayload{Answers: map[string]string{question: input}}, true
				}
			}
		}
		return &AskUserPayload{}, true
	default:
		return nil, false
	}
}

// parseUserInputDict 从 dict 解析 AskUserPayload。
func (r *AskUserRail) parseUserInputDict(userInput map[string]any, toolCall *llmschema.ToolCall) (*AskUserPayload, bool) {
	// 检查是否有 answers 字段
	if answersVal, ok := userInput["answers"]; ok {
		if answersMap, ok := answersVal.(map[string]any); ok {
			answers := make(map[string]string, len(answersMap))
			for k, v := range answersMap {
				if s, ok := v.(string); ok {
					answers[k] = s
				}
			}
			return &AskUserPayload{Answers: answers}, true
		}
	}

	// 检查 answer 字段（单问题模式）
	args := ParseToolArgs(toolCall)
	questions, _ := args["questions"].([]any)
	if len(questions) == 1 {
		if q, ok := questions[0].(map[string]any); ok {
			if question, ok := q["question"].(string); ok {
				if answerVal, ok := userInput["answer"]; ok {
					if answer, ok := answerVal.(string); ok {
						return &AskUserPayload{Answers: map[string]string{question: answer}}, true
					}
				}
			}
		}
	}

	return &AskUserPayload{}, true
}

// formatToolResult 格式化用户回答为工具返回结果。
//
// 对齐 Python: AskUserRail._format_tool_result(tool_call, payload)
func (r *AskUserRail) formatToolResult(toolCall *llmschema.ToolCall, payload *AskUserPayload) string {
	args := ParseToolArgs(toolCall)
	questions, _ := args["questions"].([]any)

	if len(questions) == 0 {
		return fmt.Sprintf("%v", payload.Answers)
	}

	parts := make([]string, 0, len(questions))
	for _, q := range questions {
		if qMap, ok := q.(map[string]any); ok {
			if question, ok := qMap["question"].(string); ok {
				answer := payload.Answers[question]
				parts = append(parts, fmt.Sprintf(`"%s"="%s"`, question, answer))
			}
		}
	}

	return fmt.Sprintf("User has answered your questions: %s. You can now continue with the user's answers in mind.", joinStrings(parts, ", "))
}

// askUserPayloadSchema 返回 AskUserPayload 的 JSON Schema。
// 严格对齐 Python Pydantic AskUserPayload.model_json_schema() 输出。
func askUserPayloadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"answers": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"description":          "Question text to answer mapping",
				"title":                "Answers",
			},
		},
		"title": "AskUserPayload",
	}
}

// joinStrings 连接字符串切片。
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += sep
		}
		result += part
	}
	return result
}
