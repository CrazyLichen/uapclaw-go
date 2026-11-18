// Package reliability 异常上报缝（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/reporter.py
package reliability

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	schemaevents "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AnomalyReporter 把检出的异常转发进补救管线。
//
// Python: reporter.py AnomalyReporter(Protocol)。monitor（成员进程）与
// remediation handler（leader 进程）之间的缝。
type AnomalyReporter interface {
	// Report 上报一个检出异常
	Report(anomaly *core.Anomaly)
}

// EventAnomalyReporter 把异常包装为 AnomalyDetectedEvent 发布到团队 TEAM topic。
//
// Python: reporter.py EventAnomalyReporter。sender_id 是本成员的 messager
// 节点 id，让 leader 的 self-message filter 忽略 leader 自己的发布；
// 成员异常携带成员的节点 id，正常到达 leader。
type EventAnomalyReporter struct {
	// messager 传输实例
	messager messager.Messager
	// teamName 团队名
	teamName string
	// senderId 本成员的 messager 节点 id
	senderId string
	// topicBuilder TEAM topic 构造（sessionID, teamName → topic 串），测试可注入
	topicBuilder func(sessionID, teamName string) string
	// sessionID 会话 id（构造期取，跨进程事件归属本 session）
	sessionID string
}

// LocalAnomalyReporter leader 自检的进程内异常 sink。
//
// Python: reporter.py LocalAnomalyReporter。leader 自己的异常不能发布——其
// messager self-filter（kernel 丢弃 sender_id 等于本地成员的事件）会把它们丢掉。
// 改为直送本地 sink（leader 的 ReliabilityHandler），dispatcher 建好后绑定一次
// （见 TeamAgent._register_reliability_local_sink 等价物）。未绑定时 no-op，
// 异常带告警丢弃而非静默丢失。
type LocalAnomalyReporter struct {
	// sink 绑定的本地异常处理入口
	sink func(*core.Anomaly)
	// routeNoticeSource leader 待发 route notices 的 drain 缝
	routeNoticeSource func() []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEventAnomalyReporter 构造跨进程事件上报器。
//
// Python: reporter.py EventAnomalyReporter.__init__。
func NewEventAnomalyReporter(msgr messager.Messager, teamName, senderID, sessionID string) *EventAnomalyReporter {
	return &EventAnomalyReporter{
		messager:     msgr,
		teamName:     teamName,
		senderId:     senderID,
		sessionID:    sessionID,
		topicBuilder: func(sessionID, tn string) string { return schemaevents.TeamTopicTeam.Build(sessionID, tn) },
	}
}

// SetTopicBuilder 注入 topic 构造（测试缝）。
func (r *EventAnomalyReporter) SetTopicBuilder(fn func(sessionID, teamName string) string) {
	r.topicBuilder = fn
}

// Report 把异常包装为事件并发布到团队 topic。
//
// Python: reporter.py EventAnomalyReporter.report。发布失败仅 Error 日志
// （DB 为事实源时另有轮询兜底；此处对齐上游 except 只告警）。
func (r *EventAnomalyReporter) Report(anomaly *core.Anomaly) {
	event := schemaevents.AnomalyDetectedEvent{
		BaseEventMessage: schemaevents.BaseEventMessage{TeamName: r.teamName, MemberName: anomaly.MemberName},
		Detector:   anomaly.Detector,
		Kind:       string(anomaly.Kind),
		Severity:   string(anomaly.Severity),
		Summary:    anomaly.Summary,
		Evidence:   anomaly.Evidence,
		PeerMember: anomaly.PeerMember,
	}
	message := schemaevents.EventMessageFromEvent(event)
	message.SenderID = r.senderId
	if err := r.messager.Publish(nil, r.topicBuilder(r.sessionID, r.teamName), message); err != nil {
		logger.Error(logComponent).
			Str("member", anomaly.MemberName).
			Err(err).
			Msg("failed to publish anomaly")
	}
}

// NewLocalAnomalyReporter 构造进程内 sink 上报器（未绑定）。
//
// Python: reporter.py LocalAnomalyReporter.__init__。
func NewLocalAnomalyReporter() *LocalAnomalyReporter {
	return &LocalAnomalyReporter{}
}

// Bind 绑定把异常路由给 handler 的本地 sink。
//
// Python: reporter.py LocalAnomalyReporter.bind。
func (r *LocalAnomalyReporter) Bind(sink func(*core.Anomaly)) {
	r.sink = sink
}

// BindRouteNoticeSource 绑定 leader 下一次输入前待发 route notices 的 drain。
//
// Python: reporter.py LocalAnomalyReporter.bind_route_notice_source。
func (r *LocalAnomalyReporter) BindRouteNoticeSource(source func() []string) {
	r.routeNoticeSource = source
}

// TakeRouteNotices 返回 leader 下一次输入前待发的 route notices 并清空。
//
// Python: reporter.py LocalAnomalyReporter.take_route_notices。
func (r *LocalAnomalyReporter) TakeRouteNotices() []string {
	if r.routeNoticeSource == nil {
		return nil
	}
	return r.routeNoticeSource()
}

// Report 把异常路由给绑定的本地 sink（若有）。
//
// Python: reporter.py LocalAnomalyReporter.report。未绑定时告警丢弃。
func (r *LocalAnomalyReporter) Report(anomaly *core.Anomaly) {
	if r.sink == nil {
		logger.Warn(logComponent).
			Str("member", anomaly.MemberName).
			Msg("local anomaly sink not bound; dropping anomaly")
		return
	}
	r.sink(anomaly)
}
