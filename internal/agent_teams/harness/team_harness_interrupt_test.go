package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	harnessworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// newHarnessWithInterruptSession 构造一个持有中断状态的 TeamHarness。
// 直接写 activeAgentSession（同包可见），跳过 prepareAgentSession 的
// PreRun 依赖；被测语义是"从会话里读中断状态"，与会话怎么建出来无关。
func newHarnessWithInterruptSession(t *testing.T, entry *agentschema.ToolInterruptEntry) (*TeamHarness, *session.Session) {
	t.Helper()
	sess := session.CreateAgentSession("sess-1", agentschema.NewAgentCard(agentschema.WithAgentID("agent-1"), agentschema.WithAgentName("agent-1")), nil)
	require.NotNil(t, sess)
	if entry != nil {
		sess.UpdateState(map[string]any{
			agentschema.InterruptionKey: &agentschema.ToolInterruptionState{
				InterruptedTools: map[string]*agentschema.ToolInterruptEntry{"tc-1": entry},
			},
		})
	}
	h := NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	h.setActiveSession(sess)
	return h, sess
}

// TestTeamHarness_HasPendingInterrupt_无会话 无会话时恒为 false（不 panic）。
func TestTeamHarness_HasPendingInterrupt_无会话(t *testing.T) {
	h := NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	assert.False(t, h.HasPendingInterrupt())
}

// TestTeamHarness_HasPendingInterrupt_会话无中断状态 会话存在但未写中断状态 → false。
func TestTeamHarness_HasPendingInterrupt_会话无中断状态(t *testing.T) {
	h, _ := newHarnessWithInterruptSession(t, nil)
	assert.False(t, h.HasPendingInterrupt())
}

// TestTeamHarness_HasPendingInterrupt_会话带中断状态 → true。
// S-01：原实现恒返回 false，导致 team 模式 HITL 恢复整条死链。
func TestTeamHarness_HasPendingInterrupt_会话带中断状态(t *testing.T) {
	h, _ := newHarnessWithInterruptSession(t, &agentschema.ToolInterruptEntry{
		InterruptRequests: map[string]agentschema.InterruptRequester{
			"n-1": &agentschema.InterruptRequest{Message: "确认执行？"},
		},
	})
	assert.True(t, h.HasPendingInterrupt())
}

// TestTeamHarness_IsPendingInterruptResumeValid_子集判定 覆盖 Python issubset 判据。
func TestTeamHarness_IsPendingInterruptResumeValid_子集判定(t *testing.T) {
	entry := &agentschema.ToolInterruptEntry{
		InterruptRequests: map[string]agentschema.InterruptRequester{
			"n-1": &agentschema.InterruptRequest{Message: "问题一"},
			"n-2": &agentschema.InterruptRequest{Message: "问题二"},
		},
	}
	h, _ := newHarnessWithInterruptSession(t, entry)

	// 全部命中
	full, err := interaction.NewInteractiveInput()
	require.NoError(t, err)
	require.NoError(t, full.Update("n-1", "是"))
	require.NoError(t, full.Update("n-2", "否"))
	assert.True(t, h.IsPendingInterruptResumeValid(full))

	// 部分命中（子集）也算有效
	partial, err := interaction.NewInteractiveInput()
	require.NoError(t, err)
	require.NoError(t, partial.Update("n-1", "是"))
	assert.True(t, h.IsPendingInterruptResumeValid(partial))

	// 含未知 id → 无效（Python: not resume_ids.issubset(pending_ids)）
	unknown, err := interaction.NewInteractiveInput()
	require.NoError(t, err)
	require.NoError(t, unknown.Update("n-9", "是"))
	assert.False(t, h.IsPendingInterruptResumeValid(unknown))

	// 空 UserInputs → 无效
	empty, err := interaction.NewInteractiveInput()
	require.NoError(t, err)
	assert.False(t, h.IsPendingInterruptResumeValid(empty))
}

// TestTeamHarness_IsPendingInterruptResumeValid_非交互输入 非 InteractiveInput 一律 false。
func TestTeamHarness_IsPendingInterruptResumeValid_非交互输入(t *testing.T) {
	h, _ := newHarnessWithInterruptSession(t, &agentschema.ToolInterruptEntry{
		InterruptRequests: map[string]agentschema.InterruptRequester{
			"n-1": &agentschema.InterruptRequest{Message: "问题"},
		},
	})
	assert.False(t, h.IsPendingInterruptResumeValid(nil))
	assert.False(t, h.IsPendingInterruptResumeValid("纯文本"))
}

