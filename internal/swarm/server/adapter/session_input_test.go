package adapter

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// SessionInputTargetErrorPtr 供 errors.As 使用的目标错误二级指针。
func SessionInputTargetErrorPtr() **sessionpkg.SessionInputTargetError {
	return new(*sessionpkg.SessionInputTargetError)
}

// steerRequest 构造补充输入请求。
func steerRequest(query string, extra map[string]any) *schema.AgentRequest {
	params := map[string]any{"input_mode": "steer", "query": query}
	for k, v := range extra {
		params[k] = v
	}
	raw, _ := json.Marshal(params)
	sid := "session-A"
	return &schema.AgentRequest{RequestID: "req-1", SessionID: &sid, Params: raw}
}

// newEnqueueTestAgent 构造带活跃轮与输出租约的 DeepAgent（enqueue 单测脚手架）。
func newEnqueueTestAgent(round bool) *harness.DeepAgent {
	agent := harness.NewDeepAgent(agentschema.NewAgentCard())
	if round {
		agent.SetActiveRoundForTest(hschema.NewUserWorkItem("req-anchor", map[string]any{"query": "q"}, false, true))
		agent.SetOutputStreamForTest(true)
		agent.SetSteeringQueueForTest(make(chan string, 8))
	}
	return agent
}

// TestEnqueueBoundSessionInput_正常入队 校验通过后入队 display 并返回条目。
func TestEnqueueBoundSessionInput_正常入队(t *testing.T) {
	agent := newEnqueueTestAgent(true)
	req := steerRequest("参考这张截图", nil)
	entry, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: agent, BoundRound: agent.ActiveRound(), Request: req, Query: "参考这张截图",
	})
	if err != nil {
		t.Fatalf("不应拒收: %v", err)
	}
	if entry.DisplayContent != "参考这张截图" || entry.RequestID != "req-1" {
		t.Errorf("条目字段不符: %+v", entry)
	}
}

// TestEnqueueBoundSessionInput_轮次变更拒收 目标轮不再是准入捕获的轮。
func TestEnqueueBoundSessionInput_轮次变更拒收(t *testing.T) {
	agent := newEnqueueTestAgent(true)
	bound := agent.ActiveRound()
	// 换新轮（模拟目标轮已切换）
	agent.SetActiveRoundForTest(hschema.NewUserWorkItem("req-next", map[string]any{"query": "q2"}, false, true))
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: agent, BoundRound: bound, Request: steerRequest("q", nil), Query: "q",
	})
	var targetErr *sessionpkg.SessionInputTargetError
	if !errors.As(err, &targetErr) {
		t.Fatalf("应返回 TARGET_CHANGED，实际 %v", err)
	}
	if targetErr.Message != "the targeted execution has ended or changed; supplemental input was not sent" {
		t.Errorf("文案不符: %s", targetErr.Message)
	}
}

// TestEnqueueBoundSessionInput_输出宿主缺失 无消费者时拒收。
func TestEnqueueBoundSessionInput_输出宿主缺失(t *testing.T) {
	agent := newEnqueueTestAgent(true)
	agent.SetOutputStreamForTest(false)
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: agent, BoundRound: agent.ActiveRound(), Request: steerRequest("q", nil), Query: "q",
	})
	if !errors.As(err, SessionInputTargetErrorPtr()) {
		t.Fatalf("应返回 TARGET_CHANGED，实际 %v", err)
	}
}

// TestEnqueueBoundSessionInput_execution复核 账本查无/终态/取消/异会话都拒收。
func TestEnqueueBoundSessionInput_execution复核(t *testing.T) {
	agent := newEnqueueTestAgent(true)
	mk := func(h *sessionpkg.SessionExecutionHandle) func(string) *sessionpkg.SessionExecutionHandle {
		return func(string) *sessionpkg.SessionExecutionHandle { return h }
	}
	running := sessionpkg.SessionExecutionStateRunning
	cases := []struct {
		name string
		h    *sessionpkg.SessionExecutionHandle
	}{
		{"查无", nil},
		{"终态", &sessionpkg.SessionExecutionHandle{SessionID: "session-A", State: sessionpkg.SessionExecutionStateSucceeded}},
		{"排队中", &sessionpkg.SessionExecutionHandle{SessionID: "session-A", State: sessionpkg.SessionExecutionStateQueued}},
		{"取消中", &sessionpkg.SessionExecutionHandle{SessionID: "session-A", State: running, CancellationRequested: true}},
		{"异会话", &sessionpkg.SessionExecutionHandle{SessionID: "session-B", State: running}},
	}
	for _, tc := range cases {
		_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
			Instance: agent, BoundRound: agent.ActiveRound(),
			Request: steerRequest("q", map[string]any{"expected_execution_id": "exec-42"}), Query: "q",
			ExecutionLookup: mk(tc.h),
		})
		if !errors.As(err, SessionInputTargetErrorPtr()) {
			t.Errorf("%s: 应返回 TARGET_CHANGED，实际 %v", tc.name, err)
		}
	}
}

// TestEnqueueBoundSessionInput_execution复核通过 RUNNING 且同会话则入队。
func TestEnqueueBoundSessionInput_execution复核通过(t *testing.T) {
	agent := newEnqueueTestAgent(true)
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: agent, BoundRound: agent.ActiveRound(),
		Request: steerRequest("q", map[string]any{"expected_execution_id": "exec-42"}), Query: "q",
		ExecutionLookup: func(string) *sessionpkg.SessionExecutionHandle {
			return &sessionpkg.SessionExecutionHandle{SessionID: "session-A", State: sessionpkg.SessionExecutionStateRunning}
		},
	})
	if err != nil {
		t.Fatalf("RUNNING 同会话应通过: %v", err)
	}
}

