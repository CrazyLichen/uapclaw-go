package processcli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// 本文件是 channels/process_cli/app.py 的 Go 对应物。
//
// Python: channels/process_cli/app.py（964 行）
//
// 职责：一条 CLI 命令拥有恰好一个 Runtime 生命周期。四条清理路径
// （cancel → cleanup_session → close → flush_session_writes）每步 5 秒有界。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// shutdownStepTimeoutSeconds 每个清理步骤的最长窗口。
	// Python: app.py:63 SHUTDOWN_STEP_TIMEOUT_SECONDS
	shutdownStepTimeoutSeconds = 5.0
	// interactionUnavailableMessage 交互不可用时的事件文案。
	// Python: app.py:64-66 INTERACTIVE_INPUT_REQUIRED
	interactionUnavailableMessage = "process CLI received an interaction request but interactive input is unavailable"
	// exitInteractionUnavailable 交互不可用退出码。
	// Python: app.py:633 return 4
	exitInteractionUnavailable = 4
	// exitTimeout 超时退出码。
	// Python: app.py:898 return 124
	exitTimeout = 124
	// exitFailure 执行失败退出码。
	// Python: app.py:922 return 1
	exitFailure = 1
	// skillsListOperation 无状态技能查询操作。
	// Python: app.py:49 SKILLS_LIST_OPERATION
	skillsListOperation = "skills.list"
	// sessionCreateOperation 创建并切换会话。
	// Python: app.py:50
	sessionCreateOperation = "session.create"
	// sessionSwitchOperation 切换会话。
	// Python: app.py:51
	sessionSwitchOperation = "session.switch"
	// sessionForkOperation 分支会话。
	// Python: app.py:52
	sessionForkOperation = "session.fork"
	// sessionDeleteOperation 删除会话。
	// Python: app.py:53
	sessionDeleteOperation = "session.delete"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// sessionOperations 四个有状态的会话操作。
