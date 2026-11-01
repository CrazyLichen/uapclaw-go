package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
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

// TestBuildExporter_otlpHTTP默认endpoint S-25 回归：
// 默认 endpoint 带 scheme，必须走 WithEndpointURL 才能构造出合法导出器（不报错）。
func TestBuildExporter_otlpHTTP默认endpoint(t *testing.T) {
	cfg := DefaultObservabilityConfig()
	cfg.Exporter = "otlp_http"

	exp, err := buildExporter(cfg, nil)
	assert.NoError(t, err)
	assert.NotNil(t, exp)
}

// TestBuildExporter_otlpGrpc默认endpoint S-25 回归：
// grpc 分支需剥离 scheme 才能得到合法 target（构造本身不连接，不会阻塞）。
func TestBuildExporter_otlpGrpc默认endpoint(t *testing.T) {
	cfg := DefaultObservabilityConfig()
	cfg.Exporter = "otlp_grpc"

	exp, err := buildExporter(cfg, nil)
	assert.NoError(t, err)
	assert.NotNil(t, exp)
}

// TestParseOTLPEndpoint 表驱动验证 endpoint 拆分（host:port / path / insecure）。
func TestParseOTLPEndpoint(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantHostPort string
		wantPath     string
		wantInsecure bool
	}{
		{"带 http scheme", "http://localhost:4317", "localhost:4317", "", true},
		{"带 https scheme", "https://collector:4317", "collector:4317", "", false},
		{"无 scheme", "localhost:4317", "localhost:4317", "", true},
		{"带路径", "http://localhost:4317/v1/traces", "localhost:4317", "/v1/traces", true},
		{"含空白", "  http://localhost:4317  ", "localhost:4317", "", true},
		{"空串", "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hostPort, urlPath, insecure := parseOTLPEndpoint(c.in)
			assert.Equal(t, c.wantHostPort, hostPort)
			assert.Equal(t, c.wantPath, urlPath)
			assert.Equal(t, c.wantInsecure, insecure)
		})
	}
}

// TestShutdownObservability_只注销自己的回调 M-23：
// 对齐 Python 逐个 unregister_sync——不调用 UnregisterNamespace，
// 因此其他模块在同一事件上的回调必须存活。
func TestShutdownObservability_只注销自己的回调(t *testing.T) {
	// 清理可能残留的全局状态
	ShutdownObservability()

	cfg := DefaultObservabilityConfig()
	cfg.Exporter = "console"
	require.NoError(t, InitObservability(cfg))

	fw := getCallbackFramework()
	require.NotNil(t, fw)

	const foreignNS = "observability.test.foreign"
	foreign := func(_ context.Context, _ *cb.LLMCallEventData) any { return nil }
	fw.OnLLM(cb.LLMInvokeInput, foreign, cb.WithNamespace(foreignNS))
	t.Cleanup(func() { fw.OffLLM(cb.LLMInvokeInput, foreign) })

	ShutdownObservability()

	for _, info := range fw.GetCallbacksForTest(cb.LLMInvokeInput) {
		assert.NotEqual(t, namespace, info.Namespace, "observability 回调应已逐个注销")
	}
	found := false
	for _, info := range fw.GetCallbacksForTest(cb.LLMInvokeInput) {
		if info.Namespace == foreignNS {
			found = true
		}
	}
	assert.True(t, found, "其他 namespace 的回调不应被误删")
}
