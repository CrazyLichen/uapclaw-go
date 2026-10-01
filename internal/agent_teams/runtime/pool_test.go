package runtime

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/registry"
)

func TestNewTeamRuntimePool(t *testing.T) {
	p := NewTeamRuntimePool()
	if p == nil {
		t.Error("NewTeamRuntimePool 应返回非 nil")
	}
}

func TestTeamRuntimePool_AddAndGet(t *testing.T) {
	p := NewTeamRuntimePool()
	entry := &ActiveTeam{
		TeamName:     "team-1",
		Agent:        nil,
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	}
	p.Add(entry)

	got := p.Get("team-1")
	if got == nil || got.TeamName != "team-1" {
		t.Error("Get 应返回已添加的条目")
	}
}

func TestTeamRuntimePool_Get不存在(t *testing.T) {
	p := NewTeamRuntimePool()
	if p.Get("ghost") != nil {
		t.Error("不存在的团队应返回 nil")
	}
}

func TestTeamRuntimePool_HasActive(t *testing.T) {
	p := NewTeamRuntimePool()
	if p.HasActive("team-1") {
		t.Error("空池不应有活跃团队")
	}
	p.Add(&ActiveTeam{TeamName: "team-1", InteractGate: NewInteractGate()})
	if !p.HasActive("team-1") {
		t.Error("添加后应有活跃团队")
	}
}

func TestTeamRuntimePool_Remove(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", InteractGate: NewInteractGate()})
	removed := p.Remove("team-1")
	if removed == nil || removed.TeamName != "team-1" {
		t.Error("Remove 应返回被移除的条目")
	}
	if p.HasActive("team-1") {
		t.Error("Remove 后不应有活跃团队")
	}
}

func TestTeamRuntimePool_Remove不存在(t *testing.T) {
	p := NewTeamRuntimePool()
	removed := p.Remove("ghost")
	if removed != nil {
		t.Error("不存在的团队应返回 nil")
	}
}

func TestTeamRuntimePool_Add覆盖(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", SessionID: "sess-1", InteractGate: NewInteractGate()})
	p.Add(&ActiveTeam{TeamName: "team-1", SessionID: "sess-2", InteractGate: NewInteractGate()})
	got := p.Get("team-1")
	if got.SessionID != "sess-2" {
		t.Errorf("SessionID = %v, want sess-2（覆盖）", got.SessionID)
	}
}

func TestTeamRuntimePool_ListTeamNames(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "alpha", InteractGate: NewInteractGate()})
	p.Add(&ActiveTeam{TeamName: "beta", InteractGate: NewInteractGate()})
	names := p.ListTeamNames()
	if len(names) != 2 {
		t.Errorf("len = %d, want 2", len(names))
	}
}

func TestTeamRuntimePool_TeamsForSession(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", SessionID: "sess-1", InteractGate: NewInteractGate()})
	p.Add(&ActiveTeam{TeamName: "team-2", SessionID: "sess-1", InteractGate: NewInteractGate()})
	p.Add(&ActiveTeam{TeamName: "team-3", SessionID: "sess-2", InteractGate: NewInteractGate()})
	teams := p.TeamsForSession("sess-1")
	if len(teams) != 2 {
		t.Errorf("len = %d, want 2", len(teams))
	}
}

func TestTeamRuntimePool_ListAllInfo(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{
		TeamName:     "team-1",
		SessionID:    "sess-1",
		State:        RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	})
	infos := p.ListAllInfo()
	if len(infos) != 1 {
		t.Fatalf("len = %d, want 1", len(infos))
	}
	if infos[0].TeamName != "team-1" {
		t.Errorf("TeamName = %v, want team-1", infos[0].TeamName)
	}
	if infos[0].State != RuntimeStateRunning {
		t.Errorf("State = %v, want RuntimeStateRunning", infos[0].State)
	}
	if infos[0].GateClosed {
		t.Error("GateClosed 应为 false")
	}
}

func TestTeamRuntimePool_满足RegistryPoolEntry(t *testing.T) {
	var _ registry.PoolEntry = (*TeamRuntimePool)(nil)
}

func TestActiveTeam_满足RegistryPoolTeamEntry(t *testing.T) {
	e := &ActiveTeam{SessionID: "sess-1"}
	if e.GetSessionID() != "sess-1" {
		t.Errorf("GetSessionID() = %q, want %q", e.GetSessionID(), "sess-1")
	}
	var _ registry.PoolTeamEntry = (*ActiveTeam)(nil)
}

func TestTeamRuntimePool_GetEntry(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", SessionID: "sess-1", InteractGate: NewInteractGate()})
	entry := p.GetEntry("team-1")
	if entry == nil {
		t.Fatal("GetEntry 应返回非 nil")
	}
	if entry.GetSessionID() != "sess-1" {
		t.Errorf("GetSessionID() = %q, want %q", entry.GetSessionID(), "sess-1")
	}
	if p.GetEntry("ghost") != nil {
		t.Error("不存在的团队应返回 nil")
	}
}

func TestTeamRuntimePool_RemoveEntry(t *testing.T) {
	p := NewTeamRuntimePool()
	p.Add(&ActiveTeam{TeamName: "team-1", InteractGate: NewInteractGate()})
	p.RemoveEntry("team-1")
	if p.HasActive("team-1") {
		t.Error("RemoveEntry 后不应有活跃团队")
	}
}

func TestTeamRuntimeManager_满足RegistryPoolAccessor(t *testing.T) {
	var _ registry.PoolAccessor = (*TeamRuntimeManager)(nil)
	mgr := NewTeamRuntimeManager()
	if mgr.PoolEntry() == nil {
		t.Error("PoolEntry() 应返回非 nil")
	}
	// PoolEntry() 返回的值应满足 PoolEntry
	//nolint:staticcheck // QF1011: 保留类型声明以实现编译时接口检查
	var _ registry.PoolEntry = mgr.PoolEntry()
	_ = context.Background()
}
