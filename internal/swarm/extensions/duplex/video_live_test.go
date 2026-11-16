package duplex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeChannel 测试桩：登记方法与响应。
type fakeChannel struct {
	methods   map[string]DuplexMethodHandler
	mu        sync.Mutex
	responses []*DuplexResponse
	events    []string
}

func newFakeChannel() *fakeChannel { return &fakeChannel{methods: map[string]DuplexMethodHandler{}} }

func (f *fakeChannel) SendResponse(_ context.Context, _ any, _ string, resp *DuplexResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, resp)
}

func (f *fakeChannel) SendEvent(_ context.Context, _ any, event string, _ map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *fakeChannel) SendEventChecked(_ context.Context, _ any, event string, _ map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return nil
}

func (f *fakeChannel) RegisterMethod(method string, handler DuplexMethodHandler, _, _ bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.methods[method] = handler
}

// TestAppendJSONL_并发单行性 对齐 video_live.py:71-78。
func TestAppendJSONL_并发单行性(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			AppendJSONL(path, map[string]any{"n": n, "text": "行" + strings.Repeat("x", 10)})
		}(i)
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 50 {
		t.Fatalf("应 50 行, got %d", len(lines))
	}
	for _, line := range lines {
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) != nil {
			t.Fatalf("行应为完整 JSON: %s", line)
		}
	}
}

// TestTelemetryEvent白名单 对齐 video_live.py:60-69（字段数机器核对）。
func TestTelemetryEvent白名单(t *testing.T) {
	// 字段数：Python 集合机器核对 70 项（re 提取 video_live.py:63-69，无重复）
	if len(realtimeTelemetryFields) != 70 {
		t.Fatalf("遥测字段白名单应 70 项（机器核对）, got %d", len(realtimeTelemetryFields))
	}
	if !realtimeTelemetryEvent.MatchString("realtime_speech_start") ||
		!realtimeTelemetryEvent.MatchString("qwen_realtime_error") ||
		!realtimeTelemetryEvent.MatchString("barge_in_detected") {
		t.Fatal("合法事件名应放行")
	}
	if realtimeTelemetryEvent.MatchString("evil_event") ||
		realtimeTelemetryEvent.MatchString("realtime_UPPER") {
		t.Fatal("非白名单前缀/大写应拒")
	}
}

// TestVideoLiveMode_VoiceChannel_Enabled 对齐 video_live.py:170-186。
func TestVideoLiveMode_VoiceChannel_Enabled(t *testing.T) {
	t.Setenv("VIDEO_LIVE_MODE", "realtime")
	if videoLiveMode() != "realtime" {
		t.Fatal("realtime 模式")
	}
	t.Setenv("VIDEO_LIVE_MODE", "joyai")
	if videoLiveMode() != "joyai" {
		t.Fatal("joyai 模式")
	}
	if usesJoyaiVoiceChannel() {
		t.Fatal("joyai+默认 provider 应为 false")
	}
	t.Setenv("JOYAI_VOICE_PROVIDER", "native")
	if !usesJoyaiVoiceChannel() {
		t.Fatal("非 openai 族 provider 应走原生通道")
	}
	t.Setenv("VIDEO_DUPLEX_ENABLED", "false")
	if VideoDuplexEnabled() {
		t.Fatal("false 应禁用")
	}
	t.Setenv("VIDEO_DUPLEX_ENABLED", "0")
	if VideoDuplexEnabled() {
		t.Fatal("0 应禁用")
	}
	os.Unsetenv("VIDEO_DUPLEX_ENABLED")
	if !VideoDuplexEnabled() {
		t.Fatal("缺省应启用")
	}
}

// TestValidateJoyaiResult_AllowedImageDataURL 对齐 video_live.py:152-168。
func TestValidateJoyaiResult_AllowedImageDataURL(t *testing.T) {
	if err := validateJoyaiResult(map[string]any{
		"decision": "response", "response": "文本", "delegation": "",
	}); err != nil {
		t.Fatalf("合法结果不应报错: %v", err)
	}
	if err := validateJoyaiResult(map[string]any{"decision": "bogus"}); err == nil {
		t.Fatal("非法 decision 应拒")
	}
	if err := validateJoyaiResult(nil); err == nil {
		t.Fatal("nil 应拒")
	}
	if !isAllowedImageDataURL("data:image/png;base64,AA") {
		t.Fatal("png 应放行")
	}
	if isAllowedImageDataURL("data:image/gif;base64,AA") {
		t.Fatal("gif 应拒")
	}
}

