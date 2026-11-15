package modalityprobe

import (
	"testing"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/stretchr/testify/assert"
)

// TestIsProbeInFlight 在途标记（Python: _probe_tasks 字典的观察面，
// 深层代理接线测试与预热防抖断言需要）
func TestIsProbeInFlight(t *testing.T) {
	ResetImageSupportCache()
	model := &llm.Model{
		ModelConfig:  &llmschema.ModelRequestConfig{ModelName: "m"},
		ClientConfig: &llmschema.ModelClientConfig{APIBase: "http://inflight"},
	}
	assert.False(t, IsProbeInFlight(model))
	ScheduleImageSupportProbe(model)
	// ScheduleImageSupportProbe 在启动 goroutine 前同步登记在途标记
	assert.True(t, IsProbeInFlight(model))
	ResetImageSupportCache()
	assert.False(t, IsProbeInFlight(model))
}
