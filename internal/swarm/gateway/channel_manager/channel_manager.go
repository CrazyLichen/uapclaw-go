package channel_manager

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OnConfigUpdatedFunc 配置更新回调函数类型。
//
// 当 Channel 配置发生变更时触发，由外部实现具体的 Channel 重新实例化逻辑。
type OnConfigUpdatedFunc func(config map[string]map[string]any)

// ChannelManager 负责 Channel 的注册、注销、查找与消息分发。
//
// 核心职责：
//  1. Channel 的注册、注销与查找
//  2. 将各 Channel 收到的消息统一转发到 MessageHandler
//  3. 配置热更新回调
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/channel_manager.py (ChannelManager)
type ChannelManager struct {
	// channels 已注册的 Channel 实例映射（channelID → BaseChannel）
	channels map[string]BaseChannel
	// config Channel 相关配置（channelID → 配置 dict）
	config map[string]map[string]any
	// onConfigUpdated 配置更新回调
	onConfigUpdated OnConfigUpdatedFunc
	// pendingChannelRestart 待强制重启的 channelID 集合
	pendingChannelRestart map[string]struct{}
	// mu 保护 channels/config/pendingChannelRestart 的并发访问
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 本包日志组件
const logComponent = logger.ComponentChannel

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelManager 创建 ChannelManager 实例。
//
// 参数：
//   - config：初始 Channel 配置（channelID → 配置 dict），可为 nil
//   - onConfigUpdated：配置更新回调，可为 nil
func NewChannelManager(config map[string]map[string]any, onConfigUpdated OnConfigUpdatedFunc) *ChannelManager {
	cfg := make(map[string]map[string]any)
	for k, v := range config {
		cfg[k] = v
	}
	return &ChannelManager{
		channels:              make(map[string]BaseChannel),
		config:                cfg,
		onConfigUpdated:       onConfigUpdated,
		pendingChannelRestart: make(map[string]struct{}),
	}
}

// Register 注册 Channel，并为其注册入站消息回调。
//
// 注册后 Channel 收到的消息将通过 onMessageCallback 转发到消息处理管线。
//
// 对应 Python: ChannelManager.register_channel()
func (cm *ChannelManager) Register(ch BaseChannel, onMessageCallback func(*schema.Message)) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cid := ch.ChannelID()
	cm.channels[cid] = ch
	ch.OnMessage(onMessageCallback)

	logger.Info(logComponent).
		Str("channel_id", cid).
		Int("total", len(cm.channels)).
		Msg("已注册 Channel")
}

// Unregister 注销指定 Channel。
//
// 对应 Python: ChannelManager.unregister_channel()
func (cm *ChannelManager) Unregister(channelID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.channels, channelID)

	logger.Info(logComponent).
		Str("channel_id", channelID).
		Msg("已注销 Channel")
}

// GetChannel 根据 channelID 获取 Channel，不存在返回 nil。
//
// 对应 Python: ChannelManager.get_channel()
func (cm *ChannelManager) GetChannel(channelID string) BaseChannel {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.channels[channelID]
}

// GetEnabledChannels 返回当前已注册的 Channel 标识列表。
//
// 对应 Python: ChannelManager.enabled_channels
func (cm *ChannelManager) GetEnabledChannels() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]string, 0, len(cm.channels))
	for id := range cm.channels {
		result = append(result, id)
	}
	return result
}

// BroadcastToChannels 向所有已注册的 Channel 广播消息。
//
// 遍历所有 Channel 并调用其 Send 方法，发送失败的 Channel 记录错误日志但不中断。
func (cm *ChannelManager) BroadcastToChannels(ctx context.Context, msg *schema.Message) error {
	cm.mu.RLock()
	channels := make([]BaseChannel, 0, len(cm.channels))
	for _, ch := range cm.channels {
		channels = append(channels, ch)
	}
	cm.mu.RUnlock()

	var firstErr error
	for _, ch := range channels {
		if err := ch.Send(ctx, msg); err != nil {
			logger.Error(logComponent).
				Str("channel_id", ch.ChannelID()).
				Err(err).
				Msg("广播消息到 Channel 失败")
			if firstErr == nil {
				firstErr = fmt.Errorf("广播到 Channel %q 失败: %w", ch.ChannelID(), err)
			}
		}
	}
	return firstErr
}

// MarkChannelRestartPending 请求在下次配置应用时强制重启该 Channel。
//
// 对应 Python: ChannelManager.mark_channel_restart_pending()
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
// 对应 Python: ChannelManager.pop_channel_restart_pending()
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
// 对应 Python: ChannelManager.get_conf()
func (cm *ChannelManager) GetConf(channelID string) map[string]any {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conf, ok := cm.config[channelID]
	if !ok {
		return make(map[string]any)
	}
	result := make(map[string]any, len(conf))
	for k, v := range conf {
		result[k] = v
	}
	return result
}

// SetConf 更新指定 channelID 的配置，并触发 onConfigUpdated 回调。
//
// 对应 Python: ChannelManager.set_conf()
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
// 对应 Python: ChannelManager.set_config()
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
// 对应 Python: ChannelManager.set_config_callback()
func (cm *ChannelManager) SetConfigCallback(callback OnConfigUpdatedFunc) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.onConfigUpdated = callback
}

// ──────────────────────────── 非导出函数 ────────────────────────────
