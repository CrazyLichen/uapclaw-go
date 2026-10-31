package a2a

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestConfigFromEnv_默认值 对齐 Python A2AChannelConfig 默认值 (L23-35)
func TestConfigFromEnv_默认值(t *testing.T) {
	cfg := ConfigFromEnv(func(string) string { return "" })

	if cfg.Enabled {
		t.Error("默认应关闭")
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 19100 {
		t.Errorf("Host/Port = %s/%d", cfg.Host, cfg.Port)
	}
	if cfg.RPCPath != "/a2a" {
		t.Errorf("RPCPath = %q", cfg.RPCPath)
	}
	if cfg.CardPath != "/.well-known/agent-card.json" {
		t.Errorf("CardPath = %q", cfg.CardPath)
	}
	if cfg.ExtendedCardPath != "/agent/authenticatedExtendedCard" {
		t.Errorf("ExtendedCardPath = %q", cfg.ExtendedCardPath)
	}
	if cfg.ProtocolVersion != "1.0.0" {
		t.Errorf("ProtocolVersion = %q", cfg.ProtocolVersion)
	}
	if cfg.ChannelID != "a2a" {
		t.Errorf("ChannelID = %q", cfg.ChannelID)
	}
	// 偏差 D9：品牌串映射 JiuwenClaw → UapClaw
	if cfg.AppName != "UapClaw Gateway A2A Server" ||
		cfg.AppDescription != "A2A ingress for UapClaw Gateway" ||
		cfg.AppVersion != "0.1.0" {
		t.Errorf("App* = %q / %q / %q", cfg.AppName, cfg.AppDescription, cfg.AppVersion)
	}
}

// TestConfigFromEnv_覆盖与修剪 覆盖全部 env，含空白值回落
func TestConfigFromEnv_覆盖与修剪(t *testing.T) {
	env := map[string]string{
		"A2A_SERVER_ENABLED":            "on",
		"A2A_SERVER_HOST":               "0.0.0.0",
		"A2A_SERVER_PORT":               "19200",
		"A2A_SERVER_PATH":               "/rpc",
		"A2A_SERVER_CARD_PATH":          "/card.json",
		"A2A_SERVER_EXTENDED_CARD_PATH": "/ext",
		"A2A_SERVER_PROTOCOL_VERSION":   "1.0.1",
		"A2A_SERVER_APP_NAME":           "  Custom  ",
		"A2A_SERVER_APP_DESCRIPTION":    "desc",
		"A2A_SERVER_APP_VERSION":        "9.9.9",
	}
	cfg := ConfigFromEnv(func(k string) string { return env[k] })

	if !cfg.Enabled {
		t.Error("on 应解析为启用")
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 19200 {
		t.Errorf("Host/Port = %s/%d", cfg.Host, cfg.Port)
	}
	if cfg.RPCPath != "/rpc" || cfg.CardPath != "/card.json" || cfg.ExtendedCardPath != "/ext" {
		t.Errorf("路径 = %q/%q/%q", cfg.RPCPath, cfg.CardPath, cfg.ExtendedCardPath)
	}
	if cfg.ProtocolVersion != "1.0.1" || cfg.AppVersion != "9.9.9" || cfg.AppDescription != "desc" {
		t.Errorf("版本/描述 = %q/%q/%q", cfg.ProtocolVersion, cfg.AppVersion, cfg.AppDescription)
	}
	if cfg.AppName != "Custom" {
		t.Errorf("AppName 未修剪: %q", cfg.AppName)
	}

	cfg2 := ConfigFromEnv(func(k string) string {
		if k == "A2A_SERVER_HOST" || k == "A2A_SERVER_PATH" {
			return "   "
		}
		return ""
	})
	if cfg2.Host != "127.0.0.1" || cfg2.RPCPath != "/a2a" {
		t.Errorf("空白值未回落: %q/%q", cfg2.Host, cfg2.RPCPath)
	}
}

// TestConfigFromEnv_非法端口回落 非法端口回默认并保持可用
func TestConfigFromEnv_非法端口回落(t *testing.T) {
	cfg := ConfigFromEnv(func(k string) string {
		if k == "A2A_SERVER_PORT" {
			return "not-a-number"
		}
		return ""
	})
	if cfg.Port != 19100 {
		t.Errorf("非法端口应回落 19100, 实际 %d", cfg.Port)
	}
}

// TestConfigFromEnv_启用取值集合 对齐 Python {"1","true","yes","on"}
func TestConfigFromEnv_启用取值集合(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "On"} {
		if !ConfigFromEnv(func(string) string { return v }).Enabled {
			t.Errorf("%q 应启用", v)
		}
	}
	for _, v := range []string{"0", "false", "", "no", "enable"} {
		if ConfigFromEnv(func(string) string { return v }).Enabled {
			t.Errorf("%q 不应启用", v)
		}
	}
}

// TestDispatchA2ARequest_无回调报错 对齐 Python L320-321
func TestDispatchA2ARequest_无回调报错(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	_, err := ch.DispatchA2ARequest(t.Context(), "req-1", "sess-1", "hi", nil, nil)
	if err == nil {
		t.Fatal("未设置回调应返回 error")
	}
	if !strings.Contains(err.Error(), "on_message callback") {
		t.Errorf("错误文案 = %v", err)
	}
}

// TestDispatchA2ARequest_字段映射 对齐 Python L311-343
func TestDispatchA2ARequest_字段映射(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	var seen *schema.Message
	ch.OnMessage(func(msg *schema.Message) bool { seen = msg; return true })

	_, err := ch.DispatchA2ARequest(t.Context(), "req-2", "sess-2", "hello",
		[]map[string]any{{"filename": "x.txt", "data": "aGVsbG8="}},
		map[string]any{"trace_id": "t-1"})
	if err != nil {
		t.Fatalf("DispatchA2ARequest 失败: %v", err)
	}
	if seen == nil {
		t.Fatal("回调未被调用")
	}
	if seen.ID != "req-2" || seen.SessionID != "sess-2" || seen.ChannelID != "a2a" {
		t.Errorf("msg = %+v", seen)
	}
	if seen.Type != schema.MessageTypeReq || seen.ReqMethod != schema.ReqMethodChatSend || !seen.IsStream || !seen.OK {
		t.Errorf("msg 类型/方法/流式/ok = %+v", seen)
	}
	var params map[string]any
	if err := json.Unmarshal(seen.Params, &params); err != nil {
		t.Fatalf("params 解析失败: %v", err)
	}
	if params["query"] != "hello" {
		t.Errorf("params.query = %v", params["query"])
	}
	if _, ok := params["files"]; !ok {
		t.Errorf("params 缺 files: %v", params)
	}
	if seen.Metadata["trace_id"] != "t-1" {
		t.Errorf("metadata = %v", seen.Metadata)
	}
}

// TestSend_命中与未命中 对齐 Python L305-309
func TestSend_命中与未命中(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ch.OnMessage(func(msg *schema.Message) bool { return true })

	pending, err := ch.DispatchA2ARequest(t.Context(), "req-3", "sess-3", "hi", nil, nil)
	if err != nil {
		t.Fatalf("dispatch 失败: %v", err)
	}

	if err := ch.Send(t.Context(), &schema.Message{ID: "other"}); err != nil {
		t.Errorf("未命中应无错误, 实际 %v", err)
	}

	out := &schema.Message{ID: "req-3", Payload: map[string]any{"content": "ok", "is_complete": true}}
	if err := ch.Send(t.Context(), out); err != nil {
		t.Errorf("Send 失败: %v", err)
	}
	select {
	case got := <-pending.C():
		if got.ID != "req-3" {
			t.Errorf("pending 收到 %q", got.ID)
		}
	default:
		t.Error("pending 未收到消息")
	}

	ch.ClearPendingRequest("req-3")
	if err := ch.Send(t.Context(), out); err != nil {
		t.Errorf("清理后 Send 应无错误, 实际 %v", err)
	}
}

// TestStop_唤醒 pending 对齐 Python L276-303
func TestStop_唤醒Pending(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ch.OnMessage(func(msg *schema.Message) bool { return true })
	pending, err := ch.DispatchA2ARequest(t.Context(), "req-4", "sess-4", "hi", nil, nil)
	if err != nil {
		t.Fatalf("dispatch 失败: %v", err)
	}

	if err := ch.Stop(t.Context()); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	select {
	case got := <-pending.C():
		if got.EventType != schema.EventTypeChatError {
			t.Errorf("唤醒消息 event_type = %q", got.EventType)
		}
		if got.Payload["error"] != shutdownWakeupError || got.Payload["is_complete"] != true {
			t.Errorf("唤醒消息 payload = %v", got.Payload)
		}
	default:
		t.Error("Stop 未唤醒 pending")
	}
}

// TestChannelID_回落 空 ChannelID 回落 "a2a"
func TestChannelID_回落(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{})
	if got := ch.ChannelID(); got != "a2a" {
		t.Errorf("ChannelID = %q, 期望 a2a", got)
	}
	ch2 := NewA2AChannel(A2AChannelConfig{ChannelID: "  custom  "})
	if got := ch2.ChannelID(); got != "custom" {
		t.Errorf("ChannelID = %q, 期望 custom", got)
	}
}

