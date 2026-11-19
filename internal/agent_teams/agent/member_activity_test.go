package agent

import (
	"context"
	"testing"

	atharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// TestMemberActivityRegistry_三信号 对齐 MemberActivityRegistry.record：
// 移动→CANCEL 武装、静止但未武装→NONE、静止且已武装→SCHEDULE（恰一次）。
// Python: member_activity.py L115-143
func TestMemberActivityRegistry_三信号(t *testing.T) {
	r := NewMemberActivityRegistry("leader")
	// 新构造：只有 leader（UNSTARTED）——平凡静止但未武装
	if got := r.Record("leader", atschema.MemberStatusUnstarted); got != IdleSignalNone {
		t.Errorf("未武装的静止应返回 NONE: %v", got)
	}
	if got := r.Record("bob", atschema.MemberStatusBusy); got != IdleSignalCancel {
		t.Errorf("移动应返回 CANCEL 并武装: %v", got)
	}
	if got := r.Record("bob", atschema.MemberStatusReady); got != IdleSignalSchedule {
		t.Errorf("武装后的静止应返回 SCHEDULE: %v", got)
	}
	if got := r.Record("bob", atschema.MemberStatusReady); got != IdleSignalNone {
		t.Errorf("SCHEDULE 后再次静止应返回 NONE（恰一次/边）: %v", got)
	}
}

// TestMemberActivityRegistry_Seed保留leader 基线缺拥有者成员时保留之。
// Python: seed L94-113。
func TestMemberActivityRegistry_Seed保留leader(t *testing.T) {
	r := NewMemberActivityRegistry("leader")
	r.Seed(map[string]atschema.MemberStatus{"bob": atschema.MemberStatusBusy})
	snap := r.Snapshot()
	if _, ok := snap["leader"]; !ok {
		t.Error("基线缺 leader 时应保留 leader 在名册")
	}
	if snap["bob"] != string(atschema.MemberStatusBusy) {
		t.Errorf("基线成员应入库: %v", snap)
	}
	if r.IsIdle() {
		t.Error("BUSY 成员在册时不应 idle")
	}
	r.Seed(nil)
	if !r.IsIdle() {
		t.Error("空基线（全部静止）应 idle")
	}
}

// TestParseMemberStatus 合法值与未知值。
func TestParseMemberStatus(t *testing.T) {
	if s, ok := atschema.ParseMemberStatus("ready"); !ok || s != atschema.MemberStatusReady {
		t.Errorf("ready 应解析: %v %v", s, ok)
	}
	if s, ok := atschema.ParseMemberStatus("dormant"); !ok || s != atschema.MemberStatusDormant {
		t.Errorf("dormant 应解析: %v %v", s, ok)
	}
	if _, ok := atschema.ParseMemberStatus("nope"); ok {
		t.Error("未知值应 ok=false")
	}
}

// TestIdleSince_置位与刷新 对齐 mapState 打点/清空 + refresh_idle_baseline：
// RUNNING 清零、IDLE 打点；refresh 只在已打点时重置。
// Python: stream_controller.py L450-457 + team_agent.py L522-535。
func TestIdleSince_置位与刷新(t *testing.T) {
	a := &TeamAgent{state: NewTeamAgentState()}
	sc := NewStreamController(
		nil, a.state, nil,
		func(_ context.Context, _ atschema.MemberStatus) error { return nil },
		func(_ context.Context, _ atschema.ExecutionStatus) error { return nil },
		WithIdleDebounceSeconds(0.01),
	)
	stateEvent := func(new harnessprotocol.HarnessState) atharness.HarnessStateEvent {
		return atharness.HarnessStateEvent{New: new}
	}

	// IDLE 打点
	sc.mapState(context.Background(), stateEvent(harnessprotocol.HarnessStateIdle))
	if a.state.IdleSince.IsZero() {
		t.Error("IDLE 应打 idle_since")
	}
	first := a.state.IdleSince

	// RUNNING 清零
	sc.mapState(context.Background(), stateEvent(harnessprotocol.HarnessStateRunning))
	if !a.state.IdleSince.IsZero() {
		t.Error("RUNNING 应清 idle_since")
	}

	// 未打点时 refresh 是 no-op
	a.RefreshIdleBaseline()
	if !a.state.IdleSince.IsZero() {
		t.Error("零值时 refresh 不应打点")
	}

	// 打点后 refresh 重置基线
	sc.mapState(context.Background(), stateEvent(harnessprotocol.HarnessStateIdle))
	if a.state.IdleSince.Equal(first) {
		t.Error("重新 IDLE 应重新打点")
	}
	a.RefreshIdleBaseline()
	if a.state.IdleSince.Equal(first) {
		t.Error("refresh 应重置基线")
	}
}
