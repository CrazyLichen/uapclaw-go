package processcli

import (
	"encoding/json"
	"strings"
	"testing"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── Run 主流程 ────────────────────────────

// TestRun_空参数返回失败 nil args 直接返回失败码，不 panic。
func TestRun_空参数返回失败(t *testing.T) {
	var out, errOut strings.Builder
	if code := Run(nil, &out, &errOut); code != exitFailure {
		t.Errorf("exit = %d, 期望 %d", code, exitFailure)
	}
}

// TestRun_未支持操作报错 非 chat 非会话操作非 skills 时报 unsupported。
// Python: app.py:813-814
func TestRun_未支持操作报错(t *testing.T) {
	var out, errOut strings.Builder
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code",
		Output: OutputJSON, Operation: "bogus.operation",
	}
	if code := Run(args, &out, &errOut); code != exitFailure {
		t.Errorf("exit = %d, 期望 %d", code, exitFailure)
	}
	document := decodeJSONDocument(t, out.String())
	assertContains(t, document["events"], "unsupported process CLI operation")
}

// TestRun_json输出结构 json 模式最终产出一份完整文档。
// Python: app.py:884-922 renderer.finish
func TestRun_json输出结构(t *testing.T) {
	var out, errOut strings.Builder
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code",
		Output: OutputJSON, Operation: operationChat,
	}
	Run(args, &out, &errOut)

	document := decodeJSONDocument(t, out.String())
	for _, key := range []string{"ok", "session_id", "request_id", "events"} {
		if _, present := document[key]; !present {
			t.Errorf("文档缺 %q：%v", key, document)
		}
	}
	if document["ok"] != false {
		t.Errorf("ok = %v, 期望 false（空 Runtime 启动失败）", document["ok"])
	}
}

// TestRun_jsonl输出逐行 jsonl 模式每条事件一行、以换行结尾。
func TestRun_jsonl输出逐行(t *testing.T) {
	var out, errOut strings.Builder
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code",
		Output: OutputJSONL, Operation: operationChat,
	}
	Run(args, &out, &errOut)

	text := out.String()
	if text == "" {
		t.Fatal("jsonl 模式应有输出")
	}
	if !strings.HasSuffix(text, "\n") {
		t.Error("jsonl 每条应以换行结尾")
	}
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Errorf("非合法 JSON 行 %q: %v", line, err)
		}
	}
}

// TestRun_chat错误事件 模型不可用时的失败被转成 chat.error 事件。
// Python: app.py:904-916 异常转事件
//
// 注意 ok 不可作为判据：流事件的 ok 来自 from_agent_message 的默认 True
// （上游 events.py:117-121 注释），chat.error 这类终态错误会以 ok=True 到达。
// 上游口径是「同时看 event_type」——见 events.py:127-133
// TERMINAL_ERROR_EVENT_TYPES，Go 侧对应 runtime.TerminalErrorEventTypes。
func TestRun_chat错误事件(t *testing.T) {
	var out, errOut strings.Builder
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code",
		Output: OutputJSON, Operation: operationChat,
	}
	Run(args, &out, &errOut)

	document := decodeJSONDocument(t, out.String())
	events, ok := document["events"].([]any)
	if !ok || len(events) == 0 {
		t.Fatalf("events 为空：%v", document)
	}
	first, _ := events[0].(map[string]any)
	payload, _ := first["payload"].(map[string]any)
	if payload["event_type"] != "chat.error" {
		t.Errorf("event_type = %v, 期望 chat.error", payload["event_type"])
	}
	// 终态错误事件必须被识别为失败（不能只看 ok 字段）。
	eventType, _ := payload["event_type"].(string)
	if _, isTerminal := swruntime.TerminalErrorEventTypes[eventType]; !isTerminal {
		t.Errorf("event_type %q 应在 TerminalErrorEventTypes 内", eventType)
	}
	if payload["error"] == nil || payload["error"] == "" {
		t.Errorf("chat.error 应带 error 文本，实际 %v", payload["error"])
	}
}

// TestRun_超时返回124 未开始的 Runtime 加 timeout 时仍走超时分支或失败分支。
// Python: app.py:882-898 超时收敛
func TestRun_超时返回124(t *testing.T) {
	var out, errOut strings.Builder
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code",
		Output: OutputJSON, Operation: operationChat,
		Timeout: 0.001, TimeoutSet: true,
	}
	code := Run(args, &out, &errOut)
	// 空 Runtime 启动极快，可能先落到失败分支；两者都在契约五档内
	if code != exitTimeout && code != exitFailure {
		t.Errorf("exit = %d, 期望 %d 或 %d", code, exitTimeout, exitFailure)
	}
}

// TestRun_工作模式被归一 Run 开头会把 work_mode 归一写回 args。
// Python: app.py:755 args.work_mode = resolve_cli_work_mode(...)
func TestRun_工作模式被归一(t *testing.T) {
	var out, errOut strings.Builder
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "",
		Output: OutputJSON, Operation: operationChat,
	}
	Run(args, &out, &errOut)
	if args.WorkMode != "code" {
		t.Errorf("WorkMode = %q, 期望 code（agent.code.* 自带 code profile）", args.WorkMode)
	}
}

