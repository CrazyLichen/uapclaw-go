package browser_move

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserService 浏览器后端服务，提供粘性会话和守护护栏。
//
// 对齐 Python: openjiuwen/harness/tools/browser_move/playwright_runtime/service.py (BrowserService)
type BrowserService struct {
	// Provider 模型提供者
	Provider string
	// APIKey API 密钥
	APIKey string
	// APIBase API 基础 URL
	APIBase string
	// ModelName 模型名称
	ModelName string
	// MCPCfg MCP 服务器配置
	MCPCfg *mcptypes.McpServerConfig
	// Guardrails 浏览器运行守护护栏
	Guardrails *BrowserRunGuardrails

	// started 服务是否已启动
	started bool
	// managedDriver 托管浏览器驱动，⤵️ 9.38-49 回填
	managedDriver any
	// browserAgent Worker Agent 实例
	browserAgent *agents.ReActAgent

	// mu 读写锁，保护内部状态
	mu sync.RWMutex
	// sessions 已注册的会话集合
	sessions map[string]struct{}
	// locksPerSession 会话级别的互斥锁
	locksPerSession map[string]*sync.Mutex
	// cancelStore 取消标记存储
	cancelStore kv.BaseKVStore
	// progressBySession 会话进度状态映射
	progressBySession map[string]*BrowserTaskProgressState
	// failureContextBySession 会话失败上下文映射
	failureContextBySession map[string]string
	// connectionHealthy 连接健康状态
	connectionHealthy bool
	// lastHeartbeatOK 最近一次心跳成功时间
	lastHeartbeatOK *time.Time
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxIterationMessage 最大迭代次数未完成消息
	// 对齐 Python: MAX_ITERATION_MESSAGE
	MaxIterationMessage = "Max iterations reached without completion"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserService 创建新的浏览器后端服务实例。
//
// 对齐 Python: BrowserService.__init__
func NewBrowserService(
	provider, apiKey, apiBase, modelName string,
	mcpCfg *mcptypes.McpServerConfig,
	guardrails *BrowserRunGuardrails,
	cancelStore kv.BaseKVStore,
) *BrowserService {
	if cancelStore == nil {
		cancelStore = kv.NewInMemoryKVStore()
	}
	return &BrowserService{
		Provider:                provider,
		APIKey:                  apiKey,
		APIBase:                 apiBase,
		ModelName:               modelName,
		MCPCfg:                  mcpCfg,
		Guardrails:              guardrails,
		cancelStore:             cancelStore,
		sessions:                make(map[string]struct{}),
		locksPerSession:         make(map[string]*sync.Mutex),
		progressBySession:       make(map[string]*BrowserTaskProgressState),
		failureContextBySession: make(map[string]string),
	}
}

// SessionNew 创建新的浏览器会话，返回会话标识。
// 如果 sessionID 为空则自动生成。
//
// 对齐 Python: BrowserService.session_new
func (s *BrowserService) SessionNew(sessionID string) string {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = "browser-" + uuid.New().String()[:8]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sid] = struct{}{}
	if _, ok := s.locksPerSession[sid]; !ok {
		s.locksPerSession[sid] = &sync.Mutex{}
	}
	return sid
}

// RequestCancel 请求取消指定会话/请求的执行。
//
// 对齐 Python: BrowserService.request_cancel
func (s *BrowserService) RequestCancel(ctx context.Context, sessionID, requestID string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return fmt.Errorf("session_id is required for cancellation")
	}
	rid := strings.TrimSpace(requestID)
	return s.cancelStore.Set(ctx, cancelKey(sid, rid), []byte("1"))
}

// ClearCancel 清除指定会话/请求的取消标记。
//
// 对齐 Python: BrowserService.clear_cancel
func (s *BrowserService) ClearCancel(ctx context.Context, sessionID, requestID string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil
	}
	rid := strings.TrimSpace(requestID)
	if rid != "" {
		return s.cancelStore.Delete(ctx, cancelKey(sid, rid))
	}
	return s.cancelStore.Delete(ctx, cancelKey(sid, "*"))
}

// IsCancelled 检查指定会话/请求是否已被取消。
//
// 对齐 Python: BrowserService.is_cancelled
func (s *BrowserService) IsCancelled(ctx context.Context, sessionID, requestID string) (bool, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false, nil
	}
	rid := strings.TrimSpace(requestID)
	if rid != "" {
		exact, err := s.cancelStore.Get(ctx, cancelKey(sid, rid))
		if err != nil {
			return false, err
		}
		if exact != nil {
			return true, nil
		}
	}
	wildcard, err := s.cancelStore.Get(ctx, cancelKey(sid, "*"))
	if err != nil {
		return false, err
	}
	return wildcard != nil, nil
}

// RecordToolProgress 记录工具执行的进度信息。
//
// 对齐 Python: BrowserService.record_tool_progress
func (s *BrowserService) RecordToolProgress(sessionID, requestID, toolName string, toolResult any) {
	updateProgressFromToolObservation(s, sessionID, requestID, toolName, toolResult)
}

