package harness_protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHarness 最小 HarnessProtocol 实现，只验证接口签名完整、不实现行为。
// Python: test_protocol.py:306 test_structural_protocols_accept_complete_implementations
type fakeHarness struct{}

// Card 返回静态身份。
func (fakeHarness) Card() HarnessCard {
	c, _ := NewHarnessCard("fake", "1")
	return c
}

// State 返回当前相位。
func (fakeHarness) State() HarnessState { return HarnessStateIdle }

// ProviderSessionID 返回 provider 原生会话标识。
func (fakeHarness) ProviderSessionID() *string { return nil }

// EventBufferConfig 返回观测缓冲策略。
func (fakeHarness) EventBufferConfig() EventBufferConfig {
	return EventBufferConfig{Capacity: 1024, Overflow: EventOverflowPolicyCoalesceOrBlock}
}

// Start 启动一次周期。
func (fakeHarness) Start(context.Context, HarnessContext) error { return nil }

// Stop 停止周期。
func (fakeHarness) Stop(context.Context) error { return nil }

// Events 返回周期级观测流。
func (fakeHarness) Events() HarnessEventCursor { return nil }

// TurnEvents 返回单轮有限观测流。
func (fakeHarness) TurnEvents(*string) HarnessEventCursor { return nil }

// Send 投递输入。
func (fakeHarness) Send(context.Context, HarnessInput, DeliveryMode) (SendReceipt, error) {
	return SendReceipt{}, nil
}

// Abort 中止在飞轮次。
func (fakeHarness) Abort(context.Context, AbortMode) error { return nil }

// Pause 暂停到可恢复边界。
func (fakeHarness) Pause(context.Context) error { return nil }

// Resume 恢复暂停轮次。
func (fakeHarness) Resume(context.Context, *HarnessInput) error { return nil }

// ExportCheckpoint 导出最新 checkpoint。
func (fakeHarness) ExportCheckpoint(context.Context) (*HarnessCheckpoint, error) { return nil, nil }

// fakeModelControl 最小 HarnessModelControl 实现。
// Python: test_protocol.py:947 test_model_control_is_an_optional_protocol_beside_harness_protocol
type fakeModelControl struct{}

// ListModels 返回模型目录。
func (fakeModelControl) ListModels(context.Context) ([]ModelOption, error) { return nil, nil }

// SetModel 切换模型 / 推理强度。
func (fakeModelControl) SetModel(context.Context, ModelSelection) error { return nil }

// fakeProvider 最小 HarnessProvider 实现。
type fakeProvider struct{}

// Card 返回 provider 的 card。
func (fakeProvider) Card() HarnessCard {
	c, _ := NewHarnessCard("fake", "1")
	return c
}

// Create 创建一个未启动的 harness。
func (fakeProvider) Create(JsonObject) (HarnessProtocol, error) { return fakeHarness{}, nil }

// fakeModelConfigProvider 最小 ModelConfigProvider 实现。
type fakeModelConfigProvider struct{}

// ModelConfig 返回运行该选择的配置片段。
func (fakeModelConfigProvider) ModelConfig(ModelChoice, *ModelChoice) (JsonObject, error) {
	return JsonObject{}, nil
}

// TestInterfaces_签名完整 验证 4 个 interface 的编译期满足关系。
func TestInterfaces_签名完整(t *testing.T) {
	// 编译期断言：fake 满足各自接口
	var _ HarnessProtocol = fakeHarness{}
	var _ HarnessModelControl = fakeModelControl{}
	var _ HarnessProvider = fakeProvider{}
	var _ ModelConfigProvider = fakeModelConfigProvider{}

	// 运行期确认可赋值（防止接口被写成不可实现的形态）
	require.Implements(t, (*HarnessProtocol)(nil), fakeHarness{})
	require.Implements(t, (*HarnessModelControl)(nil), fakeModelControl{})
	require.Implements(t, (*HarnessProvider)(nil), fakeProvider{})
	require.Implements(t, (*ModelConfigProvider)(nil), fakeModelConfigProvider{})
}

// TestHarnessModelControl_与HarnessProtocol并列 验证两者是独立接口，不互相嵌入。
// Python: protocol.py:129-137（HarnessModelControl 是 separate Protocol）
func TestHarnessModelControl_与HarnessProtocol并列(t *testing.T) {
	// fakeHarness 实现了 HarnessProtocol 但未实现 HarnessModelControl
	_, ok := any(fakeHarness{}).(HarnessModelControl)
	assert.False(t, ok, "HarnessModelControl 不应被 HarnessProtocol 隐含")
}

// TestHarnessProvider_Create返回HarnessProtocol 验证工厂 SPI 返回契约。
func TestHarnessProvider_Create返回HarnessProtocol(t *testing.T) {
	h, err := fakeProvider{}.Create(nil)
	require.NoError(t, err)
	assert.Equal(t, HarnessStateIdle, h.State())
}

// TestHarnessTelemetry_方法签名 验证 1 方法 Record。
// Python: models.py:70-76 HarnessTelemetry
func TestHarnessTelemetry_方法签名(t *testing.T) {
	rec := &fakeTelemetry{}
	var _ HarnessTelemetry = rec
	rec.Record("m", map[string]any{"a": 1})
	assert.Equal(t, "m", rec.name)
	assert.Equal(t, 1, rec.attr["a"])
}

// fakeTelemetry 最小 HarnessTelemetry 实现。
type fakeTelemetry struct {
	// name 最近一次记录名
	name string
	// attr 最近一次属性
	attr map[string]any
}

// Record 记录一条观测。
func (f *fakeTelemetry) Record(name string, attributes map[string]any) {
	f.name = name
	f.attr = attributes
}
