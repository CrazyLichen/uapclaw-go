// runtime_adapters.go 提供 RSI 对 Agent Core 与 Harness 公共 API 的自有适配器。
// Python: openjiuwen/rsi/harness_rsi/evaluator/runtime_adapters.py（495 行）。
// 已落地：RSIBashTool/RSISysOperationRail（17.4b1）、RSITrajectoryRail（25.2b
// 兑现）、run_agent_with_empty_response_recovery（17.6a）；RSISkillUseRail ⤴️
// 17.6a 后续批次（协议体量大，单独一批）。

package evaluator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/skills"
	hprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	evolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	skillsrail "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
	skilltools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/skills"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent RSI evaluator 日志组件。
const logComponent = logger.ComponentAgentCore

// pipefailPrefix bash 管道失败前缀。
// Python: runtime_adapters.py:99 "set -o pipefail;"
const pipefailPrefix = "set -o pipefail;"

// ──────────────────────────── 结构体 ────────────────────────────

// RSIBashToolDecorator 保序 bash 工具装饰器：管道中保留生产者失败。
// Python: runtime_adapters.py:86-102 RSIBashTool（仅覆写 invoke；pipefail=false
// 时行为与基类 BashTool 完全一致）。
type RSIBashToolDecorator struct {
	// Wrapped 被装饰的基类 bash 工具
	Wrapped tool.Tool
	// pipefail 是否启用 pipefail 前缀与 shell_type 强制
	pipefail bool
}

// NewRSIBashToolDecorator 包装基类 bash 工具。
// Python: runtime_adapters.py:89-91 RSIBashTool.__init__
func NewRSIBashToolDecorator(wrapped tool.Tool, pipefail bool) *RSIBashToolDecorator {
	return &RSIBashToolDecorator{Wrapped: wrapped, pipefail: pipefail}
}

// Card 透传工具卡片。
func (d *RSIBashToolDecorator) Card() *tool.ToolCard { return d.Wrapped.Card() }

// Stream 透传流式调用（Python 未覆写 stream）。
func (d *RSIBashToolDecorator) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return d.Wrapped.Stream(ctx, inputs, opts...)
}

// GetCallTimeout 透传调用预算（Python 未覆写 get_call_timeout）。
func (d *RSIBashToolDecorator) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return d.Wrapped.GetCallTimeout(inputs, defaultTimeout)
}

// Invoke pipefail 启用时为命令注入 pipefail 前缀并强制 bash shell。
// Python: runtime_adapters.py:93-102 RSIBashTool.invoke
func (d *RSIBashToolDecorator) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	if !d.pipefail {
		return d.Wrapped.Invoke(ctx, inputs, opts...)
	}

	// Python: adapted = dict(inputs)
	adapted := make(map[string]any, len(inputs))
	for key, value := range inputs {
		adapted[key] = value
	}
	// Python: command = str(adapted.get("command", "") or "").strip()
	command := ""
	if raw, ok := adapted["command"]; ok {
		if str, isStr := raw.(string); isStr {
			command = strings.TrimSpace(str)
		}
	}
	// Python: if command and not command.startswith("set -o pipefail;"): adapted["command"] = f"set -o pipefail; {command}"
	if command != "" && !strings.HasPrefix(command, pipefailPrefix) {
		adapted["command"] = pipefailPrefix + " " + command
	}
	// Python: adapted["shell_type"] = "bash"
	adapted["shell_type"] = "bash"
	return d.Wrapped.Invoke(ctx, adapted, opts...)
}

