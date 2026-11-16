//go:build integration

// Package duplex_test 提供全双工音视频（领域 23.9）跨进程边界的集成测试。
//
// 覆盖链路：duplex 配置面板 RPC → 持久任务账本（SQLite 文件）→ 假 AgentServer
// RPC（网络边界）→ execute_core_agent 全链 → 交互挂起/回答/恢复 → 遥测落盘。
// 真实边界：文件系统（账本/遥测 JSONL）+ RPC 传输（AgentClient 注入）。
package duplex_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex/tasks"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DuplexTaskSuite 双工任务全链集成测试套件
type DuplexTaskSuite struct {
	isuite.BaseIntegrationSuite
}

// stubAgentClient 集成桩：回放响应序列。
type stubAgentClient struct {
	mu        sync.Mutex
	responses []*schema.AgentResponse
	stream    []*schema.AgentResponse
	sent      []*e2a.E2AEnvelope
}

func (s *stubAgentClient) SendRequest(_ context.Context, env *e2a.E2AEnvelope) *schema.AgentResponse {
	s.mu.Lock()
	s.sent = append(s.sent, env)
	s.mu.Unlock()
	if len(s.responses) == 0 {
		return &schema.AgentResponse{OK: false, Payload: map[string]any{"error": "no responses"}}
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	return resp
}

func (s *stubAgentClient) SendRequestStream(_ context.Context, env *e2a.E2AEnvelope) <-chan *schema.AgentResponse {
	s.mu.Lock()
	s.sent = append(s.sent, env)
	s.mu.Unlock()
	// 对齐 Python 行为：模型从提示词中的语音回执协议提取真实 brief 标记后回显
	// （executor 的 brief_nonce 是任务 ID，桩无法预知，只能从 prompt 提取）。
	responses := s.stream
	if query, _ := env.Params["query"].(string); strings.Contains(query, "[[JIUWEN_BRIEF_BEGIN:") {
		start := strings.Index(query, "[[JIUWEN_BRIEF_BEGIN:")
		end := strings.Index(query[start:], "]]")
		beginMarker := query[start : start+end+2]
		nonce := beginMarker[len("[[JIUWEN_BRIEF_BEGIN:") : len(beginMarker)-2]
		endMarker := "[[JIUWEN_BRIEF_END:" + nonce + "]]"
		responses = []*schema.AgentResponse{
			{OK: true, Payload: map[string]any{"event_type": "chat.delta", "content": "答案正文"}},
			{OK: true, Payload: map[string]any{"event_type": "chat.final",
				"content": "答案正文\n" + beginMarker + "\n口头回执\n" + endMarker}},
		}
	}
	ch := make(chan *schema.AgentResponse, len(responses)+1)
	for _, resp := range responses {
		ch <- resp
	}
	close(ch)
	return ch
}

// recordingChannel 集成桩：记录 RPC 响应与事件。
type recordingChannel struct {
	methods   map[string]duplex.DuplexMethodHandler
	responses []*duplex.DuplexResponse
	events    []string
	mu        sync.Mutex
}

func (c *recordingChannel) SendResponse(_ context.Context, _ any, _ string, resp *duplex.DuplexResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, resp)
}

func (c *recordingChannel) SendEvent(_ context.Context, _ any, event string, _ map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *recordingChannel) SendEventChecked(_ context.Context, _ any, event string, _ map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
	return nil
}

