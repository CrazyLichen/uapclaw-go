// Package parser 提供文档解析抽象与具体解析器基座：把外部文档源
// （文件路径/URL）解析为统一 reranker.Document，供 Chunker 分块。
//
// 知识检索入库管线：本包解析 → chunker 分块 → ComputeChunkEmbeddings
// 嵌入 → VectorStore 入库。Parser 是管线第一站，与 Chunker 层
// （13.5-13.9）呈前后手关系——Parser 产 Document，Chunker 吃 Document。
//
// 使用方式（对齐 Python Parser 继承体系）：
//   - 具体解析器：嵌入 BaseParser 并覆写 ParseContent（对齐 Python 仅覆写 _parse），
//     构造时 NewBaseParser(self) 注入自身
//   - 组合解析器（13.11 AutoParser）：嵌入 BaseParser 并覆写 Parse 整体路由
//
// 文件目录：
//
//	parser/
//	├── doc.go           # 包文档
//	└── base.go          # Parser 接口 + ParseOptions + BaseParser 模板适配器（13.10）
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/processor/parser/
package parser
