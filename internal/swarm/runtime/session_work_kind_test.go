package runtime

import (
	"encoding/json"
	"testing"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newWorkKindRequest 构造一个带 params 的请求。
func newWorkKindRequest(
	method schema.ReqMethod, sessionID *string, stream bool, params map[string]any,
) *schema.AgentRequest {
	var raw json.RawMessage
	if params != nil {
		raw, _ = json.Marshal(params)
	}
	return &schema.AgentRequest{
		RequestID: "req-1",
		ChannelID: "ch-1",
		SessionID: sessionID,
		ReqMethod: method,
		Params:    raw,
		IsStream:  stream,
	}
}

// TestSessionWorkKind_无会话ID 第一步就返回 nil。
func TestSessionWorkKind_无会话ID(t *testing.T) {
	req := newWorkKindRequest(schema.ReqMethodChatSend, nil, true,
		map[string]any{"mode": "agent.work.normal"})
	if got := SessionWorkKind(req, false); got != nil {
		t.Errorf("无 session_id 应为 nil，实际 %v", *got)
	}
	empty := ""
	req = newWorkKindRequest(schema.ReqMethodChatSend, &empty, true,
		map[string]any{"mode": "agent.work.normal"})
	if got := SessionWorkKind(req, false); got != nil {
		t.Errorf("空 session_id 应为 nil，实际 %v", *got)
	}
}

// TestSessionWorkKind_单agent聊天 流式与非流式。
func TestSessionWorkKind_单agent聊天(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true,
		map[string]any{"mode": "agent.work.normal"})
	got := SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindChatStream {
		t.Errorf("流式单 agent 聊天应为 chat_stream，实际 %v", got)
	}

	req = newWorkKindRequest(schema.ReqMethodChatSend, &sid, false,
		map[string]any{"mode": "agent.work.normal"})
	got = SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindChatUnary {
		t.Errorf("非流式单 agent 聊天应为 chat_unary，实际 %v", got)
	}
}

// TestSessionWorkKind_非单agent模式返回nil code 模式不归 Runtime 管。
func TestSessionWorkKind_非单agent模式返回nil(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodSessionRename, &sid, false,
		map[string]any{"mode": "agent.code.normal"})
	if got := SessionWorkKind(req, false); got != nil {
		t.Errorf("非单 agent 应返回 nil，实际 %v", *got)
	}
}

// TestSessionWorkKind_团队请求 team 模式走团队分支。
func TestSessionWorkKind_团队请求(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true,
		map[string]any{"mode": "team.work.normal"})
	got := SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindTeamRequest {
		t.Errorf("团队聊天应为 team_request，实际 %v", got)
	}
}

// TestSessionWorkKind_团队后台 带 heartbeat / cron 触发。
func TestSessionWorkKind_团队后台(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true, map[string]any{
		"mode":       "team.work.normal",
		"automation": map[string]any{"kind": "heartbeat"},
	})
	got := SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindTeamBackground {
		t.Errorf("团队心跳应为 team_background，实际 %v", got)
	}

	req = newWorkKindRequest(schema.ReqMethodChatSend, &sid, true, map[string]any{
		"mode": "team.work.normal",
		"cron": map[string]any{"id": "c1"},
	})
	got = SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindTeamBackground {
		t.Errorf("团队 cron 应为 team_background，实际 %v", got)
	}
}

// TestSessionWorkKind_团队非chat轮次 团队模式下非 chat 方法返回 nil。
func TestSessionWorkKind_团队非chat轮次(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodSessionRename, &sid, false,
		map[string]any{"mode": "team.work.normal"})
	if got := SessionWorkKind(req, false); got != nil {
		t.Errorf("团队模式下非 chat 方法应为 nil，实际 %v", *got)
	}
}

// TestSessionWorkKind_团队中断续答 团队控制输入。
func TestSessionWorkKind_团队中断续答(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true, map[string]any{
		"mode":       "team.work.normal",
		"source":     "ask_user_interrupt",
		"answers":    []any{"a"},
		"request_id": "r1",
	})
	got := SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindTeamControl {
		t.Errorf("团队中断续答应为 team_control，实际 %v", got)
	}
}

// TestSessionWorkKind_目标命令 COMMAND_GOAL 按 action 分流。
func TestSessionWorkKind_目标命令(t *testing.T) {
	sid := "s1"
	for _, action := range []string{"set", "resume"} {
		req := newWorkKindRequest(schema.ReqMethodCommandGoal, &sid, true,
			map[string]any{"mode": "agent.work.normal", "action": action})
		got := SessionWorkKind(req, false)
		if got == nil || *got != sessionpkg.SessionWorkKindGoalStream {
			t.Errorf("action=%s 应为 goal_stream，实际 %v", action, got)
		}
	}
	for _, action := range []string{"get", "pause", "clear"} {
		req := newWorkKindRequest(schema.ReqMethodCommandGoal, &sid, false,
			map[string]any{"mode": "agent.work.normal", "action": action})
		got := SessionWorkKind(req, false)
		if got == nil || *got != sessionpkg.SessionWorkKindGoalControl {
			t.Errorf("action=%s 应为 goal_control，实际 %v", action, got)
		}
	}
	// 缺 action 时上游默认 "get"
	req := newWorkKindRequest(schema.ReqMethodCommandGoal, &sid, false,
		map[string]any{"mode": "agent.work.normal"})
	if got := SessionWorkKind(req, false); got == nil || *got != sessionpkg.SessionWorkKindGoalControl {
		t.Errorf("缺 action 应默认 goal_control，实际 %v", got)
	}
}

