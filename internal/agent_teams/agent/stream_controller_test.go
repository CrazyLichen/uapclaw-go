package agent

import (
	"context"
	"errors"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"strings"
	"testing"

	harness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// iiStub 创建用于测试的 *interaction.InteractiveInput 桩实例。
func iiStub(raw string) *interaction.InteractiveInput {
	ii, _ := interaction.NewInteractiveInput(raw)
	return ii
}

// newTestStreamController 创建测试用 StreamController
func newTestStreamController() *StreamController {
	return NewStreamController(
		func() *TeamAgentBlueprint { return nil },
		NewTeamAgentState(),
		&PrivateAgentResources{},
		func(ctx context.Context, status atschema.MemberStatus) error { return nil },
		func(ctx context.Context, status atschema.ExecutionStatus) error { return nil },
	)
}

// newTestStreamControllerWithBlueprint 创建带蓝图的测试 StreamController
func newTestStreamControllerWithBlueprint(memberName string, role atschema.TeamRole) *StreamController {
	bp := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{
			MemberName: memberName,
			Role:       role,
		},
	}
	return NewStreamController(
		func() *TeamAgentBlueprint { return bp },
		NewTeamAgentState(),
		&PrivateAgentResources{},
		func(ctx context.Context, status atschema.MemberStatus) error { return nil },
		func(ctx context.Context, status atschema.ExecutionStatus) error { return nil },
	)
}

// TestNewStreamController_构造函数 测试构造函数和 Option。
func TestNewStreamController_构造函数(t *testing.T) {
	sc := newTestStreamController()
	if sc == nil {
		t.Fatal("NewStreamController 返回 nil")
	}
	if sc.retryAttempt != 0 {
		t.Error("retryAttempt 应为 0")
	}
	if sc.swallowFailedRound {
		t.Error("swallowFailedRound 应为 false")
	}
	if sc.streamQueue != nil {
		t.Error("streamQueue 应为 nil")
	}
}

// TestNewStreamController_WithOptions 测试 Option 注入。
func TestNewStreamController_WithOptions(t *testing.T) {
	wakeCalled := false
	pollCalled := false
	sc := NewStreamController(
		func() *TeamAgentBlueprint { return nil },
		NewTeamAgentState(),
		&PrivateAgentResources{},
		func(ctx context.Context, status atschema.MemberStatus) error { return nil },
		func(ctx context.Context, status atschema.ExecutionStatus) error { return nil },
		WithWakeMailbox(func(ctx context.Context) error { wakeCalled = true; return nil }),
		WithRequestCompletionPoll(func(ctx context.Context) error { pollCalled = true; return nil }),
	)
	if sc.wakeMailboxCb == nil {
		t.Error("wakeMailboxCb 不应为 nil")
	}
	if sc.requestCompletionPollCb == nil {
		t.Error("requestCompletionPollCb 不应为 nil")
	}
	_ = sc.wakeMailboxCb(context.Background())
	_ = sc.requestCompletionPollCb(context.Background())
	if !wakeCalled {
		t.Error("wakeMailboxCb 未被调用")
	}
	if !pollCalled {
		t.Error("requestCompletionPollCb 未被调用")
	}
}

// TestStreamController_AddChunkObserver 测试观察者添加和移除。
func TestStreamController_AddChunkObserver(t *testing.T) {
	sc := newTestStreamController()
	cb := func(ctx context.Context, chunk streambase.Schema) error {
		return nil
	}
	sc.AddChunkObserver(cb)
	if len(sc.chunkObservers) != 1 {
		t.Errorf("chunkObservers 长度 = %d, 期望 1", len(sc.chunkObservers))
	}
	// 移除
	sc.RemoveChunkObserver(cb)
	if len(sc.chunkObservers) != 0 {
		t.Errorf("chunkObservers 长度 = %d, 期望 0", len(sc.chunkObservers))
	}
}

// TestStreamController_RemoveChunkObserver_幂等 测试移除不存在的观察者。
func TestStreamController_RemoveChunkObserver_幂等(t *testing.T) {
	sc := newTestStreamController()
	cb := func(ctx context.Context, chunk streambase.Schema) error { return nil }
	// 移除不存在的观察者，不应 panic
	sc.RemoveChunkObserver(cb)
	if len(sc.chunkObservers) != 0 {
		t.Errorf("chunkObservers 长度 = %d, 期望 0", len(sc.chunkObservers))
	}
}

// TestStreamController_IsAgentRunning 测试 IsAgentRunning。
func TestStreamController_IsAgentRunning(t *testing.T) {
	sc := newTestStreamController()
	if sc.IsAgentRunning() {
		t.Error("无 harness 时 IsAgentRunning 应为 false")
	}
}

