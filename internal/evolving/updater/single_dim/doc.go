// Package single_dim 提供单维更新器实现。
//
// SingleDimUpdater 委托内部 BaseOptimizer 的 backward→step 链路，
// 将 signals 传递给 optimizer 生成梯度，再由 step 返回更新映射。
// 更新映射由 Trainer 统一应用到 Operator 注册表。
//
// 文件目录：
//
//	single_dim/
//	├── doc.go           # 包文档
//	└── single_dim.go    # SingleDimUpdater 实现
//
// 对应 Python 代码：openjiuwen/agent_evolving/updater/single_dim.py
package single_dim
