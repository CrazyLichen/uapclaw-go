package channel_manager

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── stubChannel 测试桩 ────────────────────────────

// stubChannel 用于测试的 BaseChannel 桩实现
type stubChannel struct {
	id          string
	chType      ChannelType
	running     bool
	config      any
	onMsgCb     func(*schema.Message) bool
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

func (s *stubChannel) Config() any { return s.config }
func (s *stubChannel) Start(_ context.Context) error {
	s.startCalled = true
	s.running = true
	return nil
}
func (s *stubChannel) Stop(_ context.Context) error {
	s.stopCalled = true
	s.running = false
	return nil
}
func (s *stubChannel) Send(_ context.Context, msg *schema.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sentMsgs = append(s.sentMsgs, msg)
	return nil
}
func (s *stubChannel) OnMessage(callback func(*schema.Message) bool) { s.onMsgCb = callback }
func (s *stubChannel) IsRunning() bool                          { return s.running }
func (s *stubChannel) ChannelID() string                        { return s.id }
func (s *stubChannel) ChannelType() ChannelType                 { return s.chType }

func (s *stubChannel) getSentMsgs() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*schema.Message, len(s.sentMsgs))
	copy(result, s.sentMsgs)
	return result
}

// ──────────────────────────── stubMessageHandler 测试桩 ────────────────────────────

// stubMessageHandler 用于测试的 MessageHandlerInterface 桩实现
type stubMessageHandler struct {
	handledMessages []*schema.Message
	consumeQueue    []*schema.Message
	mu              sync.Mutex
}

func newStubMessageHandler() *stubMessageHandler {
	return &stubMessageHandler{}
}

func (s *stubMessageHandler) HandleMessage(msg *schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handledMessages = append(s.handledMessages, msg)
}

func (s *stubMessageHandler) ConsumeRobotMessages(timeout time.Duration) *schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.consumeQueue) > 0 {
		msg := s.consumeQueue[0]
		s.consumeQueue = s.consumeQueue[1:]
		return msg
	}
	return nil
}

func (s *stubMessageHandler) getHandledMessages() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*schema.Message, len(s.handledMessages))
	copy(result, s.handledMessages)
	return result
}