// TestTeamHarness_IsPendingInterruptResumeValid_无待处理请求 中断条目存在但 requests 为空 → false。
func TestTeamHarness_IsPendingInterruptResumeValid_无待处理请求(t *testing.T) {
	h, _ := newHarnessWithInterruptSession(t, &agentschema.ToolInterruptEntry{})
	in, err := interaction.NewInteractiveInput()
	require.NoError(t, err)
	require.NoError(t, in.Update("n-1", "是"))
	assert.False(t, h.IsPendingInterruptResumeValid(in))
}

// TestTeamHarness_RegisterRail_真实DeepAgent Rail 能挂到底层 Agent 上且可注销。
// S-01：原实现是空桩（return nil），Rail 挂载静默无效。
func TestTeamHarness_RegisterRail_真实DeepAgent(t *testing.T) {
	d := agentcore.NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentID("agent-1"), agentschema.WithAgentName("agent-1")))
	require.NotNil(t, d)
	h := NewTeamHarness(d, nil, string(atschema.TeamRoleLeader), "leader_1", false)

	rail := agentinterfaces.NewBaseRail()
	require.NoError(t, h.RegisterRail(context.Background(), rail))
	require.NoError(t, h.UnregisterRail(context.Background(), rail))
}

// TestTeamHarness_interruptSession_优先LoopSession 有 loop_session 时优先用它。
func TestTeamHarness_interruptSession_优先LoopSession(t *testing.T) {
	d := agentcore.NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentID("agent-1"), agentschema.WithAgentName("agent-1")))
	require.NotNil(t, d)
	h, activeSess := newHarnessWithInterruptSession(t, nil)
	h.deepAgent = d

	// 未绑定 loop_session → 回退到 activeAgentSession
	assert.Same(t, activeSess, h.interruptSession())
}

// TestTeamHarness_会话字段并发读写 轮次 goroutine 写、消息 goroutine 读必须无数据竞争。
// S-02：字段原来是 any 且无锁，接口值的两字写入可能被读到撕裂值。
// 本用例在 -race 下运行；不加 -race 时至少验证不会 panic。
func TestTeamHarness_会话字段并发读写(t *testing.T) {
	h := NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	sessions := make([]*session.Session, 4)
	for i := range sessions {
		sessions[i] = session.CreateAgentSession("sess-"+string(rune('a'+i)), nil, nil)
	}

	var wg sync.WaitGroup
	// 写侧：模拟每轮 prepareAgentSession
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			h.setActiveSession(sessions[i%len(sessions)])
		}
	}()
	// 读侧：模拟 Steer / 邮箱轮询
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = h.getActiveSession()
				_ = h.HasPendingInterrupt()
			}
		}()
	}
	wg.Wait()

	assert.NotNil(t, h.getActiveSession())
}

// TestTeamHarness_InitCwdForRound_每轮重置 上一轮的 cd 不得跨轮残留。
// Python: init_cwd_for_round() 每轮用 workspace 根重建 CwdState；
// Go 侧若只返回不改 ctx，工具会继续读上一轮被 cd 改过的状态。
func TestTeamHarness_InitCwdForRound_每轮重置(t *testing.T) {
	root := t.TempDir()
	d := agentcore.NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentID("agent-cwd"), agentschema.WithAgentName("agent-cwd")))
	require.NotNil(t, d)
	cfg := hschema.NewDeepAgentConfig()
	cfg.Workspace = &harnessworkspace.Workspace{RootPath: root}
	require.NoError(t, d.ConfigureDeepConfig(context.Background(), cfg))

	h := NewTeamHarness(d, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	round1 := h.InitCwdForRound(context.Background())
	state := cwd.CwdStateFromCtx(round1)
	require.NotNil(t, state, "本轮 ctx 必须携带新的 CwdState")
	cwd1, errCwd1 := cwd.GetCwd(round1)
	require.NoError(t, errCwd1)
	assert.Equal(t, root, cwd1)

	// 模拟第一轮里 cd 到子目录
	state.SetCwd(root + "/sub")
	cwd1b, errCwd1b := cwd.GetCwd(round1)
	require.NoError(t, errCwd1b)
	assert.NotEqual(t, root, cwd1b)

	// 第二轮必须回到 workspace 根，而不是沿用上一轮被改过的状态
	round2 := h.InitCwdForRound(context.Background())
	cwd2, errCwd2 := cwd.GetCwd(round2)
	require.NoError(t, errCwd2)
	assert.Equal(t, root, cwd2)
}

// TestTeamHarness_InitCwdForRound_无工作空间 无 workspace 时原样返回 ctx。
func TestTeamHarness_InitCwdForRound_无工作空间(t *testing.T) {
	h := NewTeamHarness(nil, nil, string(atschema.TeamRoleLeader), "leader_1", false)
	ctx := context.Background()
	assert.Equal(t, ctx, h.InitCwdForRound(ctx))
}
