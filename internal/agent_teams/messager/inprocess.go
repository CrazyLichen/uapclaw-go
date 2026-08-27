package messager

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// bus 进程内消息总线。
// 对齐 Python: _Bus (openjiuwen/agent_teams/messager/inprocess.py)
// 两个数据结构，均以 agentID 为 key 做 O(1) 查找：
//   - topicSubs: topic → {agentID → handler}（发布订阅扇出）
//   - p2p:       agentID → handler（点对点）
type bus struct {
	// mu 保护并发访问
	mu sync.Mutex
	// topicSubs 主题订阅表，topic → {agentID → handler}
	topicSubs map[string]map[string]MessagerHandler
	// p2p 点对点消息处理表，agentID → handler
	p2p map[string]MessagerHandler
}

// InProcessMessager 进程内消息通信实现。
// 对齐 Python: InProcessMessager (openjiuwen/agent_teams/messager/inprocess.py)
// 所有实例共享进程全局 Bus，消息直接传递，无序列化。
type InProcessMessager struct {
	// config 传输配置
	config schema.MessagerTransportConfig
	// subscribedTopics 已订阅的主题列表（用于 Unsubscribe 时移除条目）
	subscribedTopics []string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalBus 进程全局 Bus 单例
	globalBus *bus
	// busOnce 确保 Bus 只初始化一次
	busOnce sync.Once
	// busMu 保护 globalBus 的访问
	busMu sync.Mutex
)

// 编译时接口合规检查
var _ Messager = (*InProcessMessager)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInProcessMessager 创建进程内消息通信实例。
func NewInProcessMessager(config schema.MessagerTransportConfig) *InProcessMessager {
	return &InProcessMessager{config: config}
}

// CleanupInProcessBus 重置进程全局 Bus（测试间调用）。
// 对齐 Python: cleanup_inprocess_bus()
func CleanupInProcessBus() {
	busMu.Lock()
	defer busMu.Unlock()
	if globalBus != nil {
		globalBus.clear()
	}
	globalBus = nil
	busOnce = sync.Once{}
}

// Start 启动消息传输层（InProcess 无需操作）。
func (m *InProcessMessager) Start(_ context.Context) error {
	return nil
}

// Stop 停止消息传输层（InProcess 无需操作）。
func (m *InProcessMessager) Stop(_ context.Context) error {
	return nil
}

// Publish 向主题发布事件消息。
// 自动设置 SenderID 过滤自发布（对齐 Python message.model_copy(update={"sender_id": self._agent_id})）。
func (m *InProcessMessager) Publish(ctx context.Context, topicID string, message *schema.EventMessage) error {
	agentID := m.agentID()
	// 对齐 Python: message.model_copy(update={"sender_id": self._agent_id}) — 创建副本再修改
	if message.SenderID == "" {
		msgCopy := *message
		msgCopy.SenderID = agentID
		message = &msgCopy
	}
	b := getBus()
	b.mu.Lock()
	subs, ok := b.topicSubs[topicID]
	if !ok {
		b.mu.Unlock()
		return nil
	}
	// 对齐 Send 方法：锁内复制 handler 列表，锁外调用
	handlers := make(map[string]MessagerHandler, len(subs))
	for aid, handler := range subs {
		handlers[aid] = handler
	}
	b.mu.Unlock()

	for aid, handler := range handlers {
		if err := handler(ctx, message); err != nil {
			logger.Error(logComponent).Err(err).
				Str("agent_id", aid).Str("topic", topicID).
				Msg("InProcess Publish 处理失败")
		}
	}
	return nil
}

// Subscribe 订阅主题，注册回调。
func (m *InProcessMessager) Subscribe(_ context.Context, topicID string, handler MessagerHandler) error {
	b := getBus()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.topicSubs == nil {
		b.topicSubs = make(map[string]map[string]MessagerHandler)
	}
	if b.topicSubs[topicID] == nil {
		b.topicSubs[topicID] = make(map[string]MessagerHandler)
	}
	b.topicSubs[topicID][m.agentID()] = handler
	m.subscribedTopics = append(m.subscribedTopics, topicID)
	return nil
}

// Unsubscribe 取消订阅。
func (m *InProcessMessager) Unsubscribe(_ context.Context, topicID string) error {
	b := getBus()
	b.mu.Lock()
	defer b.mu.Unlock()
	subs, ok := b.topicSubs[topicID]
	if ok {
		delete(subs, m.agentID())
		if len(subs) == 0 {
			delete(b.topicSubs, topicID)
		}
	}
	// 从已订阅列表中移除
	for i, t := range m.subscribedTopics {
		if t == topicID {
			m.subscribedTopics = append(m.subscribedTopics[:i], m.subscribedTopics[i+1:]...)
			break
		}
	}
	return nil
}

// Send 点对点发送消息给指定 agent。
func (m *InProcessMessager) Send(ctx context.Context, agentID string, message *schema.EventMessage) error {
	b := getBus()
	b.mu.Lock()
	handler, ok := b.p2p[agentID]
	b.mu.Unlock()
	if !ok {
		logger.Warn(logComponent).Str("agent_id", agentID).
			Msg("InProcess Send: 无 P2P handler")
		return nil
	}
	return handler(ctx, message)
}

// RegisterDirectMessageHandler 注册点对点消息回调。
func (m *InProcessMessager) RegisterDirectMessageHandler(_ context.Context, handler MessagerHandler) error {
	b := getBus()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.p2p[m.agentID()] = handler
	return nil
}

// UnregisterDirectMessageHandler 取消注册点对点消息回调。
func (m *InProcessMessager) UnregisterDirectMessageHandler(_ context.Context) error {
	b := getBus()
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.p2p, m.agentID())
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// agentID 返回当前实例的 Agent 标识。
func (m *InProcessMessager) agentID() string {
	return m.config.NodeID
}

// getBus 获取或初始化进程全局 Bus。
func getBus() *bus {
	busOnce.Do(func() {
		globalBus = &bus{
			topicSubs: make(map[string]map[string]MessagerHandler),
			p2p:       make(map[string]MessagerHandler),
		}
	})
	return globalBus
}

// clear 清空 Bus 数据。
func (b *bus) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.topicSubs = nil
	b.p2p = nil
}
