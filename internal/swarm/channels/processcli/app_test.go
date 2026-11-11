package processcli

import (
	"encoding/json"
	"testing"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestBuildRequest_非worker时supportsUserInteraction为false
// 即使 stdin 是 TTY，非 worker 命令也必须为 false（test_process_cli.py:1711）。
func TestBuildRequest_非worker时supportsUserInteraction为false(t *testing.T) {
	args := &AppArgs{Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code"}
	request, err := buildRequest(args, "session-1", "req-1")
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	params := decodeParams(t, request)
	if params["supports_user_interaction"] != false {
		t.Errorf("supports_user_interaction = %v, 期望 false", params["supports_user_interaction"])
	}
	if params["supports_permission_approval"] != false {
		t.Errorf("supports_permission_approval = %v, 期望 false", params["supports_permission_approval"])
	}
}

// TestBuildRequest_worker时supportsUserInteraction为true。
func TestBuildRequest_worker时supportsUserInteraction为true(t *testing.T) {
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code",
		InteractiveWorker: true,
	}
	request, _ := buildRequest(args, "s1", "r1")
	params := decodeParams(t, request)
	if params["supports_user_interaction"] != true {
		t.Errorf("worker 应为 true，得 %v", params["supports_user_interaction"])
	}
}

// TestBuildRequest_九字段齐全 九个 params 字段都在。
// Python: app.py:86-96
func TestBuildRequest_九字段齐全(t *testing.T) {
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code",
		Cwd: "/tmp", ProjectDir: "/tmp/proj", TrustedDirs: []string{"/tmp/t"},
	}
	request, _ := buildRequest(args, "s1", "r1")
	params := decodeParams(t, request)
	for _, key := range []string{
		"query", "content", "mode", "work_mode", "cwd", "project_dir",
		"trusted_dirs", "supports_user_interaction", "supports_permission_approval",
	} {
		if _, ok := params[key]; !ok {
			t.Errorf("params 缺 %q：%v", key, params)
		}
	}
	if params["query"] != params["content"] {
		t.Error("query 与 content 应同值")
	}
}

