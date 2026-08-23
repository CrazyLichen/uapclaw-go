package interaction

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// newTestTeamBackendForInteraction 创建测试用的 TeamBackend。
func newTestTeamBackendForInteraction() *tools.TeamBackend {
	db := database.NewInMemoryTeamDatabase()
	msg := messager.NewInProcessMessager(atschema.NewMessagerTransportConfig())
	tb := tools.NewTeamBackend("test-team", "leader", true, db, msg, tools.WithEnableHITT(true))
	ctx := context.Background()
	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	tb.SpawnHumanAgent(ctx, "human_agent", "Human Agent", "", "")
	return tb
}

// newTestAgentForInteraction 创建测试用的 TeamAgent。
func newTestAgentForInteraction() *agent.TeamAgent {
	a := agent.NewTeamAgent(&agentschema.AgentCard{})
	return a
}

func TestHumanAgentNotEnabledError(t *testing.T) {
	err := &HumanAgentNotEnabledError{}
	if err.Error() == "" {
		t.Error("Error() 不应为空")
	}
	err2 := &HumanAgentNotEnabledError{Message: "custom msg"}
	if err2.Error() != "custom msg" {
		t.Errorf("Error() = %v, want custom msg", err2.Error())
	}
}

func TestUnknownHumanAgentError(t *testing.T) {
	err := &UnknownHumanAgentError{Sender: "ghost", Registered: []string{"human_agent"}}
	if err.Error() == "" {
		t.Error("Error() 不应为空")
	}
	// 应包含发送者名
	if err.Error() != "'ghost' is not a registered human-agent member; registered members: [human_agent]" {
		t.Errorf("Error() = %v", err.Error())
	}
}

func TestNewHumanAgentInbox(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	h := NewHumanAgentInbox(tb, tb.MessageManager(), nil, nil)
	if h == nil {
		t.Error("NewHumanAgentInbox 应返回非 nil")
	}
}

func TestHumanAgentInbox_Send_驱动avatar(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	var lookedUp string
	lookup := func(sender string) *agent.TeamAgent {
		lookedUp = sender
		return newTestAgentForInteraction() // 非 nil 表示有活跃运行时
	}
	h := NewHumanAgentInbox(tb, tb.MessageManager(), lookup, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
	if lookedUp != "human_agent" {
		t.Errorf("lookedUp = %v, want human_agent", lookedUp)
	}
	// 对齐 Python: deliver_to_leader 通道不产生 bus message → MessageID 为 nil
	if result.MessageID != nil {
		t.Errorf("MessageID = %v, want nil (drive avatar)", result.MessageID)
	}
}

func TestHumanAgentInbox_Send_广播(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	h := NewHumanAgentInbox(tb, tb.MessageManager(), nil, nil)
	target := "all"
	result, err := h.Send("hello all", &target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestHumanAgentInbox_Send_无lookup时驱动失败(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	h := NewHumanAgentInbox(tb, tb.MessageManager(), nil, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("无 agentLookup 时驱动 avatar 应失败")
	}
	if result.Reason == nil || *result.Reason != "agent_unavailable" {
		t.Errorf("Reason = %v, want agent_unavailable", result.Reason)
	}
}

func TestHumanAgentInbox_Send_lookup返回nil(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	lookup := func(sender string) *agent.TeamAgent { return nil }
	h := NewHumanAgentInbox(tb, tb.MessageManager(), lookup, nil)
	result, err := h.Send("hello", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("agentLookup 返回 nil 时应失败")
	}
	if result.Reason == nil || *result.Reason != "agent_unavailable" {
		t.Errorf("Reason = %v, want agent_unavailable", result.Reason)
	}
}

func TestHumanAgentInbox_Send_未知发送者(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	h := NewHumanAgentInbox(tb, tb.MessageManager(), nil, nil)
	sender := "ghost"
	_, err := h.Send("hello", nil, &sender)
	if err == nil {
		t.Error("未知发送者应返回错误")
	}
	if _, ok := err.(*UnknownHumanAgentError); !ok {
		t.Errorf("err 类型 = %T, want *UnknownHumanAgentError", err)
	}
}

func TestHumanAgentInbox_Send_指定发送者(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	lookup := func(sender string) *agent.TeamAgent { return newTestAgentForInteraction() }
	h := NewHumanAgentInbox(tb, tb.MessageManager(), lookup, nil)
	sender := "human_agent"
	result, err := h.Send("hello", nil, &sender)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestHumanAgentInbox_Send_点对点(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	// 创建 alice 成员
	tb.SpawnMember(context.Background(), "alice", "Alice", "", string(atschema.TeamRoleTeammate), "", "", "")
	h := NewHumanAgentInbox(tb, tb.MessageManager(), nil, nil)
	target := "alice"
	result, err := h.Send("hello", &target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestHumanAgentInbox_GetOnInbound(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	cb := func(event HumanAgentInboundEvent) error { return nil }
	h := NewHumanAgentInbox(tb, tb.MessageManager(), nil, cb)
	if h.GetOnInbound() == nil {
		t.Error("GetOnInbound 不应为 nil")
	}
}

func TestHumanAgentInbox_GetOnInbound_nil(t *testing.T) {
	tb := newTestTeamBackendForInteraction()
	h := NewHumanAgentInbox(tb, tb.MessageManager(), nil, nil)
	if h.GetOnInbound() != nil {
		t.Error("GetOnInbound 应为 nil")
	}
}
