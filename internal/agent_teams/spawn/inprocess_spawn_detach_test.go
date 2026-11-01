package spawn

import (
	"context"
	"testing"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeSpawnable 测试用 Agent，只提供 AgentCard。
type fakeSpawnable struct{}

// AgentCard 返回占位身份卡。
func (f *fakeSpawnable) AgentCard() *agentschema.AgentCard {
	return agentschema.NewAgentCard(agentschema.WithAgentID("fake"), agentschema.WithAgentName("fake"))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInProcessSpawn_父ctx取消后成员ctx不被取消 覆盖 S-46 的 detached 语义。
//
// Python 的 asyncio.create_task 不随调用方取消；Go 若用 WithCancel(ctx) 派生
// 成员 runCtx，成员会继承调用方取消（实测工具 ctx 进入 spawn 时已 canceled，
// 导致成员 Invoke 直接返回、轮次产出被丢弃）。这里断言父 ctx 取消后，
// 成员内部 ctx.Err() 仍为 nil，且 handle 仍存活。
func TestInProcessSpawn_父ctx取消后成员ctx不被取消(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	innerErr := make(chan error, 1)

	SetRunTeamMemberFunc(func(ctx context.Context, _ SpawnableAgent, _ map[string]any, _ string) (map[string]any, error) {
		close(started)
		<-release
		innerErr <- ctx.Err()
		return map[string]any{"ok": true}, nil
	})
	defer SetRunTeamMemberFunc(nil)

	parent, cancelParent := context.WithCancel(context.Background())
	handle, err := InProcessSpawn(
		parent,
		func(_ atschema.TeamRuntimeContext) (SpawnableAgent, error) { return &fakeSpawnable{}, nil },
		atschema.TeamRuntimeContext{MemberName: "fake"},
		"",
		"sess-detached",
	)
	if err != nil {
		t.Fatalf("InProcessSpawn 失败: %v", err)
	}

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("成员未启动")
	}

	// 取消父 ctx：成员内部 ctx 不应被连带取消
	cancelParent()
	close(release)

	select {
	case got := <-innerErr:
		if got != nil {
			t.Fatalf("父 ctx 取消后成员 ctx 应为 nil，实际: %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("成员未结束")
	}

	if handle == nil {
		t.Fatal("InProcessSpawn 应返回非 nil handle")
	}
}
