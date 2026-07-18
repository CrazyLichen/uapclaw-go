package browser_move

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserRuntimeRail 浏览器运行时进度追踪 Rail。
//
// 使直接浏览器会话可恢复和完成感知，在 ReAct 循环中：
//   - BeforeInvoke: 确保运行时就绪 + 注入 MCP 能力 + 恢复进度 + 记录任务文本
//   - BeforeModelCall: 注入进度格式指南和续行上下文 PromptSection
//   - AfterToolCall: 对 browser_ 前缀工具记录进度
//   - AfterInvoke: 提取 <browser_progress> payload；判断完成/失败；持久化进度
//
// 对齐 Python: openjiuwen/harness/tools/browser_move/playwright_runtime/runtime.py (BrowserRuntimeRail L539-808)
type BrowserRuntimeRail struct {
	*sainterfaces.BaseRail
	runtime *BrowserAgentRuntime
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// browserProgressStateKeyStr 会话状态中进度状态的键名字符串
	// 对齐 Python: _BROWSER_PROGRESS_STATE_KEY
	browserProgressStateKeyStr = "__browser_subagent_progress_state__"
	// browserProgressTaskKeyStr 会话状态中任务文本的键名字符串
	// 对齐 Python: _BROWSER_PROGRESS_TASK_KEY
	browserProgressTaskKeyStr = "__browser_subagent_last_task__"
	// browserProgressSectionName 续行上下文 PromptSection 名称
	// 对齐 Python: _BROWSER_PROGRESS_SECTION_NAME
	browserProgressSectionName = "browser_progress_continuation"
	// browserProgressFormatSectionName 格式指南 PromptSection 名称
	// 对齐 Python: _BROWSER_PROGRESS_FORMAT_SECTION_NAME
	browserProgressFormatSectionName = "browser_progress_format"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// browserProgressTagRE 浏览器进度标签正则。
//
// 对齐 Python: _BROWSER_PROGRESS_TAG_RE
var browserProgressTagRE = regexp.MustCompile(
	`<browser_progress>\s*(\{.*?\})\s*</browser_progress>`,
)

// browserProgressFormatGuidance 进度格式指南。
//
// 对齐 Python: _BROWSER_PROGRESS_FORMAT_GUIDANCE
var browserProgressFormatGuidance = map[string]string{
	"en": "When you stop and answer without another browser tool call, append exactly one " +
		"<browser_progress>{...}</browser_progress> JSON block. " +
		"Use status=completed only when the requested browser outcome is evidenced. " +
		"Include compact fields: status, completed_steps, remaining_steps, next_step, " +
		"completion_evidence, missing_requirements.",
	"cn": "当您暂停并回答问题，且未调用其他浏览器工具时，请在后面接上且仅接一个 " +
		"<browser_progress>{...}</browser_progress> JSON 块。" +
		"仅在请求的浏览器结果得到验证时才使用 status=completed。 " +
		"包含以下紧凑字段：status、completed_steps、remaining_steps、next_step、" +
		"completion_evidence、missing_requirements。",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserRuntimeRail 创建浏览器运行时进度追踪 Rail。
//
// 对齐 Python: BrowserRuntimeRail(runtime)
func NewBrowserRuntimeRail(runtime *BrowserAgentRuntime) *BrowserRuntimeRail {
	return &BrowserRuntimeRail{
		BaseRail: sainterfaces.NewBaseRail(),
		runtime:  runtime,
	}
}

// Runtime 返回关联的 BrowserAgentRuntime。
func (r *BrowserRuntimeRail) Runtime() *BrowserAgentRuntime {
	return r.runtime
}

// BeforeInvoke invoke 开始前：确保运行时就绪 + 注入 MCP 能力 + 恢复进度 + 记录任务文本。
//
// 对齐 Python: BrowserRuntimeRail.before_invoke
func (r *BrowserRuntimeRail) BeforeInvoke(ctx context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	if err := r.runtime.EnsureRuntimeReady(ctx); err != nil {
		logger.Warn(logComponentBR).
			Str("event_type", "browser_rail_before_invoke_error").
			Err(err).
			Msg("确保运行时就绪失败")
		return nil
	}

	// 注入 MCP 能力
	r.ensureBrowserMCPAbility(cbc)

	sess := cbc.Session()
	if sess == nil {
		return nil
	}

	// 从 session 恢复进度到 service
	r.hydrateServiceProgressFromSession(sess)

	// 记录任务文本
	taskText := extractQueryFromCBC(cbc)
	if taskText != "" {
		sess.UpdateState(map[string]any{browserProgressTaskKeyStr: taskText})
	}

	return nil
}

// BeforeModelCall LLM 调用前：注入进度格式指南和续行上下文 PromptSection。
//
// 对齐 Python: BrowserRuntimeRail.before_model_call
func (r *BrowserRuntimeRail) BeforeModelCall(_ context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	sess := cbc.Session()
	builder := cbc.Agent().SystemPromptBuilder()
	if sess == nil || builder == nil {
		return nil
	}

	// 注入格式指南 PromptSection
	builder.AddSection(saprompt.PromptSection{
		Name:     browserProgressFormatSectionName,
		Content:  browserProgressFormatGuidance,
		Priority: 84,
	})

	// 加载进度状态
	progressState := r.loadProgressState(sess)
	if progressState.IsEmpty() {
		builder.RemoveSection(browserProgressSectionName)
		return nil
	}

	progressContext := BuildProgressContext(progressState)
	if progressContext == "" {
		builder.RemoveSection(browserProgressSectionName)
		return nil
	}

	continuationTextEN := progressContext + "\n" +
		"Use this stored browser progress as continuation context. " +
		"Avoid repeating completed actions unless recovery requires it."
	continuationTextCN := progressContext + "\n" +
		"将此存储的浏览器进度用作延续上下文。" +
		"除非恢复操作有此需求，否则请避免重复已完成的操作。"

	builder.AddSection(saprompt.PromptSection{
		Name: browserProgressSectionName,
		Content: map[string]string{
			"en": continuationTextEN,
			"cn": continuationTextCN,
		},
		Priority: 83,
	})

	return nil
}

// AfterToolCall 工具执行后：对 browser_ 前缀工具记录进度。
//
// 对齐 Python: BrowserRuntimeRail.after_tool_call
func (r *BrowserRuntimeRail) AfterToolCall(_ context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	sess := cbc.Session()
	if sess == nil {
		return nil
	}

	inputs := cbc.Inputs()
	toolInputs, ok := inputs.(*sainterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	toolName := strings.TrimSpace(toolInputs.ToolName)
	if !isBrowserProgressTool(toolName) {
		return nil
	}

	toolResult := normalizeToolResult(toolInputs.ToolResult)
	sessionID := sess.GetSessionID()

	r.runtime.Service().RecordToolProgress(sessionID, "", toolName, toolResult)
	r.persistServiceProgressToSession(sess)

	return nil
}

// AfterInvoke invoke 完成后：提取 <browser_progress> payload；判断完成/失败；持久化进度。
//
// 对齐 Python: BrowserRuntimeRail.after_invoke
func (r *BrowserRuntimeRail) AfterInvoke(_ context.Context, cbc *sainterfaces.AgentCallbackContext) error {
	sess := cbc.Session()
	inputs := cbc.Inputs()
	if sess == nil {
		return nil
	}

	invokeInputs, ok := inputs.(*sainterfaces.InvokeInputs)
	if !ok {
		return nil
	}

	result := invokeInputs.Result
	if result == nil {
		return nil
	}

	sessionID := sess.GetSessionID()
	r.hydrateServiceProgressFromSession(sess)

	outputText := fmt.Sprintf("%v", result["output"])
	cleanOutput, progressPayload := extractProgressPayload(outputText)
	if cleanOutput != outputText {
		result["output"] = cleanOutput
	}

	if progressPayload != nil {
		parsedProgress := buildProgressResult(progressPayload, cleanOutput)
		r.runtime.Service().RecordWorkerProgress(sessionID, "", parsedProgress)

		progressState := r.runtime.Service().GetProgressState(sessionID)
		exported := r.runtime.Service().ExportProgressState(sessionID)

		if ShouldTreatAsCompleted(parsedProgress) {
			result["result_type"] = "answer"
			result["progress_state"] = exported
			r.clearProgressState(sess)
			return nil
		}

		failureSummary := r.runtime.Service().BuildFailureSummary(
			r.loadTaskText(sess),
			fmt.Sprintf("%v", parsedProgress["error"]),
			progressState.LastPageURL,
			progressState.LastPageTitle,
			cleanOutput,
			progressState.LastScreenshot,
			1,
			progressState,
		)
		result["result_type"] = "error"
		result["failure_summary"] = failureSummary
		result["progress_state"] = exported
		if cleanOutput == "" {
			result["output"] = failureSummary
		} else {
			result["output"] = cleanOutput + "\n\n" + failureSummary
		}
		r.persistServiceProgressToSession(sess)
		return nil
	}

	// 检查是否为最大迭代次数结果
	if isMaxIterationResultFromMap(result) {
		progressState := r.runtime.Service().GetProgressState(sessionID)
		failureSummary := r.runtime.Service().BuildFailureSummary(
			r.loadTaskText(sess),
			"max_iterations_reached",
			progressState.LastPageURL,
			progressState.LastPageTitle,
			railOrEmpty(cleanOutput),
			progressState.LastScreenshot,
			1,
			progressState,
		)
		result["failure_summary"] = failureSummary
		result["progress_state"] = r.runtime.Service().ExportProgressState(sessionID)
		result["output"] = failureSummary
		r.persistServiceProgressToSession(sess)
		return nil
	}

	// 正常完成 → 清理进度
	resultType := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", result["result_type"])))
	if resultType == "answer" {
		r.clearProgressState(sess)
		return nil
	}

	// 其他情况：导出进度
	exported := r.runtime.Service().ExportProgressState(sessionID)
	if exported != nil {
		result["progress_state"] = exported
		r.persistServiceProgressToSession(sess)
	}

	return nil
}

// GetCallbacks 返回已覆盖的钩子方法映射。
func (r *BrowserRuntimeRail) GetCallbacks() map[sainterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(sainterfaces.CallbackBeforeInvoke, r.wrapBeforeInvoke),
		r.CallbackFrom(sainterfaces.CallbackBeforeModelCall, r.wrapBeforeModelCall),
		r.CallbackFrom(sainterfaces.CallbackAfterToolCall, r.wrapAfterToolCall),
		r.CallbackFrom(sainterfaces.CallbackAfterInvoke, r.wrapAfterInvoke),
	)
}

// ExtractProgressPayload 从输出文本中提取 <browser_progress> payload。
// 导出供测试使用。
func ExtractProgressPayload(text string) (string, map[string]any) {
	return extractProgressPayload(text)
}

// BuildProgressResult 从 progress payload 构建进度结果。
// 导出供测试使用。
func BuildProgressResult(payload map[string]any, cleanOutput string) map[string]any {
	return buildProgressResult(payload, cleanOutput)
}

// IsBrowserProgressTool 判断工具名是否为浏览器进度工具。
// 导出供测试使用。
func IsBrowserProgressTool(toolName string) bool {
	return isBrowserProgressTool(toolName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// wrapBeforeInvoke 适配 cb.PerAgentCallbackFunc 签名
func (r *BrowserRuntimeRail) wrapBeforeInvoke(ctx context.Context, agentCallbackContext any) error {
	cbc, ok := agentCallbackContext.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeInvoke(ctx, cbc)
}

// wrapBeforeModelCall 适配 cb.PerAgentCallbackFunc 签名
func (r *BrowserRuntimeRail) wrapBeforeModelCall(ctx context.Context, agentCallbackContext any) error {
	cbc, ok := agentCallbackContext.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.BeforeModelCall(ctx, cbc)
}

// wrapAfterToolCall 适配 cb.PerAgentCallbackFunc 签名
func (r *BrowserRuntimeRail) wrapAfterToolCall(ctx context.Context, agentCallbackContext any) error {
	cbc, ok := agentCallbackContext.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.AfterToolCall(ctx, cbc)
}

// wrapAfterInvoke 适配 cb.PerAgentCallbackFunc 签名
func (r *BrowserRuntimeRail) wrapAfterInvoke(ctx context.Context, agentCallbackContext any) error {
	cbc, ok := agentCallbackContext.(*sainterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	return r.AfterInvoke(ctx, cbc)
}

// ensureBrowserMCPAbility 确保 Agent 能力管理器中包含 MCP 配置。
//
// 对齐 Python: BrowserRuntimeRail._ensure_browser_mcp_ability
func (r *BrowserRuntimeRail) ensureBrowserMCPAbility(cbc *sainterfaces.AgentCallbackContext) {
	agent := cbc.Agent()
	if agent == nil {
		return
	}
	abilityManager := agent.AbilityManager()
	if abilityManager == nil {
		return
	}
	if r.runtime.Service().MCPCfg != nil {
		abilityManager.Add(r.runtime.Service().MCPCfg)
	}
}

// hydrateServiceProgressFromSession 从会话状态恢复进度到 service。
//
// 对齐 Python: BrowserRuntimeRail._hydrate_service_progress_from_session
func (r *BrowserRuntimeRail) hydrateServiceProgressFromSession(sess sessioninterfaces.SessionFacade) *BrowserTaskProgressState {
	sessionID := sess.GetSessionID()
	progressState := r.loadProgressState(sess)
	if progressState.IsEmpty() {
		r.runtime.Service().ClearProgressState(sessionID)
		return progressState
	}
	r.runtime.Service().SetProgressState(sessionID, progressState)
	return progressState
}

// persistServiceProgressToSession 将 service 进度持久化到会话状态。
//
// 对齐 Python: BrowserRuntimeRail._persist_service_progress_to_session
func (r *BrowserRuntimeRail) persistServiceProgressToSession(sess sessioninterfaces.SessionFacade) {
	if sess == nil {
		return
	}
	sessionID := sess.GetSessionID()
	exported := r.runtime.Service().ExportProgressState(sessionID)
	progressState := r.runtime.Service().GetProgressState(sessionID)

	stateValue := map[string]any{}
	if len(exported) > 0 {
		stateValue = exported
	}
	if len(stateValue) == 0 && progressState != nil && !progressState.IsEmpty() {
		stateValue = progressState.ToDict()
	}

	sess.UpdateState(map[string]any{
		browserProgressStateKeyStr: stateValue,
	})
}

// clearProgressState 清除会话中的进度状态。
//
// 对齐 Python: BrowserRuntimeRail._clear_progress_state
func (r *BrowserRuntimeRail) clearProgressState(sess sessioninterfaces.SessionFacade) {
	if sess == nil {
		return
	}
	sessionID := sess.GetSessionID()
	r.runtime.Service().ClearProgressState(sessionID)
	sess.UpdateState(map[string]any{
		browserProgressStateKeyStr: map[string]any{},
		browserProgressTaskKeyStr:  "",
	})
}

// loadProgressState 从会话状态加载进度状态。
//
// 对齐 Python: BrowserRuntimeRail._load_progress_state
func (r *BrowserRuntimeRail) loadProgressState(sess sessioninterfaces.SessionFacade) *BrowserTaskProgressState {
	if sess == nil {
		return &BrowserTaskProgressState{}
	}
	val, err := sess.GetState(state.StringKey(browserProgressStateKeyStr))
	if err != nil || val == nil {
		return &BrowserTaskProgressState{}
	}
	if m, ok := val.(map[string]any); ok {
		return NewBrowserTaskProgressStateFromDict(m)
	}
	return &BrowserTaskProgressState{}
}

// loadTaskText 从会话状态加载任务文本。
//
// 对齐 Python: BrowserRuntimeRail._load_task_text
func (r *BrowserRuntimeRail) loadTaskText(sess sessioninterfaces.SessionFacade) string {
	if sess == nil {
		return ""
	}
	val, err := sess.GetState(state.StringKey(browserProgressTaskKeyStr))
	if err != nil || val == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", val))
}

// extractProgressPayload 从输出文本中提取 <browser_progress> payload。
//
// 对齐 Python: BrowserRuntimeRail._extract_progress_payload
func extractProgressPayload(text string) (string, map[string]any) {
	raw := text
	if raw == "" {
		return raw, nil
	}
	match := browserProgressTagRE.FindStringSubmatch(raw)
	if match == nil {
		return raw, nil
	}
	payloadText := strings.TrimSpace(match[1])
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		return raw, nil
	}
	cleaned := strings.TrimSpace(browserProgressTagRE.ReplaceAllString(raw, ""))
	return cleaned, payload
}

// buildProgressResult 从 progress payload 构建进度结果。
//
// 对齐 Python: BrowserRuntimeRail._build_progress_result
func buildProgressResult(progressPayload map[string]any, cleanOutput string) map[string]any {
	statusRaw := progressPayload["status"]
	status := ""
	if statusRaw != nil {
		status = strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", statusRaw)))
	}
	if status == "" || status == "<nil>" {
		status = "partial"
	}
	errVal := "browser_task_incomplete"
	if status == "completed" {
		errVal = ""
	}
	return map[string]any{
		"ok":       status == "completed",
		"status":   status,
		"progress": progressPayload,
		"final":    cleanOutput,
		"error":    errVal,
	}
}

// isBrowserProgressTool 判断工具名是否为浏览器进度工具。
//
// 对齐 Python: BrowserRuntimeRail._is_browser_progress_tool
func isBrowserProgressTool(toolName string) bool {
	name := strings.TrimSpace(strings.ToLower(toolName))
	if name == "" {
		return false
	}
	// 排除不需要记录进度的工具
	excluded := map[string]bool{
		"browser_cancel_run":          true,
		"browser_clear_cancel":        true,
		"browser_list_custom_actions": true,
		"browser_runtime_health":      true,
	}
	if excluded[name] {
		return false
	}
	return strings.HasPrefix(name, "browser_") || strings.Contains(name, ".browser_")
}

// normalizeToolResult 规范化工具结果。
//
// 对齐 Python: BrowserRuntimeRail._normalize_tool_result
func normalizeToolResult(toolResult any) any {
	if toolResult == nil {
		return toolResult
	}
	// 尝试提取带有 data/success 字段的结果
	m, ok := toolResult.(map[string]any)
	if !ok {
		return toolResult
	}
	if _, hasData := m["data"]; hasData {
		if _, hasSuccess := m["success"]; hasSuccess {
			data := m["data"]
			if data != nil {
				return data
			}
			if errStr, ok := m["error"].(string); ok && strings.TrimSpace(errStr) != "" {
				return map[string]any{"ok": false, "error": errStr}
			}
		}
	}
	return toolResult
}

// isMaxIterationResultFromMap 判断结果是否为最大迭代次数结果。
//
// 对齐 Python: BrowserRuntimeRail._is_max_iteration_result
func isMaxIterationResultFromMap(result map[string]any) bool {
	output := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", result["output"])))
	resultType := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", result["result_type"])))
	return resultType == "error" && strings.Contains(output, strings.ToLower(MaxIterationMessage))
}

// extractQueryFromCBC 从回调上下文中提取查询文本。
func extractQueryFromCBC(cbc *sainterfaces.AgentCallbackContext) string {
	inputs := cbc.Inputs()
	if inputs == nil {
		return ""
	}
	if invokeInputs, ok := inputs.(*sainterfaces.InvokeInputs); ok {
		if invokeInputs.Query != nil {
			return strings.TrimSpace(invokeInputs.Query.PlainText())
		}
	}
	return ""
}

// railOrEmpty 空字符串返回 "(empty)"，否则返回原值。
// browser_rail 包内使用，避免与 service.go 的 orEmpty 冲突
func railOrEmpty(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}
