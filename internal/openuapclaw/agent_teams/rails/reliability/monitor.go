// Package reliability 成员内 detector 聚合器（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/monitor.py
package reliability

import (
	"context"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/detectors"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/remediation"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识。
const logComponent = logger.ComponentTeam

// ──────────────────────────── 结构体 ────────────────────────────

// ReliabilityMonitor 聚合单成员的 detectors 并按策略路由其异常。
//
// Python: monitor.py ReliabilityMonitor。驻留成员进程（经 ReliabilityRail 挂载），
// 检测发生在信号产生处。每次 feed 把信号扇出给所有 detector 并路由每个异常：
// 上报 leader（REPORT_LEADER / ESCALATE_USER）或记日志（OBSERVE_ONLY）。
// 产出的异常返回给 rail 以应用可逆本地纠偏。行为异常的 detector 记日志并跳过，
// 一个坏 detector 绝不阻塞其余。
type ReliabilityMonitor struct {
	// detectors 本成员的全部成员内 detector
	detectors []detectors.Detector
	// reporter 异常上报缝
	reporter AnomalyReporter
	// policy 严重度→动作策略
	policy *remediation.RemediationPolicy
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReliabilityMonitor 构造聚合器。
//
// Python: monitor.py ReliabilityMonitor.__init__。
func NewReliabilityMonitor(det []detectors.Detector, reporter AnomalyReporter, policy *remediation.RemediationPolicy) *ReliabilityMonitor {
	return &ReliabilityMonitor{detectors: det, reporter: reporter, policy: policy}
}

// Feed 对信号跑全部 detector；路由并返回异常。
//
// Python: monitor.py ReliabilityMonitor.feed。detector panic 等价 except
// Exception：记 error 日志并 continue（recover 包裹，异常路径防御性日志）。
func (m *ReliabilityMonitor) Feed(ctx context.Context, signal *core.Signal) (produced []*core.Anomaly) {
	for _, detector := range m.detectors {
		anomaly := m.observeSafe(detector, signal)
		if anomaly == nil {
			continue
		}
		produced = append(produced, anomaly)
		m.route(ctx, anomaly)
	}
	return produced
}

// Reset 为新一轮重置全部 detector。
//
// Python: monitor.py ReliabilityMonitor.reset。
func (m *ReliabilityMonitor) Reset() {
	for _, detector := range m.detectors {
		detector.Reset()
	}
}

// Reporter 返回异常上报缝（跨包测试与 standalone 断言用；同文件语义，非偏差）。
func (m *ReliabilityMonitor) Reporter() AnomalyReporter { return m.reporter }

// ──────────────────────────── 非导出函数 ────────────────────────────

// observeSafe 单 detector 隔离执行：panic 记日志跳过。
//
// Python: monitor.py feed 内 try/except Exception 分支。
func (m *ReliabilityMonitor) observeSafe(detector detectors.Detector, signal *core.Signal) (anomaly *core.Anomaly) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Str("detector", detector.Name()).
				Any("panic", r).
				Msg("reliability detector raised on observe")
			anomaly = nil
		}
	}()
	return detector.Observe(signal)
}

// route 上报 leader，或按策略 observe-only 时记日志。
//
// Python: monitor.py ReliabilityMonitor._route。
func (m *ReliabilityMonitor) route(ctx context.Context, anomaly *core.Anomaly) {
	actions := m.policy.ActionsFor(anomaly.Severity)
	report := false
	observe := false
	for _, a := range actions {
		if a == remediation.RemediationActionReportLeader || a == remediation.RemediationActionEscalateUser {
			report = true
		}
		if a == remediation.RemediationActionObserveOnly {
			observe = true
		}
	}
	if report {
		m.reporter.Report(ctx, anomaly)
	} else if observe {
		logger.Info(logComponent).
			Str("member", anomaly.MemberName).
			Str("kind", string(anomaly.Kind)).
			Str("summary", anomaly.Summary).
			Msg("reliability observe-only")
	}
}