// TestChannelType_返回A2A ChannelType 为 ChannelTypeA2A
func TestChannelType_返回A2A(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{})
	if got := ch.ChannelType(); got != cm.ChannelTypeA2A {
		t.Errorf("ChannelType = %v, 期望 %v", got, cm.ChannelTypeA2A)
	}
}

// TestNewMux_三路由 校验 rpc/card/extended 三路径
func TestNewMux_三路由(t *testing.T) {
	ch := NewA2AChannel(ConfigFromEnv(func(string) string { return "" }))
	mux := ch.newMux()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ch.cfg.CardPath, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("card 状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"protocolBinding":"JSONRPC"`) {
		t.Errorf("card 内容 = %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ch.cfg.ExtendedCardPath, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("extended card 状态码 = %d, 期望 200", rec.Code)
	}

	// rpc 路径只接受 POST
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ch.cfg.RPCPath, nil))
	if rec.Code == http.StatusOK {
		t.Error("rpc GET 不应返回 200")
	}
}

// TestStart_未启用不监听 未启用时 Start 直接返回且不置运行态
func TestStart_未启用不监听(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	if err := ch.Start(t.Context()); err != nil {
		t.Fatalf("未启用 Start 应返回 nil, 实际 %v", err)
	}
	if ch.IsRunning() {
		t.Error("未启用不应处于运行态")
	}
}

