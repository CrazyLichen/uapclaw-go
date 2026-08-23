package runtime

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// newTestTeamBackendForRuntime 创建测试用的 TeamBackend。
func newTestTeamBackendForRuntime() *tools.TeamBackend {
	db := database.NewInMemoryTeamDatabase()
	msg := messager.NewInProcessMessager(atschema.NewMessagerTransportConfig())
	tb := tools.NewTeamBackend("team-1", "leader", true, db, msg)
	ctx := context.Background()
	tb.BuildTeam(ctx, "Test Team", "desc", "Leader", "leader desc", nil)
	return tb
}

// newTestAgent 创建测试用的 TeamAgent（设置 TeamBackend）。
func newTestAgent(backend *tools.TeamBackend) *agent.TeamAgent {
	a := agent.NewTeamAgent(&agentschema.AgentCard{})
	a.SetTeamBackend(backend)
	return a
}

func TestNewTeamRuntimeManager(t *testing.T) {
	m := NewTeamRuntimeManager()
	if m == nil {
		t.Error("NewTeamRuntimeManager 应返回非 nil")
	}
	if m.Pool() == nil {
		t.Error("Pool 不应为 nil")
	}
}

func TestTeamRuntimeManager_Interact_团队不存在(t *testing.T) {
	m := NewTeamRuntimeManager()
	result, err := m.Interact(context.Background(), "hello", "ghost", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("不活跃的团队应返回失败")
	}
	if result.Reason == nil || *result.Reason != "not_active" {
		t.Errorf("Reason = %v, want not_active", result.Reason)
	}
}

func TestTeamRuntimeManager_Interact_字符串输入(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	result, err := m.Interact(context.Background(), "hello leader", "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_GodView载荷(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	payload := interaction.NewGodViewMessage("hello")
	result, err := m.Interact(context.Background(), payload, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_Operator载荷(t *testing.T) {
	tb := newTestTeamBackendForRuntime()
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
		Agent:        newTestAgent(tb),
	}
	m.Pool().Add(entry)

	target := "alice"
	payload := interaction.NewOperatorMessage("hello", &target)
	result, err := m.Interact(context.Background(), payload, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_Operator广播载荷(t *testing.T) {
	tb := newTestTeamBackendForRuntime()
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
		Agent:        newTestAgent(tb),
	}
	m.Pool().Add(entry)

	payload := interaction.NewOperatorMessage("hello all", nil)
	result, err := m.Interact(context.Background(), payload, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_门控关闭(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	// 关闭门控
	_ = entry.InteractGate.CloseAndDrain(context.Background())

	result, err := m.Interact(context.Background(), "hello", "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("门控关闭时应返回失败")
	}
	if result.Reason == nil || *result.Reason != "gate_closed" {
		t.Errorf("Reason = %v, want gate_closed", result.Reason)
	}
}

func TestTeamRuntimeManager_Interact_InteractiveInput(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	input, _ := sessioninteraction.NewInteractiveInput("resume data")
	result, err := m.Interact(context.Background(), input, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	// stub 总是返回成功
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true (stub)", result.IsOK())
	}
}

func TestTeamRuntimeManager_Interact_session不匹配(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	result, err := m.Interact(context.Background(), "hello", "team-1", "wrong-session")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("session 不匹配应返回 not_active")
	}
	if result.Reason == nil || *result.Reason != "not_active" {
		t.Errorf("Reason = %v, want not_active", result.Reason)
	}
}

func TestTeamRuntimeManager_Interact_不支持的载荷类型(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	result, err := m.Interact(context.Background(), 12345, "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.IsOK() {
		t.Error("不支持的载荷类型应返回失败")
	}
	if result.Reason == nil || *result.Reason != "unsupported_payload_type" {
		t.Errorf("Reason = %v, want unsupported_payload_type", result.Reason)
	}
}

func TestTeamRuntimeManager_Interact_井号字符串解析(t *testing.T) {
	m := NewTeamRuntimeManager()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	m.Pool().Add(entry)

	// "# hello" → GodViewMessage
	result, err := m.Interact(context.Background(), "# hello", "team-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsOK() {
		t.Errorf("IsOK = %v, want true", result.IsOK())
	}
}

// ──────────── 生命周期 stub 测试 ────────────

func TestTeamRuntimeManager_Activate_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	err := m.Activate(context.Background(), "team-1", "sess-1", nil)
	if err != nil {
		t.Errorf("Activate stub 应返回 nil, got %v", err)
	}
}

func TestTeamRuntimeManager_Pause_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	_, err := m.Pause(context.Background(), "team-1", "sess-1")
	if err != nil {
		t.Errorf("Pause stub 应返回 nil, got %v", err)
	}
}

func TestTeamRuntimeManager_StopTeam_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	_, err := m.StopTeam(context.Background(), "team-1", "sess-1")
	if err != nil {
		t.Errorf("StopTeam stub 应返回 nil, got %v", err)
	}
}

func TestTeamRuntimeManager_DeleteTeam_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	_, err := m.DeleteTeam(context.Background(), "team-1", "sess-1")
	if err != nil {
		t.Errorf("DeleteTeam stub 应返回 nil, got %v", err)
	}
}

func TestTeamRuntimeManager_Finalize_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	err := m.Finalize(context.Background(), "team-1", "sess-1")
	if err != nil {
		t.Errorf("Finalize stub 应返回 nil, got %v", err)
	}
}

func TestTeamRuntimeManager_RegisterHumanAgentInbound_stub(t *testing.T) {
	m := NewTeamRuntimeManager()
	ok, err := m.RegisterHumanAgentInbound(context.Background(), "team-1", "sess-1", "human_agent", nil)
	if err != nil {
		t.Errorf("RegisterHumanAgentInbound stub 应返回 nil error, got %v", err)
	}
	if ok {
		t.Error("RegisterHumanAgentInbound stub 应返回 false")
	}
}
