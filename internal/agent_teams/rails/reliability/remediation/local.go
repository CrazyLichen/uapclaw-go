// Package remediation 可逆本地自动纠偏（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/remediation/local.py
package remediation

import (
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// noSteerKinds 模型组裁决类别：模型不可用或路由器已切换，自纠偏无益。
//
// Python: local.py _NO_STEER_KINDS。
var noSteerKinds = map[core.AnomalyKind]bool{
	core.AnomalyKindModelUnavailable: true,
	core.AnomalyKindModelRouteNotice: true,
}

// logComponent 日志组件标识。
const logComponent = logger.ComponentTeam

// ──────────────────────────── 结构体 ────────────────────────────

// LocalAutoRemediator 可逆的进程内自纠偏，受重启强度限流。
//
// Python: local.py LocalAutoRemediator。驻留成员进程，由 rail 驱动；策略对
// 异常要求 LOCAL_STEER 时返回非破坏性纠偏文案供 rail 推入 steering 队列——
// 但仅在重启强度预算内（intensity 次 / period_seconds），超预算返回空串，
// 完全让位给 leader / 用户路径。此处绝不执行破坏性动作。
type LocalAutoRemediator struct {
	// policy 严重度→动作策略
	policy *RemediationPolicy
	// intensity 周期内允许的纠偏动作数
	intensity int
	// periodSeconds 预算周期（秒）
	periodSeconds float64
	// nowFn 当前时间戳（秒），显式注入保持可测（对齐 now 参数）
	nowFn func() float64
	// window 重启强度滑动窗口
	window *core.SlidingWindowCounter
	// steerText 纠偏文案渲染函数（i18n 注入缝；默认走常量模板）
	steerText func(kind core.AnomalyKind, summary string) string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLocalAutoRemediator 创建本地自动纠偏器。
//
// Python: local.py LocalAutoRemediator.__init__（intensity=5, period=60s）。
func NewLocalAutoRemediator(policy *RemediationPolicy, intensity int, periodSeconds float64, nowFn func() float64) *LocalAutoRemediator {
	if nowFn == nil {
		nowFn = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	return &LocalAutoRemediator{
		policy:        policy,
		intensity:     intensity,
		periodSeconds: periodSeconds,
		nowFn:         nowFn,
		window:        core.NewSlidingWindowCounter(periodSeconds),
		steerText:     defaultSteerText,
	}
}

// SetSteerText 注入 i18n 渲染函数（factory 装配时接线 reliability.* 键组）。
func (r *LocalAutoRemediator) SetSteerText(fn func(kind core.AnomalyKind, summary string) string) {
	r.steerText = fn
}

// SteerMessage 返回自纠偏 steer 文案；不允许或超预算时返回空串。
//
// Python: local.py LocalAutoRemediator.steer_message。
func (r *LocalAutoRemediator) SteerMessage(anomaly *core.Anomaly) string {
	if anomaly == nil {
		return ""
	}
	if noSteerKinds[anomaly.Kind] {
		return ""
	}
	actions := r.policy.ActionsFor(anomaly.Severity)
	hasSteer := false
	for _, a := range actions {
		if a == RemediationActionLocalSteer {
			hasSteer = true
			break
		}
	}
	if !hasSteer {
		return ""
	}
	used := r.window.Add(r.nowFn())
	if used > r.intensity {
		return ""
	}
	return r.steerText(anomaly.Kind, anomaly.Summary)
}

// Reset 重置重启强度窗口。
//
// Python: local.py LocalAutoRemediator.reset。
func (r *LocalAutoRemediator) Reset() {
	r.window.Reset()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultSteerText 默认纠偏文案（i18n 未接线时的兜底；对齐
// reliability.steer_self_correct 模板形状）。导出为 DefaultSteerText：
// standalone 纠偏器（rsi runtime_reliability）的 super() 语义需要它。
func DefaultSteerText(kind core.AnomalyKind, summary string) string {
	msg := fmt.Sprintf("[reliability] self-correction hint (%s): %s", kind, summary)
	logger.Warn(logComponent).Str("kind", string(kind)).Msg("reliability steer text via fallback template")
	return msg
}

// defaultSteerText 包内别名（保持既有调用点不变）。
func defaultSteerText(kind core.AnomalyKind, summary string) string {
	return DefaultSteerText(kind, summary)
}
