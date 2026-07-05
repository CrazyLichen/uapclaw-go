package team_runtime

import (
	"strings"
	"sync"

	"github.com/danwakefield/fnmatch"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubscriptionInfo 单个 Agent 的订阅信息。
type SubscriptionInfo struct {
	// AgentID Agent 唯一标识
	AgentID string
	// Topics 订阅的主题列表
	Topics []string
}

// SubscriptionMap 全量订阅信息。
type SubscriptionMap struct {
	// Subscriptions 主题模式 → 订阅者 Agent ID 列表
	Subscriptions map[string][]string
}

// SubscriptionManager 主题到 Agent 的订阅映射管理器，支持 fnmatch 通配符匹配。
//
// 维护双向索引实现高效查找和移除：
//   - subscriptions: topic_pattern → agent_id 集合（正查：模式查订阅者）
//   - agentTopics: agent_id → topic_pattern 集合（反查：Agent 查其订阅的模式）
//
// 对应 Python: SubscriptionManager (openjiuwen/core/multi_agent/team_runtime/subscription_manager.py)
type SubscriptionManager struct {
	// subscriptions 主题模式到 Agent ID 集合的映射
	subscriptions map[string]map[string]struct{}
	// agentTopics Agent ID 到主题模式集合的映射
	agentTopics map[string]map[string]struct{}
	// mu 读写互斥锁
	mu sync.RWMutex
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// logComponent 日志组件，agentcore 下统一使用 ComponentAgentCore
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSubscriptionManager 创建订阅管理器实例。
func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		subscriptions: make(map[string]map[string]struct{}),
		agentTopics:   make(map[string]map[string]struct{}),
	}
}

// Subscribe 将 Agent 订阅到主题模式，同时维护双向索引。
//
// 对应 Python: SubscriptionManager.subscribe
func (m *SubscriptionManager) Subscribe(agentID, topicPattern string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.subscriptions[topicPattern]; !ok {
		m.subscriptions[topicPattern] = make(map[string]struct{})
	}
	m.subscriptions[topicPattern][agentID] = struct{}{}

	if _, ok := m.agentTopics[agentID]; !ok {
		m.agentTopics[agentID] = make(map[string]struct{})
	}
	m.agentTopics[agentID][topicPattern] = struct{}{}

	logger.Debug(logComponent).
		Str("agent_id", agentID).
		Str("topic_pattern", topicPattern).
		Msg("Agent 订阅主题")
}

// Unsubscribe 将 Agent 从主题模式取消订阅，清理双向索引，空集合自动删除。
//
// 对应 Python: SubscriptionManager.unsubscribe
func (m *SubscriptionManager) Unsubscribe(agentID, topicPattern string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if agents, ok := m.subscriptions[topicPattern]; ok {
		delete(agents, agentID)
		if len(agents) == 0 {
			delete(m.subscriptions, topicPattern)
		}
	}

	if topics, ok := m.agentTopics[agentID]; ok {
		delete(topics, topicPattern)
		if len(topics) == 0 {
			delete(m.agentTopics, agentID)
		}
	}

	logger.Debug(logComponent).
		Str("agent_id", agentID).
		Str("topic_pattern", topicPattern).
		Msg("Agent 取消订阅主题")
}

// UnsubscribeAll 移除 Agent 的所有订阅。
//
// 对应 Python: SubscriptionManager.unsubscribe_all
func (m *SubscriptionManager) UnsubscribeAll(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	topics, ok := m.agentTopics[agentID]
	if !ok {
		return
	}

	for topic := range topics {
		if agents, ok := m.subscriptions[topic]; ok {
			delete(agents, agentID)
			if len(agents) == 0 {
				delete(m.subscriptions, topic)
			}
		}
	}

	delete(m.agentTopics, agentID)

	logger.Debug(logComponent).
		Str("agent_id", agentID).
		Msg("移除 Agent 的所有订阅")
}

// GetSubscribers 获取匹配指定主题的所有订阅者 Agent ID。
//
// 遍历所有 pattern 进行 fnmatch 匹配，返回去重后的 Agent ID 列表。
//
// 对应 Python: SubscriptionManager.get_subscribers
func (m *SubscriptionManager) GetSubscribers(topicID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subscribers := make(map[string]struct{})
	for pattern, agents := range m.subscriptions {
		if matchPattern(topicID, pattern) {
			for agentID := range agents {
				subscribers[agentID] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(subscribers))
	for agentID := range subscribers {
		result = append(result, agentID)
	}

	logger.Debug(logComponent).
		Str("topic_id", topicID).
		Int("subscriber_count", len(result)).
		Msg("查找主题订阅者")

	return result
}

// GetSubscriptionCount 获取总订阅数。
//
// 对应 Python: SubscriptionManager.get_subscription_count
func (m *SubscriptionManager) GetSubscriptionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, agents := range m.subscriptions {
		count += len(agents)
	}
	return count
}

// ListSubscriptions 列出订阅信息用于调试，支持按 agentID 过滤。
//
// 对应 Python: SubscriptionManager.list_subscriptions
func (m *SubscriptionManager) ListSubscriptions(agentID string) any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if agentID != "" {
		topics := make([]string, 0)
		if ts, ok := m.agentTopics[agentID]; ok {
			for t := range ts {
				topics = append(topics, t)
			}
		}
		return &SubscriptionInfo{
			AgentID: agentID,
			Topics:  topics,
		}
	}

	subs := make(map[string][]string)
	for pattern, agents := range m.subscriptions {
		agentList := make([]string, 0, len(agents))
		for a := range agents {
			agentList = append(agentList, a)
		}
		subs[pattern] = agentList
	}
	return &SubscriptionMap{
		Subscriptions: subs,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// matchPattern 判断主题 ID 是否匹配订阅模式。
//
// 精确匹配优先，含 * 或 ? 时使用 fnmatch 通配符匹配。
//
// 对应 Python: SubscriptionManager._match_pattern
func matchPattern(topicID, pattern string) bool {
	if pattern == topicID {
		return true
	}
	if strings.ContainsRune(pattern, '*') || strings.ContainsRune(pattern, '?') {
		return fnmatch.Match(pattern, topicID, 0)
	}
	return false
}
