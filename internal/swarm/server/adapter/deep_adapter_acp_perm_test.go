package adapter

import (
	"context"
	"strings"
	"sync"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────── 测试辅助 ────────────────────

// acpPermSession 固定会话 ID 的 SessionFacade。
type acpPermSession struct{}

func (m *acpPermSession) GetSessionID() string                       { return "sess-acp-perm" }
func (m *acpPermSession) UpdateState(_ map[string]any)               {}
func (m *acpPermSession) GetState(_ state.StateKey) (any, error)     { return nil, nil }
func (m *acpPermSession) DumpState() map[string]any                  { return nil }
func (m *acpPermSession) WriteStream(_ context.Context, _ any) error { return nil }
func (m *acpPermSession) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}
func (m *acpPermSession) GetEnv(_ string, _ ...any) any           { return nil }
func (m *acpPermSession) Interact(_ context.Context, _ any) error { return nil }
func (m *acpPermSession) ClearSession(_ context.Context)          {}

// newAcpPermRequest 构造 ACP 通道权限确认请求（带 session 回调上下文）。
func newAcpPermRequest(t *testing.T, toolCallID, toolName string) harnesssecurity.PermissionConfirmationRequest {
	t.Helper()
	ctx := sschema.WithToolPermissionChannelID(context.Background(), "acp")
	cbCtx := sainterfaces.NewAgentCallbackContext(nil, nil, &acpPermSession{})
	req := harnesssecurity.PermissionConfirmationRequest{
		GoCtx: ctx,
		Ctx:   cbCtx,
	}
	if toolName != "" {
		req.ToolCall = &llmschema.ToolCall{
			ID:        toolCallID,
			Name:      toolName,
			Arguments: `{"path":"/tmp/x.txt"}`,
		}
	}
	return req
}

// acpPermBridge 注入 push 捕获并按应答脚本回流的测试桥。
// replies 为 jsonrpc_id → 响应 map；发送即触发回流。
type acpPermBridge struct {
	mu      sync.Mutex
	pushed  []map[string]any
	replies map[string]map[string]any
}

// push 捕获推送并立即回流（模拟 IDE 即时应答）。
func (b *acpPermBridge) push(ctx context.Context, msg map[string]any) error {
	b.mu.Lock()
	b.pushed = append(b.pushed, copyPermMsg(msg))
	id, _ := msg["jsonrpc_id"].(string)
	reply := b.replies[id]
	b.mu.Unlock()
	if reply != nil && id != "" {
		swarmtools.GetAcpOutputManager().CompleteJsonrpcResponse(id, reply)
	}
	return nil
}

// lastPushed 返回最后一条推送。
func (b *acpPermBridge) lastPushed() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pushed) == 0 {
		return nil
	}
	return b.pushed[len(b.pushed)-1]
}