func (s *stubMessageHandler) enqueueConsume(msg *schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consumeQueue = append(s.consumeQueue, msg)
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestCM 创建带默认 stubMessageHandler 的 ChannelManager
func newTestCM() (*ChannelManager, *stubMessageHandler) {
	mh := newStubMessageHandler()
	cm := NewChannelManager(mh, nil, nil)
	return cm, mh
}

// ──────────────────────────── RegisterChannel 测试 ────────────────────────────

// TestChannelManager_RegisterChannel_注册成功 测试 RegisterChannel 注册 Channel
func TestChannelManager_RegisterChannel_注册成功(t *testing.T) {
	cm, _ := newTestCM()
	ch := newStubChannel("web-001", ChannelTypeWeb)

	cm.RegisterChannel(ch)

	if cm.GetChannel("web-001") == nil {
		t.Error("RegisterChannel 后 GetChannel 应返回非 nil")
	}
	// 验证默认回调已注册
	if ch.onMsgCb == nil {
		t.Error("RegisterChannel 后 OnMessage 回调应非 nil")
	}
}

// TestChannelManager_RegisterChannel_默认回调转发 测试 RegisterChannel 默认回调转发到 MessageHandler
func TestChannelManager_RegisterChannel_默认回调转发(t *testing.T) {
	cm, mh := newTestCM()
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.RegisterChannel(ch)

	// 模拟 Channel 收到消息触发回调
	msg := &schema.Message{ID: "msg-1", ChannelID: "web-001"}
	if ch.onMsgCb != nil {
		ch.onMsgCb(msg)
	}

	handled := mh.getHandledMessages()
	if len(handled) != 1 {
		t.Errorf("默认回调应转发消息到 MessageHandler, 实际 = %d 条", len(handled))
	}
}

// TestChannelManager_RegisterChannel_存活检查 测试已注销 Channel 的消息被丢弃
func TestChannelManager_RegisterChannel_存活检查(t *testing.T) {
	cm, mh := newTestCM()
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.RegisterChannel(ch)

	// 注销后触发回调
	cm.UnregisterChannel("web-001")
	msg := &schema.Message{ID: "msg-2", ChannelID: "web-001"}
	if ch.onMsgCb != nil {
		ch.onMsgCb(msg)
	}

	handled := mh.getHandledMessages()
	if len(handled) != 0 {
		t.Errorf("已注销 Channel 的消息应被丢弃, 实际 = %d 条", len(handled))
	}
}

// ──────────────────────────── RegisterChannelWithInbound 测试 ────────────────────────────

// TestChannelManager_RegisterChannelWithInbound_自定义回调 测试自定义入站回调
func TestChannelManager_RegisterChannelWithInbound_自定义回调(t *testing.T) {
	cm, _ := newTestCM()
	ch := newStubChannel("web-001", ChannelTypeWeb)

	callbackCalled := false
	cm.RegisterChannelWithInbound(ch, func(_ *schema.Message) bool {
		callbackCalled = true
		return true
	})

	if cm.GetChannel("web-001") == nil {
		t.Error("RegisterChannelWithInbound 后 GetChannel 应返回非 nil")
	}
	if ch.onMsgCb != nil {
		ch.onMsgCb(&schema.Message{ID: "test"})
		if !callbackCalled {
			t.Error("自定义回调应被触发")
		}
	}
}

// ──────────────────────────── DeliverToMessageHandler 测试 ────────────────────────────

// TestChannelManager_DeliverToMessageHandler_直接转发 测试直接转发到 MessageHandler
func TestChannelManager_DeliverToMessageHandler_直接转发(t *testing.T) {
	cm, mh := newTestCM()

	msg := &schema.Message{ID: "test-msg", ChannelID: "web"}
	cm.DeliverToMessageHandler(msg)

	handled := mh.getHandledMessages()
	if len(handled) != 1 {
		t.Errorf("DeliverToMessageHandler 应转发 1 条消息, 实际 = %d", len(handled))
	}
}

// ──────────────────────────── UnregisterChannel 测试 ────────────────────────────

// TestChannelManager_UnregisterChannel_注销成功 测试注销 Channel
func TestChannelManager_UnregisterChannel_注销成功(t *testing.T) {
	cm, _ := newTestCM()
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.RegisterChannel(ch)

	cm.UnregisterChannel("web-001")
	if cm.GetChannel("web-001") != nil {
		t.Error("UnregisterChannel 后 GetChannel 应返回 nil")
	}
}

// TestChannelManager_UnregisterChannel_不存在不报错 测试注销不存在的 Channel 不报错
func TestChannelManager_UnregisterChannel_不存在不报错(t *testing.T) {
	cm, _ := newTestCM()
	cm.UnregisterChannel("nonexistent") // 不应 panic
}

// ──────────────────────────── GetEnabledChannels 测试 ────────────────────────────

// TestChannelManager_GetEnabledChannels_空 测试无 Channel 时返回空列表
func TestChannelManager_GetEnabledChannels_空(t *testing.T) {
	cm, _ := newTestCM()
	channels := cm.GetEnabledChannels()
	if len(channels) != 0 {
		t.Errorf("GetEnabledChannels() 应返回空列表, 实际 = %v", channels)
	}
}

// TestChannelManager_GetEnabledChannels_多个 测试多 Channel 注册后返回全部 ID
func TestChannelManager_GetEnabledChannels_多个(t *testing.T) {
	cm, _ := newTestCM()
	cm.RegisterChannel(newStubChannel("web-001", ChannelTypeWeb))
	cm.RegisterChannel(newStubChannel("feishu-001", ChannelTypeFeishu))

	channels := cm.GetEnabledChannels()
	if len(channels) != 2 {
		t.Errorf("GetEnabledChannels() 应返回 2 个, 实际 = %d", len(channels))
	}
}

// ──────────────────────────── GetChannel 测试 ────────────────────────────

// TestChannelManager_GetChannel_存在 测试获取已注册的 Channel
func TestChannelManager_GetChannel_存在(t *testing.T) {
	cm, _ := newTestCM()
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.RegisterChannel(ch)

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
	cm, _ := newTestCM()
	if cm.GetChannel("nonexistent") != nil {
		t.Error("GetChannel(\"nonexistent\") 应返回 nil")
	}
}

// ──────────────────────────── StartDispatch / StopDispatch 测试 ────────────────────────────

// TestChannelManager_StartDispatch_定向投递 测试出站派发循环按 channel_id 定向投递
func TestChannelManager_StartDispatch_定向投递(t *testing.T) {
	cm, mh := newTestCM()
	ch := newStubChannel("web-001", ChannelTypeWeb)
	cm.RegisterChannel(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cm.StartDispatch(ctx)
	if err != nil {
		t.Fatalf("StartDispatch 返回错误: %v", err)
	}

	// 向 consumeQueue 放入一条消息
	outMsg := &schema.Message{ID: "out-1", ChannelID: "web-001"}
	mh.enqueueConsume(outMsg)

	// 等待投递
	time.Sleep(100 * time.Millisecond)

	sent := ch.getSentMsgs()
	if len(sent) != 1 {
		t.Errorf("应投递 1 条消息到 Channel, 实际 = %d", len(sent))
	}
}

// TestChannelManager_StopDispatch_停止 测试停止出站派发循环
func TestChannelManager_StopDispatch_停止(t *testing.T) {
	cm, _ := newTestCM()

	ctx := context.Background()
	cm.StartDispatch(ctx)
	err := cm.StopDispatch()
	if err != nil {
		t.Fatalf("StopDispatch 返回错误: %v", err)
	}
	if cm.running.Load() {
		t.Error("StopDispatch 后 running 应为 false")
	}
}

// ──────────────────────────── 配置管理测试 ────────────────────────────

// TestChannelManager_GetConf_存在 测试获取已存在的 Channel 配置
func TestChannelManager_GetConf_存在(t *testing.T) {
	initial := map[string]map[string]any{
		"feishu-001": {"app_id": "cli_xxx", "secret": "yyy"},
	}
	cm := NewChannelManager(nil, initial, nil)

	conf := cm.GetConf("feishu-001")
	if conf["app_id"] != "cli_xxx" {
		t.Errorf("GetConf(\"feishu-001\")[\"app_id\"] = %v, 期望 %q", conf["app_id"], "cli_xxx")
	}
}

// TestChannelManager_GetConf_不存在 测试获取不存在的 Channel 配置返回空 map
func TestChannelManager_GetConf_不存在(t *testing.T) {
	cm, _ := newTestCM()
	conf := cm.GetConf("nonexistent")
	if len(conf) != 0 {
		t.Errorf("GetConf(\"nonexistent\") 应返回空 map, 实际 = %v", conf)
	}
}

// TestChannelManager_SetConf_触发回调 测试 SetConf 触发配置更新回调
func TestChannelManager_SetConf_触发回调(t *testing.T) {
	var callbackConfig map[string]map[string]any
	cm := NewChannelManager(nil, nil, func(config map[string]map[string]any) {
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
	cm := NewChannelManager(nil, nil, func(config map[string]map[string]any) {
		callbackConfig = config
	})

	newConf := map[string]map[string]any{
		"web-001":    {"port": 8080},
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
	cm, _ := newTestCM()

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
	cm, _ := newTestCM()
	cm.MarkChannelRestartPending("feishu-001")

	pending := cm.PopChannelRestartPending()
	if len(pending) != 1 || pending[0] != "feishu-001" {
		t.Errorf("PopChannelRestartPending() = %v, 期望 [feishu-001]", pending)
	}
}

// TestChannelManager_MarkChannelRestartPending_空ID忽略 测试空字符串 channelID 被忽略
func TestChannelManager_MarkChannelRestartPending_空ID忽略(t *testing.T) {
	cm, _ := newTestCM()
	cm.MarkChannelRestartPending("")

	pending := cm.PopChannelRestartPending()
	if len(pending) != 0 {
		t.Errorf("PopChannelRestartPending() 应为空, 实际 = %v", pending)
	}
}

// TestChannelManager_PopChannelRestartPending_重置 测试 Pop 后集合被重置
func TestChannelManager_PopChannelRestartPending_重置(t *testing.T) {
	cm, _ := newTestCM()
	cm.MarkChannelRestartPending("web-001")

	_ = cm.PopChannelRestartPending()
	pending2 := cm.PopChannelRestartPending()
	if len(pending2) != 0 {
		t.Errorf("第二次 Pop 应为空, 实际 = %v", pending2)
	}
}
