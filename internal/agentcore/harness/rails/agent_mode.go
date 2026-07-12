package rails

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/agent_mode"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentModeRail 三层防御式 plan 模式约束 Rail。
//
// 职责：
//  1. Init：注册 switch_mode / enter_plan_mode / exit_plan_mode 工具
//  2. BeforeModelCall：注入 MODE_INSTRUCTIONS 提示词节 + 过滤隐藏工具
//  3. BeforeToolCall：三段式拦截（enter/exit 验证、非 plan 放行、plan 白名单+路径校验）
//  4. AfterToolCall：enter_plan_mode 成功→注册 task_tool，exit_plan_mode 成功→注销 task_tool
//
// 优先级 85 确保 TaskPlanningRail(90)/SubagentRail(95) 先运行，
// 本 Rail 后运行可以移除它们注入的节。
//
// 对齐 Python: AgentModeRail (openjiuwen/harness/rails/agent_mode_rail.py)
type AgentModeRail struct {
	DeepAgentRail
	// allowedTools plan 模式允许的工具名称集合
	allowedTools map[string]struct{}
	// ownsTaskTool 是否持有 task_tool
	ownsTaskTool bool
	// taskTools 持有的 task_tool 列表
	taskTools []tool.Tool
	// ownedTaskToolNames 已持有的 task_tool 名称集合
	ownedTaskToolNames map[string]struct{}
	// tools 注册的 3 个模式切换工具
	tools []tool.Tool
	// systemPromptBuilder 系统提示词构建器
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// agent DeepAgent 实例引用
	agent hinterfaces.DeepAgentInterface
	// language 语言
	language string
	// agentID Agent 标识
	agentID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// agentModeRailPriority AgentModeRail 优先级
	// 对齐 Python: AgentModeRail.priority = 85
	agentModeRailPriority = 85

	// extraSkipToolKey extra 字典中跳过工具的键名
	extraSkipToolKey = "_skip_tool"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 AgentModeRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*AgentModeRail)(nil)

// agentModeLogComponent 日志组件标识
var agentModeLogComponent = logger.ComponentAgentCore

// todoToolNames todo 工具名称集合
// 对齐 Python L44: _TODO_TOOL_NAMES
var todoToolNames = map[string]struct{}{
	"todo_create": {}, "todo_list": {}, "todo_modify": {},
}

// sessionToolNames session 工具名称集合
// 对齐 Python L45: _SESSION_TOOL_NAMES
var sessionToolNames = map[string]struct{}{
	"sessions_list": {}, "sessions_cancel": {}, "sessions_spawn": {},
}

// hiddenInPlan plan 模式下隐藏的工具名称集合
// 对齐 Python L46: _HIDDEN_IN_PLAN = _TODO_TOOL_NAMES | _SESSION_TOOL_NAMES
var hiddenInPlan map[string]struct{}

// hiddenInNormal 普通模式下隐藏的工具名称集合
// 对齐 Python L47: _HIDDEN_IN_NORMAL
var hiddenInNormal = map[string]struct{}{
	"enter_plan_mode": {}, "exit_plan_mode": {},
}

// planFileWriteTools plan 文件写入工具名称集合
// 对齐 Python L49: _PLAN_FILE_WRITE_TOOLS
var planFileWriteTools = map[string]struct{}{
	"write_file": {}, "edit_file": {},
}

// gitWriteRE 匹配 git 写操作的编译正则表达式
// 对齐 Python L53-56: _GIT_WRITE_RE
// nolint: lll
var gitWriteRE = regexp.MustCompile(
	`\bgit\s+(add|commit|push|pull|reset\s+--hard|checkout\s+--\.|clean\s+-[a-zA-Z]*f|` +
		`stash\s+(drop|clear)|branch\s+-D|merge|tag|amend|rebase)\b`)

