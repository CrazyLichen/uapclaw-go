package rails

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StructuredAskUserPayload 结构化用户回答载荷。
// 问题文本到选择选项标签的映射。
//
// 对齐 Python: StructuredAskUserPayload
type StructuredAskUserPayload struct {
	// Answers 问题文本到选择选项标签的映射
	Answers map[string]string `json:"answers"`
}

// StructuredAskUserRail 扩展 AskUserRail，支持结构化选项问答。
// 当 LLM 调用 ask_user 工具时携带 questions 参数（包含 header/options/multi_select），
// 前端可渲染为可点击选项而非纯文本输入框。
//
// 机制：ToolCallInterruptRequest.tool_args 保留原始工具调用参数（包括 questions），
// interrupt_helpers 的 _extract_questions_from_value() 检查 tool_args 中的 questions
// 字段并转换为前端格式。
//
// 对齐 Python: StructuredAskUserRail(AskUserRail) — jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
type StructuredAskUserRail struct {
	interrupt.AskUserRail
	// structuredTools 已注册的 StructuredAskUserTool 引用，供 Uninit 注销
	structuredTools []tool.Tool
	// language 语言设置
	language string
	// parentResolve 保存父类 resolve 函数，用于非结构化路径回退
	parentResolve interrupt.ResolveInterruptFn
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 StructuredAskUserRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*StructuredAskUserRail)(nil)

// structuredAskUserRailLogComponent 日志组件标识
var structuredAskUserRailLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStructuredAskUserRail 创建 StructuredAskUserRail 实例。
//
// 对齐 Python: StructuredAskUserRail.__init__(tool_names=None, language=None)
func NewStructuredAskUserRail(language string) *StructuredAskUserRail {
	r := &StructuredAskUserRail{
		AskUserRail: *interrupt.NewAskUserRail(),
		language:    language,
	}
	// 保存父类 resolve 函数（对齐 Python super().resolve_interrupt()）
	r.parentResolve = r.AskUserRail.BaseInterruptRail.ResolveInterruptFn
	// 覆盖为自己的 resolve 函数
	r.AskUserRail.BaseInterruptRail.ResolveInterruptFn = r.resolveStructuredInterrupt
	return r
}

// Init 注册 StructuredAskUserTool 到 ResourceMgr + AbilityManager。
// 覆盖父类 AskUserRail 的 Init，注册扩展版工具。
//
// 对齐 Python: StructuredAskUserRail.init(agent)
func (r *StructuredAskUserRail) Init(agent agentinterfaces.BaseAgent) error {
	// 确定语言（对齐 Python: self._language or resolve_language()）
	language := r.language
	if language == "" {
		if sb := agent.SystemPromptBuilder(); sb != nil {
			language = sb.Language()
		}
		if language == "" {
			language = "cn"
		}
	}

	// 获取 agentID（对齐 Python: getattr(getattr(agent, "card", None), "id", None)）
	var agentID string
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 创建 StructuredAskUserTool（对齐 Python: StructuredAskUserTool(language=language, agent_id=agent_id)）
	askUserTool, err := NewStructuredAskUserTool(language, agentID)
	if err != nil {
		logger.Warn(structuredAskUserRailLogComponent).
			Str("event_type", "structured_ask_user_rail_init").
			Err(err).
			Msg("创建 StructuredAskUserTool 失败")
		return fmt.Errorf("创建 StructuredAskUserTool 失败: %w", err)
	}
	r.structuredTools = []tool.Tool{askUserTool}

	// 注册到 AbilityManager + ResourceMgr（对齐 Python: Runner.resource_mgr.add_tool + agent.ability_manager.add）
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.structuredTools {
		if am != nil {
			am.Add(t.Card())
		}
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
	}

	logger.Info(structuredAskUserRailLogComponent).
		Str("event_type", "structured_ask_user_rail_init").
		Str("language", language).
		Msg("StructuredAskUserRail 已注册 structured ask_user 工具")

	return nil
}

