package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 交互标识 ────────────────────────────

// TestInteractionIDs_按事件类型取键 chat.ask_user_question 用 request_id。
// Python: interaction.py:39-45
func TestInteractionIDs_按事件类型取键(t *testing.T) {
	event := &RuntimeEvent{Payload: map[string]any{
		"event_type": "chat.ask_user_question",
		"request_id": "  call_1  ",
	}}
	ids := InteractionIDs(event)
	require.Len(t, ids, 1)
	_, ok := ids["call_1"]
	require.True(t, ok, "应去空白")
}

// TestInteractionIDs_激活事件用interaction_id harness.activate_interaction 键不同。
// Python: interaction.py:35
func TestInteractionIDs_激活事件用interaction_id(t *testing.T) {
	event := &RuntimeEvent{Payload: map[string]any{
		"event_type":     "harness.activate_interaction",
		"interaction_id": "act-1",
	}}
	ids := InteractionIDs(event)
	require.Len(t, ids, 1)
	_, ok := ids["act-1"]
	require.True(t, ok)
}

// TestInteractionIDs_不索取返回空 chat.final 不索取回答。
func TestInteractionIDs_不索取返回空(t *testing.T) {
	require.Empty(t, InteractionIDs(&RuntimeEvent{Payload: map[string]any{
		"event_type": "chat.final",
	}}))
	require.Empty(t, InteractionIDs(nil))
	require.Empty(t, InteractionIDs(&RuntimeEvent{Payload: map[string]any{
		"event_type": "chat.ask_user_question",
	}}), "缺标识时为空")
}

// TestAskedInteractionIDs_合并多事件 多事件标识取并集。
// Python: interaction.py:47-49
func TestAskedInteractionIDs_合并多事件(t *testing.T) {
	events := []*RuntimeEvent{
		{Payload: map[string]any{"event_type": "chat.ask_user_question", "request_id": "a"}},
		{Payload: map[string]any{"event_type": "chat.ask_user_question", "request_id": "b"}},
		{Payload: map[string]any{"event_type": "chat.final"}},
	}
	asked := AskedInteractionIDs(events)
	require.Len(t, asked, 2)
}

// ──────────────────────────── answers 校验 ────────────────────────────

// TestValidateAnswerItems_合法 返回深拷贝。
// Python: interaction.py:69-81
func TestValidateAnswerItems_合法(t *testing.T) {
	original := map[string]any{"k": "v", "nested": map[string]any{"x": 1}}
	got, err := ValidateAnswerItems([]any{original})
	require.NoError(t, err)
	require.Len(t, got, 1)

	// 深拷贝：改原对象不影响结果
	original["k"] = "changed"
	require.Equal(t, "v", got[0]["k"])
}

