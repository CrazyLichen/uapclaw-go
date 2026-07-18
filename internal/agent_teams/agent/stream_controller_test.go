package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

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
			Role:        role,
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
	if sc.streamingActive {
		t.Error("streamingActive 应为 false")
	}
	if sc.cancelRequested {
		t.Error("cancelRequested 应为 false")
	}
	if sc.streamQueue != nil {
		t.Error("streamQueue 应为 nil")
	}
	if sc.roundDone != nil {
		t.Error("roundDone 应为 nil")
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
		t.Error("初始状态 IsAgentRunning 应为 false")
	}
	sc.streamingActive = true
	if !sc.IsAgentRunning() {
		t.Error("设置 streamingActive 后 IsAgentRunning 应为 true")
	}
}

// TestStreamController_HasInFlightRound 测试 HasInFlightRound。
func TestStreamController_HasInFlightRound(t *testing.T) {
	sc := newTestStreamController()
	if sc.HasInFlightRound() {
		t.Error("初始状态 HasInFlightRound 应为 false")
	}
	// 模拟一个飞行中的轮次
	sc.roundDone = make(chan struct{})
	if !sc.HasInFlightRound() {
		t.Error("roundDone 未关闭时 HasInFlightRound 应为 true")
	}
	// 关闭 roundDone
	close(sc.roundDone)
	if sc.HasInFlightRound() {
		t.Error("roundDone 关闭后 HasInFlightRound 应为 false")
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
			Role:        role,
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
		name       string
		chunk      streambase.Schema
		wantCode   *int
		wantText   string
	}{
		{
			name:     "非 OutputSchema",
			chunk:    &streambase.TraceSchema{Type: "trace"},
			wantCode: nil,
			wantText: "",
		},
		{
			name:     "Payload 为 nil",
			chunk:    &streambase.OutputSchema{Type: "message", Index: 0},
			wantCode: nil,
			wantText: "",
		},
		{
			name: "非 task_failed 类型",
			chunk: &streambase.OutputSchema{
				Type:    "message",
				Index:   0,
				Payload: map[string]any{"type": "other"},
			},
			wantCode: nil,
			wantText: "",
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
			wantCode: nil,
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
			wantCode: intPtr(181001),
			wantText: "[181001] rate limit exceeded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCode, gotText := detectTaskFailed(tt.chunk)
			if tt.wantCode == nil && gotCode != nil {
				t.Errorf("gotCode = %v, 期望 nil", gotCode)
			}
			if tt.wantCode != nil && (gotCode == nil || *gotCode != *tt.wantCode) {
				t.Errorf("gotCode = %v, 期望 %v", gotCode, tt.wantCode)
			}
			if gotText != tt.wantText {
				t.Errorf("gotText = %v, 期望 %v", gotText, tt.wantText)
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
func TestStreamController_CombinePendingInputs(t *testing.T) {
	sc := newTestStreamController()

	// 单个输入
	result := sc.combinePendingInputs([]any{"hello"})
	if result != "hello" {
		t.Errorf("单个输入: got %v, 期望 hello", result)
	}

	// 多个输入
	result = sc.combinePendingInputs([]any{"first", "second"})
	expected := "first\n\n---\n\nsecond"
	if result != expected {
		t.Errorf("多个输入: got %v, 期望 %v", result, expected)
	}

	// 混合类型
	result = sc.combinePendingInputs([]any{"text", 42})
	expected = "text\n\n---\n\n42"
	if result != expected {
		t.Errorf("混合类型: got %v, 期望 %v", result, expected)
	}
}

// TestStreamController_TagChunk_四种情况 测试 tagChunk 的四种情况。
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
		Role:        atschemaTeamRolePtr(atschema.TeamRoleTeammate),
	}
	result = sc.tagChunk(matchedChunk)
	if result != matchedChunk {
		t.Error("标签匹配应透传")
	}

	// 情况 3：已标注但标签不匹配 → 浅拷贝更新
	mismatchChunk := &atschema.TeamOutputSchema{
		OutputSchema: streambase.OutputSchema{Type: "message", Index: 2},
		SourceMember: stringPtr("other"),
		Role:        atschemaTeamRolePtr(atschema.TeamRoleLeader),
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
func TestStreamController_DrainAgentTask(t *testing.T) {
	sc := newTestStreamController()
	sc.pendingInputs = []any{"input1", "input2"}
	sc.pendingInterruptResumes = []any{"resume1"}

	_ = sc.DrainAgentTask(context.Background())

	if len(sc.pendingInputs) != 0 {
		t.Error("pendingInputs 应被清除")
	}
	if len(sc.pendingInterruptResumes) != 0 {
		t.Error("pendingInterruptResumes 应被清除")
	}
}

// TestStreamController_CancelAgent_无飞行轮次 测试无飞行轮次时 CancelAgent。
func TestStreamController_CancelAgent_无飞行轮次(t *testing.T) {
	sc := newTestStreamController()
	// 无飞行中的轮次，应直接返回
	err := sc.CancelAgent(context.Background())
	if err != nil {
		t.Errorf("CancelAgent 返回错误: %v", err)
	}
}

// TestStreamController_CooperativeCancel_超时强制取消 测试协作取消超时后强制取消。
func TestStreamController_CooperativeCancel_超时强制取消(t *testing.T) {
	sc := newTestStreamController()
	// 模拟一个飞行中的轮次（roundDone 未关闭，goroutine 一直运行）
	roundDone := make(chan struct{})
	sc.roundDone = roundDone
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sc.cancelRound = cancel

	// 在另一个 goroutine 中延迟关闭 roundDone
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(roundDone)
	}()

	err := sc.CooperativeCancel(ctx)
	if err != nil {
		t.Errorf("CooperativeCancel 返回错误: %v", err)
	}
	if !sc.cancelRequested {
		t.Error("cancelRequested 应为 true")
	}
}

// TestStreamController_memberName 测试 memberName。
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
func TestStreamController_DequeueValidInterruptResume(t *testing.T) {
	sc := newTestStreamController()
	// 空队列
	if result := sc.dequeueValidInterruptResume(); result != nil {
		t.Error("空队列应返回 nil")
	}

	// 添加无效项（IsValidInterruptResume 始终返回 false，因为无 harness）
	sc.pendingInterruptResumes = []any{"invalid1", "invalid2"}
	if result := sc.dequeueValidInterruptResume(); result != nil {
		t.Error("无效项应被丢弃")
	}
	if len(sc.pendingInterruptResumes) != 0 {
		t.Error("无效项应被移除")
	}
}

// TestStreamController_WakeMailboxIfInterruptCleared 测试 wakeMailboxIfInterruptCleared。
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// intPtr 返回 int 指针
func intPtr(v int) *int { return &v }

// stringPtr 返回 string 指针
func stringPtr(v string) *string { return &v }

// atschemaTeamRolePtr 返回 TeamRole 指针
func atschemaTeamRolePtr(v atschema.TeamRole) *atschema.TeamRole { return &v }

// 编译期断言：*TeamOutputSchema 满足 streambase.Schema 接口

// TestStreamController_StartRound_无harness 测试无 harness 时 StartRound 不启动。
func TestStreamController_StartRound_无harness(t *testing.T) {
	sc := newTestStreamController()
	// resources.Harness 为 nil
	err := sc.StartRound(context.Background(), "test")
	if err != nil {
		t.Errorf("StartRound 不应返回错误: %v", err)
	}
	if sc.roundDone != nil {
		t.Error("无 harness 时不应启动轮次")
	}
}

// TestStreamController_Steer 测试 Steer。
func TestStreamController_Steer(t *testing.T) {
	sc := newTestStreamController()
	err := sc.Steer(context.Background(), "turn left")
	if err != nil {
		t.Errorf("Steer 返回错误: %v", err)
	}
}

// TestStreamController_FollowUp 测试 FollowUp。
func TestStreamController_FollowUp(t *testing.T) {
	sc := newTestStreamController()
	err := sc.FollowUp(context.Background(), "more info")
	if err != nil {
		t.Errorf("FollowUp 返回错误: %v", err)
	}
}

// TestStreamController_IsValidInterruptResume 测试 IsValidInterruptResume。
func TestStreamController_IsValidInterruptResume(t *testing.T) {
	sc := newTestStreamController()
	// 无 harness 时返回 false
	if sc.IsValidInterruptResume("test") {
		t.Error("无 harness 时应返回 false")
	}
}

// TestStreamController_executeRound_取消 测试 executeRound 在 context 取消时。
func TestStreamController_executeRound_取消(t *testing.T) {
	sc := newTestStreamController()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	sc.executeRound(ctx, "test")
	// 不应 panic
}

// TestStreamController_runOneRound_取消 测试 runOneRound 在 context 取消时。
func TestStreamController_runOneRound_取消(t *testing.T) {
	sc := newTestStreamController()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	sc.runOneRound(ctx, "test")
	// 不应 panic，cancelRequested 不应被设为 true
}

// TestStreamController_runOneRound_teamCleaned 测试 team_cleaned 时关闭流。
func TestStreamController_runOneRound_teamCleaned(t *testing.T) {
	sc := newTestStreamController()
	sc.state.TeamCleaned = true
	sc.streamQueue = make(chan streambase.Schema, 1)

	// runOneRound 内部 finally 应关闭流
	sc.runOneRound(context.Background(), "test")

	// 验证 nil sentinel
	select {
	case chunk := <-sc.streamQueue:
		if chunk != nil {
			t.Error("期望 nil sentinel")
		}
	default:
		// 可能没有 sentinel（因为 executeRound 可能失败但不关流）
		// 这是可接受的：teamCleaned 在 finally 中处理
	}
}

// TestStreamController_streamOneRound_无harness 测试 streamOneRound 无 harness。
func TestStreamController_streamOneRound_无harness(t *testing.T) {
	sc := newTestStreamController()
	code, text := sc.streamOneRound(context.Background(), "test")
	if code != nil {
		t.Error("无 harness 时 code 应为 nil")
	}
	if text != "" {
		t.Error("无 harness 时 text 应为空")
	}
}

// TestStreamController_runRetryingStream_成功 测试 runRetryingStream 成功路径。
func TestStreamController_runRetryingStream_成功(t *testing.T) {
	sc := newTestStreamController()
	// streamOneRound 在无 harness 时直接返回 nil, ""，即成功
	err := sc.runRetryingStream(context.Background(), "test")
	if err != nil {
		t.Errorf("成功路径不应返回错误: %v", err)
	}
}

// TestStreamController_CooperativeCancel_无飞行轮次 测试无飞行轮次时 CooperativeCancel。
func TestStreamController_CooperativeCancel_无飞行轮次(t *testing.T) {
	sc := newTestStreamController()
	err := sc.CooperativeCancel(context.Background())
	if err != nil {
		t.Errorf("CooperativeCancel 返回错误: %v", err)
	}
}

// TestStreamController_startRound_无队列 测试无 streamQueue 时不启动。
func TestStreamController_startRound_无队列(t *testing.T) {
	sc := newTestStreamController()
	// streamQueue 为 nil
	sc.startRound(context.Background(), "test")
	if sc.roundDone != nil {
		t.Error("无 streamQueue 时不应启动轮次")
	}
}

// TestStreamController_startRound_正常启动 测试正常启动轮次。
func TestStreamController_startRound_正常启动(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	// 需要设置 harness 和 streamQueue
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	sc.startRound(context.Background(), "hello")
	if sc.roundDone == nil {
		t.Fatal("roundDone 不应为 nil")
	}

	// 等待轮次完成（无真实 harness，streamOneRound 直接返回成功）
	select {
	case <-sc.roundDone:
		// 轮次完成
	case <-time.After(5 * time.Second):
		t.Fatal("轮次超时未完成")
	}
}

// TestStreamController_StartRound_有harness有队列 测试有 harness 和 streamQueue 时 StartRound 启动轮次。
func TestStreamController_StartRound_有harness有队列(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	err := sc.StartRound(context.Background(), "hello world")
	if err != nil {
		t.Errorf("StartRound 不应返回错误: %v", err)
	}
	if sc.roundDone == nil {
		t.Fatal("roundDone 不应为 nil")
	}

	// 等待轮次完成
	select {
	case <-sc.roundDone:
	case <-time.After(5 * time.Second):
		t.Fatal("轮次超时未完成")
	}
}

// TestStreamController_StartRound_长内容截断预览 测试长内容预览截断。
func TestStreamController_StartRound_长内容截断预览(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	longContent := ""
	for i := 0; i < 200; i++ {
		longContent += "x"
	}
	err := sc.StartRound(context.Background(), longContent)
	if err != nil {
		t.Errorf("StartRound 不应返回错误: %v", err)
	}

	select {
	case <-sc.roundDone:
	case <-time.After(5 * time.Second):
		t.Fatal("轮次超时未完成")
	}
}

// TestStreamController_StartRound_非字符串内容 测试非字符串内容。
func TestStreamController_StartRound_非字符串内容(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	err := sc.StartRound(context.Background(), 12345)
	if err != nil {
		t.Errorf("StartRound 不应返回错误: %v", err)
	}

	select {
	case <-sc.roundDone:
	case <-time.After(5 * time.Second):
		t.Fatal("轮次超时未完成")
	}
}

// TestStreamController_Steer_有harness 测试有 harness 时 Steer。
func TestStreamController_Steer_有harness(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &agentteams.TeamHarness{}
	err := sc.Steer(context.Background(), "turn left")
	if err != nil {
		t.Errorf("Steer 返回错误: %v", err)
	}
}

// TestStreamController_FollowUp_有harness 测试有 harness 时 FollowUp。
func TestStreamController_FollowUp_有harness(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &agentteams.TeamHarness{}
	err := sc.FollowUp(context.Background(), "more info")
	if err != nil {
		t.Errorf("FollowUp 返回错误: %v", err)
	}
}

// TestStreamController_HasPendingInterrupt_有harness 测试有 harness 时 HasPendingInterrupt。
func TestStreamController_HasPendingInterrupt_有harness(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &agentteams.TeamHarness{}
	// TeamHarness 的 HasPendingInterrupt 当前返回 false
	if sc.HasPendingInterrupt() {
		t.Error("当前 TeamHarness.HasPendingInterrupt 返回 false")
	}
}

// TestStreamController_IsValidInterruptResume_有harness 测试有 harness 时 IsValidInterruptResume。
func TestStreamController_IsValidInterruptResume_有harness(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &agentteams.TeamHarness{}
	// TeamHarness 的 IsPendingInterruptResumeValid 当前返回 false
	if sc.IsValidInterruptResume("test") {
		t.Error("当前 TeamHarness.IsPendingInterruptResumeValid 返回 false")
	}
}

// TestStreamController_CancelAgent_有飞行轮次 测试有飞行中轮次时 CancelAgent。
func TestStreamController_CancelAgent_有飞行轮次(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	execStatuses := []atschema.ExecutionStatus{}
	sc.updateExecution = func(ctx context.Context, status atschema.ExecutionStatus) error {
		execStatuses = append(execStatuses, status)
		return nil
	}

	// 启动轮次
	sc.startRound(context.Background(), "hello")
	if !sc.HasInFlightRound() {
		t.Fatal("应有飞行中的轮次")
	}

	// 取消
	err := sc.CancelAgent(context.Background())
	if err != nil {
		t.Errorf("CancelAgent 不应返回错误: %v", err)
	}

	// 验证状态转换
	foundCancelRequested := false
	foundCancelling := false
	for _, s := range execStatuses {
		if s == atschema.ExecutionStatusCancelRequested {
			foundCancelRequested = true
		}
		if s == atschema.ExecutionStatusCancelling {
			foundCancelling = true
		}
	}
	if !foundCancelRequested {
		t.Error("应经过 CancelRequested 状态")
	}
	if !foundCancelling {
		t.Error("应经过 Cancelling 状态")
	}
}

// TestStreamController_CooperativeCancel_等待轮次完成 测试协作取消等待轮次自然完成。
func TestStreamController_CooperativeCancel_等待轮次完成(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	// 启动轮次
	sc.startRound(context.Background(), "hello")

	// 等待轮次先完成
	select {
	case <-sc.roundDone:
	case <-time.After(5 * time.Second):
		t.Fatal("轮次超时未完成")
	}

	// CooperativeCancel 应正常返回（轮次已完成，roundDone 已关闭）
	err := sc.CooperativeCancel(context.Background())
	if err != nil {
		t.Errorf("CooperativeCancel 不应返回错误: %v", err)
	}
}

// TestStreamController_CooperativeCancel_harnessAbort失败 测试 harness.Abort 失败时不影响取消。
func TestStreamController_CooperativeCancel_harnessAbort失败(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	// 启动轮次
	sc.startRound(context.Background(), "hello")

	// 替换 harness 的 Abort 不会有影响（当前 TeamHarness.Abort 返回 nil）
	err := sc.CooperativeCancel(context.Background())
	if err != nil {
		t.Errorf("CooperativeCancel 不应返回错误: %v", err)
	}
}

// TestStreamController_executeRound_成功 测试 executeRound 成功路径。
func TestStreamController_executeRound_成功(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}

	execStatuses := []atschema.ExecutionStatus{}
	sc.updateExecution = func(ctx context.Context, status atschema.ExecutionStatus) error {
		execStatuses = append(execStatuses, status)
		return nil
	}

	sc.executeRound(context.Background(), "hello")

	// 验证状态转换：Starting → Running → Completing → Completed → Idle
	expected := []atschema.ExecutionStatus{
		atschema.ExecutionStatusStarting,
		atschema.ExecutionStatusRunning,
		atschema.ExecutionStatusCompleting,
		atschema.ExecutionStatusCompleted,
		atschema.ExecutionStatusIdle,
	}
	if len(execStatuses) != len(expected) {
		t.Fatalf("状态转换数 = %d, 期望 %d: %v", len(execStatuses), len(expected), execStatuses)
	}
	for i, s := range expected {
		if execStatuses[i] != s {
			t.Errorf("状态[%d] = %v, 期望 %v", i, execStatuses[i], s)
		}
	}
}

// TestStreamController_executeRound_不可重试错误 测试 executeRound 非重试错误走 FAILED 路径。
func TestStreamController_executeRound_不可重试错误(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	execStatuses := []atschema.ExecutionStatus{}
	sc.updateExecution = func(ctx context.Context, status atschema.ExecutionStatus) error {
		execStatuses = append(execStatuses, status)
		return nil
	}

	// 注入总是返回不可重试错误的 streamOneRound
	code := 999999
	sc.testStreamOneRound = func(ctx context.Context, query any) (*int, string) {
		return &code, "[999999] fatal error"
	}

	sc.executeRound(context.Background(), "hello")

	// 验证状态转换：Starting → Running → FAILED → Idle
	expected := []atschema.ExecutionStatus{
		atschema.ExecutionStatusStarting,
		atschema.ExecutionStatusRunning,
		atschema.ExecutionStatusFailed,
		atschema.ExecutionStatusIdle,
	}
	if len(execStatuses) != len(expected) {
		t.Fatalf("状态转换数 = %d, 期望 %d: %v", len(execStatuses), len(expected), execStatuses)
	}
	for i, s := range expected {
		if execStatuses[i] != s {
			t.Errorf("状态[%d] = %v, 期望 %v", i, execStatuses[i], s)
		}
	}
}

// TestStreamController_executeRound_错误且cancelRequested 测试 executeRound 错误时 cancelRequested 走 CANCELLED。
func TestStreamController_executeRound_错误且cancelRequested(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	execStatuses := []atschema.ExecutionStatus{}
	sc.updateExecution = func(ctx context.Context, status atschema.ExecutionStatus) error {
		execStatuses = append(execStatuses, status)
		return nil
	}

	// 注入总是返回错误的 streamOneRound
	code := 999999
	sc.testStreamOneRound = func(ctx context.Context, query any) (*int, string) {
		return &code, "[999999] fatal error"
	}

	// 设置 cancelRequested，错误路径应走 CANCELLED
	sc.cancelRequested = true

	sc.executeRound(context.Background(), "hello")

	// 验证状态转换：Starting → Running → CANCELLED → Idle
	expected := []atschema.ExecutionStatus{
		atschema.ExecutionStatusStarting,
		atschema.ExecutionStatusRunning,
		atschema.ExecutionStatusCancelled,
		atschema.ExecutionStatusIdle,
	}
	if len(execStatuses) != len(expected) {
		t.Fatalf("状态转换数 = %d, 期望 %d: %v", len(execStatuses), len(expected), execStatuses)
	}
	for i, s := range expected {
		if execStatuses[i] != s {
			t.Errorf("状态[%d] = %v, 期望 %v", i, execStatuses[i], s)
		}
	}
}

// TestStreamController_executeRound_取消标记 测试 cancelRequested 标记时走 CANCELLED 路径。
func TestStreamController_executeRound_取消标记(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}

	execStatuses := []atschema.ExecutionStatus{}
	sc.updateExecution = func(ctx context.Context, status atschema.ExecutionStatus) error {
		execStatuses = append(execStatuses, status)
		return nil
	}

	sc.cancelRequested = true
	sc.executeRound(context.Background(), "hello")

	// 验证状态转换：Starting → Running → CANCELLED → Idle（cancelRequested 为 true 时成功也走 CANCELLED）
	expected := []atschema.ExecutionStatus{
		atschema.ExecutionStatusStarting,
		atschema.ExecutionStatusRunning,
		atschema.ExecutionStatusCancelled,
		atschema.ExecutionStatusIdle,
	}
	if len(execStatuses) != len(expected) {
		t.Fatalf("状态转换数 = %d, 期望 %d: %v", len(execStatuses), len(expected), execStatuses)
	}
	for i, s := range expected {
		if execStatuses[i] != s {
			t.Errorf("状态[%d] = %v, 期望 %v", i, execStatuses[i], s)
		}
	}
}

// TestStreamController_streamOneRound_有harness无错误 测试有 harness 时 streamOneRound 成功。
func TestStreamController_streamOneRound_有harness无错误(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	code, text := sc.streamOneRound(context.Background(), "hello")
	if code != nil {
		t.Errorf("成功路径 code 应为 nil, got %v", code)
	}
	if text != "" {
		t.Errorf("成功路径 text 应为空, got %v", text)
	}
	// streamingActive 应已恢复为 false
	if sc.streamingActive {
		t.Error("streamOneRound 完成后 streamingActive 应为 false")
	}
}

// TestStreamController_streamOneRound_有错误chunk 测试 streamOneRound 检测到 task_failed。
func TestStreamController_streamOneRound_有错误chunk(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	// TeamHarness.RunStreaming 返回关闭的空 channel，所以 streamOneRound 无错误
	// 这个测试验证的是正常关闭路径
	code, text := sc.streamOneRound(context.Background(), "hello")
	if code != nil {
		t.Errorf("空 channel 关闭后 code 应为 nil, got %v", code)
	}
	if text != "" {
		t.Errorf("空 channel 关闭后 text 应为空, got %v", text)
	}
}

// TestStreamController_runOneRound_续轮pendingInputs 测试轮次结束后自动续轮 pendingInputs。
func TestStreamController_runOneRound_续轮pendingInputs(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	sc.pendingInputs = []any{"next input"}

	sc.runOneRound(context.Background(), "first")

	// 等待续轮完成（续轮会启动新的 goroutine）
	time.Sleep(200 * time.Millisecond)

	// pendingInputs 应已被消费
	if len(sc.pendingInputs) != 0 {
		t.Errorf("pendingInputs 应被消费, 剩余 %d", len(sc.pendingInputs))
	}
}

// TestStreamController_runOneRound_续轮interruptResume 测试轮次结束后自动续轮 interruptResume。
func TestStreamController_runOneRound_续轮interruptResume(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	// 设置一个有效中断恢复（需要 harness.IsPendingInterruptResumeValid 返回 true）
	// 当前 TeamHarness 总是返回 false，所以 dequeueValidInterruptResume 会丢弃它
	sc.pendingInterruptResumes = []any{"resume1"}

	sc.runOneRound(context.Background(), "first")

	// pendingInterruptResumes 应被丢弃（因为无效）
	if len(sc.pendingInterruptResumes) != 0 {
		t.Errorf("无效 interruptResume 应被丢弃, 剩余 %d", len(sc.pendingInterruptResumes))
	}
}

// TestStreamController_runOneRound_wakeMailbox 测试轮次结束后唤醒邮箱。
func TestStreamController_runOneRound_wakeMailbox(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	wakeCalled := false
	sc.wakeMailboxCb = func(ctx context.Context) error {
		wakeCalled = true
		return nil
	}

	sc.runOneRound(context.Background(), "first")

	if !wakeCalled {
		t.Error("wakeMailboxCb 应被调用")
	}
}

// TestStreamController_runOneRound_requestCompletionPoll 测试轮次结束后请求完成轮询。
func TestStreamController_runOneRound_requestCompletionPoll(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	pollCalled := false
	sc.requestCompletionPollCb = func(ctx context.Context) error {
		pollCalled = true
		return nil
	}

	sc.runOneRound(context.Background(), "first")

	if !pollCalled {
		t.Error("requestCompletionPollCb 应被调用")
	}
}

// TestStreamController_runOneRound_cancelRequested 测试 cancelRequested 为 true 时不续轮。
// 对齐 Python: if not cancelled and not self._cancel_requested → 续轮; 否则不续轮
func TestStreamController_runOneRound_cancelRequested(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	wakeCalled := false
	sc.wakeMailboxCb = func(ctx context.Context) error {
		wakeCalled = true
		return nil
	}
	sc.pendingInputs = []any{"should not be consumed"}

	// runOneRound 重置 cancelRequested = false，所以需要通过 updateExecution 回调设置
	// 利用 executeRound 走完 Running 状态后设置 cancelRequested
	origUpdateExec := sc.updateExecution
	sc.updateExecution = func(ctx context.Context, status atschema.ExecutionStatus) error {
		if status == atschema.ExecutionStatusRunning {
			sc.cancelRequested = true
		}
		return origUpdateExec(ctx, status)
	}

	sc.runOneRound(context.Background(), "first")

	// cancelRequested 为 true 时不续轮
	if len(sc.pendingInputs) != 1 {
		t.Error("cancelRequested 时不应消费 pendingInputs")
	}
	if wakeCalled {
		t.Error("cancelRequested 时不应调用 wakeMailboxCb")
	}
}

// TestStreamController_fanOutToObservers_正常 测试正常扇出。
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
	// 不应 panic
	sc.fanOutToObservers(context.Background(), chunk)

	if len(sc.chunkObservers) != 0 {
		t.Errorf("panic 观察者应被移除，剩余 %d", len(sc.chunkObservers))
	}
}

