package channel_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageHandlerInterface 消息处理器接口，由 MessageHandler 实现。
//
// 合并原 InboundMessageHandler + RobotMessageConsumer，对齐 Python 单个 message_handler 参数。
// ChannelManager 通过此接口同时处理入站转发和出站消费，避免拆分成两个接口。
//
// Python: MessageHandler（同时提供 handle_message + consume_robot_messages）
type MessageHandlerInterface interface {
	// HandleMessage 处理入站消息（写入 userMessages 队列）。
	//
	// Python: MessageHandler.handle_message
	HandleMessage(msg *schema.Message)
	// ConsumeRobotMessages 从出站队列消费一条消息，超时返回 nil。
	//
	// Python: MessageHandler.consume_robot_messages
	ConsumeRobotMessages(timeout time.Duration) *schema.Message
}

// ExternalChannel 仅出站能力的渠道接口。
//
// 对齐 Python register_external_channel 的 duck typing 语义：
// Python 只要求对象有 send 方法即可入 channels 表；Go 侧用窄接口表达。
type ExternalChannel interface {
	// Send 向该渠道投递出站消息（对齐 Python channel.send(msg)）。
	Send(ctx context.Context, msg *schema.Message) error
}

// ChannelManager 负责 Channel 的注册、注销、查找与消息分发。
//
// 核心职责：
//  1. Channel 的注册、注销与查找
//  2. 将各 Channel 收到的消息统一转发到 MessageHandler
//  3. 运行出站派发循环：从 MessageHandler 取出 AgentServer 响应并投递到对应 Channel
//  4. 配置热更新回调
//
// Python: jiuwenswarm/gateway/channel_manager/channel_manager.py (ChannelManager)
type ChannelManager struct {
	// channels 已注册的 Channel 实例映射（channelID → BaseChannel）
	channels map[string]BaseChannel
	// externalChannels 外部渠道表（仅出站，channelID → ExternalChannel）
	// 对齐 Python register_external_channel 直接写 _channels 的语义；
	// Go 因接口宽度不同分表存放，出站派发时 channels 优先、miss 后查本表。
	externalChannels map[string]ExternalChannel
	// config Channel 相关配置（channelID → 配置 dict）
	config map[string]map[string]any
	// onConfigUpdated 配置更新回调
	onConfigUpdated OnConfigUpdatedFunc
	// pendingChannelRestart 待强制重启的 channelID 集合
	pendingChannelRestart map[string]struct{}
	// messageHandler 消息处理器（对齐 Python _message_handler，同时提供入站+出站能力）
	messageHandler MessageHandlerInterface
	// running 出站派发循环运行状态（对齐 Python _running）
	running atomic.Bool
	// dispatchCancel 出站派发循环取消函数（对齐 Python _dispatch_task）
	dispatchCancel context.CancelFunc
	// mu 保护 channels/externalChannels/config/pendingChannelRestart 的并发访问
	mu sync.RWMutex
}

// OnConfigUpdatedFunc 配置更新回调函数类型。
//
// 当 Channel 配置发生变更时触发，由外部实现具体的 Channel 重新实例化逻辑。
type OnConfigUpdatedFunc func(config map[string]map[string]any)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 本包日志组件
const logComponent = logger.ComponentChannel

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelManager 创建 ChannelManager 实例。
//
// 参数：
//   - messageHandler：消息处理器（同时提供入站转发+出站消费），可为 nil（仅配置管理场景）
//   - config：初始 Channel 配置（channelID → 配置 dict），可为 nil
//   - onConfigUpdated：配置更新回调，可为 nil
//
// Python: ChannelManager(message_handler, config=None, on_config_updated=None)
func NewChannelManager(
	messageHandler MessageHandlerInterface,
	config map[string]map[string]any,
	onConfigUpdated OnConfigUpdatedFunc,
) *ChannelManager {
	cfg := make(map[string]map[string]any)
	for k, v := range config {
		cfg[k] = v
	}
	return &ChannelManager{
		channels:              make(map[string]BaseChannel),
		externalChannels:      make(map[string]ExternalChannel),
		config:                cfg,
		onConfigUpdated:       onConfigUpdated,
		pendingChannelRestart: make(map[string]struct{}),
		messageHandler:        messageHandler,
	}
}