// Python: app.py:54-61 SESSION_OPERATIONS
var sessionOperations = map[string]struct{}{
	sessionCreateOperation: {},
	sessionSwitchOperation: {},
	sessionForkOperation:   {},
	sessionDeleteOperation: {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Run 拥有恰好一个 Runtime 生命周期。
// Python: app.py:748-944 run
//
// 返回进程退出码。异常路径与 finally 顺序严格对齐上游。
func Run(args *AppArgs, stdout, stderr io.Writer) int {
	if args == nil {
		return exitFailure
	}
	args.WorkMode = ResolveCliWorkMode(args.Mode, args.WorkMode)

	client := NewInProcessRuntimeClient(nil)
	sessionID := strings.TrimSpace(args.Session)
	requestID := newRequestID()
	operation := args.Operation
	if operation == "" {
		operation = operationChat
	}
	renderer := NewEventRenderer(
		args.Output, stdout, stderr,
		args.ShowReasoning, args.ShowTools, !args.InteractiveWorker,
	)
	renderer.Start()

	var request *schema.AgentRequest
	var runErr error
	var exitCode int

	execute := func() int {
		if err := client.Start(); err != nil {
			return exitFailure
		}
		// Python: :774-781 无状态技能查询
		if operation == skillsListOperation {
			code, req, sid := invokeSkillsList(client, args, renderer, requestID)
			request, sessionID = req, sid
			return code
		}
		// Python: :782-812 四个会话操作
		if _, isSessionOp := sessionOperations[operation]; isSessionOp {
			renderer.Working()
			var sid string
			var err error
			switch operation {
			case sessionCreateOperation:
				sid, err = createSession(client, args, renderer, requestID)
			case sessionSwitchOperation:
				sid, err = switchSession(client, args, renderer, requestID)
			case sessionForkOperation:
				sid, err = forkSession(client, args, renderer, requestID)
			default:
				sid, err = deleteSession(client, args, renderer, requestID)
			}
			if err != nil {
				runErr = err
				return exitFailure
			}
			sessionID = sid
			return 0
		}
		// Python: :813-814 未支持的操作
		if operation != operationChat {
			runErr = fmt.Errorf("unsupported process CLI operation: %s", operation)
			return exitFailure
		}
		// Python: :815-831 普通 chat：开通会话 + 组请求
		resolved, err := client.CreateOrResumeSession(processCliChannelID, optionalString(args.Session))
		if err != nil {
			runErr = err
			return exitFailure
		}
		sessionID = resolved
		_ = writeWorkerResult(args, operation, sessionID, args.Mode, args.WorkMode, args.ProjectDir)

		req, err := buildRequest(args, sessionID, requestID)
		if err != nil {
			runErr = err
			return exitFailure
		}
		request = req
		renderer.Working()
		// Python: :833-868 消费事件流
		return consume(client, request, renderer, args, sessionID, requestID)
	}

	// Python: :870-875 超时包裹执行
	if args.TimeoutSet && args.Timeout > 0 {
		ctx, cancel := context.WithTimeout(
			context.Background(), time.Duration(args.Timeout*float64(time.Second)))
		defer cancel()
		done := make(chan int, 1)
		go func() { done <- execute() }()
		select {
		case exitCode = <-done:
		case <-ctx.Done():
			// Python: :882-898 超时：先取消请求再收敛
			if request != nil && operation == operationChat {
				boundedCleanup(func() error { return client.Cancel(context.Background(), request) })
			}
			renderer.Render(&swruntime.RuntimeEvent{
				RequestID: requestID, ChannelID: processCliChannelID, SessionID: sessionID,
				Payload: map[string]any{
					"event_type": "chat.error",
					"error":      "process CLI execution timed out",
				},
				OK: false,
			}, "")
			renderer.Finish(sessionID, requestID, operation == operationChat)
			cleanupClient(client, sessionID, operation)
			return exitTimeout
		}
	} else {
		exitCode = execute()
	}

	// 清理路径（Python: :904-922 异常分支 + :923-944 finally）
	switch {
	case runErr != nil:
		payload := map[string]any{"event_type": "chat.error", "error": runErr.Error()}
		var provisionErr *swruntime.SessionProvisionError
		if errors.As(runErr, &provisionErr) && provisionErr.Code != "" {
			payload["code"] = provisionErr.Code
		}
		renderer.Render(&swruntime.RuntimeEvent{
			RequestID: requestID, ChannelID: processCliChannelID, SessionID: sessionID,
			Payload: payload,
			OK:      false,
		}, "")
		renderer.Finish(sessionID, requestID, operation == operationChat)
		exitCode = exitFailure
	case exitCode == exitInteractionUnavailable:
		renderer.Finish(sessionID, requestID, operation == operationChat)
	default:
		renderer.Finish(sessionID, requestID, operation == operationChat)
	}

	cleanupClient(client, sessionID, operation)
	return exitCode
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cleanupClient 三段清理：cleanup_session → close → flush（各 5 秒有界）。
// Python: app.py:923-944 finally 三层嵌套
//
// 会话清理被取消时 close 仍须执行；close 超时后 flush 仍有独立窗口。
func cleanupClient(client *InProcessRuntimeClient, sessionID, operation string) {
	defer func() {
		defer boundedCleanup(func() error {
			return FlushSessionWrites(shutdownStepTimeoutSeconds)
		})
		boundedCleanup(client.Close)
	}()
	if sessionID != "" && operation == operationChat {
		boundedCleanup(func() error {
			_, err := client.CleanupSession(processCliChannelID, sessionID)
			return err
		})
	}
}

// boundedCleanup 用 5 秒上限执行一个清理步骤，任何错误吞掉。
// Python: app.py:947-952 _bounded_cleanup
func boundedCleanup(step func() error) {
	if step == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		_ = step()
	}()
	select {
	case <-done:
	case <-time.After(time.Duration(shutdownStepTimeoutSeconds * float64(time.Second))):
	}
}

// buildRequest 组装一轮 chat 请求。
// Python: app.py:74-108 _build_request
//
// 关键不变量：supports_user_interaction 只取决于 InteractiveWorker——
// 非 worker 的普通命令即使 stdin 是 TTY 也必须为 false。
func buildRequest(args *AppArgs, sessionID, requestID string) (*schema.AgentRequest, error) {
	cwd, projectDir := resolvedWorkspace(args)
	trustedDirs := make([]string, 0, len(args.TrustedDirs))
	for _, path := range args.TrustedDirs {
		absolute, err := filepath.Abs(expandHome(path))
		if err == nil {
			trustedDirs = append(trustedDirs, absolute)
			continue
		}
		trustedDirs = append(trustedDirs, path)
	}
	if len(trustedDirs) == 0 {
		trustedDirs = []string{projectDir}
	}

	params := map[string]any{
		"query":                        args.Prompt,
		"content":                      args.Prompt,
		"mode":                         args.Mode,
		"work_mode":                    args.WorkMode,
		"cwd":                          cwd,
		"project_dir":                  projectDir,
		"trusted_dirs":                 trustedDirs,
		"supports_user_interaction":    args.InteractiveWorker,
		"supports_permission_approval": args.InteractiveWorker,
	}
	if selection := strings.TrimSpace(args.ModelSelection); selection != "" {
		params["model_name"] = selection
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("请求参数序列化失败: %w", err)
	}
	sid := sessionID
	return &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: processCliChannelID,
		SessionID: &sid,
		ReqMethod: schema.ReqMethodChatSend,
		IsStream:  true,
		Params:    encoded,
	}, nil
}

// resolvedWorkspace 解析 cwd 与 project_dir。
// Python: app.py:127-130 _resolved_workspace
func resolvedWorkspace(args *AppArgs) (string, string) {
	cwd := strings.TrimSpace(args.Cwd)
	if cwd == "" {
		current, err := os.Getwd()
		if err == nil {
			cwd = current
		}
	} else {
		cwd = expandHome(cwd)
	}
	if absolute, err := filepath.Abs(cwd); err == nil {
		cwd = absolute
	}
	projectDir := strings.TrimSpace(args.ProjectDir)
	if projectDir == "" {
		projectDir = cwd
	} else {
		projectDir = expandHome(projectDir)
	}
	if absolute, err := filepath.Abs(projectDir); err == nil {
		projectDir = absolute
	}
	return cwd, projectDir
}

// writeWorkerResult 把已提交的 worker 状态回执给父 REPL。
// Python: app.py:133-160 _write_worker_result
func writeWorkerResult(
	args *AppArgs, operation, sessionID, mode, workMode, projectDir string,
) error {
	if args.SessionResultFile != "" {
		if err := os.WriteFile(args.SessionResultFile, []byte(sessionID), 0o600); err != nil {
			return err
		}
	}
	if args.WorkerResultFile == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"operation":   operation,
		"session_id":  sessionID,
		"mode":        mode,
		"work_mode":   workMode,
		"project_dir": projectDir,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(args.WorkerResultFile, payload, 0o600)
}

// consume 消费一轮事件流并处理交互。
// Python: app.py:611-718 _consume
func consume(
	client *InProcessRuntimeClient, request *schema.AgentRequest,
	renderer *EventRenderer, args *AppArgs, sessionID, requestID string,
) int {
	interactive, _ := requestParamsBool(request, "supports_user_interaction")
	result := 0
	streamErr := client.Stream(
		context.Background(), request,
		func(event *swruntime.RuntimeEvent) bool {
			renderer.Render(event, "")
			if _, isInteraction := interactionEvents[event.EventType()]; isInteraction {
				nested := handleInteraction(client, request, event, renderer, interactive, requestID)
				if nested != 0 {
					result = nested
					return false
				}
			}
			// Python: :710-711 团队持久流的一轮结束
			if isTerminalTeamEvent(args, event) {
				return false
			}
			return true
		},
		nil,
	)
	if streamErr != nil && result == 0 {
		result = exitFailure
	}
	if result != 0 {
		return result
	}
	if renderer.Failed() {
		return exitFailure
	}
	return 0
}

// handleInteraction 处理一次交互请求。
// Python: app.py:620-692 handle_interaction
func handleInteraction(
	client *InProcessRuntimeClient, original *schema.AgentRequest,
	interaction *swruntime.RuntimeEvent, renderer *EventRenderer,
	interactive bool, requestID string,
) int {
	inputUnavailable := func() int {
		eventRequestID := interaction.RequestID
		if eventRequestID == "" {
			eventRequestID = original.RequestID
		}
		renderer.Render(&swruntime.RuntimeEvent{
			RequestID: eventRequestID,
			ChannelID: original.ChannelID,
			SessionID: eventSessionID(original),
			Payload: map[string]any{
				"event_type": "chat.error",
				"error":      interactionUnavailableMessage,
			},
			OK: false,
		}, "")
		return exitInteractionUnavailable
	}
	// Python: :635-636 非交互命令直接失败
	if !interactive {
		return inputUnavailable()
	}

	renderer.PrepareInteraction()
	_, answers, err := interactionAnswer(interaction.Payload, os.Stdin, renderer)
	if err != nil {
		return inputUnavailable()
	}
	answerReq, resumesStream := answerRequest(original, interaction, answers)
	if resumesStream {
		// Python: :661-671 恢复流：递归消费新请求
		return consume(client, answerReq, renderer, &AppArgs{Mode: stringOr(original.Params)}, eventSessionID(original), requestID)
	}
	// Python: :675-691 非恢复：answer_interaction 逐事件渲染
	events, err := client.AnswerInteraction(context.Background(), answerReq)
	if err != nil {
		return exitFailure
	}
	for _, event := range events {
		renderer.Render(event, "")
		if _, isInteraction := interactionEvents[event.EventType()]; isInteraction {
			nested := handleInteraction(client, answerReq, event, renderer, interactive, requestID)
			if nested != 0 {
				return nested
			}
		}
	}
	if renderer.Failed() {
		return exitFailure
	}
	return 0
}

// answerRequest 构造应答请求。
// Python: app.py:538-574 _answer_request
//
// resume 判定：source ∈ INTERRUPT_RESUME_SOURCES 且 request_id 非空。
func answerRequest(
	original *schema.AgentRequest, interaction *swruntime.RuntimeEvent, answers []map[string]any,
) (*schema.AgentRequest, bool) {
	payload := interaction.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	source := stringOr(payload["source"])
	interactionRequestID := stringOr(payload["request_id"])
	interruptID := stringOr(payload["interrupt_id"])
	_, resumeSource := swruntime.InterruptResumeSources[source]
	resume := resumeSource && interactionRequestID != ""

	originalParams := map[string]any{}
	if original.Params != nil {
		_ = json.Unmarshal(original.Params, &originalParams)
	}
	params := map[string]any{
		"session_id":                   eventSessionID(original),
		"request_id":                   interactionRequestID,
		"answers":                      answers,
		"source":                       source,
		"mode":                         originalParams["mode"],
		"work_mode":                    originalParams["work_mode"],
		"project_dir":                  originalParams["project_dir"],
		"cwd":                          originalParams["cwd"],
		"trusted_dirs":                 originalParams["trusted_dirs"],
		"supports_user_interaction":    paramBoolOr(originalParams, "supports_user_interaction", true),
		"supports_permission_approval": paramBoolOr(originalParams, "supports_permission_approval", true),
	}
	// Python: :551 仅在非空时才放 interrupt_id
	if interruptID != "" {
		params["interrupt_id"] = interruptID
	}
	// Python: :561 resume 时 query 为空串，否则不设
	if resume {
		params["query"] = ""
	}

	encoded, _ := json.Marshal(params)
	reqMethod := schema.ReqMethodChatAnswer
	if resume {
		reqMethod = schema.ReqMethodChatSend
	}
	sid := eventSessionID(original)
	return &schema.AgentRequest{
		RequestID: newRequestIDWithPrefix("answer"),
		ChannelID: original.ChannelID,
		SessionID: &sid,
		ReqMethod: reqMethod,
		IsStream:  resume,
		Params:    encoded,
	}, resume
}

// cancelRequest 构造取消请求。
// Python: app.py:577-593 _cancel_request
//
// request_id 沿用原请求（CHAT_CANCEL 要指向在飞执行本身，不能换新 id）。
func appCancelRequest(original *schema.AgentRequest) *schema.AgentRequest {
	originalParams := map[string]any{}
	if original.Params != nil {
		_ = json.Unmarshal(original.Params, &originalParams)
	}
	encoded, _ := json.Marshal(map[string]any{
		"intent":            "cancel",
		"target_request_id": original.RequestID,
		"mode":              originalParams["mode"],
		"work_mode":         originalParams["work_mode"],
		"project_dir":       originalParams["project_dir"],
	})
	sid := eventSessionID(original)
	return &schema.AgentRequest{
		RequestID: original.RequestID,
		ChannelID: original.ChannelID,
		SessionID: &sid,
		ReqMethod: schema.ReqMethodChatCancel,
		Params:    encoded,
	}
}

// isTerminalTeamEvent 持久 Team 流是否完成了当前轮。
// Python: app.py:596-608 _is_terminal_team_event
//
// 只对团队模式生效；单 Agent 的同形事件不是终态。
func isTerminalTeamEvent(args *AppArgs, event *swruntime.RuntimeEvent) bool {
	if args == nil || event == nil {
		return false
	}
	if !isTeamModeText(ResolveCliWorkMode(args.Mode, args.WorkMode)) &&
		!isTeamModeText(deprecateMode(args.Mode)) {
		return false
	}
	if event.EventType() != "chat.processing_status" {
		return false
	}
	payload := event.Payload
	if payload == nil {
		return false
	}
	isProcessing, okProcessing := payload["is_processing"].(bool)
	isComplete, okComplete := payload["is_complete"].(bool)
	return okProcessing && okComplete && !isProcessing && isComplete
}

// isTeamModeText 是否团队模式（转发 schema）。
func isTeamModeText(mode string) bool {
	return swschema.IsTeamMode(mode)
}

// invokeSkillsList 执行无状态技能查询（不开通会话）。
// Python: app.py:721-734 _invoke_skills_list
func invokeSkillsList(
	client *InProcessRuntimeClient, args *AppArgs,
	renderer *EventRenderer, requestID string,
) (int, *schema.AgentRequest, string) {
	sessionID := args.Session
	request := buildSkillsListRequest(args, requestID)
	renderer.Working()
	events, err := client.Invoke(context.Background(), request)
	if err != nil {
		return exitFailure, request, sessionID
	}
	for _, event := range events {
		renderer.Render(event, skillsListOperation)
	}
	if renderer.Failed() {
		return exitFailure, request, sessionID
	}
	return 0, request, sessionID
}

// buildSkillsListRequest 组技能查询请求。
// Python: app.py:111-124 _build_skills_list_request
func buildSkillsListRequest(args *AppArgs, requestID string) *schema.AgentRequest {
	req := &schema.AgentRequest{
		RequestID: requestID,
		ChannelID: processCliChannelID,
		ReqMethod: schema.ReqMethodSkillsList,
		IsStream:  false,
		Params:    []byte("{}"),
	}
	if strings.TrimSpace(args.Session) != "" {
		sid := strings.TrimSpace(args.Session)
		req.SessionID = &sid
	}
	return req
}

// paramBoolOr 取参数中的 bool，缺失用默认值。
func paramBoolOr(params map[string]any, key string, fallback bool) bool {
	if value, ok := params[key].(bool); ok {
		return value
	}
	return fallback
}

// requestParamsBool 解析请求参数中的 bool。
func requestParamsBool(request *schema.AgentRequest, key string) (bool, bool) {
	params := map[string]any{}
	if request.Params != nil {
		_ = json.Unmarshal(request.Params, &params)
	}
	value, ok := params[key].(bool)
	return value, ok
}

// eventSessionID 取请求的会话 ID（nil 返回空串）。
func eventSessionID(request *schema.AgentRequest) string {
	if request == nil || request.SessionID == nil {
		return ""
	}
	return *request.SessionID
}

// optionalString 把空串视为 nil。
func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// newRequestIDWithPrefix 生成带前缀的请求标识。
// Python: app.py:70-71 _new_request_id
func newRequestIDWithPrefix(prefix string) string {
	return prefix + "-" + shortHex(12)
}

// ──────────────────────────── 会话操作 ────────────────────────────

// createSession 创建并切换到一个新会话。
// Python: app.py:222-319 _create_session
//
// 时机：先渲染结果与写 worker 回执，再在 AFTER_RESULT_DELIVERY 提交，
// 与 AgentServer 的「结果先可见」契约一致。
func createSession(
	client *InProcessRuntimeClient, args *AppArgs,
	renderer *EventRenderer, requestID string,
) (string, error) {
	arguments := strings.ToLower(strings.TrimSpace(args.Prompt))
	switch arguments {
	case "", "--persist", "--persist-session":
	default:
		return "", &swruntime.SessionProvisionError{
			Message: "usage: /new [--persist|--persist-session]",
			Code:    "BAD_REQUEST",
		}
	}
	cwd, projectDir := resolvedWorkspace(args)
	previous := ownedSessionDescriptor(client, args.Session)

	input := swruntime.SessionCreateInput{
		ChannelID:              processCliChannelID,
		CreateToken:            "process-cli:" + requestID,
		PersistSession:         arguments != "",
		PersistSessionSupplied: arguments != "",
		Mode:                   args.Mode,
		IsSwarm:                isTeamModeText(args.Mode),
		TeamHint:               isTeamModeText(args.Mode),
		// Process CLI 工作区刻意不绑工程：cwd 是命令执行位置，
		// 不因为一个文件系统路径就虚构出 Project 绑定。
		ProjectDir: "",
		Cwd:        cwd,
		WorkMode:   args.WorkMode,
	}
	if previous != nil {
		input.PreviousSessionID = previous.SessionID
		input.PreviousMode = previous.Mode
	}

	prepared, err := client.PrepareSessionCreate(context.Background(), input)
	defer abortPreparedSession(client, prepared)
	if err != nil {
		return "", err
	}
	result, ok := prepared.Result().(*swruntime.SessionCreateResult)
	if !ok {
		return "", fmt.Errorf("session create result has unexpected type")
	}

	sessionID := result.SessionID
	renderer.Render(&swruntime.RuntimeEvent{
		RequestID: requestID, ChannelID: processCliChannelID, SessionID: sessionID,
		Payload: map[string]any{
			"event_type":      "session.created",
			"session_id":      sessionID,
			"mode":            result.CanonicalMode,
			"work_mode":       result.WorkMode,
			"project_dir":     result.ProjectDir,
			"persist_session": result.PersistSession,
			"prewarm_hit":     result.PrewarmHit,
			"prewarm_status":  result.PrewarmStatus,
			"created":         result.Created,
		},
		OK: true,
	}, "")

	targetProjectDir := result.ProjectDir
	if targetProjectDir == "" {
		targetProjectDir = projectDir
	}
	_ = writeWorkerResult(args, sessionCreateOperation, sessionID,
		result.CanonicalMode, result.WorkMode, targetProjectDir)

	// Python: :291-312 提交失败不推翻已交付的成功结果
	if _, commitErr := client.CommitSessionProvision(
		prepared,
		swruntime.SessionProvisionCommitTimingAfterResultDelivery,
		&swruntime.SessionProvisionCommitContext{
			ForegroundScopeID: fmt.Sprintf("process-cli:%d:%s", os.Getpid(), requestID),
		},
	); commitErr != nil {
		_ = commitErr
	}
	return sessionID, nil
}

// switchSession 切换到已有会话。
// Python: app.py:321-384 _switch_session
//
// 时机：BEFORE_RESULT_DELIVERY 提交，再写回执与事件。
func switchSession(
	client *InProcessRuntimeClient, args *AppArgs,
	renderer *EventRenderer, requestID string,
) (string, error) {
	target := strings.TrimSpace(args.Prompt)
	if target == "" {
		return "", &swruntime.SessionProvisionError{Message: "session_id is required", Code: "BAD_REQUEST"}
	}
	targetDescriptor := ownedSessionDescriptor(client, target)
	if targetDescriptor == nil {
		return "", &swruntime.SessionProvisionError{Message: "session not found", Code: "NOT_FOUND"}
	}
	previous := ownedSessionDescriptor(client, args.Session)
	targetMode := targetDescriptor.Mode
	if targetMode == "" {
		targetMode = args.Mode
	}
	targetWorkMode := descriptorWorkMode(targetDescriptor, args.WorkMode)

	input := swruntime.SessionSwitchInput{
		ChannelID:       processCliChannelID,
		TargetSessionID: target,
		Mode:            targetMode,
		TeamHint:        isTeamModeText(targetMode),
	}
	if previous != nil {
		input.PreviousSessionID = previous.SessionID
		input.PreviousMode = previous.Mode
	}

	prepared, err := client.PrepareSessionSwitch(context.Background(), input)
	defer abortPreparedSession(client, prepared)
	if err != nil {
		return "", err
	}
	raw, err := client.CommitSessionProvision(
		prepared,
		swruntime.SessionProvisionCommitTimingBeforeResultDelivery,
		&swruntime.SessionProvisionCommitContext{
			ForegroundScopeID: fmt.Sprintf("process-cli:%d:%s", os.Getpid(), requestID),
		},
	)
	if err != nil {
		return "", err
	}
	result, ok := raw.(*swruntime.SessionSwitchResult)
	if !ok {
		return "", fmt.Errorf("session switch result has unexpected type")
	}

	_ = writeWorkerResult(args, sessionSwitchOperation, result.SessionID,
		targetMode, targetWorkMode, targetDescriptor.ProjectDir)
	renderer.Render(&swruntime.RuntimeEvent{
		RequestID: requestID, ChannelID: processCliChannelID, SessionID: result.SessionID,
		Payload: map[string]any{
			"event_type": "session.switched",
			"session_id": result.SessionID,
			"mode":       targetMode,
			"switched":   result.Switched,
		},
		OK: true,
	}, "")
	return result.SessionID, nil
}

// forkSession 从当前会话创建分支。
// Python: app.py:387-441 _fork_session
func forkSession(
	client *InProcessRuntimeClient, args *AppArgs,
	renderer *EventRenderer, requestID string,
) (string, error) {
	source := strings.TrimSpace(args.Session)
	if source == "" {
		return "", &swruntime.SessionProvisionError{
			Message: "no current session to branch", Code: "BAD_REQUEST",
		}
	}
	sourceDescriptor := ownedSessionDescriptor(client, source)
	if sourceDescriptor == nil {
		return "", &swruntime.SessionProvisionError{
			Message: "source session not found", Code: "NOT_FOUND",
		}
	}

	prepared, err := client.PrepareSessionFork(context.Background(), swruntime.SessionForkInput{
		ChannelID:       processCliChannelID,
		SourceSessionID: source,
		Title:           strings.TrimSpace(args.Prompt),
	})
	defer abortPreparedSession(client, prepared)
	if err != nil {
		return "", err
	}
	raw, err := client.CommitSessionProvision(
		prepared,
		swruntime.SessionProvisionCommitTimingBeforeResultDelivery, nil,
	)
	if err != nil {
		return "", err
	}
	result, ok := raw.(*swruntime.SessionForkResult)
	if !ok {
		return "", fmt.Errorf("session fork result has unexpected type")
	}

	mode := sourceDescriptor.Mode
	if mode == "" {
		mode = args.Mode
	}
	_ = writeWorkerResult(args, sessionForkOperation, result.SessionID,
		mode, descriptorWorkMode(sourceDescriptor, args.WorkMode), sourceDescriptor.ProjectDir)
	renderer.Render(&swruntime.RuntimeEvent{
		RequestID: requestID, ChannelID: processCliChannelID, SessionID: result.SessionID,
		Payload: map[string]any{
			"event_type":        "session.forked",
			"source_session_id": result.SourceSessionID,
			"session_id":        result.SessionID,
			"title":             result.Title,
		},
		OK: true,
	}, "")
	return result.SessionID, nil
}

// deleteSession 删除指定会话。
// Python: app.py:444-479 _delete_session
//
// 无 prepare/commit，直接走 Runtime 的删除事务。
func deleteSession(
	client *InProcessRuntimeClient, args *AppArgs,
	renderer *EventRenderer, requestID string,
) (string, error) {
	target := strings.TrimSpace(args.Prompt)
	if target == "" {
		return "", &swruntime.SessionProvisionError{Message: "session_id is required", Code: "BAD_REQUEST"}
	}
	if ownedSessionDescriptor(client, target) == nil {
		return "", &swruntime.SessionProvisionError{Message: "session not found", Code: "NOT_FOUND"}
	}
	result, err := client.DeleteSession(context.Background(), processCliChannelID, target)
	if err != nil {
		return "", err
	}
	if !result.OK {
		code := result.ErrorCode
		return "", &swruntime.SessionProvisionError{
			Message: result.ErrorMessage, Code: code,
		}
	}
	current := strings.TrimSpace(args.Session)
	_ = writeWorkerResult(args, sessionDeleteOperation, current,
		args.Mode, args.WorkMode, args.ProjectDir)
	renderer.Render(&swruntime.RuntimeEvent{
		RequestID: requestID, ChannelID: processCliChannelID, SessionID: target,
		Payload: map[string]any{"event_type": "session.deleted", "session_id": target},
		OK:      true,
	}, "")
	return current, nil
}

// ──────────────────────────── 会话操作助手 ────────────────────────────

// ownedSessionDescriptor 取本渠道拥有且存在的会话描述符；否则 nil。
// Python: app.py:200-212 _owned_session_descriptor
//
// 外部渠道的会话与不存在的会话一律返回 nil（不泄露「存在但属于别人」）。
func ownedSessionDescriptor(client *InProcessRuntimeClient, sessionID string) *sessionpkg.SessionDescriptor {
	target := strings.TrimSpace(sessionID)
	if target == "" {
		return nil
	}
	descriptor, err := client.DescribeSession(target)
	if err != nil || descriptor == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(descriptor.ChannelID), processCliChannelID) {
		return nil
	}
	return descriptor
}