// Uninit 从 AbilityManager + ResourceMgr 注销 StructuredAskUserTool。
//
// 对齐 Python: StructuredAskUserRail.uninit(agent)
func (r *StructuredAskUserRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if len(r.structuredTools) == 0 {
		return nil
	}

	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.structuredTools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(structuredAskUserRailLogComponent).
						Str("event_type", "structured_ask_user_rail_uninit").
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
	r.structuredTools = nil

	logger.Info(structuredAskUserRailLogComponent).
		Str("event_type", "structured_ask_user_rail_uninit").
		Msg("StructuredAskUserRail 注销完成")

	return nil
}

// GetStructuredTools 返回已注册的结构化工具列表。
//
// 对齐 Python: StructuredAskUserRail.get_structured_tools()
func (r *StructuredAskUserRail) GetStructuredTools() []tool.Tool {
	return r.structuredTools
}

// ExtractQuestions 从工具调用参数中提取 questions 列表。
//
// 对齐 Python: StructuredAskUserRail.extract_questions(tool_call)
func (r *StructuredAskUserRail) ExtractQuestions(toolCall *llmschema.ToolCall) []map[string]any {
	if toolCall == nil {
		return nil
	}

	args := parseToolArgsJSON(toolCall.Arguments)
	questionsRaw, ok := args["questions"].([]any)
	if !ok || len(questionsRaw) == 0 {
		return nil
	}

	questions := make([]map[string]any, 0, len(questionsRaw))
	for _, q := range questionsRaw {
		if qMap, ok := q.(map[string]any); ok {
			questions = append(questions, qMap)
		}
	}
	if len(questions) == 0 {
		return nil
	}
	return questions
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveStructuredInterrupt StructuredAskUserRail 的中断解析逻辑。
//
// 结构化路径：解析为 StructuredAskUserPayload，格式化为 "question: answer" 文本，Reject。
// 非结构化路径：回退父类 resolve 函数（对齐 Python super().resolve_interrupt()）。
//
// 对齐 Python: StructuredAskUserRail.resolve_interrupt(ctx, tool_call, user_input, auto_confirm_config)
func (r *StructuredAskUserRail) resolveStructuredInterrupt(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) (decision interrupt.InterruptDecision) {
	// 对齐 Python try/except Exception：异常时回退到 interrupt
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(structuredAskUserRailLogComponent).
				Str("event_type", "structured_ask_user_rail_resolve").
				Msgf("解析结构化输入异常，回退到 interrupt: %v", rec)
			decision = r.AskUserRail.Interrupt(r.AskUserRail.BuildAskRequest(toolCall))
		}
	}()

	// 无用户输入 → 中断（对齐 Python: if user_input is None: return self.interrupt(self._build_ask_request(tool_call))）
	if userInput == nil {
		return r.AskUserRail.Interrupt(r.AskUserRail.BuildAskRequest(toolCall))
	}

	// 检测是否为结构化问答（对齐 Python: questions_data = self.extract_questions(tool_call)）
	questionsData := r.ExtractQuestions(toolCall)
	isStructured := len(questionsData) > 0

	if isStructured {
		// 结构化路径
		payload, ok := r.parseStructuredInput(userInput)
		if !ok || len(payload.Answers) == 0 {
			return r.AskUserRail.Interrupt(r.AskUserRail.BuildAskRequest(toolCall))
		}

		// 格式化回答文本（对齐 Python: answer_parts = [f"{q_text}: {selected}" ...]）
		answerParts := make([]string, 0, len(payload.Answers))
		for qText, selected := range payload.Answers {
			if qText == "__free_text__" {
				answerParts = append(answerParts, selected)
			} else {
				answerParts = append(answerParts, fmt.Sprintf("%s: %s", qText, selected))
			}
		}
		answerText := strings.Join(answerParts, "\n")

		// 对齐 Python: logger.info("[StructuredAskUserRail] Resolved structured answer: %s", answer_text)
		logger.Info(structuredAskUserRailLogComponent).
			Str("event_type", "structured_ask_user_rail_resolve").
			Str("answer_text", answerText).
			Msg("Resolved structured answer")

		// 对齐 Python: return self.reject(tool_result=answer_text)
		return r.AskUserRail.BaseInterruptRail.Reject(answerText)
	}

	// 非结构化路径（对齐 Python: Plain query — delegate to parent）
	// Python 对 string 输入直接 reject：elif isinstance(user_input, str): return self.reject(tool_result=user_input)
	// Python 对 AskUserPayload 输入回退父类：if isinstance(user_input, AskUserPayload): return await super().resolve_interrupt(...)
	// Python 对其他类型也回退父类：return await super().resolve_interrupt(...)
	if strInput, ok := userInput.(string); ok && strInput != "" {
		// 对齐 Python: elif isinstance(user_input, str): return self.reject(tool_result=user_input)
		return r.AskUserRail.BaseInterruptRail.Reject(strInput)
	}
	return r.parentResolve(ctx, cbc, toolCall, userInput, autoConfirmConfig)
}

