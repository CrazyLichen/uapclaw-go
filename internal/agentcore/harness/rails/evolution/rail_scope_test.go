package evolution

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// rail_scope_test.go 订阅窗口机械测试（对齐 evolution_rail.py 语义）。

// newScopeTestRail 构造完成一次 BeforeInvoke 的 rail。
func newScopeTestRail(t *testing.T, sessionID string) *EvolutionRail {
	t.Helper()
	rail := NewEvolutionRail(noOpExtension{})
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: sessionID})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))
	require.NotNil(t, rail.builder)
	return rail
}

// recordToolSpan 经 AfterToolCall 记录一个工具 span（走真实钩子链）。
func recordToolSpan(t *testing.T, rail *EvolutionRail, toolName string) {
	t.Helper()
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{ToolName: toolName})
	require.NoError(t, rail.AfterToolCall(context.Background(), cbc))
}

// TestScopeWindow_跨钩子增量合并 多个钩子的 span 合并进同一窗口。
// Python: _merge_clean_increment。
func TestScopeWindow_跨钩子增量合并(t *testing.T) {
	rail := newScopeTestRail(t, "sess-scope")
	recordToolSpan(t, rail, "write_file")
	recordToolSpan(t, rail, "read_file")

	capture := rail.currentCapture
	require.NotNil(t, capture)
	window, err := rail.projectScopeWindow(capture)
	require.NoError(t, err)
	require.NotNil(t, window)
	spans := trajectory.IterSpans(window)
	assert.Len(t, spans, 2)
	// 规范元数据信封：session/来源。
	if window.SessionID() == nil || *window.SessionID() != "sess-scope" {
		t.Fatalf("window session = %v", window.SessionID())
	}
	assert.Equal(t, "online", window.ResourceAttributes()[trajectory.TrajectorySource])
}

// TestScopeWindow_游标幂等 同一 span 不重复并入窗口。
func TestScopeWindow_游标幂等(t *testing.T) {
	rail := newScopeTestRail(t, "sess-scope")
	recordToolSpan(t, rail, "write_file")
	// 二次合并（无新 span）不改变窗口。
	_, err := rail.mergeScopeIncrement(rail.currentCapture)
	require.NoError(t, err)
	window, err := rail.projectScopeWindow(rail.currentCapture)
	require.NoError(t, err)
	assert.Len(t, trajectory.IterSpans(window), 1)
}

// TestScopeWindow_收口后保留 AfterInvoke 后窗口保留。
// Python: after_invoke finally 仅退订（窗口保留）。
func TestScopeWindow_收口后保留(t *testing.T) {
	rail := newScopeTestRail(t, "sess-scope")
	recordToolSpan(t, rail, "write_file")
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-scope"})
	require.NoError(t, rail.AfterInvoke(context.Background(), cbc))

	assert.True(t, rail.currentCapture == nil)
	window, err := rail.GetTrajectory("sess-scope", "", "")
	require.NoError(t, err)
	require.NotNil(t, window)
	assert.Len(t, trajectory.IterSpans(window), 1)
}

// TestGetTrajectory_团队与成员互斥报错 同传报错。
// Python: get_trajectory ValueError。
func TestGetTrajectory_团队与成员互斥报错(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	_, err := rail.GetTrajectory("sess", "m1", "team1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "member_id must be omitted")
}

// TestGetTrajectory_无窗口返回nil 未采集的 scope 返回 nil。
func TestGetTrajectory_无窗口返回nil(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	traj, err := rail.GetTrajectory("sess-none", "", "")
	require.NoError(t, err)
	assert.Nil(t, traj)
}

// TestGetTrajectory_元数据信封 窗口携带规范资源元数据。
func TestGetTrajectory_元数据信封(t *testing.T) {
	rail := newScopeTestRail(t, "sess-meta")
	recordToolSpan(t, rail, "write_file")

	traj, err := rail.GetTrajectory("sess-meta", "", "")
	require.NoError(t, err)
	require.NotNil(t, traj)
	attrs := traj.ResourceAttributes()
	assert.Equal(t, "sess-meta", attrs[trajectory.SessionID])
	assert.Equal(t, "online", attrs[trajectory.TrajectorySource])
	assert.NotEmpty(t, attrs[trajectory.TrajectoryID])
}

// TestScopeWindow_裁剪上限 maxTrajectorySteps 作用到窗口。
func TestScopeWindow_裁剪上限(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{}, WithMaxTrajectorySteps(2))
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-trim"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))
	recordToolSpan(t, rail, "t1")
	t.Logf("DBG after t1: builder=%d window=%d", len(rail.builder.Spans()), func() int {
		w, _ := rail.projectScopeWindow(rail.currentCapture)
		if w == nil {
			return -1
		}
		return len(trajectory.IterSpans(w))
	}())
	recordToolSpan(t, rail, "t2")

	recordToolSpan(t, rail, "t3")

	window, err := rail.projectScopeWindow(rail.currentCapture)
	require.NoError(t, err)
	require.NotNil(t, window)
	assert.Len(t, trajectory.IterSpans(window), 2)
}

// TestDemand_并发同实例 并发获取 demand 单例（对齐 demand.py 单锁语义）。
func TestDemand_并发同实例(t *testing.T) {
	// demand 包的并发语义在 observability/demand 包测试覆盖；此处冒烟
	// rail 侧 processor 缝的线程安全（并发 mergeScopeIncrement）。
	rail := newScopeTestRail(t, "sess-conc")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recordToolSpan(t, rail, "tool")
			_, _ = rail.mergeScopeIncrement(rail.currentCapture)
		}()
	}
	wg.Wait()
	window, err := rail.projectScopeWindow(rail.currentCapture)
	require.NoError(t, err)
	require.NotNil(t, window)
	// 游标保证无重复：8 span 全部入窗。
	assert.Len(t, trajectory.IterSpans(window), 8)
}

// TestScopeProcessor_Demand懒构造 rail 经 demand 懒构造取得进程级 processor。
func TestScopeProcessor_Demand懒构造(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	assert.False(t, rail.scopeProcessor() == nil)
}
