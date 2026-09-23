// Package ace 提供 ACE（Adaptive Context Evolution）算法的总结操作。
//
// ACE 总结管线通过 反思→策展→增量更新 的循环持续优化 Playbook：
//
//	LoadPlaybookOp >> (ReflectOp | ParallelReflectOp) >> (CurateOp | ParallelCurateOp) >> ApplyDeltaOp >> PersistMemoryOp
//
// 文件目录：
//
//	ace/
//	├── doc.go           # 包文档
//	├── playbook.go      # Playbook/DeltaBatch/Bullet/DeltaOperation/OperationType/BulletTag
//	├── update.go        # LoadPlaybookOp + ReflectOp + ParallelReflectOp + CurateOp + ParallelCurateOp + ApplyDeltaOp + PersistMemoryOp
//	├── prompt.go        # 6 个 prompt 模板 + ACEPrompt 结构体
//	└── utils.go         # SafeJSONLoads 安全 JSON 解析
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/summary/task/ace/
package ace
