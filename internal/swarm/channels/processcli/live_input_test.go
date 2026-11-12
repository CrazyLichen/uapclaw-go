package processcli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEncodeForwardedReceipt_前缀 回执带固定前缀与紧凑 JSON。
// Python: live_input.py:60-67
func TestEncodeForwardedReceipt_前缀(t *testing.T) {
	got := EncodeForwardedReceipt(LiveInputReceipt{Status: "accepted", Message: "ok"})
	if !strings.HasPrefix(got, ForwardedReceiptPrefix) {
		t.Errorf("缺前缀：%q", got)
	}
	payload := strings.TrimPrefix(got, ForwardedReceiptPrefix)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("JSON 非法：%v", err)
	}
	if decoded["status"] != "accepted" {
		t.Errorf("status = %v", decoded["status"])
	}
	if decoded["message"] != "ok" {
		t.Errorf("message = %v", decoded["message"])
	}
}

// TestParseForwardedReceipt_往返 编码后可解回。
func TestParseForwardedReceipt_往返(t *testing.T) {
	receipt := LiveInputReceipt{Status: "rejected", Message: "目标已变"}
	parsed := ParseForwardedReceipt(EncodeForwardedReceipt(receipt))
	if parsed == nil || parsed.Status != "rejected" || parsed.Message != "目标已变" {
		t.Errorf("往返失败：%+v", parsed)
	}
}

// TestParseForwardedReceipt_非回执 无前缀或 JSON 非法返回 nil。
func TestParseForwardedReceipt_非回执(t *testing.T) {
	if ParseForwardedReceipt("plain log line") != nil {
		t.Error("无前缀应返回 nil")
	}
	if ParseForwardedReceipt(ForwardedReceiptPrefix+"not-json") != nil {
		t.Error("非法 JSON 应返回 nil")
	}
}

// TestSupportsLiveSessionInput_四条件 逐条件否定。
// Python: live_input.py:103-120
func TestSupportsLiveSessionInput_四条件(t *testing.T) {
	// 条件 1：output 必须 human
	if SupportsLiveSessionInput(&AppArgs{Output: "json", Operation: "chat"}, nil, nil) {
		t.Error("output=json 应不支持")
	}
	// 条件 2：operation 必须 chat
	if SupportsLiveSessionInput(&AppArgs{
		Output: OutputHuman, Operation: "session.create", ForwardedLiveInput: true,
	}, nil, nil) {
		t.Error("非 chat 操作应不支持")
	}
	// 条件 3：非 forwarded 且无 TTY 应不支持
	if SupportsLiveSessionInput(&AppArgs{Output: OutputHuman, Operation: "chat"}, nil, nil) {
		t.Error("无 TTY 且非 forwarded 应不支持")
	}
	// 条件 4：plan 模式不支持
	if SupportsLiveSessionInput(&AppArgs{
		Output: OutputHuman, Operation: "chat", ForwardedLiveInput: true, Mode: "agent.code.plan",
	}, nil, nil) {
		t.Error("plan 模式应不支持")
	}
	// 条件 4：team 模式不支持
	if SupportsLiveSessionInput(&AppArgs{
		Output: OutputHuman, Operation: "chat", ForwardedLiveInput: true, Mode: "team.work.normal",
	}, nil, nil) {
		t.Error("team 模式应不支持")
	}
	// forwarded + 单 Agent 普通模式 → 支持
	if !SupportsLiveSessionInput(&AppArgs{
		Output: OutputHuman, Operation: "chat", ForwardedLiveInput: true, Mode: "agent.code.normal",
	}, nil, nil) {
		t.Error("forwarded + 单 Agent 应支持")
	}
	// 旧串 code.normal 经 deprecate 后也应支持
	if !SupportsLiveSessionInput(&AppArgs{
		Output: OutputHuman, Operation: "chat", ForwardedLiveInput: true, Mode: "code.normal",
	}, nil, nil) {
		t.Error("code.normal 应支持（deprecate 后为 agent.code.normal）")
	}
}

// TestClassifySteerError_五条标记 命中任一标记判 rejected。
// Python: live_input.py:44-50 + :138-148
func TestClassifySteerError_五条标记(t *testing.T) {
	for _, marker := range targetChangedMarkers {
		message := "error: " + marker + " for request x"
		if got := ClassifySteerError(message); got != supplementStatusRejected {
			t.Errorf("%q 应判 rejected，得 %q", marker, got)
		}
	}
	// 未命中判 unknown
	if got := ClassifySteerError("some other error"); got != supplementStatusUnknown {
		t.Errorf("未命中应判 unknown，得 %q", got)
	}
}

// TestSteerParams_七键 只筛出 7 个上下文键。
// Python: live_input.py:130-137
func TestSteerParams_七键(t *testing.T) {
	root := map[string]any{
		"mode":                         "agent.code.normal",
		"work_mode":                    "code",
		"cwd":                          "/tmp",
		"project_dir":                  "/tmp/p",
		"trusted_dirs":                 []string{"/tmp/t"},
		"supports_user_interaction":    true,
		"supports_permission_approval": true,
		// 下面两个不属 steer 上下文
		"query":      "hello",
		"request_id": "r1",
	}
	params := SteerParams(root)
	if len(params) != len(steerContextKeys) {
		t.Errorf("键数 = %d, 期望 %d：%v", len(params), len(steerContextKeys), params)
	}
	if _, ok := params["query"]; ok {
		t.Error("query 不应进入 steer 参数")
	}
	if params["cwd"] != "/tmp" {
		t.Errorf("cwd = %v", params["cwd"])
	}
}

// TestDeliveryTimeout 回执超时 5 秒。
func TestDeliveryTimeout(t *testing.T) {
	if got := DeliveryTimeout().Seconds(); got != deliveryTimeoutSeconds {
		t.Errorf("DeliveryTimeout = %v 秒, 期望 %v", got, deliveryTimeoutSeconds)
	}
}
