package adapter

import (
	"context"
	"path/filepath"

	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TryStartDreaming 尝试启动 dreaming 进程。
//
// Python: JiuWenClawDeepAdapter.try_start_dreaming() (line 5935-5954)
//
// Python 执行步骤：
//  1. if self._dreaming_started: return
//  2. if not self._dreaming_mode: return
//  3. if busy_checker and busy_checker(): logger.warning("agent busy, skip dreaming"); return
//  4. self._dreaming_started = True
//  5. try: await self._instance.memory.dreaming.start_dreaming(...)
//  6. except Exception: logger.error(...); self._dreaming_started = False
func (d *DeepAdapter) TryStartDreaming(ctx context.Context, busyChecker func() bool) error {
	// 步骤 1: 已启动则跳过
	if d.dreamingStarted {
		return nil
	}

	// 步骤 2: dreaming 模式未启用则跳过
	if d.dreamingMode == "" {
		logger.Info(logComponent).Msg("dreaming mode not enabled, skipping start")
		return nil
	}

	// 步骤 3: 检查是否忙碌
	if busyChecker != nil && busyChecker() {
		logger.Warn(logComponent).Msg("agent busy, skipping dreaming start")
		return nil
	}

	// 步骤 4: 标记已启动
	d.dreamingStarted = true

	// 步骤 5: 调用 swarm memory dreaming.startDreaming(...)
	// Python: from jiuwenswarm.common.utils import get_agent_sessions_dir
	// Python: sessions_dir = str(get_agent_sessions_dir() or "")
	sessionsDir := workspace.AgentSessionsDir()

	// Python: output_name = "memory" if mode == "agent" else "coding_memory"
	outputName := "memory"
	if d.dreamingMode != "agent" {
		outputName = "coding_memory"
	}

	// Python: base_dir = getattr(self, "_agent_workspace_dir", None) or self._workspace_dir
	baseDir := d.agentWorkspaceDir
	if baseDir == "" {
		baseDir = d.workspaceDir
	}
	// Python: output_dir = os.path.join(base_dir, output_name)
	outputDir := filepath.Join(baseDir, outputName)

	// 将 resolveRuntimeLanguage (cn/en) 映射为 dreaming 语言 (zh/en)
	language := d.resolveRuntimeLanguage()
	if language == "cn" || language == "zh" {
		language = "zh"
	} else {
		language = "en"
	}

	// Python: orch = await start_dreaming(
	//     sessions_dir=sessions_dir,
	//     output_dir=output_dir,
	//     mode=mode,
	//     busy_checker=busy_checker,
	// )
	orch, err := swarmdreaming.StartDreaming(ctx, sessionsDir, outputDir, d.dreamingMode, language, busyChecker)
	if err != nil {
		// 步骤 6: 启动失败，回退标记
		// Python: except Exception: logger.error(...); self._dreaming_started = False
		logger.Error(logComponent).Str("dreaming_mode", d.dreamingMode).Err(err).Msg("start_dreaming failed")
		d.dreamingStarted = false
		return err
	}

	// Python: self._dreaming_started = orch is not None
	d.dreamingStarted = orch != nil
	if orch != nil {
		logger.Info(logComponent).
			Str("dreaming_mode", d.dreamingMode).
			Str("sessions_dir", sessionsDir).
			Str("output_dir", outputDir).
			Msg("dreaming started")
	}

	return nil
}

// TryStopDreaming 停止 dreaming 进程。
//
// Python: JiuWenClawDeepAdapter.try_stop_dreaming() (line 5956-5965)
//
// Python 执行步骤：
//  1. if not self._dreaming_started: return
//  2. self._dreaming_started = False
//  3. try: await self._instance.memory.dreaming.stop_dreaming()
//  4. except Exception: logger.error(...)
func (d *DeepAdapter) TryStopDreaming(ctx context.Context) error {
	// 步骤 1: 未启动则跳过
	if !d.dreamingStarted {
		return nil
	}

	// 步骤 2: 标记已停止
	d.dreamingStarted = false

	// 步骤 3: 调用 swarm memory dreaming.stopDreaming()
	// Python: from jiuwenswarm.agents.harness.common.memory.dreaming import stop_dreaming
	// Python: mode = getattr(self, "_dreaming_mode", "agent")
	// Python: await stop_dreaming(mode=mode)
	swarmdreaming.StopDreaming(ctx, d.dreamingMode)
	logger.Info(logComponent).Str("dreaming_mode", d.dreamingMode).Msg("dreaming stopped")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
