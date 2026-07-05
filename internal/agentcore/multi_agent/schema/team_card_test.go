package schema

import (
	"encoding/json"
	"fmt"
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTeamCard_默认值 验证默认 Version="1.0.0"，其他字段为零值。
func TestNewTeamCard_默认值(t *testing.T) {
	card := NewTeamCard()
	if card.Version != "1.0.0" {
		t.Errorf("期望 Version='1.0.0'，实际 '%s'", card.Version)
	}
	if card.Topic != "" {
		t.Errorf("期望 Topic=''，实际 '%s'", card.Topic)
	}
	if card.AgentCards != nil {
		t.Errorf("期望 AgentCards=nil，实际 %v", card.AgentCards)
	}
	if card.Tags != nil {
		t.Errorf("期望 Tags=nil，实际 %v", card.Tags)
	}
	// BaseCard 字段
	if card.ID == "" {
		t.Error("期望 ID 非空")
	}
}

// TestNewTeamCard_带选项 验证所有 TeamCardOption（含 BaseCard 字段选项）。
func TestNewTeamCard_带选项(t *testing.T) {
	agentCard := agentschema.NewAgentCard(agentschema.WithAgentName("agent1"))
	cards := []*agentschema.AgentCard{agentCard}

	card := NewTeamCard(
		WithTeamCardName("my-team"),
		WithTeamCardDescription("测试团队"),
		WithAgentCards(cards),
		WithTopic("coding"),
		WithTeamVersion("2.0.0"),
		WithTags([]string{"tag1", "tag2"}),
	)

	if card.Name != "my-team" {
		t.Errorf("期望 Name='my-team'，实际 '%s'", card.Name)
	}
	if card.Description != "测试团队" {
		t.Errorf("期望 Description='测试团队'，实际 '%s'", card.Description)
	}
	if len(card.AgentCards) != 1 {
		t.Fatalf("期望 len(AgentCards)=1，实际 %d", len(card.AgentCards))
	}
	if card.AgentCards[0].Name != "agent1" {
		t.Errorf("期望 AgentCards[0].Name='agent1'，实际 '%s'", card.AgentCards[0].Name)
	}
	if card.Topic != "coding" {
		t.Errorf("期望 Topic='coding'，实际 '%s'", card.Topic)
	}
	if card.Version != "2.0.0" {
		t.Errorf("期望 Version='2.0.0'，实际 '%s'", card.Version)
	}
	if len(card.Tags) != 2 || card.Tags[0] != "tag1" {
		t.Errorf("期望 Tags=['tag1','tag2']，实际 %v", card.Tags)
	}
}

// TestNewTeamCard_WithTeamCardID 验证 WithTeamCardID 覆盖自动生成的 UUID。
func TestNewTeamCard_WithTeamCardID(t *testing.T) {
	card := NewTeamCard(WithTeamCardID("custom-id-123"))
	if card.ID != "custom-id-123" {
		t.Errorf("期望 ID='custom-id-123'，实际 '%s'", card.ID)
	}
}

// TestTeamCard_String 验证 fmt.Stringer 输出。
func TestTeamCard_String(t *testing.T) {
	card := NewTeamCard(
		WithTeamCardName("team1"),
		WithTopic("math"),
		WithTeamVersion("1.0.0"),
	)
	s := fmt.Sprintf("%v", card)
	if s == "" {
		t.Error("String() 不应返回空字符串")
	}
	// 验证包含关键字段
	if card.ID != "" && !contains(s, card.ID) {
		t.Errorf("String() 应包含 ID='%s'，实际 '%s'", card.ID, s)
	}
	if !contains(s, "team1") {
		t.Errorf("String() 应包含 Name='team1'，实际 '%s'", s)
	}
	if !contains(s, "math") {
		t.Errorf("String() 应包含 Topic='math'，实际 '%s'", s)
	}
}

// TestTeamCard_JSON序列化 验证 JSON marshal/unmarshal 和 omitempty。
func TestTeamCard_JSON序列化(t *testing.T) {
	card := NewTeamCard(
		WithTeamCardName("team1"),
		WithTopic("coding"),
		WithTeamVersion("2.0.0"),
		WithTags([]string{"ai"}),
	)

	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded TeamCard
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.Name != "team1" {
		t.Errorf("期望 Name='team1'，实际 '%s'", decoded.Name)
	}
	if decoded.Topic != "coding" {
		t.Errorf("期望 Topic='coding'，实际 '%s'", decoded.Topic)
	}
	if decoded.Version != "2.0.0" {
		t.Errorf("期望 Version='2.0.0'，实际 '%s'", decoded.Version)
	}
	if len(decoded.Tags) != 1 || decoded.Tags[0] != "ai" {
		t.Errorf("期望 Tags=['ai']，实际 %v", decoded.Tags)
	}
}

// TestTeamCard_JSON序列化_omitempty 验证零值字段不出现在 JSON 中。
func TestTeamCard_JSON序列化_omitempty(t *testing.T) {
	card := NewTeamCard() // Topic/Tags/AgentCards 全零值
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal map 失败: %v", err)
	}
	if _, ok := m["agent_cards"]; ok {
		t.Error("零值 AgentCards 不应出现在 JSON 中")
	}
	if _, ok := m["topic"]; ok {
		t.Error("零值 Topic 不应出现在 JSON 中")
	}
	if _, ok := m["tags"]; ok {
		t.Error("零值 Tags 不应出现在 JSON 中")
	}
}

