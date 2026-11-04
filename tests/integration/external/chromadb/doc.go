//go:build integration

// Package chromadb 提供 ChromaDB 向量库的集成测试。
//
// 被测边界是真实 Chroma 服务：用例需要本地或远程 ChromaDB 实例，当前无实例时
// 全部 Skip 并给出提示，因此归入集成测试而非单元测试。
//
// 文件目录：
//
//	chromadb/
//	├── doc.go        # 包文档
//	└── chroma_test.go # 集合创建、写入、检索与删除用例
//
// 对应 Python 代码：openjiuwen/core/foundation/store/vector/（Chroma 向量库适配）
package chromadb
