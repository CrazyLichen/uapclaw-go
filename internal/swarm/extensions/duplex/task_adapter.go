package duplex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex/tasks"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// AgentQueryTimeout AgentServer unary 请求超时。
// 对齐 task_adapter.py:26 AGENT_QUERY_TIMEOUT=20。
const AgentQueryTimeout = 20 * time.Second

// EventSendTimeout 事件推送超时。
// 对齐 task_adapter.py:27 EVENT_SEND_TIMEOUT=2。
const EventSendTimeout = 2 * time.Second

// ──────────────────────────── 结构体 ────────────────────────────

// AgentRequestTimeoutError AgentServer unary 请求超时。
// 对齐 task_adapter.py:28-31 AgentRequestTimeoutError。
type AgentRequestTimeoutError struct{}

func (e *AgentRequestTimeoutError) Error() string { return "AGENT_SERVER_TIMEOUT" }

// AgentTaskExecutor Video RPC 与 Agent RPC 的适配器（共享 Host 任务服务）。
// 对齐 task_adapter.py:130-316 AgentTaskExecutor。
type AgentTaskExecutor struct {
	client    AgentClient
	store     *tasks.TaskStore
	normalize func(params map[string]any, coreSessionID string) error
	endpoint  *tasks.GatewayTaskEndpoint
	logEvent  func(map[string]any)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentTaskExecutor 构造执行器。
// 对齐 task_adapter.py:137-143 __init__。
func NewAgentTaskExecutor(client AgentClient, store *tasks.TaskStore, normalize func(map[string]any, string) error, logEvent func(map[string]any)) (*AgentTaskExecutor, error) {
	endpoint, err := tasks.NewGatewayTaskEndpoint(store)
	if err != nil {
		return nil, err
	}
	return &AgentTaskExecutor{
		client: client, store: store, normalize: normalize,
		endpoint: endpoint, logEvent: logEvent,
	}, nil
}

// Close 关闭执行器。
// 对齐 task_adapter.py:145-147 close。
func (e *AgentTaskExecutor) Close() error {
	if e.endpoint != nil {
		e.endpoint.Close()
	}
	return nil
}

// Run 执行一个任务。
// 对齐 task_adapter.py:149-205 run。
func (e *AgentTaskExecutor) Run(ctx context.Context, task map[string]any, progress func(map[string]any) error) (map[string]any, error) {
	request := toMap(task["request"])
	independent := request["independent"] == true
	dependencies := toStringSlice(request["depends_on"])
	completed := []map[string]any{}
	if err := e.store.Transaction(func(db *sql.Tx) error {
		rows, err := e.store.Rows(db, toString(task["owner"]), toString(task["session"]))
		if err != nil {
			return err
		}
		for _, previous := range rows {
			if toString(previous["status"]) != "completed" || previous["result"] == nil {
				continue
			}
			if independent && !containsString(dependencies, toString(previous["id"])) {
				continue
			}
			completed = append(completed, previous)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	contextItems := []map[string]any{}
	window := completed
	if !independent && len(window) > 6 {
		window = window[len(window)-6:]
	}
	for _, previous := range window {
		result := toMap(previous["result"])
		files := []any{}
		for _, f := range toAnySlice(result["files"]) {
			fm := toMap(f)
			files = append(files, map[string]any{
				"name": toString(fm["name"]), "path": toString(fm["path"]),
			})
		}
		contextItems = append(contextItems, map[string]any{
			"question": toString(previous["instruction"]),
			"result":   toString(result["answer"]),
			"files":    files,
		})
	}
	if request["prior_result"] != nil {
		contextItems = append(contextItems, map[string]any{
			"question": "Previous revision",
			"result":   toString(toMap(request["prior_result"])["answer"]),
		})
	}
	var result map[string]any
	err := func() (runErr error) {
		defer RecoverDuplexError(&runErr)
		result, runErr = e.run(ctx, task, progress, contextItems)
		return runErr
	}()
	if err == nil {
		current, _ := e.store.ReadDirect(toString(task["id"]), nil, nil)
		if interaction, ok := current["interaction"].(map[string]any); ok &&
			toString(interaction["state"]) == "pending" {
			return nil, &tasks.ExecutionUncertain{Msg: "Answer channel closed before the question was answered"}
		}
		if current["execution_bound"] == true {
			if err := e.waitSettled(task); err != nil {
				return nil, err
			}
		}
		// send_file_to_user 是服务端推送，不属本 RPC 流（对齐 :168 注释）
		files := toAnySlice(result["files"])
		requestIDs := append(toAnySlice(task["prior_request_ids"]), task["request_id"])
		for _, rid := range requestIDs {
			payload, err := TaskAgentQuery(e.client, "voice.task.files",
				map[string]any{"execution_request_id": rid},
				toString(current["core_session_id"]), toString(task["owner"]))
			if err != nil {
				// 单轮回查失败不影响其余结果收集
				result["artifact_lookup_failed"] = true
				continue
			}
			for _, item := range toAnySlice(payload["files"]) {
				if !containsAny(toAnySlice(files), item) {
					files = append(files, item)
				}
			}
		}
		if len(files) > 0 {
			result["files"] = files
		}
		return result, nil
	}
	current, _ := e.store.ReadDirect(toString(task["id"]), nil, nil)
	if current != nil && current["execution_bound"] == true && current["execution_settled"] != true {
		return nil, &tasks.ExecutionUncertain{Msg: "Execution cleanup is unconfirmed; no work was replayed"}
	}
	return nil, err
}

// Cancel 取消执行（CHAT_CANCEL 确认后等 settled）。
// 对齐 task_adapter.py:287-305 cancel。
func (e *AgentTaskExecutor) Cancel(ctx context.Context, task map[string]any) error {
	request := e2a.E2AFromAgentFields("cancel-"+toString(task["request_id"]),
		e2a.WithFieldChannelID(VideoToolChannelID),
		e2a.WithFieldSessionID(toString(task["core_session_id"])),
		e2a.WithFieldUserID(orEmptyOwner(task["owner"])),
		e2a.WithFieldReqMethod("chat.cancel"),
		e2a.WithFieldParams(map[string]any{
			"intent": "cancel", "mode": "agent", "work_mode": "work",
			"managed_task_request": task["request_id"],
		}),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(float64(time.Now().UnixMilli())/1e6))
	done := make(chan *schema.AgentResponse, 1)
	go func() {
		done <- e.client.SendRequest(ctx, request)
	}()
	var response *schema.AgentResponse
	select {
	case response = <-done:
	case <-time.After(AgentQueryTimeout):
		return &AgentRequestTimeoutError{}
	}
	if response == nil || !response.OK || response.Payload["success"] == false {
		return errors.New("Agent has not confirmed the stop request")
	}
	// 等确切执行的本地落定（对齐 :303-305）
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := e.store.ReadDirect(toString(task["id"]), nil, nil)
		if current != nil && current["execution_settled"] == true {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("Stop accepted; execution settlement is not confirmed")
}

// Answer 向仍在输出的执行投递答案。
// 对齐 task_adapter.py:210-266 answer_input/answer。
func (e *AgentTaskExecutor) Answer(ctx context.Context, task map[string]any) error {
	interaction := toMap(task["interaction"])
	answers := toAnySlice(interaction["answers"])
	answerPairs := []any{}
	for _, a := range answers {
		am := toMap(a)
		pair := map[string]any{
			"question":         am["question"],
			"custom_input":     am["answer"],
			"selected_options": am["selected_options"],
		}
		if cardID, ok := am["card_id"].(string); ok && cardID != "" {
			pair["card_id"] = cardID
		}
		answerPairs = append(answerPairs, pair)
	}
	requestID := "answer-" + toString(interaction["operation_id"])
	if task["resume_answer"] == true {
		requestID = toString(task["request_id"])
	}
	params := map[string]any{
		"answers":     answerPairs,
		"source":      interaction["source"],
		"mode":        "agent",
		"work_mode":   "work",
		"log_as_user": false,
	}
	if interaction["interrupt_id"] != nil {
		params["interrupt_id"] = interaction["interrupt_id"]
	}
	request := e2a.E2AFromAgentFields(requestID,
		e2a.WithFieldChannelID(VideoToolChannelID),
		e2a.WithFieldSessionID(toString(task["core_session_id"])),
		e2a.WithFieldUserID(orEmptyOwner(task["owner"])),
		e2a.WithFieldReqMethod("chat.answer"),
		e2a.WithFieldParams(params),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(float64(time.Now().UnixMilli())/1e6))
	response := e.client.SendRequest(ctx, request)
	if response == nil || !response.OK || response.Payload["success"] == false {
		return errors.New("Agent did not accept the answer")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// run 单次执行入口（对齐 :254-285 _run）。
func (e *AgentTaskExecutor) run(ctx context.Context, task map[string]any, progress func(map[string]any) error, contextItems []map[string]any) (map[string]any, error) {
	request := toMap(task["request"])
	opts := CoreAgentOptions{
		Question:                  toString(task["instruction"]),
		Query:                     orChainStr(request["query"], task["instruction"]),
		VisualContext:             toString(request["visual_context"]),
		SearchSessionID:           toString(task["session"]),
		CoreSessionID:             toString(task["core_session_id"]),
		RequestID:                 toString(task["request_id"]),
		UserID:                    orEmptyOwner(task["owner"]),
		DelegationContext:         contextItems,
		FrameDataURL:              toString(request["frame_data_url"]),
		NormalizeMediaAttachments: e.normalize,
		OnProgress:                progress,
		BriefNonce:                toString(task["id"]),
		ManagedTaskBinding:        e.endpoint.Binding(task),
	}
	if task["resume_answer"] == true {
		opts.InteractionAnswer = map[string]any{
			"params": map[string]any{
				"answers":      answerPairsOf(task),
				"source":       toMap(task["interaction"])["source"],
				"mode":         "agent",
				"work_mode":    "work",
				"interrupt_id": toMap(task["interaction"])["interrupt_id"],
			},
			"req_method": "chat.answer",
		}
	}
	result := ExecuteCoreAgent(ctx, e.client, opts)
	out := toMap(result.FinalPayload)
	if out == nil {
		out = map[string]any{}
	}
	out["display_result"] = result.DisplayResult
	out["realtime_brief"] = result.RealtimeBrief
	out["answer"] = result.Answer
	out["raw_answer_chars"] = result.RawAnswerChars
	out["tools_used"] = result.ToolsUsed
	return out, nil
}

// waitSettled 等执行落定（20 秒上限）。
// 对齐 task_adapter.py:268-285 wait_settled。
func (e *AgentTaskExecutor) waitSettled(task map[string]any) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := e.store.ReadDirect(toString(task["id"]), nil, nil)
		if current != nil && current["execution_settled"] == true {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return &tasks.ExecutionUncertain{Msg: "Execution cleanup is unconfirmed; no work was replayed"}
}

// TaskAgentQuery 从路由的 AgentServer 读取（不经网关本地会话树）。
// 对齐 task_adapter.py:107-127 task_agent_query。
func TaskAgentQuery(client AgentClient, method string, params map[string]any, session, owner string) (map[string]any, error) {
	if client == nil {
		return nil, errors.New("AgentServer client is unavailable")
	}
	request := e2a.E2AFromAgentFields("task-query-"+fmt.Sprintf("%d", time.Now().UnixNano()),
		e2a.WithFieldChannelID(VideoToolChannelID),
		e2a.WithFieldSessionID(session),
		e2a.WithFieldUserID(owner),
		e2a.WithFieldReqMethod(method),
		e2a.WithFieldParams(params),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(float64(time.Now().UnixMilli())/1e6))
	done := make(chan *schema.AgentResponse, 1)
	go func() { done <- client.SendRequest(context.Background(), request) }()
	var response *schema.AgentResponse
	select {
	case response = <-done:
	case <-time.After(AgentQueryTimeout):
		return nil, &AgentRequestTimeoutError{}
	}
	payload := map[string]any{}
	if response != nil && response.Payload != nil {
		payload = response.Payload
	}
	if response == nil || !response.OK {
		return nil, errors.New(orChainStr(payload["error"], "Task AgentServer query failed"))
	}
	return payload, nil
}

// TaskIdentity 使用 WebChannel 用户作用域；连接准入属网关。
// 对齐 task_adapter.py:76-103 task_identity。
func TaskIdentity(ws any, scope string) (string, string, error) {
	if scope == "" {
		return "", "", errors.New("Invalid task conversation")
	}
	if len(scope) > 200 || strings.ContainsAny(scope, "/\\\x00") {
		return "", "", errors.New("Invalid task conversation")
	}
	// 空 user 即现有匿名作用域，不是服务器 OS 用户（对齐 :86 注释）
	owner := ConnectionUserID(ws)
	session := ""
	if strings.HasPrefix(scope, "task-duplex:") {
		session = strings.TrimPrefix(scope, "task-duplex:")
		if session == "." || session == ".." ||
			strings.ContainsAny(session, `<>:"|?*`) ||
			strings.HasSuffix(session, ".") || strings.HasSuffix(session, " ") {
			return "", "", errors.New("Invalid saved conversation identity")
		}
		if session == "new" {
			return "", "", errors.New("Conversation is unavailable to this user")
		}
	}
	return owner, scope, nil
}

// ConnectionUserID 取连接用户 ID（网关注入的鸭子类型 → 窄取法）。
// Python 侧无对应实现：Python 引 WebChannel.connection_user_id，Go 由网关桥接注入。
var ConnectionUserID = func(ws any) string {
	type userIDCarrier interface{ ConnectionUserID() string }
	if c, ok := ws.(userIDCarrier); ok {
		return c.ConnectionUserID()
	}
	return ""
}

// answerPairsOf 构造答案对（对齐 :216-247 answer_input 的 answers 投影）。
func answerPairsOf(task map[string]any) []any {
	answers := toAnySlice(toMap(task["interaction"])["answers"])
	pairs := []any{}
	for _, a := range answers {
		am := toMap(a)
		pair := map[string]any{"question": am["question"], "custom_input": am["answer"]}
		if selected, ok := am["selected_options"].([]any); ok {
			pair["selected_options"] = selected
		}
		if cardID, ok := am["card_id"].(string); ok && cardID != "" {
			pair["card_id"] = cardID
		}
		pairs = append(pairs, pair)
	}
	return pairs
}

// orEmptyOwner 空 owner 回空串（nil 保持 nil 语义由 store 校验）。
func orEmptyOwner(value any) string {
	return toString(value)
}

// orChainStr any 链取第一个非空字符串。
func orChainStr(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// containsAny 判断列表包含（any 深比较按 JSON 编码）。
func containsAny(list []any, target any) bool {
	encoded, _ := json.Marshal(target)
	for _, item := range list {
		data, _ := json.Marshal(item)
		if string(data) == string(encoded) {
			return true
		}
	}
	return false
}

var _ = sync.Mutex{}
var _ = sort.Strings

// ──────────────────────────── 通用辅助（任务适配） ────────────────────────────

// toMap any → map（nil 安全）。
// Python 侧无对应实现：鸭子类型取法的显式化。
func toMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

// toAnySlice any → []any。
func toAnySlice(value any) []any {
	list, _ := value.([]any)
	return list
}

// toStringSlice any → []string。
func toStringSlice(value any) []string {
	list, _ := value.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, toString(item))
	}
	return out
}

// containsString 判定包含。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// ──────────────────────────── 结构体（VideoSearchManager） ────────────────────────────

// VideoSearchManager 仅传输与 provider 投影；任务状态全部归 TaskService。
// 对齐 task_adapter.py:308-333 VideoSearchManager。
type VideoSearchManager struct {
	channel    DuplexChannel
	logEvent   func(map[string]any)
	qwenActive func() bool
	client     AgentClient
	normalize  func(map[string]any, string) error

	path        string
	authorize   func(ws any, scope string) (string, string, error)
	concurrency int

	mu            sync.Mutex
	service       *tasks.TaskService
	subscribers   map[string]any // key: owner|session|wsToken
	approvalCards map[string]string
	executor      *AgentTaskExecutor
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVideoSearchManager 构造管理器。
// 对齐 task_adapter.py:315-332 __init__。
func NewVideoSearchManager(channel DuplexChannel, client AgentClient, normalize func(map[string]any, string) error, logEvent func(map[string]any), qwenActive func() bool, path string) *VideoSearchManager {
	return &VideoSearchManager{
		channel: channel, logEvent: logEvent, qwenActive: qwenActive,
		client: client, normalize: normalize, path: path,
		authorize:     TaskIdentity,
		concurrency:   2,
		subscribers:   map[string]any{},
		approvalCards: map[string]string{},
	}
}

// Service 惰性构造任务服务。
// 对齐 task_adapter.py:334-348 service property。
func (m *VideoSearchManager) Service() (*tasks.TaskService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.service != nil {
		return m.service, nil
	}
	store, err := tasks.NewTaskStore(orFallbackPath(m.path))
	if err != nil {
		return nil, err
	}
	executor, err := NewAgentTaskExecutor(m.client, store, m.normalize, m.logEvent)
	if err != nil {
		return nil, err
	}
	m.executor = executor
	svc, err := tasks.NewTaskService(store, executor,
		func(task map[string]any) error { return m.changed(task) },
		m.concurrency, AgentQueryTimeout+2*EventSendTimeout+time.Second)
	if err != nil {
		return nil, err
	}
	m.service = svc
	return svc, nil
}

// Scope 订阅作用域。
// 对齐 task_adapter.py:350-353 scope。
func (m *VideoSearchManager) Scope(ws any, params map[string]any) (string, string, error) {
	owner, session, err := m.AuthorizedScope(ws, toString(params["search_session_id"]))
	if err != nil {
		return "", "", err
	}
	m.mu.Lock()
	m.subscribers[scopeKey(owner, session, ws)] = ws
	m.mu.Unlock()
	return owner, session, nil
}

// AuthorizedScope 作用域鉴权（task-duplex: 前缀校验归属）。
// 对齐 task_adapter.py:355-370 authorized_scope。
func (m *VideoSearchManager) AuthorizedScope(ws any, scope string) (string, string, error) {
	owner, session, err := m.authorize(ws, scope)
	if err != nil {
		return "", "", err
	}
	if strings.HasPrefix(session, "task-duplex:") {
		savedSession := strings.TrimPrefix(session, "task-duplex:")
		metadata, err := TaskAgentQuery(m.client, "session.get_metadata",
			map[string]any{"session_id": savedSession}, savedSession, owner)
		if err != nil {
			return "", "", err
		}
		if toString(metadata["user_id"]) != owner {
			return "", "", errors.New("Conversation is unavailable to this user")
		}
	}
	return owner, session, nil
}

// Public 任务公开投影。
// 对齐 task_adapter.py:377-418 public（静态方法 → 导出函数）。
func (m *VideoSearchManager) Public(task map[string]any) map[string]any {
	request := toMap(task["request"])
	result := toMap(task["result"])
	files := toAnySlice(result["files"])
	if len(files) == 0 {
		for _, entry := range toAnySlice(task["progress"]) {
			em := toMap(entry)
			if toString(em["stage"]) == "file" {
				files = append(files, toAnySlice(em["files"])...)
			}
		}
	}
	public := map[string]any{
		"engine":                 "Jiuwen Core Agent",
		"id":                     task["id"],
		"job_id":                 task["id"],
		"search_session_id":      task["session"],
		"question":               task["instruction"],
		"query":                  orChainStr(request["query"], task["instruction"]),
		"status":                 task["status"],
		"revision":               task["revision"],
		"sequence":               task["sequence"],
		"result":                 result["answer"],
		"display_result":         result["display_result"],
		"realtime_brief":         result["realtime_brief"],
		"files":                  files,
		"artifact_lookup_failed": result["artifact_lookup_failed"] == true,
		"progress_history":       task["progress"],
		"error":                  task["error"],
		"adjustments":            task["changes"],
		"successor_id":           task["successor_id"],
		"parent_id":              task["parent_id"],
		"tool_call_id":           request["tool_call_id"],
		"tool_name":              request["tool_name"],
		"turn_id":                request["turn_id"],
		"reused":                 task["reused"] == true,
		"independent":            request["independent"] == true,
		"depends_on":             request["depends_on"],
		"resources":              request["resources"],
		"interaction":            task["interaction"],
		"wait_reason":            task["wait_reason"],
	}
	if last, ok := lastProgress(task); ok {
		public["progress"] = last
	} else {
		public["progress"] = nil
	}
	return public
}

// VoiceJob 返回控制事实（不重复 UI 的推理/工具历史）。
// 对齐 task_adapter.py:421-469 voice_job（静态方法 → 导出函数）。
func (m *VideoSearchManager) VoiceJob(job map[string]any) map[string]any {
	fields := []string{
		"id", "job_id", "status", "revision", "queue_version", "queue_position",
		"independent", "parent_id", "successor_id", "wait_reason",
	}
	result := map[string]any{}
	for _, key := range fields {
		result[key] = job[key]
	}
	for _, key := range []string{"question", "query", "error"} {
		text := toString(job[key])
		result[key] = runePreview(text, 1000)
		if len([]rune(text)) > 1000 {
			result[key+"_truncated"] = true
		}
	}
	brief := toMap(job["realtime_brief"])
	text := orChainStr(brief["summary"], job["display_result"], job["result"])
	result["result_summary"] = runePreview(text, 1200)
	result["result_truncated"] = len([]rune(text)) > 1200
	changes := toAnySlice(job["adjustments"])
	result["adjustment_count"] = len(changes)
	adjustments := []any{}
	tail := changes
	if len(tail) > 3 {
		tail = tail[len(tail)-3:]
	}
	for _, c := range tail {
		change := toMap(c)
		entry := map[string]any{}
		for _, key := range []string{"id", "state", "instruction", "error"} {
			entry[key] = runePreview(toString(change[key]), 500)
		}
		adjustments = append(adjustments, entry)
	}
	result["adjustments"] = adjustments
	files := toAnySlice(job["files"])
	if len(files) > 0 {
		result["artifact_status"] = "reported_by_agent"
	} else {
		result["artifact_status"] = "not_confirmed"
	}
	result["artifact_lookup_failed"] = job["artifact_lookup_failed"] == true
	filtered := []any{}
	for _, f := range files {
		fm := toMap(f)
		if len(fm["path"].(string)) > 1024 || len(toString(fm["path"])) > 1024 {
			continue
		}
		if len(toString(fm["name"])) > 256 {
			continue
		}
		filtered = append(filtered, map[string]any{"name": toString(fm["name"]), "path": toString(fm["path"])})
		if len(filtered) == 5 {
			break
		}
	}
	result["files"] = filtered
	result["files_truncated"] = len(files) != len(filtered)
	interaction := job["interaction"]
	if interaction != nil {
		encoded, _ := json.Marshal(interaction)
		if len(encoded) > 8000 {
			// 绝不截断答案必须绑定的确切提问（对齐 :466 注释）
			result["interaction_requires_ui"] = true
		} else {
			result["interaction"] = interaction
		}
	}
	encoded, _ := json.Marshal(result)
	if len(encoded) > 12000 {
		// 五个任务仍远低于 provider 的 256 KiB 帧上限（对齐 :473 注释）
		fallback := map[string]any{}
		for _, key := range fields {
			fallback[key] = job[key]
		}
		fallback["question"] = runePreview(toString(job["question"]), 1000)
		fallback["details_omitted"] = true
		fallback["artifact_status"] = "not_in_response"
		return fallback
	}
	return result
}

// Start 经工具调用/委派入口提交任务。
// 对齐 task_adapter.py:519-540 start。
func (m *VideoSearchManager) Start(ctx context.Context, ws any, question, query, searchSessionID, visualContext, frameDataURL, toolCallID, toolName, turnID, commandID string, scheduling map[string]any) (map[string]any, error) {
	owner, scope, err := m.Scope(ws, map[string]any{"search_session_id": searchSessionID})
	if err != nil {
		return nil, err
	}
	request := map[string]any{
		"query": query, "visual_context": visualContext,
		"frame_data_url": frameDataURL, "tool_call_id": toolCallID,
		"tool_name": toolName, "turn_id": turnID,
	}
	for k, v := range scheduling {
		request[k] = v
	}
	svc, err := m.Service()
	if err != nil {
		return nil, err
	}
	task, err := svc.Submit(owner, scope, orChainStr(commandID, toolCallID), orChainStr(question, query), request)
	if err != nil {
		return nil, err
	}
	return m.Public(task), nil
}

// Operate 执行任务管理工具调用。
// 对齐 task_adapter.py:574-660 operate。
func (m *VideoSearchManager) Operate(ctx context.Context, owner, scope string, call QwenOmniToolCall) (map[string]any, error) {
	args := call.Arguments
	svc, err := m.Service()
	if err != nil {
		return nil, err
	}
	switch call.Name {
	case "jiuwen_task_query":
		snapshot, err := m.Snapshot(owner, scope)
		if err != nil {
			return nil, err
		}
		jobs := toAnySlice(snapshot["jobs"])
		if args["job_id"] != nil {
			if _, err := svc.Get(owner, scope, toString(args["job_id"])); err != nil {
				return nil, err // 保留归属校验（对齐 :579 注释）
			}
			filtered := []any{}
			for _, j := range jobs {
				if toString(toMap(j)["id"]) == toString(args["job_id"]) {
					filtered = append(filtered, j)
				}
			}
			jobs = filtered
		} else {
			query := strings.ToLower(toString(args["query"]))
			filtered := []any{}
			for _, j := range jobs {
				if strings.Contains(strings.ToLower(toString(toMap(j)["question"])), query) {
					filtered = append(filtered, j)
				}
			}
			jobs = filtered
		}
		counts := map[string]int{}
		unfinished := 0
		for _, j := range jobs {
			status := toString(toMap(j)["status"])
			counts[status]++
			if status != "completed" && status != "failed" && status != "cancelled" {
				unfinished += counts[status] - (counts[status] - 1) + 0
			}
		}
		// 重新精确统计未完成数
		unfinished = 0
		for state, n := range counts {
			if state != "completed" && state != "failed" && state != "cancelled" {
				unfinished += n
			}
		}
		selected := jobs
		status := toString(args["status"])
		if status == "unfinished" {
			filtered := []any{}
			for _, j := range jobs {
				st := toString(toMap(j)["status"])
				if st != "completed" && st != "failed" && st != "cancelled" {
					filtered = append(filtered, j)
				}
			}
			selected = filtered
		} else if status != "" {
			filtered := []any{}
			for _, j := range jobs {
				if toString(toMap(j)["status"]) == status {
					filtered = append(filtered, j)
				}
			}
			selected = filtered
		}
		offset := numInt(args["offset"])
		end := offset + 5
		if end > len(selected) {
			end = len(selected)
		}
		if offset > len(selected) {
			offset = len(selected)
		}
		page := selected[offset:end]
		voiceJobs := []any{}
		for _, j := range page {
			voiceJobs = append(voiceJobs, m.VoiceJob(toMap(j)))
		}
		allFinished := len(jobs) > 0 && unfinished == 0
		allSucceeded := len(jobs) > 0 && counts["completed"] == len(jobs)
		result := map[string]any{
			"jobs": voiceJobs,
			"scope": map[string]any{
				"session_id": scope, "job_id": args["job_id"],
				"query": toString(args["query"]), "status": status,
			},
			"summary": map[string]any{
				"total": len(jobs), "status_counts": counts,
				"unfinished": unfinished, "all_finished": allFinished,
				"all_succeeded": allSucceeded,
			},
			"matched_total": len(selected),
			"message":       tasks.TaskQueryInstructions(len(jobs), counts, unfinished, len(page), len(selected)),
		}
		for k, v := range snapshot {
			if k != "jobs" {
				result[k] = v
			}
		}
		if offset+5 < len(selected) {
			result["next_offset"] = offset + 5
		} else {
			result["next_offset"] = nil
		}
		if len(jobs) == 0 {
			result["message"] = tasks.EmptyQueryInstructions
		}
		return result, nil
	case "jiuwen_task_cancel":
		return svc.Cancel(ctx, owner, scope, toString(args["job_id"]), call.CallID)
	case "jiuwen_task_reorder":
		if toString(args["action"]) == "before" && toString(args["before_job_id"]) == "" {
			return nil, errors.New("before requires before_job_id")
		}
		receipt, err := svc.Reorder(owner, scope, toString(args["job_id"]), numInt(args["queue_version"]), toString(args["before_job_id"]), call.CallID)
		if err != nil && errors.Is(err, tasks.QueueVersionConflict) {
			receipt = map[string]any{
				"task_id": args["job_id"], "state": "rejected", "applied": false,
				"code": "QUEUE_VERSION_CONFLICT", "error": err.Error(),
			}
		} else if err != nil {
			return nil, err
		}
		snapshot, err := m.Snapshot(owner, scope)
		if err != nil {
			return nil, err
		}
		waiting := []map[string]any{}
		for _, j := range toAnySlice(snapshot["jobs"]) {
			if toString(toMap(j)["status"]) == "queued" {
				waiting = append(waiting, toMap(j))
			}
		}
		sort.Slice(waiting, func(i, j int) bool {
			return numInt(waiting[i]["queue_position"]) < numInt(waiting[j]["queue_position"])
		})
		var target map[string]any
		for _, j := range toAnySlice(snapshot["jobs"]) {
			if toString(toMap(j)["id"]) == toString(args["job_id"]) {
				target = toMap(j)
			}
		}
		index := 0
		for i, t := range waiting {
			if toString(t["id"]) == toString(target["id"]) {
				index = i
				break
			}
		}
		start := index - 1
		if start < 0 {
			start = 0
		}
		end := index + 4
		if end > len(waiting) {
			end = len(waiting)
		}
		windowJobs := []any{}
		for _, t := range waiting[start:end] {
			windowJobs = append(windowJobs, m.VoiceJob(t))
		}
		result := map[string]any{
			"operation": receipt, "queue_version": snapshot["queue_version"],
			"target": m.VoiceJob(target), "jobs": windowJobs,
			"waiting_total": len(waiting), "message": tasks.ReorderInstructions,
		}
		if toString(receipt["state"]) == "rejected" {
			for k, v := range receipt {
				result[k] = v
			}
			result["message"] = tasks.QueueConflictInstructions
		}
		return result, nil
	case "jiuwen_task_answer":
		return svc.Answer(ctx, owner, scope, toString(args["job_id"]), call.CallID,
			toString(args["interaction_id"]), toAnySlice(args["answers"]), false)
	default:
		result, err := svc.Modify(owner, scope, toString(args["job_id"]), call.CallID,
			tasks.TaskModification{Revision: numInt(args["revision"]), Instruction: toString(args["instruction"])})
		if err != nil && errors.Is(err, tasks.TaskRevisionConflict) {
			current, getErr := svc.Get(owner, scope, toString(args["job_id"]))
			if getErr != nil {
				return nil, getErr
			}
			return map[string]any{
				"state": "rejected", "error": err.Error(), "applied": false,
				"current_task": m.VoiceJob(m.Public(current)),
				"message":      tasks.RevisionConflictInstructions,
			}, nil
		}
		return result, err
	}
}

// Snapshot 快照与排队位置投影。
// 对齐 task_adapter.py:541-556 snapshot。
func (m *VideoSearchManager) Snapshot(owner, scope string) (map[string]any, error) {
	svc, err := m.Service()
	if err != nil {
		return nil, err
	}
	tasksList, version, err := svc.Snapshot(owner, scope)
	if err != nil {
		return nil, err
	}
	queued := []map[string]any{}
	for _, t := range tasksList {
		if toString(t["status"]) == "queued" {
			queued = append(queued, t)
		}
	}
	sort.Slice(queued, func(i, j int) bool { return numInt(queued[i]["position"]) < numInt(queued[j]["position"]) })
	positions := map[string]int{}
	for i, t := range queued {
		positions[toString(t["id"])] = i + 1
	}
	jobs := []any{}
	for _, t := range tasksList {
		job := m.Public(t)
		job["queue_version"] = version
		job["replay"] = true
		job["queue_position"] = positions[toString(t["id"])]
		jobs = append(jobs, job)
	}
	return map[string]any{
		"search_session_id": scope,
		"queue_version":     version,
		"max_concurrency":   m.concurrency,
		"concurrency_scope": "owned_active_executions; unknown execution activity is unconfirmed",
		"jobs":              jobs,
	}, nil
}

// Changed 变更通知投递。
// 对齐 task_adapter.py:471-517 changed。
func (m *VideoSearchManager) changed(task map[string]any) error {
	state := toString(task["status"])
	if state == "failed" || state == "completed" || state == "cancelled" {
		m.logEvent(map[string]any{"stage": "search_" + state, "job_id": task["id"]})
	}
	event := "progress"
	if state == "completed" || state == "failed" || state == "cancelled" {
		event = state
	}
	m.mu.Lock()
	subscribers := map[string]any{}
	for key, ws := range m.subscribers {
		if strings.HasPrefix(key, scopeKeyPrefix(toString(task["owner"]), toString(task["session"]))) {
			subscribers[key] = ws
		}
	}
	m.mu.Unlock()
	incomplete := false
	for key, ws := range subscribers {
		if err := m.deliverTo(key, ws, task, event); err != nil {
			incomplete = true
		}
	}
	if incomplete {
		return errors.New("Task notification delivery was incomplete")
	}
	return nil
}

// deliverTo 单订阅者投递（含审批卡同步）。
// 对齐 task_adapter.py:476-493 deliver。
func (m *VideoSearchManager) deliverTo(key string, ws any, task map[string]any, event string) error {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) < 3 {
		return errors.New("bad subscriber key")
	}
	owner, session := parts[0], parts[1]
	gotOwner, gotSession, err := m.AuthorizedScope(ws, session)
	if err != nil || gotOwner != owner || gotSession != session {
		m.mu.Lock()
		delete(m.subscribers, key)
		for cardKey := range m.approvalCards {
			if strings.HasPrefix(cardKey, key) {
				delete(m.approvalCards, cardKey)
			}
		}
		m.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), EventSendTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.channel.SendEventChecked(ctx, ws, "video.search."+event, m.Public(task)) }()
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	done2 := make(chan error, 1)
	go func() {
		snapshot, err := m.Snapshot(owner, session)
		if err != nil {
			done2 <- err
			return
		}
		done2 <- m.channel.SendEventChecked(ctx, ws, "video.search.queue", snapshot)
	}()
	select {
	case err := <-done2:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return m.syncApprovalCard(ctx, ws, task, key)
}

// SyncApprovalCard 审批卡同步。
// 对齐 task_adapter.py:495-517 _sync_approval_card。
func (m *VideoSearchManager) syncApprovalCard(ctx context.Context, ws any, task map[string]any, key string) error {
	interaction := toMap(task["interaction"])
	cardKey := key + "|" + toString(task["id"])
	session := toString(task["session"])
	pending := strings.HasPrefix(session, "task-duplex:") &&
		toString(task["status"]) == "waiting_user" &&
		toString(interaction["state"]) == "pending" &&
		(interaction["source"] == "permission_interrupt" || interaction["source"] == "confirm_interrupt")
	m.mu.Lock()
	defer m.mu.Unlock()
	if pending {
		if m.approvalCards[cardKey] == toString(interaction["id"]) {
			return nil
		}
		payload := map[string]any{
			"session_id":    strings.TrimPrefix(session, "task-duplex:"),
			"duplex_job_id": task["id"],
			"request_id":    interaction["request_id"],
			"source":        interaction["source"],
			"questions":     interaction["questions"],
		}
		if interaction["interrupt_id"] != nil {
			payload["interrupt_id"] = interaction["interrupt_id"]
		}
		if interaction["approval_schema"] != nil {
			payload["approval_schema"] = interaction["approval_schema"]
		}
		done := make(chan error, 1)
		go func() { done <- m.channel.SendEventChecked(ctx, ws, "chat.ask_user_question", payload) }()
		select {
		case err := <-done:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		m.approvalCards[cardKey] = toString(interaction["id"])
		return nil
	}
	if _, exists := m.approvalCards[cardKey]; exists {
		done := make(chan error, 1)
		go func() {
			done <- m.channel.SendEventChecked(ctx, ws, "video.search.confirmation_closed", map[string]any{
				"session_id":    strings.TrimPrefix(session, "task-duplex:"),
				"duplex_job_id": task["id"],
				"request_id":    interaction["request_id"],
			})
		}()
		select {
		case err := <-done:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		delete(m.approvalCards, cardKey)
	}
	return nil
}

// HandleQwenTool video.qwen.tool RPC。
// 对齐 task_adapter.py:548-572 handle_qwen_tool。
func (m *VideoSearchManager) HandleQwenTool(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
	payload, err := func() (result map[string]any, err error) {
		defer RecoverDuplexError(&err)
		owner, scope, err := m.Scope(ws, params)
		if err != nil {
			return nil, err
		}
		if !m.qwenActive() {
			return nil, errors.New("Qwen Omni Realtime is not the active provider")
		}
		call, err := ParseQwenOmniToolCall(params)
		if err != nil {
			return nil, err
		}
		if call.Name == QwenOmniDelegateToolName || call.Name == QwenOmniResearchToolName {
			frame := toString(params["frame_data_url"])
			if frame != "" && (len(frame) > MaxFrameChars || frameMediaItem(frame) == nil) {
				return nil, errors.New("Invalid video frame")
			}
			scheduling := map[string]any{}
			for _, k := range []string{"independent", "depends_on", "resources"} {
				if v, ok := call.Arguments[k]; ok {
					scheduling[k] = v
				}
			}
			task, err := m.Start(ctx, ws,
				orChainStr(params["question"], call.Task),
				call.Task, scope, "", frame, call.CallID, call.Name,
				toString(params["turn_id"]), "", scheduling)
			if err != nil {
				return nil, err
			}
			return map[string]any{"search_job": task, "call_id": call.CallID}, nil
		}
		opResult, err := m.Operate(ctx, owner, scope, call)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tool_result": opResult, "call_id": call.CallID}, nil
	}()
	if err != nil {
		return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
	}
	return &DuplexResponse{OK: true, Payload: payload}
}

// HandleStatus video.search.status RPC。
// 对齐 task_adapter.py:662-669 handle_status。
func (m *VideoSearchManager) HandleStatus(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
	payload, err := func() (map[string]any, error) {
		owner, scope, err := m.Scope(ws, params)
		if err != nil {
			return nil, err
		}
		svc, err := m.Service()
		if err != nil {
			return nil, err
		}
		task, err := svc.Get(owner, scope, toString(params["job_id"]))
		if err != nil {
			return nil, err
		}
		return m.Public(task), nil
	}()
	if err != nil {
		return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
	}
	return &DuplexResponse{OK: true, Payload: payload}
}

// HandleList video.search.list RPC。
// 对齐 task_adapter.py:671-685 handle_list。
func (m *VideoSearchManager) HandleList(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
	payload, err := func() (map[string]any, error) {
		owner, scope, err := m.Scope(ws, params)
		if err != nil {
			return nil, err
		}
		svc, err := m.Service()
		if err != nil {
			return nil, err
		}
		tasksList, cursor, err := svc.List(owner, scope, "", "", numInt(params["offset"]), 50)
		if err != nil {
			return nil, err
		}
		jobs := []any{}
		for _, t := range tasksList {
			_ = m.syncApprovalCard(ctx, ws, t, scopeKey(owner, scope, ws))
			jobs = append(jobs, m.Public(t))
		}
		return map[string]any{"jobs": jobs, "next_offset": cursor, "replay": true}, nil
	}()
	if err != nil {
		return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
	}
	return &DuplexResponse{OK: true, Payload: payload}
}

// HandleAnswer video.search.answer RPC（原生 UI 确认）。
// 对齐 task_adapter.py:687-706 handle_answer。
func (m *VideoSearchManager) HandleAnswer(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
	payload, err := func() (map[string]any, error) {
		visibleSession := toString(params["session_id"])
		if visibleSession == "" {
			return nil, errors.New("Confirmation session is required")
		}
		owner, scope, err := m.Scope(ws, map[string]any{"search_session_id": "task-duplex:" + visibleSession})
		if err != nil {
			return nil, err
		}
		svc, err := m.Service()
		if err != nil {
			return nil, err
		}
		task, err := svc.Get(owner, scope, toString(params["job_id"]))
		if err != nil {
			return nil, err
		}
		interaction := toMap(task["interaction"])
		if toString(interaction["request_id"]) != toString(params["request_id"]) {
			return nil, errors.New("Confirmation is no longer pending")
		}
		return svc.Answer(ctx, owner, scope, toString(task["id"]), reqID,
			toString(interaction["id"]), toAnySlice(params["answers"]), true)
	}()
	if err != nil {
		return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
	}
	return &DuplexResponse{OK: true, Payload: payload}
}

// HandleControl video.search.control RPC。
// 对齐 task_adapter.py:708-742 handle_control。
func (m *VideoSearchManager) HandleControl(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
	payload, err := func() (map[string]any, error) {
		owner, scope, err := m.Scope(ws, params)
		if err != nil {
			return nil, err
		}
		svc, err := m.Service()
		if err != nil {
			return nil, err
		}
		taskID := toString(params["job_id"])
		commandID := orChainStr(params["command_id"], reqID)
		switch toString(params["action"]) {
		case "cancel":
			_, err = svc.Cancel(ctx, owner, scope, taskID, commandID)
			if err != nil {
				return nil, err
			}
		case "next", "before":
			before := toString(params["before_job_id"])
			if (toString(params["action"]) == "before") != (before != "") {
				return nil, errors.New("Only before requires before_job_id")
			}
			if _, err = svc.Reorder(owner, scope, taskID, numInt(params["queue_version"]), before, commandID); err != nil {
				return nil, err
			}
		case "preempt":
			if err = svc.Preempt(ctx, owner, scope, taskID, numInt(params["queue_version"]), commandID); err != nil {
				return nil, err
			}
		case "modify":
			if _, err = svc.Modify(owner, scope, taskID, commandID,
				tasks.TaskModification{Revision: numInt(params["revision"]), Instruction: toString(params["instruction"])}); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("Unsupported control; stop the active task before reordering")
		}
		snapshot, err := m.Snapshot(owner, scope)
		if err != nil {
			return nil, err
		}
		_ = m.channel.SendEventChecked(ctx, ws, "video.search.queue", snapshot)
		return snapshot, nil
	}()
	if err != nil {
		return &DuplexResponse{OK: false, Error: err.Error(), Code: "TASK_REQUEST_REJECTED"}
	}
	return &DuplexResponse{OK: true, Payload: payload}
}

// Close 关停管理器。
// 对齐 task_adapter.py:744-748 close。
func (m *VideoSearchManager) Close() error {
	m.mu.Lock()
	svc := m.service
	m.mu.Unlock()
	if svc != nil {
		return svc.Close()
	}
	if m.executor != nil {
		return m.executor.Close()
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// scopeKey 订阅者复合键。
// 对齐 task_adapter.py:351-352 key=(owner,session,id(ws))。
func scopeKey(owner, session string, ws any) string {
	return owner + "|" + session + "|" + fmt.Sprintf("%p", ws)
}

// scopeKeyPrefix 订阅键前缀。
func scopeKeyPrefix(owner, session string) string {
	return owner + "|" + session + "|"
}

// lastProgress 取最后一个进度条目。
// 对齐 task_adapter.py:416-417 progress=task["progress"][-1]。
func lastProgress(task map[string]any) (map[string]any, bool) {
	progress := toAnySlice(task["progress"])
	if len(progress) == 0 {
		return nil, false
	}
	last, _ := progress[len(progress)-1].(map[string]any)
	return last, true
}

// orFallbackPath 空路径回默认账本。
func orFallbackPath(path string) string {
	if path != "" {
		return path
	}
	return tasks.TaskDatabase()
}

// numInt any 数值取整（缺省 0）。
func numInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}