// defaultPlanModeAllowedTools plan 模式默认允许的工具名称集合
// 对齐 Python L58-71: DEFAULT_PLAN_MODE_ALLOWED_TOOLS
var defaultPlanModeAllowedTools = map[string]struct{}{
	"switch_mode": {}, "enter_plan_mode": {}, "exit_plan_mode": {},
	"ask_user": {}, "task_tool": {}, "read_file": {}, "grep": {},
	"list_files": {}, "glob": {}, "bash": {}, "write_file": {}, "edit_file": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentModeRail 创建 AgentModeRail 实例。
//
// allowedTools 为 nil 时使用 defaultPlanModeAllowedTools。
// 对齐 Python: AgentModeRail.__init__(allowed_tools)
func NewAgentModeRail(allowedTools []string) *AgentModeRail {
	r := &AgentModeRail{
		DeepAgentRail:      *NewDeepAgentRail(),
		allowedTools:       make(map[string]struct{}),
		ownedTaskToolNames: make(map[string]struct{}),
	}

	if allowedTools == nil {
		r.allowedTools = make(map[string]struct{}, len(defaultPlanModeAllowedTools))
		for k := range defaultPlanModeAllowedTools {
			r.allowedTools[k] = struct{}{}
		}
	} else {
		for _, name := range allowedTools {
			r.allowedTools[name] = struct{}{}
		}
	}

	r.WithPriority(agentModeRailPriority)
	return r
}

// Init 注册 switch_mode / enter_plan_mode / exit_plan_mode 工具。
//
// 对齐 Python: AgentModeRail.init() L105-124
func (r *AgentModeRail) Init(agent agentinterfaces.BaseAgent) error {
	// 对齐 Python L111: self._agent = agent
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		logger.Warn(agentModeLogComponent).
			Str("event_type", "agent_mode_rail_init_skip").
			Str("agent_type", fmt.Sprintf("%T", agent)).
			Msg("agent 未实现 DeepAgentInterface，跳过初始化")
		return nil
	}
	r.agent = deepAgent

	// 对齐 Python L112-113: system_prompt_builder 和 language
	sb := agent.SystemPromptBuilder()
	r.systemPromptBuilder = sb
	if sb != nil {
		r.language = sb.Language()
	} else {
		r.language = "cn"
	}

	// 对齐 Python L102: agent_id
	if card := agent.Card(); card != nil {
		r.agentID = card.ID
	}

	// 对齐 Python L115-119: 创建 3 个模式切换工具
	r.tools = []tool.Tool{
		agent_mode.NewSwitchModeTool(deepAgent, r.language, r.agentID),
		agent_mode.NewEnterPlanModeTool(deepAgent, r.language, r.agentID),
		agent_mode.NewExitPlanModeTool(deepAgent, r.language, r.agentID),
	}

	// 对齐 Python L120-122: 注册每个工具的 Card 到 AbilityManager 和 ResourceMgr
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

	logger.Info(agentModeLogComponent).
		Str("event_type", "agent_mode_rail_init").
		Msg("AgentModeRail 已注册 enter/exit plan mode 工具")

	return nil
}

// Uninit 移除本 Rail 注册的所有工具。
//
// 对齐 Python: AgentModeRail.uninit() L132-150
func (r *AgentModeRail) Uninit(agent agentinterfaces.BaseAgent) error {
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()

	// 对齐 Python L138-145: 移除 3 个模式切换工具
	for _, t := range r.tools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(agentModeLogComponent).
						Str("event_type", "agent_mode_rail_uninit").
						Str("tool_name", t.Card().Name).
						Msgf("移除工具失败: %v", rec)
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

	// 对齐 Python L148-149: 如果持有 task_tool 则注销
	if r.ownsTaskTool && len(r.taskTools) > 0 {
		r.unregisterTaskTool(agent)
	}

	logger.Info(agentModeLogComponent).
		Str("event_type", "agent_mode_rail_uninit").
		Msg("AgentModeRail 注销完成")

	return nil
}

