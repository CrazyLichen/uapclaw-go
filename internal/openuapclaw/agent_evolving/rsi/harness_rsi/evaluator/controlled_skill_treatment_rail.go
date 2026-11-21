// controlled_skill_treatment_rail.go 候选技能的评估专属受控交付。
// Python: openjiuwen/rsi/harness_rsi/evaluator/controlled_skill_treatment_rail.py
// （198 行全量对齐）。仅在诊断 sidecar 挂载——自然主评估未命中技能激活时，
// 区分"发现失败"与"内容失败"；结果永不参与候选接受或评分。

package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
)

// ──────────────────────────── 常量 ────────────────────────────

// ControlledSkillTreatmentCaseKey 受控技能治疗 case 键
// （Python: CONTROLLED_SKILL_TREATMENT_CASE_KEY）。
const ControlledSkillTreatmentCaseKey = "_controlled_skill_treatment"

// extraSkipToolKeyLocal 跳过工具 extra 键（与 AgentModeRail 同键名约定）。
const extraSkipToolKeyLocal = "_skip_tool"

// ──────────────────────────── 结构体 ────────────────────────────

// ControlledSkillTreatmentRail 在求解器调查前交付确定的候选技能。
// Python: controlled_skill_treatment_rail.py:44 ControlledSkillTreatmentRail(DeepAgentRail)。
type ControlledSkillTreatmentRail struct {
	rails.DeepAgentRail
	// systemPromptBuilder 系统提示词构建器（Python self.system_prompt_builder）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// skillName 目标技能名（归一化后）
	skillName string
	// modelCallCount 交付前模型调用计数
	modelCallCount int
	// toolCallCount 交付前工具调用计数
	toolCallCount int
	// firstRequestedToolName 首个请求工具名
	firstRequestedToolName string
	// rewrittenSkillNames 被改写的技能名清单
	rewrittenSkillNames []string
	// blockedToolNames 被拦截的工具名清单
	blockedToolNames []string
	// delivered 是否已交付
	delivered bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewControlledSkillTreatmentRail 构造受控技能治疗护栏。
// Python: controlled_skill_treatment_rail.py:52-64 __init__。
func NewControlledSkillTreatmentRail(skillName string) (*ControlledSkillTreatmentRail, error) {
	normalized := strings.TrimSpace(skillName)
	if normalized == "" {
		return nil, fmt.Errorf("controlled Skill treatment requires a skill name")
	}
	return &ControlledSkillTreatmentRail{
		skillName:           normalized,
		rewrittenSkillNames: []string{},
		blockedToolNames:    []string{},
	}, nil
}

// Priority 优先级 -100（Python: priority = -100）。
func (r *ControlledSkillTreatmentRail) Priority() int { return -100 }

// Init 记录系统提示词构建器。
// Python: controlled_skill_treatment_rail.py:66-68 init。
func (r *ControlledSkillTreatmentRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	if agent != nil {
		r.systemPromptBuilder = agent.SystemPromptBuilder()
	}
	return nil
}

// Uninit 移除提示节并清构建器。
// Python: controlled_skill_treatment_rail.py:70-73 uninit。
func (r *ControlledSkillTreatmentRail) Uninit(_ agentinterfaces.BaseAgent) error {
	r.removeSection()
	r.systemPromptBuilder = nil
	return nil
}

// BeforeInvoke 重置一轮治疗状态。
// Python: controlled_skill_treatment_rail.py:75-83 before_invoke。
func (r *ControlledSkillTreatmentRail) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	r.modelCallCount = 0
	r.toolCallCount = 0
	r.firstRequestedToolName = ""
	r.rewrittenSkillNames = []string{}
	r.blockedToolNames = []string{}
	r.delivered = false
	return nil
}

// BeforeModelCall 安装提示节并裁剪工具清单至 skill 工具。
// Python: controlled_skill_treatment_rail.py:85-95 before_model_call。
func (r *ControlledSkillTreatmentRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.delivered {
		return nil
	}
	r.modelCallCount++
	r.installSection()
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}
	// Python: ctx.inputs.tools = [tool for tool in tools if _canonical_tool_name(...) == "skill"]
	tools := make([]cschema.ToolInfoInterface, 0, len(inputs.Tools))
	for _, item := range inputs.Tools {
		if canonicalToolName(toolDefinitionName(item)) == "skill" {
			tools = append(tools, item)
		}
	}
	inputs.Tools = tools
	return nil
}