// RegisterChannel 注册 Channel，并为其注册默认入站回调（存活检查+转发）。
//
// 注册后 Channel 收到的消息将通过 onChannelMessage 转发到 MessageHandler。
//
// Python: ChannelManager.register_channel()
func (cm *ChannelManager) RegisterChannel(ch BaseChannel) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cid := ch.ChannelID()
	cm.channels[cid] = ch
	ch.OnMessage(cm.onChannelMessage)

	logger.Info(logComponent).
		Str("channel_id", cid).
		Int("total", len(cm.channels)).
		Msg("Channel registered")
}

// RegisterChannelWithInbound 注册 Channel 并使用自定义入站回调。
//
// 不替换为默认 onChannelMessage，由调用方决定消息处理路径。
// 回调返回 true 表示已处理（短路后续 method handler），返回 false 继续默认处理。
//
// Python: ChannelManager.register_channel_with_inbound()
func (cm *ChannelManager) RegisterChannelWithInbound(ch BaseChannel, onMessage func(*schema.Message) bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.channels[ch.ChannelID()] = ch
	ch.OnMessage(onMessage)

	logger.Info(logComponent).
		Str("channel_id", ch.ChannelID()).
		Int("total", len(cm.channels)).
		Msg("Channel registered (custom inbound)")
}

// RegisterExternalChannel 登记一个已由外部完成入站装配的渠道实例。
//
// 不做存活检查、不设 onMessage，仅入 externalChannels 表供出站派发。
// Python: ChannelManager.register_external_channel()（channel_manager.py L86-88）。
func (cm *ChannelManager) RegisterExternalChannel(channelID string, ch ExternalChannel) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.externalChannels[channelID]; exists {
		logger.Warn(logComponent).
			Str("channel_id", channelID).
			Msg("external channel re-registered, overwriting")
	}
	if _, exists := cm.channels[channelID]; exists {
		logger.Warn(logComponent).
			Str("channel_id", channelID).
			Msg("external channel shadows registered BaseChannel in dispatch priority")
	}
	cm.externalChannels[channelID] = ch
}

// GetExternalChannel 根据 channelID 获取外部渠道，不存在返回 nil。
func (cm *ChannelManager) GetExternalChannel(channelID string) ExternalChannel {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.externalChannels[channelID]
}

// DeliverToMessageHandler 将消息直接交给 MessageHandler。
//
// 供自定义入站路径使用，不做存活检查。
//
// Python: ChannelManager.deliver_to_message_handler()
func (cm *ChannelManager) DeliverToMessageHandler(msg *schema.Message) {
	if cm.messageHandler == nil {
		logger.Warn(logComponent).Str("msg_id", msg.ID).Msg("messageHandler is nil, cannot forward message")
		return
	}
	cm.messageHandler.HandleMessage(msg)
}

// UnregisterChannel 注销指定 Channel。
//
// 同时清理 BaseChannel 表与外部渠道表（对齐 Python 单 dict pop 语义）。
// Python: ChannelManager.unregister_channel()
func (cm *ChannelManager) UnregisterChannel(channelID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.channels, channelID)
	delete(cm.externalChannels, channelID)

	logger.Info(logComponent).
		Str("channel_id", channelID).
		Msg("Channel unregistered")
}

// GetChannel 根据 channelID 获取 Channel，不存在返回 nil。
//
// Python: ChannelManager.get_channel()
func (cm *ChannelManager) GetChannel(channelID string) BaseChannel {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.channels[channelID]
}

// GetEnabledChannels 返回当前已注册的 Channel 标识列表。
//
// Python: ChannelManager.enabled_channels
func (cm *ChannelManager) GetEnabledChannels() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]string, 0, len(cm.channels))
	for id := range cm.channels {
		result = append(result, id)
	}
	return result
}

