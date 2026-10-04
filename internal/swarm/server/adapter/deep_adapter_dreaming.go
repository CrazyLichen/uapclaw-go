package adapter

import (
	"context"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
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
//  4. try: await self._instance.memory.dreaming.start_dreaming(...)
//  5. except Exception: logger.warning(...); # _dreaming_started 保持 False
//  6. self._dreaming_started = orch is not None  # 仅在成功后设置
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

	// 步骤 4: 调用 swarm memory dreaming.startDreaming(...)
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
		// 对齐 Python: except Exception as exc: logger.warning(...)
		// dreaming 启动失败不是致命错误，系统仍可正常运行
		logger.Warn(logComponent).Str("dreaming_mode", d.dreamingMode).Err(err).Msg("start_dreaming failed")
		return err
	}

	// Python: self._dreaming_started = orch is not None
	// 仅在成功后设置，不存在中间态
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
//  2. try: await stop_dreaming(mode=mode)
//  3. self._dreaming_started = False  # 仅在成功后清除
//  4. except Exception: logger.warning(...)
func (d *DeepAdapter) TryStopDreaming(ctx context.Context) error {
	// 步骤 1: 未启动则跳过
	if !d.dreamingStarted {
		return nil
	}

	// 步骤 2: 调用 swarm memory dreaming.stopDreaming()
	// Python: await stop_dreaming(mode=mode) — Python 的 stop_dreaming 内部吞错
	// Go 的 StopDreaming 是 void，内部已处理错误日志
	swarmdreaming.StopDreaming(ctx, d.dreamingMode)

	// S-05: 对齐 Python，仅在成功后清除标志
	// Python: self._dreaming_started = False  # 仅在成功后清除
	d.dreamingStarted = false
	logger.Info(logComponent).Str("dreaming_mode", d.dreamingMode).Msg("dreaming stopped")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