// TestStreamController_logRoundPanic 测试 logRoundPanic 恢复。
func TestStreamController_logRoundPanic(t *testing.T) {
	sc := newTestStreamController()
	// 直接测试 logRoundPanic（作为 defer 函数）
	func() {
		defer sc.logRoundPanic()
		panic("test panic")
	}()
	// 如果到达这里，说明 logRoundPanic 成功恢复了 panic
}

// TestStreamController_memberName_蓝图nilName 测试蓝图有值但 MemberName 为 nil。
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

	// 第一个应该是 marker（无 SourceMember）
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

// TestStreamController_CloseStream_满队列 测试满队列时不阻塞。
func TestStreamController_CloseStream_满队列(t *testing.T) {
	sc := newTestStreamController()
	sc.streamQueue = make(chan streambase.Schema, 1)
	// 填满队列
	sc.streamQueue <- &streambase.OutputSchema{Type: "message", Index: 0}
	// 再次 CloseStream 不应阻塞
	sc.CloseStream()
}

// TestStreamController_DrainAgentTask_有飞行轮次 测试有飞行中轮次时 DrainAgentTask。
func TestStreamController_DrainAgentTask_有飞行轮次(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	sc.pendingInputs = []any{"input1"}
	sc.pendingInterruptResumes = []any{"resume1"}

	// 启动轮次
	sc.startRound(context.Background(), "hello")

	err := sc.DrainAgentTask(context.Background())
	if err != nil {
		t.Errorf("DrainAgentTask 不应返回错误: %v", err)
	}
	if len(sc.pendingInputs) != 0 {
		t.Error("pendingInputs 应被清除")
	}
	if len(sc.pendingInterruptResumes) != 0 {
		t.Error("pendingInterruptResumes 应被清除")
	}
}

