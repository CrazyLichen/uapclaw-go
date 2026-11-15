package telemetry

import (
	"testing"
)

// TestGetSharedOtlpReceiver_单例 单例恒等（Python get_shared_otlp_receiver 语义）
func TestGetSharedOtlpReceiver_单例(t *testing.T) {
	first := GetSharedOtlpReceiver()
	second := GetSharedOtlpReceiver()
	if first != second {
		t.Fatal("GetSharedOtlpReceiver 应返回同一实例")
	}
}

// TestNewSharedOtlpReceiver_初始态 新实例未启动、未禁用、端点为空
func TestNewSharedOtlpReceiver_初始态(t *testing.T) {
	r := NewSharedOtlpReceiver()
	if r.EndpointURL() != "" || r.GrpcEndpoint() != "" || r.BaseURL() != "" {
		t.Fatal("新实例端点应为空")
	}
	if _, ok := r.Subscribe(func(map[string]any) {}); !ok {
		t.Fatal("新实例首次订阅应成功并惰性启动")
	}
	r.Unsubscribe(1)
	if r.EndpointURL() == "" {
		t.Fatal("惰性启动后应产出 HTTP 端点")
	}
	r.Aclose(t.Context())
	if _, ok := r.Subscribe(func(map[string]any) {}); ok {
		t.Fatal("Aclose 后订阅应失败（永久禁用）")
	}
}
