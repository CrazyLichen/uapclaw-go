package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	stream "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// fakeSessionAgent 测试用 BaseAgent——记录 Invoke 调用并返回预置结果
type fakeSessionAgent struct {
	invokeErr   error
	invokeDelay time.Duration
	mu          sync.Mutex
	invocations []map[string]any
}

var _ agentinterfaces.BaseAgent = (*fakeSessionAgent)(nil)

func (f *fakeSessionAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeSessionAgent) Invoke(ctx context.Context, inputs map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	f.mu.Lock()
	f.invocations = append(f.invocations, inputs)
	f.mu.Unlock()
	if f.invokeDelay > 0 {
		select {
		case <-time.After(f.invokeDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	return map[string]any{"output": "done-" + todoStr(inputs, "query")}, nil
}
func (f *fakeSessionAgent) Stream(_ context.Context, inputs map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema, 2)
	go func() {
		defer close(ch)
		ch <- &stream.OutputSchema{Type: "content_chunk", Payload: map[string]any{"content": "summary of " + todoStr(inputs, "conversation_id")}}
		ch <- &stream.OutputSchema{Type: "answer", Payload: map[string]any{"output": map[string]any{"output": "final answer"}}}
	}()
	return ch, nil
}
func (f *fakeSessionAgent) Card() *agentschema.AgentCard { return nil }
func (f *fakeSessionAgent) Config() agentinterfaces.AgentConfig {
	return nil
}
func (f *fakeSessionAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeSessionAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return nil }
func (f *fakeSessionAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}
func (f *fakeSessionAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeSessionAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeSessionAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// pushRecorder 线程安全的推送记录器（race 检测用）
type pushRecorder struct {
	mu      sync.Mutex
	pushed  []map[string]any
	pushSem chan struct{}
}

func newPushRecorder() *pushRecorder {
	return &pushRecorder{pushSem: make(chan struct{}, 1)}
}

func (p *pushRecorder) record(msg map[string]any) {
	p.mu.Lock()
	p.pushed = append(p.pushed, msg)
	p.mu.Unlock()
	select {
	case p.pushSem <- struct{}{}:
	default:
	}
}

// snapshot 加锁快照
func (p *pushRecorder) snapshot() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]map[string]any, len(p.pushed))
	copy(out, p.pushed)
	return out
}

// newTestToolkit 测试脚手架：注入 fake provider
func newTestToolkit(t *testing.T) (*MultiSessionToolkit, *pushRecorder) {
	t.Helper()
	rec := newPushRecorder()
	SetSessionPushFunc(func(_ context.Context, msg map[string]any) error {
		rec.record(msg)
		return nil
	})
	t.Cleanup(func() { SetSessionPushFunc(nil) })
	return NewMultiSessionToolkit("parent-sess", "web", "req-1", &saconfig.ReActAgentConfig{}), rec
}

// TestNewMultiSessionToolkit_基础 初始化字段
func TestNewMultiSessionToolkit_基础(t *testing.T) {
	tk := NewMultiSessionToolkit("s1", "ch1", "r1", &saconfig.ReActAgentConfig{})
	if tk.sessionID != "s1" || tk.channelID != "ch1" || tk.requestID != "r1" {
		t.Errorf("toolkit fields: %+v", tk)
	}
	if len(tk.GetTools()) != 3 {
		t.Errorf("GetTools len=%d, want 3", len(tk.GetTools()))
	}
	names := []string{tk.GetTools()[0].Card().Name, tk.GetTools()[1].Card().Name, tk.GetTools()[2].Card().Name}
	for i, want := range []string{"session_new", "session_cancel", "session_list"} {
		if names[i] != want {
			t.Errorf("tool[%d] name=%s, want %s", i, names[i], want)
		}
	}
}