// TestSessionWorkKind_补充输入 steer/follow_up 归为 session_input。
func TestSessionWorkKind_补充输入(t *testing.T) {
	sid := "s1"
	for _, mode := range []string{"steer", "follow_up"} {
		req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true,
			map[string]any{"mode": "agent.work.normal", "input_mode": mode})
		got := SessionWorkKind(req, false)
		if got == nil || *got != sessionpkg.SessionWorkKindSessionInput {
			t.Errorf("input_mode=%s 应为 session_input，实际 %v", mode, got)
		}
	}
}

// TestSessionWorkKind_中断续答 归为 control_input。
func TestSessionWorkKind_中断续答(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true, map[string]any{
		"mode":       "agent.work.normal",
		"source":     "ask_user_interrupt",
		"answers":    []any{"a"},
		"request_id": "r1",
	})
	got := SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindControlInput {
		t.Errorf("中断续答应为 control_input，实际 %v", got)
	}
}

// TestSessionWorkKind_目标挂载 attach_goal 归为 goal_attach。
func TestSessionWorkKind_目标挂载(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true,
		map[string]any{"mode": "agent.work.normal", "attach_goal": true})
	got := SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindGoalAttach {
		t.Errorf("attach_goal 应为 goal_attach，实际 %v", got)
	}
}

// TestSessionWorkKind_后台跨会话消息 background 分支。
func TestSessionWorkKind_后台跨会话消息(t *testing.T) {
	sid := "s1"
	params := map[string]any{
		"mode":                    "agent.work.normal",
		sessionMessageInternalKey: map[string]any{"message_id": "m1"},
	}
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true, params)
	got := SessionWorkKind(req, true)
	if got == nil || *got != sessionpkg.SessionWorkKindSessionMessage {
		t.Errorf("后台跨会话消息应为 session_message，实际 %v", got)
	}
	// 非后台时该载荷不特殊处理，仍走普通聊天
	got = SessionWorkKind(req, false)
	if got == nil || *got != sessionpkg.SessionWorkKindChatStream {
		t.Errorf("非后台时应走 chat_stream，实际 %v", got)
	}
}

// TestSessionWorkKind_后台非chat方法 background 分支非 chat 方法返回 nil。
func TestSessionWorkKind_后台非chat方法(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodSessionRename, &sid, false,
		map[string]any{"mode": "agent.work.normal"})
	if got := SessionWorkKind(req, true); got != nil {
		t.Errorf("后台 + 非 chat 方法应为 nil，实际 %v", *got)
	}
}

// TestSessionWorkKind_非chat轮次返回nil 零碎 RPC 不归 Runtime。
func TestSessionWorkKind_非chat轮次返回nil(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodSessionRename, &sid, false,
		map[string]any{"mode": "agent.work.normal"})
	if got := SessionWorkKind(req, false); got != nil {
		t.Errorf("session.rename 应为 nil，实际 %v", *got)
	}
}

// TestSessionWorkKind_nil请求 nil 请求安全返回 nil。
func TestSessionWorkKind_nil请求(t *testing.T) {
	if got := SessionWorkKind(nil, false); got != nil {
		t.Errorf("nil 请求应为 nil，实际 %v", *got)
	}
}

// TestUsesSessionRuntime 验证上游语义：为真走 ExecuteMessage，为假走 ProcessMessage。
func TestUsesSessionRuntime(t *testing.T) {
	sid := "s1"
	chat := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true,
		map[string]any{"mode": "agent.work.normal"})
	if !UsesSessionRuntime(chat, false) {
		t.Error("聊天请求应归 Runtime")
	}
	rename := newWorkKindRequest(schema.ReqMethodSessionRename, &sid, false,
		map[string]any{"mode": "agent.work.normal"})
	if UsesSessionRuntime(rename, false) {
		t.Error("session.rename 不应归 Runtime")
	}
}