// RecordWorkerProgress 记录 Worker 执行结果的进度信息。
//
// 对齐 Python: BrowserService.record_worker_progress
func (s *BrowserService) RecordWorkerProgress(sessionID, requestID string, parsed map[string]any) {
	updateProgressFromWorkerResult(s, sessionID, requestID, parsed)
}

// GetProgressState 获取指定会话的进度状态。
//
// 对齐 Python: BrowserService.get_progress_state
func (s *BrowserService) GetProgressState(sessionID string) *BrowserTaskProgressState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.progressBySession[strings.TrimSpace(sessionID)]
}

// ExportProgressState 导出指定会话的进度状态为字典，空状态返回 nil。
//
// 对齐 Python: BrowserService.export_progress_state
func (s *BrowserService) ExportProgressState(sessionID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.progressBySession[strings.TrimSpace(sessionID)]
	if state == nil {
		return nil
	}
	if isEmptyProgressState(state) {
		return nil
	}
	return state.ToDict()
}

// SetProgressState 设置指定会话的进度状态。空状态时移除。
//
// 对齐 Python: BrowserService.set_progress_state
func (s *BrowserService) SetProgressState(sessionID string, state *BrowserTaskProgressState) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.IsEmpty() {
		delete(s.progressBySession, sid)
		return
	}
	s.progressBySession[sid] = state
}

// ClearProgressState 清除指定会话的进度状态。
//
// 对齐 Python: BrowserService.clear_progress_state
func (s *BrowserService) ClearProgressState(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.progressBySession, sid)
}

// BuildProgressContext 根据进度状态构建上下文文本。
//
// 对齐 Python: BrowserService.build_progress_context
func BuildProgressContext(state *BrowserTaskProgressState) string {
	return buildProgressContext(state)
}

// BuildFailureSummary 构建失败摘要，供后续重试时使用。
//
// 对齐 Python: BrowserService.build_failure_summary
func (s *BrowserService) BuildFailureSummary(
	task, errStr, pageURL, pageTitle, final string,
	screenshot any,
	attempt int,
	progressState *BrowserTaskProgressState,
) string {
	return buildFailureSummary(task, errStr, pageURL, pageTitle, final, screenshot, attempt, progressState)
}

// ShouldTreatAsCompleted 判断是否应将结果视为已完成。
//
// 对齐 Python: BrowserService.should_treat_as_completed
func ShouldTreatAsCompleted(parsed map[string]any) bool {
	return shouldTreatAsCompleted(parsed)
}

// NormalizeProgressStatus 规范化进度状态字符串。
// complete/completed/done → completed，partial/in_progress/in-progress → partial，
// blocked → blocked，failed → failed。
//
// 对齐 Python: BrowserService._normalize_progress_status
func NormalizeProgressStatus(value any) string {
	normalized := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", value)))
	aliases := map[string]string{
		"complete":    "completed",
		"completed":   "completed",
		"done":        "completed",
		"partial":     "partial",
		"in_progress": "partial",
		"in-progress": "partial",
		"blocked":     "blocked",
		"failed":      "failed",
	}
	if result, ok := aliases[normalized]; ok {
		return result
	}
	return ""
}

// NormalizeScreenshotValue 规范化截图值，本地路径 → data URL。
//
// 对齐 Python: BrowserService._normalize_screenshot_value
// ⤵️ 9.38-49 回填：本地路径转 data URL 的文件系统逻辑
func NormalizeScreenshotValue(screenshot any) any {
	if screenshot == nil {
		return screenshot
	}
	strVal, ok := screenshot.(string)
	if !ok {
		return screenshot
	}
	raw := strings.TrimSpace(strVal)
	if raw == "" {
		return nil
	}
	lowered := strings.ToLower(raw)
	if strings.HasPrefix(lowered, "http://") ||
		strings.HasPrefix(lowered, "https://") ||
		strings.HasPrefix(lowered, "data:image/") {
		return raw
	}
	// TODO: ⤵️ 9.38-49 回填：本地路径解析、截图文件夹拷贝、data URL 转换
	return raw
}

