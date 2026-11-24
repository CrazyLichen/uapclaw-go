// Package signal 提供自动技能创建建议信号。
//
// 对应 Python 代码：openjiuwen/agent_evolving/signal/skill_creation.py
// （1:1 移植；15.24a Task 7 消费面补缺——Python 于 89b9cbe8 切 spans 访问器，
// Go 侧整缺随本批回填）。
package signal

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SkillCreationSignalPromptEligible 达到提示阈值，可建议创建技能
	// Python: SKILL_CREATION_SIGNAL_PROMPT_ELIGIBLE
	SkillCreationSignalPromptEligible = "prompt_eligible"
	// SkillCreationSignalSkillToolCover 窗口内已使用 skill_tool
	// Python: SKILL_CREATION_SIGNAL_SKILL_TOOL_COVER
	SkillCreationSignalSkillToolCover = "skill_tool_cover"
	// FirstPromptToolIterationThreshold 首次提示：有效工具调用迭代阈值
	// Python: FIRST_PROMPT_TOOL_ITERATION_THRESHOLD
	FirstPromptToolIterationThreshold = 6
	// FirstPromptToolCallThreshold 首次提示：有效工具调用次数阈值
	// Python: FIRST_PROMPT_TOOL_CALL_THRESHOLD
	FirstPromptToolCallThreshold = 10
	// RepromptToolIterationThreshold 重复提示：迭代增量阈值
	// Python: REPROMPT_TOOL_ITERATION_THRESHOLD
	RepromptToolIterationThreshold = 2
	// RepromptToolCallThreshold 重复提示：调用次数增量阈值
	// Python: REPROMPT_TOOL_CALL_THRESHOLD
	RepromptToolCallThreshold = 4
)

// excludedSkillCreationToolNames 排除的工具名（非任务执行类）。
// Python: _EXCLUDED_TOOL_NAMES
var excludedSkillCreationToolNames = map[string]struct{}{
	"ask_user": {}, "skill_tool": {}, "prepare_skill_evolution": {},
	"evolve_review_task": {}, "evolve_skill_experiences": {},
	"spawn_member": {}, "spawn_teammate": {}, "spawn_human_agent": {},
	"spawn_bridge_agent": {}, "spawn_external_cli": {},
	"send_message": {}, "view_task": {},
}

// excludedToolKeywords 排除的工具名关键词。
// Python: _EXCLUDED_TOOL_KEYWORDS
var excludedToolKeywords = []string{
	"follow_up", "followup", "heartbeat", "cron", "background",
}

// ──────────────────────────── 结构体 ────────────────────────────

// SkillCreationWindowMetrics 检测技能创建跟进信号用的窗口指标。
// Python: SkillCreationWindowMetrics dataclass。
type SkillCreationWindowMetrics struct {
	// TotalRawToolCalls 原始工具 span 总数
	TotalRawToolCalls int
	// WindowEffectiveToolCallingIterations 窗口内有效工具调用迭代数
	WindowEffectiveToolCallingIterations int
	// WindowEffectiveToolCalls 窗口内有效工具调用次数
	WindowEffectiveToolCalls int
	// TotalEffectiveToolCallingIterations 全程有效工具调用迭代数
	TotalEffectiveToolCallingIterations int
	// TotalEffectiveToolCalls 全程有效工具调用次数
	TotalEffectiveToolCalls int
	// SkillToolUsedInWindow 窗口内是否用过 skill_tool
	SkillToolUsedInWindow bool
}

