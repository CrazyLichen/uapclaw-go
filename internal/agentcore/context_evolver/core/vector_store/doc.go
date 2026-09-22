// Package vector_store 提供上下文记忆演化系统的向量存储。
//
// MemoryVectorStore 是内存向量库，使用余弦相似度搜索。
// Python 用 numpy 计算余弦相似度，Go 手写（点积 / 范数乘积），纯 math 包。
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/vector_store/
package vector_store
