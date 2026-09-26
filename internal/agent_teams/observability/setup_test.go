package observability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// newTestExporter 创建测试用 SpanExporter（使用 stdouttrace 输出到 os.Stdout，测试中忽略输出）
func newTestExporter() sdktrace.SpanExporter {
	exp, err := stdouttrace.New()
	if err != nil {
		panic("创建测试 exporter 失败: " + err.Error())
	}
	return exp
}

func TestInitObservability_创建provider和handler(t *testing.T) {
	provider = nil
	callbackHandler = nil
	monitorHandler = nil

	cfg := &ObservabilityConfig{
		Enabled:     true,
		ServiceName: "test-service",
		Exporter:    "console",
	}
	err := InitObservability(cfg, WithSpanExporterOverride(newTestExporter()))
	require.NoError(t, err)
	defer ShutdownObservability()

	assert.NotNil(t, provider)
	assert.NotNil(t, callbackHandler)
	assert.NotNil(t, monitorHandler)
}

func TestInitObservability_重复初始化不崩溃(t *testing.T) {
	provider = nil
	callbackHandler = nil
	monitorHandler = nil

	cfg := &ObservabilityConfig{Enabled: true, ServiceName: "test", Exporter: "console"}

	err := InitObservability(cfg, WithSpanExporterOverride(newTestExporter()))
	require.NoError(t, err)
	defer ShutdownObservability()

	// 第二次初始化应返回 warn 但不崩溃
	err = InitObservability(cfg, WithSpanExporterOverride(newTestExporter()))
	require.NoError(t, err)
}

func TestInitObservability_enabled为false时为noop(t *testing.T) {
	provider = nil
	callbackHandler = nil
	monitorHandler = nil

	cfg := &ObservabilityConfig{Enabled: false}
	err := InitObservability(cfg)
	require.NoError(t, err)

	assert.Nil(t, provider)
	assert.Nil(t, callbackHandler)
}

func TestShutdownObservability_清理全局变量(t *testing.T) {
	provider = nil
	callbackHandler = nil
	monitorHandler = nil

	cfg := &ObservabilityConfig{Enabled: true, ServiceName: "test", Exporter: "console"}
	err := InitObservability(cfg, WithSpanExporterOverride(newTestExporter()))
	require.NoError(t, err)

	ShutdownObservability()

	assert.Nil(t, provider)
	assert.Nil(t, callbackHandler)
	assert.Nil(t, monitorHandler)
}

func TestGetTracer_无provider时回退(t *testing.T) {
	provider = nil
	tracer := GetTracer("test")
	assert.NotNil(t, tracer)
}

func TestAttachToTeamAgent_桩实现(t *testing.T) {
	// 桩实现，不应 panic
	AttachToTeamAgent(nil)
}

func TestDetachFromTeamAgent_桩实现(t *testing.T) {
	// 桩实现，不应 panic
	DetachFromTeamAgent(nil)
}

func TestIsConsoleExporter(t *testing.T) {
	assert.True(t, isConsoleExporter(&ObservabilityConfig{Exporter: "console"}))
	assert.False(t, isConsoleExporter(&ObservabilityConfig{Exporter: "otlp_grpc"}))
	assert.False(t, isConsoleExporter(&ObservabilityConfig{Exporter: "otlp_http"}))
}

func TestBuildExporter_console(t *testing.T) {
	cfg := &ObservabilityConfig{Exporter: "console"}
	exp, err := buildExporter(cfg, nil)
	assert.NoError(t, err)
	assert.NotNil(t, exp)
}

func TestBuildExporter_otlpGrpc(t *testing.T) {
	cfg := &ObservabilityConfig{Exporter: "otlp_grpc", Endpoint: "localhost:4317"}
	// 不真正连接 gRPC，用 override 代替
	exp, err := buildExporter(cfg, newTestExporter())
	assert.NoError(t, err)
	assert.NotNil(t, exp)
}
