// multi_session_toolkits.go 提供 agent.fast 模式多会话并行子代理工具集：
// session_new 为每个任务描述 spawn 子 ReActAgent 协程并发执行，
// chat.session_result 推送实时进度，全部完成后汇总并 chat.final 收尾。
// Python: jiuwenswarm/agents/harness/common/tools/multi_session_toolkits.py（10.6.24c）
package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionStatus 子会话状态。
// Python: Status(str, Enum)
type SessionStatus string

// SessionTask 子会话任务记录。
// Python: SessionTask(BaseModel)
type SessionTask struct {
	// SessionID 子会话标识
	SessionID string
	// Description 任务描述
	Description string
	// Status 状态
	Status SessionStatus
	// Result 结果文本
	Result string
}

// sessionTaskHandle 运行中任务句柄（偏差#9：per-task done channel）。
type sessionTaskHandle struct {
	// cancel 取消函数
	cancel context.CancelFunc
	// done 任务结束信号（goroutine defer close）
	done chan struct{}
}

// MultiSessionToolkit 多会话并行子代理工具集。
// Python: MultiSessionToolkit——生命周期从创建新 session 到所有协程结束。
type MultiSessionToolkit struct {
	// sessionID 父会话标识
	sessionID string
	// channelID 推送回父会话的渠道
	channelID string
	// requestID 请求标识
	requestID string
	// sessions 任务记录列表（Python self.sessions）
	sessions []SessionTask
	// tasks 运行中任务句柄（Python _tasks: Dict[str, asyncio.Task]）
	tasks map[string]*sessionTaskHandle
	// subAgentConfig 子代理配置（Python _sub_agent_config）
	subAgentConfig *saconfig.ReActAgentConfig
	// mu 保护 sessions/tasks 并发访问（Python 单线程 GIL 免锁）
	mu sync.Mutex
	// sumOnce 汇总链单次触发守卫（Python GIL+协程原子性 → sync.Once）
	sumOnce sync.Once
}

// ──────────────────────────── 枚举 ────────────────────────────