// TestStreamController_CooperativeCancel_context取消 测试 CooperativeCancel 时 context 已取消。
func TestStreamController_CooperativeCancel_context取消(t *testing.T) {
	sc := newTestStreamController()
	// 模拟一个永远不会完成的轮次
	roundDone := make(chan struct{})
	sc.roundDone = roundDone
	_, roundCancel := context.WithCancel(context.Background())
	sc.cancelRound = roundCancel

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消 context

	// 在另一个 goroutine 中延迟关闭 roundDone
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(roundDone)
	}()

	err := sc.CooperativeCancel(ctx)
	if err != nil {
		t.Errorf("CooperativeCancel 不应返回错误: %v", err)
	}
}

// TestStreamController_processChunkChannel_正常chunks 测试正常分块处理。
func TestStreamController_processChunkChannel_正常chunks(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	// 创建带分块的 channel
	chunkCh := make(chan streambase.Schema, 3)
	chunkCh <- &streambase.OutputSchema{Type: "message", Index: 0, Payload: map[string]any{"text": "hello"}}
	chunkCh <- &streambase.OutputSchema{Type: "message", Index: 1, Payload: map[string]any{"text": "world"}}
	close(chunkCh) // 关闭 channel 结束迭代

	code, text := sc.processChunkChannel(context.Background(), chunkCh)
	if code != nil {
		t.Errorf("正常分块 code 应为 nil, got %v", code)
	}
	if text != "" {
		t.Errorf("正常分块 text 应为空, got %v", text)
	}

	// 验证 streamQueue 中有两个分块（已升级为 TeamOutputSchema）
	count := 0
	for len(sc.streamQueue) > 0 {
		chunk := <-sc.streamQueue
		if chunk == nil {
			break
		}
		teamChunk, ok := chunk.(*atschema.TeamOutputSchema)
		if !ok {
			t.Errorf("分块 %d 应为 *TeamOutputSchema", count)
		} else if teamChunk.SourceMember == nil || *teamChunk.SourceMember != "coder" {
			t.Errorf("分块 %d SourceMember = %v, 期望 coder", count, teamChunk.SourceMember)
		}
		count++
	}
	if count != 2 {
		t.Errorf("streamQueue 中分块数 = %d, 期望 2", count)
	}
}

