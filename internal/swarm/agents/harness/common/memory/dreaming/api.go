package dreaming

import (
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// orchestrators 全局 Orchestrator 映射。
// Python: _orchestrators: dict[str, DreamingOrchestrator] = {}
var orchestrators sync.Map

// apiLogComponent 日志组件标识
var apiLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// GetDreamingOrchestrator 获取指定 mode 的 Orchestrator 实例。
// Python: get_dreaming_orchestrator(mode)
func GetDreamingOrchestrator(mode string) *dreaming.DreamingOrchestrator {
	if v, ok := orchestrators.Load(mode); ok {
		return v.(*dreaming.DreamingOrchestrator)
	}
	return nil
}

// StartDreaming 启动 dreaming 服务。
// 幂等：同一 mode 重复调用返回已有实例。
//
// Python: start_dreaming(sessions_dir, output_dir, mode, busy_checker)
func StartDreaming(sessionsDir, outputDir, mode, language string, busyChecker func() bool) (*dreaming.DreamingOrchestrator, error) {
	// Python: if mode in _orchestrators: return _orchestrators[mode]
	if v, ok := orchestrators.Load(mode); ok {
		return v.(*dreaming.DreamingOrchestrator), nil
	}

	// Python: cfg = DreamingConfig.load(mode)
	cfg := LoadDreamingConfig(mode)
	if !cfg.Enabled {
		// Python: logger.info("[dreaming] %s mode enabled=false, not started", mode)
		logger.Info(apiLogComponent).Str("mode", mode).Msg("[dreaming] enabled=false, not started")
		return nil, nil
	}

	// Python: sweeper = Sweeper(sessions_dir, output_dir, mode=mode)
	sweeper := NewSweeper(sessionsDir, outputDir, mode, language)
	// Python: sweeper.init()
	if err := sweeper.Init(); err != nil {
		logger.Error(apiLogComponent).Str("mode", mode).Err(err).Msg("[dreaming] sweeper init failed")
		return nil, err
	}

	// Python: orch = DreamingOrchestrator(
	//     sweep_fn=sweeper.run_sweep,
	//     interval_seconds=cfg.interval_seconds,
	//     busy_checker=busy_checker,
	//     name=f"dreaming-{mode}",
	// )
	interval := time.Duration(cfg.IntervalSeconds * float64(time.Second))
	orch := dreaming.NewDreamingOrchestrator(
		func(ctx context.Context) error {
			return sweeper.RunSweep(ctx)
		},
		interval,
		dreaming.WithBusyChecker(busyChecker),
		dreaming.WithName("dreaming-"+mode),
	)

	// Python: await orch.start()
	ctx := context.Background()
	if err := orch.Start(ctx); err != nil {
		logger.Error(apiLogComponent).Str("mode", mode).Err(err).Msg("[dreaming] orchestrator start failed")
		return nil, err
	}

	// Python: _orchestrators[mode] = orch
	orchestrators.Store(mode, orch)
	logger.Info(apiLogComponent).Str("mode", mode).Msg("[dreaming] started")
	return orch, nil
}

// StopDreaming 停止 dreaming 服务。
// 幂等：同一 mode 重复调用不做任何操作。
// mode 为空时停止所有模式。
//
// Python: stop_dreaming(mode)
func StopDreaming(mode string) {
	ctx := context.Background()
	if mode != "" {
		// Python: orch = _orchestrators.pop(mode, None)
		if v, loaded := orchestrators.LoadAndDelete(mode); loaded {
			orch := v.(*dreaming.DreamingOrchestrator)
			if err := orch.Stop(ctx); err != nil {
				// Python: logger.warning("[dreaming] stop(%s) exception: %s", mode, exc)
				logger.Warn(apiLogComponent).Str("mode", mode).Err(err).Msg("[dreaming] stop exception")
			}
		}
		return
	}

	// Python: for m, orch in list(_orchestrators.items()):
	orchestrators.Range(func(key, value any) bool {
		orchestrators.Delete(key)
		orch := value.(*dreaming.DreamingOrchestrator)
		if err := orch.Stop(ctx); err != nil {
			m, _ := key.(string)
			logger.Warn(apiLogComponent).Str("mode", m).Err(err).Msg("[dreaming] stop exception")
		}
		return true
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resetOrchestrators 清空全局 orchestrators（仅测试使用）。
func resetOrchestrators() {
	orchestrators.Range(func(key, _ any) bool {
		orchestrators.Delete(key)
		return true
	})
}