// BeforeModelCall 注入 MODE_INSTRUCTIONS 并在 plan 模式下过滤隐藏工具。
//
// 对齐 Python: AgentModeRail.before_model_call() L151-196
func (r *AgentModeRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	agent := r.agent
	sess := cbc.Session()
	planState := agent.LoadState(sess).PlanMode

	if planState.Mode != hschema.AgentModePlan.String() {
		// 非 plan 模式
		// 对齐 Python L162: 移除 MODE_INSTRUCTIONS 节
		if r.systemPromptBuilder != nil {
			r.systemPromptBuilder.RemoveSection(sections.SectionModeInstructions)
		}

		// 对齐 Python L163: 同步 task_tool 可见性
		r.syncTaskToolForModelToolInputs(cbc)

		// 对齐 Python L164-168: 过滤 hiddenInNormal 工具
		r.filterHiddenTools(cbc, hiddenInNormal)

		return nil
	}

	// plan 模式
	// 对齐 Python L172-174: 获取 plan 文件路径和存在状态
	planFilePath := agent.GetPlanFilePath(sess)
	planExists := false
	if planFilePath != "" {
		if _, err := os.Stat(planFilePath); err == nil {
			planExists = true
		}
	}

	// 对齐 Python L176-182: 构建 plan 模式提示词节
	enterStatus := r.buildEnterPlanModeStatus(planFilePath, planExists)
	planFileInfo := r.buildPlanFileInfo(planFilePath, planExists)
	section := sections.BuildPlanModeSection(enterStatus, planFileInfo, r.language)

	// 对齐 Python L183: 添加节
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.AddSection(section)
	}

	// 对齐 Python L186-187: 移除 Todo 和 SessionTools 节
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionTodo)
		r.systemPromptBuilder.RemoveSection(sections.SectionSessionTools)
	}

	// 对齐 Python L190-194: 过滤 hiddenInPlan 工具
	r.filterHiddenTools(cbc, hiddenInPlan)

	// 对齐 Python L196: 同步 task_tool 可见性
	r.syncTaskToolForModelToolInputs(cbc)

	return nil
}

// BeforeToolCall 三段式工具调用拦截，执行 plan 模式约束。
//
// 三段：
//  1. enter/exit_plan_mode → 验证模式后放行
//  2. 非 plan 模式 → 无条件放行
//  3. plan 模式 → 白名单 + 路径校验 + 硬性隐藏
//
// 对齐 Python: AgentModeRail.before_tool_call() L232-329
func (r *AgentModeRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return nil
	}
	toolName := inputs.ToolName

	// ─── 段 1: enter/exit_plan_mode — 模式校验 + 放行 ───
	// 对齐 Python L250-255
	if toolName == "enter_plan_mode" {
		r.handleEnter(cbc)
		return nil
	}
	if toolName == "exit_plan_mode" {
		r.handleExit(cbc)
		return nil
	}

	// ─── 段 2: 非 plan 模式 → 无条件放行 ───
	// 对齐 Python L260-262
	agent := r.agent
	sess := cbc.Session()
	planState := agent.LoadState(sess).PlanMode
	if planState.Mode != hschema.AgentModePlan.String() {
		return nil
	}

	// ─── 段 3: plan 模式 → 白名单 + 路径校验 + 硬性隐藏 ───

	// 对齐 Python L267-268: 已被跳过则放行
	if skipVal, exists := cbc.Extra()[extraSkipToolKey]; exists && skipVal == true {
		return nil
	}

	// 3a. 硬性屏蔽 todo/session 工具
	// 对齐 Python L270-279
	if _, hidden := hiddenInPlan[toolName]; hidden {
		var msg string
		if r.languageIsCN() {
			msg = fmt.Sprintf("[AgentModeRail] 工具「%s」在 plan 模式下已隐藏。", toolName)
		} else {
			msg = fmt.Sprintf("[AgentModeRail] Tool '%s' is hidden in plan mode.", toolName)
		}
		r.rejectTool(cbc, msg)
		return nil
	}

	// 3b. 不在白名单 → 拒绝
	// 对齐 Python L282-293
	if len(r.allowedTools) > 0 {
		if _, allowed := r.allowedTools[toolName]; !allowed {
			logger.Info(agentModeLogComponent).
				Str("event_type", "reject_tool_not_in_allowed").
				Str("tool_name", toolName).
				Msg("reject tool call by not in allowed tools")
			var msg string
			if r.languageIsCN() {
				msg = fmt.Sprintf("[AgentModeRail] 工具「%s」不在 plan 模式允许列表中。", toolName)
			} else {
				msg = fmt.Sprintf("[AgentModeRail] Tool '%s' is not available in plan mode.", toolName)
			}
			r.rejectTool(cbc, msg)
			return nil
		}
	}

	// 3c. bash → 阻止 git 写操作
	// 对齐 Python L296-310
	if toolName == "bash" {
		command := r.extractBashCommand(cbc)
		if gitWriteRE.MatchString(command) {
			logger.Info(agentModeLogComponent).
				Str("event_type", "reject_bash_git_write").
				Str("command", command).
				Msg("reject bash call: git write operation in plan mode")
			var msg string
			if r.languageIsCN() {
				msg = fmt.Sprintf("[AgentModeRail] plan 模式下禁止执行 git 写操作（%q）。", command)
			} else {
				msg = fmt.Sprintf("[AgentModeRail] Git write operations are blocked in plan mode (%q).", command)
			}
			r.rejectTool(cbc, msg)
			return nil
		}
	}

	// 3d. write_file / edit_file → 仅允许目标为 plan 文件
	// 对齐 Python L313-329
	if _, isWriteTool := planFileWriteTools[toolName]; isWriteTool {
		filePath := r.extractFilePath(cbc)
		planPath := agent.GetPlanFilePath(sess)
		if !r.isPlanFile(filePath, planPath) {
			logger.Info(agentModeLogComponent).
				Str("event_type", "reject_tool_not_plan_file").
				Str("tool_name", toolName).
				Str("file_path", filePath).
				Str("plan_path", planPath).
				Msg("reject tool call by not in plan file")
			var msg string
			if r.languageIsCN() {
				msg = fmt.Sprintf("[AgentModeRail] 「%s」仅能用于计划文件（%s）。", toolName, planPath)
			} else {
				msg = fmt.Sprintf("[AgentModeRail] '%s' can only target the plan file (%s).", toolName, planPath)
			}
			r.rejectTool(cbc, msg)
			return nil
		}
	}

	return nil
}