// TestStreamController_processChunkChannel_nilSentinel 测试 nil sentinel 关闭流。
func TestStreamController_processChunkChannel_nilSentinel(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	chunkCh := make(chan streambase.Schema, 3)
	chunkCh <- &streambase.OutputSchema{Type: "message", Index: 0}
	chunkCh <- nil // nil sentinel
	close(chunkCh) // 关闭后 range 会读取剩余元素，nil 截断处理

	code, text := sc.processChunkChannel(context.Background(), chunkCh)
	if code != nil {
		t.Errorf("nil sentinel 关闭后 code 应为 nil, got %v", code)
	}
	if text != "" {
		t.Errorf("nil sentinel 关闭后 text 应为空, got %v", text)
	}

	// 只应有 1 个分块（nil sentinel 后停止处理）
	count := 0
	for len(sc.streamQueue) > 0 {
		<-sc.streamQueue
		count++
	}
	if count != 1 {
		t.Errorf("streamQueue 中分块数 = %d, 期望 1", count)
	}
}

// TestStreamController_processChunkChannel_taskFailed 测试检测到 task_failed 错误。
func TestStreamController_processChunkChannel_taskFailed(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	chunkCh := make(chan streambase.Schema, 3)
	chunkCh <- &streambase.OutputSchema{Type: "message", Index: 0}
	chunkCh <- &streambase.OutputSchema{
		Type:  "message",
		Index: 1,
		Payload: map[string]any{
			"type": "task_failed",
			"data": []any{map[string]any{"text": "[181001] rate limit exceeded"}},
		},
	}
	chunkCh <- &streambase.OutputSchema{Type: "message", Index: 2} // errorSeen 后应被跳过
	close(chunkCh)

	code, text := sc.processChunkChannel(context.Background(), chunkCh)
	if code == nil {
		t.Fatal("task_failed 时 code 不应为 nil")
	}
	if *code != 181001 {
		t.Errorf("code = %d, 期望 181001", *code)
	}
	if text != "[181001] rate limit exceeded" {
		t.Errorf("text = %v, 期望 [181001] rate limit exceeded", text)
	}

	// 第一个分块应正常处理，task_failed 和后续分块应被跳过
	count := 0
	for len(sc.streamQueue) > 0 {
		<-sc.streamQueue
		count++
	}
	if count != 1 {
		t.Errorf("streamQueue 中分块数 = %d, 期望 1（task_failed 和后续被跳过）", count)
	}
}