func (c *recordingChannel) RegisterMethod(method string, handler duplex.DuplexMethodHandler, _, _ bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.methods[method] = handler
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDuplexTaskSuite 运行套件
func TestDuplexTaskSuite(t *testing.T) {
	suite.Run(t, new(DuplexTaskSuite))
}

// ──────────────────────────── 套件方法 ────────────────────────────

// TestSettingsRPCFullChain 配置面板 RPC 全链（对齐 spec §8.1）
func (s *DuplexTaskSuite) TestSettingsRPCFullChain() {
	channel := &recordingChannel{methods: map[string]duplex.DuplexMethodHandler{}}
	envFile := filepath.Join(s.T().TempDir(), ".env")
	s.setEnvFile(envFile)
	plugin := &duplex.VideoDuplexApplicationPlugin{}
	plugin.BindWebChannel(context.Background(), channel)

	// settings.get
	getHandler := channel.methods["video.duplex.settings.get"]
	s.Require().NotNil(getHandler)
	resp := getHandler(context.Background(), nil, "r1", nil, "")
	s.Require().True(resp.OK)
	values := resp.Payload["values"].(map[string]string)
	s.Require().Equal("match", values["reply_language"])

	// settings.update
	updateHandler := channel.methods["video.duplex.settings.update"]
	s.Require().NotNil(updateHandler)
	resp = updateHandler(context.Background(), nil, "r2", map[string]any{
		// 对齐 Python 契约：VIDEO_LIVE_MODE 由 video_live_provider 推导，须显式传
		"values":        map[string]any{"video_live_provider": "joyai", "joyai_model": " joyai-large ", "reply_language": "zh-CN"},
		"clear_secrets": false,
	}, "")
	s.Require().True(resp.OK)
	data, err := os.ReadFile(envFile)
	s.Require().NoError(err)
	s.Require().Contains(string(data), `JOYAI_MODEL_NAME="joyai-large"`)
	s.Require().Contains(string(data), `VIDEO_LIVE_MODE="joyai"`)

	// settings.test（端点不可达 → CONNECTION_FAILED）
	testHandler := channel.methods["video.duplex.settings.test"]
	s.Require().NotNil(testHandler)
	resp = testHandler(context.Background(), nil, "r3", nil, "")
	s.Require().False(resp.OK)
	s.Require().Equal("CONNECTION_FAILED", resp.Code)
}

// TestTaskLifecycleFullChain 工具调用→入账→执行→回执全链（对齐 spec §8.2）
func (s *DuplexTaskSuite) TestTaskLifecycleFullChain() {
	channel := &recordingChannel{methods: map[string]duplex.DuplexMethodHandler{}}
	taskPath := filepath.Join(s.T().TempDir(), "tasks.sqlite")
	briefNonce := "nonce-1"
	client := &stubAgentClient{stream: []*schema.AgentResponse{
		{OK: true, Payload: map[string]any{"event_type": "chat.delta", "content": "答案正文"}},
		{OK: true, Payload: map[string]any{
			"event_type": "chat.final",
			"content":    "答案正文\n[[JIUWEN_BRIEF_BEGIN:" + briefNonce + "]]\n口头回执\n[[JIUWEN_BRIEF_END:" + briefNonce + "]]",
		}},
	}}
	plugin := &duplex.VideoDuplexApplicationPlugin{}
	plugin.InjectDependencies(client, nil)
	plugin.BindWebChannel(context.Background(), channel)
	// TaskDBPath 注入：BindWebChannel 用默认账本路径，此处经任务路径覆盖
	manager := plugin.SearchManager()
	s.Require().NotNil(manager)
	manager.SetTaskDBPath(taskPath)

	// video.qwen.tool：委派建任务
	toolHandler := channel.methods["video.qwen.tool"]
	s.Require().NotNil(toolHandler)
	s.Require().NotNil(toolHandler)
	// qwen 非 active（realtime 模式默认 qwen active = true）
	os.Setenv("VIDEO_LIVE_MODE", "realtime")
	resp := toolHandler(context.Background(), nil, "r1", map[string]any{
		"name": duplex.QwenOmniDelegateToolName, "call_id": "c1",
		"search_session_id": "s1",
		"arguments":         `{"task":"写一份关于账本规范的报告"}`,
	}, "")
	s.Require().True(resp.OK, "tool resp: %+v", resp)
	job := resp.Payload["search_job"].(map[string]any)
	jobID := job["id"].(string)

	// 等任务完成
	s.waitForStatus(taskPath, jobID, "completed", 10)
	store := s.openStore(taskPath)
	task, err := store.Read(jobID, nil, nil)
	s.Require().NoError(err)
	s.Require().Equal("completed", task["status"])
	result := task["result"].(map[string]any)
	s.Require().Equal("答案正文", result["answer"])
	s.Require().Equal("core_agent", result["realtime_brief"].(map[string]any)["source"])

	// video.search.status 投影
	statusHandler := channel.methods["video.search.status"]
	resp = statusHandler(context.Background(), nil, "r2", map[string]any{
		"job_id": jobID, "search_session_id": "s1"}, "")
	s.Require().True(resp.OK)
	s.Require().Equal("completed", resp.Payload["status"])

	// 事件推送已发生
	s.Require().NotEmpty(channel.events)
}

// TestTaskResumeAfterRestart 任务账本持久化：重启恢复（对齐 spec §8.3）
func (s *DuplexTaskSuite) TestTaskResumeAfterRestart() {
	taskPath := filepath.Join(s.T().TempDir(), "tasks.sqlite")
	store := s.openStore(taskPath)
	seed := map[string]any{
		"id": "rt1", "owner": "u", "session": "s", "status": "running",
		"sequence": 1, "instruction": "中断的任务", "request": map[string]any{},
		"changes": []any{}, "progress": []any{}, "position": 1,
		"request_id": "task-seed",
	}
	s.Require().NoError(store.Transaction(func(db *sql.Tx) error { return tasks.TxPut(db, seed) }))

	// 新服务启动 → 所有权丢失重置
	gate := make(chan struct{})
	executor := newStubExecutor(store)
	executor.runGates = []chan struct{}{gate}
	svc, err := tasks.NewTaskService(store, executor, nil, 2, 2*time.Second)
	s.Require().NoError(err)
	s.Require().NoError(svc.Start())
	task, err := store.Read("rt1", nil, nil)
	s.Require().NoError(err)
	s.Require().Equal("unknown", task["status"])
	s.Require().Equal("Execution ownership lost; tools were not replayed", task["error"])
	close(gate)
	s.Require().NoError(svc.Close())
}

// TestInteractionSuspendAndAnswer 交互挂起→回答→恢复（对齐 spec §8.4）
func (s *DuplexTaskSuite) TestInteractionSuspendAndAnswer() {
	taskPath := filepath.Join(s.T().TempDir(), "tasks.sqlite")
	store := s.openStore(taskPath)
	svc, err := tasks.NewTaskService(store, newStubExecutor(store), nil, 2, 2*time.Second)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = svc.Close() })
	s.Require().NoError(svc.Start())
	task, err := svc.Submit("u", "s", "i1", "任务", nil)
	s.Require().NoError(err)
	// 手工进入等待用户
	_, err = store.Update(task["id"].(string), func(current map[string]any) {
		current["status"] = "waiting_user"
		current["interaction"] = map[string]any{
			"id": "i-1", "source": "ask_user", "state": "pending", "request_id": "rq1",
			"questions": []any{map[string]any{"question": "预算多少？"}},
		}
	})
	s.Require().NoError(err)
	receipt, err := svc.Answer(context.Background(), "u", "s", task["id"].(string), "i2", "i-1",
		[]any{map[string]any{"question": "预算多少？", "answer": "5000元"}}, false)
	s.Require().NoError(err)
	s.Require().Equal("accepted", receipt["state"])
	current, err := store.Read(task["id"].(string), nil, nil)
	s.Require().NoError(err)
	s.Require().Equal("running", current["status"])
}

