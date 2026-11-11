package processcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestBoundedCleanup_执行与超时 正常执行调用一次；超时不阻塞。
// Python: app.py:947-952 _bounded_cleanup
func TestBoundedCleanup_执行与超时(t *testing.T) {
	called := 0
	boundedCleanup(func() error { called++; return nil })
	if called != 1 {
		t.Errorf("正常步骤应执行 1 次，得 %d", called)
	}

	start := time.Now()
	boundedCleanup(func() error {
		time.Sleep(10 * time.Second)
		return nil
	})
	if elapsed := time.Since(start); elapsed > time.Duration((shutdownStepTimeoutSeconds+1)*float64(time.Second)) {
		t.Errorf("超时未生效，耗时 %v", elapsed)
	}

	// nil 步骤不 panic
	boundedCleanup(nil)
}

// TestBoundedCleanup_吞掉错误 步骤错误不外抛。
func TestBoundedCleanup_吞掉错误(t *testing.T) {
	boundedCleanup(func() error { return os.ErrNotExist })
}

// TestWriteWorkerResult_两文件 回执文件与纯文本文件都写。
// Python: app.py:133-160
func TestWriteWorkerResult_两文件(t *testing.T) {
	dir := t.TempDir()
	args := &AppArgs{
		SessionResultFile: filepath.Join(dir, "session-id.txt"),
		WorkerResultFile:  filepath.Join(dir, "worker-result.json"),
	}
	if err := writeWorkerResult(args, sessionCreateOperation, "s1",
		"agent.code.normal", "code", "/proj"); err != nil {
		t.Fatalf("writeWorkerResult: %v", err)
	}

	raw, err := os.ReadFile(args.SessionResultFile)
	if err != nil || string(raw) != "s1" {
		t.Errorf("会话 ID 文件 = %q/%v", string(raw), err)
	}
	loaded, ok := readWorkerResult(args.WorkerResultFile, sessionCreateOperation)
	if !ok {
		t.Fatal("worker 回执读取失败")
	}
	if stringOr(loaded["mode"]) != "agent.code.normal" || stringOr(loaded["work_mode"]) != "code" {
		t.Errorf("回执字段 = %v", loaded)
	}
	if stringOr(loaded["project_dir"]) != "/proj" {
		t.Errorf("project_dir = %v", loaded["project_dir"])
	}
}

// TestWriteWorkerResult_无文件路径 两个路径都空时不报错。
func TestWriteWorkerResult_无文件路径(t *testing.T) {
	if err := writeWorkerResult(&AppArgs{}, operationChat, "s1", "m", "code", ""); err != nil {
		t.Errorf("空路径应无错：%v", err)
	}
}

// TestBuildSkillsListRequest 无会话时 SessionID 为 nil。
// Python: app.py:111-124
func TestBuildSkillsListRequest(t *testing.T) {
	req := buildSkillsListRequest(&AppArgs{}, "r1")
	if req.SessionID != nil {
		t.Errorf("空会话应 nil，得 %v", *req.SessionID)
	}
	if req.ReqMethod != "skills.list" {
		t.Errorf("ReqMethod = %v", req.ReqMethod)
	}
	if req.IsStream {
		t.Error("技能查询应为非流式")
	}

	req = buildSkillsListRequest(&AppArgs{Session: " s1 "}, "r1")
	if req.SessionID == nil || *req.SessionID != "s1" {
		t.Errorf("会话 ID 未 trim：%v", req.SessionID)
	}
}

// TestRequestParamsBool 解析与非解析两种。
func TestRequestParamsBool(t *testing.T) {
	req := buildRequestMust(t, &AppArgs{
		Prompt: "x", Mode: "agent.code.normal", WorkMode: "code", InteractiveWorker: true,
	})
	if value, ok := requestParamsBool(req, "supports_user_interaction"); !ok || !value {
		t.Errorf("worker 应为 true：%v/%v", value, ok)
	}
	if _, ok := requestParamsBool(req, "nonexistent"); ok {
		t.Error("缺失键应返回 false")
	}
}

// TestOptionalString 空串转 nil。
func TestOptionalString(t *testing.T) {
	if optionalString("") != nil {
		t.Error("空串应返回 nil")
	}
	if optionalString("   ") != nil {
		t.Error("空白应返回 nil")
	}
	if got := optionalString(" s1 "); got == nil || *got != "s1" {
		t.Errorf("trim 失败：%v", got)
	}
}

