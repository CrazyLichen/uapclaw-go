// Package query 提供查询过滤表达式的构建与转换框架。
//
// 本包定义了统一的查询表达式抽象（QueryExpr 接口），支持 9 种表达式类型
// （Comparison/Range/Arithmetic/Null/JSON/Array/Logical/Match/Custom）
// 和便捷的工厂函数（Eq/Gt/Lt 等）及逻辑组合函数（And/Or/Not），
// 使得上层可以用类型安全的方式组合过滤条件。
// 通过注册表机制，表达式可按后端名称自动转换为数据库特定格式
// （如 Milvus 过滤字符串、Chroma where 字典）。
//
// 文件目录：
//
//	query/
//	├── doc.go                  # 包文档
//	├── base.go                 # QueryExpr 接口 + 9 种表达式结构体 + 辅助函数
//	├── factory.go              # 便捷工厂函数 + 逻辑组合函数
//	├── registry.go             # 注册表 + RegisterDatabaseQueryLanguage()
//	├── milvus_query_func.go    # Milvus 后端转换函数 + init() 注册
//	└── chroma_query_func.go    # Chroma 后端转换函数 + init() 注册
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/query/
//
// 核心类型/接口索引：
//
//	QueryExpr                — 查询过滤表达式接口
//	QueryLanguageDefinition  — 数据库查询语言定义（8 个转换回调）
//	ComparisonExpr           — 比较表达式（==, !=, >, <, >=, <=）
//	RangeExpr                — 范围表达式（in, like）
//	ArithmeticExpr           — 算术表达式（字段 + - * / % ** 比较值）
//	NullExpr                 — 空值检查表达式（is null / is not null）
//	JSONExpr                 — JSON 字段查询表达式
//	ArrayExpr                — 数组字段查询表达式
//	LogicalExpr              — 逻辑组合表达式（and, or, not）
//	MatchExpr                — 文本匹配表达式（prefix, suffix, infix, exact）
//	CustomExpr               — 自定义原始表达式
package query
