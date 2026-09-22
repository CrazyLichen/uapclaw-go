// Package reme 提供 ReMe 算法的记忆检索操作。
//
// ReMe 检索管线：RecallMemoryOp（向量检索）→ RerankMemoryOp（LLM 重排序）→ RewriteMemoryOp（LLM 改写）。
// 三个操作通过 SequentialOp 顺序组合，结果写入 RuntimeContext 供 Agent 使用。
//
// 文件目录：
//
//	reme/
//	├── doc.go       # 包文档
//	├── run.go       # RecallMemoryOp + RerankMemoryOp + RewriteMemoryOp
//	├── prompt.go    # rerank + rewrite 提示词
//	└── utils.go     # ParseJSONListResponse + ParseJSONField
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/retrieve/task/reme/
package reme
