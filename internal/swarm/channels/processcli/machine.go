package processcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"bytes"
	"encoding/json"

	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	permissiontools "github.com/uapclaw/uapclaw-go/internal/common/permission_tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	runtimectx "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/ctx"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MachineRunError 非交互执行边界的稳定失败。
// Python: machine.py:63-75 MachineRunError
type MachineRunError struct {
	// Message 失败说明
	Message string
	// Code 稳定错误码
	Code string
	// Details 附加详情
	Details map[string]any
	// Retryable 是否可重试
	Retryable bool
}

// MachineRun 只持有本进程的公有客户端、请求与流句柄。
// Python: machine.py:145-516 _MachineRun
type MachineRun struct {
	// runInput 本次运行的输入
	runInput *protocol.OneShotRunInput
	// runContext 本次调用的不可变选项
	// Python: machine.py:156-162 self.run_context = RunContext(...)
	runContext *runtimectx.RunContext
	// writer 输出写入器
	writer *OneShotWriter
	// client 进程内 Runtime 客户端
	client *InProcessRuntimeClient
	// request 本次聊天请求
	request *schema.AgentRequest
	// summary 事件流归约器
	summary *RunSummary
	// limits 用量限额计量器
	limits *RunLimits
	// outputJSON 结构化输出
	outputJSON map[string]any
	// err 第一条运行级错误
	err *protocol.RuntimeErrorInfo
	// status 终态
	status string
	// exitCode 退出码
	exitCode int
	// cleanupErrors 清理步骤失败名列表
	cleanupErrors []string
	// control 双工控制器
	// ⤵️ 回填 20.4：DuplexController 未实现（用户决策），本批恒 nil，只走非交互分支。
	// 上游类型 Python: machine.py:153 control: DuplexController | None
	control any
	// sessionLease 会话互斥租约
	// ⤵️ 回填 20.5：SessionLease 未实现（用户决策），本批恒 nil，
	// acquire/release 全部跳过（D-4 静默放行）。
	// 上游类型 Python: machine.py:178 session_lease: SessionLease | None
	sessionLease any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// machineShutdownTimeoutSeconds 清理步骤的单步窗口。
	// Python: machine.py:50 SHUTDOWN_TIMEOUT_SECONDS = 5.0
	machineShutdownTimeoutSeconds = 5.0
	// machineChannelID Process CLI 渠道标识。
	// Python: machine.py:49 CHANNEL_ID = "process_cli"
	machineChannelID = "process_cli"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// interactionEvents 需要交互的事件类型（非交互模式下遇到即失败）。
	// Python: machine.py:51-53 _INTERACTION_EVENTS
	interactionEvents = map[string]struct{}{
		"chat.ask_user_question":       {},
		"plan.approval_required":       {},
		"harness.activate_interaction": {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *MachineRunError) Error() string { return e.Message }

// CodeValue 稳定错误码（对齐 Python 的 error.code）。
func (e *MachineRunError) CodeValue() string { return e.Code }

// NewMachineRun 构造一次机器运行。
// Python: machine.py:148-178 _MachineRun.__init__
func NewMachineRun(
	runInput *protocol.OneShotRunInput, writer *OneShotWriter, control any,
) *MachineRun {
	// Python: :156-162 RunContext(request_id=writer.request_id, max_turns=...,
	//                            max_budget_usd=..., host_tools=..., tool_permissions=...)
	runContext := runtimectx.NewRunContext()
	runContext.RequestID = writer.RequestID
	runContext.MaxTurns = runInput.MaxTurns
	runContext.MaxBudgetUSD = runInput.MaxBudgetUSD
	if runInput.HostTools != nil {
		tools := make([]any, 0, len(runInput.HostTools))
		for i := range runInput.HostTools {
			tools = append(tools, runInput.HostTools[i])
		}
		runContext.HostTools = tools
	}
	if runInput.Permissions != nil {
		runContext.ToolPermissions = permissionLevels(runInput.Permissions)
	}
	return &MachineRun{
		runInput:   runInput,
		runContext: runContext,
		writer:     writer,
		summary:    NewRunSummary(),
		limits:     NewRunLimits(runContext.MaxTurns, runContext.MaxBudgetUSD),
		status:     string(protocol.StatusCompleted),
		exitCode:   0,
		control:    control,
	}
}

// Execute 按上游 25 步完成一次机器执行。
// Python: machine.py:193-388 _MachineRun.execute
func (m *MachineRun) Execute(
	ctx context.Context, clientFactory func() *InProcessRuntimeClient, deadline time.Time,
) error {
	// 步骤 1: 输出 schema 预校验（在启动 Agent 或计费之前）
	// Python: :196-204 if output_schema is not None: validate_output_schema(...)
	if m.runInput.OutputSchema != nil {
		if schemaErr := ValidateOutputSchema(m.runInput.OutputSchema); schemaErr != nil {
			return &MachineRunError{
				Message: schemaErr.Error(),
				Code:    "INVALID_OUTPUT_SCHEMA",
				Details: map[string]any{"field": "/output_schema", "reason": "invalid_schema"},
			}
		}
	}
	// 步骤 2: 宿主工具校验 + 双工要求 + 白名单
	// Python: :205-233
	if len(m.runInput.HostTools) > 0 {
		// ⤵️ 回填 20.6：host_tools.validate_host_tool_schemas 未实现
		// （用户决策：本批 hostTools 非空即报错）
		if hostErr := validateHostToolsForRun(m.runInput); hostErr != nil {
			return hostErr
		}
	}
	// 步骤 3: 会话租约（显式 session_id 时先占）
	// Python: :234-236 if session_id is not None: SessionLease(session_id).acquire()
	// ⤵️ 回填 20.5：SessionLease 未实现（用户决策：sessionLease 恒 nil）
	//
	// 步骤 4: 启动客户端
	// Python: :237-238 self.client = client_factory(); await self.client.start()
	factory := clientFactory
	if factory == nil {
		factory = func() *InProcessRuntimeClient { return NewInProcessRuntimeClient(nil) }
	}
	m.client = factory()
	if startErr := m.client.Start(); startErr != nil {
		return startErr
	}

	// 步骤 5: 恢复会话时读描述并校验既有 root 模式
	// Python: :239-247 descriptor = await describe_session(...); if None: SESSION_NOT_FOUND
	var descriptor *sessionpkg.SessionDescriptor
	if m.runInput.SessionID != nil && strings.TrimSpace(*m.runInput.SessionID) != "" {
		described, descErr := m.client.DescribeSession(strings.TrimSpace(*m.runInput.SessionID))
		if descErr != nil {
			return descErr
		}
		if described == nil {
			return &MachineRunError{Message: "Session not found.", Code: "SESSION_NOT_FOUND"}
		}
		descriptor = described
		// Python: :246-247 即使调用方给了新 mode，也先校验既有 root 模式
		if _, modeErr := m.client.ResolveModeCapability(descriptor.Mode); modeErr != nil {
			return modeErr
		}
	}

	// 步骤 6: 解析模式
	// Python: :248-251 mode_name = run_input.mode or (descriptor.mode if descriptor else "agent.code.normal")
	modeName := m.executeMode()
	if m.runInput.Mode == nil && descriptor != nil {
		modeName = descriptor.Mode
	}
	mode, modeErr := m.client.ResolveModeCapability(modeName)
	if modeErr != nil {
		return modeErr
	}

	// 步骤 7: 声明式 Agent 定义校验
	// Python: :252-255 if agent is not None: validate_agent_definition(agent.to_dict(), mode=mode.mode)
	if m.runInput.Agent != nil {
		if _, defErr := m.client.ValidateAgentDefinition(
			m.runInput.Agent.ToDict(), mode.Mode); defErr != nil {
			return defErr
		}
	}

	// 步骤 8: 解析模型
	// Python: :256-260 selected_model = resolve_model_capability(model) if model is not None else None
	var selectedModel *swruntime.RuntimeModelDescriptor
	if m.runInput.Model != nil {
		resolved, modelErr := m.client.ResolveModelCapability(*m.runInput.Model)
		if modelErr != nil {
			return modelErr
		}
		selectedModel = &resolved
	}

	// 步骤 9: MCP 引用校验
	// Python: :261-267 mcp_status = validate_mcp_references(mcp); if not valid: MCP_NOT_READY
	if len(m.runInput.Mcp) > 0 {
		status, mcpErr := m.client.ValidateMcpReferences(m.runInput.Mcp)
		if mcpErr != nil {
			return mcpErr
		}
		if !status.Valid {
			return &MachineRunError{
				Message: "Selected MCP references are unavailable.",
				Code:    "MCP_NOT_READY",
			}
		}
	}

	// 步骤 10: 创建或恢复会话
	// Python: :268-272 session_id = await create_or_resume_session(...); writer.session_id = session_id
	sessionID, sessionErr := m.client.CreateOrResumeSession(machineChannelID, m.runInput.SessionID)
	if sessionErr != nil {
		return sessionErr
	}
	m.writer.SessionID = &sessionID

	// 步骤 11: 会话租约（新建会话后补占）
	// Python: :273-275 if self.session_lease is None: SessionLease(session_id).acquire()
	// ⤵️ 回填 20.5：SessionLease 未实现（sessionLease 恒 nil）

	// 步骤 12: 绑定声明式 Agent 到会话
	// Python: :276-282 bind_agent(session_id, agent.to_dict() if agent else None,
	//                             resumed=descriptor is not None)
	// ⤵️ 回填 20.5：bind_agent 未实现（用户决策）。声明式 Agent 的定义仍经
	// stream_agent 的 PrepareAgentExecution 传递，只是不写入会话绑定表。

	// 步骤 13: workspace 参数
	// Python: :283 params = _workspace_params(run_input, resumed=descriptor is not None)
	params := workspaceParams(m.runInput, descriptor != nil)

	// 步骤 14: 结构化提示词
	// Python: :284-288 query = structured_prompt(input, output_schema) if output_schema else input
	query := m.runInput.Input
	if m.runInput.OutputSchema != nil {
		query = StructuredPrompt(m.runInput.Input, m.runInput.OutputSchema)
	}

	// 步骤 15: 公共参数字段
	// Python: :289-297 params.update({query, content, mode, work_mode, supports_user_interaction})
	params["query"] = query
	params["content"] = query
	params["mode"] = mode.Mode
	params["work_mode"] = mode.WorkMode
	// Python: :295 supports_user_interaction = self.control is not None
	params["supports_user_interaction"] = m.control != nil

	// 步骤 16: 模型选择键
	// Python: :298-299 if selected_model is not None: params["model_name"] = selection_key
	if selectedModel != nil {
		params["model_name"] = selectedModel.SelectionKey
	}
	// 步骤 17: skills / mcp
	// Python: :300-303
	if m.runInput.Skills != nil {
		params["skills"] = stringSliceToAny(m.runInput.Skills)
	}
	if m.runInput.Mcp != nil {
		params["mcp"] = stringSliceToAny(m.runInput.Mcp)
	}

	// 步骤 18: 运行级工具权限归一
	// Python: :304-318
	if m.runContext.ToolPermissions != nil {
		toolLevels := make(map[string]any, len(m.runContext.ToolPermissions))
		for _, entry := range m.runContext.ToolPermissions {
			canonical := permissiontools.NormalizePermissionToolName(entry.Name)
			if canonical == "" {
				return &MachineRunError{
					Message: "Run permission tool names are ambiguous.",
					Code:    "INVALID_RUN_PERMISSIONS",
				}
			}
			if _, duplicate := toolLevels[canonical]; duplicate {
				return &MachineRunError{
					Message: "Run permission tool names are ambiguous.",
					Code:    "INVALID_RUN_PERMISSIONS",
				}
			}
			toolLevels[canonical] = entry.Level
		}
		params["run_permissions"] = map[string]any{"tools": toolLevels}
	}

	// 步骤 19: 构造聊天请求
	// Python: :319-327 AgentRequest(request_id, channel_id, session_id, CHAT_SEND, is_stream=True, ...)
	m.request = &schema.AgentRequest{
		RequestID: m.writer.RequestID,
		ChannelID: machineChannelID,
		SessionID: &sessionID,
		ReqMethod: schema.ReqMethodChatSend,
		IsStream:  true,
	}
	if encoded, encodeErr := marshalParams(params); encodeErr == nil {
		m.request.Params = encoded
	} else {
		return encodeErr
	}

	// 步骤 20: 装配 on_agent_ready
	// Python: :328-347 ready = configure if (max_turns is not None or host_tools)
	var onAgentReady func(context.Context, harnessinterfaces.DeepAgentInterface) error
	if m.runContext.MaxTurns != nil || len(m.runContext.HostTools) > 0 {
		onAgentReady = func(agentCtx context.Context, agent harnessinterfaces.DeepAgentInterface) error {
			// Python: :332-341 host_tools 转 HostCallbackTool
			// ⤵️ 回填 20.6：HostCallbackTool 未实现（本批 host_tools 已在步骤 2 拒绝）
			// Python: :342-345 await agent.configure_process_cli_run(max_turns=..., host_tools=tools)
			if configurable, ok := agent.(interface {
				ConfigureProcessCliRun(context.Context, *int, []any) error
			}); ok {
				return configurable.ConfigureProcessCliRun(agentCtx, m.runContext.MaxTurns, nil)
			}
			return nil
		}
	}

	// 步骤 21: 选择 stream / stream_agent
	// Python: :348-363
	events := make([]*swruntime.RuntimeEvent, 0, 64)
	collect := func(event *swruntime.RuntimeEvent) bool {
		events = append(events, event)
		return true
	}
	if m.runInput.Agent == nil {
		if streamErr := m.client.Stream(ctx, m.request, collect, onAgentReady); streamErr != nil {
			return streamErr
		}
	} else {
		if streamErr := m.client.StreamAgent(
			ctx, m.request, m.runInput.Agent.ToDict(), collect, onAgentReady); streamErr != nil {
			return streamErr
		}
	}

	// 步骤 22: 消费事件流（非交互路径）
	// Python: :364-372 if control is not None: control.consume(...) else consume_noninteractive()
	completed, consumeErr := m.consumeEvents(events)
	if consumeErr != nil {
		return consumeErr
	}

	// 步骤 23: 运行级错误归一
	// Python: :373-379
	if summaryErr := m.summary.Error(); summaryErr != nil {
		m.Fail(m.NormalizeRunError(summaryErr), string(protocol.StatusFailed), 1)
	} else if !completed {
		return &MachineRunError{
			Message: "Runtime stream ended without a completion event.",
			Code:    "INCOMPLETE_RUN",
		}
	}

	// 步骤 24: 结构化输出解析
	// Python: :380-388
	if m.runInput.OutputSchema != nil {
		parsed, parseErr := ParseStructuredOutput(m.summary.FinalAnswer(), m.runInput.OutputSchema)
		if parseErr != nil {
			return &MachineRunError{Message: parseErr.Error(), Code: "OUTPUT_SCHEMA_MISMATCH"}
		}
		m.outputJSON = parsed
	}

	// 步骤 25: 超时判定
	// Python: :534-546 asyncio.timeout 包住整个 execute
	if !deadline.IsZero() && time.Now().After(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// Cleanup 依次释放本次运行持有的资源，单步失败不阻断后续步骤。
// Python: machine.py:456-488 _MachineRun.cleanup
func (m *MachineRun) Cleanup(ctx context.Context) {
	// Python: :457-458 if control is not None: cleanup_step("control_input", control.stop_input)
	// ⤵️ 回填 20.4：DuplexController 未实现（control 恒 nil）

	// Python: :459-464 client is None 时只记录已有清理错误
	if m.client == nil {
		m.RecordCleanupErrors()
		return
	}

	// Python: :465-471 失败时先取消请求
	if m.err != nil && m.request != nil {
		// Python: :469 _cancel_request(request, session_scope=self.control is not None)
		m.cleanupStep(ctx, "cancel", func(stepCtx context.Context) error {
			return m.client.Cancel(stepCtx, cancelRequest(m.request, m.control != nil))
		})
	}
	// Python: :472-473 if control is not None: cleanup_step("control_streams", ...)
	// ⤵️ 回填 20.4

	// Python: :474-476 关闭事件流（Go 的 Stream 是回调式，无需 aclose）
	//
	// Python: :477-484 清理会话
	if m.writer.SessionID != nil {
		sessionID := *m.writer.SessionID
		m.cleanupStep(ctx, "cleanup_session", func(stepCtx context.Context) error {
			_, cleanupErr := m.client.CleanupSession(machineChannelID, sessionID)
			return cleanupErr
		})
	}
	// Python: :485 关闭 Runtime（内部已含 flush_session_writes）
	m.cleanupStep(ctx, "runtime_close", func(_ context.Context) error {
		return m.client.Close()
	})
	// Python: :486-487 flush 已在 client.Close 内完成，此处不重复
	//
	// Python: :488 self._record_cleanup_errors()
	m.RecordCleanupErrors()
}

// RecordCleanupErrors 把清理失败并入结果错误。
// Python: machine.py:490-503 _MachineRun._record_cleanup_errors
func (m *MachineRun) RecordCleanupErrors() {
	if len(m.cleanupErrors) == 0 {
		return
	}
	// Python: :492-493 error = self.error or RuntimeErrorInfo(SHUTDOWN_FAILED, ...)
	info := m.err
	if info == nil {
		info = &protocol.RuntimeErrorInfo{
			Code:    "SHUTDOWN_FAILED",
			Message: "Runtime cleanup failed.",
		}
	}
	// Python: :495-496 if self.error is None: self.fail(error)
	if m.err == nil {
		m.Fail(info, string(protocol.StatusFailed), 1)
	}
	// Python: :497-503 error = replace(error, details={**details, "cleanup_errors": tuple(...)})
	details := map[string]any{}
	for k, v := range info.Details {
		details[k] = v
	}
	names := make([]any, len(m.cleanupErrors))
	for i, name := range m.cleanupErrors {
		names[i] = name
	}
	details["cleanup_errors"] = names
	m.err = &protocol.RuntimeErrorInfo{
		Code:      info.Code,
		Message:   info.Message,
		Retryable: info.Retryable,
		Details:   details,
	}
}

// Fail 记录失败（只在第一条时生效）。
// Python: machine.py:180-191 _MachineRun.fail
func (m *MachineRun) Fail(errorInfo *protocol.RuntimeErrorInfo, status string, exitCode int) {
	if m.err != nil {
		return
	}
	m.err = errorInfo
	m.status = status
	m.exitCode = exitCode
}

// Status 当前终态。
func (m *MachineRun) Status() string { return m.status }

// ExitCode 当前退出码。
func (m *MachineRun) ExitCode() int { return m.exitCode }

// CleanupErrors 清理失败步骤名。
func (m *MachineRun) CleanupErrors() []string { return m.cleanupErrors }

// NormalizeRunError 识别本次请求触顶导致的迭代停止。
// Python: machine.py:390-401 _MachineRun._normalize_run_error
//
// Python: max_turns is not None and self.limits.model_calls >= max_turns
//
//	and error.code == "MAX_ITERATIONS_REACHED"
func (m *MachineRun) NormalizeRunError(errorInfo *protocol.RuntimeErrorInfo) *protocol.RuntimeErrorInfo {
	if errorInfo == nil || m.runInput == nil {
		return errorInfo
	}
	maxTurns := m.runInput.MaxTurns
	atTurnLimit := maxTurns != nil && m.limits.ModelCalls() >= *maxTurns
	isIterationError := errorInfo.Code == "MAX_ITERATIONS_REACHED"
	if atTurnLimit && isIterationError {
		copied := *errorInfo
		copied.Code = "TURN_LIMIT_EXCEEDED"
		copied.Message = "Run reached max_turns before completion."
		return &copied
	}
	return errorInfo
}

// Result 组装终态结果。
// Python: machine.py:505-516 _MachineRun.result
func (m *MachineRun) Result() *protocol.OneShotRunResult {
	return &protocol.OneShotRunResult{
		SchemaVersion: protocol.CurrentSchemaVersion,
		Sequence:      m.writer.Sequence,
		RequestID:     m.writer.RequestID,
		SessionID:     m.writer.SessionID,
		Status:        m.status,
		ExitCode:      m.exitCode,
		Output:        m.summary.Output(),
		OutputJSON:    m.outputJSON,
		Usage:         m.limits.AddUsage(m.summary.Usage()),
		Error:         m.err,
	}
}

// RunMachine 只在取消/释放自有资源之后返回唯一结果。
// Python: machine.py:519-585 run_machine
//
// 调用方在它返回之后才写出结果，而不是 Runtime 仍活跃时。
// 管道断掉也算一次失败的运行，清理照做。
func RunMachine(
	ctx context.Context,
	runInput *protocol.OneShotRunInput,
	writer *OneShotWriter,
	control any,
	clientFactory func() *InProcessRuntimeClient,
) *protocol.OneShotRunResult {
	run := NewMachineRun(runInput, writer, control)

	// Python: :532-533 context_token = CURRENT_RUN_CONTEXT.set(run.run_context)
	ctx = runtimectx.WithRunContext(ctx, run.runContext)

	// Python: :534 deadline = asyncio.timeout(run_input.timeout_seconds)
	var deadline time.Time
	if runInput.TimeoutSeconds != nil && *runInput.TimeoutSeconds > 0 {
		deadline = time.Now().Add(time.Duration(*runInput.TimeoutSeconds * float64(time.Second)))
	}

	// Python: :536-539 if control is not None: control.start(); async with deadline: await run.execute(...)
	if err := run.Execute(ctx, clientFactory, deadline); err != nil {
		run.classifyFailure(err, writer)
	}

	// Python: :575-584 finally: defer_command_signals(); await run.cleanup();
	//                              session_lease.release(); CURRENT_RUN_CONTEXT.reset(token)
	DeferCommandSignals(nil)
	run.Cleanup(ctx)
	// ⤵️ 回填 20.5：sessionLease.release()（本批恒 nil）
	return run.Result()
}

// RunWithSignals 把 OS 终止信号路由到同一条有界取消/清理路径。
// Python: machine.py:588-596 run_with_signals
func RunWithSignals(
	ctx context.Context,
	runInput *protocol.OneShotRunInput,
	writer *OneShotWriter,
	control any,
) *protocol.OneShotRunResult {
	if ctx == nil {
		ctx = context.Background()
	}
	// Python: :595 with command_signals() as signals:
	//                return await signals.run(run_machine(...))
	signals, restore := CommandSignalsContext()
	defer restore()

	// Python: signals.run(operation) —— 绑定取消函数后执行，finally 置 cleaning
	runCtx, cancel := context.WithCancel(ctx)
	unbind := signals.BindCancel(cancel)
	defer unbind()
	defer cancel()

	return RunMachine(runCtx, runInput, writer, control, nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// permissionLevels 把 permissions.tools 转成 RunContext 的二元组列表。
// Python: machine.py:160-161 tuple(run_input.permissions["tools"].items())
func permissionLevels(permissions map[string]any) []runtimectx.ToolPermissionLevel {
	tools, ok := permissions["tools"].(map[string]any)
	if !ok {
		return nil
	}
	levels := make([]runtimectx.ToolPermissionLevel, 0, len(tools))
	for name, level := range tools {
		text, _ := level.(string)
		levels = append(levels, runtimectx.ToolPermissionLevel{Name: name, Level: text})
	}
	return levels
}

// classifyFailure 把 execute 返回的错误映射为终态。
// Python: machine.py:540-574 五个 except 分支
func (m *MachineRun) classifyFailure(err error, writer *OneShotWriter) {
	if err == nil {
		return
	}
	// Python: :540-548 TimeoutError → TIMEOUT / TIMED_OUT / 124
	if errors.Is(err, context.DeadlineExceeded) {
		m.Fail(&protocol.RuntimeErrorInfo{Code: "TIMEOUT", Message: "Command timed out."},
			string(protocol.StatusTimedOut), 124)
		return
	}
	// Python: :549-561 CancelledError → CANCELLED / CANCELLED / 130
	if errors.Is(err, context.Canceled) {
		m.Fail(&protocol.RuntimeErrorInfo{Code: "CANCELLED", Message: "Command interrupted."},
			string(protocol.StatusCancelled), 130)
		return
	}
	// Python: :562-565 OSError → OUTPUT_CLOSED（流坏了）或 RUNTIME_FAILED
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		code := "RUNTIME_FAILED"
		if writer.Broken {
			code = "OUTPUT_CLOSED"
		}
		m.Fail(&protocol.RuntimeErrorInfo{Code: code, Message: "Command I/O failed."},
			string(protocol.StatusFailed), 1)
		return
	}
	// Python: :572-574 兜底 Exception → _exception_info(error)
	m.Fail(exceptionInfo(err), string(protocol.StatusFailed), 1)
}

// exceptionInfo 把异常转成传输安全的错误详情。
// Python: machine.py:78-94 _exception_info
//
// 未知依赖异常可能带凭证或整段请求，故非本包错误一律用固定文案。
func exceptionInfo(err error) *protocol.RuntimeErrorInfo {
	code := "RUNTIME_FAILED"
	message := "Runtime operation failed."
	details := map[string]any{}
	if err == nil {
		return &protocol.RuntimeErrorInfo{Code: code, Message: message}
	}
	if coder, ok := err.(interface{ CodeValue() string }); ok {
		if value := strings.TrimSpace(coder.CodeValue()); value != "" {
			code = value
		}
	}
	// Python: :84-88 isinstance(error, (MachineRunError, SessionGuardError, DuplexControlError))
	//           才回显原文
	var runErr *MachineRunError
	if errors.As(err, &runErr) {
		message = runErr.Message
		if runErr.Details != nil {
			details = runErr.Details
		}
	} else if boundary, isBoundary := err.(*ProcessCliBoundaryError); isBoundary {
		message = boundary.Message
	}
	info := &protocol.RuntimeErrorInfo{Code: code, Message: message}
	if len(details) > 0 {
		info.Details = details
	}
	return info
}

// executeMode 取本次请求的 mode 名。
func (m *MachineRun) executeMode() string {
	if m.runInput.Mode != nil {
		return *m.runInput.Mode
	}
	return string(protocol.ModeAgentCodeNormal)
}

// describeError 拼接可读描述（诊断用，不进机器结果）。
func describeError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}

var _ = swruntime.RuntimeEvent{}

// consumeEvents 非交互消费：写事件、归约、计量、检查交互与完成。
// Python: machine.py:420-436 _MachineRun.consume_noninteractive
func (m *MachineRun) consumeEvents(events []*swruntime.RuntimeEvent) (bool, error) {
	completed := false
	for _, event := range events {
		// Python: :426 self.observe(event)
		if observeErr := m.Observe(event); observeErr != nil {
			return completed, observeErr
		}
		// Python: :427-428 if self.summary.error is not None: return False
		if m.summary.Error() != nil {
			return false, nil
		}
		// Python: :429-430 if event.ok and (event.is_complete or event_type == "chat.final")
		if event.OK && (event.IsComplete || event.EventType() == "chat.final") {
			completed = true
		}
		// Python: :431-435 交互事件在非交互模式下直接失败
		if _, needsInteraction := interactionEvents[event.EventType()]; needsInteraction {
			return completed, &MachineRunError{
				Message: "This command cannot answer interactions; no approval was granted.",
				Code:    "INTERACTION_REQUIRED",
			}
		}
	}
	return completed, nil
}

// Observe 消费一条事件：写输出、归约、计量限额。
// Python: machine.py:403-418 _MachineRun.observe
func (m *MachineRun) Observe(event *swruntime.RuntimeEvent) error {
	// Python: :404 self.writer.write_event(event)
	//
	// Python 的 writer.write_event 直接收 RuntimeEvent；Go 的 WriteEvent 收
	// protocol.OneShotEvent（序列在 writer 内部管理），故此处先归一。
	if writeErr := m.writer.WriteEvent(oneShotEventOf(event)); writeErr != nil {
		return writeErr
	}
	// Python: :405 self.summary.observe(event)
	m.summary.Observe(event)
	// Python: :406-416 limits.observe(event) 命中即抛 MachineRunError
	if limitCode := m.limits.Observe(event); limitCode != nil {
		return &MachineRunError{
			Message: limitMessage(*limitCode),
			Code:    *limitCode,
		}
	}
	// Python: :417-418 if summary.error is not None: self.fail(normalize(...))
	if summaryErr := m.summary.Error(); summaryErr != nil {
		m.Fail(m.NormalizeRunError(summaryErr), string(protocol.StatusFailed), 1)
	}
	return nil
}

// cleanupStep 执行一步清理，失败只记录不阻断。
// Python: machine.py:438-454 _MachineRun.cleanup_step
func (m *MachineRun) cleanupStep(ctx context.Context, name string, operation func(context.Context) error) {
	// Python: :444 async with asyncio.timeout(SHUTDOWN_TIMEOUT_SECONDS)
	stepCtx, cancel := context.WithTimeout(
		ctx, time.Duration(machineShutdownTimeoutSeconds*float64(time.Second)))
	defer cancel()

	if err := operation(stepCtx); err != nil {
		// Python: :447-448 self.cleanup_errors.append(name) + warning
		m.cleanupErrors = append(m.cleanupErrors, name)
		// Python: :449-454 CancelledError 且尚无错误时置 CANCELLED
		if errors.Is(err, context.Canceled) && m.err == nil {
			m.Fail(&protocol.RuntimeErrorInfo{Code: "CANCELLED", Message: "Command interrupted."},
				string(protocol.StatusCancelled), exitCancelled)
		}
	}
}

// workspaceParams 构造 workspace 相关参数。
// Python: machine.py:118-142 _workspace_params
func workspaceParams(runInput *protocol.OneShotRunInput, resumed bool) map[string]any {
	workspace := runInput.Workspace
	// Python: :120-132 resumed：省略即继承，不用调用方当前目录覆盖持久化绑定
	if resumed {
		if workspace == nil {
			return map[string]any{}
		}
		params := map[string]any{}
		if workspace.Cwd != nil {
			params["cwd"] = *workspace.Cwd
		}
		if workspace.ProjectDir != nil {
			params["project_dir"] = *workspace.ProjectDir
		}
		if len(workspace.TrustedDirs) > 0 {
			params["trusted_dirs"] = stringSliceToAny(workspace.TrustedDirs)
		}
		return params
	}
	// Python: :133-142 新建：缺省用当前目录，project_dir 缺省等于 cwd
	var cwd, projectDir string
	if workspace != nil && workspace.Cwd != nil {
		cwd = *workspace.Cwd
	}
	if workspace != nil && workspace.ProjectDir != nil {
		projectDir = *workspace.ProjectDir
	}
	if cwd == "" {
		if current, err := os.Getwd(); err == nil {
			cwd = current
		}
	}
	if projectDir == "" {
		projectDir = cwd
	}
	trusted := []any{}
	if workspace != nil && len(workspace.TrustedDirs) > 0 {
		trusted = stringSliceToAny(workspace.TrustedDirs)
	}
	if len(trusted) == 0 {
		trusted = []any{projectDir}
	}
	return map[string]any{
		"cwd":          cwd,
		"project_dir":  projectDir,
		"trusted_dirs": trusted,
	}
}

// cancelRequest 构造取消请求。
// Python: machine.py:97-115 _cancel_request
func cancelRequest(request *schema.AgentRequest, sessionScope bool) *schema.AgentRequest {
	// Python: :100 params = request.params or {}
	params := map[string]any{}
	if len(request.Params) > 0 {
		if decoded, err := unmarshalParams(request.Params); err == nil {
			params = decoded
		}
	}
	target := request.RequestID
	if sessionScope {
		target = ""
	}
	// Python: :101-115 AgentRequest(..., req_method=CHAT_CANCEL, is_stream=False,
	//                               params={intent, target_request_id, mode, work_mode, project_dir})
	cancelParams := map[string]any{
		"intent":            "cancel",
		"target_request_id": target,
		"mode":              params["mode"],
		"work_mode":         params["work_mode"],
		"project_dir":       params["project_dir"],
	}
	cancelled := &schema.AgentRequest{
		RequestID: request.RequestID,
		ChannelID: machineChannelID,
		SessionID: request.SessionID,
		ReqMethod: schema.ReqMethodChatCancel,
		IsStream:  false,
	}
	if encoded, err := marshalParams(cancelParams); err == nil {
		cancelled.Params = encoded
	}
	return cancelled
}

// limitMessage 限额错误码对应的说明。
// Python: machine.py:408-412 messages 映射
func limitMessage(code string) string {
	switch code {
	case "BUDGET_METER_UNAVAILABLE":
		return "Model did not report a usable cost for the requested budget."
	case "BUDGET_EXCEEDED":
		return "Run exceeded max_budget_usd."
	case "TURN_LIMIT_EXCEEDED":
		return "Run exceeded max_turns."
	default:
		return "Run exceeded a configured limit."
	}
}

// validateHostToolsForRun 宿主工具前置校验。
// Python: machine.py:205-233
//
// ⤵️ 回填 20.6：validate_host_tool_schemas / HOST_TOOLS_REQUIRE_DUPLEX /
// HOST_TOOL_NOT_ALLOWED 三项依赖 host_tools 模块与 duplex 通道。
// 用户决策：本批 hostTools 非空即报错。
func validateHostToolsForRun(runInput *protocol.OneShotRunInput) error {
	return &MachineRunError{
		Message: "Host tools are not supported yet (see 20.6).",
		Code:    "INVALID_HOST_TOOL",
		Details: map[string]any{"field": "/host_tools", "reason": "unsupported"},
	}
}

// marshalParams 序列化请求参数（不转义 HTML，对齐 Python json.dumps）。
func marshalParams(params map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(params); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// unmarshalParams 反序列化请求参数。
func unmarshalParams(raw []byte) (map[string]any, error) {
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	return params, nil
}

// oneShotEventOf 把 Runtime 事件归一为协议事件（供 writer 补齐稳定公开字段）。
// Python: machine_io.py:236-253 write_event 内部用 public_event_payload
func oneShotEventOf(event *swruntime.RuntimeEvent) *protocol.OneShotEvent {
	if event == nil {
		return &protocol.OneShotEvent{SchemaVersion: protocol.CurrentSchemaVersion}
	}
	eventType := event.EventType()
	if eventType == "" {
		eventType = "runtime.event"
	}
	out := &protocol.OneShotEvent{
		SchemaVersion: protocol.CurrentSchemaVersion,
		EventType:     eventType,
	}
	if payload, ok := protocol.PublicEventPayload(eventType, event.Payload).(map[string]any); ok {
		out.Payload = payload
	}
	return out
}
