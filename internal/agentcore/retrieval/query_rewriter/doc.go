// Package query_rewriter 提供检索前查询改写器（Query Rewriter）。
//
// 把多轮对话中的省略/指代 query 改写成独立可检索的 standalone_query；
// 历史条数达阈值时用 LLM 压缩历史（失败降级为原文写回），控制提示词
// token 膨胀。只消费 ModelContext 的 GetMessages/SetMessages 两方法，
// 当前轮消息的追加由调用方负责（rewrite → 检索 → add_messages 顺序）。
//
// 文件目录：
//
//	query_rewriter/
//	├── doc.go                 # 包文档
//	├── embed.go               # go:embed 嵌入提示词模板
//	├── query_rewriter.go      # QueryRewriter 主体（构造/LoadTemplate/Msg2Text/Compress/Rewrite）
//	├── repair.go              # 模块级函数（fillTemplate/extractJSON/parseLLMJSON/force 三兄弟/schemaRepair）
//	└── prompts/               # 4 个提示词模板（zh/en × compression/intention_completion）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/query_rewriter/
package query_rewriter
