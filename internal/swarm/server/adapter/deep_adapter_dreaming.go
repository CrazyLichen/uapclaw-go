package adapter

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TryStartDreaming 尝试启动 dreaming 进程。
//
// 对应 Python: JiuWenClawDeepAdapter.try_start_dreaming() (line 5935-5954)
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
		logger.Info(logComponent).Msg("dreaming 模式未启用，跳过启动")
		return nil
	}

	// 步骤 3: 检查是否忙碌
	if busyChecker != nil && busyChecker() {
		logger.Warn(logComponent).Msg("Agent 忙碌中，跳过 dreaming 启动")
		return nil
	}

	// 步骤 4: 标记已启动
	d.dreamingStarted = true

	// 步骤 5: 调用 swarm memory dreaming.startDreaming(...)
	// ⤵️ 10.6.13-18: 调用 swarm memory dreaming.startDreaming(...)
	logger.Info(logComponent).
		Str("dreaming_mode", d.dreamingMode).
		Msg("dreaming 启动（实际调用待回填）")

	return nil
}

// TryStopDreaming 停止 dreaming 进程。
//
// 对应 Python: JiuWenClawDeepAdapter.try_stop_dreaming() (line 5956-5965)
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
	// ⤵️ 10.6.13-18: 调用 swarm memory dreaming.stopDreaming(...)
	logger.Info(logComponent).Msg("dreaming 停止（实际调用待回填）")

	return nil
}