// TestStreamController_HasInFlightRound 测试 HasInFlightRound。
func TestStreamController_HasInFlightRound(t *testing.T) {
	sc := newTestStreamController()
	if sc.HasInFlightRound() {
		t.Error("无 harness 时 HasInFlightRound 应为 false")
	}
}

// TestStreamController_HasPendingInterrupt 测试 HasPendingInterrupt。
func TestStreamController_HasPendingInterrupt(t *testing.T) {
	sc := newTestStreamController()
	// 无 harness 时返回 false
	if sc.HasPendingInterrupt() {
		t.Error("无 harness 时 HasPendingInterrupt 应为 false")
	}
}

// TestStreamController_CloseStream 测试 CloseStream。
func TestStreamController_CloseStream(t *testing.T) {
	sc := newTestStreamController()
	// streamQueue 为 nil 时不应 panic
	sc.CloseStream()

	// 创建带缓冲的 streamQueue
	sc.streamQueue = make(chan streambase.Schema, 1)
	sc.CloseStream()
	// 读取 nil sentinel
	select {
	case chunk := <-sc.streamQueue:
		if chunk != nil {
			t.Error("期望 nil sentinel")
		}
	default:
		t.Error("streamQueue 应有 nil sentinel")
	}
}

// TestStreamController_EmitCompletionAndClose 测试 EmitCompletionAndClose。
func TestStreamController_EmitCompletionAndClose(t *testing.T) {
	memberName := "leader"
	role := atschema.TeamRoleLeader
	bp := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{
			MemberName: memberName,
			Role:       role,
		},
	}
	sc := NewStreamController(
		func() *TeamAgentBlueprint { return bp },
		NewTeamAgentState(),
		&PrivateAgentResources{},
		func(ctx context.Context, status atschema.MemberStatus) error { return nil },
		func(ctx context.Context, status atschema.ExecutionStatus) error { return nil },
	)
	sc.streamQueue = make(chan streambase.Schema, 10)

	sc.EmitCompletionAndClose(5, 3)

	// 第一个应该是 team.completed marker
	select {
	case chunk := <-sc.streamQueue:
		teamChunk, ok := chunk.(*atschema.TeamOutputSchema)
		if !ok {
			t.Fatal("期望 *TeamOutputSchema")
		}
		payload, ok := teamChunk.Payload.(map[string]any)
		if !ok {
			t.Fatal("Payload 应为 map[string]any")
		}
		if payload["event_type"] != "team.completed" {
			t.Errorf("event_type = %v, 期望 team.completed", payload["event_type"])
		}
		if payload["member_count"] != 5 {
			t.Errorf("member_count = %v, 期望 5", payload["member_count"])
		}
		if payload["task_count"] != 3 {
			t.Errorf("task_count = %v, 期望 3", payload["task_count"])
		}
	default:
		t.Error("streamQueue 应有 marker chunk")
	}

	// 第二个应该是 nil sentinel
	select {
	case chunk := <-sc.streamQueue:
		if chunk != nil {
			t.Error("期望 nil sentinel")
		}
	default:
		t.Error("streamQueue 应有 nil sentinel")
	}
}

// TestStreamController_EmitCompletionAndClose_无队列 测试 streamQueue 为 nil 时。
func TestStreamController_EmitCompletionAndClose_无队列(t *testing.T) {
	sc := newTestStreamController()
	// streamQueue 为 nil，不应 panic
	sc.EmitCompletionAndClose(1, 1)
}