// TestRegisterVideoLiveHandler_RPC路由 对齐 video_live.py:674-690。
func TestRegisterVideoLiveHandler_RPC路由(t *testing.T) {
	channel := newFakeChannel()
	registerVideoLiveHandler(channel, registerOptions{})
	expected := []string{
		"video.realtime.config", "video.joyai.frame", "video.realtime.telemetry",
		"video.conversation.append", "video.transcribe", "tts.synthesize",
		"tts.stream.start", "tts.stream.cancel",
		"video.qwen.tool", "video.search.status", "video.search.answer",
		"video.search.list", "video.search.control",
	}
	for _, method := range expected {
		if _, ok := channel.methods[method]; !ok {
			t.Fatalf("缺 RPC %s", method)
		}
	}
}

// TestRealtimeConfigRPC_两分支 对齐 video_live.py:213-248。
func TestRealtimeConfigRPC_两分支(t *testing.T) {
	channel := newFakeChannel()
	registerVideoLiveHandler(channel, registerOptions{})
	handler := channel.methods["video.realtime.config"]

	// joyai 缺配置
	t.Setenv("VIDEO_LIVE_MODE", "joyai")
	os.Unsetenv("JOYAI_API_BASE")
	os.Unsetenv("JOYAI_MODEL_NAME")
	resp := handler(context.Background(), nil, "r1", nil, "")
	if resp.OK || resp.Code != "VIDEO_CONFIG_ERROR" ||
		resp.Error != "请配置 JOYAI_API_BASE 和 JOYAI_MODEL_NAME" {
		t.Fatalf("joyai 缺配置: %v", resp)
	}
	// joyai 有配置
	t.Setenv("JOYAI_API_BASE", "https://joyai.example")
	t.Setenv("JOYAI_MODEL_NAME", "joyai-large")
	resp = handler(context.Background(), nil, "r2", nil, "")
	if !resp.OK || resp.Payload["provider"] != "joyai" || resp.Payload["model"] != "joyai-large" {
		t.Fatalf("joyai 配置: %v", resp)
	}
	// qwen_omni
	t.Setenv("VIDEO_LIVE_MODE", "realtime")
	t.Setenv("QWEN_OMNI_REALTIME_URL", "wss://api.example")
	t.Setenv("QWEN_OMNI_API_KEY", "k")
	t.Setenv("QWEN_OMNI_MODEL_NAME", "omni")
	resp = handler(context.Background(), nil, "r3", nil, "")
	if !resp.OK || resp.Payload["provider"] != "qwen_omni" || resp.Payload["url"] != QwenOmniProxyPath {
		t.Fatalf("qwen 配置: %v", resp)
	}
	if _, ok := resp.Payload["tools"].([]map[string]any); !ok {
		t.Fatal("tools 应随配置下发")
	}
}

// TestJoyaiFrameHandler_校验链 对齐 video_live.py:279-328。
func TestJoyaiFrameHandler_校验链(t *testing.T) {
	channel := newFakeChannel()
	registerVideoLiveHandler(channel, registerOptions{})
	handler := channel.methods["video.joyai.frame"]
	call := func(params map[string]any) *DuplexResponse {
		return handler(context.Background(), nil, "r1", params, "")
	}
	// 非 dataURL 帧
	resp := call(map[string]any{"frame_data_url": "nope", "instruction": "看"})
	if resp.OK || resp.Error != "frame_data_url must be a base64 JPEG, PNG, or WebP data URL" {
		t.Fatalf("非法帧: %v", resp)
	}
	// request_kind 非法
	resp = call(map[string]any{"frame_data_url": "data:image/png;base64,AA", "instruction": "看", "request_kind": "other"})
	if resp.OK || resp.Error != "request_kind is invalid" {
		t.Fatalf("request_kind: %v", resp)
	}
	// tool_context 仅限 user 轮
	resp = call(map[string]any{"frame_data_url": "data:image/png;base64,AA", "instruction": "看", "tool_context": "结果"})
	if resp.OK || resp.Error != "tool_context is allowed only for user requests" {
		t.Fatalf("tool_context: %v", resp)
	}
	// 帧时间范围格式
	resp = call(map[string]any{"frame_data_url": "data:image/png;base64,AA", "instruction": "看", "frame_time_range": "1s ~ 2s"})
	if resp.OK || resp.Error != "frame_time_range is invalid" {
		t.Fatalf("frame_time_range: %v", resp)
	}
	// 合法格式 + joyai 缺配置 → JOYAI_ERROR（模型端点缺失）
	t.Setenv("JOYAI_API_BASE", "https://joyai.example")
	t.Setenv("JOYAI_MODEL_NAME", "joyai-large")
	resp = call(map[string]any{
		"frame_data_url": "data:image/png;base64,AA", "instruction": "看",
		"frame_time_range": "1.5 seconds ~ 3.0 seconds",
	})
	if resp.OK || resp.Code != "JOYAI_ERROR" {
		t.Fatalf("joyai 请求失败应报 JOYAI_ERROR: %v", resp)
	}
}