// AfterToolCall enter_plan_mode 成功时注册 task_tool，exit_plan_mode 成功时注销。
//
// 对齐 Python: AgentModeRail.after_tool_call() L331-344
func (r *AgentModeRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return nil
	}
	toolName := inputs.ToolName

	// 对齐 Python L340-341
	if toolName == "enter_plan_mode" {
		if skipVal, exists := cbc.Extra()[extraSkipToolKey]; !exists || skipVal != true {
			r.registerTaskTool(cbc.Agent())
		}
	}

	// 对齐 Python L343-344
	if toolName == "exit_plan_mode" {
		if skipVal, exists := cbc.Extra()[extraSkipToolKey]; !exists || skipVal != true {
			r.unregisterTaskTool(cbc.Agent())
		}
	}

	return nil
}

// GetCallbacks 覆盖基类回调映射，增加 BeforeModelCall + BeforeToolCall + AfterToolCall。
//
// 对齐 Python: AgentModeRail 隐式覆盖 before_model_call/before_tool_call/after_tool_call
func (r *AgentModeRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 构建 hiddenInPlan = todoToolNames ∪ sessionToolNames
	hiddenInPlan = make(map[string]struct{}, len(todoToolNames)+len(sessionToolNames))
	for k := range todoToolNames {
		hiddenInPlan[k] = struct{}{}
	}
	for k := range sessionToolNames {
		hiddenInPlan[k] = struct{}{}
	}
}

// rejectTool 轻量级工具拒绝——设置 _skip_tool 并注入错误结果。
//
// 对齐 Python: AgentModeRail._reject_tool() L476-488
func (r *AgentModeRail) rejectTool(cbc *agentinterfaces.AgentCallbackContext, errorMsg string) {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		// 即使 inputs 不是 ToolCallInputs，也要设置 skip 标记
		cbc.Extra()[extraSkipToolKey] = true
		return
	}

	toolCallID := ""
	if inputs.ToolCall != nil {
		toolCallID = inputs.ToolCall.ID
	}

	msg := llmschema.NewToolMessage(toolCallID, errorMsg)
	cbc.Extra()[extraSkipToolKey] = true
	inputs.ToolResult = map[string]any{"error": errorMsg}
	inputs.ToolMsg = msg
}

// handleEnter 验证 enter_plan_mode 的模式前提。
//
// 对齐 Python: AgentModeRail._handle_enter() L434-457
func (r *AgentModeRail) handleEnter(cbc *agentinterfaces.AgentCallbackContext) {
	agent := r.agent
	sess := cbc.Session()
	planState := agent.LoadState(sess).PlanMode

	// 对齐 Python L445-446: 不在 plan 模式则拒绝
	if planState.Mode != hschema.AgentModePlan.String() {
		logger.Info(agentModeLogComponent).
			Str("event_type", "reject_enter_not_plan_mode").
			Msg("reject enter tool because of not plan mode")
		var msg string
		if r.languageIsCN() {
			msg = "[AgentModeRail] enter_plan_mode 只能在 plan 模式下被调用。请调用 switch_mode 工具切换到 plan 模式。"
		} else {
			msg = "[AgentModeRail] enter_plan_mode can only be called in plan mode. Use the switch_mode tool to switch to plan mode."
		}
		r.rejectTool(cbc, msg)
	}
}