const (
	// SessionStatusRunning 运行中
	SessionStatusRunning SessionStatus = "running"
	// SessionStatusCompleted 已完成
	SessionStatusCompleted SessionStatus = "completed"
	// SessionStatusCancelled 已取消
	SessionStatusCancelled SessionStatus = "cancelled"
	// SessionStatusError 已失败
	SessionStatusError SessionStatus = "error"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sessionPushFunc E2A 推送函数（agent_server 启动注入，24b 先例）。
	sessionPushFunc func(ctx context.Context, msg map[string]any) error
	// sessionAgentProvider default agent 提供者（Python AgentWebSocketServer.get_agent()）。
	sessionAgentProvider func() agentinterfaces.BaseAgent
	// sessionInjectMu 保护注入函数并发设置。
	sessionInjectMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetSessionPushFunc 注入推送函数（agent_server 启动调用）。
func SetSessionPushFunc(fn func(ctx context.Context, msg map[string]any) error) {
	sessionInjectMu.Lock()
	defer sessionInjectMu.Unlock()
	sessionPushFunc = fn
}

// SetSessionAgentProvider 注入 default agent 提供者（agent_server 启动调用）。
func SetSessionAgentProvider(fn func() agentinterfaces.BaseAgent) {
	sessionInjectMu.Lock()
	defer sessionInjectMu.Unlock()
	sessionAgentProvider = fn
}

// NewMultiSessionToolkit 创建多会话工具集。
// Python: MultiSessionToolkit.__init__
func NewMultiSessionToolkit(sessionID, channelID, requestID string, subAgentConfig *saconfig.ReActAgentConfig) *MultiSessionToolkit {
	logger.Info(logComponent).
		Str("parent_session_id", sessionID).
		Str("channel_id", channelID).
		Str("request_id", requestID).
		Msg("[MultiSessionToolkit] 初始化")
	return &MultiSessionToolkit{
		sessionID:      sessionID,
		channelID:      channelID,
		requestID:      requestID,
		tasks:          make(map[string]*sessionTaskHandle),
		subAgentConfig: subAgentConfig,
	}
}

// GetTools 返回三工具（session_new/session_cancel/session_list）。
// Python: get_tools()
func (t *MultiSessionToolkit) GetTools() []tool.Tool {
	return []tool.Tool{
		t.newSessionNewTool(),
		t.newSessionCancelTool(),
		t.newSessionListTool(),
	}
}

// CreateNewSessions 为每个任务描述创建子代理会话。
// Python: create_new_sessions()
func (t *MultiSessionToolkit) CreateNewSessions(ctx context.Context, inputs map[string]any) (map[string]any, error) {
	descs := toStringSlice(inputs["task_descriptions"])
	logger.Info(logComponent).
		Str("parent_session_id", t.sessionID).
		Int("task_count", len(descs)).
		Msg("[MultiSessionToolkit] create_new_sessions 开始")
	var created []string
	for i, desc := range descs {
		sessionID := fmt.Sprintf("spawn_%d_%s", time.Now().UnixNano(), randHex4())
		logger.Debug(logComponent).
			Int("index", i+1).Int("total", len(descs)).
			Str("session_id", sessionID).Str("description", truncate(desc, 60)).
			Msg("[MultiSessionToolkit] 创建协程")
		agent := t.getSubAgent()
		inputsForTask := map[string]any{
			"conversation_id": sessionID,
			"query":           desc,
		}
		// 偏差#7：WithoutCancel 保后台生命周期，WithCancel 供 cancel_session
		taskCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		// 偏差#9：per-task done channel——cancel_session 只等被取消的单个任务
		handle := &sessionTaskHandle{cancel: cancel, done: make(chan struct{})}
		t.mu.Lock()
		t.tasks[sessionID] = handle
		t.mu.Unlock()
		go func() {
			defer close(handle.done)
			t.runAndNotify(taskCtx, sessionID, desc, agent, inputsForTask)
		}()
		created = append(created, sessionID)
	}
	logger.Info(logComponent).
		Int("count", len(created)).
		Str("ids", strings.Join(created, ", ")).
		Msg("[MultiSessionToolkit] create_new_sessions 完成")
	return map[string]any{"content": fmt.Sprintf("已创建 %d 个协程: %s", len(created), strings.Join(created, ", "))}, nil
}

// CancelSession 按 sessionID 取消运行中会话。
// Python: cancel_session()
func (t *MultiSessionToolkit) CancelSession(ctx context.Context, inputs map[string]any) (map[string]any, error) {
	sessionID := todoStr(inputs, "session_id")
	logger.Info(logComponent).
		Str("parent_session_id", t.sessionID).
		Str("target_session_id", sessionID).
		Msg("[MultiSessionToolkit] cancel_session 请求")
	t.mu.Lock()
	handle, ok := t.tasks[sessionID]
	t.mu.Unlock()
	if !ok {
		// 未在 tasks 中：从未创建或已结束（Python task is None / task.done() 双分支）
		t.mu.Lock()
		known := false
		for _, s := range t.sessions {
			if s.SessionID == sessionID {
				known = true
				break
			}
		}
		t.mu.Unlock()
		if known {
			// Python: task.done() → 已结束，无需取消
			logger.Info(logComponent).Str("session_id", sessionID).Msg("[MultiSessionToolkit] cancel_session 已结束，无需取消")
			return map[string]any{"content": fmt.Sprintf("session_id=%s 已结束", sessionID)}, nil
		}
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Msg("[MultiSessionToolkit] cancel_session 未找到")
		return map[string]any{"content": fmt.Sprintf("未找到 session_id=%s", sessionID)}, nil
	}
	// 偏差#9：只等被取消的单个任务（对齐 await asyncio.gather(task, return_exceptions=True)）——
	// 此前实现为 t.wg.Wait() 等 toolkit 全部任务，并发场景 session_cancel 会被无关任务卡住
	handle.cancel()
	<-handle.done
	logger.Info(logComponent).Str("session_id", sessionID).Msg("[MultiSessionToolkit] cancel_session 已取消")
	return map[string]any{"content": fmt.Sprintf("已取消 session_id=%s", sessionID)}, nil
}

// ListAllSessions 列出全部会话任务。
// Python: list_all_sessions()
func (t *MultiSessionToolkit) ListAllSessions(_ context.Context, _ map[string]any) (map[string]any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	logger.Debug(logComponent).
		Str("parent_session_id", t.sessionID).
		Int("session_count", len(t.sessions)).
		Msg("[MultiSessionToolkit] list_all_sessions")
	if len(t.sessions) == 0 {
		return map[string]any{"content": "暂无协程"}, nil
	}
	var lines []string
	for _, st := range t.sessions {
		lines = append(lines, fmt.Sprintf("%s | %s | %s | %s", st.SessionID, st.Description, st.Status, st.Result))
	}
	return map[string]any{"content": strings.Join(lines, "\n")}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSessionPushFunc 读注入的推送函数。
func getSessionPushFunc() func(ctx context.Context, msg map[string]any) error {
	sessionInjectMu.RLock()
	defer sessionInjectMu.RUnlock()
	return sessionPushFunc
}

// getSessionAgentProvider 读注入的 agent 提供者。
func getSessionAgentProvider() func() agentinterfaces.BaseAgent {
	sessionInjectMu.RLock()
	defer sessionInjectMu.RUnlock()
	return sessionAgentProvider
}

// newSessionNewTool 构造 session_new 工具。
// Python: get_tools() 第一个 make_tool（文案逐字）
func (t *MultiSessionToolkit) newSessionNewTool() tool.Tool {
	card := tool.NewToolCard(
		"session_new",
		"创建多个协程任务。接收任务描述列表，每个任务创建一个子 agent 并异步运行。协程完成后会通过 notify 发送结果。",
		[]*schema.Param{
			{Name: "task_descriptions", Description: "任务描述列表", Type: schema.ParamTypeArray, Required: true, Items: &schema.Param{Type: schema.ParamTypeString}},
		},
		nil,
	)
	mf, _ := tool.NewMapFunction(card, t.CreateNewSessions, nil)
	return mf
}

// newSessionCancelTool 构造 session_cancel 工具。
func (t *MultiSessionToolkit) newSessionCancelTool() tool.Tool {
	card := tool.NewToolCard(
		"session_cancel",
		"根据 session_id 取消正在运行的协程。",
		[]*schema.Param{
			{Name: "session_id", Description: "要取消的协程 session_id", Type: schema.ParamTypeString, Required: true},
		},
		nil,
	)
	mf, _ := tool.NewMapFunction(card, t.CancelSession, nil)
	return mf
}

// newSessionListTool 构造 session_list 工具。
func (t *MultiSessionToolkit) newSessionListTool() tool.Tool {
	card := tool.NewToolCard(
		"session_list",
		"查看所有协程列表及其状态（session_id | description | status | result）。",
		[]*schema.Param{},
		nil,
	)
	mf, _ := tool.NewMapFunction(card, t.ListAllSessions, nil)
	return mf
}

// getSubAgent 创建子代理实例。
// Python: get_sub_agent()
func (t *MultiSessionToolkit) getSubAgent() agentinterfaces.BaseAgent {
	logger.Debug(logComponent).Msg("[MultiSessionToolkit] get_sub_agent 创建子 agent")
	card := agentschema.NewAgentCard(agentschema.WithAgentName("spawn_sub_agent"))
	agent := agents.NewReActAgent(card, t.subAgentConfig)
	// Python: agent.configure(self._sub_agent_config)
	if t.subAgentConfig != nil {
		if cerr := agent.Configure(context.Background(), t.subAgentConfig); cerr != nil {
			logger.Error(logComponent).Err(cerr).Msg("[MultiSessionToolkit] sub agent Configure failed")
		}
	}
	mcpTools := GetMcpTools()
	for _, mcpTool := range mcpTools {
		_ = runner.GetResourceMgr().AddTool(mcpTool) // Python 裸调 add_tool（重复静默失败）
		agent.AbilityManager().Add(mcpTool.Card())
	}
	logger.Debug(logComponent).Int("mcp_tools_count", len(mcpTools)).Msg("[MultiSessionToolkit] get_sub_agent 完成")
	return agent
}

// updateSession 更新会话任务状态。
// Python: _update_session()
func (t *MultiSessionToolkit) updateSession(sessionID string, status SessionStatus, result string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.sessions {
		if t.sessions[i].SessionID == sessionID {
			t.sessions[i].Status = status
			t.sessions[i].Result = result
			logger.Debug(logComponent).
				Str("session_id", sessionID).Str("status", string(status)).
				Msg("[MultiSessionToolkit] update_session")
			break
		}
	}
}

// allTasksDone 全部任务结束（不含 CANCELLED——Python 逐字复刻）。
// Python: all_tasks_done()
func (t *MultiSessionToolkit) allTasksDone() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range t.sessions {
		if s.Status != SessionStatusCompleted && s.Status != SessionStatusError {
			return false
		}
	}
	return true
}

// toStringSlice 宽容转字符串切片。
func toStringSlice(v any) []string {
	switch arr := v.(type) {
	case []string:
		return arr
	case []any:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{arr}
	}
	return nil
}

// randHex4 生成 4 字节 hex（对齐 secrets.token_hex(4)）。
func randHex4() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// truncate 超长截断加省略号（日志描述截断用，对齐 Python desc[:60]+"..."）。
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// runAndNotify 运行子代理并在结束（成功/取消/异常）时通知。
// Python: _run_and_notify()
func (t *MultiSessionToolkit) runAndNotify(ctx context.Context, sessionID, description string, agent agentinterfaces.BaseAgent, inputs map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			// Python except Exception → ERROR + notify + re-raise（Task 记录异常）
			errStr := fmt.Sprintf("%v", r)
			logger.Error(logComponent).
				Str("session_id", sessionID).Str("error", errStr).
				Msg("[MultiSessionToolkit] 协程异常")
			t.updateSession(sessionID, SessionStatusError, errStr)
			t.notify(ctx, sessionID, SessionStatusError, "", errStr)
		}
		// Python finally: self._tasks.pop(session_id, None)
		t.mu.Lock()
		delete(t.tasks, sessionID)
		remaining := len(t.tasks)
		t.mu.Unlock()
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Int("remaining", remaining).
			Msg("[MultiSessionToolkit] run_and_notify 结束")
	}()

	logger.Debug(logComponent).
		Str("session_id", sessionID).Str("description", truncate(description, 80)).
		Msg("[MultiSessionToolkit] run_and_notify 开始")
	t.mu.Lock()
	t.sessions = append(t.sessions, SessionTask{
		SessionID: sessionID, Description: description, Status: SessionStatusRunning, Result: "",
	})
	t.mu.Unlock()

	result, err := runner.RunAgent(ctx, runner.ByAgent(agent), inputs, runner.SessionRef{}, nil, nil)
	if err == nil {
		resultStr := ""
		if output, ok := result["output"].(string); ok {
			resultStr = output
		} else if result != nil {
			resultStr = fmt.Sprintf("%v", result)
		}
		logger.Info(logComponent).
			Str("session_id", sessionID).
			Int("result_len", len(resultStr)).
			Msg("[MultiSessionToolkit] 协程完成 status=completed")
		t.updateSession(sessionID, SessionStatusCompleted, resultStr)
		t.notify(ctx, sessionID, SessionStatusCompleted, resultStr, "")
		return
	}
	if errors.Is(err, context.Canceled) {
		logger.Info(logComponent).Str("session_id", sessionID).Msg("[MultiSessionToolkit] 协程已取消")
		t.updateSession(sessionID, SessionStatusCancelled, "任务已取消")
		t.notify(ctx, sessionID, SessionStatusCancelled, "", "")
		return
	}
	errStr := err.Error()
	logger.Error(logComponent).
		Str("session_id", sessionID).Str("error", errStr).
		Msg("[MultiSessionToolkit] 协程异常")
	t.updateSession(sessionID, SessionStatusError, errStr)
	t.notify(ctx, sessionID, SessionStatusError, "", errStr)
}