// copyPermMsg 浅拷贝 map。
func copyPermMsg(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// setupAcpPermBridge 装配测试桥并注册清理。
func setupAcpPermBridge(t *testing.T, replies map[string]map[string]any) *acpPermBridge {
	t.Helper()
	mgr := swarmtools.GetAcpOutputManager()
	mgr.ResetState()
	bridge := &acpPermBridge{replies: replies}
	mgr.SetSendPushFunc(bridge.push)
	t.Cleanup(func() {
		mgr.ResetState()
		mgr.SetSendPushFunc(nil)
	})
	return bridge
}

// permOutcome 组装 selected/cancelled outcome 响应。
func permOutcome(outcome, optionID string) map[string]any {
	inner := map[string]any{"outcome": outcome}
	if optionID != "" {
		inner["optionId"] = optionID
	}
	return map[string]any{"result": map[string]any{"outcome": inner}}
}

// permError 组装错误响应。
func permError(message string) map[string]any {
	return map[string]any{"error": map[string]any{"code": -32000, "message": message}}
}

// ──────────────────── ACP 分支测试 ────────────────────

// TestRequestPermissionConfirmation_ACP推送结构 验证推送的 JSON-RPC 结构：
// method=session/request_permission、toolCall descriptor、三选项。
func TestRequestPermissionConfirmation_ACP推送结构(t *testing.T) {
	bridge := setupAcpPermBridge(t, map[string]map[string]any{
		"1": permOutcome("selected", "allow-once"),
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-1", "write_text_file"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || !resp.Approved {
		t.Fatalf("allow-once 应批准, resp=%+v", resp)
	}

	pushed := bridge.lastPushed()
	if pushed == nil {
		t.Fatal("应有推送")
	}
	// payload 层校验
	payload, _ := pushed["payload"].(map[string]any)
	if payload == nil {
		payload, _ = pushed["body"].(map[string]any)
	}
	if payload == nil {
		t.Fatalf("推送缺 payload: %v", pushed)
	}
	if payload["method"] != "session/request_permission" {
		t.Errorf("method = %v", payload["method"])
	}
	params, _ := payload["params"].(map[string]any)
	if params == nil {
		t.Fatalf("缺 params: %v", payload)
	}
	toolCall, _ := params["toolCall"].(map[string]any)
	if toolCall == nil {
		t.Fatalf("缺 toolCall: %v", params)
	}
	if toolCall["toolCallId"] != "tc-1" {
		t.Errorf("toolCallId = %v", toolCall["toolCallId"])
	}
	if toolCall["status"] != "pending" {
		t.Errorf("status = %v", toolCall["status"])
	}
	if toolCall["kind"] != "other" {
		t.Errorf("kind = %v", toolCall["kind"])
	}
	// 标题非空（descriptor 生成，如 "Editing ..."；空时才 "Approve `<name>`" 兜底）
	title, _ := toolCall["title"].(string)
	if title == "" {
		t.Errorf("title 为空")
	}
	// 三选项
	options, _ := params["options"].([]any)
	if len(options) != 3 {
		t.Fatalf("options = %v", options)
	}
}

// TestRequestPermissionConfirmation_ACP空toolCallID兜底 无 ID 时生成 permission_<name>。
func TestRequestPermissionConfirmation_ACP空toolCallID兜底(t *testing.T) {
	bridge := setupAcpPermBridge(t, map[string]map[string]any{
		"1": permOutcome("selected", "allow-once"),
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || !resp.Approved {
		t.Fatalf("resp=%+v", resp)
	}
	pushed := bridge.lastPushed()
	payload, _ := pushed["payload"].(map[string]any)
	params, _ := payload["params"].(map[string]any)
	toolCall, _ := params["toolCall"].(map[string]any)
	if toolCall["toolCallId"] != "permission_bash" {
		t.Errorf("toolCallId = %v", toolCall["toolCallId"])
	}
}

// TestRequestPermissionConfirmation_ACP无ToolCall ToolCall 为 nil 不 panic。
func TestRequestPermissionConfirmation_ACP无ToolCall(t *testing.T) {
	bridge := setupAcpPermBridge(t, map[string]map[string]any{
		"1": permOutcome("cancelled", ""),
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "", ""))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || resp.Approved {
		t.Fatalf("cancelled 应不批准, resp=%+v", resp)
	}
	if !strings.Contains(resp.Feedback, "[PERMISSION_REJECTED]") {
		t.Errorf("feedback = %q", resp.Feedback)
	}
	pushed := bridge.lastPushed()
	payload, _ := pushed["payload"].(map[string]any)
	params, _ := payload["params"].(map[string]any)
	toolCall, _ := params["toolCall"].(map[string]any)
	if toolCall["toolCallId"] != "permission_tool" {
		t.Errorf("toolCallId = %v", toolCall["toolCallId"])
	}
}

// TestRequestPermissionConfirmation_ACP允许一次 selected+allow-once → 批准不自动。
func TestRequestPermissionConfirmation_ACP允许一次(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": permOutcome("selected", "allow-once"),
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-2", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || !resp.Approved || resp.AutoConfirm {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Feedback != "" {
		t.Errorf("feedback 应空, got %q", resp.Feedback)
	}
}

// TestRequestPermissionConfirmation_ACP总是允许 selected+allow-always → 批准+自动。
func TestRequestPermissionConfirmation_ACP总是允许(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": permOutcome("selected", "allow-always"),
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-3", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || !resp.Approved || !resp.AutoConfirm {
		t.Fatalf("resp=%+v", resp)
	}
}

// TestRequestPermissionConfirmation_ACP拒绝 selected+其他选项 → 拒绝。
func TestRequestPermissionConfirmation_ACP拒绝(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": permOutcome("selected", "reject-once"),
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-4", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || resp.Approved {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Feedback != "[PERMISSION_REJECTED] User rejected the request." {
		t.Errorf("feedback = %q", resp.Feedback)
	}
}

// TestRequestPermissionConfirmation_ACP取消 cancelled → 拒绝文案。
func TestRequestPermissionConfirmation_ACP取消(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": permOutcome("cancelled", ""),
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-5", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || resp.Approved {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Feedback != "[PERMISSION_REJECTED] Permission request was cancelled." {
		t.Errorf("feedback = %q", resp.Feedback)
	}
}