// TestTeamCard_嵌入BaseCard 验证嵌入后 ID/Name/Description 可访问。
func TestTeamCard_嵌入BaseCard(t *testing.T) {
	card := NewTeamCard(WithTeamCardID("abc123"), WithTeamCardName("n"), WithTeamCardDescription("d"))
	if card.ID != "abc123" {
		t.Errorf("期望 ID='abc123'，实际 '%s'", card.ID)
	}
	if card.Name != "n" {
		t.Errorf("期望 Name='n'，实际 '%s'", card.Name)
	}
	if card.Description != "d" {
		t.Errorf("期望 Description='d'，实际 '%s'", card.Description)
	}
}

// TestTeamCardInterface_TeamCard满足接口 验证 *TeamCard 满足 TeamCardInterface。
func TestTeamCardInterface_TeamCard满足接口(t *testing.T) {
	card := NewTeamCard(WithTopic("test"))
	var iface TeamCardInterface = card
	if iface.GetTopic() != "test" {
		t.Errorf("GetTopic() = %q, want %q", iface.GetTopic(), "test")
	}
	if iface.GetSubscriptions() != nil {
		t.Error("TeamCard.GetSubscriptions() 应返回 nil")
	}
}

// TestTeamCard_满足CardInterface 验证 *TeamCard 满足 schema.CardInterface。
func TestTeamCard_满足CardInterface(t *testing.T) {
	card := NewTeamCard(WithTeamCardID("tc-1"), WithTeamCardName("tc-name"))
	var iface schema.CardInterface = card
	if iface.GetID() != "tc-1" {
		t.Errorf("GetID() = %q, want %q", iface.GetID(), "tc-1")
	}
	if iface.GetName() != "tc-name" {
		t.Errorf("GetName() = %q, want %q", iface.GetName(), "tc-name")
	}
}

// TestTeamCard_GetSubscriptions_返回nil 验证 TeamCard.GetSubscriptions() 返回 nil。
func TestTeamCard_GetSubscriptions_返回nil(t *testing.T) {
	card := NewTeamCard()
	if subs := card.GetSubscriptions(); subs != nil {
		t.Errorf("期望 nil，实际 %v", subs)
	}
}

// TestTeamCard_GetAgentCards 验证 GetAgentCards() 返回 AgentCards 字段。
func TestTeamCard_GetAgentCards(t *testing.T) {
	agentCard := agentschema.NewAgentCard(agentschema.WithAgentName("a1"))
	card := NewTeamCard(WithAgentCards([]*agentschema.AgentCard{agentCard}))
	if got := card.GetAgentCards(); len(got) != 1 || got[0].Name != "a1" {
		t.Errorf("GetAgentCards() = %v, want 1 个 agent a1", got)
	}
}

// TestTeamCard_GetTopic 验证 GetTopic() 返回 Topic 字段。
func TestTeamCard_GetTopic(t *testing.T) {
	card := NewTeamCard(WithTopic("math"))
	if got := card.GetTopic(); got != "math" {
		t.Errorf("GetTopic() = %q, want %q", got, "math")
	}
}

// TestTeamCard_GetVersion 验证 GetVersion() 返回 Version 字段。
func TestTeamCard_GetVersion(t *testing.T) {
	card := NewTeamCard(WithTeamVersion("2.0.0"))
	if got := card.GetVersion(); got != "2.0.0" {
		t.Errorf("GetVersion() = %q, want %q", got, "2.0.0")
	}
}