// notify 推送子任务更新；全部完成时触发汇总链。
// Python: notify()——send_push(chat.session_result) + all_tasks_done → 汇总 → chat.final
func (t *MultiSessionToolkit) notify(ctx context.Context, sessionID string, status SessionStatus, result, errStr string) {
	pushFunc := getSessionPushFunc()
	if pushFunc == nil {
		// Python: MessageHandler 未初始化 → warn 跳过
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Msg("[MultiSessionToolkit] MessageHandler 未初始化，跳过 notify")
		return
	}

	t.mu.Lock()
	description := ""
	index := 0
	for i, s := range t.sessions {
		if s.SessionID == sessionID {
			description = s.Description
			index = i + 1
			break
		}
	}
	total := len(t.sessions)
	// Python: allDone = all(s.status in [COMPLETED, ERROR])（all_tasks_done 内联等价）
	allDone := true
	for _, s := range t.sessions {
		if s.Status != SessionStatusCompleted && s.Status != SessionStatusError {
			allDone = false
			break
		}
	}
	t.mu.Unlock()

	// Python: 前端 SubtaskStatus: 'completed' | 'error'，cancelled 映射为 error
	payloadStatus := "error"
	message := errStr
	switch status {
	case SessionStatusCompleted:
		payloadStatus = "completed"
		message = result
	case SessionStatusCancelled:
		message = strOr(errStr, "任务已取消")
	}
	msg := map[string]any{
		"request_id": t.requestID,
		"channel_id": t.channelID,
		"session_id": t.sessionID,
		"payload": map[string]any{
			"event_type":  "chat.session_result",
			"session_id":  sessionID,
			"description": description,
			"status":      payloadStatus,
			"index":       index,
			"total":       total,
			"result":      message,
			"is_parallel": true,
		},
		"is_complete": false,
	}
	logger.Debug(logComponent).
		Str("session_id", sessionID).Str("status", payloadStatus).
		Int("index", index).Int("total", total).
		Msg("[MultiSessionToolkit] notify 发送 subtask_update")
	if err := pushFunc(ctx, msg); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[MultiSessionToolkit] push session_result failed")
	}

	if !allDone {
		return
	}
	// 汇总链：仅一个完成者进入（sync.Once，Python 单线程事件循环原子性等价）
	t.sumOnce.Do(func() {
		t.runSummary(ctx, pushFunc)
	})
}

