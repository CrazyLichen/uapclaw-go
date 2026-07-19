package runtime

import (
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ActiveTeam 活跃团队条目。
// 对齐 Python: ActiveTeam (openjiuwen/agent_teams/runtime/pool.py)
//
// 每个 ActiveTeam 持有一个 TeamAgent Leader 实例、绑定的 session ID、
// 生命周期状态和 InteractGate 门控。
// Pool key 为 team_name——同一团队同时绑定最多一个 session。
type ActiveTeam struct {
	// TeamName 团队名
	TeamName string
	// Agent TeamAgent Leader 实例
	// ⤵️ 待 9.55 回填: *TeamAgent
	Agent any
	// SessionID 当前绑定的 session ID
	SessionID string
	// State 生命周期状态
	State RuntimeState
	// InteractGate 并发门控
	InteractGate *InteractGate
}

// ActiveTeamInfo 活跃团队信息（只读视图）。
// 对齐 Python: ActiveTeamInfo (openjiuwen/agent_teams/runtime/pool.py)
//
// 排除 TeamAgent 引用和 InteractGate，供外部消费者安全读取。
type ActiveTeamInfo struct {
	// TeamName 团队名
	TeamName string
	// SessionID session ID
	SessionID string
	// State 生命周期状态
	State RuntimeState
	// GateClosed 门控是否已关闭
	GateClosed bool
}

// TeamRuntimePool 进程内活跃 TeamAgent 运行时池。
// 对齐 Python: TeamRuntimePool (openjiuwen/agent_teams/runtime/pool.py)
//
// 按 team_name 索引，同一团队同时绑定最多一个 session。
// 切换 session 是硬边界：activate 先 stop_team 旧条目，
// 新 session 通过 CREATE/NEW_TEAM_IN_SESSION/COLD_RECOVER 进入空池槽。
type TeamRuntimePool struct {
	// entries 团队条目映射
	entries map[string]*ActiveTeam
	// mu 读写锁（Python 使用 asyncio.Lock，Go 使用 sync.RWMutex）
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// RuntimeState 运行时状态。
// 对齐 Python: RuntimeState (openjiuwen/agent_teams/runtime/pool.py)
type RuntimeState int

const (
	// RuntimeStateRunning 运行中
	// 对齐 Python: RuntimeState.RUNNING
	RuntimeStateRunning RuntimeState = iota
	// RuntimeStatePaused 已暂停
	// 对齐 Python: RuntimeState.PAUSED
	RuntimeStatePaused
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamRuntimePool 创建运行时池。
// 对齐 Python: TeamRuntimePool.__init__()
func NewTeamRuntimePool() *TeamRuntimePool {
	return &TeamRuntimePool{
		entries: make(map[string]*ActiveTeam),
	}
}

// Get 获取活跃团队。
// 对齐 Python: TeamRuntimePool.get(team_name)
func (p *TeamRuntimePool) Get(teamName string) *ActiveTeam {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.entries[teamName]
}

// HasActive 检查是否有活跃团队。
// 对齐 Python: TeamRuntimePool.has_active(team_name)
func (p *TeamRuntimePool) HasActive(teamName string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.entries[teamName]
	return ok
}

// Add 注册活跃团队（覆盖同名的已有条目）。
// 对齐 Python: TeamRuntimePool.add(entry)
func (p *TeamRuntimePool) Add(entry *ActiveTeam) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[entry.TeamName] = entry
}

// Remove 移除活跃团队并返回条目。
// 对齐 Python: TeamRuntimePool.remove(team_name)
func (p *TeamRuntimePool) Remove(teamName string) *ActiveTeam {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.entries[teamName]
	delete(p.entries, teamName)
	return entry
}

// ListTeamNames 返回所有团队名的快照。
// 对齐 Python: TeamRuntimePool.list_team_names()
func (p *TeamRuntimePool) ListTeamNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	names := make([]string, 0, len(p.entries))
	for name := range p.entries {
		names = append(names, name)
	}
	return names
}

// TeamsForSession 返回绑定到指定 session 的所有活跃团队。
// 对齐 Python: TeamRuntimePool.teams_for_session(session_id)
func (p *TeamRuntimePool) TeamsForSession(sessionID string) []*ActiveTeam {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []*ActiveTeam
	for _, entry := range p.entries {
		if entry.SessionID == sessionID {
			result = append(result, entry)
		}
	}
	return result
}

// ListAllInfo 返回所有活跃团队的只读快照。
// 对齐 Python: TeamRuntimePool.list_all_info()
//
// 排除 TeamAgent 引用，外部消费者不能通过返回条目意外修改运行时状态。
func (p *TeamRuntimePool) ListAllInfo() []ActiveTeamInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]ActiveTeamInfo, 0, len(p.entries))
	for _, entry := range p.entries {
		result = append(result, ActiveTeamInfo{
			TeamName:   entry.TeamName,
			SessionID:  entry.SessionID,
			State:      entry.State,
			GateClosed: entry.InteractGate.Closed(),
		})
	}
	return result
}