// IsRetryableTransportMessage 判断是否为可重试的传输层错误消息。
//
// 对齐 Python: BrowserService._is_retryable_transport_message
func IsRetryableTransportMessage(text string) bool {
	lowered := strings.ToLower(text)
	markers := []string{
		"session terminated",
		"not connected",
		"endofstream",
		"closedresourceerror",
		"brokenresourceerror",
		"stream closed",
		"connection closed",
		"broken pipe",
		"remoteprotocolerror",
		"readerror",
		"writeerror",
	}
	for _, marker := range markers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// IsRetryableRuntimeResult 判断运行时结果是否为可重试错误。
//
// 对齐 Python: BrowserService._is_retryable_runtime_result
func IsRetryableRuntimeResult(parsed map[string]any) bool {
	if parsed == nil {
		return false
	}
	if okVal, ok := parsed["ok"]; ok && isTruthy(okVal) {
		return false
	}
	text := strings.ToLower(
		fmt.Sprintf("%v\n%v", parsed["error"], parsed["final"]),
	)
	markers := []string{
		"frame has been detached",
		"execution context was destroyed",
		"target page, context or browser has been closed",
		"target closed",
		"navigation failed because browser has disconnected",
		"context closed",
		"page crashed",
		"net::err_network_changed",
		"net::err_internet_disconnected",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// EnsureRuntimeReady 确保浏览器运行时已就绪。
//
// 对齐 Python: BrowserService.ensure_runtime_ready
// TODO: ⤵️ 9.38-49 回填 ManagedBrowserDriver 逻辑
func (s *BrowserService) EnsureRuntimeReady(_ context.Context) error {
	// TODO: ⤵️ 9.38-49 回填 ManagedBrowserDriver 逻辑
	return nil
}

// EnsureStarted 确保浏览器服务已启动（运行时就绪 + Worker Agent 已构建）。
//
// 对齐 Python: BrowserService.ensure_started
// TODO: ⤵️ 9.38-49 回填 BuildBrowserWorkerAgent
func (s *BrowserService) EnsureStarted(ctx context.Context) error {
	if err := s.EnsureRuntimeReady(ctx); err != nil {
		return err
	}
	// TODO: ⤵️ 9.38-49 回填 BuildBrowserWorkerAgent
	return nil
}

// Shutdown 关闭浏览器服务。
//
// 对齐 Python: BrowserService.shutdown
func (s *BrowserService) Shutdown(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = false
	s.connectionHealthy = false
	s.managedDriver = nil
	s.browserAgent = nil
	return nil
}

// RunTask 执行浏览器任务，支持 retry_once + resume_on_max_iterations + transport error 重启。
//
// 对齐 Python: BrowserService.run_task (service.py:1263-1494)
func (s *BrowserService) RunTask(
	ctx context.Context,
	task, sessionID, requestID string,
	timeoutS *int,
) (map[string]any, error) {
	if err := s.EnsureStarted(ctx); err != nil {
		return nil, err
	}

	sid := s.SessionNew(sessionID)
	rid := strings.TrimSpace(requestID)
	if rid == "" {
		rid = uuid.New().String()[:8]
	}

	effectiveTimeout := s.Guardrails.TimeoutS
	if timeoutS != nil && *timeoutS > 0 {
		effectiveTimeout = *timeoutS
	}

	attempts := 1
	if s.Guardrails.RetryOnce {
		attempts = 2
	}
	baseTask := strings.TrimSpace(task)

	s.mu.RLock()
	previousFailureSummary := s.failureContextBySession[sid]
	s.mu.RUnlock()

	// 获取会话锁
	lock := s.getLock(sid)
	lock.Lock()
	defer lock.Unlock()

	// 检查是否已取消
	cancelled, err := s.IsCancelled(ctx, sid, rid)
	if err != nil {
		return nil, err
	}
	if cancelled {
		_ = s.ClearCancel(ctx, sid, rid)
		_ = s.ClearCancel(ctx, sid, "")
		return map[string]any{
			"ok":              false,
			"session_id":      sid,
			"request_id":      rid,
			"final":           "",
			"page":            map[string]any{"url": "", "title": ""},
			"screenshot":      nil,
			"error":           "cancelled_by_frontend",
			"attempt":         0,
			"failure_summary": nil,
			"progress_state":  nil,
		}, nil
	}

	var lastError string
	usedMaxIterationResume := false
	nextTask := buildTaskWithFailureContext(baseTask, previousFailureSummary)
	attemptIdx := 0
	maxAttempts := attempts
	if s.Guardrails.ResumeOnMaxIterations {
		maxAttempts++
	}
	lastFailureFinal := ""
	var lastFailurePage map[string]any
	var lastFailureScreenshot any

	for attemptIdx < maxAttempts {
		attemptIdx++

		// 执行单次任务（带超时）
		parsed, execErr := s.runTaskOnceWithTimeout(ctx, nextTask, sid, rid, effectiveTimeout)

		if execErr != nil {
			// 超时或其他异常
			if ctx.Err() == context.Canceled {
				// 被取消
				_ = s.ClearCancel(ctx, sid, rid)
				_ = s.ClearCancel(ctx, sid, "")
				return map[string]any{
					"ok":              false,
					"session_id":      sid,
					"request_id":      rid,
					"final":           "",
					"page":            map[string]any{"url": "", "title": ""},
					"screenshot":      nil,
					"error":           "cancelled_by_frontend",
					"attempt":         attemptIdx,
					"failure_summary": nil,
					"progress_state":  nil,
				}, nil
			}

			if strings.Contains(execErr.Error(), "task_timeout") {
				lastError = fmt.Sprintf("task_timeout: exceeded %ds", effectiveTimeout)
				if attemptIdx >= attempts {
					break
				}
				continue
			}

			lastError = execErr.Error()
			// 传输层错误时尝试重启后重试
			if IsRetryableTransportMessage(lastError) {
				if attemptIdx < attempts {
					// TODO: ⤵️ 9.38-49 回填 _restart 调用 ManagedBrowserDriver
					_ = s.restart(ctx)
					continue
				}
			}
			if attemptIdx >= attempts {
				break
			}
			continue
		}

		if parsed == nil {
			parsed = map[string]any{}
		}

		// 更新进度
		updateProgressFromWorkerResult(s, sid, rid, parsed)

		parsedOK := isTruthy(parsed["ok"])

		// 检查是否应视为已完成
		if !parsedOK && shouldTreatAsCompleted(parsed) {
			newParsed := make(map[string]any)
			for k, v := range parsed {
				newParsed[k] = v
			}
			newParsed["ok"] = true
			newParsed["error"] = nil
			parsed = newParsed
			parsedOK = true
		}

		shouldResumeMaxIter := !parsedOK &&
			isMaxIterationResult(parsed) &&
			s.Guardrails.ResumeOnMaxIterations &&
			!usedMaxIterationResume

		if !parsedOK {
			lastError = fmt.Sprintf("%v", parsed["error"])
			lastFailureFinal = fmt.Sprintf("%v", parsed["final"])
			if page, ok := parsed["page"].(map[string]any); ok {
				lastFailurePage = page
			} else {
				lastFailurePage = map[string]any{}
			}
			lastFailureScreenshot = parsed["screenshot"]
		}

		// 最大迭代次数续行
		if shouldResumeMaxIter {
			usedMaxIterationResume = true
			s.mu.RLock()
			progressCtx := buildProgressContext(s.progressBySession[sid])
			s.mu.RUnlock()
			nextTask = buildResumeTask(
				nextTask,
				fmt.Sprintf("%v", parsed["final"]),
				progressCtx,
			)
			lastError = fmt.Sprintf("%v", parsed["error"])
			if lastError == "" || lastError == "<nil>" {
				lastError = MaxIterationMessage
			}
			continue
		}

		// 可重试的运行时错误
		if !parsedOK && attemptIdx < attempts && IsRetryableRuntimeResult(parsed) {
			page := map[string]any{}
			if p, ok := parsed["page"].(map[string]any); ok {
				page = p
			}
			failureSummary := buildFailureSummary(
				baseTask,
				fmt.Sprintf("%v", parsed["error"]),
				fmt.Sprintf("%v", page["url"]),
				fmt.Sprintf("%v", page["title"]),
				fmt.Sprintf("%v", parsed["final"]),
				parsed["screenshot"],
				attemptIdx,
				s.getProgressState(sid),
			)
			shouldRestart := IsRetryableTransportMessage(failureSummary) ||
				shouldRestartAfterRuntimeResult(parsed)
			if shouldRestart {
				if restartErr := s.restart(ctx); restartErr != nil {
					lastError = fmt.Sprintf("restart_failed: %v", restartErr)
					break
				}
			}
			nextTask = buildTaskWithFailureContext(baseTask, failureSummary)
			continue
		}

		// 构建响应
		page := map[string]any{}
		if p, ok := parsed["page"].(map[string]any); ok {
			page = p
		}
		screenshot := NormalizeScreenshotValue(parsed["screenshot"])

		response := map[string]any{
			"ok":         parsedOK,
			"session_id": sid,
			"request_id": rid,
			"final":      fmt.Sprintf("%v", parsed["final"]),
			"page": map[string]any{
				"url":   fmt.Sprintf("%v", page["url"]),
				"title": fmt.Sprintf("%v", page["title"]),
			},
			"screenshot":      screenshot,
			"error":           parsed["error"],
			"attempt":         attemptIdx,
			"progress_state":  nil,
			"failure_summary": nil,
		}

		if parsedOK {
			s.mu.Lock()
			delete(s.failureContextBySession, sid)
			delete(s.progressBySession, sid)
			s.mu.Unlock()
			return response, nil
		}

		// 失败响应
		failureSummary := buildFailureSummary(
			baseTask,
			fmt.Sprintf("%v", parsed["error"]),
			fmt.Sprintf("%v", page["url"]),
			fmt.Sprintf("%v", page["title"]),
			fmt.Sprintf("%v", parsed["final"]),
			parsed["screenshot"],
			attemptIdx,
			s.getProgressState(sid),
		)
		s.mu.Lock()
		s.failureContextBySession[sid] = failureSummary
		s.mu.Unlock()
		response["failure_summary"] = failureSummary
		response["progress_state"] = s.ExportProgressState(sid)
		return response, nil
	}

	// 所有尝试用尽
	_ = s.ClearCancel(ctx, sid, rid)
	_ = s.ClearCancel(ctx, sid, "")

	pageURL := ""
	pageTitle := ""
	if lastFailurePage != nil {
		pageURL = fmt.Sprintf("%v", lastFailurePage["url"])
		pageTitle = fmt.Sprintf("%v", lastFailurePage["title"])
	}
	if lastError == "" {
		lastError = "unknown browser execution error"
	}

	failureSummary := buildFailureSummary(
		baseTask,
		lastError,
		pageURL,
		pageTitle,
		lastFailureFinal,
		lastFailureScreenshot,
		min(attemptIdx, maxAttempts),
		s.getProgressState(sid),
	)
	s.mu.Lock()
	s.failureContextBySession[sid] = failureSummary
	s.mu.Unlock()

	return map[string]any{
		"ok":              false,
		"session_id":      sid,
		"request_id":      rid,
		"final":           "",
		"page":            map[string]any{"url": "", "title": ""},
		"screenshot":      nil,
		"error":           lastError,
		"attempt":         min(attemptIdx, maxAttempts),
		"failure_summary": failureSummary,
		"progress_state":  s.ExportProgressState(sid),
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cancelKey 生成取消标记的键。
// 对齐 Python: BrowserService._cancel_key
func cancelKey(sessionID, requestID string) string {
	rid := strings.TrimSpace(requestID)
	if rid == "" {
		rid = "*"
	}
	return fmt.Sprintf("playwright_runtime:cancel:%s:%s", sessionID, rid)
}

// getLock 获取会话级别的互斥锁。
func (s *BrowserService) getLock(sid string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lock, ok := s.locksPerSession[sid]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	s.locksPerSession[sid] = lock
	return lock
}

// getProgressState 获取或创建指定会话的进度状态。
// 对齐 Python: BrowserService._get_progress_state
func (s *BrowserService) getProgressState(sid string) *BrowserTaskProgressState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.progressBySession[sid]
	if !ok {
		state = &BrowserTaskProgressState{
			Status:              "unknown",
			CompletedSteps:      []string{},
			RemainingSteps:      []string{},
			CompletionEvidence:  []string{},
			MissingRequirements: []string{},
			RecentToolSteps:     []string{},
		}
		s.progressBySession[sid] = state
	}
	return state
}

// runTaskOnceWithTimeout 带超时执行单次任务。
// 对齐 Python: BrowserService._run_task_once（占位，待 Worker Agent 回填）
func (s *BrowserService) runTaskOnceWithTimeout(
	ctx context.Context,
	task, sessionID, requestID string,
	timeoutS int,
) (map[string]any, error) {
	// TODO: ⤵️ 9.38-49 回填 Worker Agent 执行逻辑
	// 对齐 Python: _run_task_once → Runner.run_agent
	if s.browserAgent == nil {
		return nil, fmt.Errorf("BrowserService is not started")
	}

	// 占位：使用 context 超时模拟
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutS)*time.Second)
	defer cancel()

	// TODO: ⤵️ 9.38-49 回填实际 Agent 调用
	_ = timeoutCtx
	return map[string]any{
		"ok":    false,
		"final": "",
		"page":  map[string]any{"url": "", "title": ""},
		"error": "BrowserService.runTaskOnce not implemented yet",
	}, nil
}

// restart 重启浏览器服务。
// 对齐 Python: BrowserService._restart
// TODO: ⤵️ 9.38-49 回填 _restartBrowserRuntime 调用 ManagedBrowserDriver
func (s *BrowserService) restart(_ context.Context) error {
	// TODO: ⤵️ 9.38-49 回填 _restartBrowserRuntime 调用 ManagedBrowserDriver
	return nil
}

// updateProgressFromToolObservation 从工具执行结果更新进度状态。
// 对齐 Python: BrowserService._update_progress_from_tool_observation
func updateProgressFromToolObservation(
	s *BrowserService,
	sessionID, requestID, toolName string,
	toolResult any,
) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	progressState := s.getProgressState(sid)
	if requestID != "" {
		progressState.RequestID = requestID
	}
	progressState.RecentToolSteps = pushRecentToolStep(
		progressState.RecentToolSteps,
		summarizeToolResult(toolName, toolResult),
		8,
	)
	url, title := extractPageSnapshot(toolResult)
	if url != "" {
		progressState.LastPageURL = url
	}
	if title != "" {
		progressState.LastPageTitle = title
	}
	screenshot := extractScreenshotSnapshot(toolResult)
	if screenshot != nil {
		progressState.LastScreenshot = screenshot
	}
	if progressState.Status == "unknown" {
		progressState.Status = "partial"
	}
}

// updateProgressFromWorkerResult 从 Worker 执行结果更新进度状态。
// 对齐 Python: BrowserService._update_progress_from_worker_result
func updateProgressFromWorkerResult(
	s *BrowserService,
	sessionID, requestID string,
	parsed map[string]any,
) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || parsed == nil {
		return
	}
	progressState := s.getProgressState(sid)
	if requestID != "" {
		progressState.RequestID = requestID
	}

	progressPayload := map[string]any{}
	if pp, ok := parsed["progress"].(map[string]any); ok {
		progressPayload = pp
	}

	status := NormalizeProgressStatus(
		firstNonEmpty(parsed["status"], parsed["task_status"]),
	)
	if status == "" {
		if isTruthy(parsed["ok"]) {
			status = "completed"
		} else if isMaxIterationResult(parsed) {
			status = "partial"
		} else if strings.TrimSpace(fmt.Sprintf("%v", parsed["error"])) != "" {
			status = "failed"
		} else {
			status = "partial"
		}
	}
	progressState.Status = status

	completedSteps := cleanProgressItems(
		firstNonEmpty(progressPayload["completed_steps"], parsed["completed_steps"]),
		8,
	)
	if len(completedSteps) > 0 {
		progressState.CompletedSteps = completedSteps
	}

	remainingSteps := cleanProgressItems(
		firstNonEmpty(progressPayload["remaining_steps"], parsed["remaining_steps"]),
		8,
	)
	if len(remainingSteps) > 0 {
		progressState.RemainingSteps = remainingSteps
	}

	nextStep := strings.TrimSpace(
		fmt.Sprintf("%v", firstNonEmpty(progressPayload["next_step"], parsed["next_step"])),
	)
	if nextStep != "" {
		progressState.NextStep = trimText(nextStep, 220)
	}

	completionEvidence := cleanProgressItems(
		firstNonEmpty(progressPayload["completion_evidence"], parsed["completion_evidence"]),
		6,
	)
	if len(completionEvidence) > 0 {
		progressState.CompletionEvidence = completionEvidence
	}

	missingRequirements := cleanProgressItems(
		firstNonEmpty(progressPayload["missing_requirements"], parsed["missing_requirements"]),
		6,
	)
	if len(missingRequirements) > 0 {
		progressState.MissingRequirements = missingRequirements
	}

	url, title := extractPageSnapshot(parsed)
	if url != "" {
		progressState.LastPageURL = url
	}
	if title != "" {
		progressState.LastPageTitle = title
	}
	screenshot := extractScreenshotSnapshot(parsed)
	if screenshot != nil {
		progressState.LastScreenshot = screenshot
	}

	finalText := strings.TrimSpace(fmt.Sprintf("%v", parsed["final"]))
	if finalText != "" {
		progressState.LastWorkerFinal = trimText(finalText, 1200)
		if isTruthy(parsed["ok"]) && len(progressState.CompletionEvidence) == 0 {
			progressState.CompletionEvidence = []string{trimText(finalText, 220)}
		}
	}
}

