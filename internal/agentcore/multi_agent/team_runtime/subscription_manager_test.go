package team_runtime

import (
	"sort"
	"testing"
)

// TestNewSubscriptionManager 测试创建订阅管理器实例
func TestNewSubscriptionManager(t *testing.T) {
	m := NewSubscriptionManager()
	if m == nil {
		t.Fatal("NewSubscriptionManager() 返回 nil")
	}
	if m.subscriptions == nil {
		t.Error("subscriptions 未初始化")
	}
	if m.agentTopics == nil {
		t.Error("agentTopics 未初始化")
	}
	if m.GetSubscriptionCount() != 0 {
		t.Errorf("新实例订阅数 = %d, want 0", m.GetSubscriptionCount())
	}
}

// TestSubscriptionManager_Subscribe_Unsubscribe 测试基本订阅和取消订阅操作
func TestSubscriptionManager_Subscribe_Unsubscribe(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic-1")
	if m.GetSubscriptionCount() != 1 {
		t.Errorf("订阅后计数 = %d, want 1", m.GetSubscriptionCount())
	}

	subs := m.GetSubscribers("topic-1")
	if len(subs) != 1 || subs[0] != "agent-1" {
		t.Errorf("GetSubscribers = %v, want [agent-1]", subs)
	}

	m.Unsubscribe("agent-1", "topic-1")
	if m.GetSubscriptionCount() != 0 {
		t.Errorf("取消订阅后计数 = %d, want 0", m.GetSubscriptionCount())
	}

	subs = m.GetSubscribers("topic-1")
	if len(subs) != 0 {
		t.Errorf("取消订阅后 GetSubscribers = %v, want []", subs)
	}
}

// TestSubscriptionManager_UnsubscribeAll 测试移除 Agent 的所有订阅
func TestSubscriptionManager_UnsubscribeAll(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic-1")
	m.Subscribe("agent-1", "topic-2")
	m.Subscribe("agent-2", "topic-1")

	if m.GetSubscriptionCount() != 3 {
		t.Errorf("订阅后计数 = %d, want 3", m.GetSubscriptionCount())
	}

	m.UnsubscribeAll("agent-1")

	if m.GetSubscriptionCount() != 1 {
		t.Errorf("UnsubscribeAll 后计数 = %d, want 1", m.GetSubscriptionCount())
	}

	subs := m.GetSubscribers("topic-1")
	if len(subs) != 1 || subs[0] != "agent-2" {
		t.Errorf("topic-1 订阅者 = %v, want [agent-2]", subs)
	}

	subs = m.GetSubscribers("topic-2")
	if len(subs) != 0 {
		t.Errorf("topic-2 订阅者 = %v, want []", subs)
	}
}

// TestSubscriptionManager_GetSubscribers_精确匹配 测试精确主题匹配
func TestSubscriptionManager_GetSubscribers_精确匹配(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic-1")
	m.Subscribe("agent-2", "topic-2")

	subs := m.GetSubscribers("topic-1")
	if len(subs) != 1 || subs[0] != "agent-1" {
		t.Errorf("GetSubscribers(topic-1) = %v, want [agent-1]", subs)
	}

	subs = m.GetSubscribers("topic-2")
	if len(subs) != 1 || subs[0] != "agent-2" {
		t.Errorf("GetSubscribers(topic-2) = %v, want [agent-2]", subs)
	}

	subs = m.GetSubscribers("topic-3")
	if len(subs) != 0 {
		t.Errorf("GetSubscribers(topic-3) = %v, want []", subs)
	}
}

// TestSubscriptionManager_GetSubscribers_通配符星号 测试星号通配符匹配
func TestSubscriptionManager_GetSubscribers_通配符星号(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic.*")
	m.Subscribe("agent-2", "event.*")

	subs := m.GetSubscribers("topic.update")
	if len(subs) != 1 || subs[0] != "agent-1" {
		t.Errorf("GetSubscribers(topic.update) = %v, want [agent-1]", subs)
	}

	subs = m.GetSubscribers("event.create")
	if len(subs) != 1 || subs[0] != "agent-2" {
		t.Errorf("GetSubscribers(event.create) = %v, want [agent-2]", subs)
	}

	subs = m.GetSubscribers("other.thing")
	if len(subs) != 0 {
		t.Errorf("GetSubscribers(other.thing) = %v, want []", subs)
	}
}

// TestSubscriptionManager_GetSubscribers_通配符问号 测试问号通配符匹配
func TestSubscriptionManager_GetSubscribers_通配符问号(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic-?")

	subs := m.GetSubscribers("topic-1")
	if len(subs) != 1 || subs[0] != "agent-1" {
		t.Errorf("GetSubscribers(topic-1) = %v, want [agent-1]", subs)
	}

	subs = m.GetSubscribers("topic-A")
	if len(subs) != 1 || subs[0] != "agent-1" {
		t.Errorf("GetSubscribers(topic-A) = %v, want [agent-1]", subs)
	}

	// 问号仅匹配单个字符
	subs = m.GetSubscribers("topic-12")
	if len(subs) != 0 {
		t.Errorf("GetSubscribers(topic-12) = %v, want []", subs)
	}
}