// MarkChannelRestartPending 请求在下次配置应用时强制重启该 Channel。
//
// Python: ChannelManager.mark_channel_restart_pending()
func (cm *ChannelManager) MarkChannelRestartPending(channelID string) {
	if channelID == "" {
		return
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.pendingChannelRestart[channelID] = struct{}{}
}

// PopChannelRestartPending 取出并重置待强制重启集合。
//
// Python: ChannelManager.pop_channel_restart_pending()
func (cm *ChannelManager) PopChannelRestartPending() []string {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	result := make([]string, 0, len(cm.pendingChannelRestart))
	for id := range cm.pendingChannelRestart {
		result = append(result, id)
	}
	cm.pendingChannelRestart = make(map[string]struct{})
	return result
}

// GetConf 返回指定 channelID 的配置浅拷贝；不存在则返回空 map。
//
// Python: ChannelManager.get_conf()
func (cm *ChannelManager) GetConf(channelID string) map[string]any {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conf, ok := cm.config[channelID]
	if !ok {
		return make(map[string]any)
	}
	return maps.Clone(conf)
}

// SetConf 更新指定 channelID 的配置，并触发 onConfigUpdated 回调。
//
// Python: ChannelManager.set_conf()
func (cm *ChannelManager) SetConf(channelID string, newConf map[string]any) {
	cm.mu.Lock()
	merged := make(map[string]map[string]any, len(cm.config))
	for k, v := range cm.config {
		merged[k] = v
	}
	confCopy := make(map[string]any, len(newConf))
	for k, v := range newConf {
		confCopy[k] = v
	}
	merged[channelID] = confCopy
	cm.config = merged
	cb := cm.onConfigUpdated
	cm.mu.Unlock()

	if cb != nil {
		cb(merged)
	}
}

// SetConfig 整体替换配置并触发 onConfigUpdated 回调。
//
// Python: ChannelManager.set_config()
func (cm *ChannelManager) SetConfig(newConf map[string]map[string]any) {
	cm.mu.Lock()
	cfg := make(map[string]map[string]any, len(newConf))
	for k, v := range newConf {
		cfg[k] = v
	}
	cm.config = cfg
	cb := cm.onConfigUpdated
	cm.mu.Unlock()

	if cb != nil {
		cb(cfg)
	}
}

// SetConfigCallback 设置配置更新回调。
//
// Python: ChannelManager.set_config_callback()
func (cm *ChannelManager) SetConfigCallback(callback OnConfigUpdatedFunc) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.onConfigUpdated = callback
}

// StartDispatch 启动出站派发循环（消费 MessageHandler.robot_messages 并发送到各 Channel）。
//
// Python: ChannelManager.start_dispatch()
func (cm *ChannelManager) StartDispatch(ctx context.Context) error {
	if cm.running.Load() {
		return nil
	}
	cm.running.Store(true)
	dispatchCtx, cancel := context.WithCancel(ctx)
	cm.dispatchCancel = cancel
	go cm.dispatchRobotMessages(dispatchCtx)
	logger.Info(logComponent).Msg("outbound dispatch loop started (robot_messages -> Channel.send)")
	return nil
}