// RSISysOperationRail 以 RSI 评估策略挂载上游系统操作工具。
// Python: runtime_adapters.py:105-167 RSISysOperationRail（继承 SysOperationRail，
// 覆写 init；Go 经基类扩展点 shellOnly/allowShell/bashDecorator 表达覆写差异）。
type RSISysOperationRail struct {
	rails.SysOperationRail
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRSISysOperationRail 创建 RSI 系统操作护栏。
// Python: runtime_adapters.py:108-119 RSISysOperationRail.__init__
// （shell_only/bash_pipefail/allow_shell 为 RSI 专属；其余 kwargs 透传基类）。
func NewRSISysOperationRail(shellOnly, bashPipefail, allowShell bool, baseOpts ...rails.SysOperationRailOption) *RSISysOperationRail {
	opts := []rails.SysOperationRailOption{
		rails.WithShellOnly(shellOnly),
		rails.WithAllowShell(allowShell),
	}
	if bashPipefail {
		opts = append(opts, rails.WithBashToolDecorator(func(wrapped tool.Tool) tool.Tool {
			return NewRSIBashToolDecorator(wrapped, true)
		}))
	}
	opts = append(opts, baseOpts...)
	return &RSISysOperationRail{SysOperationRail: *rails.NewSysOperationRail(opts...)}
}

// ──────────────────────────── RSITrajectoryRail（17.6a / 25.2b 兑现） ────────────────────────────

// RSITrajectoryRail 保持采集质量对评估证据消费方可见的轨迹轨道。
// Python: runtime_adapters.py:46-84 RSITrajectoryRail(TrajectoryRail)——
// capture_issues 采集 + model/tool 调用计数。Go 侧基座计数与 issues 经
// CaptureIssueObserver 可选接口回调，本轨道以 ext 包装实现观察
// （与 EvolutionRail ext 设计同源偏差，Python 为方法覆写）。
// 生产消费方：per-case 归档轨迹进诊断库（25.2b 登记），供 17.4b1 读件层读证据。
type RSITrajectoryRail struct {
	*evolution.TrajectoryRail
	// issuesMu 采集质量问题集合锁
	issuesMu sync.Mutex
	// captureIssues 采集质量问题码集合（Python self.capture_issues set[str]）
	captureIssues map[string]struct{}
	// modelCallCount 模型调用计数（Python self.model_call_count）
	modelCallCount int
	// toolCallCount 工具调用计数（Python self.tool_call_count）
	toolCallCount int
}

// rsiTrajectoryExt RSITrajectoryRail 的 ext 包装：issues 观察 + 计数，
// 其余委托 TrajectoryRail 自带的归档包装。
type rsiTrajectoryExt struct {
	inner evolution.EvolutionExtension
	rail  *RSITrajectoryRail
}

var _ evolution.EvolutionExtension = (*rsiTrajectoryExt)(nil)
var _ evolution.CaptureIssueObserver = (*rsiTrajectoryExt)(nil)

// NewRSITrajectoryRail 创建 RSI 轨迹轨道。
// Python: runtime_adapters.py:50-62 __init__（trajectory_span_processor/
// trajectory_store 注入——Go 经 TrajectoryRail 选项承载，见文件头偏差登记）。
func NewRSITrajectoryRail(opts ...evolution.EvolutionRailOption) *RSITrajectoryRail {
	inner := evolution.NewTrajectoryRail(opts...)
	rail := &RSITrajectoryRail{
		TrajectoryRail: inner,
		captureIssues:  map[string]struct{}{},
	}
	inner.SetExtension(&rsiTrajectoryExt{inner: inner.GetExtension(), rail: rail})
	return rail
}

// ObserveCaptureIssues 采集 drain 的质量问题码（Python: _drain_for_hook
// self.capture_issues.update(str(issue.get("code", "capture_error")))）。
func (e *rsiTrajectoryExt) ObserveCaptureIssues(issues []map[string]any) {
	e.rail.issuesMu.Lock()
	defer e.rail.issuesMu.Unlock()
	for _, issue := range issues {
		code, _ := issue["code"].(string)
		if code == "" {
			code = "capture_error"
		}
		e.rail.captureIssues[code] = struct{}{}
	}
}

// OnAfterModelCall 模型调用计数后委托内层（Python: after_model_call 计数+super）。
func (e *rsiTrajectoryExt) OnAfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	e.rail.issuesMu.Lock()
	e.rail.modelCallCount++
	e.rail.issuesMu.Unlock()
	return e.inner.OnAfterModelCall(ctx, cbc)
}

// OnAfterToolCall 工具调用计数后委托内层（Python: after_tool_call 计数+super）。
func (e *rsiTrajectoryExt) OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	e.rail.issuesMu.Lock()
	e.rail.toolCallCount++
	e.rail.issuesMu.Unlock()
	return e.inner.OnAfterToolCall(ctx, cbc)
}

