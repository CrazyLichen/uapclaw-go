// Package trainer 提供离线自演化训练编排器。
//
// Trainer 编排 "evaluate → update → writeback" 自演化循环：
// 接受 Updater 和 BaseEvaluator，管理检查点保存/恢复和早停。
// Progress 追踪训练进度（epoch/batch/score），Callbacks 提供训练生命周期钩子。
//
// 文件目录：
//
//	trainer/
//	├── doc.go           # 包文档
//	├── trainer.go       # Trainer 结构体 + 方法桩 + TrainerOption
//	└── progress.go      # Progress + Callbacks 结构体
//
// 对应 Python 代码：openjiuwen/agent_evolving/trainer/
package trainer
