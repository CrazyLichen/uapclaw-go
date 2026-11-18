// Package detectors 模型调用（含流式）错误检测（25.1g）。
//
// 对应 Python：openjiuwen/agent_teams/reliability/detectors/model_error.py
package detectors

import (
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
)

// ──────────────────────────── 常量 ────────────────────────────

// 模型组客户端附加到错误上的 route_case 取值。
//
// Python: model_error.py 顶部常量（源自
// core/foundation/llm/routing/intelli_router_errors.py）。
const (
	routeCaseSwitchable      = "switchable"
	routeCaseRequestRejected = "exhausted_request_rejected"
	// reasonStreamInterrupted 值得通告 leader 的切换原因之一（瞬态切换不是）
	reasonStreamInterrupted = "stream_interrupted"
)

// noticeReasons 值得通告 leader 的切换原因集合。
//
// Python: model_error.py _NOTICE_REASONS = {"deterministic", stream_interrupted}。
var noticeReasons = map[string]bool{"deterministic": true, reasonStreamInterrupted: true}

// ──────────────────────────── 结构体 ────────────────────────────

// TextRenderer i18n 文案渲染缝：按 dotted key + 命名参数渲染本地化字符串。
// factory 装配时接线 reliability.* 键组（Task 10）；未设置时用英文兜底。
type TextRenderer func(key string, kwargs map[string]any) string

