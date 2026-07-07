package channel_manager

import (
	"context"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── stubChannel 测试桩 ────────────────────────────

// stubChannel 用于测试的 BaseChannel 桩实现
type stubChannel struct {
	id          string
	chType      ChannelType
	running     bool
	config      any
	onMsgCb     func(*schema.Message)
	sentMsgs    []*schema.Message
	mu          sync.Mutex
	startCalled bool
	stopCalled  bool
}

func newStubChannel(id string, chType ChannelType) *stubChannel {
	return &stubChannel{
		id:     id,
		chType: chType,
	}
}

func (s *stubChannel) Config() any                  { return s.config }
func (s *stubChannel) Start(_ context.Context) error { s.startCalled = true; s.running = true; return nil }
func (s *stubChannel) Stop(_ context.Context) error  { s.stopCalled = true; s.running = false; return nil }
func (s *stubChannel) Send(_ context.Context, msg *schema.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sentMsgs = append(s.sentMsgs, msg)
	return nil
}
func (s *stubChannel) OnMessage(callback func(*schema.Message)) { s.onMsgCb = callback }
func (s *stubChannel) IsRunning() bool                         { return s.running }
func (s *stubChannel) ChannelID() string                       { return s.id }
func (s *stubChannel) ChannelType() ChannelType                { return s.chType }

func (s *stubChannel) getSentMsgs() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*schema.Message, len(s.sentMsgs))
	copy(result, s.sentMsgs)
	return result
}

// ──────────────────────────── Register / Unregister 测试 ────────────────────────────

// TestChannelManager_Register_注册成功 测试 Register 注册 Channel
func TestChannelManager_Register_注册成功(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	ch := newStubChannel("web-001", ChannelTypeWeb)

	callbackCalled := false
	cm.Register(ch, func(_ *schema.Message) { callbackCalled = true })

	if cm.GetChannel("web-001") == nil {
		t.Error("Register 后 GetChannel 应返回非 nil")
	}

	// 验证 OnMessage 回调已注册
	if ch.onMsgCb == nil {
		t.Error("Register 后 OnMessage 回调应非 nil")
	}
	// 模拟触发回调
	if ch.onMsgCb != nil {
		ch.onMsgCb(&schema.Message{ID: "test"})
		if !callbackCalled {
			t.Error("OnMessage 回调应被触发")
		}
	}
}

// TestChannelManager_Unregister_注销成功 测试 Unregister 注销 Channel
func TestChannelManager_Unregister_注销成功(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.Register(ch, func(_ *schema.Message) {})

	cm.Unregister("web-001")
	if cm.GetChannel("web-001") != nil {
		t.Error("Unregister 后 GetChannel 应返回 nil")
	}
}

// TestChannelManager_Unregister_不存在不报错 测试注销不存在的 Channel 不报错
func TestChannelManager_Unregister_不存在不报错(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	cm.Unregister("nonexistent") // 不应 panic
}

// ──────────────────────────── GetEnabledChannels 测试 ────────────────────────────

// TestChannelManager_GetEnabledChannels_空 测试无 Channel 时返回空列表
func TestChannelManager_GetEnabledChannels_空(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	channels := cm.GetEnabledChannels()
	if len(channels) != 0 {
		t.Errorf("GetEnabledChannels() 应返回空列表, 实际 = %v", channels)
	}
}

// TestChannelManager_GetEnabledChannels_多个 测试多 Channel 注册后返回全部 ID
func TestChannelManager_GetEnabledChannels_多个(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	cm.Register(newStubChannel("web-001", ChannelTypeWeb), func(_ *schema.Message) {})
	cm.Register(newStubChannel("feishu-001", ChannelTypeFeishu), func(_ *schema.Message) {})

	channels := cm.GetEnabledChannels()
	if len(channels) != 2 {
		t.Errorf("GetEnabledChannels() 应返回 2 个, 实际 = %d", len(channels))
	}
}

// ──────────────────────────── GetChannel 测试 ────────────────────────────

// TestChannelManager_GetChannel_存在 测试获取已注册的 Channel
func TestChannelManager_GetChannel_存在(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.Register(ch, func(_ *schema.Message) {})

	got := cm.GetChannel("web-001")
	if got == nil {
		t.Fatal("GetChannel(\"web-001\") 应返回非 nil")
	}
	if got.ChannelID() != "web-001" {
		t.Errorf("ChannelID() = %q, 期望 %q", got.ChannelID(), "web-001")
	}
}

// TestChannelManager_GetChannel_不存在 测试获取未注册的 Channel 返回 nil
func TestChannelManager_GetChannel_不存在(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	if cm.GetChannel("nonexistent") != nil {
		t.Error("GetChannel(\"nonexistent\") 应返回 nil")
	}
}

// ──────────────────────────── BroadcastToChannels 测试 ────────────────────────────

