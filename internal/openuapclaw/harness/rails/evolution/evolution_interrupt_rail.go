package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/evoschema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/interrupt"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// evolutionInterruptRailPriority 审批中断 rail 优先级。
	// Python: EvolutionInterruptRail.priority = 89 (evolution_interrupt_rail.py:35)
	evolutionInterruptRailPriority = 89
	// evolutionInterruptSource 中断来源标识。
	// Python: EVOLUTION_INTERRUPT_SOURCE (evolution_interrupt_rail.py:37)
	evolutionInterruptSource = "evolution_interrupt"
)

// activeEvolutionToolOperations 演进工具名 → 操作类型映射。
// Python: _ACTIVE_EVOLUTION_TOOL_OPERATIONS (evolution_interrupt_rail.py:29-33)
var activeEvolutionToolOperations = map[string]string{
	"evolve_skill_experiences":   "evolve",
	"simplify_skill_experiences": "simplify",
}

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionInterruptRail 活跃技能演进工具的审批中断 rail。
// Python: EvolutionInterruptRail(BaseInterruptRail) (evolution_interrupt_rail.py:35-76)
type EvolutionInterruptRail struct {
	interrupt.BaseInterruptRail
	// reviewRuntime 审查运行时（configure 经 SkillEvolutionRail 绑定；可为 nil）
	// reviewRuntime 审查运行时（configure 经 SkillEvolutionRail 绑定；可为 nil）。
	// review 为本包子包，父→子导入无环（2026-11-23 审查修正：原空结构体占位废除）。
	reviewRuntime *review.EvolutionReviewRuntime
	// submissionService 提交服务（configure 经 SkillEvolutionRail 绑定；可为 nil）。
	// 2026-12-24 15.24b：真实现 ExperienceSubmissionServiceAdapter 随批接线
	//（submission.go 229 行 1:1 移植完成），preflight 对两者 nil 时按 Python
	// 语义报未配置错误。
	submissionService EvolutionSubmissionService
	// autoSave 活跃演进工具是否绕过用户审批
	autoSave bool
	// language 语言标识
	language string
}

