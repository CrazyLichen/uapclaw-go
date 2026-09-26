package observability

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// namespace 注册回调时使用的命名空间（用于注销）
	// Python: _NAMESPACE = "agent_teams.observability"
	namespace = "agent_teams.observability"
	// callbackTracerName Callback 层 tracer 名称
	// Python: _CALLBACK_TRACER_NAME
	callbackTracerName = "openjiuwen.agent_teams.observability"
	// monitorTracerName Monitor 层 tracer 名称
	// Python: _MONITOR_TRACER_NAME
	monitorTracerName = "openjiuwen.agent_teams.observability.monitor"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventListenerRegistrar 事件监听注册接口，打破 observability → agent 循环依赖。
// TeamAgent 隐式实现此接口，AttachToTeamAgent / DetachFromTeamAgent 通过接口操作。
// Python: TeamAgent.add_event_listener / remove_event_listener
type EventListenerRegistrar interface {
	// AddEventListener 添加事件监听器，返回可移除的句柄
	AddEventListener(handler messager.MessagerHandler) *messager.EventListenerHandle
	// RemoveEventListener 移除事件监听器
	RemoveEventListener(handle *messager.EventListenerHandle)
}

// ObservabilityOption InitObservability 的函数选项。
type ObservabilityOption func(*observabilityOptions)

type observabilityOptions struct {
	spanExporterOverride sdktrace.SpanExporter
}

// WithSpanExporterOverride 设置 span 导出器覆盖（测试用）。
// Python: init_observability(config, span_exporter_override=...)
func WithSpanExporterOverride(exporter sdktrace.SpanExporter) ObservabilityOption {
	return func(o *observabilityOptions) {
		o.spanExporterOverride = exporter
	}
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// provider 全局 TracerProvider（由 InitObservability 设置）
	// Python: _provider: Optional[TracerProvider] = None
	provider trace.TracerProvider
	// callbackHandler 全局 OtelCallbackHandler（由 InitObservability 设置）
	// Python: _callback_handler: Optional[OtelCallbackHandler] = None
	callbackHandler *OtelCallbackHandler
	// monitorHandler 全局 OtelTeamMonitorHandler（由 InitObservability 设置）
	// Python: _monitor_handler: Optional[OtelTeamMonitorHandler] = None
	monitorHandler *OtelTeamMonitorHandler
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitObservability 初始化 TracerProvider 并注册回调处理器。
// Python: init_observability(config, span_exporter_override=...)
func InitObservability(config *ObservabilityConfig, opts ...ObservabilityOption) error {
	o := &observabilityOptions{}
	for _, opt := range opts {
		opt(o)
	}

	// Python: if not config.enabled: return
	if !config.Enabled {
		logger.Info(logComponent).Msg("observability 已禁用")
		return nil
	}

	// Python: if _provider is not None: warning; return
	if provider != nil {
		logger.Warn(logComponent).Msg("observability 已初始化，跳过重复初始化")
		return nil
	}

	// Python: resource = Resource.create({"service.name": config.service_name})
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String(config.ServiceName)),
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("创建 OTel resource 失败，使用默认")
		res = resource.Default()
	}

	// Python: sampler = ParentBased(root=TraceIdRatioBased(config.sample_rate))
	sampler := sdktrace.ParentBased(
		sdktrace.TraceIDRatioBased(config.SampleRate),
	)

	// Python: _provider = TracerProvider(resource=resource, sampler=sampler)
	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	}

	// 构建导出器
	exporter, err := buildExporter(config, o.spanExporterOverride)
	if err != nil {
		return fmt.Errorf("构建 OTel 导出器失败: %w", err)
	}

	// Python: SimpleSpanProcessor 用于测试/Console，BatchSpanProcessor 用于生产
	if o.spanExporterOverride != nil || isConsoleExporter(config) {
		tpOpts = append(tpOpts, sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	} else {
		tpOpts = append(tpOpts, sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter)))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	provider = tp

	// Python: 尝试设置全局 TracerProvider
	otel.SetTracerProvider(tp)

	// Python: _callback_handler = OtelCallbackHandler(config, tracer=...)
	callbackHandler = NewOtelCallbackHandler(config, tp.Tracer(callbackTracerName))
	// Python: _monitor_handler = OtelTeamMonitorHandler(config, tracer=...)
	monitorHandler = NewOtelTeamMonitorHandler(config, tp.Tracer(monitorTracerName))

	// Python: _wire_callback_handlers(_callback_handler)
	wireCallbackHandlers(callbackHandler)

	logger.Info(logComponent).Str("service_name", config.ServiceName).
		Str("exporter", config.Exporter).
		Float64("sample_rate", config.SampleRate).
		Msg("observability 初始化完成")

	return nil
}