// runSummary 全部完成后的汇总链。
// Python: notify() 尾部——汇总文本 + run_agent_streaming + chat.final
func (t *MultiSessionToolkit) runSummary(ctx context.Context, pushFunc func(context.Context, map[string]any) error) {
	// 偏差#4：汇总独立于父请求取消（Python 协程天然独立）
	summaryCtx := context.WithoutCancel(ctx)

	provider := getSessionAgentProvider()
	if provider == nil {
		logger.Error(logComponent).Msg("[MultiSessionToolkit] agentProvider not injected, skip summary")
		return
	}
	agent := provider()
	if agent == nil {
		logger.Error(logComponent).Msg("[MultiSessionToolkit] get_agent returned nil, skip summary")
		return
	}

	var sb strings.Builder
	sb.WriteString("后台会话任务均已完成：\n")
	t.mu.Lock()
	for _, st := range t.sessions {
		fmt.Fprintf(&sb, "\nsession_id: %s\ndescription: %s\nresult: %s\n", st.SessionID, st.Description, st.Result)
	}
	t.mu.Unlock()

	queryBytes, _ := json.Marshal(map[string]any{
		"source":  "system",
		"content": sb.String(),
		"type":    "notify",
	})
	inputs := map[string]any{
		"conversation_id": t.sessionID,
		"query":           string(queryBytes),
	}

	// Python 注释：用 run_agent_streaming 而非 run_agent，以确保 session.post_run() 被调用，
	// 从而将对话历史持久化到 checkpoint
	ch, err := runner.RunAgentStreaming(summaryCtx, runner.ByAgent(agent), inputs, runner.SessionRef{}, nil, nil, nil)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[MultiSessionToolkit] summary streaming failed")
		return
	}
	var accumulated []string
	var finalOutput string
	for chunk := range ch {
		// 偏差#1：stream.Schema 接口三态适配（Python hasattr type/payload 判定）
		var chunkType string
		var payload any
		switch s := chunk.(type) {
		case *stream.OutputSchema:
			chunkType = s.Type
			payload = s.Payload
		case *stream.CustomSchema:
			chunkType = s.Type
			payload = s.Data
		default:
			continue // TraceSchema 等无 type 语义 → skip
		}
		payloadMap, _ := payload.(map[string]any)
		switch chunkType {
		case "content_chunk":
			if c, _ := payloadMap["content"].(string); c != "" {
				accumulated = append(accumulated, c)
			}
		case "answer":
			switch out := payloadMap["output"].(type) {
			case map[string]any:
				temp := ""
				if s, _ := out["output"].(string); s != "" {
					temp = s
				} else if len(accumulated) > 0 {
					temp = strings.Join(accumulated, "")
				}
				if temp != "" {
					finalOutput = temp
				}
			case string:
				if out != "" {
					finalOutput = out
				}
			default:
				if len(accumulated) > 0 {
					finalOutput = strings.Join(accumulated, "")
				}
			}
		}
	}
	if finalOutput == "" {
		finalOutput = strings.Join(accumulated, "")
	}
	result := map[string]any{
		"output":      finalOutput,
		"result_type": "answer",
	}
	finalMsg := map[string]any{
		"request_id": t.requestID,
		"channel_id": t.channelID,
		"session_id": t.sessionID,
		"payload": map[string]any{
			"event_type": "chat.final",
			"task_id":    t.sessionID,
			"content":    result,
		},
		"is_complete": true,
	}
	if err := pushFunc(summaryCtx, finalMsg); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[MultiSessionToolkit] push chat.final failed")
	}
}