// TestBuildRequest_modelSelection仅在非空时入参。
func TestBuildRequest_modelSelection(t *testing.T) {
	args := &AppArgs{Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code"}
	request, _ := buildRequest(args, "s1", "r1")
	if _, ok := decodeParams(t, request)["model_name"]; ok {
		t.Error("空 selection 不应出现 model_name")
	}
	args.ModelSelection = "gpt-4#0"
	request, _ = buildRequest(args, "s1", "r1")
	if decodeParams(t, request)["model_name"] != "gpt-4#0" {
		t.Error("model_name 未透传")
	}
}

// TestBuildRequest_trustedDirs空时回落projectDir。
// Python: app.py:84-85 trusted_dirs = [project_dir]
func TestBuildRequest_trustedDirs空时回落(t *testing.T) {
	args := &AppArgs{Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code", Cwd: t.TempDir()}
	request, _ := buildRequest(args, "s1", "r1")
	params := decodeParams(t, request)
	dirs, ok := params["trusted_dirs"].([]any)
	if !ok || len(dirs) != 1 {
		t.Fatalf("trusted_dirs = %v, 期望 1 项", params["trusted_dirs"])
	}
	if dirs[0] != params["project_dir"] {
		t.Errorf("trusted_dirs[0] = %v, 期望等于 project_dir %v", dirs[0], params["project_dir"])
	}
}

// TestIsTerminalTeamEvent_只团队生效 单 Agent 的 processing_status 不算终态。
// Python: app.py:596-608
func TestIsTerminalTeamEvent_只团队生效(t *testing.T) {
	event := &swruntime.RuntimeEvent{
		Payload: map[string]any{
			"event_type":    "chat.processing_status",
			"is_processing": false,
			"is_complete":   true,
		},
		OK: true,
	}
	singleAgent := &AppArgs{Mode: "agent.code.normal"}
	if isTerminalTeamEvent(singleAgent, event) {
		t.Error("单 Agent 不应把 processing_status 当终态")
	}
	team := &AppArgs{Mode: "team.work.normal"}
	if !isTerminalTeamEvent(team, event) {
		t.Error("团队模式应视为本轮结束")
	}
	// 非该事件类型不算终态
	other := &swruntime.RuntimeEvent{
		Payload: map[string]any{"event_type": "chat.delta", "delta": "x"}, OK: true,
	}
	if isTerminalTeamEvent(team, other) {
		t.Error("非 processing_status 不应视为终态")
	}
}

// TestIsTerminalTeamEvent_isProcessing为真不算终态。
func TestIsTerminalTeamEvent_处理中不算终态(t *testing.T) {
	event := &swruntime.RuntimeEvent{
		Payload: map[string]any{
			"event_type":    "chat.processing_status",
			"is_processing": true,
			"is_complete":   false,
		},
		OK: true,
	}
	if isTerminalTeamEvent(&AppArgs{Mode: "team.work.normal"}, event) {
		t.Error("处理中不应视为终态")
	}
}

// TestAnswerRequest_resume判定 interrupt_id 仅在非空时入 params。
// Python: app.py:538-574
func TestAnswerRequest_resume判定(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"mode": "agent.code.normal", "work_mode": "code",
		"supports_user_interaction": true, "supports_permission_approval": true,
	})
	sid := "s-1"
	original := &swschema.AgentRequest{
		RequestID: "req-1", ChannelID: processCliChannelID,
		SessionID: &sid, Params: params,
	}
	interaction := &swruntime.RuntimeEvent{
		Payload: map[string]any{
			"event_type":   "chat.ask_user_question",
			"request_id":   "call-1",
			"source":       "ask_user_interrupt",
			"interrupt_id": "intr-1",
		},
	}
	answer, resume := answerRequest(original, interaction, nil)
	if !resume {
		t.Error("ask_user_interrupt + request_id 应判为 resume")
	}
	if answer.ReqMethod != swschema.ReqMethodChatSend {
		t.Errorf("resume 时 ReqMethod = %v, 期望 ChatSend", answer.ReqMethod)
	}
	if !answer.IsStream {
		t.Error("resume 时应为流式")
	}
	answerParams := decodeParams(t, answer)
	if answerParams["interrupt_id"] != "intr-1" {
		t.Errorf("interrupt_id = %v", answerParams["interrupt_id"])
	}
	if answerParams["query"] != "" {
		t.Errorf("resume 时 query 应为空串，得 %v", answerParams["query"])
	}

	// 空 interrupt_id 时不应出现在 params
	interaction.Payload["interrupt_id"] = ""
	answer, _ = answerRequest(original, interaction, nil)
	if _, ok := decodeParams(t, answer)["interrupt_id"]; ok {
		t.Error("空 interrupt_id 不应入 params")
	}
}

// TestAnswerRequest_非resume source 不属 resume 集合时走 ChatAnswer。
func TestAnswerRequest_非resume(t *testing.T) {
	params, _ := json.Marshal(map[string]any{"mode": "agent.code.normal"})
	sid := "s-1"
	original := &swschema.AgentRequest{
		RequestID: "req-1", ChannelID: processCliChannelID, SessionID: &sid, Params: params,
	}
	interaction := &swruntime.RuntimeEvent{
		Payload: map[string]any{
			"event_type": "chat.ask_user_question",
			"request_id": "call-1",
			"source":     "unknown_source",
		},
	}
	answer, resume := answerRequest(original, interaction, nil)
	if resume {
		t.Error("非 resume source 不应判为 resume")
	}
	if answer.ReqMethod != swschema.ReqMethodChatAnswer {
		t.Errorf("ReqMethod = %v, 期望 ChatAnswer", answer.ReqMethod)
	}
}