// TestStart_端口占用返错 端口被占时 Start 返错
func TestStart_端口占用返错(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("预占端口失败: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	ch := NewA2AChannel(testConfig(port))
	if err := ch.Start(t.Context()); err == nil {
		t.Error("端口被占时应返回错误")
	}
	if ch.IsRunning() {
		t.Error("启动失败不应置运行态")
	}
}

// TestStartStop_幂等与运行态 重复 Start 幂等，Stop 后非运行态
func TestStartStop_幂等与运行态(t *testing.T) {
	ch := NewA2AChannel(testConfig(freePort(t)))
	if err := ch.Start(t.Context()); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if !ch.IsRunning() {
		t.Error("Start 后应处于运行态")
	}
	if err := ch.Start(t.Context()); err != nil {
		t.Errorf("重复 Start 应返回 nil, 实际 %v", err)
	}
	if err := ch.Stop(t.Context()); err != nil {
		t.Errorf("Stop 失败: %v", err)
	}
	if ch.IsRunning() {
		t.Error("Stop 后不应运行")
	}
}

// testConfig 构造测试用完整配置
func testConfig(port int) A2AChannelConfig {
	return A2AChannelConfig{
		Enabled: true, Host: "127.0.0.1", Port: port,
		RPCPath: "/a2a", CardPath: "/.well-known/agent-card.json",
		ExtendedCardPath: "/agent/authenticatedExtendedCard",
		ProtocolVersion:  "1.0.0", ChannelID: "a2a",
		AppName: "UapClaw Gateway A2A Server", AppDescription: "d", AppVersion: "0.1.0",
	}
}

// freePort 申请一个当前空闲的本地端口
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请端口失败: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
