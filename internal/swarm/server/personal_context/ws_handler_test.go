package personalcontext

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// fakeWireSender 记录 wire 帧的测试发送面
type fakeWireSender struct {
	frames [][]byte
	closed bool
}

// SendWire 记录帧
func (f *fakeWireSender) SendWire(data []byte) bool {
	if f.closed {
		return false
	}
	f.frames = append(f.frames, data)
	return true
}

// newTestRequest 构造 AgentRequest（params 经 RawMessage）
func newTestRequest(method schema.ReqMethod, params map[string]any, isStream bool) *schema.AgentRequest {
	raw, _ := json.Marshal(params)
	opts := []schema.AgentRequestOption{schema.WithAgentIsStream(isStream)}
	return schema.NewAgentRequest("req-1", "web", method, raw, opts...)
}

// decodePayload 解析帧 payload（区分 response/chunk 形态）
func decodePayload(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("wire 解析失败: %v", err)
	}
	// e2a wire 形态（D-24.3-7）：
	//   成功响应 body.result；流式 chunk 业务字段在 body.delta；错误响应 body{details{code,error,status},message}
	if body, ok := wire["body"].(map[string]any); ok {
		if result, ok := body["result"].(map[string]any); ok {
			return result
		}
		if delta, ok := body["delta"].(map[string]any); ok {
			return delta
		}
		if _, hasCode := body["code"]; hasCode {
			flat := map[string]any{}
			if details, ok := body["details"].(map[string]any); ok {
				for k, v := range details {
					flat[k] = v
				}
			}
			flat["message"] = body["message"]
			return flat
		}
	}
	if payload, ok := wire["payload"].(map[string]any); ok {
		return payload
	}
	return wire
}

// IsPersonalContextMethod 集合恰好 26 成员（Python PERSONAL_CONTEXT_REQUEST_METHODS frozenset）
func TestPersonalContextRequestMethods数量(t *testing.T) {
	if len(personalContextRequestMethods) != 26 {
		t.Errorf("集合成员 = %d, want 26", len(personalContextRequestMethods))
	}
	if !IsPersonalContextMethod(schema.ReqMethodPersonalContextRuntimeStatus) {
		t.Errorf("runtime.status 应在集合")
	}
	if IsPersonalContextMethod(schema.ReqMethodChatSend) {
		t.Errorf("chat.send 不应在集合")
	}
}

// 非流式成功路径：runtime_status 空态返回（ws_handler.py:235-236 + _send_result）
func TestHandleRequest_runtimeStatus空态(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextRuntimeStatus, map[string]any{}, false)
	resp, err := HandlePersonalContextRequest(h, sender, req, nil)
	if err != nil || resp != nil {
		t.Fatalf("Go 适配：响应经 sender 发送，应返回 (nil, nil): %v %v", resp, err)
	}
	if len(sender.frames) != 1 {
		t.Fatalf("应恰好 1 帧: %d", len(sender.frames))
	}
	payload := decodePayload(t, sender.frames[0])
	if payload["state"] != "CREATED" {
		t.Errorf("空态 state 应 CREATED: %v", payload)
	}
}

// set_master_enabled 成功路径 + enabled 非布尔拒绝（ws_handler.py:249-255）
func TestHandleRequest_setMasterEnabled(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextRuntimeSetMasterEnabled, map[string]any{"enabled": true}, false)
	if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
		t.Fatal(err)
	}
	payload := decodePayload(t, sender.frames[0])
	if payload["master_enabled"] != true {
		t.Errorf("应返回投影: %v", payload)
	}
	// enabled 非布尔 → BAD_REQUEST
	sender2 := &fakeWireSender{}
	req2 := newTestRequest(schema.ReqMethodPersonalContextRuntimeSetMasterEnabled, map[string]any{"enabled": "yes"}, false)
	if _, err := HandlePersonalContextRequest(h, sender2, req2, nil); err != nil {
		t.Fatal(err)
	}
	errPayload := decodePayload(t, sender2.frames[0])
	if errPayload["code"] != "BAD_REQUEST" {
		t.Errorf("校验错应 BAD_REQUEST: %v", errPayload)
	}
	if !strings.Contains(errPayload["error"].(string), "请求参数无效") {
		t.Errorf("错误应本地化（parameter_invalid）: %v", errPayload["error"])
	}
}

// runtime_enabled_changed 回调：start/stop agent_use 与 set_master 触发（ws_handler.py:241-255）
func TestHandleRequest_runtimeEnabledChanged回调(t *testing.T) {
	h := newTestHost(t)
	if _, err := h.SetMasterEnabled(true); err != nil {
		t.Fatal(err)
	}
	var notified []bool
	callback := func(enabled bool) error {
		notified = append(notified, enabled)
		return nil
	}
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextRuntimeStartAgentUse, map[string]any{}, false)
	if _, err := HandlePersonalContextRequest(h, sender, req, callback); err != nil {
		t.Fatal(err)
	}
	if len(notified) != 1 || notified[0] != true {
		t.Errorf("start_agent_use 应通知 true: %v", notified)
	}
	sender2 := &fakeWireSender{}
	req2 := newTestRequest(schema.ReqMethodPersonalContextRuntimeSetMasterEnabled, map[string]any{"enabled": false}, false)
	if _, err := HandlePersonalContextRequest(h, sender2, req2, callback); err != nil {
		t.Fatal(err)
	}
	if len(notified) != 2 || notified[1] != false {
		t.Errorf("set_master false 应通知 false: %v", notified)
	}
}

