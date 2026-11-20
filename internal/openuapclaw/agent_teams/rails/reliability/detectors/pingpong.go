// Package detectors 团队级乒乓检测（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/detectors/pingpong.py
package detectors

import (
	"sort"
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PingPongDetector 检测两个成员互发消息来回踢皮球无进展。
//
// Python: pingpong.py PingPongDetector。消费 MESSAGE 信号（member_name=发送方，
// peer_member=接收方）。同对成员间严格的方向反转延长连击；任何第三方或非反转
// 消息重置连击。一次 volley 是单条消息，min_volleys=6 约等于三个来回。
// 这是唯一的团队级 detector——由 leader 进程的 coordination 喂入，不经成员 rail。
type PingPongDetector struct {
	// minVolleys 触发 MEDIUM 的连击数
	minVolleys int
	// lastFrom 最近一条消息的发送方
	lastFrom *string
	// lastTo 最近一条消息的接收方
	lastTo *string
	// count 当前连击数
	count int
	// firedSeverity 已发射最高严重度（nil 未发射；重置连击时一并清）
	firedSeverity *core.Severity
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPingPongDetector 构造团队级乒乓检测器。
//
// Python: pingpong.py PingPongDetector.__init__（默认 min_volleys=6）。
func NewPingPongDetector(minVolleys int) *PingPongDetector {
	return &PingPongDetector{minVolleys: minVolleys}
}

// Name 返回 detector 稳定标识。
//
// Python: pingpong.py PingPongDetector.name。
func (d *PingPongDetector) Name() string { return "ping_pong" }

// Observe 追踪来回连击；连击跨档时发射。
//
// Python: pingpong.py PingPongDetector.observe。
func (d *PingPongDetector) Observe(signal *core.Signal) *core.Anomaly {
	if signal.Kind != core.SignalKindMessage || signal.PeerMember == nil {
		return nil
	}
	sender := signal.MemberName
	recipient := *signal.PeerMember
	isReversal := d.lastTo != nil && d.lastFrom != nil &&
		sender == *d.lastTo && recipient == *d.lastFrom
	if isReversal {
		d.count++
	} else {
		d.count = 1
		d.firedSeverity = nil
	}
	d.lastFrom = &sender
	d.lastTo = &recipient
	severity := d.classify()
	if severity == "" {
		return nil
	}
	if d.firedSeverity != nil && severity.Rank() <= d.firedSeverity.Rank() {
		return nil
	}
	d.firedSeverity = &severity
	pair := []string{sender, recipient}
	sort.Strings(pair)
	return &core.Anomaly{
		Detector:   "ping_pong",
		Kind:       core.AnomalyKindPingPong,
		Severity:   severity,
		MemberName: sender,
		Summary:    strconv.Itoa(d.count) + " consecutive volleys between " + sender + " and " + recipient,
		Evidence: map[string]any{
			"volleys": d.count,
			"pair":    pair,
		},
		PeerMember: &recipient,
	}
}

// Reset 重置连击追踪。
//
// Python: pingpong.py PingPongDetector.reset。
func (d *PingPongDetector) Reset() {
	d.lastFrom = nil
	d.lastTo = nil
	d.count = 0
	d.firedSeverity = nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// classify 把连击数映射到严重度。
//
// Python: pingpong.py PingPongDetector._classify（min_volleys*2 → HIGH）。
func (d *PingPongDetector) classify() core.Severity {
	if d.count >= d.minVolleys*2 {
		return core.SeverityHigh
	}
	if d.count >= d.minVolleys {
		return core.SeverityMedium
	}
	return ""
}
