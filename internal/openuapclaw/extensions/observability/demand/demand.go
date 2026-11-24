// Package demand provider demand 引用计数协调（跨 runtime 共享 provider）。
//
// Python: extensions/observability/demand.py:130-200。OTel 每进程仅一个全局
// TracerProvider，初始化后其余为 no-op——多 runtime（单 agent / Team）进程里
// 谁先初始化谁赢，其余静默复用；因此任何 runtime 都不得在其他 runtime 仍
// 依赖时拆 provider。本层持有共享记账：每个 runtime 用前 acquire、关时
// release，最后一个 demand 消失才 shutdown。runtime 无关——runtime 专属的
// initializer 由调用方提供，拥有 runtime 的层（harness/observability 与
// agent_teams/observability）各自保有自己的生命周期。
//
// 独立包原因：父级 agent_teams/observability 不能 import 本包再到 trajectory
// （obs→demand→trajectory→obs 环），镜像 Python extensions 层位（偏差登记
// spec §4）。
package demand

import (
	"fmt"
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	obsdemand "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability/demand"
)

// logComponent 日志组件。
const logComponent = logger.ComponentTeam

// ObservabilityInitializer 用共享 provider 初始化一个 runtime 的观测。
// 收到 runtime 配置与协调者要挂载的 span processors。
// Python: ObservabilityInitializer = Callable[[config, Sequence[SpanProcessor]], None]。
type ObservabilityInitializer func(config *obs.ObservabilityConfig, processors []sdktrace.SpanProcessor) error

var (
	// mu demand 记账锁。Python: _DEMAND_LOCK（RLock）。
	mu sync.Mutex
	// activeRuntimes 当前持有 demand 的 runtime 键集合。
	// Python: _ACTIVE_RUNTIMES: set[str]（闭集：typo 会造出无人释放的 demand）。
	activeRuntimes = map[string]bool{}
	// providerOwned 仅关闭由本协调者创建的 provider。provider 可能先被 RL
	// 采集器或别的 SDK 入口初始化——那些调用者保有它的生命周期。
	// Python: _PROVIDER_OWNED。
	providerOwned bool
)

// AcquireObservabilityDemand 获取一个 runtime 对共享 provider 的 demand。
// provider 不存在时调 initializer（传 trajectory processor 等）；已存在则复用。
// Python: acquire_observability_demand（demand.py:130-168）——初始化错误向上
// 传播；返回 provider 是否已存在（复用则 exporter 配置不生效）。
func AcquireObservabilityDemand(runtime string, config *obs.ObservabilityConfig, initializer ObservabilityInitializer) (bool, error) {
	// Python: _DEMAND_LOCK 为 RLock；Go 无可重入锁，内部改走无锁变体
	// isInitializedLocked（偏差 spec §4）。
	mu.Lock()
	defer mu.Unlock()
	if runtime != "agent" && runtime != "team" {
		return false, fmt.Errorf("unknown observability runtime: %s", runtime)
	}
	if activeRuntimes[runtime] && isInitializedLocked() {
		return true, nil
	}
	existed := isInitializedLocked()
	// Python: (get_trajectory_span_processor(), get_span_record_processor())——
	// span_record 落盘域未移植，偏差登记 spec §4。
	processors := []sdktrace.SpanProcessor{obsdemand.GetTrajectorySpanProcessor()}
	if err := initializer(config, processors); err != nil {
		return existed, err
	}
	if !isInitializedLocked() {
		return existed, fmt.Errorf("%s observability initialization did not create a provider", runtime)
	}
	if !existed {
		providerOwned = true
	}
	activeRuntimes[runtime] = true
	return existed, nil
}

// ReleaseObservabilityDemand 释放一个 runtime 的 demand；最后一个 demand 释放
// 且 provider 由协调者创建时执行共享 shutdown。
// Python: release_observability_demand（demand.py:170-200）。
func ReleaseObservabilityDemand(runtime string) {
	mu.Lock()
	defer mu.Unlock()
	if runtime != "agent" && runtime != "team" {
		return
	}
	delete(activeRuntimes, runtime)
	if len(activeRuntimes) > 0 || !providerOwned {
		return
	}
	if isInitializedLocked() {
		logger.Info(logComponent).Msg("last observability demand released, shutting provider down")
		obs.ShutdownObservability()
		providerOwned = false
	}
}

// IsInitialized 共享 provider 是否已初始化。
// Python: is_initialized 经 demand 直查共享 setup。
func IsInitialized() bool {
	mu.Lock()
	defer mu.Unlock()
	return isInitializedLocked()
}

// isInitializedLocked 无锁内部变体（持 mu 调用）。
func isInitializedLocked() bool {
	return obs.ProviderInitialized()
}

// GetConfig 当前生效的共享观测配置。
// Python: get_config()。
func GetConfig() *obs.ObservabilityConfig {
	return obs.ActiveConfig()
}
