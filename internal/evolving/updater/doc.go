// Package updater 提供自演化更新器协议与实现。
//
// Updater 是 Trainer 和 Optimizer 之间的核心协议层。
// Trainer 只依赖 Updater 接口，不直接依赖 BaseOptimizer。
// SingleDimUpdater 委托 BaseOptimizer 的 backward→step 链路；
// MultiDimUpdater 按 domain 分发 signals 到不同 optimizer 并合并 updates。
//
// 文件目录：
//
//	updater/
//	├── doc.go           # 包文档
//	├── protocol.go      # Updater 接口定义
//	├── single_dim/      # SingleDimUpdater（单维更新器）
//	│   ├── doc.go
//	│   └── single_dim.go
//	└── multi_dim/       # MultiDimUpdater（多维更新器）
//	    ├── doc.go
//	    └── multi_dim.go
//
// 对应 Python 代码：openjiuwen/agent_evolving/updater/
package updater