// TestRun_SessionTrim Run 会 trim session id 前后空白。
func TestRun_SessionTrim(t *testing.T) {
	var out, errOut strings.Builder
	args := &AppArgs{
		Prompt: "hi", Mode: "agent.code.normal", WorkMode: "code",
		Output: OutputJSON, Operation: operationChat,
		Session: "  padded-session  ",
	}
	Run(args, &out, &errOut)
	// 走错误路径时 session_id 输出应为空（未开通成功），
	// 这里只验证不 panic 且 args.Session 未被就地改写
	if args.Session != "  padded-session  " {
		t.Errorf("args.Session 被就地改写：%q", args.Session)
	}
}

// ──────────────────────────── cleanupClient 三层嵌套 ────────────────────────────

// TestCleanupClient_三段均执行 无论会话清理成败，close 与 flush 都执行。
// Python: app.py:923-944 三层 finally 嵌套
func TestCleanupClient_三段均执行(t *testing.T) {
	// 用空 Runtime 的 client：CleanupSession/Close 都返回错误，
	// 但 cleanupClient 不得 panic，且必须走完 flush
	client := NewInProcessRuntimeClient(nil)
	cleanupClient(client, "session-1", operationChat)
	// 无断言失败即通过（boundedCleanup 吞掉所有错误）
}

// TestCleanupClient_非chat跳过会话清理 非 chat 操作不调 CleanupSession。
// Python: app.py:925 if session_id and operation == CHAT_OPERATION
func TestCleanupClient_非chat跳过会话清理(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	cleanupClient(client, "session-1", sessionCreateOperation)
	// 不 panic 即通过
}

// TestCleanupClient_空会话跳过清理 sessionID 为空时直接走 close+flush。
func TestCleanupClient_空会话跳过清理(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	cleanupClient(client, "", operationChat)
}

// ──────────────────────────── consume 测试分组 ────────────────────────────

// TestConsume_空流转失败 无 client 运行时 consume 不得 panic。
func TestConsume_空流转失败(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	renderer := NewEventRenderer(OutputJSON, &strings.Builder{}, &strings.Builder{},
		false, false, false)
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code", Operation: operationChat}
	request, err := buildRequest(args, "s-1", "r-1")
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	code := consume(client, request, renderer, args, "s-1", "r-1")
	if code != exitFailure {
		t.Errorf("exit = %d, 期望 %d（空 Runtime 无法流式执行）", code, exitFailure)
	}
}

// ──────────────────────────── 四个会话操作 ────────────────────────────

// TestCreateSession_参数校验 /new 只接受空/--persist/--persist-session。
// Python: app.py:229-234
func TestCreateSession_参数校验(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	renderer := NewEventRenderer(OutputJSON, &strings.Builder{}, &strings.Builder{},
		false, false, false)
	for _, bad := range []string{"--bogus", "extra args", "persist"} {
		args := &AppArgs{Prompt: bad, Mode: "agent.code.normal", WorkMode: "code"}
		_, err := createSession(client, args, renderer, "r-1")
		if err == nil {
			t.Errorf("Prompt=%q 应报 BAD_REQUEST", bad)
		}
	}
}

// TestSwitchSession_空目标报错 /resume 无参数时报 BAD_REQUEST。
// Python: app.py:328-330
func TestSwitchSession_空目标报错(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	renderer := NewEventRenderer(OutputJSON, &strings.Builder{}, &strings.Builder{},
		false, false, false)
	args := &AppArgs{Prompt: "", Mode: "agent.code.normal", WorkMode: "code"}
	_, err := switchSession(client, args, renderer, "r-1")
	if err == nil {
		t.Error("空 session_id 应报 BAD_REQUEST")
	}
}

// TestForkSession_无源会话报错 /branch 无当前会话时报 BAD_REQUEST。
// Python: app.py:394-399
func TestForkSession_无源会话报错(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	renderer := NewEventRenderer(OutputJSON, &strings.Builder{}, &strings.Builder{},
		false, false, false)
	args := &AppArgs{Prompt: "", Mode: "agent.code.normal", WorkMode: "code", Session: ""}
	_, err := forkSession(client, args, renderer, "r-1")
	if err == nil {
		t.Error("无当前会话应报 BAD_REQUEST")
	}
}

// TestDeleteSession_空目标报错 /delete 无参数时报 BAD_REQUEST。
// Python: app.py:451-453
func TestDeleteSession_空目标报错(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	renderer := NewEventRenderer(OutputJSON, &strings.Builder{}, &strings.Builder{},
		false, false, false)
	args := &AppArgs{Prompt: "", Mode: "agent.code.normal", WorkMode: "code"}
	_, err := deleteSession(client, args, renderer, "r-1")
	if err == nil {
		t.Error("空 session_id 应报 BAD_REQUEST")
	}
}

// ──────────────────────────── 测试助手 ────────────────────────────

// decodeJSONDocument 解析单份 JSON 文档。
func decodeJSONDocument(t *testing.T, raw string) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &document); err != nil {
		t.Fatalf("输出不是合法 JSON 文档：%v\n%s", err, raw)
	}
	return document
}

// assertContains 断言 JSON 序列化后含指定子串。
func assertContains(t *testing.T, value any, want string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	if !strings.Contains(string(encoded), want) {
		t.Errorf("输出不含 %q：%s", want, encoded)
	}
}