// fetch_list_services 空态返回空列表（ws_handler.py:264-265）
func TestHandleRequest_listServices空态(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextFetchListServices, map[string]any{}, false)
	if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
		t.Fatal(err)
	}
	payload := decodePayload(t, sender.frames[0])
	services, ok := payload["services"].([]any)
	if !ok || len(services) != 0 {
		t.Errorf("services 应空列表: %v", payload)
	}
}

// delete_service 缺 service_id 拒绝（ws_handler.py:271-273 + _text）
func TestHandleRequest_deleteService缺参(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextFetchDeleteService, map[string]any{}, false)
	if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
		t.Fatal(err)
	}
	payload := decodePayload(t, sender.frames[0])
	if payload["code"] != "BAD_REQUEST" {
		t.Errorf("缺参应 BAD_REQUEST: %v", payload)
	}
}

// HostError 路径：run_one 未配置 → code=154001 + status（ws_handler.py:394-403）
func TestHandleRequest_HostError线格式(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextFetchRunOne, map[string]any{"service_id": "s1"}, false)
	if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
		t.Fatal(err)
	}
	payload := decodePayload(t, sender.frames[0])
	if payload["code"] != "154001" || payload["status"] != "CONTEXT_PROACTIVE_STATE_INVALID" {
		t.Errorf("HostError 应带 code+status: %v", payload)
	}
	if payload["error"] != messages["not_configured"][0] {
		t.Errorf("错误应按 not_configured reason 本地化: %v", payload["error"])
	}
}

// stream_graph 空态四事件帧序：start → nodes([]) → edges([]) → end（ws_handler.py:157-224）
func TestStreamGraph空态四事件帧序(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextContextStreamGraph, map[string]any{"depth": 3}, true)
	if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
		t.Fatal(err)
	}
	// Python for start in range(0, len(nodes), 200)：空列表不产 nodes/edges 帧 → 仅 start+end 两帧
	if len(sender.frames) != 2 {
		t.Fatalf("空态应 2 帧（start+end）: %d", len(sender.frames))
	}
	types := make([]string, 0, len(sender.frames))
	for _, frame := range sender.frames {
		payload := decodePayload(t, frame)
		if et, ok := payload["event_type"].(string); ok {
			types = append(types, et)
		} else {
			types = append(types, "<none>")
		}
	}
	want := []string{"personal_context.context.start", "personal_context.context.end"}
	for i, w := range want {
		if types[i] != w {
			t.Errorf("帧 %d = %s, want %s", i, types[i], w)
		}
	}
	start := decodePayload(t, sender.frames[0])
	if start["context_ready"] != false || mustInt(t, start["depth"]) != 3 {
		t.Errorf("start 帧字段不符: %v", start)
	}
	end := decodePayload(t, sender.frames[1])
	if mustInt(t, end["node_count"]) != 0 || mustInt(t, end["edge_count"]) != 0 {
		t.Errorf("end 帧计数应为 0: %v", end)
	}
}

// stream_graph 非流式请求拒绝（ws_handler.py:359-360）
func TestStreamGraph_非流式拒绝(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextContextStreamGraph, map[string]any{}, false)
	if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
		t.Fatal(err)
	}
	payload := decodePayload(t, sender.frames[0])
	// Python ws_handler.py:390：ValueError 统一按 parameter_invalid 本地化
	if payload["code"] != "BAD_REQUEST" || payload["error"] != messages["parameter_invalid"][0] {
		t.Errorf("非流式应拒绝: %v", payload)
	}
}

// stream_graph depth 边界（ws_handler.py:168-170）
func TestStreamGraph_depth边界(t *testing.T) {
	h := newTestHost(t)
	for _, depth := range []any{0, 11, "3"} {
		sender := &fakeWireSender{}
		req := newTestRequest(schema.ReqMethodPersonalContextContextStreamGraph, map[string]any{"depth": depth}, true)
		if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
			t.Fatal(err)
		}
		payload := decodePayload(t, sender.frames[0])
		// Python ws_handler.py:390：ValueError 统一按 parameter_invalid 本地化
		if payload["code"] != "BAD_REQUEST" || payload["error"] != messages["parameter_invalid"][0] {
			t.Errorf("depth=%v 应拒绝: %v", depth, payload)
		}
	}
}

// 未知 method 拒绝（ws_handler.py:319）
func TestHandleRequest_未知method(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest("personal_context.bogus", map[string]any{}, false)
	if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
		t.Fatal(err)
	}
	payload := decodePayload(t, sender.frames[0])
	if payload["code"] != "BAD_REQUEST" {
		t.Errorf("未知 method 应 BAD_REQUEST: %v", payload)
	}
}

// 授权空态经分发面（authorize_provider 表单校验 + D-24.3-4 空态）
func TestHandleRequest_authorize空态(t *testing.T) {
	h := newTestHost(t)
	sender := &fakeWireSender{}
	req := newTestRequest(schema.ReqMethodPersonalContextFetchAuthorizeProvider,
		map[string]any{"provider": "github", "credentials": map[string]any{"token": "tok"}}, false)
	if _, err := HandlePersonalContextRequest(h, sender, req, nil); err != nil {
		t.Fatal(err)
	}
	payload := decodePayload(t, sender.frames[0])
	if payload["state"] != "not_authorized" {
		t.Errorf("授权空态应 not_authorized: %v", payload)
	}
}

// mustInt 数值断言辅助（JSON 往返后 int→float64）
func mustInt(t *testing.T, value any) int {
	t.Helper()
	v, ok := toInt(value)
	if !ok {
		t.Fatalf("非数值: %v", value)
	}
	return v
}
