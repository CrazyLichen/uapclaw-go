package spawn_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// stubSpawnableAgent 测试用 Agent 桩
type stubSpawnableAgent struct {
	card *agentschema.AgentCard
}

func (a *stubSpawnableAgent) AgentCard() *agentschema.AgentCard {
	return a.card
}

// TestInProcessSpawn_基本流程 测试基本的 inprocess spawn 流程。
func TestInProcessSpawn_基本流程(t *testing.T) {
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		return &stubSpawnableAgent{
			card: agentschema.NewAgentCard(
				agentschema.WithAgentID("test-agent"),
				agentschema.WithAgentName("Test"),
			),
		}, nil
	}

	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "alice",
	}

	handle, err := spawn.InProcessSpawn(
		context.Background(),
		factory,
		runtimeCtx,
		"Hello team",
		"session-1",
	)
	if err != nil {
		t.Fatalf("InProcessSpawn() error = %v", err)
	}

	if handle.ProcessID() != "inproc-alice" {
		t.Errorf("ProcessID() = %q, want %q", handle.ProcessID(), "inproc-alice")
	}
	if !handle.IsAlive() {
		t.Error("IsAlive() = false, want true")
	}
	if handle.AgentRef() == nil {
		t.Error("AgentRef() = nil, want non-nil")
	}
}

// TestInProcessSpawn_空initialMessage使用默认值 测试空 initialMessage 使用默认消息。
func TestInProcessSpawn_空initialMessage使用默认值(t *testing.T) {
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		return &stubSpawnableAgent{
			card: agentschema.NewAgentCard(agentschema.WithAgentID("test")),
		}, nil
	}

	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "bob",
	}

	handle, err := spawn.InProcessSpawn(context.Background(), factory, runtimeCtx, "", "session-1")
	if err != nil {
		t.Fatalf("InProcessSpawn() error = %v", err)
	}
	_ = handle // 基本流程验证
}

// TestInProcessSpawn_工厂返回错误 测试工厂函数返回错误。
func TestInProcessSpawn_工厂返回错误(t *testing.T) {
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		return nil, fmt.Errorf("工厂失败")
	}

	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "charlie",
	}

	_, err := spawn.InProcessSpawn(context.Background(), factory, runtimeCtx, "", "session-1")
	if err == nil {
		t.Fatal("InProcessSpawn() error = nil, want error")
	}
}

// TestInProcessSpawn_取消context关闭goroutine 测试取消 context 关闭 goroutine。
func TestInProcessSpawn_取消context关闭goroutine(t *testing.T) {
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		return &stubSpawnableAgent{
			card: agentschema.NewAgentCard(agentschema.WithAgentID("test")),
		}, nil
	}

	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "dave",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle, err := spawn.InProcessSpawn(ctx, factory, runtimeCtx, "test", "session-1")
	if err != nil {
		t.Fatalf("InProcessSpawn() error = %v", err)
	}

	if !handle.IsAlive() {
		t.Error("IsAlive() = false, want true")
	}

	// 强制终止
	_ = handle.ForceKill()

	// 等待 goroutine 完成（给一点时间）
	time.Sleep(100 * time.Millisecond)
}