// descriptorWorkMode 由描述符推断 work_mode。
// Python: app.py:215-219 _descriptor_work_mode
func descriptorWorkMode(descriptor *sessionpkg.SessionDescriptor, fallback string) string {
	if descriptor == nil {
		return fallback
	}
	return ResolveCliWorkMode(descriptor.Mode, descriptor.WorkMode)
}

// abortPreparedSession 放弃仍处 PREPARED 的准备态。
// Python: app.py:180-197 _abort_prepared_session
//
// 取消发生在结果已刷新但提交尚未进入 Runtime 之间时，必须 abort，
// 否则 Runtime 关闭会继承一个未完成的 create 事务。
func abortPreparedSession(client *InProcessRuntimeClient, prepared *swruntime.PreparedSessionProvision) {
	if prepared == nil || prepared.State() != swruntime.SessionProvisionStatePrepared {
		return
	}
	_ = client.AbortSessionProvision(prepared)
}

// ──────────────────────────── 交互应答 ────────────────────────────

// interactionQuestions 归一交互负载的提问列表。
// Python: app.py:513-519 _interaction_questions
func interactionQuestions(payload map[string]any) []map[string]any {
	if raw, ok := payload["questions"].([]any); ok {
		prompts := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			if mapping, ok := item.(map[string]any); ok {
				prompts = append(prompts, mapping)
			}
		}
		if len(prompts) > 0 {
			return prompts
		}
	}
	return []map[string]any{payload}
}

