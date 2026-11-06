package adapter

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildModelAnomalyDetectionRail_开关与缺省 enabled 非 true 时一律不构建。
func TestBuildModelAnomalyDetectionRail_开关与缺省(t *testing.T) {
	d := NewDeepAdapter()

	off := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{"enabled": false},
	}}
	if rail := d.buildModelAnomalyDetectionRail(off); rail != nil {
		t.Fatal("enabled=false 时不应构建")
	}

	if rail := d.buildModelAnomalyDetectionRail(map[string]any{}); rail != nil {
		t.Fatal("缺省时不应构建（对齐上游 enabled 默认 False）")
	}
}

// TestBuildModelAnomalyDetectionRail_开启后参数生效 enabled=true 时构建且 priority=70。
func TestBuildModelAnomalyDetectionRail_开启后参数生效(t *testing.T) {
	d := NewDeepAdapter()
	on := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{
			"enabled":                  true,
			"max_retries":              1,
			"single_char_repeat_count": 90,
			"tool_loop_compact": map[string]any{
				"enabled":               true,
				"consecutive_threshold": 5,
				"bailout_threshold":     2,
			},
		},
	}}
	rail := d.buildModelAnomalyDetectionRail(on)
	if rail == nil {
		t.Fatal("enabled=true 时应构建")
	}
	if rail.Priority() != 70 {
		t.Fatalf("priority 期望 70，实际 %d", rail.Priority())
	}
}

// TestBuildModelAnomalyDetectionRail_非法参数不阻断 max_retries 为负时返回 nil 而不是 panic。
func TestBuildModelAnomalyDetectionRail_非法参数不阻断(t *testing.T) {
	d := NewDeepAdapter()
	bad := map[string]any{"execution_guard": map[string]any{
		"model_anomaly_detection_rail": map[string]any{
			"enabled":     true,
			"max_retries": -1,
		},
	}}
	if rail := d.buildModelAnomalyDetectionRail(bad); rail != nil {
		t.Fatal("参数非法时应返回 nil（对齐上游 try/except 返回 None）")
	}
}

// TestResolveEnableModelAnomalyDetectionRail 开关读取覆盖缺省 / 无 guard / 显式开关四态。
func TestResolveEnableModelAnomalyDetectionRail(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want bool
	}{
		{"nil 配置", nil, false},
		{"无 execution_guard", map[string]any{}, false},
		{"无 rail 段", map[string]any{"execution_guard": map[string]any{}}, false},
		{"显式开启", map[string]any{"execution_guard": map[string]any{
			"model_anomaly_detection_rail": map[string]any{"enabled": true},
		}}, true},
		{"显式关闭", map[string]any{"execution_guard": map[string]any{
			"model_anomaly_detection_rail": map[string]any{"enabled": false},
		}}, false},
		{"非布尔值按关闭", map[string]any{"execution_guard": map[string]any{
			"model_anomaly_detection_rail": map[string]any{"enabled": "yes"},
		}}, false},
	}
	for _, c := range cases {
		if got := resolveEnableModelAnomalyDetectionRail(c.cfg); got != c.want {
			t.Errorf("%s: 期望 %v，实际 %v", c.name, c.want, got)
		}
	}
}