// TestTeamCard_GetTags 验证 GetTags() 返回 Tags 字段。
func TestTeamCard_GetTags(t *testing.T) {
	card := NewTeamCard(WithTags([]string{"ai", "ml"}))
	if got := card.GetTags(); len(got) != 2 || got[0] != "ai" {
		t.Errorf("GetTags() = %v, want [ai ml]", got)
	}
}

// TestNewEventDrivenTeamCard_默认值 验证默认 Version="1.0.0"，Subscriptions=nil。
func TestNewEventDrivenTeamCard_默认值(t *testing.T) {
	card := NewEventDrivenTeamCard()
	if card.Version != "1.0.0" {
		t.Errorf("期望 Version='1.0.0'，实际 '%s'", card.Version)
	}
	if card.Subscriptions != nil {
		t.Errorf("期望 Subscriptions=nil，实际 %v", card.Subscriptions)
	}
	if card.Topic != "" {
		t.Errorf("期望 Topic=''，实际 '%s'", card.Topic)
	}
	if card.ID == "" {
		t.Error("期望 ID 非空")
	}
}

// TestNewEventDrivenTeamCard_带选项 验证所有 EventDrivenTeamCardOption。
func TestNewEventDrivenTeamCard_带选项(t *testing.T) {
	agentCard := agentschema.NewAgentCard(agentschema.WithAgentName("agent1"))
	subs := map[string][]string{
		"reviewer": {"code_events", "task_updates"},
		"coder":    {"review_events"},
	}
	card := NewEventDrivenTeamCard(
		WithEventDrivenID("team-123"),
		WithEventDrivenName("event-team"),
		WithEventDrivenDescription("事件驱动团队"),
		WithEventDrivenAgentCards([]*agentschema.AgentCard{agentCard}),
		WithEventDrivenTopic("coding"),
		WithEventDrivenTeamVersion("2.0.0"),
		WithEventDrivenTags([]string{"event", "driven"}),
		WithSubscriptions(subs),
	)
	if card.ID != "team-123" {
		t.Errorf("期望 ID='team-123'，实际 '%s'", card.ID)
	}
	if card.Name != "event-team" {
		t.Errorf("期望 Name='event-team'，实际 '%s'", card.Name)
	}
	if card.Description != "事件驱动团队" {
		t.Errorf("期望 Description='事件驱动团队'，实际 '%s'", card.Description)
	}
	if len(card.AgentCards) != 1 || card.AgentCards[0].Name != "agent1" {
		t.Errorf("期望 AgentCards[0].Name='agent1'，实际 %v", card.AgentCards)
	}
	if card.Topic != "coding" {
		t.Errorf("期望 Topic='coding'，实际 '%s'", card.Topic)
	}
	if card.Version != "2.0.0" {
		t.Errorf("期望 Version='2.0.0'，实际 '%s'", card.Version)
	}
	if len(card.Tags) != 2 || card.Tags[0] != "event" {
		t.Errorf("期望 Tags=['event','driven']，实际 %v", card.Tags)
	}
	if len(card.Subscriptions) != 2 {
		t.Errorf("期望 len(Subscriptions)=2，实际 %d", len(card.Subscriptions))
	}
	if topics, ok := card.Subscriptions["reviewer"]; !ok || len(topics) != 2 {
		t.Errorf("期望 reviewer 订阅 2 个 topic，实际 %v", topics)
	}
}

// TestEventDrivenTeamCard_GetSubscriptions 验证 GetSubscriptions() 返回实际订阅映射。
func TestEventDrivenTeamCard_GetSubscriptions(t *testing.T) {
	subs := map[string][]string{"agent1": {"topic1"}}
	card := NewEventDrivenTeamCard(WithSubscriptions(subs))
	if got := card.GetSubscriptions(); len(got) != 1 || got["agent1"][0] != "topic1" {
		t.Errorf("GetSubscriptions() = %v, want agent1→[topic1]", got)
	}
}

// TestEventDrivenTeamCard_满足CardInterface 验证 *EventDrivenTeamCard 满足 schema.CardInterface。
func TestEventDrivenTeamCard_满足CardInterface(t *testing.T) {
	card := NewEventDrivenTeamCard(WithEventDrivenID("ed-1"), WithEventDrivenName("ed-name"))
	var iface schema.CardInterface = card
	if iface.GetID() != "ed-1" {
		t.Errorf("GetID() = %q, want %q", iface.GetID(), "ed-1")
	}
	if iface.GetName() != "ed-name" {
		t.Errorf("GetName() = %q, want %q", iface.GetName(), "ed-name")
	}
}