// interactionAnswers 把一行答案转成 Runtime 期望的结构。
// Python: app.py:499-510 _interaction_answers
func interactionAnswers(answer string, options []map[string]any) []map[string]any {
	selected := answer
	if len(options) > 0 && isDecimalText(answer) {
		index := 0
		for _, r := range answer {
			index = index*10 + int(r-'0')
		}
		index--
		if index >= 0 && index < len(options) {
			if value, ok := options[index]["value"].(string); ok && value != "" {
				selected = value
			} else if label, ok := options[index]["label"].(string); ok && label != "" {
				selected = label
			}
		}
	}
	return []map[string]any{{"selected_options": []string{selected}, "custom_input": answer}}
}

// interactionAnswer 逐题渲染提示并读一行答案。
// Python: app.py:522-535 _interaction_answer
func interactionAnswer(
	payload map[string]any, stdin io.Reader, renderer *EventRenderer,
) (string, []map[string]any, error) {
	reader := bufio.NewReader(stdin)
	answers := make([]map[string]any, 0, 1)
	answer := ""
	for _, question := range interactionQuestions(payload) {
		options := renderInteractionPrompt(question, renderer.stderr)
		line, err := reader.ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return "", nil, io.EOF
		}
		answer = strings.TrimSpace(line)
		answers = append(answers, interactionAnswers(answer, options)...)
	}
	return answer, answers, nil
}

