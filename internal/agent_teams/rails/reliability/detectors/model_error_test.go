// Package detectors 单元测试（model_error / compaction）。
//
// 对应 Python：detectors/model_error.py、compaction.py 行为内联断言。
package detectors

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
)

// fallbackText i18n 兜底渲染：key+kwargs 拼接，便于断言。
func fallbackText(key string, kwargs map[string]any) string {
	s := key
	for k, v := range kwargs {
		s += "|" + k + "=" + strOf2(v)
	}
	return s
}

// strOf2 测试用 any→string。
func strOf2(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconvTest(val)
	case float64:
		return strconvTest(int(val))
	default:
		return ""
	}
}

func strconvTest(i int) string {
	if i == 0 {
		return "0"
	}
	var buf []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}

// verdictSignal 构造携带路由裁决的 MODEL_EXCEPTION 信号。
func verdictSignal(verdict map[string]any) *core.Signal {
	return &core.Signal{Kind: core.SignalKindModelException, MemberName: "coder", ModelFailure: verdict, Error: ptrTo("boom")}
}

func ptrTo(s string) *string { return &s }

// newModelError 构造默认阈值检测器（120s/3/2）。
func newModelError() *ModelStreamErrorDetector {
	now := 0.0
	d := NewModelStreamErrorDetector(120, 3, 2, func() float64 { return now })
	d.SetTextRenderer(fallbackText)
	return d
}

func TestModelStreamErrorDetector_无裁决走爆发计数(t *testing.T) {
	d := newModelError()
	m := "boom"
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindModelException, MemberName: "coder", Error: &m}))
	// consecutive_threshold=2：第二次连续失败即触发 MEDIUM
	a := d.Observe(&core.Signal{Kind: core.SignalKindModelException, MemberName: "coder", Error: &m})
	assert.NotNil(t, a)
	assert.Equal(t, core.AnomalyKindModelError, a.Kind)
	assert.Equal(t, core.SeverityMedium, a.Severity)
}

func TestModelStreamErrorDetector_用户介入升级CRITICAL只发一次(t *testing.T) {
	d := newModelError()
	verdict := map[string]any{"route_case": "exhausted_user_action", "user_action_required": true,
		"failures": []any{map[string]any{"model_name": "m1", "error_type": "DeploymentRateLimitError"}}}
	a1 := d.Observe(verdictSignal(verdict))
	assert.NotNil(t, a1)
	assert.Equal(t, core.AnomalyKindModelUnavailable, a1.Kind)
	assert.Equal(t, core.SeverityCritical, a1.Severity)
	assert.Equal(t, "reliability.model_unavailable|model=m1|reason="+
		"reliability.model_failure|model=reliability.model_label|name=m1|where=|reason=reliability.reason_rate_limited"+
		"|no_substitute=reliability.model_no_substitute_group", a1.Summary)
	// 第二次同裁决：已 CRITICAL，不重复发
	assert.Nil(t, d.Observe(verdictSignal(verdict)))
}

func TestModelStreamErrorDetector_切换通告按deployment去重(t *testing.T) {
	d := newModelError()
	verdict := map[string]any{"route_case": "switchable", "reason": "deterministic",
		"failures": []any{map[string]any{"deployment_id": "dep1", "status_code": 401, "model_name": "m1", "provider": "prov", "endpoint": "http://e"}}}
	a := d.Observe(verdictSignal(verdict))
	assert.NotNil(t, a)
	assert.Equal(t, core.AnomalyKindModelRouteNotice, a.Kind)
	assert.Equal(t, core.SeverityMedium, a.Severity)
	assert.Equal(t, "reliability.model_route_switched|model=reliability.model_label|name=m1|where=prov, http://e"+
		"|reason=reliability.reason_api_key|status=401", a.Summary)
	// 同 (deployment, reason, status) 二次丢弃
	assert.Nil(t, d.Observe(verdictSignal(verdict)))
	// reset 后 _noticed 仍保留（对齐上游 reset 不清 _noticed）
	d.Reset()
	assert.Nil(t, d.Observe(verdictSignal(verdict)))
}