// handleExit 验证 exit_plan_mode 的模式前提。
//
// 对齐 Python: AgentModeRail._handle_exit() L459-474
func (r *AgentModeRail) handleExit(cbc *agentinterfaces.AgentCallbackContext) {
	agent := r.agent
	sess := cbc.Session()
	planState := agent.LoadState(sess).PlanMode

	// 对齐 Python L469-470: 不在 plan 模式则拒绝
	if planState.Mode != hschema.AgentModePlan.String() {
		var msg string
		if r.languageIsCN() {
			msg = "[AgentModeRail] exit_plan_mode 只能在 plan 模式下被调用。"
		} else {
			msg = "[AgentModeRail] exit_plan_mode can only be called in plan mode."
		}
		r.rejectTool(cbc, msg)
	}
}

// isPlanFile 检查给定文件路径是否解析到 plan 文件。
//
// 对齐 Python: AgentModeRail._is_plan_file() L490-506
func (r *AgentModeRail) isPlanFile(filePath, planPath string) bool {
	if planPath == "" || filePath == "" {
		return false
	}
	// 对齐 Python L503-505: Path(file_path).resolve() == Path(plan_path).resolve()
	// resolve = Abs + EvalSymlinks（与 Python Path.resolve() 行为一致）
	tryResolve := func(p string) string {
		abs, err := filepath.Abs(p)
		if err != nil {
			return p
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return abs
		}
		return resolved
	}
	return tryResolve(filePath) == tryResolve(planPath)
}

// extractFilePath 从 ToolCallInputs.ToolArgs 中提取 file_path 参数。
//
// 对齐 Python: AgentModeRail._extract_file_path() L508-523
func (r *AgentModeRail) extractFilePath(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return ""
	}
	args := r.parseToolArgs(inputs.ToolArgs)
	if args == nil {
		return ""
	}
	fp, _ := args["file_path"].(string)
	return fp
}

// extractBashCommand 从 ToolCallInputs.ToolArgs 中提取 command 参数。
//
// 对齐 Python: AgentModeRail._extract_bash_command() L525-548
func (r *AgentModeRail) extractBashCommand(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return ""
	}
	args := r.parseToolArgs(inputs.ToolArgs)
	if args == nil {
		return ""
	}
	cmd, _ := args["command"].(string)
	return cmd
}

// parseToolArgs 将 JSON 字符串形式的工具参数解析为 map[string]any。
func (r *AgentModeRail) parseToolArgs(toolArgs string) map[string]any {
	if toolArgs == "" {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(toolArgs), &args); err != nil {
		return nil
	}
	return args
}

// registerTaskTool 在 enter_plan_mode 成功后注册 task_tool。
//
// 对齐 Python: AgentModeRail._register_task_tool() L358-387
func (r *AgentModeRail) registerTaskTool(agent agentinterfaces.BaseAgent) {
	if r.ownsTaskTool {
		return
	}

	existing := r.isTaskToolRegistered()
	if existing {
		logger.Info(agentModeLogComponent).
			Str("event_type", "agent_mode_task_tool_exists").
			Msg("task tool already registered, skip register")
		return
	}

	// 对齐 Python L370-371: 无 subagents 则跳过
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok || deepAgent.DeepConfig() == nil || len(deepAgent.DeepConfig().Subagents) == 0 {
		return
	}

	// 对齐 Python L373-378: 构建 available_agents 并创建 task_tool
	availableAgents := r.buildAvailableAgents(deepAgent.DeepConfig().Subagents)

	// 对齐 Python: create_task_tool(parent_agent, available_agents, language)
	taskTools := agent_mode.CreateTaskTool(deepAgent, availableAgents, r.language)
	if len(taskTools) == 0 {
		return
	}

	r.taskTools = taskTools
	r.ownedTaskToolNames = make(map[string]struct{}, len(taskTools))
	for _, t := range taskTools {
		r.ownedTaskToolNames[t.Card().Name] = struct{}{}
	}

	// 对齐 Python L383-385: 注册到 ResourceMgr 和 AbilityManager
	resourceMgr := runner.GetResourceMgr()
	am := agent.AbilityManager()
	for _, t := range taskTools {
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
		if am != nil {
			am.Add(t.Card())
		}
	}

	r.ownsTaskTool = true

	logger.Info(agentModeLogComponent).
		Str("event_type", "agent_mode_register_task_tool").
		Msg("AgentModeRail 已注册 task_tool for plan mode")
}

