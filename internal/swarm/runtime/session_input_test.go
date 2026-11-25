package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	session "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// SessionInputTargetErrorPtr 供 errors.As 使用的目标错误二级指针。
func SessionInputTargetErrorPtr() **SessionInputTargetError {
	return new(*SessionInputTargetError)
}

// TestResolveSessionInputMode 主字段 / 遗留别名 / 冲突 / 未知值。
func TestResolveSessionInputMode(t *testing.T) {
	if got := ResolveSessionInputMode(map[string]any{"input_mode": "steer"}); got == nil || *got != SessionInputModeSteer {
		t.Errorf("input_mode=steer 应解析为 steer，实际 %v", got)
	}
	if got := ResolveSessionInputMode(map[string]any{"runtime_mode": "follow_up"}); got == nil || *got != SessionInputModeFollowUp {
		t.Errorf("runtime_mode 别名应生效，实际 %v", got)
	}
	if got := ResolveSessionInputMode(map[string]any{"input_mode": "STEER"}); got == nil || *got != SessionInputModeSteer {
		t.Errorf("大小写应归一化，实际 %v", got)
	}
	if got := ResolveSessionInputMode(map[string]any{"input_mode": "unknown"}); got != nil {
		t.Errorf("未知值应返回 nil，实际 %v", got)
	}
	if got := ResolveSessionInputMode(nil); got != nil {
		t.Errorf("nil 参数应返回 nil，实际 %v", got)
	}
	if got := ResolveSessionInputMode(map[string]any{}); got != nil {
		t.Errorf("无字段应返回 nil，实际 %v", got)
	}
	// 上游用真值判断：空串主字段不算"存在"，不触发冲突
	if got := ResolveSessionInputMode(map[string]any{"input_mode": "", "runtime_mode": "steer"}); got == nil || *got != SessionInputModeSteer {
		t.Errorf("主字段为空串时应取别名的值，实际 %v", got)
	}
}

// TestResolveSessionInputMode_主字段与别名冲突 冲突时报错。
func TestResolveSessionInputMode_主字段与别名冲突(t *testing.T) {
	_, err := ResolveSessionInputModeChecked(map[string]any{
		"input_mode":   "steer",
		"runtime_mode": "follow_up",
	})
	if err == nil {
		t.Fatal("主字段与别名冲突应报错")
	}
	if err.Error() != "input_mode and runtime_mode must agree" {
		t.Errorf("错误文案不符: %q", err.Error())
	}
	// 非冲突版本应吞掉错误返回 nil
	if got := ResolveSessionInputMode(map[string]any{
		"input_mode": "steer", "runtime_mode": "follow_up",
	}); got != nil {
		t.Errorf("冲突时 ResolveSessionInputMode 应返回 nil，实际 %v", got)
	}
}

// TestValidateSteerImageReferences 六类拒收分支 + 合法路径。
func TestValidateSteerImageReferences(t *testing.T) {
	for _, key := range []string{"images", "image_files", "files", "attachments", "audio_files", "video_files"} {
		_, err := ValidateSteerImageReferences(map[string]any{key: []any{"x"}})
		if err == nil {
			t.Errorf("字段 %s 非空时应拒收", key)
		}
	}

	if _, err := ValidateSteerImageReferences(map[string]any{"media_items": "not-a-list"}); err == nil {
		t.Error("media_items 非 list 应报错")
	}

	if _, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1), "extra": "x"}},
	}); err == nil {
		t.Error("含多余键应报错")
	}

	if _, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "video", "path": "p", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1)}},
	}); err == nil {
		t.Error("type 非 image 应报错")
	}

	if _, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "image", "path": "", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1)}},
	}); err == nil {
		t.Error("path 为空应报错")
	}

	if _, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/bmp", "size_bytes": float64(1)}},
	}); err == nil {
		t.Error("不支持的 MIME 应报错")
	}

	refs, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1)}},
	})
	if err != nil {
		t.Fatalf("合法输入不应报错: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("应返回 1 条引用，实际 %d", len(refs))
	}

	refs, err = ValidateSteerImageReferences(map[string]any{})
	if err != nil || len(refs) != 0 {
		t.Errorf("无 media_items 应返回空且无错，实际 %v / %v", refs, err)
	}

	// 空切片视为"未提供"，不应拒收
	if _, err := ValidateSteerImageReferences(map[string]any{"files": []any{}}); err != nil {
		t.Errorf("空切片不应拒收: %v", err)
	}
}