func TestModelStreamErrorDetector_瞬态原因不通告(t *testing.T) {
	d := newModelError()
	verdict := map[string]any{"route_case": "switchable", "reason": "transient",
		"failures": []any{map[string]any{"deployment_id": "dep1", "status_code": 500}}}
	assert.Nil(t, d.Observe(verdictSignal(verdict)), "非 deterministic/stream_interrupted 原因不通告")
}

func TestModelStreamErrorDetector_exhaustedRetryable静默(t *testing.T) {
	d := newModelError()
	verdict := map[string]any{"route_case": "exhausted_retryable"}
	assert.Nil(t, d.Observe(verdictSignal(verdict)))
}

func TestModelStreamErrorDetector_请求被拒走爆发计数(t *testing.T) {
	d := newModelError()
	verdict := map[string]any{"route_case": "exhausted_request_rejected"}
	m := "rejected"
	sig := &core.Signal{Kind: core.SignalKindModelException, MemberName: "coder", ModelFailure: verdict, Error: &m}
	assert.Nil(t, d.Observe(sig))
	a := d.Observe(sig)
	assert.NotNil(t, a, "exhausted_request_rejected 按普通模型错误计数（上下文恢复或可修复）")
	assert.Equal(t, core.AnomalyKindModelError, a.Kind)
}

func TestModelStreamErrorDetector_成功清零后CRITICAL可再发(t *testing.T) {
	d := newModelError()
	verdict := map[string]any{"route_case": "exhausted_user_action", "user_action_required": true,
		"failures": []any{map[string]any{"model_name": "m1", "error_type": "DeploymentTimeoutError"}}}
	assert.NotNil(t, d.Observe(verdictSignal(verdict)))
	assert.Nil(t, d.Observe(okSignal(core.SignalKindAfterModelCall)), "成功清零连击并清已发射级别")
	assert.NotNil(t, d.Observe(verdictSignal(verdict)), "清零后同裁决可再次升级")
}

func TestReasonText_错误类型与状态码分支(t *testing.T) {
	cases := []struct {
		failure map[string]any
		want    string
	}{
		{map[string]any{}, "reliability.reason_unavailable"},
		{map[string]any{"error_type": "DeploymentTimeoutError"}, "reliability.reason_timeout"},
		{map[string]any{"error_type": "DeploymentNetworkError"}, "reliability.reason_unreachable"},
		{map[string]any{"error_type": "DeploymentRateLimitError"}, "reliability.reason_rate_limited"},
		{map[string]any{"status_code": 429}, "reliability.reason_rate_limited"},
		{map[string]any{"status_code": 401}, "reliability.reason_api_key|status=401"},
		{map[string]any{"status_code": 403}, "reliability.reason_api_key|status=403"},
		{map[string]any{"status_code": 404}, "reliability.reason_not_found"},
		{map[string]any{"status_code": 502}, "reliability.reason_server_error|status=502"},
		{map[string]any{"status_code": 400, "message": "bad: 提供商解释"}, "reliability.reason_rejected_detail|status=400|detail=提供商解释"},
		{map[string]any{"status_code": 400, "message": "bad", "category": "stream_interrupted"}, "reliability.reason_rejected|status=400"},
		{map[string]any{"status_code": 400}, "reliability.reason_rejected|status=400"},
		{map[string]any{"error_type": "OtherError"}, "reliability.reason_failed"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, reasonText(fallbackText, c.failure), "failure=%v", c.failure)
	}
}