// SkillCreationSignal 从工具轨迹指标检出的技能创建信号。
// Python: SkillCreationSignal dataclass。
type SkillCreationSignal struct {
	// SignalType 信号类别（prompt_eligible / skill_tool_cover）
	SignalType string
	// Metrics 检出时的窗口指标
	Metrics SkillCreationWindowMetrics
	// Reason 检出原因
	Reason string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SkillCreationSignalDetector 从轨迹工具窗口检测技能创建信号。
// Python: SkillCreationSignalDetector。
type SkillCreationSignalDetector struct{}

// CollectMetrics 收集技能创建触发的确定性有效指标。
// Python: SkillCreationSignalDetector.collect_metrics。
func (SkillCreationSignalDetector) CollectMetrics(traj *trajectory.Trajectory, rawToolCallWatermark int) SkillCreationWindowMetrics {
	if traj == nil {
		return SkillCreationWindowMetrics{}
	}

	var toolSpans []map[string]any
	for _, span := range trajectory.IterSpans(traj) {
		if trajectory.SpanCategory(span) == "tool" {
			toolSpans = append(toolSpans, span)
		}
	}
	totalRawToolCalls := len(toolSpans)
	watermark := rawToolCallWatermark
	if watermark < 0 {
		watermark = 0
	}
	var windowToolSpans []map[string]any
	if watermark < len(toolSpans) {
		windowToolSpans = toolSpans[watermark:]
	}

	effectiveCallIDsAfterWatermark := map[string]struct{}{}
	windowEffectiveToolCalls := 0
	totalEffectiveToolCalls := 0
	skillToolUsedInWindow := false
	for index, span := range toolSpans {
		toolName := spanToolName(span)
		if index >= watermark && NormalizeToolName(toolName) == "skill_tool" {
			skillToolUsedInWindow = true
		}
		if !IsEffectiveTaskTool(toolName) {
			continue
		}
		totalEffectiveToolCalls++
		if index >= watermark {
			windowEffectiveToolCalls++
			if toolCallID := spanToolCallID(span); toolCallID != "" {
				effectiveCallIDsAfterWatermark[toolCallID] = struct{}{}
			}
		}
	}

	totalEffectiveToolCallingIterations := CountToolCallingIterations(traj)
	windowEffectiveToolCallingIterations := countNewEffectiveToolCallingIterations(
		traj, effectiveCallIDsAfterWatermark, windowToolSpans)

	return SkillCreationWindowMetrics{
		TotalRawToolCalls:                    totalRawToolCalls,
		WindowEffectiveToolCallingIterations: windowEffectiveToolCallingIterations,
		WindowEffectiveToolCalls:             windowEffectiveToolCalls,
		TotalEffectiveToolCallingIterations:  totalEffectiveToolCallingIterations,
		TotalEffectiveToolCalls:              totalEffectiveToolCalls,
		SkillToolUsedInWindow:                skillToolUsedInWindow,
	}
}

// Detect 检测当前轨迹窗口的技能创建信号。
// metrics 非 nil 时直接使用该快照，不重扫轨迹、不再应用 watermark。
// promptedSnapshot 为 nil 表示首次提示判定；非 nil 为 (iterations, calls)。
// Python: SkillCreationSignalDetector.detect。
func (SkillCreationSignalDetector) Detect(traj *trajectory.Trajectory, rawToolCallWatermark int, promptedSnapshot *[2]int, metrics *SkillCreationWindowMetrics) []SkillCreationSignal {
	if traj == nil {
		return nil
	}
	var currentMetrics SkillCreationWindowMetrics
	if metrics != nil {
		currentMetrics = *metrics
	} else {
		currentMetrics = SkillCreationSignalDetector{}.CollectMetrics(traj, rawToolCallWatermark)
	}

	if currentMetrics.SkillToolUsedInWindow {
		return []SkillCreationSignal{{
			SignalType: SkillCreationSignalSkillToolCover,
			Metrics:    currentMetrics,
			Reason:     "skill_tool_used",
		}}
	}

	if promptedSnapshot == nil {
		if currentMetrics.WindowEffectiveToolCallingIterations >= FirstPromptToolIterationThreshold ||
			currentMetrics.WindowEffectiveToolCalls >= FirstPromptToolCallThreshold {
			return []SkillCreationSignal{{
				SignalType: SkillCreationSignalPromptEligible,
				Metrics:    currentMetrics,
				Reason:     "first_prompt_threshold",
			}}
		}
		return nil
	}

	newIterations := currentMetrics.TotalEffectiveToolCallingIterations - promptedSnapshot[0]
	if newIterations < 0 {
		newIterations = 0
	}
	newCalls := currentMetrics.TotalEffectiveToolCalls - promptedSnapshot[1]
	if newCalls < 0 {
		newCalls = 0
	}
	if newIterations >= RepromptToolIterationThreshold || newCalls >= RepromptToolCallThreshold {
		return []SkillCreationSignal{{
			SignalType: SkillCreationSignalPromptEligible,
			Metrics:    currentMetrics,
			Reason:     "reprompt_threshold",
		}}
	}
	return nil
}

// NormalizeToolName 命名空间工具名归一为基础名。
// Python: normalize_tool_name。
func NormalizeToolName(toolName string) string {
	tool := strings.TrimSpace(toolName)
	if idx := strings.LastIndex(tool, "."); idx >= 0 {
		tool = tool[idx+1:]
	}
	return tool
}

// IsEffectiveTaskTool 工具是否代表任务执行工作。
// Python: is_effective_task_tool。
func IsEffectiveTaskTool(toolName string) bool {
	tool := NormalizeToolName(toolName)
	if tool == "" {
		return false
	}
	if _, excluded := excludedSkillCreationToolNames[tool]; excluded {
		return false
	}
	for _, keyword := range excludedToolKeywords {
		if strings.Contains(tool, keyword) {
			return false
		}
	}
	return true
}

// CountToolCallingIterations 统计请求了至少一个有效任务工具的 LLM 迭代数。
// Python: count_tool_calling_iterations。
func CountToolCallingIterations(traj *trajectory.Trajectory) int {
	if traj == nil {
		return 0
	}
	count := 0
	for _, span := range trajectory.IterSpans(traj) {
		if trajectory.SpanCategory(span) != "llm" {
			continue
		}
		if responseHasEffectiveToolCalls(spanToolCalls(span)) {
			count++
		}
	}
	return count
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// countNewEffectiveToolCallingIterations 统计窗口内新增的有效工具调用迭代。
// Python: _count_new_effective_tool_calling_iterations。
func countNewEffectiveToolCallingIterations(traj *trajectory.Trajectory, effectiveCallIDsAfterWatermark map[string]struct{}, newToolSpans []map[string]any) int {
	if len(newToolSpans) == 0 {
		return 0
	}

	if len(effectiveCallIDsAfterWatermark) > 0 {
		count := 0
		for _, span := range trajectory.IterSpans(traj) {
			if trajectory.SpanCategory(span) != "llm" {
				continue
			}
			if responseHasToolCallID(spanToolCalls(span), effectiveCallIDsAfterWatermark) {
				count++
			}
		}
		return count
	}

	// 无 tool_call_id 关联的轨迹回退：把有效工具步骤计作独立迭代，
	// 不混用原始与有效索引。
	count := 0
	for _, span := range newToolSpans {
		if IsEffectiveTaskTool(spanToolName(span)) {
			count++
		}
	}
	return count
}

// spanToolName 返回工具 span 的规范名（带 name 兜底）。
// Python: _span_tool_name。
func spanToolName(span map[string]any) string {
	toolName := trajectory.ReadToolCall(span)["name"]
	if toolName != nil && fmt.Sprintf("%v", toolName) != "" {
		return fmt.Sprintf("%v", toolName)
	}
	name := fmt.Sprintf("%v", span["name"])
	if strings.HasPrefix(name, "tool.") {
		return name[5:]
	}
	return name
}

// spanToolCallID 返回工具 span 的调用 id（空值返回空串）。
// Python: _span_tool_call_id。
func spanToolCallID(span map[string]any) string {
	value := trajectory.ReadToolCall(span)["id"]
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

// spanToolCalls 从规范 LLM span 只读 completion 的工具调用。
// Python: _span_tool_calls。
func spanToolCalls(span map[string]any) []any {
	_, completions := trajectory.ReadLLMExchange(span)
	for i := len(completions) - 1; i >= 0; i-- {
		message := completions[i]
		if toolCalls, ok := message["tool_calls"].([]any); ok && len(toolCalls) > 0 {
			return toolCalls
		}
	}
	return nil
}

// responseHasEffectiveToolCalls 响应是否含有效任务工具调用。
// Python: _response_has_effective_tool_calls。
func responseHasEffectiveToolCalls(response []any) bool {
	for _, toolCall := range iterSkillCreationToolCalls(response) {
		name := ""
		if v := trajectory.ToolCallName(toolCall); v != nil {
			name = fmt.Sprintf("%v", v)
		}
		if IsEffectiveTaskTool(name) {
			return true
		}
	}
	return false
}

// responseHasToolCallID 响应是否含指定调用 id 的工具调用。
// Python: _response_has_tool_call_id。
func responseHasToolCallID(response []any, toolCallIDs map[string]struct{}) bool {
	for _, toolCall := range iterSkillCreationToolCalls(response) {
		callID := trajectory.ToolCallID(toolCall)
		if callID == nil {
			continue
		}
		if _, ok := toolCallIDs[fmt.Sprintf("%v", callID)]; ok {
			return true
		}
	}
	return false
}

// iterSkillCreationToolCalls 归一响应的工具调用列表。
// Python: _iter_tool_calls。
func iterSkillCreationToolCalls(response []any) []map[string]any {
	result := make([]map[string]any, 0, len(response))
	for _, item := range response {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}