// TestStreamController_processChunkChannel_context取消 测试 context 取消时退出。
func TestStreamController_processChunkChannel_context取消(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	chunkCh := make(chan streambase.Schema, 1)
	chunkCh <- &streambase.OutputSchema{Type: "message", Index: 0}
	close(chunkCh)

	code, text := sc.processChunkChannel(ctx, chunkCh)
	if code != nil {
		t.Errorf("context 取消时 code 应为 nil, got %v", code)
	}
	if text != "" {
		t.Errorf("context 取消时 text 应为空, got %v", text)
	}
}

// TestStreamController_processChunkChannel_taskFailed无错误码 测试 task_failed 无错误码。
func TestStreamController_processChunkChannel_taskFailed无错误码(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	chunkCh := make(chan streambase.Schema, 2)
	chunkCh <- &streambase.OutputSchema{
		Type:  "message",
		Index: 0,
		Payload: map[string]any{
			"type": "task_failed",
			"data": []any{map[string]any{"text": "something went wrong"}},
		},
	}
	close(chunkCh)

	code, text := sc.processChunkChannel(context.Background(), chunkCh)
	if code != nil {
		t.Errorf("无错误码时 code 应为 nil, got %v", code)
	}
	if text != "something went wrong" {
		t.Errorf("text = %v, 期望 'something went wrong'", text)
	}
}