// StopDispatch 停止出站派发循环。
//
// 先设 running=false，让 dispatchRobotMessages 循环在当前迭代完成后自然退出。
// 不使用 cancel() 中断 context，保证进行中的 channel.Send 完整执行。
// Python: ChannelManager.stop_dispatch()（先设 _running=False，再 await task）
func (cm *ChannelManager) StopDispatch() error {
	if !cm.running.Load() {
		return nil
	}
	cm.running.Store(false)
	// 不调用 cancel()，让 dispatchRobotMessages 循环自然退出
	// 当前迭代中的 channel.Send 会完整执行后再退出
	cm.dispatchCancel = nil
	logger.Info(logComponent).Msg("outbound dispatch loop stopped")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// onChannelMessage 默认入站回调：存活检查 + 转发到 MessageHandler。
// 返回 true 表示已处理，返回 false 表示 Channel 已注销或处理器为空。
//
// Python: ChannelManager._on_channel_message()
func (cm *ChannelManager) onChannelMessage(msg *schema.Message) bool {
	logger.Info(logComponent).
		Str("msg_id", msg.ID).
		Str("channel_id", msg.ChannelID).
		Msg("Channel message -> MessageHandler")

	// 存活检查：Channel 已注销则丢弃
	cm.mu.RLock()
	_, exists := cm.channels[msg.ChannelID]
	cm.mu.RUnlock()

	if !exists {
		logger.Info(logComponent).
			Str("channel_id", msg.ChannelID).
			Msg("Channel closed, dropping message")
		return false
	}

	if cm.messageHandler == nil {
		logger.Warn(logComponent).Str("msg_id", msg.ID).Msg("messageHandler is nil, cannot forward message")
		return false
	}
	cm.messageHandler.HandleMessage(msg)
	return true
}

// dispatchRobotMessages 出站派发循环：从 MessageHandler 消费 robot_messages，按 channel_id 投递到对应 Channel。
//
// Python: ChannelManager._dispatch_robot_messages()
func (cm *ChannelManager) dispatchRobotMessages(ctx context.Context) {
	if cm.messageHandler == nil {
		logger.Warn(logComponent).Msg("messageHandler is nil, outbound dispatch skipped")
		return
	}
	for cm.running.Load() {
		msg := cm.messageHandler.ConsumeRobotMessages(1 * time.Second)
		if msg == nil {
			continue
		}
		cm.mu.RLock()
		channel, exists := cm.channels[msg.ChannelID]
		var ext ExternalChannel
		if !exists {
			ext = cm.externalChannels[msg.ChannelID]
		}
		cm.mu.RUnlock()

		if exists {
			if err := channel.Send(ctx, msg); err != nil {
				logger.Error(logComponent).
					Str("channel_id", msg.ChannelID).
					Err(err).
					Msg("failed to deliver message to Channel")
				// cron 投递失败通知
				if strings.HasPrefix(msg.ID, "cron-push-") {
					cm.notifyCronDeliveryError(msg, err)
				}
			}
		} else if ext != nil {
			// 外部渠道派发（对齐 Python register_external_channel 后的 send 路径）
			if err := ext.Send(ctx, msg); err != nil {
				logger.Error(logComponent).
					Str("channel_id", msg.ChannelID).
					Err(err).
					Msg("failed to deliver message to external channel")
			}
		} else {
			logger.Warn(logComponent).
				Str("channel_id", msg.ChannelID).
				Str("msg_id", msg.ID).
				Msg("Channel not found, dropping robot_messages")
		}
	}
}

// notifyCronDeliveryError 推送失败时，通过 web channel 发送 chat.error 通知前端。
//
// Python: ChannelManager._notify_cron_delivery_error()
func (cm *ChannelManager) notifyCronDeliveryError(originalMsg *schema.Message, deliveryErr error) {
	cronInfo, _ := originalMsg.Payload["cron"].(map[string]any)
	jobName := ""
	if cronInfo != nil {
		jobName, _ = cronInfo["job_name"].(string)
	}
	errorText := fmt.Sprintf("定时任务「%s」推送到 %s 失败：%v", jobName, originalMsg.ChannelID, deliveryErr)

	errMsg := &schema.Message{
		ID:        "cron-delivery-error-" + originalMsg.ID,
		Type:      "event",
		ChannelID: "web",
		SessionID: originalMsg.SessionID,
		Params:    json.RawMessage(`{}`),
		Timestamp: schema.NowTimestamp(),
		OK:        false,
		EventType: schema.EventTypeChatError,
		Payload: map[string]any{
			"event_type": "chat.error",
			"error":      errorText,
		},
	}

	cm.mu.RLock()
	webChannel, exists := cm.channels["web"]
	cm.mu.RUnlock()

	if exists {
		if err := webChannel.Send(context.Background(), errMsg); err != nil {
			logger.Warn(logComponent).Msg("failed to send cron push failure notification to web")
		}
	}
}