// 其余扩展点委托内层（归档行为由 TrajectoryRail 自带包装承载）。
func (e *rsiTrajectoryExt) OnBeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return e.inner.OnBeforeInvoke(ctx, cbc)
}

func (e *rsiTrajectoryExt) OnAfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return e.inner.OnAfterInvoke(ctx, cbc)
}

func (e *rsiTrajectoryExt) OnAfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return e.inner.OnAfterTaskIteration(ctx, cbc)
}

func (e *rsiTrajectoryExt) OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error {
	return e.inner.OnAfterEvolutionTriggered(ctx, traj, cbc)
}

func (e *rsiTrajectoryExt) AllowEvolutionTrigger(trigger evolution.EvolutionTriggerPoint, cbc *agentinterfaces.AgentCallbackContext) bool {
	return e.inner.AllowEvolutionTrigger(trigger, cbc)
}

func (e *rsiTrajectoryExt) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *evolution.EvolutionSnapshot {
	return e.inner.SnapshotForEvolution(ctx, traj, cbc)
}

func (e *rsiTrajectoryExt) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *evolution.EvolutionSnapshot) error {
	return e.inner.RunEvolution(ctx, traj, snapshot)
}

func (e *rsiTrajectoryExt) GetEvolutionTotalTimeoutSecs() float64 {
	return e.inner.GetEvolutionTotalTimeoutSecs()
}

