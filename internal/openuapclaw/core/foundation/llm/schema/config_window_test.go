package schema

import "testing"

// TestModelRequestConfig_ContextWindow不外发 验证 context_window 是内部元数据。
// 请求参数由 base_client 以 map 拼装并只合并 Extra；若 context_window 落进 Extra
// 就会被当厂商参数下发（上游 base_model_client 显式 exclude 该字段）。
func TestModelRequestConfig_ContextWindow不外发(t *testing.T) {
	cfg := NewModelRequestConfig(WithModelName("m"), WithContextWindow(262144))
	if cfg.ContextWindow == nil || *cfg.ContextWindow != 262144 {
		t.Fatalf("ContextWindow = %v, want 262144", cfg.ContextWindow)
	}
	if _, ok := cfg.Extra["context_window"]; ok {
		t.Fatalf("context_window 不得进入 Extra（会被当厂商参数下发）")
	}
}

// TestWithContextWindow_非正数忽略 验证非法值不会写入字段。
func TestWithContextWindow_非正数忽略(t *testing.T) {
	cfg := NewModelRequestConfig(WithContextWindow(0))
	if cfg.ContextWindow != nil {
		t.Fatalf("<=0 应保持 nil，got %v", *cfg.ContextWindow)
	}
}