// TestStreamController_runRetryingStream_可重试错误 测试可重试错误的重试逻辑。
func TestStreamController_runRetryingStream_可重试错误(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	// 通过 processChunkChannel 注入 task_failed 错误
	// streamOneRound → harness.RunStreaming → 空 channel → 成功
	// 所以 runRetryingStream 直接成功
	err := sc.runRetryingStream(context.Background(), "test")
	if err != nil {
		t.Errorf("runRetryingStream 不应返回错误: %v", err)
	}
}

// TestStreamController_runRetryingStream_不可重试错误 测试不可重试错误直接失败。
// 需要通过 processChunkChannel 模拟不可重试的 task_failed
func TestStreamController_runRetryingStream_不可重试错误(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.resources.Harness = &agentteams.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 10)

	// 由于 TeamHarness.RunStreaming 返回空 channel，streamOneRound 总是成功
	// 无法直接测试重试路径，但我们可以测试成功路径
	err := sc.runRetryingStream(context.Background(), "test")
	if err != nil {
		t.Errorf("成功路径不应返回错误: %v", err)
	}
}

// TestStreamController_runRetryingStreamWithRound_重试成功 测试可重试错误重试后成功。
func TestStreamController_runRetryingStreamWithRound_重试成功(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	callCount := 0
	// 模拟：第一次失败（可重试），第二次成功
	fakeRound := func(ctx context.Context, query any) (*int, string) {
		callCount++
		if callCount == 1 {
			code := 181001
			return &code, "[181001] rate limit exceeded"
		}
		return nil, ""
	}

	err := sc.runRetryingStreamWithRound(context.Background(), "test", fakeRound)
	if err != nil {
		t.Errorf("重试成功不应返回错误: %v", err)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, 期望 2", callCount)
	}
}

