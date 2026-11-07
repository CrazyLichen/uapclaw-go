package session

import "testing"

// TestSessionWorkKind_Scheduled 校验 scheduled 属性成员集合。
//
// Python: SessionWorkKind.scheduled —— CHAT_UNARY / CHAT_STREAM / SESSION_MESSAGE
func TestSessionWorkKind_Scheduled(t *testing.T) {
	scheduled := []SessionWorkKind{
		SessionWorkKindChatUnary,
		SessionWorkKindChatStream,
		SessionWorkKindSessionMessage,
	}
	all := allWorkKinds()
	if len(all) != 12 {
		t.Fatalf("work_kind 成员数应为 12，实际 %d", len(all))
	}
	for _, k := range all {
		want := false
		for _, s := range scheduled {
			if k == s {
				want = true
			}
		}
		if got := k.Scheduled(); got != want {
			t.Errorf("%s.Scheduled() = %v, want %v", k, got, want)
		}
	}
}

// TestSessionWorkKind_LatestFirst 校验 latest_first 属性成员集合。
//
// Python: SessionWorkKind.latest_first —— 仅 CHAT_UNARY / CHAT_STREAM
func TestSessionWorkKind_LatestFirst(t *testing.T) {
	latest := []SessionWorkKind{
		SessionWorkKindChatUnary,
		SessionWorkKindChatStream,
	}
	for _, k := range allWorkKinds() {
		want := false
		for _, l := range latest {
			if k == l {
				want = true
			}
		}
		if got := k.LatestFirst(); got != want {
			t.Errorf("%s.LatestFirst() = %v, want %v", k, got, want)
		}
	}
}

// TestSessionExecutionState_Terminal 校验终态集合。
//
// Python: SessionExecutionState.terminal —— SUCCEEDED / FAILED / CANCELLED
func TestSessionExecutionState_Terminal(t *testing.T) {
	cases := map[SessionExecutionState]bool{
		SessionExecutionStateQueued:            false,
		SessionExecutionStateRunning:           false,
		SessionExecutionStateWaitingForControl: false,
		SessionExecutionStateWaitingForInput:   false,
		SessionExecutionStateSucceeded:         true,
		SessionExecutionStateFailed:            true,
		SessionExecutionStateCancelled:         true,
	}
	for state, want := range cases {
		if got := state.Terminal(); got != want {
			t.Errorf("%s.Terminal() = %v, want %v", state, got, want)
		}
	}
}

// TestModel_枚举取值与上游逐字一致 校验线协议字符串。
//
// 用切片而非 map：SessionPersistencePolicy 与 TeamWaiterKind 都有 "persistent"
// 值，作为 map 键会重复。
func TestModel_枚举取值与上游逐字一致(t *testing.T) {
	pairs := []struct {
		// got 实际取值
		got string
		// want 期望取值
		want string
	}{
		{string(SessionWorkKindChatUnary), "chat_unary"},
		{string(SessionWorkKindChatStream), "chat_stream"},
		{string(SessionWorkKindSessionMessage), "session_message"},
		{string(SessionWorkKindGoalStream), "goal_stream"},
		{string(SessionWorkKindGoalControl), "goal_control"},
		{string(SessionWorkKindGoalAttach), "goal_attach"},
		{string(SessionWorkKindControlInput), "control_input"},
		{string(SessionWorkKindSessionInput), "session_input"},
		{string(SessionWorkKindHeartbeat), "heartbeat"},
		{string(SessionWorkKindTeamRequest), "team_request"},
		{string(SessionWorkKindTeamControl), "team_control"},
		{string(SessionWorkKindTeamBackground), "team_background"},
		{string(SessionExecutionStateQueued), "queued"},
		{string(SessionExecutionStateRunning), "running"},
		{string(SessionExecutionStateWaitingForControl), "waiting_for_control"},
		{string(SessionExecutionStateWaitingForInput), "waiting_for_input"},
		{string(SessionExecutionStateSucceeded), "succeeded"},
		{string(SessionExecutionStateFailed), "failed"},
		{string(SessionExecutionStateCancelled), "cancelled"},
		{string(RuntimeSessionStateReady), "ready"},
		{string(RuntimeSessionStateActive), "active"},
		{string(RuntimeSessionStateQuiescing), "quiescing"},
		{string(RuntimeSessionStateClosed), "closed"},
		{string(SessionPersistencePolicyPersistent), "persistent"},
		{string(ExecutionCancelCauseConsumerClosed), "consumer_closed"},
		{string(ExecutionCancelCauseClientDisconnected), "client_disconnected"},
		{string(ExecutionCancelCauseUserPause), "user_pause"},
		{string(ExecutionCancelCauseUserCancel), "user_cancel"},
		{string(ExecutionCancelCauseSessionDeleted), "session_deleted"},
		{string(ExecutionCancelCauseRuntimeClosed), "runtime_closed"},
		{string(TeamWaiterKindNone), "none"},
		{string(TeamWaiterKindPersistent), "persistent"},
		{string(TeamWaiterKindRequest), "request"},
		{string(TeamWaiterKindExclusive), "exclusive"},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Errorf("枚举取值 = %q, want %q", p.got, p.want)
		}
	}
}