// buildProgressContext 根据进度状态构建上下文文本。
// 对齐 Python: BrowserService._build_progress_context
func buildProgressContext(state *BrowserTaskProgressState) string {
	if state == nil {
		return ""
	}
	var lines []string
	if state.Status != "" && state.Status != "unknown" {
		lines = append(lines, fmt.Sprintf("- Known progress status: %s", state.Status))
	}
	if len(state.CompletedSteps) > 0 {
		lines = append(lines, fmt.Sprintf("- Completed steps: %s", strings.Join(state.CompletedSteps, " | ")))
	}
	if len(state.RemainingSteps) > 0 {
		lines = append(lines, fmt.Sprintf("- Remaining steps: %s", strings.Join(state.RemainingSteps, " | ")))
	}
	if state.NextStep != "" {
		lines = append(lines, fmt.Sprintf("- Next step to try: %s", state.NextStep))
	}
	if len(state.CompletionEvidence) > 0 {
		lines = append(lines, fmt.Sprintf("- Completion evidence observed: %s", strings.Join(state.CompletionEvidence, " | ")))
	}
	if len(state.MissingRequirements) > 0 {
		lines = append(lines, fmt.Sprintf("- Missing requirements / blockers: %s", strings.Join(state.MissingRequirements, " | ")))
	}
	if len(state.RecentToolSteps) > 0 {
		lines = append(lines, "- Recent browser tool activity:")
		start := len(state.RecentToolSteps) - 6
		if start < 0 {
			start = 0
		}
		for _, step := range state.RecentToolSteps[start:] {
			lines = append(lines, fmt.Sprintf("  - %s", step))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Known progress for continuation:\n" + strings.Join(lines, "\n")
}

// buildFailureSummary 构建失败摘要。
// 对齐 Python: BrowserService._build_failure_summary
func buildFailureSummary(
	task, errStr, pageURL, pageTitle, final string,
	screenshot any,
	attempt int,
	progressState *BrowserTaskProgressState,
) string {
	lines := []string{
		"Failure summary for continuation:",
		fmt.Sprintf("- Original task: %s", orEmpty(trimText(task, 400))),
		fmt.Sprintf("- Failed attempt: %d", attempt),
		fmt.Sprintf("- Error: %s", orUnknown(trimText(errStr, 300))),
	}
	if pageURL != "" || pageTitle != "" {
		lines = append(lines, fmt.Sprintf(
			"- Last page: url=%s, title=%s",
			orUnknown(trimText(pageURL, 240)),
			orUnknown(trimText(pageTitle, 120)),
		))
	}
	screenshotText := trimText(screenshot, 200)
	if screenshotText != "" {
		lines = append(lines, fmt.Sprintf("- Last screenshot: %s", screenshotText))
	}
	progressContext := buildProgressContext(progressState)
	if progressContext != "" {
		lines = append(lines, progressContext)
	}
	finalExcerpt := trimText(final, 1200)
	if finalExcerpt != "" {
		lines = append(lines, "- Partial output excerpt:")
		lines = append(lines, finalExcerpt)
	}
	return strings.Join(lines, "\n")
}

// shouldTreatAsCompleted 判断是否应将结果视为已完成。
// 对齐 Python: BrowserService._should_treat_as_completed
func shouldTreatAsCompleted(parsed map[string]any) bool {
	status := NormalizeProgressStatus(
		firstNonEmpty(parsed["status"], parsed["task_status"]),
	)
	if status != "completed" {
		return false
	}
	progress := map[string]any{}
	if p, ok := parsed["progress"].(map[string]any); ok {
		progress = p
	}
	missing := cleanProgressItems(
		firstNonEmpty(progress["missing_requirements"], parsed["missing_requirements"]),
		4,
	)
	evidence := cleanProgressItems(
		firstNonEmpty(progress["completion_evidence"], parsed["completion_evidence"]),
		4,
	)
	finalText := strings.TrimSpace(fmt.Sprintf("%v", parsed["final"]))
	if finalText == "<nil>" {
		finalText = ""
	}
	return len(missing) == 0 && (len(evidence) > 0 || finalText != "")
}

// isEmptyProgressState 判断进度状态是否为空。
// 对齐 Python: BrowserService._is_empty_progress_state
func isEmptyProgressState(state *BrowserTaskProgressState) bool {
	return state.Status == "unknown" &&
		len(state.CompletedSteps) == 0 &&
		len(state.RemainingSteps) == 0 &&
		state.NextStep == "" &&
		len(state.CompletionEvidence) == 0 &&
		len(state.MissingRequirements) == 0 &&
		len(state.RecentToolSteps) == 0 &&
		state.LastPageURL == "" &&
		state.LastPageTitle == "" &&
		state.LastWorkerFinal == "" &&
		(state.LastScreenshot == nil || state.LastScreenshot == "")
}

// isMaxIterationResult 判断是否为最大迭代次数结果。
// 对齐 Python: BrowserService._is_max_iteration_result
func isMaxIterationResult(parsed map[string]any) bool {
	if parsed == nil {
		return false
	}
	errStr := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", parsed["error"])))
	if errStr == "max_iterations_reached" {
		return true
	}
	marker := strings.ToLower(MaxIterationMessage)
	for _, key := range []string{"final", "error"} {
		value, ok := parsed[key]
		if !ok || value == nil {
			continue
		}
		if strings.Contains(strings.ToLower(fmt.Sprintf("%v", value)), marker) {
			return true
		}
	}
	return false
}

// buildResumeTask 构建续行任务提示词。
// 对齐 Python: BrowserService._build_resume_task
func buildResumeTask(task, previousFinal, progressContext string) string {
	base := strings.TrimSpace(task)
	previous := strings.TrimSpace(previousFinal)
	if len(previous) > 1200 {
		previous = previous[:1200] + "...[truncated]"
	}
	contextParts := []string{
		"Continuation context:",
		"- The previous run reached max iterations before completion.",
		"- Continue from the current browser state in this same session.",
		"- Avoid repeating already completed steps unless needed for recovery.",
	}
	if progressContext != "" {
		contextParts = append(contextParts, progressContext)
	}
	if previous != "" {
		contextParts = append(contextParts,
			"- Previous partial status (may be incomplete):",
			previous,
		)
	}
	return base + "\n\n" + strings.Join(contextParts, "\n")
}

// buildTaskWithFailureContext 构建带失败上下文的任务提示词。
// 对齐 Python: BrowserService._build_task_with_failure_context
func buildTaskWithFailureContext(task, failureSummary string) string {
	base := strings.TrimSpace(task)
	summary := strings.TrimSpace(failureSummary)
	if summary == "" {
		return base
	}
	return base + "\n\n" +
		"Previous failed attempt context:\n" +
		summary + "\n\n" +
		"Continuation instructions:\n" +
		"- Continue from the current browser state in this same session.\n" +
		"- Do not repeat completed steps unless required for recovery.\n" +
		"- Prioritize resolving the listed failure."
}

// cleanProgressItems 清理和去重进度项列表。
// 对齐 Python: BrowserService._clean_progress_items
func cleanProgressItems(value any, limit int) []string {
	if value == nil {
		return []string{}
	}
	var candidates []any
	switch v := value.(type) {
	case []any:
		candidates = v
	case []string:
		candidates = make([]any, len(v))
		for i, s := range v {
			candidates[i] = s
		}
	default:
		candidates = []any{value}
	}
	var cleaned []string
	seen := make(map[string]bool)
	for _, item := range candidates {
		textValue := strings.TrimSpace(strings.Join(strings.Fields(fmt.Sprintf("%v", item)), " "))
		if textValue == "" {
			continue
		}
		lowered := strings.ToLower(textValue)
		if seen[lowered] {
			continue
		}
		seen[lowered] = true
		cleaned = append(cleaned, trimText(textValue, 220))
		if len(cleaned) >= limit {
			break
		}
	}
	return cleaned
}

// pushRecentToolStep 追加最近工具步骤，去重并限制数量，返回新切片。
// 对齐 Python: BrowserService._push_recent_tool_step
func pushRecentToolStep(existing []string, step string, limit ...int) []string {
	maxLen := 8
	if len(limit) > 0 {
		maxLen = limit[0]
	}
	normalized := strings.TrimSpace(strings.Join(strings.Fields(step), " "))
	if normalized == "" {
		return existing
	}
	updated := make([]string, 0, len(existing))
	for _, item := range existing {
		if item != normalized {
			updated = append(updated, item)
		}
	}
	updated = append(updated, normalized)
	if len(updated) > maxLen {
		updated = updated[len(updated)-maxLen:]
	}
	return updated
}

// extractPageSnapshot 从值中提取页面快照（URL 和标题）。
// 对齐 Python: BrowserService._extract_page_snapshot
func extractPageSnapshot(value any) (string, string) {
	m, ok := value.(map[string]any)
	if !ok {
		return "", ""
	}
	page := map[string]any{}
	if p, ok := m["page"].(map[string]any); ok {
		page = p
	}
	url := strings.TrimSpace(fmt.Sprintf("%v", firstNonEmpty(m["url"], page["url"])))
	title := strings.TrimSpace(fmt.Sprintf("%v", firstNonEmpty(m["title"], page["title"])))
	return trimText(url, 240), trimText(title, 120)
}

// extractScreenshotSnapshot 从值中提取截图。
// 对齐 Python: BrowserService._extract_screenshot_snapshot
func extractScreenshotSnapshot(value any) any {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	screenshot := m["screenshot"]
	if screenshot == nil || screenshot == "" {
		return nil
	}
	return screenshot
}

// summarizeObservationPayload 汇总观测负载信息。
// 对齐 Python: BrowserService._summarize_observation_payload
func summarizeObservationPayload(value any) string {
	switch v := value.(type) {
	case map[string]any:
		var parts []string
		errorText := strings.TrimSpace(fmt.Sprintf("%v", v["error"]))
		if errorText != "" {
			parts = append(parts, fmt.Sprintf("error=%s", trimText(errorText, 140)))
		}
		for _, key := range []string{"message", "text", "result", "output", "value", "selector"} {
			candidate := v[key]
			if candidate == nil || candidate == "" {
				continue
			}
			switch candidate.(type) {
			case map[string]any, []any:
				continue
			}
			parts = append(parts, trimText(fmt.Sprintf("%v", candidate), 160))
			break
		}
		url, title := extractPageSnapshot(v)
		if url != "" {
			parts = append(parts, fmt.Sprintf("url=%s", url))
		}
		if title != "" {
			parts = append(parts, fmt.Sprintf("title=%s", title))
		}
		if isTruthy(v["ok"]) && len(parts) == 0 {
			parts = append(parts, "ok")
		}
		if len(parts) == 0 && len(v) > 0 {
			keys := make([]string, 0, 4)
			count := 0
			for k := range v {
				keys = append(keys, k)
				count++
				if count >= 4 {
					break
				}
			}
			parts = append(parts, strings.Join(keys, ", "))
		}
		return strings.Join(parts, "; ")
	case []any:
		var nested []string
		for i, item := range v {
			if i >= 2 {
				break
			}
			if s := summarizeObservationPayload(item); s != "" {
				nested = append(nested, s)
			}
		}
		return strings.Join(nested, " | ")
	default:
		textValue := strings.TrimSpace(strings.Join(strings.Fields(fmt.Sprintf("%v", value)), " "))
		return trimText(textValue, 160)
	}
}

// summarizeToolResult 汇总工具执行结果。
// 对齐 Python: BrowserService._summarize_tool_result
func summarizeToolResult(toolName string, toolResult any) string {
	payloadSummary := summarizeObservationPayload(toolResult)
	if payloadSummary == "" {
		payloadSummary = "completed"
	}
	if toolName != "" {
		return fmt.Sprintf("%s: %s", toolName, payloadSummary)
	}
	return payloadSummary
}

// shouldRestartAfterRuntimeResult 判断是否应在运行时结果后重启。
// 对齐 Python: BrowserService._should_restart_after_runtime_result
func shouldRestartAfterRuntimeResult(parsed map[string]any) bool {
	if parsed == nil {
		return false
	}
	text := strings.ToLower(
		fmt.Sprintf("%v\n%v", parsed["error"], parsed["final"]),
	)
	restartMarkers := []string{
		"frame has been detached",
		"target page, context or browser has been closed",
		"target closed",
		"context closed",
		"page crashed",
	}
	for _, marker := range restartMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// trimText 截断文本到指定长度。
// 对齐 Python: BrowserService._trim_text
func trimText(value any, limit int) string {
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if len(text) > limit {
		return text[:limit] + "...[truncated]"
	}
	return text
}

// firstNonEmpty 返回第一个非零值。
func firstNonEmpty(values ...any) any {
	for _, v := range values {
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s != "" && s != "<nil>" {
			return v
		}
	}
	return nil
}

// isTruthy 判断值是否为真。
func isTruthy(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		return val != "" && val != "false" && val != "0"
	case nil:
		return false
	default:
		return fmt.Sprintf("%v", v) != "" && fmt.Sprintf("%v", v) != "false"
	}
}

// orEmpty 空值返回 "(empty)"，否则返回原值。
func orEmpty(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}

// orUnknown 空值返回 "(unknown)"，否则返回原值。
func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

// buildWorkerConversationID 构建Worker会话标识。
// 对齐 Python: BrowserService._build_worker_conversation_id
func buildWorkerConversationID(sessionID, requestID string) string {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = "browser-session"
	}
	rid := strings.TrimSpace(requestID)
	if rid == "" {
		rid = "request"
	}
	return fmt.Sprintf("%s:worker:%s:%s", sid, rid, uuid.New().String()[:8])
}