// TestEnqueueBoundSessionInput_入队失败透传 steering 队列缺席时上抛。
func TestEnqueueBoundSessionInput_入队失败透传(t *testing.T) {
	agent := harness.NewDeepAgent(agentschema.NewAgentCard())
	agent.SetActiveRoundForTest(hschema.NewUserWorkItem("req-a", map[string]any{"query": "q"}, false, true))
	agent.SetOutputStreamForTest(true)
	// 无 loop controller → 无 steering 队列 → 入队失败
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: agent, BoundRound: agent.ActiveRound(), Request: steerRequest("q", nil), Query: "q",
	})
	if err == nil || !strings.Contains(err.Error(), "no steering queue") {
		t.Fatalf("应透传入队错误，实际 %v", err)
	}
}

// TestEnqueueBoundSessionInput_跨会话路由五键 跨会话键构造 message_route。
func TestEnqueueBoundSessionInput_跨会话路由五键(t *testing.T) {
	agent := newEnqueueTestAgent(true)
	cross := map[string]any{"chain_id": "c1", "message_id": "m1", "hop_count": 2}
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: agent, BoundRound: agent.ActiveRound(),
		Request: steerRequest("q", map[string]any{"user_id": "u1", schema.CrossSessionInternalKey: cross}), Query: "q",
	})
	if err != nil {
		t.Fatalf("不应拒收: %v", err)
	}
	entry, _ := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: agent, BoundRound: agent.ActiveRound(),
		Request: steerRequest("q", map[string]any{"user_id": "u1", schema.CrossSessionInternalKey: cross}), Query: "q",
	})
	route := entry.MessageRoute
	if route == nil {
		t.Fatal("跨会话键存在时必须有 message_route")
	}
	for _, key := range []string{"session_id", "request_id", "user_id", "chain_id", "parent_message_id", "hop_count"} {
		if _, ok := route[key]; !ok {
			t.Errorf("message_route 缺键 %s: %v", key, route)
		}
	}
	if route["chain_id"] != "c1" || route["parent_message_id"] != "m1" || route["hop_count"] != float64(2) {
		t.Errorf("路由值不符: %v", route)
	}
	if entry.CrossSession["content"] != "q" {
		t.Errorf("cross_session.content 应为 display 文本，实际 %v", entry.CrossSession["content"])
	}
}

// TestClassifyNotAccepted_跨会话键 拒收 queue the message。
func TestClassifyNotAccepted_跨会话键(t *testing.T) {
	err := classifyNotAccepted(map[string]any{schema.CrossSessionInternalKey: map[string]any{"a": 1}}, false)
	var rejected *sessionpkg.SessionInputRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("应 REJECTED，实际 %v", err)
	}
	if rejected.Message != "target execution changed before submission; queue the message" {
		t.Errorf("文案不符: %s", rejected.Message)
	}
}

// TestClassifyNotAccepted_锚定失配 TARGET_CHANGED。
func TestClassifyNotAccepted_锚定失配(t *testing.T) {
	err := classifyNotAccepted(map[string]any{"expected_execution_id": "exec-9"}, false)
	var target *sessionpkg.SessionInputTargetError
	if !errors.As(err, &target) {
		t.Fatalf("应 TARGET_CHANGED，实际 %v", err)
	}
	if target.Message != "the targeted execution has ended; supplemental input was not sent" {
		t.Errorf("文案不符: %s", target.Message)
	}
}

// TestClassifyNotAccepted_输出收尾 finishing 错误。
func TestClassifyNotAccepted_输出收尾(t *testing.T) {
	err := classifyNotAccepted(map[string]any{}, true)
	if err == nil || err.Error() != "session output is finishing; supplemental input was not sent, retry after it settles" {
		t.Fatalf("应 finishing 文案，实际 %v", err)
	}
}

// TestClassifyNotAccepted_idle兜底 全空 → nil。
func TestClassifyNotAccepted_idle兜底(t *testing.T) {
	if err := classifyNotAccepted(map[string]any{}, false); err != nil {
		t.Fatalf("应允许 idle 兜底，实际 %v", err)
	}
}

// TestDisplayContentOf content 优先、回落 query、再回落 inputs。
func TestDisplayContentOf(t *testing.T) {
	if got := displayContentOf(map[string]any{"content": "c", "query": "q"}, nil); got != "c" {
		t.Errorf("content 优先: %s", got)
	}
	if got := displayContentOf(map[string]any{"query": "q"}, nil); got != "q" {
		t.Errorf("回落 query: %s", got)
	}
	if got := displayContentOf(map[string]any{}, map[string]any{"query": "iq"}); got != "iq" {
		t.Errorf("回落 inputs: %s", got)
	}
}

// TestIsInterruptResumePayload answers 非空即 resume。
func TestIsInterruptResumePayload(t *testing.T) {
	if !isInterruptResumePayload(map[string]any{"answers": []any{"a"}}) {
		t.Error("answers 非空应为 resume")
	}
	if isInterruptResumePayload(map[string]any{"answers": []any{}}) {
		t.Error("answers 空不应为 resume")
	}
}

// TestSteerMediaItemsOf 提取与容错。
func TestSteerMediaItemsOf(t *testing.T) {
	items := steerMediaItemsOf(map[string]any{"media_items": []any{
		map[string]any{"type": "image", "path": "p"},
		map[string]any{"type": "image", "path": "q"},
	}})
	if len(items) != 2 {
		t.Fatalf("应提取 2 项，实际 %d", len(items))
	}
	if got := steerMediaItemsOf(map[string]any{"media_items": "bad"}); got != nil {
		t.Errorf("非 list 应 nil，实际 %v", got)
	}
}