// TestSubscriptionManager_双向索引一致性 测试双向索引始终保持一致
func TestSubscriptionManager_双向索引一致性(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic-1")
	m.Subscribe("agent-1", "topic-2")
	m.Subscribe("agent-2", "topic-1")

	// 通过 ListSubscriptions 验证双向索引
	info1 := m.ListSubscriptions("agent-1")
	topics1 := sortStrings(info1["topics"].([]string))
	if len(topics1) != 2 || topics1[0] != "topic-1" || topics1[1] != "topic-2" {
		t.Errorf("agent-1 的主题 = %v, want [topic-1, topic-2]", topics1)
	}

	info2 := m.ListSubscriptions("agent-2")
	topics2 := info2["topics"].([]string)
	if len(topics2) != 1 || topics2[0] != "topic-1" {
		t.Errorf("agent-2 的主题 = %v, want [topic-1]", topics2)
	}

	// 取消订阅后双向索引应同步清理
	m.Unsubscribe("agent-1", "topic-1")
	info1 = m.ListSubscriptions("agent-1")
	topics1 = info1["topics"].([]string)
	if len(topics1) != 1 || topics1[0] != "topic-2" {
		t.Errorf("取消订阅后 agent-1 的主题 = %v, want [topic-2]", topics1)
	}

	// topic-1 的订阅者中不应再有 agent-1
	subs := m.GetSubscribers("topic-1")
	if len(subs) != 1 || subs[0] != "agent-2" {
		t.Errorf("取消订阅后 topic-1 订阅者 = %v, want [agent-2]", subs)
	}
}

// TestSubscriptionManager_GetSubscriptionCount 测试总订阅数统计
func TestSubscriptionManager_GetSubscriptionCount(t *testing.T) {
	m := NewSubscriptionManager()

	if count := m.GetSubscriptionCount(); count != 0 {
		t.Errorf("初始计数 = %d, want 0", count)
	}

	m.Subscribe("agent-1", "topic-1")
	m.Subscribe("agent-1", "topic-2")
	m.Subscribe("agent-2", "topic-1")

	if count := m.GetSubscriptionCount(); count != 3 {
		t.Errorf("订阅后计数 = %d, want 3", count)
	}

	m.Unsubscribe("agent-1", "topic-1")
	if count := m.GetSubscriptionCount(); count != 2 {
		t.Errorf("取消订阅后计数 = %d, want 2", count)
	}

	m.UnsubscribeAll("agent-2")
	if count := m.GetSubscriptionCount(); count != 1 {
		t.Errorf("UnsubscribeAll 后计数 = %d, want 1", count)
	}
}

// TestSubscriptionManager_ListSubscriptions 测试调试用的订阅列表
func TestSubscriptionManager_ListSubscriptions(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic-1")
	m.Subscribe("agent-1", "topic-2")
	m.Subscribe("agent-2", "topic-1")

	t.Run("按 agentID 过滤", func(t *testing.T) {
		info := m.ListSubscriptions("agent-1")
		if info["agent_id"] != "agent-1" {
			t.Errorf("agent_id = %v, want agent-1", info["agent_id"])
		}
		topics := sortStrings(info["topics"].([]string))
		if len(topics) != 2 || topics[0] != "topic-1" || topics[1] != "topic-2" {
			t.Errorf("topics = %v, want [topic-1, topic-2]", topics)
		}
	})

	t.Run("不存在的 agentID", func(t *testing.T) {
		info := m.ListSubscriptions("agent-unknown")
		if info["agent_id"] != "agent-unknown" {
			t.Errorf("agent_id = %v, want agent-unknown", info["agent_id"])
		}
		topics := info["topics"].([]string)
		if len(topics) != 0 {
			t.Errorf("topics = %v, want []", topics)
		}
	})

	t.Run("空 agentID 返回全部订阅", func(t *testing.T) {
		info := m.ListSubscriptions("")
		subsMap, ok := info["subscriptions"].(map[string][]string)
		if !ok {
			t.Fatalf("subscriptions 类型错误: %T", info["subscriptions"])
		}
		if len(subsMap) != 2 {
			t.Errorf("订阅模式数 = %d, want 2", len(subsMap))
		}
		agents1 := sortStrings(subsMap["topic-1"])
		if len(agents1) != 2 || agents1[0] != "agent-1" || agents1[1] != "agent-2" {
			t.Errorf("topic-1 订阅者 = %v, want [agent-1, agent-2]", agents1)
		}
	})
}

// TestSubscriptionManager_空集合自动清理 测试取消订阅后空集合自动删除
func TestSubscriptionManager_空集合自动清理(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic-1")

	// 取消订阅后，subscriptions 和 agentTopics 中空集合应被删除
	m.Unsubscribe("agent-1", "topic-1")

	m.mu.RLock()
	_, hasPattern := m.subscriptions["topic-1"]
	_, hasAgent := m.agentTopics["agent-1"]
	m.mu.RUnlock()

	if hasPattern {
		t.Error("取消订阅后 subscriptions 中仍存在空集合 topic-1")
	}
	if hasAgent {
		t.Error("取消订阅后 agentTopics 中仍存在空集合 agent-1")
	}
}

// TestSubscriptionManager_重复订阅幂等 测试重复订阅的幂等性
func TestSubscriptionManager_重复订阅幂等(t *testing.T) {
	m := NewSubscriptionManager()

	m.Subscribe("agent-1", "topic-1")
	m.Subscribe("agent-1", "topic-1")
	m.Subscribe("agent-1", "topic-1")

	if count := m.GetSubscriptionCount(); count != 1 {
		t.Errorf("重复订阅后计数 = %d, want 1", count)
	}

	info := m.ListSubscriptions("agent-1")
	topics := info["topics"].([]string)
	if len(topics) != 1 || topics[0] != "topic-1" {
		t.Errorf("重复订阅后主题 = %v, want [topic-1]", topics)
	}
}

// sortStrings 对字符串切片排序，用于测试结果确定性
func sortStrings(s []string) []string {
	sorted := make([]string, len(s))
	copy(sorted, s)
	sort.Strings(sorted)
	return sorted
}