// TestSessionTools_文案 session_new/cancel/list 返回文案逐字
func TestSessionTools_文案(t *testing.T) {
	tk, _ := newTestToolkit(t)
	ctx := context.Background()

	// session_list 空态
	res, err := tk.GetTools()[2].Invoke(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !containsDeep(res, "暂无协程") {
		t.Errorf("empty list res=%v", res)
	}

	// session_cancel 未找到
	res2, _ := tk.GetTools()[1].Invoke(ctx, map[string]any{"session_id": "nope"})
	if !containsDeep(res2, "未找到 session_id=nope") {
		t.Errorf("cancel missing res=%v", res2)
	}
}

// containsDeep 深度查找 map 中含子串的字符串值
func containsDeep(m map[string]any, sub string) bool {
	for _, v := range m {
		if s, ok := v.(string); ok && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ──────────────────────────── Task 4：runAndNotify/notify 测试 ────────────────────────────

// TestRunAndNotify_完成推送与汇总 chat.session_result + chat.final 单次
func TestRunAndNotify_完成推送与汇总(t *testing.T) {
	tk, rec := newTestToolkit(t)
	// 注入 fake provider：汇总 agent
	SetSessionAgentProvider(func() agentinterfaces.BaseAgent {
		return &fakeSessionAgent{}
	})
	t.Cleanup(func() { SetSessionAgentProvider(nil) })

	// 直接调 runAndNotify（绕过 CreateNewSessions 的 goroutine 编排）
	// 注意：不手动 append sessions——runAndNotify 内部会 append
	agent := &fakeSessionAgent{}
	tk.runAndNotify(context.Background(), "sub-1", "task one", agent, map[string]any{"conversation_id": "sub-1", "query": "q"})

	pushed := rec.snapshot()
	// 至少两条：session_result + final
	if len(pushed) < 2 {
		t.Fatalf("pushed len=%d, want >=2: %+v", len(pushed), pushed)
	}
	first := pushed[0]
	payload := first["payload"].(map[string]any)
	if payload["event_type"] != "chat.session_result" {
		t.Errorf("first push event_type=%v", payload["event_type"])
	}
	if payload["status"] != "completed" || payload["index"] != 1 || payload["total"] != 1 {
		t.Errorf("session_result payload=%+v", payload)
	}
	if payload["is_parallel"] != true {
		t.Errorf("is_parallel should be true")
	}
	if first["is_complete"] != false || first["request_id"] != "req-1" || first["session_id"] != "parent-sess" {
		t.Errorf("first msg envelope=%+v", first)
	}
	last := pushed[len(pushed)-1]
	lastPayload := last["payload"].(map[string]any)
	if lastPayload["event_type"] != "chat.final" || last["is_complete"] != true {
		t.Errorf("last push=%+v", last)
	}
	if lastPayload["task_id"] != "parent-sess" {
		t.Errorf("final task_id=%v", lastPayload["task_id"])
	}
}

// TestRunAndNotify_汇总单次触发 sumOnce（两个并发完成）
func TestRunAndNotify_汇总单次触发(t *testing.T) {
	tk, rec := newTestToolkit(t)
	SetSessionAgentProvider(func() agentinterfaces.BaseAgent { return &fakeSessionAgent{} })
	t.Cleanup(func() { SetSessionAgentProvider(nil) })

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		sid := fmt.Sprintf("sub-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk.runAndNotify(context.Background(), sid, "d", &fakeSessionAgent{}, map[string]any{"conversation_id": sid, "query": "q"})
		}()
	}
	wg.Wait()

	finals := 0
	for _, m := range rec.snapshot() {
		if p, ok := m["payload"].(map[string]any); ok && p["event_type"] == "chat.final" {
			finals++
		}
	}
	if finals != 1 {
		t.Errorf("chat.final count=%d, want 1 (sumOnce)", finals)
	}
}

// TestRunAndNotify_取消分支 cancelled→error 映射
func TestRunAndNotify_取消分支(t *testing.T) {
	tk, rec := newTestToolkit(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	tk.runAndNotify(ctx, "sub-c", "cancel me", &fakeSessionAgent{invokeDelay: 5 * time.Second}, map[string]any{"conversation_id": "sub-c", "query": "q"})

	found := false
	for _, m := range rec.snapshot() {
		if p, ok := m["payload"].(map[string]any); ok && p["event_type"] == "chat.session_result" {
			found = true
			if p["status"] != "error" {
				t.Errorf("cancelled should map to error, got %v", p["status"])
			}
			if p["result"] != "任务已取消" {
				t.Errorf("cancelled result=%v", p["result"])
			}
		}
	}
	if !found {
		t.Fatal("no session_result pushed for cancelled task")
	}
}

// TestRunAndNotify_后台不随父ctx死 偏差#7：父 ctx 取消后子任务照常完成
func TestRunAndNotify_后台不随父ctx死(t *testing.T) {
	tk, rec := newTestToolkit(t)
	parentCtx, parentCancel := context.WithCancel(context.Background())

	// 模拟 CreateNewSessions 的派生方式
	taskCtx, taskCancel := context.WithCancel(context.WithoutCancel(parentCtx))
	_ = taskCancel
	go tk.runAndNotify(taskCtx, "sub-bg", "bg", &fakeSessionAgent{invokeDelay: 100 * time.Millisecond}, map[string]any{"conversation_id": "sub-bg", "query": "q"})
	parentCancel() // 父请求结束

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, m := range rec.snapshot() {
			if p, ok := m["payload"].(map[string]any); ok && p["event_type"] == "chat.session_result" {
				found = true
			}
		}
		if found {
			return // 子任务在父 ctx 取消后仍完成推送 ✅
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("background task killed by parent ctx cancel (违反偏差#7)")
}

// TestRunAndNotify_错误分支
func TestRunAndNotify_错误分支(t *testing.T) {
	tk, rec := newTestToolkit(t)
	SetSessionAgentProvider(func() agentinterfaces.BaseAgent { return &fakeSessionAgent{} })
	t.Cleanup(func() { SetSessionAgentProvider(nil) })

	tk.runAndNotify(context.Background(), "sub-e", "boom", &fakeSessionAgent{invokeErr: errors.New("llm down")}, map[string]any{"conversation_id": "sub-e", "query": "q"})
	found := false
	for _, m := range rec.snapshot() {
		if p, ok := m["payload"].(map[string]any); ok && p["event_type"] == "chat.session_result" {
			found = true
			if p["status"] != "error" || p["result"] != "llm down" {
				t.Errorf("error branch payload=%+v", p)
			}
		}
	}
	if !found {
		t.Fatal("no session_result pushed for error task")
	}
	// ERROR 也算 done：触发 final（agentProvider 已注入）
	hasFinal := false
	for _, m := range rec.snapshot() {
		if p, ok := m["payload"].(map[string]any); ok && p["event_type"] == "chat.final" {
			hasFinal = true
		}
	}
	if !hasFinal {
		t.Errorf("error status should also trigger summary final")
	}
}

// TestNotify_推送函数未注入降级
func TestNotify_推送函数未注入(t *testing.T) {
	SetSessionPushFunc(nil)
	tk := NewMultiSessionToolkit("s", "c", "r", &saconfig.ReActAgentConfig{})
	// 不应 panic
	tk.notify(context.Background(), "sub-x", SessionStatusCompleted, "ok", "")
}

// ──────────────────────────── 覆盖率补全：CreateNewSessions/CancelSession/getSubAgent 等 ────────────────────────────

// TestCreateNewSessions_创建与文案 task_descriptions → goroutine 派生 + 返回文案
func TestCreateNewSessions_创建与文案(t *testing.T) {
	tk, rec := newTestToolkit(t)
	res, err := tk.CreateNewSessions(context.Background(), map[string]any{
		"task_descriptions": []any{"task A", "task B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := res["content"].(string)
	if !strings.Contains(content, "已创建 2 个协程") {
		t.Errorf("create res=%v", res)
	}
	// 等待两个后台协程完成推送（getSubAgent 走真实 ReActAgent，无 LLM 配置时
	// 走 error 降级分支——同样推送 session_result，对齐 Python 异常路径）
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		results := 0
		for _, m := range rec.snapshot() {
			if p, ok := m["payload"].(map[string]any); ok && p["event_type"] == "chat.session_result" {
				results++
			}
		}
		if results >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// session_list 非空路径：两条记录均在（completed/error 皆为合法结束态）
	listRes, _ := tk.ListAllSessions(context.Background(), nil)
	listContent, _ := listRes["content"].(string)
	if !strings.Contains(listContent, "task A") || !strings.Contains(listContent, "task B") {
		t.Errorf("list content=%q", listContent)
	}
	// tasks map 已清理（finally 弹出）
	tk.mu.Lock()
	remaining := len(tk.tasks)
	tk.mu.Unlock()
	if remaining != 0 {
		t.Errorf("tasks remaining=%d, want 0（finally 弹出）", remaining)
	}
}

// TestCancelSession_运行中取消 delay agent 创建后取消
func TestCancelSession_运行中取消(t *testing.T) {
	tk, _ := newTestToolkit(t)
	// 混入长时任务：通过 CreateNewSessions 派生（内部走 getSubAgent 真实例，但 agent 调用需 LLM——
	// 改为直接操作 tasks map 模拟运行中会话：cancel 被调用即模拟协程退出并关闭 done）
	done := make(chan struct{})
	tk.mu.Lock()
	tk.tasks["spawn_cancel_me"] = &sessionTaskHandle{cancel: func() { close(done) }, done: done}
	tk.mu.Unlock()

	res, err := tk.CancelSession(context.Background(), map[string]any{"session_id": "spawn_cancel_me"})
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := res["content"].(string); !strings.Contains(content, "已取消 session_id=spawn_cancel_me") {
		t.Errorf("cancel res=%v", res)
	}
}

// TestCancelSession_只等被取消的单个任务 偏差#9：并发多任务时取消一个不被其他任务卡住
func TestCancelSession_只等被取消的单个任务(t *testing.T) {
	tk, _ := newTestToolkit(t)

	// 第一个任务：真实运行 runAndNotify，阻塞在 agent Invoke（长时）
	blockAgent := &fakeSessionAgent{invokeDelay: 10 * time.Second}
	taskCtx1, cancel1 := context.WithCancel(context.Background())
	handle1 := &sessionTaskHandle{cancel: cancel1, done: make(chan struct{})}
	tk.mu.Lock()
	tk.tasks["spawn_slow"] = handle1
	tk.mu.Unlock()
	go func() {
		defer close(handle1.done)
		tk.runAndNotify(taskCtx1, "spawn_slow", "slow task", blockAgent, map[string]any{"conversation_id": "spawn_slow", "query": "q"})
	}()

	// 第二个任务：另一个仍在跑的长时任务（验证取消第一个不等第二个）
	handle2 := &sessionTaskHandle{cancel: func() {}, done: make(chan struct{})}
	tk.mu.Lock()
	tk.tasks["spawn_other"] = handle2
	tk.mu.Unlock()

	// 取消 spawn_slow：若实现退化为等全部任务（旧 wg.Wait bug），此调用会阻塞 10s 直至超时
	start := time.Now()
	done := make(chan struct{})
	var res map[string]any
	var err error
	go func() {
		res, err = tk.CancelSession(context.Background(), map[string]any{"session_id": "spawn_slow"})
		close(done)
	}()
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("cancel blocked %v, want ms-level（旧 wg.Wait bug：被无关任务卡住）", elapsed)
		}
		if err != nil {
			t.Fatal(err)
		}
		if content, _ := res["content"].(string); !strings.Contains(content, "已取消 session_id=spawn_slow") {
			t.Errorf("cancel res=%v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CancelSession blocked >5s——退化为等全部任务（旧 wg.Wait bug 复现）")
	}

	// 收尾：放行并等第一个任务退出，避免泄漏
	cancel1()
	<-handle1.done
	// spawn_other 仍在运行（未被动过）——按设计不清理（toolkit 生命周期放跑）
}

// TestCancelSession_已结束分支 sessions 有记录但 tasks 无 → "已结束"
func TestCancelSession_已结束分支(t *testing.T) {
	tk, _ := newTestToolkit(t)
	tk.mu.Lock()
	tk.sessions = append(tk.sessions, SessionTask{SessionID: "spawn_done", Description: "d", Status: SessionStatusCompleted})
	tk.mu.Unlock()

	res, _ := tk.CancelSession(context.Background(), map[string]any{"session_id": "spawn_done"})
	if content, _ := res["content"].(string); !strings.Contains(content, "session_id=spawn_done 已结束") {
		t.Errorf("done branch res=%v", res)
	}
}

// TestAllTasksDone 空列表/全完成/含 running 三态
func TestAllTasksDone(t *testing.T) {
	tk := NewMultiSessionToolkit("s", "c", "r", &saconfig.ReActAgentConfig{})
	if !tk.allTasksDone() {
		t.Errorf("empty sessions should be all done")
	}
	tk.mu.Lock()
	tk.sessions = append(tk.sessions,
		SessionTask{SessionID: "a", Status: SessionStatusCompleted},
		SessionTask{SessionID: "b", Status: SessionStatusError})
	tk.mu.Unlock()
	if !tk.allTasksDone() {
		t.Errorf("completed+error should be all done")
	}
	tk.mu.Lock()
	tk.sessions = append(tk.sessions, SessionTask{SessionID: "c", Status: SessionStatusCancelled})
	tk.mu.Unlock()
	if tk.allTasksDone() {
		t.Errorf("cancelled should NOT be all done（Python 逐字：不含 CANCELLED）")
	}
}

// TestToStringSlice 宽容转换
func TestToStringSlice(t *testing.T) {
	if got := toStringSlice([]string{"a"}); len(got) != 1 || got[0] != "a" {
		t.Errorf("[]string case=%v", got)
	}
	if got := toStringSlice([]any{"a", 1, "b"}); len(got) != 2 {
		t.Errorf("[]any case=%v（非字符串跳过）", got)
	}
	if got := toStringSlice("solo"); len(got) != 1 || got[0] != "solo" {
		t.Errorf("string case=%v", got)
	}
	if got := toStringSlice(42); got != nil {
		t.Errorf("int case=%v", got)
	}
}

// TestTruncate 截断边界
func TestTruncate(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Errorf("short passthrough failed")
	}
	if got := truncate("0123456789A", 10); got != "0123456789..." {
		t.Errorf("truncate=%q", got)
	}
}

// TestRandHex4 长度 8
func TestRandHex4(t *testing.T) {
	if len(randHex4()) != 8 {
		t.Errorf("randHex4 len=%d", len(randHex4()))
	}
}

// TestGetSubAgent_真实构造 ReActAgent + MCP 工具注入
func TestGetSubAgent_真实构造(t *testing.T) {
	tk := NewMultiSessionToolkit("s", "c", "r", &saconfig.ReActAgentConfig{ModelNameVal: "test-model"})
	agent := tk.getSubAgent()
	if agent == nil {
		t.Fatal("getSubAgent returned nil")
	}
	if agent.Card() == nil || agent.Card().Name != "spawn_sub_agent" {
		t.Errorf("agent card=%+v", agent.Card())
	}
	// MCP 工具卡已注入 AbilityManager
	names := map[string]bool{}
	for _, a := range agent.AbilityManager().List() {
		names[a.AbilityName()] = true
	}
	if !names["mcp_fetch_webpage"] || !names["mcp_exec_command"] {
		t.Errorf("mcp tools not injected: %v", names)
	}
}
