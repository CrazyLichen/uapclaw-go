// Package schema 提供上下文记忆演化系统的数据模式定义。
//
// VectorNode 是向量存储的标准序列化格式，
// 所有记忆类型（ACE/ReasoningBank/ReMe）通过此格式统一序列化。
//
// 文件目录：
//
//	schema/
//	├── doc.go            # 包文档
//	└── vector_node.go    # VectorNode 向量存储格式
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/schema/
package schema
