package session

import (
	"context"
	"sync"
)

// ──────────────────────────── 接口 ────────────────────────────

// TeamExecutionController 团队执行控制面（由 TeamManager 实现）。
// Python: runtime/session_delete.py TeamExecutionController
type TeamExecutionController interface {
	// QuiesceForDelete 删除前静默团队执行
	QuiesceForDelete(ctx context.Context, target SessionLifecycleTarget, reason string) error
	// DisposeAfterResourceRelease 资源释放后销毁团队执行
	DisposeAfterResourceRelease(ctx context.Context, target SessionLifecycleTarget, reason string) error
	// DeleteAborted 删除已中止通知
	DeleteAborted(target SessionLifecycleTarget)
	// DeleteCommitted 删除已提交通知
	DeleteCommitted(target SessionLifecycleTarget)
}

// ──────────────────────────── 结构体 ────────────────────────────

// SessionDeleteResult 一次永久会话删除的传输无关结果。
// Python: runtime/session_delete.py SessionDeleteResult
type SessionDeleteResult struct {
	// OK 操作是否成功
	OK bool
	// SessionID 会话标识
	SessionID string
	// ChannelID 渠道标识
	ChannelID string
	// IsTeam 是否团队会话
	IsTeam bool
	// TeamName 团队名
	TeamName string
	// Deleted 是否已删除
	Deleted bool
	// RecoveryRequired 是否需要恢复处理（破坏性操作已开始但失败）
	RecoveryRequired bool
	// ErrorCode 错误码
	ErrorCode string
	// ErrorMessage 错误说明
	ErrorMessage string
}

// TeamDeleteResult 一次团队永久删除的传输无关结果。
// Python: runtime/session_delete.py TeamDeleteResult
type TeamDeleteResult struct {
	// OK 操作是否成功
	OK bool
	// TeamName 团队名
	TeamName string
	// SessionIDs 团队下全部会话标识
	SessionIDs []string
	// FailedSessionIDs 删除失败的会话标识
	FailedSessionIDs []string
	// Deleted 是否已删除
	Deleted bool
	// RecoveryRequired 是否需要恢复处理
	RecoveryRequired bool
	// ErrorCode 错误码
	ErrorCode string
	// ErrorMessage 错误说明
	ErrorMessage string
}

// TeamDeletionInProgress 团队变更与进程内永久删除竞争。
// Python: runtime/session_delete.py TeamDeletionInProgress
type TeamDeletionInProgress struct {
	// TeamName 团队名
	TeamName string
}

// TeamDeletionGate 团队删除/创建/绑定路径共享的进程级变更围栏。
// Python: runtime/session_delete.py TeamDeletionGate
type TeamDeletionGate struct {
	// mu 保护以下字段
	mu sync.Mutex
	// locks 团队名 → 变更锁
	locks map[string]*sync.Mutex
	// deleting 正在删除的团队名集合
	deleting map[string]struct{}
}

// lockTable 带互斥保护的锁表（替代上游 WeakValueDictionary）。
// Go 无弱引用，条目需由调用方显式回收（spec §8 D-6）。
type lockTable struct {
	// mu 保护 m
	mu sync.Mutex
	// m 键 → 锁
	m map[string]*sync.Mutex
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sessionDeleteLocks 会话删除锁表（会话标识 → 锁）
	sessionDeleteLocks = &lockTable{m: map[string]*sync.Mutex{}}
	// teamDeleteLocks 团队删除锁表（团队名 → 锁）
	teamDeleteLocks = &lockTable{m: map[string]*sync.Mutex{}}
	// globalTeamDeletionGate 进程级团队删除围栏
	globalTeamDeletionGate = NewTeamDeletionGate()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口，文案与上游逐字一致。
func (e *TeamDeletionInProgress) Error() string {
	return "TEAM_DELETING: team is being permanently deleted: " + e.TeamName
}

// Code 稳定错误码。
func (e *TeamDeletionInProgress) Code() string { return "TEAM_DELETING" }

// SessionDeleteResultFailure 构造失败结果。
// Python: SessionDeleteResult.failure
func SessionDeleteResultFailure(sessionID, code, message string) SessionDeleteResult {
	return SessionDeleteResult{
		OK:           false,
		SessionID:    sessionID,
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

// NewTeamDeletionGate 创建删除围栏。
// Python: TeamDeletionGate.__init__
func NewTeamDeletionGate() *TeamDeletionGate {
	return &TeamDeletionGate{
		locks:    map[string]*sync.Mutex{},
		deleting: map[string]struct{}{},
	}
}

// MutationLock 取团队的变更锁。
// Python: TeamDeletionGate.mutation_lock
func (g *TeamDeletionGate) MutationLock(teamName string) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	lock, ok := g.locks[teamName]
	if !ok {
		lock = &sync.Mutex{}
		g.locks[teamName] = lock
	}
	return lock
}

// BeginDeleteLocked 标记团队开始删除，返回是否首次标记。
// Python: TeamDeletionGate.begin_delete_locked
func (g *TeamDeletionGate) BeginDeleteLocked(teamName string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, existed := g.deleting[teamName]
	g.deleting[teamName] = struct{}{}
	return !existed
}

// AssertNotDeletingLocked 团队正在删除时返回错误。
// Python: TeamDeletionGate.assert_not_deleting_locked
func (g *TeamDeletionGate) AssertNotDeletingLocked(teamName string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, deleting := g.deleting[teamName]; deleting {
		return &TeamDeletionInProgress{TeamName: teamName}
	}
	return nil
}

// FinishDeleteLocked 清除删除标记。
// Python: TeamDeletionGate.finish_delete_locked
func (g *TeamDeletionGate) FinishDeleteLocked(teamName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.deleting, teamName)
}

// GlobalTeamDeletionGate 返回进程级团队删除围栏。
// Python: runtime/session_delete.py TEAM_DELETION_GATE
func GlobalTeamDeletionGate() *TeamDeletionGate { return globalTeamDeletionGate }

// SessionDeleteLock 取会话删除锁（同一标识返回同一把）。
// Python: runtime/session_delete.py session_delete_lock
func SessionDeleteLock(sessionID string) *sync.Mutex {
	return sessionDeleteLocks.lockFor(sessionID)
}

// TeamDeleteLock 取团队删除锁（同一名称返回同一把）。
// Python: runtime/session_delete.py team_delete_lock
func TeamDeleteLock(teamName string) *sync.Mutex {
	return teamDeleteLocks.lockFor(teamLockKey(teamName))
}

// ReleaseSessionDeleteLock 移除会话删除锁条目（删除提交后调用，避免无限增长）。
// Go 无弱引用，需显式回收（spec §8 D-6）。
func ReleaseSessionDeleteLock(sessionID string) {
	sessionDeleteLocks.release(sessionID)
}

// ReleaseTeamDeleteLock 移除团队删除锁条目。
// Go 无弱引用，需显式回收（spec §8 D-6）。
func ReleaseTeamDeleteLock(teamName string) {
	teamDeleteLocks.release(teamLockKey(teamName))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// teamLockKey 团队锁表键（加前缀避免与会话锁表键语义混淆）。
func teamLockKey(teamName string) string { return "team:" + teamName }

// lockFor 取或创建锁。
func (t *lockTable) lockFor(key string) *sync.Mutex {
	t.mu.Lock()
	defer t.mu.Unlock()
	lock, ok := t.m[key]
	if !ok {
		lock = &sync.Mutex{}
		t.m[key] = lock
	}
	return lock
}

// release 移除锁条目。
func (t *lockTable) release(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, key)
}