// TestValidateSessionInput expected_execution_id 两条约束 + query 约束。
func TestValidateSessionInput(t *testing.T) {
	if err := ValidateSessionInput(map[string]any{}); err == nil {
		t.Error("无 query 应报错")
	}
	if err := ValidateSessionInput(map[string]any{"query": "   "}); err == nil {
		t.Error("空白 query 应报错")
	}
	if err := ValidateSessionInput(map[string]any{"query": "hello"}); err != nil {
		t.Errorf("合法 query 不应报错: %v", err)
	}
	if err := ValidateSessionInput(map[string]any{
		"query": "hi", "input_mode": "steer", "expected_execution_id": "",
	}); err == nil {
		t.Error("expected_execution_id 为空串应报错")
	}
	if err := ValidateSessionInput(map[string]any{
		"query": "hi", "expected_execution_id": "e1",
	}); err == nil {
		t.Error("非 steer 模式带 expected_execution_id 应报错")
	}
	if err := ValidateSessionInput(map[string]any{
		"query": "hi", "input_mode": "steer", "expected_execution_id": "e1",
	}); err != nil {
		t.Errorf("合法组合不应报错: %v", err)
	}
	if err := ValidateSessionInput(map[string]any{
		"input_mode": "steer",
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/jpeg", "size_bytes": float64(1)}},
	}); err != nil {
		t.Errorf("仅图片的 steer 输入不应报错: %v", err)
	}
}

// TestSessionInputErrors_错误码 三个异常的稳定 code。
func TestSessionInputErrors_错误码(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{&SessionInputRejectedError{Message: "x"}, "SESSION_INPUT_REJECTED"},
		{&SessionInputTargetError{Message: "x"}, "SESSION_INPUT_TARGET_CHANGED"},
		{&SessionInputQueueRequiredError{Message: "x"}, "SESSION_INPUT_QUEUE_REQUIRED"},
	}
	for _, tc := range cases {
		var coded interface{ Code() string }
		if !errors.As(tc.err, &coded) {
			t.Fatalf("%T 应实现 Code()", tc.err)
		}
		if coded.Code() != tc.code {
			t.Errorf("%T code = %q, want %q", tc.err, coded.Code(), tc.code)
		}
	}
}

// TestSessionInputErrors_继承关系 两个子类可被 errors.As 命中父类（对齐上游继承）。
func TestSessionInputErrors_继承关系(t *testing.T) {
	var rejected *SessionInputRejectedError
	if !errors.As(&SessionInputTargetError{Message: "x"}, &rejected) {
		t.Error("TargetError 应可被识别为 RejectedError")
	}
	if !errors.As(&SessionInputQueueRequiredError{Message: "x"}, &rejected) {
		t.Error("QueueRequiredError 应可被识别为 RejectedError")
	}
}

// TestSessionInputErrors_Error消息 Error() 返回 Message 原文。
func TestSessionInputErrors_Error消息(t *testing.T) {
	cases := []error{
		&SessionInputRejectedError{Message: "rejected-msg"},
		&SessionInputTargetError{Message: "target-msg"},
		&SessionInputQueueRequiredError{Message: "queue-msg"},
	}
	wants := []string{"rejected-msg", "target-msg", "queue-msg"}
	for i, err := range cases {
		if err.Error() != wants[i] {
			t.Errorf("%T.Error() = %q, want %q", err, err.Error(), wants[i])
		}
	}
}

// TestIsTruthySessionInputValue 复刻 Python 真值判断。
func TestIsTruthySessionInputValue(t *testing.T) {
	truthy := []any{true, "x", []any{"a"}, map[string]any{"k": "v"}, 1, int64(2), 3.5}
	for _, value := range truthy {
		if !isTruthySessionInputValue(value) {
			t.Errorf("%v 应为真", value)
		}
	}
	falsy := []any{nil, false, "", []any{}, map[string]any{}, 0, int64(0), 0.0}
	for _, value := range falsy {
		if isTruthySessionInputValue(value) {
			t.Errorf("%v 应为假", value)
		}
	}
}