func TestModelLabel_名与顺位(t *testing.T) {
	assert.Equal(t, "reliability.model_label|name=m1|where=prov, http://e",
		modelLabel(fallbackText, map[string]any{"model_name": "m1", "provider": "prov", "endpoint": "http://e"}))
	assert.Equal(t, "reliability.model_label_ranked|position=2|name=m2|where=",
		modelLabel(fallbackText, map[string]any{"model_name": "m2", "position": 2}))
	// 无模型名回退 deployment_id，再回退「组内其他模型」
	assert.Equal(t, "reliability.model_label|name=dep9|where=",
		modelLabel(fallbackText, map[string]any{"deployment_id": "dep9"}))
	assert.Equal(t, "reliability.model_label|name=reliability.model_route_other|where=", modelLabel(fallbackText, map[string]any{}), "fallback 名仍走 model_label（对齐 Python）")
}

func TestModelLabel_逻辑星号不直传(t *testing.T) {
	// model_name="*" 不是无模型名（对齐：绝不把逻辑名展示给 leader 的判断由
	// name 非空即透出——上游同样透出 "*"，此处对齐上游不做额外过滤）
	got := modelLabel(fallbackText, map[string]any{"model_name": "*"})
	assert.Equal(t, "reliability.model_label|name=*|where=", got)
}

func TestFrequentCompactionDetector_骤降推断(t *testing.T) {
	now := 0.0
	d := NewFrequentCompactionDetector(300, 3, 0.3, func() float64 { return now })
	mc := func(n int) *core.Signal { return &core.Signal{Kind: core.SignalKindBeforeModelCall, MemberName: "c", MessageCount: &n} }
	assert.Nil(t, d.Observe(mc(10)))
	// 10 → 4：降幅 60% ≥ 30%，第 1 次压缩
	assert.Nil(t, d.Observe(mc(4)), "窗口内第 1 次压缩 < 频次阈值")
	assert.Nil(t, d.Observe(mc(8)))
	assert.Nil(t, d.Observe(mc(2)))
	now = 100
	a := d.Observe(mc(1))
	assert.NotNil(t, a, "窗口内第 3 次压缩触发 MEDIUM")
	assert.Equal(t, core.AnomalyKindFrequentCompaction, a.Kind)
	assert.Equal(t, core.SeverityMedium, a.Severity)
	assert.Equal(t, map[string]any{"compactions": 3, "prev_count": 2, "cur_count": 1, "drop": 1}, a.Evidence)
	assert.Equal(t, "3 inferred compactions; last drop 2->1", a.Summary)
	// 已 fired 不再发
	assert.Nil(t, d.Observe(mc(10)))
	assert.Nil(t, d.Observe(mc(1)))
}

func TestFrequentCompactionDetector_小幅裁剪不算(t *testing.T) {
	now := 0.0
	d := NewFrequentCompactionDetector(300, 3, 0.3, func() float64 { return now })
	mc := func(n int) *core.Signal { return &core.Signal{Kind: core.SignalKindBeforeModelCall, MemberName: "c", MessageCount: &n} }
	assert.Nil(t, d.Observe(mc(10)))
	assert.Nil(t, d.Observe(mc(8)), "10→8 降幅 20% < 30%：正常多轮裁剪")
}

func TestFrequentCompactionDetector_基线为负不发射(t *testing.T) {
	now := 0.0
	d := NewFrequentCompactionDetector(300, 1, 0.3, func() float64 { return now })
	zero := 0
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindBeforeModelCall, MemberName: "c", MessageCount: &zero}))
	zero2 := -5
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindBeforeModelCall, MemberName: "c", MessageCount: &zero2}),
		"prev<=0 跳过")
}

func TestFrequentCompactionDetector_Reset(t *testing.T) {
	now := 0.0
	d := NewFrequentCompactionDetector(300, 1, 0.3, func() float64 { return now })
	mc := func(n int) *core.Signal { return &core.Signal{Kind: core.SignalKindBeforeModelCall, MemberName: "c", MessageCount: &n} }
	d.Observe(mc(10))
	a := d.Observe(mc(1))
	assert.NotNil(t, a)
	d.Reset()
	assert.Nil(t, d.Observe(mc(1)), "重置后无基线，不发射")
}