// renderInteractionPrompt 打印一道提问及其选项。
// Python: app.py:482-496 _render_interaction_prompt
func renderInteractionPrompt(payload map[string]any, stream io.Writer) []map[string]any {
	prompt := stringOr(payload["question"])
	if prompt == "" {
		prompt = stringOr(payload["message"])
	}
	if prompt == "" {
		prompt = "需要输入"
	}
	fmt.Fprintf(stream, "\n? %s\n", prompt)

	options := make([]map[string]any, 0)
	if raw, ok := payload["options"].([]any); ok {
		for _, item := range raw {
			if mapping, ok := item.(map[string]any); ok {
				options = append(options, mapping)
			}
		}
	}
	for index, option := range options {
		label := stringOr(option["label"])
		if label == "" {
			label = stringOr(option["value"])
		}
		if label == "" {
			label = "?"
		}
		suffix := ""
		if description := stringOr(option["description"]); description != "" {
			suffix = " — " + description
		}
		fmt.Fprintf(stream, "  %d. %s%s\n", index+1, label, suffix)
	}
	fmt.Fprint(stream, "请输入选项或自定义内容：")
	return options
}

// isDecimalText 判断字符串是否全为十进制数字。
func isDecimalText(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// shortHex 生成 n 字符的十六进制随机串。
func shortHex(n int) string {
	value, err := randomHex((n + 1) / 2)
	if err != nil {
		return strings.Repeat("0", n)
	}
	if len(value) > n {
		return value[:n]
	}
	return value
}