// TestValidateAnswerItems_非法 空/非对象/非序列都报错。
func TestValidateAnswerItems_非法(t *testing.T) {
	_, err := ValidateAnswerItems(nil)
	require.Error(t, err)

	_, err = ValidateAnswerItems([]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")

	_, err = ValidateAnswerItems([]any{"string-item"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "answers[0] must be an object")
}

// ──────────────────────────── 构造校验 ────────────────────────────

// validAnswerInput 构造一份最小合法输入。
func validAnswerInput() InteractionAnswerInput {
	return InteractionAnswerInput{
		RequestID:     "req-1",
		ChannelID:     "web",
		SessionID:     "sess-1",
		InteractionID: "act-1",
		Answers:       []map[string]any{{"answer": "yes"}},
	}
}

// TestNewInteractionAnswerInput_最简 必填齐全可构造。
// Python: interaction.py:132-168
func TestNewInteractionAnswerInput_最简(t *testing.T) {
	got, err := NewInteractionAnswerInput(validAnswerInput())
	require.NoError(t, err)
	require.Equal(t, "req-1", got.RequestID)
	require.Len(t, got.Answers, 1)
}

// TestNewInteractionAnswerInput_必填缺失 四个必填逐一报错。
func TestNewInteractionAnswerInput_必填缺失(t *testing.T) {
	cases := map[string]func(*InteractionAnswerInput){
		"request_id":     func(in *InteractionAnswerInput) { in.RequestID = "  " },
		"channel_id":     func(in *InteractionAnswerInput) { in.ChannelID = "" },
		"session_id":     func(in *InteractionAnswerInput) { in.SessionID = "" },
		"interaction_id": func(in *InteractionAnswerInput) { in.InteractionID = "" },
	}
	for name, mutate := range cases {
		input := validAnswerInput()
		mutate(&input)
		_, err := NewInteractionAnswerInput(input)
		require.Error(t, err, name)
		require.Contains(t, err.Error(), name+" is required", name)
	}
}

// TestNewInteractionAnswerInput_workMode取值 只接受 work / code。
// Python: interaction.py:157-159
func TestNewInteractionAnswerInput_workMode取值(t *testing.T) {
	for _, mode := range []string{"work", "CODE", " Code "} {
		input := validAnswerInput()
		input.WorkMode = mode
		got, err := NewInteractionAnswerInput(input)
		require.NoError(t, err, mode)
		require.Contains(t, []string{"work", "code"}, got.WorkMode)
	}

	input := validAnswerInput()
	input.WorkMode = "plan"
	_, err := NewInteractionAnswerInput(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "work_mode must be 'work' or 'code'")
}

// TestNewInteractionAnswerInput_可信目录去重 trusted_dirs 不得重复。
// Python: interaction.py:83-90
func TestNewInteractionAnswerInput_可信目录去重(t *testing.T) {
	input := validAnswerInput()
	input.TrustedDirs = []string{"/a", "/b"}
	got, err := NewInteractionAnswerInput(input)
	require.NoError(t, err)
	require.Equal(t, []string{"/a", "/b"}, got.TrustedDirs)

	input = validAnswerInput()
	input.TrustedDirs = []string{"/a", "/a"}
	_, err = NewInteractionAnswerInput(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not contain duplicates")

	input = validAnswerInput()
	input.TrustedDirs = []string{"/a", "  "}
	_, err = NewInteractionAnswerInput(input)
	require.Error(t, err, "空目录项应报错")
}

// ──────────────────────────── 续跑判定 ────────────────────────────

// TestResumesInterruptedTurn_中断来源 属于恢复来源集合即为真。
// Python: interaction.py:170-186
func TestResumesInterruptedTurn_中断来源(t *testing.T) {
	for source := range InterruptResumeSources {
		input, err := NewInteractionAnswerInput(InteractionAnswerInput{
			RequestID: "r", ChannelID: "web", SessionID: "s", InteractionID: "i",
			Answers: []map[string]any{{"a": 1}}, Source: source,
		})
		require.NoError(t, err)
		require.True(t, input.ResumesInterruptedTurn(), "source=%s", source)
	}
}

// TestResumesInterruptedTurn_演进审批 三条判定各自成立。
// Python: evolution.py:41-52
func TestResumesInterruptedTurn_演进审批(t *testing.T) {
	cases := []struct {
		name   string
		input  InteractionAnswerInput
		resume bool
	}{
		// call_ 前缀只是"第二道门"：必须先通过 is_evolution_approval_payload
		// （request_id 以 skill_evolve_ 开头、或来源/schema 命中）。
		// 单独 call_abc 不成立——与上游 evolution.py:41-46 一致。
		{"call_前缀但非演进payload", InteractionAnswerInput{InteractionID: "call_abc"}, false},
		{"演进payload且call_前缀", InteractionAnswerInput{
			InteractionID: "call_abc", Source: "skill_evolution_approval"}, true},
		{"evolution_interrupt源", InteractionAnswerInput{
			InteractionID: "x", Source: "evolution_interrupt"}, true},
		{"approval_transport", InteractionAnswerInput{
			InteractionID: "x", EvolutionMeta: map[string]any{
				"event_kind": "approval", "approval_transport": "interrupt",
			}}, true},
		{"普通交互", InteractionAnswerInput{InteractionID: "x", Source: "web"}, false},
		{"空来源", InteractionAnswerInput{InteractionID: "x"}, false},
	}
	for _, tc := range cases {
		input := tc.input
		// 补齐必填项
		input.RequestID = "r"
		input.ChannelID = "web"
		input.SessionID = "s"
		if len(input.Answers) == 0 {
			input.Answers = []map[string]any{{"a": 1}}
		}
		built, err := NewInteractionAnswerInput(input)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.resume, built.ResumesInterruptedTurn(), tc.name)
	}
}

// ──────────────────────────── 转请求 ────────────────────────────

// TestToAgentRequest_普通交互 chat.answer + query=nil + 非流式。
// Python: interaction.py:188-232
func TestToAgentRequest_普通交互(t *testing.T) {
	input, err := NewInteractionAnswerInput(validAnswerInput())
	require.NoError(t, err)

	req, err := input.ToAgentRequest()
	require.NoError(t, err)
	require.Equal(t, schema.ReqMethodChatAnswer, req.ReqMethod)
	require.False(t, req.IsStream)
	require.Equal(t, "req-1", req.RequestID)
	require.Equal(t, "web", req.ChannelID)
	require.NotNil(t, req.SessionID)
	require.Equal(t, "sess-1", *req.SessionID)

	params := map[string]any{}
	require.NoError(t, json.Unmarshal(req.Params, &params))
	require.Nil(t, params["query"], "普通交互 query 应为 nil")
	require.True(t, params["supports_user_interaction"].(bool))
	require.Equal(t, "act-1", params["request_id"])
}

// TestToAgentRequest_续跑 chat.send + query="" + 流式。
func TestToAgentRequest_续跑(t *testing.T) {
	input := validAnswerInput()
	input.Source = "permission_interrupt"
	built, err := NewInteractionAnswerInput(input)
	require.NoError(t, err)

	req, err := built.ToAgentRequest()
	require.NoError(t, err)
	require.Equal(t, schema.ReqMethodChatSend, req.ReqMethod, "续跑用 chat.send")
	require.True(t, req.IsStream)

	params := map[string]any{}
	require.NoError(t, json.Unmarshal(req.Params, &params))
	require.Equal(t, "", params["query"], "续跑 query 为空串")
}

// TestToAgentRequest_可选字段仅非空写入 空字段不出现。
// Python: interaction.py:198-205
func TestToAgentRequest_可选字段仅非空写入(t *testing.T) {
	input := validAnswerInput()
	built, err := NewInteractionAnswerInput(input)
	require.NoError(t, err)
	req, err := built.ToAgentRequest()
	require.NoError(t, err)

	params := map[string]any{}
	require.NoError(t, json.Unmarshal(req.Params, &params))
	for _, key := range []string{
		"interrupt_id", "mode", "work_mode", "project_dir", "cwd",
		"approval_schema", "evolution_meta", "trusted_dirs", "run_permissions",
	} {
		require.NotContains(t, params, key, "空字段 %s 不应输出", key)
	}
}

// TestToAgentRequest_可选字段有值就写入 全字段赋值后逐一出现。
func TestToAgentRequest_可选字段有值就写入(t *testing.T) {
	input := validAnswerInput()
	input.InterruptID = "int-1"
	input.Mode = "agent.work.normal"
	input.WorkMode = "code"
	input.ProjectDir = "/proj"
	input.Cwd = "/cwd"
	input.ApprovalSchema = "schema.v1"
	input.EvolutionMeta = map[string]any{"k": "v"}
	input.TrustedDirs = []string{"/a"}
	input.RunPermissions = map[string]any{"bash": "allow"}

	built, err := NewInteractionAnswerInput(input)
	require.NoError(t, err)
	req, err := built.ToAgentRequest()
	require.NoError(t, err)

	params := map[string]any{}
	require.NoError(t, json.Unmarshal(req.Params, &params))
	for _, key := range []string{
		"interrupt_id", "mode", "work_mode", "project_dir", "cwd",
		"approval_schema", "evolution_meta", "trusted_dirs", "run_permissions",
	} {
		require.Contains(t, params, key, "字段 %s 应有值", key)
	}
	require.Equal(t, "code", params["work_mode"])
}

// ──────────────────────────── 读端 ────────────────────────────

// TestAnswerInteractionInput_nil被拒 类型守卫。
// Python: service.py:2010-2011
func TestAnswerInteractionInput_nil被拒(t *testing.T) {
	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	_, err := runtime.AnswerInteractionInput(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "answer must be an InteractionAnswerInput")
}

// TestAnswerInteractionInput_自动启动后走准入 start 自动执行，随后准入 fail-closed。
// Python: service.py:2012-2014（await self.start() 后再 _require_owned_single_agent_session）
//
// 会话不存在 → 准入拒绝。这里同时验证 start() 确实被调用（否则会报 not started）。
func TestAnswerInteractionInput_自动启动后走准入(t *testing.T) {
	stubAdmissionMissing(t)

	runtime := NewAgentRuntime(AgentRuntimeDeps{})
	input, err := NewInteractionAnswerInput(validAnswerInput())
	require.NoError(t, err)

	_, err = runtime.AnswerInteractionInput(context.Background(), input)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "not started", "start() 应已自动执行")
	require.ErrorIs(t, err, ErrSessionNotFound)
}
