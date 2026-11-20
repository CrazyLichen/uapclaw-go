// Package remediation 严重度→动作策略（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/remediation/policy.py
package remediation

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// DefaultSeverityActions 默认分层策略（leader-first）。
//
// Python: policy.py DEFAULT_SEVERITY_ACTIONS：LOW 仅观测、MEDIUM 上报 leader
// （LLM 决策）、HIGH 先可逆本地纠偏再上报、CRITICAL 纠偏后升级用户。
var DefaultSeverityActions = map[core.Severity][]RemediationAction{
	core.SeverityLow:      {RemediationActionObserveOnly},
	core.SeverityMedium:   {RemediationActionReportLeader},
	core.SeverityHigh:     {RemediationActionLocalSteer, RemediationActionReportLeader},
	core.SeverityCritical: {RemediationActionLocalSteer, RemediationActionEscalateUser},
}

// ──────────────────────────── 结构体 ────────────────────────────

// RemediationPolicy 把异常严重度映射为有序的补救动作。
//
// Python: policy.py RemediationPolicy。
type RemediationPolicy struct {
	// m 严重度→动作表（构造时拷贝，不共享默认表）
	m map[core.Severity][]RemediationAction
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRemediationPolicy 从严重度→动作表构造策略；nil 时用默认表。
//
// Python: policy.py RemediationPolicy.__init__（dict 拷贝语义）。
func NewRemediationPolicy(severityActions map[core.Severity][]RemediationAction) *RemediationPolicy {
	if severityActions == nil {
		severityActions = DefaultSeverityActions
	}
	m := make(map[core.Severity][]RemediationAction, len(severityActions))
	for k, v := range severityActions {
		m[k] = append([]RemediationAction(nil), v...)
	}
	return &RemediationPolicy{m: m}
}

// ActionsFor 返回该严重度配置的补救动作列表。
//
// Python: policy.py RemediationPolicy.actions_for（未配置返回空切片）。
func (p *RemediationPolicy) ActionsFor(severity core.Severity) []RemediationAction {
	return p.m[severity]
}

// BuildRemediationPolicy 从配置构造策略。
//
// Python: factory.py build_remediation_policy（经字符串解析桥接
// RemediationPolicyConfig 的 string 形状，spec D-g3 同族）。
func BuildRemediationPolicy(severityActions map[string][]string) *RemediationPolicy {
	if severityActions == nil {
		return NewRemediationPolicy(nil)
	}
	m := make(map[core.Severity][]RemediationAction, len(severityActions))
	for sevStr, actions := range severityActions {
		sev := core.Severity(sevStr)
		if sev.Rank() < 0 {
			continue
		}
		list := make([]RemediationAction, 0, len(actions))
		for _, a := range actions {
			list = append(list, RemediationAction(a))
		}
		m[sev] = list
	}
	return NewRemediationPolicy(m)
}