// TestDetectTaskFailed 测试 detectTaskFailed。
func TestDetectTaskFailed(t *testing.T) {
	tests := []struct {
		name     string
		chunk    streambase.Schema
		wantFail bool
		wantCode int
		wantText string
	}{
		{
			name:     "非 OutputSchema",
			chunk:    &streambase.TraceSchema{Type: "trace"},
			wantFail: false,
		},
		{
			name:     "Payload 为 nil",
			chunk:    &streambase.OutputSchema{Type: "message", Index: 0},
			wantFail: false,
		},
		{
			name: "非 task_failed 类型",
			chunk: &streambase.OutputSchema{
				Type:    "message",
				Index:   0,
				Payload: map[string]any{"type": "other"},
			},
			wantFail: false,
		},
		{
			name: "task_failed 无错误码",
			chunk: &streambase.OutputSchema{
				Type:  "message",
				Index: 0,
				Payload: map[string]any{
					"type": "task_failed",
					"data": []any{map[string]any{"text": "something went wrong"}},
				},
			},
			wantFail: true,
			wantCode: 0,
			wantText: "something went wrong",
		},
		{
			name: "task_failed 有错误码",
			chunk: &streambase.OutputSchema{
				Type:  "message",
				Index: 0,
				Payload: map[string]any{
					"type": "task_failed",
					"data": []any{map[string]any{"text": "[181001] rate limit exceeded"}},
				},
			},
			wantFail: true,
			wantCode: 181001,
			wantText: "[181001] rate limit exceeded",
		},
		{
			name: "task_failed 无 text",
			chunk: &streambase.OutputSchema{
				Type:  "message",
				Index: 0,
				Payload: map[string]any{
					"type": "task_failed",
					"data": []any{},
				},
			},
			wantFail: true,
			wantCode: 0,
			wantText: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := detectTaskFailed(tt.chunk)
			if !tt.wantFail {
				if err != nil {
					t.Errorf("期望无错误，got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("期望 task_failed 错误，got nil")
			}
			taskErr, ok := err.(*taskFailedError)
			if !ok {
				t.Fatalf("期望 *taskFailedError，got %T", err)
			}
			if taskErr.Code() != tt.wantCode {
				t.Errorf("Code() = %d, 期望 %d", taskErr.Code(), tt.wantCode)
			}
			if taskErr.Text() != tt.wantText {
				t.Errorf("Text() = %v, 期望 %v", taskErr.Text(), tt.wantText)
			}
		})
	}
}

// TestIsRetryableErrorCode 测试 isRetryableErrorCode。
func TestIsRetryableErrorCode(t *testing.T) {
	if !isRetryableErrorCode(181001) {
		t.Error("181001 应可重试")
	}
	if isRetryableErrorCode(999999) {
		t.Error("999999 不应可重试")
	}
}

// TestStreamController_CombinePendingInputs 测试 combinePendingInputs。
func TestStreamController_TagChunk_四种情况(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)

	// 情况 1：TraceSchema 透传
	traceChunk := &streambase.TraceSchema{Type: "trace"}
	result := sc.tagChunk(traceChunk)
	if result != traceChunk {
		t.Error("TraceSchema 应透传")
	}

	// 情况 4：OutputSchema → 升级为 TeamOutputSchema
	outChunk := &streambase.OutputSchema{Type: "message", Index: 0}
	result = sc.tagChunk(outChunk)
	teamChunk, ok := result.(*atschema.TeamOutputSchema)
	if !ok {
		t.Fatal("OutputSchema 应升级为 *TeamOutputSchema")
	}
	if teamChunk.SourceMember == nil || *teamChunk.SourceMember != "coder" {
		t.Errorf("SourceMember = %v, 期望 coder", teamChunk.SourceMember)
	}

	// 情况 2：已标注且匹配 → 透传
	matchedChunk := &atschema.TeamOutputSchema{
		OutputSchema: streambase.OutputSchema{Type: "message", Index: 1},
		SourceMember: stringPtr("coder"),
		Role:         atschemaTeamRolePtr(atschema.TeamRoleTeammate),
	}
	result = sc.tagChunk(matchedChunk)
	if result != matchedChunk {
		t.Error("标签匹配应透传")
	}

	// 情况 3：已标注但标签不匹配 → 浅拷贝更新
	mismatchChunk := &atschema.TeamOutputSchema{
		OutputSchema: streambase.OutputSchema{Type: "message", Index: 2},
		SourceMember: stringPtr("other"),
		Role:         atschemaTeamRolePtr(atschema.TeamRoleLeader),
	}
	result = sc.tagChunk(mismatchChunk)
	updatedChunk, ok := result.(*atschema.TeamOutputSchema)
	if !ok {
		t.Fatal("标签不匹配应返回 *TeamOutputSchema")
	}
	if updatedChunk.SourceMember == nil || *updatedChunk.SourceMember != "coder" {
		t.Errorf("SourceMember = %v, 期望 coder", updatedChunk.SourceMember)
	}
	// 原始 chunk 不应被修改
	if *mismatchChunk.SourceMember != "other" {
		t.Error("原始 chunk 不应被修改")
	}
}

// TestStreamController_TagChunk_无蓝图 测试无蓝图时透传。
func TestStreamController_TagChunk_无蓝图(t *testing.T) {
	sc := newTestStreamController()
	chunk := &streambase.OutputSchema{Type: "message", Index: 0}
	result := sc.tagChunk(chunk)
	if result != chunk {
		t.Error("无蓝图时应透传")
	}
}