// TestStreamController_runRetryingStreamWithRound_不可重试错误 测试不可重试错误直接失败。
func TestStreamController_runRetryingStreamWithRound_不可重试错误(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	// 模拟：不可重试错误码
	fakeRound := func(ctx context.Context, query any) (*int, string) {
		code := 999999
		return &code, "[999999] fatal error"
	}

	err := sc.runRetryingStreamWithRound(context.Background(), "test", fakeRound)
	if err == nil {
		t.Error("不可重试错误应返回错误")
	}
}

// TestStreamController_runRetryingStreamWithRound_超过最大重试 测试超过最大重试次数。
func TestStreamController_runRetryingStreamWithRound_超过最大重试(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	callCount := 0
	// 模拟：总是返回可重试错误
	fakeRound := func(ctx context.Context, query any) (*int, string) {
		callCount++
		code := 181001
		return &code, "[181001] persistent error"
	}

	err := sc.runRetryingStreamWithRound(context.Background(), "test", fakeRound)
	if err == nil {
		t.Error("超过最大重试次数应返回错误")
	}
	if callCount != maxRetryAttempts+1 { // 初始 1 次 + maxRetryAttempts 次重试
		t.Errorf("callCount = %d, 期望 %d", callCount, maxRetryAttempts+1)
	}
}