// parseStructuredInput 解析用户输入为 StructuredAskUserPayload。
// 支持 StructuredAskUserPayload / map[string]any / interrupt.AskUserPayload / string 四种格式。
//
// 对齐 Python: StructuredAskUserRail.resolve_interrupt 中的解析逻辑
func (r *StructuredAskUserRail) parseStructuredInput(userInput any) (*StructuredAskUserPayload, bool) {
	switch input := userInput.(type) {
	case *StructuredAskUserPayload:
		// 对齐 Python: isinstance(user_input, StructuredAskUserPayload)
		return input, true
	case *interrupt.AskUserPayload:
		// 对齐 Python: isinstance(user_input, AskUserPayload)
		// Python 中先检查 free_text = getattr(user_input, "answer", None)
		// Go 的 AskUserPayload 没有 answer 字段，只有 answers，直接透传
		return &StructuredAskUserPayload{Answers: input.Answers}, true
	case map[string]any:
		// 对齐 Python: isinstance(user_input, dict)
		if answersVal, ok := input["answers"]; ok {
			// 对齐 Python: if "answers" in user_input: StructuredAskUserPayload(answers=user_input.get("answers", {}))
			if answersMap, ok := answersVal.(map[string]any); ok {
				answers := make(map[string]string, len(answersMap))
				for k, v := range answersMap {
					if s, ok := v.(string); ok {
						answers[k] = s
					}
				}
				return &StructuredAskUserPayload{Answers: answers}, true
			}
		}
		// 对齐 Python: else: StructuredAskUserPayload(answers=user_input)
		// Frontend sends answers as {question: selected_option}
		answers := make(map[string]string, len(input))
		for k, v := range input {
			if s, ok := v.(string); ok {
				answers[k] = s
			}
		}
		return &StructuredAskUserPayload{Answers: answers}, true
	case string:
		// 对齐 Python: isinstance(user_input, str) → StructuredAskUserPayload(answers={"__free_text__": user_input})
		if input == "" {
			return &StructuredAskUserPayload{}, true
		}
		return &StructuredAskUserPayload{Answers: map[string]string{"__free_text__": input}}, true
	default:
		// 对齐 Python: else: return self.interrupt(self._build_ask_request(tool_call))
		return nil, false
	}
}

// parseToolArgsJSON 解析 ToolCall.Arguments JSON 字符串为 map。
// 本包本地实现，不依赖 interrupt 包的未导出函数。
//
// 对齐 Python: StructuredAskUserRail.extract_questions 中的 json.loads(tool_call.arguments)
func parseToolArgsJSON(arguments string) map[string]any {
	if arguments == "" {
		return map[string]any{}
	}
	args := make(map[string]any)
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return map[string]any{}
	}
	return args
}
