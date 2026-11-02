package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextEngineConfig_压缩比例字段 验证比例字段的序列化与零值语义
func TestContextEngineConfig_压缩比例字段(t *testing.T) {
	cfg := NewContextEngineConfig()
	if cfg.CompressionTriggerRatio != 0 || cfg.CompressionTargetRatio != 0 {
		t.Fatalf("默认应为 0（不启用比例），实际 trigger=%v target=%v",
			cfg.CompressionTriggerRatio, cfg.CompressionTargetRatio)
	}

	cfg.CompressionTriggerRatio = 0.85
	cfg.CompressionTargetRatio = 0.55
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if raw["compression_trigger_ratio"] != 0.85 {
		t.Errorf("compression_trigger_ratio 应为 0.85，实际 %v", raw["compression_trigger_ratio"])
	}
	if raw["compression_target_ratio"] != 0.55 {
		t.Errorf("compression_target_ratio 应为 0.55，实际 %v", raw["compression_target_ratio"])
	}
}

// TestContextEngineConfigValidate_压缩比例校验 验证比例区间与相对大小校验
func TestContextEngineConfigValidate_压缩比例校验(t *testing.T) {
	cases := []struct {
		name    string
		trigger float64
		target  float64
		wantErr bool
	}{
		{"都为0不启用", 0, 0, false},
		{"正常区间", 0.85, 0.55, false},
		{"target大于trigger", 0.55, 0.85, true},
		{"target等于trigger", 0.6, 0.6, true},
		{"trigger超过1", 1.2, 0.5, true},
		{"target为负", 0.85, -0.1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewContextEngineConfig()
			cfg.CompressionTriggerRatio = tc.trigger
			cfg.CompressionTargetRatio = tc.target
			err := cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v，实际 err=%v", tc.wantErr, err)
			}
		})
	}
}