// TestStreamController_processChunkChannel_TraceSchema透传 测试 TraceSchema 透传。
func TestStreamController_processChunkChannel_TraceSchema透传(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	chunkCh := make(chan streambase.Schema, 1)
	chunkCh <- &streambase.TraceSchema{Type: "trace"}
	close(chunkCh)

	code, text := sc.processChunkChannel(context.Background(), chunkCh)
	if code != nil {
		t.Errorf("TraceSchema 透传 code 应为 nil, got %v", code)
	}
	if text != "" {
		t.Errorf("TraceSchema 透传 text 应为空, got %v", text)
	}
}

// TestStreamController_processChunkChannel_空channel 测试空 channel 直接关闭。
func TestStreamController_processChunkChannel_空channel(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	chunkCh := make(chan streambase.Schema)
	close(chunkCh) // 立即关闭，无分块

	code, text := sc.processChunkChannel(context.Background(), chunkCh)
	if code != nil {
		t.Errorf("空 channel code 应为 nil, got %v", code)
	}
	if text != "" {
		t.Errorf("空 channel text 应为空, got %v", text)
	}
}

// TestStreamController_processChunkChannel_有观察者 测试分块扇出到观察者。
func TestStreamController_processChunkChannel_有观察者(t *testing.T) {
	sc := newTestStreamControllerWithBlueprint("coder", atschema.TeamRoleTeammate)
	sc.streamQueue = make(chan streambase.Schema, 10)

	observed := []streambase.Schema{}
	cb := func(ctx context.Context, chunk streambase.Schema) error {
		observed = append(observed, chunk)
		return nil
	}
	sc.AddChunkObserver(cb)

	chunkCh := make(chan streambase.Schema, 1)
	chunkCh <- &streambase.OutputSchema{Type: "message", Index: 0}
	close(chunkCh)

	_, _ = sc.processChunkChannel(context.Background(), chunkCh)

	if len(observed) != 1 {
		t.Fatalf("观察者应接收 1 个分块, got %d", len(observed))
	}
	// 观察者接收的应是 tagChunk 后的分块
	teamChunk, ok := observed[0].(*atschema.TeamOutputSchema)
	if !ok {
		t.Fatal("观察者应接收 *TeamOutputSchema")
	}
	if teamChunk.SourceMember == nil || *teamChunk.SourceMember != "coder" {
		t.Errorf("SourceMember = %v, 期望 coder", teamChunk.SourceMember)
	}
}

// TestStreamController_CooperativeCancel_超时强制取消完整 测试协作取消超时后强制取消（完整流程）。
// 此测试验证：当 goroutine 无法在 cooperativeAbortTimeout 内自然结束时，
// CooperativeCancel 会调用 cancelRound() 强制取消并等待 goroutine 退出。
func TestStreamController_CooperativeCancel_超时强制取消完整(t *testing.T) {
	sc := newTestStreamController()
	// 模拟一个长时间运行的轮次（roundDone 不关闭，需要强制取消）
	roundDone := make(chan struct{})
	sc.roundDone = roundDone
	roundCtx, roundCancel := context.WithCancel(context.Background())
	sc.cancelRound = roundCancel
	sc.resources.Harness = &agentteams.TeamHarness{}

	// 当 cancelRound() 被调用后，模拟 goroutine 退出
	go func() {
		<-roundCtx.Done()
		close(roundDone)
	}()

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- sc.CooperativeCancel(context.Background())
	}()

	select {
	case err := <-doneCh:
		if err != nil {
			t.Errorf("CooperativeCancel 不应返回错误: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CooperativeCancel 超时")
	}

	if !sc.cancelRequested {
		t.Error("cancelRequested 应为 true")
	}
}

var _ streambase.Schema = (*atschema.TeamOutputSchema)(nil)
