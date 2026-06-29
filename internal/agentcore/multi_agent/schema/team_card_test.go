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
	agentCard := agentschema.NewAgentCard(schema.WithName("agent1"))
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