// TestChannelManager_BroadcastToChannels_广播成功 测试广播消息到所有 Channel
func TestChannelManager_BroadcastToChannels_广播成功(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	ch1 := newStubChannel("web-001", ChannelTypeWeb)
	ch2 := newStubChannel("feishu-001", ChannelTypeFeishu)
	cm.Register(ch1, func(_ *schema.Message) {})
	cm.Register(ch2, func(_ *schema.Message) {})

	msg := &schema.Message{ID: "test-msg"}
	err := cm.BroadcastToChannels(context.Background(), msg)
	if err != nil {
		t.Fatalf("BroadcastToChannels 返回错误: %v", err)
	}

	if len(ch1.getSentMsgs()) != 1 {
		t.Errorf("ch1 应收到 1 条消息, 实际 = %d", len(ch1.getSentMsgs()))
	}
	if len(ch2.getSentMsgs()) != 1 {
		t.Errorf("ch2 应收到 1 条消息, 实际 = %d", len(ch2.getSentMsgs()))
	}
}

// ──────────────────────────── 配置管理测试 ────────────────────────────

// TestChannelManager_GetConf_存在 测试获取已存在的 Channel 配置
func TestChannelManager_GetConf_存在(t *testing.T) {
	initial := map[string]map[string]any{
		"feishu-001": {"app_id": "cli_xxx", "secret": "yyy"},
	}
	cm := NewChannelManager(initial, nil)

	conf := cm.GetConf("feishu-001")
	if conf["app_id"] != "cli_xxx" {
		t.Errorf("GetConf(\"feishu-001\")[\"app_id\"] = %v, 期望 %q", conf["app_id"], "cli_xxx")
	}
}

// TestChannelManager_GetConf_不存在 测试获取不存在的 Channel 配置返回空 map
func TestChannelManager_GetConf_不存在(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	conf := cm.GetConf("nonexistent")
	if len(conf) != 0 {
		t.Errorf("GetConf(\"nonexistent\") 应返回空 map, 实际 = %v", conf)
	}
}

// TestChannelManager_SetConf_触发回调 测试 SetConf 触发配置更新回调
func TestChannelManager_SetConf_触发回调(t *testing.T) {
	var callbackConfig map[string]map[string]any
	cm := NewChannelManager(nil, func(config map[string]map[string]any) {
		callbackConfig = config
	})

	cm.SetConf("feishu-001", map[string]any{"app_id": "cli_new"})

	if callbackConfig == nil {
		t.Fatal("SetConf 应触发回调")
	}
	if callbackConfig["feishu-001"]["app_id"] != "cli_new" {
		t.Errorf("回调配置 feishu-001.app_id = %v, 期望 %q", callbackConfig["feishu-001"]["app_id"], "cli_new")
	}
}

// TestChannelManager_SetConfig_整体替换 测试 SetConfig 整体替换配置
func TestChannelManager_SetConfig_整体替换(t *testing.T) {
	var callbackConfig map[string]map[string]any
	cm := NewChannelManager(nil, func(config map[string]map[string]any) {
		callbackConfig = config
	})

	newConf := map[string]map[string]any{
		"web-001":   {"port": 8080},
		"feishu-001": {"app_id": "cli_xxx"},
	}
	cm.SetConfig(newConf)

	if callbackConfig == nil {
		t.Fatal("SetConfig 应触发回调")
	}
	if len(callbackConfig) != 2 {
		t.Errorf("回调配置应有 2 项, 实际 = %d", len(callbackConfig))
	}
}

// TestChannelManager_SetConfigCallback_更新回调 测试 SetConfigCallback 动态更新回调
func TestChannelManager_SetConfigCallback_更新回调(t *testing.T) {
	cm := NewChannelManager(nil, nil)

	callbackCalled := false
	cm.SetConfigCallback(func(_ map[string]map[string]any) {
		callbackCalled = true
	})

	cm.SetConf("test", map[string]any{"key": "value"})
	if !callbackCalled {
		t.Error("SetConfigCallback 设置的回调应被触发")
	}
}

// ──────────────────────────── MarkChannelRestartPending 测试 ────────────────────────────

// TestChannelManager_MarkChannelRestartPending_正常 测试标记待重启 Channel
func TestChannelManager_MarkChannelRestartPending_正常(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	cm.MarkChannelRestartPending("feishu-001")

	pending := cm.PopChannelRestartPending()
	if len(pending) != 1 || pending[0] != "feishu-001" {
		t.Errorf("PopChannelRestartPending() = %v, 期望 [feishu-001]", pending)
	}
}

// TestChannelManager_MarkChannelRestartPending_空ID忽略 测试空字符串 channelID 被忽略
func TestChannelManager_MarkChannelRestartPending_空ID忽略(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	cm.MarkChannelRestartPending("")

	pending := cm.PopChannelRestartPending()
	if len(pending) != 0 {
		t.Errorf("PopChannelRestartPending() 应为空, 实际 = %v", pending)
	}
}

// TestChannelManager_PopChannelRestartPending_重置 测试 Pop 后集合被重置
func TestChannelManager_PopChannelRestartPending_重置(t *testing.T) {
	cm := NewChannelManager(nil, nil)
	cm.MarkChannelRestartPending("web-001")

	_ = cm.PopChannelRestartPending()
	pending2 := cm.PopChannelRestartPending()
	if len(pending2) != 0 {
		t.Errorf("第二次 Pop 应为空, 实际 = %v", pending2)
	}
}
