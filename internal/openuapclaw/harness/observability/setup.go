// Package harnessobservability 单 agent 侧观测生命周期所有者。
//
// Python: openjiuwen/harness/observability/（setup.py / span_context.py / run_span.py /
// subagent.py 一包四文件；Go 落本包，一文件对一文件）。
// 与 agent_teams/observability 共享同一 provider（demand 引用计数）：谁先来谁建，
// 引用归零才 shutdown——单 agent 关闭不拆 team 依赖的 provider。
// provider 起来后，通用 OtelCallbackHandler 注册进 CallbackFramework，LLM/tool 事件
// 由 foundation 层对每个 agent（无论单/团队）发射——provider 已初始化即自动有
// LLM/tool span 追踪；team-only 的 OtelTeamMonitorHandler 此处刻意不挂。
//
// 对应 Python 代码：
//
//	openjiuwen/harness/observability/setup.py
//	openjiuwen/harness/observability/span_context.py
//	openjiuwen/harness/observability/run_span.py
//	openjiuwen/harness/observability/subagent.py
package harnessobservability

import (
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	extdemand "github.com/uapclaw/uapclaw-go/internal/openuapclaw/extensions/observability/demand"
)

// logComponent 日志组件。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 常量 ────────────────────────────

const (
	// runtimeKey 单 agent runtime 的 demand 键。
	// Python: _RUNTIME_KEY = "agent"（setup.py:47）。
	runtimeKey = "agent"
	// runTracerName 单 agent 根 span 的 tracer 名。
	// Python: run_span.py _RUN_TRACER_NAME = "openjiuwen.harness.observability"。
	runTracerName = "openjiuwen.harness.observability"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// lifecycleLock 生命周期互斥。Python: _lifecycle_lock（RLock）。
	lifecycleLock sync.Mutex
	// tracingEnabled 单 agent 运行是否应开根 span。与"provider 存在"是两件事：
	// team 子系统可能在同进程持有 provider 而单 agent tracing 关闭，此时单 agent
	// span 不得出现。Python: _tracing_enabled（setup.py:53）。
	tracingEnabled bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AcquireObservability 开启单 agent tracing，provider 不存在时初始化。
// 初始化错误向上传播，调用方以此区分"可选 tracing 失败"与"必须的演进采集失败"。
// 返回 provider 是否已存在（复用他人所有时 exporter 配置不生效）。
// Python: acquire_observability（setup.py:74-99）→ acquire_observability_demand
// （demand.py:130-158，initializer 收 (config, additional_processors)）。
func AcquireObservability(config *obs.ObservabilityConfig) (bool, error) {
	lifecycleLock.Lock()
	defer lifecycleLock.Unlock()

	existed, err := extdemand.AcquireObservabilityDemand(runtimeKey, config,
		func(cfg *obs.ObservabilityConfig, processors []sdktrace.SpanProcessor) error {
			return obs.InitObservability(cfg,
				obs.WithAdditionalSpanProcessors(processors...))
		})
	if err != nil {
		return existed, err
	}
	// Python: install_root_span_fallback()（setup.py:64）——session 注册表回退。
	obs.SetRootSpanFallback(ResolveRunRootSpan)
	tracingEnabled = true
	return existed, nil
}

// ReleaseObservability 关闭单 agent tracing 并释放 provider demand；
// 仅当无其他 runtime 持有 demand 时 provider 才关闭。
// Python: release_observability（setup.py:100-115）。
func ReleaseObservability() {
	lifecycleLock.Lock()
	defer lifecycleLock.Unlock()
	tracingEnabled = false
	ResetRunRootSpans()
	extdemand.ReleaseObservabilityDemand(runtimeKey)
}

// IsTracingEnabled 单 agent 运行是否应开根 span。
// Python: is_tracing_enabled（setup.py:116-119）。
func IsTracingEnabled() bool {
	lifecycleLock.Lock()
	defer lifecycleLock.Unlock()
	return tracingEnabled
}

// IsInitialized 共享观测 runtime 是否已初始化。
// Python: is_initialized（setup.py:121-123）。
func IsInitialized() bool {
	return extdemand.IsInitialized()
}

// GetConfig 当前生效的共享观测配置。
// Python: get_config（setup.py:125-127）。
func GetConfig() *obs.ObservabilityConfig {
	return extdemand.GetConfig()
}
