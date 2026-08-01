package evolving

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CheckpointProgress 训练进度接口，供 CheckpointManager 构建检查点时提取进度字段。
//
// 此接口定义在 evolving 根包（而非 trainer 包），解决 checkpointing ↔ trainer 循环依赖：
// checkpointing 包需要从 progress 提取 epoch/best_score 等字段，
// 但不能导入 trainer 包（trainer 导入 checkpointing）。
//
// trainer.Progress 实现此接口，通过方法暴露 CurrentEpoch/BestScore 等字段。
// Seed 方法返回 *int，对齐 Python getattr(progress, "seed", None) 行为。
//
// 对应 Python: DefaultCheckpointManager.build_checkpoint 中
//
//	int(getattr(progress, "current_epoch", 0))
//	float(getattr(progress, "best_score", 0.0))
//	getattr(progress, "seed", None)
type CheckpointProgress interface {
	// GetEpoch 返回当前 epoch 编号。
	GetEpoch() int
	// GetBatchIter 返回当前 batch 迭代步。
	GetBatchIter() int
	// GetBestScore 返回历史最佳分数。
	GetBestScore() float64
	// GetCurrentEpochScore 返回当前 epoch 分数。
	GetCurrentEpochScore() float64
	// GetSeed 返回种子值（nil 表示无种子）。
	GetSeed() *int
}

// ──────────────────────────── 非导出函数 ────────────────────────────