// TestEventDrivenTeamCard_String 验证 String() 包含 subscriptions 数量。
func TestEventDrivenTeamCard_String(t *testing.T) {
	subs := map[string][]string{"a": {"t1"}, "b": {"t2"}}
	card := NewEventDrivenTeamCard(
		WithEventDrivenName("team1"),
		WithEventDrivenTopic("math"),
		WithSubscriptions(subs),
	)
	s := fmt.Sprintf("%v", card)
	if !contains(s, "team1") {
		t.Errorf("String() 应包含 Name='team1'，实际 '%s'", s)
	}
	if !contains(s, "math") {
		t.Errorf("String() 应包含 Topic='math'，实际 '%s'", s)
	}
	if !contains(s, "subscriptions=2") {
		t.Errorf("String() 应包含 subscriptions=2，实际 '%s'", s)
	}
}

// TestEventDrivenTeamCard_JSON序列化 验证 JSON marshal/unmarshal。
func TestEventDrivenTeamCard_JSON序列化(t *testing.T) {
	subs := map[string][]string{"reviewer": {"code_events"}}
	card := NewEventDrivenTeamCard(
		WithEventDrivenName("event-team"),
		WithEventDrivenTopic("coding"),
		WithEventDrivenTeamVersion("2.0.0"),
		WithSubscriptions(subs),
	)
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var decoded EventDrivenTeamCard
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if decoded.Name != "event-team" {
		t.Errorf("期望 Name='event-team'，实际 '%s'", decoded.Name)
	}
	if decoded.Topic != "coding" {
		t.Errorf("期望 Topic='coding'，实际 '%s'", decoded.Topic)
	}
	if len(decoded.Subscriptions) != 1 || decoded.Subscriptions["reviewer"][0] != "code_events" {
		t.Errorf("期望 Subscriptions[reviewer]=[code_events]，实际 %v", decoded.Subscriptions)
	}
}

// TestEventDrivenTeamCard_JSON序列化_omitempty 验证零值 Subscriptions 不出现在 JSON 中。
func TestEventDrivenTeamCard_JSON序列化_omitempty(t *testing.T) {
	card := NewEventDrivenTeamCard() // Subscriptions 为 nil
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal map 失败: %v", err)
	}
	if _, ok := m["subscriptions"]; ok {
		t.Error("零值 Subscriptions 不应出现在 JSON 中")
	}
}

// TestEventDrivenTeamCard_嵌入TeamCard 验证嵌入后 TeamCard 字段可访问。
func TestEventDrivenTeamCard_嵌入TeamCard(t *testing.T) {
	card := NewEventDrivenTeamCard(
		WithEventDrivenID("abc123"),
		WithEventDrivenName("n"),
		WithEventDrivenDescription("d"),
	)
	if card.ID != "abc123" {
		t.Errorf("期望 ID='abc123'，实际 '%s'", card.ID)
	}
	if card.Name != "n" {
		t.Errorf("期望 Name='n'，实际 '%s'", card.Name)
	}
	if card.Description != "d" {
		t.Errorf("期望 Description='d'，实际 '%s'", card.Description)
	}
}

// TestTeamCardInterface_EventDrivenTeamCard满足接口 验证 *EventDrivenTeamCard 满足 TeamCardInterface。
func TestTeamCardInterface_EventDrivenTeamCard满足接口(t *testing.T) {
	subs := map[string][]string{"agent1": {"topic1"}}
	card := NewEventDrivenTeamCard(
		WithEventDrivenName("ed-team"),
		WithEventDrivenTopic("coding"),
		WithSubscriptions(subs),
	)
	var iface TeamCardInterface = card
	if iface.GetName() != "ed-team" {
		t.Errorf("GetName() = %q, want %q", iface.GetName(), "ed-team")
	}
	if iface.GetTopic() != "coding" {
		t.Errorf("GetTopic() = %q, want %q", iface.GetTopic(), "coding")
	}
	if got := iface.GetSubscriptions(); len(got) != 1 || got["agent1"][0] != "topic1" {
		t.Errorf("GetSubscriptions() = %v, want agent1→[topic1]", got)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// contains 检查字符串是否包含子串。
func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