// TestStreamController_FanOutToObservers_异常自动移除 测试观察者异常时自动移除。
func TestStreamController_FanOutToObservers_异常自动移除(t *testing.T) {
	sc := newTestStreamController()
	errObs := func(ctx context.Context, chunk streambase.Schema) error {
		return errors.New("observer error")
	}
	sc.AddChunkObserver(errObs)

	chunk := &streambase.OutputSchema{Type: "message", Index: 0}
	sc.fanOutToObservers(context.Background(), chunk)

	if len(sc.chunkObservers) != 0 {
		t.Errorf("异常观察者应被移除，剩余 %d", len(sc.chunkObservers))
	}
}

// TestStreamController_DrainAgentTask 测试 DrainAgentTask。
func TestStreamController_CancelAgent_无飞行轮次(t *testing.T) {
	sc := newTestStreamController()
	// 无飞行中的轮次，应直接返回
	err := sc.CancelAgent(context.Background())
	if err != nil {
		t.Errorf("CancelAgent 返回错误: %v", err)
	}
}

// TestStreamController_CooperativeCancel_超时强制取消 测试协作取消超时后强制取消。
func TestStreamController_memberName(t *testing.T) {
	// 无蓝图
	sc := newTestStreamController()
	if sc.memberName() != "" {
		t.Error("无蓝图时 memberName 应为空")
	}

	// 有蓝图
	sc = newTestStreamControllerWithBlueprint("leader", atschema.TeamRoleLeader)
	if sc.memberName() != "leader" {
		t.Errorf("memberName = %v, 期望 leader", sc.memberName())
	}
}

// TestStreamController_DequeueValidInterruptResume 测试 dequeueValidInterruptResume。
func TestStreamController_WakeMailboxIfInterruptCleared(t *testing.T) {
	sc := newTestStreamController()
	// 无回调
	if err := sc.wakeMailboxIfInterruptCleared(context.Background()); err != nil {
		t.Errorf("无回调时应返回 nil, got %v", err)
	}

	// 有回调
	called := false
	sc.wakeMailboxCb = func(ctx context.Context) error { called = true; return nil }
	_ = sc.wakeMailboxIfInterruptCleared(context.Background())
	if !called {
		t.Error("回调应被调用")
	}
}

// TestStreamController_StartRound_无harness 测试无 harness 时 StartRound 不启动。
func TestStreamController_IsValidInterruptResume(t *testing.T) {
	sc := newTestStreamController()
	// 无 harness 时返回 false
	if sc.IsValidInterruptResume(iiStub("test")) {
		t.Error("无 harness 时应返回 false")
	}
}

// TestStreamController_executeRound_取消 测试 executeRound 在 context 取消时。
func TestStreamController_CooperativeCancel_无飞行轮次(t *testing.T) {
	sc := newTestStreamController()
	err := sc.CooperativeCancel(context.Background())
	if err != nil {
		t.Errorf("CooperativeCancel 返回错误: %v", err)
	}
}

// TestStreamController_startRound_无队列 测试无 streamQueue 时不启动。
func TestStreamController_HasPendingInterrupt_有harness(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &harness.TeamHarness{}
	if sc.HasPendingInterrupt() {
		t.Error("当前 TeamHarness.HasPendingInterrupt 返回 false")
	}
}

// TestStreamController_IsValidInterruptResume_有harness 测试有 harness 时 IsValidInterruptResume。
func TestStreamController_IsValidInterruptResume_有harness(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &harness.TeamHarness{}
	if sc.IsValidInterruptResume(iiStub("test")) {
		t.Error("当前 TeamHarness.IsPendingInterruptResumeValid 返回 false")
	}
}

// TestStreamController_CancelAgent_有飞行轮次 测试有飞行中轮次时 CancelAgent。
// 由于 TeamHarness.RunStreaming 当前为 stub（返回立即关闭的 channel），
// 轮次会瞬间完成，无法可靠地让 HasInFlightRound 返回 true。
// 因此本测试直接设置 roundDone 和 cancelRound 模拟飞行中轮次，
// 验证 CancelAgent 的状态机推进逻辑。
func TestStreamController_fanOutToObservers_正常(t *testing.T) {
	sc := newTestStreamController()
	received := []streambase.Schema{}
	cb := func(ctx context.Context, chunk streambase.Schema) error {
		received = append(received, chunk)
		return nil
	}
	sc.AddChunkObserver(cb)

	chunk := &streambase.OutputSchema{Type: "message", Index: 0}
	sc.fanOutToObservers(context.Background(), chunk)

	if len(received) != 1 {
		t.Fatalf("接收 chunk 数 = %d, 期望 1", len(received))
	}
	if received[0] != chunk {
		t.Error("应接收到相同 chunk")
	}
}

