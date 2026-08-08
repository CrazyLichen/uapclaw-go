package trajectory

import (
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberTrajectorySnapshot 团队成员在单个会话中的最新有界轨迹视图。
//
// 对应 Python: MemberTrajectorySnapshot dataclass
type MemberTrajectorySnapshot struct {
	// TeamID 团队标识
	TeamID string
	// SessionID 会话标识
	SessionID string
	// MemberID 成员标识
	MemberID string
	// MemberRole 成员角色（可选）
	MemberRole string
	// Trajectory 成员轨迹
	Trajectory *Trajectory
	// RecordedAtMs 记录时间（毫秒时间戳）
	RecordedAtMs int
}

// TrajectorySink 成员轨迹快照写入端点。
//
// 对应 Python: TrajectorySink(Protocol)
type TrajectorySink interface {
	// PublishMemberTrajectory 发布成员最新轨迹快照。
	PublishMemberTrajectory(snapshot *MemberTrajectorySnapshot)
}

// TrajectorySource 聚合轨迹证据读取端点。
//
// 对应 Python: TrajectorySource(Protocol)
type TrajectorySource interface {
	// GetTrajectory 返回指定会话的聚合团队轨迹。
	GetTrajectory(teamID, sessionID string, filterCollaborative bool) *Trajectory
}

// InMemoryTrajectoryRegistry 内存轨迹注册表，同时实现 TrajectorySink 和 TrajectorySource。
//
// 对应 Python: InMemoryTrajectoryRegistry
type InMemoryTrajectoryRegistry struct {
	// snapshots 快照表，(teamID, sessionID) → memberID → snapshotEntry
	snapshots map[registryKey]map[string]*snapshotEntry
	// sequence 全局递增序列号
	sequence int
	// mu 读写锁
	mu sync.RWMutex
}

// registryKey 注册表键。
type registryKey struct {
	teamID    string
	sessionID string
}

// snapshotEntry 注册表内部快照条目，包含排序元数据。
//
// 对应 Python: _SnapshotEntry dataclass
type snapshotEntry struct {
	snapshot *MemberTrajectorySnapshot
	sequence int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberTrajectorySnapshot 创建成员轨迹快照，填充运行时默认值。
//
// 对齐 Python: MemberTrajectorySnapshot.make()
func NewMemberTrajectorySnapshot(
	teamID, memberID string,
	trajectory *Trajectory,
	memberRole string,
	sessionID string,
	recordedAtMs int,
) *MemberTrajectorySnapshot {
	sid := sessionID
	if sid == "" {
		sid = trajectory.SessionID
	}
	ts := recordedAtMs
	if ts == 0 {
		ts = NowMs()
	}
	return &MemberTrajectorySnapshot{
		TeamID:       teamID,
		SessionID:    sid,
		MemberID:     memberID,
		MemberRole:   memberRole,
		Trajectory:   trajectory,
		RecordedAtMs: ts,
	}
}

// NewInMemoryTrajectoryRegistry 创建内存轨迹注册表。
//
// 对应 Python: InMemoryTrajectoryRegistry()
func NewInMemoryTrajectoryRegistry() *InMemoryTrajectoryRegistry {
	return &InMemoryTrajectoryRegistry{
		snapshots: make(map[registryKey]map[string]*snapshotEntry),
	}
}

// PublishMemberTrajectory 发布成员最新轨迹快照。
//
// 对齐 Python:
//
//	key = (snapshot.team_id, snapshot.session_id)
//	with self._lock:
//	    self._sequence += 1
//	    incoming = _SnapshotEntry(snapshot=snapshot, sequence=self._sequence)
//	    members = self._snapshots.setdefault(key, {})
//	    current = members.get(snapshot.member_id)
//	    if current is not None and _should_keep_current(current, incoming):
//	        return
//	    members[snapshot.member_id] = incoming
func (r *InMemoryTrajectoryRegistry) PublishMemberTrajectory(snapshot *MemberTrajectorySnapshot) {
	key := registryKey{teamID: snapshot.TeamID, sessionID: snapshot.SessionID}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sequence++
	incoming := &snapshotEntry{snapshot: snapshot, sequence: r.sequence}
	members, ok := r.snapshots[key]
	if !ok {
		members = make(map[string]*snapshotEntry)
		r.snapshots[key] = members
	}
	current, exists := members[snapshot.MemberID]
	if exists && shouldKeepCurrent(current, incoming) {
		return
	}
	members[snapshot.MemberID] = incoming
}

// GetTrajectory 返回指定会话的聚合团队轨迹。
//
// 对齐 Python:
//
//	key = (team_id, session_id)
//	with self._lock:
//	    snapshots = [entry.snapshot for entry in self._snapshots.get(key, {}).values()]
//	if not snapshots:
//	    return None
//	return aggregate_member_trajectories(
//	    [_trajectory_for_snapshot(snapshot) for snapshot in snapshots],
//	    team_id=team_id,
//	    session_id=session_id,
//	    filter_collaborative=filter_collaborative,
//	)
func (r *InMemoryTrajectoryRegistry) GetTrajectory(teamID, sessionID string, filterCollaborative bool) *Trajectory {
	key := registryKey{teamID: teamID, sessionID: sessionID}
	r.mu.RLock()
	entryMap, ok := r.snapshots[key]
	r.mu.RUnlock()
	if !ok || len(entryMap) == 0 {
		return nil
	}

	// 收集快照
	snapshots := make([]*MemberTrajectorySnapshot, 0, len(entryMap))
	for _, entry := range entryMap {
		snapshots = append(snapshots, entry.snapshot)
	}

	// 转换为轨迹列表
	trajectories := make([]*Trajectory, 0, len(snapshots))
	for _, snapshot := range snapshots {
		trajectories = append(trajectories, trajectoryForSnapshot(snapshot))
	}

	return AggregateMemberTrajectories(trajectories, teamID, sessionID, filterCollaborative)
}

// ClearSession 清除指定会话的快照。
//
// 对应 Python: InMemoryTrajectoryRegistry.clear_session()
func (r *InMemoryTrajectoryRegistry) ClearSession(teamID, sessionID string) {
	key := registryKey{teamID: teamID, sessionID: sessionID}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.snapshots, key)
}

// NowMs 返回当前墙钟时间（毫秒时间戳）。
//
// 对应 Python: now_ms()
func NowMs() int {
	return int(time.Now().UnixMilli())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// trajectoryForSnapshot 从快照构造轨迹，注入 member_id 和 member_role。
//
// 对齐 Python:
//
//	meta = dict(snapshot.trajectory.meta)
//	meta["member_id"] = snapshot.member_id
//	if snapshot.member_role is not None:
//	    meta["member_role"] = snapshot.member_role
//	return replace(snapshot.trajectory, meta=meta)
func trajectoryForSnapshot(snapshot *MemberTrajectorySnapshot) *Trajectory {
	meta := make(map[string]any)
	for k, v := range snapshot.Trajectory.Meta {
		meta[k] = v
	}
	meta["member_id"] = snapshot.MemberID
	if snapshot.MemberRole != "" {
		meta["member_role"] = snapshot.MemberRole
	}
	return &Trajectory{
		ExecutionID: snapshot.Trajectory.ExecutionID,
		Steps:       snapshot.Trajectory.Steps,
		Source:      snapshot.Trajectory.Source,
		CaseID:      snapshot.Trajectory.CaseID,
		SessionID:   snapshot.Trajectory.SessionID,
		Cost:        snapshot.Trajectory.Cost,
		Meta:        meta,
	}
}

// shouldKeepCurrent 判断是否应保留当前快照（而非替换为传入快照）。
//
// 对齐 Python:
//
//	if incoming.snapshot.recorded_at_ms != current.snapshot.recorded_at_ms:
//	    return current.snapshot.recorded_at_ms > incoming.snapshot.recorded_at_ms
//	return current.sequence >= incoming.sequence
func shouldKeepCurrent(current, incoming *snapshotEntry) bool {
	if incoming.snapshot.RecordedAtMs != current.snapshot.RecordedAtMs {
		return current.snapshot.RecordedAtMs > incoming.snapshot.RecordedAtMs
	}
	return current.sequence >= incoming.sequence
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时接口合规检查
var (
	_ TrajectorySink   = (*InMemoryTrajectoryRegistry)(nil)
	_ TrajectorySource = (*InMemoryTrajectoryRegistry)(nil)
)