// EvolutionSubmissionService 演进提交服务缝。
// 方法签名对齐 Python submission_service.prepare_evolve_submission /
// prepare_simplify_submission；真实现 = ExperienceSubmissionServiceAdapter
// （submission_adapter.go，2026-12-24 15.24b 接线）。
type EvolutionSubmissionService interface {
	// PrepareEvolveSubmission 预检 evolve 提交（对齐 prepare_evolve_submission）。
	PrepareEvolveSubmission(reviewRuntime *review.EvolutionReviewRuntime, evolutionReviewRef string, subject map[string]any, selectedProposalIDs []string, sessionID string) (*review.EvolutionProposalSelection, error)
	// PrepareSimplifySubmission 预检 simplify 提交（对齐 prepare_simplify_submission）。
	PrepareSimplifySubmission(subject map[string]any, actions []map[string]any) error
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 AgentRail 接口实现
var _ agentinterfaces.AgentRail = (*EvolutionInterruptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionInterruptRail 创建审批中断 rail。
// Python: EvolutionInterruptRail.__init__ (evolution_interrupt_rail.py:41-52)
// review_runtime/submission_service 经 Configure 绑定（构造期 nil，对齐 Python 默认值）。
func NewEvolutionInterruptRail() *EvolutionInterruptRail {
	toolNames := make([]string, 0, len(activeEvolutionToolOperations))
	for name := range activeEvolutionToolOperations {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	r := &EvolutionInterruptRail{
		BaseInterruptRail: *interrupt.NewBaseInterruptRail(toolNames...),
		autoSave:          false,
		language:          "cn",
	}
	r.WithPriority(evolutionInterruptRailPriority)
	// Python: interrupt_kind = EVOLUTION_APPROVAL_INTERRUPT_KIND（类属性 → 构造期注入）
	r.SetInterruptKind(saschema.InterruptKindSkillEvolutionApproval)
	r.ResolveInterruptFn = r.resolveInterrupt
	return r
}

// Configure 绑定由 SkillEvolutionRail 注入的运行时依赖。
// Python: EvolutionInterruptRail.configure (evolution_interrupt_rail.py:55-68)
func (r *EvolutionInterruptRail) Configure(reviewRuntime *review.EvolutionReviewRuntime, submissionService EvolutionSubmissionService, autoSave bool, language string) {
	r.reviewRuntime = reviewRuntime
	r.submissionService = submissionService
	r.autoSave = autoSave
	r.language = language
}

// AutoSave 返回活跃演进工具是否绕过用户审批。
// Python: auto_save property (evolution_interrupt_rail.py:70-72)
func (r *EvolutionInterruptRail) AutoSave() bool { return r.autoSave }

// SetAutoSave 覆盖 auto_save。
// Python: auto_save.setter (evolution_interrupt_rail.py:74-76)
func (r *EvolutionInterruptRail) SetAutoSave(value bool) { r.autoSave = value }

// Language 返回语言标识。
func (r *EvolutionInterruptRail) Language() string { return r.language }

// SetLanguage 覆盖语言标识。
func (r *EvolutionInterruptRail) SetLanguage(language string) { r.language = language }

// RenderApprovalMessageForTest 渲染审批消息的测试锚点（生产路径经 resolveInterrupt 内部调用）。
// 形参与 Python _render_approval_message(operation, args, subject, resolved) 对齐，
// 此处仅暴露骨架计数分支；proposal/列表渲染依赖 EvolutionProposalSelection，
// 属 15.24 回填面。
func (r *EvolutionInterruptRail) RenderApprovalMessageForTest(operation string, subjectName string, subjectKind string, count int) string {
	return r.renderApprovalMessage(operation, subjectName, subjectKind, count)
}

// ApprovalUIOptions 返回审批 UI 选项（导出供测试与上游消费）。
// Python: _approval_ui_options (evolution_interrupt_rail.py:253-269)
func (r *EvolutionInterruptRail) ApprovalUIOptions() []map[string]any { return r.approvalUIOptions() }

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveInterrupt 中断解析主入口。
// Python: EvolutionInterruptRail.resolve_interrupt (evolution_interrupt_rail.py:78-114)
func (r *EvolutionInterruptRail) resolveInterrupt(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) interrupt.InterruptDecision {
	toolName := cbcToolName(cbc)
	operation, known := activeEvolutionToolOperations[toolName]
	if !known {
		// Python: _ACTIVE_EVOLUTION_TOOL_OPERATIONS[tool_name] KeyError 上抛；
		// Go ResolveInterruptFn 无 error 通道 → 防御性拒绝（偏差 D-15.21-6）。
		return r.rejectTool(cbc, fmt.Sprintf("unknown evolution tool: %s", toolName), nil)
	}
	var (
		args     map[string]any
		subject  evoschema.EvolutionSubject
		resolved *review.EvolutionProposalSelection
	)
	// Python: try: args=_active_tool_args; subject=...to_payload(); resolved=await _preflight;
	//         self._replace_active_tool_args
	// except Exception: return self._reject_tool(...)
	if err := func() error {
		var err error
		args, err = activeToolArgs(cbc)
		if err != nil {
			return err
		}
		subject, err = r.normalizeSubject(args["subject"])
		if err != nil {
			return err
		}
		resolved, err = r.preflight(operation, args, subject, cbc)
		if err != nil {
			return err
		}
		// Python: self._replace_active_tool_args(ctx, args)（步骤对齐；当前
		// preflight 不修改 args，回写为等价形态）
		replaceActiveToolArgs(cbc, args)
		return nil
	}(); err != nil {
		return r.rejectTool(cbc, err.Error(), r.errorData(toolName, cbc))
	}

	if r.autoSave {
		return r.Approve(nil)
	}

	if userInput != nil {
		return r.resolveUserInput(cbc, toolName, userInput)
	}

	autoConfirmKey := activeAutoConfirmKey(toolName, subject)
	if isActiveAutoConfirmed(autoConfirmConfig, autoConfirmKey) {
		return r.Approve(nil)
	}

	// Python: count = len(resolved.proposals)（evolve）/ len(actions)（simplify）(:187/:217)
	count := len(objectListOrEmpty(args["actions"]))
	if operation == "evolve" && resolved != nil {
		count = len(resolved.Proposals)
	}
	message := r.renderApprovalMessage(operation, subject.Name, subject.Kind, count)
	return r.Interrupt(&saschema.InterruptRequest{
		Message:        message,
		PayloadSchema:  actionPayloadSchema(),
		AutoConfirmKey: autoConfirmKey,
		UIOptions:      r.approvalUIOptions(),
		Metadata:       approvalMetadata(),
	})
}

// preflight 提交预检。
// Python: _preflight (evolution_interrupt_rail.py:116-138)
// evolve 分支返回 submission 服务解析出的 proposal 选择载体（供审批消息渲染）；
// simplify 分支返回 nil。偏差 ⤴️15.24：submission_service 的真实实现随工具链
// 回填，接口实现方负责 evolution_review_ref 解析与动作校验语义。
func (r *EvolutionInterruptRail) preflight(operation string, args map[string]any, subject evoschema.EvolutionSubject, cbc *agentinterfaces.AgentCallbackContext) (*review.EvolutionProposalSelection, error) {
	if r.reviewRuntime == nil || r.submissionService == nil {
		return nil, fmt.Errorf("EvolutionInterruptRail is not configured")
	}
	sessionID := railSessionID(cbc)
	if operation == "evolve" {
		selected := stringListOrEmpty(args["selected_proposal_ids"])
		ref := asEvolutionString(args["evolution_review_ref"])
		return r.submissionService.PrepareEvolveSubmission(r.reviewRuntime, ref, subject.ToPayload(), selected, sessionID)
	}
	actions := objectListOrEmpty(args["actions"])
	return nil, r.submissionService.PrepareSimplifySubmission(subject.ToPayload(), actions)
}

// resolveUserInput 用户输入四分支。
// Python: _resolve_user_input (evolution_interrupt_rail.py:140-166)
func (r *EvolutionInterruptRail) resolveUserInput(cbc *agentinterfaces.AgentCallbackContext, toolName string, userInput any) interrupt.InterruptDecision {
	payload, _ := userInput.(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	action := strings.ToLower(asEvolutionString(payload["action"]))
	switch action {
	case "allow_once":
		return r.Approve(nil)
	case "allow_always":
		r.storeAutoConfirm(cbc, toolName)
		return r.Approve(nil)
	case "reject":
		feedback := asEvolutionString(payload["feedback"])
		if feedback == "" {
			feedback = "evolution tool call rejected by user"
		}
		return r.rejectTool(cbc, feedback, map[string]any{
			"tool_name":    toolName,
			"tool_call_id": activeToolCallID(cbc),
		})
	default:
		return r.Interrupt(&saschema.InterruptRequest{
			Message:       "Invalid evolution approval action.",
			PayloadSchema: actionPayloadSchema(),
			UIOptions:     r.approvalUIOptions(),
			Metadata:      approvalMetadata(),
		})
	}
}

// rejectTool 构造拒绝决策。
// Python: _reject_tool (evolution_interrupt_rail.py:168-181)
// 偏差 D-15.21-4：Python ToolOutput(success=False, error, data) 未移植，Go 以
// map[string]any{"error","data"} 承载；偏差 D-15.21-5：Go ToolMessage 无 name
// 字段，仅填 content/tool_call_id。
func (r *EvolutionInterruptRail) rejectTool(cbc *agentinterfaces.AgentCallbackContext, errText string, data map[string]any) *interrupt.RejectResult {
	toolName := cbcToolName(cbc)
	if data == nil {
		data = map[string]any{"tool_name": toolName}
	}
	toolCallID := activeToolCallID(cbc)
	return &interrupt.RejectResult{
		ToolResult:  map[string]any{"error": errText, "data": data},
		ToolMessage: llmschema.NewToolMessage(toolCallID, errText),
	}
}

// renderApprovalMessage 渲染审批确认消息。
// Python: _render_approval_message (evolution_interrupt_rail.py:183-238)
// 偏差 ⤴️15.24：proposal/明细行渲染依赖 preflight 返回的
// EvolutionProposalSelection，工具链回填后补全；此处渲染标题骨架。
func (r *EvolutionInterruptRail) renderApprovalMessage(operation string, subjectName string, subjectKind string, count int) string {
	if operation == "evolve" {
		if r.isEnglish() {
			return fmt.Sprintf("Approve %d skill evolution experience(s) for `%s` (%s)?", count, subjectName, subjectKind)
		}
		return fmt.Sprintf("是否批准 `%s` (%s) 的 %d 条 Skill 演进经验？", subjectName, subjectKind, count)
	}
	if r.isEnglish() {
		return fmt.Sprintf("Approve %d skill experience simplification action(s) for `%s` (%s)?", count, subjectName, subjectKind)
	}
	return fmt.Sprintf("是否批准 `%s` (%s) 的 %d 项 Skill 经验精简操作？", subjectName, subjectKind, count)
}

// approvalUIOptions 审批 UI 选项（双语）。
// Python: _approval_ui_options (evolution_interrupt_rail.py:253-269)
func (r *EvolutionInterruptRail) approvalUIOptions() []map[string]any {
	if r.isEnglish() {
		return []map[string]any{
			{"label": "Allow Once", "value": "allow_once", "description": "Allow this skill evolution change"},
			{"label": "Always Allow", "value": "allow_always", "description": "Automatically allow future matching skill evolution changes"},
			{"label": "Reject", "value": "reject", "description": "Skip this skill evolution change"},
		}
	}
	return []map[string]any{
		{"label": "本次允许", "value": "allow_once", "description": "允许本次技能演进变更执行"},
		{"label": "总是允许", "value": "allow_always", "description": "自动允许后续匹配的技能演进变更"},
		{"label": "拒绝", "value": "reject", "description": "跳过本次技能演进变更"},
	}
}

// actionPayloadSchema 动作载荷 schema。
// Python: _action_payload_schema (evolution_interrupt_rail.py:271-280)
func actionPayloadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":   map[string]any{"type": "string", "enum": []string{"allow_once", "allow_always", "reject"}},
			"feedback": map[string]any{"type": "string"},
		},
		"required": []string{"action"},
	}
}

// approvalMetadata 中断元数据。
// Python: _approval_metadata (evolution_interrupt_rail.py:282-288)
func approvalMetadata() map[string]any {
	return map[string]any{
		"source":             evolutionInterruptSource,
		"interrupt_kind":     saschema.InterruptKindSkillEvolutionApproval,
		"evolution_approval": true,
	}
}

// activeToolArgs 提取活跃工具参数。
// Python: _active_tool_args (evolution_interrupt_rail.py:290-293)
// Go ToolCallInputs.ToolArgs 为 map 形态（interfaces/callback.go:298 注释），
// Python 侧 str JSON 形态的兼容分支在 Go 侧不可达（登记偏差不移植）。
func activeToolArgs(cbc *agentinterfaces.AgentCallbackContext) (map[string]any, error) {
	inputs, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if inputs == nil {
		return map[string]any{}, nil
	}
	if inputs.ToolArgs == nil {
		return map[string]any{}, nil
	}
	return inputs.ToolArgs, nil
}

// replaceActiveToolArgs 回写活跃工具参数。
// Python: _replace_active_tool_args (evolution_interrupt_rail.py:295-301)
// ⤴️15.24：preflight 返回 EvolutionProposalSelection 后的参数回写语义随
// 提交服务实现接线；当前 args 未被 preflight 修改，回写为无操作等价形态。
func replaceActiveToolArgs(cbc *agentinterfaces.AgentCallbackContext, args map[string]any) {
	inputs, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if inputs == nil {
		return
	}
	serialized, err := json.Marshal(args)
	if err != nil {
		return
	}
	// Python: json.dumps(..., ensure_ascii=False, sort_keys=True) 序列化字符串回写；
	// Go ToolArgs 为 map 形态，直接回写字典（偏差 D-15.21-7：无 str 形态回写语义）。
	var parsed map[string]any
	if json.Unmarshal(serialized, &parsed) == nil {
		inputs.ToolArgs = parsed
	}
}

// activeToolCallID 提取工具调用 ID。
// Python: _active_tool_call_id (evolution_interrupt_rail.py:303-312)
func activeToolCallID(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if inputs == nil {
		return ""
	}
	if inputs.ToolCall != nil && inputs.ToolCall.ID != "" {
		return inputs.ToolCall.ID
	}
	if inputs.ToolName != "" {
		return inputs.ToolName
	}
	return ""
}

// activeAutoConfirmKey 构造自动确认键。
// Python: _active_auto_confirm_key (evolution_interrupt_rail.py:314-316)
func activeAutoConfirmKey(toolName string, subject evoschema.EvolutionSubject) string {
	return fmt.Sprintf("evolution:%s:%s:%s", toolName, subject.Kind, subject.Name)
}

// isActiveAutoConfirmed 检查自动确认配置命中。
// Python: _is_auto_confirmed (evolution_interrupt_rail.py:318-321)
func isActiveAutoConfirmed(autoConfirmConfig map[string]any, key string) bool {
	if autoConfirmConfig == nil {
		return false
	}
	v, ok := autoConfirmConfig[key]
	return ok && v == true
}

// storeAutoConfirm 写入自动确认配置。
// Python: _store_auto_confirm (evolution_interrupt_rail.py:323-333)
func (r *EvolutionInterruptRail) storeAutoConfirm(cbc *agentinterfaces.AgentCallbackContext, toolName string) {
	sess := cbc.Session()
	if sess == nil {
		return
	}
	args, err := activeToolArgs(cbc)
	if err != nil {
		return
	}
	subject, err := r.normalizeSubject(args["subject"])
	if err != nil {
		return
	}
	config := make(map[string]any)
	if raw, err := sess.GetState(sessionstate.StringKey(saschema.InterruptAutoConfirmKey)); err == nil && raw != nil {
		if existing, ok := raw.(map[string]any); ok {
			config = existing
		}
	}
	config[activeAutoConfirmKey(toolName, subject)] = true
	sess.UpdateState(map[string]any{saschema.InterruptAutoConfirmKey: config})
}

// railSessionID 提取会话 ID。
// Python: _session_id (evolution_interrupt_rail.py:335-342)
func railSessionID(cbc *agentinterfaces.AgentCallbackContext) string {
	sess := cbc.Session()
	if sess != nil {
		if sessionID := sess.GetSessionID(); sessionID != "" {
			return sessionID
		}
	}
	// 偏差 D-15.21-8：Python inputs.conversation_id 兜底在 Go ToolCallInputs
	// 无对应字段（会话标识统一走 SessionFacade.GetSessionID），回退空串。
	return ""
}

// isEnglish 语言是否英文。
// Python: _is_english (evolution_interrupt_rail.py:344-345)
func (r *EvolutionInterruptRail) isEnglish() bool {
	return strings.ToLower(r.language) == "en"
}

// IsEnglish 导出语言判定（测试锚点）。
func (r *EvolutionInterruptRail) IsEnglish() bool { return r.isEnglish() }

// normalizeSubject subject 归一化（要求已配置）。
// Python: _normalize_subject (evolution_interrupt_rail.py:347-350)
func (r *EvolutionInterruptRail) normalizeSubject(subject any) (evoschema.EvolutionSubject, error) {
	if r.reviewRuntime == nil || r.submissionService == nil {
		return evoschema.EvolutionSubject{}, fmt.Errorf("EvolutionInterruptRail is not configured")
	}
	return evoschema.NormalizeSubject(subject, nil)
}

// errorData 错误数据载荷。
// Python: _error_data (evolution_interrupt_rail.py:352-359)
func (r *EvolutionInterruptRail) errorData(toolName string, cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	args, _ := activeToolArgs(cbc)
	return map[string]any{
		"tool_name":             toolName,
		"evolution_review_ref":  asEvolutionString(args["evolution_review_ref"]),
		"selected_proposal_ids": stringListOrEmpty(args["selected_proposal_ids"]),
	}
}

// ──────────────────────────── 包级辅助 ────────────────────────────

// textOr 返回首个非空文本。
// Python: _text_or (evolution_interrupt_rail.py:239-245)
func textOr(values ...string) string {
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			return text
		}
	}
	return ""
}

// previewText 预览截断（默认 240 字符）。
// Python: _preview (evolution_interrupt_rail.py:247-251)
func previewText(value string) string {
	text := strings.TrimSpace(value)
	limit := 240
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimRight(string(runes[:limit]), " \t\n") + "..."
}

// asEvolutionString 宽松转字符串（Python str(v) or "" 等价）。
func asEvolutionString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// stringListOrEmpty 宽松转 []string。
func stringListOrEmpty(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, asEvolutionString(item))
	}
	return out
}

// objectListOrEmpty 宽松转 []map[string]any（保留非 dict 项过滤语义）。
// Python: [item for item in ... if isinstance(item, dict)]（simplify 渲染 :215）
func objectListOrEmpty(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// cbcToolName 提取工具名。
func cbcToolName(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if inputs == nil {
		return ""
	}
	return inputs.ToolName
}