// TestStreamController_fanOutToObservers_panic自动移除 测试观察者 panic 时自动移除。
func TestStreamController_fanOutToObservers_panic自动移除(t *testing.T) {
	sc := newTestStreamController()
	panicObs := func(ctx context.Context, chunk streambase.Schema) error {
		panic("observer panic")
	}
	sc.AddChunkObserver(panicObs)

	chunk := &streambase.OutputSchema{Type: "message", Index: 0}
	sc.fanOutToObservers(context.Background(), chunk)

	if len(sc.chunkObservers) != 0 {
		t.Errorf("panic 观察者应被移除，剩余 %d", len(sc.chunkObservers))
	}
}

// TestStreamController_logRoundPanic 测试 logRoundPanic 恢复。
func TestStreamController_memberName_蓝图nilName(t *testing.T) {
	bp := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{
			MemberName: "",
			Role:       atschema.TeamRoleLeader,
		},
	}
	sc := NewStreamController(
		func() *TeamAgentBlueprint { return bp },
		NewTeamAgentState(),
		&PrivateAgentResources{},
		func(ctx context.Context, status atschema.MemberStatus) error { return nil },
		func(ctx context.Context, status atschema.ExecutionStatus) error { return nil },
	)
	if sc.memberName() != "" {
		t.Error("空 MemberName 时应返回空字符串")
	}
}

// TestStreamController_EmitCompletionAndClose_无蓝图 测试无蓝图时 EmitCompletionAndClose。
func TestStreamController_EmitCompletionAndClose_无蓝图(t *testing.T) {
	sc := newTestStreamController()
	sc.streamQueue = make(chan streambase.Schema, 10)

	sc.EmitCompletionAndClose(2, 1)

	select {
	case chunk := <-sc.streamQueue:
		teamChunk, ok := chunk.(*atschema.TeamOutputSchema)
		if !ok {
			t.Fatal("期望 *TeamOutputSchema")
		}
		if teamChunk.SourceMember != nil {
			t.Error("无蓝图时 SourceMember 应为 nil")
		}
	default:
		t.Error("streamQueue 应有 marker chunk")
	}

	select {
	case chunk := <-sc.streamQueue:
		if chunk != nil {
			t.Error("期望 nil sentinel")
		}
	default:
		t.Error("streamQueue 应有 nil sentinel")
	}
}

// TestStreamController_CloseStream_满队列 测试满队列时不阻塞。
func TestStreamController_CloseStream_满队列(t *testing.T) {
	sc := newTestStreamController()
	sc.streamQueue = make(chan streambase.Schema, 1)
	sc.streamQueue <- &streambase.OutputSchema{Type: "message", Index: 0}
	sc.CloseStream()
}

// TestStreamController_DrainAgentTask_有飞行轮次 测试有飞行中轮次时 DrainAgentTask。
func TestDetectTaskFailed_结构体载荷直读(t *testing.T) {
	mk := func(cop *cschema.ControllerOutputPayload) streambase.Schema {
		return &streambase.OutputSchema{Payload: cop}
	}

	// 非 task_failed 类型 → nil
	if err := detectTaskFailed(mk(&cschema.ControllerOutputPayload{Type: "other"})); err != nil {
		t.Errorf("非 task_failed 不应报错，实际 %v", err)
	}
	// task_failed 且带文本帧 → 返回携带文本的错误
	cop := &cschema.ControllerOutputPayload{
		Type: taskFailedPayloadType,
		Data: []cschema.DataFrame{&cschema.TextDataFrame{Text: "boom"}},
	}
	err := detectTaskFailed(mk(cop))
	if err == nil {
		t.Fatal("task_failed 应返回错误")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("错误应携带文本，实际 %v", err)
	}
	// typed nil 指针 → nil（不 panic）
	var nilCop *cschema.ControllerOutputPayload
	if err := detectTaskFailed(&streambase.OutputSchema{Payload: nilCop}); err != nil {
		t.Errorf("typed nil 载荷应返回 nil，实际 %v", err)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// intPtr 返回 int 指针
func intPtr(v int) *int { return &v }

// stringPtr 返回 string 指针
func stringPtr(v string) *string { return &v }

// atschemaTeamRolePtr 返回 TeamRole 指针
func atschemaTeamRolePtr(v atschema.TeamRole) *atschema.TeamRole { return &v }

// 编译期断言：*TeamOutputSchema 满足 streambase.Schema 接口
var _ streambase.Schema = (*atschema.TeamOutputSchema)(nil)