// BeforeToolCall 拦截非 skill 工具、改写技能名（Python: :97-120 before_tool_call）。
func (r *ControlledSkillTreatmentRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.delivered {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	requestedTool := inputs.ToolName
	if r.firstRequestedToolName == "" {
		r.firstRequestedToolName = requestedTool
	}
	if canonicalToolName(requestedTool) != "skill" {
		r.blockedToolNames = append(r.blockedToolNames, requestedTool)
		r.rejectTool(cbc, fmt.Sprintf(
			"Controlled candidate treatment requires skill_tool('%s') "+
				"before any investigation tool.", r.skillName))
		return nil
	}
	args := normalizeArgsLocal(inputs.ToolArgs)
	requestedSkill := ""
	if raw, exists := args["skill_name"]; exists {
		requestedSkill = strings.TrimSpace(fmt.Sprint(raw))
	}
	if requestedSkill != r.skillName {
		if requestedSkill != "" {
			r.rewrittenSkillNames = append(r.rewrittenSkillNames, requestedSkill)
		}
		inputs.ToolArgs = map[string]any{"skill_name": r.skillName}
	}
	return nil
}

// AfterToolCall 技能成功载入即完成交付并移除提示节。
// Python: controlled_skill_treatment_rail.py:122-135 after_tool_call。
func (r *ControlledSkillTreatmentRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.delivered {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	if canonicalToolName(inputs.ToolName) != "skill" {
		return nil
	}
	args := normalizeArgsLocal(inputs.ToolArgs)
	requestedSkill := ""
	if raw, exists := args["skill_name"]; exists {
		requestedSkill = strings.TrimSpace(fmt.Sprint(raw))
	}
	if requestedSkill != r.skillName {
		return nil
	}
	if !toolResultSucceeded(inputs.ToolResult) {
		return nil
	}
	r.delivered = true
	r.removeSection()
	return nil
}

// AfterInvoke 收口移除提示节。
// Python: controlled_skill_treatment_rail.py:137-139 after_invoke。
func (r *ControlledSkillTreatmentRail) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	r.removeSection()
	return nil
}

// Evidence 机器可读的治疗交付证据。
// Python: controlled_skill_treatment_rail.py:141-152 evidence。
func (r *ControlledSkillTreatmentRail) Evidence() map[string]any {
	return map[string]any{
		"mode":                             "controlled_first_action",
		"expected_skill_name":              r.skillName,
		"model_call_count_before_delivery": r.modelCallCount,
		"tool_call_count_before_delivery":  r.toolCallCount,
		"first_requested_tool_name":        r.firstRequestedToolName,
		"rewritten_skill_names":            append([]string{}, r.rewrittenSkillNames...),
		"blocked_tool_names":               append([]string{}, r.blockedToolNames...),
		"delivered":                        r.delivered,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// installSection 安装受控治疗提示节。
// Python: controlled_skill_treatment_rail.py:154-172 _install_section（文案逐字）。
func (r *ControlledSkillTreatmentRail) installSection() {
	if r.systemPromptBuilder == nil {
		return
	}
	content := "# Controlled Candidate Skill Treatment\n\n" +
		"This turn is a controlled capability experiment, not natural routing. " +
		"Before reading files, running commands, forming a task hypothesis, or " +
		"editing anything, your first and only action on this turn must be " +
		fmt.Sprintf("`skill_tool` with `{\"skill_name\": \"%s\"}`. ", r.skillName) +
		"Do not call another tool in the same response. After the Skill result is " +
		"returned, use its causal discriminator and acceptance probe to solve the " +
		"original task."
	r.systemPromptBuilder.AddSection(saprompt.NewPromptSection(
		"controlled_candidate_skill_treatment",
		map[string]string{"cn": content, "en": content},
		1,
	))
}

// removeSection 移除提示节。
// Python: controlled_skill_treatment_rail.py:174-177 _remove_section。
func (r *ControlledSkillTreatmentRail) removeSection() {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection("controlled_candidate_skill_treatment")
	}
}

// rejectTool 拒绝工具调用（Python: _reject_tool——skip 标记 + 预设结果 + ToolMsg）。
func (r *ControlledSkillTreatmentRail) rejectTool(cbc *agentinterfaces.AgentCallbackContext, message string) {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	toolCallID := ""
	if ok && inputs != nil && inputs.ToolCall != nil {
		toolCallID = inputs.ToolCall.ID
	}
	cbc.SetExtra(extraSkipToolKeyLocal, true)
	if ok && inputs != nil {
		inputs.ToolResult = map[string]any{"error": message}
		inputs.ToolMsg = llmschema.NewToolMessage(toolCallID, message)
	}
}

// canonicalToolName 复用 trajectory_usage.go 同名助手（Python 同源静态方法）。

// toolDefinitionName 工具定义名提取（Python: _tool_definition_name——
// OpenAI function 信封 / dict / 对象三形态；Go ToolInfoInterface 走 GetName）。
func toolDefinitionName(raw any) string {
	switch tool := raw.(type) {
	case cschema.ToolInfoInterface:
		return tool.GetName()
	case map[string]any:
		if fn, ok := tool["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				return name
			}
		}
		if name, ok := tool["name"].(string); ok {
			return name
		}
		return ""
	default:
		return ""
	}
}

// toolResultSucceeded 工具结果成功判定（Python: _tool_result_succeeded——
// dict 的 success=false/error/status 黑名单；字符串 error/exception/failed 前缀）。
func toolResultSucceeded(result any) bool {
	if result == nil {
		return false
	}
	switch v := result.(type) {
	case map[string]any:
		if success, ok := v["success"].(bool); ok && !success {
			return false
		}
		if errText, exists := v["error"]; exists {
			if errText != nil && fmt.Sprint(errText) != "" {
				return false
			}
		}
		status := strings.ToLower(strings.TrimSpace(stringOrTP(v["status"], "")))
		return status != "error" && status != "failed" && status != "failure" && status != "skipped"
	case string:
		normalized := strings.ToLower(strings.TrimSpace(v))
		return !strings.HasPrefix(normalized, "error") &&
			!strings.HasPrefix(normalized, "exception") &&
			!strings.HasPrefix(normalized, "failed")
	default:
		return true
	}
}

// normalizeArgsLocal 参数规范化（Python: _normalize_args——dict 拷贝 / JSON 字符串）。
func normalizeArgsLocal(raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		cp := map[string]any{}
		for key, value := range m {
			cp[key] = value
		}
		return cp
	}
	if text, ok := raw.(string); ok {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			return map[string]any{}
		}
		if parsed != nil {
			return parsed
		}
	}
	return map[string]any{}
}
