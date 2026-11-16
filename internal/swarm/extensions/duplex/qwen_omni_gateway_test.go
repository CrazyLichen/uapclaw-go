package duplex

import (
	"os"
	"testing"
)

// TestQwenOmniRealtimeConfig_FromEnvironment_Validate 对齐 qwen_omni_gateway.py:25-59。
func TestQwenOmniRealtimeConfig_FromEnvironment_Validate(t *testing.T) {
	for _, k := range []string{"QWEN_OMNI_REALTIME_URL", "QWEN_OMNI_API_KEY", "QWEN_OMNI_MODEL_NAME", "QWEN_OMNI_VOICE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	// 缺 URL
	config := QwenOmniRealtimeConfigFromEnvironment()
	if err := config.Validate(); err == nil || err.Error() != "请配置 QWEN_OMNI_REALTIME_URL" {
		t.Fatalf("缺 URL 报错不符: %v", err)
	}
	// 非法 scheme
	t.Setenv("QWEN_OMNI_REALTIME_URL", "http://x.example")
	config = QwenOmniRealtimeConfigFromEnvironment()
	if err := config.Validate(); err == nil || err.Error() != "QWEN_OMNI_REALTIME_URL 必须是有效的 ws:// 或 wss:// 地址" {
		t.Fatalf("非法 scheme 报错不符: %v", err)
	}
	// 含用户名密码
	t.Setenv("QWEN_OMNI_REALTIME_URL", "ws://user:pass@x.example")
	config = QwenOmniRealtimeConfigFromEnvironment()
	if err := config.Validate(); err == nil || err.Error() != "QWEN_OMNI_REALTIME_URL 不得包含用户名或密码" {
		t.Fatalf("含凭据报错不符: %v", err)
	}
	// 缺 key / model
	t.Setenv("QWEN_OMNI_REALTIME_URL", "ws://x.example")
	if err := QwenOmniRealtimeConfigFromEnvironment().Validate(); err == nil || err.Error() != "请配置 QWEN_OMNI_API_KEY" {
		t.Fatalf("缺 key 报错不符: %v", err)
	}
	t.Setenv("QWEN_OMNI_API_KEY", "sk-1")
	if err := QwenOmniRealtimeConfigFromEnvironment().Validate(); err == nil || err.Error() != "请配置 QWEN_OMNI_MODEL_NAME" {
		t.Fatalf("缺 model 报错不符: %v", err)
	}
	// voice 缺省 Tina
	t.Setenv("QWEN_OMNI_MODEL_NAME", "qwen-omni")
	config = QwenOmniRealtimeConfigFromEnvironment()
	if config.Voice != "Tina" {
		t.Fatalf("voice 缺省应 Tina, got %q", config.Voice)
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}
}

// TestUpstreamWithModel_查询串合并 对齐 qwen_omni_gateway.py:57-60。
func TestUpstreamWithModel_查询串合并(t *testing.T) {
	t.Setenv("QWEN_OMNI_REALTIME_URL", "wss://api.example/v1/rt?a=1&model=old")
	t.Setenv("QWEN_OMNI_API_KEY", "k")
	t.Setenv("QWEN_OMNI_MODEL_NAME", "m2")
	got := QwenOmniRealtimeConfigFromEnvironment().UpstreamWithModel()
	if got != "wss://api.example/v1/rt?a=1&model=m2" {
		t.Fatalf("model 应覆盖且保持其余参数, got %q", got)
	}
}