// TestRequestPermissionConfirmation_ACP错误响应 error 响应 → 拒绝并带消息。
func TestRequestPermissionConfirmation_ACP错误响应(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": permError("boom"),
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-6", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || resp.Approved {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Feedback != "[PERMISSION_DENIED] boom" {
		t.Errorf("feedback = %q", resp.Feedback)
	}
}

// TestRequestPermissionConfirmation_ACP错误响应无消息 error 无 message → 默认文案。
func TestRequestPermissionConfirmation_ACP错误响应无消息(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": {"error": map[string]any{"code": -32000}},
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-7", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || resp.Approved {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Feedback != "[PERMISSION_DENIED] Permission request failed" {
		t.Errorf("feedback = %q", resp.Feedback)
	}
}

// TestRequestPermissionConfirmation_ACP未知outcome 未知 outcome → nil。
func TestRequestPermissionConfirmation_ACP未知outcome(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": {"result": map[string]any{"outcome": map[string]any{"outcome": "weird"}}},
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-8", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp != nil {
		t.Fatalf("未知 outcome 应 nil, got %+v", resp)
	}
}

// TestRequestPermissionConfirmation_ACP空结果 无 result → nil。
func TestRequestPermissionConfirmation_ACP空结果(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": {"result": "not-a-map"},
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-9", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp != nil {
		t.Fatalf("非 dict result 应 nil, got %+v", resp)
	}
}

// TestRequestPermissionConfirmation_ACP发送失败 push 未注入 → 发送错 → nil。
func TestRequestPermissionConfirmation_ACP发送失败(t *testing.T) {
	mgr := swarmtools.GetAcpOutputManager()
	mgr.ResetState()
	mgr.SetSendPushFunc(nil)
	t.Cleanup(mgr.ResetState)
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-10", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp != nil {
		t.Fatalf("发送失败应 nil, got %+v", resp)
	}
}

// TestRequestPermissionConfirmation_ACP带Reason 判定结果带 reason 时追加到标题。
func TestRequestPermissionConfirmation_ACP带Reason(t *testing.T) {
	bridge := setupAcpPermBridge(t, map[string]map[string]any{
		"1": permOutcome("selected", "allow-once"),
	})
	d := NewDeepAdapter()
	req := newAcpPermRequest(t, "tc-11", "write_text_file")
	req.Result = &harnesssecurity.PermissionResult{Reason: "external path"}
	resp, err := d.requestPermissionConfirmation(req)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || !resp.Approved {
		t.Fatalf("resp=%+v", resp)
	}
	pushed := bridge.lastPushed()
	payload, _ := pushed["payload"].(map[string]any)
	params, _ := payload["params"].(map[string]any)
	toolCall, _ := params["toolCall"].(map[string]any)
	title, _ := toolCall["title"].(string)
	if !strings.Contains(title, "external path") {
		t.Errorf("title 应含 reason, got %q", title)
	}
}

// TestRequestPermissionConfirmation_大小写归一 outcome/optionId 大小写混合也能识别。
func TestRequestPermissionConfirmation_大小写归一(t *testing.T) {
	setupAcpPermBridge(t, map[string]map[string]any{
		"1": {"result": map[string]any{"outcome": map[string]any{
			"outcome": "SELECTED", "optionId": "Allow-Once",
		}}},
	})
	d := NewDeepAdapter()
	resp, err := d.requestPermissionConfirmation(newAcpPermRequest(t, "tc-12", "bash"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil || !resp.Approved {
		t.Fatalf("resp=%+v", resp)
	}
}

// TestResolvePermissionSessionID 会话 ID 解析。
func TestResolvePermissionSessionID(t *testing.T) {
	// nil ctx 空上下文返回空串
	if got := resolvePermissionSessionID(nil); got != "" {
		t.Errorf("nil ctx = %q", got)
	}
	// 带 session
	cbCtx := sainterfaces.NewAgentCallbackContext(nil, nil, &acpPermSession{})
	if got := resolvePermissionSessionID(cbCtx); got != "sess-acp-perm" {
		t.Errorf("session = %q", got)
	}
	// 无 session
	cbCtx2 := sainterfaces.NewAgentCallbackContext(nil, nil, nil)
	if got := resolvePermissionSessionID(cbCtx2); got != "" {
		t.Errorf("无 session = %q", got)
	}
}
