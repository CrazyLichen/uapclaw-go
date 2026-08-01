package checkpointing

// ──────────────────────────── 结构体 ────────────────────────────

// EvolveCheckpoint 训练检查点，用于断点续训。
//
// 保存训练过程中的完整状态快照，包括运行标识、步数进度、
// 最佳指标、各 Operator 状态、更新器和搜索器状态等。
// Trainer 通过 FileCheckpointStore 持久化此数据，
// 并在 ResumeIfNeeded 时恢复。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/state.py EvolveCheckpoint
type EvolveCheckpoint struct {
	// Version 检查点版本标签，默认 "v1"
	Version string
	// RunID 运行标识
	RunID string
	// Step 当前步数 {"epoch": int, "batch": int}
	Step map[string]int
	// Best 最佳指标 {"best_score": float}
	Best map[string]any
	// Seed 随机种子（可选）
	Seed *int
	// OperatorsState 各 Operator 的状态快照 operator_id → state dict
	OperatorsState map[string]map[string]any
	// UpdaterState 更新器状态
	UpdaterState map[string]any
	// SearcherState 搜索器状态
	SearcherState map[string]any
	// LastMetrics 上一次指标 {"current_epoch_score": float}
	LastMetrics map[string]any
}
