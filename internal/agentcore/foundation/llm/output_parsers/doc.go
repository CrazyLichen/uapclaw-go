// Package output_parsers 提供 LLM 输出解析器，将 LLM 原始输出转换为结构化数据。
//
// 本包是领域二（LLM 基础层）2.16 节的实现，提供两种内置解析器：
//
//	JsonOutputParser      — 从 LLM 输出中提取 JSON（支持 markdown 代码块和裸 JSON）
//	MarkdownOutputParser  — 将 LLM 输出解析为结构化 Markdown 元素
//
// # 接口
//
//	BaseOutputParser（定义在 model_clients 包）
//	  Parse(input any)       — 非流式解析，输入 string 或 *AssistantMessage
//	  StreamParse(chunks)    — 流式解析，独立使用场景预留
//
// # 使用方式
//
// 非流式（在 Invoke 时传入 parser）：
//
//	parser := output_parsers.NewJsonOutputParser()
//	msg, _ := model.Invoke(ctx, messages, model_clients.WithInvokeOutputParser(parser))
//	// msg.ParserContent 包含解析后的 map[string]any
//
// 流式（在 Stream 时传入 parser，_astream_with_parser 自动处理）：
//
//	result, _ := model.Stream(ctx, messages, model_clients.WithStreamOutputParser(parser))
//	for chunk := range result.Chunks {
//	    if chunk.ParserContent != nil {
//	        // 解析成功
//	    }
//	}
//
// # 文件清单
//
//	output_parsers/
//	  doc.go                    — 包文档（本文件）
//	  output_parser.go          — 重新导出类型 + 辅助函数
//	  json_output_parser.go     — JsonOutputParser
//	  json_output_parser_test.go
//	  markdown_types.go         — MarkdownElementType/MarkdownElement/MarkdownContent
//	  markdown_output_parser.go — MarkdownOutputParser
//	  markdown_output_parser_test.go
//
// # Python 对应路径
//
//	openjiuwen/core/foundation/llm/output_parsers/output_parser.py          — BaseOutputParser
//	openjiuwen/core/foundation/llm/output_parsers/json_output_parser.py     — JsonOutputParser
//	openjiuwen/core/foundation/llm/output_parsers/markdown_output_parser.py — MarkdownOutputParser
package output_parsers