// TestSessionWorkKind_从不产出Heartbeat 确认与上游一致。
func TestSessionWorkKind_从不产出Heartbeat(t *testing.T) {
	sid := "s1"
	methods := []schema.ReqMethod{
		schema.ReqMethodChatSend, schema.ReqMethodCommandGoal,
		schema.ReqMethodSessionRename, schema.ReqMethodChatCancel,
	}
	for _, method := range methods {
		for _, background := range []bool{false, true} {
			req := newWorkKindRequest(method, &sid, true,
				map[string]any{"mode": "agent.work.normal"})
			if got := SessionWorkKind(req, background); got != nil && *got == sessionpkg.SessionWorkKindHeartbeat {
				t.Errorf("method=%s background=%v 不应产出 heartbeat", method, background)
			}
		}
	}
}

// TestParseWorkKindParams 非法 JSON 退化为空 map。
func TestParseWorkKindParams(t *testing.T) {
	req := &schema.AgentRequest{Params: json.RawMessage(`{bad json`)}
	if got := parseWorkKindParams(req); len(got) != 0 {
		t.Errorf("非法 JSON 应返回空 map，实际 %v", got)
	}
	if got := parseWorkKindParams(nil); len(got) != 0 {
		t.Errorf("nil 请求应返回空 map，实际 %v", got)
	}
}

// TestTeamBackgroundTrigger metadata 里的 automation 也算（对齐上游 metadata 回退）。
func TestTeamBackgroundTrigger(t *testing.T) {
	if got := teamBackgroundTrigger(map[string]any{
		"cron": map[string]any{"id": "c1"},
	}); got != "cron" {
		t.Errorf("cron 应识别，实际 %q", got)
	}
	if got := teamBackgroundTrigger(map[string]any{}); got != "" {
		t.Errorf("无触发应为空串，实际 %q", got)
	}
}

// TestRequestTargetsTeam_元数据回退 请求无 mode 时读会话存下的模式。
func TestRequestTargetsTeam_元数据回退(t *testing.T) {
	sid := "s1"
	// 无 mode 参数 → 回退读元数据（元数据缺失时告警并保持空 mode）。
	// 空 mode 经 canonicalize_mode_text 归为 "agent"，单 agent 族 → chat_stream。
	// Python: canonicalize_mode_text("") -> "agent"
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true, nil)
	if got := SessionWorkKind(req, false); got == nil ||
		*got != sessionpkg.SessionWorkKindChatStream {
		t.Errorf("无 mode 时应回落单 agent 聊天 chat_stream，实际 %v", got)
	}

	// work_mode 参与解析：裸 agent + code work_mode → code.normal，
	// 再 canonicalize 为 agent.code.normal，属单 agent 族（Python 同）。
	sid2 := "s2"
	req = newWorkKindRequest(schema.ReqMethodChatSend, &sid2, true,
		map[string]any{"mode": "agent", "work_mode": "code"})
	if got := SessionWorkKind(req, false); got == nil ||
		*got != sessionpkg.SessionWorkKindChatStream {
		t.Errorf("agent+code work_mode 应落 agent.code.normal 单 agent 族，实际 %v", got)
	}

	// 真正的非单 agent 族：team 之外的 code profile 之外的分支不存在；
	// 用 stateless 之外的方式验证门卫——team.plan 属团队，走步骤 1。
	// 这里验证 code.team 的四段 canonical 不被判为单 agent。
	req = newWorkKindRequest(schema.ReqMethodChatSend, &sid2, true,
		map[string]any{"mode": "code.team"})
	if got := SessionWorkKind(req, false); got == nil ||
		*got != sessionpkg.SessionWorkKindTeamRequest {
		t.Errorf("code.team 应判为团队请求，实际 %v", got)
	}
}

// TestRequestTargetsTeam_直接判定 有 mode 时不读元数据。
func TestRequestTargetsTeam_直接判定(t *testing.T) {
	sid := "s1"
	req := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true,
		map[string]any{"mode": "team"})
	_, _, canonical := ResolveAgentRequestMode("team", "")
	if !schema.IsTeamMode(canonical) {
		t.Fatalf("team 应判为团队模式，实际 %q", canonical)
	}
	if got := SessionWorkKind(req, false); got == nil ||
		*got != sessionpkg.SessionWorkKindTeamRequest {
		t.Errorf("mode=team 应为 team_request，实际 %v", got)
	}
}

// TestUsesSessionRuntime_团队为真 上游语义：团队执行同样由 Runtime 协调器持有。
// 与历史实现（team 返回 false，判「是否占用会话流槽」）相反，此处固化上游定义。
func TestUsesSessionRuntime_团队为真(t *testing.T) {
	sid := "s1"
	team := newWorkKindRequest(schema.ReqMethodChatSend, &sid, true,
		map[string]any{"mode": "team.work.normal"})
	if !UsesSessionRuntime(team, false) {
		t.Error("团队请求应由 Runtime 持有调度（上游语义为 true）")
	}
	goal := newWorkKindRequest(schema.ReqMethodCommandGoal, &sid, true,
		map[string]any{"mode": "agent.work.normal", "action": "set"})
	if !UsesSessionRuntime(goal, false) {
		t.Error("goal 请求应由 Runtime 持有调度")
	}
}