// ModelStreamErrorDetector 检测频繁或连续的模型调用错误。
//
// Python: model_error.py ModelStreamErrorDetector。无逐 chunk 流式钩子，故以
// 模型调用为粒度：MODEL_EXCEPTION 由调用抛出，或 AFTER_MODEL_CALL 上浮的错误由
// rail 重发为 MODEL_EXCEPTION 信号。流中单 chunk 错误在此不可观测。AFTER_MODEL_CALL
// （成功）清零连击。
//
// 携带模型组路由裁决（Signal.model_failure）的错误绕过爆发计数：
//   - 需要人工介入的裁决立刻升级（CRITICAL，直到一次调用成功只发一次）；
//   - 切换到其他 deployment 每个 deployment 与原因组合只通告一次（MEDIUM 通告）；
//   - 冷却后待重试的保持静默。
type ModelStreamErrorDetector struct {
	// burst 共享错误爆发检测器（Python 继承 super 的等价包装）
	burst *ErrorBurstDetector
	// noticed 已通告为 notice 的 (deployment_id, reason, status_code)
	noticed map[[3]any]bool
	// t 文案渲染缝
	t TextRenderer
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelStreamErrorDetector 构造模型错误检测器。
//
// Python: model_error.py ModelStreamErrorDetector.__init__（默认 120s/3/2）。
func NewModelStreamErrorDetector(windowSeconds float64, rateThreshold, consecutiveThreshold int, nowFn func() float64) *ModelStreamErrorDetector {
	return &ModelStreamErrorDetector{
		burst: NewErrorBurstDetector(
			"model_error",
			core.SignalKindModelException,
			core.SignalKindAfterModelCall,
			core.AnomalyKindModelError,
			windowSeconds, rateThreshold, consecutiveThreshold, nowFn,
		),
		noticed: make(map[[3]any]bool),
	}
}

// SetTextRenderer 注入 i18n 渲染缝（factory 装配时调用）。
func (d *ModelStreamErrorDetector) SetTextRenderer(t TextRenderer) { d.t = t }

// Name 返回 detector 稳定标识。
func (d *ModelStreamErrorDetector) Name() string { return d.burst.Name() }

// Observe 分流模型组裁决；其余模型错误照旧计数。
//
// Python: model_error.py ModelStreamErrorDetector.observe。
func (d *ModelStreamErrorDetector) Observe(signal *core.Signal) *core.Anomaly {
	var verdict map[string]any
	if signal.Kind == core.SignalKindModelException {
		verdict = signal.ModelFailure
	}
	if len(verdict) == 0 {
		return d.burst.Observe(signal)
	}
	if truthy(verdict["user_action_required"]) {
		return d.unavailable(signal, verdict)
	}
	switch verdict["route_case"] {
	case routeCaseSwitchable:
		return d.notice(signal, verdict)
	case routeCaseRequestRejected:
		// 请求本身被拒（如超长）：按普通模型错误计数；上下文恢复或可修复
		return d.burst.Observe(signal)
	}
	// exhausted_retryable：重试 rail 等冷却后重试
	return nil
}

// Reset 重置窗口/连击/已发射级别。
//
// Python: model_error.py ModelStreamErrorDetector.reset（继承；_noticed 不清——
// 跨轮仍按 deployment+reason 去重，对齐上游 reset 未触及 _noticed 的行为）。
func (d *ModelStreamErrorDetector) Reset() { d.burst.Reset() }

// ──────────────────────────── 非导出函数 ────────────────────────────

// unavailable 需人工介入的裁决：升级为 CRITICAL（到成功前只发一次）。
//
// Python: model_error.py ModelStreamErrorDetector._unavailable。
func (d *ModelStreamErrorDetector) unavailable(signal *core.Signal, verdict map[string]any) *core.Anomaly {
	if fired, ok := d.burst.FiredSeverity(); ok && fired.Rank() >= core.SeverityCritical.Rank() {
		return nil
	}
	d.burst.SetFiredSeverity(core.SeverityCritical)
	failures := dictSlice(verdict["failures"])
	separator := d.t("reliability.model_failure_separator", nil)
	var parts []string
	for _, f := range failures {
		parts = append(parts, d.t("reliability.model_failure", map[string]any{
			"model":  modelLabel(d.t, f),
			"reason": reasonText(d.t, f),
		}))
	}
	reason := strings.Join(parts, separator)
	if reason == "" {
		reason = d.t("reliability.model_not_in_group", nil)
	}
	var noSubstitute string
	if _, hasPin := verdict["pinned_model"]; hasPin && verdict["pinned_model"] != nil && verdict["pinned_model"] != "" {
		noSubstitute = d.t("reliability.model_no_substitute_pinned", nil)
	} else {
		noSubstitute = d.t("reliability.model_no_substitute_group", nil)
	}
	var model string
	if pinned, ok := verdict["pinned_model"]; ok && pinned != nil && pinned != "" {
		model = strOf(pinned)
	} else {
		var names []string
		seen := map[string]bool{}
		for _, f := range failures {
			if n := strOf(f["model_name"]); n != "" && !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		sort.Strings(names)
		model = strings.Join(names, ", ")
	}
	if model == "" {
		if gid := strOf(verdict["model_group_id"]); gid != "" {
			model = gid
		} else {
			model = d.t("reliability.model_route_other", nil)
		}
	}
	return &core.Anomaly{
		Detector: d.Name(),
		Kind:     core.AnomalyKindModelUnavailable,
		Severity: core.SeverityCritical,
		MemberName: signal.MemberName,
		Summary: d.t("reliability.model_unavailable", map[string]any{
			"model": model, "reason": reason, "no_substitute": noSubstitute,
		}),
		Evidence: map[string]any{"model_failure": verdict, "last_error": signal.Error},
	}
}

// notice 报告「从故障 deployment 切走」，每个 deployment 与原因组合只报一次。
//
// Python: model_error.py ModelStreamErrorDetector._notice。仅确定性错误或流中断
// 造成的切换才通告；路由器已在绕行的瞬态失败无需关注。failures[0] 是抛错的
// deployment。刻意不指名替代模型：路由器按次挑选、更早顺位可能已恢复，
// 通告只说组内已切换。
func (d *ModelStreamErrorDetector) notice(signal *core.Signal, verdict map[string]any) *core.Anomaly {
	reason := strOf(verdict["reason"])
	if !noticeReasons[reason] {
		return nil
	}
	failures := dictSlice(verdict["failures"])
	if len(failures) == 0 {
		return nil
	}
	trigger := failures[0]
	key := [3]any{trigger["deployment_id"], reason, trigger["status_code"]}
	if d.noticed[key] {
		return nil
	}
	d.noticed[key] = true
	template := "reliability.model_route_switched"
	if reason == reasonStreamInterrupted {
		template = "reliability.model_route_interrupted"
	}
	summary := d.t(template, map[string]any{
		"model":  modelLabel(d.t, trigger),
		"reason": reasonText(d.t, trigger),
	})
	return &core.Anomaly{
		Detector:   d.Name(),
		Kind:       core.AnomalyKindModelRouteNotice,
		Severity:   core.SeverityMedium,
		MemberName: signal.MemberName,
		Summary:    summary,
		Evidence:   map[string]any{"model_failure": verdict},
	}
}

// ──────────────────────────── 包级助手（model_error.py 模块级函数） ────────────────────────────

// modelLabel 把 failures/usable 条目渲染为 "#N name (provider, endpoint)"。
//
// Python: model_error.py _model_label。没有模型名的 deployment 仍是组内一员；
// 绝不把逻辑名 "*" 展示给 leader。
func modelLabel(t TextRenderer, entry map[string]any) string {
	name := strOf(entry["model_name"])
	if name == "" {
		name = strOf(entry["deployment_id"])
	}
	if name == "" {
		name = t("reliability.model_route_other", nil)
	}
	var parts []string
	if p := strOf(entry["provider"]); p != "" {
		parts = append(parts, p)
	}
	if e := strOf(entry["endpoint"]); e != "" {
		parts = append(parts, e)
	}
	where := strings.Join(parts, ", ")
	position := intOf(entry["position"])
	if position != 0 {
		return t("reliability.model_label_ranked", map[string]any{
			"position": position, "name": name, "where": where,
		})
	}
	return t("reliability.model_label", map[string]any{"name": name, "where": where})
}

// reasonText 一条 failures 条目的本地化原因。
//
// Python: model_error.py _reason_text。由错误类型与状态码构建（而非客户端的
// 英文 message），leader 以团队语言转述。
func reasonText(t TextRenderer, failure map[string]any) string {
	errorType := strOf(failure["error_type"])
	status := intOf(failure["status_code"])
	// Python: isinstance(status, int) and not isinstance(status, bool) — bool 排除
	statusValid := status != 0 || failure["status_code"] == 0
	switch {
	case errorType == "" && !statusValid:
		return t("reliability.reason_unavailable", nil)
	case errorType == "DeploymentTimeoutError":
		return t("reliability.reason_timeout", nil)
	case errorType == "DeploymentNetworkError":
		return t("reliability.reason_unreachable", nil)
	case errorType == "DeploymentRateLimitError" || status == 429:
		return t("reliability.reason_rate_limited", nil)
	case status == 401 || status == 403:
		return t("reliability.reason_api_key", map[string]any{"status": status})
	case status == 404:
		return t("reliability.reason_not_found", nil)
	case status >= 500:
		return t("reliability.reason_server_error", map[string]any{"status": status})
	case status >= 400 && status < 500:
		// 客户端在 ": " 之后附加提供商自己的解释
		message := strOf(failure["message"])
		detail := ""
		if idx := strings.Index(message, ": "); idx >= 0 {
			detail = message[idx+2:]
		}
		if detail != "" && strOf(failure["category"]) != reasonStreamInterrupted {
			return t("reliability.reason_rejected_detail", map[string]any{"status": status, "detail": detail})
		}
		return t("reliability.reason_rejected", map[string]any{"status": status})
	default:
		return t("reliability.reason_failed", nil)
	}
}

// ──────────────────────────── 通用小助手 ────────────────────────────

// truthy 对齐 Python 真值判断（None/0/""/false 均 falsy）。
func truthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case string:
		return val != ""
	case int:
		return val != 0
	case float64:
		return val != 0
	default:
		return true
	}
}

// strOf any → string（nil 返回空串）。
func strOf(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// intOf any → int（非整型或 nil 返回 0）。
func intOf(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(val)
		return n
	default:
		return 0
	}
}

// dictSlice any → []map[string]any（仅保留 dict 元素，对齐 isinstance(f, dict) 过滤）。
func dictSlice(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

