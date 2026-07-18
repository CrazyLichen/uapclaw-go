// Package multi_dim 提供多维更新器实现。
//
// MultiDimUpdater 按 Operator domain（llm/tool/memory/skill_experience）
// 分配 signals 到对应域的优化器，合并各域的更新映射，由 Trainer 统一应用。
//
// 一致性约束：维度仅按 Operator domain 划分，用户只需配置
// domain_optimizers 映射，每个域仅允许一个优化器。
//
// 当前 bind/process/get_state/load_state 为默认实现（返回零值），
// 后续具体子类实现时重写。
//
// 文件目录：
//
//	multi_dim/
//	├── doc.go          # 包文档
//	└── multi_dim.go    # MultiDimUpdater 实现
//
// 对应 Python 代码：openjiuwen/agent_evolving/updater/multi_dim.py
package multi_dim