// CaptureIssues 采集质量问题码快照（评估证据消费方读取）。
func (r *RSITrajectoryRail) CaptureIssues() []string {
	r.issuesMu.Lock()
	defer r.issuesMu.Unlock()
	codes := make([]string, 0, len(r.captureIssues))
	for code := range r.captureIssues {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// ModelCallCount 模型调用计数。
func (r *RSITrajectoryRail) ModelCallCount() int {
	r.issuesMu.Lock()
	defer r.issuesMu.Unlock()
	return r.modelCallCount
}

// ToolCallCount 工具调用计数。
func (r *RSITrajectoryRail) ToolCallCount() int {
	r.issuesMu.Lock()
	defer r.issuesMu.Unlock()
	return r.toolCallCount
}

// ──────────────────────────── 空响应恢复（17.6a） ────────────────────────────

// AgentInvokeFunc 单次 agent 调用的注入缝（绑定 agent/session 后的 Runner 调用）。
// 偏差登记：Python run_agent_with_empty_response_recovery(agent, inputs, *, session)
// 内部直接调 Runner.run_agent；Go Runner.RunAgent 签名含 AgentRef/SessionRef/
// modelCtx/envs，闭包注入以对齐调用方装配（17.6b case_backend 接线）。
type AgentInvokeFunc func(ctx context.Context, inputs map[string]any) (map[string]any, error)

// RunAgentWithEmptyResponseRecovery 无可行动输出时重跑一次 RSI 评估 invoke。
// Python: runtime_adapters.py:447-470 run_agent_with_empty_response_recovery。
func RunAgentWithEmptyResponseRecovery(ctx context.Context, invoke AgentInvokeFunc,
	inputs map[string]any) (map[string]any, error) {
	response, err := invoke(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if responseText(response) != "" {
		return response, nil
	}
	logger.Warn(logComponent).Msg("[RSI evaluator] recovering once from an empty agent response")
	return invoke(ctx, map[string]any{
		"query": ("[RECOVERY] The previous turn produced no answer or tool action. " +
			"Do not restart the analysis. Apply the smallest justified workspace " +
			"edit now, then run the acceptance probe."),
	})
}

// responseText 递归抽取响应文本（Python: runtime_adapters.py:473-487
// _response_text——output/content/response/result 四键递归，dict 无命中键时
// JSON 序列化，None→空串）。
func responseText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		if v == nil {
			// Python None 等价物（类型化 nil map 不落入 dict 键循环）。
			return ""
		}
		recognized := false
		for _, key := range []string{"output", "content", "response", "result"} {
			if inner, exists := v[key]; exists {
				recognized = true
				if text := responseText(inner); text != "" {
					return text
				}
			}
		}
		if !recognized {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Sprint(v)
			}
			return string(raw)
		}
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// ──────────────────────────── RSISkillUseRail（17.6a） ────────────────────────────

const (
	// activeSkillSection 活动技能附件节名（Python: _ACTIVE_SKILL_SECTION）。
	activeSkillSection = "rsi.active_skill"
	// activeSkillSectionsKey ctx.extra 内跟踪的活动节键（Python: "_rsi_active_skill_sections"）。
	activeSkillSectionsKey = "_rsi_active_skill_sections"
	// activeSkillSource 附件来源（Python add_section source="rsi.skill_delivery"）。
	activeSkillSource = "rsi.skill_delivery"
	// activeSkillPriority 附件优先级（Python add_section priority=35）。
	activeSkillPriority = 35
)

// RSISkillUseRail 把上游 SkillUseRail 适配到 RSI 的度量交付协议。
// Python: runtime_adapters.py:170-446 RSISkillUseRail(SkillUseRail)——
// 任务启动路由触发 + skill_tool SKILL.md 载入的活动技能附件协议。
// 偏差登记：Python 为子类覆写；Go Final 回调映射下经 GetCallbacks 覆盖注册
// 本轨道的 BeforeInvoke/AfterInvoke/AfterToolCall（与 AgentModeRail 同款）。
type RSISkillUseRail struct {
	*skillsrail.SkillUseRail
	// triggerAtTaskStart 任务启动即触发技能路由（Python self.trigger_at_task_start）
	triggerAtTaskStart bool
	// runtimeSkillTool 运行时技能工具（Python self._runtime_skill_tool）
	runtimeSkillTool *skilltools.SkillTool
	// evidenceMu 证据字段锁
	evidenceMu sync.Mutex
	// taskTriggerEvidence 任务触发证据（Python self._task_trigger_evidence）
	taskTriggerEvidence map[string]any
	// taskSkillLoads 任务内技能载入记录（Python self._task_skill_loads）
	taskSkillLoads []map[string]any
}

// NewRSISkillUseRail 创建 RSI 技能使用护栏。
// Python: runtime_adapters.py:173-182 __init__（trigger_at_task_start=False 缺省，
// 其余 kwargs 透传基类）。
func NewRSISkillUseRail(triggerAtTaskStart bool, baseOpts ...skillsrail.SkillUseRailOption) *RSISkillUseRail {
	return &RSISkillUseRail{
		SkillUseRail:        skillsrail.NewSkillUseRail(nil, baseOpts...),
		triggerAtTaskStart:  triggerAtTaskStart,
		taskTriggerEvidence: map[string]any{},
	}
}

// Init 初始化基类并构建运行时技能工具。
// Python: runtime_adapters.py:184-201 init。
func (r *RSISkillUseRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if err := r.SkillUseRail.Init(ctx, agent); err != nil {
		return err
	}
	// Python: lang = agent.system_prompt_builder.language; agent_id = card.id
	lang := r.LanguageName()
	agentID := ""
	if agent != nil {
		if card := agent.Card(); card != nil {
			agentID = card.GetID()
		}
	}
	// Python: build_read_image_multimodal_resolver(agent)——Go 侧经 NewSkillTool
	// 的布尔回调承载；nil 回落静态 True（与工具缺省一致）。
	r.runtimeSkillTool = skilltools.NewSkillTool(
		r.SysOperation(),
		func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill {
			return r.GetSkillsForSession(sess)
		},
		lang, agentID, nil,
	)
	return nil
}

// GetCallbacks 注册本轨道的 BeforeInvoke/AfterInvoke/AfterToolCall 覆盖。
// Python: before_invoke/after_invoke/after_tool_call 覆写。
func (r *RSISkillUseRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]callback.PerAgentCallbackFunc {
	callbacks := r.SkillUseRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterInvoke] = func(ctx context.Context, railCtx any) error {
		return r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// BeforeInvoke 基类初始化后触发任务启动技能路由。
// Python: runtime_adapters.py:203-206 before_invoke。
func (r *RSISkillUseRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if err := r.SkillUseRail.BeforeInvoke(ctx, cbc); err != nil {
		return err
	}
	return r.triggerRelevantSkill(ctx, cbc)
}

// AfterInvoke 清活动技能附件后委托基类。
// Python: runtime_adapters.py:207-210 after_invoke。
func (r *RSISkillUseRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.clearActiveSkillAttachment(cbc)
	return r.SkillUseRail.AfterInvoke(ctx, cbc)
}

// AfterToolCall skill_tool SKILL.md 载入的活动技能附件协议（无 super 调用，
// 非 skill_tool 直接返回——Python L219-249 同语义）。
func (r *RSISkillUseRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	if inputs.ToolName != "skill_tool" {
		return nil
	}
	toolArgs := normalizeToolArgs(inputs.ToolArgs)
	relativePath, _ := toolArgs["relative_file_path"].(string)
	if strings.TrimSpace(relativePath) == "" {
		relativePath = "SKILL.md"
	}
	if relativePath != "SKILL.md" {
		return nil
	}
	success := false
	var data map[string]any
	switch result := inputs.ToolResult.(type) {
	case map[string]any:
		// Python: isinstance(result, dict) → success/data 取字典值
		if s, ok := result["success"].(bool); ok {
			success = s
		}
		data, _ = result["data"].(map[string]any)
	}
	if !success || data == nil {
		return nil
	}
	skillContent, _ := data["skill_content"].(string)
	skillContent = strings.TrimSpace(skillContent)
	if skillContent == "" || r.AttachmentManagerAccessor() == nil {
		return nil
	}
	skillName, _ := toolArgs["skill_name"].(string)
	if strings.TrimSpace(skillName) == "" {
		skillName = "selected skill"
	}
	decisionCapsule := extractDecisionCapsule(skillContent)
	activeContent := decisionCapsule
	if activeContent == "" {
		activeContent = skillContent
	}
	memo := buildActiveSkillAttachment(skillName, activeContent, decisionCapsule != "")
	writer := r.AttachmentManagerAccessor().BindContext(cbc)
	if writer.SessionID() == "" {
		return nil
	}
	// Python: section = f"{_ACTIVE_SKILL_SECTION}:{sha256(skill_name)[:16]}"
	section := activeSkillSection + ":" + fmt.Sprintf("%x", sha256.Sum256([]byte(skillName)))[:16]
	_, err := writer.AddSection(section, memo, hprompts.AttachmentSkill, activeSkillSource,
		hprompts.WithAttachmentPriority(activeSkillPriority),
		hprompts.WithAttachmentMetadata(map[string]any{
			"skill_name":           skillName,
			"skill_content_chars":  len(skillContent),
			"skill_content_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(skillContent))),
			"delivery_mode":        deliveryMode(decisionCapsule != ""),
		}),
	)
	if err != nil {
		return err
	}
	// Python: ctx.extra.setdefault("_rsi_active_skill_sections", []).append(section)
	extra := cbc.Extra()
	if extra == nil {
		extra = map[string]any{}
	}
	sections, _ := extra[activeSkillSectionsKey].([]string)
	for _, existing := range sections {
		if existing == section {
			return nil
		}
	}
	extra[activeSkillSectionsKey] = append(sections, section)
	for key, value := range extra {
		cbc.SetExtra(key, value)
	}
	return nil
}

// triggerRelevantSkill 任务启动技能路由（Python: runtime_adapters.py:289-346
// _trigger_relevant_skill——守卫链顺序逐条对齐，不得调换）。
func (r *RSISkillUseRail) triggerRelevantSkill(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.evidenceMu.Lock()
	r.taskSkillLoads = []map[string]any{}
	r.taskTriggerEvidence = map[string]any{
		"mode":                "task_start_metadata_trigger",
		"attempted":           false,
		"selected_skill_name": "",
		"delivered":           false,
		"reason":              "disabled",
	}
	evidence := r.taskTriggerEvidence
	r.evidenceMu.Unlock()

	_ = evidence
	if !r.triggerAtTaskStart {
		return nil
	}
	if r.ListSkillModelAccessor() == nil {
		r.setEvidence("reason", "routing_model_unavailable")
		return nil
	}
	if len(r.SkillsMeta()) == 0 {
		r.setEvidence("reason", "no_available_skills")
		return nil
	}
	if r.runtimeSkillTool == nil {
		r.setEvidence("reason", "skill_tool_unavailable")
		return nil
	}
	query := taskQuery(cbcInputsQuery(cbc))
	if query == "" {
		r.setEvidence("reason", "empty_task")
		return nil
	}
	r.setEvidence("attempted", true)
	selector := skilltools.NewListSkillTool(
		func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill {
			return r.GetSkillsForSession(sess)
		},
		r.ListSkillModelAccessor(),
		r.LanguageName(),
		agentIDOf(cbc),
	)
	result, err := selector.Invoke(context.Background(), map[string]any{"query": query})
	if err != nil || result["success"] != true {
		errText := ""
		if err != nil {
			errText = err.Error()
		} else if e, ok := result["error"].(string); ok {
			errText = e
		}
		r.setEvidence("reason", "routing_failed")
		r.setEvidence("error", errText)
		return nil
	}
	data, _ := result["data"].(map[string]any)
	rawNames, _ := data["selected_skill_names"].([]any)
	selected := []string{}
	seen := map[string]struct{}{}
	for _, raw := range rawNames {
		name := strings.TrimSpace(fmt.Sprint(raw))
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		selected = append(selected, name)
	}
	if len(selected) == 0 {
		r.setEvidence("reason", "no_relevant_skill")
		return nil
	}
	for _, skillName := range selected {
		perSkill := map[string]any{}
		for k, v := range r.snapshotEvidence() {
			perSkill[k] = v
		}
		perSkill["selected_skill_name"] = skillName
		r.evidenceMu.Lock()
		r.taskSkillLoads = append(r.taskSkillLoads, perSkill)
		r.evidenceMu.Unlock()
		r.loadTaskStartSkill(cbc, skillName, perSkill)
	}
	// Python: self._task_trigger_evidence = dict(self._task_skill_loads[0])
	r.evidenceMu.Lock()
	if len(r.taskSkillLoads) > 0 {
		first := map[string]any{}
		for k, v := range r.taskSkillLoads[0] {
			first[k] = v
		}
		r.taskTriggerEvidence = first
	}
	r.evidenceMu.Unlock()
	return nil
}

// loadTaskStartSkill 经运行时技能工具载入任务启动技能。
// Python: runtime_adapters.py:348-368 _load_task_start_skill。
func (r *RSISkillUseRail) loadTaskStartSkill(cbc *agentinterfaces.AgentCallbackContext,
	skillName string, evidence map[string]any) {
	loadArgs := map[string]any{"skill_name": skillName, "relative_file_path": "SKILL.md"}
	loadResult, err := r.runtimeSkillTool.Invoke(context.Background(), loadArgs)
	if err != nil || loadResult["success"] != true {
		errText := ""
		if err != nil {
			errText = err.Error()
		} else if e, ok := loadResult["error"].(string); ok {
			errText = e
		}
		evidence["reason"] = "skill_load_failed"
		evidence["error"] = errText
		return
	}
	// Python: await self.after_tool_call(AgentCallbackContext(..., extra=ctx.extra))
	synthetic := &agentinterfaces.AgentCallbackContext{}
	synthetic.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName:   "skill_tool",
		ToolArgs:   loadArgs,
		ToolResult: loadResult,
	})
	for key, value := range cbc.Extra() {
		synthetic.SetExtra(key, value)
	}
	if err := r.AfterToolCall(context.Background(), synthetic); err != nil {
		logger.Warn(logComponent).Err(err).Msg("task-start skill attachment failed")
	}
	evidence["delivered"] = true
	evidence["reason"] = "loaded"
	logger.Info(logComponent).Str("skill_name", skillName).
		Msg("[RSISkillUseRail] task-start Skill loaded")
}

// TaskTriggerEvidence 任务触发证据快照。
// Python: runtime_adapters.py:370-372 task_trigger_evidence。
func (r *RSISkillUseRail) TaskTriggerEvidence() map[string]any {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	snapshot := map[string]any{}
	for k, v := range r.taskTriggerEvidence {
		snapshot[k] = v
	}
	return snapshot
}

// TaskTriggerRecords 每个被选中技能保留一条既有格式结果。
// Python: runtime_adapters.py:374-378 task_trigger_records。
func (r *RSISkillUseRail) TaskTriggerRecords() []map[string]any {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	if len(r.taskSkillLoads) == 0 {
		snapshot := map[string]any{}
		for k, v := range r.taskTriggerEvidence {
			snapshot[k] = v
		}
		return []map[string]any{snapshot}
	}
	records := make([]map[string]any, 0, len(r.taskSkillLoads))
	for _, record := range r.taskSkillLoads {
		cp := map[string]any{}
		for k, v := range record {
			cp[k] = v
		}
		records = append(records, cp)
	}
	return records
}

// clearActiveSkillAttachment 清活动技能附件节（含 ctx.extra 跟踪的逐会话节）。
// Python: runtime_adapters.py:424-433 _clear_active_skill_attachment。
func (r *RSISkillUseRail) clearActiveSkillAttachment(cbc *agentinterfaces.AgentCallbackContext) {
	manager := r.AttachmentManagerAccessor()
	if manager == nil {
		return
	}
	writer := manager.BindContext(cbc)
	if writer.SessionID() == "" {
		return
	}
	_, _ = writer.ClearSection(activeSkillSection)
	extra := cbc.Extra()
	if extra != nil {
		if sections, ok := extra[activeSkillSectionsKey].([]string); ok {
			for _, section := range sections {
				_, _ = writer.ClearSection(section)
			}
		}
		delete(extra, activeSkillSectionsKey)
	}
}

// ──────────────────────────── 非导出助手（Python 静态方法） ────────────────────────────

// taskQuery 任务查询规范化（Python: runtime_adapters.py:380-388 _task_query——
// dict 时 JSON 序列化 sort_keys）。
func taskQuery(raw any) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		payload, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(payload)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// normalizeToolArgs 工具参数规范化（Python: runtime_adapters.py:390-397
// _normalize_tool_args——dict 原样 / JSON 字符串解析 / 其余空 dict）。
func normalizeToolArgs(raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		return m
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

// extractDecisionCapsule 提取 "## decision capsule" 节（含节头，至下一 "## "）。
// Python: runtime_adapters.py:399-413 _extract_decision_capsule。
func extractDecisionCapsule(content string) string {
	lines := strings.Split(content, "\n")
	start := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(line), "## decision capsule") {
			start = index
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		if strings.HasPrefix(lines[index], "## ") {
			end = index
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

// buildActiveSkillAttachment 构造活动技能附件备忘。
// Python: runtime_adapters.py:415-422 _build_active_skill_attachment（文案逐字）。
func buildActiveSkillAttachment(skillName, content string, decisionCapsule bool) string {
	sourceLabel := "Skill instructions"
	if decisionCapsule {
		sourceLabel = "decision capsule"
	}
	return fmt.Sprintf(
		"# Active Skill: %s\n\n"+
			"This skill was already loaded successfully. Keep its requirements active "+
			"through diagnosis, editing, and verification. Do not reload it; act on "+
			"the %s below.\n\n%s",
		skillName, sourceLabel, strings.TrimSpace(content),
	)
}

// deliveryMode 交付模式标识（Python: "decision_capsule" / "full_fallback"）。
func deliveryMode(decisionCapsule bool) string {
	if decisionCapsule {
		return "decision_capsule"
	}
	return "full_fallback"
}

// setEvidence 更新触发证据字段。
func (r *RSISkillUseRail) setEvidence(key string, value any) {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	r.taskTriggerEvidence[key] = value
}

// snapshotEvidence 触发证据快照（持锁外拷贝）。
func (r *RSISkillUseRail) snapshotEvidence() map[string]any {
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	snapshot := map[string]any{}
	for k, v := range r.taskTriggerEvidence {
		snapshot[k] = v
	}
	return snapshot
}

// cbcInputsQuery 从 invoke inputs 取 query 字段。
// Python: getattr(ctx.inputs, "query", "")——Go InvokeInputs.Query 为
// InvokeQuery 接口（InvokeQueryString / *InteractiveInput 两形态）。
func cbcInputsQuery(cbc *agentinterfaces.AgentCallbackContext) any {
	if cbc == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok || inputs.Query == nil {
		return nil
	}
	if s, ok := inputs.Query.(agentinterfaces.InvokeQueryString); ok {
		return string(s)
	}
	return inputs.Query
}

// agentIDOf 从上下文取 agent 标识。
func agentIDOf(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil || cbc.Agent() == nil {
		return ""
	}
	if card := cbc.Agent().Card(); card != nil {
		return card.GetID()
	}
	return ""
}