// TestCancelRequest_沿用原requestID 取消必须指向在飞执行本身。
// Python: app.py:577-593
func TestCancelRequest_沿用原requestID(t *testing.T) {
	params, _ := json.Marshal(map[string]any{"mode": "agent.code.normal"})
	sid := "s-1"
	original := &swschema.AgentRequest{
		RequestID: "req-1", ChannelID: processCliChannelID, SessionID: &sid, Params: params,
	}
	cancel := appCancelRequest(original)
	if cancel.RequestID != "req-1" {
		t.Errorf("cancel RequestID = %q, 期望沿用 req-1", cancel.RequestID)
	}
	if cancel.ReqMethod != swschema.ReqMethodChatCancel {
		t.Errorf("ReqMethod = %v, 期望 ChatCancel", cancel.ReqMethod)
	}
	cancelParams := decodeParams(t, cancel)
	if cancelParams["intent"] != "cancel" {
		t.Errorf("intent = %v", cancelParams["intent"])
	}
	if cancelParams["target_request_id"] != "req-1" {
		t.Errorf("target_request_id = %v", cancelParams["target_request_id"])
	}
}

// TestInteractionAnswers_序号选值 数字输入按选项序号取 value。
// Python: app.py:499-510
func TestInteractionAnswers_序号选值(t *testing.T) {
	options := []map[string]any{
		{"label": "A", "value": "allow"},
		{"label": "B", "value": "deny"},
	}
	answers := interactionAnswers("2", options)
	if len(answers) != 1 {
		t.Fatalf("答案数 = %d", len(answers))
	}
	selected, _ := answers[0]["selected_options"].([]string)
	if len(selected) != 1 || selected[0] != "deny" {
		t.Errorf("selected_options = %v, 期望 [deny]", selected)
	}
	if answers[0]["custom_input"] != "2" {
		t.Errorf("custom_input = %v", answers[0]["custom_input"])
	}
}

// TestInteractionAnswers_越界序号 用原文。
func TestInteractionAnswers_越界序号(t *testing.T) {
	answers := interactionAnswers("9", []map[string]any{{"value": "a"}})
	selected, _ := answers[0]["selected_options"].([]string)
	if selected[0] != "9" {
		t.Errorf("越界序号应用原文，得 %q", selected[0])
	}
}

// TestInteractionQuestions_单问归一 questions 缺失时把 payload 当一问。
// Python: app.py:513-519
func TestInteractionQuestions_单问归一(t *testing.T) {
	single := interactionQuestions(map[string]any{"question": "q1"})
	if len(single) != 1 || single[0]["question"] != "q1" {
		t.Errorf("单问归一失败：%v", single)
	}
	multiple := interactionQuestions(map[string]any{
		"questions": []any{
			map[string]any{"question": "q1"},
			map[string]any{"question": "q2"},
		},
	})
	if len(multiple) != 2 {
		t.Errorf("多问 = %v", multiple)
	}
}

// TestSessionOperations_四成员 四个有状态操作都在集合里。
// Python: app.py:54-61
func TestSessionOperations_四成员(t *testing.T) {
	for _, operation := range []string{
		sessionCreateOperation, sessionSwitchOperation,
		sessionForkOperation, sessionDeleteOperation,
	} {
		if _, ok := sessionOperations[operation]; !ok {
			t.Errorf("%q 应在 sessionOperations", operation)
		}
	}
	if _, ok := sessionOperations[operationChat]; ok {
		t.Error("chat 不属有状态操作")
	}
}

// TestOwnedSessionDescriptor_空ID 空会话 ID 返回 nil。
// Python: app.py:200-212
func TestOwnedSessionDescriptor_空ID(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	if ownedSessionDescriptor(client, "") != nil {
		t.Error("空 ID 应返回 nil")
	}
	if ownedSessionDescriptor(client, "   ") != nil {
		t.Error("空白 ID 应返回 nil")
	}
}

// decodeParams 解析请求参数。
func decodeParams(t *testing.T, request *swschema.AgentRequest) map[string]any {
	t.Helper()
	params := map[string]any{}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatalf("params 解析失败: %v", err)
		}
	}
	return params
}