// unregisterTaskTool 注销本 Rail 持有的 task_tool。
//
// 对齐 Python: AgentModeRail._unregister_task_tool() L389-408
func (r *AgentModeRail) unregisterTaskTool(agent agentinterfaces.BaseAgent) {
	if !r.ownsTaskTool || len(r.taskTools) == 0 {
		logger.Info(agentModeLogComponent).
			Str("event_type", "agent_mode_no_task_tool_to_unregister").
			Msg("no task tool registered, skip unregister")
		return
	}

	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()

	for _, t := range r.taskTools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(agentModeLogComponent).
						Str("event_type", "agent_mode_unregister_task_tool_failed").
						Str("tool_name", t.Card().Name).
						Msgf("注销 task_tool 失败: %v", rec)
				}
			}()
			if am != nil {
				am.Remove(t.Card().Name)
			}
			if resourceMgr != nil {
				_, _ = resourceMgr.RemoveTool([]string{t.Card().ID})
			}
			logger.Info(agentModeLogComponent).
				Str("event_type", "agent_mode_unregistered_task_tool").
				Str("tool_name", t.Card().Name).
				Msg("AgentModeRail 已注销 plan-mode task_tool")
		}(t)
	}

	r.taskTools = nil
	r.ownsTaskTool = false
}

// isTaskToolRegistered 检查 task_tool 是否已在 AbilityManager 中注册。
//
// 对齐 Python: AgentModeRail._is_task_tool_registered() L346-356
func (r *AgentModeRail) isTaskToolRegistered() bool {
	// 对齐 Python L348-356: 在已注册工具中搜索 "task_tool" 名称
	if r.ownsTaskTool {
		return true
	}
	if r.agent == nil {
		return false
	}
	// 对齐 Python: Runner.resource_mgr.get_tool() → 遍历已注册工具
	// Go 等价: 通过 ReactAgent 获取 AbilityManager
	reactAgent := r.agent.ReactAgent()
	if reactAgent == nil {
		return false
	}
	am := reactAgent.AbilityManager()
	if am == nil {
		return false
	}
	for _, ability := range am.List() {
		if ability.AbilityName() == "task_tool" {
			return true
		}
	}
	return false
}

// syncTaskToolForModelToolInputs 同步 task_tool 在 model 可见工具列表中的可见性。
//
// 在 react-agent 流程中 ctx.inputs.tools 可能跨 turn 复用，
// 此方法保持 task_tool 与当前注册状态一致：
//   - 持有+已注册 → 确保存在
//   - 未持有 → 确保不存在
//
// 对齐 Python: AgentModeRail._sync_task_tool_for_model_tool_inputs() L198-230
func (r *AgentModeRail) syncTaskToolForModelToolInputs(cbc *agentinterfaces.AgentCallbackContext) {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || inputs == nil {
		return
	}
	if inputs.Tools == nil {
		return
	}

	// 对齐 Python L211-219: 持有 task_tool → 确保在 tools 列表中
	if r.ownsTaskTool && len(r.taskTools) > 0 {
		existingNames := make(map[string]struct{}, len(inputs.Tools))
		for _, t := range inputs.Tools {
			name := t.GetName()
			if name != "" {
				existingNames[name] = struct{}{}
			}
		}
		for _, t := range r.taskTools {
			if _, exists := existingNames[t.Card().Name]; !exists {
				inputs.Tools = append(inputs.Tools, t.Card().ToolInfo())
			}
		}
		return
	}

	// 对齐 Python L223-228: 未持有 → 从 tools 列表中移除已知的 task_tool 名称
	if !r.ownsTaskTool && len(r.ownedTaskToolNames) > 0 {
		var filtered []cschema.ToolInfoInterface
		for _, t := range inputs.Tools {
			if _, shouldRemove := r.ownedTaskToolNames[t.GetName()]; !shouldRemove {
				filtered = append(filtered, t)
			}
		}
		inputs.Tools = filtered
		// 清空已记录的名称，避免重复过滤
		r.ownedTaskToolNames = make(map[string]struct{})
	}
}