// TestTelemetryJSONLConcurrent 遥测 JSONL 并发写（对齐 spec §8.5）
func (s *DuplexTaskSuite) TestTelemetryJSONLConcurrent() {
	path := filepath.Join(s.T().TempDir(), "telemetry.jsonl")
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			duplex.AppendJSONL(path, map[string]any{"n": n, "event": "realtime_speech_start"})
		}(i)
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	s.Require().Len(lines, 30)
	for _, line := range lines {
		var event map[string]any
		s.Require().NoError(json.Unmarshal([]byte(line), &event))
	}
}

// ──────────────────────────── 套件辅助 ────────────────────────────

// setEnvFile 注入 .env 路径（经 settings 包测试口——集成层用环境隔离）
func (s *DuplexTaskSuite) setEnvFile(path string) {
	// duplex.envFileOverride 为包内变量，集成层经 SetEnvFileOverride 导出口设置
	duplex.SetEnvFileOverride(path)
}

// openStore 打开账本
func (s *DuplexTaskSuite) openStore(path string) *tasks.TaskStore {
	store, err := tasks.NewTaskStore(path)
	s.Require().NoError(err)
	return store
}

// waitForStatus 轮询任务状态
func (s *DuplexTaskSuite) waitForStatus(path, taskID, status string, seconds int) {
	store := s.openStore(path)
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		task, err := store.Read(taskID, nil, nil)
		if err == nil && task["status"] == status {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.T().Fatalf("任务 %s 未在 %ds 内到达 %s", taskID, seconds, status)
}

// stubExecutorWithStore 集成桩（复用 tasks 测试桩形态）
type stubExecutorForIntegration struct {
	store    *tasks.TaskStore
	runGates []chan struct{}
	runs     int
	mu       sync.Mutex
}

func newStubExecutor(store *tasks.TaskStore) *stubExecutorForIntegration {
	return &stubExecutorForIntegration{store: store}
}

func (e *stubExecutorForIntegration) Run(ctx context.Context, task map[string]any, progress func(map[string]any) error) (map[string]any, error) {
	e.mu.Lock()
	index := e.runs
	e.runs++
	var gate chan struct{}
	if index < len(e.runGates) {
		gate = e.runGates[index]
	}
	e.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return map[string]any{"answer": "done"}, nil
}

func (e *stubExecutorForIntegration) Cancel(_ context.Context, task map[string]any) error {
	_, _ = e.store.Update(task["id"].(string), func(current map[string]any) {
		current["execution_cancelled"] = true
		current["execution_settled"] = true
		current["output_closed"] = true
	})
	return nil
}

func (e *stubExecutorForIntegration) Answer(_ context.Context, task map[string]any) error {
	return nil
}

func (e *stubExecutorForIntegration) Close() error { return nil }

// TestConversationAppendRealHistory 会话历史真实链：video.conversation.append →
// 真实 session 包 AppendHistoryRecord 落盘（对齐 video_live.py:538-676 _append_conversation
// 与 session_history.append_history_record 的跨模块边界）。
func (s *DuplexTaskSuite) TestConversationAppendRealHistory() {
	tmpDir := s.T().TempDir()
	s.T().Setenv("UAPCLAW_DATA_DIR", tmpDir)
	path.ResetCache()

	// 注入真实会话历史桥（网关装配层在生产中的等价接线）
	duplex.SetConversationHistoryBridge(&duplex.ConversationHistoryBridge{
		AppendHistoryRecord: func(sessionID, requestID, channelID, role, content string,
			timestamp float64, eventType string, extra map[string]any, mode string) {
			sessionpkg.AppendHistoryRecord(sessionID, requestID, channelID, role, content,
				timestamp, eventType, extra, nil, mode)
		},
		GetSessionMetadata: func(sessionID string, _ bool) map[string]any {
			return sessionpkg.GetSessionMetadata(sessionID)
		},
		UpdateSessionMetadata: func(sessionID, userContent string) {
			sessionpkg.UpdateSessionMetadata(sessionpkg.SessionMetadataUpdate{
				SessionID: sessionID, UserContent: &userContent,
			})
		},
	})
	s.T().Cleanup(func() { duplex.SetConversationHistoryBridge(nil) })

	channel := &recordingChannel{methods: map[string]duplex.DuplexMethodHandler{}}
	plugin := &duplex.VideoDuplexApplicationPlugin{}
	plugin.BindWebChannel(context.Background(), channel)
	handler := channel.methods["video.conversation.append"]
	s.Require().NotNil(handler)

	// 合法 user 事件
	resp := handler(context.Background(), nil, "r1", map[string]any{
		"session_id": "duplex-hist-1", "event_id": "ev-1", "kind": "user",
		"content": "帮我看看这帧画面", "timestamp": 1700000000.0,
	}, "")
	s.Require().True(resp.OK, "append resp: %+v", resp)
	s.Require().Equal(true, resp.Payload["persisted"])

	// 非法 event_id → INVALID_EVENT_ID
	resp = handler(context.Background(), nil, "r2", map[string]any{
		"session_id": "duplex-hist-1", "event_id": "", "kind": "user", "content": "x"}, "")
	s.Require().False(resp.OK)
	s.Require().Equal("INVALID_EVENT_ID", resp.Code)

	// 等待异步写入完成并读真实历史文件
	deadline := time.Now().Add(5 * time.Second)
	var records []map[string]any
	var err error
	for time.Now().Before(deadline) {
		records, err = sessionpkg.ReadHistoryRecords("duplex-hist-1")
		if err == nil && len(records) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.Require().NoError(err)
	s.Require().NotEmpty(records)
	s.Require().Equal("video-duplex-ev-1:user", records[0]["id"])
	s.Require().Equal("video_duplex", records[0]["channel_id"])
	s.Require().Equal("帮我看看这帧画面", records[0]["content"])
}
