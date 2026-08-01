// Package trainer 提供离线自演化训练编排器。
//
// Trainer 编排 "evaluate → update → writeback" 自演化循环：
// 接受 Updater 和 BaseEvaluator，管理检查点保存/恢复和早停。
// Progress 追踪训练进度（epoch/batch/score），Callbacks 提供训练生命周期钩子。
//
// TrainableAgent 接口已迁移至 evolving 根包（evolving/trainable_agent.go），
// 以解决 checkpointing ↔ trainer 循环依赖问题。
//
// 核心方法实现状态：
//   - ✅ SnapshotOperatorsState / RestoreOperatorsState（Operator 状态快照/恢复）
//   - ✅ Predict / PredictOnly / Evaluate（推理与评估）
//   - ✅ Forward（前向推理+评估+轨迹提取，轨迹提取 ⤵️ 待 9.77 回填）
//   - ✅ Train（训练主循环，检查点 ⤵️ 待 9.78 回填）
//   - ✅ GetOperatorRegistry / BindUpdater / UpdaterRequiresForward
//   - ⤵️ ResumeIfNeeded / SaveCheckpointIfNeeded（待 9.78 EvolveCheckpoint 回填）
//
// 文件目录：
//
//	trainer/
//	├── doc.go           # 包文档
//	├── trainer.go       # Trainer 结构体 + 方法实现 + TrainerOption（TrainableAgent 已迁移至 evolving 根包）
//	└── progress.go      # Progress + Callbacks 结构体
//
// 对应 Python 代码：openjiuwen/agent_evolving/trainer/
package trainer
