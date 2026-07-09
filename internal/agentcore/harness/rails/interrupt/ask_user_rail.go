package interrupt

import (
	"context"
	"encoding/json"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
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

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 AskUserRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*AskUserRail)(nil)

// askUserRailLogComponent 日志组件标识
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
	// 覆盖 resolveInterruptFn
	r.resolveInterruptFn = r.resolveAskUserInterrupt
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

	// 构建 AskUserTool 的 ToolCard
	toolCard, err := hprompts.BuildToolCard("ask_user", "ask_user", language, nil, agentID)
	if err != nil {
		logger.Warn(askUserRailLogComponent).
			Str("event_type", "ask_user_rail_init").
			Err(err).
			Msg("构建 ask_user ToolCard 失败")
		return fmt.Errorf("构建 ask_user ToolCard 失败: %w", err)
	}

	// 创建 MapFunction 空壳工具（逻辑在 Rail 中，工具本身不执行）
	askUserTool, err := tool.NewMapFunction(
		toolCard,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{}, nil
		},
		nil,
	)
	if err != nil {
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
) InterruptDecision {
	// 无用户输入 → 中断
	if userInput == nil {
		return r.Interrupt(r.buildAskRequest(toolCall))
	}

	// 解析用户输入为 AskUserPayload
	payload, ok := r.parseUserInput(userInput, toolCall)
	if !ok || len(payload.Answers) == 0 {
		return r.Interrupt(r.buildAskRequest(toolCall))
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
		args := parseToolArgs(toolCall)
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
	args := parseToolArgs(toolCall)
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
	args := parseToolArgs(toolCall)
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

	return fmt.Sprintf("用户已回答你的问题: %s。你现在可以继续。", joinStrings(parts, ", "))
}

// buildAskRequest 构建 AskUserRequest。
//
// 对齐 Python: AskUserRail._build_ask_request(tool_call)
func (r *AskUserRail) buildAskRequest(toolCall *llmschema.ToolCall) *saschema.InterruptRequest {
	args := parseToolArgs(toolCall)
	questions, _ := args["questions"].([]any)
	questionsAny := make([]map[string]any, 0, len(questions))
	for _, q := range questions {
		if qMap, ok := q.(map[string]any); ok {
			questionsAny = append(questionsAny, qMap)
		}
	}

	return &saschema.InterruptRequest{
		Message:       "",
		PayloadSchema: askUserPayloadSchema(),
		AutoConfirmKey: "",
		UIOptions:     nil,
	}
}

// askUserPayloadSchema 返回 AskUserPayload 的 JSON Schema。
func askUserPayloadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"answers": map[string]any{
				"type":        "object",
				"description": "问题文本到回答的映射",
				"additionalProperties": map[string]any{
					"type": "string",
				},
			},
		},
	}
}

// parseToolArgs 解析 ToolCall.Arguments JSON 为 map。
//
// 对齐 Python: AskUserRail._parse_tool_args(tool_call)
func parseToolArgs(toolCall *llmschema.ToolCall) map[string]any {
	if toolCall == nil {
		return map[string]any{}
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
		return map[string]any{}
	}
	return args
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