// buildAvailableAgents 构建格式化的子 Agent 描述。
//
// 对齐 Python: AgentModeRail._build_available_agents() L410-432
func (r *AgentModeRail) buildAvailableAgents(subagents []hschema.SubagentSpec) string {
	var lines []string
	for _, spec := range subagents {
		// 对齐 Python L424: isinstance(spec, SubAgentConfig)
		if saConfig, ok := spec.(*hschema.SubAgentConfig); ok && saConfig != nil {
			// 对齐 Python L425-426: spec.agent_card.name / spec.agent_card.description
			// Python 不防御 nil agent_card，Go 需防御
			name := "general-purpose"
			desc := "DeepAgent instance"
			if saConfig.AgentCard != nil {
				name = saConfig.AgentCard.Name
				desc = saConfig.AgentCard.Description
			}
			lines = append(lines, fmt.Sprintf("%q: %s", name, desc))
		} else {
			// 对齐 Python L427-430: getattr(spec, "card", None) → getattr(card, "name", None) or "general-purpose"
			name := "general-purpose"
			desc := "DeepAgent instance"
			if baseAgent, ok := spec.(agentinterfaces.BaseAgent); ok {
				if card := baseAgent.Card(); card != nil {
					if card.Name != "" {
						name = card.Name
					}
					if card.Description != "" {
						desc = card.Description
					}
				}
			}
			if name == "general-purpose" {
				// 尝试 SpecName 作为 fallback
				if specName := spec.SpecName(); specName != "" {
					name = specName
				}
			}
			lines = append(lines, fmt.Sprintf("%q: %s", name, desc))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

// languageIsCN 检查当前语言是否为中文。
//
// 对齐 Python: AgentModeRail._language_is_cn() L126-130
func (r *AgentModeRail) languageIsCN() bool {
	if r.systemPromptBuilder == nil {
		return true
	}
	return r.systemPromptBuilder.Language() == "cn"
}

// buildEnterPlanModeStatus 构建 enter_plan_mode 状态描述。
//
// 对齐 Python: _build_enter_plan_mode_status() L200-223 + L309
// 三状态分支：planFilePath 是否非空判断 enter_plan_mode 是否已调用
func (r *AgentModeRail) buildEnterPlanModeStatus(planFilePath string, planExists bool) string {
	if r.languageIsCN() {
		if planFilePath != "" {
			// 对齐 Python L309: 中文分支不含 Plan 文件路径（路径由 _build_plan_file_info 单独返回）
			return "enter_plan_mode 已调用完成。请继续工作流。"
		}
		return "你尚未调用 enter_plan_mode。请立即调用它作为你的第一个操作。"
	}
	// 英文
	if planFilePath != "" {
		return "enter_plan_mode has been called. Proceed with the workflow."
	}
	return "You have NOT called enter_plan_mode yet. Call it NOW as your first action."
}

// buildPlanFileInfo 构建 plan 文件信息描述。
//
// 对齐 Python: _build_plan_file_info() L226-257
// 三状态分支 + 提示词一比一复刻 Python（含 edit_file/write_file 工具名引用）
func (r *AgentModeRail) buildPlanFileInfo(planFilePath string, planExists bool) string {
	if planFilePath == "" {
		if r.languageIsCN() {
			return "尚无 plan 文件。请先调用 enter_plan_mode 创建。"
		}
		return "No plan file yet. Call enter_plan_mode first to create one."
	}
	if planExists {
		if r.languageIsCN() {
			return fmt.Sprintf("计划文件已存在于 %s。你可以使用 edit_file 工具读取并增量编辑它。", planFilePath)
		}
		return fmt.Sprintf("A plan file already exists at %s. You can read it and make incremental edits using the edit_file tool.", planFilePath)
	}
	if r.languageIsCN() {
		return fmt.Sprintf("计划文件尚不存在。你应该使用 write_file 工具在 %s 创建计划。", planFilePath)
	}
	return fmt.Sprintf("No plan file exists yet. You should create your plan at %s using the write_file tool.", planFilePath)
}

// filterHiddenTools 从 ModelCallInputs.Tools 中过滤掉指定名称集合的工具。
func (r *AgentModeRail) filterHiddenTools(cbc *agentinterfaces.AgentCallbackContext, hiddenNames map[string]struct{}) {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || inputs == nil || inputs.Tools == nil {
		return
	}
	var filtered []cschema.ToolInfoInterface
	for _, t := range inputs.Tools {
		if _, hidden := hiddenNames[t.GetName()]; !hidden {
			filtered = append(filtered, t)
		}
	}
	inputs.Tools = filtered
}
