package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMGenerationResult LLM 生成结果。
// 对齐 Python: _generate_agent_with_llm 返回 (when_to_use, system_prompt)
type LLMGenerationResult struct {
	// WhenToUse 调度描述（告诉 LLM 何时调度此 agent）
	WhenToUse string
	// SystemPrompt 系统提示词
	SystemPrompt string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// agentCreationSystemPrompt LLM 生成 agent 配置的系统提示词。
	// 对齐 Python: _AGENT_CREATION_SYSTEM_PROMPT
	agentCreationSystemPrompt = `You are an elite AI agent architect. When given an agent name and description, your job is to design a high-performance agent that EXECUTES tasks to completion — not just analyzes and reports.

The agent will have access to tools (Read, Write, Edit, Bash, etc.) to complete tasks. Design it as an autonomous expert capable of handling its designated tasks with minimal additional guidance. The system prompt you write is the agent's complete operational manual.

1. **whenToUse**: A precise description of when the main assistant should dispatch to this agent.
   - Start with "Use this agent when..."
   - Include concrete triggering conditions
   - Add 2-3 <example> blocks showing specific scenarios where the assistant uses the Agent tool to fully delegate the task
   - Each <example> should show: user says X → assistant dispatches to this agent with the Agent tool, passing the complete task
   - Write in the same language as the agent description (Chinese description → Chinese whenToUse)

2. **systemPrompt**: The complete system prompt governing the agent's behavior.
   - Define expert persona and role
   - Specify workflow and methodology — end-to-end, from analysis through execution
   - Establish clear behavioral boundaries and operational parameters
   - Provide specific methodologies and best practices for task execution
   - Define output format expectations when relevant
   - Include self-verification steps
   - Write in the same language as the agent description

Key principles:
- Be specific rather than generic — avoid vague instructions
- Include concrete examples when they would clarify behavior
- Balance comprehensiveness with clarity — every instruction should add value
- Ensure the agent has enough context to handle variations of the core task
- Build in quality assurance and self-correction mechanisms

Return ONLY a JSON object:
{"whenToUse": "...", "systemPrompt": "..."}`
)

// ──────────────────────────── 全局变量 ────────────────────────────

// jsonBlockPattern 用于从 LLM 响应中提取 JSON 块的正则
var jsonBlockPattern = regexp.MustCompile(`\{[\s\S]*\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateAgentWithLLM 调用 LLM 生成 agent 的 whenToUse 和 systemPrompt。
// 对齐 Python: _generate_agent_with_llm(name, description)
//
// 返回 (when_to_use, system_prompt) 或 nil（生成失败时回退到模板）。
func GenerateAgentWithLLM(ctx context.Context, model *llm.Model, name string, description string) *LLMGenerationResult {
	// 步骤 1: 校验参数
	if model == nil {
		logger.Warn(logComponent).Msg("[agents.create] no model available for LLM generation")
		return nil
	}
	if name == "" || description == "" {
		logger.Warn(logComponent).Msg("[agents.create] name or description is empty")
		return nil
	}

	// 步骤 2: 构建完整 prompt
	// 对齐 Python: full_prompt = f"{_AGENT_CREATION_SYSTEM_PROMPT}\n---\n请为以下 agent 生成配置：\n名称: {name}\n描述: {description}\n..."
	fullPrompt := fmt.Sprintf(`%s

---
请为以下 agent 生成配置：

名称: %s
描述: %s

返回 JSON 对象，包含 whenToUse 和 systemPrompt 两个字段。不要返回其他内容。`, agentCreationSystemPrompt, name, description)

	// 步骤 3: 调用 LLM
	// 对齐 Python: result = await model.invoke([UserMessage(content=full_prompt)], max_tokens=2000, temperature=0.3)
	messages := model_clients.NewMessagesParam(llmschema.NewUserMessage(fullPrompt))
	result, err := model.Invoke(ctx, messages,
		model_clients.WithInvokeMaxTokens(2000),
		model_clients.WithInvokeTemperature(0.3),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("[agents.create] LLM generation failed")
		return nil
	}

	// 步骤 4: 提取文本内容
	// 对齐 Python: text = getattr(result, "content", None) or str(result)
	text := ""
	if result != nil {
		text = result.Content.Text()
	}
	if text == "" {
		logger.Warn(logComponent).Msg("[agents.create] LLM returned empty response")
		return nil
	}

	// 步骤 5: 解析 JSON 响应
	// 对齐 Python: data = _json.loads(text.strip()) / match = _re.search(r"\{[\s\S]*\}", text)
	return parseLLMGenerationResponse(text)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseLLMGenerationResponse 从 LLM 响应文本中解析 whenToUse 和 systemPrompt。
// 对齐 Python: _generate_agent_with_llm 中 JSON 解析逻辑
func parseLLMGenerationResponse(text string) *LLMGenerationResult {
	text = strings.TrimSpace(text)

	// 步骤 1: 直接解析 JSON
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		// 步骤 2: 尝试从文本中提取 JSON 块
		match := jsonBlockPattern.FindString(text)
		if match == "" {
			logger.Warn(logComponent).Str("response", truncate(text, 200)).Msg("[agents.create] no JSON found in LLM response")
			return nil
		}
		if err := json.Unmarshal([]byte(match), &data); err != nil {
			logger.Warn(logComponent).Str("response", truncate(text, 200)).Msg("[agents.create] JSON parse failed")
			return nil
		}
	}

	// 步骤 3: 提取字段
	// 对齐 Python: when_to_use = (data.get("whenToUse") or "").strip()
	whenToUse, _ := data["whenToUse"].(string)
	whenToUse = strings.TrimSpace(whenToUse)
	systemPrompt, _ := data["systemPrompt"].(string)
	systemPrompt = strings.TrimSpace(systemPrompt)

	// 步骤 4: 校验完整性
	if whenToUse == "" || systemPrompt == "" {
		logger.Warn(logComponent).Any("data", data).Msg("[agents.create] incomplete LLM response")
		return nil
	}

	return &LLMGenerationResult{
		WhenToUse:    whenToUse,
		SystemPrompt: systemPrompt,
	}
}

// truncate 截断字符串到指定长度
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
