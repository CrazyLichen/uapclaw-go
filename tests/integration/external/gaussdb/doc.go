//go:build integration

// Package gaussdb 提供 GaussDB 向量库的集成测试。
//
// 被测边界是真实数据库：连接参数从环境变量 GAUSS_DB_CONN_STRING 读取，
// 未设置时全部用例 Skip，因此归入集成测试而非单元测试。
//
// 文件目录：
//
//	gaussdb/
//	├── doc.go       # 包文档
//	└── gauss_test.go # GaussVectorStore 建表、写入与检索用例
//
// 对应 Python 代码：openjiuwen/core/foundation/store/vector/（GaussDB 向量库适配）
package gaussdb