// TestEventSessionID nil 安全。
func TestEventSessionID(t *testing.T) {
	if got := eventSessionID(nil); got != "" {
		t.Errorf("nil 请求 = %q", got)
	}
	if got := eventSessionID(buildRequestMust(t, &AppArgs{
		Prompt: "x", Mode: "agent.code.normal", WorkMode: "code",
	})); got != "s1" {
		t.Errorf("会话 ID = %q, 期望 s1", got)
	}
}

// TestDescriptorWorkMode nil 安全与 mode 优先。
// Python: app.py:215-219
func TestDescriptorWorkMode(t *testing.T) {
	if got := descriptorWorkMode(nil, "work"); got != "work" {
		t.Errorf("nil 描述符应回落 fallback，得 %q", got)
	}
}

// TestAbortPreparedSession_nil安全 nil 不 panic。
func TestAbortPreparedSession_nil安全(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	abortPreparedSession(client, nil)
}

// TestRenderInteractionPrompt 渲染提问与选项。
// Python: app.py:482-496
func TestRenderInteractionPrompt(t *testing.T) {
	var out strings.Builder
	options := renderInteractionPrompt(map[string]any{
		"question": "是否继续？",
		"options": []any{
			map[string]any{"label": "是", "value": "yes", "description": "继续执行"},
			map[string]any{"label": "否", "value": "no"},
		},
	}, &out)
	if len(options) != 2 {
		t.Fatalf("选项数 = %d", len(options))
	}
	text := out.String()
	for _, want := range []string{"是否继续？", "1. 是 — 继续执行", "2. 否", "请输入选项或自定义内容"} {
		if !strings.Contains(text, want) {
			t.Errorf("缺 %q：%q", want, text)
		}
	}
}

// TestRenderInteractionPrompt_无选项 只有提问与提示。
func TestRenderInteractionPrompt_无选项(t *testing.T) {
	var out strings.Builder
	options := renderInteractionPrompt(map[string]any{"message": "说点什么"}, &out)
	if len(options) != 0 {
		t.Errorf("无选项应为空：%v", options)
	}
	if !strings.Contains(out.String(), "说点什么") {
		t.Errorf("message 未渲染：%q", out.String())
	}
}

// TestInteractionAnswer_读到EOF 空输入返回 EOF。
// Python: app.py:522-535（EOFError → INTERACTIVE_INPUT_REQUIRED）
func TestInteractionAnswer_读到EOF(t *testing.T) {
	var out strings.Builder
	renderer := NewEventRenderer(OutputHuman, &out, &out, false, false, false)
	_, _, err := interactionAnswer(
		map[string]any{"question": "q"}, strings.NewReader(""), renderer,
	)
	if err == nil {
		t.Error("空输入应返回 EOF")
	}
}

// TestInteractionAnswer_读一行 单问单答。
func TestInteractionAnswer_读一行(t *testing.T) {
	var out strings.Builder
	renderer := NewEventRenderer(OutputHuman, &out, &out, false, false, false)
	answer, answers, err := interactionAnswer(
		map[string]any{"question": "q"}, strings.NewReader("yes\n"), renderer,
	)
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if answer != "yes" {
		t.Errorf("answer = %q", answer)
	}
	if len(answers) != 1 {
		t.Errorf("answers = %v", answers)
	}
}

// TestOwnedSessionDescriptor_外部渠道 nil client 时不 panic 且返回 nil。
func TestOwnedSessionDescriptor_nilClient(t *testing.T) {
	client := NewInProcessRuntimeClient(nil)
	if ownedSessionDescriptor(client, "nonexistent-session-id") != nil {
		t.Error("不存在的会话应返回 nil")
	}
}

// TestIsTerminalTeamEvent_nil参数 nil 安全。
func TestIsTerminalTeamEvent_nil参数(t *testing.T) {
	if isTerminalTeamEvent(nil, &swruntime.RuntimeEvent{}) {
		t.Error("nil args 应返回 false")
	}
	if isTerminalTeamEvent(&AppArgs{Mode: "team.work.normal"}, nil) {
		t.Error("nil event 应返回 false")
	}
}

// buildRequestMust 构造请求并断言无错。
func buildRequestMust(t *testing.T, args *AppArgs) *swschema.AgentRequest {
	t.Helper()
	request, err := buildRequest(args, "s1", "r1")
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	return request
}