// TestResolveSessionInputMode_枚举直传 直接给枚举值。
func TestResolveSessionInputMode_枚举直传(t *testing.T) {
	if got := ResolveSessionInputMode(map[string]any{"input_mode": SessionInputModeSteer}); got == nil || *got != SessionInputModeSteer {
		t.Errorf("枚举值直传应识别，实际 %v", got)
	}
}

// TestNormalizeSessionInputRaw 非字符串输入归一化为空串。
func TestNormalizeSessionInputRaw(t *testing.T) {
	if got := normalizeSessionInputRaw(nil); got != "" {
		t.Errorf("nil 应归一化为空串，实际 %q", got)
	}
	if got := normalizeSessionInputRaw(42); got != "" {
		t.Errorf("非字符串应归一化为空串，实际 %q", got)
	}
	if got := normalizeSessionInputRaw("  STEER  "); got != "steer" {
		t.Errorf("应去空白并小写，实际 %q", got)
	}
}

// TestValidateSessionInput_非字符串query 有图片但 query 非字符串时仍要求文本。
func TestValidateSessionInput_非字符串query(t *testing.T) {
	err := ValidateSessionInput(map[string]any{
		"input_mode": "steer",
		"query":      map[string]any{"bad": true},
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1)}},
	})
	if err == nil {
		t.Error("query 非字符串时应报 session input requires non-empty text")
	}
}

// fakeSteerTarget SteerTarget 测试替身（记录调用与入队内容）。
type fakeSteerTarget struct {
	activeRound any
	hasStream   bool
	enqueued    []string
	enqueueErr  error
}

func (f *fakeSteerTarget) ActiveRoundIs(bound any) bool { return f.activeRound != nil && f.activeRound == bound }
func (f *fakeSteerTarget) HasOutputStream() bool        { return f.hasStream }
func (f *fakeSteerTarget) EnqueueSteerEntry(display string) error {
	if f.enqueueErr != nil {
		return f.enqueueErr
	}
	f.enqueued = append(f.enqueued, display)
	return nil
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

// TestEnqueueBoundSessionInput_正常入队 校验通过后入队 display 并返回条目。
func TestEnqueueBoundSessionInput_正常入队(t *testing.T) {
	target := &fakeSteerTarget{activeRound: "round-1", hasStream: true}
	req := steerRequest("参考这张截图", nil)
	entry, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: target, BoundRound: "round-1", Request: req, Query: "参考这张截图",
	})
	if err != nil {
		t.Fatalf("不应拒收: %v", err)
	}
	if len(target.enqueued) != 1 || target.enqueued[0] != "参考这张截图" {
		t.Errorf("应入队 display 文本，实际 %v", target.enqueued)
	}
	if entry.DisplayContent != "参考这张截图" || entry.RequestID != "req-1" {
		t.Errorf("条目字段不符: %+v", entry)
	}
}

// TestEnqueueBoundSessionInput_轮次变更拒收 目标轮不再是准入捕获的轮。
func TestEnqueueBoundSessionInput_轮次变更拒收(t *testing.T) {
	target := &fakeSteerTarget{activeRound: "round-2", hasStream: true}
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: target, BoundRound: "round-1", Request: steerRequest("q", nil), Query: "q",
	})
	var targetErr *SessionInputTargetError
	if !errors.As(err, &targetErr) {
		t.Fatalf("应返回 TARGET_CHANGED，实际 %v", err)
	}
	if targetErr.Message != "the targeted execution has ended or changed; supplemental input was not sent" {
		t.Errorf("文案不符: %s", targetErr.Message)
	}
	if len(target.enqueued) != 0 {
		t.Error("拒收时不得入队")
	}
}

// TestEnqueueBoundSessionInput_输出宿主缺失 无消费者时拒收。
func TestEnqueueBoundSessionInput_输出宿主缺失(t *testing.T) {
	target := &fakeSteerTarget{activeRound: "round-1", hasStream: false}
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: target, BoundRound: "round-1", Request: steerRequest("q", nil), Query: "q",
	})
	if !errors.As(err, SessionInputTargetErrorPtr()) {
		t.Fatalf("应返回 TARGET_CHANGED，实际 %v", err)
	}
}

