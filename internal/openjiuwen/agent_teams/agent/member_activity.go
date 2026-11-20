// 本文件提供 leader 内存中的成员活动名册（团队 idle 边判定的第一半）。
//
// 不持久化、不跨进程共享：它是 leader 在每个 run-cycle 开始时从 DB 重建、
// 再由事件保持新鲜的投影。leader 从未听说的成员保持种子状态。
//
// Python: openjiuwen/agent_teams/agent/member_activity.py（153 行一比一）
package agent

import (
	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// IdleSignal 一次状态观测对 team-idle 标记计时器欠下什么。
// Python: IdleSignal（member_activity.py L37-52）
type IdleSignal int

const (
	// IdleSignalNone 没有东西在动，但此前也静止——不重新武装标记。
	IdleSignalNone IdleSignal = iota
	// IdleSignalCancel 有东西在动：撤销任何 pending 的 idle 标记。
	IdleSignalCancel
	// IdleSignalSchedule 每个成员都静止且此前有过活动：恰好一次，武装标记。
	IdleSignalSchedule
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberActivityRegistry leader 内存持有的成员状态名册。
// Python: MemberActivityRegistry（member_activity.py L74-150）
type MemberActivityRegistry struct {
	// selfMemberName 拥有者（leader）自己的成员名——总在名册里，
	// 调用点无需为「包括我自己」做特判。
	// Python: self.self_member_name
	selfMemberName string
	// statuses 成员名 → 当前状态
	// Python: self._statuses: dict[str, MemberStatus]
	statuses map[string]atschema.MemberStatus
	// armed 「团队已 idle」边是否仍欠消费者。
	// 刻意以解除武装起步：新 built 的团队平凡地静止（还没有人启动过），
	// 在任何人工作之前宣告 idle 是噪音。必须先有动静；回落到静止才是信号。
	// Python: self._armed = False
	armed bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberActivityRegistry 构造名册，仅含拥有者成员（UNSTARTED）。
// Python: __init__（member_activity.py L81-91）
func NewMemberActivityRegistry(selfMemberName string) *MemberActivityRegistry {
	return &MemberActivityRegistry{
		selfMemberName: selfMemberName,
		statuses: map[string]atschema.MemberStatus{
			selfMemberName: atschema.MemberStatusUnstarted,
		},
	}
}

// Seed 用 DB 读出的基线整体替换名册。
// 基线不含拥有者成员时保留之——leader 自己的行只在 build_team 运行后才物化，
// 空团队或缺失团队是普通状态而非错误。
// Python: seed（member_activity.py L94-113）
func (r *MemberActivityRegistry) Seed(statuses map[string]atschema.MemberStatus) {
	merged := make(map[string]atschema.MemberStatus, len(statuses)+1)
	for name, status := range statuses {
		merged[name] = status
	}
	if _, ok := merged[r.selfMemberName]; !ok {
		prev, ok := r.statuses[r.selfMemberName]
		if !ok {
			prev = atschema.MemberStatusUnstarted
		}
		merged[r.selfMemberName] = prev
	}
	r.statuses = merged
}

// Record 记录一个成员的当前状态并回报标记欠什么。
// 任何移动的观测都无条件返回 CANCEL，而不追踪标记是否实际 pending：
// 撤销空的东西是免费的，否则这个对象要替它不拥有的计时器做二次推断。
// Python: record（member_activity.py L115-143）
func (r *MemberActivityRegistry) Record(memberName string, status atschema.MemberStatus) IdleSignal {
	r.statuses[memberName] = status
	if !r.IsIdle() {
		r.armed = true
		return IdleSignalCancel
	}
	if !r.armed {
		return IdleSignalNone
	}
	r.armed = false
	return IdleSignalSchedule
}

// IsIdle 是否所有已知成员当前都处于静止。
// Python: is_idle（member_activity.py L144-146）
func (r *MemberActivityRegistry) IsIdle() bool {
	for _, status := range r.statuses {
		if !atschema.IsMemberQuiescent(status) {
			return false
		}
	}
	return true
}

// Snapshot 名册的 名→状态值 副本，供上报。
// Python: snapshot（member_activity.py L148-150）
func (r *MemberActivityRegistry) Snapshot() map[string]string {
	out := make(map[string]string, len(r.statuses))
	for name, status := range r.statuses {
		out[name] = string(status)
	}
	return out
}