// TestRealtimeTelemetryHandler_裁剪与路由 对齐 video_live.py:497-536。
func TestRealtimeTelemetryHandler_裁剪与路由(t *testing.T) {
	resp := realtimeTelemetryHandler(map[string]any{"event": "unsupported!"})
	if resp.OK || resp.Error != "unsupported telemetry event" {
		t.Fatalf("非法事件名: %v", resp)
	}
	longQuestion := strings.Repeat("问", 1500)
	resp = realtimeTelemetryHandler(map[string]any{
		"event": "realtime_speech_start", "question": longQuestion,
		"unknown_field": "dropped",
	})
	if !resp.OK {
		t.Fatalf("合法事件应受理: %v", resp)
	}
	resp = realtimeTelemetryHandler(map[string]any{
		"event": "search_result_received", "result": strings.Repeat("果", 60000),
	})
	if !resp.OK {
		t.Fatal("staged 事件应受理")
	}
}

// TestAppendConversationHandler_校验 对齐 video_live.py:538-676 的校验分支。
func TestAppendConversationHandler_校验(t *testing.T) {
	channel := newFakeChannel()
	handler := func(params map[string]any) *DuplexResponse {
		return appendConversationHandler(context.Background(), nil, "r1", params, channel)
	}
	// 未持久化会话
	resp := handler(map[string]any{"session_id": "new", "event_id": "e1", "kind": "user", "content": "x"})
	if resp.OK || resp.Code != "SESSION_NOT_READY" {
		t.Fatalf("new 会话应拒: %v", resp)
	}
	// 空/超长 event_id
	resp = handler(map[string]any{"session_id": "s1", "event_id": "", "kind": "user", "content": "x"})
	if resp.OK || resp.Code != "INVALID_EVENT_ID" {
		t.Fatalf("空 event_id: %v", resp)
	}
	// 非法 kind
	resp = handler(map[string]any{"session_id": "s1", "event_id": "e1", "kind": "bogus", "content": "x"})
	if resp.OK || resp.Code != "INVALID_EVENT_KIND" {
		t.Fatalf("非法 kind: %v", resp)
	}
	// 超长 content
	resp = handler(map[string]any{"session_id": "s1", "event_id": "e1", "kind": "user", "content": strings.Repeat("长", 100001)})
	if resp.OK || resp.Code != "CONTENT_TOO_LARGE" {
		t.Fatalf("超长 content: %v", resp)
	}
	// tool_call 非对象
	resp = handler(map[string]any{"session_id": "s1", "event_id": "e1", "kind": "tool_call", "content": ""})
	if resp.OK || resp.Code != "INVALID_TOOL_CALL" {
		t.Fatalf("tool_call 非对象: %v", resp)
	}
	// file 空 files
	resp = handler(map[string]any{"session_id": "s1", "event_id": "e1", "kind": "file", "content": ""})
	if resp.OK || resp.Code != "INVALID_FILES" {
		t.Fatalf("空 files: %v", resp)
	}
	// 历史桥未注入 → 直接成功返回（持久化跳过）
	conversationHistory = nil
	resp = handler(map[string]any{"session_id": "s1", "event_id": "e1", "kind": "user", "content": "你好"})
	if !resp.OK || resp.Payload["persisted"] != true {
		t.Fatalf("合法事件应受理: %v", resp)
	}
}