// ShutdownObservability 注销回调、flush span、重置全局状态。
// Python: shutdown_observability()
func ShutdownObservability() {
	// Python: 注销回调
	if callbackHandler != nil {
		fw := getCallbackFramework()
		if fw != nil {
			fw.UnregisterNamespace(namespace)
		}
	}

	// Python: if _provider is not None: _provider.shutdown()
	if tp, ok := provider.(*sdktrace.TracerProvider); ok && tp != nil {
		if err := tp.Shutdown(context.Background()); err != nil {
			logger.Warn(logComponent).Err(err).Msg("OTel provider shutdown 失败")
		}
	}

	provider = nil
	callbackHandler = nil
	monitorHandler = nil

	logger.Info(logComponent).Msg("observability 已关闭")
}

// GetTracer 返回绑定到当前可观测性 provider 的 tracer。
// Python: get_tracer(name)
//
// 如果 InitObservability 尚未调用，回退到全局 TracerProvider。
func GetTracer(name string) trace.Tracer {
	if provider != nil {
		return provider.Tracer(name)
	}
	return otel.GetTracerProvider().Tracer(name)
}

// AttachToTeamAgent 在 TeamAgent 上注册 Monitor handler。
// Python: attach_to_team_agent(team_agent)
//
// 当前为桩实现（no-op），待 9.55 TeamAgent 完成后回填。
func AttachToTeamAgent(teamAgent EventListenerRegistrar) {
	if monitorHandler == nil {
		logger.Warn(logComponent).Msg("attach_to_team_agent 在 init_observability 之前调用")
		return
	}
	// TODO(#9.55): 待 TeamAgent.add_event_listener 实现后回填
	// Python: team_agent.add_event_listener(_monitor_handler)
	logger.Info(logComponent).Msg("attach_to_team_agent 暂为桩实现（待 9.55 完成）")
}

// DetachFromTeamAgent 反向操作 attach_to_team_agent。
// Python: detach_from_team_agent(team_agent)
//
// 当前为桩实现。
func DetachFromTeamAgent(teamAgent EventListenerRegistrar) {
	if monitorHandler == nil {
		return
	}
	// TODO(#9.55): 待 TeamAgent.remove_event_listener 实现后回填
	// Python: team_agent.remove_event_listener(_monitor_handler)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// wireCallbackHandlers 注册 10 个回调到 CallbackFramework。
// Python: _wire_callback_handlers(handler)
func wireCallbackHandlers(handler *OtelCallbackHandler) {
	fw := getCallbackFramework()
	if fw == nil {
		logger.Warn(logComponent).Msg("CallbackFramework 不可用，跳过回调注册")
		return
	}
	ns := cb.WithNamespace(namespace)

	// LLM 回调（5 个）
	// Python: LLMCallEvents.LLM_INVOKE_INPUT → handler.on_llm_invoke_input
	fw.OnLLM(cb.LLMInvokeInput, handler.OnLLMInvokeInput, ns)
	fw.OnLLM(cb.LLMStreamInput, handler.OnLLMStreamInput, ns)
	fw.OnLLM(cb.LLMStreamOutput, handler.OnLLMStreamOutput, ns)
	fw.OnLLM(cb.LLMInvokeOutput, handler.OnLLMInvokeOutput, ns)
	fw.OnLLM(cb.LLMCallError, handler.OnLLMCallError, ns)

	// Tool 回调（3 个）
	// Python: ToolCallEvents.TOOL_CALL_STARTED → handler.on_tool_call_started
	fw.OnTool(cb.ToolCallStarted, handler.OnToolCallStarted, ns)
	fw.OnTool(cb.ToolCallFinished, handler.OnToolCallFinished, ns)
	fw.OnTool(cb.ToolCallError, handler.OnToolCallError, ns)

	// Agent 回调（2 个）
	// Python: AgentEvents.AGENT_INVOKE_INPUT → handler.on_agent_invoke_input
	fw.OnGlobalAgent(cb.GlobalAgentInvokeInput, handler.OnAgentInvokeInput, ns)
	fw.OnGlobalAgent(cb.GlobalAgentInvokeOutput, handler.OnAgentInvokeOutput, ns)
}

// buildExporter 构建 span 导出器。
// Python: _build_exporter(config)
func buildExporter(config *ObservabilityConfig, override sdktrace.SpanExporter) (sdktrace.SpanExporter, error) {
	if override != nil {
		return override, nil
	}
	switch config.Exporter {
	case "console":
		// Python: ConsoleSpanExporter()
		return stdouttrace.New(stdouttrace.WithWriter(os.Stdout))
	case "otlp_grpc":
		// Python: OTLPSpanExporter(endpoint=config.endpoint, insecure=True)
		return otlptracegrpc.New(
			context.Background(),
			otlptracegrpc.WithEndpoint(config.Endpoint),
			otlptracegrpc.WithInsecure(),
		)
	case "otlp_http":
		return nil, fmt.Errorf("otlp_http 导出器暂未实现，请使用 otlp_grpc 或 console")
	default:
		return nil, fmt.Errorf("不支持的导出器类型: %s", config.Exporter)
	}
}

// isConsoleExporter 检查是否为 console 导出器。
func isConsoleExporter(config *ObservabilityConfig) bool {
	return config.Exporter == "console"
}

// getCallbackFramework 懒加载 CallbackFramework 单例。
// Python: _runner_callback_framework()
func getCallbackFramework() *cb.CallbackFramework {
	return cb.GetCallbackFramework()
}