// TestEnqueueBoundSessionInput_execution复核 账本查无/终态/取消/异会话都拒收。
func TestEnqueueBoundSessionInput_execution复核(t *testing.T) {
	target := &fakeSteerTarget{activeRound: "round-1", hasStream: true}
	mk := func(h *session.SessionExecutionHandle) func(string) *session.SessionExecutionHandle {
		return func(string) *session.SessionExecutionHandle { return h }
	}
	running := session.SessionExecutionStateRunning
	cases := []struct {
		name string
		h    *session.SessionExecutionHandle
	}{
		{"查无", nil},
		{"终态", &session.SessionExecutionHandle{SessionID: "session-A", State: session.SessionExecutionStateSucceeded}},
		{"排队中", &session.SessionExecutionHandle{SessionID: "session-A", State: session.SessionExecutionStateQueued}},
		{"取消中", &session.SessionExecutionHandle{SessionID: "session-A", State: running, CancellationRequested: true}},
		{"异会话", &session.SessionExecutionHandle{SessionID: "session-B", State: running}},
	}
	for _, tc := range cases {
		_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
			Instance: target, BoundRound: "round-1",
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
	target := &fakeSteerTarget{activeRound: "round-1", hasStream: true}
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: target, BoundRound: "round-1",
		Request: steerRequest("q", map[string]any{"expected_execution_id": "exec-42"}), Query: "q",
		ExecutionLookup: func(string) *session.SessionExecutionHandle {
			return &session.SessionExecutionHandle{SessionID: "session-A", State: session.SessionExecutionStateRunning}
		},
	})
	if err != nil {
		t.Fatalf("RUNNING 同会话应通过: %v", err)
	}
}

// TestEnqueueBoundSessionInput_入队失败透传 EnqueueSteerEntry 的错误原样上抛。
func TestEnqueueBoundSessionInput_入队失败透传(t *testing.T) {
	target := &fakeSteerTarget{activeRound: "round-1", hasStream: true,
		enqueueErr: errors.New("active execution has no steering queue; supplemental input was not sent")}
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: target, BoundRound: "round-1", Request: steerRequest("q", nil), Query: "q",
	})
	if err == nil || !strings.Contains(err.Error(), "no steering queue") {
		t.Fatalf("应透传入队错误，实际 %v", err)
	}
}

// TestEnqueueBoundSessionInput_跨会话路由五键 跨会话键构造 message_route。
func TestEnqueueBoundSessionInput_跨会话路由五键(t *testing.T) {
	target := &fakeSteerTarget{activeRound: "round-1", hasStream: true}
	cross := map[string]any{"chain_id": "c1", "message_id": "m1", "hop_count": 2}
	_, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: target, BoundRound: "round-1",
		Request: steerRequest("q", map[string]any{"user_id": "u1", SessionMessageInternalKey: cross}), Query: "q",
	})
	if err != nil {
		t.Fatalf("不应拒收: %v", err)
	}
	// 再跑一次拿条目（第一次丢弃了返回值也校验过 err）
	entry, _ := EnqueueBoundSessionInput(EnqueueBoundOptions{
		Instance: target, BoundRound: "round-1",
		Request: steerRequest("q", map[string]any{"user_id": "u1", SessionMessageInternalKey: cross}), Query: "q",
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
	// JSON 反序列化后数值一律 float64
	if route["chain_id"] != "c1" || route["parent_message_id"] != "m1" || route["hop_count"] != float64(2) {
		t.Errorf("路由值不符: %v", route)
	}
	if entry.CrossSession["content"] != "q" {
		t.Errorf("cross_session.content 应为 display 文本，实际 %v", entry.CrossSession["content"])
	}
}

// TestQueuedSessionInput_BoundaryBoundaryFail 幂等与成功路径。
func TestQueuedSessionInput_BoundaryBoundaryFail(t *testing.T) {
	entry := NewQueuedSessionInput("q", "r")
	entry.CompleteBoundary()
	select {
	case <-entry.BoundaryReady:
	default:
		t.Error("成功路径应关闭边界信号")
	}
	failed := NewQueuedSessionInput("q", "r")
	first := errors.New("first")
	failed.FailBoundary(first)
	failed.FailBoundary(errors.New("second"))
	if failed.BoundaryError != first {
		t.Errorf("应保留首个错误，实际 %v", failed.BoundaryError)
	}
	select {
	case <-failed.BoundaryReady:
	default:
		t.Error("失败路径应关闭边界信号")
	}
}