// allWorkKinds 返回全部 work_kind 成员（用于集合属性校验）。
func allWorkKinds() []SessionWorkKind {
	return []SessionWorkKind{
		SessionWorkKindChatUnary,
		SessionWorkKindChatStream,
		SessionWorkKindSessionMessage,
		SessionWorkKindGoalStream,
		SessionWorkKindGoalControl,
		SessionWorkKindGoalAttach,
		SessionWorkKindControlInput,
		SessionWorkKindSessionInput,
		SessionWorkKindHeartbeat,
		SessionWorkKindTeamRequest,
		SessionWorkKindTeamControl,
		SessionWorkKindTeamBackground,
	}
}

// TestTeamExecutionDetails_默认值 校验 waiter_kind 默认 none。
func TestTeamExecutionDetails_默认值(t *testing.T) {
	d := TeamExecutionDetails{}
	if d.WaiterKind != "" && d.WaiterKind != TeamWaiterKindNone {
		t.Fatalf("零值 WaiterKind 应等价 none，实际 %q", d.WaiterKind)
	}
	if d.ObservationOnly {
		t.Fatal("ObservationOnly 默认应为 false")
	}
}

// TestSessionExecutionSnapshot_不含运行时字段 校验快照结构体可独立构造。
func TestSessionExecutionSnapshot_不含运行时字段(t *testing.T) {
	snap := SessionExecutionSnapshot{
		ExecutionID: "e1", SessionID: "s1", RequestID: "r1",
		Generation: 3, WorkKind: SessionWorkKindChatStream, ChannelID: "web",
		State: SessionExecutionStateRunning, CreatedAt: 1.5,
	}
	if snap.Generation != 3 || snap.State != SessionExecutionStateRunning {
		t.Fatalf("快照字段赋值失败: %+v", snap)
	}
}

// TestTeamExecutionContext_嵌入执行身份 校验嵌入字段可访问。
func TestTeamExecutionContext_嵌入执行身份(t *testing.T) {
	ctx := TeamExecutionContext{
		ExecutionLifecycleContext: ExecutionLifecycleContext{
			Session:     SessionLifecycleContext{SessionID: "s1", Generation: 2},
			ExecutionID: "e1",
			RequestID:   "r1",
			WorkKind:    SessionWorkKindTeamRequest,
			ChannelID:   "web",
		},
		Details: TeamExecutionDetails{ControlID: "c1", WaiterKind: TeamWaiterKindRequest},
	}
	if ctx.ExecutionID != "e1" || ctx.Details.ControlID != "c1" {
		t.Fatalf("嵌入字段访问失败: %+v", ctx)
	}
}

// TestCloseSessionResult_代次可空 校验 Generation 为指针。
func TestCloseSessionResult_代次可空(t *testing.T) {
	res := CloseSessionResult{SessionID: "s1"}
	if res.Generation != nil {
		t.Fatal("Generation 默认应为 nil（调用方未指定代次）")
	}
	gen := 3
	res.Generation = &gen
	if *res.Generation != 3 {
		t.Fatal("Generation 赋值失败")
	}
}
