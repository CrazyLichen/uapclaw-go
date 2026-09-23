// Package reme 提供 ReMe 算法的记忆总结操作。
//
// ReMe 总结管线：TrajectoryPreprocessOp（轨迹预处理）→ SuccessExtractionOp（成功经验提取）
// → FailureExtractionOp（失败经验提取）→ ComparativeExtractionOp（对比经验提取）
// → ComparativeAllExtractionOp（全量对比提取）→ MemoryValidationOp（记忆验证）
// → MemoryDeduplicationOp（去重）→ UpdateVectorStoreOp（更新向量库）→ PersistMemoryOp（持久化）。
// 所有操作通过 SequentialOp 顺序组合，从轨迹中蒸馏经验知识并持久化。
//
// 文件目录：
//
//	task/reme/
//	├── doc.go       # 包文档
//	├── update.go    # 9 个总结 Op 实现
//	├── prompt.go    # 5 个提取/验证提示词
//	└── utils.go     # ParseJSONExperienceResponse + CalculateCosineSimilarity
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/summary/task/reme/
package reme
